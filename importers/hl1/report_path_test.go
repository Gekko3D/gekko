package hl1

import (
	"path/filepath"
	"testing"
)

func TestInferGameDirFromBSPPath(t *testing.T) {
	path := filepath.Join("/tmp", "hl", "valve", "maps", "gasworks.bsp")
	if got, want := InferGameDirFromBSPPath(path), filepath.Join("/tmp", "hl"); got != want {
		t.Fatalf("InferGameDirFromBSPPath = %q, want %q", got, want)
	}
}
