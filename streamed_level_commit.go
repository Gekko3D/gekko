package gekko

import "container/heap"

// Workers publish only to the existing transport channels. The main thread
// transfers envelopes here without acknowledging or releasing their leases.
type streamedReadyResult struct {
	identity streamedPrepareIdentity
	birth    uint64
	chunk    *streamedPreparedChunk
	proxy    *streamedPreparedSectorProxy
}

type streamedReadyOwner struct {
	sequence, nextID       uint64
	nextKind               streamedPrepareKind
	chunkCount, proxyCount int
	results                map[uint64]streamedReadyResult
}

// Heap records contain only scalar scheduling metadata, never payloads or ECS
// rows. Priority is rebuilt from current demand for each captured frontier.
type streamedReadyCandidate struct {
	id uint64
	streamedPrepareCandidate
}

type streamedReadyQueue []streamedReadyCandidate

func (q streamedReadyQueue) Len() int { return len(q) }
func (q streamedReadyQueue) Less(i, j int) bool {
	a, b := q[i].streamedPrepareCandidate, q[j].streamedPrepareCandidate
	return (streamedPrepareQueue{a, b}).Less(0, 1)
}
func (q streamedReadyQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *streamedReadyQueue) Push(value any) {
	*q = append(*q, value.(streamedReadyCandidate))
}
func (q *streamedReadyQueue) Pop() any {
	last := len(*q) - 1
	value := (*q)[last]
	*q = (*q)[:last]
	return value
}

func streamedReadyCapacity(state *StreamedLevelRuntimeState) int {
	full, proxy := cap(state.PreparedLoads), cap(state.PreparedProxyLoads)
	maxInt := int(^uint(0) >> 1)
	if full > maxInt-proxy {
		return maxInt
	}
	return max(2, full+proxy)
}

func (owner *streamedReadyOwner) retain(result streamedReadyResult) {
	owner.nextID++
	if owner.nextID == 0 {
		panic("streamed ready identity space exhausted")
	}
	if owner.results == nil {
		owner.results = make(map[uint64]streamedReadyResult)
	}
	result.birth = owner.sequence
	owner.results[owner.nextID] = result
	if result.identity.kind == streamedPrepareProxy {
		owner.proxyCount++
	} else {
		owner.chunkCount++
	}
}

func (owner *streamedReadyOwner) take(id uint64) (streamedReadyResult, bool) {
	result, present := owner.results[id]
	if !present {
		return streamedReadyResult{}, false
	}
	delete(owner.results, id)
	if result.identity.kind == streamedPrepareProxy {
		owner.proxyCount--
	} else {
		owner.chunkCount--
	}
	if len(owner.results) == 0 {
		owner.results = nil
	}
	return result, true
}

func streamedReadyPriority(state *StreamedLevelRuntimeState, result streamedReadyResult) int {
	coord := result.identity.coord
	if result.proxy != nil {
		p := result.proxy
		if p.Generation != state.Generation || state.proxyPrepareCancels[coord] != p.prepareCancel ||
			streamedPreparationCancelled(p.prepareCancel) || p.Err != nil || !streamedProxySectorDesired(state, coord) {
			return -1
		}
		if _, loaded := state.LoadedSectorProxies[coord]; loaded || !streamedSectorProxyCommitNeeded(state, coord) {
			return -1
		}
	} else {
		p := result.chunk
		if p.Generation != state.Generation || state.chunkPrepareCancels[coord] != p.prepareCancel ||
			streamedPreparationCancelled(p.prepareCancel) || p.Err != nil {
			return -1
		}
		_, desired := state.DesiredChunks[coord]
		_, loaded := state.LoadedChunks[coord]
		if !desired || loaded {
			return -1
		}
	}
	priority := streamedPreparationPriority(state, result.identity)
	promotion := min(uint64(priority), (state.readyCommits.sequence-result.birth)/8)
	return priority - int(promotion)
}

func captureStreamedReadyFrontier(state *StreamedLevelRuntimeState) streamedReadyQueue {
	owner := &state.readyCommits
	owner.sequence++
	if owner.sequence == 0 {
		owner.sequence = 1
		for id, result := range owner.results {
			result.birth = owner.sequence
			owner.results[id] = result
		}
	}
	// Capture entry lengths once. Every receive is nonblocking, including a
	// single possible rendezvous per unbuffered lane. Concurrent raw receivers
	// can empty a lane without making the streaming stage wait.
	remaining := [2]int{len(state.PreparedProxyLoads), len(state.PreparedLoads)}
	if state.PreparedProxyLoads != nil && cap(state.PreparedProxyLoads) == 0 {
		remaining[streamedPrepareProxy] = 1
	}
	if state.PreparedLoads != nil && cap(state.PreparedLoads) == 0 {
		remaining[streamedPrepareFull] = 1
	}
	capacity := streamedReadyCapacity(state)
	for len(owner.results) < capacity && (remaining[0] > 0 || remaining[1] > 0) {
		kind := owner.nextKind
		if remaining[kind] == 0 {
			kind = 1 - kind
		}
		if kind == streamedPrepareProxy {
			select {
			case prepared, open := <-state.PreparedProxyLoads:
				remaining[kind]--
				if !open {
					remaining[kind] = 0
					continue
				}
				owner.retain(streamedReadyResult{identity: streamedPrepareIdentity{coord: prepared.SectorCoord, kind: kind}, proxy: &prepared})
			default:
				remaining[kind] = 0
				continue
			}
		} else {
			select {
			case prepared, open := <-state.PreparedLoads:
				remaining[kind]--
				if !open {
					remaining[kind] = 0
					continue
				}
				owner.retain(streamedReadyResult{identity: streamedPrepareIdentity{coord: prepared.Coord, kind: kind}, chunk: &prepared})
			default:
				remaining[kind] = 0
				continue
			}
		}
		owner.nextKind = 1 - kind
	}
	queue := make(streamedReadyQueue, 0, len(owner.results))
	for id, result := range owner.results {
		queue = append(queue, streamedReadyCandidate{id: id, streamedPrepareCandidate: streamedPrepareCandidate{
			identity: result.identity, priority: streamedReadyPriority(state, result), birth: result.birth,
		}})
	}
	heap.Init(&queue)
	return queue
}

