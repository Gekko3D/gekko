package volume

import (
	"testing"
)

func TestBrickDenseOccupancyWordsPackVoxelOrder(t *testing.T) {
	brick := NewBrick()
	brick.SetVoxel(0, 0, 0, 1)
	brick.SetVoxel(7, 0, 0, 2)
	brick.SetVoxel(0, 1, 0, 3)
	brick.SetVoxel(0, 0, 1, 4)
	brick.SetVoxel(7, 7, 7, 5)

	words := brick.DenseOccupancyWords()

	if got := words[0]; got != (1<<0 | 1<<7 | 1<<8) {
		t.Fatalf("unexpected first dense occupancy word: got %#x", got)
	}
	if got := words[2]; got != 1<<0 {
		t.Fatalf("unexpected dense occupancy word 2: got %#x", got)
	}
	if got := words[15]; got != 1<<31 {
		t.Fatalf("unexpected dense occupancy word 15: got %#x", got)
	}

	for i, word := range words {
		if i == 0 || i == 2 || i == 15 {
			continue
		}
		if word != 0 {
			t.Fatalf("expected dense occupancy word %d to be empty, got %#x", i, word)
		}
	}
}

func TestRefreshMaterialFlagsMarksSolidBrick(t *testing.T) {
	brick := NewBrick()
	for z := 0; z < BrickSize; z++ {
		for y := 0; y < BrickSize; y++ {
			for x := 0; x < BrickSize; x++ {
				brick.SetVoxel(x, y, z, 7)
			}
		}
	}

	if !brick.RefreshMaterialFlags() {
		t.Fatal("expected solid brick classification to return true")
	}
	if brick.Flags&BrickFlagSolid == 0 {
		t.Fatalf("expected solid flag to be set, got %#x", brick.Flags)
	}
	if brick.Flags&BrickFlagUniformMaterial != 0 {
		t.Fatalf("expected uniform sparse flag to be cleared for solid brick, got %#x", brick.Flags)
	}
	if brick.AtlasOffset != 7 {
		t.Fatalf("expected atlas offset to store solid palette 7, got %d", brick.AtlasOffset)
	}
}

func TestRefreshMaterialFlagsMarksUniformSparseBrick(t *testing.T) {
	brick := NewBrick()
	brick.SetVoxel(0, 0, 0, 4)
	brick.SetVoxel(3, 2, 1, 4)
	brick.SetVoxel(7, 7, 7, 4)

	if brick.RefreshMaterialFlags() {
		t.Fatal("expected sparse uniform brick classification to return false for non-solid brick")
	}
	if brick.Flags&BrickFlagSolid != 0 {
		t.Fatalf("expected solid flag to be cleared, got %#x", brick.Flags)
	}
	if brick.Flags&BrickFlagUniformMaterial == 0 {
		t.Fatalf("expected uniform sparse flag to be set, got %#x", brick.Flags)
	}
	if brick.AtlasOffset != 4 {
		t.Fatalf("expected atlas offset to store uniform palette 4, got %d", brick.AtlasOffset)
	}
}

func TestRefreshMaterialFlagsFallsBackToPayloadSparse(t *testing.T) {
	brick := NewBrick()
	brick.SetVoxel(0, 0, 0, 2)
	brick.SetVoxel(7, 7, 7, 5)

	if brick.RefreshMaterialFlags() {
		t.Fatal("expected mixed sparse brick classification to stay non-solid")
	}
	if brick.Flags != 0 {
		t.Fatalf("expected mixed sparse brick to keep no special flags, got %#x", brick.Flags)
	}
	if brick.AtlasOffset != 0 {
		t.Fatalf("expected payload sparse brick to clear atlas offset, got %d", brick.AtlasOffset)
	}
}

func TestSetVoxelRefreshesBrickClassificationAcrossTransitions(t *testing.T) {
	xbm := NewXBrickMap()
	for z := 0; z < BrickSize; z++ {
		for y := 0; y < BrickSize; y++ {
			for x := 0; x < BrickSize; x++ {
				xbm.SetVoxel(x, y, z, 6)
			}
		}
	}

	sector := xbm.Sectors[[3]int{0, 0, 0}]
	brick := sector.GetBrick(0, 0, 0)
	if brick == nil {
		t.Fatal("expected brick to exist after filling first brick")
	}
	if brick.Flags&BrickFlagSolid == 0 {
		t.Fatalf("expected brick to compress to solid, got %#x", brick.Flags)
	}

	xbm.SetVoxel(0, 0, 0, 0)
	if brick.Flags&BrickFlagSolid != 0 {
		t.Fatalf("expected solid flag to clear after deleting one voxel, got %#x", brick.Flags)
	}
	if brick.Flags&BrickFlagUniformMaterial == 0 {
		t.Fatalf("expected sparse uniform flag after deletion, got %#x", brick.Flags)
	}
	if brick.AtlasOffset != 6 {
		t.Fatalf("expected uniform sparse atlas offset 6 after deletion, got %d", brick.AtlasOffset)
	}

	xbm.SetVoxel(1, 0, 0, 3)
	if brick.Flags != 0 {
		t.Fatalf("expected mixed sparse brick to fall back to payload mode, got %#x", brick.Flags)
	}
	if brick.AtlasOffset != 0 {
		t.Fatalf("expected payload sparse transition to clear atlas offset, got %d", brick.AtlasOffset)
	}
}

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
