package gekko

import (
	"fmt"
	"math"
	"sort"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// VoxelBackingProvider classifies immutable base matter in global voxel
// coordinates. XBrickMap remains the live geometry authority; providers are
// consulted only when an edit reaches a brick that has not been explicit yet.
type VoxelBackingProvider interface {
	VoxelValue(global [3]int) uint8
	Bounds() (min [3]int, max [3]int)
}

// VoxelBackingComponent attaches one finite chunk of an immutable backing to a
// voxel entity. Removals are local brick coordinates and are removal-only.
type VoxelBackingComponent struct {
	OwnerKind  string
	OwnerID    string
	SourceHash string
	ChunkCoord [3]int
	ChunkSize  int
	BoundsMin  [3]int
	BoundsMax  [3]int
	Provider   VoxelBackingProvider
	Removals   map[[3]int][16]uint32
	Dirty      bool
}

func NewPlaneTreeVoxelBacking(def *content.VoxelBackingDef) (VoxelBackingProvider, error) {
	if err := content.ValidateVoxelBacking(def); err != nil {
		return nil, err
	}
	return &planeTreeVoxelBacking{def: def}, nil
}

func NewTerrainColumnVoxelBacking(chunk *content.TerrainChunkDef) VoxelBackingProvider {
	if chunk == nil || chunk.ChunkSize <= 0 {
		return nil
	}
	heights := make(map[[2]int]int, len(chunk.Columns))
	maxHeight := 0
	for _, column := range chunk.Columns {
		heights[[2]int{column.X, column.Z}] = column.FilledVoxels
		maxHeight = max(maxHeight, column.FilledVoxels)
	}
	origin := [3]int{
		chunk.Coord.X * chunk.ChunkSize,
		chunk.Coord.Y * chunk.ChunkSize,
		chunk.Coord.Z * chunk.ChunkSize,
	}
	return &terrainColumnVoxelBacking{
		origin:     origin,
		chunkSize:  chunk.ChunkSize,
		solidValue: chunk.SolidValue,
		heights:    heights,
		maxHeight:  maxHeight,
	}
}

func NewVoxelBackingComponent(ownerKind, ownerID, sourceHash string, coord [3]int, chunkSize int, provider VoxelBackingProvider, removal *content.VoxelBackingRemovalDef) *VoxelBackingComponent {
	if provider == nil || chunkSize <= 0 {
		return nil
	}
	component := &VoxelBackingComponent{
		OwnerKind:  ownerKind,
		OwnerID:    ownerID,
		SourceHash: sourceHash,
		ChunkCoord: coord,
		ChunkSize:  chunkSize,
		BoundsMax:  [3]int{chunkSize, chunkSize, chunkSize},
		Provider:   provider,
		Removals:   make(map[[3]int][16]uint32),
	}
	if ownerKind == content.VoxelBackingOwnerTerrain {
		providerMin, providerMax := provider.Bounds()
		origin := [3]int{coord[0] * chunkSize, coord[1] * chunkSize, coord[2] * chunkSize}
		for axis := 0; axis < 3; axis++ {
			component.BoundsMin[axis] = providerMin[axis] - origin[axis]
			component.BoundsMax[axis] = providerMax[axis] - origin[axis]
		}
	}
	if removal != nil && removal.OwnerKind == ownerKind && removal.OwnerID == ownerID && removal.SourceHash == sourceHash {
		for _, brick := range removal.Bricks {
			component.Removals[brick.Coord] = brick.Bits
		}
	}
	return component
}

func (component *VoxelBackingComponent) RemovalDef() content.VoxelBackingRemovalDef {
	def := content.VoxelBackingRemovalDef{
		OwnerKind:  component.OwnerKind,
		OwnerID:    component.OwnerID,
		SourceHash: component.SourceHash,
		ChunkCoord: content.TerrainChunkCoordDef{X: component.ChunkCoord[0], Y: component.ChunkCoord[1], Z: component.ChunkCoord[2]},
	}
	coords := make([][3]int, 0, len(component.Removals))
	for coord, bits := range component.Removals {
		if voxelRemovalBitsEmpty(bits) {
			continue
		}
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		if coords[i][0] != coords[j][0] {
			return coords[i][0] < coords[j][0]
		}
		if coords[i][1] != coords[j][1] {
			return coords[i][1] < coords[j][1]
		}
		return coords[i][2] < coords[j][2]
	})
	for _, coord := range coords {
		def.Bricks = append(def.Bricks, content.VoxelBackingRemovalBrickDef{Coord: coord, Bits: component.Removals[coord]})
	}
	return def
}

