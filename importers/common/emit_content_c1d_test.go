package common

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/derived"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c1dEmission(t *testing.T, phase int) ImportedWorldEmission {
	t.Helper()
	voxels := []Voxel{{X: 31, Palette: 1}, {X: 320, Y: 3, Z: 2, Palette: 1}}
	if phase < 2 {
		voxels = append(voxels, Voxel{X: 32, Y: phase, Palette: 1})
	}
	if phase == 0 {
		voxels = append(voxels, Voxel{X: 32, Y: 1, Z: 1, Palette: 1})
	}
	e, err := BuildImportedWorldEmission(voxels, []Material{{ID: 1, PaletteIndex: 1}}, ImportedWorldEmitOptions{WorldID: "c1d", ChunkSize: 32, VoxelResolution: .5})
	if err != nil {
		t.Fatal(err)
	}
	e.Chunks[[3]int{}].Voxels[0].Value = 255
	e.Chunks[[3]int{}].Voxels[0].MaterialValue = 7
	if len(e.ProxyChunks) == 0 {
		t.Fatal("fixture lacks proxies")
	}
	return e
}

func c1dParity(t *testing.T, path string, e ImportedWorldEmission, c *voxelcodec.Codec) map[[3]int]*content.ImportedWorldChunkDef {
	t.Helper()
	out := map[[3]int]*content.ImportedWorldChunkDef{}
	neighbors := map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{}
	for _, chunk := range e.Chunks {
		neighbors[chunk.Coord] = chunk
	}
	check := func(relative string, want *content.ImportedWorldChunkDef, full bool) *content.ImportedWorldChunkDef {
		got, err := content.LoadImportedWorldChunkWithCodec(filepath.Join(filepath.Dir(path), relative), c)
		if err != nil {
			t.Fatal(err)
		}
		g, size, err := content.ImportedWorldChunkCompiledGeometryIdentity(want, c)
		if err != nil {
			t.Fatal(err)
		}
		gotG, gotSize, err := content.ImportedWorldChunkCompiledGeometryIdentity(got, c)
		if err != nil || gotG != g || gotSize != size || got.EmbeddedAux == nil {
			t.Fatal("emission lost raw geometry identity or normals", err)
		}
		sample := neighbors
		if !full {
			sample = nil
		}
		fitted := derived.BuildImportedWorldChunkAux(want, sample, g, size, full)
		// Fitter record traversal differs from canonical frame ordering; compare by origin.
		records := map[[3]int][]byte{}
		for _, r := range fitted.Records {
			records[r.Origin] = r.Bytes
		}
		if len(records) != len(got.EmbeddedAux.Records) {
			t.Fatal("emission dropped normal records")
		}
		for _, r := range got.EmbeddedAux.Records {
			if !bytes.Equal(records[r.Origin], r.Bytes) {
				t.Fatal("emission changed exact fitter normal bytes")
			}
		}
		return got
	}
	for _, entry := range e.Manifest.Entries {
		if entry.Aux != nil {
			t.Fatal("embedded full retained sidecar ref")
		}
		key := [3]int{entry.Coord.X, entry.Coord.Y, entry.Coord.Z}
		got := check(entry.ChunkPath, e.Chunks[key], true)
		if entry.PayloadHash != got.PayloadHash || entry.PayloadSizeBytes != got.PayloadSizeBytes || entry.PayloadKind != got.PayloadKind {
			t.Fatal("full manifest has stale frame metadata")
		}
		out[key] = got
	}
	for _, sector := range e.Manifest.Sectors {
		for _, lod := range sector.LODs {
			if lod.Aux != nil {
				t.Fatal("embedded proxy retained sidecar ref")
			}
			got := check(lod.ChunkPath, e.ProxyChunks[lod.ChunkPath], false)
			if lod.PayloadHash != got.PayloadHash || lod.PayloadSizeBytes != got.PayloadSizeBytes || lod.PayloadKind != got.PayloadKind {
				t.Fatal("proxy manifest has stale frame metadata")
			}
		}
	}
	return out
}

