package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestSphereColliderSnapshotScalesRadiusAndAABB(t *testing.T) {
	cmd, _, world, proxy, _ := newPhysicsBootstrapHarness()
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{1, 2, 3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{2, 0.5, -3}},
		&RigidBodyComponent{Mass: 1},
		&ColliderComponent{Shape: ShapeSphere, Radius: 1.5},
	)
	cmd.app.FlushCommands()

	PhysicsPushSystem(cmd, &Time{Dt: 1.0 / 60.0}, world, proxy)
	snapshot := proxy.pendingState.Load()
	if snapshot == nil || len(snapshot.Entities) != 1 {
		t.Fatalf("expected one sphere-only physics snapshot entity, got %#v", snapshot)
	}

	state := snapshot.Entities[0]
	if state.Shape != ShapeSphere {
		t.Fatalf("expected sphere shape in snapshot, got %v", state.Shape)
	}
	if state.Radius != 4.5 {
		t.Fatalf("expected radius scaled by max absolute transform scale to 4.5, got %.3f", state.Radius)
	}
	if len(state.Model.Boxes) != 0 || state.Model.Grid != nil {
		t.Fatalf("expected sphere-only snapshot not to synthesize a box model, got %+v", state.Model)
	}

	body := &internalBody{Eid: state.Eid}
	syncInternalBody(body, state, true)
	body.updateAABB()

	wantMin := mgl32.Vec3{-3.5, -2.5, -1.5}
	wantMax := mgl32.Vec3{5.5, 6.5, 7.5}
	if body.aabbMin != wantMin || body.aabbMax != wantMax {
		t.Fatalf("expected sphere AABB %v..%v, got %v..%v", wantMin, wantMax, body.aabbMin, body.aabbMax)
	}
}

func TestSphereColliderUsesSolidSphereInertia(t *testing.T) {
	body := &internalBody{
		mass:   5,
		shape:  ShapeSphere,
		radius: 2,
	}

	inertia := calculateLocalInertiaTensor(body)
	diag := mat3Diagonal(inertia)
	wantMoment := float32(8)
	if absf(diag.X()-wantMoment) > 1e-5 || absf(diag.Y()-wantMoment) > 1e-5 || absf(diag.Z()-wantMoment) > 1e-5 {
		t.Fatalf("expected solid sphere inertia diagonal %.6f, got %v", wantMoment, diag)
	}

	invInertia := calculateInverseInertiaLocal(body)
	invDiag := mat3Diagonal(invInertia)
	wantInverse := float32(1.0 / 8.0)
	if absf(invDiag.X()-wantInverse) > 1e-5 || absf(invDiag.Y()-wantInverse) > 1e-5 || absf(invDiag.Z()-wantInverse) > 1e-5 {
		t.Fatalf("expected solid sphere inverse inertia diagonal %.6f, got %v", wantInverse, invDiag)
	}
}

func TestSyncInternalBodyRecalculatesInertiaWhenSphereRadiusChanges(t *testing.T) {
	initialState := PhysicsEntityState{
		Eid:    7,
		Pos:    mgl32.Vec3{},
		Rot:    mgl32.QuatIdent(),
		Mass:   5,
		Shape:  ShapeSphere,
		Radius: 1,
	}
	editedState := initialState
	editedState.Radius = 2

	body := &internalBody{Eid: initialState.Eid}
	syncInternalBody(body, initialState, true)
	initialInvInertia := body.invInertiaLocal

	syncInternalBody(body, editedState, false)
	if diff := mat3MaxDiff(initialInvInertia, body.invInertiaLocal); diff <= 1e-4 {
		t.Fatalf("expected sphere radius change to recalculate inverse inertia, max diff was %.6f", diff)
	}

	wantInverse := float32(1.0 / 8.0)
	diag := mat3Diagonal(body.invInertiaLocal)
	if absf(diag.X()-wantInverse) > 1e-5 || absf(diag.Y()-wantInverse) > 1e-5 || absf(diag.Z()-wantInverse) > 1e-5 {
		t.Fatalf("expected updated sphere inverse inertia diagonal %.6f, got %v", wantInverse, diag)
	}
}

func TestSphereSphereOverlapProducesContact(t *testing.T) {
	contact, ok := checkSphereSphereCollision(mgl32.Vec3{0, 0, 0}, 1, mgl32.Vec3{1.5, 0, 0}, 1)
	if !ok {
		t.Fatal("expected overlapping spheres to produce contact")
	}
	if contact.normal != (mgl32.Vec3{-1, 0, 0}) {
		t.Fatalf("expected normal from body B toward body A, got %v", contact.normal)
	}
	if absf(contact.penetration-0.5) > 1e-5 {
		t.Fatalf("expected penetration 0.5, got %.6f", contact.penetration)
	}
	if absf(contact.point.X()-0.75) > 1e-5 {
		t.Fatalf("expected midpoint contact x near 0.75, got %v", contact.point)
	}
}

func TestSphereSphereNonOverlapProducesNoContact(t *testing.T) {
	if _, ok := checkSphereSphereCollision(mgl32.Vec3{0, 0, 0}, 1, mgl32.Vec3{2.1, 0, 0}, 1); ok {
		t.Fatal("expected separated spheres not to produce contact")
	}
}

