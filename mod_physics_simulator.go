package gekko

import (
	"runtime"
	"sync"
	"time"

	"github.com/go-gl/mathgl/mgl32"
)

type PhysicsSimulator struct {
	internalBodies          map[EntityId]*internalBody
	bodiesByID              map[EntityId]*internalBody
	staticContactBodies     map[EntityId]bool
	grid                    *SpatialHashGrid
	manifolds               []collisionManifold
	previousPairs           map[collisionPair]PhysicsCollisionEvent
	currentPairs            map[collisionPair]PhysicsCollisionEvent
	previousContactImpulses map[collisionPair][]cachedContactImpulse
	currentContactImpulses  map[collisionPair][]cachedContactImpulse
	tick                    uint64
}

func NewPhysicsSimulator(gridCellSize float32) *PhysicsSimulator {
	return &PhysicsSimulator{
		internalBodies:          make(map[EntityId]*internalBody),
		bodiesByID:              make(map[EntityId]*internalBody),
		staticContactBodies:     make(map[EntityId]bool),
		grid:                    NewSpatialHashGrid(gridCellSize),
		manifolds:               make([]collisionManifold, 0, 256),
		previousPairs:           make(map[collisionPair]PhysicsCollisionEvent),
		currentPairs:            make(map[collisionPair]PhysicsCollisionEvent),
		previousContactImpulses: make(map[collisionPair][]cachedContactImpulse),
		currentContactImpulses:  make(map[collisionPair][]cachedContactImpulse),
	}
}

