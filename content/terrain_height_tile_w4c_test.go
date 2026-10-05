package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func w4cTile() *TerrainHeightTileDef {
	return &TerrainHeightTileDef{SchemaVersion: 1, TerrainID: "terrain", SourceHash: "source-identity", Coord: TerrainChunkCoordDef{X: -1, Z: 2}, WorldOrigin: [3]float32{-6, 0, 12}, SampleWidth: 3, SampleHeight: 3, SampleSpacing: 2, HeightScale: 64, HeightSamples: []uint16{0, 1, 2, 3, 4, 5, 6, 7, 65535}, SurfaceMask: []byte{255, 1}}
}
func w4cRaw(d *TerrainHeightTileDef) []byte {
	b := make([]byte, len(d.HeightSamples)*2)
	for i, h := range d.HeightSamples {
		binary.LittleEndian.PutUint16(b[i*2:], h)
	}
	b = append(b, d.SurfaceMask...)
	return append(b, d.OutdoorNavExclusionMask...)
}
func w4cHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func w4cMetadata(d *TerrainHeightTileDef, b []byte) map[string]any {
	return map[string]any{"schema_version": d.SchemaVersion, "terrain_id": d.TerrainID, "source_hash": d.SourceHash, "coord": d.Coord, "world_origin": d.WorldOrigin, "sample_width": d.SampleWidth, "sample_height": d.SampleHeight, "sample_spacing": d.SampleSpacing, "height_offset": d.HeightOffset, "height_scale": d.HeightScale, "payload_kind": TerrainHeightTilePayloadKind, "payload_hash": w4cHash(b), "payload_size_bytes": len(b), "surface_mask_bytes": len(d.SurfaceMask), "outdoor_nav_cell_size": d.OutdoorNavCellSize, "outdoor_nav_exclusion_mask_bytes": len(d.OutdoorNavExclusionMask)}
}
func w4cFrame(t *testing.T, m map[string]any, b []byte) []byte {
	t.Helper()
	j, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	f := append([]byte("GKHTIL1\n"), make([]byte, 4)...)
	binary.LittleEndian.PutUint32(f[8:], uint32(len(j)))
	f = append(f, j...)
	return append(f, b...)
}
func w4cWrite(t *testing.T, p string, b []byte) {
	t.Helper()
	if e := os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
}

func TestW4cHeightTileIndependentBinaryAndDeterministicSave(t *testing.T) {
	if TerrainHeightTilePayloadKind != "height_u16_binary_v1" || TerrainHeightTileSchemaVersion != 1 || TerrainHeightTileManifestSchemaVersion != 3 || CurrentTerrainChunkManifestSchemaVersion != 2 {
		t.Fatal("version contracts changed")
	}
	d := w4cTile()
	before := *d
	before.HeightSamples = append([]uint16(nil), d.HeightSamples...)
	before.SurfaceMask = append([]byte(nil), d.SurfaceMask...)
	if e := ValidateTerrainHeightTile(d); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "tile.bin")
	raw := w4cRaw(d)
	w4cWrite(t, p, w4cFrame(t, w4cMetadata(d, raw), raw))
	got, e := LoadTerrainHeightTile(p)
	if e != nil || !reflect.DeepEqual(got, d) {
		t.Fatalf("independent format: got=%+v err=%v", got, e)
	}
	result, e := SaveTerrainHeightTile(p, d)
	if e != nil {
		t.Fatal(e)
	}
	if result.PayloadHash != w4cHash(raw) || result.PayloadSizeBytes != len(raw) {
		t.Fatalf("raw payload result: %+v", result)
	}
	file, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if len(file) < 12 || string(file[:8]) != "GKHTIL1\n" {
		t.Fatal("frame magic")
	}
	n := int(binary.LittleEndian.Uint32(file[8:12]))
	if n > 65536 || 12+n > len(file) {
		t.Fatal("metadata size")
	}
	var m map[string]any
	if e = json.Unmarshal(file[12:12+n], &m); e != nil {
		t.Fatal(e)
	}
	if m["payload_hash"] != w4cHash(raw) || m["payload_size_bytes"] != float64(len(raw)) || m["surface_mask_bytes"] != float64(2) || m["payload_kind"] != TerrainHeightTilePayloadKind || !bytes.Equal(file[12+n:], raw) {
		t.Fatalf("metadata/payload mismatch: %v", m)
	}
	if _, e = SaveTerrainHeightTile(p, d); e != nil {
		t.Fatal(e)
	}
	again, _ := os.ReadFile(p)
	if !bytes.Equal(file, again) || !reflect.DeepEqual(*d, before) {
		t.Fatal("save is nondeterministic or mutated source")
	}
}

