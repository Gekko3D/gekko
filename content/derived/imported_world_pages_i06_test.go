package derived

import (
	"encoding/binary"
	"encoding/json"
	"github.com/gekko3d/gekko/content"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func i06Source() (*content.ImportedWorldDef, map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef) {
	d := &content.ImportedWorldDef{WorldID: "page-source", SchemaVersion: 2, Kind: content.ImportedWorldKindVoxelWorld, ChunkSize: 4, VoxelResolution: 1, Palette: make([]content.ImportedWorldPaletteColor, 256), MaterialPalette: make([]content.ImportedWorldPaletteColor, 256)}
	chunks := map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{}
	for _, x := range []int{0, -1} {
		c := content.TerrainChunkCoordDef{X: x}
		voxels := []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1, MaterialValue: 6}, {X: 1, Y: 0, Z: 0, Value: 2, MaterialValue: 7}}
		d.Entries = append(d.Entries, content.ImportedWorldChunkEntryDef{Coord: c, ChunkPath: "full" + content.TerrainChunkKey(c) + ".gkchunk", NonEmptyVoxelCount: len(voxels)})
		chunks[c] = &content.ImportedWorldChunkDef{WorldID: d.WorldID, SchemaVersion: 1, Coord: c, ChunkSize: 4, VoxelResolution: 1, Voxels: voxels, NonEmptyVoxelCount: len(voxels), Tags: []string{"source"}}
	}
	return d, chunks
}
func i06Options() ImportedWorldPageBakeOptions {
	return ImportedWorldPageBakeOptions{RegionalSpan: 8, MacroSpan: 16, RootSpan: 32, RegionalResolution: 1, MacroResolution: 2, RootResolution: 4}
}
func i06JSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func i06Build(t *testing.T, d *content.ImportedWorldDef, chunks map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, opts ImportedWorldPageBakeOptions) *ImportedWorldPageBake {
	t.Helper()
	b, e := BuildImportedWorldPageBake(d, chunks, opts)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func i06RootPairs(t *testing.T, b *ImportedWorldPageBake) [][2]uint8 {
	t.Helper()
	var pairs [][2]uint8
	for _, p := range b.Manifest.Pages {
		if p.Level != content.StreamPageLevelRoot {
			continue
		}
		c := b.PageChunks[p.Payload.Path]
		if c == nil {
			t.Fatal("root payload missing")
		}
		for _, v := range c.Voxels {
			if v.Value != 0 {
				pairs = append(pairs, [2]uint8{v.Value, content.ImportedWorldVoxelMaterialValue(v)})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i][1] != pairs[j][1] {
			return pairs[i][1] < pairs[j][1]
		}
		return pairs[i][0] < pairs[j][0]
	})
	return pairs
}
func i06Files(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	if e := filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			rel, e := filepath.Rel(dir, p)
			if e != nil {
				return e
			}
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			out[rel] = b
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	return out
}

func TestI06PageBakeFourTiersSignedOriginsAndSourceOwnership(t *testing.T) {
	d, chunks := i06Source()
	before := i06JSON(t, d)
	beforeChunks := map[content.TerrainChunkCoordDef][]byte{}
	for c, v := range chunks {
		beforeChunks[c] = i06JSON(t, v)
	}
	b := i06Build(t, d, chunks, i06Options())
	if b.Manifest.SchemaVersion != 3 || len(b.Manifest.Entries) != 2 || len(b.Chunks) != 2 {
		t.Fatal("full source ownership/version")
	}
	levels := [4]bool{}
	for _, p := range b.Manifest.Pages {
		levels[p.Level] = true
		if p.Level == content.StreamPageLevelLeaf {
			continue
		}
		c := b.PageChunks[p.Payload.Path]
		if c == nil || c.Coord != (content.TerrainChunkCoordDef{}) || c.ChunkSize != p.Payload.ChunkSize || c.VoxelResolution != p.Payload.VoxelResolution {
			t.Fatalf("page local grid %+v", p)
		}
		for _, v := range c.Voxels {
			if v.X < 0 || v.Y < 0 || v.Z < 0 || v.X >= c.ChunkSize || v.Y >= c.ChunkSize || v.Z >= c.ChunkSize {
				t.Fatal("unbounded payload cell")
			}
		}
		side := float32(c.ChunkSize) * c.VoxelResolution
		for axis := 0; axis < 3; axis++ {
			if p.BoundsMin[axis] > p.Payload.WorldOrigin[axis] || p.BoundsMax[axis] < p.Payload.WorldOrigin[axis]+side {
				t.Fatal("declared cube not covered")
			}
		}
	}
	if levels != ([4]bool{true, true, true, true}) {
		t.Fatalf("hierarchy levels %v", levels)
	}
	if b.Manifest.Entries[0].Coord.X != -1 {
		t.Fatal("source entries not canonical beforeIDs")
	}
	if string(i06JSON(t, d)) != string(before) {
		t.Fatal("source manifest mutated")
	}
	for c, v := range chunks {
		if string(i06JSON(t, v)) != string(beforeChunks[c]) {
			t.Fatal("source payload mutated")
		}
	}
	b.Chunks[content.TerrainChunkCoordDef{}].Voxels[0].Value = 9
	b.Chunks[content.TerrainChunkCoordDef{}].Tags[0] = "changed"
	if chunks[content.TerrainChunkCoordDef{}].Voxels[0].Value != 1 || chunks[content.TerrainChunkCoordDef{}].Tags[0] != "source" {
		t.Fatal("full payload copy aliases source")
	}
}

func TestI06PageBakeAggregatesOriginalOpaquePairsWithStableTies(t *testing.T) {
	d, chunks := i06Source()
	d.Entries = d.Entries[:1]
	c := chunks[content.TerrainChunkCoordDef{}]
	chunks = map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{c.Coord: c}
	c.Voxels = []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1, MaterialValue: 6}, {X: 1, Y: 0, Z: 0, Value: 1, MaterialValue: 6}, {X: 0, Y: 1, Z: 0, Value: 1, MaterialValue: 6}, {X: 1, Y: 1, Z: 0, Value: 2, MaterialValue: 7}, {X: 0, Y: 0, Z: 1, Value: 2, MaterialValue: 7}, {X: 2, Y: 0, Z: 0, Value: 2, MaterialValue: 7}, {X: 3, Y: 0, Z: 0, Value: 2, MaterialValue: 7}}
	c.NonEmptyVoxelCount = len(c.Voxels)
	d.Entries[0].NonEmptyVoxelCount = len(c.Voxels)
	opts := i06Options()
	opts.RegionalResolution = 2
	opts.MacroResolution = 4
	opts.RootResolution = 8
	b := i06Build(t, d, chunks, opts)
	if pairs := i06RootPairs(t, b); !reflect.DeepEqual(pairs, [][2]uint8{{2, 7}}) {
		t.Fatalf("successive majority lost original counts: %v", pairs)
	}
	c.Voxels = []content.ImportedWorldVoxelDef{{X: 0, Value: 1, MaterialValue: 6}, {X: 1, Value: 2, MaterialValue: 5}}
	c.NonEmptyVoxelCount = 2
	d.Entries[0].NonEmptyVoxelCount = 2
	b = i06Build(t, d, chunks, opts)
	if pairs := i06RootPairs(t, b); !reflect.DeepEqual(pairs, [][2]uint8{{2, 5}}) {
		t.Fatalf("material tie order %v", pairs)
	}
	d.Materials = []content.ImportedWorldMaterialDef{{ID: 2, PaletteIndex: 2, Transparent: true}, {ID: 3, PaletteIndex: 3, Kind: "water"}, {ID: 4, PaletteIndex: 4, EmitsLight: true, Emissive: 1}}
	c.Voxels = []content.ImportedWorldVoxelDef{{X: 0, Value: 1, MaterialValue: 2}, {X: 1, Value: 2, MaterialValue: 3}, {X: 2, Value: 1, MaterialValue: 4}}
	c.NonEmptyVoxelCount = 3
	d.Entries[0].NonEmptyVoxelCount = 3
	b = i06Build(t, d, chunks, opts)
	if pairs := i06RootPairs(t, b); !reflect.DeepEqual(pairs, [][2]uint8{{1, 4}}) {
		t.Fatalf("opaque/emissive filtering %v", pairs)
	}
	if len(b.Chunks[c.Coord].Voxels) != 3 {
		t.Fatal("coarse filter changed authoritative fullsource")
	}
}

