package gekko

import (
	"fmt"
	"iter"
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
	// ponytail: one material per removal brick; use sparse per-voxel hints if mixed-material reload artifacts appear.
	Materials           map[[3]int]uint8
	surfaceSupportSeeds map[int]struct{}
	Dirty               bool
}

func NewPlaneTreeVoxelBacking(def *content.VoxelBackingDef) (VoxelBackingProvider, error) {
	if err := content.ValidateVoxelBacking(def); err != nil {
		return nil, err
	}
	backing := &planeTreeVoxelBacking{def: def}
	backing.indexSurfaceSupports()
	return backing, nil
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
		OwnerKind:           ownerKind,
		OwnerID:             ownerID,
		SourceHash:          sourceHash,
		ChunkCoord:          coord,
		ChunkSize:           chunkSize,
		BoundsMax:           [3]int{chunkSize, chunkSize, chunkSize},
		Provider:            provider,
		Removals:            make(map[[3]int][16]uint32),
		Materials:           make(map[[3]int]uint8),
		surfaceSupportSeeds: make(map[int]struct{}),
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
			if brick.Material != 0 {
				component.Materials[brick.Coord] = brick.Material
			}
		}
	}
	component.indexSurfaceSupportSeeds()
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
		def.Bricks = append(def.Bricks, content.VoxelBackingRemovalBrickDef{Coord: coord, Bits: component.Removals[coord], Material: component.Materials[coord]})
	}
	return def
}

// ApplyRemovals reconstructs removed voxels and their exposed face neighbors.
func (component *VoxelBackingComponent) ApplyRemovals(xbm *volume.XBrickMap) {
	if component == nil || xbm == nil {
		return
	}
	removed := component.removedVoxels()
	surfaceSupports := component.activeSurfaceSupports(mgl32.Vec3{}, 0, removed)
	materials := make([]uint8, len(removed))
	for i, local := range removed {
		materials[i] = voxelBackingMaterialHint(xbm, local)
		if materials[i] == 0 {
			brick, _ := voxelRemovalAddress(local)
			materials[i] = component.Materials[brick]
		}
	}
	component.applyMaterializationWrites(xbm, func(yield func(volume.VoxelWrite) bool) {
		for i, local := range removed {
			for _, offset := range voxelBackingCarveShellOffsets {
				write, ok := component.materializationWrite(xbm, [3]int{local[0] + offset[0], local[1] + offset[1], local[2] + offset[2]}, materials[i], surfaceSupports)
				if ok && !yield(write) {
					return
				}
			}
		}
	})
}

func (component *VoxelBackingComponent) MaterializeSphere(xbm *volume.XBrickMap, center mgl32.Vec3, radius float32) bool {
	return component.materializeSphere(xbm, center, radius, 0)
}

