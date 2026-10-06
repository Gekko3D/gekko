package derived

import (
	"github.com/gekko3d/gekko/content"
	"path/filepath"
	"testing"
)

func TestI07VisualRatioReviewRejectsLargeNonIntegralRatioBeforeBuilding(t *testing.T) {
	for _, empty := range []bool{false, true} {
		name := "populated"
		if empty {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			d, tiles := i07Fixture(8, content.TerrainChunkCoordDef{})
			if empty {
				d.Entries = nil
				tiles = map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef{}
			}
			opts := i07Options()
			opts.RootSpan = 1 << 30
			opts.RootSampleSpacing = 1 << 28
			opts.RootVoxelResolution = 3
			// The ratio is 89478485+1/3. Converting its rounded integer to float32
			// discards units, so multiplication roundtrip alone cannot prove integrality.
			if got, e := BuildTerrainPageBake(d, tiles, opts); e == nil || got != nil {
				t.Fatal("large nonintegral visual ratio accepted before building")
			}
		})
	}
}

func TestI07VisualRatioReviewLargeIntegralGridStillPublishes(t *testing.T) {
	for _, empty := range []bool{false, true} {
		name := "populated"
		if empty {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			d, tiles := i07Fixture(8, content.TerrainChunkCoordDef{})
			if empty {
				d.Entries = nil
				tiles = map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef{}
			}
			opts := i07Options()
			opts.RootSpan = 1 << 30
			opts.RootSampleSpacing = 1 << 28
			opts.RootVoxelResolution = 4
			b, e := BuildTerrainPageBake(d, tiles, opts)
			if e != nil {
				t.Fatalf("aligned large visual ratio rejected: %v", e)
			}
			path := filepath.Join(t.TempDir(), "terrain.gkterrainmanifest")
			if e = SaveTerrainPageBake(path, b); e != nil {
				t.Fatal(e)
			}
			loaded, e := content.LoadTerrainChunkManifest(path)
			if e != nil {
				t.Fatal(e)
			}
			for i := range loaded.Pages {
				if _, e = content.LoadTerrainHeightPagePayload(loaded, path, uint32(i)); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
