package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLevelRoundTripPreservesSchemaAndIDs(t *testing.T) {
	directionalCastsShadows := true
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
			Preset:                  "orbit",
			DirectionalCastsShadows: &directionalCastsShadows,
			Tags:                    []string{"placeholder"},
		},
		Lights: []LevelLightDef{{
			ID:            "light-1",
			Name:          "Lamp",
			Type:          LevelLightTypeSpot,
			Color:         [3]float32{1, 0.8, 0.6},
			Intensity:     1.25,
			Range:         12,
			ConeAngle:     45,
			SourceRadius:  0.25,
			EmitterLinkID: 42,
			Transform: LevelTransformDef{
				Position: Vec3{3, 4, 5},
				Rotation: Quat{0, 0, 0, 1},
				Scale:    Vec3{1, 1, 1},
			},
			Tags: []string{"source:hl1"},
		}},
		BaseWorld: &LevelBaseWorldDef{
			Kind:              ImportedWorldKindVoxelWorld,
			ManifestPath:      "worlds/station.gkworld",
			ReadOnlyByDefault: true,
			CollisionEnabled:  true,
			Tags:              []string{"imported"},
		},
		Player: &LevelPlayerDef{
			SpawnKind:        "hl1_player_spawn",
			Height:           1.8288,
			EyeHeight:        1.6256,
			Radius:           0.4064,
			Speed:            4.8,
			SprintMultiplier: 1.2,
			JumpSpeed:        5,
			Gravity:          18,
			StepHeight:       0.4572,
			Tags:             []string{"source:hl1"},
		},
		LadderVolumes: []LevelLadderVolumeDef{{
			ID:                "ladder-1",
			Name:              "maintenance ladder",
			BoundsCenter:      Vec3{2, 3, 4},
			BoundsHalfExtents: Vec3{0.25, 2, 0.5},
			ClimbSpeed:        3.25,
			SourceTag:         "hl1:func_ladder",
			Tags:              []string{"source:hl1"},
		}},
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
	if loaded.Player == nil || loaded.Player.SpawnKind != "hl1_player_spawn" || loaded.Player.Height != 1.8288 || loaded.Player.EyeHeight != 1.6256 || loaded.Player.Radius != 0.4064 {
		t.Fatalf("expected player metadata to round-trip, got %+v", loaded.Player)
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
	if loaded.Environment.DirectionalCastsShadows == nil || !*loaded.Environment.DirectionalCastsShadows {
		t.Fatalf("expected directional shadow flag to round-trip, got %+v", loaded.Environment)
	}
	if len(loaded.Lights) != 1 || loaded.Lights[0].Type != LevelLightTypeSpot || loaded.Lights[0].ConeAngle != 45 || loaded.Lights[0].SourceRadius != 0.25 || loaded.Lights[0].EmitterLinkID != 42 {
		t.Fatalf("expected lights to round-trip, got %+v", loaded.Lights)
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

func TestValidateLevelRejectsInvalidSpotCone(t *testing.T) {
	def := NewLevelDef("lights")
	def.Lights = []LevelLightDef{
		{
			ID:        "zero-cone",
			Name:      "zero-cone",
			Type:      LevelLightTypeSpot,
			Range:     1,
			ConeAngle: 0,
		},
		{
			ID:        "wide-cone",
			Name:      "wide-cone",
			Type:      LevelLightTypeSpot,
			Range:     1,
			ConeAngle: 180,
		},
		{
			ID:        "point-zero-cone",
			Name:      "point-zero-cone",
			Type:      LevelLightTypePoint,
			Range:     1,
			ConeAngle: 0,
		},
	}

	result := ValidateLevel(def, LevelValidationOptions{})
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	if result.HardErrorCount != 2 {
		t.Fatalf("expected only the two spot cones to fail validation, got %d: %+v", result.HardErrorCount, result.Issues)
	}
	assertHasLevelValidationCode(t, result, "invalid_light_cone_angle")
}

func TestValidateLevelRejectsZeroRangeLocalLights(t *testing.T) {
	def := NewLevelDef("lights")
	def.Lights = []LevelLightDef{
		{
			ID:    "zero-point",
			Name:  "zero-point",
			Type:  LevelLightTypePoint,
			Range: 0,
		},
		{
			ID:        "zero-spot",
			Name:      "zero-spot",
			Type:      LevelLightTypeSpot,
			Range:     0,
			ConeAngle: 45,
		},
		{
			ID:    "directional-zero-range",
			Name:  "directional-zero-range",
			Type:  LevelLightTypeDirectional,
			Range: 0,
		},
	}

	result := ValidateLevel(def, LevelValidationOptions{})
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	if result.HardErrorCount != 2 {
		t.Fatalf("expected only the two local lights to fail validation, got %d: %+v", result.HardErrorCount, result.Issues)
	}
	assertHasLevelValidationCode(t, result, "invalid_light_range")
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

func TestLevelWaterBodyRoundTripAndValidate(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "water.gklevel")
	def := NewLevelDef("water")
	directLightOcclusion := float32(0.75)
	def.WaterBodies = []LevelWaterBodyDef{{
		ID:                   "water-1",
		Name:                 "pool",
		Mode:                 LevelWaterBodyModeExplicitRect,
		SurfaceY:             2.5,
		Depth:                1.25,
		RectHalfExtents:      Vec2{4, 6},
		ContinuityGroup:      "pool-a",
		Color:                Vec3{0.1, 0.3, 0.8},
		DirectLightOcclusion: &directLightOcclusion,
		FlowDirection:        Vec2{1, 0},
		Transform: LevelTransformDef{
			Position: Vec3{10, 2.5, 20},
			Rotation: Quat{0, 0, 0, 1},
			Scale:    Vec3{1, 1, 1},
		},
		Tags: []string{"source:hl1"},
	}}
	if result := ValidateLevel(def, LevelValidationOptions{DocumentPath: path}); result.HasErrors() {
		t.Fatalf("expected valid water body, got %+v", result.Issues)
	}
	if err := SaveLevel(path, def); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}
	loaded, err := LoadLevel(path)
	if err != nil {
		t.Fatalf("LoadLevel failed: %v", err)
	}
	if len(loaded.WaterBodies) != 1 || loaded.WaterBodies[0].RectHalfExtents != (Vec2{4, 6}) ||
		loaded.WaterBodies[0].ContinuityGroup != "pool-a" ||
		loaded.WaterBodies[0].DirectLightOcclusion == nil || *loaded.WaterBodies[0].DirectLightOcclusion != 0.75 {
		t.Fatalf("water bodies did not round-trip: %+v", loaded.WaterBodies)
	}
}

func TestLevelLadderVolumeRoundTripAndValidate(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "ladder.gklevel")
	def := NewLevelDef("ladder")
	def.LadderVolumes = []LevelLadderVolumeDef{{
		ID:                "ladder-1",
		Name:              "ladder",
		BoundsCenter:      Vec3{1, 2, 3},
		BoundsHalfExtents: Vec3{0.25, 2, 0.4},
		ClimbSpeed:        3.5,
		SourceTag:         "hl1:func_ladder",
		Tags:              []string{"source:hl1"},
	}}
	if result := ValidateLevel(def, LevelValidationOptions{DocumentPath: path}); result.HasErrors() {
		t.Fatalf("expected valid ladder volume, got %+v", result.Issues)
	}
	if err := SaveLevel(path, def); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}
	loaded, err := LoadLevel(path)
	if err != nil {
		t.Fatalf("LoadLevel failed: %v", err)
	}
	if len(loaded.LadderVolumes) != 1 || loaded.LadderVolumes[0].BoundsHalfExtents != (Vec3{0.25, 2, 0.4}) || loaded.LadderVolumes[0].ClimbSpeed != 3.5 {
		t.Fatalf("ladder volumes did not round-trip: %+v", loaded.LadderVolumes)
	}
}

