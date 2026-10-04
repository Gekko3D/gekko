package gekko

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
)

var c3d4cKinds = []string{"moving", "charger", "breakable", "pickup"}

func c3d4cFixture(t *testing.T) (string, string) {
	t.Helper()
	input, path, asset := c3d3Fixture(t)
	second := *asset.Parts[2].Source.VoxelShape
	second.Voxels = []content.VoxelObjectVoxelDef{{X: 40, Y: -2, Value: 3}}
	asset.Parts[2].Source.VoxelShape = &second
	c3cWrite(t, input, asset)
	if _, err := CompileAuthoredAsset(input, path, nil); err != nil {
		t.Fatal(err)
	}
	return input, path
}

func c3d4cSpawn(kind string, cmd *Commands, assets *AssetServer, loader *RuntimeContentLoader, parent EntityId, path string) (EntityId, error) {
	levelPath := filepath.Join(filepath.Dir(path), "level.gklevel")
	ref := filepath.Base(path)
	center, half := content.Vec3{4, 5, 6}, content.Vec3{1, 2, 3}
	switch kind {
	case "moving":
		return spawnAuthoredLevelMovingBrush(cmd, assets, loader, parent, "level", levelPath, content.LevelMovingBrushDef{ID: kind, Name: "Door", AssetPath: ref, BoundsCenter: center, BoundsHalfExtents: half, MoveDirection: content.Vec3{2, 0, 0}, MoveDistance: 7, Speed: 2, Wait: 3, Lip: 1, Target: "target", Tags: []string{"door"}})
	case "charger":
		return spawnAuthoredLevelCharger(cmd, assets, loader, parent, "level", levelPath, content.LevelChargerDef{ID: kind, Name: "Charger", AssetPath: ref, BoundsCenter: center, BoundsHalfExtents: half, ChargeKind: "armor", Capacity: 90, Rate: 4, Tags: []string{"supply"}})
	case "breakable":
		return spawnAuthoredLevelBreakable(cmd, assets, loader, parent, "level", levelPath, content.LevelBreakableDef{ID: kind, Name: "Glass", AssetPath: ref, BoundsCenter: center, BoundsHalfExtents: half, Health: 25, Material: "glass", SpawnObject: "debris", Target: "target", Tags: []string{"fragile"}})
	default:
		return spawnAuthoredLevelPickup(cmd, assets, loader, parent, "level", levelPath, content.LevelPickupDef{ID: kind, Name: "Ammo", AssetPath: ref, Category: "ammo", Item: "rounds", Amount: 15, Tags: []string{"supply"}, Transform: content.LevelTransformDef{Position: center, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}})
	}
}

func c3d4cState(t *testing.T, kind string, cmd *Commands, entity EntityId) []any {
	t.Helper()
	switch kind {
	case "moving":
		return []any{s3cComponent[MovingBrushComponent](t, cmd, entity), s3cComponent[AuthoredLevelMovingBrushRefComponent](t, cmd, entity)}
	case "charger":
		return []any{s3cComponent[ChargerComponent](t, cmd, entity), s3cComponent[AuthoredLevelChargerRefComponent](t, cmd, entity)}
	case "breakable":
		return []any{s3cComponent[BreakableComponent](t, cmd, entity), s3cComponent[AuthoredLevelBreakableRefComponent](t, cmd, entity), s3cComponent[AABBComponent](t, cmd, entity), s3cComponent[NavigationBlockerComponent](t, cmd, entity)}
	default:
		pickup := *s3cComponent[PickupComponent](t, cmd, entity)
		pickup.AssetPath = ""
		return []any{pickup, s3cComponent[AuthoredLevelPickupRefComponent](t, cmd, entity)}
	}
}

