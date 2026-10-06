package core_test

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s1l6Capture(t *testing.T, obj *core.VoxelObject) core.ManagedGeometryInput {
	t.Helper()
	input, ok := obj.CaptureManagedGeometryInput()
	if !ok {
		t.Fatal("qualified producer capture unavailable")
	}
	if !input.SameSource(input) {
		t.Fatal("successful input has no attachment identity")
	}
	return input
}

func s1l6Unavailable(t *testing.T, obj *core.VoxelObject) {
	t.Helper()
	input, ok := obj.CaptureManagedGeometryInput()
	if ok || input.Generation() != 0 || input.Geometry().Len() != 0 || input.SameSource(input) {
		t.Fatal("unavailable capture must return zero input")
	}
}

func TestS1l6CoreZeroNilEmptyAndLazyProducer(t *testing.T) {
	var zero core.ManagedGeometryInput
	if zero.SameSource(zero) || zero.Generation() != 0 || zero.Geometry().Len() != 0 {
		t.Fatal("zero input has data or source identity")
	}
	var nilObject *core.VoxelObject
	s1l6Unavailable(t, nilObject)
	obj := core.NewVoxelObject()
	s1l6Unavailable(t, obj)
	derivative := volume.NewXBrickMap()
	owner := volume.NewManagedXBrickMap(nil)
	obj.XBrickMap = derivative
	calls := 0
	obj.SetManagedGeometryProducer(derivative, func() (volume.ManagedGeometryView, uint64, bool) {
		calls++
		view, ok := owner.CaptureGeometry()
		return view, 0, ok
	})
	if calls != 0 {
		t.Fatal("install eagerly invoked producer")
	}
	empty := s1l6Capture(t, obj)
	if calls != 1 || empty.Generation() != 0 || empty.Geometry().Len() != 0 || empty.Geometry().RetainedBytes() != 0 || empty.Geometry().CopyBytes() != 0 {
		t.Fatal("qualified empty generation zero lost availability or charge")
	}
	again := s1l6Capture(t, obj)
	if !empty.SameSource(again) || empty.SameSource(zero) || zero.SameSource(empty) {
		t.Fatal("source identity changed without replacement")
	}
	obj.SetManagedGeometryProducer(derivative, func() (volume.ManagedGeometryView, uint64, bool) {
		view, ok := owner.CaptureGeometry()
		return view, 0, ok
	})
	reinstalled := s1l6Capture(t, obj)
	if reinstalled.Generation() != empty.Generation() || reinstalled.SameSource(empty) || empty.SameSource(reinstalled) {
		t.Fatal("reinstalled provider reused attachment identity at equal generation")
	}
	obj.SetManagedGeometryProducer(derivative, nil)
	s1l6Unavailable(t, obj)
	if !empty.SameSource(again) {
		t.Fatal("clearing changed historical identity")
	}
	obj.SetManagedGeometryProducer(derivative, func() (volume.ManagedGeometryView, uint64, bool) { return empty.Geometry(), 99, false })
	s1l6Unavailable(t, obj)
}

func TestS1l6CoreRejectsNilAndGPUDerivativeWithoutCallingProducer(t *testing.T) {
	for _, kind := range []string{"nil", "gpu"} {
		t.Run(kind, func(t *testing.T) {
			obj := core.NewVoxelObject()
			if kind == "nil" {
				obj.XBrickMap = nil
			} else {
				obj.XBrickMap = volume.NewXBrickMap()
				obj.XBrickMap.GPUEditMode = true
			}
			calls := 0
			obj.SetManagedGeometryProducer(obj.XBrickMap, func() (volume.ManagedGeometryView, uint64, bool) {
				calls++
				return volume.ManagedGeometryView{}, 0, true
			})
			s1l6Unavailable(t, obj)
			if calls != 0 {
				t.Fatal("ineligible CPU derivative invoked trusted producer")
			}
		})
	}
}

