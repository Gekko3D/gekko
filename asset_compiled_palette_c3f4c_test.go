package gekko

import (
	"fmt"
	"strings"
	"testing"
)

type c3f4cNamedString string
type c3f4cNamedInt int32
type c3f4cNamedFloat float32
type c3f4cNamedBool bool

func c3f4cChargeParity(t *testing.T, source *VoxelPaletteAsset) {
	t.Helper()
	registration, err := prepareCompiledPaletteRegistration(source)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.release()
	// This specialization is defined only for the constructor's independently cloned copy.
	owned := registration.palette
	want := runtimeContentGraphCharge(owned)
	if got := compiledPalettePublicationCharge(owned); got != want || registration.bytes != want {
		t.Fatalf("publication charge differs from generic estimate: specialized=%d stored=%d generic=%d", got, registration.bytes, want)
	}
}

func TestC3f4cPublicationChargeMatchesGenericRichAndEmptyCopies(t *testing.T) {
	rich := c3f4aPalette()
	emptyNested := VoxelPaletteAsset{Materials: []VoxMaterial{{Property: map[string]interface{}{}}}, SurfaceMaterials: map[uint8]VoxelSurfaceMaterial{3: {Tags: []string{}}}, MaterialFrameOverrides: map[uint8]VoxelPaletteMaterialFrameOverride{}, Animations: []VoxelPaletteAnimation{{PaletteIndices: []uint8{}, Tags: []string{}, Frames: []VoxelPaletteAnimationFrame{{Colors: [][4]uint8{}, EmissiveColors: [][4]uint8{}, Emission: []float32{}, Roughness: []float32{}, Transparency: []float32{}}}, UVScroll: &VoxelPaletteUVScroll{}}}}
	for name, source := range map[string]VoxelPaletteAsset{"rich": rich, "zero": {}, "empty-containers": {Materials: []VoxMaterial{}, SurfaceMaterials: map[uint8]VoxelSurfaceMaterial{}, Animations: []VoxelPaletteAnimation{}, MaterialFrameOverrides: map[uint8]VoxelPaletteMaterialFrameOverride{}}, "empty-nested": emptyNested} {
		t.Run(name, func(t *testing.T) { c3f4cChargeParity(t, &source) })
	}
}

func TestC3f4cPublicationChargeHandlesClonedAliasesScalarTypesAndStringViews(t *testing.T) {
	backing := strings.Repeat("shared-backing", 3)
	whole, prefix, offset := backing, backing[:6], backing[3:9]
	properties := map[string]interface{}{
		"nil": nil, "bool": true, "int": int(1), "int8": int8(2), "int16": int16(3), "int32": int32(4), "int64": int64(5),
		"uint": uint(6), "uint8": uint8(7), "uint16": uint16(8), "uint32": uint32(9), "uint64": uint64(10), "uintptr": uintptr(11),
		"float32": float32(.25), "float64": float64(.5), "named-int": c3f4cNamedInt(12), "named-float": c3f4cNamedFloat(.75), "named-bool": c3f4cNamedBool(false),
		"whole": whole, "same-view": whole, "prefix": prefix, "offset": offset, "named-string": c3f4cNamedString(whole), "same-named-view": c3f4cNamedString(whole), "empty": "", "named-empty": c3f4cNamedString(""),
		"equal-independent": strings.Clone(whole),
		whole:               whole,
	}
	tags := []string{whole, whole, prefix, offset}
	colors := make([][4]uint8, 1, 4)
	colors[0] = [4]uint8{1, 2, 3, 4}
	floatValues := make([]float32, 1, 4)
	floatValues[0] = .25
	frames := []VoxelPaletteAnimationFrame{{Duration: .1, Colors: colors, EmissiveColors: colors, Emission: floatValues, Roughness: floatValues, Transparency: floatValues}}
	uv := &VoxelPaletteUVScroll{Velocity: [2]float32{.5, -.5}}
	animation := VoxelPaletteAnimation{ID: whole, Kind: prefix, Mode: offset, PaletteIndices: []uint8{3, 4}, Frames: frames, UVScroll: uv, Tags: tags}
	source := VoxelPaletteAsset{SourcePath: whole, Materials: []VoxMaterial{{ID: 3, Property: properties}, {ID: 4, Property: properties}}, SurfaceMaterials: map[uint8]VoxelSurfaceMaterial{3: {Kind: whole, Tags: tags}, 4: {Kind: prefix, Tags: tags}}, Animations: []VoxelPaletteAnimation{animation, animation}}
	c3f4cChargeParity(t, &source)
}

func c3f4cMaterialPalette() VoxelPaletteAsset {
	source := VoxelPaletteAsset{Materials: make([]VoxMaterial, 256), SurfaceMaterials: make(map[uint8]VoxelSurfaceMaterial, 256)}
	for i := range source.Materials {
		source.Materials[i] = VoxMaterial{ID: i, Property: map[string]interface{}{"_type": "diffuse", "_rough": float32(.5), "_metal": float32(.25), "_ior": float32(1.5)}}
		source.SurfaceMaterials[uint8(i)] = VoxelSurfaceMaterial{Kind: "solid", Tags: []string{"surface:solid", fmt.Sprintf("material:%d", i)}}
	}
	return source
}

func TestC3f4cPublicationChargeReducesEstimatorAllocations(t *testing.T) {
	source := c3f4cMaterialPalette()
	registration, err := prepareCompiledPaletteRegistration(&source)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.release()
	owned := registration.palette
	var genericCharge, specializedCharge int64
	generic := testing.AllocsPerRun(20, func() { genericCharge = runtimeContentGraphCharge(owned) })
	specialized := testing.AllocsPerRun(20, func() { specializedCharge = compiledPalettePublicationCharge(owned) })
	if genericCharge != specializedCharge || genericCharge != registration.bytes {
		t.Fatal("allocation fixture charge lost exact parity")
	}
	if generic <= 0 || specialized > generic*.5 {
		t.Fatalf("specialized estimator must substantially reduce allocations: specialized=%.1f generic=%.1f", specialized, generic)
	}
}
