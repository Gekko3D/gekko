package app

import (
	"testing"
	"unsafe"
)

func TestParticleEmitterBytesPacksRendererInput(t *testing.T) {
	emitters := []ParticleEmitterInput{
		{
			Pos:         [3]float32{1, 2, 3},
			SpawnCount:  4,
			Rot:         [4]float32{0, 0, 0, 1},
			LifeMin:     0.5,
			LifeMax:     2,
			SpeedMin:    3,
			SpeedMax:    4,
			SizeMin:     0.1,
			SizeMax:     0.2,
			Gravity:     9.8,
			Drag:        0.5,
			ColorMin:    [4]float32{0.1, 0.2, 0.3, 0.4},
			ColorMax:    [4]float32{0.5, 0.6, 0.7, 0.8},
			ConeAngle:   35,
			SpriteIndex: 6,
			AtlasCols:   7,
			AtlasRows:   8,
			AlphaMode:   2,
		},
	}

	bytes, count := particleEmitterBytes(emitters)

	if count != 1 {
		t.Fatalf("emitter count = %d, want 1", count)
	}
	if got, want := len(bytes), int(unsafe.Sizeof(ParticleEmitterInput{})); got != want {
		t.Fatalf("emitter byte length = %d, want %d", got, want)
	}
	emptyBytes, emptyCount := particleEmitterBytes(nil)
	if len(emptyBytes) != 0 || emptyCount != 0 {
		t.Fatalf("expected empty emitter byte output, got bytes=%d count=%d", len(emptyBytes), emptyCount)
	}
}

func TestClearParticleInputClearsSpawnCount(t *testing.T) {
	app := &App{}
	app.SetParticleSpawnCount(3)

	app.ClearParticleInput()

	if got := app.particleSpawnCount(); got != 0 {
		t.Fatalf("expected particle spawn count cleared, got %d", got)
	}
}
