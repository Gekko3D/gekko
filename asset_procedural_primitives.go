package gekko

import "math"

func (server *AssetServer) CreateSphereModel(radius float32, resolution float32) AssetId {
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

	return server.CreateVoxelGeometry(VoxModel{
		SizeX: size, SizeY: size, SizeZ: size,
		Voxels: voxels,
	}, 1.0)
}

func (server *AssetServer) CreateCubeModel(sizeX, sizeY, sizeZ float32, resolution float32) AssetId {
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

	return server.CreateVoxelGeometry(VoxModel{
		SizeX: uint32(sx), SizeY: uint32(sy), SizeZ: uint32(sz),
		Voxels: voxels,
	}, 1.0)
}

func (server *AssetServer) CreateFrameModel(sizeX, sizeY, sizeZ, thickness float32, resolution float32) AssetId {
	sx, sy, sz := int(sizeX*resolution), int(sizeY*resolution), int(sizeZ*resolution)
	if sx <= 0 || sy <= 0 || sz <= 0 {
		return server.CreateVoxelGeometry(VoxModel{}, 1.0)
	}

	frame := int(thickness * resolution)
	if frame < 1 {
		frame = 1
	}

	voxels := []Voxel{}
	for x := 0; x < sx; x++ {
		isFrameX := x < frame || x >= sx-frame
		for y := 0; y < sy; y++ {
			for z := 0; z < sz; z++ {
				if !isFrameX && z >= frame && z < sz-frame {
					continue
				}
				voxels = append(voxels, Voxel{
					X:          uint32(x),
					Y:          uint32(y),
					Z:          uint32(z),
					ColorIndex: 1,
				})
			}
		}
	}

	return server.CreateVoxelGeometry(VoxModel{
		SizeX:  uint32(sx),
		SizeY:  uint32(sy),
		SizeZ:  uint32(sz),
		Voxels: voxels,
	}, 1.0)
}

func (server *AssetServer) CreateConeModel(radius, height float32, resolution float32) AssetId {
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

	return server.CreateVoxelGeometry(VoxModel{
		SizeX: uint32(r*2 + 1), SizeY: uint32(r*2 + 1), SizeZ: uint32(h),
		Voxels: voxels,
	}, 1.0)
}

func (server *AssetServer) CreatePyramidModel(size, height float32, resolution float32) AssetId {
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

	return server.CreateVoxelGeometry(VoxModel{
		SizeX: uint32(scaledSize), SizeY: uint32(scaledSize), SizeZ: uint32(scaledHeight),
		Voxels: voxels,
	}, 1.0)
}

func (server *AssetServer) CreateCylinderModel(radius, height float32, resolution float32) AssetId {
	scaledRadius := radius * resolution
	scaledHeight := height * resolution
	r := int(scaledRadius)
	h := int(scaledHeight)
	voxels := []Voxel{}
	r2 := scaledRadius * scaledRadius

	for z := 0; z < h; z++ {
		for x := -r; x <= r; x++ {
			for y := -r; y <= r; y++ {
				fx, fy := float32(x), float32(y)
				if fx*fx+fy*fy <= r2 {
					voxels = append(voxels, Voxel{
						X:          uint32(x + r),
						Y:          uint32(y + r),
						Z:          uint32(z),
						ColorIndex: 1,
					})
				}
			}
		}
	}

	return server.CreateVoxelGeometry(VoxModel{
		SizeX: uint32(r*2 + 1), SizeY: uint32(r*2 + 1), SizeZ: uint32(h),
		Voxels: voxels,
	}, 1.0)
}

