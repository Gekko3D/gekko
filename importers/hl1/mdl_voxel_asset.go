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
	Name                string
	SourceRef           string
	VoxelResolution     float32
	VoxelizationProfile MDLVoxelizationProfile
	// StaticPose emits a single, transform-baked voxel part. It is for world
	// model uses (such as pickups) whose runtime contract is a static visual,
	// regardless of the skeletal representation in the source MDL.
	StaticPose      bool
	RebaseBoneIndex int
	RebaseToBone    bool
	// AlignStaticVoxelFrame rotates a static model into the nearest cardinal
	// frame of VoxelFrameBoneIndex while sampling, then restores its original
	// pose on the emitted part. It avoids stair-stepping on nearly axis-aligned
	// source geometry without changing its runtime orientation.
	AlignStaticVoxelFrame bool
	VoxelFrameBoneIndex   int
	// IncludeBoneIndices retains geometry weighted to these source bones. It
	// turns a composite source rig into independently mountable static props.
	IncludeBoneIndices []int
	// SemanticAnchors maps stable marker IDs to verified source bone indices.
	// Player catalog import supplies only known GoldSrc player-bone mappings.
	SemanticAnchors map[string]int
	// LockRootMotion keeps an animation-driven actor registered to its
	// controller rather than applying source root translation twice.
	LockRootMotion bool
}

// MDLVoxelizationProfile owns import-time visual quality policy. Runtime keeps
// consuming ordinary authored voxel parts and does not need source-format
// knowledge.
type MDLVoxelizationProfile struct {
	ID                          string  `json:"id"`
	CoverageSamples             int     `json:"coverage_samples"`
	RespectMaskedTextures       bool    `json:"respect_masked_textures"`
	FillClosedInterior          bool    `json:"fill_closed_interior,omitempty"`
	PartitionBySkeletonSegments bool    `json:"partition_by_skeleton_segments,omitempty"`
	JointCapVoxels              int     `json:"joint_cap_voxels,omitempty"`
	MaxInteriorSampleCells      int     `json:"max_interior_sample_cells,omitempty"`
	TargetMaxVoxelCount         int     `json:"target_max_voxel_count,omitempty"`
	CoarsestResolution          float32 `json:"coarsest_resolution,omitempty"`
}

func DefaultMDLVoxelizationProfile() MDLVoxelizationProfile {
	return MDLVoxelizationProfile{
		ID:                    "hl1_mdl_surface_v1",
		CoverageSamples:       7,
		RespectMaskedTextures: true,
	}
}

func MDLVoxelizationProfileForCategory(category HL1VoxelResolutionCategory) MDLVoxelizationProfile {
	profile := DefaultMDLVoxelizationProfile()
	switch category {
	case HL1VoxelResolutionCategoryStaticProp:
		profile.ID = "hl1_static_prop_solid_v1"
		profile.FillClosedInterior = true
		profile.MaxInteriorSampleCells = 4000000
		profile.TargetMaxVoxelCount = 120000
		profile.CoarsestResolution = 0.1
	case HL1VoxelResolutionCategoryNPC:
		profile.ID = "hl1_npc_rigid_v3"
		profile.FillClosedInterior = true
		profile.PartitionBySkeletonSegments = true
		profile.JointCapVoxels = 1
		profile.MaxInteriorSampleCells = 4000000
		profile.TargetMaxVoxelCount = 120000
		profile.CoarsestResolution = 0.08
	}
	return profile
}

func effectiveMDLVoxelizationProfile(profile MDLVoxelizationProfile) MDLVoxelizationProfile {
	if profile == (MDLVoxelizationProfile{}) {
		return DefaultMDLVoxelizationProfile()
	}
	if profile.ID == "" {
		profile.ID = "custom"
	}
	if profile.CoverageSamples <= 0 {
		profile.CoverageSamples = 1
	}
	if profile.JointCapVoxels < 0 {
		profile.JointCapVoxels = 0
	}
	if profile.MaxInteriorSampleCells < 0 {
		profile.MaxInteriorSampleCells = 0
	}
	if profile.TargetMaxVoxelCount < 0 {
		profile.TargetMaxVoxelCount = 0
	}
	return profile
}

type MDLVoxelAssetDocuments struct {
	Asset      *content.AssetDef
	Clips      []content.AssetAnimationClipDef
	VoxelCount int
}

func BuildMDLVoxelAssetDocuments(geometry MDLGeometry, opts MDLVoxelAssetOptions) (MDLVoxelAssetDocuments, error) {
	resolution := opts.VoxelResolution
	if resolution <= 0 {
		resolution = DefaultImportedVoxelResolution
	}
	opts.VoxelizationProfile = effectiveMDLVoxelizationProfile(opts.VoxelizationProfile)
	if len(geometry.Triangles) == 0 {
		return MDLVoxelAssetDocuments{}, fmt.Errorf("mdl contains no decoded triangles")
	}
	if len(opts.IncludeBoneIndices) > 0 {
		geometry = filterMDLGeometryByBones(geometry, opts.IncludeBoneIndices)
		if len(geometry.Triangles) == 0 {
			return MDLVoxelAssetDocuments{}, fmt.Errorf("mdl has no geometry for selected bones")
		}
	}
	if opts.RebaseToBone {
		geometry = rebaseMDLGeometryToBoneOrigin(geometry, opts.RebaseBoneIndex)
	}
	if opts.StaticPose {
		alignment := mdlStaticVoxelFrameAlignment(geometry.Info.Bones, opts)
		voxels, effectiveResolution := voxelizeMDLGeometryToBudgetInFrame(geometry, resolution, opts.VoxelizationProfile, alignment)
		asset, count, err := buildMDLStaticPoseVoxelAsset(geometry, opts, effectiveResolution, voxels, alignment.Inverse())
		return finishMDLVoxelAssetDocuments(asset, nil, count, err)
	}
	if boneVoxels, effectiveResolution := voxelizeMDLGeometryByBoneToBudget(geometry, resolution, opts.VoxelizationProfile); len(boneVoxels) > 0 {
		asset, clips, count, err := buildMDLRigidBoneVoxelAsset(geometry, opts, effectiveResolution, boneVoxels)
		return finishMDLVoxelAssetDocuments(asset, clips, count, err)
	}
	voxels, resolution := voxelizeMDLGeometryToBudget(geometry, resolution, opts.VoxelizationProfile)
	if len(voxels) == 0 {
		return MDLVoxelAssetDocuments{}, fmt.Errorf("mdl voxelization produced no voxels")
	}
	localVoxels, origin := localizeMDLVoxels(voxels, resolution)
	materials, palette := mdlAssetMaterialsAndPalette(voxels)
	asset := newMDLVoxelAssetBase(geometry, opts)
	asset.Skeleton = mdlAssetSkeleton(geometry.Info.Bones)
	clips := mdlAnimationClips(geometry.Info.Sequences, geometry.Info.Bones, []mdlAnimationBindTarget{{
		ID:        "mdl_surface",
		BoneIndex: -1,
		Position:  content.Vec3{origin.X, origin.Y, origin.Z},
		Rotation:  content.Quat{0, 0, 0, 1},
		Scale:     content.Vec3{1, 1, 1},
	}}, opts.LockRootMotion)
	if len(clips) > 0 {
		asset.Tags = append(asset.Tags, "animation:bind_pose_clip")
	}
	asset.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: len(clips) == 0}
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
	return finishMDLVoxelAssetDocuments(asset, clips, len(localVoxels), nil)
}

func finishMDLVoxelAssetDocuments(asset *content.AssetDef, clips []content.AssetAnimationClipDef, voxelCount int, buildErr error) (MDLVoxelAssetDocuments, error) {
	if buildErr != nil {
		return MDLVoxelAssetDocuments{}, buildErr
	}
	if err := assignDeterministicHL1AssetID(asset); err != nil {
		return MDLVoxelAssetDocuments{}, err
	}
	return MDLVoxelAssetDocuments{Asset: asset, Clips: clips, VoxelCount: voxelCount}, nil
}

func filterMDLGeometryByBones(geometry MDLGeometry, boneIndices []int) MDLGeometry {
	include := make(map[int]struct{}, len(boneIndices))
	for _, boneIndex := range boneIndices {
		include[boneIndex] = struct{}{}
	}
	triangles := make([]MDLTriangle, 0, len(geometry.Triangles))
	for _, triangle := range geometry.Triangles {
		if _, ok := include[dominantMDLTriangleBone(triangle, len(geometry.Info.Bones))]; ok {
			triangles = append(triangles, triangle)
		}
	}
	geometry.Triangles = triangles
	return geometry
}

func rebaseMDLGeometryToBoneOrigin(geometry MDLGeometry, boneIndex int) MDLGeometry {
	if boneIndex < 0 || boneIndex >= len(geometry.Info.Bones) {
		return geometry
	}
	frame := mdlGlobalBoneFrameTransforms(geometry.Info.Bones, nil, 0)[boneIndex]
	inverse := frame.Rotation.Inverse()
	for i := range geometry.Triangles {
		for j := range geometry.Triangles[i].Vertices {
			geometry.Triangles[i].Vertices[j].Position = quatRotateImportVec3(inverse, subVec3(geometry.Triangles[i].Vertices[j].Position, frame.Position))
		}
	}
	return geometry
}

func mdlStaticVoxelFrameAlignment(bones []MDLBoneInfo, opts MDLVoxelAssetOptions) mgl32.Quat {
	if !opts.AlignStaticVoxelFrame || opts.VoxelFrameBoneIndex < 0 || opts.VoxelFrameBoneIndex >= len(bones) {
		return mgl32.QuatIdent()
	}
	frames := mdlGlobalBoneFrameTransforms(bones, nil, 0)
	frame := frames[opts.VoxelFrameBoneIndex].Rotation
	if opts.RebaseToBone && opts.RebaseBoneIndex >= 0 && opts.RebaseBoneIndex < len(frames) {
		frame = frames[opts.RebaseBoneIndex].Rotation.Inverse().Mul(frame).Normalize()
	}
	frame = hammerQuatToMgl(frame)
	return mdlNearestVoxelAxisFrame(frame).Mul(frame.Inverse()).Normalize()
}

