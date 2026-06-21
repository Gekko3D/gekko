package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

type NavTileBuildOptions struct {
	NavID           string
	BuilderVersion  string
	SourceDeltaHash string
	NavBuildHash    string
	NeighborChunks  map[TerrainChunkCoordDef]*ImportedWorldChunkDef
	BuildCache      *NavTileBuildCache
	BuildSource     *NavBuildSourceDef
	ClearanceSource *NavClearanceSourceTileDef
	// BuildSourcePrimary lets authored/imported source surfaces replace voxel-derived regions
	// for tiles where the source emits geometry. Voxel regions remain the fallback.
	BuildSourcePrimary bool
}

type NavClearanceSourceTileBuildOptions struct {
	NavID              string
	BuilderVersion     string
	SourceDeltaHash    string
	NavBuildHash       string
	NeighborChunks     map[TerrainChunkCoordDef]*ImportedWorldChunkDef
	BuildCache         *NavTileBuildCache
	MaxClearanceRadius float32
}

type NavTileBuildCache struct {
	// TrustPayloadHash lets imported-world bakes use persisted chunk payload hashes as the
	// source identity. Leave this false for mutable in-memory chunks and editor deltas.
	TrustPayloadHash   bool
	mu                 sync.Mutex
	intermediates      map[navTileBuildIntermediateCacheKey]navTileBuildIntermediate
	chunkData          map[navTileBuildChunkDataCacheKey]navChunkNavData
	intermediateBuilds int
}

type NavBuildWalkableCell struct {
	X         int
	Y         int
	Z         int
	PolygonID string
	Neighbors []string
}

type NavClearanceSourceTileBuildResult struct {
	Tile           *NavClearanceSourceTileDef
	CandidateSpans int
	AcceptedSpans  int
}

type navBuildWalkableRegion struct {
	MinX      int
	MaxX      int
	MinZ      int
	MaxZ      int
	BaseY     int
	SlopeX    int
	SlopeZ    int
	HasPlane  bool
	PlaneBase float32
	PlaneX    float32
	PlaneZ    float32
	Area      string
	Contour   []navBuildContourPoint
	PolygonID string
	Neighbors []string
	Cells     []NavBuildWalkableCell
	Indices   []int
}

type navBuildContourPoint struct {
	X int
	Z int
}

type navBuildCellMetrics struct {
	Horizontal   float32
	Vertical     float32
	WorldAligned bool
	OriginX      float32
	OriginZ      float32
}

type navTileBuildIntermediate struct {
	Cells   []NavBuildWalkableCell
	Metrics navBuildCellMetrics
	Regions []navBuildWalkableRegion
	Stats   NavTileBuildStats
}

type navTileBuildIntermediateCacheKey struct {
	Coord           TerrainChunkCoordDef
	ChunkSize       int
	VoxelResolution float32
	ProfileHash     string
	SourceHash      string
	BuildSourceHash string
	NeighborHash    string
}

type navTileBuildChunkDataCacheKey struct {
	Coord           TerrainChunkCoordDef
	ChunkSize       int
	VoxelResolution float32
	SourceHash      string
}

type navChunkNavData struct {
	Occupancy      navDenseVoxelOccupancy
	CandidateSpans []navVoxelCandidateSpan
}

type navDenseVoxelOccupancy struct {
	ChunkSize int
	Bits      []uint64
	Count     int
}

type navVoxelCandidateSpan struct {
	X      int
	SolidY int
	Z      int
}

type navBuildBorderSide int

const (
	navBuildBorderMinX navBuildBorderSide = iota
	navBuildBorderMaxX
	navBuildBorderMinZ
	navBuildBorderMaxZ
)

type navBuildBorderStitcher struct {
	Profile   NavAgentProfileDef
	Metrics   navBuildCellMetrics
	Center    TerrainChunkCoordDef
	Neighbors map[TerrainChunkCoordDef]navBuildBorderNeighbor
}

type navBuildBorderNeighbor struct {
	Origin  [3]float32
	Regions []navBuildWalkableRegion
}

type navCoarseVoxelBucket struct {
	X     int
	Z     int
	Cells []NavBuildWalkableCell
}

type NavTileBuildResult struct {
	Tile          *NavTileDef
	WalkableCells []NavBuildWalkableCell
	Stats         NavTileBuildStats
}

type NavTileBuildStats struct {
	OccupiedVoxels int
	CandidateSpans int
	AcceptedSpans  int
	CompactCells   int
	Regions        int
	Polygons       int
	Portals        int
}

type navBuildSourceRectPolygon struct {
	MinX   float32
	MaxX   float32
	MinZ   float32
	MaxZ   float32
	Y      float32
	Area   string
	Flags  []string
	ID     string
	Merged bool
}

type navBuildSourceBlockerRect struct {
	MinX float32
	MaxX float32
	MinY float32
	MaxY float32
	MinZ float32
	MaxZ float32
}

type navBuildSourceRasterPlane struct {
	X float32
	Z float32
	C float32
}

type navBuildSourceRasterCell struct {
	X             int
	Z             int
	MinX          float32
	MaxX          float32
	MinZ          float32
	MaxZ          float32
	Vertices      []Vec3
	Full          bool
	MergeKey      string
	Plane         navBuildSourceRasterPlane
	Surface       NavBuildSurfaceDef
	SurfaceIndex  int
	TriangleIndex int
}

type navBuildSourceRasterRegion struct {
	MinX          int
	MaxX          int
	MinZ          int
	MaxZ          int
	BoundsMinX    float32
	BoundsMaxX    float32
	BoundsMinZ    float32
	BoundsMaxZ    float32
	Plane         navBuildSourceRasterPlane
	MergeKey      string
	Surface       NavBuildSurfaceDef
	SurfaceIndex  int
	TriangleIndex int
	Cells         []navBuildSourceRasterCell
	Indices       []int
}

func BuildNavClearanceSourceTileFromImportedWorldChunk(chunk *ImportedWorldChunkDef, opts NavClearanceSourceTileBuildOptions) (NavClearanceSourceTileBuildResult, error) {
	if chunk == nil {
		return NavClearanceSourceTileBuildResult{}, fmt.Errorf("imported world chunk is nil")
	}
	if chunk.ChunkSize <= 0 {
		return NavClearanceSourceTileBuildResult{}, fmt.Errorf("imported world chunk_size must be positive")
	}
	if chunk.VoxelResolution <= 0 {
		return NavClearanceSourceTileBuildResult{}, fmt.Errorf("imported world voxel_resolution must be positive")
	}
	origin := importedWorldChunkWorldOrigin(chunk)
	worldSize := float32(chunk.ChunkSize) * chunk.VoxelResolution
	maxClearance := opts.MaxClearanceRadius
	if maxClearance <= 0 {
		defaultProfile := DefaultHL1NavAgentProfile()
		maxClearance = maxNavFloat32(chunk.VoxelResolution, defaultProfile.Radius*2)
	}
	data := navTileBuildChunkData(opts.BuildCache, chunk)
	samplerProfile := navClearanceSourceSamplerProfile(chunk, maxClearance)
	sampler := newImportedWorldNavOccupancySampler(opts.BuildCache, chunk, data, opts.NeighborChunks, samplerProfile)
	cells := make([]NavClearanceSourceCellDef, 0, len(data.CandidateSpans))
	acceptedSpans := 0
	for _, span := range data.CandidateSpans {
		floorY := span.SolidY + 1
		if floorY < 0 || floorY > chunk.ChunkSize {
			continue
		}
		if !navVoxelHasExposedFloorSpan(sampler, span.X, floorY, span.Z) {
			continue
		}
		acceptedSpans++
		headroomVoxels := navClearanceSourceHeadroomVoxels(sampler, span.X, floorY, span.Z)
		if headroomVoxels <= 0 {
			continue
		}
		clearance := navClearanceSourceRadius(chunk, sampler, span.X, floorY, span.Z, headroomVoxels, maxClearance)
		cells = append(cells, NavClearanceSourceCellDef{
			X:               span.X,
			Y:               floorY,
			Z:               span.Z,
			Position:        Vec3{origin[0] + (float32(span.X)+0.5)*chunk.VoxelResolution, origin[1] + float32(floorY)*chunk.VoxelResolution, origin[2] + (float32(span.Z)+0.5)*chunk.VoxelResolution},
			Headroom:        float32(headroomVoxels) * chunk.VoxelResolution,
			ClearanceRadius: clearance,
			Area:            NavTraversalWalk,
		})
	}
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].X != cells[j].X {
			return cells[i].X < cells[j].X
		}
		if cells[i].Z != cells[j].Z {
			return cells[i].Z < cells[j].Z
		}
		return cells[i].Y < cells[j].Y
	})
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
	tile := &NavClearanceSourceTileDef{
		NavID:              navID,
		SchemaVersion:      CurrentNavClearanceSourceTileSchemaVersion,
		Coord:              chunk.Coord,
		Kind:               NavClearanceSourceKindVoxelSpans,
		BuilderVersion:     builderVersion,
		PayloadKind:        NavClearanceSourceTilePayloadJSONV1,
		SourcePayloadHash:  chunk.PayloadHash,
		SourceDeltaHash:    opts.SourceDeltaHash,
		NavBuildHash:       opts.NavBuildHash,
		BoundsMin:          [3]float32{origin[0], origin[1], origin[2]},
		BoundsMax:          [3]float32{origin[0] + worldSize, origin[1] + worldSize, origin[2] + worldSize},
		VoxelResolution:    chunk.VoxelResolution,
		MaxClearanceRadius: maxClearance,
		Cells:              cells,
	}
	EnsureNavClearanceSourceTileDefaults(tile)
	return NavClearanceSourceTileBuildResult{Tile: tile, CandidateSpans: len(data.CandidateSpans), AcceptedSpans: acceptedSpans}, nil
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
	builderVersion := opts.BuilderVersion
	if builderVersion == "" {
		builderVersion = DefaultNavBuilderVersion
	}
	if builderVersion == NavBuilderVersionVoxelRecastV1 {
		return buildRecastNavTileFromImportedWorldChunk(chunk, profile, opts)
	}

	origin := importedWorldChunkWorldOrigin(chunk)
	worldSize := float32(chunk.ChunkSize) * chunk.VoxelResolution
	sourceHasBlockers := navBuildSourceHasClearanceBlockers(opts.BuildSource)
	intermediate, usedClearanceSource := navTileBuildIntermediateFromClearanceSource(opts.BuildCache, chunk, opts.ClearanceSource, profile)
	if sourceHasBlockers {
		usedClearanceSource = false
	}
	if !usedClearanceSource {
		intermediate = navTileBuildIntermediateForChunkWithSource(opts.BuildCache, chunk, profile, opts.NeighborChunks, opts.BuildSource)
	}
	cells := intermediate.Cells
	metrics := intermediate.Metrics
	regions := intermediate.Regions
	stitcher := newNavBuildBorderStitcher(chunk, profile, opts.NeighborChunks, metrics, opts.BuildCache, opts.BuildSource)

	navID := opts.NavID
	if navID == "" {
		navID = chunk.WorldID
	}
	if navID == "" {
		navID = newID()
	}
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
	sourcePrimary := false
	if opts.BuildSourcePrimary && opts.BuildSource != nil {
		startPolygon := len(tile.Polygons)
		appendNavBuildSourcePrimaryPolygons(tile, opts.BuildSource, profile, chunk, opts.NeighborChunks, opts.BuildCache)
		if len(tile.Polygons) > startPolygon {
			applyNavSurfaceNeighbors(tile, startPolygon, profile)
		}
		sourcePrimary = len(tile.Polygons) > startPolygon
	}
	if !sourcePrimary {
		for i := range regions {
			appendNavRegionPolygon(tile, origin, metrics, &regions[i], stitcher)
		}
	}
	if !sourcePrimary && len(regions) > 0 {
		applyNavSurfaceNeighbors(tile, 0, profile)
		assignNavBuildCellPolygonIDsFromTile(tile, origin, metrics, cells, chunk.VoxelResolution, profile)
		connectNavTilePolygonsFromCells(tile, origin, metrics, cells, chunk.VoxelResolution, profile)
		appendNavTileBorderSpans(tile, origin, metrics, cells, chunk.VoxelResolution)
		appendNavTileDropLinks(tile, origin, metrics, cells, chunk.VoxelResolution, profile, opts.NeighborChunks, opts.BuildCache, opts.BuildSource)
	} else if !sourcePrimary {
		appendNavBuildSourcePolygons(tile, opts.BuildSource, profile)
	}
	EnsureNavTileDefaults(tile)
	stats := intermediate.Stats
	stats.Polygons = len(tile.Polygons)
	stats.Portals = len(tile.Portals)
	return NavTileBuildResult{Tile: tile, WalkableCells: append([]NavBuildWalkableCell(nil), cells...), Stats: stats}, nil
}

func importedWorldChunkOccupancy(chunk *ImportedWorldChunkDef) map[[3]int]struct{} {
	out := make(map[[3]int]struct{})
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

func navTileBuildChunkData(cache *NavTileBuildCache, chunk *ImportedWorldChunkDef) navChunkNavData {
	if cache == nil {
		return importedWorldChunkNavData(chunk)
	}
	key := navTileBuildChunkDataCacheKeyForChunk(cache, chunk)
	cache.mu.Lock()
	if cache.chunkData == nil {
		cache.chunkData = make(map[navTileBuildChunkDataCacheKey]navChunkNavData)
	}
	if data, ok := cache.chunkData[key]; ok {
		cache.mu.Unlock()
		return data
	}
	cache.mu.Unlock()

	data := importedWorldChunkNavData(chunk)
	cache.mu.Lock()
	if cached, ok := cache.chunkData[key]; ok {
		cache.mu.Unlock()
		return cached
	}
	cache.chunkData[key] = data
	cache.mu.Unlock()
	return data
}

func navTileBuildChunkDataCacheKeyForChunk(cache *NavTileBuildCache, chunk *ImportedWorldChunkDef) navTileBuildChunkDataCacheKey {
	key := navTileBuildChunkDataCacheKey{}
	if chunk != nil {
		key.Coord = chunk.Coord
		key.ChunkSize = chunk.ChunkSize
		key.VoxelResolution = chunk.VoxelResolution
		key.SourceHash = navTileBuildChunkCacheSourceHash(cache, chunk)
	}
	return key
}

func importedWorldChunkNavData(chunk *ImportedWorldChunkDef) navChunkNavData {
	if chunk == nil {
		return navChunkNavData{}
	}
	occupancy := newNavDenseVoxelOccupancy(chunk.ChunkSize)
	columns := make(map[int][]int)
	for _, voxel := range chunk.Voxels {
		if voxel.Value == 0 {
			continue
		}
		if voxel.X < 0 || voxel.Y < 0 || voxel.Z < 0 || voxel.X >= chunk.ChunkSize || voxel.Y >= chunk.ChunkSize || voxel.Z >= chunk.ChunkSize {
			continue
		}
		if !occupancy.Set(voxel.X, voxel.Y, voxel.Z) {
			continue
		}
		columnKey := voxel.X*chunk.ChunkSize + voxel.Z
		columns[columnKey] = append(columns[columnKey], voxel.Y)
	}
	return navChunkNavData{
		Occupancy:      occupancy,
		CandidateSpans: navVoxelCandidateSpansFromColumns(chunk, columns),
	}
}

func newNavDenseVoxelOccupancy(chunkSize int) navDenseVoxelOccupancy {
	if chunkSize <= 0 {
		return navDenseVoxelOccupancy{}
	}
	voxelCount := chunkSize * chunkSize * chunkSize
	return navDenseVoxelOccupancy{
		ChunkSize: chunkSize,
		Bits:      make([]uint64, (voxelCount+63)/64),
	}
}

func (o navDenseVoxelOccupancy) Valid() bool {
	return o.ChunkSize > 0 && len(o.Bits) > 0
}

func (o *navDenseVoxelOccupancy) Set(x int, y int, z int) bool {
	if o == nil || !o.Valid() || x < 0 || y < 0 || z < 0 || x >= o.ChunkSize || y >= o.ChunkSize || z >= o.ChunkSize {
		return false
	}
	index := navDenseVoxelOccupancyIndex(o.ChunkSize, x, y, z)
	wordIndex := index / 64
	mask := uint64(1) << uint(index%64)
	if o.Bits[wordIndex]&mask != 0 {
		return false
	}
	o.Bits[wordIndex] |= mask
	o.Count++
	return true
}

func (o navDenseVoxelOccupancy) Occupied(x int, y int, z int) bool {
	if !o.Valid() || x < 0 || y < 0 || z < 0 || x >= o.ChunkSize || y >= o.ChunkSize || z >= o.ChunkSize {
		return false
	}
	index := navDenseVoxelOccupancyIndex(o.ChunkSize, x, y, z)
	return o.Bits[index/64]&(uint64(1)<<uint(index%64)) != 0
}

func navDenseVoxelOccupancyIndex(chunkSize int, x int, y int, z int) int {
	return ((z*chunkSize)+x)*chunkSize + y
}

func navVoxelCandidateSpansFromColumns(chunk *ImportedWorldChunkDef, columns map[int][]int) []navVoxelCandidateSpan {
	if chunk == nil || chunk.ChunkSize <= 0 || len(columns) == 0 {
		return nil
	}
	spans := make([]navVoxelCandidateSpan, 0, len(columns))
	for columnKey, ys := range columns {
		if len(ys) == 0 {
			continue
		}
		sort.Ints(ys)
		x := columnKey / chunk.ChunkSize
		z := columnKey % chunk.ChunkSize
		for i := 0; i < len(ys); i++ {
			y := ys[i]
			for i+1 < len(ys) && ys[i+1] == y {
				i++
			}
			if i+1 < len(ys) && ys[i+1] == y+1 {
				continue
			}
			spans = append(spans, navVoxelCandidateSpan{X: x, SolidY: y, Z: z})
		}
	}
	return spans
}

func buildNavWalkableCells(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, candidateSpans []navVoxelCandidateSpan, sampler importedWorldNavOccupancySampler) ([]NavBuildWalkableCell, int) {
	cells := make([]NavBuildWalkableCell, 0)
	acceptedCandidates := 0
	for _, span := range candidateSpans {
		x := span.X
		solidY := span.SolidY
		z := span.Z
		floorY := solidY + 1
		if floorY < 0 || floorY > chunk.ChunkSize {
			continue
		}
		if !navVoxelHasExposedFloorSpan(sampler, x, floorY, z) {
			continue
		}
		acceptedCandidates++
		if navCellHasAgentClearance(chunk, profile, sampler, x, floorY, z) {
			id := navCellPolygonID(x, floorY, z)
			cells = append(cells, NavBuildWalkableCell{X: x, Y: floorY, Z: z, PolygonID: id})
		}
	}
	return cells, acceptedCandidates
}

func navVoxelHasExposedFloorSpan(sampler importedWorldNavOccupancySampler, x, floorY, z int) bool {
	blocked, known := sampler.Occupied(x, floorY, z)
	return known && !blocked
}

func navClearanceSourceSamplerProfile(chunk *ImportedWorldChunkDef, maxClearance float32) NavAgentProfileDef {
	profile := DefaultHL1NavAgentProfile()
	if chunk != nil && chunk.VoxelResolution > 0 {
		profile.Height = chunk.VoxelResolution
		profile.StepHeight = chunk.VoxelResolution
	}
	if maxClearance > 0 {
		profile.Radius = maxClearance
	}
	return profile
}

func navClearanceSourceHeadroomVoxels(sampler importedWorldNavOccupancySampler, x, floorY, z int) int {
	headroom := 0
	for y := floorY; ; y++ {
		blocked, known := sampler.Occupied(x, y, z)
		if !known || blocked {
			break
		}
		headroom++
	}
	return headroom
}

func navClearanceSourceRadius(chunk *ImportedWorldChunkDef, sampler importedWorldNavOccupancySampler, x, floorY, z int, headroomVoxels int, maxClearance float32) float32 {
	if chunk == nil || chunk.VoxelResolution <= 0 || headroomVoxels <= 0 || maxClearance <= 0 {
		return 0
	}
	maxRadiusVoxels := float64(maxClearance / chunk.VoxelResolution)
	radiusCells := int(math.Ceil(maxRadiusVoxels))
	centerX := float64(x) + 0.5
	centerZ := float64(z) + 0.5
	nearest := maxRadiusVoxels
	for oz := z - radiusCells; oz <= z+radiusCells; oz++ {
		for ox := x - radiusCells; ox <= x+radiusCells; ox++ {
			distance := navClearanceSourceVoxelDistance2D(centerX, centerZ, ox, oz)
			if distance > nearest {
				continue
			}
			for oy := floorY; oy < floorY+headroomVoxels; oy++ {
				blocked, known := sampler.Occupied(ox, oy, oz)
				if !known || blocked {
					nearest = distance
					break
				}
			}
		}
	}
	return float32(nearest) * chunk.VoxelResolution
}

func navClearanceSourceVoxelDistance2D(pointX, pointZ float64, voxelX, voxelZ int) float64 {
	minX := float64(voxelX)
	maxX := float64(voxelX + 1)
	minZ := float64(voxelZ)
	maxZ := float64(voxelZ + 1)
	dx := 0.0
	if pointX < minX {
		dx = minX - pointX
	} else if pointX > maxX {
		dx = pointX - maxX
	}
	dz := 0.0
	if pointZ < minZ {
		dz = minZ - pointZ
	} else if pointZ > maxZ {
		dz = pointZ - maxZ
	}
	return math.Sqrt(dx*dx + dz*dz)
}

func navTileBuildIntermediateForChunk(cache *NavTileBuildCache, chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef) navTileBuildIntermediate {
	return navTileBuildIntermediateForChunkWithSource(cache, chunk, profile, chunks, nil)
}

func navTileBuildIntermediateForChunkWithSource(cache *NavTileBuildCache, chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, source *NavBuildSourceDef) navTileBuildIntermediate {
	if cache == nil {
		return buildNavTileIntermediate(nil, chunk, profile, chunks, source)
	}
	key := navTileBuildIntermediateCacheKeyForChunk(cache, chunk, profile, chunks, source)
	cache.mu.Lock()
	if cache.intermediates == nil {
		cache.intermediates = make(map[navTileBuildIntermediateCacheKey]navTileBuildIntermediate)
	}
	if cached, ok := cache.intermediates[key]; ok {
		cache.mu.Unlock()
		return cached
	}
	cache.mu.Unlock()

	intermediate := buildNavTileIntermediate(cache, chunk, profile, chunks, source)
	cache.mu.Lock()
	if cached, ok := cache.intermediates[key]; ok {
		cache.mu.Unlock()
		return cached
	}
	cache.intermediates[key] = intermediate
	cache.intermediateBuilds++
	cache.mu.Unlock()
	return intermediate
}

func navTileBuildIntermediateCacheKeyForChunk(cache *NavTileBuildCache, chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, source *NavBuildSourceDef) navTileBuildIntermediateCacheKey {
	EnsureNavAgentProfileDefaults(&profile)
	key := navTileBuildIntermediateCacheKey{
		ProfileHash: navBuildHash("", profile, ""),
	}
	if chunk != nil {
		key.Coord = chunk.Coord
		key.ChunkSize = chunk.ChunkSize
		key.VoxelResolution = chunk.VoxelResolution
		key.SourceHash = navTileBuildChunkCacheSourceHash(cache, chunk)
		key.BuildSourceHash = navBuildSourceClearanceBlockerHash(source)
		key.NeighborHash = navTileBuildNeighborCacheHash(cache, chunk, profile, chunks)
	}
	return key
}

func navTileBuildChunkCacheSourceHash(cache *NavTileBuildCache, chunk *ImportedWorldChunkDef) string {
	if chunk == nil {
		return ""
	}
	if cache != nil && cache.TrustPayloadHash && strings.TrimSpace(chunk.PayloadHash) != "" {
		return chunk.PayloadHash
	}
	return navCombinedSourceHash(chunk.PayloadHash, importedWorldChunkNavSourceHash(chunk))
}

func navTileBuildNeighborCacheHash(cache *NavTileBuildCache, chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef) string {
	if chunk == nil || len(chunks) == 0 {
		return ""
	}
	reach := navBuildChunkReachForProfile(chunk.ChunkSize, chunk.VoxelResolution, profile)
	coords := make([]TerrainChunkCoordDef, 0, len(chunks))
	for coord, neighbor := range chunks {
		if coord == chunk.Coord || !navBuildChunksCompatible(chunk, neighbor) || !navBuildCoordWithinReach(chunk.Coord, coord, reach) {
			continue
		}
		coords = append(coords, coord)
	}
	if len(coords) == 0 {
		return ""
	}
	sort.Slice(coords, func(i, j int) bool {
		return terrainChunkCoordLess(coords[i], coords[j])
	})
	values := make([]string, 0, len(coords))
	for _, coord := range coords {
		values = append(values, fmt.Sprintf("%d:%d:%d:%s", coord.X, coord.Y, coord.Z, navTileBuildChunkCacheSourceHash(cache, chunks[coord])))
	}
	return navCombinedSourceHash(values...)
}

func buildNavTileIntermediate(cache *NavTileBuildCache, chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, source *NavBuildSourceDef) navTileBuildIntermediate {
	data := navTileBuildChunkData(cache, chunk)
	sampler := newImportedWorldNavOccupancySampler(cache, chunk, data, chunks, profile)
	addNavBuildSourceClearanceBlockersToSampler(&sampler, source, chunk, profile)
	cells, metrics, candidateSpans, acceptedSpans := buildNavWalkableCellsForProfile(chunk, profile, data.CandidateSpans, sampler)
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].X != cells[j].X {
			return cells[i].X < cells[j].X
		}
		if cells[i].Z != cells[j].Z {
			return cells[i].Z < cells[j].Z
		}
		return cells[i].Y < cells[j].Y
	})
	regions := mergeNavWalkableCells(cells, metrics, profile)
	applyNavRegionNeighbors(regions, metrics, profile)
	assignNavCellRegionIDs(cells, regions)
	return navTileBuildIntermediate{
		Cells:   cells,
		Metrics: metrics,
		Regions: regions,
		Stats: NavTileBuildStats{
			OccupiedVoxels: data.Occupancy.Count,
			CandidateSpans: candidateSpans,
			AcceptedSpans:  acceptedSpans,
			CompactCells:   len(cells),
			Regions:        len(regions),
		},
	}
}

