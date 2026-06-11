package content

import (
	"fmt"
	"math"
)

const (
	DefaultNavPathMaxTileSearchRadius = 8
	DefaultNavPathMaxTileLoads        = 512
)

const (
	NavPathFailureMissingTile         = "missing_tile"
	NavPathFailureDeltaEmptyTile      = "delta_empty_tile"
	NavPathFailureDisallowedTile      = "disallowed_tile"
	NavPathFailureStartPolygonMissing = "start_polygon_missing"
	NavPathFailureEndPolygonMissing   = "end_polygon_missing"
	NavPathFailureNoPath              = "no_path"
)

type NavPathOptions struct {
	AgentProfileID      string
	MaxTileSearchRadius int
	MaxTileLoads        int
	AllowedTileCoords   map[TerrainChunkCoordDef]struct{}
}

type NavPathStep struct {
	Coord     TerrainChunkCoordDef
	PolygonID string
	Source    string
}

type NavPathResult struct {
	Found         bool
	Steps         []NavPathStep
	Waypoints     []Vec3
	FailureReason string
	FailureCoord  TerrainChunkCoordDef
}

func FindEffectiveNavPath(baseNav *NavManifestDef, baseNavPath string, delta *WorldDeltaDef, deltaPath string, start Vec3, end Vec3, opts NavPathOptions) (NavPathResult, error) {
	if baseNav == nil {
		return NavPathResult{}, fmt.Errorf("base nav manifest is nil")
	}
	EnsureNavManifestDefaults(baseNav)
	opts = normalizeNavPathOptions(opts)
	startCoord, ok := navTileCoordForPoint(start, baseNav.ChunkSize, baseNav.VoxelResolution)
	if !ok {
		return NavPathResult{}, fmt.Errorf("invalid nav manifest chunk metrics")
	}
	endCoord, _ := navTileCoordForPoint(end, baseNav.ChunkSize, baseNav.VoxelResolution)
	ctx := &effectiveNavPathContext{
		baseNav:     baseNav,
		baseNavPath: baseNavPath,
		delta:       delta,
		deltaPath:   deltaPath,
		opts:        opts,
		cache:       map[TerrainChunkCoordDef]*effectiveNavPathTile{},
	}
	if !ctx.coordAllowed(startCoord) {
		return NavPathResult{FailureReason: NavPathFailureDisallowedTile, FailureCoord: startCoord}, nil
	}
	if !ctx.coordAllowed(endCoord) {
		return NavPathResult{FailureReason: NavPathFailureDisallowedTile, FailureCoord: endCoord}, nil
	}
	startTile, err := ctx.loadTile(startCoord)
	if err != nil || startTile == nil || startTile.empty {
		return ctx.failureForTile(startCoord, startTile), err
	}
	endTile, err := ctx.loadTile(endCoord)
	if err != nil || endTile == nil || endTile.empty {
		return ctx.failureForTile(endCoord, endTile), err
	}
	startPolygon, ok := startTile.query.FindPolygonAt(start)
	if !ok {
		return NavPathResult{FailureReason: NavPathFailureStartPolygonMissing, FailureCoord: startCoord}, nil
	}
	endPolygon, ok := endTile.query.FindPolygonAt(end)
	if !ok {
		return NavPathResult{FailureReason: NavPathFailureEndPolygonMissing, FailureCoord: endCoord}, nil
	}
	startNode := navPathNode{coord: startCoord, polygonID: startPolygon.ID}
	endNode := navPathNode{coord: endCoord, polygonID: endPolygon.ID}
	if startNode == endNode {
		center, _ := startTile.query.PolygonCenter(startPolygon.ID)
		return NavPathResult{
			Found:     true,
			Steps:     []NavPathStep{{Coord: startCoord, PolygonID: startPolygon.ID, Source: startTile.lookup.Source}},
			Waypoints: []Vec3{center},
		}, nil
	}
	bounds := navPathSearchBounds(startCoord, endCoord, opts.MaxTileSearchRadius)
	queue := []navPathNode{startNode}
	visited := map[navPathNode]struct{}{startNode: {}}
	previous := map[navPathNode]navPathNode{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		neighbors, err := ctx.neighbors(current, bounds)
		if err != nil {
			return NavPathResult{}, err
		}
		for _, neighbor := range neighbors {
			if _, ok := visited[neighbor]; ok {
				continue
			}
			visited[neighbor] = struct{}{}
			previous[neighbor] = current
			if neighbor == endNode {
				return ctx.buildResult(startNode, endNode, previous), nil
			}
			queue = append(queue, neighbor)
		}
	}
	return ctx.noPathResult(), nil
}

