package gekko

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func c3d4bItems(cmd *Commands) (EntityId, map[string]EntityId) {
	var root EntityId
	items := make(map[string]EntityId)
	MakeQuery1[AuthoredAssetRootComponent](cmd).Map(func(entity EntityId, _ *AuthoredAssetRootComponent) bool {
		root = entity
		return true
	})
	MakeQuery1[AuthoredAssetRefComponent](cmd).Map(func(entity EntityId, ref *AuthoredAssetRefComponent) bool {
		items[ref.ItemID] = entity
		return true
	})
	return root, items
}

func TestC3d4bCompiledNPCMatchesJSONAfterAuthoringSourceRemoval(t *testing.T) {
	input, path, authored := c3d3Fixture(t)
	// The legacy emitter resolves its texture directly; only this local comparator is rebased.
	authored.Emitters[0].Emitter.TexturePath = filepath.Join(filepath.Dir(input), "sprite.png")
	c3cWrite(t, input, authored)
	npc := content.LevelNPCDef{ID: "npc", Name: "Guard", Kind: "guard", ClassName: "guard-class", ModelRef: "guard-model", Health: 35, TargetName: "guard-a", Target: "target", SquadName: "security", Weapons: 2, SpawnFlags: 3, SourceTag: "authored", Tags: []string{"friendly", "armed"}, Transform: content.LevelTransformDef{Position: content.Vec3{4, 5, 6}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{2, 2, 2}}}
	legacyApp, compiledApp := NewApp(), NewApp()
	legacyAssets, compiledAssets := c3d3Server(), c3d3Server()
	legacyParent := legacyApp.Commands().AddEntity(&TransformComponent{})
	compiledParent := compiledApp.Commands().AddEntity(&TransformComponent{})
	npc.AssetPath = filepath.Base(input)
	legacyNPC, err := spawnAuthoredLevelNPC(legacyApp.Commands(), legacyAssets, NewRuntimeContentLoader(), legacyParent, "level", filepath.Join(filepath.Dir(input), "level.gklevel"), npc)
	if err != nil {
		t.Fatal("JSON NPC baseline failed", err)
	}
	legacyApp.FlushCommands()
	legacyRoot, legacyItems := c3d4bItems(legacyApp.Commands())
	if err := os.RemoveAll(filepath.Dir(input)); err != nil {
		t.Fatal(err)
	}
	npc.AssetPath = filepath.Base(path)
	compiledNPC, err := spawnAuthoredLevelNPC(compiledApp.Commands(), compiledAssets, NewRuntimeContentLoader(), compiledParent, "level", filepath.Join(filepath.Dir(path), "level.gklevel"), npc)
	if err != nil {
		t.Fatal("compiled NPC must use source-free closure", err)
	}
	compiledApp.FlushCommands()
	compiledRoot, compiledItems := c3d4bItems(compiledApp.Commands())
	actual := *s3cComponent[NPCComponent](t, compiledApp.Commands(), compiledNPC)
	want := *s3cComponent[NPCComponent](t, legacyApp.Commands(), legacyNPC)
	want.AssetPath = npc.AssetPath
	if !reflect.DeepEqual(actual, want) || actual.Health != 35 || actual.MaxHealth != 35 || !reflect.DeepEqual(s3cComponent[NPCAnimationComponent](t, compiledApp.Commands(), compiledNPC), s3cComponent[NPCAnimationComponent](t, legacyApp.Commands(), legacyNPC)) || !reflect.DeepEqual(s3cComponent[AuthoredLevelNPCRefComponent](t, compiledApp.Commands(), compiledNPC), s3cComponent[AuthoredLevelNPCRefComponent](t, legacyApp.Commands(), legacyNPC)) {
		t.Fatal("compiled NPC changed health, animation state, authored reference or tags")
	}
	if s3cComponent[Parent](t, compiledApp.Commands(), compiledNPC).Entity != compiledParent || compiledRoot == 0 || s3cComponent[Parent](t, compiledApp.Commands(), compiledRoot).Entity != compiledNPC {
		t.Fatal("compiled NPC asset root lost level/NPC parent chain")
	}
	if !reflect.DeepEqual(s3cComponent[LocalTransformComponent](t, compiledApp.Commands(), compiledNPC), s3cComponent[LocalTransformComponent](t, legacyApp.Commands(), legacyNPC)) || len(compiledItems) != len(legacyItems) {
		t.Fatal("compiled NPC changed transform or multipart item set")
	}
	for _, part := range authored.Parts {
		entity, old := compiledItems[part.ID], legacyItems[part.ID]
		if entity == 0 || old == 0 || !reflect.DeepEqual(s3cComponent[LocalTransformComponent](t, compiledApp.Commands(), entity), s3cComponent[LocalTransformComponent](t, legacyApp.Commands(), old)) {
			t.Fatal("compiled NPC lost part/local transform", part.ID)
		}
		parent := compiledRoot
		if part.ParentID != "" {
			parent = compiledItems[part.ParentID]
		}
		if s3cComponent[Parent](t, compiledApp.Commands(), entity).Entity != parent {
			t.Fatal("compiled NPC lost group hierarchy", part.ID)
		}
		if part.Source.Kind != content.AssetSourceKindVoxelShape {
			continue
		}
		model := s3cComponent[VoxelModelComponent](t, compiledApp.Commands(), entity)
		oldModel := s3cComponent[VoxelModelComponent](t, legacyApp.Commands(), old)
		geometry, ok := compiledAssets.getVoxelGeometry(model.GeometryAsset())
		oldGeometry, oldOK := legacyAssets.getVoxelGeometry(oldModel.GeometryAsset())
		if !ok || !oldOK || !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(geometry.XBrickMap).Voxels), c3cGeometry(VoxelObjectSnapshotFromXBrickMap(oldGeometry.XBrickMap).Voxels)) || model.VoxelResolution != oldModel.VoxelResolution || model.CustomPivot != oldModel.CustomPivot || model.PivotMode != oldModel.PivotMode {
			t.Fatal("compiled NPC changed signed geometry/lattice/pivot", part.ID)
		}
		palette, _ := compiledAssets.GetVoxelPalette(model.VoxelPalette)
		oldPalette, _ := legacyAssets.GetVoxelPalette(oldModel.VoxelPalette)
		palette.SourcePath, oldPalette.SourcePath = "", ""
		if !reflect.DeepEqual(palette, oldPalette) {
			t.Fatal("compiled NPC changed material tables", part.ID)
		}
	}
	if !reflect.DeepEqual(s3cComponent[AuthoredMarkerComponent](t, compiledApp.Commands(), compiledItems["marker"]), s3cComponent[AuthoredMarkerComponent](t, legacyApp.Commands(), legacyItems["marker"])) || !reflect.DeepEqual(s3cComponent[AuthoredAssetAnimationSetComponent](t, compiledApp.Commands(), compiledRoot), s3cComponent[AuthoredAssetAnimationSetComponent](t, legacyApp.Commands(), legacyRoot)) {
		t.Fatal("compiled NPC lost marker/skeleton/rig and direct animation binding")
	}
}