func TestS1l6CoreHistoricalGeometryIdentityAndCharges(t *testing.T) {
	source := volume.NewXBrickMap()
	source.SetVoxel(-33, 0, 0, 1)
	source.SetVoxel(32, 0, 0, 2)
	brick := source.Sectors[[3]int{-2, 0, 0}].PackedBricks[0]
	brick.Flags, brick.AtlasOffset, brick.OccupancyMask64 = 17, 23, 31
	brick.PrecomputedAux = []byte{4, 5, 6}
	source.Sectors[[3]int{-2, 0, 0}].Coords = [3]int{9, 8, 7}
	owner := volume.NewManagedXBrickMap(source)
	want := owner.Snapshot()
	generation := uint64(7)
	obj := core.NewVoxelObject()
	obj.XBrickMap = source.Copy()
	capture := func() (volume.ManagedGeometryView, uint64, bool) {
		view, ok := owner.CaptureGeometry()
		return view, generation, ok
	}
	obj.SetManagedGeometryProducer(obj.XBrickMap, capture)
	old := s1l6Capture(t, obj)
	direct, _ := owner.CaptureGeometry()
	if old.Geometry().RetainedBytes() != direct.RetainedBytes() || old.Geometry().CopyBytes() != direct.CopyBytes() {
		t.Fatal("input changed volume charge domains")
	}
	for i, key := range [][3]int{{-2, 0, 0}, {1, 0, 0}} {
		coord, ok := old.Geometry().Coord(i)
		copy, copied := old.Geometry().CopySector(i)
		if !ok || coord != key || !copied || !reflect.DeepEqual(copy, want.Sectors[key]) {
			t.Fatal("capture lost signed order or frozen geometry")
		}
		copy.Coords, copy.BrickMask64 = [3]int{}, 0
		copy.PackedBricks[0].Payload[0][0][0] = 99
		if len(copy.PackedBricks[0].PrecomputedAux) > 0 {
			copy.PackedBricks[0].PrecomputedAux[0] = 99
		}
		fresh, _ := old.Geometry().CopySector(i)
		if !reflect.DeepEqual(fresh, want.Sectors[key]) {
			t.Fatal("sector copy changed capture")
		}
	}
	owner.SetVoxel(-33, 0, 0, 3)
	generation++
	current := s1l6Capture(t, obj)
	if !old.SameSource(current) || !current.SameSource(old) || old.Generation() != 7 || current.Generation() != 8 {
		t.Fatal("generation change confused attachment identity")
	}
	obj.SetManagedGeometryProducer(obj.XBrickMap, nil)
	owner.ExposeMutable().SetVoxel(-33, 0, 0, 0)
	owner = nil
	for i, key := range [][3]int{{-2, 0, 0}, {1, 0, 0}} {
		copy, _ := old.Geometry().CopySector(i)
		if !reflect.DeepEqual(copy, want.Sectors[key]) {
			t.Fatal("historical capture lost geometry after exposure and clearing")
		}
	}
}

func TestS1l6CoreRejectsChangedDerivativeSpecialLatticeAndSelectedLOD(t *testing.T) {
	tags := map[string]func(*core.VoxelObject){
		"derivative":    func(o *core.VoxelObject) { o.XBrickMap = o.XBrickMap.Copy() },
		"terrain":       func(o *core.VoxelObject) { o.IsTerrainChunk = true },
		"planet":        func(o *core.VoxelObject) { o.IsPlanetTile = true },
		"adjacency":     func(o *core.VoxelObject) { o.VoxelAdjacencyGroupID = 1 },
		"terrain-group": func(o *core.VoxelObject) { o.TerrainGroupID = 1 },
		"planet-group":  func(o *core.VoxelObject) { o.PlanetTileGroupID = 1 },
		"selected-valid": func(o *core.VoxelObject) {
			if !o.SetRenderLOD2(o.XBrickMap.Copy()) {
				t.Fatal("LOD setup rejected")
			}
		},
		"selected-invalid": func(o *core.VoxelObject) {
			coarse := o.XBrickMap.Copy()
			if !o.SetRenderLOD2(coarse) {
				t.Fatal("LOD setup rejected")
			}
			coarse.SetVoxel(0, 0, 0, 2)
		},
	}
	for name, invalidate := range tags {
		t.Run(name, func(t *testing.T) {
			source := volume.NewXBrickMap()
			source.SetVoxel(0, 0, 0, 1)
			owner := volume.NewManagedXBrickMap(source)
			obj := core.NewVoxelObject()
			obj.XBrickMap = source.Copy()
			calls := 0
			obj.SetManagedGeometryProducer(obj.XBrickMap, func() (volume.ManagedGeometryView, uint64, bool) {
				calls++
				view, ok := owner.CaptureGeometry()
				return view, 4, ok
			})
			old := s1l6Capture(t, obj)
			invalidate(obj)
			before := calls
			s1l6Unavailable(t, obj)
			if calls != before {
				t.Fatal("invalid core selection invoked trusted producer")
			}
			if old.Geometry().Len() != 1 {
				t.Fatal("invalidation changed historical capture")
			}
		})
	}
}
