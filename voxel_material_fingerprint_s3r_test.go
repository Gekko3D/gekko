package gekko

import (
	"math"
	"strings"
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func s3rBuilds(t *testing.T, state *VoxelRtState, want uint64) {
	t.Helper()
	if state.VoxelMaterialFingerprintBuildCount != want {
		t.Fatalf("fingerprint builds=%d, want %d", state.VoxelMaterialFingerprintBuildCount, want)
	}
	if state.VoxelMaterialFingerprintBytes > VoxelMaterialFingerprintBudgetBytes {
		t.Fatalf("fingerprint bytes=%d exceeds budget %d", state.VoxelMaterialFingerprintBytes, VoxelMaterialFingerprintBudgetBytes)
	}
}

func s3rOwned(t *testing.T, state *VoxelRtState, want int) {
	t.Helper()
	if state.VoxelMaterialFingerprintCount != want {
		t.Fatalf("fingerprint count=%d, want %d", state.VoxelMaterialFingerprintCount, want)
	}
	if (state.VoxelMaterialFingerprintBytes == 0) != (want == 0) {
		t.Fatalf("fingerprint bytes=%d inconsistent with count=%d", state.VoxelMaterialFingerprintBytes, want)
	}
	s3rBuilds(t, state, state.VoxelMaterialFingerprintBuildCount)
}

func s3rMaterial(t *testing.T, state *VoxelRtState, entity EntityId) core.Material {
	t.Helper()
	obj := state.GetVoxelObject(entity)
	if obj == nil || len(obj.MaterialTable) <= 1 {
		t.Fatalf("entity %d missing renderer material", entity)
	}
	return obj.MaterialTable[1]
}

func s3rPalette(t *testing.T, server *AssetServer, id AssetId) VoxelPaletteAsset {
	t.Helper()
	p, ok := server.GetVoxelPalette(id)
	if !ok {
		t.Fatalf("palette %v missing", id)
	}
	return p
}

// The public getter returns a value whose maps and slices remain mutable aliases.
// Tests mutate those aliases directly; whole-value replacements below only set up
// same-ID fixtures because the server has no public palette replacement method.
func s3rReplace(server *AssetServer, id AssetId, palette VoxelPaletteAsset) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.voxPalettes[id] = palette
}

func TestS3rSharedPaletteReuseKeepsBridgeLive(t *testing.T) {
	f := newS3cVoxelFixture(t)
	first, second := f.add(1), f.add(2)
	f.app.FlushCommands()
	f.sync()
	s3rBuilds(t, f.state, 1)
	s3rOwned(t, f.state, 1)
	obj := f.state.GetVoxelObject(first)
	f.sync()
	s3rBuilds(t, f.state, 1)
	tr := s3cComponent[TransformComponent](t, f.cmd, first)
	tr.Position, tr.Scale = mgl32.Vec3{7, 8, 9}, mgl32.Vec3{2, 3, 4}
	model := s3cComponent[VoxelModelComponent](t, f.cmd, first)
	model.DisableShadows, model.ShadowGroupID = true, 23
	model.OverrideGeometry = f.server.CreateCubeModel(2, 3, 5, 1)
	f.sync()
	s3rBuilds(t, f.state, 1)
	geometry, _ := f.server.GetVoxelGeometry(model.OverrideGeometry)
	if f.state.GetVoxelObject(first) != obj || obj.Transform.Position != tr.Position || obj.Transform.Scale != tr.Scale || obj.XBrickMap != geometry.XBrickMap || obj.CastsShadows || obj.ShadowGroupID != 23 {
		t.Fatal("fingerprint reuse skipped live transform/geometry/metadata processing")
	}
	obj.Transform.Position = mgl32.Vec3{99, 99, 99}
	f.sync()
	s3rBuilds(t, f.state, 1)
	if obj.Transform.Position != tr.Position {
		t.Fatal("unchanged inputs must still repair renderer transform destination")
	}
	other := f.server.CreateSimplePalette([4]uint8{210, 30, 40, 255})
	s3cComponent[VoxelModelComponent](t, f.cmd, second).VoxelPalette = other
	f.sync()
	s3rBuilds(t, f.state, 2)
	s3rOwned(t, f.state, 2)
	if s3rMaterial(t, f.state, first).BaseColor != ([4]uint8{40, 80, 120, 255}) || s3rMaterial(t, f.state, second).BaseColor != ([4]uint8{210, 30, 40, 255}) {
		t.Fatal("palette switch changed unrelated object materials")
	}
	p := s3rPalette(t, f.server, other)
	p.VoxPalette[1] = [4]uint8{10, 200, 90, 255}
	s3rReplace(f.server, other, p)
	f.sync()
	s3rBuilds(t, f.state, 3)
	if s3rMaterial(t, f.state, second).BaseColor != p.VoxPalette[1] || s3rMaterial(t, f.state, first).BaseColor != ([4]uint8{40, 80, 120, 255}) {
		t.Fatal("same-ID color edit failed to refresh only its users")
	}
}

