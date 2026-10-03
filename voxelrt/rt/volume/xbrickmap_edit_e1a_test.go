package volume_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// These retain the public primitive predicates/order and use SetVoxel as the
// independent sequential edit contract.
func e1aSphere(x *volume.XBrickMap, center mgl32.Vec3, radius float32, value uint8) {
	for gx := int(math.Floor(float64(center.X() - radius))); gx <= int(math.Ceil(float64(center.X()+radius))); gx++ {
		for gy := int(math.Floor(float64(center.Y() - radius))); gy <= int(math.Ceil(float64(center.Y()+radius))); gy++ {
			for gz := int(math.Floor(float64(center.Z() - radius))); gz <= int(math.Ceil(float64(center.Z()+radius))); gz++ {
				dx, dy, dz := float32(gx)-center.X()+0.5, float32(gy)-center.Y()+0.5, float32(gz)-center.Z()+0.5
				if dx*dx+dy*dy+dz*dz <= radius*radius {
					x.SetVoxel(gx, gy, gz, value)
				}
			}
		}
	}
}

func e1aCube(x *volume.XBrickMap, lo, hi mgl32.Vec3, value uint8) {
	for gx := int(math.Floor(float64(lo.X()))); gx <= int(math.Floor(float64(hi.X()))); gx++ {
		for gy := int(math.Floor(float64(lo.Y()))); gy <= int(math.Floor(float64(hi.Y()))); gy++ {
			for gz := int(math.Floor(float64(lo.Z()))); gz <= int(math.Floor(float64(hi.Z()))); gz++ {
				x.SetVoxel(gx, gy, gz, value)
			}
		}
	}
}

func e1aParity(t *testing.T, got, want *volume.XBrickMap) {
	t.Helper()
	if got.Revision != want.Revision || !reflect.DeepEqual(got.SectorRevisions, want.SectorRevisions) || got.GetVoxelCount() != want.GetVoxelCount() {
		t.Fatal("primitive changed content revisions, sector tombstones or voxel count")
	}
	// Public dense brick fields encode authoritative voxels and GPU material/occupancy inputs.
	if !reflect.DeepEqual(got.Sectors, want.Sectors) {
		t.Fatal("primitive changed voxels, brick membership, masks, material flags or auxiliary bytes")
	}
	if got.StructureDirty != want.StructureDirty || !reflect.DeepEqual(got.DirtySectors, want.DirtySectors) || !reflect.DeepEqual(got.DirtyBricks, want.DirtyBricks) {
		t.Fatal("primitive changed existing upload dirtiness or exact normal halo coverage")
	}
	if got.AABBDirty != want.AABBDirty || got.CachedMin != want.CachedMin || got.CachedMax != want.CachedMax {
		t.Fatal("primitive changed cached bound invalidation/expansion")
	}
	gm, gx := got.ComputeAABB()
	wm, wx := want.ComputeAABB()
	if gm != wm || gx != wx {
		t.Fatal("primitive changed authoritative bounds")
	}
}

func TestE1aOrderedPrimitiveParity(t *testing.T) {
	got, want := volume.NewXBrickMap(), volume.NewXBrickMap()
	lo, hi := mgl32.Vec3{-33.2, -1.2, -8.2}, mgl32.Vec3{-30.1, 1.9, -6.1}
	volume.Cube(got, lo, hi, 4)
	e1aCube(want, lo, hi, 4)
	e1aParity(t, got, want)
	volume.Cube(got, hi, lo, 8)
	e1aCube(want, hi, lo, 8)
	e1aParity(t, got, want)
	for _, value := range []uint8{4, 7, 0, 9} {
		center := mgl32.Vec3{-31.5, -0.5, -7.5}
		volume.Sphere(got, center, 2.25, value)
		e1aSphere(want, center, 2.25, value)
		e1aParity(t, got, want)
	}
	volume.Cube(got, mgl32.Vec3{-40, -8, -16}, mgl32.Vec3{-24, 8, 0}, 0)
	e1aCube(want, mgl32.Vec3{-40, -8, -16}, mgl32.Vec3{-24, 8, 0}, 0)
	e1aParity(t, got, want)
	if len(got.Sectors) != 0 || len(got.SectorRevisions) == 0 {
		t.Fatal("full carve must retain sector revision tombstones without occupied sectors")
	}
	// Full uniform brick becomes mixed, then sparse, then full again.
	for _, value := range []uint8{3, 3, 5, 0, 3} {
		if value == 5 || value == 0 {
			volume.Sphere(got, mgl32.Vec3{0.5, 0.5, 0.5}, 0, value)
			e1aSphere(want, mgl32.Vec3{0.5, 0.5, 0.5}, 0, value)
		} else {
			volume.Cube(got, mgl32.Vec3{}, mgl32.Vec3{7.9, 7.9, 7.9}, value)
			e1aCube(want, mgl32.Vec3{}, mgl32.Vec3{7.9, 7.9, 7.9}, value)
		}
		e1aParity(t, got, want)
	}
}

