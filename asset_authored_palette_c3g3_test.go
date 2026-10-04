package gekko

import (
	"encoding/json"
	"reflect"

	"testing"

	"github.com/gekko3d/gekko/content"
)

func c3g3Inputs() (*content.AssetDef, content.AssetPartDef, VoxPalette, []VoxMaterial, VoxModel) {
	material := content.AssetMaterialDef{ID: "m", BaseColor: [4]uint8{11, 22, 33, 44}, Roughness: .2, Metallic: .3, Emissive: .4, IOR: 1.7, Transparency: .6, Tags: []string{" KIND:Stone ", "Solid", "solid", "", " Impact "}}
	def := &content.AssetDef{Materials: []content.AssetMaterialDef{material}}
	part := content.AssetPartDef{ID: "p", Source: content.AssetSourceDef{MaterialID: "m"}, ModelScale: .25}
	var palette VoxPalette
	palette[0] = [4]uint8{1, 2, 3, 4}
	palette[3] = [4]uint8{5, 6, 7, 8}
	palette[9] = [4]uint8{10, 20, 30, 40}
	materials := []VoxMaterial{{ID: 3, Type: 2, Weight: .7, Property: map[string]interface{}{"_rough": float32(.25), "name": "original"}}}
	model := VoxModel{SizeX: 2, SizeY: 1, SizeZ: 1, Voxels: []Voxel{{ColorIndex: 0}, {ColorIndex: 3}, {X: 1, ColorIndex: 9}, {X: 1, ColorIndex: 3}}}
	return def, part, palette, materials, model
}

func c3g3MaterialExpected() VoxelPaletteAsset {
	var palette VoxPalette
	for i := range palette {
		palette[i] = [4]uint8{11, 22, 33, 44}
	}
	return VoxelPaletteAsset{VoxPalette: palette, IsPBR: true, Roughness: .2, Metalness: .3, Emission: .4, IOR: 1.7, Transparency: .6, SurfaceMaterials: map[uint8]VoxelSurfaceMaterial{1: {Kind: "stone", Tags: []string{"kind:stone", "solid", "impact"}}}}
}

func c3g3JSONEqual(t *testing.T, got, want VoxelPaletteAsset) {
	t.Helper()
	a, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("palette JSON differs:\ngot %s\nwant %s", a, b)
	}
}

func TestC3g3HistoricalPaletteAssetsAndCacheDomains(t *testing.T) {
	def, part, palette, materials, model := c3g3Inputs()
	assets := &AssetServer{}
	materialID := createAuthoredMaterialVoxelPalette(assets, def.Materials[0])
	got, _ := assets.GetVoxelPalette(materialID)
	want := c3g3MaterialExpected()
	c3g3JSONEqual(t, got, want)
	proceduralID, err := authoredProceduralPalette(assets, def, part)
	if err != nil || proceduralID != materialID || assets.voxPaletteKeys[voxelPaletteAssetCacheKey(want)] != materialID {
		t.Fatal("authored material warm key changed", err)
	}
	part.Source.MaterialID = ""
	defaultID, err := authoredProceduralPalette(assets, nil, part)
	if err != nil {
		t.Fatal(err)
	}
	var white VoxPalette
	for i := range white {
		white[i] = [4]uint8{255, 255, 255, 255}
	}
	defaultWant := VoxelPaletteAsset{VoxPalette: white, IsPBR: true, Roughness: 1, IOR: 1.5}
	got, _ = assets.GetVoxelPalette(defaultID)
	c3g3JSONEqual(t, got, defaultWant)
	if assets.voxPaletteKeys[voxelPaletteAssetCacheKey(defaultWant)] != defaultID {
		t.Fatal("default procedural asset key changed")
	}
	rawID, err := authoredVoxFilePalette(assets, nil, part, palette, materials, model, "source.vox")
	if err != nil {
		t.Fatal(err)
	}
	rawWant := VoxelPaletteAsset{VoxPalette: palette, Materials: materials, SourcePath: "source.vox"}
	got, _ = assets.GetVoxelPalette(rawID)
	c3g3JSONEqual(t, got, rawWant)
	if assets.voxPaletteKeys[voxelPaletteCacheKey(palette, materials, "source.vox")] != rawID {
		t.Fatal("plain VOX source key changed")
	}
	again, err := authoredVoxFilePalette(assets, nil, part, palette, materials, model, "source.vox")
	if err != nil || again != rawID {
		t.Fatal("plain VOX warm reuse", err)
	}
	part.Source.MaterialID = "m"
	overrideID, err := authoredVoxFilePalette(assets, def, part, palette, materials, model, "source.vox")
	if err != nil {
		t.Fatal(err)
	}
	facts := VoxelSurfaceMaterial{Kind: "stone", Tags: []string{"kind:stone", "solid", "impact"}}
	overrideWant := rawWant
	overrideWant.SurfaceMaterials = map[uint8]VoxelSurfaceMaterial{3: facts, 9: facts}
	got, _ = assets.GetVoxelPalette(overrideID)
	c3g3JSONEqual(t, got, overrideWant)
	if overrideID == rawID || assets.voxPaletteKeys[voxelPaletteAssetCacheKey(overrideWant)] != overrideID {
		t.Fatal("VOX override cache domain changed")
	}
	again, err = authoredVoxFilePalette(assets, def, part, palette, materials, model, "source.vox")
	if err != nil || again != overrideID {
		t.Fatal("override warm reuse", err)
	}
	materials[0].Property["name"] = "edited"
	got, _ = assets.GetVoxelPalette(rawID)
	if got.Materials[0].Property["name"] != "edited" {
		t.Fatal("plain VOX no longer borrows material map")
	}
	got, _ = assets.GetVoxelPalette(overrideID)
	if got.Materials[0].Property["name"] != "edited" {
		t.Fatal("override VOX no longer borrows material map")
	}
	before := len(assets.voxPalettes)
	part.Source.MaterialID = "missing"
	if id, err := authoredProceduralPalette(assets, def, part); err == nil || err.Error() != "missing material missing for part p" || id != (AssetId{}) {
		t.Fatal("procedural missing material contract changed")
	}
	if id, err := authoredVoxFilePalette(assets, def, part, palette, materials, model, "source.vox"); err == nil || err.Error() != "missing material missing for part p" || id != (AssetId{}) {
		t.Fatal("VOX missing material contract changed")
	}
	if len(assets.voxPalettes) != before {
		t.Fatal("failed palette published")
	}
	if id, err := authoredProceduralPalette(nil, nil, part); err != nil || id != (AssetId{}) {
		t.Fatal("nil server precedence changed")
	}
}

