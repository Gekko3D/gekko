package content

import (
	"fmt"
	"math"
	"sort"
)

const (
	NavRouteStartUnsupported = "start_unsupported"
	NavRouteGoalUnsupported  = "goal_unsupported"
	NavRouteNoRoute          = "no_route"
)

type navRouteNode struct {
	Tile   TerrainChunkCoordDef
	Region uint32
}

type navResolvedSpan struct {
	Ref       NavSpanRef
	Region    navRouteNode
	Projected Vec3
}

const navUnassignedRegion = ^uint32(0)

type navLocalEdge struct {
	to       uint32
	movement uint8
}

type navResidentTile struct {
	coord              TerrainChunkCoordDef
	spans              []NavSpanDef
	accepted           []uint64
	spanRegions        []uint32
	localOffsets       []uint32
	localEdges         []navLocalEdge
	exceptionalOffsets []uint32
	exceptional        []NavSpanTransitionDef
	regions            []NavRegionDef
	regionOffsets      []uint32
	regionEdges        []NavRegionTransitionDef
	backing            []NavSpanTransitionDef
	backingSet         []uint64
	columnOffsets      []uint32
	columnSpans        []uint32
	spanScale          float32
	classIDs           []uint32
	walkExits          []uint8
	activeDegrees      []uint8
	sectorEdges        []TerrainChunkCoordDef
}

type navGraphQuery struct {
	chunkSize       int
	voxelResolution float32
	tiles           map[TerrainChunkCoordDef]*navResidentTile
	blocked         map[TerrainChunkCoordDef][]uint64
	spanComponents  map[TerrainChunkCoordDef][]uint32
	componentEdges  map[uint32][]uint32
	componentRoutes map[uint32][]navOverlayEdge
}

type navBackingKey struct {
	fromRegion, toRegion         uint32
	toTile                       TerrainChunkCoordDef
	kind, flags, traversal, gate string
}

type navBackingCandidate struct {
	transition NavRegionTransitionDef
	want       Vec3
}

// NavSnapshot is immutable resident navigation state. Its storage is private so
// published slices and bitsets cannot be mutated by callers.
type NavSnapshot struct {
	query *navGraphQuery
}

// NavGraphQuery reads one immutable resident snapshot. Runtime publication
// replaces this pointer rather than patching resident tiles in place.
type NavGraphQuery struct {
	snapshot *NavSnapshot
}

func NewNavGraphQuery(sources []NavSourceTileDef, graphs []NavGraphTileDef, chunkSize int, voxelResolution float32) (*NavGraphQuery, error) {
	query, err := newNavGraphQuery(sources, graphs, chunkSize, voxelResolution)
	if err != nil {
		return nil, err
	}
	return &NavGraphQuery{snapshot: &NavSnapshot{query: query}}, nil
}

func (q *NavGraphQuery) resident() *navGraphQuery {
	if q == nil || q.snapshot == nil {
		return nil
	}
	return q.snapshot.query
}

func newNavGraphQuery(sources []NavSourceTileDef, graphs []NavGraphTileDef, chunkSize int, voxelResolution float32) (*navGraphQuery, error) {
	if chunkSize <= 0 {
		return nil, fmt.Errorf("navigation graph chunk size must be positive")
	}
	if !finite(voxelResolution) || voxelResolution <= 0 {
		return nil, fmt.Errorf("navigation graph voxel resolution must be finite and positive")
	}
	if len(graphs) == 0 {
		return nil, fmt.Errorf("navigation route requires graph tiles")
	}

	query := &navGraphQuery{
		chunkSize: chunkSize, voxelResolution: voxelResolution,
		tiles: make(map[TerrainChunkCoordDef]*navResidentTile, len(graphs)),
	}
	sourceByCoord := make(map[TerrainChunkCoordDef]NavSourceTileDef, len(sources))
	for _, source := range sources {
		if _, exists := sourceByCoord[source.Coord]; exists {
			return nil, fmt.Errorf("duplicate navigation source tile %s", TerrainChunkKey(source.Coord))
		}
		if validation := ValidateNavSourceTile(&source); validation.HasErrors() {
			return nil, fmt.Errorf("invalid navigation source tile %s: %s", TerrainChunkKey(source.Coord), validation.Error())
		}
		sourceByCoord[source.Coord] = source
	}

	var navID, builderVersion, profileID string
	for _, graph := range graphs {
		if _, exists := query.tiles[graph.Coord]; exists {
			return nil, fmt.Errorf("duplicate navigation graph tile %s", TerrainChunkKey(graph.Coord))
		}
		if validation := ValidateNavGraphTile(&graph); validation.HasErrors() {
			return nil, fmt.Errorf("invalid navigation graph tile %s: %s", TerrainChunkKey(graph.Coord), validation.Error())
		}
		source, exists := sourceByCoord[graph.Coord]
		if !exists {
			return nil, fmt.Errorf("navigation graph tile %s has no source tile", TerrainChunkKey(graph.Coord))
		}
		if source.NavID != graph.NavID || source.BuilderVersion != graph.BuilderVersion || source.SourceHash != graph.SourceHash || source.DependencyHash != graph.DependencyHash {
			return nil, fmt.Errorf("navigation source and graph tile metadata do not match at %s", TerrainChunkKey(graph.Coord))
		}
		if navID == "" {
			navID, builderVersion, profileID = graph.NavID, graph.BuilderVersion, graph.AgentProfileID
		} else if graph.NavID != navID || graph.BuilderVersion != builderVersion || graph.AgentProfileID != profileID {
			return nil, fmt.Errorf("navigation route graph tiles have incompatible metadata")
		}
		tile, err := newNavResidentTile(source, graph, chunkSize, voxelResolution)
		if err != nil {
			return nil, err
		}
		query.tiles[graph.Coord] = tile
	}
	query.indexWalkableTransitions()
	query.indexBackingTransitions()
	query.indexRouteEdges()
	query.indexActiveDegrees()
	return query, nil
}

