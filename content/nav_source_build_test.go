package content

import "testing"

func TestBuildNavBuildSourceFromImportedWorldCreatesMergedWalkableSurfaces(t *testing.T) {
	chunk := navTestChunk(8, 1.0, navTestFloorVoxels(1, 4, 1, 4, 0)...)
	world := &ImportedWorldDef{
		WorldID:         "world-source",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       chunk.ChunkSize,
		VoxelResolution: chunk.VoxelResolution,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              chunk.Coord,
			ChunkPath:          "chunks/world-source_0_0_0.gkchunk",
			NonEmptyVoxelCount: chunk.NonEmptyVoxelCount,
		}},
	}

	source, err := BuildNavBuildSourceFromImportedWorld(world, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		chunk.Coord: chunk,
	}, NavBuildSourceBuildOptions{
		AgentProfile: navTestAgentProfile(0.2, 1.0, 0.5),
	})
	if err != nil {
		t.Fatalf("BuildNavBuildSourceFromImportedWorld failed: %v", err)
	}
	if source.SourceWorldID != world.WorldID || source.SourceHash == "" {
		t.Fatalf("unexpected source metadata: %+v", source)
	}
	if len(source.Surfaces) != 1 {
		t.Fatalf("expected one merged walkable source surface, got %+v", source.Surfaces)
	}
	surface := source.Surfaces[0]
	if surface.Kind != NavBuildSurfaceWalkable || surface.Area != NavTraversalWalk || len(surface.Vertices) != 4 {
		t.Fatalf("unexpected source surface: %+v", surface)
	}
	if source.BoundsMin != (Vec3{1, 1, 1}) || source.BoundsMax != (Vec3{5, 1, 5}) {
		t.Fatalf("unexpected source bounds min=%+v max=%+v", source.BoundsMin, source.BoundsMax)
	}
}

func TestBuildNavBuildSourceFromImportedWorldSynthesizesEmptyNeighborContext(t *testing.T) {
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	chunk := navTestChunk(4, 1.0, ImportedWorldVoxelDef{X: 3, Y: 0, Z: 1, Value: 1})
	chunk.Coord = coord
	world := &ImportedWorldDef{
		WorldID:         "world-source-sparse",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       chunk.ChunkSize,
		VoxelResolution: chunk.VoxelResolution,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              coord,
			ChunkPath:          "chunks/world-source-sparse_0_0_0.gkchunk",
			NonEmptyVoxelCount: chunk.NonEmptyVoxelCount,
		}},
	}
	profile := navTestAgentProfile(0.6, 1.0, 1.0)

	source, err := BuildNavBuildSourceFromImportedWorld(world, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		coord: chunk,
	}, NavBuildSourceBuildOptions{AgentProfile: profile})
	if err != nil {
		t.Fatalf("BuildNavBuildSourceFromImportedWorld failed: %v", err)
	}
	if len(source.Surfaces) != 1 {
		t.Fatalf("expected boundary source surface with synthesized empty neighbor, got %+v", source.Surfaces)
	}

	emptyChunk := navTestChunk(4, 1.0)
	result, err := BuildNavTileFromImportedWorldChunk(emptyChunk, profile, NavTileBuildOptions{
		BuildSource: source,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTileHasCell(result.Tile, 3, 1, 1, chunk.VoxelResolution) {
		t.Fatalf("expected source boundary surface to cover sparse edge cell, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavBuildSourceFromImportedWorldDoesNotMutateChunkMap(t *testing.T) {
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	chunk := navTestChunk(4, 1.0, ImportedWorldVoxelDef{X: 3, Y: 0, Z: 1, Value: 1})
	chunk.Coord = coord
	world := &ImportedWorldDef{
		WorldID:         "world-source-immutable",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       chunk.ChunkSize,
		VoxelResolution: chunk.VoxelResolution,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              coord,
			ChunkPath:          "chunks/world-source-immutable_0_0_0.gkchunk",
			NonEmptyVoxelCount: chunk.NonEmptyVoxelCount,
		}},
	}
	chunks := map[TerrainChunkCoordDef]*ImportedWorldChunkDef{coord: chunk}

	if _, err := BuildNavBuildSourceFromImportedWorld(world, chunks, NavBuildSourceBuildOptions{AgentProfile: navTestAgentProfile(0.6, 1.0, 1.0)}); err != nil {
		t.Fatalf("BuildNavBuildSourceFromImportedWorld failed: %v", err)
	}
	if len(chunks) != 1 || chunks[coord] != chunk {
		t.Fatalf("expected caller chunk map to remain unchanged, got %+v", chunks)
	}
}

func TestBuildNavBuildSourceFromImportedWorldUsesProfileNavCellSize(t *testing.T) {
	voxels := navTestFloorVoxels(10, 29, 10, 29, 0)
	voxels = removeNavTestFloorVoxels(voxels, 15, 19, 15, 19, 0)
	voxels = append(voxels, navTestFloorVoxels(15, 19, 15, 19, 1)...)
	chunk := navTestChunk(40, 0.1, voxels...)
	world := &ImportedWorldDef{
		WorldID:         "world-source-coarse",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       chunk.ChunkSize,
		VoxelResolution: chunk.VoxelResolution,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              chunk.Coord,
			ChunkPath:          "chunks/world-source-coarse_0_0_0.gkchunk",
			NonEmptyVoxelCount: chunk.NonEmptyVoxelCount,
		}},
	}
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	profile.NavCellSize = 0.5

	source, err := BuildNavBuildSourceFromImportedWorld(world, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		chunk.Coord: chunk,
	}, NavBuildSourceBuildOptions{AgentProfile: profile})
	if err != nil {
		t.Fatalf("BuildNavBuildSourceFromImportedWorld failed: %v", err)
	}
	if len(source.Surfaces) != 1 {
		t.Fatalf("expected one coarse source surface, got %+v", source.Surfaces)
	}
	if source.BoundsMin != (Vec3{1, 0.1, 1}) || source.BoundsMax != (Vec3{3, 0.1, 3}) {
		t.Fatalf("expected source bounds to match coarse tile raster, got min=%+v max=%+v", source.BoundsMin, source.BoundsMax)
	}
}

