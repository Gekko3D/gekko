package gekko

// voxelCandidateInventory owns only membership and row locations. Typed columns
// and component pointers are acquired transiently on each bridge pass.
type voxelCandidateInventory struct {
	owner       *ecsStorage
	revision    uint64
	initialized bool
	transformID componentId
	modelID     componentId
	batches     []voxelCandidateBatch
	candidates  []voxelCandidate
}

type voxelCandidateBatch struct {
	archetype *archetype
	start     int
	end       int
}

type voxelCandidate struct {
	entity EntityId
	row    row
}

func (state *VoxelRtState) rebuildVoxelCandidates(ecs *Ecs) {
	inventory := &state.voxelCandidates
	if inventory.initialized && inventory.owner == ecs.storage && inventory.revision == ecs.StructuralRevision() {
		return
	}

	// Clear the entire capacity before reuse so shrinking batches cannot retain
	// obsolete archetypes through the backing array's tail.
	clear(inventory.batches[:cap(inventory.batches)])
	clear(inventory.candidates[:cap(inventory.candidates)])
	inventory.batches = inventory.batches[:0]
	inventory.candidates = inventory.candidates[:0]
	inventory.owner = ecs.storage
	inventory.revision = ecs.StructuralRevision()
	inventory.transformID, inventory.modelID = identifyComponents2[TransformComponent, VoxelModelComponent](ecs)

	for _, arch := range ecs.storage.archetypes {
		if len(arch.entities) == 0 {
			continue
		}
		if _, ok := arch.componentData[inventory.transformID]; !ok {
			continue
		}
		if _, ok := arch.componentData[inventory.modelID]; !ok {
			continue
		}
		start := len(inventory.candidates)
		for entity, row := range arch.entities {
			inventory.candidates = append(inventory.candidates, voxelCandidate{entity: entity, row: row})
		}
		inventory.batches = append(inventory.batches, voxelCandidateBatch{
			archetype: arch,
			start:     start,
			end:       len(inventory.candidates),
		})
	}
	if len(inventory.candidates) == 0 {
		inventory.batches = nil
		inventory.candidates = nil
	}
	inventory.initialized = true
	state.VoxelCandidateInventoryBuildCount++
	state.VoxelCandidateCount = len(inventory.candidates)
}

// eachVoxelCandidate follows query pointer and mutation rules: field writes and
// buffered commands are supported; immediate structural mutation or a manual
// command flush during iteration is unsupported. Iteration order is unspecified.
func (state *VoxelRtState) eachVoxelCandidate(cmd *Commands, visit func(EntityId, *TransformComponent, *VoxelModelComponent) bool) {
	state.rebuildVoxelCandidates(cmd.app.ecs)
	inventory := &state.voxelCandidates
	for _, batch := range inventory.batches {
		transforms := batch.archetype.componentData[inventory.transformID].([]TransformComponent)
		models := batch.archetype.componentData[inventory.modelID].([]VoxelModelComponent)
		for _, candidate := range inventory.candidates[batch.start:batch.end] {
			if !visit(candidate.entity, &transforms[candidate.row], &models[candidate.row]) {
				return
			}
		}
	}
}