func TestS3rAliasedPropertiesAndExactInputEquality(t *testing.T) {
	f := newS3cVoxelFixture(t)
	p := s3rPalette(t, f.server, f.palette)
	p.Materials = []VoxMaterial{{ID: 1, Property: map[string]interface{}{"_rough": float32(.25)}}}
	p.SurfaceMaterials = map[uint8]VoxelSurfaceMaterial{1: {Kind: "stone"}}
	s3rReplace(f.server, f.palette, p)
	entity := f.add(1)
	f.add(2)
	f.app.FlushCommands()
	f.sync()
	s3rBuilds(t, f.state, 1)
	alias := s3rPalette(t, f.server, f.palette)
	alias.Materials[0].Property["_rough"] = float32(.75)
	f.sync()
	s3rBuilds(t, f.state, 2)
	if s3rMaterial(t, f.state, entity).Roughness != .75 {
		t.Fatal("aliased property edit must reach real MaterialTable")
	}
	// float64 has the same old hash/output as float32, but differs as input.
	alias.Materials[0].Property["_rough"] = float64(.75)
	f.sync()
	s3rBuilds(t, f.state, 3)
	alias.Materials[0].Property["_rough"] = math.Nextafter(.75, 1)
	f.sync()
	s3rBuilds(t, f.state, 4)
	if s3rMaterial(t, f.state, entity).Roughness != .75 {
		t.Fatal("exact comparison must preserve original float64 hash/material semantics")
	}
	alias.Materials[0].Property["metadata"] = int(4)
	f.sync()
	s3rBuilds(t, f.state, 5)
	alias.Materials[0].Property["metadata"] = "four"
	f.sync()
	s3rBuilds(t, f.state, 6)
	delete(alias.Materials[0].Property, "_rough")
	f.sync()
	s3rBuilds(t, f.state, 7)
	if s3rMaterial(t, f.state, entity).Roughness != 1 {
		t.Fatal("property removal must restore material default")
	}
	alias.SurfaceMaterials[1] = VoxelSurfaceMaterial{Kind: "glass", Tags: []string{"edited"}}
	f.sync()
	s3rBuilds(t, f.state, 7)
	// A present zero override differs from an absent map entry.
	alias.MaterialFrameOverrides = map[uint8]VoxelPaletteMaterialFrameOverride{1: {}}
	s3rReplace(f.server, f.palette, alias)
	f.sync()
	s3rBuilds(t, f.state, 8)
	delete(s3rPalette(t, f.server, f.palette).MaterialFrameOverrides, 1)
	f.sync()
	s3rBuilds(t, f.state, 9)
	s3rOwned(t, f.state, 1)
}

