package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestCapsuleColliderSnapshotScalesRadiusHalfHeightAndAABB(t *testing.T) {
	cmd, _, world, proxy, _ := newPhysicsBootstrapHarness()
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{1, 2, 3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{2, 3, 4}},
		&RigidBodyComponent{Mass: 1},
		&ColliderComponent{Shape: ShapeCapsule, Radius: 0.5, CapsuleHalfHeight: 2},
	)
	cmd.app.FlushCommands()

	PhysicsPushSystem(cmd, &Time{Dt: 1.0 / 60.0}, world, proxy)
	snapshot := proxy.pendingState.Load()
	if snapshot == nil || len(snapshot.Entities) != 1 {
		t.Fatalf("expected one capsule-only physics snapshot entity, got %#v", snapshot)
	}

	state := snapshot.Entities[0]
	if state.Shape != ShapeCapsule {
		t.Fatalf("expected capsule shape in snapshot, got %v", state.Shape)
	}
	if state.Radius != 2 {
		t.Fatalf("expected capsule radius scaled by max X/Z scale to 2, got %.3f", state.Radius)
	}
	if state.CapsuleHalfHeight != 6 {
		t.Fatalf("expected capsule half-height scaled by Y scale to 6, got %.3f", state.CapsuleHalfHeight)
	}
	if len(state.Model.Boxes) != 0 || state.Model.Grid != nil {
		t.Fatalf("expected capsule-only snapshot not to synthesize a box model, got %+v", state.Model)
	}

	body := &internalBody{Eid: state.Eid}
	syncInternalBody(body, state, true)
	body.updateAABB()

	wantMin := mgl32.Vec3{-1, -6, 1}
	wantMax := mgl32.Vec3{3, 10, 5}
	if body.aabbMin != wantMin || body.aabbMax != wantMax {
		t.Fatalf("expected capsule AABB %v..%v, got %v..%v", wantMin, wantMax, body.aabbMin, body.aabbMax)
	}
}

func TestCapsuleAABBUsesRotatedSegmentEndpoints(t *testing.T) {
	body := &internalBody{
		pos:               mgl32.Vec3{},
		rot:               mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 0, 1}),
		shape:             ShapeCapsule,
		radius:            0.5,
		capsuleHalfHeight: 2,
	}
	body.updateAABB()

	wantMin := mgl32.Vec3{-2.5, -0.5, -0.5}
	wantMax := mgl32.Vec3{2.5, 0.5, 0.5}
	if vec3MaxDiff(body.aabbMin, wantMin) > 1e-5 || vec3MaxDiff(body.aabbMax, wantMax) > 1e-5 {
		t.Fatalf("expected rotated capsule AABB %v..%v, got %v..%v", wantMin, wantMax, body.aabbMin, body.aabbMax)
	}
}

func TestCapsuleSphereOverlapAndPairOrder(t *testing.T) {
	capsule := capsulePrimitive{a: mgl32.Vec3{0, -1, 0}, b: mgl32.Vec3{0, 1, 0}, radius: 0.5}
	contact, ok := checkCapsuleSphereCollision(capsule, mgl32.Vec3{0, 1.8, 0}, 0.5)
	if !ok {
		t.Fatal("expected capsule-sphere overlap")
	}
	if contact.normal != (mgl32.Vec3{0, -1, 0}) {
		t.Fatalf("expected normal from sphere toward capsule, got %v", contact.normal)
	}
	if absf(contact.penetration-0.2) > 1e-5 {
		t.Fatalf("expected penetration 0.2, got %.6f", contact.penetration)
	}

	bodyCapsule := &internalBody{shape: ShapeCapsule, radius: 0.5, capsuleHalfHeight: 1, rot: mgl32.QuatIdent()}
	bodySphere := &internalBody{pos: mgl32.Vec3{0, 1.8, 0}, shape: ShapeSphere, radius: 0.5, rot: mgl32.QuatIdent()}
	contacts := collectNarrowPhaseContacts(bodySphere, bodyCapsule, 0.01, nil)
	if len(contacts) == 0 {
		t.Fatal("expected reversed sphere-capsule pair to produce contact")
	}
	if contacts[0].normal != (mgl32.Vec3{0, 1, 0}) {
		t.Fatalf("expected reversed pair normal from capsule toward sphere, got %v", contacts[0].normal)
	}

	if _, ok := checkCapsuleSphereCollision(capsule, mgl32.Vec3{0, 2.1, 0}, 0.5); ok {
		t.Fatal("expected separated capsule-sphere pair not to collide")
	}
}

