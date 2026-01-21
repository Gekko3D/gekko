package gekko

import (
	"time"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// RaycastHit describes a raycast result against voxel objects in the scene.
type RaycastHit struct {
	Hit    bool
	T      float32
	Pos    [3]int
	Normal mgl32.Vec3
	Entity EntityId
}

// VoxelSeparationResult holds the outcome of splitting disconnected voxel components.
type VoxelSeparationResult struct {
	Entity    EntityId
	XBrickMap *volume.XBrickMap
	Min       mgl32.Vec3
	Max       mgl32.Vec3
}

// VoxelSphereEdit carves or fills a sphere in local object space, using world-space
// coordinates and the current transform of the voxel object. Positive val places voxels,
// zero removes (carve).
func (s *VoxelRtState) VoxelSphereEdit(eid EntityId, worldCenter mgl32.Vec3, radius float32, val uint8) {
	if s == nil {
		return
	}
	obj := s.getVoxelObject(eid)
	if obj == nil || obj.XBrickMap == nil {
		return
	}

	// Transform center to local space
	w2o := obj.Transform.WorldToObject()
	localCenter := w2o.Mul4x1(worldCenter.Vec4(1.0)).Vec3()

	// Scale radius by object scale (approximate by avg scale)
	scale := obj.Transform.Scale
	avgScale := (scale.X() + scale.Y() + scale.Z()) / 3.0
	if avgScale == 0 {
		avgScale = 1.0
	}
	localRadius := radius / avgScale

	volume.Sphere(obj.XBrickMap, localCenter, localRadius, val)
}

// SplitDisconnectedComponents separates disconnected voxel components on an entity's map.
// It keeps the largest component on the original entity and returns the others as results
// for spawning separate entities. Also updates physics data back into ECS for the modified
// parent object.
func (s *VoxelRtState) SplitDisconnectedComponents(cmd *Commands, eid EntityId) []VoxelSeparationResult {
	if s == nil {
		return nil
	}
	obj := s.getVoxelObject(eid)
	if obj == nil || obj.XBrickMap == nil {
		return nil
	}

	var results []VoxelSeparationResult
	components := obj.XBrickMap.SplitDisconnectedComponents()
	if len(components) > 1 {
		// Keep largest in original object
		largestIdx := 0
		maxVoxels := 0
		for i, comp := range components {
			if comp.VoxelCount > maxVoxels {
				maxVoxels = comp.VoxelCount
				largestIdx = i
			}
		}

		obj.XBrickMap = components[largestIdx].Map
		obj.XBrickMap.StructureDirty = true
		obj.XBrickMap.AABBDirty = true
		obj.UpdateWorldAABB()

		// Sync back to ECS parent VM component
		if cmd != nil {
			allComps := cmd.GetAllComponents(eid)
			for _, c := range allComps {
				if vm, ok := c.(VoxelModelComponent); ok {
					vm.CustomMap = obj.XBrickMap
					vm.CustomPhysicsData = AnalyzePhysicsFromMap(obj.XBrickMap)
					cmd.AddComponents(eid, vm)
					break
				}
			}
		}

		// Return separated parts
		for i, comp := range components {
			if i == largestIdx {
				continue
			}
			results = append(results, VoxelSeparationResult{
				Entity:    eid,
				XBrickMap: comp.Map,
				Min:       comp.Min,
				Max:       comp.Max,
			})
		}
	} else {
		// No split, but ensure physics data is updated for destructible meshes
		if cmd != nil {
			allComps := cmd.GetAllComponents(eid)
			for _, c := range allComps {
				if vm, ok := c.(VoxelModelComponent); ok {
					vm.CustomPhysicsData = AnalyzePhysicsFromMap(obj.XBrickMap)
					cmd.AddComponents(eid, vm)
					break
				}
			}
		}
	}

	return results
}

// IsEntityEmpty returns true if the voxel object for the entity has zero voxels.
func (s *VoxelRtState) IsEntityEmpty(eid EntityId) bool {
	if s == nil {
		return true
	}
	obj := s.getVoxelObject(eid)
	if obj == nil || obj.XBrickMap == nil {
		return true
	}
	return obj.XBrickMap.GetVoxelCount() == 0
}

// ApplySeparation spawns a new entity for a separated component, inheriting
// key properties (transform, physics, palette/model) from the parent entity.
func (s *VoxelRtState) ApplySeparation(cmd *Commands, res VoxelSeparationResult, prof *Profiler) {
	if s == nil || s.RtApp == nil || cmd == nil {
		return
	}

	// Need parent components
	var parentTr TransformComponent
	var parentVm VoxelModelComponent
	foundTr, foundVm := false, false

	allComps := cmd.GetAllComponents(res.Entity)
	for _, c := range allComps {
		switch comp := c.(type) {
		case TransformComponent:
			parentTr = comp
			foundTr = true
		case VoxelModelComponent:
			parentVm = comp
			foundVm = true
		}
	}

	if !foundTr || !foundVm {
		return
	}

	// Check for optional physics components on parent
	var hasRb, hasCol bool
	for _, c := range allComps {
		if _, ok := c.(RigidBodyComponent); ok {
			hasRb = true
		}
		if _, ok := c.(ColliderComponent); ok {
			hasCol = true
		}
	}

	// 1. Center the map
	shiftedMap, localCenter := res.XBrickMap.Center()

	// 2. Calculate world position
	vSize := s.RtApp.Scene.TargetVoxelSize
	if vSize == 0 {
		vSize = 0.1
	}

	// World offset is local center rotated and scaled by parent
	worldOffset := parentTr.Rotation.Rotate(localCenter.Mul(vSize))
	worldOffset = mgl32.Vec3{
		worldOffset.X() * parentTr.Scale.X(),
		worldOffset.Y() * parentTr.Scale.Y(),
		worldOffset.Z() * parentTr.Scale.Z(),
	}
	newPos := parentTr.Position.Add(worldOffset)

	// 3. Half extents for collider
	minB, maxB := res.Min, res.Max
	halfExtents := maxB.Sub(minB).Mul(0.5)

	// 4. Create new entity
	newComps := []any{
		&TransformComponent{
			Position: newPos,
			Rotation: parentTr.Rotation,
			Scale:    parentTr.Scale,
		},
		&VoxelModelComponent{
			VoxelModel:        parentVm.VoxelModel,
			VoxelPalette:      parentVm.VoxelPalette,
			CustomMap:         shiftedMap,
			CustomPhysicsData: AnalyzePhysicsFromMap(shiftedMap),
		},
	}

	if hasCol {
		newComps = append(newComps, &ColliderComponent{
			AABBHalfExtents: halfExtents.Mul(vSize),
		})
	}

	if hasRb {
		newComps = append(newComps, &RigidBodyComponent{Mass: 1.0, GravityScale: 1.0})
	}

	cmd.AddEntity(newComps...)
}

// Moved from mod_vox_rt.go: apply voxel edits and splitting, with nav dirty marking and per-frame amortization
func VoxelAppliedEditSystem(cmd *Commands, editQueue *VoxelEditQueue, state *VoxelRtState, navSys *NavigationSystem, prof *Profiler) {
	if prof != nil {
		start := time.Now()
		defer func() { prof.EditTime += time.Since(start) }()
	}
	if editQueue == nil || state == nil {
		return
	}

	if len(editQueue.Spheres) == 0 && len(editQueue.Edits) == 0 {
		return
	}

	// 1. Process Spheres
	if state.splitQueue == nil {
		state.splitQueue = make(map[EntityId]bool)
	}

	count := 0
	budget := editQueue.BudgetPerFrame
	if budget <= 0 {
		budget = 1024 // Default budget
	}

	// Drain spheres
	for len(editQueue.Spheres) > 0 && count < budget {
		sphere := editQueue.Spheres[0]
		editQueue.Spheres = editQueue.Spheres[1:]

		state.VoxelSphereEdit(sphere.Entity, sphere.Center, sphere.Radius, sphere.Value)
		if navSys != nil {
			// Notify navigation system of dirty area
			// Assume standard voxel size if not available
			vSize := float32(0.1)
			if state.RtApp != nil && state.RtApp.Scene != nil {
				vSize = state.RtApp.Scene.TargetVoxelSize
			}
			min := sphere.Center.Sub(mgl32.Vec3{sphere.Radius, sphere.Radius, sphere.Radius})
			max := sphere.Center.Add(mgl32.Vec3{sphere.Radius, sphere.Radius, sphere.Radius})
			navSys.MarkDirtyArea(min, max, vSize, 8) // Hardcoded region size 8 for now
		}
		if sphere.Value == 0 {
			state.splitQueue[sphere.Entity] = true
		}
		count++
	}

	// Drain point edits
	for len(editQueue.Edits) > 0 && count < budget {
		edit := editQueue.Edits[0]
		editQueue.Edits = editQueue.Edits[1:]

		// For now use a tiny sphere to approximate a point set
		state.VoxelSphereEdit(edit.Entity, mgl32.Vec3{float32(edit.Pos[0]), float32(edit.Pos[1]), float32(edit.Pos[2])}, 0.1, edit.Val)

		if navSys != nil {
			vSize := float32(0.1)
			if state.RtApp != nil && state.RtApp.Scene != nil {
				vSize = state.RtApp.Scene.TargetVoxelSize
			}
			pos := mgl32.Vec3{float32(edit.Pos[0]), float32(edit.Pos[1]), float32(edit.Pos[2])}
			min := pos.Sub(mgl32.Vec3{0.1, 0.1, 0.1})
			max := pos.Add(mgl32.Vec3{0.1, 0.1, 0.1})
			navSys.MarkDirtyArea(min, max, vSize, 8)
		}

		if edit.Val == 0 {
			state.splitQueue[edit.Entity] = true
		}
		count++
	}

	// 2. Process Separations (Amortized: only one entity per frame)
	for eid := range state.splitQueue {
		delete(state.splitQueue, eid)

		// Optimization: The large-scale world should not be checked for splits
		// as it would be extremely expensive and technically impossible for the terrain.
		if _, isWorld := state.worldMap[eid]; isWorld {
			continue
		}

		results := state.SplitDisconnectedComponents(cmd, eid)
		for _, res := range results {
			state.ApplySeparation(cmd, res, prof)
		}

		if state.IsEntityEmpty(eid) {
			cmd.RemoveEntity(eid)
		}

		// Only one per frame to avoid stutters
		break
	}
}

// Moved from mod_vox_rt.go: simple debug key to cycle camera debug mode
func voxelRtDebugSystem(input *Input, state *VoxelRtState) {
	if input.JustPressed[KeyF2] {
		mode := state.RtApp.Camera.DebugMode
		state.RtApp.Camera.DebugMode = (mode + 1) % 3
	}
}