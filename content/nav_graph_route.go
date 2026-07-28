package content

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
)

type navRegionRouteStep struct {
	Node       navRouteNode
	Transition NavRegionTransitionDef
}

type navRegionPortalKey struct {
	FromTile   TerrainChunkCoordDef
	Transition uint32
	Start      bool
}

type navRegionPortalState struct {
	Node   navRouteNode
	Point  Vec3
	Via    NavRegionTransitionDef
	Parent navRegionPortalKey
}

// FindNavGraphRoute resolves world points, restricts long searches through a
// tile-sector corridor, searches regions, then refines the selected corridor
// in the span graph. Callers pass graph tiles for exactly one agent profile.
func FindNavGraphRoute(sources []NavSourceTileDef, graphs []NavGraphTileDef, chunkSize int, voxelResolution float32, startPoint, goalPoint Vec3) (NavRouteResult, error) {
	query, err := NewNavGraphQuery(sources, graphs, chunkSize, voxelResolution)
	if err != nil {
		return NavRouteResult{}, err
	}
	return query.FindRoute(startPoint, goalPoint)
}

func (q *NavGraphQuery) FindRoute(startPoint, goalPoint Vec3) (NavRouteResult, error) {
	if !validVec3(startPoint) || !validVec3(goalPoint) {
		return NavRouteResult{}, fmt.Errorf("navigation route points must be finite")
	}
	if q == nil || q.query == nil {
		return NavRouteResult{}, fmt.Errorf("navigation graph query is required")
	}
	query := q.query
	start, startTile, found := q.locate(startPoint)
	if !found {
		return NavRouteResult{FailureReason: NavRouteStartUnsupported, FailureTile: startTile}, nil
	}
	goal, goalTile, found := query.resolve(goalPoint)
	if !found {
		return NavRouteResult{FailureReason: NavRouteGoalUnsupported, FailureTile: goalTile}, nil
	}
	if len(query.blocked) != 0 {
		result := query.findBlockerRoute(start, goal)
		if result.Found {
			result.StartLocation, result.GoalLocation = navPointResult(start), navPointResult(goal)
		}
		return result, nil
	}

	var allowed map[TerrainChunkCoordDef]struct{}
	if start.Region.Tile != goal.Region.Tile {
		if sectors, ok := findNavSectorRoute(query, start.Region.Tile, goal.Region.Tile); ok {
			allowed = navSectorCorridor(query, sectors)
		}
	}
	regionRoute, ok := findNavRegionRoute(query, start.Region, goal.Region, start.Projected, goal.Projected, allowed)
	if !ok && allowed != nil {
		// A tile corridor can hide a valid region detour through another tile.
		regionRoute, ok = findNavRegionRoute(query, start.Region, goal.Region, start.Projected, goal.Projected, nil)
	}
	if !ok {
		return NavRouteResult{FailureReason: NavRouteNoRoute, FailureTile: goal.Ref.Tile}, nil
	}
	backing := make([]NavSpanTransitionDef, len(regionRoute)-1)
	for i := range backing {
		edge, ok := query.findBackingSpanTransition(regionRoute[i].Node, regionRoute[i+1].Transition)
		if !ok {
			return NavRouteResult{}, fmt.Errorf("navigation region transition %s/%d -> %s/%d has no backing span transition", TerrainChunkKey(regionRoute[i].Node.Tile), regionRoute[i].Node.Region, TerrainChunkKey(regionRoute[i+1].Node.Tile), regionRoute[i+1].Node.Region)
		}
		backing[i] = edge
	}
	result := NavRouteResult{
		Found: true, StartLocation: navPointResult(start), GoalLocation: navPointResult(goal),
	}
	result.Steps = append(result.Steps, NavRouteStep{Tile: start.Region.Tile, Region: start.Region.Region, Target: start.Projected, TraversalWaypoint: -1})
	for i := 1; i < len(regionRoute); i++ {
		crossing := query.spanTransitionTarget(regionRoute[i-1].Node.Tile, backing[i-1])
		result.Steps = append(result.Steps, NavRouteStep{
			Tile: regionRoute[i].Node.Tile, Region: regionRoute[i].Node.Region,
			EnterTransition: regionRoute[i].Transition.ID, Target: crossing,
			RequiredAction: regionRoute[i].Transition.Kind,
			Traversal:      cloneNavTraversal(regionRoute[i].Transition.Traversal),
			Gate:           cloneNavTransitionGate(regionRoute[i].Transition.Gate), TraversalWaypoint: -1,
		})
	}
	if navRouteWalkOnly(result.Steps) && query.waypointLineVisible(start.Projected, goal.Projected) {
		result.Waypoints = []Vec3{goal.Projected}
		result.WaypointSpans = []NavSpanRef{goal.Ref}
		return result, nil
	}

	if len(backing) == 0 {
		path := query.findSpanRoute(start.Ref.Tile, start.Region.Region, start.Ref.Span, goal.Ref.Span)
		if !path.Found {
			return NavRouteResult{}, fmt.Errorf("navigation region %s/%d does not preserve span reachability", TerrainChunkKey(start.Region.Tile), start.Region.Region)
		}
		query.appendSpanPath(&result, start.Ref.Tile, path.Spans, 1)
	} else {
		startPath := query.findSpanRoute(start.Ref.Tile, start.Region.Region, start.Ref.Span, backing[0].From)
		if !startPath.Found {
			return NavRouteResult{}, fmt.Errorf("navigation start region %s/%d cannot reach route transition", TerrainChunkKey(start.Region.Tile), start.Region.Region)
		}
		query.appendSpanPath(&result, start.Ref.Tile, startPath.Spans, 1)
		for i, edge := range backing {
			if edge.Traversal != nil {
				from := NavSpanRef{Tile: regionRoute[i].Node.Tile, Span: edge.From}
				result.Steps[i+1].TraversalWaypoint = query.appendWaypointIndex(&result, edge.Traversal.Start, from)
				query.appendWaypoint(&result, edge.Traversal.End, edge.To)
			} else {
				query.appendWaypoint(&result, query.spanTransitionTarget(regionRoute[i].Node.Tile, edge), edge.To)
			}
			if i+1 < len(backing) {
				node := regionRoute[i+1].Node
				path := query.findSpanRoute(node.Tile, node.Region, edge.To.Span, backing[i+1].From)
				if !path.Found {
					return NavRouteResult{}, fmt.Errorf("navigation intermediate region %s/%d does not preserve span reachability", TerrainChunkKey(node.Tile), node.Region)
				}
				query.appendSpanPath(&result, node.Tile, path.Spans, 1)
			}
		}
		last := backing[len(backing)-1].To
		goalPath := query.findSpanRoute(goal.Ref.Tile, goal.Region.Region, last.Span, goal.Ref.Span)
		if !goalPath.Found {
			return NavRouteResult{}, fmt.Errorf("navigation goal region %s/%d cannot reach goal span", TerrainChunkKey(goal.Region.Tile), goal.Region.Region)
		}
		query.appendSpanPath(&result, goal.Ref.Tile, goalPath.Spans, 1)
	}
	query.appendWaypoint(&result, goal.Projected, goal.Ref)
	if navRouteWalkOnly(result.Steps) {
		query.simplifyWaypoints(start.Projected, &result)
	}
	return result, nil
}