func TestValidateLevelRejectsInvalidLadderVolume(t *testing.T) {
	def := NewLevelDef("bad-ladder")
	def.LadderVolumes = []LevelLadderVolumeDef{{
		ID:                "bad-ladder",
		BoundsHalfExtents: Vec3{0, 2, 0.4},
		ClimbSpeed:        -1,
	}}
	result := ValidateLevel(def, LevelValidationOptions{})
	if !result.HasErrors() {
		t.Fatal("expected ladder volume validation errors")
	}
	assertHasLevelValidationCode(t, result, "invalid_ladder_volume_bounds")
	assertHasLevelValidationCode(t, result, "invalid_ladder_climb_speed")
}

func TestLevelMovingBrushAndUseTriggerRoundTripAndValidate(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "moving.gklevel")
	def := NewLevelDef("moving")
	def.MovingBrushes = []LevelMovingBrushDef{{
		ID:                "door-1",
		Name:              "door",
		Kind:              "hl1_func_door",
		MotionKind:        "rotate",
		BoundsCenter:      Vec3{1, 2, 3},
		BoundsHalfExtents: Vec3{0.5, 1, 0.25},
		MoveDirection:     Vec3{1, 0, 0},
		MoveDistance:      2,
		RotationOrigin:    Vec3{0, 2, 3},
		RotationAxis:      Vec3{0, 1, 0},
		OpenAngle:         -90,
		PathTarget:        "corner_a",
		Speed:             3.5,
		Wait:              1,
		Lip:               0.1,
		TargetName:        "door_a",
		SourceTag:         "hl1:func_door",
	}}
	def.PathNodes = []LevelPathNodeDef{{
		ID:         "corner-1",
		Name:       "corner",
		TargetName: "corner_a",
		Target:     "corner_b",
		Position:   Vec3{2, 3, 4},
		Wait:       0.25,
		Speed:      1.5,
		SourceTag:  "hl1:path_corner",
	}}
	def.UseTriggers = []LevelUseTriggerDef{{
		ID:                "button-1",
		Name:              "button",
		Kind:              "hl1_func_button",
		BoundsCenter:      Vec3{4, 5, 6},
		BoundsHalfExtents: Vec3{0.25, 0.5, 0.25},
		Target:            "door_a",
		SourceTag:         "hl1:func_button",
	}}
	def.TriggerVolumes = []LevelTriggerVolumeDef{{
		ID:                "trigger-1",
		Name:              "trigger",
		Kind:              "hl1_trigger_once",
		BoundsCenter:      Vec3{7, 8, 9},
		BoundsHalfExtents: Vec3{1, 2, 3},
		Target:            "manager_a",
		Delay:             0.25,
		Wait:              0.5,
		Once:              true,
		SourceTag:         "hl1:trigger_once",
	}}
	def.DamageVolumes = []LevelDamageVolumeDef{{
		ID:                "damage-1",
		Name:              "acid",
		Kind:              "hl1_trigger_hurt",
		BoundsCenter:      Vec3{6, 7, 8},
		BoundsHalfExtents: Vec3{1, 1, 1},
		Damage:            15,
		DamageInterval:    0.75,
		TargetName:        "acid_a",
		Target:            "alarm_a",
		Delay:             0.2,
		SpawnFlags:        1,
		StartDisabled:     true,
		SourceTag:         "hl1:trigger_hurt",
	}}
	def.ChangeLevels = []LevelChangeLevelDef{{
		ID:                "change-1",
		Name:              "exit",
		Kind:              "hl1_trigger_changelevel",
		BoundsCenter:      Vec3{8, 9, 10},
		BoundsHalfExtents: Vec3{1, 1, 1},
		TargetMap:         "c1a1",
		Landmark:          "lm_a",
		TargetName:        "exit_a",
		SpawnFlags:        2,
		StartDisabled:     true,
		SourceTag:         "hl1:trigger_changelevel",
	}}
	def.Chargers = []LevelChargerDef{{
		ID:                "charger-1",
		Name:              "wall health",
		Kind:              "hl1_func_healthcharger",
		BoundsCenter:      Vec3{9, 10, 11},
		BoundsHalfExtents: Vec3{0.5, 0.5, 0.5},
		ChargeKind:        "health",
		Capacity:          50,
		Rate:              15,
		TargetName:        "charger_a",
		SpawnFlags:        1,
		StartDisabled:     true,
		SourceTag:         "hl1:func_healthcharger",
	}}
	def.MultiTargets = []LevelMultiTargetDef{{
		ID:         "manager-1",
		Name:       "manager",
		TargetName: "manager_a",
		Delay:      0.1,
		Events: []LevelTargetEventDef{{
			Target: "door_a",
			Delay:  0.2,
		}},
		SourceTag: "hl1:multi_manager",
	}}
	def.TargetRelays = []LevelTargetRelayDef{{
		ID:           "relay-1",
		Name:         "relay",
		Kind:         "hl1_trigger_relay",
		TargetName:   "relay_a",
		Target:       "door_a",
		Delay:        0.15,
		KillTarget:   "old_target",
		TriggerState: 1,
		SpawnFlags:   1,
		SourceTag:    "hl1:trigger_relay",
	}}
	def.Breakables = []LevelBreakableDef{{
		ID:                "breakable-1",
		Name:              "crate",
		Kind:              "hl1_func_breakable",
		BoundsCenter:      Vec3{2, 3, 4},
		BoundsHalfExtents: Vec3{0.5, 0.5, 0.5},
		Health:            25,
		Material:          "1",
		SpawnObject:       "0",
		SpawnFlags:        1,
		TargetName:        "crate_a",
		Target:            "door_a",
		Delay:             0.2,
		SourceTag:         "hl1:func_breakable",
	}}
	def.Pickups = []LevelPickupDef{{
		ID:        "pickup-1",
		Name:      "clip",
		Kind:      "hl1_pickup",
		Category:  "ammo",
		Item:      "9mmclip",
		Amount:    17,
		ClassName: "ammo_9mmclip",
		Transform: LevelTransformDef{
			Position: Vec3{3, 4, 5},
			Rotation: Quat{0, 0, 0, 1},
			Scale:    Vec3{1, 1, 1},
		},
		TargetName: "clip_a",
		SourceTag:  "hl1:ammo_9mmclip",
	}}
	if result := ValidateLevel(def, LevelValidationOptions{DocumentPath: path}); result.HasErrors() {
		t.Fatalf("expected valid moving brush/use trigger, got %+v", result.Issues)
	}
	if err := SaveLevel(path, def); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}
	loaded, err := LoadLevel(path)
	if err != nil {
		t.Fatalf("LoadLevel failed: %v", err)
	}
	if len(loaded.MovingBrushes) != 1 || loaded.MovingBrushes[0].TargetName != "door_a" || loaded.MovingBrushes[0].Speed != 3.5 || loaded.MovingBrushes[0].MoveDistance != 2 || loaded.MovingBrushes[0].MotionKind != "rotate" || loaded.MovingBrushes[0].OpenAngle != -90 || loaded.MovingBrushes[0].PathTarget != "corner_a" {
		t.Fatalf("moving brushes did not round-trip: %+v", loaded.MovingBrushes)
	}
	if len(loaded.PathNodes) != 1 || loaded.PathNodes[0].TargetName != "corner_a" || loaded.PathNodes[0].Target != "corner_b" || loaded.PathNodes[0].Speed != 1.5 {
		t.Fatalf("path nodes did not round-trip: %+v", loaded.PathNodes)
	}
	if len(loaded.UseTriggers) != 1 || loaded.UseTriggers[0].Target != "door_a" {
		t.Fatalf("use triggers did not round-trip: %+v", loaded.UseTriggers)
	}
	if len(loaded.TriggerVolumes) != 1 || !loaded.TriggerVolumes[0].Once || loaded.TriggerVolumes[0].Target != "manager_a" || loaded.TriggerVolumes[0].Delay != 0.25 {
		t.Fatalf("trigger volumes did not round-trip: %+v", loaded.TriggerVolumes)
	}
	if len(loaded.DamageVolumes) != 1 || loaded.DamageVolumes[0].Damage != 15 || loaded.DamageVolumes[0].DamageInterval != 0.75 || loaded.DamageVolumes[0].Delay != 0.2 || !loaded.DamageVolumes[0].StartDisabled {
		t.Fatalf("damage volumes did not round-trip: %+v", loaded.DamageVolumes)
	}
	if len(loaded.ChangeLevels) != 1 || loaded.ChangeLevels[0].TargetMap != "c1a1" || loaded.ChangeLevels[0].Landmark != "lm_a" || !loaded.ChangeLevels[0].StartDisabled {
		t.Fatalf("changelevel volumes did not round-trip: %+v", loaded.ChangeLevels)
	}
	if len(loaded.Chargers) != 1 || loaded.Chargers[0].ChargeKind != "health" || loaded.Chargers[0].Capacity != 50 || loaded.Chargers[0].Rate != 15 || !loaded.Chargers[0].StartDisabled {
		t.Fatalf("chargers did not round-trip: %+v", loaded.Chargers)
	}
	if len(loaded.MultiTargets) != 1 || loaded.MultiTargets[0].TargetName != "manager_a" || len(loaded.MultiTargets[0].Events) != 1 || loaded.MultiTargets[0].Events[0].Target != "door_a" {
		t.Fatalf("multi-targets did not round-trip: %+v", loaded.MultiTargets)
	}
	if len(loaded.TargetRelays) != 1 || loaded.TargetRelays[0].TargetName != "relay_a" || loaded.TargetRelays[0].Target != "door_a" || loaded.TargetRelays[0].TriggerState != 1 {
		t.Fatalf("target relays did not round-trip: %+v", loaded.TargetRelays)
	}
	if len(loaded.Breakables) != 1 || loaded.Breakables[0].TargetName != "crate_a" || loaded.Breakables[0].Target != "door_a" || loaded.Breakables[0].Health != 25 {
		t.Fatalf("breakables did not round-trip: %+v", loaded.Breakables)
	}
	if len(loaded.Pickups) != 1 || loaded.Pickups[0].Category != "ammo" || loaded.Pickups[0].Item != "9mmclip" || loaded.Pickups[0].Amount != 17 {
		t.Fatalf("pickups did not round-trip: %+v", loaded.Pickups)
	}
}

