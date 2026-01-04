package volume

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/go-gl/mathgl/mgl32"
)

const (
	BrickSize    = 8
	MicroSize    = 2
	SectorBricks = 4
	SectorSize   = SectorBricks * BrickSize // 32

	BrickFlagSolid = 1
)

type Brick struct {
	OccupancyMask64 uint64
	Payload         [BrickSize][BrickSize][BrickSize]uint8
	AtlasOffset     uint32
	Flags           uint32
}

func NewBrick() *Brick {
	return &Brick{}
}

func (b *Brick) Copy() *Brick {
	newB := *b
	return &newB
}

func (b *Brick) SetVoxel(bx, by, bz int, val uint8) {
	b.Payload[bx][by][bz] = val

	mx, my, mz := bx/MicroSize, by/MicroSize, bz/MicroSize
	bitIdx := mx + my*4 + mz*16

	if val != 0 {
		b.OccupancyMask64 |= (1 << bitIdx)
	} else {
		// Re-evaluate
		empty := true
		ms := MicroSize
		startMx, startMy, startMz := mx*ms, my*ms, mz*ms

		for x := 0; x < ms; x++ {
			for y := 0; y < ms; y++ {
				for z := 0; z < ms; z++ {
					if b.Payload[startMx+x][startMy+y][startMz+z] != 0 {
						empty = false
						break
					}
				}
				if !empty {
					break
				}
			}
			if !empty {
				break
			}
		}

		if empty {
			b.OccupancyMask64 &^= (1 << bitIdx)
		}
	}
}

func (b *Brick) Expand(paletteIdx uint8) {
	b.Flags &^= BrickFlagSolid
	b.OccupancyMask64 = 0xFFFFFFFFFFFFFFFF
	for z := 0; z < BrickSize; z++ {
		for y := 0; y < BrickSize; y++ {
			for x := 0; x < BrickSize; x++ {
				b.Payload[x][y][z] = paletteIdx
			}
		}
	}
}

func (b *Brick) TryCompress() bool {
	if b.IsEmpty() {
		return false
	}
	firstVal := b.Payload[0][0][0]
	if firstVal == 0 {
		return false
	}
	for z := 0; z < BrickSize; z++ {
		for y := 0; y < BrickSize; y++ {
			for x := 0; x < BrickSize; x++ {
				if b.Payload[x][y][z] != firstVal {
					return false
				}
			}
		}
	}
	b.Flags |= BrickFlagSolid
	b.AtlasOffset = uint32(firstVal)
	return true
}

func (b *Brick) IsEmpty() bool {
	return b.OccupancyMask64 == 0
}

type Sector struct {
	Coords       [3]int
	BrickMask64  uint64
	PackedBricks []*Brick
}

func NewSector(sx, sy, sz int) *Sector {
	return &Sector{
		Coords: [3]int{sx, sy, sz},
	}
}

func (s *Sector) Copy() *Sector {
	newS := NewSector(s.Coords[0], s.Coords[1], s.Coords[2])
	newS.BrickMask64 = s.BrickMask64
	newS.PackedBricks = make([]*Brick, len(s.PackedBricks))
	for i, b := range s.PackedBricks {
		newS.PackedBricks[i] = b.Copy()
	}
	return newS
}

func (s *Sector) GetPackedIndex(flatIdx int) int {
	maskBelow := (uint64(1) << flatIdx) - 1
	return bits.OnesCount64(s.BrickMask64 & maskBelow)
}

func (s *Sector) GetBrick(bx, by, bz int) *Brick {
	flatIdx := bx + by*4 + bz*16
	if (s.BrickMask64 & (1 << flatIdx)) == 0 {
		return nil
	}
	packedIdx := s.GetPackedIndex(flatIdx)
	return s.PackedBricks[packedIdx]
}

