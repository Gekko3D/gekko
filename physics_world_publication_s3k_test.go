package gekko

import (
	"math"
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

// These tests exercise the committed World destination. PhysicsResults is input
// to the pull bridge; simulator internals and publication sequence increments
// are deliberately not part of the contract.
type s3kStamp struct {
	revisions  [4]uint64
	structural uint64
	worlds     map[EntityId][10]uint32
}

func s3kWorldBits(cmd *Commands) map[EntityId][10]uint32 {
	worlds := make(map[EntityId][10]uint32)
	MakeQuery1[TransformComponent](cmd).Map(func(id EntityId, world *TransformComponent) bool {
		worlds[id] = s3gBits(world.Position, world.Rotation, world.Scale)
		return true
	})
	return worlds
}

func s3kBefore(cmd *Commands) s3kStamp {
	return s3kStamp{s3gRevisions(cmd), cmd.StructuralRevision(), s3kWorldBits(cmd)}
}

func s3kPublished(t *testing.T, cmd *Commands, before s3kStamp, changed bool) {
	t.Helper()
	want := [4]int{}
	if changed {
		want[0] = 1
	}
	s3gPublications(t, cmd, before.revisions, before.structural, want)
	after := s3kWorldBits(cmd)
	if len(after) != len(before.worlds) {
		t.Errorf("World membership changed before explicit flush: before=%d after=%d", len(before.worlds), len(after))
	}
	changedWorlds := 0
	for id, bits := range before.worlds {
		current, ok := after[id]
		if !ok {
			t.Errorf("committed World %d disappeared before explicit flush", id)
			continue
		}
		if current != bits {
			changedWorlds++
		}
	}
	for id := range after {
		if _, ok := before.worlds[id]; !ok {
			t.Errorf("World %d appeared before explicit flush", id)
		}
	}
	wantChangedWorlds := 0
	if changed {
		wantChangedWorlds = 1
	}
	if changedWorlds != wantChangedWorlds {
		t.Errorf("changed World destinations=%d, want=%d", changedWorlds, wantChangedWorlds)
	}
}

func s3kBody(cmd *Commands, world TransformComponent, rb RigidBodyComponent, extras ...any) EntityId {
	components := []any{world, rb, ColliderComponent{Shape: ShapeSphere, Radius: 0.5}}
	return cmd.AddEntity(append(components, extras...)...)
}

func s3kResult(id EntityId, position mgl32.Vec3, rotation mgl32.Quat) PhysicsEntityResult {
	return PhysicsEntityResult{Eid: id, Pos: position, Rot: rotation, Vel: mgl32.Vec3{2, 3, 4}, AngVel: mgl32.Vec3{0, 1, 0}, Sleeping: true, IdleTime: 0.75}
}

func s3kPull(cmd *Commands, proxy *PhysicsProxy, world *PhysicsWorld, clock *Time, tick uint64, results ...PhysicsEntityResult) {
	proxy.latestResults.Store(&PhysicsResults{Tick: tick, Entities: results})
	PhysicsPullSystem(cmd, clock, proxy, world)
}

func s3kPreserved(t *testing.T, cmd *Commands, id EntityId, old TransformComponent) {
	t.Helper()
	got := s3cComponent[TransformComponent](t, cmd, id)
	if s3gBits(mgl32.Vec3{}, mgl32.QuatIdent(), got.Scale) != s3gBits(mgl32.Vec3{}, mgl32.QuatIdent(), old.Scale) || s3gBits(got.Pivot, mgl32.QuatIdent(), mgl32.Vec3{}) != s3gBits(old.Pivot, mgl32.QuatIdent(), mgl32.Vec3{}) {
		t.Fatal("physics changed Scale or Pivot bits")
	}
}

func TestS3kPullFreshResultsPublishOnlyChangedWorld(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target int
		rotate bool
	}{{"position first", 0, false}, {"rotation middle", 1, true}, {"position last", 2, false}} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _, world, proxy, clock := newPhysicsBootstrapHarness()
			clock.Alpha = 1
			parent := cmd.AddEntity(s3dWorld(100))
			ids := make([]EntityId, 3)
			originals := make([]TransformComponent, 3)
			results := make([]PhysicsEntityResult, 3)
			local := s3dLocal(77)
			for i := range ids {
				originals[i] = s3dWorld(float32(i) * 20)
				originals[i].Scale = mgl32.Vec3{2, 3, 4}
				ids[i] = s3kBody(cmd, originals[i], RigidBodyComponent{Mass: 2, AccumulatedImpulse: mgl32.Vec3{1, 2, 3}, AccumulatedTorque: mgl32.Vec3{4, 5, 6}, ForceTeleport: true}, local, Parent{Entity: parent}, s3gUnrelated{9})
				results[i] = s3kResult(ids[i], originals[i].Position, originals[i].Rotation)
			}
			id := ids[tc.target]
			if tc.rotate {
				results[tc.target].Rot = mgl32.QuatRotate(0.8, mgl32.Vec3{0, 1, 0})
			} else {
				results[tc.target].Pos[1] = 7
			}
			cmd.app.FlushCommands()
			before := s3kBefore(cmd)
			s3kPull(cmd, proxy, world, clock, 7, results...)
			for i, candidate := range ids {
				s3dTRS(t, cmd, candidate, results[i].Pos, originals[i].Scale, results[i].Rot)
				s3kPreserved(t, cmd, candidate, originals[i])
				if *s3cComponent[LocalTransformComponent](t, cmd, candidate) != local || s3cComponent[Parent](t, cmd, candidate).Entity != parent || s3cComponent[s3gUnrelated](t, cmd, candidate).Value != 9 {
					t.Fatal("physics changed non-World ownership")
				}
			}
			rb := s3cComponent[RigidBodyComponent](t, cmd, id)
			res := results[tc.target]
			if rb.LastPhysicsTick != 7 || rb.PreviousPhysicsPos != res.Pos || rb.CurrentPhysicsPos != res.Pos || rb.PreviousPhysicsRot != res.Rot || rb.CurrentPhysicsRot != res.Rot || rb.LastPulledPos != res.Pos || rb.LastPulledRot != s3cComponent[TransformComponent](t, cmd, id).Rotation || rb.Velocity != res.Vel || rb.AngularVelocity != res.AngVel || rb.Sleeping != res.Sleeping || rb.IdleTime != res.IdleTime || rb.AccumulatedImpulse != (mgl32.Vec3{}) || rb.AccumulatedTorque != (mgl32.Vec3{}) || !rb.ForceTeleport {
				t.Fatal("fresh pull lost rigid-body result/interpolation state")
			}
			s3kPublished(t, cmd, before, true)
			before = s3kBefore(cmd)
			PhysicsPullSystem(cmd, clock, proxy, world)
			s3kPublished(t, cmd, before, false)
		})
	}
}

