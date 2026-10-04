package gekko

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

var c3g1Kinds = []string{"cube", "sphere", "cone", "pyramid", "cylinder", "capsule", "ramp"}

func c3g1Public(assets *AssetServer, kind string, params map[string]float32, scale float32) AssetId {
	switch kind {
	case "cube":
		return assets.CreateCubeModel(params["sx"], params["sy"], params["sz"], scale)
	case "sphere":
		return assets.CreateSphereModel(params["radius"], scale)
	case "cone":
		return assets.CreateConeModel(params["radius"], params["height"], scale)
	case "pyramid":
		return assets.CreatePyramidModel(params["size"], params["height"], scale)
	case "cylinder":
		return assets.CreateCylinderModel(params["radius"], params["height"], scale)
	case "capsule":
		return assets.CreateCapsuleModel(params["radius"], params["height"], scale)
	default:
		return assets.CreateRampModel(params["sx"], params["sy"], params["sz"], scale)
	}
}

func c3g1Params() map[string]float32 {
	return map[string]float32{"sx": 2.75, "sy": 3.25, "sz": 1.75, "radius": 1.6, "height": 4.25, "size": 3.75}
}

// Captured from the public constructors before pure-builder factoring. Hashes
// include declared dimensions, raw voxel order, duplicates and nil/empty slices.
var c3g1Golden = map[string]struct {
	hash       string
	dimensions [3]uint32
	count      int
}{
	"fractional/cube":     {"d946c32ca5408ade0d06edd8adace96989daaf0a8cc3998ce962400aebb7c6c4", [3]uint32{4, 4, 2}, 32},
	"fractional/sphere":   {"aa6db833af84e4ca2f8874caca3c9925abf49116cf731504a58fb4cbfdb47caa", [3]uint32{5, 5, 5}, 57},
	"fractional/cone":     {"b122dba6d3a49d9565cf74948624d56ebb4a9242e103bf06d7252106cfa71085", [3]uint32{5, 5, 6}, 50},
	"fractional/pyramid":  {"7207f6ac7c35205f908430ef1e13b5dbe895b73f5c82a5b01aea75b77d424abd", [3]uint32{5, 5, 6}, 78},
	"fractional/cylinder": {"1718ee7bb9c18f76dd20f5b56de782e9cac1f982ca286ee2dd54dce5e1edd438", [3]uint32{5, 5, 6}, 126},
	"fractional/capsule":  {"21194640107dac623845bff8d8e627234767b6e103ff276b9a26973c04a3d437", [3]uint32{5, 5, 6}, 78},
	"fractional/ramp":     {"9ef8fcdde35b3d70b2b0d37bcfe11355d2129f4f66ee478476ff13306961f17a", [3]uint32{4, 4, 2}, 20},
	"zero/cube":           {"c8803baa8efcd7013b863f0f783101f1a2c06c09dda274c48a9d371465effd15", [3]uint32{}, 0},
	"zero/sphere":         {"8e9077b078478e132971c1d5e5353e49a5bb16fab5b9e3e740d75c483becf02b", [3]uint32{1, 1, 1}, 1},
	"zero/cone":           {"69895a0d842cdf4fda2ed6319f0ae8cc1d676689bbbaf79cc78d4b8047381454", [3]uint32{1, 1, 0}, 0},
	"zero/pyramid":        {"c8803baa8efcd7013b863f0f783101f1a2c06c09dda274c48a9d371465effd15", [3]uint32{}, 0},
	"zero/cylinder":       {"69895a0d842cdf4fda2ed6319f0ae8cc1d676689bbbaf79cc78d4b8047381454", [3]uint32{1, 1, 0}, 0},
	"zero/capsule":        {"1705c281c8709f5a15dc1bb981470053cd3de637b107cb604e865783a42c449d", [3]uint32{1, 1, 2}, 2},
	"zero/ramp":           {"e202aeb74d0798b71529058b0521089f6dfe6d7ace5c98c9a6419555d1c075cd", [3]uint32{}, 0},
}

func c3g1AssertModel(t *testing.T, name string, model VoxModel) {
	t.Helper()
	golden := c3g1Golden[name]
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(encoded)) != golden.hash || [3]uint32{model.SizeX, model.SizeY, model.SizeZ} != golden.dimensions || len(model.Voxels) != golden.count {
		t.Fatal("primitive changed frozen raw ordered model", name)
	}
	for _, voxel := range model.Voxels {
		if voxel.ColorIndex != 1 {
			t.Fatal("primitive changed primary palette index", name)
		}
	}
}

