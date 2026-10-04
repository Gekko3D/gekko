package content

import (
	"fmt"
	"math"
)

type ResolvedAssetAnimations struct {
	DefaultClipID string
	Clips         []AssetAnimationClipDef
	JointTargets  map[string]string
}

func BuildRigAnimationDocuments(asset *AssetDef, clips []AssetAnimationClipDef) (*AnimationRigDef, []AssetAnimationClipDef, error) {
	if asset == nil || asset.Skeleton == nil {
		return nil, nil, fmt.Errorf("target asset has no skeleton")
	}
	bones := make(map[string]AssetBoneDef, len(asset.Skeleton.Bones))
	parts := make(map[string]AssetPartDef, len(asset.Parts))
	for _, bone := range asset.Skeleton.Bones {
		bones[bone.ID] = bone
	}
	for _, part := range asset.Parts {
		parts[part.ID] = part
	}
	included := map[string]struct{}{}
	for _, clip := range clips {
		for _, track := range animationClipTracks(clip) {
			bone, ok := bones[track.TargetID]
			if !ok || bone.JointID == "" {
				return nil, nil, fmt.Errorf("animation target %q has no semantic joint", track.TargetID)
			}
			for bone.ID != "" {
				included[bone.ID] = struct{}{}
				bone = bones[bone.ParentID]
			}
		}
	}
	rig := &AnimationRigDef{SchemaVersion: CurrentAnimationRigSchemaVersion}
	seen := map[string]struct{}{}
	for _, bone := range asset.Skeleton.Bones {
		if _, ok := included[bone.ID]; !ok {
			continue
		}
		if _, exists := seen[bone.JointID]; exists {
			return nil, nil, fmt.Errorf("target asset maps semantic joint %q more than once", bone.JointID)
		}
		part, ok := parts[bone.ID]
		if !ok {
			return nil, nil, fmt.Errorf("target bone %q has no animated part", bone.ID)
		}
		parentJoint := ""
		if bone.ParentID != "" {
			parentJoint = bones[bone.ParentID].JointID
		}
		rig.Joints = append(rig.Joints, AnimationRigJointDef{ID: bone.JointID, ParentID: parentJoint, Transform: part.Transform})
		seen[bone.JointID] = struct{}{}
	}
	converted := cloneAnimationClips(clips)
	for i := range converted {
		mapAnimationClipTargets(&converted[i], func(id string) string { return bones[id].JointID })
	}
	return rig, converted, nil
}

func ResolveAssetAnimations(def *AssetDef, documentPath string) (*ResolvedAssetAnimations, error) {
	return resolveAssetAnimations(def, documentPath, func(reference, document string) (string, error) {
		return ResolveDocumentPath(reference, document), nil
	})
}

// ResolveCompiledAssetAnimations resolves animation sets and their rigs strictly
// relative to their owning compiled documents, without legacy path fallbacks.
func ResolveCompiledAssetAnimations(def *AssetDef, documentPath string) (*ResolvedAssetAnimations, error) {
	return resolveAssetAnimations(def, documentPath, ResolveCompiledAssetReference)
}

func resolveAssetAnimations(def *AssetDef, documentPath string, resolvePath func(string, string) (string, error)) (*ResolvedAssetAnimations, error) {
	if def == nil {
		return nil, fmt.Errorf("asset definition is nil")
	}
	if len(def.AnimationSetPaths) == 0 {
		if def.DefaultAnimationClipID != "" {
			return nil, fmt.Errorf("asset %q has a default animation clip but no animation sets", def.Name)
		}
		return nil, nil
	}
	if documentPath == "" {
		return nil, fmt.Errorf("asset %q requires a document path to resolve animation sets", def.Name)
	}

	itemIDs := assetAnimationItemIDs(def)
	jointTargets, bonesByJoint, bonesByID, partsByID, err := assetAnimationJointTargets(def, itemIDs)
	if err != nil {
		return nil, err
	}
	resolved := &ResolvedAssetAnimations{DefaultClipID: def.DefaultAnimationClipID, JointTargets: jointTargets}
	seenClips := map[string]string{}
	for _, ref := range def.AnimationSetPaths {
		setPath, err := resolvePath(ref, documentPath)
		if err != nil {
			return nil, fmt.Errorf("resolve animation set %q: %w", ref, err)
		}
		set, err := LoadAnimationSet(setPath)
		if err != nil {
			return nil, fmt.Errorf("load animation set %q: %w", ref, err)
		}
		clips, err := resolveAnimationSetForAsset(def, set, setPath, itemIDs, jointTargets, bonesByJoint, bonesByID, partsByID, resolvePath)
		if err != nil {
			return nil, fmt.Errorf("resolve animation set %q: %w", ref, err)
		}
		for _, clip := range clips {
			if previous, exists := seenClips[clip.ID]; exists {
				return nil, fmt.Errorf("duplicate animation clip %q in %s and %s", clip.ID, previous, ref)
			}
			seenClips[clip.ID] = ref
			resolved.Clips = append(resolved.Clips, clip)
		}
	}
	if _, ok := seenClips[resolved.DefaultClipID]; !ok {
		return nil, fmt.Errorf("asset %q default animation clip %q was not resolved", def.Name, resolved.DefaultClipID)
	}
	return resolved, nil
}