func TestW4cHeightTileRejectsCorruptFramesAndSemanticPayload(t *testing.T) {
	d := w4cTile()
	raw := w4cRaw(d)
	good := w4cFrame(t, w4cMetadata(d, raw), raw)
	cases := map[string][]byte{"wrong-magic": append([]byte("BADMAGIC"), good[8:]...), "short-header": good[:10], "short-payload": good[:len(good)-1], "trailing": append(append([]byte(nil), good...), 0), "bad-hash": append([]byte(nil), good...)}
	cases["bad-hash"][len(good)-1] ^= 1
	j, err := json.Marshal(w4cMetadata(d, raw))
	if err != nil {
		t.Fatal(err)
	}
	j = append(j, bytes.Repeat([]byte(" "), 65537-len(j))...)
	huge := append([]byte("GKHTIL1\n"), make([]byte, 4)...)
	binary.LittleEndian.PutUint32(huge[8:], 65537)
	cases["oversized-metadata"] = append(append(huge, j...), raw...)
	mutations := map[string]func(map[string]any){"schema": func(m map[string]any) { m["schema_version"] = 2 }, "kind": func(m map[string]any) { m["payload_kind"] = "unknown" }, "width": func(m map[string]any) { m["sample_width"] = 129 }, "height": func(m map[string]any) { m["sample_height"] = 0 }, "negative-mask-count": func(m map[string]any) { m["surface_mask_bytes"] = -1 }, "negative-nav-count": func(m map[string]any) { m["outdoor_nav_exclusion_mask_bytes"] = -1 }, "mask-count": func(m map[string]any) { m["surface_mask_bytes"] = 1 }, "nav-settings": func(m map[string]any) { m["outdoor_nav_cell_size"] = 1 }, "hash-uppercase": func(m map[string]any) { m["payload_hash"] = strings.ToUpper(w4cHash(raw)) }, "payload-limit": func(m map[string]any) { m["payload_size_bytes"] = 43009 }, "empty-terrain": func(m map[string]any) { m["terrain_id"] = "" }, "empty-source": func(m map[string]any) { m["source_hash"] = "" }}
	for name, mutate := range mutations {
		m := w4cMetadata(d, raw)
		mutate(m)
		cases[name] = w4cFrame(t, m, raw)
	}
	padding := append([]byte(nil), raw...)
	padding[len(padding)-1] = 128
	cases["mask-tail-rehashed"] = w4cFrame(t, w4cMetadata(d, padding), padding)
	for name, file := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "bad.bin")
			w4cWrite(t, p, file)
			if tile, e := LoadTerrainHeightTile(p); e == nil || tile != nil {
				t.Fatalf("invalid binary returned tile=%+v err=%v", tile, e)
			}
		})
	}
}

