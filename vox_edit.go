package gekko

import (
	"github.com/go-gl/mathgl/mgl32"
)

type VoxelEdit struct {
	Entity EntityId
	Pos    [3]int
	Val    uint8
}

type SphereCarve struct {
	Entity         EntityId
	Center         mgl32.Vec3
	Radius         float32
	Value          uint8
	DensityFalloff bool
}

type VoxelEditQueue struct {
	BudgetPerFrame int
	Edits          []VoxelEdit
	Spheres        []SphereCarve
}
