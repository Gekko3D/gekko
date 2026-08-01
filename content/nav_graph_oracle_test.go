package content

import (
	"container/heap"
	"math"
	"testing"
)

type navDijkstraItem struct {
	ref  NavSpanRef
	cost float32
}

type navDijkstraQueue []navDijkstraItem

func (q navDijkstraQueue) Len() int           { return len(q) }
func (q navDijkstraQueue) Less(i, j int) bool { return q[i].cost < q[j].cost }
func (q navDijkstraQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *navDijkstraQueue) Push(value any)    { *q = append(*q, value.(navDijkstraItem)) }
func (q *navDijkstraQueue) Pop() any {
	last := len(*q) - 1
	value := (*q)[last]
	*q = (*q)[:last]
	return value
}

// navDijkstraReachability is deliberately slow and test-only. It reads graph
// transitions directly so route optimizations cannot share its search index.
func navDijkstraReachability(sources []NavSourceTileDef, graphs []NavGraphTileDef, chunkSize int, voxelResolution float32, profile NavAgentProfileDef, blockers []NavBlockerDef, start, goal NavSpanRef) (float32, bool) {
	spans := make(map[NavSpanRef]NavSpanDef)
	for _, source := range sources {
		for _, span := range source.Spans {
			spans[NavSpanRef{Tile: source.Coord, Span: span.ID}] = span
		}
	}
	blocked := make(map[NavSpanRef]struct{})
	for ref, span := range spans {
		for _, blocker := range blockers {
			if navBlockerOverlapsSpan(blocker, profile, ref.Tile, chunkSize, voxelResolution, span) {
				blocked[ref] = struct{}{}
				break
			}
		}
	}
	if _, blocked := blocked[start]; blocked {
		return 0, false
	}
	if _, blocked := blocked[goal]; blocked {
		return 0, false
	}
	edges := make(map[NavSpanRef][]NavSpanTransitionDef)
	for _, graph := range graphs {
		for _, edge := range graph.SpanTransitions {
			from := NavSpanRef{Tile: graph.Coord, Span: edge.From}
			to := edge.To
			if _, fromOK := spans[from]; !fromOK {
				continue
			}
			if _, toOK := spans[to]; !toOK {
				continue
			}
			edges[from] = append(edges[from], edge)
		}
	}

	frontier := navDijkstraQueue{{ref: start}}
	heap.Init(&frontier)
	costs := map[NavSpanRef]float32{start: 0}
	for frontier.Len() > 0 {
		current := heap.Pop(&frontier).(navDijkstraItem)
		if current.cost != costs[current.ref] {
			continue
		}
		if current.ref == goal {
			return current.cost, true
		}
		for _, edge := range edges[current.ref] {
			if _, blocked := blocked[edge.To]; blocked {
				continue
			}
			cost := current.cost + edge.Cost
			if previous, seen := costs[edge.To]; seen && cost >= previous {
				continue
			}
			costs[edge.To] = cost
			heap.Push(&frontier, navDijkstraItem{ref: edge.To, cost: cost})
		}
	}
	return 0, false
}

func TestNavGraphRouteReachabilityMatchesDijkstra(t *testing.T) {
	const chunkSize = 7
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.1, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	sources, graphs := buildFlatNavRouteWorld(t, []TerrainChunkCoordDef{{}}, chunkSize, profile)
	start := NavSpanRef{Span: 2}
	goal := NavSpanRef{Span: 6*chunkSize + 2}
	startPoint, goalPoint := Vec3{0.1, 0.2, 2.5}, Vec3{6.9, 0.2, 2.5}

	cases := []struct {
		name     string
		blockers []NavBlockerDef
	}{
		{name: "ordinary"},
		{name: "detour", blockers: []NavBlockerDef{{ID: "crate", Min: Vec3{3.25, 0, 2.25}, Max: Vec3{3.75, 1, 2.75}}}},
		{name: "split", blockers: []NavBlockerDef{{ID: "wall", Min: Vec3{3.25, 0, 0}, Max: Vec3{3.75, 1, 7}}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var route NavRouteResult
			var err error
			if len(test.blockers) == 0 {
				route, err = FindNavGraphRoute(sources, graphs, chunkSize, 1, startPoint, goalPoint)
			} else {
				query, queryErr := NewNavGraphQueryWithBlockers(sources, graphs, chunkSize, 1, profile, test.blockers)
				if queryErr != nil {
					t.Fatal(queryErr)
				}
				route, err = query.FindRoute(startPoint, goalPoint)
			}
			if err != nil {
				t.Fatal(err)
			}
			cost, found := navDijkstraReachability(sources, graphs, chunkSize, 1, profile, test.blockers, start, goal)
			if route.Found != found {
				t.Fatalf("route found=%t, Dijkstra found=%t cost=%g route=%+v", route.Found, found, cost, route)
			}
			if found && (math.IsNaN(float64(cost)) || math.IsInf(float64(cost), 0) || cost <= 0) {
				t.Fatalf("invalid Dijkstra cost %g", cost)
			}
		})
	}
	t.Run("cross_tile", func(t *testing.T) {
		crossTileSources, crossTileGraphs := buildFlatNavRouteWorld(t, []TerrainChunkCoordDef{{}, {X: 1}}, 4, profile)
		route, err := FindNavGraphRoute(crossTileSources, crossTileGraphs, 4, 1, Vec3{0.5, 0, 0.5}, Vec3{7.5, 0, 0.5})
		if err != nil {
			t.Fatal(err)
		}
		cost, found := navDijkstraReachability(
			crossTileSources, crossTileGraphs, 4, 1, profile, nil,
			NavSpanRef{Tile: TerrainChunkCoordDef{}, Span: 0}, NavSpanRef{Tile: TerrainChunkCoordDef{X: 1}, Span: 12},
		)
		if !route.Found || !found || cost <= 0 {
			t.Fatalf("cross-tile route=%+v Dijkstra=%t/%g", route, found, cost)
		}
	})
}

func TestNavGraphBlockerRoutesMatchDijkstraGeneratedCases(t *testing.T) {
	const chunkSize = 9
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.1, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	sources, graphs := buildFlatNavRouteWorld(t, []TerrainChunkCoordDef{{}}, chunkSize, profile)
	start, goal := NavSpanRef{}, NavSpanRef{Span: chunkSize*chunkSize - 1}
	for x := 1; x < chunkSize-1; x++ {
		for z := 0; z < chunkSize; z += 2 {
			blockers := []NavBlockerDef{{
				ID: "generated", Min: Vec3{float32(x) + 0.25, 0, float32(z) + 0.25},
				Max: Vec3{float32(x) + 0.75, 1, float32(min(z+2, chunkSize)) - 0.25},
			}}
			query, err := NewNavGraphQueryWithBlockers(sources, graphs, chunkSize, 1, profile, blockers)
			if err != nil {
				t.Fatal(err)
			}
			route, err := query.FindRoute(Vec3{0.5, 0, 0.5}, Vec3{8.5, 0, 8.5})
			if err != nil {
				t.Fatal(err)
			}
			_, found := navDijkstraReachability(sources, graphs, chunkSize, 1, profile, blockers, start, goal)
			if route.Found != found {
				t.Fatalf("blocker x=%d z=%d route=%t oracle=%t", x, z, route.Found, found)
			}
		}
	}
}
