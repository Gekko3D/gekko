package hl1

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
	"github.com/go-gl/mathgl/mgl32"
)

type MDLVoxelAssetOptions struct {
	Name            string
	SourceRef       string
	VoxelResolution float32
}

func BuildMDLVoxelAsset(geometry MDLGeometry, opts MDLVoxelAssetOptions) (*content.AssetDef, int, error) {
	resolution := opts.VoxelResolution
	if resolution <= 0 {
		resolution = DefaultImportedVoxelResolution
	}
	if len(geometry.Triangles) == 0 {
		return nil, 0, fmt.Errorf("mdl contains no decoded triangles")
	}
	if boneVoxels := voxelizeMDLGeometryByBone(geometry, resolution); len(boneVoxels) > 0 {
		return buildMDLRigidBoneVoxelAsset(geometry, opts, resolution, boneVoxels)
	}
	voxels := voxelizeMDLGeometry(geometry, resolution)
	if len(voxels) == 0 {
		return nil, 0, fmt.Errorf("mdl voxelization produced no voxels")
	}
	localVoxels, origin := localizeMDLVoxels(voxels, resolution)
	materials, palette := mdlAssetMaterialsAndPalette(voxels)
	asset := newMDLVoxelAssetBase(geometry, opts)
	asset.Skeleton = mdlAssetSkeleton(geometry.Info.Bones)
	asset.AnimationClips = mdlAnimationClips(geometry.Info.Sequences, geometry.Info.Bones, []mdlAnimationBindTarget{{
		ID:        "mdl_surface",
		BoneIndex: -1,
		Position:  content.Vec3{origin.X, origin.Y, origin.Z},
		Rotation:  content.Quat{0, 0, 0, 1},
		Scale:     content.Vec3{1, 1, 1},
	}})
	if len(asset.AnimationClips) > 0 {
		asset.Tags = append(asset.Tags, "animation:bind_pose_clip")
	}
	asset.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: len(asset.AnimationClips) == 0}
	asset.Materials = materials
	asset.Parts = []content.AssetPartDef{{
		ID:              "mdl_surface",
		Name:            "mdl_surface",
		VoxelResolution: resolution,
		Transform: content.AssetTransformDef{
			Position: content.Vec3{origin.X, origin.Y, origin.Z},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Source: content.AssetSourceDef{
			Kind: content.AssetSourceKindVoxelShape,
			VoxelShape: &content.AssetVoxelShapeDef{
				Palette: palette,
				Voxels:  localVoxels,
			},
		},
		Tags: []string{"source:hl1", "source_asset:mdl", "generated:mdl_voxel_surface"},
	}}
	return asset, len(localVoxels), nil
}

type mdlAnimationBindTarget struct {
	ID        string
	BoneIndex int
	Position  content.Vec3
	Rotation  content.Quat
	Scale     content.Vec3
}

func newMDLVoxelAssetBase(geometry MDLGeometry, opts MDLVoxelAssetOptions) *content.AssetDef {
	name := opts.Name
	if name == "" {
		name = geometry.Info.Name
	}
	if name == "" {
		name = "hl1_model"
	}
	asset := content.NewAssetDef(name)
	asset.Tags = []string{"source:hl1", "source_asset:mdl", "generated:mdl_voxel_surface"}
	if opts.SourceRef != "" {
		asset.Tags = append(asset.Tags, "source_ref:"+opts.SourceRef)
	}
	return asset
}

