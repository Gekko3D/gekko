package content

import "testing"

func TestBuildNavClearanceLocalRegionsMergesFlatField(t *testing.T) {
	chunk := navTestChunk(16, 1.0, navTestFloorVoxels(1, 14, 1, 14, 0)...)
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 0.5)
	source, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
		NavID:              "nav-regions-flat",
		MaxClearanceRadius: 1.0,
	})
	if err != nil {
		t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
	}

	result, err := BuildNavClearanceLocalRegions(source.Tile, NavClearanceRegionBuildOptions{AgentProfile: profile})
	if err != nil {
		t.Fatalf("BuildNavClearanceLocalRegions failed: %v", err)
	}

	if len(result.Regions) != 1 {
		t.Fatalf("expected flat field to merge into one local region, got %+v", result.Regions)
	}
	if len(result.Regions[0].Cells) != result.SupportedCells || result.SupportedCells == 0 {
		t.Fatalf("expected all supported cells in one region, result=%+v", result)
	}
	height, ok := NavClearanceLocalRegionHeightAt(result.Regions[0], 2.5, 2.5)
	if !ok || height < 0.99 || height > 1.01 {
		t.Fatalf("expected flat region projection height around 1.0, height=%.3f ok=%t region=%+v", height, ok, result.Regions[0])
	}
}

func TestBuildNavClearanceLocalRegionsKeepsUnevenSurfaceConnected(t *testing.T) {
	voxels := make([]ImportedWorldVoxelDef, 0)
	for x := 2; x <= 13; x++ {
		for z := 2; z <= 13; z++ {
			height := (x-2)/4 + (z-2)/6
			for y := 0; y <= height; y++ {
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}
	chunk := navTestChunk(20, 0.1, voxels...)
	profile := navTestAgentProfileWithID("tiny", 0.05, 0.5, 0.15)
	profile.NavCellSize = 0.1
	profile.MaxSlopeDegrees = 45
	source, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
		NavID:              "nav-regions-uneven",
		MaxClearanceRadius: 0.5,
	})
	if err != nil {
		t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
	}

	result, err := BuildNavClearanceLocalRegions(source.Tile, NavClearanceRegionBuildOptions{AgentProfile: profile})
	if err != nil {
		t.Fatalf("BuildNavClearanceLocalRegions failed: %v", err)
	}

	if len(result.Regions) != 1 {
		t.Fatalf("expected uneven walkable surface to remain one local region, got %+v", result.Regions)
	}
	if len(result.Regions[0].Cells) < 16 {
		t.Fatalf("expected sizeable uneven region, got %+v", result.Regions[0])
	}
	lowHeight, lowOK := NavClearanceLocalRegionHeightAt(result.Regions[0], 0.25, 0.25)
	highHeight, highOK := NavClearanceLocalRegionHeightAt(result.Regions[0], 1.25, 1.25)
	if !lowOK || !highOK || highHeight <= lowHeight {
		t.Fatalf("expected uneven region projection to rise across the surface, low=%.3f/%t high=%.3f/%t region=%+v", lowHeight, lowOK, highHeight, highOK, result.Regions[0])
	}
	if !result.Regions[0].Projection.HasHeightPlane || result.Regions[0].Projection.CellCount != len(result.Regions[0].Cells) {
		t.Fatalf("expected projection metadata to describe region cells, region=%+v", result.Regions[0])
	}
}

func TestBuildNavClearanceLocalRegionsSeparatesTooHighStep(t *testing.T) {
	chunk := navTestChunk(8, 1.0,
		ImportedWorldVoxelDef{X: 1, Y: 0, Z: 1, Value: 1},
		ImportedWorldVoxelDef{X: 2, Y: 0, Z: 1, Value: 1},
		ImportedWorldVoxelDef{X: 2, Y: 1, Z: 1, Value: 1},
		ImportedWorldVoxelDef{X: 2, Y: 2, Z: 1, Value: 1},
	)
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 1.0)
	source, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
		NavID:              "nav-regions-step",
		MaxClearanceRadius: 1.0,
	})
	if err != nil {
		t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
	}

	result, err := BuildNavClearanceLocalRegions(source.Tile, NavClearanceRegionBuildOptions{AgentProfile: profile})
	if err != nil {
		t.Fatalf("BuildNavClearanceLocalRegions failed: %v", err)
	}

	if len(result.Regions) != 2 {
		t.Fatalf("expected high step to split local regions, got %+v", result.Regions)
	}
}

