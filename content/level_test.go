package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLevelRoundTripPreservesSchemaAndIDs(t *testing.T) {
	level := &LevelDef{
		Name:            "Station",
		ChunkSize:       48,
		StreamingRadius: 6,
		Tags:            []string{"space"},
		Terrain: &LevelTerrainDef{
			Kind:       TerrainKindHeightfield,
			SourcePath: "terrain/height.png",
		},
		Environment: &LevelEnvironmentDef{
			Preset: "orbit",
			Tags:   []string{"placeholder"},
		},
		Placements: []LevelPlacementDef{
			{
				AssetPath:     "assets/station.gkasset",
				PlacementMode: LevelPlacementModePlaneSnap,
				Transform: LevelTransformDef{
					Position: Vec3{1, 2, 3},
					Rotation: Quat{0, 0, 0, 1},
					Scale:    Vec3{1, 1, 1},
				},
			},
		},
		Markers: []LevelMarkerDef{
			{
				Name: "spawn",
				Kind: "player_spawn",
				Transform: LevelTransformDef{
					Rotation: Quat{0, 0, 0, 1},
					Scale:    Vec3{1, 1, 1},
				},
			},
		},
	}

	tmpDir, err := os.MkdirTemp("", "content_level_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	assetPath := filepath.Join(tmpDir, "assets", "station.gkasset")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	terrainSourcePath := filepath.Join(tmpDir, "assets", "terrain", "station.gkterrain")
	if err := os.MkdirAll(filepath.Dir(terrainSourcePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := SaveTerrainSource(terrainSourcePath, &TerrainSourceDef{
		ID:              "terrain-1",
		SchemaVersion:   CurrentTerrainSchemaVersion,
		Name:            "station_terrain",
		Kind:            TerrainKindHeightfield,
		SampleWidth:     2,
		SampleHeight:    2,
		HeightSamples:   []uint16{0, 1, 2, 3},
		WorldSize:       Vec2{32, 32},
		HeightScale:     16,
		VoxelResolution: 1,
		ChunkSize:       16,
	}); err != nil {
		t.Fatalf("SaveTerrainSource failed: %v", err)
	}
	level.Terrain.SourcePath = filepath.Join("terrain", "station.gkterrain")

	path := filepath.Join(tmpDir, "assets", "station.gklevel")
	if err := SaveLevel(path, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	loaded, err := LoadLevel(path)
	if err != nil {
		t.Fatalf("LoadLevel failed: %v", err)
	}

	if loaded.SchemaVersion != CurrentLevelSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", CurrentLevelSchemaVersion, loaded.SchemaVersion)
	}
	if loaded.ID == "" || loaded.Placements[0].ID == "" || loaded.Markers[0].ID == "" {
		t.Fatal("expected IDs to be assigned")
	}
	if loaded.Placements[0].PlacementMode != LevelPlacementModePlaneSnap {
		t.Fatalf("expected plane snap mode, got %q", loaded.Placements[0].PlacementMode)
	}
	if loaded.Environment == nil || loaded.Environment.Preset != "orbit" {
		t.Fatalf("expected placeholder environment to round-trip, got %+v", loaded.Environment)
	}
}

func TestLevelJSONUsesStringPlacementModes(t *testing.T) {
	level := NewLevelDef("Mode Test")
	level.Placements = []LevelPlacementDef{{
		AssetPath:     "assets/ship.gkasset",
		PlacementMode: LevelPlacementModeFree3D,
		Transform: LevelTransformDef{
			Rotation: Quat{0, 0, 0, 1},
			Scale:    Vec3{1, 1, 1},
		},
	}}

	data, err := json.Marshal(level)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if !strings.Contains(string(data), `"placement_mode":"free_3d"`) {
		t.Fatalf("expected string placement mode in JSON, got %s", string(data))
	}
}

func TestLoadLevelRejectsUnknownSchemaVersion(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "content_level_invalid_schema")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "bad.gklevel")
	if err := os.WriteFile(path, []byte(`{"id":"1","schema_version":99,"name":"bad"}`), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadLevel(path); err == nil {
		t.Fatal("expected LoadLevel to reject unsupported schema_version")
	}
}

func TestValidateLevelRejectsInvalidPlacements(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "content_level_validate")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	def := NewLevelDef("")
	def.Placements = []LevelPlacementDef{
		{
			ID:            "dup",
			AssetPath:     "",
			PlacementMode: LevelPlacementMode("bad_mode"),
		},
		{
			ID:            "dup",
			AssetPath:     "assets/missing.gkasset",
			PlacementMode: LevelPlacementModePlaneSnap,
		},
	}

	result := ValidateLevel(def, LevelValidationOptions{DocumentPath: filepath.Join(tmpDir, "assets", "level.gklevel")})
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	assertHasLevelValidationCode(t, result, "empty_name")
	assertHasLevelValidationCode(t, result, "duplicate_id")
	assertHasLevelValidationCode(t, result, "empty_asset_path")
	assertHasLevelValidationCode(t, result, "invalid_placement_mode")
	assertHasLevelValidationCode(t, result, "missing_asset_file")
}

func TestValidateLevelRejectsBrokenTerrainReference(t *testing.T) {
	tmpDir := t.TempDir()
	def := NewLevelDef("terrain")
	def.Terrain = &LevelTerrainDef{
		Kind:       TerrainKindHeightfield,
		SourcePath: "missing.gkterrain",
	}

	result := ValidateLevel(def, LevelValidationOptions{DocumentPath: filepath.Join(tmpDir, "assets", "level.gklevel")})
	if !result.HasErrors() {
		t.Fatal("expected terrain validation errors")
	}
	assertHasLevelValidationCode(t, result, "missing_terrain_source_file")
}

func assertHasLevelValidationCode(t *testing.T, result LevelValidationResult, want string) {
	t.Helper()
	for _, issue := range result.Issues {
		if issue.Code == want {
			return
		}
	}
	t.Fatalf("expected validation code %q, got %+v", want, result.Issues)
}