func TestW4cHeightTileValidationAndOptionalNav(t *testing.T) {
	if ValidateTerrainHeightTile(nil) == nil {
		t.Fatal("accepted nil")
	}
	bad := map[string]func(*TerrainHeightTileDef){"schema": func(d *TerrainHeightTileDef) { d.SchemaVersion = 0 }, "dimension-count-mismatch": func(d *TerrainHeightTileDef) { d.SampleHeight = 2 }, "samples": func(d *TerrainHeightTileDef) { d.HeightSamples = d.HeightSamples[:8] }, "zero-spacing": func(d *TerrainHeightTileDef) { d.SampleSpacing = 0 }, "nan-spacing": func(d *TerrainHeightTileDef) { d.SampleSpacing = float32(math.NaN()) }, "inf-origin": func(d *TerrainHeightTileDef) { d.WorldOrigin[0] = float32(math.Inf(1)) }, "nan-offset": func(d *TerrainHeightTileDef) { d.HeightOffset = float32(math.NaN()) }, "zero-scale": func(d *TerrainHeightTileDef) { d.HeightScale = 0 }, "mask": func(d *TerrainHeightTileDef) { d.SurfaceMask = []byte{255} }, "mask-padding": func(d *TerrainHeightTileDef) { d.SurfaceMask[1] = 2 }, "nav": func(d *TerrainHeightTileDef) { d.OutdoorNavExclusionMask = []byte{1} }}
	for name, f := range bad {
		t.Run(name, func(t *testing.T) {
			d := w4cTile()
			f(d)
			if ValidateTerrainHeightTile(d) == nil {
				t.Fatal("accepted invalid tile")
			}
			p := filepath.Join(t.TempDir(), "existing.bin")
			sentinel := []byte("existing file")
			w4cWrite(t, p, sentinel)
			if _, e := SaveTerrainHeightTile(p, d); e == nil {
				t.Fatal("saved invalid tile")
			}
			file, e := os.ReadFile(p)
			if e != nil || !bytes.Equal(file, sentinel) {
				t.Fatal("invalid save changed existing file")
			}
		})
	}
	d := w4cTile()
	d.SampleWidth = 128
	d.SampleHeight = 128
	d.HeightSamples = make([]uint16, 128*128)
	d.SurfaceMask = nil
	d.OutdoorNavCellSize = 1
	d.OutdoorNavExclusionMask = make([]byte, 8192)
	if e := ValidateTerrainHeightTile(d); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "nav.bin")
	if _, e := SaveTerrainHeightTile(p, d); e != nil {
		t.Fatal(e)
	}
	got, e := LoadTerrainHeightTile(p)
	if e != nil || !reflect.DeepEqual(got, d) {
		t.Fatalf("nav round trip: %v", e)
	}
	for _, f := range []func(*TerrainHeightTileDef){func(d *TerrainHeightTileDef) { d.SampleSpacing = 1 }, func(d *TerrainHeightTileDef) { d.OutdoorNavCellSize = 2 }, func(d *TerrainHeightTileDef) { d.OutdoorNavExclusionMask = d.OutdoorNavExclusionMask[:8191] }} {
		c := *d
		f(&c)
		if ValidateTerrainHeightTile(&c) == nil {
			t.Fatal("accepted invalid nav configuration")
		}
	}
}

