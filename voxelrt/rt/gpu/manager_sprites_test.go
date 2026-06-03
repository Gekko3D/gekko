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
