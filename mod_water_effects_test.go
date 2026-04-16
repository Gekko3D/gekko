package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
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

func TestWaterBodyResolutionCopiesSplashOverrideToResolvedPatches(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := &WaterBodyResolutionState{}

	textureID := AssetId{UUID: uuid.New()}
	owner := cmd.AddEntity(
		&WaterBodyComponent{
			Mode:            WaterBodyModeExplicitRect,
			SurfaceY:        2,
			Depth:           3,
			RectHalfExtents: [2]float32{2, 2},
		},
		&WaterSplashEffectComponent{
			Texture:        textureID,
			AtlasCols:      2,
			AtlasRows:      3,
			SplashSprite:   7,
			MinImpactSpeed: 3,
		},
		&TransformComponent{Position: mgl32.Vec3{1, 2, 3}},
	)
	app.FlushCommands()

	waterBodyResolutionSystem(cmd, nil, state)
	app.FlushCommands()

	patchIDs := state.PatchEntitiesByOwner[owner]
	if len(patchIDs) != 1 {
		t.Fatalf("expected one resolved patch entity, got %d", len(patchIDs))
	}

	found := false
	MakeQuery1[WaterSplashEffectComponent](cmd).Map(func(eid EntityId, splash *WaterSplashEffectComponent) bool {
		if eid == patchIDs[0] {
			found = true
			if splash.Texture != textureID || splash.AtlasCols != 2 || splash.AtlasRows != 3 || splash.SplashSprite != 7 {
				t.Fatalf("unexpected copied splash override: %+v", *splash)
			}
			return false
		}
		return true
	})
	if !found {
		t.Fatal("expected resolved patch to carry copied splash override")
	}
}