func findNavSectorRoute(query *navGraphQuery, start, goal TerrainChunkCoordDef) ([]TerrainChunkCoordDef, bool) {
	if start == goal {
		return []TerrainChunkCoordDef{start}, true
	}
	edges := make(map[TerrainChunkCoordDef][]TerrainChunkCoordDef)
	for coord, graph := range query.graphs {
		seen := map[TerrainChunkCoordDef]struct{}{}
		for _, transition := range graph.Transitions {
			if transition.ToTile == coord {
				continue
			}
			if _, loaded := query.graphs[transition.ToTile]; loaded {
				seen[transition.ToTile] = struct{}{}
			}
		}
		for next := range seen {
			edges[coord] = append(edges[coord], next)
		}
		sort.Slice(edges[coord], func(i, j int) bool { return terrainCoordLess(edges[coord][i], edges[coord][j]) })
	}

	startNode, goalNode := navRouteNode{Tile: start}, navRouteNode{Tile: goal}
	frontier := navRouteQueue{{node: startNode, estimate: navSectorDistance(start, goal)}}
	heap.Init(&frontier)
	costs := map[navRouteNode]float32{startNode: 0}
	parents := make(map[navRouteNode]navRouteNode)
	for frontier.Len() > 0 {
		current := heap.Pop(&frontier).(navRouteQueueItem)
		if current.cost != costs[current.node] {
			continue
		}
		if current.node == goalNode {
			path := []TerrainChunkCoordDef{goal}
			for node := goalNode; node != startNode; {
				node = parents[node]
				path = append(path, node.Tile)
			}
			reverseTerrainCoords(path)
			return path, true
		}
		for _, nextTile := range edges[current.node.Tile] {
			next := navRouteNode{Tile: nextTile}
			cost := current.cost + 1
			if previous, seen := costs[next]; seen && cost >= previous {
				continue
			}
			costs[next], parents[next] = cost, current.node
			heap.Push(&frontier, navRouteQueueItem{node: next, cost: cost, estimate: cost + navSectorDistance(nextTile, goal)})
		}
	}
	return nil, false
}

