package derived

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func i09PublicationOptions() IslandStreamHarnessOptions {
	return IslandStreamHarnessOptions{PlayableSpan: 256, CoverageSpan: 512, RootSpan: 256, MacroSpan: 64, RegionalSpan: 16, HeightTileSpan: 256, HeightSampleSpacing: 2}
}
func i09PublicationFixture(t *testing.T) (string, *content.ImportedWorldDef, func(content.ImportedWorldChunkEntryDef) (*content.ImportedWorldChunkDef, error)) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "gasworks.gkworld")
	chunk := &content.ImportedWorldChunkDef{WorldID: "fixture-gasworks", ChunkSize: 4, VoxelResolution: 1, Voxels: []content.ImportedWorldVoxelDef{{X: 1, Y: 1, Z: 1, Value: 7, MaterialValue: 2}, {X: 2, Y: 1, Z: 1, Value: 8, MaterialValue: 3}}, NonEmptyVoxelCount: 2}
	result, e := content.SaveImportedWorldChunkWithOptionsResult(filepath.Join(dir, "full.gkchunk"), chunk, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1})
	if e != nil {
		t.Fatal(e)
	}
	if result.PayloadKind != content.ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1 {
		t.Fatalf("material binary fixture was not emitted: %s", result.PayloadKind)
	}
	aux := BuildImportedWorldChunkAux(chunk, map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{{}: chunk}, result.PayloadHash, result.PayloadSizeBytes, true)
	if aux == nil {
		t.Fatal("missing aux control")
	}
	if e = content.SaveImportedWorldChunkAux(filepath.Join(dir, "full.gkaux"), aux); e != nil {
		t.Fatal(e)
	}
	d := &content.ImportedWorldDef{SchemaVersion: 2, WorldID: chunk.WorldID, SourceHash: strings.Repeat("b", 64), Kind: content.ImportedWorldKindVoxelWorld, ChunkSize: 4, VoxelResolution: 1, Materials: []content.ImportedWorldMaterialDef{{PaletteIndex: 2}, {PaletteIndex: 3, Transparent: true, Transparency: 1}}, Entries: []content.ImportedWorldChunkEntryDef{{ChunkPath: "full.gkchunk", NonEmptyVoxelCount: 2, PayloadKind: result.PayloadKind, PayloadHash: result.PayloadHash, PayloadSizeBytes: result.PayloadSizeBytes, Aux: content.ImportedWorldChunkAuxRef("full.gkaux", aux)}}, Sectors: []content.ImportedWorldSectorDef{{BoundsMax: [3]float32{4, 4, 4}, FullChunkRefs: []content.TerrainChunkCoordDef{{}}, VisibleSectorRefs: []content.TerrainChunkCoordDef{{}}}}}
	if e = content.SaveImportedWorld(p, d); e != nil {
		t.Fatal(e)
	}
	load := func(entry content.ImportedWorldChunkEntryDef) (*content.ImportedWorldChunkDef, error) {
		return content.LoadImportedWorldChunk(content.ResolveImportedWorldChunkPath(entry, p))
	}
	return p, d, load
}
func i09FileTree(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	e := filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		r, e := filepath.Rel(dir, p)
		if e != nil {
			return e
		}
		out[filepath.ToSlash(r)], e = os.ReadFile(p)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func i09ReadPublished(t *testing.T, out string) (*content.LevelDef, *content.TerrainChunkManifestDef, *content.ImportedWorldDef) {
	t.Helper()
	p := filepath.Join(out, "island_streaming_harness.gklevel")
	l, e := content.LoadLevel(p)
	if e != nil {
		t.Fatal(e)
	}
	if l.Terrain == nil || l.BaseWorld == nil {
		t.Fatal("missing published layer references")
	}
	tp := content.ResolveDocumentPath(l.Terrain.ManifestPath, p)
	wp := content.ResolveDocumentPath(l.BaseWorld.ManifestPath, p)
	for _, inner := range []string{tp, wp} {
		rel, e := filepath.Rel(out, inner)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			t.Fatalf("generated inner manifest escaped output: %s", inner)
		}
	}
	terrain, e := content.LoadTerrainChunkManifest(tp)
	if e != nil {
		t.Fatal(e)
	}
	poi, e := content.LoadImportedWorld(wp)
	if e != nil {
		t.Fatal(e)
	}
	if terrain.SchemaVersion != 3 || poi.SchemaVersion != 3 {
		t.Fatal("harness layers are not explicit3")
	}
	if r := content.ValidateLevel(l, content.LevelValidationOptions{DocumentPath: p}); r.HasErrors() {
		t.Fatalf("published level fails independent validation: %+v", r.Issues)
	}
	for _, entry := range terrain.Entries {
		if _, e := content.LoadTerrainHeightTileEntry(entry, tp); e != nil {
			t.Fatal(e)
		}
	}
	for i := range terrain.Pages {
		if _, e := content.LoadTerrainHeightPagePayload(terrain, tp, uint32(i)); e != nil {
			t.Fatal(e)
		}
	}
	if r := content.ValidateImportedWorld(poi, content.ImportedWorldValidationOptions{DocumentPath: wp}); r.HasErrors() {
		t.Fatalf("published POI payload qualification: %+v", r.Issues)
	}
	return l, terrain, poi
}
func TestI09HarnessPublicationReferencesOriginalFullAndAux(t *testing.T) {
	p, d, load := i09PublicationFixture(t)
	original := i09FileTree(t, filepath.Dir(p))
	h, e := BuildIslandStreamHarness(d, load, i09PublicationOptions())
	if e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(t.TempDir(), "fixture")
	if e = SaveIslandStreamHarness(out, p, h); e != nil {
		t.Fatal(e)
	}
	l, _, poi := i09ReadPublished(t, out)
	wp := content.ResolveDocumentPath(l.BaseWorld.ManifestPath, filepath.Join(out, "island_streaming_harness.gklevel"))
	if len(poi.Entries) != len(d.Entries) {
		t.Fatal("full membership changed")
	}
	for i, entry := range poi.Entries {
		want := d.Entries[i]
		if content.ResolveImportedWorldChunkPath(entry, wp) != content.ResolveImportedWorldChunkPath(want, p) {
			t.Fatal("full payload copied instead of referenced")
		}
		if entry.Aux == nil || content.ResolveDocumentPath(entry.Aux.AuxPath, wp) != content.ResolveDocumentPath(want.Aux.AuxPath, p) {
			t.Fatal("full aux copied instead of referenced")
		}
		if entry.OccupiedSectorCount != 1 || entry.OccupiedBrickCount != 1 {
			t.Fatalf("full occupied costs not qualified: %+v", entry)
		}
		a, b := entry, want
		// Optional costs are newly qualified metadata, not changed payload bytes.
		a.OccupiedSectorCount, a.OccupiedBrickCount = b.OccupiedSectorCount, b.OccupiedBrickCount
		a.ChunkPath = b.ChunkPath
		a.Aux = &content.ImportedWorldChunkAuxRefDef{}
		*a.Aux = *entry.Aux
		a.Aux.AuxPath = b.Aux.AuxPath
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("original full identity/reference metadata changed: %+v vs %+v", a, b)
		}
	}
	if !reflect.DeepEqual(original, i09FileTree(t, filepath.Dir(p))) {
		t.Fatal("publication wrote original Gasworks tree")
	}
	for _, raw := range i09FileTree(t, out) {
		if bytes.Equal(raw, original["full.gkchunk"]) || bytes.Equal(raw, original["full.gkaux"]) {
			t.Fatal("original full/aux bytes duplicated under harness")
		}
	}
	before := i09FileTree(t, out)
	if e = SaveIslandStreamHarness(out, p, h); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, i09FileTree(t, out)) {
		t.Fatal("repeat publication changed bytes or added files")
	}
}
func TestI09HarnessPublicationDeterministicAcrossSiblingOutputs(t *testing.T) {
	p, d, load := i09PublicationFixture(t)
	parent := t.TempDir()
	var first map[string][]byte
	for _, name := range []string{"first", "second"} {
		h, e := BuildIslandStreamHarness(d, load, i09PublicationOptions())
		if e != nil {
			t.Fatal(e)
		}
		out := filepath.Join(parent, name)
		if e = SaveIslandStreamHarness(out, p, h); e != nil {
			t.Fatal(e)
		}
		i09ReadPublished(t, out)
		tree := i09FileTree(t, out)
		if first == nil {
			first = tree
		} else if !reflect.DeepEqual(first, tree) {
			t.Fatal("same recipe changed payload/manifest/marker bytes with sibling output name")
		}
	}
}
func TestI09HarnessPublicationRejectsInvalidBeforeTargetWrites(t *testing.T) {
	for _, bad := range []string{"nil", "draft", "source-header", "source-body", "wrong-manifest", "escape"} {
		t.Run(bad, func(t *testing.T) {
			p, d, load := i09PublicationFixture(t)
			h, e := BuildIslandStreamHarness(d, load, i09PublicationOptions())
			if e != nil {
				t.Fatal(e)
			}
			out := filepath.Join(t.TempDir(), "absent")
			switch bad {
			case "nil":
				h = nil
			case "draft":
				h.Level.Name = "tampered"
			case "escape":
				h.Level.BaseWorld.ManifestPath = "../escaped.gkworld"
			case "wrong-manifest":
				other := *d
				other.WorldID = "different"
				p = filepath.Join(filepath.Dir(p), "other.gkworld")
				if e = content.SaveImportedWorld(p, &other); e != nil {
					t.Fatal(e)
				}
			case "source-header":
				c, e := load(d.Entries[0])
				if e != nil {
					t.Fatal(e)
				}
				c.WorldID = "wrong-owner"
				if e = content.SaveImportedWorldChunkWithOptions(content.ResolveImportedWorldChunkPath(d.Entries[0], p), c, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1}); e != nil {
					t.Fatal(e)
				}
			case "source-body":
				q := content.ResolveImportedWorldChunkPath(d.Entries[0], p)
				raw, e := os.ReadFile(q)
				if e != nil {
					t.Fatal(e)
				}
				raw[len(raw)-1] ^= 1
				if e = os.WriteFile(q, raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if e = SaveIslandStreamHarness(out, p, h); e == nil {
				t.Fatal("invalid publication accepted")
			}
			if _, e = os.Stat(out); !os.IsNotExist(e) {
				t.Fatalf("invalid publication created target: %v", e)
			}
		})
	}
}
func TestI09HarnessPublicationRejectsOutputOverlapAndSymlinkEscapes(t *testing.T) {
	p, d, load := i09PublicationFixture(t)
	original := i09FileTree(t, filepath.Dir(p))
	h, e := BuildIslandStreamHarness(d, load, i09PublicationOptions())
	if e != nil {
		t.Fatal(e)
	}
	if e = SaveIslandStreamHarness(filepath.Dir(p), p, h); e == nil {
		t.Fatal("output overlapping source tree accepted")
	}
	if !reflect.DeepEqual(original, i09FileTree(t, filepath.Dir(p))) {
		t.Fatal("overlap modified source files")
	}
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "sentinel")
	if e = os.WriteFile(sentinel, []byte("untouched"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"root", "inner"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			out := filepath.Join(base, "fixture")
			if mode == "root" {
				if e := os.Symlink(outside, out); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := os.MkdirAll(out, 0755); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(outside, filepath.Join(out, "terrain")); e != nil {
					t.Fatal(e)
				}
			}
			if e := SaveIslandStreamHarness(out, p, h); e == nil {
				t.Fatal("symlink publication escape accepted")
			}
			tree := i09FileTree(t, outside)
			if len(tree) != 1 || !bytes.Equal(tree["sentinel"], []byte("untouched")) {
				t.Fatal("publication wrote outside output")
			}
		})
	}
}
func TestI09HarnessPublicationFailurePreservesOldRootAndReferences(t *testing.T) {
	p, d, load := i09PublicationFixture(t)
	out := filepath.Join(t.TempDir(), "fixture")
	h, e := BuildIslandStreamHarness(d, load, i09PublicationOptions())
	if e != nil {
		t.Fatal(e)
	}
	if e = SaveIslandStreamHarness(out, p, h); e != nil {
		t.Fatal(e)
	}
	before := i09FileTree(t, out)
	i09ReadPublished(t, out)
	opts := i09PublicationOptions()
	opts.PlayableSpan = 320
	next, e := BuildIslandStreamHarness(d, load, opts)
	if e != nil {
		t.Fatal(e)
	}
	if next.Level.BaseWorld.ManifestPath == h.Level.BaseWorld.ManifestPath {
		t.Fatal("inner manifest paths are not generation-qualified")
	}
	blocker := content.ResolveDocumentPath(next.Level.BaseWorld.ManifestPath, filepath.Join(out, "island_streaming_harness.gklevel"))
	if e = os.MkdirAll(blocker, 0755); e != nil {
		t.Fatal(e)
	}
	if e = SaveIslandStreamHarness(out, p, next); e == nil {
		t.Fatal("directory blocker did not fail publication")
	}
	after := i09FileTree(t, out)
	for key, want := range before {
		if !bytes.Equal(want, after[key]) {
			t.Fatalf("old generation changed on late failure: %s", key)
		}
	}
	i09ReadPublished(t, out)
}
func TestI09HarnessPublicationImmutableCollisionPreservesUnrelatedFiles(t *testing.T) {
	p, d, load := i09PublicationFixture(t)
	h, e := BuildIslandStreamHarness(d, load, i09PublicationOptions())
	if e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(t.TempDir(), "fixture")
	if e = SaveIslandStreamHarness(out, p, h); e != nil {
		t.Fatal(e)
	}
	unrelated := filepath.Join(out, "user-note.txt")
	if e = os.WriteFile(unrelated, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	l, terrain, _ := i09ReadPublished(t, out)
	mp := content.ResolveDocumentPath(l.Terrain.ManifestPath, filepath.Join(out, "island_streaming_harness.gklevel"))
	paths := make([]string, 0, len(terrain.Pages))
	for _, page := range terrain.Pages {
		paths = append(paths, content.ResolveDocumentPath(page.Payload.Path, mp))
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("missing generated payload")
	}
	if e = os.WriteFile(paths[0], []byte("different immutable bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	before := i09FileTree(t, out)
	if e = SaveIslandStreamHarness(out, p, h); e == nil {
		t.Fatal("differing existing generation payload overwritten")
	}
	if !reflect.DeepEqual(before, i09FileTree(t, out)) {
		t.Fatal("failed immutable collision changed output")
	}
}