func newNavResidentTile(source NavSourceTileDef, graph NavGraphTileDef, chunkSize int, voxelResolution float32) (*navResidentTile, error) {
	tile := &navResidentTile{
		coord: graph.Coord, spans: append([]NavSpanDef(nil), source.Spans...),
		accepted:    make([]uint64, (len(source.Spans)+63)/64),
		spanRegions: make([]uint32, len(source.Spans)),
		regions:     append([]NavRegionDef(nil), graph.Regions...),
		classIDs:    make([]uint32, len(source.Spans)), walkExits: make([]uint8, len(source.Spans)),
	}
	for i := range tile.spanRegions {
		tile.spanRegions[i] = navUnassignedRegion
	}
	for _, id := range graph.SpanIDs {
		if int(id) >= len(tile.spans) {
			return nil, fmt.Errorf("navigation graph tile %s references missing source span %d", TerrainChunkKey(graph.Coord), id)
		}
		bitSet(tile.accepted, id)
	}
	for _, region := range graph.Regions {
		for _, run := range region.SpanRuns {
			for id := run.Start; id < run.Start+run.Count; id++ {
				tile.spanRegions[id] = region.ID
			}
		}
	}
	for _, id := range graph.SpanIDs {
		if tile.spanRegions[id] == navUnassignedRegion {
			return nil, fmt.Errorf("navigation graph tile %s is not region-compressed", TerrainChunkKey(graph.Coord))
		}
		span := tile.spans[id]
		if span.X < 0 || span.X >= chunkSize || span.Z < 0 || span.Z >= chunkSize {
			return nil, fmt.Errorf("navigation span %d is outside tile %s", id, TerrainChunkKey(graph.Coord))
		}
	}
	tile.buildColumns(chunkSize)
	tile.buildLocalEdges(graph.SpanTransitions, voxelResolution)
	tile.buildRegionEdges(graph.Transitions)
	return tile, nil
}

func bitSet(words []uint64, id uint32) { words[id/64] |= 1 << (id % 64) }
func bitHas(words []uint64, id uint32) bool {
	return int(id/64) < len(words) && words[id/64]&(1<<(id%64)) != 0
}

func (t *navResidentTile) buildColumns(chunkSize int) {
	counts := make([]uint32, chunkSize*chunkSize)
	for id := range t.spans {
		if bitHas(t.accepted, uint32(id)) {
			span := t.spans[id]
			counts[span.Z*chunkSize+span.X]++
		}
	}
	t.columnOffsets = make([]uint32, len(counts)+1)
	for i, count := range counts {
		t.columnOffsets[i+1] = t.columnOffsets[i] + count
	}
	t.columnSpans = make([]uint32, t.columnOffsets[len(counts)])
	next := append([]uint32(nil), t.columnOffsets[:len(counts)]...)
	for id := range t.spans {
		if bitHas(t.accepted, uint32(id)) {
			span := t.spans[id]
			column := span.Z*chunkSize + span.X
			t.columnSpans[next[column]] = uint32(id)
			next[column]++
		}
	}
}