func mdlNearestVoxelAxisFrame(frame mgl32.Quat) mgl32.Quat {
	x := mdlNearestVoxelAxis(frame.Rotate(mgl32.Vec3{1, 0, 0}), mgl32.Vec3{})
	y := mdlNearestVoxelAxis(frame.Rotate(mgl32.Vec3{0, 1, 0}), x)
	z := x.Cross(y)
	return mgl32.Mat4ToQuat(mgl32.Mat3FromCols(x, y, z).Mat4()).Normalize()
}

func mdlNearestVoxelAxis(direction, excluded mgl32.Vec3) mgl32.Vec3 {
	axes := [3]mgl32.Vec3{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	bestAxis, bestDot := mgl32.Vec3{}, float32(-1)
	for _, axis := range axes {
		if excluded != (mgl32.Vec3{}) && axis.Dot(excluded) != 0 {
			continue
		}
		dot := direction.Dot(axis)
		if abs := float32(math.Abs(float64(dot))); abs > bestDot {
			bestAxis, bestDot = axis, abs
			if dot < 0 {
				bestAxis = bestAxis.Mul(-1)
			}
		}
	}
	return bestAxis
}

func buildMDLStaticPoseVoxelAsset(geometry MDLGeometry, opts MDLVoxelAssetOptions, resolution float32, voxels map[[3]int]mdlVoxelSample, rotation mgl32.Quat) (*content.AssetDef, int, error) {
	if len(voxels) == 0 {
		return nil, 0, fmt.Errorf("mdl voxelization produced no voxels")
	}
	materials, palette := mdlAssetMaterialsAndPalette(voxels)
	asset := newMDLVoxelAssetBase(geometry, opts)
	asset.Tags = append(asset.Tags, "generated:mdl_static_world_model")
	asset.Materials = materials
	asset.Parts = []content.AssetPartDef{{
		ID:              "mdl_surface",
		Name:            "mdl_surface",
		VoxelResolution: resolution,
		Transform: content.AssetTransformDef{
			Rotation: mglQuatToContent(rotation),
			Scale:    content.Vec3{1, 1, 1},
		},
		Source: content.AssetSourceDef{
			Kind: content.AssetSourceKindVoxelShape,
			VoxelShape: &content.AssetVoxelShapeDef{
				Palette: palette,
				Voxels:  mdlVoxelsAtSourceOrigin(voxels, newMDLColorPalette(voxels)),
			},
		},
		Tags: []string{"source:hl1", "source_asset:mdl", "generated:mdl_static_world_model"},
	}}
	return asset, len(voxels), nil
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
	if opts.VoxelizationProfile.ID != "" {
		asset.Tags = append(asset.Tags, "voxelization_profile:"+opts.VoxelizationProfile.ID)
	}
	if opts.SourceRef != "" {
		asset.Tags = append(asset.Tags, "source_ref:"+opts.SourceRef)
	}
	return asset
}

func buildMDLRigidBoneVoxelAsset(geometry MDLGeometry, opts MDLVoxelAssetOptions, resolution float32, boneVoxels map[int]map[[3]int]mdlVoxelSample) (*content.AssetDef, []content.AssetAnimationClipDef, int, error) {
	if mdlBoneVoxelCount(boneVoxels) == 0 {
		return nil, nil, 0, fmt.Errorf("mdl voxelization produced no voxels")
	}
	bonePalettes := make(map[int]mdlColorPalette, len(boneVoxels))
	for boneIndex, voxels := range boneVoxels {
		if len(voxels) > 0 {
			bonePalettes[boneIndex] = newMDLColorPalette(voxels)
		}
	}
	asset := newMDLVoxelAssetBase(geometry, opts)
	asset.Tags = append(asset.Tags, "generated:mdl_rigid_bone_parts", content.AssetTagSkeletonRestBasis)
	asset.Skeleton = mdlAssetSkeleton(geometry.Info.Bones)
	asset.Materials = mdlAssetMaterialsForPalettes(bonePalettes, boneVoxels)
	asset.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: false}

	boneIDs := mdlAssetBoneIDs(geometry.Info.Bones)
	bindTargets := make([]mdlAnimationBindTarget, 0, len(geometry.Info.Bones))
	voxelCount := 0
	for boneIndex := range geometry.Info.Bones {
		voxels := boneVoxels[boneIndex]
		if boneIndex < 0 || boneIndex >= len(boneIDs) {
			continue
		}
		boneID := boneIDs[boneIndex]
		boneOrigin := mdlBoneGlobalOriginGekko(geometry.Info.Bones, boneIndex)
		boneRotation := mdlBoneGlobalRotationGekko(geometry.Info.Bones, boneIndex)
		boneBindPosition := boneOrigin
		parentID := ""
		if parentIndex := geometry.Info.Bones[boneIndex].Parent; parentIndex >= 0 && parentIndex < len(boneIDs) && parentIndex != boneIndex {
			parentID = boneIDs[parentIndex]
			parentOrigin := mdlBoneGlobalOriginGekko(geometry.Info.Bones, parentIndex)
			boneBindPosition = subVec3(boneOrigin, parentOrigin)
		}

		asset.Parts = append(asset.Parts, content.AssetPartDef{
			ID:       boneID,
			Name:     nonEmptyString(geometry.Info.Bones[boneIndex].Name, boneID),
			ParentID: parentID,
			Source:   content.AssetSourceDef{Kind: content.AssetSourceKindGroup},
			Transform: content.AssetTransformDef{
				Position: content.Vec3{boneBindPosition.X, boneBindPosition.Y, boneBindPosition.Z},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
			Tags: []string{"source:hl1", "source_asset:mdl", "kind:mdl_bone", fmt.Sprintf("bone_index:%d", boneIndex)},
		})
		if len(voxels) > 0 {
			palette := bonePalettes[boneIndex]
			localVoxels, visualOrigin := localizeMDLVoxelsWithPalette(voxels, resolution, palette)
			voxelCount += len(localVoxels)
			childOffset := boneRotation.Rotate(mgl32.Vec3{visualOrigin.X, visualOrigin.Y, visualOrigin.Z})
			asset.Parts = append(asset.Parts, content.AssetPartDef{
				ID:              boneID + "_voxels",
				Name:            nonEmptyString(geometry.Info.Bones[boneIndex].Name, boneID) + " voxels",
				ParentID:        boneID,
				VoxelResolution: resolution,
				Transform: content.AssetTransformDef{
					Position: content.Vec3{childOffset.X(), childOffset.Y(), childOffset.Z()},
					Rotation: mglQuatToContent(boneRotation),
					Scale:    content.Vec3{1, 1, 1},
				},
				Source: content.AssetSourceDef{
					Kind: content.AssetSourceKindVoxelShape,
					VoxelShape: &content.AssetVoxelShapeDef{
						Palette: mdlAssetShapePalette(palette),
						Voxels:  localVoxels,
					},
				},
				Tags: []string{"source:hl1", "source_asset:mdl", "generated:mdl_rigid_bone_part", fmt.Sprintf("bone_index:%d", boneIndex)},
			})
		}
		bindTargets = append(bindTargets, mdlAnimationBindTarget{
			ID:        boneID,
			BoneIndex: boneIndex,
			Position:  content.Vec3{boneBindPosition.X, boneBindPosition.Y, boneBindPosition.Z},
			Rotation:  content.Quat{0, 0, 0, 1},
			Scale:     content.Vec3{1, 1, 1},
		})
	}
	if len(asset.Parts) == 0 || voxelCount == 0 {
		return nil, nil, 0, fmt.Errorf("mdl rigid bone voxelization produced no voxel parts")
	}
	markerIDs := make([]string, 0, len(opts.SemanticAnchors))
	for markerID := range opts.SemanticAnchors {
		markerIDs = append(markerIDs, markerID)
	}
	sort.Strings(markerIDs)
	for _, markerID := range markerIDs {
		boneIndex := opts.SemanticAnchors[markerID]
		if boneIndex < 0 || boneIndex >= len(boneIDs) {
			continue
		}
		asset.Markers = append(asset.Markers, content.AssetMarkerDef{
			ID:       markerID,
			Name:     markerID,
			Kind:     content.AssetMarkerKindEffectAnchor,
			ParentID: boneIDs[boneIndex],
			Transform: content.AssetTransformDef{
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
			Tags: []string{"source:hl1", "semantic:" + markerID},
		})
	}
	for index, hitbox := range geometry.Info.Hitboxes {
		if hitbox.Bone < 0 || hitbox.Bone >= len(boneIDs) {
			continue
		}
		bounds := HammerBoundsToGekko(hitbox.Bounds.Min, hitbox.Bounds.Max)
		center := mgl32.Vec3{
			(bounds.Min.X + bounds.Max.X) * 0.5,
			(bounds.Min.Y + bounds.Max.Y) * 0.5,
			(bounds.Min.Z + bounds.Max.Z) * 0.5,
		}
		half := mgl32.Vec3{
			(bounds.Max.X - bounds.Min.X) * 0.5,
			(bounds.Max.Y - bounds.Min.Y) * 0.5,
			(bounds.Max.Z - bounds.Min.Z) * 0.5,
		}
		rotation := mdlBoneGlobalRotationGekko(geometry.Info.Bones, hitbox.Bone)
		center = rotation.Rotate(center)
		asset.Markers = append(asset.Markers, content.AssetMarkerDef{
			ID:       fmt.Sprintf("mdl_hitbox_%02d", index),
			Name:     fmt.Sprintf("hitbox %d", index),
			Kind:     content.AssetMarkerKindEffectAnchor,
			ParentID: boneIDs[hitbox.Bone],
			Transform: content.AssetTransformDef{
				Position: content.Vec3{center.X(), center.Y(), center.Z()},
				Rotation: mglQuatToContent(rotation),
				Scale:    content.Vec3{half.X(), half.Y(), half.Z()},
			},
			Tags: []string{"source:hl1", fmt.Sprintf("source:hl1_hitbox:%d", index), fmt.Sprintf("source:hl1_hitgroup:%d", hitbox.Group)},
		})
	}
	for index, attachment := range geometry.Info.Attachments {
		if attachment.Bone < 0 || attachment.Bone >= len(boneIDs) {
			continue
		}
		rotation := mdlBoneGlobalRotationGekko(geometry.Info.Bones, attachment.Bone)
		origin := HammerToGekko(attachment.Origin)
		position := rotation.Rotate(mgl32.Vec3{origin.X, origin.Y, origin.Z})
		asset.Markers = append(asset.Markers, content.AssetMarkerDef{
			ID:       fmt.Sprintf("mdl_attachment_%02d", index),
			Name:     nonEmptyString(attachment.Name, fmt.Sprintf("attachment %d", index)),
			Kind:     content.AssetMarkerKindEffectAnchor,
			ParentID: boneIDs[attachment.Bone],
			Transform: content.AssetTransformDef{
				Position: content.Vec3{position.X(), position.Y(), position.Z()},
				Rotation: mglQuatToContent(rotation),
				Scale:    content.Vec3{1, 1, 1},
			},
			Tags: []string{"source:hl1", fmt.Sprintf("source:hl1_attachment:%d", index)},
		})
	}
	clips := mdlAnimationClips(geometry.Info.Sequences, geometry.Info.Bones, bindTargets, opts.LockRootMotion)
	if len(clips) > 0 {
		asset.Tags = append(asset.Tags, "animation:bind_pose_clip")
	}
	return asset, clips, voxelCount, nil
}

func mdlAssetSkeleton(bones []MDLBoneInfo) *content.AssetSkeletonDef {
	if len(bones) == 0 {
		return nil
	}
	boneIDs := mdlAssetBoneIDs(bones)
	jointIDs := mdlAssetJointIDs(bones)
	out := &content.AssetSkeletonDef{Bones: make([]content.AssetBoneDef, 0, len(bones))}
	for i, bone := range bones {
		pos := HammerToGekko(bone.Position)
		parentID := ""
		if bone.Parent >= 0 && bone.Parent < len(boneIDs) && bone.Parent != i {
			parentID = boneIDs[bone.Parent]
		}
		out.Bones = append(out.Bones, content.AssetBoneDef{
			ID:       boneIDs[i],
			JointID:  jointIDs[i],
			Name:     nonEmptyString(bone.Name, fmt.Sprintf("bone_%02d", i)),
			ParentID: parentID,
			Transform: content.AssetTransformDef{
				Position: content.Vec3{pos.X, pos.Y, pos.Z},
				Rotation: hammerQuatToContentQuat(mdlEulerXYZQuat(bone.Rotation)),
				Scale:    content.Vec3{1, 1, 1},
			},
			Tags: []string{"source:hl1", "source_asset:mdl", fmt.Sprintf("bone_index:%d", i)},
		})
	}
	return out
}

func mdlAssetJointIDs(bones []MDLBoneInfo) []string {
	ids := make([]string, len(bones))
	seen := map[string]int{}
	for i, bone := range bones {
		id := mdlSemanticJointID(bone.Name)
		if id == "" {
			id = fmt.Sprintf("bone.%02d", i)
		}
		if seen[id] > 0 && bone.Parent >= 0 && bone.Parent < i {
			id = ids[bone.Parent] + "." + id
		}
		seen[id]++
		if seen[id] > 1 {
			id = fmt.Sprintf("%s.%d", id, seen[id])
		}
		ids[i] = id
	}
	return ids
}

func mdlSemanticJointID(name string) string {
	key := strings.ToLower(strings.Join(strings.Fields(name), " "))
	if strings.HasPrefix(key, "xbow biped ") {
		key = "bip01 " + strings.TrimPrefix(key, "xbow biped ")
	}
	aliases := map[string]string{
		"bip01 l leg": "bip01.left.thigh", "bip01 l thigh": "bip01.left.thigh",
		"bip01 l leg1": "bip01.left.calf", "bip01 l calf": "bip01.left.calf",
		"bip01 r leg": "bip01.right.thigh", "bip01 r thigh": "bip01.right.thigh",
		"bip01 r leg1": "bip01.right.calf", "bip01 r calf": "bip01.right.calf",
		"bip01 l arm": "bip01.left.clavicle", "bip01 l clavicle": "bip01.left.clavicle",
		"bip01 l arm1": "bip01.left.upper_arm", "bip01 l upperarm": "bip01.left.upper_arm",
		"bip01 l arm2": "bip01.left.forearm", "bip01 l forearm": "bip01.left.forearm",
		"bip01 r arm": "bip01.right.clavicle", "bip01 r clavicle": "bip01.right.clavicle",
		"bip01 r arm1": "bip01.right.upper_arm", "bip01 r upperarm": "bip01.right.upper_arm",
		"bip01 r arm2": "bip01.right.forearm", "bip01 r forearm": "bip01.right.forearm",
	}
	if alias := aliases[key]; alias != "" {
		return alias
	}
	return strings.ReplaceAll(safeMDLAssetID(key), "_", ".")
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

func mdlAnimationClips(sequences []MDLSequenceInfo, bones []MDLBoneInfo, targets []mdlAnimationBindTarget, lockRootMotion bool) []content.AssetAnimationClipDef {
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
		if (len(seq.BoneAnimations) > 0 || len(seq.BlendAnimations) > 0) && len(bones) > 0 {
			clip, ok = mdlDecodedAnimationClip(seq, bones, targets, lockRootMotion)
		}
		if seq.NumBlends > 1 && !ok {
			continue
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
		ID:             id,
		Name:           name,
		FPS:            fps,
		Duration:       duration,
		Loop:           seq.Loop,
		ActivityWeight: &seq.ActivityWeight,
		Events:         mdlAnimationEvents(seq, fps),
		Tracks:         tracks,
		Tags:           mdlAnimationTags(seq, "generated:bind_pose_clip"),
	}, true
}

func mdlDecodedAnimationClip(seq MDLSequenceInfo, bones []MDLBoneInfo, targets []mdlAnimationBindTarget, lockRootMotion bool) (content.AssetAnimationClipDef, bool) {
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

	clip := content.AssetAnimationClipDef{
		ID: id, Name: name, FPS: fps, Duration: duration, Loop: seq.Loop, ActivityWeight: &seq.ActivityWeight,
		Events: mdlAnimationEvents(seq, fps), Tags: mdlAnimationTags(seq, "generated:sequence_clip"),
	}
	if len(seq.BlendAnimations) > 0 {
		if mdlUnsupportedBlendReason(seq) != "" || len(seq.BlendAnimations) != 2 {
			return content.AssetAnimationClipDef{}, false
		}
		values := []float32{seq.BlendStart[0], seq.BlendEnd[0]}
		animations := seq.BlendAnimations
		if values[1] < values[0] {
			values[0], values[1] = values[1], values[0]
			animations = [][]MDLBoneAnimationInfo{animations[1], animations[0]}
		}
		blend := &content.AssetAnimationBlend1DDef{Parameter: mdlBlendParameter(seq.BlendType[0]), Default: max(values[0], min(values[1], 0))}
		for index, animation := range animations {
			tracks, ok := mdlDecodedAnimationTracks(seq, bones, targets, animation, duration, fps, lockRootMotion)
			if !ok {
				return content.AssetAnimationClipDef{}, false
			}
			blend.Samples = append(blend.Samples, content.AssetAnimationBlendSampleDef{Value: values[index], Tracks: tracks})
		}
		clip.Blend1D = blend
		return clip, true
	}
	tracks, ok := mdlDecodedAnimationTracks(seq, bones, targets, seq.BoneAnimations, duration, fps, lockRootMotion)
	if !ok {
		return content.AssetAnimationClipDef{}, false
	}
	clip.Tracks = tracks
	return clip, true
}

func mdlDecodedAnimationTracks(seq MDLSequenceInfo, bones []MDLBoneInfo, targets []mdlAnimationBindTarget, animations []MDLBoneAnimationInfo, duration, fps float32, lockRootMotion bool) ([]content.AssetAnimationTrackDef, bool) {
	bindFrames := mdlGlobalBoneFrameTransforms(bones, nil, 0)
	if len(bindFrames) != len(bones) {
		return nil, false
	}
	tracks := make([]content.AssetAnimationTrackDef, 0, len(targets))
	rootBone := mdlRootBoneIndex(bones)
	for _, target := range targets {
		if target.BoneIndex < 0 || target.BoneIndex >= len(bones) {
			tracks = append(tracks, mdlBindPoseTrack(target, duration))
			continue
		}
		positionKeys := make([]content.AssetVec3KeyDef, 0, seq.FrameCount)
		rotationKeys := make([]content.AssetQuatKeyDef, 0, seq.FrameCount)
		scaleKeys := make([]content.AssetVec3KeyDef, 0, 1)
		for frame := 0; frame < seq.FrameCount; frame++ {
			frameTransforms := mdlGlobalBoneFrameTransforms(bones, animations, frame)
			if target.BoneIndex >= len(frameTransforms) {
				return nil, false
			}
			position, rotation := mdlLocalAnimationTransform(target.BoneIndex, rootBone, lockRootMotion, bindFrames, frameTransforms, bones)
			t := float32(frame) / fps
			positionKeys = append(positionKeys, content.AssetVec3KeyDef{Time: t, Value: position})
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
	return tracks, len(tracks) > 0
}

func mdlUnsupportedBlendReason(seq MDLSequenceInfo) string {
	if seq.NumBlends <= 1 {
		return ""
	}
	if seq.NumBlends != 2 || seq.BlendType[1] != 0 {
		return fmt.Sprintf("%d-sample two-dimensional blend grid unsupported", seq.NumBlends)
	}
	if mdlBlendParameter(seq.BlendType[0]) == "" || seq.BlendStart[0] == seq.BlendEnd[0] {
		return "one-dimensional blend has no usable parameter range"
	}
	return ""
}

func mdlBlendParameter(blendType int) string {
	return map[int]string{1: "x", 2: "y", 4: "z", 8: "pitch", 16: "yaw", 32: "roll"}[blendType]
}

func mdlAnimationEvents(seq MDLSequenceInfo, fps float32) []content.AssetAnimationEventDef {
	if len(seq.Events) == 0 {
		return nil
	}
	if fps <= 0 {
		fps = 30
	}
	events := make([]content.AssetAnimationEventDef, 0, len(seq.Events))
	for _, event := range seq.Events {
		if event.Frame < 0 {
			continue
		}
		events = append(events, content.AssetAnimationEventDef{Frame: event.Frame, Time: float32(event.Frame) / fps, ID: event.ID, Type: event.Type, Options: event.Options})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Frame < events[j].Frame })
	return events
}

func mdlAnimationTags(seq MDLSequenceInfo, kind string) []string {
	return []string{"source:hl1", "source_asset:mdl", kind, fmt.Sprintf("source:hl1_activity:%d", seq.Activity)}
}

// mdlLocalAnimationTransform converts source global frames into local part
// transforms. Rigid parts are parented by bone, so world-space tracks would
// otherwise apply every ancestor transform twice.
func mdlLocalAnimationTransform(boneIndex, rootBone int, lockRootMotion bool, bindFrames, frameTransforms []mdlBoneFrameTransform, bones []MDLBoneInfo) (content.Vec3, content.Quat) {
	frame := frameTransforms[boneIndex]
	if lockRootMotion && rootBone >= 0 && rootBone < len(frameTransforms) {
		frame.Position = addVec3(frame.Position, subVec3(bindFrames[rootBone].Position, frameTransforms[rootBone].Position))
	}
	delta := hammerQuatToMgl(frame.Rotation.Mul(bindFrames[boneIndex].Rotation.Inverse()).Normalize())
	position := HammerToGekko(frame.Position)
	if boneIndex >= len(bones) {
		return content.Vec3{position.X, position.Y, position.Z}, mglQuatToContent(delta)
	}
	parentIndex := bones[boneIndex].Parent
	if parentIndex < 0 || parentIndex >= len(frameTransforms) || parentIndex == boneIndex {
		return content.Vec3{position.X, position.Y, position.Z}, mglQuatToContent(delta)
	}
	parentFrame := frameTransforms[parentIndex]
	if lockRootMotion && rootBone >= 0 && rootBone < len(frameTransforms) {
		parentFrame.Position = addVec3(parentFrame.Position, subVec3(bindFrames[rootBone].Position, frameTransforms[rootBone].Position))
	}
	parentDelta := hammerQuatToMgl(parentFrame.Rotation.Mul(bindFrames[parentIndex].Rotation.Inverse()).Normalize())
	parentPosition := HammerToGekko(parentFrame.Position)
	localPosition := parentDelta.Inverse().Rotate(mgl32.Vec3{position.X, position.Y, position.Z}.Sub(mgl32.Vec3{parentPosition.X, parentPosition.Y, parentPosition.Z}))
	localRotation := parentDelta.Inverse().Mul(delta).Normalize()
	return content.Vec3{localPosition.X(), localPosition.Y(), localPosition.Z()}, mglQuatToContent(localRotation)
}

func hammerQuatToMgl(q mgl32.Quat) mgl32.Quat {
	value := hammerQuatToContentQuat(q)
	return mgl32.Quat{V: mgl32.Vec3{value[0], value[1], value[2]}, W: value[3]}.Normalize()
}

func mglQuatToContent(q mgl32.Quat) content.Quat {
	return content.Quat{q.V.X(), q.V.Y(), q.V.Z(), q.W}
}

// hl1BoneLocalMuzzleFrame converts the source-up axis into the local frame of
// a static p_ model rebased to boneIndex. Its -Z axis remains the imported
// +X barrel direction; only the roll comes from the verified source rig.
func hl1BoneLocalMuzzleFrame(bones []MDLBoneInfo, boneIndex int) content.Quat {
	if boneIndex < 0 || boneIndex >= len(bones) {
		return content.Quat{}
	}
	frames := mdlGlobalBoneFrameTransforms(bones, nil, 0)
	if boneIndex >= len(frames) {
		return content.Quat{}
	}
	boneRotation := hammerQuatToMgl(frames[boneIndex].Rotation)
	forward := mgl32.Vec3{1, 0, 0}
	up := boneRotation.Inverse().Rotate(mgl32.Vec3{0, 1, 0})
	up = up.Sub(forward.Mul(forward.Dot(up)))
	if up.LenSqr() <= 1e-8 {
		return content.Quat{}
	}
	up = up.Normalize()
	right := forward.Cross(up).Normalize()
	rotation := mgl32.Mat4ToQuat(mgl32.Mat4FromCols(
		mgl32.Vec4{right.X(), right.Y(), right.Z(), 0},
		mgl32.Vec4{up.X(), up.Y(), up.Z(), 0},
		mgl32.Vec4{-forward.X(), -forward.Y(), -forward.Z(), 0},
		mgl32.Vec4{0, 0, 0, 1},
	)).Normalize()
	return mglQuatToContent(rotation)
}

func mdlRootBoneIndex(bones []MDLBoneInfo) int {
	for index, bone := range bones {
		if bone.Parent < 0 || bone.Parent >= len(bones) {
			return index
		}
	}
	return -1
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
	duration := float32(seq.FrameCount-1) / fps
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
	return voxelizeMDLGeometryWithProfile(geometry, resolution, DefaultMDLVoxelizationProfile())
}

func voxelizeMDLGeometryToBudget(geometry MDLGeometry, resolution float32, profile MDLVoxelizationProfile) (map[[3]int]mdlVoxelSample, float32) {
	return voxelizeMDLGeometryToBudgetInFrame(geometry, resolution, profile, mgl32.QuatIdent())
}

func voxelizeMDLGeometryToBudgetInFrame(geometry MDLGeometry, resolution float32, profile MDLVoxelizationProfile, frame mgl32.Quat) (map[[3]int]mdlVoxelSample, float32) {
	for attempt := 0; ; attempt++ {
		voxels := voxelizeMDLGeometryWithProfileInFrame(geometry, resolution, profile, frame)
		if profile.FillClosedInterior {
			boundsCells := mdlVoxelBoundsCellCount(voxels)
			if profile.MaxInteriorSampleCells > 0 && boundsCells > int64(profile.MaxInteriorSampleCells) {
				limitProfile := profile
				limitProfile.TargetMaxVoxelCount = profile.MaxInteriorSampleCells
				if next, retry := nextMDLVoxelResolution(resolution, boundsCells, limitProfile, attempt); retry {
					resolution = next
					continue
				}
			}
			fillMDLSurfaceClosedInterior(voxels)
		}
		next, retry := nextMDLVoxelResolution(resolution, int64(len(voxels)), profile, attempt)
		if !retry {
			return voxels, resolution
		}
		resolution = next
	}
}

func voxelizeMDLGeometryWithProfile(geometry MDLGeometry, resolution float32, profile MDLVoxelizationProfile) map[[3]int]mdlVoxelSample {
	return voxelizeMDLGeometryWithProfileInFrame(geometry, resolution, profile, mgl32.QuatIdent())
}

func voxelizeMDLGeometryWithProfileInFrame(geometry MDLGeometry, resolution float32, profile MDLVoxelizationProfile, frame mgl32.Quat) map[[3]int]mdlVoxelSample {
	out := map[[3]int]mdlVoxelSample{}
	half := importcommon.Vec3{X: resolution * 0.5, Y: resolution * 0.5, Z: resolution * 0.5}
	for _, tri := range geometry.Triangles {
		triWorld := [3]importcommon.Vec3{
			mdlVoxelFramePoint(frame, tri.Vertices[0].Position),
			mdlVoxelFramePoint(frame, tri.Vertices[1].Position),
			mdlVoxelFramePoint(frame, tri.Vertices[2].Position),
		}
		minB, maxB := triangleVoxelBounds(triWorld, resolution)
		for x := minB[0]; x <= maxB[0]; x++ {
			for y := minB[1]; y <= maxB[1]; y++ {
				for z := minB[2]; z <= maxB[2]; z++ {
					key := [3]int{x, y, z}
					if !triangleIntersectsVoxel(triWorld, key, half, resolution) {
						continue
					}
					color := sampleMDLTriangleVoxelColor(geometry, tri, triWorld, key, resolution, profile)
					if color[3] == 0 {
						continue
					}
					out[key] = mdlVoxelSampleForTriangle(geometry, tri, color)
				}
			}
		}
	}
	return out
}

func mdlVoxelFramePoint(frame mgl32.Quat, position importcommon.Vec3) importcommon.Vec3 {
	position = HammerToGekko(position)
	rotated := frame.Rotate(mgl32.Vec3{position.X, position.Y, position.Z})
	return importcommon.Vec3{X: rotated.X(), Y: rotated.Y(), Z: rotated.Z()}
}

func voxelizeMDLGeometryByBone(geometry MDLGeometry, resolution float32) map[int]map[[3]int]mdlVoxelSample {
	return voxelizeMDLGeometryByBoneWithProfile(geometry, resolution, DefaultMDLVoxelizationProfile())
}

func voxelizeMDLGeometryByBoneToBudget(geometry MDLGeometry, resolution float32, profile MDLVoxelizationProfile) (map[int]map[[3]int]mdlVoxelSample, float32) {
	for attempt := 0; ; attempt++ {
		boneVoxels := voxelizeMDLGeometryByBoneWithProfile(geometry, resolution, profile)
		if profile.FillClosedInterior {
			bindPoseVoxels := voxelizeMDLGeometryByBoneInBindPoseWithProfile(geometry, resolution, profile)
			boundsCells := mdlBoneVoxelBoundsCellCount(bindPoseVoxels)
			if profile.MaxInteriorSampleCells > 0 && boundsCells > int64(profile.MaxInteriorSampleCells) {
				limitProfile := profile
				limitProfile.TargetMaxVoxelCount = profile.MaxInteriorSampleCells
				next, retry := nextMDLVoxelResolution(resolution, boundsCells, limitProfile, attempt)
				if retry {
					resolution = next
					continue
				}
			}
			interior := fillMDLClosedInterior(bindPoseVoxels)
			if profile.PartitionBySkeletonSegments {
				partitionMDLVoxelsBySkeleton(bindPoseVoxels, geometry.Info.Bones, interior, resolution)
			}
			if profile.JointCapVoxels > 0 {
				applyMDLInteriorJointCaps(bindPoseVoxels, geometry.Info.Bones, interior, profile.JointCapVoxels)
			}
			fillMDLBoneLocalInteriors(boneVoxels, bindPoseVoxels, geometry.Info.Bones, interior, resolution)
		}
		next, retry := nextMDLVoxelResolution(resolution, mdlBoneVoxelCount(boneVoxels), profile, attempt)
		if !retry {
			return boneVoxels, resolution
		}
		resolution = next
	}
}

func voxelizeMDLGeometryByBoneWithProfile(geometry MDLGeometry, resolution float32, profile MDLVoxelizationProfile) map[int]map[[3]int]mdlVoxelSample {
	if len(geometry.Info.Bones) == 0 {
		return nil
	}
	out := map[int]map[[3]int]mdlVoxelSample{}
	boneFrames := mdlGlobalBoneBindFramesGekko(geometry.Info.Bones)
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
		for _, boneIndex := range mdlTriangleBoneIndices(tri, len(geometry.Info.Bones), fallbackBoneIndex) {
			frame := boneFrames[boneIndex]
			triLocal := mdlTriangleInBoneLocalFrame(triWorld, frame)
			minB, maxB := triangleVoxelBounds(triLocal, resolution)
			for x := minB[0]; x <= maxB[0]; x++ {
				for y := minB[1]; y <= maxB[1]; y++ {
					for z := minB[2]; z <= maxB[2]; z++ {
						key := [3]int{x, y, z}
						if !triangleIntersectsVoxel(triLocal, key, half, resolution) {
							continue
						}
						localCenter := voxelCenter(key, resolution)
						worldCenter := mdlBoneLocalPointToWorld(localCenter, frame)
						owner, _ := mdlTriangleBoneOwnershipAtPoint(tri, triWorld, worldCenter, len(geometry.Info.Bones), fallbackBoneIndex)
						if owner != boneIndex {
							continue
						}
						color := sampleMDLTriangleVoxelColor(geometry, tri, triLocal, key, resolution, profile)
						if color[3] == 0 {
							continue
						}
						if out[boneIndex] == nil {
							out[boneIndex] = map[[3]int]mdlVoxelSample{}
						}
						out[boneIndex][key] = mdlVoxelSampleForTriangle(geometry, tri, color)
					}
				}
			}
		}
	}
	return out
}

// voxelizeMDLGeometryByBoneInBindPoseWithProfile records surface ownership on
// one shared lattice. It is used only to classify safe interior fill; rendered
// surfaces stay on their artifact-free bone-local lattices.
func voxelizeMDLGeometryByBoneInBindPoseWithProfile(geometry MDLGeometry, resolution float32, profile MDLVoxelizationProfile) map[int]map[[3]int]mdlVoxelSample {
	out := map[int]map[[3]int]mdlVoxelSample{}
	owners := map[[3]int]int{}
	weights := map[[3]int]float32{}
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
					color := sampleMDLTriangleVoxelColor(geometry, tri, triWorld, key, resolution, profile)
					if color[3] == 0 {
						continue
					}
					owner, weight := mdlTriangleBoneOwnershipAtPoint(tri, triWorld, voxelCenter(key, resolution), len(geometry.Info.Bones), fallbackBoneIndex)
					previous, claimed := owners[key]
					if claimed && (weights[key] > weight || weights[key] == weight && previous <= owner) {
						continue
					}
					if claimed {
						delete(out[previous], key)
					}
					if out[owner] == nil {
						out[owner] = map[[3]int]mdlVoxelSample{}
					}
					out[owner][key] = mdlVoxelSampleForTriangle(geometry, tri, color)
					owners[key], weights[key] = owner, weight
				}
			}
		}
	}
	return out
}