func w4cSource() *TerrainSourceDef {
	return &TerrainSourceDef{ID: "ramp", SchemaVersion: 1, Kind: TerrainKindHeightfield, SampleWidth: 2, SampleHeight: 2, HeightSamples: []uint16{0, 65535, 0, 65535}, WorldSize: Vec2{6, 6}, HeightScale: 64, VoxelResolution: 1, ChunkSize: 32}
}
func TestW4cBakeSignedPartialTilesAndEntryRoundTrip(t *testing.T) {
	d := w4cSource()
	before := *d
	before.HeightSamples = append([]uint16(nil), d.HeightSamples...)
	p := filepath.Join(t.TempDir(), "ramp.gkterrainmanifest")
	opts := TerrainHeightTileBakeOptions{TileSize: 2, SampleSpacing: 2}
	m, tiles, e := BakeTerrainHeightTiles(d, p, opts)
	if e != nil {
		t.Fatal(e)
	}
	m2, tiles2, e := BakeTerrainHeightTiles(d, p, opts)
	if e != nil || !reflect.DeepEqual(m, m2) || !reflect.DeepEqual(tiles, tiles2) || !reflect.DeepEqual(*d, before) {
		t.Fatal("bake nondeterministic or source mutated")
	}
	if m.SchemaVersion != 3 || m.TerrainID != d.ID || m.SourceHash != TerrainBakeSourceHash(d) || m.ChunkSize != 2 || m.VoxelResolution != 2 || len(m.Entries) != 4 || len(tiles) != 4 {
		t.Fatalf("manifest: %+v", m)
	}
	coords := []TerrainChunkCoordDef{{X: -1, Z: -1}, {X: 0, Z: -1}, {X: -1, Z: 0}, {X: 0, Z: 0}}
	for i, entry := range m.Entries {
		if entry.Coord != coords[i] || filepath.IsAbs(entry.ChunkPath) || entry.ChunkPath == "" {
			t.Fatalf("entry ordering/path: %+v", entry)
		}
		tile := tiles[TerrainChunkKey(entry.Coord)]
		if tile == nil {
			t.Fatal("missing tile")
		}
		origin := [3]float32{float32(entry.Coord.X * 4), 0, float32(entry.Coord.Z * 4)}
		if tile.WorldOrigin != origin || entry.WorldOrigin != origin || tile.HeightOffset != 0 || tile.HeightScale != 64 || tile.OutdoorNavCellSize != 0 || len(tile.OutdoorNavExclusionMask) != 0 {
			t.Fatalf("tile metadata: %+v", tile)
		}
		valid := 0
		for z := 0; z < 2; z++ {
			for x := 0; x < 2; x++ {
				j := z*2 + x
				wx := origin[0] + float32(x*2+1)
				wz := origin[2] + float32(z*2+1)
				ok := wx >= -3 && wx < 3 && wz >= -3 && wz < 3
				var want uint16
				if ok {
					valid++
					nx := (wx + 3) / 6
					height := float32(64) * nx
					want = uint16(math.Round(float64((height / 64) * 65535)))
				}
				if tile.HeightSamples[j] != want {
					t.Fatalf("coord %v sample %d got %d want %d", entry.Coord, j, tile.HeightSamples[j], want)
				}
				if len(tile.SurfaceMask) > 0 && ((tile.SurfaceMask[j/8]>>uint(j%8))&1 == 1) != ok {
					t.Fatal("surface mask")
				}
			}
		}
		if valid == 4 {
			if len(tile.SurfaceMask) != 0 {
				t.Fatal("all valid mask must be omitted")
			}
		} else if len(tile.SurfaceMask) != 1 || tile.SurfaceMask[0]&240 != 0 {
			t.Fatal("partial mask length/padding")
		}
		path := ResolveTerrainChunkPath(entry, p)
		result, e := SaveTerrainHeightTile(path, tile)
		if e != nil {
			t.Fatal(e)
		}
		if entry.PayloadKind != TerrainHeightTilePayloadKind || entry.PayloadHash != result.PayloadHash || entry.PayloadSizeBytes != result.PayloadSizeBytes {
			t.Fatal("baked reference mismatch")
		}
		loaded, e := LoadTerrainHeightTileEntry(entry, p)
		if e != nil || !reflect.DeepEqual(loaded, tile) {
			t.Fatalf("entry round trip: %v", e)
		}
		for name, mutate := range map[string]func(*TerrainChunkEntryDef){"coord-y": func(c *TerrainChunkEntryDef) { c.Coord.Y++ }, "origin-y": func(c *TerrainChunkEntryDef) { c.WorldOrigin[1]++ }, "origin-z": func(c *TerrainChunkEntryDef) { c.WorldOrigin[2]++ }, "coord": func(c *TerrainChunkEntryDef) { c.Coord.X++ }, "origin": func(c *TerrainChunkEntryDef) { c.WorldOrigin[0]++ }, "identity": func(c *TerrainChunkEntryDef) { c.TerrainID = "other" }, "source": func(c *TerrainChunkEntryDef) { c.SourceHash = "other" }, "size": func(c *TerrainChunkEntryDef) { c.ChunkSize++ }, "spacing": func(c *TerrainChunkEntryDef) { c.VoxelResolution++ }, "hash": func(c *TerrainChunkEntryDef) { c.PayloadHash = strings.Repeat("0", 64) }, "bytes": func(c *TerrainChunkEntryDef) { c.PayloadSizeBytes++ }, "kind": func(c *TerrainChunkEntryDef) { c.PayloadKind = "unknown" }} {
			c := entry
			mutate(&c)
			if _, e := LoadTerrainHeightTileEntry(c, p); e == nil {
				t.Fatalf("accepted mismatched %s", name)
			}
		}
	}
	if e := SaveTerrainChunkManifest(p, m); e != nil {
		t.Fatal(e)
	}
	loaded, e := LoadTerrainChunkManifest(p)
	if e != nil || !reflect.DeepEqual(loaded, m) {
		t.Fatalf("v3 manifest: %v", e)
	}
	for name, mutate := range map[string]func(*TerrainChunkManifestDef){"duplicate": func(c *TerrainChunkManifestDef) { c.Entries = append(c.Entries, c.Entries[0]) }, "identity": func(c *TerrainChunkManifestDef) { c.TerrainID = "" }, "source": func(c *TerrainChunkManifestDef) { c.SourceHash = "" }, "top-size": func(c *TerrainChunkManifestDef) { c.ChunkSize++ }, "top-spacing": func(c *TerrainChunkManifestDef) { c.VoxelResolution++ }, "entry-id": func(c *TerrainChunkManifestDef) { c.Entries[0].TerrainID = "other" }, "path": func(c *TerrainChunkManifestDef) { c.Entries[0].ChunkPath = "" }, "uppercase-hash": func(c *TerrainChunkManifestDef) { c.Entries[0].PayloadHash = strings.ToUpper(c.Entries[0].PayloadHash) }} {
		t.Run(name, func(t *testing.T) {
			c := *m
			c.Entries = append([]TerrainChunkEntryDef(nil), m.Entries...)
			mutate(&c)
			if SaveTerrainChunkManifest(p, &c) == nil {
				t.Fatal("save accepted invalid manifest")
			}
			b, _ := json.Marshal(c)
			w4cWrite(t, p, b)
			if _, e := LoadTerrainChunkManifest(p); e == nil {
				t.Fatal("load accepted invalid manifest")
			}
		})
	}
	legacy := &TerrainChunkManifestDef{TerrainID: "legacy"}
	if e := SaveTerrainChunkManifest(p, legacy); e != nil {
		t.Fatal(e)
	}
	if got, e := LoadTerrainChunkManifest(p); e != nil || got.SchemaVersion != 2 {
		t.Fatalf("legacy default: %v", e)
	}
}

