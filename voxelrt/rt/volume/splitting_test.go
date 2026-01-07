package volume

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestSplitDisconnectedComponents_Basic(t *testing.T) {
	xbm := NewXBrickMap()

	// Block 1: (0,0,0) to (2,2,2)
	Cube(xbm, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{3, 3, 3}, 1)

	// Block 2: (5,0,0) to (7,2,2)
	Cube(xbm, mgl32.Vec3{5, 0, 0}, mgl32.Vec3{8, 3, 3}, 1)

	// They are separated by gap of 2 voxels (x=3, x=4 empty).

	comps := xbm.SplitDisconnectedComponents()
	if len(comps) != 2 {
		t.Errorf("Expected 2 components, got %d", len(comps))
	}
}

func TestSplitDisconnectedComponents_Bridge(t *testing.T) {
	xbm := NewXBrickMap()

	// Block 1
	Cube(xbm, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{3, 3, 3}, 1)
	// Block 2
	Cube(xbm, mgl32.Vec3{5, 0, 0}, mgl32.Vec3{8, 3, 3}, 1)

	// Bridge: (3,0,0) and (4,0,0)
	xbm.SetVoxel(3, 0, 0, 1)
	xbm.SetVoxel(4, 0, 0, 1)

	comps := xbm.SplitDisconnectedComponents()
	// Should be 1 component (or nil optimization)
	if comps != nil && len(comps) != 1 {
		t.Errorf("Expected nil or 1 component, got %d", len(comps))
	}
}

func TestSplitDisconnectedComponents_Diagonal(t *testing.T) {
	xbm := NewXBrickMap()
	// Voxel A
	xbm.SetVoxel(0, 0, 0, 1)
	// Voxel B at diagonal (1,1,0) - No 6-way connection
	xbm.SetVoxel(1, 1, 0, 1)

	comps := xbm.SplitDisconnectedComponents()
	if len(comps) != 2 {
		t.Errorf("Expected 2 components for diagonal arrangement, got %d", len(comps))
	}
}

func TestSplitDisconnectedComponents_SolidBricks(t *testing.T) {
	xbm := NewXBrickMap()

	// Force solid bricks manually?
	// Cube likely creates solid bricks if 8x8x8.
	// BrickSize is 8.
	// Let's create two 8x8x8 cubes at (0,0,0) and (10,0,0).
	// They should optimize to Solid bricks.

	Cube(xbm, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{8, 8, 8}, 1)
	Cube(xbm, mgl32.Vec3{10, 0, 0}, mgl32.Vec3{18, 8, 8}, 1)

	// Verify they are solid
	s1, found := xbm.Sectors[[3]int{0, 0, 0}]
	if !found {
		t.Fatal("Sector 0 missing")
	}
	b1 := s1.GetBrick(0, 0, 0)
	if b1.Flags&BrickFlagSolid == 0 {
		t.Log("Warning: Brick 1 not optimized to solid automatically. Forcing...")
		b1.TryCompress()
	}

	s2, found := xbm.Sectors[[3]int{0, 0, 0}]
	// (10,0,0) is in same sector? SectorSize=32. Yes.
	// 10 / 8 = 1 remainder 2. Brick x=1.
	b2 := s2.GetBrick(1, 0, 0)
	if b2 == nil {
		t.Fatal("Brick 2 missing")
	}
	// Gap at x=8,9. So b2 starts at x=10? No.
	// 8x8x8 cube at 0..8 fills brick 0 (0..7).
	// Wait. 0..8 is 8 voxels? 0,1,2,3,4,5,6,7.
	// MaxB is exclusive in Cube function?
	// Cube: x <= maxI[0]. maxI = floor(maxB).
	// 0..8 is 9 voxels!
	// BrickSize is 8.

	// Let's use exact coordinates 0..7
	xbm2 := NewXBrickMap()
	Cube(xbm2, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{7.9, 7.9, 7.9}, 1) // 0..7
	// This fits in brick 0.

	// Next brick at generic distance.
	Cube(xbm2, mgl32.Vec3{10, 0, 0}, mgl32.Vec3{17.9, 7.9, 7.9}, 1)

	// Try compress
	s := xbm2.Sectors[[3]int{0, 0, 0}]
	s.GetBrick(0, 0, 0).TryCompress() // Force solid

	comps := xbm2.SplitDisconnectedComponents()
	if len(comps) != 2 {
		t.Errorf("Expected 2 components with solid bricks, got %d", len(comps))
	}
}