func TestS3kPullSameTickAlphaAndNewTickInterpolation(t *testing.T) {
	cmd, _, world, proxy, clock := newPhysicsBootstrapHarness()
	id := s3kBody(cmd, s3dWorld(0), RigidBodyComponent{Mass: 1})
	cmd.app.FlushCommands()
	clock.Alpha = 1
	s3kPull(cmd, proxy, world, clock, 1, s3kResult(id, mgl32.Vec3{}, mgl32.QuatIdent()))
	clock.Alpha = 0.25
	endRot := mgl32.QuatRotate(1, mgl32.Vec3{0, 1, 0})
	before := s3kBefore(cmd)
	s3kPull(cmd, proxy, world, clock, 2, s3kResult(id, mgl32.Vec3{8, 0, 0}, endRot))
	s3dTRS(t, cmd, id, mgl32.Vec3{2, 0, 0}, mgl32.Vec3{1, 1, 1}, mgl32.QuatNlerp(mgl32.QuatIdent(), endRot, 0.25))
	s3kPublished(t, cmd, before, true)
	before = s3kBefore(cmd)
	clock.Alpha = 0.75
	PhysicsPullSystem(cmd, clock, proxy, world)
	s3dTRS(t, cmd, id, mgl32.Vec3{6, 0, 0}, mgl32.Vec3{1, 1, 1}, mgl32.QuatNlerp(mgl32.QuatIdent(), endRot, 0.75))
	s3kPublished(t, cmd, before, true)
	before = s3kBefore(cmd)
	settled := *s3cComponent[TransformComponent](t, cmd, id)
	PhysicsPullSystem(cmd, clock, proxy, world)
	if *s3cComponent[TransformComponent](t, cmd, id) != settled {
		t.Fatal("same tick and alpha changed destination")
	}
	s3kPublished(t, cmd, before, false)
	before = s3kBefore(cmd)
	clock.Alpha = 0.5
	s3kPull(cmd, proxy, world, clock, 3, s3kResult(id, mgl32.Vec3{16, 0, 0}, endRot))
	s3dTRS(t, cmd, id, mgl32.Vec3{12, 0, 0}, mgl32.Vec3{1, 1, 1}, endRot)
	rb := s3cComponent[RigidBodyComponent](t, cmd, id)
	if rb.LastPhysicsTick != 3 || rb.PreviousPhysicsPos != (mgl32.Vec3{8, 0, 0}) || rb.CurrentPhysicsPos != (mgl32.Vec3{16, 0, 0}) || rb.PreviousPhysicsRot != endRot || rb.CurrentPhysicsRot != endRot {
		t.Fatal("new tick did not advance interpolation endpoints")
	}
	if HasComponent[LocalTransformComponent](cmd, id) {
		t.Fatal("physics created missing Local")
	}
	s3kPublished(t, cmd, before, true)
}

