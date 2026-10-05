package derived

import (
	"fmt"
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestI06ReviewParentSpanRatiosRequirePowersOfTwo(t *testing.T) {
	d, c := i06Source()
	o := i06Options()
	o.MacroSpan = 24
	o.RootSpan = 48
	if _, e := BuildImportedWorldPageBake(d, c, o); e == nil {
		t.Fatal("integral3x parent ratio accepted; alignedhierarchy requires powerof2 ratios")
	}
}

func TestI06ReviewDecimalLeafOriginMatchesRuntimeFloat32Order(t *testing.T) {
	const size = 12
	const res = float32(.1)
	candidate := 0
	for n := 1; n <= 1024; n++ {
		if float32(n*size)*res != float32(n)*(float32(size)*res) {
			candidate = n
			break
		}
	}
	if candidate == 0 {
		t.Fatal("no decimal operation-order fixture")
	}
	for _, x := range []int{candidate, -candidate} {
		t.Run(fmt.Sprint(x), func(t *testing.T) {
			d, c := i06Source()
			d.ChunkSize = size
			d.VoxelResolution = res
			d.Entries = d.Entries[:1]
			coord := content.TerrainChunkCoordDef{X: x}
			d.Entries[0].Coord = coord
			chunk := c[content.TerrainChunkCoordDef{}]
			chunk.Coord = coord
			chunk.ChunkSize = size
			chunk.VoxelResolution = res
			c = map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{coord: chunk}
			b := i06Build(t, d, c, ImportedWorldPageBakeOptions{RegionalSpan: 4.8, MacroSpan: 9.6, RootSpan: 19.2, RegionalResolution: 1.2, MacroResolution: 2.4, RootResolution: 4.8})
			want := float32(x) * (float32(size) * res)
			found := false
			for _, p := range b.Manifest.Pages {
				if p.Level == content.StreamPageLevelLeaf {
					found = true
					if p.Payload.WorldOrigin[0] != want {
						t.Errorf("leaforigin %.9g want runtime %.9g", p.Payload.WorldOrigin[0], want)
					}
					if p.BoundsMin[0] > want || p.BoundsMax[0] < want+float32(size)*res {
						t.Error("leafbounds fail tocover actualruntimecube")
					}
				}
			}
			if !found {
				t.Fatal("no leafpage")
			}
		})
	}
}
func i06ReviewCosts(chunk *content.ImportedWorldChunkDef) (int, int) {
	sectors, bricks := map[[3]int]bool{}, map[[3]int]bool{}
	for _, v := range chunk.Voxels {
		if v.Value == 0 {
			continue
		}
		sectors[[3]int{v.X / volume.SectorSize, v.Y / volume.SectorSize, v.Z / volume.SectorSize}] = true
		bricks[[3]int{v.X / volume.BrickSize, v.Y / volume.BrickSize, v.Z / volume.BrickSize}] = true
	}
	return len(sectors), len(bricks)
}
func TestI06ReviewPublishedOccupancyCostsMatchActualPayloads(t *testing.T) {
	d, c := i06Source()
	b := i06Build(t, d, c, i06Options())
	p := filepath.Join(t.TempDir(), "world.gkworld")
	if e := SaveImportedWorldPageBake(p, b); e != nil {
		t.Fatal(e)
	}
	m, e := content.LoadImportedWorld(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range m.Entries {
		chunk, e := content.LoadImportedWorldChunk(content.ResolveImportedWorldChunkPath(entry, p))
		if e != nil {
			t.Fatal(e)
		}
		s, br := i06ReviewCosts(chunk)
		if entry.OccupiedSectorCount != s || entry.OccupiedBrickCount != br {
			t.Errorf("fullentry%v costs(%d,%d) want(%d,%d)", entry.Coord, entry.OccupiedSectorCount, entry.OccupiedBrickCount, s, br)
		}
	}
	for i, page := range m.Pages {
		chunk, e := content.LoadImportedWorldPagePayload(m, p, uint32(i))
		if e != nil {
			t.Fatal(e)
		}
		s, br := i06ReviewCosts(chunk)
		if page.Payload.OccupiedSectorCount != s || page.Payload.OccupiedBrickCount != br {
			t.Errorf("pagelevel%d costs(%d,%d) want(%d,%d)", page.Level, page.Payload.OccupiedSectorCount, page.Payload.OccupiedBrickCount, s, br)
		}
	}
}
func TestI06ReviewNoInventedCrossLayerCoverageGroups(t *testing.T) {
	d, c := i06Source()
	b := i06Build(t, d, c, i06Options())
	for _, p := range b.Manifest.Pages {
		if p.CoverageGroup != "" {
			t.Errorf("pagelevel%d invents crosslayer group %q without authoredreplacementset", p.Level, p.CoverageGroup)
		}
	}
}

func i06ReviewVisibilitySource() (*content.ImportedWorldDef, map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef) {
	d, c := i06Source()
	for _, x := range []int{1, -2} {
		base := c[content.TerrainChunkCoordDef{}]
		copy := *base
		copy.Coord = content.TerrainChunkCoordDef{X: x}
		copy.Voxels = append([]content.ImportedWorldVoxelDef(nil), base.Voxels...)
		c[copy.Coord] = &copy
		d.Entries = append(d.Entries, content.ImportedWorldChunkEntryDef{Coord: copy.Coord, ChunkPath: fmt.Sprintf("full%d.gkchunk", x), NonEmptyVoxelCount: len(copy.Voxels)})
	}
	zero, negative := content.TerrainChunkCoordDef{}, content.TerrainChunkCoordDef{X: -1}
	d.Sectors = []content.ImportedWorldSectorDef{{Coord: zero, BoundsMax: [3]float32{8, 4, 4}, FullChunkRefs: []content.TerrainChunkCoordDef{{X: 0}, {X: 1}}, VisibleSectorRefs: []content.TerrainChunkCoordDef{zero, negative}, AdjacentSectorRefs: []content.TerrainChunkCoordDef{negative, zero}}, {Coord: negative, BoundsMin: [3]float32{-8, 0, 0}, BoundsMax: [3]float32{0, 4, 4}, FullChunkRefs: []content.TerrainChunkCoordDef{{X: -1}, {X: -2}}, VisibleSectorRefs: []content.TerrainChunkCoordDef{negative, zero}, AdjacentSectorRefs: []content.TerrainChunkCoordDef{zero, negative}}}
	return d, c
}
func TestI06ReviewSectorPermutationCanonicalizesMembershipAndFiles(t *testing.T) {
	d, c := i06ReviewVisibilitySource()
	first := i06Build(t, d, c, i06Options())
	d.Entries[0], d.Entries[3] = d.Entries[3], d.Entries[0]
	d.Sectors[0], d.Sectors[1] = d.Sectors[1], d.Sectors[0]
	for i := range d.Sectors {
		for _, refs := range [][]content.TerrainChunkCoordDef{d.Sectors[i].FullChunkRefs, d.Sectors[i].VisibleSectorRefs, d.Sectors[i].AdjacentSectorRefs} {
			refs[0], refs[1] = refs[1], refs[0]
		}
	}
	second := i06Build(t, d, c, i06Options())
	for _, b := range []*ImportedWorldPageBake{first, second} {
		for _, sector := range b.Manifest.IndexedSectors {
			var got []int
			for _, i := range sector.FullChunkIndices {
				got = append(got, b.Manifest.Entries[i].Coord.X)
			}
			sort.Ints(got)
			want := []int{0, 1}
			if sector.Coord.X == -1 {
				want = []int{-2, -1}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("visibility membership changed %v =>%v", sector.Coord, got)
			}
			for _, refs := range [][]uint32{sector.VisibleSectorIndices, sector.AdjacentSectorIndices} {
				var coords []int
				for _, i := range refs {
					coords = append(coords, b.Manifest.IndexedSectors[i].Coord.X)
				}
				sort.Ints(coords)
				if !reflect.DeepEqual(coords, []int{-1, 0}) {
					t.Fatal("PVSindices were not remapped tooriginalsectorcoords")
				}
			}
		}
	}
	dir, dir2 := t.TempDir(), t.TempDir()
	if e := SaveImportedWorldPageBake(filepath.Join(dir, "world.gkworld"), first); e != nil {
		t.Fatal(e)
	}
	if e := SaveImportedWorldPageBake(filepath.Join(dir2, "world.gkworld"), second); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(i06Files(t, dir), i06Files(t, dir2)) {
		t.Fatal("sector/ref inputpermutation changes manifest or payload bytes")
	}
}