func (component *VoxelBackingComponent) materializeSphere(xbm *volume.XBrickMap, center mgl32.Vec3, radius float32, eventMaterial uint8) bool {
	if component == nil || component.Provider == nil || xbm == nil || component.ChunkSize <= 0 || radius < 0 {
		return false
	}
	minVoxel, maxVoxel := voxelSphereIntegerBounds(center, radius)

	radius2 := radius * radius
	carveVoxels := make([][3]int, 0)
	continued := make([][3]int, 0)
	for x := max(component.BoundsMin[0]-1, minVoxel[0]); x <= min(component.BoundsMax[0], maxVoxel[0]); x++ {
		for y := max(component.BoundsMin[1]-1, minVoxel[1]); y <= min(component.BoundsMax[1], maxVoxel[1]); y++ {
			for z := max(component.BoundsMin[2]-1, minVoxel[2]); z <= min(component.BoundsMax[2], maxVoxel[2]); z++ {
				dx := float32(x) - center.X() + 0.5
				dy := float32(y) - center.Y() + 0.5
				dz := float32(z) - center.Z() + 0.5
				if dx*dx+dy*dy+dz*dz > radius2 {
					continue
				}
				local := [3]int{x, y, z}
				global := [3]int{
					component.ChunkCoord[0]*component.ChunkSize + x,
					component.ChunkCoord[1]*component.ChunkSize + y,
					component.ChunkCoord[2]*component.ChunkSize + z,
				}
				if component.removed(local) || component.Provider.VoxelValue(global) == 0 {
					continue
				}
				carveVoxels = append(carveVoxels, local)
				for _, offset := range voxelBackingCarveShellOffsets {
					neighbor := [3]int{local[0] + offset[0], local[1] + offset[1], local[2] + offset[2]}
					if component.removed(neighbor) {
						continued = append(continued, neighbor)
					}
				}
			}
		}
	}
	surfaceSupports := component.activeSurfaceSupports(center, radius, continued)
	changed := false
	// One virtual layer records shell-only neighbor chunks for reload.
	component.applyMaterializationWrites(xbm, func(yield func(volume.VoxelWrite) bool) {
		for _, local := range carveVoxels {
			material := voxelBackingMaterialHint(xbm, local)
			if material == 0 {
				material = eventMaterial
			}
			for _, offset := range voxelBackingCarveShellOffsets {
				write, ok := component.materializationWrite(xbm, [3]int{local[0] + offset[0], local[1] + offset[1], local[2] + offset[2]}, material, surfaceSupports)
				if ok && !yield(write) {
					return
				}
			}
			changed = component.markRemoved(local, material) || changed
		}
	})
	component.Dirty = component.Dirty || changed
	return changed
}

var voxelBackingCarveShellOffsets = [...][3]int{
	{0, 0, 0},
	{-1, 0, 0},
	{1, 0, 0},
	{0, -1, 0},
	{0, 1, 0},
	{0, 0, -1},
	{0, 0, 1},
}

func (component *VoxelBackingComponent) materializeVoxel(xbm *volume.XBrickMap, local [3]int, material uint8, surfaceSupports map[int]struct{}) {
	if write, ok := component.materializationWrite(xbm, local, material, surfaceSupports); ok {
		xbm.SetVoxel(write.X, write.Y, write.Z, write.Value)
	}
}

// Only built-in immutable classifiers use batched materialization writes.
// Arbitrary providers can inspect or reenter the map from classification calls,
// so their writes retain complete sequential SetVoxel finalization.
func (component *VoxelBackingComponent) applyMaterializationWrites(xbm *volume.XBrickMap, writes iter.Seq[volume.VoxelWrite]) {
	switch component.Provider.(type) {
	case *planeTreeVoxelBacking, *terrainColumnVoxelBacking:
		xbm.ApplyVoxelWrites(writes)
	default:
		for write := range writes {
			xbm.SetVoxel(write.X, write.Y, write.Z, write.Value)
		}
	}
}

func (component *VoxelBackingComponent) materializationWrite(xbm *volume.XBrickMap, local [3]int, material uint8, surfaceSupports map[int]struct{}) (volume.VoxelWrite, bool) {
	if component == nil || component.Provider == nil || xbm == nil {
		return volume.VoxelWrite{}, false
	}
	for axis := 0; axis < 3; axis++ {
		if local[axis] < component.BoundsMin[axis] || local[axis] >= component.BoundsMax[axis] {
			return volume.VoxelWrite{}, false
		}
	}
	if component.removed(local) {
		return volume.VoxelWrite{X: local[0], Y: local[1], Z: local[2]}, true
	}
	chunkOrigin := [3]int{
		component.ChunkCoord[0] * component.ChunkSize,
		component.ChunkCoord[1] * component.ChunkSize,
		component.ChunkCoord[2] * component.ChunkSize,
	}
	global := [3]int{chunkOrigin[0] + local[0], chunkOrigin[1] + local[1], chunkOrigin[2] + local[2]}
	var value uint8
	if backing, ok := component.Provider.(*planeTreeVoxelBacking); ok {
		value = backing.voxelValueForMaterialization(global, surfaceSupports)
	} else {
		value = component.Provider.VoxelValue(global)
	}
	if value != 0 {
		if material != 0 {
			value = material
		}
		if found, _ := xbm.GetVoxel(local[0], local[1], local[2]); !found {
			return volume.VoxelWrite{X: local[0], Y: local[1], Z: local[2], Value: value}, true
		}
	}
	return volume.VoxelWrite{}, false
}

