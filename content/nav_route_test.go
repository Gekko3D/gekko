package content

import (
	"path/filepath"
	"testing"
)

func TestFindHierarchicalNavRouteReturnsCoarseRouteWithoutLoadedTiles(t *testing.T) {
	manifest := navRouteTestManifest("nav-route", "tiny", nil)

	route, err := FindHierarchicalNavRoute(manifest, "route.gknav", nil, "", Vec3{7.5, 1, 0.5}, Vec3{8.5, 1, 0.5}, NavHierarchicalRouteOptions{
		LocalPath: NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1},
	})
	if err != nil {
		t.Fatalf("FindHierarchicalNavRoute failed: %v", err)
	}
	if !route.Found || !route.SectorPath.Found || route.Refined || route.LocalPath.Found {
		t.Fatalf("expected coarse-only route when local tiles are absent, got %+v", route)
	}
	if route.RefinementStatus != NavRouteRefinementStatusCoarseOnly || route.RefinementReason != NavPathFailureMissingTile {
		t.Fatalf("expected missing-tile refinement reason, got %+v", route)
	}
	if route.StartSector != (TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}) || route.EndSector != (TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}) {
		t.Fatalf("unexpected route sectors: %+v -> %+v", route.StartSector, route.EndSector)
	}
}

func TestFindHierarchicalNavRouteRefinesThroughEffectiveTiles(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "route.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "route_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "route_navtiles", "tiny_1_0_0.gknavtile")
	if err := SaveNavTile(leftPath, navPathTestTile("nav-route", "tiny", leftCoord, "left", Vec3{7, 1, 0}, Vec3{8, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(rightPath, navPathTestTile("nav-route", "tiny", rightCoord, "right", Vec3{8, 1, 0}, Vec3{9, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	manifest := navRouteTestManifest("nav-route", "tiny", map[TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})

	route, err := FindHierarchicalNavRoute(manifest, navPath, nil, "", Vec3{7.5, 1, 0.5}, Vec3{8.5, 1, 0.5}, NavHierarchicalRouteOptions{
		LocalPath: NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1},
	})
	if err != nil {
		t.Fatalf("FindHierarchicalNavRoute failed: %v", err)
	}
	if !route.Found || !route.SectorPath.Found || !route.Refined || !route.LocalPath.Found {
		t.Fatalf("expected refined hierarchical route, got %+v", route)
	}
	if len(route.SectorPath.SectorCoords) != 2 || len(route.LocalPath.Steps) != 2 {
		t.Fatalf("expected sector and tile paths to cross the boundary, got %+v", route)
	}
	if route.RefinementStatus != NavRouteRefinementStatusRefined || route.RefinementReason != "" {
		t.Fatalf("expected refined status without failure reason, got %+v", route)
	}
}

func TestFindHierarchicalNavRouteKeepsCoarseRouteWhenDeltaBlocksLocalTile(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "route.gknav")
	deltaPath := filepath.Join(root, "levels", "route.gkworlddelta")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "route_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "route_navtiles", "tiny_1_0_0.gknavtile")
	if err := SaveNavTile(leftPath, navPathTestTile("nav-route", "tiny", leftCoord, "left", Vec3{7, 1, 0}, Vec3{8, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(rightPath, navPathTestTile("nav-route", "tiny", rightCoord, "right", Vec3{8, 1, 0}, Vec3{9, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	manifest := navRouteTestManifest("nav-route", "tiny", map[TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})
	delta := &WorldDeltaDef{
		NavigationTileOverrides: []NavigationTileOverrideDef{{
			NavID:          "nav-route",
			AgentProfileID: "tiny",
			ChunkCoord:     rightCoord,
			Empty:          true,
			NavBuildHash:   "empty",
		}},
	}

	route, err := FindHierarchicalNavRoute(manifest, navPath, delta, deltaPath, Vec3{7.5, 1, 0.5}, Vec3{8.5, 1, 0.5}, NavHierarchicalRouteOptions{
		LocalPath: NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1},
	})
	if err != nil {
		t.Fatalf("FindHierarchicalNavRoute failed: %v", err)
	}
	if !route.Found || !route.SectorPath.Found || route.Refined || route.LocalPath.Found {
		t.Fatalf("expected coarse route without blocked local refinement, got %+v", route)
	}
	if route.RefinementStatus != NavRouteRefinementStatusCoarseOnly || route.RefinementReason != NavPathFailureDeltaEmptyTile || route.RefinementTile != rightCoord {
		t.Fatalf("expected delta-empty refinement reason at right tile, got %+v", route)
	}
}

func TestFindHierarchicalNavRouteReportsDisabledLocalRefinement(t *testing.T) {
	manifest := navRouteTestManifest("nav-route", "tiny", nil)

	route, err := FindHierarchicalNavRoute(manifest, "route.gknav", nil, "", Vec3{7.5, 1, 0.5}, Vec3{8.5, 1, 0.5}, NavHierarchicalRouteOptions{
		DisableLocalRefinement: true,
	})
	if err != nil {
		t.Fatalf("FindHierarchicalNavRoute failed: %v", err)
	}
	if !route.Found || !route.SectorPath.Found || route.Refined || route.LocalPath.Found {
		t.Fatalf("expected coarse route with disabled refinement, got %+v", route)
	}
	if route.RefinementStatus != NavRouteRefinementStatusDisabled || route.RefinementReason != NavRouteRefinementReasonDisabled {
		t.Fatalf("expected disabled refinement reason, got %+v", route)
	}
}

func TestFindHierarchicalNavRouteConstrainsLocalRefinementToSectorCorridor(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "route.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	middleCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "route_navtiles", "tiny_0_0_0.gknavtile")
	middlePath := filepath.Join(root, "worlds", "route_navtiles", "tiny_1_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "route_navtiles", "tiny_2_0_0.gknavtile")
	if err := SaveNavTile(leftPath, navPathTestTile("nav-route", "tiny", leftCoord, "left", Vec3{7, 1, 0}, Vec3{8, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(middlePath, navPathTestTile("nav-route", "tiny", middleCoord, "middle", Vec3{8, 1, 0}, Vec3{16, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile middle failed: %v", err)
	}
	if err := SaveNavTile(rightPath, navPathTestTile("nav-route", "tiny", rightCoord, "right", Vec3{16, 1, 0}, Vec3{17, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	manifest := navRouteTestManifest("nav-route", "tiny", map[TerrainChunkCoordDef]string{
		leftCoord:   leftPath,
		middleCoord: middlePath,
		rightCoord:  rightPath,
	})
	manifest.Sectors = append(manifest.Sectors, NavSectorEntryDef{
		Coord:     rightCoord,
		BoundsMin: [3]float32{16, 0, 0},
		BoundsMax: [3]float32{24, 8, 8},
	})
	manifest.Sectors[0].Links = []NavSectorLinkDef{{
		ID:   "coarse-shortcut",
		To:   rightCoord,
		Kind: NavTraversalWalk,
		Cost: 1,
	}}

	route, err := FindHierarchicalNavRoute(manifest, navPath, nil, "", Vec3{7.5, 1, 0.5}, Vec3{16.5, 1, 0.5}, NavHierarchicalRouteOptions{
		LocalPath: NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 2},
	})
	if err != nil {
		t.Fatalf("FindHierarchicalNavRoute failed: %v", err)
	}
	if !route.Found || !route.SectorPath.Found || len(route.SectorPath.SectorCoords) != 2 {
		t.Fatalf("expected coarse shortcut corridor, got %+v", route)
	}
	if route.Refined || route.LocalPath.Found {
		t.Fatalf("did not expect refinement through middle sector outside coarse corridor, got %+v", route)
	}
	if route.RefinementStatus != NavRouteRefinementStatusCoarseOnly || route.RefinementReason != NavPathFailureDisallowedTile || route.RefinementTile != middleCoord {
		t.Fatalf("expected corridor disallowed-tile refinement reason, got %+v", route)
	}
}

func TestFindHierarchicalNavRouteCanFallbackOutsideSectorCorridor(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "route.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	middleCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "route_navtiles", "tiny_0_0_0.gknavtile")
	middlePath := filepath.Join(root, "worlds", "route_navtiles", "tiny_1_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "route_navtiles", "tiny_2_0_0.gknavtile")
	if err := SaveNavTile(leftPath, navPathTestTile("nav-route", "tiny", leftCoord, "left", Vec3{7, 1, 0}, Vec3{8, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(middlePath, navPathTestTile("nav-route", "tiny", middleCoord, "middle", Vec3{8, 1, 0}, Vec3{16, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile middle failed: %v", err)
	}
	if err := SaveNavTile(rightPath, navPathTestTile("nav-route", "tiny", rightCoord, "right", Vec3{16, 1, 0}, Vec3{17, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	manifest := navRouteTestManifest("nav-route", "tiny", map[TerrainChunkCoordDef]string{
		leftCoord:   leftPath,
		middleCoord: middlePath,
		rightCoord:  rightPath,
	})
	manifest.Sectors = append(manifest.Sectors, NavSectorEntryDef{
		Coord:     rightCoord,
		BoundsMin: [3]float32{16, 0, 0},
		BoundsMax: [3]float32{24, 8, 8},
	})
	manifest.Sectors[0].Links = []NavSectorLinkDef{{
		ID:   "coarse-shortcut",
		To:   rightCoord,
		Kind: NavTraversalWalk,
		Cost: 1,
	}}

	route, err := FindHierarchicalNavRoute(manifest, navPath, nil, "", Vec3{7.5, 1, 0.5}, Vec3{16.5, 1, 0.5}, NavHierarchicalRouteOptions{
		AllowLocalCorridorFallback: true,
		LocalPath:                  NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 2},
	})
	if err != nil {
		t.Fatalf("FindHierarchicalNavRoute failed: %v", err)
	}
	if !route.Found || !route.Refined || !route.LocalPath.Found || !route.LocalPathCorridorFallback {
		t.Fatalf("expected fallback-refined route, got %+v", route)
	}
	if len(route.LocalPath.Steps) != 3 || route.LocalPath.Steps[1].Coord != middleCoord {
		t.Fatalf("expected fallback local path through middle tile, got %+v", route.LocalPath.Steps)
	}
	if route.RefinementStatus != NavRouteRefinementStatusRefined || route.RefinementReason != "" {
		t.Fatalf("expected refined status after fallback, got %+v", route)
	}
}

func TestFindHierarchicalNavRouteCanFallbackOutsideSectorCorridorAfterClearanceNoPath(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "route.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	middleCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}
	manifest := &NavManifestDef{
		NavID:           "nav-route-clearance",
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		BuilderVersion:  DefaultNavBuilderVersion,
		ChunkSize:       4,
		VoxelResolution: 1,
		AgentProfiles: []NavAgentProfileDef{
			navTestAgentProfileWithID("small", 0.2, 1.0, 1.0),
		},
		Sectors: []NavSectorEntryDef{
			{
				Coord:     leftCoord,
				BoundsMin: [3]float32{0, 0, 0},
				BoundsMax: [3]float32{4, 4, 4},
				Links: []NavSectorLinkDef{{
					ID:   "coarse-shortcut",
					To:   rightCoord,
					Kind: NavTraversalWalk,
					Cost: 1,
				}},
			},
			{
				Coord:     rightCoord,
				BoundsMin: [3]float32{8, 0, 0},
				BoundsMax: [3]float32{12, 4, 4},
			},
		},
	}
	for _, coord := range []TerrainChunkCoordDef{leftCoord, middleCoord, rightCoord} {
		chunk := navTestChunk(4, 1.0, navTestFloorVoxels(0, 3, 1, 1, 0)...)
		chunk.Coord = coord
		source, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
			NavID:              manifest.NavID,
			MaxClearanceRadius: 1.0,
		})
		if err != nil {
			t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
		}
		sourcePath := filepath.Join(root, "worlds", "route_navsources", TerrainChunkKey(coord)+".gknavsource")
		if err := SaveNavClearanceSourceTile(sourcePath, source.Tile); err != nil {
			t.Fatalf("SaveNavClearanceSourceTile failed: %v", err)
		}
		manifest.ClearanceSourceTiles = append(manifest.ClearanceSourceTiles, NavClearanceSourceTileEntryDef{
			Coord:              coord,
			TilePath:           AuthorDocumentPath(sourcePath, navPath),
			PayloadKind:        source.Tile.PayloadKind,
			BoundsMin:          source.Tile.BoundsMin,
			BoundsMax:          source.Tile.BoundsMax,
			MaxClearanceRadius: source.Tile.MaxClearanceRadius,
		})
	}
	EnsureNavManifestDefaults(manifest)

	restricted, err := FindEffectiveNavPath(manifest, navPath, nil, "", Vec3{3.5, 1, 1.5}, Vec3{8.5, 1, 1.5}, NavPathOptions{
		AgentProfileID:      "small",
		MaxTileSearchRadius: 2,
		AllowedTileCoords: map[TerrainChunkCoordDef]struct{}{
			leftCoord:  {},
			rightCoord: {},
		},
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath restricted failed: %v", err)
	}
	if restricted.Found || restricted.FailureReason != NavPathFailureNoPath {
		t.Fatalf("expected restricted clearance path to fail with no_path, got %+v", restricted)
	}

	route, err := FindHierarchicalNavRoute(manifest, navPath, nil, "", Vec3{3.5, 1, 1.5}, Vec3{8.5, 1, 1.5}, NavHierarchicalRouteOptions{
		AllowLocalCorridorFallback: true,
		LocalPath:                  NavPathOptions{AgentProfileID: "small", MaxTileSearchRadius: 2},
	})
	if err != nil {
		t.Fatalf("FindHierarchicalNavRoute failed: %v", err)
	}
	if !route.Found || !route.Refined || !route.LocalPath.Found || !route.LocalPathCorridorFallback {
		t.Fatalf("expected clearance fallback-refined route, got %+v", route)
	}
	if len(route.LocalPath.Steps) < 3 || route.LocalPath.Steps[1].Coord != middleCoord {
		t.Fatalf("expected fallback clearance path through middle tile, got %+v", route.LocalPath.Steps)
	}
}

func navRouteTestManifest(navID string, profileID string, tilePaths map[TerrainChunkCoordDef]string) *NavManifestDef {
	manifest := navPathTestManifest(navID, profileID, "route.gknav", tilePaths)
	manifest.Sectors = []NavSectorEntryDef{
		{
			Coord:     TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			BoundsMin: [3]float32{0, 0, 0},
			BoundsMax: [3]float32{8, 8, 8},
		},
		{
			Coord:     TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
			BoundsMin: [3]float32{8, 0, 0},
			BoundsMax: [3]float32{16, 8, 8},
		},
	}
	return manifest
}
