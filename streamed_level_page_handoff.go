package gekko

import (
	"fmt"
	"math"
	"reflect"
	"sort"
)

// StreamedPageCoverageTransition identifies the complete replacement group whose
// collision publication must accompany this proposed visual transition.
type StreamedPageCoverageTransition struct {
	GroupID  string
	Activate bool
}

// StreamedPageCoverageStatus is publisher-owned collision proof for one exact plan.
type StreamedPageCoverageStatus struct {
	GroupID                  string
	Generation, PlanRevision uint64
	Ready                    bool
	Failure                  string
}
type StreamedPageHandoffBlock struct {
	Key    StreamedPageKey
	Reason string
}

// StreamedPageHandoffPlan is detached logical intent. It never publishes native
// visibility or collision; a future publisher validates, publishes, then accepts.
type StreamedPageHandoffPlan struct {
	Generation, Revision                uint64
	Before, Visible, Show, Hide, Retain []StreamedPageKey
	Requests                            []StreamedPageRequest
	CoverageGroups                      []StreamedPageCoverageTransition
	Blocked                             []StreamedPageHandoffBlock
}
type streamedPageHandoffOwner struct {
	frontier []StreamedPageKey
	pending  *streamedPageHandoffPending
	groups   []streamedPageHandoffGroup
}
type streamedPageHandoffPending struct {
	plan                               StreamedPageHandoffPlan
	selectionRevision, bindingRevision uint64
	bindings                           map[StreamedPageKey]streamedPageBinding
}

func cloneHandoffValues[T any](v []T) []T {
	if v == nil {
		return nil
	}
	return append([]T{}, v...)
}
func clonePageHandoffPlan(p StreamedPageHandoffPlan) StreamedPageHandoffPlan {
	p.Before = cloneHandoffValues(p.Before)
	p.Visible = cloneHandoffValues(p.Visible)
	p.Show = cloneHandoffValues(p.Show)
	p.Hide = cloneHandoffValues(p.Hide)
	p.Retain = cloneHandoffValues(p.Retain)
	p.Requests = cloneHandoffValues(p.Requests)
	p.CoverageGroups = cloneHandoffValues(p.CoverageGroups)
	p.Blocked = cloneHandoffValues(p.Blocked)
	return p
}
func handoffKeySet(keys []StreamedPageKey) map[StreamedPageKey]struct{} {
	out := make(map[StreamedPageKey]struct{}, len(keys))
	for _, key := range keys {
		out[key] = struct{}{}
	}
	return out
}
func (c *streamedPageControl) pageParent(key StreamedPageKey) (StreamedPageKey, bool) {
	layer := c.layer(key.Layer)
	parent := layer.Forest.ParentPageIndices[key.PageIndex]
	if parent < 0 {
		return StreamedPageKey{}, false
	}
	return streamedPageKey(key.Layer, layer, uint32(parent)), true
}
func (c *streamedPageControl) pageRoot(key StreamedPageKey) StreamedPageKey {
	for {
		parent, ok := c.pageParent(key)
		if !ok {
			return key
		}
		key = parent
	}
}
func (c *streamedPageControl) pageDescends(key, ancestor StreamedPageKey) bool {
	if key.Layer != ancestor.Layer {
		return false
	}
	for {
		if key == ancestor {
			return true
		}
		parent, ok := c.pageParent(key)
		if !ok {
			return false
		}
		key = parent
	}
}
func (c *streamedPageControl) pageChildren(key StreamedPageKey) []StreamedPageKey {
	layer := c.layer(key.Layer)
	out := make([]StreamedPageKey, 0, len(layer.Pages[key.PageIndex].ChildPageIndices))
	for _, i := range layer.Pages[key.PageIndex].ChildPageIndices {
		out = append(out, streamedPageKey(key.Layer, layer, i))
	}
	sortStreamedPageKeys(out)
	return out
}
func (c *streamedPageControl) pageReady(cmd *Commands, renderer *VoxelRtState, key StreamedPageKey) (bool, string) {
	binding, ok := c.startup.bindings[key]
	if !ok {
		return false, "page has no renderer binding"
	}
	if !cmd.EntityExists(binding.entity) {
		return false, "bound page entity is absent"
	}
	marker, ok := cmd.GetComponent(binding.entity, reflect.TypeOf(StreamedVoxelRenderComponent{})).(*StreamedVoxelRenderComponent)
	if !ok || marker == nil || marker.Ticket != binding.ticket || marker.Generation != c.generation {
		return false, "page marker identity is absent or stale"
	}
	status, known := renderer.StreamedVoxelStatus(binding.ticket)
	if !known || status.Entity != binding.entity || status.Generation != c.generation {
		return false, "renderer page identity is absent or stale"
	}
	if status.State != StreamedVoxelRenderReady {
		if status.Failure != "" {
			return false, status.Failure
		}
		return false, fmt.Sprintf("renderer page state %d is not ready", status.State)
	}
	return true, ""
}
func (c *streamedPageControl) frontierSubtree(frontier []StreamedPageKey, node StreamedPageKey) []StreamedPageKey {
	var out []StreamedPageKey
	for _, key := range frontier {
		if c.pageDescends(key, node) {
			out = append(out, key)
		}
	}
	return out
}
func (c *streamedPageControl) proposePage(cmd *Commands, renderer *VoxelRtState, node StreamedPageKey, before []StreamedPageKey, visible, desired map[StreamedPageKey]struct{}, blocks map[StreamedPageKey]string) []StreamedPageKey {
	children := c.pageChildren(node)
	wantsChildren := false
	for _, key := range children {
		if _, ok := desired[key]; ok {
			wantsChildren = true
			break
		}
	}
	if _, shown := visible[node]; shown {
		if !wantsChildren || len(children) == 0 {
			return []StreamedPageKey{node}
		}
		complete := true
		for _, child := range children {
			if ready, reason := c.pageReady(cmd, renderer, child); !ready {
				complete = false
				blocks[child] = reason
			}
		}
		if complete {
			return children
		}
		return []StreamedPageKey{node}
	}
	if !wantsChildren {
		if ready, reason := c.pageReady(cmd, renderer, node); ready {
			return []StreamedPageKey{node}
		} else {
			blocks[node] = reason
			return c.frontierSubtree(before, node)
		}
	}
	var out []StreamedPageKey
	for _, child := range children {
		out = append(out, c.proposePage(cmd, renderer, child, before, visible, desired, blocks)...)
	}
	return out
}

