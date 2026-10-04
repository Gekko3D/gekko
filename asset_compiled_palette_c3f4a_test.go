package gekko

import (
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func c3f4aPalette() VoxelPaletteAsset {
	asset := VoxelPaletteAsset{IsPBR: true, Roughness: .2, Metalness: .3, Emission: .4, IOR: 1.5, Transparency: .6, SourcePath: "authored-palette", Materials: []VoxMaterial{{ID: 3, Type: 2, Weight: .7, Property: map[string]interface{}{"float32": float32(.25), "float64": float64(.5), "int": 4, "string": "metal", "bool": true, "nil": nil}}}, SurfaceMaterials: map[uint8]VoxelSurfaceMaterial{3: {Kind: "metal", Tags: []string{"surface:metal", "solid"}}}, MaterialFrameOverrides: map[uint8]VoxelPaletteMaterialFrameOverride{3: {EmissiveColor: [4]uint8{1, 2, 3, 4}, HasEmissiveColor: true, Emission: .8, HasEmission: true, Roughness: .9, HasRoughness: true, Transparency: .1, HasTransparency: true}}, Animations: []VoxelPaletteAnimation{{ID: "pulse", Kind: "palette", FPS: 12, Mode: "loop", PaletteIndices: []uint8{3}, UVScroll: &VoxelPaletteUVScroll{Velocity: [2]float32{.25, -.5}}, Tags: []string{"animated"}, Frames: []VoxelPaletteAnimationFrame{{Duration: .1, Colors: [][4]uint8{{10, 20, 30, 255}}, EmissiveColors: [][4]uint8{{40, 50, 60, 255}}, Emission: []float32{.4}, Roughness: []float32{.2}, Transparency: []float32{.3}}}}}}
	asset.VoxPalette[3] = [4]uint8{10, 20, 30, 255}
	return asset
}

func c3f4aMutate(asset *VoxelPaletteAsset) {
	asset.VoxPalette[3][0] = 99
	asset.Materials[0].Property["float32"] = float32(99)
	asset.SurfaceMaterials[3].Tags[0] = "changed"
	asset.MaterialFrameOverrides[3] = VoxelPaletteMaterialFrameOverride{}
	animation := &asset.Animations[0]
	animation.PaletteIndices[0] = 99
	animation.Tags[0] = "changed"
	animation.UVScroll.Velocity[0] = 99
	frame := &animation.Frames[0]
	frame.Colors[0][0], frame.EmissiveColors[0][0] = 99, 99
	frame.Emission[0], frame.Roughness[0], frame.Transparency[0] = 99, 99, 99
}

func TestC3f4aPurePaletteBuilderPreservesExistingAuthoredResult(t *testing.T) {
	_, _, def := c3d3Fixture(t)
	def.MaterialAnimations = []content.AssetMaterialAnimationDef{{ID: "pulse", Kind: "palette", FPS: 12, Mode: "loop", PaletteIndices: []uint8{3}, UVScroll: &content.AssetMaterialUVScrollDef{Velocity: [2]float32{.25, -.5}}, Tags: []string{"animated"}, Frames: []content.AssetMaterialAnimationFrameDef{{Duration: .1, Colors: [][4]uint8{{1, 2, 3, 4}}, EmissiveColors: [][4]uint8{{5, 6, 7, 8}}, Emission: []float32{.4}, Roughness: []float32{.2}, Transparency: []float32{.3}}}}}
	part := def.Parts[0]
	assets := c3d3Server()
	id, err := authoredVoxelShapePalette(assets, def, part)
	if err != nil {
		t.Fatal(err)
	}
	want, ok := assets.GetVoxelPalette(id)
	if !ok || want.VoxPalette[3] != def.Materials[0].BaseColor || len(want.Animations) != 1 || want.Materials[0].Property["_rough"] != def.Materials[0].Roughness {
		t.Fatal("existing authored palette baseline invalid")
	}
	got, err := buildAuthoredVoxelShapePalette(def, part)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("pure builder changed existing full material/animation result", err)
	}
	if len(assets.voxPalettes) != 1 {
		t.Fatal("pure builder published assets")
	}
	for _, failure := range []string{"payload", "material"} {
		bad := part
		if failure == "payload" {
			bad.Source.VoxelShape = nil
		} else {
			shape := *part.Source.VoxelShape
			shape.Palette = []content.AssetVoxelPaletteEntryDef{{Value: 3, MaterialID: "absent"}}
			bad.Source.VoxelShape = &shape
		}
		if result, err := buildAuthoredVoxelShapePalette(def, bad); err == nil || !reflect.DeepEqual(result, VoxelPaletteAsset{}) {
			t.Fatal("invalid pure build must return zero palette/error", failure)
		}
		if id, err := authoredVoxelShapePalette(nil, def, bad); err != nil || id != (AssetId{}) {
			t.Fatal("nil-server legacy wrapper no longer returns before validation")
		}
	}
}

