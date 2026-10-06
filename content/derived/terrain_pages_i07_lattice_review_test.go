package derived

import (
	"github.com/gekko3d/gekko/content"
	"path/filepath"
	"testing"
)

func i07LatticeSource(spacing float32) (*content.TerrainChunkManifestDef, map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
	d, tiles := i07Fixture(8, content.TerrainChunkCoordDef{})
	d.VoxelResolution = spacing
	for coord, tile := range tiles {
		tile.SampleSpacing = spacing
		tile.WorldOrigin = [3]float32{float32(coord.X) * 8 * spacing, 0, float32(coord.Z) * 8 * spacing}
	}
	for i, entry := range d.Entries {
		d.Entries[i] = i07Ref(tiles[entry.Coord])
	}
	return d, tiles
}

func TestI07LatticeRejectsLargeFractionalTierBeforeBuilding(t *testing.T) {
	opts := TerrainPageBakeOptions{RegionalSpan: 24, RegionalSampleSpacing: 3, RegionalVoxelResolution: 3, MacroSpan: 48, MacroSampleSpacing: 6, MacroVoxelResolution: 6, RootSpan: 3145727, RootSampleSpacing: 24576, RootVoxelResolution: 24576}
	for _, empty := range []bool{false, true} {
		name := "populated"
		if empty {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			d, tiles := i07LatticeSource(3)
			if empty {
				d.Entries = nil
				tiles = map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef{}
			}
			// 3145727/3 and 3145727/24576 are nonintegral, and the root/macro
			// span ratio is not a power of two. Relative tolerances must not admit them.
			if got, e := BuildTerrainPageBake(d, tiles, opts); e == nil || got != nil {
				t.Fatal("large fractional tier passed lattice validation")
			}
		})
	}
}

func TestI07LatticeWholeLargeAndDecimalGridsRemainValid(t *testing.T) {
	for _, test := range []struct {
		name    string
		spacing float32
		options TerrainPageBakeOptions
	}{
		{"large-whole", 3, TerrainPageBakeOptions{RegionalSpan: 24, RegionalSampleSpacing: 3, RegionalVoxelResolution: 3, MacroSpan: 48, MacroSampleSpacing: 6, MacroVoxelResolution: 6, RootSpan: 3145728, RootSampleSpacing: 24576, RootVoxelResolution: 24576}},
		{"decimal", .1, TerrainPageBakeOptions{RegionalSpan: .8, RegionalSampleSpacing: .1, RegionalVoxelResolution: .1, MacroSpan: 1.6, MacroSampleSpacing: .2, MacroVoxelResolution: .2, RootSpan: 3.2, RootSampleSpacing: .4, RootVoxelResolution: .4}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, tiles := i07LatticeSource(test.spacing)
			b, e := BuildTerrainPageBake(d, tiles, test.options)
			if e != nil {
				t.Fatalf("aligned finite grid rejected: %v", e)
			}
			path := filepath.Join(t.TempDir(), "terrain.gkterrainmanifest")
			if e = SaveTerrainPageBake(path, b); e != nil {
				t.Fatal(e)
			}
			loaded, e := content.LoadTerrainChunkManifest(path)
			if e != nil || loaded.PageIndex == nil {
				t.Fatalf("aligned grid publication: %v", e)
			}
			for i := range loaded.Pages {
				if _, e = content.LoadTerrainHeightPagePayload(loaded, path, uint32(i)); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