func navSectorCorridor(query *navGraphQuery, sectors []TerrainChunkCoordDef) map[TerrainChunkCoordDef]struct{} {
	allowed := make(map[TerrainChunkCoordDef]struct{}, len(sectors)*3)
	for coord := range query.graphs {
		for _, sector := range sectors {
			if absNavSpanInt(coord.X-sector.X)+absNavSpanInt(coord.Y-sector.Y)+absNavSpanInt(coord.Z-sector.Z) <= 1 {
				allowed[coord] = struct{}{}
				break
			}
		}
	}
	return allowed
}

func findNavRegionRoute(query *navGraphQuery, start, goal navRouteNode, startPoint, goalPoint Vec3, allowed map[TerrainChunkCoordDef]struct{}) ([]navRegionRouteStep, bool) {
	if start == goal {
		return []navRegionRouteStep{{Node: start}}, true
	}
	startKey := navRegionPortalKey{Start: true}
	frontier := navRegionPortalQueue{{key: startKey, estimate: navVec3Distance(startPoint, goalPoint)}}
	heap.Init(&frontier)
	costs := map[navRegionPortalKey]float32{startKey: 0}
	states := map[navRegionPortalKey]navRegionPortalState{startKey: {Node: start, Point: startPoint}}
	for frontier.Len() > 0 {
		current := heap.Pop(&frontier).(navRegionPortalQueueItem)
		if current.cost != costs[current.key] {
			continue
		}
		state := states[current.key]
		if state.Node == goal {
			path := []navRegionRouteStep{{Node: start}}
			var reversed []navRegionPortalState
			for key := current.key; key != startKey; {
				entry := states[key]
				reversed = append(reversed, entry)
				key = entry.Parent
			}
			for i := len(reversed) - 1; i >= 0; i-- {
				path = append(path, navRegionRouteStep{Node: reversed[i].Node, Transition: reversed[i].Via})
			}
			return path, true
		}
		graph := query.graphs[state.Node.Tile]
		for _, transition := range graph.Transitions {
			if transition.FromRegion != state.Node.Region {
				continue
			}
			next := navRouteNode{Tile: transition.ToTile, Region: transition.ToRegion}
			if !query.hasRegion(next) || !tileAllowed(allowed, next.Tile) {
				continue
			}
			edge, ok := query.findBackingSpanTransition(state.Node, transition)
			if !ok {
				continue
			}
			entry := query.spanTransitionEntry(state.Node.Tile, edge)
			exit := query.spanTransitionTarget(state.Node.Tile, edge)
			cost := current.cost + navVec3Distance(state.Point, entry) + transition.Cost
			key := navRegionPortalKey{FromTile: state.Node.Tile, Transition: transition.ID}
			if previous, seen := costs[key]; seen && cost >= previous {
				continue
			}
			costs[key] = cost
			states[key] = navRegionPortalState{Node: next, Point: exit, Via: transition, Parent: current.key}
			heap.Push(&frontier, navRegionPortalQueueItem{
				key: key, cost: cost,
				estimate: cost + navVec3Distance(exit, goalPoint),
			})
		}
	}
	return nil, false
}

