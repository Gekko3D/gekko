package content

import (
	"fmt"
	"math"
	"sort"
)

type NavTileBuildOptions struct {
	NavID           string
	BuilderVersion  string
	SourceDeltaHash string
	NavBuildHash    string
	NeighborChunks  map[TerrainChunkCoordDef]*ImportedWorldChunkDef
}

type NavBuildWalkableCell struct {
	X         int
	Y         int
	Z         int
	PolygonID string
	Neighbors []string
}

type NavTileBuildResult struct {
	Tile          *NavTileDef
	WalkableCells []NavBuildWalkableCell
}

func BuildNavTileFromImportedWorldChunk(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, opts NavTileBuildOptions) (NavTileBuildResult, error) {
	if chunk == nil {
		return NavTileBuildResult{}, fmt.Errorf("imported world chunk is nil")
	}
	if chunk.ChunkSize <= 0 {
		return NavTileBuildResult{}, fmt.Errorf("imported world chunk_size must be positive")
	}
	if chunk.VoxelResolution <= 0 {
		return NavTileBuildResult{}, fmt.Errorf("imported world voxel_resolution must be positive")
	}
	EnsureNavAgentProfileDefaults(&profile)
	if profile.Radius <= 0 || profile.Height <= 0 || profile.StepHeight <= 0 {
		return NavTileBuildResult{}, fmt.Errorf("nav agent profile radius, height, and step_height must be positive")
	}

	occupancy := importedWorldChunkOccupancy(chunk)
	sampler := newImportedWorldNavOccupancySampler(chunk, occupancy, opts.NeighborChunks)
	cells := buildNavWalkableCells(chunk, profile, occupancy, sampler)
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].X != cells[j].X {
			return cells[i].X < cells[j].X
		}
		if cells[i].Z != cells[j].Z {
			return cells[i].Z < cells[j].Z
		}
		return cells[i].Y < cells[j].Y
	})

	polygonByCell := make(map[[2]int]int, len(cells))
	for i := range cells {
		polygonByCell[[2]int{cells[i].X, cells[i].Z}] = i
	}
	applyNavCellNeighbors(cells, polygonByCell, chunk.VoxelResolution, profile.StepHeight)

	navID := opts.NavID
	if navID == "" {
		navID = chunk.WorldID
	}
	if navID == "" {
		navID = newID()
	}
	builderVersion := opts.BuilderVersion
	if builderVersion == "" {
		builderVersion = DefaultNavBuilderVersion
	}
	origin := importedWorldChunkWorldOrigin(chunk)
	worldSize := float32(chunk.ChunkSize) * chunk.VoxelResolution
	tile := &NavTileDef{
		NavID:             navID,
		SchemaVersion:     CurrentNavTileSchemaVersion,
		Coord:             chunk.Coord,
		AgentProfileID:    profile.ID,
		BuilderVersion:    builderVersion,
		PayloadKind:       NavTilePayloadJSONV1,
		SourcePayloadHash: chunk.PayloadHash,
		SourceDeltaHash:   opts.SourceDeltaHash,
		NavBuildHash:      opts.NavBuildHash,
		BoundsMin:         [3]float32{origin[0], origin[1], origin[2]},
		BoundsMax:         [3]float32{origin[0] + worldSize, origin[1] + worldSize, origin[2] + worldSize},
	}
	for i := range cells {
		appendNavCellPolygon(tile, chunk, origin, &cells[i])
	}
	EnsureNavTileDefaults(tile)
	return NavTileBuildResult{Tile: tile, WalkableCells: cells}, nil
}

func importedWorldChunkOccupancy(chunk *ImportedWorldChunkDef) map[[3]int]struct{} {
	out := make(map[[3]int]struct{}, len(chunk.Voxels))
	if chunk == nil {
		return out
	}
	for _, voxel := range chunk.Voxels {
		if voxel.Value == 0 {
			continue
		}
		if voxel.X < 0 || voxel.Y < 0 || voxel.Z < 0 || voxel.X >= chunk.ChunkSize || voxel.Y >= chunk.ChunkSize || voxel.Z >= chunk.ChunkSize {
			continue
		}
		out[[3]int{voxel.X, voxel.Y, voxel.Z}] = struct{}{}
	}
	return out
}

