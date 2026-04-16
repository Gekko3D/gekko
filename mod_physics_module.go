package gekko

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-gl/mathgl/mgl32"
)

type CollisionEventType uint8

const (
	CollisionEventEnter CollisionEventType = iota
	CollisionEventStay
	CollisionEventExit
)

func (t CollisionEventType) String() string {
	switch t {
	case CollisionEventEnter:
		return "enter"
	case CollisionEventStay:
		return "stay"
	case CollisionEventExit:
		return "exit"
	default:
		return "unknown"
	}
}

type PhysicsCollisionEvent struct {
	Type          CollisionEventType
	A             EntityId
	B             EntityId
	Point         mgl32.Vec3
	Normal        mgl32.Vec3
	Penetration   float32
	NormalImpulse float32
	RelativeSpeed float32
	Tick          uint64
}

type PhysicsModule struct {
	UpdateFrequency float32
	Threads         int
	Synchronous     bool
}

func (m PhysicsModule) Install(app *App, cmd *Commands) {
	world := NewPhysicsWorld()

	if m.UpdateFrequency > 0 {
		world.UpdateFrequency = m.UpdateFrequency
	}
	if m.Threads > 0 {
		world.Threads = m.Threads
	}
	cmd.AddResources(world)

	proxy := &PhysicsProxy{}
	cmd.AddResources(proxy)

	if m.Synchronous {
		simulator := NewPhysicsSimulator(world.SpatialGridCellSize)
		cmd.AddResources(simulator)

		app.UseSystem(
			System(SynchronousPhysicsSystem).
				InStage(PhysicsUpdate).
				RunAlways(),
		)
	} else {
		// Start the async physics loop
		go physicsLoop(world, proxy)

		app.UseSystem(
			System(PhysicsPushSystem).
				InStage(PostUpdate).
				RunAlways(),
		)
	}

	app.UseSystem(
		System(PhysicsPullSystem).
			InStage(PreUpdate).
			RunAlways(),
	)
}

func SynchronousPhysicsSystem(cmd *Commands, time *Time, physics *PhysicsWorld, proxy *PhysicsProxy, simulator *PhysicsSimulator) {
	assets := assetServerFromApp(cmd.app)
	snapshot, entities := collectPhysicsSnapshot(cmd, time, physics, assets)

	proxy.pendingState.Store(snapshot)

	// 2. Step
	results := simulator.Step(physics, proxy)

	// 3. Immediately apply results back to components so the next Fixed step sees them
	for _, res := range results.Entities {
		if e, ok := entities[res.Eid]; ok {
			// Update Previous state for interpolation
			if e.rb.LastPhysicsTick != results.Tick {
				if e.rb.LastPhysicsTick == 0 {
					e.rb.PreviousPhysicsPos = res.Pos
					e.rb.PreviousPhysicsRot = res.Rot
				} else {
					e.rb.PreviousPhysicsPos = e.rb.CurrentPhysicsPos
					e.rb.PreviousPhysicsRot = e.rb.CurrentPhysicsRot
				}
				e.rb.CurrentPhysicsPos = res.Pos
				e.rb.CurrentPhysicsRot = res.Rot
				e.rb.LastPhysicsTick = results.Tick
				e.rb.AccumulatedImpulse = mgl32.Vec3{}
				e.rb.AccumulatedTorque = mgl32.Vec3{}
			}

			// We update the Transform IMMEDIATELY to avoid the "frozen frame" bug
			// But note that interpolation will overwrite it in PreUpdate.
			// This is fine because PreUpdate runs AFTER all fixed steps.
			e.tr.Rotation = res.Rot
			e.tr.Position = physicsToRenderPositionWithAssets(assets, res.Pos, res.Rot, e.tr, &e.pm, e.vm)
			e.rb.Velocity = res.Vel
			e.rb.AngularVelocity = res.AngVel
			e.rb.Sleeping = res.Sleeping
			e.rb.IdleTime = res.IdleTime
			e.rb.LastPulledPos = e.tr.Position
			e.rb.LastPulledRot = e.tr.Rotation
		}
	}

	// 4. Publish
	proxy.latestResults.Store(results)
}

type PhysicsProxy struct {
	latestResults atomic.Pointer[PhysicsResults]
	pendingState  atomic.Pointer[PhysicsSnapshot]

	collisionMu       sync.Mutex
	collisionBuffer   []PhysicsCollisionEvent
	lastCollisionTick uint64
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
	CollisionLayer uint32
	CollisionMask  uint32
	IdleTime       float32
	GravityScale   float32
	LinearDamping  float32
	AngularDamping float32
	Sleeping       bool
	Teleport       bool
}

