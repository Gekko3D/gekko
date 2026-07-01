package content

import (
	"container/heap"
	"fmt"
	"sort"
)

const NavClearanceCoarseEdgeSourcePortal = "region_portal"

type NavClearanceCoarseGraphOptions struct {
	MaxRegionSearch int
}

type NavClearanceCoarseGraph struct {
	Nodes map[string]NavClearanceCoarseGraphNode
	Edges map[string][]NavClearanceCoarseGraphEdge
}

type NavClearanceCoarseGraphNode struct {
	ID     string
	Coord  TerrainChunkCoordDef
	Region NavClearanceLocalRegionDef
}

type NavClearanceCoarseGraphEdge struct {
	From      string
	To        string
	FromCoord TerrainChunkCoordDef
	ToCoord   TerrainChunkCoordDef
	Kind      string
	Source    string
	Cost      float32
	Portal    NavClearanceRegionPortalDef
	Width     float32
	Required  float32
	Clearance float32
	Position  Vec3
	Traversal string
}

type NavClearanceCoarsePathResult struct {
	Found bool
	Steps []NavClearanceRegionPathStep
	Edges []NavClearanceCoarseGraphEdge
	Cost  float32
}

type navClearanceCoarsePathNode struct {
	ID    string
	Cost  float32
	Score float32
	Index int
}

type navClearanceCoarsePathQueue []*navClearanceCoarsePathNode

func BuildNavClearanceCoarseGraph(results map[TerrainChunkCoordDef]*NavClearanceLocalRegionBuildResult) (NavClearanceCoarseGraph, error) {
	graph := NavClearanceCoarseGraph{
		Nodes: map[string]NavClearanceCoarseGraphNode{},
		Edges: map[string][]NavClearanceCoarseGraphEdge{},
	}
	coords := sortedNavClearanceLocalRegionResultCoords(results)
	for _, coord := range coords {
		result := results[coord]
		if result == nil {
			continue
		}
		for _, region := range result.Regions {
			if region.ID == "" {
				return NavClearanceCoarseGraph{}, fmt.Errorf("nav clearance region in %s has empty id", TerrainChunkKey(coord))
			}
			if _, exists := graph.Nodes[region.ID]; exists {
				return NavClearanceCoarseGraph{}, fmt.Errorf("duplicate nav clearance region id %q", region.ID)
			}
			graph.Nodes[region.ID] = NavClearanceCoarseGraphNode{
				ID:     region.ID,
				Coord:  coord,
				Region: region,
			}
		}
	}
	for _, coord := range coords {
		result := results[coord]
		if result == nil {
			continue
		}
		for _, region := range result.Regions {
			for _, portal := range region.Portals {
				if portal.FromRegionID == "" {
					portal.FromRegionID = region.ID
				}
				from, fromOK := graph.Nodes[portal.FromRegionID]
				to, toOK := graph.Nodes[portal.ToRegionID]
				if !fromOK || !toOK {
					continue
				}
				graph.addEdge(navClearanceCoarseGraphEdge(from, to, portal))
			}
		}
	}
	graph.sortEdges()
	return graph, nil
}

func FindNavClearanceCoarseRegionPath(graph NavClearanceCoarseGraph, startRegionID string, endRegionID string, opts NavClearanceCoarseGraphOptions) NavClearanceCoarsePathResult {
	if _, ok := graph.Nodes[startRegionID]; !ok {
		return NavClearanceCoarsePathResult{}
	}
	if _, ok := graph.Nodes[endRegionID]; !ok {
		return NavClearanceCoarsePathResult{}
	}
	if startRegionID == endRegionID {
		node := graph.Nodes[startRegionID]
		return NavClearanceCoarsePathResult{
			Found: true,
			Steps: []NavClearanceRegionPathStep{{
				Coord:      node.Coord,
				RegionID:   node.ID,
				Area:       node.Region.Area,
				BoundsMin:  node.Region.BoundsMin,
				BoundsMax:  node.Region.BoundsMax,
				Centroid:   node.Region.Centroid,
				Projection: node.Region.Projection,
			}},
		}
	}
	maxSearch := opts.MaxRegionSearch
	if maxSearch <= 0 || maxSearch > len(graph.Nodes) {
		maxSearch = len(graph.Nodes)
	}
	dist := map[string]float32{startRegionID: 0}
	prev := map[string]string{}
	prevEdge := map[string]NavClearanceCoarseGraphEdge{}
	visited := map[string]struct{}{}
	queue := &navClearanceCoarsePathQueue{}
	heap.Init(queue)
	heap.Push(queue, &navClearanceCoarsePathNode{ID: startRegionID})
	for queue.Len() > 0 && len(visited) < maxSearch {
		current := heap.Pop(queue).(*navClearanceCoarsePathNode)
		if _, ok := visited[current.ID]; ok {
			continue
		}
		visited[current.ID] = struct{}{}
		if current.ID == endRegionID {
			return navClearanceCoarseBuildPathResult(graph, startRegionID, endRegionID, dist[endRegionID], prev, prevEdge)
		}
		for _, edge := range graph.Edges[current.ID] {
			if _, ok := visited[edge.To]; ok {
				continue
			}
			nextCost := dist[current.ID] + edge.Cost
			existing, seen := dist[edge.To]
			if seen && nextCost >= existing {
				continue
			}
			dist[edge.To] = nextCost
			prev[edge.To] = current.ID
			prevEdge[edge.To] = edge
			heuristic := navClearanceCoarseRegionDistance(graph.Nodes[edge.To].Region, graph.Nodes[endRegionID].Region)
			heap.Push(queue, &navClearanceCoarsePathNode{
				ID:    edge.To,
				Cost:  nextCost,
				Score: nextCost + heuristic,
			})
		}
	}
	return NavClearanceCoarsePathResult{}
}

