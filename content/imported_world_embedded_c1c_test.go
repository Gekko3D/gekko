package content_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c1cAux(t *testing.T, chunk *content.ImportedWorldChunkDef) *content.ImportedWorldChunkAuxDef {
	t.Helper()
	hash, size, err := content.ImportedWorldChunkCompiledGeometryIdentity(chunk, nil)
	if err != nil {
		t.Fatal(err)
	}
	aux := &content.ImportedWorldChunkAuxDef{WorldID: chunk.WorldID, SchemaVersion: content.CurrentImportedWorldChunkAuxSchemaVersion, Coord: chunk.Coord, ChunkSize: chunk.ChunkSize, VoxelResolution: chunk.VoxelResolution, NormalBakeVersion: content.ImportedWorldNormalBakeVersion, SourcePayloadHash: hash, SourcePayloadSizeBytes: size}
	// Partial coverage, intentionally reversed. Preserve normal words at unoccupied cells.
	for _, x := range []int{8, 0} {
		record := content.ImportedWorldBrickAuxDef{Origin: [3]int{x, 0, 0}, Bytes: make([]byte, 1088)}
		for i := 64; i < len(record.Bytes); i++ {
			record.Bytes[i] = byte(i + x)
		}
		mask := uint64(1)
		if x == 0 {
			mask |= 1 << 8
		}
		binary.LittleEndian.PutUint64(record.Bytes, mask)
		aux.Records = append(aux.Records, record)
	}
	return aux
}

func TestC1cEmbeddedIdentityBytesResaveAndRemoval(t *testing.T) {
	chunk := c1aImported()
	before := c1aImported()
	aux := c1cAux(t, chunk)
	if !reflect.DeepEqual(chunk, before) {
		t.Fatal("geometry projection mutated source")
	}
	projection, err := content.SaveImportedWorldChunkCompiledWithCodec(filepath.Join(t.TempDir(), "geometry.gkchunk"), chunk, nil)
	if err != nil || projection.PayloadHash != aux.SourcePayloadHash || projection.PayloadSizeBytes != aux.SourcePayloadSizeBytes {
		t.Fatal("geometry projection differs from existing C1a document", err)
	}
	path := filepath.Join(t.TempDir(), "embedded.gkchunk")
	auxBefore, _ := json.Marshal(aux)
	saved, err := content.SaveImportedWorldChunkCompiledWithAux(path, chunk, aux, nil)
	if err != nil {
		t.Fatal(err)
	}
	auxAfter, _ := json.Marshal(aux)
	if !bytes.Equal(auxBefore, auxAfter) || chunk.EmbeddedAux == nil || chunk.EmbeddedAux == aux {
		t.Fatal("save mutated caller bake or failed to publish owned layer")
	}
	ownedByte := chunk.EmbeddedAux.Records[0].Bytes[1087]
	aux.Records[1].Bytes[1087] ^= 1
	if chunk.EmbeddedAux.Records[0].Bytes[1087] != ownedByte {
		t.Fatal("save retained caller-owned bake bytes")
	}
	aux.Records[1].Bytes[1087] ^= 1
	loaded, err := content.LoadImportedWorldChunk(path)
	if err != nil || loaded == nil || loaded.EmbeddedAux == nil {
		t.Fatal("embedded decode missing", err)
	}
	got := loaded.EmbeddedAux
	authored, err := json.Marshal(loaded)
	if err != nil || bytes.Contains(authored, []byte("embedded_aux")) || bytes.Contains(authored, []byte("EmbeddedAux")) || bytes.Contains(authored, []byte("normal_bake_version")) {
		t.Fatal("embedded shipping layer leaked into authored JSON")
	}
	if got.SourcePayloadHash != aux.SourcePayloadHash || got.SourcePayloadSizeBytes != aux.SourcePayloadSizeBytes || saved.PayloadHash == aux.SourcePayloadHash || got.NormalBakeVersion != aux.NormalBakeVersion {
		t.Fatal("geometry/final identities conflated")
	}
	wantRecords := []content.ImportedWorldBrickAuxDef{aux.Records[1], aux.Records[0]}
	if !reflect.DeepEqual(got.Records, wantRecords) {
		t.Fatal("canonical records lost exact normal bytes")
	}
	legacy := *aux
	legacy.Records = wantRecords
	if err := content.SaveImportedWorldChunkAux(filepath.Join(t.TempDir(), "expected.gkaux"), &legacy); err != nil {
		t.Fatal(err)
	}
	if got.PayloadHash != legacy.PayloadHash || got.PayloadSizeBytes != legacy.PayloadSizeBytes {
		t.Fatal("auxiliary record identity differs from legacy encoding")
	}
	for _, options := range []bool{false, true} {
		var result content.ImportedWorldChunkSaveResult
		if options {
			result, err = content.SaveImportedWorldChunkWithOptionsResult(path, loaded, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadBrickZstdBinaryV1})
		} else {
			result, err = content.SaveImportedWorldChunkCompiledWithCodec(path, loaded, nil)
		}
		if err != nil || result.Wrote || result.PayloadHash != saved.PayloadHash {
			t.Fatal("no-op resave discarded normals", err)
		}
	}
	frame, _ := os.ReadFile(path)
	loaded.Voxels[0].MaterialValue ^= 1
	if _, err := content.SaveImportedWorldChunkCompiledWithCodec(path, loaded, nil); err == nil {
		t.Fatal("stale normals accepted after raw material edit")
	}
	unchanged, _ := os.ReadFile(path)
	if !bytes.Equal(frame, unchanged) {
		t.Fatal("rejected save changed file")
	}
	if _, err := content.SaveImportedWorldChunkCompiledWithAux(path, loaded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if loaded.EmbeddedAux != nil {
		t.Fatal("explicit nil retained stale source layer")
	}
	if result, err := content.SaveImportedWorldChunkCompiledWithCodec(path, loaded, nil); err != nil || result.Wrote {
		t.Fatal("ordinary save resurrected explicitly removed normals", err)
	}
	clean, err := content.LoadImportedWorldChunk(path)
	if err != nil || clean.EmbeddedAux != nil {
		t.Fatal("explicit nil did not remove layer", err)
	}
	// Decodes own their normal bytes, independently of the supplied bake.
	original := append([]byte(nil), aux.Records[1].Bytes...)
	got.Records[0].Bytes[1087] ^= 1
	if !bytes.Equal(aux.Records[1].Bytes, original) {
		t.Fatal("decode aliased bake input")
	}
}