func (component *VoxelBackingComponent) activeSurfaceSupports(center mgl32.Vec3, radius float32, continued [][3]int) map[int]struct{} {
	backing, ok := component.Provider.(*planeTreeVoxelBacking)
	if !ok || len(backing.surfaceSupports) == 0 {
		return nil
	}
	origin := mgl32.Vec3{
		float32(component.ChunkCoord[0] * component.ChunkSize),
		float32(component.ChunkCoord[1] * component.ChunkSize),
		float32(component.ChunkCoord[2] * component.ChunkSize),
	}
	globalContinued := make([][3]int, len(continued))
	for i, local := range continued {
		globalContinued[i] = [3]int{
			component.ChunkCoord[0]*component.ChunkSize + local[0],
			component.ChunkCoord[1]*component.ChunkSize + local[1],
			component.ChunkCoord[2]*component.ChunkSize + local[2],
		}
	}
	active := backing.activeSurfaceSupports(center.Add(origin), radius, globalContinued, component.surfaceSupportSeeds)
	for index := range active {
		component.surfaceSupportSeeds[index] = struct{}{}
	}
	return active
}

func (component *VoxelBackingComponent) indexSurfaceSupportSeeds() {
	backing, ok := component.Provider.(*planeTreeVoxelBacking)
	if !ok || len(backing.surfaceSupports) == 0 {
		return
	}
	chunkOrigin := [3]int{
		component.ChunkCoord[0] * component.ChunkSize,
		component.ChunkCoord[1] * component.ChunkSize,
		component.ChunkCoord[2] * component.ChunkSize,
	}
	for _, local := range component.removedVoxels() {
		global := [3]int{chunkOrigin[0] + local[0], chunkOrigin[1] + local[1], chunkOrigin[2] + local[2]}
		for _, index := range backing.surfaceSupportsNearSurface(global) {
			component.surfaceSupportSeeds[index] = struct{}{}
		}
	}
}

func (component *VoxelBackingComponent) removedVoxels() [][3]int {
	removed := make([][3]int, 0)
	for brickCoord, words := range component.Removals {
		for index := 0; index < volume.BrickSize*volume.BrickSize*volume.BrickSize; index++ {
			if words[index/32]&(uint32(1)<<uint(index%32)) == 0 {
				continue
			}
			within := voxelIndexInBrick(index)
			removed = append(removed, [3]int{
				brickCoord[0]*volume.BrickSize + within[0],
				brickCoord[1]*volume.BrickSize + within[1],
				brickCoord[2]*volume.BrickSize + within[2],
			})
		}
	}
	sort.Slice(removed, func(i, j int) bool {
		for axis := 0; axis < 3; axis++ {
			if removed[i][axis] != removed[j][axis] {
				return removed[i][axis] < removed[j][axis]
			}
		}
		return false
	})
	return removed
}

func voxelBackingMaterialHint(xbm *volume.XBrickMap, local [3]int) uint8 {
	for _, offset := range voxelBackingCarveShellOffsets {
		if found, value := xbm.GetVoxel(local[0]+offset[0], local[1]+offset[1], local[2]+offset[2]); found {
			return value
		}
	}
	return 0
}

func (component *VoxelBackingComponent) removed(local [3]int) bool {
	brickCoord, index := voxelRemovalAddress(local)
	bits := component.Removals[brickCoord]
	return bits[index/32]&(uint32(1)<<uint(index%32)) != 0
}

