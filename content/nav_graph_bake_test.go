package content

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBakeNavGraphWorld(t *testing.T) {
	const chunkSize = 4
	coords := []TerrainChunkCoordDef{{}, {X: 1}}
	entries := make([]ImportedWorldChunkEntryDef, 0, len(coords))
	chunks := make([]ImportedWorldChunkDef, 0, len(coords))
	for _, coord := range coords {
		voxels := make([]ImportedWorldVoxelDef, 0, chunkSize*chunkSize)
		for x := 0; x < chunkSize; x++ {
			for z := 0; z < chunkSize; z++ {
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Z: z, Value: 1})
			}
		}
		entries = append(entries, ImportedWorldChunkEntryDef{Coord: coord, ChunkPath: navGraphCoordFilename(coord) + ".gkchunk", NonEmptyVoxelCount: len(voxels)})
		chunks = append(chunks, ImportedWorldChunkDef{WorldID: "bake-test", Coord: coord, ChunkSize: chunkSize, VoxelResolution: 1, Voxels: voxels})
	}
	world := &ImportedWorldDef{WorldID: "bake-test", SchemaVersion: CurrentImportedWorldSchemaVersion, Kind: ImportedWorldKindVoxelWorld, ChunkSize: chunkSize, VoxelResolution: 1, Entries: entries}
	EnsureImportedWorldSectors(world)
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.4, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}

	bake, err := BakeNavGraphWorld(world, chunks, []NavAgentProfileDef{profile})
	if err != nil {
		t.Fatalf("BakeNavGraphWorld failed: %v", err)
	}
	repeated, err := BakeNavGraphWorld(world, []ImportedWorldChunkDef{chunks[1], chunks[0]}, []NavAgentProfileDef{profile})
	if err != nil {
		t.Fatalf("repeated BakeNavGraphWorld failed: %v", err)
	}
	if !reflect.DeepEqual(bake, repeated) {
		t.Fatal("full-world navigation bake is not deterministic")
	}
	route, err := FindNavGraphRoute(bake.SourceTiles, bake.GraphTiles, chunkSize, 1, Vec3{0.5, 1, 0.5}, Vec3{7.5, 1, 0.5})
	if err != nil || !route.Found {
		t.Fatalf("cross-tile route failed: route=%+v err=%v", route, err)
	}

	manifestPath := filepath.Join(t.TempDir(), "bake"+NavGraphManifestExtension)
	if err := SaveNavGraphBake(manifestPath, &bake); err != nil {
		t.Fatalf("SaveNavGraphBake failed: %v", err)
	}
	firstManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveNavGraphBake(manifestPath, &bake); err != nil {
		t.Fatalf("repeated SaveNavGraphBake failed: %v", err)
	}
	secondManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstManifest, secondManifest) {
		t.Fatal("navigation manifest save is not deterministic")
	}
	loaded, err := LoadNavGraphBake(manifestPath)
	if err != nil {
		t.Fatalf("LoadNavGraphBake failed: %v", err)
	}
	if !reflect.DeepEqual(bake.Manifest, loaded.Manifest) || !reflect.DeepEqual(bake.SourceTiles, loaded.SourceTiles) || !reflect.DeepEqual(bake.GraphTiles, loaded.GraphTiles) {
		t.Fatal("saved navigation bake did not preserve topology")
	}

	bad := bake
	bad.Manifest.NavID = ""
	badPath := filepath.Join(t.TempDir(), "invalid", "bad"+NavGraphManifestExtension)
	if err := SaveNavGraphBake(badPath, &bad); err == nil {
		t.Fatal("invalid navigation bake was saved")
	}
	if _, err := os.Stat(filepath.Dir(badPath)); !os.IsNotExist(err) {
		t.Fatalf("invalid navigation bake created partial output: %v", err)
	}
}