// PageVisibleFrontier returns the acknowledged logical frontier, never pending
// visibility. Reset or a stale runtime generation invalidates the accessor.
func (state *StreamedLevelRuntimeState) PageVisibleFrontier() ([]StreamedPageKey, error) {
	c, err := state.currentPageControl()
	if err != nil {
		return nil, err
	}
	return cloneHandoffValues(c.handoff.frontier), nil
}

// PlanPageHandoff proposes one level of refinement per current visible parent,
// or a complete subtree coarsening. Planning does not acknowledge publication.
func (state *StreamedLevelRuntimeState) PlanPageHandoff(cmd *Commands, renderer *VoxelRtState) (StreamedPageHandoffPlan, error) {
	c, err := state.currentPageControl()
	if err != nil {
		return StreamedPageHandoffPlan{}, err
	}
	if !c.selected || cmd == nil || cmd.app == nil || renderer == nil {
		return StreamedPageHandoffPlan{}, fmt.Errorf("handoff requires selection, commands and renderer evidence")
	}
	if state.nextPageHandoffRevision == math.MaxUint64 {
		return StreamedPageHandoffPlan{}, fmt.Errorf("handoff plan revision exhausted")
	}
	groups := c.handoff.groups
	desired := c.handoffEffectiveDemand(groups)
	before := cloneHandoffValues(c.handoff.frontier)
	beforeSet := handoffKeySet(before)
	blocks := map[StreamedPageKey]string{}
	proposals := make(map[StreamedPageKey][]StreamedPageKey, len(c.roots))
	bootstrap := len(before) == 0
	if bootstrap {
		complete := true
		for _, root := range c.roots {
			if ready, reason := c.pageReady(cmd, renderer, root); !ready {
				complete = false
				blocks[root] = reason
			}
		}
		for _, root := range c.roots {
			if complete {
				proposals[root] = []StreamedPageKey{root}
			}
		}
	} else {
		for _, root := range c.roots {
			proposals[root] = c.proposePage(cmd, renderer, root, before, beforeSet, desired, blocks)
		}
	}
	c.handoffRollbackGroups(groups, before, proposals, blocks)
	proposed := c.handoffFlatten(proposals)
	if bootstrap && len(proposed) != len(c.roots) {
		proposed = nil
	}
	for _, key := range proposed {
		if ready, reason := c.pageReady(cmd, renderer, key); !ready {
			blocks[key] = reason
		}
	}
	afterSet := handoffKeySet(proposed)
	plan := StreamedPageHandoffPlan{Generation: c.generation, Revision: state.nextPageHandoffRevision + 1, Before: before, Visible: proposed}
	for _, key := range proposed {
		if _, ok := beforeSet[key]; !ok {
			plan.Show = append(plan.Show, key)
		}
	}
	for _, key := range before {
		if _, ok := afterSet[key]; !ok {
			plan.Hide = append(plan.Hide, key)
		}
	}
	plan.CoverageGroups = handoffGroupTransitions(groups, beforeSet, afterSet)
	retain := handoffKeySet(c.selection.Keep)
	unionStreamedPageSets(retain, handoffKeySet(c.selection.Desired))
	unionStreamedPageSets(retain, handoffKeySet(c.selection.Prefetch))
	unionStreamedPageSets(retain, desired)
	unionStreamedPageSets(retain, beforeSet)
	unionStreamedPageSets(retain, afterSet)
	for _, key := range streamedPageKeySetKeys(retain) {
		for parent, ok := c.pageParent(key); ok; parent, ok = c.pageParent(parent) {
			retain[parent] = struct{}{}
		}
	}
	for _, root := range c.roots {
		retain[root] = struct{}{}
	}
	plan.Retain = streamedPageKeySetKeys(retain)
	requests := map[StreamedPageKey]StreamedVoxelPriority{}
	for _, r := range c.selection.Requests {
		requests[r.Key] = r.Priority
	}
	roots := handoffKeySet(c.roots)
	for key := range desired {
		priority := StreamedVoxelPriorityVisible
		if _, root := roots[key]; root {
			priority = StreamedVoxelPriorityFallback
		}
		if prior, ok := requests[key]; !ok || priority < prior {
			requests[key] = priority
		}
	}
	for key, priority := range requests {
		plan.Requests = append(plan.Requests, StreamedPageRequest{Key: key, Priority: priority})
	}
	sort.Slice(plan.Requests, func(i, j int) bool {
		a, b := plan.Requests[i], plan.Requests[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		return streamedPageKeyLess(a.Key, b.Key)
	})
	for _, key := range streamedPageKeySetKeys(handoffKeySetMap(blocks)) {
		plan.Blocked = append(plan.Blocked, StreamedPageHandoffBlock{Key: key, Reason: blocks[key]})
	}
	captured := make(map[StreamedPageKey]streamedPageBinding, len(proposed))
	for _, key := range proposed {
		captured[key] = c.startup.bindings[key]
	}
	pending := &streamedPageHandoffPending{plan: clonePageHandoffPlan(plan), selectionRevision: c.selection.BuildCount, bindingRevision: c.startup.revision, bindings: captured}
	state.nextPageHandoffRevision = plan.Revision
	c.handoff.pending = pending
	return clonePageHandoffPlan(plan), nil
}
func handoffKeySetMap(m map[StreamedPageKey]string) map[StreamedPageKey]struct{} {
	out := map[StreamedPageKey]struct{}{}
	for key := range m {
		out[key] = struct{}{}
	}
	return out
}

// ValidatePageHandoff is poll-only prepublication validation. A publisher must
// perform its same-stage transaction and acknowledge without intervening changes.
func (state *StreamedLevelRuntimeState) ValidatePageHandoff(cmd *Commands, renderer *VoxelRtState, plan StreamedPageHandoffPlan, collision []StreamedPageCoverageStatus) error {
	c, err := state.currentPageControl()
	if err != nil {
		return err
	}
	pending := c.handoff.pending
	if pending == nil || cmd == nil || cmd.app == nil || renderer == nil {
		return fmt.Errorf("handoff pending plan or evidence is absent")
	}
	if !reflect.DeepEqual(plan, pending.plan) || plan.Generation != c.generation || pending.selectionRevision != c.selection.BuildCount || pending.bindingRevision != c.startup.revision {
		return fmt.Errorf("handoff plan is modified or stale")
	}
	for _, key := range plan.Visible {
		if c.startup.bindings[key] != pending.bindings[key] {
			return fmt.Errorf("handoff binding changed")
		}
		if ready, reason := c.pageReady(cmd, renderer, key); !ready {
			return fmt.Errorf("handoff target %d/%d: %s", key.Layer, key.PageIndex, reason)
		}
	}
	requirements := make(map[string]struct{}, len(plan.CoverageGroups))
	for _, g := range plan.CoverageGroups {
		requirements[g.GroupID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(collision))
	for _, proof := range collision {
		if _, known := requirements[proof.GroupID]; !known {
			return fmt.Errorf("unknown handoff collision group")
		}
		if _, duplicate := seen[proof.GroupID]; duplicate {
			return fmt.Errorf("duplicate handoff collision proof")
		}
		seen[proof.GroupID] = struct{}{}
		if proof.Generation != plan.Generation || proof.PlanRevision != plan.Revision || !proof.Ready || proof.Failure != "" {
			return fmt.Errorf("handoff collision group is not current and ready")
		}
	}
	if len(seen) != len(requirements) {
		return fmt.Errorf("handoff collision proof is incomplete")
	}
	return nil
}

// AcceptPageHandoff acknowledges logical publication only. It cannot implement
// native visibility/collision atomicity or bypass the separate spawn gate.
func (state *StreamedLevelRuntimeState) AcceptPageHandoff(cmd *Commands, renderer *VoxelRtState, plan StreamedPageHandoffPlan, collision []StreamedPageCoverageStatus) error {
	if err := state.ValidatePageHandoff(cmd, renderer, plan, collision); err != nil {
		return err
	}
	c := state.pageControl
	c.handoff.frontier = cloneHandoffValues(c.handoff.pending.plan.Visible)
	c.handoff.pending = nil
	return nil
}
