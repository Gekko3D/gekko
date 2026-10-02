package gekko

import "reflect"

type streamedRenderKind uint8

const (
	streamedRenderTerrain streamedRenderKind = iota
	streamedRenderImported
	streamedRenderProxy
)

// Values only: ECS rows move when visibility/components change. Readiness is
// always checked against the live marker, never against a captured row pointer.
type streamedRenderTarget struct {
	coord      ChunkCoord
	kind       streamedRenderKind
	ticket     uint64
	generation uint64
}

// A streaming stage shares one live-marker floor across its prepared commits.
// Queue cursors examine each newly buffered marker once, and internal flushes
// reset them. Hooks invalidate the live floor because they may change markers.
type streamedRenderTicketBatch struct {
	depth             int
	floorValid        bool
	pendingEntities   int
	pendingComponents int
}

func beginStreamedRenderTicketBatch(state *StreamedLevelRuntimeState) func() {
	if state == nil {
		return func() {}
	}
	if state.renderTicketBatch.depth == 0 {
		state.renderTicketBatch = streamedRenderTicketBatch{}
	}
	state.renderTicketBatch.depth++
	return func() {
		state.renderTicketBatch.depth--
		if state.renderTicketBatch.depth == 0 {
			state.renderTicketBatch = streamedRenderTicketBatch{}
		}
	}
}

func invalidateStreamedRenderTicketFloor(state *StreamedLevelRuntimeState) {
	state.renderTicketBatch.floorValid = false
	state.renderTicketBatch.pendingEntities, state.renderTicketBatch.pendingComponents = 0, 0
}

func raiseStreamedRenderTicketFloor(state *StreamedLevelRuntimeState, ticket uint64) {
	if ticket > state.nextRenderTicket {
		state.nextRenderTicket = ticket
	}
}

func scanQueuedStreamedRenderTicketFloor(cmd *Commands, state *StreamedLevelRuntimeState) {
	cmd.app.cmdMutex.Lock()
	defer cmd.app.cmdMutex.Unlock()
	batch := &state.renderTicketBatch
	if batch.pendingEntities > len(cmd.app.pendingAdditions) {
		batch.pendingEntities = 0
	}
	if batch.pendingComponents > len(cmd.app.pendingCompAdds) {
		batch.pendingComponents = 0
	}
	scan := func(components []any) {
		for _, component := range components {
			switch marker := component.(type) {
			case *StreamedVoxelRenderComponent:
				raiseStreamedRenderTicketFloor(state, marker.Ticket)
			case StreamedVoxelRenderComponent:
				raiseStreamedRenderTicketFloor(state, marker.Ticket)
			}
		}
	}
	for _, pending := range cmd.app.pendingAdditions[batch.pendingEntities:] {
		scan(pending.components)
	}
	for _, pending := range cmd.app.pendingCompAdds[batch.pendingComponents:] {
		scan(pending.components)
	}
	batch.pendingEntities = len(cmd.app.pendingAdditions)
	batch.pendingComponents = len(cmd.app.pendingCompAdds)
}

func streamedRenderPriority(state *StreamedLevelRuntimeState, target streamedRenderTarget) StreamedVoxelPriority {
	if target.kind == streamedRenderProxy {
		return StreamedVoxelPriorityFallback
	}
	if _, collision := state.CollisionChunks[target.coord]; collision {
		return StreamedVoxelPriorityCollision
	}
	if _, desired := state.DesiredChunks[target.coord]; desired {
		return StreamedVoxelPriorityVisible
	}
	return StreamedVoxelPriorityKeep
}

func nextStreamedRenderTicket(cmd *Commands, state *StreamedLevelRuntimeState) uint64 {
	if !state.renderTicketBatch.floorValid {
		MakeQuery1[StreamedVoxelRenderComponent](cmd).Map(func(_ EntityId, marker *StreamedVoxelRenderComponent) bool {
			raiseStreamedRenderTicketFloor(state, marker.Ticket)
			return true
		})
		state.renderTicketBatch.floorValid = true
	}
	scanQueuedStreamedRenderTicketFloor(cmd, state)
	renderer := voxelRtStateFromApp(cmd.app)
	for {
		state.nextRenderTicket++
		if state.nextRenderTicket == 0 {
			panic("streamed render ticket space exhausted")
		}
		if _, known := renderer.StreamedVoxelStatus(state.nextRenderTicket); !known {
			return state.nextRenderTicket
		}
	}
}

