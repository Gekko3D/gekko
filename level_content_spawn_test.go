package gekko

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func TestLoadAndSpawnAuthoredLevelSpawnsPlacementWithLevelAndAssetMetadata(t *testing.T) {
	root := t.TempDir()
	assetPath := filepath.Join(root, "assets", "ship.gkasset")
	levelPath := filepath.Join(root, "levels", "demo.gklevel")

	writeProceduralAssetForLevelTest(t, assetPath, "ship-asset")

	level := content.NewLevelDef("demo")
	level.Environment = &content.LevelEnvironmentDef{Preset: "orbit"}
	level.Placements = []content.LevelPlacementDef{{
		ID:        "placement-1",
		AssetPath: filepath.Join("..", "assets", "ship.gkasset"),
		Transform: content.LevelTransformDef{
			Position: content.Vec3{4, 5, 6},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app := NewApp()
	cmd := app.Commands()
	result, err := LoadAndSpawnAuthoredLevel(levelPath, cmd, newSpawnTestAssetServer(), NewRuntimeContentLoader(), AuthoredLevelSpawnOptions{})
	if err != nil {
		t.Fatalf("LoadAndSpawnAuthoredLevel failed: %v", err)
	}
	app.FlushCommands()

	if !IsAuthoredLevelRootEntity(cmd, result.RootEntity) {
		t.Fatal("expected authored level root metadata on root entity")
	}

	rootEntity := result.PlacementRootEntities["placement-1"]
	if rootEntity == 0 {
		t.Fatal("expected placement root entity")
	}
	if !IsAuthoredAssetRootEntity(cmd, rootEntity) {
		t.Fatal("expected placement root to retain authored asset root metadata")
	}

	ref, ok := AuthoredLevelPlacementRefForEntity(cmd, rootEntity)
	if !ok {
		t.Fatal("expected level placement ref component on placement root")
	}
	if ref.LevelID != level.ID || ref.PlacementID != "placement-1" || ref.AssetPath != filepath.Clean(filepath.Join("..", "assets", "ship.gkasset")) || ref.VolumeID != "" {
		t.Fatalf("unexpected placement ref %+v", ref)
	}

	var foundAssetItem bool
	MakeQuery1[AuthoredAssetRefComponent](cmd).Map(func(eid EntityId, ref *AuthoredAssetRefComponent) bool {
		if ref.AssetID == "ship-asset" && ref.ItemID == "ship-asset-part" {
			foundAssetItem = true
			return false
		}
		return true
	})
	if !foundAssetItem {
		t.Fatal("expected spawned level placement to retain authored asset item metadata")
	}
}

func TestLoadAndSpawnAuthoredLevelAppliesDaylightEnvironment(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	level := content.NewLevelDef("daylight")
	level.Environment = &content.LevelEnvironmentDef{Preset: "daylight"}

	if _, err := SpawnAuthoredLevel(cmd, nil, NewRuntimeContentLoader(), level, AuthoredLevelSpawnOptions{}); err != nil {
		t.Fatalf("SpawnAuthoredLevel failed: %v", err)
	}
	app.FlushCommands()

	ambientCount := 0
	directionalCount := 0
	MakeQuery1[LightComponent](cmd).Map(func(_ EntityId, light *LightComponent) bool {
		switch light.Type {
		case LightTypeAmbient:
			ambientCount++
		case LightTypeDirectional:
			directionalCount++
		}
		return true
	})
	if ambientCount != 1 {
		t.Fatalf("expected one ambient light, got %d", ambientCount)
	}
	if directionalCount != 1 {
		t.Fatalf("expected one directional light, got %d", directionalCount)
	}

	var gradientCount, noiseCount int
	MakeQuery1[SkyboxLayerComponent](cmd).Map(func(_ EntityId, layer *SkyboxLayerComponent) bool {
		if layer.LayerType == SkyboxLayerGradient {
			gradientCount++
		} else {
			noiseCount++
		}
		return true
	})
	if gradientCount != 1 || noiseCount != 2 {
		t.Fatalf("expected daylight skybox to spawn 1 gradient and 2 noise layers, got gradient=%d noise=%d", gradientCount, noiseCount)
	}
}

func TestLoadAndSpawnAuthoredLevelExpandsPlacementVolumesDeterministically(t *testing.T) {
	root := t.TempDir()
	assetPath := filepath.Join(root, "assets", "asteroid.gkasset")
	levelPath := filepath.Join(root, "levels", "volumes.gklevel")

	writeProceduralAssetForLevelTest(t, assetPath, "asteroid-asset")

	level := content.NewLevelDef("volumes")
	level.PlacementVolumes = []content.PlacementVolumeDef{{
		ID:        "volume-a",
		Kind:      content.PlacementVolumeKindSphere,
		AssetPath: filepath.Join("..", "assets", "asteroid.gkasset"),
		Transform: content.LevelTransformDef{
			Position: content.Vec3{10, 0, 0},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Radius:     12,
		RandomSeed: 77,
		Rule: content.PlacementVolumeRuleDef{
			Mode:  content.PlacementVolumeRuleModeCount,
			Count: 4,
		},
	}}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	runSpawn := func() (AuthoredLevelSpawnResult, *Commands) {
		app := NewApp()
		cmd := app.Commands()
		result, err := LoadAndSpawnAuthoredLevel(levelPath, cmd, newSpawnTestAssetServer(), NewRuntimeContentLoader(), AuthoredLevelSpawnOptions{
			MaxVolumeInstances: 32,
		})
		if err != nil {
			t.Fatalf("LoadAndSpawnAuthoredLevel failed: %v", err)
		}
		app.FlushCommands()
		return result, cmd
	}

	first, firstCmd := runSpawn()
	second, _ := runSpawn()

	if !reflect.DeepEqual(first.ExpandedVolumeInstances, second.ExpandedVolumeInstances) {
		t.Fatalf("expected deterministic volume expansion, first=%+v second=%+v", first.ExpandedVolumeInstances, second.ExpandedVolumeInstances)
	}
	for i := range first.ExpandedVolumeInstances {
		placementID := "volume-a:" + strconv.Itoa(i)
		rootEntity := first.PlacementRootEntities[placementID]
		if rootEntity == 0 {
			t.Fatalf("expected placement root for %s", placementID)
		}
		ref, ok := AuthoredLevelPlacementRefForEntity(firstCmd, rootEntity)
		if !ok {
			t.Fatalf("expected placement ref metadata for %s", placementID)
		}
		if ref.VolumeID != "volume-a" || ref.PlacementID != placementID {
			t.Fatalf("unexpected placement ref for %s: %+v", placementID, ref)
		}
	}
}

func TestLoadAndSpawnAuthoredLevelSpawnsTerrainChunksWithMetadata(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "terrain_demo.gklevel")
	terrainPath := filepath.Join(root, "terrain", "terrain.gkterrain")
	manifestPath := filepath.Join(root, "terrain", "terrain.gkterrainmanifest")
	nonEmptyChunkPath := filepath.Join(root, "terrain", "terrain_chunks", "1_0_-1.gkchunk")
	emptyChunkPath := filepath.Join(root, "terrain", "terrain_chunks", "0_0_0.gkchunk")

	terrain := content.NewTerrainSourceDef("terrain")
	terrain.SampleWidth = 1
	terrain.SampleHeight = 1
	terrain.HeightSamples = []uint16{65535}
	terrain.WorldSize = content.Vec2{16, 16}
	terrain.HeightScale = 4
	terrain.VoxelResolution = 2
	terrain.ChunkSize = 8

	if err := os.MkdirAll(filepath.Dir(terrainPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveTerrainSource(terrainPath, terrain); err != nil {
		t.Fatalf("SaveTerrainSource failed: %v", err)
	}

	nonEmptyChunk := &content.TerrainChunkDef{
		TerrainID:          terrain.ID,
		Coord:              content.TerrainChunkCoordDef{X: 1, Y: 0, Z: -1},
		ChunkSize:          8,
		VoxelResolution:    2,
		SolidValue:         3,
		Columns:            []content.TerrainChunkColumnDef{{X: 2, Z: 3, FilledVoxels: 4}},
		NonEmptyVoxelCount: 4,
	}
	if err := content.SaveTerrainChunk(nonEmptyChunkPath, nonEmptyChunk); err != nil {
		t.Fatalf("SaveTerrainChunk failed: %v", err)
	}

	emptyChunk := &content.TerrainChunkDef{
		TerrainID:          terrain.ID,
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          8,
		VoxelResolution:    2,
		SolidValue:         3,
		NonEmptyVoxelCount: 0,
	}
	if err := content.SaveTerrainChunk(emptyChunkPath, emptyChunk); err != nil {
		t.Fatalf("SaveTerrainChunk failed: %v", err)
	}

	manifest := &content.TerrainChunkManifestDef{
		TerrainID:       terrain.ID,
		ChunkSize:       8,
		VoxelResolution: 2,
		Entries: []content.TerrainChunkEntryDef{
			{
				Coord:              nonEmptyChunk.Coord,
				ChunkSize:          nonEmptyChunk.ChunkSize,
				VoxelResolution:    nonEmptyChunk.VoxelResolution,
				TerrainID:          terrain.ID,
				ChunkPath:          content.AuthorDocumentPath(nonEmptyChunkPath, manifestPath),
				NonEmptyVoxelCount: nonEmptyChunk.NonEmptyVoxelCount,
			},
			{
				Coord:              emptyChunk.Coord,
				ChunkSize:          emptyChunk.ChunkSize,
				VoxelResolution:    emptyChunk.VoxelResolution,
				TerrainID:          terrain.ID,
				ChunkPath:          content.AuthorDocumentPath(emptyChunkPath, manifestPath),
				NonEmptyVoxelCount: emptyChunk.NonEmptyVoxelCount,
			},
		},
	}
	if err := content.SaveTerrainChunkManifest(manifestPath, manifest); err != nil {
		t.Fatalf("SaveTerrainChunkManifest failed: %v", err)
	}

	level := content.NewLevelDef("terrain-demo")
	level.ChunkSize = 8
	level.VoxelResolution = 2
	level.Terrain = &content.LevelTerrainDef{
		Kind:         content.TerrainKindHeightfield,
		SourcePath:   content.AuthorDocumentPath(terrainPath, levelPath),
		ManifestPath: content.AuthorDocumentPath(manifestPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app := NewApp()
	cmd := app.Commands()
	result, err := LoadAndSpawnAuthoredLevel(levelPath, cmd, newSpawnTestAssetServer(), NewRuntimeContentLoader(), AuthoredLevelSpawnOptions{
		TerrainGroupID: 77,
	})
	if err != nil {
		t.Fatalf("LoadAndSpawnAuthoredLevel failed: %v", err)
	}
	app.FlushCommands()

	if len(result.TerrainChunkEntities) != 1 {
		t.Fatalf("expected only non-empty terrain chunk to spawn, got %+v", result.TerrainChunkEntities)
	}

	entity := result.TerrainChunkEntities[content.TerrainChunkKey(nonEmptyChunk.Coord)]
	if entity == 0 {
		t.Fatal("expected spawned terrain chunk entity")
	}

	ref, ok := AuthoredTerrainChunkRefForEntity(cmd, entity)
	if !ok {
		t.Fatal("expected terrain chunk metadata")
	}
	if ref.LevelID != level.ID || ref.TerrainID != terrain.ID || ref.ChunkCoord != [3]int{1, 0, -1} {
		t.Fatalf("unexpected terrain chunk ref %+v", ref)
	}

	transform := mustWorldTransformForSpawnTest(t, cmd, entity)
	wantPos := mgl32.Vec3{float32(nonEmptyChunk.ChunkSize) * nonEmptyChunk.VoxelResolution, 0, -float32(nonEmptyChunk.ChunkSize) * nonEmptyChunk.VoxelResolution}
	if transform.Position.Sub(wantPos).Len() > 1e-5 {
		t.Fatalf("expected terrain position %v, got %v", wantPos, transform.Position)
	}
	if got := transform.Scale; got.Sub(mgl32.Vec3{20, 20, 20}).Len() > 1e-5 {
		t.Fatalf("expected terrain scale [20 20 20], got %v", got)
	}

	vmc := mustVoxelModelComponentForLevelTest(t, cmd, entity)
	if !vmc.IsTerrainChunk || vmc.TerrainGroupID != 77 || vmc.TerrainChunkCoord != [3]int{1, 0, -1} || vmc.TerrainChunkSize != 8 {
		t.Fatalf("unexpected terrain voxel model metadata %+v", vmc)
	}
	if vmc.PivotMode != PivotModeCorner {
		t.Fatalf("expected terrain pivot mode corner, got %v", vmc.PivotMode)
	}
	if vmc.CustomMap == nil || vmc.CustomMap.GetVoxelCount() != 4 {
		t.Fatalf("expected terrain custom map with 4 voxels, got %+v", vmc.CustomMap)
	}
}

func writeProceduralAssetForLevelTest(t *testing.T, path string, assetID string) {
	t.Helper()
	def := content.NewAssetDef(assetID)
	def.ID = assetID
	def.Parts = []content.AssetPartDef{{
		ID:     assetID + "-part",
		Name:   "root",
		Source: testProceduralPartSource(),
		Transform: content.AssetTransformDef{
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveAsset(path, def); err != nil {
		t.Fatalf("SaveAsset failed: %v", err)
	}
}

func mustVoxelModelComponentForLevelTest(t *testing.T, cmd *Commands, eid EntityId) VoxelModelComponent {
	t.Helper()
	for _, comp := range cmd.GetAllComponents(eid) {
		if model, ok := comp.(VoxelModelComponent); ok {
			return model
		}
		if model, ok := comp.(*VoxelModelComponent); ok {
			return *model
		}
	}
	t.Fatalf("missing VoxelModelComponent for entity %d", eid)
	return VoxelModelComponent{}
}
