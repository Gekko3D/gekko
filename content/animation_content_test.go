package content

import (
	"path/filepath"
	"testing"
)

func TestResolveAssetAnimationsBindsRigJointsAndAllowsExtraModelBones(t *testing.T) {
	dir := t.TempDir()
	identity := AssetTransformDef{Rotation: Quat{0, 0, 0, 1}, Scale: Vec3{1, 1, 1}}
	rigPath := filepath.Join(dir, "humanoid.gkrig")
	if err := SaveAnimationRig(rigPath, &AnimationRigDef{ID: "humanoid", SchemaVersion: 1, Name: "Humanoid", Joints: []AnimationRigJointDef{
		{ID: "root", Transform: identity},
		{ID: "head", ParentID: "root", Transform: identity},
	}}); err != nil {
		t.Fatal(err)
	}
	setPath := filepath.Join(dir, "movement.gkanim")
	if err := SaveAnimationSet(setPath, &AnimationSetDef{ID: "movement", SchemaVersion: 1, Name: "Movement", RigPath: "humanoid.gkrig", Clips: []AssetAnimationClipDef{{
		ID: "idle", Name: "Idle", Duration: 1, Loop: true, Tracks: []AssetAnimationTrackDef{{TargetID: "head", RotationKeys: []AssetQuatKeyDef{{Value: Quat{0, 0, 0, 1}}}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	asset := &AssetDef{ID: "model", SchemaVersion: 4, Name: "Model", AnimationSetPaths: []string{"movement.gkanim"}, DefaultAnimationClipID: "idle",
		Skeleton: &AssetSkeletonDef{Bones: []AssetBoneDef{
			{ID: "bone_9", JointID: "root", Name: "Root", Transform: identity},
			{ID: "bone_2", JointID: "head", Name: "Head", ParentID: "bone_9", Transform: identity},
			{ID: "finger", Name: "Finger", ParentID: "bone_2", Transform: identity},
		}},
		Parts: []AssetPartDef{
			{ID: "bone_9", Name: "Root", Source: AssetSourceDef{Kind: AssetSourceKindGroup}, Transform: identity},
			{ID: "bone_2", Name: "Head", ParentID: "bone_9", Source: AssetSourceDef{Kind: AssetSourceKindGroup}, Transform: identity},
			{ID: "finger", Name: "Finger", ParentID: "bone_2", Source: AssetSourceDef{Kind: AssetSourceKindGroup}, Transform: identity},
		}}
	resolved, err := ResolveAssetAnimations(asset, filepath.Join(dir, "model.gkasset"))
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Clips) != 1 || resolved.Clips[0].Tracks[0].TargetID != "bone_2" || resolved.JointTargets["head"] != "bone_2" {
		t.Fatalf("unexpected resolved animations: %+v", resolved)
	}
}

func TestResolveAssetAnimationsRejectsIncompatibleRig(t *testing.T) {
	dir := t.TempDir()
	identity := AssetTransformDef{Rotation: Quat{0, 0, 0, 1}, Scale: Vec3{1, 1, 1}}
	if err := SaveAnimationRig(filepath.Join(dir, "rig.gkrig"), &AnimationRigDef{ID: "rig", SchemaVersion: 1, Name: "Rig", Joints: []AnimationRigJointDef{{ID: "root", Transform: identity}, {ID: "head", ParentID: "root", Transform: identity}}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveAnimationSet(filepath.Join(dir, "set.gkanim"), &AnimationSetDef{ID: "set", SchemaVersion: 1, Name: "Set", RigPath: "rig.gkrig", Clips: []AssetAnimationClipDef{{ID: "idle", Name: "Idle", Duration: 1, Tracks: []AssetAnimationTrackDef{{TargetID: "head"}}}}}); err != nil {
		t.Fatal(err)
	}
	model := func() *AssetDef {
		return &AssetDef{ID: "model", SchemaVersion: 4, Name: "Model", AnimationSetPaths: []string{"set.gkanim"}, DefaultAnimationClipID: "idle",
			Skeleton: &AssetSkeletonDef{Bones: []AssetBoneDef{
				{ID: "root", JointID: "root", Name: "Root", Transform: identity},
				{ID: "head", JointID: "head", Name: "Head", ParentID: "root", Transform: identity},
			}},
			Parts: []AssetPartDef{
				{ID: "root", Name: "Root", Source: AssetSourceDef{Kind: AssetSourceKindGroup}, Transform: identity},
				{ID: "head", Name: "Head", ParentID: "root", Source: AssetSourceDef{Kind: AssetSourceKindGroup}, Transform: identity},
			}}
	}
	for _, test := range []struct {
		name   string
		mutate func(*AssetDef)
	}{
		{"missing joint", func(asset *AssetDef) { asset.Skeleton.Bones = asset.Skeleton.Bones[:1]; asset.Parts = asset.Parts[:1] }},
		{"hierarchy mismatch", func(asset *AssetDef) {
			asset.Skeleton.Bones = append(asset.Skeleton.Bones, AssetBoneDef{ID: "helper", JointID: "helper", Name: "Helper", ParentID: "root", Transform: identity})
			asset.Parts = append(asset.Parts, AssetPartDef{ID: "helper", Name: "Helper", ParentID: "root", Source: AssetSourceDef{Kind: AssetSourceKindGroup}, Transform: identity})
			asset.Skeleton.Bones[1].ParentID, asset.Parts[1].ParentID = "helper", "helper"
		}},
		{"bind mismatch", func(asset *AssetDef) { asset.Parts[1].Transform.Position = Vec3{1, 0, 0} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			asset := model()
			test.mutate(asset)
			if _, err := ResolveAssetAnimations(asset, filepath.Join(dir, "model.gkasset")); err == nil {
				t.Fatal("expected incompatible rig to fail")
			}
		})
	}
}

func TestResolveAssetAnimationsBindsAssetLocalSet(t *testing.T) {
	dir := t.TempDir()
	setPath := filepath.Join(dir, "door.gkanim")
	if err := SaveAnimationSet(setPath, &AnimationSetDef{ID: "door", SchemaVersion: 1, Name: "Door", TargetAssetID: "door-asset", Clips: []AssetAnimationClipDef{{ID: "open", Name: "Open", Duration: 1, Tracks: []AssetAnimationTrackDef{{TargetID: "panel"}}}}}); err != nil {
		t.Fatal(err)
	}
	asset := &AssetDef{ID: "door-asset", SchemaVersion: 4, Name: "Door", AnimationSetPaths: []string{"door.gkanim"}, DefaultAnimationClipID: "open", Parts: []AssetPartDef{{ID: "panel", Name: "Panel", Source: AssetSourceDef{Kind: AssetSourceKindGroup}, Transform: AssetTransformDef{Rotation: Quat{0, 0, 0, 1}, Scale: Vec3{1, 1, 1}}}}}
	resolved, err := ResolveAssetAnimations(asset, filepath.Join(dir, "door.gkasset"))
	if err != nil || resolved.Clips[0].Tracks[0].TargetID != "panel" {
		t.Fatalf("resolve local set: %+v, %v", resolved, err)
	}
}

func TestAnimationDocumentValidationRejectsAmbiguousOrDuplicateContracts(t *testing.T) {
	clip := AssetAnimationClipDef{ID: "idle", Name: "Idle"}
	for _, test := range []struct {
		name string
		err  error
	}{
		{"mixed binding", ValidateAnimationSet(&AnimationSetDef{ID: "set", SchemaVersion: 1, Name: "Set", RigPath: "rig.gkrig", TargetAssetID: "asset", Clips: []AssetAnimationClipDef{clip}})},
		{"duplicate clips", ValidateAnimationSet(&AnimationSetDef{ID: "set", SchemaVersion: 1, Name: "Set", RigPath: "rig.gkrig", Clips: []AssetAnimationClipDef{clip, clip}})},
		{"duplicate joints", ValidateAnimationRig(&AnimationRigDef{ID: "rig", SchemaVersion: 1, Name: "Rig", Joints: []AnimationRigJointDef{{ID: "root"}, {ID: "root"}}})},
		{"missing parent", ValidateAnimationRig(&AnimationRigDef{ID: "rig", SchemaVersion: 1, Name: "Rig", Joints: []AnimationRigJointDef{{ID: "child", ParentID: "root"}}})},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}