type effectiveNavPathContext struct {
	baseNav     *NavManifestDef
	baseNavPath string
	delta       *WorldDeltaDef
	deltaPath   string
	opts        NavPathOptions
	cache       map[TerrainChunkCoordDef]*effectiveNavPathTile
	failures    []navPathFailure
}

type effectiveNavPathTile struct {
	lookup        NavTileLookupResult
	query         *NavTileQuery
	empty         bool
	failureReason string
}

type navPathFailure struct {
	reason string
	coord  TerrainChunkCoordDef
}

type navPathNode struct {
	coord     TerrainChunkCoordDef
	polygonID string
}

type navPathCoordBounds struct {
	min TerrainChunkCoordDef
	max TerrainChunkCoordDef
}

func normalizeNavPathOptions(opts NavPathOptions) NavPathOptions {
	if opts.AgentProfileID == "" {
		opts.AgentProfileID = DefaultNavAgentProfileID
	}
	if opts.MaxTileSearchRadius <= 0 {
		opts.MaxTileSearchRadius = DefaultNavPathMaxTileSearchRadius
	}
	if opts.MaxTileLoads <= 0 {
		opts.MaxTileLoads = DefaultNavPathMaxTileLoads
	}
	return opts
}

func (ctx *effectiveNavPathContext) loadTile(coord TerrainChunkCoordDef) (*effectiveNavPathTile, error) {
	if tile, ok := ctx.cache[coord]; ok {
		return tile, nil
	}
	if !ctx.coordAllowed(coord) {
		tile := &effectiveNavPathTile{empty: true, failureReason: NavPathFailureDisallowedTile}
		ctx.cache[coord] = tile
		ctx.recordFailure(tile.failureReason, coord)
		return tile, nil
	}
	if len(ctx.cache) >= ctx.opts.MaxTileLoads {
		return nil, fmt.Errorf("nav path tile load budget exceeded (%d)", ctx.opts.MaxTileLoads)
	}
	lookup, err := LoadEffectiveNavTile(ctx.baseNav, ctx.baseNavPath, ctx.delta, ctx.deltaPath, coord, ctx.opts.AgentProfileID)
	if err != nil {
		return nil, err
	}
	if !lookup.Found || lookup.Empty || lookup.Tile == nil {
		tile := &effectiveNavPathTile{lookup: lookup, empty: true, failureReason: navPathFailureReasonForLookup(lookup)}
		ctx.cache[coord] = tile
		ctx.recordFailure(tile.failureReason, coord)
		return tile, nil
	}
	query, err := NewNavTileQuery(lookup.Tile)
	if err != nil {
		return nil, err
	}
	tile := &effectiveNavPathTile{lookup: lookup, query: query}
	ctx.cache[coord] = tile
	return tile, nil
}

func (ctx *effectiveNavPathContext) coordAllowed(coord TerrainChunkCoordDef) bool {
	if len(ctx.opts.AllowedTileCoords) == 0 {
		return true
	}
	_, ok := ctx.opts.AllowedTileCoords[coord]
	return ok
}

func (ctx *effectiveNavPathContext) neighbors(node navPathNode, bounds navPathCoordBounds) ([]navPathNode, error) {
	tile, err := ctx.loadTile(node.coord)
	if err != nil || tile == nil || tile.empty {
		return nil, err
	}
	polygon, ok := tile.query.FindPolygonByID(node.polygonID)
	if !ok {
		return nil, nil
	}
	out := make([]navPathNode, 0, len(polygon.Neighbors)+4)
	for _, neighborID := range polygon.Neighbors {
		if _, ok := tile.query.FindPolygonByID(neighborID); ok {
			out = append(out, navPathNode{coord: node.coord, polygonID: neighborID})
		}
	}
	for _, dir := range navPathCardinalDirs() {
		neighborCoord := TerrainChunkCoordDef{X: node.coord.X + dir.X, Y: node.coord.Y + dir.Y, Z: node.coord.Z + dir.Z}
		if !navPathCoordInBounds(neighborCoord, bounds) {
			continue
		}
		if !ctx.coordAllowed(neighborCoord) {
			ctx.recordFailure(NavPathFailureDisallowedTile, neighborCoord)
			continue
		}
		neighborTile, err := ctx.loadTile(neighborCoord)
		if err != nil {
			return nil, err
		}
		if neighborTile == nil || neighborTile.empty {
			continue
		}
		for _, candidate := range neighborTile.query.Tile.Polygons {
			if navPolygonsTouchAcrossTileBoundary(tile.query.Tile, polygon, neighborTile.query.Tile, candidate, dir) {
				out = append(out, navPathNode{coord: neighborCoord, polygonID: candidate.ID})
			}
		}
	}
	return out, nil
}

