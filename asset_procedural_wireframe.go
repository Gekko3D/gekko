package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

func (server *AssetServer) CreateWireframeBoxModel(size mgl32.Vec3, thickness float32) AssetId {
	resolution := float32(10.0)
	sx := int(size.X() * resolution)
	sy := int(size.Y() * resolution)
	sz := int(size.Z() * resolution)

	thickVoxels := int(thickness * resolution)
	if thickVoxels < 1 {
		thickVoxels = 1
	}

	var voxels []Voxel
	voxelSet := make(map[[3]int]bool)

	edges := [][2][3]int{
		{{0, 0, 0}, {sx, 0, 0}},
		{{sx, 0, 0}, {sx, sy, 0}},
		{{sx, sy, 0}, {0, sy, 0}},
		{{0, sy, 0}, {0, 0, 0}},
		{{0, 0, sz}, {sx, 0, sz}},
		{{sx, 0, sz}, {sx, sy, sz}},
		{{sx, sy, sz}, {0, sy, sz}},
		{{0, sy, sz}, {0, 0, sz}},
		{{0, 0, 0}, {0, 0, sz}},
		{{sx, 0, 0}, {sx, 0, sz}},
		{{sx, sy, 0}, {sx, sy, sz}},
		{{0, sy, 0}, {0, sy, sz}},
	}

	for _, edge := range edges {
		start := edge[0]
		end := edge[1]

		dx := end[0] - start[0]
		dy := end[1] - start[1]
		dz := end[2] - start[2]

		absDx := dx
		if absDx < 0 {
			absDx = -absDx
		}
		absDy := dy
		if absDy < 0 {
			absDy = -absDy
		}
		absDz := dz
		if absDz < 0 {
			absDz = -absDz
		}

		maxDist := absDx
		if absDy > maxDist {
			maxDist = absDy
		}
		if absDz > maxDist {
			maxDist = absDz
		}
		if maxDist < 1 {
			maxDist = 1
		}

		for i := 0; i <= maxDist; i++ {
			t := float32(i) / float32(maxDist)
			x := start[0] + int(float32(dx)*t)
			y := start[1] + int(float32(dy)*t)
			z := start[2] + int(float32(dz)*t)

			for dtx := -thickVoxels; dtx <= thickVoxels; dtx++ {
				for dty := -thickVoxels; dty <= thickVoxels; dty++ {
					for dtz := -thickVoxels; dtz <= thickVoxels; dtz++ {
						voxelSet[[3]int{x + dtx, y + dty, z + dtz}] = true
					}
				}
			}
		}
	}

	for key := range voxelSet {
		voxels = append(voxels, Voxel{
			X:          uint32(key[0]),
			Y:          uint32(key[1]),
			Z:          uint32(key[2]),
			ColorIndex: 1,
		})
	}

	return server.CreateVoxelGeometry(VoxModel{
		SizeX: uint32(sx + thickVoxels*2), SizeY: uint32(sy + thickVoxels*2), SizeZ: uint32(sz + thickVoxels*2),
		Voxels: voxels,
	}, 1.0)
}

func (server *AssetServer) CreateWireframeSphereModel(radius, thickness float32) AssetId {
	resolution := float32(10.0)
	r := int(radius * resolution)
	thickVoxels := int(thickness * resolution)
	if thickVoxels < 1 {
		thickness = 1
	}

	var voxels []Voxel
	voxelSet := make(map[[3]int]bool)
	numSegments := 32

	for i := 0; i < numSegments; i++ {
		angle := float32(i) * 2.0 * math.Pi / float32(numSegments)
		x := int(float32(r) * float32(math.Cos(float64(angle))))
		y := int(float32(r) * float32(math.Sin(float64(angle))))
		z := 0
		for dtx := -thickVoxels; dtx <= thickVoxels; dtx++ {
			for dty := -thickVoxels; dty <= thickVoxels; dty++ {
				for dtz := -thickVoxels; dtz <= thickVoxels; dtz++ {
					voxelSet[[3]int{x + dtx + r, y + dty + r, z + dtz + r}] = true
				}
			}
		}
	}

	for i := 0; i < numSegments; i++ {
		angle := float32(i) * 2.0 * math.Pi / float32(numSegments)
		x := int(float32(r) * float32(math.Cos(float64(angle))))
		z := int(float32(r) * float32(math.Sin(float64(angle))))
		y := 0
		for dtx := -thickVoxels; dtx <= thickVoxels; dtx++ {
			for dty := -thickVoxels; dty <= thickVoxels; dty++ {
				for dtz := -thickVoxels; dtz <= thickVoxels; dtz++ {
					voxelSet[[3]int{x + dtx + r, y + dty + r, z + dtz + r}] = true
				}
			}
		}
	}

	for i := 0; i < numSegments; i++ {
		angle := float32(i) * 2.0 * math.Pi / float32(numSegments)
		y := int(float32(r) * float32(math.Cos(float64(angle))))
		z := int(float32(r) * float32(math.Sin(float64(angle))))
		x := 0
		for dtx := -thickVoxels; dtx <= thickVoxels; dtx++ {
			for dty := -thickVoxels; dty <= thickVoxels; dty++ {
				for dtz := -thickVoxels; dtz <= thickVoxels; dtz++ {
					voxelSet[[3]int{x + dtx + r, y + dty + r, z + dtz + r}] = true
				}
			}
		}
	}

	for key := range voxelSet {
		voxels = append(voxels, Voxel{
			X:          uint32(key[0]),
			Y:          uint32(key[1]),
			Z:          uint32(key[2]),
			ColorIndex: 1,
		})
	}

	size := uint32(r*2 + thickVoxels*2)
	return server.CreateVoxelGeometry(VoxModel{
		SizeX: size, SizeY: size, SizeZ: size,
		Voxels: voxels,
	}, 1.0)
}

func (server *AssetServer) CreateCrossModel(size, thickness float32) AssetId {
	resolution := float32(10.0)
	s := int(size * resolution)
	thickVoxels := int(thickness * resolution)
	if thickVoxels < 1 {
		thickVoxels = 1
	}
	var voxels []Voxel
	voxelSet := make(map[[3]int]bool)
	center := s / 2

	for x := 0; x < s; x++ {
		for dt := -thickVoxels; dt <= thickVoxels; dt++ {
			for dt2 := -thickVoxels; dt2 <= thickVoxels; dt2++ {
				voxelSet[[3]int{x, center + dt, center + dt2}] = true
			}
		}
	}
	for y := 0; y < s; y++ {
		for dt := -thickVoxels; dt <= thickVoxels; dt++ {
			for dt2 := -thickVoxels; dt2 <= thickVoxels; dt2++ {
				voxelSet[[3]int{center + dt, y, center + dt2}] = true
			}
		}
	}
	for z := 0; z < s; z++ {
		for dt := -thickVoxels; dt <= thickVoxels; dt++ {
			for dt2 := -thickVoxels; dt2 <= thickVoxels; dt2++ {
				voxelSet[[3]int{center + dt, center + dt2, z}] = true
			}
		}
	}

	for key := range voxelSet {
		voxels = append(voxels, Voxel{
			X:          uint32(key[0]),
			Y:          uint32(key[1]),
			Z:          uint32(key[2]),
			ColorIndex: 1,
		})
	}

	return server.CreateVoxelGeometry(VoxModel{
		SizeX: uint32(s), SizeY: uint32(s), SizeZ: uint32(s),
		Voxels: voxels,
	}, 1.0)
}
