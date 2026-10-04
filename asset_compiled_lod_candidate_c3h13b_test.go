package gekko

import (
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

type c3h13bFixture struct {
	assets  *AssetServer
	intent  compiledAssetLODComponent
	vox     *VoxelModelComponent
	object  *core.VoxelObject
	binding *compiledAssetLODBinding
	coarse  *volume.XBrickMap
}

func c3h13bPrepare(t *testing.T) *c3h13bFixture {
	t.Helper()
	path := c3h13aFixture(t, "eligible")
	return c3h13bFromPath(t, path)
}

func c3h13bFromPath(t *testing.T, path string) *c3h13bFixture {
	t.Helper()
	assets := c3d3Server()
	prepared, err := LoadAndPrepareAuthoredAsset(path, assets, nil)
	if err != nil {
		t.Fatal(err)
	}
	part := prepared.parts["shape"]
	binding := c3h12bBinding(t, assets, part.model)
	full := assets.voxModels[part.model].XBrickMap
	object := core.NewVoxelObject()
	object.XBrickMap = full.Copy()
	object.MaterialTable = make([]core.Material, 8)
	object.MaterialTable[binding.proof.value] = core.Material{BaseColor: [4]uint8{23, 42, 61, 255}}
	return &c3h13bFixture{assets: assets, intent: compiledAssetLODComponent{fullID: part.model, coarseID: part.compiledLOD}, vox: &VoxelModelComponent{SharedGeometry: part.model, VoxelPalette: part.palette, VoxelResolution: binding.proof.lattice.VoxelResolution}, object: object, binding: binding, coarse: assets.voxModels[binding.coarseID].XBrickMap}
}

func c3h13bCheck(t *testing.T, q *compiledAssetLODQualification, f *c3h13bFixture, want bool) {
	t.Helper()
	// Snapshot raw primary and bookkeeping state. Qualification owns no mutation,
	// dirty-consumption, hydration, registration or render activation authority.
	beforeObject := *f.object
	if f.object.XBrickMap != nil {
		beforeObject.XBrickMap = c3h11OwnedCopy(f.object.XBrickMap)
	}
	if f.object.Transform != nil {
		transform := *f.object.Transform
		beforeObject.Transform = &transform
	}
	beforeObject.MaterialTable = append([]core.Material(nil), f.object.MaterialTable...)
	beforeVox := *f.vox
	beforeCoarse := c3h11OwnedCopy(f.coarse)
	beforeProofFull := c3h11OwnedCopy(f.binding.proof.full)
	beforeProofCoarse := c3h11OwnedCopy(f.binding.proof.coarse)
	stats := f.assets.compiledAssetLODStorageStats()
	result, ok := q.candidate(f.assets, f.intent, f.vox, f.object)
	if ok != want {
		t.Fatalf("qualification=%v want=%v", ok, want)
	}
	if ok && (result.binding != f.binding || result.coarse != f.coarse) {
		t.Fatal("qualified result is not exact ordinary binding/coarse input")
	}
	if !ok && (result.binding != nil || result.coarse != nil) {
		t.Fatal("rejected qualification exposed partial candidate")
	}
	afterVox := *f.vox
	resolutionUnchanged := math.Float32bits(afterVox.VoxelResolution) == math.Float32bits(beforeVox.VoxelResolution)
	afterVox.VoxelResolution, beforeVox.VoxelResolution = 0, 0
	if !reflect.DeepEqual(f.object, &beforeObject) || afterVox != beforeVox || !resolutionUnchanged || !reflect.DeepEqual(f.coarse, beforeCoarse) || !reflect.DeepEqual(f.binding.proof.full, beforeProofFull) || !reflect.DeepEqual(f.binding.proof.coarse, beforeProofCoarse) || stats != f.assets.compiledAssetLODStorageStats() {
		t.Fatal("qualification mutated object/component/geometry/proof/owner statistics")
	}
}

func TestC3h13bCandidateUsesObjectOwnedFineAndMemoizesExactGeometryPairs(t *testing.T) {
	f := c3h13bPrepare(t)
	f.vox.RetainRendererGeometry = true // ordinary streamed GPU retention is allowed
	var q compiledAssetLODQualification
	c3h12aIndependentStorage(t, f.object.XBrickMap, f.assets.voxModels[f.intent.fullID].XBrickMap, f.coarse, f.binding.proof.full, f.binding.proof.coarse)
	c3h13bCheck(t, &q, f, true)
	fullPair := compiledAssetLODGeometryPair{current: f.object.XBrickMap, baseline: f.binding.proof.full}
	coarsePair := compiledAssetLODGeometryPair{current: f.coarse, baseline: f.binding.proof.coarse}
	if !q.geometry[fullPair] || !q.geometry[coarsePair] {
		t.Fatal("zero-value qualification failed to retain true exact pointer pairs")
	}
	c3h13bCheck(t, &q, f, true)
	// Another actual object has its own fine copy; sharing the ordinary ID alone
	// must not reuse an earlier object's geometry result.
	other := *f.object
	other.XBrickMap = f.object.XBrickMap.Copy()
	f.object = &other
	c3h13bCheck(t, &q, f, true)
	if !q.geometry[compiledAssetLODGeometryPair{current: f.object.XBrickMap, baseline: f.binding.proof.full}] {
		t.Fatal("actual object copy geometry match missing")
	}
	for _, sector := range f.object.XBrickMap.Sectors {
		sector.PackedBricks[0].Payload[0][0][0] = 91
		break
	}
	var next compiledAssetLODQualification
	c3h13bCheck(t, &next, f, false)
	badPair := compiledAssetLODGeometryPair{current: f.object.XBrickMap, baseline: f.binding.proof.full}
	if match, exists := next.geometry[badPair]; !exists || match {
		t.Fatal("negative geometry result not memoized by exact pointer pair")
	}
	// Do not mutate either map again inside this memo's stable sync lifetime.
	c3h13bCheck(t, &next, f, false)
}

func TestC3h13bNewQualificationLifetimeObservesRawFineAndCoarseEdits(t *testing.T) {
	for _, which := range []string{"object-fine", "server-coarse"} {
		t.Run(which, func(t *testing.T) {
			f := c3h13bPrepare(t)
			var previous compiledAssetLODQualification
			c3h13bCheck(t, &previous, f, true)
			edited := f.object.XBrickMap
			if which == "server-coarse" {
				edited = f.coarse
			}
			revision := edited.Revision
			for _, sector := range edited.Sectors {
				sector.PackedBricks[0].Payload[0][0][0] = 99
				break
			}
			if edited.Revision != revision {
				t.Fatal("raw fixture unexpectedly changed revision")
			}
			var current compiledAssetLODQualification
			c3h13bCheck(t, &current, f, false)
		})
	}
	// The actual renderer object's primary storage, rather than merely the live
	// source ID or server's independent full map, is the qualification input.
	f := c3h13bPrepare(t)
	full := f.assets.voxModels[f.intent.fullID].XBrickMap
	for _, sector := range full.Sectors {
		sector.PackedBricks[0].Payload[0][0][0] = 99
		break
	}
	var q compiledAssetLODQualification
	c3h13bCheck(t, &q, f, true)
}

func TestC3h13bMaterialsAndAnimationsRefreshDuringGeometryMemoReuse(t *testing.T) {
	f := c3h13bPrepare(t)
	var q compiledAssetLODQualification
	c3h13bCheck(t, &q, f, true)
	used := f.binding.proof.value
	for _, change := range []struct {
		name string
		edit func(*core.Material)
	}{
		{"alpha", func(m *core.Material) { m.BaseColor[3] = 254 }},
		{"transparent", func(m *core.Material) { m.Transparency = .1 }},
		{"transmission", func(m *core.Material) { m.Transmission = .1 }},
		{"nonfinite", func(m *core.Material) { m.Transmission = float32(math.Inf(1)) }},
	} {
		original := f.object.MaterialTable[used]
		change.edit(&f.object.MaterialTable[used])
		c3h13bCheck(t, &q, f, false)
		f.object.MaterialTable[used] = original
		c3h13bCheck(t, &q, f, true)
	}
	table := f.object.MaterialTable
	f.object.MaterialTable = table[:int(used)]
	c3h13bCheck(t, &q, f, false)
	f.object.MaterialTable = table
	// A transparent unused slot must not disqualify single-value geometry.
	f.object.MaterialTable[4] = core.Material{BaseColor: [4]uint8{1, 2, 3, 12}, Transparency: .8}
	c3h13bCheck(t, &q, f, true)
	paletteID := f.vox.VoxelPalette
	palette := f.assets.voxPalettes[paletteID]
	for _, indices := range [][]uint8{{used}, {4}, {4, used}} {
		palette.Animations = []VoxelPaletteAnimation{{Kind: "frames", PaletteIndices: indices, Frames: []VoxelPaletteAnimationFrame{{Colors: [][4]uint8{{1, 2, 3, 255}}}}}}
		f.assets.voxPalettes[paletteID] = palette
		c3h13bCheck(t, &q, f, len(indices) == 1 && indices[0] == 4)
	}
	palette.Animations = nil
	f.assets.voxPalettes[paletteID] = palette
	c3h13bCheck(t, &q, f, true)
	// Geometry memo cannot hide current palette removal/replacement.
	delete(f.assets.voxPalettes, paletteID)
	c3h13bCheck(t, &q, f, false)
	replacement := makeAssetId()
	f.assets.voxPalettes[replacement] = palette
	f.vox.VoxelPalette = replacement
	c3h13bCheck(t, &q, f, true)
}

func TestC3h13bCandidateRejectsMissingOverrideLatticeAndIdentityInputs(t *testing.T) {
	for _, kind := range []string{"zero-intent", "wrong-full", "wrong-coarse", "override", "legacy-effective-geometry", "resolution", "zero-resolution", "nan-resolution", "inf-resolution", "nil-transform", "nil-object-map", "fine-proof-alias", "coarse-proof-alias", "gpu-fine", "gpu-coarse", "deleted-full", "deleted-coarse", "missing-palette"} {
		t.Run(kind, func(t *testing.T) {
			f := c3h13bPrepare(t)
			switch kind {
			case "zero-intent":
				f.intent = compiledAssetLODComponent{}
			case "wrong-full":
				f.intent.fullID = makeAssetId()
			case "wrong-coarse":
				f.intent.coarseID = makeAssetId()
			case "override":
				f.vox.OverrideGeometry = makeAssetId()
			case "legacy-effective-geometry":
				f.vox.VoxelModel = makeAssetId()
			case "resolution":
				f.vox.VoxelResolution *= 2
			case "zero-resolution":
				f.vox.VoxelResolution = 0
			case "nan-resolution":
				f.vox.VoxelResolution = float32(math.NaN())
			case "inf-resolution":
				f.vox.VoxelResolution = float32(math.Inf(1))
			case "nil-transform":
				f.object.Transform = nil
			case "nil-object-map":
				f.object.XBrickMap = nil
			case "fine-proof-alias":
				f.object.XBrickMap = f.binding.proof.full
			case "coarse-proof-alias":
				coarse := f.assets.voxModels[f.intent.coarseID]
				coarse.XBrickMap = f.binding.proof.coarse
				f.assets.voxModels[f.intent.coarseID] = coarse
				f.coarse = coarse.XBrickMap
			case "gpu-fine":
				f.object.XBrickMap.GPUEditMode = true
			case "gpu-coarse":
				f.coarse.GPUEditMode = true
			case "deleted-full":
				f.assets.DeleteVoxelGeometry(f.intent.fullID)
			case "deleted-coarse":
				f.assets.DeleteVoxelGeometry(f.intent.coarseID)
			case "missing-palette":
				delete(f.assets.voxPalettes, f.vox.VoxelPalette)
			}
			var q compiledAssetLODQualification
			c3h13bCheck(t, &q, f, false)
		})
	}
	f := c3h13bPrepare(t)
	var nilQ *compiledAssetLODQualification
	for _, which := range []string{"qualifier", "server", "component", "object"} {
		var q compiledAssetLODQualification
		qualifier := &q
		server := f.assets
		vox := f.vox
		object := f.object
		switch which {
		case "qualifier":
			qualifier = nilQ
		case "server":
			server = nil
		case "component":
			vox = nil
		case "object":
			object = nil
		}
		result, ok := qualifier.candidate(server, f.intent, vox, object)
		if ok || result.binding != nil || result.coarse != nil {
			t.Fatal("nil candidate input accepted", which)
		}
	}
}

func TestC3h13bSpecialLatticeTagsRejectOnEitherComponentOrObject(t *testing.T) {
	componentTags := []func(*VoxelModelComponent){
		func(v *VoxelModelComponent) { v.VoxelAdjacencyGroupID = 1 }, func(v *VoxelModelComponent) { v.VoxelAdjacencyChunkCoord = [3]int{1, 0, 0} }, func(v *VoxelModelComponent) { v.VoxelAdjacencyChunkSize = 1 },
		func(v *VoxelModelComponent) { v.IsTerrainChunk = true }, func(v *VoxelModelComponent) { v.ShareTerrainGeometry = true },
		func(v *VoxelModelComponent) { v.TerrainGroupID = 1 }, func(v *VoxelModelComponent) { v.TerrainChunkCoord = [3]int{0, 1, 0} }, func(v *VoxelModelComponent) { v.TerrainChunkSize = 1 },
		func(v *VoxelModelComponent) { v.IsPlanetTile = true }, func(v *VoxelModelComponent) { v.PlanetTileGroupID = 1 }, func(v *VoxelModelComponent) { v.PlanetTileFace = 1 }, func(v *VoxelModelComponent) { v.PlanetTileLevel = 1 }, func(v *VoxelModelComponent) { v.PlanetTileX = 1 }, func(v *VoxelModelComponent) { v.PlanetTileY = 1 },
	}
	objectTags := []func(*core.VoxelObject){
		func(v *core.VoxelObject) { v.VoxelAdjacencyGroupID = 1 }, func(v *core.VoxelObject) { v.VoxelAdjacencyChunkCoord = [3]int{1, 0, 0} }, func(v *core.VoxelObject) { v.VoxelAdjacencyChunkSize = 1 },
		func(v *core.VoxelObject) { v.IsTerrainChunk = true }, func(v *core.VoxelObject) { v.TerrainGroupID = 1 }, func(v *core.VoxelObject) { v.TerrainChunkCoord = [3]int{0, 1, 0} }, func(v *core.VoxelObject) { v.TerrainChunkSize = 1 },
		func(v *core.VoxelObject) { v.IsPlanetTile = true }, func(v *core.VoxelObject) { v.PlanetTileGroupID = 1 }, func(v *core.VoxelObject) { v.PlanetTileFace = 1 }, func(v *core.VoxelObject) { v.PlanetTileLevel = 1 }, func(v *core.VoxelObject) { v.PlanetTileX = 1 }, func(v *core.VoxelObject) { v.PlanetTileY = 1 },
	}
	// Reuse stable geometry pairs, but special ownership tags remain current
	// per-instance facts and may never be cached with primary geometry matches.
	f := c3h13bPrepare(t)
	var q compiledAssetLODQualification
	c3h13bCheck(t, &q, f, true)
	originalVox, originalObject := *f.vox, *f.object
	for i, tag := range componentTags {
		tag(f.vox)
		t.Run("component-"+string(rune('a'+i)), func(t *testing.T) { c3h13bCheck(t, &q, f, false) })
		*f.vox = originalVox
	}
	for i, tag := range objectTags {
		tag(f.object)
		t.Run("object-"+string(rune('a'+i)), func(t *testing.T) { c3h13bCheck(t, &q, f, false) })
		*f.object = originalObject
	}
	c3h13bCheck(t, &q, f, true)
}

func TestC3h13bCandidateNeverHydratesMissingOrdinaryMaps(t *testing.T) {
	for _, which := range []string{"full", "coarse"} {
		t.Run(which, func(t *testing.T) {
			f := c3h13bPrepare(t)
			id := f.intent.fullID
			if which == "coarse" {
				id = f.intent.coarseID
			}
			asset := f.assets.voxModels[id]
			asset.XBrickMap = nil
			asset.VoxModel = VoxModel{Voxels: []Voxel{{X: 0, Y: 0, Z: 0, ColorIndex: 3}}}
			f.assets.voxModels[id] = asset
			before := f.assets.voxModels[id]
			var q compiledAssetLODQualification
			result, ok := q.candidate(f.assets, f.intent, f.vox, f.object)
			if ok || result.binding != nil || result.coarse != nil || !reflect.DeepEqual(f.assets.voxModels[id], before) {
				t.Fatal("candidate hydrated/repaired missing ordinary primary geometry")
			}
		})
	}
}

func TestC3h13bResolutionDefaultUsesExistingEffectiveResolutionContract(t *testing.T) {
	input, output, asset := c3h4Fixture(t)
	asset.Emitters = nil
	for i := range asset.Parts {
		asset.Parts[i].VoxelResolution = VoxelSize
	}
	c3cWrite(t, input, asset)
	output += "c"
	if _, err := CompileAuthoredAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: true}); err != nil {
		t.Fatal(err)
	}
	f := c3h13bFromPath(t, output)
	if f.binding.proof.lattice.VoxelResolution != VoxelSize {
		t.Fatal("fixture failed to produce default resolution proof")
	}
	for _, resolution := range []float32{VoxelSize, 0, -1, float32(math.NaN())} {
		f.vox.VoxelResolution = resolution
		var q compiledAssetLODQualification
		c3h13bCheck(t, &q, f, true)
	}
}
