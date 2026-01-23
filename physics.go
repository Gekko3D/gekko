package gekko

import (
	"math"

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

type PhysicsWorld struct {
	Gravity        mgl32.Vec3
	VoxelSize      float32
	SleepThreshold float32
	SleepTime      float32
}

func NewPhysicsWorld() *PhysicsWorld {
	return &PhysicsWorld{
		Gravity:        mgl32.Vec3{0, -9.81, 0},
		VoxelSize:      0.1,
		SleepThreshold: 0.05,
		SleepTime:      1.0,
	}
}

type PhysicsModule struct{}

func (m PhysicsModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(NewPhysicsWorld())

	app.UseSystem(
		System(PhysicsSystem).
			InStage(Update).
			RunAlways(),
	)
}

type BodyInfo struct {
	Eid           EntityId
	Tr            *TransformComponent
	Rb            *RigidBodyComponent
	Col           *ColliderComponent
	ScaledExtents mgl32.Vec3
	Model         *VoxModel
}

func PhysicsSystem(cmd *Commands, time *Time, physics *PhysicsWorld, vrs *VoxelRtState, assets *AssetServer, grid *SpatialHashGrid) {
	dt := float32(time.Dt)
	if dt <= 0 || dt > 1.0 { // Safety cap for dt
		return
	}

	// 1. Sync Voxel Science from RtState resource
	if vrs != nil && vrs.RtApp != nil && vrs.RtApp.Scene != nil {
		physics.VoxelSize = vrs.RtApp.Scene.TargetVoxelSize
	}

	// 2. Collect all active colliders for inter-entity collision details
	// We still need a map or lookup to get Component Data from EntityID returned by Grid
	// The Grid gives us IDs. We need to access RB/Col/Tr for those IDs.
	// ECS currently doesn't support Random Access by ID efficiently without a map or query.
	// But `bodies` slice built here is basically that map (indexable by loop, but lookup by ID is O(N)).
	// Let's build a map for quick lookup:  EntityId -> *BodyInfo
	bodyMap := make(map[EntityId]*BodyInfo)
	var bodies []BodyInfo // Keep slice for iteration over active bodies

	MakeQuery4[TransformComponent, RigidBodyComponent, ColliderComponent, VoxelModelComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, col *ColliderComponent, vmc *VoxelModelComponent) bool {
		scaledHalfExtents := mgl32.Vec3{
			col.AABBHalfExtents.X() * tr.Scale.X(),
			col.AABBHalfExtents.Y() * tr.Scale.Y(),
			col.AABBHalfExtents.Z() * tr.Scale.Z(),
		}
		// Validate extents
		if scaledHalfExtents.X() < 0.001 {
			scaledHalfExtents[0] = 0.001
		}
		if scaledHalfExtents.Y() < 0.001 {
			scaledHalfExtents[1] = 0.001
		}
		if scaledHalfExtents.Z() < 0.001 {
			scaledHalfExtents[2] = 0.001
		}

		var model *VoxModel
		if vmc != nil && assets != nil {
			if vmAsset, ok := assets.voxModels[vmc.VoxelModel]; ok {
				model = &vmAsset.VoxModel
			}
		}

		info := BodyInfo{eid, tr, rb, col, scaledHalfExtents, model}
		bodies = append(bodies, info)
		// We insert pointer to the slice element? No, slice reallocates.
		// We can't store pointer to slice element if we append.
		// So we loop AGAIN to build map? Or just store by value in map?
		// Map of pointers is better if we want to mutate?
		// Actually, we are mutating `tr.Position` and `rb.Velocity`.
		// Identify: BodyInfo contains POINTERS to components. So copying BodyInfo is fine.
		// The components themselves are pointers in ECS Query (usually).
		// Let's verify ECS Map signature: `func(eid, *T, *U...)`. Yes, pointers to component data.
		// So `info` contains pointers `tr`, `rb`, `col`.
		// Safe to copy `info` struct.
		// bodyMap[eid] = &info // Warning: Taking address of loop variable or local...
		// We need to append to slice first, then take address from slice?
		// Actually, we can just use the map primarily? Or just iterate the slice.
		return true
	}, VoxelModelComponent{})

	// Rebuild map from slice to be safe
	for i := range bodies {
		bodyMap[bodies[i].Eid] = &bodies[i]
	}

	// 3. Update Rigid Bodies
	for i := range bodies {
		b := &bodies[i]
		if b.Rb.IsStatic || b.Rb.Sleeping {
			continue
		}

		// Apply Gravity
		if b.Rb.GravityScale != 0 {
			b.Rb.Velocity = b.Rb.Velocity.Add(physics.Gravity.Mul(b.Rb.GravityScale * dt))
		}

		// Integrate Position
		displacement := b.Rb.Velocity.Mul(dt)

		// NaN/Inf check
		if math.IsNaN(float64(displacement.Len())) || math.IsInf(float64(displacement.Len()), 0) {
			b.Rb.Velocity = mgl32.Vec3{0, 0, 0}
			continue
		}

		// Resolve collisions axis by axis for stability
		startPos := b.Tr.Position

		friction := b.Col.Friction
		restitution := b.Col.Restitution

		// Broadphase Query using SpatialGrid
		// We need potential colliders for THIS entity.
		// Construct AABB for the query (include displacement to catch fast objects involved?)
		// For now, query AABB at current position + expanded by displacement/velocity?
		// Or just Current AABB?
		// Simple approach: Current AABB extended by displacement.
		// Let's just use the larger of current vs current+disp?
		// Safe bet: Query around current pos with slightly larger radius/aabb.
		// Let's use `SpatialHashGrid`'s Inserted AABBs (which are frame-start AABBs).
		// We use `b.Col.AABBHalfExtents` (scaled).

		queryAABB := AABBComponent{
			Min: b.Tr.Position.Sub(b.ScaledExtents).Sub(mgl32.Vec3{0.5, 0.5, 0.5}), // Margin
			Max: b.Tr.Position.Add(b.ScaledExtents).Add(mgl32.Vec3{0.5, 0.5, 0.5}),
		}
		// Only if we have a Grid
		var candidateIds []EntityId
		if grid != nil {
			candidateIds = grid.QueryAABB(queryAABB)
		} else {
			// Fallback if no grid (shouldn't happen if module installed)
			// But strictly following signature...
		}

		// Filter candidates to BodyInfo list
		var candidates []*BodyInfo
		if grid != nil {
			for _, cid := range candidateIds {
				if other, ok := bodyMap[cid]; ok {
					candidates = append(candidates, other)
				}
			}
		} else {
			// Fallback: All bodies
			for k := range bodies {
				candidates = append(candidates, &bodies[k])
			}
		}

		// Y Axis
		b.Tr.Position, b.Rb.Velocity = PhysicsResolveAxis(candidates, b, b.Tr.Position, b.Rb.Velocity, displacement, 1, physics.VoxelSize, friction, restitution)

		// X & Z
		displacement = b.Rb.Velocity.Mul(dt)
		b.Tr.Position, b.Rb.Velocity = PhysicsResolveAxis(candidates, b, b.Tr.Position, b.Rb.Velocity, displacement, 0, physics.VoxelSize, friction, restitution)
		displacement = b.Rb.Velocity.Mul(dt)
		b.Tr.Position, b.Rb.Velocity = PhysicsResolveAxis(candidates, b, b.Tr.Position, b.Rb.Velocity, displacement, 2, physics.VoxelSize, friction, restitution)

		// 4. Wake neighbors if we moved
		moveDist := b.Tr.Position.Sub(startPos).Len()
		if moveDist > 0.001 {
			// We can use the same candidates for waking up?
			// Yes, neighbors are likely in the same grid cells.
			for _, other := range candidates {
				if other.Rb.Sleeping && other.Eid != b.Eid {
					// Check if 'other' is touching 'b' (with some margin)
					margin := float32(0.05)

					// AABB collision with margin
					if math.Abs(float64(other.Tr.Position.X()-b.Tr.Position.X())) < float64(other.ScaledExtents.X()+b.ScaledExtents.X()+margin) &&
						math.Abs(float64(other.Tr.Position.Y()-b.Tr.Position.Y())) < float64(other.ScaledExtents.Y()+b.ScaledExtents.Y()+margin) &&
						math.Abs(float64(other.Tr.Position.Z()-b.Tr.Position.Z())) < float64(other.ScaledExtents.Z()+b.ScaledExtents.Z()+margin) {

						other.Rb.Wake()
					}
				}
			}
		}

		// 5. Sleeping Logic
		velLen := b.Rb.Velocity.Len()
		if velLen < physics.SleepThreshold {
			b.Rb.IdleTime += dt
			if b.Rb.IdleTime > physics.SleepTime {
				b.Rb.Sleeping = true
				b.Rb.Velocity = mgl32.Vec3{0, 0, 0}
			}
		} else {
			b.Rb.IdleTime = 0
		}
	}
}

