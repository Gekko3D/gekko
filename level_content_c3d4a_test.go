package gekko

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestC3d4aCompiledDirectExpandedAndLibraryPlacements(t *testing.T) {
	input, path, asset := c3d3Fixture(t)
	root := filepath.Dir(path)
	levelPath := filepath.Join(root, "level.gklevel")
	level := content.NewLevelDef("Compiled placements")
	transform := content.LevelTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}
	level.Placements = []content.LevelPlacementDef{{ID: "first", AssetPath: filepath.Base(path), Transform: transform}, {ID: "second", AssetPath: filepath.Base(path), Transform: transform}}
	setPath := filepath.Join(root, "set.gkassetset")
	if err := content.SaveAssetSet(setPath, &content.AssetSetDef{ID: "set", SchemaVersion: 1, Name: "Set", Entries: []content.AssetSetEntryDef{{AssetPath: filepath.Base(path), Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	shadows := false
	level.PlacementVolumes = []content.PlacementVolumeDef{{ID: "volume", Kind: content.PlacementVolumeKindSphere, AssetSetPath: filepath.Base(setPath), CastsShadows: &shadows, ShadowMaxDistance: 33, MaxShadowCasters: 5, Radius: 8, RandomSeed: 7, Rule: content.PlacementVolumeRuleDef{Mode: content.PlacementVolumeRuleModeCount, Count: 1}, Transform: transform}}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(input)); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	assets := c3d3Server()
	app.Commands().AddResources(assets)
	loader := NewRuntimeContentLoader()
	result, err := LoadAndSpawnAuthoredLevel(levelPath, app.Commands(), assets, loader, AuthoredLevelSpawnOptions{})
	if err != nil {
		t.Fatal("compiled level placement failed", err)
	}
	app.FlushCommands()
	var shared AssetId
	for _, placement := range []string{"first", "second", "volume:0"} {
		rootEntity := result.PlacementRootEntities[placement]
		if rootEntity == 0 {
			t.Fatal("compiled placement root missing", placement)
		}
		ref := s3cComponent[AuthoredLevelPlacementRefComponent](t, app.Commands(), rootEntity)
		if ref.LevelID != level.ID || ref.PlacementID != placement || filepath.Base(ref.AssetPath) != filepath.Base(path) {
			t.Fatal("compiled placement ownership/path lost")
		}
		entity := placementItemEntityByIDForStreamedTest(app.Commands(), placement, "shape")
		if entity == 0 {
			t.Fatal("compiled placement voxel item missing")
		}
		item := s3cComponent[AuthoredLevelItemRefComponent](t, app.Commands(), entity)
		if item.AssetID != asset.ID || item.ItemID != "shape" || item.PlacementID != placement || filepath.Base(item.AssetPath) != filepath.Base(path) {
			t.Fatal("compiled item authored ownership lost")
		}
		parent := s3cComponent[Parent](t, app.Commands(), entity)
		if parent.Entity != placementItemEntityByIDForStreamedTest(app.Commands(), placement, "root") {
			t.Fatal("compiled placement hierarchy lost")
		}
		vmc := s3cComponent[VoxelModelComponent](t, app.Commands(), entity)
		if shared == (AssetId{}) {
			shared = vmc.GeometryAsset()
		} else if vmc.GeometryAsset() != shared {
			t.Fatal("repeated compiled placements did not share primary geometry")
		}
		if vmc.CustomPivot != mgl32.Vec3(asset.Parts[0].Transform.Pivot) || vmc.VoxelResolution != asset.Parts[0].VoxelResolution {
			t.Fatal("compiled placement pivot/lattice changed")
		}
		if placement == "volume:0" && (!vmc.DisableShadows || vmc.ShadowMaxDistance != 33 || vmc.ShadowCasterGroupID == 0 || vmc.ShadowCasterGroupLimit != 5 || ref.VolumeID != "volume") {
			t.Fatal("compiled volume placement shadow overrides lost")
		}
		palette, ok := assets.GetVoxelPalette(vmc.VoxelPalette)
		if !ok || palette.VoxPalette[3] != asset.Materials[0].BaseColor {
			t.Fatal("compiled level placement material lost")
		}
	}
	first := placementItemEntityByIDForStreamedTest(app.Commands(), "first", "shape")
	if err := EnableManagedVoxelGeometry(app.Commands(), assets, first); err != nil {
		t.Fatal(err)
	}
	app.FlushCommands()
	if err := ApplyManagedVoxelWrites(app.Commands(), assets, first, p1dWrites(volume.VoxelWrite{X: -18, Value: 77})); err != nil {
		t.Fatal(err)
	}
	other := placementItemEntityByIDForStreamedTest(app.Commands(), "second", "shape")
	otherMap, _, ok := currentVoxelMapForEntity(app.Commands(), other)
	if !ok || otherMap == nil {
		t.Fatal("compiled sibling geometry resource missing")
	}
	if found, value := otherMap.GetVoxel(-18, 0, 0); !found || value == 77 {
		t.Fatal("compiled placement edit leaked into sibling")
	}
	library := &content.AssetLibraryDef{ID: "library", SchemaVersion: 1, Name: "Library", Entries: []content.AssetLibraryEntryDef{{Key: "compiled", AssetPath: filepath.Base(path)}}}
	if _, err := LoadAndSpawnAuthoredAssetFromLibrary(library, filepath.Join(root, "library.gklibrary"), "compiled", app.Commands(), assets, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}); err != nil {
		t.Fatal("compiled library asset failed", err)
	}
}

func c3d4aRuntime(t *testing.T) (*s1gFixture, content.AssetPartDef, string, string) {
	t.Helper()
	f, part, deltaPath := e2b2Runtime(t)
	source := filepath.Join(filepath.Dir(f.runtime.Config.LevelPath), "placement.gkasset")
	asset, err := content.LoadAsset(source)
	if err != nil {
		t.Fatal(err)
	}
	part.Source.VoxelShape.Voxels = nil
	for i := 0; i < 34; i++ {
		part.Source.VoxelShape.Voxels = append(part.Source.VoxelShape.Voxels, content.VoxelObjectVoxelDef{X: i % 8, Y: (i / 8) % 8, Value: 1})
	}
	asset.Parts[0] = part
	if err := content.SaveAsset(source, asset); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "placement.gkassetc")
	if _, err := CompileAuthoredAsset(source, path, nil); err != nil {
		t.Fatal(err)
	}
	f.runtime.PlacementsByChunk[ChunkCoord{}][0].AssetPath = path
	f.runtime.Level.Placements[0].AssetPath = path
	f.runtime.Config.EnableHybridVoxelObjectDeltas = true
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	return f, part, path, deltaPath
}