func e1aAuxFixture() *volume.XBrickMap {
	x := volume.NewXBrickMap()
	for _, gx := range []int{7, 8, 40} {
		x.SetVoxel(gx, 0, 0, 2)
	}
	x.ComputeAABB()
	x.ClearDirty()
	x.DirtySectors[[3]int{9, 9, 9}] = true
	x.DirtyBricks[[6]int{9, 9, 9, 0, 0, 0}] = true
	for _, sector := range x.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.PrecomputedAux = []byte{1, 2, 3}
		}
	}
	return x
}

func TestE1aAuxBoundsAndCopyIsolation(t *testing.T) {
	got, want := e1aAuxFixture(), e1aAuxFixture()
	independent := got.Copy()
	volume.Cube(got, mgl32.Vec3{7, 0, 0}, mgl32.Vec3{7, 0, 0}, 2)
	e1aCube(want, mgl32.Vec3{7, 0, 0}, mgl32.Vec3{7, 0, 0}, 2)
	e1aParity(t, got, want) // No-op must preserve precomputed normals and dirtiness.
	volume.Sphere(got, mgl32.Vec3{7.5, 0.5, 0.5}, 0, 5)
	e1aSphere(want, mgl32.Vec3{7.5, 0.5, 0.5}, 0, 5)
	e1aParity(t, got, want)
	if got.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux != nil || got.Sectors[[3]int{}].GetBrick(1, 0, 0).PrecomputedAux != nil {
		t.Fatal("changed brick and normal halo neighbor must invalidate authored auxiliary normals")
	}
	if len(got.Sectors[[3]int{1, 0, 0}].GetBrick(1, 0, 0).PrecomputedAux) == 0 {
		t.Fatal("outside-halo auxiliary normals must remain")
	}
	e1aParity(t, independent, e1aAuxFixture().Copy())
	volume.Cube(got, mgl32.Vec3{40, 0, 0}, mgl32.Vec3{40, 0, 0}, 0)
	e1aCube(want, mgl32.Vec3{40, 0, 0}, mgl32.Vec3{40, 0, 0}, 0)
	e1aParity(t, got, want)
}

type e1aCallbackObservation struct {
	Coordinate      [3]int
	Value, Before   uint8
	Revision        uint64
	Flags, Material uint32
}

type e1aEditRecorder struct {
	x            *volume.XBrickMap
	observations []e1aCallbackObservation
	reentered    bool
}

func (r *e1aEditRecorder) QueueEdit(x, y, z int, value uint8) {
	_, before := r.x.GetVoxel(x, y, z)
	o := e1aCallbackObservation{Coordinate: [3]int{x, y, z}, Value: value, Before: before, Revision: r.x.Revision}
	if sector := r.x.Sectors[[3]int{}]; sector != nil {
		if brick := sector.GetBrick(0, 0, 0); brick != nil {
			o.Flags, o.Material = brick.Flags, brick.AtlasOffset
		}
	}
	r.observations = append(r.observations, o)
	if !r.reentered {
		r.reentered = true
		r.x.SetVoxel(40, 0, 0, 6)
	}
}

func TestE1aGPUEditCallbackCompatibility(t *testing.T) {
	got, want := e1aAuxFixture(), e1aAuxFixture()
	gr, wr := &e1aEditRecorder{x: got}, &e1aEditRecorder{x: want}
	got.EnableGPUEditing(gr)
	want.EnableGPUEditing(wr)
	lo, hi := mgl32.Vec3{0, 0, 0}, mgl32.Vec3{1, 1, 1}
	volume.Cube(got, lo, hi, 3)
	e1aCube(want, lo, hi, 3)
	volume.Sphere(got, mgl32.Vec3{0.5, 0.5, 0.5}, 1.5, 5)
	e1aSphere(want, mgl32.Vec3{0.5, 0.5, 0.5}, 1.5, 5)
	e1aParity(t, got, want)
	if len(gr.observations) == 0 || !reflect.DeepEqual(gr.observations, wr.observations) {
		t.Fatal("GPU callbacks changed coordinate order, revision increment or prewrite voxel/material state")
	}
	before := len(gr.observations)
	volume.Sphere(got, mgl32.Vec3{0.5, 0.5, 0.5}, 0, 5)
	if len(gr.observations) != before {
		t.Fatal("no-op primitive must not queue GPU edits")
	}
	for _, manager := range []any{nil, struct{}{}} {
		g, w := e1aAuxFixture(), e1aAuxFixture()
		g.EnableGPUEditing(manager)
		w.EnableGPUEditing(manager)
		volume.Cube(g, lo, hi, 3)
		e1aCube(w, lo, hi, 3)
		volume.Sphere(g, mgl32.Vec3{7.5, 0.5, 0.5}, 0, 0)
		e1aSphere(w, mgl32.Vec3{7.5, 0.5, 0.5}, 0, 0)
		e1aParity(t, g, w)
	}
}