func TestC1dEmbeddedEmissionParityReuseAndNeighborhoodChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.gkworld")
	opts := ImportedWorldSaveOptions{ChunkPayloadKind: content.ImportedWorldChunkPayloadBrickZstdBinaryV1, EmbedNormals: true}
	checkSidecarStats := func(s ImportedWorldSaveStats) {
		t.Helper()
		if s.ChunkAuxWritten != 0 || s.ChunkAuxSkipped != 0 || s.ProxyAuxWritten != 0 || s.ProxyAuxSkipped != 0 {
			t.Fatalf("embedded output reported sidecar writes/skips: %+v", s)
		}
	}
	first := c1dEmission(t, 0)
	stats, err := SaveImportedWorldEmissionWithOptionsResult(path, first, opts)
	if err != nil {
		t.Fatal(err)
	}
	checkSidecarStats(stats)
	before := c1dParity(t, path, first, nil)
	if stats.ChunksWritten != len(first.Chunks) || stats.ProxyChunksWritten != len(first.ProxyChunks) || stats.ChunkAuxReused != 0 || stats.ProxyAuxReused != 0 {
		t.Fatalf("first emission counters %+v", stats)
	}
	files := map[string][]byte{}
	times := map[string]os.FileInfo{}
	if err := filepath.Walk(filepath.Dir(path), func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files[p], _ = os.ReadFile(p)
			times[p] = info
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	repeated := c1dEmission(t, 0)
	stats, err = SaveImportedWorldEmissionWithOptionsResult(path, repeated, opts)
	if err != nil {
		t.Fatal(err)
	}
	checkSidecarStats(stats)
	if stats.ChunksWritten != 0 || stats.ProxyChunksWritten != 0 || stats.ChunksSkipped != len(first.Chunks) || stats.ProxyChunksSkipped != len(first.ProxyChunks) || stats.ChunkAuxReused != len(first.Chunks) || stats.ProxyAuxReused != len(first.ProxyChunks) {
		t.Fatalf("fresh no-op failed actual reuse: %+v", stats)
	}
	for p, want := range files {
		got, _ := os.ReadFile(p)
		info, _ := os.Stat(p)
		if !bytes.Equal(got, want) || !info.ModTime().Equal(times[p].ModTime()) {
			t.Fatal("no-op emission rewrote", p)
		}
	}
	for _, phase := range []int{1, 2} {
		e := c1dEmission(t, phase)
		stats, err = SaveImportedWorldEmissionWithOptionsResult(path, e, opts)
		if err != nil {
			t.Fatal(err)
		}
		checkSidecarStats(stats)
		after := c1dParity(t, path, e, nil)
		if phase == 1 {
			for p, proxy := range e.ProxyChunks {
				g, _, err := content.ImportedWorldChunkCompiledGeometryIdentity(proxy, nil)
				oldG, _, oldErr := content.ImportedWorldChunkCompiledGeometryIdentity(first.ProxyChunks[p], nil)
				if err != nil || oldErr != nil || g != oldG {
					t.Fatal("phase1 fixture changed coarse proxy geometry")
				}
			}
			if stats.ProxyAuxReused != len(e.ProxyChunks) {
				t.Fatal("local proxies invalidated by full neighborhood change")
			}
		}
		left, distant := [3]int{}, [3]int{10, 0, 0}
		if after[left].EmbeddedAux.SourcePayloadHash != before[left].EmbeddedAux.SourcePayloadHash || reflect.DeepEqual(after[left].EmbeddedAux.Records, before[left].EmbeddedAux.Records) {
			t.Fatal("changed/deleted neighbor failed normal-only rebuild")
		}
		if after[distant].PayloadHash != before[distant].PayloadHash || stats.ChunkAuxReused != 1 {
			t.Fatalf("distant full lost reuse: %+v", stats)
		}
		before = after
	}
	if stats.ChunkAuxWritten != 0 || stats.ChunkAuxSkipped != 0 || stats.ProxyAuxWritten != 0 || stats.ProxyAuxSkipped != 0 {
		t.Fatal("embedded output wrote sidecars")
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "aux", "*.gkaux"))
	if len(matches) != 0 {
		t.Fatal("embedded emission created sidecars")
	}
}

