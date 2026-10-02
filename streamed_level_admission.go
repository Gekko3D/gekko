package gekko

// Admission is main-thread owned. Records contain only attempt identities and
// renderer tickets; workers, decoded payloads and ECS rows remain elsewhere.
type streamedWorkTarget struct {
	ticket, generation uint64
	done, removed      bool
}

type streamedWorkItem struct {
	generation uint64
	cancel     <-chan struct{}
	cpu        bool
	carryover  bool
	targets    map[EntityId]streamedWorkTarget
	fences     map[uint64]struct{}
}

type streamedWorkOwner struct {
	nextID         uint64
	active         uint64
	committing     bool
	blocked        uint64
	currentCount   int
	carryoverCount int
	items          map[uint64]*streamedWorkItem
	attempts       map[<-chan struct{}]uint64
	targets        map[EntityId]uint64
}

func streamedWorkLimit(state *StreamedLevelRuntimeState) int {
	if state.Config.MaxStreamingWorkItems > 0 {
		return state.Config.MaxStreamingWorkItems
	}
	return 32
}

func (owner *streamedWorkOwner) create(generation uint64) uint64 {
	owner.nextID++
	if owner.nextID == 0 {
		panic("streaming work identity space exhausted")
	}
	if owner.items == nil {
		owner.items = make(map[uint64]*streamedWorkItem)
	}
	owner.items[owner.nextID] = &streamedWorkItem{generation: generation, cpu: true}
	owner.currentCount++
	return owner.nextID
}

func acquireStreamedWorkAttempt(state *StreamedLevelRuntimeState, generation uint64, cancel <-chan struct{}) {
	owner := &state.streamingWork
	id := owner.create(generation)
	item := owner.items[id]
	item.cancel = cancel
	if owner.attempts == nil {
		owner.attempts = make(map[<-chan struct{}]uint64)
	}
	owner.attempts[cancel] = id
}

func (owner *streamedWorkOwner) reap(id uint64) {
	item := owner.items[id]
	if item == nil || item.cpu || len(item.fences) != 0 {
		return
	}
	for _, target := range item.targets {
		if !target.done {
			return
		}
	}
	delete(owner.items, id)
	if item.carryover {
		owner.carryoverCount--
	} else {
		owner.currentCount--
	}
}

// Only the exact dispatch token and generation can end CPU ownership. A stale
// acknowledgement cannot release a newer attempt at the same coordinate.
func finishStreamedWorkAttempt(state *StreamedLevelRuntimeState, generation uint64, cancel <-chan struct{}) {
	owner := &state.streamingWork
	id := owner.attempts[cancel]
	item := owner.items[id]
	if item == nil || item.generation != generation || item.cancel != cancel {
		return
	}
	delete(owner.attempts, cancel)
	item.cpu = false
	owner.reap(id)
}

// A commit groups all initial runtime-owned terrain/imported/proxy targets in
// one item, including targets flushed before a later placement/hook failure.
func beginStreamedWorkCommit(state *StreamedLevelRuntimeState, generation uint64, cancel <-chan struct{}) func() {
	owner := &state.streamingWork
	id := owner.attempts[cancel]
	item := owner.items[id]
	attempt := item != nil && item.generation == generation
	if !attempt {
		id = 0
	}
	previous, wasCommitting := owner.active, owner.committing
	owner.active, owner.committing = id, true
	return func() {
		id = owner.active
		owner.active, owner.committing = previous, wasCommitting
		if attempt {
			finishStreamedWorkAttempt(state, generation, cancel)
		} else if id != 0 {
			owner.items[id].cpu = false
			owner.reap(id)
		}
	}
}

func attachStreamedWorkTarget(state *StreamedLevelRuntimeState, entity EntityId, target streamedRenderTarget) {
	owner := &state.streamingWork
	id := owner.active
	standalone := !owner.committing
	if id == 0 {
		id = owner.create(state.Generation)
		if owner.committing {
			owner.active = id
		}
	}
	item := owner.items[id]
	if item.targets == nil {
		item.targets = make(map[EntityId]streamedWorkTarget)
	}
	if owner.targets == nil {
		owner.targets = make(map[EntityId]uint64)
	}
	item.targets[entity] = streamedWorkTarget{ticket: target.ticket, generation: target.generation}
	owner.targets[entity] = id
	if standalone {
		item.cpu = false
	}
}