// Called after AddEntity and before its first internal spawn flush. The optional
// renderer resource is enough to opt into managed residency, even without GPU.
func stageStreamedRenderTarget(cmd *Commands, state *StreamedLevelRuntimeState, entity EntityId, coord ChunkCoord, kind streamedRenderKind) bool {
	if voxelRtStateFromApp(cmd.app) != nil {
		state.renderManaged = true
	}
	if !state.renderManaged || entity == 0 {
		return false
	}
	if state.renderTargets == nil {
		state.renderTargets = make(map[EntityId]streamedRenderTarget)
	}
	target := streamedRenderTarget{coord: coord, kind: kind, generation: state.Generation}
	target.ticket = nextStreamedRenderTicket(cmd, state)
	state.renderTargets[entity] = target
	attachStreamedWorkTarget(state, entity, target)
	cmd.AddComponents(entity, &StreamedVoxelRenderComponent{
		Ticket: target.ticket, Generation: target.generation, Priority: streamedRenderPriority(state, target),
	}, &VoxelRenderHiddenComponent{})
	return true
}

func queueStreamedRenderRetirement(state *StreamedLevelRuntimeState, ticket uint64) {
	if ticket == 0 {
		return
	}
	if state.retiredRenderIDs == nil {
		state.retiredRenderIDs = make(map[uint64]struct{})
	}
	state.retiredRenderIDs[ticket] = struct{}{}
}

func retireStreamedRenderTarget(cmd *Commands, state *StreamedLevelRuntimeState, entity EntityId) {
	if target, owned := state.renderTargets[entity]; owned {
		queueStreamedRenderRetirement(state, target.ticket)
		retireStreamedWorkTarget(state, entity)
		cmd.RemoveComponents(entity, &StreamedVoxelRenderComponent{})
		delete(state.renderTargets, entity)
	}
}

// Retirement requires an observed absence of the old marker. Unfinished
// renderer statuses stay alive until the bridge cancels them in PreRender.
func sweepStreamedRenderRetirement(cmd *Commands, state *StreamedLevelRuntimeState, markers map[EntityId]StreamedVoxelRenderComponent) {
	renderer := voxelRtStateFromApp(cmd.app)
	if renderer == nil {
		return
	}
	live := make(map[uint64]struct{}, len(markers))
	for _, marker := range markers {
		live[marker.Ticket] = struct{}{}
	}
	for ticket := range state.retiredRenderIDs {
		if _, exists := live[ticket]; exists {
			continue
		}
		status, known := renderer.StreamedVoxelStatus(ticket)
		if !known {
			delete(state.retiredRenderIDs, ticket)
		} else if status.State >= StreamedVoxelRenderReady {
			renderer.ForgetStreamedVoxel(ticket)
			delete(state.retiredRenderIDs, ticket)
		}
	}
}

func streamedRenderSourceValid(cmd *Commands, entity EntityId) bool {
	if cmd.GetComponent(entity, reflect.TypeOf(TransformComponent{})) == nil {
		return false
	}
	model, exists := voxelModelComponentForEntity(cmd, entity)
	assets := assetServerFromApp(cmd.app)
	if !exists || assets == nil {
		return false
	}
	geometry, ok := ResolveVoxelGeometryMap(assets, &model)
	if !ok || geometry == nil {
		return false
	}
	if model.VoxelPalette != (AssetId{}) {
		_, ok = assets.GetVoxelPalette(model.VoxelPalette)
	}
	return ok
}