func TestC3f4aPaletteRegistrationDeepOwnershipAndValidation(t *testing.T) {
	for _, direction := range []string{"source", "adopted"} {
		t.Run(direction, func(t *testing.T) {
			source := c3f4aPalette()
			key := voxelPaletteAssetCacheKey(source)
			registration, err := prepareCompiledPaletteRegistration(&source)
			if err != nil || registration == nil || registration.charge() <= 0 {
				t.Fatal("valid palette registration failed", err)
			}
			defer registration.release()
			if registration.palette == nil || registration.palette == &source {
				t.Fatal("registration omitted independent owned publication copy")
			}
			publicationCopy := *registration.palette
			assets := &AssetServer{}
			id, ok := assets.adoptCompiledAssetPalette(key, &source, registration)
			if !ok || id == (AssetId{}) || registration.charge() != 0 || registration.source != nil || registration.palette != nil {
				t.Fatal("cold palette adoption failed")
			}
			adopted, ok := assets.GetVoxelPalette(id)
			if !ok || !reflect.DeepEqual(adopted, c3f4aPalette()) || voxelPaletteAssetCacheKey(adopted) != key {
				t.Fatal("registration changed fields/scalar property types/cache key")
			}
			for _, storage := range []struct{ source, prepared, adopted any }{
				{source.Materials, publicationCopy.Materials, adopted.Materials},
				{source.Materials[0].Property, publicationCopy.Materials[0].Property, adopted.Materials[0].Property},
				{source.SurfaceMaterials, publicationCopy.SurfaceMaterials, adopted.SurfaceMaterials},
				{source.SurfaceMaterials[3].Tags, publicationCopy.SurfaceMaterials[3].Tags, adopted.SurfaceMaterials[3].Tags},
				{source.MaterialFrameOverrides, publicationCopy.MaterialFrameOverrides, adopted.MaterialFrameOverrides},
				{source.Animations, publicationCopy.Animations, adopted.Animations},
				{source.Animations[0].Frames, publicationCopy.Animations[0].Frames, adopted.Animations[0].Frames},
				{source.Animations[0].UVScroll, publicationCopy.Animations[0].UVScroll, adopted.Animations[0].UVScroll},
			} {
				owned := reflect.ValueOf(storage.prepared).UnsafePointer()
				if owned == reflect.ValueOf(storage.source).UnsafePointer() || owned != reflect.ValueOf(storage.adopted).UnsafePointer() {
					t.Fatal("cold adoption must transfer independently prepared storage without another deep copy")
				}
			}
			if direction == "source" {
				c3f4aMutate(&source)
				if !reflect.DeepEqual(adopted, c3f4aPalette()) {
					t.Fatal("source mutations alias adopted nested storage")
				}
			} else {
				c3f4aMutate(&adopted)
				if !reflect.DeepEqual(source, c3f4aPalette()) {
					t.Fatal("public adopted mutations alias retained source")
				}
			}
			registration.release()
			if _, exists := assets.GetVoxelPalette(id); !exists {
				t.Fatal("late release deleted global palette")
			}
		})
	}
	for _, invalid := range []string{"nil", "nested-map", "nested-slice", "pointer", "channel", "nan", "infinity"} {
		t.Run(invalid, func(t *testing.T) {
			source := c3f4aPalette()
			input := &source
			switch invalid {
			case "nil":
				input = nil
			case "nested-map":
				source.Materials[0].Property["bad"] = map[string]string{"value": "mutable"}
			case "nested-slice":
				source.Materials[0].Property["bad"] = []int{1}
			case "pointer":
				value := 1
				source.Materials[0].Property["bad"] = &value
			case "channel":
				source.Materials[0].Property["bad"] = make(chan int)
			case "nan":
				source.Roughness = float32(math.NaN())
			case "infinity":
				source.Materials[0].Property["bad"] = math.Inf(1)
			}
			if handle, err := prepareCompiledPaletteRegistration(input); err == nil || handle != nil {
				t.Fatal("unsupported mutable/nonmarshalable palette accepted")
			}
		})
	}
}

