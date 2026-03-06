package gekko

import (
	"math"
	"sync/atomic"

	"github.com/go-gl/mathgl/mgl32"
)

type PhysicsModule struct {
	UpdateFrequency float32
	Threads         int
}

func (m PhysicsModule) Install(app *App, cmd *Commands) {
	world := NewPhysicsWorld()
	// world.VoxelScale = 0.1 // Default scale - removed as NewPhysicsWorld already sets it

	if m.UpdateFrequency > 0 {
		world.UpdateFrequency = m.UpdateFrequency
	}
	if m.Threads > 0 {
		world.Threads = m.Threads
	}
	cmd.AddResources(world)

	proxy := &PhysicsProxy{}
	cmd.AddResources(proxy)

	// Start the async physics loop
	go physicsLoop(world, proxy)

	app.UseSystem(
		System(PhysicsPullSystem).
			InStage(PreUpdate).
			RunAlways(),
	)
	app.UseSystem(
		System(PhysicsPushSystem).
			InStage(PostUpdate).
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
	Eid            EntityId
	Pos            mgl32.Vec3
	Rot            mgl32.Quat
	Vel            mgl32.Vec3
	AngVel         mgl32.Vec3
	IsStatic       bool
	Mass           float32
	Model          PhysicsModel
	Friction       float32
	Restitution    float32
	IdleTime       float32
	GravityScale   float32
	LinearDamping  float32
	AngularDamping float32
	Sleeping       bool
	Teleport       bool
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

func PhysicsPullSystem(cmd *Commands, proxy *PhysicsProxy, physics *PhysicsWorld) {
	// Pull latest results from simulation
	results := proxy.latestResults.Load()
	if results != nil {
		resMap := make(map[EntityId]PhysicsEntityResult)
		for _, res := range results.Entities {
			resMap[res.Eid] = res
		}

		MakeQuery3[TransformComponent, RigidBodyComponent, PhysicsModel](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, pm *PhysicsModel) bool {
			if res, ok := resMap[eid]; ok {
				// Update components from physics result
				// Render point = pos + rot * (scale * (localPos - pivot)).
				// Physics sets res.Pos at localPos = pm.CenterOffset_unscaled.
				// pm.CenterOffset is ALREADY scaled by tr.Scale.X() from pre-calc system!
				// Pivot is unscaled. So scaled_pivot = tr.Scale * tr.Pivot.
				vSize := VoxelSize
				scaledPivot := mgl32.Vec3{tr.Pivot.X() * tr.Scale.X() * vSize, tr.Pivot.Y() * tr.Scale.Y() * vSize, tr.Pivot.Z() * tr.Scale.Z() * vSize}
				// Center offset in PhysicsModel is scaled, but Transform's scale is applied AFTER rotation? NO!
				// Translate * Rotate * Scale * Translate(-Pivot). Rotate happens AFTER Scale.
				// So offset vector to subtract from res.Pos is Rotate( pm.CenterOffset - scaledPivot )
				diff := pm.CenterOffset.Sub(scaledPivot)
				rotatedOffset := res.Rot.Rotate(diff)

				tr.Position = res.Pos.Sub(rotatedOffset)
				tr.Rotation = res.Rot
				rb.Velocity = res.Vel
				rb.AngularVelocity = res.AngVel
				rb.Sleeping = res.Sleeping
				rb.IdleTime = res.IdleTime
				rb.LastPulledPos = tr.Position
				rb.LastPulledRot = tr.Rotation
			}
			return true
		})
	}
}

func PhysicsPushSystem(cmd *Commands, time *Time, physics *PhysicsWorld, proxy *PhysicsProxy) {
	// Push current state to simulation
	snap := &PhysicsSnapshot{
		Gravity: physics.Gravity,
		Dt:      float32(time.Dt),
	}

	MakeQuery4[TransformComponent, RigidBodyComponent, PhysicsModel, ColliderComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, pm *PhysicsModel, col *ColliderComponent) bool {
		// Calculate the physics position from visual transform
		vSize := VoxelSize
		scaledPivot := mgl32.Vec3{tr.Pivot.X() * tr.Scale.X() * vSize, tr.Pivot.Y() * tr.Scale.Y() * vSize, tr.Pivot.Z() * tr.Scale.Z() * vSize}
		diff := pm.CenterOffset.Sub(scaledPivot)
		rotatedOffset := tr.Rotation.Rotate(diff)
		physicsPos := tr.Position.Add(rotatedOffset)

		// Apply accumulated forces to the velocity we send to physics
		invMass := float32(1.0)
		if rb.Mass > 0 {
			invMass = 1.0 / rb.Mass
		}
		vel := rb.Velocity.Add(rb.AccumulatedImpulse.Mul(invMass))
		angVel := rb.AngularVelocity.Add(rb.AccumulatedTorque.Mul(invMass))

		// Clear accumulators
		rb.AccumulatedImpulse = mgl32.Vec3{0, 0, 0}
		rb.AccumulatedTorque = mgl32.Vec3{0, 0, 0}

		// Detect manual move or rotate (teleport)
		isTeleport := false
		posDiff := tr.Position.Sub(rb.LastPulledPos).Len()

		// For rotation, check dot product (1.0 means same orientation)
		rotDiff := 1.0 - math.Abs(float64(tr.Rotation.Dot(rb.LastPulledRot)))

		if posDiff > 0.001 || rotDiff > 0.001 {
			isTeleport = true
		}

		snap.Entities = append(snap.Entities, PhysicsEntityState{
			Eid:            eid,
			Pos:            physicsPos,
			Rot:            tr.Rotation,
			Vel:            vel,
			AngVel:         angVel,
			IsStatic:       rb.IsStatic,
			Mass:           rb.Mass,
			Model:          *pm,
			Friction:       col.Friction,
			Restitution:    col.Restitution,
			IdleTime:       rb.IdleTime,
			GravityScale:   rb.GravityScale,
			LinearDamping:  rb.LinearDamping,
			AngularDamping: rb.AngularDamping,
			Sleeping:       rb.Sleeping,
			Teleport:       isTeleport,
		})
		return true
	})

	proxy.pendingState.Store(snap)
}
