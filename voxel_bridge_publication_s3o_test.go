package gekko

import (
	"math"
	"reflect"
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/go-gl/mathgl/mgl32"
)

type s3oModelValue struct {
	model       VoxelModelComponent
	customPivot [3]uint32
}

func s3oVecBits(v mgl32.Vec3) [3]uint32 {
	return [3]uint32{math.Float32bits(v[0]), math.Float32bits(v[1]), math.Float32bits(v[2])}
}

func s3oModels(cmd *Commands) map[EntityId]s3oModelValue {
	result := map[EntityId]s3oModelValue{}
	MakeQuery1[VoxelModelComponent](cmd).Map(func(id EntityId, model *VoxelModelComponent) bool {
		value := s3oModelValue{*model, s3oVecBits(model.CustomPivot)}
		value.model.CustomPivot = mgl32.Vec3{}
		result[id] = value
		return true
	})
	return result
}

// Assert committed values and membership through public queries, and treat
// aggregate publication revisions as opaque changed/unchanged stamps.
func s3oStep(t *testing.T, cmd *Commands, call func(), normalized []EntityId, pivots map[EntityId]mgl32.Vec3) {
	t.Helper()
	wantValues, wantModels := s3nSnapshot(cmd), s3oModels(cmd)
	for _, id := range normalized {
		value, present := wantModels[id]
		if !present || value.model.SharedGeometry != (AssetId{}) || value.model.VoxelModel == (AssetId{}) {
			t.Fatalf("invalid normalization expectation for entity %d", id)
		}
		value.model.SharedGeometry = value.model.VoxelModel
		wantModels[id] = value
	}
	for id, pivot := range pivots {
		if wantValues.pivots[id] == s3oVecBits(pivot) {
			t.Fatalf("invalid changed Pivot expectation for entity %d", id)
		}
		wantValues.pivots[id] = s3oVecBits(pivot)
	}
	physics := map[EntityId]PhysicsModel{}
	MakeQuery1[PhysicsModel](cmd).Map(func(id EntityId, model *PhysicsModel) bool {
		physics[id] = *model
		return true
	})
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	modelType, physicsType := reflect.TypeOf(VoxelModelComponent{}), reflect.TypeOf(PhysicsModel{})
	modelRevision, physicsRevision := cmd.ComponentRevision(modelType), cmd.ComponentRevision(physicsType)
	call()
	if !reflect.DeepEqual(s3nSnapshot(cmd), wantValues) || !reflect.DeepEqual(s3oModels(cmd), wantModels) {
		t.Error("bridge changed committed membership or fields beyond expected SharedGeometry/Pivot")
	}
	afterPhysics := map[EntityId]PhysicsModel{}
	MakeQuery1[PhysicsModel](cmd).Map(func(id EntityId, model *PhysicsModel) bool {
		afterPhysics[id] = *model
		return true
	})
	if !reflect.DeepEqual(afterPhysics, physics) || cmd.ComponentRevision(physicsType) != physicsRevision {
		t.Error("bridge applied or published queued PhysicsModel before flush")
	}
	after := s3gRevisions(cmd)
	for i, typ := range s3gTypes {
		if got, want := after[i] != before[i], i == 0 && len(pivots) != 0; got != want {
			t.Errorf("%v publication changed=%v, want %v", typ, got, want)
		}
	}
	if got, want := cmd.ComponentRevision(modelType) != modelRevision, len(normalized) != 0; got != want {
		t.Errorf("VoxelModelComponent publication changed=%v, want %v", got, want)
	}
	if cmd.StructuralRevision() != structural {
		t.Error("bridge flushed or changed committed structure")
	}
}

