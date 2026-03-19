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
	VolumeID      string                  `json:"volume_id,omitempty"`
	VolumePath    string                  `json:"volume_path,omitempty"`
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
		result.addError("nil_level", "level definition is nil", "", "", "", "", "")
		return result
	}

	if strings.TrimSpace(def.Name) == "" {
		result.addError("empty_name", "level name is required", "", "", "", "", "")
	}

	seenPlacementIDs := map[string]struct{}{}
	for _, placement := range def.Placements {
		validateLevelUniqueID(&result, seenPlacementIDs, placement.ID, placement.AssetPath, "")
		if strings.TrimSpace(placement.AssetPath) == "" {
			result.addError("empty_asset_path", "placement asset_path is required", placement.ID, placement.AssetPath, "", "", "")
		}
		if !isValidPlacementMode(placement.PlacementMode) {
			result.addError("invalid_placement_mode", fmt.Sprintf("unsupported placement mode %q", placement.PlacementMode), placement.ID, placement.AssetPath, "", "", "")
		}
		validatePlacementAssetPath(&result, placement, opts)
	}

	seenMarkerIDs := map[string]struct{}{}
	for _, marker := range def.Markers {
		validateLevelUniqueID(&result, seenMarkerIDs, marker.ID, "", marker.ID)
		if strings.TrimSpace(marker.Name) == "" {
			result.addError("empty_marker_name", "level marker name is required", "", "", "", "", marker.ID)
		}
		if strings.TrimSpace(marker.Kind) == "" {
			result.addError("empty_marker_kind", "level marker kind is required", "", "", "", "", marker.ID)
		}
	}

	seenVolumeIDs := map[string]struct{}{}
	for _, volume := range def.PlacementVolumes {
		validateLevelVolumeUniqueID(&result, seenVolumeIDs, volume.ID)
		validatePlacementVolume(&result, volume, opts)
	}

	validateLevelTerrain(&result, def.Terrain, opts)

	return result
}

func (r *LevelValidationResult) addError(code string, message string, placementID string, placementPath string, volumeID string, volumePath string, markerID string) {
	r.Issues = append(r.Issues, LevelValidationIssue{
		Severity:      LevelValidationSeverityError,
		Code:          code,
		Message:       message,
		PlacementID:   placementID,
		PlacementPath: placementPath,
		VolumeID:      volumeID,
		VolumePath:    volumePath,
		MarkerID:      markerID,
	})
	r.HardErrorCount++
}

func validateLevelUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string, placementPath string, markerID string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_id", fmt.Sprintf("duplicate id %s", id), id, placementPath, "", "", markerID)
		return
	}
	seen[id] = struct{}{}
}

func validateLevelVolumeUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_volume_id", fmt.Sprintf("duplicate placement volume id %s", id), "", "", id, "", "")
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

func isValidPlacementVolumeKind(kind PlacementVolumeKind) bool {
	switch kind {
	case PlacementVolumeKindSphere, PlacementVolumeKindBox:
		return true
	default:
		return false
	}
}

func isValidPlacementVolumeRuleMode(mode PlacementVolumeRuleMode) bool {
	switch mode {
	case PlacementVolumeRuleModeCount, PlacementVolumeRuleModeDensity:
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
		result.addError("missing_asset_file", fmt.Sprintf("missing asset file %s", placement.AssetPath), placement.ID, placement.AssetPath, "", "", "")
	}
}

