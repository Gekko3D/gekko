package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetSetRoundTripPreservesSchema(t *testing.T) {
	tmpDir := t.TempDir()
	assetPath := filepath.Join(tmpDir, "assets", "rock.gkasset")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	def := &AssetSetDef{
		Name: "Asteroids",
		Entries: []AssetSetEntryDef{
			{AssetPath: filepath.Base(assetPath), Weight: 2},
		},
	}
	path := filepath.Join(tmpDir, "assets", "asteroids.gkset")
	if err := SaveAssetSet(path, def); err != nil {
		t.Fatalf("SaveAssetSet failed: %v", err)
	}

	loaded, err := LoadAssetSet(path)
	if err != nil {
		t.Fatalf("LoadAssetSet failed: %v", err)
	}
	if loaded.SchemaVersion != CurrentAssetSetSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", CurrentAssetSetSchemaVersion, loaded.SchemaVersion)
	}
	if loaded.ID == "" || len(loaded.Entries) != 1 || loaded.Entries[0].Weight != 2 {
		t.Fatalf("unexpected loaded asset set: %+v", loaded)
	}
}

func TestAssetSetJSONUsesExplicitFields(t *testing.T) {
	def := &AssetSetDef{
		Name: "Debris",
		Entries: []AssetSetEntryDef{
			{AssetPath: "assets/debris.gkasset", Weight: 1.5},
		},
	}
	data, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if !strings.Contains(string(data), `"asset_path":"assets/debris.gkasset"`) {
		t.Fatalf("expected asset_path in JSON, got %s", string(data))
	}
}

func TestValidateAssetSetRejectsInvalidEntries(t *testing.T) {
	def := NewAssetSetDef("")
	def.Entries = []AssetSetEntryDef{
		{AssetPath: "", Weight: 0},
	}
	result := ValidateAssetSet(def, AssetSetValidationOptions{})
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	assertHasAssetSetValidationCode(t, result, "empty_name")
	assertHasAssetSetValidationCode(t, result, "empty_asset_path")
	assertHasAssetSetValidationCode(t, result, "invalid_weight")
}

func assertHasAssetSetValidationCode(t *testing.T, result AssetSetValidationResult, want string) {
	t.Helper()
	for _, issue := range result.Issues {
		if issue.Code == want {
			return
		}
	}
	t.Fatalf("expected validation code %q, got %+v", want, result.Issues)
}