func TestS3kSynchronousPublishesActualSimulatorMotion(t *testing.T) {
	for _, kind := range []string{"translation", "rotation", "sleeping", "static", "kinematic", "no motion"} {
		t.Run(kind, func(t *testing.T) {
			cmd, _, world, proxy, sim, clock := newPhysicsSceneHarness()
			world.Gravity, world.Threads = mgl32.Vec3{}, 1
			rb := RigidBodyComponent{Mass: 1}
			switch kind {
			case "translation":
				rb.Velocity = mgl32.Vec3{6, 0, 0}
			case "rotation":
				rb.AngularVelocity = mgl32.Vec3{0, 3, 0}
			case "sleeping":
				rb.Sleeping = true
			case "static":
				rb.BodyMode = BodyModeStatic
				rb.Velocity = mgl32.Vec3{6, 0, 0}
			case "kinematic":
				rb.BodyMode = BodyModeKinematic
				rb.Velocity = mgl32.Vec3{6, 0, 0}
			}
			original := s3dWorld(10)
			id := s3kBody(cmd, original, rb, s3dLocal(77), Parent{Entity: 9999}, s3gUnrelated{5})
			cmd.app.FlushCommands()
			before := s3kBefore(cmd)
			SynchronousPhysicsSystem(cmd, clock, world, proxy, sim)
			got := *s3cComponent[TransformComponent](t, cmd, id)
			s3dTRS(t, cmd, id, got.Position, original.Scale, got.Rotation)
			changed := kind == "translation" || kind == "rotation"
			if kind == "translation" && (got.Position[0] < 10.09 || got.Position[0] > 10.11 || got.Position[1] != 0 || got.Position[2] != 0) {
				t.Fatalf("real simulator did not translate: %+v", got)
			}
			if kind == "rotation" && (got.Position != original.Position || got.Rotation.V[1] < 0.02 || got.Rotation.V[1] > 0.03) {
				t.Fatalf("real simulator did not rotate: %+v", got)
			}
			if !changed && s3gBits(got.Position, got.Rotation, got.Scale) != s3gBits(original.Position, original.Rotation, original.Scale) {
				t.Fatal("quiet body moved")
			}
			current := s3cComponent[RigidBodyComponent](t, cmd, id)
			if current.LastPhysicsTick == 0 || current.CurrentPhysicsPos != got.Position || current.CurrentPhysicsRot != got.Rotation || current.PreviousPhysicsPos != current.CurrentPhysicsPos || current.PreviousPhysicsRot != current.CurrentPhysicsRot || current.LastPulledPos != got.Position || current.LastPulledRot != got.Rotation {
				t.Fatal("synchronous result state was not applied")
			}
			if *s3cComponent[LocalTransformComponent](t, cmd, id) != s3dLocal(77) || s3cComponent[Parent](t, cmd, id).Entity != 9999 || s3cComponent[s3gUnrelated](t, cmd, id).Value != 5 {
				t.Fatal("synchronous physics changed other ownership")
			}
			s3kPreserved(t, cmd, id, original)
			s3kPublished(t, cmd, before, changed)
			if !changed {
				previousTick := current.LastPhysicsTick
				before = s3kBefore(cmd)
				SynchronousPhysicsSystem(cmd, clock, world, proxy, sim)
				if current.LastPhysicsTick <= previousTick {
					t.Fatal("quiet body did not receive next physics tick")
				}
				s3kPublished(t, cmd, before, false)
			}
		})
	}
}

