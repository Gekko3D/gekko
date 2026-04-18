package content

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestAssetRoundTripPreservesSchemaAndIDs(t *testing.T) {
	asset := &AssetDef{
		Name: "Test Asset",
		Materials: []AssetMaterialDef{{
			ID:           "mat_painted_metal",
			Name:         "Painted Metal",
			BaseColor:    [4]uint8{180, 186, 196, 255},
			Roughness:    0.72,
			Metallic:     0.68,
			Emissive:     0.05,
			IOR:          1.45,
			Transparency: 0.1,
		}},
		Parts: []AssetPartDef{
			{
				Name: "Part 1",
				Source: AssetSourceDef{
					Kind:       AssetSourceKindVoxModel,
					Path:       "test.vox",
					ModelIndex: 2,
					MaterialID: "mat_painted_metal",
					Operation:  AssetShapeOperationAdd,
				},
				Transform: AssetTransformDef{
					Position: Vec3{1, 2, 3},
					Rotation: Quat{0, 0, 0, 1},
					Scale:    Vec3{1, 1, 1},
				},
				VoxelResolution: DefaultAssetVoxelSize,
				ModelScale:      1,
				Tags:            []string{"hero"},
			},
		},
		Lights: []AssetLightDef{
			{
				Name: "light_0",
				Transform: AssetTransformDef{
					Position: Vec3{0, 10, 0},
					Rotation: Quat{0, 0, 0, 1},
					Scale:    Vec3{1, 1, 1},
				},
				Type:         AssetLightTypeSpot,
				Color:        [3]float32{1, 0, 0},
				Intensity:    1,
				Range:        12,
				ConeAngle:    30,
				CastsShadows: true,
			},
		},
		Emitters: []AssetEmitterDef{
			{
				Name: "emitter_0",
				Transform: AssetTransformDef{
					Position: Vec3{4, 5, 6},
					Rotation: Quat{0, 0, 0, 1},
					Scale:    Vec3{1, 1, 1},
				},
				Emitter: EmitterDef{
					Enabled:          true,
					MaxParticles:     128,
					SpawnRate:        20,
					LifetimeRange:    Range2{1, 2},
					StartSpeedRange:  Range2{3, 4},
					StartSizeRange:   Range2{0.2, 0.4},
					StartColorMin:    Vec4{1, 0.4, 0, 1},
					StartColorMax:    Vec4{1, 1, 0, 1},
					Gravity:          9.8,
					Drag:             0.2,
					ConeAngleDegrees: 25,
					SpriteIndex:      3,
					AtlasCols:        4,
					AtlasRows:        5,
					TexturePath:      "assets/particles.png",
					AlphaMode:        AssetAlphaModeLuminance,
				},
			},
		},
		Markers: []AssetMarkerDef{
			{
				Name: "socket_muzzle",
				Transform: AssetTransformDef{
					Position: Vec3{0, 0, 1},
					Rotation: Quat{0, 0, 0, 1},
					Scale:    Vec3{1, 1, 1},
				},
				Kind: "socket",
				Tags: []string{"fx"},
			},
		},
	}

	tmpDir, err := os.MkdirTemp("", "content_asset_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "asset.gkasset")
	if err := SaveAsset(path, asset); err != nil {
		t.Fatalf("SaveAsset failed: %v", err)
	}

	loaded, err := LoadAsset(path)
	if err != nil {
		t.Fatalf("LoadAsset failed: %v", err)
	}

	if loaded.SchemaVersion != CurrentAssetSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", CurrentAssetSchemaVersion, loaded.SchemaVersion)
	}
	if loaded.ID == "" || loaded.Parts[0].ID == "" || loaded.Lights[0].ID == "" || loaded.Emitters[0].ID == "" || loaded.Markers[0].ID == "" {
		t.Fatal("expected asset and nested items to receive IDs")
	}
	if len(loaded.Materials) != 1 || loaded.Materials[0].ID != "mat_painted_metal" {
		t.Fatalf("expected authored material to round-trip, got %+v", loaded.Materials)
	}
	if loaded.Parts[0].Source.Kind != AssetSourceKindVoxModel || loaded.Parts[0].Source.ModelIndex != 2 {
		t.Fatalf("unexpected part source after round-trip: %+v", loaded.Parts[0].Source)
	}
	if loaded.Parts[0].Source.MaterialID != "mat_painted_metal" {
		t.Fatalf("expected part material ref to round-trip, got %+v", loaded.Parts[0].Source)
	}
	if EffectiveAssetSourceOperation(loaded.Parts[0].Source) != AssetShapeOperationAdd {
		t.Fatalf("expected part operation add, got %+v", loaded.Parts[0].Source)
	}
	if loaded.Lights[0].Type != AssetLightTypeSpot {
		t.Fatalf("expected light type %q, got %q", AssetLightTypeSpot, loaded.Lights[0].Type)
	}
	if !loaded.Lights[0].CastsShadows {
		t.Fatal("expected light shadow flag to survive round-trip")
	}
	if loaded.Emitters[0].Emitter.AlphaMode != AssetAlphaModeLuminance {
		t.Fatalf("expected alpha mode %q, got %q", AssetAlphaModeLuminance, loaded.Emitters[0].Emitter.AlphaMode)
	}
}