func TestCapsuleOBBOverlapProducesStableContact(t *testing.T) {
	capsule := capsulePrimitive{a: mgl32.Vec3{0, 0.8, 0}, b: mgl32.Vec3{0, 2.0, 0}, radius: 0.5}
	contact, ok := checkCapsuleOBBCollision(
		capsule,
		mgl32.Vec3{},
		mgl32.QuatIdent(),
		CollisionBox{HalfExtents: mgl32.Vec3{2, 0.5, 2}},
	)
	if !ok {
		t.Fatal("expected capsule-box overlap")
	}
	if contact.normal != (mgl32.Vec3{0, 1, 0}) {
		t.Fatalf("expected normal from box toward capsule, got %v", contact.normal)
	}
	if absf(contact.penetration-0.2) > 0.02 {
		t.Fatalf("expected penetration near 0.2, got %.6f", contact.penetration)
	}
}

func TestCapsuleCapsuleOverlapAndNonOverlap(t *testing.T) {
	capsuleA := capsulePrimitive{a: mgl32.Vec3{0, -1, 0}, b: mgl32.Vec3{0, 1, 0}, radius: 0.5}
	capsuleB := capsulePrimitive{a: mgl32.Vec3{0.8, -1, 0}, b: mgl32.Vec3{0.8, 1, 0}, radius: 0.5}
	contact, ok := checkCapsuleCapsuleCollision(capsuleA, capsuleB)
	if !ok {
		t.Fatal("expected capsule-capsule overlap")
	}
	if contact.normal != (mgl32.Vec3{-1, 0, 0}) {
		t.Fatalf("expected normal from capsule B toward capsule A, got %v", contact.normal)
	}
	if absf(contact.penetration-0.2) > 1e-5 {
		t.Fatalf("expected penetration 0.2, got %.6f", contact.penetration)
	}

	capsuleB.a[0] = 1.2
	capsuleB.b[0] = 1.2
	if _, ok := checkCapsuleCapsuleCollision(capsuleA, capsuleB); ok {
		t.Fatal("expected separated capsules not to collide")
	}
}

func TestCapsuleVoxelOverlapProducesContact(t *testing.T) {
	grid := testSolidGrid{
		size:   [3]int{3, 1, 3},
		vScale: mgl32.Vec3{1, 1, 1},
	}
	center := mgl32.Vec3{1.5, 0.5, 1.5}

	capsuleBody := &internalBody{
		Eid:               1,
		pos:               mgl32.Vec3{1.5, 1.2, 1.5},
		rot:               mgl32.QuatIdent(),
		shape:             ShapeCapsule,
		radius:            0.5,
		capsuleHalfHeight: 0.4,
	}
	voxelBody := &internalBody{
		Eid:      2,
		pos:      center,
		rot:      mgl32.QuatIdent(),
		isStatic: true,
		model: PhysicsModel{
			CenterOffset: center,
			Boxes:        []CollisionBox{{HalfExtents: center}},
			Grid:         grid,
		},
		boxes: []InternalBox{{Box: CollisionBox{HalfExtents: center}}},
	}
	capsuleBody.updateAABB()
	voxelBody.updateAABB()

	contacts := collectNarrowPhaseContacts(capsuleBody, voxelBody, 0.01, nil)
	if len(contacts) == 0 {
		t.Fatal("expected capsule-only body to collide with voxel grid")
	}
	if contacts[0].normal.Y() <= 0 {
		t.Fatalf("expected normal from voxel grid toward capsule, got %v", contacts[0].normal)
	}
}

func TestCapsuleBoxCollisionResolvesInSynchronousPhysics(t *testing.T) {
	cmd, _, world, proxy, sim, timeRes := newPhysicsSceneHarness()
	world.UpdateFrequency = 120
	timeRes.Dt = 1.0 / float64(world.UpdateFrequency)

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, -1, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{IsStatic: true, Mass: 0},
		&ColliderComponent{Friction: 0.6, Restitution: 0.0},
		&PhysicsModel{Boxes: []CollisionBox{{HalfExtents: mgl32.Vec3{6, 1, 6}}}},
	)

	capsuleID := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 3, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Mass: 1, GravityScale: 1, LinearDamping: 0.01, AngularDamping: 0.01},
		&ColliderComponent{Shape: ShapeCapsule, Radius: 0.5, CapsuleHalfHeight: 1, Friction: 0.3, Restitution: 0.0},
	)
	cmd.app.FlushCommands()

	stepSynchronousPhysics(cmd, world, proxy, sim, timeRes, 180)
	position, ok := testEntityPosition(cmd, capsuleID)
	if !ok {
		t.Fatal("expected capsule transform after stepping")
	}
	if position.Y() < 1.2 {
		t.Fatalf("expected capsule to be held above the box floor, got %v", position)
	}
	if len(proxy.DrainCollisionEvents()) == 0 {
		t.Fatal("expected capsule-box collision events")
	}
}

