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
	VoxPalette             VoxPalette
	Materials              []VoxMaterial
	Animations             []VoxelPaletteAnimation
	MaterialFrameOverrides map[uint8]VoxelPaletteMaterialFrameOverride
	IsPBR                  bool
	Roughness              float32
	Metalness              float32
	Emission               float32
	IOR                    float32
	Transparency           float32
	SourcePath             string
}

type VoxelPaletteAnimation struct {
	ID             string
	Kind           string
	FPS            float32
	Mode           string
	PaletteIndices []uint8
	Frames         []VoxelPaletteAnimationFrame
	UVScroll       *VoxelPaletteUVScroll
	Tags           []string
}

type VoxelPaletteUVScroll struct {
	Velocity [2]float32
}

type VoxelPaletteAnimationFrame struct {
	Duration       float32
	Colors         [][4]uint8
	EmissiveColors [][4]uint8
	Emission       []float32
	Roughness      []float32
	Transparency   []float32
}

type VoxelPaletteMaterialFrameOverride struct {
	EmissiveColor    [4]uint8
	HasEmissiveColor bool
	Emission         float32
	HasEmission      bool
	Roughness        float32
	HasRoughness     bool
	Transparency     float32
	HasTransparency  bool
}
