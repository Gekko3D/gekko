package gekko

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/go-gl/mathgl/mgl32"
)

type ColliderShape int

const (
	ShapeBox ColliderShape = iota
	ShapeSphere
)

type RigidBodyComponent struct {
	Velocity        mgl32.Vec3
	AngularVelocity mgl32.Vec3
	Mass            float32
	GravityScale    float32
	IsStatic        bool
	Sleeping        bool
	IdleTime        float32
}

func (rb *RigidBodyComponent) Wake() {
	rb.Sleeping = false
	rb.IdleTime = 0
}

func (rb *RigidBodyComponent) ApplyImpulse(impulse mgl32.Vec3) {
	rb.Wake()
	if rb.Mass > 0 {
		rb.Velocity = rb.Velocity.Add(impulse.Mul(1.0 / rb.Mass))
	} else {
		rb.Velocity = rb.Velocity.Add(impulse)
	}
}

type ColliderComponent struct {
	Shape           ColliderShape
	HalfExtents     mgl32.Vec3 // For Box
	Radius          float32    // For Sphere
	Friction        float32
	Restitution     float32
	AABBHalfExtents mgl32.Vec3 // Cached or calculated total half extents
}

// PhysicsModel is a generic component that describes the object's physics model.
// It is agnostic of the renderer.
type PhysicsModel struct {
	AABBMin      mgl32.Vec3
	AABBMax      mgl32.Vec3
	CenterOffset mgl32.Vec3 // Offset from transform position to center of collision box
	// KeyPoints will contain corner and edge key-points in future phases.
	KeyPoints []mgl32.Vec3
}

type PhysicsWorld struct {
	Gravity         mgl32.Vec3
	VoxelSize       float32
	SleepThreshold  float32
	SleepTime       float32
	UpdateFrequency float32 // Hz
}

func NewPhysicsWorld() *PhysicsWorld {
	return &PhysicsWorld{
		Gravity:         mgl32.Vec3{0, -9.81, 0},
		VoxelSize:       0.1,
		SleepThreshold:  0.05,
		SleepTime:       1.0,
		UpdateFrequency: 60.0,
	}
}

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

