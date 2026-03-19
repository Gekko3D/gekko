package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func TestSpawnAuthoredAssetResolvesChildBeforeParent(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	def := content.NewAssetDef("ordered")
	parent := content.AssetPartDef{
		ID:   "parent",
		Name: "parent",
		Transform: content.AssetTransformDef{
			Position: content.Vec3{5, 0, 0},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}
	child := content.AssetPartDef{
		ID:       "child",
		Name:     "child",
		ParentID: "parent",
		Transform: content.AssetTransformDef{
			Position: content.Vec3{0, 2, 0},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}
	def.Parts = []content.AssetPartDef{child, parent}

	result, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{
		Position: mgl32.Vec3{0, 0, 0},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset returned error: %v", err)
	}
	app.FlushCommands()
	childEntity := result.EntitiesByAssetID["child"]
	if childEntity == 0 {
		t.Fatal("expected child entity mapping")
	}

	var childWorld *TransformComponent
	for _, comp := range cmd.GetAllComponents(childEntity) {
		if tr, ok := comp.(TransformComponent); ok {
			childWorld = &tr
		}
	}
	if childWorld == nil {
		t.Fatal("expected child world transform")
	}
	if got := childWorld.Position; got.Sub(mgl32.Vec3{5, 2, 0}).Len() > 1e-5 {
		t.Fatalf("expected child world position [5 2 0], got %v", got)
	}
}

func TestSpawnAuthoredAssetRejectsMissingParent(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	def := content.NewAssetDef("invalid")
	def.Parts = []content.AssetPartDef{{
		ID:       "child",
		Name:     "child",
		ParentID: "missing",
		Transform: content.AssetTransformDef{
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}}

	if _, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}); err == nil {
		t.Fatal("expected missing parent to be rejected")
	}
}

func TestSpawnAuthoredAssetRejectsCycle(t *testing.T) {
	def := content.NewAssetDef("cycle")
	def.Parts = []content.AssetPartDef{
		{ID: "a", Name: "a", ParentID: "b"},
		{ID: "b", Name: "b", ParentID: "a"},
	}
	if err := ValidateAssetHierarchy(def); err == nil {
		t.Fatal("expected cycle to be rejected")
	}
}

func TestSpawnAuthoredAssetSpawnsPartLightAndEmitterHierarchy(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	def := content.NewAssetDef("mixed")
	def.Parts = []content.AssetPartDef{{
		ID:   "root-part",
		Name: "root-part",
		Transform: content.AssetTransformDef{
			Position: content.Vec3{1, 0, 0},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}}
	def.Lights = []content.AssetLightDef{{
		ID:       "light",
		Name:     "light",
		ParentID: "root-part",
		Transform: content.AssetTransformDef{
			Position: content.Vec3{0, 3, 0},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Type: content.AssetLightTypePoint,
	}}
	def.Emitters = []content.AssetEmitterDef{{
		ID:       "emitter",
		Name:     "emitter",
		ParentID: "root-part",
		Transform: content.AssetTransformDef{
			Position: content.Vec3{0, 0, 2},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Emitter: content.EmitterDef{Enabled: true, AlphaMode: content.AssetAlphaModeTexture},
	}}

	result, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset returned error: %v", err)
	}
	app.FlushCommands()
	if _, ok := result.PartIDs["root-part"]; !ok {
		t.Fatal("expected part id map to include root-part")
	}
	if result.EntitiesByAssetID["light"] == 0 || result.EntitiesByAssetID["emitter"] == 0 {
		t.Fatal("expected light and emitter entity mappings")
	}
}