func TestBuildNavBuildSourceFromImportedWorldPreservesContourSurfaces(t *testing.T) {
	voxels := navTestFloorVoxels(1, 4, 1, 4, 0)
	voxels = removeNavTestFloorVoxels(voxels, 3, 4, 3, 4, 0)
	chunk := navTestChunk(6, 1.0, voxels...)
	world := &ImportedWorldDef{
		WorldID:         "world-contour-source",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       chunk.ChunkSize,
		VoxelResolution: chunk.VoxelResolution,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              chunk.Coord,
			ChunkPath:          "chunks/world-contour-source_0_0_0.gkchunk",
			NonEmptyVoxelCount: len(chunk.Voxels),
		}},
	}
	profile := navTestAgentProfile(0.2, 1.0, 0.5)

	source, err := BuildNavBuildSourceFromImportedWorld(world, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		chunk.Coord: chunk,
	}, NavBuildSourceBuildOptions{AgentProfile: profile})
	if err != nil {
		t.Fatalf("BuildNavBuildSourceFromImportedWorld failed: %v", err)
	}
	if len(source.Surfaces) != 1 {
		t.Fatalf("expected one contour source surface, got %+v", source.Surfaces)
	}
	if len(source.Surfaces[0].Vertices) != 6 {
		t.Fatalf("expected L-shaped source contour with 6 vertices, got %+v", source.Surfaces[0].Vertices)
	}

	emptyChunk := navTestChunk(6, 1.0)
	result, err := BuildNavTileFromImportedWorldChunk(emptyChunk, profile, NavTileBuildOptions{
		BuildSource: source,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTileHasCell(result.Tile, 1, 1, 1, chunk.VoxelResolution) ||
		!navTileHasCell(result.Tile, 4, 1, 2, chunk.VoxelResolution) ||
		!navTileHasCell(result.Tile, 2, 1, 4, chunk.VoxelResolution) {
		t.Fatalf("expected source contour polygon to cover L-shaped floor cells, got %+v", result.Tile.Polygons)
	}
	if navTileHasCell(result.Tile, 4, 1, 4, chunk.VoxelResolution) {
		t.Fatalf("did not expect source contour polygon to cover missing corner, got %+v", result.Tile.Polygons)
	}
}

func TestBuildNavTileUsesPrimaryGenericSourceBeforeVoxelNavigation(t *testing.T) {
	chunk := navTestChunk(8, 1.0, navTestFloorVoxels(1, 4, 1, 4, 0)...)
	chunk.NonEmptyVoxelCount = len(chunk.Voxels)
	world := &ImportedWorldDef{
		WorldID:         "world-source",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       chunk.ChunkSize,
		VoxelResolution: chunk.VoxelResolution,
	}
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	source, err := BuildNavBuildSourceFromImportedWorld(world, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		chunk.Coord: chunk,
	}, NavBuildSourceBuildOptions{AgentProfile: profile})
	if err != nil {
		t.Fatalf("BuildNavBuildSourceFromImportedWorld failed: %v", err)
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
		t.Fatalf("expected source-primary nav polygon without voxel duplicate, got tile=%+v source=%+v", result.Tile.Polygons, source.Surfaces)
	}
	if result.Tile.Polygons[0].ID != "surface:"+source.Surfaces[0].ID {
		t.Fatalf("expected source-derived polygon id, got %+v", result.Tile.Polygons[0])
	}
}

