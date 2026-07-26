package content

import (
	"fmt"
	"math"
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

type navGraphQuery struct {
	chunkSize       int
	voxelResolution float32
	sources         map[TerrainChunkCoordDef]NavSourceTileDef
	graphs          map[TerrainChunkCoordDef]NavGraphTileDef
	spans           map[TerrainChunkCoordDef]map[uint32]NavSpanDef
	spanRegions     map[TerrainChunkCoordDef]map[uint32]uint32
	spanEdges       map[TerrainChunkCoordDef]map[uint32][]navSpanSearchEdge
	spanScale       map[TerrainChunkCoordDef]float32
	backing         map[TerrainChunkCoordDef]map[uint32]NavSpanTransitionDef
	walkableCells   map[navSpanPathCell]navWalkableCell
	spanClassIDs    map[string]uint32
	blocked         map[NavSpanRef]struct{}
	globalEdges     map[NavSpanRef][]NavSpanTransitionDef
	globalScale     float32
	spanComponents  map[TerrainChunkCoordDef][]uint32
	componentEdges  map[uint32][]uint32
	activeDegrees   map[TerrainChunkCoordDef][]uint8
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

// NavGraphQuery is an immutable, reusable index over resident graph tiles.
type NavGraphQuery struct {
	query *navGraphQuery
}

func NewNavGraphQuery(sources []NavSourceTileDef, graphs []NavGraphTileDef, chunkSize int, voxelResolution float32) (*NavGraphQuery, error) {
	query, err := newNavGraphQuery(sources, graphs, chunkSize, voxelResolution)
	if err != nil {
		return nil, err
	}
	return &NavGraphQuery{query: query}, nil
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

	acceptedSpans := 0
	for _, graph := range graphs {
		acceptedSpans += len(graph.SpanIDs)
	}
	query := &navGraphQuery{
		chunkSize: chunkSize, voxelResolution: voxelResolution,
		sources:       make(map[TerrainChunkCoordDef]NavSourceTileDef, len(sources)),
		graphs:        make(map[TerrainChunkCoordDef]NavGraphTileDef, len(graphs)),
		spans:         make(map[TerrainChunkCoordDef]map[uint32]NavSpanDef, len(sources)),
		spanRegions:   make(map[TerrainChunkCoordDef]map[uint32]uint32, len(graphs)),
		spanEdges:     make(map[TerrainChunkCoordDef]map[uint32][]navSpanSearchEdge, len(graphs)),
		spanScale:     make(map[TerrainChunkCoordDef]float32, len(graphs)),
		backing:       make(map[TerrainChunkCoordDef]map[uint32]NavSpanTransitionDef, len(graphs)),
		walkableCells: make(map[navSpanPathCell]navWalkableCell, acceptedSpans),
		spanClassIDs:  make(map[string]uint32),
	}
	for _, source := range sources {
		if _, exists := query.sources[source.Coord]; exists {
			return nil, fmt.Errorf("duplicate navigation source tile %s", TerrainChunkKey(source.Coord))
		}
		if validation := ValidateNavSourceTile(&source); validation.HasErrors() {
			return nil, fmt.Errorf("invalid navigation source tile %s: %s", TerrainChunkKey(source.Coord), validation.Error())
		}
		query.sources[source.Coord] = source
		query.spans[source.Coord] = make(map[uint32]NavSpanDef, len(source.Spans))
		for _, span := range source.Spans {
			query.spans[source.Coord][span.ID] = span
		}
	}

	var navID, builderVersion, profileID string
	for _, graph := range graphs {
		if _, exists := query.graphs[graph.Coord]; exists {
			return nil, fmt.Errorf("duplicate navigation graph tile %s", TerrainChunkKey(graph.Coord))
		}
		if validation := ValidateNavGraphTile(&graph); validation.HasErrors() {
			return nil, fmt.Errorf("invalid navigation graph tile %s: %s", TerrainChunkKey(graph.Coord), validation.Error())
		}
		source, exists := query.sources[graph.Coord]
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
		regions := navGraphSpanRegions(graph)
		if len(regions) != len(graph.SpanIDs) {
			return nil, fmt.Errorf("navigation graph tile %s is not region-compressed", TerrainChunkKey(graph.Coord))
		}
		for _, spanID := range graph.SpanIDs {
			span, exists := query.spans[graph.Coord][spanID]
			if !exists {
				return nil, fmt.Errorf("navigation graph tile %s references missing source span %d", TerrainChunkKey(graph.Coord), spanID)
			}
			query.indexWalkableCell(graph.Coord, span)
		}
		query.graphs[graph.Coord] = graph
		query.spanRegions[graph.Coord] = regions
		edges := make(map[uint32][]navSpanSearchEdge)
		scale := float32(math.Inf(1))
		for _, transition := range graph.SpanTransitions {
			query.indexWalkableTransition(graph.Coord, transition)
			if transition.To.Tile != graph.Coord {
				continue
			}
			edges[transition.From] = append(edges[transition.From], navSpanSearchEdge{to: transition.To.Span, cost: transition.Cost})
			distance := navSpanSearchDistance(query.spans[graph.Coord][transition.From], query.spans[graph.Coord][transition.To.Span], voxelResolution)
			if distance > 0 {
				scale = min(scale, transition.Cost/distance)
			}
		}
		if !finite(scale) {
			scale = 0
		}
		query.spanEdges[graph.Coord], query.spanScale[graph.Coord] = edges, scale
	}
	query.indexBackingTransitions()
	query.indexActiveDegrees()
	return query, nil
}

func (q *navGraphQuery) indexWalkableCell(coord TerrainChunkCoordDef, span NavSpanDef) {
	classKey := navRegionSpanClass(span)
	classID := q.spanClassIDs[classKey]
	if classID == 0 {
		classID = uint32(len(q.spanClassIDs) + 1)
		q.spanClassIDs[classKey] = classID
	}
	cell := navSpanPathCell{
		x:      coord.X*q.chunkSize + span.X,
		z:      coord.Z*q.chunkSize + span.Z,
		height: math.Float32bits(span.SupportHeight),
	}
	entry := q.walkableCells[cell]
	if entry.classID != 0 && entry.classID != classID {
		q.walkableCells[cell] = navWalkableCell{}
		return
	}
	entry.classID = classID
	q.walkableCells[cell] = entry
}

func (q *navGraphQuery) indexWalkableTransition(fromTile TerrainChunkCoordDef, transition NavSpanTransitionDef) {
	if transition.Kind != NavTransitionWalk {
		return
	}
	from := q.spans[fromTile][transition.From]
	to := q.spans[transition.To.Tile][transition.To.Span]
	if math.Float32bits(from.SupportHeight) != math.Float32bits(to.SupportHeight) {
		return
	}
	fromX, fromZ := fromTile.X*q.chunkSize+from.X, fromTile.Z*q.chunkSize+from.Z
	toX, toZ := transition.To.Tile.X*q.chunkSize+to.X, transition.To.Tile.Z*q.chunkSize+to.Z
	exit := navWalkableExit(toX-fromX, toZ-fromZ)
	if exit == 0 {
		return
	}
	cell := navSpanPathCell{x: fromX, z: fromZ, height: math.Float32bits(from.SupportHeight)}
	entry := q.walkableCells[cell]
	entry.exits |= exit
	q.walkableCells[cell] = entry
}

func (q *navGraphQuery) indexBackingTransitions() {
	for coord, graph := range q.graphs {
		candidates := make(map[navBackingKey][]navBackingCandidate)
		for _, transition := range graph.Transitions {
			want := midpointVec3(transition.CrossingStart, transition.CrossingEnd)
			traversalID := ""
			if transition.Traversal != nil {
				traversalID = transition.Traversal.ID
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
		best := make(map[uint32]NavSpanTransitionDef, len(graph.Transitions))
		distances := make(map[uint32]float32, len(graph.Transitions))
		for _, edge := range graph.SpanTransitions {
			fromRegion, fromOK := q.spanRegions[coord][edge.From]
			toRegions, tileOK := q.spanRegions[edge.To.Tile]
			toRegion, toOK := toRegions[edge.To.Span]
			if !fromOK || !tileOK || !toOK {
				continue
			}
			traversalID := ""
			if edge.Traversal != nil {
				traversalID = edge.Traversal.ID
			}
			gateID := ""
			if edge.Gate != nil {
				gateID = edge.Gate.Kind + "\x00" + edge.Gate.ID
			}
			key := navBackingKey{fromRegion, toRegion, edge.To.Tile, edge.Kind, navQueryFlagsKey(edge.RequiresFlags), traversalID, gateID}
			matches := candidates[key]
			if len(matches) == 0 {
				continue
			}
			point := q.spanTransitionTarget(coord, edge)
			if edge.Traversal != nil {
				point = midpointVec3(edge.Traversal.Start, edge.Traversal.End)
			}
			for _, candidate := range matches {
				distance := navVec3Distance(point, candidate.want)
				previous, found := best[candidate.transition.ID]
				if found && (distance > distances[candidate.transition.ID] || distance == distances[candidate.transition.ID] && !navBackingEdgeLess(edge, previous)) {
					continue
				}
				best[candidate.transition.ID], distances[candidate.transition.ID] = edge, distance
			}
		}
		q.backing[coord] = best
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

func (q *navGraphQuery) resolve(point Vec3) (navResolvedSpan, TerrainChunkCoordDef, bool) {
	cellX := int(math.Floor(float64(point[0]) / float64(q.voxelResolution)))
	cellY := int(math.Floor(float64(point[1]) / float64(q.voxelResolution)))
	cellZ := int(math.Floor(float64(point[2]) / float64(q.voxelResolution)))
	tile := TerrainChunkCoordDef{
		X: floorDivNavSpan(cellX, q.chunkSize),
		Y: floorDivNavSpan(cellY, q.chunkSize),
		Z: floorDivNavSpan(cellZ, q.chunkSize),
	}
	localX, localZ := positiveModNavSpan(cellX, q.chunkSize), positiveModNavSpan(cellZ, q.chunkSize)

	bestDistance := float32(math.Inf(1))
	var best navResolvedSpan
	found := false
	for coord, graph := range q.graphs {
		if coord.X != tile.X || coord.Z != tile.Z {
			continue
		}
		for _, spanID := range graph.SpanIDs {
			ref := NavSpanRef{Tile: coord, Span: spanID}
			if _, blocked := q.blocked[ref]; blocked {
				continue
			}
			span := q.spans[coord][spanID]
			if span.X != localX || span.Z != localZ {
				continue
			}
			distance := absFloat32(span.SupportHeight - point[1])
			better := !found || distance < bestDistance || distance == bestDistance && (terrainCoordLess(coord, best.Ref.Tile) || coord == best.Ref.Tile && spanID < best.Ref.Span)
			if !better {
				continue
			}
			bestDistance = distance
			best = navResolvedSpan{
				Ref:       NavSpanRef{Tile: coord, Span: spanID},
				Region:    navRouteNode{Tile: coord, Region: q.spanRegions[coord][spanID]},
				Projected: Vec3{point[0], span.SupportHeight, point[2]},
			}
			found = true
		}
	}
	return best, tile, found
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
	if q == nil || q.query == nil {
		return NavPointResult{}, fmt.Errorf("navigation graph query is required")
	}
	// ponytail: linear resident-span scan; add a spatial index only if point
	// projection becomes a measured hot path outside cursor/debug commands.
	best := NavPointResult{Distance: float32(math.Inf(1))}
	for coord, graph := range q.query.graphs {
		for _, spanID := range graph.SpanIDs {
			ref := NavSpanRef{Tile: coord, Span: spanID}
			if _, blocked := q.query.blocked[ref]; blocked {
				continue
			}
			span := q.query.spans[coord][spanID]
			minX := float32(coord.X*q.query.chunkSize+span.X) * q.query.voxelResolution
			minZ := float32(coord.Z*q.query.chunkSize+span.Z) * q.query.voxelResolution
			projected := Vec3{
				navClampToSpanAxis(point[0], minX, q.query.voxelResolution),
				span.SupportHeight,
				navClampToSpanAxis(point[2], minZ, q.query.voxelResolution),
			}
			distance := navVec3Distance(point, projected)
			if distance > maxDistance || best.Found && (distance > best.Distance || distance == best.Distance && !navSpanRefLess(ref, best.Ref)) {
				continue
			}
			best = NavPointResult{Found: true, Ref: ref, Region: q.query.spanRegions[coord][spanID], Point: projected, Distance: distance}
		}
	}
	return best, nil
}

// IsSpanBlocked reports whether a runtime blocker removed a baked span.
func (q *NavGraphQuery) IsSpanBlocked(ref NavSpanRef) bool {
	if q == nil || q.query == nil {
		return false
	}
	_, blocked := q.query.blocked[ref]
	return blocked
}

// IsSpanActive reports whether a span belongs to the resident graph and is not
// removed by the runtime overlay.
func (q *NavGraphQuery) IsSpanActive(ref NavSpanRef) bool {
	if q == nil || q.query == nil {
		return false
	}
	if _, accepted := q.query.spanRegions[ref.Tile][ref.Span]; !accepted {
		return false
	}
	_, blocked := q.query.blocked[ref]
	return !blocked
}

// ReachableSpans returns target spans reachable from start through the active
// traversal and blocker overlay. Unknown, blocked, and unreachable targets are
// omitted. Values are active outgoing span counts, capped at two.
func (q *NavGraphQuery) ReachableSpans(start NavSpanRef, targets []NavSpanRef) map[NavSpanRef]uint8 {
	result := make(map[NavSpanRef]uint8, len(targets))
	if q == nil || q.query == nil || len(targets) == 0 {
		return result
	}
	query := q.query
	startRegion, ok := query.spanRegions[start.Tile][start.Span]
	if !ok {
		return result
	}
	if _, blocked := query.blocked[start]; blocked {
		return result
	}
	wanted := make(map[NavSpanRef]struct{}, len(targets))
	for _, target := range targets {
		if _, ok := query.spanRegions[target.Tile][target.Span]; !ok {
			continue
		}
		if _, blocked := query.blocked[target]; !blocked {
			wanted[target] = struct{}{}
		}
	}
	if len(query.blocked) == 0 {
		reachable := map[navRouteNode]struct{}{{Tile: start.Tile, Region: startRegion}: {}}
		queue := []navRouteNode{{Tile: start.Tile, Region: startRegion}}
		for head := 0; head < len(queue); head++ {
			current := queue[head]
			for _, transition := range query.graphs[current.Tile].Transitions {
				if transition.FromRegion != current.Region {
					continue
				}
				next := navRouteNode{Tile: transition.ToTile, Region: transition.ToRegion}
				if _, seen := reachable[next]; seen || !query.hasRegion(next) {
					continue
				}
				reachable[next] = struct{}{}
				queue = append(queue, next)
			}
		}
		for target := range wanted {
			region := navRouteNode{Tile: target.Tile, Region: query.spanRegions[target.Tile][target.Span]}
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
	q.activeDegrees = make(map[TerrainChunkCoordDef][]uint8, len(q.graphs))
	for coord, graph := range q.graphs {
		degrees := make([]uint8, len(q.sources[coord].Spans))
		for _, edge := range graph.SpanTransitions {
			from := NavSpanRef{Tile: coord, Span: edge.From}
			if _, blocked := q.blocked[from]; blocked {
				continue
			}
			if _, blocked := q.blocked[edge.To]; blocked {
				continue
			}
			if int(edge.From) < len(degrees) && degrees[edge.From] < 2 {
				degrees[edge.From]++
			}
		}
		q.activeDegrees[coord] = degrees
	}
}

func (q *navGraphQuery) spanExitCount(ref NavSpanRef) uint8 {
	degrees := q.activeDegrees[ref.Tile]
	if int(ref.Span) >= len(degrees) {
		return 0
	}
	return degrees[ref.Span]
}

func navClampToSpanAxis(value, minimum, size float32) float32 {
	return min(max(value, math.Nextafter32(minimum, minimum+size)), math.Nextafter32(minimum+size, minimum))
}

func navSpanRefLess(a, b NavSpanRef) bool {
	return terrainCoordLess(a.Tile, b.Tile) || a.Tile == b.Tile && a.Span < b.Span
}

func (q *navGraphQuery) spanCenter(ref NavSpanRef) Vec3 {
	span := q.spans[ref.Tile][ref.Span]
	return Vec3{
		(float32(ref.Tile.X*q.chunkSize+span.X) + 0.5) * q.voxelResolution,
		span.SupportHeight,
		(float32(ref.Tile.Z*q.chunkSize+span.Z) + 0.5) * q.voxelResolution,
	}
}

func (q *navGraphQuery) regionCenter(node navRouteNode) Vec3 {
	center := q.graphs[node.Tile].Regions[node.Region].Center
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