func buildMDLRigidBoneVoxelAsset(geometry MDLGeometry, opts MDLVoxelAssetOptions, resolution float32, boneVoxels map[int]map[[3]int]mdlVoxelSample) (*content.AssetDef, int, error) {
	allVoxels := mergeMDLVoxelMaps(boneVoxels)
	if len(allVoxels) == 0 {
		return nil, 0, fmt.Errorf("mdl voxelization produced no voxels")
	}
	materials, palette := mdlAssetMaterialsAndPalette(allVoxels)
	asset := newMDLVoxelAssetBase(geometry, opts)
	asset.Tags = append(asset.Tags, "generated:mdl_rigid_bone_parts")
	asset.Skeleton = mdlAssetSkeleton(geometry.Info.Bones)
	asset.Materials = materials
	asset.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: false}

	boneIDs := mdlAssetBoneIDs(geometry.Info.Bones)
	boneIndices := sortedMDLBoneVoxelIndices(boneVoxels)
	bindTargets := make([]mdlAnimationBindTarget, 0, len(boneIndices))
	voxelCount := 0
	for _, boneIndex := range boneIndices {
		voxels := boneVoxels[boneIndex]
		if len(voxels) == 0 || boneIndex < 0 || boneIndex >= len(boneIDs) {
			continue
		}
		boneID := boneIDs[boneIndex]
		boneOrigin := mdlBoneGlobalOriginGekko(geometry.Info.Bones, boneIndex)
		localVoxels, visualOrigin := localizeMDLVoxelsWithPalette(voxels, resolution, newMDLColorPalette(allVoxels))
		voxelCount += len(localVoxels)
		childOffset := importcommon.Vec3{
			X: visualOrigin.X - boneOrigin.X,
			Y: visualOrigin.Y - boneOrigin.Y,
			Z: visualOrigin.Z - boneOrigin.Z,
		}

		asset.Parts = append(asset.Parts, content.AssetPartDef{
			ID:     boneID,
			Name:   nonEmptyString(geometry.Info.Bones[boneIndex].Name, boneID),
			Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup},
			Transform: content.AssetTransformDef{
				Position: content.Vec3{boneOrigin.X, boneOrigin.Y, boneOrigin.Z},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
			Tags: []string{"source:hl1", "source_asset:mdl", "kind:mdl_bone", fmt.Sprintf("bone_index:%d", boneIndex)},
		})
		asset.Parts = append(asset.Parts, content.AssetPartDef{
			ID:              boneID + "_voxels",
			Name:            nonEmptyString(geometry.Info.Bones[boneIndex].Name, boneID) + " voxels",
			ParentID:        boneID,
			VoxelResolution: resolution,
			Transform: content.AssetTransformDef{
				Position: content.Vec3{childOffset.X, childOffset.Y, childOffset.Z},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
			Source: content.AssetSourceDef{
				Kind: content.AssetSourceKindVoxelShape,
				VoxelShape: &content.AssetVoxelShapeDef{
					Palette: palette,
					Voxels:  localVoxels,
				},
			},
			Tags: []string{"source:hl1", "source_asset:mdl", "generated:mdl_rigid_bone_part", fmt.Sprintf("bone_index:%d", boneIndex)},
		})
		bindTargets = append(bindTargets, mdlAnimationBindTarget{
			ID:        boneID,
			BoneIndex: boneIndex,
			Position:  content.Vec3{boneOrigin.X, boneOrigin.Y, boneOrigin.Z},
			Rotation:  content.Quat{0, 0, 0, 1},
			Scale:     content.Vec3{1, 1, 1},
		})
	}
	if len(asset.Parts) == 0 || voxelCount == 0 {
		return nil, 0, fmt.Errorf("mdl rigid bone voxelization produced no voxel parts")
	}
	asset.AnimationClips = mdlAnimationClips(geometry.Info.Sequences, geometry.Info.Bones, bindTargets)
	if len(asset.AnimationClips) > 0 {
		asset.Tags = append(asset.Tags, "animation:bind_pose_clip")
	}
	return asset, voxelCount, nil
}

