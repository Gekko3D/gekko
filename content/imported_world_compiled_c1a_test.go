package content_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c1aImported() *content.ImportedWorldChunkDef {
	return &content.ImportedWorldChunkDef{WorldID: "world-c1a", SchemaVersion: content.CurrentImportedWorldChunkSchemaVersion, Coord: content.TerrainChunkCoordDef{X: -4, Y: 2, Z: -3}, ChunkSize: 32, VoxelResolution: 0.125, Tags: []string{"compiled"}, Voxels: []content.ImportedWorldVoxelDef{
		{X: 0, Y: 1, Z: 0, Value: 5, MaterialValue: 5}, {X: 8, Y: 0, Z: 0, Value: 255, MaterialValue: 7},
		{X: 0, Y: 0, Z: 0, Value: 3}, {X: 31, Y: 31, Z: 31, Value: 7, MaterialValue: 255},
		{X: 8, Y: 0, Z: 0, Value: 0, MaterialValue: 255}, {X: 999, Value: 0},
	}}
}

func c1aContentCodec(t *testing.T, options voxelcodec.Options) *voxelcodec.Codec {
	t.Helper()
	c, err := voxelcodec.New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestC1aImportedCompiledRoundTripIdentityAndUnchangedSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chunk.gkchunk")
	def := c1aImported()
	source := slices.Clone(def.Voxels)
	result, err := content.SaveImportedWorldChunkWithOptionsResult(path, def, content.ImportedWorldChunkSaveOptions{PayloadKind: "brick_zstd_binary_v1"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Wrote || result.PayloadKind != "brick_zstd_binary_v1" || !slices.Equal(def.Voxels, source) {
		t.Fatal("compiled save lost its new kind or mutated source records")
	}
	loaded, err := content.LoadImportedWorldChunk(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []content.ImportedWorldVoxelDef{source[2], source[1], source[0], source[3]}
	if !slices.Equal(loaded.Voxels, want) || loaded.NonEmptyVoxelCount != len(want) || loaded.WorldID != def.WorldID || loaded.Coord != def.Coord || loaded.ChunkSize != def.ChunkSize || loaded.VoxelResolution != def.VoxelResolution || !slices.Equal(loaded.Tags, def.Tags) {
		t.Fatal("compiled adapter lost raw palette/material bytes, lattice metadata or global x-fast order")
	}
	frame, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	codec := c1aContentCodec(t, voxelcodec.Options{})
	doc, info, err := codec.Decode(frame)
	if err != nil || doc.Kind != "imported_chunk" || info.ContentID != loaded.PayloadHash || result.PayloadHash != loaded.PayloadHash || int64(loaded.PayloadSizeBytes) != int64(info.DecodedBytes) || result.PayloadSizeBytes != loaded.PayloadSizeBytes {
		t.Fatal("compiled identity/decoded-size metadata differs from canonical document")
	}
	slices.Reverse(def.Voxels)
	repeated, err := content.SaveImportedWorldChunkWithOptionsResult(path, def, content.ImportedWorldChunkSaveOptions{PayloadKind: "brick_zstd_binary_v1"})
	if err != nil || repeated.Wrote || repeated.PayloadHash != result.PayloadHash {
		t.Fatal("equivalent reordered save rewrote canonical content")
	}
	loaded.Voxels[0].Value = 99
	again, err := content.LoadImportedWorldChunk(path)
	if err != nil || !slices.Equal(again.Voxels, want) {
		t.Fatal("separate file decode reused mutable output records")
	}
}

func TestC1aImportedExplicitDictionaryAndLegacyDispatch(t *testing.T) {
	root := t.TempDir()
	plain := c1aContentCodec(t, voxelcodec.Options{})
	dict := c1aContentCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 17, Bytes: []byte("world-c1a imported_chunk voxel material palette history")}})
	path := filepath.Join(root, "dictionary.gkchunk")
	def := c1aImported()
	result, err := content.SaveImportedWorldChunkCompiledWithCodec(path, def, dict)
	if err != nil {
		t.Fatal(err)
	}
	if loaded, err := content.LoadImportedWorldChunk(path); err == nil || loaded != nil {
		t.Fatal("default loader silently accepted a missing dictionary")
	}
	loaded, err := content.LoadImportedWorldChunkWithCodec(path, dict)
	if err != nil || loaded.PayloadHash != result.PayloadHash || len(loaded.Voxels) != 4 {
		t.Fatal("explicit dictionary profile failed compiled loading")
	}
	other := c1aImported()
	plainResult, err := content.SaveImportedWorldChunkCompiledWithCodec(filepath.Join(root, "plain.gkchunk"), other, plain)
	if err != nil || plainResult.PayloadHash != result.PayloadHash {
		t.Fatal("dictionary profile changed imported logical identity")
	}
	// Legacy files retain their own material normalization and dispatch behavior.
	for _, kind := range []string{content.ImportedWorldChunkPayloadSparseJSONV1, content.ImportedWorldChunkPayloadDenseRLEBinaryV1} {
		legacy := &content.ImportedWorldChunkDef{WorldID: "legacy", ChunkSize: 8, VoxelResolution: 1, Voxels: []content.ImportedWorldVoxelDef{{X: 1, Value: 4, MaterialValue: 12}}}
		legacyPath := filepath.Join(root, kind+".gkchunk")
		if err := content.SaveImportedWorldChunkWithOptions(legacyPath, legacy, content.ImportedWorldChunkSaveOptions{PayloadKind: kind}); err != nil {
			t.Fatal(err)
		}
		read, err := content.LoadImportedWorldChunkWithCodec(legacyPath, dict)
		if err != nil || !reflect.DeepEqual(read.Voxels, legacy.Voxels) {
			t.Fatal("new explicit profile changed legacy JSON/RLE dispatch")
		}
	}
	aux := &content.ImportedWorldChunkAuxDef{WorldID: "legacy", ChunkSize: 8, VoxelResolution: 1, SourcePayloadHash: "source", SourcePayloadSizeBytes: 77, Records: []content.ImportedWorldBrickAuxDef{{Origin: [3]int{}, Bytes: []byte{0, 255, 3, 0}}}}
	auxPath := filepath.Join(root, "legacy.gkaux")
	if err := content.SaveImportedWorldChunkAux(auxPath, aux); err != nil {
		t.Fatal(err)
	}
	readAux, err := content.LoadImportedWorldChunkAux(auxPath)
	if err != nil || !reflect.DeepEqual(readAux.Records, aux.Records) || readAux.SourcePayloadHash != "source" {
		t.Fatal("compiled introduction changed legacy aux bytes/source reference")
	}
}

