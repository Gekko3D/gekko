package volume

import (
	"testing"
)

func TestBitOps(t *testing.T) {
	mask := uint64(0b101101)

	bitTest := func(m uint64, i uint) bool {
		return (m & (1 << i)) != 0
	}

	if !bitTest(mask, 0) {
		t.Error("Bit 0 should be set")
	}
	if bitTest(mask, 1) {
		t.Error("Bit 1 should not be set")
	}
	if !bitTest(mask, 2) {
		t.Error("Bit 2 should be set")
	}
	if !bitTest(mask, 3) {
		t.Error("Bit 3 should be set")
	}
}

func TestXBrickMapIndexMath(t *testing.T) {
	xbm := NewXBrickMap()

	// Test positive coordinate
	xbm.SetVoxel(10, 10, 10, 5)
	found, val := xbm.GetVoxel(10, 10, 10)
	if !found || val != 5 {
		t.Errorf("Expected voxel at (10,10,10) to be 5, got found=%v val=%d", found, val)
	}

	// Test negative coordinate
	xbm.SetVoxel(-1, -1, -1, 3)
	found, val = xbm.GetVoxel(-1, -1, -1)
	if !found || val != 3 {
		t.Errorf("Expected voxel at (-1,-1,-1) to be 3, got found=%v val=%d", found, val)
	}

	// Verify sector calculation
	sx, sy, sz := -1/32, -1/32, -1/32
	sectorKey := [3]int{sx, sy, sz}
	if _, exists := xbm.Sectors[sectorKey]; !exists {
		t.Errorf("Sector %v should exist", sectorKey)
	}

	// Boundary case: 31, 32
	xbm.SetVoxel(31, 0, 0, 1)
	xbm.SetVoxel(32, 0, 0, 2)

	found, val = xbm.GetVoxel(31, 0, 0)
	if !found || val != 1 {
		t.Errorf("Expected voxel at (31,0,0) to be 1, got found=%v val=%d", found, val)
	}

	found, val = xbm.GetVoxel(32, 0, 0)
	if !found || val != 2 {
		t.Errorf("Expected voxel at (32,0,0) to be 2, got found=%v val=%d", found, val)
	}

	// Check both sectors exist
	if _, exists := xbm.Sectors[[3]int{0, 0, 0}]; !exists {
		t.Error("Sector (0,0,0) should exist")
	}
	if _, exists := xbm.Sectors[[3]int{1, 0, 0}]; !exists {
		t.Error("Sector (1,0,0) should exist")
	}
}

func TestDirtyTracking(t *testing.T) {
	xbm := NewXBrickMap()
	xbm.SetVoxel(0, 0, 0, 1)
	xbm.ClearDirty()

	if len(xbm.DirtyBricks) != 0 {
		t.Error("Dirty bricks should be empty after clear")
	}

	// Change voxel
	xbm.SetVoxel(0, 0, 0, 2)

	brickKey := [6]int{0, 0, 0, 0, 0, 0}
	if _, exists := xbm.DirtyBricks[brickKey]; !exists {
		t.Errorf("Brick %v should be marked dirty", brickKey)
	}

	sectorKey := [3]int{0, 0, 0}
	if _, exists := xbm.DirtySectors[sectorKey]; !exists {
		t.Errorf("Sector %v should be marked dirty", sectorKey)
	}
}

func TestHierarchyBoundaries(t *testing.T) {
	xbm := NewXBrickMap()

	// Place voxels at boundaries
	coords := []struct{ x, y, z int }{
		{0, 0, 0},
		{31, 31, 31}, // End of first sector
		{32, 0, 0},   // Start of second sector in X
		{65, 10, 10}, // Way out
	}

	for _, c := range coords {
		xbm.SetVoxel(c.x, c.y, c.z, 1)
	}

	for _, c := range coords {
		found, _ := xbm.GetVoxel(c.x, c.y, c.z)
		if !found {
			t.Errorf("Failed at boundary (%d,%d,%d)", c.x, c.y, c.z)
		}
	}
}