func (t *navResidentTile) column(x, z, chunkSize int) []uint32 {
	if x < 0 || x >= chunkSize || z < 0 || z >= chunkSize {
		return nil
	}
	column := z*chunkSize + x
	return t.columnSpans[t.columnOffsets[column]:t.columnOffsets[column+1]]
}

func (t *navResidentTile) buildLocalEdges(transitions []NavSpanTransitionDef, voxelResolution float32) {
	counts := make([]uint32, len(t.spans))
	exceptionalCounts := make([]uint32, len(t.spans))
	for _, edge := range transitions {
		if navBinaryOrdinaryLocalEdge(t.coord, edge) {
			counts[edge.From]++
		} else {
			exceptionalCounts[edge.From]++
		}
	}
	t.localOffsets = make([]uint32, len(t.spans)+1)
	for i, count := range counts {
		t.localOffsets[i+1] = t.localOffsets[i] + count
	}
	t.localEdges = make([]navLocalEdge, t.localOffsets[len(t.spans)])
	t.exceptionalOffsets = make([]uint32, len(t.spans)+1)
	for i, count := range exceptionalCounts {
		t.exceptionalOffsets[i+1] = t.exceptionalOffsets[i] + count
	}
	t.exceptional = make([]NavSpanTransitionDef, t.exceptionalOffsets[len(t.spans)])
	next := append([]uint32(nil), t.localOffsets[:len(t.spans)]...)
	exceptionalNext := append([]uint32(nil), t.exceptionalOffsets[:len(t.spans)]...)
	t.spanScale = float32(math.Inf(1))
	for _, edge := range transitions {
		if !navBinaryOrdinaryLocalEdge(t.coord, edge) {
			t.exceptional[exceptionalNext[edge.From]] = cloneNavSpanTransition(edge)
			exceptionalNext[edge.From]++
			continue
		}
		index := next[edge.From]
		t.localEdges[index] = navLocalEdge{to: edge.To.Span, movement: navMovementClass(edge.Kind)}
		next[edge.From]++
		cost := navDerivedLocalCost(t.spans[edge.From], t.spans[edge.To.Span], voxelResolution)
		distance := navSpanSearchDistance(t.spans[edge.From], t.spans[edge.To.Span], voxelResolution)
		if distance > 0 {
			t.spanScale = min(t.spanScale, cost/distance)
		}
	}
	for _, edge := range t.exceptional {
		if edge.To.Tile != t.coord {
			continue
		}
		distance := navSpanSearchDistance(t.spans[edge.From], t.spans[edge.To.Span], voxelResolution)
		if distance > 0 {
			t.spanScale = min(t.spanScale, edge.Cost/distance)
		}
	}
	if !finite(t.spanScale) {
		t.spanScale = 0
	}
}

func (t *navResidentTile) buildRegionEdges(transitions []NavRegionTransitionDef) {
	counts := make([]uint32, len(t.regions))
	for _, edge := range transitions {
		counts[edge.FromRegion]++
	}
	t.regionOffsets = make([]uint32, len(t.regions)+1)
	for i, count := range counts {
		t.regionOffsets[i+1] = t.regionOffsets[i] + count
	}
	t.regionEdges = make([]NavRegionTransitionDef, len(transitions))
	next := append([]uint32(nil), t.regionOffsets[:len(t.regions)]...)
	for _, edge := range transitions {
		t.regionEdges[next[edge.FromRegion]] = cloneNavRegionTransition(edge)
		next[edge.FromRegion]++
	}
	for region := range t.regions {
		edges := t.regionTransitions(uint32(region))
		sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	}
}

func (t *navResidentTile) regionTransitions(region uint32) []NavRegionTransitionDef {
	if int(region)+1 >= len(t.regionOffsets) {
		return nil
	}
	return t.regionEdges[t.regionOffsets[region]:t.regionOffsets[region+1]]
}

func navMovementClass(kind string) uint8 {
	switch kind {
	case NavTransitionStep:
		return 1
	case NavTransitionStair:
		return 2
	default:
		return 0
	}
}

func navMovementKind(class uint8) string {
	if class == 1 {
		return NavTransitionStep
	}
	if class == 2 {
		return NavTransitionStair
	}
	return NavTransitionWalk
}

func navDerivedLocalCost(from, to NavSpanDef, voxelResolution float32) float32 {
	return float32(math.Hypot(float64(voxelResolution), float64(to.SupportHeight-from.SupportHeight)))
}

