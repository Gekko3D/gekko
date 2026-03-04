package app

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

func (a *App) SetParticleAtlas(texels []byte, w, h uint32) {
	if texels == nil || w == 0 || h == 0 {
		return
	}

	tex, err := a.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "Particle Atlas",
		Usage: wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
		Size: wgpu.Extent3D{
			Width:              w,
			Height:             h,
			DepthOrArrayLayers: 1,
		},
		Format:        wgpu.TextureFormatRGBA8Unorm,
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create particle atlas texture: %v\n", err)
		return
	}

	a.Queue.WriteTexture(tex.AsImageCopy(), texels, &wgpu.TextureDataLayout{
		BytesPerRow:  w * 4,
		RowsPerImage: h,
	}, &wgpu.Extent3D{Width: w, Height: h, DepthOrArrayLayers: 1})

	a.BufferManager.ParticleAtlasTex = tex
	a.BufferManager.ParticleAtlasView, _ = tex.CreateView(nil)
	a.BufferManager.ParticleAtlasSampler, _ = a.Device.CreateSampler(&wgpu.SamplerDescriptor{
		MagFilter:     wgpu.FilterModeLinear,
		MinFilter:     wgpu.FilterModeLinear,
		MipmapFilter:  wgpu.MipmapFilterModeLinear,
		AddressModeU:  wgpu.AddressModeClampToEdge,
		AddressModeV:  wgpu.AddressModeClampToEdge,
		LodMinClamp:   0,
		LodMaxClamp:   0,
		MaxAnisotropy: 1,
	})

	// Recreate particle bind groups to include the new texture
	a.BufferManager.CreateParticlesBindGroups(a.ParticlesPipeline)
}