type PhysicsResults struct {
	Tick       uint64
	Generated  time.Time
	Entities   []PhysicsEntityResult
	Collisions []PhysicsCollisionEvent
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

func PhysicsPullSystem(cmd *Commands, time *Time, proxy *PhysicsProxy, physics *PhysicsWorld) {
	assets := assetServerFromApp(cmd.app)

	// Pull latest results from simulation
	results := proxy.latestResults.Load()
	if results != nil {
		proxy.captureCollisionResults(results)

		alpha := time.Alpha
		if time.Alpha == 0 {
			// If not using accumulator (async mode), fallback to old alpha
			alpha = physicsInterpolationAlpha(results.Generated, physics.UpdateFrequency)
		}

		resMap := make(map[EntityId]PhysicsEntityResult)
		for _, res := range results.Entities {
			resMap[res.Eid] = res
		}

		MakeQuery4[TransformComponent, RigidBodyComponent, PhysicsModel, VoxelModelComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, pm *PhysicsModel, vm *VoxelModelComponent) bool {
			if res, ok := resMap[eid]; ok {
				resolvedModel, ok := resolvePhysicsModelForStep(assets, tr, pm, vm)
				if !ok {
					return true
				}
				if pm == nil {
					cmd.AddComponents(eid, resolvedModel)
				}

				if rb.LastPhysicsTick != results.Tick {
					if rb.LastPhysicsTick == 0 {
						rb.PreviousPhysicsPos = res.Pos
						rb.PreviousPhysicsRot = res.Rot
					} else {
						rb.PreviousPhysicsPos = rb.CurrentPhysicsPos
						rb.PreviousPhysicsRot = rb.CurrentPhysicsRot
					}
					rb.CurrentPhysicsPos = res.Pos
					rb.CurrentPhysicsRot = res.Rot
					rb.LastPhysicsTick = results.Tick
					rb.AccumulatedImpulse = mgl32.Vec3{}
					rb.AccumulatedTorque = mgl32.Vec3{}
				}

				interpPos := rb.CurrentPhysicsPos
				interpRot := rb.CurrentPhysicsRot
				if rb.LastPhysicsTick > 0 {
					interpPos = rb.PreviousPhysicsPos.Add(rb.CurrentPhysicsPos.Sub(rb.PreviousPhysicsPos).Mul(alpha))
					interpRot = mgl32.QuatNlerp(rb.PreviousPhysicsRot, rb.CurrentPhysicsRot, alpha)
				}

				tr.Position = physicsToRenderPositionWithAssets(assets, interpPos, interpRot, tr, &resolvedModel, vm)
				tr.Rotation = interpRot
				rb.Velocity = res.Vel
				rb.AngularVelocity = res.AngVel
				rb.Sleeping = res.Sleeping
				rb.IdleTime = res.IdleTime
				rb.LastPulledPos = tr.Position
				rb.LastPulledRot = tr.Rotation
			}
			return true
		}, PhysicsModel{}, VoxelModelComponent{})
	}
}

func (p *PhysicsProxy) captureCollisionResults(results *PhysicsResults) {
	if p == nil || results == nil {
		return
	}

	p.collisionMu.Lock()
	defer p.collisionMu.Unlock()

	if results.Tick == p.lastCollisionTick {
		return
	}
	p.lastCollisionTick = results.Tick
	if len(results.Collisions) == 0 {
		return
	}
	p.collisionBuffer = append(p.collisionBuffer, results.Collisions...)
}

func (p *PhysicsProxy) DrainCollisionEvents() []PhysicsCollisionEvent {
	if p == nil {
		return nil
	}

	p.collisionMu.Lock()
	defer p.collisionMu.Unlock()

	if len(p.collisionBuffer) == 0 {
		return nil
	}

	events := append([]PhysicsCollisionEvent(nil), p.collisionBuffer...)
	p.collisionBuffer = p.collisionBuffer[:0]
	return events
}

func PhysicsPushSystem(cmd *Commands, time *Time, physics *PhysicsWorld, proxy *PhysicsProxy) {
	assets := assetServerFromApp(cmd.app)
	snap, _ := collectPhysicsSnapshot(cmd, time, physics, assets)
	proxy.pendingState.Store(snap)
}

func physicsToRenderPosition(physicsPos mgl32.Vec3, rot mgl32.Quat, tr *TransformComponent, pm *PhysicsModel, vm *VoxelModelComponent) mgl32.Vec3 {
	return physicsToRenderPositionWithAssets(nil, physicsPos, rot, tr, pm, vm)
}

