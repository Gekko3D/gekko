package content

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type TerrainValidationSeverity string

const (
	TerrainValidationSeverityError TerrainValidationSeverity = "error"
)

type TerrainValidationIssue struct {
	Severity TerrainValidationSeverity `json:"severity"`
	Code     string                    `json:"code"`
	Message  string                    `json:"message"`
}

type TerrainValidationOptions struct {
	DocumentPath string
}

type TerrainValidationResult struct {
	Issues         []TerrainValidationIssue `json:"issues,omitempty"`
	HardErrorCount int                      `json:"hard_error_count"`
}

func (r TerrainValidationResult) HasErrors() bool {
	return r.HardErrorCount > 0
}

func (r *TerrainValidationResult) addError(code string, message string) {
	r.Issues = append(r.Issues, TerrainValidationIssue{
		Severity: TerrainValidationSeverityError,
		Code:     code,
		Message:  message,
	})
	r.HardErrorCount++
}

func ValidateTerrainSource(def *TerrainSourceDef, opts TerrainValidationOptions) TerrainValidationResult {
	result := TerrainValidationResult{}
	if def == nil {
		result.addError("nil_terrain", "terrain definition is nil")
		return result
	}
	if strings.TrimSpace(def.Name) == "" {
		result.addError("empty_name", "terrain name is required")
	}
	if def.Kind != TerrainKindHeightfield {
		result.addError("invalid_kind", fmt.Sprintf("unsupported terrain kind %q", def.Kind))
	}
	if def.SampleWidth <= 0 || def.SampleHeight <= 0 {
		result.addError("invalid_dimensions", "terrain sample_width and sample_height must be positive")
	}
	if want := def.SampleWidth * def.SampleHeight; want > 0 && len(def.HeightSamples) != want {
		result.addError("invalid_sample_count", fmt.Sprintf("terrain height_samples must contain %d samples", want))
	}
	if def.WorldSize[0] <= 0 || def.WorldSize[1] <= 0 {
		result.addError("invalid_world_size", "terrain world_size must be positive")
	}
	if def.HeightScale <= 0 {
		result.addError("invalid_height_scale", "terrain height_scale must be positive")
	}
	if def.VoxelResolution <= 0 {
		result.addError("invalid_voxel_resolution", "terrain voxel_resolution must be positive")
	}
	if def.ChunkSize <= 0 {
		result.addError("invalid_chunk_size", "terrain chunk_size must be positive")
	}
	if def.ImportSource != nil && strings.TrimSpace(def.ImportSource.PNGPath) != "" {
		pngPath := ResolveDocumentPath(def.ImportSource.PNGPath, opts.DocumentPath)
		if strings.ToLower(filepath.Ext(pngPath)) != ".png" {
			result.addError("invalid_import_source", "terrain import_source.png_path must point to a .png")
		} else if _, err := os.Stat(pngPath); err != nil {
			result.addError("missing_import_png", fmt.Sprintf("missing terrain import png %s", def.ImportSource.PNGPath))
		}
	}
	return result
}