func fillMDLBoneLocalInteriors(localVoxels, bindPoseVoxels map[int]map[[3]int]mdlVoxelSample, bones []MDLBoneInfo, interior map[[3]int]struct{}, resolution float32) {
	if len(interior) == 0 || resolution <= 0 {
		return
	}
	frames := mdlGlobalBoneBindFramesGekko(bones)
	solid := make(map[[3]int]struct{})
	for _, voxels := range bindPoseVoxels {
		for key := range voxels {
			solid[key] = struct{}{}
		}
	}
	for boneIndex, voxels := range bindPoseVoxels {
		if boneIndex < 0 || boneIndex >= len(frames) {
			continue
		}
		localSurface := localVoxels[boneIndex]
		if len(localSurface) == 0 {
			continue
		}
		first := true
		var minLocal, maxLocal [3]int
		for key := range localSurface {
			if first {
				minLocal, maxLocal, first = key, key, false
				continue
			}
			for axis := range 3 {
				minLocal[axis] = min(minLocal[axis], key[axis])
				maxLocal[axis] = max(maxLocal[axis], key[axis])
			}
		}
		frame := frames[boneIndex]
		candidates := make(map[[3]int]struct{})
		for worldKey := range voxels {
			if _, inside := interior[worldKey]; !inside {
				continue
			}
			localCenter := mdlBoneWorldPointToLocal(voxelCenter(worldKey, resolution), frame)
			base := keyForPosition(localCenter, resolution)
			for x := base[0] - 1; x <= base[0]+1; x++ {
				for y := base[1] - 1; y <= base[1]+1; y++ {
					for z := base[2] - 1; z <= base[2]+1; z++ {
						candidates[[3]int{x, y, z}] = struct{}{}
					}
				}
			}
		}
		for localKey := range candidates {
			// A filled rigid part must not outgrow its imported surface bounds;
			// otherwise the added volume becomes a protrusion when the bone moves.
			if localKey[0] < minLocal[0] || localKey[0] > maxLocal[0] ||
				localKey[1] < minLocal[1] || localKey[1] > maxLocal[1] ||
				localKey[2] < minLocal[2] || localKey[2] > maxLocal[2] {
				continue
			}
			if _, surface := localSurface[localKey]; surface {
				continue
			}
			worldKey := keyForPosition(mdlBoneLocalPointToWorld(voxelCenter(localKey, resolution), frame), resolution)
			sample, owned := voxels[worldKey]
			if _, inside := interior[worldKey]; !inside || !owned || !mdlBoneLocalVoxelInsideMask(localKey, frame, resolution, solid) {
				continue
			}
			localSurface[localKey] = sample
		}
	}
}

