package gekko

import (
	"github.com/gekko3d/gekko/content"
	"path/filepath"
	"strings"
	"testing"
)

func TestW4cRuntimeRejectsHeightTileManifestUntilCollisionReady(t *testing.T) {
	p := filepath.Join(t.TempDir(), "height.gkterrainmanifest")
	m := &content.TerrainChunkManifestDef{SchemaVersion: content.TerrainHeightTileManifestSchemaVersion, TerrainID: "terrain", SourceHash: "source", ChunkSize: 1, VoxelResolution: 2, Entries: []content.TerrainChunkEntryDef{{TerrainID: "terrain", SourceHash: "source", ChunkSize: 1, VoxelResolution: 2, ChunkPath: "tile.bin", PayloadKind: content.TerrainHeightTilePayloadKind, PayloadHash: strings.Repeat("0", 64), PayloadSizeBytes: 2}}}
	if e := content.SaveTerrainChunkManifest(p, m); e != nil {
		t.Fatal(e)
	}
	if _, e := content.LoadTerrainChunkManifest(p); e != nil {
		t.Fatalf("tooling must support valid v3: %v", e)
	}
	loader := NewRuntimeContentLoader()
	if _, e := loader.LoadTerrainChunkManifest(p); e == nil {
		t.Fatal("live runtime accepted height tiles without height collision support")
	}
	legacyPath := filepath.Join(t.TempDir(), "legacy.gkterrainmanifest")
	legacy := &content.TerrainChunkManifestDef{SchemaVersion: 2, TerrainID: "legacy", ChunkSize: 32, VoxelResolution: 1}
	if e := content.SaveTerrainChunkManifest(legacyPath, legacy); e != nil {
		t.Fatal(e)
	}
	if got, e := loader.LoadTerrainChunkManifest(legacyPath); e != nil || got.SchemaVersion != 2 {
		t.Fatalf("legacy runtime regression: %v", e)
	}
}