func cloneNavSpanTransition(edge NavSpanTransitionDef) NavSpanTransitionDef {
	edge.RequiresFlags = append([]string(nil), edge.RequiresFlags...)
	edge.Traversal = cloneNavTraversal(edge.Traversal)
	edge.Gate = cloneNavTransitionGate(edge.Gate)
	return edge
}

func cloneNavRegionTransition(edge NavRegionTransitionDef) NavRegionTransitionDef {
	edge.RequiresFlags = append([]string(nil), edge.RequiresFlags...)
	edge.Traversal = cloneNavTraversal(edge.Traversal)
	edge.Gate = cloneNavTransitionGate(edge.Gate)
	return edge
}

func (q *navGraphQuery) indexRouteEdges() {
	for coord, tile := range q.tiles {
		seen := make(map[TerrainChunkCoordDef]struct{})
		for _, transition := range tile.regionEdges {
			if transition.ToTile == coord {
				continue
			}
			if _, loaded := q.tiles[transition.ToTile]; loaded {
				seen[transition.ToTile] = struct{}{}
			}
		}
		for next := range seen {
			tile.sectorEdges = append(tile.sectorEdges, next)
		}
		sort.Slice(tile.sectorEdges, func(i, j int) bool {
			return terrainCoordLess(tile.sectorEdges[i], tile.sectorEdges[j])
		})
	}
}

func (q *navGraphQuery) indexWalkableTransitions() {
	classSet := make(map[string]struct{})
	for _, tile := range q.tiles {
		for id, span := range tile.spans {
			if !bitHas(tile.accepted, uint32(id)) {
				continue
			}
			classSet[navRegionSpanClass(span)] = struct{}{}
		}
	}
	classKeys := make([]string, 0, len(classSet))
	for key := range classSet {
		classKeys = append(classKeys, key)
	}
	sort.Strings(classKeys)
	classes := make(map[string]uint32, len(classKeys))
	for i, key := range classKeys {
		classes[key] = uint32(i + 1)
	}
	for _, tile := range q.tiles {
		for id, span := range tile.spans {
			if bitHas(tile.accepted, uint32(id)) {
				tile.classIDs[id] = classes[navRegionSpanClass(span)]
			}
		}
	}
	for _, tile := range q.tiles {
		for from := range tile.spans {
			q.visitSpanEdges(NavSpanRef{Tile: tile.coord, Span: uint32(from)}, func(edge NavSpanTransitionDef) {
				if edge.Kind != NavTransitionWalk {
					return
				}
				fromSpan, fromOK := q.span(NavSpanRef{Tile: tile.coord, Span: edge.From})
				toSpan, toOK := q.span(edge.To)
				if !fromOK || !toOK || math.Float32bits(fromSpan.SupportHeight) != math.Float32bits(toSpan.SupportHeight) {
					return
				}
				fromX, fromZ := tile.coord.X*q.chunkSize+fromSpan.X, tile.coord.Z*q.chunkSize+fromSpan.Z
				toX, toZ := edge.To.Tile.X*q.chunkSize+toSpan.X, edge.To.Tile.Z*q.chunkSize+toSpan.Z
				tile.walkExits[from] |= navWalkableExit(toX-fromX, toZ-fromZ)
			})
		}
	}
}

