package gekko

import (
	"math"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func p4StaticPalette() VoxelPaletteAsset {
	var colors VoxPalette
	colors[1] = [4]uint8{40, 80, 120, 255}
	return VoxelPaletteAsset{VoxPalette: colors, SurfaceMaterials: map[uint8]VoxelSurfaceMaterial{1: {Kind: "stone", Tags: []string{"solid"}}}}
}

// Give independently published palettes distinct runtime IDs, as may happen
// across asset publication paths. Identity is observed through renderer objects.
func p4AddPaletteObject(f *s3cVoxelFixture, palette VoxelPaletteAsset, x float32) (EntityId, AssetId) {
	id := makeAssetId()
	f.server.mu.Lock()
	f.server.voxPalettes[id] = palette
	f.server.mu.Unlock()
	model := f.voxelModel()
	model.VoxelPalette = id
	return f.cmd.AddEntity(s3cTransform(x), model), id
}

func p4MaterialObject(t *testing.T, f *s3cVoxelFixture, entity EntityId) *core.VoxelObject {
	t.Helper()
	obj := f.state.GetVoxelObject(entity)
	if obj == nil || len(obj.MaterialTable) != 256 {
		t.Fatalf("entity %d missing complete material table", entity)
	}
	return obj
}

func p4Binding(t *testing.T, obj *core.VoxelObject) *core.ImmutableMaterialTable {
	t.Helper()
	binding := obj.ImmutableMaterialTable()
	if binding == nil {
		t.Fatal("static palette lacks immutable material binding")
	}
	return binding
}

func TestP4BridgeEqualSemanticsAcrossAssetIDsAndIdleReuse(t *testing.T) {
	f := newS3cVoxelFixture(t)
	a, aid := p4AddPaletteObject(f, p4StaticPalette(), 1)
	b, bid := p4AddPaletteObject(f, p4StaticPalette(), 2)
	if aid == bid {
		t.Fatal("fixture must publish distinct palette IDs")
	}
	f.app.FlushCommands()
	f.sync()
	aObj, bObj := p4MaterialObject(t, f, a), p4MaterialObject(t, f, b)
	aBinding, bBinding := p4Binding(t, aObj), p4Binding(t, bObj)
	if aBinding.Identity() != bBinding.Identity() {
		t.Fatal("equal full semantics must certify equal GPU sharing identity across asset IDs")
	}
	before := f.state.VoxelMaterialTableCacheStats()
	builds := f.state.VoxelMaterialSemanticIdentityBuildCount
	if builds != 2 || f.state.VoxelMaterialSemanticIdentityCount != 2 || f.state.VoxelMaterialSemanticIdentityBytes == 0 {
		t.Fatal("distinct published palette IDs must retain two owned semantic snapshots")
	}
	f.sync()
	if p4Binding(t, aObj) != aBinding || p4Binding(t, bObj) != bBinding {
		t.Fatal("idle sync replaced immutable bindings")
	}
	if after := f.state.VoxelMaterialTableCacheStats(); after.Builds != before.Builds || after.Bytes != before.Bytes {
		t.Fatalf("idle sync rebuilt or expanded retained material tables: before=%+v after=%+v", before, after)
	}
	if f.state.VoxelMaterialSemanticIdentityBuildCount != builds {
		t.Fatal("idle sync serialized unchanged semantic identities")
	}
}

func TestP4BridgeSurfaceTagAliasMutationChangesOnlySemanticIdentity(t *testing.T) {
	f := newS3cVoxelFixture(t)
	a, aid := p4AddPaletteObject(f, p4StaticPalette(), 1)
	b, _ := p4AddPaletteObject(f, p4StaticPalette(), 2)
	f.app.FlushCommands()
	f.sync()
	aObj, bObj := p4MaterialObject(t, f, a), p4MaterialObject(t, f, b)
	aBinding, bBinding := p4Binding(t, aObj), p4Binding(t, bObj)
	oldRow := aObj.MaterialTable[1]
	builds := f.state.VoxelMaterialSemanticIdentityBuildCount
	palette, _ := f.server.GetVoxelPalette(aid)
	palette.SurfaceMaterials[1].Tags[0] = "breakable"
	f.sync()
	if p4Binding(t, aObj).Identity() == aBinding.Identity() {
		t.Fatal("same-ID nested surface-tag edit retained obsolete semantic identity")
	}
	if p4Binding(t, bObj).Identity() != bBinding.Identity() || aObj.MaterialTable[1] != oldRow || bObj.MaterialTable[1] != oldRow {
		t.Fatal("surface-only edit changed another binding or rendered material rows")
	}
	if f.state.VoxelMaterialSemanticIdentityBuildCount != builds+1 {
		t.Fatal("one surface-tag edit must serialize exactly one replacement semantic identity")
	}
}

func TestP4BridgePaletteColorChangeIsolatedFromOtherBinding(t *testing.T) {
	f := newS3cVoxelFixture(t)
	a, aid := p4AddPaletteObject(f, p4StaticPalette(), 1)
	b, _ := p4AddPaletteObject(f, p4StaticPalette(), 2)
	f.app.FlushCommands()
	f.sync()
	aObj, bObj := p4MaterialObject(t, f, a), p4MaterialObject(t, f, b)
	aBinding, bBinding := p4Binding(t, aObj), p4Binding(t, bObj)
	f.server.mu.Lock()
	palette := f.server.voxPalettes[aid]
	palette.VoxPalette[1] = [4]uint8{210, 30, 40, 255}
	f.server.voxPalettes[aid] = palette
	f.server.mu.Unlock()
	f.sync()
	if aObj.MaterialTable[1].BaseColor != palette.VoxPalette[1] || p4Binding(t, aObj).Identity() == aBinding.Identity() {
		t.Fatal("palette edit failed to replace rendered rows and semantic binding")
	}
	if bObj.MaterialTable[1].BaseColor != ([4]uint8{40, 80, 120, 255}) || p4Binding(t, bObj) != bBinding {
		t.Fatal("palette edit altered independent placement")
	}
}

func TestP4BridgeAnimationOverridesAndUnsupportedIdentityStayPrivate(t *testing.T) {
	for _, kind := range []string{"animation", "override", "unsupported-json", "nested-property", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			f := newS3cVoxelFixture(t)
			palette := p4StaticPalette()
			switch kind {
			case "animation":
				palette.Animations = []VoxelPaletteAnimation{{ID: "pulse", PaletteIndices: []uint8{1}, Frames: []VoxelPaletteAnimationFrame{{Colors: [][4]uint8{{40, 80, 120, 255}}}}}}
			case "override":
				palette.MaterialFrameOverrides = map[uint8]VoxelPaletteMaterialFrameOverride{1: {Emission: 2, HasEmission: true}}
			case "unsupported-json":
				palette.Materials = []VoxMaterial{{ID: 1, Property: map[string]interface{}{"metadata": math.NaN()}}}
			case "nested-property":
				palette.Materials = []VoxMaterial{{ID: 1, Property: map[string]interface{}{"metadata": map[string]interface{}{"tag": "mutable"}}}}
			case "oversized":
				palette.SurfaceMaterials[1] = VoxelSurfaceMaterial{Kind: "stone", Tags: []string{strings.Repeat("x", VoxelMaterialSemanticIdentityBudgetBytes+1)}}
			}
			entity, _ := p4AddPaletteObject(f, palette, 1)
			f.app.FlushCommands()
			f.sync()
			obj := p4MaterialObject(t, f, entity)
			if obj.ImmutableMaterialTable() != nil {
				t.Fatal("dynamic or unencodable palette received static sharing certification")
			}
			if obj.MaterialTable[1].BaseColor != ([4]uint8{40, 80, 120, 255}) {
				t.Fatal("private fallback changed rendered color")
			}
			if f.state.VoxelMaterialSemanticIdentityCount != 0 || f.state.VoxelMaterialSemanticIdentityBytes != 0 || f.state.VoxelMaterialSemanticIdentityBuildCount != 0 {
				t.Fatal("ineligible semantic input was retained or serialized before support/budget preflight")
			}
		})
	}
}

