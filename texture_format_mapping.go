package gekko

import "github.com/cogentcore/webgpu/wgpu"

func assetTextureFormatToWGPU(format TextureFormat) wgpu.TextureFormat {
	switch format {
	case TextureFormatRGBA8Unorm:
		return wgpu.TextureFormatRGBA8Unorm
	case TextureFormatRGBA8UnormSrgb:
		return wgpu.TextureFormatRGBA8UnormSrgb
	default:
		return wgpu.TextureFormatRGBA8Unorm
	}
}