func physicsToRenderPositionWithAssets(assets *AssetServer, physicsPos mgl32.Vec3, rot mgl32.Quat, tr *TransformComponent, pm *PhysicsModel, vm *VoxelModelComponent) mgl32.Vec3 {
	diff := renderToPhysicsOffsetWithAssets(assets, tr, pm, vm)
	rotatedOffset := rot.Rotate(diff)
	return physicsPos.Sub(rotatedOffset)
}

func renderToPhysicsOffset(tr *TransformComponent, pm *PhysicsModel, vm *VoxelModelComponent) mgl32.Vec3 {
	return renderToPhysicsOffsetWithAssets(nil, tr, pm, vm)
}

func renderToPhysicsOffsetWithAssets(assets *AssetServer, tr *TransformComponent, pm *PhysicsModel, vm *VoxelModelComponent) mgl32.Vec3 {
	if tr == nil || pm == nil {
		return mgl32.Vec3{}
	}
	return pm.CenterOffset.Sub(resolveScaledPivotWorldForPhysics(assets, tr, vm))
}

type physicsStepEntityRefs struct {
	tr *TransformComponent
	rb *RigidBodyComponent
	pm PhysicsModel
	vm *VoxelModelComponent
}

func collectPhysicsSnapshot(cmd *Commands, time *Time, physics *PhysicsWorld, assets *AssetServer) (*PhysicsSnapshot, map[EntityId]physicsStepEntityRefs) {
	snapshot := &PhysicsSnapshot{
		Gravity: physics.Gravity,
		Dt:      float32(time.Dt),
	}

	entities := make(map[EntityId]physicsStepEntityRefs)

	MakeQuery5[TransformComponent, RigidBodyComponent, ColliderComponent, PhysicsModel, VoxelModelComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, col *ColliderComponent, pm *PhysicsModel, vm *VoxelModelComponent) bool {
		resolvedModel, ok := resolvePhysicsModelForStep(assets, tr, pm, vm)
		if !ok {
			return true
		}
		if pm == nil {
			cmd.AddComponents(eid, resolvedModel)
		}

		diff := renderToPhysicsOffsetWithAssets(assets, tr, &resolvedModel, vm)

		// Start with visual state
		physPos := tr.Position.Add(tr.Rotation.Rotate(diff))
		physRot := tr.Rotation

		// Detect teleport BEFORE choosing physical state
		isTeleport := false
		if rb.LastPhysicsTick > 0 {
			posDiff := tr.Position.Sub(rb.LastPulledPos).Len()
			rotDiff := 1.0 - float64(absf(tr.Rotation.Dot(rb.LastPulledRot)))
			if posDiff > 0.05 || rotDiff > 0.05 {
				isTeleport = true
			}
		}

		// Use the core physical state if no manual teleport occurred
		// This avoids reading back interpolated values from the last Dynamic frame.
		if !isTeleport && rb.LastPhysicsTick > 0 {
			physPos = rb.CurrentPhysicsPos
			physRot = rb.CurrentPhysicsRot
		}

		invMass := float32(1.0)
		if rb.Mass > 0 {
			invMass = 1.0 / rb.Mass
		}

		invInertiaLocal := CalculateInverseInertiaLocal(rb.Mass, &resolvedModel)
		vel := rb.Velocity.Add(rb.AccumulatedImpulse.Mul(invMass))
		angVel := rb.AngularVelocity.Add(ApplyInverseInertiaWorld(physRot, invInertiaLocal, rb.AccumulatedTorque))

		snapshot.Entities = append(snapshot.Entities, PhysicsEntityState{
			Eid:            eid,
			Pos:            physPos,
			Rot:            physRot,
			Vel:            vel,
			AngVel:         angVel,
			IsStatic:       rb.IsStatic,
			Mass:           rb.Mass,
			Model:          resolvedModel,
			Friction:       col.Friction,
			Restitution:    col.Restitution,
			CollisionLayer: col.CollisionLayer,
			CollisionMask:  col.CollisionMask,
			IdleTime:       rb.IdleTime,
			GravityScale:   rb.GravityScale,
			LinearDamping:  rb.LinearDamping,
			AngularDamping: rb.AngularDamping,
			Sleeping:       rb.Sleeping,
			Teleport:       isTeleport,
		})

		entities[eid] = physicsStepEntityRefs{
			tr: tr,
			rb: rb,
			pm: resolvedModel,
			vm: vm,
		}

		return true
	}, PhysicsModel{}, VoxelModelComponent{})

	return snapshot, entities
}