// ApplyRemovals reconstructs touched backing bricks and reapplies saved holes.
func (component *VoxelBackingComponent) ApplyRemovals(xbm *volume.XBrickMap) {
	if component == nil || xbm == nil {
		return
	}
	for brickCoord, words := range component.Removals {
		component.materializeBrick(xbm, brickCoord)
		for index := 0; index < volume.BrickSize*volume.BrickSize*volume.BrickSize; index++ {
			if words[index/32]&(uint32(1)<<uint(index%32)) == 0 {
				continue
			}
			local := voxelIndexInBrick(index)
			xbm.SetVoxel(
				brickCoord[0]*volume.BrickSize+local[0],
				brickCoord[1]*volume.BrickSize+local[1],
				brickCoord[2]*volume.BrickSize+local[2],
				0,
			)
		}
	}
}

func (component *VoxelBackingComponent) MaterializeSphere(xbm *volume.XBrickMap, center mgl32.Vec3, radius float32) bool {
	if component == nil || component.Provider == nil || xbm == nil || component.ChunkSize <= 0 || radius < 0 {
		return false
	}
	minVoxel, maxVoxel := voxelSphereIntegerBounds(center, radius)
	minBrick := [3]int{
		floorDivVoxelBacking(minVoxel[0], volume.BrickSize),
		floorDivVoxelBacking(minVoxel[1], volume.BrickSize),
		floorDivVoxelBacking(minVoxel[2], volume.BrickSize),
	}
	maxBrick := [3]int{
		floorDivVoxelBacking(maxVoxel[0], volume.BrickSize),
		floorDivVoxelBacking(maxVoxel[1], volume.BrickSize),
		floorDivVoxelBacking(maxVoxel[2], volume.BrickSize),
	}
	for axis := 0; axis < 3; axis++ {
		minBrick[axis] = max(floorDivVoxelBacking(component.BoundsMin[axis], volume.BrickSize), minBrick[axis])
		maxBrick[axis] = min(floorDivVoxelBacking(component.BoundsMax[axis]-1, volume.BrickSize), maxBrick[axis])
	}
	for bx := minBrick[0]; bx <= maxBrick[0]; bx++ {
		for by := minBrick[1]; by <= maxBrick[1]; by++ {
			for bz := minBrick[2]; bz <= maxBrick[2]; bz++ {
				component.materializeBrick(xbm, [3]int{bx, by, bz})
			}
		}
	}

	changed := false
	radius2 := radius * radius
	for x := max(component.BoundsMin[0], minVoxel[0]); x <= min(component.BoundsMax[0]-1, maxVoxel[0]); x++ {
		for y := max(component.BoundsMin[1], minVoxel[1]); y <= min(component.BoundsMax[1]-1, maxVoxel[1]); y++ {
			for z := max(component.BoundsMin[2], minVoxel[2]); z <= min(component.BoundsMax[2]-1, maxVoxel[2]); z++ {
				dx := float32(x) - center.X() + 0.5
				dy := float32(y) - center.Y() + 0.5
				dz := float32(z) - center.Z() + 0.5
				if dx*dx+dy*dy+dz*dz > radius2 {
					continue
				}
				changed = component.markRemoved([3]int{x, y, z}) || changed
			}
		}
	}
	component.Dirty = component.Dirty || changed
	return changed
}

