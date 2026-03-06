package gpu

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

// UpdateSprites uploads sprite instance data to a GPU buffer.
func (m *GpuBufferManager) UpdateSprites(data []byte, count uint32) bool {
	m.SpriteCount = count
	if count == 0 {
		return false
	}

	recreated := m.ensureBuffer("SpriteBuf", &m.SpriteBuf, data, wgpu.BufferUsageStorage, 0)
	return recreated
}

// CreateSpritesBindGroups creates the bind groups for the sprite rendering pipeline.
func (m *GpuBufferManager) CreateSpritesBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil {
		return
	}

	if m.SpriteAtlasView == nil {
		m.CreateDefaultSpriteAtlas()
	}

	var err error
	// Group 0: Camera + Sprites + Atlas
	m.SpritesBindGroup0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "Sprites BindGroup 0",
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.SpriteBuf, Size: wgpu.WholeSize},
			{Binding: 2, TextureView: m.SpriteAtlasView},
			{Binding: 3, Sampler: m.SpriteAtlasSampler},
		},
	})
	if err != nil {
		panic(fmt.Errorf("failed to create sprites bind group 0: %v", err))
	}

	// Group 1: G-Buffer Depth
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
}

func (m *GpuBufferManager) CreateDefaultSpriteAtlas() {
	// Reuse particle atlas logic or create a simple 1x1 white texture
	width, height := uint32(1), uint32(1)
	pixels := []byte{255, 255, 255, 255}
	m.SetSpriteAtlas(pixels, width, height)
}

func (m *GpuBufferManager) SetSpriteAtlas(data []byte, w, h uint32) {
	if m.SpriteAtlasTex != nil {
		m.SpriteAtlasTex.Release()
	}
	if m.SpriteAtlasView != nil {
		m.SpriteAtlasView.Release()
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
	m.SpriteAtlasTex = tex
	m.SpriteAtlasView, _ = tex.CreateView(nil)

	if m.SpriteAtlasSampler == nil {
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
	m.SpriteAtlasDirty = true
}