func (q *navGraphQuery) hasRegion(node navRouteNode) bool {
	graph, ok := q.graphs[node.Tile]
	return ok && int(node.Region) < len(graph.Regions) && graph.Regions[node.Region].ID == node.Region
}

func (q *navGraphQuery) findBackingSpanTransition(from navRouteNode, transition NavRegionTransitionDef) (NavSpanTransitionDef, bool) {
	edge, ok := q.backing[from.Tile][transition.ID]
	return edge, ok
}

func (q *navGraphQuery) spanTransitionTarget(fromTile TerrainChunkCoordDef, edge NavSpanTransitionDef) Vec3 {
	if edge.Traversal != nil {
		return edge.Traversal.End
	}
	from := q.spanCenter(NavSpanRef{Tile: fromTile, Span: edge.From})
	to := q.spanCenter(edge.To)
	return Vec3{(from[0] + to[0]) * 0.5, to[1], (from[2] + to[2]) * 0.5}
}

func (q *navGraphQuery) spanTransitionEntry(fromTile TerrainChunkCoordDef, edge NavSpanTransitionDef) Vec3 {
	if edge.Traversal != nil {
		return edge.Traversal.Start
	}
	return q.spanTransitionTarget(fromTile, edge)
}

func (q *navGraphQuery) appendSpanPath(route *NavRouteResult, tile TerrainChunkCoordDef, spans []uint32, skip int) {
	if skip >= len(spans) {
		return
	}
	anchor := skip - 1
	if anchor < 0 {
		anchor = 0
		ref := NavSpanRef{Tile: tile, Span: spans[anchor]}
		q.appendWaypoint(route, q.spanCenter(ref), ref)
	}
	region := q.spanRegions[tile][spans[anchor]]
	cells := make(map[navSpanPathCell]struct{})
	for id, spanRegion := range q.spanRegions[tile] {
		if spanRegion == region {
			span := q.spans[tile][id]
			cells[navSpanPathCell{span.X, span.Z, math.Float32bits(span.SupportHeight)}] = struct{}{}
		}
	}
	// ponytail: greedy string pulling is not globally minimal; replace it only
	// if measured route quality needs a full visibility-graph optimizer.
	for anchor+1 < len(spans) {
		next := anchor + 1
		for next+1 < len(spans) && navSpanPathVisible(cells, q.spans[tile][spans[anchor]], q.spans[tile][spans[next+1]]) {
			next++
		}
		ref := NavSpanRef{Tile: tile, Span: spans[next]}
		q.appendWaypoint(route, q.spanCenter(ref), ref)
		anchor = next
	}
}

type navSpanPathCell struct {
	x, z   int
	height uint32
}

type navWalkableCell struct {
	classID uint32
	exits   uint8
}

const (
	navWalkableExitPositiveX uint8 = 1 << iota
	navWalkableExitNegativeX
	navWalkableExitPositiveZ
	navWalkableExitNegativeZ
)

func navWalkableExit(dx, dz int) uint8 {
	switch {
	case dx == 1 && dz == 0:
		return navWalkableExitPositiveX
	case dx == -1 && dz == 0:
		return navWalkableExitNegativeX
	case dx == 0 && dz == 1:
		return navWalkableExitPositiveZ
	case dx == 0 && dz == -1:
		return navWalkableExitNegativeZ
	default:
		return 0
	}
}

