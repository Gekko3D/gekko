package content

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type LevelValidationSeverity string

const (
	LevelValidationSeverityError LevelValidationSeverity = "error"
)

type LevelValidationIssue struct {
	Severity      LevelValidationSeverity `json:"severity"`
	Code          string                  `json:"code"`
	Message       string                  `json:"message"`
	PlacementID   string                  `json:"placement_id,omitempty"`
	PlacementPath string                  `json:"placement_path,omitempty"`
	MarkerID      string                  `json:"marker_id,omitempty"`
}

type LevelValidationOptions struct {
	DocumentPath string
}

type LevelValidationResult struct {
	Issues         []LevelValidationIssue `json:"issues,omitempty"`
	HardErrorCount int                    `json:"hard_error_count"`
}

func (r LevelValidationResult) HasErrors() bool {
	return r.HardErrorCount > 0
}

func (r LevelValidationResult) FirstError() (LevelValidationIssue, bool) {
	for _, issue := range r.Issues {
		if issue.Severity == LevelValidationSeverityError {
			return issue, true
		}
	}
	return LevelValidationIssue{}, false
}

func (r LevelValidationResult) Error() string {
	if issue, ok := r.FirstError(); ok {
		return issue.Message
	}
	return ""
}

func ValidateLevel(def *LevelDef, opts LevelValidationOptions) LevelValidationResult {
	result := LevelValidationResult{}
	if def == nil {
		result.addError("nil_level", "level definition is nil", "", "", "")
		return result
	}

	if strings.TrimSpace(def.Name) == "" {
		result.addError("empty_name", "level name is required", "", "", "")
	}

	seenPlacementIDs := map[string]struct{}{}
	for _, placement := range def.Placements {
		validateLevelUniqueID(&result, seenPlacementIDs, placement.ID, placement.AssetPath, "")
		if strings.TrimSpace(placement.AssetPath) == "" {
			result.addError("empty_asset_path", "placement asset_path is required", placement.ID, placement.AssetPath, "")
		}
		if !isValidPlacementMode(placement.PlacementMode) {
			result.addError("invalid_placement_mode", fmt.Sprintf("unsupported placement mode %q", placement.PlacementMode), placement.ID, placement.AssetPath, "")
		}
		validatePlacementAssetPath(&result, placement, opts)
	}

	seenMarkerIDs := map[string]struct{}{}
	for _, marker := range def.Markers {
		validateLevelUniqueID(&result, seenMarkerIDs, marker.ID, "", marker.ID)
		if strings.TrimSpace(marker.Name) == "" {
			result.addError("empty_marker_name", "level marker name is required", "", "", marker.ID)
		}
		if strings.TrimSpace(marker.Kind) == "" {
			result.addError("empty_marker_kind", "level marker kind is required", "", "", marker.ID)
		}
	}

	validateLevelTerrain(&result, def.Terrain, opts)

	return result
}

func (r *LevelValidationResult) addError(code string, message string, placementID string, placementPath string, markerID string) {
	r.Issues = append(r.Issues, LevelValidationIssue{
		Severity:      LevelValidationSeverityError,
		Code:          code,
		Message:       message,
		PlacementID:   placementID,
		PlacementPath: placementPath,
		MarkerID:      markerID,
	})
	r.HardErrorCount++
}

func validateLevelUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string, placementPath string, markerID string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_id", fmt.Sprintf("duplicate id %s", id), id, placementPath, markerID)
		return
	}
	seen[id] = struct{}{}
}

func isValidPlacementMode(mode LevelPlacementMode) bool {
	switch mode {
	case LevelPlacementModeSurfaceSnap, LevelPlacementModePlaneSnap, LevelPlacementModeFree3D:
		return true
	default:
		return false
	}
}

func validatePlacementAssetPath(result *LevelValidationResult, placement LevelPlacementDef, opts LevelValidationOptions) {
	if strings.TrimSpace(placement.AssetPath) == "" || opts.DocumentPath == "" {
		return
	}

	resolvedPath := ResolveDocumentPath(placement.AssetPath, opts.DocumentPath)
	if _, err := os.Stat(resolvedPath); err != nil {
		result.addError("missing_asset_file", fmt.Sprintf("missing asset file %s", placement.AssetPath), placement.ID, placement.AssetPath, "")
	}
}

func validateLevelTerrain(result *LevelValidationResult, terrain *LevelTerrainDef, opts LevelValidationOptions) {
	if terrain == nil {
		return
	}
	if terrain.Kind != TerrainKindHeightfield {
		result.addError("invalid_terrain_kind", fmt.Sprintf("unsupported terrain kind %q", terrain.Kind), "", "", "")
	}
	if strings.TrimSpace(terrain.SourcePath) == "" {
		result.addError("empty_terrain_source_path", "terrain source_path is required", "", "", "")
		return
	}

	resolvedPath := ResolveDocumentPath(terrain.SourcePath, opts.DocumentPath)
	if strings.ToLower(filepath.Ext(resolvedPath)) != ".gkterrain" {
		result.addError("invalid_terrain_source_path", fmt.Sprintf("terrain source_path must point to a .gkterrain: %s", terrain.SourcePath), "", "", "")
		return
	}

	if _, err := os.Stat(resolvedPath); err != nil {
		result.addError("missing_terrain_source_file", fmt.Sprintf("missing terrain source file %s", terrain.SourcePath), "", "", "")
		return
	}

	terrainDef, err := LoadTerrainSource(resolvedPath)
	if err != nil {
		result.addError("invalid_terrain_source", fmt.Sprintf("failed to load terrain source %s: %v", terrain.SourcePath, err), "", "", "")
		return
	}
	terrainValidation := ValidateTerrainSource(terrainDef, TerrainValidationOptions{DocumentPath: resolvedPath})
	for _, issue := range terrainValidation.Issues {
		result.addError("invalid_terrain_source", issue.Message, "", "", "")
	}
}
