package gpu

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// ManagedGeometryStageView retains an immutable snapshot of a copied prefix.
// Caller-retained views and inspection copies are outside manager charges.
type ManagedGeometryStageView struct {
	input       core.ManagedGeometryInput
	root        *managedGeometryStageNode
	copied      int
	copiedBytes uint64
}

func (v ManagedGeometryStageView) Input() core.ManagedGeometryInput { return v.input }
func (v ManagedGeometryStageView) Len() int                         { return v.copied }
func (v ManagedGeometryStageView) Total() int                       { return v.input.Geometry().Len() }
func (v ManagedGeometryStageView) CopiedBytes() uint64              { return v.copiedBytes }
func (v ManagedGeometryStageView) Complete() bool                   { return v.Len() == v.Total() }

func (v ManagedGeometryStageView) entry(index int) *managedGeometryStageNode {
	if index < 0 || index >= v.copied {
		return nil
	}
	return managedGeometryLeaf(v.root, 0, v.Total(), index)
}
func (v ManagedGeometryStageView) Coord(index int) ([3]int, bool) {
	if v.entry(index) == nil {
		return [3]int{}, false
	}
	return v.input.Geometry().Coord(index)
}

// CopySector returns independent output, including initialized tombstones.
func (v ManagedGeometryStageView) CopySector(index int) (*volume.Sector, bool) {
	entry := v.entry(index)
	if entry == nil {
		return nil, false
	}
	return managedGeometryInspectSector(entry.sector), true
}
func (v ManagedGeometryStageView) SectorGeneration(index int) (uint64, bool) {
	entry := v.entry(index)
	if entry == nil {
		return 0, false
	}
	return entry.generation, true
}

// ManagedGeometryStage reads accepted staging without producer capture.
// Calls require exclusive manager access, as does the admission ledger.
func (m *GpuBufferManager) ManagedGeometryStage(object *core.VoxelObject) (ManagedGeometryStageView, bool) {
	owner := m.managedGeometryOwner(object)
	if owner == nil {
		return ManagedGeometryStageView{}, false
	}
	gen := owner.accepted
	return ManagedGeometryStageView{input: gen.input, root: gen.root, copied: gen.copied, copiedBytes: gen.copiedBytes}, true
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
		_, coordOK := geometry.Coord(gen.copied)
		bytes, bytesOK := geometry.CopySectorBytes(gen.copied)
		copiedBytes, sumOK := managedGeometryAdd(gen.copiedBytes, bytes)
		// Frozen preflight guarantees these checks. Keep them ahead of any
		// allocation so inconsistent input cannot exceed its reservation.
		if !coordOK || !bytesOK || !sumOK {
			break
		}
		sector, ok := geometry.CopySector(gen.copied)
		if !ok {
			break
		}
		gen.root = managedGeometryInstall(gen.root, 0, geometry.Len(), gen.copied, sector, bytes, gen.input.Generation())
		gen.copied++
		gen.copiedBytes = copiedBytes
		serviced++
	}
	return serviced
}