func mdlBoneLocalVoxelInsideMask(key [3]int, frame mdlBoneVoxelFrame, resolution float32, solid map[[3]int]struct{}) bool {
	center := voxelCenter(key, resolution)
	offset := resolution * 0.49
	for _, x := range []float32{-offset, offset} {
		for _, y := range []float32{-offset, offset} {
			for _, z := range []float32{-offset, offset} {
				point := mdlBoneLocalPointToWorld(addVec3(center, importcommon.Vec3{X: x, Y: y, Z: z}), frame)
				if _, inside := solid[keyForPosition(point, resolution)]; !inside {
					return false
				}
			}
		}
	}
	return true
}

type mdlBoneVoxelFrame struct {
	Position importcommon.Vec3
	Rotation mgl32.Quat
}

func mdlGlobalBoneBindFramesGekko(bones []MDLBoneInfo) []mdlBoneVoxelFrame {
	frames := mdlGlobalBoneFrameTransforms(bones, nil, 0)
	out := make([]mdlBoneVoxelFrame, len(frames))
	for index, frame := range frames {
		out[index] = mdlBoneVoxelFrame{Position: HammerToGekko(frame.Position), Rotation: hammerQuatToMgl(frame.Rotation)}
	}
	return out
}

func mdlTriangleBoneIndices(tri MDLTriangle, boneCount, fallback int) []int {
	seen := make(map[int]struct{}, len(tri.Vertices))
	for _, vertex := range tri.Vertices {
		if vertex.BoneIndex >= 0 && vertex.BoneIndex < boneCount {
			seen[vertex.BoneIndex] = struct{}{}
		}
	}
	if len(seen) == 0 && fallback >= 0 && fallback < boneCount {
		seen[fallback] = struct{}{}
	}
	out := make([]int, 0, len(seen))
	for boneIndex := range seen {
		out = append(out, boneIndex)
	}
	sort.Ints(out)
	return out
}

