package gekko

import "github.com/gekko3d/gekko/voxelrt/rt/core"

// GameplaySeeThroughMaterial creates a transparent material that does not use
// transmission, density, or refraction. Use it for readability helpers rather
// than optical glass.
func GameplaySeeThroughMaterial(baseColor [4]uint8, transparency float32) core.Material {
	return core.NewGameplaySeeThroughMaterial(baseColor, transparency)
}

// ApplyGameplaySeeThroughMaterial converts an existing material into the
// gameplay see-through mode while preserving its other visual properties.
func ApplyGameplaySeeThroughMaterial(mat *core.Material, transparency float32) {
	if mat == nil {
		return
	}
	mat.ApplyGameplaySeeThrough(transparency)
}