func (ctx *effectiveNavPathContext) buildResult(start navPathNode, end navPathNode, previous map[navPathNode]navPathNode) NavPathResult {
	reversed := []navPathNode{end}
	for current := end; current != start; {
		current = previous[current]
		reversed = append(reversed, current)
	}
	steps := make([]NavPathStep, len(reversed))
	waypoints := make([]Vec3, 0, len(reversed))
	for i := range reversed {
		node := reversed[len(reversed)-1-i]
		tile := ctx.cache[node.coord]
		center, _ := tile.query.PolygonCenter(node.polygonID)
		steps[i] = NavPathStep{Coord: node.coord, PolygonID: node.polygonID, Source: tile.lookup.Source}
		waypoints = append(waypoints, center)
	}
	return NavPathResult{Found: true, Steps: steps, Waypoints: waypoints}
}

func (ctx *effectiveNavPathContext) failureForTile(coord TerrainChunkCoordDef, tile *effectiveNavPathTile) NavPathResult {
	reason := NavPathFailureMissingTile
	if tile != nil && tile.failureReason != "" {
		reason = tile.failureReason
	}
	return NavPathResult{FailureReason: reason, FailureCoord: coord}
}

func (ctx *effectiveNavPathContext) noPathResult() NavPathResult {
	for _, reason := range []string{NavPathFailureDisallowedTile, NavPathFailureDeltaEmptyTile, NavPathFailureMissingTile} {
		for _, failure := range ctx.failures {
			if failure.reason == reason {
				return NavPathResult{FailureReason: reason, FailureCoord: failure.coord}
			}
		}
	}
	return NavPathResult{FailureReason: NavPathFailureNoPath}
}

func (ctx *effectiveNavPathContext) recordFailure(reason string, coord TerrainChunkCoordDef) {
	if reason == "" {
		return
	}
	ctx.failures = append(ctx.failures, navPathFailure{reason: reason, coord: coord})
}

func navPathFailureReasonForLookup(lookup NavTileLookupResult) string {
	if lookup.Empty && lookup.Source == NavTileLookupSourceDelta {
		return NavPathFailureDeltaEmptyTile
	}
	return NavPathFailureMissingTile
}

func navTileCoordForPoint(point Vec3, chunkSize int, voxelResolution float32) (TerrainChunkCoordDef, bool) {
	worldSize := float32(chunkSize) * voxelResolution
	if worldSize <= 0 {
		return TerrainChunkCoordDef{}, false
	}
	return TerrainChunkCoordDef{
		X: int(math.Floor(float64(point[0] / worldSize))),
		Y: int(math.Floor(float64(point[1] / worldSize))),
		Z: int(math.Floor(float64(point[2] / worldSize))),
	}, true
}

func navPathSearchBounds(a TerrainChunkCoordDef, b TerrainChunkCoordDef, radius int) navPathCoordBounds {
	return navPathCoordBounds{
		min: TerrainChunkCoordDef{
			X: minNavInt(a.X, b.X) - radius,
			Y: minNavInt(a.Y, b.Y) - radius,
			Z: minNavInt(a.Z, b.Z) - radius,
		},
		max: TerrainChunkCoordDef{
			X: maxNavInt(a.X, b.X) + radius,
			Y: maxNavInt(a.Y, b.Y) + radius,
			Z: maxNavInt(a.Z, b.Z) + radius,
		},
	}
}

