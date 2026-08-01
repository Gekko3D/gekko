package content

import (
	"container/heap"
	"fmt"
	"math"
)

const (
	NavSpanSearchStartMissing = "start_span_missing"
	NavSpanSearchGoalMissing  = "goal_span_missing"
	NavSpanSearchNoRoute      = "no_route"
)

type NavSpanSearchResult struct {
	Found         bool
	Spans         []uint32
	Cost          float32
	FailureReason string
}

type navSpanSearchEdge struct {
	to   uint32
	cost float32
}

// FindNavSpanRoute runs local A* over one graph tile.
func FindNavSpanRoute(source NavSourceTileDef, graph NavGraphTileDef, voxelResolution float32, start, goal uint32) (NavSpanSearchResult, error) {
	if validation := ValidateNavSourceTile(&source); validation.HasErrors() {
		return NavSpanSearchResult{}, fmt.Errorf("invalid navigation source tile: %s", validation.Error())
	}
	if validation := ValidateNavGraphTile(&graph); validation.HasErrors() {
		return NavSpanSearchResult{}, fmt.Errorf("invalid navigation graph tile: %s", validation.Error())
	}
	if !finite(voxelResolution) || voxelResolution <= 0 {
		return NavSpanSearchResult{}, fmt.Errorf("navigation graph voxel resolution must be finite and positive")
	}
	if source.NavID != graph.NavID || source.Coord != graph.Coord || source.BuilderVersion != graph.BuilderVersion || source.SourceHash != graph.SourceHash || source.DependencyHash != graph.DependencyHash {
		return NavSpanSearchResult{}, fmt.Errorf("navigation source and graph tile metadata do not match")
	}

	tile, err := newNavResidentTile(source, graph, source.ChunkSize, voxelResolution)
	if err != nil {
		return NavSpanSearchResult{}, err
	}
	if !bitHas(tile.accepted, start) {
		return NavSpanSearchResult{FailureReason: NavSpanSearchStartMissing}, nil
	}
	if !bitHas(tile.accepted, goal) {
		return NavSpanSearchResult{FailureReason: NavSpanSearchGoalMissing}, nil
	}
	if start == goal {
		return NavSpanSearchResult{Found: true, Spans: []uint32{start}}, nil
	}

	return findNavSpanRouteIndexed(tile, voxelResolution, start, goal,
		func(id uint32) bool { return bitHas(tile.accepted, id) }), nil
}

func (q *navGraphQuery) findSpanRoute(tile TerrainChunkCoordDef, region, start, goal uint32) NavSpanSearchResult {
	resident := q.tile(tile)
	if resident == nil {
		return NavSpanSearchResult{FailureReason: NavSpanSearchStartMissing}
	}
	return findNavSpanRouteIndexed(resident, q.voxelResolution, start, goal,
		func(id uint32) bool { return int(id) < len(resident.spanRegions) && resident.spanRegions[id] == region })
}

func (q *navGraphQuery) findComponentSpanRoute(component uint32, start, goal NavSpanRef) NavSpanSearchResult {
	if start.Tile != goal.Tile {
		return NavSpanSearchResult{FailureReason: NavSpanSearchNoRoute}
	}
	resident := q.tile(start.Tile)
	if resident == nil {
		return NavSpanSearchResult{FailureReason: NavSpanSearchStartMissing}
	}
	return findNavSpanRouteIndexed(resident, q.voxelResolution, start.Span, goal.Span, func(id uint32) bool {
		value, ok := q.spanComponent(NavSpanRef{Tile: start.Tile, Span: id})
		return ok && value == component
	})
}

func findNavSpanRouteIndexed(tile *navResidentTile, voxelResolution float32, start, goal uint32, accepted func(uint32) bool) NavSpanSearchResult {
	spans, heuristicScale := tile.spans, tile.spanScale
	if !accepted(start) {
		return NavSpanSearchResult{FailureReason: NavSpanSearchStartMissing}
	}
	if !accepted(goal) {
		return NavSpanSearchResult{FailureReason: NavSpanSearchGoalMissing}
	}
	if start == goal {
		return NavSpanSearchResult{Found: true, Spans: []uint32{start}}
	}
	frontier := navSpanSearchQueue{{span: start, estimate: heuristicScale * navSpanSearchDistance(spans[start], spans[goal], voxelResolution)}}
	heap.Init(&frontier)
	costs := map[uint32]float32{start: 0}
	parents := make(map[uint32]uint32)
	for frontier.Len() > 0 {
		current := heap.Pop(&frontier).(navSpanSearchQueueItem)
		if current.cost != costs[current.span] {
			continue
		}
		if current.span == goal {
			path := []uint32{goal}
			for path[len(path)-1] != start {
				path = append(path, parents[path[len(path)-1]])
			}
			for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
				path[left], path[right] = path[right], path[left]
			}
			return NavSpanSearchResult{Found: true, Spans: path, Cost: costs[goal]}
		}
		for _, edge := range tile.localEdges[tile.localOffsets[current.span]:tile.localOffsets[current.span+1]] {
			relaxNavSpanSearchEdge(&frontier, costs, parents, spans, accepted, heuristicScale, voxelResolution, current, goal,
				navSpanSearchEdge{to: edge.to, cost: navDerivedLocalCost(spans[current.span], spans[edge.to], voxelResolution)})
		}
		for _, edge := range tile.exceptional[tile.exceptionalOffsets[current.span]:tile.exceptionalOffsets[current.span+1]] {
			if edge.To.Tile == tile.coord {
				relaxNavSpanSearchEdge(&frontier, costs, parents, spans, accepted, heuristicScale, voxelResolution, current, goal,
					navSpanSearchEdge{to: edge.To.Span, cost: edge.Cost})
			}
		}
	}
	return NavSpanSearchResult{FailureReason: NavSpanSearchNoRoute}
}

func relaxNavSpanSearchEdge(frontier *navSpanSearchQueue, costs map[uint32]float32, parents map[uint32]uint32, spans []NavSpanDef, accepted func(uint32) bool, heuristicScale, voxelResolution float32, current navSpanSearchQueueItem, goal uint32, edge navSpanSearchEdge) {
	if !accepted(edge.to) {
		return
	}
	cost := current.cost + edge.cost
	if previous, seen := costs[edge.to]; seen && cost >= previous {
		return
	}
	costs[edge.to] = cost
	parents[edge.to] = current.span
	heap.Push(frontier, navSpanSearchQueueItem{span: edge.to, cost: cost, estimate: cost + heuristicScale*navSpanSearchDistance(spans[edge.to], spans[goal], voxelResolution)})
}

func navSpanSearchDistance(from, to NavSpanDef, voxelResolution float32) float32 {
	dx := float64(from.X-to.X) * float64(voxelResolution)
	dy := float64(from.SupportHeight - to.SupportHeight)
	dz := float64(from.Z-to.Z) * float64(voxelResolution)
	return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
}

type navSpanSearchQueueItem struct {
	span     uint32
	cost     float32
	estimate float32
}

type navSpanSearchQueue []navSpanSearchQueueItem

func (q navSpanSearchQueue) Len() int { return len(q) }
func (q navSpanSearchQueue) Less(i, j int) bool {
	if q[i].estimate != q[j].estimate {
		return q[i].estimate < q[j].estimate
	}
	if q[i].cost != q[j].cost {
		return q[i].cost < q[j].cost
	}
	return q[i].span < q[j].span
}
func (q navSpanSearchQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *navSpanSearchQueue) Push(value any) {
	*q = append(*q, value.(navSpanSearchQueueItem))
}
func (q *navSpanSearchQueue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]
	return last
}
