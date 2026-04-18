package volume

import (
	"math/bits"

	"github.com/go-gl/mathgl/mgl32"
)

const (
	BrickSize               = 8
	MicroSize               = 2
	SectorBricks            = 4
	SectorSize              = SectorBricks * BrickSize // 32
	DenseOccupancyWordCount = (BrickSize * BrickSize * BrickSize) / 32

	BrickFlagSolid           = 1
	BrickFlagUniformMaterial = 1 << 1
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
	b.Flags &^= BrickFlagSolid | BrickFlagUniformMaterial
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
	return b.RefreshMaterialFlags()
}

func (b *Brick) RefreshMaterialFlags() bool {
	b.Flags &^= BrickFlagSolid | BrickFlagUniformMaterial
	b.AtlasOffset = 0
	if b.IsEmpty() {
		return false
	}

	solidPalette := b.Payload[0][0][0]
	isSolid := solidPalette != 0
	var uniformPalette uint8
	hasOccupied := false

	for z := 0; z < BrickSize; z++ {
		for y := 0; y < BrickSize; y++ {
			for x := 0; x < BrickSize; x++ {
				val := b.Payload[x][y][z]
				if val == 0 {
					isSolid = false
					continue
				}
				if !hasOccupied {
					uniformPalette = val
					hasOccupied = true
				} else if val != uniformPalette {
					return false
				}
				if val != solidPalette {
					isSolid = false
				}
			}
		}
	}

	if isSolid {
		b.Flags |= BrickFlagSolid
		b.AtlasOffset = uint32(solidPalette)
		return true
	}

	if hasOccupied {
		b.Flags |= BrickFlagUniformMaterial
		b.AtlasOffset = uint32(uniformPalette)
	}
	return false
}

func (b *Brick) IsEmpty() bool {
	return b.OccupancyMask64 == 0
}

func denseOccupancyLinearIndex(x, y, z int) int {
	return x + y*BrickSize + z*BrickSize*BrickSize
}

func (b *Brick) DenseOccupancyWords() [DenseOccupancyWordCount]uint32 {
	var words [DenseOccupancyWordCount]uint32
	for z := 0; z < BrickSize; z++ {
		for y := 0; y < BrickSize; y++ {
			for x := 0; x < BrickSize; x++ {
				if b.Payload[x][y][z] == 0 {
					continue
				}
				linear := denseOccupancyLinearIndex(x, y, z)
				wordIdx := linear >> 5
				bitIdx := uint32(linear & 31)
				words[wordIdx] |= 1 << bitIdx
			}
		}
	}
	return words
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

var NextMapID uint32 = 1

type XBrickMap struct {
	ID           uint32
	Sectors      map[[3]int]*Sector
	DirtySectors map[[3]int]bool
	DirtyBricks  map[[6]int]bool

	AABBDirty      bool
	StructureDirty bool // True if bricks were added or removed
	CachedMin      mgl32.Vec3
	CachedMax      mgl32.Vec3

	// GPU editing
	GPUEditMode bool
	gpuManager  interface{} // *gpu.GpuBufferManager (avoid circular import)
}

func NewXBrickMap() *XBrickMap {
	id := NextMapID
	NextMapID++
	return &XBrickMap{
		ID:             id,
		Sectors:        make(map[[3]int]*Sector),
		DirtySectors:   make(map[[3]int]bool),
		DirtyBricks:    make(map[[6]int]bool),
		AABBDirty:      true,
		StructureDirty: true, // Initial state needs build
	}
}

func (x *XBrickMap) ClearDirty() {
	x.DirtySectors = make(map[[3]int]bool)
	x.DirtyBricks = make(map[[6]int]bool)
	x.StructureDirty = false
}