func drainStreamedReadyResults(state *StreamedLevelRuntimeState) {
	owner := &state.readyCommits
	for id := range owner.results {
		result, present := owner.take(id)
		if !present {
			continue
		}
		if result.proxy != nil {
			p := result.proxy
			acknowledgeStreamedProxyPreparation(state, *p)
			finishStreamedWorkAttempt(state, p.Generation, p.prepareCancel)
			p.release()
		} else {
			p := result.chunk
			acknowledgeStreamedChunkPreparation(state, *p)
			finishStreamedWorkAttempt(state, p.Generation, p.prepareCancel)
			p.release()
		}
	}
}

func consumeStreamedPreparedProxy(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, prepared streamedPreparedSectorProxy) {
	defer prepared.release()
	defer finishStreamedWorkAttempt(state, prepared.Generation, prepared.prepareCancel)
	if !acknowledgeStreamedProxyPreparation(state, prepared) {
		return
	}
	if prepared.retryCost > 0 {
		if state.pendingProxyCostHints == nil {
			state.pendingProxyCostHints = make(map[ChunkCoord]int64)
		}
		state.pendingProxyCostHints[prepared.SectorCoord] = prepared.retryCost
		return
	}
	delete(state.pendingProxyCostHints, prepared.SectorCoord)
	recordPreparedStreamedSectorProxyAuxMetrics(state, prepared)
	if prepared.Err != nil {
		state.Metrics.PrepareErrorCount++
		if state.InitErr == nil {
			state.InitErr = prepared.Err
		}
		return
	}
	if !streamedProxySectorDesired(state, prepared.SectorCoord) {
		return
	}
	if _, alreadyLoaded := state.LoadedSectorProxies[prepared.SectorCoord]; alreadyLoaded {
		return
	}
	if !streamedSectorProxyCommitNeeded(state, prepared.SectorCoord) {
		return
	}
	entityCount, err := commitPreparedStreamedSectorProxy(cmd, assets, state, prepared)
	if err != nil {
		state.Metrics.CommitErrorCount++
		if state.InitErr == nil {
			state.InitErr = err
		}
		return
	}
	state.Metrics.ChunksCommittedLastFrame++
	state.Metrics.ProxyChunksCommittedLastFrame++
	state.Metrics.EntitiesCommittedLastFrame += entityCount
	return
}

func consumeStreamedPreparedChunk(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, prepared streamedPreparedChunk) {
	defer prepared.release()
	defer finishStreamedWorkAttempt(state, prepared.Generation, prepared.prepareCancel)
	if !acknowledgeStreamedChunkPreparation(state, prepared) {
		return
	}
	if prepared.retryCost > 0 {
		if state.pendingChunkCostHints == nil {
			state.pendingChunkCostHints = make(map[ChunkCoord]int64)
		}
		state.pendingChunkCostHints[prepared.Coord] = prepared.retryCost
		return
	}
	delete(state.pendingChunkCostHints, prepared.Coord)
	recordPreparedStreamedChunkMetrics(state, prepared)
	if prepared.Err != nil {
		state.Metrics.PrepareErrorCount++
		if state.InitErr == nil {
			state.InitErr = prepared.Err
		}
		return
	}
	if _, stillDesired := state.DesiredChunks[prepared.Coord]; !stillDesired {
		return
	}
	if _, alreadyLoaded := state.LoadedChunks[prepared.Coord]; alreadyLoaded {
		return
	}
	collisionCommitCountBefore := state.Metrics.CollisionChunkCommitCount
	entityCount, err := commitPreparedStreamedChunk(cmd, assets, state, prepared)
	if err != nil {
		state.Metrics.CommitErrorCount++
		if state.InitErr == nil {
			state.InitErr = err
		}
		return
	}
	state.Metrics.ChunksCommittedLastFrame++
	state.Metrics.FullChunksCommittedLastFrame++
	if state.Metrics.CollisionChunkCommitCount > collisionCommitCountBefore {
		state.Metrics.CollisionChunksCommittedLastFrame++
	}
	state.Metrics.EntitiesCommittedLastFrame += entityCount
	if sectorCoord, ok := state.ImportedChunkSector[prepared.Coord]; ok {
		reconcileStreamedSectorProxyAfterFullCommit(cmd, state, sectorCoord)
	}
}
