package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestS1l7ManagedBridgeAdmissionEditExposureAndCancel(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	p1dEnable(t, f, eid)
	f.app.FlushCommands()
	f.sync()
	obj := f.state.GetVoxelObject(eid)
	m := &gpu.GpuBufferManager{}
	m.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
	if got := m.AdmitManagedGeometry(obj); got != gpu.ManagedGeometryAdmissionAccepted {
		t.Fatalf("first admission = %v", got)
	}
	accepted, _ := m.ManagedGeometryInputs(obj)
	historical := s1l6Map(t, accepted)
	p1dApply(t, f, eid, volume.VoxelWrite{X: 0, Value: 7})
	f.sync()
	current := s1l6Input(t, obj)
	if !accepted.SameSource(current) || current.Generation() <= accepted.Generation() {
		t.Fatal("tracked edit did not publish same-source successor")
	}
	if got := m.AdmitManagedGeometry(obj); got != gpu.ManagedGeometryAdmissionCoalesced {
		t.Fatalf("edited admission = %v", got)
	}
	a, b := m.ManagedGeometryInputs(obj)
	if !a.SameSource(accepted) || a.Generation() != accepted.Generation() || !b.SameSource(current) || b.Generation() != current.Generation() {
		t.Fatal("bridge publication identity changed during admission")
	}
	p1dVoxel(t, s1l6Map(t, b), 0, 7)
	before := m.ManagedGeometryAdmissionStats()
	vmc := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
	raw, ok := f.server.GetVoxelGeometry(vmc.OverrideGeometry)
	if !ok {
		t.Fatal("managed override unavailable")
	}
	raw.XBrickMap.SetVoxel(0, 0, 0, 9)
	if got := m.AdmitManagedGeometry(obj); got != gpu.ManagedGeometryAdmissionUnavailable {
		t.Fatalf("exposed admission = %v", got)
	}
	if m.ManagedGeometryAdmissionStats() != before {
		t.Fatal("exposure changed historical admission charges")
	}
	a, b = m.ManagedGeometryInputs(obj)
	if a.Generation() != accepted.Generation() || !a.SameSource(accepted) || b.Generation() != current.Generation() || !b.SameSource(current) {
		t.Fatal("exposure dropped retained inputs")
	}
	if !m.CancelManagedGeometryInputs(obj) || m.ManagedGeometryAdmissionStats() != (gpu.ManagedGeometryAdmissionStats{}) {
		t.Fatal("cancel did not release ledger")
	}
	if !reflect.DeepEqual(s1l6Map(t, accepted).Sectors, historical.Sectors) {
		t.Fatal("caller capture changed after edit, exposure and cancel")
	}
}
