package content

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestImportedWorldChunkAuxRoundTripPreservesRecordsAndHash(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "worlds", "aux", "chunk.gkaux")
	aux := &ImportedWorldChunkAuxDef{
		WorldID:                "world-a",
		Coord:                  TerrainChunkCoordDef{X: 1, Y: 2, Z: 3},
		ChunkSize:              32,
		VoxelResolution:        0.125,
		SourcePayloadHash:      "chunk-hash",
		SourcePayloadSizeBytes: 77,
		Records: []ImportedWorldBrickAuxDef{{
			Origin: [3]int{8, 0, 0},
			Bytes:  []byte{1, 2, 3, 4},
		}},
	}
	if err := SaveImportedWorldChunkAux(path, aux); err != nil {
		t.Fatalf("SaveImportedWorldChunkAux failed: %v", err)
	}
	if aux.PayloadHash == "" || aux.PayloadSizeBytes <= 0 {
		t.Fatalf("expected aux payload metadata to be populated, got %+v", aux)
	}
	loaded, err := LoadImportedWorldChunkAux(path)
	if err != nil {
		t.Fatalf("LoadImportedWorldChunkAux failed: %v", err)
	}
	if loaded.SchemaVersion != CurrentImportedWorldChunkAuxSchemaVersion || loaded.PayloadKind != ImportedWorldChunkAuxPayloadBinaryV1 {
		t.Fatalf("unexpected loaded aux metadata %+v", loaded)
	}
	if loaded.NormalBakeVersion != ImportedWorldNormalBakeVersion || loaded.SourcePayloadHash != "chunk-hash" || loaded.SourcePayloadSizeBytes != 77 {
		t.Fatalf("expected bake/source metadata to round-trip, got %+v", loaded)
	}
	if len(loaded.Records) != 1 || loaded.Records[0].Origin != ([3]int{8, 0, 0}) || !bytes.Equal(loaded.Records[0].Bytes, []byte{1, 2, 3, 4}) {
		t.Fatalf("expected aux record to round-trip, got %+v", loaded.Records)
	}
}

func TestImportedWorldChunkAuxRejectsPayloadHashMismatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "chunk.gkaux")
	aux := &ImportedWorldChunkAuxDef{
		WorldID:         "world-a",
		Coord:           TerrainChunkCoordDef{},
		ChunkSize:       32,
		VoxelResolution: 1,
		Records:         []ImportedWorldBrickAuxDef{{Origin: [3]int{}, Bytes: []byte{1, 2, 3}}},
	}
	if err := SaveImportedWorldChunkAux(path, aux); err != nil {
		t.Fatalf("SaveImportedWorldChunkAux failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImportedWorldChunkAux(path); err == nil {
		t.Fatal("expected hash mismatch to fail")
	}
}

func TestDefaultImportedWorldChunkAuxPathUsesDerivedAuxDirectory(t *testing.T) {
	got := DefaultImportedWorldChunkAuxPath("chunks/demo_0_0_0.gkchunk")
	if got != "aux/demo_0_0_0.gkaux" {
		t.Fatalf("unexpected aux path %q", got)
	}
}
