package gekko

// Cancellation ownership and dispatch maps belong to the main thread. Workers
// receive only the channel's read end and own their scopes until publication.
func streamedPreparationCancelled(cancel <-chan struct{}) bool {
	select {
	case <-cancel:
		return true
	default:
		return false
	}
}

func cancelStreamedPreparation(cancel chan struct{}) {
	if cancel != nil && !streamedPreparationCancelled(cancel) {
		close(cancel)
	}
}

func cancelAllStreamedPreparation(state *StreamedLevelRuntimeState) {
	for _, cancel := range state.chunkPrepareCancels {
		cancelStreamedPreparation(cancel)
	}
	for _, cancel := range state.proxyPrepareCancels {
		cancelStreamedPreparation(cancel)
	}
}

// Cancel satisfied dispatches before persistence or residency upgrades can remove
// their loaded replacement. Lost demand is checked after fallback pins rebuild.
func cancelSatisfiedStreamedPreparation(state *StreamedLevelRuntimeState) {
	for coord, cancel := range state.chunkPrepareCancels {
		if _, loaded := state.LoadedChunks[coord]; loaded {
			cancelStreamedPreparation(cancel)
		}
	}
	for coord, cancel := range state.proxyPrepareCancels {
		if _, loaded := state.LoadedSectorProxies[coord]; loaded {
			cancelStreamedPreparation(cancel)
		}
	}
}

func cancelObsoleteStreamedPreparation(state *StreamedLevelRuntimeState) {
	for coord, cancel := range state.chunkPrepareCancels {
		_, desired := state.DesiredChunks[coord]
		_, loaded := state.LoadedChunks[coord]
		if !desired || loaded {
			cancelStreamedPreparation(cancel)
		}
	}
	for coord, cancel := range state.proxyPrepareCancels {
		_, loaded := state.LoadedSectorProxies[coord]
		if state.Config.DisableSectorProxies || !streamedProxySectorDesired(state, coord) || loaded || !streamedSectorProxyCommitNeeded(state, coord) {
			cancelStreamedPreparation(cancel)
		}
	}
}

// A nil token keeps synchronous preparation and legacy result fixtures usable,
// but can never acknowledge a coordinate owned by a real dispatch.
func acknowledgeStreamedChunkPreparation(state *StreamedLevelRuntimeState, p streamedPreparedChunk) bool {
	if p.Generation != state.Generation || state.chunkPrepareCancels[p.Coord] != p.prepareCancel {
		return false
	}
	delete(state.chunkPrepareCancels, p.Coord)
	delete(state.PendingLoads, p.Coord)
	if streamedPreparationCancelled(p.prepareCancel) {
		delete(state.pendingChunkCostHints, p.Coord)
		state.Metrics.PrepareCancelledCount++
		return false
	}
	return true
}

func acknowledgeStreamedProxyPreparation(state *StreamedLevelRuntimeState, p streamedPreparedSectorProxy) bool {
	if p.Generation != state.Generation || state.proxyPrepareCancels[p.SectorCoord] != p.prepareCancel {
		return false
	}
	delete(state.proxyPrepareCancels, p.SectorCoord)
	delete(state.PendingProxyLoads, p.SectorCoord)
	if streamedPreparationCancelled(p.prepareCancel) {
		delete(state.pendingProxyCostHints, p.SectorCoord)
		state.Metrics.PrepareCancelledCount++
		return false
	}
	return true
}

func cancelledStreamedPreparedChunk(p streamedPreparedChunk) streamedPreparedChunk {
	p.release()
	return streamedPreparedChunk{prepareCancel: p.prepareCancel, Generation: p.Generation, Coord: p.Coord, PrepareDuration: p.PrepareDuration}
}

func cancelledStreamedPreparedProxy(p streamedPreparedSectorProxy) streamedPreparedSectorProxy {
	p.release()
	return streamedPreparedSectorProxy{prepareCancel: p.prepareCancel, Generation: p.Generation, SectorCoord: p.SectorCoord, PrepareDuration: p.PrepareDuration}
}
