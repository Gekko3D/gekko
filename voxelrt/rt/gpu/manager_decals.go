package gpu

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

type DecalBatchDesc struct {
	AtlasKey      string
	FirstInstance uint32
	InstanceCount uint32
}

func (m *GpuBufferManager) UpdateDecals(data []byte, count uint32) bool {
	if m == nil {
		return false
	}
	m.DecalCount = count
	if count == 0 {
		return false
	}
	return m.ensureBuffer("DecalBuf", &m.DecalBuf, data, wgpu.BufferUsageStorage, 0)
}

func (m *GpuBufferManager) CreateDecalTargets(w, h uint32) {
	if m == nil || m.Device == nil || w == 0 || h == 0 {
		return
	}
	if m.DecalView != nil {
		m.DecalView.Release()
	}
	if m.DecalTex != nil {
		m.DecalTex.Release()
	}
	tex, err := m.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "Deferred Decal Overlay", Size: wgpu.Extent3D{Width: w, Height: h, DepthOrArrayLayers: 1},
		MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D,
		Format: wgpu.TextureFormatRGBA8Unorm, Usage: wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding,
	})
	if err != nil {
		panic(fmt.Errorf("create decal target: %w", err))
	}
	view, err := tex.CreateView(nil)
	if err != nil {
		tex.Release()
		panic(fmt.Errorf("create decal target view: %w", err))
	}
	m.DecalTex, m.DecalView = tex, view
	m.RebuildDecalBindGroups(m.decalBGPipeline)
}

func (m *GpuBufferManager) CreateDecalFallback() {
	if m == nil || m.Device == nil || m.DecalFallbackView != nil {
		return
	}
	tex, err := m.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "Deferred Decal Fallback", Size: wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1},
		MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D,
		Format: wgpu.TextureFormatRGBA8Unorm, Usage: wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
	})
	if err != nil {
		panic(fmt.Errorf("create decal fallback: %w", err))
	}
	view, err := tex.CreateView(nil)
	if err != nil {
		tex.Release()
		panic(fmt.Errorf("create decal fallback view: %w", err))
	}
	m.DecalFallbackTex, m.DecalFallbackView = tex, view
	if err := m.Device.GetQueue().WriteTexture(tex.AsImageCopy(), []byte{0, 0, 0, 0}, &wgpu.TextureDataLayout{BytesPerRow: 4, RowsPerImage: 1}, &wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1}); err != nil {
		panic(fmt.Errorf("clear decal fallback: %w", err))
	}
}

func (m *GpuBufferManager) decalLightingView() *wgpu.TextureView {
	if m != nil && m.DecalView != nil {
		return m.DecalView
	}
	if m != nil {
		return m.DecalFallbackView
	}
	return nil
}

func (m *GpuBufferManager) SyncDecalBatches(pipeline *wgpu.RenderPipeline, batches []DecalBatchDesc) {
	if m == nil {
		return
	}
	for _, batch := range m.DecalBatches {
		if batch.BindGroup0 != nil {
			batch.BindGroup0.Release()
		}
	}
	m.DecalBatches = m.DecalBatches[:0]
	if pipeline == nil || m.DecalCount == 0 || len(batches) == 0 || m.CameraBuf == nil || m.DecalBuf == nil || m.DepthView == nil || m.NormalView == nil {
		return
	}
	m.ensureSpriteAtlasSampler()
	if m.SpriteAtlasSampler == nil {
		return
	}
	for _, batch := range batches {
		if batch.InstanceCount == 0 {
			continue
		}
		atlas := m.spriteAtlasView(batch.AtlasKey)
		if atlas == nil {
			continue
		}
		bg, err := m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Label: "Decals BindGroup 0", Layout: pipeline.GetBindGroupLayout(0), Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize}, {Binding: 1, Buffer: m.DecalBuf, Size: wgpu.WholeSize},
			{Binding: 2, TextureView: atlas}, {Binding: 3, Sampler: m.SpriteAtlasSampler},
		}})
		if err != nil {
			panic(fmt.Errorf("create decals bind group 0: %w", err))
		}
		m.DecalBatches = append(m.DecalBatches, DecalRenderBatch{FirstInstance: batch.FirstInstance, InstanceCount: batch.InstanceCount, AtlasKey: batch.AtlasKey, AtlasView: atlas, BindGroup0: bg})
	}
	m.syncDecalGBufferBindGroup(pipeline)
}

