package core_test

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s1l10CoreUnavailable(t *testing.T, obj *core.VoxelObject, expected core.ManagedGeometryInput, coord [3]int) {
	t.Helper()
	got, ok := obj.CaptureManagedGeometrySector(expected, coord)
	sector, copied := got.Sector().CopySector()
	if ok || got.Generation() != 0 || got.SameSource(expected) || got.Sector().Coord() != [3]int{} || got.Sector().Present() || got.Sector().RetainedBytes() != 0 || got.Sector().CopyBytes() != 0 || copied || sector != nil {
		t.Fatal("refusal did not return zero sector input")
	}
}

func TestS1l10CoreLazyDualCallbacksCurrentGenerationAndAbsence(t *testing.T) {
	var zero core.ManagedGeometrySectorInput
	var zeroFull core.ManagedGeometryInput
	if zero.Generation() != 0 || zero.SameSource(zeroFull) || zero.Sector().Present() {
		t.Fatal("zero sector input has identity or data")
	}
	var nilObject *core.VoxelObject
	s1l10CoreUnavailable(t, nilObject, zeroFull, [3]int{})
	obj := core.NewVoxelObject()
	s1l10CoreUnavailable(t, obj, zeroFull, [3]int{})
	source := volume.NewXBrickMap()
	source.SetVoxel(0, 0, 0, 1)
	owner := volume.NewManagedXBrickMap(source)
	obj.XBrickMap = source.Copy()
	fullCalls, readCalls := 0, 0
	generation := uint64(0)
	capture := func() (volume.ManagedGeometryView, uint64, bool) {
		fullCalls++
		view, ok := owner.CaptureGeometry()
		return view, generation, ok
	}
	read := func(coord [3]int) (volume.ManagedSectorView, uint64, bool) {
		readCalls++
		view, ok := owner.CaptureSector(coord)
		return view, generation, ok
	}
	obj.SetManagedGeometryProducerWithSectorReader(obj.XBrickMap, capture, read)
	if fullCalls != 0 || readCalls != 0 {
		t.Fatal("installation invoked callbacks")
	}
	old := s1l6Capture(t, obj)
	first, ok := obj.CaptureManagedGeometrySector(old, [3]int{})
	if !ok || !first.SameSource(old) || first.Generation() != 0 || !first.Sector().Present() || readCalls != 1 || fullCalls != 1 {
		t.Fatal("same-generation sector capture unavailable or not lazy")
	}
	want, _ := first.Sector().CopySector()
	owner.SetVoxel(64, 0, 0, 4)
	generation = 8
	// New topology is permitted here; accepted-coordinate filtering belongs to S1l9.
	newer, ok := obj.CaptureManagedGeometrySector(old, [3]int{2, 0, 0})
	if !ok || !newer.SameSource(old) || newer.Generation() != 8 || !newer.Sector().Present() || fullCalls != 1 {
		t.Fatal("old attachment could not read newer unaccepted coordinate without full recapture")
	}
	owner.SetVoxel(0, 0, 0, 0)
	generation++
	absent, ok := obj.CaptureManagedGeometrySector(old, [3]int{})
	if !ok || !absent.SameSource(old) || absent.Generation() != 9 || absent.Sector().Present() || absent.Sector().CopyBytes() != 0 || absent.Sector().RetainedBytes() != 0 {
		t.Fatal("current removal did not produce available tombstone")
	}
	if copy, ok := absent.Sector().CopySector(); ok || copy != nil {
		t.Fatal("tombstone copied a sector")
	}
	obj.SetManagedGeometryProducerWithSectorReader(obj.XBrickMap, capture, read)
	replacement := s1l6Capture(t, obj)
	if replacement.SameSource(old) {
		t.Fatal("dual installation reused source")
	}
	calls := readCalls
	s1l10CoreUnavailable(t, obj, old, [3]int{})
	s1l10CoreUnavailable(t, obj, zeroFull, [3]int{})
	if readCalls != calls {
		t.Fatal("old/zero attachment invoked reader")
	}
	obj.SetManagedGeometryProducer(obj.XBrickMap, nil)
	if copy, _ := first.Sector().CopySector(); !reflect.DeepEqual(copy, want) || !first.SameSource(old) {
		t.Fatal("historical input changed after replacement/clearing")
	}
	s1l10CoreUnavailable(t, obj, replacement, [3]int{})
}

