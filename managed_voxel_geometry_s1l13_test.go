package gekko

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"reflect"
	"testing"
)

func s1l13RootCoherent(t *testing.T, m *gpu.GpuBufferManager, obj *core.VoxelObject, want bool) {
	t.Helper()
	s, ok := m.ManagedGeometryReconciliationStatus(obj)
	if !ok || !s.Qualified || s.Coherent != want {
		t.Fatalf("reconciliation %+v/%v want coherent %v", s, ok, want)
	}
}
func s1l13RootCurrent(t *testing.T, m *gpu.GpuBufferManager, obj *core.VoxelObject, in core.ManagedGeometryInput, index int) {
	t.Helper()
	stage, ok := m.ManagedGeometryStage(obj)
	if !ok {
		t.Fatal("stage")
	}
	c, _ := stage.Coord(index)
	current := s1l10BridgeSector(t, obj, in, c)
	want, _ := current.Sector().CopySector()
	got, ok := stage.CopySector(index)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatal("reconciled content differs from sealed current authority")
	}
	if got != nil {
		for i, b := range got.PackedBricks {
			w := want.PackedBricks[i]
			if cap(b.PrecomputedAux) != cap(w.PrecomputedAux) || (b.PrecomputedAux == nil) != (w.PrecomputedAux == nil) {
				t.Fatal("sealed auxiliary shape changed")
			}
		}
	}
	g, ok := stage.SectorGeneration(index)
	if !ok || g != current.Generation() {
		t.Fatalf("leaf generation %d/%v want %d", g, ok, current.Generation())
	}
}
func TestS1l13EngineContinuousSparseFeedAvoidsFullSweep(t *testing.T) {
	f, eid, obj, m, in := s1l11FeedFixture(t, s1l11Source(16, 80, 144, 208))
	if m.ServiceManagedGeometryReconciliation(obj, in, 4) != 4 {
		t.Fatal("enumeration")
	}
	s1l13RootCoherent(t, m, obj, true)
	frozen, _ := m.ManagedGeometryStage(obj)
	saved, _ := frozen.CopySector(0)
	for _, value := range []uint8{2, 3, 4} {
		p1dApply(t, f, eid, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: value})
		s1l13RootCoherent(t, m, obj, false)
		if got := m.ServiceManagedGeometryReconciliation(obj, in, 100); got != 1 {
			t.Fatalf("sparse publication attempted %d want 1", got)
		}
		s1l13RootCoherent(t, m, obj, true)
		s1l13RootCurrent(t, m, obj, in, 0)
	}
	old, _ := frozen.CopySector(0)
	if !reflect.DeepEqual(old, saved) {
		t.Fatal("engine edits mutated frozen stage")
	}
	// Changes wholly outside accepted coordinates still close the exhaustive edge.
	p1dApply(t, f, eid, volume.VoxelWrite{X: 400, Y: 16, Z: 16, Value: 7})
	if got := m.ServiceManagedGeometryReconciliation(obj, in, 100); got != 0 {
		t.Fatalf("outside accepted topology forced %d attempts", got)
	}
	s1l13RootCoherent(t, m, obj, true)
	if m.AdmitManagedGeometry(obj) != gpu.ManagedGeometryAdmissionCoalesced {
		t.Fatal("new topology missing successor")
	}
	accepted, next := m.ManagedGeometryInputs(obj)
	if accepted.Generation() != in.Generation() || next.Geometry().Len() != 5 {
		t.Fatal("accepted topology changed")
	}
	if len(m.Allocations)+len(m.PendingUpdates)+len(m.SectorToInfo)+len(m.BrickToSlot) != 0 {
		t.Fatal("coherence published GPU work")
	}
}
func TestS1l13EngineNoopsAreSilentAndPanicPrefixHasCompleteMarker(t *testing.T) {
	f, eid, obj, m, in := s1l11FeedFixture(t, s1l11Source(16, 80, 144))
	m.ServiceManagedGeometryReconciliation(obj, in, 3)
	before, _ := m.ManagedGeometryReconciliationStatus(obj)
	for _, writes := range [][]volume.VoxelWrite{nil, {}, {{X: 16, Y: 16, Z: 16, Value: 1}}, {{X: 500, Y: 16, Z: 16}}} {
		p1dApply(t, f, eid, writes...)
	}
	after, _ := m.ManagedGeometryReconciliationStatus(obj)
	if before != after {
		t.Fatal("no-op changed generation/coverage")
	}
	sentinel := &struct{ n int }{3}
	func() {
		defer func() {
			if recover() != sentinel {
				t.Fatal("panic changed")
			}
		}()
		_ = ApplyManagedVoxelWrites(f.cmd, f.server, eid, func(yield func(volume.VoxelWrite) bool) {
			yield(volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 8})
			panic(sentinel)
		})
	}()
	if got := m.ServiceManagedGeometryReconciliation(obj, in, 100); got != 1 {
		t.Fatalf("panic prefix marker missing, attempts %d", got)
	}
	s1l13RootCoherent(t, m, obj, true)
	s1l13RootCurrent(t, m, obj, in, 0)
}
func TestS1l13EngineBoundaryHaloAndTombstoneReconciled(t *testing.T) {
	source := s1l11Source(31, 32, 80)
	source.Sectors[[3]int{1, 0, 0}].PackedBricks[0].PrecomputedAux = make([]byte, 2, 80)
	f, eid, obj, m, in := s1l11FeedFixture(t, source)
	m.ServiceManagedGeometryReconciliation(obj, in, 3)
	p1dApply(t, f, eid, volume.VoxelWrite{X: 31, Y: 16, Z: 16, Value: 2})
	if got := m.ServiceManagedGeometryReconciliation(obj, in, 100); got != 2 {
		t.Fatalf("target+halo attempts %d", got)
	}
	s1l13RootCoherent(t, m, obj, true)
	s1l13RootCurrent(t, m, obj, in, 0)
	s1l13RootCurrent(t, m, obj, in, 1)
	p1dApply(t, f, eid, volume.VoxelWrite{X: 31, Y: 16, Z: 16})
	m.ServiceManagedGeometryReconciliation(obj, in, 100)
	s1l13RootCoherent(t, m, obj, true)
	stage, _ := m.ManagedGeometryStage(obj)
	if s, ok := stage.CopySector(0); !ok || s != nil {
		t.Fatal("removed accepted coordinate is not initialized tombstone")
	}
}
func TestS1l13EngineScalarReaderRejectsExposureAndPendingBinding(t *testing.T) {
	for _, kind := range []string{"exposed", "pending-model", "pending-remove", "derivative", "reinstall"} {
		t.Run(kind, func(t *testing.T) {
			f, eid, obj, m, in := s1l11FeedFixture(t, s1l11Source(16))
			m.ServiceManagedGeometryReconciliation(obj, in, 1)
			if g, ok := obj.CurrentManagedGeometryGeneration(in); !ok || g != in.Generation() {
				t.Fatal("initial scalar qualification")
			}
			switch kind {
			case "exposed":
				vmc := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
				f.server.GetVoxelGeometry(vmc.OverrideGeometry)
			case "pending-model":
				vmc := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
				vmc.OverrideGeometry = f.server.CreateCubeModel(1, 1, 1, 2)
				f.cmd.AddComponents(eid, vmc)
			case "pending-remove":
				f.cmd.RemoveEntity(eid)
			case "derivative":
				obj.XBrickMap = obj.XBrickMap.Copy()
			case "reinstall":
				obj.SetManagedGeometryProducer(obj.XBrickMap, nil)
			}
			if g, ok := obj.CurrentManagedGeometryGeneration(in); ok || g != 0 {
				t.Fatal("invalid engine binding scalar-qualified")
			}
			s, ok := m.ManagedGeometryReconciliationStatus(obj)
			if !ok || s.Qualified || s.Coherent {
				t.Fatalf("invalid status %+v/%v", s, ok)
			}
		})
	}
}