func TestS3kConvertedWorldOffsetIsPublicationDestination(t *testing.T) {
	cmd, assets, world, proxy, clock := newPhysicsBootstrapHarness()
	clock.Alpha = 1
	original := s3dWorld(10)
	original.Scale, original.Pivot = mgl32.Vec3{2, 3, 4}, mgl32.Vec3{1, 0, 0}
	// Custom resolution makes scaled pivot (1,0,0); center offset is (3,0,0).
	id := s3kBody(cmd, original, RigidBodyComponent{Mass: 1}, PhysicsModel{CenterOffset: mgl32.Vec3{3, 0, 0}}, VoxelModelComponent{PivotMode: PivotModeCustom, CustomPivot: original.Pivot, VoxelResolution: 0.5})
	cmd.app.FlushCommands()
	before := s3kBefore(cmd)
	s3kPull(cmd, proxy, world, clock, 1, s3kResult(id, mgl32.Vec3{12, 0, 0}, mgl32.QuatIdent()))
	s3dTRS(t, cmd, id, original.Position, original.Scale, original.Rotation)
	s3kPublished(t, cmd, before, false)
	before = s3kBefore(cmd)
	halfTurn := mgl32.Quat{W: 0, V: mgl32.Vec3{0, 0, 1}}
	s3kPull(cmd, proxy, world, clock, 2, s3kResult(id, mgl32.Vec3{12, 0, 0}, halfTurn))
	s3dTRS(t, cmd, id, mgl32.Vec3{14, 0, 0}, original.Scale, halfTurn)
	s3kPreserved(t, cmd, id, original)
	s3kPublished(t, cmd, before, true)
	before = s3kBefore(cmd)
	PhysicsPullSystem(cmd, clock, proxy, world)
	s3kPublished(t, cmd, before, false)

	// A synchronous static body has a distinct physics center but an equal
	// render destination. Rotating a dynamic body moves only the render origin.
	for _, moving := range []bool{false, true} {
		cmd, _, world, proxy, sim, clock := newPhysicsSceneHarness()
		world.Gravity, world.Threads = mgl32.Vec3{}, 1
		original := s3dWorld(10)
		original.Pivot = mgl32.Vec3{2, 0, 0}
		rb := RigidBodyComponent{Mass: 1, BodyMode: BodyModeStatic}
		if moving {
			rb.BodyMode, rb.AngularVelocity = BodyModeDynamic, mgl32.Vec3{0, 3, 0}
		}
		id := s3kBody(cmd, original, rb, PhysicsModel{CenterOffset: mgl32.Vec3{3, 0, 0}}, VoxelModelComponent{PivotMode: PivotModeCustom, CustomPivot: original.Pivot, VoxelResolution: 0.5})
		cmd.app.FlushCommands()
		before := s3kBefore(cmd)
		SynchronousPhysicsSystem(cmd, clock, world, proxy, sim)
		got := *s3cComponent[TransformComponent](t, cmd, id)
		state := s3cComponent[RigidBodyComponent](t, cmd, id)
		if state.CurrentPhysicsPos != (mgl32.Vec3{12, 0, 0}) {
			t.Fatal("rotation-only simulation moved physics center")
		}
		if moving {
			if got.Position[2] < 0.09 || got.Position[2] > 0.11 {
				t.Fatalf("rotating offset did not move render origin: %+v", got)
			}
		} else {
			s3dPosition(t, cmd, id, 10)
		}
		s3dTRS(t, cmd, id, got.Position, original.Scale, got.Rotation)
		s3kPreserved(t, cmd, id, original)
		s3kPublished(t, cmd, before, moving)
	}

	// Geometry center fallback cancels center offset without writing Pivot.
	cmd, assets, world, proxy, clock = newPhysicsBootstrapHarness()
	clock.Alpha = 1
	model := assets.CreateCubeModel(6, 6, 6, 1)
	original = s3dWorld(10)
	id = s3kBody(cmd, original, RigidBodyComponent{Mass: 1}, VoxelModelComponent{VoxelModel: model, VoxelResolution: 0.5}, PhysicsModel{CenterOffset: mgl32.Vec3{1.5, 1.5, 1.5}})
	cmd.app.FlushCommands()
	before = s3kBefore(cmd)
	s3kPull(cmd, proxy, world, clock, 1, s3kResult(id, mgl32.Vec3{11, 0, 0}, mgl32.QuatIdent()))
	s3dPosition(t, cmd, id, 11)
	s3kPreserved(t, cmd, id, original)
	s3kPublished(t, cmd, before, true)
}