func TestSparseMasks(t *testing.T) {
	xbm := NewXBrickMap()

	// Add one voxel
	xbm.SetVoxel(5, 5, 5, 1)

	// Check sector exists
	sectorKey := [3]int{0, 0, 0}
	sector, exists := xbm.Sectors[sectorKey]
	if !exists {
		t.Fatal("Sector missing")
	}

	if sector.BrickMask64 == 0 {
		t.Error("Brick mask should not be empty")
	}

	// Brick coords for 5,5,5:
	// Sector local: 5,5,5. Brick size 8.
	// Brick coords: 0,0,0.
	brick := sector.GetBrick(0, 0, 0)
	if brick == nil {
		t.Fatal("Brick missing")
	}

	if brick.OccupancyMask64 == 0 {
		t.Error("Brick occupancy should not be empty")
	}

	// Remove voxel
	xbm.SetVoxel(5, 5, 5, 0)

	// Check cleanup
	sector, exists = xbm.Sectors[sectorKey]
	if exists {
		brick := sector.GetBrick(0, 0, 0)
		if brick != nil {
			t.Error("Brick should be removed after clearing")
		}
		if sector.BrickMask64 != 0 {
			t.Error("Sector mask should be cleared")
		}
	}
}

func TestAABB(t *testing.T) {
	xbm := NewXBrickMap()

	// Single voxel at 0,0,0
	xbm.SetVoxel(0, 0, 0, 1)

	minB, maxB := xbm.ComputeAABB()
	t.Logf("AABB 1: %v -> %v", minB, maxB)

	// AABB is computed at voxel level for sparse bricks
	if minB[0] != 0 || minB[1] != 0 || minB[2] != 0 {
		t.Errorf("Expected min [0,0,0], got %v", minB)
	}
	if maxB[0] != 1 || maxB[1] != 1 || maxB[2] != 1 {
		t.Errorf("Expected max [1,1,1] (voxel precision), got %v", maxB)
	}

	// Add distant voxel
	xbm.SetVoxel(100, 50, 20, 1)

	minB, maxB = xbm.ComputeAABB()
	t.Logf("AABB 2: %v -> %v", minB, maxB)

	if minB[0] > 0 || minB[1] > 0 || minB[2] > 0 {
		t.Errorf("Min bound wrong: %v", minB)
	}
	// Voxel at (100,50,20)
	if maxB[0] < 101 || maxB[1] < 51 || maxB[2] < 21 {
		t.Errorf("Max bound wrong: %v (expected at least [101,51,21])", maxB)
	}
}

func TestSolidCompression(t *testing.T) {
	xbm := NewXBrickMap()

	// Fill a whole brick (8x8x8)
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			for z := 0; z < 8; z++ {
				xbm.SetVoxel(x, y, z, 7)
			}
		}
	}

	// Check if brick is solid
	sector := xbm.Sectors[[3]int{0, 0, 0}]
	brick := sector.GetBrick(0, 0, 0)
	if brick.Flags&BrickFlagSolid == 0 {
		t.Error("Brick should be solid after full fill")
	}
	if brick.AtlasOffset != 7 {
		t.Errorf("Solid brick should store material 7 in AtlasOffset, got %d", brick.AtlasOffset)
	}

	// Check AABB - should be full brick now
	minB, maxB := xbm.ComputeAABB()
	if maxB[0] != 8 || maxB[1] != 8 || maxB[2] != 8 {
		t.Errorf("Solid brick AABB should be [8,8,8], got %v", maxB)
	}

	// Edit one voxel -> should decompress
	xbm.SetVoxel(0, 0, 0, 0)
	if brick.Flags&BrickFlagSolid != 0 {
		t.Error("Brick should be sparse after editing away from solid")
	}
	if brick.Payload[1][1][1] != 7 {
		t.Error("Decompressed brick should preserve original material")
	}

	// Check AABB again after decompression
	minB, maxB = xbm.ComputeAABB()
	// (0,0,0) is now empty. Smallest voxel is (1,0,0) etc.
	// Actually min should be (0,0,0) if we only cleared (0,0,0)?
	// Wait, if (0,0,0) is 0, minX should be 0 because (0,1,0) etc are still 7?
	// Yes. But if we clear a whole face...
	if minB[0] != 0 {
		t.Errorf("AABB min should still be 0, got %f", minB[0])
	}
}
