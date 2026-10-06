package gpu

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// ManagedGeometryStageView retains an immutable snapshot of a copied prefix.
// Caller-retained views and inspection copies are outside manager charges.
type ManagedGeometryStageView struct {
	input       core.ManagedGeometryInput
	head        *managedGeometrySectorEntry
	copied      int
	copiedBytes uint64
}

func (v ManagedGeometryStageView) Input() core.ManagedGeometryInput { return v.input }
func (v ManagedGeometryStageView) Len() int                         { return v.copied }
func (v ManagedGeometryStageView) Total() int                       { return v.input.Geometry().Len() }
func (v ManagedGeometryStageView) CopiedBytes() uint64              { return v.copiedBytes }
func (v ManagedGeometryStageView) Complete() bool                   { return v.Len() == v.Total() }

// entry walks only the retained copied prefix, in reverse insertion order.
// Inspection is linear and is separate from the service allowance.
func (v ManagedGeometryStageView) entry(index int) *managedGeometrySectorEntry {
	if index < 0 || index >= v.copied {
		return nil
	}
	entry := v.head
	for remaining := v.copied - 1 - index; remaining > 0; remaining-- {
		entry = entry.previous
	}
	return entry
}

func (v ManagedGeometryStageView) Coord(index int) ([3]int, bool) {
	entry := v.entry(index)
	if entry == nil {
		return [3]int{}, false
	}
	return entry.coord, true
}

// CopySector returns an independent mutable copy of the stored sector.
func (v ManagedGeometryStageView) CopySector(index int) (*volume.Sector, bool) {
	entry := v.entry(index)
	if entry == nil {
		return nil, false
	}
	return entry.sector.Copy(), true
}

// ManagedGeometryStage reads accepted staging without producer capture.
// Calls require exclusive manager access, as does the admission ledger.
func (m *GpuBufferManager) ManagedGeometryStage(object *core.VoxelObject) (ManagedGeometryStageView, bool) {
	owner := m.managedGeometryOwner(object)
	if owner == nil {
		return ManagedGeometryStageView{}, false
	}
	gen := owner.accepted
	return ManagedGeometryStageView{input: gen.input, head: gen.head, copied: gen.copied, copiedBytes: gen.copiedBytes}, true
}

// ServiceManagedGeometry copies at most maxEntries accepted sectors using the
// existing reservation. It neither captures producers nor publishes GPU state.
// The allowance bounds sector count, not allocation time or auxiliary capacity.
// Calls require exclusive manager access.
func (m *GpuBufferManager) ServiceManagedGeometry(object *core.VoxelObject, maxEntries int) int {
	if maxEntries <= 0 {
		return 0
	}
	owner := m.managedGeometryOwner(object)
	if owner == nil {
		return 0
	}
	gen := owner.accepted
	geometry := gen.input.Geometry()
	serviced := 0
	for serviced < maxEntries && gen.copied < geometry.Len() {
		coord, coordOK := geometry.Coord(gen.copied)
		bytes, bytesOK := geometry.CopySectorBytes(gen.copied)
		copiedBytes, sumOK := managedGeometryAdd(gen.copiedBytes, bytes)
		// Frozen preflight guarantees these checks. Keep them ahead of any
		// allocation so inconsistent input cannot exceed its reservation.
		if !coordOK || !bytesOK || !sumOK || copiedBytes > geometry.CopyBytes() {
			break
		}
		sector, ok := geometry.CopySector(gen.copied)
		if !ok {
			break
		}
		gen.head = &managedGeometrySectorEntry{coord: coord, sector: sector, previous: gen.head}
		gen.copied++
		gen.copiedBytes = copiedBytes
		serviced++
	}
	return serviced
}