func (q *navGraphQuery) indexBackingTransitions() {
	for coord, tile := range q.tiles {
		candidates := make(map[navBackingKey][]navBackingCandidate)
		for _, transition := range tile.regionEdges {
			want := midpointVec3(transition.CrossingStart, transition.CrossingEnd)
			traversalID := ""
			if transition.Traversal != nil {
				traversalID = transition.Traversal.StableLinkID()
				want = midpointVec3(transition.Traversal.Start, transition.Traversal.End)
			} else if transition.ToTile == coord {
				want[0] += float32(coord.X*q.chunkSize) * q.voxelResolution
				want[2] += float32(coord.Z*q.chunkSize) * q.voxelResolution
			}
			gateID := ""
			if transition.Gate != nil {
				gateID = transition.Gate.Kind + "\x00" + transition.Gate.ID
			}
			key := navBackingKey{transition.FromRegion, transition.ToRegion, transition.ToTile, transition.Kind, navQueryFlagsKey(transition.RequiresFlags), traversalID, gateID}
			candidates[key] = append(candidates[key], navBackingCandidate{transition, want})
		}
		tile.backing = make([]NavSpanTransitionDef, len(tile.regionEdges))
		tile.backingSet = make([]uint64, (len(tile.regionEdges)+63)/64)
		distances := make([]float32, len(tile.regionEdges))
		for from := range tile.spans {
			q.visitSpanEdges(NavSpanRef{Tile: coord, Span: uint32(from)}, func(edge NavSpanTransitionDef) {
				fromRegion, fromOK := q.spanRegion(NavSpanRef{Tile: coord, Span: edge.From})
				toRegion, toOK := q.spanRegion(edge.To)
				if !fromOK || !toOK {
					return
				}
				traversalID := ""
				if edge.Traversal != nil {
					traversalID = edge.Traversal.StableLinkID()
				}
				gateID := ""
				if edge.Gate != nil {
					gateID = edge.Gate.Kind + "\x00" + edge.Gate.ID
				}
				key := navBackingKey{fromRegion, toRegion, edge.To.Tile, edge.Kind, navQueryFlagsKey(edge.RequiresFlags), traversalID, gateID}
				matches := candidates[key]
				if len(matches) == 0 {
					return
				}
				point := q.spanTransitionTarget(coord, edge)
				if edge.Traversal != nil {
					point = midpointVec3(edge.Traversal.Start, edge.Traversal.End)
				}
				for _, candidate := range matches {
					distance := navVec3Distance(point, candidate.want)
					previous, found := tile.backing[candidate.transition.ID], bitHas(tile.backingSet, candidate.transition.ID)
					if found && (distance > distances[candidate.transition.ID] || distance == distances[candidate.transition.ID] && !navBackingEdgeLess(edge, previous)) {
						continue
					}
					tile.backing[candidate.transition.ID], distances[candidate.transition.ID] = edge, distance
					bitSet(tile.backingSet, candidate.transition.ID)
				}
			})
		}
	}
}

func navQueryFlagsKey(flags []string) string {
	if len(flags) == 0 {
		return ""
	}
	return navRegionFlagsKey(flags)
}

func navBackingEdgeLess(a, b NavSpanTransitionDef) bool {
	return a.From < b.From || a.From == b.From && (terrainCoordLess(a.To.Tile, b.To.Tile) || a.To.Tile == b.To.Tile && a.To.Span < b.To.Span)
}

func (q *navGraphQuery) tile(coord TerrainChunkCoordDef) *navResidentTile { return q.tiles[coord] }

func (q *navGraphQuery) span(ref NavSpanRef) (NavSpanDef, bool) {
	tile := q.tile(ref.Tile)
	if tile == nil || int(ref.Span) >= len(tile.spans) || !bitHas(tile.accepted, ref.Span) {
		return NavSpanDef{}, false
	}
	return tile.spans[ref.Span], true
}

func (q *navGraphQuery) spanRegion(ref NavSpanRef) (uint32, bool) {
	tile := q.tile(ref.Tile)
	if tile == nil || int(ref.Span) >= len(tile.spanRegions) || tile.spanRegions[ref.Span] == navUnassignedRegion {
		return 0, false
	}
	return tile.spanRegions[ref.Span], true
}

func (q *navGraphQuery) visitSpanEdges(from NavSpanRef, visit func(NavSpanTransitionDef)) {
	tile := q.tile(from.Tile)
	if tile == nil || int(from.Span)+1 >= len(tile.localOffsets) {
		return
	}
	local := tile.localEdges[tile.localOffsets[from.Span]:tile.localOffsets[from.Span+1]]
	for _, edge := range local {
		to := tile.spans[edge.to]
		fromSpan := tile.spans[from.Span]
		visit(NavSpanTransitionDef{
			From: from.Span, To: NavSpanRef{Tile: from.Tile, Span: edge.to}, Kind: navMovementKind(edge.movement),
			StepDelta: to.SupportHeight - fromSpan.SupportHeight, Width: q.voxelResolution,
			MinHeadroom:  min(fromSpan.CeilingHeight, to.CeilingHeight) - max(fromSpan.SupportHeight, to.SupportHeight),
			MinClearance: min(fromSpan.ClearanceRadius, to.ClearanceRadius), Cost: navDerivedLocalCost(fromSpan, to, q.voxelResolution),
		})
	}
	for _, edge := range tile.exceptional[tile.exceptionalOffsets[from.Span]:tile.exceptionalOffsets[from.Span+1]] {
		visit(edge)
	}
}

