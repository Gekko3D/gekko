package common

import (
	"os"
	"path/filepath"
	"testing"

	contentderived "github.com/gekko3d/gekko/content/derived"
)

func TestI06ReviewCommonSaveRejectsUnownedPageBakeWithoutFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "world.gkworld")
	emission := ImportedWorldEmission{PageBake: &contentderived.ImportedWorldPageBake{}}
	// A nil draft is an input error, not a programmer-only precondition.
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("unowned page bake panicked: %v", p)
		}
	}()
	if _, err := SaveImportedWorldEmissionWithOptionsResult(path, emission, ImportedWorldSaveOptions{}); err == nil {
		t.Fatal("unowned page bake accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("invalid save created files")
	}
}
