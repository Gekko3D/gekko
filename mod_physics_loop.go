package gekko

import (
	"math"
	"reflect"
	"runtime"
	"sync"
	"time"

	rootphysics "github.com/gekko3d/gekko/physics"
	"github.com/go-gl/mathgl/mgl32"
)

type collisionManifold struct {
	bodyA, bodyB              *internalBody
	normal                    mgl32.Vec3
	penetration               float32
	point                     mgl32.Vec3
	normalImpulse             float32
	relativeSpeed             float32
	accumulatedNormalImpulse  float32
	accumulatedTangentImpulse mgl32.Vec3
}

type collisionPair struct {
	A EntityId
	B EntityId
}

type cachedContactImpulse struct {
	point         mgl32.Vec3
	localPointA   mgl32.Vec3
	localPointB   mgl32.Vec3
	normal        mgl32.Vec3
	normalImpulse float32
}

func physicsLoop(world *PhysicsWorld, proxy *PhysicsProxy) {
	ticker := time.NewTicker(time.Duration(1000.0/world.UpdateFrequency) * time.Millisecond)
	defer ticker.Stop()

	// Internal state
	internalBodies := make(map[EntityId]*internalBody)
	bodiesByID := make(map[EntityId]*internalBody)
	staticContactBodies := make(map[EntityId]bool)
	grid := NewSpatialHashGrid(world.SpatialGridCellSize)
	manifolds := make([]collisionManifold, 0, 256)
	previousPairs := make(map[collisionPair]PhysicsCollisionEvent)
	currentPairs := make(map[collisionPair]PhysicsCollisionEvent)
	previousContactImpulses := make(map[collisionPair][]cachedContactImpulse)
	currentContactImpulses := make(map[collisionPair][]cachedContactImpulse)
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
				syncInternalBody(body, es, !ok)
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

					// Damping (scaled to preserve current 60 Hz tuning across physics rates)
					lDamp := dampingRetentionFactor(b.linearDamping, 0.999)
					aDamp := dampingRetentionFactor(b.angularDamping, 0.99)
					const dampingReferenceHz = float32(60.0)
					b.vel = b.vel.Mul(powf(lDamp, dt*dampingReferenceHz))
					b.angVel = b.angVel.Mul(powf(aDamp, dt*dampingReferenceHz))

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
		triggerEvents := make([]PhysicsCollisionEvent, 0, 64)
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
				localTriggerEvents := make([]PhysicsCollisionEvent, 0, 16)
				for _, b := range bodies {
					// Only dynamic bodies initiate collision checks
					if b.isStatic || b.sleeping {
						continue
					}

					// Query spatial grid for candidates
					candidates = grid.QueryAABBInto(AABBComponent{Min: b.aabbMin, Max: b.aabbMax}, queryUnique, candidates)
					localContacts := make([]narrowPhaseContact, 0, 16)
					for _, otherEid := range candidates {
						if otherEid == b.Eid {
							continue
						}

						other := bodiesByID[otherEid]
						if other == nil {
							continue
						}

						// Ensure we only check each pair once.
						// If both are dynamic and awake, only initiate once.
						// If other is static or sleeping, we MUST check it now because it won't run its own initiation.
						if !other.isStatic && !other.sleeping && b.Eid > other.Eid {
							continue
						}
						if !shouldBodiesCollide(b, other) {
							continue
						}

						localContacts = collectNarrowPhaseContacts(b, other, world.PointInOBBEpsilon, localContacts[:0])
						for _, contact := range localContacts {
							if isTriggerPair(b, other) {
								pair := orderedCollisionPair(b.Eid, other.Eid)
								localTriggerEvents = append(localTriggerEvents, PhysicsCollisionEvent{
									IsTrigger:     true,
									A:             pair.A,
									B:             pair.B,
									Point:         contact.point,
									Normal:        contact.normal,
									Penetration:   contact.penetration,
									RelativeSpeed: b.vel.Sub(other.vel).Len(),
									Tick:          tick,
								})
								continue
							}
							localManifolds = append(localManifolds, collisionManifold{
								bodyA:       b,
								bodyB:       other,
								normal:      contact.normal,
								penetration: contact.penetration,
								point:       contact.point,
							})
						}
					}
				}
				if len(localManifolds) > 0 || len(localTriggerEvents) > 0 {
					manifoldMu.Lock()
					if len(localManifolds) > 0 {
						manifolds = append(manifolds, localManifolds...)
					}
					if len(localTriggerEvents) > 0 {
						triggerEvents = append(triggerEvents, localTriggerEvents...)
					}
					manifoldMu.Unlock()
				}
			}(bodiesList[start:end])
		}
		wg.Wait()

		cachePointThreshold := maxf(world.CollisionSlop*2.0, 0.06)
		const cacheNormalThreshold = float32(0.9)
		seedManifoldImpulses(manifolds, previousContactImpulses, cachePointThreshold, cacheNormalThreshold)

		// 3. Sequential Resolution
		for _, m := range manifolds {
			b := m.bodyA
			other := m.bodyB

			slop := world.CollisionSlop
			positionCorrectionPercent := world.PositionCorrection
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

		for i := range manifolds {
			warmStartManifold(&manifolds[i])
		}

		for iter := 0; iter < world.SolverIterations; iter++ {
			for i := range manifolds {
				m := &manifolds[i]
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
				impactSpeed := absf(velAlongNormal)
				if impactSpeed > m.relativeSpeed {
					m.relativeSpeed = impactSpeed
				}

				if velAlongNormal > 0 && m.accumulatedNormalImpulse <= 0 {
					continue
				}

				restitution := (b.restitution + other.restitution) * 0.5
				if velAlongNormal > world.RestitutionThreshold || m.accumulatedNormalImpulse > 0 {
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
				oldNormalImpulse := m.accumulatedNormalImpulse
				m.accumulatedNormalImpulse = maxf(oldNormalImpulse+j, 0)
				j = m.accumulatedNormalImpulse - oldNormalImpulse
				impulse := m.normal.Mul(j)
				if m.accumulatedNormalImpulse > m.normalImpulse {
					m.normalImpulse = m.accumulatedNormalImpulse
				}

				applyWorldImpulse(b, rA, impulse, 1)
				applyWorldImpulse(other, rB, impulse, -1)

				impactWakeThreshold := world.WakeThreshold
				highImpact := absf(velAlongNormal) > impactWakeThreshold
				deepPenetration := m.penetration > world.CollisionSlop
				wakeBodyForContact(b, highImpact, deepPenetration)
				wakeBodyForContact(other, highImpact, deepPenetration)

				friction := (b.friction + other.friction) * 0.5
				vA = b.vel.Add(b.angVel.Cross(rA))
				vB = other.vel
				if !other.isStatic {
					vB = other.vel.Add(other.angVel.Cross(rB))
				}
				relativeVel = vA.Sub(vB)
				tangent := relativeVel.Sub(m.normal.Mul(relativeVel.Dot(m.normal)))
				if tangent.Len() > 0.0001 {
					tangent = tangent.Normalize()
					tangentDenom := invMassA + invMassB + angularConstraintDenominator(b, rA, tangent) + angularConstraintDenominator(other, rB, tangent)
					if tangentDenom > 0 {
						jt := -relativeVel.Dot(tangent)
						jt /= tangentDenom
						oldTangentImpulse := m.accumulatedTangentImpulse
						candidateTangentImpulse := oldTangentImpulse.Add(tangent.Mul(jt))
						maxFriction := friction * m.accumulatedNormalImpulse
						if candidateLen := candidateTangentImpulse.Len(); candidateLen > maxFriction && candidateLen > 1e-6 {
							candidateTangentImpulse = candidateTangentImpulse.Mul(maxFriction / candidateLen)
						}

						fImpulse := candidateTangentImpulse.Sub(oldTangentImpulse)
						m.accumulatedTangentImpulse = candidateTangentImpulse
						applyWorldImpulse(b, rA, fImpulse, 1)
						applyWorldImpulse(other, rB, fImpulse, -1)
					}
				}

			}
		}

		clearCollisionImpulseMap(currentContactImpulses)
		storeCachedManifoldImpulses(currentContactImpulses, manifolds, cachePointThreshold, cacheNormalThreshold)

		for pair := range currentPairs {
			delete(currentPairs, pair)
		}
		for _, manifold := range manifolds {
			pair := orderedCollisionPair(manifold.bodyA.Eid, manifold.bodyB.Eid)
			event := PhysicsCollisionEvent{
				A:             pair.A,
				B:             pair.B,
				Point:         manifold.point,
				Normal:        manifold.normal,
				Penetration:   manifold.penetration,
				NormalImpulse: manifold.normalImpulse,
				RelativeSpeed: manifold.relativeSpeed,
				Tick:          tick,
			}

			if existing, ok := currentPairs[pair]; ok {
				currentPairs[pair] = mergeCollisionEvent(existing, event)
			} else {
				currentPairs[pair] = event
			}
		}
		for _, event := range triggerEvents {
			pair := orderedCollisionPair(event.A, event.B)
			if existing, ok := currentPairs[pair]; ok {
				currentPairs[pair] = mergeCollisionEvent(existing, event)
			} else {
				currentPairs[pair] = event
			}
		}

		// 4. Sleeping and Results
		groundedSleepThreshold := maxf(world.SleepThreshold, gravity.Len()*dt*2.0)
		groundedAngularThreshold := maxf(world.SleepThreshold, world.GroundedAngularThreshold)
		groundedSleepTime := minf(world.SleepTime, world.GroundedSleepTime)
		for _, b := range internalBodies {
			if !b.isStatic && !b.sleeping {
				if b.vel.Len() < world.VelocityZeroThreshold {
					b.vel = mgl32.Vec3{}
				}
				if b.angVel.Len() < world.VelocityZeroThreshold {
					b.angVel = mgl32.Vec3{}
				}

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
		for pair, event := range currentPairs {
			if _, ok := previousPairs[pair]; ok {
				event.Type = CollisionEventStay
			} else {
				event.Type = CollisionEventEnter
			}
			res.Collisions = append(res.Collisions, event)
		}
		for pair, previous := range previousPairs {
			if _, ok := currentPairs[pair]; ok {
				continue
			}
			previous.Type = CollisionEventExit
			previous.Penetration = 0
			previous.NormalImpulse = 0
			previous.RelativeSpeed = 0
			previous.Tick = tick
			res.Collisions = append(res.Collisions, previous)
		}
		previousPairs, currentPairs = currentPairs, previousPairs
		previousContactImpulses, currentContactImpulses = currentContactImpulses, previousContactImpulses
		proxy.latestResults.Store(res)
	}
}

func syncInternalBody(body *internalBody, es PhysicsEntityState, isNew bool) {
	if es.Teleport || isNew {
		body.pos = es.Pos
		body.rot = es.Rot
	}

	body.vel = es.Vel
	body.angVel = es.AngVel
	body.isStatic = es.IsStatic

	massChanged := body.mass != es.Mass
	modelChanged := physicsModelChanged(body, es.Model)
	primitiveChanged := body.shape != es.Shape || body.radius != es.Radius || body.capsuleHalfHeight != es.CapsuleHalfHeight

	body.mass = es.Mass
	body.model = es.Model
	body.shape = es.Shape
	body.radius = es.Radius
	body.capsuleHalfHeight = es.CapsuleHalfHeight
	body.friction = es.Friction
	body.restitution = es.Restitution
	body.collisionLayer = es.CollisionLayer
	body.collisionMask = es.CollisionMask
	body.isTrigger = es.IsTrigger
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

	if massChanged || modelChanged || primitiveChanged {
		body.invInertiaLocal = CalculateInverseInertiaLocalForCollider(body.mass, body.shape, body.radius, body.capsuleHalfHeight, &body.model)
	}
}

func orderedCollisionPair(a, b EntityId) collisionPair {
	if a <= b {
		return collisionPair{A: a, B: b}
	}
	return collisionPair{A: b, B: a}
}

func effectiveCollisionLayer(layer uint32) uint32 {
	return rootphysics.EffectiveCollisionLayer(layer)
}

func effectiveCollisionMask(mask uint32) uint32 {
	return rootphysics.EffectiveCollisionMask(mask)
}

func shouldBodiesCollide(a, b *internalBody) bool {
	if a == nil || b == nil {
		return false
	}
	layerA := effectiveCollisionLayer(a.collisionLayer)
	layerB := effectiveCollisionLayer(b.collisionLayer)
	maskA := effectiveCollisionMask(a.collisionMask)
	maskB := effectiveCollisionMask(b.collisionMask)
	return maskA&layerB != 0 && maskB&layerA != 0
}

func isTriggerPair(a, b *internalBody) bool {
	return a != nil && a.isTrigger || b != nil && b.isTrigger
}

func seedManifoldImpulses(manifolds []collisionManifold, cache map[collisionPair][]cachedContactImpulse, pointThreshold float32, normalThreshold float32) {
	if len(manifolds) == 0 || len(cache) == 0 {
		return
	}
	if pointThreshold <= 0 {
		pointThreshold = 0.05
	}
	pointThresholdSq := pointThreshold * pointThreshold
	if normalThreshold <= 0 {
		normalThreshold = 0.75
	}

	manifoldIndexesByPair := make(map[collisionPair][]int, len(manifolds))
	for i := range manifolds {
		pair := orderedCollisionPair(manifolds[i].bodyA.Eid, manifolds[i].bodyB.Eid)
		manifoldIndexesByPair[pair] = append(manifoldIndexesByPair[pair], i)
	}

	for pair, manifoldIndexes := range manifoldIndexesByPair {
		cachedContacts := cache[pair]
		if len(cachedContacts) == 0 {
			continue
		}

		usedManifolds := make([]bool, len(manifoldIndexes))
		usedCachedContacts := make([]bool, len(cachedContacts))
		for {
			bestManifold := -1
			bestCachedContact := -1
			bestDistanceSq := float32(0)
			bestImpulse := float32(0)

			for localManifoldIndex, manifoldIndex := range manifoldIndexes {
				if usedManifolds[localManifoldIndex] {
					continue
				}

				manifold := &manifolds[manifoldIndex]
				localPointA := worldPointToLocal(manifold.bodyA, manifold.point)
				localPointB := worldPointToLocal(manifold.bodyB, manifold.point)
				for cachedIndex, cached := range cachedContacts {
					if usedCachedContacts[cachedIndex] || cached.normalImpulse <= 0 {
						continue
					}
					if manifold.normal.Dot(cached.normal) < normalThreshold {
						continue
					}

					distanceASq := localPointA.Sub(cached.localPointA).LenSqr()
					if distanceASq > pointThresholdSq {
						continue
					}
					distanceBSq := localPointB.Sub(cached.localPointB).LenSqr()
					if distanceBSq > pointThresholdSq {
						continue
					}
					distanceSq := distanceASq + distanceBSq

					if bestManifold == -1 || distanceSq < bestDistanceSq || (distanceSq == bestDistanceSq && cached.normalImpulse > bestImpulse) {
						bestManifold = localManifoldIndex
						bestCachedContact = cachedIndex
						bestDistanceSq = distanceSq
						bestImpulse = cached.normalImpulse
					}
				}
			}

			if bestManifold == -1 || bestCachedContact == -1 {
				break
			}

			manifold := &manifolds[manifoldIndexes[bestManifold]]
			cached := cachedContacts[bestCachedContact]
			manifold.accumulatedNormalImpulse = cached.normalImpulse
			manifold.normalImpulse = maxf(manifold.normalImpulse, cached.normalImpulse)
			usedManifolds[bestManifold] = true
			usedCachedContacts[bestCachedContact] = true
		}
	}
}

func warmStartManifold(manifold *collisionManifold) {
	if manifold == nil {
		return
	}
	if manifold.accumulatedNormalImpulse <= 0 {
		return
	}

	rA := manifold.point.Sub(manifold.bodyA.pos)
	rB := manifold.point.Sub(manifold.bodyB.pos)
	impulse := manifold.normal.Mul(manifold.accumulatedNormalImpulse)
	applyWorldImpulse(manifold.bodyA, rA, impulse, 1)
	applyWorldImpulse(manifold.bodyB, rB, impulse, -1)
}

func applyWorldImpulse(body *internalBody, r, impulse mgl32.Vec3, direction float32) {
	invMass := inverseMass(body)
	if invMass <= 0 {
		return
	}

	signedImpulse := impulse.Mul(direction)
	body.vel = body.vel.Add(signedImpulse.Mul(invMass))
	body.angVel = body.angVel.Add(ApplyInverseInertiaWorld(body.rot, body.invInertiaLocal, r.Cross(signedImpulse)))
}

func clearCollisionImpulseMap(cache map[collisionPair][]cachedContactImpulse) {
	for key := range cache {
		delete(cache, key)
	}
}

func storeCachedManifoldImpulses(cache map[collisionPair][]cachedContactImpulse, manifolds []collisionManifold, pointThreshold float32, normalThreshold float32) {
	if len(manifolds) == 0 {
		return
	}

	manifoldIndexesByPair := make(map[collisionPair][]int, len(manifolds))
	for i := range manifolds {
		if manifolds[i].accumulatedNormalImpulse <= 0 {
			continue
		}
		pair := orderedCollisionPair(manifolds[i].bodyA.Eid, manifolds[i].bodyB.Eid)
		manifoldIndexesByPair[pair] = append(manifoldIndexesByPair[pair], i)
	}

	const dominantNormalThreshold = float32(0.9)
	const maxCachedContactsPerPair = 4
	for _, manifoldIndexes := range manifoldIndexesByPair {
		dominantNormal := manifolds[manifoldIndexes[0]].normal
		dominantImpulse := manifolds[manifoldIndexes[0]].accumulatedNormalImpulse
		for _, manifoldIndex := range manifoldIndexes[1:] {
			if manifolds[manifoldIndex].accumulatedNormalImpulse > dominantImpulse {
				dominantImpulse = manifolds[manifoldIndex].accumulatedNormalImpulse
				dominantNormal = manifolds[manifoldIndex].normal
			}
		}

		used := make([]bool, len(manifoldIndexes))
		cachedCount := 0
		for cachedCount < maxCachedContactsPerPair {
			bestLocalIndex := -1
			bestImpulse := float32(0)
			bestPenetration := float32(0)

			for localIndex, manifoldIndex := range manifoldIndexes {
				if used[localIndex] {
					continue
				}

				manifold := &manifolds[manifoldIndex]
				if manifold.normal.Dot(dominantNormal) < dominantNormalThreshold {
					continue
				}

				if bestLocalIndex == -1 ||
					manifold.accumulatedNormalImpulse > bestImpulse ||
					(manifold.accumulatedNormalImpulse == bestImpulse && manifold.penetration > bestPenetration) {
					bestLocalIndex = localIndex
					bestImpulse = manifold.accumulatedNormalImpulse
					bestPenetration = manifold.penetration
				}
			}

			if bestLocalIndex == -1 {
				break
			}

			used[bestLocalIndex] = true
			storeCachedManifoldImpulse(cache, &manifolds[manifoldIndexes[bestLocalIndex]], pointThreshold, normalThreshold)
			cachedCount++
		}
	}
}

func storeCachedManifoldImpulse(cache map[collisionPair][]cachedContactImpulse, manifold *collisionManifold, pointThreshold float32, normalThreshold float32) {
	if manifold == nil || manifold.accumulatedNormalImpulse <= 0 {
		return
	}
	if pointThreshold <= 0 {
		pointThreshold = 0.05
	}
	pointThresholdSq := pointThreshold * pointThreshold
	if normalThreshold <= 0 {
		normalThreshold = 0.75
	}

	pair := orderedCollisionPair(manifold.bodyA.Eid, manifold.bodyB.Eid)
	candidate := cachedContactImpulse{
		point:         manifold.point,
		localPointA:   worldPointToLocal(manifold.bodyA, manifold.point),
		localPointB:   worldPointToLocal(manifold.bodyB, manifold.point),
		normal:        manifold.normal,
		normalImpulse: manifold.accumulatedNormalImpulse,
	}

	for i, existing := range cache[pair] {
		if manifold.normal.Dot(existing.normal) < normalThreshold {
			continue
		}
		if candidate.localPointA.Sub(existing.localPointA).LenSqr() > pointThresholdSq {
			continue
		}
		if candidate.localPointB.Sub(existing.localPointB).LenSqr() > pointThresholdSq {
			continue
		}

		merged := candidate
		if existing.normalImpulse > merged.normalImpulse {
			merged.normalImpulse = existing.normalImpulse
		}
		cache[pair][i] = merged
		return
	}

	cache[pair] = append(cache[pair], candidate)
}

func worldPointToLocal(body *internalBody, point mgl32.Vec3) mgl32.Vec3 {
	if body == nil {
		return point
	}
	return body.rot.Conjugate().Rotate(point.Sub(body.pos))
}

func mergeCollisionEvent(current, candidate PhysicsCollisionEvent) PhysicsCollisionEvent {
	if candidate.IsTrigger {
		current.IsTrigger = true
	}
	if candidate.Penetration > current.Penetration {
		current.Point = candidate.Point
		current.Normal = candidate.Normal
		current.Penetration = candidate.Penetration
	}
	if candidate.NormalImpulse > current.NormalImpulse {
		current.NormalImpulse = candidate.NormalImpulse
	}
	if candidate.RelativeSpeed > current.RelativeSpeed {
		current.RelativeSpeed = candidate.RelativeSpeed
	}
	if candidate.Tick > current.Tick {
		current.Tick = candidate.Tick
	}
	return current
}

func wakeBodyForContact(body *internalBody, highImpact bool, deepPenetration bool) {
	if body == nil || body.isStatic {
		return
	}

	// Resting contacts can sit slightly above slop for several ticks while the
	// position solver settles. Only reset awake bodies on real impacts; otherwise
	// they never accumulate enough idle time to go to sleep and appear to jitter.
	if highImpact || (body.sleeping && deepPenetration) {
		body.Wake()
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
	Eid               EntityId
	pos               mgl32.Vec3
	rot               mgl32.Quat
	vel               mgl32.Vec3
	angVel            mgl32.Vec3
	isStatic          bool
	mass              float32
	model             PhysicsModel
	shape             ColliderShape
	radius            float32
	capsuleHalfHeight float32
	boxes             []InternalBox
	sleeping          bool
	idleTime          float32
	friction          float32
	restitution       float32
	collisionLayer    uint32
	collisionMask     uint32
	isTrigger         bool
	gravityScale      float32
	linearDamping     float32
	angularDamping    float32
	invInertiaLocal   mgl32.Mat3
	aabbMin           mgl32.Vec3
	aabbMax           mgl32.Vec3
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
	if b.shape == ShapeSphere && b.radius > 0 {
		extents := mgl32.Vec3{b.radius, b.radius, b.radius}
		b.aabbMin = b.pos.Sub(extents)
		b.aabbMax = b.pos.Add(extents)
		return
	}

	if b.shape == ShapeCapsule && b.radius > 0 && b.capsuleHalfHeight >= 0 {
		endA, endB := capsuleSegmentEndpoints(b.pos, b.rot, b.capsuleHalfHeight)
		extents := mgl32.Vec3{b.radius, b.radius, b.radius}
		b.aabbMin = vec3Min(endA, endB).Sub(extents)
		b.aabbMax = vec3Max(endA, endB).Add(extents)
		return
	}

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

func sameVoxelGrid(a, b rootphysics.VoxelGrid) bool {
	if a == nil || b == nil {
		return a == b
	}

	ta := reflect.TypeOf(a)
	tb := reflect.TypeOf(b)
	if ta != tb || !ta.Comparable() {
		return false
	}

	return reflect.ValueOf(a).Interface() == reflect.ValueOf(b).Interface()
}

func physicsModelChanged(body *internalBody, model PhysicsModel) bool {
	if body == nil {
		return true
	}

	return body.model.CenterOffset != model.CenterOffset ||
		!sameCollisionBoxes(body.boxes, model.Boxes) ||
		!sameVoxelGrid(body.model.Grid, model.Grid)
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
	invInertiaTerm := ApplyInverseInertiaWorld(b.rot, b.invInertiaLocal, rCrossAxis)
	return axis.Dot(invInertiaTerm.Cross(r))
}

func ApplyInverseInertiaWorld(rot mgl32.Quat, invInertiaLocal mgl32.Mat3, angularImpulse mgl32.Vec3) mgl32.Vec3 {
	rotMat := RotationMat3(rot)
	invInertiaWorld := rotMat.Mul3(invInertiaLocal).Mul3(rotMat.Transpose())
	return invInertiaWorld.Mul3x1(angularImpulse)
}

func CalculateInverseInertiaLocal(mass float32, model *PhysicsModel) mgl32.Mat3 {
	localInertia := calculateLocalInertiaTensorFromModel(mass, model)
	return inverseInertiaFromLocalTensor(localInertia)
}

func CalculateInverseInertiaLocalForCollider(mass float32, shape ColliderShape, radius, capsuleHalfHeight float32, model *PhysicsModel) mgl32.Mat3 {
	localInertia := calculateLocalInertiaTensorForCollider(mass, shape, radius, capsuleHalfHeight, model)
	return inverseInertiaFromLocalTensor(localInertia)
}

func inverseInertiaFromLocalTensor(localInertia mgl32.Mat3) mgl32.Mat3 {
	if absf(localInertia.Det()) > 1e-6 {
		return localInertia.Inv()
	}
	if inverse, ok := invertSymmetricInertiaTensor(localInertia); ok {
		return inverse
	}
	if absf(localInertia.Det()) <= 1e-6 {
		return mgl32.Ident3()
	}
	return localInertia.Inv()
}

// calculateLocalInertiaTensor is a compatibility wrapper retained for tests and
// older internal call sites. Runtime code should prefer calculateLocalInertiaTensorFromModel.
func calculateLocalInertiaTensor(body *internalBody) mgl32.Mat3 {
	if body == nil {
		return mgl32.Ident3()
	}
	model := physicsModelForInertia(body)
	return calculateLocalInertiaTensorForCollider(body.mass, body.shape, body.radius, body.capsuleHalfHeight, &model)
}

// calculateInverseInertiaLocal is a compatibility wrapper retained for tests and
// older internal call sites. Runtime code should prefer CalculateInverseInertiaLocal.
func calculateInverseInertiaLocal(body *internalBody) mgl32.Mat3 {
	if body == nil {
		return mgl32.Ident3()
	}
	model := physicsModelForInertia(body)
	return CalculateInverseInertiaLocalForCollider(body.mass, body.shape, body.radius, body.capsuleHalfHeight, &model)
}

func physicsModelForInertia(body *internalBody) PhysicsModel {
	if body == nil {
		return PhysicsModel{}
	}
	if body.model.Grid != nil || len(body.model.Boxes) > 0 {
		return body.model
	}
	if len(body.boxes) == 0 {
		return body.model
	}
	model := body.model
	model.Boxes = make([]CollisionBox, len(body.boxes))
	for i := range body.boxes {
		model.Boxes[i] = body.boxes[i].Box
	}
	return model
}

func calculateLocalInertiaTensorFromModel(mass float32, model *PhysicsModel) mgl32.Mat3 {
	return calculateLocalInertiaTensorForCollider(mass, ShapeBox, 0, 0, model)
}

func calculateLocalInertiaTensorForCollider(mass float32, shape ColliderShape, radius, capsuleHalfHeight float32, model *PhysicsModel) mgl32.Mat3 {
	if mass <= 0 {
		return mgl32.Ident3()
	}

	if shape == ShapeSphere && radius > 0 {
		moment := (2.0 / 5.0) * mass * radius * radius
		return mgl32.Mat3FromRows(
			mgl32.Vec3{moment, 0, 0},
			mgl32.Vec3{0, moment, 0},
			mgl32.Vec3{0, 0, moment},
		)
	}

	if shape == ShapeCapsule && radius > 0 && capsuleHalfHeight >= 0 {
		return capsuleLocalInertiaTensor(mass, radius, capsuleHalfHeight)
	}

	if model == nil {
		return mgl32.Ident3()
	}

	if model.Grid != nil {
		grid := model.Grid
		voxelScale := grid.VoxelScale()
		voxelHalfExtents := voxelScale.Mul(0.5)
		minV := grid.GetAABBMin()
		maxV := grid.GetAABBMax()

		// First pass: count voxels
		var count int
		for vz := int(minV.Z()); vz < int(maxV.Z()); vz++ {
			for vy := int(minV.Y()); vy < int(maxV.Y()); vy++ {
				for vx := int(minV.X()); vx < int(maxV.X()); vx++ {
					if found, _ := grid.GetVoxel(vx, vy, vz); found {
						count++
					}
				}
			}
		}

		if count == 0 {
			return mgl32.Ident3()
		}

		m := mass / float32(count)
		com := model.CenterOffset
		voxelIxx := (1.0 / 3.0) * m * (voxelHalfExtents.Y()*voxelHalfExtents.Y() + voxelHalfExtents.Z()*voxelHalfExtents.Z())
		voxelIyy := (1.0 / 3.0) * m * (voxelHalfExtents.X()*voxelHalfExtents.X() + voxelHalfExtents.Z()*voxelHalfExtents.Z())
		voxelIzz := (1.0 / 3.0) * m * (voxelHalfExtents.X()*voxelHalfExtents.X() + voxelHalfExtents.Y()*voxelHalfExtents.Y())

		// Single pass: compute inertia tensor directly without storing positions
		var ixx, iyy, izz, ixy, ixz, iyz float32
		for vz := int(minV.Z()); vz < int(maxV.Z()); vz++ {
			for vy := int(minV.Y()); vy < int(maxV.Y()); vy++ {
				for vx := int(minV.X()); vx < int(maxV.X()); vx++ {
					if found, _ := grid.GetVoxel(vx, vy, vz); found {
						pos := vec3MulComponents(mgl32.Vec3{float32(vx) + 0.5, float32(vy) + 0.5, float32(vz) + 0.5}, voxelScale)
						d := pos.Sub(com)
						ixx += voxelIxx + m*(d.Y()*d.Y()+d.Z()*d.Z())
						iyy += voxelIyy + m*(d.X()*d.X()+d.Z()*d.Z())
						izz += voxelIzz + m*(d.X()*d.X()+d.Y()*d.Y())
						ixy -= m * d.X() * d.Y()
						ixz -= m * d.X() * d.Z()
						iyz -= m * d.Y() * d.Z()
					}
				}
			}
		}

		res := mgl32.Mat3FromRows(
			mgl32.Vec3{ixx, ixy, ixz},
			mgl32.Vec3{ixy, iyy, iyz},
			mgl32.Vec3{ixz, iyz, izz},
		)

		return res
	}

	if len(model.Boxes) == 0 {
		return mgl32.Ident3()
	}

	totalVolume := float32(0)
	for _, box := range model.Boxes {
		totalVolume += effectiveCollisionBoxVolume(box.HalfExtents)
	}
	if totalVolume <= 0 {
		return mgl32.Ident3()
	}

	totalInertia := mgl32.Mat3{}
	for _, box := range model.Boxes {
		half := box.HalfExtents
		volume := effectiveCollisionBoxVolume(half)
		boxMass := (volume / totalVolume) * mass

		ix := (1.0 / 3.0) * boxMass * (half.Y()*half.Y() + half.Z()*half.Z())
		iy := (1.0 / 3.0) * boxMass * (half.X()*half.X() + half.Z()*half.Z())
		iz := (1.0 / 3.0) * boxMass * (half.X()*half.X() + half.Y()*half.Y())
		boxTensor := mgl32.Mat3FromRows(
			mgl32.Vec3{ix, 0, 0},
			mgl32.Vec3{0, iy, 0},
			mgl32.Vec3{0, 0, iz},
		)

		offset := box.LocalOffset
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

func capsuleLocalInertiaTensor(mass, radius, halfHeight float32) mgl32.Mat3 {
	if halfHeight <= 0 {
		moment := (2.0 / 5.0) * mass * radius * radius
		return mgl32.Mat3FromRows(
			mgl32.Vec3{moment, 0, 0},
			mgl32.Vec3{0, moment, 0},
			mgl32.Vec3{0, 0, moment},
		)
	}

	r2 := radius * radius
	cylinderVolume := 2 * r2 * halfHeight
	sphereVolume := (4.0 / 3.0) * r2 * radius
	totalVolume := cylinderVolume + sphereVolume
	if totalVolume <= 0 {
		return mgl32.Ident3()
	}

	cylinderMass := mass * cylinderVolume / totalVolume
	sphereMass := mass * sphereVolume / totalVolume
	perpendicular := cylinderMass*(0.25*r2+(1.0/3.0)*halfHeight*halfHeight) +
		sphereMass*(halfHeight*halfHeight+0.75*halfHeight*radius+(2.0/5.0)*r2)
	axis := 0.5*cylinderMass*r2 + (2.0/5.0)*sphereMass*r2

	return mgl32.Mat3FromRows(
		mgl32.Vec3{perpendicular, 0, 0},
		mgl32.Vec3{0, axis, 0},
		mgl32.Vec3{0, 0, perpendicular},
	)
}

func RotationMat3(q mgl32.Quat) mgl32.Mat3 {
	rot := q.Mat4()
	return mgl32.Mat3FromCols(rot.Col(0).Vec3(), rot.Col(1).Vec3(), rot.Col(2).Vec3())
}

func effectiveCollisionBoxVolume(halfExtents mgl32.Vec3) float32 {
	volume := halfExtents.X() * halfExtents.Y() * halfExtents.Z() * 8.0
	if volume > 0 {
		return volume
	}

	maxHalfExtent := maxf(halfExtents.X(), maxf(halfExtents.Y(), halfExtents.Z()))
	if maxHalfExtent <= 0 {
		return 0
	}

	extentFloor := maxf(maxHalfExtent*1e-3, 1e-4)
	effectiveHalfExtents := mgl32.Vec3{
		maxf(halfExtents.X(), extentFloor),
		maxf(halfExtents.Y(), extentFloor),
		maxf(halfExtents.Z(), extentFloor),
	}
	return effectiveHalfExtents.X() * effectiveHalfExtents.Y() * effectiveHalfExtents.Z() * 8.0
}

func invertSymmetricInertiaTensor(m mgl32.Mat3) (mgl32.Mat3, bool) {
	a := [3][3]float64{
		{float64(m[0]), float64(m[3]), float64(m[6])},
		{float64(m[1]), float64(m[4]), float64(m[7])},
		{float64(m[2]), float64(m[5]), float64(m[8])},
	}
	for row := 0; row < 3; row++ {
		for col := row + 1; col < 3; col++ {
			symmetric := 0.5 * (a[row][col] + a[col][row])
			a[row][col] = symmetric
			a[col][row] = symmetric
		}
	}

	maxEntry := 0.0
	for row := 0; row < 3; row++ {
		for col := 0; col < 3; col++ {
			maxEntry = math.Max(maxEntry, math.Abs(a[row][col]))
		}
	}
	if maxEntry <= 1e-12 {
		return mgl32.Mat3{}, false
	}

	v := [3][3]float64{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
	}
	epsilon := maxEntry * 1e-10
	for sweep := 0; sweep < 8; sweep++ {
		rotated := false
		rotated = jacobiRotateSymmetric3(&a, &v, 0, 1, epsilon) || rotated
		rotated = jacobiRotateSymmetric3(&a, &v, 0, 2, epsilon) || rotated
		rotated = jacobiRotateSymmetric3(&a, &v, 1, 2, epsilon) || rotated
		if !rotated {
			break
		}
	}

	maxPrincipalMoment := 0.0
	for axis := 0; axis < 3; axis++ {
		maxPrincipalMoment = math.Max(maxPrincipalMoment, a[axis][axis])
	}
	if maxPrincipalMoment <= 1e-12 {
		return mgl32.Mat3{}, false
	}

	// Bound anisotropy so extremely thin or degenerate bodies keep their axis
	// preference without producing explosive angular response.
	principalMomentFloor := math.Max(maxPrincipalMoment*1e-2, 1e-9)
	var inverse [3][3]float64
	for axis := 0; axis < 3; axis++ {
		principalMoment := a[axis][axis]
		if principalMoment < principalMomentFloor {
			principalMoment = principalMomentFloor
		}
		invPrincipalMoment := 1.0 / principalMoment
		for row := 0; row < 3; row++ {
			for col := 0; col < 3; col++ {
				inverse[row][col] += v[row][axis] * invPrincipalMoment * v[col][axis]
			}
		}
	}

	return mgl32.Mat3FromRows(
		mgl32.Vec3{float32(inverse[0][0]), float32(inverse[0][1]), float32(inverse[0][2])},
		mgl32.Vec3{float32(inverse[1][0]), float32(inverse[1][1]), float32(inverse[1][2])},
		mgl32.Vec3{float32(inverse[2][0]), float32(inverse[2][1]), float32(inverse[2][2])},
	), true
}

func jacobiRotateSymmetric3(a *[3][3]float64, v *[3][3]float64, p int, q int, epsilon float64) bool {
	if math.Abs(a[p][q]) <= epsilon {
		return false
	}

	tau := (a[q][q] - a[p][p]) / (2.0 * a[p][q])
	t := 1.0 / (math.Abs(tau) + math.Sqrt(1.0+tau*tau))
	if tau < 0 {
		t = -t
	}
	c := 1.0 / math.Sqrt(1.0+t*t)
	s := t * c

	app := a[p][p]
	aqq := a[q][q]
	apq := a[p][q]
	a[p][p] = app - t*apq
	a[q][q] = aqq + t*apq
	a[p][q] = 0
	a[q][p] = 0

	for r := 0; r < 3; r++ {
		if r == p || r == q {
			continue
		}
		arp := a[r][p]
		arq := a[r][q]
		a[r][p] = c*arp - s*arq
		a[p][r] = a[r][p]
		a[r][q] = c*arq + s*arp
		a[q][r] = a[r][q]
	}

	for r := 0; r < 3; r++ {
		vrp := v[r][p]
		vrq := v[r][q]
		v[r][p] = c*vrp - s*vrq
		v[r][q] = c*vrq + s*vrp
	}

	return true
}
