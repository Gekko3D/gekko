package derived

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestI09PublicationRejectsBorrowedPayloadInsideOutput(t *testing.T) {
	for _, kind := range []string{"full", "aux"} {
		t.Run(kind, func(t *testing.T) {
			p, source, load := i09PublicationFixture(t)
			out := filepath.Join(t.TempDir(), "fixture")
			if err := os.MkdirAll(out, 0755); err != nil {
				t.Fatal(err)
			}
			old := content.ResolveImportedWorldChunkPath(source.Entries[0], p)
			name := "borrowed.gkchunk"
			if kind == "aux" {
				old = content.ResolveDocumentPath(source.Entries[0].Aux.AuxPath, p)
				name = "borrowed.gkaux"
			}
			borrowed := filepath.Join(out, name)
			if err := os.Rename(old, borrowed); err != nil {
				t.Fatal(err)
			}
			if kind == "full" {
				source.Entries[0].ChunkPath = borrowed
			} else {
				source.Entries[0].Aux.AuxPath = borrowed
			}
			if err := content.SaveImportedWorld(p, source); err != nil {
				t.Fatal(err)
			}
			h, err := BuildIslandStreamHarness(source, load, i09PublicationOptions())
			if err != nil {
				t.Fatal(err)
			}
			beforeOutput := i09FileTree(t, out)
			beforeSource := i09FileTree(t, filepath.Dir(p))
			if err = SaveIslandStreamHarness(out, p, h); err == nil {
				t.Fatal("borrowed input owned by output tree was accepted")
			}
			if !reflect.DeepEqual(beforeOutput, i09FileTree(t, out)) || !reflect.DeepEqual(beforeSource, i09FileTree(t, filepath.Dir(p))) {
				t.Fatal("rejected publication changed a borrowed input tree")
			}
		})
	}
}