func mdlTriangleInBoneLocalFrame(tri [3]importcommon.Vec3, frame mdlBoneVoxelFrame) [3]importcommon.Vec3 {
	return [3]importcommon.Vec3{
		mdlBoneWorldPointToLocal(tri[0], frame),
		mdlBoneWorldPointToLocal(tri[1], frame),
		mdlBoneWorldPointToLocal(tri[2], frame),
	}
}

func mdlBoneWorldPointToLocal(point importcommon.Vec3, frame mdlBoneVoxelFrame) importcommon.Vec3 {
	local := frame.Rotation.Inverse().Rotate(mgl32.Vec3{point.X - frame.Position.X, point.Y - frame.Position.Y, point.Z - frame.Position.Z})
	return importcommon.Vec3{X: local.X(), Y: local.Y(), Z: local.Z()}
}

func mdlBoneLocalPointToWorld(point importcommon.Vec3, frame mdlBoneVoxelFrame) importcommon.Vec3 {
	world := frame.Rotation.Rotate(mgl32.Vec3{point.X, point.Y, point.Z})
	return importcommon.Vec3{X: world.X() + frame.Position.X, Y: world.Y() + frame.Position.Y, Z: world.Z() + frame.Position.Z}
}

func nextMDLVoxelResolution(resolution float32, voxelCount int64, profile MDLVoxelizationProfile, attempt int) (float32, bool) {
	if profile.TargetMaxVoxelCount <= 0 || voxelCount <= int64(profile.TargetMaxVoxelCount) || attempt >= 3 {
		return resolution, false
	}
	limit := profile.CoarsestResolution
	if limit <= resolution {
		return resolution, false
	}
	ratio := float64(voxelCount) / float64(profile.TargetMaxVoxelCount)
	scale := float32(math.Sqrt(ratio))
	if profile.FillClosedInterior {
		scale = float32(math.Cbrt(ratio))
	}
	if scale < 1.1 {
		scale = 1.1
	}
	next := minFloat32(limit, resolution*scale*1.02)
	return next, next > resolution*1.001
}

