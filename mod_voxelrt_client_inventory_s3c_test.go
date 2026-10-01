package gekko

import (
	"reflect"
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

type s3cVoxelFixture struct {
	app            *App
	cmd            *Commands
	server         *AssetServer
	state          *VoxelRtState
	model, palette AssetId
}

func newS3cVoxelFixture(t *testing.T) *s3cVoxelFixture {
	t.Helper()
	f := &s3cVoxelFixture{app: NewApp(), server: newVoxelRtAssetServerTest(t), state: newVoxelRtStateTest()}
	f.cmd = f.app.Commands()
	f.model = f.server.CreateCubeModel(4, 4, 4, 1)
	f.palette = f.server.CreateSimplePalette([4]uint8{40, 80, 120, 255})
	return f
}

func s3cTransform(x float32) TransformComponent {
	return TransformComponent{Position: mgl32.Vec3{x, 0, -10}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}
}

func (f *s3cVoxelFixture) voxelModel() VoxelModelComponent {
	return VoxelModelComponent{VoxelModel: f.model, VoxelPalette: f.palette, VoxelResolution: 1, PivotMode: PivotModeCorner}
}

func (f *s3cVoxelFixture) add(x float32, extra ...any) EntityId {
	return f.cmd.AddEntity(append([]any{s3cTransform(x), f.voxelModel()}, extra...)...)
}

func (f *s3cVoxelFixture) sync() {
	voxelRtSystem(nil, f.state, f.server, &Time{Dt: 1.0 / 60.0}, f.cmd, nil)
}

func s3cComponent[T any](t *testing.T, cmd *Commands, entity EntityId) *T {
	t.Helper()
	var zero T
	value, ok := cmd.GetComponent(entity, reflect.TypeOf(zero)).(*T)
	if !ok || value == nil {
		t.Fatalf("entity %d missing %T", entity, zero)
	}
	return value
}

func s3cInventory(t *testing.T, state *VoxelRtState, builds uint64, candidates int) {
	t.Helper()
	if state.VoxelCandidateInventoryBuildCount != builds || state.VoxelCandidateCount != candidates {
		t.Fatalf("inventory builds/candidates=%d/%d, want %d/%d", state.VoxelCandidateInventoryBuildCount, state.VoxelCandidateCount, builds, candidates)
	}
}

func s3cResident(t *testing.T, f *s3cVoxelFixture, entity EntityId, x float32, geometry AssetId) *core.VoxelObject {
	t.Helper()
	obj := f.state.GetVoxelObject(entity)
	asset, ok := f.server.GetVoxelGeometry(geometry)
	if obj == nil || !ok || obj.XBrickMap != asset.XBrickMap || obj.Transform.Position != (mgl32.Vec3{x, 0, -10}) {
		t.Fatalf("entity %d renderer geometry/position mismatched: object=%+v geometry=%v x=%v", entity, obj, geometry, x)
	}
	return obj
}

func TestS3cVoxelInventoryEmptyIdleAndCameraLights(t *testing.T) {
	f := newS3cVoxelFixture(t)
	s3cInventory(t, f.state, 0, 0)
	f.sync()
	s3cInventory(t, f.state, 1, 0)
	f.sync()
	s3cInventory(t, f.state, 1, 0)
	entity := f.cmd.AddEntity(s3cTransform(0), &CameraComponent{Position: mgl32.Vec3{1, 2, 3}, LookAt: mgl32.Vec3{0, 0, -1}}, &LightComponent{Type: LightTypePoint, Color: [3]float32{1, 0, 0}, Intensity: 2, Range: 20})
	f.app.FlushCommands()
	f.sync()
	s3cInventory(t, f.state, 2, 0)
	revision := f.cmd.StructuralRevision()
	s3cComponent[CameraComponent](t, f.cmd, entity).Position = mgl32.Vec3{4, 5, 6}
	s3cComponent[LightComponent](t, f.cmd, entity).Intensity = 7
	f.sync()
	s3cInventory(t, f.state, 2, 0)
	if f.cmd.StructuralRevision() != revision || f.state.RtApp.Camera.Position != (mgl32.Vec3{4, 5, 6}) || len(f.state.RtApp.Scene.Lights) != 1 || f.state.RtApp.Scene.Lights[0].Color[3] != 7 {
		t.Fatal("empty inventory reuse must still synchronize direct camera/light changes")
	}
}

func TestS3cVoxelInventoryBufferedAdmissionAndRequiredComponents(t *testing.T) {
	for _, required := range []string{"transform", "model"} {
		t.Run(required, func(t *testing.T) {
			f := newS3cVoxelFixture(t)
			var entity EntityId
			var missing any
			if required == "transform" {
				entity, missing = f.cmd.AddEntity(f.voxelModel()), s3cTransform(3)
			} else {
				entity, missing = f.cmd.AddEntity(s3cTransform(3)), f.voxelModel()
			}
			f.app.FlushCommands()
			f.sync()
			s3cInventory(t, f.state, 1, 0)
			f.cmd.AddComponents(entity, missing)
			f.sync()
			s3cInventory(t, f.state, 1, 0)
			if f.state.GetVoxelObject(entity) != nil {
				t.Fatal("queued admission must remain invisible")
			}
			f.app.FlushCommands()
			f.sync()
			s3cInventory(t, f.state, 2, 1)
			obj := s3cResident(t, f, entity, 3, f.model)
			f.cmd.RemoveComponents(entity, missing)
			f.sync()
			s3cInventory(t, f.state, 2, 1)
			if f.state.GetVoxelObject(entity) != obj {
				t.Fatal("queued removal must retain renderer object")
			}
			f.app.FlushCommands()
			f.sync()
			s3cInventory(t, f.state, 3, 0)
			if f.state.GetVoxelObject(entity) != nil || len(f.state.RtApp.Scene.Objects) != 0 {
				t.Fatal("required-component removal must discard residency")
			}
		})
	}
}

func TestS3cVoxelInventoryGrowthReplacementAndRecycledRows(t *testing.T) {
	f := newS3cVoxelFixture(t)
	first := f.add(1)
	f.app.FlushCommands()
	f.sync()
	firstObj := s3cResident(t, f, first, 1, f.model)
	s3cInventory(t, f.state, 1, 1)
	want := map[EntityId]float32{first: 1}
	for i := 2; i <= 65; i++ {
		want[f.add(float32(i))] = float32(i)
	}
	f.sync()
	s3cInventory(t, f.state, 1, 1)
	f.app.FlushCommands()
	f.sync()
	s3cInventory(t, f.state, 2, len(want))
	for entity, x := range want {
		s3cResident(t, f, entity, x, f.model)
	}
	if f.state.GetVoxelObject(first) != firstObj {
		t.Fatal("same-archetype growth must preserve existing renderer identity")
	}

	replacementGeometry := f.server.CreateCubeModel(6, 2, 2, 1)
	replacement := f.voxelModel()
	replacement.VoxelModel = replacementGeometry
	f.cmd.AddComponents(first, s3cTransform(101), replacement)
	f.sync()
	s3cInventory(t, f.state, 2, len(want))
	s3cResident(t, f, first, 1, f.model)
	f.app.FlushCommands()
	f.sync()
	s3cInventory(t, f.state, 3, len(want))
	if s3cResident(t, f, first, 101, replacementGeometry) != firstObj {
		t.Fatal("same-archetype replacement must update the existing renderer object")
	}
	for entity, x := range want {
		if entity != first {
			s3cResident(t, f, entity, x, f.model)
		}
	}

	f.cmd.RemoveEntity(first)
	f.app.FlushCommands()
	f.sync()
	s3cInventory(t, f.state, 4, len(want)-1)
	if f.state.GetVoxelObject(first) != nil {
		t.Fatal("removed entity must leave renderer")
	}
	delete(want, first)
	recycled := f.add(202)
	f.app.FlushCommands()
	f.sync()
	s3cInventory(t, f.state, 5, len(want)+1)
	s3cResident(t, f, recycled, 202, f.model)
	for entity, x := range want {
		s3cResident(t, f, entity, x, f.model)
	}
	if len(f.state.RtApp.Scene.Objects) != len(want)+1 {
		t.Fatal("row reuse must retain exact live scene membership")
	}
	for entity := range want {
		f.cmd.RemoveEntity(entity)
	}
	f.cmd.RemoveEntity(recycled)
	f.app.FlushCommands()
	f.sync()
	s3cInventory(t, f.state, 6, 0)
	if len(f.state.RtApp.Scene.Objects) != 0 {
		t.Fatal("empty inventory must release scene residency")
	}
	f.sync()
	s3cInventory(t, f.state, 6, 0)
	reused := f.add(303)
	f.app.FlushCommands()
	f.sync()
	s3cInventory(t, f.state, 7, 1)
	s3cResident(t, f, reused, 303, f.model)
	if f.state.GetVoxelObject(first) != nil || f.state.GetVoxelObject(recycled) != nil || len(f.state.RtApp.Scene.Objects) != 1 {
		t.Fatal("existing empty archetype reuse must exclude retired IDs")
	}
}

func TestS3cVoxelInventoryDirectValuesGeometryAndMaterials(t *testing.T) {
	f := newS3cVoxelFixture(t)
	entity := f.add(1)
	f.app.FlushCommands()
	f.sync()
	obj := s3cResident(t, f, entity, 1, f.model)
	revision := f.cmd.StructuralRevision()
	f.sync()
	s3cInventory(t, f.state, 1, 1)
	if f.state.GetVoxelObject(entity) != obj {
		t.Fatal("idle frame must retain renderer object identity")
	}
	tr := s3cComponent[TransformComponent](t, f.cmd, entity)
	tr.Position, tr.Scale = mgl32.Vec3{7, 8, 9}, mgl32.Vec3{2, 3, 4}
	tr.Rotation = mgl32.QuatRotate(0.5, mgl32.Vec3{0, 1, 0})
	model := s3cComponent[VoxelModelComponent](t, f.cmd, entity)
	model.DisableShadows, model.DisableOcclusionCulling, model.ShadowGroupID = true, true, 23
	model.PivotMode, model.CustomPivot = PivotModeCustom, mgl32.Vec3{1, 2, 3}
	geometryID := f.server.CreateCubeModel(2, 3, 5, 1)
	model.OverrideGeometry = geometryID
	model.VoxelPalette = f.server.CreateSimplePalette([4]uint8{120, 30, 50, 255})
	f.sync()
	geometry, _ := f.server.GetVoxelGeometry(geometryID)
	if f.state.GetVoxelObject(entity) != obj || obj.XBrickMap != geometry.XBrickMap || obj.Transform.Position != tr.Position || obj.Transform.Rotation != tr.Rotation || obj.Transform.Scale != tr.Scale || obj.Transform.Pivot != model.CustomPivot || obj.CastsShadows || obj.AllowOcclusionCulling || obj.ShadowGroupID != 23 || obj.MaterialTable[1].BaseColor != ([4]uint8{120, 30, 50, 255}) {
		t.Fatal("cached candidates must apply direct transform/model/geometry/palette changes to the real object")
	}
	palette := f.server.voxPalettes[model.VoxelPalette]
	palette.VoxPalette[1] = [4]uint8{10, 200, 90, 255}
	f.server.voxPalettes[model.VoxelPalette] = palette
	f.sync()
	if obj.MaterialTable[1].BaseColor != palette.VoxPalette[1] {
		t.Fatal("in-place asset palette edits must reach cached candidates")
	}
	model.VoxelPalette = f.server.CreateVoxelPaletteAsset(VoxelPaletteAsset{VoxPalette: palette.VoxPalette, Animations: []VoxelPaletteAnimation{{ID: "s3c", Kind: "palette_sequence", FPS: 2, Mode: "loop", PaletteIndices: []uint8{1}, Frames: []VoxelPaletteAnimationFrame{{Colors: [][4]uint8{{10, 20, 30, 255}}}, {Colors: [][4]uint8{{90, 100, 110, 255}}}}}}})
	voxelRtSystem(nil, f.state, f.server, &Time{Elapsed: 0}, f.cmd, nil)
	if obj.MaterialTable[1].BaseColor != ([4]uint8{10, 20, 30, 255}) {
		t.Fatal("animated palette first frame must reach renderer")
	}
	voxelRtSystem(nil, f.state, f.server, &Time{Elapsed: 0.5}, f.cmd, nil)
	if obj.MaterialTable[1].BaseColor != ([4]uint8{90, 100, 110, 255}) {
		t.Fatal("elapsed-time material processing must run on reused candidates")
	}
	s3cInventory(t, f.state, 1, 1)
	if f.cmd.StructuralRevision() != revision {
		t.Fatal("value and asset edits must preserve structural stamp")
	}
}

func TestS3cVoxelInventoryExcludedCandidatesAndLateGeometry(t *testing.T) {
	f := newS3cVoxelFixture(t)
	hidden := f.add(1, VoxelRenderHiddenComponent{})
	missing := makeAssetId()
	model := f.voxelModel()
	model.VoxelModel = missing
	late := f.cmd.AddEntity(s3cTransform(2), model)
	f.app.FlushCommands()
	f.sync()
	s3cInventory(t, f.state, 1, 2)
	if f.state.GetVoxelObject(hidden) != nil || f.state.GetVoxelObject(late) != nil || len(f.state.RtApp.Scene.Objects) != 0 {
		t.Fatal("excluded candidates must count without renderer residency")
	}
	revision := f.cmd.StructuralRevision()
	// Asset loading makes the same reference resolve, without an ECS mutation.
	loaded, _ := f.server.GetVoxelGeometry(f.model)
	f.server.voxModels[missing] = loaded
	f.sync()
	s3cInventory(t, f.state, 1, 2)
	s3cResident(t, f, late, 2, missing)
	if f.cmd.StructuralRevision() != revision || f.state.GetVoxelObject(hidden) != nil {
		t.Fatal("late asset adoption must reuse membership and preserve hidden exclusion")
	}
	f.cmd.RemoveComponents(hidden, VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.sync()
	s3cInventory(t, f.state, 2, 2)
	s3cResident(t, f, hidden, 1, f.model)
}

func TestS3cVoxelInventoryCachedStreamedTicketsAndPriority(t *testing.T) {
	f := newStreamedVoxelFixture(t)
	f.sync()
	s3cInventory(t, f.state, 1, 1)
	obj := f.state.GetVoxelObject(f.entity)
	if obj == nil || obj.RenderEnabled {
		t.Fatal("hidden streamed candidate must remain resident")
	}
	revision := f.cmd.StructuralRevision()
	f.marker.Ticket, f.marker.Generation, f.marker.Priority = 102, 10, StreamedVoxelPriorityCollision
	f.sync()
	s3cInventory(t, f.state, 1, 1)
	streamedStatus(t, f.state, 101, StreamedVoxelRenderCancelled)
	status := streamedStatus(t, f.state, 102, StreamedVoxelRenderUploading)
	if f.cmd.StructuralRevision() != revision || f.state.GetVoxelObject(f.entity) != obj || status.Entity != f.entity || status.Generation != 10 || obj.VoxelUploadOrder != 102 || obj.VoxelUploadPriority != uint8(StreamedVoxelPriorityCollision) || obj.RenderEnabled {
		t.Fatal("cached streamed candidate must apply direct ticket/generation/priority changes")
	}
}

func TestS3cVoxelInventoryCameraDependentLODTransitions(t *testing.T) {
	f := newS3cVoxelFixture(t)
	f.state.RtApp.RegisterFeature(&app_rt.SpriteFeature{})
	camera := f.cmd.AddEntity(&CameraComponent{Position: mgl32.Vec3{0, 0, 0}, LookAt: mgl32.Vec3{0, 0, -1}, Up: mgl32.Vec3{0, 1, 0}})
	entity := f.add(0, &EntityLODComponent{Bands: []EntityLODBand{{MaxDistance: 20, Representation: EntityLODRepresentationFullVoxel}, {MaxDistance: 0, Representation: EntityLODRepresentationDot}}})
	f.app.FlushCommands()
	entityLODSelectionSystem(f.cmd, f.state)
	f.sync()
	s3cResident(t, f, entity, 0, f.model)
	revision := f.cmd.StructuralRevision()
	s3cComponent[CameraComponent](t, f.cmd, camera).Position = mgl32.Vec3{0, 0, 100}
	entityLODSelectionSystem(f.cmd, f.state)
	f.sync()
	s3cInventory(t, f.state, 1, 1)
	if f.state.GetVoxelObject(entity) != nil || f.state.RuntimeSpriteCount() != 1 || len(f.state.RtApp.Scene.Objects) != 0 {
		t.Fatal("reused voxel candidate must transition to sprite LOD")
	}
	s3cComponent[CameraComponent](t, f.cmd, camera).Position = mgl32.Vec3{}
	entityLODSelectionSystem(f.cmd, f.state)
	f.sync()
	s3cInventory(t, f.state, 1, 1)
	s3cResident(t, f, entity, 0, f.model)
	if f.cmd.StructuralRevision() != revision || f.state.RuntimeSpriteCount() != 0 {
		t.Fatal("sprite-excluded candidate must return to voxel LOD without structural notification")
	}
}

func TestS3cVoxelInventoryHierarchyWritesReachBridge(t *testing.T) {
	f := newS3cVoxelFixture(t)
	parent := f.cmd.AddEntity(s3cTransform(1))
	other := f.cmd.AddEntity(s3cTransform(20))
	child := f.cmd.AddEntity(s3cTransform(0), f.voxelModel(), &Parent{Entity: parent}, &LocalTransformComponent{Position: mgl32.Vec3{2, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	f.app.FlushCommands()
	TransformHierarchySystem(f.cmd)
	f.sync()
	obj := s3cResident(t, f, child, 3, f.model)
	revision := f.cmd.StructuralRevision()
	s3cComponent[TransformComponent](t, f.cmd, parent).Position[0] = 5
	TransformHierarchySystem(f.cmd)
	f.sync()
	if s3cResident(t, f, child, 7, f.model) != obj {
		t.Fatal("ancestor motion must preserve renderer identity")
	}
	s3cComponent[LocalTransformComponent](t, f.cmd, child).Position[0] = 4
	TransformHierarchySystem(f.cmd)
	f.sync()
	s3cResident(t, f, child, 9, f.model)
	s3cComponent[Parent](t, f.cmd, child).Entity = other
	TransformHierarchySystem(f.cmd)
	f.sync()
	s3cResident(t, f, child, 24, f.model)
	s3cInventory(t, f.state, 1, 1)
	if f.cmd.StructuralRevision() != revision {
		t.Fatal("ancestor/local/reparent field propagation must preserve membership stamp")
	}
}

func TestS3cVoxelInventoryEqualRevisionOwnerSwitch(t *testing.T) {
	f := newS3cVoxelFixture(t)
	old := f.add(1)
	f.cmd.AddEntity(s3cTransform(2))
	f.app.FlushCommands()
	f.sync()
	s3cInventory(t, f.state, 1, 1)

	other := NewApp()
	cmd := other.Commands()
	cmd.AddEntity(s3cRevisionMarker{})
	// Different membership and type-registration order at the same stamp.
	next := cmd.AddEntity(f.voxelModel(), s3cTransform(42))
	other.FlushCommands()
	if cmd.StructuralRevision() != f.cmd.StructuralRevision() || next == old {
		t.Fatal("fixture must switch owners at equal stamps with distinct candidate IDs")
	}
	voxelRtSystem(nil, f.state, f.server, &Time{}, cmd, nil)
	s3cInventory(t, f.state, 2, 1)
	f.cmd = cmd
	s3cResident(t, f, next, 42, f.model)
	if f.state.GetVoxelObject(old) != nil || len(f.state.RtApp.Scene.Objects) != 1 {
		t.Fatal("equal-stamp owner switch must discard previous membership")
	}
	voxelRtSystem(nil, f.state, f.server, &Time{}, cmd, nil)
	s3cInventory(t, f.state, 2, 1)
}