func (s *Sector) GetOrCreateBrick(bx, by, bz int) (*Brick, bool) {
	flatIdx := bx + by*4 + bz*16
	if (s.BrickMask64 & (1 << flatIdx)) != 0 {
		packedIdx := s.GetPackedIndex(flatIdx)
		return s.PackedBricks[packedIdx], false
	}

	newBrick := NewBrick()
	packedIdx := s.GetPackedIndex(flatIdx)

	// Insert
	s.PackedBricks = append(s.PackedBricks, nil)
	copy(s.PackedBricks[packedIdx+1:], s.PackedBricks[packedIdx:])
	s.PackedBricks[packedIdx] = newBrick

	s.BrickMask64 |= (1 << flatIdx)
	return newBrick, true
}

func (s *Sector) RemoveBrickIfEmpty(bx, by, bz int) {
	flatIdx := bx + by*4 + bz*16
	if (s.BrickMask64 & (1 << flatIdx)) == 0 {
		return
	}

	packedIdx := s.GetPackedIndex(flatIdx)
	brick := s.PackedBricks[packedIdx]
	if brick.IsEmpty() {
		// Remove
		s.PackedBricks = append(s.PackedBricks[:packedIdx], s.PackedBricks[packedIdx+1:]...)
		s.BrickMask64 &^= (1 << flatIdx)
	}
}

func (s *Sector) IsEmpty() bool {
	return s.BrickMask64 == 0
}

type XBrickMap struct {
	Sectors      map[[3]int]*Sector
	DirtySectors map[[3]int]bool
	DirtyBricks  map[[6]int]bool

	NextAtlasOffset uint32
	FreeSlots       []uint32
	BrickAtlasMap   map[[6]int]uint32

	AABBDirty      bool
	StructureDirty bool // True if bricks were added or removed
	CachedMin      mgl32.Vec3
	CachedMax      mgl32.Vec3

	// GPU editing
	GPUEditMode bool
	gpuManager  interface{} // *gpu.GpuBufferManager (avoid circular import)
}

func NewXBrickMap() *XBrickMap {
	return &XBrickMap{
		Sectors:        make(map[[3]int]*Sector),
		DirtySectors:   make(map[[3]int]bool),
		DirtyBricks:    make(map[[6]int]bool),
		BrickAtlasMap:  make(map[[6]int]uint32),
		AABBDirty:      true,
		StructureDirty: true, // Initial state needs build
	}
}

func (x *XBrickMap) AllocateAtlasSlot(brickKey [6]int) uint32 {
	var offset uint32
	if len(x.FreeSlots) > 0 {
		offset = x.FreeSlots[len(x.FreeSlots)-1]
		x.FreeSlots = x.FreeSlots[:len(x.FreeSlots)-1]
	} else {
		offset = x.NextAtlasOffset
		x.NextAtlasOffset += 512
	}
	x.BrickAtlasMap[brickKey] = offset
	return offset
}

func (x *XBrickMap) FreeAtlasSlot(brickKey [6]int) {
	if offset, ok := x.BrickAtlasMap[brickKey]; ok {
		delete(x.BrickAtlasMap, brickKey)
		x.FreeSlots = append(x.FreeSlots, offset)
	}
}

func (x *XBrickMap) ClearDirty() {
	x.DirtySectors = make(map[[3]int]bool)
	x.DirtyBricks = make(map[[6]int]bool)
	x.StructureDirty = false
}

