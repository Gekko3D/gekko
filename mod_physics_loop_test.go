package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func makeVoxelGridSnapshot(coords [][3]int, vSize float32) *voxelGridSnapshot {
	xbm := volume.NewXBrickMap()
	for _, coord := range coords {
		xbm.SetVoxel(coord[0], coord[1], coord[2], 1)
	}
	min, max := xbm.ComputeAABB()
	return &voxelGridSnapshot{
		xbm:        xbm,
		vSize:      vSize,
		voxelScale: mgl32.Vec3{vSize, vSize, vSize},
		cachedMin:  min,
		cachedMax:  max,
	}
}

func makeGridState(grid *voxelGridSnapshot) PhysicsEntityState {
	min := vec3MulComponents(grid.GetAABBMin(), grid.VoxelScale())
	max := vec3MulComponents(grid.GetAABBMax(), grid.VoxelScale())
	center := min.Add(max).Mul(0.5)
	return PhysicsEntityState{
		Eid:  42,
		Pos:  mgl32.Vec3{2, 3, 4},
		Rot:  mgl32.QuatIdent(),
		Mass: 1,
		Model: PhysicsModel{
			CenterOffset: center,
			Boxes: []CollisionBox{{
				HalfExtents: max.Sub(min).Mul(0.5),
			}},
			Grid: grid,
		},
	}
}

func mat3MaxDiff(a, b mgl32.Mat3) float32 {
	maxDiff := float32(0)
	for i := 0; i < 9; i++ {
		diff := absf(a[i] - b[i])
		if diff > maxDiff {
			maxDiff = diff
		}
	}
	return maxDiff
}

func mat3Diagonal(m mgl32.Mat3) mgl32.Vec3 {
	return mgl32.Vec3{m[0], m[4], m[8]}
}

func TestSyncInternalBodyRecalculatesInertiaWhenVoxelGridChanges(t *testing.T) {
	initialGrid := makeVoxelGridSnapshot([][3]int{
		{0, 0, 0},
		{2, 0, 0},
		{0, 1, 1},
		{2, 1, 1},
	}, VoxelSize)
	var filledCoords [][3]int
	for z := 0; z < 2; z++ {
		for y := 0; y < 2; y++ {
			for x := 0; x < 3; x++ {
				filledCoords = append(filledCoords, [3]int{x, y, z})
			}
		}
	}
	editedGrid := makeVoxelGridSnapshot(filledCoords, VoxelSize)

	initialState := makeGridState(initialGrid)
	editedState := makeGridState(editedGrid)

	body := &internalBody{Eid: initialState.Eid}
	syncInternalBody(body, initialState, true)

	if !sameCollisionBoxes(body.boxes, editedState.Model.Boxes) {
		t.Fatal("expected regression test to keep the same AABB boxes across voxel edits")
	}
	if !physicsModelChanged(body, editedState.Model) {
		t.Fatal("expected voxel-grid edit to invalidate the cached physics model")
	}

	sentinel := mgl32.Mat3{
		11, 12, 13,
		21, 22, 23,
		31, 32, 33,
	}
	body.invInertiaLocal = sentinel

	syncInternalBody(body, editedState, false)
	if diff := mat3MaxDiff(sentinel, body.invInertiaLocal); diff <= 1e-4 {
		t.Fatalf("expected syncInternalBody to recalculate inertia after voxel edit, max diff from sentinel was %.6f", diff)
	}
}

func TestCalculateLocalInertiaTensorSingleVoxelIncludesVoxelExtents(t *testing.T) {
	body := &internalBody{
		mass: 2,
		model: PhysicsModel{
			CenterOffset: mgl32.Vec3{VoxelSize * 0.5, VoxelSize * 0.5, VoxelSize * 0.5},
			Grid:         testSolidGrid{size: [3]int{1, 1, 1}, vSize: VoxelSize},
		},
	}

	inertia := calculateLocalInertiaTensor(body)
	diag := mat3Diagonal(inertia)
	want := body.mass * VoxelSize * VoxelSize / 6.0
	if absf(diag.X()-want) > 1e-5 || absf(diag.Y()-want) > 1e-5 || absf(diag.Z()-want) > 1e-5 {
		t.Fatalf("expected single voxel inertia diagonal near %.6f, got %v", want, diag)
	}
	if absf(inertia.Det()) <= 1e-8 {
		t.Fatalf("expected single voxel inertia tensor to be non-degenerate, got determinant %.8f", inertia.Det())
	}
}