func mdlAssetSkeleton(bones []MDLBoneInfo) *content.AssetSkeletonDef {
	if len(bones) == 0 {
		return nil
	}
	boneIDs := mdlAssetBoneIDs(bones)
	out := &content.AssetSkeletonDef{Bones: make([]content.AssetBoneDef, 0, len(bones))}
	for i, bone := range bones {
		pos := HammerToGekko(bone.Position)
		parentID := ""
		if bone.Parent >= 0 && bone.Parent < len(boneIDs) && bone.Parent != i {
			parentID = boneIDs[bone.Parent]
		}
		out.Bones = append(out.Bones, content.AssetBoneDef{
			ID:       boneIDs[i],
			Name:     nonEmptyString(bone.Name, fmt.Sprintf("bone_%02d", i)),
			ParentID: parentID,
			Transform: content.AssetTransformDef{
				Position: content.Vec3{pos.X, pos.Y, pos.Z},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
			Tags: []string{"source:hl1", "source_asset:mdl", fmt.Sprintf("bone_index:%d", i)},
		})
	}
	return out
}

func mdlAssetBoneIDs(bones []MDLBoneInfo) []string {
	ids := make([]string, len(bones))
	seen := map[string]int{}
	for i, bone := range bones {
		base := "bone_" + fmt.Sprintf("%02d", i)
		if safe := safeMDLAssetID(bone.Name); safe != "" {
			base += "_" + safe
		}
		id := base
		seen[base]++
		if seen[base] > 1 {
			id = fmt.Sprintf("%s_%d", base, seen[base])
		}
		ids[i] = id
	}
	return ids
}

func mdlAnimationClips(sequences []MDLSequenceInfo, bones []MDLBoneInfo, targets []mdlAnimationBindTarget) []content.AssetAnimationClipDef {
	if len(sequences) == 0 || len(targets) == 0 {
		return nil
	}
	clips := make([]content.AssetAnimationClipDef, 0, len(sequences))
	seenIDs := map[string]int{}
	for _, seq := range sequences {
		if seq.FrameCount <= 0 {
			continue
		}
		var clip content.AssetAnimationClipDef
		var ok bool
		if len(seq.BoneAnimations) > 0 && len(bones) > 0 {
			clip, ok = mdlDecodedAnimationClip(seq, bones, targets)
		}
		if !ok {
			clip, ok = mdlBindPoseAnimationClip(seq, targets)
		}
		if !ok {
			continue
		}
		clip.ID = uniqueMDLAnimationClipID(clip.ID, seenIDs)
		clips = append(clips, clip)
	}
	return clips
}

func uniqueMDLAnimationClipID(id string, seen map[string]int) string {
	if id == "" {
		id = "mdl_clip"
	}
	seen[id]++
	if seen[id] == 1 {
		return id
	}
	return fmt.Sprintf("%s_%d", id, seen[id])
}

func mdlBindPoseAnimationClip(seq MDLSequenceInfo, targets []mdlAnimationBindTarget) (content.AssetAnimationClipDef, bool) {
	fps := seq.FPS
	if fps <= 0 {
		fps = 30
	}
	duration := mdlSequenceDuration(seq, fps)
	name := nonEmptyString(seq.Name, "bind_pose")
	id := "mdl_" + safeMDLAssetID(name)
	if id == "mdl_" {
		id = "mdl_bind_pose"
	}
	tracks := make([]content.AssetAnimationTrackDef, 0, len(targets))
	for _, target := range targets {
		tracks = append(tracks, content.AssetAnimationTrackDef{
			TargetID: target.ID,
			PositionKeys: []content.AssetVec3KeyDef{
				{Time: 0, Value: target.Position},
				{Time: duration, Value: target.Position},
			},
			RotationKeys: []content.AssetQuatKeyDef{
				{Time: 0, Value: target.Rotation},
				{Time: duration, Value: target.Rotation},
			},
			ScaleKeys: []content.AssetVec3KeyDef{
				{Time: 0, Value: target.Scale},
				{Time: duration, Value: target.Scale},
			},
		})
	}
	return content.AssetAnimationClipDef{
		ID:       id,
		Name:     name,
		FPS:      fps,
		Duration: duration,
		Loop:     true,
		Tracks:   tracks,
		Tags:     []string{"source:hl1", "source_asset:mdl", "generated:bind_pose_clip"},
	}, true
}

func mdlDecodedAnimationClip(seq MDLSequenceInfo, bones []MDLBoneInfo, targets []mdlAnimationBindTarget) (content.AssetAnimationClipDef, bool) {
	fps := seq.FPS
	if fps <= 0 {
		fps = 30
	}
	duration := mdlSequenceDuration(seq, fps)
	name := nonEmptyString(seq.Name, "animation")
	id := "mdl_" + safeMDLAssetID(name)
	if id == "mdl_" {
		id = "mdl_animation"
	}

	bindFrames := mdlGlobalBoneFrameTransforms(bones, nil, 0)
	if len(bindFrames) != len(bones) {
		return content.AssetAnimationClipDef{}, false
	}
	tracks := make([]content.AssetAnimationTrackDef, 0, len(targets))
	for _, target := range targets {
		if target.BoneIndex < 0 || target.BoneIndex >= len(bones) {
			tracks = append(tracks, mdlBindPoseTrack(target, duration))
			continue
		}
		positionKeys := make([]content.AssetVec3KeyDef, 0, seq.FrameCount)
		rotationKeys := make([]content.AssetQuatKeyDef, 0, seq.FrameCount)
		scaleKeys := make([]content.AssetVec3KeyDef, 0, 1)
		bind := bindFrames[target.BoneIndex]
		for frame := 0; frame < seq.FrameCount; frame++ {
			frameTransforms := mdlGlobalBoneFrameTransforms(bones, seq.BoneAnimations, frame)
			if target.BoneIndex >= len(frameTransforms) {
				return content.AssetAnimationClipDef{}, false
			}
			frameTransform := frameTransforms[target.BoneIndex]
			position := HammerToGekko(frameTransform.Position)
			deltaRotation := frameTransform.Rotation.Mul(bind.Rotation.Inverse()).Normalize()
			rotation := hammerQuatToContentQuat(deltaRotation)
			t := float32(frame) / fps
			positionKeys = append(positionKeys, content.AssetVec3KeyDef{Time: t, Value: content.Vec3{position.X, position.Y, position.Z}})
			rotationKeys = append(rotationKeys, content.AssetQuatKeyDef{Time: t, Value: rotation})
		}
		scaleKeys = append(scaleKeys, content.AssetVec3KeyDef{Time: 0, Value: content.Vec3{1, 1, 1}})
		tracks = append(tracks, content.AssetAnimationTrackDef{
			TargetID:     target.ID,
			PositionKeys: positionKeys,
			RotationKeys: rotationKeys,
			ScaleKeys:    scaleKeys,
		})
	}
	return content.AssetAnimationClipDef{
		ID:       id,
		Name:     name,
		FPS:      fps,
		Duration: duration,
		Loop:     true,
		Tracks:   tracks,
		Tags:     []string{"source:hl1", "source_asset:mdl", "generated:sequence_clip"},
	}, len(tracks) > 0
}

func mdlBindPoseTrack(target mdlAnimationBindTarget, duration float32) content.AssetAnimationTrackDef {
	return content.AssetAnimationTrackDef{
		TargetID: target.ID,
		PositionKeys: []content.AssetVec3KeyDef{
			{Time: 0, Value: target.Position},
			{Time: duration, Value: target.Position},
		},
		RotationKeys: []content.AssetQuatKeyDef{
			{Time: 0, Value: target.Rotation},
			{Time: duration, Value: target.Rotation},
		},
		ScaleKeys: []content.AssetVec3KeyDef{
			{Time: 0, Value: target.Scale},
			{Time: duration, Value: target.Scale},
		},
	}
}

type mdlBoneFrameTransform struct {
	Position importcommon.Vec3
	Rotation mgl32.Quat
}

func mdlGlobalBoneFrameTransforms(bones []MDLBoneInfo, animations []MDLBoneAnimationInfo, frame int) []mdlBoneFrameTransform {
	localByBone := make(map[int]MDLBoneAnimationInfo, len(animations))
	for _, animation := range animations {
		localByBone[animation.BoneIndex] = animation
	}
	out := make([]mdlBoneFrameTransform, len(bones))
	for i, bone := range bones {
		localPosition := bone.Position
		localRotationEuler := bone.Rotation
		if animation, ok := localByBone[i]; ok {
			if frame >= 0 && frame < len(animation.PositionFrames) {
				localPosition = animation.PositionFrames[frame]
			}
			if frame >= 0 && frame < len(animation.RotationFrames) {
				localRotationEuler = animation.RotationFrames[frame]
			}
		}
		localRotation := mdlEulerXYZQuat(localRotationEuler)
		transform := mdlBoneFrameTransform{
			Position: localPosition,
			Rotation: localRotation,
		}
		if bone.Parent >= 0 && bone.Parent < len(out) && bone.Parent != i {
			parent := out[bone.Parent]
			transform.Position = addVec3(parent.Position, quatRotateImportVec3(parent.Rotation, localPosition))
			transform.Rotation = parent.Rotation.Mul(localRotation).Normalize()
		}
		out[i] = transform
	}
	return out
}

func mdlEulerXYZQuat(rotation importcommon.Vec3) mgl32.Quat {
	qx := mgl32.QuatRotate(rotation.X, mgl32.Vec3{1, 0, 0})
	qy := mgl32.QuatRotate(rotation.Y, mgl32.Vec3{0, 1, 0})
	qz := mgl32.QuatRotate(rotation.Z, mgl32.Vec3{0, 0, 1})
	return qz.Mul(qy).Mul(qx).Normalize()
}

func hammerQuatToContentQuat(q mgl32.Quat) content.Quat {
	col0 := hammerDirectionToGekko(q.Rotate(mgl32.Vec3{1, 0, 0}))
	col1 := hammerDirectionToGekko(q.Rotate(mgl32.Vec3{0, 0, 1}))
	col2 := hammerDirectionToGekko(q.Rotate(mgl32.Vec3{0, -1, 0}))
	mat := mgl32.Mat3FromCols(col0, col1, col2).Mat4()
	converted := mgl32.Mat4ToQuat(mat).Normalize()
	if converted == (mgl32.Quat{}) {
		converted = mgl32.QuatIdent()
	}
	return content.Quat{converted.V.X(), converted.V.Y(), converted.V.Z(), converted.W}
}

func hammerDirectionToGekko(v mgl32.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{v.X(), v.Z(), -v.Y()}
}

func quatRotateImportVec3(q mgl32.Quat, v importcommon.Vec3) importcommon.Vec3 {
	rotated := q.Rotate(mgl32.Vec3{v.X, v.Y, v.Z})
	return importcommon.Vec3{X: rotated.X(), Y: rotated.Y(), Z: rotated.Z()}
}

func addVec3(a, b importcommon.Vec3) importcommon.Vec3 {
	return importcommon.Vec3{X: a.X + b.X, Y: a.Y + b.Y, Z: a.Z + b.Z}
}

func mdlSequenceDuration(seq MDLSequenceInfo, fps float32) float32 {
	if fps <= 0 {
		fps = 30
	}
	duration := float32(seq.FrameCount) / fps
	if duration <= 0 {
		duration = 1.0 / fps
	}
	return duration
}

func safeMDLAssetID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	return out
}