// CreateCapsuleModel creates a capsule whose long axis is local Z, matching the
// legacy procedural model convention used by authored content primitives.
func (server *AssetServer) CreateCapsuleModel(radius, height float32, resolution float32) AssetId {
	scaledRadius := radius * resolution
	totalHeight := height * resolution
	r := int(scaledRadius)
	bodyHeight := int(math.Max(1, float64(totalHeight-scaledRadius*2)))
	totalSizeZ := bodyHeight + r*2 + 1
	voxels := []Voxel{}
	r2 := scaledRadius * scaledRadius
	cylinderOffset := float32(r)
	topCenter := cylinderOffset + float32(bodyHeight)

	for x := -r; x <= r; x++ {
		for y := -r; y <= r; y++ {
			for z := 0; z < totalSizeZ; z++ {
				fx, fy, fz := float32(x), float32(y), float32(z)
				insideCylinder := z >= r && z < r+bodyHeight && fx*fx+fy*fy <= r2
				bottomDz := fz - cylinderOffset
				topDz := fz - topCenter
				insideBottom := fx*fx+fy*fy+bottomDz*bottomDz <= r2
				insideTop := fx*fx+fy*fy+topDz*topDz <= r2
				if insideCylinder || insideBottom || insideTop {
					voxels = append(voxels, Voxel{
						X:          uint32(x + r),
						Y:          uint32(y + r),
						Z:          uint32(z),
						ColorIndex: 1,
					})
				}
			}
		}
	}

	return server.CreateVoxelGeometry(VoxModel{
		SizeX: uint32(r*2 + 1), SizeY: uint32(r*2 + 1), SizeZ: uint32(totalSizeZ),
		Voxels: voxels,
	}, 1.0)
}

// CreateCapsuleYModel creates a capsule whose long axis is local Y, matching
// ShapeCapsule's physics collider contract.
func (server *AssetServer) CreateCapsuleYModel(radius, height float32, resolution float32) AssetId {
	scaledRadius := radius * resolution
	totalHeight := height * resolution
	r := int(scaledRadius)
	bodyHeight := int(math.Max(1, float64(totalHeight-scaledRadius*2)))
	totalSizeY := bodyHeight + r*2 + 1
	voxels := []Voxel{}
	r2 := scaledRadius * scaledRadius
	cylinderOffset := float32(r)
	topCenter := cylinderOffset + float32(bodyHeight)

	for x := -r; x <= r; x++ {
		for y := 0; y < totalSizeY; y++ {
			for z := -r; z <= r; z++ {
				fx, fy, fz := float32(x), float32(y), float32(z)
				insideCylinder := y >= r && y < r+bodyHeight && fx*fx+fz*fz <= r2
				bottomDy := fy - cylinderOffset
				topDy := fy - topCenter
				insideBottom := fx*fx+fz*fz+bottomDy*bottomDy <= r2
				insideTop := fx*fx+fz*fz+topDy*topDy <= r2
				if insideCylinder || insideBottom || insideTop {
					voxels = append(voxels, Voxel{
						X:          uint32(x + r),
						Y:          uint32(y),
						Z:          uint32(z + r),
						ColorIndex: 1,
					})
				}
			}
		}
	}

	return server.CreateVoxelGeometry(VoxModel{
		SizeX: uint32(r*2 + 1), SizeY: uint32(totalSizeY), SizeZ: uint32(r*2 + 1),
		Voxels: voxels,
	}, 1.0)
}

func (server *AssetServer) CreateRampModel(sizeX, sizeY, sizeZ float32, resolution float32) AssetId {
	sx, sy, sz := int(sizeX*resolution), int(sizeY*resolution), int(sizeZ*resolution)
	voxels := []Voxel{}
	if sx <= 0 || sy <= 0 || sz <= 0 {
		return server.CreateVoxelGeometry(VoxModel{}, 1.0)
	}

	for x := 0; x < sx; x++ {
		maxY := int(math.Round(float64(float32(x+1) / float32(sx) * float32(sy))))
		if maxY < 1 {
			maxY = 1
		}
		if maxY > sy {
			maxY = sy
		}
		for y := 0; y < maxY; y++ {
			for z := 0; z < sz; z++ {
				voxels = append(voxels, Voxel{
					X:          uint32(x),
					Y:          uint32(y),
					Z:          uint32(z),
					ColorIndex: 1,
				})
			}
		}
	}

	return server.CreateVoxelGeometry(VoxModel{
		SizeX: uint32(sx), SizeY: uint32(sy), SizeZ: uint32(sz),
		Voxels: voxels,
	}, 1.0)
}
