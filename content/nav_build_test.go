package content

import "testing"

func TestBuildNavTileFromImportedWorldChunkFlatFloor(t *testing.T) {
	chunk := navTestChunk(6, 0.5, navTestFloorVoxels(1, 4, 1, 4, 0)...)
	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{NavID: "nav-flat"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if result.Tile.NavID != "nav-flat" || result.Tile.Coord != chunk.Coord {
		t.Fatalf("unexpected tile metadata: %+v", result.Tile)
	}
	if len(result.WalkableCells) != 16 || len(result.Tile.Polygons) != 16 {
		t.Fatalf("expected 16 interior walkable cells, got cells=%d polygons=%d", len(result.WalkableCells), len(result.Tile.Polygons))
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileWallBlocksWalkableCellsAndConnectivity(t *testing.T) {
	voxels := navTestFloorVoxels(1, 5, 1, 5, 0)
	for z := 1; z <= 5; z++ {
		voxels = append(voxels,
			ImportedWorldVoxelDef{X: 3, Y: 1, Z: z, Value: 1},
			ImportedWorldVoxelDef{X: 3, Y: 2, Z: z, Value: 1},
		)
	}
	chunk := navTestChunk(8, 0.5, voxels...)
	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if navTileHasPolygon(result.Tile, navCellPolygonID(3, 1, 3)) {
		t.Fatalf("expected wall column to be blocked, got polygons %+v", result.Tile.Polygons)
	}
	if navTilePolygonsAreNeighbors(result.Tile, navCellPolygonID(2, 1, 3), navCellPolygonID(4, 1, 3)) {
		t.Fatal("did not expect navigation connectivity through wall column")
	}
}

func TestBuildNavTileConnectsStairsWithinStepHeight(t *testing.T) {
	voxels := []ImportedWorldVoxelDef{
		{X: 2, Y: 0, Z: 1, Value: 1},
		{X: 2, Y: 0, Z: 2, Value: 1},
		{X: 2, Y: 1, Z: 2, Value: 1},
		{X: 2, Y: 0, Z: 3, Value: 1},
		{X: 2, Y: 1, Z: 3, Value: 1},
		{X: 2, Y: 2, Z: 3, Value: 1},
	}
	chunk := navTestChunk(8, 0.5, voxels...)
	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTilePolygonsAreNeighbors(result.Tile, navCellPolygonID(2, 1, 1), navCellPolygonID(2, 2, 2)) {
		t.Fatalf("expected first stair step to connect, got %+v", result.Tile.Polygons)
	}
	if !navTilePolygonsAreNeighbors(result.Tile, navCellPolygonID(2, 2, 2), navCellPolygonID(2, 3, 3)) {
		t.Fatalf("expected second stair step to connect, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileRejectsLedgeAsNormalWalkingEdge(t *testing.T) {
	voxels := []ImportedWorldVoxelDef{
		{X: 2, Y: 0, Z: 1, Value: 1},
		{X: 2, Y: 0, Z: 2, Value: 1},
		{X: 2, Y: 1, Z: 2, Value: 1},
		{X: 2, Y: 2, Z: 2, Value: 1},
	}
	chunk := navTestChunk(8, 0.5, voxels...)
	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if navTilePolygonsAreNeighbors(result.Tile, navCellPolygonID(2, 1, 1), navCellPolygonID(2, 3, 2)) {
		t.Fatalf("did not expect ledge to connect as walking edge, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileOneVoxelLipFollowsStepHeight(t *testing.T) {
	voxels := []ImportedWorldVoxelDef{
		{X: 2, Y: 0, Z: 1, Value: 1},
		{X: 2, Y: 0, Z: 2, Value: 1},
		{X: 2, Y: 1, Z: 2, Value: 1},
	}
	chunk := navTestChunk(8, 0.5, voxels...)
	lowStep, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.49), NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk low step failed: %v", err)
	}
	if navTilePolygonsAreNeighbors(lowStep.Tile, navCellPolygonID(2, 1, 1), navCellPolygonID(2, 2, 2)) {
		t.Fatal("did not expect one-voxel lip to connect below step height")
	}
	exactStep, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk exact step failed: %v", err)
	}
	if !navTilePolygonsAreNeighbors(exactStep.Tile, navCellPolygonID(2, 1, 1), navCellPolygonID(2, 2, 2)) {
		t.Fatal("expected one-voxel lip to connect at step height")
	}
}

func TestBuildNavTileRadiusRejectsNarrowGap(t *testing.T) {
	voxels := navTestFloorVoxels(1, 5, 1, 3, 0)
	for x := 1; x <= 5; x++ {
		voxels = append(voxels,
			ImportedWorldVoxelDef{X: x, Y: 1, Z: 1, Value: 1},
			ImportedWorldVoxelDef{X: x, Y: 1, Z: 3, Value: 1},
		)
	}
	chunk := navTestChunk(7, 1.0, voxels...)
	small, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 1.0), NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk small radius failed: %v", err)
	}
	if !navTileHasPolygon(small.Tile, navCellPolygonID(3, 1, 2)) {
		t.Fatalf("expected small agent to fit through one-cell gap, got %+v", small.Tile.Polygons)
	}
	large, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.6, 1.0, 1.0), NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk large radius failed: %v", err)
	}
	if navTileHasPolygon(large.Tile, navCellPolygonID(3, 1, 2)) {
		t.Fatalf("did not expect large agent to fit through one-cell gap, got %+v", large.Tile.Polygons)
	}
}