func (component *VoxelBackingComponent) materializeBrick(xbm *volume.XBrickMap, brickCoord [3]int) {
	chunkOrigin := [3]int{
		component.ChunkCoord[0] * component.ChunkSize,
		component.ChunkCoord[1] * component.ChunkSize,
		component.ChunkCoord[2] * component.ChunkSize,
	}
	for x := 0; x < volume.BrickSize; x++ {
		localX := brickCoord[0]*volume.BrickSize + x
		if localX < component.BoundsMin[0] || localX >= component.BoundsMax[0] {
			continue
		}
		for y := 0; y < volume.BrickSize; y++ {
			localY := brickCoord[1]*volume.BrickSize + y
			if localY < component.BoundsMin[1] || localY >= component.BoundsMax[1] {
				continue
			}
			for z := 0; z < volume.BrickSize; z++ {
				localZ := brickCoord[2]*volume.BrickSize + z
				if localZ < component.BoundsMin[2] || localZ >= component.BoundsMax[2] {
					continue
				}
				local := [3]int{localX, localY, localZ}
				if component.removed(local) {
					xbm.SetVoxel(localX, localY, localZ, 0)
					continue
				}
				value := component.Provider.VoxelValue([3]int{chunkOrigin[0] + localX, chunkOrigin[1] + localY, chunkOrigin[2] + localZ})
				if value != 0 {
					if found, _ := xbm.GetVoxel(localX, localY, localZ); !found {
						xbm.SetVoxel(localX, localY, localZ, value)
					}
				}
			}
		}
	}
}

func (component *VoxelBackingComponent) removed(local [3]int) bool {
	brickCoord, index := voxelRemovalAddress(local)
	bits := component.Removals[brickCoord]
	return bits[index/32]&(uint32(1)<<uint(index%32)) != 0
}

func (component *VoxelBackingComponent) markRemoved(local [3]int) bool {
	brickCoord, index := voxelRemovalAddress(local)
	bits := component.Removals[brickCoord]
	mask := uint32(1) << uint(index%32)
	if bits[index/32]&mask != 0 {
		return false
	}
	bits[index/32] |= mask
	component.Removals[brickCoord] = bits
	return true
}

func voxelBackingForEntity(cmd *Commands, eid EntityId) (*VoxelBackingComponent, bool) {
	if cmd == nil {
		return nil, false
	}
	for _, value := range cmd.GetAllComponents(eid) {
		if component, ok := value.(*VoxelBackingComponent); ok {
			return component, true
		}
	}
	return nil, false
}

func voxelBackingEventIntersects(component *VoxelBackingComponent, tr *core.Transform, center mgl32.Vec3, radius float32) bool {
	if component == nil || tr == nil {
		return false
	}
	localCenter, localRadius := voxelBackingLocalSphere(tr, center, radius)
	return localCenter.X()+localRadius >= float32(component.BoundsMin[0]) && localCenter.Y()+localRadius >= float32(component.BoundsMin[1]) && localCenter.Z()+localRadius >= float32(component.BoundsMin[2]) &&
		localCenter.X()-localRadius < float32(component.BoundsMax[0]) && localCenter.Y()-localRadius < float32(component.BoundsMax[1]) && localCenter.Z()-localRadius < float32(component.BoundsMax[2])
}

func voxelBackingLocalSphere(tr *core.Transform, worldCenter mgl32.Vec3, radius float32) (mgl32.Vec3, float32) {
	center := tr.WorldToObject().Mul4x1(worldCenter.Vec4(1)).Vec3()
	averageScale := (tr.Scale.X() + tr.Scale.Y() + tr.Scale.Z()) / 3
	if averageScale == 0 {
		averageScale = 1
	}
	return center, radius / averageScale
}

type planeTreeVoxelBacking struct {
	def *content.VoxelBackingDef
}

func (backing *planeTreeVoxelBacking) Bounds() ([3]int, [3]int) {
	return backing.def.BoundsMin, backing.def.BoundsMax
}

