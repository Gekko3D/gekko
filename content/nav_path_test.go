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