func TestS3oNormalizationSystemOrdersAndFilters(t *testing.T) {
	for _, first := range []string{"renderer", "physics"} {
		t.Run(first, func(t *testing.T) {
			f := newS3cVoxelFixture(t)
			_, _, cache := newVoxelPhysicsPrecalcTestHarness()
			valid := f.add(1, RigidBodyComponent{}, s3gUnrelated{1})
			missingModel := f.voxelModel()
			missingModel.VoxelModel = makeAssetId()
			missingTransform := s3cTransform(2)
			missingTransform.Pivot = mgl32.Vec3{9, 8, 7}
			unresolved := f.cmd.AddEntity(missingTransform, missingModel, RigidBodyComponent{}, s3gUnrelated{2})
			normal := f.voxelModel()
			normal.SharedGeometry = f.server.CreateCubeModel(2, 2, 2, 1)
			already := f.cmd.AddEntity(s3cTransform(3), normal, RigidBodyComponent{}, s3gUnrelated{3})
			zero := f.cmd.AddEntity(missingTransform, VoxelModelComponent{}, RigidBodyComponent{}, s3gUnrelated{4})
			hidden := f.add(4, RigidBodyComponent{}, VoxelRenderHiddenComponent{}, s3gUnrelated{5})
			f.app.FlushCommands()
			render := func() { f.sync() }
			physics := func() { VoxPhysicsPreCalcSystem(f.cmd, f.server, nil, cache) }
			// Both existing nil-server guards precede committed normalization.
			s3oStep(t, f.cmd, func() { voxelRtSystem(nil, f.state, nil, &Time{}, f.cmd, nil) }, nil, nil)
			s3oStep(t, f.cmd, func() { VoxPhysicsPreCalcSystem(f.cmd, nil, nil, cache) }, nil, nil)
			if first == "renderer" {
				s3oStep(t, f.cmd, render, []EntityId{valid, unresolved}, nil)
				s3oStep(t, f.cmd, physics, []EntityId{hidden}, nil)
			} else {
				s3oStep(t, f.cmd, physics, []EntityId{valid, unresolved, hidden}, nil)
				s3oStep(t, f.cmd, render, nil, nil)
			}
			s3oStep(t, f.cmd, render, nil, nil)
			s3oStep(t, f.cmd, physics, nil, nil)
			asset, _ := f.server.GetVoxelGeometry(f.model)
			if obj := f.state.GetVoxelObject(already); obj == nil || obj.XBrickMap != asset.XBrickMap {
				t.Fatal("normalization changed legacy VoxelModel geometry precedence")
			}
			if f.state.GetVoxelObject(unresolved) != nil || f.state.GetVoxelObject(zero) != nil || f.state.GetVoxelObject(hidden) != nil {
				t.Fatal("renderer adopted failed, zero or ordinary hidden geometry")
			}
			f.app.FlushCommands()
			if s3cComponent[PhysicsModel](t, f.cmd, valid).Grid == nil || s3cComponent[PhysicsModel](t, f.cmd, unresolved).Grid != nil {
				t.Fatal("physics output changed or queued missing-PhysicsModel addition was lost")
			}
			s3oStep(t, f.cmd, physics, nil, nil) // Existing build skip stays quiet.
			// Compare the actual destination after an external unmarked edit.
			s3cComponent[VoxelModelComponent](t, f.cmd, valid).SharedGeometry = AssetId{}
			s3oStep(t, f.cmd, physics, []EntityId{valid}, nil)
		})
	}
}

func TestS3oRendererLODEarlyReturnAndOwnerlessHelpers(t *testing.T) {
	f := newS3cVoxelFixture(t)
	f.state.RtApp.RegisterFeature(&app_rt.SpriteFeature{})
	tr := s3cTransform(1)
	tr.Pivot = mgl32.Vec3{9, 8, 7}
	lod := f.cmd.AddEntity(tr, f.voxelModel(), EntityLODComponent{SelectionValid: true, ActiveRepresentation: EntityLODRepresentationDot}, s3gUnrelated{1})
	hidden := f.cmd.AddEntity(tr, f.voxelModel(), VoxelRenderHiddenComponent{}, s3gUnrelated{2})
	missingTransform := f.cmd.AddEntity(f.voxelModel(), s3gUnrelated{3})
	f.app.FlushCommands()
	s3oStep(t, f.cmd, f.sync, []EntityId{lod}, nil)
	s3oStep(t, f.cmd, f.sync, nil, nil)
	if f.state.GetVoxelObject(lod) != nil || f.state.RuntimeSpriteCount() != 1 || f.state.GetVoxelObject(hidden) != nil || f.state.GetVoxelObject(missingTransform) != nil {
		t.Fatal("existing LOD, hidden or missing-Transform filters changed")
	}
	// Copy-only edit lookup normalizes its copy without touching ECS storage.
	s3oStep(t, f.cmd, func() {
		copy, ok := voxelModelComponentForEdit(f.cmd, hidden)
		if !ok || copy.SharedGeometry != f.model {
			t.Fatal("copy-only normalization lost existing behavior")
		}
		id, _, ok := ResolveVoxelGeometry(f.server, &copy)
		if !ok || id != f.model {
			t.Fatal("ownerless geometry resolver lost existing API behavior")
		}
	}, nil, nil)
	// Even a supplied committed pointer has no ECS ownership in the resolver.
	before := f.cmd.ComponentRevision(reflect.TypeOf(VoxelModelComponent{}))
	if _, _, ok := ResolveVoxelGeometry(f.server, s3cComponent[VoxelModelComponent](t, f.cmd, hidden)); !ok {
		t.Fatal("pointer resolver failed")
	}
	if s3cComponent[VoxelModelComponent](t, f.cmd, hidden).SharedGeometry != f.model || f.cmd.ComponentRevision(reflect.TypeOf(VoxelModelComponent{})) != before {
		t.Fatal("ownerless resolver must normalize without publishing ECS")
	}
}

