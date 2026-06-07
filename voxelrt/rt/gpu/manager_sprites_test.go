package gpu

import (
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
)

func TestSpriteBatchDescsFromRenderBatchesPreservesActiveBatchDescriptors(t *testing.T) {
	batches := []SpriteRenderBatch{
		{
			AtlasKey:      "hud",
			FirstInstance: 3,
			InstanceCount: 2,
			BindGroup0:    &wgpu.BindGroup{},
		},
		{
			AtlasKey:      "empty",
			FirstInstance: 5,
			InstanceCount: 0,
			BindGroup0:    &wgpu.BindGroup{},
		},
		{
			AtlasKey:      "reticle",
			FirstInstance: 8,
			InstanceCount: 4,
			BindGroup0:    &wgpu.BindGroup{},
		},
	}

	descs := spriteBatchDescsFromRenderBatches(batches)
	if len(descs) != 2 {
		t.Fatalf("expected two active sprite descriptors, got %d", len(descs))
	}
	if descs[0] != (SpriteBatchDesc{AtlasKey: "hud", FirstInstance: 3, InstanceCount: 2}) {
		t.Fatalf("unexpected first descriptor: %+v", descs[0])
	}
	if descs[1] != (SpriteBatchDesc{AtlasKey: "reticle", FirstInstance: 8, InstanceCount: 4}) {
		t.Fatalf("unexpected second descriptor: %+v", descs[1])
	}
}

func TestSpriteAtlasUploadMipLevelCountKeepsBackendSafeSingleMip(t *testing.T) {
	if got := spriteAtlasUploadMipLevelCount(64, 64); got != 1 {
		t.Fatalf("expected live sprite atlas upload to stay single-mip, got %d", got)
	}
}

func TestSpriteAtlasMipChainUsesCeilHalfDimensions(t *testing.T) {
	data := make([]byte, 3*5*4)
	for i := 3; i < len(data); i += 4 {
		data[i] = 255
	}

	mips := buildSpriteAtlasMipChainRGBA8(data, 3, 5)
	if len(mips) != 4 {
		t.Fatalf("expected four mip levels, got %d", len(mips))
	}

	want := [][2]uint32{{3, 5}, {2, 3}, {1, 2}, {1, 1}}
	for i, dims := range want {
		if mips[i].Width != dims[0] || mips[i].Height != dims[1] {
			t.Fatalf("mip %d dimensions = %dx%d, want %dx%d", i, mips[i].Width, mips[i].Height, dims[0], dims[1])
		}
	}
	if got := spriteAtlasMipLevelCount(3, 5); got != uint32(len(want)) {
		t.Fatalf("mip count = %d, want %d", got, len(want))
	}
}

func TestSpriteAtlasMipChainAlphaWeightsRGB(t *testing.T) {
	data := []byte{
		255, 0, 0, 255,
		0, 255, 0, 0,
		0, 0, 255, 0,
		255, 255, 255, 0,
	}

	mips := buildSpriteAtlasMipChainRGBA8(data, 2, 2)
	if len(mips) != 2 {
		t.Fatalf("expected two mip levels, got %d", len(mips))
	}

	got := mips[1].Data
	want := []byte{255, 0, 0, 64}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mip texel = %v, want %v", got[:4], want)
		}
	}
}
