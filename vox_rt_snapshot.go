package gekko

import (
	"sync/atomic"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
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
	VoxelModel   AssetId           `gekko:"voxel" usage:"model"`
	VoxelPalette AssetId           `gekko:"voxel" usage:"palette"`
	PivotMode    VoxelPivotMode    // How to determine the rotation pivot
	CustomPivot  mgl32.Vec3        // Used if PivotMode == PivotModeCustom
	CustomMap    *volume.XBrickMap // If set, use this instead of loading from VoxelModel asset
}