func TestC3d4aCompiledStreamCommitEditSaveReload(t *testing.T) {
	for _, mode := range []string{"sparse", "hybrid", "full"} {
		t.Run(mode, func(t *testing.T) {
			f, part, _, deltaPath := c3d4aRuntime(t)
			placement := s1gID(0, 0)
			if mode == "full" {
				_, lattice, _ := e2b1Canonical(t, part)
				e2b2Save(t, f, deltaPath, &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: content.VoxelObjectPayloadFull, PlacementID: placement, ItemID: "body", Lattice: lattice, Voxels: []content.VoxelObjectVoxelDef{{Value: 5}}})
			}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := e2b2Body(t, f, placement)
			e2b3Enable(t, f, eid)
			if mode != "full" {
				e2b3Qualified(t, f, eid, placement, part)
			}
			writes := []volume.VoxelWrite{{Value: 7}}
			if mode == "hybrid" {
				for i := 1; i < 34; i++ {
					writes = append(writes, volume.VoxelWrite{X: i % 8, Y: (i / 8) % 8})
				}
			}
			e2b4Edit(t, f, eid, writes...)
			current, _, ok := currentVoxelMapForEntity(f.cmd, eid)
			if !ok {
				t.Fatal("compiled stream current geometry missing")
			}
			want := c3cGeometry(VoxelObjectSnapshotFromXBrickMap(current).Voxels)
			e2b4Depart(f)
			e2b4Unloaded(t, f)
			durable := e2b4Durable(t, f)
			if mode == "sparse" && (durable.SchemaVersion != 2 || durable.Mode != content.VoxelObjectPayloadBaseDelta) {
				t.Fatal("compiled sparse edit lost original-base delta")
			}
			if mode == "hybrid" && (durable.SchemaVersion != 3 || durable.Mode != content.VoxelObjectPayloadHybridDelta) {
				t.Fatal("compiled hybrid edit lost replacement selection")
			}
			if mode == "full" && durable.Mode != content.VoxelObjectPayloadFull {
				t.Fatal("compiled full replacement incorrectly invented original history")
			}
			f.hooks[placement] = 0
			s3aMove(f.cmd, f.observer, mgl32.Vec3{1, 1, 1})
			updateStreamedObserverSelection(f.cmd, f.runtime)
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			reloaded := e2b2Body(t, f, placement)
			loaded, _, ok := currentVoxelMapForEntity(f.cmd, reloaded)
			if !ok || !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(loaded).Voxels), want) {
				t.Fatal("compiled stream delta reload changed geometry")
			}
			e2b3Enable(t, f, reloaded)
			if mode != "full" {
				e2b3Qualified(t, f, reloaded, placement, part)
				changes, tracked := ManagedVoxelGeometryChanges(f.cmd, f.assets, reloaded)
				if !tracked || len(changes) != len(writes) {
					t.Fatal("compiled delta reload failed to restore original edit history", changes)
				}
			}
		})
	}
}