func TestS1l10CoreReaderAvailabilityAndNumericRollback(t *testing.T) {
	for _, kind := range []string{"legacy-setter", "missing-reader", "refused", "coord", "rollback", "wrap", "nil-full"} {
		t.Run(kind, func(t *testing.T) {
			owner := volume.NewManagedXBrickMap(nil)
			obj := core.NewVoxelObject()
			obj.XBrickMap = volume.NewXBrickMap()
			fullCalls, reads := 0, 0
			generation := uint64(7)
			if kind == "wrap" {
				generation = ^uint64(0)
			}
			capture := func() (volume.ManagedGeometryView, uint64, bool) {
				fullCalls++
				view, ok := owner.CaptureGeometry()
				return view, generation, ok
			}
			read := func(coord [3]int) (volume.ManagedSectorView, uint64, bool) {
				reads++
				if kind == "coord" {
					coord[0]++
				}
				view, ok := owner.CaptureSector(coord)
				return view, generation, ok && kind != "refused"
			}
			switch kind {
			case "legacy-setter":
				obj.SetManagedGeometryProducer(obj.XBrickMap, capture)
			case "missing-reader":
				obj.SetManagedGeometryProducerWithSectorReader(obj.XBrickMap, capture, nil)
			default:
				obj.SetManagedGeometryProducerWithSectorReader(obj.XBrickMap, capture, read)
			}
			expected := s1l6Capture(t, obj)
			switch kind {
			case "rollback":
				generation = 6
			case "wrap":
				generation = 0
			case "nil-full":
				obj.SetManagedGeometryProducerWithSectorReader(obj.XBrickMap, nil, read)
				s1l6Unavailable(t, obj)
			}
			s1l10CoreUnavailable(t, obj, expected, [3]int{-2, 3, 4})
			if fullCalls != 1 {
				t.Fatal("sector read fell back to full capture")
			}
			wantReads := 1
			if kind == "legacy-setter" || kind == "missing-reader" || kind == "nil-full" {
				wantReads = 0
			}
			if reads != wantReads {
				t.Fatalf("reader calls=%d want=%d", reads, wantReads)
			}
		})
	}
}

func TestS1l10CoreSourceMismatchAvoidsReader(t *testing.T) {
	owner := volume.NewManagedXBrickMap(nil)
	capture := func() (volume.ManagedGeometryView, uint64, bool) { v, ok := owner.CaptureGeometry(); return v, 3, ok }
	first, second := core.NewVoxelObject(), core.NewVoxelObject()
	first.XBrickMap, second.XBrickMap = volume.NewXBrickMap(), volume.NewXBrickMap()
	calls := 0
	read := func(c [3]int) (volume.ManagedSectorView, uint64, bool) {
		calls++
		v, ok := owner.CaptureSector(c)
		return v, 3, ok
	}
	first.SetManagedGeometryProducerWithSectorReader(first.XBrickMap, capture, read)
	second.SetManagedGeometryProducerWithSectorReader(second.XBrickMap, capture, read)
	wrong := s1l6Capture(t, first)
	s1l10CoreUnavailable(t, second, wrong, [3]int{})
	if calls != 0 {
		t.Fatal("foreign attachment invoked reader")
	}
}

func TestS1l10CoreRechecksSelectionAndAttachmentAroundReader(t *testing.T) {
	for _, phase := range []string{"before", "after"} {
		for _, kind := range []string{"derivative", "nil-derivative", "gpu", "terrain", "planet", "adjacency", "terrain-group", "planet-group", "lod", "invalid-lod", "reinstall", "clear"} {
			t.Run(phase+"/"+kind, func(t *testing.T) {
				source := volume.NewXBrickMap()
				source.SetVoxel(0, 0, 0, 1)
				owner := volume.NewManagedXBrickMap(source)
				obj := core.NewVoxelObject()
				obj.XBrickMap = source.Copy()
				capture := func() (volume.ManagedGeometryView, uint64, bool) { v, ok := owner.CaptureGeometry(); return v, 4, ok }
				calls := 0
				var invalidate func()
				read := func(coord [3]int) (volume.ManagedSectorView, uint64, bool) {
					calls++
					v, ok := owner.CaptureSector(coord)
					if phase == "after" {
						invalidate()
					}
					return v, 4, ok
				}
				invalidate = func() {
					switch kind {
					case "derivative":
						obj.XBrickMap = obj.XBrickMap.Copy()
					case "nil-derivative":
						obj.XBrickMap = nil
					case "gpu":
						obj.XBrickMap.GPUEditMode = true
					case "terrain":
						obj.IsTerrainChunk = true
					case "planet":
						obj.IsPlanetTile = true
					case "adjacency":
						obj.VoxelAdjacencyGroupID = 1
					case "terrain-group":
						obj.TerrainGroupID = 1
					case "planet-group":
						obj.PlanetTileGroupID = 1
					case "lod", "invalid-lod":
						coarse := obj.XBrickMap.Copy()
						if !obj.SetRenderLOD2(coarse) {
							t.Fatal("LOD setup rejected")
						}
						if kind == "invalid-lod" {
							coarse.SetVoxel(0, 0, 0, 2)
						}
					case "reinstall":
						obj.SetManagedGeometryProducerWithSectorReader(obj.XBrickMap, capture, read)
					case "clear":
						obj.SetManagedGeometryProducer(obj.XBrickMap, nil)
					}
				}
				obj.SetManagedGeometryProducerWithSectorReader(obj.XBrickMap, capture, read)
				expected := s1l6Capture(t, obj)
				if phase == "before" {
					invalidate()
				}
				s1l10CoreUnavailable(t, obj, expected, [3]int{})
				want := 0
				if phase == "after" {
					want = 1
				}
				if calls != want {
					t.Fatalf("reader calls=%d want=%d", calls, want)
				}
			})
		}
	}
}