func TestS3rAnimationPhasesAndAliasedNestedInputs(t *testing.T) {
	f := newS3cVoxelFixture(t)
	p := s3rPalette(t, f.server, f.palette)
	p.Animations = []VoxelPaletteAnimation{{
		ID: "s3r", Kind: "material_sequence", FPS: 2, Mode: "loop", PaletteIndices: []uint8{1},
		Frames: []VoxelPaletteAnimationFrame{
			{Colors: [][4]uint8{{10, 20, 30, 255}}, Roughness: []float32{.8}},
			{Colors: [][4]uint8{{90, 100, 110, 255}}, Emission: []float32{2}, Roughness: []float32{.2}},
		},
		UVScroll: &VoxelPaletteUVScroll{Velocity: [2]float32{1, 2}}, Tags: []string{"initial"},
	}}
	p.MaterialFrameOverrides = map[uint8]VoxelPaletteMaterialFrameOverride{1: {HasTransparency: true, Transparency: .3}}
	s3rReplace(f.server, f.palette, p)
	entity := f.add(1)
	f.app.FlushCommands()
	at := func(elapsed float64) { voxelRtSystem(nil, f.state, f.server, &Time{Elapsed: elapsed}, f.cmd, nil) }
	at(0)
	s3rBuilds(t, f.state, 1)
	at(.1)
	s3rBuilds(t, f.state, 1)
	if mat := s3rMaterial(t, f.state, entity); mat.BaseColor != ([4]uint8{10, 20, 30, 255}) || mat.Roughness != .8 || mat.Transparency != .3 {
		t.Fatal("first effective phase material mismatch")
	}
	at(.5)
	s3rBuilds(t, f.state, 2)
	if mat := s3rMaterial(t, f.state, entity); mat.BaseColor != ([4]uint8{90, 100, 110, 255}) || mat.Emission != 2 || mat.Roughness != .2 {
		t.Fatal("elapsed phase change must refresh materials")
	}
	alias := s3rPalette(t, f.server, f.palette)
	alias.Animations[0].Frames[1].Colors[0] = [4]uint8{150, 160, 170, 255}
	alias.Animations[0].Frames[1].Roughness[0] = .4
	at(.6)
	s3rBuilds(t, f.state, 3)
	if mat := s3rMaterial(t, f.state, entity); mat.BaseColor != ([4]uint8{150, 160, 170, 255}) || mat.Roughness != .4 {
		t.Fatal("same-phase nested frame edit must refresh materials")
	}
	alias.MaterialFrameOverrides[1] = VoxelPaletteMaterialFrameOverride{HasTransparency: true, Transparency: .6}
	at(.6)
	s3rBuilds(t, f.state, 4)
	if s3rMaterial(t, f.state, entity).Transparency != .6 {
		t.Fatal("effective override map edit must refresh materials")
	}
	// An inactive frame is still part of the original fingerprint input.
	alias.Animations[0].Frames[0].Emission = []float32{3}
	at(.6)
	s3rBuilds(t, f.state, 5)
	alias.Animations[0].UVScroll.Velocity[0] = 4
	alias.Animations[0].Tags[0] = "edited"
	at(.6)
	s3rBuilds(t, f.state, 6)
	at(.6)
	s3rBuilds(t, f.state, 6)
	s3rOwned(t, f.state, 1)
}

func TestS3rFloatBitsPreserveNaNAndSignedZero(t *testing.T) {
	f := newS3cVoxelFixture(t)
	entity := f.add(1)
	f.app.FlushCommands()
	p := s3rPalette(t, f.server, f.palette)
	p.Roughness = math.Float32frombits(0x7fc01234)
	s3rReplace(f.server, f.palette, p)
	f.sync()
	s3rBuilds(t, f.state, 1)
	f.sync()
	s3rBuilds(t, f.state, 1)
	for i, bits := range []uint32{0x7fc01235, 0, 0x80000000, 0} {
		p.Roughness = math.Float32frombits(bits)
		s3rReplace(f.server, f.palette, p)
		f.sync()
		s3rBuilds(t, f.state, uint64(i+2))
		f.sync()
		s3rBuilds(t, f.state, uint64(i+2))
	}
	if s3rMaterial(t, f.state, entity).BaseColor != p.VoxPalette[1] {
		t.Fatal("bit-exact fingerprint reuse changed original material output")
	}
}

