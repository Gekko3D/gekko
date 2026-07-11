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
// tile-sector corridor, searches regions, then refines only endpoint tiles in
// the span graph. Callers pass graph tiles for exactly one agent profile.
func FindNavGraphRoute(sources []NavSourceTileDef, graphs []NavGraphTileDef, chunkSize int, voxelResolution float32, startPoint, goalPoint Vec3) (NavRouteResult, error) {
	if !validVec3(startPoint) || !validVec3(goalPoint) {
		return NavRouteResult{}, fmt.Errorf("navigation route points must be finite")
	}
	query, err := newNavGraphQuery(sources, graphs, chunkSize, voxelResolution)
	if err != nil {
		return NavRouteResult{}, err
	}
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
		path, err := FindNavSpanRoute(query.sources[start.Ref.Tile], query.graphs[start.Ref.Tile], voxelResolution, start.Ref.Span, goal.Ref.Span)
		if err != nil {
			return NavRouteResult{}, err
		}
		if !path.Found {
			return NavRouteResult{}, fmt.Errorf("navigation region %s/%d does not preserve span reachability", TerrainChunkKey(start.Region.Tile), start.Region.Region)
		}
		query.appendSpanPath(&result.Waypoints, start.Ref.Tile, path.Spans, 1)
	} else {
		startPath, err := FindNavSpanRoute(query.sources[start.Ref.Tile], query.graphs[start.Ref.Tile], voxelResolution, start.Ref.Span, backing[0].From)
		if err != nil {
			return NavRouteResult{}, err
		}
		if !startPath.Found {
			return NavRouteResult{}, fmt.Errorf("navigation start region %s/%d cannot reach route transition", TerrainChunkKey(start.Region.Tile), start.Region.Region)
		}
		query.appendSpanPath(&result.Waypoints, start.Ref.Tile, startPath.Spans, 1)
		for i, edge := range backing {
			query.appendWaypoint(&result.Waypoints, query.spanTransitionTarget(regionRoute[i].Node.Tile, edge))
		}
		last := backing[len(backing)-1].To
		goalPath, err := FindNavSpanRoute(query.sources[goal.Ref.Tile], query.graphs[goal.Ref.Tile], voxelResolution, last.Span, goal.Ref.Span)
		if err != nil {
			return NavRouteResult{}, err
		}
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
	target := navRouteNode{Tile: transition.ToTile, Region: transition.ToRegion}
	want := midpointVec3(transition.CrossingStart, transition.CrossingEnd)
	if transition.ToTile == from.Tile {
		want[0] += float32(from.Tile.X*q.chunkSize) * q.voxelResolution
		want[2] += float32(from.Tile.Z*q.chunkSize) * q.voxelResolution
	}
	bestDistance := float32(math.Inf(1))
	var best NavSpanTransitionDef
	found := false
	for _, edge := range q.graphs[from.Tile].SpanTransitions {
		if q.spanRegions[from.Tile][edge.From] != from.Region || edge.To.Tile != target.Tile || q.spanRegions[target.Tile][edge.To.Span] != target.Region || edge.Kind != transition.Kind || navRegionFlagsKey(edge.RequiresFlags) != navRegionFlagsKey(transition.RequiresFlags) {
			continue
		}
		point := q.spanTransitionTarget(from.Tile, edge)
		distance := navVec3Distance(point, want)
		better := !found || distance < bestDistance || distance == bestDistance && (edge.From < best.From || edge.From == best.From && (terrainCoordLess(edge.To.Tile, best.To.Tile) || edge.To.Tile == best.To.Tile && edge.To.Span < best.To.Span))
		if !better {
			continue
		}
		bestDistance, best, found = distance, edge, true
	}
	return best, found
}

func (q *navGraphQuery) spanTransitionTarget(fromTile TerrainChunkCoordDef, edge NavSpanTransitionDef) Vec3 {
	from := q.spanCenter(NavSpanRef{Tile: fromTile, Span: edge.From})
	to := q.spanCenter(edge.To)
	return Vec3{(from[0] + to[0]) * 0.5, to[1], (from[2] + to[2]) * 0.5}
}

func (q *navGraphQuery) appendSpanPath(dst *[]Vec3, tile TerrainChunkCoordDef, spans []uint32, skip int) {
	for _, span := range spans[skip:] {
		q.appendWaypoint(dst, q.spanCenter(NavSpanRef{Tile: tile, Span: span}))
	}
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
