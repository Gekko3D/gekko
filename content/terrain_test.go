package content

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestTerrainSourceRoundTripPreservesSamplesAndMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	pngPath := filepath.Join(tmpDir, "height.png")
	writeTerrainPNG(t, pngPath, [][]uint16{
		{0, 32768},
		{65535, 16384},
	})

	def, err := ImportHeightmapPNG(pngPath, "terrain", Vec2{64, 96}, 18, 0.5, 16)
	if err != nil {
		t.Fatalf("ImportHeightmapPNG failed: %v", err)
	}

	path := filepath.Join(tmpDir, "terrain.gkterrain")
	if err := SaveTerrainSource(path, def); err != nil {
		t.Fatalf("SaveTerrainSource failed: %v", err)
	}

	loaded, err := LoadTerrainSource(path)
	if err != nil {
		t.Fatalf("LoadTerrainSource failed: %v", err)
	}

	if loaded.SchemaVersion != CurrentTerrainSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", CurrentTerrainSchemaVersion, loaded.SchemaVersion)
	}
	if loaded.SampleWidth != 2 || loaded.SampleHeight != 2 {
		t.Fatalf("expected 2x2 heightmap, got %dx%d", loaded.SampleWidth, loaded.SampleHeight)
	}
	if len(loaded.HeightSamples) != 4 || loaded.HeightSamples[2] != 65535 {
		t.Fatalf("unexpected height samples: %+v", loaded.HeightSamples)
	}
	if loaded.ImportSource == nil || loaded.ImportSource.PNGPath != pngPath || loaded.ImportSource.SourceHash == "" {
		t.Fatalf("expected import metadata to round-trip, got %+v", loaded.ImportSource)
	}
}

func TestLoadHeightmapPNGReadsGray16Samples(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "height.png")
	writeTerrainPNG(t, path, [][]uint16{
		{0, 65535},
		{32768, 16384},
	})

	width, height, samples, hash, err := LoadHeightmapPNG(path)
	if err != nil {
		t.Fatalf("LoadHeightmapPNG failed: %v", err)
	}

	if width != 2 || height != 2 {
		t.Fatalf("expected 2x2 image, got %dx%d", width, height)
	}
	if want := []uint16{0, 65535, 32768, 16384}; len(samples) != len(want) || samples[1] != want[1] || samples[2] != want[2] {
		t.Fatalf("unexpected samples: %+v", samples)
	}
	if hash == "" {
		t.Fatal("expected source hash")
	}
}

func TestValidateTerrainSourceRejectsBrokenData(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "terrain.gkterrain")
	def := &TerrainSourceDef{
		Name:            "",
		Kind:            TerrainKind("bad"),
		SampleWidth:     4,
		SampleHeight:    2,
		HeightSamples:   []uint16{1, 2},
		WorldSize:       Vec2{0, 16},
		HeightScale:     0,
		VoxelResolution: 0,
		ChunkSize:       0,
		ImportSource: &TerrainImportDef{
			PNGPath: "missing.jpg",
		},
	}

	result := ValidateTerrainSource(def, TerrainValidationOptions{DocumentPath: path})
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	assertHasTerrainValidationCode(t, result, "empty_name")
	assertHasTerrainValidationCode(t, result, "invalid_kind")
	assertHasTerrainValidationCode(t, result, "invalid_sample_count")
	assertHasTerrainValidationCode(t, result, "invalid_world_size")
	assertHasTerrainValidationCode(t, result, "invalid_height_scale")
	assertHasTerrainValidationCode(t, result, "invalid_voxel_resolution")
	assertHasTerrainValidationCode(t, result, "invalid_chunk_size")
	assertHasTerrainValidationCode(t, result, "invalid_import_source")
}

func TestLoadTerrainSourceRejectsUnknownSchemaVersion(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad.gkterrain")
	if err := os.WriteFile(path, []byte(`{"id":"1","schema_version":99,"name":"bad","kind":"heightfield"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTerrainSource(path); err == nil {
		t.Fatal("expected LoadTerrainSource to reject unsupported schema_version")
	}
}

func assertHasTerrainValidationCode(t *testing.T, result TerrainValidationResult, want string) {
	t.Helper()
	for _, issue := range result.Issues {
		if issue.Code == want {
			return
		}
	}
	t.Fatalf("expected terrain validation code %q, got %+v", want, result.Issues)
}

func writeTerrainPNG(t *testing.T, path string, rows [][]uint16) {
	t.Helper()
	if len(rows) == 0 || len(rows[0]) == 0 {
		t.Fatal("rows must be non-empty")
	}
	img := image.NewGray16(image.Rect(0, 0, len(rows[0]), len(rows)))
	for y := range rows {
		for x := range rows[y] {
			img.SetGray16(x, y, color.Gray16{Y: rows[y][x]})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
}