func TestAssetJSONUsesStringEnums(t *testing.T) {
	asset := NewAssetDef("Enum Test")
	asset.Parts = []AssetPartDef{{
		Name: "part_0",
		Source: AssetSourceDef{
			Kind:       AssetSourceKindVoxModel,
			Path:       "test.vox",
			ModelIndex: 1,
			MaterialID: "mat_0",
			Operation:  AssetShapeOperationSubtract,
		},
		Transform: AssetTransformDef{
			Rotation: Quat{0, 0, 0, 1},
			Scale:    Vec3{1, 1, 1},
		},
	}}
	asset.Lights = []AssetLightDef{{
		Name: "light_0",
		Transform: AssetTransformDef{
			Rotation: Quat{0, 0, 0, 1},
			Scale:    Vec3{1, 1, 1},
		},
		Type: AssetLightTypeAmbient,
	}}
	asset.Emitters = []AssetEmitterDef{{
		Name: "emitter_0",
		Transform: AssetTransformDef{
			Rotation: Quat{0, 0, 0, 1},
			Scale:    Vec3{1, 1, 1},
		},
		Emitter: EmitterDef{
			Enabled:   true,
			AlphaMode: AssetAlphaModeTexture,
		},
	}}

	data, err := json.Marshal(asset)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	jsonText := string(data)
	for _, want := range []string{`"schema_version":3`, `"kind":"vox_model"`, `"type":"ambient"`, `"alpha_mode":"texture"`, `"material_id":"mat_0"`, `"operation":"subtract"`} {
		if !contains(jsonText, want) {
			t.Fatalf("expected JSON to contain %s, got %s", want, jsonText)
		}
	}
}

func TestAssetVoxelShapeRoundTrip(t *testing.T) {
	asset := NewAssetDef("custom")
	asset.Materials = []AssetMaterialDef{{
		ID:           "mat_red",
		Name:         "Red",
		BaseColor:    [4]uint8{255, 80, 80, 255},
		Roughness:    0.6,
		Metallic:     0.1,
		IOR:          1.5,
		Transparency: 0,
	}}
	asset.Parts = []AssetPartDef{{
		ID:   "shape",
		Name: "shape",
		Source: AssetSourceDef{
			Kind: AssetSourceKindVoxelShape,
			VoxelShape: &AssetVoxelShapeDef{
				Palette: []AssetVoxelPaletteEntryDef{{Value: 1, MaterialID: "mat_red"}},
				Voxels: []VoxelObjectVoxelDef{
					{X: 0, Y: 0, Z: 0, Value: 1},
					{X: 1, Y: 0, Z: 0, Value: 1},
				},
			},
		},
		Transform: AssetTransformDef{Rotation: Quat{0, 0, 0, 1}, Scale: Vec3{1, 1, 1}},
	}}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "custom.gkasset")
	if err := SaveAsset(path, asset); err != nil {
		t.Fatalf("SaveAsset failed: %v", err)
	}

	loaded, err := LoadAsset(path)
	if err != nil {
		t.Fatalf("LoadAsset failed: %v", err)
	}
	if loaded.Parts[0].Source.Kind != AssetSourceKindVoxelShape {
		t.Fatalf("expected voxel_shape source, got %+v", loaded.Parts[0].Source)
	}
	if loaded.Parts[0].Source.VoxelShape == nil || len(loaded.Parts[0].Source.VoxelShape.Voxels) != 2 {
		t.Fatalf("expected voxel shape voxels to round-trip, got %+v", loaded.Parts[0].Source.VoxelShape)
	}
}