func (x *XBrickMap) SetVoxel(gx, gy, gz int, val uint8) {
	// GPU-first mode: queue edit on GPU instead of CPU update
	if x.GPUEditMode && x.gpuManager != nil {
		type EditQueuer interface {
			QueueEdit(x, y, z int, val uint8)
		}
		if mgr, ok := x.gpuManager.(EditQueuer); ok {
			mgr.QueueEdit(gx, gy, gz, val)
			// Continue to CPU update
		}
	}

	// CPU update path (original logic)
	sx, sy, sz := gx/SectorSize, gy/SectorSize, gz/SectorSize
	slx, sly, slz := gx%SectorSize, gy%SectorSize, gz%SectorSize
	if slx < 0 {
		slx += SectorSize
		sx--
	}
	if sly < 0 {
		sly += SectorSize
		sy--
	}
	if slz < 0 {
		slz += SectorSize
		sz--
	}

	bx, by, bz := slx/BrickSize, sly/BrickSize, slz/BrickSize
	vx, vy, vz := slx%BrickSize, sly%BrickSize, slz%BrickSize

	sKey := [3]int{sx, sy, sz}
	bKey := [6]int{sx, sy, sz, bx, by, bz}

	if val == 0 {
		if sector, ok := x.Sectors[sKey]; ok {
			brick := sector.GetBrick(bx, by, bz)
			if brick != nil {
				if brick.Flags&BrickFlagSolid != 0 {
					brick.Expand(uint8(brick.AtlasOffset))
					// Reset atlas offset because it now needs a real slot
					offset := x.AllocateAtlasSlot(bKey)
					brick.AtlasOffset = offset
				}

				brick.SetVoxel(vx, vy, vz, 0)
				if !x.GPUEditMode {
					x.DirtySectors[sKey] = true
					x.DirtyBricks[bKey] = true
				}
				x.AABBDirty = true

				sector.RemoveBrickIfEmpty(bx, by, bz)
				if sector.IsEmpty() {
					x.FreeAtlasSlot(bKey)
					delete(x.Sectors, sKey)
					x.StructureDirty = true
				} else if brick.IsEmpty() {
					x.FreeAtlasSlot(bKey)
					x.StructureDirty = true
				} else {
					// Try to re-compress? Or leave as sparse until full rebuild?
					// For simple editing, leave sparse.
				}
			}
		}
	} else {
		sector, ok := x.Sectors[sKey]
		if !ok {
			sector = NewSector(sx, sy, sz)
			x.Sectors[sKey] = sector
			x.StructureDirty = true
		}

		brick, isNew := sector.GetOrCreateBrick(bx, by, bz)
		if isNew {
			offset := x.AllocateAtlasSlot(bKey)
			brick.AtlasOffset = offset
			x.StructureDirty = true
		} else {
			if brick.Flags&BrickFlagSolid != 0 {
				if brick.AtlasOffset == uint32(val) {
					return // Already solid with this value
				}
				brick.Expand(uint8(brick.AtlasOffset))
				// Now needs a real slot
				offset := x.AllocateAtlasSlot(bKey)
				brick.AtlasOffset = offset
			} else if _, has := x.BrickAtlasMap[bKey]; has {
				brick.AtlasOffset = x.BrickAtlasMap[bKey]
			}
		}

		brick.SetVoxel(vx, vy, vz, val)
		if !x.GPUEditMode {
			x.DirtySectors[sKey] = true
			x.DirtyBricks[bKey] = true
		}
		x.AABBDirty = true

		// Optional: Compress if full
		brick.TryCompress()
		if brick.Flags&BrickFlagSolid != 0 {
			x.FreeAtlasSlot(bKey)
		}
	}
}

// EnableGPUEditing enables GPU-accelerated voxel editing
func (x *XBrickMap) EnableGPUEditing(mgr interface{}) {
	x.GPUEditMode = true
	x.gpuManager = mgr
}

// GetVoxel returns (found, value) for a voxel at global coordinates
func (x *XBrickMap) GetVoxel(gx, gy, gz int) (bool, uint8) {
	sx, sy, sz := gx/SectorSize, gy/SectorSize, gz/SectorSize
	slx, sly, slz := gx%SectorSize, gy%SectorSize, gz%SectorSize
	if slx < 0 {
		slx += SectorSize
		sx--
	}
	if sly < 0 {
		sly += SectorSize
		sy--
	}
	if slz < 0 {
		slz += SectorSize
		sz--
	}

	bx, by, bz := slx/BrickSize, sly/BrickSize, slz/BrickSize
	vx, vy, vz := slx%BrickSize, sly%BrickSize, slz%BrickSize

	sKey := [3]int{sx, sy, sz}

	sector, ok := x.Sectors[sKey]
	if !ok {
		return false, 0
	}

	brick := sector.GetBrick(bx, by, bz)
	if brick == nil {
		return false, 0
	}

	val := brick.Payload[vx][vy][vz]
	return val != 0, val
}