func PhysicsResolveAxis(bodies []*BodyInfo, self *BodyInfo, pos, vel, displacement mgl32.Vec3, axis int, vSize, friction, restitution float32) (mgl32.Vec3, mgl32.Vec3) {
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
	if remaining > 10.0 { // Safety cap: max 10 meters per frame resolution
		remaining = 10.0
	}

	iterations := 0
	for remaining > 0 && iterations < 200 { // Max 200 steps
		iterations++
		move := stepSize
		if remaining < float32(math.Abs(float64(stepSize))) {
			move = dist / float32(math.Abs(float64(dist))) * remaining
		}

		testPos := newPos
		testPos[axis] += move

		if PhysicsCheckCollision(bodies, self, testPos, self.ScaledExtents, vSize) {
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

func PhysicsCheckCollision(bodies []*BodyInfo, self *BodyInfo, pos mgl32.Vec3, halfExtents mgl32.Vec3, vSize float32) bool {
	if halfExtents.X() < 0.001 || halfExtents.Y() < 0.001 || halfExtents.Z() < 0.001 {
		return false
	}

	// 1. World Voxel Collision removed (WorldComponent deleted)

	// 2. Entity-vs-Entity AABB Collision
	for _, other := range bodies {
		if self != nil && other.Eid == self.Eid {
			continue
		}

		// AABB Check
		otherPos := other.Tr.Position
		otherExt := other.ScaledExtents

		if math.Abs(float64(pos.X()-otherPos.X())) < float64(halfExtents.X()+otherExt.X()) &&
			math.Abs(float64(pos.Y()-otherPos.Y())) < float64(halfExtents.Y()+otherExt.Y()) &&
			math.Abs(float64(pos.Z()-otherPos.Z())) < float64(halfExtents.Z()+otherExt.Z()) {
			return true
		}
	}

	return false
}
