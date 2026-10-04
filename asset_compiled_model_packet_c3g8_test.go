package gekko

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func c3g8Fixture(t *testing.T, dims [3]uint32) (string, *content.CompiledAssetModelHeaderDef) {
	t.Helper()
	path, h := c3g7Fixture(t)
	modelPath := filepath.Join(filepath.Dir(path), h.Models[0].Path)
	model, _, err := content.LoadCompiledAssetModel(modelPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.Dimensions = dims
	model.Bricks[0].Coord[0] = 1 // primary intentionally lies outside declared X dimensions.
	info, err := content.SaveCompiledAssetModel(modelPath, model, nil)
	if err != nil {
		t.Fatal(err)
	}
	base, _, err := content.CompiledAssetModelBaseIdentity(model, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range h.Models {
		h.Models[i].ContentID = info.ContentID
		h.Models[i].BaseIdentity = base
		h.Models[i].EncodedBytes = info.EncodedBytes
		h.Models[i].DecodedBytes = info.DecodedBytes
	}
	second := h.Palettes[0]
	second.Colors[3][0] = 44
	second.ID, err = content.CompiledAssetModelPaletteIdentity(&second)
	if err != nil {
		t.Fatal(err)
	}
	h.Palettes = append(h.Palettes, second)
	h.Models[1].PaletteID = second.ID
	c3g7Save(t, path, h)
	return path, h
}

func c3g8PaletteExpected(p content.CompiledAssetModelPaletteDef) VoxelPaletteAsset {
	out := VoxelPaletteAsset{VoxPalette: p.Colors, IsPBR: p.IsPBR, Roughness: p.Roughness, Metalness: p.Metalness, Emission: p.Emission, IOR: p.IOR, Transparency: p.Transparency}
	if p.Materials != nil {
		out.Materials = make([]VoxMaterial, len(p.Materials))
		for i, m := range p.Materials {
			out.Materials[i] = VoxMaterial{ID: m.ID, Type: m.Type, Weight: m.Weight, Property: m.Property}
		}
	}
	if p.SurfaceMaterials != nil {
		out.SurfaceMaterials = make(map[uint8]VoxelSurfaceMaterial, len(p.SurfaceMaterials))
		for index, m := range p.SurfaceMaterials {
			out.SurfaceMaterials[index] = VoxelSurfaceMaterial{Kind: m.Kind, Tags: m.Tags}
		}
	}
	return out
}

func TestC3g8PacketModelOwnershipAndBakedPalettes(t *testing.T) {
	path, h := c3g8Fixture(t, [3]uint32{2, 0, 4})
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	caller := owner.NewScope()
	defer caller.Close()
	cached, _, err := caller.Loader().LoadCompiledAssetModelHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	model, _, err := caller.Loader().LoadCompiledAssetModel(filepath.Join(filepath.Dir(path), h.Models[0].Path))
	if err != nil {
		t.Fatal(err)
	}
	before := owner.Stats()
	packet, err := prepareCompiledAssetPacket(path, caller.Loader(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer packet.release()
	id := h.Models[0].ContentID
	shape := packet.shapes[id]
	if shape == nil || !shape.model || shape.dimensions != [3]uint32{2, 0, 4} || packet.parts["model"] != id || packet.parts["other-model"] != id || packet.partPalettes["model"] == packet.partPalettes["other-model"] {
		t.Fatal("shared model geometry or distinct palette membership lost")
	}
	if len(packet.partLODs) != 2 || len(packet.lods) != 1 || len(packet.shapes) != 2 {
		t.Fatal("mixed inline/LOD tables changed")
	}
	if after := owner.Stats(); after.Bytes != before.Bytes || after.PinnedBytes != before.PinnedBytes || after.Entries != before.Entries {
		t.Fatal("packet construction leaked scope leases", after)
	}
	other, err := prepareCompiledAssetPacket(path, caller.Loader(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer other.release()
	c3h12aIndependentStorage(t, shape.source, shape.registration.geometry, other.shapes[id].source, other.shapes[id].registration.geometry)
	for _, ref := range h.Models {
		var dto content.CompiledAssetModelPaletteDef
		for _, p := range h.Palettes {
			if p.ID == ref.PaletteID {
				dto = p
			}
		}
		p := packet.palettes[packet.partPalettes[ref.PartID]]
		c3g3JSONEqual(t, *p.source, c3g8PaletteExpected(dto))
		c3g3JSONEqual(t, *p.registration.palette, c3g8PaletteExpected(dto))
		p.source.Materials[0].Property["name"] = "changed"
		facts := p.source.SurfaceMaterials[3]
		facts.Tags[0] = "changed"
		p.source.SurfaceMaterials[3] = facts
		if p.registration.palette.Materials[0].Property["name"] != "owned" || p.registration.palette.SurfaceMaterials[3].Tags[0] != "solid" {
			t.Fatal("palette registration aliases packet source")
		}
	}
	if cached.Palettes[0].Materials[0].Property["name"] != "owned" || model.Bricks[0].Values[0] != 3 {
		t.Fatal("packet palette/model aliases cache")
	}
	if other.palettes[other.partPalettes["model"]].source.Materials[0].Property["name"] != "owned" {
		t.Fatal("independent packet shares mutable palette")
	}
	// Release drains only pending handles; retained sources still charge once through aliases.
	charge := streamedCompiledAssetPacketsCharge(map[string]*compiledAssetPacket{"one": other})
	other.release()
	afterCharge := streamedCompiledAssetPacketsCharge(map[string]*compiledAssetPacket{"one": other})
	if afterCharge <= 0 || afterCharge >= charge {
		t.Fatal("release did not retain sources while draining pending storage")
	}
	if alias := streamedCompiledAssetPacketsCharge(map[string]*compiledAssetPacket{"one": other, "alias": other}); alias <= afterCharge {
		t.Fatal("alias map metadata missing")
	}
}

func TestC3g8ModelPaletteNilEmptyOwnership(t *testing.T) {
	for _, empty := range []bool{false, true} {
		dto := &content.CompiledAssetModelPaletteDef{}
		if empty {
			dto.Materials = []content.CompiledAssetModelMaterialDef{}
			dto.SurfaceMaterials = map[uint8]content.CompiledAssetModelSurfaceMaterialDef{}
		}
		got := compiledModelRuntimePalette(dto)
		if (got.Materials == nil) != (dto.Materials == nil) || (got.SurfaceMaterials == nil) != (dto.SurfaceMaterials == nil) || got.SourcePath != "" {
			t.Fatal("baked palette nil/empty or source path changed")
		}
	}
	dto := &content.CompiledAssetModelPaletteDef{Materials: []content.CompiledAssetModelMaterialDef{{ID: 3, Property: map[string]any{"bool": true, "nil": nil, "float": .5}}}, SurfaceMaterials: map[uint8]content.CompiledAssetModelSurfaceMaterialDef{3: {Kind: "stone", Tags: []string{"solid"}}}}
	before, _ := json.Marshal(dto)
	a, b := compiledModelRuntimePalette(dto), compiledModelRuntimePalette(dto)
	a.Materials[0].Property["bool"] = false
	a.SurfaceMaterials[3].Tags[0] = "changed"
	after, _ := json.Marshal(dto)
	if string(before) != string(after) || b.Materials[0].Property["bool"] != true || b.SurfaceMaterials[3].Tags[0] != "solid" {
		t.Fatal("DTO conversion does not independently own nested payload")
	}
}

func TestC3g8ModelPublicationDimensionsWarmReuseAndRebuild(t *testing.T) {
	for _, dims := range [][3]uint32{{}, {2, 0, 4}} {
		t.Run("dimensions", func(t *testing.T) {
			path, h := c3g8Fixture(t, dims)
			packet := c3h12aPacket(t, path, NewRuntimeContentLoader())
			assets := &AssetServer{}
			prepared, err := publishCompiledAssetPacket(packet, assets, nil)
			if err != nil {
				t.Fatal(err)
			}
			first, second := prepared.parts["model"], prepared.parts["other-model"]
			if first.model == (AssetId{}) || first.model != second.model || first.palette == second.palette || first.compiledLOD != (AssetId{}) || second.compiledLOD != (AssetId{}) {
				t.Fatal("model publication geometry/palette/LOD membership changed")
			}
			geometry, _ := assets.GetVoxelGeometry(first.model)
			if [3]uint32{geometry.VoxModel.SizeX, geometry.VoxModel.SizeY, geometry.VoxModel.SizeZ} != dims || len(geometry.VoxModel.Voxels) != 0 || geometry.XBrickMap.GetVoxelCount() != 1 {
				t.Fatal("model geometry retained raw rows or lost declared dimensions")
			}
			wantMin, wantMax := mgl32.Vec3{8, 0, 0}, mgl32.Vec3{9, 1, 1}
			if dims != ([3]uint32{}) {
				wantMin = mgl32.Vec3{}
				wantMax = mgl32.Vec3{float32(dims[0]), float32(dims[1]), float32(dims[2])}
			}
			if geometry.LocalMin != wantMin || geometry.LocalMax != wantMax {
				t.Fatalf("declared/occupied bounds got %v %v want %v %v", geometry.LocalMin, geometry.LocalMax, wantMin, wantMax)
			}
			key := "compiled-asset-model:" + h.Models[0].ContentID
			if geometry.SourcePath != key || assets.voxModelKeys[key] != first.model {
				t.Fatal("model namespace ownership changed")
			}
			lattice := packet.shapes[h.Models[0].ContentID].lattice
			if assets.authoredVoxelBaseIdentity(first.model, lattice) != h.Models[0].BaseIdentity || assets.authoredVoxelBaseIdentity(first.model, authoredVoxelShapeLattice(.125)) != "" {
				t.Fatal("new model base acquired inline E2 lattice authority")
			}
			shape := packet.shapes[h.Models[0].ContentID]
			c3h12aIndependentStorage(t, shape.source, geometry.XBrickMap)
			palette, _ := assets.GetVoxelPalette(first.palette)
			c3g3JSONEqual(t, palette, *packet.palettes[packet.partPalettes["model"]].source)
			// Ordinary mutable warm owners remain edited across fresh packets.
			geometry.XBrickMap.SetVoxel(8, 0, 0, 7)
			assets.mu.Lock()
			editedGeometry := assets.voxModels[first.model]
			editedGeometry.VoxModel.SizeX, editedGeometry.VoxModel.SizeY, editedGeometry.VoxModel.SizeZ = 99, 88, 77
			editedGeometry.LocalMin, editedGeometry.LocalMax = mgl32.Vec3{-5, -4, -3}, mgl32.Vec3{20, 21, 22}
			assets.voxModels[first.model] = editedGeometry
			edited := assets.voxPalettes[first.palette]
			edited.VoxPalette[3] = [4]uint8{9, 8, 7, 6}
			assets.voxPalettes[first.palette] = edited
			assets.mu.Unlock()
			fresh := c3h12aPacket(t, path, NewRuntimeContentLoader())
			warm, err := publishCompiledAssetPacket(fresh, assets, nil)
			if err != nil || warm.parts["model"].model != first.model || warm.parts["model"].palette != first.palette {
				t.Fatal("warm owner reuse", err)
			}
			current, _ := assets.GetVoxelGeometry(first.model)
			currentPalette, _ := assets.GetVoxelPalette(first.palette)
			if c3h12aGeometry(current.XBrickMap)[[3]int{8, 0, 0}] != 7 || currentPalette.VoxPalette[3] != ([4]uint8{9, 8, 7, 6}) {
				t.Fatal("warm edit overwritten")
			}
			if [3]uint32{current.VoxModel.SizeX, current.VoxModel.SizeY, current.VoxModel.SizeZ} != ([3]uint32{99, 88, 77}) || current.LocalMin != editedGeometry.LocalMin || current.LocalMax != editedGeometry.LocalMax {
				t.Fatal("warm declared bounds/dimensions overwritten")
			}
			if !assets.DeleteVoxelGeometry(first.model) {
				t.Fatal("model deletion failed")
			}
			if err := os.RemoveAll(filepath.Dir(path)); err != nil {
				t.Fatal(err)
			}
			rebuilt, err := publishCompiledAssetPacket(packet, assets, nil)
			if err != nil {
				t.Fatal("consumed cold rebuild reread files", err)
			}
			rebuiltGeometry, _ := assets.GetVoxelGeometry(rebuilt.parts["model"].model)
			if rebuilt.parts["model"].model == first.model || c3h12aGeometry(rebuiltGeometry.XBrickMap)[[3]int{8, 0, 0}] != 3 || rebuiltGeometry.LocalMin != wantMin || rebuiltGeometry.LocalMax != wantMax || [3]uint32{rebuiltGeometry.VoxModel.SizeX, rebuiltGeometry.VoxModel.SizeY, rebuiltGeometry.VoxModel.SizeZ} != dims {
				t.Fatal("cold rebuild lost packet maps or dimensions")
			}
			packet.release()
			fresh.release()
			if shape.registration.charge() != 0 {
				t.Fatal("adopted handle not consumed")
			}
		})
	}
}

func TestC3g8TerminalModelPacketCancellationReleasesOnlyChild(t *testing.T) {
	path, h := c3g8Fixture(t, [3]uint32{2, 0, 4})
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	caller := owner.NewScope()
	defer caller.Close()
	model, _, err := caller.Loader().LoadCompiledAssetModel(filepath.Join(filepath.Dir(path), h.Models[0].Path))
	if err != nil {
		t.Fatal(err)
	}
	before := owner.Stats()
	calls := 0
	packet, err := prepareCompiledAssetPacket(path, caller.Loader(), func() bool { calls++; return false })
	if err != nil {
		t.Fatal(err)
	}
	packet.release()
	reached := 0
	if rejected, err := prepareCompiledAssetPacket(path, caller.Loader(), func() bool { reached++; return reached == calls }); err == nil || rejected != nil {
		if rejected != nil {
			rejected.release()
		}
		t.Fatal("terminal model packet cancellation accepted")
	}
	if after := owner.Stats(); after.Entries != before.Entries || after.Bytes != before.Bytes || after.PinnedBytes != before.PinnedBytes {
		t.Fatal("terminal model packet cancellation leaked child or revoked caller", after)
	}
	if model.Bricks[0].Values[0] != 3 {
		t.Fatal("cancel changed borrowed model")
	}
}

func TestC3g8ModelAdoptionConflictOriginAndConcurrentOwnership(t *testing.T) {
	path, h := c3g8Fixture(t, [3]uint32{2, 0, 4})
	packets := make([]*compiledAssetPacket, 6)
	for i := range packets {
		packets[i] = c3h12aPacket(t, path, NewRuntimeContentLoader())
	}
	assets := &AssetServer{}
	var wg sync.WaitGroup
	ids := make(chan AssetId, 6)
	errs := make(chan error, 6)
	for _, p := range packets {
		wg.Add(1)
		go func(p *compiledAssetPacket) {
			defer wg.Done()
			prepared, err := publishCompiledAssetPacket(p, assets, nil)
			if err != nil {
				errs <- err
				return
			}
			ids <- prepared.parts["model"].model
		}(p)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	close(ids)
	var shared AssetId
	for id := range ids {
		if shared != (AssetId{}) && shared != id {
			t.Fatal("concurrent model ownership duplicated")
		}
		shared = id
	}
	key := "compiled-asset-model:" + h.Models[0].ContentID
	assets.mu.RLock()
	count := len(assets.voxModels)
	assets.mu.RUnlock()
	conflict := c3h12aPacket(t, path, NewRuntimeContentLoader())
	s := conflict.shapes[h.Models[0].ContentID]
	assets.mu.Lock()
	assets.authoredVoxelBases[shared][s.lattice] = "conflicting-base"
	assets.mu.Unlock()
	if id, ok := assets.adoptCompiledAssetModelGeometry(s.contentID, s.lattice, s.baseIdentity, s.dimensions, s.source, s.registration); ok || id != (AssetId{}) || s.registration.charge() != 0 {
		t.Fatal("warm base conflict accepted or handle retained")
	}
	if len(assets.voxModels) != count || assets.voxModelKeys[key] != shared {
		t.Fatal("conflict overwrote global owner")
	}
	assets.mu.Lock()
	assets.voxModelKeys[key] = makeAssetId()
	assets.mu.Unlock()
	stale := c3h12aPacket(t, path, NewRuntimeContentLoader())
	if prepared, err := publishCompiledAssetPacket(stale, assets, nil); err == nil || prepared != nil {
		t.Fatal("stale model key bypassed")
	}
	origin := NewRuntimeContentLoader().NewScope()
	origin.Close()
	pending := c3h12aPacket(t, path, nil)
	before := pending.shapes[h.Models[0].ContentID].registration.charge()
	if prepared, err := publishCompiledAssetPacket(pending, &AssetServer{}, origin.Loader()); err == nil || prepared != nil {
		t.Fatal("closed origin published")
	}
	if pending.shapes[h.Models[0].ContentID].registration.charge() != before {
		t.Fatal("rejected caller-owned handle unexpectedly consumed")
	}
	if prepared, err := publishCompiledAssetPacket(pending, nil, nil); err != nil || prepared == nil || prepared.parts["model"].model != (AssetId{}) || prepared.parts["model"].palette != (AssetId{}) {
		t.Fatal("nil publisher model metadata result changed", err)
	}
	if pending.shapes[h.Models[0].ContentID].registration.charge() != before {
		t.Fatal("nil publisher consumed model registration")
	}
	pending.release()
	if !reflect.DeepEqual(pending.shapes[h.Models[0].ContentID].dimensions, [3]uint32{2, 0, 4}) {
		t.Fatal("release erased immutable dimensions")
	}
}
