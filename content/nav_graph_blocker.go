package content

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
	"strings"
)

// NavBlockerDef is a temporary world-space obstacle over an immutable baked graph.
type NavBlockerDef struct {
	ID  string
	Min Vec3
	Max Vec3
}

// NavSupportHazardDef removes one physical walkable support from a query
// without depending on transient span IDs.
type NavSupportHazardDef struct {
	ID            string
	Tile          TerrainChunkCoordDef
	X, Z          int
	SupportHeight float32
}

// NewNavGraphQueryWithBlockers builds a query overlay for one agent profile.
// Blockers remove overlapping spans; they do not mutate or rebake graph tiles.
func NewNavGraphQueryWithBlockers(sources []NavSourceTileDef, graphs []NavGraphTileDef, chunkSize int, voxelResolution float32, profile NavAgentProfileDef, blockers []NavBlockerDef) (*NavGraphQuery, error) {
	return NewNavGraphQueryWithBlockersAndSupportHazards(sources, graphs, chunkSize, voxelResolution, profile, blockers, nil)
}

// NewNavGraphQueryWithBlockersAndSupportHazards builds a query overlay for
// obstacles and removed walkable supports.
func NewNavGraphQueryWithBlockersAndSupportHazards(sources []NavSourceTileDef, graphs []NavGraphTileDef, chunkSize int, voxelResolution float32, profile NavAgentProfileDef, blockers []NavBlockerDef, hazards []NavSupportHazardDef) (*NavGraphQuery, error) {
	query, err := newNavGraphQuery(sources, graphs, chunkSize, voxelResolution)
	if err != nil {
		return nil, err
	}
	if len(blockers) == 0 && len(hazards) == 0 {
		return newPublishedNavGraphQuery(query, nil), nil
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
	for _, hazard := range hazards {
		if strings.TrimSpace(hazard.ID) == "" {
			return nil, fmt.Errorf("navigation support hazard id is required")
		}
		if _, exists := seen[hazard.ID]; exists {
			return nil, fmt.Errorf("duplicate navigation overlay id %q", hazard.ID)
		}
		seen[hazard.ID] = struct{}{}
		if hazard.X < 0 || hazard.X >= chunkSize || hazard.Z < 0 || hazard.Z >= chunkSize || !finite(hazard.SupportHeight) {
			return nil, fmt.Errorf("navigation support hazard %q requires a valid cell and height", hazard.ID)
		}
	}
	query.enableBlockers(profile, blockers, hazards)
	return newPublishedNavGraphQuery(query, nil), nil
}

func (q *navGraphQuery) enableBlockers(profile NavAgentProfileDef, blockers []NavBlockerDef, hazards []NavSupportHazardDef) {
	q.blocked = make(map[TerrainChunkCoordDef][]uint64)
	affected := make(map[navRouteNode]struct{})
	block := func(coord TerrainChunkCoordDef, spanID uint32) {
		tile := q.tiles[coord]
		words := q.blocked[coord]
		if words == nil {
			words = make([]uint64, (len(tile.spans)+63)/64)
			q.blocked[coord] = words
		}
		bitSet(words, spanID)
		affected[navRouteNode{Tile: coord, Region: tile.spanRegions[spanID]}] = struct{}{}
	}
	tilesByColumn := make(map[[2]int][]TerrainChunkCoordDef)
	for coord := range q.tiles {
		key := [2]int{coord.X, coord.Z}
		tilesByColumn[key] = append(tilesByColumn[key], coord)
	}
	tileSize := float32(q.chunkSize) * q.voxelResolution
	for _, blocker := range blockers {
		minTileX := int(math.Floor(float64((blocker.Min[0] - profile.Radius) / tileSize)))
		minTileZ := int(math.Floor(float64((blocker.Min[2] - profile.Radius) / tileSize)))
		maxTileX := int(math.Ceil(float64((blocker.Max[0]+profile.Radius)/tileSize))) - 1
		maxTileZ := int(math.Ceil(float64((blocker.Max[2]+profile.Radius)/tileSize))) - 1
		for tileZ := minTileZ; tileZ <= maxTileZ; tileZ++ {
			for tileX := minTileX; tileX <= maxTileX; tileX++ {
				for _, coord := range tilesByColumn[[2]int{tileX, tileZ}] {
					tile := q.tiles[coord]
					tileMinX := float32(coord.X*q.chunkSize) * q.voxelResolution
					tileMinZ := float32(coord.Z*q.chunkSize) * q.voxelResolution
					minX := max(0, int(math.Floor(float64((blocker.Min[0]-profile.Radius-tileMinX)/q.voxelResolution))))
					minZ := max(0, int(math.Floor(float64((blocker.Min[2]-profile.Radius-tileMinZ)/q.voxelResolution))))
					maxX := min(q.chunkSize-1, int(math.Ceil(float64((blocker.Max[0]+profile.Radius-tileMinX)/q.voxelResolution)))-1)
					maxZ := min(q.chunkSize-1, int(math.Ceil(float64((blocker.Max[2]+profile.Radius-tileMinZ)/q.voxelResolution)))-1)
					for z := minZ; z <= maxZ; z++ {
						for x := minX; x <= maxX; x++ {
							for _, spanID := range tile.column(x, z, q.chunkSize) {
								span := tile.spans[spanID]
								if !navBlockerOverlapsSpan(blocker, profile, coord, q.chunkSize, q.voxelResolution, span) {
									continue
								}
								block(coord, spanID)
							}
						}
					}
				}
			}
		}
	}
	for _, hazard := range hazards {
		tile := q.tiles[hazard.Tile]
		if tile == nil {
			continue
		}
		for _, spanID := range tile.column(hazard.X, hazard.Z, q.chunkSize) {
			span := tile.spans[spanID]
			if bitHas(tile.accepted, spanID) && absFloat32(span.SupportHeight-hazard.SupportHeight) <= 1e-4 {
				block(hazard.Tile, spanID)
			}
		}
	}
	if len(q.blocked) == 0 {
		return
	}
	q.indexBlockerComponents(affected)
	q.indexActiveDegrees()
}

const navUnassignedComponent = ^uint32(0)

type navOverlayEdge struct {
	id   uint32
	from NavSpanRef
	edge NavSpanTransitionDef
}

func (q *navGraphQuery) indexBlockerComponents(affected map[navRouteNode]struct{}) {
	q.spanComponents = make(map[TerrainChunkCoordDef][]uint32, len(q.tiles))
	coords := make([]TerrainChunkCoordDef, 0, len(q.tiles))
	for coord, tile := range q.tiles {
		components := make([]uint32, len(tile.spans))
		for id := range components {
			components[id] = navUnassignedComponent
		}
		q.spanComponents[coord] = components
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool { return terrainCoordLess(coords[i], coords[j]) })

	baseline := make(map[navRouteNode]uint32)
	nextComponent := uint32(0)
	for _, coord := range coords {
		tile := q.tiles[coord]
		for spanIndex := range tile.spans {
			ref := NavSpanRef{Tile: coord, Span: uint32(spanIndex)}
			if !bitHas(tile.accepted, ref.Span) || q.isBlocked(ref) {
				continue
			}
			region := navRouteNode{Tile: coord, Region: tile.spanRegions[ref.Span]}
			if _, split := affected[region]; !split {
				component, ok := baseline[region]
				if !ok {
					component, nextComponent = nextComponent, nextComponent+1
					baseline[region] = component
				}
				q.spanComponents[coord][ref.Span] = component
				continue
			}
			if _, assigned := q.spanComponent(ref); assigned {
				continue
			}
			component := nextComponent
			nextComponent++
			q.spanComponents[coord][ref.Span] = component
			queue := []NavSpanRef{ref}
			for head := 0; head < len(queue); head++ {
				current := queue[head]
				q.visitSpanEdges(current, func(edge NavSpanTransitionDef) {
					toRegion, ok := q.spanRegion(edge.To)
					if !ok || edge.To.Tile != coord || toRegion != region.Region || q.isBlocked(edge.To) {
						return
					}
					if _, assigned := q.spanComponent(edge.To); assigned || !q.hasActiveEdge(edge.To, current) {
						return
					}
					q.spanComponents[coord][edge.To.Span] = component
					queue = append(queue, edge.To)
				})
			}
		}
	}

	q.componentEdges = make(map[uint32][]uint32)
	q.componentRoutes = make(map[uint32][]navOverlayEdge)
	seen := make(map[[2]uint32]struct{})
	edgeID := uint32(0)
	for _, coord := range coords {
		for fromID := range q.tiles[coord].spans {
			from := NavSpanRef{Tile: coord, Span: uint32(fromID)}
			fromComponent, ok := q.spanComponent(from)
			if !ok {
				continue
			}
			q.visitSpanEdges(from, func(edge NavSpanTransitionDef) {
				toComponent, ok := q.spanComponent(edge.To)
				if !ok || fromComponent == toComponent {
					return
				}
				q.componentRoutes[fromComponent] = append(q.componentRoutes[fromComponent], navOverlayEdge{id: edgeID, from: from, edge: edge})
				edgeID++
				key := [2]uint32{fromComponent, toComponent}
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
					q.componentEdges[fromComponent] = append(q.componentEdges[fromComponent], toComponent)
				}
			})
		}
	}
}

