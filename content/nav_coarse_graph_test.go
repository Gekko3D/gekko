package content

import "testing"

func TestBuildNavClearanceCoarseGraphUsesRegionPortals(t *testing.T) {
	results := navTestBuildRegionStrip(t, 2)
	graph, err := BuildNavClearanceCoarseGraph(results)
	if err != nil {
		t.Fatalf("BuildNavClearanceCoarseGraph failed: %v", err)
	}

	left := results[TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}].Regions[0]
	right := results[TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}].Regions[0]
	if len(graph.Nodes) != 2 {
		t.Fatalf("expected one coarse node per local region, got %+v", graph.Nodes)
	}
	edges := graph.Edges[left.ID]
	if len(edges) != 1 {
		t.Fatalf("expected one portal edge from left region, got %+v", edges)
	}
	edge := edges[0]
	if edge.From != left.ID || edge.To != right.ID || edge.Source != NavClearanceCoarseEdgeSourcePortal {
		t.Fatalf("unexpected coarse edge identity: %+v", edge)
	}
	if edge.FromCoord != left.Coord || edge.ToCoord != right.Coord {
		t.Fatalf("expected edge coords to match crossed regions, edge=%+v left=%+v right=%+v", edge, left, right)
	}
	if edge.Cost <= 0 || edge.Width <= 0 || edge.Clearance <= 0 {
		t.Fatalf("expected coarse edge to carry cost and clearance metadata, got %+v", edge)
	}
}

func TestFindNavClearanceCoarseRegionPathRoutesAcrossRegionChain(t *testing.T) {
	results := navTestBuildRegionStrip(t, 4)
	graph, err := BuildNavClearanceCoarseGraph(results)
	if err != nil {
		t.Fatalf("BuildNavClearanceCoarseGraph failed: %v", err)
	}
	start := results[TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}].Regions[0]
	end := results[TerrainChunkCoordDef{X: 3, Y: 0, Z: 0}].Regions[0]

	path := FindNavClearanceCoarseRegionPath(graph, start.ID, end.ID, NavClearanceCoarseGraphOptions{})

	if !path.Found {
		t.Fatalf("expected coarse region path to be found")
	}
	if len(path.Steps) != 4 || len(path.Edges) != 3 {
		t.Fatalf("expected four region steps and three portal edges, got %+v", path)
	}
	for i, step := range path.Steps {
		expected := TerrainChunkCoordDef{X: i, Y: 0, Z: 0}
		if step.Coord != expected {
			t.Fatalf("expected step %d to visit %s, got %+v", i, TerrainChunkKey(expected), path.Steps)
		}
	}
	if path.Cost <= 0 {
		t.Fatalf("expected positive path cost, got %+v", path)
	}
}

func TestFindNavClearanceCoarseRegionPathDoesNotRouteDisconnectedRegions(t *testing.T) {
	results := navTestBuildRegionStrip(t, 2)
	left := results[TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}]
	right := results[TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}]
	left.Regions[0].Portals = nil
	right.Regions[0].Portals = nil
	graph, err := BuildNavClearanceCoarseGraph(results)
	if err != nil {
		t.Fatalf("BuildNavClearanceCoarseGraph failed: %v", err)
	}

	path := FindNavClearanceCoarseRegionPath(graph, left.Regions[0].ID, right.Regions[0].ID, NavClearanceCoarseGraphOptions{})

	if path.Found {
		t.Fatalf("expected disconnected region graph to return no path, got %+v", path)
	}
}

func navTestBuildRegionStrip(t *testing.T, count int) map[TerrainChunkCoordDef]*NavClearanceLocalRegionBuildResult {
	t.Helper()
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 1.0)
	tiles := make(map[TerrainChunkCoordDef]*NavClearanceSourceTileDef, count)
	results := make(map[TerrainChunkCoordDef]*NavClearanceLocalRegionBuildResult, count)
	for x := 0; x < count; x++ {
		coord := TerrainChunkCoordDef{X: x, Y: 0, Z: 0}
		chunk := navTestChunk(4, 1.0, navTestFloorVoxels(0, 3, 1, 1, 0)...)
		chunk.Coord = coord
		source, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
			NavID:              "nav-coarse-region-strip",
			MaxClearanceRadius: 1.0,
		})
		if err != nil {
			t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
		}
		result, err := BuildNavClearanceLocalRegions(source.Tile, NavClearanceRegionBuildOptions{AgentProfile: profile})
		if err != nil {
			t.Fatalf("BuildNavClearanceLocalRegions failed: %v", err)
		}
		tiles[coord] = source.Tile
		results[coord] = &result
	}
	if err := BuildNavClearanceCrossTileRegionPortals(results, tiles, NavClearanceRegionPortalBuildOptions{
		AgentProfile: profile,
		ChunkSize:    4,
	}); err != nil {
		t.Fatalf("BuildNavClearanceCrossTileRegionPortals failed: %v", err)
	}
	return results
}
