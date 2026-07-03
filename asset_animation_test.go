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

func TestAuthoredAssetAnimationLayersPreserveMaskedStanceAndLockedRoot(t *testing.T) {
	bind := map[string]LocalTransformComponent{
		"root":  {Position: mgl32.Vec3{1, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		"upper": {Position: mgl32.Vec3{2, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		"lower": {Position: mgl32.Vec3{3, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
	}
	targets := map[string]*LocalTransformComponent{}
	for id, local := range bind {
		copy := local
		targets[id] = &copy
	}
	clip := func(id string, tracks ...content.AssetAnimationTrackDef) content.AssetAnimationClipDef {
		return content.AssetAnimationClipDef{ID: id, Duration: 1, Loop: true, Tracks: tracks}
	}
	track := func(id string, x float32) content.AssetAnimationTrackDef {
		return content.AssetAnimationTrackDef{TargetID: id, PositionKeys: []content.AssetVec3KeyDef{{Time: 0, Value: content.Vec3{x, 0, 0}}}}
	}
	set := &AuthoredAssetAnimationSetComponent{BindTransforms: bind, Clips: map[string]content.AssetAnimationClipDef{
		"stance": clip("stance", track("root", 5), track("upper", 20), track("lower", 30)),
		"gait":   clip("gait", track("root", 9), track("upper", 99), track("lower", 40)),
		"kick":   clip("kick", track("lower", 2)),
	}}
	player := &AnimationPlayerComponent{
		ClipID:           "stance",
		RootMotionPolicy: AnimationRootMotionLocked,
		Layers: []AnimationLayer{
			{ClipID: "gait", Weight: 1, Mode: AnimationLayerOverride, BoneMask: []string{"root", "lower"}, RootMotionPolicy: AnimationRootMotionLocked},
			{ClipID: "kick", Weight: 1, Mode: AnimationLayerAdditive, BoneMask: []string{"lower"}, RootMotionPolicy: AnimationRootMotionLocked},
		},
	}
	applyAnimationLayers(player, set, targets, map[string]struct{}{"root": {}}, 0)
	if got := targets["root"].Position.X(); got != 1 {
		t.Fatalf("locked root position = %f, want bind 1", got)
	}
	if got := targets["upper"].Position.X(); got != 20 {
		t.Fatalf("upper stance position = %f, want base 20", got)
	}
	if got := targets["lower"].Position.X(); got != 39 {
		t.Fatalf("ordered override/additive lower position = %f, want 39", got)
	}
}

func TestNPCAnimationSystemSelectsSemanticClip(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	npc := cmd.AddEntity(
		&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&NPCComponent{ClassName: "monster_barney"},
		&NPCAnimationComponent{State: NPCAnimationStateWalk},
	)
	def := npcAnimationTestAsset("barney-animation", []content.AssetAnimationClipDef{
		npcAnimationTestClip("mdl_idle1", "idle1", true),
		npcAnimationTestClip("mdl_walk", "walk", true),
	})
	result, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset failed: %v", err)
	}
	cmd.AddComponents(result.RootEntity, &Parent{Entity: npc})
	app.FlushCommands()

	npcAnimationSystem(cmd)

	player := animationPlayerForTest(t, cmd, result.RootEntity)
	if player.ClipID != "mdl_walk" || !player.Playing || !player.Loop {
		t.Fatalf("expected walk clip to play, got %+v", player)
	}
	anim := npcAnimationForTest(t, cmd, npc)
	if anim.ActiveClipID != "mdl_walk" {
		t.Fatalf("expected active NPC clip to be recorded, got %+v", anim)
	}
}

func TestNPCAnimationSystemFallsBackWhenSemanticClipMissing(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	npc := cmd.AddEntity(
		&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&NPCComponent{ClassName: "monster_barney"},
		&NPCAnimationComponent{State: NPCAnimationStateAttack, FallbackClipID: "mdl_idle1"},
	)
	def := npcAnimationTestAsset("barney-animation", []content.AssetAnimationClipDef{
		npcAnimationTestClip("mdl_idle1", "idle1", true),
		npcAnimationTestClip("mdl_walk", "walk", true),
	})
	result, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset failed: %v", err)
	}
	cmd.AddComponents(result.RootEntity, &Parent{Entity: npc})
	app.FlushCommands()

	npcAnimationSystem(cmd)

	player := animationPlayerForTest(t, cmd, result.RootEntity)
	if player.ClipID != "mdl_idle1" {
		t.Fatalf("expected fallback idle clip, got %+v", player)
	}
}

func TestNPCAnimationSystemPrefersDecodedIdleOverBindPoseSubstring(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	npc := cmd.AddEntity(
		&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&NPCComponent{ClassName: "monster_barney"},
		&NPCAnimationComponent{State: NPCAnimationStateIdle},
	)
	bindPoseIdle := npcAnimationTestClip("mdl_almostidle", "almostidle", true)
	bindPoseIdle.Tags = []string{"source:hl1", "source_asset:mdl", "generated:bind_pose_clip"}
	decodedIdle := npcAnimationTestClip("mdl_idle1", "idle1", true)
	decodedIdle.Tags = []string{"source:hl1", "source_asset:mdl", "generated:sequence_clip"}
	def := npcAnimationTestAsset("barney-animation", []content.AssetAnimationClipDef{
		bindPoseIdle,
		decodedIdle,
		npcAnimationTestClip("mdl_walk", "walk", true),
	})
	result, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset failed: %v", err)
	}
	cmd.AddComponents(result.RootEntity, &Parent{Entity: npc})
	app.FlushCommands()

	npcAnimationSystem(cmd)

	player := animationPlayerForTest(t, cmd, result.RootEntity)
	if player.ClipID != "mdl_idle1" {
		t.Fatalf("expected decoded idle clip, got %+v", player)
	}
}

func npcAnimationTestAsset(id string, clips []content.AssetAnimationClipDef) *content.AssetDef {
	def := content.NewAssetDef(id)
	def.Parts = []content.AssetPartDef{{
		ID:        "root",
		Name:      "root",
		Source:    content.AssetSourceDef{Kind: content.AssetSourceKindGroup},
		Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
	}}
	def.AnimationClips = clips
	return def
}

func npcAnimationTestClip(id string, name string, loop bool) content.AssetAnimationClipDef {
	return content.AssetAnimationClipDef{
		ID:       id,
		Name:     name,
		FPS:      30,
		Duration: 1,
		Loop:     loop,
		Tracks: []content.AssetAnimationTrackDef{{
			TargetID: "root",
			RotationKeys: []content.AssetQuatKeyDef{
				{Time: 0, Value: content.Quat{0, 0, 0, 1}},
				{Time: 1, Value: content.Quat{0, 0, 0, 1}},
			},
		}},
	}
}

func animationPlayerForTest(t *testing.T, cmd *Commands, eid EntityId) *AnimationPlayerComponent {
	t.Helper()
	for _, comp := range cmd.GetAllComponents(eid) {
		if player, ok := comp.(*AnimationPlayerComponent); ok {
			return player
		}
	}
	t.Fatalf("missing AnimationPlayerComponent for entity %d", eid)
	return nil
}

func npcAnimationForTest(t *testing.T, cmd *Commands, eid EntityId) *NPCAnimationComponent {
	t.Helper()
	for _, comp := range cmd.GetAllComponents(eid) {
		if anim, ok := comp.(*NPCAnimationComponent); ok {
			return anim
		}
	}
	t.Fatalf("missing NPCAnimationComponent for entity %d", eid)
	return nil
}