func TestP4BridgeSemanticCaptureOncePerPaletteAndOwnedLifetime(t *testing.T) {
	f := newS3cVoxelFixture(t)
	f.state.SetVoxelMaterialTableCacheBudgetBytes(-1)
	entities := []EntityId{f.add(1), f.add(2), f.add(3)}
	f.app.FlushCommands()
	f.sync()
	borrowed := p4Binding(t, p4MaterialObject(t, f, entities[0]))
	want := borrowed.MaterialTable()[1]
	if f.state.VoxelMaterialSemanticIdentityBuildCount != 1 || f.state.VoxelMaterialSemanticIdentityCount != 1 || f.state.VoxelMaterialSemanticIdentityBytes == 0 || f.state.VoxelMaterialSemanticIdentityBytes > VoxelMaterialSemanticIdentityBudgetBytes {
		t.Fatal("same-palette placements must capture one bounded semantic snapshot")
	}
	f.sync()
	if f.state.VoxelMaterialSemanticIdentityBuildCount != 1 {
		t.Fatal("idle same-palette placements repeated canonical serialization")
	}
	for _, entity := range entities {
		f.cmd.RemoveEntity(entity)
	}
	f.app.FlushCommands()
	f.sync()
	if f.state.VoxelMaterialSemanticIdentityCount != 0 || f.state.VoxelMaterialSemanticIdentityBytes != 0 || f.state.VoxelMaterialTableCacheStats().Bytes != 0 {
		t.Fatal("final instance release retained semantic or disabled warm-table ownership")
	}
	if borrowed.MaterialTable()[1] != want {
		t.Fatal("cache cleanup altered independently borrowed immutable material")
	}
}