func TestSplitDisconnectedComponents_CarvedSolid(t *testing.T) {
	xbm := NewXBrickMap()
	// Create larger block that becomes solid
	Cube(xbm, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{15.9, 7.9, 7.9}, 1) // 16x8x8 (2 bricks wide)

	// Compress
	xbm.Sectors[[3]int{0, 0, 0}].GetBrick(0, 0, 0).TryCompress()
	xbm.Sectors[[3]int{0, 0, 0}].GetBrick(1, 0, 0).TryCompress()

	// Now carve a slice in the middle to split them
	// Cut x=7 and x=8?
	// Brick 0: 0..7. Brick 1: 8..15.
	// Cut x=7 (boundary of brick 0).
	// Cut x=8 (boundary of brick 1).

	for y := 0; y < 8; y++ {
		for z := 0; z < 8; z++ {
			xbm.SetVoxel(7, y, z, 0)
			xbm.SetVoxel(8, y, z, 0)
		}
	}

	// Now we should have 0..6 (Brick 0 modified) and 9..15 (Brick 1 modified).
	// Gap at 7,8.
	// Should be 2 components.

	comps := xbm.SplitDisconnectedComponents()
	if len(comps) != 2 {
		t.Errorf("Expected 2 components after carving solid bricks, got %d", len(comps))
	}
}

func TestSplitDisconnectedComponents_MultiSector(t *testing.T) {
	xbm := NewXBrickMap()
	// Sector 0: 0..31. Sector 1: 32..63.
	// Place voxel at 31 (Sector 0) and 32 (Sector 1).
	xbm.SetVoxel(31, 0, 0, 1)
	xbm.SetVoxel(32, 0, 0, 1)

	// Should be 1 component
	comps := xbm.SplitDisconnectedComponents()
	if comps != nil && len(comps) != 1 {
		t.Errorf("Expected 1 component spanning sectors, got %d", len(comps))
	}

	// Now remove 32. Add 33.
	xbm.SetVoxel(32, 0, 0, 0)
	xbm.SetVoxel(33, 0, 0, 1) // Gap at 32

	comps = xbm.SplitDisconnectedComponents()
	if len(comps) != 2 {
		t.Errorf("Expected 2 components with gap at sector boundary, got %d", len(comps))
	}
}

func TestSplitDisconnectedComponents_NegativeCoords(t *testing.T) {
	xbm := NewXBrickMap()
	// Place voxels at negative coords
	xbm.SetVoxel(-10, 0, 0, 1)
	xbm.SetVoxel(-9, 0, 0, 1)
	// Gap at -8
	xbm.SetVoxel(-7, 0, 0, 1)

	comps := xbm.SplitDisconnectedComponents()
	comps = xbm.SplitDisconnectedComponents()
	if len(comps) != 2 {
		t.Errorf("Expected 2 components with negative coords, got %d", len(comps))
	}
}

func TestSplitDisconnectedComponents_LargeGap(t *testing.T) {
	xbm := NewXBrickMap()
	xbm.SetVoxel(0, 0, 0, 1)
	xbm.SetVoxel(150, 0, 0, 1)

	comps := xbm.SplitDisconnectedComponents()
	if len(comps) != 2 {
		t.Errorf("Expected 2 components with large gap, got %d", len(comps))
	}
}

func TestSplitDisconnectedComponents_Limit(t *testing.T) {
	xbm := NewXBrickMap()
	// Create AABB > 4M voxels.
	// 200 * 200 * 100 = 4,000,000.
	// Let's go slightly larger. 200 * 200 * 101 = 4,040,000.
	xbm.SetVoxel(0, 0, 0, 1)
	xbm.SetVoxel(199, 199, 100, 1)

	comps := xbm.SplitDisconnectedComponents()
	if comps != nil {
		t.Errorf("Expected nil due to volume limit > 4M, got components")
	}
}

func TestSplit_SphereCut(t *testing.T) {
	xbm := NewXBrickMap()
	// Create a long bar: 20x5x5
	Cube(xbm, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{20, 5, 5}, 1)

	// Cut it in half with a sphere of air at center
	// Center (10, 2.5, 2.5). Radius > 2.5 to cut through 5x5 width.
	// Try Radius 6 (guaranteed cut for 5x5, diagonal ~7).
	Sphere(xbm, mgl32.Vec3{10, 2.5, 2.5}, 6.0, 0)

	// Should be 2 components
	comps := xbm.SplitDisconnectedComponents()
	if len(comps) != 2 {
		t.Errorf("Expected 2 components after sphere cut, got %d", len(comps))

		// Debug: check middle voxels
		found, val := xbm.GetVoxel(10, 2, 2)
		t.Logf("Center voxel (10,2,2) exists? %v val=%d", found, val)
	}
}

func TestGetVoxelCount(t *testing.T) {
	xbm := NewXBrickMap()
	if xbm.GetVoxelCount() != 0 {
		t.Errorf("Expected 0 voxels for new map, got %d", xbm.GetVoxelCount())
	}

	xbm.SetVoxel(0, 0, 0, 1)
	if xbm.GetVoxelCount() != 1 {
		t.Errorf("Expected 1 voxel, got %d", xbm.GetVoxelCount())
	}

	xbm.SetVoxel(0, 0, 0, 0)
	if xbm.GetVoxelCount() != 0 {
		t.Errorf("Expected 0 voxels after clearing, got %d", xbm.GetVoxelCount())
	}
}
