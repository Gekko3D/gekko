package derived

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func i07Raw(tile *content.TerrainHeightTileDef) []byte {
	raw := make([]byte, 2*len(tile.HeightSamples))
	for i, v := range tile.HeightSamples {
		binary.LittleEndian.PutUint16(raw[2*i:], v)
	}
	raw = append(raw, tile.SurfaceMask...)
	return append(raw, tile.OutdoorNavExclusionMask...)
}
func i07Ref(tile *content.TerrainHeightTileDef) content.TerrainChunkEntryDef {
	raw := i07Raw(tile)
	hash := sha256.Sum256(raw)
	return content.TerrainChunkEntryDef{TerrainID: tile.TerrainID, SourceHash: tile.SourceHash, Coord: tile.Coord, WorldOrigin: tile.WorldOrigin, ChunkSize: tile.SampleWidth, VoxelResolution: tile.SampleSpacing, HeightOffset: tile.HeightOffset, HeightScale: tile.HeightScale, ChunkPath: "source_" + tile.Coord.String() + ".gkchunk", PayloadKind: content.TerrainHeightTilePayloadKind, PayloadHash: hex.EncodeToString(hash[:]), PayloadSizeBytes: len(raw)}
}
func i07Fixture(side int, coords ...content.TerrainChunkCoordDef) (*content.TerrainChunkManifestDef, map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
	d := &content.TerrainChunkManifestDef{SchemaVersion: 3, TerrainID: "terrain-pages", SourceHash: strings.Repeat("a", 64), ChunkSize: side, VoxelResolution: 2}
	tiles := map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef{}
	for _, coord := range coords {
		tile := &content.TerrainHeightTileDef{SchemaVersion: 1, TerrainID: d.TerrainID, SourceHash: d.SourceHash, Coord: coord, WorldOrigin: [3]float32{float32(coord.X) * float32(side) * 2, 0, float32(coord.Z) * float32(side) * 2}, SampleWidth: side, SampleHeight: side, SampleSpacing: 2, HeightScale: 65535, HeightSamples: make([]uint16, side*side)}
		for i := range tile.HeightSamples {
			tile.HeightSamples[i] = uint16(100 + i)
		}
		tiles[coord] = tile
		d.Entries = append(d.Entries, i07Ref(tile))
	}
	return d, tiles
}
func i07Options() TerrainPageBakeOptions {
	return TerrainPageBakeOptions{RegionalSpan: 8, MacroSpan: 16, RootSpan: 32, RegionalSampleSpacing: 2, MacroSampleSpacing: 4, RootSampleSpacing: 8, RegionalVoxelResolution: 1, MacroVoxelResolution: 2, RootVoxelResolution: 4}
}
func i07Build(t *testing.T, d *content.TerrainChunkManifestDef, tiles map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef, o TerrainPageBakeOptions) *TerrainPageBake {
	t.Helper()
	b, e := BuildTerrainPageBake(d, tiles, o)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func i07Valid(tile *content.TerrainHeightTileDef, i int) bool {
	return len(tile.SurfaceMask) == 0 || tile.SurfaceMask[i/8]&(1<<uint(i%8)) != 0
}
func i07Height(tile *content.TerrainHeightTileDef, i int) float64 {
	return float64(tile.HeightOffset) + float64(tile.HeightSamples[i])/65535*float64(tile.HeightScale)
}
func i07CopyTile(tile *content.TerrainHeightTileDef) *content.TerrainHeightTileDef {
	c := *tile
	c.HeightSamples = append([]uint16(nil), tile.HeightSamples...)
	c.SurfaceMask = append([]byte(nil), tile.SurfaceMask...)
	c.OutdoorNavExclusionMask = append([]byte(nil), tile.OutdoorNavExclusionMask...)
	return &c
}
func i07Files(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	if e := filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(dir, p)
		if e != nil {
			return e
		}
		files[rel], e = os.ReadFile(p)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	return files
}

func TestI07TerrainDefaultVisualGridIsIndependentOfBackingTiles(t *testing.T) {
	coord := content.TerrainChunkCoordDef{}
	d, tiles := i07Fixture(128, coord)
	tile := tiles[coord]
	tile.SurfaceMask = make([]byte, 2048)
	for i := range tile.SurfaceMask {
		tile.SurfaceMask[i] = 255
	}
	tile.OutdoorNavCellSize = 1
	tile.OutdoorNavExclusionMask = make([]byte, 8192)
	tile.OutdoorNavExclusionMask[3] = 165
	d.Entries[0] = i07Ref(tile)
	b := i07Build(t, d, tiles, TerrainPageBakeOptions{})
	if b.Manifest.SchemaVersion != 3 || len(b.Manifest.Entries) != 1 || len(b.SourceTiles) != 1 {
		t.Fatal("source ownership changed")
	}
	counts := map[uint8]int{}
	origins := map[[2]float32]bool{}
	for _, page := range b.Manifest.Pages {
		counts[page.Level]++
		if len(page.LeafEntryIndices) != 0 {
			t.Fatal("visual page claimed unique backing source ownership")
		}
		p := page.Payload
		out := b.PageTiles[p.Path]
		if out == nil {
			t.Fatal("missing page payload")
		}
		if out.OutdoorNavCellSize != 0 || len(out.OutdoorNavExclusionMask) != 0 {
			t.Fatal("coarse page copied authoritative nav")
		}
		if page.Level == content.StreamPageLevelRegional {
			if out.SampleWidth != 64 || out.SampleHeight != 64 || out.SampleSpacing != 2 || p.VoxelResolution != 1 {
				t.Fatal("default regional data/visual grid")
			}
			origins[[2]float32{p.WorldOrigin[0], p.WorldOrigin[2]}] = true
		}
	}
	if counts[content.StreamPageLevelRegional] != 4 || counts[content.StreamPageLevelMacro] != 1 || counts[content.StreamPageLevelRoot] != 1 || counts[content.StreamPageLevelLeaf] != 0 {
		t.Fatalf("visual forest counts %v", counts)
	}
	for _, origin := range [][2]float32{{0, 0}, {128, 0}, {0, 128}, {128, 128}} {
		if !origins[origin] {
			t.Fatalf("missing 128m visual quadrant %v", origin)
		}
	}
	oldPath := b.Manifest.Entries[0].ChunkPath
	tile.OutdoorNavExclusionMask[3] ^= 1
	d.Entries[0] = i07Ref(tile)
	navChanged := i07Build(t, d, tiles, TerrainPageBakeOptions{})
	if navChanged.Manifest.Entries[0].ChunkPath == oldPath {
		t.Fatal("whole generation omitted JSON-excluded navigation bytes")
	}
	tile.OutdoorNavExclusionMask[3] ^= 1
	d.Entries[0] = i07Ref(tile)
	sourceCopy := b.SourceTiles[coord]
	if !reflect.DeepEqual(sourceCopy.OutdoorNavExclusionMask, tile.OutdoorNavExclusionMask) || sourceCopy.OutdoorNavCellSize != 1 {
		t.Fatal("backing nav changed")
	}
	sourceCopy.SurfaceMask[0] ^= 1
	sourceCopy.HeightSamples[0]++
	sourceCopy.OutdoorNavExclusionMask[3] ^= 1
	if tile.SurfaceMask[0] != 255 || tile.HeightSamples[0] != 100 || tile.OutdoorNavExclusionMask[3] != 165 {
		t.Fatal("JSON-excluded source arrays alias")
	}
}

func TestI07TerrainSignedSeamsAndSameSpacingPreserveSourceSamples(t *testing.T) {
	d, tiles := i07Fixture(8, content.TerrainChunkCoordDef{X: 0, Z: 1}, content.TerrainChunkCoordDef{X: -1, Z: 0}, content.TerrainChunkCoordDef{})
	b := i07Build(t, d, tiles, i07Options())
	if b.Manifest.Entries[0].Coord != (content.TerrainChunkCoordDef{X: -1}) {
		t.Fatal("source entries not Z/X canonical")
	}
	seen := map[[2]int]bool{}
	for _, page := range b.Manifest.Pages {
		if page.Level != content.StreamPageLevelRegional {
			continue
		}
		out := b.PageTiles[page.Payload.Path]
		for z := 0; z < out.SampleHeight; z++ {
			for x := 0; x < out.SampleWidth; x++ {
				i := z*out.SampleWidth + x
				if !i07Valid(out, i) {
					continue
				}
				wx := float64(out.WorldOrigin[0]) + (float64(x)+.5)*float64(out.SampleSpacing)
				wz := float64(out.WorldOrigin[2]) + (float64(z)+.5)*float64(out.SampleSpacing)
				key := [2]int{int(math.Floor(wx / 2)), int(math.Floor(wz / 2))}
				if seen[key] {
					t.Fatal("overlapping regional source cells")
				}
				seen[key] = true
				found := false
				for _, source := range tiles {
					sx := int(math.Floor((wx - float64(source.WorldOrigin[0])) / 2))
					sz := int(math.Floor((wz - float64(source.WorldOrigin[2])) / 2))
					if sx >= 0 && sx < 8 && sz >= 0 && sz < 8 {
						found = true
						if math.Abs(i07Height(out, i)-i07Height(source, sz*8+sx)) > float64(out.HeightScale)/65535*.501 {
							t.Fatal("same-grid height changed beyond quantization")
						}
					}
				}
				if !found {
					t.Fatal("invented source surface across seam")
				}
			}
		}
	}
	if len(seen) != 3*64 {
		t.Fatalf("source cells lost across signed seams: %d", len(seen))
	}
}

func TestI07TerrainEveryTierUsesOriginalValidCellWeightedMeans(t *testing.T) {
	coord := content.TerrainChunkCoordDef{}
	d, tiles := i07Fixture(8, coord)
	tile := tiles[coord]
	for i := range tile.HeightSamples {
		tile.HeightSamples[i] = 0
	}
	tile.HeightSamples[0] = 60000
	tile.SurfaceMask = make([]byte, 8)
	for i := range tile.SurfaceMask {
		tile.SurfaceMask[i] = 255
	}
	for _, i := range []int{1, 8, 9} {
		tile.SurfaceMask[i/8] &^= 1 << uint(i%8)
	}
	d.Entries[0] = i07Ref(tile)
	b := i07Build(t, d, tiles, i07Options())
	found := false
	for _, p := range b.Manifest.Pages {
		if p.Level != content.StreamPageLevelRoot {
			continue
		}
		out := b.PageTiles[p.Payload.Path]
		if out.WorldOrigin[0] == 0 && out.WorldOrigin[2] == 0 {
			found = true
			if !i07Valid(out, 0) {
				t.Fatal("any-valid source cell became absent")
			}
			want := float64(60000) / 13
			if math.Abs(i07Height(out, 0)-want) > .501 {
				t.Fatalf("root average %.9g want original 13-cell average %.9g (mean-of-means would 15000)", i07Height(out, 0), want)
			}
		}
	}
	if !found {
		t.Fatal("root containing weighted fixture missing")
	}
	for _, p := range b.Manifest.Pages {
		if p.Level != content.StreamPageLevelRoot {
			continue
		}
		out := b.PageTiles[p.Payload.Path]
		for z := 0; z < out.SampleHeight; z++ {
			for x := 0; x < out.SampleWidth; x++ {
				if x >= 2 || z >= 2 {
					if i07Valid(out, z*out.SampleWidth+x) {
						t.Fatal("root invented surface outside backing source")
					}
				}
			}
		}
	}
	// Wholly masked source data supplies no visual ground and no fake pages.
	for i := range tile.SurfaceMask {
		tile.SurfaceMask[i] = 0
	}
	d.Entries[0] = i07Ref(tile)
	empty := i07Build(t, d, tiles, i07Options())
	if len(empty.Manifest.Pages) != 0 || len(empty.Manifest.RootPageIndices) != 0 || len(empty.PageTiles) != 0 || len(empty.Manifest.Entries) != 1 {
		t.Fatal("all-masked source invented a visual forest or discarded backing")
	}
	if e := SaveTerrainPageBake(filepath.Join(t.TempDir(), "empty.gkterrainchunks"), empty); e != nil {
		t.Fatal(e)
	}
}

func TestI07TerrainPublicationIdentityDeterminismAndFailureIsolation(t *testing.T) {
	d, tiles := i07Fixture(8, content.TerrainChunkCoordDef{}, content.TerrainChunkCoordDef{X: -1})
	b := i07Build(t, d, tiles, i07Options())
	dir := t.TempDir()
	path := filepath.Join(dir, "world.gkterrainchunks")
	if e := SaveTerrainPageBake(path, b); e != nil {
		t.Fatal(e)
	}
	loaded, e := content.LoadTerrainChunkManifest(path)
	if e != nil {
		t.Fatal(e)
	}
	if len(loaded.Pages) == 0 || len(b.Manifest.Pages) == 0 {
		t.Fatal("saved render pages missing")
	}
	for _, entry := range loaded.Entries {
		out, e := content.LoadTerrainHeightTileEntry(entry, path)
		if e != nil {
			t.Fatal(e)
		}
		if out.HeightOffset != entry.HeightOffset || out.HeightScale != entry.HeightScale || !reflect.DeepEqual(out.HeightSamples, tiles[entry.Coord].HeightSamples) {
			t.Fatal("full source emission changed array or calibration")
		}
	}
	for _, page := range loaded.Pages {
		p := page.Payload
		out, e := content.LoadTerrainHeightTile(content.ResolveDocumentPath(p.Path, path))
		if e != nil {
			t.Fatal(e)
		}
		raw := i07Raw(out)
		h := sha256.Sum256(raw)
		if p.PayloadHash != hex.EncodeToString(h[:]) || p.PayloadSizeBytes != len(raw) || p.Kind != content.TerrainHeightTilePayloadKind || out.TerrainID != loaded.TerrainID || out.SourceHash != loaded.SourceHash || out.WorldOrigin != p.WorldOrigin || out.SampleWidth != p.ChunkSize || out.SampleHeight != p.ChunkSize || out.SampleSpacing != p.SampleSpacing || out.HeightOffset != p.HeightOffset || out.HeightScale != p.HeightScale {
			t.Fatal("page codec/header reference qualification")
		}
	}
	pointer := b.Manifest
	old := i07Files(t, dir)
	if e := SaveTerrainPageBake(path, b); e != nil {
		t.Fatal(e)
	}
	if b.Manifest != pointer || !reflect.DeepEqual(old, i07Files(t, dir)) {
		t.Fatal("repeat save changed public pointer or bytes")
	}
	d.Entries[0], d.Entries[1] = d.Entries[1], d.Entries[0]
	other := i07Build(t, d, tiles, i07Options())
	dir2 := t.TempDir()
	if e := SaveTerrainPageBake(filepath.Join(dir2, "world.gkterrainchunks"), other); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(old, i07Files(t, dir2)) {
		t.Fatal("shuffled source order changed files")
	}
	for _, tile := range b.PageTiles {
		saved := tile.HeightSamples[0]
		tile.HeightSamples[0]++
		if e := SaveTerrainPageBake(path, b); e == nil {
			t.Fatal("changed private-array draft accepted stale generation")
		}
		if !reflect.DeepEqual(old, i07Files(t, dir)) {
			t.Fatal("invalid mutation changed existing files")
		}
		tile.HeightSamples[0] = saved
		break
	}
	// A single source neighbor change requalifies all full-source paths, including unchanged data.
	tiles[content.TerrainChunkCoordDef{}].HeightSamples[0]++
	for i := range d.Entries {
		d.Entries[i] = i07Ref(tiles[d.Entries[i].Coord])
	}
	next := i07Build(t, d, tiles, i07Options())
	oldPaths := map[content.TerrainChunkCoordDef]string{}
	for _, entry := range b.Manifest.Entries {
		oldPaths[entry.Coord] = entry.ChunkPath
	}
	for _, entry := range next.Manifest.Entries {
		if entry.ChunkPath == oldPaths[entry.Coord] {
			t.Fatal("generation omitted raw source arrays or retained old geometry alias")
		}
	}
	var paths []string
	for p := range next.PageTiles {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("no next visual payload")
	}
	if e := os.MkdirAll(content.ResolveDocumentPath(paths[len(paths)-1], path), 0755); e != nil {
		t.Fatal(e)
	}
	if e := SaveTerrainPageBake(path, next); e == nil {
		t.Fatal("late directory blocker accepted")
	}
	for rel, want := range old {
		got, e := os.ReadFile(filepath.Join(dir, rel))
		if e != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("failed generation changed old file %s", rel)
		}
	}
}