func TestSphereOBBOverlapProducesStableContact(t *testing.T) {
	contact, ok := checkSphereOBBCollision(
		mgl32.Vec3{1.4, 0, 0},
		0.5,
		mgl32.Vec3{},
		mgl32.QuatIdent(),
		CollisionBox{HalfExtents: mgl32.Vec3{1, 1, 1}},
	)
	if !ok {
		t.Fatal("expected sphere and box overlap")
	}
	if contact.normal != (mgl32.Vec3{1, 0, 0}) {
		t.Fatalf("expected normal from box toward sphere, got %v", contact.normal)
	}
	if absf(contact.penetration-0.1) > 1e-5 {
		t.Fatalf("expected penetration 0.1, got %.6f", contact.penetration)
	}
	if contact.point != (mgl32.Vec3{1, 0, 0}) {
		t.Fatalf("expected closest box point, got %v", contact.point)
	}
}

func TestSphereVoxelOverlapProducesContact(t *testing.T) {
	voxelScale := mgl32.Vec3{1, 1, 1}
	grid := testSolidGrid{
		size:   [3]int{3, 1, 3},
		vScale: voxelScale,
	}
	center := mgl32.Vec3{1.5, 0.5, 1.5}

	sphereBody := &internalBody{
		Eid:    1,
		pos:    mgl32.Vec3{1.5, 1.2, 1.5},
		rot:    mgl32.QuatIdent(),
		shape:  ShapeSphere,
		radius: 0.5,
	}
	voxelBody := &internalBody{
		Eid:      2,
		pos:      center,
		rot:      mgl32.QuatIdent(),
		isStatic: true,
		model: PhysicsModel{
			CenterOffset: center,
			Boxes: []CollisionBox{{
				HalfExtents: center,
			}},
			Grid: grid,
		},
		boxes: []InternalBox{{
			Box: CollisionBox{
				HalfExtents: center,
			},
		}},
	}
	sphereBody.updateAABB()
	voxelBody.updateAABB()

	contacts := collectNarrowPhaseContacts(sphereBody, voxelBody, 0.01, nil)
	if len(contacts) == 0 {
		t.Fatal("expected sphere-only body to collide with voxel grid")
	}
	if contacts[0].normal.Y() <= 0 {
		t.Fatalf("expected normal from voxel grid toward sphere, got %v", contacts[0].normal)
	}

	reversedContacts := collectNarrowPhaseContacts(voxelBody, sphereBody, 0.01, nil)
	if len(reversedContacts) == 0 {
		t.Fatal("expected voxel grid to collide with sphere-only body when pair order is reversed")
	}
	if reversedContacts[0].normal.Y() >= 0 {
		t.Fatalf("expected reversed pair normal from sphere toward voxel grid, got %v", reversedContacts[0].normal)
	}
}

func TestSphereBoxCollisionResolvesInSynchronousPhysics(t *testing.T) {
	cmd, _, world, proxy, sim, timeRes := newPhysicsSceneHarness()
	world.UpdateFrequency = 120
	timeRes.Dt = 1.0 / float64(world.UpdateFrequency)

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, -1, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{IsStatic: true, Mass: 0},
		&ColliderComponent{Friction: 0.6, Restitution: 0.0},
		&PhysicsModel{Boxes: []CollisionBox{{HalfExtents: mgl32.Vec3{6, 1, 6}}}},
	)

	sphereID := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 3, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Mass: 1, GravityScale: 1, LinearDamping: 0.01, AngularDamping: 0.01},
		&ColliderComponent{Shape: ShapeSphere, Radius: 0.5, Friction: 0.3, Restitution: 0.0},
	)
	cmd.app.FlushCommands()

	stepSynchronousPhysics(cmd, world, proxy, sim, timeRes, 180)
	position, ok := testEntityPosition(cmd, sphereID)
	if !ok {
		t.Fatal("expected sphere transform after stepping")
	}
	if position.Y() < 0.25 {
		t.Fatalf("expected sphere to be held above the box floor, got %v", position)
	}
	if len(proxy.DrainCollisionEvents()) == 0 {
		t.Fatal("expected sphere-box collision events")
	}
}

func TestSphereTriggerReportsEnterAndExitWithoutResolution(t *testing.T) {
	cmd, _, world, proxy, sim, timeRes := newPhysicsSceneHarness()
	world.UpdateFrequency = 120
	timeRes.Dt = 1.0 / float64(world.UpdateFrequency)

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{IsStatic: true, Mass: 0},
		&ColliderComponent{Shape: ShapeSphere, Radius: 2, IsTrigger: true, CollisionLayer: DefaultCollisionLayer, CollisionMask: AllCollisionLayers},
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
		t.Fatal("expected dynamic sphere transform after trigger test")
	}
	if position.Y() >= -2.0 {
		t.Fatalf("expected sphere to pass through trigger without physical resolution, got %v", position)
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
		t.Fatal("expected sphere trigger enter event")
	}
	if !sawExit {
		t.Fatal("expected sphere trigger exit event")
	}
}

func TestSphereLayerMaskFilteringSkipsPairs(t *testing.T) {
	cmd, _, world, proxy, sim, timeRes := newPhysicsSceneHarness()
	world.UpdateFrequency = 120
	timeRes.Dt = 1.0 / float64(world.UpdateFrequency)

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{IsStatic: true, Mass: 0},
		&ColliderComponent{Shape: ShapeSphere, Radius: 2, IsTrigger: true, CollisionLayer: 1 << 0, CollisionMask: 1 << 0},
	)

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Mass: 1},
		&ColliderComponent{Shape: ShapeSphere, Radius: 0.5, CollisionLayer: 1 << 1, CollisionMask: 1 << 1},
	)
	cmd.app.FlushCommands()

	stepSynchronousPhysics(cmd, world, proxy, sim, timeRes, 2)
	if events := proxy.DrainCollisionEvents(); len(events) != 0 {
		t.Fatalf("expected masked-out sphere pair to emit no events, got %d", len(events))
	}
}