func resolveAnimationSetForAsset(def *AssetDef, set *AnimationSetDef, setPath string, itemIDs, jointTargets map[string]string, bonesByJoint, bonesByID map[string]AssetBoneDef, partsByID map[string]AssetPartDef, resolvePath func(string, string) (string, error)) ([]AssetAnimationClipDef, error) {
	if set.TargetAssetID != "" {
		if set.TargetAssetID != def.ID {
			return nil, fmt.Errorf("targets asset %q, not %q", set.TargetAssetID, def.ID)
		}
		result := AssetValidationResult{}
		validateAnimationClips(&result, set.Clips, itemIDs, true)
		if result.HasErrors() {
			return nil, fmt.Errorf("invalid local animation: %s", result.Error())
		}
		return cloneAnimationClips(set.Clips), nil
	}

	rigPath, err := resolvePath(set.RigPath, setPath)
	if err != nil {
		return nil, fmt.Errorf("resolve rig %q: %w", set.RigPath, err)
	}
	rig, err := LoadAnimationRig(rigPath)
	if err != nil {
		return nil, fmt.Errorf("load rig %q: %w", set.RigPath, err)
	}
	rigTargets := make(map[string]string, len(rig.Joints))
	for _, joint := range rig.Joints {
		targetID, ok := jointTargets[joint.ID]
		if !ok {
			return nil, fmt.Errorf("asset %q does not map required rig joint %q", def.Name, joint.ID)
		}
		bone := bonesByJoint[joint.ID]
		modelParentJoint := ""
		if bone.ParentID != "" {
			modelParentJoint = bonesByID[bone.ParentID].JointID
		}
		if modelParentJoint != joint.ParentID {
			return nil, fmt.Errorf("asset joint %q parent is %q, rig requires %q", joint.ID, modelParentJoint, joint.ParentID)
		}
		if !animationTransformsEqual(partsByID[bone.ID].Transform, joint.Transform) {
			return nil, fmt.Errorf("asset joint %q bind transform differs from rig", joint.ID)
		}
		rigTargets[joint.ID] = targetID
	}
	result := AssetValidationResult{}
	validateAnimationClips(&result, set.Clips, rigTargets, true)
	if result.HasErrors() {
		return nil, fmt.Errorf("invalid rig animation: %s", result.Error())
	}
	clips := cloneAnimationClips(set.Clips)
	for i := range clips {
		mapAnimationClipTargets(&clips[i], func(id string) string { return rigTargets[id] })
	}
	return clips, nil
}

func assetAnimationItemIDs(def *AssetDef) map[string]string {
	ids := make(map[string]string, len(def.Parts)+len(def.Lights)+len(def.Emitters)+len(def.Markers))
	for _, part := range def.Parts {
		ids[part.ID] = "part"
	}
	for _, light := range def.Lights {
		ids[light.ID] = "light"
	}
	for _, emitter := range def.Emitters {
		ids[emitter.ID] = "emitter"
	}
	for _, marker := range def.Markers {
		ids[marker.ID] = "marker"
	}
	return ids
}

