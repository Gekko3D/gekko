package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestI05LegacyUnspecifiedSectorBounds(t *testing.T) {
	for _, v := range []struct {
		name     string
		sector   int
		proxy    bool
		min, max [3]float32
	}{{"full-leaf-coverage", 0, false, [3]float32{0, 0, 0}, [3]float32{8, 4, 4}}, {"signed-full-leaf-coverage", 1, false, [3]float32{-8, 0, 0}, [3]float32{-4, 4, 4}}, {"positive-away-origin", 1, false, [3]float32{12, 0, 0}, [3]float32{16, 4, 4}}, {"signed-known-proxy-coverage", 1, true, [3]float32{-6, 0, 0}, [3]float32{0, 6, 6}}} {
		t.Run(v.name, func(t *testing.T) {
			d := i05LegacyWorld()
			s := &d.Sectors[v.sector]
			if v.name == "signed-full-leaf-coverage" {
				d.Entries[2].Coord.X = -2
				s.FullChunkRefs = []TerrainChunkCoordDef{d.Entries[2].Coord}
			}
			if v.name == "positive-away-origin" {
				d.Entries[2].Coord.X = 3
				s.FullChunkRefs = []TerrainChunkCoordDef{d.Entries[2].Coord}
			}
			s.BoundsMin = [3]float32{}
			s.BoundsMax = [3]float32{}
			s.LODs = nil
			if v.proxy {
				s.LODs = []ImportedWorldLODDef{{Kind: "voxel_proxy", ChunkPath: "negative.gkchunk", ChunkSize: 3, VoxelResolution: 2, NonEmptyVoxelCount: 1}}
			}
			before := i05Clone(t, d)
			index, e := NormalizeImportedWorldPages(d)
			if e != nil {
				t.Fatalf("unspecified legacy bounds rejected: %v", e)
			}
			if !reflect.DeepEqual(d, before) {
				t.Fatal("normalizer rewrote legacy metadata")
			}
			page := index.Pages[v.sector]
			if page.BoundsMin != v.min || page.BoundsMax != v.max {
				t.Fatalf("derived page coverage got %v..%v want%v..%v", page.BoundsMin, page.BoundsMax, v.min, v.max)
			}
			sector := index.Sectors[v.sector]
			if sector.BoundsMin != ([3]float32{}) || sector.BoundsMax != ([3]float32{}) {
				t.Fatal("indexed visibility sector lost unspecified authored bounds")
			}
			p := filepath.Join(t.TempDir(), "unspecified.gkworld")
			b, e := json.Marshal(d)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(p, b, 0600); e != nil {
				t.Fatal(e)
			}
			loaded, e := LoadImportedWorld(p)
			if e != nil || loaded == nil {
				t.Fatalf("legacy reader rejected unspecified bounds: %v", e)
			}
			if loaded.SchemaVersion != 2 || loaded.PageIndex == nil {
				t.Fatal("reader missing canonical indexed legacy view")
			}
			got := loaded.Sectors[v.sector]
			if got.BoundsMin != ([3]float32{}) || got.BoundsMax != ([3]float32{}) {
				t.Fatal("reader rewrote authored sector bounds")
			}
			if !reflect.DeepEqual(loaded.PageIndex, index) {
				t.Fatal("reader index differs from pure normalization")
			}
		})
	}
	// Explicit nonzero degenerate metadata is malformed, rather than unspecified.
	for _, bounds := range []struct{ min, max [3]float32 }{{[3]float32{1, 1, 1}, [3]float32{1, 1, 1}}, {[3]float32{}, [3]float32{0, 1, 1}}, {[3]float32{1, 0, 0}, [3]float32{1, 1, 1}}} {
		d := i05LegacyWorld()
		d.Sectors[0].BoundsMin = bounds.min
		d.Sectors[0].BoundsMax = bounds.max
		if got, e := NormalizeImportedWorldPages(d); e == nil || got != nil {
			t.Fatal("explicit degenerate bounds admitted")
		}
	}
	d := i05LegacyWorld()
	d.Sectors[0].BoundsMin = [3]float32{}
	d.Sectors[0].BoundsMax = [3]float32{}
	d.Sectors[0].FullChunkRefs = nil
	d.Entries = d.Entries[2:]
	if got, e := NormalizeImportedWorldPages(d); e == nil || got != nil {
		t.Fatal("unspecified empty sector admitted")
	}
}
