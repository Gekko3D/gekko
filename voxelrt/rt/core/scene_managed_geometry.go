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

type managedGeometryProducer struct {
	derivative *volume.XBrickMap
	capture    func() (volume.ManagedGeometryView, uint64, bool)
	source     *managedGeometrySource
}

// SetManagedGeometryProducer installs a trusted ownership adapter on the engine
// thread. It does not certify raw renderer edits or invoke capture. Nil clears it.
// Each installation establishes a fresh attachment identity.
func (obj *VoxelObject) SetManagedGeometryProducer(derivative *volume.XBrickMap, capture func() (volume.ManagedGeometryView, uint64, bool)) {
	if obj == nil {
		return
	}
	obj.managedGeometryProducer = nil
	if capture != nil {
		obj.managedGeometryProducer = &managedGeometryProducer{derivative: derivative, capture: capture, source: &managedGeometrySource{}}
	}
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
