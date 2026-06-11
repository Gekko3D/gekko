package content

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	NavSectorEdgeSourceAdjacentRefs    = "adjacent_sector_refs"
	NavSectorEdgeSourceBoundsAdjacency = "bounds_adjacency"
	NavSectorEdgeSourceLink            = "sector_link"
)

type NavSectorGraphOptions struct {
	DisableBoundsAdjacencyInference bool
}

type NavSectorPathOptions struct {
	AllowedKinds    []string
	AgentTags       []string
	AgentSpeed      float32
	MaxSectorSearch int
}

type NavSectorGraph struct {
	Sectors map[TerrainChunkCoordDef]NavSectorEntryDef
	Edges   map[TerrainChunkCoordDef][]NavSectorGraphEdge
}

type NavSectorGraphEdge struct {
	From        TerrainChunkCoordDef
	To          TerrainChunkCoordDef
	Kind        string
	Cost        float32
	Source      string
	LinkID      string
	Openable    bool
	RequiresTag string
	Tags        []string
}

type NavSectorPathResult struct {
	Found                  bool
	SectorCoords           []TerrainChunkCoordDef
	Edges                  []NavSectorGraphEdge
	Cost                   float32
	EstimatedTravelSeconds float32
}

func BuildNavSectorGraph(manifest *NavManifestDef, opts NavSectorGraphOptions) (NavSectorGraph, error) {
	if manifest == nil {
		return NavSectorGraph{}, fmt.Errorf("nav manifest is nil")
	}
	EnsureNavManifestDefaults(manifest)
	graph := NavSectorGraph{
		Sectors: make(map[TerrainChunkCoordDef]NavSectorEntryDef, len(manifest.Sectors)),
		Edges:   make(map[TerrainChunkCoordDef][]NavSectorGraphEdge, len(manifest.Sectors)),
	}
	for _, sector := range manifest.Sectors {
		if _, exists := graph.Sectors[sector.Coord]; exists {
			return NavSectorGraph{}, fmt.Errorf("duplicate nav sector coord %s", TerrainChunkKey(sector.Coord))
		}
		graph.Sectors[sector.Coord] = sector
	}
	for _, sector := range manifest.Sectors {
		for _, to := range sector.AdjacentSectorRefs {
			if _, ok := graph.Sectors[to]; !ok {
				return NavSectorGraph{}, fmt.Errorf("nav sector %s references missing adjacent sector %s", TerrainChunkKey(sector.Coord), TerrainChunkKey(to))
			}
			graph.addEdge(navSectorGraphEdgeFromRefs(graph, sector.Coord, to, NavSectorEdgeSourceAdjacentRefs))
		}
		for _, link := range sector.Links {
			if _, ok := graph.Sectors[link.To]; !ok {
				return NavSectorGraph{}, fmt.Errorf("nav sector %s link references missing sector %s", TerrainChunkKey(sector.Coord), TerrainChunkKey(link.To))
			}
			graph.addEdge(navSectorGraphEdgeFromLink(graph, sector.Coord, link))
		}
	}
	if !opts.DisableBoundsAdjacencyInference {
		graph.addBoundsAdjacencyEdges()
	}
	graph.sortEdges()
	return graph, nil
}

func FindNavSectorContainingPoint(manifest *NavManifestDef, point Vec3) (TerrainChunkCoordDef, bool) {
	if manifest == nil {
		return TerrainChunkCoordDef{}, false
	}
	EnsureNavManifestDefaults(manifest)
	sectors := append([]NavSectorEntryDef(nil), manifest.Sectors...)
	sort.Slice(sectors, func(i, j int) bool {
		return terrainChunkCoordLess(sectors[i].Coord, sectors[j].Coord)
	})
	for _, sector := range sectors {
		if navPointInBounds(point, sector.BoundsMin, sector.BoundsMax) {
			return sector.Coord, true
		}
	}
	return TerrainChunkCoordDef{}, false
}

func FindNavSectorPathBetweenPoints(manifest *NavManifestDef, start Vec3, end Vec3, opts NavSectorPathOptions) (NavSectorPathResult, error) {
	startCoord, ok := FindNavSectorContainingPoint(manifest, start)
	if !ok {
		return NavSectorPathResult{}, nil
	}
	endCoord, ok := FindNavSectorContainingPoint(manifest, end)
	if !ok {
		return NavSectorPathResult{}, nil
	}
	return FindNavSectorPath(manifest, startCoord, endCoord, opts)
}

func FindNavSectorPath(manifest *NavManifestDef, start TerrainChunkCoordDef, end TerrainChunkCoordDef, opts NavSectorPathOptions) (NavSectorPathResult, error) {
	graph, err := BuildNavSectorGraph(manifest, NavSectorGraphOptions{})
	if err != nil {
		return NavSectorPathResult{}, err
	}
	return FindNavSectorPathInGraph(graph, start, end, opts), nil
}