func TestLoadAssetRejectsUnknownSchemaVersion(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "content_asset_invalid_schema")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "bad.gkasset")
	if err := os.WriteFile(path, []byte(`{"id":"1","schema_version":99,"name":"bad"}`), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadAsset(path); err == nil {
		t.Fatal("expected LoadAsset to reject unsupported schema_version")
	}
}

func TestLoadAssetNormalizesLegacyV1SourceScaleDefaults(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "content_asset_legacy_v1")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "legacy.gkasset")
	if err := os.WriteFile(path, []byte(`{
  "id":"legacy-asset",
  "schema_version":1,
  "name":"legacy",
  "parts":[
    {
      "id":"part",
      "name":"part",
      "source":{"kind":"vox_model","path":"crate.vox","model_index":0},
      "transform":{"rotation":[0,0,0,1],"scale":[1,1,1]}
    }
  ]
}`), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadAsset(path)
	if err != nil {
		t.Fatalf("LoadAsset failed: %v", err)
	}
	if loaded.SchemaVersion != CurrentAssetSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", CurrentAssetSchemaVersion, loaded.SchemaVersion)
	}
	if got := loaded.Parts[0].VoxelResolution; got != DefaultAssetVoxelSize {
		t.Fatalf("expected voxel resolution %.2f, got %.4f", DefaultAssetVoxelSize, got)
	}
	if got := loaded.Parts[0].ModelScale; got != 1.0 {
		t.Fatalf("expected model scale 1.0, got %.4f", got)
	}
}

func TestLoadAssetParsesWithoutRunningValidation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "content_asset_invalid_but_parseable")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "invalid.gkasset")
	def := &AssetDef{
		Name: "invalid",
		Parts: []AssetPartDef{{
			ID:   "part",
			Name: "part",
			Source: AssetSourceDef{
				Kind: AssetSourceKindVoxModel,
			},
			Transform: AssetTransformDef{
				Rotation: Quat{0, 0, 0, 1},
				Scale:    Vec3{1, 1, 1},
			},
		}},
	}
	if err := SaveAsset(path, def); err != nil {
		t.Fatalf("SaveAsset failed: %v", err)
	}

	loaded, err := LoadAsset(path)
	if err != nil {
		t.Fatalf("LoadAsset failed: %v", err)
	}

	validation := ValidateAsset(loaded, AssetValidationOptions{DocumentPath: path})
	if !validation.HasErrors() {
		t.Fatal("expected validation to fail for parseable invalid asset")
	}
}

func TestGoldenSimpleAssetParsesValidatesAndRoundTrips(t *testing.T) {
	assertGoldenAssetRoundTrip(t, "simple_single_part.gkasset")
}

func TestGoldenCompositeAssetParsesValidatesAndRoundTrips(t *testing.T) {
	def := assertGoldenAssetRoundTrip(t, "composite_authored_asset.gkasset")

	if len(def.Markers) != 1 {
		t.Fatalf("expected composite golden asset marker to survive round-trip, got %+v", def.Markers)
	}
	if def.Markers[0].Kind != AssetMarkerKindMuzzle {
		t.Fatalf("expected muzzle marker kind, got %q", def.Markers[0].Kind)
	}
	if !reflect.DeepEqual(def.Markers[0].Tags, []string{"fx", "socket"}) {
		t.Fatalf("unexpected marker tags %+v", def.Markers[0].Tags)
	}
}

func TestGoldenCompositeAssetPreservesChildBeforeParentOrder(t *testing.T) {
	def, err := LoadAsset(goldenAssetPathForTest(t, "composite_authored_asset.gkasset"))
	if err != nil {
		t.Fatalf("LoadAsset failed: %v", err)
	}

	if len(def.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %+v", def.Parts)
	}
	if def.Parts[0].ID != "part-child" || def.Parts[1].ID != "part-root" {
		t.Fatalf("expected child-before-parent file order to remain intact, got %+v", def.Parts)
	}

	validation := ValidateAsset(def, AssetValidationOptions{DocumentPath: goldenAssetPathForTest(t, "composite_authored_asset.gkasset")})
	if validation.HasErrors() {
		t.Fatalf("expected child-before-parent golden asset to validate cleanly, got %+v", validation.Issues)
	}
}

