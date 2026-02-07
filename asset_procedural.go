package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

func (server AssetServer) CreateSphereModel(radius float32, resolution float32) AssetId {
	id := makeAssetId()
	scaledRadius := radius * resolution
	r := int(scaledRadius)
	size := uint32(r*2 + 1)
	voxels := []Voxel{}
	r2 := scaledRadius * scaledRadius

	for x := -r; x <= r; x++ {
		for y := -r; y <= r; y++ {
			for z := -r; z <= r; z++ {
				fx, fy, fz := float32(x), float32(y), float32(z)
				if fx*fx+fy*fy+fz*fz <= r2 {
					voxels = append(voxels, Voxel{
						X:          uint32(x + r),
						Y:          uint32(y + r),
						Z:          uint32(z + r),
						ColorIndex: 1,
					})
				}
			}
		}
	}

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: size, SizeY: size, SizeZ: size,
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreateCubeModel(sizeX, sizeY, sizeZ float32, resolution float32) AssetId {
	id := makeAssetId()
	sx, sy, sz := int(sizeX*resolution), int(sizeY*resolution), int(sizeZ*resolution)
	voxels := []Voxel{}

	for x := 0; x < sx; x++ {
		for y := 0; y < sy; y++ {
			for z := 0; z < sz; z++ {
				voxels = append(voxels, Voxel{
					X: uint32(x), Y: uint32(y), Z: uint32(z),
					ColorIndex: 1,
				})
			}
		}
	}

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: uint32(sx), SizeY: uint32(sy), SizeZ: uint32(sz),
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreateConeModel(radius, height float32, resolution float32) AssetId {
	id := makeAssetId()
	scaledRadius := radius * resolution
	scaledHeight := height * resolution
	r := int(scaledRadius)
	h := int(scaledHeight)
	voxels := []Voxel{}

	for z := 0; z < h; z++ {
		currR := scaledRadius * (1.0 - float32(z)/scaledHeight)
		currR2 := currR * currR
		for x := -r; x <= r; x++ {
			for y := -r; y <= r; y++ {
				fx, fy := float32(x), float32(y)
				if fx*fx+fy*fy <= currR2 {
					voxels = append(voxels, Voxel{
						X: uint32(x + r), Y: uint32(y + r), Z: uint32(z),
						ColorIndex: 1,
					})
				}
			}
		}
	}

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: uint32(r*2 + 1), SizeY: uint32(r*2 + 1), SizeZ: uint32(h),
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreatePyramidModel(size, height float32, resolution float32) AssetId {
	id := makeAssetId()
	scaledSize := size * resolution
	scaledHeight := height * resolution
	h := int(scaledHeight)
	voxels := []Voxel{}
	halfS := scaledSize * 0.5

	for z := 0; z < h; z++ {
		scale := 1.0 - float32(z)/scaledHeight
		limit := halfS * scale
		for x := int(-limit); x <= int(limit); x++ {
			for y := int(-limit); y <= int(limit); y++ {
				voxels = append(voxels, Voxel{
					X: uint32(float32(x) + halfS), Y: uint32(float32(y) + halfS), Z: uint32(z),
					ColorIndex: 1,
				})
			}
		}
	}

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: uint32(scaledSize), SizeY: uint32(scaledSize), SizeZ: uint32(scaledHeight),
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreateLineModel(start, end mgl32.Vec3, thickness float32) AssetId {
	id := makeAssetId()

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

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: sizeX, SizeY: sizeY, SizeZ: sizeZ,
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreateArrowModel(start, end mgl32.Vec3, thickness, headSize float32) AssetId {
	id := makeAssetId()

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

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: sizeX, SizeY: sizeY, SizeZ: sizeZ,
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreateWireframeBoxModel(size mgl32.Vec3, thickness float32) AssetId {
	id := makeAssetId()

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

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: uint32(sx + thickVoxels*2), SizeY: uint32(sy + thickVoxels*2), SizeZ: uint32(sz + thickVoxels*2),
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreateWireframeSphereModel(radius, thickness float32) AssetId {
	id := makeAssetId()

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
	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: size, SizeY: size, SizeZ: size,
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreateCrossModel(size, thickness float32) AssetId {
	id := makeAssetId()
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

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: uint32(s), SizeY: uint32(s), SizeZ: uint32(s),
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}