func FindNavSectorPathInGraph(graph NavSectorGraph, start TerrainChunkCoordDef, end TerrainChunkCoordDef, opts NavSectorPathOptions) NavSectorPathResult {
	if _, ok := graph.Sectors[start]; !ok {
		return NavSectorPathResult{}
	}
	if _, ok := graph.Sectors[end]; !ok {
		return NavSectorPathResult{}
	}
	if start == end {
		return NavSectorPathResult{Found: true, SectorCoords: []TerrainChunkCoordDef{start}}
	}
	maxSearch := opts.MaxSectorSearch
	if maxSearch <= 0 || maxSearch > len(graph.Sectors) {
		maxSearch = len(graph.Sectors)
	}
	allowedKinds := navSectorAllowedKindSet(opts.AllowedKinds)
	agentTags := navSectorAgentTagSet(opts.AgentTags)
	dist := map[TerrainChunkCoordDef]float32{start: 0}
	prev := map[TerrainChunkCoordDef]TerrainChunkCoordDef{}
	prevEdge := map[TerrainChunkCoordDef]NavSectorGraphEdge{}
	visited := map[TerrainChunkCoordDef]struct{}{}
	for len(visited) < maxSearch {
		current, ok := navSectorClosestUnvisited(dist, visited)
		if !ok {
			break
		}
		if current == end {
			return navSectorBuildPathResult(start, end, dist[end], prev, prevEdge, opts)
		}
		visited[current] = struct{}{}
		for _, edge := range graph.Edges[current] {
			if !navSectorEdgeAllowed(edge, allowedKinds, agentTags) {
				continue
			}
			if _, done := visited[edge.To]; done {
				continue
			}
			nextCost := dist[current] + edge.Cost
			if existing, ok := dist[edge.To]; !ok || nextCost < existing {
				dist[edge.To] = nextCost
				prev[edge.To] = current
				prevEdge[edge.To] = edge
			}
		}
	}
	return NavSectorPathResult{}
}

func (graph NavSectorGraph) addEdge(edge NavSectorGraphEdge) {
	if edge.Kind == "" {
		edge.Kind = NavTraversalWalk
	}
	if edge.Cost <= 0 {
		edge.Cost = 1
	}
	graph.Edges[edge.From] = append(graph.Edges[edge.From], edge)
}

func (graph NavSectorGraph) addBoundsAdjacencyEdges() {
	coords := make([]TerrainChunkCoordDef, 0, len(graph.Sectors))
	for coord := range graph.Sectors {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		return terrainChunkCoordLess(coords[i], coords[j])
	})
	for i := 0; i < len(coords); i++ {
		for j := i + 1; j < len(coords); j++ {
			a := graph.Sectors[coords[i]]
			b := graph.Sectors[coords[j]]
			if !navSectorBoundsTouch(a.BoundsMin, a.BoundsMax, b.BoundsMin, b.BoundsMax) {
				continue
			}
			graph.addEdge(navSectorGraphEdgeFromRefs(graph, a.Coord, b.Coord, NavSectorEdgeSourceBoundsAdjacency))
			graph.addEdge(navSectorGraphEdgeFromRefs(graph, b.Coord, a.Coord, NavSectorEdgeSourceBoundsAdjacency))
		}
	}
}

func (graph NavSectorGraph) sortEdges() {
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
			return terrainChunkCoordLess(a.To, b.To)
		})
	}
}

func navSectorGraphEdgeFromRefs(graph NavSectorGraph, from TerrainChunkCoordDef, to TerrainChunkCoordDef, source string) NavSectorGraphEdge {
	return NavSectorGraphEdge{
		From:   from,
		To:     to,
		Kind:   NavTraversalWalk,
		Cost:   navSectorCenterDistance(graph.Sectors[from], graph.Sectors[to]),
		Source: source,
	}
}

func navSectorGraphEdgeFromLink(graph NavSectorGraph, from TerrainChunkCoordDef, link NavSectorLinkDef) NavSectorGraphEdge {
	kind := link.Kind
	if kind == "" {
		kind = NavTraversalWalk
	}
	cost := link.Cost
	if cost <= 0 {
		cost = navSectorCenterDistance(graph.Sectors[from], graph.Sectors[link.To])
	}
	return NavSectorGraphEdge{
		From:        from,
		To:          link.To,
		Kind:        kind,
		Cost:        cost,
		Source:      NavSectorEdgeSourceLink,
		LinkID:      link.ID,
		Openable:    link.Openable,
		RequiresTag: link.RequiresTag,
		Tags:        append([]string(nil), link.Tags...),
	}
}

