package gekko

import (
	"github.com/gekko3d/gekko/content"
	"path/filepath"
	"strings"
	"testing"
)

func TestI06RuntimeImportedWorldV3Gate(t *testing.T) {
	d := &content.ImportedWorldDef{WorldID: "tooling-v3", SchemaVersion: content.ImportedWorldPageSchemaVersion, SourceHash: strings.Repeat("a", 64), ChunkSize: 4, VoxelResolution: 1, Pages: []content.StreamPageDef{{Level: content.StreamPageLevelRoot, BoundsMax: [3]float32{4, 4, 4}, Payload: content.StreamPagePayloadDef{Kind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1, Path: "root.gkchunk", ChunkSize: 4, VoxelResolution: 1, PayloadHash: strings.Repeat("b", 64), PayloadSizeBytes: 24}}}, RootPageIndices: []uint32{0}}
	p := filepath.Join(t.TempDir(), "world.gkworld")
	if e := content.SaveImportedWorld(p, d); e != nil {
		t.Fatal(e)
	}
	if _, e := content.LoadImportedWorld(p); e != nil {
		t.Fatalf("tooling v3 rejected: %v", e)
	}
	if got, e := NewRuntimeContentLoader().LoadImportedWorld(p); e == nil || got != nil {
		t.Fatal("live runtime admitted v3 before page handoff/registration")
	}
}