func nonEmptyString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func voxelizeMDLGeometry(geometry MDLGeometry, resolution float32) map[[3]int]mdlVoxelSample {
	out := map[[3]int]mdlVoxelSample{}
	half := importcommon.Vec3{X: resolution * 0.5, Y: resolution * 0.5, Z: resolution * 0.5}
	for _, tri := range geometry.Triangles {
		triWorld := [3]importcommon.Vec3{
			HammerToGekko(tri.Vertices[0].Position),
			HammerToGekko(tri.Vertices[1].Position),
			HammerToGekko(tri.Vertices[2].Position),
		}
		minB, maxB := triangleVoxelBounds(triWorld, resolution)
		for x := minB[0]; x <= maxB[0]; x++ {
			for y := minB[1]; y <= maxB[1]; y++ {
				for z := minB[2]; z <= maxB[2]; z++ {
					key := [3]int{x, y, z}
					if !triangleIntersectsVoxel(triWorld, key, half, resolution) {
						continue
					}
					color := sampleMDLTriangleColor(geometry, tri, triWorld, voxelCenter(key, resolution))
					if color[3] == 0 {
						continue
					}
					out[key] = mdlVoxelSample{Color: color}
				}
			}
		}
	}
	return out
}

func voxelizeMDLGeometryByBone(geometry MDLGeometry, resolution float32) map[int]map[[3]int]mdlVoxelSample {
	if len(geometry.Info.Bones) == 0 {
		return nil
	}
	out := map[int]map[[3]int]mdlVoxelSample{}
	half := importcommon.Vec3{X: resolution * 0.5, Y: resolution * 0.5, Z: resolution * 0.5}
	for _, tri := range geometry.Triangles {
		fallbackBoneIndex := dominantMDLTriangleBone(tri, len(geometry.Info.Bones))
		if fallbackBoneIndex < 0 {
			continue
		}
		triWorld := [3]importcommon.Vec3{
			HammerToGekko(tri.Vertices[0].Position),
			HammerToGekko(tri.Vertices[1].Position),
			HammerToGekko(tri.Vertices[2].Position),
		}
		minB, maxB := triangleVoxelBounds(triWorld, resolution)
		for x := minB[0]; x <= maxB[0]; x++ {
			for y := minB[1]; y <= maxB[1]; y++ {
				for z := minB[2]; z <= maxB[2]; z++ {
					key := [3]int{x, y, z}
					if !triangleIntersectsVoxel(triWorld, key, half, resolution) {
						continue
					}
					center := voxelCenter(key, resolution)
					boneIndex := mdlTriangleBoneAtPoint(tri, triWorld, center, len(geometry.Info.Bones), fallbackBoneIndex)
					if boneIndex < 0 {
						continue
					}
					color := sampleMDLTriangleColor(geometry, tri, triWorld, center)
					if color[3] == 0 {
						continue
					}
					if out[boneIndex] == nil {
						out[boneIndex] = map[[3]int]mdlVoxelSample{}
					}
					out[boneIndex][key] = mdlVoxelSample{Color: color}
				}
			}
		}
	}
	return out
}

