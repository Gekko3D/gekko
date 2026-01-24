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
	AABBMin mgl32.Vec3
	AABBMax mgl32.Vec3
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
	Vel          mgl32.Vec3
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
	Vel      mgl32.Vec3
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

		MakeQuery2[TransformComponent, RigidBodyComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent) bool {
			if res, ok := resMap[eid]; ok {
				tr.Position = res.Pos
				rb.Velocity = res.Vel
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
		snap.Entities = append(snap.Entities, PhysicsEntityState{
			Eid:          eid,
			Pos:          tr.Position,
			Vel:          rb.Velocity,
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
				body.vel = es.Vel
				body.isStatic = es.IsStatic
				body.mass = es.Mass
				body.model = es.Model
				body.friction = es.Friction
				body.restitution = es.Restitution
				body.idleTime = es.IdleTime
				body.gravityScale = es.GravityScale
				body.gravityScale = es.GravityScale
				// Store bounds in world units relative to position
				body.min = es.Model.AABBMin.Mul(world.VoxelSize)
				body.max = es.Model.AABBMax.Mul(world.VoxelSize)
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

			// Integrate
			displacement := b.vel.Mul(dt)

			// Resolve collisions axis by axis
			// (Using the same logic as before, but adapted to internalBody)
			b.pos, b.vel = physicsResolveAxis(bodies, b, b.pos, b.vel, displacement, 1, world.VoxelSize, b.friction, b.restitution)
			displacement = b.vel.Mul(dt)
			b.pos, b.vel = physicsResolveAxis(bodies, b, b.pos, b.vel, displacement, 0, world.VoxelSize, b.friction, b.restitution)
			displacement = b.vel.Mul(dt)
			b.pos, b.vel = physicsResolveAxis(bodies, b, b.pos, b.vel, displacement, 2, world.VoxelSize, b.friction, b.restitution)

			// Sleeping logic simplified for now
			if b.vel.Len() < world.SleepThreshold {
				b.idleTime += dt
				if b.idleTime > world.SleepTime {
					b.sleeping = true
					b.vel = mgl32.Vec3{0, 0, 0}
				}
			} else {
				b.idleTime = 0
			}
		}

		// Push results
		res := &PhysicsResults{}
		for _, b := range internalBodies {
			res.Entities = append(res.Entities, PhysicsEntityResult{
				Eid:      b.Eid,
				Pos:      b.pos,
				Vel:      b.vel,
				Sleeping: b.sleeping,
				IdleTime: b.idleTime,
			})
		}
		proxy.latestResults.Store(res)
	}
}

type internalBody struct {
	Eid          EntityId
	pos          mgl32.Vec3
	vel          mgl32.Vec3
	isStatic     bool
	mass         float32
	model        PhysicsModel
	min, max     mgl32.Vec3 // Bounds relative to position, in world units
	sleeping     bool
	idleTime     float32
	friction     float32
	restitution  float32
	gravityScale float32
}

func physicsResolveAxis(bodies []*internalBody, self *internalBody, pos, vel, displacement mgl32.Vec3, axis int, vSize, friction, restitution float32) (mgl32.Vec3, mgl32.Vec3) {
	newPos := pos
	dist := displacement[axis]
	if math.Abs(float64(dist)) < 0.0001 {
		return pos, vel
	}

	stepSize := float32(0.1)
	if dist < 0 {
		stepSize = -0.1
	}

	remaining := float32(math.Abs(float64(dist)))
	iterations := 0
	for remaining > 0 && iterations < 100 {
		iterations++
		move := stepSize
		if remaining < float32(math.Abs(float64(stepSize))) {
			move = dist / float32(math.Abs(float64(dist))) * remaining
		}

		testPos := newPos
		testPos[axis] += move

		if physicsCheckCollision(bodies, self, testPos) {
			// Apply restitution
			vel[axis] = -vel[axis] * restitution
			if math.Abs(float64(vel[axis])) < 0.1 {
				vel[axis] = 0
			}

			// Apply friction to tangential axes
			for a := 0; a < 3; a++ {
				if a != axis {
					vel[a] *= (1.0 - friction)
					if math.Abs(float64(vel[a])) < 0.01 {
						vel[a] = 0
					}
				}
			}
			break
		}
		newPos = testPos
		remaining -= float32(math.Abs(float64(move)))
	}

	return newPos, vel
}

func physicsCheckCollision(bodies []*internalBody, self *internalBody, pos mgl32.Vec3) bool {
	selfMin := pos.Add(self.min)
	selfMax := pos.Add(self.max)

	for _, other := range bodies {
		if other.Eid == self.Eid {
			continue
		}

		// AABB Check
		otherMin := other.pos.Add(other.min)
		otherMax := other.pos.Add(other.max)

		if selfMin.X() < otherMax.X() && selfMax.X() > otherMin.X() &&
			selfMin.Y() < otherMax.Y() && selfMax.Y() > otherMin.Y() &&
			selfMin.Z() < otherMax.Z() && selfMax.Z() > otherMin.Z() {
			return true
		}
	}
	return false
}

// physicsResolveAxis and physicsCheckCollision are already implemented above.