func TestCalculateInverseInertiaLocalUsesBoundedPseudoInverseForThinRod(t *testing.T) {
	body := &internalBody{
		mass: 3,
		boxes: []InternalBox{{
			Box: CollisionBox{
				HalfExtents: mgl32.Vec3{2, 0, 0},
			},
		}},
	}

	invInertia := calculateInverseInertiaLocal(body)
	diag := mat3Diagonal(invInertia)
	if absf(diag.Y()-0.25) > 1e-4 || absf(diag.Z()-0.25) > 1e-4 {
		t.Fatalf("expected rod inverse inertia around 0.25 on transverse axes, got %v", diag)
	}
	if diag.X() <= diag.Y()*10 {
		t.Fatalf("expected rod inverse inertia to keep strong axis preference, got %v", diag)
	}
	if absf(diag.X()-1) < 1e-4 && absf(diag.Y()-1) < 1e-4 && absf(diag.Z()-1) < 1e-4 {
		t.Fatalf("expected thin rod inverse inertia not to fall back to identity, got %v", diag)
	}
}

func TestWakeBodyForContact_DoesNotResetAwakeRestingBody(t *testing.T) {
	body := &internalBody{
		sleeping: false,
		idleTime: 0.75,
	}

	wakeBodyForContact(body, false, true)

	if body.sleeping {
		t.Fatal("expected awake resting body to stay awake")
	}
	if body.idleTime != 0.75 {
		t.Fatalf("expected resting contact not to reset idle time, got %.2f", body.idleTime)
	}
}

func TestWakeBodyForContact_WakesSleepingBodyOnDeepPenetration(t *testing.T) {
	body := &internalBody{
		sleeping: true,
		idleTime: 0.75,
	}

	wakeBodyForContact(body, false, true)

	if body.sleeping {
		t.Fatal("expected deep penetration to wake a sleeping body")
	}
	if body.idleTime != 0 {
		t.Fatalf("expected wake to reset idle time, got %.2f", body.idleTime)
	}
}

func TestWakeBodyForContact_ResetsIdleTimeOnHighImpact(t *testing.T) {
	body := &internalBody{
		sleeping: false,
		idleTime: 0.75,
	}

	wakeBodyForContact(body, true, false)

	if body.sleeping {
		t.Fatal("expected high-impact wake on an awake body to keep it awake")
	}
	if body.idleTime != 0 {
		t.Fatalf("expected high-impact wake to reset idle time, got %.2f", body.idleTime)
	}
}

func TestSeedManifoldImpulsesRestoresNearestCachedImpulse(t *testing.T) {
	manifolds := []collisionManifold{
		{
			bodyA:  &internalBody{Eid: 1},
			bodyB:  &internalBody{Eid: 2},
			point:  mgl32.Vec3{1.00, 2.00, 3.00},
			normal: mgl32.Vec3{0, 1, 0},
		},
		{
			bodyA:  &internalBody{Eid: 1},
			bodyB:  &internalBody{Eid: 2},
			point:  mgl32.Vec3{1.22, 2.00, 3.00},
			normal: mgl32.Vec3{0, 1, 0},
		},
	}

	pair := orderedCollisionPair(1, 2)
	cache := map[collisionPair][]cachedContactImpulse{
		pair: {
			{
				point:         mgl32.Vec3{1.01, 2.00, 3.00},
				localPointA:   mgl32.Vec3{1.01, 2.00, 3.00},
				localPointB:   mgl32.Vec3{1.01, 2.00, 3.00},
				normal:        mgl32.Vec3{0, 1, 0},
				normalImpulse: 1.75,
			},
			{
				point:         mgl32.Vec3{1.20, 2.00, 3.00},
				localPointA:   mgl32.Vec3{1.20, 2.00, 3.00},
				localPointB:   mgl32.Vec3{1.20, 2.00, 3.00},
				normal:        mgl32.Vec3{0, 1, 0},
				normalImpulse: 0.75,
			},
			{
				point:         mgl32.Vec3{1.00, 2.00, 3.00},
				localPointA:   mgl32.Vec3{1.00, 2.00, 3.00},
				localPointB:   mgl32.Vec3{1.00, 2.00, 3.00},
				normal:        mgl32.Vec3{0, -1, 0},
				normalImpulse: 5.00,
			},
		},
	}

	seedManifoldImpulses(manifolds, cache, 0.08, 0.75)

	if manifolds[0].accumulatedNormalImpulse != 1.75 {
		t.Fatalf("expected first cached normal impulse 1.75, got %.2f", manifolds[0].accumulatedNormalImpulse)
	}
	if manifolds[1].accumulatedNormalImpulse != 0.75 {
		t.Fatalf("expected second cached normal impulse 0.75, got %.2f", manifolds[1].accumulatedNormalImpulse)
	}
	if manifolds[0].accumulatedTangentImpulse != (mgl32.Vec3{}) || manifolds[1].accumulatedTangentImpulse != (mgl32.Vec3{}) {
		t.Fatalf("expected cached tangent impulse to remain zero across frames, got %v and %v", manifolds[0].accumulatedTangentImpulse, manifolds[1].accumulatedTangentImpulse)
	}
}