func TestW4cBakeValidationDefaultsAndAllocationBound(t *testing.T) {
	p := filepath.Join(t.TempDir(), "terrain.gkterrainmanifest")
	if _, _, e := BakeTerrainHeightTiles(nil, p, TerrainHeightTileBakeOptions{}); e == nil {
		t.Fatal("nil source")
	}
	bad := map[string]func(*TerrainSourceDef){"id": func(d *TerrainSourceDef) { d.ID = "" }, "schema": func(d *TerrainSourceDef) { d.SchemaVersion = 0 }, "kind": func(d *TerrainSourceDef) { d.Kind = "other" }, "dimensions": func(d *TerrainSourceDef) { d.SampleWidth = 0 }, "count": func(d *TerrainSourceDef) { d.HeightSamples = d.HeightSamples[:3] }, "product-overflow": func(d *TerrainSourceDef) { d.SampleWidth = int(^uint(0) >> 1); d.SampleHeight = 2 }, "size": func(d *TerrainSourceDef) { d.WorldSize[0] = 0 }, "nan-size": func(d *TerrainSourceDef) { d.WorldSize[0] = float32(math.NaN()) }, "inf-scale": func(d *TerrainSourceDef) { d.HeightScale = float32(math.Inf(1)) }, "scale": func(d *TerrainSourceDef) { d.HeightScale = 0 }, "resolution": func(d *TerrainSourceDef) { d.VoxelResolution = 0 }, "chunk": func(d *TerrainSourceDef) { d.ChunkSize = 0 }, "huge-finite-extent": func(d *TerrainSourceDef) { d.WorldSize = Vec2{math.MaxFloat32, math.MaxFloat32} }}
	for name, f := range bad {
		t.Run(name, func(t *testing.T) {
			d := w4cSource()
			f(d)
			snapshot := *d
			samples := append([]uint16(nil), d.HeightSamples...)
			if _, _, e := BakeTerrainHeightTiles(d, p, TerrainHeightTileBakeOptions{}); e == nil {
				t.Fatal("accepted invalid source")
			}
			if d.ID != snapshot.ID || d.SchemaVersion != snapshot.SchemaVersion || d.Kind != snapshot.Kind || !reflect.DeepEqual(d.HeightSamples, samples) {
				t.Fatal("invalid source mutated")
			}
		})
	}
	for _, opts := range []TerrainHeightTileBakeOptions{{TileSize: -1}, {TileSize: 129}, {SampleSpacing: -1}, {SampleSpacing: float32(math.NaN())}, {SampleSpacing: float32(math.Inf(1))}, {MaxTiles: -1}, {TileSize: 2, SampleSpacing: 2, MaxTiles: 3}} {
		if _, _, e := BakeTerrainHeightTiles(w4cSource(), p, opts); e == nil {
			t.Fatalf("accepted options %+v", opts)
		}
	}
	m, tiles, e := BakeTerrainHeightTiles(w4cSource(), p, TerrainHeightTileBakeOptions{})
	if e != nil || m.ChunkSize != 128 || m.VoxelResolution != 2 || len(tiles) != 4 {
		t.Fatalf("defaults: %+v %v", m, e)
	}
	tiny := w4cSource()
	tiny.WorldSize = Vec2{.1, .1}
	m, tiles, e = BakeTerrainHeightTiles(tiny, p, TerrainHeightTileBakeOptions{TileSize: 1, SampleSpacing: 2})
	if e != nil || len(m.Entries) != 4 {
		t.Fatalf("tiny extent: %v", e)
	}
	for _, tile := range tiles {
		if len(tile.SurfaceMask) != 1 || tile.SurfaceMask[0] != 0 || tile.HeightSamples[0] != 0 {
			t.Fatal("all-padding intersecting tile omitted or incorrectly sampled")
		}
	}
}

