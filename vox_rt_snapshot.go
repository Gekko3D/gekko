package gekko

import (
	"sync/atomic"

	"github.com/go-gl/mathgl/mgl32"
)

type VoxelInstanceSnapshot struct {
	EntityId  EntityId
	Position  mgl32.Vec3
	Rotation  mgl32.Quat
	Scale     mgl32.Vec3
	ModelId   AssetId
	PaletteId AssetId
}

type VoxelCameraSnapshot struct {
	Position mgl32.Vec3
	LookAt   mgl32.Vec3
	Up       mgl32.Vec3
	Fov      float32
	Aspect   float32
	Near     float32
	Far      float32
}

type VoxelRtSnapshot struct {
	Instances []VoxelInstanceSnapshot
	Camera    VoxelCameraSnapshot
}

type VoxelRtSnapshotContainer struct {
	latest atomic.Pointer[VoxelRtSnapshot]
}

func (c *VoxelRtSnapshotContainer) Update(s *VoxelRtSnapshot) {
	c.latest.Store(s)
}

func (c *VoxelRtSnapshotContainer) Get() *VoxelRtSnapshot {
	return c.latest.Load()
}

type VoxelPivotMode uint32

const (
	PivotModeCenter VoxelPivotMode = iota // Default: automatically center the pivot
	PivotModeCorner                       // Legacy behavior: pivot at (0,0,0) corner
	PivotModeCustom                       // Use CustomPivot value
)

type VoxelModelComponent struct {
	SharedGeometry         AssetId `gekko:"voxel" usage:"geometry"`
	OverrideGeometry       AssetId
	VoxelModel             AssetId `gekko:"voxel" usage:"model"`
	VoxelPalette           AssetId `gekko:"voxel" usage:"palette"`
	VoxelResolution        float32
	PivotMode              VoxelPivotMode // How to determine the rotation pivot
	CustomPivot            mgl32.Vec3     // Used if PivotMode == PivotModeCustom
	ShadowGroupID          uint32
	ShadowSeamWorldEpsilon float32
	IsTerrainChunk         bool
	TerrainGroupID         uint32
	TerrainChunkCoord      [3]int
	TerrainChunkSize       int
}

func (vmc *VoxelModelComponent) NormalizeGeometryRefs() {
	if vmc == nil {
		return
	}
	if vmc.SharedGeometry == (AssetId{}) && vmc.VoxelModel != (AssetId{}) {
		vmc.SharedGeometry = vmc.VoxelModel
	}
}

func (vmc *VoxelModelComponent) GeometryAsset() AssetId {
	if vmc == nil {
		return AssetId{}
	}
	if vmc.OverrideGeometry != (AssetId{}) {
		return vmc.OverrideGeometry
	}
	if vmc.VoxelModel != (AssetId{}) {
		return vmc.VoxelModel
	}
	return vmc.SharedGeometry
}

func VoxelResolutionOrDefault(vmc *VoxelModelComponent) float32 {
	if vmc != nil && vmc.VoxelResolution > 0 {
		return vmc.VoxelResolution
	}
	return VoxelSize
}

func EffectiveVoxelScale(vmc *VoxelModelComponent, tr *TransformComponent) mgl32.Vec3 {
	resolution := VoxelResolutionOrDefault(vmc)
	scale := mgl32.Vec3{1, 1, 1}
	if tr != nil {
		scale = tr.Scale
	}
	return mgl32.Vec3{
		resolution * scale.X(),
		resolution * scale.Y(),
		resolution * scale.Z(),
	}
}