func TestBuildNavClearanceLocalRegionsAddsPortalsBetweenAreas(t *testing.T) {
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 0.5)
	tile := &NavClearanceSourceTileDef{
		NavID:           "nav-regions-portals",
		SchemaVersion:   CurrentNavClearanceSourceTileSchemaVersion,
		Coord:           TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		VoxelResolution: 1.0,
		Cells: []NavClearanceSourceCellDef{
			{X: 1, Y: 1, Z: 1, Position: Vec3{1.5, 1, 1.5}, Headroom: 2.0, ClearanceRadius: 0.5, Area: NavTraversalWalk},
			{X: 2, Y: 1, Z: 1, Position: Vec3{2.5, 1, 1.5}, Headroom: 2.0, ClearanceRadius: 0.4, Area: NavTraversalRamp},
		},
	}

	result, err := BuildNavClearanceLocalRegions(tile, NavClearanceRegionBuildOptions{AgentProfile: profile})
	if err != nil {
		t.Fatalf("BuildNavClearanceLocalRegions failed: %v", err)
	}

	if len(result.Regions) != 2 {
		t.Fatalf("expected area boundary to split regions, got %+v", result.Regions)
	}
	if len(result.Regions[0].Portals) != 1 || len(result.Regions[1].Portals) != 1 {
		t.Fatalf("expected reciprocal portals between area regions, got %+v", result.Regions)
	}
	portal := result.Regions[0].Portals[0]
	if portal.Kind != NavClearanceRegionPortalWalk || portal.FromRegionID != result.Regions[0].ID || portal.ToRegionID != result.Regions[1].ID {
		t.Fatalf("unexpected portal identity: %+v regions=%+v", portal, result.Regions)
	}
	if portal.Width < 0.79 || portal.Width > 0.81 || portal.RequiredWidth < 0.39 || portal.RequiredWidth > 0.41 {
		t.Fatalf("unexpected portal clearance metadata: %+v", portal)
	}
	back := result.Regions[1].Portals[0]
	if back.ToRegionID != result.Regions[0].ID || back.FromCell != portal.ToCell || back.ToCell != portal.FromCell {
		t.Fatalf("expected reciprocal portal, forward=%+v back=%+v", portal, back)
	}
}

func TestBuildNavClearanceCrossTileRegionPortalsConnectsBoundaryRegions(t *testing.T) {
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 1.0)
	left := navTestChunk(4, 1.0, navTestFloorVoxels(0, 3, 1, 1, 0)...)
	left.Coord = TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	right := navTestChunk(4, 1.0, navTestFloorVoxels(0, 3, 1, 1, 0)...)
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	tiles := make(map[TerrainChunkCoordDef]*NavClearanceSourceTileDef)
	results := make(map[TerrainChunkCoordDef]*NavClearanceLocalRegionBuildResult)
	for _, chunk := range []*ImportedWorldChunkDef{left, right} {
		source, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
			NavID:              "nav-regions-cross",
			MaxClearanceRadius: 1.0,
		})
		if err != nil {
			t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
		}
		result, err := BuildNavClearanceLocalRegions(source.Tile, NavClearanceRegionBuildOptions{AgentProfile: profile})
		if err != nil {
			t.Fatalf("BuildNavClearanceLocalRegions failed: %v", err)
		}
		tiles[chunk.Coord] = source.Tile
		results[chunk.Coord] = &result
	}

	if err := BuildNavClearanceCrossTileRegionPortals(results, tiles, NavClearanceRegionPortalBuildOptions{
		AgentProfile: profile,
		ChunkSize:    4,
	}); err != nil {
		t.Fatalf("BuildNavClearanceCrossTileRegionPortals failed: %v", err)
	}

	leftRegion := results[left.Coord].Regions[0]
	rightRegion := results[right.Coord].Regions[0]
	if len(leftRegion.Portals) != 1 || len(rightRegion.Portals) != 1 {
		t.Fatalf("expected one reciprocal cross-tile portal per region, left=%+v right=%+v", leftRegion.Portals, rightRegion.Portals)
	}
	leftPortal := leftRegion.Portals[0]
	if leftPortal.FromRegionID != leftRegion.ID || leftPortal.ToRegionID != rightRegion.ID {
		t.Fatalf("unexpected left portal region refs: %+v left=%+v right=%+v", leftPortal, leftRegion, rightRegion)
	}
	if leftPortal.FromCell.Coord != left.Coord || leftPortal.FromCell.X != 3 || leftPortal.ToCell.Coord != right.Coord || leftPortal.ToCell.X != 0 {
		t.Fatalf("expected left portal to cross max-x boundary, got %+v", leftPortal)
	}
	rightPortal := rightRegion.Portals[0]
	if rightPortal.FromRegionID != rightRegion.ID || rightPortal.ToRegionID != leftRegion.ID {
		t.Fatalf("unexpected right portal region refs: %+v left=%+v right=%+v", rightPortal, leftRegion, rightRegion)
	}
	if rightPortal.FromCell.Coord != right.Coord || rightPortal.FromCell.X != 0 || rightPortal.ToCell.Coord != left.Coord || rightPortal.ToCell.X != 3 {
		t.Fatalf("expected right portal to cross min-x boundary, got %+v", rightPortal)
	}
}