func TestC3f4aPaletteNilAndEmptyIdentityRemainDistinct(t *testing.T) {
	for _, field := range []string{"materials", "properties", "surfaces", "animations", "overrides", "tags", "frames"} {
		t.Run(field, func(t *testing.T) {
			a, b := VoxelPaletteAsset{}, VoxelPaletteAsset{}
			switch field {
			case "materials":
				b.Materials = []VoxMaterial{}
			case "properties":
				a.Materials = []VoxMaterial{{}}
				b.Materials = []VoxMaterial{{Property: map[string]interface{}{}}}
			case "surfaces":
				b.SurfaceMaterials = map[uint8]VoxelSurfaceMaterial{}
			case "animations":
				b.Animations = []VoxelPaletteAnimation{}
			case "overrides":
				b.MaterialFrameOverrides = map[uint8]VoxelPaletteMaterialFrameOverride{}
			case "tags":
				a.SurfaceMaterials = map[uint8]VoxelSurfaceMaterial{3: {}}
				b.SurfaceMaterials = map[uint8]VoxelSurfaceMaterial{3: {Tags: []string{}}}
			case "frames":
				a.Animations = []VoxelPaletteAnimation{{}}
				b.Animations = []VoxelPaletteAnimation{{Frames: []VoxelPaletteAnimationFrame{}}}
			}
			assets := &AssetServer{}
			ids := []AssetId{}
			for _, source := range []*VoxelPaletteAsset{&a, &b} {
				key := voxelPaletteAssetCacheKey(*source)
				handle, err := prepareCompiledPaletteRegistration(source)
				if err != nil {
					t.Fatal(err)
				}
				defer handle.release()
				id, ok := assets.adoptCompiledAssetPalette(key, source, handle)
				got, exists := assets.GetVoxelPalette(id)
				if !ok || !exists || !reflect.DeepEqual(got, *source) || voxelPaletteAssetCacheKey(got) != key {
					t.Fatal("copy collapsed nil/empty identity", field)
				}
				ids = append(ids, id)
			}
			if ids[0] == ids[1] {
				t.Fatal("nil/empty legacy cache identities merged")
			}
		})
	}
}

func TestC3f4aPaletteMismatchSingleUseWarmAndPublicCompatibility(t *testing.T) {
	source := c3f4aPalette()
	key := voxelPaletteAssetCacheKey(source)
	handle, err := prepareCompiledPaletteRegistration(&source)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.release()
	assets := &AssetServer{}
	other := c3f4aPalette()
	for _, mismatch := range []string{"key", "source"} {
		passedKey, passedSource := key, &source
		if mismatch == "key" {
			passedKey = key + "bad"
		} else {
			passedSource = &other
		}
		if id, ok := assets.adoptCompiledAssetPalette(passedKey, passedSource, handle); ok || id != (AssetId{}) || len(assets.voxPalettes) != 0 || handle.charge() <= 0 {
			t.Fatal("cold mismatch stole valid handle or published", mismatch)
		}
	}
	id, ok := assets.adoptCompiledAssetPalette(key, &source, handle)
	if !ok {
		t.Fatal("mismatch stole subsequent valid adoption")
	}
	alias := handle
	alias.release()
	mutable, _ := assets.GetVoxelPalette(id)
	c3f4aMutate(&mutable)
	// The getter returns the palette by value: nested edits persist, array edits do not.
	mutable, _ = assets.GetVoxelPalette(id)
	unused, err := prepareCompiledPaletteRegistration(&source)
	if err != nil {
		t.Fatal(err)
	}
	defer unused.release()
	otherWarmSource := c3f4aPalette()
	otherWarmSource.SourcePath = "different-existing-palette"
	otherWarmID := assets.CreateVoxelPaletteAsset(otherWarmSource)
	for _, mismatch := range []string{"key", "source"} {
		passedKey, passedSource := key, &source
		if mismatch == "key" {
			passedKey = voxelPaletteAssetCacheKey(otherWarmSource)
		} else {
			passedSource = &other
		}
		if got, ok := assets.adoptCompiledAssetPalette(passedKey, passedSource, unused); ok || got != (AssetId{}) || unused.charge() <= 0 {
			t.Fatal("warm mismatch consumed live handle or returned unrelated palette", mismatch)
		}
	}
	if _, exists := assets.GetVoxelPalette(otherWarmID); !exists {
		t.Fatal("warm mismatch deleted unrelated existing palette")
	}
	warmID, ok := assets.adoptCompiledAssetPalette(key, &source, unused)
	got, _ := assets.GetVoxelPalette(id)
	if !ok || warmID != id || unused.charge() != 0 || unused.source != nil || unused.palette != nil || !reflect.DeepEqual(got, mutable) {
		t.Fatal("warm adoption replaced mutable palette or retained copy")
	}
	if publicID := assets.CreateVoxelPaletteAsset(source); publicID != id {
		t.Fatal("adoption differs from existing public namespace/stale-key behavior")
	}
	if repeated, ok := assets.adoptCompiledAssetPalette(key, &source, handle); !ok || repeated != id || handle.charge() != 0 {
		t.Fatal("consumed sealed handle failed independently verified warm reuse")
	}
	released, err := prepareCompiledPaletteRegistration(&source)
	if err != nil {
		t.Fatal(err)
	}
	released.release()
	if released.source != nil || released.palette != nil {
		t.Fatal("released handle retains heavy source/publication copy")
	}
	if repeated, ok := assets.adoptCompiledAssetPalette(key, &source, released); !ok || repeated != id || released.charge() != 0 {
		t.Fatal("released sealed handle failed independently verified warm reuse")
	}
	fresh := &AssetServer{}
	if another, ok := fresh.adoptCompiledAssetPalette(key, &source, handle); ok || another != (AssetId{}) || len(fresh.voxPalettes) != 0 {
		t.Fatal("consumed handle published another cold owner")
	}
	publicSource := c3f4aPalette()
	public := &AssetServer{}
	publicID := public.CreateVoxelPaletteAsset(publicSource)
	publicSource.Materials[0].Property["int"] = 99
	retained, _ := public.GetVoxelPalette(publicID)
	if retained.Materials[0].Property["int"] != 99 {
		t.Fatal("public creator retained-alias contract changed")
	}
	publicHandle, err := prepareCompiledPaletteRegistration(&source)
	if err != nil {
		t.Fatal(err)
	}
	defer publicHandle.release()
	if got, ok := public.adoptCompiledAssetPalette(key, &source, publicHandle); !ok || got != publicID || publicHandle.charge() != 0 {
		t.Fatal("compiled warm adoption misses existing public key")
	}
}

