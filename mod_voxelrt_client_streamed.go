package gekko

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// StreamedVoxelPriority is scheduling metadata; lower values upload first.
type StreamedVoxelPriority uint8

const (
	StreamedVoxelPriorityFallback StreamedVoxelPriority = iota
	StreamedVoxelPriorityCollision
	StreamedVoxelPriorityVisible
	StreamedVoxelPriorityPrefetch
	StreamedVoxelPriorityKeep
)

// StreamedVoxelRenderComponent keeps an entity resident while hidden. Its owner
// supplies nonzero, unique, monotonically increasing tickets. Generation is
// supplied by the owner and returned unchanged.
type StreamedVoxelRenderComponent struct {
	Ticket     uint64
	Generation uint64
	Priority   StreamedVoxelPriority
}

type StreamedVoxelRenderState uint8

const (
	StreamedVoxelRenderPendingBridge StreamedVoxelRenderState = iota
	StreamedVoxelRenderUploading
	StreamedVoxelRenderReady
	StreamedVoxelRenderCancelled
	StreamedVoxelRenderFailed
)

type StreamedVoxelRenderStatus struct {
	State          StreamedVoxelRenderState
	Entity         EntityId
	Generation     uint64
	MapID          uint32
	TargetRevision uint64
	PendingSectors int
	PendingBricks  int
	Failure        string
}

type streamedVoxelTicket struct {
	status    StreamedVoxelRenderStatus
	object    *core.VoxelObject
	mapTarget *volume.XBrickMap
}

func (record *streamedVoxelTicket) finish(state StreamedVoxelRenderState) {
	record.status.State = state
	// Terminal diagnostics need only value metadata. Release captured handles so
	// retaining the status does not keep retired renderer objects/geometry alive.
	record.object, record.mapTarget = nil, nil
}

func (s *VoxelRtState) StreamedVoxelStatus(ticket uint64) (StreamedVoxelRenderStatus, bool) {
	if s != nil {
		if record := s.streamedVoxelTickets[ticket]; record != nil {
			return record.status, true
		}
	}
	return StreamedVoxelRenderStatus{}, false
}

// ForgetStreamedVoxel only releases terminal records. The owner removes or
// changes the terminal marker before forgetting it.
func (s *VoxelRtState) ForgetStreamedVoxel(ticket uint64) {
	if s == nil {
		return
	}
	if record := s.streamedVoxelTickets[ticket]; record != nil && record.status.State >= StreamedVoxelRenderReady {
		delete(s.streamedVoxelTickets, ticket)
	}
}

func (s *VoxelRtState) streamedVoxelTargetCurrent(record *streamedVoxelTicket) bool {
	obj := s.instanceMap[record.status.Entity]
	return obj != nil && obj == record.object && obj.XBrickMap != nil &&
		obj.XBrickMap == record.mapTarget && obj.XBrickMap.ID == record.status.MapID &&
		obj.XBrickMap.Revision == record.status.TargetRevision
}

// Snapshot values rather than retaining pointers into movable ECS storage.
func (s *VoxelRtState) beginStreamedVoxelSync(cmd *Commands) map[EntityId]StreamedVoxelRenderComponent {
	markers := make(map[EntityId]StreamedVoxelRenderComponent)
	MakeQuery1[StreamedVoxelRenderComponent](cmd).Map(func(entity EntityId, marker *StreamedVoxelRenderComponent) bool {
		markers[entity] = *marker
		return true
	})
	if s.streamedVoxelTickets == nil {
		s.streamedVoxelTickets = make(map[uint64]*streamedVoxelTicket)
	}
	for ticket, record := range s.streamedVoxelTickets {
		if record.status.State >= StreamedVoxelRenderReady {
			continue
		}
		marker, exists := markers[record.status.Entity]
		if !exists || marker.Ticket != ticket || marker.Generation != record.status.Generation ||
			(record.status.State == StreamedVoxelRenderUploading && !s.streamedVoxelTargetCurrent(record)) {
			record.finish(StreamedVoxelRenderCancelled)
		}
	}
	for entity, marker := range markers {
		if marker.Ticket != 0 && s.streamedVoxelTickets[marker.Ticket] == nil {
			s.streamedVoxelTickets[marker.Ticket] = &streamedVoxelTicket{status: StreamedVoxelRenderStatus{
				State: StreamedVoxelRenderPendingBridge, Entity: entity, Generation: marker.Generation,
			}}
		}
	}
	return markers
}

func (s *VoxelRtState) pendingStreamedVoxel(entity EntityId, marker StreamedVoxelRenderComponent) *streamedVoxelTicket {
	record := s.streamedVoxelTickets[marker.Ticket]
	if record != nil && record.status.Entity == entity && record.status.Generation == marker.Generation && record.status.State == StreamedVoxelRenderPendingBridge {
		return record
	}
	return nil
}

func (s *VoxelRtState) failStreamedVoxelAdoption(entity EntityId, marker StreamedVoxelRenderComponent, failure string) {
	if record := s.pendingStreamedVoxel(entity, marker); record != nil {
		record.finish(StreamedVoxelRenderFailed)
		record.status.Failure = failure
	}
}

func (s *VoxelRtState) adoptStreamedVoxel(entity EntityId, marker StreamedVoxelRenderComponent, obj *core.VoxelObject) {
	if record := s.pendingStreamedVoxel(entity, marker); record != nil {
		record.object, record.mapTarget = obj, obj.XBrickMap
		record.status.MapID, record.status.TargetRevision = obj.XBrickMap.ID, obj.XBrickMap.Revision
		record.status.PendingSectors, record.status.PendingBricks = len(obj.XBrickMap.DirtySectors), len(obj.XBrickMap.DirtyBricks)
		record.status.State = StreamedVoxelRenderUploading
	}
}

func (s *VoxelRtState) endStreamedVoxelSync() {
	for _, record := range s.streamedVoxelTickets {
		switch record.status.State {
		case StreamedVoxelRenderPendingBridge:
			record.finish(StreamedVoxelRenderFailed)
			record.status.Failure = "streamed voxel requires transform and voxel model components"
		case StreamedVoxelRenderUploading:
			if !s.streamedVoxelTargetCurrent(record) {
				record.finish(StreamedVoxelRenderCancelled)
			}
		}
	}
}

// The main-thread bridge calls this immediately after RtApp.Update queues the
// frame's uploads. Terminal status is latched even after later voxel edits.
func (s *VoxelRtState) refreshStreamedVoxelStatuses() {
	if s == nil || s.RtApp == nil {
		return
	}
	for _, record := range s.streamedVoxelTickets {
		if record.status.State != StreamedVoxelRenderUploading {
			continue
		}
		if !s.streamedVoxelTargetCurrent(record) {
			record.finish(StreamedVoxelRenderCancelled)
			continue
		}
		ready, sectors, bricks := s.RtApp.BufferManager.VoxelObjectReady(record.object, record.mapTarget, record.status.TargetRevision)
		record.status.PendingSectors, record.status.PendingBricks = sectors, bricks
		if ready {
			record.finish(StreamedVoxelRenderReady)
		}
	}
}
