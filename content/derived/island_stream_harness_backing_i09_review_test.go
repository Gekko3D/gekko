package derived

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestI09PublicationBorrowedBackingQualifiedAndRebased(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "malformed", "metadata"} {
		t.Run(mode, func(t *testing.T) {
			sourcePath, source, load := i09PublicationFixture(t)
			backingPath := filepath.Join(filepath.Dir(sourcePath), "solid.gkvoxelbacking")
			backing := &content.VoxelBackingDef{Kind: content.VoxelBackingKindPlaneTreeV1, SourceHash: source.SourceHash, BoundsMax: [3]int{4, 4, 4}, SolidValue: 7, PlaneTree: &content.VoxelBackingPlaneTreeDef{Root: -1, Leaves: []content.VoxelBackingPlaneLeafDef{{Solid: true}}}}
			if err := content.SaveVoxelBacking(backingPath, backing); err != nil {
				t.Fatal(err)
			}
			source.Backing = &content.VoxelBackingRefDef{Path: "solid.gkvoxelbacking", Kind: backing.Kind, SourceHash: backing.SourceHash, BoundsMin: backing.BoundsMin, BoundsMax: backing.BoundsMax}
			if err := content.SaveImportedWorld(sourcePath, source); err != nil {
				t.Fatal(err)
			}
			harness, err := BuildIslandStreamHarness(source, load, i09PublicationOptions())
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "missing":
				if err := os.Remove(backingPath); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				if err := os.WriteFile(backingPath, []byte("not backing JSON"), 0600); err != nil {
					t.Fatal(err)
				}
			case "metadata":
				backing.SourceHash = "different"
				if err := content.SaveVoxelBacking(backingPath, backing); err != nil {
					t.Fatal(err)
				}
			}
			before := i09FileTree(t, filepath.Dir(sourcePath))
			out := filepath.Join(t.TempDir(), "out")
			err = SaveIslandStreamHarness(out, sourcePath, harness)
			if mode != "valid" {
				if err == nil {
					t.Error("invalid borrowed backing was published")
				}
				if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
					t.Errorf("invalid backing created output: %v", statErr)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				_, _, poi := i09ReadPublished(t, out)
				wp := content.ResolveDocumentPath(harness.Level.BaseWorld.ManifestPath, filepath.Join(out, "island_streaming_harness.gklevel"))
				got := content.ResolveDocumentPath(poi.Backing.Path, wp)
				if filepath.Clean(got) != filepath.Clean(backingPath) {
					t.Fatalf("backing resolves to %s, want original %s", got, backingPath)
				}
				decoded, err := content.LoadVoxelBacking(got)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(decoded, backing) {
					t.Fatal("borrowed backing metadata changed")
				}
				for _, data := range i09FileTree(t, out) {
					if bytes.Equal(data, before["solid.gkvoxelbacking"]) {
						t.Fatal("borrowed backing copied into output")
					}
				}
			}
			if !reflect.DeepEqual(before, i09FileTree(t, filepath.Dir(sourcePath))) {
				t.Fatal("source files changed")
			}
		})
	}
}