func (x *XBrickMap) Copy() *XBrickMap {
	newMap := NewXBrickMap()
	newMap.NextAtlasOffset = x.NextAtlasOffset // Usually we'd start fresh but for simple copy we might persist

	// Copy sectors
	for k, v := range x.Sectors {
		newMap.Sectors[k] = v.Copy()
	}

	// Rebuild atlas map in new map usually, but for simple clone:
	// If we want FULL independence, we should probably re-allocate offsets?
	// But if we want simple COW semantics where existing data is shared until modified,
	// we would need more complex memory management (shared atlas buffers).
	// For now, let's just deep copy logic as per Python reference which seemed to imply a logical copy.
	// In the python code: `xbm.copy()` creates new Sectors and Bricks.
	// `brick.atlas_offset` is copied.
	// So they essentially point to the same "slots" in the buffer?
	// Wait, if they point to the same slots, modifying one would modify the other if they share the GPU buffer.
	// But `GpuBufferManager` uploads based on `xbm` object identity.
	// If we create a NEW `xbm`, the `GpuBufferManager` treats it as a new map.
	// It will upload it to new slots.
	// So `AtlasOffset` stored in the Brick is purely relative to THAT map's payload base.
	// So it is safe to copy `AtlasOffset` numerical value, as long as `GpuBufferManager`
	// assigns a fresh `payload_base` for this new map.
	// Wait, `AtlasOffset` is the offset *within* the payload buffer chunk for this map.
	// So yes, copying it preserves the internal structure.

	for k, v := range x.BrickAtlasMap {
		newMap.BrickAtlasMap[k] = v
	}

	// Free slots
	newMap.FreeSlots = make([]uint32, len(x.FreeSlots))
	copy(newMap.FreeSlots, x.FreeSlots)

	return newMap
}

