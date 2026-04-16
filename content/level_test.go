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
		Name:      "Station",
		ChunkSize: 48,
		Tags:      []string{"space"},
		Materials: []LevelMaterialDef{{
			ID:           "mat_wall",
			Name:         "Wall",
			BaseColor:    [4]uint8{180, 190, 210, 255},
			Roughness:    0.6,
			Metallic:     0.1,
			Emissive:     0,
			IOR:          1.4,
			Transparency: 0,
		}},
		BrushLayers: []LevelBrushLayerDef{{
			ID:   "layer-1",
			Name: "Blockout",
			Brushes: []LevelBrushDef{{
				ID:         "brush-1",
				Name:       "solid",
				Primitive:  "cube",
				Params:     map[string]float32{"sx": 4, "sy": 2, "sz": 6},
				MaterialID: "mat_wall",
				Operation:  AssetShapeOperationAdd,
				Transform: LevelTransformDef{
					Position: Vec3{2, 0, 1},
					Rotation: Quat{0, 0, 0, 1},
					Scale:    Vec3{1, 1, 1},
				},
				Tags: []string{"blockout"},
			}},
		}},
		Terrain: &LevelTerrainDef{
			Kind:       TerrainKindHeightfield,
			SourcePath: "assets/heightmap.png",
		},
		Environment: &LevelEnvironmentDef{
			Preset: "orbit",
			Tags:   []string{"placeholder"},
		},
		BaseWorld: &LevelBaseWorldDef{
			Kind:              ImportedWorldKindVoxelWorld,
			ManifestPath:      "worlds/station.gkworld",
			ReadOnlyByDefault: true,
			CollisionEnabled:  true,
			Tags:              []string{"imported"},
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
		PlacementVolumes: []PlacementVolumeDef{
			{
				ID:        "volume-1",
				Kind:      PlacementVolumeKindSphere,
				AssetPath: "assets/station.gkasset",
				Transform: LevelTransformDef{
					Position: Vec3{4, 5, 6},
					Rotation: Quat{0, 0, 0, 1},
					Scale:    Vec3{1, 1, 1},
				},
				Radius: 12,
				Rule: PlacementVolumeRuleDef{
					Mode:  PlacementVolumeRuleModeCount,
					Count: 24,
				},
				RandomSeed: 99,
				Tags:       []string{"asteroids"},
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
	savedBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if strings.Contains(string(savedBytes), `"streaming_radius"`) {
		t.Fatalf("did not expect streaming_radius in saved level JSON, got %s", string(savedBytes))
	}
	if !strings.Contains(string(savedBytes), `"brush_layers"`) {
		t.Fatalf("expected brush_layers in saved level JSON, got %s", string(savedBytes))
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
	if len(loaded.Materials) != 1 || loaded.Materials[0].ID != "mat_wall" {
		t.Fatalf("expected level material to round-trip, got %+v", loaded.Materials)
	}
	loadedBrushes := LevelBrushes(loaded)
	if len(loadedBrushes) != 1 || loadedBrushes[0].ID != "brush-1" || loadedBrushes[0].MaterialID != "mat_wall" {
		t.Fatalf("expected level brush to round-trip, got %+v", loadedBrushes)
	}
	if EffectiveLevelBrushKind(loadedBrushes[0]) != LevelBrushKindProcedural {
		t.Fatalf("expected procedural brush kind, got %+v", loadedBrushes[0])
	}
	if EffectiveLevelBrushOperation(loadedBrushes[0]) != AssetShapeOperationAdd {
		t.Fatalf("expected brush operation add, got %+v", loadedBrushes[0])
	}
	if len(loaded.PlacementVolumes) != 1 || loaded.PlacementVolumes[0].ID != "volume-1" {
		t.Fatalf("expected placement volume to round-trip, got %+v", loaded.PlacementVolumes)
	}
	if loaded.Placements[0].PlacementMode != LevelPlacementModePlaneSnap {
		t.Fatalf("expected plane snap mode, got %q", loaded.Placements[0].PlacementMode)
	}
	if loaded.Environment == nil || loaded.Environment.Preset != "orbit" {
		t.Fatalf("expected placeholder environment to round-trip, got %+v", loaded.Environment)
	}
	if loaded.BaseWorld == nil || loaded.BaseWorld.Kind != ImportedWorldKindVoxelWorld || loaded.BaseWorld.ManifestPath != "worlds/station.gkworld" {
		t.Fatalf("expected base world to round-trip, got %+v", loaded.BaseWorld)
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
	level.BrushLayers[0].Brushes = []LevelBrushDef{{
		ID:         "brush-1",
		Name:       "cut",
		Primitive:  "cube",
		Params:     map[string]float32{"sx": 1, "sy": 1, "sz": 1},
		MaterialID: "mat_0",
		Operation:  AssetShapeOperationSubtract,
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
	if !strings.Contains(string(data), `"operation":"subtract"`) {
		t.Fatalf("expected brush operation in JSON, got %s", string(data))
	}
}

func TestLevelVoxelShapeBrushRoundTrip(t *testing.T) {
	level := NewLevelDef("Voxel Brush")
	level.Materials = []LevelMaterialDef{{
		ID:   "mat_0",
		Name: "Stone",
	}}
	level.BrushLayers[0].Brushes = []LevelBrushDef{{
		ID:   "brush-voxel",
		Name: "custom",
		Kind: LevelBrushKindVoxelShape,
		VoxelShape: &AssetVoxelShapeDef{
			Palette: []AssetVoxelPaletteEntryDef{{Value: 1, MaterialID: "mat_0"}},
			Voxels: []VoxelObjectVoxelDef{
				{X: 0, Y: 0, Z: 0, Value: 1},
				{X: 1, Y: 0, Z: 0, Value: 1},
			},
		},
		Transform: LevelTransformDef{
			Rotation: Quat{0, 0, 0, 1},
			Scale:    Vec3{1, 1, 1},
		},
	}}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "voxel.gklevel")
	if err := SaveLevel(path, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	loaded, err := LoadLevel(path)
	if err != nil {
		t.Fatalf("LoadLevel failed: %v", err)
	}
	brushes := LevelBrushes(loaded)
	if len(brushes) != 1 || EffectiveLevelBrushKind(brushes[0]) != LevelBrushKindVoxelShape {
		t.Fatalf("expected voxel_shape brush after load, got %+v", brushes)
	}
	if brushes[0].VoxelShape == nil || len(brushes[0].VoxelShape.Voxels) != 2 {
		t.Fatalf("expected voxel payload to round-trip, got %+v", brushes[0].VoxelShape)
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

func TestValidateLevelValidatesBrushesAndMaterials(t *testing.T) {
	def := NewLevelDef("brushes")
	def.Materials = []LevelMaterialDef{{
		ID:           "mat_bad",
		Name:         "Bad",
		Roughness:    2,
		Metallic:     -1,
		Emissive:     -1,
		IOR:          0,
		Transparency: 2,
	}}
	def.BrushLayers[0].Brushes = []LevelBrushDef{
		{
			ID:         "brush-1",
			Name:       "solid",
			Primitive:  "cube",
			Params:     map[string]float32{"sx": 1, "sy": 1, "sz": 1},
			MaterialID: "missing",
			Transform: LevelTransformDef{
				Rotation: Quat{0, 0, 0, 1},
				Scale:    Vec3{1, 1, 1},
			},
		},
		{
			ID:        "brush-2",
			Name:      "bad-op",
			Primitive: "cube",
			Params:    map[string]float32{"sx": 1, "sy": 1, "sz": 1},
			Operation: AssetShapeOperation("replace"),
			Transform: LevelTransformDef{
				Rotation: Quat{0, 0, 0, 1},
				Scale:    Vec3{1, 1, 1},
			},
		},
		{
			ID:        "brush-3",
			Name:      "bad-primitive",
			Primitive: "capsule",
			Params:    map[string]float32{"radius": 2},
			Transform: LevelTransformDef{
				Rotation: Quat{0, 0, 0, 1},
				Scale:    Vec3{1, 1, 1},
			},
		},
		{
			ID:   "brush-4",
			Name: "bad-custom",
			Kind: LevelBrushKindVoxelShape,
			VoxelShape: &AssetVoxelShapeDef{
				Palette: []AssetVoxelPaletteEntryDef{{Value: 1, MaterialID: "missing"}},
				Voxels:  []VoxelObjectVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
			},
			Transform: LevelTransformDef{
				Rotation: Quat{0, 0, 0, 1},
				Scale:    Vec3{1, 1, 1},
			},
		},
	}

	result := ValidateLevel(def, LevelValidationOptions{})
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	assertHasLevelValidationCode(t, result, "invalid_material_payload")
	assertHasLevelValidationCode(t, result, "missing_material_reference")
	assertHasLevelValidationCode(t, result, "invalid_brush_payload")
}

func TestValidateLevelRejectsInvalidBaseWorld(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "demo.gklevel")
	worldPath := filepath.Join(root, "worlds", "demo.gkworld")
	chunkPath := filepath.Join(root, "worlds", "demo_chunks", "0_0_0.gkchunk")

	if err := SaveImportedWorldChunk(chunkPath, &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	}); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}
	if err := SaveImportedWorld(worldPath, &ImportedWorldDef{
		WorldID:         "world-a",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       16,
		VoxelResolution: 1,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ChunkPath:          AuthorDocumentPath(chunkPath, worldPath),
			NonEmptyVoxelCount: 1,
		}},
	}); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	def := NewLevelDef("baseworld")
	def.ChunkSize = 4
	def.BaseWorld = &LevelBaseWorldDef{
		Kind:         ImportedWorldKindVoxelWorld,
		ManifestPath: filepath.Join("..", "worlds", "demo.gkworld"),
	}

	result := ValidateLevel(def, LevelValidationOptions{DocumentPath: levelPath})
	if !result.HasErrors() {
		t.Fatal("expected base world validation error")
	}
	assertHasLevelValidationCode(t, result, "base_world_chunk_size_mismatch")

	def.BaseWorld = &LevelBaseWorldDef{
		Kind:         ImportedWorldKind("bad"),
		ManifestPath: "missing.txt",
	}
	result = ValidateLevel(def, LevelValidationOptions{DocumentPath: levelPath})
	assertHasLevelValidationCode(t, result, "invalid_base_world_kind")
	assertHasLevelValidationCode(t, result, "invalid_base_world_manifest_path")
}

func TestValidateLevelShooterRequiresPlayerSpawnAndClearMarkerPlacement(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "demo.gklevel")
	worldPath := filepath.Join(root, "worlds", "demo.gkworld")
	chunkPath := filepath.Join(root, "worlds", "demo_chunks", "0_0_0.gkchunk")

	if err := SaveImportedWorldChunk(chunkPath, &ImportedWorldChunkDef{
		WorldID:         "world-a",
		Coord:           TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:       16,
		VoxelResolution: 1,
		Voxels: []ImportedWorldVoxelDef{
			{X: 1, Y: 1, Z: 1, Value: 1},
		},
		NonEmptyVoxelCount: 1,
	}); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}
	if err := SaveImportedWorld(worldPath, &ImportedWorldDef{
		WorldID:         "world-a",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       16,
		VoxelResolution: 1,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ChunkPath:          AuthorDocumentPath(chunkPath, worldPath),
			NonEmptyVoxelCount: 1,
		}},
	}); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	def := NewLevelDef("shooter")
	def.ChunkSize = 16
	def.Tags = []string{LevelTagShooter}
	def.BaseWorld = &LevelBaseWorldDef{
		Kind:         ImportedWorldKindVoxelWorld,
		ManifestPath: filepath.Join("..", "worlds", "demo.gkworld"),
	}
	def.Markers = []LevelMarkerDef{{
		ID:   "bad-spawn",
		Name: "bad-spawn",
		Kind: LevelMarkerKindAISpawn,
		Transform: LevelTransformDef{
			Position: Vec3{1, 1, 1},
			Rotation: Quat{0, 0, 0, 1},
			Scale:    Vec3{1, 1, 1},
		},
	}}

	result := ValidateLevel(def, LevelValidationOptions{DocumentPath: levelPath})
	if !result.HasErrors() {
		t.Fatal("expected shooter validation errors")
	}
	assertHasLevelValidationCode(t, result, "missing_player_spawn")
	assertHasLevelValidationCode(t, result, "marker_inside_solid")

	def.Markers = append(def.Markers, LevelMarkerDef{
		ID:   "player-spawn",
		Name: "player",
		Kind: LevelMarkerKindPlayerSpawn,
		Transform: LevelTransformDef{
			Position: Vec3{0.4, 0.0, 0.4},
			Rotation: Quat{0, 0, 0, 1},
			Scale:    Vec3{1, 1, 1},
		},
	})
	result = ValidateLevel(def, LevelValidationOptions{DocumentPath: levelPath})
	assertHasLevelValidationCode(t, result, "marker_inside_solid")
}

