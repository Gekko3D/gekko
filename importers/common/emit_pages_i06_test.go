package common

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	contentderived "github.com/gekko3d/gekko/content/derived"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func TestI06CommonPageBakeAdapterRoundTripAndOptions(t *testing.T) {
	voxels := []Voxel{{X: 0, Y: 0, Z: 0, Palette: 1, MaterialID: 1}, {X: 3, Y: 3, Z: 3, Palette: 2, MaterialID: 2}}
	materials := []Material{{ID: 1, PaletteIndex: 1, BaseColor: [4]uint8{100, 90, 80, 255}}, {ID: 2, PaletteIndex: 2, BaseColor: [4]uint8{50, 60, 70, 255}, EmitsLight: true, Emissive: 2}}
	before, err := json.Marshal([]any{voxels, materials})
	if err != nil {
		t.Fatal(err)
	}
	opts := ImportedWorldEmitOptions{WorldID: "adapter-common", ChunkSize: 4, VoxelResolution: 1, SourceHash: strings.Repeat("a", 64), PageBakeOptions: &contentderived.ImportedWorldPageBakeOptions{}}
	emission, err := BuildImportedWorldEmission(voxels, materials, opts)
	if err != nil {
		t.Fatal(err)
	}
	if emission.PageBake == nil || emission.Manifest == nil || emission.Manifest.SchemaVersion != 3 {
		t.Fatal("missing opt-in page draft")
	}
	path := filepath.Join(t.TempDir(), "world.gkworld")
	if err = SaveImportedWorldEmission(path, emission); err != nil {
		t.Fatal(err)
	}
	loaded, err := content.LoadImportedWorld(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != 3 || loaded.PageIndex == nil || loaded.PageIndex.LegacyDistance {
		t.Fatal("saved adapter did not produce strict v3")
	}
	if _, err = content.ValidateImportedWorldV3(loaded); err != nil {
		t.Fatal(err)
	}
	levels := map[uint8]bool{}
	for _, page := range loaded.Pages {
		levels[page.Level] = true
		p := page.Payload
		chunk, e := content.LoadImportedWorldChunk(content.ResolveDocumentPath(p.Path, path))
		if e != nil {
			t.Fatal(e)
		}
		if p.PayloadHash == "" || p.PayloadSizeBytes <= 0 || p.PayloadHash != chunk.PayloadHash || p.PayloadSizeBytes != chunk.PayloadSizeBytes || p.Kind != chunk.PayloadKind || p.ChunkSize != chunk.ChunkSize || p.VoxelResolution != chunk.VoxelResolution {
			t.Fatalf("page metadata mismatch: %+v", p)
		}
	}
	for level := uint8(0); level <= 3; level++ {
		if !levels[level] {
			t.Fatalf("missing tier %d", level)
		}
	}
	for _, entry := range loaded.Entries {
		chunk, e := content.LoadImportedWorldChunk(content.ResolveImportedWorldChunkPath(entry, path))
		if e != nil {
			t.Fatal(e)
		}
		if entry.PayloadHash == "" || entry.PayloadHash != chunk.PayloadHash || entry.PayloadSizeBytes <= 0 || entry.PayloadSizeBytes != chunk.PayloadSizeBytes || entry.PayloadKind != chunk.PayloadKind {
			t.Fatalf("full metadata mismatch: %+v", entry)
		}
	}
	after, err := json.Marshal([]any{voxels, materials})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("adapter mutated common inputs")
	}
	codec, err := voxelcodec.New(voxelcodec.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	for _, saveOpts := range []ImportedWorldSaveOptions{{ChunkPayloadKind: content.ImportedWorldChunkPayloadBrickZstdBinaryV1}, {ChunkPayloadKind: content.ImportedWorldChunkPayloadSparseJSONV1}, {EmbedNormals: true}, {ChunkCodec: codec}} {
		badPath := filepath.Join(t.TempDir(), "unsupported.gkworld")
		if err := SaveImportedWorldEmissionWithOptions(badPath, emission, saveOpts); err == nil {
			t.Fatalf("silently ignored page save options: %+v", saveOpts)
		}
	}
	opts.PageBakeOptions = nil
	legacy, err := BuildImportedWorldEmission(voxels, materials, opts)
	if err != nil || legacy.PageBake != nil || legacy.Manifest.SchemaVersion != 2 {
		t.Fatalf("nil option changed legacy path: %v", err)
	}
	legacyPath := filepath.Join(t.TempDir(), "legacy.gkworld")
	if err := SaveImportedWorldEmission(legacyPath, legacy); err != nil {
		t.Fatal(err)
	}
	legacyLoaded, err := content.LoadImportedWorld(legacyPath)
	if err != nil || legacyLoaded.SchemaVersion != 2 || legacyLoaded.PageIndex == nil || !legacyLoaded.PageIndex.LegacyDistance {
		t.Fatalf("legacy roundtrip changed dispatch: %v", err)
	}
	if !reflect.DeepEqual(legacyLoaded.Sectors, legacy.Manifest.Sectors) {
		t.Fatal("legacy saved sector/first-LOD fields changed on reload")
	}
}