func TestI06PageBakeLandmarkOverridesAndPayloadOnlyBranches(t *testing.T) {
	d, chunks := i06Source()
	opts := i06Options()
	landmark := ImportedWorldLandmarkDef{ID: "tower", MinimumLevel: content.StreamPageLevelRoot, WorldOrigin: [3]float32{100, 20, 100}, VoxelResolution: 1, Voxels: []content.ImportedWorldVoxelDef{{Value: 3, MaterialValue: 6}}}
	opts.Landmarks = []ImportedWorldLandmarkDef{landmark}
	b := i06Build(t, d, chunks, opts)
	if len(b.Manifest.Entries) != 2 {
		t.Fatal("landmark added fake full leaf")
	}
	found := false
	for _, p := range b.Manifest.Pages {
		if p.Level != content.StreamPageLevelRoot {
			continue
		}
		c := b.PageChunks[p.Payload.Path]
		for _, v := range c.Voxels {
			if v.Value == 3 && content.ImportedWorldVoxelMaterialValue(v) == 6 {
				wx := p.Payload.WorldOrigin[0] + float32(v.X)*c.VoxelResolution
				wy := p.Payload.WorldOrigin[1] + float32(v.Y)*c.VoxelResolution
				wz := p.Payload.WorldOrigin[2] + float32(v.Z)*c.VoxelResolution
				if wx <= 100.5 && 100.5 < wx+c.VoxelResolution && wy <= 20.5 && 20.5 < wy+c.VoxelResolution && wz <= 100.5 && 100.5 < wz+c.VoxelResolution {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("root-only authored landmark missing")
	}
	second := landmark
	second.ID = "conflict"
	second.Voxels = []content.ImportedWorldVoxelDef{{Value: 2, MaterialValue: 7}}
	opts.Landmarks = append(opts.Landmarks, second)
	if _, e := BuildImportedWorldPageBake(d, chunks, opts); e == nil {
		t.Fatal("conflicting landmark overrides silently prioritized")
	}
	opts.Landmarks = []ImportedWorldLandmarkDef{landmark, landmark}
	if _, e := BuildImportedWorldPageBake(d, chunks, opts); e == nil {
		t.Fatal("duplicate landmarkID accepted")
	}
}

func TestI06PageBakePublicationCodecDeterminismAndFailureIsolation(t *testing.T) {
	d, chunks := i06Source()
	b := i06Build(t, d, chunks, i06Options())
	dir := t.TempDir()
	p := filepath.Join(dir, "world.gkworld")
	if e := SaveImportedWorldPageBake(p, b); e != nil {
		t.Fatal(e)
	}
	loaded, e := content.LoadImportedWorld(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = content.ValidateImportedWorldV3(loaded); e != nil {
		t.Fatal(e)
	}
	for _, entry := range loaded.Entries {
		full, e := content.LoadImportedWorldChunk(content.ResolveImportedWorldChunkPath(entry, p))
		if e != nil {
			t.Fatal(e)
		}
		if full.PayloadHash != entry.PayloadHash || full.PayloadSizeBytes != entry.PayloadSizeBytes || full.PayloadKind != content.ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1 {
			t.Fatal("actual fullcodec reference/material loss")
		}
		if full.Voxels[0].Value == content.ImportedWorldVoxelMaterialValue(full.Voxels[0]) {
			t.Fatal("semanticmaterialpair collapsed")
		}
	}
	for pageIndex, page := range loaded.Pages {
		if page.Level == content.StreamPageLevelLeaf {
			continue
		}
		payload, e := content.LoadImportedWorldPagePayload(loaded, p, uint32(pageIndex))
		if e != nil {
			t.Fatal(e)
		}
		if payload.PayloadHash != page.Payload.PayloadHash || payload.PayloadSizeBytes != page.Payload.PayloadSizeBytes || payload.Coord != (content.TerrainChunkCoordDef{}) {
			t.Fatal("actual page payload qualification")
		}
	}
	for pageIndex, page := range loaded.Pages {
		if page.Level == content.StreamPageLevelLeaf {
			continue
		}
		payload, e := content.LoadImportedWorldPagePayload(loaded, p, uint32(pageIndex))
		if e != nil {
			t.Fatal(e)
		}
		if len(payload.Voxels) == 0 {
			continue
		}
		if page.Payload.Aux == nil {
			t.Fatal("nonempty coarsepage lacks regenerated normalaux")
		}
		ref := page.Payload.Aux
		aux, e := content.LoadImportedWorldChunkAux(content.ResolveDocumentPath(ref.AuxPath, p))
		if e != nil {
			t.Fatal(e)
		}
		if ref.SourcePayloadHash != page.Payload.PayloadHash || ref.SourcePayloadSizeBytes != page.Payload.PayloadSizeBytes || ref.NormalBakeVersion != content.ImportedWorldNormalBakeVersion || aux.SourcePayloadHash != ref.SourcePayloadHash || aux.PayloadHash != ref.PayloadHash || aux.PayloadSizeBytes != ref.PayloadSizeBytes || aux.Coord != (content.TerrainChunkCoordDef{}) || aux.ChunkSize != payload.ChunkSize || aux.VoxelResolution != payload.VoxelResolution {
			t.Fatal("page normalaux reference/grid qualification")
		}
		badPage := page
		badPage.Payload.ChunkSize++
		badManifest := *loaded
		badManifest.Pages = append([]content.StreamPageDef(nil), loaded.Pages...)
		badManifest.Pages[pageIndex] = badPage
		if _, e := content.LoadImportedWorldPagePayload(&badManifest, p, uint32(pageIndex)); e == nil {
			t.Fatal("page decoder ignored declared grid mismatch")
		}
	}
	// Qualify source leaf reuse and metadata that is not covered by the raw RLE body hash.
	for pageIndex, page := range loaded.Pages {
		payload, e := content.LoadImportedWorldPagePayload(loaded, p, uint32(pageIndex))
		if e != nil {
			t.Fatal(e)
		}
		if page.Level == content.StreamPageLevelLeaf {
			entry := loaded.Entries[page.LeafEntryIndices[0]]
			if payload.Coord != entry.Coord || page.Payload.Path != entry.ChunkPath || page.Payload.PayloadHash != entry.PayloadHash || page.Payload.PayloadSizeBytes != entry.PayloadSizeBytes || page.Payload.Kind != entry.PayloadKind {
				t.Fatal("fullleaf coordinate not qualified")
			}
		}
	}
	for pageIndex, page := range loaded.Pages {
		filePath := content.ResolveDocumentPath(page.Payload.Path, p)
		original, e := os.ReadFile(filePath)
		if e != nil {
			t.Fatal(e)
		}
		if len(original) < 12 {
			t.Fatal("RLEframe missing")
		}
		n := int(binary.LittleEndian.Uint32(original[8:12]))
		var metadata map[string]any
		if e = json.Unmarshal(original[12:12+n], &metadata); e != nil {
			t.Fatal(e)
		}
		for _, field := range []string{"world_id", "coord"} {
			edited := map[string]any{}
			for k, v := range metadata {
				edited[k] = v
			}
			if field == "world_id" {
				edited[field] = "foreign-world"
			} else {
				edited[field] = map[string]int{"x": 99, "z": 0}
			}
			header, e := json.Marshal(edited)
			if e != nil {
				t.Fatal(e)
			}
			changed := append([]byte(nil), original[:8]...)
			changed = append(changed, make([]byte, 4)...)
			binary.LittleEndian.PutUint32(changed[8:12], uint32(len(header)))
			changed = append(changed, header...)
			changed = append(changed, original[12+n:]...)
			if e = os.WriteFile(filePath, changed, 0600); e != nil {
				t.Fatal(e)
			}
			if got, e := content.LoadImportedWorldPagePayload(loaded, p, uint32(pageIndex)); e == nil || got != nil {
				t.Fatalf("samebodyhash accepted wrong%s", field)
			}
		}
		if e = os.WriteFile(filePath, original, 0600); e != nil {
			t.Fatal(e)
		}
	}
	old := i06Files(t, dir)
	// Public draft objects cannot silently rewrite an already published content-qualified generation.
	for _, payload := range b.PageChunks {
		if len(payload.Voxels) == 0 {
			continue
		}
		originalValue := payload.Voxels[0].Value
		payload.Voxels[0].Value = 9
		if e := SaveImportedWorldPageBake(p, b); e == nil {
			t.Fatal("valid geometry mutation rewrote stale content-qualified generation")
		}
		if !reflect.DeepEqual(old, i06Files(t, dir)) {
			t.Fatal("stale identity mutation changed published files")
		}
		payload.Voxels[0].Value = originalValue
		break
	}
	if e := EnsureImportedWorldAuxSidecarsForManifest(p); e == nil {
		t.Fatal("generic repair silently accepted immutablev3page publication")
	}
	if !reflect.DeepEqual(old, i06Files(t, dir)) {
		t.Fatal("generic repair changed immutablepage publication")
	}

	if e = SaveImportedWorldPageBake(p, b); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(old, i06Files(t, dir)) {
		t.Fatal("repeat save changed bytes")
	}
	// Shuffling entry and unique voxel input order must produce identical canonical files across directories.
	d.Entries[0], d.Entries[1] = d.Entries[1], d.Entries[0]
	for _, c := range chunks {
		c.Voxels[0], c.Voxels[1] = c.Voxels[1], c.Voxels[0]
	}
	other := i06Build(t, d, chunks, i06Options())
	dir2 := t.TempDir()
	if e = SaveImportedWorldPageBake(filepath.Join(dir2, "world.gkworld"), other); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(old, i06Files(t, dir2)) {
		t.Fatal("source order/directory affects output bytes")
	}
	chunks[content.TerrainChunkCoordDef{}].Voxels[0].Value = 3
	bad := i06Build(t, d, chunks, i06Options())
	for _, c := range bad.PageChunks {
		c.Voxels = append(c.Voxels, content.ImportedWorldVoxelDef{X: c.ChunkSize, Value: 1})
		break
	}
	if e = SaveImportedWorldPageBake(p, bad); e == nil {
		t.Fatal("invalid draft published")
	}
	if !reflect.DeepEqual(old, i06Files(t, dir)) {
		t.Fatal("failed publication modified old manifest or referenced payloads")
	}
}

func TestI06PageBakeRejectsMalformedSourceAndUnsafeBudgets(t *testing.T) {
	cases := map[string]func(*content.ImportedWorldDef, map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef){"future": func(d *content.ImportedWorldDef, c map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef) {
		d.SchemaVersion = 4
	}, "duplicate-entry": func(d *content.ImportedWorldDef, c map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef) {
		d.Entries = append(d.Entries, d.Entries[0])
	}, "missing-chunk": func(d *content.ImportedWorldDef, c map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef) {
		delete(c, d.Entries[0].Coord)
	}, "grid-mismatch": func(d *content.ImportedWorldDef, c map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef) {
		c[d.Entries[0].Coord].VoxelResolution = 2
	}, "duplicate-voxel": func(d *content.ImportedWorldDef, c map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef) {
		v := c[d.Entries[0].Coord]
		v.Voxels = append(v.Voxels, v.Voxels[0])
		v.NonEmptyVoxelCount++
		d.Entries[0].NonEmptyVoxelCount++
	}, "wrong-count": func(d *content.ImportedWorldDef, c map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef) {
		c[d.Entries[0].Coord].NonEmptyVoxelCount++
	}}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d, c := i06Source()
			mutate(d, c)
			if _, e := BuildImportedWorldPageBake(d, c, i06Options()); e == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
	for _, mutate := range []func(*ImportedWorldPageBakeOptions){func(o *ImportedWorldPageBakeOptions) { o.MaxPages = 1 }, func(o *ImportedWorldPageBakeOptions) { o.MaxPayloadSide = 2 }, func(o *ImportedWorldPageBakeOptions) { o.RegionalSpan = 6 }, func(o *ImportedWorldPageBakeOptions) { o.MacroSpan = 12 }, func(o *ImportedWorldPageBakeOptions) { o.RootSpan = float32(math.Inf(1)) }, func(o *ImportedWorldPageBakeOptions) { o.RootResolution = -1 }} {
		d, c := i06Source()
		o := i06Options()
		mutate(&o)
		if _, e := BuildImportedWorldPageBake(d, c, o); e == nil {
			t.Fatalf("unsafe options %+v", o)
		}
	}
}

func TestI06PageBakeTallWorldCubeAndHardSafetyCaps(t *testing.T) {
	d, chunks := i06Source()
	d.Entries = d.Entries[:1]
	old := d.Entries[0].Coord
	c := chunks[old]
	coord := content.TerrainChunkCoordDef{Y: 5}
	d.Entries[0].Coord = coord
	c.Coord = coord
	chunks = map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{coord: c}
	b := i06Build(t, d, chunks, i06Options())
	found := false
	for _, p := range b.Manifest.Pages {
		if p.Level != content.StreamPageLevelRoot {
			continue
		}
		if p.BoundsMin[1] > 20 || p.BoundsMax[1] < 24 {
			t.Fatal("root lost non-nominalY fullleaf coverage")
		}
		payload := b.PageChunks[p.Payload.Path]
		for _, v := range payload.Voxels {
			wy := p.Payload.WorldOrigin[1] + float32(v.Y)*payload.VoxelResolution
			if wy <= 20.5 && 20.5 < wy+payload.VoxelResolution {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("tall geometry packing lost occupiedworldcells")
	}
	for _, mutate := range []func(*ImportedWorldPageBakeOptions){func(o *ImportedWorldPageBakeOptions) { o.MaxPayloadSide = 257 }, func(o *ImportedWorldPageBakeOptions) { o.MaxPages = 4097 }, func(o *ImportedWorldPageBakeOptions) { o.MaxPages = -1 }, func(o *ImportedWorldPageBakeOptions) { o.MaxPayloadSide = -1 }} {
		o := i06Options()
		mutate(&o)
		if _, e := BuildImportedWorldPageBake(d, chunks, o); e == nil {
			t.Fatal("hard safety cap not enforced")
		}
	}
}

func TestI06PageBakeDefaultsDecimalSourceAlignmentAndLandmarkReplacement(t *testing.T) {
	d, chunks := i06Source()
	b := i06Build(t, d, chunks, ImportedWorldPageBakeOptions{})
	for _, p := range b.Manifest.Pages {
		want := map[uint8]float32{content.StreamPageLevelRegional: 1, content.StreamPageLevelMacro: 4, content.StreamPageLevelRoot: 16}
		if res, ok := want[p.Level]; ok && p.Payload.VoxelResolution != res {
			t.Fatal("default coarse resolution")
		}
	}
	// The documented 0.1m source grid must tolerate its finite float32 25.6m side when checking whole-span ratios.
	d, chunks = i06Source()
	d.ChunkSize = 256
	d.VoxelResolution = .1
	for _, c := range chunks {
		c.ChunkSize = 256
		c.VoxelResolution = .1
	}
	_ = i06Build(t, d, chunks, ImportedWorldPageBakeOptions{})
	d, chunks = i06Source()
	opts := i06Options()
	opts.Landmarks = []ImportedWorldLandmarkDef{{ID: "replacement", MinimumLevel: content.StreamPageLevelRegional, WorldOrigin: [3]float32{}, VoxelResolution: 1, Voxels: []content.ImportedWorldVoxelDef{{Value: 3, MaterialValue: 6}}}}
	b = i06Build(t, d, chunks, opts)
	seen := map[uint8]bool{}
	for _, p := range b.Manifest.Pages {
		if p.Level == content.StreamPageLevelLeaf {
			continue
		}
		c := b.PageChunks[p.Payload.Path]
		for _, v := range c.Voxels {
			if v.Value == 3 && content.ImportedWorldVoxelMaterialValue(v) == 6 {
				seen[p.Level] = true
			}
		}
	}
	for _, level := range []uint8{content.StreamPageLevelRegional, content.StreamPageLevelMacro, content.StreamPageLevelRoot} {
		if !seen[level] {
			t.Fatalf("override missing atlevel%d", level)
		}
	}
	if b.Chunks[content.TerrainChunkCoordDef{}].Voxels[0].Value != 1 {
		t.Fatal("landmark changed authoritative source")
	}
}

func TestI06PageBakeLateIOFailurePreservesPreviousGeneration(t *testing.T) {
	d, chunks := i06Source()
	opts := i06Options()
	old := i06Build(t, d, chunks, opts)
	dir := t.TempDir()
	p := filepath.Join(dir, "world.gkworld")
	if e := SaveImportedWorldPageBake(p, old); e != nil {
		t.Fatal(e)
	}
	before := i06Files(t, dir)
	changed := content.TerrainChunkCoordDef{}
	chunks[changed].Voxels[0].Value = 3
	next := i06Build(t, d, chunks, opts)
	oldPaths := map[content.TerrainChunkCoordDef]string{}
	for _, entry := range old.Manifest.Entries {
		oldPaths[entry.Coord] = entry.ChunkPath
	}
	for _, entry := range next.Manifest.Entries {
		if entry.ChunkPath == oldPaths[entry.Coord] {
			t.Fatalf("wholebake identity did not requalify fullchunk %v", entry.Coord)
		}
	}
	var paths []string
	for path := range next.PageChunks {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("no coarsepayload")
	}
	blocker := content.ResolveDocumentPath(paths[len(paths)-1], p)
	if e := os.MkdirAll(blocker, 0755); e != nil {
		t.Fatal(e)
	}
	if e := SaveImportedWorldPageBake(p, next); e == nil {
		t.Fatal("directoryblocker did not cause IO failure")
	}
	for relative, want := range before {
		got, e := os.ReadFile(filepath.Join(dir, relative))
		if e != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("failedsave changed priorgeneration file %s", relative)
		}
	}
}

func TestI06PageBakeSafetyAndDeepMetadataOwnership(t *testing.T) {
	d, chunks := i06Source()
	d.Materials = []content.ImportedWorldMaterialDef{{ID: 6, PaletteIndex: 6, Tags: []string{"opaque"}}}
	b := i06Build(t, d, chunks, i06Options())
	b.Manifest.Palette[0][0] = 99
	b.Manifest.MaterialPalette[0][0] = 88
	b.Manifest.Materials[0].Tags[0] = "changed"
	if d.Palette[0][0] != 0 || d.MaterialPalette[0][0] != 0 || d.Materials[0].Tags[0] != "opaque" {
		t.Fatal("bake material/palette aliases source")
	}
	d.Materials = append(d.Materials, content.ImportedWorldMaterialDef{ID: 7, PaletteIndex: 6, Transparent: true})
	if _, e := BuildImportedWorldPageBake(d, chunks, i06Options()); e == nil {
		t.Fatal("duplicate materialpalette index chooses inputorder")
	}
	d, chunks = i06Source()
	d.ChunkSize = 512
	d.VoxelResolution = .25
	for _, c := range chunks {
		c.ChunkSize = 512
		c.VoxelResolution = .25
	}
	if _, e := BuildImportedWorldPageBake(d, chunks, ImportedWorldPageBakeOptions{}); e == nil {
		t.Fatal("fullsourcegrid exceeded hardpayloadside cap")
	}
	d, chunks = i06Source()
	old := d.Entries[0].Coord
	huge := content.TerrainChunkCoordDef{X: int(^uint(0) >> 1)}
	c := chunks[old]
	delete(chunks, old)
	c.Coord = huge
	chunks[huge] = c
	d.Entries[0].Coord = huge
	if _, e := BuildImportedWorldPageBake(d, chunks, i06Options()); e == nil {
		t.Fatal("lostf32 sourcecell precision admitted")
	}
	for _, mutate := range []func(*ImportedWorldPageBakeOptions){func(o *ImportedWorldPageBakeOptions) { o.RootResolution = 3 }, func(o *ImportedWorldPageBakeOptions) { o.RegionalResolution = .5 }} {
		d, c := i06Source()
		o := i06Options()
		mutate(&o)
		if _, e := BuildImportedWorldPageBake(d, c, o); e == nil {
			t.Fatal("nondivisor or finerthansource coarsegrid admitted")
		}
	}
}

func TestI06PageBakeLandmarkOrderProducesIdenticalFiles(t *testing.T) {
	d, chunks := i06Source()
	opts := i06Options()
	a := ImportedWorldLandmarkDef{ID: "a", MinimumLevel: content.StreamPageLevelRoot, VoxelResolution: 1, Voxels: []content.ImportedWorldVoxelDef{{Value: 3, MaterialValue: 6}}}
	b := a
	b.ID = "b"
	b.WorldOrigin = [3]float32{100, 20, 100}
	opts.Landmarks = []ImportedWorldLandmarkDef{a, b}
	first := i06Build(t, d, chunks, opts)
	dir := t.TempDir()
	if e := SaveImportedWorldPageBake(filepath.Join(dir, "world.gkworld"), first); e != nil {
		t.Fatal(e)
	}
	opts.Landmarks = []ImportedWorldLandmarkDef{b, a}
	second := i06Build(t, d, chunks, opts)
	dir2 := t.TempDir()
	if e := SaveImportedWorldPageBake(filepath.Join(dir2, "world.gkworld"), second); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(i06Files(t, dir), i06Files(t, dir2)) {
		t.Fatal("landmarkinputorder changes canonicalidentity/files")
	}
}