func validatePlacementVolume(result *LevelValidationResult, volume PlacementVolumeDef, opts LevelValidationOptions) {
	if !isValidPlacementVolumeKind(volume.Kind) {
		result.addError("invalid_volume_kind", fmt.Sprintf("unsupported placement volume kind %q", volume.Kind), "", "", volume.ID, "", "")
	}
	switch volume.Kind {
	case PlacementVolumeKindSphere:
		if volume.Radius <= 0 {
			result.addError("invalid_volume_radius", "sphere placement volume radius must be positive", "", "", volume.ID, "", "")
		}
	case PlacementVolumeKindBox:
		if volume.Extents[0] <= 0 || volume.Extents[1] <= 0 || volume.Extents[2] <= 0 {
			result.addError("invalid_volume_extents", "box placement volume extents must be positive", "", "", volume.ID, "", "")
		}
	}
	if !isValidPlacementVolumeRuleMode(volume.Rule.Mode) {
		result.addError("invalid_volume_rule_mode", fmt.Sprintf("unsupported placement volume rule mode %q", volume.Rule.Mode), "", "", volume.ID, "", "")
	}
	if volume.Rule.Mode == PlacementVolumeRuleModeCount && volume.Rule.Count <= 0 {
		result.addError("invalid_volume_count", "placement volume count must be positive", "", "", volume.ID, "", "")
	}
	if volume.Rule.Mode == PlacementVolumeRuleModeDensity && volume.Rule.DensityPer1000Volume <= 0 {
		result.addError("invalid_volume_density", "placement volume density_per_1000_volume must be positive", "", "", volume.ID, "", "")
	}

	hasAssetPath := strings.TrimSpace(volume.AssetPath) != ""
	hasAssetSetPath := strings.TrimSpace(volume.AssetSetPath) != ""
	switch {
	case hasAssetPath && hasAssetSetPath:
		result.addError("invalid_volume_source", "placement volume must not define both asset_path and asset_set_path", "", "", volume.ID, "", "")
	case !hasAssetPath && !hasAssetSetPath:
		result.addError("missing_volume_source", "placement volume must define asset_path or asset_set_path", "", "", volume.ID, "", "")
	}

	if hasAssetPath && opts.DocumentPath != "" {
		resolvedPath := ResolveDocumentPath(volume.AssetPath, opts.DocumentPath)
		if _, err := os.Stat(resolvedPath); err != nil {
			result.addError("missing_volume_asset_file", fmt.Sprintf("missing asset file %s", volume.AssetPath), "", "", volume.ID, volume.AssetPath, "")
		}
	}

	if hasAssetSetPath && opts.DocumentPath != "" {
		resolvedPath := ResolveDocumentPath(volume.AssetSetPath, opts.DocumentPath)
		if _, err := os.Stat(resolvedPath); err != nil {
			result.addError("missing_asset_set_file", fmt.Sprintf("missing asset set file %s", volume.AssetSetPath), "", "", volume.ID, volume.AssetSetPath, "")
			return
		}
		assetSet, err := LoadAssetSet(resolvedPath)
		if err != nil {
			result.addError("invalid_asset_set", fmt.Sprintf("failed to load asset set %s: %v", volume.AssetSetPath, err), "", "", volume.ID, volume.AssetSetPath, "")
			return
		}
		validation := ValidateAssetSet(assetSet, AssetSetValidationOptions{DocumentPath: resolvedPath})
		for _, issue := range validation.Issues {
			result.addError("invalid_asset_set", issue.Message, "", "", volume.ID, volume.AssetSetPath, "")
		}
	}
}

func validateLevelTerrain(result *LevelValidationResult, terrain *LevelTerrainDef, opts LevelValidationOptions) {
	if terrain == nil {
		return
	}
	if terrain.Kind != TerrainKindHeightfield {
		result.addError("invalid_terrain_kind", fmt.Sprintf("unsupported terrain kind %q", terrain.Kind), "", "", "", "", "")
	}
	if strings.TrimSpace(terrain.SourcePath) == "" {
		result.addError("empty_terrain_source_path", "terrain source_path is required", "", "", "", "", "")
		return
	}

	resolvedPath := ResolveDocumentPath(terrain.SourcePath, opts.DocumentPath)
	if strings.ToLower(filepath.Ext(resolvedPath)) != ".gkterrain" {
		result.addError("invalid_terrain_source_path", fmt.Sprintf("terrain source_path must point to a .gkterrain: %s", terrain.SourcePath), "", "", "", "", "")
		return
	}

	if _, err := os.Stat(resolvedPath); err != nil {
		result.addError("missing_terrain_source_file", fmt.Sprintf("missing terrain source file %s", terrain.SourcePath), "", "", "", "", "")
		return
	}

	terrainDef, err := LoadTerrainSource(resolvedPath)
	if err != nil {
		result.addError("invalid_terrain_source", fmt.Sprintf("failed to load terrain source %s: %v", terrain.SourcePath, err), "", "", "", "", "")
		return
	}
	terrainValidation := ValidateTerrainSource(terrainDef, TerrainValidationOptions{DocumentPath: resolvedPath})
	for _, issue := range terrainValidation.Issues {
		result.addError("invalid_terrain_source", issue.Message, "", "", "", "", "")
	}
}
