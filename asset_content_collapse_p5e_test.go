package gekko

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Reflection keeps the pre-implementation failure on the missing public
// observation, while the fixtures still exercise real authored spawning.
func p5eCollapseStats(t *testing.T, assets *AssetServer) (uint64, uint64) {
	t.Helper()
	method := reflect.ValueOf(assets).MethodByName("AuthoredVoxelCollapseStats")
	if !method.IsValid() {
		t.Fatal("missing public AssetServer.AuthoredVoxelCollapseStats observation")
	}
	result := method.Call(nil)
	if len(result) != 1 || result[0].Type().Name() != "AuthoredVoxelCollapseStats" {
		t.Fatal("expected named AuthoredVoxelCollapseStats value")
	}
	builds, hits := result[0].FieldByName("Builds"), result[0].FieldByName("Hits")
	if !builds.IsValid() || !hits.IsValid() || builds.Kind() != reflect.Uint64 || hits.Kind() != reflect.Uint64 {
		t.Fatal("collapse stats must expose uint64 Builds and Hits")
	}
	return builds.Uint(), hits.Uint()
}

func p5eShapeDef() *content.AssetDef {
	def := content.NewAssetDef("p5e-shape")
	def.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true}
	def.Materials = []content.AssetMaterialDef{{ID: "red", Name: "Red", BaseColor: [4]uint8{255, 40, 30, 255}, Roughness: 1, IOR: 1.5}}
	for i, id := range []string{"left", "right"} {
		def.Parts = append(def.Parts, content.AssetPartDef{
			ID: id, Name: id, VoxelResolution: 1, ModelScale: 1,
			Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
			Source: content.AssetSourceDef{Kind: content.AssetSourceKindVoxelShape, VoxelShape: &content.AssetVoxelShapeDef{
				Palette: []content.AssetVoxelPaletteEntryDef{{Value: 1, MaterialID: "red"}},
				Voxels:  []content.VoxelObjectVoxelDef{{X: i, Value: 1}},
			}},
		})
	}
	return def
}

func p5eSpawn(t *testing.T, app *App, assets *AssetServer, def *content.AssetDef, mode VoxelPartCollapseMode) (AuthoredAssetSpawnResult, error) {
	t.Helper()
	result, err := SpawnAuthoredAssetWithOptions(app.Commands(), assets, def,
		TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		AuthoredAssetSpawnOptions{CollapseVoxelParts: mode})
	app.FlushCommands()
	return result, err
}

func p5eCollapsedModel(t *testing.T, app *App, result AuthoredAssetSpawnResult) VoxelModelComponent {
	t.Helper()
	if !result.Collapsed || len(result.CollapsedPartIDs) != 2 || len(result.PartIDs) != 2 {
		t.Fatalf("expected both authored parts collapsed, got %+v", result)
	}
	for _, id := range []string{"left", "right"} {
		if _, ok := result.CollapsedPartIDs[id]; !ok {
			t.Fatalf("missing collapsed part %s", id)
		}
	}
	return mustVoxelModelForSpawnTest(t, app.Commands(), onlyVoxelEntityForSpawnTest(t, app.Commands(), result.RootEntity))
}