func TestConvertNavClearancePathToRegionPathCompressesSingleRegionCells(t *testing.T) {
	chunk := navTestChunk(8, 1.0, navTestFloorVoxels(1, 6, 1, 1, 0)...)
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 1.0)
	source, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
		NavID:              "nav-region-path-single",
		MaxClearanceRadius: 1.0,
	})
	if err != nil {
		t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
	}
	regions, err := BuildNavClearanceLocalRegions(source.Tile, NavClearanceRegionBuildOptions{AgentProfile: profile})
	if err != nil {
		t.Fatalf("BuildNavClearanceLocalRegions failed: %v", err)
	}
	raw := NavClearancePathResult{
		Found: true,
		Steps: []NavClearancePathStep{
			{Coord: chunk.Coord, X: 1, Y: 1, Z: 1},
			{Coord: chunk.Coord, X: 2, Y: 1, Z: 1},
			{Coord: chunk.Coord, X: 3, Y: 1, Z: 1},
			{Coord: chunk.Coord, X: 4, Y: 1, Z: 1},
			{Coord: chunk.Coord, X: 5, Y: 1, Z: 1},
			{Coord: chunk.Coord, X: 6, Y: 1, Z: 1},
		},
		Waypoints: []Vec3{{1.5, 1, 1.5}, {6.5, 1, 1.5}},
	}

	regionPath, err := ConvertNavClearancePathToRegionPath(raw, map[TerrainChunkCoordDef]*NavClearanceLocalRegionBuildResult{chunk.Coord: &regions})
	if err != nil {
		t.Fatalf("ConvertNavClearancePathToRegionPath failed: %v", err)
	}

	if !regionPath.Found || len(regionPath.Steps) != 1 || regionPath.Steps[0].RegionID != regions.Regions[0].ID {
		t.Fatalf("expected raw cells to compress to one region step, got %+v regions=%+v", regionPath, regions.Regions)
	}
	if len(regionPath.Portals) != 0 || regionPath.RawStepCount != len(raw.Steps) {
		t.Fatalf("unexpected single-region path metadata: %+v", regionPath)
	}
}

func TestConvertNavClearancePathToRegionPathKeepsCrossTilePortal(t *testing.T) {
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 1.0)
	left := navTestChunk(4, 1.0, navTestFloorVoxels(0, 3, 1, 1, 0)...)
	left.Coord = TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	right := navTestChunk(4, 1.0, navTestFloorVoxels(0, 3, 1, 1, 0)...)
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	tiles := make(map[TerrainChunkCoordDef]*NavClearanceSourceTileDef)
	results := make(map[TerrainChunkCoordDef]*NavClearanceLocalRegionBuildResult)
	for _, chunk := range []*ImportedWorldChunkDef{left, right} {
		source, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
			NavID:              "nav-region-path-cross",
			MaxClearanceRadius: 1.0,
		})
		if err != nil {
			t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
		}
		result, err := BuildNavClearanceLocalRegions(source.Tile, NavClearanceRegionBuildOptions{AgentProfile: profile})
		if err != nil {
			t.Fatalf("BuildNavClearanceLocalRegions failed: %v", err)
		}
		tiles[chunk.Coord] = source.Tile
		results[chunk.Coord] = &result
	}
	if err := BuildNavClearanceCrossTileRegionPortals(results, tiles, NavClearanceRegionPortalBuildOptions{
		AgentProfile: profile,
		ChunkSize:    4,
	}); err != nil {
		t.Fatalf("BuildNavClearanceCrossTileRegionPortals failed: %v", err)
	}
	raw := NavClearancePathResult{
		Found: true,
		Steps: []NavClearancePathStep{
			{Coord: left.Coord, X: 2, Y: 1, Z: 1},
			{Coord: left.Coord, X: 3, Y: 1, Z: 1},
			{Coord: right.Coord, X: 0, Y: 1, Z: 1},
			{Coord: right.Coord, X: 1, Y: 1, Z: 1},
		},
	}

	regionPath, err := ConvertNavClearancePathToRegionPath(raw, results)
	if err != nil {
		t.Fatalf("ConvertNavClearancePathToRegionPath failed: %v", err)
	}

	if len(regionPath.Steps) != 2 || regionPath.Steps[0].RegionID != results[left.Coord].Regions[0].ID || regionPath.Steps[1].RegionID != results[right.Coord].Regions[0].ID {
		t.Fatalf("expected cross-tile raw path to compress to two region steps, got %+v", regionPath)
	}
	if len(regionPath.Portals) != 1 {
		t.Fatalf("expected one cross-tile portal, got %+v", regionPath)
	}
	portal := regionPath.Portals[0]
	if portal.FromCell.Coord != left.Coord || portal.ToCell.Coord != right.Coord {
		t.Fatalf("expected region path portal to cross from left to right, got %+v", portal)
	}
}
