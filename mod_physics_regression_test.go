package gekko

import (
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl32"
)

type testSolidGrid struct {
	size  [3]int
	vSize float32
}

func (g testSolidGrid) GetVoxel(gx, gy, gz int) (bool, uint8) {
	if gx < 0 || gy < 0 || gz < 0 {
		return false, 0
	}
	if gx >= g.size[0] || gy >= g.size[1] || gz >= g.size[2] {
		return false, 0
	}
	return true, 1
}

func (g testSolidGrid) GetAABBMin() mgl32.Vec3 {
	return mgl32.Vec3{0, 0, 0}
}

func (g testSolidGrid) GetAABBMax() mgl32.Vec3 {
	return mgl32.Vec3{float32(g.size[0]), float32(g.size[1]), float32(g.size[2])}
}

func (g testSolidGrid) VoxelSize() float32 {
	return g.vSize
}

func waitForPhysicsTick(t *testing.T, proxy *PhysicsProxy, minTick uint64, timeout time.Duration) *PhysicsResults {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if res := proxy.latestResults.Load(); res != nil && res.Tick >= minTick {
			return res
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for physics tick %d", minTick)
	return nil
}

func findPhysicsResult(t *testing.T, res *PhysicsResults, eid EntityId) PhysicsEntityResult {
	t.Helper()

	for _, entity := range res.Entities {
		if entity.Eid == eid {
			return entity
		}
	}

	t.Fatalf("missing physics result for entity %d", eid)
	return PhysicsEntityResult{}
}

func runDropSimulation(t *testing.T, useVoxelGrid bool, angularVelocity mgl32.Vec3, updateFrequency float32, minTick uint64) PhysicsEntityResult {
	t.Helper()

	world := NewPhysicsWorld()
	world.UpdateFrequency = updateFrequency
	world.Threads = 1
	world.SolverIterations = 12

	proxy := &PhysicsProxy{}
	go physicsLoop(world, proxy)

	const floorID EntityId = 1
	const bodyID EntityId = 2
	const vSize = VoxelSize

	floorSize := [3]int{100, 2, 100}
	bodySize := [3]int{10, 10, 10}

	floorCenter := mgl32.Vec3{
		float32(floorSize[0]) * vSize * 0.5,
		float32(floorSize[1]) * vSize * 0.5,
		float32(floorSize[2]) * vSize * 0.5,
	}
	bodyCenterOffset := mgl32.Vec3{
		float32(bodySize[0]) * vSize * 0.5,
		float32(bodySize[1]) * vSize * 0.5,
		float32(bodySize[2]) * vSize * 0.5,
	}
	floorModel := PhysicsModel{
		CenterOffset: floorCenter,
		Boxes: []CollisionBox{{
			HalfExtents: floorCenter,
		}},
	}
	bodyModel := PhysicsModel{
		CenterOffset: bodyCenterOffset,
		Boxes: []CollisionBox{{
			HalfExtents: bodyCenterOffset,
		}},
	}
	if useVoxelGrid {
		floorModel.Grid = testSolidGrid{size: floorSize, vSize: vSize}
		bodyModel.Grid = testSolidGrid{size: bodySize, vSize: vSize}
	}

	proxy.pendingState.Store(&PhysicsSnapshot{
		Entities: []PhysicsEntityState{
			{
				Eid:         floorID,
				Pos:         floorCenter,
				Rot:         mgl32.QuatIdent(),
				IsStatic:    true,
				Model:       floorModel,
				Teleport:    true,
				Friction:    0.5,
				Restitution: 0.2,
			},
			{
				Eid:          bodyID,
				Pos:          mgl32.Vec3{floorCenter.X(), 2.5, floorCenter.Z()},
				Rot:          mgl32.QuatIdent(),
				AngVel:       angularVelocity,
				Mass:         1,
				Model:        bodyModel,
				Teleport:     true,
				Friction:     0.3,
				Restitution:  0.0,
				GravityScale: 1,
			},
		},
	})

	timeout := time.Duration(float32(minTick)/updateFrequency*float32(time.Second)) + time.Second
	res := waitForPhysicsTick(t, proxy, minTick, timeout)
	return findPhysicsResult(t, res, bodyID)
}

func TestVoxelBodyDoesNotPickUpSidewaysVelocityFromFlatFloor(t *testing.T) {
	body := runDropSimulation(t, true, mgl32.Vec3{}, 240, 480)
	horizontalSpeed := mgl32.Vec2{body.Vel.X(), body.Vel.Z()}.Len()
	if horizontalSpeed > 0.05 {
		t.Fatalf("expected body to settle without sideways drift, got horizontal speed %.4f and velocity %v", horizontalSpeed, body.Vel)
	}
}

func TestVoxelCubeEventuallySleepsOnFlatFloor(t *testing.T) {
	body := runDropSimulation(t, true, mgl32.Vec3{1, 2, 0.5}, 60, 360)
	speed := body.Vel.Len()
	angularSpeed := body.AngVel.Len()
	t.Logf("settled voxel body pos=%v vel=%v angVel=%v sleeping=%v", body.Pos, body.Vel, body.AngVel, body.Sleeping)
	if !body.Sleeping && (speed > 0.2 || angularSpeed > 0.2) {
		t.Fatalf("expected voxel body to settle or nearly stop on flat floor, got speed=%.3f angularSpeed=%.3f", speed, angularSpeed)
	}
}

func TestVoxelCollisionDetectsShallowFloorOverlap(t *testing.T) {
	const vSize = VoxelSize

	floor := &internalBody{
		Eid:      1,
		pos:      mgl32.Vec3{5, 0.1, 5},
		rot:      mgl32.QuatIdent(),
		isStatic: true,
		boxes: []InternalBox{{
			Box: CollisionBox{
				HalfExtents: mgl32.Vec3{5, 0.1, 5},
			},
		}},
		model: PhysicsModel{
			CenterOffset: mgl32.Vec3{5, 0.1, 5},
			Boxes: []CollisionBox{{
				HalfExtents: mgl32.Vec3{5, 0.1, 5},
			}},
			Grid: testSolidGrid{size: [3]int{100, 2, 100}, vSize: vSize},
		},
	}
	body := &internalBody{
		Eid: 2,
		pos: mgl32.Vec3{5, 0.66, 5},
		rot: mgl32.QuatIdent(),
		boxes: []InternalBox{{
			Box: CollisionBox{
				HalfExtents: mgl32.Vec3{0.5, 0.5, 0.5},
			},
		}},
		model: PhysicsModel{
			CenterOffset: mgl32.Vec3{0.5, 0.5, 0.5},
			Boxes: []CollisionBox{{
				HalfExtents: mgl32.Vec3{0.5, 0.5, 0.5},
			}},
			Grid: testSolidGrid{size: [3]int{10, 10, 10}, vSize: vSize},
		},
	}

	floor.updateAABB()
	body.updateAABB()

	contacts, handled := checkVoxelCollision(body, floor, 0.01)
	if !handled {
		t.Fatal("expected voxel path to handle shallow overlap")
	}
	if len(contacts) == 0 {
		t.Fatal("expected shallow overlap to be detected, got zero contacts")
	}
}

func TestVoxelCollisionReturnsRepresentativeContactPatch(t *testing.T) {
	const vSize = VoxelSize

	floor := &internalBody{
		Eid:      1,
		pos:      mgl32.Vec3{5, 0.1, 5},
		rot:      mgl32.QuatIdent(),
		isStatic: true,
		boxes: []InternalBox{{
			Box: CollisionBox{
				HalfExtents: mgl32.Vec3{5, 0.1, 5},
			},
		}},
		model: PhysicsModel{
			CenterOffset: mgl32.Vec3{5, 0.1, 5},
			Boxes: []CollisionBox{{
				HalfExtents: mgl32.Vec3{5, 0.1, 5},
			}},
			Grid: testSolidGrid{size: [3]int{100, 2, 100}, vSize: vSize},
		},
	}
	body := &internalBody{
		Eid: 2,
		pos: mgl32.Vec3{5, 0.65, 5},
		rot: mgl32.QuatIdent(),
		boxes: []InternalBox{{
			Box: CollisionBox{
				HalfExtents: mgl32.Vec3{0.5, 0.5, 0.5},
			},
		}},
		model: PhysicsModel{
			CenterOffset: mgl32.Vec3{0.5, 0.5, 0.5},
			Boxes: []CollisionBox{{
				HalfExtents: mgl32.Vec3{0.5, 0.5, 0.5},
			}},
			Grid: testSolidGrid{size: [3]int{10, 10, 10}, vSize: vSize},
		},
	}

	floor.updateAABB()
	body.updateAABB()

	contacts, handled := checkVoxelCollision(body, floor, 0.01)
	if !handled {
		t.Fatal("expected voxel path to handle face contact")
	}
	if len(contacts) < 2 {
		t.Fatalf("expected multiple representative contacts for face overlap, got %d", len(contacts))
	}
}

func TestPhysicsLoopInitialSnapshotAppliesPoseWithoutTeleport(t *testing.T) {
	world := NewPhysicsWorld()
	world.UpdateFrequency = 240
	world.Threads = 1
	proxy := &PhysicsProxy{}
	go physicsLoop(world, proxy)

	const bodyID EntityId = 7
	expectedPos := mgl32.Vec3{3, 4, 5}
	expectedRot := mgl32.QuatRotate(0.25, mgl32.Vec3{0, 1, 0})

	proxy.pendingState.Store(&PhysicsSnapshot{
		Entities: []PhysicsEntityState{{
			Eid:          bodyID,
			Pos:          expectedPos,
			Rot:          expectedRot,
			IsStatic:     true,
			GravityScale: 0,
			Model: PhysicsModel{
				CenterOffset: mgl32.Vec3{0.5, 0.5, 0.5},
				Boxes: []CollisionBox{{
					HalfExtents: mgl32.Vec3{0.5, 0.5, 0.5},
				}},
			},
		}},
	})

	res := waitForPhysicsTick(t, proxy, 1, time.Second)
	body := findPhysicsResult(t, res, bodyID)
	if body.Pos.Sub(expectedPos).Len() > 1e-4 {
		t.Fatalf("expected initial snapshot position %v, got %v", expectedPos, body.Pos)
	}
	if 1.0-absf(body.Rot.Dot(expectedRot)) > 1e-4 {
		t.Fatalf("expected initial snapshot rotation %v, got %v", expectedRot, body.Rot)
	}
}