func TestS3kSkippedAndOptionalModelPaths(t *testing.T) {
	for _, kind := range []string{"no results", "missing result", "presentation", "missing World", "missing rigid body", "missing collider", "unresolved box", "unresolved voxel", "sphere without model", "capsule without model", "empty model"} {
		t.Run(kind, func(t *testing.T) {
			cmd, _, world, proxy, clock := newPhysicsBootstrapHarness()
			clock.Alpha = 1
			original := s3dWorld(10)
			rb := RigidBodyComponent{Mass: 1}
			col := ColliderComponent{Shape: ShapeSphere, Radius: 0.5}
			if kind == "presentation" {
				rb.BodyMode = BodyModePresentationOnly
			}
			if kind == "unresolved box" || kind == "unresolved voxel" || kind == "empty model" {
				col.Shape = ShapeBox
			}
			if kind == "capsule without model" {
				col.Shape, col.CapsuleHalfHeight = ShapeCapsule, 1
			}
			components := []any{s3gUnrelated{7}}
			if kind != "missing World" {
				components = append(components, original)
			}
			if kind != "missing rigid body" {
				components = append(components, rb)
			}
			if kind != "missing collider" {
				components = append(components, col)
			}
			if kind == "unresolved voxel" {
				components = append(components, VoxelModelComponent{})
			}
			if kind == "empty model" {
				components = append(components, PhysicsModel{})
			}
			id := cmd.AddEntity(components...)
			cmd.app.FlushCommands()
			pending := cmd.AddEntity(s3dWorld(50))
			before := s3kBefore(cmd)
			if kind == "no results" {
				PhysicsPullSystem(cmd, clock, proxy, world)
			} else if kind == "missing result" {
				s3kPull(cmd, proxy, world, clock, 1)
			} else {
				s3kPull(cmd, proxy, world, clock, 1, s3kResult(id, mgl32.Vec3{12, 0, 0}, mgl32.QuatIdent()))
			}
			accepted := kind == "sphere without model" || kind == "capsule without model" || kind == "empty model"
			if kind != "missing World" {
				want := float32(10)
				if accepted {
					want = 12
				}
				s3dPosition(t, cmd, id, want)
			}
			if kind != "missing rigid body" && !accepted && *s3cComponent[RigidBodyComponent](t, cmd, id) != rb {
				t.Fatal("skipped pull changed rigid body")
			}
			if HasComponent[LocalTransformComponent](cmd, id) || cmd.EntityExists(pending) {
				t.Fatal("pull created Local or flushed pending entity")
			}
			s3kPublished(t, cmd, before, accepted)
			cmd.app.FlushCommands()
			if !cmd.EntityExists(pending) {
				t.Fatal("skipped pull discarded pending work")
			}
		})
	}
	for _, kind := range []string{"presentation", "unresolved box"} {
		t.Run("synchronous "+kind, func(t *testing.T) {
			cmd, _, world, proxy, sim, clock := newPhysicsSceneHarness()
			world.Gravity, world.Threads = mgl32.Vec3{}, 1
			rb := RigidBodyComponent{Mass: 1, Velocity: mgl32.Vec3{6, 0, 0}}
			if kind == "presentation" {
				rb.BodyMode = BodyModePresentationOnly
			}
			id := cmd.AddEntity(s3dWorld(10), rb, ColliderComponent{Shape: ShapeBox})
			cmd.app.FlushCommands()
			before := s3kBefore(cmd)
			SynchronousPhysicsSystem(cmd, clock, world, proxy, sim)
			s3dPosition(t, cmd, id, 10)
			if *s3cComponent[RigidBodyComponent](t, cmd, id) != rb {
				t.Fatal("rejected synchronous body changed")
			}
			s3kPublished(t, cmd, before, false)
		})
	}
}