func navTileBuildIntermediateFromClearanceSource(cache *NavTileBuildCache, chunk *ImportedWorldChunkDef, source *NavClearanceSourceTileDef, profile NavAgentProfileDef) (navTileBuildIntermediate, bool) {
	if chunk == nil || source == nil || source.Coord != chunk.Coord || source.VoxelResolution <= 0 || chunk.VoxelResolution <= 0 {
		return navTileBuildIntermediate{}, false
	}
	if !navAlmostEqual(source.VoxelResolution, chunk.VoxelResolution, 1e-5) {
		return navTileBuildIntermediate{}, false
	}
	data := navTileBuildChunkData(cache, chunk)
	cells, metrics, candidateSpans, acceptedSpans := buildNavWalkableCellsForProfileFromClearanceSource(chunk, profile, source)
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].X != cells[j].X {
			return cells[i].X < cells[j].X
		}
		if cells[i].Z != cells[j].Z {
			return cells[i].Z < cells[j].Z
		}
		return cells[i].Y < cells[j].Y
	})
	regions := mergeNavWalkableCells(cells, metrics, profile)
	applyNavRegionNeighbors(regions, metrics, profile)
	assignNavCellRegionIDs(cells, regions)
	return navTileBuildIntermediate{
		Cells:   cells,
		Metrics: metrics,
		Regions: regions,
		Stats: NavTileBuildStats{
			OccupiedVoxels: data.Occupancy.Count,
			CandidateSpans: candidateSpans,
			AcceptedSpans:  acceptedSpans,
			CompactCells:   len(cells),
			Regions:        len(regions),
		},
	}, true
}

func buildNavWalkableCellsForProfile(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, candidateSpans []navVoxelCandidateSpan, sampler importedWorldNavOccupancySampler) ([]NavBuildWalkableCell, navBuildCellMetrics, int, int) {
	metrics := navBuildCellMetrics{
		Horizontal: chunk.VoxelResolution,
		Vertical:   chunk.VoxelResolution,
	}
	rawCells, acceptedCandidates := buildNavWalkableCells(chunk, profile, candidateSpans, sampler)
	acceptedSpans := len(rawCells)
	cellSize := navVoxelRasterCellSize(profile, chunk.VoxelResolution)
	if cellSize <= 0 {
		smoothNavCompactHeightfieldCells(rawCells, metrics, profile)
		return rawCells, metrics, acceptedCandidates, acceptedSpans
	}
	metrics.Horizontal = cellSize
	metrics.WorldAligned = true
	cells := buildCoarseNavWalkableCellsFromVoxelCells(chunk, profile, rawCells, cellSize)
	smoothNavCompactHeightfieldCells(cells, metrics, profile)
	return cells, metrics, acceptedCandidates, acceptedSpans
}

func buildNavWalkableCellsForProfileFromClearanceSource(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, source *NavClearanceSourceTileDef) ([]NavBuildWalkableCell, navBuildCellMetrics, int, int) {
	metrics := navBuildCellMetrics{
		Horizontal: chunk.VoxelResolution,
		Vertical:   chunk.VoxelResolution,
	}
	rawCells := make([]NavBuildWalkableCell, 0, len(source.Cells))
	for _, cell := range source.Cells {
		if cell.X < 0 || cell.Y < 0 || cell.Z < 0 || cell.X >= chunk.ChunkSize || cell.Y > chunk.ChunkSize || cell.Z >= chunk.ChunkSize {
			continue
		}
		if !NavClearanceSourceCellSupportsAgent(cell, profile) {
			continue
		}
		rawCells = append(rawCells, NavBuildWalkableCell{X: cell.X, Y: cell.Y, Z: cell.Z, PolygonID: navCellPolygonID(cell.X, cell.Y, cell.Z)})
	}
	acceptedSpans := len(rawCells)
	cellSize := navVoxelRasterCellSize(profile, chunk.VoxelResolution)
	if cellSize <= 0 {
		smoothNavCompactHeightfieldCells(rawCells, metrics, profile)
		return rawCells, metrics, len(source.Cells), acceptedSpans
	}
	metrics.Horizontal = cellSize
	metrics.WorldAligned = true
	cells := buildCoarseNavWalkableCellsFromVoxelCells(chunk, profile, rawCells, cellSize)
	smoothNavCompactHeightfieldCells(cells, metrics, profile)
	return cells, metrics, len(source.Cells), acceptedSpans
}

func newNavBuildBorderStitcher(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, metrics navBuildCellMetrics, cache *NavTileBuildCache, source *NavBuildSourceDef) *navBuildBorderStitcher {
	if chunk == nil || len(chunks) == 0 || metrics.Horizontal <= 0 || metrics.Vertical <= 0 {
		return nil
	}
	stitcher := &navBuildBorderStitcher{
		Profile:   profile,
		Metrics:   metrics,
		Center:    chunk.Coord,
		Neighbors: make(map[TerrainChunkCoordDef]navBuildBorderNeighbor),
	}
	for _, offset := range navBuildBorderNeighborOffsets() {
		coord := TerrainChunkCoordDef{X: chunk.Coord.X + offset.X, Y: chunk.Coord.Y, Z: chunk.Coord.Z + offset.Z}
		neighborChunk := chunks[coord]
		if !navBuildChunksCompatible(chunk, neighborChunk) {
			continue
		}
		regions := navTileBuildIntermediateForChunkWithSource(cache, neighborChunk, profile, chunks, source).Regions
		if len(regions) == 0 {
			continue
		}
		stitcher.Neighbors[coord] = navBuildBorderNeighbor{
			Origin:  importedWorldChunkWorldOrigin(neighborChunk),
			Regions: regions,
		}
	}
	if len(stitcher.Neighbors) == 0 {
		return nil
	}
	return stitcher
}

func navBuildBorderNeighborOffsets() []TerrainChunkCoordDef {
	return []TerrainChunkCoordDef{
		{X: -1}, {X: 1},
		{Z: -1}, {Z: 1},
		{X: -1, Z: -1}, {X: -1, Z: 1},
		{X: 1, Z: -1}, {X: 1, Z: 1},
	}
}

func navBuildChunksCompatible(a *ImportedWorldChunkDef, b *ImportedWorldChunkDef) bool {
	return a != nil && b != nil && a.ChunkSize == b.ChunkSize && a.VoxelResolution == b.VoxelResolution
}

func (s *navBuildBorderStitcher) StitchedHeight(tile *NavTileDef, worldX, worldZ, currentHeight float32) (float32, bool) {
	if s == nil || tile == nil || len(s.Neighbors) == 0 {
		return 0, false
	}
	sides := navBuildBorderSidesForPoint(tile, worldX, worldZ, s.Metrics)
	if len(sides) == 0 {
		return 0, false
	}
	sum := currentHeight
	count := 1
	for _, offset := range navBuildBorderOffsetsForSides(sides) {
		coord := TerrainChunkCoordDef{X: s.Center.X + offset.X, Y: s.Center.Y, Z: s.Center.Z + offset.Z}
		neighbor, ok := s.Neighbors[coord]
		if !ok {
			continue
		}
		height, ok := neighbor.HeightAtWorld(worldX, worldZ, currentHeight, s.Profile, s.Metrics)
		if !ok {
			continue
		}
		sum += height
		count++
	}
	if count <= 1 {
		return 0, false
	}
	return sum / float32(count), true
}

func navBuildBorderOffsetsForSides(sides []navBuildBorderSide) []TerrainChunkCoordDef {
	hasMinX := false
	hasMaxX := false
	hasMinZ := false
	hasMaxZ := false
	for _, side := range sides {
		switch side {
		case navBuildBorderMinX:
			hasMinX = true
		case navBuildBorderMaxX:
			hasMaxX = true
		case navBuildBorderMinZ:
			hasMinZ = true
		case navBuildBorderMaxZ:
			hasMaxZ = true
		}
	}
	offsets := make([]TerrainChunkCoordDef, 0, 3)
	if hasMinX {
		offsets = append(offsets, TerrainChunkCoordDef{X: -1})
	}
	if hasMaxX {
		offsets = append(offsets, TerrainChunkCoordDef{X: 1})
	}
	if hasMinZ {
		offsets = append(offsets, TerrainChunkCoordDef{Z: -1})
	}
	if hasMaxZ {
		offsets = append(offsets, TerrainChunkCoordDef{Z: 1})
	}
	if hasMinX && hasMinZ {
		offsets = append(offsets, TerrainChunkCoordDef{X: -1, Z: -1})
	}
	if hasMinX && hasMaxZ {
		offsets = append(offsets, TerrainChunkCoordDef{X: -1, Z: 1})
	}
	if hasMaxX && hasMinZ {
		offsets = append(offsets, TerrainChunkCoordDef{X: 1, Z: -1})
	}
	if hasMaxX && hasMaxZ {
		offsets = append(offsets, TerrainChunkCoordDef{X: 1, Z: 1})
	}
	return offsets
}

func navBuildBorderSidesForPoint(tile *NavTileDef, worldX, worldZ float32, metrics navBuildCellMetrics) []navBuildBorderSide {
	if tile == nil {
		return nil
	}
	epsilon := navBuildBorderStitchEpsilon(metrics)
	sides := make([]navBuildBorderSide, 0, 2)
	if absNavFloat32(worldX-tile.BoundsMin[0]) <= epsilon {
		sides = append(sides, navBuildBorderMinX)
	}
	if absNavFloat32(worldX-tile.BoundsMax[0]) <= epsilon {
		sides = append(sides, navBuildBorderMaxX)
	}
	if absNavFloat32(worldZ-tile.BoundsMin[2]) <= epsilon {
		sides = append(sides, navBuildBorderMinZ)
	}
	if absNavFloat32(worldZ-tile.BoundsMax[2]) <= epsilon {
		sides = append(sides, navBuildBorderMaxZ)
	}
	return sides
}

func navBuildBorderStitchEpsilon(metrics navBuildCellMetrics) float32 {
	epsilon := float32(1e-4)
	if metrics.Horizontal > 0 {
		epsilon = maxNavFloat32(epsilon, metrics.Horizontal*1e-4)
	}
	return epsilon
}

func (n navBuildBorderNeighbor) HeightAtWorld(worldX, worldZ, currentHeight float32, profile NavAgentProfileDef, metrics navBuildCellMetrics) (float32, bool) {
	if len(n.Regions) == 0 || metrics.Horizontal <= 0 || metrics.Vertical <= 0 {
		return 0, false
	}
	gridX := navWorldToRegionGridX(n.Origin, metrics, worldX)
	gridZ := navWorldToRegionGridZ(n.Origin, metrics, worldZ)
	bestHeight := float32(0)
	bestDelta := float32(0)
	found := false
	for _, region := range n.Regions {
		if !navBuildRegionContainsGridPoint(region, gridX, gridZ) {
			continue
		}
		height := n.Origin[1] + navRegionHeightAtGrid(region, gridX, gridZ)*metrics.Vertical
		if !navPortalHeightsConnect(currentHeight, height, profile) {
			continue
		}
		delta := absNavFloat32(currentHeight - height)
		if found && delta >= bestDelta {
			continue
		}
		bestHeight = height
		bestDelta = delta
		found = true
	}
	return bestHeight, found
}

func navBuildRegionContainsGridPoint(region navBuildWalkableRegion, x, z float32) bool {
	const epsilon = float32(1e-4)
	if x < float32(region.MinX)-epsilon || x > float32(region.MaxX+1)+epsilon ||
		z < float32(region.MinZ)-epsilon || z > float32(region.MaxZ+1)+epsilon {
		return false
	}
	if len(region.Contour) >= 3 {
		return navBuildContourContainsGridPoint(region.Contour, x, z)
	}
	return true
}

func navBuildContourContainsGridPoint(contour []navBuildContourPoint, x, z float32) bool {
	if len(contour) < 3 {
		return false
	}
	point := Vec3{x, 0, z}
	inside := false
	j := len(contour) - 1
	const epsilon = float32(1e-5)
	for i := range contour {
		a := Vec3{float32(contour[i].X), 0, float32(contour[i].Z)}
		b := Vec3{float32(contour[j].X), 0, float32(contour[j].Z)}
		if navPointOnSegmentXZ(point, a, b, epsilon) {
			return true
		}
		intersects := (a[2] > point[2]) != (b[2] > point[2])
		if intersects {
			edgeX := (b[0]-a[0])*(point[2]-a[2])/(b[2]-a[2]) + a[0]
			if point[0] < edgeX {
				inside = !inside
			}
		}
		j = i
	}
	return inside
}

func navVoxelRasterCellSize(profile NavAgentProfileDef, voxelResolution float32) float32 {
	const epsilon = float32(1e-4)
	if profile.NavCellSize <= 0 || voxelResolution <= 0 {
		return 0
	}
	if profile.NavCellSize <= voxelResolution+epsilon {
		return 0
	}
	return profile.NavCellSize
}

func buildCoarseNavWalkableCellsFromVoxelCells(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, rawCells []NavBuildWalkableCell, cellSize float32) []NavBuildWalkableCell {
	if chunk == nil || len(rawCells) == 0 || cellSize <= 0 {
		return nil
	}
	origin := importedWorldChunkWorldOrigin(chunk)
	buckets := make(map[[2]int]*navCoarseVoxelBucket)
	for _, cell := range rawCells {
		centerX := origin[0] + (float32(cell.X)+0.5)*chunk.VoxelResolution
		centerZ := origin[2] + (float32(cell.Z)+0.5)*chunk.VoxelResolution
		x := int(math.Floor(float64(centerX / cellSize)))
		z := int(math.Floor(float64(centerZ / cellSize)))
		key := [2]int{x, z}
		bucket := buckets[key]
		if bucket == nil {
			bucket = &navCoarseVoxelBucket{X: x, Z: z}
			buckets[key] = bucket
		}
		bucket.Cells = append(bucket.Cells, cell)
	}
	keys := make([][2]int, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][1] != keys[j][1] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][0] < keys[j][0]
	})
	out := make([]NavBuildWalkableCell, 0, len(buckets))
	for _, key := range keys {
		bucket := buckets[key]
		out = append(out, buildCoarseNavWalkableCellsFromBucket(chunk, profile, origin, *bucket, cellSize)...)
	}
	normalizeCoarseNavWalkableCellHeights(out, chunk.VoxelResolution)
	return out
}

func buildCoarseNavWalkableCellsFromBucket(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, origin [3]float32, bucket navCoarseVoxelBucket, cellSize float32) []NavBuildWalkableCell {
	if len(bucket.Cells) == 0 {
		return nil
	}
	sort.Slice(bucket.Cells, func(i, j int) bool {
		if bucket.Cells[i].Y != bucket.Cells[j].Y {
			return bucket.Cells[i].Y < bucket.Cells[j].Y
		}
		if bucket.Cells[i].Z != bucket.Cells[j].Z {
			return bucket.Cells[i].Z < bucket.Cells[j].Z
		}
		return bucket.Cells[i].X < bucket.Cells[j].X
	})
	worldSize := float32(chunk.ChunkSize) * chunk.VoxelResolution
	tileMinX := origin[0]
	tileMaxX := origin[0] + worldSize
	tileMinZ := origin[2]
	tileMaxZ := origin[2] + worldSize
	cellMinX := maxNavFloat32(float32(bucket.X)*cellSize, tileMinX)
	cellMaxX := minNavFloat32(float32(bucket.X+1)*cellSize, tileMaxX)
	cellMinZ := maxNavFloat32(float32(bucket.Z)*cellSize, tileMinZ)
	cellMaxZ := minNavFloat32(float32(bucket.Z+1)*cellSize, tileMaxZ)
	cellArea := (cellMaxX - cellMinX) * (cellMaxZ - cellMinZ)
	if cellArea <= 0 {
		return nil
	}
	minCoverageArea := cellArea * navCoarseVoxelMinCoverage(profile, chunk.VoxelResolution, cellSize)
	clusterStepVoxels := int(math.Ceil(float64(profile.StepHeight / chunk.VoxelResolution)))
	if clusterStepVoxels < 1 {
		clusterStepVoxels = 1
	}
	out := make([]NavBuildWalkableCell, 0, 1)
	clusterStart := 0
	for clusterStart < len(bucket.Cells) {
		clusterEnd := clusterStart + 1
		clusterBaseY := bucket.Cells[clusterStart].Y
		for clusterEnd < len(bucket.Cells) && bucket.Cells[clusterEnd].Y-clusterBaseY <= clusterStepVoxels {
			clusterEnd++
		}
		cluster := bucket.Cells[clusterStart:clusterEnd]
		coverageArea := float32(len(cluster)) * chunk.VoxelResolution * chunk.VoxelResolution
		if coverageArea+1e-5 >= minCoverageArea {
			y := navAverageWalkableCellY(cluster)
			out = append(out, NavBuildWalkableCell{
				X:         bucket.X,
				Y:         y,
				Z:         bucket.Z,
				PolygonID: navVoxelRasterCellPolygonID(bucket.X, y, bucket.Z),
			})
		}
		clusterStart = clusterEnd
	}
	return out
}

func navCoarseVoxelMinCoverage(profile NavAgentProfileDef, voxelResolution float32, cellSize float32) float32 {
	_ = profile
	if voxelResolution <= 0 || cellSize <= 0 {
		return 1
	}
	return 0.45
}

func navAverageWalkableCellY(cells []NavBuildWalkableCell) int {
	if len(cells) == 0 {
		return 0
	}
	sum := 0
	for _, cell := range cells {
		sum += cell.Y
	}
	return int(math.Round(float64(sum) / float64(len(cells))))
}

func navVoxelRasterCellPolygonID(x, y, z int) string {
	return "voxel_cell:" + itoa(x) + ":" + itoa(y) + ":" + itoa(z)
}

func normalizeCoarseNavWalkableCellHeights(cells []NavBuildWalkableCell, verticalResolution float32) {
	if len(cells) < 2 || verticalResolution <= 0 {
		return
	}
	tolerance := navCoarseHeightSnapToleranceVoxels(verticalResolution)
	cellsByXZ := make(map[[2]int][]int, len(cells))
	for i, cell := range cells {
		key := [2]int{cell.X, cell.Z}
		cellsByXZ[key] = append(cellsByXZ[key], i)
	}
	visited := make([]bool, len(cells))
	for i := range cells {
		if visited[i] {
			continue
		}
		component := collectCoarseHeightComponent(cells, cellsByXZ, visited, i, tolerance)
		normalizeCoarseHeightComponent(cells, component, tolerance)
	}
}

func smoothNavCompactHeightfieldCells(cells []NavBuildWalkableCell, metrics navBuildCellMetrics, profile NavAgentProfileDef) {
	if len(cells) < 2 || metrics.Vertical <= 0 {
		return
	}
	maxDeltaCells := navCompactHeightfieldSmoothToleranceCells(metrics, profile)
	if maxDeltaCells <= 0 {
		return
	}
	cellsByXZ := make(map[[2]int][]int, len(cells))
	for i, cell := range cells {
		cellsByXZ[[2]int{cell.X, cell.Z}] = append(cellsByXZ[[2]int{cell.X, cell.Z}], i)
	}
	visited := make([]bool, len(cells))
	for i := range cells {
		if visited[i] {
			continue
		}
		component := collectCompactHeightfieldSmoothComponent(cells, cellsByXZ, visited, i, metrics, profile)
		snapCompactHeightfieldNoiseComponent(cells, component, maxDeltaCells)
	}
}

func navCompactHeightfieldSmoothToleranceCells(metrics navBuildCellMetrics, profile NavAgentProfileDef) int {
	if metrics.Vertical <= 0 {
		return 0
	}
	EnsureNavAgentProfileDefaults(&profile)
	tolerance := profile.StepHeight * 0.35
	if tolerance <= 0 {
		return 0
	}
	return int(math.Floor(float64(tolerance/metrics.Vertical + 1e-4)))
}