func buildNavWalkableCells(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, occupancy map[[3]int]struct{}, sampler importedWorldNavOccupancySampler) []NavBuildWalkableCell {
	cells := make([]NavBuildWalkableCell, 0)
	topSolidByColumn := navTopSolidYByColumn(chunk)
	for x := 0; x < chunk.ChunkSize; x++ {
		for z := 0; z < chunk.ChunkSize; z++ {
			solidY, ok := topSolidByColumn[[2]int{x, z}]
			if !ok {
				continue
			}
			floorY := solidY + 1
			if navCellHasAgentClearance(chunk, profile, sampler, x, floorY, z) {
				id := navCellPolygonID(x, floorY, z)
				cells = append(cells, NavBuildWalkableCell{X: x, Y: floorY, Z: z, PolygonID: id})
			}
		}
	}
	return cells
}

func navTopSolidYByColumn(chunk *ImportedWorldChunkDef) map[[2]int]int {
	out := make(map[[2]int]int)
	if chunk == nil {
		return out
	}
	for _, voxel := range chunk.Voxels {
		if voxel.Value == 0 {
			continue
		}
		if voxel.X < 0 || voxel.Y < 0 || voxel.Z < 0 || voxel.X >= chunk.ChunkSize || voxel.Y >= chunk.ChunkSize || voxel.Z >= chunk.ChunkSize {
			continue
		}
		key := [2]int{voxel.X, voxel.Z}
		if y, ok := out[key]; !ok || voxel.Y > y {
			out[key] = voxel.Y
		}
	}
	return out
}

func navCellHasAgentClearance(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, sampler importedWorldNavOccupancySampler, x, floorY, z int) bool {
	clearanceVoxels := int(math.Ceil(float64(profile.Height / chunk.VoxelResolution)))
	if clearanceVoxels <= 0 {
		clearanceVoxels = 1
	}
	if floorY < 0 {
		return false
	}
	radiusVoxels := float64(profile.Radius / chunk.VoxelResolution)
	radiusCells := int(math.Ceil(radiusVoxels))
	centerX := float64(x) + 0.5
	centerZ := float64(z) + 0.5
	for oz := z - radiusCells; oz <= z+radiusCells; oz++ {
		for ox := x - radiusCells; ox <= x+radiusCells; ox++ {
			if !navVoxelAABBTouchesAgentRadius(centerX, centerZ, radiusVoxels, ox, oz) {
				continue
			}
			for oy := floorY; oy < floorY+clearanceVoxels; oy++ {
				blocked, known := sampler.Occupied(ox, oy, oz)
				if !known || blocked {
					return false
				}
			}
		}
	}
	return true
}

type importedWorldNavOccupancySampler struct {
	center       TerrainChunkCoordDef
	chunkSize    int
	resolution   float32
	chunks       map[TerrainChunkCoordDef]*ImportedWorldChunkDef
	occupancies  map[TerrainChunkCoordDef]map[[3]int]struct{}
	centerOffset TerrainChunkCoordDef
}

func newImportedWorldNavOccupancySampler(center *ImportedWorldChunkDef, centerOccupancy map[[3]int]struct{}, neighbors map[TerrainChunkCoordDef]*ImportedWorldChunkDef) importedWorldNavOccupancySampler {
	sampler := importedWorldNavOccupancySampler{
		chunks:      make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef),
		occupancies: make(map[TerrainChunkCoordDef]map[[3]int]struct{}),
	}
	if center == nil {
		return sampler
	}
	sampler.center = center.Coord
	sampler.chunkSize = center.ChunkSize
	sampler.resolution = center.VoxelResolution
	sampler.chunks[center.Coord] = center
	if centerOccupancy == nil {
		centerOccupancy = importedWorldChunkOccupancy(center)
	}
	sampler.occupancies[center.Coord] = centerOccupancy
	for coord, chunk := range neighbors {
		if chunk == nil {
			continue
		}
		if chunk.ChunkSize != center.ChunkSize || chunk.VoxelResolution != center.VoxelResolution {
			continue
		}
		if !navChunkCoordNear(center.Coord, coord) {
			continue
		}
		sampler.chunks[coord] = chunk
	}
	return sampler
}

func (s *importedWorldNavOccupancySampler) Occupied(x, y, z int) (bool, bool) {
	if s.chunkSize <= 0 {
		return false, false
	}
	coord := TerrainChunkCoordDef{
		X: s.center.X + navFloorDiv(x, s.chunkSize),
		Y: s.center.Y + navFloorDiv(y, s.chunkSize),
		Z: s.center.Z + navFloorDiv(z, s.chunkSize),
	}
	occupancy, ok := s.occupancy(coord)
	if !ok {
		return false, false
	}
	local := [3]int{navPositiveMod(x, s.chunkSize), navPositiveMod(y, s.chunkSize), navPositiveMod(z, s.chunkSize)}
	_, blocked := occupancy[local]
	return blocked, true
}