func mdlBoneVoxelCount(boneVoxels map[int]map[[3]int]mdlVoxelSample) int64 {
	count := int64(0)
	for _, voxels := range boneVoxels {
		count += int64(len(voxels))
	}
	return count
}

func mdlBoneVoxelBoundsCellCount(boneVoxels map[int]map[[3]int]mdlVoxelSample) int64 {
	first := true
	var minKey, maxKey [3]int
	for _, voxels := range boneVoxels {
		for key := range voxels {
			if first {
				minKey, maxKey, first = key, key, false
				continue
			}
			for axis := range 3 {
				minKey[axis] = min(minKey[axis], key[axis])
				maxKey[axis] = max(maxKey[axis], key[axis])
			}
		}
	}
	if first {
		return 0
	}
	return int64(maxKey[0]-minKey[0]+1) * int64(maxKey[1]-minKey[1]+1) * int64(maxKey[2]-minKey[2]+1)
}

func mdlBoneLocalVoxelBoundsCellCount(boneVoxels map[int]map[[3]int]mdlVoxelSample) int64 {
	var total int64
	for _, voxels := range boneVoxels {
		total += mdlBoneVoxelBoundsCellCount(map[int]map[[3]int]mdlVoxelSample{0: voxels})
	}
	return total
}

func mdlVoxelBoundsCellCount(voxels map[[3]int]mdlVoxelSample) int64 {
	return mdlBoneVoxelBoundsCellCount(map[int]map[[3]int]mdlVoxelSample{0: voxels})
}

func fillMDLSurfaceClosedInterior(voxels map[[3]int]mdlVoxelSample) {
	fillMDLClosedInterior(map[int]map[[3]int]mdlVoxelSample{0: voxels})
}