func TestS3rUnsupportedAndOversizedReplacementFallback(t *testing.T) {
	f := newS3cVoxelFixture(t)
	p := s3rPalette(t, f.server, f.palette)
	p.Materials = []VoxMaterial{{ID: 1, Property: map[string]interface{}{"_rough": float32(.2)}}}
	s3rReplace(f.server, f.palette, p)
	entity := f.add(1)
	f.add(2)
	f.app.FlushCommands()
	f.sync()
	s3rBuilds(t, f.state, 1)
	s3rOwned(t, f.state, 1)
	alias := s3rPalette(t, f.server, f.palette)
	alias.Materials[0].Property["unsupported"] = []float32{1, 2}
	alias.Materials[0].Property["_rough"] = float32(.7)
	f.sync()
	s3rBuilds(t, f.state, 2)
	s3rOwned(t, f.state, 0)
	if s3rMaterial(t, f.state, entity).Roughness != .7 {
		t.Fatal("unsupported property must preserve original material fallback")
	}
	f.sync()
	s3rBuilds(t, f.state, 3)
	delete(alias.Materials[0].Property, "unsupported")
	f.sync()
	s3rBuilds(t, f.state, 4)
	s3rOwned(t, f.state, 1)
	p = s3rPalette(t, f.server, f.palette)
	p.SourcePath = strings.Repeat("x", VoxelMaterialFingerprintBudgetBytes+1)
	p.Materials[0].Property["_rough"] = float32(.4)
	s3rReplace(f.server, f.palette, p)
	f.sync()
	s3rBuilds(t, f.state, 5)
	s3rOwned(t, f.state, 0)
	if s3rMaterial(t, f.state, entity).Roughness != .4 {
		t.Fatal("oversized replacement must preserve material output")
	}
	f.sync()
	s3rBuilds(t, f.state, 6)
	p.SourcePath = "small"
	s3rReplace(f.server, f.palette, p)
	f.sync()
	s3rBuilds(t, f.state, 7)
	s3rOwned(t, f.state, 1)
}

func TestS3rGlobalBudgetPressurePreservesEveryMaterial(t *testing.T) {
	if VoxelMaterialFingerprintBudgetBytes != 8<<20 {
		t.Fatalf("fingerprint budget=%d, want 8 MiB", VoxelMaterialFingerprintBudgetBytes)
	}
	f := newS3cVoxelFixture(t)
	want := make(map[EntityId][4]uint8)
	for i := 0; i < 12; i++ {
		color := [4]uint8{uint8(i + 1), 80, 120, 255}
		p := VoxelPaletteAsset{SourcePath: strings.Repeat(string(rune('a'+i)), 1<<20)}
		p.VoxPalette[1] = color
		model := f.voxelModel()
		model.VoxelPalette = f.server.CreateVoxelPaletteAsset(p)
		want[f.cmd.AddEntity(s3cTransform(float32(i)), model)] = color
	}
	f.app.FlushCommands()
	f.sync()
	s3rBuilds(t, f.state, uint64(len(want)))
	if f.state.VoxelMaterialFingerprintCount <= 0 || f.state.VoxelMaterialFingerprintCount >= len(want) {
		t.Fatalf("pressure must retain a bounded subset, got %d of %d", f.state.VoxelMaterialFingerprintCount, len(want))
	}
	f.sync()
	builds := f.state.VoxelMaterialFingerprintBuildCount
	if builds <= uint64(len(want)) || builds > uint64(2*len(want)) {
		t.Fatalf("pressure fallback builds=%d outside expected range", builds)
	}
	s3rBuilds(t, f.state, builds)
	for entity, color := range want {
		if s3rMaterial(t, f.state, entity).BaseColor != color {
			t.Fatalf("budget pressure lost entity %d material", entity)
		}
		f.cmd.RemoveEntity(entity)
	}
	f.app.FlushCommands()
	f.sync()
	s3rOwned(t, f.state, 0)
}

