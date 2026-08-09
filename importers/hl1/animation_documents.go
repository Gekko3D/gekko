package hl1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"

	"github.com/gekko3d/gekko/content"
)

type MDLAnimationDocuments struct {
	RigPath string
	Rig     *content.AnimationRigDef
	SetPath string
	Set     *content.AnimationSetDef
}

func BuildMDLAnimationDocuments(asset *content.AssetDef, clips []content.AssetAnimationClipDef, assetPath string) (MDLAnimationDocuments, error) {
	return BuildMDLAnimationDocumentsAtRoot(asset, clips, assetPath, filepath.Dir(filepath.Dir(assetPath)))
}

func BuildMDLAnimationDocumentsAtRoot(asset *content.AssetDef, clips []content.AssetAnimationClipDef, assetPath, root string) (MDLAnimationDocuments, error) {
	if asset == nil || len(clips) == 0 {
		return MDLAnimationDocuments{}, nil
	}
	set := &content.AnimationSetDef{SchemaVersion: content.CurrentAnimationSetSchemaVersion, Name: asset.Name + " animations"}
	rig, rigClips, ok, err := rigAnimationDocuments(asset, clips)
	if err != nil {
		return MDLAnimationDocuments{}, err
	}
	result := MDLAnimationDocuments{}
	if ok {
		rigHash, err := animationDocumentHash(rig.Joints)
		if err != nil {
			return MDLAnimationDocuments{}, err
		}
		rig.ID = "rig." + rigHash
		rig.Name = rig.ID
		result.RigPath = filepath.Join(root, "rigs", rig.ID+".gkrig")
		result.Rig = rig
		set.RigPath, err = filepath.Rel(filepath.Join(root, "animations"), result.RigPath)
		if err != nil {
			return MDLAnimationDocuments{}, err
		}
		set.RigPath = filepath.ToSlash(set.RigPath)
		set.Clips = rigClips
	} else {
		set.TargetAssetID = asset.ID
		set.Clips = cloneMDLAnimationClips(clips)
	}
	setHash, err := animationDocumentHash(struct {
		RigID         string
		TargetAssetID string
		Clips         []content.AssetAnimationClipDef
	}{RigID: func() string {
		if result.Rig != nil {
			return result.Rig.ID
		}
		return ""
	}(), TargetAssetID: set.TargetAssetID, Clips: set.Clips})
	if err != nil {
		return MDLAnimationDocuments{}, err
	}
	set.ID = "animation." + setHash
	set.Name = set.ID
	result.SetPath = filepath.Join(root, "animations", set.ID+".gkanim")
	result.Set = set
	rel, err := filepath.Rel(filepath.Dir(assetPath), result.SetPath)
	if err != nil {
		return MDLAnimationDocuments{}, err
	}
	asset.AnimationSetPaths = []string{filepath.ToSlash(rel)}
	asset.DefaultAnimationClipID = clips[0].ID
	return result, nil
}

func rigAnimationDocuments(asset *content.AssetDef, clips []content.AssetAnimationClipDef) (*content.AnimationRigDef, []content.AssetAnimationClipDef, bool, error) {
	if asset.Skeleton == nil {
		return nil, nil, false, nil
	}
	bones := make(map[string]content.AssetBoneDef, len(asset.Skeleton.Bones))
	for _, bone := range asset.Skeleton.Bones {
		bones[bone.ID] = bone
	}
	for _, clip := range clips {
		for _, track := range clip.Tracks {
			bone, ok := bones[track.TargetID]
			if !ok || bone.JointID == "" {
				return nil, nil, false, nil
			}
		}
	}
	rig, converted, err := content.BuildRigAnimationDocuments(asset, clips)
	return rig, converted, true, err
}

func animationDocumentHash(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8]), nil
}

func cloneMDLAnimationClips(clips []content.AssetAnimationClipDef) []content.AssetAnimationClipDef {
	out := make([]content.AssetAnimationClipDef, len(clips))
	for i, clip := range clips {
		out[i] = clip
		out[i].Tags = append([]string(nil), clip.Tags...)
		out[i].TraversalMotion = append([]content.AssetVec3KeyDef(nil), clip.TraversalMotion...)
		out[i].Tracks = append([]content.AssetAnimationTrackDef(nil), clip.Tracks...)
		for j := range out[i].Tracks {
			out[i].Tracks[j].PositionKeys = append([]content.AssetVec3KeyDef(nil), clip.Tracks[j].PositionKeys...)
			out[i].Tracks[j].RotationKeys = append([]content.AssetQuatKeyDef(nil), clip.Tracks[j].RotationKeys...)
			out[i].Tracks[j].ScaleKeys = append([]content.AssetVec3KeyDef(nil), clip.Tracks[j].ScaleKeys...)
		}
	}
	return out
}

func assignDeterministicHL1AssetID(asset *content.AssetDef) error {
	if asset == nil {
		return nil
	}
	identity := *asset
	identity.ID = ""
	hash, err := animationDocumentHash(identity)
	if err != nil {
		return err
	}
	asset.ID = "asset." + hash
	return nil
}
