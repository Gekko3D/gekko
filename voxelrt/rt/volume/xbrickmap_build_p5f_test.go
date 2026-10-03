package volume_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func p5fSequential(writes []volume.VoxelWrite) *volume.XBrickMap {
	x := volume.NewXBrickMap()
	for _, w := range writes {
		x.SetVoxel(w.X, w.Y, w.Z, w.Value)
	}
	return x
}

func p5fSameMap(t *testing.T, got, want *volume.XBrickMap) {
	t.Helper()
	if got == nil || got.GetVoxelCount() != want.GetVoxelCount() || got.Revision != want.Revision || !reflect.DeepEqual(got.SectorRevisions, want.SectorRevisions) {
		t.Fatal("builder changed voxel count or content/sector revision semantics")
	}
	for x := -34; x <= 40; x++ {
		for y := -2; y <= 8; y++ {
			for z := -2; z <= 8; z++ {
				_, gv := got.GetVoxel(x, y, z)
				_, wv := want.GetVoxel(x, y, z)
				if gv != wv {
					t.Fatalf("voxel (%d,%d,%d) = %d, want %d", x, y, z, gv, wv)
				}
			}
		}
	}
	gm, gx := got.ComputeAABB()
	wm, wx := want.ComputeAABB()
	if gm != wm || gx != wx {
		t.Fatalf("bounds = %v..%v, want %v..%v", gm, gx, wm, wx)
	}
}

func TestP5fBuildOrderedWritesAndSnapshots(t *testing.T) {
	writes := []volume.VoxelWrite{
		{X: -33, Value: 3}, {X: -32, Value: 4}, {X: -1, Value: 5},
		{X: 0, Value: 6}, {X: 7, Value: 7}, {X: 8, Value: 8},
		{X: 31, Value: 9}, {X: 32, Value: 10},
		{X: -33, Value: 3}, {X: -33, Value: 0}, {X: -33, Value: 11},
		{X: 32, Value: 0}, {X: 32, Value: 0}, {X: 40, Value: 0},
		{X: 7, Value: 12}, {X: 8, Value: 0},
		{X: -8, Y: -32, Z: -1, Value: 14}, {X: 1, Y: -1, Z: -32, Value: 15},
	}
	calls, yielded := 0, 0
	got := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		calls++
		for _, w := range writes {
			yielded++
			if !yield(w) {
				return
			}
		}
	})
	if calls != 1 || yielded != len(writes) {
		t.Fatal("builder must consume its ordered input synchronously once")
	}
	want := p5fSequential(writes)
	p5fSameMap(t, got, want)
	for _, position := range [][3]int{{-8, -32, -1}, {1, -1, -32}} {
		_, gv := got.GetVoxel(position[0], position[1], position[2])
		_, wv := want.GetVoxel(position[0], position[1], position[2])
		if gv != wv {
			t.Fatalf("negative-axis voxel %v = %d, want %d", position, gv, wv)
		}
	}
	for key, dirty := range want.DirtySectors {
		if dirty && !got.DirtySectors[key] {
			t.Fatalf("missing initial/removed sector upload coverage at %v", key)
		}
	}
	for key, dirty := range want.DirtyBricks {
		if dirty && !got.DirtySectors[[3]int{key[0], key[1], key[2]}] && !got.DirtyBricks[key] {
			t.Fatalf("missing normal halo upload coverage at %v", key)
		}
	}
	previous := p5fSequential(writes[:8]).Copy()
	p5fSameMap(t, got.CopyChangedSectors(previous, previous.Revision), want)
	copy := got.Copy()
	got.SetVoxel(-33, 0, 0, 0)
	copy.SetVoxel(40, 0, 0, 13)
	if _, value := copy.GetVoxel(-33, 0, 0); value != 11 {
		t.Fatal("editing built map changed independent copy")
	}
	if _, value := got.GetVoxel(40, 0, 0); value != 0 {
		t.Fatal("editing copy changed built map")
	}
	deleted := []volume.VoxelWrite{{X: -1, Value: 2}, {X: -1}}
	empty := volume.BuildXBrickMap(slices.Values(deleted))
	p5fSameMap(t, empty, p5fSequential(deleted))
	if len(empty.Sectors) != 0 {
		t.Fatal("full deletion must leave no occupied sector")
	}
	p5fSameMap(t, volume.BuildXBrickMap(nil), volume.NewXBrickMap())
	p5fSameMap(t, volume.BuildXBrickMap(slices.Values([]volume.VoxelWrite{{X: -1}})), volume.NewXBrickMap())
}

func TestP5fBuildMaterialFlagsAndRaycast(t *testing.T) {
	var writes []volume.VoxelWrite
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			for z := 0; z < 8; z++ {
				writes = append(writes, volume.VoxelWrite{X: x, Y: y, Z: z, Value: 3})
			}
		}
	}
	writes = append(writes, volume.VoxelWrite{X: 8, Value: 4}, volume.VoxelWrite{X: 16, Value: 5}, volume.VoxelWrite{X: 17, Value: 6})
	got, want := volume.BuildXBrickMap(slices.Values(writes)), p5fSequential(writes)
	p5fSameMap(t, got, want)
	sector := got.Sectors[[3]int{}]
	for index, flags := range []uint32{volume.BrickFlagSolid, volume.BrickFlagUniformMaterial, 0} {
		brick := sector.GetBrick(index, 0, 0)
		if brick == nil || brick.Flags != flags {
			t.Fatalf("brick %d material flags did not preserve full/uniform/mixed semantics", index)
		}
	}
	origin, direction := mgl32.Vec3{-2, 0.5, 0.5}, mgl32.Vec3{1, 0, 0}
	gh, gt, gp, gn := got.RayMarch(origin, direction, 0, 30)
	wh, wt, wp, wn := want.RayMarch(origin, direction, 0, 30)
	if !gh || gh != wh || gt != wt || gp != wp || gn != wn {
		t.Fatal("bulk construction changed authoritative raycast")
	}
	// A full brick must expand correctly when carved and repainted.
	for _, tail := range [][]volume.VoxelWrite{{{X: 0, Value: 0}}, {{X: 0, Value: 0}, {X: 0, Value: 9}}} {
		edited := append(slices.Clone(writes), tail...)
		built, sequential := volume.BuildXBrickMap(slices.Values(edited)), p5fSequential(edited)
		p5fSameMap(t, built, sequential)
		bb, sb := built.Sectors[[3]int{}].GetBrick(0, 0, 0), sequential.Sectors[[3]int{}].GetBrick(0, 0, 0)
		if bb.Flags != sb.Flags || bb.AtlasOffset != sb.AtlasOffset || bb.OccupancyMask64 != sb.OccupancyMask64 {
			t.Fatal("carve/repaint changed shared GPU brick encoding")
		}
		for _, start := range []mgl32.Vec3{origin, {15, 0.5, 0.5}} {
			bh, bt, bp, bn := built.RayMarch(start, direction, 0, 30)
			sh, st, sp, sn := sequential.RayMarch(start, direction, 0, 30)
			if !bh || bh != sh || bt != st || bp != sp || bn != sn {
				t.Fatal("carved/mixed brick raycast differs from ordered writes")
			}
		}
	}
}
