package volume_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func e1bFixture() *volume.XBrickMap {
	x := e1aAuxFixture()
	volume.Cube(x, mgl32.Vec3{}, mgl32.Vec3{7, 7, 7}, 3)
	x.ComputeAABB()
	for _, sector := range x.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.PrecomputedAux = []byte{1, 2, 3}
		}
	}
	return x
}

func TestE1bOrderedStreamMutatesLivePayload(t *testing.T) {
	got, want := e1bFixture(), e1bFixture()
	writes := []volume.VoxelWrite{{Value: 5}, {Value: 5}, {}, {Value: 9},
		{X: -32, Y: -1, Z: -8, Value: 7}, {X: -32, Y: -1, Z: -8}, {X: -32, Y: -1, Z: -8, Value: 8},
		{X: -64, Value: 4}, {X: -64}, {X: 8, Value: 2}, {X: 8}, {X: 40, Value: 2}}
	calls := 0
	got.ApplyVoxelWrites(func(yield func(volume.VoxelWrite) bool) {
		calls++
		for _, w := range writes {
			want.SetVoxel(w.X, w.Y, w.Z, w.Value)
			if !yield(w) {
				t.Fatal("ordered stream must consume all writes")
			}
			_, gv := got.GetVoxel(w.X, w.Y, w.Z)
			_, wv := want.GetVoxel(w.X, w.Y, w.Z)
			if gv != wv || got.Revision != want.Revision {
				t.Fatal("producer must observe applied payload/revision before yielding next write")
			}
		}
	})
	if calls != 1 {
		t.Fatal("stream must execute synchronously once")
	}
	e1aParity(t, got, want)
	got.ApplyVoxelWrites(nil)
	got.ApplyVoxelWrites(slices.Values([]volume.VoxelWrite{{Value: 9}, {X: -100}}))
	e1aParity(t, got, want)
	copy := got.Copy()
	got.ApplyVoxelWrites(slices.Values([]volume.VoxelWrite{{Value: 0}}))
	if _, value := copy.GetVoxel(0, 0, 0); value != 9 {
		t.Fatal("streamed edits must not mutate an independent copy")
	}
}

func TestE1bProducerPanicFinalizesAppliedPrefix(t *testing.T) {
	got, want := e1bFixture(), e1bFixture()
	writes := []volume.VoxelWrite{{Value: 0}, {X: 1, Value: 9}}
	for _, w := range writes {
		want.SetVoxel(w.X, w.Y, w.Z, w.Value)
	}
	func() {
		defer func() {
			if recover() != "producer panic" {
				t.Fatal("producer panic must propagate unchanged")
			}
		}()
		got.ApplyVoxelWrites(func(yield func(volume.VoxelWrite) bool) {
			for _, w := range writes {
				yield(w)
			}
			panic("producer panic")
		})
	}()
	e1aParity(t, got, want)
}

type e1bModeChangingRecorder struct{ *e1aEditRecorder }

func (r *e1bModeChangingRecorder) QueueEdit(x, y, z int, value uint8) {
	r.e1aEditRecorder.QueueEdit(x, y, z, value)
	r.x.GPUEditMode = false
}

func TestE1bGPUSequentialCallbacksAndModeChange(t *testing.T) {
	applyAndObserve := func(x *volume.XBrickMap, writes []volume.VoxelWrite) {
		x.ApplyVoxelWrites(func(yield func(volume.VoxelWrite) bool) {
			for _, write := range writes {
				yield(write)
				for _, sector := range x.Sectors {
					for _, brick := range sector.PackedBricks {
						final := brick.Copy()
						final.RefreshMaterialFlags()
						if brick.Flags != final.Flags || brick.AtlasOffset != final.AtlasOffset {
							t.Fatal("GPU-started stream must finalize each write even after callback mode changes")
						}
					}
				}
			}
		})
	}
	got, want := e1bFixture(), e1bFixture()
	gr, wr := &e1bModeChangingRecorder{&e1aEditRecorder{x: got}}, &e1bModeChangingRecorder{&e1aEditRecorder{x: want}}
	got.EnableGPUEditing(gr)
	want.EnableGPUEditing(wr)
	writes := []volume.VoxelWrite{{Value: 5}, {Value: 5}, {X: 1, Value: 0}, {X: -32, Value: 7}, {X: -32}}
	applyAndObserve(got, writes)
	for _, w := range writes {
		want.SetVoxel(w.X, w.Y, w.Z, w.Value)
	}
	e1aParity(t, got, want)
	if len(gr.observations) == 0 || !reflect.DeepEqual(gr.observations, wr.observations) {
		t.Fatal("GPU mode changes/reentry must retain sequential callback order and prewrite state")
	}
	for _, managerKind := range []string{"recorder", "nil", "nonconforming"} {
		g, w := e1bFixture(), e1bFixture()
		gr, wr := &e1aEditRecorder{x: g}, &e1aEditRecorder{x: w}
		switch managerKind {
		case "recorder":
			g.EnableGPUEditing(gr)
			w.EnableGPUEditing(wr)
		case "nil":
			g.EnableGPUEditing(nil)
			w.EnableGPUEditing(nil)
		default:
			g.EnableGPUEditing(struct{}{})
			w.EnableGPUEditing(struct{}{})
		}
		applyAndObserve(g, writes)
		for _, write := range writes {
			w.SetVoxel(write.X, write.Y, write.Z, write.Value)
		}
		e1aParity(t, g, w)
		if managerKind == "recorder" && (len(gr.observations) < 2 || !reflect.DeepEqual(gr.observations, wr.observations)) {
			t.Fatal("ordinary GPU recorder must retain multiple ordered prewrite callbacks")
		}
	}
}
