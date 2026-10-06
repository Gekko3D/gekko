package gpu

import (
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

type ManagedGeometrySectorReservationResult uint8

const (
	ManagedGeometrySectorReservationDisabled ManagedGeometrySectorReservationResult = iota
	ManagedGeometrySectorReservationUnavailable
	ManagedGeometrySectorReservationMismatch
	ManagedGeometrySectorReservationIgnored
	ManagedGeometrySectorReservationStale
	ManagedGeometrySectorReservationPressure
	ManagedGeometrySectorReservationOverflow
	ManagedGeometrySectorReservationReserved
)

// The descriptor owns a frozen input and caches its exact ledger charges.
// Its generation's pointer is covered by generation metadata admission.
type managedGeometrySectorReservation struct {
	input                core.ManagedGeometrySectorInput
	inputBytes, reserved uint64
}

// ReserveManagedGeometrySector retains current qualified sector content without
// copying it or advancing stage/content work. Calls require exclusive manager
// access. The trusted reader may change policy or admit, cancel or promote, but
// must not reenter reservation/release or stage/content service.
func (m *GpuBufferManager) ReserveManagedGeometrySector(object *core.VoxelObject, expected core.ManagedGeometryInput, coord [3]int) ManagedGeometrySectorReservationResult {
	if m == nil || !m.managedGeometryBudget.Enabled {
		return ManagedGeometrySectorReservationDisabled
	}
	owner := m.managedGeometryOwner(object)
	if owner == nil {
		return ManagedGeometrySectorReservationUnavailable
	}
	gen := owner.accepted
	if !managedGeometryContentMatches(gen, expected) {
		return ManagedGeometrySectorReservationMismatch
	}
	if !managedGeometryContentContains(gen.input, coord) {
		return ManagedGeometrySectorReservationIgnored
	}
	if m.managedGeometryBudget.MaxInputBytes == 0 || m.managedGeometryBudget.MaxCopiedStageBytes == 0 {
		return ManagedGeometrySectorReservationPressure
	}

	input, ok := object.CaptureManagedGeometrySector(gen.input, coord)
	// Reader effects survive refusal. Even equal publication tokens after
	// cancellation/readmission must not let this call charge a detached owner.
	if !m.managedGeometryBudget.Enabled {
		return ManagedGeometrySectorReservationDisabled
	}
	if m.managedGeometryOwner(object) != owner || owner.accepted != gen {
		return ManagedGeometrySectorReservationMismatch
	}
	if !ok {
		return ManagedGeometrySectorReservationUnavailable
	}
	if gen.pending != nil && input.Generation() < gen.pending.input.Generation() {
		return ManagedGeometrySectorReservationStale
	}

	sector := input.Sector()
	inputBytes, reserved := sector.RetainedBytes(), sector.CopyBytes()
	metadata := uint64(unsafe.Sizeof(managedGeometrySectorReservation{}))
	// Use the postcallback ledger. An admitted successor or other owner and the
	// old pending input all remain charged throughout the incoming peak.
	peak := m.managedGeometryStats
	peak.InputBytes, ok = managedGeometryAdd(peak.InputBytes, inputBytes)
	if !ok {
		return ManagedGeometrySectorReservationOverflow
	}
	peak.ReservedCopiedStageBytes, ok = managedGeometryAdd(peak.ReservedCopiedStageBytes, reserved)
	if !ok {
		return ManagedGeometrySectorReservationOverflow
	}
	peak.OwnedMetadataBytes, ok = managedGeometryAdd(peak.OwnedMetadataBytes, metadata)
	if !ok {
		return ManagedGeometrySectorReservationOverflow
	}
	peak.TotalStageBytes, ok = managedGeometryAdd(peak.OwnedMetadataBytes, peak.ReservedCopiedStageBytes)
	if !ok {
		return ManagedGeometrySectorReservationOverflow
	}
	budget := m.managedGeometryBudget
	if budget.MaxInputBytes == 0 || budget.MaxCopiedStageBytes == 0 || peak.InputBytes > budget.MaxInputBytes || peak.TotalStageBytes > budget.MaxCopiedStageBytes {
		return ManagedGeometrySectorReservationPressure
	}

	pending := &managedGeometrySectorReservation{input: input, inputBytes: inputBytes, reserved: reserved}
	m.managedGeometryStats = peak
	m.releaseManagedGeometrySectorReservation(gen)
	gen.pending = pending
	return ManagedGeometrySectorReservationReserved
}

// ManagedGeometrySectorReservation reads the frozen held input without invoking
// a provider. Caller-retained values are outside manager ledger charges.
func (m *GpuBufferManager) ManagedGeometrySectorReservation(object *core.VoxelObject) (core.ManagedGeometrySectorInput, bool) {
	owner := m.managedGeometryOwner(object)
	if owner == nil || owner.accepted.pending == nil {
		return core.ManagedGeometrySectorInput{}, false
	}
	return owner.accepted.pending.input, true
}

// ReleaseManagedGeometrySectorReservation releases only a matching accepted
// generation's pending input, even under pressure or invalid live selection.
func (m *GpuBufferManager) ReleaseManagedGeometrySectorReservation(object *core.VoxelObject, expected core.ManagedGeometryInput) bool {
	owner := m.managedGeometryOwner(object)
	if owner == nil || !managedGeometryContentMatches(owner.accepted, expected) || owner.accepted.pending == nil {
		return false
	}
	m.releaseManagedGeometrySectorReservation(owner.accepted)
	return true
}

func (m *GpuBufferManager) releaseManagedGeometrySectorReservation(gen *managedGeometryGeneration) {
	pending := gen.pending
	if pending == nil {
		return
	}
	gen.pending = nil
	metadata := uint64(unsafe.Sizeof(managedGeometrySectorReservation{}))
	s := &m.managedGeometryStats
	s.InputBytes -= pending.inputBytes
	s.ReservedCopiedStageBytes -= pending.reserved
	s.OwnedMetadataBytes -= metadata
	s.TotalStageBytes -= pending.reserved
	s.TotalStageBytes -= metadata
}
