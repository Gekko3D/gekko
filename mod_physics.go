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

type VoxelCategory int

const (
	VoxelCategoryInternal VoxelCategory = iota
	VoxelCategoryFace
	VoxelCategoryEdge
	VoxelCategoryCorner
)

type VoxelData struct {
	RelativePos mgl32.Vec3
	Category    VoxelCategory
}

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

type CollisionBox struct {
	HalfExtents mgl32.Vec3
	LocalOffset mgl32.Vec3 // Offset relative to the body's local origin
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
	Boxes        []CollisionBox
	CenterOffset mgl32.Vec3 // Global offset for the whole model (e.g. for AABB pre-calc)
	// KeyPoints will contain corner and edge key-points in future phases.
	KeyPoints []mgl32.Vec3
	Voxels    []VoxelData
	GridSize  [3]uint32
}

type CollisionMode int

const (
	CollisionModeOBB CollisionMode = iota
	CollisionModeVoxel
)

type PhysicsWorld struct {
	Gravity         mgl32.Vec3
	VoxelSize       float32
	SleepThreshold  float32
	SleepTime       float32
	UpdateFrequency float32 // Hz
	CollisionMode   CollisionMode
}

func NewPhysicsWorld() *PhysicsWorld {
	return &PhysicsWorld{
		Gravity:         mgl32.Vec3{0, -9.81, 0},
		VoxelSize:       0.1,
		SleepThreshold:  0.05,
		SleepTime:       1.0,
		UpdateFrequency: 60.0,
		CollisionMode:   CollisionModeOBB, // Default to OBB for backward compatibility
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
				// Store boxes
				body.boxes = make([]InternalBox, len(es.Model.Boxes))
				for i, box := range es.Model.Boxes {
					body.boxes[i].Box = box
				}
				// Store voxels and build lookup
				body.voxels = es.Model.Voxels
				body.gridLookup = make(map[[3]int32]bool)
				for _, v := range body.voxels {
					// Extract grid coordinates from relative position
					// RelativePos = (coord + 0.5) * VoxelSize - CenterOffset
					// So coord = (RelativePos + CenterOffset) / VoxelSize - 0.5
					// But we can just store the original coordinates if we update PhysicsModel.
					// For now, let's reverse it accurately.
					coordX := int32(math.Round(float64((v.RelativePos.X()+es.Model.CenterOffset.X())/world.VoxelSize - 0.5)))
					coordY := int32(math.Round(float64((v.RelativePos.Y()+es.Model.CenterOffset.Y())/world.VoxelSize - 0.5)))
					coordZ := int32(math.Round(float64((v.RelativePos.Z()+es.Model.CenterOffset.Z())/world.VoxelSize - 0.5)))
					body.gridLookup[[3]int32{coordX, coordY, coordZ}] = true
				}
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
			b.updateAABB()
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

			// Apply Damping (more aggressive to reduce jitter)
			b.vel = b.vel.Mul(0.98)
			b.angVel = b.angVel.Mul(0.95)

			// Integrate linear
			oldPos := b.pos
			b.pos = b.pos.Add(b.vel.Mul(dt))

			// Integrate angular
			if b.angVel.Len() > 0 {
				angVelQuat := mgl32.Quat{W: 0, V: b.angVel.Mul(0.5 * dt)}
				b.rot = b.rot.Add(angVelQuat.Mul(b.rot))
				b.rot = b.rot.Normalize()
				b.updateAABB()
			}

			// Check and resolve collisions
			for _, other := range bodies {
				if b.Eid == other.Eid {
					continue
				}

				// Broad-phase: Body-Body AABB
				if b.aabbMin.X() > other.aabbMax.X() || b.aabbMax.X() < other.aabbMin.X() ||
					b.aabbMin.Y() > other.aabbMax.Y() || b.aabbMax.Y() < other.aabbMin.Y() ||
					b.aabbMin.Z() > other.aabbMax.Z() || b.aabbMax.Z() < other.aabbMin.Z() {
					continue
				}

				if world.CollisionMode == CollisionModeVoxel {
					if collision, normal, penetration, contactPoint := checkVoxelCollision(b, other, world); collision {
						resolveCollision(b, other, normal, penetration, contactPoint)
					}
				} else {
					for _, boxA := range b.boxes {
						for _, boxB := range other.boxes {
							// Box-Box AABB check using pre-calculated bounds
							if boxA.Min.X() > boxB.Max.X() || boxA.Max.X() < boxB.Min.X() ||
								boxA.Min.Y() > boxB.Max.Y() || boxA.Max.Y() < boxB.Min.Y() ||
								boxA.Min.Z() > boxB.Max.Z() || boxA.Max.Z() < boxB.Min.Z() {
								continue
							}

							if collision, normal, penetration, contactPoint := checkSingleOBBCollision(b.pos, b.rot, boxA.Box, other.pos, other.rot, boxB.Box); collision {
								resolveCollision(b, other, normal, penetration, contactPoint)
							}
						}
					}
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

type InternalBox struct {
	Box      CollisionBox
	Min, Max mgl32.Vec3
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
	boxes        []InternalBox
	sleeping     bool
	idleTime     float32
	friction     float32
	restitution  float32
	gravityScale float32
	aabbMin      mgl32.Vec3
	aabbMax      mgl32.Vec3
	voxels       []VoxelData
	gridLookup   map[[3]int32]bool
}

func resolveCollision(b, other *internalBody, normal mgl32.Vec3, penetration float32, contactPoint mgl32.Vec3) {
	// Static resolution: push out of collision
	// Use a small slop to avoid jittering
	slop := float32(0.02)
	if penetration > slop {
		b.pos = b.pos.Add(normal.Mul(penetration - slop))
	}

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
		return
	}

	restitution := (b.restitution + other.restitution) * 0.5
	// If velocity is low, disable restitution to help settling
	if velAlongNormal > -0.5 {
		restitution = 0
	}

	inertiaA := calculateInertia(b)

	denom := 1.0 / b.mass
	rAn := rA.Cross(normal)
	denom += rAn.Dot(rAn) / inertiaA

	if !other.isStatic && other.mass > 0 {
		inertiaB := calculateInertia(other)
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
		inertiaB := calculateInertia(other)
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

	// Stabilization: if velocity is very low after resolution, zero it
	if b.vel.Len() < 0.01 {
		b.vel = mgl32.Vec3{0, 0, 0}
	}
	if b.angVel.Len() < 0.01 {
		b.angVel = mgl32.Vec3{0, 0, 0}
	}

	// Only wake if we have significant relative velocity
	if math.Abs(float64(velAlongNormal)) > 0.1 {
		b.Wake()
	}
}

func checkVoxelCollision(b1, b2 *internalBody, world *PhysicsWorld) (bool, mgl32.Vec3, float32, mgl32.Vec3) {
	// 1. Broad-phase AABB sub-volume
	minV := mgl32.Vec3{
		float32(math.Max(float64(b1.aabbMin.X()), float64(b2.aabbMin.X()))),
		float32(math.Max(float64(b1.aabbMin.Y()), float64(b2.aabbMin.Y()))),
		float32(math.Max(float64(b1.aabbMin.Z()), float64(b2.aabbMin.Z()))),
	}
	maxV := mgl32.Vec3{
		float32(math.Min(float64(b1.aabbMax.X()), float64(b2.aabbMax.X()))),
		float32(math.Min(float64(b1.aabbMax.Y()), float64(b2.aabbMax.Y()))),
		float32(math.Min(float64(b1.aabbMax.Z()), float64(b2.aabbMax.Z()))),
	}

	// Expand sub-volume slightly
	padding := world.VoxelSize * 0.5
	minV = minV.Sub(mgl32.Vec3{padding, padding, padding})
	maxV = maxV.Add(mgl32.Vec3{padding, padding, padding})

	if minV.X() > maxV.X() || minV.Y() > maxV.Y() || minV.Z() > maxV.Z() {
		return false, mgl32.Vec3{}, 0, mgl32.Vec3{}
	}

	var collisionPoints []mgl32.Vec3
	var collisionNormals []mgl32.Vec3
	var penetrations []float32

	type deepContact struct {
		cp         mgl32.Vec3
		norm       mgl32.Vec3
		pen        float32
		flipNormal bool
	}
	var potentialDeepContacts []deepContact

	invRot1 := b1.rot.Conjugate()
	invRot2 := b2.rot.Conjugate()

	halfVoxel := world.VoxelSize / 2.0

	// Helper for checking neighbors in body's local space
	checkNeighbors := func(v1Index int, sourceBody, targetBody *internalBody, invRotTarget mgl32.Quat, flipNormal bool) bool {
		v1 := sourceBody.voxels[v1Index]
		worldPoint := sourceBody.pos.Add(sourceBody.rot.Rotate(v1.RelativePos))
		localPoint := invRotTarget.Rotate(worldPoint.Sub(targetBody.pos))

		// Convert to grid coordinates in target body
		fx := (localPoint.X()+targetBody.model.CenterOffset.X())/world.VoxelSize - 0.5
		fy := (localPoint.Y()+targetBody.model.CenterOffset.Y())/world.VoxelSize - 0.5
		fz := (localPoint.Z()+targetBody.model.CenterOffset.Z())/world.VoxelSize - 0.5

		ix, iy, iz := int32(math.Floor(float64(fx))), int32(math.Floor(float64(fy))), int32(math.Floor(float64(fz)))

		found := false

		// Define the source box for SAT
		box1 := CollisionBox{
			HalfExtents: mgl32.Vec3{halfVoxel, halfVoxel, halfVoxel},
			LocalOffset: v1.RelativePos,
		}

		// Check 2x2x2 neighborhood
		for dx := int32(0); dx <= 1; dx++ {
			for dy := int32(0); dy <= 1; dy++ {
				for dz := int32(0); dz <= 1; dz++ {
					tx, ty, tz := ix+dx, iy+dy, iz+dz
					if targetBody.gridLookup[[3]int32{tx, ty, tz}] {
						// Voxel center in target's local space
						targetRelPos := mgl32.Vec3{
							(float32(tx)+0.5)*world.VoxelSize - targetBody.model.CenterOffset.X(),
							(float32(ty)+0.5)*world.VoxelSize - targetBody.model.CenterOffset.Y(),
							(float32(tz)+0.5)*world.VoxelSize - targetBody.model.CenterOffset.Z(),
						}

						box2 := CollisionBox{
							HalfExtents: mgl32.Vec3{halfVoxel, halfVoxel, halfVoxel},
							LocalOffset: targetRelPos,
						}

						if hit, norm, pen, cp := checkSingleOBBCollision(sourceBody.pos, sourceBody.rot, box1, targetBody.pos, targetBody.rot, box2); hit {
							// Stability Fix: Only accept normals that point towards an open face of the target voxel.
							// norm points from Target -> Source (CollisionBox2 -> CollisionBox1).
							// Transform normal to target's local space to check grid alignment.
							localNorm := invRotTarget.Rotate(norm)

							// Find dominant axis in LOCAL space to pick neighbor direction
							absX := math.Abs(float64(localNorm.X()))
							absY := math.Abs(float64(localNorm.Y()))
							absZ := math.Abs(float64(localNorm.Z()))

							validNormal := true

							// Check if the neighbor in the direction of the normal is also occupied
							if absX > absY && absX > absZ {
								neighborX := tx
								if localNorm.X() > 0 {
									neighborX++
								} else {
									neighborX--
								}
								if targetBody.gridLookup[[3]int32{neighborX, ty, tz}] {
									validNormal = false
								}
							} else if absY > absX && absY > absZ {
								neighborY := ty
								if localNorm.Y() > 0 {
									neighborY++
								} else {
									neighborY--
								}
								if targetBody.gridLookup[[3]int32{tx, neighborY, tz}] {
									validNormal = false
								}
							} else {
								neighborZ := tz
								if localNorm.Z() > 0 {
									neighborZ++
								} else {
									neighborZ--
								}
								if targetBody.gridLookup[[3]int32{tx, ty, neighborZ}] {
									validNormal = false
								}
							}

							// Extra Stability Check: If objects are roughly aligned (flat stacking),
							// we should prefer normals that align with the global Up axis (Y) to prevent sliding.
							// This is a heuristic for voxel games where gravity is -Y.
							if validNormal && norm.Y() > 0.9 {
								// Keep this strong Up normal
							} else if validNormal {
								// If we have a valid local normal, but it's skewed (e.g. corner collision),
								// and we are simply stacking, this might cause sliding.
								// However, for generic physics we shouldn't force it too much.
								// Let's trust the SAT but maybe filter out very small penetrations that are just noise on the sides.
								if pen < 0.005 {
									// Ignore tiny side touches if we are finding better contacts elsewhere
									// But for now, let's keep it simple.
								}
							}

							if validNormal {
								collisionPoints = append(collisionPoints, cp)
								if flipNormal {
									collisionNormals = append(collisionNormals, norm.Mul(-1))
								} else {
									collisionNormals = append(collisionNormals, norm)
								}
								penetrations = append(penetrations, pen)
								found = true
							}
						}
					}
				}
			}
		}
		return found
	}

	// 2. Sample categorized voxels from B1 against B2 grid
	for i, v1 := range b1.voxels {
		if v1.Category != VoxelCategoryCorner && v1.Category != VoxelCategoryEdge {
			continue
		}
		// checkSingleOBBCollision returns normal pointing towards Source (b1).
		// We want normal pointing towards b1. So flip=false.
		checkNeighbors(i, b1, b2, invRot2, false)
	}

	// 3. Sample categorized voxels from B2 against B1 grid
	for i, v2 := range b2.voxels {
		if v2.Category != VoxelCategoryCorner && v2.Category != VoxelCategoryEdge {
			continue
		}
		// checkSingleOBBCollision returns normal pointing towards Source (b2).
		// We want normal pointing towards b1. So flip=true.
		checkNeighbors(i, b2, b1, invRot1, true)
	}

	if len(collisionPoints) == 0 {
		// Fallback: If no "valid" open-face collisions, but we have deep contacts, use them.
		if len(potentialDeepContacts) > 0 {
			for _, dc := range potentialDeepContacts {
				collisionPoints = append(collisionPoints, dc.cp)
				if dc.flipNormal {
					collisionNormals = append(collisionNormals, dc.norm.Mul(-1))
				} else {
					collisionNormals = append(collisionNormals, dc.norm)
				}
				penetrations = append(penetrations, dc.pen)
			}
		} else {
			return false, mgl32.Vec3{}, 0, mgl32.Vec3{}
		}
	}

	// 5. Manifold Reduction: Prioritize Deepest Contacts
	// Averaging everything causes jitter when many shallow contacts fight with few deep ones.
	// New strategy:
	// 1. Find max penetration
	// 2. Average only contacts within a threshold of max penetration (e.g. 10%)

	maxPen := float32(-1.0)
	for _, p := range penetrations {
		if p > maxPen {
			maxPen = p
		}
	}

	// Threshold: keep contacts that are within a small absolute distance of the deepest penetration.
	// Using a percentage (90%) works for deep collisions but can be too strict or loose for shallow ones.
	// A fixed small tolerance (e.g. 1mm) is better for stability.
	threshold := maxPen - 0.01 // 1 cm tolerance
	if threshold < 0 {
		threshold = 0
	}

	avgPos := mgl32.Vec3{0, 0, 0}
	avgNormal := mgl32.Vec3{0, 0, 0}
	count := float32(0)

	for i := range collisionPoints {
		if penetrations[i] >= threshold {
			avgPos = avgPos.Add(collisionPoints[i])
			avgNormal = avgNormal.Add(collisionNormals[i])
			count++
		}
	}

	if count > 0 {
		avgPos = avgPos.Mul(1.0 / count)
		if avgNormal.LenSqr() > 1e-6 {
			avgNormal = avgNormal.Normalize()
		} else {
			avgNormal = mgl32.Vec3{0, 1, 0} // Fallback
		}
	} else {
		// Should not happen if logic is correct, but safe fallback
		return false, mgl32.Vec3{}, 0, mgl32.Vec3{}
	}

	return true, avgNormal, maxPen, avgPos
}

func (b *internalBody) updateAABB() {
	if len(b.boxes) == 0 {
		b.aabbMin = b.pos
		b.aabbMax = b.pos
		return
	}

	minP := mgl32.Vec3{1e9, 1e9, 1e9}
	maxP := mgl32.Vec3{-1e9, -1e9, -1e9}

	rotMat := b.rot.Mat4()
	axes := [3]mgl32.Vec3{rotMat.Col(0).Vec3(), rotMat.Col(1).Vec3(), rotMat.Col(2).Vec3()}

	for i := range b.boxes {
		box := &b.boxes[i]
		worldBoxPos := b.pos.Add(b.rot.Rotate(box.Box.LocalOffset))

		// Calculate world-space AABB of this OBB
		extents := mgl32.Vec3{0, 0, 0}
		for i := 0; i < 3; i++ {
			for j := 0; j < 3; j++ {
				extents[i] += float32(math.Abs(float64(axes[j][i]))) * box.Box.HalfExtents[j]
			}
		}

		box.Min = worldBoxPos.Sub(extents)
		box.Max = worldBoxPos.Add(extents)

		for i := 0; i < 3; i++ {
			minP[i] = float32(math.Min(float64(minP[i]), float64(box.Min[i])))
			maxP[i] = float32(math.Max(float64(maxP[i]), float64(box.Max[i])))
		}
	}

	b.aabbMin = minP
	b.aabbMax = maxP
}

func calculateInertia(b *internalBody) float32 {
	if len(b.boxes) == 0 {
		return 1.0
	}

	totalInertia := float32(0)
	totalMass := b.mass
	if totalMass <= 0 {
		return 1.0
	}

	// Calculate mass distribution assuming uniform density
	totalVolume := float32(0)
	for _, box := range b.boxes {
		totalVolume += box.Box.HalfExtents.X() * box.Box.HalfExtents.Y() * box.Box.HalfExtents.Z() * 8.0
	}

	for _, box := range b.boxes {
		volume := box.Box.HalfExtents.X() * box.Box.HalfExtents.Y() * box.Box.HalfExtents.Z() * 8.0
		boxMass := (volume / totalVolume) * totalMass

		// Inertia of this box about its own center
		sizeSq := box.Box.HalfExtents.LenSqr() * 4.0
		boxInertia := (1.0 / 6.0) * boxMass * sizeSq

		// Parallel Axis Theorem
		distSq := box.Box.LocalOffset.LenSqr()
		totalInertia += boxInertia + boxMass*distSq
	}

	return totalInertia
}

func checkSingleOBBCollision(posA mgl32.Vec3, rotA mgl32.Quat, boxA CollisionBox, posB mgl32.Vec3, rotB mgl32.Quat, boxB CollisionBox) (bool, mgl32.Vec3, float32, mgl32.Vec3) {
	worldPosA := posA.Add(rotA.Rotate(boxA.LocalOffset))
	worldPosB := posB.Add(rotB.Rotate(boxB.LocalOffset))

	matA := rotA.Mat4()
	matB := rotB.Mat4()

	axesA := [3]mgl32.Vec3{matA.Col(0).Vec3(), matA.Col(1).Vec3(), matA.Col(2).Vec3()}
	axesB := [3]mgl32.Vec3{matB.Col(0).Vec3(), matB.Col(1).Vec3(), matB.Col(2).Vec3()}

	L := worldPosB.Sub(worldPosA)
	minOverlap := float32(math.MaxFloat32)
	var collisionNormal mgl32.Vec3

	var testAxes []mgl32.Vec3
	for i := 0; i < 3; i++ {
		testAxes = append(testAxes, axesA[i], axesB[i])
	}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			cross := axesA[i].Cross(axesB[j])
			if cross.LenSqr() > 1e-4 {
				testAxes = append(testAxes, cross.Normalize())
			}
		}
	}

	for _, axis := range testAxes {
		projectionA := float32(0)
		for i := 0; i < 3; i++ {
			projectionA += float32(math.Abs(float64(axesA[i].Dot(axis)))) * boxA.HalfExtents[i]
		}
		projectionB := float32(0)
		for i := 0; i < 3; i++ {
			projectionB += float32(math.Abs(float64(axesB[i].Dot(axis)))) * boxB.HalfExtents[i]
		}
		distance := float32(math.Abs(float64(L.Dot(axis))))
		overlap := projectionA + projectionB - distance
		if overlap <= 0 {
			return false, mgl32.Vec3{}, 0, mgl32.Vec3{}
		}
		if overlap < minOverlap {
			minOverlap = overlap
			collisionNormal = axis
		}
	}

	if L.Dot(collisionNormal) > 0 {
		collisionNormal = collisionNormal.Mul(-1)
	}

	// Contact point
	cornersA := getCorners(worldPosA, axesA, boxA.HalfExtents)
	cornersB := getCorners(worldPosB, axesB, boxB.HalfExtents)
	var contactPoints []mgl32.Vec3
	for _, p := range cornersA {
		if isPointInOBB(p, worldPosB, axesB, boxB.HalfExtents) {
			contactPoints = append(contactPoints, p)
		}
	}
	for _, p := range cornersB {
		if isPointInOBB(p, worldPosA, axesA, boxA.HalfExtents) {
			contactPoints = append(contactPoints, p)
		}
	}

	var cp mgl32.Vec3
	if len(contactPoints) == 0 {
		cp = worldPosA.Add(worldPosB).Mul(0.5)
	} else {
		for _, p := range contactPoints {
			cp = cp.Add(p)
		}
		cp = cp.Mul(1.0 / float32(len(contactPoints)))
	}

	return true, collisionNormal, minOverlap, cp
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

func (rb *RigidBodyComponent) ApplyTorque(torque mgl32.Vec3) {
	rb.Wake()
	// Simplified: no moment of inertia calculation for now, just apply directly
	// In a real engine, we'd scale by inverse inertia tensor.
	rb.AngularVelocity = rb.AngularVelocity.Add(torque.Mul(1.0 / rb.Mass))
}