func physicsLoop(world *PhysicsWorld, proxy *PhysicsProxy) {
	ticker := time.NewTicker(time.Duration(1000.0/world.UpdateFrequency) * time.Millisecond)
	defer ticker.Stop()

	// Internal state
	internalBodies := make(map[EntityId]*internalBody)

	for range ticker.C {
		// Pick up new snapshot
		snap := proxy.pendingState.Swap(nil)
		if snap != nil {
			// Update internal state from snapshot
			// Current simple policy: snapshot is the truth for external changes
			for _, es := range snap.Entities {
				body, ok := internalBodies[es.Eid]
				if !ok {
					body = &internalBody{Eid: es.Eid}
					internalBodies[es.Eid] = body
				}
				body.pos = es.Pos
				body.rot = es.Rot
				body.vel = es.Vel
				body.angVel = es.AngVel
				body.isStatic = es.IsStatic
				body.mass = es.Mass
				body.model = es.Model
				body.friction = es.Friction
				body.restitution = es.Restitution
				body.idleTime = es.IdleTime
				body.gravityScale = es.GravityScale
				// Store bounds in world units relative to position
				body.halfExtents = es.Model.AABBMax.Sub(es.Model.AABBMin).Mul(0.5 * world.VoxelSize)
			}
			// Cleanup dead entities
			snapMap := make(map[EntityId]bool)
			for _, es := range snap.Entities {
				snapMap[es.Eid] = true
			}
			for eid := range internalBodies {
				if !snapMap[eid] {
					delete(internalBodies, eid)
				}
			}
		}

		if len(internalBodies) == 0 {
			continue
		}

		dt := float32(1.0 / world.UpdateFrequency) // Fixed DT for stability
		gravity := world.Gravity

		// Simulate
		var bodies []*internalBody
		for _, b := range internalBodies {
			bodies = append(bodies, b)
		}

		for _, b := range bodies {
			if b.isStatic || b.sleeping {
				continue
			}

			// Apply Gravity
			if b.gravityScale != 0 {
				b.vel = b.vel.Add(gravity.Mul(b.gravityScale * dt))
			}

			// Apply Damping
			b.vel = b.vel.Mul(0.99)
			b.angVel = b.angVel.Mul(0.98)

			// Integrate linear
			oldPos := b.pos
			b.pos = b.pos.Add(b.vel.Mul(dt))

			// Integrate angular
			if b.angVel.Len() > 0 {
				angVelQuat := mgl32.Quat{W: 0, V: b.angVel.Mul(0.5 * dt)}
				b.rot = b.rot.Add(angVelQuat.Mul(b.rot))
				b.rot = b.rot.Normalize()
			}

			// Check and resolve collisions
			for _, other := range bodies {
				if b.Eid == other.Eid {
					continue
				}

				if collision, normal, penetration, contactPoint := checkOBBCollision(b, other); collision {
					// Static resolution: push out of collision
					b.pos = b.pos.Add(normal.Mul(penetration))

					// Relative velocity at contact point
					rA := contactPoint.Sub(b.pos)
					rB := contactPoint.Sub(other.pos)

					vA := b.vel.Add(b.angVel.Cross(rA))
					vB := other.vel
					if !other.isStatic {
						vB = other.vel.Add(other.angVel.Cross(rB))
					}

					relativeVel := vA.Sub(vB)
					velAlongNormal := relativeVel.Dot(normal)

					// Do not resolve if velocities are separating
					if velAlongNormal > 0 {
						continue
					}

					restitution := (b.restitution + other.restitution) * 0.5

					// Calculating impulse scalar j
					// j = -(1 + e) * v_rel . n / (1/mA + 1/mB + (rA x n)^2 / IA + (rB x n)^2 / IB)

					// Simplified Moment of Inertia for a cube: I = (1/6) * mass * size^2
					// size is approx 2 * average half extent
					avgSizeA := (b.halfExtents.X() + b.halfExtents.Y() + b.halfExtents.Z()) / 3.0 * 2.0
					inertiaA := (1.0 / 6.0) * b.mass * avgSizeA * avgSizeA

					denom := 1.0 / b.mass
					rAn := rA.Cross(normal)
					denom += rAn.Dot(rAn) / inertiaA

					if !other.isStatic && other.mass > 0 {
						avgSizeB := (other.halfExtents.X() + other.halfExtents.Y() + other.halfExtents.Z()) / 3.0 * 2.0
						inertiaB := (1.0 / 6.0) * other.mass * avgSizeB * avgSizeB
						denom += 1.0 / other.mass
						rBn := rB.Cross(normal)
						denom += rBn.Dot(rBn) / inertiaB
					}

					j := -(1 + restitution) * velAlongNormal
					j /= denom

					impulse := normal.Mul(j)

					// Apply linear impulse
					b.vel = b.vel.Add(impulse.Mul(1.0 / b.mass))

					// Apply angular impulse
					b.angVel = b.angVel.Add(rA.Cross(impulse).Mul(1.0 / inertiaA))

					if !other.isStatic && other.mass > 0 {
						avgSizeB := (other.halfExtents.X() + other.halfExtents.Y() + other.halfExtents.Z()) / 3.0 * 2.0
						inertiaB := (1.0 / 6.0) * other.mass * avgSizeB * avgSizeB
						other.vel = other.vel.Sub(impulse.Mul(1.0 / other.mass))
						other.angVel = other.angVel.Sub(rB.Cross(impulse).Mul(1.0 / inertiaB))
					}

					// Friction
					friction := (b.friction + other.friction) * 0.5
					tangent := relativeVel.Sub(normal.Mul(relativeVel.Dot(normal)))
					if tangent.Len() > 0.0001 {
						tangent = tangent.Normalize()
						jt := -relativeVel.Dot(tangent) * friction
						jt /= denom // Approximate denominator for friction

						fImpulse := tangent.Mul(jt)
						b.vel = b.vel.Add(fImpulse.Mul(1.0 / b.mass))
						b.angVel = b.angVel.Add(rA.Cross(fImpulse).Mul(1.0 / inertiaA))
					}

					b.Wake()
				}
			}

			// Sleeping logic
			if b.vel.Len() < world.SleepThreshold && b.angVel.Len() < world.SleepThreshold {
				b.idleTime += dt
				if b.idleTime > world.SleepTime {
					b.sleeping = true
					b.vel = mgl32.Vec3{0, 0, 0}
					b.angVel = mgl32.Vec3{0, 0, 0}
				}
			} else {
				b.idleTime = 0
			}

			_ = oldPos // To prevent unused variable error if I don't use it elsewhere
		}

		// Push results
		res := &PhysicsResults{}
		for _, b := range internalBodies {
			res.Entities = append(res.Entities, PhysicsEntityResult{
				Eid:      b.Eid,
				Pos:      b.pos,
				Rot:      b.rot,
				Vel:      b.vel,
				AngVel:   b.angVel,
				Sleeping: b.sleeping,
				IdleTime: b.idleTime,
			})
		}
		proxy.latestResults.Store(res)
	}
}

func (b *internalBody) Wake() {
	b.sleeping = false
	b.idleTime = 0
}

type internalBody struct {
	Eid          EntityId
	pos          mgl32.Vec3
	rot          mgl32.Quat
	vel          mgl32.Vec3
	angVel       mgl32.Vec3
	isStatic     bool
	mass         float32
	model        PhysicsModel
	halfExtents  mgl32.Vec3 // Half extents of the box in world units
	sleeping     bool
	idleTime     float32
	friction     float32
	restitution  float32
	gravityScale float32
}