func (x *XBrickMap) ComputeAABB() (mgl32.Vec3, mgl32.Vec3) {
	if !x.AABBDirty {
		return x.CachedMin, x.CachedMax
	}

	if len(x.Sectors) == 0 {
		x.CachedMin = mgl32.Vec3{}
		x.CachedMax = mgl32.Vec3{}
		x.AABBDirty = false
		return x.CachedMin, x.CachedMax
	}

	minB := mgl32.Vec3{float32(1e20), float32(1e20), float32(1e20)}
	maxB := mgl32.Vec3{float32(-1e20), float32(-1e20), float32(-1e20)}
	found := false

	for sKey, sector := range x.Sectors {
		if sector.IsEmpty() {
			continue
		}
		ox, oy, oz := float32(sKey[0]*SectorSize), float32(sKey[1]*SectorSize), float32(sKey[2]*SectorSize)

		for i := 0; i < 64; i++ {
			if (sector.BrickMask64 & (1 << i)) != 0 {
				bx, by, bz := i%4, (i/4)%4, i/16
				brick := sector.GetBrick(bx, by, bz)
				if brick == nil || brick.IsEmpty() {
					continue
				}

				brickOx := ox + float32(bx*BrickSize)
				brickOy := oy + float32(by*BrickSize)
				brickOz := oz + float32(bz*BrickSize)

				if brick.Flags&BrickFlagSolid != 0 {
					vMin := mgl32.Vec3{brickOx, brickOy, brickOz}
					vMax := vMin.Add(mgl32.Vec3{float32(BrickSize), float32(BrickSize), float32(BrickSize)})
					minB = mgl32.Vec3{min(minB.X(), vMin.X()), min(minB.Y(), vMin.Y()), min(minB.Z(), vMin.Z())}
					maxB = mgl32.Vec3{max(maxB.X(), vMax.X()), max(maxB.Y(), vMax.Y()), max(maxB.Z(), vMax.Z())}
					found = true
					continue
				}

				// Iterate microblocks (2x2x2) using mask
				for m := 0; m < 64; m++ {
					if (brick.OccupancyMask64 & (1 << m)) != 0 {
						mx, my, mz := m%4, (m/4)%4, m/16
						ms := MicroSize
						startVx, startVy, startVz := mx*ms, my*ms, mz*ms
						for vx := 0; vx < ms; vx++ {
							for vy := 0; vy < ms; vy++ {
								for vz := 0; vz < ms; vz++ {
									if brick.Payload[startVx+vx][startVy+vy][startVz+vz] != 0 {
										vMin := mgl32.Vec3{
											brickOx + float32(startVx+vx),
											brickOy + float32(startVy+vy),
											brickOz + float32(startVz+vz),
										}
										vMax := vMin.Add(mgl32.Vec3{1, 1, 1})

										minB = mgl32.Vec3{min(minB.X(), vMin.X()), min(minB.Y(), vMin.Y()), min(minB.Z(), vMin.Z())}
										maxB = mgl32.Vec3{max(maxB.X(), vMax.X()), max(maxB.Y(), vMax.Y()), max(maxB.Z(), vMax.Z())}
										found = true
									}
								}
							}
						}
					}
				}
			}
		}
	}

	if !found {
		x.CachedMin = mgl32.Vec3{}
		x.CachedMax = mgl32.Vec3{}
	} else {
		x.CachedMin = minB
		x.CachedMax = maxB
	}
	x.AABBDirty = false
	return x.CachedMin, x.CachedMax
}

