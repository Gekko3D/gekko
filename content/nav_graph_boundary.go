package content

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
)

// ConnectNavGraphTiles adds deterministic cross-tile transitions between the
// known graph tiles. Missing graph tiles stay disconnected. Sources may include
// halo-only tiles; their source hashes participate in dependency hashes.
func ConnectNavGraphTiles(sources []NavSourceTileDef, graphs []NavGraphTileDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32) ([]NavSourceTileDef, []NavGraphTileDef, error) {
	return connectNavGraphTiles(sources, graphs, profile, chunkSize, voxelResolution, false)
}

// ConnectNavGraphTilesWithContext recomputes profile clearance from source
// occupancy before validating cross-tile transitions.
func ConnectNavGraphTilesWithContext(sources []NavSourceTileDef, graphs []NavGraphTileDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32) ([]NavSourceTileDef, []NavGraphTileDef, error) {
	return connectNavGraphTiles(sources, graphs, profile, chunkSize, voxelResolution, true)
}

func connectNavGraphTiles(sources []NavSourceTileDef, graphs []NavGraphTileDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32, profileClearance bool) ([]NavSourceTileDef, []NavGraphTileDef, error) {
	if chunkSize <= 0 {
		return nil, nil, fmt.Errorf("navigation graph chunk size must be positive")
	}
	if !finite(voxelResolution) || voxelResolution <= 0 {
		return nil, nil, fmt.Errorf("navigation graph voxel resolution must be finite and positive")
	}
	if profile.ID == "" || !finite(profile.Radius) || profile.Radius <= 0 || !finite(profile.Height) || profile.Height <= 0 || !finite(profile.StepHeight) || profile.StepHeight < 0 || !finite(profile.MaxSlopeDegrees) || profile.MaxSlopeDegrees < 0 || profile.MaxSlopeDegrees >= 90 || !navCapabilitiesValid(profile.Capabilities) {
		return nil, nil, fmt.Errorf("invalid navigation agent profile")
	}

	sources = append([]NavSourceTileDef(nil), sources...)
	graphs = append([]NavGraphTileDef(nil), graphs...)
	sort.Slice(sources, func(i, j int) bool { return terrainCoordLess(sources[i].Coord, sources[j].Coord) })
	sort.Slice(graphs, func(i, j int) bool { return terrainCoordLess(graphs[i].Coord, graphs[j].Coord) })

	sourceIndex := make(map[TerrainChunkCoordDef]int, len(sources))
	for i := range sources {
		if _, exists := sourceIndex[sources[i].Coord]; exists {
			return nil, nil, fmt.Errorf("duplicate navigation source tile %s", TerrainChunkKey(sources[i].Coord))
		}
		sourceIndex[sources[i].Coord] = i
	}
	for i := range sources {
		if validation := ValidateNavSourceTile(&sources[i]); validation.HasErrors() {
			return nil, nil, fmt.Errorf("invalid navigation source tile %s: %s", TerrainChunkKey(sources[i].Coord), validation.Error())
		}
		if sources[i].ChunkSize != chunkSize {
			return nil, nil, fmt.Errorf("navigation source tile %s has mismatched chunk_size", TerrainChunkKey(sources[i].Coord))
		}
		for _, span := range sources[i].Spans {
			if span.X < 0 || span.X >= chunkSize || span.Z < 0 || span.Z >= chunkSize {
				return nil, nil, fmt.Errorf("navigation source span %d is outside tile %s", span.ID, TerrainChunkKey(sources[i].Coord))
			}
		}
	}
	boundarySources := sources
	if profileClearance {
		boundarySources = append([]NavSourceTileDef(nil), sources...)
		for i := range sources {
			profiled, err := navSourceWithProfileClearance(sources[i], sources, profile, voxelResolution)
			if err != nil {
				return nil, nil, fmt.Errorf("calculate navigation clearance for tile %s: %w", TerrainChunkKey(sources[i].Coord), err)
			}
			boundarySources[i] = profiled
		}
	}

	graphIndex := make(map[TerrainChunkCoordDef]int, len(graphs))
	for i := range graphs {
		graph := &graphs[i]
		if _, exists := graphIndex[graph.Coord]; exists {
			return nil, nil, fmt.Errorf("duplicate navigation graph tile %s", TerrainChunkKey(graph.Coord))
		}
		sourcePos, exists := sourceIndex[graph.Coord]
		if !exists {
			return nil, nil, fmt.Errorf("navigation graph tile %s has no source tile", TerrainChunkKey(graph.Coord))
		}
		source := sources[sourcePos]
		expectedHash := navGraphDependencyHash(source.Coord, func(coord TerrainChunkCoordDef) (string, bool) {
			neighbor, known := sourceIndex[coord]
			if !known {
				return "", false
			}
			return sources[neighbor].SourceHash, true
		})
		if source.DependencyHash != expectedHash {
			return nil, nil, fmt.Errorf("navigation source tile %s dependency halo does not match known sources", TerrainChunkKey(source.Coord))
		}
		if graph.AgentProfileID != profile.ID || source.NavID != graph.NavID || source.BuilderVersion != graph.BuilderVersion || source.SourceHash != graph.SourceHash || source.DependencyHash != graph.DependencyHash {
			return nil, nil, fmt.Errorf("navigation source and graph tile metadata do not match at %s", TerrainChunkKey(graph.Coord))
		}
		graph.SpanTransitions = keepLocalNavSpanTransitions(graph.Coord, graph.SpanTransitions)
		graph.Transitions = keepLocalNavRegionTransitions(graph.Coord, graph.Transitions)
		if validation := ValidateNavGraphTile(graph); validation.HasErrors() {
			return nil, nil, fmt.Errorf("invalid navigation graph tile %s: %s", TerrainChunkKey(graph.Coord), validation.Error())
		}
		if len(navGraphSpanRegions(*graph)) != len(graph.SpanIDs) {
			return nil, nil, fmt.Errorf("navigation graph tile %s is not region-compressed", TerrainChunkKey(graph.Coord))
		}
		graphIndex[graph.Coord] = i
	}

	boundaryGroups := make([]map[navBoundaryRegionTransitionKey][]navRegionTransitionSegment, len(graphs))
	for i := range boundaryGroups {
		boundaryGroups[i] = make(map[navBoundaryRegionTransitionKey][]navRegionTransitionSegment)
	}
	for i := range graphs {
		for _, direction := range [...]TerrainChunkCoordDef{{X: 1}, {Z: 1}} {
			neighborCoord := graphs[i].Coord
			neighborCoord.X += direction.X
			neighborCoord.Z += direction.Z
			neighbor, known := graphIndex[neighborCoord]
			if !known {
				continue
			}
			if graphs[i].NavID != graphs[neighbor].NavID || graphs[i].BuilderVersion != graphs[neighbor].BuilderVersion {
				return nil, nil, fmt.Errorf("navigation graph tiles %s and %s have incompatible metadata", TerrainChunkKey(graphs[i].Coord), TerrainChunkKey(graphs[neighbor].Coord))
			}
			connectNavGraphBoundary(boundarySources[sourceIndex[graphs[i].Coord]], boundarySources[sourceIndex[neighborCoord]], &graphs[i], &graphs[neighbor], profile, chunkSize, voxelResolution, boundaryGroups[i], boundaryGroups[neighbor])
		}
	}

	for i := range graphs {
		graphs[i].Transitions = append(graphs[i].Transitions, buildNavBoundaryRegionTransitions(boundaryGroups[i], voxelResolution)...)
		for id := range graphs[i].Transitions {
			graphs[i].Transitions[id].ID = uint32(id)
		}
		if validation := ValidateNavGraphTile(&graphs[i]); validation.HasErrors() {
			return nil, nil, fmt.Errorf("invalid connected navigation graph tile %s: %s", TerrainChunkKey(graphs[i].Coord), validation.Error())
		}
	}
	return sources, graphs, nil
}

