package gekko

import "github.com/go-gl/mathgl/mgl32"

type DestructionEvent struct {
	Entity EntityId
	Center mgl32.Vec3 // World-space center of destruction
	Radius float32    // Destruction radius in world units
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

	for _, event := range queue.Events {
		processDestructionEvent(state, event, cmd, server)
	}

	// Clear the queue
	queue.Events = queue.Events[:0]
}

func processDestructionEvent(state *VoxelRtState, event DestructionEvent, cmd *Commands, server *AssetServer) {
	voxObj := state.GetVoxelObject(event.Entity)
	if voxObj == nil || voxObj.XBrickMap == nil {
		return
	}

	// 1. Carve voxels on a private geometry clone.
	_, _, editableMap, err := EnsureEditableVoxelGeometry(cmd, server, event.Entity)
	if err != nil || editableMap == nil {
		return
	}
	voxelSphereEditWithTransform(editableMap, voxObj.Transform, event.Center, event.Radius, 0)

	// 2. Detect disconnected components
	components := editableMap.SplitDisconnectedComponents()
	if len(components) <= 1 {
		// If the entity is empty now, remove it
		if editableMap.GetVoxelCount() == 0 {
			cmd.RemoveEntity(event.Entity)
		}
		return
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

	for _, c := range cmd.GetAllComponents(event.Entity) {
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
		return
	}

	// Keep largest in original, inherit original ID for rendering stability
	newMap := components[largestIdx].Map
	newMap.ID = editableMap.ID

	// Replace the entity's override geometry with the largest surviving component.
	originalVMC.OverrideGeometry = server.RegisterSharedVoxelGeometry(newMap, "")
	cmd.AddComponents(event.Entity, &originalVMC)

	// Update original entity's mass
	if originalRB != nil {
		originalRB.Mass = float32(components[largestIdx].VoxelCount) * 0.1
		cmd.AddComponents(event.Entity, originalRB)
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
				MaxAge:     15.0 + float32(event.Entity%50)/10.0, // 15-20s lifetime
				VoxelCount: comp.VoxelCount,
			},
		)
	}
}
