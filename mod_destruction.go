package gekko

import "github.com/go-gl/mathgl/mgl32"

type DestructionEvent struct {
	Entity          EntityId
	Center          mgl32.Vec3 // World-space center of destruction
	Radius          float32    // Destruction radius in world units
	CarveOnly       bool       // Edit geometry without scanning/splitting disconnected components.
	RetainEntity    bool       // Keep an empty entity so its owner can restore its source geometry.
	backingMaterial uint8
}

type DestructionQueue struct {
	Events []DestructionEvent
}

type DestructionModule struct{}

func (m DestructionModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(&DestructionQueue{})

	app.UseSystem(
		System(destructionSystem).
			InStage(Update).
			RunAlways(),
	)
}

func destructionSystem(state *VoxelRtState, queue *DestructionQueue, cmd *Commands, server *AssetServer) {
	if queue == nil || len(queue.Events) == 0 {
		return
	}

	events := expandVoxelBackingDestructionEvents(state, queue.Events, cmd)
	byEntity := make(map[EntityId][]DestructionEvent, len(events))
	order := make([]EntityId, 0, len(queue.Events))
	for _, event := range events {
		if _, exists := byEntity[event.Entity]; !exists {
			order = append(order, event.Entity)
		}
		byEntity[event.Entity] = append(byEntity[event.Entity], event)
	}
	for _, entity := range order {
		processDestructionEvents(state, byEntity[entity], cmd, server)
	}

	// Clear the queue
	queue.Events = queue.Events[:0]
}

func expandVoxelBackingDestructionEvents(state *VoxelRtState, events []DestructionEvent, cmd *Commands) []DestructionEvent {
	if state == nil || cmd == nil || len(events) == 0 {
		return events
	}
	expanded := make([]DestructionEvent, 0, len(events))
	for _, event := range events {
		target, backed := voxelBackingForEntity(cmd, event.Entity)
		if !backed {
			expanded = append(expanded, event)
			continue
		}
		if object := state.GetVoxelObject(event.Entity); object != nil && object.XBrickMap != nil {
			localCenter, _ := voxelBackingLocalSphere(object.Transform, event.Center, event.Radius)
			local, _ := voxelSphereIntegerBounds(localCenter, 0)
			event.backingMaterial = voxelBackingMaterialHint(object.XBrickMap, local)
		}
		matched := false
		MakeQuery1[VoxelBackingComponent](cmd).Map(func(eid EntityId, candidate *VoxelBackingComponent) bool {
			if candidate == nil || candidate.OwnerKind != target.OwnerKind || candidate.OwnerID != target.OwnerID || candidate.SourceHash != target.SourceHash {
				return true
			}
			object := state.GetVoxelObject(eid)
			if object == nil || !voxelBackingEventIntersects(candidate, object.Transform, event.Center, event.Radius) {
				return true
			}
			routed := event
			routed.Entity = eid
			expanded = append(expanded, routed)
			matched = true
			return true
		})
		if !matched {
			expanded = append(expanded, event)
		}
	}
	return expanded
}

func processDestructionEvent(state *VoxelRtState, event DestructionEvent, cmd *Commands, server *AssetServer) bool {
	return processDestructionEvents(state, []DestructionEvent{event}, cmd, server)
}