func (q *navGraphQuery) isBlocked(ref NavSpanRef) bool {
	return bitHas(q.blocked[ref.Tile], ref.Span)
}

func (q *navGraphQuery) spanComponent(ref NavSpanRef) (uint32, bool) {
	components := q.spanComponents[ref.Tile]
	if int(ref.Span) >= len(components) || components[ref.Span] == navUnassignedComponent {
		return 0, false
	}
	return components[ref.Span], true
}

func (q *navGraphQuery) hasActiveEdge(from, to NavSpanRef) bool {
	found := false
	q.visitSpanEdges(from, func(edge NavSpanTransitionDef) { found = found || edge.To == to })
	return found
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
	span, _ := q.span(ref)
	return navSpanPathCell{x: ref.Tile.X*q.chunkSize + span.X, z: ref.Tile.Z*q.chunkSize + span.Z, height: math.Float32bits(span.SupportHeight)}
}

type navOverlayState struct {
	component uint32
	point     Vec3
	via       navOverlayEdge
	parent    uint32
}

func (q *navGraphQuery) findBlockerRoute(start, goal navResolvedSpan) NavRouteResult {
	startComponent, startOK := q.spanComponent(start.Ref)
	goalComponent, goalOK := q.spanComponent(goal.Ref)
	if !startOK || !goalOK {
		return NavRouteResult{FailureReason: NavRouteNoRoute, FailureTile: goal.Ref.Tile}
	}
	states := map[uint32]navOverlayState{0: {component: startComponent, point: start.Projected}}
	costs := map[uint32]float32{0: 0}
	frontier := navOverlayQueue{{key: 0, estimate: navVec3Distance(start.Projected, goal.Projected)}}
	heap.Init(&frontier)
	goalKey := uint32(0)
	found := false
	for frontier.Len() > 0 {
		current := heap.Pop(&frontier).(navOverlayQueueItem)
		if current.cost != costs[current.key] {
			continue
		}
		state := states[current.key]
		if state.component == goalComponent {
			goalKey, found = current.key, true
			break
		}
		for _, edge := range q.componentRoutes[state.component] {
			toComponent, ok := q.spanComponent(edge.edge.To)
			if !ok {
				continue
			}
			entry := q.spanTransitionEntry(edge.from.Tile, edge.edge)
			exit := q.spanTransitionTarget(edge.from.Tile, edge.edge)
			cost := current.cost + navVec3Distance(state.point, entry) + edge.edge.Cost
			key := edge.id + 1
			if previous, seen := costs[key]; seen && cost >= previous {
				continue
			}
			costs[key] = cost
			states[key] = navOverlayState{component: toComponent, point: exit, via: edge, parent: current.key}
			heap.Push(&frontier, navOverlayQueueItem{key: key, cost: cost, estimate: cost + navVec3Distance(exit, goal.Projected)})
		}
	}
	if !found {
		return NavRouteResult{FailureReason: NavRouteNoRoute, FailureTile: goal.Ref.Tile}
	}
	var crossings []navOverlayEdge
	for key := goalKey; key != 0; key = states[key].parent {
		crossings = append(crossings, states[key].via)
	}
	for left, right := 0, len(crossings)-1; left < right; left, right = left+1, right-1 {
		crossings[left], crossings[right] = crossings[right], crossings[left]
	}
	return q.refineBlockerRoute(start, goal, startComponent, crossings)
}