func TestS3kExactDestinationBits(t *testing.T) {
	for _, path := range []string{"pull", "synchronous"} {
		t.Run(path+" signed zero", func(t *testing.T) {
			cmd, _, world, proxy, sim, clock := newPhysicsSceneHarness()
			world.Gravity, world.Threads = mgl32.Vec3{}, 1
			original := s3dWorld(0)
			original.Position[0] = math.Float32frombits(0x80000000)
			id := s3kBody(cmd, original, RigidBodyComponent{Mass: 1, BodyMode: BodyModeStatic})
			cmd.app.FlushCommands()
			before := s3kBefore(cmd)
			run := func() {
				if path == "pull" {
					s3kPull(cmd, proxy, world, clock, 1, s3kResult(id, mgl32.Vec3{}, mgl32.QuatIdent()))
				} else {
					SynchronousPhysicsSystem(cmd, clock, world, proxy, sim)
				}
			}
			run()
			got := s3cComponent[TransformComponent](t, cmd, id)
			if math.Float32bits(got.Position[0]) != 0 {
				t.Fatal("fixture did not write positive zero")
			}
			s3dPosition(t, cmd, id, 0)
			s3kPublished(t, cmd, before, true)
			before = s3kBefore(cmd)
			run()
			s3kPublished(t, cmd, before, false)
		})
	}
	t.Run("pull stable NaN Scale payload", func(t *testing.T) {
		cmd, _, world, proxy, clock := newPhysicsBootstrapHarness()
		clock.Alpha = 1
		original := s3dWorld(0)
		original.Scale[0] = math.Float32frombits(0x7fc01234)
		id := s3kBody(cmd, original, RigidBodyComponent{Mass: 1})
		cmd.app.FlushCommands()
		// Settle the conversion's nonfinite result before checking equality. NaNs
		// never enter a simulator or spatial grid in this pull-only fixture.
		s3kPull(cmd, proxy, world, clock, 1, s3kResult(id, mgl32.Vec3{}, mgl32.QuatIdent()))
		got := *s3cComponent[TransformComponent](t, cmd, id)
		settled := s3gBits(got.Position, got.Rotation, got.Scale)
		before := s3kBefore(cmd)
		PhysicsPullSystem(cmd, clock, proxy, world)
		got = *s3cComponent[TransformComponent](t, cmd, id)
		if s3gBits(got.Position, got.Rotation, got.Scale) != settled {
			t.Fatal("stable NaN conversion changed bits")
		}
		s3kPublished(t, cmd, before, false)
		before = s3kBefore(cmd)
		rot := mgl32.Quat{W: 0, V: mgl32.Vec3{0, 1, 0}}
		s3kPull(cmd, proxy, world, clock, 2, s3kResult(id, mgl32.Vec3{}, rot))
		got = *s3cComponent[TransformComponent](t, cmd, id)
		if got.Rotation != rot || math.Float32bits(got.Scale[0]) != 0x7fc01234 || got.Rotation.Dot(got.Rotation) != 1 {
			t.Fatal("rotation write lost preserved NaN Scale or unit rotation")
		}
		s3kPreserved(t, cmd, id, original)
		s3kPublished(t, cmd, before, true)
		before = s3kBefore(cmd)
		PhysicsPullSystem(cmd, clock, proxy, world)
		s3kPublished(t, cmd, before, false)
	})
}

