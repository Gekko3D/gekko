package gekko

import (
	"testing"

	"github.com/google/uuid"
)

func TestWaterSplashEffectsSystemSpawnsEmittersFromImpactEvents(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	interactions := &WaterInteractionState{
		impactBuffer: []WaterImpactEvent{
			{WaterEntity: 1, Speed: 4.5, Strength: 0.8},
		},
	}
	effects := &WaterEffectsState{}

	waterSplashEffectsSystem(cmd, interactions, effects)
	app.FlushCommands()

	emitterCount := 0
	MakeQuery1[ParticleEmitterComponent](cmd).Map(func(eid EntityId, emitter *ParticleEmitterComponent) bool {
		emitterCount++
		if emitter.Texture != (AssetId{}) {
			t.Fatalf("expected default splash to use default particle atlas, got texture %v", emitter.Texture)
		}
		return true
	})
	if emitterCount != 2 {
		t.Fatalf("expected two splash emitters, got %d", emitterCount)
	}

	spriteCount := 0
	MakeQuery1[SpriteComponent](cmd).Map(func(eid EntityId, sprite *SpriteComponent) bool {
		spriteCount++
		return true
	})
	if spriteCount != 1 {
		t.Fatalf("expected one splash flash sprite, got %d", spriteCount)
	}
}

func TestWaterSplashEffectsSystemUsesPerWaterOverride(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	textureID := AssetId{UUID: uuid.New()}
	water := cmd.AddEntity(&WaterSplashEffectComponent{
		Texture:        textureID,
		AtlasCols:      2,
		AtlasRows:      3,
		MinImpactSpeed: 3.0,
	})
	app.FlushCommands()

	interactions := &WaterInteractionState{
		impactBuffer: []WaterImpactEvent{
			{WaterEntity: water, Speed: 4.5, Strength: 0.8},
		},
	}
	effects := &WaterEffectsState{}

	waterSplashEffectsSystem(cmd, interactions, effects)
	app.FlushCommands()

	foundOverride := false
	MakeQuery1[ParticleEmitterComponent](cmd).Map(func(eid EntityId, emitter *ParticleEmitterComponent) bool {
		if emitter.Texture == textureID {
			foundOverride = true
			if emitter.AtlasCols != 2 || emitter.AtlasRows != 3 {
				t.Fatalf("expected override atlas grid 2x3, got %dx%d", emitter.AtlasCols, emitter.AtlasRows)
			}
		}
		return true
	})
	if !foundOverride {
		t.Fatal("expected at least one emitter to use per-water splash override")
	}
}