func TestC3f4aConcurrentPaletteAdoptionAndRelease(t *testing.T) {
	const workers = 6
	assets := &AssetServer{}
	type result struct {
		id AssetId
		ok bool
	}
	sources := make([]VoxelPaletteAsset, workers)
	handles := make([]*compiledPaletteRegistration, workers)
	for i := range sources {
		sources[i] = c3f4aPalette()
		var err error
		handles[i], err = prepareCompiledPaletteRegistration(&sources[i])
		if err != nil {
			t.Fatal(err)
		}
		defer handles[i].release()
	}
	start, results := make(chan struct{}), make(chan result, workers)
	for i := range sources {
		go func(i int) {
			<-start
			id, ok := assets.adoptCompiledAssetPalette(voxelPaletteAssetCacheKey(sources[i]), &sources[i], handles[i])
			results <- result{id, ok}
		}(i)
	}
	close(start)
	completed := make([]result, workers)
	for i := range completed {
		completed[i] = <-results
	}
	for _, got := range completed {
		if !got.ok || got.id != completed[0].id || got.id == (AssetId{}) {
			t.Fatal("concurrent matching palettes did not publish one global owner")
		}
	}
	if len(assets.voxPalettes) != 1 {
		t.Fatal("concurrent palette publication duplicated global storage")
	}
	for _, handle := range handles {
		if handle.charge() != 0 {
			t.Fatal("concurrent adoption retained unused handles")
		}
	}
	// Release races with transfer safely: either transfer or release wins, never two owners.
	source := c3f4aPalette()
	handle, err := prepareCompiledPaletteRegistration(&source)
	if err != nil {
		t.Fatal(err)
	}
	fresh := &AssetServer{}
	raceStart, done := make(chan struct{}), make(chan struct{}, 2)
	go func() {
		<-raceStart
		fresh.adoptCompiledAssetPalette(voxelPaletteAssetCacheKey(source), &source, handle)
		done <- struct{}{}
	}()
	go func() { <-raceStart; handle.release(); handle.release(); done <- struct{}{} }()
	close(raceStart)
	<-done
	<-done
	handle.release()
	if handle.charge() != 0 || len(fresh.voxPalettes) > 1 {
		t.Fatal("release/transfer race leaked copy or duplicated palette")
	}
}