func TestC3g1PublicAndPurePrimitivesPreserveFrozenModelsAndBounds(t *testing.T) {
	for _, variant := range []string{"fractional", "zero"} {
		for _, kind := range c3g1Kinds {
			t.Run(variant+"/"+kind, func(t *testing.T) {
				params := c3g1Params()
				if variant == "zero" {
					params = nil
				}
				assets := &AssetServer{}
				id := c3g1Public(assets, kind, params, 1.5)
				if warm := c3g1Public(assets, kind, params, 1.5); warm != id {
					t.Fatal("public primitive warm cache identity changed")
				}
				geometry, ok := assets.GetVoxelGeometry(id)
				if !ok {
					t.Fatal("public primitive geometry missing")
				}
				name := variant + "/" + kind
				c3g1AssertModel(t, name, geometry.VoxModel)
				d := c3g1Golden[name].dimensions
				if geometry.LocalMin != (mgl32.Vec3{}) || geometry.LocalMax != (mgl32.Vec3{float32(d[0]), float32(d[1]), float32(d[2])}) || geometry.BrickSize != ([3]uint32{8, 8, 8}) {
					t.Fatal("public primitive registration bounds changed")
				}
				pure, err := buildProceduralPrimitiveModel(kind, params, 1.5)
				if err != nil {
					t.Fatal(err)
				}
				c3g1AssertModel(t, name, pure)
				if !reflect.DeepEqual(pure, geometry.VoxModel) {
					t.Fatal("pure model differs from public frozen model")
				}
				if len(pure.Voxels) > 0 {
					again, err := buildProceduralPrimitiveModel(kind, params, 1.5)
					if err != nil {
						t.Fatal(err)
					}
					pure.Voxels[0].ColorIndex = 99
					c3g1AssertModel(t, name, again)
					c3g1AssertModel(t, name, geometry.VoxModel)
				}
			})
		}
	}
}

func TestC3g1AuthoredRuntimeDispatchPreservesGeometryPaletteAndPivot(t *testing.T) {
	for _, kind := range c3g1Kinds {
		t.Run(kind, func(t *testing.T) {
			assets, app := newSpawnTestAssetServer(), NewApp()
			def := content.NewAssetDef("procedural")
			material := content.AssetMaterialDef{ID: "material", Name: "Material", BaseColor: [4]uint8{20, 40, 60, 255}, Roughness: .25, Metallic: .75, IOR: 1.5, Emissive: .1}
			def.Materials = []content.AssetMaterialDef{material}
			def.Parts = []content.AssetPartDef{{ID: "part", Name: "Part", ModelScale: 1.5, VoxelResolution: .125, Source: content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: kind, Params: c3g1Params(), MaterialID: "material"}, Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}, Pivot: content.Vec3{.5, .25, 0}}}}
			publicID := c3g1Public(assets, kind, c3g1Params(), 1.5)
			result, err := SpawnAuthoredAsset(app.Commands(), assets, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
			if err != nil {
				t.Fatal(err)
			}
			app.FlushCommands()
			model := mustVoxelModelForSpawnTest(t, app.Commands(), result.EntitiesByAssetID["part"])
			if model.GeometryAsset() != publicID || model.PivotMode != PivotModeCenter || model.VoxelResolution != .125 {
				t.Fatal("authored dispatcher changed primitive sharing/pivot/resolution")
			}
			palette, ok := assets.GetVoxelPalette(model.VoxelPalette)
			if !ok || palette.VoxPalette[1] != material.BaseColor || palette.Roughness != material.Roughness || palette.Metalness != material.Metallic || palette.Emission != material.Emissive {
				t.Fatal("authored procedural material binding changed")
			}
			geometry, _ := assets.GetVoxelGeometry(model.GeometryAsset())
			c3g1AssertModel(t, "fractional/"+kind, geometry.VoxModel)
		})
	}
}

func TestC3g1UnsupportedPrimitiveAndNilServerRemainUnpublished(t *testing.T) {
	assets := newSpawnTestAssetServer()
	part := content.AssetPartDef{ID: "part", ModelScale: 1.5, Source: content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: "unsupported"}}
	want := `unsupported procedural primitive "unsupported"`
	if model, err := buildProceduralPrimitiveModel(part.Source.Primitive, nil, part.ModelScale); err == nil || err.Error() != want || !reflect.DeepEqual(model, VoxModel{}) {
		t.Fatal("unsupported pure primitive must retain exact error and zero model")
	}
	if model, palette, err := modelAndPaletteFromSource(assets, content.NewAssetDef("invalid"), part, ""); err == nil || err.Error() != want || model != (AssetId{}) || palette != (AssetId{}) || len(assets.voxModels) != 0 || len(assets.voxPalettes) != 0 {
		t.Fatal("unsupported authored primitive published assets or changed error")
	}
	if model, palette, err := modelAndPaletteFromSource(nil, nil, part, ""); err != nil || model != (AssetId{}) || palette != (AssetId{}) {
		t.Fatal("nil-server authored source validation order changed")
	}
}
