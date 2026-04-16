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

func TestLoadAndSpawnAuthoredLevelCollapsedAssetKeepsPlacementRefWithoutItemRefs(t *testing.T) {
	root := t.TempDir()
	assetPath := filepath.Join(root, "assets", "ship_collapsed.gkasset")
	levelPath := filepath.Join(root, "levels", "demo_collapsed.gklevel")

	writeCollapsedProceduralAssetForLevelTest(t, assetPath, "ship-collapsed")

	level := content.NewLevelDef("demo-collapsed")
	level.Placements = []content.LevelPlacementDef{{
		ID:        "placement-1",
		AssetPath: filepath.Join("..", "assets", "ship_collapsed.gkasset"),
		Transform: content.LevelTransformDef{
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

	rootEntity := result.PlacementRootEntities["placement-1"]
	if rootEntity == 0 {
		t.Fatal("expected placement root entity")
	}
	if _, ok := AuthoredLevelPlacementRefForEntity(cmd, rootEntity); !ok {
		t.Fatal("expected placement ref on collapsed placement root")
	}

	var itemRefs int
	MakeQuery1[AuthoredLevelItemRefComponent](cmd).Map(func(_ EntityId, ref *AuthoredLevelItemRefComponent) bool {
		if ref.PlacementID == "placement-1" {
			itemRefs++
		}
		return true
	})
	if itemRefs != 0 {
		t.Fatalf("expected collapsed placement to skip per-item refs, got %d", itemRefs)
	}
}

func TestLoadAndSpawnAuthoredLevelPlacementVolumeOverridesShadowSettings(t *testing.T) {
	root := t.TempDir()
	assetPath := filepath.Join(root, "assets", "asteroid.gkasset")
	levelPath := filepath.Join(root, "levels", "shadow_volume.gklevel")

	writeProceduralAssetForLevelTest(t, assetPath, "asteroid-asset")

	castsShadows := false
	level := content.NewLevelDef("shadow-volume")
	level.PlacementVolumes = []content.PlacementVolumeDef{{
		ID:                "volume-a",
		Kind:              content.PlacementVolumeKindSphere,
		AssetPath:         filepath.Join("..", "assets", "asteroid.gkasset"),
		CastsShadows:      &castsShadows,
		ShadowMaxDistance: 33,
		MaxShadowCasters:  5,
		Transform: content.LevelTransformDef{
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Radius:     8,
		RandomSeed: 7,
		Rule: content.PlacementVolumeRuleDef{
			Mode:  content.PlacementVolumeRuleModeCount,
			Count: 1,
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

	rootEntity := result.PlacementRootEntities["volume-a:0"]
	if rootEntity == 0 {
		t.Fatal("expected placement root entity")
	}
	var voxelEntity EntityId
	MakeQuery1[VoxelModelComponent](cmd).Map(func(eid EntityId, _ *VoxelModelComponent) bool {
		parent, ok := parentEntityForTest(cmd, eid)
		if ok && parent == rootEntity {
			voxelEntity = eid
			return false
		}
		return true
	})
	if voxelEntity == 0 {
		t.Fatal("expected spawned voxel child for placement volume instance")
	}
	vmc := mustVoxelModelComponentForLevelTest(t, cmd, voxelEntity)
	if !vmc.DisableShadows {
		t.Fatal("expected placement volume casts_shadows=false override to disable shadows")
	}
	if vmc.ShadowMaxDistance != 33 {
		t.Fatalf("expected placement volume shadow max distance 33, got %v", vmc.ShadowMaxDistance)
	}
	if vmc.ShadowCasterGroupID == 0 {
		t.Fatal("expected placement volume to assign a shadow caster group")
	}
	if vmc.ShadowCasterGroupLimit != 5 {
		t.Fatalf("expected placement volume max shadow casters 5, got %d", vmc.ShadowCasterGroupLimit)
	}
}

func TestSpawnAuthoredLevelCollapsesProceduralBrushesAndAppliesSubtract(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	assets := newSpawnTestAssetServer()

	level := content.NewLevelDef("brushes")
	level.VoxelResolution = 0.1
	level.Materials = []content.LevelMaterialDef{{
		ID:           "mat_stone",
		Name:         "Stone",
		BaseColor:    [4]uint8{180, 180, 180, 255},
		Roughness:    0.8,
		Metallic:     0,
		Emissive:     0,
		IOR:          1.4,
		Transparency: 0,
	}}
	level.BrushLayers[0].Brushes = []content.LevelBrushDef{
		{
			ID:         "solid",
			Name:       "solid",
			Primitive:  "cube",
			Params:     map[string]float32{"sx": 3, "sy": 3, "sz": 3},
			MaterialID: "mat_stone",
			Transform: content.LevelTransformDef{
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
		{
			ID:         "cut",
			Name:       "cut",
			Primitive:  "cube",
			Params:     map[string]float32{"sx": 1, "sy": 1, "sz": 1},
			MaterialID: "mat_stone",
			Operation:  content.AssetShapeOperationSubtract,
			Transform: content.LevelTransformDef{
				Position: content.Vec3{0.1, 0.1, 0.1},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
	}

	result, err := SpawnAuthoredLevel(cmd, assets, NewRuntimeContentLoader(), level, AuthoredLevelSpawnOptions{})
	if err != nil {
		t.Fatalf("SpawnAuthoredLevel failed: %v", err)
	}
	app.FlushCommands()

	root := result.BrushRootEntities["solid"]
	if root == 0 {
		t.Fatalf("expected brush root entity, got %#v", result.BrushRootEntities)
	}
	var bakedEntity EntityId
	MakeQuery1[VoxelModelComponent](cmd).Map(func(eid EntityId, _ *VoxelModelComponent) bool {
		parent, ok := parentEntityForTest(cmd, eid)
		if ok && parent == root {
			bakedEntity = eid
			return false
		}
		return true
	})
	if bakedEntity == 0 {
		t.Fatal("expected baked runtime brush child entity")
	}
	vmc := mustVoxelModelForSpawnTest(t, cmd, bakedEntity)
	geometry, ok := assets.GetVoxelGeometry(vmc.GeometryAsset())
	if !ok || geometry.XBrickMap == nil {
		t.Fatalf("expected collapsed brush geometry asset, got %+v ok=%v", geometry, ok)
	}
	if geometry.XBrickMap.GetVoxelCount() != 26 {
		t.Fatalf("expected subtractive brush bake to remove one voxel, got %d", geometry.XBrickMap.GetVoxelCount())
	}
	if found, _ := geometry.XBrickMap.GetVoxel(1, 1, 1); found {
		t.Fatal("expected subtractive brush to clear voxel at [1 1 1]")
	}
}

func TestBakeAuthoredLevelBrushesRespectsAuthorOrder(t *testing.T) {
	assets := newSpawnTestAssetServer()

	levelSubtractFirst := content.NewLevelDef("subtract-first")
	levelSubtractFirst.VoxelResolution = 0.1
	levelSubtractFirst.BrushLayers[0].Brushes = []content.LevelBrushDef{
		{
			ID:        "cut",
			Name:      "cut",
			Primitive: "cube",
			Params:    map[string]float32{"sx": 1, "sy": 1, "sz": 1},
			Operation: content.AssetShapeOperationSubtract,
			Transform: content.LevelTransformDef{
				Position: content.Vec3{0.1, 0.1, 0.1},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
		{
			ID:        "solid",
			Name:      "solid",
			Primitive: "cube",
			Params:    map[string]float32{"sx": 3, "sy": 3, "sz": 3},
			Transform: content.LevelTransformDef{
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
	}
	levelAddFirst := content.NewLevelDef("add-first")
	levelAddFirst.VoxelResolution = 0.1
	levelAddFirst.BrushLayers[0].Brushes = []content.LevelBrushDef{
		levelSubtractFirst.BrushLayers[0].Brushes[1],
		levelSubtractFirst.BrushLayers[0].Brushes[0],
	}

	firstBake, err := BakeAuthoredLevelBrushes(assets, levelSubtractFirst)
	if err != nil {
		t.Fatalf("BakeAuthoredLevelBrushes subtract-first failed: %v", err)
	}
	secondBake, err := BakeAuthoredLevelBrushes(assets, levelAddFirst)
	if err != nil {
		t.Fatalf("BakeAuthoredLevelBrushes add-first failed: %v", err)
	}

	if len(firstBake.Batches) != 1 || len(secondBake.Batches) != 1 {
		t.Fatalf("expected one baked batch per level, got first=%d second=%d", len(firstBake.Batches), len(secondBake.Batches))
	}
	firstGeometry, ok := assets.GetVoxelGeometry(firstBake.Batches[0].Geometry)
	if !ok || firstGeometry.XBrickMap == nil {
		t.Fatalf("expected first baked geometry, got %+v ok=%v", firstGeometry, ok)
	}
	secondGeometry, ok := assets.GetVoxelGeometry(secondBake.Batches[0].Geometry)
	if !ok || secondGeometry.XBrickMap == nil {
		t.Fatalf("expected second baked geometry, got %+v ok=%v", secondGeometry, ok)
	}

	if firstGeometry.XBrickMap.GetVoxelCount() != 27 {
		t.Fatalf("expected subtract-first bake to preserve later-empty space, got %d", firstGeometry.XBrickMap.GetVoxelCount())
	}
	if found, _ := firstGeometry.XBrickMap.GetVoxel(1, 1, 1); !found {
		t.Fatal("expected subtract-first bake to leave voxel at [1 1 1]")
	}
	if secondGeometry.XBrickMap.GetVoxelCount() != 26 {
		t.Fatalf("expected add-first bake to remove one voxel, got %d", secondGeometry.XBrickMap.GetVoxelCount())
	}
	if found, _ := secondGeometry.XBrickMap.GetVoxel(1, 1, 1); found {
		t.Fatal("expected add-first bake to clear voxel at [1 1 1]")
	}
	if reflect.DeepEqual(
		VoxelObjectSnapshotFromXBrickMap(firstGeometry.XBrickMap),
		VoxelObjectSnapshotFromXBrickMap(secondGeometry.XBrickMap),
	) {
		t.Fatal("expected authored order changes to produce different geometry")
	}
}

func TestBakeAuthoredLevelBrushesPreservesSequentialAuthorOrder(t *testing.T) {
	assets := newSpawnTestAssetServer()

	level := content.NewLevelDef("sequential-order")
	level.VoxelResolution = 0.1
	level.BrushLayers[0].Brushes = []content.LevelBrushDef{
		{
			ID:        "solid",
			Name:      "solid",
			Primitive: "cube",
			Params:    map[string]float32{"sx": 3, "sy": 3, "sz": 3},
			Transform: content.LevelTransformDef{
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
		{
			ID:        "cut",
			Name:      "cut",
			Primitive: "cube",
			Params:    map[string]float32{"sx": 1, "sy": 1, "sz": 1},
			Operation: content.AssetShapeOperationSubtract,
			Transform: content.LevelTransformDef{
				Position: content.Vec3{0.1, 0.1, 0.1},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
		{
			ID:        "detail",
			Name:      "detail",
			Primitive: "cube",
			Params:    map[string]float32{"sx": 1, "sy": 1, "sz": 1},
			Transform: content.LevelTransformDef{
				Position: content.Vec3{0.1, 0.1, 0.1},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
	}

	bake, err := BakeAuthoredLevelBrushes(assets, level)
	if err != nil {
		t.Fatalf("BakeAuthoredLevelBrushes failed: %v", err)
	}
	if len(bake.Batches) != 1 {
		t.Fatalf("expected one batch for sequential order test, got %d", len(bake.Batches))
	}
	geometry, ok := assets.GetVoxelGeometry(bake.Batches[0].Geometry)
	if !ok || geometry.XBrickMap == nil {
		t.Fatalf("expected baked geometry, got %+v ok=%v", geometry, ok)
	}
	if geometry.XBrickMap.GetVoxelCount() != 27 {
		t.Fatalf("expected later additive brush to refill cut voxel, got %d voxels", geometry.XBrickMap.GetVoxelCount())
	}
	if found, _ := geometry.XBrickMap.GetVoxel(1, 1, 1); !found {
		t.Fatal("expected final additive brush to restore voxel at [1 1 1]")
	}
}

func TestBakeAuthoredLevelBrushesSupportsMultipleAdditiveMaterials(t *testing.T) {
	assets := newSpawnTestAssetServer()

	level := content.NewLevelDef("multi-material")
	level.VoxelResolution = 0.1
	level.Materials = []content.LevelMaterialDef{
		{
			ID:           "red",
			Name:         "Red",
			BaseColor:    [4]uint8{220, 80, 80, 255},
			Roughness:    0.8,
			Metallic:     0.05,
			Emissive:     0,
			IOR:          1.4,
			Transparency: 0,
		},
		{
			ID:           "blue",
			Name:         "Blue",
			BaseColor:    [4]uint8{80, 120, 220, 255},
			Roughness:    0.4,
			Metallic:     0.2,
			Emissive:     0,
			IOR:          1.3,
			Transparency: 0,
		},
	}
	level.BrushLayers[0].Brushes = []content.LevelBrushDef{
		{
			ID:         "red-voxel",
			Name:       "red-voxel",
			Primitive:  "cube",
			Params:     map[string]float32{"sx": 1, "sy": 1, "sz": 1},
			MaterialID: "red",
			Transform: content.LevelTransformDef{
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
		{
			ID:         "blue-voxel",
			Name:       "blue-voxel",
			Primitive:  "cube",
			Params:     map[string]float32{"sx": 1, "sy": 1, "sz": 1},
			MaterialID: "blue",
			Transform: content.LevelTransformDef{
				Position: content.Vec3{0.1, 0, 0},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
	}

	bake, err := BakeAuthoredLevelBrushes(assets, level)
	if err != nil {
		t.Fatalf("BakeAuthoredLevelBrushes failed: %v", err)
	}
	if len(bake.Batches) != 2 {
		t.Fatalf("expected two baked batches for distinct additive materials, got %d", len(bake.Batches))
	}
	for i, want := range [][4]uint8{{220, 80, 80, 255}, {80, 120, 220, 255}} {
		geometry, ok := assets.GetVoxelGeometry(bake.Batches[i].Geometry)
		if !ok || geometry.XBrickMap == nil {
			t.Fatalf("expected baked geometry for batch %d, got %+v ok=%v", i, geometry, ok)
		}
		if geometry.XBrickMap.GetVoxelCount() != 1 {
			t.Fatalf("expected one voxel in batch %d, got %d", i, geometry.XBrickMap.GetVoxelCount())
		}
		palette, ok := assets.GetVoxelPalette(bake.Batches[i].Palette)
		if !ok {
			t.Fatalf("expected palette for batch %d", i)
		}
		if palette.VoxPalette[1] != want {
			t.Fatalf("expected palette color %v for batch %d, got %v", want, i, palette.VoxPalette[1])
		}
	}
}

func TestBakeAuthoredLevelBrushesUsesLastWriterWhenMaterialsOverlap(t *testing.T) {
	assets := newSpawnTestAssetServer()

	level := content.NewLevelDef("multi-material-overlap")
	level.VoxelResolution = 0.1
	level.Materials = []content.LevelMaterialDef{
		{
			ID:           "red",
			Name:         "Red",
			BaseColor:    [4]uint8{220, 80, 80, 255},
			Roughness:    0.8,
			Metallic:     0.05,
			Emissive:     0,
			IOR:          1.4,
			Transparency: 0,
		},
		{
			ID:           "blue",
			Name:         "Blue",
			BaseColor:    [4]uint8{80, 120, 220, 255},
			Roughness:    0.4,
			Metallic:     0.2,
			Emissive:     0,
			IOR:          1.3,
			Transparency: 0,
		},
	}
	level.BrushLayers[0].Brushes = []content.LevelBrushDef{
		{
			ID:         "red-voxel",
			Name:       "red-voxel",
			Primitive:  "cube",
			Params:     map[string]float32{"sx": 1, "sy": 1, "sz": 1},
			MaterialID: "red",
			Transform: content.LevelTransformDef{
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
		{
			ID:         "blue-voxel",
			Name:       "blue-voxel",
			Primitive:  "cube",
			Params:     map[string]float32{"sx": 1, "sy": 1, "sz": 1},
			MaterialID: "blue",
			Transform: content.LevelTransformDef{
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
	}

	bake, err := BakeAuthoredLevelBrushes(assets, level)
	if err != nil {
		t.Fatalf("BakeAuthoredLevelBrushes failed: %v", err)
	}
	if len(bake.Batches) != 1 {
		t.Fatalf("expected overwritten material batch to be dropped, got %d batches", len(bake.Batches))
	}
	geometry, ok := assets.GetVoxelGeometry(bake.Batches[0].Geometry)
	if !ok || geometry.XBrickMap == nil {
		t.Fatalf("expected surviving geometry, got %+v ok=%v", geometry, ok)
	}
	if geometry.XBrickMap.GetVoxelCount() != 1 {
		t.Fatalf("expected surviving batch to own the overlapping voxel, got %d voxels", geometry.XBrickMap.GetVoxelCount())
	}
	palette, ok := assets.GetVoxelPalette(bake.Batches[0].Palette)
	if !ok {
		t.Fatal("expected surviving batch palette")
	}
	if palette.VoxPalette[1] != ([4]uint8{80, 120, 220, 255}) {
		t.Fatalf("expected last writer palette color, got %v", palette.VoxPalette[1])
	}
}

func TestBakeAuthoredLevelBrushesMatchesSpawnedRuntimeBrushGeometryAndPalette(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	assets := newSpawnTestAssetServer()

	level := content.NewLevelDef("parity")
	level.VoxelResolution = 0.1
	level.Materials = []content.LevelMaterialDef{{
		ID:           "glass",
		Name:         "Glass",
		BaseColor:    [4]uint8{150, 190, 220, 180},
		Roughness:    0.15,
		Metallic:     0,
		Emissive:     0,
		IOR:          1.45,
		Transparency: 0.3,
	}}
	level.BrushLayers[0].Brushes = []content.LevelBrushDef{
		{
			ID:         "wall",
			Name:       "wall",
			Primitive:  "cube",
			Params:     map[string]float32{"sx": 4, "sy": 4, "sz": 1},
			MaterialID: "glass",
			Transform: content.LevelTransformDef{
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
		{
			ID:        "cut",
			Name:      "cut",
			Primitive: "cube",
			Params:    map[string]float32{"sx": 1, "sy": 2, "sz": 1},
			Operation: content.AssetShapeOperationSubtract,
			Transform: content.LevelTransformDef{
				Position: content.Vec3{0.1, 0.1, 0},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
	}

	bake, err := BakeAuthoredLevelBrushes(assets, level)
	if err != nil {
		t.Fatalf("BakeAuthoredLevelBrushes failed: %v", err)
	}
	result, err := SpawnAuthoredLevel(cmd, assets, NewRuntimeContentLoader(), level, AuthoredLevelSpawnOptions{})
	if err != nil {
		t.Fatalf("SpawnAuthoredLevel failed: %v", err)
	}
	app.FlushCommands()

	root := result.BrushRootEntities["wall"]
	if root == 0 {
		t.Fatalf("expected brush root entity, got %#v", result.BrushRootEntities)
	}
	var bakedEntity EntityId
	MakeQuery1[VoxelModelComponent](cmd).Map(func(eid EntityId, _ *VoxelModelComponent) bool {
		parent, ok := parentEntityForTest(cmd, eid)
		if ok && parent == root {
			bakedEntity = eid
			return false
		}
		return true
	})
	if bakedEntity == 0 {
		t.Fatal("expected baked runtime brush child entity")
	}
	vmc := mustVoxelModelForSpawnTest(t, cmd, bakedEntity)
	runtimeGeometry, ok := assets.GetVoxelGeometry(vmc.GeometryAsset())
	if !ok || runtimeGeometry.XBrickMap == nil {
		t.Fatalf("expected runtime geometry asset, got %+v ok=%v", runtimeGeometry, ok)
	}
	if len(bake.Batches) != 1 {
		t.Fatalf("expected single baked batch for parity test, got %d", len(bake.Batches))
	}
	bakedGeometry, ok := assets.GetVoxelGeometry(bake.Batches[0].Geometry)
	if !ok || bakedGeometry.XBrickMap == nil {
		t.Fatalf("expected baked geometry asset, got %+v ok=%v", bakedGeometry, ok)
	}
	if !reflect.DeepEqual(
		VoxelObjectSnapshotFromXBrickMap(runtimeGeometry.XBrickMap),
		VoxelObjectSnapshotFromXBrickMap(bakedGeometry.XBrickMap),
	) {
		t.Fatal("expected baked brush geometry to match spawned runtime geometry")
	}

	runtimePalette, ok := assets.GetVoxelPalette(vmc.VoxelPalette)
	if !ok {
		t.Fatalf("expected runtime palette asset %v", vmc.VoxelPalette)
	}
	bakedPalette, ok := assets.GetVoxelPalette(bake.Batches[0].Palette)
	if !ok {
		t.Fatalf("expected baked palette asset %v", bake.Batches[0].Palette)
	}
	if !reflect.DeepEqual(runtimePalette, bakedPalette) {
		t.Fatalf("expected baked/runtime palettes to match, runtime=%+v baked=%+v", runtimePalette, bakedPalette)
	}
}

func TestBakeAuthoredLevelBrushesSupportsVoxelShapeBrushes(t *testing.T) {
	assets := newSpawnTestAssetServer()
	level := content.NewLevelDef("custom")
	level.Materials = []content.LevelMaterialDef{{
		ID:        "stone",
		Name:      "Stone",
		BaseColor: [4]uint8{120, 130, 140, 255},
		Roughness: 1,
		IOR:       1.5,
	}}
	level.BrushLayers[0].Brushes = []content.LevelBrushDef{{
		ID:   "brush-custom",
		Name: "custom",
		Kind: content.LevelBrushKindVoxelShape,
		VoxelShape: &content.AssetVoxelShapeDef{
			Palette: []content.AssetVoxelPaletteEntryDef{{Value: 1, MaterialID: "stone"}},
			Voxels: []content.VoxelObjectVoxelDef{
				{X: 0, Y: 0, Z: 0, Value: 1},
				{X: 1, Y: 0, Z: 0, Value: 1},
			},
		},
		Transform: content.LevelTransformDef{
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}}

	bake, err := BakeAuthoredLevelBrushes(assets, level)
	if err != nil {
		t.Fatalf("BakeAuthoredLevelBrushes failed: %v", err)
	}
	if len(bake.Batches) != 1 {
		t.Fatalf("expected one baked batch, got %d", len(bake.Batches))
	}
	geometry, ok := assets.GetVoxelGeometry(bake.Batches[0].Geometry)
	if !ok || geometry.XBrickMap == nil {
		t.Fatalf("expected baked geometry asset, got %+v ok=%v", geometry, ok)
	}
	if geometry.XBrickMap.GetVoxelCount() != 2 {
		t.Fatalf("expected two voxels in baked custom brush, got %d", geometry.XBrickMap.GetVoxelCount())
	}
}

func TestCachedLevelBrushModelAssetReusesRepeatedPrimitiveCombos(t *testing.T) {
	assets := newSpawnTestAssetServer()
	cache := &levelBrushBakeResolveCache{
		models:   make(map[string]AssetId),
		palettes: make(map[string]AssetId),
	}

	first, err := cachedLevelBrushModelAsset(assets, cache, content.LevelBrushDef{
		Primitive: "cube",
		Params:    map[string]float32{"sx": 4, "sy": 2, "sz": 6},
	})
	if err != nil {
		t.Fatalf("cachedLevelBrushModelAsset first failed: %v", err)
	}
	second, err := cachedLevelBrushModelAsset(assets, cache, content.LevelBrushDef{
		Primitive: "cube",
		Params:    map[string]float32{"sx": 4, "sy": 2, "sz": 6},
	})
	if err != nil {
		t.Fatalf("cachedLevelBrushModelAsset second failed: %v", err)
	}
	third, err := cachedLevelBrushModelAsset(assets, cache, content.LevelBrushDef{
		Primitive: "cube",
		Params:    map[string]float32{"sx": 5, "sy": 2, "sz": 6},
	})
	if err != nil {
		t.Fatalf("cachedLevelBrushModelAsset third failed: %v", err)
	}

	if first != second {
		t.Fatalf("expected repeated primitive combo to reuse cached model, got %v and %v", first, second)
	}
	if third == first {
		t.Fatalf("expected distinct primitive params to produce distinct model ids, got %v", third)
	}
	if len(cache.models) != 2 {
		t.Fatalf("expected two cached model combos, got %d", len(cache.models))
	}
}

func TestCachedLevelBrushPaletteReusesRepeatedMaterialCombos(t *testing.T) {
	assets := newSpawnTestAssetServer()
	cache := &levelBrushBakeResolveCache{
		models:   make(map[string]AssetId),
		palettes: make(map[string]AssetId),
	}
	level := content.NewLevelDef("palette-cache")
	level.Materials = []content.LevelMaterialDef{
		{
			ID:           "stone",
			Name:         "Stone",
			BaseColor:    [4]uint8{140, 140, 140, 255},
			Roughness:    0.8,
			Metallic:     0,
			Emissive:     0,
			IOR:          1.4,
			Transparency: 0,
		},
		{
			ID:           "glass",
			Name:         "Glass",
			BaseColor:    [4]uint8{150, 190, 220, 180},
			Roughness:    0.15,
			Metallic:     0,
			Emissive:     0,
			IOR:          1.45,
			Transparency: 0.3,
		},
	}

	first, err := cachedAuthoredLevelBrushPalette(assets, cache, level, content.LevelBrushDef{MaterialID: "stone"})
	if err != nil {
		t.Fatalf("cachedAuthoredLevelBrushPalette first failed: %v", err)
	}
	second, err := cachedAuthoredLevelBrushPalette(assets, cache, level, content.LevelBrushDef{MaterialID: "stone"})
	if err != nil {
		t.Fatalf("cachedAuthoredLevelBrushPalette second failed: %v", err)
	}
	third, err := cachedAuthoredLevelBrushPalette(assets, cache, level, content.LevelBrushDef{MaterialID: "glass"})
	if err != nil {
		t.Fatalf("cachedAuthoredLevelBrushPalette third failed: %v", err)
	}

	if first != second {
		t.Fatalf("expected repeated material combo to reuse cached palette, got %v and %v", first, second)
	}
	if third == first {
		t.Fatalf("expected distinct material combo to produce distinct palette ids, got %v", third)
	}
	if len(cache.palettes) != 2 {
		t.Fatalf("expected two cached palette combos, got %d", len(cache.palettes))
	}
}

func TestBakeAuthoredLevelBrushesHandlesLargeRepeatedBrushCounts(t *testing.T) {
	assets := newSpawnTestAssetServer()
	level := content.NewLevelDef("large-repeated")
	level.ChunkSize = 16
	level.VoxelResolution = 1
	level.BrushLayers[0].Brushes = make([]content.LevelBrushDef, 0, 96)
	for i := 0; i < 96; i++ {
		level.BrushLayers[0].Brushes = append(level.BrushLayers[0].Brushes, content.LevelBrushDef{
			ID:        "brush-" + strconv.Itoa(i),
			Name:      "pillar",
			Primitive: "cube",
			Params:    map[string]float32{"sx": 2, "sy": 6, "sz": 2},
			Transform: content.LevelTransformDef{
				Position: content.Vec3{float32((i % 12) * 3), 0, float32((i / 12) * 3)},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		})
	}

	bake, err := BakeAuthoredLevelBrushes(assets, level)
	if err != nil {
		t.Fatalf("BakeAuthoredLevelBrushes failed for large repeated set: %v", err)
	}
	if len(bake.Batches) != 1 {
		t.Fatalf("expected one baked batch for repeated brush set, got %d", len(bake.Batches))
	}
	geometry, ok := assets.GetVoxelGeometry(bake.Batches[0].Geometry)
	if !ok || geometry.XBrickMap == nil {
		t.Fatalf("expected baked geometry for repeated brush set, got %+v ok=%v", geometry, ok)
	}
	if geometry.XBrickMap.GetVoxelCount() == 0 {
		t.Fatal("expected non-empty baked geometry for repeated brush set")
	}
	if len(assets.voxModels) < 2 {
		t.Fatalf("expected primitive and baked geometry assets to be registered, got %d", len(assets.voxModels))
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

func TestLoadAndSpawnAuthoredLevelWithoutEnvironmentDoesNotInjectLightingOrSkybox(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	level := content.NewLevelDef("no-environment")

	if _, err := SpawnAuthoredLevel(cmd, nil, NewRuntimeContentLoader(), level, AuthoredLevelSpawnOptions{}); err != nil {
		t.Fatalf("SpawnAuthoredLevel failed: %v", err)
	}
	app.FlushCommands()

	var lightCount, skyAmbientCount, skySunCount, skyboxLayerCount int
	MakeQuery1[LightComponent](cmd).Map(func(_ EntityId, _ *LightComponent) bool {
		lightCount++
		return true
	})
	MakeQuery1[SkyAmbientComponent](cmd).Map(func(_ EntityId, _ *SkyAmbientComponent) bool {
		skyAmbientCount++
		return true
	})
	MakeQuery1[SkyboxSunComponent](cmd).Map(func(_ EntityId, _ *SkyboxSunComponent) bool {
		skySunCount++
		return true
	})
	MakeQuery1[SkyboxLayerComponent](cmd).Map(func(_ EntityId, _ *SkyboxLayerComponent) bool {
		skyboxLayerCount++
		return true
	})

	if lightCount != 0 {
		t.Fatalf("expected no injected lights, got %d", lightCount)
	}
	if skyAmbientCount != 0 {
		t.Fatalf("expected no injected sky ambient, got %d", skyAmbientCount)
	}
	if skySunCount != 0 {
		t.Fatalf("expected no injected sky sun, got %d", skySunCount)
	}
	if skyboxLayerCount != 0 {
		t.Fatalf("expected no injected skybox layers, got %d", skyboxLayerCount)
	}
}

func TestLoadAndSpawnAuthoredLevelAppliesFullmoonNightEnvironment(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	level := content.NewLevelDef("fullmoon")
	level.Environment = &content.LevelEnvironmentDef{Preset: "fullmoonNight"}

	if _, err := SpawnAuthoredLevel(cmd, nil, NewRuntimeContentLoader(), level, AuthoredLevelSpawnOptions{}); err != nil {
		t.Fatalf("SpawnAuthoredLevel failed: %v", err)
	}
	app.FlushCommands()

	var skySunCount, skyAmbientCount, starLayers int
	MakeQuery1[SkyboxSunComponent](cmd).Map(func(_ EntityId, _ *SkyboxSunComponent) bool {
		skySunCount++
		return true
	})
	MakeQuery1[SkyAmbientComponent](cmd).Map(func(_ EntityId, ambient *SkyAmbientComponent) bool {
		skyAmbientCount++
		if ambient.SkyMix <= 0 || ambient.SkyMix >= 0.2 {
			t.Fatalf("expected low fullmoon sky ambient mix, got %f", ambient.SkyMix)
		}
		return true
	})
	MakeQuery1[SkyboxLayerComponent](cmd).Map(func(_ EntityId, layer *SkyboxLayerComponent) bool {
		if layer.LayerType == SkyboxLayerStars {
			starLayers++
		}
		return true
	})

	if skySunCount != 1 {
		t.Fatalf("expected one sky sun/moon component, got %d", skySunCount)
	}
	if skyAmbientCount != 1 {
		t.Fatalf("expected one sky ambient component, got %d", skyAmbientCount)
	}
	if starLayers == 0 {
		t.Fatal("expected fullmoonNight skybox to include stars")
	}
}

func TestLoadAndSpawnAuthoredLevelAppliesFullmoonNightGIEnvironment(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	level := content.NewLevelDef("fullmoon-gi")
	level.Environment = &content.LevelEnvironmentDef{Preset: "fullmoonnight_gi"}

	if _, err := SpawnAuthoredLevel(cmd, nil, NewRuntimeContentLoader(), level, AuthoredLevelSpawnOptions{}); err != nil {
		t.Fatalf("SpawnAuthoredLevel failed: %v", err)
	}
	app.FlushCommands()

	var ambientFound, directionalFound bool
	MakeQuery1[LightComponent](cmd).Map(func(_ EntityId, light *LightComponent) bool {
		switch light.Type {
		case LightTypeAmbient:
			ambientFound = true
			if light.Intensity != 0.004 {
				t.Fatalf("expected fullmoonnight_gi ambient intensity 0.004, got %f", light.Intensity)
			}
		case LightTypeDirectional:
			directionalFound = true
			if light.Intensity != 0.16 {
				t.Fatalf("expected fullmoonnight_gi directional intensity 0.16, got %f", light.Intensity)
			}
		}
		return true
	})

	var skyAmbientCount, starLayers int
	MakeQuery1[SkyAmbientComponent](cmd).Map(func(_ EntityId, ambient *SkyAmbientComponent) bool {
		skyAmbientCount++
		if ambient.SkyMix != 0.08 {
			t.Fatalf("expected fullmoonnight_gi sky mix 0.08, got %f", ambient.SkyMix)
		}
		return true
	})
	MakeQuery1[SkyboxLayerComponent](cmd).Map(func(_ EntityId, layer *SkyboxLayerComponent) bool {
		if layer.LayerType == SkyboxLayerStars {
			starLayers++
		}
		return true
	})

	if !ambientFound {
		t.Fatal("expected ambient light for fullmoonnight_gi")
	}
	if !directionalFound {
		t.Fatal("expected directional light for fullmoonnight_gi")
	}
	if skyAmbientCount != 1 {
		t.Fatalf("expected one sky ambient component, got %d", skyAmbientCount)
	}
	if starLayers == 0 {
		t.Fatal("expected fullmoonnight_gi skybox to include stars")
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
	assets := newSpawnTestAssetServer()
	result, err := LoadAndSpawnAuthoredLevel(levelPath, cmd, assets, NewRuntimeContentLoader(), AuthoredLevelSpawnOptions{
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
	geometryMap, ok := ResolveVoxelGeometryMap(assets, &vmc)
	if !ok || geometryMap.GetVoxelCount() != 4 {
		t.Fatalf("expected terrain override geometry with 4 voxels, got %+v", geometryMap)
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

func writeCollapsedProceduralAssetForLevelTest(t *testing.T, path string, assetID string) {
	t.Helper()
	def := content.NewAssetDef(assetID)
	def.ID = assetID
	def.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true}
	def.Parts = []content.AssetPartDef{
		{
			ID:     assetID + "-part-a",
			Name:   "part-a",
			Source: testProceduralPartSource(),
			Transform: content.AssetTransformDef{
				Position: content.Vec3{-0.5, 0, 0},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
		{
			ID:     assetID + "-part-b",
			Name:   "part-b",
			Source: testProceduralPartSource(),
			Transform: content.AssetTransformDef{
				Position: content.Vec3{0.5, 0, 0},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
	}
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