func mdlTriangleBoneAtPoint(tri MDLTriangle, triWorld [3]importcommon.Vec3, point importcommon.Vec3, boneCount int, fallback int) int {
	if !mdlTriangleHasMixedBones(tri, boneCount) {
		return fallback
	}
	bary, ok := barycentricPoint(triWorld, point)
	if !ok {
		return fallback
	}
	bestBone := fallback
	bestWeight := float32(-math.MaxFloat32)
	for i, vertex := range tri.Vertices {
		if vertex.BoneIndex < 0 || vertex.BoneIndex >= boneCount {
			continue
		}
		if bary[i] > bestWeight {
			bestBone = vertex.BoneIndex
			bestWeight = bary[i]
		}
	}
	return bestBone
}

func mdlTriangleHasMixedBones(tri MDLTriangle, boneCount int) bool {
	first := -1
	for _, vertex := range tri.Vertices {
		if vertex.BoneIndex < 0 || vertex.BoneIndex >= boneCount {
			continue
		}
		if first < 0 {
			first = vertex.BoneIndex
			continue
		}
		if vertex.BoneIndex != first {
			return true
		}
	}
	return false
}

func dominantMDLTriangleBone(tri MDLTriangle, boneCount int) int {
	counts := map[int]int{}
	bestBone := -1
	bestCount := 0
	for _, vertex := range tri.Vertices {
		if vertex.BoneIndex < 0 || vertex.BoneIndex >= boneCount {
			continue
		}
		counts[vertex.BoneIndex]++
		if counts[vertex.BoneIndex] > bestCount {
			bestBone = vertex.BoneIndex
			bestCount = counts[vertex.BoneIndex]
		}
	}
	return bestBone
}