func replaceStreamedWorkTarget(state *StreamedLevelRuntimeState, entity EntityId, old, next streamedRenderTarget) {
	owner := &state.streamingWork
	id := owner.targets[entity]
	item := owner.items[id]
	if item == nil {
		// Terminal failure was already latched. Repair is fresh compatibility
		// work, even when another target of the old item remains unfinished.
		attachStreamedWorkTarget(state, entity, next)
		return
	}
	if item.fences == nil {
		item.fences = make(map[uint64]struct{})
	}
	item.fences[old.ticket] = struct{}{}
	item.targets[entity] = streamedWorkTarget{ticket: next.ticket, generation: next.generation}
}

func retireStreamedWorkTarget(state *StreamedLevelRuntimeState, entity EntityId) {
	owner := &state.streamingWork
	if item := owner.items[owner.targets[entity]]; item != nil {
		target := item.targets[entity]
		target.removed = true
		item.targets[entity] = target
		delete(owner.targets, entity)
	}
}

// Poll before automatic reticketing so qualified Failed is terminal for its
// initial item. Ready latches only against the live current ownership tuple.
func observeStreamedWorkTargets(cmd *Commands, state *StreamedLevelRuntimeState, markers map[EntityId]StreamedVoxelRenderComponent) {
	owner := &state.streamingWork
	renderer := voxelRtStateFromApp(cmd.app)
	if renderer == nil {
		return
	}
	for id, item := range owner.items {
		for entity, target := range item.targets {
			if target.done || target.removed || item.carryover {
				continue
			}
			live, owned := state.renderTargets[entity]
			marker, marked := markers[entity]
			if !owned || !marked || !cmd.EntityExists(entity) || target.generation != state.Generation ||
				live.ticket != target.ticket || live.generation != target.generation || marker.Ticket != target.ticket || marker.Generation != target.generation {
				continue
			}
			status, known := renderer.StreamedVoxelStatus(target.ticket)
			if known && status.Entity == entity && status.Generation == target.generation &&
				(status.State == StreamedVoxelRenderReady || status.State == StreamedVoxelRenderFailed) {
				target.done = true
				item.targets[entity] = target
				if owner.targets[entity] == id {
					delete(owner.targets, entity)
				}
			}
		}
		owner.reap(id)
	}
}

// The S1c retirement owner supplies proof, after observing marker absence and
// a terminal or forgotten renderer ticket. Missing renderer proves nothing.
func reapStreamedWorkRetirement(cmd *Commands, state *StreamedLevelRuntimeState, markers map[EntityId]StreamedVoxelRenderComponent) {
	if voxelRtStateFromApp(cmd.app) == nil {
		return
	}
	live := make(map[uint64]struct{}, len(markers))
	for _, marker := range markers {
		live[marker.Ticket] = struct{}{}
	}
	retired := func(ticket uint64) bool {
		_, marked := live[ticket]
		_, pending := state.retiredRenderIDs[ticket]
		return !marked && !pending
	}
	owner := &state.streamingWork
	for id, item := range owner.items {
		for ticket := range item.fences {
			if retired(ticket) {
				delete(item.fences, ticket)
			}
		}
		for entity, target := range item.targets {
			if !target.done && target.removed && retired(target.ticket) {
				target.done = true
				item.targets[entity] = target
			}
		}
		owner.reap(id)
	}
}

func carryStreamedWorkAfterStop(state *StreamedLevelRuntimeState) {
	owner := &state.streamingWork
	for _, item := range owner.items {
		if item.carryover {
			continue
		}
		item.carryover = true
		owner.currentCount--
		owner.carryoverCount++
	}
}

func refreshStreamedWorkMetrics(state *StreamedLevelRuntimeState) {
	owner := &state.streamingWork
	limit := streamedWorkLimit(state)
	state.Metrics.StreamingWorkCount = owner.currentCount
	state.Metrics.StreamingWorkMaxCount = limit
	state.Metrics.StreamingWorkOverBudgetCount = max(0, owner.currentCount-limit)
	state.Metrics.StreamingWorkCarryoverCount = owner.carryoverCount
	state.Metrics.StreamingWorkAdmissionBlockedCount = owner.blocked
}
