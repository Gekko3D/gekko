package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestI06ReviewEmptyFullEntryStillRequiresPayloadIdentity(t *testing.T) {
	d := i06World()
	d.Entries[0].NonEmptyVoxelCount = 0
	d.Entries[0].PayloadHash = ""
	d.Entries[0].PayloadSizeBytes = 0
	if index, err := ValidateImportedWorldV3(d); err == nil || index != nil {
		t.Fatal("empty full payload lost strict identity")
	}
}

func TestI06ReviewV3WireRejectsLegacyLODKeyEvenEmpty(t *testing.T) {
	d := i06World()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	var sectors []map[string]json.RawMessage
	if err = json.Unmarshal(fields["sectors"], &sectors); err != nil {
		t.Fatal(err)
	}
	sectors[0]["lods"] = json.RawMessage(`[]`)
	fields["sectors"], err = json.Marshal(sectors)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "bad.gkworld")
	if err = os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadImportedWorld(p); err == nil || got != nil {
		t.Fatal("v3 silently accepted legacy sector LOD meaning")
	}
}

func TestI06ReviewPublicFileValidationQualifiesAuxOwnerAndGrid(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "world.gkworld")
	coord := TerrainChunkCoordDef{X: 1}
	chunk := &ImportedWorldChunkDef{WorldID: "v3-world", SchemaVersion: 1, Coord: coord, ChunkSize: 4, VoxelResolution: 1, NonEmptyVoxelCount: 1, Voxels: []ImportedWorldVoxelDef{{Value: 1}}}
	if _, err := SaveImportedWorldChunkWithOptionsResult(filepath.Join(dir, "full.gkchunk"), chunk, ImportedWorldChunkSaveOptions{PayloadKind: ImportedWorldChunkPayloadDenseRLEBinaryV1}); err != nil {
		t.Fatal(err)
	}
	aux := &ImportedWorldChunkAuxDef{WorldID: chunk.WorldID, SchemaVersion: 1, Coord: coord, ChunkSize: 4, VoxelResolution: 1, NormalBakeVersion: ImportedWorldNormalBakeVersion, SourcePayloadHash: chunk.PayloadHash, SourcePayloadSizeBytes: chunk.PayloadSizeBytes}
	auxPath := filepath.Join(dir, "full.gkchunkaux")
	if err := SaveImportedWorldChunkAux(auxPath, aux); err != nil {
		t.Fatal(err)
	}
	ref := ImportedWorldChunkAuxRef("full.gkchunkaux", aux)
	d := i06World()
	d.Pages = d.Pages[:1]
	d.RootPageIndices = []uint32{0}
	d.IndexedSectors = nil
	d.Entries[0] = ImportedWorldChunkEntryDef{Coord: coord, ChunkPath: "full.gkchunk", NonEmptyVoxelCount: 1, PayloadKind: chunk.PayloadKind, PayloadHash: chunk.PayloadHash, PayloadSizeBytes: chunk.PayloadSizeBytes, OccupiedSectorCount: 1, OccupiedBrickCount: 1, Aux: ref}
	d.Pages[0].BoundsMin = [3]float32{4, 0, 0}
	d.Pages[0].BoundsMax = [3]float32{8, 4, 4}
	d.Pages[0].Payload = StreamPagePayloadDef{Kind: chunk.PayloadKind, Path: "full.gkchunk", WorldOrigin: [3]float32{4, 0, 0}, ChunkSize: 4, VoxelResolution: 1, PayloadHash: chunk.PayloadHash, PayloadSizeBytes: chunk.PayloadSizeBytes, OccupiedSectorCount: 1, OccupiedBrickCount: 1, Aux: ref}
	opts := ImportedWorldValidationOptions{DocumentPath: manifestPath}
	if result := ValidateImportedWorld(d, opts); result.HasErrors() {
		t.Fatalf("valid aux control failed: %+v", result)
	}
	for name, mutate := range map[string]func(*ImportedWorldChunkAuxDef){"world": func(a *ImportedWorldChunkAuxDef) { a.WorldID = "foreign" }, "coordinate": func(a *ImportedWorldChunkAuxDef) { a.Coord.X = 99 }, "size": func(a *ImportedWorldChunkAuxDef) { a.ChunkSize = 8 }, "resolution": func(a *ImportedWorldChunkAuxDef) { a.VoxelResolution = 2 }} {
		t.Run(name, func(t *testing.T) {
			changed := *aux
			mutate(&changed)
			if err := SaveImportedWorldChunkAux(auxPath, &changed); err != nil {
				t.Fatal(err)
			}
			if changed.PayloadHash != aux.PayloadHash || changed.SourcePayloadHash != aux.SourcePayloadHash {
				t.Fatal("control changed protected body/source hashes")
			}
			if result := ValidateImportedWorld(d, opts); !result.HasErrors() {
				t.Fatal("same-body aux header accepted wrong owner/grid")
			}
			if err := SaveImportedWorldChunkAux(auxPath, aux); err != nil {
				t.Fatal(err)
			}
		})
	}
}
