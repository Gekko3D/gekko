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

func TestFindNavSectorPathDynamicOverlayBlocksDoorLink(t *testing.T) {
	manifest := navSectorGraphTestManifest()
	manifest.Sectors[0].Links = []NavSectorLinkDef{{
		ID:         "door-a",
		TargetName: "door_a",
		To:         TerrainChunkCoordDef{X: 2, Y: 0, Z: 0},
		Kind:       NavTraversalDoor,
		Cost:       1,
		Openable:   true,
	}}

	path, err := FindNavSectorPath(manifest, TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}, NavSectorPathOptions{
		AllowedKinds: []string{NavTraversalWalk, NavTraversalDoor},
		DynamicOverlay: NavDynamicOverlayDef{Entries: []NavDynamicOverlayEntryDef{{
			ID:      "closed-door-a",
			Kind:    NavDynamicOverlayEntryKindLink,
			LinkID:  "door-a",
			Blocked: true,
			Reason:  "door_closed",
		}}},
	})
	if err != nil {
		t.Fatalf("FindNavSectorPath failed: %v", err)
	}
	if !path.Found || len(path.SectorCoords) != 3 || path.Cost != 20 {
		t.Fatalf("expected blocked door shortcut to fall back to walk sectors, got %+v", path)
	}
	for _, edge := range path.Edges {
		if edge.LinkID == "door-a" {
			t.Fatalf("did not expect blocked door link in path, got %+v", path)
		}
	}
}

func TestFindNavSectorPathDynamicOverlayAddsDoorAction(t *testing.T) {
	manifest := navSectorGraphTestManifest()
	manifest.Sectors[0].Links = []NavSectorLinkDef{{
		ID:         "door-a",
		TargetName: "door_a",
		To:         TerrainChunkCoordDef{X: 2, Y: 0, Z: 0},
		Kind:       NavTraversalDoor,
		Cost:       1,
		Openable:   true,
	}}

	path, err := FindNavSectorPath(manifest, TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}, NavSectorPathOptions{
		AllowedKinds: []string{NavTraversalWalk, NavTraversalDoor},
		DynamicOverlay: NavDynamicOverlayDef{Entries: []NavDynamicOverlayEntryDef{{
			ID:         "open-door-a",
			Kind:       NavDynamicOverlayEntryKindLink,
			TargetName: "door_a",
			Action:     "open_door",
			Reason:     "door_openable",
		}}},
	})
	if err != nil {
		t.Fatalf("FindNavSectorPath failed: %v", err)
	}
	if !path.Found || len(path.SectorCoords) != 2 || len(path.Edges) != 1 || path.Edges[0].LinkID != "door-a" {
		t.Fatalf("expected door shortcut path, got %+v", path)
	}
	if len(path.Actions) != 1 || path.Actions[0].Action != "open_door" || path.Actions[0].LinkID != "door-a" || path.Actions[0].TargetName != "door_a" {
		t.Fatalf("expected door traversal action, got %+v", path.Actions)
	}
}

func TestFindNavSectorPathDynamicOverlayMatchesLinkTargetName(t *testing.T) {
	manifest := navSectorGraphTestManifest()
	manifest.Sectors[0].Links = []NavSectorLinkDef{{
		ID:         "link-1",
		TargetName: "door_a",
		To:         TerrainChunkCoordDef{X: 2, Y: 0, Z: 0},
		Kind:       NavTraversalDoor,
		Cost:       1,
		Openable:   true,
	}}

	path, err := FindNavSectorPath(manifest, TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}, NavSectorPathOptions{
		AllowedKinds: []string{NavTraversalWalk, NavTraversalDoor},
		DynamicOverlay: NavDynamicOverlayDef{Entries: []NavDynamicOverlayEntryDef{{
			ID:         "closed-door-a",
			Kind:       NavDynamicOverlayEntryKindLink,
			TargetName: "door_a",
			Blocked:    true,
		}}},
	})
	if err != nil {
		t.Fatalf("FindNavSectorPath failed: %v", err)
	}
	if !path.Found || len(path.SectorCoords) != 3 || path.Cost != 20 {
		t.Fatalf("expected target-name overlay to block door link, got %+v", path)
	}
}

