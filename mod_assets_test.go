package gekko

import (
	"testing"
)

func TestScaleVoxModel_Upscale(t *testing.T) {
	model := VoxModel{
		SizeX: 2, SizeY: 2, SizeZ: 2,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 1},
		},
	}

	scaled := ScaleVoxModel(model, 2.0)

	if scaled.SizeX != 4 || scaled.SizeY != 4 || scaled.SizeZ != 4 {
		t.Errorf("Expected size 4x4x4, got %dx%dx%d", scaled.SizeX, scaled.SizeY, scaled.SizeZ)
	}

	// 1 voxel at 2x should become 2x2x2 = 8 voxels
	if len(scaled.Voxels) != 8 {
		t.Errorf("Expected 8 voxels, got %d", len(scaled.Voxels))
	}

	for _, v := range scaled.Voxels {
		if v.X >= 2 || v.Y >= 2 || v.Z >= 2 {
			t.Errorf("Voxel at (%d, %d, %d) is outside expected range [0, 1]", v.X, v.Y, v.Z)
		}
		if v.ColorIndex != 1 {
			t.Errorf("Expected color index 1, got %d", v.ColorIndex)
		}
	}
}

func TestScaleVoxModel_Downscale(t *testing.T) {
	model := VoxModel{
		SizeX: 4, SizeY: 4, SizeZ: 4,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 1},
			{X: 1, Y: 0, Z: 0, ColorIndex: 2}, // maps to (0,0,0) at 0.5x
			{X: 0, Y: 1, Z: 0, ColorIndex: 1}, // maps to (0,0,0) at 0.5x
			{X: 1, Y: 1, Z: 0, ColorIndex: 1}, // maps to (0,0,0) at 0.5x
		},
	}

	scaled := ScaleVoxModel(model, 0.5)

	if scaled.SizeX != 2 || scaled.SizeY != 2 || scaled.SizeZ != 2 {
		t.Errorf("Expected size 2x2x2, got %dx%dx%d", scaled.SizeX, scaled.SizeY, scaled.SizeZ)
	}

	// Should have 1 voxel at (0,0,0) with color 1 (max count)
	if len(scaled.Voxels) != 1 {
		t.Errorf("Expected 1 voxel, got %d", len(scaled.Voxels))
	}

	v := scaled.Voxels[0]
	if v.X != 0 || v.Y != 0 || v.Z != 0 {
		t.Errorf("Expected voxel at (0,0,0), got (%d, %d, %d)", v.X, v.Y, v.Z)
	}
	if v.ColorIndex != 1 {
		t.Errorf("Expected color index 1 (winner of 3 vs 1), got %d", v.ColorIndex)
	}
}

func TestScaleVoxModel_Identity(t *testing.T) {
	model := VoxModel{
		SizeX: 2, SizeY: 2, SizeZ: 2,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 1},
		},
	}

	scaled := ScaleVoxModel(model, 1.0)

	if len(scaled.Voxels) != 1 {
		t.Errorf("Expected 1 voxel, got %d", len(scaled.Voxels))
	}
}