func TestP5eWarmCollapseReusePreservesMutableCompositeAndRebuilds(t *testing.T) {
	app, assets, def := NewApp(), newSpawnTestAssetServer(), p5eShapeDef()
	first, err := p5eSpawn(t, app, assets, def, VoxelPartCollapseForce)
	if err != nil {
		t.Fatal(err)
	}
	model := p5eCollapsedModel(t, app, first)
	geometry, ok := assets.GetVoxelGeometry(model.GeometryAsset())
	if !ok || geometry.XBrickMap.GetVoxelCount() != 2 || model.VoxelResolution != 1 {
		t.Fatal("cold collapse must preserve shape samples and resolution")
	}
	geometry.XBrickMap.SetVoxel(23, 2, 1, 7)
	for i := 0; i < 2; i++ {
		spawn, err := p5eSpawn(t, app, assets, def, VoxelPartCollapseForce)
		if err != nil {
			t.Fatal(err)
		}
		warm := p5eCollapsedModel(t, app, spawn)
		if warm.GeometryAsset() != model.GeometryAsset() || warm.VoxelPalette != model.VoxelPalette || warm.VoxelResolution != model.VoxelResolution {
			t.Fatal("warm collapse changed geometry, palette or resolution")
		}
		live, _ := assets.GetVoxelGeometry(warm.GeometryAsset())
		if found, value := live.XBrickMap.GetVoxel(23, 2, 1); !found || value != 7 {
			t.Fatal("warm reuse must preserve public edits to cached composite")
		}
	}
	def.ID = "p5e-distinct"
	distinct, err := p5eSpawn(t, app, assets, def, VoxelPartCollapseForce)
	if err != nil {
		t.Fatal(err)
	}
	distinctModel := p5eCollapsedModel(t, app, distinct)
	if distinctModel.GeometryAsset() == model.GeometryAsset() {
		t.Fatal("distinct key reused old composite")
	}
	def.ID = "p5e-shape"
	if !assets.DeleteVoxelGeometry(model.GeometryAsset()) {
		t.Fatal("delete cached geometry failed")
	}
	rebuilt, err := p5eSpawn(t, app, assets, def, VoxelPartCollapseForce)
	if err != nil {
		t.Fatal(err)
	}
	newModel := p5eCollapsedModel(t, app, rebuilt)
	newGeometry, _ := assets.GetVoxelGeometry(newModel.GeometryAsset())
	if newModel.GeometryAsset() == model.GeometryAsset() || newGeometry.XBrickMap.GetVoxelCount() != 2 {
		t.Fatal("deleted cache entry did not cold rebuild")
	}
	if builds, hits := p5eCollapseStats(t, assets); builds != 3 || hits != 2 {
		t.Fatalf("stats builds=%d hits=%d, want 3/2", builds, hits)
	}
	if builds, hits := p5eCollapseStats(t, assets); builds != 3 || hits != 2 {
		t.Fatal("stats reads changed counters")
	}
	if builds, hits := p5eCollapseStats(t, nil); builds != 0 || hits != 0 {
		t.Fatal("nil server stats must be zero")
	}
}

// Public registration primes the exact authored key for definitions which
// cannot themselves complete a cold collapse. It grants no validation bypass.
func p5ePrimeComposite(t *testing.T, assets *AssetServer, def *content.AssetDef) {
	t.Helper()
	content.NormalizeAssetDef(def)
	key, err := collapseGeometryCacheKey(def, "", def.Parts[0].VoxelResolution)
	if err != nil {
		t.Fatal(err)
	}
	xbm := volume.NewXBrickMap()
	xbm.SetVoxel(0, 0, 0, 1)
	assets.RegisterSharedVoxelGeometryWithCacheKey(key, xbm, key)
}

func TestP5eWarmCollapseValidatesCurrentSourcesAndErrorPrecedence(t *testing.T) {
	for _, name := range []string{"source", "palette", "resolution", "samples-before-scale", "scale"} {
		t.Run(name, func(t *testing.T) {
			app, assets, def := NewApp(), newSpawnTestAssetServer(), p5eShapeDef()
			want := ""
			switch name {
			case "source":
				def.Parts[0].Source = content.AssetSourceDef{Kind: content.AssetSourceKindVoxModel, Path: t.TempDir() + "/missing.vox"}
				want = "missing.vox"
			case "palette":
				def.Materials = append(def.Materials, content.AssetMaterialDef{ID: "blue", Name: "Blue", BaseColor: [4]uint8{20, 30, 255, 255}, Roughness: 1, IOR: 1.5})
				def.Parts[1].Source.VoxelShape.Palette[0].MaterialID = "blue"
				def.Parts[1].VoxelResolution = 2 // Palette failure still precedes resolution.
				want = "voxel collapse requires compatible palettes"
			case "resolution":
				def.Parts[1].VoxelResolution = 2
				want = "voxel collapse requires matching voxel_resolution"
			case "samples-before-scale", "scale":
				def.Parts[0].Transform.Scale = content.Vec3{0, 1, 1}
				want = "collapse bake failed for part left: part left has zero voxel scale"
				if name == "samples-before-scale" {
					spawn, err := p5eSpawn(t, app, assets, def, VoxelPartCollapseDisable)
					if err != nil {
						t.Fatal(err)
					}
					source := mustSpawnedVoxelAssetForTest(t, app.Commands(), assets, spawn, "left")
					for _, sector := range source.XBrickMap.Sectors {
						for _, brick := range sector.PackedBricks {
							brick.Payload = [volume.BrickSize][volume.BrickSize][volume.BrickSize]uint8{}
							brick.Flags |= volume.BrickFlagSolid
						}
					}
					if source.XBrickMap.GetVoxelCount() == 0 {
						t.Fatal("fixture must look nonempty to occupancy counting")
					}
					want = "collapse bake failed for part left: part left has no voxel geometry"
				}
			}
			p5ePrimeComposite(t, assets, def)
			_, err := p5eSpawn(t, app, assets, def, VoxelPartCollapseForce)
			if err == nil || (name == "source" && !strings.Contains(err.Error(), want)) || (name != "source" && err.Error() != want) {
				t.Fatalf("forced error=%v, want %q", err, want)
			}
			fallback, fallbackErr := p5eSpawn(t, app, assets, def, VoxelPartCollapseDefault)
			if name == "source" {
				if fallbackErr == nil {
					t.Fatal("missing source must also fail normal spawning")
				}
			} else if fallbackErr != nil || fallback.Collapsed {
				t.Fatalf("automatic collapse must fall back: collapsed=%v err=%v", fallback.Collapsed, fallbackErr)
			}
			if builds, hits := p5eCollapseStats(t, assets); builds != 0 || hits != 0 {
				t.Fatalf("invalid warm reuse counted build/hit: %d/%d", builds, hits)
			}
		})
	}
}

