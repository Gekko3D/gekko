package gpu

import "github.com/gekko3d/gekko/voxelrt/rt/core"

type ManagedGeometryContentResult uint8

const (
	ManagedGeometryContentUnavailable ManagedGeometryContentResult = iota
	ManagedGeometryContentMismatch
	ManagedGeometryContentIgnored
	ManagedGeometryContentQueued
	ManagedGeometryContentCoalesced
	ManagedGeometryContentSweepScheduled
)

// ManagedGeometryContentStatus describes accepted-generation notification work,
// not content coherence or GPU readiness. Reads do not capture the producer.
type ManagedGeometryContentStatus struct {
	Input              core.ManagedGeometryInput
	PendingCoordinates int
	SweepPending       bool
	SweepCursor        int
	SweepAgain         bool
	Pending            bool
}

func managedGeometryContentMatches(gen *managedGeometryGeneration, expected core.ManagedGeometryInput) bool {
	return expected.SameSource(gen.input) && expected.Generation() == gen.input.Generation()
}

// managedGeometryContentCompare uses comparisons rather than subtraction so
// signed coordinates at either integer limit remain correctly ordered.
func managedGeometryContentCompare(a, b [3]int) int {
	for axis := range a {
		if a[axis] < b[axis] {
			return -1
		}
		if a[axis] > b[axis] {
			return 1
		}
	}
	return 0
}

func managedGeometryContentContains(input core.ManagedGeometryInput, coord [3]int) bool {
	geometry := input.Geometry()
	lo, hi := 0, geometry.Len()
	for lo < hi {
		mid := lo + (hi-lo)/2
		candidate, ok := geometry.Coord(mid)
		if !ok {
			return false
		}
		switch managedGeometryContentCompare(candidate, coord) {
		case -1:
			lo = mid + 1
		case 1:
			hi = mid
		default:
			return true
		}
	}
	return false
}

// requestContentSweep subsumes the journal only into a pass guaranteed to visit
// all its coordinates. Once progress exists, that requires one follow-up pass.
func (gen *managedGeometryGeneration) requestContentSweep() {
	gen.contentCount = 0
	if gen.input.Geometry().Len() == 0 {
		return
	}
	if !gen.sweepPending {
		gen.sweepPending = true
		gen.sweepCursor = 0
		gen.sweepAgain = false
	} else if gen.sweepCursor > 0 {
		gen.sweepAgain = true
	}
}

// QueueManagedGeometryContent journals a notification only within accepted
// topology and identity. At capacity, a conservative sweep subsumes the work.
// Calls require exclusive manager access; no producer capture occurs.
func (m *GpuBufferManager) QueueManagedGeometryContent(object *core.VoxelObject, expected core.ManagedGeometryInput, coord [3]int) ManagedGeometryContentResult {
	owner := m.managedGeometryOwner(object)
	if owner == nil {
		return ManagedGeometryContentUnavailable
	}
	gen := owner.accepted
	if !managedGeometryContentMatches(gen, expected) {
		return ManagedGeometryContentMismatch
	}
	if !managedGeometryContentContains(gen.input, coord) {
		return ManagedGeometryContentIgnored
	}
	for i := 0; i < gen.contentCount; i++ {
		if gen.contentJournal[i] == coord {
			return ManagedGeometryContentCoalesced
		}
	}
	if gen.contentCount == 1024 {
		gen.requestContentSweep()
		return ManagedGeometryContentSweepScheduled
	}
	if gen.contentJournal == nil {
		gen.contentJournal = new([1024][3]int)
	}
	gen.contentJournal[gen.contentCount] = coord
	gen.contentCount++
	return ManagedGeometryContentQueued
}

// RequestManagedGeometryContentSweep preserves an active cursor and schedules
// at most one follow-up pass. Later notifications remain journaled separately.
func (m *GpuBufferManager) RequestManagedGeometryContentSweep(object *core.VoxelObject, expected core.ManagedGeometryInput) bool {
	owner := m.managedGeometryOwner(object)
	if owner == nil || !managedGeometryContentMatches(owner.accepted, expected) {
		return false
	}
	owner.accepted.requestContentSweep()
	return true
}

func (m *GpuBufferManager) ManagedGeometryContentStatus(object *core.VoxelObject) (ManagedGeometryContentStatus, bool) {
	owner := m.managedGeometryOwner(object)
	if owner == nil {
		return ManagedGeometryContentStatus{}, false
	}
	gen := owner.accepted
	return ManagedGeometryContentStatus{
		Input: gen.input, PendingCoordinates: gen.contentCount,
		SweepPending: gen.sweepPending, SweepCursor: gen.sweepCursor,
		SweepAgain: gen.sweepAgain, Pending: gen.contentCount > 0 || gen.sweepPending,
	}, true
}

// ServiceManagedGeometryContent attempts at most maxEntries coordinates, with
// sweeps before the journal. False stops service; false and panic retain the
// current visit. The return count includes failed attempts. Acknowledgement
// does not copy sector content or imply GPU readiness.
//
// Calls require exclusive manager access. The callback may inspect immutable
// inputs and status, but must not mutate the manager or producer, admit, cancel
// or promote generations, or reenter service.
func (m *GpuBufferManager) ServiceManagedGeometryContent(object *core.VoxelObject, expected core.ManagedGeometryInput, maxEntries int, visit func([3]int) bool) int {
	if maxEntries <= 0 || visit == nil {
		return 0
	}
	owner := m.managedGeometryOwner(object)
	if owner == nil || !managedGeometryContentMatches(owner.accepted, expected) {
		return 0
	}
	gen := owner.accepted
	geometry := gen.input.Geometry()
	attempted := 0
	for attempted < maxEntries {
		if gen.sweepPending {
			coord, ok := geometry.Coord(gen.sweepCursor)
			if !ok {
				break
			}
			attempted++
			if !visit(coord) {
				break
			}
			gen.sweepCursor++
			if gen.sweepCursor == geometry.Len() {
				gen.sweepCursor = 0
				gen.sweepPending = gen.sweepAgain
				gen.sweepAgain = false
			}
		} else if gen.contentCount > 0 {
			coord := gen.contentJournal[gen.contentCount-1]
			attempted++
			if !visit(coord) {
				break
			}
			gen.contentCount--
		} else {
			break
		}
	}
	return attempted
}