func TestValidateLevelRejectsInvalidPlacementVolumeAndAssetSet(t *testing.T) {
	tmpDir := t.TempDir()
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := SaveAssetSet(filepath.Join(assetsDir, "bad.gkset"), &AssetSetDef{
		Name: "bad",
		Entries: []AssetSetEntryDef{
			{AssetPath: "missing.gkasset", Weight: 0},
		},
	}); err != nil {
		t.Fatalf("SaveAssetSet failed: %v", err)
	}

	def := NewLevelDef("volumes")
	def.PlacementVolumes = []PlacementVolumeDef{
		{
			ID:           "volume-1",
			Kind:         PlacementVolumeKindSphere,
			AssetPath:    "missing.gkasset",
			AssetSetPath: "bad.gkset",
			Radius:       0,
			Rule: PlacementVolumeRuleDef{
				Mode: PlacementVolumeRuleModeDensity,
			},
		},
	}

	result := ValidateLevel(def, LevelValidationOptions{DocumentPath: filepath.Join(assetsDir, "level.gklevel")})
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	assertHasLevelValidationCode(t, result, "invalid_volume_radius")
	assertHasLevelValidationCode(t, result, "invalid_volume_density")
	assertHasLevelValidationCode(t, result, "invalid_volume_source")
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
