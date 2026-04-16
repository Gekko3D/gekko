package gpu

import "testing"

func TestCAVolumeBudgetConfigWithDefaults(t *testing.T) {
	cfg := (CAVolumeBudgetConfig{}).WithDefaults()
	def := DefaultCAVolumeBudgetConfig()

	if cfg != def {
		t.Fatalf("expected zero config to normalize to defaults, got %+v want %+v", cfg, def)
	}
}

func TestComputeCAAtlasDimensionsReportsCellsAndBytes(t *testing.T) {
	atlasW, atlasH, atlasD, visibleCount, cellCount, byteCount := computeCAAtlasDimensions([]CAVolumeHost{
		{Resolution: [3]uint32{12, 18, 10}, Intensity: 1},
		{Resolution: [3]uint32{16, 14, 7}, Intensity: 0},
	})

	if atlasW != 16 || atlasH != 18 || atlasD != 18 {
		t.Fatalf("unexpected atlas dimensions: %d x %d x %d", atlasW, atlasH, atlasD)
	}
	if visibleCount != 1 {
		t.Fatalf("expected one visible volume, got %d", visibleCount)
	}
	if cellCount != uint64(16*18*18) {
		t.Fatalf("unexpected atlas cell count %d", cellCount)
	}
	if byteCount != cellCount*16 {
		t.Fatalf("expected 16 bytes per atlas cell, got %d for %d cells", byteCount, cellCount)
	}
}