func TestWarmStartManifoldAppliesCachedImpulse(t *testing.T) {
	bodyA := &internalBody{
		Eid:             1,
		mass:            2,
		invInertiaLocal: mgl32.Ident3(),
	}
	bodyB := &internalBody{
		Eid:      2,
		isStatic: true,
	}
	manifold := collisionManifold{
		bodyA:                    bodyA,
		bodyB:                    bodyB,
		point:                    mgl32.Vec3{0, 0, 0},
		normal:                   mgl32.Vec3{0, 1, 0},
		accumulatedNormalImpulse: 4,
	}

	warmStartManifold(&manifold)

	wantVel := mgl32.Vec3{0, 2, 0}
	if bodyA.vel != wantVel {
		t.Fatalf("expected warm-started velocity %v, got %v", wantVel, bodyA.vel)
	}
	if bodyB.vel != (mgl32.Vec3{}) {
		t.Fatalf("expected static body velocity to stay zero, got %v", bodyB.vel)
	}
}

func TestStoreCachedManifoldImpulseKeepsLargestImpulsePerContactCluster(t *testing.T) {
	cache := make(map[collisionPair][]cachedContactImpulse)

	storeCachedManifoldImpulse(cache, &collisionManifold{
		bodyA:                    &internalBody{Eid: 1},
		bodyB:                    &internalBody{Eid: 2},
		point:                    mgl32.Vec3{1.00, 2.00, 3.00},
		normal:                   mgl32.Vec3{0, 1, 0},
		accumulatedNormalImpulse: 1.5,
	}, 0.05, 0.75)
	storeCachedManifoldImpulse(cache, &collisionManifold{
		bodyA:                    &internalBody{Eid: 1},
		bodyB:                    &internalBody{Eid: 2},
		point:                    mgl32.Vec3{1.02, 2.00, 3.00},
		normal:                   mgl32.Vec3{0, 1, 0},
		accumulatedNormalImpulse: 0.5,
	}, 0.05, 0.75)
	storeCachedManifoldImpulse(cache, &collisionManifold{
		bodyA:                    &internalBody{Eid: 1},
		bodyB:                    &internalBody{Eid: 2},
		point:                    mgl32.Vec3{1.20, 2.00, 3.00},
		normal:                   mgl32.Vec3{0, 1, 0},
		accumulatedNormalImpulse: 0.75,
	}, 0.05, 0.75)

	pair := orderedCollisionPair(1, 2)
	cached := cache[pair]
	if len(cached) != 2 {
		t.Fatalf("expected two cached contact clusters, got %d", len(cached))
	}

	var foundMerged bool
	var foundSeparate bool
	for _, impulse := range cached {
		switch {
		case impulse.point.Sub(mgl32.Vec3{1.02, 2.00, 3.00}).Len() < 1e-4:
			foundMerged = true
			if impulse.normalImpulse != 1.5 {
				t.Fatalf("expected strongest merged normal impulse 1.5, got %.2f", impulse.normalImpulse)
			}
		case impulse.point.Sub(mgl32.Vec3{1.20, 2.00, 3.00}).Len() < 1e-4:
			foundSeparate = true
			if impulse.normalImpulse != 0.75 {
				t.Fatalf("expected separate normal impulse 0.75, got %.2f", impulse.normalImpulse)
			}
		}
	}

	if !foundMerged {
		t.Fatal("expected merged cached contact cluster to keep the latest nearby contact point")
	}
	if !foundSeparate {
		t.Fatal("expected distant contact to remain cached separately")
	}
}
