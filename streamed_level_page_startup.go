package gekko

import (
	"fmt"
	"reflect"
)

// StreamedPageSpawnCollisionStatus is explicit evidence from the future spawn
// collision owner. Visual readiness alone never authorizes player startup.
type StreamedPageSpawnCollisionStatus struct {
	Generation uint64
	Ready      bool
	Failure    string
}

// StreamedPageStartupStatus is a current, poll-only reduction of required root
// rendering and spawn collision evidence. Eligibility is never latched.
type StreamedPageStartupStatus struct {
	Generation                uint64
	RequiredRoots, ReadyRoots int
	CollisionReady, Eligible  bool
	Failure                   string
}

type streamedPageBinding struct {
	entity EntityId
	ticket uint64
}

type streamedPageGate struct {
	bindings    map[StreamedPageKey]streamedPageBinding
	ticketPages map[uint64]StreamedPageKey
	entityPages map[EntityId]StreamedPageKey
}

// BindPageRenderTicket records renderer identity for a valid current page. It
// neither changes renderer status nor takes ownership of entities or tickets.
func (state *StreamedLevelRuntimeState) BindPageRenderTicket(key StreamedPageKey, entity EntityId, ticket uint64) error {
	control, err := state.currentPageControl()
	if err != nil {
		return err
	}
	if _, ok := control.pages[key]; !ok {
		return fmt.Errorf("unknown current streamed page key")
	}
	if entity == 0 || ticket == 0 {
		return fmt.Errorf("page binding requires nonzero entity and ticket")
	}
	gate := &control.startup
	if other, ok := gate.ticketPages[ticket]; ok && other != key {
		return fmt.Errorf("renderer ticket already bound to another page")
	}
	if other, ok := gate.entityPages[entity]; ok && other != key {
		return fmt.Errorf("renderer entity already bound to another page")
	}
	if previous, ok := gate.bindings[key]; ok {
		if previous.entity == entity && previous.ticket == ticket {
			return nil
		}
		delete(gate.ticketPages, previous.ticket)
		delete(gate.entityPages, previous.entity)
	}
	if gate.bindings == nil {
		gate.bindings = make(map[StreamedPageKey]streamedPageBinding)
		gate.ticketPages = make(map[uint64]StreamedPageKey)
		gate.entityPages = make(map[EntityId]StreamedPageKey)
	}
	gate.bindings[key] = streamedPageBinding{entity: entity, ticket: ticket}
	gate.ticketPages[ticket] = key
	gate.entityPages[entity] = key
	return nil
}

// PageStartupStatus observes committed ECS markers and exact renderer identity
// for all pinned roots. It does not publish entities, change visibility, mutate
// renderer records, or enforce a timeout. Every poll can revoke eligibility.
func (state *StreamedLevelRuntimeState) PageStartupStatus(cmd *Commands, renderer *VoxelRtState, collision StreamedPageSpawnCollisionStatus) StreamedPageStartupStatus {
	var result StreamedPageStartupStatus
	if state != nil {
		result.Generation = state.Generation
	}
	control, err := state.currentPageControl()
	if err != nil {
		return result
	}
	result.Generation = control.generation
	result.RequiredRoots = len(control.roots)
	for _, key := range control.roots {
		binding, bound := control.startup.bindings[key]
		if !bound || !cmd.EntityExists(binding.entity) {
			continue
		}
		marker, ok := cmd.GetComponent(binding.entity, reflect.TypeOf(StreamedVoxelRenderComponent{})).(*StreamedVoxelRenderComponent)
		if !ok || marker == nil || marker.Ticket != binding.ticket || marker.Generation != control.generation {
			continue
		}
		status, known := renderer.StreamedVoxelStatus(binding.ticket)
		if !known || status.Entity != binding.entity || status.Generation != control.generation {
			continue
		}
		switch status.State {
		case StreamedVoxelRenderReady:
			result.ReadyRoots++
		case StreamedVoxelRenderFailed, StreamedVoxelRenderCancelled:
			if result.Failure == "" {
				result.Failure = status.Failure
				if result.Failure == "" {
					result.Failure = fmt.Sprintf("streamed root %d/%d renderer state %d", key.Layer, key.PageIndex, status.State)
				}
			}
		}
	}
	if collision.Generation == control.generation {
		result.CollisionReady = collision.Ready && collision.Failure == ""
		if result.Failure == "" && collision.Failure != "" {
			result.Failure = collision.Failure
		}
	}
	result.Eligible = result.RequiredRoots > 0 && result.ReadyRoots == result.RequiredRoots && result.CollisionReady && result.Failure == ""
	return result
}
