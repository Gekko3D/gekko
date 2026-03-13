package gekko

import (
	"github.com/go-gl/mathgl/mgl32"
)

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
	// 1. Carve voxels
	state.VoxelSphereEdit(event.Entity, event.Center, event.Radius, 0)

	// 2. Get the runtime XBrickMap
	voxObj := state.GetVoxelObject(event.Entity)
	if voxObj == nil || voxObj.XBrickMap == nil {
		return
	}

	// 3. Detect disconnected components
	components := voxObj.XBrickMap.SplitDisconnectedComponents()
	if len(components) <= 1 {
		// If the entity is empty now, remove it
		if state.IsEntityEmpty(event.Entity) {
			cmd.RemoveEntity(event.Entity)
		}
		return
	}

	// 4. Handle splitting
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
	foundTransform := false
	foundVMC := false

	for _, c := range cmd.GetAllComponents(event.Entity) {
		switch t := c.(type) {
		case *VoxelModelComponent:
			originalVMC = *t
			originalPalette = t.VoxelPalette
			originalModel = t.VoxelModel
			foundVMC = true
		case VoxelModelComponent:
			originalVMC = t
			originalPalette = t.VoxelPalette
			originalModel = t.VoxelModel
			foundVMC = true
		case *TransformComponent:
			originalTransform = *t
			foundTransform = true
		case TransformComponent:
			originalTransform = t
			foundTransform = true
		}
	}

	if !foundTransform || !foundVMC {
		return
	}

	// Keep largest in original, inherit original ID for rendering stability
	newMap := components[largestIdx].Map
	newMap.ID = voxObj.XBrickMap.ID

	// Update the ECS component to match the new state.
	// CRITICAL: We avoid direct mutation of voxObj.XBrickMap here. 
	// Instead, we update the ECS component's CustomMap and let the 
	// sync system in mod_voxelrt_client_systems.go detect the change.
	// This ensures StructureRevision is incremented and avoids a race
	// condition where GPU flags might be cleared prematurely.
	originalVMC.CustomMap = newMap
	cmd.AddComponents(event.Entity, &originalVMC)

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
		vSize := VoxelSize
		scaledLocalCenter := localCenter.Mul(vSize)
		// Apply original entity's scale
		scaledLocalCenter = scaledLocalCenter.Mul(originalTransform.Scale.X())

		worldOffset := originalTransform.Rotation.Rotate(scaledLocalCenter)
		newWorldPos := originalTransform.Position.Add(worldOffset)

		// Create new entity
		cmd.AddEntity(
			&TransformComponent{
				Position: newWorldPos,
				Rotation: originalTransform.Rotation,
				Scale:    originalTransform.Scale,
			},
			&VoxelModelComponent{
				VoxelModel:   originalModel,
				VoxelPalette: originalPalette,
				CustomMap:    centeredMap,
				PivotMode:    PivotModeCenter,
			},
			&RigidBodyComponent{
				Mass:         float32(comp.VoxelCount) * 0.1, // Simple mass proportional to voxel count
				GravityScale: 1,
			},
			&ColliderComponent{
				Friction:    0.5,
				Restitution: 0.3,
			},
			&DebrisComponent{
				Age:        0,
				MaxAge:     15.0 + float32(event.Entity%50)/10.0, // 15-20s lifetime
				VoxelCount: comp.VoxelCount,
			},
		)
	}
}