func TestC1cEmbeddingRejectsInvalidBindingsAndMalformedFrames(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func(*content.ImportedWorldChunkAuxDef)
	}{
		{"source hash", func(a *content.ImportedWorldChunkAuxDef) { a.SourcePayloadHash = "wrong" }},
		{"source size", func(a *content.ImportedWorldChunkAuxDef) { a.SourcePayloadSizeBytes++ }},
		{"bake", func(a *content.ImportedWorldChunkAuxDef) { a.NormalBakeVersion = "old" }},
		{"world", func(a *content.ImportedWorldChunkAuxDef) { a.WorldID = "wrong" }},
		{"schema", func(a *content.ImportedWorldChunkAuxDef) { a.SchemaVersion++ }},
		{"coordinate", func(a *content.ImportedWorldChunkAuxDef) { a.Coord.X++ }},
		{"lattice", func(a *content.ImportedWorldChunkAuxDef) { a.VoxelResolution *= 2 }},
		{"alignment", func(a *content.ImportedWorldChunkAuxDef) { a.Records[0].Origin[0] = 1 }},
		{"unoccupied", func(a *content.ImportedWorldChunkAuxDef) { a.Records[0].Origin = [3]int{16, 0, 0} }},
		{"duplicates", func(a *content.ImportedWorldChunkAuxDef) { a.Records = append(a.Records, a.Records[0]) }},
		{"length", func(a *content.ImportedWorldChunkAuxDef) { a.Records[0].Bytes = a.Records[0].Bytes[:1087] }},
		{"occupancy", func(a *content.ImportedWorldChunkAuxDef) { a.Records[0].Bytes[0] ^= 2 }},
		{"empty", func(a *content.ImportedWorldChunkAuxDef) { a.Records = nil }},
	} {
		t.Run(change.name, func(t *testing.T) {
			chunk := c1aImported()
			aux := c1cAux(t, chunk)
			change.mutate(aux)
			result, err := content.SaveImportedWorldChunkCompiledWithAux(filepath.Join(t.TempDir(), "invalid"), chunk, aux, nil)
			if err == nil || result != (content.ImportedWorldChunkSaveResult{}) {
				t.Fatal("invalid bake published", err)
			}
		})
	}
	chunk := c1aImported()
	path := filepath.Join(t.TempDir(), "frame")
	codec := c1aContentCodec(t, voxelcodec.Options{})
	if _, err := content.SaveImportedWorldChunkCompiledWithAux(path, chunk, c1cAux(t, chunk), codec); err != nil {
		t.Fatal(err)
	}
	frame, _ := os.ReadFile(path)
	doc, _, err := codec.Decode(frame)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name   string
		mutate func(*voxelcodec.Document)
	}{
		{"false binding", func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(d.Metadata, []byte(chunk.EmbeddedAux.SourcePayloadHash), []byte("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"), 1)
		}},
		{"aux schema", func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(d.Metadata, []byte(`"aux":{"schema_version":1`), []byte(`"aux":{"schema_version":2`), 1)
		}},
		{"absent bake", func(d *voxelcodec.Document) { d.NormalBakeVersion = "" }},
		{"missing material", func(d *voxelcodec.Document) { d.Bricks[0].Materials = nil }},
		{"noncanonical metadata", func(d *voxelcodec.Document) { d.Metadata = append([]byte(" "), d.Metadata...) }},
		{"unknown metadata", func(d *voxelcodec.Document) {
			d.Metadata = append(append([]byte(nil), d.Metadata[:len(d.Metadata)-1]...), []byte(`,"unknown":1}`)...)
		}},
		{"normal version", func(d *voxelcodec.Document) { d.NormalBakeVersion = "old" }},
		{"occupancy disagreement", func(d *voxelcodec.Document) {
			for i := range d.Bricks {
				if d.Bricks[i].Aux != nil {
					d.Bricks[i].Aux[0] ^= 2
					break
				}
			}
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			d, _, err := codec.Decode(frame)
			if err != nil {
				t.Fatal(err)
			}
			change.mutate(&d)
			wire, _, err := codec.Encode(d)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, wire, 0600); err != nil {
				t.Fatal(err)
			}
			if got, err := content.LoadImportedWorldChunk(path); err == nil || got != nil {
				t.Fatal("checksum-valid malformed embedded frame published")
			}
		})
	}
	// Geometry-only C1a metadata acceptance remains permissive.
	if _, err := content.SaveImportedWorldChunkCompiledWithAux(path, chunk, nil, codec); err != nil {
		t.Fatal(err)
	}
	plain, _ := os.ReadFile(path)
	doc, _, err = codec.Decode(plain)
	if err != nil {
		t.Fatal(err)
	}
	doc.Metadata = append([]byte(" "), doc.Metadata...)
	doc.Bricks[0].Materials = nil
	plain, _, err = codec.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, plain, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := content.LoadImportedWorldChunk(path); err != nil || got == nil || got.EmbeddedAux != nil {
		t.Fatal("embedded restrictions narrowed plain C1a acceptance", err)
	}
	// Strict embedding must not narrow the legacy auxiliary reader.
	legacy := c1cAux(t, c1aImported())
	legacy.Records = []content.ImportedWorldBrickAuxDef{{Origin: [3]int{-1, 3, 0}, Bytes: []byte{0, 255}}}
	if err := content.SaveImportedWorldChunkAux(path, legacy); err != nil {
		t.Fatal(err)
	}
	got, err := content.LoadImportedWorldChunkAux(path)
	if err != nil || !reflect.DeepEqual(got.Records, legacy.Records) {
		t.Fatal("legacy permissive records rejected", err)
	}
}
