package content

import (
	"path/filepath"
	"testing"
)

func TestFindEffectiveNavPathStitchesAdjacentStaticTiles(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_1_0_0.gknavtile")
	leftTile := navPathTestTile("nav-path", "tiny", leftCoord, "left", Vec3{7, 1, 0}, Vec3{8, 1, 1})
	rightTile := navPathTestTile("nav-path", "tiny", rightCoord, "right", Vec3{8, 1, 0}, Vec3{9, 1, 1})
	if err := SaveNavTile(leftPath, leftTile); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(rightPath, rightTile); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{7.5, 1, 0.5}, Vec3{8.5, 1, 0.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 2 || len(path.Waypoints) != 2 {
		t.Fatalf("expected cross-tile path, got %+v", path)
	}
	if path.Steps[0].Coord != leftCoord || path.Steps[0].PolygonID != "left" || path.Steps[1].Coord != rightCoord || path.Steps[1].PolygonID != "right" {
		t.Fatalf("unexpected path steps: %+v", path.Steps)
	}
	if path.Waypoints[0] != (Vec3{7.5, 1, 0.5}) || path.Waypoints[1] != (Vec3{8.5, 1, 0.5}) {
		t.Fatalf("unexpected waypoints: %+v", path.Waypoints)
	}
}

func TestFindEffectiveNavPathUsesOneWayOffMeshDropLink(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	tilePath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	tile := &NavTileDef{
		NavID:          "nav-path",
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          coord,
		AgentProfileID: "tiny",
		BuilderVersion: DefaultNavBuilderVersion,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{8, 8, 8},
		Vertices: []Vec3{
			{1, 3, 1}, {2, 3, 1}, {2, 3, 2}, {1, 3, 2},
			{3, 1, 1}, {4, 1, 1}, {4, 1, 2}, {3, 1, 2},
		},
		Polygons: []NavPolygonDef{
			{ID: "upper", Vertices: []int{0, 1, 2, 3}, Area: NavTraversalWalk},
			{ID: "lower", Vertices: []int{4, 5, 6, 7}, Area: NavTraversalWalk},
		},
		OffMeshLinks: []NavOffMeshLinkDef{{
			ID:            "drop:upper:lower",
			Kind:          NavTraversalDrop,
			Start:         Vec3{1.5, 3, 1.5},
			End:           Vec3{3.5, 1, 1.5},
			Radius:        0.2,
			Cost:          2.828,
			Bidirectional: false,
			Tags:          []string{"generated"},
		}},
	}
	if err := SaveNavTile(tilePath, tile); err != nil {
		t.Fatalf("SaveNavTile failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		coord: tilePath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{1.5, 3, 1.5}, Vec3{3.5, 1, 1.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath upper->lower failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 2 || path.Steps[0].PolygonID != "upper" || path.Steps[1].PolygonID != "lower" {
		t.Fatalf("expected path down one-way off-mesh drop link, got %+v", path)
	}
	if path.Steps[1].EnterKind != NavTraversalDrop || path.Steps[1].EnterLinkID != "drop:upper:lower" || path.Steps[1].EnterLinkKind != NavTraversalDrop {
		t.Fatalf("expected drop traversal metadata on entered step, got %+v", path.Steps)
	}
	reverse, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{3.5, 1, 1.5}, Vec3{1.5, 3, 1.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath lower->upper failed: %v", err)
	}
	if reverse.Found {
		t.Fatalf("did not expect one-way off-mesh drop link to be climbable, got %+v", reverse)
	}
}

func TestFindEffectiveNavPathUsesCrossTileOffMeshDropLink(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 0.5)
	profile.MaxDropHeight = 3
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	left := navTestChunk(8, 1.0, navTestFloorVoxels(7, 7, 1, 2, 2)...)
	right := navTestChunk(8, 1.0, navTestFloorVoxels(0, 0, 1, 2, 0)...)
	right.Coord = rightCoord
	chunks := map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		leftCoord:  left,
		rightCoord: right,
	}
	leftResult, err := BuildNavTileFromImportedWorldChunk(left, profile, NavTileBuildOptions{NavID: "nav-path", NeighborChunks: chunks})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk left failed: %v", err)
	}
	rightResult, err := BuildNavTileFromImportedWorldChunk(right, profile, NavTileBuildOptions{NavID: "nav-path", NeighborChunks: chunks})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk right failed: %v", err)
	}
	leftPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_1_0_0.gknavtile")
	if err := SaveNavTile(leftPath, leftResult.Tile); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(rightPath, rightResult.Tile); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{7.5, 3, 1.5}, Vec3{8.5, 1, 1.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath upper->lower failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 2 || path.Steps[0].Coord != leftCoord || path.Steps[1].Coord != rightCoord {
		t.Fatalf("expected path down cross-tile off-mesh drop link, got %+v", path)
	}
	if path.Steps[1].EnterKind != NavTraversalDrop || path.Steps[1].EnterLinkID == "" || path.Steps[1].EnterLinkKind != NavTraversalDrop {
		t.Fatalf("expected cross-tile drop traversal metadata on entered step, got %+v", path.Steps)
	}
	reverse, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{8.5, 1, 1.5}, Vec3{7.5, 3, 1.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath lower->upper failed: %v", err)
	}
	if reverse.Found {
		t.Fatalf("did not expect cross-tile drop link to be climbable, got %+v", reverse)
	}
}

