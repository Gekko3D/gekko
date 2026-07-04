package content

import (
	"path/filepath"
	"testing"
)

func TestAssetLibraryRoundTripAndResolve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "assets.gkassetlibrary")
	def := NewAssetLibraryDef("Test assets")
	def.Entries = []AssetLibraryEntryDef{{Key: "weapons.handgun", AssetPath: "models/handgun.gkasset"}}
	if err := SaveAssetLibrary(path, def); err != nil {
		t.Fatalf("save library: %v", err)
	}
	loaded, err := LoadAssetLibrary(path)
	if err != nil {
		t.Fatalf("load library: %v", err)
	}
	resolved, err := ResolveAssetLibraryPath(loaded, path, "weapons.handgun")
	if err != nil || resolved != filepath.Join(dir, "models", "handgun.gkasset") {
		t.Fatalf("resolved asset = %q, %v", resolved, err)
	}
}

func TestAssetLibraryRejectsDuplicateKeys(t *testing.T) {
	def := NewAssetLibraryDef("Duplicate")
	def.Entries = []AssetLibraryEntryDef{{Key: "tool", AssetPath: "a.gkasset"}, {Key: "tool", AssetPath: "b.gkasset"}}
	if err := ValidateAssetLibrary(def); err == nil {
		t.Fatal("expected duplicate key error")
	}
}