func (m *GpuBufferManager) syncDecalGBufferBindGroup(pipeline *wgpu.RenderPipeline) {
	if m == nil || pipeline == nil || m.DecalCount == 0 || m.DepthView == nil || m.NormalView == nil {
		return
	}
	// Group 1 is shared by every atlas batch and is rebuilt lazily when sampled
	// G-buffer views change.
	m.decalBGPipeline = pipeline
	m.decalBGCamera, m.decalBGBuf, m.decalBGDepth, m.decalBGNormal = m.CameraBuf, m.DecalBuf, m.DepthView, m.NormalView
}

func (m *GpuBufferManager) DecalGBufferBindGroup(pipeline *wgpu.RenderPipeline) *wgpu.BindGroup {
	if m == nil || pipeline == nil || m.Device == nil || m.DepthView == nil || m.NormalView == nil {
		return nil
	}
	// Group 1 only contains G-buffer textures. Rebuild it when either view changes.
	if m.DecalBindGroup1 != nil && m.decalBGPipeline == pipeline && m.decalBGDepth == m.DepthView && m.decalBGNormal == m.NormalView {
		return m.DecalBindGroup1
	}
	if m.DecalBindGroup1 != nil {
		m.DecalBindGroup1.Release()
	}
	m.DecalBindGroup1 = m.createDecalGBufferBindGroup(pipeline)
	m.decalBGPipeline, m.decalBGDepth, m.decalBGNormal = pipeline, m.DepthView, m.NormalView
	return m.DecalBindGroup1
}

func (m *GpuBufferManager) createDecalGBufferBindGroup(pipeline *wgpu.RenderPipeline) *wgpu.BindGroup {
	bg, err := m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Label: "Decals BindGroup 1", Layout: pipeline.GetBindGroupLayout(1), Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: m.DepthView}, {Binding: 1, TextureView: m.NormalView}}})
	if err != nil {
		panic(fmt.Errorf("create decals bind group 1: %w", err))
	}
	return bg
}

func (m *GpuBufferManager) RebuildDecalBindGroups(pipeline *wgpu.RenderPipeline) {
	if m == nil {
		return
	}
	descs := make([]DecalBatchDesc, 0, len(m.DecalBatches))
	for _, batch := range m.DecalBatches {
		descs = append(descs, DecalBatchDesc{AtlasKey: batch.AtlasKey, FirstInstance: batch.FirstInstance, InstanceCount: batch.InstanceCount})
	}
	m.SyncDecalBatches(pipeline, descs)
}

func (m *GpuBufferManager) ReleaseDecals() {
	if m == nil {
		return
	}
	for _, batch := range m.DecalBatches {
		if batch.BindGroup0 != nil {
			batch.BindGroup0.Release()
		}
	}
	m.DecalBatches = nil
	if m.DecalBindGroup1 != nil {
		m.DecalBindGroup1.Release()
		m.DecalBindGroup1 = nil
	}
	if m.DecalView != nil {
		m.DecalView.Release()
		m.DecalView = nil
	}
	if m.DecalTex != nil {
		m.DecalTex.Release()
		m.DecalTex = nil
	}
	if m.DecalFallbackView != nil {
		m.DecalFallbackView.Release()
		m.DecalFallbackView = nil
	}
	if m.DecalFallbackTex != nil {
		m.DecalFallbackTex.Release()
		m.DecalFallbackTex = nil
	}
	if m.DecalBuf != nil {
		m.retireBuffer(m.DecalBuf)
		m.DecalBuf = nil
	}
	m.DecalCount = 0
	m.decalBGCamera, m.decalBGBuf, m.decalBGDepth, m.decalBGNormal, m.decalBGPipeline = nil, nil, nil, nil, nil
}