func (graph NavClearanceCoarseGraph) addEdge(edge NavClearanceCoarseGraphEdge) {
	if edge.Kind == "" {
		edge.Kind = NavClearanceRegionPortalWalk
	}
	if edge.Source == "" {
		edge.Source = NavClearanceCoarseEdgeSourcePortal
	}
	if edge.Cost <= 0 {
		edge.Cost = 1
	}
	graph.Edges[edge.From] = append(graph.Edges[edge.From], edge)
}

func (graph NavClearanceCoarseGraph) sortEdges() {
	for from := range graph.Edges {
		sort.Slice(graph.Edges[from], func(i, j int) bool {
			a := graph.Edges[from][i]
			b := graph.Edges[from][j]
			if a.Cost != b.Cost {
				return a.Cost < b.Cost
			}
			if a.Kind != b.Kind {
				return a.Kind < b.Kind
			}
			if a.Source != b.Source {
				return a.Source < b.Source
			}
			return a.To < b.To
		})
	}
}

func navClearanceCoarseGraphEdge(from NavClearanceCoarseGraphNode, to NavClearanceCoarseGraphNode, portal NavClearanceRegionPortalDef) NavClearanceCoarseGraphEdge {
	kind := firstNonEmptyNavString(portal.Kind, NavClearanceRegionPortalWalk)
	return NavClearanceCoarseGraphEdge{
		From:      from.ID,
		To:        to.ID,
		FromCoord: from.Coord,
		ToCoord:   to.Coord,
		Kind:      kind,
		Source:    NavClearanceCoarseEdgeSourcePortal,
		Cost:      navClearanceCoarseEdgeCost(from.Region, to.Region, portal),
		Portal:    portal,
		Width:     portal.Width,
		Required:  portal.RequiredWidth,
		Clearance: portal.ClearanceRadius,
		Position:  portal.Position,
		Traversal: kind,
	}
}

func navClearanceCoarseEdgeCost(from NavClearanceLocalRegionDef, to NavClearanceLocalRegionDef, portal NavClearanceRegionPortalDef) float32 {
	cost := navVec3Distance(from.Centroid, portal.Position) + navVec3Distance(portal.Position, to.Centroid)
	if cost <= 0 {
		cost = navClearanceCoarseRegionDistance(from, to)
	}
	if cost <= 0 {
		return 1
	}
	return cost
}

func navClearanceCoarseRegionDistance(a NavClearanceLocalRegionDef, b NavClearanceLocalRegionDef) float32 {
	return navVec3Distance(a.Centroid, b.Centroid)
}

func navClearanceCoarseBuildPathResult(graph NavClearanceCoarseGraph, startRegionID string, endRegionID string, cost float32, prev map[string]string, prevEdge map[string]NavClearanceCoarseGraphEdge) NavClearanceCoarsePathResult {
	ids := []string{endRegionID}
	edges := []NavClearanceCoarseGraphEdge{}
	for ids[len(ids)-1] != startRegionID {
		current := ids[len(ids)-1]
		from, ok := prev[current]
		if !ok {
			return NavClearanceCoarsePathResult{}
		}
		ids = append(ids, from)
		edges = append(edges, prevEdge[current])
	}
	reverseNavClearanceCoarseIDs(ids)
	reverseNavClearanceCoarseEdges(edges)
	steps := make([]NavClearanceRegionPathStep, 0, len(ids))
	for _, id := range ids {
		node, ok := graph.Nodes[id]
		if !ok {
			return NavClearanceCoarsePathResult{}
		}
		steps = append(steps, NavClearanceRegionPathStep{
			Coord:      node.Coord,
			RegionID:   node.ID,
			Area:       node.Region.Area,
			BoundsMin:  node.Region.BoundsMin,
			BoundsMax:  node.Region.BoundsMax,
			Centroid:   node.Region.Centroid,
			Projection: node.Region.Projection,
		})
	}
	return NavClearanceCoarsePathResult{
		Found: true,
		Steps: steps,
		Edges: edges,
		Cost:  cost,
	}
}

func sortedNavClearanceLocalRegionResultCoords(results map[TerrainChunkCoordDef]*NavClearanceLocalRegionBuildResult) []TerrainChunkCoordDef {
	coords := make([]TerrainChunkCoordDef, 0, len(results))
	for coord := range results {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		return terrainChunkCoordLess(coords[i], coords[j])
	})
	return coords
}

func reverseNavClearanceCoarseIDs(values []string) {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
}

func reverseNavClearanceCoarseEdges(values []NavClearanceCoarseGraphEdge) {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
}

func (q navClearanceCoarsePathQueue) Len() int { return len(q) }

func (q navClearanceCoarsePathQueue) Less(i, j int) bool {
	if q[i].Score != q[j].Score {
		return q[i].Score < q[j].Score
	}
	if q[i].Cost != q[j].Cost {
		return q[i].Cost < q[j].Cost
	}
	return q[i].ID < q[j].ID
}

func (q navClearanceCoarsePathQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].Index = i
	q[j].Index = j
}

func (q *navClearanceCoarsePathQueue) Push(x interface{}) {
	node := x.(*navClearanceCoarsePathNode)
	node.Index = len(*q)
	*q = append(*q, node)
}

func (q *navClearanceCoarsePathQueue) Pop() interface{} {
	old := *q
	n := len(old)
	node := old[n-1]
	old[n-1] = nil
	node.Index = -1
	*q = old[0 : n-1]
	return node
}