func (s *PhysicsSimulator) Step(world *PhysicsWorld, proxy *PhysicsProxy) *PhysicsResults {
	s.tick++

	// Pick up new snapshot
	snap := proxy.pendingState.Swap(nil)
	if snap != nil {
		for _, es := range snap.Entities {
			body, ok := s.internalBodies[es.Eid]
			if !ok {
				body = &internalBody{Eid: es.Eid}
				s.internalBodies[es.Eid] = body
			}
			syncInternalBody(body, es, !ok)
		}
		// Cleanup dead entities
		snapMap := make(map[EntityId]bool)
		for _, es := range snap.Entities {
			snapMap[es.Eid] = true
		}
		for eid := range s.internalBodies {
			if !snapMap[eid] {
				delete(s.internalBodies, eid)
			}
		}
	}

	numWorkers := world.Threads
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}

	bodiesList := make([]*internalBody, 0, len(s.internalBodies))
	clear(s.bodiesByID)
	for eid, b := range s.internalBodies {
		bodiesList = append(bodiesList, b)
		s.bodiesByID[eid] = b
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

				if b.gravityScale != 0 {
					b.vel = b.vel.Add(gravity.Mul(b.gravityScale * dt))
				}

				lDamp := float32(0.999)
				if b.linearDamping > 0 {
					lDamp = b.linearDamping
				}
				aDamp := float32(0.99)
				if b.angularDamping > 0 {
					aDamp = b.angularDamping
				}
				const dampingReferenceHz = float32(60.0)
				b.vel = b.vel.Mul(powf(lDamp, dt*dampingReferenceHz))
				b.angVel = b.angVel.Mul(powf(aDamp, dt*dampingReferenceHz))

				b.pos = b.pos.Add(b.vel.Mul(dt))
				b.rot = integrateAngularVelocity(b.rot, b.angVel, dt)
				b.updateAABB()
			}
		}(bodiesList[start:end])
	}
	wg.Wait()

	s.grid.Clear()
	for _, b := range bodiesList {
		if b.isStatic {
			b.updateAABB()
		}
		s.grid.Insert(b.Eid, AABBComponent{Min: b.aabbMin, Max: b.aabbMax})
	}

	s.manifolds = s.manifolds[:0]
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
				if b.isStatic || b.sleeping {
					continue
				}

				candidates = s.grid.QueryAABBInto(AABBComponent{Min: b.aabbMin, Max: b.aabbMax}, queryUnique, candidates)
				for _, otherEid := range candidates {
					if otherEid == b.Eid {
						continue
					}

					other := s.bodiesByID[otherEid]
					if other == nil {
						continue
					}

					if !other.isStatic && !other.sleeping && b.Eid > other.Eid {
						continue
					}

					voxelHandled := false
					if b.model.Grid != nil || other.model.Grid != nil {
						contacts, wasHandled := checkVoxelCollision(b, other, world.PointInOBBEpsilon)
						if wasHandled {
							voxelHandled = true
							for _, contact := range contacts {
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

					if !voxelHandled {
						for _, boxA := range b.boxes {
							for _, boxB := range other.boxes {
								if collision, normal, penetration, contactPoint := checkSingleOBBCollision(b.pos, b.rot, boxA.Box, other.pos, other.rot, boxB.Box, world.PointInOBBEpsilon); collision {
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
			}
			if len(localManifolds) > 0 {
				manifoldMu.Lock()
				s.manifolds = append(s.manifolds, localManifolds...)
				manifoldMu.Unlock()
			}
		}(bodiesList[start:end])
	}
	wg.Wait()

	cachePointThreshold := maxf(world.CollisionSlop*2.0, 0.06)
	const cacheNormalThreshold = float32(0.9)
	seedManifoldImpulses(s.manifolds, s.previousContactImpulses, cachePointThreshold, cacheNormalThreshold)

	for _, m := range s.manifolds {
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

	clear(s.staticContactBodies)
	for _, m := range s.manifolds {
		if !m.bodyA.isStatic && m.bodyB.isStatic {
			s.staticContactBodies[m.bodyA.Eid] = true
		}
		if !m.bodyB.isStatic && m.bodyA.isStatic {
			s.staticContactBodies[m.bodyB.Eid] = true
		}
	}

	for i := range s.manifolds {
		warmStartManifold(&s.manifolds[i])
	}

	for iter := 0; iter < world.SolverIterations; iter++ {
		for i := range s.manifolds {
			m := &s.manifolds[i]
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

	clearCollisionImpulseMap(s.currentContactImpulses)
	storeCachedManifoldImpulses(s.currentContactImpulses, s.manifolds, cachePointThreshold, cacheNormalThreshold)

	for pair := range s.currentPairs {
		delete(s.currentPairs, pair)
	}
	for _, manifold := range s.manifolds {
		pair := orderedCollisionPair(manifold.bodyA.Eid, manifold.bodyB.Eid)
		event := PhysicsCollisionEvent{
			A:             pair.A,
			B:             pair.B,
			Point:         manifold.point,
			Normal:        manifold.normal,
			Penetration:   manifold.penetration,
			NormalImpulse: manifold.normalImpulse,
			RelativeSpeed: manifold.relativeSpeed,
			Tick:          s.tick,
		}

		if existing, ok := s.currentPairs[pair]; ok {
			s.currentPairs[pair] = mergeCollisionEvent(existing, event)
		} else {
			s.currentPairs[pair] = event
		}
	}

	groundedSleepThreshold := maxf(world.SleepThreshold, gravity.Len()*dt*2.0)
	groundedAngularThreshold := maxf(world.SleepThreshold, world.GroundedAngularThreshold)
	groundedSleepTime := minf(world.SleepTime, world.GroundedSleepTime)
	for _, b := range s.internalBodies {
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
			if s.staticContactBodies[b.Eid] {
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

	res := &PhysicsResults{Tick: s.tick, Generated: time.Now()}
	for _, b := range s.internalBodies {
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
	for pair, event := range s.currentPairs {
		if _, ok := s.previousPairs[pair]; ok {
			event.Type = CollisionEventStay
		} else {
			event.Type = CollisionEventEnter
		}
		res.Collisions = append(res.Collisions, event)
	}
	for pair, previous := range s.previousPairs {
		if _, ok := s.currentPairs[pair]; ok {
			continue
		}
		previous.Type = CollisionEventExit
		previous.Penetration = 0
		previous.NormalImpulse = 0
		previous.RelativeSpeed = 0
		previous.Tick = s.tick
		res.Collisions = append(res.Collisions, previous)
	}
	s.previousPairs, s.currentPairs = s.currentPairs, s.previousPairs
	s.previousContactImpulses, s.currentContactImpulses = s.currentContactImpulses, s.previousContactImpulses

	return res
}
