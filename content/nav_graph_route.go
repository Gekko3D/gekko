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

type navRegionParent struct {
	Node       navRouteNode
	Transition NavRegionTransitionDef
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
	start, startTile, found := query.resolve(startPoint)
	if !found {
		return NavRouteResult{FailureReason: NavRouteStartUnsupported, FailureTile: startTile}, nil
	}
	goal, goalTile, found := query.resolve(goalPoint)
	if !found {
		return NavRouteResult{FailureReason: NavRouteGoalUnsupported, FailureTile: goalTile}, nil
	}

	var allowed map[TerrainChunkCoordDef]struct{}
	if start.Region.Tile != goal.Region.Tile {
		if sectors, ok := findNavSectorRoute(query, start.Region.Tile, goal.Region.Tile); ok {
			allowed = make(map[TerrainChunkCoordDef]struct{}, len(sectors))
			for _, sector := range sectors {
				allowed[sector] = struct{}{}
			}
		}
	}
	regionRoute, ok := findNavRegionRoute(query, start.Region, goal.Region, allowed)
	if !ok && allowed != nil {
		// A tile corridor can hide a valid region detour through another tile.
		regionRoute, ok = findNavRegionRoute(query, start.Region, goal.Region, nil)
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
	result := NavRouteResult{Found: true}
	result.Steps = append(result.Steps, NavRouteStep{Tile: start.Region.Tile, Region: start.Region.Region, Target: start.Projected})
	for i := 1; i < len(regionRoute); i++ {
		crossing := query.spanTransitionTarget(regionRoute[i-1].Node.Tile, backing[i-1])
		result.Steps = append(result.Steps, NavRouteStep{
			Tile: regionRoute[i].Node.Tile, Region: regionRoute[i].Node.Region,
			EnterTransition: regionRoute[i].Transition.ID, Target: crossing,
			RequiredAction: regionRoute[i].Transition.Kind,
		})
	}

	if len(backing) == 0 {
		path := query.findSpanRoute(start.Ref.Tile, start.Region.Region, start.Ref.Span, goal.Ref.Span)
		if !path.Found {
			return NavRouteResult{}, fmt.Errorf("navigation region %s/%d does not preserve span reachability", TerrainChunkKey(start.Region.Tile), start.Region.Region)
		}
		query.appendSpanPath(&result.Waypoints, start.Ref.Tile, path.Spans, 1)
	} else {
		startPath := query.findSpanRoute(start.Ref.Tile, start.Region.Region, start.Ref.Span, backing[0].From)
		if !startPath.Found {
			return NavRouteResult{}, fmt.Errorf("navigation start region %s/%d cannot reach route transition", TerrainChunkKey(start.Region.Tile), start.Region.Region)
		}
		query.appendSpanPath(&result.Waypoints, start.Ref.Tile, startPath.Spans, 1)
		for i, edge := range backing {
			query.appendWaypoint(&result.Waypoints, query.spanTransitionTarget(regionRoute[i].Node.Tile, edge))
			if i+1 < len(backing) {
				node := regionRoute[i+1].Node
				path := query.findSpanRoute(node.Tile, node.Region, edge.To.Span, backing[i+1].From)
				if !path.Found {
					return NavRouteResult{}, fmt.Errorf("navigation intermediate region %s/%d does not preserve span reachability", TerrainChunkKey(node.Tile), node.Region)
				}
				query.appendSpanPath(&result.Waypoints, node.Tile, path.Spans, 1)
			}
		}
		last := backing[len(backing)-1].To
		goalPath := query.findSpanRoute(goal.Ref.Tile, goal.Region.Region, last.Span, goal.Ref.Span)
		if !goalPath.Found {
			return NavRouteResult{}, fmt.Errorf("navigation goal region %s/%d cannot reach goal span", TerrainChunkKey(goal.Region.Tile), goal.Region.Region)
		}
		query.appendSpanPath(&result.Waypoints, goal.Ref.Tile, goalPath.Spans, 1)
	}
	query.appendWaypoint(&result.Waypoints, goal.Projected)
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

func findNavRegionRoute(query *navGraphQuery, start, goal navRouteNode, allowed map[TerrainChunkCoordDef]struct{}) ([]navRegionRouteStep, bool) {
	if start == goal {
		return []navRegionRouteStep{{Node: start}}, true
	}
	heuristicScale := navRegionHeuristicScale(query, allowed)
	frontier := navRouteQueue{{node: start, estimate: heuristicScale * navVec3Distance(query.regionCenter(start), query.regionCenter(goal))}}
	heap.Init(&frontier)
	costs := map[navRouteNode]float32{start: 0}
	parents := make(map[navRouteNode]navRegionParent)
	for frontier.Len() > 0 {
		current := heap.Pop(&frontier).(navRouteQueueItem)
		if current.cost != costs[current.node] {
			continue
		}
		if current.node == goal {
			path := []navRegionRouteStep{{Node: goal, Transition: parents[goal].Transition}}
			for node := goal; node != start; {
				parent := parents[node]
				node = parent.Node
				step := navRegionRouteStep{Node: node}
				if node != start {
					step.Transition = parents[node].Transition
				}
				path = append(path, step)
			}
			reverseRegionRoute(path)
			return path, true
		}
		graph := query.graphs[current.node.Tile]
		for _, transition := range graph.Transitions {
			if transition.FromRegion != current.node.Region {
				continue
			}
			next := navRouteNode{Tile: transition.ToTile, Region: transition.ToRegion}
			if !query.hasRegion(next) || !tileAllowed(allowed, next.Tile) {
				continue
			}
			cost := current.cost + transition.Cost
			if previous, seen := costs[next]; seen && cost >= previous {
				continue
			}
			costs[next] = cost
			parents[next] = navRegionParent{Node: current.node, Transition: transition}
			heap.Push(&frontier, navRouteQueueItem{
				node: next, cost: cost,
				estimate: cost + heuristicScale*navVec3Distance(query.regionCenter(next), query.regionCenter(goal)),
			})
		}
	}
	return nil, false
}

func navRegionHeuristicScale(query *navGraphQuery, allowed map[TerrainChunkCoordDef]struct{}) float32 {
	scale := float32(math.Inf(1))
	for coord, graph := range query.graphs {
		if !tileAllowed(allowed, coord) {
			continue
		}
		for _, transition := range graph.Transitions {
			from := navRouteNode{Tile: coord, Region: transition.FromRegion}
			to := navRouteNode{Tile: transition.ToTile, Region: transition.ToRegion}
			if !query.hasRegion(to) || !tileAllowed(allowed, to.Tile) {
				continue
			}
			distance := navVec3Distance(query.regionCenter(from), query.regionCenter(to))
			if distance > 0 {
				scale = min(scale, transition.Cost/distance)
			}
		}
	}
	if !finite(scale) {
		return 0
	}
	return scale
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
	from := q.spanCenter(NavSpanRef{Tile: fromTile, Span: edge.From})
	to := q.spanCenter(edge.To)
	return Vec3{(from[0] + to[0]) * 0.5, to[1], (from[2] + to[2]) * 0.5}
}

func (q *navGraphQuery) appendSpanPath(dst *[]Vec3, tile TerrainChunkCoordDef, spans []uint32, skip int) {
	if skip >= len(spans) {
		return
	}
	anchor := skip - 1
	if anchor < 0 {
		anchor = 0
		q.appendWaypoint(dst, q.spanCenter(NavSpanRef{Tile: tile, Span: spans[anchor]}))
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
		q.appendWaypoint(dst, q.spanCenter(NavSpanRef{Tile: tile, Span: spans[next]}))
		anchor = next
	}
}

type navSpanPathCell struct {
	x, z   int
	height uint32
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

func (*navGraphQuery) appendWaypoint(dst *[]Vec3, waypoint Vec3) {
	if len(*dst) == 0 || (*dst)[len(*dst)-1] != waypoint {
		*dst = append(*dst, waypoint)
	}
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