func collectCompactHeightfieldSmoothComponent(cells []NavBuildWalkableCell, cellsByXZ map[[2]int][]int, visited []bool, start int, metrics navBuildCellMetrics, profile NavAgentProfileDef) []int {
	component := make([]int, 0, 16)
	queue := []int{start}
	visited[start] = true
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		component = append(component, index)
		cell := cells[index]
		neighbors := [][2]int{
			{cell.X + 1, cell.Z},
			{cell.X - 1, cell.Z},
			{cell.X, cell.Z + 1},
			{cell.X, cell.Z - 1},
		}
		for _, key := range neighbors {
			for _, next := range cellsByXZ[key] {
				if visited[next] {
					continue
				}
				if !navCellsCanConnect(cell, cells[next], metrics, profile) {
					continue
				}
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	return component
}

func snapCompactHeightfieldNoiseComponent(cells []NavBuildWalkableCell, component []int, maxDeltaCells int) {
	if len(component) < navMinContourRegionCells() || maxDeltaCells <= 0 {
		return
	}
	minY := cells[component[0]].Y
	maxY := minY
	heightCounts := make(map[int]int)
	for _, index := range component {
		y := cells[index].Y
		minY = minNavInt(minY, y)
		maxY = maxNavInt(maxY, y)
		heightCounts[y]++
	}
	if maxY-minY > maxDeltaCells {
		return
	}
	targetY := compactHeightfieldDominantY(heightCounts)
	for _, index := range component {
		cells[index].Y = targetY
		cells[index].PolygonID = navVoxelRasterCellPolygonID(cells[index].X, targetY, cells[index].Z)
	}
}

func compactHeightfieldDominantY(heightCounts map[int]int) int {
	targetY := 0
	targetCount := -1
	for y, count := range heightCounts {
		if count > targetCount || (count == targetCount && y < targetY) {
			targetY = y
			targetCount = count
		}
	}
	return targetY
}

func navCoarseHeightSnapToleranceVoxels(verticalResolution float32) int {
	_ = verticalResolution
	return 1
}

func collectCoarseHeightComponent(cells []NavBuildWalkableCell, cellsByXZ map[[2]int][]int, visited []bool, start int, tolerance int) []int {
	component := make([]int, 0, 8)
	queue := []int{start}
	visited[start] = true
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		component = append(component, index)
		cell := cells[index]
		neighbors := [][2]int{
			{cell.X + 1, cell.Z},
			{cell.X - 1, cell.Z},
			{cell.X, cell.Z + 1},
			{cell.X, cell.Z - 1},
		}
		for _, key := range neighbors {
			for _, next := range cellsByXZ[key] {
				if visited[next] {
					continue
				}
				if absNavIntAsInt(cells[next].Y-cell.Y) > tolerance {
					continue
				}
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	return component
}

func normalizeCoarseHeightComponent(cells []NavBuildWalkableCell, component []int, tolerance int) {
	if len(component) < 2 {
		return
	}
	heightCounts := make(map[int]int)
	for _, index := range component {
		heightCounts[cells[index].Y]++
	}
	dominantY := cells[component[0]].Y
	dominantCount := 0
	for y, count := range heightCounts {
		if count > dominantCount || (count == dominantCount && y < dominantY) {
			dominantY = y
			dominantCount = count
		}
	}
	if dominantCount*5 < len(component)*3 {
		return
	}
	for _, index := range component {
		if absNavIntAsInt(cells[index].Y-dominantY) > tolerance {
			continue
		}
		cells[index].Y = dominantY
		cells[index].PolygonID = navVoxelRasterCellPolygonID(cells[index].X, dominantY, cells[index].Z)
	}
}

func navCellHasAgentClearance(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, sampler importedWorldNavOccupancySampler, x, floorY, z int) bool {
	return navCellAgentClearanceRejectReason(chunk, profile, sampler, x, floorY, z) == ""
}

func navCellAgentClearanceRejectReason(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, sampler importedWorldNavOccupancySampler, x, floorY, z int) string {
	clearanceVoxels := int(math.Ceil(float64(profile.Height / chunk.VoxelResolution)))
	if clearanceVoxels <= 0 {
		clearanceVoxels = 1
	}
	if floorY < 0 {
		return "floor_below_chunk"
	}
	radiusVoxels := float64(profile.Radius / chunk.VoxelResolution)
	radiusCells := int(math.Ceil(radiusVoxels))
	centerX := float64(x) + 0.5
	centerZ := float64(z) + 0.5
	stepBlockerNegX := false
	stepBlockerPosX := false
	stepBlockerNegZ := false
	stepBlockerPosZ := false
	for oz := z - radiusCells; oz <= z+radiusCells; oz++ {
		for ox := x - radiusCells; ox <= x+radiusCells; ox++ {
			if !navVoxelAABBTouchesAgentRadius(centerX, centerZ, radiusVoxels, ox, oz) {
				continue
			}
			columnFloorY := floorY
			if ox != x || oz != z {
				if localFloorY, ok := navReachableTerrainFloorYForClearance(chunk, profile, sampler, floorY, centerX, centerZ, ox, oz); ok {
					columnFloorY = localFloorY
					switch navCardinalStepBlockerDirection(x, z, ox, oz) {
					case "neg_x":
						stepBlockerNegX = true
					case "pos_x":
						stepBlockerPosX = true
					case "neg_z":
						stepBlockerNegZ = true
					case "pos_z":
						stepBlockerPosZ = true
					}
				}
			}
			for oy := columnFloorY; oy < columnFloorY+clearanceVoxels; oy++ {
				blocked, known := sampler.Occupied(ox, oy, oz)
				if !known {
					return fmt.Sprintf("unknown_clearance:%d:%d:%d", ox, oy, oz)
				}
				if blocked {
					if columnFloorY == floorY && (ox != x || oz != z) && navOccupiedVoxelIsStepTerrain(chunk, profile, sampler, floorY, ox, oy, oz) {
						switch navStepBlockerDirection(x, z, ox, oz) {
						case "neg_x":
							stepBlockerNegX = true
						case "pos_x":
							stepBlockerPosX = true
						case "neg_z":
							stepBlockerNegZ = true
						case "pos_z":
							stepBlockerPosZ = true
						}
						continue
					}
					return fmt.Sprintf("blocked_clearance:%d:%d:%d", ox, oy, oz)
				}
			}
		}
	}
	if (stepBlockerNegX && stepBlockerPosX) || (stepBlockerNegZ && stepBlockerPosZ) {
		return "opposing_step_blockers"
	}
	return ""
}

func navReachableTerrainFloorYForClearance(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, sampler importedWorldNavOccupancySampler, baseFloorY int, centerX, centerZ float64, x, z int) (int, bool) {
	maxRiseVoxels := navReachableTerrainRiseVoxels(chunk, profile, centerX, centerZ, x, z)
	if maxRiseVoxels <= 0 {
		return 0, false
	}
	baseSupported, known := sampler.Occupied(x, baseFloorY-1, z)
	if !known || !baseSupported {
		return 0, false
	}
	for candidateFloorY := baseFloorY + 1; candidateFloorY <= baseFloorY+maxRiseVoxels; candidateFloorY++ {
		supported, known := sampler.Occupied(x, candidateFloorY-1, z)
		if !known {
			return 0, false
		}
		if !supported {
			continue
		}
		blockedAbove, known := sampler.Occupied(x, candidateFloorY, z)
		if !known {
			return 0, false
		}
		if !blockedAbove {
			return candidateFloorY, true
		}
	}
	return 0, false
}

func navReachableTerrainRiseVoxels(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, centerX, centerZ float64, x, z int) int {
	if chunk == nil || chunk.VoxelResolution <= 0 || profile.StepHeight <= 0 {
		return 0
	}
	stepVoxels := int(math.Floor(float64(profile.StepHeight/chunk.VoxelResolution + 1e-4)))
	if stepVoxels <= 0 {
		return 0
	}
	return stepVoxels
}

func navCardinalStepBlockerDirection(x int, z int, blockerX int, blockerZ int) string {
	dx := blockerX - x
	dz := blockerZ - z
	if dx != 0 && dz != 0 {
		return ""
	}
	return navStepBlockerDirection(x, z, blockerX, blockerZ)
}

func navStepBlockerDirection(x int, z int, blockerX int, blockerZ int) string {
	dx := blockerX - x
	dz := blockerZ - z
	if absNavIntAsInt(dx) >= absNavIntAsInt(dz) {
		if dx < 0 {
			return "neg_x"
		}
		if dx > 0 {
			return "pos_x"
		}
	}
	if dz < 0 {
		return "neg_z"
	}
	if dz > 0 {
		return "pos_z"
	}
	return ""
}

func navOccupiedVoxelIsStepTerrain(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, sampler importedWorldNavOccupancySampler, floorY int, x int, y int, z int) bool {
	if chunk == nil || chunk.VoxelResolution <= 0 || y < floorY {
		return false
	}
	supported, known := sampler.Occupied(x, floorY-1, z)
	if !known || !supported {
		return false
	}
	stepVoxels := int(math.Floor(float64(profile.StepHeight/chunk.VoxelResolution + 1e-4)))
	if stepVoxels <= 0 {
		return false
	}
	maxFloorY := floorY + stepVoxels
	topY := y
	for nextY := y + 1; nextY <= maxFloorY; nextY++ {
		blocked, known := sampler.Occupied(x, nextY, z)
		if !known {
			return false
		}
		if !blocked {
			break
		}
		topY = nextY
	}
	candidateFloorY := topY + 1
	if candidateFloorY <= floorY || candidateFloorY > maxFloorY {
		return false
	}
	blockedAbove, known := sampler.Occupied(x, candidateFloorY, z)
	return known && !blockedAbove
}

type importedWorldNavOccupancySampler struct {
	center       TerrainChunkCoordDef
	chunkSize    int
	resolution   float32
	reach        navBuildChunkReach
	cache        *NavTileBuildCache
	chunks       map[TerrainChunkCoordDef]*ImportedWorldChunkDef
	occupancies  map[TerrainChunkCoordDef]navDenseVoxelOccupancy
	blockers     map[[3]int]struct{}
	centerOffset TerrainChunkCoordDef
}

func newImportedWorldNavOccupancySampler(cache *NavTileBuildCache, center *ImportedWorldChunkDef, centerData navChunkNavData, neighbors map[TerrainChunkCoordDef]*ImportedWorldChunkDef, profile NavAgentProfileDef) importedWorldNavOccupancySampler {
	sampler := importedWorldNavOccupancySampler{
		chunks:      make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef),
		occupancies: make(map[TerrainChunkCoordDef]navDenseVoxelOccupancy),
		cache:       cache,
	}
	if center == nil {
		return sampler
	}
	sampler.center = center.Coord
	sampler.chunkSize = center.ChunkSize
	sampler.resolution = center.VoxelResolution
	sampler.reach = navBuildChunkReachForProfile(center.ChunkSize, center.VoxelResolution, profile)
	sampler.chunks[center.Coord] = center
	if !centerData.Occupancy.Valid() {
		centerData = navTileBuildChunkData(cache, center)
	}
	sampler.occupancies[center.Coord] = centerData.Occupancy
	for coord, chunk := range neighbors {
		if chunk == nil {
			continue
		}
		if chunk.ChunkSize != center.ChunkSize || chunk.VoxelResolution != center.VoxelResolution {
			continue
		}
		if !navBuildCoordWithinReach(center.Coord, coord, sampler.reach) {
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
	if _, ok := s.blockers[[3]int{x, y, z}]; ok {
		return true, true
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
	return occupancy.Occupied(navPositiveMod(x, s.chunkSize), navPositiveMod(y, s.chunkSize), navPositiveMod(z, s.chunkSize)), true
}

func (s *importedWorldNavOccupancySampler) occupancy(coord TerrainChunkCoordDef) (navDenseVoxelOccupancy, bool) {
	if s == nil {
		return navDenseVoxelOccupancy{}, false
	}
	if occupancy, ok := s.occupancies[coord]; ok {
		return occupancy, true
	}
	chunk, ok := s.chunks[coord]
	if !ok || chunk == nil {
		return navDenseVoxelOccupancy{}, false
	}
	data := navTileBuildChunkData(s.cache, chunk)
	occupancy := data.Occupancy
	s.occupancies[coord] = occupancy
	return occupancy, true
}

func addNavBuildSourceClearanceBlockersToSampler(s *importedWorldNavOccupancySampler, source *NavBuildSourceDef, center *ImportedWorldChunkDef, profile NavAgentProfileDef) {
	if s == nil || source == nil || center == nil || center.ChunkSize <= 0 || center.VoxelResolution <= 0 {
		return
	}
	EnsureNavBuildSourceDefaults(source)
	origin := importedWorldChunkWorldOrigin(center)
	resolution := center.VoxelResolution
	worldSize := float32(center.ChunkSize) * resolution
	padding := maxNavFloat32(profile.Radius+resolution, resolution)
	boundsMin := Vec3{origin[0] - padding, origin[1] - padding, origin[2] - padding}
	boundsMax := Vec3{origin[0] + worldSize + padding, origin[1] + worldSize + maxNavFloat32(profile.Height, resolution), origin[2] + worldSize + padding}
	for surfaceIndex := range source.Surfaces {
		surface := source.Surfaces[surfaceIndex]
		EnsureNavBuildSurfaceDefaults(&surface)
		if surface.Kind != NavBuildSurfaceClearanceBlocker {
			continue
		}
		if len(surface.Indices) >= 3 && len(surface.Indices)%3 == 0 {
			for tri := 0; tri < len(surface.Indices); tri += 3 {
				a := surface.Indices[tri]
				b := surface.Indices[tri+1]
				c := surface.Indices[tri+2]
				if a < 0 || b < 0 || c < 0 || a >= len(surface.Vertices) || b >= len(surface.Vertices) || c >= len(surface.Vertices) {
					continue
				}
				addNavBuildSourceClearanceBlockerVerticesToSampler(s, []Vec3{surface.Vertices[a], surface.Vertices[b], surface.Vertices[c]}, origin, resolution, boundsMin, boundsMax)
			}
			continue
		}
		addNavBuildSourceClearanceBlockerVerticesToSampler(s, surface.Vertices, origin, resolution, boundsMin, boundsMax)
	}
}

func navBuildSourceHasClearanceBlockers(source *NavBuildSourceDef) bool {
	if source == nil {
		return false
	}
	EnsureNavBuildSourceDefaults(source)
	for i := range source.Surfaces {
		surface := source.Surfaces[i]
		EnsureNavBuildSurfaceDefaults(&surface)
		if surface.Kind == NavBuildSurfaceClearanceBlocker && len(surface.Vertices) >= 3 {
			return true
		}
	}
	return false
}

func navBuildSourceClearanceBlockerHash(source *NavBuildSourceDef) string {
	if source == nil {
		return ""
	}
	EnsureNavBuildSourceDefaults(source)
	blockers := make([]NavBuildSurfaceDef, 0)
	for i := range source.Surfaces {
		surface := source.Surfaces[i]
		EnsureNavBuildSurfaceDefaults(&surface)
		if surface.Kind == NavBuildSurfaceClearanceBlocker && len(surface.Vertices) >= 3 {
			blockers = append(blockers, surface)
		}
	}
	if len(blockers) == 0 {
		return ""
	}
	sort.Slice(blockers, func(i, j int) bool {
		if blockers[i].ID != blockers[j].ID {
			return blockers[i].ID < blockers[j].ID
		}
		return blockers[i].SourceTag < blockers[j].SourceTag
	})
	h := sha256.New()
	writeStringHash(h, "nav_build_source_clearance_blockers_v1")
	data, _ := json.Marshal(blockers)
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func addNavBuildSourceClearanceBlockerVerticesToSampler(s *importedWorldNavOccupancySampler, vertices []Vec3, origin [3]float32, resolution float32, boundsMin Vec3, boundsMax Vec3) {
	if s == nil || len(vertices) < 3 || resolution <= 0 {
		return
	}
	minX, maxX := vertices[0][0], vertices[0][0]
	minY, maxY := vertices[0][1], vertices[0][1]
	minZ, maxZ := vertices[0][2], vertices[0][2]
	for _, vertex := range vertices[1:] {
		minX = minNavFloat32(minX, vertex[0])
		maxX = maxNavFloat32(maxX, vertex[0])
		minY = minNavFloat32(minY, vertex[1])
		maxY = maxNavFloat32(maxY, vertex[1])
		minZ = minNavFloat32(minZ, vertex[2])
		maxZ = maxNavFloat32(maxZ, vertex[2])
	}
	half := resolution * 0.5
	minX -= half
	maxX += half
	minY -= half
	maxY += half
	minZ -= half
	maxZ += half
	if maxX < boundsMin[0] || minX > boundsMax[0] || maxY < boundsMin[1] || minY > boundsMax[1] || maxZ < boundsMin[2] || minZ > boundsMax[2] {
		return
	}
	minX = maxNavFloat32(minX, boundsMin[0])
	maxX = minNavFloat32(maxX, boundsMax[0])
	minY = maxNavFloat32(minY, boundsMin[1])
	maxY = minNavFloat32(maxY, boundsMax[1])
	minZ = maxNavFloat32(minZ, boundsMin[2])
	maxZ = minNavFloat32(maxZ, boundsMax[2])
	startX := int(math.Floor(float64((minX - origin[0]) / resolution)))
	endX := int(math.Ceil(float64((maxX-origin[0])/resolution))) - 1
	startY := int(math.Floor(float64((minY - origin[1]) / resolution)))
	endY := int(math.Ceil(float64((maxY-origin[1])/resolution))) - 1
	startZ := int(math.Floor(float64((minZ - origin[2]) / resolution)))
	endZ := int(math.Ceil(float64((maxZ-origin[2])/resolution))) - 1
	if s.blockers == nil {
		s.blockers = make(map[[3]int]struct{})
	}
	for x := startX; x <= endX; x++ {
		for y := startY; y <= endY; y++ {
			for z := startZ; z <= endZ; z++ {
				center := Vec3{
					origin[0] + (float32(x)+0.5)*resolution,
					origin[1] + (float32(y)+0.5)*resolution,
					origin[2] + (float32(z)+0.5)*resolution,
				}
				if !navBuildSourceClearanceBlockerTouchesVoxel(vertices, center, resolution) {
					continue
				}
				s.blockers[[3]int{x, y, z}] = struct{}{}
			}
		}
	}
}

func navBuildSourceClearanceBlockerTouchesVoxel(vertices []Vec3, center Vec3, resolution float32) bool {
	if len(vertices) < 3 || resolution <= 0 {
		return false
	}
	normal := navSurfaceNormal(vertices)
	length := navVec3Length(normal)
	if length <= 1e-5 {
		return false
	}
	normal = Vec3{normal[0] / length, normal[1] / length, normal[2] / length}
	offset := navVec3Sub(center, vertices[0])
	dist := offset[0]*normal[0] + offset[1]*normal[1] + offset[2]*normal[2]
	if absNavFloat32(dist) > resolution*0.75 {
		return false
	}
	axis := 0
	absX := absNavFloat32(normal[0])
	absY := absNavFloat32(normal[1])
	absZ := absNavFloat32(normal[2])
	if absY >= absX && absY >= absZ {
		axis = 1
	} else if absZ >= absX && absZ >= absY {
		axis = 2
	}
	return navBuildSourceProjectedPointInPolygon(vertices, center, axis, resolution*0.55)
}

func navBuildSourceProjectedPointInPolygon(vertices []Vec3, point Vec3, dropAxis int, epsilon float32) bool {
	if len(vertices) < 3 {
		return false
	}
	px, py := navBuildSourceProjectPoint2D(point, dropAxis)
	inside := false
	j := len(vertices) - 1
	for i := range vertices {
		ax, ay := navBuildSourceProjectPoint2D(vertices[i], dropAxis)
		bx, by := navBuildSourceProjectPoint2D(vertices[j], dropAxis)
		if navPointOnSegment2D(px, py, ax, ay, bx, by, epsilon) {
			return true
		}
		intersects := (ay > py) != (by > py)
		if intersects {
			x := (bx-ax)*(py-ay)/(by-ay) + ax
			if px < x {
				inside = !inside
			}
		}
		j = i
	}
	if inside {
		return true
	}
	return navBuildSourceProjectedPointNearPolygon(vertices, px, py, dropAxis, epsilon)
}

func navBuildSourceProjectedPointNearPolygon(vertices []Vec3, px float32, py float32, dropAxis int, epsilon float32) bool {
	if epsilon <= 0 {
		return false
	}
	epsilonSq := epsilon * epsilon
	j := len(vertices) - 1
	for i := range vertices {
		ax, ay := navBuildSourceProjectPoint2D(vertices[i], dropAxis)
		bx, by := navBuildSourceProjectPoint2D(vertices[j], dropAxis)
		if navPointSegmentDistanceSq2D(px, py, ax, ay, bx, by) <= epsilonSq {
			return true
		}
		j = i
	}
	return false
}

func navBuildSourceProjectPoint2D(point Vec3, dropAxis int) (float32, float32) {
	switch dropAxis {
	case 0:
		return point[1], point[2]
	case 1:
		return point[0], point[2]
	default:
		return point[0], point[1]
	}
}

func navPointOnSegment2D(px, py, ax, ay, bx, by, epsilon float32) bool {
	return navPointSegmentDistanceSq2D(px, py, ax, ay, bx, by) <= epsilon*epsilon
}

func navPointSegmentDistanceSq2D(px, py, ax, ay, bx, by float32) float32 {
	dx := bx - ax
	dy := by - ay
	lenSq := dx*dx + dy*dy
	if lenSq <= 1e-8 {
		ox := px - ax
		oy := py - ay
		return ox*ox + oy*oy
	}
	t := ((px-ax)*dx + (py-ay)*dy) / lenSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	cx := ax + t*dx
	cy := ay + t*dy
	ox := px - cx
	oy := py - cy
	return ox*ox + oy*oy
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

func mergeNavWalkableCells(cells []NavBuildWalkableCell, metrics navBuildCellMetrics, profile NavAgentProfileDef) []navBuildWalkableRegion {
	if len(cells) == 0 {
		return nil
	}
	remainingByXZ := make(map[[2]int][]int, len(cells))
	assigned := make([]bool, len(cells))
	for i, cell := range cells {
		key := [2]int{cell.X, cell.Z}
		remainingByXZ[key] = append(remainingByXZ[key], i)
	}
	keys := make([]int, 0, len(cells))
	for i := range cells {
		keys = append(keys, i)
	}
	sort.Slice(keys, func(i, j int) bool {
		a := cells[keys[i]]
		b := cells[keys[j]]
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		if a.X != b.X {
			return a.X < b.X
		}
		return a.Y < b.Y
	})

	regions := make([]navBuildWalkableRegion, 0)
	for _, region := range buildNonFlatNavContourRegions(cells, remainingByXZ, assigned, keys, metrics, profile) {
		for _, index := range region.Indices {
			assigned[index] = true
		}
		region.Area = navRegionTraversalArea(region, metrics, profile)
		region.PolygonID = navRegionPolygonID(region)
		regions = append(regions, region)
	}
	for _, region := range buildFlatNavContourRegions(cells, remainingByXZ, assigned, keys, metrics, profile) {
		for _, index := range region.Indices {
			assigned[index] = true
		}
		region.Area = navRegionTraversalArea(region, metrics, profile)
		region.PolygonID = navRegionPolygonID(region)
		regions = append(regions, region)
	}
	for _, startIndex := range keys {
		if assigned[startIndex] {
			continue
		}
		region := growNavWalkableRegion(cells, remainingByXZ, assigned, startIndex, metrics, profile)
		for _, index := range region.Indices {
			assigned[index] = true
		}
		region.Area = navRegionTraversalArea(region, metrics, profile)
		region.PolygonID = navRegionPolygonID(region)
		regions = append(regions, region)
	}
	return regions
}

func buildNonFlatNavContourRegions(cells []NavBuildWalkableCell, cellsByXZ map[[2]int][]int, assigned []bool, keys []int, metrics navBuildCellMetrics, profile NavAgentProfileDef) []navBuildWalkableRegion {
	visited := make([]bool, len(cells))
	regions := make([]navBuildWalkableRegion, 0)
	for _, startIndex := range keys {
		if assigned[startIndex] || visited[startIndex] {
			continue
		}
		component := collectConnectedNavCellComponent(cells, cellsByXZ, assigned, visited, startIndex, metrics, profile)
		if len(component) < navMinContourRegionCells() {
			continue
		}
		region, ok := nonFlatNavContourRegionFromComponent(cells, component, metrics, profile)
		if !ok {
			continue
		}
		regions = append(regions, region)
	}
	return regions
}

func buildFlatNavContourRegions(cells []NavBuildWalkableCell, cellsByXZ map[[2]int][]int, assigned []bool, keys []int, metrics navBuildCellMetrics, profile NavAgentProfileDef) []navBuildWalkableRegion {
	visited := make([]bool, len(cells))
	regions := make([]navBuildWalkableRegion, 0)
	for _, startIndex := range keys {
		if assigned[startIndex] || visited[startIndex] {
			continue
		}
		component := collectFlatNavCellComponent(cells, cellsByXZ, assigned, visited, startIndex)
		if len(component) < navMinContourRegionCells() {
			continue
		}
		if flatNavCellComponentTouchesWalkableHeightTransition(cells, cellsByXZ, assigned, component, metrics, profile) &&
			flatNavCellComponentShouldDeferToHeightfieldMerge(cells, component) {
			continue
		}
		contour, ok := traceFlatNavCellComponentContour(cells, component, metrics, profile)
		if !ok {
			continue
		}
		region := flatNavContourRegionFromComponent(cells, component, contour)
		regions = append(regions, region)
	}
	return regions
}

func navMinContourRegionCells() int {
	return 4
}

func collectFlatNavCellComponent(cells []NavBuildWalkableCell, cellsByXZ map[[2]int][]int, assigned []bool, visited []bool, startIndex int) []int {
	start := cells[startIndex]
	component := make([]int, 0, 16)
	queue := []int{startIndex}
	visited[startIndex] = true
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		component = append(component, index)
		cell := cells[index]
		neighbors := [][2]int{
			{cell.X + 1, cell.Z},
			{cell.X - 1, cell.Z},
			{cell.X, cell.Z + 1},
			{cell.X, cell.Z - 1},
		}
		for _, key := range neighbors {
			for _, next := range cellsByXZ[key] {
				if assigned[next] || visited[next] || cells[next].Y != start.Y {
					continue
				}
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	return component
}

func collectConnectedNavCellComponent(cells []NavBuildWalkableCell, cellsByXZ map[[2]int][]int, assigned []bool, visited []bool, startIndex int, metrics navBuildCellMetrics, profile NavAgentProfileDef) []int {
	component := make([]int, 0, 16)
	queue := []int{startIndex}
	visited[startIndex] = true
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		component = append(component, index)
		cell := cells[index]
		neighbors := [][2]int{
			{cell.X + 1, cell.Z},
			{cell.X - 1, cell.Z},
			{cell.X, cell.Z + 1},
			{cell.X, cell.Z - 1},
		}
		for _, key := range neighbors {
			for _, next := range cellsByXZ[key] {
				if assigned[next] || visited[next] {
					continue
				}
				if !navCellsCanConnect(cell, cells[next], metrics, profile) {
					continue
				}
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	return component
}

func flatNavCellComponentTouchesWalkableHeightTransition(cells []NavBuildWalkableCell, cellsByXZ map[[2]int][]int, assigned []bool, component []int, metrics navBuildCellMetrics, profile NavAgentProfileDef) bool {
	for _, index := range component {
		cell := cells[index]
		neighbors := [][2]int{
			{cell.X + 1, cell.Z},
			{cell.X - 1, cell.Z},
			{cell.X, cell.Z + 1},
			{cell.X, cell.Z - 1},
		}
		for _, key := range neighbors {
			for _, next := range cellsByXZ[key] {
				if assigned[next] || cells[next].Y == cell.Y {
					continue
				}
				if navCellsCanConnect(cell, cells[next], metrics, profile) {
					return true
				}
			}
		}
	}
	return false
}

func flatNavCellComponentShouldDeferToHeightfieldMerge(cells []NavBuildWalkableCell, component []int) bool {
	if len(component) == 0 {
		return false
	}
	minX := cells[component[0]].X
	maxX := minX
	minZ := cells[component[0]].Z
	maxZ := minZ
	for _, index := range component {
		cell := cells[index]
		minX = minNavInt(minX, cell.X)
		maxX = maxNavInt(maxX, cell.X)
		minZ = minNavInt(minZ, cell.Z)
		maxZ = maxNavInt(maxZ, cell.Z)
	}
	width := maxX - minX + 1
	depth := maxZ - minZ + 1
	return minNavInt(width, depth) <= navHeightfieldMergeFlatBandMaxWidth()
}

func navHeightfieldMergeFlatBandMaxWidth() int {
	return 4
}

func traceFlatNavCellComponentContour(cells []NavBuildWalkableCell, component []int, metrics navBuildCellMetrics, profile NavAgentProfileDef) ([]navBuildContourPoint, bool) {
	if len(component) == 0 {
		return nil, false
	}
	cellSet := make(map[[2]int]struct{}, len(component))
	for _, index := range component {
		cell := cells[index]
		cellSet[[2]int{cell.X, cell.Z}] = struct{}{}
	}
	type edge struct {
		From navBuildContourPoint
		To   navBuildContourPoint
	}
	edges := make([]edge, 0, len(component)*4)
	for _, index := range component {
		cell := cells[index]
		x := cell.X
		z := cell.Z
		if _, ok := cellSet[[2]int{x, z - 1}]; !ok {
			edges = append(edges, edge{From: navBuildContourPoint{X: x, Z: z}, To: navBuildContourPoint{X: x + 1, Z: z}})
		}
		if _, ok := cellSet[[2]int{x + 1, z}]; !ok {
			edges = append(edges, edge{From: navBuildContourPoint{X: x + 1, Z: z}, To: navBuildContourPoint{X: x + 1, Z: z + 1}})
		}
		if _, ok := cellSet[[2]int{x, z + 1}]; !ok {
			edges = append(edges, edge{From: navBuildContourPoint{X: x + 1, Z: z + 1}, To: navBuildContourPoint{X: x, Z: z + 1}})
		}
		if _, ok := cellSet[[2]int{x - 1, z}]; !ok {
			edges = append(edges, edge{From: navBuildContourPoint{X: x, Z: z + 1}, To: navBuildContourPoint{X: x, Z: z}})
		}
	}
	if len(edges) < 4 {
		return nil, false
	}
	nextByPoint := make(map[navBuildContourPoint]navBuildContourPoint, len(edges))
	start := edges[0].From
	for _, edge := range edges {
		if _, exists := nextByPoint[edge.From]; exists {
			return nil, false
		}
		nextByPoint[edge.From] = edge.To
		if navContourPointLess(edge.From, start) {
			start = edge.From
		}
	}
	contour := make([]navBuildContourPoint, 0, len(edges))
	current := start
	for step := 0; step <= len(edges); step++ {
		contour = append(contour, current)
		next, ok := nextByPoint[current]
		if !ok {
			return nil, false
		}
		current = next
		if current == start {
			if step+1 != len(edges) {
				return nil, false
			}
			contour = simplifyNavContourPoints(contour)
			contour = simplifyNavContourPointsBounded(contour, cells, component, metrics, profile)
			return contour, true
		}
	}
	return nil, false
}

func navContourPointLess(a, b navBuildContourPoint) bool {
	if a.Z != b.Z {
		return a.Z < b.Z
	}
	return a.X < b.X
}

func simplifyNavContourPoints(points []navBuildContourPoint) []navBuildContourPoint {
	if len(points) < 3 {
		return points
	}
	out := make([]navBuildContourPoint, 0, len(points))
	for i := range points {
		prev := points[(i+len(points)-1)%len(points)]
		curr := points[i]
		next := points[(i+1)%len(points)]
		if (prev.X == curr.X && curr.X == next.X) || (prev.Z == curr.Z && curr.Z == next.Z) {
			continue
		}
		out = append(out, curr)
	}
	return out
}

func simplifyNavContourPointsBounded(points []navBuildContourPoint, cells []NavBuildWalkableCell, component []int, metrics navBuildCellMetrics, profile NavAgentProfileDef) []navBuildContourPoint {
	if len(points) <= 4 || len(component) == 0 || metrics.Horizontal <= 0 {
		return points
	}
	tolerance := navContourSimplificationToleranceGrid(metrics, profile)
	if tolerance <= 0 {
		return points
	}
	out := append([]navBuildContourPoint(nil), points...)
	for {
		removed := false
		for i := range out {
			if len(out) <= 3 {
				return out
			}
			prev := out[(i+len(out)-1)%len(out)]
			curr := out[i]
			next := out[(i+1)%len(out)]
			if navContourPointDistanceToSegment(curr, prev, next) > tolerance {
				continue
			}
			candidate := make([]navBuildContourPoint, 0, len(out)-1)
			candidate = append(candidate, out[:i]...)
			candidate = append(candidate, out[i+1:]...)
			candidate = simplifyNavContourPoints(candidate)
			if !navContourSimplificationPreservesComponent(candidate, cells, component) {
				continue
			}
			out = candidate
			removed = true
			break
		}
		if !removed {
			return out
		}
	}
}

func navContourSimplificationToleranceGrid(metrics navBuildCellMetrics, profile NavAgentProfileDef) float32 {
	if metrics.Horizontal <= 0 {
		return 0
	}
	EnsureNavAgentProfileDefaults(&profile)
	toleranceWorld := profile.Radius * 0.5
	if toleranceWorld <= 0 {
		toleranceWorld = metrics.Horizontal
	}
	toleranceWorld = minNavFloat32(toleranceWorld, metrics.Horizontal)
	return toleranceWorld / metrics.Horizontal
}

func navContourPointDistanceToSegment(point, a, b navBuildContourPoint) float32 {
	px := float32(point.X)
	pz := float32(point.Z)
	ax := float32(a.X)
	az := float32(a.Z)
	bx := float32(b.X)
	bz := float32(b.Z)
	dx := bx - ax
	dz := bz - az
	lengthSq := dx*dx + dz*dz
	if lengthSq <= 1e-6 {
		return navVec2Length(px-ax, pz-az)
	}
	t := ((px-ax)*dx + (pz-az)*dz) / lengthSq
	t = clampNavFloat32(t, 0, 1)
	closestX := ax + dx*t
	closestZ := az + dz*t
	return navVec2Length(px-closestX, pz-closestZ)
}

func navContourSimplificationPreservesComponent(contour []navBuildContourPoint, cells []NavBuildWalkableCell, component []int) bool {
	if len(contour) < 3 || absNavFloat32(navContourSignedArea(contour)) <= 1e-4 || navContourSelfIntersects(contour) {
		return false
	}
	minX := cells[component[0]].X
	maxX := minX
	minZ := cells[component[0]].Z
	maxZ := minZ
	componentCells := make(map[[2]int]struct{}, len(component))
	for _, index := range component {
		cell := cells[index]
		key := [2]int{cell.X, cell.Z}
		componentCells[key] = struct{}{}
		minX = minNavInt(minX, cell.X)
		maxX = maxNavInt(maxX, cell.X)
		minZ = minNavInt(minZ, cell.Z)
		maxZ = maxNavInt(maxZ, cell.Z)
	}
	for _, index := range component {
		cell := cells[index]
		if !navBuildContourContainsGridPoint(contour, float32(cell.X)+0.5, float32(cell.Z)+0.5) {
			return false
		}
	}
	for z := minZ; z <= maxZ; z++ {
		for x := minX; x <= maxX; x++ {
			if _, ok := componentCells[[2]int{x, z}]; ok {
				continue
			}
			if navBuildContourContainsGridPoint(contour, float32(x)+0.5, float32(z)+0.5) {
				return false
			}
		}
	}
	return true
}

func navContourSignedArea(contour []navBuildContourPoint) float32 {
	area := float32(0)
	for i := range contour {
		a := contour[i]
		b := contour[(i+1)%len(contour)]
		area += float32(a.X*b.Z - b.X*a.Z)
	}
	return area * 0.5
}

func navContourSelfIntersects(contour []navBuildContourPoint) bool {
	for i := range contour {
		a0 := contour[i]
		a1 := contour[(i+1)%len(contour)]
		for j := i + 1; j < len(contour); j++ {
			if i == j || (i+1)%len(contour) == j || i == (j+1)%len(contour) {
				continue
			}
			b0 := contour[j]
			b1 := contour[(j+1)%len(contour)]
			if navContourSegmentsIntersect(a0, a1, b0, b1) {
				return true
			}
		}
	}
	return false
}

func navContourSegmentsIntersect(a0, a1, b0, b1 navBuildContourPoint) bool {
	o1 := navContourOrientation(a0, a1, b0)
	o2 := navContourOrientation(a0, a1, b1)
	o3 := navContourOrientation(b0, b1, a0)
	o4 := navContourOrientation(b0, b1, a1)
	if o1 != o2 && o3 != o4 {
		return true
	}
	if o1 == 0 && navContourPointOnSegment(b0, a0, a1) {
		return true
	}
	if o2 == 0 && navContourPointOnSegment(b1, a0, a1) {
		return true
	}
	if o3 == 0 && navContourPointOnSegment(a0, b0, b1) {
		return true
	}
	if o4 == 0 && navContourPointOnSegment(a1, b0, b1) {
		return true
	}
	return false
}

func navContourOrientation(a, b, c navBuildContourPoint) int {
	value := (b.X-a.X)*(c.Z-a.Z) - (b.Z-a.Z)*(c.X-a.X)
	if value > 0 {
		return 1
	}
	if value < 0 {
		return -1
	}
	return 0
}

func navContourPointOnSegment(point, a, b navBuildContourPoint) bool {
	return point.X >= minNavInt(a.X, b.X) &&
		point.X <= maxNavInt(a.X, b.X) &&
		point.Z >= minNavInt(a.Z, b.Z) &&
		point.Z <= maxNavInt(a.Z, b.Z) &&
		navContourOrientation(a, b, point) == 0
}

func flatNavContourRegionFromComponent(cells []NavBuildWalkableCell, component []int, contour []navBuildContourPoint) navBuildWalkableRegion {
	first := cells[component[0]]
	region := navBuildWalkableRegion{
		MinX:    first.X,
		MaxX:    first.X,
		MinZ:    first.Z,
		MaxZ:    first.Z,
		BaseY:   first.Y,
		Contour: append([]navBuildContourPoint(nil), contour...),
		Cells:   make([]NavBuildWalkableCell, 0, len(component)),
		Indices: append([]int(nil), component...),
	}
	for _, index := range component {
		cell := cells[index]
		region.MinX = minNavInt(region.MinX, cell.X)
		region.MaxX = maxNavInt(region.MaxX, cell.X)
		region.MinZ = minNavInt(region.MinZ, cell.Z)
		region.MaxZ = maxNavInt(region.MaxZ, cell.Z)
		region.Cells = append(region.Cells, cell)
	}
	return region
}

func nonFlatNavContourRegionFromComponent(cells []NavBuildWalkableCell, component []int, metrics navBuildCellMetrics, profile NavAgentProfileDef) (navBuildWalkableRegion, bool) {
	if len(component) < navMinContourRegionCells() {
		return navBuildWalkableRegion{}, false
	}
	plane, ok := navFitComponentHeightPlane(cells, component)
	if !ok || (absNavFloat32(plane.x) <= 1e-4 && absNavFloat32(plane.z) <= 1e-4) {
		return navBuildWalkableRegion{}, false
	}
	if !navComponentMatchesHeightPlane(cells, component, plane, navNonFlatContourPlaneToleranceVoxels(metrics, profile)) {
		return navBuildWalkableRegion{}, false
	}
	contour, ok := traceFlatNavCellComponentContour(cells, component, metrics, profile)
	if !ok {
		return navBuildWalkableRegion{}, false
	}
	first := cells[component[0]]
	region := navBuildWalkableRegion{
		MinX:     first.X,
		MaxX:     first.X,
		MinZ:     first.Z,
		MaxZ:     first.Z,
		HasPlane: true,
		Contour:  append([]navBuildContourPoint(nil), contour...),
		Cells:    make([]NavBuildWalkableCell, 0, len(component)),
		Indices:  append([]int(nil), component...),
	}
	for _, index := range component {
		cell := cells[index]
		region.MinX = minNavInt(region.MinX, cell.X)
		region.MaxX = maxNavInt(region.MaxX, cell.X)
		region.MinZ = minNavInt(region.MinZ, cell.Z)
		region.MaxZ = maxNavInt(region.MaxZ, cell.Z)
		region.Cells = append(region.Cells, cell)
	}
	region.PlaneBase = navHeightPlaneYAt(plane, float32(region.MinX), float32(region.MinZ))
	region.PlaneX = plane.x
	region.PlaneZ = plane.z
	region.BaseY = int(math.Round(float64(region.PlaneBase)))
	return region, true
}

type navBuildHeightPlane struct {
	base float32
	x    float32
	z    float32
}

func navFitComponentHeightPlane(cells []NavBuildWalkableCell, component []int) (navBuildHeightPlane, bool) {
	if len(component) < 3 {
		return navBuildHeightPlane{}, false
	}
	var ata [3][3]float64
	var aty [3]float64
	for _, index := range component {
		cell := cells[index]
		row := [3]float64{1, float64(cell.X), float64(cell.Z)}
		y := float64(cell.Y)
		for r := 0; r < 3; r++ {
			aty[r] += row[r] * y
			for c := 0; c < 3; c++ {
				ata[r][c] += row[r] * row[c]
			}
		}
	}
	solution, ok := solveNavBuild3x3(ata, aty)
	if !ok {
		return navBuildHeightPlane{}, false
	}
	return navBuildHeightPlane{base: float32(solution[0]), x: float32(solution[1]), z: float32(solution[2])}, true
}

func solveNavBuild3x3(a [3][3]float64, b [3]float64) ([3]float64, bool) {
	var m [3][4]float64
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			m[r][c] = a[r][c]
		}
		m[r][3] = b[r]
	}
	for col := 0; col < 3; col++ {
		pivot := col
		for row := col + 1; row < 3; row++ {
			if math.Abs(m[row][col]) > math.Abs(m[pivot][col]) {
				pivot = row
			}
		}
		if math.Abs(m[pivot][col]) <= 1e-9 {
			return [3]float64{}, false
		}
		if pivot != col {
			m[col], m[pivot] = m[pivot], m[col]
		}
		divisor := m[col][col]
		for c := col; c < 4; c++ {
			m[col][c] /= divisor
		}
		for row := 0; row < 3; row++ {
			if row == col {
				continue
			}
			factor := m[row][col]
			for c := col; c < 4; c++ {
				m[row][c] -= factor * m[col][c]
			}
		}
	}
	return [3]float64{m[0][3], m[1][3], m[2][3]}, true
}

func navComponentMatchesHeightPlane(cells []NavBuildWalkableCell, component []int, plane navBuildHeightPlane, tolerance float32) bool {
	for _, index := range component {
		cell := cells[index]
		if absNavFloat32(navHeightPlaneYAt(plane, float32(cell.X), float32(cell.Z))-float32(cell.Y)) > tolerance {
			return false
		}
	}
	return true
}

func navHeightPlaneYAt(plane navBuildHeightPlane, x, z float32) float32 {
	return plane.base + plane.x*x + plane.z*z
}

func navNonFlatContourPlaneToleranceVoxels(metrics navBuildCellMetrics, profile NavAgentProfileDef) float32 {
	if metrics.Vertical <= 0 {
		return 1
	}
	EnsureNavAgentProfileDefaults(&profile)
	toleranceWorld := profile.StepHeight * 0.5
	if toleranceWorld <= 0 {
		return 1
	}
	tolerance := toleranceWorld / metrics.Vertical
	if tolerance < 1.5 {
		tolerance = 1.5
	}
	return tolerance
}

func growNavWalkableRegion(cells []NavBuildWalkableCell, cellsByXZ map[[2]int][]int, assigned []bool, startIndex int, metrics navBuildCellMetrics, profile NavAgentProfileDef) navBuildWalkableRegion {
	start := cells[startIndex]
	row := []int{startIndex}
	slopeX := 0
	slopeXSet := false
	for nextX := start.X + 1; ; nextX++ {
		expectedY := 0
		hasExpectedY := false
		if slopeXSet {
			expectedY = start.Y + slopeX*(nextX-start.X)
			hasExpectedY = true
		}
		prev := cells[row[len(row)-1]]
		nextIndex, ok := findNavRegionCandidate(cells, cellsByXZ, assigned, nextX, start.Z, hasExpectedY, expectedY, &prev, metrics, profile)
		if !ok {
			break
		}
		if !slopeXSet {
			slopeX = cells[nextIndex].Y - start.Y
			slopeXSet = true
		}
		row = append(row, nextIndex)
	}
	slopeZ := 0
	slopeZSet := false
	rows := [][]int{row}
	for nextZ := start.Z + 1; ; nextZ++ {
		nextRow := make([]int, 0, len(row))
		rowIndex := nextZ - start.Z
		if !slopeZSet {
			above := cells[rows[len(rows)-1][0]]
			firstIndex, ok := findNavRegionCandidate(cells, cellsByXZ, assigned, start.X, nextZ, false, 0, &above, metrics, profile)
			if !ok {
				break
			}
			slopeZ = cells[firstIndex].Y - start.Y
			slopeZSet = true
			nextRow = append(nextRow, firstIndex)
		}
		complete := true
		for xOffset := len(nextRow); xOffset < len(row); xOffset++ {
			x := start.X + xOffset
			expectedY := start.Y + slopeX*xOffset + slopeZ*rowIndex
			above := cells[rows[len(rows)-1][xOffset]]
			nextIndex, ok := findNavRegionCandidate(cells, cellsByXZ, assigned, x, nextZ, true, expectedY, &above, metrics, profile)
			if !ok {
				complete = false
				break
			}
			if xOffset > 0 && !navCellsCanConnect(cells[nextRow[xOffset-1]], cells[nextIndex], metrics, profile) {
				complete = false
				break
			}
			nextRow = append(nextRow, nextIndex)
		}
		if !complete {
			break
		}
		rows = append(rows, nextRow)
	}
	region := navBuildWalkableRegion{
		MinX:    start.X,
		MaxX:    start.X + len(row) - 1,
		MinZ:    start.Z,
		MaxZ:    start.Z + len(rows) - 1,
		BaseY:   start.Y,
		SlopeX:  slopeX,
		SlopeZ:  slopeZ,
		Cells:   make([]NavBuildWalkableCell, 0, len(row)*len(rows)),
		Indices: make([]int, 0, len(row)*len(rows)),
	}
	for _, row := range rows {
		for _, index := range row {
			region.Cells = append(region.Cells, cells[index])
			region.Indices = append(region.Indices, index)
		}
	}
	return region
}

func findNavRegionCandidate(cells []NavBuildWalkableCell, cellsByXZ map[[2]int][]int, assigned []bool, x, z int, hasExpectedY bool, expectedY int, from *NavBuildWalkableCell, metrics navBuildCellMetrics, profile NavAgentProfileDef) (int, bool) {
	candidates := cellsByXZ[[2]int{x, z}]
	bestIndex := -1
	bestDelta := int(0)
	compareY := expectedY
	if !hasExpectedY && from != nil {
		compareY = from.Y
	}
	for _, index := range candidates {
		if assigned[index] {
			continue
		}
		cell := cells[index]
		if hasExpectedY && cell.Y != expectedY {
			continue
		}
		if from != nil && !navCellsCanConnect(*from, cell, metrics, profile) {
			continue
		}
		delta := absNavIntAsInt(cell.Y - compareY)
		if bestIndex == -1 || delta < bestDelta {
			bestIndex = index
			bestDelta = delta
		}
	}
	return bestIndex, bestIndex != -1
}

func assignNavCellRegionIDs(cells []NavBuildWalkableCell, regions []navBuildWalkableRegion) {
	for i := range cells {
		for _, region := range regions {
			if !navRegionContainsCell(region, cells[i]) {
				continue
			}
			cells[i].PolygonID = region.PolygonID
			cells[i].Neighbors = append([]string(nil), region.Neighbors...)
			break
		}
	}
}

func navRegionContainsCell(region navBuildWalkableRegion, cell NavBuildWalkableCell) bool {
	if len(region.Contour) > 0 {
		for _, regionCell := range region.Cells {
			if regionCell.X == cell.X && regionCell.Y == cell.Y && regionCell.Z == cell.Z {
				return true
			}
		}
		return false
	}
	if cell.X < region.MinX || cell.X > region.MaxX || cell.Z < region.MinZ || cell.Z > region.MaxZ {
		return false
	}
	return cell.Y == navRegionCellY(region, cell.X, cell.Z)
}

func applyNavRegionNeighbors(regions []navBuildWalkableRegion, metrics navBuildCellMetrics, profile NavAgentProfileDef) {
	for i := range regions {
		neighbors := make([]string, 0, 4)
		for j := range regions {
			if i == j {
				continue
			}
			if navRegionsConnectHorizontally(regions[i], regions[j], metrics, profile) {
				neighbors = append(neighbors, regions[j].PolygonID)
			}
		}
		sort.Strings(neighbors)
		regions[i].Neighbors = neighbors
	}
}

func navRegionsConnectHorizontally(a, b navBuildWalkableRegion, metrics navBuildCellMetrics, profile NavAgentProfileDef) bool {
	if len(a.Contour) > 0 || len(b.Contour) > 0 {
		return navRegionsConnectByCells(a, b, metrics, profile)
	}
	xTouch := a.MaxX+1 == b.MinX || b.MaxX+1 == a.MinX
	zOverlap := navIntRangesOverlap(a.MinZ, a.MaxZ, b.MinZ, b.MaxZ)
	if xTouch && zOverlap {
		left := a
		right := b
		if b.MaxX+1 == a.MinX {
			left = b
			right = a
		}
		for z := maxNavInt(left.MinZ, right.MinZ); z <= minNavInt(left.MaxZ, right.MaxZ); z++ {
			if navCellsCanConnect(
				NavBuildWalkableCell{X: left.MaxX, Y: navRegionCellY(left, left.MaxX, z), Z: z},
				NavBuildWalkableCell{X: right.MinX, Y: navRegionCellY(right, right.MinX, z), Z: z},
				metrics,
				profile,
			) {
				return true
			}
		}
		return false
	}
	zTouch := a.MaxZ+1 == b.MinZ || b.MaxZ+1 == a.MinZ
	xOverlap := navIntRangesOverlap(a.MinX, a.MaxX, b.MinX, b.MaxX)
	if zTouch && xOverlap {
		front := a
		back := b
		if b.MaxZ+1 == a.MinZ {
			front = b
			back = a
		}
		for x := maxNavInt(front.MinX, back.MinX); x <= minNavInt(front.MaxX, back.MaxX); x++ {
			if navCellsCanConnect(
				NavBuildWalkableCell{X: x, Y: navRegionCellY(front, x, front.MaxZ), Z: front.MaxZ},
				NavBuildWalkableCell{X: x, Y: navRegionCellY(back, x, back.MinZ), Z: back.MinZ},
				metrics,
				profile,
			) {
				return true
			}
		}
	}
	return false
}

func navRegionsConnectByCells(a, b navBuildWalkableRegion, metrics navBuildCellMetrics, profile NavAgentProfileDef) bool {
	if len(a.Cells) == 0 || len(b.Cells) == 0 {
		return false
	}
	bCellsByXZ := make(map[[2]int][]NavBuildWalkableCell, len(b.Cells))
	for _, cell := range b.Cells {
		key := [2]int{cell.X, cell.Z}
		bCellsByXZ[key] = append(bCellsByXZ[key], cell)
	}
	for _, aCell := range a.Cells {
		neighbors := [][2]int{
			{aCell.X + 1, aCell.Z},
			{aCell.X - 1, aCell.Z},
			{aCell.X, aCell.Z + 1},
			{aCell.X, aCell.Z - 1},
		}
		for _, key := range neighbors {
			for _, bCell := range bCellsByXZ[key] {
				if navCellsCanConnect(aCell, bCell, metrics, profile) {
					return true
				}
			}
		}
	}
	return false
}

func navIntRangesOverlap(aMin, aMax, bMin, bMax int) bool {
	return maxNavInt(aMin, bMin) <= minNavInt(aMax, bMax)
}

func navCellsCanConnect(a, b NavBuildWalkableCell, metrics navBuildCellMetrics, profile NavAgentProfileDef) bool {
	horizontalCells := absNavIntAsInt(a.X-b.X) + absNavIntAsInt(a.Z-b.Z)
	if horizontalCells != 1 {
		return false
	}
	delta := absNavInt(a.Y-b.Y) * metrics.Vertical
	const epsilon = float32(1e-4)
	return delta <= profile.StepHeight+epsilon
}

func navRegionTraversalArea(region navBuildWalkableRegion, metrics navBuildCellMetrics, profile NavAgentProfileDef) string {
	EnsureNavAgentProfileDefaults(&profile)
	if navRegionHeightRangeCells(region) <= 0 {
		return NavTraversalWalk
	}
	if navRegionSlopeWithinProfile(region, metrics, profile) {
		return NavTraversalRamp
	}
	if navRegionIsStepLike(region) {
		return NavTraversalStep
	}
	return NavTraversalStair
}

func navRegionHeightRangeCells(region navBuildWalkableRegion) int {
	if len(region.Cells) > 0 {
		minY := region.Cells[0].Y
		maxY := region.Cells[0].Y
		for _, cell := range region.Cells[1:] {
			minY = minNavInt(minY, cell.Y)
			maxY = maxNavInt(maxY, cell.Y)
		}
		return maxY - minY
	}
	if region.HasPlane {
		minY := navRegionHeightAtGrid(region, float32(region.MinX), float32(region.MinZ))
		maxY := minY
		for _, point := range []navBuildContourPoint{
			{X: region.MaxX + 1, Z: region.MinZ},
			{X: region.MaxX + 1, Z: region.MaxZ + 1},
			{X: region.MinX, Z: region.MaxZ + 1},
		} {
			y := navRegionHeightAtGrid(region, float32(point.X), float32(point.Z))
			minY = minNavFloat32(minY, y)
			maxY = maxNavFloat32(maxY, y)
		}
		return int(math.Round(float64(maxY - minY)))
	}
	minY := navRegionCellY(region, region.MinX, region.MinZ)
	maxY := minY
	for z := region.MinZ; z <= region.MaxZ; z++ {
		for x := region.MinX; x <= region.MaxX; x++ {
			y := navRegionCellY(region, x, z)
			minY = minNavInt(minY, y)
			maxY = maxNavInt(maxY, y)
		}
	}
	return maxY - minY
}

func navRegionSlopeWithinProfile(region navBuildWalkableRegion, metrics navBuildCellMetrics, profile NavAgentProfileDef) bool {
	if metrics.Horizontal <= 0 || metrics.Vertical <= 0 {
		return false
	}
	if profile.MaxSlopeDegrees <= 0 {
		return true
	}
	slopeX := float32(region.SlopeX)
	slopeZ := float32(region.SlopeZ)
	if region.HasPlane {
		slopeX = region.PlaneX
		slopeZ = region.PlaneZ
	}
	risePerHorizontal := navVec2Length(slopeX*metrics.Vertical, slopeZ*metrics.Vertical) / metrics.Horizontal
	angle := float32(math.Atan(float64(risePerHorizontal)) * 180 / math.Pi)
	const epsilon = float32(1e-4)
	return angle <= profile.MaxSlopeDegrees+epsilon
}

func navRegionIsStepLike(region navBuildWalkableRegion) bool {
	width := region.MaxX - region.MinX + 1
	depth := region.MaxZ - region.MinZ + 1
	return minNavInt(width, depth) <= 1 || len(region.Cells) < navMinContourRegionCells()
}

func appendNavRegionPolygon(tile *NavTileDef, origin [3]float32, metrics navBuildCellMetrics, region *navBuildWalkableRegion, stitcher *navBuildBorderStitcher) {
	if tile == nil || region == nil {
		return
	}
	area := firstNonEmptyNavString(region.Area, NavTraversalWalk)
	if len(region.Contour) >= 3 {
		vertices := make([]Vec3, 0, len(region.Contour))
		for _, point := range region.Contour {
			vertices = append(vertices, navRegionVertex(tile, origin, metrics, *region, float32(point.X), float32(point.Z), stitcher))
		}
		appendNavPolygonVertices(tile, region.PolygonID, vertices, area, nil)
		return
	}
	base := len(tile.Vertices)
	tile.Vertices = append(tile.Vertices,
		navRegionVertex(tile, origin, metrics, *region, float32(region.MinX), float32(region.MinZ), stitcher),
		navRegionVertex(tile, origin, metrics, *region, float32(region.MaxX+1), float32(region.MinZ), stitcher),
		navRegionVertex(tile, origin, metrics, *region, float32(region.MaxX+1), float32(region.MaxZ+1), stitcher),
		navRegionVertex(tile, origin, metrics, *region, float32(region.MinX), float32(region.MaxZ+1), stitcher),
	)
	tile.Polygons = append(tile.Polygons, NavPolygonDef{
		ID:       region.PolygonID,
		Vertices: []int{base, base + 1, base + 2, base + 3},
		Area:     area,
	})
}

func appendNavTileBorderSpans(tile *NavTileDef, origin [3]float32, metrics navBuildCellMetrics, cells []NavBuildWalkableCell, voxelResolution float32) {
	if tile == nil || metrics.Horizontal <= 0 || len(cells) == 0 {
		return
	}
	type spanKey struct {
		polygonID string
		edge      string
		area      string
	}
	spans := make(map[spanKey][]navPortalBoundarySpan)
	epsilon := navBuildBorderStitchEpsilon(metrics)
	for _, cell := range cells {
		polygon, ok := navTilePolygonAtBuildCell(tile, origin, metrics, cell, voxelResolution)
		if !ok || polygon.ID == "" {
			continue
		}
		area := firstNonEmptyNavString(polygon.Area, NavTraversalWalk)
		minX := navRegionGridToWorldX(origin, metrics, float32(cell.X))
		maxX := navRegionGridToWorldX(origin, metrics, float32(cell.X+1))
		minZ := navRegionGridToWorldZ(origin, metrics, float32(cell.Z))
		maxZ := navRegionGridToWorldZ(origin, metrics, float32(cell.Z+1))
		if minX > maxX {
			minX, maxX = maxX, minX
		}
		if minZ > maxZ {
			minZ, maxZ = maxZ, minZ
		}
		minX = clampNavFloat32(minX, tile.BoundsMin[0], tile.BoundsMax[0])
		maxX = clampNavFloat32(maxX, tile.BoundsMin[0], tile.BoundsMax[0])
		minZ = clampNavFloat32(minZ, tile.BoundsMin[2], tile.BoundsMax[2])
		maxZ = clampNavFloat32(maxZ, tile.BoundsMin[2], tile.BoundsMax[2])
		if absNavFloat32(minX-tile.BoundsMin[0]) <= epsilon && maxZ-minZ > epsilon {
			key := spanKey{polygonID: polygon.ID, edge: NavBorderEdgeMinX, area: area}
			spans[key] = appendNavPortalBoundarySpan(spans[key], minZ, maxZ)
		}
		if absNavFloat32(maxX-tile.BoundsMax[0]) <= epsilon && maxZ-minZ > epsilon {
			key := spanKey{polygonID: polygon.ID, edge: NavBorderEdgeMaxX, area: area}
			spans[key] = appendNavPortalBoundarySpan(spans[key], minZ, maxZ)
		}
		if absNavFloat32(minZ-tile.BoundsMin[2]) <= epsilon && maxX-minX > epsilon {
			key := spanKey{polygonID: polygon.ID, edge: NavBorderEdgeMinZ, area: area}
			spans[key] = appendNavPortalBoundarySpan(spans[key], minX, maxX)
		}
		if absNavFloat32(maxZ-tile.BoundsMax[2]) <= epsilon && maxX-minX > epsilon {
			key := spanKey{polygonID: polygon.ID, edge: NavBorderEdgeMaxZ, area: area}
			spans[key] = appendNavPortalBoundarySpan(spans[key], minX, maxX)
		}
	}
	if len(spans) == 0 {
		return
	}
	keys := make([]spanKey, 0, len(spans))
	for key := range spans {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].polygonID != keys[j].polygonID {
			return keys[i].polygonID < keys[j].polygonID
		}
		if keys[i].edge != keys[j].edge {
			return keys[i].edge < keys[j].edge
		}
		return keys[i].area < keys[j].area
	})
	tile.BorderSpans = tile.BorderSpans[:0]
	for _, key := range keys {
		for _, span := range navPortalMergeBoundarySpans(spans[key]) {
			tile.BorderSpans = append(tile.BorderSpans, NavBorderSpanDef{
				PolygonID: key.polygonID,
				Edge:      key.edge,
				Min:       span.Min,
				Max:       span.Max,
				Area:      key.area,
			})
		}
	}
}

func assignNavBuildCellPolygonIDsFromTile(tile *NavTileDef, origin [3]float32, metrics navBuildCellMetrics, cells []NavBuildWalkableCell, voxelResolution float32, profile NavAgentProfileDef) {
	for i := range cells {
		if polygonID, ok := navTilePolygonIDForBuildCell(tile, origin, metrics, cells[i], voxelResolution, profile); ok {
			cells[i].PolygonID = polygonID
		}
	}
}

func connectNavTilePolygonsFromCells(tile *NavTileDef, origin [3]float32, metrics navBuildCellMetrics, cells []NavBuildWalkableCell, voxelResolution float32, profile NavAgentProfileDef) {
	if tile == nil || len(tile.Polygons) == 0 || len(cells) == 0 {
		return
	}
	polygonIndexes := make(map[string]int, len(tile.Polygons))
	for i := range tile.Polygons {
		if tile.Polygons[i].ID != "" {
			polygonIndexes[tile.Polygons[i].ID] = i
		}
	}
	cellsByXZ := make(map[[2]int][]int, len(cells))
	cellPolygonIDs := make([]string, len(cells))
	for i, cell := range cells {
		cellsByXZ[[2]int{cell.X, cell.Z}] = append(cellsByXZ[[2]int{cell.X, cell.Z}], i)
		if cell.PolygonID != "" {
			if _, ok := polygonIndexes[cell.PolygonID]; ok {
				cellPolygonIDs[i] = cell.PolygonID
				continue
			}
		}
		if polygonID, ok := navTilePolygonIDForBuildCell(tile, origin, metrics, cell, voxelResolution, profile); ok {
			cellPolygonIDs[i] = polygonID
		}
	}
	for i, cell := range cells {
		fromID := cellPolygonIDs[i]
		fromIndex, ok := polygonIndexes[fromID]
		if !ok {
			continue
		}
		for _, key := range [][2]int{
			{cell.X + 1, cell.Z},
			{cell.X, cell.Z + 1},
		} {
			for _, next := range cellsByXZ[key] {
				toID := cellPolygonIDs[next]
				toIndex, ok := polygonIndexes[toID]
				if !ok || fromID == toID {
					continue
				}
				if !navCellsCanConnect(cell, cells[next], metrics, profile) {
					continue
				}
				tile.Polygons[fromIndex].Neighbors = appendUniqueNavString(tile.Polygons[fromIndex].Neighbors, toID)
				tile.Polygons[toIndex].Neighbors = appendUniqueNavString(tile.Polygons[toIndex].Neighbors, fromID)
			}
		}
	}
	for i := range tile.Polygons {
		sort.Strings(tile.Polygons[i].Neighbors)
	}
}

func navTilePolygonIDForBuildCell(tile *NavTileDef, origin [3]float32, metrics navBuildCellMetrics, cell NavBuildWalkableCell, voxelResolution float32, profile NavAgentProfileDef) (string, bool) {
	if polygon, ok := navTilePolygonAtBuildCell(tile, origin, metrics, cell, voxelResolution); ok && polygon.ID != "" {
		return polygon.ID, true
	}
	point := navBuildCellCenterPoint(origin, metrics, cell)
	if voxelResolution > 0 && !metrics.WorldAligned && metrics.Horizontal == voxelResolution {
		point[0] = origin[0] + (float32(cell.X)+0.5)*voxelResolution
		point[2] = origin[2] + (float32(cell.Z)+0.5)*voxelResolution
	}
	EnsureNavAgentProfileDefaults(&profile)
	tolerance := maxNavFloat32(metrics.Vertical*0.5, profile.StepHeight+metrics.Vertical*0.25)
	bestID := ""
	bestDelta := float32(0)
	for _, polygon := range tile.Polygons {
		if polygon.ID == "" || !navPolygonContainsXZ(tile, polygon, point) {
			continue
		}
		height, ok := navPolygonHeightAtXZ(tile, polygon, point)
		if !ok {
			height = navPolygonCenter(tile, polygon)[1]
		}
		delta := absNavFloat32(height - point[1])
		if delta > tolerance {
			continue
		}
		if bestID == "" || delta < bestDelta {
			bestID = polygon.ID
			bestDelta = delta
		}
	}
	return bestID, bestID != ""
}

func appendNavTileDropLinks(tile *NavTileDef, origin [3]float32, metrics navBuildCellMetrics, cells []NavBuildWalkableCell, voxelResolution float32, profile NavAgentProfileDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, cache *NavTileBuildCache, source *NavBuildSourceDef) {
	if tile == nil || metrics.Horizontal <= 0 || metrics.Vertical <= 0 || len(cells) == 0 {
		return
	}
	EnsureNavAgentProfileDefaults(&profile)
	if profile.MaxDropHeight <= 0 {
		return
	}
	refsByXZ := make(map[[2]int][]navDropLinkCellRef, len(cells))
	for _, cell := range cells {
		appendNavDropLinkCellRef(refsByXZ, navDropLinkCellRef{Coord: tile.Coord, Origin: origin, Cell: cell}, metrics)
	}
	for _, offset := range navBuildBorderNeighborOffsets() {
		if offset.X != 0 && offset.Z != 0 {
			continue
		}
		coord := TerrainChunkCoordDef{X: tile.Coord.X + offset.X, Y: tile.Coord.Y, Z: tile.Coord.Z + offset.Z}
		neighborChunk := chunks[coord]
		if neighborChunk == nil {
			continue
		}
		neighborIntermediate := navTileBuildIntermediateForChunkWithSource(cache, neighborChunk, profile, chunks, source)
		neighborOrigin := importedWorldChunkWorldOrigin(neighborChunk)
		for _, cell := range neighborIntermediate.Cells {
			appendNavDropLinkCellRef(refsByXZ, navDropLinkCellRef{Coord: coord, Origin: neighborOrigin, Cell: cell}, metrics)
		}
	}
	candidates := collectNavTileDropLinkCandidates(tile, metrics, refsByXZ, profile, voxelResolution)
	appendMergedNavTileDropLinks(tile, candidates, profile)
}

type navDropLinkCellRef struct {
	Coord  TerrainChunkCoordDef
	Origin [3]float32
	Cell   NavBuildWalkableCell
}

type navDropLinkCandidate struct {
	FromPolygonID string
	ToTileCoord   TerrainChunkCoordDef
	ToPolygonID   string
	DirX          int
	DirZ          int
	Span          int
	Start         Vec3
	End           Vec3
}

type navDropLinkMergeKey struct {
	FromPolygonID string
	ToTileCoord   TerrainChunkCoordDef
	ToPolygonID   string
	DirX          int
	DirZ          int
}

func appendNavDropLinkCellRef(refsByXZ map[[2]int][]navDropLinkCellRef, ref navDropLinkCellRef, metrics navBuildCellMetrics) {
	key := navDropLinkCellGridKey(ref, metrics)
	refsByXZ[key] = append(refsByXZ[key], ref)
}

func collectNavTileDropLinkCandidates(tile *NavTileDef, metrics navBuildCellMetrics, refsByXZ map[[2]int][]navDropLinkCellRef, profile NavAgentProfileDef, voxelResolution float32) []navDropLinkCandidate {
	if tile == nil || len(refsByXZ) == 0 {
		return nil
	}
	dirs := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	const epsilon = float32(1e-4)
	out := make([]navDropLinkCandidate, 0)
	seen := make(map[string]struct{})
	keys := make([][2]int, 0, len(refsByXZ))
	for key := range refsByXZ {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	for _, key := range keys {
		refs := refsByXZ[key]
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].Cell.Y != refs[j].Cell.Y {
				return refs[i].Cell.Y < refs[j].Cell.Y
			}
			if refs[i].Coord != refs[j].Coord {
				return terrainChunkCoordLess(refs[i].Coord, refs[j].Coord)
			}
			return refs[i].Cell.X < refs[j].Cell.X || (refs[i].Cell.X == refs[j].Cell.X && refs[i].Cell.Z < refs[j].Cell.Z)
		})
		for _, ref := range refs {
			for _, dir := range dirs {
				for _, neighbor := range refsByXZ[[2]int{key[0] + dir[0], key[1] + dir[1]}] {
					candidate, ok := navDropLinkCandidateForRefs(tile, ref, neighbor, dir, metrics, profile, voxelResolution, epsilon)
					if !ok {
						continue
					}
					seenKey := navDropLinkCandidateKey(candidate)
					if _, ok := seen[seenKey]; ok {
						continue
					}
					seen[seenKey] = struct{}{}
					out = append(out, candidate)
				}
			}
		}
	}
	return out
}

func navDropLinkCandidateForRefs(tile *NavTileDef, a, b navDropLinkCellRef, _ [2]int, metrics navBuildCellMetrics, profile NavAgentProfileDef, voxelResolution float32, epsilon float32) (navDropLinkCandidate, bool) {
	high, low, ok := navDropLinkOrderedRefs(a, b, metrics, profile, epsilon)
	if !ok || high.Coord != tile.Coord {
		return navDropLinkCandidate{}, false
	}
	fromPolygon, ok := navTilePolygonAtBuildCell(tile, high.Origin, metrics, high.Cell, voxelResolution)
	if !ok || fromPolygon.ID == "" {
		return navDropLinkCandidate{}, false
	}
	toPolygonID := low.Cell.PolygonID
	if low.Coord == tile.Coord {
		toPolygon, ok := navTilePolygonAtBuildCell(tile, low.Origin, metrics, low.Cell, voxelResolution)
		if !ok || toPolygon.ID == "" || toPolygon.ID == fromPolygon.ID {
			return navDropLinkCandidate{}, false
		}
		toPolygonID = toPolygon.ID
	}
	if toPolygonID == "" {
		return navDropLinkCandidate{}, false
	}
	highKey := navDropLinkCellGridKey(high, metrics)
	lowKey := navDropLinkCellGridKey(low, metrics)
	dir := [2]int{clampNavInt(lowKey[0]-highKey[0], -1, 1), clampNavInt(lowKey[1]-highKey[1], -1, 1)}
	if absNavIntAsInt(dir[0])+absNavIntAsInt(dir[1]) != 1 {
		return navDropLinkCandidate{}, false
	}
	return navDropLinkCandidate{
		FromPolygonID: fromPolygon.ID,
		ToTileCoord:   low.Coord,
		ToPolygonID:   toPolygonID,
		DirX:          dir[0],
		DirZ:          dir[1],
		Span:          navDropLinkSpanCoord(highKey, dir),
		Start:         navBuildCellCenterPoint(high.Origin, metrics, high.Cell),
		End:           navBuildCellCenterPoint(low.Origin, metrics, low.Cell),
	}, true
}

func navDropLinkCandidateKey(candidate navDropLinkCandidate) string {
	return fmt.Sprintf("%s|%d:%d:%d|%s|%d:%d|%.4f:%.4f:%.4f|%.4f:%.4f:%.4f",
		candidate.FromPolygonID,
		candidate.ToTileCoord.X, candidate.ToTileCoord.Y, candidate.ToTileCoord.Z,
		candidate.ToPolygonID,
		candidate.DirX, candidate.DirZ,
		candidate.Start[0], candidate.Start[1], candidate.Start[2],
		candidate.End[0], candidate.End[1], candidate.End[2])
}

func appendMergedNavTileDropLinks(tile *NavTileDef, candidates []navDropLinkCandidate, profile NavAgentProfileDef) {
	if tile == nil || len(candidates) == 0 {
		return
	}
	byKey := make(map[navDropLinkMergeKey][]navDropLinkCandidate)
	for _, candidate := range candidates {
		key := navDropLinkMergeKey{
			FromPolygonID: candidate.FromPolygonID,
			ToTileCoord:   candidate.ToTileCoord,
			ToPolygonID:   candidate.ToPolygonID,
			DirX:          candidate.DirX,
			DirZ:          candidate.DirZ,
		}
		byKey[key] = append(byKey[key], candidate)
	}
	keys := make([]navDropLinkMergeKey, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].FromPolygonID != keys[j].FromPolygonID {
			return keys[i].FromPolygonID < keys[j].FromPolygonID
		}
		if keys[i].ToTileCoord != keys[j].ToTileCoord {
			return terrainChunkCoordLess(keys[i].ToTileCoord, keys[j].ToTileCoord)
		}
		if keys[i].ToPolygonID != keys[j].ToPolygonID {
			return keys[i].ToPolygonID < keys[j].ToPolygonID
		}
		if keys[i].DirX != keys[j].DirX {
			return keys[i].DirX < keys[j].DirX
		}
		return keys[i].DirZ < keys[j].DirZ
	})
	for _, key := range keys {
		group := byKey[key]
		sort.Slice(group, func(i, j int) bool {
			return group[i].Span < group[j].Span
		})
		start := 0
		for start < len(group) {
			end := start + 1
			for end < len(group) && group[end].Span == group[end-1].Span+1 {
				end++
			}
			startPoint, endPoint := navMergedDropLinkEndpoints(group[start:end])
			tile.OffMeshLinks = append(tile.OffMeshLinks, NavOffMeshLinkDef{
				ID:            fmt.Sprintf("drop:%s:%d:%d:%d:%s:%d:%d:%d", key.FromPolygonID, key.ToTileCoord.X, key.ToTileCoord.Y, key.ToTileCoord.Z, key.ToPolygonID, key.DirX, key.DirZ, len(tile.OffMeshLinks)),
				Kind:          NavTraversalDrop,
				FromPolygonID: key.FromPolygonID,
				ToTileCoord:   key.ToTileCoord,
				ToPolygonID:   key.ToPolygonID,
				Start:         startPoint,
				End:           endPoint,
				Radius:        profile.Radius,
				Cost:          navVec3Distance(startPoint, endPoint),
				Bidirectional: false,
				Tags:          []string{"generated"},
			})
			start = end
		}
	}
	sort.Slice(tile.OffMeshLinks, func(i, j int) bool {
		return tile.OffMeshLinks[i].ID < tile.OffMeshLinks[j].ID
	})
}

func navMergedDropLinkEndpoints(group []navDropLinkCandidate) (Vec3, Vec3) {
	var start Vec3
	var end Vec3
	if len(group) == 0 {
		return start, end
	}
	for _, candidate := range group {
		for axis := 0; axis < 3; axis++ {
			start[axis] += candidate.Start[axis]
			end[axis] += candidate.End[axis]
		}
	}
	count := float32(len(group))
	for axis := 0; axis < 3; axis++ {
		start[axis] /= count
		end[axis] /= count
	}
	return start, end
}

func navDropLinkSpanCoord(key [2]int, dir [2]int) int {
	if dir[0] != 0 {
		return key[1]
	}
	return key[0]
}

func navDropLinkCellGridKey(ref navDropLinkCellRef, metrics navBuildCellMetrics) [2]int {
	minX := navRegionGridToWorldX(ref.Origin, metrics, float32(ref.Cell.X))
	minZ := navRegionGridToWorldZ(ref.Origin, metrics, float32(ref.Cell.Z))
	return [2]int{
		int(math.Round(float64(minX / metrics.Horizontal))),
		int(math.Round(float64(minZ / metrics.Horizontal))),
	}
}

func navDropLinkOrderedCells(a, b NavBuildWalkableCell, metrics navBuildCellMetrics, profile NavAgentProfileDef, epsilon float32) (NavBuildWalkableCell, NavBuildWalkableCell, bool) {
	if a.Y == b.Y {
		return NavBuildWalkableCell{}, NavBuildWalkableCell{}, false
	}
	high := a
	low := b
	if b.Y > a.Y {
		high = b
		low = a
	}
	dropHeight := float32(high.Y-low.Y) * metrics.Vertical
	if dropHeight <= profile.StepHeight+epsilon || dropHeight > profile.MaxDropHeight+epsilon {
		return NavBuildWalkableCell{}, NavBuildWalkableCell{}, false
	}
	return high, low, true
}

func navDropLinkOrderedRefs(a, b navDropLinkCellRef, metrics navBuildCellMetrics, profile NavAgentProfileDef, epsilon float32) (navDropLinkCellRef, navDropLinkCellRef, bool) {
	highCell, lowCell, ok := navDropLinkOrderedCells(a.Cell, b.Cell, metrics, profile, epsilon)
	if !ok {
		return navDropLinkCellRef{}, navDropLinkCellRef{}, false
	}
	high := a
	low := b
	if highCell.X == b.Cell.X && highCell.Y == b.Cell.Y && highCell.Z == b.Cell.Z {
		high = b
		low = a
	}
	high.Cell = highCell
	low.Cell = lowCell
	return high, low, true
}

func navBuildCellCenterPoint(origin [3]float32, metrics navBuildCellMetrics, cell NavBuildWalkableCell) Vec3 {
	return Vec3{
		navRegionGridToWorldX(origin, metrics, float32(cell.X)+0.5),
		origin[1] + float32(cell.Y)*metrics.Vertical,
		navRegionGridToWorldZ(origin, metrics, float32(cell.Z)+0.5),
	}
}

func navTilePolygonAtBuildCell(tile *NavTileDef, origin [3]float32, metrics navBuildCellMetrics, cell NavBuildWalkableCell, voxelResolution float32) (NavPolygonDef, bool) {
	if tile == nil || metrics.Horizontal <= 0 || metrics.Vertical <= 0 {
		return NavPolygonDef{}, false
	}
	minX := navRegionGridToWorldX(origin, metrics, float32(cell.X))
	maxX := navRegionGridToWorldX(origin, metrics, float32(cell.X+1))
	minZ := navRegionGridToWorldZ(origin, metrics, float32(cell.Z))
	maxZ := navRegionGridToWorldZ(origin, metrics, float32(cell.Z+1))
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	if minZ > maxZ {
		minZ, maxZ = maxZ, minZ
	}
	point := Vec3{
		(minX + maxX) * 0.5,
		origin[1] + float32(cell.Y)*metrics.Vertical,
		(minZ + maxZ) * 0.5,
	}
	if voxelResolution > 0 && !metrics.WorldAligned && metrics.Horizontal == voxelResolution {
		point[0] = origin[0] + (float32(cell.X)+0.5)*voxelResolution
		point[2] = origin[2] + (float32(cell.Z)+0.5)*voxelResolution
	}
	const epsilon = float32(1e-4)
	for _, polygon := range tile.Polygons {
		if !navPolygonContainsXZ(tile, polygon, point) {
			continue
		}
		height, ok := navPolygonHeightAtXZ(tile, polygon, point)
		if !ok {
			height = navPolygonCenter(tile, polygon)[1]
		}
		if absNavFloat32(height-point[1]) <= epsilon {
			return polygon, true
		}
	}
	return NavPolygonDef{}, false
}

func navRegionVertex(tile *NavTileDef, origin [3]float32, metrics navBuildCellMetrics, region navBuildWalkableRegion, gridX, gridZ float32, stitcher *navBuildBorderStitcher) Vec3 {
	worldX := navRegionGridToWorldX(origin, metrics, gridX)
	worldZ := navRegionGridToWorldZ(origin, metrics, gridZ)
	if tile != nil {
		worldX = clampNavFloat32(worldX, tile.BoundsMin[0], tile.BoundsMax[0])
		worldZ = clampNavFloat32(worldZ, tile.BoundsMin[2], tile.BoundsMax[2])
	}
	heightGridX := gridX
	heightGridZ := gridZ
	if metrics.Horizontal > 0 {
		heightGridX = navWorldToRegionGridX(origin, metrics, worldX)
		heightGridZ = navWorldToRegionGridZ(origin, metrics, worldZ)
	}
	height := origin[1] + navRegionHeightAtGrid(region, heightGridX, heightGridZ)*metrics.Vertical
	if stitchedHeight, ok := stitcher.StitchedHeight(tile, worldX, worldZ, height); ok {
		height = stitchedHeight
	}
	if tile != nil {
		height = clampNavFloat32(height, tile.BoundsMin[1], tile.BoundsMax[1])
	}
	return Vec3{
		worldX,
		height,
		worldZ,
	}
}

func navRegionGridToWorldX(origin [3]float32, metrics navBuildCellMetrics, gridX float32) float32 {
	if metrics.WorldAligned {
		return metrics.OriginX + gridX*metrics.Horizontal
	}
	return origin[0] + gridX*metrics.Horizontal
}

func navRegionGridToWorldZ(origin [3]float32, metrics navBuildCellMetrics, gridZ float32) float32 {
	if metrics.WorldAligned {
		return metrics.OriginZ + gridZ*metrics.Horizontal
	}
	return origin[2] + gridZ*metrics.Horizontal
}

func navWorldToRegionGridX(origin [3]float32, metrics navBuildCellMetrics, worldX float32) float32 {
	if metrics.Horizontal <= 0 {
		return 0
	}
	if metrics.WorldAligned {
		return (worldX - metrics.OriginX) / metrics.Horizontal
	}
	return (worldX - origin[0]) / metrics.Horizontal
}

func navWorldToRegionGridZ(origin [3]float32, metrics navBuildCellMetrics, worldZ float32) float32 {
	if metrics.Horizontal <= 0 {
		return 0
	}
	if metrics.WorldAligned {
		return (worldZ - metrics.OriginZ) / metrics.Horizontal
	}
	return (worldZ - origin[2]) / metrics.Horizontal
}

func appendNavBuildSourcePolygons(tile *NavTileDef, source *NavBuildSourceDef, profile NavAgentProfileDef) {
	appendNavBuildSourcePolygonsSkipping(tile, source, profile, nil)
}

func appendNavBuildSourcePolygonsSkipping(tile *NavTileDef, source *NavBuildSourceDef, profile NavAgentProfileDef, skipSurfaces map[int]struct{}) {
	if tile == nil || source == nil {
		return
	}
	EnsureNavBuildSourceDefaults(source)
	startPolygon := len(tile.Polygons)
	blockers := navBuildSourceClearanceBlockerRects(source, profile)
	for surfaceIndex := range source.Surfaces {
		if _, skip := skipSurfaces[surfaceIndex]; skip {
			continue
		}
		surface := source.Surfaces[surfaceIndex]
		EnsureNavBuildSurfaceDefaults(&surface)
		if surface.Kind != NavBuildSurfaceWalkable {
			continue
		}
		if len(surface.Indices) >= 3 && len(surface.Indices)%3 == 0 {
			for tri := 0; tri < len(surface.Indices); tri += 3 {
				a := surface.Indices[tri]
				b := surface.Indices[tri+1]
				c := surface.Indices[tri+2]
				if a < 0 || b < 0 || c < 0 || a >= len(surface.Vertices) || b >= len(surface.Vertices) || c >= len(surface.Vertices) {
					continue
				}
				vertices := []Vec3{surface.Vertices[a], surface.Vertices[b], surface.Vertices[c]}
				if !navBuildSurfacePolygonOverlapsTile(vertices, tile.BoundsMin, tile.BoundsMax) {
					continue
				}
				if !navBuildSurfacePolygonSlopeWalkable(vertices, surface.Normal, profile) {
					continue
				}
				clipped := navClipNavBuildSourcePolygonToTile(vertices, tile.BoundsMin, tile.BoundsMax)
				if len(clipped) < 3 {
					continue
				}
				appendNavBuildSourcePolygonPieces(tile, source, surface, surfaceIndex, tri/3, clipped, profile, blockers)
			}
			continue
		}
		if len(surface.Vertices) < 3 {
			continue
		}
		if !navBuildSurfacePolygonOverlapsTile(surface.Vertices, tile.BoundsMin, tile.BoundsMax) {
			continue
		}
		if !navBuildSurfacePolygonSlopeWalkable(surface.Vertices, surface.Normal, profile) {
			continue
		}
		clipped := navClipNavBuildSourcePolygonToTile(surface.Vertices, tile.BoundsMin, tile.BoundsMax)
		if len(clipped) < 3 {
			continue
		}
		appendNavBuildSourcePolygonPieces(tile, source, surface, surfaceIndex, -1, clipped, profile, blockers)
	}
	if len(tile.Polygons) > startPolygon {
		mergeNavBuildSourceRectPolygons(tile, startPolygon)
		applyNavSurfaceNeighbors(tile, startPolygon, profile)
	}
}

func appendNavBuildSourcePrimaryPolygons(tile *NavTileDef, source *NavBuildSourceDef, profile NavAgentProfileDef, chunk *ImportedWorldChunkDef, neighbors map[TerrainChunkCoordDef]*ImportedWorldChunkDef, cache *NavTileBuildCache) {
	if tile == nil || source == nil {
		return
	}
	cellSize := float32(0)
	if chunk != nil {
		cellSize = navBuildSourceRasterCellSize(profile, chunk.VoxelResolution)
	}
	rasterized := map[int]struct{}{}
	if cellSize > 0 {
		data := navTileBuildChunkData(cache, chunk)
		sampler := newImportedWorldNavOccupancySampler(cache, chunk, data, neighbors, profile)
		addNavBuildSourceClearanceBlockersToSampler(&sampler, source, chunk, profile)
		rasterized = appendNavBuildSourceRasterPolygons(tile, source, profile, chunk.VoxelResolution, chunk, sampler)
	}
	appendNavBuildSourcePolygonsSkipping(tile, source, profile, rasterized)
}

func navClipNavBuildSourcePolygonToTile(vertices []Vec3, boundsMin [3]float32, boundsMax [3]float32) []Vec3 {
	if len(vertices) < 3 {
		return nil
	}
	clipped := navClipPolygonXZ(vertices, boundsMin[0], boundsMax[0], boundsMin[2], boundsMax[2])
	const epsilon = float32(1e-5)
	if len(clipped) < 3 || navPolygonAreaXZAbs(clipped) <= epsilon {
		return nil
	}
	lifted, ok := navLiftRasterCellPolygon(vertices, clipped)
	if !ok {
		return nil
	}
	return navClipNavBuildSourceLiftedPolygonToTileY(lifted, boundsMin, boundsMax)
}

func navClipNavBuildSourceLiftedPolygonToTileY(vertices []Vec3, boundsMin [3]float32, boundsMax [3]float32) []Vec3 {
	if len(vertices) < 3 {
		return nil
	}
	if navBuildSourceFlatPolygonOnTileMaxY(vertices, boundsMax[1]) {
		return nil
	}
	clipped := navClipPolygonY(vertices, boundsMin[1], boundsMax[1])
	clipped = navCleanClippedPolygonXZ(clipped)
	const epsilon = float32(1e-5)
	if len(clipped) < 3 || navPolygonAreaXZAbs(clipped) <= epsilon {
		return nil
	}
	return clipped
}

func navBuildSourceFlatPolygonOnTileMaxY(vertices []Vec3, maxY float32) bool {
	const epsilon = float32(1e-4)
	for _, vertex := range vertices {
		if absNavFloat32(vertex[1]-maxY) > epsilon {
			return false
		}
	}
	return true
}

func appendNavBuildSourcePolygonPieces(tile *NavTileDef, source *NavBuildSourceDef, surface NavBuildSurfaceDef, surfaceIndex int, triangleIndex int, vertices []Vec3, profile NavAgentProfileDef, blockers []navBuildSourceBlockerRect) {
	pieces := navBuildSourcePolygonPiecesAroundBlockers(source, surface, vertices, profile, blockers)
	for i, piece := range pieces {
		pieceSurface := surface
		if len(pieces) > 1 {
			pieceSurface.ID = navBuildSourceClippedSurfaceID(surface, surfaceIndex, triangleIndex, i)
		}
		appendNavBuildSourcePolygon(tile, pieceSurface, surfaceIndex, triangleIndex, piece, profile)
	}
}

func navBuildSourceClippedSurfaceID(surface NavBuildSurfaceDef, surfaceIndex int, triangleIndex int, pieceIndex int) string {
	id := strings.TrimSpace(surface.ID)
	if id == "" {
		id = "surface_" + itoa(surfaceIndex)
		if triangleIndex >= 0 {
			id += "_tri_" + itoa(triangleIndex)
		}
	}
	return id + "_clip_" + itoa(pieceIndex)
}

func navBuildSourcePolygonPiecesAroundBlockers(source *NavBuildSourceDef, surface NavBuildSurfaceDef, vertices []Vec3, profile NavAgentProfileDef, blockers []navBuildSourceBlockerRect) [][]Vec3 {
	if len(vertices) < 3 || len(blockers) == 0 || !navBuildSourceSurfaceUsesBlockerClipping(source, surface) {
		return [][]Vec3{vertices}
	}
	minX, maxX, minZ, maxZ := navBuildPolygonBoundsXZ(vertices)
	relevant := make([]navBuildSourceBlockerRect, 0)
	xs := []float32{minX, maxX}
	zs := []float32{minZ, maxZ}
	for _, blocker := range blockers {
		if blocker.MaxX <= minX || blocker.MinX >= maxX || blocker.MaxZ <= minZ || blocker.MinZ >= maxZ {
			continue
		}
		if !navBuildSourceBlockerTouchesWalkableSurface(blocker, vertices, profile) {
			continue
		}
		blocker.MinX = maxNavFloat32(blocker.MinX, minX)
		blocker.MaxX = minNavFloat32(blocker.MaxX, maxX)
		blocker.MinZ = maxNavFloat32(blocker.MinZ, minZ)
		blocker.MaxZ = minNavFloat32(blocker.MaxZ, maxZ)
		const epsilon = float32(1e-4)
		if blocker.MaxX-blocker.MinX <= epsilon || blocker.MaxZ-blocker.MinZ <= epsilon {
			continue
		}
		relevant = append(relevant, blocker)
		xs = append(xs, blocker.MinX, blocker.MaxX)
		zs = append(zs, blocker.MinZ, blocker.MaxZ)
	}
	if len(relevant) == 0 {
		return [][]Vec3{vertices}
	}
	xs = navBuildSortedUniqueFloat32(xs, 1e-4)
	zs = navBuildSortedUniqueFloat32(zs, 1e-4)
	if len(xs) < 2 || len(zs) < 2 {
		return [][]Vec3{vertices}
	}
	pieces := make([][]Vec3, 0)
	const epsilon = float32(1e-5)
	for xi := 0; xi+1 < len(xs); xi++ {
		cellMinX := xs[xi]
		cellMaxX := xs[xi+1]
		if cellMaxX-cellMinX <= epsilon {
			continue
		}
		for zi := 0; zi+1 < len(zs); zi++ {
			cellMinZ := zs[zi]
			cellMaxZ := zs[zi+1]
			if cellMaxZ-cellMinZ <= epsilon {
				continue
			}
			center := Vec3{(cellMinX + cellMaxX) * 0.5, 0, (cellMinZ + cellMaxZ) * 0.5}
			if !navBuildSourceVerticesContainXZ(vertices, center) || navBuildSourcePointInsideAnyBlockerRect(center, relevant) {
				continue
			}
			clipped := navClipPolygonXZ(vertices, cellMinX, cellMaxX, cellMinZ, cellMaxZ)
			if len(clipped) < 3 || navPolygonAreaXZAbs(clipped) <= epsilon {
				continue
			}
			lifted, ok := navLiftRasterCellPolygon(vertices, clipped)
			if !ok || len(lifted) < 3 || navPolygonAreaXZAbs(lifted) <= epsilon {
				continue
			}
			pieces = append(pieces, lifted)
		}
	}
	if len(pieces) == 0 {
		return nil
	}
	return pieces
}

func navBuildSourceSurfaceUsesBlockerClipping(source *NavBuildSourceDef, surface NavBuildSurfaceDef) bool {
	if strings.HasPrefix(surface.SourceTag, "hl1:") || navBuildSourceSurfaceHasTag(surface, "source:hl1") {
		return true
	}
	if source == nil {
		return false
	}
	for _, tag := range source.Tags {
		if tag == "source:hl1" {
			return true
		}
	}
	return false
}

func navBuildSourceClearanceBlockerRects(source *NavBuildSourceDef, profile NavAgentProfileDef) []navBuildSourceBlockerRect {
	if source == nil {
		return nil
	}
	EnsureNavAgentProfileDefaults(&profile)
	expand := profile.Radius
	if expand <= 0 {
		return nil
	}
	rects := make([]navBuildSourceBlockerRect, 0)
	for i := range source.Surfaces {
		surface := source.Surfaces[i]
		EnsureNavBuildSurfaceDefaults(&surface)
		if surface.Kind != NavBuildSurfaceClearanceBlocker {
			continue
		}
		if len(surface.Indices) >= 3 && len(surface.Indices)%3 == 0 {
			for tri := 0; tri < len(surface.Indices); tri += 3 {
				a := surface.Indices[tri]
				b := surface.Indices[tri+1]
				c := surface.Indices[tri+2]
				if a < 0 || b < 0 || c < 0 || a >= len(surface.Vertices) || b >= len(surface.Vertices) || c >= len(surface.Vertices) {
					continue
				}
				if rect, ok := navBuildSourceClearanceBlockerRectFromVertices([]Vec3{surface.Vertices[a], surface.Vertices[b], surface.Vertices[c]}, expand); ok {
					rects = append(rects, rect)
				}
			}
			continue
		}
		if rect, ok := navBuildSourceClearanceBlockerRectFromVertices(surface.Vertices, expand); ok {
			rects = append(rects, rect)
		}
	}
	return rects
}

func navBuildSourceClearanceBlockerRectFromVertices(vertices []Vec3, expand float32) (navBuildSourceBlockerRect, bool) {
	if len(vertices) < 3 {
		return navBuildSourceBlockerRect{}, false
	}
	minX, maxX := vertices[0][0], vertices[0][0]
	minY, maxY := vertices[0][1], vertices[0][1]
	minZ, maxZ := vertices[0][2], vertices[0][2]
	for _, vertex := range vertices[1:] {
		minX = minNavFloat32(minX, vertex[0])
		maxX = maxNavFloat32(maxX, vertex[0])
		minY = minNavFloat32(minY, vertex[1])
		maxY = maxNavFloat32(maxY, vertex[1])
		minZ = minNavFloat32(minZ, vertex[2])
		maxZ = maxNavFloat32(maxZ, vertex[2])
	}
	const epsilon = float32(1e-5)
	if maxX-minX <= epsilon && maxZ-minZ <= epsilon {
		return navBuildSourceBlockerRect{}, false
	}
	return navBuildSourceBlockerRect{
		MinX: minX - expand,
		MaxX: maxX + expand,
		MinY: minY,
		MaxY: maxY,
		MinZ: minZ - expand,
		MaxZ: maxZ + expand,
	}, true
}

func navBuildSourceBlockerTouchesWalkableSurface(blocker navBuildSourceBlockerRect, vertices []Vec3, profile NavAgentProfileDef) bool {
	center := Vec3{(blocker.MinX + blocker.MaxX) * 0.5, 0, (blocker.MinZ + blocker.MaxZ) * 0.5}
	y, ok := navBuildSurfaceHeightAtXZ(vertices, center)
	if !ok {
		return false
	}
	verticalPadding := maxNavFloat32(profile.StepHeight, 0.2)
	return y >= blocker.MinY-verticalPadding && y <= blocker.MaxY+verticalPadding
}

func navBuildSourcePointInsideAnyBlockerRect(point Vec3, blockers []navBuildSourceBlockerRect) bool {
	for _, blocker := range blockers {
		if point[0] >= blocker.MinX && point[0] <= blocker.MaxX && point[2] >= blocker.MinZ && point[2] <= blocker.MaxZ {
			return true
		}
	}
	return false
}

func navBuildSortedUniqueFloat32(values []float32, epsilon float32) []float32 {
	if len(values) == 0 {
		return nil
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	out := make([]float32, 0, len(values))
	for _, value := range values {
		if len(out) == 0 || absNavFloat32(value-out[len(out)-1]) > epsilon {
			out = append(out, value)
		}
	}
	return out
}

func appendNavBuildSourceRasterPolygons(tile *NavTileDef, source *NavBuildSourceDef, profile NavAgentProfileDef, voxelResolution float32, chunk *ImportedWorldChunkDef, sampler importedWorldNavOccupancySampler) map[int]struct{} {
	rasterized := map[int]struct{}{}
	cellSize := navBuildSourceRasterCellSize(profile, voxelResolution)
	if tile == nil || source == nil || cellSize <= 0 {
		return rasterized
	}
	cells := make([]navBuildSourceRasterCell, 0)
	for surfaceIndex := range source.Surfaces {
		surface := source.Surfaces[surfaceIndex]
		EnsureNavBuildSurfaceDefaults(&surface)
		if surface.Kind != NavBuildSurfaceWalkable {
			continue
		}
		forceRaster := navBuildSourceSurfaceUsesOccupancyClearance(source, surface)
		surfaceCells := make([]navBuildSourceRasterCell, 0)
		clearanceRejected := false
		if len(surface.Indices) >= 3 && len(surface.Indices)%3 == 0 {
			for tri := 0; tri < len(surface.Indices); tri += 3 {
				a := surface.Indices[tri]
				b := surface.Indices[tri+1]
				c := surface.Indices[tri+2]
				if a < 0 || b < 0 || c < 0 || a >= len(surface.Vertices) || b >= len(surface.Vertices) || c >= len(surface.Vertices) {
					continue
				}
				vertices := []Vec3{surface.Vertices[a], surface.Vertices[b], surface.Vertices[c]}
				polygonCells, rejected := buildNavBuildSourceRasterPolygonCells(tile, surface, surfaceIndex, tri/3, vertices, cellSize, profile, chunk, sampler)
				if len(polygonCells) > 0 {
					surfaceCells = append(surfaceCells, polygonCells...)
				}
				clearanceRejected = clearanceRejected || rejected
			}
			if forceRaster || (clearanceRejected && navBuildSourceSurfaceUsesSelectiveClearanceRaster(source, surface)) {
				cells = append(cells, surfaceCells...)
				rasterized[surfaceIndex] = struct{}{}
			}
			continue
		}
		if len(surface.Vertices) < 3 {
			continue
		}
		polygonCells, rejected := buildNavBuildSourceRasterPolygonCells(tile, surface, surfaceIndex, -1, surface.Vertices, cellSize, profile, chunk, sampler)
		if forceRaster || (rejected && navBuildSourceSurfaceUsesSelectiveClearanceRaster(source, surface)) {
			cells = append(cells, polygonCells...)
			rasterized[surfaceIndex] = struct{}{}
		}
	}
	appendNavBuildSourceRasterCells(tile, cells, profile)
	return rasterized
}

func navBuildSourceHasOccupancyClearanceSurfaces(source *NavBuildSourceDef) bool {
	if source == nil {
		return false
	}
	EnsureNavBuildSourceDefaults(source)
	for _, surface := range source.Surfaces {
		EnsureNavBuildSurfaceDefaults(&surface)
		if surface.Kind == NavBuildSurfaceWalkable && navBuildSourceSurfaceUsesOccupancyClearance(source, surface) {
			return true
		}
	}
	return false
}

func navBuildSourceSurfaceUsesOccupancyClearance(source *NavBuildSourceDef, surface NavBuildSurfaceDef) bool {
	_ = source
	if strings.HasPrefix(surface.SourceTag, "hl1:") || navBuildSourceSurfaceHasTag(surface, "source:hl1") {
		return false
	}
	if strings.HasPrefix(surface.SourceTag, "imported_world:") {
		return true
	}
	for _, tag := range surface.Tags {
		if tag == "source:imported_world" {
			return true
		}
	}
	return false
}

func navBuildSourceSurfaceUsesSelectiveClearanceRaster(source *NavBuildSourceDef, surface NavBuildSurfaceDef) bool {
	_ = source
	_ = surface
	return false
}

func navBuildSourceSurfaceHasTag(surface NavBuildSurfaceDef, want string) bool {
	for _, tag := range surface.Tags {
		if tag == want {
			return true
		}
	}
	return false
}

func navBuildSourceRasterCellSize(profile NavAgentProfileDef, voxelResolution float32) float32 {
	const epsilon = float32(1e-4)
	if profile.NavCellSize <= 0 || voxelResolution <= 0 {
		return 0
	}
	if profile.NavCellSize <= voxelResolution+epsilon {
		return 0
	}
	return profile.NavCellSize
}

func buildNavBuildSourceRasterPolygonCells(tile *NavTileDef, surface NavBuildSurfaceDef, surfaceIndex int, triangleIndex int, vertices []Vec3, cellSize float32, profile NavAgentProfileDef, chunk *ImportedWorldChunkDef, sampler importedWorldNavOccupancySampler) ([]navBuildSourceRasterCell, bool) {
	if tile == nil || len(vertices) < 3 || cellSize <= 0 {
		return nil, false
	}
	if !navBuildSurfacePolygonOverlapsTile(vertices, tile.BoundsMin, tile.BoundsMax) {
		return nil, false
	}
	if !navBuildSurfacePolygonSlopeWalkable(vertices, surface.Normal, profile) {
		return nil, false
	}
	plane, ok := navBuildSourceRasterPlaneFromSurface(vertices)
	if !ok {
		return nil, false
	}
	minX, maxX, minZ, maxZ := navBuildPolygonBoundsXZ(vertices)
	minX = maxNavFloat32(minX, tile.BoundsMin[0])
	maxX = minNavFloat32(maxX, tile.BoundsMax[0])
	minZ = maxNavFloat32(minZ, tile.BoundsMin[2])
	maxZ = minNavFloat32(maxZ, tile.BoundsMax[2])
	const epsilon = float32(1e-5)
	if maxX-minX <= epsilon || maxZ-minZ <= epsilon {
		return nil, false
	}
	startX := int(math.Floor(float64(minX / cellSize)))
	endX := int(math.Ceil(float64(maxX/cellSize))) - 1
	startZ := int(math.Floor(float64(minZ / cellSize)))
	endZ := int(math.Ceil(float64(maxZ/cellSize))) - 1
	cells := make([]navBuildSourceRasterCell, 0)
	mergeKey := navBuildSourceRasterMergeKey(surface, plane)
	clearanceRejected := false
	for x := startX; x <= endX; x++ {
		cellMinX := maxNavFloat32(maxNavFloat32(float32(x)*cellSize, minX), tile.BoundsMin[0])
		cellMaxX := minNavFloat32(minNavFloat32(float32(x+1)*cellSize, maxX), tile.BoundsMax[0])
		if cellMaxX-cellMinX <= epsilon {
			continue
		}
		for z := startZ; z <= endZ; z++ {
			cellMinZ := maxNavFloat32(maxNavFloat32(float32(z)*cellSize, minZ), tile.BoundsMin[2])
			cellMaxZ := minNavFloat32(minNavFloat32(float32(z+1)*cellSize, maxZ), tile.BoundsMax[2])
			if cellMaxZ-cellMinZ <= epsilon {
				continue
			}
			clipped := navClipPolygonXZ(vertices, cellMinX, cellMaxX, cellMinZ, cellMaxZ)
			if len(clipped) < 3 || navPolygonAreaXZAbs(clipped) <= epsilon {
				continue
			}
			lifted, ok := navLiftRasterCellPolygon(vertices, clipped)
			if !ok {
				continue
			}
			lifted = navClipNavBuildSourceLiftedPolygonToTileY(lifted, tile.BoundsMin, tile.BoundsMax)
			if len(lifted) < 3 || navPolygonAreaXZAbs(lifted) <= epsilon {
				continue
			}
			cellArea := (cellMaxX - cellMinX) * (cellMaxZ - cellMinZ)
			full := navPolygonAreaXZAbs(lifted) >= cellArea-epsilon
			cell := navBuildSourceRasterCell{
				X:             x,
				Z:             z,
				MinX:          cellMinX,
				MaxX:          cellMaxX,
				MinZ:          cellMinZ,
				MaxZ:          cellMaxZ,
				Vertices:      lifted,
				Full:          full,
				MergeKey:      mergeKey,
				Plane:         plane,
				Surface:       surface,
				SurfaceIndex:  surfaceIndex,
				TriangleIndex: triangleIndex,
			}
			if !navBuildSourceRasterCellHasAgentClearance(tile, cell, chunk, profile, sampler) {
				clearanceRejected = true
				continue
			}
			cells = append(cells, cell)
		}
	}
	return cells, clearanceRejected
}

func navBuildSourceRasterCellHasAgentClearance(tile *NavTileDef, cell navBuildSourceRasterCell, chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, sampler importedWorldNavOccupancySampler) bool {
	if tile == nil || chunk == nil || chunk.VoxelResolution <= 0 || len(cell.Vertices) < 3 {
		return true
	}
	origin := importedWorldChunkWorldOrigin(chunk)
	resolution := chunk.VoxelResolution
	minX := int(math.Floor(float64((cell.MinX - origin[0]) / resolution)))
	maxX := int(math.Ceil(float64((cell.MaxX-origin[0])/resolution))) - 1
	minZ := int(math.Floor(float64((cell.MinZ - origin[2]) / resolution)))
	maxZ := int(math.Ceil(float64((cell.MaxZ-origin[2])/resolution))) - 1
	for x := minX; x <= maxX; x++ {
		worldX := origin[0] + (float32(x)+0.5)*resolution
		for z := minZ; z <= maxZ; z++ {
			worldZ := origin[2] + (float32(z)+0.5)*resolution
			point := Vec3{worldX, 0, worldZ}
			if !navBuildSourceVerticesContainXZ(cell.Vertices, point) {
				continue
			}
			height := navBuildSourceRasterPlaneHeight(cell.Plane, worldX, worldZ)
			floorY := int(math.Floor(float64((height-origin[1])/resolution) + 1e-4))
			if !navCellHasAgentClearance(chunk, profile, sampler, x, floorY, z) {
				return false
			}
		}
	}
	return true
}

func navBuildSourceVerticesContainXZ(vertices []Vec3, point Vec3) bool {
	if len(vertices) < 3 {
		return false
	}
	inside := false
	j := len(vertices) - 1
	const epsilon = float32(1e-5)
	for i := range vertices {
		a := vertices[i]
		b := vertices[j]
		if navPointOnSegmentXZ(point, a, b, epsilon) {
			return true
		}
		intersects := (a[2] > point[2]) != (b[2] > point[2])
		if intersects {
			x := (b[0]-a[0])*(point[2]-a[2])/(b[2]-a[2]) + a[0]
			if point[0] < x {
				inside = !inside
			}
		}
		j = i
	}
	return inside
}

func appendNavBuildSourceRasterCells(tile *NavTileDef, cells []navBuildSourceRasterCell, profile NavAgentProfileDef) {
	if tile == nil || len(cells) == 0 {
		return
	}
	cells = dedupeNavBuildSourceRasterCells(cells)
	regions, passthrough := mergeNavBuildSourceRasterCells(cells)
	for _, region := range regions {
		appendNavBuildSourceRasterRegionPolygon(tile, region, profile)
	}
	for _, cell := range passthrough {
		appendNavBuildSourceRasterCellPolygon(tile, cell, profile)
	}
}

func dedupeNavBuildSourceRasterCells(cells []navBuildSourceRasterCell) []navBuildSourceRasterCell {
	if len(cells) < 2 {
		return cells
	}
	out := make([]navBuildSourceRasterCell, 0, len(cells))
	byKey := make(map[string]int, len(cells))
	for _, cell := range cells {
		key := navBuildSourceRasterCellDedupeKey(cell)
		if existingIndex, ok := byKey[key]; ok {
			if navPolygonAreaXZAbs(cell.Vertices) > navPolygonAreaXZAbs(out[existingIndex].Vertices)+1e-5 {
				out[existingIndex] = cell
			}
			continue
		}
		byKey[key] = len(out)
		out = append(out, cell)
	}
	return out
}

func navBuildSourceRasterCellDedupeKey(cell navBuildSourceRasterCell) string {
	return cell.MergeKey + "|" +
		navBuildFloatKey(cell.MinX) + "|" +
		navBuildFloatKey(cell.MaxX) + "|" +
		navBuildFloatKey(cell.MinZ) + "|" +
		navBuildFloatKey(cell.MaxZ)
}

func navLiftRasterCellPolygon(sourceVertices []Vec3, clipped []Vec3) ([]Vec3, bool) {
	if len(clipped) < 3 {
		return nil, false
	}
	lifted := make([]Vec3, 0, len(clipped))
	for _, vertex := range clipped {
		y, ok := navBuildSurfaceHeightAtXZ(sourceVertices, vertex)
		if !ok {
			return nil, false
		}
		lifted = append(lifted, Vec3{vertex[0], y, vertex[2]})
	}
	return lifted, true
}

func appendNavBuildSourceRasterCellPolygon(tile *NavTileDef, cell navBuildSourceRasterCell, profile NavAgentProfileDef) {
	if tile == nil || len(cell.Vertices) < 3 {
		return
	}
	area := navBuildSourceSurfaceTraversalArea(cell.Surface.Area, cell.Vertices, cell.Surface.Normal, profile)
	appendNavPolygonVertices(tile, navBuildSourceRasterPolygonID(cell.Surface, cell.SurfaceIndex, cell.TriangleIndex, cell.X, cell.Z), cell.Vertices, area, cell.Surface.Flags)
}

func navBuildSourceRasterPolygonID(surface NavBuildSurfaceDef, surfaceIndex int, triangleIndex int, x int, z int) string {
	return "raster_cell:" + navBuildSurfacePolygonID(surface, surfaceIndex, triangleIndex) + ":" + itoa(x) + ":" + itoa(z)
}

func mergeNavBuildSourceRasterCells(cells []navBuildSourceRasterCell) ([]navBuildSourceRasterRegion, []navBuildSourceRasterCell) {
	if len(cells) == 0 {
		return nil, nil
	}
	full := make([]navBuildSourceRasterCell, 0, len(cells))
	passthrough := make([]navBuildSourceRasterCell, 0)
	for _, cell := range cells {
		if cell.Full && len(cell.Vertices) == 4 {
			full = append(full, cell)
			continue
		}
		passthrough = append(passthrough, cell)
	}
	sort.Slice(full, func(i, j int) bool {
		if full[i].Z != full[j].Z {
			return full[i].Z < full[j].Z
		}
		if full[i].X != full[j].X {
			return full[i].X < full[j].X
		}
		return full[i].MergeKey < full[j].MergeKey
	})
	cellsByCoord := make(map[[2]int][]int, len(full))
	for i, cell := range full {
		key := [2]int{cell.X, cell.Z}
		cellsByCoord[key] = append(cellsByCoord[key], i)
	}
	assigned := make([]bool, len(full))
	regions := make([]navBuildSourceRasterRegion, 0)
	for i := range full {
		if assigned[i] {
			continue
		}
		region := growNavBuildSourceRasterRegion(full, cellsByCoord, assigned, i)
		for _, index := range region.Indices {
			assigned[index] = true
		}
		regions = append(regions, region)
	}
	return regions, passthrough
}

func growNavBuildSourceRasterRegion(cells []navBuildSourceRasterCell, cellsByCoord map[[2]int][]int, assigned []bool, startIndex int) navBuildSourceRasterRegion {
	start := cells[startIndex]
	row := []int{startIndex}
	for nextX := start.X + 1; ; nextX++ {
		prev := cells[row[len(row)-1]]
		nextIndex, ok := findNavBuildSourceRasterCellIndex(cells, cellsByCoord, assigned, nextX, start.Z, start.MergeKey, true)
		if !ok || !navRasterCellsTouchX(prev, cells[nextIndex]) {
			break
		}
		row = append(row, nextIndex)
	}
	rows := [][]int{row}
	for nextZ := start.Z + 1; ; nextZ++ {
		nextRow := make([]int, 0, len(row))
		complete := true
		for xOffset, rowCellIndex := range row {
			above := cells[rows[len(rows)-1][xOffset]]
			template := cells[rowCellIndex]
			nextIndex, ok := findNavBuildSourceRasterCellIndex(cells, cellsByCoord, assigned, template.X, nextZ, start.MergeKey, true)
			if !ok || !navRasterCellsTouchZ(above, cells[nextIndex]) || !navRasterCellsSameColumnBounds(template, cells[nextIndex]) {
				complete = false
				break
			}
			nextRow = append(nextRow, nextIndex)
		}
		if !complete {
			break
		}
		rows = append(rows, nextRow)
	}
	region := navBuildSourceRasterRegion{
		MinX:          start.X,
		MaxX:          start.X + len(row) - 1,
		MinZ:          start.Z,
		MaxZ:          start.Z + len(rows) - 1,
		BoundsMinX:    start.MinX,
		BoundsMaxX:    cells[row[len(row)-1]].MaxX,
		BoundsMinZ:    start.MinZ,
		BoundsMaxZ:    cells[rows[len(rows)-1][0]].MaxZ,
		Plane:         start.Plane,
		MergeKey:      start.MergeKey,
		Surface:       start.Surface,
		SurfaceIndex:  start.SurfaceIndex,
		TriangleIndex: start.TriangleIndex,
		Cells:         make([]navBuildSourceRasterCell, 0, len(row)*len(rows)),
		Indices:       make([]int, 0, len(row)*len(rows)),
	}
	for _, row := range rows {
		for _, index := range row {
			region.Cells = append(region.Cells, cells[index])
			region.Indices = append(region.Indices, index)
		}
	}
	return region
}

func findNavBuildSourceRasterCellIndex(cells []navBuildSourceRasterCell, cellsByCoord map[[2]int][]int, assigned []bool, x int, z int, mergeKey string, requireUnassigned bool) (int, bool) {
	for _, index := range cellsByCoord[[2]int{x, z}] {
		if requireUnassigned && assigned[index] {
			continue
		}
		if cells[index].MergeKey == mergeKey {
			return index, true
		}
	}
	return -1, false
}

func navRasterCellsTouchX(a, b navBuildSourceRasterCell) bool {
	const epsilon = float32(1e-4)
	return navAlmostEqual(a.MaxX, b.MinX, epsilon) &&
		navAlmostEqual(a.MinZ, b.MinZ, epsilon) &&
		navAlmostEqual(a.MaxZ, b.MaxZ, epsilon)
}

func navRasterCellsTouchZ(a, b navBuildSourceRasterCell) bool {
	const epsilon = float32(1e-4)
	return navAlmostEqual(a.MaxZ, b.MinZ, epsilon) &&
		navAlmostEqual(a.MinX, b.MinX, epsilon) &&
		navAlmostEqual(a.MaxX, b.MaxX, epsilon)
}

func navRasterCellsSameColumnBounds(a, b navBuildSourceRasterCell) bool {
	const epsilon = float32(1e-4)
	return navAlmostEqual(a.MinX, b.MinX, epsilon) && navAlmostEqual(a.MaxX, b.MaxX, epsilon)
}

func appendNavBuildSourceRasterRegionPolygon(tile *NavTileDef, region navBuildSourceRasterRegion, profile NavAgentProfileDef) {
	if tile == nil || len(region.Cells) == 0 {
		return
	}
	corners := []Vec3{
		{region.BoundsMinX, 0, region.BoundsMinZ},
		{region.BoundsMaxX, 0, region.BoundsMinZ},
		{region.BoundsMaxX, 0, region.BoundsMaxZ},
		{region.BoundsMinX, 0, region.BoundsMaxZ},
	}
	for i := range corners {
		corners[i][1] = navBuildSourceRasterPlaneHeight(region.Plane, corners[i][0], corners[i][2])
	}
	base := len(tile.Vertices)
	tile.Vertices = append(tile.Vertices, corners...)
	tile.Polygons = append(tile.Polygons, NavPolygonDef{
		ID:       navBuildSourceRasterRegionID(region),
		Vertices: []int{base, base + 1, base + 2, base + 3},
		Area:     navBuildSourceSurfaceTraversalArea(region.Surface.Area, corners, region.Surface.Normal, profile),
		Flags:    append([]string(nil), region.Surface.Flags...),
	})
}

func navBuildSourceRasterRegionID(region navBuildSourceRasterRegion) string {
	return "raster_region:" +
		navBuildFloatKey(region.BoundsMinX) + ":" +
		navBuildFloatKey(region.BoundsMinZ) + ":" +
		navBuildFloatKey(region.BoundsMaxX) + ":" +
		navBuildFloatKey(region.BoundsMaxZ) + ":" +
		navBuildFloatKey(region.Plane.X) + ":" +
		navBuildFloatKey(region.Plane.Z) + ":" +
		navBuildFloatKey(region.Plane.C)
}

func navBuildSourceRasterPlaneFromSurface(vertices []Vec3) (navBuildSourceRasterPlane, bool) {
	if len(vertices) < 3 {
		return navBuildSourceRasterPlane{}, false
	}
	normal := navVec3Cross(navVec3Sub(vertices[1], vertices[0]), navVec3Sub(vertices[2], vertices[0]))
	if absNavFloat32(normal[1]) <= 1e-6 {
		return navBuildSourceRasterPlane{}, false
	}
	x := -normal[0] / normal[1]
	z := -normal[2] / normal[1]
	c := vertices[0][1] - x*vertices[0][0] - z*vertices[0][2]
	return navBuildSourceRasterPlane{X: x, Z: z, C: c}, true
}

func navBuildSourceRasterPlaneHeight(plane navBuildSourceRasterPlane, x, z float32) float32 {
	return plane.X*x + plane.Z*z + plane.C
}

func navBuildSourceRasterMergeKey(surface NavBuildSurfaceDef, plane navBuildSourceRasterPlane) string {
	return surface.Area + "|" +
		strings.Join(surface.Flags, ",") + "|" +
		navBuildFloatKey(plane.X) + "|" +
		navBuildFloatKey(plane.Z) + "|" +
		navBuildFloatKey(plane.C)
}

func navBuildPolygonBoundsXZ(vertices []Vec3) (float32, float32, float32, float32) {
	if len(vertices) == 0 {
		return 0, 0, 0, 0
	}
	minX := vertices[0][0]
	maxX := vertices[0][0]
	minZ := vertices[0][2]
	maxZ := vertices[0][2]
	for _, vertex := range vertices[1:] {
		minX = minNavFloat32(minX, vertex[0])
		maxX = maxNavFloat32(maxX, vertex[0])
		minZ = minNavFloat32(minZ, vertex[2])
		maxZ = maxNavFloat32(maxZ, vertex[2])
	}
	return minX, maxX, minZ, maxZ
}

func navBuildSurfaceHeightAtXZ(vertices []Vec3, point Vec3) (float32, bool) {
	if len(vertices) < 3 {
		return 0, false
	}
	a := vertices[0]
	for i := 1; i+1 < len(vertices); i++ {
		if y, ok := navTriangleHeightAtXZ(a, vertices[i], vertices[i+1], point); ok {
			return y, true
		}
	}
	normal := navVec3Cross(navVec3Sub(vertices[1], vertices[0]), navVec3Sub(vertices[2], vertices[0]))
	if absNavFloat32(normal[1]) <= 1e-6 {
		return 0, false
	}
	dx := point[0] - vertices[0][0]
	dz := point[2] - vertices[0][2]
	return vertices[0][1] - (normal[0]*dx+normal[2]*dz)/normal[1], true
}

func navClipPolygonXZ(vertices []Vec3, minX, maxX, minZ, maxZ float32) []Vec3 {
	out := append([]Vec3(nil), vertices...)
	out = navClipPolygonXZAxis(out, 0, minX, true)
	out = navClipPolygonXZAxis(out, 0, maxX, false)
	out = navClipPolygonXZAxis(out, 2, minZ, true)
	out = navClipPolygonXZAxis(out, 2, maxZ, false)
	return navCleanClippedPolygonXZ(out)
}

func navClipPolygonY(vertices []Vec3, minY, maxY float32) []Vec3 {
	out := append([]Vec3(nil), vertices...)
	out = navClipPolygonYAxis(out, minY, true)
	out = navClipPolygonYAxis(out, maxY, false)
	return out
}

func navClipPolygonYAxis(vertices []Vec3, boundary float32, keepGreater bool) []Vec3 {
	if len(vertices) == 0 {
		return nil
	}
	out := make([]Vec3, 0, len(vertices)+1)
	prev := vertices[len(vertices)-1]
	prevInside := navClipPolygonYInside(prev, boundary, keepGreater)
	for _, curr := range vertices {
		currInside := navClipPolygonYInside(curr, boundary, keepGreater)
		if currInside {
			if !prevInside {
				out = append(out, navClipPolygonYIntersection(prev, curr, boundary))
			}
			out = append(out, curr)
		} else if prevInside {
			out = append(out, navClipPolygonYIntersection(prev, curr, boundary))
		}
		prev = curr
		prevInside = currInside
	}
	return out
}

func navClipPolygonYInside(vertex Vec3, boundary float32, keepGreater bool) bool {
	const epsilon = float32(1e-5)
	if keepGreater {
		return vertex[1] >= boundary-epsilon
	}
	return vertex[1] <= boundary+epsilon
}

func navClipPolygonYIntersection(a Vec3, b Vec3, boundary float32) Vec3 {
	denominator := b[1] - a[1]
	if absNavFloat32(denominator) <= 1e-6 {
		out := a
		out[1] = boundary
		return out
	}
	t := (boundary - a[1]) / denominator
	return Vec3{
		a[0] + (b[0]-a[0])*t,
		boundary,
		a[2] + (b[2]-a[2])*t,
	}
}

func navClipPolygonXZAxis(vertices []Vec3, axis int, boundary float32, keepGreater bool) []Vec3 {
	if len(vertices) == 0 {
		return nil
	}
	out := make([]Vec3, 0, len(vertices)+1)
	prev := vertices[len(vertices)-1]
	prevInside := navClipPolygonXZInside(prev, axis, boundary, keepGreater)
	for _, curr := range vertices {
		currInside := navClipPolygonXZInside(curr, axis, boundary, keepGreater)
		if currInside {
			if !prevInside {
				out = append(out, navClipPolygonXZIntersection(prev, curr, axis, boundary))
			}
			out = append(out, curr)
		} else if prevInside {
			out = append(out, navClipPolygonXZIntersection(prev, curr, axis, boundary))
		}
		prev = curr
		prevInside = currInside
	}
	return navCleanClippedPolygonXZ(out)
}

func navClipPolygonXZInside(vertex Vec3, axis int, boundary float32, keepGreater bool) bool {
	const epsilon = float32(1e-5)
	if keepGreater {
		return vertex[axis] >= boundary-epsilon
	}
	return vertex[axis] <= boundary+epsilon
}

func navClipPolygonXZIntersection(a Vec3, b Vec3, axis int, boundary float32) Vec3 {
	denominator := b[axis] - a[axis]
	if absNavFloat32(denominator) <= 1e-6 {
		out := a
		out[axis] = boundary
		return out
	}
	t := (boundary - a[axis]) / denominator
	otherAxis := 2
	if axis == 2 {
		otherAxis = 0
	}
	out := Vec3{}
	out[axis] = boundary
	out[otherAxis] = a[otherAxis] + (b[otherAxis]-a[otherAxis])*t
	return out
}

func navCleanClippedPolygonXZ(vertices []Vec3) []Vec3 {
	if len(vertices) == 0 {
		return nil
	}
	const epsilon = float32(1e-5)
	out := make([]Vec3, 0, len(vertices))
	for _, vertex := range vertices {
		if len(out) > 0 && navVec2AlmostEqualXZ(out[len(out)-1], vertex, epsilon) {
			continue
		}
		out = append(out, vertex)
	}
	if len(out) > 1 && navVec2AlmostEqualXZ(out[0], out[len(out)-1], epsilon) {
		out = out[:len(out)-1]
	}
	if len(out) < 3 {
		return out
	}
	cleaned := make([]Vec3, 0, len(out))
	for i := range out {
		prev := out[(i+len(out)-1)%len(out)]
		curr := out[i]
		next := out[(i+1)%len(out)]
		if navPointsCollinearXZ(prev, curr, next, epsilon) {
			continue
		}
		cleaned = append(cleaned, curr)
	}
	return cleaned
}

func navVec2AlmostEqualXZ(a Vec3, b Vec3, epsilon float32) bool {
	return absNavFloat32(a[0]-b[0]) <= epsilon && absNavFloat32(a[2]-b[2]) <= epsilon
}

func navPointsCollinearXZ(a Vec3, b Vec3, c Vec3, epsilon float32) bool {
	abX := b[0] - a[0]
	abZ := b[2] - a[2]
	bcX := c[0] - b[0]
	bcZ := c[2] - b[2]
	scale := maxNavFloat32(1, navVec2Length(abX, abZ)*navVec2Length(bcX, bcZ))
	return absNavFloat32(navVec2Cross(abX, abZ, bcX, bcZ)) <= epsilon*scale
}

func navPolygonAreaXZAbs(vertices []Vec3) float32 {
	if len(vertices) < 3 {
		return 0
	}
	area := float32(0)
	for i := range vertices {
		a := vertices[i]
		b := vertices[(i+1)%len(vertices)]
		area += a[0]*b[2] - b[0]*a[2]
	}
	if area < 0 {
		area = -area
	}
	return area * 0.5
}

func navBuildSourceRectFromSurface(surface NavBuildSurfaceDef, surfaceIndex int) (navBuildSourceRectPolygon, bool) {
	if len(surface.Vertices) != 4 {
		return navBuildSourceRectPolygon{}, false
	}
	const epsilon = float32(1e-4)
	y := surface.Vertices[0][1]
	xs := make([]float32, 0, 2)
	zs := make([]float32, 0, 2)
	for _, vertex := range surface.Vertices {
		if !navAlmostEqual(vertex[1], y, epsilon) {
			return navBuildSourceRectPolygon{}, false
		}
		xs = appendUniqueNavBuildFloat(xs, vertex[0], epsilon)
		zs = appendUniqueNavBuildFloat(zs, vertex[2], epsilon)
	}
	if len(xs) != 2 || len(zs) != 2 {
		return navBuildSourceRectPolygon{}, false
	}
	minX, maxX := minNavFloat32(xs[0], xs[1]), maxNavFloat32(xs[0], xs[1])
	minZ, maxZ := minNavFloat32(zs[0], zs[1]), maxNavFloat32(zs[0], zs[1])
	if !navBuildRectHasCorners(surface.Vertices, minX, maxX, minZ, maxZ, epsilon) {
		return navBuildSourceRectPolygon{}, false
	}
	return navBuildSourceRectPolygon{
		MinX:  minX,
		MaxX:  maxX,
		MinZ:  minZ,
		MaxZ:  maxZ,
		Y:     y,
		Area:  surface.Area,
		Flags: append([]string(nil), surface.Flags...),
		ID:    navBuildSurfacePolygonID(surface, surfaceIndex, -1),
	}, true
}

func navBuildSourceRasterCellID(rect navBuildSourceRectPolygon, x, z int) string {
	id := rect.ID
	if id == "" {
		id = "surface"
	}
	return "raster_cell:" + id + ":" + itoa(x) + ":" + itoa(z)
}

func mergeNavBuildSourceRectPolygons(tile *NavTileDef, firstSourcePolygon int) {
	if tile == nil || firstSourcePolygon < 0 || firstSourcePolygon >= len(tile.Polygons) {
		return
	}
	prefix := append([]NavPolygonDef(nil), tile.Polygons[:firstSourcePolygon]...)
	rects := make([]navBuildSourceRectPolygon, 0, len(tile.Polygons)-firstSourcePolygon)
	passthrough := make([]NavPolygonDef, 0)
	for _, polygon := range tile.Polygons[firstSourcePolygon:] {
		rect, ok := navBuildSourceRectFromPolygon(tile, polygon)
		if !ok {
			passthrough = append(passthrough, polygon)
			continue
		}
		rects = append(rects, rect)
	}
	if len(rects) < 2 {
		return
	}
	rects = normalizeNavBuildSourceRectPolygons(rects)
	merged := true
	for merged {
		merged = false
		for i := 0; i < len(rects) && !merged; i++ {
			for j := i + 1; j < len(rects); j++ {
				combined, ok := mergeNavBuildSourceRectPair(rects[i], rects[j])
				if !ok {
					continue
				}
				rects[i] = combined
				rects = append(rects[:j], rects[j+1:]...)
				merged = true
				break
			}
		}
	}
	if len(rects)+len(passthrough) == len(tile.Polygons)-firstSourcePolygon {
		return
	}
	out := make([]NavPolygonDef, 0, len(prefix)+len(rects)+len(passthrough))
	out = append(out, prefix...)
	for _, rect := range rects {
		out = append(out, appendNavBuildSourceRectPolygon(tile, rect))
	}
	out = append(out, passthrough...)
	tile.Polygons = out
}

func normalizeNavBuildSourceRectPolygons(rects []navBuildSourceRectPolygon) []navBuildSourceRectPolygon {
	if len(rects) < 2 {
		return rects
	}
	groups := make(map[string][]navBuildSourceRectPolygon)
	keys := make([]string, 0)
	for _, rect := range rects {
		key := navBuildSourceRectUnionKey(rect)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], rect)
	}
	sort.Strings(keys)
	out := make([]navBuildSourceRectPolygon, 0, len(rects))
	for _, key := range keys {
		group := groups[key]
		if !navBuildSourceRectGroupHasOverlap(group) {
			out = append(out, group...)
			continue
		}
		out = append(out, navBuildSourceRectUnion(group)...)
	}
	return out
}

func navBuildSourceRectUnionKey(rect navBuildSourceRectPolygon) string {
	return rect.Area + "|" +
		strings.Join(rect.Flags, ",") + "|" +
		navBuildFloatKey(rect.Y)
}

func navBuildSourceRectGroupHasOverlap(rects []navBuildSourceRectPolygon) bool {
	const epsilon = float32(1e-4)
	for i := 0; i < len(rects); i++ {
		for j := i + 1; j < len(rects); j++ {
			if navRangesOverlapPositive(rects[i].MinX, rects[i].MaxX, rects[j].MinX, rects[j].MaxX, epsilon) &&
				navRangesOverlapPositive(rects[i].MinZ, rects[i].MaxZ, rects[j].MinZ, rects[j].MaxZ, epsilon) {
				return true
			}
		}
	}
	return false
}

func navBuildSourceRectUnion(rects []navBuildSourceRectPolygon) []navBuildSourceRectPolygon {
	if len(rects) == 0 {
		return nil
	}
	xs := make([]float32, 0, len(rects)*2)
	zs := make([]float32, 0, len(rects)*2)
	for _, rect := range rects {
		xs = append(xs, rect.MinX, rect.MaxX)
		zs = append(zs, rect.MinZ, rect.MaxZ)
	}
	xs = navBuildSortedUniqueFloat32(xs, 1e-4)
	zs = navBuildSortedUniqueFloat32(zs, 1e-4)
	if len(xs) < 2 || len(zs) < 2 {
		return rects
	}
	out := make([]navBuildSourceRectPolygon, 0, len(rects))
	const epsilon = float32(1e-5)
	for xi := 0; xi+1 < len(xs); xi++ {
		for zi := 0; zi+1 < len(zs); zi++ {
			minX := xs[xi]
			maxX := xs[xi+1]
			minZ := zs[zi]
			maxZ := zs[zi+1]
			if maxX-minX <= epsilon || maxZ-minZ <= epsilon {
				continue
			}
			centerX := (minX + maxX) * 0.5
			centerZ := (minZ + maxZ) * 0.5
			if !navBuildSourceRectGroupContainsXZ(rects, centerX, centerZ) {
				continue
			}
			rect := rects[0]
			rect.MinX = minX
			rect.MaxX = maxX
			rect.MinZ = minZ
			rect.MaxZ = maxZ
			rect.ID = ""
			rect.Merged = true
			out = append(out, rect)
		}
	}
	if len(out) == 0 {
		return rects
	}
	return out
}

func navBuildSourceRectGroupContainsXZ(rects []navBuildSourceRectPolygon, x, z float32) bool {
	const epsilon = float32(1e-4)
	for _, rect := range rects {
		if x >= rect.MinX-epsilon && x <= rect.MaxX+epsilon && z >= rect.MinZ-epsilon && z <= rect.MaxZ+epsilon {
			return true
		}
	}
	return false
}

func navBuildSourceRectFromPolygon(tile *NavTileDef, polygon NavPolygonDef) (navBuildSourceRectPolygon, bool) {
	if tile == nil || len(polygon.Vertices) != 4 {
		return navBuildSourceRectPolygon{}, false
	}
	vertices := make([]Vec3, 0, 4)
	for _, index := range polygon.Vertices {
		if index < 0 || index >= len(tile.Vertices) {
			return navBuildSourceRectPolygon{}, false
		}
		vertices = append(vertices, tile.Vertices[index])
	}
	const epsilon = float32(1e-4)
	y := vertices[0][1]
	xs := make([]float32, 0, 2)
	zs := make([]float32, 0, 2)
	for _, vertex := range vertices {
		if !navAlmostEqual(vertex[1], y, epsilon) {
			return navBuildSourceRectPolygon{}, false
		}
		xs = appendUniqueNavBuildFloat(xs, vertex[0], epsilon)
		zs = appendUniqueNavBuildFloat(zs, vertex[2], epsilon)
	}
	if len(xs) != 2 || len(zs) != 2 {
		return navBuildSourceRectPolygon{}, false
	}
	minX, maxX := minNavFloat32(xs[0], xs[1]), maxNavFloat32(xs[0], xs[1])
	minZ, maxZ := minNavFloat32(zs[0], zs[1]), maxNavFloat32(zs[0], zs[1])
	if !navBuildRectHasCorners(vertices, minX, maxX, minZ, maxZ, epsilon) {
		return navBuildSourceRectPolygon{}, false
	}
	return navBuildSourceRectPolygon{
		MinX:  minX,
		MaxX:  maxX,
		MinZ:  minZ,
		MaxZ:  maxZ,
		Y:     y,
		Area:  polygon.Area,
		Flags: append([]string(nil), polygon.Flags...),
		ID:    polygon.ID,
	}, true
}

func appendUniqueNavBuildFloat(values []float32, value float32, epsilon float32) []float32 {
	for _, existing := range values {
		if navAlmostEqual(existing, value, epsilon) {
			return values
		}
	}
	return append(values, value)
}

func navBuildRectHasCorners(vertices []Vec3, minX, maxX, minZ, maxZ, epsilon float32) bool {
	return navBuildRectHasCorner(vertices, minX, minZ, epsilon) &&
		navBuildRectHasCorner(vertices, maxX, minZ, epsilon) &&
		navBuildRectHasCorner(vertices, maxX, maxZ, epsilon) &&
		navBuildRectHasCorner(vertices, minX, maxZ, epsilon)
}

func navBuildRectHasCorner(vertices []Vec3, x, z, epsilon float32) bool {
	for _, vertex := range vertices {
		if navAlmostEqual(vertex[0], x, epsilon) && navAlmostEqual(vertex[2], z, epsilon) {
			return true
		}
	}
	return false
}

func mergeNavBuildSourceRectPair(a navBuildSourceRectPolygon, b navBuildSourceRectPolygon) (navBuildSourceRectPolygon, bool) {
	const epsilon = float32(1e-4)
	if a.Area != b.Area || !navStringSlicesEqual(a.Flags, b.Flags) || !navAlmostEqual(a.Y, b.Y, epsilon) {
		return navBuildSourceRectPolygon{}, false
	}
	out := a
	switch {
	case navAlmostEqual(a.MinZ, b.MinZ, epsilon) &&
		navAlmostEqual(a.MaxZ, b.MaxZ, epsilon) &&
		(navAlmostEqual(a.MaxX, b.MinX, epsilon) || navAlmostEqual(b.MaxX, a.MinX, epsilon)):
		out.MinX = minNavFloat32(a.MinX, b.MinX)
		out.MaxX = maxNavFloat32(a.MaxX, b.MaxX)
	case navAlmostEqual(a.MinX, b.MinX, epsilon) &&
		navAlmostEqual(a.MaxX, b.MaxX, epsilon) &&
		(navAlmostEqual(a.MaxZ, b.MinZ, epsilon) || navAlmostEqual(b.MaxZ, a.MinZ, epsilon)):
		out.MinZ = minNavFloat32(a.MinZ, b.MinZ)
		out.MaxZ = maxNavFloat32(a.MaxZ, b.MaxZ)
	default:
		return navBuildSourceRectPolygon{}, false
	}
	out.ID = navBuildSourceMergedRectID(out)
	out.Merged = true
	return out, true
}

func appendNavBuildSourceRectPolygon(tile *NavTileDef, rect navBuildSourceRectPolygon) NavPolygonDef {
	base := len(tile.Vertices)
	tile.Vertices = append(tile.Vertices,
		Vec3{rect.MinX, rect.Y, rect.MinZ},
		Vec3{rect.MaxX, rect.Y, rect.MinZ},
		Vec3{rect.MaxX, rect.Y, rect.MaxZ},
		Vec3{rect.MinX, rect.Y, rect.MaxZ},
	)
	return NavPolygonDef{
		ID:       navBuildSourceRectPolygonID(rect),
		Vertices: []int{base, base + 1, base + 2, base + 3},
		Area:     rect.Area,
		Flags:    append([]string(nil), rect.Flags...),
	}
}

func navBuildSourceRectPolygonID(rect navBuildSourceRectPolygon) string {
	if rect.Merged || rect.ID == "" {
		return navBuildSourceMergedRectID(rect)
	}
	return rect.ID
}

func navBuildSourceMergedRectID(rect navBuildSourceRectPolygon) string {
	return "merged_rect:" +
		navBuildFloatKey(rect.MinX) + ":" +
		navBuildFloatKey(rect.Y) + ":" +
		navBuildFloatKey(rect.MinZ) + ":" +
		navBuildFloatKey(rect.MaxX) + ":" +
		navBuildFloatKey(rect.MaxZ)
}

func navBuildFloatKey(value float32) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", value), "0"), ".")
}

func navStringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func appendNavBuildSourcePolygon(tile *NavTileDef, surface NavBuildSurfaceDef, surfaceIndex int, triangleIndex int, vertices []Vec3, profile NavAgentProfileDef) {
	polygonID := navBuildSurfacePolygonID(surface, surfaceIndex, triangleIndex)
	area := navBuildSourceSurfaceTraversalArea(surface.Area, vertices, surface.Normal, profile)
	appendNavPolygonVertices(tile, polygonID, vertices, area, surface.Flags)
}

func navBuildSourceSurfaceTraversalArea(area string, vertices []Vec3, authoredNormal Vec3, profile NavAgentProfileDef) string {
	area = firstNonEmptyNavString(area, NavTraversalWalk)
	if area != NavTraversalWalk {
		return area
	}
	normal := authoredNormal
	if navVec3Length(normal) == 0 {
		if len(vertices) < 3 {
			return area
		}
		normal = navVec3Cross(navVec3Sub(vertices[1], vertices[0]), navVec3Sub(vertices[2], vertices[0]))
		if normal[1] < 0 {
			normal[0] = -normal[0]
			normal[1] = -normal[1]
			normal[2] = -normal[2]
		}
	}
	length := navVec3Length(normal)
	if length == 0 || normal[1] <= 0 {
		return area
	}
	upness := clampNavFloat32(normal[1]/length, -1, 1)
	angle := float32(math.Acos(float64(upness)) * 180.0 / math.Pi)
	const epsilon = float32(1e-4)
	if angle <= epsilon {
		return area
	}
	EnsureNavAgentProfileDefaults(&profile)
	if profile.MaxSlopeDegrees <= 0 || angle <= profile.MaxSlopeDegrees+epsilon {
		return NavTraversalRamp
	}
	return area
}

func appendNavPolygonVertices(tile *NavTileDef, polygonID string, vertices []Vec3, area string, flags []string) []string {
	if tile == nil || len(vertices) < 3 {
		return nil
	}
	vertices = navCleanClippedPolygonXZ(vertices)
	if len(vertices) < 3 || navPolygonAreaXZAbs(vertices) <= 1e-5 {
		return nil
	}
	pieces := [][]Vec3{vertices}
	if !navPolygonVerticesConvexXZ(vertices) {
		if triangles, ok := triangulateNavPolygonVerticesXZ(vertices); ok {
			pieces = triangles
		}
	}
	ids := make([]string, 0, len(pieces))
	for i, piece := range pieces {
		if len(piece) < 3 || navPolygonAreaXZAbs(piece) <= 1e-5 {
			continue
		}
		base := len(tile.Vertices)
		tile.Vertices = append(tile.Vertices, piece...)
		indices := make([]int, 0, len(piece))
		for vertexIndex := range piece {
			indices = append(indices, base+vertexIndex)
		}
		id := navPolygonPartID(polygonID, i, len(pieces))
		tile.Polygons = append(tile.Polygons, NavPolygonDef{
			ID:       id,
			Vertices: indices,
			Area:     area,
			Flags:    append([]string(nil), flags...),
		})
		ids = append(ids, id)
	}
	return ids
}

func navPolygonPartID(baseID string, partIndex int, partCount int) string {
	if partCount <= 1 || baseID == "" {
		return baseID
	}
	return baseID + ":part:" + itoa(partIndex)
}

func navPolygonVerticesConvexXZ(vertices []Vec3) bool {
	if len(vertices) < 4 {
		return true
	}
	sign := 0
	const epsilon = float32(1e-5)
	for i := range vertices {
		prev := vertices[(i+len(vertices)-1)%len(vertices)]
		curr := vertices[i]
		next := vertices[(i+1)%len(vertices)]
		cross := navVec2Cross(curr[0]-prev[0], curr[2]-prev[2], next[0]-curr[0], next[2]-curr[2])
		if absNavFloat32(cross) <= epsilon {
			continue
		}
		currentSign := 1
		if cross < 0 {
			currentSign = -1
		}
		if sign == 0 {
			sign = currentSign
			continue
		}
		if sign != currentSign {
			return false
		}
	}
	return true
}

func triangulateNavPolygonVerticesXZ(vertices []Vec3) ([][]Vec3, bool) {
	if len(vertices) < 3 {
		return nil, false
	}
	if len(vertices) == 3 {
		return [][]Vec3{append([]Vec3(nil), vertices...)}, true
	}
	area := navPolygonSignedAreaXZ(vertices)
	if absNavFloat32(area) <= 1e-5 {
		return nil, false
	}
	orientation := float32(1)
	if area < 0 {
		orientation = -1
	}
	remaining := make([]int, len(vertices))
	for i := range vertices {
		remaining[i] = i
	}
	triangles := make([][]Vec3, 0, len(vertices)-2)
	for len(remaining) > 3 {
		earIndex := -1
		for i := range remaining {
			prevIndex := remaining[(i+len(remaining)-1)%len(remaining)]
			currIndex := remaining[i]
			nextIndex := remaining[(i+1)%len(remaining)]
			if !navTriangleCornerConvexXZ(vertices[prevIndex], vertices[currIndex], vertices[nextIndex], orientation) {
				continue
			}
			if navTriangleContainsAnyPolygonVertexXZ(vertices, remaining, prevIndex, currIndex, nextIndex) {
				continue
			}
			earIndex = i
			triangles = append(triangles, []Vec3{vertices[prevIndex], vertices[currIndex], vertices[nextIndex]})
			break
		}
		if earIndex < 0 {
			return nil, false
		}
		remaining = append(remaining[:earIndex], remaining[earIndex+1:]...)
	}
	triangles = append(triangles, []Vec3{vertices[remaining[0]], vertices[remaining[1]], vertices[remaining[2]]})
	return triangles, true
}

func navPolygonSignedAreaXZ(vertices []Vec3) float32 {
	area := float32(0)
	for i := range vertices {
		a := vertices[i]
		b := vertices[(i+1)%len(vertices)]
		area += a[0]*b[2] - b[0]*a[2]
	}
	return area * 0.5
}

func navTriangleCornerConvexXZ(prev Vec3, curr Vec3, next Vec3, orientation float32) bool {
	cross := navVec2Cross(curr[0]-prev[0], curr[2]-prev[2], next[0]-curr[0], next[2]-curr[2])
	return cross*orientation > 1e-5
}

func navTriangleContainsAnyPolygonVertexXZ(vertices []Vec3, remaining []int, aIndex int, bIndex int, cIndex int) bool {
	a := vertices[aIndex]
	b := vertices[bIndex]
	c := vertices[cIndex]
	for _, index := range remaining {
		if index == aIndex || index == bIndex || index == cIndex {
			continue
		}
		if navPointInTriangleXZ(vertices[index], a, b, c) {
			return true
		}
	}
	return false
}

func navPointInTriangleXZ(point Vec3, a Vec3, b Vec3, c Vec3) bool {
	const epsilon = float32(1e-5)
	area := absNavFloat32(navVec2Cross(b[0]-a[0], b[2]-a[2], c[0]-a[0], c[2]-a[2]))
	if area <= epsilon {
		return false
	}
	area0 := absNavFloat32(navVec2Cross(a[0]-point[0], a[2]-point[2], b[0]-point[0], b[2]-point[2]))
	area1 := absNavFloat32(navVec2Cross(b[0]-point[0], b[2]-point[2], c[0]-point[0], c[2]-point[2]))
	area2 := absNavFloat32(navVec2Cross(c[0]-point[0], c[2]-point[2], a[0]-point[0], a[2]-point[2]))
	return absNavFloat32((area0+area1+area2)-area) <= epsilon*maxNavFloat32(1, area)
}

func navBuildSurfacePolygonID(surface NavBuildSurfaceDef, surfaceIndex int, triangleIndex int) string {
	sourceID := surface.ID
	if sourceID == "" {
		sourceID = "surface_" + itoa(surfaceIndex)
	}
	if triangleIndex >= 0 {
		return "surface:" + sourceID + ":tri:" + itoa(triangleIndex)
	}
	return "surface:" + sourceID
}

func navBuildSourceHasTileGeometry(source *NavBuildSourceDef, coord TerrainChunkCoordDef, chunkSize int, voxelResolution float32) bool {
	if source == nil || chunkSize <= 0 || voxelResolution <= 0 {
		return false
	}
	EnsureNavBuildSourceDefaults(source)
	boundsMin, boundsMax := navTileBoundsForCoord(coord, chunkSize, voxelResolution)
	for surfaceIndex := range source.Surfaces {
		surface := source.Surfaces[surfaceIndex]
		EnsureNavBuildSurfaceDefaults(&surface)
		if surface.Kind != NavBuildSurfaceWalkable {
			continue
		}
		if len(surface.Indices) >= 3 && len(surface.Indices)%3 == 0 {
			for tri := 0; tri < len(surface.Indices); tri += 3 {
				a := surface.Indices[tri]
				b := surface.Indices[tri+1]
				c := surface.Indices[tri+2]
				if a < 0 || b < 0 || c < 0 || a >= len(surface.Vertices) || b >= len(surface.Vertices) || c >= len(surface.Vertices) {
					continue
				}
				if len(navClipNavBuildSourcePolygonToTile([]Vec3{surface.Vertices[a], surface.Vertices[b], surface.Vertices[c]}, boundsMin, boundsMax)) >= 3 {
					return true
				}
			}
			continue
		}
		if len(surface.Vertices) >= 3 && len(navClipNavBuildSourcePolygonToTile(surface.Vertices, boundsMin, boundsMax)) >= 3 {
			return true
		}
	}
	return false
}

func navTileBoundsForCoord(coord TerrainChunkCoordDef, chunkSize int, voxelResolution float32) ([3]float32, [3]float32) {
	worldSize := float32(chunkSize) * voxelResolution
	min := [3]float32{
		float32(coord.X) * worldSize,
		float32(coord.Y) * worldSize,
		float32(coord.Z) * worldSize,
	}
	max := [3]float32{min[0] + worldSize, min[1] + worldSize, min[2] + worldSize}
	return min, max
}

func navBuildSurfacePolygonOverlapsTile(vertices []Vec3, boundsMin [3]float32, boundsMax [3]float32) bool {
	if len(vertices) == 0 {
		return false
	}
	const epsilon = float32(1e-4)
	min := vertices[0]
	max := vertices[0]
	for _, vertex := range vertices[1:] {
		for axis := 0; axis < 3; axis++ {
			if vertex[axis] < min[axis] {
				min[axis] = vertex[axis]
			}
			if vertex[axis] > max[axis] {
				max[axis] = vertex[axis]
			}
		}
	}
	return max[0] >= boundsMin[0]-epsilon && min[0] <= boundsMax[0]+epsilon &&
		max[1] >= boundsMin[1]-epsilon && min[1] <= boundsMax[1]+epsilon &&
		max[2] >= boundsMin[2]-epsilon && min[2] <= boundsMax[2]+epsilon
}

func navBuildSurfacePolygonSlopeWalkable(vertices []Vec3, authoredNormal Vec3, profile NavAgentProfileDef) bool {
	normal := authoredNormal
	hasAuthoredNormal := navVec3Length(normal) != 0
	if !hasAuthoredNormal {
		if len(vertices) < 3 {
			return false
		}
		normal = navVec3Cross(navVec3Sub(vertices[1], vertices[0]), navVec3Sub(vertices[2], vertices[0]))
		if normal[1] < 0 {
			normal[0] = -normal[0]
			normal[1] = -normal[1]
			normal[2] = -normal[2]
		}
	}
	length := navVec3Length(normal)
	if length == 0 {
		return false
	}
	if hasAuthoredNormal && normal[1] <= 0 {
		return false
	}
	upness := normal[1] / length
	if upness <= 0 {
		return false
	}
	if profile.MaxSlopeDegrees <= 0 {
		return true
	}
	angle := float32(math.Acos(float64(upness)) * 180.0 / math.Pi)
	const epsilon = float32(1e-4)
	return angle <= profile.MaxSlopeDegrees+epsilon
}

func applyNavSurfaceNeighbors(tile *NavTileDef, firstNewPolygon int, profile NavAgentProfileDef) {
	if tile == nil || firstNewPolygon < 0 || firstNewPolygon >= len(tile.Polygons) {
		return
	}
	for i := 0; i < len(tile.Polygons); i++ {
		startJ := firstNewPolygon
		if i >= firstNewPolygon {
			startJ = i + 1
		}
		for j := startJ; j < len(tile.Polygons); j++ {
			if i == j {
				continue
			}
			if navPolygonsShareEdge(tile, tile.Polygons[i], tile.Polygons[j]) ||
				navPolygonsConnectAcrossStep(tile, tile.Polygons[i], tile.Polygons[j], profile) {
				tile.Polygons[i].Neighbors = appendUniqueNavString(tile.Polygons[i].Neighbors, tile.Polygons[j].ID)
				tile.Polygons[j].Neighbors = appendUniqueNavString(tile.Polygons[j].Neighbors, tile.Polygons[i].ID)
			}
		}
	}
	for i := range tile.Polygons {
		sort.Strings(tile.Polygons[i].Neighbors)
	}
}

func navPolygonsShareEdge(tile *NavTileDef, a NavPolygonDef, b NavPolygonDef) bool {
	if a.ID == "" || b.ID == "" {
		return false
	}
	matches := 0
	for _, ai := range a.Vertices {
		if ai < 0 || ai >= len(tile.Vertices) {
			continue
		}
		for _, bi := range b.Vertices {
			if bi < 0 || bi >= len(tile.Vertices) {
				continue
			}
			if navVec3AlmostEqual(tile.Vertices[ai], tile.Vertices[bi], 1e-4) {
				matches++
				break
			}
		}
	}
	return matches >= 2
}

func navPolygonsConnectAcrossStep(tile *NavTileDef, a NavPolygonDef, b NavPolygonDef, profile NavAgentProfileDef) bool {
	if a.ID == "" || b.ID == "" || profile.StepHeight <= 0 {
		return false
	}
	for ai := range a.Vertices {
		a0, a1, ok := navPolygonEdge(tile, a, ai)
		if !ok {
			continue
		}
		for bi := range b.Vertices {
			b0, b1, ok := navPolygonEdge(tile, b, bi)
			if !ok {
				continue
			}
			samples, ok := navEdgeOverlapSamplesXZ(a0, a1, b0, b1)
			if !ok {
				continue
			}
			if navPolygonsStepSamplesWalkable(tile, a, b, samples, profile) {
				return true
			}
		}
	}
	return false
}

func navPolygonEdge(tile *NavTileDef, polygon NavPolygonDef, edgeIndex int) (Vec3, Vec3, bool) {
	if tile == nil || edgeIndex < 0 || edgeIndex >= len(polygon.Vertices) {
		return Vec3{}, Vec3{}, false
	}
	aIndex := polygon.Vertices[edgeIndex]
	bIndex := polygon.Vertices[(edgeIndex+1)%len(polygon.Vertices)]
	if aIndex < 0 || aIndex >= len(tile.Vertices) || bIndex < 0 || bIndex >= len(tile.Vertices) {
		return Vec3{}, Vec3{}, false
	}
	return tile.Vertices[aIndex], tile.Vertices[bIndex], true
}

func navEdgeOverlapSamplesXZ(a0, a1 Vec3, b0, b1 Vec3) ([]Vec3, bool) {
	const epsilon = float32(1e-4)
	aDX := a1[0] - a0[0]
	aDZ := a1[2] - a0[2]
	bDX := b1[0] - b0[0]
	bDZ := b1[2] - b0[2]
	aLength := navVec2Length(aDX, aDZ)
	bLength := navVec2Length(bDX, bDZ)
	if aLength <= epsilon || bLength <= epsilon {
		return nil, false
	}
	if !navSegmentsCollinearXZ(a0, aDX, aDZ, aLength, b0, bDX, bDZ, bLength, epsilon) {
		return nil, false
	}

	useX := absNavFloat32(aDX) >= absNavFloat32(aDZ)
	aStart := a0[2]
	aEnd := a1[2]
	bStart := b0[2]
	bEnd := b1[2]
	if useX {
		aStart = a0[0]
		aEnd = a1[0]
		bStart = b0[0]
		bEnd = b1[0]
	}
	denominator := aEnd - aStart
	if absNavFloat32(denominator) <= epsilon {
		return nil, false
	}

	aMin := minNavFloat32(aStart, aEnd)
	aMax := maxNavFloat32(aStart, aEnd)
	bMin := minNavFloat32(bStart, bEnd)
	bMax := maxNavFloat32(bStart, bEnd)
	overlapMin := maxNavFloat32(aMin, bMin)
	overlapMax := minNavFloat32(aMax, bMax)
	if overlapMax-overlapMin <= epsilon {
		return nil, false
	}

	overlapLength := overlapMax - overlapMin
	positions := []float32{(overlapMin + overlapMax) * 0.5}
	if overlapLength > epsilon*4 {
		positions = append(positions,
			overlapMin+overlapLength*0.25,
			overlapMin+overlapLength*0.75,
		)
	}
	samples := make([]Vec3, 0, len(positions))
	for _, position := range positions {
		t := (position - aStart) / denominator
		samples = append(samples, Vec3{
			a0[0] + aDX*t,
			0,
			a0[2] + aDZ*t,
		})
	}
	return samples, true
}

func navSegmentsCollinearXZ(a0 Vec3, aDX float32, aDZ float32, aLength float32, b0 Vec3, bDX float32, bDZ float32, bLength float32, epsilon float32) bool {
	directionCross := navVec2Cross(aDX, aDZ, bDX, bDZ)
	directionTolerance := epsilon * maxNavFloat32(1, aLength) * maxNavFloat32(1, bLength)
	if absNavFloat32(directionCross) > directionTolerance {
		return false
	}
	offsetCross := navVec2Cross(b0[0]-a0[0], b0[2]-a0[2], aDX, aDZ)
	offsetTolerance := epsilon * maxNavFloat32(1, aLength)
	return absNavFloat32(offsetCross) <= offsetTolerance
}

func navPolygonsStepSamplesWalkable(tile *NavTileDef, a NavPolygonDef, b NavPolygonDef, samples []Vec3, profile NavAgentProfileDef) bool {
	if len(samples) == 0 {
		return false
	}
	const epsilon = float32(1e-4)
	for _, sample := range samples {
		aHeight, ok := navPolygonHeightAtXZ(tile, a, sample)
		if !ok {
			return false
		}
		bHeight, ok := navPolygonHeightAtXZ(tile, b, sample)
		if !ok {
			return false
		}
		if absNavFloat32(aHeight-bHeight) > profile.StepHeight+epsilon {
			return false
		}
	}
	return true
}

func navVec2Length(x, z float32) float32 {
	return float32(math.Sqrt(float64(x*x + z*z)))
}

func navVec2Cross(ax, az, bx, bz float32) float32 {
	return ax*bz - az*bx
}

func appendUniqueNavString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func navVec3Sub(a, b Vec3) Vec3 {
	return Vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

func navVec3Cross(a, b Vec3) Vec3 {
	return Vec3{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func navVec3Length(v Vec3) float32 {
	return float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])))
}

func navVec3AlmostEqual(a, b Vec3, epsilon float32) bool {
	return absNavFloat32(a[0]-b[0]) <= epsilon &&
		absNavFloat32(a[1]-b[1]) <= epsilon &&
		absNavFloat32(a[2]-b[2]) <= epsilon
}

func navRegionCellY(region navBuildWalkableRegion, x, z int) int {
	return region.BaseY + region.SlopeX*(x-region.MinX) + region.SlopeZ*(z-region.MinZ)
}

func navRegionCornerY(region navBuildWalkableRegion, x, z int) float32 {
	return navRegionHeightAtGrid(region, float32(x), float32(z))
}

func navRegionHeightAtGrid(region navBuildWalkableRegion, x, z float32) float32 {
	xOffset := x - float32(region.MinX) - 0.5
	zOffset := z - float32(region.MinZ) - 0.5
	if region.HasPlane {
		return region.PlaneBase + region.PlaneX*xOffset + region.PlaneZ*zOffset
	}
	return float32(region.BaseY) + float32(region.SlopeX)*xOffset + float32(region.SlopeZ)*zOffset
}

func clampNavFloat32(value, minValue, maxValue float32) float32 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
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

func navRegionPolygonID(region navBuildWalkableRegion) string {
	if region.MinX == region.MaxX && region.MinZ == region.MaxZ {
		return navCellPolygonID(region.MinX, region.BaseY, region.MinZ)
	}
	if len(region.Contour) > 0 {
		if region.HasPlane {
			base := navBuildPlaneIDValue(region.PlaneBase)
			slopeX := navBuildPlaneIDValue(region.PlaneX)
			slopeZ := navBuildPlaneIDValue(region.PlaneZ)
			if navContourMatchesRegionRect(region) {
				return "plane_contour_rect:" + itoa(region.MinX) + ":" + base + ":" + itoa(region.MinZ) + ":" + itoa(region.MaxX) + ":" + itoa(region.MaxZ) + ":" + slopeX + ":" + slopeZ
			}
			return "plane_contour:" + itoa(region.MinX) + ":" + base + ":" + itoa(region.MinZ) + ":" + itoa(region.MaxX) + ":" + itoa(region.MaxZ) + ":" + slopeX + ":" + slopeZ + ":" + itoa(len(region.Contour))
		}
		if region.SlopeX != 0 || region.SlopeZ != 0 {
			if navContourMatchesRegionRect(region) {
				return "plane_contour_rect:" + itoa(region.MinX) + ":" + itoa(region.BaseY) + ":" + itoa(region.MinZ) + ":" + itoa(region.MaxX) + ":" + itoa(region.MaxZ) + ":" + itoa(region.SlopeX) + ":" + itoa(region.SlopeZ)
			}
			return "plane_contour:" + itoa(region.MinX) + ":" + itoa(region.BaseY) + ":" + itoa(region.MinZ) + ":" + itoa(region.MaxX) + ":" + itoa(region.MaxZ) + ":" + itoa(region.SlopeX) + ":" + itoa(region.SlopeZ) + ":" + itoa(len(region.Contour))
		}
		if navContourMatchesRegionRect(region) {
			return "rect:" + itoa(region.MinX) + ":" + itoa(region.BaseY) + ":" + itoa(region.MinZ) + ":" + itoa(region.MaxX) + ":" + itoa(region.MaxZ)
		}
		return "contour:" + itoa(region.MinX) + ":" + itoa(region.BaseY) + ":" + itoa(region.MinZ) + ":" + itoa(region.MaxX) + ":" + itoa(region.MaxZ) + ":" + itoa(len(region.Contour))
	}
	if region.SlopeX != 0 || region.SlopeZ != 0 {
		return "plane:" + itoa(region.MinX) + ":" + itoa(region.BaseY) + ":" + itoa(region.MinZ) + ":" + itoa(region.MaxX) + ":" + itoa(region.MaxZ) + ":" + itoa(region.SlopeX) + ":" + itoa(region.SlopeZ)
	}
	return "rect:" + itoa(region.MinX) + ":" + itoa(region.BaseY) + ":" + itoa(region.MinZ) + ":" + itoa(region.MaxX) + ":" + itoa(region.MaxZ)
}

func navBuildPlaneIDValue(value float32) string {
	return itoa(int(math.Round(float64(value * 1000))))
}

func navContourMatchesRegionRect(region navBuildWalkableRegion) bool {
	if len(region.Contour) != 4 {
		return false
	}
	corners := map[navBuildContourPoint]struct{}{
		{X: region.MinX, Z: region.MinZ}:         {},
		{X: region.MaxX + 1, Z: region.MinZ}:     {},
		{X: region.MaxX + 1, Z: region.MaxZ + 1}: {},
		{X: region.MinX, Z: region.MaxZ + 1}:     {},
	}
	for _, point := range region.Contour {
		if _, ok := corners[point]; !ok {
			return false
		}
	}
	return true
}

func absNavInt(v int) float32 {
	if v < 0 {
		return float32(-v)
	}
	return float32(v)
}

func absNavIntAsInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func clampNavInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