func TestFindNavSectorPathDynamicOverlayBlocksBoundsEdge(t *testing.T) {
	manifest := navSectorGraphTestManifest()

	path, err := FindNavSectorPath(manifest, TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}, NavSectorPathOptions{
		DynamicOverlay: NavDynamicOverlayDef{Entries: []NavDynamicOverlayEntryDef{{
			ID:        "crate-0-1",
			Kind:      NavDynamicOverlayEntryKindBounds,
			BoundsMin: Vec3{9, 0, 4},
			BoundsMax: Vec3{11, 4, 6},
			Blocked:   true,
			Reason:    "temporary_blocker",
		}}},
	})
	if err != nil {
		t.Fatalf("FindNavSectorPath failed: %v", err)
	}
	if path.Found {
		t.Fatalf("expected bounds blocker to disconnect the only sector route, got %+v", path)
	}
}

func TestFindNavSectorPathDynamicOverlayBoundsCostChoosesDetour(t *testing.T) {
	manifest := navSectorGraphTestManifestWithDetour()
	start := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	end := TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}

	path, err := FindNavSectorPath(manifest, start, end, NavSectorPathOptions{})
	if err != nil {
		t.Fatalf("FindNavSectorPath failed: %v", err)
	}
	if !path.Found || len(path.SectorCoords) != 3 || path.SectorCoords[1] != (TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}) {
		t.Fatalf("expected direct middle route before overlay, got %+v", path)
	}

	path, err = FindNavSectorPath(manifest, start, end, NavSectorPathOptions{
		DynamicOverlay: NavDynamicOverlayDef{Entries: []NavDynamicOverlayEntryDef{{
			ID:        "mud-0-1",
			Kind:      NavDynamicOverlayEntryKindBounds,
			BoundsMin: Vec3{9, 0, 4},
			BoundsMax: Vec3{11, 4, 6},
			CostAdd:   100,
			Reason:    "temporary_slow_area",
		}}},
	})
	if err != nil {
		t.Fatalf("FindNavSectorPath with overlay failed: %v", err)
	}
	if !path.Found || len(path.SectorCoords) < 4 || path.SectorCoords[1] != (TerrainChunkCoordDef{X: 0, Y: 0, Z: 1}) {
		t.Fatalf("expected bounds cost to route through detour, got %+v", path)
	}
	for _, edge := range path.Edges {
		if edge.From == start && edge.To == (TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}) {
			t.Fatalf("expected bounds cost to avoid the repriced direct edge, got %+v", path)
		}
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

func navSectorGraphTestManifestWithDetour() *NavManifestDef {
	manifest := navSectorGraphTestManifest()
	manifest.Sectors = append(manifest.Sectors,
		NavSectorEntryDef{
			Coord:     TerrainChunkCoordDef{X: 0, Y: 0, Z: 1},
			BoundsMin: [3]float32{0, 0, 10},
			BoundsMax: [3]float32{10, 10, 20},
		},
		NavSectorEntryDef{
			Coord:     TerrainChunkCoordDef{X: 1, Y: 0, Z: 1},
			BoundsMin: [3]float32{10, 0, 10},
			BoundsMax: [3]float32{20, 10, 20},
		},
		NavSectorEntryDef{
			Coord:     TerrainChunkCoordDef{X: 2, Y: 0, Z: 1},
			BoundsMin: [3]float32{20, 0, 10},
			BoundsMax: [3]float32{30, 10, 20},
		},
	)
	return manifest
}

func navSectorGraphHasEdge(graph NavSectorGraph, from TerrainChunkCoordDef, to TerrainChunkCoordDef, source string) bool {
	for _, edge := range graph.Edges[from] {
		if edge.To == to && edge.Source == source {
			return true
		}
	}
	return false
}
