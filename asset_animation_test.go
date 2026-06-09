package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func TestAuthoredAssetAnimationInterpolatesLocalTransform(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	def := content.NewAssetDef("animated")
	def.Parts = []content.AssetPartDef{{
		ID:        "arm",
		Name:      "arm",
		Source:    content.AssetSourceDef{Kind: content.AssetSourceKindGroup},
		Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
	}}
	def.AnimationClips = []content.AssetAnimationClipDef{{
		ID:       "move",
		Name:     "move",
		Duration: 1,
		Loop:     true,
		Tracks: []content.AssetAnimationTrackDef{{
			TargetID: "arm",
			PositionKeys: []content.AssetVec3KeyDef{
				{Time: 0, Value: content.Vec3{0, 0, 0}},
				{Time: 1, Value: content.Vec3{10, 0, 0}},
			},
		}},
	}}

	result, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset failed: %v", err)
	}

	assetAnimationSystem(&Time{Dt: 0.5}, cmd)
	local, ok := localTransformForAnimationBind(cmd, result.EntitiesByAssetID["arm"])
	if !ok {
		t.Fatal("expected animated part to have a local transform")
	}
	if !local.Position.ApproxEqualThreshold(mgl32.Vec3{5, 0, 0}, 1e-4) {
		t.Fatalf("expected interpolated local position {5 0 0}, got %v", local.Position)
	}
}

func TestAuthoredAssetAnimationKeepsBindChannelsWhenTrackOmitsThem(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	def := content.NewAssetDef("animated")
	def.Parts = []content.AssetPartDef{{
		ID:     "head",
		Name:   "head",
		Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup},
		Transform: content.AssetTransformDef{
			Position: content.Vec3{1, 2, 3},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{2, 2, 2},
		},
	}}
	def.AnimationClips = []content.AssetAnimationClipDef{{
		ID:       "turn",
		Name:     "turn",
		Duration: 1,
		Tracks: []content.AssetAnimationTrackDef{{
			TargetID: "head",
			RotationKeys: []content.AssetQuatKeyDef{
				{Time: 0, Value: content.Quat{0, 0, 0, 1}},
				{Time: 1, Value: content.Quat{0, 0, 0, 1}},
			},
		}},
	}}

	result, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset failed: %v", err)
	}

	assetAnimationSystem(&Time{Dt: 0.5}, cmd)
	local, ok := localTransformForAnimationBind(cmd, result.EntitiesByAssetID["head"])
	if !ok {
		t.Fatal("expected animated part to have a local transform")
	}
	if !local.Position.ApproxEqualThreshold(mgl32.Vec3{1, 2, 3}, 1e-4) {
		t.Fatalf("expected bind position to remain, got %v", local.Position)
	}
	if !local.Scale.ApproxEqualThreshold(mgl32.Vec3{2, 2, 2}, 1e-4) {
		t.Fatalf("expected bind scale to remain, got %v", local.Scale)
	}
}