func navSectorBuildPathResult(start TerrainChunkCoordDef, end TerrainChunkCoordDef, cost float32, prev map[TerrainChunkCoordDef]TerrainChunkCoordDef, prevEdge map[TerrainChunkCoordDef]NavSectorGraphEdge, opts NavSectorPathOptions) NavSectorPathResult {
	coords := []TerrainChunkCoordDef{end}
	edges := []NavSectorGraphEdge{}
	for coords[len(coords)-1] != start {
		current := coords[len(coords)-1]
		from, ok := prev[current]
		if !ok {
			return NavSectorPathResult{}
		}
		coords = append(coords, from)
		edges = append(edges, prevEdge[current])
	}
	reverseSectorCoords(coords)
	reverseSectorEdges(edges)
	result := NavSectorPathResult{
		Found:        true,
		SectorCoords: coords,
		Edges:        edges,
		Cost:         cost,
	}
	if opts.AgentSpeed > 0 {
		result.EstimatedTravelSeconds = cost / opts.AgentSpeed
	}
	return result
}

func navSectorClosestUnvisited(dist map[TerrainChunkCoordDef]float32, visited map[TerrainChunkCoordDef]struct{}) (TerrainChunkCoordDef, bool) {
	var best TerrainChunkCoordDef
	bestCost := float32(math.MaxFloat32)
	found := false
	for coord, cost := range dist {
		if _, ok := visited[coord]; ok {
			continue
		}
		if !found || cost < bestCost || (cost == bestCost && terrainChunkCoordLess(coord, best)) {
			best = coord
			bestCost = cost
			found = true
		}
	}
	return best, found
}

func navSectorAllowedKindSet(kinds []string) map[string]struct{} {
	if len(kinds) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		kind = strings.TrimSpace(kind)
		if kind != "" {
			out[kind] = struct{}{}
		}
	}
	return out
}

func navSectorAgentTagSet(tags []string) map[string]struct{} {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			out[tag] = struct{}{}
		}
	}
	return out
}

func navSectorEdgeAllowed(edge NavSectorGraphEdge, allowedKinds map[string]struct{}, agentTags map[string]struct{}) bool {
	kind := edge.Kind
	if kind == "" {
		kind = NavTraversalWalk
	}
	if len(allowedKinds) > 0 {
		if _, ok := allowedKinds[kind]; !ok {
			return false
		}
	}
	if edge.RequiresTag != "" {
		_, ok := agentTags[edge.RequiresTag]
		return ok
	}
	return true
}

func navSectorCenterDistance(a NavSectorEntryDef, b NavSectorEntryDef) float32 {
	ac := navSectorBoundsCenter(a.BoundsMin, a.BoundsMax)
	bc := navSectorBoundsCenter(b.BoundsMin, b.BoundsMax)
	dx := float64(ac[0] - bc[0])
	dy := float64(ac[1] - bc[1])
	dz := float64(ac[2] - bc[2])
	return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
}

func navSectorBoundsCenter(min [3]float32, max [3]float32) [3]float32 {
	return [3]float32{
		(min[0] + max[0]) * 0.5,
		(min[1] + max[1]) * 0.5,
		(min[2] + max[2]) * 0.5,
	}
}

func navPointInBounds(point Vec3, min [3]float32, max [3]float32) bool {
	const epsilon = float32(1e-4)
	return point[0] >= min[0]-epsilon && point[0] <= max[0]+epsilon &&
		point[1] >= min[1]-epsilon && point[1] <= max[1]+epsilon &&
		point[2] >= min[2]-epsilon && point[2] <= max[2]+epsilon
}

func navSectorBoundsTouch(aMin [3]float32, aMax [3]float32, bMin [3]float32, bMax [3]float32) bool {
	touchAxes := 0
	for axis := 0; axis < 3; axis++ {
		if navFloatNearlyEqual(aMax[axis], bMin[axis]) || navFloatNearlyEqual(bMax[axis], aMin[axis]) {
			touchAxes++
			continue
		}
		if !navRangesOverlap(aMin[axis], aMax[axis], bMin[axis], bMax[axis]) {
			return false
		}
	}
	return touchAxes == 1
}

func navRangesOverlap(aMin float32, aMax float32, bMin float32, bMax float32) bool {
	const epsilon = float32(1e-4)
	return aMax > bMin+epsilon && bMax > aMin+epsilon
}

func navFloatNearlyEqual(a float32, b float32) bool {
	const epsilon = float32(1e-4)
	if a > b {
		return a-b <= epsilon
	}
	return b-a <= epsilon
}

func reverseSectorCoords(values []TerrainChunkCoordDef) {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
}

func reverseSectorEdges(values []NavSectorGraphEdge) {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
}
