package volume_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Derive ordinary coordinates independently with floor division, including the
// negative faces where truncating integer division would give the wrong sector.
func s1l11HaloExpected(p [3]int) map[[3]int]bool {
	result := map[[3]int]bool{}
	var lo, hi [3]int
	for i, v := range p {
		lo[i] = int(math.Floor(float64(v-volume.VoxelNormalExtendedSurfaceFitRadius) / float64(volume.SectorSize)))
		hi[i] = int(math.Floor(float64(v+volume.VoxelNormalExtendedSurfaceFitRadius) / float64(volume.SectorSize)))
	}
	for x := lo[0]; x <= hi[0]; x++ {
		for y := lo[1]; y <= hi[1]; y++ {
			for z := lo[2]; z <= hi[2]; z++ {
				result[[3]int{x, y, z}] = true
			}
		}
	}
	return result
}

func TestS1l11VolumeHaloOrdinaryFacesEdgesCorners(t *testing.T) {
	for _, p := range [][3]int{{16, 16, 16}, {31, 16, 16}, {31, 31, 16}, {31, 31, 31}, {32, 32, 32}, {-1, -16, -16}, {-1, -1, -16}, {-1, -1, -1}, {-32, -32, -32}, {-33, 31, -1}} {
		got := map[[3]int]bool{}
		var first [3]int
		n := 0
		volume.VisitVoxelNormalHaloSectors(p[0], p[1], p[2], func(c [3]int) bool {
			if n == 0 {
				first = c
			}
			n++
			if got[c] {
				t.Fatalf("%v duplicate %v", p, c)
			}
			got[c] = true
			return true
		})
		var target [3]int
		for i, v := range p {
			target[i] = int(math.Floor(float64(v) / float64(volume.SectorSize)))
		}
		if first != target || n > 8 || !reflect.DeepEqual(got, s1l11HaloExpected(p)) {
			t.Fatalf("%v first=%v visits=%v", p, first, got)
		}
	}
}

func TestS1l11VolumeHaloNilEarlyStopAndSignedLimits(t *testing.T) {
	volume.VisitVoxelNormalHaloSectors(31, 31, 31, nil)
	for stop := 1; stop <= 8; stop++ {
		calls := 0
		volume.VisitVoxelNormalHaloSectors(31, 31, 31, func([3]int) bool { calls++; return calls < stop })
		if calls != stop {
			t.Fatalf("stop=%d calls=%d", stop, calls)
		}
	}
	max := int(^uint(0) >> 1)
	min := -max - 1
	for _, p := range [][3]int{{min, min, min}, {max, max, max}, {min, max, 0}, {max, 0, min}} {
		calls := 0
		seen := map[[3]int]bool{}
		volume.VisitVoxelNormalHaloSectors(p[0], p[1], p[2], func(c [3]int) bool {
			calls++
			if calls == 1 && c != [3]int{p[0] / volume.SectorSize, p[1] / volume.SectorSize, p[2] / volume.SectorSize} {
				t.Fatalf("missing edge target %v: %v", p, c)
			}
			if calls > 8 || seen[c] {
				t.Fatalf("unbounded/duplicate edge visit %v", c)
			}
			seen[c] = true
			return true
		})
		if calls == 0 {
			t.Fatal("target not visited")
		}
	}
}

func TestS1l11VolumeHaloAllocatesNothing(t *testing.T) {
	calls := 0
	visit := func([3]int) bool { calls++; return true }
	if n := testing.AllocsPerRun(100, func() { volume.VisitVoxelNormalHaloSectors(31, -1, 32, visit) }); n != 0 {
		t.Fatalf("allocations=%g", n)
	}
	if calls == 0 {
		t.Fatal("visitor unused")
	}
}