func (q *navGraphQuery) columnSpans(cellX, cellY, cellZ, verticalTiles int) []NavSpanRef {
	tileX, tileY, tileZ := floorDivNavSpan(cellX, q.chunkSize), floorDivNavSpan(cellY, q.chunkSize), floorDivNavSpan(cellZ, q.chunkSize)
	localX, localZ := cellX-tileX*q.chunkSize, cellZ-tileZ*q.chunkSize
	var refs []NavSpanRef
	for y := tileY - verticalTiles; y <= tileY+verticalTiles; y++ {
		coord := TerrainChunkCoordDef{X: tileX, Y: y, Z: tileZ}
		if tile := q.tile(coord); tile != nil {
			for _, id := range tile.column(localX, localZ, q.chunkSize) {
				refs = append(refs, NavSpanRef{Tile: coord, Span: id})
			}
		}
	}
	return refs
}

func (q *navGraphQuery) resolve(point Vec3) (navResolvedSpan, TerrainChunkCoordDef, bool) {
	cellX := int(math.Floor(float64(point[0]) / float64(q.voxelResolution)))
	cellY := int(math.Floor(float64(point[1]) / float64(q.voxelResolution)))
	cellZ := int(math.Floor(float64(point[2]) / float64(q.voxelResolution)))
	tile := TerrainChunkCoordDef{
		X: floorDivNavSpan(cellX, q.chunkSize),
		Y: floorDivNavSpan(cellY, q.chunkSize),
		Z: floorDivNavSpan(cellZ, q.chunkSize),
	}
	bestDistance := float32(math.Inf(1))
	var best navResolvedSpan
	found := false
	for _, ref := range q.columnSpans(cellX, cellY, cellZ, 1) {
		if q.isBlocked(ref) {
			continue
		}
		span, _ := q.span(ref)
		distance := absFloat32(span.SupportHeight - point[1])
		if distance > q.voxelResolution+1e-4 {
			continue
		}
		if found && (distance > bestDistance || distance == bestDistance && !navSpanRefLess(ref, best.Ref)) {
			continue
		}
		bestDistance = distance
		best = navResolvedSpan{
			Ref:       ref,
			Region:    navRouteNode{Tile: ref.Tile, Region: q.tile(ref.Tile).spanRegions[ref.Span]},
			Projected: Vec3{point[0], span.SupportHeight, point[2]},
		}
		found = true
	}
	return best, tile, found
}

func (q *navGraphQuery) resolveNearby(point Vec3, maxDistance float32) (navResolvedSpan, bool) {
	cellX := int(math.Floor(float64(point[0]) / float64(q.voxelResolution)))
	cellY := int(math.Floor(float64(point[1]) / float64(q.voxelResolution)))
	cellZ := int(math.Floor(float64(point[2]) / float64(q.voxelResolution)))
	radius := int(math.Ceil(float64(maxDistance / q.voxelResolution)))
	verticalTiles := radius/q.chunkSize + 1
	maxDistanceSqr := maxDistance * maxDistance
	bestDistance := float32(math.Inf(1))
	var best navResolvedSpan
	found := false
	for dz := -radius; dz <= radius; dz++ {
		for dx := -radius; dx <= radius; dx++ {
			for _, ref := range q.columnSpans(cellX+dx, cellY, cellZ+dz, verticalTiles) {
				if q.isBlocked(ref) {
					continue
				}
				span, _ := q.span(ref)
				minX := float32(ref.Tile.X*q.chunkSize+span.X) * q.voxelResolution
				minZ := float32(ref.Tile.Z*q.chunkSize+span.Z) * q.voxelResolution
				projected := Vec3{
					navClampToSpanAxis(point[0], minX, q.voxelResolution),
					span.SupportHeight,
					navClampToSpanAxis(point[2], minZ, q.voxelResolution),
				}
				offsetX, offsetZ := point[0]-projected[0], point[2]-projected[2]
				offsetY := point[1] - projected[1]
				if offsetX*offsetX+offsetY*offsetY+offsetZ*offsetZ > maxDistanceSqr+1e-4 {
					continue
				}
				distance := navVec3Distance(point, projected)
				if found && (distance > bestDistance || distance == bestDistance && !navSpanRefLess(ref, best.Ref)) {
					continue
				}
				bestDistance = distance
				best = navResolvedSpan{
					Ref:       ref,
					Region:    navRouteNode{Tile: ref.Tile, Region: q.tile(ref.Tile).spanRegions[ref.Span]},
					Projected: projected,
				}
				found = true
			}
		}
	}
	return best, found
}

func (q *NavGraphQuery) locate(point Vec3) (navResolvedSpan, TerrainChunkCoordDef, bool) {
	query := q.resident()
	result, tile, found := query.resolve(point)
	if !found {
		result, found = query.resolveNearby(point, query.voxelResolution)
	}
	return result, tile, found
}