func TestS3kFallbackModelAndCollisionEventsKeepBoundaries(t *testing.T) {
	for _, path := range []string{"pull", "synchronous"} {
		t.Run(path+" fallback", func(t *testing.T) {
			cmd, assets, world, proxy, sim, clock := newPhysicsSceneHarness()
			world.Gravity, world.Threads = mgl32.Vec3{}, 1
			model := assets.CreateCubeModel(6, 6, 6, 1)
			id := cmd.AddEntity(s3dWorld(10), RigidBodyComponent{Mass: 1, Velocity: mgl32.Vec3{6, 0, 0}}, ColliderComponent{Shape: ShapeBox}, VoxelModelComponent{VoxelModel: model})
			cmd.app.FlushCommands()
			modelType := reflect.TypeOf(PhysicsModel{})
			modelRevision := cmd.ComponentRevision(modelType)
			before := s3kBefore(cmd)
			if path == "pull" {
				s3kPull(cmd, proxy, world, clock, 1, s3kResult(id, mgl32.Vec3{12, 0, 0}, mgl32.QuatIdent()))
			} else {
				SynchronousPhysicsSystem(cmd, clock, world, proxy, sim)
			}
			got := *s3cComponent[TransformComponent](t, cmd, id)
			s3dTRS(t, cmd, id, got.Position, mgl32.Vec3{1, 1, 1}, mgl32.QuatIdent())
			if got.Position[0] <= 10 {
				t.Fatal("fallback body did not move")
			}
			if HasComponent[PhysicsModel](cmd, id) || cmd.ComponentRevision(modelType) != modelRevision {
				t.Fatal("fallback model published before explicit flush")
			}
			s3kPublished(t, cmd, before, true)
			cmd.app.FlushCommands()
			if !HasComponent[PhysicsModel](cmd, id) || cmd.ComponentRevision(modelType) <= modelRevision || cmd.StructuralRevision() <= before.structural {
				t.Fatal("fallback model did not commit at explicit flush")
			}
		})
	}
	t.Run("quiet World drains collisions once per tick", func(t *testing.T) {
		cmd, _, world, proxy, clock := newPhysicsBootstrapHarness()
		clock.Alpha = 1
		id := s3kBody(cmd, s3dWorld(10), RigidBodyComponent{Mass: 1})
		cmd.app.FlushCommands()
		event := PhysicsCollisionEvent{Type: CollisionEventEnter, A: id, B: 9999, Tick: 1, Normal: mgl32.Vec3{0, 1, 0}, NormalImpulse: 2}
		proxy.latestResults.Store(&PhysicsResults{Tick: 1, Entities: []PhysicsEntityResult{s3kResult(id, mgl32.Vec3{10, 0, 0}, mgl32.QuatIdent())}, Collisions: []PhysicsCollisionEvent{event}})
		before := s3kBefore(cmd)
		PhysicsPullSystem(cmd, clock, proxy, world)
		if events := proxy.DrainCollisionEvents(); len(events) != 1 || events[0] != event {
			t.Fatal("pull did not capture collision event")
		}
		if len(proxy.DrainCollisionEvents()) != 0 {
			t.Fatal("collision drain repeated event")
		}
		PhysicsPullSystem(cmd, clock, proxy, world)
		if len(proxy.DrainCollisionEvents()) != 0 {
			t.Fatal("same tick recaptured collision event")
		}
		s3kPublished(t, cmd, before, false)
		event.Type, event.Tick = CollisionEventStay, 2
		proxy.latestResults.Store(&PhysicsResults{Tick: 2, Entities: []PhysicsEntityResult{s3kResult(id, mgl32.Vec3{10, 0, 0}, mgl32.QuatIdent())}, Collisions: []PhysicsCollisionEvent{event}})
		before = s3kBefore(cmd)
		PhysicsPullSystem(cmd, clock, proxy, world)
		if events := proxy.DrainCollisionEvents(); len(events) != 1 || events[0] != event {
			t.Fatal("new tick did not capture next collision event")
		}
		if len(proxy.DrainCollisionEvents()) != 0 {
			t.Fatal("next tick collision drain repeated event")
		}
		s3kPublished(t, cmd, before, false)
	})
}