func TestI07TerrainBakeRejectsMalformedSourceAndUnsafeOptions(t *testing.T) {
	for name, mutate := range map[string]func(*content.TerrainChunkManifestDef, map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef){"version": func(d *content.TerrainChunkManifestDef, m map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
		d.SchemaVersion = 2
	}, "missing": func(d *content.TerrainChunkManifestDef, m map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
		delete(m, d.Entries[0].Coord)
	}, "extra": func(d *content.TerrainChunkManifestDef, m map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
		m[content.TerrainChunkCoordDef{X: 2}] = i07CopyTile(m[d.Entries[0].Coord])
	}, "duplicate": func(d *content.TerrainChunkManifestDef, m map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
		d.Entries = append(d.Entries, d.Entries[0])
	}, "body-hash": func(d *content.TerrainChunkManifestDef, m map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
		m[d.Entries[0].Coord].HeightSamples[0]++
	}, "dimension": func(d *content.TerrainChunkManifestDef, m map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
		m[d.Entries[0].Coord].SampleHeight--
	}, "calibration": func(d *content.TerrainChunkManifestDef, m map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
		m[d.Entries[0].Coord].HeightOffset = 10
	}, "negative-coordinate-overflow": func(d *content.TerrainChunkManifestDef, m map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
		old := d.Entries[0].Coord
		tile := m[old]
		delete(m, old)
		coord := content.TerrainChunkCoordDef{X: -int(^uint(0)>>1) - 1}
		tile.Coord = coord
		tile.WorldOrigin[0] = float32(coord.X) * float32(tile.SampleWidth) * tile.SampleSpacing
		m[coord] = tile
		d.Entries[0] = i07Ref(tile)
	}, "lostprecision": func(d *content.TerrainChunkManifestDef, m map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
		old := d.Entries[0].Coord
		tile := m[old]
		delete(m, old)
		coord := content.TerrainChunkCoordDef{X: int(^uint(0) >> 1)}
		tile.Coord = coord
		tile.WorldOrigin[0] = float32(coord.X) * float32(tile.SampleWidth) * tile.SampleSpacing
		m[coord] = tile
		d.Entries[0] = i07Ref(tile)
	}, "nonfinite": func(d *content.TerrainChunkManifestDef, m map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
		m[d.Entries[0].Coord].HeightScale = float32(math.Inf(1))
	}} {
		t.Run(name, func(t *testing.T) {
			d, m := i07Fixture(8, content.TerrainChunkCoordDef{})
			mutate(d, m)
			if b, e := BuildTerrainPageBake(d, m, i07Options()); e == nil || b != nil {
				t.Fatal("invalid backing source accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*TerrainPageBakeOptions){"page-budget": func(o *TerrainPageBakeOptions) { o.MaxPages = 1 }, "source-budget": func(o *TerrainPageBakeOptions) { o.MaxSourceTiles = 1 }, "above-pages": func(o *TerrainPageBakeOptions) { o.MaxPages = 16385 }, "above-sources": func(o *TerrainPageBakeOptions) { o.MaxSourceTiles = 4097 }, "negative": func(o *TerrainPageBakeOptions) { o.MaxPages = -1 }, "negative-source": func(o *TerrainPageBakeOptions) { o.MaxSourceTiles = -1 }, "fractional-span": func(o *TerrainPageBakeOptions) { o.RegionalSpan = 7 }, "parent3": func(o *TerrainPageBakeOptions) { o.MacroSpan = 24; o.RootSpan = 48 }, "finer-data": func(o *TerrainPageBakeOptions) { o.RegionalSampleSpacing = 1 }, "nondivisor-visual": func(o *TerrainPageBakeOptions) { o.RegionalVoxelResolution = 3 }, "oversized-grid": func(o *TerrainPageBakeOptions) { o.RootSampleSpacing = 2; o.RootSpan = 1024 }, "nonfinite": func(o *TerrainPageBakeOptions) { o.RootSpan = float32(math.Inf(1)) }} {
		t.Run(name, func(t *testing.T) {
			d, m := i07Fixture(8, content.TerrainChunkCoordDef{}, content.TerrainChunkCoordDef{X: 1})
			o := i07Options()
			mutate(&o)
			if b, e := BuildTerrainPageBake(d, m, o); e == nil || b != nil {
				t.Fatal("unsafe page options accepted")
			}
		})
	}
	d, m := i07Fixture(8, content.TerrainChunkCoordDef{})
	o := i07Options()
	o.MaxPages = 16384
	o.MaxSourceTiles = 4096
	_ = i07Build(t, d, m, o)
	// Invalid draft must fail before creating target files.
	b := i07Build(t, d, m, o)
	for _, tile := range b.SourceTiles {
		tile.SurfaceMask = []byte{1}
		break
	}
	dir := t.TempDir()
	if e := SaveTerrainPageBake(filepath.Join(dir, "invalid.gkterrainchunks"), b); e == nil {
		t.Fatal("malformed draft accepted")
	}
	files, e := os.ReadDir(dir)
	if e != nil || len(files) != 0 {
		t.Fatal("invalid draft touched target files")
	}
}

func TestI07TerrainGlobalCalibrationAndIndependentWorldHeightMeans(t *testing.T) {
	d, tiles := i07Fixture(8, content.TerrainChunkCoordDef{}, content.TerrainChunkCoordDef{X: 1})
	tiles[content.TerrainChunkCoordDef{}].HeightOffset = -100
	tiles[content.TerrainChunkCoordDef{}].HeightScale = 200
	tiles[content.TerrainChunkCoordDef{X: 1}].HeightOffset = 1000
	tiles[content.TerrainChunkCoordDef{X: 1}].HeightScale = 9000
	for i := range d.Entries {
		d.Entries[i] = i07Ref(tiles[d.Entries[i].Coord])
	}
	originals := map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef{}
	for c, tile := range tiles {
		originals[c] = i07CopyTile(tile)
	}
	b := i07Build(t, d, tiles, i07Options())
	for _, page := range b.Manifest.Pages {
		out := b.PageTiles[page.Payload.Path]
		if out.HeightOffset != -100 || out.HeightScale != 10100 {
			t.Fatal("coarse calibration did not use global declared source endpoints")
		}
		for z := 0; z < out.SampleHeight; z++ {
			for x := 0; x < out.SampleWidth; x++ {
				i := z*out.SampleWidth + x
				loX := float64(out.WorldOrigin[0]) + float64(x)*float64(out.SampleSpacing)
				loZ := float64(out.WorldOrigin[2]) + float64(z)*float64(out.SampleSpacing)
				hiX, hiZ := loX+float64(out.SampleSpacing), loZ+float64(out.SampleSpacing)
				sum := 0.0
				count := 0
				for _, source := range originals {
					for sz := 0; sz < source.SampleHeight; sz++ {
						for sx := 0; sx < source.SampleWidth; sx++ {
							j := sz*source.SampleWidth + sx
							if !i07Valid(source, j) {
								continue
							}
							wx := float64(source.WorldOrigin[0]) + (float64(sx)+.5)*float64(source.SampleSpacing)
							wz := float64(source.WorldOrigin[2]) + (float64(sz)+.5)*float64(source.SampleSpacing)
							if wx >= loX && wx < hiX && wz >= loZ && wz < hiZ {
								sum += i07Height(source, j)
								count++
							}
						}
					}
				}
				if i07Valid(out, i) != (count > 0) {
					t.Fatal("validity disagrees with original source cell coverage")
				}
				if count > 0 && math.Abs(i07Height(out, i)-sum/float64(count)) > 10100.0/65535*.501 {
					t.Fatalf("tier%d world-height mean differs", page.Level)
				}
			}
		}
	}
	for coord, original := range originals {
		if !reflect.DeepEqual(original, tiles[coord]) {
			t.Fatal("build mutated source arrays or metadata")
		}
	}
}

func TestI07TerrainEmptyNilAndAlreadyPagedInputs(t *testing.T) {
	if b, e := BuildTerrainPageBake(nil, nil, TerrainPageBakeOptions{}); e == nil || b != nil {
		t.Fatal("nil backing source accepted")
	}
	d, tiles := i07Fixture(8)
	empty := i07Build(t, d, tiles, TerrainPageBakeOptions{})
	if len(empty.Manifest.Entries) != 0 || len(empty.Manifest.Pages) != 0 || len(empty.PageTiles) != 0 || len(empty.SourceTiles) != 0 {
		t.Fatal("empty source invented terrain")
	}
	if e := SaveTerrainPageBake(filepath.Join(t.TempDir(), "empty.gkterrainchunks"), empty); e != nil {
		t.Fatal(e)
	}
	d, tiles = i07Fixture(8, content.TerrainChunkCoordDef{})
	full := i07Build(t, d, tiles, i07Options())
	if b, e := BuildTerrainPageBake(full.Manifest, full.SourceTiles, i07Options()); e == nil || b != nil {
		t.Fatal("already-paged visual manifest admitted as raw backing source")
	}
	dir := t.TempDir()
	if e := SaveTerrainPageBake(filepath.Join(dir, "nil.gkterrainchunks"), nil); e == nil {
		t.Fatal("nil bake accepted")
	}
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) != 0 {
		t.Fatal("nil draft created target files")
	}
	// A valid shape and mask mutation still invalidates the sealed generation.
	coord := content.TerrainChunkCoordDef{}
	d, tiles = i07Fixture(8, coord)
	tiles[coord].SurfaceMask = make([]byte, 8)
	for i := range tiles[coord].SurfaceMask {
		tiles[coord].SurfaceMask[i] = 255
	}
	d.Entries[0] = i07Ref(tiles[coord])
	masked := i07Build(t, d, tiles, i07Options())
	masked.SourceTiles[coord].SurfaceMask[0] ^= 1
	if e := SaveTerrainPageBake(filepath.Join(dir, "mask.gkterrainchunks"), masked); e == nil {
		t.Fatal("changed source mask accepted stale identity")
	}
	entries, e = os.ReadDir(dir)
	if e != nil || len(entries) != 0 {
		t.Fatal("valid stale mask mutation touched target files")
	}
}

func TestI07TerrainPublicationNeverRewritesQualifiedPayloadCollisions(t *testing.T) {
	d, tiles := i07Fixture(8, content.TerrainChunkCoordDef{})
	b := i07Build(t, d, tiles, i07Options())
	dir := t.TempDir()
	manifest := filepath.Join(dir, "world.gkterrainchunks")
	if e := SaveTerrainPageBake(manifest, b); e != nil {
		t.Fatal(e)
	}
	var paths []string
	for path := range b.PageTiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("no page payload")
	}
	path := content.ResolveDocumentPath(paths[0], manifest)
	original, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	changed := append([]byte(nil), original...)
	changed[len(changed)-1] ^= 1
	if e = os.WriteFile(path, changed, 0600); e != nil {
		t.Fatal(e)
	}
	before := i07Files(t, dir)
	if e := SaveTerrainPageBake(manifest, b); e == nil {
		t.Fatal("immutable qualified payload collision overwritten")
	}
	if !reflect.DeepEqual(before, i07Files(t, dir)) {
		t.Fatal("collision rejection changed published manifest or files")
	}
}
