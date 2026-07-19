package hl1

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestCompatibleModelVariantsShareRigAnimationDocuments(t *testing.T) {
	identity := content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}
	build := func(assetID, rootID, headID, path string) MDLAnimationDocuments {
		asset := &content.AssetDef{
			ID: assetID, SchemaVersion: content.CurrentAssetSchemaVersion, Name: assetID,
			Skeleton: &content.AssetSkeletonDef{Bones: []content.AssetBoneDef{
				{ID: rootID, JointID: "bip01.pelvis", Name: "Bip01 Pelvis", Transform: identity},
				{ID: headID, JointID: "bip01.head", Name: "Bip01 Head", ParentID: rootID, Transform: identity},
			}},
			Parts: []content.AssetPartDef{
				{ID: rootID, Name: "Pelvis", Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup}, Transform: identity},
				{ID: headID, Name: "Head", ParentID: rootID, Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup}, Transform: identity},
			},
		}
		clips := []content.AssetAnimationClipDef{{ID: "run", Name: "Run", Duration: 1, Tracks: []content.AssetAnimationTrackDef{{TargetID: headID, RotationKeys: []content.AssetQuatKeyDef{{Value: content.Quat{0, 0, 0, 1}}}}}}}
		documents, err := BuildMDLAnimationDocumentsAtRoot(asset, clips, path, filepath.Join(t.TempDir(), "content"))
		if err != nil {
			t.Fatal(err)
		}
		return documents
	}
	root := t.TempDir()
	left := build("hgrunt", "hgrunt_pelvis", "hgrunt_head", filepath.Join(root, "models", "hgrunt.gkasset"))
	right := build("robo", "robo_pelvis", "robo_head", filepath.Join(root, "models", "robo.gkasset"))
	if filepath.Base(left.RigPath) != filepath.Base(right.RigPath) || filepath.Base(left.SetPath) != filepath.Base(right.SetPath) {
		t.Fatalf("compatible variants did not deduplicate: left=%s/%s right=%s/%s", left.RigPath, left.SetPath, right.RigPath, right.SetPath)
	}
	if !reflect.DeepEqual(left.Rig, right.Rig) || !reflect.DeepEqual(left.Set, right.Set) {
		t.Fatal("compatible variants produced different rig-bound documents")
	}
}