func (component *VoxelBackingComponent) markRemoved(local [3]int, material uint8) bool {
	brickCoord, index := voxelRemovalAddress(local)
	bits := component.Removals[brickCoord]
	mask := uint32(1) << uint(index%32)
	changed := false
	if bits[index/32]&mask == 0 {
		bits[index/32] |= mask
		component.Removals[brickCoord] = bits
		changed = true
	}
	if material != 0 && component.Materials[brickCoord] == 0 {
		component.Materials[brickCoord] = material
		changed = true
	}
	return changed
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
	localRadius++
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
	def             *content.VoxelBackingDef
	surfaceSupports []voxelBackingSurfaceSupport
	supportCells    map[[3]int][]int
}

type voxelBackingSurfaceSupport struct {
	vertices  [3]mgl32.Vec3
	normal    mgl32.Vec3
	depth     float32
	tolerance [3]float32
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
	if backing.planeTreeSolid(global) || backing.surfaceSupportSolid(global) {
		return backing.def.SolidValue
	}
	return 0
}

func (backing *planeTreeVoxelBacking) voxelValueForMaterialization(global [3]int, activeSurfaceSupports map[int]struct{}) uint8 {
	minBound, maxBound := backing.Bounds()
	for axis := 0; axis < 3; axis++ {
		if global[axis] < minBound[axis] || global[axis] >= maxBound[axis] {
			return 0
		}
	}
	if backing.planeTreeSolid(global) || backing.surfaceSupportSolidScoped(global, activeSurfaceSupports) {
		return backing.def.SolidValue
	}
	return 0
}

func (backing *planeTreeVoxelBacking) planeTreeSolid(global [3]int) bool {
	tree := backing.def.PlaneTree
	if len(tree.Volumes) == 0 {
		return backing.planeTreeRootSolid(global, tree.Root)
	}
	for _, volume := range tree.Volumes {
		inside := true
		for axis := 0; axis < 3; axis++ {
			if global[axis] < volume.BoundsMin[axis] || global[axis] >= volume.BoundsMax[axis] {
				inside = false
				break
			}
		}
		if inside && backing.planeTreeRootSolid(global, volume.Root) {
			return true
		}
	}
	return false
}

func (backing *planeTreeVoxelBacking) planeTreeRootSolid(global [3]int, root int32) bool {
	point := [3]float32{float32(global[0]) + 0.5, float32(global[1]) + 0.5, float32(global[2]) + 0.5}
	tree := backing.def.PlaneTree
	nodeID := root
	for steps := 0; steps <= len(tree.Nodes); steps++ {
		if nodeID < 0 {
			leafID := int(^nodeID)
			if leafID >= 0 && leafID < len(tree.Leaves) && tree.Leaves[leafID].Solid {
				return true
			}
			return false
		}
		if int(nodeID) >= len(tree.Nodes) {
			return false
		}
		node := tree.Nodes[nodeID]
		if int(node.Plane) >= len(tree.Planes) {
			return false
		}
		plane := tree.Planes[node.Plane]
		distance := point[0]*plane.Normal[0] + point[1]*plane.Normal[1] + point[2]*plane.Normal[2] - plane.Distance
		child := 1
		if distance >= 0 {
			child = 0
		}
		nodeID = node.Children[child]
	}
	return false
}

