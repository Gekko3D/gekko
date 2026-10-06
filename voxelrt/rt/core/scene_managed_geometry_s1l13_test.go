package core_test

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"testing"
)

func TestS1l13ScalarGenerationReaderIsLazyAndDoesNotCapture(t *testing.T) {
	raw := volume.NewXBrickMap()
	raw.SetVoxel(0, 0, 0, 1)
	owner := volume.NewManagedXBrickMap(raw)
	obj := core.NewVoxelObject()
	obj.XBrickMap = raw.Copy()
	full, sector, scalar := 0, 0, 0
	generation, qualified := uint64(7), true
	obj.SetManagedGeometryProducerWithGenerationReader(obj.XBrickMap,
		func() (volume.ManagedGeometryView, uint64, bool) {
			full++
			v, ok := owner.CaptureGeometry()
			return v, generation, ok
		},
		func(c [3]int) (volume.ManagedSectorView, uint64, bool) {
			sector++
			v, ok := owner.CaptureSector(c)
			return v, generation, ok
		},
		func() (uint64, bool) { scalar++; return generation, qualified })
	if full+sector+scalar != 0 {
		t.Fatal("installation invoked producer")
	}
	in, ok := obj.CaptureManagedGeometryInput()
	if !ok {
		t.Fatal("capture")
	}
	for _, g := range []uint64{7, 9, ^uint64(0)} {
		generation = g
		got, ok := obj.CurrentManagedGeometryGeneration(in)
		if !ok || got != g {
			t.Fatalf("scalar %d/%v want %d", got, ok, g)
		}
	}
	if full != 1 || sector != 0 || scalar != 3 {
		t.Fatalf("callbacks full/sector/scalar %d/%d/%d", full, sector, scalar)
	}
	for _, g := range []uint64{6, 0} {
		generation = g
		got, ok := obj.CurrentManagedGeometryGeneration(in)
		if ok || got != 0 {
			t.Fatal("rollback qualified")
		}
	}
	generation = 9
	qualified = false
	if g, ok := obj.CurrentManagedGeometryGeneration(in); ok || g != 0 {
		t.Fatal("unavailable not zero/false")
	}
}

func TestS1l13ScalarReaderMissingAndInvalidSourcesNeverFallback(t *testing.T) {
	for _, kind := range []string{"nil", "zero", "foreign", "missing-scalar", "nil-full", "derivative", "gpu", "terrain", "lod", "cleared"} {
		t.Run(kind, func(t *testing.T) {
			raw := volume.NewXBrickMap()
			raw.SetVoxel(0, 0, 0, 1)
			owner := volume.NewManagedXBrickMap(raw)
			obj := core.NewVoxelObject()
			obj.XBrickMap = raw
			captures, reads := 0, 0
			capture := func() (volume.ManagedGeometryView, uint64, bool) {
				captures++
				v, ok := owner.CaptureGeometry()
				return v, 4, ok
			}
			sector := func(c [3]int) (volume.ManagedSectorView, uint64, bool) {
				t.Fatal("scalar fell back to sector")
				return volume.ManagedSectorView{}, 0, false
			}
			scalar := func() (uint64, bool) { reads++; return 5, true }
			obj.SetManagedGeometryProducerWithGenerationReader(raw, capture, sector, scalar)
			in, _ := obj.CaptureManagedGeometryInput()
			switch kind {
			case "nil":
				obj = nil
			case "zero":
				in = core.ManagedGeometryInput{}
			case "foreign":
				other := core.NewVoxelObject()
				other.XBrickMap = raw
				other.SetManagedGeometryProducer(raw, capture)
				in, _ = other.CaptureManagedGeometryInput()
			case "missing-scalar":
				obj.SetManagedGeometryProducerWithSectorReader(raw, capture, sector)
				in, _ = obj.CaptureManagedGeometryInput()
			case "nil-full":
				obj.SetManagedGeometryProducerWithGenerationReader(raw, nil, sector, scalar)
			case "derivative":
				obj.XBrickMap = raw.Copy()
			case "gpu":
				raw.GPUEditMode = true
			case "terrain":
				obj.IsTerrainChunk = true
			case "lod":
				obj.SetRenderLOD2(raw.Copy())
			case "cleared":
				obj.SetManagedGeometryProducer(nil, nil)
			}
			before := captures
			if g, ok := obj.CurrentManagedGeometryGeneration(in); ok || g != 0 {
				t.Fatal("invalid source qualified")
			}
			if captures != before || reads != 0 {
				t.Fatal("early refusal invoked callback")
			}
		})
	}
}

func TestS1l13ScalarReaderRechecksSelectionAndAttachmentAfterCallback(t *testing.T) {
	for _, kind := range []string{"reinstall", "clear", "derivative", "gpu", "panic"} {
		t.Run(kind, func(t *testing.T) {
			raw := volume.NewXBrickMap()
			owner := volume.NewManagedXBrickMap(raw)
			obj := core.NewVoxelObject()
			obj.XBrickMap = raw
			capture := func() (volume.ManagedGeometryView, uint64, bool) { v, ok := owner.CaptureGeometry(); return v, 4, ok }
			sentinel := &struct{ n int }{1}
			obj.SetManagedGeometryProducerWithGenerationReader(raw, capture, nil, func() (uint64, bool) {
				switch kind {
				case "reinstall":
					obj.SetManagedGeometryProducer(raw, capture)
				case "clear":
					obj.SetManagedGeometryProducer(nil, nil)
				case "derivative":
					obj.XBrickMap = raw.Copy()
				case "gpu":
					raw.GPUEditMode = true
				case "panic":
					panic(sentinel)
				}
				return 5, true
			})
			in, _ := obj.CaptureManagedGeometryInput()
			if kind == "panic" {
				defer func() {
					if recover() != sentinel {
						t.Fatal("panic changed")
					}
				}()
			}
			if g, ok := obj.CurrentManagedGeometryGeneration(in); ok || g != 0 {
				t.Fatal("callback-invalidated source qualified")
			}
		})
	}
}