func TestValidateLevelRejectsInvalidMovingBrushAndUseTrigger(t *testing.T) {
	def := NewLevelDef("bad-moving")
	def.MovingBrushes = []LevelMovingBrushDef{{
		ID:                "bad-door",
		BoundsHalfExtents: Vec3{0, 1, 1},
		MoveDistance:      -1,
		Speed:             -1,
	}}
	def.PathNodes = []LevelPathNodeDef{{
		ID:         "bad-path",
		TargetName: "",
		Wait:       -1,
		Speed:      -1,
	}}
	def.UseTriggers = []LevelUseTriggerDef{{
		ID:                "bad-button",
		BoundsHalfExtents: Vec3{1, 0, 1},
	}}
	def.TriggerVolumes = []LevelTriggerVolumeDef{{
		ID:                "bad-trigger",
		BoundsHalfExtents: Vec3{1, 0, 1},
		Delay:             -1,
		Wait:              -1,
	}}
	def.DamageVolumes = []LevelDamageVolumeDef{{
		ID:                "bad-damage",
		BoundsHalfExtents: Vec3{1, 0, 1},
		Damage:            -1,
		DamageInterval:    -1,
		Delay:             -1,
	}}
	def.ChangeLevels = []LevelChangeLevelDef{{
		ID:                "bad-change",
		BoundsHalfExtents: Vec3{1, 0, 1},
	}}
	def.Chargers = []LevelChargerDef{{
		ID:                "bad-charger",
		BoundsHalfExtents: Vec3{1, 0, 1},
		ChargeKind:        "mana",
		Capacity:          -1,
		Rate:              -1,
	}}
	def.MultiTargets = []LevelMultiTargetDef{{
		ID:         "bad-manager",
		TargetName: "",
		Delay:      -1,
		Events: []LevelTargetEventDef{{
			Target: "",
			Delay:  -1,
		}},
	}}
	def.TargetRelays = []LevelTargetRelayDef{{
		ID:           "bad-relay",
		TargetName:   "",
		Delay:        -1,
		TriggerState: 3,
	}}
	def.Breakables = []LevelBreakableDef{{
		ID:                "bad-breakable",
		BoundsHalfExtents: Vec3{1, 0, 1},
		Health:            -1,
		Delay:             -1,
	}}
	def.Pickups = []LevelPickupDef{{
		ID:     "bad-pickup",
		Amount: -1,
	}}
	result := ValidateLevel(def, LevelValidationOptions{})
	if !result.HasErrors() {
		t.Fatal("expected moving brush/use trigger validation errors")
	}
	assertHasLevelValidationCode(t, result, "invalid_moving_brush_bounds")
	assertHasLevelValidationCode(t, result, "invalid_moving_brush_distance")
	assertHasLevelValidationCode(t, result, "invalid_moving_brush_speed")
	assertHasLevelValidationCode(t, result, "empty_path_node_target_name")
	assertHasLevelValidationCode(t, result, "invalid_path_node_wait")
	assertHasLevelValidationCode(t, result, "invalid_path_node_speed")
	assertHasLevelValidationCode(t, result, "invalid_use_trigger_bounds")
	assertHasLevelValidationCode(t, result, "invalid_trigger_volume_bounds")
	assertHasLevelValidationCode(t, result, "invalid_trigger_volume_delay")
	assertHasLevelValidationCode(t, result, "invalid_trigger_volume_wait")
	assertHasLevelValidationCode(t, result, "invalid_damage_volume_bounds")
	assertHasLevelValidationCode(t, result, "invalid_damage_volume_damage")
	assertHasLevelValidationCode(t, result, "invalid_damage_volume_interval")
	assertHasLevelValidationCode(t, result, "invalid_damage_volume_delay")
	assertHasLevelValidationCode(t, result, "invalid_change_level_bounds")
	assertHasLevelValidationCode(t, result, "empty_change_level_target_map")
	assertHasLevelValidationCode(t, result, "invalid_charger_bounds")
	assertHasLevelValidationCode(t, result, "invalid_charger_kind")
	assertHasLevelValidationCode(t, result, "invalid_charger_capacity")
	assertHasLevelValidationCode(t, result, "invalid_charger_rate")
	assertHasLevelValidationCode(t, result, "empty_multi_target_name")
	assertHasLevelValidationCode(t, result, "invalid_multi_target_delay")
	assertHasLevelValidationCode(t, result, "empty_multi_target_event")
	assertHasLevelValidationCode(t, result, "invalid_multi_target_event_delay")
	assertHasLevelValidationCode(t, result, "empty_target_relay_name")
	assertHasLevelValidationCode(t, result, "invalid_target_relay_delay")
	assertHasLevelValidationCode(t, result, "invalid_target_relay_state")
	assertHasLevelValidationCode(t, result, "invalid_breakable_bounds")
	assertHasLevelValidationCode(t, result, "invalid_breakable_health")
	assertHasLevelValidationCode(t, result, "invalid_breakable_delay")
	assertHasLevelValidationCode(t, result, "empty_pickup_category")
	assertHasLevelValidationCode(t, result, "empty_pickup_item")
	assertHasLevelValidationCode(t, result, "invalid_pickup_amount")
}

func TestValidateLevelRejectsInvalidWaterBody(t *testing.T) {
	def := NewLevelDef("bad-water")
	directLightOcclusion := float32(1.5)
	def.WaterBodies = []LevelWaterBodyDef{{
		ID:                   "bad-water",
		Mode:                 LevelWaterBodyModeExplicitRect,
		SurfaceY:             1,
		Depth:                0,
		RectHalfExtents:      Vec2{0, 4},
		DirectLightOcclusion: &directLightOcclusion,
	}}
	result := ValidateLevel(def, LevelValidationOptions{})
	if !result.HasErrors() {
		t.Fatal("expected water body validation errors")
	}
	assertHasLevelValidationCode(t, result, "invalid_water_body_depth")
	assertHasLevelValidationCode(t, result, "invalid_water_body_rect")
	assertHasLevelValidationCode(t, result, "invalid_water_body_direct_light_occlusion")
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
