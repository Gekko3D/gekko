package app

import (
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

func TestSpriteInstanceBytesPacksRendererInput(t *testing.T) {
	instances := []SpriteInstanceInput{
		{
			Pos:           [3]float32{1, 2, 3},
			IsUI:          1,
			Size:          [2]float32{4, 5},
			IsUnlit:       1,
			AlphaMode:     2,
			Color:         [4]float32{0.1, 0.2, 0.3, 0.4},
			SpriteIndex:   6,
			AtlasCols:     7,
			AtlasRows:     8,
			BillboardMode: 9,
		},
	}

	bytes, count := spriteInstanceBytes(instances)

	if count != 1 {
		t.Fatalf("sprite count = %d, want 1", count)
	}
	if got, want := len(bytes), int(unsafe.Sizeof(SpriteInstanceInput{})); got != want {
		t.Fatalf("sprite byte length = %d, want %d", got, want)
	}
	emptyBytes, emptyCount := spriteInstanceBytes(nil)
	if len(emptyBytes) != 0 || emptyCount != 0 {
		t.Fatalf("expected empty sprite byte output, got bytes=%d count=%d", len(emptyBytes), emptyCount)
	}
}

func TestSpriteBatchDescsMapRendererInput(t *testing.T) {
	descs := spriteBatchDescs([]SpriteBatchInput{
		{AtlasKey: "hud", FirstInstance: 3, InstanceCount: 2},
	})

	if len(descs) != 1 {
		t.Fatalf("expected one sprite batch desc, got %d", len(descs))
	}
	if descs[0] != (gpu.SpriteBatchDesc{AtlasKey: "hud", FirstInstance: 3, InstanceCount: 2}) {
		t.Fatalf("unexpected sprite batch desc: %+v", descs[0])
	}
}

func TestClearSpriteInputClearsBufferManagerSpriteState(t *testing.T) {
	app := &App{
		BufferManager: &gpu.GpuBufferManager{
			SpriteCount: 2,
			SpriteBatches: []gpu.SpriteRenderBatch{
				{FirstInstance: 0, InstanceCount: 1},
			},
		},
	}

	app.ClearSpriteInput()

	if app.BufferManager.SpriteCount != 0 {
		t.Fatalf("expected sprite count cleared, got %d", app.BufferManager.SpriteCount)
	}
	if len(app.BufferManager.SpriteBatches) != 0 {
		t.Fatalf("expected sprite batches cleared, got %d", len(app.BufferManager.SpriteBatches))
	}
}