func TestBuildNavTileUsesNeighborClearanceAtChunkBoundary(t *testing.T) {
	center := navTestChunk(4, 1.0, ImportedWorldVoxelDef{X: 3, Y: 0, Z: 1, Value: 1})
	right := navTestChunk(4, 1.0)
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}

	withoutNeighbor, err := BuildNavTileFromImportedWorldChunk(center, navTestAgentProfile(0.6, 1.0, 1.0), NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk without neighbor failed: %v", err)
	}
	if navTileHasPolygon(withoutNeighbor.Tile, navCellPolygonID(3, 1, 1)) {
		t.Fatalf("did not expect boundary polygon without neighbor context, got %+v", withoutNeighbor.Tile.Polygons)
	}

	withNeighbor, err := BuildNavTileFromImportedWorldChunk(center, navTestAgentProfile(0.6, 1.0, 1.0), NavTileBuildOptions{
		NeighborChunks: map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
			right.Coord: right,
		},
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk with neighbor failed: %v", err)
	}
	if !navTileHasPolygon(withNeighbor.Tile, navCellPolygonID(3, 1, 1)) {
		t.Fatalf("expected boundary polygon with clear neighbor context, got %+v", withNeighbor.Tile.Polygons)
	}
}

func TestBuildNavTileNeighborOccupancyBlocksBoundaryClearance(t *testing.T) {
	center := navTestChunk(4, 1.0, ImportedWorldVoxelDef{X: 3, Y: 0, Z: 1, Value: 1})
	right := navTestChunk(4, 1.0, ImportedWorldVoxelDef{X: 0, Y: 1, Z: 1, Value: 1})
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}

	result, err := BuildNavTileFromImportedWorldChunk(center, navTestAgentProfile(0.6, 1.0, 1.0), NavTileBuildOptions{
		NeighborChunks: map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
			right.Coord: right,
		},
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if navTileHasPolygon(result.Tile, navCellPolygonID(3, 1, 1)) {
		t.Fatalf("did not expect boundary polygon when neighbor blocks clearance, got %+v", result.Tile.Polygons)
	}
}

func navTestChunk(size int, voxelResolution float32, voxels ...ImportedWorldVoxelDef) *ImportedWorldChunkDef {
	return &ImportedWorldChunkDef{
		WorldID:         "world-test",
		Coord:           TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:       size,
		VoxelResolution: voxelResolution,
		Voxels:          voxels,
	}
}

func navTestAgentProfile(radius, height, stepHeight float32) NavAgentProfileDef {
	return NavAgentProfileDef{
		ID:              "test-agent",
		Radius:          radius,
		Height:          height,
		StepHeight:      stepHeight,
		MaxSlopeDegrees: 45,
	}
}

func navTestFloorVoxels(minX, maxX, minZ, maxZ, y int) []ImportedWorldVoxelDef {
	out := make([]ImportedWorldVoxelDef, 0, (maxX-minX+1)*(maxZ-minZ+1))
	for x := minX; x <= maxX; x++ {
		for z := minZ; z <= maxZ; z++ {
			out = append(out, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
		}
	}
	return out
}

func navTileHasPolygon(tile *NavTileDef, id string) bool {
	if tile == nil {
		return false
	}
	for _, polygon := range tile.Polygons {
		if polygon.ID == id {
			return true
		}
	}
	return false
}

func navTilePolygonsAreNeighbors(tile *NavTileDef, a, b string) bool {
	if tile == nil {
		return false
	}
	for _, polygon := range tile.Polygons {
		if polygon.ID != a {
			continue
		}
		for _, neighbor := range polygon.Neighbors {
			if neighbor == b {
				return true
			}
		}
	}
	return false
}
