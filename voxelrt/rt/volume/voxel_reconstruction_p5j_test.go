package volume_test

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func p5jShiftFixture() *volume.XBrickMap {
	x := volume.NewXBrickMap()
	volume.Cube(x, mgl32.Vec3{-32, -8, -8}, mgl32.Vec3{-25, -1, -1}, 3)
	x.SetVoxel(-24, -8, -8, 5)
	x.SetVoxel(-23, -8, -8, 6)
	x.ComputeAABB()
	for _, sector := range x.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.PrecomputedAux = []byte{1, 2, 3}
		}
	}
	return x
}

func p5jExpectedShift(dx, dy, dz int) *volume.XBrickMap {
	x := volume.NewXBrickMap()
	// Single source sector: these are the existing brick then z/y/x write order.
	for z := -8; z < 0; z++ {
		for y := -8; y < 0; y++ {
			for gx := -32; gx < -24; gx++ {
				x.SetVoxel(gx+dx, y+dy, z+dz, 3)
			}
		}
	}
	x.SetVoxel(-24+dx, -8+dy, -8+dz, 5)
	x.SetVoxel(-23+dx, -8+dy, -8+dz, 6)
	return x
}

func TestP5jShiftAndCenterReconstructFreshGeometry(t *testing.T) {
	source := p5jShiftFixture()
	before := source.Copy()
	shifted := source.Shift(63, 9, 9)
	e1aParity(t, shifted, p5jExpectedShift(63, 9, 9))
	zero := source.Shift(0, 0, 0)
	e1aParity(t, zero, p5jExpectedShift(0, 0, 0))
	centered, center := source.Center()
	if center != (mgl32.Vec3{-27, -4, -4}) {
		t.Fatalf("local center = %v", center)
	}
	e1aParity(t, centered, p5jExpectedShift(27, 4, 4))
	if shifted == source || zero == source || centered == source || zero.ID == source.ID {
		t.Fatal("shift/center including zero shift must return fresh maps")
	}
	if !reflect.DeepEqual(source.Sectors, before.Sectors) || source.Revision != before.Revision {
		t.Fatal("reconstruction must preserve source voxels and auxiliary records")
	}
	zero.SetVoxel(-32, -8, -8, 0)
	if _, value := source.GetVoxel(-32, -8, -8); value != 3 {
		t.Fatal("editing shifted geometry changed source")
	}
}

func TestP5jResampleCenterProjectionAndAdmission(t *testing.T) {
	source := volume.NewXBrickMap()
	source.SetVoxel(-1, 0, 0, 3)
	source.SetVoxel(0, 0, 0, 5)
	source.ComputeAABB()
	before := source.Copy()
	got := source.Resample(2)
	want := volume.NewXBrickMap()
	for x := -2; x <= 1; x++ {
		for y := 0; y <= 1; y++ {
			for z := 0; z <= 1; z++ {
				value := uint8(5)
				if x < 0 {
					value = 3
				}
				want.SetVoxel(x, y, z, value)
			}
		}
	}
	want.ComputeAABB()
	e1aParity(t, got, want)
	if got.GetVoxelCount() != 16 || got.CachedMin != (mgl32.Vec3{-2, 0, 0}) || got.CachedMax != (mgl32.Vec3{2, 2, 2}) {
		t.Fatal("resampling changed center-aligned material coverage or tight bounds")
	}
	if source.Resample(2000) != source {
		t.Fatal("iteration-limit rejection must return the original source")
	}
	empty := volume.NewXBrickMap()
	if fresh := empty.Resample(2); fresh == empty || fresh.GetVoxelCount() != 0 || !fresh.StructureDirty {
		t.Fatal("empty resampling must retain fresh empty output behavior")
	}
	if !reflect.DeepEqual(source.Sectors, before.Sectors) || source.Revision != before.Revision {
		t.Fatal("resampling must leave source geometry unchanged")
	}
	got.SetVoxel(-2, 0, 0, 0)
	if _, value := source.GetVoxel(-1, 0, 0); value != 3 {
		t.Fatal("resampled edits must be independent")
	}
}

func TestP5jSplitReconstructionPreservesPartitionAndBounds(t *testing.T) {
	source := volume.NewXBrickMap()
	volume.Cube(source, mgl32.Vec3{}, mgl32.Vec3{7, 7, 7}, 3)
	source.SetVoxel(16, 0, 0, 5)
	source.SetVoxel(17, 0, 0, 6)
	for _, sector := range source.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.PrecomputedAux = []byte{4, 5}
		}
	}
	before := source.Copy()
	components := source.SplitDisconnectedComponents()
	if len(components) != 2 {
		t.Fatalf("component count = %d, want 2", len(components))
	}
	for _, component := range components {
		want := volume.NewXBrickMap()
		if component.Min.X() == 0 {
			volume.Cube(want, mgl32.Vec3{}, mgl32.Vec3{7, 7, 7}, 3)
		} else {
			want.SetVoxel(16, 0, 0, 5)
			want.SetVoxel(17, 0, 0, 6)
		}
		min, max := want.ComputeAABB()
		if component.VoxelCount != want.GetVoxelCount() || component.Min != min || component.Max != max || component.Map.AABBDirty {
			t.Fatal("component reconstruction changed captured count/bounds")
		}
		e1aParity(t, component.Map, want)
	}
	if !reflect.DeepEqual(source.Sectors, before.Sectors) || source.Revision != before.Revision {
		t.Fatal("split extraction changed source geometry or auxiliary normals")
	}
	components[0].Map.SetVoxel(0, 0, 0, 0)
	if _, value := source.GetVoxel(0, 0, 0); value != 3 {
		t.Fatal("component maps must own editable geometry independently")
	}
}