func mergeMDLVoxelMaps(boneVoxels map[int]map[[3]int]mdlVoxelSample) map[[3]int]mdlVoxelSample {
	out := map[[3]int]mdlVoxelSample{}
	for _, voxels := range boneVoxels {
		for key, sample := range voxels {
			out[key] = sample
		}
	}
	return out
}

func sortedMDLBoneVoxelIndices(boneVoxels map[int]map[[3]int]mdlVoxelSample) []int {
	indices := make([]int, 0, len(boneVoxels))
	for boneIndex, voxels := range boneVoxels {
		if len(voxels) > 0 {
			indices = append(indices, boneIndex)
		}
	}
	sort.Ints(indices)
	return indices
}

func mdlBoneGlobalOriginGekko(bones []MDLBoneInfo, boneIndex int) importcommon.Vec3 {
	if boneIndex < 0 || boneIndex >= len(bones) {
		return importcommon.Vec3{}
	}
	transforms := make([]mdlBoneTransform, 0, len(bones))
	for _, bone := range bones {
		transforms = append(transforms, mdlBoneTransform{
			Parent:   bone.Parent,
			Position: bone.Position,
			Rotation: bone.Rotation,
		})
	}
	return HammerToGekko(transformMDLPointByBone(importcommon.Vec3{}, boneIndex, transforms, 0))
}