func TestS3kBufferedWorldTargetsWaitForExplicitFlush(t *testing.T) {
	for _, tc := range []struct{ path, work string }{{"pull", "replace World"}, {"synchronous", "replace World"}, {"pull", "remove World"}, {"synchronous", "remove entity"}, {"pull", "add World"}, {"synchronous", "new entity"}, {"pull", "quiet replacement"}, {"synchronous", "skipped addition"}} {
		t.Run(tc.path+" "+tc.work, func(t *testing.T) {
			cmd, _, world, proxy, sim, clock := newPhysicsSceneHarness()
			world.Gravity, world.Threads = mgl32.Vec3{}, 1
			rb := RigidBodyComponent{Mass: 1, Velocity: mgl32.Vec3{6, 0, 0}}
			var id EntityId
			if tc.work == "add World" || tc.work == "skipped addition" {
				id = cmd.AddEntity(rb, ColliderComponent{Shape: ShapeSphere, Radius: 0.5})
			} else {
				id = s3kBody(cmd, s3dWorld(10), rb)
			}
			cmd.app.FlushCommands()
			var pending EntityId
			switch tc.work {
			case "replace World", "quiet replacement":
				cmd.AddComponents(id, s3dWorld(77))
			case "remove World":
				cmd.RemoveComponents(id, TransformComponent{})
			case "remove entity":
				cmd.RemoveEntity(id)
			case "add World", "skipped addition":
				cmd.AddComponents(id, s3dWorld(77))
			case "new entity":
				pending = s3kBody(cmd, s3dWorld(77), rb)
			}
			before := s3kBefore(cmd)
			if tc.path == "pull" {
				x := float32(12)
				if tc.work == "quiet replacement" {
					x = 10
				}
				s3kPull(cmd, proxy, world, clock, 1, s3kResult(id, mgl32.Vec3{x, 0, 0}, mgl32.QuatIdent()))
			} else {
				SynchronousPhysicsSystem(cmd, clock, world, proxy, sim)
			}
			changed := tc.work != "add World" && tc.work != "skipped addition" && tc.work != "quiet replacement"
			if tc.work == "add World" || tc.work == "skipped addition" {
				if HasComponent[TransformComponent](cmd, id) || *s3cComponent[RigidBodyComponent](t, cmd, id) != rb {
					t.Fatal("physics consumed pending World addition")
				}
			} else {
				got := *s3cComponent[TransformComponent](t, cmd, id)
				if changed && got.Position[0] <= 10 {
					t.Fatal("pending removal/replacement hid committed World")
				}
				if !changed {
					s3dPosition(t, cmd, id, 10)
				}
				s3dTRS(t, cmd, id, got.Position, got.Scale, got.Rotation)
			}
			if HasComponent[LocalTransformComponent](cmd, id) {
				t.Fatal("physics created missing Local")
			}
			if !cmd.EntityExists(id) || (pending != 0 && cmd.EntityExists(pending)) {
				t.Fatal("physics crossed entity flush boundary")
			}
			s3kPublished(t, cmd, before, changed)
			cmd.app.FlushCommands()
			switch tc.work {
			case "remove World":
				if HasComponent[TransformComponent](cmd, id) {
					t.Fatal("queued World removal lost")
				}
			case "remove entity":
				if cmd.EntityExists(id) {
					t.Fatal("queued entity removal lost")
				}
			case "new entity":
				if !cmd.EntityExists(pending) {
					t.Fatal("queued entity addition lost")
				}
				s3dPosition(t, cmd, pending, 77)
			default:
				s3dPosition(t, cmd, id, 77)
			}
		})
	}
}