func TestP5eWarmCollapseAcceptsEmptyOutputAndZeroColorModelSamples(t *testing.T) {
	for _, name := range []string{"full-subtraction", "zero-color-model", "empty-part-before-scale"} {
		t.Run(name, func(t *testing.T) {
			app, assets, def := NewApp(), newSpawnTestAssetServer(), p5eShapeDef()
			wantVoxels := 0
			if name == "empty-part-before-scale" {
				def.Parts[1].Transform.Scale = content.Vec3{0, 1, 1}
				spawn, err := p5eSpawn(t, app, assets, def, VoxelPartCollapseDisable)
				if err != nil {
					t.Fatal(err)
				}
				source := mustSpawnedVoxelAssetForTest(t, app.Commands(), assets, spawn, "right")
				source.XBrickMap.SetVoxel(1, 0, 0, 0)
				if source.XBrickMap.GetVoxelCount() != 0 {
					t.Fatal("right source must be empty")
				}
				wantVoxels = 1
			} else if name == "full-subtraction" {
				def.Parts[1].Source.VoxelShape.Voxels[0].X = 0
				def.Parts[1].Source.Operation = content.AssetShapeOperationSubtract
			} else {
				for i := range def.Parts {
					def.Parts[i].Source = testProceduralPartSource()
				}
				spawn, err := p5eSpawn(t, app, assets, def, VoxelPartCollapseDisable)
				if err != nil {
					t.Fatal(err)
				}
				source := mustSpawnedVoxelAssetForTest(t, app.Commands(), assets, spawn, "left")
				if len(source.VoxModel.Voxels) == 0 {
					t.Fatal("fixture needs model samples")
				}
				for i := range source.VoxModel.Voxels {
					source.VoxModel.Voxels[i].ColorIndex = 0
				}
			}
			var first AssetId
			for i := 0; i < 2; i++ {
				spawn, err := p5eSpawn(t, app, assets, def, VoxelPartCollapseForce)
				if err != nil {
					t.Fatal(err)
				}
				model := p5eCollapsedModel(t, app, spawn)
				geometry, _ := assets.GetVoxelGeometry(model.GeometryAsset())
				if got := geometry.XBrickMap.GetVoxelCount(); got != wantVoxels {
					t.Fatalf("composite voxels=%d, want %d", got, wantVoxels)
				}
				if i == 0 {
					first = model.GeometryAsset()
				} else if model.GeometryAsset() != first {
					t.Fatal("accepted composite must reuse cached ID")
				}
			}
			if builds, hits := p5eCollapseStats(t, assets); builds != 1 || hits != 1 {
				t.Fatalf("accepted composite stats=%d/%d, want 1/1", builds, hits)
			}
		})
	}
}