func TestC1aImportedRejectsMalformedMetadataAndOccupiedInputs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "chunk.gkchunk")
	c := c1aContentCodec(t, voxelcodec.Options{})
	if _, err := content.SaveImportedWorldChunkCompiledWithCodec(path, c1aImported(), c); err != nil {
		t.Fatal(err)
	}
	frame, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*voxelcodec.Document){
		func(d *voxelcodec.Document) { d.Kind = "other" },
		func(d *voxelcodec.Document) {
			var meta map[string]any
			json.Unmarshal(d.Metadata, &meta)
			meta["chunk_size"] = 1
			d.Metadata, _ = json.Marshal(meta)
		},
		func(d *voxelcodec.Document) {
			var meta map[string]any
			json.Unmarshal(d.Metadata, &meta)
			meta["schema_version"] = 999
			d.Metadata, _ = json.Marshal(meta)
		},
		func(d *voxelcodec.Document) {
			var meta map[string]any
			json.Unmarshal(d.Metadata, &meta)
			meta["non_empty_voxel_count"] = 999
			d.Metadata, _ = json.Marshal(meta)
		},
	} {
		doc, _, err := c.Decode(frame)
		if err != nil {
			t.Fatal(err)
		}
		mutate(&doc)
		bad, _, err := c.Encode(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, bad, 0644); err != nil {
			t.Fatal(err)
		}
		if partial, err := content.LoadImportedWorldChunkWithCodec(path, c); err == nil || partial != nil {
			t.Fatal("new imported metadata/lattice mismatch published a chunk")
		}
	}
	for _, bad := range []*content.ImportedWorldChunkDef{nil, {WorldID: "bad", ChunkSize: 8, VoxelResolution: 1, Voxels: []content.ImportedWorldVoxelDef{{Value: 1}, {Value: 2}}}, {WorldID: "bad", ChunkSize: 8, VoxelResolution: 1, Voxels: []content.ImportedWorldVoxelDef{{X: 8, Value: 1}}}} {
		if _, err := content.SaveImportedWorldChunkCompiledWithCodec(path, bad, c); err == nil {
			t.Fatal("duplicate/out-of-lattice occupied input was accepted")
		}
	}
}