// Refresh ticket lifetimes/priorities only. Visibility must wait until the stage
// has completed its observer selection, removals and all prepared commits.
func refreshStreamedRenderResidency(cmd *Commands, state *StreamedLevelRuntimeState) {
	if cmd == nil || cmd.app == nil || state == nil {
		return
	}
	markers := make(map[EntityId]StreamedVoxelRenderComponent)
	MakeQuery1[StreamedVoxelRenderComponent](cmd).Map(func(entity EntityId, marker *StreamedVoxelRenderComponent) bool {
		markers[entity] = *marker
		raiseStreamedRenderTicketFloor(state, marker.Ticket)
		return true
	})
	state.renderTicketBatch.floorValid = true
	observeStreamedWorkTargets(cmd, state, markers)
	for entity, target := range state.renderTargets {
		if !cmd.EntityExists(entity) {
			queueStreamedRenderRetirement(state, target.ticket)
			retireStreamedWorkTarget(state, entity)
			delete(state.renderTargets, entity)
		}
	}
	if state.renderManaged {
		// A removed ECS entity cannot keep blocking alreadyLoaded admission.
		// Use the normal unload owner to retire its cache reference as well.
		for coord, proxy := range state.LoadedSectorProxies {
			if proxy != nil && !cmd.EntityExists(proxy.Entity) {
				unloadStreamedSectorProxy(cmd, state, coord)
			}
		}
	}
	sweepStreamedRenderRetirement(cmd, state, markers)
	reapStreamedWorkRetirement(cmd, state, markers)
	refreshStreamedWorkMetrics(state)
	if !state.Initialized || state.InitErr != nil {
		return
	}
	renderer := voxelRtStateFromApp(cmd.app)
	if renderer != nil {
		state.renderManaged = true
	}
	for entity, target := range state.renderTargets {
		marker := markers[entity]
		status, known := renderer.StreamedVoxelStatus(target.ticket)
		stale := marker.Ticket != target.ticket || marker.Generation != target.generation || target.generation != state.Generation ||
			(known && (status.Entity != entity || status.Generation != target.generation))
		retry := stale || (known && status.State == StreamedVoxelRenderCancelled) ||
			(known && status.State == StreamedVoxelRenderFailed && streamedRenderSourceValid(cmd, entity))
		if retry {
			queueStreamedRenderRetirement(state, target.ticket)
			previous := target
			target.ticket = nextStreamedRenderTicket(cmd, state)
			target.generation = state.Generation
			state.renderTargets[entity] = target
			replaceStreamedWorkTarget(state, entity, previous, target)
			cmd.AddComponents(entity, &StreamedVoxelRenderComponent{
				Ticket: target.ticket, Generation: target.generation, Priority: streamedRenderPriority(state, target),
			})
		}
	}
	if renderer != nil {
		// Optional renderer installation can follow CPU-only residency. Adopt
		// only completed runtime-owned voxel targets; placement ownership and
		// partial failed commits remain with their existing lifecycle.
		for coord, chunk := range state.LoadedChunks {
			finish := beginStreamedWorkCommit(state, state.Generation, nil)
			for entity := range chunk.TerrainEntities {
				if _, owned := state.renderTargets[entity]; !owned && cmd.EntityExists(entity) {
					stageStreamedRenderTarget(cmd, state, entity, coord, streamedRenderTerrain)
				}
			}
			for entity := range chunk.ImportedWorldEntities {
				if _, owned := state.renderTargets[entity]; !owned && cmd.EntityExists(entity) {
					stageStreamedRenderTarget(cmd, state, entity, coord, streamedRenderImported)
				}
			}
			finish()
		}
		for coord, proxy := range state.LoadedSectorProxies {
			if _, owned := state.renderTargets[proxy.Entity]; !owned && cmd.EntityExists(proxy.Entity) {
				stageStreamedRenderTarget(cmd, state, proxy.Entity, coord, streamedRenderProxy)
			}
		}
	}
	// Mutate scalar scheduling metadata without structural churn. Do not keep
	// query pointers across a spawn flush or an archetype move.
	MakeQuery1[StreamedVoxelRenderComponent](cmd).Map(func(entity EntityId, marker *StreamedVoxelRenderComponent) bool {
		refreshStreamedRenderPriority(state, entity, marker)
		return true
	})
}

func refreshStreamedRenderPriority(state *StreamedLevelRuntimeState, entity EntityId, marker *StreamedVoxelRenderComponent) {
	if target, owned := state.renderTargets[entity]; owned && marker.Ticket == target.ticket && marker.Generation == target.generation {
		marker.Priority = streamedRenderPriority(state, target)
	}
}

func streamedRenderTargetReady(cmd *Commands, state *StreamedLevelRuntimeState, entity EntityId) bool {
	target, owned := state.renderTargets[entity]
	if !owned || target.generation != state.Generation || !cmd.EntityExists(entity) {
		return false
	}
	marker, ok := cmd.GetComponent(entity, reflect.TypeOf(StreamedVoxelRenderComponent{})).(*StreamedVoxelRenderComponent)
	if !ok || marker.Ticket != target.ticket || marker.Generation != target.generation {
		return false
	}
	status, known := voxelRtStateFromApp(cmd.app).StreamedVoxelStatus(target.ticket)
	return known && status.State == StreamedVoxelRenderReady && status.Entity == entity && status.Generation == target.generation
}

func streamedRenderSectorHasProxy(state *StreamedLevelRuntimeState, coord ChunkCoord) bool {
	if state.Config.DisableSectorProxies {
		return false
	}
	sector, exists := state.ImportedWorldSectors[coord]
	return exists && len(sector.LODs) > 0 && sector.LODs[0].Kind == "voxel_proxy" && sector.LODs[0].NonEmptyVoxelCount > 0 && sector.LODs[0].ChunkPath != ""
}

