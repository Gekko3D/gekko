package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAssetRoundTripPreservesSchemaAndIDs(t *testing.T) {
	asset := &AssetDef{
		Name: "Test Asset",
		Parts: []AssetPartDef{
			{
				Name: "Part 1",
				Source: AssetSourceDef{
					Kind:       AssetSourceKindVoxModel,
					Path:       "test.vox",
					ModelIndex: 2,
				},
				Transform: AssetTransformDef{
					Position: Vec3{1, 2, 3},
					Rotation: Quat{0, 0, 0, 1},
					Scale:    Vec3{1, 1, 1},
				},
				ModelScale: 1,
				Tags:       []string{"hero"},
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
				Type:      AssetLightTypeSpot,
				Color:     [3]float32{1, 0, 0},
				Intensity: 1,
				Range:     12,
				ConeAngle: 30,
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
	if loaded.Parts[0].Source.Kind != AssetSourceKindVoxModel || loaded.Parts[0].Source.ModelIndex != 2 {
		t.Fatalf("unexpected part source after round-trip: %+v", loaded.Parts[0].Source)
	}
	if loaded.Lights[0].Type != AssetLightTypeSpot {
		t.Fatalf("expected light type %q, got %q", AssetLightTypeSpot, loaded.Lights[0].Type)
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
	for _, want := range []string{`"schema_version":1`, `"kind":"vox_model"`, `"type":"ambient"`, `"alpha_mode":"texture"`} {
		if !contains(jsonText, want) {
			t.Fatalf("expected JSON to contain %s, got %s", want, jsonText)
		}
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

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sep string) int {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}
