package assets

type VoxelFileAsset struct {
	VoxFile *VoxFile
}

type VoxelModelAsset struct {
	VoxModel   VoxModel
	BrickSize  [3]uint32
	SourcePath string
}

type VoxelPaletteAsset struct {
	VoxPalette VoxPalette
	Materials  []VoxMaterial
	IsPBR      bool
	Roughness  float32
	Metalness  float32
	Emission   float32
	IOR        float32
	SourcePath string
}
