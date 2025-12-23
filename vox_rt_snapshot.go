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

type VoxelModelComponent struct {
	VoxelModel   AssetId `gekko:"voxel" usage:"model"`
	VoxelPalette AssetId `gekko:"voxel" usage:"palette"`
}
