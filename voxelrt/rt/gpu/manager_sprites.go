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

func spriteBatchDescsFromRenderBatches(batches []SpriteRenderBatch) []SpriteBatchDesc {
	if len(batches) == 0 {
		return nil
	}
	descs := make([]SpriteBatchDesc, 0, len(batches))
	for _, batch := range batches {
		if batch.InstanceCount == 0 {
			continue
		}
		descs = append(descs, SpriteBatchDesc{
			AtlasKey:      batch.AtlasKey,
			FirstInstance: batch.FirstInstance,
			InstanceCount: batch.InstanceCount,
		})
	}
	return descs
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

func (m *GpuBufferManager) RebuildSpriteBindGroups(pipeline *wgpu.RenderPipeline) {
	if m == nil {
		return
	}
	m.SyncSpriteBatches(pipeline, spriteBatchDescsFromRenderBatches(m.SpriteBatches))
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
	m.SetSpriteAtlas(defaultSpriteAtlasKey, pixels, width, height, 1, wgpu.TextureFormatRGBA8UnormSrgb)
}

func (m *GpuBufferManager) SetSpriteAtlas(key string, data []byte, w, h uint32, version uint, format wgpu.TextureFormat) {
	if format == 0 {
		format = wgpu.TextureFormatRGBA8UnormSrgb
	}
	mipLevels := spriteAtlasUploadMipLevelCount(w, h)
	if existing, ok := m.SpriteAtlases[key]; ok &&
		existing != nil &&
		existing.Version == version &&
		existing.Format == format &&
		existing.Width == w &&
		existing.Height == h &&
		existing.MipLevels == mipLevels {
		return
	}
	required := int(w) * int(h) * 4
	if w == 0 || h == 0 {
		panic("sprite atlas dimensions must be non-zero")
	}
	if len(data) < required {
		panic(fmt.Errorf("sprite atlas data too short: got %d bytes, need %d", len(data), required))
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
		MipLevelCount: mipLevels,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        format,
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
		&wgpu.ImageCopyTexture{
			Texture:  tex,
			MipLevel: 0,
			Origin:   wgpu.Origin3D{X: 0, Y: 0, Z: 0},
		},
		data[:required],
		&wgpu.TextureDataLayout{
			Offset:       0,
			BytesPerRow:  4 * w,
			RowsPerImage: h,
		},
		&size,
	)
	m.SpriteAtlases[key] = &SpriteAtlasResource{
		Texture:   tex,
		View:      view,
		Version:   version,
		Format:    format,
		Width:     w,
		Height:    h,
		MipLevels: mipLevels,
	}
}

func spriteAtlasUploadMipLevelCount(w, h uint32) uint32 {
	// Keep live uploads at one mip level for now. The current native backend
	// reports a validation-invalid texture when sprite atlases are created with
	// a CPU-supplied mip chain, so the alpha-aware helpers below stay tested but
	// offline until the backend upload path is verified in a windowed smoke run.
	return 1
}

type spriteAtlasMipLevel struct {
	Data   []byte
	Width  uint32
	Height uint32
}

func spriteAtlasMipLevelCount(w, h uint32) uint32 {
	if w == 0 || h == 0 {
		return 1
	}
	levels := uint32(1)
	for w > 1 || h > 1 {
		w = max(uint32(1), (w+1)/2)
		h = max(uint32(1), (h+1)/2)
		levels++
	}
	return levels
}

func buildSpriteAtlasMipChainRGBA8(data []byte, w, h uint32) []spriteAtlasMipLevel {
	if w == 0 || h == 0 {
		panic("sprite atlas dimensions must be non-zero")
	}
	required := int(w) * int(h) * 4
	if len(data) < required {
		panic(fmt.Errorf("sprite atlas data too short: got %d bytes, need %d", len(data), required))
	}

	base := make([]byte, required)
	copy(base, data[:required])
	mips := []spriteAtlasMipLevel{{Data: base, Width: w, Height: h}}
	for w > 1 || h > 1 {
		prev := mips[len(mips)-1]
		w = max(uint32(1), (prev.Width+1)/2)
		h = max(uint32(1), (prev.Height+1)/2)
		next := downsampleSpriteMipRGBA8(prev.Data, prev.Width, prev.Height, w, h)
		mips = append(mips, spriteAtlasMipLevel{Data: next, Width: w, Height: h})
	}
	return mips
}

func downsampleSpriteMipRGBA8(prev []byte, prevW, prevH, nextW, nextH uint32) []byte {
	next := make([]byte, int(nextW)*int(nextH)*4)
	for y := uint32(0); y < nextH; y++ {
		for x := uint32(0); x < nextW; x++ {
			srcX0 := x * 2
			srcY0 := y * 2
			srcX1 := min(srcX0+2, prevW)
			srcY1 := min(srcY0+2, prevH)

			var sumR, sumG, sumB, sumA uint32
			var weightedR, weightedG, weightedB uint32
			var count uint32
			for sy := srcY0; sy < srcY1; sy++ {
				for sx := srcX0; sx < srcX1; sx++ {
					idx := (int(sy)*int(prevW) + int(sx)) * 4
					r := uint32(prev[idx+0])
					g := uint32(prev[idx+1])
					b := uint32(prev[idx+2])
					a := uint32(prev[idx+3])
					sumR += r
					sumG += g
					sumB += b
					sumA += a
					weightedR += r * a
					weightedG += g * a
					weightedB += b * a
					count++
				}
			}

			dst := (int(y)*int(nextW) + int(x)) * 4
			if sumA > 0 {
				next[dst+0] = uint8((weightedR + sumA/2) / sumA)
				next[dst+1] = uint8((weightedG + sumA/2) / sumA)
				next[dst+2] = uint8((weightedB + sumA/2) / sumA)
			} else {
				next[dst+0] = uint8((sumR + count/2) / count)
				next[dst+1] = uint8((sumG + count/2) / count)
				next[dst+2] = uint8((sumB + count/2) / count)
			}
			next[dst+3] = uint8((sumA + count/2) / count)
		}
	}
	return next
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
		LodMaxClamp:   0,
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
