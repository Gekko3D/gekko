package gekko

import (
	"fmt"
	"reflect"
	"sort"
)

type streamedPageHandoffGroup struct {
	id        string
	members   []StreamedPageKey
	supported bool
}

func (c *streamedPageControl) handoffGroups() []streamedPageHandoffGroup {
	byID := map[string][]StreamedPageKey{}
	for key, page := range c.pages {
		if page.CoverageGroup != "" {
			byID[page.CoverageGroup] = append(byID[page.CoverageGroup], key)
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	groups := make([]streamedPageHandoffGroup, 0, len(ids))
	for _, id := range ids {
		members := byID[id]
		sortStreamedPageKeys(members)
		supported := true
		for _, key := range members {
			if _, hasParent := c.pageParent(key); !hasParent || len(c.pages[key].ChildPageIndices) != 0 {
				supported = false
				break
			}
		}
		// Distinct terminal members of a validated forest cannot be ancestors.
		groups = append(groups, streamedPageHandoffGroup{id: id, members: members, supported: supported})
	}
	return groups
}
func (c *streamedPageControl) handoffEffectiveDemand(groups []streamedPageHandoffGroup) map[StreamedPageKey]struct{} {
	desired := handoffKeySet(c.selection.Desired)
	for {
		count := len(desired)
		for _, g := range groups {
			touched := false
			for _, key := range g.members {
				if _, ok := desired[key]; ok {
					touched = true
					break
				}
			}
			if touched {
				for _, key := range g.members {
					desired[key] = struct{}{}
				}
			}
		}
		desired = c.coverageClosure(desired)
		if len(desired) == count {
			return desired
		}
	}
}
func (c *streamedPageControl) handoffFlatten(proposals map[StreamedPageKey][]StreamedPageKey) []StreamedPageKey {
	var out []StreamedPageKey
	for _, root := range c.roots {
		out = append(out, proposals[root]...)
	}
	sortStreamedPageKeys(out)
	return out
}
func handoffGroupCount(g streamedPageHandoffGroup, set map[StreamedPageKey]struct{}) int {
	n := 0
	for _, key := range g.members {
		if _, ok := set[key]; ok {
			n++
		}
	}
	return n
}
func (c *streamedPageControl) handoffRollbackGroups(groups []streamedPageHandoffGroup, before []StreamedPageKey, proposals map[StreamedPageKey][]StreamedPageKey, blocks map[StreamedPageKey]string) {
	beforeSet := handoffKeySet(before)
	for {
		changed := false
		for _, g := range groups {
			candidate := c.handoffFlatten(proposals)
			afterSet := handoffKeySet(candidate)
			rollback := map[StreamedPageKey]struct{}{}
			reason := ""
			if !g.supported {
				for _, member := range g.members {
					if !reflect.DeepEqual(c.frontierSubtree(before, member), c.frontierSubtree(candidate, member)) {
						rollback[c.pageRoot(member)] = struct{}{}
					}
				}
				if len(rollback) > 0 {
					reason = fmt.Sprintf("unsupported coverage group %q requires non-root terminal members", g.id)
				}
			} else if count := handoffGroupCount(g, afterSet); count != 0 && count != len(g.members) {
				reason = fmt.Sprintf("coverage group %q requires its complete cohort", g.id)
				for _, member := range g.members {
					_, was := beforeSet[member]
					_, will := afterSet[member]
					if was != will {
						rollback[c.pageRoot(member)] = struct{}{}
					}
				}
			}
			if reason != "" {
				for _, member := range g.members {
					if _, exists := blocks[member]; !exists {
						blocks[member] = reason
					}
				}
			}
			for _, root := range streamedPageKeySetKeys(rollback) {
				old := c.frontierSubtree(before, root)
				if !reflect.DeepEqual(proposals[root], old) {
					proposals[root] = old
					changed = true
				}
			}
		}
		if !changed {
			return
		}
	}
}
func handoffGroupTransitions(groups []streamedPageHandoffGroup, before, after map[StreamedPageKey]struct{}) []StreamedPageCoverageTransition {
	var transitions []StreamedPageCoverageTransition
	for _, g := range groups {
		if !g.supported {
			continue
		}
		was := handoffGroupCount(g, before) == len(g.members)
		will := handoffGroupCount(g, after) == len(g.members)
		if was != will {
			transitions = append(transitions, StreamedPageCoverageTransition{GroupID: g.id, Activate: will})
		}
	}
	return transitions
}
