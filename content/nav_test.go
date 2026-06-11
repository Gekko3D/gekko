package content

import (
	"encoding/json"
	"os"
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
		Portals: []NavPortalDef{{
			ID:            "portal-1",
			FromPolygonID: "poly-1",
			ToTileCoord:   TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
			ToPolygonID:   "poly-2",
			Start:         Vec3{1, 0, 0},
			End:           Vec3{1, 0, 1},
			Area:          NavTraversalWalk,
		}},
		OffMeshLinks: []NavOffMeshLinkDef{{
			ID:            "ladder-1",
			Kind:          NavTraversalLadder,
			FromPolygonID: "poly-1",
			ToTileCoord:   TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ToPolygonID:   "poly-1",
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
	if len(loadedTile.Polygons) != 1 || len(loadedTile.Portals) != 1 || len(loadedTile.OffMeshLinks) != 1 {
		t.Fatalf("expected tile geometry and links to round-trip, got %+v", loadedTile)
	}
	if loadedTile.OffMeshLinks[0].FromPolygonID != "poly-1" || loadedTile.OffMeshLinks[0].ToTileCoord != (TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}) || loadedTile.OffMeshLinks[0].ToPolygonID != "poly-1" {
		t.Fatalf("expected off-mesh link refs to round-trip, got %+v", loadedTile.OffMeshLinks[0])
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
		Portals: []NavPortalDef{{
			ID:            "bad-portal",
			FromPolygonID: "missing",
			ToTileCoord:   TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
			ToPolygonID:   "remote",
		}},
	}

	result := ValidateNavTile(tile)
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	assertHasNavValidationCode(t, result, "invalid_polygon_vertex_ref")
	assertHasNavValidationCode(t, result, "invalid_off_mesh_link_radius")
	assertHasNavValidationCode(t, result, "missing_portal_from_polygon")
	assertHasNavValidationCode(t, result, "invalid_portal_segment")
}

func TestValidateNavTileRejectsOutOfBoundsPolygonVertices(t *testing.T) {
	tile := &NavTileDef{
		NavID:          "nav-bad",
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{1, 1, 1},
		Vertices: []Vec3{
			{0, 0, 0},
			{2, 0, 0},
			{0, 0, 1},
		},
		Polygons: []NavPolygonDef{{
			ID:       "escaped",
			Vertices: []int{0, 1, 2},
			Area:     NavTraversalWalk,
		}},
	}

	result := ValidateNavTile(tile)
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	assertHasNavValidationCode(t, result, "invalid_polygon_vertex_bounds")
}

func TestValidateNavTileRejectsOverlappingPolygons(t *testing.T) {
	tile := &NavTileDef{
		NavID:          "nav-overlap",
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{4, 1, 4},
		Vertices: []Vec3{
			{0, 0, 0}, {3, 0, 0}, {3, 0, 3}, {0, 0, 3},
			{1, 0, 1}, {4, 0, 1}, {4, 0, 4}, {1, 0, 4},
		},
		Polygons: []NavPolygonDef{
			{ID: "a", Vertices: []int{0, 1, 2, 3}, Area: NavTraversalWalk},
			{ID: "b", Vertices: []int{4, 5, 6, 7}, Area: NavTraversalWalk},
		},
	}

	result := ValidateNavTile(tile)
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	assertHasNavValidationCode(t, result, "overlapping_nav_polygons")
}

func TestSaveNavTileRejectsInvalidTile(t *testing.T) {
	root := t.TempDir()
	tilePath := filepath.Join(root, "bad.gknavtile")
	tile := &NavTileDef{
		NavID:          "nav-bad",
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{1, 1, 1},
		Vertices: []Vec3{
			{0, 0, 0},
			{2, 0, 0},
			{0, 0, 1},
		},
		Polygons: []NavPolygonDef{{
			ID:       "escaped",
			Vertices: []int{0, 1, 2},
			Area:     NavTraversalWalk,
		}},
	}

	if err := SaveNavTile(tilePath, tile); err == nil {
		t.Fatal("expected SaveNavTile to reject invalid tile")
	}
}

func TestValidateNavManifestRejectsInvalidTileFileContents(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "nav", "demo.gknav")
	tilePath := filepath.Join(root, "nav", "tiles", "bad.gknavtile")
	tile := &NavTileDef{
		NavID:          "nav-bad",
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BuilderVersion: DefaultNavBuilderVersion,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{1, 1, 1},
		Vertices: []Vec3{
			{0, 0, 0},
			{2, 0, 0},
			{0, 0, 1},
		},
		Polygons: []NavPolygonDef{{
			ID:       "escaped",
			Vertices: []int{0, 1, 2},
			Area:     NavTraversalWalk,
		}},
	}
	data, err := json.MarshalIndent(tile, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(tilePath), 0755); err != nil {
		t.Fatalf("os.MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(tilePath, data, 0644); err != nil {
		t.Fatalf("os.WriteFile failed: %v", err)
	}
	manifest := NewNavManifestDef("nav-bad")
	manifest.Tiles = []NavTileEntryDef{{
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		TilePath:       AuthorDocumentPath(tilePath, manifestPath),
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{1, 1, 1},
	}}

	result := ValidateNavManifest(manifest, NavValidationOptions{DocumentPath: manifestPath})
	if !result.HasErrors() {
		t.Fatal("expected manifest validation to reject invalid tile contents")
	}
	assertHasNavValidationCode(t, result, "invalid_polygon_vertex_bounds")
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
