package content

import (
	"testing"

	"github.com/gekko3d/gekko/content/recastnav"
)

func TestBuildRecastNavTileFromImportedWorldChunkFlatFloor(t *testing.T) {
	chunk := navTestChunk(16, 0.25, navTestFloorVoxels(2, 13, 2, 13, 0)...)
	profile := navTestAgentProfile(0.1, 1.0, 0.3)
	profile.NavCellSize = 0.1
	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		NavID:          "nav-recast-flat",
		BuilderVersion: NavBuilderVersionVoxelRecastV1,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if result.Tile.BuilderVersion != NavBuilderVersionVoxelRecastV1 {
		t.Fatalf("expected Recast builder version, got %q", result.Tile.BuilderVersion)
	}
	if len(result.Tile.Polygons) == 0 || len(result.Tile.Polygons) > 12 {
		t.Fatalf("expected Recast to emit a small flat-floor mesh, got %d polygons", len(result.Tile.Polygons))
	}
	if !navTileHasPolygonArea(result.Tile, NavTraversalWalk) {
		t.Fatalf("expected flat Recast floor to be walkable, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestBuildRecastNavTileFromImportedWorldChunkWalkableStepsStayConnected(t *testing.T) {
	voxels := make([]ImportedWorldVoxelDef, 0)
	for x := 2; x <= 13; x++ {
		height := (x - 1) / 2
		for z := 2; z <= 13; z++ {
			for y := 0; y <= height; y++ {
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}
	chunk := navTestChunk(16, 0.2, voxels...)
	profile := navTestAgentProfile(0.05, 0.8, 0.25)
	profile.NavCellSize = 0.1
	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		NavID:          "nav-recast-steps",
		BuilderVersion: NavBuilderVersionVoxelRecastV1,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) < 2 {
		t.Fatalf("expected stepped Recast surface to emit polygons, got %+v", result.Tile.Polygons)
	}
	neighborEdges := 0
	for _, polygon := range result.Tile.Polygons {
		neighborEdges += len(polygon.Neighbors)
	}
	if neighborEdges == 0 {
		t.Fatalf("expected walkable Recast steps to be connected, got %+v", result.Tile.Polygons)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestRecastNavTileConfigUsesTileBorderSize(t *testing.T) {
	chunk := navTestChunk(16, 0.25, navTestFloorVoxels(2, 13, 2, 13, 0)...)
	profile := navTestAgentProfile(0.2, 1.0, 0.3)
	profile.NavCellSize = 0.1
	origin := importedWorldChunkWorldOrigin(chunk)
	worldSize := float32(chunk.ChunkSize) * chunk.VoxelResolution
	boundsMin, boundsMax := navRecastExpandedBuildBounds(origin, worldSize, profile)
	tile := &NavTileDef{
		BoundsMin: [3]float32{origin[0], origin[1], origin[2]},
		BoundsMax: [3]float32{origin[0] + worldSize, origin[1] + worldSize, origin[2] + worldSize},
	}
	cfg := navRecastConfigForTile(tile, chunk, profile, boundsMin, boundsMax)
	if cfg.BorderSize <= cfg.WalkableRadius {
		t.Fatalf("expected Recast border to include padding beyond walkable radius, got border=%d radius=%d", cfg.BorderSize, cfg.WalkableRadius)
	}
	expectedPad := float32(cfg.BorderSize) * cfg.CellSize
	if absNavFloat32(boundsMin[0]-(tile.BoundsMin[0]-expectedPad)) > 1e-4 ||
		absNavFloat32(boundsMax[0]-(tile.BoundsMax[0]+expectedPad)) > 1e-4 ||
		absNavFloat32(boundsMin[2]-(tile.BoundsMin[2]-expectedPad)) > 1e-4 ||
		absNavFloat32(boundsMax[2]-(tile.BoundsMax[2]+expectedPad)) > 1e-4 {
		t.Fatalf("expected expanded bounds to match Recast border pad %.3f, got min=%v max=%v tile=%v..%v", expectedPad, boundsMin, boundsMax, tile.BoundsMin, tile.BoundsMax)
	}
}

func TestAppendRecastMeshToNavTilePreservesRecastNeighbors(t *testing.T) {
	tile := &NavTileDef{
		NavID:          "nav-recast-neighbor",
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          TerrainChunkCoordDef{},
		AgentProfileID: "test-agent",
		BuilderVersion: NavBuilderVersionVoxelRecastV1,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{4, 4, 4},
	}
	mesh := recastnav.Mesh{
		Vertices: [][3]float32{
			{0.2, 1, 0.2},
			{1.0, 1, 0.2},
			{0.2, 1, 1.0},
			{1.2, 1, 0.2},
			{2.0, 1, 0.2},
			{1.2, 1, 1.0},
		},
		Polys:     [][]int{{0, 1, 2}, {3, 4, 5}},
		Neighbors: [][]int{{1}, {0}},
	}
	appendRecastMeshToNavTile(tile, mesh, navTestAgentProfile(0.2, 1.0, 0.3))
	if !navTilePolygonsAreNeighbors(tile, "recast:0", "recast:1") {
		t.Fatalf("expected converted tile to preserve Recast adjacency, got %+v", tile.Polygons)
	}
}

func TestBuildNavRecastDebugIncludesDetailAndContours(t *testing.T) {
	chunk := navTestChunk(16, 0.25, navTestFloorVoxels(2, 13, 2, 13, 0)...)
	profile := navTestAgentProfile(0.1, 1.0, 0.3)
	profile.NavCellSize = 0.1
	debug, err := BuildNavRecastDebug(chunk, profile, NavRecastDebugOptions{})
	if err != nil {
		t.Fatalf("BuildNavRecastDebug failed: %v", err)
	}
	if debug.Summary.InputTriangles == 0 || debug.Summary.PolyMeshPolygons == 0 || debug.Summary.ConvertedPolygons == 0 {
		t.Fatalf("expected populated Recast debug summary, got %+v", debug.Summary)
	}
	if debug.Summary.DetailMeshTriangles == 0 || len(debug.DetailMesh.Triangles) == 0 {
		t.Fatalf("expected detail mesh triangles in Recast debug output, got summary=%+v detail=%+v", debug.Summary, debug.DetailMesh)
	}
	if debug.Summary.Contours == 0 || len(debug.Contours) == 0 {
		t.Fatalf("expected contours in Recast debug output, got summary=%+v", debug.Summary)
	}
	if debug.ConvertedTile == nil || len(debug.ConvertedTile.Polygons) == 0 {
		t.Fatalf("expected converted nav tile in Recast debug output")
	}
}

func TestBuildNavRecastDebugCanIncludeInputMesh(t *testing.T) {
	chunk := navTestChunk(8, 0.5, navTestFloorVoxels(1, 6, 1, 6, 0)...)
	profile := navTestAgentProfile(0.1, 1.0, 0.3)
	profile.NavCellSize = 0.1
	debug, err := BuildNavRecastDebug(chunk, profile, NavRecastDebugOptions{IncludeInputMesh: true})
	if err != nil {
		t.Fatalf("BuildNavRecastDebug failed: %v", err)
	}
	if debug.InputMesh == nil || len(debug.InputMesh.Vertices) == 0 || len(debug.InputMesh.Triangles) == 0 {
		t.Fatalf("expected full input mesh in debug output, got %+v", debug.InputMesh)
	}
}

func TestBuildRecastNavTileClearanceBlockerDoesNotBecomeWalkable(t *testing.T) {
	chunk := navTestChunk(8, 0.5)
	profile := navTestAgentProfile(0.1, 1.0, 0.3)
	profile.NavCellSize = 0.1
	source := &NavBuildSourceDef{
		SchemaVersion: CurrentNavBuildSourceSchemaVersion,
		Kind:          NavBuildSourceKindGeneric,
		Surfaces: []NavBuildSurfaceDef{{
			ID:   "flat_blocker",
			Kind: NavBuildSurfaceClearanceBlocker,
			Vertices: []Vec3{
				{1, 1, 1},
				{3, 1, 1},
				{3, 1, 3},
				{1, 1, 3},
			},
		}},
	}
	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		NavID:          "nav-recast-blocker",
		BuilderVersion: NavBuilderVersionVoxelRecastV1,
		BuildSource:    source,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) != 0 {
		t.Fatalf("expected flat clearance blocker to remain non-walkable, got polygons=%+v", result.Tile.Polygons)
	}
}

func TestBuildRecastNavTileIgnoresSourceWalkablesWhenVoxelBacked(t *testing.T) {
	chunk := navTestChunk(8, 0.5, navTestFloorVoxels(1, 6, 1, 6, 0)...)
	profile := navTestAgentProfile(0.1, 1.0, 0.3)
	profile.NavCellSize = 0.1
	source := &NavBuildSourceDef{
		SchemaVersion: CurrentNavBuildSourceSchemaVersion,
		Kind:          NavBuildSourceKindGeneric,
		Surfaces: []NavBuildSurfaceDef{{
			ID:        "floating_source_floor",
			Kind:      NavBuildSurfaceWalkable,
			SourceTag: "imported_world:0:0:0",
			Vertices: []Vec3{
				{1, 3, 1},
				{3, 3, 1},
				{3, 3, 3},
				{1, 3, 3},
			},
		}},
	}
	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		NavID:          "nav-recast-ignore-source-walk",
		BuilderVersion: NavBuilderVersionVoxelRecastV1,
		BuildSource:    source,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if len(result.Tile.Polygons) == 0 {
		t.Fatalf("expected voxel floor polygons")
	}
	for _, vertex := range result.Tile.Vertices {
		if vertex[1] > 2 {
			t.Fatalf("expected voxel-backed Recast tile to ignore floating source walkable, got vertex %v", vertex)
		}
	}
}

func TestAppendRecastTileBorderSpansUsesActualBoundaryEdges(t *testing.T) {
	tile := &NavTileDef{
		NavID:          "nav-recast-span",
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          TerrainChunkCoordDef{},
		AgentProfileID: "test-agent",
		BuilderVersion: NavBuilderVersionVoxelRecastV1,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{10, 10, 10},
		Vertices: []Vec3{
			{10, 1, 2},
			{10, 1, 4},
			{5, 1, 8},
		},
		Polygons: []NavPolygonDef{{
			ID:       "recast:edge",
			Vertices: []int{0, 1, 2},
			Area:     NavTraversalWalk,
		}},
	}
	appendRecastTileBorderSpans(tile, navTestAgentProfile(0.2, 1.0, 0.5))
	if len(tile.BorderSpans) != 1 {
		t.Fatalf("expected one exact border span, got %+v", tile.BorderSpans)
	}
	span := tile.BorderSpans[0]
	if span.Edge != NavBorderEdgeMaxX || span.Min != 2 || span.Max != 4 {
		t.Fatalf("expected exact x_max span 2..4, got %+v", span)
	}
}

func TestAppendRecastTileBorderSpansUsesNearBoundaryEdges(t *testing.T) {
	tile := &NavTileDef{
		NavID:          "nav-recast-near-span",
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          TerrainChunkCoordDef{},
		AgentProfileID: "test-agent",
		BuilderVersion: NavBuilderVersionVoxelRecastV1,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{8, 8, 8},
		Vertices: []Vec3{
			{7.9, 1, 2},
			{7.9, 1, 4},
			{5, 1, 6},
		},
		Polygons: []NavPolygonDef{{
			ID:       "recast:near-edge",
			Vertices: []int{0, 1, 2},
			Area:     NavTraversalWalk,
		}},
	}
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = 0.3
	appendRecastTileBorderSpans(tile, profile)
	if len(tile.BorderSpans) != 1 {
		t.Fatalf("expected one near-boundary span, got %+v", tile.BorderSpans)
	}
	span := tile.BorderSpans[0]
	if span.Edge != NavBorderEdgeMaxX || span.Min != 2 || span.Max != 4 {
		t.Fatalf("expected near x_max span 2..4, got %+v", span)
	}
}