func TestC3g3PurePaletteParityAndOwnership(t *testing.T) {
	def, part, palette, materials, model := c3g3Inputs()
	before, _ := json.Marshal(def)
	material := buildAuthoredMaterialVoxelPalette(def.Materials[0])
	c3g3JSONEqual(t, material, c3g3MaterialExpected())
	procedural, err := buildAuthoredProceduralPalette(def, part)
	if err != nil {
		t.Fatal(err)
	}
	c3g3JSONEqual(t, procedural, material)
	other := buildAuthoredMaterialVoxelPalette(def.Materials[0])
	// Normalized tags own their storage, separate from authored input and other builds.
	facts := material.SurfaceMaterials[1]
	facts.Tags[0] = "changed"
	material.SurfaceMaterials[1] = facts
	c3g3JSONEqual(t, other, c3g3MaterialExpected())
	c3g3JSONEqual(t, procedural, c3g3MaterialExpected())
	after, _ := json.Marshal(def)
	if string(before) != string(after) {
		t.Fatal("palette builder mutated authored definition")
	}
	override, err := buildAuthoredVoxFilePalette(def, part, palette, materials, model, "source.vox")
	if err != nil {
		t.Fatal(err)
	}
	expected := VoxelPaletteAsset{VoxPalette: palette, Materials: materials, SourcePath: "source.vox", SurfaceMaterials: map[uint8]VoxelSurfaceMaterial{3: {Kind: "stone", Tags: []string{"kind:stone", "solid", "impact"}}, 9: {Kind: "stone", Tags: []string{"kind:stone", "solid", "impact"}}}}
	c3g3JSONEqual(t, override, expected)
	// Both values survive semantic tags even when downscaling would discard their raw samples.
	if _, ok := override.SurfaceMaterials[0]; ok {
		t.Fatal("zero raw sample received surface tags")
	}
	materials[0].Weight = .9
	materials[0].Property["name"] = "borrowed"
	if override.Materials[0].Weight != .9 || override.Materials[0].Property["name"] != "borrowed" {
		t.Fatal("pure VOX changed borrowed materials contract")
	}
	part.Source.MaterialID = ""
	raw, err := buildAuthoredVoxFilePalette(nil, part, palette, materials, model, "plain.vox")
	if err != nil {
		t.Fatal(err)
	}
	c3g3JSONEqual(t, raw, VoxelPaletteAsset{VoxPalette: palette, Materials: materials, SourcePath: "plain.vox"})
	defaultPalette, err := buildAuthoredProceduralPalette(nil, part)
	if err != nil {
		t.Fatal(err)
	}
	var white VoxPalette
	for i := range white {
		white[i] = [4]uint8{255, 255, 255, 255}
	}
	c3g3JSONEqual(t, defaultPalette, VoxelPaletteAsset{VoxPalette: white, IsPBR: true, Roughness: 1, IOR: 1.5})
	part.Source.MaterialID = "missing"
	if out, err := buildAuthoredProceduralPalette(def, part); err == nil || err.Error() != "missing material missing for part p" || !reflect.DeepEqual(out, VoxelPaletteAsset{}) {
		t.Fatal("pure procedural missing material returned partial result")
	}
	if out, err := buildAuthoredVoxFilePalette(def, part, palette, materials, model, "source.vox"); err == nil || err.Error() != "missing material missing for part p" || !reflect.DeepEqual(out, VoxelPaletteAsset{}) {
		t.Fatal("pure VOX missing material returned partial result")
	}
}
