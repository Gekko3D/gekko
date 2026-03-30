package gekko

import (
	"github.com/go-gl/mathgl/mgl32"
)

func (server *AssetServer) CreateLineModel(start, end mgl32.Vec3, thickness float32) AssetId {
	dir := end.Sub(start)
	length := dir.Len()
	dir = dir.Normalize()

	resolution := float32(10.0)
	numSteps := int(length * resolution)
	if numSteps < 2 {
		numSteps = 2
	}

	thickVoxels := int(thickness * resolution)
	if thickVoxels < 1 {
		thickVoxels = 1
	}

	var voxels []Voxel
	voxelSet := make(map[[3]int]bool)

	for i := 0; i <= numSteps; i++ {
		t := float32(i) / float32(numSteps)
		pos := start.Add(dir.Mul(length * t))

		for dx := -thickVoxels; dx <= thickVoxels; dx++ {
			for dy := -thickVoxels; dy <= thickVoxels; dy++ {
				for dz := -thickVoxels; dz <= thickVoxels; dz++ {
					vx := int(pos.X()*resolution) + dx
					vy := int(pos.Y()*resolution) + dy
					vz := int(pos.Z()*resolution) + dz

					key := [3]int{vx, vy, vz}
					if !voxelSet[key] {
						voxelSet[key] = true
					}
				}
			}
		}
	}

	minX, minY, minZ := int(1e9), int(1e9), int(1e9)
	maxX, maxY, maxZ := int(-1e9), int(-1e9), int(-1e9)
	for key := range voxelSet {
		if key[0] < minX {
			minX = key[0]
		}
		if key[0] > maxX {
			maxX = key[0]
		}
		if key[1] < minY {
			minY = key[1]
		}
		if key[1] > maxY {
			maxY = key[1]
		}
		if key[2] < minZ {
			minZ = key[2]
		}
		if key[2] > maxZ {
			maxZ = key[2]
		}
	}

	for key := range voxelSet {
		voxels = append(voxels, Voxel{
			X:          uint32(key[0] - minX),
			Y:          uint32(key[1] - minY),
			Z:          uint32(key[2] - minZ),
			ColorIndex: 1,
		})
	}

	sizeX := uint32(maxX - minX + 1)
	sizeY := uint32(maxY - minY + 1)
	sizeZ := uint32(maxZ - minZ + 1)

	return server.CreateVoxelGeometry(VoxModel{
		SizeX: sizeX, SizeY: sizeY, SizeZ: sizeZ,
		Voxels: voxels,
	}, 1.0)
}

func (server *AssetServer) CreateArrowModel(start, end mgl32.Vec3, thickness, headSize float32) AssetId {
	dir := end.Sub(start)
	length := dir.Len()
	dir = dir.Normalize()

	resolution := float32(10.0)
	numSteps := int(length * resolution)
	if numSteps < 2 {
		numSteps = 2
	}

	thickVoxels := int(thickness * resolution)
	if thickVoxels < 1 {
		thickVoxels = 1
	}

	headVoxels := int(headSize * resolution)
	if headVoxels < 2 {
		headVoxels = 2
	}

	var voxels []Voxel
	voxelSet := make(map[[3]int]bool)

	shaftSteps := int(float32(numSteps) * 0.8)
	for i := 0; i <= shaftSteps; i++ {
		t := float32(i) / float32(numSteps)
		pos := start.Add(dir.Mul(length * t))

		for dx := -thickVoxels; dx <= thickVoxels; dx++ {
			for dy := -thickVoxels; dy <= thickVoxels; dy++ {
				for dz := -thickVoxels; dz <= thickVoxels; dz++ {
					vx := int(pos.X()*resolution) + dx
					vy := int(pos.Y()*resolution) + dy
					vz := int(pos.Z()*resolution) + dz
					voxelSet[[3]int{vx, vy, vz}] = true
				}
			}
		}
	}

	for i := shaftSteps; i <= numSteps; i++ {
		t := float32(i) / float32(numSteps)
		pos := start.Add(dir.Mul(length * t))

		progress := float32(i-shaftSteps) / float32(numSteps-shaftSteps)
		currentSize := int(float32(headVoxels) * (1.0 - progress))
		if currentSize < thickVoxels {
			currentSize = thickVoxels
		}

		for dx := -currentSize; dx <= currentSize; dx++ {
			for dy := -currentSize; dy <= currentSize; dy++ {
				for dz := -currentSize; dz <= currentSize; dz++ {
					vx := int(pos.X()*resolution) + dx
					vy := int(pos.Y()*resolution) + dy
					vz := int(pos.Z()*resolution) + dz
					voxelSet[[3]int{vx, vy, vz}] = true
				}
			}
		}
	}

	minX, minY, minZ := int(1e9), int(1e9), int(1e9)
	maxX, maxY, maxZ := int(-1e9), int(-1e9), int(-1e9)
	for key := range voxelSet {
		if key[0] < minX {
			minX = key[0]
		}
		if key[0] > maxX {
			maxX = key[0]
		}
		if key[1] < minY {
			minY = key[1]
		}
		if key[1] > maxY {
			maxY = key[1]
		}
		if key[2] < minZ {
			minZ = key[2]
		}
		if key[2] > maxZ {
			maxZ = key[2]
		}
	}

	for key := range voxelSet {
		voxels = append(voxels, Voxel{
			X:          uint32(key[0] - minX),
			Y:          uint32(key[1] - minY),
			Z:          uint32(key[2] - minZ),
			ColorIndex: 1,
		})
	}

	sizeX := uint32(maxX - minX + 1)
	sizeY := uint32(maxY - minY + 1)
	sizeZ := uint32(maxZ - minZ + 1)

	return server.CreateVoxelGeometry(VoxModel{
		SizeX: sizeX, SizeY: sizeY, SizeZ: sizeZ,
		Voxels: voxels,
	}, 1.0)
}
