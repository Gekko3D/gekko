package gpu

import (
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// ManagedGeometryAdmissionBudget controls the explicit CPU admission ledger.
// The zero value disables admission. An enabled zero cap pauses new inputs.
type ManagedGeometryAdmissionBudget struct {
	Enabled             bool
	MaxInputBytes       uint64
	MaxCopiedStageBytes uint64
}

func DefaultManagedGeometryAdmissionBudget() ManagedGeometryAdmissionBudget {
	return ManagedGeometryAdmissionBudget{Enabled: true, MaxInputBytes: 128 << 20, MaxCopiedStageBytes: 128 << 20}
}

type ManagedGeometryAdmissionResult uint8

const (
	ManagedGeometryAdmissionDisabled ManagedGeometryAdmissionResult = iota
	ManagedGeometryAdmissionUnavailable
	ManagedGeometryAdmissionAccepted
	ManagedGeometryAdmissionCoalesced
	ManagedGeometryAdmissionUnchanged
	ManagedGeometryAdmissionStale
	ManagedGeometryAdmissionSourceChanged
	ManagedGeometryAdmissionPressure
	ManagedGeometryAdmissionOverflow
)

type ManagedGeometryAdmissionStats struct {
	InputBytes               uint64
	OwnedMetadataBytes       uint64
	ReservedCopiedStageBytes uint64
	TotalStageBytes          uint64
	InputPressureBytes       uint64
	StagePressureBytes       uint64
	OwnerCount               int
	GenerationCount          int
}

// Intrusive nodes make their language-level metadata charge exact. Fixed
// manager storage, allocator overhead and graphs behind source/owner pointers
// are outside the admission domains.
type managedGeometryOwner struct {
	object              *core.VoxelObject
	accepted, successor *managedGeometryGeneration
	next                *managedGeometryOwner
}

type managedGeometryGeneration struct {
	input       core.ManagedGeometryInput
	inputBytes  uint64
	reserved    uint64
	head        *managedGeometrySectorEntry
	copied      int
	copiedBytes uint64
}

// Admission reserves each full entry before service allocates it. Entries are
// immutable after prepend; no entry array or journal is allocated by admission.
type managedGeometrySectorEntry struct {
	coord    [3]int
	sector   *volume.Sector
	previous *managedGeometrySectorEntry
}

func (m *GpuBufferManager) SetManagedGeometryAdmissionBudget(budget ManagedGeometryAdmissionBudget) {
	if m == nil {
		return
	}
	m.managedGeometryBudget = budget
	if !budget.Enabled {
		m.managedGeometryOwners = nil
		m.managedGeometryStats = ManagedGeometryAdmissionStats{}
	}
}

func (m *GpuBufferManager) ManagedGeometryAdmissionBudget() ManagedGeometryAdmissionBudget {
	if m == nil {
		return ManagedGeometryAdmissionBudget{}
	}
	return m.managedGeometryBudget
}

// ManagedGeometryAdmissionStats reads scalar counters without capturing or
// traversing geometry or owners. Calls require exclusive manager access.
func (m *GpuBufferManager) ManagedGeometryAdmissionStats() ManagedGeometryAdmissionStats {
	if m == nil {
		return ManagedGeometryAdmissionStats{}
	}
	s := m.managedGeometryStats
	b := m.managedGeometryBudget
	if b.Enabled {
		if s.InputBytes > b.MaxInputBytes {
			s.InputPressureBytes = s.InputBytes - b.MaxInputBytes
		}
		if s.TotalStageBytes > b.MaxCopiedStageBytes {
			s.StagePressureBytes = s.TotalStageBytes - b.MaxCopiedStageBytes
		}
	}
	return s
}

func (m *GpuBufferManager) managedGeometryOwner(object *core.VoxelObject) *managedGeometryOwner {
	if m == nil || object == nil {
		return nil
	}
	for owner := m.managedGeometryOwners; owner != nil; owner = owner.next {
		if owner.object == object {
			return owner
		}
	}
	return nil
}

func (m *GpuBufferManager) ManagedGeometryInputs(object *core.VoxelObject) (core.ManagedGeometryInput, core.ManagedGeometryInput) {
	owner := m.managedGeometryOwner(object)
	if owner == nil {
		return core.ManagedGeometryInput{}, core.ManagedGeometryInput{}
	}
	if owner.successor == nil {
		return owner.accepted.input, core.ManagedGeometryInput{}
	}
	return owner.accepted.input, owner.successor.input
}

func managedGeometryAdd(a, b uint64) (uint64, bool) {
	if b > ^uint64(0)-a {
		return 0, false
	}
	return a + b, true
}

func managedGeometryMultiply(a, b uint64) (uint64, bool) {
	if b != 0 && a > ^uint64(0)/b {
		return 0, false
	}
	return a * b, true
}

// AdmitManagedGeometry captures only on explicit enabled calls. Capture can
// qualify pending producer commands; this API makes no frame-time bound claim.
// Refusal preserves all admitted generations and reservations.
func (m *GpuBufferManager) AdmitManagedGeometry(object *core.VoxelObject) ManagedGeometryAdmissionResult {
	if m == nil || !m.managedGeometryBudget.Enabled {
		return ManagedGeometryAdmissionDisabled
	}
	input, ok := object.CaptureManagedGeometryInput()
	// Capture invokes the producer, which may explicitly disable admission.
	if !m.managedGeometryBudget.Enabled {
		return ManagedGeometryAdmissionDisabled
	}
	if !ok {
		return ManagedGeometryAdmissionUnavailable
	}
	owner := m.managedGeometryOwner(object)
	if owner != nil {
		latest := owner.accepted
		if owner.successor != nil {
			latest = owner.successor
		}
		if !input.SameSource(latest.input) {
			return ManagedGeometryAdmissionSourceChanged
		}
		if input.Generation() == latest.input.Generation() {
			return ManagedGeometryAdmissionUnchanged
		}
		if input.Generation() < latest.input.Generation() {
			return ManagedGeometryAdmissionStale
		}
	}

	view := input.Geometry()
	entries, ok := managedGeometryMultiply(uint64(view.Len()), uint64(unsafe.Sizeof(managedGeometrySectorEntry{})))
	if !ok {
		return ManagedGeometryAdmissionOverflow
	}
	reserved, ok := managedGeometryAdd(view.CopyBytes(), entries)
	if !ok {
		return ManagedGeometryAdmissionOverflow
	}
	reserved, ok = managedGeometryAdd(reserved, uint64(unsafe.Sizeof([1024][3]int{})))
	if !ok {
		return ManagedGeometryAdmissionOverflow
	}
	metadata := uint64(unsafe.Sizeof(managedGeometryGeneration{}))
	if owner == nil {
		metadata, ok = managedGeometryAdd(metadata, uint64(unsafe.Sizeof(managedGeometryOwner{})))
		if !ok {
			return ManagedGeometryAdmissionOverflow
		}
	}
	// Preflight the simultaneous old + incoming peak, including an obsolete
	// successor. Only after success may replacement release that descriptor.
	peak := m.managedGeometryStats
	peak.InputBytes, ok = managedGeometryAdd(peak.InputBytes, view.RetainedBytes())
	if !ok {
		return ManagedGeometryAdmissionOverflow
	}
	peak.OwnedMetadataBytes, ok = managedGeometryAdd(peak.OwnedMetadataBytes, metadata)
	if !ok {
		return ManagedGeometryAdmissionOverflow
	}
	peak.ReservedCopiedStageBytes, ok = managedGeometryAdd(peak.ReservedCopiedStageBytes, reserved)
	if !ok {
		return ManagedGeometryAdmissionOverflow
	}
	peak.TotalStageBytes, ok = managedGeometryAdd(peak.OwnedMetadataBytes, peak.ReservedCopiedStageBytes)
	if !ok || peak.GenerationCount == int(^uint(0)>>1) || (owner == nil && peak.OwnerCount == int(^uint(0)>>1)) {
		return ManagedGeometryAdmissionOverflow
	}
	peak.GenerationCount++
	if owner == nil {
		peak.OwnerCount++
	}
	budget := m.managedGeometryBudget
	if budget.MaxInputBytes == 0 || budget.MaxCopiedStageBytes == 0 || peak.InputBytes > budget.MaxInputBytes || peak.TotalStageBytes > budget.MaxCopiedStageBytes {
		return ManagedGeometryAdmissionPressure
	}
	gen := &managedGeometryGeneration{input: input, inputBytes: view.RetainedBytes(), reserved: reserved}
	m.managedGeometryStats = peak
	if owner == nil {
		m.managedGeometryOwners = &managedGeometryOwner{object: object, accepted: gen, next: m.managedGeometryOwners}
		return ManagedGeometryAdmissionAccepted
	}
	if owner.successor != nil {
		m.releaseManagedGeometryGeneration(owner.successor)
	}
	owner.successor = gen
	return ManagedGeometryAdmissionCoalesced
}

func (m *GpuBufferManager) releaseManagedGeometryGeneration(gen *managedGeometryGeneration) {
	s := &m.managedGeometryStats
	s.InputBytes -= gen.inputBytes
	s.ReservedCopiedStageBytes -= gen.reserved
	s.OwnedMetadataBytes -= uint64(unsafe.Sizeof(managedGeometryGeneration{}))
	s.TotalStageBytes -= gen.reserved
	s.TotalStageBytes -= uint64(unsafe.Sizeof(managedGeometryGeneration{}))
	s.GenerationCount--
}

// AdvanceManagedGeometryInput only promotes an existing CPU successor. It
// neither allocates GPU resources nor changes readiness or lookup publication.
func (m *GpuBufferManager) AdvanceManagedGeometryInput(object *core.VoxelObject, expected core.ManagedGeometryInput) bool {
	owner := m.managedGeometryOwner(object)
	if owner == nil || owner.successor == nil || !expected.SameSource(owner.accepted.input) || expected.Generation() != owner.accepted.input.Generation() {
		return false
	}
	m.releaseManagedGeometryGeneration(owner.accepted)
	owner.accepted, owner.successor = owner.successor, nil
	return true
}

func (m *GpuBufferManager) CancelManagedGeometryInputs(object *core.VoxelObject) bool {
	if m == nil || object == nil {
		return false
	}
	for link := &m.managedGeometryOwners; *link != nil; link = &(*link).next {
		owner := *link
		if owner.object != object {
			continue
		}
		*link = owner.next
		m.releaseManagedGeometryGeneration(owner.accepted)
		if owner.successor != nil {
			m.releaseManagedGeometryGeneration(owner.successor)
		}
		m.managedGeometryStats.OwnedMetadataBytes -= uint64(unsafe.Sizeof(managedGeometryOwner{}))
		m.managedGeometryStats.TotalStageBytes -= uint64(unsafe.Sizeof(managedGeometryOwner{}))
		m.managedGeometryStats.OwnerCount--
		return true
	}
	return false
}
