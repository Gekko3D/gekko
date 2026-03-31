package gpu

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

const defaultSpriteAtlasKey = ""

type SpriteBatchDesc struct {
	AtlasKey      string
	FirstInstance uint32
	InstanceCount uint32
}

// UpdateSprites uploads sprite instance data to a GPU buffer.
func (m *GpuBufferManager) UpdateSprites(data []byte, count uint32) bool {
	m.SpriteCount = count
	if count == 0 {
		return false
	}

	recreated := m.ensureBuffer("SpriteBuf", &m.SpriteBuf, data, wgpu.BufferUsageStorage, 0)
	return recreated
}

// SyncSpriteBatches refreshes per-atlas bind groups for the current sprite list.
func (m *GpuBufferManager) SyncSpriteBatches(pipeline *wgpu.RenderPipeline, batches []SpriteBatchDesc) {
	if pipeline == nil || m.SpriteCount == 0 || len(batches) == 0 {
		for i := range m.SpriteBatches {
			if m.SpriteBatches[i].BindGroup0 != nil {
				m.SpriteBatches[i].BindGroup0.Release()
			}
		}
		m.SpriteBatches = m.SpriteBatches[:0]
		return
	}

	if _, ok := m.SpriteAtlases[defaultSpriteAtlasKey]; !ok {
		m.CreateDefaultSpriteAtlas()
	}
	m.ensureSpriteAtlasSampler()
	m.ensureSpritesDepthBindGroup(pipeline)

	oldBatches := m.SpriteBatches
	newBatches := make([]SpriteRenderBatch, 0, len(batches))
	for i, batch := range batches {
		atlasView := m.spriteAtlasView(batch.AtlasKey)
		var existing SpriteRenderBatch
		if i < len(oldBatches) {
			existing = oldBatches[i]
		}
		if existing.BindGroup0 != nil &&
			existing.AtlasKey == batch.AtlasKey &&
			existing.FirstInstance == batch.FirstInstance &&
			existing.InstanceCount == batch.InstanceCount &&
			existing.AtlasView == atlasView &&
			existing.SpriteBuf == m.SpriteBuf &&
			existing.CameraBuf == m.CameraBuf &&
			existing.Sampler == m.SpriteAtlasSampler &&
			existing.Pipeline == pipeline {
			newBatches = append(newBatches, existing)
			continue
		}
		if existing.BindGroup0 != nil {
			existing.BindGroup0.Release()
		}
		bindGroup0, err := m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Label:  "Sprites BindGroup 0",
			Layout: pipeline.GetBindGroupLayout(0),
			Entries: []wgpu.BindGroupEntry{
				{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
				{Binding: 1, Buffer: m.SpriteBuf, Size: wgpu.WholeSize},
				{Binding: 2, TextureView: atlasView},
				{Binding: 3, Sampler: m.SpriteAtlasSampler},
			},
		})
		if err != nil {
			panic(fmt.Errorf("failed to create sprites bind group 0: %v", err))
		}
		newBatches = append(newBatches, SpriteRenderBatch{
			FirstInstance: batch.FirstInstance,
			InstanceCount: batch.InstanceCount,
			AtlasKey:      batch.AtlasKey,
			AtlasView:     atlasView,
			SpriteBuf:     m.SpriteBuf,
			CameraBuf:     m.CameraBuf,
			Sampler:       m.SpriteAtlasSampler,
			Pipeline:      pipeline,
			BindGroup0:    bindGroup0,
		})
	}

	for i := len(batches); i < len(oldBatches); i++ {
		if oldBatches[i].BindGroup0 != nil {
			oldBatches[i].BindGroup0.Release()
		}
	}
	m.SpriteBatches = newBatches
}

func (m *GpuBufferManager) ensureSpritesDepthBindGroup(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil {
		return
	}
	if m.SpritesBindGroup1 != nil && m.spritesBG1Pipeline == pipeline && m.spritesBG1Depth == m.DepthView {
		return
	}
	if m.SpritesBindGroup1 != nil {
		m.SpritesBindGroup1.Release()
		m.SpritesBindGroup1 = nil
	}

	var err error
	m.SpritesBindGroup1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "Sprites BindGroup 1",
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
		},
	})
	if err != nil {
		panic(fmt.Errorf("failed to create sprites bind group 1: %v", err))
	}
	m.spritesBG1Pipeline = pipeline
	m.spritesBG1Depth = m.DepthView
}

func (m *GpuBufferManager) CreateDefaultSpriteAtlas() {
	width, height := uint32(1), uint32(1)
	pixels := []byte{255, 255, 255, 255}
	m.SetSpriteAtlas(defaultSpriteAtlasKey, pixels, width, height, 1)
}

func (m *GpuBufferManager) SetSpriteAtlas(key string, data []byte, w, h uint32, version uint) {
	if version != 0 {
		if existing, ok := m.SpriteAtlases[key]; ok && existing != nil && existing.Version == version {
			return
		}
	}
	if existing, ok := m.SpriteAtlases[key]; ok && existing != nil {
		if existing.View != nil {
			existing.View.Release()
		}
		if existing.Texture != nil {
			existing.Texture.Release()
		}
	}

	size := wgpu.Extent3D{Width: w, Height: h, DepthOrArrayLayers: 1}
	tex, err := m.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Sprite Atlas",
		Size:          size,
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatRGBA8Unorm,
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}
	view, err := tex.CreateView(nil)
	if err != nil {
		tex.Release()
		panic(err)
	}

	m.ensureSpriteAtlasSampler()
	m.Device.GetQueue().WriteTexture(
		&wgpu.ImageCopyTexture{Texture: tex},
		data,
		&wgpu.TextureDataLayout{
			Offset:       0,
			BytesPerRow:  4 * w,
			RowsPerImage: h,
		},
		&size,
	)
	m.SpriteAtlases[key] = &SpriteAtlasResource{Texture: tex, View: view, Version: version}
}

func (m *GpuBufferManager) ensureSpriteAtlasSampler() {
	if m.SpriteAtlasSampler != nil {
		return
	}
	m.SpriteAtlasSampler, _ = m.Device.CreateSampler(&wgpu.SamplerDescriptor{
		AddressModeU:  wgpu.AddressModeClampToEdge,
		AddressModeV:  wgpu.AddressModeClampToEdge,
		AddressModeW:  wgpu.AddressModeClampToEdge,
		MagFilter:     wgpu.FilterModeLinear,
		MinFilter:     wgpu.FilterModeLinear,
		MipmapFilter:  wgpu.MipmapFilterModeLinear,
		LodMinClamp:   0,
		LodMaxClamp:   32,
		MaxAnisotropy: 1,
	})
}

func (m *GpuBufferManager) spriteAtlasView(key string) *wgpu.TextureView {
	if entry, ok := m.SpriteAtlases[key]; ok && entry != nil && entry.View != nil {
		return entry.View
	}
	if entry, ok := m.SpriteAtlases[defaultSpriteAtlasKey]; ok && entry != nil {
		return entry.View
	}
	return nil
}