func TestCapsuleTriggerReportsEnterAndExitWithoutResolution(t *testing.T) {
	cmd, _, world, proxy, sim, timeRes := newPhysicsSceneHarness()
	world.UpdateFrequency = 120
	timeRes.Dt = 1.0 / float64(world.UpdateFrequency)

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{IsStatic: true, Mass: 0},
		&ColliderComponent{Shape: ShapeCapsule, Radius: 2, CapsuleHalfHeight: 1, IsTrigger: true, CollisionLayer: DefaultCollisionLayer, CollisionMask: AllCollisionLayers},
	)

	sphereID := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 6, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Mass: 1, GravityScale: 1, LinearDamping: 0.01, AngularDamping: 0.01},
		&ColliderComponent{Shape: ShapeSphere, Radius: 0.5, Friction: 0.3, Restitution: 0.0},
	)
	cmd.app.FlushCommands()

	stepSynchronousPhysics(cmd, world, proxy, sim, timeRes, 220)
	position, ok := testEntityPosition(cmd, sphereID)
	if !ok {
		t.Fatal("expected dynamic sphere transform after capsule trigger test")
	}
	if position.Y() >= -2.0 {
		t.Fatalf("expected sphere to pass through capsule trigger without physical resolution, got %v", position)
	}

	events := proxy.DrainCollisionEvents()
	var sawEnter, sawExit bool
	for _, event := range events {
		if !event.IsTrigger {
			continue
		}
		sawEnter = sawEnter || event.Type == CollisionEventEnter
		sawExit = sawExit || event.Type == CollisionEventExit
	}
	if !sawEnter {
		t.Fatal("expected capsule trigger enter event")
	}
	if !sawExit {
		t.Fatal("expected capsule trigger exit event")
	}
}

func TestCapsuleLayerMaskFilteringSkipsPairs(t *testing.T) {
	cmd, _, world, proxy, sim, timeRes := newPhysicsSceneHarness()
	world.UpdateFrequency = 120
	timeRes.Dt = 1.0 / float64(world.UpdateFrequency)

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{IsStatic: true, Mass: 0},
		&ColliderComponent{Shape: ShapeCapsule, Radius: 2, CapsuleHalfHeight: 1, IsTrigger: true, CollisionLayer: 1 << 0, CollisionMask: 1 << 0},
	)

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Mass: 1},
		&ColliderComponent{Shape: ShapeSphere, Radius: 0.5, CollisionLayer: 1 << 1, CollisionMask: 1 << 1},
	)
	cmd.app.FlushCommands()

	stepSynchronousPhysics(cmd, world, proxy, sim, timeRes, 2)
	if events := proxy.DrainCollisionEvents(); len(events) != 0 {
		t.Fatalf("expected masked-out capsule pair to emit no events, got %d", len(events))
	}
}

func TestCapsuleColliderUsesSolidCapsuleInertia(t *testing.T) {
	body := &internalBody{
		mass:              5,
		shape:             ShapeCapsule,
		radius:            1,
		capsuleHalfHeight: 2,
	}

	inertia := calculateLocalInertiaTensor(body)
	diag := mat3Diagonal(inertia)
	if absf(diag.X()-13.3125) > 1e-4 || absf(diag.Y()-2.375) > 1e-4 || absf(diag.Z()-13.3125) > 1e-4 {
		t.Fatalf("expected solid capsule inertia diagonal near [13.3125 2.375 13.3125], got %v", diag)
	}
}

func TestSyncInternalBodyRecalculatesInertiaWhenCapsuleDimensionsChange(t *testing.T) {
	initialState := PhysicsEntityState{
		Eid:               8,
		Pos:               mgl32.Vec3{},
		Rot:               mgl32.QuatIdent(),
		Mass:              5,
		Shape:             ShapeCapsule,
		Radius:            1,
		CapsuleHalfHeight: 1,
	}
	editedState := initialState
	editedState.CapsuleHalfHeight = 2

	body := &internalBody{Eid: initialState.Eid}
	syncInternalBody(body, initialState, true)
	initialInvInertia := body.invInertiaLocal

	syncInternalBody(body, editedState, false)
	if diff := mat3MaxDiff(initialInvInertia, body.invInertiaLocal); diff <= 1e-4 {
		t.Fatalf("expected capsule dimension change to recalculate inverse inertia, max diff was %.6f", diff)
	}
}
