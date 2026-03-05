package gekko

import (
	"math"
	"time"

	"github.com/go-gl/mathgl/mgl32"
)

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
				if es.Teleport {
					body.pos = es.Pos
					body.rot = es.Rot
				}
				body.vel = es.Vel
				body.angVel = es.AngVel
				body.isStatic = es.IsStatic
				body.mass = es.Mass
				body.model = es.Model
				body.friction = es.Friction
				body.restitution = es.Restitution
				body.gravityScale = es.GravityScale
				body.sleeping = es.Sleeping
				body.linearDamping = es.LinearDamping
				body.angularDamping = es.AngularDamping
				// Store boxes
				body.boxes = make([]InternalBox, len(es.Model.Boxes))
				for i, box := range es.Model.Boxes {
					body.boxes[i].Box = box
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

			// Apply Damping (per-body or light default)
			lDamp := float32(0.999)
			if b.linearDamping > 0 {
				lDamp = b.linearDamping
			}
			aDamp := float32(0.99)
			if b.angularDamping > 0 {
				aDamp = b.angularDamping
			}

			b.vel = b.vel.Mul(lDamp)
			b.angVel = b.angVel.Mul(aDamp)

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

				for _, boxA := range b.boxes {
					for _, boxB := range other.boxes {
						// Box-Box AABB check using pre-calculated bounds
						if boxA.Min.X() > boxB.Max.X() || boxA.Max.X() < boxB.Min.X() ||
							boxA.Min.Y() > boxB.Max.Y() || boxA.Max.Y() < boxB.Min.Y() ||
							boxA.Min.Z() > boxB.Max.Z() || boxA.Max.Z() < boxB.Min.Z() {
							continue
						}

						if collision, normal, penetration, contactPoint := checkSingleOBBCollision(b.pos, b.rot, boxA.Box, other.pos, other.rot, boxB.Box); collision {
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
								continue
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
	Eid            EntityId
	pos            mgl32.Vec3
	rot            mgl32.Quat
	vel            mgl32.Vec3
	angVel         mgl32.Vec3
	isStatic       bool
	mass           float32
	model          PhysicsModel
	boxes          []InternalBox
	sleeping       bool
	idleTime       float32
	friction       float32
	restitution    float32
	gravityScale   float32
	linearDamping  float32
	angularDamping float32
	aabbMin        mgl32.Vec3
	aabbMax        mgl32.Vec3
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
