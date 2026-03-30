package assets

import (
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

type VoxelFileAsset struct {
	VoxFile *VoxFile
}

type VoxelGeometryAsset struct {
	VoxModel     VoxModel
	XBrickMap    *volume.XBrickMap
	LocalMin     mgl32.Vec3
	LocalMax     mgl32.Vec3
	BrickSize    [3]uint32
	SourcePath   string
	RuntimeOwned bool
}

type VoxelModelAsset = VoxelGeometryAsset

type VoxelPaletteAsset struct {
	VoxPalette   VoxPalette
	Materials    []VoxMaterial
	IsPBR        bool
	Roughness    float32
	Metalness    float32
	Emission     float32
	IOR          float32
	Transparency float32
	SourcePath   string
}