func navPointResult(resolved navResolvedSpan) NavPointResult {
	return NavPointResult{
		Found: true, Ref: resolved.Ref, Region: resolved.Region.Region,
		Point: resolved.Projected, Distance: 0,
	}
}

// FindNavGraphLocation binds a world point to its canonical resident span.
// Localization repairs at most one voxel of 3D drift.
func FindNavGraphLocation(sources []NavSourceTileDef, graphs []NavGraphTileDef, chunkSize int, voxelResolution float32, point Vec3) (NavPointResult, error) {
	query, err := NewNavGraphQuery(sources, graphs, chunkSize, voxelResolution)
	if err != nil {
		return NavPointResult{}, err
	}
	return query.Locate(point)
}

// Locate binds a world point to its canonical resident span. Route starts and
// actor navigation state must use this instead of caller-specific projection.
func (q *NavGraphQuery) Locate(point Vec3) (NavPointResult, error) {
	if !validVec3(point) {
		return NavPointResult{}, fmt.Errorf("navigation point must be finite")
	}
	if q == nil || q.resident() == nil {
		return NavPointResult{}, fmt.Errorf("navigation graph query is required")
	}
	resolved, _, found := q.locate(point)
	if !found {
		return NavPointResult{}, nil
	}
	result := navPointResult(resolved)
	result.Distance = navVec3Distance(point, resolved.Projected)
	return result, nil
}

// FindNearestNavGraphPoint projects a world point onto the nearest supported
// span in the loaded graph, within maxDistance.
func FindNearestNavGraphPoint(sources []NavSourceTileDef, graphs []NavGraphTileDef, chunkSize int, voxelResolution float32, point Vec3, maxDistance float32) (NavPointResult, error) {
	query, err := NewNavGraphQuery(sources, graphs, chunkSize, voxelResolution)
	if err != nil {
		return NavPointResult{}, err
	}
	return query.ProjectPoint(point, maxDistance)
}

func (q *NavGraphQuery) ProjectPoint(point Vec3, maxDistance float32) (NavPointResult, error) {
	if !validVec3(point) || !finite(maxDistance) || maxDistance < 0 {
		return NavPointResult{}, fmt.Errorf("navigation point and max distance must be finite, with non-negative max distance")
	}
	if q == nil || q.resident() == nil {
		return NavPointResult{}, fmt.Errorf("navigation graph query is required")
	}
	resolved, found := q.resident().resolveNearby(point, maxDistance)
	if !found {
		return NavPointResult{}, nil
	}
	result := navPointResult(resolved)
	result.Distance = navVec3Distance(point, resolved.Projected)
	return result, nil
}

// IsSpanBlocked reports whether a runtime blocker removed a baked span.
func (q *NavGraphQuery) IsSpanBlocked(ref NavSpanRef) bool {
	if q == nil || q.resident() == nil {
		return false
	}
	return q.resident().isBlocked(ref)
}

// IsSpanActive reports whether a span belongs to the resident graph and is not
// removed by the runtime overlay.
func (q *NavGraphQuery) IsSpanActive(ref NavSpanRef) bool {
	if q == nil || q.resident() == nil {
		return false
	}
	query := q.resident()
	if _, accepted := query.spanRegion(ref); !accepted {
		return false
	}
	return !query.isBlocked(ref)
}

