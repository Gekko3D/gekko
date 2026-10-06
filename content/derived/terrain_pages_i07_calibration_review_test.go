package derived

import (
	"github.com/gekko3d/gekko/content"
	"math"
	"path/filepath"
	"testing"
)

func TestI07CalibrationGlobalRangeContainsFloat32SourceEndpoints(t *testing.T) {
	d, tiles := i07Fixture(1, content.TerrainChunkCoordDef{}, content.TerrainChunkCoordDef{X: 1})
	low := tiles[content.TerrainChunkCoordDef{}]
	low.HeightOffset = -1000000
	low.HeightScale = 1
	low.HeightSamples[0] = 65535
	high := tiles[content.TerrainChunkCoordDef{X: 1}]
	high.HeightOffset = 1000000
	high.HeightScale = .0625
	high.HeightSamples[0] = 65535
	minHeight, maxHeight := math.Inf(1), math.Inf(-1)
	for i, entry := range d.Entries {
		tile := tiles[entry.Coord]
		d.Entries[i] = i07Ref(tile)
		minHeight = math.Min(minHeight, float64(tile.HeightOffset))
		maxHeight = math.Max(maxHeight, float64(tile.HeightOffset)+float64(tile.HeightScale))
	}
	// Both source intervals have representable positive float32 height extents.
	// Their combined width lies between float32 values, so rounding it downward
	// would clip the high source endpoint despite individually valid calibration.
	b := i07Build(t, d, tiles, i07Options())
	check := func(p content.StreamPageDef) {
		t.Helper()
		if float64(p.Payload.HeightOffset) > minHeight || float64(p.Payload.HeightOffset)+float64(p.Payload.HeightScale) < maxHeight || float64(p.Payload.HeightOffset+p.Payload.HeightScale) < maxHeight || float64(p.BoundsMin[1]) > minHeight || float64(p.BoundsMax[1]) < maxHeight {
			t.Fatalf("global range clipped original source interval [%g,%g]: offset%g scale%g bounds[%g,%g]", minHeight, maxHeight, p.Payload.HeightOffset, p.Payload.HeightScale, p.BoundsMin[1], p.BoundsMax[1])
		}
	}
	if len(b.Manifest.Pages) == 0 {
		t.Fatal("valid source had no visual pages")
	}
	for _, p := range b.Manifest.Pages {
		check(p)
	}
	path := filepath.Join(t.TempDir(), "terrain.gkterrainmanifest")
	if e := SaveTerrainPageBake(path, b); e != nil {
		t.Fatal(e)
	}
	loaded, e := content.LoadTerrainChunkManifest(path)
	if e != nil {
		t.Fatal(e)
	}
	for i, p := range loaded.Pages {
		check(p)
		tile, e := content.LoadTerrainHeightPagePayload(loaded, path, uint32(i))
		if e != nil {
			t.Fatal(e)
		}
		if float64(tile.HeightOffset) > minHeight || float64(tile.HeightOffset)+float64(tile.HeightScale) < maxHeight || float64(tile.HeightOffset+tile.HeightScale) < maxHeight {
			t.Fatal("published height codec lost conservative global calibration")
		}
	}
}