func processDestructionEvents(state *VoxelRtState, events []DestructionEvent, cmd *Commands, server *AssetServer) bool {
	if len(events) == 0 || !destructionEventAllowedForEntity(cmd, events[0].Entity) {
		return false
	}
	entity := events[0].Entity
	voxObj := state.GetVoxelObject(entity)
	if voxObj == nil || voxObj.XBrickMap == nil {
		return false
	}

	// Streamed destruction-resident chunks already own a private live map. Keep
	// its renderer allocation and upload only the edited bricks.
	editableMap := voxObj.XBrickMap
	if _, imported := AuthoredImportedWorldChunkRefForEntity(cmd, entity); !imported {
		_, _, clonedMap, err := EnsureEditableVoxelGeometry(cmd, server, entity)
		if err != nil || clonedMap == nil {
			return false
		}
		editableMap = clonedMap
		voxObj.XBrickMap = editableMap
	}
	backing, backed := voxelBackingForEntity(cmd, entity)
	carveOnly := true
	retainEntity := false
	for _, event := range events {
		if backed {
			center, radius := voxelBackingLocalSphere(voxObj.Transform, event.Center, event.Radius)
			backing.materializeSphere(editableMap, center, radius, event.backingMaterial)
		}
		voxelSphereEditWithTransform(editableMap, voxObj.Transform, event.Center, event.Radius, 0)
		carveOnly = carveOnly && event.CarveOnly
		retainEntity = retainEntity || event.RetainEntity
	}
	if backed {
		// Connectivity splitting is invalid for a partially materialized base.
		carveOnly = true
	}
	MarkVoxelEntityPersistenceDirty(cmd, entity)
	state.markRuntimeEditedVoxelEntity(entity)
	if carveOnly {
		if !backed && !retainEntity && editableMap.GetVoxelCount() == 0 {
			notifyImportedWorldChunkDirty(cmd, entity, editableMap)
			cmd.RemoveEntity(entity)
		}
		return true
	}

	// 2. Detect disconnected components
	components := editableMap.SplitDisconnectedComponents()
	if len(components) <= 1 {
		// If the entity is empty now, remove it
		if !backed && !retainEntity && editableMap.GetVoxelCount() == 0 {
			notifyImportedWorldChunkDirty(cmd, entity, editableMap)
			cmd.RemoveEntity(entity)
		}
		return true
	}

	// 3. Handle splitting
	// Find the largest component to keep in the original entity
	largestIdx := 0
	maxVoxels := -1
	for i, comp := range components {
		if comp.VoxelCount > maxVoxels {
			maxVoxels = comp.VoxelCount
			largestIdx = i
		}
	}
	var originalPalette AssetId
	var originalModel AssetId
	var originalTransform TransformComponent
	var originalVMC VoxelModelComponent
	var originalRB *RigidBodyComponent
	var originalCollider *ColliderComponent

	foundTransform := false
	foundVMC := false

	for _, c := range cmd.GetAllComponents(entity) {
		switch t := c.(type) {
		case *VoxelModelComponent:
			originalVMC = *t
			originalPalette = t.VoxelPalette
			originalVMC.NormalizeGeometryRefs()
			originalModel = t.GeometryAsset()
			foundVMC = true
		case VoxelModelComponent:
			originalVMC = t
			originalPalette = t.VoxelPalette
			originalVMC.NormalizeGeometryRefs()
			originalModel = t.GeometryAsset()
			foundVMC = true
		case *TransformComponent:
			originalTransform = *t
			foundTransform = true
		case TransformComponent:
			originalTransform = t
			foundTransform = true
		case *RigidBodyComponent:
			originalRB = t
		case RigidBodyComponent:
			originalRB = &t
		case *ColliderComponent:
			originalCollider = t
		case ColliderComponent:
			originalCollider = &t
		}
	}

	if !foundTransform || !foundVMC {
		return true
	}

	// Keep largest in original, inherit original ID for rendering stability
	newMap := components[largestIdx].Map
	newMap.ID = editableMap.ID
	voxObj.XBrickMap = newMap

	// Replace the entity's override geometry with the largest surviving component.
	originalVMC.OverrideGeometry = server.RegisterSharedVoxelGeometry(newMap, "")
	cmd.AddComponents(entity, &originalVMC)

	// Update original entity's mass
	if originalRB != nil {
		originalRB.Mass = float32(components[largestIdx].VoxelCount) * 0.1
		cmd.AddComponents(entity, originalRB)
	}

	friction := float32(0.5)
	restitution := float32(0.3)
	if originalCollider != nil {
		friction = originalCollider.Friction
		restitution = originalCollider.Restitution
	}

	// Spawn new entities for smaller components
	for i, comp := range components {
		if i == largestIdx {
			continue
		}

		// Skip if too small for debris
		if comp.VoxelCount < 8 {
			continue
		}

		// Center the component's XBrickMap
		centeredMap, localCenter := comp.Map.Center()

		// Calculate world position: original position + (local center transformed to world)
		vSize := VoxelResolutionOrDefault(&originalVMC)
		scaledLocalCenter := localCenter.Mul(vSize)
		// Apply original entity's scale
		scaledLocalCenter = scaledLocalCenter.Mul(originalTransform.Scale.X())

		worldOffset := originalTransform.Rotation.Rotate(scaledLocalCenter)
		newWorldPos := originalTransform.Position.Add(worldOffset)

		// Inherit velocity from parent (V_shard = V_parent + Omega_parent x WorldOffset)
		vel := mgl32.Vec3{0, 0, 0}
		angVel := mgl32.Vec3{0, 0, 0}
		if originalRB != nil {
			vel = originalRB.Velocity.Add(originalRB.AngularVelocity.Cross(worldOffset))
			angVel = originalRB.AngularVelocity
		}

		// Create new entity
		cmd.AddEntity(
			&TransformComponent{
				Position: newWorldPos,
				Rotation: originalTransform.Rotation,
				Scale:    originalTransform.Scale,
			},
			&VoxelModelComponent{
				SharedGeometry:   originalModel,
				OverrideGeometry: server.RegisterSharedVoxelGeometry(centeredMap, ""),
				VoxelPalette:     originalPalette,
				VoxelResolution:  originalVMC.VoxelResolution,
				PivotMode:        PivotModeCenter,
			},
			&RigidBodyComponent{
				Velocity:        vel,
				AngularVelocity: angVel,
				Mass:            float32(comp.VoxelCount) * 0.1,
				GravityScale:    1,
			},
			&ColliderComponent{
				Friction:    friction,
				Restitution: restitution,
			},
			&DebrisComponent{
				Age:        0,
				MaxAge:     15.0 + float32(entity%50)/10.0, // 15-20s lifetime
				VoxelCount: comp.VoxelCount,
			},
		)
	}
	return true
}

func destructionEventAllowedForEntity(cmd *Commands, eid EntityId) bool {
	if cmd == nil {
		return false
	}
	if _, imported := AuthoredImportedWorldChunkRefForEntity(cmd, eid); !imported {
		return true
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		switch comp.(type) {
		case *StreamedDestructionResidentComponent, StreamedDestructionResidentComponent:
			return true
		}
	}
	return false
}