func TestP4BridgeServerReplacementReleasesSemanticOwnership(t *testing.T) {
	f := newS3cVoxelFixture(t)
	entity := f.add(1)
	f.app.FlushCommands()
	f.sync()
	borrowed := p4Binding(t, p4MaterialObject(t, f, entity))
	want := borrowed.MaterialTable()[1]
	f.server = newVoxelRtAssetServerTest(t)
	f.sync()
	if f.state.VoxelMaterialSemanticIdentityCount != 0 || f.state.VoxelMaterialSemanticIdentityBytes != 0 {
		t.Fatal("server replacement retained semantic snapshots from previous asset owner")
	}
	if borrowed.MaterialTable()[1] != want {
		t.Fatal("server replacement cleared borrowed immutable rows")
	}
}

func TestP4BridgeRawMaterialMutationSurvivesIdleSyncAndIsolatesInstances(t *testing.T) {
	f := newS3cVoxelFixture(t)
	a := f.add(1)
	b := f.add(2)
	f.app.FlushCommands()
	f.sync()
	aObj, bObj := p4MaterialObject(t, f, a), p4MaterialObject(t, f, b)
	p4Binding(t, aObj)
	bBinding := p4Binding(t, bObj)
	want := [4]uint8{200, 210, 220, 255}
	aObj.MaterialTable[1].BaseColor = want
	if aObj.ImmutableMaterialTable() != nil {
		t.Fatal("raw material mutation must detach immutable proof")
	}
	f.sync()
	f.sync()
	if aObj.ImmutableMaterialTable() != nil || aObj.MaterialTable[1].BaseColor != want {
		t.Fatal("unchanged palette sync recertified or overwrote raw material mutation")
	}
	if bObj.MaterialTable[1].BaseColor != ([4]uint8{40, 80, 120, 255}) || p4Binding(t, bObj) != bBinding {
		t.Fatal("raw material mutation escaped into another instance")
	}
}
