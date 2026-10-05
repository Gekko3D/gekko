package derived

import (
	"github.com/gekko3d/gekko/content"
	"path/filepath"
	"testing"
)

func TestI06PackingValidMaximumPayloadSide(t *testing.T) {
	coord := content.TerrainChunkCoordDef{}
	source := &content.ImportedWorldDef{WorldID: "maximum-side", SchemaVersion: 2, ChunkSize: 256, VoxelResolution: 1, Entries: []content.ImportedWorldChunkEntryDef{{Coord: coord, NonEmptyVoxelCount: 1}}}
	chunk := &content.ImportedWorldChunkDef{WorldID: source.WorldID, SchemaVersion: 1, Coord: coord, ChunkSize: 256, VoxelResolution: 1, NonEmptyVoxelCount: 1, Voxels: []content.ImportedWorldVoxelDef{{Value: 1}}}
	bake, err := BuildImportedWorldPageBake(source, map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{coord: chunk}, ImportedWorldPageBakeOptions{RegionalSpan: 256, MacroSpan: 512, RootSpan: 1024, RegionalResolution: 1, MacroResolution: 2, RootResolution: 4})
	if err != nil {
		t.Fatalf("valid exactly-at-limit grid rejected: %v", err)
	}
	for _, page := range bake.Manifest.Pages {
		if page.Payload.ChunkSize > 256 {
			t.Fatalf("payload exceeds supported side: %d", page.Payload.ChunkSize)
		}
		for axis := 0; axis < 3; axis++ {
			if page.BoundsMin[axis] > 0 || page.BoundsMax[axis] < 256 {
				t.Fatalf("lost conservative full-child coverage: %+v", page)
			}
		}
	}
	path := filepath.Join(t.TempDir(), "world.gkworld")
	if err = SaveImportedWorldPageBake(path, bake); err != nil {
		t.Fatal(err)
	}
	loaded, err := content.LoadImportedWorld(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != 3 {
		t.Fatal("page bake did not publish v3")
	}
	for i, page := range loaded.Pages {
		payload, err := content.LoadImportedWorldPagePayload(loaded, path, uint32(i))
		if err != nil {
			t.Fatal(err)
		}
		if payload.ChunkSize > 256 || page.Payload.ChunkSize != payload.ChunkSize {
			t.Fatal("published grid exceeds cap or differs from reference")
		}
	}
}
