package gekko

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	contentderived "github.com/gekko3d/gekko/content/derived"
)

func TestI06VoxPageBakeAdapterRoundTripAndLegacyDefault(t *testing.T) {
	source := &VoxFile{Models: []VoxModel{{SizeX: 4, SizeY: 4, SizeZ: 4, Voxels: []Voxel{{X: 0, Y: 0, Z: 0, ColorIndex: 1}, {X: 3, Y: 3, Z: 3, ColorIndex: 2}}}}}
	before, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	cfg := ImportedWorldBakeConfig{WorldID: "adapter-vox", ChunkSize: 4, VoxelResolution: 1, PageBakeOptions: &contentderived.ImportedWorldPageBakeOptions{}}
	bake, err := BakeImportedWorldFromVox(source, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if bake.PageBake == nil || bake.Manifest == nil || bake.Manifest.SchemaVersion != 3 {
		t.Fatalf("missing opt-in page draft: %+v", bake)
	}
	path := filepath.Join(t.TempDir(), "world.gkworld")
	if err = SaveImportedWorldBake(path, bake); err != nil {
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
		payload := page.Payload
		chunk, e := content.LoadImportedWorldChunk(content.ResolveDocumentPath(payload.Path, path))
		if e != nil {
			t.Fatal(e)
		}
		if payload.PayloadHash == "" || payload.PayloadSizeBytes <= 0 || payload.PayloadHash != chunk.PayloadHash || payload.PayloadSizeBytes != chunk.PayloadSizeBytes || payload.Kind != chunk.PayloadKind || payload.ChunkSize != chunk.ChunkSize || payload.VoxelResolution != chunk.VoxelResolution {
			t.Fatalf("page payload identity mismatch: %+v", payload)
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
			t.Fatalf("full payload identity mismatch: %+v", entry)
		}
	}
	after, err := json.Marshal(source)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("adapter mutated VOX input")
	}
	cfg.PageBakeOptions = nil
	legacy, err := BakeImportedWorldFromVox(source, cfg)
	if err != nil || legacy.PageBake != nil || legacy.Manifest.SchemaVersion != 2 {
		t.Fatalf("nil option changed legacy path: %v", err)
	}
	legacyPath := filepath.Join(t.TempDir(), "legacy.gkworld")
	if err := SaveImportedWorldBake(legacyPath, legacy); err != nil {
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