func TestC3d4aInvalidCompiledPlacementPublishesNoAssetEntities(t *testing.T) {
	for _, kind := range []string{"later-frame", "animation"} {
		t.Run(kind, func(t *testing.T) {
			_, path, _ := c3d3Fixture(t)
			header, _, err := content.LoadCompiledAssetHeader(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "later-frame" {
				bad := filepath.Join(filepath.Dir(path), "bad.gkshape")
				if err := os.WriteFile(bad, []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
				header.Shapes[0].Path = "bad.gkshape"
			} else {
				header.Asset.AnimationSetPaths[0] = "dependencies/missing.gkanim"
			}
			if _, err := content.SaveCompiledAssetHeader(path, header, nil); err != nil {
				t.Fatal(err)
			}
			app := NewApp()
			assets := c3d3Server()
			callbacks := 0
			_, err = spawnAuthoredLevelPlacementWithOwnership(app.Commands(), assets, NewRuntimeContentLoader(), 0, "level", filepath.Join(filepath.Dir(path), "level.gklevel"), AuthoredPlacementSpawnDef{PlacementID: "placement", AssetPath: path, Transform: content.LevelTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}}, func(EntityId, string, bool, bool) { callbacks++ })
			app.FlushCommands()
			entities := 0
			MakeQuery1[AuthoredAssetRootComponent](app.Commands()).Map(func(EntityId, *AuthoredAssetRootComponent) bool { entities++; return true })
			MakeQuery1[AuthoredAssetRefComponent](app.Commands()).Map(func(EntityId, *AuthoredAssetRefComponent) bool { entities++; return true })
			if err == nil || callbacks != 0 || entities != 0 || len(assets.voxModels) != 0 || len(assets.voxPalettes) != 0 {
				t.Fatal("invalid compiled placement partially published geometry/entities/ownership")
			}
		})
	}
}

func TestC3d4aCompiledStreamHookFailureStopOwnsEntitiesPreservesCallerPins(t *testing.T) {
	f, _, path, _ := c3d4aRuntime(t)
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	independent := owner.NewScope()
	defer independent.Close()
	header, _, err := independent.Loader().LoadCompiledAssetHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	shapePath := filepath.Join(filepath.Dir(path), header.Shapes[0].Path)
	shape, _, err := independent.Loader().LoadCompiledAssetShape(shapePath)
	if err != nil {
		t.Fatal(err)
	}
	before := owner.Stats()
	f.runtime.Loader = owner
	var spawned []EntityId
	f.runtime.Config.PlacementHooks = append(f.runtime.Config.PlacementHooks, func(_ *Commands, context PostSpawnPlacementContext) {
		spawned = append(spawned, context.SpawnResult.Entities...)
		f.runtime.InitErr = fmt.Errorf("compiled hook failure")
	})
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
	f.app.FlushCommands()
	if f.runtime.InitErr == nil || len(spawned) < 2 || len(f.runtime.LoadedChunks) != 0 {
		t.Fatal("compiled hook failure did not retain partial transaction ownership")
	}
	for _, eid := range spawned {
		if !f.cmd.EntityExists(eid) {
			t.Fatal("hook fixture did not observe published entity")
		}
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	for _, eid := range spawned {
		if f.cmd.EntityExists(eid) {
			t.Fatal("compiled partial placement leaked entity after Stop")
		}
	}
	if stats := owner.Stats(); stats.Entries != before.Entries || stats.PinnedBytes != before.PinnedBytes {
		t.Fatal("stream Stop revoked independent accepted compiled pins")
	}
	if got, _, err := independent.Loader().LoadCompiledAssetHeader(path); err != nil || got != header {
		t.Fatal("independent compiled header lost", err)
	}
	if got, _, err := independent.Loader().LoadCompiledAssetShape(shapePath); err != nil || got != shape {
		t.Fatal("independent compiled shape lost", err)
	}
}