func fillMDLClosedInterior(boneVoxels map[int]map[[3]int]mdlVoxelSample) map[[3]int]struct{} {
	solid := make(map[[3]int]importcommon.Voxel)
	surfaceKeys := make(map[[3]int]struct{})
	owners := make(map[[3]int]int)
	samples := make(map[[3]int]mdlVoxelSample)
	for _, boneIndex := range sortedMDLBoneVoxelIndices(boneVoxels) {
		for _, key := range sortedMDLVoxelKeys(boneVoxels[boneIndex]) {
			solid[key] = importcommon.Voxel{X: key[0], Y: key[1], Z: key[2], Palette: 1}
			surfaceKeys[key] = struct{}{}
			if _, exists := owners[key]; !exists {
				owners[key] = boneIndex
				samples[key] = boneVoxels[boneIndex][key]
			}
		}
	}
	if len(solid) == 0 {
		return nil
	}
	surfaceCount := len(solid)
	fillClosedInterior(solid)
	if len(solid) == surfaceCount {
		return nil
	}

	queue := sortedMDLVoxelKeys(samples)
	directions := [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	for head := 0; head < len(queue); head++ {
		key := queue[head]
		for _, direction := range directions {
			next := [3]int{key[0] + direction[0], key[1] + direction[1], key[2] + direction[2]}
			if _, occupied := solid[next]; !occupied {
				continue
			}
			if _, assigned := owners[next]; assigned {
				continue
			}
			owners[next] = owners[key]
			samples[next] = samples[key]
			queue = append(queue, next)
		}
	}

	interior := make(map[[3]int]struct{}, len(solid)-surfaceCount)
	for _, key := range sortedVoxelKeys(solid) {
		if _, surface := surfaceKeys[key]; surface {
			continue
		}
		boneIndex, assigned := owners[key]
		if !assigned {
			continue
		}
		if boneVoxels[boneIndex] == nil {
			boneVoxels[boneIndex] = map[[3]int]mdlVoxelSample{}
		}
		boneVoxels[boneIndex][key] = samples[key]
		interior[key] = struct{}{}
	}
	return interior
}

// partitionMDLVoxelsBySkeleton cuts the unified bind-pose volume along nearby
// source-fitted skeleton capsules. Only skin-active bones can own voxels, so
// control joints do not acquire visible chunks. Source skinning remains a
// surface-only bias, so branch hips and bent ankles use the same rule while
// flood-filled interiors cannot preserve accidental propagation seams.
func partitionMDLVoxelsBySkeleton(boneVoxels map[int]map[[3]int]mdlVoxelSample, bones []MDLBoneInfo, interior map[[3]int]struct{}, resolution float32) int {
	if resolution <= 0 || len(bones) == 0 {
		return 0
	}
	children := make([][]int, len(bones))
	for boneIndex, bone := range bones {
		if bone.Parent >= 0 && bone.Parent < len(bones) && bone.Parent != boneIndex {
			children[bone.Parent] = append(children[bone.Parent], boneIndex)
		}
	}
	origins := make([]importcommon.Vec3, len(bones))
	active := make([]bool, len(bones))
	for boneIndex := range bones {
		origins[boneIndex] = mdlBoneGlobalOriginGekko(bones, boneIndex)
		active[boneIndex] = len(boneVoxels[boneIndex]) > 0
	}
	activeParent := make([]int, len(bones))
	activeChildren := make([][]int, len(bones))
	for boneIndex := range activeParent {
		activeParent[boneIndex] = -1
	}
	for boneIndex := range bones {
		if !active[boneIndex] {
			continue
		}
		parent := bones[boneIndex].Parent
		for depth := 0; parent >= 0 && parent < len(bones) && depth < len(bones); depth++ {
			if active[parent] {
				activeParent[boneIndex] = parent
				activeChildren[parent] = append(activeChildren[parent], boneIndex)
				break
			}
			parent = bones[parent].Parent
		}
	}
	radiusSquared := mdlBoneCapsuleRadiiSquared(boneVoxels, bones, origins, children, active, interior, resolution)
	repartitioned := make(map[int]map[[3]int]mdlVoxelSample)
	moved := 0
	bias := resolution * resolution
	for _, sourceBone := range sortedMDLBoneVoxelIndices(boneVoxels) {
		if sourceBone < 0 || sourceBone >= len(bones) {
			continue
		}
		eligible := make([]bool, len(bones))
		eligible[sourceBone] = true
		parent := activeParent[sourceBone]
		if parent >= 0 {
			eligible[parent] = true
		}
		for _, child := range activeChildren[sourceBone] {
			eligible[child] = true
		}
		for _, key := range sortedMDLVoxelKeys(boneVoxels[sourceBone]) {
			point := voxelCenter(key, resolution)
			owner := sourceBone
			bestScore := float32(math.MaxFloat32)
			_, isInterior := interior[key]
			for candidate := range bones {
				if !eligible[candidate] {
					continue
				}
				score := mdlBoneSegmentDistanceSquared(point, candidate, bones, origins, children, active) / radiusSquared[candidate]
				if candidate == sourceBone && !isInterior {
					score -= min(1, bias/radiusSquared[candidate])
				}
				if score < bestScore || (score == bestScore && candidate < owner) {
					owner, bestScore = candidate, score
				}
			}
			if repartitioned[owner] == nil {
				repartitioned[owner] = map[[3]int]mdlVoxelSample{}
			}
			repartitioned[owner][key] = boneVoxels[sourceBone][key]
			if owner != sourceBone {
				moved++
			}
		}
	}
	moved += planarizeMDLLinearJointCuts(repartitioned, origins, children, activeParent, radiusSquared, resolution)
	clear(boneVoxels)
	for boneIndex, voxels := range repartitioned {
		boneVoxels[boneIndex] = voxels
	}
	return moved
}

// planarizeMDLLinearJointCuts replaces capsule-distance seams near simple
// chain joints with a plane through the child pivot, perpendicular to the
// incoming bone. This gives rigid elbows and knees flat mating faces.
func planarizeMDLLinearJointCuts(boneVoxels map[int]map[[3]int]mdlVoxelSample, origins []importcommon.Vec3, children [][]int, activeParent []int, radiusSquared []float32, resolution float32) int {
	moved := 0
	for child, parent := range activeParent {
		if parent < 0 || len(children[parent]) != 1 || len(boneVoxels[parent]) == 0 || len(boneVoxels[child]) == 0 {
			continue
		}
		axis := subVec3(origins[child], origins[parent])
		axisLengthSquared := dotVec3(axis, axis)
		if axisLengthSquared <= 1e-8 {
			continue
		}
		axisScale := 1 / float32(math.Sqrt(float64(axisLengthSquared)))
		axis = importcommon.Vec3{X: axis.X * axisScale, Y: axis.Y * axisScale, Z: axis.Z * axisScale}
		joint := origins[child]
		jointRadiusSquared := max(radiusSquared[parent], radiusSquared[child])

		toChild := make(map[[3]int]mdlVoxelSample)
		for _, key := range sortedMDLVoxelKeys(boneVoxels[parent]) {
			delta := subVec3(voxelCenter(key, resolution), joint)
			if dotVec3(delta, delta) <= jointRadiusSquared && dotVec3(delta, axis) >= 0 {
				toChild[key] = boneVoxels[parent][key]
			}
		}
		toParent := make(map[[3]int]mdlVoxelSample)
		for _, key := range sortedMDLVoxelKeys(boneVoxels[child]) {
			delta := subVec3(voxelCenter(key, resolution), joint)
			if dotVec3(delta, delta) <= jointRadiusSquared && dotVec3(delta, axis) < 0 {
				toParent[key] = boneVoxels[child][key]
			}
		}
		for key, sample := range toChild {
			delete(boneVoxels[parent], key)
			boneVoxels[child][key] = sample
			moved++
		}
		for key, sample := range toParent {
			delete(boneVoxels[child], key)
			boneVoxels[parent][key] = sample
			moved++
		}
	}
	// ponytail: branch joints keep capsule cuts; add conflict resolution only
	// if shoulders or hips visibly need competing planar cuts.
	return moved
}

func mdlBoneCapsuleRadiiSquared(boneVoxels map[int]map[[3]int]mdlVoxelSample, bones []MDLBoneInfo, origins []importcommon.Vec3, children [][]int, active []bool, interior map[[3]int]struct{}, resolution float32) []float32 {
	radii := make([]float32, len(bones))
	minimum := resolution * resolution
	for boneIndex := range bones {
		if !active[boneIndex] {
			continue
		}
		distances := make([]float32, 0, len(boneVoxels[boneIndex]))
		for _, key := range sortedMDLVoxelKeys(boneVoxels[boneIndex]) {
			if _, isInterior := interior[key]; isInterior {
				continue
			}
			distances = append(distances, mdlBoneSegmentDistanceSquared(voxelCenter(key, resolution), boneIndex, bones, origins, children, active))
		}
		if len(distances) == 0 {
			radii[boneIndex] = minimum
			continue
		}
		sort.Slice(distances, func(i, j int) bool { return distances[i] < distances[j] })
		radii[boneIndex] = max(minimum, distances[(len(distances)-1)*9/10])
	}
	return radii
}

func mdlBoneSegmentDistanceSquared(point importcommon.Vec3, boneIndex int, bones []MDLBoneInfo, origins []importcommon.Vec3, children [][]int, active []bool) float32 {
	start := origins[boneIndex]
	best := float32(math.MaxFloat32)
	for _, child := range children[boneIndex] {
		best = min(best, pointSegmentDistanceSquared(point, start, origins[child]))
	}
	parent := bones[boneIndex].Parent
	if parent >= 0 && parent < len(bones) && !active[parent] {
		ancestor := bones[parent].Parent
		for depth := 0; ancestor >= 0 && ancestor < len(bones) && depth < len(bones); depth++ {
			if active[ancestor] {
				best = min(best, pointSegmentDistanceSquared(point, origins[parent], start))
				break
			}
			ancestor = bones[ancestor].Parent
		}
	}
	if best == float32(math.MaxFloat32) {
		delta := subVec3(point, start)
		return dotVec3(delta, delta)
	}
	return best
}

func pointSegmentDistanceSquared(point, start, end importcommon.Vec3) float32 {
	segment := subVec3(end, start)
	lengthSquared := dotVec3(segment, segment)
	t := float32(0)
	if lengthSquared > 0 {
		t = max(0, min(1, dotVec3(subVec3(point, start), segment)/lengthSquared))
	}
	closest := addVec3(start, importcommon.Vec3{X: segment.X * t, Y: segment.Y * t, Z: segment.Z * t})
	delta := subVec3(point, closest)
	return dotVec3(delta, delta)
}

func applyMDLInteriorJointCaps(boneVoxels map[int]map[[3]int]mdlVoxelSample, bones []MDLBoneInfo, interior map[[3]int]struct{}, layers int) {
	directions := [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	connections := mdlBoneVoxelConnections(boneVoxels, bones)
	for layer := 0; layer < layers; layer++ {
		owners := make(map[[3]int][]int)
		for _, boneIndex := range sortedMDLBoneVoxelIndices(boneVoxels) {
			for _, key := range sortedMDLVoxelKeys(boneVoxels[boneIndex]) {
				owners[key] = append(owners[key], boneIndex)
			}
		}
		additions := make(map[int]map[[3]int]mdlVoxelSample)
		for _, boneIndex := range sortedMDLBoneVoxelIndices(boneVoxels) {
			for _, key := range sortedMDLVoxelKeys(boneVoxels[boneIndex]) {
				if _, isInterior := interior[key]; !isInterior {
					continue
				}
				sample := boneVoxels[boneIndex][key]
				for _, direction := range directions {
					neighbor := [3]int{key[0] + direction[0], key[1] + direction[1], key[2] + direction[2]}
					for _, otherBone := range owners[neighbor] {
						if _, connected := connections[[2]int{boneIndex, otherBone}]; otherBone == boneIndex || !connected {
							continue
						}
						if additions[otherBone] == nil {
							additions[otherBone] = map[[3]int]mdlVoxelSample{}
						}
						additions[otherBone][key] = sample
					}
				}
			}
		}
		if len(additions) == 0 {
			return
		}
		for boneIndex, voxels := range additions {
			for key, sample := range voxels {
				boneVoxels[boneIndex][key] = sample
			}
		}
	}
}

func mdlBoneVoxelConnections(boneVoxels map[int]map[[3]int]mdlVoxelSample, bones []MDLBoneInfo) map[[2]int]struct{} {
	connections := make(map[[2]int]struct{})
	for boneIndex := range bones {
		if len(boneVoxels[boneIndex]) == 0 {
			continue
		}
		parent := bones[boneIndex].Parent
		for depth := 0; parent >= 0 && parent < len(bones) && depth < len(bones); depth++ {
			if len(boneVoxels[parent]) > 0 {
				connections[[2]int{boneIndex, parent}] = struct{}{}
				connections[[2]int{parent, boneIndex}] = struct{}{}
				break
			}
			parent = bones[parent].Parent
		}
	}
	return connections
}

func mdlTriangleBoneAtPoint(tri MDLTriangle, triWorld [3]importcommon.Vec3, point importcommon.Vec3, boneCount int, fallback int) int {
	boneIndex, _ := mdlTriangleBoneOwnershipAtPoint(tri, triWorld, point, boneCount, fallback)
	return boneIndex
}

func mdlTriangleBoneOwnershipAtPoint(tri MDLTriangle, triWorld [3]importcommon.Vec3, point importcommon.Vec3, boneCount int, fallback int) (int, float32) {
	if !mdlTriangleHasMixedBones(tri, boneCount) {
		return fallback, 1
	}
	bary, ok := barycentricPoint(triWorld, point)
	if !ok {
		return fallback, 1
	}
	weights := make([]float32, boneCount)
	for i, vertex := range tri.Vertices {
		if vertex.BoneIndex < 0 || vertex.BoneIndex >= boneCount {
			continue
		}
		weights[vertex.BoneIndex] += bary[i]
	}
	bestBone := fallback
	bestWeight := float32(-math.MaxFloat32)
	for boneIndex, weight := range weights {
		if weight > bestWeight || (weight == bestWeight && boneIndex == fallback) {
			bestBone, bestWeight = boneIndex, weight
		}
	}
	return bestBone, bestWeight
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
	frame, ok := mdlBoneGlobalFrameGekko(bones, boneIndex)
	if !ok {
		return importcommon.Vec3{}
	}
	return frame.Position
}

func mdlBoneGlobalRotationGekko(bones []MDLBoneInfo, boneIndex int) mgl32.Quat {
	frame, ok := mdlBoneGlobalFrameGekko(bones, boneIndex)
	if !ok {
		return mgl32.QuatIdent()
	}
	return frame.Rotation
}

func mdlBoneGlobalFrameGekko(bones []MDLBoneInfo, boneIndex int) (mdlBoneVoxelFrame, bool) {
	if boneIndex < 0 || boneIndex >= len(bones) {
		return mdlBoneVoxelFrame{}, false
	}
	frames := mdlGlobalBoneBindFramesGekko(bones)
	if boneIndex >= len(frames) {
		return mdlBoneVoxelFrame{}, false
	}
	return frames[boneIndex], true
}

type mdlVoxelSample struct {
	Color        [4]uint8
	TextureName  string
	TextureFlags int
}

func mdlVoxelSampleForTriangle(geometry MDLGeometry, tri MDLTriangle, color [4]uint8) mdlVoxelSample {
	sample := mdlVoxelSample{Color: color}
	if tri.TextureIndex >= 0 && tri.TextureIndex < len(geometry.Textures) {
		sample.TextureName = geometry.Textures[tri.TextureIndex].Info.Name
		sample.TextureFlags = geometry.Textures[tri.TextureIndex].Info.Flags
	}
	return sample
}

func sampleMDLTriangleVoxelColor(geometry MDLGeometry, tri MDLTriangle, triWorld [3]importcommon.Vec3, key [3]int, resolution float32, profile MDLVoxelizationProfile) [4]uint8 {
	center := voxelCenter(key, resolution)
	probes := []importcommon.Vec3{center}
	if profile.CoverageSamples > 1 {
		offset := resolution * 0.45
		probes = append(probes,
			importcommon.Vec3{X: center.X + offset, Y: center.Y, Z: center.Z},
			importcommon.Vec3{X: center.X - offset, Y: center.Y, Z: center.Z},
			importcommon.Vec3{X: center.X, Y: center.Y + offset, Z: center.Z},
			importcommon.Vec3{X: center.X, Y: center.Y - offset, Z: center.Z},
			importcommon.Vec3{X: center.X, Y: center.Y, Z: center.Z + offset},
			importcommon.Vec3{X: center.X, Y: center.Y, Z: center.Z - offset},
		)
	}
	counts := make(map[[4]uint8]int)
	for _, probe := range probes {
		bary, ok := barycentricPoint(triWorld, probe)
		if !ok || !mdlBaryPointInsideVoxel(triWorld, bary, key, resolution) {
			continue
		}
		color := sampleMDLTriangleColorAtBary(geometry, tri, bary, profile.RespectMaskedTextures)
		if color[3] != 0 {
			counts[color]++
		}
	}
	if len(counts) == 0 {
		bary, ok := barycentricPoint(triWorld, center)
		if !ok {
			return [4]uint8{180, 180, 180, 255}
		}
		return sampleMDLTriangleColorAtBary(geometry, tri, bary, profile.RespectMaskedTextures)
	}
	var best [4]uint8
	bestCount := -1
	for color, count := range counts {
		if count > bestCount || (count == bestCount && colorKey(color) < colorKey(best)) {
			best, bestCount = color, count
		}
	}
	return best
}

func mdlBaryPointInsideVoxel(tri [3]importcommon.Vec3, bary [3]float32, key [3]int, resolution float32) bool {
	point := importcommon.Vec3{
		X: tri[0].X*bary[0] + tri[1].X*bary[1] + tri[2].X*bary[2],
		Y: tri[0].Y*bary[0] + tri[1].Y*bary[1] + tri[2].Y*bary[2],
		Z: tri[0].Z*bary[0] + tri[1].Z*bary[1] + tri[2].Z*bary[2],
	}
	epsilon := resolution * 1e-4
	return point.X >= float32(key[0])*resolution-epsilon && point.X <= float32(key[0]+1)*resolution+epsilon &&
		point.Y >= float32(key[1])*resolution-epsilon && point.Y <= float32(key[1]+1)*resolution+epsilon &&
		point.Z >= float32(key[2])*resolution-epsilon && point.Z <= float32(key[2]+1)*resolution+epsilon
}

func sampleMDLTriangleColorAtBary(geometry MDLGeometry, tri MDLTriangle, bary [3]float32, respectMaskedTextures bool) [4]uint8 {
	u := bary[0]*tri.Vertices[0].UV[0] + bary[1]*tri.Vertices[1].UV[0] + bary[2]*tri.Vertices[2].UV[0]
	v := bary[0]*tri.Vertices[0].UV[1] + bary[1]*tri.Vertices[1].UV[1] + bary[2]*tri.Vertices[2].UV[1]
	if tri.TextureIndex < 0 || tri.TextureIndex >= len(geometry.Textures) {
		return [4]uint8{180, 180, 180, 255}
	}
	texture := geometry.Textures[tri.TextureIndex]
	color, ok := sampleMDLTextureWithMask(texture, u, v, respectMaskedTextures)
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
	return sampleMDLTextureWithMask(texture, u, v, true)
}

const mdlTextureFlagMasked = 0x0040

func sampleMDLTextureWithMask(texture MDLTexturePixels, u float32, v float32, respectMaskedTextures bool) ([4]uint8, bool) {
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
	if respectMaskedTextures && texture.Info.Flags&mdlTextureFlagMasked != 0 && paletteIndex == 255 {
		return [4]uint8{}, true
	}
	color := texture.Palette[paletteIndex]
	return [4]uint8{color[0], color[1], color[2], 255}, true
}

func localizeMDLVoxels(voxels map[[3]int]mdlVoxelSample, resolution float32) ([]content.VoxelObjectVoxelDef, importcommon.Vec3) {
	return localizeMDLVoxelsWithPalette(voxels, resolution, newMDLColorPalette(voxels))
}

func localizeMDLVoxelsWithPalette(voxels map[[3]int]mdlVoxelSample, resolution float32, palette mdlColorPalette) ([]content.VoxelObjectVoxelDef, importcommon.Vec3) {
	keys := sortedMDLVoxelKeys(voxels)
	first := true
	var minK [3]int
	for _, key := range keys {
		if first {
			minK = key
			first = false
			continue
		}
		minK[0] = min(minK[0], key[0])
		minK[1] = min(minK[1], key[1])
		minK[2] = min(minK[2], key[2])
	}
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

func mdlVoxelsAtSourceOrigin(voxels map[[3]int]mdlVoxelSample, palette mdlColorPalette) []content.VoxelObjectVoxelDef {
	keys := sortedMDLVoxelKeys(voxels)
	out := make([]content.VoxelObjectVoxelDef, 0, len(keys))
	for _, key := range keys {
		out = append(out, content.VoxelObjectVoxelDef{
			X:     key[0],
			Y:     key[1],
			Z:     key[2],
			Value: palette.valueForColor(voxels[key].Color),
		})
	}
	return out
}

func sortedMDLVoxelKeys(voxels map[[3]int]mdlVoxelSample) [][3]int {
	keys := make([][3]int, 0, len(voxels))
	for key := range voxels {
		keys = append(keys, key)
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
	return keys
}

func mdlAssetMaterialsAndPalette(voxels map[[3]int]mdlVoxelSample) ([]content.AssetMaterialDef, []content.AssetVoxelPaletteEntryDef) {
	pal := newMDLColorPalette(voxels)
	materials := make([]content.AssetMaterialDef, 0, len(pal.colors))
	shapePalette := make([]content.AssetVoxelPaletteEntryDef, 0, len(pal.colors))
	for _, entry := range pal.colors {
		materialID := fmt.Sprintf("mat_%d", entry.Value)
		materials = append(materials, mdlAssetMaterialDef(materialID, entry.Color, mdlSamplesForColor(voxels, entry.Color)))
		shapePalette = append(shapePalette, content.AssetVoxelPaletteEntryDef{Value: entry.Value, MaterialID: materialID})
	}
	return materials, shapePalette
}

func mdlAssetMaterialsForPalettes(palettes map[int]mdlColorPalette, boneVoxels map[int]map[[3]int]mdlVoxelSample) []content.AssetMaterialDef {
	colors := make(map[[4]uint8]struct{})
	for _, palette := range palettes {
		for _, entry := range palette.colors {
			colors[entry.Color] = struct{}{}
		}
	}
	ordered := make([][4]uint8, 0, len(colors))
	for color := range colors {
		ordered = append(ordered, color)
	}
	sort.Slice(ordered, func(i, j int) bool { return colorKey(ordered[i]) < colorKey(ordered[j]) })
	materials := make([]content.AssetMaterialDef, 0, len(ordered))
	for _, color := range ordered {
		materialID := mdlMaterialIDForColor(color)
		var samples []mdlVoxelSample
		for _, voxels := range boneVoxels {
			samples = append(samples, mdlSamplesForColor(voxels, color)...)
		}
		materials = append(materials, mdlAssetMaterialDef(materialID, color, samples))
	}
	return materials
}

func mdlSamplesForColor(voxels map[[3]int]mdlVoxelSample, color [4]uint8) []mdlVoxelSample {
	out := make([]mdlVoxelSample, 0)
	for _, sample := range voxels {
		if sample.Color == color {
			out = append(out, sample)
		}
	}
	return out
}

func mdlAssetMaterialDef(id string, color [4]uint8, samples []mdlVoxelSample) content.AssetMaterialDef {
	tags := []string{"source:hl1", "source_asset:mdl", "material:texture_baked", "material:static_prop"}
	textureCounts := map[string]int{}
	flagValues := map[int]struct{}{}
	for _, sample := range samples {
		if sample.TextureName != "" {
			textureCounts[sample.TextureName]++
		}
		if sample.TextureFlags != 0 {
			flagValues[sample.TextureFlags] = struct{}{}
		}
	}
	textureNames := make([]string, 0, len(textureCounts))
	bestTexture, bestCount := "", 0
	for name, count := range textureCounts {
		textureNames = append(textureNames, name)
		if count > bestCount || (count == bestCount && strings.ToLower(name) < strings.ToLower(bestTexture)) {
			bestTexture, bestCount = name, count
		}
	}
	sort.Slice(textureNames, func(i, j int) bool { return strings.ToLower(textureNames[i]) < strings.ToLower(textureNames[j]) })
	for _, name := range textureNames {
		tags = appendUniqueString(tags, "source_texture:"+name)
	}
	flags := make([]int, 0, len(flagValues))
	for value := range flagValues {
		flags = append(flags, value)
	}
	sort.Ints(flags)
	for _, value := range flags {
		tags = append(tags, fmt.Sprintf("source_texture_flags:%d", value))
		if value&mdlTextureFlagMasked != 0 {
			tags = appendUniqueString(tags, "alpha:masked")
		}
	}
	semantics := materialSemantics(bestTexture)
	if bestTexture != "" {
		tags = appendUniqueString(tags, "kind:"+semantics.Kind)
		tags = appendUniqueString(tags, "classification:inferred")
	}
	roughness := semantics.Roughness
	if roughness <= 0 {
		roughness = 0.85
	}
	return content.AssetMaterialDef{
		ID:           id,
		Name:         id,
		BaseColor:    color,
		Roughness:    roughness,
		Metallic:     semantics.Metallic,
		Emissive:     semantics.Emissive,
		IOR:          1.5,
		Transparency: semantics.Transparency,
		Tags:         tags,
	}
}

func mdlAssetShapePalette(pal mdlColorPalette) []content.AssetVoxelPaletteEntryDef {
	shapePalette := make([]content.AssetVoxelPaletteEntryDef, 0, len(pal.colors))
	for _, entry := range pal.colors {
		shapePalette = append(shapePalette, content.AssetVoxelPaletteEntryDef{Value: entry.Value, MaterialID: mdlMaterialIDForColor(entry.Color)})
	}
	return shapePalette
}

func mdlMaterialIDForColor(color [4]uint8) string {
	return fmt.Sprintf("mat_%02x%02x%02x%02x", color[0], color[1], color[2], color[3])
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
