package content

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestI06AuxBoundsRejectUntrustedCountWithoutExpansion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "world.gkworld")
	chunk := &ImportedWorldChunkDef{WorldID: "v3-world", SchemaVersion: 1, ChunkSize: 4, VoxelResolution: 1, NonEmptyVoxelCount: 1, Voxels: []ImportedWorldVoxelDef{{Value: 1}}}
	if _, err := SaveImportedWorldChunkWithOptionsResult(filepath.Join(dir, "full.gkchunk"), chunk, ImportedWorldChunkSaveOptions{PayloadKind: ImportedWorldChunkPayloadDenseRLEBinaryV1}); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, 1<<18)
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	metadata, err := json.Marshal(map[string]any{"world_id": chunk.WorldID, "schema_version": 1, "coord": chunk.Coord, "chunk_size": 4, "voxel_resolution": 1, "payload_kind": ImportedWorldChunkAuxPayloadBinaryV1, "payload_hash": hash, "payload_size_bytes": 4, "normal_bake_version": ImportedWorldNormalBakeVersion, "source_payload_hash": chunk.PayloadHash, "source_payload_size_bytes": chunk.PayloadSizeBytes})
	if err != nil {
		t.Fatal(err)
	}
	frame := append([]byte("GKAUX1\n"), make([]byte, 4)...)
	binary.LittleEndian.PutUint32(frame[7:], uint32(len(metadata)))
	frame = append(frame, metadata...)
	frame = append(frame, body...)
	if err = os.WriteFile(filepath.Join(dir, "bad.gkchunkaux"), frame, 0600); err != nil {
		t.Fatal(err)
	}
	d := i06World()
	d.Pages = d.Pages[:1]
	d.RootPageIndices = []uint32{0}
	d.IndexedSectors = nil
	d.Entries[0] = ImportedWorldChunkEntryDef{ChunkPath: "full.gkchunk", NonEmptyVoxelCount: 1, PayloadKind: chunk.PayloadKind, PayloadHash: chunk.PayloadHash, PayloadSizeBytes: chunk.PayloadSizeBytes}
	d.Pages[0].Payload = StreamPagePayloadDef{Kind: chunk.PayloadKind, Path: "full.gkchunk", ChunkSize: 4, VoxelResolution: 1, PayloadHash: chunk.PayloadHash, PayloadSizeBytes: chunk.PayloadSizeBytes}
	opts := ImportedWorldValidationOptions{DocumentPath: path}
	if control := ValidateImportedWorld(d, opts); control.HasErrors() {
		t.Fatalf("valid geometry control: %s", control.Error())
	}
	d.Pages[0].Payload.Aux = &ImportedWorldChunkAuxRefDef{AuxPath: "bad.gkchunkaux", PayloadKind: ImportedWorldChunkAuxPayloadBinaryV1, PayloadHash: hash, PayloadSizeBytes: 4, NormalBakeVersion: ImportedWorldNormalBakeVersion, SourcePayloadHash: chunk.PayloadHash, SourcePayloadSizeBytes: chunk.PayloadSizeBytes}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	result := ValidateImportedWorld(d, opts)
	runtime.ReadMemStats(&after)
	if !result.HasErrors() {
		t.Fatal("truncated claimed aux records accepted")
	}
	// The four-byte body cannot contain even one record. Allow ample ordinary
	// validation overhead while preventing allocation proportional to its count.
	if delta := after.TotalAlloc - before.TotalAlloc; delta > 4<<20 {
		t.Fatalf("tiny malformed aux expanded untrusted record count: allocated %d bytes", delta)
	}
}
