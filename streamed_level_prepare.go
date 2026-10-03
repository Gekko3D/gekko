package gekko

import "container/heap"

type streamedPrepareKind uint8

const (
	streamedPrepareProxy streamedPrepareKind = iota
	streamedPrepareFull
)

type streamedPrepareIdentity struct {
	coord ChunkCoord
	kind  streamedPrepareKind
}

// Waiting belongs to unmet demand, independently of a dispatch's cancellation
// token. In particular, an old terminal acknowledgement cannot erase new age.
type streamedPrepareScheduler struct {
	sequence uint64
	waiting  map[streamedPrepareIdentity]uint64
}

type streamedPrepareCandidate struct {
	identity streamedPrepareIdentity
	priority int
	birth    uint64
}

type streamedPrepareQueue []streamedPrepareCandidate

func (q streamedPrepareQueue) Len() int { return len(q) }
func (q streamedPrepareQueue) Less(i, j int) bool {
	a, b := q[i], q[j]
	if a.priority != b.priority {
		return a.priority < b.priority
	}
	if a.birth != b.birth {
		return a.birth < b.birth
	}
	if a.identity.coord.X != b.identity.coord.X {
		return a.identity.coord.X < b.identity.coord.X
	}
	if a.identity.coord.Y != b.identity.coord.Y {
		return a.identity.coord.Y < b.identity.coord.Y
	}
	if a.identity.coord.Z != b.identity.coord.Z {
		return a.identity.coord.Z < b.identity.coord.Z
	}
	return a.identity.kind < b.identity.kind
}
func (q streamedPrepareQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *streamedPrepareQueue) Push(value any) {
	*q = append(*q, value.(streamedPrepareCandidate))
}
func (q *streamedPrepareQueue) Pop() any {
	last := len(*q) - 1
	value := (*q)[last]
	*q = (*q)[:last]
	return value
}

func streamedPreparationNeeded(state *StreamedLevelRuntimeState, identity streamedPrepareIdentity) bool {
	coord := identity.coord
	if identity.kind == streamedPrepareFull {
		_, desired := state.DesiredChunks[coord]
		_, loaded := state.LoadedChunks[coord]
		return desired && !loaded && streamedChunkHasLoadableContent(state, coord)
	}
	if state.Config.DisableSectorProxies || !streamedProxySectorDesired(state, coord) {
		return false
	}
	if _, loaded := state.LoadedSectorProxies[coord]; loaded {
		return false
	}
	sector, exists := state.ImportedWorldSectors[coord]
	return exists && len(sector.LODs) > 0 && streamedSectorProxyCommitNeeded(state, coord)
}

// Observe withdrawal, policy and loaded satisfaction before persistence or a
// residency upgrade can remove the satisfying owner later in this update.
func advanceStreamedPreparationSchedule(state *StreamedLevelRuntimeState) {
	scheduler := &state.prepareScheduler
	scheduler.sequence++
	if scheduler.sequence == 0 {
		// Sequence overflow begins a fresh era; old births cannot be compared.
		*scheduler = streamedPrepareScheduler{sequence: 1}
	}
	for identity := range scheduler.waiting {
		if !streamedPreparationNeeded(state, identity) {
			delete(scheduler.waiting, identity)
		}
	}
	if len(scheduler.waiting) == 0 {
		scheduler.waiting = nil
	}
}

func streamedPreparationPriority(state *StreamedLevelRuntimeState, identity streamedPrepareIdentity) int {
	if identity.kind == streamedPrepareProxy {
		return 0
	}
	coord := identity.coord
	if _, collision := state.CollisionChunks[coord]; collision {
		return 1
	}
	if _, destruction := state.DestructionChunks[coord]; destruction {
		return 1
	}
	if owner := state.observerSelection; owner != nil {
		if owner.raw[streamedSelectionCurrent][coord] > 0 {
			return 2
		}
		if sector, imported := state.ImportedChunkSector[coord]; imported && owner.currentSectors[sector] > 0 {
			return 2
		}
	}
	return 3
}

