package content

import (
	"path/filepath"
	"testing"
)

func TestNavManifestAndTileRoundTrip(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "nav", "demo.gknav")
	tilePath := filepath.Join(root, "nav", "tiles", "0_0_0.gknavtile")

	tile := &NavTileDef{
		NavID:             "nav-demo",
		Coord:             TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID:    DefaultNavAgentProfileID,
		SourcePayloadHash: "chunk-hash",
		NavBuildHash:      "nav-hash",
		BoundsMin:         [3]float32{0, 0, 0},
		BoundsMax:         [3]float32{16, 16, 16},
		Vertices: []Vec3{
			{0, 0, 0},
			{1, 0, 0},
			{1, 0, 1},
			{0, 0, 1},
		},
		Polygons: []NavPolygonDef{{
			ID:       "poly-1",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
		OffMeshLinks: []NavOffMeshLinkDef{{
			ID:            "ladder-1",
			Kind:          NavTraversalLadder,
			Start:         Vec3{0.5, 0, 0.5},
			End:           Vec3{0.5, 2, 0.5},
			Bidirectional: true,
		}},
	}
	if err := SaveNavTile(tilePath, tile); err != nil {
		t.Fatalf("SaveNavTile failed: %v", err)
	}

	manifest := &NavManifestDef{
		NavID:           "nav-demo",
		LevelID:         "level-a",
		SourceWorldID:   "world-a",
		SourceLevelHash: "level-hash",
		ChunkSize:       16,
		VoxelResolution: 1,
		AgentProfiles:   []NavAgentProfileDef{DefaultHL1NavAgentProfile()},
		Tiles: []NavTileEntryDef{{
			Coord:             TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			AgentProfileID:    DefaultNavAgentProfileID,
			TilePath:          AuthorDocumentPath(tilePath, manifestPath),
			SourcePayloadHash: "chunk-hash",
			NavBuildHash:      "nav-hash",
			BoundsMin:         [3]float32{0, 0, 0},
			BoundsMax:         [3]float32{16, 16, 16},
		}},
		Sectors: []NavSectorEntryDef{{
			Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			BoundsMin:          [3]float32{0, 0, 0},
			BoundsMax:          [3]float32{16, 16, 16},
			AdjacentSectorRefs: []TerrainChunkCoordDef{{X: 1, Y: 0, Z: 0}},
			Links: []NavSectorLinkDef{{
				ID:       "door-1",
				To:       TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
				Kind:     NavTraversalDoor,
				Cost:     2,
				Openable: true,
			}},
		}, {
			Coord:              TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
			BoundsMin:          [3]float32{16, 0, 0},
			BoundsMax:          [3]float32{32, 16, 16},
			AdjacentSectorRefs: []TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}},
		}},
	}
	if err := SaveNavManifest(manifestPath, manifest); err != nil {
		t.Fatalf("SaveNavManifest failed: %v", err)
	}

	loadedManifest, err := LoadNavManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadNavManifest failed: %v", err)
	}
	if loadedManifest.SchemaVersion != CurrentNavManifestSchemaVersion {
		t.Fatalf("expected manifest schema version %d, got %d", CurrentNavManifestSchemaVersion, loadedManifest.SchemaVersion)
	}
	if len(loadedManifest.AgentProfiles) != 1 || loadedManifest.AgentProfiles[0].ID != DefaultNavAgentProfileID {
		t.Fatalf("expected agent profile to round-trip, got %+v", loadedManifest.AgentProfiles)
	}
	if len(loadedManifest.Tiles) != 1 || loadedManifest.Tiles[0].PayloadKind != NavTilePayloadJSONV1 {
		t.Fatalf("expected tile entry defaults to round-trip, got %+v", loadedManifest.Tiles)
	}
	if validation := ValidateNavManifest(loadedManifest, NavValidationOptions{DocumentPath: manifestPath}); validation.HasErrors() {
		t.Fatalf("ValidateNavManifest failed: %s", validation.Error())
	}

	loadedTile, err := LoadNavTile(tilePath)
	if err != nil {
		t.Fatalf("LoadNavTile failed: %v", err)
	}
	if loadedTile.SchemaVersion != CurrentNavTileSchemaVersion || loadedTile.PayloadKind != NavTilePayloadJSONV1 {
		t.Fatalf("expected tile defaults, got %+v", loadedTile)
	}
	if len(loadedTile.Polygons) != 1 || len(loadedTile.OffMeshLinks) != 1 {
		t.Fatalf("expected tile geometry and links to round-trip, got %+v", loadedTile)
	}
	if validation := ValidateNavTile(loadedTile); validation.HasErrors() {
		t.Fatalf("ValidateNavTile failed: %s", validation.Error())
	}
}

func TestValidateNavManifestRejectsBrokenReferences(t *testing.T) {
	def := NewNavManifestDef("nav-bad")
	def.AgentProfiles = []NavAgentProfileDef{{
		ID:              "small",
		Radius:          0.25,
		Height:          1,
		StepHeight:      0.25,
		MaxSlopeDegrees: 45,
	}}
	def.Tiles = []NavTileEntryDef{{
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: "missing",
		TilePath:       "tile.txt",
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{1, 1, 1},
	}, {
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: "missing",
		TilePath:       "other.gknavtile",
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{1, 1, 1},
	}}
	def.Sectors = []NavSectorEntryDef{{
		Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		BoundsMin:          [3]float32{0, 0, 0},
		BoundsMax:          [3]float32{1, 1, 1},
		AdjacentSectorRefs: []TerrainChunkCoordDef{{X: 2, Y: 0, Z: 0}},
	}}

	result := ValidateNavManifest(def, NavValidationOptions{})
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	assertHasNavValidationCode(t, result, "missing_tile_agent_profile")
	assertHasNavValidationCode(t, result, "invalid_tile_path")
	assertHasNavValidationCode(t, result, "duplicate_tile_entry")
	assertHasNavValidationCode(t, result, "missing_sector_ref")
}

func TestValidateNavTileRejectsBrokenPolygonRefs(t *testing.T) {
	tile := &NavTileDef{
		NavID:          "nav-bad",
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{1, 1, 1},
		Vertices:       []Vec3{{0, 0, 0}},
		Polygons: []NavPolygonDef{{
			Vertices: []int{0, 1, 2},
		}},
		OffMeshLinks: []NavOffMeshLinkDef{{
			ID:     "bad-link",
			Kind:   NavTraversalJump,
			Radius: -1,
		}},
	}

	result := ValidateNavTile(tile)
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	assertHasNavValidationCode(t, result, "invalid_polygon_vertex_ref")
	assertHasNavValidationCode(t, result, "invalid_off_mesh_link_radius")
}

func assertHasNavValidationCode(t *testing.T, result NavValidationResult, code string) {
	t.Helper()
	for _, issue := range result.Issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected nav validation code %q in %+v", code, result.Issues)
}