func TestS3oRendererPivotExactBitsPreservesOtherFields(t *testing.T) {
	f := newS3cVoxelFixture(t)
	tr := s3cTransform(1)
	tr.Position[0], tr.Rotation.W, tr.Scale[2] = math.Float32frombits(0x7fc04567), math.Float32frombits(0x7fc04568), math.Float32frombits(0x7fc04569)
	model := f.voxelModel()
	model.SharedGeometry, model.PivotMode = f.model, PivotModeCustom
	parent := f.cmd.AddEntity(s3cTransform(10), s3gUnrelated{1})
	id := f.cmd.AddEntity(tr, model, s3lLocal(tr), Parent{Entity: parent}, s3gUnrelated{2})
	f.cmd.AddEntity(s3cTransform(20), s3lLocal(s3cTransform(3)), Parent{Entity: id}, s3gUnrelated{3})
	f.app.FlushCommands()
	for _, step := range []struct {
		bits    uint32
		changed bool
	}{{0, false}, {0x80000000, true}, {0x80000000, false}, {0, true}, {0x7fc01234, true}, {0x7fc01234, false}, {0x7fc01235, true}} {
		pivot := mgl32.Vec3{math.Float32frombits(step.bits), 0, 0}
		s3cComponent[VoxelModelComponent](t, f.cmd, id).CustomPivot = pivot
		var changed map[EntityId]mgl32.Vec3
		if step.changed {
			changed = map[EntityId]mgl32.Vec3{id: pivot}
		}
		s3oStep(t, f.cmd, f.sync, nil, changed)
	}
	// Actual destination edits must be repaired even with unchanged model input.
	s3cComponent[TransformComponent](t, f.cmd, id).Pivot = mgl32.Vec3{9, 8, 7}
	pivot := s3cComponent[VoxelModelComponent](t, f.cmd, id).CustomPivot
	s3oStep(t, f.cmd, f.sync, nil, map[EntityId]mgl32.Vec3{id: pivot})
}

func TestS3oRendererPivotFormulaGeometryAndHierarchy(t *testing.T) {
	f := newS3cVoxelFixture(t)
	parent := f.cmd.AddEntity(s3cTransform(10), s3gUnrelated{1})
	tr := s3cTransform(1)
	tr.Scale = mgl32.Vec3{2, 3, 4}
	model := f.voxelModel()
	model.SharedGeometry = f.server.CreateCubeModel(2, 2, 2, 1)
	model.OverrideGeometry = f.server.CreateCubeModel(6, 2, 2, 1)
	model.VoxelResolution, model.PivotMode = .5, PivotModeCenter
	id := f.cmd.AddEntity(tr, model, s3lLocal(tr), Parent{Entity: parent}, s3gUnrelated{2})
	f.cmd.AddEntity(s3cTransform(20), s3lLocal(s3cTransform(3)), Parent{Entity: id}, s3gUnrelated{3})
	f.app.FlushCommands()
	TransformHierarchySystem(f.cmd)
	s3oStep(t, f.cmd, f.sync, nil, map[EntityId]mgl32.Vec3{id: {3, 1, 1}})
	s3oStep(t, f.cmd, f.sync, nil, nil)
	geometry, _ := f.server.GetVoxelGeometry(model.OverrideGeometry)
	obj := f.state.GetVoxelObject(id)
	if obj == nil || obj.XBrickMap != geometry.XBrickMap || obj.Transform.Scale != (mgl32.Vec3{1, 1.5, 2}) || obj.Transform.Pivot != (mgl32.Vec3{3, 1, 1}) {
		t.Fatal("override precedence, effective scale or source-space center Pivot changed")
	}
	before := s3nSnapshot(f.cmd)
	TransformHierarchySystem(f.cmd)
	if !reflect.DeepEqual(s3nSnapshot(f.cmd), before) {
		t.Fatal("derived renderer Pivot changed later hierarchy TRS")
	}
}