func (q *navGraphQuery) refineBlockerRoute(start, goal navResolvedSpan, component uint32, crossings []navOverlayEdge) NavRouteResult {
	result := NavRouteResult{Found: true, Steps: []NavRouteStep{{
		Tile: start.Region.Tile, Region: start.Region.Region, Target: start.Projected, TraversalWaypoint: -1,
	}}}
	current := start.Ref
	for _, crossing := range crossings {
		path := q.findComponentSpanRoute(component, current, crossing.from)
		if !path.Found {
			return NavRouteResult{FailureReason: NavRouteNoRoute, FailureTile: crossing.from.Tile}
		}
		q.appendOverlaySpanPath(&result, current.Tile, path.Spans, 1)
		fromRegion, _ := q.spanRegion(crossing.from)
		toRegion, _ := q.spanRegion(crossing.edge.To)
		fromNode := navRouteNode{Tile: crossing.from.Tile, Region: fromRegion}
		toNode := navRouteNode{Tile: crossing.edge.To.Tile, Region: toRegion}
		step := -1
		if fromNode != toNode || crossing.edge.Traversal != nil || crossing.edge.Kind != NavTransitionWalk {
			step = len(result.Steps)
			result.Steps = append(result.Steps, NavRouteStep{
				Tile: toNode.Tile, Region: toNode.Region, Target: q.spanTransitionTarget(crossing.from.Tile, crossing.edge),
				RequiredAction: crossing.edge.Kind, Traversal: cloneNavTraversal(crossing.edge.Traversal),
				Gate: cloneNavTransitionGate(crossing.edge.Gate), TraversalWaypoint: -1,
			})
		}
		if crossing.edge.Traversal != nil {
			waypoint := q.appendWaypointIndex(&result, crossing.edge.Traversal.Start, crossing.from)
			q.appendWaypoint(&result, crossing.edge.Traversal.End, crossing.edge.To)
			if step >= 0 {
				result.Steps[step].TraversalWaypoint = waypoint
			}
		} else {
			q.appendWaypoint(&result, q.spanTransitionTarget(crossing.from.Tile, crossing.edge), crossing.edge.To)
		}
		current = crossing.edge.To
		component, _ = q.spanComponent(current)
	}
	path := q.findComponentSpanRoute(component, current, goal.Ref)
	if !path.Found {
		return NavRouteResult{FailureReason: NavRouteNoRoute, FailureTile: goal.Ref.Tile}
	}
	q.appendOverlaySpanPath(&result, current.Tile, path.Spans, 1)
	q.appendWaypoint(&result, goal.Projected, goal.Ref)
	if navRouteWalkOnly(result.Steps) {
		q.simplifyWaypoints(start.Projected, &result)
	}
	return result
}

func (q *navGraphQuery) appendOverlaySpanPath(route *NavRouteResult, tile TerrainChunkCoordDef, spans []uint32, skip int) {
	if skip >= len(spans) {
		return
	}
	for _, span := range spans[skip:] {
		ref := NavSpanRef{Tile: tile, Span: span}
		q.appendWaypoint(route, q.spanCenter(ref), ref)
	}
}

type navOverlayQueueItem struct {
	key      uint32
	cost     float32
	estimate float32
}

type navOverlayQueue []navOverlayQueueItem

func (q navOverlayQueue) Len() int { return len(q) }
func (q navOverlayQueue) Less(i, j int) bool {
	if q[i].estimate != q[j].estimate {
		return q[i].estimate < q[j].estimate
	}
	if q[i].cost != q[j].cost {
		return q[i].cost < q[j].cost
	}
	return q[i].key < q[j].key
}
func (q navOverlayQueue) Swap(i, j int)   { q[i], q[j] = q[j], q[i] }
func (q *navOverlayQueue) Push(value any) { *q = append(*q, value.(navOverlayQueueItem)) }
func (q *navOverlayQueue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]
	return last
}
