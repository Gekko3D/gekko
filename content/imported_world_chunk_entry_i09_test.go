package content

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func i09EntryFixture(t *testing.T) (*ImportedWorldDef, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "world.gkworld")
	c := &ImportedWorldChunkDef{WorldID: "entry-i09", ChunkSize: 4, VoxelResolution: .1, Voxels: []ImportedWorldVoxelDef{{X: 1, Y: 1, Z: 1, Value: 7, MaterialValue: 2}}, NonEmptyVoxelCount: 1}
	saved, e := SaveImportedWorldChunkWithOptionsResult(filepath.Join(filepath.Dir(p), "full.gkchunk"), c, ImportedWorldChunkSaveOptions{PayloadKind: ImportedWorldChunkPayloadDenseRLEBinaryV1})
	if e != nil {
		t.Fatal(e)
	}
	if saved.PayloadKind != ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1 {
		t.Fatalf("material binary fixture was not emitted: %s", saved.PayloadKind)
	}
	aux := &ImportedWorldChunkAuxDef{WorldID: c.WorldID, ChunkSize: 4, VoxelResolution: .1, NormalBakeVersion: ImportedWorldNormalBakeVersion, SourcePayloadHash: saved.PayloadHash, SourcePayloadSizeBytes: saved.PayloadSizeBytes, Records: []ImportedWorldBrickAuxDef{{Bytes: make([]byte, 1088)}}}
	if e = SaveImportedWorldChunkAux(filepath.Join(filepath.Dir(p), "full.gkaux"), aux); e != nil {
		t.Fatal(e)
	}
	d := &ImportedWorldDef{SchemaVersion: 2, WorldID: c.WorldID, Kind: ImportedWorldKindVoxelWorld, SourceHash: strings.Repeat("a", 64), ChunkSize: 4, VoxelResolution: .1, Entries: []ImportedWorldChunkEntryDef{{ChunkPath: "full.gkchunk", NonEmptyVoxelCount: 1, PayloadKind: saved.PayloadKind, PayloadHash: saved.PayloadHash, PayloadSizeBytes: saved.PayloadSizeBytes, Aux: ImportedWorldChunkAuxRef("full.gkaux", aux)}}}
	return d, p
}
func i09EntryHeader(t *testing.T, path string, aux bool, mutate func(map[string]any)) {
	t.Helper()
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	prefix, offset := 12, 8
	if aux {
		prefix, offset = 11, 7
	}
	n := int(binary.LittleEndian.Uint32(raw[offset:prefix]))
	var m map[string]any
	if e = json.Unmarshal(raw[prefix:prefix+n], &m); e != nil {
		t.Fatal(e)
	}
	mutate(m)
	meta, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	out := append([]byte(nil), raw[:prefix]...)
	binary.LittleEndian.PutUint32(out[offset:prefix], uint32(len(meta)))
	out = append(out, meta...)
	out = append(out, raw[prefix+n:]...)
	if e = os.WriteFile(path, out, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestI09QualifiedFullEntryPreservesMaterialAndChecksBeforeExpansion(t *testing.T) {
	d, p := i09EntryFixture(t)
	got, e := LoadImportedWorldChunkEntry(d, p, 0)
	if e != nil {
		t.Fatal(e)
	}
	if len(got.Voxels) != 1 || got.Voxels[0].Value != 7 || got.Voxels[0].MaterialValue != 2 {
		t.Fatalf("semantic pair lost: %+v", got.Voxels)
	}
	for _, bad := range []string{"owner", "coordinate", "grid", "huge-grid", "resolution", "hash", "count", "body", "aux-owner", "aux-grid"} {
		t.Run(bad, func(t *testing.T) {
			d, p := i09EntryFixture(t)
			dir := filepath.Dir(p)
			switch bad {
			case "body":
				path := filepath.Join(dir, "full.gkchunk")
				raw, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				raw[len(raw)-1] ^= 1
				if e = os.WriteFile(path, raw, 0600); e != nil {
					t.Fatal(e)
				}
			case "aux-owner", "aux-grid":
				i09EntryHeader(t, filepath.Join(dir, "full.gkaux"), true, func(m map[string]any) {
					if bad == "aux-owner" {
						m["world_id"] = "wrong"
					} else {
						m["chunk_size"] = 8
					}
				})
			default:
				i09EntryHeader(t, filepath.Join(dir, "full.gkchunk"), false, func(m map[string]any) {
					switch bad {
					case "owner":
						m["world_id"] = "wrong"
					case "coordinate":
						m["coord"] = map[string]any{"x": 1}
					case "grid":
						m["chunk_size"] = 8
					case "huge-grid":
						m["chunk_size"] = 1 << 30
					case "resolution":
						m["voxel_resolution"] = .2
					case "hash":
						m["payload_hash"] = strings.Repeat("f", 64)
					case "count":
						m["non_empty_voxel_count"] = 2
					}
				})
			}
			if got, e := LoadImportedWorldChunkEntry(d, p, 0); e == nil || got != nil {
				t.Fatalf("unqualified %s admitted: %v", bad, e)
			}
		})
	}
}
func TestI09QualifiedFullEntryRejectsUnboundedOrUnqualifiedReferences(t *testing.T) {
	for _, bad := range []string{"nil", "index", "version", "empty-path", "empty-world", "huge-grid", "negative-grid", "bad-resolution", "missing-hash", "missing-size", "negative-count"} {
		t.Run(bad, func(t *testing.T) {
			d, p := i09EntryFixture(t)
			index := uint32(0)
			switch bad {
			case "nil":
				d = nil
			case "index":
				index = ^uint32(0)
			case "version":
				d.SchemaVersion = 99
			case "empty-path":
				p = ""
			case "empty-world":
				d.WorldID = ""
			case "huge-grid":
				d.ChunkSize = 1 << 30
			case "negative-grid":
				d.ChunkSize = -1
			case "bad-resolution":
				d.VoxelResolution = 0
			case "missing-hash":
				d.Entries[0].PayloadHash = ""
			case "missing-size":
				d.Entries[0].PayloadSizeBytes = 0
			case "negative-count":
				d.Entries[0].NonEmptyVoxelCount = -1
			}
			if got, e := LoadImportedWorldChunkEntry(d, p, index); e == nil || got != nil {
				t.Fatalf("invalid reference %s accepted", bad)
			}
		})
	}
}
