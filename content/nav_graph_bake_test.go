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
	tilePath := ResolveDocumentPath(bake.Manifest.SourceTiles[0].TilePath, manifestPath)
	tileBytes, err := os.ReadFile(tilePath)
	if err != nil {
		t.Fatal(err)
	}
	tileBytes[len(tileBytes)-1] ^= 0xff
	if err := os.WriteFile(tilePath, tileBytes, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNavGraphBake(manifestPath); err == nil {
		t.Fatal("navigation bake accepted a sidecar that did not match its manifest hash")
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

func TestBakeLevelNavGraphIncludesStationaryMovingBrushSupport(t *testing.T) {
	const chunkSize = 8
	voxels := make([]ImportedWorldVoxelDef, 0, 35)
	for x := 0; x < chunkSize; x++ {
		if x == 3 {
			continue
		}
		for z := 1; z < 6; z++ {
			voxels = append(voxels, ImportedWorldVoxelDef{X: x, Z: z, Value: 1})
		}
	}
	coord := TerrainChunkCoordDef{}
	chunk := ImportedWorldChunkDef{
		WorldID: "stationary-support-test", SchemaVersion: CurrentImportedWorldChunkSchemaVersion,
		Coord: coord, ChunkSize: chunkSize, VoxelResolution: 1, Voxels: voxels, NonEmptyVoxelCount: len(voxels),
	}
	world := &ImportedWorldDef{
		WorldID: "stationary-support-test", SchemaVersion: CurrentImportedWorldSchemaVersion, Kind: ImportedWorldKindVoxelWorld,
		ChunkSize: chunkSize, VoxelResolution: 1,
		Entries: []ImportedWorldChunkEntryDef{{Coord: coord, ChunkPath: "chunks/0_0_0.gkchunk", NonEmptyVoxelCount: len(voxels)}},
	}
	EnsureImportedWorldSectors(world)
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.4, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	withoutSupport, err := BakeNavGraphWorld(world, []ImportedWorldChunkDef{chunk}, []NavAgentProfileDef{profile})
	if err != nil {
		t.Fatalf("BakeNavGraphWorld failed: %v", err)
	}
	start, goal := Vec3{1.5, 1, 3.5}, Vec3{5.5, 1, 3.5}
	missing, err := FindNavGraphRoute(withoutSupport.SourceTiles, withoutSupport.GraphTiles, chunkSize, 1, start, goal)
	if err != nil {
		t.Fatalf("route without stationary support failed: %v", err)
	}
	if missing.Found {
		t.Fatal("route unexpectedly crossed missing stationary support")
	}

	root := t.TempDir()
	chunkPath := filepath.Join(root, "chunks", "0_0_0.gkchunk")
	worldPath := filepath.Join(root, "world.gkworld")
	assetPath := filepath.Join(root, "support.gkasset")
	levelPath := filepath.Join(root, "level.gklevel")
	if err := SaveImportedWorldChunk(chunkPath, &chunk); err != nil {
		t.Fatal(err)
	}
	if err := SaveImportedWorld(worldPath, world); err != nil {
		t.Fatal(err)
	}
	asset := NewAssetDef("stationary support")
	asset.Materials = []AssetMaterialDef{{ID: "solid", Name: "solid"}}
	asset.Parts = []AssetPartDef{{
		ID: "support", Name: "support", VoxelResolution: 1, ModelScale: 1,
		Transform: AssetTransformDef{Rotation: Quat{0, 0, 0, 1}, Scale: Vec3{1, 1, 1}},
		Source: AssetSourceDef{Kind: AssetSourceKindVoxelShape, VoxelShape: &AssetVoxelShapeDef{
			Palette: []AssetVoxelPaletteEntryDef{{Value: 1, MaterialID: "solid"}},
			Voxels: []VoxelObjectVoxelDef{
				{Z: 0, Value: 1}, {Z: 1, Value: 1}, {Z: 2, Value: 1}, {Z: 3, Value: 1}, {Z: 4, Value: 1},
			},
		}},
	}}
	if err := SaveAsset(assetPath, asset); err != nil {
		t.Fatal(err)
	}
	level := NewLevelDef("stationary support")
	level.ChunkSize = chunkSize
	level.VoxelResolution = 1
	level.BaseWorld = &LevelBaseWorldDef{Kind: ImportedWorldKindVoxelWorld, ManifestPath: "world.gkworld", CollisionEnabled: true}
	level.Navigation = &LevelNavigationDef{ManifestPath: "missing-output.gknav"}
	level.MovingBrushes = []LevelMovingBrushDef{{
		ID: "support", Name: "support", MotionKind: "static", AssetPath: "support.gkasset",
		BoundsCenter: Vec3{3.5, 0.5, 3.5}, BoundsHalfExtents: Vec3{0.5, 0.5, 2.5}, VisualOrigin: Vec3{3, 0, 1},
	}}
	if err := SaveLevel(levelPath, level); err != nil {
		t.Fatal(err)
	}

	withSupport, err := BakeLevelNavGraph(levelPath, []NavAgentProfileDef{profile})
	if err != nil {
		t.Fatalf("BakeLevelNavGraph failed: %v", err)
	}
	route, err := FindNavGraphRoute(withSupport.SourceTiles, withSupport.GraphTiles, chunkSize, 1, start, goal)
	if err != nil || !route.Found {
		t.Fatalf("route across stationary support failed: route=%+v err=%v", route, err)
	}
}