func (backing *planeTreeVoxelBacking) indexSurfaceSupports() {
	if backing == nil || backing.def == nil || len(backing.def.SurfaceSupports) == 0 {
		return
	}
	const cellSize = 16
	backing.supportCells = make(map[[3]int][]int)
	for _, authored := range backing.def.SurfaceSupports {
		support := voxelBackingSurfaceSupport{
			vertices: [3]mgl32.Vec3{
				{authored.Vertices[0][0], authored.Vertices[0][1], authored.Vertices[0][2]},
				{authored.Vertices[1][0], authored.Vertices[1][1], authored.Vertices[1][2]},
				{authored.Vertices[2][0], authored.Vertices[2][1], authored.Vertices[2][2]},
			},
			normal: mgl32.Vec3{authored.Normal[0], authored.Normal[1], authored.Normal[2]}.Normalize(),
			depth:  authored.Depth,
		}
		area2 := support.vertices[1].Sub(support.vertices[0]).Cross(support.vertices[2].Sub(support.vertices[0])).Len()
		for vertex := range support.tolerance {
			opposite := support.vertices[(vertex+2)%3].Sub(support.vertices[(vertex+1)%3]).Len()
			altitude := area2 / max(opposite, 1e-6)
			support.tolerance[vertex] = min(1, 0.75/max(altitude, 1e-6))
		}
		index := len(backing.surfaceSupports)
		backing.surfaceSupports = append(backing.surfaceSupports, support)

		minVoxel := mgl32.Vec3{float32(math.MaxFloat32), float32(math.MaxFloat32), float32(math.MaxFloat32)}
		maxVoxel := mgl32.Vec3{-float32(math.MaxFloat32), -float32(math.MaxFloat32), -float32(math.MaxFloat32)}
		inward := support.normal.Mul(-support.depth)
		for _, vertex := range support.vertices {
			extruded := vertex.Add(inward)
			for axis := 0; axis < 3; axis++ {
				minVoxel[axis] = min(minVoxel[axis], vertex[axis], extruded[axis])
				maxVoxel[axis] = max(maxVoxel[axis], vertex[axis], extruded[axis])
			}
		}
		minCell := [3]int{}
		maxCell := [3]int{}
		for axis := 0; axis < 3; axis++ {
			minCell[axis] = floorDivVoxelBacking(int(math.Floor(float64(minVoxel[axis]-1))), cellSize)
			maxCell[axis] = floorDivVoxelBacking(int(math.Floor(float64(maxVoxel[axis]+1))), cellSize)
		}
		for x := minCell[0]; x <= maxCell[0]; x++ {
			for y := minCell[1]; y <= maxCell[1]; y++ {
				for z := minCell[2]; z <= maxCell[2]; z++ {
					cell := [3]int{x, y, z}
					backing.supportCells[cell] = append(backing.supportCells[cell], index)
				}
			}
		}
	}
}

func (backing *planeTreeVoxelBacking) surfaceSupportSolid(global [3]int) bool {
	if len(backing.surfaceSupports) == 0 {
		return false
	}
	const cellSize = 16
	cell := [3]int{
		floorDivVoxelBacking(global[0], cellSize),
		floorDivVoxelBacking(global[1], cellSize),
		floorDivVoxelBacking(global[2], cellSize),
	}
	point := mgl32.Vec3{float32(global[0]) + 0.5, float32(global[1]) + 0.5, float32(global[2]) + 0.5}
	for _, index := range backing.supportCells[cell] {
		if backing.surfaceSupports[index].contains(point) {
			return true
		}
	}
	return false
}

func (backing *planeTreeVoxelBacking) surfaceSupportSolidScoped(global [3]int, active map[int]struct{}) bool {
	if len(active) == 0 {
		return false
	}
	const cellSize = 16
	cell := [3]int{
		floorDivVoxelBacking(global[0], cellSize),
		floorDivVoxelBacking(global[1], cellSize),
		floorDivVoxelBacking(global[2], cellSize),
	}
	point := mgl32.Vec3{float32(global[0]) + 0.5, float32(global[1]) + 0.5, float32(global[2]) + 0.5}
	for _, index := range backing.supportCells[cell] {
		if _, enabled := active[index]; enabled && backing.surfaceSupports[index].contains(point) {
			return true
		}
	}
	return false
}

