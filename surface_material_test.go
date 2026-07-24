package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func TestSurfaceMaterialForRaycastHitResolvesImportedWorldMaterial(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	assets := &AssetServer{}
	palette := ImportedWorldPaletteAsset(assets, &content.ImportedWorldDef{
		MaterialPalette: []content.ImportedWorldPaletteColor{
			{}, {}, {}, {}, {120, 130, 140, 255},
		},
		Materials: []content.ImportedWorldMaterialDef{{
			PaletteIndex: 4,
			Kind:         " Metal ",
			Tags:         []string{" Material:Metal ", "material:metal"},
		}},
	})
	entity := cmd.AddEntity(&VoxelModelComponent{VoxelPalette: palette})
	app.FlushCommands()

	facts, ok := SurfaceMaterialForRaycastHit(cmd, assets, RaycastHit{Hit: true, Entity: entity, PaletteIndex: 4})
	if !ok || facts.Kind != "metal" || facts.PaletteValue != 4 || !reflect.DeepEqual(facts.Tags, []string{"material:metal"}) {
		t.Fatalf("imported-world surface facts = %+v, ok=%t", facts, ok)
	}
}

func TestImportedWorldSurfaceMaterialPrefersRuntimeFactsOverSourceProvenance(t *testing.T) {
	materials := importedWorldSurfaceMaterials(&content.ImportedWorldDef{
		SourceMaterials: []content.ImportedWorldMaterialDef{{PaletteIndex: 7, Kind: "metal", Tags: []string{"material:metal"}}},
		Materials:       []content.ImportedWorldMaterialDef{{PaletteIndex: 7, Kind: "computer", Tags: []string{"material:computer"}}},
	})

	if got := materials[7]; got.Kind != "computer" || !reflect.DeepEqual(got.Tags, []string{"material:computer"}) {
		t.Fatalf("source palette provenance replaced runtime gameplay facts: %+v", got)
	}
}

func TestSurfaceMaterialForRaycastHitResolvesAuthoredVoxelAssets(t *testing.T) {
	tests := []struct {
		name         string
		source       content.AssetSourceDef
		paletteValue uint8
	}{
		{
			name: "voxel shape",
			source: content.AssetSourceDef{
				Kind: content.AssetSourceKindVoxelShape,
				VoxelShape: &content.AssetVoxelShapeDef{
					Palette: []content.AssetVoxelPaletteEntryDef{{Value: 7, MaterialID: "surface"}},
					Voxels:  []content.VoxelObjectVoxelDef{{Value: 7}},
				},
			},
			paletteValue: 7,
		},
		{
			name: "procedural primitive",
			source: content.AssetSourceDef{
				Kind:       content.AssetSourceKindProceduralPrimitive,
				Primitive:  "cube",
				Params:     map[string]float32{"sx": 1, "sy": 1, "sz": 1},
				MaterialID: "surface",
			},
			paletteValue: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			assets := &AssetServer{}
			def := content.NewAssetDef(test.name)
			def.Materials = []content.AssetMaterialDef{{
				ID:        "surface",
				Name:      "Surface",
				BaseColor: [4]uint8{100, 110, 120, 255},
				Roughness: 0.5,
				IOR:       1.5,
				Tags:      []string{" Kind:Metal ", "material:metal", "MATERIAL:METAL"},
			}}
			def.Parts = []content.AssetPartDef{{
				ID:        "part",
				Name:      "Part",
				Source:    test.source,
				Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
			}}

			spawned, err := SpawnAuthoredAsset(cmd, assets, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
			if err != nil {
				t.Fatalf("SpawnAuthoredAsset: %v", err)
			}
			facts, ok := SurfaceMaterialForRaycastHit(cmd, assets, RaycastHit{Hit: true, Entity: spawned.EntitiesByAssetID["part"], PaletteIndex: test.paletteValue})
			if !ok || facts.Kind != "metal" || facts.PaletteValue != test.paletteValue || !reflect.DeepEqual(facts.Tags, []string{"kind:metal", "material:metal"}) {
				t.Fatalf("authored surface facts = %+v, ok=%t", facts, ok)
			}
		})
	}
}

func TestSurfaceMaterialForRaycastHitReportsMissingMetadata(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	assets := &AssetServer{}
	entity := cmd.AddEntity(&VoxelModelComponent{VoxelPalette: assets.CreateSimplePalette([4]uint8{255, 255, 255, 255})})
	app.FlushCommands()

	if facts, ok := SurfaceMaterialForRaycastHit(cmd, assets, RaycastHit{Hit: true, Entity: entity, PaletteIndex: 1}); ok {
		t.Fatalf("missing surface metadata resolved as %+v", facts)
	}
}

func TestVoxelPaletteCacheIncludesSurfaceMaterials(t *testing.T) {
	assets := &AssetServer{}
	base := VoxelPaletteAsset{}
	base.VoxPalette[1] = [4]uint8{100, 110, 120, 255}
	metal := base
	metal.SurfaceMaterials = map[uint8]VoxelSurfaceMaterial{1: {Kind: "metal"}}
	stone := base
	stone.SurfaceMaterials = map[uint8]VoxelSurfaceMaterial{1: {Kind: "stone"}}

	if assets.CreateVoxelPaletteAsset(metal) == assets.CreateVoxelPaletteAsset(stone) {
		t.Fatal("voxel palette cache merged distinct surface semantics")
	}
}
