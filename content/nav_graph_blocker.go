package content

import (
	"container/heap"
	"fmt"
	"math"
	"strings"
)

// NavBlockerDef is a temporary world-space obstacle over an immutable baked graph.
type NavBlockerDef struct {
	ID  string
	Min Vec3
	Max Vec3
}

// NewNavGraphQueryWithBlockers builds a query overlay for one agent profile.
// Blockers remove overlapping spans; they do not mutate or rebake graph tiles.
func NewNavGraphQueryWithBlockers(sources []NavSourceTileDef, graphs []NavGraphTileDef, chunkSize int, voxelResolution float32, profile NavAgentProfileDef, blockers []NavBlockerDef) (*NavGraphQuery, error) {
	query, err := newNavGraphQuery(sources, graphs, chunkSize, voxelResolution)
	if err != nil {
		return nil, err
	}
	if len(blockers) == 0 {
		return &NavGraphQuery{query: query}, nil
	}
	if !finite(profile.Radius) || profile.Radius <= 0 || !finite(profile.Height) || profile.Height <= 0 {
		return nil, fmt.Errorf("navigation blocker agent radius and height must be finite and positive")
	}
	for _, graph := range graphs {
		if graph.AgentProfileID != profile.ID {
			return nil, fmt.Errorf("navigation blocker profile %q does not match graph profile %q", profile.ID, graph.AgentProfileID)
		}
	}
	seen := make(map[string]struct{}, len(blockers))
	for _, blocker := range blockers {
		if strings.TrimSpace(blocker.ID) == "" {
			return nil, fmt.Errorf("navigation blocker id is required")
		}
		if _, exists := seen[blocker.ID]; exists {
			return nil, fmt.Errorf("duplicate navigation blocker id %q", blocker.ID)
		}
		seen[blocker.ID] = struct{}{}
		if !validVec3(blocker.Min) || !validVec3(blocker.Max) || blocker.Min[0] > blocker.Max[0] || blocker.Min[1] > blocker.Max[1] || blocker.Min[2] > blocker.Max[2] {
			return nil, fmt.Errorf("navigation blocker %q requires finite ordered bounds", blocker.ID)
		}
	}
	query.enableBlockers(profile, blockers)
	return &NavGraphQuery{query: query}, nil
}

func (q *navGraphQuery) enableBlockers(profile NavAgentProfileDef, blockers []NavBlockerDef) {
	q.blocked = make(map[NavSpanRef]struct{})
	for coord, graph := range q.graphs {
		for _, spanID := range graph.SpanIDs {
			span := q.spans[coord][spanID]
			for _, blocker := range blockers {
				if navBlockerOverlapsSpan(blocker, profile, coord, q.chunkSize, q.voxelResolution, span) {
					ref := NavSpanRef{Tile: coord, Span: spanID}
					q.blocked[ref] = struct{}{}
					delete(q.walkableCells, q.spanPathCell(ref))
					break
				}
			}
		}
	}
	if len(q.blocked) == 0 {
		return
	}
	q.globalEdges = make(map[NavSpanRef][]NavSpanTransitionDef)
	q.globalScale = float32(math.Inf(1))
	for coord, graph := range q.graphs {
		for _, edge := range graph.SpanTransitions {
			from := NavSpanRef{Tile: coord, Span: edge.From}
			if _, ok := q.spanRegions[coord][edge.From]; !ok {
				continue
			}
			if _, ok := q.spanRegions[edge.To.Tile][edge.To.Span]; !ok {
				continue
			}
			q.globalEdges[from] = append(q.globalEdges[from], edge)
			distance := navVec3Distance(q.spanCenter(from), q.spanCenter(edge.To))
			if distance > 0 {
				q.globalScale = min(q.globalScale, edge.Cost/distance)
			}
		}
	}
	if !finite(q.globalScale) {
		q.globalScale = 0
	}
}

func navBlockerOverlapsSpan(blocker NavBlockerDef, profile NavAgentProfileDef, coord TerrainChunkCoordDef, chunkSize int, voxelResolution float32, span NavSpanDef) bool {
	minX := float32(coord.X*chunkSize+span.X) * voxelResolution
	minZ := float32(coord.Z*chunkSize+span.Z) * voxelResolution
	maxX, maxZ := minX+voxelResolution, minZ+voxelResolution
	return maxX > blocker.Min[0]-profile.Radius && minX < blocker.Max[0]+profile.Radius &&
		maxZ > blocker.Min[2]-profile.Radius && minZ < blocker.Max[2]+profile.Radius &&
		span.SupportHeight+profile.Height > blocker.Min[1] && span.SupportHeight < blocker.Max[1]
}

func (q *navGraphQuery) spanPathCell(ref NavSpanRef) navSpanPathCell {
	span := q.spans[ref.Tile][ref.Span]
	return navSpanPathCell{
		x:      ref.Tile.X*q.chunkSize + span.X,
		z:      ref.Tile.Z*q.chunkSize + span.Z,
		height: math.Float32bits(span.SupportHeight),
	}
}

type navBlockerRoute struct {
	refs  []NavSpanRef
	edges []NavSpanTransitionDef
}

