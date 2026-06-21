package content

import (
	"math"
	"sort"
	"strings"
	"testing"
)

func TestBuildNavTileFromImportedWorldChunkFlatFloor(t *testing.T) {
	chunk := navTestChunk(6, 0.5, navTestFloorVoxels(1, 4, 1, 4, 0)...)
	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{NavID: "nav-flat"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if result.Tile.NavID != "nav-flat" || result.Tile.Coord != chunk.Coord {
		t.Fatalf("unexpected tile metadata: %+v", result.Tile)
	}
	if len(result.WalkableCells) != 16 || len(result.Tile.Polygons) != 1 {
		t.Fatalf("expected 16 interior walkable cells merged to one polygon, got cells=%d polygons=%d", len(result.WalkableCells), len(result.Tile.Polygons))
	}
	if !navTileHasPolygonArea(result.Tile, NavTraversalWalk) {
		t.Fatalf("expected flat voxel floor to be classified as walk, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileStatsCountOnlyExposedCandidateSpans(t *testing.T) {
	voxels := navTestFloorVoxels(1, 4, 1, 4, 0)
	voxels = append(voxels, navTestFloorVoxels(1, 4, 1, 4, 1)...)
	chunk := navTestChunk(6, 0.5, voxels...)
	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{NavID: "nav-stats"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if result.Stats.OccupiedVoxels != 32 {
		t.Fatalf("expected occupied voxel count to include buried support voxels, got %+v", result.Stats)
	}
	if result.Stats.CandidateSpans != 16 {
		t.Fatalf("expected only exposed top support voxels to become candidate spans, got %+v", result.Stats)
	}
	if result.Stats.AcceptedSpans != 16 {
		t.Fatalf("expected exposed spans to pass clearance, got %+v", result.Stats)
	}
}

func TestNavDenseVoxelOccupancySetAndLookup(t *testing.T) {
	occupancy := newNavDenseVoxelOccupancy(4)
	if !occupancy.Set(1, 2, 3) {
		t.Fatal("expected first set to record occupied voxel")
	}
	if occupancy.Set(1, 2, 3) {
		t.Fatal("did not expect duplicate set to increment occupancy")
	}
	if occupancy.Count != 1 {
		t.Fatalf("expected one occupied voxel, got %d", occupancy.Count)
	}
	if !occupancy.Occupied(1, 2, 3) {
		t.Fatal("expected occupied voxel lookup to succeed")
	}
	if occupancy.Occupied(1, 3, 3) || occupancy.Occupied(-1, 2, 3) || occupancy.Occupied(4, 2, 3) {
		t.Fatal("unexpected occupied lookup outside the set voxel")
	}
}

func TestImportedWorldChunkNavDataUsesDenseOccupancyAndUniqueCandidates(t *testing.T) {
	chunk := navTestChunk(4, 1,
		ImportedWorldVoxelDef{X: 1, Y: 0, Z: 1, Value: 1},
		ImportedWorldVoxelDef{X: 1, Y: 0, Z: 1, Value: 1},
		ImportedWorldVoxelDef{X: 1, Y: 1, Z: 1, Value: 1},
		ImportedWorldVoxelDef{X: 1, Y: 3, Z: 1, Value: 1},
	)
	data := importedWorldChunkNavData(chunk)
	if data.Occupancy.Count != 3 {
		t.Fatalf("expected dense occupancy to deduplicate voxels, got %+v", data.Occupancy)
	}
	if !data.Occupancy.Occupied(1, 0, 1) || !data.Occupancy.Occupied(1, 1, 1) || !data.Occupancy.Occupied(1, 3, 1) {
		t.Fatalf("expected dense occupancy to preserve source voxels")
	}
	if len(data.CandidateSpans) != 2 {
		t.Fatalf("expected one candidate for each solid run top, got %+v", data.CandidateSpans)
	}
}

func TestNavCellsCanConnectUsesStepHeightIndependentlyOfSlopeLimit(t *testing.T) {
	profile := navTestAgentProfile(0.2, 1.0, 0.45)
	profile.NavCellSize = 0.3
	profile.MaxSlopeDegrees = 45
	metrics := navBuildCellMetrics{Horizontal: 0.3, Vertical: 0.1}

	if !navCellsCanConnect(
		NavBuildWalkableCell{X: 1, Y: 1, Z: 1},
		NavBuildWalkableCell{X: 2, Y: 5, Z: 1},
		metrics,
		profile,
	) {
		t.Fatal("expected adjacent cells within step height to connect even when slope limit would reject a ramp")
	}
}

func TestConnectNavTilePolygonsFromCellsUsesHeightfieldConnectivity(t *testing.T) {
	tile := &NavTileDef{
		Vertices: []Vec3{
			{0, 1, 0}, {0.4, 1, 0}, {0.4, 1, 1}, {0, 1, 1},
			{1.6, 1, 0}, {2, 1, 0}, {2, 1, 1}, {1.6, 1, 1},
		},
		Polygons: []NavPolygonDef{
			{ID: "left", Vertices: []int{0, 1, 2, 3}, Area: NavTraversalWalk},
			{ID: "right", Vertices: []int{4, 5, 6, 7}, Area: NavTraversalWalk},
		},
	}
	cells := []NavBuildWalkableCell{
		{X: 0, Y: 1, Z: 0, PolygonID: "left"},
		{X: 1, Y: 1, Z: 0, PolygonID: "right"},
	}
	profile := navTestAgentProfile(0.2, 1.0, 0.5)

	connectNavTilePolygonsFromCells(tile, [3]float32{}, navBuildCellMetrics{Horizontal: 1, Vertical: 1}, cells, 1, profile)

	if !navTilePolygonsAreNeighbors(tile, "left", "right") {
		t.Fatalf("expected heightfield-adjacent polygons to be connected, got %+v", tile.Polygons)
	}
}

func TestBuildNavTileGeneratesOneWayDropLinks(t *testing.T) {
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.MaxDropHeight = 3
	voxels := navTestFloorVoxels(1, 2, 1, 2, 2)
	voxels = append(voxels, navTestFloorVoxels(3, 4, 1, 2, 0)...)
	chunk := navTestChunk(8, 1.0, voxels...)

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{NavID: "nav-drop"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.OffMeshLinks) == 0 {
		t.Fatalf("expected generated drop links, got tile=%+v", result.Tile)
	}
	for _, link := range result.Tile.OffMeshLinks {
		if link.Kind != NavTraversalDrop || link.Bidirectional {
			t.Fatalf("expected one-way drop link, got %+v", link)
		}
		if link.Start[1] <= link.End[1] {
			t.Fatalf("expected drop link to descend, got %+v", link)
		}
	}
	high, ok := navTilePolygonAtCell(result.Tile, 2, 3, 1, chunk.VoxelResolution)
	if !ok {
		t.Fatalf("expected high ledge polygon, polygons=%+v", result.Tile.Polygons)
	}
	low, ok := navTilePolygonAtCell(result.Tile, 3, 1, 1, chunk.VoxelResolution)
	if !ok {
		t.Fatalf("expected low landing polygon, polygons=%+v", result.Tile.Polygons)
	}
	if navTilePolygonsAreNeighbors(result.Tile, high.ID, low.ID) {
		t.Fatalf("did not expect drop to be ordinary polygon adjacency, high=%s low=%s polygons=%+v", high.ID, low.ID, result.Tile.Polygons)
	}
	query, err := NewNavTileQuery(result.Tile)
	if err != nil {
		t.Fatalf("NewNavTileQuery failed: %v", err)
	}
	path, err := query.FindPathBetweenPolygons(high.ID, low.ID)
	if err != nil {
		t.Fatalf("FindPathBetweenPolygons high->low failed: %v", err)
	}
	if !path.Found {
		t.Fatalf("expected path down generated drop link, got %+v", path)
	}
	reverse, err := query.FindPathBetweenPolygons(low.ID, high.ID)
	if err != nil {
		t.Fatalf("FindPathBetweenPolygons low->high failed: %v", err)
	}
	if reverse.Found {
		t.Fatalf("did not expect one-way drop link to climb back up, got %+v", reverse)
	}
}

func TestBuildNavTileGeneratesMergedCrossTileDropLinks(t *testing.T) {
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.MaxDropHeight = 3
	left := navTestChunk(8, 1.0, navTestFloorVoxels(7, 7, 1, 2, 2)...)
	right := navTestChunk(8, 1.0, navTestFloorVoxels(0, 0, 1, 2, 0)...)
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	chunks := map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		left.Coord:  left,
		right.Coord: right,
	}

	result, err := BuildNavTileFromImportedWorldChunk(left, profile, NavTileBuildOptions{
		NavID:          "nav-cross-drop",
		NeighborChunks: chunks,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	links := navTestDropLinksToCoord(result.Tile, right.Coord)
	if len(links) != 1 {
		t.Fatalf("expected adjacent ledge cells to merge into one cross-tile drop link, got %+v", links)
	}
	link := links[0]
	if link.FromPolygonID == "" || link.ToPolygonID == "" || link.ToTileCoord != right.Coord {
		t.Fatalf("expected explicit cross-tile drop link refs, got %+v", link)
	}
	if link.Start[1] <= link.End[1] || link.Bidirectional {
		t.Fatalf("expected one-way descending cross-tile drop link, got %+v", link)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}

	rightResult, err := BuildNavTileFromImportedWorldChunk(right, profile, NavTileBuildOptions{
		NavID:          "nav-cross-drop",
		NeighborChunks: chunks,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk right failed: %v", err)
	}
	if reverseLinks := navTestDropLinksToCoord(rightResult.Tile, left.Coord); len(reverseLinks) != 0 {
		t.Fatalf("did not expect low tile to generate reverse climb links, got %+v", reverseLinks)
	}
}

func TestNavReachableTerrainRiseUsesStepHeightIndependentlyOfSlopeLimit(t *testing.T) {
	profile := navTestAgentProfile(0.2, 1.0, 0.45)
	profile.MaxSlopeDegrees = 45
	chunk := navTestChunk(16, 0.1)

	rise := navReachableTerrainRiseVoxels(chunk, profile, 0.5, 0.5, 1, 0)
	if rise != 4 {
		t.Fatalf("expected step-height rise of 4 voxels, got %d", rise)
	}
}

func TestBuildNavTileContoursFlatVoxelComponent(t *testing.T) {
	voxels := navTestFloorVoxels(1, 4, 1, 4, 0)
	voxels = removeNavTestFloorVoxels(voxels, 3, 4, 3, 4, 0)
	chunk := navTestChunk(6, 1.0, voxels...)
	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{NavID: "nav-contour"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) != 4 {
		t.Fatalf("expected L-shaped contour to decompose to four convex polygons, got %+v", result.Tile.Polygons)
	}
	if !navTilePolygonsAreConvexXZ(result.Tile) {
		t.Fatalf("expected decomposed contour polygons to be convex, got %+v", result.Tile.Polygons)
	}
	if !navTileHasCell(result.Tile, 1, 1, 1, chunk.VoxelResolution) ||
		!navTileHasCell(result.Tile, 4, 1, 2, chunk.VoxelResolution) ||
		!navTileHasCell(result.Tile, 2, 1, 4, chunk.VoxelResolution) {
		t.Fatalf("expected contour polygon to cover all L-shaped floor cells, got %+v", result.Tile.Polygons)
	}
	if navTileHasCell(result.Tile, 4, 1, 4, chunk.VoxelResolution) {
		t.Fatalf("did not expect contour polygon to cover missing corner, got %+v", result.Tile.Polygons)
	}
	if !navTileCellsHavePath(result.Tile, 1, 1, 1, 4, 1, 2, chunk.VoxelResolution) ||
		!navTileCellsHavePath(result.Tile, 1, 1, 1, 2, 1, 4, chunk.VoxelResolution) {
		t.Fatalf("expected decomposed contour pieces to stay connected, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTilePreservesFlatVoxelHole(t *testing.T) {
	voxels := navTestFloorVoxels(1, 6, 1, 6, 0)
	voxels = removeNavTestFloorVoxels(voxels, 3, 4, 3, 4, 0)
	chunk := navTestChunk(8, 1.0, voxels...)
	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{NavID: "nav-hole"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) == 0 {
		t.Fatalf("expected donut floor to produce nav polygons")
	}
	if !navTilePolygonsAreConvexXZ(result.Tile) {
		t.Fatalf("expected hole fixture polygons to be convex, got %+v", result.Tile.Polygons)
	}
	navAssertTileCoversCells(t, result.Tile, chunk.VoxelResolution, navTestCellCoordsForVoxels(voxels, 1)...)
	navAssertTileExcludesCells(t, result.Tile, chunk.VoxelResolution,
		navTestCellCoord{X: 3, Y: 1, Z: 3},
		navTestCellCoord{X: 4, Y: 1, Z: 3},
		navTestCellCoord{X: 3, Y: 1, Z: 4},
		navTestCellCoord{X: 4, Y: 1, Z: 4},
	)
	if !navTileCellsHavePath(result.Tile, 1, 1, 1, 6, 1, 6, chunk.VoxelResolution) {
		t.Fatalf("expected walkable ring around hole to stay connected, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileSimplifiesJaggedDiagonalContour(t *testing.T) {
	voxels := make([]ImportedWorldVoxelDef, 0)
	for z := 2; z <= 14; z++ {
		for x := 2; x <= z; x++ {
			voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: 0, Z: z, Value: 1})
		}
	}
	chunk := navTestChunk(18, 0.1, voxels...)
	profile := navTestAgentProfile(0.4, 1.0, 0.45)
	profile.NavCellSize = 0.1

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{NavID: "nav-jagged-diagonal"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) == 0 {
		t.Fatalf("expected jagged diagonal floor to produce nav polygons")
	}
	if len(result.Tile.Polygons) > 3 {
		t.Fatalf("expected jagged diagonal contour to simplify to a small polygon set, got %+v", result.Tile.Polygons)
	}
	if !navTilePolygonsAreConvexXZ(result.Tile) {
		t.Fatalf("expected simplified jagged contour polygons to be convex, got %+v", result.Tile.Polygons)
	}
	navAssertTileCoversCells(t, result.Tile, chunk.VoxelResolution,
		navTestCellCoord{X: 4, Y: 1, Z: 4},
		navTestCellCoord{X: 8, Y: 1, Z: 10},
		navTestCellCoord{X: 13, Y: 1, Z: 13},
	)
	navAssertTileExcludesCells(t, result.Tile, chunk.VoxelResolution,
		navTestCellCoord{X: 13, Y: 1, Z: 4},
		navTestCellCoord{X: 10, Y: 1, Z: 7},
	)
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileKeepsFlatVoxelIslandsDisconnected(t *testing.T) {
	voxels := navTestFloorVoxels(1, 2, 1, 2, 0)
	voxels = append(voxels, navTestFloorVoxels(5, 6, 5, 6, 0)...)
	chunk := navTestChunk(8, 1.0, voxels...)
	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{NavID: "nav-islands"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) != 2 {
		t.Fatalf("expected two disconnected island polygons, got %+v", result.Tile.Polygons)
	}
	if !navTilePolygonsAreConvexXZ(result.Tile) {
		t.Fatalf("expected island polygons to be convex, got %+v", result.Tile.Polygons)
	}
	navAssertTileCoversCells(t, result.Tile, chunk.VoxelResolution, navTestCellCoordsForVoxels(voxels, 1)...)
	if navTileCellsHavePath(result.Tile, 1, 1, 1, 6, 1, 6, chunk.VoxelResolution) {
		t.Fatalf("did not expect separated voxel islands to be path-connected, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTilePreservesMultipleHolesInConcaveRegion(t *testing.T) {
	voxels := navTestFloorVoxels(1, 10, 1, 10, 0)
	voxels = removeNavTestFloorVoxels(voxels, 4, 5, 4, 5, 0)
	voxels = removeNavTestFloorVoxels(voxels, 7, 8, 7, 8, 0)
	voxels = removeNavTestFloorVoxels(voxels, 1, 3, 8, 10, 0)
	voxels = removeNavTestFloorVoxels(voxels, 9, 10, 1, 3, 0)
	chunk := navTestChunk(12, 1.0, voxels...)

	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{NavID: "nav-complex-holes"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) == 0 {
		t.Fatalf("expected complex floor to produce nav polygons")
	}
	if !navTilePolygonsAreConvexXZ(result.Tile) {
		t.Fatalf("expected complex hole fixture polygons to be convex, got %+v", result.Tile.Polygons)
	}
	navAssertTileCoversCells(t, result.Tile, chunk.VoxelResolution,
		navTestCellCoord{X: 1, Y: 1, Z: 1},
		navTestCellCoord{X: 10, Y: 1, Z: 10},
		navTestCellCoord{X: 6, Y: 1, Z: 6},
		navTestCellCoord{X: 3, Y: 1, Z: 7},
		navTestCellCoord{X: 9, Y: 1, Z: 4},
	)
	navAssertTileExcludesCells(t, result.Tile, chunk.VoxelResolution,
		navTestCellCoord{X: 4, Y: 1, Z: 4},
		navTestCellCoord{X: 5, Y: 1, Z: 5},
		navTestCellCoord{X: 7, Y: 1, Z: 7},
		navTestCellCoord{X: 8, Y: 1, Z: 8},
		navTestCellCoord{X: 2, Y: 1, Z: 9},
		navTestCellCoord{X: 9, Y: 1, Z: 2},
	)
	navAssertTileCellsHaveSinglePolygon(t, result.Tile, chunk.VoxelResolution, navTestCellCoordsForVoxels(voxels, 1)...)
	if !navTileCellsHavePath(result.Tile, 1, 1, 1, 10, 1, 10, chunk.VoxelResolution) {
		t.Fatalf("expected concave ring around holes to stay connected, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileKeepsNestedIslandInsideHoleDisconnected(t *testing.T) {
	voxels := navTestFloorVoxels(1, 9, 1, 9, 0)
	voxels = removeNavTestFloorVoxels(voxels, 3, 7, 3, 7, 0)
	voxels = append(voxels, navTestFloorVoxels(5, 5, 5, 5, 0)...)
	chunk := navTestChunk(11, 1.0, voxels...)

	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{NavID: "nav-nested-island"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) < 2 {
		t.Fatalf("expected outer ring and inner island polygons, got %+v", result.Tile.Polygons)
	}
	navAssertTileCoversCells(t, result.Tile, chunk.VoxelResolution,
		navTestCellCoord{X: 1, Y: 1, Z: 1},
		navTestCellCoord{X: 9, Y: 1, Z: 9},
		navTestCellCoord{X: 5, Y: 1, Z: 5},
	)
	navAssertTileExcludesCells(t, result.Tile, chunk.VoxelResolution,
		navTestCellCoord{X: 3, Y: 1, Z: 3},
		navTestCellCoord{X: 4, Y: 1, Z: 5},
		navTestCellCoord{X: 6, Y: 1, Z: 5},
		navTestCellCoord{X: 7, Y: 1, Z: 7},
	)
	navAssertTileCellsHaveSinglePolygon(t, result.Tile, chunk.VoxelResolution, navTestCellCoordsForVoxels(voxels, 1)...)
	if navTileCellsHavePath(result.Tile, 1, 1, 1, 5, 1, 5, chunk.VoxelResolution) {
		t.Fatalf("did not expect island inside hole to connect to outer ring, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileDoesNotOverlapPolygonsOnMazeConcavity(t *testing.T) {
	voxels := navTestFloorVoxels(1, 11, 1, 11, 0)
	for z := 2; z <= 10; z++ {
		if z == 5 {
			continue
		}
		voxels = removeNavTestFloorVoxels(voxels, 3, 3, z, z, 0)
	}
	for z := 2; z <= 10; z++ {
		if z == 8 {
			continue
		}
		voxels = removeNavTestFloorVoxels(voxels, 7, 7, z, z, 0)
	}
	for x := 4; x <= 10; x++ {
		if x == 9 {
			continue
		}
		voxels = removeNavTestFloorVoxels(voxels, x, x, 6, 6, 0)
	}
	chunk := navTestChunk(13, 1.0, voxels...)

	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{NavID: "nav-maze-concavity"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) == 0 {
		t.Fatalf("expected maze floor to produce nav polygons")
	}
	if !navTilePolygonsAreConvexXZ(result.Tile) {
		t.Fatalf("expected maze polygons to be convex, got %+v", result.Tile.Polygons)
	}
	navAssertTileCellsHaveSinglePolygon(t, result.Tile, chunk.VoxelResolution, navTestCellCoordsForVoxels(voxels, 1)...)
	if !navTileCellsHavePath(result.Tile, 1, 1, 1, 11, 1, 11, chunk.VoxelResolution) {
		t.Fatalf("expected maze concavity to remain connected through openings, got %+v", result.Tile.Polygons)
	}
	navAssertTileExcludesCells(t, result.Tile, chunk.VoxelResolution,
		navTestCellCoord{X: 3, Y: 1, Z: 2},
		navTestCellCoord{X: 3, Y: 1, Z: 10},
		navTestCellCoord{X: 7, Y: 1, Z: 2},
		navTestCellCoord{X: 7, Y: 1, Z: 10},
		navTestCellCoord{X: 5, Y: 1, Z: 6},
	)
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileUsesCoarseVoxelRasterAtProfileCellSize(t *testing.T) {
	chunk := navTestChunk(40, 0.1, navTestFloorVoxels(10, 29, 10, 29, 0)...)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = 0.5
	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{NavID: "nav-coarse-voxel"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.WalkableCells) != 16 {
		t.Fatalf("expected dense voxel floor to downsample to 16 nav cells, got %d", len(result.WalkableCells))
	}
	if len(result.Tile.Polygons) != 1 {
		t.Fatalf("expected coarse voxel cells to merge into one polygon, got %+v", result.Tile.Polygons)
	}
	polygon := result.Tile.Polygons[0]
	if polygon.ID != "rect:2:1:2:5:5" {
		t.Fatalf("expected stable coarse voxel region id, got %+v", polygon)
	}
	bounds, ok := navPolygonBoundsForTile(result.Tile, polygon)
	if !ok {
		t.Fatalf("expected coarse voxel polygon bounds")
	}
	if bounds.min != (Vec3{1, 0.1, 1}) || bounds.max != (Vec3{3, 0.1, 3}) {
		t.Fatalf("unexpected coarse voxel bounds: %+v", bounds)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileUsesWorldAlignedCoarseVoxelRaster(t *testing.T) {
	chunk := navTestChunk(7, 1.0, navTestFloorVoxels(0, 6, 1, 5, 0)...)
	chunk.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = 3.0

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{NavID: "nav-world-aligned-coarse"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.WalkableCells) == 0 {
		t.Fatalf("expected coarse walkable cells")
	}
	seenX := map[int]struct{}{}
	for _, cell := range result.WalkableCells {
		seenX[cell.X] = struct{}{}
	}
	for _, x := range []int{2, 3, 4} {
		if _, ok := seenX[x]; !ok {
			t.Fatalf("expected world-aligned coarse X bucket %d in %+v", x, result.WalkableCells)
		}
	}
	if _, ok := seenX[0]; ok {
		t.Fatalf("did not expect chunk-local coarse X bucket 0 in non-zero chunk cells: %+v", result.WalkableCells)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileClampsCoarseVoxelPolygonsToTileBounds(t *testing.T) {
	center := navTestChunk(256, 0.1, navTestFloorVoxels(246, 255, 32, 64, 0)...)
	right := navTestChunk(256, 0.1)
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = DefaultNavCellSize

	result, err := BuildNavTileFromImportedWorldChunk(center, profile, NavTileBuildOptions{
		NavID: "nav-coarse-boundary",
		NeighborChunks: map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
			right.Coord: right,
		},
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) == 0 {
		t.Fatalf("expected boundary floor to produce nav polygons")
	}
	for i, vertex := range result.Tile.Vertices {
		if vertex[0] < result.Tile.BoundsMin[0]-1e-4 || vertex[0] > result.Tile.BoundsMax[0]+1e-4 ||
			vertex[2] < result.Tile.BoundsMin[2]-1e-4 || vertex[2] > result.Tile.BoundsMax[2]+1e-4 {
			t.Fatalf("vertex %d escaped tile bounds: vertex=%+v bounds=%+v..%+v", i, vertex, result.Tile.BoundsMin, result.Tile.BoundsMax)
		}
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileClampedCoarseVoxelPolygonsPortalAcrossBoundary(t *testing.T) {
	left := navTestChunk(256, 0.1, navTestFloorVoxels(246, 255, 32, 64, 0)...)
	left.Coord = TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	right := navTestChunk(256, 0.1, navTestFloorVoxels(0, 9, 32, 64, 0)...)
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = DefaultNavCellSize

	leftResult, err := BuildNavTileFromImportedWorldChunk(left, profile, NavTileBuildOptions{
		NavID: "nav-coarse-left",
		NeighborChunks: map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
			right.Coord: right,
		},
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk left failed: %v", err)
	}
	rightResult, err := BuildNavTileFromImportedWorldChunk(right, profile, NavTileBuildOptions{
		NavID: "nav-coarse-right",
		NeighborChunks: map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
			left.Coord: left,
		},
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk right failed: %v", err)
	}

	applyNavTilePortals([]*NavTileDef{leftResult.Tile, rightResult.Tile}, map[string]NavAgentProfileDef{profile.ID: profile})

	if len(leftResult.Tile.Portals) == 0 || len(rightResult.Tile.Portals) == 0 {
		t.Fatalf("expected clamped coarse boundary polygons to portal, left=%+v right=%+v", leftResult.Tile.Portals, rightResult.Tile.Portals)
	}
	for _, portal := range leftResult.Tile.Portals {
		if portal.Start[0] != leftResult.Tile.BoundsMax[0] || portal.End[0] != leftResult.Tile.BoundsMax[0] {
			t.Fatalf("expected left portal on tile boundary, got %+v bounds=%+v", portal, leftResult.Tile.BoundsMax)
		}
	}
}

func TestBuildNavTileBorderSpansDrivePortalIntervals(t *testing.T) {
	leftVoxels := navTestFloorVoxels(6, 7, 0, 7, 0)
	leftVoxels = removeNavTestFloorVoxels(leftVoxels, 6, 7, 3, 4, 0)
	left := navTestChunk(8, 1.0, leftVoxels...)
	left.Coord = TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	right := navTestChunk(8, 1.0, navTestFloorVoxels(0, 1, 0, 7, 0)...)
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	chunks := map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		left.Coord:  left,
		right.Coord: right,
	}
	profile := navTestAgentProfile(0.2, 1.0, 0.5)

	leftResult, err := BuildNavTileFromImportedWorldChunk(left, profile, NavTileBuildOptions{
		NavID:          "nav-border-span-left",
		NeighborChunks: chunks,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk left failed: %v", err)
	}
	rightResult, err := BuildNavTileFromImportedWorldChunk(right, profile, NavTileBuildOptions{
		NavID:          "nav-border-span-right",
		NeighborChunks: chunks,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk right failed: %v", err)
	}

	leftMaxSpans := navTestBorderSpansForEdge(leftResult.Tile, NavBorderEdgeMaxX)
	if len(leftMaxSpans) != 2 {
		t.Fatalf("expected left voxel-derived border spans to preserve seam gap, got %+v tile=%+v", leftMaxSpans, leftResult.Tile)
	}
	if !navAlmostEqual(leftMaxSpans[0].Min, 0, 1e-4) || !navAlmostEqual(leftMaxSpans[0].Max, 3, 1e-4) ||
		!navAlmostEqual(leftMaxSpans[1].Min, 5, 1e-4) || !navAlmostEqual(leftMaxSpans[1].Max, 8, 1e-4) {
		t.Fatalf("unexpected left border spans: %+v", leftMaxSpans)
	}

	applyNavTilePortals([]*NavTileDef{leftResult.Tile, rightResult.Tile}, map[string]NavAgentProfileDef{profile.ID: profile})

	if len(leftResult.Tile.Portals) != 2 || len(rightResult.Tile.Portals) != 2 {
		t.Fatalf("expected two span-backed seam portals, left=%+v right=%+v", leftResult.Tile.Portals, rightResult.Tile.Portals)
	}
	first := leftResult.Tile.Portals[0]
	second := leftResult.Tile.Portals[1]
	if first.Start[2] > second.Start[2] {
		first, second = second, first
	}
	if !navAlmostEqual(first.Start[2], 0, 1e-4) || !navAlmostEqual(first.End[2], 3, 1e-4) {
		t.Fatalf("expected first span-backed portal to cover 0..3, got %+v", first)
	}
	if !navAlmostEqual(second.Start[2], 5, 1e-4) || !navAlmostEqual(second.End[2], 8, 1e-4) {
		t.Fatalf("expected second span-backed portal to cover 5..8, got %+v", second)
	}
}

func TestBuildNavTileStitchesBoundaryHeightsFromNeighborChunk(t *testing.T) {
	left := navTestChunk(256, 0.1, navTestFloorVoxels(246, 255, 32, 64, 0)...)
	left.Coord = TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightVoxels := navTestFloorVoxels(0, 9, 32, 64, 0)
	rightVoxels = append(rightVoxels, navTestFloorVoxels(0, 9, 32, 64, 1)...)
	right := navTestChunk(256, 0.1, rightVoxels...)
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	chunks := map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		left.Coord:  left,
		right.Coord: right,
	}
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = DefaultNavCellSize

	leftResult, err := BuildNavTileFromImportedWorldChunk(left, profile, NavTileBuildOptions{
		NavID:          "nav-stitch-left",
		NeighborChunks: chunks,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk left failed: %v", err)
	}
	rightResult, err := BuildNavTileFromImportedWorldChunk(right, profile, NavTileBuildOptions{
		NavID:          "nav-stitch-right",
		NeighborChunks: chunks,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk right failed: %v", err)
	}

	leftHeights := navTileBoundaryVertexHeights(leftResult.Tile, 0, leftResult.Tile.BoundsMax[0], 2, 3.0, 6.6)
	rightHeights := navTileBoundaryVertexHeights(rightResult.Tile, 0, rightResult.Tile.BoundsMin[0], 2, 3.0, 6.6)
	if len(leftHeights) == 0 || len(rightHeights) == 0 {
		t.Fatalf("expected seam vertices on both tiles, left=%+v right=%+v", leftResult.Tile.Vertices, rightResult.Tile.Vertices)
	}
	for _, height := range append(leftHeights, rightHeights...) {
		if absNavFloat32(height-0.15) > 1e-4 {
			t.Fatalf("expected stitched seam height 0.15, got left=%+v right=%+v", leftHeights, rightHeights)
		}
	}
}

func TestNavTileBuildCacheReusesBorderStitchIntermediates(t *testing.T) {
	left := navTestChunk(256, 0.1, navTestFloorVoxels(246, 255, 32, 64, 0)...)
	left.Coord = TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	right := navTestChunk(256, 0.1, navTestFloorVoxels(0, 9, 32, 64, 0)...)
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	chunks := map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		left.Coord:  left,
		right.Coord: right,
	}
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = DefaultNavCellSize
	cache := &NavTileBuildCache{}

	for i := 0; i < 2; i++ {
		if _, err := BuildNavTileFromImportedWorldChunk(left, profile, NavTileBuildOptions{
			NavID:          "nav-cache-left",
			NeighborChunks: chunks,
			BuildCache:     cache,
		}); err != nil {
			t.Fatalf("BuildNavTileFromImportedWorldChunk left failed: %v", err)
		}
		if _, err := BuildNavTileFromImportedWorldChunk(right, profile, NavTileBuildOptions{
			NavID:          "nav-cache-right",
			NeighborChunks: chunks,
			BuildCache:     cache,
		}); err != nil {
			t.Fatalf("BuildNavTileFromImportedWorldChunk right failed: %v", err)
		}
	}

	if cache.intermediateBuilds != 2 {
		t.Fatalf("expected each chunk/profile intermediate to be built once, got %d builds", cache.intermediateBuilds)
	}
	if len(cache.intermediates) != 2 {
		t.Fatalf("expected two cached intermediates, got %d", len(cache.intermediates))
	}
}

func TestNavTileBuildCacheInvalidatesWhenChunkVoxelsChange(t *testing.T) {
	chunk := navTestChunk(6, 1.0, navTestFloorVoxels(1, 4, 1, 4, 0)...)
	chunk.PayloadHash = "stale-payload-hash"
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	cache := &NavTileBuildCache{}

	first, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		NavID:      "nav-cache-mutation",
		BuildCache: cache,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk first failed: %v", err)
	}
	if len(first.Tile.Polygons) == 0 {
		t.Fatalf("expected initial floor polygons")
	}

	chunk.Voxels = nil
	second, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		NavID:      "nav-cache-mutation",
		BuildCache: cache,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk second failed: %v", err)
	}
	if len(second.Tile.Polygons) != 0 {
		t.Fatalf("expected mutated empty chunk to rebuild without polygons, got %+v", second.Tile.Polygons)
	}
	if cache.intermediateBuilds != 2 {
		t.Fatalf("expected cache to rebuild after voxel mutation, got %d builds", cache.intermediateBuilds)
	}
}

func TestNavTileBuildCacheCanTrustPayloadHashForImmutableImportedChunks(t *testing.T) {
	chunk := navTestChunk(6, 1.0, navTestFloorVoxels(1, 4, 1, 4, 0)...)
	chunk.PayloadHash = "payload-hash"
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	cache := &NavTileBuildCache{TrustPayloadHash: true}

	first, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		NavID:      "nav-cache-trust-payload",
		BuildCache: cache,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk first failed: %v", err)
	}
	if len(first.Tile.Polygons) == 0 {
		t.Fatalf("expected initial floor polygons")
	}

	chunk.Voxels = nil
	second, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		NavID:      "nav-cache-trust-payload",
		BuildCache: cache,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk second failed: %v", err)
	}
	if len(second.Tile.Polygons) == 0 {
		t.Fatalf("expected trusted payload cache to reuse cached polygons")
	}
	if cache.intermediateBuilds != 1 {
		t.Fatalf("expected trusted payload hash cache to reuse intermediate, got %d builds", cache.intermediateBuilds)
	}
}

func TestNavTileBuildCacheInvalidatesWhenNeighborVoxelsChange(t *testing.T) {
	center := navTestChunk(4, 1.0, ImportedWorldVoxelDef{X: 3, Y: 0, Z: 1, Value: 1})
	right := navTestChunk(4, 1.0)
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	chunks := map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		center.Coord: center,
		right.Coord:  right,
	}
	profile := navTestAgentProfile(0.6, 1.0, 1.0)
	cache := &NavTileBuildCache{}

	first, err := BuildNavTileFromImportedWorldChunk(center, profile, NavTileBuildOptions{
		NeighborChunks: chunks,
		BuildCache:     cache,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk first failed: %v", err)
	}
	if !navTileHasCell(first.Tile, 3, 1, 1, center.VoxelResolution) {
		t.Fatalf("expected boundary cell with initially clear neighbor, got %+v", first.Tile.Polygons)
	}

	right.Voxels = []ImportedWorldVoxelDef{{X: 0, Y: 1, Z: 1, Value: 1}}
	second, err := BuildNavTileFromImportedWorldChunk(center, profile, NavTileBuildOptions{
		NeighborChunks: chunks,
		BuildCache:     cache,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk second failed: %v", err)
	}
	if navTileHasCell(second.Tile, 3, 1, 1, center.VoxelResolution) {
		t.Fatalf("expected neighbor mutation to block boundary cell, got %+v", second.Tile.Polygons)
	}
	if cache.intermediateBuilds < 3 {
		t.Fatalf("expected cache to rebuild after neighbor mutation, got %d builds", cache.intermediateBuilds)
	}
}

func TestNavBuildBorderStitcherUsesDiagonalNeighborAtCorner(t *testing.T) {
	profile := navTestAgentProfile(0.2, 1.0, 10)
	profile.MaxSlopeDegrees = 0
	tile := &NavTileDef{
		Coord:     TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		BoundsMin: [3]float32{0, 0, 0},
		BoundsMax: [3]float32{1, 1, 1},
	}
	stitcher := &navBuildBorderStitcher{
		Profile: profile,
		Metrics: navBuildCellMetrics{Horizontal: 1, Vertical: 0.1},
		Center:  TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		Neighbors: map[TerrainChunkCoordDef]navBuildBorderNeighbor{
			{X: 1, Y: 0, Z: 0}: {
				Origin:  [3]float32{1, 0, 0},
				Regions: []navBuildWalkableRegion{{MinX: 0, MaxX: 0, MinZ: 0, MaxZ: 0, BaseY: 11}},
			},
			{X: 0, Y: 0, Z: 1}: {
				Origin:  [3]float32{0, 0, 1},
				Regions: []navBuildWalkableRegion{{MinX: 0, MaxX: 0, MinZ: 0, MaxZ: 0, BaseY: 12}},
			},
			{X: 1, Y: 0, Z: 1}: {
				Origin:  [3]float32{1, 0, 1},
				Regions: []navBuildWalkableRegion{{MinX: 0, MaxX: 0, MinZ: 0, MaxZ: 0, BaseY: 13}},
			},
		},
	}

	height, ok := stitcher.StitchedHeight(tile, 1, 1, 1)
	if !ok {
		t.Fatalf("expected corner stitch height")
	}
	if absNavFloat32(height-1.15) > 1e-4 {
		t.Fatalf("expected current plus side and diagonal neighbors to average to 1.15, got %f", height)
	}
}

func TestBuildNavTileSnapsCoarseVoxelHeightNoiseOnWideFloor(t *testing.T) {
	voxels := navTestFloorVoxels(10, 29, 10, 29, 0)
	voxels = removeNavTestFloorVoxels(voxels, 15, 19, 15, 19, 0)
	voxels = append(voxels, navTestFloorVoxels(15, 19, 15, 19, 1)...)
	chunk := navTestChunk(40, 0.1, voxels...)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = 0.5

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{NavID: "nav-coarse-noise"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.WalkableCells) != 16 {
		t.Fatalf("expected noisy dense floor to downsample to 16 nav cells, got %d", len(result.WalkableCells))
	}
	if len(result.Tile.Polygons) != 1 {
		t.Fatalf("expected one snapped wide-floor polygon, got %+v", result.Tile.Polygons)
	}
	polygon := result.Tile.Polygons[0]
	if polygon.ID != "rect:2:1:2:5:5" {
		t.Fatalf("expected noisy cell to snap into dominant floor region, got %+v", polygon)
	}
	bounds, ok := navPolygonBoundsForTile(result.Tile, polygon)
	if !ok {
		t.Fatalf("expected snapped polygon bounds")
	}
	if bounds.min != (Vec3{1, 0.1, 1}) || bounds.max != (Vec3{3, 0.1, 3}) {
		t.Fatalf("unexpected snapped polygon bounds: %+v", bounds)
	}
}

func TestBuildNavTileSmoothsCompactHeightfieldNoiseInWorldUnits(t *testing.T) {
	voxels := make([]ImportedWorldVoxelDef, 0)
	for x := 4; x <= 15; x++ {
		for z := 4; z <= 15; z++ {
			topY := 0
			if x >= 8 && x <= 9 && z >= 8 && z <= 9 {
				topY = 1
			}
			for y := 0; y <= topY; y++ {
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}
	chunk := navTestChunk(24, 0.1, voxels...)
	profile := navTestAgentProfile(0.2, 1.0, 0.45)
	profile.NavCellSize = 0.05

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{NavID: "nav-compact-noise"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.WalkableCells) == 0 {
		t.Fatalf("expected noisy compact floor to keep walkable cells")
	}
	if len(result.Tile.Polygons) != 1 {
		t.Fatalf("expected compact heightfield smoothing to produce one floor polygon, got %+v", result.Tile.Polygons)
	}
	polygon := result.Tile.Polygons[0]
	if polygon.Area != NavTraversalWalk {
		t.Fatalf("expected smoothed slight noise to remain ordinary walk, got %+v", polygon)
	}
	navAssertTileCoversCells(t, result.Tile, chunk.VoxelResolution,
		navTestCellCoord{X: 4, Y: 1, Z: 4},
		navTestCellCoord{X: 15, Y: 1, Z: 15},
	)
	if navTileHasCell(result.Tile, 8, 2, 8, chunk.VoxelResolution) {
		t.Fatalf("did not expect one-voxel noise to survive as separate nav height, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileDoesNotSmoothWorldSignificantVoxelLip(t *testing.T) {
	voxels := navTestFloorVoxels(1, 3, 1, 3, 0)
	voxels = append(voxels, navTestFloorVoxels(4, 6, 1, 3, 1)...)
	chunk := navTestChunk(8, 0.25, voxels...)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = 0.1

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{NavID: "nav-significant-lip"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTileHasCell(result.Tile, 2, 1, 2, chunk.VoxelResolution) {
		t.Fatalf("expected lower floor cell to remain walkable, got %+v", result.Tile.Polygons)
	}
	if !navTileHasCell(result.Tile, 5, 2, 2, chunk.VoxelResolution) {
		t.Fatalf("expected world-significant upper lip height to be preserved, got %+v", result.Tile.Polygons)
	}
	if navTileHasCell(result.Tile, 5, 1, 2, chunk.VoxelResolution) {
		t.Fatalf("did not expect upper lip to be flattened to the lower floor, got %+v", result.Tile.Polygons)
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
	if navTileHasCell(result.Tile, 3, 1, 3, chunk.VoxelResolution) {
		t.Fatalf("expected wall column to be blocked, got polygons %+v", result.Tile.Polygons)
	}
	if navTileCellsAreConnected(result.Tile, 2, 1, 3, 4, 1, 3, chunk.VoxelResolution) {
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
	if !navTileCellsAreConnected(result.Tile, 2, 1, 1, 2, 2, 2, chunk.VoxelResolution) {
		t.Fatalf("expected first stair step to connect, got %+v", result.Tile.Polygons)
	}
	if !navTileCellsAreConnected(result.Tile, 2, 2, 2, 2, 3, 3, chunk.VoxelResolution) {
		t.Fatalf("expected second stair step to connect, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileMergesRampStepsIntoSlopedPolygon(t *testing.T) {
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
	if len(result.Tile.Polygons) != 1 {
		t.Fatalf("expected ramp steps to merge to one sloped polygon, got %+v", result.Tile.Polygons)
	}
	polygon := result.Tile.Polygons[0]
	if polygon.ID != "plane:2:1:1:2:3:0:1" {
		t.Fatalf("expected sloped plane polygon id, got %q", polygon.ID)
	}
	if polygon.Area != NavTraversalRamp {
		t.Fatalf("expected slope within limit to be classified as ramp, got %+v", polygon)
	}
	if !navTileHasCell(result.Tile, 2, 1, 1, chunk.VoxelResolution) ||
		!navTileHasCell(result.Tile, 2, 2, 2, chunk.VoxelResolution) ||
		!navTileHasCell(result.Tile, 2, 3, 3, chunk.VoxelResolution) {
		t.Fatalf("expected sloped polygon surface to cover all ramp cells, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileClassifiesSteepVoxelStairsAsStair(t *testing.T) {
	voxels := make([]ImportedWorldVoxelDef, 0)
	for x := 2; x <= 5; x++ {
		for z := 1; z <= 4; z++ {
			height := z - 1
			for y := 0; y <= height; y++ {
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}
	chunk := navTestChunk(8, 0.5, voxels...)
	profile := navTestAgentProfile(0.1, 0.5, 0.5)
	profile.MaxSlopeDegrees = 30

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTileHasPolygonArea(result.Tile, NavTraversalStair) {
		t.Fatalf("expected steep stepped surface to be classified as stair, got %+v", result.Tile.Polygons)
	}
	if navTileHasPolygonArea(result.Tile, NavTraversalRamp) {
		t.Fatalf("did not expect steep stepped surface above slope limit to be classified as ramp, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileClassifiesTinyVoxelRiseAsStep(t *testing.T) {
	voxels := []ImportedWorldVoxelDef{
		{X: 2, Y: 0, Z: 1, Value: 1},
		{X: 2, Y: 0, Z: 2, Value: 1},
		{X: 2, Y: 1, Z: 2, Value: 1},
	}
	chunk := navTestChunk(8, 0.5, voxels...)
	profile := navTestAgentProfile(0.1, 0.5, 0.5)
	profile.MaxSlopeDegrees = 30

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTileHasPolygonArea(result.Tile, NavTraversalStep) {
		t.Fatalf("expected tiny stepped surface to be classified as step, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileClampsSlopedVerticesToTopTileBounds(t *testing.T) {
	voxels := make([]ImportedWorldVoxelDef, 0)
	for x := 1; x <= 2; x++ {
		for z := 0; z <= 3; z++ {
			for y := 0; y <= z; y++ {
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}
	chunk := navTestChunk(4, 1.0, voxels...)
	chunk.Coord = TerrainChunkCoordDef{X: 0, Y: -1, Z: 0}
	above := navTestChunk(4, 1.0)
	above.Coord = TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	profile := navTestAgentProfile(0.2, 0.5, 1.0)

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		NeighborChunks: map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
			above.Coord: above,
		},
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) == 0 {
		t.Fatalf("expected top-boundary sloped surface to produce nav")
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s (%+v)", validation.Error(), validation.Issues)
	}
	for _, vertex := range result.Tile.Vertices {
		if vertex[1] > result.Tile.BoundsMax[1]+1e-4 {
			t.Fatalf("expected vertex height to be clamped to tile max, vertex=%+v bounds=%+v", vertex, result.Tile.BoundsMax)
		}
	}
}

func TestBuildNavTileKeepsHL1ScaleVoxelRampWalkable(t *testing.T) {
	voxels := make([]ImportedWorldVoxelDef, 0)
	for x := 10; x <= 24; x++ {
		for z := 10; z <= 24; z++ {
			height := (z - 10) / 5
			for y := 0; y <= height; y++ {
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}
	chunk := navTestChunk(40, 0.1, voxels...)
	profile := DefaultHL1NavAgentProfile()

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.WalkableCells) == 0 || len(result.Tile.Polygons) == 0 {
		t.Fatalf("expected HL1-scale voxel ramp to produce nav, cells=%d tile=%+v", len(result.WalkableCells), result.Tile)
	}
	if len(result.Tile.Polygons) > 4 {
		t.Fatalf("expected HL1-scale voxel ramp to decompose into few convex polygons, got %+v", result.Tile.Polygons)
	}
	if !navTilePolygonsAreConvexXZ(result.Tile) {
		t.Fatalf("expected HL1-scale voxel ramp polygons to be convex, got %+v", result.Tile.Polygons)
	}
	if !navTileHasNonFlatPolygon(result.Tile) {
		t.Fatalf("expected HL1-scale voxel ramp to produce at least one non-flat polygon, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileMergesHighResolutionUnevenWalkableSurface(t *testing.T) {
	voxels := make([]ImportedWorldVoxelDef, 0)
	for x := 10; x <= 25; x++ {
		for z := 10; z <= 25; z++ {
			height := (x-10)/2 + (z-10)/8
			height += int(math.Round(5 * math.Sin(float64(x-10)/15*math.Pi)))
			for y := 0; y <= height; y++ {
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}
	chunk := navTestChunk(40, 0.025, voxels...)
	profile := navTestAgentProfile(0.05, 0.5, 0.3)
	profile.NavCellSize = 0.025
	profile.MaxSlopeDegrees = 45

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) != 1 {
		t.Fatalf("expected high-resolution uneven walkable surface to merge into one non-flat polygon, got %+v", result.Tile.Polygons)
	}
	if !navTileHasNonFlatPolygon(result.Tile) {
		t.Fatalf("expected high-resolution uneven walkable surface to use a non-flat contour polygon, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileMergesRampBesideWideFlatFloor(t *testing.T) {
	voxels := make([]ImportedWorldVoxelDef, 0)
	for x := 8; x <= 30; x++ {
		for z := 4; z <= 11; z++ {
			voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: 0, Z: z, Value: 1})
		}
	}
	for x := 8; x <= 30; x++ {
		for z := 12; z <= 30; z++ {
			height := (z - 12) / 4
			for y := 0; y <= height; y++ {
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}
	chunk := navTestChunk(40, 0.1, voxels...)
	profile := DefaultHL1NavAgentProfile()

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) > 4 {
		t.Fatalf("expected wide floor plus ramp to merge into few polygons, got %+v", result.Tile.Polygons)
	}
	if !navTileHasNonFlatPolygon(result.Tile) {
		t.Fatalf("expected wide floor plus ramp to keep a non-flat ramp polygon, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileKeepsHL1ScaleVoxelStairsWalkableAcrossAgentRadius(t *testing.T) {
	voxels := make([]ImportedWorldVoxelDef, 0)
	for x := 10; x <= 24; x++ {
		for z := 10; z <= 24; z++ {
			height := z - 10
			for y := 0; y <= height; y++ {
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}
	chunk := navTestChunk(40, 0.1, voxels...)
	profile := DefaultHL1NavAgentProfile()
	profile.StepHeight = 0.5

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.WalkableCells) == 0 || len(result.Tile.Polygons) == 0 {
		t.Fatalf("expected dense voxel stairs to produce nav, cells=%d tile=%+v", len(result.WalkableCells), result.Tile)
	}
	if len(result.Tile.Polygons) > 4 {
		t.Fatalf("expected dense voxel stairs to decompose into few convex polygons, got %+v", result.Tile.Polygons)
	}
	if !navTilePolygonsAreConvexXZ(result.Tile) {
		t.Fatalf("expected dense voxel stair polygons to be convex, got %+v", result.Tile.Polygons)
	}
	if !navTileHasNonFlatPolygon(result.Tile) {
		t.Fatalf("expected dense voxel stairs to produce at least one non-flat polygon, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileRejectsSlopeAboveProfileLimit(t *testing.T) {
	chunk := navTestChunk(8, 0.5)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.MaxSlopeDegrees = 30
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{{
			ID: "steep_ramp",
			Vertices: []Vec3{
				{1, 1, 1},
				{4, 1, 1},
				{4, 4, 4},
				{1, 4, 4},
			},
		}},
	}
	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) != 0 {
		t.Fatalf("did not expect steep ramp above slope profile, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileUsesPrimarySourceRampOverVoxelRegions(t *testing.T) {
	chunk := navTestChunk(8, 0.5, navTestFloorVoxels(1, 6, 1, 6, 0)...)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.MaxSlopeDegrees = 45
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{{
			ID: "authored_ramp",
			Vertices: []Vec3{
				{1, 1, 1},
				{4, 1, 1},
				{4, 2, 4},
				{1, 2, 4},
			},
			Area: NavTraversalWalk,
		}},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.WalkableCells) == 0 {
		t.Fatalf("expected voxel cells to remain available for fallback/debug data")
	}
	if len(result.Tile.Polygons) != 1 {
		t.Fatalf("expected primary source ramp without voxel duplicates, got %+v", result.Tile.Polygons)
	}
	polygon := result.Tile.Polygons[0]
	if polygon.ID != "surface:authored_ramp" {
		t.Fatalf("expected source ramp polygon, got %+v", polygon)
	}
	if polygon.Area != NavTraversalRamp {
		t.Fatalf("expected primary source ramp to be classified as ramp, got %+v", polygon)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTilePrimarySourceRejectsCellsNearThinBlocker(t *testing.T) {
	voxels := []ImportedWorldVoxelDef{{X: 8, Y: 1, Z: 8, Value: 1}}
	chunk := navTestChunk(16, 0.25, voxels...)
	profile := navTestAgentProfile(0.4, 1.0, 0.5)
	profile.NavCellSize = 0.5
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Tags:     []string{"source:imported_world"},
		Surfaces: []NavBuildSurfaceDef{{
			ID: "source_floor",
			Vertices: []Vec3{
				{0.5, 0.25, 0.5},
				{3.5, 0.25, 0.5},
				{3.5, 0.25, 3.5},
				{0.5, 0.25, 3.5},
			},
			Area:      NavTraversalWalk,
			SourceTag: "imported_world:0:0:0",
			Tags:      []string{"source:imported_world"},
		}},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
		NeighborChunks:     navTestEmptyNeighborChunks(chunk, navBuildChunkReachForProfile(chunk.ChunkSize, chunk.VoxelResolution, profile)),
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) == 0 {
		t.Fatalf("expected remaining source nav away from blocker, got %+v", result.Tile)
	}
	if navTileHasCell(result.Tile, 8, 1, 8, chunk.VoxelResolution) {
		t.Fatalf("did not expect primary source nav over thin blocker clearance, got %+v", result.Tile.Polygons)
	}
	if !navTileHasCell(result.Tile, 2, 1, 2, chunk.VoxelResolution) {
		t.Fatalf("expected source nav away from blocker to remain, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileHL1SourceKeepsDirectPolygonsDespiteImportedWorldSourceTag(t *testing.T) {
	chunk := navTestChunk(16, 0.25)
	profile := navTestAgentProfile(0.4, 1.0, 0.5)
	profile.NavCellSize = 0.5
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Tags:     []string{"source:imported_world", "source:hl1"},
		Surfaces: []NavBuildSurfaceDef{{
			ID: "hl1_floor",
			Vertices: []Vec3{
				{0.5, 0.25, 0.5},
				{3.5, 0.25, 0.5},
				{3.5, 0.25, 3.5},
				{0.5, 0.25, 3.5},
			},
			Area:      NavTraversalWalk,
			SourceTag: "hl1:model:0:face:4",
			Tags:      []string{"source:explicit_surface", "source:hl1"},
		}},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTileHasPolygon(result.Tile, "surface:hl1_floor") {
		t.Fatalf("expected HL1 source surface to keep direct polygon, got %+v", result.Tile.Polygons)
	}
	for _, polygon := range result.Tile.Polygons {
		if strings.HasPrefix(polygon.ID, "raster_region:") || strings.HasPrefix(polygon.ID, "raster_cell:") {
			t.Fatalf("did not expect HL1 source surface to rasterize, got %+v", result.Tile.Polygons)
		}
	}
}

func TestBuildNavTileHL1SourceKeepsDirectPolygonsDespiteClearanceBlocker(t *testing.T) {
	voxels := []ImportedWorldVoxelDef{{X: 8, Y: 1, Z: 8, Value: 1}}
	chunk := navTestChunk(16, 0.25, voxels...)
	profile := navTestAgentProfile(0.4, 1.0, 0.5)
	profile.NavCellSize = 0.5
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Tags:     []string{"source:imported_world", "source:hl1"},
		Surfaces: []NavBuildSurfaceDef{
			{
				ID: "clean_hl1_floor",
				Vertices: []Vec3{
					{0.5, 0.25, 0.5},
					{1.5, 0.25, 0.5},
					{1.5, 0.25, 1.5},
					{0.5, 0.25, 1.5},
				},
				Area:      NavTraversalWalk,
				SourceTag: "hl1:model:0:face:4",
				Tags:      []string{"source:explicit_surface", "source:hl1"},
			},
			{
				ID: "blocked_hl1_floor",
				Vertices: []Vec3{
					{1.5, 0.25, 1.5},
					{3.5, 0.25, 1.5},
					{3.5, 0.25, 3.5},
					{1.5, 0.25, 3.5},
				},
				Area:      NavTraversalWalk,
				SourceTag: "hl1:model:0:face:5",
				Tags:      []string{"source:explicit_surface", "source:hl1"},
			},
		},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
		NeighborChunks:     navTestEmptyNeighborChunks(chunk, navBuildChunkReachForProfile(chunk.ChunkSize, chunk.VoxelResolution, profile)),
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTileHasPolygon(result.Tile, "surface:clean_hl1_floor") {
		t.Fatalf("expected clean HL1 surface to stay direct, got %+v", result.Tile.Polygons)
	}
	for _, polygon := range result.Tile.Polygons {
		if strings.HasPrefix(polygon.ID, "raster_region:") || strings.HasPrefix(polygon.ID, "raster_cell:") {
			t.Fatalf("did not expect HL1 source surface to rasterize from clearance blockers, got %+v", result.Tile.Polygons)
		}
	}
	if !navTileHasPolygon(result.Tile, "surface:blocked_hl1_floor") {
		t.Fatalf("expected clearance-blocked HL1 surface to stay direct until polygon clipping exists, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileHL1SourceNormalizesOverlappingRectPolygons(t *testing.T) {
	chunk := navTestChunk(16, 0.25)
	profile := navTestAgentProfile(0.4, 1.0, 0.5)
	profile.NavCellSize = 0.5
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Tags:     []string{"source:hl1"},
		Surfaces: []NavBuildSurfaceDef{
			{
				ID: "floor_a",
				Vertices: []Vec3{
					{0.5, 0.25, 0.5},
					{3.0, 0.25, 0.5},
					{3.0, 0.25, 2.0},
					{0.5, 0.25, 2.0},
				},
				Area:      NavTraversalWalk,
				SourceTag: "hl1:model:0:face:4",
				Tags:      []string{"source:explicit_surface", "source:hl1"},
			},
			{
				ID: "floor_b",
				Vertices: []Vec3{
					{1.5, 0.25, 1.0},
					{3.5, 0.25, 1.0},
					{3.5, 0.25, 3.0},
					{1.5, 0.25, 3.0},
				},
				Area:      NavTraversalWalk,
				SourceTag: "hl1:model:0:face:5",
				Tags:      []string{"source:explicit_surface", "source:hl1"},
			},
		},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) < 2 {
		t.Fatalf("expected normalized source polygon union, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s (%+v)", validation.Error(), validation.Issues)
	}
	if !navTilePointCovered(result.Tile, Vec3{2.0, 0.25, 1.5}) || !navTilePointCovered(result.Tile, Vec3{3.25, 0.25, 2.5}) {
		t.Fatalf("expected normalized union to keep both overlapping source surfaces, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileHL1SourceClearanceBlockerDoesNotRasterizeFloor(t *testing.T) {
	chunk := navTestChunk(16, 0.25)
	profile := navTestAgentProfile(0.4, 1.0, 0.5)
	profile.NavCellSize = 0.5
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Tags:     []string{"source:hl1"},
		Surfaces: []NavBuildSurfaceDef{
			{
				ID: "hl1_floor",
				Vertices: []Vec3{
					{0.5, 0.25, 0.5},
					{3.5, 0.25, 0.5},
					{3.5, 0.25, 3.5},
					{0.5, 0.25, 3.5},
				},
				Area:      NavTraversalWalk,
				SourceTag: "hl1:model:0:face:4",
				Tags:      []string{"source:explicit_surface", "source:hl1"},
			},
			{
				ID:   "rail_blocker",
				Kind: NavBuildSurfaceClearanceBlocker,
				Vertices: []Vec3{
					{2.0, 0.25, 0.5},
					{2.0, 1.25, 0.5},
					{2.0, 1.25, 3.5},
					{2.0, 0.25, 3.5},
				},
				SourceTag: "hl1:model:0:face:5",
				Tags:      []string{"source:explicit_surface", "source:hl1", "source_texture:{rail1"},
			},
		},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
		NeighborChunks:     navTestEmptyNeighborChunks(chunk, navBuildChunkReachForProfile(chunk.ChunkSize, chunk.VoxelResolution, profile)),
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if navTileHasPolygon(result.Tile, "surface:hl1_floor") {
		t.Fatalf("expected HL1 floor to be split around source blocker, got %+v", result.Tile.Polygons)
	}
	if navTileHasPolygon(result.Tile, "surface:rail_blocker") {
		t.Fatalf("did not expect source clearance blocker to become walkable nav, got %+v", result.Tile.Polygons)
	}
	if navTileHasCell(result.Tile, 8, 1, 8, chunk.VoxelResolution) {
		t.Fatalf("did not expect nav through expanded rail blocker clearance, got %+v", result.Tile.Polygons)
	}
	if !navTileHasCell(result.Tile, 4, 1, 8, chunk.VoxelResolution) || !navTileHasCell(result.Tile, 12, 1, 8, chunk.VoxelResolution) {
		t.Fatalf("expected clipped floor pieces on both sides of blocker, got %+v", result.Tile.Polygons)
	}
	for _, polygon := range result.Tile.Polygons {
		if strings.HasPrefix(polygon.ID, "raster_region:") || strings.HasPrefix(polygon.ID, "raster_cell:") {
			t.Fatalf("did not expect source blocker to rasterize HL1 floor, got %+v", result.Tile.Polygons)
		}
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileAddsGenericWalkableSurface(t *testing.T) {
	chunk := navTestChunk(8, 1.0)
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{{
			ID: "flat_quad",
			Vertices: []Vec3{
				{1, 1, 1},
				{3, 1, 1},
				{3, 1, 3},
				{1, 1, 3},
			},
		}},
	}
	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{
		BuildSource: source,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.WalkableCells) != 0 {
		t.Fatalf("expected no voxel-derived walkable cells, got %d", len(result.WalkableCells))
	}
	if !navTileHasPolygon(result.Tile, "surface:flat_quad") {
		t.Fatalf("expected generic surface polygon, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileDecomposesGenericConcaveSourceSurface(t *testing.T) {
	chunk := navTestChunk(6, 1.0)
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{{
			ID: "concave_l",
			Vertices: []Vec3{
				{1, 1, 1},
				{5, 1, 1},
				{5, 1, 3},
				{3, 1, 3},
				{3, 1, 5},
				{1, 1, 5},
			},
		}},
	}
	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{
		BuildSource: source,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) != 4 {
		t.Fatalf("expected concave source surface to decompose to four convex polygons, got %+v", result.Tile.Polygons)
	}
	if !navTilePolygonsAreConvexXZ(result.Tile) {
		t.Fatalf("expected decomposed source polygons to be convex, got %+v", result.Tile.Polygons)
	}
	if !navTileHasCell(result.Tile, 1, 1, 1, chunk.VoxelResolution) ||
		!navTileHasCell(result.Tile, 4, 1, 2, chunk.VoxelResolution) ||
		!navTileHasCell(result.Tile, 2, 1, 4, chunk.VoxelResolution) {
		t.Fatalf("expected decomposed source surface to cover all L-shaped floor cells, got %+v", result.Tile.Polygons)
	}
	if navTileHasCell(result.Tile, 4, 1, 4, chunk.VoxelResolution) {
		t.Fatalf("did not expect decomposed source surface to cover missing corner, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileClipsGenericSourceToTileYBounds(t *testing.T) {
	chunk := navTestChunk(8, 1.0)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.MaxSlopeDegrees = 89
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{{
			ID: "vertical_span_ramp",
			Vertices: []Vec3{
				{1, -1, 1},
				{4, -1, 1},
				{4, 9, 4},
				{1, 9, 4},
			},
			Area: NavTraversalWalk,
		}},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource: source,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) == 0 {
		t.Fatalf("expected Y-clipped source polygon")
	}
	for i, vertex := range result.Tile.Vertices {
		if vertex[1] < result.Tile.BoundsMin[1]-1e-4 || vertex[1] > result.Tile.BoundsMax[1]+1e-4 {
			t.Fatalf("vertex %d escaped tile Y bounds: vertex=%+v bounds=%+v..%+v", i, vertex, result.Tile.BoundsMin, result.Tile.BoundsMax)
		}
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileMergesAdjacentGenericSourceFlatRects(t *testing.T) {
	chunk := navTestChunk(8, 1.0)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{
			{
				ID: "left_field",
				Vertices: []Vec3{
					{1, 1, 1},
					{3, 1, 1},
					{3, 1, 3},
					{1, 1, 3},
				},
				Normal: Vec3{0, 1, 0},
				Area:   NavTraversalWalk,
			},
			{
				ID: "right_field",
				Vertices: []Vec3{
					{3, 1, 1},
					{5, 1, 1},
					{5, 1, 3},
					{3, 1, 3},
				},
				Normal: Vec3{0, 1, 0},
				Area:   NavTraversalWalk,
			},
		},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) != 1 {
		t.Fatalf("expected adjacent flat source rects to merge into one polygon, got %+v", result.Tile.Polygons)
	}
	polygon := result.Tile.Polygons[0]
	if polygon.ID != "merged_rect:1:1:1:5:3" {
		t.Fatalf("expected stable merged rect id, got %+v", polygon)
	}
	bounds, ok := navPolygonBoundsForTile(result.Tile, polygon)
	if !ok {
		t.Fatalf("expected merged polygon bounds")
	}
	if bounds.min != (Vec3{1, 1, 1}) || bounds.max != (Vec3{5, 1, 3}) {
		t.Fatalf("unexpected merged polygon bounds: %+v", bounds)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileKeepsDenseGenericSourceRectDirect(t *testing.T) {
	chunk := navTestChunk(80, 0.1)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = 0.5
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{{
			ID: "dense_field",
			Vertices: []Vec3{
				{1.1, 1, 1.2},
				{4.9, 1, 1.2},
				{4.9, 1, 3.8},
				{1.1, 1, 3.8},
			},
			Normal: Vec3{0, 1, 0},
			Area:   NavTraversalWalk,
		}},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) != 1 {
		t.Fatalf("expected source rect to remain one polygon, got %+v", result.Tile.Polygons)
	}
	polygon := result.Tile.Polygons[0]
	if polygon.ID != "surface:dense_field" {
		t.Fatalf("expected stable direct source id, got %+v", polygon)
	}
	bounds, ok := navPolygonBoundsForTile(result.Tile, polygon)
	if !ok {
		t.Fatalf("expected source polygon bounds")
	}
	if bounds.min != (Vec3{1.1, 1, 1.2}) || bounds.max != (Vec3{4.9, 1, 3.8}) {
		t.Fatalf("unexpected source polygon bounds: %+v", bounds)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileKeepsGenericSourceTriangleDirectInWorldSpace(t *testing.T) {
	chunk := navTestChunk(8, 1.0)
	chunk.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = 3.0
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{{
			ID: "partial_source",
			Vertices: []Vec3{
				{8.2, 1, 1.2},
				{8.8, 1, 1.2},
				{8.2, 1, 1.8},
			},
			Normal: Vec3{0, 1, 0},
			Area:   NavTraversalWalk,
		}},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) != 1 {
		t.Fatalf("expected one direct source polygon, got %+v", result.Tile.Polygons)
	}
	if result.Tile.Polygons[0].ID != "surface:partial_source" {
		t.Fatalf("expected direct source id, got %+v", result.Tile.Polygons[0])
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileKeepsIndexedTriangleDirect(t *testing.T) {
	chunk := navTestChunk(80, 0.1)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = 1.0
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{{
			ID: "indexed_tri",
			Vertices: []Vec3{
				{1, 1, 1},
				{4, 1, 1},
				{1, 1, 4},
			},
			Indices: []int{0, 1, 2},
			Normal:  Vec3{0, 1, 0},
			Area:    NavTraversalWalk,
		}},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) == 0 {
		t.Fatal("expected indexed triangle to emit nav polygons")
	}
	if !navTileHasPolygon(result.Tile, "surface:indexed_tri:tri:0") {
		t.Fatalf("expected direct indexed triangle polygon, got %+v", result.Tile.Polygons)
	}
	for _, polygon := range result.Tile.Polygons {
		for _, vertexIndex := range polygon.Vertices {
			if vertexIndex < 0 || vertexIndex >= len(result.Tile.Vertices) {
				t.Fatalf("polygon references invalid vertex %d", vertexIndex)
			}
			vertex := result.Tile.Vertices[vertexIndex]
			if vertex[0] < 1-1e-4 || vertex[2] < 1-1e-4 || vertex[0]+vertex[2] > 5+1e-4 {
				t.Fatalf("direct triangle vertex escaped source triangle: %+v polygon=%+v", vertex, polygon)
			}
		}
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileKeepsSlopedGenericSurfaceDirect(t *testing.T) {
	chunk := navTestChunk(80, 0.1)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = 1.0
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{{
			ID: "sloped_field",
			Vertices: []Vec3{
				{1, 1, 1},
				{4, 1, 1},
				{4, 2, 4},
				{1, 2, 4},
			},
			Area: NavTraversalWalk,
		}},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) != 1 {
		t.Fatalf("expected sloped source surface to remain one polygon, got %+v", result.Tile.Polygons)
	}
	if !navTileHasPolygon(result.Tile, "surface:sloped_field") {
		t.Fatalf("expected direct sloped source polygon, got %+v", result.Tile.Polygons)
	}
	if result.Tile.Polygons[0].Area != NavTraversalRamp {
		t.Fatalf("expected direct sloped source polygon to be classified as ramp, got %+v", result.Tile.Polygons[0])
	}
	minY := result.Tile.Vertices[0][1]
	maxY := result.Tile.Vertices[0][1]
	for _, vertex := range result.Tile.Vertices[1:] {
		minY = minNavFloat32(minY, vertex[1])
		maxY = maxNavFloat32(maxY, vertex[1])
	}
	if maxY-minY <= 0.5 {
		t.Fatalf("expected direct sloped polygon to preserve height variation, got minY=%f maxY=%f", minY, maxY)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileDirectGenericSourceClipsToTileYBounds(t *testing.T) {
	chunk := navTestChunk(80, 0.1)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = 1.0
	profile.MaxSlopeDegrees = 89
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{{
			ID: "raster_vertical_span_ramp",
			Vertices: []Vec3{
				{1, -1, 1},
				{4, -1, 1},
				{4, 9, 4},
				{1, 9, 4},
			},
			Area: NavTraversalWalk,
		}},
	}

	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) == 0 {
		t.Fatalf("expected Y-clipped source polygons")
	}
	if !navTileHasPolygon(result.Tile, "surface:raster_vertical_span_ramp") {
		t.Fatalf("expected direct source polygon, got %+v", result.Tile.Polygons)
	}
	for i, vertex := range result.Tile.Vertices {
		if vertex[1] < result.Tile.BoundsMin[1]-1e-4 || vertex[1] > result.Tile.BoundsMax[1]+1e-4 {
			t.Fatalf("vertex %d escaped tile Y bounds: vertex=%+v bounds=%+v..%+v", i, vertex, result.Tile.BoundsMin, result.Tile.BoundsMax)
		}
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildNavTileConnectsGenericSurfaceStairsWithinStepHeight(t *testing.T) {
	chunk := navTestChunk(8, 1.0)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{
			{
				ID: "lower_tread",
				Vertices: []Vec3{
					{1, 1, 1},
					{3, 1, 1},
					{3, 1, 3},
					{1, 1, 3},
				},
				Normal: Vec3{0, 1, 0},
			},
			{
				ID: "upper_tread",
				Vertices: []Vec3{
					{3, 1.4, 1},
					{5, 1.4, 1},
					{5, 1.4, 3},
					{3, 1.4, 3},
				},
				Normal: Vec3{0, 1, 0},
			},
		},
	}
	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTilePolygonsAreNeighbors(result.Tile, "surface:lower_tread", "surface:upper_tread") ||
		!navTilePolygonsAreNeighbors(result.Tile, "surface:upper_tread", "surface:lower_tread") {
		t.Fatalf("expected explicit stair treads to connect within step height, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileConnectsGenericSurfaceDiagonalPartialPortalWithinStepHeight(t *testing.T) {
	chunk := navTestChunk(10, 1.0)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{
			{
				ID: "lower_diag",
				Vertices: []Vec3{
					{1, 1, 1},
					{3, 1, 1},
					{7, 1, 5},
					{5, 1, 5},
				},
				Normal: Vec3{0, 1, 0},
			},
			{
				ID: "upper_diag",
				Vertices: []Vec3{
					{4, 1.4, 2},
					{6, 1.4, 2},
					{8, 1.4, 4},
					{6, 1.4, 4},
				},
				Normal: Vec3{0, 1, 0},
			},
		},
	}
	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTilePolygonsAreNeighbors(result.Tile, "surface:lower_diag", "surface:upper_diag") ||
		!navTilePolygonsAreNeighbors(result.Tile, "surface:upper_diag", "surface:lower_diag") {
		t.Fatalf("expected diagonal partial portal to connect within step height, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileRejectsGenericSurfaceStairAboveStepHeight(t *testing.T) {
	chunk := navTestChunk(8, 1.0)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{
			{
				ID: "lower_tread",
				Vertices: []Vec3{
					{1, 1, 1},
					{3, 1, 1},
					{3, 1, 3},
					{1, 1, 3},
				},
				Normal: Vec3{0, 1, 0},
			},
			{
				ID: "upper_tread",
				Vertices: []Vec3{
					{3, 1.75, 1},
					{5, 1.75, 1},
					{5, 1.75, 3},
					{3, 1.75, 3},
				},
				Normal: Vec3{0, 1, 0},
			},
		},
	}
	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if navTilePolygonsAreNeighbors(result.Tile, "surface:lower_tread", "surface:upper_tread") ||
		navTilePolygonsAreNeighbors(result.Tile, "surface:upper_tread", "surface:lower_tread") {
		t.Fatalf("did not expect explicit treads above step height to connect, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileRejectsGenericSurfaceAboveSlopeLimit(t *testing.T) {
	chunk := navTestChunk(8, 1.0)
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.MaxSlopeDegrees = 30
	source := &NavBuildSourceDef{
		SourceID: "test-source",
		Surfaces: []NavBuildSurfaceDef{{
			ID: "steep_tri",
			Vertices: []Vec3{
				{1, 0, 1},
				{1, 2, 1},
				{1, 0, 3},
			},
		}},
	}
	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource: source,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if navTileHasPolygon(result.Tile, "surface:steep_tri") {
		t.Fatalf("did not expect steep generic surface polygon, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileKeepsMultipleWalkableSpansInColumn(t *testing.T) {
	voxels := []ImportedWorldVoxelDef{
		{X: 2, Y: 0, Z: 2, Value: 1},
		{X: 2, Y: 4, Z: 2, Value: 1},
	}
	chunk := navTestChunk(8, 0.5, voxels...)
	result, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTileHasCell(result.Tile, 2, 1, 2, chunk.VoxelResolution) {
		t.Fatalf("expected lower floor span to be walkable, got %+v", result.Tile.Polygons)
	}
	if !navTileHasCell(result.Tile, 2, 5, 2, chunk.VoxelResolution) {
		t.Fatalf("expected upper floor span to be walkable, got %+v", result.Tile.Polygons)
	}
	if len(result.Tile.Polygons) != 2 {
		t.Fatalf("expected two vertical walkable spans, got %+v", result.Tile.Polygons)
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
	if navTileCellsAreConnected(result.Tile, 2, 1, 1, 2, 3, 2, chunk.VoxelResolution) {
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
	if navTileCellsAreConnected(lowStep.Tile, 2, 1, 1, 2, 2, 2, chunk.VoxelResolution) {
		t.Fatal("did not expect one-voxel lip to connect below step height")
	}
	exactStep, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.2, 1.0, 0.5), NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk exact step failed: %v", err)
	}
	if !navTileCellsAreConnected(exactStep.Tile, 2, 1, 1, 2, 2, 2, chunk.VoxelResolution) {
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
	if !navTileHasCell(small.Tile, 3, 1, 2, chunk.VoxelResolution) {
		t.Fatalf("expected small agent to fit through one-cell gap, got %+v", small.Tile.Polygons)
	}
	large, err := BuildNavTileFromImportedWorldChunk(chunk, navTestAgentProfile(0.6, 1.0, 1.0), NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk large radius failed: %v", err)
	}
	if navTileHasCell(large.Tile, 3, 1, 2, chunk.VoxelResolution) {
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
	if navTileHasCell(withoutNeighbor.Tile, 3, 1, 1, center.VoxelResolution) {
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
	if !navTileHasCell(withNeighbor.Tile, 3, 1, 1, center.VoxelResolution) {
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
	if navTileHasCell(result.Tile, 3, 1, 1, center.VoxelResolution) {
		t.Fatalf("did not expect boundary polygon when neighbor blocks clearance, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileUsesMultiChunkNeighborClearanceAtChunkBoundary(t *testing.T) {
	center := navTestChunk(2, 1.0, ImportedWorldVoxelDef{X: 1, Y: 0, Z: 1, Value: 1})
	profile := navTestAgentProfile(3.2, 1.0, 1.0)
	neighbors := navTestEmptyNeighborChunks(center, navBuildChunkReachForProfile(center.ChunkSize, center.VoxelResolution, profile))

	result, err := BuildNavTileFromImportedWorldChunk(center, profile, NavTileBuildOptions{
		NeighborChunks: neighbors,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTileHasCell(result.Tile, 1, 1, 1, center.VoxelResolution) {
		t.Fatalf("expected large-agent boundary cell with multi-chunk empty context, got %+v", result.Tile.Polygons)
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

func navTestEmptyNeighborChunks(center *ImportedWorldChunkDef, reach navBuildChunkReach) map[TerrainChunkCoordDef]*ImportedWorldChunkDef {
	out := make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef)
	if center == nil {
		return out
	}
	for dx := -reach.Horizontal; dx <= reach.Horizontal; dx++ {
		for dy := -reach.Vertical; dy <= reach.Vertical; dy++ {
			for dz := -reach.Horizontal; dz <= reach.Horizontal; dz++ {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				coord := TerrainChunkCoordDef{X: center.Coord.X + dx, Y: center.Coord.Y + dy, Z: center.Coord.Z + dz}
				out[coord] = &ImportedWorldChunkDef{
					WorldID:         center.WorldID,
					SchemaVersion:   CurrentImportedWorldChunkSchemaVersion,
					Coord:           coord,
					ChunkSize:       center.ChunkSize,
					VoxelResolution: center.VoxelResolution,
					PayloadKind:     center.PayloadKind,
				}
			}
		}
	}
	return out
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

func removeNavTestFloorVoxels(voxels []ImportedWorldVoxelDef, minX, maxX, minZ, maxZ, y int) []ImportedWorldVoxelDef {
	out := make([]ImportedWorldVoxelDef, 0, len(voxels))
	for _, voxel := range voxels {
		if voxel.Y == y && voxel.X >= minX && voxel.X <= maxX && voxel.Z >= minZ && voxel.Z <= maxZ {
			continue
		}
		out = append(out, voxel)
	}
	return out
}

type navTestCellCoord struct {
	X int
	Y int
	Z int
}

func navTestCellCoordsForVoxels(voxels []ImportedWorldVoxelDef, floorYOffset int) []navTestCellCoord {
	out := make([]navTestCellCoord, 0, len(voxels))
	for _, voxel := range voxels {
		if voxel.Value == 0 {
			continue
		}
		out = append(out, navTestCellCoord{X: voxel.X, Y: voxel.Y + floorYOffset, Z: voxel.Z})
	}
	return out
}

func navAssertTileCoversCells(t *testing.T, tile *NavTileDef, voxelResolution float32, cells ...navTestCellCoord) {
	t.Helper()
	for _, cell := range cells {
		if !navTileHasCell(tile, cell.X, cell.Y, cell.Z, voxelResolution) {
			t.Fatalf("expected nav tile to cover cell %+v, polygons=%+v", cell, tile.Polygons)
		}
	}
}

func navAssertTileExcludesCells(t *testing.T, tile *NavTileDef, voxelResolution float32, cells ...navTestCellCoord) {
	t.Helper()
	for _, cell := range cells {
		if navTileHasCell(tile, cell.X, cell.Y, cell.Z, voxelResolution) {
			t.Fatalf("did not expect nav tile to cover cell %+v, polygons=%+v", cell, tile.Polygons)
		}
	}
}

func navAssertTileCellsHaveSinglePolygon(t *testing.T, tile *NavTileDef, voxelResolution float32, cells ...navTestCellCoord) {
	t.Helper()
	for _, cell := range cells {
		count := navTilePolygonCountAtCell(tile, cell.X, cell.Y, cell.Z, voxelResolution)
		if count != 1 {
			t.Fatalf("expected exactly one polygon to cover cell %+v, got %d polygons=%+v", cell, count, tile.Polygons)
		}
	}
}

func navTestBorderSpansForEdge(tile *NavTileDef, edge string) []NavBorderSpanDef {
	if tile == nil {
		return nil
	}
	out := make([]NavBorderSpanDef, 0)
	for _, span := range tile.BorderSpans {
		if span.Edge == edge {
			out = append(out, span)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if navAlmostEqual(out[i].Min, out[j].Min, 1e-4) {
			return out[i].Max < out[j].Max
		}
		return out[i].Min < out[j].Min
	})
	return out
}

func navTestDropLinksToCoord(tile *NavTileDef, coord TerrainChunkCoordDef) []NavOffMeshLinkDef {
	if tile == nil {
		return nil
	}
	out := make([]NavOffMeshLinkDef, 0)
	for _, link := range tile.OffMeshLinks {
		if link.Kind == NavTraversalDrop && link.ToTileCoord == coord {
			out = append(out, link)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
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

func navTileHasPolygonArea(tile *NavTileDef, area string) bool {
	if tile == nil {
		return false
	}
	for _, polygon := range tile.Polygons {
		if polygon.Area == area {
			return true
		}
	}
	return false
}

func navTileHasNonFlatPolygon(tile *NavTileDef) bool {
	if tile == nil {
		return false
	}
	for _, polygon := range tile.Polygons {
		if len(polygon.Vertices) < 2 {
			continue
		}
		firstYSet := false
		firstY := float32(0)
		for _, index := range polygon.Vertices {
			if index < 0 || index >= len(tile.Vertices) {
				continue
			}
			y := tile.Vertices[index][1]
			if !firstYSet {
				firstY = y
				firstYSet = true
				continue
			}
			if absNavFloat32(y-firstY) > 1e-4 {
				return true
			}
		}
	}
	return false
}

func navTilePolygonsAreConvexXZ(tile *NavTileDef) bool {
	if tile == nil {
		return false
	}
	for _, polygon := range tile.Polygons {
		vertices := make([]Vec3, 0, len(polygon.Vertices))
		for _, index := range polygon.Vertices {
			if index < 0 || index >= len(tile.Vertices) {
				return false
			}
			vertices = append(vertices, tile.Vertices[index])
		}
		if !navPolygonVerticesConvexXZ(vertices) {
			return false
		}
	}
	return true
}

func navTileBoundaryVertexHeights(tile *NavTileDef, normalAxis int, coord float32, spanAxis int, minSpan float32, maxSpan float32) []float32 {
	if tile == nil {
		return nil
	}
	heights := make([]float32, 0)
	for _, vertex := range tile.Vertices {
		if absNavFloat32(vertex[normalAxis]-coord) > 1e-4 {
			continue
		}
		if vertex[spanAxis] < minSpan-1e-4 || vertex[spanAxis] > maxSpan+1e-4 {
			continue
		}
		heights = append(heights, vertex[1])
	}
	return heights
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

func navTileHasCell(tile *NavTileDef, x, y, z int, voxelResolution float32) bool {
	polygon, ok := navTilePolygonAtCell(tile, x, y, z, voxelResolution)
	return ok && polygon.ID != ""
}

func navTilePointCovered(tile *NavTileDef, point Vec3) bool {
	if tile == nil {
		return false
	}
	const epsilon = float32(1e-4)
	for _, polygon := range tile.Polygons {
		if !navPolygonContainsXZ(tile, polygon, point) {
			continue
		}
		height, ok := navPolygonHeightAtXZ(tile, polygon, point)
		if ok && absNavFloat32(height-point[1]) <= epsilon {
			return true
		}
	}
	return false
}

func navTileCellsAreConnected(tile *NavTileDef, ax, ay, az, bx, by, bz int, voxelResolution float32) bool {
	a, ok := navTilePolygonAtCell(tile, ax, ay, az, voxelResolution)
	if !ok {
		return false
	}
	b, ok := navTilePolygonAtCell(tile, bx, by, bz, voxelResolution)
	if !ok {
		return false
	}
	if a.ID == b.ID {
		return true
	}
	return navTilePolygonsAreNeighbors(tile, a.ID, b.ID)
}

func navTileCellsHavePath(tile *NavTileDef, ax, ay, az, bx, by, bz int, voxelResolution float32) bool {
	a, ok := navTilePolygonAtCell(tile, ax, ay, az, voxelResolution)
	if !ok {
		return false
	}
	b, ok := navTilePolygonAtCell(tile, bx, by, bz, voxelResolution)
	if !ok {
		return false
	}
	query, err := NewNavTileQuery(tile)
	if err != nil {
		return false
	}
	path, err := query.FindPathBetweenPolygons(a.ID, b.ID)
	return err == nil && path.Found
}

func navTilePolygonAtCell(tile *NavTileDef, x, y, z int, voxelResolution float32) (NavPolygonDef, bool) {
	if tile == nil {
		return NavPolygonDef{}, false
	}
	point := Vec3{
		(float32(x) + 0.5) * voxelResolution,
		float32(y) * voxelResolution,
		(float32(z) + 0.5) * voxelResolution,
	}
	const epsilon = float32(1e-4)
	for _, polygon := range tile.Polygons {
		if !navPolygonContainsXZ(tile, polygon, point) {
			continue
		}
		height, ok := navPolygonHeightAtXZ(tile, polygon, point)
		if !ok {
			height = navPolygonCenter(tile, polygon)[1]
		}
		if absNavFloat32(height-point[1]) <= epsilon {
			return polygon, true
		}
	}
	return NavPolygonDef{}, false
}

func navTilePolygonCountAtCell(tile *NavTileDef, x, y, z int, voxelResolution float32) int {
	if tile == nil {
		return 0
	}
	point := Vec3{
		(float32(x) + 0.5) * voxelResolution,
		float32(y) * voxelResolution,
		(float32(z) + 0.5) * voxelResolution,
	}
	const epsilon = float32(1e-4)
	count := 0
	for _, polygon := range tile.Polygons {
		if !navPolygonContainsXZ(tile, polygon, point) {
			continue
		}
		height, ok := navPolygonHeightAtXZ(tile, polygon, point)
		if !ok {
			height = navPolygonCenter(tile, polygon)[1]
		}
		if absNavFloat32(height-point[1]) <= epsilon {
			count++
		}
	}
	return count
}