func (s *importedWorldNavOccupancySampler) occupancy(coord TerrainChunkCoordDef) (map[[3]int]struct{}, bool) {
	if s == nil {
		return nil, false
	}
	if occupancy, ok := s.occupancies[coord]; ok {
		return occupancy, true
	}
	chunk, ok := s.chunks[coord]
	if !ok || chunk == nil {
		return nil, false
	}
	occupancy := importedWorldChunkOccupancy(chunk)
	s.occupancies[coord] = occupancy
	return occupancy, true
}

func navChunkCoordNear(center TerrainChunkCoordDef, coord TerrainChunkCoordDef) bool {
	return absNavCoordDelta(center.X, coord.X) <= 1 &&
		absNavCoordDelta(center.Y, coord.Y) <= 1 &&
		absNavCoordDelta(center.Z, coord.Z) <= 1
}

func absNavCoordDelta(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

func navFloorDiv(v int, divisor int) int {
	if divisor <= 0 {
		return 0
	}
	q := v / divisor
	r := v % divisor
	if r != 0 && ((r < 0) != (divisor < 0)) {
		q--
	}
	return q
}

func navPositiveMod(v int, divisor int) int {
	if divisor <= 0 {
		return 0
	}
	m := v % divisor
	if m < 0 {
		m += divisor
	}
	return m
}

func navVoxelAABBTouchesAgentRadius(centerX, centerZ, radius float64, voxelX, voxelZ int) bool {
	closestX := clampFloat64(centerX, float64(voxelX), float64(voxelX+1))
	closestZ := clampFloat64(centerZ, float64(voxelZ), float64(voxelZ+1))
	dx := centerX - closestX
	dz := centerZ - closestZ
	return dx*dx+dz*dz < radius*radius
}

func clampFloat64(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func applyNavCellNeighbors(cells []NavBuildWalkableCell, polygonByCell map[[2]int]int, voxelResolution float32, stepHeight float32) {
	dirs := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	epsilon := float32(1e-4)
	for i := range cells {
		neighbors := make([]string, 0, 4)
		for _, dir := range dirs {
			j, ok := polygonByCell[[2]int{cells[i].X + dir[0], cells[i].Z + dir[1]}]
			if !ok {
				continue
			}
			delta := absNavInt(cells[i].Y-cells[j].Y) * voxelResolution
			if delta <= stepHeight+epsilon {
				neighbors = append(neighbors, cells[j].PolygonID)
			}
		}
		sort.Strings(neighbors)
		cells[i].Neighbors = neighbors
	}
}

func appendNavCellPolygon(tile *NavTileDef, chunk *ImportedWorldChunkDef, origin [3]float32, cell *NavBuildWalkableCell) {
	if tile == nil || chunk == nil || cell == nil {
		return
	}
	v := chunk.VoxelResolution
	x0 := origin[0] + float32(cell.X)*v
	x1 := x0 + v
	y := origin[1] + float32(cell.Y)*v
	z0 := origin[2] + float32(cell.Z)*v
	z1 := z0 + v
	base := len(tile.Vertices)
	tile.Vertices = append(tile.Vertices,
		Vec3{x0, y, z0},
		Vec3{x1, y, z0},
		Vec3{x1, y, z1},
		Vec3{x0, y, z1},
	)
	tile.Polygons = append(tile.Polygons, NavPolygonDef{
		ID:        cell.PolygonID,
		Vertices:  []int{base, base + 1, base + 2, base + 3},
		Area:      NavTraversalWalk,
		Neighbors: append([]string(nil), cell.Neighbors...),
	})
}

func importedWorldChunkWorldOrigin(chunk *ImportedWorldChunkDef) [3]float32 {
	if chunk == nil {
		return [3]float32{}
	}
	size := float32(chunk.ChunkSize) * chunk.VoxelResolution
	return [3]float32{
		float32(chunk.Coord.X) * size,
		float32(chunk.Coord.Y) * size,
		float32(chunk.Coord.Z) * size,
	}
}

func navCellPolygonID(x, y, z int) string {
	return "cell:" + itoa(x) + ":" + itoa(y) + ":" + itoa(z)
}

func absNavInt(v int) float32 {
	if v < 0 {
		return float32(-v)
	}
	return float32(v)
}