func TestS3rSnapshotUsersPruningAndServerIdentity(t *testing.T) {
	f := newS3cVoxelFixture(t)
	first, second := f.add(1), f.add(2)
	f.app.FlushCommands()
	f.sync()
	f.cmd.RemoveEntity(first)
	f.app.FlushCommands()
	f.sync()
	s3rBuilds(t, f.state, 1)
	s3rOwned(t, f.state, 1)
	// Identical inputs and IDs on another server still require a new hash.
	other := newVoxelRtAssetServerTest(t)
	geometry, _ := f.server.GetVoxelGeometry(f.model)
	other.voxModels[f.model] = geometry
	s3rReplace(other, f.palette, s3rPalette(t, f.server, f.palette))
	f.server = other
	f.sync()
	s3rBuilds(t, f.state, 2)
	s3rOwned(t, f.state, 1)
	f.state.RtApp.RegisterFeature(&app_rt.SpriteFeature{})
	for _, exclusion := range []string{"hidden", "sprite", "missing_geometry", "nil_server"} {
		t.Run(exclusion, func(t *testing.T) {
			switch exclusion {
			case "hidden":
				f.cmd.AddComponents(second, VoxelRenderHiddenComponent{})
				f.app.FlushCommands()
			case "sprite":
				f.cmd.AddComponents(second, EntityLODComponent{SelectionValid: true, ActiveRepresentation: EntityLODRepresentationDot})
				f.app.FlushCommands()
			case "missing_geometry":
				s3cComponent[VoxelModelComponent](t, f.cmd, second).VoxelModel = makeAssetId()
			}
			if exclusion == "nil_server" {
				voxelRtSystem(nil, f.state, nil, &Time{}, f.cmd, nil)
			} else {
				f.sync()
			}
			s3rOwned(t, f.state, 0)
			if f.state.GetVoxelObject(second) != nil {
				t.Fatal("excluded candidate retained voxel object")
			}
			switch exclusion {
			case "hidden":
				f.cmd.RemoveComponents(second, VoxelRenderHiddenComponent{})
			case "sprite":
				if f.state.RuntimeSpriteCount() != 1 {
					t.Fatal("sprite exclusion must retain sprite processing")
				}
				f.cmd.RemoveComponents(second, EntityLODComponent{})
			case "missing_geometry":
				s3cComponent[VoxelModelComponent](t, f.cmd, second).VoxelModel = f.model
			}
			f.app.FlushCommands()
			builds := f.state.VoxelMaterialFingerprintBuildCount
			f.sync()
			s3rBuilds(t, f.state, builds+1)
			s3rOwned(t, f.state, 1)
			s3rMaterial(t, f.state, second)
		})
	}
	f.cmd.RemoveEntity(second)
	f.app.FlushCommands()
	f.sync()
	s3rOwned(t, f.state, 0)
}

func TestS3rHiddenStreamedPaletteRecoveryAndTicketContinuity(t *testing.T) {
	f := newStreamedVoxelFixture(t)
	paletteID := f.model.VoxelPalette
	p := s3rPalette(t, f.server, paletteID)
	delete(f.server.voxPalettes, paletteID)
	f.sync()
	s3rBuilds(t, f.state, 0)
	s3rOwned(t, f.state, 0)
	streamedStatus(t, f.state, 101, StreamedVoxelRenderFailed)
	s3rReplace(f.server, paletteID, p)
	f.marker.Ticket = 102 // Failed tickets remain terminal; recovery gets a new ticket.
	f.sync()
	s3rBuilds(t, f.state, 1)
	s3rOwned(t, f.state, 1)
	obj := f.state.GetVoxelObject(f.entity)
	if obj == nil || obj.RenderEnabled {
		t.Fatal("hidden streamed object must use materials and remain resident")
	}
	streamedStatus(t, f.state, 101, StreamedVoxelRenderFailed)
	streamedStatus(t, f.state, 102, StreamedVoxelRenderUploading)
	f.marker.Ticket, f.marker.Generation, f.marker.Priority = 103, 10, StreamedVoxelPriorityCollision
	f.sync()
	s3rBuilds(t, f.state, 1)
	streamedStatus(t, f.state, 102, StreamedVoxelRenderCancelled)
	status := streamedStatus(t, f.state, 103, StreamedVoxelRenderUploading)
	if f.state.GetVoxelObject(f.entity) != obj || status.Entity != f.entity || status.Generation != 10 || obj.VoxelUploadOrder != 103 || obj.VoxelUploadPriority != uint8(StreamedVoxelPriorityCollision) {
		t.Fatal("fingerprint reuse skipped streamed ticket adoption")
	}
	f.cmd.RemoveEntity(f.entity)
	f.flush()
	f.sync()
	s3rOwned(t, f.state, 0)
}