type mdlVoxelSample struct {
	Color [4]uint8
}

func sampleMDLTriangleColor(geometry MDLGeometry, tri MDLTriangle, triWorld [3]importcommon.Vec3, point importcommon.Vec3) [4]uint8 {
	bary, ok := barycentricPoint(triWorld, point)
	if !ok {
		return [4]uint8{180, 180, 180, 255}
	}
	u := bary[0]*tri.Vertices[0].UV[0] + bary[1]*tri.Vertices[1].UV[0] + bary[2]*tri.Vertices[2].UV[0]
	v := bary[0]*tri.Vertices[0].UV[1] + bary[1]*tri.Vertices[1].UV[1] + bary[2]*tri.Vertices[2].UV[1]
	if tri.TextureIndex < 0 || tri.TextureIndex >= len(geometry.Textures) {
		return [4]uint8{180, 180, 180, 255}
	}
	texture := geometry.Textures[tri.TextureIndex]
	color, ok := sampleMDLTexture(texture, u, v)
	if !ok {
		return [4]uint8{180, 180, 180, 255}
	}
	return color
}

func barycentricPoint(tri [3]importcommon.Vec3, point importcommon.Vec3) ([3]float32, bool) {
	v0 := subVec3(tri[1], tri[0])
	v1 := subVec3(tri[2], tri[0])
	v2 := subVec3(point, tri[0])
	d00 := dotVec3(v0, v0)
	d01 := dotVec3(v0, v1)
	d11 := dotVec3(v1, v1)
	d20 := dotVec3(v2, v0)
	d21 := dotVec3(v2, v1)
	denom := d00*d11 - d01*d01
	if float32(math.Abs(float64(denom))) < 1e-8 {
		return [3]float32{}, false
	}
	v := (d11*d20 - d01*d21) / denom
	w := (d00*d21 - d01*d20) / denom
	u := 1 - v - w
	u = clampFloat32(u, 0, 1)
	v = clampFloat32(v, 0, 1)
	w = clampFloat32(w, 0, 1)
	sum := u + v + w
	if sum <= 1e-6 {
		return [3]float32{}, false
	}
	return [3]float32{u / sum, v / sum, w / sum}, true
}

func sampleMDLTexture(texture MDLTexturePixels, u float32, v float32) ([4]uint8, bool) {
	width := texture.Info.Width
	height := texture.Info.Height
	if width <= 0 || height <= 0 || len(texture.Pixels) < width*height || len(texture.Palette) == 0 {
		return [4]uint8{}, false
	}
	x := wrapTextureCoord(int(math.Floor(float64(u*float32(width)))), width)
	y := wrapTextureCoord(int(math.Floor(float64(v*float32(height)))), height)
	paletteIndex := int(texture.Pixels[y*width+x])
	if paletteIndex < 0 || paletteIndex >= len(texture.Palette) {
		return [4]uint8{}, false
	}
	color := texture.Palette[paletteIndex]
	return [4]uint8{color[0], color[1], color[2], 255}, true
}

func localizeMDLVoxels(voxels map[[3]int]mdlVoxelSample, resolution float32) ([]content.VoxelObjectVoxelDef, importcommon.Vec3) {
	return localizeMDLVoxelsWithPalette(voxels, resolution, newMDLColorPalette(voxels))
}

