package volume

import (
	"testing"
)

func TestSplitDisconnectedComponents(t *testing.T) {
	xbm := NewXBrickMap()

	// Create two separated cubes
	// Cube 1: (0,0,0) to (2,2,2)
	for x := 0; x < 3; x++ {
		for y := 0; y < 3; y++ {
			for z := 0; z < 3; z++ {
				xbm.SetVoxel(x, y, z, 1)
			}
		}
	}

	// Cube 2: (10,0,0) to (12,2,2)
	for x := 10; x < 13; x++ {
		for y := 0; y < 3; y++ {
			for z := 0; z < 3; z++ {
				xbm.SetVoxel(x, y, z, 2)
			}
		}
	}

	components := xbm.SplitDisconnectedComponents()
	if len(components) != 2 {
		t.Errorf("Expected 2 components, got %d", len(components))
	}

	// Verify counts (3x3x3 = 27 voxels each)
	for i, comp := range components {
		if comp.VoxelCount != 27 {
			t.Errorf("Component %d: expected 27 voxels, got %d", i, comp.VoxelCount)
		}
	}

	// Test case 3: Single bridge between cubes
	xbm.SetVoxel(3, 0, 0, 1)
	xbm.SetVoxel(4, 0, 0, 1)
	xbm.SetVoxel(5, 0, 0, 1)
	xbm.SetVoxel(6, 0, 0, 1)
	xbm.SetVoxel(7, 0, 0, 1)
	xbm.SetVoxel(8, 0, 0, 1)
	xbm.SetVoxel(9, 0, 0, 1)

	components = xbm.SplitDisconnectedComponents()
	if len(components) != 0 { // SplitDisconnectedComponents returns nil if only 1 component
		t.Errorf("Expected 0 (nil) components for merged cubes, got %d", len(components))
	}

	// Test case 4: Break bridge
	xbm.SetVoxel(5, 0, 0, 0)
	components = xbm.SplitDisconnectedComponents()
	if len(components) != 2 {
		t.Errorf("Expected 2 components after breaking bridge, got %d", len(components))
	}
}