func navSpanPathVisible(cells map[navSpanPathCell]struct{}, from, to NavSpanDef) bool {
	height := math.Float32bits(from.SupportHeight)
	if math.Float32bits(to.SupportHeight) != height {
		return false
	}
	steps := 2 * max(absNavSpanInt(to.X-from.X), absNavSpanInt(to.Z-from.Z))
	if steps == 0 {
		return true
	}
	for i := 0; i <= steps; i++ {
		x := ((2*from.X+1)*(steps-i) + (2*to.X+1)*i) / (2 * steps)
		z := ((2*from.Z+1)*(steps-i) + (2*to.Z+1)*i) / (2 * steps)
		if _, ok := cells[navSpanPathCell{x, z, height}]; !ok {
			return false
		}
	}
	return true
}

func (*navGraphQuery) appendWaypoint(route *NavRouteResult, waypoint Vec3, span NavSpanRef) {
	if len(route.Waypoints) > 0 && route.Waypoints[len(route.Waypoints)-1] == waypoint {
		route.WaypointSpans[len(route.WaypointSpans)-1] = span
		return
	}
	route.Waypoints = append(route.Waypoints, waypoint)
	route.WaypointSpans = append(route.WaypointSpans, span)
}

func (q *navGraphQuery) appendWaypointIndex(route *NavRouteResult, waypoint Vec3, span NavSpanRef) int {
	q.appendWaypoint(route, waypoint, span)
	return len(route.Waypoints) - 1
}

func cloneNavTraversal(source *NavTraversalDef) *NavTraversalDef {
	if source == nil {
		return nil
	}
	copy := *source
	if source.Carrier != nil {
		carrier := *source.Carrier
		copy.Carrier = &carrier
	}
	return &copy
}

func cloneNavTransitionGate(source *NavTransitionGateDef) *NavTransitionGateDef {
	if source == nil {
		return nil
	}
	copy := *source
	return &copy
}

func navRouteWalkOnly(steps []NavRouteStep) bool {
	for _, step := range steps {
		if step.Gate != nil || step.RequiredAction != "" && step.RequiredAction != NavTransitionWalk {
			return false
		}
	}
	return true
}

func (q *navGraphQuery) simplifyWaypoints(start Vec3, route *NavRouteResult) {
	if len(route.Waypoints) < 2 {
		return
	}
	waypoints := make([]Vec3, 0, len(route.Waypoints))
	spans := make([]NavSpanRef, 0, len(route.WaypointSpans))
	anchor := start
	for first := 0; first < len(route.Waypoints); {
		next := first
		for candidate := len(route.Waypoints) - 1; candidate > first; candidate-- {
			if q.waypointLineVisible(anchor, route.Waypoints[candidate]) {
				next = candidate
				break
			}
		}
		waypoints = append(waypoints, route.Waypoints[next])
		spans = append(spans, route.WaypointSpans[next])
		anchor = route.Waypoints[next]
		first = next + 1
	}
	route.Waypoints, route.WaypointSpans = waypoints, spans
}

func (q *navGraphQuery) waypointLineVisible(from, to Vec3) bool {
	if q == nil || math.Float32bits(from[1]) != math.Float32bits(to[1]) {
		return false
	}
	fromX := int(math.Floor(float64(from[0] / q.voxelResolution)))
	fromZ := int(math.Floor(float64(from[2] / q.voxelResolution)))
	toX := int(math.Floor(float64(to[0] / q.voxelResolution)))
	toZ := int(math.Floor(float64(to[2] / q.voxelResolution)))
	height := math.Float32bits(from[1])
	classID := q.walkableCells[navSpanPathCell{x: fromX, z: fromZ, height: height}].classID
	if classID == 0 || q.walkableCells[navSpanPathCell{x: toX, z: toZ, height: height}].classID != classID {
		return false
	}
	steps := 2 * max(absNavSpanInt(toX-fromX), absNavSpanInt(toZ-fromZ))
	if steps == 0 {
		return true
	}
	previousX, previousZ := fromX, fromZ
	for i := 1; i <= steps; i++ {
		x := ((2*fromX+1)*(steps-i) + (2*toX+1)*i) / (2 * steps)
		z := ((2*fromZ+1)*(steps-i) + (2*toZ+1)*i) / (2 * steps)
		if !q.walkableCellsConnected(previousX, previousZ, x, z, height, classID) {
			return false
		}
		previousX, previousZ = x, z
	}
	return true
}