func checkOBBCollision(a, b *internalBody) (bool, mgl32.Vec3, float32, mgl32.Vec3) {
	// AABB pre-check for performance
	posA := a.pos
	posB := b.pos

	rotA := a.rot.Mat4()
	rotB := b.rot.Mat4()

	axesA := [3]mgl32.Vec3{
		rotA.Col(0).Vec3(),
		rotA.Col(1).Vec3(),
		rotA.Col(2).Vec3(),
	}
	axesB := [3]mgl32.Vec3{
		rotB.Col(0).Vec3(),
		rotB.Col(1).Vec3(),
		rotB.Col(2).Vec3(),
	}

	L := posB.Sub(posA)

	minOverlap := float32(math.MaxFloat32)
	var collisionNormal mgl32.Vec3

	// SAT axes to check
	var testAxes []mgl32.Vec3
	for i := 0; i < 3; i++ {
		testAxes = append(testAxes, axesA[i])
		testAxes = append(testAxes, axesB[i])
	}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			cross := axesA[i].Cross(axesB[j])
			if cross.LenSqr() > 0.0001 {
				testAxes = append(testAxes, cross.Normalize())
			}
		}
	}

	for _, axis := range testAxes {
		overlap, collision := getOverlap(a, b, axesA, axesB, axis, L)
		if !collision {
			return false, mgl32.Vec3{}, 0, mgl32.Vec3{}
		}
		if overlap < minOverlap {
			minOverlap = overlap
			collisionNormal = axis
		}
	}

	// Ensure normal points from B to A (so we push A away)
	if L.Dot(collisionNormal) > 0 {
		collisionNormal = collisionNormal.Mul(-1)
	}

	// Contact point calculation
	contactPoint := findContactPoint(a, b, axesA, axesB)

	return true, collisionNormal, minOverlap, contactPoint
}

func findContactPoint(a, b *internalBody, axesA, axesB [3]mgl32.Vec3) mgl32.Vec3 {
	// Simple approach: test corners of a against b and vice versa
	cornersA := getCorners(a.pos, axesA, a.halfExtents)
	cornersB := getCorners(b.pos, axesB, b.halfExtents)

	var contactPoints []mgl32.Vec3
	for _, p := range cornersA {
		if isPointInOBB(p, b.pos, axesB, b.halfExtents) {
			contactPoints = append(contactPoints, p)
		}
	}
	for _, p := range cornersB {
		if isPointInOBB(p, a.pos, axesA, a.halfExtents) {
			contactPoints = append(contactPoints, p)
		}
	}

	if len(contactPoints) == 0 {
		// Fallback: average of positions
		return a.pos.Add(b.pos).Mul(0.5)
	}

	// Average of all points found
	avg := mgl32.Vec3{0, 0, 0}
	for _, p := range contactPoints {
		avg = avg.Add(p)
	}
	return avg.Mul(1.0 / float32(len(contactPoints)))
}

func getCorners(pos mgl32.Vec3, axes [3]mgl32.Vec3, halfExtents mgl32.Vec3) []mgl32.Vec3 {
	var corners []mgl32.Vec3
	for i := 0; i < 8; i++ {
		p := pos
		if i&1 != 0 {
			p = p.Add(axes[0].Mul(halfExtents.X()))
		} else {
			p = p.Sub(axes[0].Mul(halfExtents.X()))
		}
		if i&2 != 0 {
			p = p.Add(axes[1].Mul(halfExtents.Y()))
		} else {
			p = p.Sub(axes[1].Mul(halfExtents.Y()))
		}
		if i&4 != 0 {
			p = p.Add(axes[2].Mul(halfExtents.Z()))
		} else {
			p = p.Sub(axes[2].Mul(halfExtents.Z()))
		}
		corners = append(corners, p)
	}
	return corners
}

func isPointInOBB(p, pos mgl32.Vec3, axes [3]mgl32.Vec3, halfExtents mgl32.Vec3) bool {
	d := p.Sub(pos)
	for i := 0; i < 3; i++ {
		dist := float32(math.Abs(float64(d.Dot(axes[i]))))
		if dist > halfExtents[i]+0.01 { // Small epsilon
			return false
		}
	}
	return true
}

func getOverlap(a, b *internalBody, axesA, axesB [3]mgl32.Vec3, axis mgl32.Vec3, L mgl32.Vec3) (float32, bool) {
	projectionA := float32(0)
	for i := 0; i < 3; i++ {
		projectionA += float32(math.Abs(float64(axesA[i].Dot(axis)))) * a.halfExtents[i]
	}

	projectionB := float32(0)
	for i := 0; i < 3; i++ {
		projectionB += float32(math.Abs(float64(axesB[i].Dot(axis)))) * b.halfExtents[i]
	}

	distance := float32(math.Abs(float64(L.Dot(axis))))

	overlap := projectionA + projectionB - distance
	return overlap, overlap > 0
}

func (rb *RigidBodyComponent) ApplyTorque(torque mgl32.Vec3) {
	rb.Wake()
	// Simplified: no moment of inertia calculation for now, just apply directly
	// In a real engine, we'd scale by inverse inertia tensor.
	rb.AngularVelocity = rb.AngularVelocity.Add(torque.Mul(1.0 / rb.Mass))
}