func assetAnimationJointTargets(def *AssetDef, itemIDs map[string]string) (map[string]string, map[string]AssetBoneDef, map[string]AssetBoneDef, map[string]AssetPartDef, error) {
	targets := map[string]string{}
	byJoint := map[string]AssetBoneDef{}
	byID := map[string]AssetBoneDef{}
	if def.Skeleton == nil {
		return targets, byJoint, byID, map[string]AssetPartDef{}, nil
	}
	parts := make(map[string]AssetPartDef, len(def.Parts))
	for _, part := range def.Parts {
		parts[part.ID] = part
	}
	for _, bone := range def.Skeleton.Bones {
		byID[bone.ID] = bone
		if bone.JointID == "" {
			continue
		}
		if _, exists := targets[bone.JointID]; exists {
			return nil, nil, nil, nil, fmt.Errorf("asset %q maps rig joint %q more than once", def.Name, bone.JointID)
		}
		if itemIDs[bone.ID] != "part" {
			return nil, nil, nil, nil, fmt.Errorf("asset joint %q has no matching part %q", bone.JointID, bone.ID)
		}
		part := parts[bone.ID]
		if part.ParentID != bone.ParentID {
			return nil, nil, nil, nil, fmt.Errorf("asset joint %q skeleton and part hierarchies differ", bone.JointID)
		}
		targets[bone.JointID] = bone.ID
		byJoint[bone.JointID] = bone
	}
	return targets, byJoint, byID, parts, nil
}

func animationTransformsEqual(a, b AssetTransformDef) bool {
	return animationVec3Equal(a.Position, b.Position) && animationVec3Equal(a.Scale, b.Scale) && animationQuatEqual(a.Rotation, b.Rotation)
}

func animationVec3Equal(a, b Vec3) bool {
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > 1e-5 {
			return false
		}
	}
	return true
}

func animationQuatEqual(a, b Quat) bool {
	dot := float64(a[0]*b[0] + a[1]*b[1] + a[2]*b[2] + a[3]*b[3])
	return math.Abs(math.Abs(dot)-1) <= 1e-5
}

func cloneAnimationClips(clips []AssetAnimationClipDef) []AssetAnimationClipDef {
	out := make([]AssetAnimationClipDef, len(clips))
	for i, clip := range clips {
		out[i] = clip
		out[i].Tags = append([]string(nil), clip.Tags...)
		out[i].Events = append([]AssetAnimationEventDef(nil), clip.Events...)
		out[i].TraversalMotion = append([]AssetVec3KeyDef(nil), clip.TraversalMotion...)
		out[i].Tracks = append([]AssetAnimationTrackDef(nil), clip.Tracks...)
		cloneAnimationTracks(out[i].Tracks, clip.Tracks)
		if clip.Blend1D != nil {
			out[i].Blend1D = &AssetAnimationBlend1DDef{Parameter: clip.Blend1D.Parameter, Default: clip.Blend1D.Default, Samples: append([]AssetAnimationBlendSampleDef(nil), clip.Blend1D.Samples...)}
			for sample := range out[i].Blend1D.Samples {
				tracks := append([]AssetAnimationTrackDef(nil), clip.Blend1D.Samples[sample].Tracks...)
				cloneAnimationTracks(tracks, clip.Blend1D.Samples[sample].Tracks)
				out[i].Blend1D.Samples[sample].Tracks = tracks
			}
		}
	}
	return out
}

func animationClipTracks(clip AssetAnimationClipDef) []AssetAnimationTrackDef {
	if clip.Blend1D != nil && len(clip.Blend1D.Samples) > 0 {
		return clip.Blend1D.Samples[0].Tracks
	}
	return clip.Tracks
}

func mapAnimationClipTargets(clip *AssetAnimationClipDef, mapID func(string) string) {
	for index := range clip.Tracks {
		clip.Tracks[index].TargetID = mapID(clip.Tracks[index].TargetID)
	}
	if clip.Blend1D != nil {
		for sample := range clip.Blend1D.Samples {
			for index := range clip.Blend1D.Samples[sample].Tracks {
				clip.Blend1D.Samples[sample].Tracks[index].TargetID = mapID(clip.Blend1D.Samples[sample].Tracks[index].TargetID)
			}
		}
	}
}

func cloneAnimationTracks(out, source []AssetAnimationTrackDef) {
	for index := range out {
		out[index].PositionKeys = append([]AssetVec3KeyDef(nil), source[index].PositionKeys...)
		out[index].RotationKeys = append([]AssetQuatKeyDef(nil), source[index].RotationKeys...)
		out[index].ScaleKeys = append([]AssetVec3KeyDef(nil), source[index].ScaleKeys...)
	}
}