func (backing *planeTreeVoxelBacking) VoxelValue(global [3]int) uint8 {
	minBound, maxBound := backing.Bounds()
	for axis := 0; axis < 3; axis++ {
		if global[axis] < minBound[axis] || global[axis] >= maxBound[axis] {
			return 0
		}
	}
	point := [3]float32{float32(global[0]) + 0.5, float32(global[1]) + 0.5, float32(global[2]) + 0.5}
	tree := backing.def.PlaneTree
	nodeID := tree.Root
	for steps := 0; steps <= len(tree.Nodes); steps++ {
		if nodeID < 0 {
			leafID := int(^nodeID)
			if leafID >= 0 && leafID < len(tree.Leaves) && tree.Leaves[leafID].Solid {
				return backing.def.SolidValue
			}
			return 0
		}
		if int(nodeID) >= len(tree.Nodes) {
			return 0
		}
		node := tree.Nodes[nodeID]
		if int(node.Plane) >= len(tree.Planes) {
			return 0
		}
		plane := tree.Planes[node.Plane]
		distance := point[0]*plane.Normal[0] + point[1]*plane.Normal[1] + point[2]*plane.Normal[2] - plane.Distance
		child := 1
		if distance >= 0 {
			child = 0
		}
		nodeID = node.Children[child]
	}
	return 0
}

type terrainColumnVoxelBacking struct {
	origin     [3]int
	chunkSize  int
	solidValue uint8
	heights    map[[2]int]int
	maxHeight  int
}

func (backing *terrainColumnVoxelBacking) Bounds() ([3]int, [3]int) {
	return backing.origin, [3]int{backing.origin[0] + backing.chunkSize, backing.origin[1] + backing.maxHeight, backing.origin[2] + backing.chunkSize}
}

func (backing *terrainColumnVoxelBacking) VoxelValue(global [3]int) uint8 {
	localX := global[0] - backing.origin[0]
	localY := global[1] - backing.origin[1]
	localZ := global[2] - backing.origin[2]
	if localX < 0 || localY < 0 || localZ < 0 || localX >= backing.chunkSize || localZ >= backing.chunkSize || localY >= backing.heights[[2]int{localX, localZ}] {
		return 0
	}
	return backing.solidValue
}

func voxelSphereIntegerBounds(center mgl32.Vec3, radius float32) ([3]int, [3]int) {
	return [3]int{
			int(math.Floor(float64(center.X() - radius))),
			int(math.Floor(float64(center.Y() - radius))),
			int(math.Floor(float64(center.Z() - radius))),
		}, [3]int{
			int(math.Ceil(float64(center.X() + radius))),
			int(math.Ceil(float64(center.Y() + radius))),
			int(math.Ceil(float64(center.Z() + radius))),
		}
}

func voxelRemovalAddress(local [3]int) ([3]int, int) {
	brick := [3]int{
		floorDivVoxelBacking(local[0], volume.BrickSize),
		floorDivVoxelBacking(local[1], volume.BrickSize),
		floorDivVoxelBacking(local[2], volume.BrickSize),
	}
	within := [3]int{
		local[0] - brick[0]*volume.BrickSize,
		local[1] - brick[1]*volume.BrickSize,
		local[2] - brick[2]*volume.BrickSize,
	}
	return brick, within[0] + volume.BrickSize*(within[1]+volume.BrickSize*within[2])
}

func voxelIndexInBrick(index int) [3]int {
	x := index % volume.BrickSize
	y := (index / volume.BrickSize) % volume.BrickSize
	z := index / (volume.BrickSize * volume.BrickSize)
	return [3]int{x, y, z}
}

func voxelRemovalBitsEmpty(bits [16]uint32) bool {
	for _, word := range bits {
		if word != 0 {
			return false
		}
	}
	return true
}

func floorDivVoxelBacking(value, divisor int) int {
	if divisor <= 0 {
		panic(fmt.Sprintf("invalid voxel backing divisor %d", divisor))
	}
	quotient := value / divisor
	if value < 0 && value%divisor != 0 {
		quotient--
	}
	return quotient
}