func navGraphDependencyHash(coord TerrainChunkCoordDef, sourceHash func(TerrainChunkCoordDef) (string, bool)) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("nav-graph-dependencies-v1\n"))
	for y := -1; y <= 1; y++ {
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				if x == 0 && y == 0 && z == 0 {
					continue
				}
				neighbor := TerrainChunkCoordDef{X: coord.X + x, Y: coord.Y + y, Z: coord.Z + z}
				hashValue := "unknown"
				if hash, known := sourceHash(neighbor); known {
					hashValue = fmt.Sprintf("known:%q", hash)
				}
				_, _ = fmt.Fprintf(hash, "%d:%d:%d=%s\n", neighbor.X, neighbor.Y, neighbor.Z, hashValue)
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func keepLocalNavSpanTransitions(coord TerrainChunkCoordDef, transitions []NavSpanTransitionDef) []NavSpanTransitionDef {
	result := make([]NavSpanTransitionDef, 0, len(transitions))
	for _, transition := range transitions {
		if transition.To.Tile == coord {
			result = append(result, transition)
		}
	}
	return result
}

func keepLocalNavRegionTransitions(coord TerrainChunkCoordDef, transitions []NavRegionTransitionDef) []NavRegionTransitionDef {
	result := make([]NavRegionTransitionDef, 0, len(transitions))
	for _, transition := range transitions {
		if transition.ToTile == coord {
			result = append(result, transition)
		}
	}
	return result
}

type navBoundaryRegionTransitionKey struct {
	from, to    uint32
	toTile      TerrainChunkCoordDef
	kind, flags string
	axis        uint8
	line        int
	height      float32
}

func connectNavGraphBoundary(aSource, bSource NavSourceTileDef, aGraph, bGraph *NavGraphTileDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32, aGroups, bGroups map[navBoundaryRegionTransitionKey][]navRegionTransitionSegment) {
	axis := uint8(0)
	if bSource.Coord.Z != aSource.Coord.Z {
		axis = 1
	}
	aBoundary, bBoundary := chunkSize-1, 0
	aSpans := navGraphBoundarySpans(aSource, *aGraph, axis, aBoundary)
	bSpans := navGraphBoundarySpans(bSource, *bGraph, axis, bBoundary)
	aRegions, bRegions := navGraphSpanRegions(*aGraph), navGraphSpanRegions(*bGraph)
	line := (bSource.Coord.X * chunkSize)
	rowBase := aSource.Coord.Z * chunkSize
	if axis == 1 {
		line = bSource.Coord.Z * chunkSize
		rowBase = aSource.Coord.X * chunkSize
	}

	for row := 0; row < chunkSize; row++ {
		for _, a := range aSpans[row] {
			for _, b := range bSpans[row] {
				aEdge, aCode := buildNavSpanTransition(a, b, bSource.Coord, profile, voxelResolution)
				bEdge, bCode := buildNavSpanTransition(b, a, aSource.Coord, profile, voxelResolution)
				if aCode == "" {
					aGraph.SpanTransitions = append(aGraph.SpanTransitions, aEdge)
					appendNavBoundaryRegionSegment(aGroups, aRegions[a.ID], bRegions[b.ID], bSource.Coord, aEdge, axis, line, rowBase+row, b.SupportHeight)
				}
				if bCode == "" {
					bGraph.SpanTransitions = append(bGraph.SpanTransitions, bEdge)
					appendNavBoundaryRegionSegment(bGroups, bRegions[b.ID], aRegions[a.ID], aSource.Coord, bEdge, axis, line, rowBase+row, a.SupportHeight)
				}
			}
		}
	}
}

func navGraphBoundarySpans(source NavSourceTileDef, graph NavGraphTileDef, axis uint8, boundary int) map[int][]NavSpanDef {
	accepted := make(map[uint32]struct{}, len(graph.SpanIDs))
	for _, id := range graph.SpanIDs {
		accepted[id] = struct{}{}
	}
	result := make(map[int][]NavSpanDef)
	for _, span := range source.Spans {
		coordinate, row := span.X, span.Z
		if axis == 1 {
			coordinate, row = span.Z, span.X
		}
		if coordinate == boundary {
			if _, ok := accepted[span.ID]; ok {
				result[row] = append(result[row], span)
			}
		}
	}
	return result
}

func navGraphSpanRegions(graph NavGraphTileDef) map[uint32]uint32 {
	result := make(map[uint32]uint32, len(graph.SpanIDs))
	for _, region := range graph.Regions {
		for _, run := range region.SpanRuns {
			for offset := uint32(0); offset < run.Count; offset++ {
				result[run.Start+offset] = region.ID
			}
		}
	}
	return result
}

func appendNavBoundaryRegionSegment(groups map[navBoundaryRegionTransitionKey][]navRegionTransitionSegment, from, to uint32, toTile TerrainChunkCoordDef, edge NavSpanTransitionDef, axis uint8, line, row int, height float32) {
	key := navBoundaryRegionTransitionKey{
		from: from, to: to, toTile: toTile, kind: edge.Kind, flags: navRegionFlagsKey(edge.RequiresFlags),
		axis: axis, line: line, height: height,
	}
	groups[key] = append(groups[key], navRegionTransitionSegment{
		start: row, end: row + 1, headroom: edge.MinHeadroom, clearance: edge.MinClearance, cost: edge.Cost,
	})
}

func buildNavBoundaryRegionTransitions(groups map[navBoundaryRegionTransitionKey][]navRegionTransitionSegment, voxelResolution float32) []NavRegionTransitionDef {
	keys := make([]navBoundaryRegionTransitionKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.from != b.from {
			return a.from < b.from
		}
		if a.toTile != b.toTile {
			return terrainCoordLess(a.toTile, b.toTile)
		}
		if a.to != b.to {
			return a.to < b.to
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		if a.flags != b.flags {
			return a.flags < b.flags
		}
		if a.axis != b.axis {
			return a.axis < b.axis
		}
		if a.line != b.line {
			return a.line < b.line
		}
		return math.Float32bits(a.height) < math.Float32bits(b.height)
	})

	var result []NavRegionTransitionDef
	for _, key := range keys {
		segments := groups[key]
		sort.Slice(segments, func(i, j int) bool { return segments[i].start < segments[j].start })
		for i := 0; i < len(segments); {
			run := segments[i]
			i++
			for i < len(segments) && segments[i].start <= run.end {
				run.end = max(run.end, segments[i].end)
				run.headroom = min(run.headroom, segments[i].headroom)
				run.clearance = min(run.clearance, segments[i].clearance)
				run.cost = min(run.cost, segments[i].cost)
				i++
			}
			transition := NavRegionTransitionDef{
				FromRegion: key.from, ToTile: key.toTile, ToRegion: key.to, Kind: key.kind,
				Width: float32(run.end-run.start) * voxelResolution, MinHeadroom: run.headroom,
				MinClearance: run.clearance, Cost: run.cost, RequiresFlags: navRegionFlags(key.flags),
			}
			if key.axis == 0 {
				transition.CrossingStart = Vec3{float32(key.line) * voxelResolution, key.height, float32(run.start) * voxelResolution}
				transition.CrossingEnd = Vec3{float32(key.line) * voxelResolution, key.height, float32(run.end) * voxelResolution}
			} else {
				transition.CrossingStart = Vec3{float32(run.start) * voxelResolution, key.height, float32(key.line) * voxelResolution}
				transition.CrossingEnd = Vec3{float32(run.end) * voxelResolution, key.height, float32(key.line) * voxelResolution}
			}
			result = append(result, transition)
		}
	}
	return result
}