func TestC3d4cSingleModelConsumersPreserveJSONFirstPartSemantics(t *testing.T) {
	input, path := c3d4cFixture(t)
	legacyApp, compiledApp := NewApp(), NewApp()
	legacyAssets, compiledAssets := c3d3Server(), c3d3Server()
	legacyParent := legacyApp.Commands().AddEntity(&TransformComponent{})
	compiledParent := compiledApp.Commands().AddEntity(&TransformComponent{})
	legacy := make(map[string]EntityId)
	for _, kind := range c3d4cKinds {
		entity, err := c3d4cSpawn(kind, legacyApp.Commands(), legacyAssets, NewRuntimeContentLoader(), legacyParent, input)
		if err != nil {
			t.Fatal("JSON baseline", kind, err)
		}
		legacy[kind] = entity
	}
	legacyApp.FlushCommands()
	if err := os.RemoveAll(filepath.Dir(input)); err != nil {
		t.Fatal(err)
	}
	loader := NewRuntimeContentLoader()
	var shared AssetId
	for _, kind := range c3d4cKinds {
		entity, err := c3d4cSpawn(kind, compiledApp.Commands(), compiledAssets, loader, compiledParent, path)
		if err != nil {
			t.Fatal("compiled source-free consumer", kind, err)
		}
		compiledApp.FlushCommands()
		old := legacy[kind]
		if !reflect.DeepEqual(c3d4cState(t, kind, compiledApp.Commands(), entity), c3d4cState(t, kind, legacyApp.Commands(), old)) || !reflect.DeepEqual(s3cComponent[LocalTransformComponent](t, compiledApp.Commands(), entity), s3cComponent[LocalTransformComponent](t, legacyApp.Commands(), old)) || s3cComponent[Parent](t, compiledApp.Commands(), entity).Entity != compiledParent {
			t.Fatal("consumer metadata/motion/refs/transform changed", kind)
		}
		model := s3cComponent[VoxelModelComponent](t, compiledApp.Commands(), entity)
		oldModel := s3cComponent[VoxelModelComponent](t, legacyApp.Commands(), old)
		geometry, ok := compiledAssets.getVoxelGeometry(model.GeometryAsset())
		oldGeometry, oldOK := legacyAssets.getVoxelGeometry(oldModel.GeometryAsset())
		if !ok || !oldOK || !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(geometry.XBrickMap).Voxels), c3cGeometry(VoxelObjectSnapshotFromXBrickMap(oldGeometry.XBrickMap).Voxels)) || model.PivotMode != PivotModeCorner || model.PivotMode != oldModel.PivotMode || model.ShadowSeamWorldEpsilon != oldModel.ShadowSeamWorldEpsilon || model.VoxelResolution != oldModel.VoxelResolution {
			t.Fatal("first-part signed postscale geometry/corner pivot/seam changed", kind)
		}
		palette, _ := compiledAssets.GetVoxelPalette(model.VoxelPalette)
		oldPalette, _ := legacyAssets.GetVoxelPalette(oldModel.VoxelPalette)
		palette.SourcePath, oldPalette.SourcePath = "", ""
		if !reflect.DeepEqual(palette, oldPalette) {
			t.Fatal("first-part material changed", kind)
		}
		if shared == (AssetId{}) {
			shared = model.GeometryAsset()
		} else if shared != model.GeometryAsset() {
			t.Fatal("warm consumers failed to share first-part geometry")
		}
	}
	header, _, err := content.LoadCompiledAssetHeader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range header.Shapes {
		if ref.PartID == "duplicate" {
			if _, exists := compiledAssets.SharedVoxelGeometryByCacheKey("compiled-asset-shape:" + ref.ContentID); exists {
				t.Fatal("single-model consumer registered unused distinct second shape")
			}
		}
	}
	if len(compiledAssets.voxModels) != 1 || len(compiledAssets.voxPalettes) != 1 {
		t.Fatal("single-model consumers published unused parts/palettes")
	}
}