func TestAssetRoundTripPreservesSpaceSimMarkerKindsAndTags(t *testing.T) {
	asset := NewAssetDef("spacesim-markers")
	asset.Parts = []AssetPartDef{{
		ID:   "hull",
		Name: "hull",
		Source: AssetSourceDef{
			Kind:      AssetSourceKindGroup,
			Operation: AssetShapeOperationAdd,
		},
		Transform: AssetTransformDef{
			Rotation: Quat{0, 0, 0, 1},
			Scale:    Vec3{1, 1, 1},
		},
	}}
	asset.Markers = []AssetMarkerDef{
		{
			ID:       "dock",
			Name:     "dock_a",
			ParentID: "hull",
			Transform: AssetTransformDef{
				Position: Vec3{1, 2, 3},
				Rotation: Quat{0, 0, 0, 1},
				Scale:    Vec3{1, 1, 1},
			},
			Kind: AssetMarkerKindDockPort,
			Tags: []string{"station", "primary"},
		},
		{
			ID:       "weapon",
			Name:     "weapon_front",
			ParentID: "hull",
			Transform: AssetTransformDef{
				Position: Vec3{0, 0, 4},
				Rotation: Quat{0, 0, 0, 1},
				Scale:    Vec3{1, 1, 1},
			},
			Kind: AssetMarkerKindWeaponSlot,
			Tags: []string{"laser"},
		},
	}

	tmpDir, err := os.MkdirTemp("", "content_asset_marker_contract_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "spacesim_markers.gkasset")
	if err := SaveAsset(path, asset); err != nil {
		t.Fatalf("SaveAsset failed: %v", err)
	}

	loaded, err := LoadAsset(path)
	if err != nil {
		t.Fatalf("LoadAsset failed: %v", err)
	}
	if len(loaded.Markers) != 2 {
		t.Fatalf("expected 2 markers, got %+v", loaded.Markers)
	}
	if loaded.Markers[0].Kind != AssetMarkerKindDockPort || loaded.Markers[0].ParentID != "hull" {
		t.Fatalf("expected dock marker contract to round-trip, got %+v", loaded.Markers[0])
	}
	if !reflect.DeepEqual(loaded.Markers[0].Tags, []string{"station", "primary"}) {
		t.Fatalf("expected dock marker tags to round-trip, got %+v", loaded.Markers[0].Tags)
	}
	if loaded.Markers[1].Kind != AssetMarkerKindWeaponSlot || loaded.Markers[1].ParentID != "hull" {
		t.Fatalf("expected weapon marker contract to round-trip, got %+v", loaded.Markers[1])
	}
}

func TestKnownAssetMarkerKindsIncludesSpaceSimAnchors(t *testing.T) {
	kinds := KnownAssetMarkerKinds()
	if !containsString(kinds, AssetMarkerKindDockPort) {
		t.Fatalf("expected known marker kinds to include %q, got %+v", AssetMarkerKindDockPort, kinds)
	}
	if !containsString(kinds, AssetMarkerKindWeaponSlot) {
		t.Fatalf("expected known marker kinds to include %q, got %+v", AssetMarkerKindWeaponSlot, kinds)
	}
}

func assertGoldenAssetRoundTrip(t *testing.T, fileName string) *AssetDef {
	t.Helper()

	path := goldenAssetPathForTest(t, fileName)
	def, err := LoadAsset(path)
	if err != nil {
		t.Fatalf("LoadAsset(%s) failed: %v", fileName, err)
	}

	validation := ValidateAsset(def, AssetValidationOptions{DocumentPath: path})
	if validation.HasErrors() {
		t.Fatalf("expected golden asset %s to validate cleanly, got %+v", fileName, validation.Issues)
	}

	tmpDir := t.TempDir()
	savedPath := filepath.Join(tmpDir, fileName)
	if err := SaveAsset(savedPath, def); err != nil {
		t.Fatalf("SaveAsset(%s) failed: %v", fileName, err)
	}

	fixtureBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) failed: %v", path, err)
	}
	savedBytes, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) failed: %v", savedPath, err)
	}
	if !bytes.Equal(bytes.TrimSpace(savedBytes), bytes.TrimSpace(fixtureBytes)) {
		t.Fatalf("expected saved golden asset %s to match fixture\nfixture:\n%s\nsaved:\n%s", fileName, fixtureBytes, savedBytes)
	}

	reloaded, err := LoadAsset(savedPath)
	if err != nil {
		t.Fatalf("reloaded LoadAsset(%s) failed: %v", fileName, err)
	}
	if !reflect.DeepEqual(reloaded, def) {
		t.Fatalf("expected reloaded golden asset %s to match original\nwant: %+v\ngot: %+v", fileName, def, reloaded)
	}

	return def
}

func goldenAssetPathForTest(t *testing.T, fileName string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(currentFile), "testdata", fileName)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || strings.Contains(haystack, needle)
}
