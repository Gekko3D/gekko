package gpu

import (
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

type ManagedGeometrySectorApplyResult uint8

const (
	ManagedGeometrySectorApplyDisabled ManagedGeometrySectorApplyResult = iota
	ManagedGeometrySectorApplyUnavailable
	ManagedGeometrySectorApplyMismatch
	ManagedGeometrySectorApplyUncopied
	ManagedGeometrySectorApplyStale
	ManagedGeometrySectorApplyOverflow
	ManagedGeometrySectorApplyApplied
)

// Successful scalar observations advance a sticky manager floor. Ownership is
// rechecked before recording it because trusted readers may detach this generation.
func (m *GpuBufferManager) managedGeometryCurrent(object *core.VoxelObject, owner *managedGeometryOwner, gen *managedGeometryGeneration) (uint64, bool) {
	g, ok := object.CurrentManagedGeometryGeneration(gen.input)
	if m.managedGeometryOwner(object) != owner || owner.accepted != gen {
		return 0, false
	}
	if !ok || g < gen.scalarFloor {
		// Lost qualification or token reuse cannot restore a history certificate. Leave
		// scheduling and all existing work untouched until explicit service.
		gen.coverageValid = false
		gen.repairRequired = true
		gen.passStable = false
		gen.passEntryMaterialized = false
		return 0, false
	}
	gen.scalarFloor = g
	return g, true
}

// ApplyManagedGeometrySectorReservation transfers a held output to its initialized ordinal.
// It never acknowledges notification work or certifies coherence.
func (m *GpuBufferManager) ApplyManagedGeometrySectorReservation(object *core.VoxelObject, expected core.ManagedGeometryInput) ManagedGeometrySectorApplyResult {
	if m == nil || !m.managedGeometryBudget.Enabled {
		return ManagedGeometrySectorApplyDisabled
	}
	owner := m.managedGeometryOwner(object)
	if owner == nil {
		return ManagedGeometrySectorApplyUnavailable
	}
	gen := owner.accepted
	if !managedGeometryContentMatches(gen, expected) {
		return ManagedGeometrySectorApplyMismatch
	}
	pending := gen.pending
	if pending == nil {
		return ManagedGeometrySectorApplyUnavailable
	}
	index, ok := managedGeometryOrdinal(gen.input, pending.input.Sector().Coord())
	if !ok {
		return ManagedGeometrySectorApplyMismatch
	}
	if index >= gen.copied {
		return ManagedGeometrySectorApplyUncopied
	}
	leaf := managedGeometryLeaf(gen.root, 0, gen.input.Geometry().Len(), index)
	if leaf == nil {
		return ManagedGeometrySectorApplyUncopied
	}
	if pending.input.Generation() < leaf.generation {
		return ManagedGeometrySectorApplyStale
	}
	g, qualified := m.managedGeometryCurrent(object, owner, gen)
	if !m.managedGeometryBudget.Enabled {
		return ManagedGeometrySectorApplyDisabled
	}
	if m.managedGeometryOwner(object) != owner || owner.accepted != gen || gen.pending != pending {
		return ManagedGeometrySectorApplyMismatch
	}
	if !qualified {
		return ManagedGeometrySectorApplyUnavailable
	}
	if g != pending.input.Generation() {
		return ManagedGeometrySectorApplyStale
	}
	if !pending.input.SameSource(gen.input) {
		return ManagedGeometrySectorApplyMismatch
	}
	copied, ok := managedGeometryAdd(gen.copiedBytes-leaf.bytes, pending.reserved)
	if !ok {
		return ManagedGeometrySectorApplyOverflow
	}
	reserved, ok := managedGeometryAdd(gen.reserved-leaf.bytes, pending.reserved)
	if !ok {
		return ManagedGeometrySectorApplyOverflow
	}
	// Both payload and cloned path were precharged, so lowered enabled caps cannot refuse.
	sector, present := pending.input.Sector().CopySector()
	if !present && pending.input.Sector().Present() {
		return ManagedGeometrySectorApplyUnavailable
	}
	root := managedGeometryInstall(gen.root, 0, gen.input.Geometry().Len(), index, sector, pending.reserved, g)
	gen.root, gen.copiedBytes, gen.reserved = root, copied, reserved
	gen.pending = nil
	metadata := uint64(unsafe.Sizeof(managedGeometrySectorReservation{}))
	s := &m.managedGeometryStats
	s.InputBytes -= pending.inputBytes
	s.OwnedMetadataBytes -= metadata
	s.ReservedCopiedStageBytes -= leaf.bytes
	s.TotalStageBytes -= leaf.bytes + metadata
	return ManagedGeometrySectorApplyApplied
}

// RecordManagedGeometryContentPublication marks an exhaustive producer batch edge.
func (m *GpuBufferManager) RecordManagedGeometryContentPublication(object *core.VoxelObject, expected core.ManagedGeometryInput, previous, current uint64) bool {
	owner := m.managedGeometryOwner(object)
	if owner == nil || !managedGeometryContentMatches(owner.accepted, expected) {
		return false
	}
	gen := owner.accepted
	if current > gen.scalarFloor {
		gen.scalarFloor = current
	}
	if current == gen.covered && current > previous {
		return true
	}
	if current <= previous || current < gen.covered || previous != gen.covered {
		gen.coverageValid = false
		gen.repairRequired = true
		gen.passStable = false
	} else if !gen.coverageValid {
		gen.repairRequired = true
	}
	if current > gen.covered {
		gen.covered = current
	}
	return true
}

type ManagedGeometryReconciliationStatus struct {
	Input                    core.ManagedGeometryInput
	Copied, Total            int
	Pending, Qualified       bool
	Generation               uint64
	Coherent, RepairRequired bool
}

func (m *GpuBufferManager) ManagedGeometryReconciliationStatus(object *core.VoxelObject) (ManagedGeometryReconciliationStatus, bool) {
	owner := m.managedGeometryOwner(object)
	if owner == nil {
		return ManagedGeometryReconciliationStatus{}, false
	}
	gen := owner.accepted
	g, ok := m.managedGeometryCurrent(object, owner, gen)
	if m.managedGeometryOwner(object) != owner || owner.accepted != gen {
		return ManagedGeometryReconciliationStatus{}, false
	}
	repair := gen.repairRequired || !gen.coverageValid || ok && g != gen.covered
	pending := gen.pending != nil || gen.sweepPending || gen.contentCount > 0 || gen.copied < gen.input.Geometry().Len() || repair
	return ManagedGeometryReconciliationStatus{Input: gen.input, Copied: gen.copied, Total: gen.input.Geometry().Len(), Pending: pending, Qualified: ok, Generation: g, RepairRequired: repair, Coherent: ok && gen.copied == gen.input.Geometry().Len() && !pending && !repair && g == gen.covered}, true
}

func (m *GpuBufferManager) managedGeometryReconcileCoordinate(object *core.VoxelObject, owner *managedGeometryOwner, gen *managedGeometryGeneration, coord [3]int, forceMaterialize bool) (uint64, bool) {
	g, ok := m.managedGeometryCurrent(object, owner, gen)
	if !ok {
		return 0, false
	}
	index, found := managedGeometryOrdinal(gen.input, coord)
	if !found || index >= gen.copied {
		return 0, false
	}
	leaf := managedGeometryLeaf(gen.root, 0, gen.input.Geometry().Len(), index)
	if !forceMaterialize && gen.pending == nil && leaf != nil && leaf.generation == g {
		return g, true
	}
	if gen.pending != nil && gen.pending.input.Sector().Coord() != coord {
		return 0, false
	}
	if gen.pending == nil || gen.pending.input.Generation() != g {
		if m.ReserveManagedGeometrySector(object, gen.input, coord) != ManagedGeometrySectorReservationReserved {
			return 0, false
		}
	}
	if m.managedGeometryOwner(object) != owner || owner.accepted != gen {
		return 0, false
	}
	if m.ApplyManagedGeometrySectorReservation(object, gen.input) != ManagedGeometrySectorApplyApplied {
		return 0, false
	}
	return gen.scalarFloor, true
}

// ServiceManagedGeometryReconciliation shares a coordinate allowance across all CPU work.
func (m *GpuBufferManager) ServiceManagedGeometryReconciliation(object *core.VoxelObject, expected core.ManagedGeometryInput, maxEntries int) int {
	if maxEntries <= 0 {
		return 0
	}
	owner := m.managedGeometryOwner(object)
	if owner == nil || !managedGeometryContentMatches(owner.accepted, expected) {
		return 0
	}
	gen := owner.accepted
	attempted := m.ServiceManagedGeometry(object, maxEntries)
	if attempted == maxEntries || gen.copied < gen.input.Geometry().Len() {
		return attempted
	}
	if gen.pending != nil {
		coord := gen.pending.input.Sector().Coord()
		attempted++
		if _, ok := m.managedGeometryReconcileCoordinate(object, owner, gen, coord, false); !ok {
			return attempted
		}
		if attempted == maxEntries {
			return attempted
		}
	}
	g, qualified := m.managedGeometryCurrent(object, owner, gen)
	if !qualified {
		return attempted
	}
	if g != gen.covered || !gen.coverageValid || gen.repairRequired {
		gen.coverageValid = false
		gen.repairRequired = true
		if gen.input.Geometry().Len() == 0 {
			end, ok := m.managedGeometryCurrent(object, owner, gen)
			if ok && end == g {
				gen.covered = g
				gen.coverageValid = true
				gen.repairRequired = false
			}
			return attempted
		}
		if !gen.sweepPending {
			gen.requestContentSweep()
		}
	}
	for attempted < maxEntries {
		if m.managedGeometryOwner(object) != owner || owner.accepted != gen {
			break
		}
		sweep := gen.sweepPending
		cursor, count := gen.sweepCursor, gen.contentCount
		var coord [3]int
		if sweep {
			var ok bool
			coord, ok = gen.input.Geometry().Coord(cursor)
			if !ok {
				break
			}
			if !gen.passActive {
				gen.passActive = true
				gen.passEntryMaterialized = false
				gen.passStable = cursor == 0
				gen.passGeneration = g
				if cursor > 0 {
					gen.sweepAgain = true
				}
			}
		} else if count > 0 {
			coord = gen.contentJournal[count-1]
		} else {
			break
		}
		attempted++
		// Unknown history requires a new materialization even when numeric tokens
		// match; only a completed current-coordinate copy permits panic retry reuse.
		observed, ok := m.managedGeometryReconcileCoordinate(object, owner, gen, coord, sweep && gen.repairRequired && !gen.passEntryMaterialized)
		if !ok {
			break
		}
		if m.managedGeometryOwner(object) != owner || owner.accepted != gen {
			break
		}
		// Selected work must still be exactly the same after callbacks.
		if sweep {
			if !gen.sweepPending || gen.sweepCursor != cursor {
				break
			}
			gen.passEntryMaterialized = true
			if observed != gen.passGeneration {
				gen.passStable = false
				gen.sweepAgain = true
			}
			if cursor+1 == gen.input.Geometry().Len() {
				// Qualify before acknowledging the final coordinate: a panic must
				// retain a valid cursor so a fresh leaf can finish this pass on retry.
				end, ok := m.managedGeometryCurrent(object, owner, gen)
				if m.managedGeometryOwner(object) != owner || owner.accepted != gen || !gen.sweepPending || gen.sweepCursor != cursor || !ok {
					break
				}
				stable := ok && gen.passStable && end == gen.passGeneration
				if !stable {
					gen.sweepAgain = true
				}
				if stable && !gen.sweepAgain {
					gen.covered = end
					gen.coverageValid = true
					gen.repairRequired = false
				}
				gen.sweepCursor = 0
				gen.sweepPending = gen.sweepAgain
				gen.sweepAgain = false
				gen.passActive = false
				gen.passEntryMaterialized = false
				if ok {
					g = end
				}
			} else {
				gen.sweepCursor++
				gen.passEntryMaterialized = false
			}
		} else {
			if gen.sweepPending || gen.contentCount != count || gen.contentJournal[count-1] != coord {
				break
			}
			gen.contentCount--
		}
	}
	return attempted
}