func localizeMDLVoxelsWithPalette(voxels map[[3]int]mdlVoxelSample, resolution float32, palette mdlColorPalette) ([]content.VoxelObjectVoxelDef, importcommon.Vec3) {
	keys := make([][3]int, 0, len(voxels))
	first := true
	var minK [3]int
	for key := range voxels {
		keys = append(keys, key)
		if first {
			minK = key
			first = false
			continue
		}
		minK[0] = min(minK[0], key[0])
		minK[1] = min(minK[1], key[1])
		minK[2] = min(minK[2], key[2])
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		if keys[i][1] != keys[j][1] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][2] < keys[j][2]
	})
	out := make([]content.VoxelObjectVoxelDef, 0, len(keys))
	for _, key := range keys {
		out = append(out, content.VoxelObjectVoxelDef{
			X:     key[0] - minK[0],
			Y:     key[1] - minK[1],
			Z:     key[2] - minK[2],
			Value: palette.valueForColor(voxels[key].Color),
		})
	}
	return out, importcommon.Vec3{X: float32(minK[0]) * resolution, Y: float32(minK[1]) * resolution, Z: float32(minK[2]) * resolution}
}

func mdlAssetMaterialsAndPalette(voxels map[[3]int]mdlVoxelSample) ([]content.AssetMaterialDef, []content.AssetVoxelPaletteEntryDef) {
	pal := newMDLColorPalette(voxels)
	materials := make([]content.AssetMaterialDef, 0, len(pal.colors))
	shapePalette := make([]content.AssetVoxelPaletteEntryDef, 0, len(pal.colors))
	for _, entry := range pal.colors {
		materialID := fmt.Sprintf("mat_%d", entry.Value)
		materials = append(materials, content.AssetMaterialDef{
			ID:        materialID,
			Name:      materialID,
			BaseColor: entry.Color,
			Roughness: 0.85,
			IOR:       1.5,
			Tags:      []string{"source:hl1", "source_asset:mdl", "material:texture_baked", "material:static_prop"},
		})
		shapePalette = append(shapePalette, content.AssetVoxelPaletteEntryDef{Value: entry.Value, MaterialID: materialID})
	}
	return materials, shapePalette
}

type mdlPaletteColor struct {
	Value uint8
	Color [4]uint8
}

type mdlColorPalette struct {
	colors  []mdlPaletteColor
	byColor map[[4]uint8]uint8
}

func newMDLColorPalette(voxels map[[3]int]mdlVoxelSample) mdlColorPalette {
	counts := map[[4]uint8]int{}
	for _, sample := range voxels {
		counts[sample.Color]++
	}
	type counted struct {
		color [4]uint8
		count int
	}
	values := make([]counted, 0, len(counts))
	for color, count := range counts {
		values = append(values, counted{color: color, count: count})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].count != values[j].count {
			return values[i].count > values[j].count
		}
		return colorKey(values[i].color) < colorKey(values[j].color)
	})
	limit := min(len(values), 255)
	pal := mdlColorPalette{colors: make([]mdlPaletteColor, 0, limit), byColor: map[[4]uint8]uint8{}}
	for i := 0; i < limit; i++ {
		value := uint8(i + 1)
		pal.colors = append(pal.colors, mdlPaletteColor{Value: value, Color: values[i].color})
		pal.byColor[values[i].color] = value
	}
	return pal
}

func (p mdlColorPalette) valueForColor(color [4]uint8) uint8 {
	if value, ok := p.byColor[color]; ok {
		return value
	}
	bestValue := uint8(1)
	bestDistance := int(^uint(0) >> 1)
	for _, entry := range p.colors {
		distance := colorDistanceSquared(color, entry.Color)
		if distance < bestDistance {
			bestDistance = distance
			bestValue = entry.Value
		}
	}
	return bestValue
}

func colorDistanceSquared(a, b [4]uint8) int {
	dr := int(a[0]) - int(b[0])
	dg := int(a[1]) - int(b[1])
	db := int(a[2]) - int(b[2])
	return dr*dr + dg*dg + db*db
}

func colorKey(color [4]uint8) int {
	return int(color[0])<<24 | int(color[1])<<16 | int(color[2])<<8 | int(color[3])
}

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
