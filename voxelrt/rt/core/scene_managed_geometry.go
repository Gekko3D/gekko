package core

import "github.com/gekko3d/gekko/voxelrt/rt/volume"

// A nonzero-size token has stable pointer identity without retaining its producer.
type managedGeometrySource struct{ marker byte }

// ManagedGeometryInput retains frozen geometry and publication identity only.
// It can outlive the engine-thread producer and its mutable owner.
type ManagedGeometryInput struct {
	geometry   volume.ManagedGeometryView
	generation uint64
	source     *managedGeometrySource
}

func (input ManagedGeometryInput) Geometry() volume.ManagedGeometryView { return input.geometry }
func (input ManagedGeometryInput) Generation() uint64                   { return input.generation }
func (input ManagedGeometryInput) SameSource(other ManagedGeometryInput) bool {
	return input.source != nil && input.source == other.source
}

// ManagedGeometrySectorInput retains one frozen current-sector view and its
// attachment identity, independently of the producer and mutable owner.
type ManagedGeometrySectorInput struct {
	sector     volume.ManagedSectorView
	generation uint64
	source     *managedGeometrySource
}

func (input ManagedGeometrySectorInput) Sector() volume.ManagedSectorView { return input.sector }
func (input ManagedGeometrySectorInput) Generation() uint64               { return input.generation }
func (input ManagedGeometrySectorInput) SameSource(other ManagedGeometryInput) bool {
	return input.source != nil && input.source == other.source
}

type managedGeometryProducer struct {
	derivative *volume.XBrickMap
	capture    func() (volume.ManagedGeometryView, uint64, bool)
	read       func([3]int) (volume.ManagedSectorView, uint64, bool)
	source     *managedGeometrySource
}

// SetManagedGeometryProducer installs a trusted ownership adapter on the engine
// thread. It does not certify raw renderer edits or invoke capture. Nil clears it.
// Each installation establishes a fresh attachment identity.
func (obj *VoxelObject) SetManagedGeometryProducer(derivative *volume.XBrickMap, capture func() (volume.ManagedGeometryView, uint64, bool)) {
	obj.SetManagedGeometryProducerWithSectorReader(derivative, capture, nil)
}

// SetManagedGeometryProducerWithSectorReader atomically installs lazy full and
// coordinate callbacks under a fresh identity. A nil full callback clears both.
func (obj *VoxelObject) SetManagedGeometryProducerWithSectorReader(derivative *volume.XBrickMap, capture func() (volume.ManagedGeometryView, uint64, bool), read func([3]int) (volume.ManagedSectorView, uint64, bool)) {
	if obj == nil {
		return
	}
	obj.managedGeometryProducer = nil
	if capture != nil {
		obj.managedGeometryProducer = &managedGeometryProducer{derivative: derivative, capture: capture, read: read, source: &managedGeometrySource{}}
	}
}

// CaptureManagedGeometrySector reads current qualified content from the expected
// live attachment, never falling back to a full capture. Qualified absence is
// available; rollback and callback-induced attachment or selection changes refuse.
// Calls require exclusive engine-thread access.
func (obj *VoxelObject) CaptureManagedGeometrySector(expected ManagedGeometryInput, coord [3]int) (ManagedGeometrySectorInput, bool) {
	if obj == nil {
		return ManagedGeometrySectorInput{}, false
	}
	producer := obj.managedGeometryProducer
	if producer == nil || producer.read == nil || expected.source == nil || expected.source != producer.source || !obj.managedGeometrySelectionQualified(producer) {
		return ManagedGeometrySectorInput{}, false
	}
	sector, generation, ok := producer.read(coord)
	if !ok || generation < expected.generation || sector.Coord() != coord || obj.managedGeometryProducer != producer || !obj.managedGeometrySelectionQualified(producer) {
		return ManagedGeometrySectorInput{}, false
	}
	return ManagedGeometrySectorInput{sector: sector, generation: generation, source: producer.source}, true
}

func (obj *VoxelObject) managedGeometrySelectionQualified(producer *managedGeometryProducer) bool {
	return obj.XBrickMap != nil && obj.XBrickMap == producer.derivative && !obj.XBrickMap.GPUEditMode && !obj.hasSpecialRenderLattice() && obj.renderRepresentation == nil
}

// MatchesManagedGeometrySource checks current attachment identity and selection
// without invoking either provider. Live ownership and generation qualification
// remain the caller's responsibility. Calls require exclusive engine-thread access.
func (obj *VoxelObject) MatchesManagedGeometrySource(expected ManagedGeometryInput) bool {
	if obj == nil || expected.source == nil {
		return false
	}
	producer := obj.managedGeometryProducer
	return producer != nil && producer.source == expected.source && obj.managedGeometrySelectionQualified(producer)
}

// CaptureManagedGeometryInput lazily captures ordinary sealed geometry. Capture
// requires exclusive engine-thread access; the resulting view is independently readable.
func (obj *VoxelObject) CaptureManagedGeometryInput() (ManagedGeometryInput, bool) {
	if obj == nil || obj.XBrickMap == nil || obj.XBrickMap.GPUEditMode || obj.hasSpecialRenderLattice() || obj.renderRepresentation != nil {
		return ManagedGeometryInput{}, false
	}
	producer := obj.managedGeometryProducer
	if producer == nil || producer.derivative != obj.XBrickMap {
		return ManagedGeometryInput{}, false
	}
	geometry, generation, ok := producer.capture()
	if !ok {
		return ManagedGeometryInput{}, false
	}
	return ManagedGeometryInput{geometry: geometry, generation: generation, source: producer.source}, true
}
