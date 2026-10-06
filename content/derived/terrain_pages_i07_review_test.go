package derived

import (
	"math"
	"testing"
	"time"

	"github.com/gekko3d/gekko/content"
)

func TestI07ReviewSparseHugePageVisitsOnlyBackingSources(t *testing.T) {
	d, tiles := i07Fixture(8, content.TerrainChunkCoordDef{})
	opts := i07Options()
	opts.RootSpan = 1 << 40
	opts.RootSampleSpacing = 1 << 38
	opts.RootVoxelResolution = 1 << 38
	type result struct {
		bake *TerrainPageBake
		err  error
	}
	done := make(chan result, 1)
	go func() {
		bake, err := BuildTerrainPageBake(d, tiles, opts)
		done <- result{bake, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if len(got.bake.Manifest.Pages) != 6 {
			t.Fatal("sparse page span invented empty branches")
		}
		for _, page := range got.bake.Manifest.Pages {
			if page.Level != content.StreamPageLevelRoot {
				continue
			}
			tile := got.bake.PageTiles[page.Payload.Path]
			if tile.SampleWidth != 4 || !i07Valid(tile, 0) || math.Abs(i07Height(tile, 0)-131.5) > .501 {
				t.Fatal("sparse root lost original source-cell weights")
			}
			for i := 1; i < len(tile.HeightSamples); i++ {
				if i07Valid(tile, i) {
					t.Fatal("sparse root invented ground outside source")
				}
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bounded sparse bake scanned empty lattice space")
	}
}