type navBlockerParent struct {
	from NavSpanRef
	edge NavSpanTransitionDef
}

func (q *navGraphQuery) findBlockerSpanRoute(start, goal NavSpanRef) navBlockerRoute {
	// ponytail: global resident-span A* only while blockers overlap graph; add
	// overlay-aware region refinement if blocker-heavy worlds make this hot.
	if start == goal {
		return navBlockerRoute{refs: []NavSpanRef{start}}
	}
	frontier := navBlockerQueue{{ref: start, estimate: q.globalScale * navVec3Distance(q.spanCenter(start), q.spanCenter(goal))}}
	heap.Init(&frontier)
	costs := map[NavSpanRef]float32{start: 0}
	parents := make(map[NavSpanRef]navBlockerParent)
	for frontier.Len() > 0 {
		current := heap.Pop(&frontier).(navBlockerQueueItem)
		if current.cost != costs[current.ref] {
			continue
		}
		if current.ref == goal {
			refs := []NavSpanRef{goal}
			var edges []NavSpanTransitionDef
			for refs[len(refs)-1] != start {
				parent := parents[refs[len(refs)-1]]
				refs = append(refs, parent.from)
				edges = append(edges, parent.edge)
			}
			reverseNavSpanRefs(refs)
			reverseNavSpanTransitions(edges)
			return navBlockerRoute{refs: refs, edges: edges}
		}
		for _, edge := range q.globalEdges[current.ref] {
			if _, blocked := q.blocked[edge.To]; blocked {
				continue
			}
			cost := current.cost + edge.Cost
			if previous, seen := costs[edge.To]; seen && cost >= previous {
				continue
			}
			costs[edge.To] = cost
			parents[edge.To] = navBlockerParent{from: current.ref, edge: edge}
			heap.Push(&frontier, navBlockerQueueItem{
				ref: edge.To, cost: cost,
				estimate: cost + q.globalScale*navVec3Distance(q.spanCenter(edge.To), q.spanCenter(goal)),
			})
		}
	}
	return navBlockerRoute{}
}

func (q *navGraphQuery) findBlockerRoute(start, goal navResolvedSpan) NavRouteResult {
	path := q.findBlockerSpanRoute(start.Ref, goal.Ref)
	if len(path.refs) == 0 {
		return NavRouteResult{FailureReason: NavRouteNoRoute, FailureTile: goal.Ref.Tile}
	}
	result := NavRouteResult{Found: true, Steps: []NavRouteStep{{
		Tile: start.Region.Tile, Region: start.Region.Region, Target: start.Projected, TraversalWaypoint: -1,
	}}}
	edgeSteps := make([]int, len(path.edges))
	for i := range edgeSteps {
		edgeSteps[i] = -1
	}
	for i, edge := range path.edges {
		from, to := path.refs[i], path.refs[i+1]
		fromNode := navRouteNode{Tile: from.Tile, Region: q.spanRegions[from.Tile][from.Span]}
		toNode := navRouteNode{Tile: to.Tile, Region: q.spanRegions[to.Tile][to.Span]}
		if fromNode == toNode && edge.Traversal == nil {
			continue
		}
		edgeSteps[i] = len(result.Steps)
		result.Steps = append(result.Steps, NavRouteStep{
			Tile: toNode.Tile, Region: toNode.Region, Target: q.spanTransitionTarget(from.Tile, edge),
			RequiredAction: edge.Kind, Traversal: cloneNavTraversal(edge.Traversal), Gate: cloneNavTransitionGate(edge.Gate), TraversalWaypoint: -1,
		})
	}
	for i, edge := range path.edges {
		if edge.Traversal != nil {
			waypoint := q.appendWaypointIndex(&result.Waypoints, edge.Traversal.Start)
			q.appendWaypoint(&result.Waypoints, edge.Traversal.End)
			if edgeSteps[i] >= 0 {
				result.Steps[edgeSteps[i]].TraversalWaypoint = waypoint
			}
			continue
		}
		q.appendWaypoint(&result.Waypoints, q.spanCenter(path.refs[i+1]))
	}
	q.appendWaypoint(&result.Waypoints, goal.Projected)
	if navRouteWalkOnly(result.Steps) {
		result.Waypoints = q.simplifyWaypoints(start.Projected, result.Waypoints)
	}
	return result
}

func reverseNavSpanRefs(values []NavSpanRef) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseNavSpanTransitions(values []NavSpanTransitionDef) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

type navBlockerQueueItem struct {
	ref      NavSpanRef
	cost     float32
	estimate float32
}

type navBlockerQueue []navBlockerQueueItem

func (q navBlockerQueue) Len() int { return len(q) }
func (q navBlockerQueue) Less(i, j int) bool {
	if q[i].estimate != q[j].estimate {
		return q[i].estimate < q[j].estimate
	}
	if q[i].cost != q[j].cost {
		return q[i].cost < q[j].cost
	}
	return navSpanRefLess(q[i].ref, q[j].ref)
}
func (q navBlockerQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *navBlockerQueue) Push(value any) {
	*q = append(*q, value.(navBlockerQueueItem))
}
func (q *navBlockerQueue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]
	return last
}