func (x *XBrickMap) RayMarch(rayOrigin, rayDir mgl32.Vec3, tMin, tMax float32) (bool, float32, [3]int, mgl32.Vec3) {
	t := tMin
	// Protect against very small or zero direction components
	safeX := rayDir.X()
	if math.Abs(float64(safeX)) < 1e-7 {
		if safeX >= 0 {
			safeX = 1e-7
		} else {
			safeX = -1e-7
		}
	}
	safeY := rayDir.Y()
	if math.Abs(float64(safeY)) < 1e-7 {
		if safeY >= 0 {
			safeY = 1e-7
		} else {
			safeY = -1e-7
		}
	}
	safeZ := rayDir.Z()
	if math.Abs(float64(safeZ)) < 1e-7 {
		if safeZ >= 0 {
			safeZ = 1e-7
		} else {
			safeZ = -1e-7
		}
	}

	invDir := mgl32.Vec3{1.0 / safeX, 1.0 / safeY, 1.0 / safeZ}

	iterations := 0
	const maxIterations = 10000 // Increased from 2000

	for t < tMax && iterations < maxIterations {
		iterations++

		// Use a dynamic offset for higher stability at large t
		tBias := 0.001
		if t > 100 {
			tBias = 0.005
		}

		p := rayOrigin.Add(rayDir.Mul(t + float32(tBias)))

		sx, sy, sz := int(math.Floor(float64(p.X()/SectorSize))), int(math.Floor(float64(p.Y()/SectorSize))), int(math.Floor(float64(p.Z()/SectorSize)))

		sKey := [3]int{sx, sy, sz}
		sector, ok := x.Sectors[sKey]
		if !ok {
			// Step to next sector
			res := x.stepToNext(p, rayDir, invDir, float32(SectorSize))
			t += float32(math.Max(float64(res), 0.001))
			continue
		}

		// Inside a sector, check bricks
		flX, flY, flZ := math.Floor(float64(p.X())), math.Floor(float64(p.Y())), math.Floor(float64(p.Z()))
		slx := int(flX) % SectorSize
		if slx < 0 {
			slx += SectorSize
		}
		sly := int(flY) % SectorSize
		if sly < 0 {
			sly += SectorSize
		}
		slz := int(flZ) % SectorSize
		if slz < 0 {
			slz += SectorSize
		}

		bx, by, bz := slx/BrickSize, sly/BrickSize, slz/BrickSize
		brick := sector.GetBrick(bx, by, bz)
		if brick == nil {
			res := x.stepToNext(p, rayDir, invDir, float32(BrickSize))
			t += float32(math.Max(float64(res), 0.001))
			continue
		}

		// Inside a brick, check microcells
		blx := slx % BrickSize
		bly := sly % BrickSize
		blz := slz % BrickSize

		mx, my, mz := blx/MicroSize, bly/MicroSize, blz/MicroSize
		microIdx := mx + my*4 + mz*16
		if (brick.OccupancyMask64 & (1 << microIdx)) == 0 {
			res := x.stepToNext(p, rayDir, invDir, float32(MicroSize))
			t += float32(math.Max(float64(res), 0.001))
			continue
		}

		// Inside a microcell, check voxels
		vx, vy, vz := blx, bly, blz
		paletteIdx := brick.Payload[vx][vy][vz]
		if paletteIdx != 0 {
			// Hit!
			vMin := mgl32.Vec3{
				float32(sx*SectorSize + bx*BrickSize + vx),
				float32(sy*SectorSize + by*BrickSize + vy),
				float32(sz*SectorSize + bz*BrickSize + vz),
			}
			vCenter := vMin.Add(mgl32.Vec3{0.5, 0.5, 0.5})
			pHit := rayOrigin.Add(rayDir.Mul(t))
			localP := pHit.Sub(vCenter)
			absP := mgl32.Vec3{float32(math.Abs(float64(localP.X()))), float32(math.Abs(float64(localP.Y()))), float32(math.Abs(float64(localP.Z())))}
			maxC := float32(math.Max(float64(absP.X()), math.Max(float64(absP.Y()), float64(absP.Z()))))

			normal := mgl32.Vec3{0, 0, 0}
			if absP.X() >= maxC-0.01 {
				if localP.X() > 0 {
					normal[0] = 1
				} else {
					normal[0] = -1
				}
			} else if absP.Y() >= maxC-0.01 {
				if localP.Y() > 0 {
					normal[1] = 1
				} else {
					normal[1] = -1
				}
			} else if absP.Z() >= maxC-0.01 {
				if localP.Z() > 0 {
					normal[2] = 1
				} else {
					normal[2] = -1
				}
			}

			return true, t, [3]int{int(flX), int(flY), int(flZ)}, normal
		}

		res := x.stepToNext(p, rayDir, invDir, 1.0)
		t += float32(math.Max(float64(res), 0.001))
	}

	if iterations >= maxIterations {
		fmt.Printf("WARNING: Max iterations reached in RayMarch (t=%f, tMax=%f)\n", t, tMax)
	}

	return false, t, [3]int{}, mgl32.Vec3{}
}

func (x *XBrickMap) stepToNext(p, dir, invDir mgl32.Vec3, size float32) float32 {
	res := float32(1e10)
	for i := 0; i < 3; i++ {
		if dir[i] == 0 {
			continue
		}

		var dist float32
		if dir[i] > 0 {
			// Distance to next whole size boundary
			dist = (float32(math.Floor(float64(p[i]/size+1e-6)))+1)*size - p[i]
		} else {
			// Distance to previous whole size boundary
			dist = (float32(math.Floor(float64(p[i]/size-1e-6))))*size - p[i]
		}

		tVal := dist * invDir[i]
		if tVal > 1e-6 && tVal < res {
			res = tVal
		}
	}
	// Add a tiny extra bit to ensure we actually cross the boundary in the next iteration
	if res < 1e10 {
		return res + 1e-4
	}
	return res
}
