package gekko

import (
	"runtime"
	"sync"
	"time"

	"github.com/go-gl/mathgl/mgl32"
)

type collisionManifold struct {
	bodyA, bodyB *internalBody
	normal       mgl32.Vec3
	penetration  float32
	point        mgl32.Vec3
}

func physicsLoop(world *PhysicsWorld, proxy *PhysicsProxy) {
	ticker := time.NewTicker(time.Duration(1000.0/world.UpdateFrequency) * time.Millisecond)
	defer ticker.Stop()

	// Internal state
	internalBodies := make(map[EntityId]*internalBody)
	bodiesByID := make(map[EntityId]*internalBody)
	staticContactBodies := make(map[EntityId]bool)
	grid := NewSpatialHashGrid(10.0)
	manifolds := make([]collisionManifold, 0, 256)
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
				massChanged := body.mass != es.Mass
				modelChanged := !sameCollisionBoxes(body.boxes, es.Model.Boxes)
				body.mass = es.Mass
				body.model = es.Model
				body.friction = es.Friction
				body.restitution = es.Restitution
				body.gravityScale = es.GravityScale
				body.sleeping = es.Sleeping
				body.linearDamping = es.LinearDamping
				body.angularDamping = es.AngularDamping
				if modelChanged {
					if cap(body.boxes) < len(es.Model.Boxes) {
						body.boxes = make([]InternalBox, len(es.Model.Boxes))
					} else {
						body.boxes = body.boxes[:len(es.Model.Boxes)]
					}
					for i, box := range es.Model.Boxes {
						body.boxes[i].Box = box
					}
				}
				if massChanged || modelChanged {
					body.invInertiaLocal = calculateInverseInertiaLocal(body)
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

		// Convert map to per-tick immutable views for parallel processing
		bodiesList := make([]*internalBody, 0, len(internalBodies))
		clear(bodiesByID)
		for eid, b := range internalBodies {
			bodiesList = append(bodiesList, b)
			bodiesByID[eid] = b
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
					b.rot = integrateAngularVelocity(b.rot, b.angVel, dt)
					b.updateAABB()
				}
			}(bodiesList[start:end])
		}
		wg.Wait()

		// Broad-phase: Update spatial grid AFTER integration
		grid.Clear() // 10 units cell size
		for _, b := range bodiesList {
			// updateAABB was already called in integration for dynamic bodies,
			// but static bodies need it called once or preserved.
			if b.isStatic {
				b.updateAABB()
			}
			grid.Insert(b.Eid, AABBComponent{Min: b.aabbMin, Max: b.aabbMax})
		}

		// 2. Parallel Collision Detection (Narrow-phase)
		manifolds = manifolds[:0]
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
				queryUnique := make(map[EntityId]struct{})
				var candidates []EntityId
				localManifolds := make([]collisionManifold, 0, 32)
				for _, b := range bodies {
					// Only dynamic bodies initiate collision checks
					if b.isStatic || b.sleeping {
						continue
					}

					// Query spatial grid for candidates
					candidates = grid.QueryAABBInto(AABBComponent{Min: b.aabbMin, Max: b.aabbMax}, queryUnique, candidates)
					for _, otherEid := range candidates {
						if otherEid == b.Eid {
							continue
						}

						other := bodiesByID[otherEid]
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
									localManifolds = append(localManifolds, collisionManifold{
										bodyA:       b,
										bodyB:       other,
										normal:      normal,
										penetration: penetration,
										point:       contactPoint,
									})
								}
							}
						}
					}
				}
				if len(localManifolds) > 0 {
					manifoldMu.Lock()
					manifolds = append(manifolds, localManifolds...)
					manifoldMu.Unlock()
				}
			}(bodiesList[start:end])
		}
		wg.Wait()

		// 3. Sequential Resolution
		for _, m := range manifolds {
			b := m.bodyA
			other := m.bodyB

			slop := float32(0.02)
			positionCorrectionPercent := float32(0.2)
			if m.penetration > slop {
				depth := (m.penetration - slop) * positionCorrectionPercent
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
		}

		clear(staticContactBodies)
		for _, m := range manifolds {
			if !m.bodyA.isStatic && m.bodyB.isStatic {
				staticContactBodies[m.bodyA.Eid] = true
			}
			if !m.bodyB.isStatic && m.bodyA.isStatic {
				staticContactBodies[m.bodyB.Eid] = true
			}
		}

		const solverIterations = 8
		for iter := 0; iter < solverIterations; iter++ {
			for _, m := range manifolds {
				b := m.bodyA
				other := m.bodyB

				rA := m.point.Sub(b.pos)
				rB := m.point.Sub(other.pos)

				vA := b.vel.Add(b.angVel.Cross(rA))
				vB := other.vel
				if !other.isStatic {
					vB = other.vel.Add(other.angVel.Cross(rB))
				}

				relativeVel := vA.Sub(vB)
				velAlongNormal := relativeVel.Dot(m.normal)

				if velAlongNormal > 0 {
					continue
				}

				restitution := (b.restitution + other.restitution) * 0.5
				if velAlongNormal > -0.5 {
					restitution = 0
				}

				invMassA := inverseMass(b)
				invMassB := inverseMass(other)
				denom := invMassA + invMassB + angularConstraintDenominator(b, rA, m.normal) + angularConstraintDenominator(other, rB, m.normal)
				if denom <= 0 {
					continue
				}

				j := -(1 + restitution) * velAlongNormal
				j /= denom

				impulse := m.normal.Mul(j)

				if invMassA > 0 {
					b.vel = b.vel.Add(impulse.Mul(invMassA))
					b.angVel = b.angVel.Add(applyInverseInertiaWorld(b, rA.Cross(impulse)))
				}
				if invMassB > 0 {
					other.vel = other.vel.Sub(impulse.Mul(invMassB))
					other.angVel = other.angVel.Sub(applyInverseInertiaWorld(other, rB.Cross(impulse)))
				}

				friction := (b.friction + other.friction) * 0.5
				tangent := relativeVel.Sub(m.normal.Mul(relativeVel.Dot(m.normal)))
				if tangent.Len() > 0.0001 {
					tangent = tangent.Normalize()
					tangentDenom := invMassA + invMassB + angularConstraintDenominator(b, rA, tangent) + angularConstraintDenominator(other, rB, tangent)
					if tangentDenom > 0 {
						jt := -relativeVel.Dot(tangent)
						jt /= tangentDenom
						maxFriction := friction * absf(j)
						if jt > maxFriction {
							jt = maxFriction
						}
						if jt < -maxFriction {
							jt = -maxFriction
						}

						fImpulse := tangent.Mul(jt)
						if invMassA > 0 {
							b.vel = b.vel.Add(fImpulse.Mul(invMassA))
							b.angVel = b.angVel.Add(applyInverseInertiaWorld(b, rA.Cross(fImpulse)))
						}
						if invMassB > 0 {
							other.vel = other.vel.Sub(fImpulse.Mul(invMassB))
							other.angVel = other.angVel.Sub(applyInverseInertiaWorld(other, rB.Cross(fImpulse)))
						}
					}
				}

			}
		}

		// 4. Sleeping and Results
		groundedSleepThreshold := maxf(world.SleepThreshold, gravity.Len()*dt*2.0)
		groundedAngularThreshold := maxf(world.SleepThreshold, 0.1)
		groundedSleepTime := minf(world.SleepTime, 0.25)
		for _, b := range internalBodies {
			if !b.isStatic && !b.sleeping {
				sleepThreshold := world.SleepThreshold
				angularThreshold := world.SleepThreshold
				sleepTime := world.SleepTime
				if staticContactBodies[b.Eid] {
					sleepThreshold = groundedSleepThreshold
					angularThreshold = groundedAngularThreshold
					sleepTime = groundedSleepTime
				}
				if b.vel.Len() < sleepThreshold && b.angVel.Len() < angularThreshold {
					b.idleTime += dt
					if b.idleTime > sleepTime {
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
	Eid             EntityId
	pos             mgl32.Vec3
	rot             mgl32.Quat
	vel             mgl32.Vec3
	angVel          mgl32.Vec3
	isStatic        bool
	mass            float32
	model           PhysicsModel
	boxes           []InternalBox
	sleeping        bool
	idleTime        float32
	friction        float32
	restitution     float32
	gravityScale    float32
	linearDamping   float32
	angularDamping  float32
	invInertiaLocal mgl32.Mat3
	aabbMin         mgl32.Vec3
	aabbMax         mgl32.Vec3
}

func integrateAngularVelocity(rot mgl32.Quat, angVel mgl32.Vec3, dt float32) mgl32.Quat {
	omegaMag := angVel.Len()
	if omegaMag <= 1e-6 || dt <= 0 {
		return rot
	}

	angle := omegaMag * dt
	axis := angVel.Mul(1.0 / omegaMag)
	delta := mgl32.QuatRotate(angle, axis)
	return delta.Mul(rot).Normalize()
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
				extents[i] += absf(axes[j][i]) * box.Box.HalfExtents[j]
			}
		}

		box.Min = worldBoxPos.Sub(extents)
		box.Max = worldBoxPos.Add(extents)

		for i := 0; i < 3; i++ {
			minP[i] = minf(minP[i], box.Min[i])
			maxP[i] = maxf(maxP[i], box.Max[i])
		}
	}

	b.aabbMin = minP
	b.aabbMax = maxP
}

func sameCollisionBoxes(boxes []InternalBox, modelBoxes []CollisionBox) bool {
	if len(boxes) != len(modelBoxes) {
		return false
	}
	for i, modelBox := range modelBoxes {
		if boxes[i].Box != modelBox {
			return false
		}
	}
	return true
}

func inverseMass(b *internalBody) float32 {
	if b == nil || b.isStatic || b.mass <= 0 {
		return 0
	}
	return 1.0 / b.mass
}

func angularConstraintDenominator(b *internalBody, r mgl32.Vec3, axis mgl32.Vec3) float32 {
	if b == nil || b.isStatic || b.mass <= 0 {
		return 0
	}
	rCrossAxis := r.Cross(axis)
	invInertiaTerm := applyInverseInertiaWorld(b, rCrossAxis)
	return axis.Dot(invInertiaTerm.Cross(r))
}

func applyInverseInertiaWorld(b *internalBody, angularImpulse mgl32.Vec3) mgl32.Vec3 {
	if b == nil || b.isStatic || b.mass <= 0 {
		return mgl32.Vec3{}
	}
	rotMat := rotationMat3(b.rot)
	invInertiaWorld := rotMat.Mul3(b.invInertiaLocal).Mul3(rotMat.Transpose())
	return invInertiaWorld.Mul3x1(angularImpulse)
}

func calculateInverseInertiaLocal(b *internalBody) mgl32.Mat3 {
	localInertia := calculateLocalInertiaTensor(b)
	if absf(localInertia.Det()) <= 1e-6 {
		return mgl32.Ident3()
	}
	return localInertia.Inv()
}

func calculateLocalInertiaTensor(b *internalBody) mgl32.Mat3 {
	if len(b.boxes) == 0 || b.mass <= 0 {
		return mgl32.Ident3()
	}

	totalVolume := float32(0)
	for _, box := range b.boxes {
		totalVolume += box.Box.HalfExtents.X() * box.Box.HalfExtents.Y() * box.Box.HalfExtents.Z() * 8.0
	}
	if totalVolume <= 0 {
		return mgl32.Ident3()
	}

	totalInertia := mgl32.Mat3{}
	for _, box := range b.boxes {
		half := box.Box.HalfExtents
		volume := half.X() * half.Y() * half.Z() * 8.0
		boxMass := (volume / totalVolume) * b.mass

		ix := (1.0 / 3.0) * boxMass * (half.Y()*half.Y() + half.Z()*half.Z())
		iy := (1.0 / 3.0) * boxMass * (half.X()*half.X() + half.Z()*half.Z())
		iz := (1.0 / 3.0) * boxMass * (half.X()*half.X() + half.Y()*half.Y())
		boxTensor := mgl32.Mat3FromRows(
			mgl32.Vec3{ix, 0, 0},
			mgl32.Vec3{0, iy, 0},
			mgl32.Vec3{0, 0, iz},
		)

		offset := box.Box.LocalOffset
		offsetSq := offset.LenSqr()
		outer := mgl32.Mat3FromRows(
			mgl32.Vec3{offset.X() * offset.X(), offset.X() * offset.Y(), offset.X() * offset.Z()},
			mgl32.Vec3{offset.Y() * offset.X(), offset.Y() * offset.Y(), offset.Y() * offset.Z()},
			mgl32.Vec3{offset.Z() * offset.X(), offset.Z() * offset.Y(), offset.Z() * offset.Z()},
		)
		parallelAxis := mgl32.Ident3().Mul(offsetSq).Sub(outer).Mul(boxMass)

		totalInertia = totalInertia.Add(boxTensor).Add(parallelAxis)
	}

	return totalInertia
}

func rotationMat3(q mgl32.Quat) mgl32.Mat3 {
	rot := q.Mat4()
	return mgl32.Mat3FromCols(rot.Col(0).Vec3(), rot.Col(1).Vec3(), rot.Col(2).Vec3())
}