// ReachableSpans returns target spans reachable from start through the active
// traversal and blocker overlay. Unknown, blocked, and unreachable targets are
// omitted. Values are active outgoing span counts, capped at two.
func (q *NavGraphQuery) ReachableSpans(start NavSpanRef, targets []NavSpanRef) map[NavSpanRef]uint8 {
	result := make(map[NavSpanRef]uint8, len(targets))
	if q == nil || q.resident() == nil || len(targets) == 0 {
		return result
	}
	query := q.resident()
	startRegion, ok := query.spanRegion(start)
	if !ok {
		return result
	}
	if query.isBlocked(start) {
		return result
	}
	wanted := make(map[NavSpanRef]struct{}, len(targets))
	for _, target := range targets {
		if _, ok := query.spanRegion(target); !ok {
			continue
		}
		if !query.isBlocked(target) {
			wanted[target] = struct{}{}
		}
	}
	if len(query.blocked) == 0 {
		wantedRegions := make(map[navRouteNode]struct{}, len(wanted))
		for target := range wanted {
			region, _ := query.spanRegion(target)
			wantedRegions[navRouteNode{Tile: target.Tile, Region: region}] = struct{}{}
		}
		reachable := map[navRouteNode]struct{}{{Tile: start.Tile, Region: startRegion}: {}}
		queue := []navRouteNode{{Tile: start.Tile, Region: startRegion}}
		found := make(map[navRouteNode]struct{}, len(wantedRegions))
		for head := 0; head < len(queue) && len(found) < len(wantedRegions); head++ {
			current := queue[head]
			if _, wanted := wantedRegions[current]; wanted {
				found[current] = struct{}{}
			}
			for _, transition := range query.regionTransitions(current) {
				next := navRouteNode{Tile: transition.ToTile, Region: transition.ToRegion}
				if _, seen := reachable[next]; seen || !query.hasRegion(next) {
					continue
				}
				reachable[next] = struct{}{}
				queue = append(queue, next)
			}
		}
		for target := range wanted {
			regionID, _ := query.spanRegion(target)
			region := navRouteNode{Tile: target.Tile, Region: regionID}
			if _, ok := reachable[region]; ok {
				result[target] = query.spanExitCount(target)
			}
		}
		return result
	}

	startComponent, ok := query.spanComponent(start)
	if !ok {
		return result
	}
	wantedComponents := make(map[uint32]struct{}, len(wanted))
	for target := range wanted {
		if component, ok := query.spanComponent(target); ok {
			wantedComponents[component] = struct{}{}
		}
	}
	reachable := map[uint32]struct{}{startComponent: {}}
	found := make(map[uint32]struct{}, len(wantedComponents))
	queue := []uint32{startComponent}
	for head := 0; head < len(queue) && len(found) < len(wantedComponents); head++ {
		current := queue[head]
		if _, wanted := wantedComponents[current]; wanted {
			found[current] = struct{}{}
		}
		for _, next := range query.componentEdges[current] {
			if _, seen := reachable[next]; seen {
				continue
			}
			reachable[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	for target := range wanted {
		if component, ok := query.spanComponent(target); ok {
			if _, ok := reachable[component]; ok {
				result[target] = query.spanExitCount(target)
			}
		}
	}
	return result
}

func (q *navGraphQuery) indexActiveDegrees() {
	for coord, tile := range q.tiles {
		degrees := make([]uint8, len(tile.spans))
		for from := range tile.spans {
			q.visitSpanEdges(NavSpanRef{Tile: coord, Span: uint32(from)}, func(edge NavSpanTransitionDef) {
				from := NavSpanRef{Tile: coord, Span: edge.From}
				if q.isBlocked(from) {
					return
				}
				if q.isBlocked(edge.To) {
					return
				}
				if int(edge.From) < len(degrees) && degrees[edge.From] < 2 {
					degrees[edge.From]++
				}
			})
		}
		tile.activeDegrees = degrees
	}
}

func (q *navGraphQuery) spanExitCount(ref NavSpanRef) uint8 {
	tile := q.tile(ref.Tile)
	if tile == nil {
		return 0
	}
	degrees := tile.activeDegrees
	if int(ref.Span) >= len(degrees) {
		return 0
	}
	return degrees[ref.Span]
}

func (q *navGraphQuery) regionTransitions(node navRouteNode) []NavRegionTransitionDef {
	tile := q.tile(node.Tile)
	if tile == nil {
		return nil
	}
	return tile.regionTransitions(node.Region)
}

func navClampToSpanAxis(value, minimum, size float32) float32 {
	return min(max(value, math.Nextafter32(minimum, minimum+size)), math.Nextafter32(minimum+size, minimum))
}

func navSpanRefLess(a, b NavSpanRef) bool {
	return terrainCoordLess(a.Tile, b.Tile) || a.Tile == b.Tile && a.Span < b.Span
}

func (q *navGraphQuery) spanCenter(ref NavSpanRef) Vec3 {
	span, _ := q.span(ref)
	return Vec3{
		(float32(ref.Tile.X*q.chunkSize+span.X) + 0.5) * q.voxelResolution,
		span.SupportHeight,
		(float32(ref.Tile.Z*q.chunkSize+span.Z) + 0.5) * q.voxelResolution,
	}
}

func (q *navGraphQuery) regionCenter(node navRouteNode) Vec3 {
	center := q.tile(node.Tile).regions[node.Region].Center
	center[0] += float32(node.Tile.X*q.chunkSize) * q.voxelResolution
	center[2] += float32(node.Tile.Z*q.chunkSize) * q.voxelResolution
	return center
}

func navVec3Distance(a, b Vec3) float32 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
}

func navRouteNodeLess(a, b navRouteNode) bool {
	if a.Tile != b.Tile {
		return terrainCoordLess(a.Tile, b.Tile)
	}
	return a.Region < b.Region
}

func absFloat32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
