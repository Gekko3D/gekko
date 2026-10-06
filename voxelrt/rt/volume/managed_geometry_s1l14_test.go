package volume_test

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s1l14Geometry(t *testing.T, m *volume.ManagedXBrickMap) volume.ManagedGeometryView {
	t.Helper()
	v, ok := m.CaptureGeometry()
	if !ok {
		t.Fatal("sealed geometry unavailable")
	}
	return v
}

func TestS1l14TopologyIdentitySurvivesContentAndHaloButNotMembership(t *testing.T) {
	raw := volume.NewXBrickMap()
	raw.SetVoxel(-1, 16, 16, 1)
	raw.SetVoxel(0, 16, 16, 2)
	m := volume.NewManagedXBrickMap(raw)
	before := s1l14Geometry(t, m)
	frozen, _ := before.CopySector(0)
	for _, w := range []volume.VoxelWrite{{X: -1, Y: 16, Z: 16, Value: 3}, {X: 1, Y: 16, Z: 16, Value: 4}, {X: 400, Y: 16, Z: 16}} {
		m.SetVoxel(w.X, w.Y, w.Z, w.Value)
		after := s1l14Geometry(t, m)
		if !before.SameTopology(after) || !after.SameTopology(before) {
			t.Fatal("content/halo changed coordinate frontier identity")
		}
	}
	child := m.Fork()
	if !before.SameTopology(s1l14Geometry(t, child)) {
		t.Fatal("sealed fork lost inherited topology identity")
	}
	m.SetVoxel(-1, 16, 16, 0)
	removed := s1l14Geometry(t, m)
	if before.SameTopology(removed) {
		t.Fatal("removal retained identity")
	}
	m.SetVoxel(64, 16, 16, 5)
	replaced := s1l14Geometry(t, m)
	if replaced.Len() != before.Len() || before.SameTopology(replaced) {
		t.Fatal("equal-count replacement falsely matched topology")
	}
	if !before.SameTopology(s1l14Geometry(t, child)) {
		t.Fatal("parent edit changed fork frontier")
	}
	m.ExposeMutable().SetVoxel(96, 0, 0, 6)
	got, _ := before.CopySector(0)
	if !reflect.DeepEqual(got, frozen) || !before.SameTopology(before) || !replaced.SameTopology(replaced) {
		t.Fatal("exposure changed retained geometry")
	}
}

func TestS1l14SameTopologyDoesNotEquateIndependentEqualCoordinates(t *testing.T) {
	a, b := volume.NewXBrickMap(), volume.NewXBrickMap()
	a.SetVoxel(0, 0, 0, 1)
	b.SetVoxel(0, 0, 0, 2)
	x, y := s1l14Geometry(t, volume.NewManagedXBrickMap(a)), s1l14Geometry(t, volume.NewManagedXBrickMap(b))
	if x.SameTopology(y) || y.SameTopology(x) {
		t.Fatal("coordinate equality is not immutable frontier identity")
	}
	if n := testing.AllocsPerRun(100, func() {
		if !x.SameTopology(x) {
			t.Fatal("self identity")
		}
	}); n != 0 {
		t.Fatalf("identity allocates %g", n)
	}
}
