package gekko

import (
	"sync/atomic"

	"github.com/go-gl/mathgl/mgl32"
)

type PhysicsModule struct{}

func (m PhysicsModule) Install(app *App, cmd *Commands) {
	world := NewPhysicsWorld()
	cmd.AddResources(world)

	proxy := &PhysicsProxy{}
	cmd.AddResources(proxy)

	// Start the async physics loop
	go physicsLoop(world, proxy)

	app.UseSystem(
		System(PhysicsSyncSystem).
			InStage(Update).
			RunAlways(),
	)
}

type PhysicsProxy struct {
	latestResults atomic.Pointer[PhysicsResults]
	pendingState  atomic.Pointer[PhysicsSnapshot]
}

type PhysicsSnapshot struct {
	Entities []PhysicsEntityState
	Gravity  mgl32.Vec3
	Dt       float32
}

type PhysicsEntityState struct {
	Eid          EntityId
	Pos          mgl32.Vec3
	Rot          mgl32.Quat
	Vel          mgl32.Vec3
	AngVel       mgl32.Vec3
	IsStatic     bool
	Mass         float32
	Model        PhysicsModel
	Friction     float32
	Restitution  float32
	IdleTime     float32
	GravityScale float32
}

type PhysicsResults struct {
	Entities []PhysicsEntityResult
}

type PhysicsEntityResult struct {
	Eid      EntityId
	Pos      mgl32.Vec3
	Rot      mgl32.Quat
	Vel      mgl32.Vec3
	AngVel   mgl32.Vec3
	Sleeping bool
	IdleTime float32
}

func PhysicsSyncSystem(cmd *Commands, time *Time, physics *PhysicsWorld, proxy *PhysicsProxy) {
	// 1. Pull latest results
	results := proxy.latestResults.Swap(nil)
	if results != nil {
		resMap := make(map[EntityId]PhysicsEntityResult)
		for _, res := range results.Entities {
			resMap[res.Eid] = res
		}

		MakeQuery3[TransformComponent, RigidBodyComponent, PhysicsModel](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, pm *PhysicsModel) bool {
			if res, ok := resMap[eid]; ok {
				// tr.Position = res.Pos - res.Rot * pm.CenterOffset
				rotatedOffset := res.Rot.Rotate(pm.CenterOffset)
				tr.Position = res.Pos.Sub(rotatedOffset)
				tr.Rotation = res.Rot
				rb.Velocity = res.Vel
				rb.AngularVelocity = res.AngVel
				rb.Sleeping = res.Sleeping
				rb.IdleTime = res.IdleTime
			}
			return true
		})
	}

	// 2. Push current state
	snap := &PhysicsSnapshot{
		Gravity: physics.Gravity,
		Dt:      float32(time.Dt),
	}

	MakeQuery4[TransformComponent, RigidBodyComponent, PhysicsModel, ColliderComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, pm *PhysicsModel, col *ColliderComponent) bool {
		// physicsCenter = tr.Position + tr.Rotation * pm.CenterOffset
		rotatedOffset := tr.Rotation.Rotate(pm.CenterOffset)
		snap.Entities = append(snap.Entities, PhysicsEntityState{
			Eid:          eid,
			Pos:          tr.Position.Add(rotatedOffset),
			Rot:          tr.Rotation,
			Vel:          rb.Velocity,
			AngVel:       rb.AngularVelocity,
			IsStatic:     rb.IsStatic,
			Mass:         rb.Mass,
			Model:        *pm,
			Friction:     col.Friction,
			Restitution:  col.Restitution,
			IdleTime:     rb.IdleTime,
			GravityScale: rb.GravityScale,
		})
		return true
	})

	proxy.pendingState.Store(snap)
}
