package content

import (
	"path/filepath"
	"testing"
)

func TestLoadEffectiveNavTileFallsBackToStaticTile(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "demo.gknav")
	staticTilePath := filepath.Join(root, "worlds", "demo_navtiles", "tiny_0_0_0.gknavtile")
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	staticTile := navLookupTestTile("nav-demo", "tiny", coord, "static-build", "")
	if err := SaveNavTile(staticTilePath, staticTile); err != nil {
		t.Fatalf("SaveNavTile static failed: %v", err)
	}
	baseNav := navLookupTestManifest("nav-demo", "tiny", coord, AuthorDocumentPath(staticTilePath, navPath))

	result, err := LoadEffectiveNavTile(baseNav, navPath, nil, "", coord, "tiny")
	if err != nil {
		t.Fatalf("LoadEffectiveNavTile failed: %v", err)
	}
	if !result.Found || result.Empty || result.Source != NavTileLookupSourceStatic || result.Tile == nil {
		t.Fatalf("expected static tile result, got %+v", result)
	}
	if result.Tile.NavBuildHash != "static-build" || result.TilePath != staticTilePath || result.StaticEntry == nil || result.DeltaOverride != nil {
		t.Fatalf("unexpected static tile lookup result: %+v tile=%+v", result, result.Tile)
	}
}

func TestLoadEffectiveNavTilePrefersDeltaOverride(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "demo.gknav")
	deltaPath := filepath.Join(root, "levels", "demo.gkworlddelta")
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	staticTilePath := filepath.Join(root, "worlds", "demo_navtiles", "tiny_0_0_0.gknavtile")
	deltaTilePath := filepath.Join(root, "levels", "demo.gkworlddelta_data", "nav", "nav-demo", "tiny", "tiny_0_0_0.gknavtile")
	if err := SaveNavTile(staticTilePath, navLookupTestTile("nav-demo", "tiny", coord, "static-build", "")); err != nil {
		t.Fatalf("SaveNavTile static failed: %v", err)
	}
	if err := SaveNavTile(deltaTilePath, navLookupTestTile("nav-demo", "tiny", coord, "delta-build", "delta-source")); err != nil {
		t.Fatalf("SaveNavTile delta failed: %v", err)
	}
	baseNav := navLookupTestManifest("nav-demo", "tiny", coord, AuthorDocumentPath(staticTilePath, navPath))
	delta := &WorldDeltaDef{
		LevelID: "level-a",
		NavigationTileOverrides: []NavigationTileOverrideDef{{
			NavID:              "nav-demo",
			AgentProfileID:     "tiny",
			ChunkCoord:         coord,
			TilePath:           authorPathRelativeToDocument(deltaTilePath, deltaPath),
			SourceDeltaHash:    "delta-source",
			NavBuildHash:       "delta-build",
			SourceOverrideKind: NavSourceOverrideKindImportedWorld,
		}},
	}

	result, err := LoadEffectiveNavTile(baseNav, navPath, delta, deltaPath, coord, "tiny")
	if err != nil {
		t.Fatalf("LoadEffectiveNavTile failed: %v", err)
	}
	if !result.Found || result.Empty || result.Source != NavTileLookupSourceDelta || result.Tile == nil {
		t.Fatalf("expected delta tile result, got %+v", result)
	}
	if result.Tile.NavBuildHash != "delta-build" || result.Tile.SourceDeltaHash != "delta-source" || result.TilePath != deltaTilePath || result.DeltaOverride == nil || result.StaticEntry != nil {
		t.Fatalf("unexpected delta tile lookup result: %+v tile=%+v", result, result.Tile)
	}
}

func TestLoadEffectiveNavTileHonorsEmptyDeltaOverride(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "demo.gknav")
	deltaPath := filepath.Join(root, "levels", "demo.gkworlddelta")
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	staticTilePath := filepath.Join(root, "worlds", "demo_navtiles", "tiny_0_0_0.gknavtile")
	if err := SaveNavTile(staticTilePath, navLookupTestTile("nav-demo", "tiny", coord, "static-build", "")); err != nil {
		t.Fatalf("SaveNavTile static failed: %v", err)
	}
	baseNav := navLookupTestManifest("nav-demo", "tiny", coord, AuthorDocumentPath(staticTilePath, navPath))
	delta := &WorldDeltaDef{
		LevelID: "level-a",
		NavigationTileOverrides: []NavigationTileOverrideDef{{
			NavID:          "nav-demo",
			AgentProfileID: "tiny",
			ChunkCoord:     coord,
			Empty:          true,
			NavBuildHash:   "empty-build",
		}},
	}

	result, err := LoadEffectiveNavTile(baseNav, navPath, delta, deltaPath, coord, "tiny")
	if err != nil {
		t.Fatalf("LoadEffectiveNavTile failed: %v", err)
	}
	if !result.Found || !result.Empty || result.Source != NavTileLookupSourceDelta || result.Tile != nil || result.TilePath != "" || result.DeltaOverride == nil {
		t.Fatalf("expected authoritative empty delta result, got %+v", result)
	}
}

func TestResolveEffectiveNavTileReportsMissingTile(t *testing.T) {
	baseNav := navLookupTestManifest("nav-demo", "tiny", TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, "tiles/tiny_0_0_0.gknavtile")
	result, err := ResolveEffectiveNavTile(baseNav, "demo.gknav", nil, "", TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}, "tiny")
	if err != nil {
		t.Fatalf("ResolveEffectiveNavTile failed: %v", err)
	}
	if result.Found || result.Tile != nil || result.Empty {
		t.Fatalf("expected missing tile result, got %+v", result)
	}
}

func navLookupTestManifest(navID string, profileID string, coord TerrainChunkCoordDef, tilePath string) *NavManifestDef {
	return &NavManifestDef{
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
		Tiles: []NavTileEntryDef{{
			Coord:          coord,
			AgentProfileID: profileID,
			TilePath:       tilePath,
			BoundsMin:      [3]float32{0, 0, 0},
			BoundsMax:      [3]float32{8, 8, 8},
		}},
	}
}

func navLookupTestTile(navID string, profileID string, coord TerrainChunkCoordDef, buildHash string, deltaHash string) *NavTileDef {
	return &NavTileDef{
		NavID:           navID,
		SchemaVersion:   CurrentNavTileSchemaVersion,
		Coord:           coord,
		AgentProfileID:  profileID,
		BuilderVersion:  DefaultNavBuilderVersion,
		PayloadKind:     NavTilePayloadJSONV1,
		SourceDeltaHash: deltaHash,
		NavBuildHash:    buildHash,
		BoundsMin:       [3]float32{0, 0, 0},
		BoundsMax:       [3]float32{8, 8, 8},
		Vertices: []Vec3{
			{0, 1, 0},
			{1, 1, 0},
			{1, 1, 1},
			{0, 1, 1},
		},
		Polygons: []NavPolygonDef{{
			ID:       "cell:0:1:0",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}
}