func TestBuildNavBuildSourceFromExplicitSurfacesKeepsSlopedSurface(t *testing.T) {
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	source, err := BuildNavBuildSourceFromExplicitSurfaces("explicit-world", []NavBuildExplicitSurfaceInput{{
		ID: "ramp",
		Vertices: []Vec3{
			{0, 0, 0},
			{4, 0, 0},
			{4, 1, 4},
			{0, 1, 4},
		},
		Normal: Vec3{0, 4, -1},
	}}, NavBuildSourceBuildOptions{AgentProfile: profile})
	if err != nil {
		t.Fatalf("BuildNavBuildSourceFromExplicitSurfaces failed: %v", err)
	}
	if len(source.Surfaces) != 1 || source.Surfaces[0].ID != "ramp" {
		t.Fatalf("expected explicit ramp surface, got %+v", source.Surfaces)
	}
	if source.BoundsMin != (Vec3{0, 0, 0}) || source.BoundsMax != (Vec3{4, 1, 4}) {
		t.Fatalf("unexpected explicit source bounds min=%+v max=%+v", source.BoundsMin, source.BoundsMax)
	}
}

func TestBuildNavBuildSourceFromExplicitSurfacesRejectsDownwardAuthoredNormal(t *testing.T) {
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	source, err := BuildNavBuildSourceFromExplicitSurfaces("explicit-world", []NavBuildExplicitSurfaceInput{{
		ID: "ceiling",
		Vertices: []Vec3{
			{0, 2, 0},
			{4, 2, 0},
			{4, 2, 4},
			{0, 2, 4},
		},
		Normal: Vec3{0, -1, 0},
	}}, NavBuildSourceBuildOptions{AgentProfile: profile})
	if err != nil {
		t.Fatalf("BuildNavBuildSourceFromExplicitSurfaces failed: %v", err)
	}
	if len(source.Surfaces) != 0 {
		t.Fatalf("did not expect downward-authored surface, got %+v", source.Surfaces)
	}
}

func TestBuildNavTileUsesExplicitSurfaceOverlappingTileBounds(t *testing.T) {
	profile := navTestAgentProfile(0.2, 1.0, 0.5)
	source, err := BuildNavBuildSourceFromExplicitSurfaces("explicit-world", []NavBuildExplicitSurfaceInput{{
		ID: "large_floor",
		Vertices: []Vec3{
			{0, 1, 0},
			{12, 1, 0},
			{12, 1, 4},
			{0, 1, 4},
		},
		Normal: Vec3{0, 1, 0},
	}}, NavBuildSourceBuildOptions{AgentProfile: profile})
	if err != nil {
		t.Fatalf("BuildNavBuildSourceFromExplicitSurfaces failed: %v", err)
	}
	chunk := navTestChunk(8, 1.0)
	chunk.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	result, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		BuildSource:        source,
		BuildSourcePrimary: true,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk failed: %v", err)
	}
	if !navTileHasPolygon(result.Tile, "surface:large_floor") {
		t.Fatalf("expected overlapping explicit surface in second tile, got %+v", result.Tile.Polygons)
	}
	bounds, ok := navPolygonBoundsForTile(result.Tile, result.Tile.Polygons[0])
	if !ok {
		t.Fatalf("expected clipped polygon bounds")
	}
	if !navAlmostEqual(bounds.min[0], 8, 1e-4) || !navAlmostEqual(bounds.min[1], 1, 1e-4) ||
		!navAlmostEqual(bounds.min[2], 0, 1e-4) || !navAlmostEqual(bounds.max[0], 12, 1e-4) ||
		!navAlmostEqual(bounds.max[1], 1, 1e-4) || !navAlmostEqual(bounds.max[2], 4, 1e-4) {
		t.Fatalf("expected overlapping source polygon clipped to tile bounds, got %+v", bounds)
	}
	if validation := ValidateNavTile(result.Tile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}