func TestW4cRectangularCodecButSquareEntryReference(t *testing.T) {
	d := w4cTile()
	d.SampleWidth = 2
	d.SampleHeight = 3
	d.HeightSamples = []uint16{1, 2, 3, 4, 5, 6}
	d.SurfaceMask = []byte{63}
	if e := ValidateTerrainHeightTile(d); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "rect.bin")
	result, e := SaveTerrainHeightTile(p, d)
	if e != nil {
		t.Fatal(e)
	}
	got, e := LoadTerrainHeightTile(p)
	if e != nil || !reflect.DeepEqual(got, d) {
		t.Fatalf("rectangular codec: %v", e)
	}
	entry := TerrainChunkEntryDef{Coord: d.Coord, WorldOrigin: d.WorldOrigin, ChunkSize: 2, VoxelResolution: d.SampleSpacing, TerrainID: d.TerrainID, SourceHash: d.SourceHash, ChunkPath: filepath.Base(p), PayloadKind: TerrainHeightTilePayloadKind, PayloadHash: result.PayloadHash, PayloadSizeBytes: result.PayloadSizeBytes}
	if tile, e := LoadTerrainHeightTileEntry(entry, filepath.Join(filepath.Dir(p), "manifest.json")); e == nil || tile != nil {
		t.Fatalf("square entry accepted rectangular tile: %+v %v", tile, e)
	}
}

func TestW4cBakeIndependentTwoDimensionalBilinearAndNearestQuantization(t *testing.T) {
	d := w4cSource()
	d.WorldSize = Vec2{8, 8}
	d.HeightSamples = []uint16{0, 32768, 16384, 65535}
	p := filepath.Join(t.TempDir(), "bilinear.gkterrainmanifest")
	_, tiles, e := BakeTerrainHeightTiles(d, p, TerrainHeightTileBakeOptions{TileSize: 2, SampleSpacing: 2})
	if e != nil {
		t.Fatal(e)
	}
	for _, tile := range tiles {
		for z := 0; z < 2; z++ {
			for x := 0; x < 2; x++ {
				wx := tile.WorldOrigin[0] + float32(x*2+1)
				wz := tile.WorldOrigin[2] + float32(z*2+1)
				tx := (wx + 4) / 8
				tz := (wz + 4) / 8
				// Independent interpolation of authored corner heights, preserving the specified float32 operation order.
				h00 := float32(0) / 65535 * 64
				h10 := float32(32768) / 65535 * 64
				h01 := float32(16384) / 65535 * 64
				h11 := float32(65535) / 65535 * 64
				a := h00 + (h10-h00)*tx
				b := h01 + (h11-h01)*tx
				h := a + (b-a)*tz
				want := uint16(math.Round(float64((h / 64) * 65535)))
				if got := tile.HeightSamples[z*2+x]; got != want {
					t.Fatalf("(%g,%g) got %d want %d", wx, wz, got, want)
				}
			}
		}
	}
	// A quarter of a 0..2 authored ramp is exactly 0.5, which nearest rounds upward.
	d = w4cSource()
	d.WorldSize = Vec2{2, 2}
	d.HeightSamples = []uint16{0, 2, 0, 2}
	_, tiles, e = BakeTerrainHeightTiles(d, p, TerrainHeightTileBakeOptions{TileSize: 1, SampleSpacing: 1})
	if e != nil {
		t.Fatal(e)
	}
	for _, tile := range tiles {
		wx := tile.WorldOrigin[0] + .5
		if wx == -.5 && tile.HeightSamples[0] != 1 {
			t.Fatalf("nearest midpoint got %d want 1", tile.HeightSamples[0])
		}
	}

}