func (q *navGraphQuery) walkableCellsConnected(fromX, fromZ, toX, toZ int, height, classID uint32) bool {
	dx, dz := toX-fromX, toZ-fromZ
	if dx == 0 && dz == 0 {
		return true
	}
	if navWalkableExit(dx, dz) != 0 {
		return q.walkableCardinalExit(fromX, fromZ, toX, toZ, height, classID)
	}
	if absNavSpanInt(dx) != 1 || absNavSpanInt(dz) != 1 {
		return false
	}
	return q.walkableCardinalExit(fromX, fromZ, toX, fromZ, height, classID) &&
		q.walkableCardinalExit(toX, fromZ, toX, toZ, height, classID) ||
		q.walkableCardinalExit(fromX, fromZ, fromX, toZ, height, classID) &&
			q.walkableCardinalExit(fromX, toZ, toX, toZ, height, classID)
}

func (q *navGraphQuery) walkableCardinalExit(fromX, fromZ, toX, toZ int, height, classID uint32) bool {
	exit := navWalkableExit(toX-fromX, toZ-fromZ)
	if exit == 0 {
		return false
	}
	from := q.walkableCells[navSpanPathCell{x: fromX, z: fromZ, height: height}]
	to := q.walkableCells[navSpanPathCell{x: toX, z: toZ, height: height}]
	return from.classID == classID && to.classID == classID && from.exits&exit != 0
}

func tileAllowed(allowed map[TerrainChunkCoordDef]struct{}, tile TerrainChunkCoordDef) bool {
	if allowed == nil {
		return true
	}
	_, ok := allowed[tile]
	return ok
}

func navSectorDistance(a, b TerrainChunkCoordDef) float32 {
	return float32(absNavSpanInt(a.X-b.X) + absNavSpanInt(a.Y-b.Y) + absNavSpanInt(a.Z-b.Z))
}

func midpointVec3(a, b Vec3) Vec3 {
	return Vec3{(a[0] + b[0]) * 0.5, (a[1] + b[1]) * 0.5, (a[2] + b[2]) * 0.5}
}

func reverseTerrainCoords(values []TerrainChunkCoordDef) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseRegionRoute(values []navRegionRouteStep) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

type navRouteQueueItem struct {
	node     navRouteNode
	cost     float32
	estimate float32
}

type navRouteQueue []navRouteQueueItem

func (q navRouteQueue) Len() int { return len(q) }
func (q navRouteQueue) Less(i, j int) bool {
	if q[i].estimate != q[j].estimate {
		return q[i].estimate < q[j].estimate
	}
	if q[i].cost != q[j].cost {
		return q[i].cost < q[j].cost
	}
	return navRouteNodeLess(q[i].node, q[j].node)
}
func (q navRouteQueue) Swap(i, j int)   { q[i], q[j] = q[j], q[i] }
func (q *navRouteQueue) Push(value any) { *q = append(*q, value.(navRouteQueueItem)) }
func (q *navRouteQueue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]
	return last
}

type navRegionPortalQueueItem struct {
	key      navRegionPortalKey
	cost     float32
	estimate float32
}

type navRegionPortalQueue []navRegionPortalQueueItem

func (q navRegionPortalQueue) Len() int { return len(q) }
func (q navRegionPortalQueue) Less(i, j int) bool {
	if q[i].estimate != q[j].estimate {
		return q[i].estimate < q[j].estimate
	}
	if q[i].cost != q[j].cost {
		return q[i].cost < q[j].cost
	}
	return navRegionPortalKeyLess(q[i].key, q[j].key)
}
func (q navRegionPortalQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *navRegionPortalQueue) Push(value any) {
	*q = append(*q, value.(navRegionPortalQueueItem))
}
func (q *navRegionPortalQueue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]
	return last
}

func navRegionPortalKeyLess(a, b navRegionPortalKey) bool {
	if a.Start != b.Start {
		return a.Start
	}
	if a.FromTile != b.FromTile {
		return terrainCoordLess(a.FromTile, b.FromTile)
	}
	return a.Transition < b.Transition
}
