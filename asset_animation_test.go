package gekko

import (
	"path/filepath"
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
	clips := []content.AssetAnimationClipDef{{
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

	documentPath := attachTestAnimationSet(t, def, clips)
	result, err := SpawnAuthoredAssetWithOptions(cmd, nil, def, TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}, AuthoredAssetSpawnOptions{DocumentPath: documentPath})
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

func TestAuthoredAssetAnimationInterpolatesOneDimensionalBlend(t *testing.T) {
	track := func(x float32) content.AssetAnimationTrackDef {
		return content.AssetAnimationTrackDef{TargetID: "arm", PositionKeys: []content.AssetVec3KeyDef{{Value: content.Vec3{x, 0, 0}}}}
	}
	clip := content.AssetAnimationClipDef{Blend1D: &content.AssetAnimationBlend1DDef{
		Parameter: "pitch", Default: 50,
		Samples: []content.AssetAnimationBlendSampleDef{{Value: 0, Tracks: []content.AssetAnimationTrackDef{track(0)}}, {Value: 100, Tracks: []content.AssetAnimationTrackDef{track(10)}}},
	}}
	bind := map[string]LocalTransformComponent{"arm": {Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}}
	target := bind["arm"]
	targets := map[string]*LocalTransformComponent{"arm": &target}
	applyAnimationClipPose(clip, 0, 25, true, 1, AnimationLayerOverride, nil, AnimationRootMotionApply, nil, bind, targets)
	if got := target.Position.X(); got != 2.5 {
		t.Fatalf("blend at 25 = %g, want 2.5", got)
	}
	applyAnimationClipPose(clip, 0, 0, false, 1, AnimationLayerOverride, nil, AnimationRootMotionApply, nil, bind, targets)
	if got := target.Position.X(); got != 5 {
		t.Fatalf("default blend = %g, want 5", got)
	}
	applyAnimationClipPose(clip, 0, 200, true, 1, AnimationLayerOverride, nil, AnimationRootMotionApply, nil, bind, targets)
	if got := target.Position.X(); got != 10 {
		t.Fatalf("clamped blend = %g, want 10", got)
	}
}

func TestNPCAnimationBlendDoesNotRestartClip(t *testing.T) {
	animation := &NPCAnimationComponent{}
	RequestNPCAnimationClip(animation, "shoot", true)
	requestID := animation.RequestID
	SetNPCAnimationBlend(animation, 25)
	if animation.RequestID != requestID || animation.BlendValue != 25 || !animation.HasBlendValue {
		t.Fatalf("blend update restarted or was lost: %+v", animation)
	}
}

func TestNPCAnimationEventDeliveryCopiesEachEventOnce(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	npc := cmd.AddEntity(&NPCComponent{}, &NPCAnimationComponent{})
	root := cmd.AddEntity(
		&AuthoredAssetRootComponent{},
		&Parent{Entity: npc},
		&AnimationPlayerComponent{CrossedEvents: []AnimationEvent{{ClipID: "shoot", ID: 3}}},
		&AuthoredAssetAnimationSetComponent{},
	)
	app.FlushCommands()
	npcAnimationEventDeliverySystem(cmd)
	animation := npcAnimationForTest(t, cmd, npc)
	player := animationPlayerForTest(t, cmd, root)
	if len(animation.CrossedEvents) != 1 || animation.CrossedEvents[0].ID != 3 || len(player.CrossedEvents) != 0 {
		t.Fatalf("event delivery duplicated or retained event: animation=%+v player=%+v", animation.CrossedEvents, player.CrossedEvents)
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
	clips := []content.AssetAnimationClipDef{{
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

	documentPath := attachTestAnimationSet(t, def, clips)
	result, err := SpawnAuthoredAssetWithOptions(cmd, nil, def, TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}, AuthoredAssetSpawnOptions{DocumentPath: documentPath})
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

func TestAuthoredAssetAnimationOneShotLayerRemovesAtEnd(t *testing.T) {
	bind := map[string]LocalTransformComponent{"upper": {Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}}
	target := bind["upper"]
	set := &AuthoredAssetAnimationSetComponent{BindTransforms: bind, Clips: map[string]content.AssetAnimationClipDef{
		"base":   {ID: "base", Loop: true, Tracks: []content.AssetAnimationTrackDef{{TargetID: "upper", PositionKeys: []content.AssetVec3KeyDef{{Value: content.Vec3{1, 0, 0}}}}}},
		"recoil": {ID: "recoil", Duration: 1, Loop: true, Tracks: []content.AssetAnimationTrackDef{{TargetID: "upper", PositionKeys: []content.AssetVec3KeyDef{{Value: content.Vec3{2, 0, 0}}}}}},
	}}
	player := &AnimationPlayerComponent{ClipID: "base", Layers: []AnimationLayer{{Tag: "weapon_recoil", ClipID: "recoil", Weight: 1, Mode: AnimationLayerOverride, BoneMask: []string{"upper"}, LoopOverride: true}}}
	targets := map[string]*LocalTransformComponent{"upper": &target}
	applyAnimationLayers(player, set, targets, nil, 0.5)
	if len(player.Layers) != 1 || target.Position.X() != 2 {
		t.Fatalf("one-shot recoil should apply before end, layers=%+v position=%v", player.Layers, target.Position)
	}
	applyAnimationLayers(player, set, targets, nil, 0.6)
	if len(player.Layers) != 0 || target.Position.X() != 1 {
		t.Fatalf("finished recoil should be removed and expose base, layers=%+v position=%v", player.Layers, target.Position)
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
	def, documentPath := npcAnimationTestAsset(t, "barney-animation", []content.AssetAnimationClipDef{
		npcAnimationTestClip("mdl_idle1", "idle1", true),
		npcAnimationTestClip("mdl_walk", "walk", true),
	})
	result, err := SpawnAuthoredAssetWithOptions(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}, AuthoredAssetSpawnOptions{DocumentPath: documentPath})
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
	def, documentPath := npcAnimationTestAsset(t, "barney-animation", []content.AssetAnimationClipDef{
		npcAnimationTestClip("mdl_idle1", "idle1", true),
		npcAnimationTestClip("mdl_walk", "walk", true),
	})
	result, err := SpawnAuthoredAssetWithOptions(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}, AuthoredAssetSpawnOptions{DocumentPath: documentPath})
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
	def, documentPath := npcAnimationTestAsset(t, "barney-animation", []content.AssetAnimationClipDef{
		bindPoseIdle,
		decodedIdle,
		npcAnimationTestClip("mdl_walk", "walk", true),
	})
	result, err := SpawnAuthoredAssetWithOptions(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}, AuthoredAssetSpawnOptions{DocumentPath: documentPath})
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

func npcAnimationTestAsset(t *testing.T, id string, clips []content.AssetAnimationClipDef) (*content.AssetDef, string) {
	def := content.NewAssetDef(id)
	def.Parts = []content.AssetPartDef{{
		ID:        "root",
		Name:      "root",
		Source:    content.AssetSourceDef{Kind: content.AssetSourceKindGroup},
		Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
	}}
	return def, attachTestAnimationSet(t, def, clips)
}

func attachTestAnimationSet(t *testing.T, def *content.AssetDef, clips []content.AssetAnimationClipDef) string {
	t.Helper()
	dir := t.TempDir()
	documentPath := filepath.Join(dir, "asset.gkasset")
	setPath := filepath.Join(dir, "asset.gkanim")
	if err := content.SaveAnimationSet(setPath, &content.AnimationSetDef{ID: def.ID + ".animation", SchemaVersion: content.CurrentAnimationSetSchemaVersion, Name: def.Name + " animation", TargetAssetID: def.ID, Clips: clips}); err != nil {
		t.Fatal(err)
	}
	def.AnimationSetPaths = []string{"asset.gkanim"}
	def.DefaultAnimationClipID = clips[0].ID
	return documentPath
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