func streamedRenderSectorReady(cmd *Commands, state *StreamedLevelRuntimeState, coord ChunkCoord) bool {
	sector, exists := state.ImportedWorldSectors[coord]
	if !exists || len(sector.FullChunkRefs) == 0 {
		return false
	}
	for _, ref := range sector.FullChunkRefs {
		chunkCoord := chunkCoordFromTerrain(ref)
		loaded := state.LoadedChunks[chunkCoord]
		// Explicit emptiness is generation qualified, and is recorded only when
		// preparation created no effective backing target.
		if loaded != nil && loaded.importedEmptyGeneration == state.Generation && state.Generation != 0 {
			continue
		}
		entry, known := state.ImportedWorldEntries[chunkCoord]
		_, overridden := state.importedWorldOverrideMap[importedWorldChunkRuntimeKey(state.BaseWorldID, ref)]
		if known && entry.NonEmptyVoxelCount <= 0 && state.BaseWorldBacking == nil && !overridden && (loaded == nil || len(loaded.ImportedWorldEntities) == 0) {
			continue
		}
		if loaded == nil || len(loaded.ImportedWorldEntities) == 0 {
			return false
		}
		for entity := range loaded.ImportedWorldEntities {
			target, owned := state.renderTargets[entity]
			if !owned || target.kind != streamedRenderImported || target.coord != chunkCoord || !streamedRenderTargetReady(cmd, state, entity) {
				return false
			}
		}
	}
	return true
}

func streamedChunkNeedsRenderProxyBeforeUnload(cmd *Commands, state *StreamedLevelRuntimeState, coord ChunkCoord) bool {
	if !state.renderManaged {
		return streamedChunkNeedsProxyBeforeUnload(state, coord)
	}
	sectorCoord, exists := state.ImportedChunkSector[coord]
	if !exists || !streamedRenderSectorHasProxy(state, sectorCoord) {
		return false
	}
	proxy := state.LoadedSectorProxies[sectorCoord]
	if proxy == nil {
		return true
	}
	target, owned := state.renderTargets[proxy.Entity]
	return !owned || target.kind != streamedRenderProxy || target.coord != sectorCoord || !streamedRenderTargetReady(cmd, state, proxy.Entity)
}

func setStreamedRenderTargetHidden(cmd *Commands, entity EntityId, hidden bool) {
	if !cmd.EntityExists(entity) {
		return
	}
	if hidden {
		if !VoxelEntityRenderHidden(cmd, entity) {
			cmd.AddComponents(entity, &VoxelRenderHiddenComponent{})
		}
	} else if VoxelEntityRenderHidden(cmd, entity) {
		cmd.RemoveComponents(entity, &VoxelRenderHiddenComponent{})
	}
}

// Publish the final visibility decision once per completed streaming stage.
// With a temporarily missing renderer, preserve the last qualified coverage;
// absence never authorizes CPU-ready refinement or coarsening.
func reconcileStreamedRenderResidency(cmd *Commands, state *StreamedLevelRuntimeState) {
	if cmd == nil || state == nil || state.InitErr != nil || !state.renderManaged || voxelRtStateFromApp(cmd.app) == nil {
		return
	}
	// Observer selection may have changed demand since the stage's initial
	// status refresh. Apply its final priorities before the later bridge.
	MakeQuery1[StreamedVoxelRenderComponent](cmd).Map(func(entity EntityId, marker *StreamedVoxelRenderComponent) bool {
		refreshStreamedRenderPriority(state, entity, marker)
		return true
	})
	sectorReady := make(map[ChunkCoord]bool)
	for coord := range state.ImportedWorldSectors {
		sectorReady[coord] = streamedRenderSectorReady(cmd, state, coord)
	}
	// Unadopted CPU entities cannot establish renderer coverage. Normally late
	// installation adopts them above; keep absent targets hidden as well.
	for _, chunk := range state.LoadedChunks {
		for entity := range chunk.TerrainEntities {
			if _, owned := state.renderTargets[entity]; !owned {
				setStreamedRenderTargetHidden(cmd, entity, true)
			}
		}
		for entity := range chunk.ImportedWorldEntities {
			if _, owned := state.renderTargets[entity]; !owned {
				setStreamedRenderTargetHidden(cmd, entity, true)
			}
		}
	}
	for _, proxy := range state.LoadedSectorProxies {
		if _, owned := state.renderTargets[proxy.Entity]; !owned {
			setStreamedRenderTargetHidden(cmd, proxy.Entity, true)
		}
	}
	for entity, target := range state.renderTargets {
		ready := streamedRenderTargetReady(cmd, state, entity)
		switch target.kind {
		case streamedRenderProxy:
			setStreamedRenderTargetHidden(cmd, entity, !ready || !streamedRenderSectorHasProxy(state, target.coord) || sectorReady[target.coord])
		case streamedRenderImported:
			if sector, exists := state.ImportedChunkSector[target.coord]; exists && streamedRenderSectorHasProxy(state, sector) {
				ready = ready && sectorReady[sector]
			}
			setStreamedRenderTargetHidden(cmd, entity, !ready)
		case streamedRenderTerrain:
			setStreamedRenderTargetHidden(cmd, entity, !ready)
		}
	}
}