func navPathCoordInBounds(coord TerrainChunkCoordDef, bounds navPathCoordBounds) bool {
	return coord.X >= bounds.min.X && coord.X <= bounds.max.X &&
		coord.Y >= bounds.min.Y && coord.Y <= bounds.max.Y &&
		coord.Z >= bounds.min.Z && coord.Z <= bounds.max.Z
}

func navPathCardinalDirs() []TerrainChunkCoordDef {
	return []TerrainChunkCoordDef{
		{X: 1}, {X: -1},
		{Y: 1}, {Y: -1},
		{Z: 1}, {Z: -1},
	}
}

type navPolygonBounds struct {
	min Vec3
	max Vec3
}

func navPolygonBoundsForTile(tile *NavTileDef, polygon NavPolygonDef) (navPolygonBounds, bool) {
	if tile == nil || len(polygon.Vertices) == 0 {
		return navPolygonBounds{}, false
	}
	var bounds navPolygonBounds
	set := false
	for _, index := range polygon.Vertices {
		if index < 0 || index >= len(tile.Vertices) {
			return navPolygonBounds{}, false
		}
		v := tile.Vertices[index]
		if !set {
			bounds.min = v
			bounds.max = v
			set = true
			continue
		}
		for axis := 0; axis < 3; axis++ {
			if v[axis] < bounds.min[axis] {
				bounds.min[axis] = v[axis]
			}
			if v[axis] > bounds.max[axis] {
				bounds.max[axis] = v[axis]
			}
		}
	}
	return bounds, set
}

func navPolygonsTouchAcrossTileBoundary(tileA *NavTileDef, polygonA NavPolygonDef, tileB *NavTileDef, polygonB NavPolygonDef, dir TerrainChunkCoordDef) bool {
	a, ok := navPolygonBoundsForTile(tileA, polygonA)
	if !ok {
		return false
	}
	b, ok := navPolygonBoundsForTile(tileB, polygonB)
	if !ok {
		return false
	}
	const epsilon = float32(1e-4)
	switch {
	case dir.X == 1:
		return navAlmostEqual(a.max[0], b.min[0], epsilon) && navRangesOverlapOrTouch(a.min[1], a.max[1], b.min[1], b.max[1], epsilon) && navRangesOverlapPositive(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	case dir.X == -1:
		return navAlmostEqual(a.min[0], b.max[0], epsilon) && navRangesOverlapOrTouch(a.min[1], a.max[1], b.min[1], b.max[1], epsilon) && navRangesOverlapPositive(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	case dir.Y == 1:
		return navAlmostEqual(a.max[1], b.min[1], epsilon) && navRangesOverlapPositive(a.min[0], a.max[0], b.min[0], b.max[0], epsilon) && navRangesOverlapPositive(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	case dir.Y == -1:
		return navAlmostEqual(a.min[1], b.max[1], epsilon) && navRangesOverlapPositive(a.min[0], a.max[0], b.min[0], b.max[0], epsilon) && navRangesOverlapPositive(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	case dir.Z == 1:
		return navAlmostEqual(a.max[2], b.min[2], epsilon) && navRangesOverlapPositive(a.min[0], a.max[0], b.min[0], b.max[0], epsilon) && navRangesOverlapOrTouch(a.min[1], a.max[1], b.min[1], b.max[1], epsilon)
	case dir.Z == -1:
		return navAlmostEqual(a.min[2], b.max[2], epsilon) && navRangesOverlapPositive(a.min[0], a.max[0], b.min[0], b.max[0], epsilon) && navRangesOverlapOrTouch(a.min[1], a.max[1], b.min[1], b.max[1], epsilon)
	default:
		return false
	}
}

func navAlmostEqual(a, b, epsilon float32) bool {
	return absNavFloat32(a-b) <= epsilon
}

func navRangesOverlapPositive(aMin, aMax, bMin, bMax, epsilon float32) bool {
	overlap := minNavFloat32(aMax, bMax) - maxNavFloat32(aMin, bMin)
	return overlap > epsilon
}

func navRangesOverlapOrTouch(aMin, aMax, bMin, bMax, epsilon float32) bool {
	overlap := minNavFloat32(aMax, bMax) - maxNavFloat32(aMin, bMin)
	return overlap >= -epsilon
}

func minNavInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxNavInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
