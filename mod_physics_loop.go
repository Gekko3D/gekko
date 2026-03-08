package gekko

import (
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/go-gl/mathgl/mgl32"
)

func physicsLoop(world *PhysicsWorld, proxy *PhysicsProxy) {
	ticker := time.NewTicker(time.Duration(1000.0/world.UpdateFrequency) * time.Millisecond)
	defer ticker.Stop()

	// Internal state
	internalBodies := make(map[EntityId]*internalBody)
	var tick uint64

	for range ticker.C {
		tick++
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

		// Prepare parallel workers
		numWorkers := world.Threads
		if numWorkers <= 0 {
			numWorkers = runtime.NumCPU()
		}

		// Convert map to slice for parallel processing
		bodiesList := make([]*internalBody, 0, len(internalBodies))
		for _, b := range internalBodies {
			bodiesList = append(bodiesList, b)
		}

		dt := float32(1.0 / world.UpdateFrequency)
		gravity := world.Gravity

		// 1. Parallel Integration
		var wg sync.WaitGroup
		chunkSize := (len(bodiesList) + numWorkers - 1) / numWorkers
		for i := 0; i < numWorkers; i++ {
			start := i * chunkSize
			end := start + chunkSize
			if start >= len(bodiesList) {
				break
			}
			if end > len(bodiesList) {
				end = len(bodiesList)
			}

			wg.Add(1)
			go func(bodies []*internalBody) {
				defer wg.Done()
				for _, b := range bodies {
					if b.isStatic || b.sleeping {
						continue
					}

					// Apply Gravity
					if b.gravityScale != 0 {
						b.vel = b.vel.Add(gravity.Mul(b.gravityScale * dt))
					}

					// Damping
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

					// Integrate
					b.pos = b.pos.Add(b.vel.Mul(dt))
					if b.angVel.Len() > 0 {
						angVelQuat := mgl32.Quat{W: 0, V: b.angVel.Mul(0.5 * dt)}
						b.rot = b.rot.Add(angVelQuat.Mul(b.rot))
						b.rot = b.rot.Normalize()
					}
					b.updateAABB()
				}
			}(bodiesList[start:end])
		}
		wg.Wait()

		// Broad-phase: Update spatial grid AFTER integration
		grid := NewSpatialHashGrid(10.0) // 10 units cell size
		for _, b := range bodiesList {
			// updateAABB was already called in integration for dynamic bodies,
			// but static bodies need it called once or preserved.
			if b.isStatic {
				b.updateAABB()
			}
			grid.Insert(b.Eid, AABBComponent{Min: b.aabbMin, Max: b.aabbMax})
		}

		// 2. Parallel Collision Detection (Narrow-phase)
		type collisionManifold struct {
			bodyA, bodyB *internalBody
			normal       mgl32.Vec3
			penetration  float32
			point        mgl32.Vec3
		}
		var manifolds []collisionManifold
		var manifoldMu sync.Mutex

		for i := 0; i < numWorkers; i++ {
			start := i * chunkSize
			end := start + chunkSize
			if start >= len(bodiesList) {
				break
			}
			if end > len(bodiesList) {
				end = len(bodiesList)
			}

			wg.Add(1)
			go func(bodies []*internalBody) {
				defer wg.Done()
				for _, b := range bodies {
					// Only dynamic bodies initiate collision checks
					if b.isStatic || b.sleeping {
						continue
					}

					// Query spatial grid for candidates
					candidates := grid.QueryAABB(AABBComponent{Min: b.aabbMin, Max: b.aabbMax})
					for _, otherEid := range candidates {
						if otherEid == b.Eid {
							continue
						}

						other := internalBodies[otherEid]
						if other == nil {
							continue
						}

						// Ensure we only check each pair once.
						// If both are dynamic, only process if b.Eid < other.Eid.
						// If other is static, always process (since static won't initiate checks).
						if !other.isStatic && b.Eid > other.Eid {
							continue
						}

						// Narrow-phase
						for _, boxA := range b.boxes {
							for _, boxB := range other.boxes {
								if collision, normal, penetration, contactPoint := checkSingleOBBCollision(b.pos, b.rot, boxA.Box, other.pos, other.rot, boxB.Box); collision {
									manifoldMu.Lock()
									manifolds = append(manifolds, collisionManifold{
										bodyA:       b,
										bodyB:       other,
										normal:      normal,
										penetration: penetration,
										point:       contactPoint,
									})
									manifoldMu.Unlock()
								}
							}
						}
					}
				}
			}(bodiesList[start:end])
		}
		wg.Wait()

		// 3. Sequential Resolution
		for _, m := range manifolds {
			b := m.bodyA
			other := m.bodyB

			slop := float32(0.02)
			if m.penetration > slop {
				depth := m.penetration - slop
				invMassA := float32(0)
				invMassB := float32(0)
				if !b.isStatic && b.mass > 0 {
					invMassA = 1.0 / b.mass
				}
				if !other.isStatic && other.mass > 0 {
					invMassB = 1.0 / other.mass
				}

				totalInvMass := invMassA + invMassB
				if totalInvMass > 0 {
					correction := m.normal.Mul(depth / totalInvMass)
					if invMassA > 0 {
						b.pos = b.pos.Add(correction.Mul(invMassA))
					}
					if invMassB > 0 {
						other.pos = other.pos.Sub(correction.Mul(invMassB))
					}
				}
			}

			rA := m.point.Sub(b.pos)
			rB := m.point.Sub(other.pos)

			vA := b.vel.Add(b.angVel.Cross(rA))
			vB := other.vel
			if !other.isStatic {
				vB = other.vel.Add(other.angVel.Cross(rB))
			}

			relativeVel := vA.Sub(vB)
			velAlongNormal := relativeVel.Dot(m.normal)

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
			rAn := rA.Cross(m.normal)
			denom += rAn.Dot(rAn) / inertiaA

			if !other.isStatic && other.mass > 0 {
				inertiaB := calculateInertia(other)
				denom += 1.0 / other.mass
				rBn := rB.Cross(m.normal)
				denom += rBn.Dot(rBn) / inertiaB
			}

			j := -(1 + restitution) * velAlongNormal
			j /= denom

			impulse := m.normal.Mul(j)

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
			tangent := relativeVel.Sub(m.normal.Mul(relativeVel.Dot(m.normal)))
			if tangent.Len() > 0.0001 {
				tangent = tangent.Normalize()
				jt := -relativeVel.Dot(tangent)
				jt /= denom // Approximate denominator for friction
				maxFriction := friction * float32(math.Abs(float64(j)))
				if jt > maxFriction {
					jt = maxFriction
				}
				if jt < -maxFriction {
					jt = -maxFriction
				}

				fImpulse := tangent.Mul(jt)
				b.vel = b.vel.Add(fImpulse.Mul(1.0 / b.mass))
				b.angVel = b.angVel.Add(rA.Cross(fImpulse).Mul(1.0 / inertiaA))

				if !other.isStatic && other.mass > 0 {
					inertiaB := calculateInertia(other)
					other.vel = other.vel.Sub(fImpulse.Mul(1.0 / other.mass))
					other.angVel = other.angVel.Sub(rB.Cross(fImpulse).Mul(1.0 / inertiaB))
				}
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

		// 4. Sleeping and Results
		for _, b := range internalBodies {
			if !b.isStatic && !b.sleeping {
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
			}
		}

		// Push results
		res := &PhysicsResults{Tick: tick, Generated: time.Now()}
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