func TestC1dDictionaryRewriteAndIncompatibleOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.gkworld")
	opts := ImportedWorldSaveOptions{ChunkPayloadKind: content.ImportedWorldChunkPayloadBrickZstdBinaryV1, EmbedNormals: true}
	e := c1dEmission(t, 0)
	if _, err := SaveImportedWorldEmissionWithOptionsResult(path, e, opts); err != nil {
		t.Fatal(err)
	}
	plain := c1dParity(t, path, e, nil)
	c, err := voxelcodec.New(voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 61, Bytes: bytes.Repeat([]byte("imported_chunk world_id c1d schema_version source_payload_hash normal_bake_version"), 32)}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	opts.ChunkCodec = c
	e = c1dEmission(t, 0)
	if _, err := SaveImportedWorldEmissionWithOptionsResult(path, e, opts); err != nil {
		t.Fatal(err)
	}
	dict := c1dParity(t, path, e, c)
	for key, want := range plain {
		if dict[key].PayloadHash != want.PayloadHash || !reflect.DeepEqual(dict[key].EmbeddedAux, want.EmbeddedAux) {
			t.Fatal("dictionary rewrite changed logical geometry/normals")
		}
	}
	framePaths := make([]string, 0, len(e.Chunks)+len(e.ProxyChunks))
	for _, entry := range e.Manifest.Entries {
		framePaths = append(framePaths, entry.ChunkPath)
	}
	for proxyPath := range e.ProxyChunks {
		framePaths = append(framePaths, proxyPath)
	}
	for _, framePath := range framePaths {
		wire, err := os.ReadFile(filepath.Join(filepath.Dir(path), framePath))
		if err != nil {
			t.Fatal(err)
		}
		if _, info, err := c.Decode(wire); err != nil || info.DictionaryID == 0 {
			t.Fatal("fixture did not emit dictionary frame", err)
		}
	}
	opts.ChunkCodec = nil
	fresh := c1dEmission(t, 0)
	stats, err := SaveImportedWorldEmissionWithOptionsResult(path, fresh, opts)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ChunkAuxReused != 0 || stats.ProxyAuxReused != 0 {
		t.Fatal("unreadable previous dictionary frame reused unchecked normals")
	}
	restored := c1dParity(t, path, fresh, nil)
	for key, want := range plain {
		if restored[key].PayloadHash != want.PayloadHash {
			t.Fatal("conservative rebake changed logical identity")
		}
	}
	if _, _, err := c.Encode(voxelcodec.Document{Kind: "borrowed"}); err != nil {
		t.Fatal("emitter closed borrowed codec", err)
	}
	for _, kind := range []string{content.ImportedWorldChunkPayloadSparseJSONV1, content.ImportedWorldChunkPayloadDenseRLEBinaryV1} {
		dir := t.TempDir()
		bad := filepath.Join(dir, "invalid.gkworld")
		if _, err := SaveImportedWorldEmissionWithOptionsResult(bad, c1dEmission(t, 0), ImportedWorldSaveOptions{ChunkPayloadKind: kind, EmbedNormals: true}); err == nil {
			t.Fatal("incompatible embedding accepted")
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatal("incompatible options wrote files")
		}
	}
}

func TestC1dSamePathMovedChunkInvalidatesOldAndNewNeighbors(t *testing.T) {
	makeEmission := func(moved bool) ImportedWorldEmission {
		e := c1dEmission(t, 0)
		e.ProxyChunks = nil
		e.Manifest.Sectors = nil
		near := *e.Chunks[[3]int{}]
		near.Coord = content.TerrainChunkCoordDef{X: 2}
		near.Voxels = []content.ImportedWorldVoxelDef{{X: 31, Value: 1}}
		near.NonEmptyVoxelCount = 1
		e.Chunks[[3]int{2, 0, 0}] = &near
		e.Manifest.Entries = append(e.Manifest.Entries, content.ImportedWorldChunkEntryDef{Coord: near.Coord, ChunkPath: "chunks/near2.gkchunk", NonEmptyVoxelCount: 1})
		if moved {
			chunk := e.Chunks[[3]int{1, 0, 0}]
			delete(e.Chunks, [3]int{1, 0, 0})
			chunk.Coord.X = 3
			e.Chunks[[3]int{3, 0, 0}] = chunk
			for i := range e.Manifest.Entries {
				if e.Manifest.Entries[i].Coord.X == 1 {
					e.Manifest.Entries[i].Coord = chunk.Coord
				}
			}
		}
		return e
	}
	path := filepath.Join(t.TempDir(), "world.gkworld")
	opts := ImportedWorldSaveOptions{ChunkPayloadKind: content.ImportedWorldChunkPayloadBrickZstdBinaryV1, EmbedNormals: true}
	old := makeEmission(false)
	if _, err := SaveImportedWorldEmissionWithOptionsResult(path, old, opts); err != nil {
		t.Fatal(err)
	}
	before := c1dParity(t, path, old, nil)
	next := makeEmission(true)
	stats, err := SaveImportedWorldEmissionWithOptionsResult(path, next, opts)
	if err != nil {
		t.Fatal(err)
	}
	after := c1dParity(t, path, next, nil)
	if stats.ChunkAuxReused != 1 {
		t.Fatalf("move failed old/new neighborhood invalidation: %+v", stats)
	}
	for _, key := range [][3]int{{0, 0, 0}, {2, 0, 0}} {
		if before[key].EmbeddedAux.SourcePayloadHash != after[key].EmbeddedAux.SourcePayloadHash || reflect.DeepEqual(before[key].EmbeddedAux.Records, after[key].EmbeddedAux.Records) {
			t.Fatal("move missed unchanged-geometry neighbor normals", key)
		}
	}
	if before[[3]int{10, 0, 0}].PayloadHash != after[[3]int{10, 0, 0}].PayloadHash {
		t.Fatal("move invalidated distant geometry")
	}
}