func TestFindEffectiveNavPathStitchesHorizontalNeighborAcrossExactYBoundary(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	lowerCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	upperRightCoord := TerrainChunkCoordDef{X: 1, Y: 1, Z: 0}
	lowerPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	upperRightPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_1_1_0.gknavtile")
	lowerTile := navPathTestTile("nav-path", "tiny", lowerCoord, "voxel_top_boundary", Vec3{7, 8, 1}, Vec3{8, 8, 3})
	upperRightTile := navPathTestTile("nav-path", "tiny", upperRightCoord, "source_exact_boundary", Vec3{8, 8, 1}, Vec3{9, 8, 3})
	if err := SaveNavTile(lowerPath, lowerTile); err != nil {
		t.Fatalf("SaveNavTile lower failed: %v", err)
	}
	if err := SaveNavTile(upperRightPath, upperRightTile); err != nil {
		t.Fatalf("SaveNavTile upper right failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		lowerCoord:      lowerPath,
		upperRightCoord: upperRightPath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{7.5, 8, 2}, Vec3{8.5, 8, 2}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 2 {
		t.Fatalf("expected path across mixed Y-owner horizontal boundary, got %+v", path)
	}
	if path.Steps[0].Coord != lowerCoord || path.Steps[1].Coord != upperRightCoord {
		t.Fatalf("unexpected path steps: %+v", path.Steps)
	}
}

func TestFindEffectiveNavPathFindsPolygonOnExactTileMaxBoundary(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	tilePath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	tile := navPathTestTile("nav-path", "tiny", coord, "top_boundary", Vec3{1, 8, 1}, Vec3{4, 8, 4})
	if err := SaveNavTile(tilePath, tile); err != nil {
		t.Fatalf("SaveNavTile failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		coord: tilePath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{2, 8, 2}, Vec3{3, 8, 3}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 1 || path.Steps[0].Coord != coord || path.Steps[0].PolygonID != "top_boundary" {
		t.Fatalf("expected exact max-boundary point to resolve to lower tile polygon, got %+v", path)
	}
}

func TestFindEffectiveNavPathSnapsEndpointToNearbyPolygon(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	tilePath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	tile := navPathTestTile("nav-path", "tiny", coord, "floor", Vec3{1, 1, 1}, Vec3{4, 1, 4})
	if err := SaveNavTile(tilePath, tile); err != nil {
		t.Fatalf("SaveNavTile failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		coord: tilePath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{4.25, 1, 2}, Vec3{3, 1, 2}, NavPathOptions{
		AgentProfileID:       "tiny",
		MaxTileSearchRadius:  1,
		EndpointSnapDistance: 0.3,
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 1 || path.Steps[0].PolygonID != "floor" {
		t.Fatalf("expected snapped single-polygon path, got %+v", path)
	}
	if !path.StartSnapped || !navAlmostEqual(path.StartSnapDistance, 0.25, 1e-5) {
		t.Fatalf("expected start snap distance 0.25, got %+v", path)
	}
	if path.EndSnapped || path.EndSnapDistance != 0 {
		t.Fatalf("did not expect end snap, got %+v", path)
	}
	if !navAlmostEqual(path.StartPoint[0], 4, 1e-5) || !navAlmostEqual(path.StartPoint[1], 1, 1e-5) || !navAlmostEqual(path.StartPoint[2], 2, 1e-5) {
		t.Fatalf("unexpected snapped start point: %+v", path.StartPoint)
	}
}

func TestFindEffectiveNavPathRejectsEndpointBeyondSnapDistance(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	tilePath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	tile := navPathTestTile("nav-path", "tiny", coord, "floor", Vec3{1, 1, 1}, Vec3{4, 1, 4})
	if err := SaveNavTile(tilePath, tile); err != nil {
		t.Fatalf("SaveNavTile failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		coord: tilePath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{4.25, 1, 2}, Vec3{3, 1, 2}, NavPathOptions{
		AgentProfileID:       "tiny",
		MaxTileSearchRadius:  1,
		EndpointSnapDistance: 0.1,
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if path.Found || path.FailureReason != NavPathFailureStartPolygonMissing || path.FailureCoord != coord {
		t.Fatalf("expected start polygon missing beyond snap distance, got %+v", path)
	}
}

func TestFindEffectiveNavPathStitchesSameSourcePolygonCopiedAcrossTiles(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_1_0_0.gknavtile")
	leftTile := navPathTestTile("nav-path", "tiny", leftCoord, "surface:long_floor", Vec3{0, 1, 0}, Vec3{8, 1, 1})
	rightTile := navPathTestTile("nav-path", "tiny", rightCoord, "surface:long_floor", Vec3{8, 1, 0}, Vec3{16, 1, 1})
	if err := SaveNavTile(leftPath, leftTile); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(rightPath, rightTile); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{7.5, 1, 0.5}, Vec3{8.5, 1, 0.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 2 {
		t.Fatalf("expected path across duplicated source polygon tiles, got %+v", path)
	}
	if path.Steps[0].Coord != leftCoord || path.Steps[0].PolygonID != "surface:long_floor" || path.Steps[1].Coord != rightCoord || path.Steps[1].PolygonID != "surface:long_floor" {
		t.Fatalf("unexpected path steps: %+v", path.Steps)
	}
}

func TestFindEffectiveNavPathHonorsEmptyDeltaTile(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	deltaPath := filepath.Join(root, "levels", "path.gkworlddelta")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_1_0_0.gknavtile")
	if err := SaveNavTile(leftPath, navPathTestTile("nav-path", "tiny", leftCoord, "left", Vec3{7, 1, 0}, Vec3{8, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(rightPath, navPathTestTile("nav-path", "tiny", rightCoord, "right", Vec3{8, 1, 0}, Vec3{9, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})
	delta := &WorldDeltaDef{
		LevelID: "level-a",
		NavigationTileOverrides: []NavigationTileOverrideDef{{
			NavID:          "nav-path",
			AgentProfileID: "tiny",
			ChunkCoord:     rightCoord,
			Empty:          true,
			NavBuildHash:   "empty",
		}},
	}

	path, err := FindEffectiveNavPath(baseNav, navPath, delta, deltaPath, Vec3{7.5, 1, 0.5}, Vec3{8.5, 1, 0.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if path.Found {
		t.Fatalf("did not expect path through empty delta tile, got %+v", path)
	}
}

func TestFindEffectiveNavPathRequiresTouchingBoundaryPolygons(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_1_0_0.gknavtile")
	if err := SaveNavTile(leftPath, navPathTestTile("nav-path", "tiny", leftCoord, "left", Vec3{6, 1, 0}, Vec3{7, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(rightPath, navPathTestTile("nav-path", "tiny", rightCoord, "right", Vec3{8, 1, 0}, Vec3{9, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{6.5, 1, 0.5}, Vec3{8.5, 1, 0.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if path.Found {
		t.Fatalf("did not expect path across non-touching polygons, got %+v", path)
	}
}

func TestFindEffectiveNavPathUsesStoredTilePortals(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_1_0_0.gknavtile")
	leftTile := navPathTestTile("nav-path", "tiny", leftCoord, "left", Vec3{6, 1, 0}, Vec3{7, 1, 1})
	rightTile := navPathTestTile("nav-path", "tiny", rightCoord, "right", Vec3{8, 1, 0}, Vec3{9, 1, 1})
	leftTile.Portals = []NavPortalDef{{
		ID:            "portal:left:right",
		FromPolygonID: "left",
		ToTileCoord:   rightCoord,
		ToPolygonID:   "right",
		Start:         Vec3{7, 1, 0},
		End:           Vec3{7, 1, 1},
		Area:          NavTraversalWalk,
	}}
	if err := SaveNavTile(leftPath, leftTile); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(rightPath, rightTile); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{6.5, 1, 0.5}, Vec3{8.5, 1, 0.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 2 {
		t.Fatalf("expected explicit portal path across non-touching polygons, got %+v", path)
	}
}

func TestFindEffectiveNavPathReportsNarrowPortalClearance(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_1_0_0.gknavtile")
	leftTile := navPathTestTile("nav-path", "tiny", leftCoord, "left", Vec3{6, 1, 0}, Vec3{7, 1, 1})
	rightTile := navPathTestTile("nav-path", "tiny", rightCoord, "right", Vec3{8, 1, 0}, Vec3{9, 1, 1})
	leftTile.Portals = []NavPortalDef{{
		ID:            "portal:left:right:narrow",
		FromPolygonID: "left",
		ToTileCoord:   rightCoord,
		ToPolygonID:   "right",
		Start:         Vec3{7, 1, 0.45},
		End:           Vec3{7, 1, 0.55},
		Area:          NavTraversalWalk,
	}}
	if err := SaveNavTile(leftPath, leftTile); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(rightPath, rightTile); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{6.5, 1, 0.5}, Vec3{8.5, 1, 0.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Portals) != 1 {
		t.Fatalf("expected explicit narrow portal path, got %+v", path)
	}
	portal := path.Portals[0]
	if portal.ClearanceOK || portal.ClearanceReason != NavPathPortalClearanceTooNarrow {
		t.Fatalf("expected narrow portal clearance warning, got %+v", portal)
	}
	if !navAlmostEqual(portal.Width, 0.1, 1e-4) || !navAlmostEqual(portal.RequiredWidth, 0.4, 1e-4) {
		t.Fatalf("unexpected portal clearance sizes, got %+v", portal)
	}
}

func TestFindEffectiveNavPathUsesBoundaryFallbackWhenStoredPortalsArePartial(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	centerCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	eastCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	northCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 1}
	centerPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	eastPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_1_0_0.gknavtile")
	northPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_1.gknavtile")
	centerTile := navPathTestTile("nav-path", "tiny", centerCoord, "center", Vec3{7, 1, 7}, Vec3{8, 1, 8})
	centerTile.Portals = []NavPortalDef{{
		ID:            "portal:center:east",
		FromPolygonID: "center",
		ToTileCoord:   eastCoord,
		ToPolygonID:   "east",
		Start:         Vec3{8, 1, 7},
		End:           Vec3{8, 1, 8},
		Area:          NavTraversalWalk,
	}}
	eastTile := navPathTestTile("nav-path", "tiny", eastCoord, "east", Vec3{8, 1, 7}, Vec3{9, 1, 8})
	northTile := navPathTestTile("nav-path", "tiny", northCoord, "north", Vec3{7, 1, 8}, Vec3{8, 1, 9})
	if err := SaveNavTile(centerPath, centerTile); err != nil {
		t.Fatalf("SaveNavTile center failed: %v", err)
	}
	if err := SaveNavTile(eastPath, eastTile); err != nil {
		t.Fatalf("SaveNavTile east failed: %v", err)
	}
	if err := SaveNavTile(northPath, northTile); err != nil {
		t.Fatalf("SaveNavTile north failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		centerCoord: centerPath,
		eastCoord:   eastPath,
		northCoord:  northPath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{7.5, 1, 7.5}, Vec3{7.5, 1, 8.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 2 {
		t.Fatalf("expected geometry fallback to supplement partial stored portals, got %+v", path)
	}
	if path.Steps[0].Coord != centerCoord || path.Steps[0].PolygonID != "center" || path.Steps[1].Coord != northCoord || path.Steps[1].PolygonID != "north" {
		t.Fatalf("unexpected path steps: %+v", path.Steps)
	}
}

func TestFindEffectiveNavPathFallsBackWhenCurrentPolygonHasNoStoredPortal(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_1_0_0.gknavtile")
	leftTile := &NavTileDef{
		NavID:          "nav-path",
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          leftCoord,
		AgentProfileID: "tiny",
		BuilderVersion: DefaultNavBuilderVersion,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{8, 8, 8},
		Vertices: []Vec3{
			{7, 1, 0}, {8, 1, 0}, {8, 1, 1}, {7, 1, 1},
			{7, 1, 2}, {8, 1, 2}, {8, 1, 3}, {7, 1, 3},
		},
		Polygons: []NavPolygonDef{
			{ID: "start", Vertices: []int{0, 1, 2, 3}, Area: NavTraversalWalk},
			{ID: "other", Vertices: []int{4, 5, 6, 7}, Area: NavTraversalWalk},
		},
		Portals: []NavPortalDef{{
			ID:            "portal:other",
			FromPolygonID: "other",
			ToTileCoord:   rightCoord,
			ToPolygonID:   "other_right",
			Start:         Vec3{8, 1, 2},
			End:           Vec3{8, 1, 3},
			Area:          NavTraversalWalk,
		}},
	}
	rightTile := &NavTileDef{
		NavID:          "nav-path",
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          rightCoord,
		AgentProfileID: "tiny",
		BuilderVersion: DefaultNavBuilderVersion,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{8, 0, 0},
		BoundsMax:      [3]float32{16, 8, 8},
		Vertices: []Vec3{
			{8, 1, 0}, {9, 1, 0}, {9, 1, 1}, {8, 1, 1},
			{8, 1, 2}, {9, 1, 2}, {9, 1, 3}, {8, 1, 3},
		},
		Polygons: []NavPolygonDef{
			{ID: "target", Vertices: []int{0, 1, 2, 3}, Area: NavTraversalWalk},
			{ID: "other_right", Vertices: []int{4, 5, 6, 7}, Area: NavTraversalWalk},
		},
	}
	if err := SaveNavTile(leftPath, leftTile); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(rightPath, rightTile); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{7.5, 1, 0.5}, Vec3{8.5, 1, 0.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 2 {
		t.Fatalf("expected per-polygon fallback path despite unrelated stored portal, got %+v", path)
	}
	if path.Steps[0].PolygonID != "start" || path.Steps[1].PolygonID != "target" {
		t.Fatalf("unexpected path steps: %+v", path.Steps)
	}
}

func TestFindEffectiveNavPathFallsBackWhenCurrentPortalIsUnusable(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_1_0_0.gknavtile")
	leftTile := navPathTestTile("nav-path", "tiny", leftCoord, "left", Vec3{7, 1, 0}, Vec3{8, 1, 1})
	leftTile.Portals = []NavPortalDef{{
		ID:            "portal:left:missing",
		FromPolygonID: "left",
		ToTileCoord:   rightCoord,
		ToPolygonID:   "missing",
		Start:         Vec3{8, 1, 0},
		End:           Vec3{8, 1, 1},
		Area:          NavTraversalWalk,
	}}
	rightTile := navPathTestTile("nav-path", "tiny", rightCoord, "right", Vec3{8, 1, 0}, Vec3{9, 1, 1})
	if err := SaveNavTile(leftPath, leftTile); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(rightPath, rightTile); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{7.5, 1, 0.5}, Vec3{8.5, 1, 0.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 2 {
		t.Fatalf("expected geometry fallback path despite unusable stored portal, got %+v", path)
	}
	if path.Steps[0].PolygonID != "left" || path.Steps[1].PolygonID != "right" {
		t.Fatalf("unexpected path steps: %+v", path.Steps)
	}
}

func TestFindEffectiveNavPathHonorsAllowedTileCoords(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	middleCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 2, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_0_0_0.gknavtile")
	middlePath := filepath.Join(root, "worlds", "path_navtiles", "tiny_1_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "path_navtiles", "tiny_2_0_0.gknavtile")
	if err := SaveNavTile(leftPath, navPathTestTile("nav-path", "tiny", leftCoord, "left", Vec3{7, 1, 0}, Vec3{8, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := SaveNavTile(middlePath, navPathTestTile("nav-path", "tiny", middleCoord, "middle", Vec3{8, 1, 0}, Vec3{16, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile middle failed: %v", err)
	}
	if err := SaveNavTile(rightPath, navPathTestTile("nav-path", "tiny", rightCoord, "right", Vec3{16, 1, 0}, Vec3{17, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-path", "tiny", navPath, map[TerrainChunkCoordDef]string{
		leftCoord:   leftPath,
		middleCoord: middlePath,
		rightCoord:  rightPath,
	})

	unrestricted, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{7.5, 1, 0.5}, Vec3{16.5, 1, 0.5}, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 2})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath unrestricted failed: %v", err)
	}
	if !unrestricted.Found || len(unrestricted.Steps) != 3 {
		t.Fatalf("expected unrestricted path through middle tile, got %+v", unrestricted)
	}

	restricted, err := FindEffectiveNavPath(baseNav, navPath, nil, "", Vec3{7.5, 1, 0.5}, Vec3{16.5, 1, 0.5}, NavPathOptions{
		AgentProfileID:      "tiny",
		MaxTileSearchRadius: 2,
		AllowedTileCoords: map[TerrainChunkCoordDef]struct{}{
			leftCoord:  {},
			rightCoord: {},
		},
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath restricted failed: %v", err)
	}
	if restricted.Found {
		t.Fatalf("did not expect restricted path through disallowed middle tile, got %+v", restricted)
	}
	if restricted.FailureReason != NavPathFailureDisallowedTile || restricted.FailureCoord != middleCoord {
		t.Fatalf("expected disallowed-tile failure at middle tile, got %+v", restricted)
	}
}

func TestFindEffectiveNavPathStringPullsAcrossTriangleFan(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "fan.gknav")
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	tilePath := filepath.Join(root, "worlds", "fan_navtiles", "tiny_0_0_0.gknavtile")
	tile := &NavTileDef{
		NavID:          "nav-fan",
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          coord,
		AgentProfileID: "tiny",
		BuilderVersion: DefaultNavBuilderVersion,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{8, 2, 8},
		Vertices: []Vec3{
			{0, 1, 0},
			{8, 1, 0},
			{8, 1, 8},
			{0, 1, 8},
			{4, 1, 4},
		},
		Polygons: []NavPolygonDef{
			{ID: "bottom", Vertices: []int{0, 1, 4}, Area: NavTraversalWalk, Neighbors: []string{"left", "right"}},
			{ID: "right", Vertices: []int{1, 2, 4}, Area: NavTraversalWalk, Neighbors: []string{"bottom", "top"}},
			{ID: "top", Vertices: []int{2, 3, 4}, Area: NavTraversalWalk, Neighbors: []string{"left", "right"}},
			{ID: "left", Vertices: []int{3, 0, 4}, Area: NavTraversalWalk, Neighbors: []string{"bottom", "top"}},
		},
	}
	if err := SaveNavTile(tilePath, tile); err != nil {
		t.Fatalf("SaveNavTile failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-fan", "tiny", navPath, map[TerrainChunkCoordDef]string{
		coord: tilePath,
	})
	start := Vec3{6.5, 1, 0.5}
	end := Vec3{6.5, 1, 7.5}

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", start, end, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}

	if !path.Found || len(path.Steps) != 3 {
		t.Fatalf("expected fan corridor through three triangles, got %+v", path)
	}
	if len(path.Portals) != 2 {
		t.Fatalf("expected fan corridor portals, got %+v", path.Portals)
	}
	if len(path.Waypoints) != 2 || path.Waypoints[0] != start || path.Waypoints[1] != end {
		t.Fatalf("expected string-pulled effective path waypoints, got %+v", path.Waypoints)
	}
}

func TestFindEffectiveNavPathPreservesRampTransitionWaypoints(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "ramp.gknav")
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	tilePath := filepath.Join(root, "worlds", "ramp_navtiles", "tiny_0_0_0.gknavtile")
	tile := &NavTileDef{
		NavID:          "nav-ramp",
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          coord,
		AgentProfileID: "tiny",
		BuilderVersion: DefaultNavBuilderVersion,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{8, 4, 8},
		Vertices: []Vec3{
			{0, 3, 0}, {8, 3, 0}, {8, 3, 3}, {0, 3, 3},
			{0, 0, 5}, {8, 0, 5}, {8, 0, 8}, {0, 0, 8},
			{6, 3, 3}, {8, 3, 3}, {8, 0, 5}, {6, 0, 5},
		},
		Polygons: []NavPolygonDef{
			{ID: "plateau", Vertices: []int{0, 1, 2, 3}, Area: NavTraversalWalk, Neighbors: []string{"ramp"}},
			{ID: "ramp", Vertices: []int{8, 9, 10, 11}, Area: NavTraversalRamp, Neighbors: []string{"plateau", "lower"}},
			{ID: "lower", Vertices: []int{4, 5, 6, 7}, Area: NavTraversalWalk, Neighbors: []string{"ramp"}},
		},
	}
	if err := SaveNavTile(tilePath, tile); err != nil {
		t.Fatalf("SaveNavTile failed: %v", err)
	}
	baseNav := navPathTestManifest("nav-ramp", "tiny", navPath, map[TerrainChunkCoordDef]string{
		coord: tilePath,
	})
	start := Vec3{1, 3, 1}
	end := Vec3{1, 0, 7}

	path, err := FindEffectiveNavPath(baseNav, navPath, nil, "", start, end, NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}

	if !path.Found || len(path.Steps) != 3 || path.Steps[1].PolygonID != "ramp" {
		t.Fatalf("expected corridor through ramp, got %+v", path)
	}
	if len(path.Waypoints) < 4 {
		t.Fatalf("expected ramp transition waypoints to be preserved, got %+v", path.Waypoints)
	}
	if path.Waypoints[0] != start || path.Waypoints[len(path.Waypoints)-1] != end {
		t.Fatalf("expected path to keep exact start/end, got %+v", path.Waypoints)
	}
	foundRampEntry := false
	foundRampExit := false
	for _, waypoint := range path.Waypoints {
		foundRampEntry = foundRampEntry || navAlmostEqual(waypoint[2], 3, 1e-4) && waypoint[1] > 2
		foundRampExit = foundRampExit || navAlmostEqual(waypoint[2], 5, 1e-4) && waypoint[1] < 1
	}
	if !foundRampEntry || !foundRampExit {
		t.Fatalf("expected waypoints at ramp entry and exit, got %+v", path.Waypoints)
	}
}

func navPathTestManifest(navID string, profileID string, navPath string, tilePaths map[TerrainChunkCoordDef]string) *NavManifestDef {
	manifest := &NavManifestDef{
		NavID:           navID,
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		BuilderVersion:  DefaultNavBuilderVersion,
		ChunkSize:       8,
		VoxelResolution: 1,
		AgentProfiles: []NavAgentProfileDef{{
			ID:              profileID,
			Radius:          0.2,
			Height:          1,
			StepHeight:      0.5,
			MaxSlopeDegrees: 45,
		}},
	}
	for coord, path := range tilePaths {
		manifest.Tiles = append(manifest.Tiles, NavTileEntryDef{
			Coord:          coord,
			AgentProfileID: profileID,
			TilePath:       AuthorDocumentPath(path, navPath),
			BoundsMin:      [3]float32{float32(coord.X) * 8, float32(coord.Y) * 8, float32(coord.Z) * 8},
			BoundsMax:      [3]float32{float32(coord.X+1) * 8, float32(coord.Y+1) * 8, float32(coord.Z+1) * 8},
		})
	}
	return manifest
}

func navPathTestTile(navID string, profileID string, coord TerrainChunkCoordDef, polygonID string, min Vec3, max Vec3) *NavTileDef {
	return &NavTileDef{
		NavID:          navID,
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          coord,
		AgentProfileID: profileID,
		BuilderVersion: DefaultNavBuilderVersion,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{float32(coord.X) * 8, float32(coord.Y) * 8, float32(coord.Z) * 8},
		BoundsMax:      [3]float32{float32(coord.X+1) * 8, float32(coord.Y+1) * 8, float32(coord.Z+1) * 8},
		Vertices: []Vec3{
			{min[0], min[1], min[2]},
			{max[0], min[1], min[2]},
			{max[0], min[1], max[2]},
			{min[0], min[1], max[2]},
		},
		Polygons: []NavPolygonDef{{
			ID:       polygonID,
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}
}
