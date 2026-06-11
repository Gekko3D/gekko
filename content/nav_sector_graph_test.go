package content

import "testing"

func TestBuildNavSectorGraphInfersBoundsAdjacency(t *testing.T) {
	manifest := navSectorGraphTestManifest()

	graph, err := BuildNavSectorGraph(manifest, NavSectorGraphOptions{})
	if err != nil {
		t.Fatalf("BuildNavSectorGraph failed: %v", err)
	}
	if len(graph.Sectors) != 3 {
		t.Fatalf("expected three sectors, got %+v", graph.Sectors)
	}
	if !navSectorGraphHasEdge(graph, TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}, NavSectorEdgeSourceBoundsAdjacency) {
		t.Fatalf("expected inferred edge from sector 0 to 1, got %+v", graph.Edges)
	}
	if navSectorGraphHasEdge(graph, TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}, NavSectorEdgeSourceBoundsAdjacency) {
		t.Fatalf("did not expect inferred edge across non-touching sector, got %+v", graph.Edges)
	}
}

func TestFindNavSectorPathRoutesWithoutLoadedTiles(t *testing.T) {
	manifest := navSectorGraphTestManifest()

	path, err := FindNavSectorPath(manifest, TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}, NavSectorPathOptions{
		AgentSpeed: 2,
	})
	if err != nil {
		t.Fatalf("FindNavSectorPath failed: %v", err)
	}
	if !path.Found || len(path.SectorCoords) != 3 || path.Cost != 20 || path.EstimatedTravelSeconds != 10 {
		t.Fatalf("expected coarse sector route through unloaded local tiles, got %+v", path)
	}
	if path.SectorCoords[0] != (TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}) || path.SectorCoords[2] != (TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}) {
		t.Fatalf("unexpected sector coords: %+v", path.SectorCoords)
	}
}

func TestFindNavSectorPathUsesAllowedTaggedShortcut(t *testing.T) {
	manifest := navSectorGraphTestManifest()
	manifest.Sectors[0].Links = []NavSectorLinkDef{{
		ID:          "maintenance-door",
		To:          TerrainChunkCoordDef{X: 2, Y: 0, Z: 0},
		Kind:        NavTraversalDoor,
		Cost:        5,
		Openable:    true,
		RequiresTag: "maintenance",
	}}

	withoutTag, err := FindNavSectorPath(manifest, TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}, NavSectorPathOptions{})
	if err != nil {
		t.Fatalf("FindNavSectorPath without tag failed: %v", err)
	}
	if !withoutTag.Found || withoutTag.Cost != 20 || len(withoutTag.SectorCoords) != 3 {
		t.Fatalf("expected route to ignore tagged shortcut, got %+v", withoutTag)
	}

	withTag, err := FindNavSectorPath(manifest, TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}, NavSectorPathOptions{
		AllowedKinds: []string{NavTraversalWalk, NavTraversalDoor},
		AgentTags:    []string{"maintenance"},
	})
	if err != nil {
		t.Fatalf("FindNavSectorPath with tag failed: %v", err)
	}
	if !withTag.Found || withTag.Cost != 5 || len(withTag.SectorCoords) != 2 || len(withTag.Edges) != 1 {
		t.Fatalf("expected tagged shortcut route, got %+v", withTag)
	}
	if withTag.Edges[0].Source != NavSectorEdgeSourceLink || withTag.Edges[0].LinkID != "maintenance-door" {
		t.Fatalf("expected path to use sector link, got %+v", withTag.Edges)
	}
}

func TestFindNavSectorPathBetweenPoints(t *testing.T) {
	manifest := navSectorGraphTestManifest()

	path, err := FindNavSectorPathBetweenPoints(manifest, Vec3{1, 1, 1}, Vec3{25, 1, 1}, NavSectorPathOptions{})
	if err != nil {
		t.Fatalf("FindNavSectorPathBetweenPoints failed: %v", err)
	}
	if !path.Found || len(path.SectorCoords) != 3 {
		t.Fatalf("expected point route across sectors, got %+v", path)
	}
}

func navSectorGraphTestManifest() *NavManifestDef {
	return &NavManifestDef{
		NavID:           "nav-sector-test",
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		BuilderVersion:  DefaultNavBuilderVersion,
		ChunkSize:       10,
		VoxelResolution: 1,
		AgentProfiles:   []NavAgentProfileDef{DefaultHL1NavAgentProfile()},
		Tiles:           nil,
		Sectors:         navSectorGraphTestSectors(),
		SourceWorldID:   "world-sector-test",
		SourceLevelHash: "sector-source",
		LevelID:         "level-sector-test",
	}
}

func navSectorGraphTestSectors() []NavSectorEntryDef {
	return []NavSectorEntryDef{
		{
			Coord:     TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			BoundsMin: [3]float32{0, 0, 0},
			BoundsMax: [3]float32{10, 10, 10},
		},
		{
			Coord:     TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
			BoundsMin: [3]float32{10, 0, 0},
			BoundsMax: [3]float32{20, 10, 10},
		},
		{
			Coord:     TerrainChunkCoordDef{X: 2, Y: 0, Z: 0},
			BoundsMin: [3]float32{20, 0, 0},
			BoundsMax: [3]float32{30, 10, 10},
		},
	}
}

func navSectorGraphHasEdge(graph NavSectorGraph, from TerrainChunkCoordDef, to TerrainChunkCoordDef, source string) bool {
	for _, edge := range graph.Edges[from] {
		if edge.To == to && edge.Source == source {
			return true
		}
	}
	return false
}
