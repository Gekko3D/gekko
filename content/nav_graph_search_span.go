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

	spans := make(map[uint32]NavSpanDef, len(source.Spans))
	for _, span := range source.Spans {
		spans[span.ID] = span
	}
	accepted := make(map[uint32]struct{}, len(graph.SpanIDs))
	for _, id := range graph.SpanIDs {
		if _, ok := spans[id]; !ok {
			return NavSpanSearchResult{}, fmt.Errorf("navigation graph references missing source span %d", id)
		}
		accepted[id] = struct{}{}
	}
	if _, ok := accepted[start]; !ok {
		return NavSpanSearchResult{FailureReason: NavSpanSearchStartMissing}, nil
	}
	if _, ok := accepted[goal]; !ok {
		return NavSpanSearchResult{FailureReason: NavSpanSearchGoalMissing}, nil
	}
	if start == goal {
		return NavSpanSearchResult{Found: true, Spans: []uint32{start}}, nil
	}

	edges := make(map[uint32][]NavSpanTransitionDef)
	heuristicScale := float32(math.Inf(1))
	for _, transition := range graph.SpanTransitions {
		if transition.To.Tile == graph.Coord {
			edges[transition.From] = append(edges[transition.From], transition)
			distance := navSpanSearchDistance(spans[transition.From], spans[transition.To.Span], voxelResolution)
			if distance > 0 {
				heuristicScale = min(heuristicScale, transition.Cost/distance)
			}
		}
	}
	if !finite(heuristicScale) {
		heuristicScale = 0
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
			return NavSpanSearchResult{Found: true, Spans: path, Cost: costs[goal]}, nil
		}
		for _, edge := range edges[current.span] {
			if _, ok := accepted[edge.To.Span]; !ok {
				continue
			}
			cost := current.cost + edge.Cost
			previous, seen := costs[edge.To.Span]
			if seen && cost >= previous {
				continue
			}
			costs[edge.To.Span] = cost
			parents[edge.To.Span] = current.span
			heap.Push(&frontier, navSpanSearchQueueItem{
				span:     edge.To.Span,
				cost:     cost,
				estimate: cost + heuristicScale*navSpanSearchDistance(spans[edge.To.Span], spans[goal], voxelResolution),
			})
		}
	}
	return NavSpanSearchResult{FailureReason: NavSpanSearchNoRoute}, nil
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