func TestC3d4bCompiledNPCFailurePreservesCallerPinsAndExistingNPCPublication(t *testing.T) {
	for _, failure := range []string{"missing-animation", "closed-origin"} {
		t.Run(failure, func(t *testing.T) {
			_, path, _ := c3d3Fixture(t)
			owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
			caller := owner.NewScope()
			defer caller.Close()
			header, _, err := caller.Loader().LoadCompiledAssetHeader(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, ref := range header.Shapes {
				if _, _, err := caller.Loader().LoadCompiledAssetShape(filepath.Join(filepath.Dir(path), ref.Path)); err != nil {
					t.Fatal(err)
				}
			}
			loader := caller.Loader()
			if failure == "closed-origin" {
				closed := owner.NewScope()
				loader = closed.Loader()
				closed.Close()
			} else if err := os.Remove(filepath.Join(filepath.Dir(path), header.Asset.AnimationSetPaths[0])); err != nil {
				t.Fatal(err)
			}
			before := owner.Stats()
			app, assets := NewApp(), c3d3Server()
			parent := app.Commands().AddEntity(&TransformComponent{})
			entity, err := spawnAuthoredLevelNPC(app.Commands(), assets, loader, parent, "level", filepath.Join(filepath.Dir(path), "level.gklevel"), content.LevelNPCDef{ID: "npc", Name: "Guard", AssetPath: filepath.Base(path), Transform: content.LevelTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}})
			if err == nil || entity != 0 {
				t.Fatal("invalid compiled NPC closure succeeded")
			}
			app.FlushCommands()
			count := 0
			MakeQuery1[NPCComponent](app.Commands()).Map(func(entity EntityId, npc *NPCComponent) bool {
				count++
				if npc.Health != 1 || npc.MaxHealth != 1 || s3cComponent[Parent](t, app.Commands(), entity).Entity != parent {
					t.Fatal("failure changed existing NPC creation-before-load contract")
				}
				return true
			})
			root, items := c3d4bItems(app.Commands())
			if count != 1 || root != 0 || len(items) != 0 || len(assets.voxModels) != 0 || len(assets.voxPalettes) != 0 {
				t.Fatal("failed NPC published asset entities/geometry or erased existing NPC")
			}
			after := owner.Stats()
			if after.Entries != before.Entries || after.PinnedBytes != before.PinnedBytes || after.Bytes != before.Bytes {
				t.Fatal("failed NPC revoked caller pins or retained child loads")
			}
			if got, _, err := caller.Loader().LoadCompiledAssetHeader(path); err != nil || got != header {
				t.Fatal("accepted caller header lost", err)
			}
		})
	}
}