func (backing *planeTreeVoxelBacking) activeSurfaceSupports(center mgl32.Vec3, radius float32, continued [][3]int, surfaceSeeds map[int]struct{}) map[int]struct{} {
	active := make(map[int]struct{})
	const cellSize = 16
	if radius > 0 {
		minCell := [3]int{}
		maxCell := [3]int{}
		for axis := 0; axis < 3; axis++ {
			minCell[axis] = floorDivVoxelBacking(int(math.Floor(float64(center[axis]-radius-1))), cellSize)
			maxCell[axis] = floorDivVoxelBacking(int(math.Floor(float64(center[axis]+radius+1))), cellSize)
		}
		candidates := make(map[int]struct{})
		for x := minCell[0]; x <= maxCell[0]; x++ {
			for y := minCell[1]; y <= maxCell[1]; y++ {
				for z := minCell[2]; z <= maxCell[2]; z++ {
					for _, index := range backing.supportCells[[3]int{x, y, z}] {
						candidates[index] = struct{}{}
					}
				}
			}
		}
		for index := range candidates {
			if backing.surfaceSupports[index].intersectsSurfaceSphere(center, radius) {
				active[index] = struct{}{}
			}
		}
	}
	for _, global := range continued {
		cell := [3]int{
			floorDivVoxelBacking(global[0], cellSize),
			floorDivVoxelBacking(global[1], cellSize),
			floorDivVoxelBacking(global[2], cellSize),
		}
		point := mgl32.Vec3{float32(global[0]) + 0.5, float32(global[1]) + 0.5, float32(global[2]) + 0.5}
		for _, index := range backing.supportCells[cell] {
			if _, seeded := surfaceSeeds[index]; seeded && backing.surfaceSupports[index].contains(point) {
				active[index] = struct{}{}
			}
		}
	}
	return active
}

func (backing *planeTreeVoxelBacking) surfaceSupportsNearSurface(global [3]int) []int {
	const cellSize = 16
	cell := [3]int{
		floorDivVoxelBacking(global[0], cellSize),
		floorDivVoxelBacking(global[1], cellSize),
		floorDivVoxelBacking(global[2], cellSize),
	}
	point := mgl32.Vec3{float32(global[0]) + 0.5, float32(global[1]) + 0.5, float32(global[2]) + 0.5}
	var result []int
	for _, index := range backing.supportCells[cell] {
		if backing.surfaceSupports[index].nearSurface(point) {
			result = append(result, index)
		}
	}
	return result
}

func (support voxelBackingSurfaceSupport) contains(point mgl32.Vec3) bool {
	normalMargin := 0.5 * (absf(support.normal.X()) + absf(support.normal.Y()) + absf(support.normal.Z()))
	signedDistance := point.Sub(support.vertices[0]).Dot(support.normal)
	if signedDistance > normalMargin || signedDistance < -support.depth-normalMargin {
		return false
	}
	return support.containsProjected(point.Sub(support.normal.Mul(signedDistance)))
}

func (support voxelBackingSurfaceSupport) intersectsSurfaceSphere(center mgl32.Vec3, radius float32) bool {
	normalMargin := 0.5 * (absf(support.normal.X()) + absf(support.normal.Y()) + absf(support.normal.Z()))
	signedDistance := center.Sub(support.vertices[0]).Dot(support.normal)
	if absf(signedDistance) > radius+normalMargin {
		return false
	}
	return support.containsProjected(center.Sub(support.normal.Mul(signedDistance)))
}

func (support voxelBackingSurfaceSupport) nearSurface(point mgl32.Vec3) bool {
	signedDistance := point.Sub(support.vertices[0]).Dot(support.normal)
	return signedDistance >= -1.5 && support.contains(point)
}

func (support voxelBackingSurfaceSupport) containsProjected(projected mgl32.Vec3) bool {
	v0 := support.vertices[1].Sub(support.vertices[0])
	v1 := support.vertices[2].Sub(support.vertices[0])
	v2 := projected.Sub(support.vertices[0])
	d00, d01, d11 := v0.Dot(v0), v0.Dot(v1), v1.Dot(v1)
	d20, d21 := v2.Dot(v0), v2.Dot(v1)
	denominator := d00*d11 - d01*d01
	if absf(denominator) <= 1e-8 {
		return false
	}
	v := (d11*d20 - d01*d21) / denominator
	w := (d00*d21 - d01*d20) / denominator
	u := 1 - v - w
	return u >= -support.tolerance[0] && v >= -support.tolerance[1] && w >= -support.tolerance[2]
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