func TestS3oQueuedMutationBoundaries(t *testing.T) {
	for _, writer := range []string{"renderer", "physics"} {
		t.Run(writer, func(t *testing.T) {
			f := newS3cVoxelFixture(t)
			_, _, cache := newVoxelPhysicsPrecalcTestHarness()
			oldPhysics := PhysicsModel{CenterOffset: mgl32.Vec3{50, 60, 70}}
			replace := f.add(1, RigidBodyComponent{}, oldPhysics, s3gUnrelated{1})
			removeEntity := f.add(2, RigidBodyComponent{}, s3gUnrelated{2})
			removeModel := f.add(3, RigidBodyComponent{}, s3gUnrelated{3})
			addModel := f.cmd.AddEntity(s3cTransform(4), RigidBodyComponent{}, s3gUnrelated{4})
			f.app.FlushCommands()
			replacement := f.voxelModel()
			replacement.VoxelModel = f.server.CreateCubeModel(2, 2, 2, 1)
			replacement.PivotMode, replacement.CustomPivot = PivotModeCustom, mgl32.Vec3{3, 4, 5}
			queuedTransform := s3cTransform(99)
			queuedTransform.Pivot = mgl32.Vec3{9, 8, 7}
			f.cmd.AddComponents(replace, replacement, queuedTransform)
			f.cmd.RemoveEntity(removeEntity)
			f.cmd.RemoveComponents(removeModel, VoxelModelComponent{})
			f.cmd.AddComponents(addModel, f.voxelModel())
			pending := f.add(5, RigidBodyComponent{}, s3gUnrelated{5})
			call := f.sync
			if writer == "physics" {
				call = func() { VoxPhysicsPreCalcSystem(f.cmd, f.server, nil, cache) }
			}
			s3oStep(t, f.cmd, call, []EntityId{replace, removeEntity, removeModel}, nil)
			if !f.cmd.EntityExists(removeEntity) || f.cmd.EntityExists(pending) || HasComponent[VoxelModelComponent](f.cmd, addModel) {
				t.Fatal("bridge applied pending additions/removals")
			}
			f.app.FlushCommands()
			if f.cmd.EntityExists(removeEntity) || HasComponent[VoxelModelComponent](f.cmd, removeModel) || !f.cmd.EntityExists(pending) {
				t.Fatal("queued membership mutations were lost")
			}
			if *s3cComponent[VoxelModelComponent](t, f.cmd, replace) != replacement || *s3cComponent[TransformComponent](t, f.cmd, replace) != queuedTransform {
				t.Fatal("queued replacement was lost or rewritten by old committed writer")
			}
			if writer == "physics" {
				// addModel lacked a committed voxel model during the prior call.
				if s3cComponent[PhysicsModel](t, f.cmd, replace).Grid == nil || HasComponent[PhysicsModel](f.cmd, addModel) {
					t.Fatal("queued PhysicsModel replacement was lost or pending model created output early")
				}
			}
			var pivots map[EntityId]mgl32.Vec3
			if writer == "renderer" {
				pivots = map[EntityId]mgl32.Vec3{replace: replacement.CustomPivot}
			}
			s3oStep(t, f.cmd, call, []EntityId{replace, addModel, pending}, pivots)
		})
	}
}