func resolvePhysicsModelForStep(assets *AssetServer, tr *TransformComponent, pm *PhysicsModel, vm *VoxelModelComponent) (PhysicsModel, bool) {
	if pm != nil {
		return *pm, true
	}
	return buildFallbackPhysicsModelFromVoxel(assets, tr, vm)
}

func buildFallbackPhysicsModelFromVoxel(assets *AssetServer, tr *TransformComponent, vm *VoxelModelComponent) (PhysicsModel, bool) {
	if assets == nil || vm == nil {
		return PhysicsModel{}, false
	}

	geometryID := vm.GeometryAsset()
	if geometryID == (AssetId{}) {
		return PhysicsModel{}, false
	}

	geometryAsset, ok := assets.GetVoxelGeometry(geometryID)
	if !ok {
		return PhysicsModel{}, false
	}

	voxelScale := EffectiveVoxelScale(vm, tr)
	minW := vec3MulComponents(geometryAsset.LocalMin, voxelScale)
	maxW := vec3MulComponents(geometryAsset.LocalMax, voxelScale)
	center := minW.Add(maxW).Mul(0.5)
	half := maxW.Sub(minW).Mul(0.5)

	model := PhysicsModel{
		CenterOffset: center,
		Boxes: []CollisionBox{{
			HalfExtents: half,
			LocalOffset: mgl32.Vec3{},
		}},
	}

	if geometryAsset.XBrickMap != nil {
		model.Grid = &voxelGridSnapshot{
			xbm:        geometryAsset.XBrickMap,
			vSize:      voxelScale.X(),
			voxelScale: voxelScale,
			cachedMin:  geometryAsset.LocalMin,
			cachedMax:  geometryAsset.LocalMax,
		}
	}

	return model, true
}

func resolveScaledPivotWorldForPhysics(assets *AssetServer, tr *TransformComponent, vm *VoxelModelComponent) mgl32.Vec3 {
	if vm == nil {
		return scaledPivotWorld(tr, vm)
	}

	pivot := resolveVoxelPivotLocalForPhysics(assets, tr, vm)
	voxelScale := EffectiveVoxelScale(vm, tr)
	return mgl32.Vec3{
		pivot.X() * voxelScale.X(),
		pivot.Y() * voxelScale.Y(),
		pivot.Z() * voxelScale.Z(),
	}
}

func resolveVoxelPivotLocalForPhysics(assets *AssetServer, tr *TransformComponent, vm *VoxelModelComponent) mgl32.Vec3 {
	if vm == nil {
		if tr != nil {
			return tr.Pivot
		}
		return mgl32.Vec3{}
	}

	switch vm.PivotMode {
	case PivotModeCustom:
		return vm.CustomPivot
	case PivotModeCorner:
		return mgl32.Vec3{}
	case PivotModeCenter:
		if assets != nil {
			geometryID := vm.GeometryAsset()
			if geometryID != (AssetId{}) {
				if geometryAsset, ok := assets.GetVoxelGeometry(geometryID); ok {
					if geometryAsset.XBrickMap != nil {
						minB, maxB := geometryAsset.XBrickMap.ComputeAABB()
						return minB.Add(maxB).Mul(0.5)
					}
					return geometryAsset.LocalMin.Add(geometryAsset.LocalMax).Mul(0.5)
				}
			}
		}
	}

	if tr != nil {
		return tr.Pivot
	}
	return mgl32.Vec3{}
}

func scaledPivotWorld(tr *TransformComponent, vm *VoxelModelComponent) mgl32.Vec3 {
	if tr == nil {
		return mgl32.Vec3{}
	}
	voxelScale := EffectiveVoxelScale(vm, tr)
	return mgl32.Vec3{
		tr.Pivot.X() * voxelScale.X(),
		tr.Pivot.Y() * voxelScale.Y(),
		tr.Pivot.Z() * voxelScale.Z(),
	}
}

func physicsInterpolationAlpha(generated time.Time, updateFrequency float32) float32 {
	if generated.IsZero() || updateFrequency <= 0 {
		return 1.0
	}
	step := time.Duration(float64(time.Second) / float64(updateFrequency))
	if step <= 0 {
		return 1.0
	}
	alpha := float32(time.Since(generated)) / float32(step)
	if alpha < 0 {
		return 0
	}
	if alpha > 1 {
		return 1
	}
	return alpha
}