func streamedPreparationDispatchEligible(state *StreamedLevelRuntimeState, identity streamedPrepareIdentity) bool {
	if !streamedPreparationNeeded(state, identity) {
		return false
	}
	var cost int64
	if identity.kind == streamedPrepareFull {
		if activeStreamedChunkCommit(state, identity.coord) != nil {
			return false
		}
		if _, pending := state.PendingLoads[identity.coord]; pending {
			return false
		}
		cost = state.pendingChunkCostHints[identity.coord]
	} else {
		if _, pending := state.PendingProxyLoads[identity.coord]; pending {
			return false
		}
		cost = state.pendingProxyCostHints[identity.coord]
	}
	return cost <= 0 || state.pendingPrepared.canReserve(cost)
}

func scheduleStreamedPreparation(cmd *Commands, state *StreamedLevelRuntimeState) {
	scheduler := &state.prepareScheduler
	active := streamedActivePrepareJobCounts(state)
	limit := streamedMaxPrepareJobs(state)
	var queue streamedPrepareQueue
	retain := func(identity streamedPrepareIdentity) {
		if !streamedPreparationNeeded(state, identity) {
			delete(scheduler.waiting, identity)
			return
		}
		birth, waiting := scheduler.waiting[identity]
		if !waiting {
			if scheduler.waiting == nil {
				scheduler.waiting = make(map[streamedPrepareIdentity]uint64)
			}
			birth = scheduler.sequence
			scheduler.waiting[identity] = birth
		}
		// Retain age even when no worker is free, but avoid candidate storage.
		if active >= limit || !streamedPreparationDispatchEligible(state, identity) {
			return
		}
		priority := streamedPreparationPriority(state, identity)
		promotion := min(uint64(priority), (scheduler.sequence-birth)/8)
		queue = append(queue, streamedPrepareCandidate{identity: identity, priority: priority - int(promotion), birth: birth})
	}
	for coord := range state.DesiredProxySectors {
		if !state.Config.DisableSectorProxies {
			if _, loaded := state.LoadedSectorProxies[coord]; loaded {
				reconcileStreamedSectorProxyAfterFullCommit(cmd, state, coord)
			}
		}
		retain(streamedPrepareIdentity{coord: coord, kind: streamedPrepareProxy})
	}
	for coord := range state.DesiredChunks {
		retain(streamedPrepareIdentity{coord: coord, kind: streamedPrepareFull})
	}
	heap.Init(&queue)
	for active < limit && queue.Len() > 0 {
		identity := heap.Pop(&queue).(streamedPrepareCandidate).identity
		// Workers can change byte pressure during queue construction. Recheck
		// admission before capturing the selected job's current overrides.
		if !streamedPreparationDispatchEligible(state, identity) {
			continue
		}
		if state.streamingWork.currentCount >= streamedWorkLimit(state) {
			state.streamingWork.blocked++
			break
		}
		coord := identity.coord
		if identity.kind == streamedPrepareFull {
			delete(state.pendingChunkCostHints, coord)
			state.PendingLoads[coord] = struct{}{}
			startStreamedChunkPrepareJob(state, buildStreamedChunkLoadJob(state, coord))
		} else {
			delete(state.pendingProxyCostHints, coord)
			state.PendingProxyLoads[coord] = struct{}{}
			lod := state.ImportedWorldSectors[coord].LODs[0]
			startStreamedSectorProxyPrepareJob(state, buildStreamedSectorProxyLoadJob(state, coord, lod))
		}
		active++
	}
}

func recordStreamedPreparationDispatch(state *StreamedLevelRuntimeState, coord ChunkCoord, kind string) {
	state.Metrics.PrepareDispatchCount++
	state.Metrics.LastPrepareDispatchCoord = coord
	state.Metrics.LastPrepareDispatchKind = kind
}