func TestC3d4cGroupFirstNilServerAndEmptyAsset(t *testing.T) {
	for _, variant := range []string{"group-first", "nil-server", "empty", "empty-nil-server"} {
		t.Run(variant, func(t *testing.T) {
			input, path, asset := c3d3Fixture(t)
			if variant == "group-first" {
				asset.Parts[0], asset.Parts[1] = asset.Parts[1], asset.Parts[0]
			}
			if variant == "empty" || variant == "empty-nil-server" {
				asset.Parts, asset.Markers, asset.Emitters, asset.Skeleton, asset.AnimationSetPaths = nil, nil, nil, nil, nil
				asset.DefaultAnimationClipID = ""
			}
			c3cWrite(t, input, asset)
			if _, err := CompileAuthoredAsset(input, path, nil); err != nil {
				t.Fatal(err)
			}
			app, assets := NewApp(), c3d3Server()
			if variant == "nil-server" || variant == "empty-nil-server" {
				assets = nil
			}
			entity, err := c3d4cSpawn("charger", app.Commands(), assets, nil, 0, path)
			if variant == "empty" {
				if err == nil || entity != 0 {
					t.Fatal("empty first-part asset must retain legacy no-parts error")
				}
				return
			}
			if err != nil || entity == 0 {
				t.Fatal("metadata-only single-model consumer rejected", err)
			}
			app.FlushCommands()
			if hasComponentOfType[VoxelModelComponent](app.Commands(), entity) || assets != nil && (len(assets.voxModels) != 0 || len(assets.voxPalettes) != 0) {
				t.Fatal("group-first/nil server must not select later shape or publish geometry")
			}
		})
	}
}

func TestC3d4cPickupMissingInputOnlyAndCompleteClosureProof(t *testing.T) {
	for _, failure := range []string{"missing-input", "corrupt-header", "missing-shape", "missing-animation", "bad-unused-shape", "nil-server-missing-animation"} {
		t.Run(failure, func(t *testing.T) {
			_, path := c3d4cFixture(t)
			owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
			caller := owner.NewScope()
			defer caller.Close()
			header, _, err := content.LoadCompiledAssetHeader(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, ref := range header.Shapes {
				if _, _, err := caller.Loader().LoadCompiledAssetShape(filepath.Join(filepath.Dir(path), ref.Path)); err != nil {
					t.Fatal(err)
				}
			}
			switch failure {
			case "missing-input":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "corrupt-header":
				if err := os.WriteFile(path, []byte("bad compiled header"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-shape", "bad-unused-shape":
				index := 0
				partID := "shape"
				if failure == "bad-unused-shape" {
					partID = "duplicate"
				}
				for i, ref := range header.Shapes {
					if ref.PartID == partID {
						index = i
					}
				}
				header.Shapes[index].Path = "shapes/absent.gkshape"
				if _, err := content.SaveCompiledAssetHeader(path, header, nil); err != nil {
					t.Fatal(err)
				}
			case "missing-animation", "nil-server-missing-animation":
				header.Asset.AnimationSetPaths[0] = "dependencies/absent.gkanim"
				if _, err := content.SaveCompiledAssetHeader(path, header, nil); err != nil {
					t.Fatal(err)
				}
			}
			before := owner.Stats()
			app, assets := NewApp(), c3d3Server()
			if failure == "nil-server-missing-animation" {
				assets = nil
			}
			kind := "pickup"
			if failure == "bad-unused-shape" {
				kind = "moving"
			}
			entity, err := c3d4cSpawn(kind, app.Commands(), assets, owner, 0, path)
			if failure == "missing-input" {
				if err != nil || entity == 0 {
					t.Fatal("missing selected pickup input must remain tolerated", err)
				}
			} else if err == nil || entity != 0 {
				t.Fatal("present compiled header with invalid dependency must fail")
			}
			if assets != nil && (len(assets.voxModels) != 0 || len(assets.voxPalettes) != 0) {
				t.Fatal("invalid unused dependency published first-part assets")
			}
			after := owner.Stats()
			if after.Entries != before.Entries || after.PinnedBytes != before.PinnedBytes || after.Bytes != before.Bytes {
				t.Fatal("failed closure proof revoked accepted caller shape pins or leaked loads")
			}
		})
	}
}
