package content

import (
	"fmt"
	"math"
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
	BrushID       string                  `json:"brush_id,omitempty"`
	MarkerID      string                  `json:"marker_id,omitempty"`
	BaseWorldPath string                  `json:"base_world_path,omitempty"`
}

type LevelValidationOptions struct {
	DocumentPath     string
	RuntimeVoxelSize float32
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
	if opts.RuntimeVoxelSize <= 0 {
		opts.RuntimeVoxelSize = 0.1
	}
	if def == nil {
		result.addError("nil_level", "level definition is nil", "", "", "", "", "", "", "")
		return result
	}
	EnsureLevelIDs(def)

	if strings.TrimSpace(def.Name) == "" {
		result.addError("empty_name", "level name is required", "", "", "", "", "", "", "")
	}

	seenIDs := map[string]struct{}{}
	materialIDs := make(map[string]struct{}, len(def.Materials))
	for _, material := range def.Materials {
		validateLevelUniqueID(&result, seenIDs, material.ID, "", "", "", "")
		if strings.TrimSpace(material.Name) == "" {
			result.addError("empty_material_name", "level material name is required", "", "", "", "", "", "", "")
		}
		validateLevelMaterial(&result, material)
		materialIDs[material.ID] = struct{}{}
	}

	for _, layer := range def.BrushLayers {
		validateLevelUniqueID(&result, seenIDs, layer.ID, "", "", "", "")
		if strings.TrimSpace(layer.Name) == "" {
			result.addError("empty_brush_layer_name", "level brush layer name is required", "", "", "", "", "", "", "")
		}
		for _, brush := range layer.Brushes {
			validateLevelUniqueID(&result, seenIDs, brush.ID, "", "", "", brush.ID)
			if strings.TrimSpace(brush.Name) == "" {
				result.addError("empty_brush_name", "level brush name is required", "", "", "", "", "", brush.ID, "")
			}
			validateLevelBrush(&result, brush, materialIDs)
		}
	}
	if len(def.Brushes) > 0 {
		result.addError("legacy_brush_storage", "top-level level.brushes is no longer supported; use brush_layers", "", "", "", "", "", "", "")
	}

	for _, placement := range def.Placements {
		validateLevelUniqueID(&result, seenIDs, placement.ID, placement.AssetPath, "", "", "")
		if strings.TrimSpace(placement.AssetPath) == "" {
			result.addError("empty_asset_path", "placement asset_path is required", placement.ID, placement.AssetPath, "", "", "", "", "")
		}
		if !isValidPlacementMode(placement.PlacementMode) {
			result.addError("invalid_placement_mode", fmt.Sprintf("unsupported placement mode %q", placement.PlacementMode), placement.ID, placement.AssetPath, "", "", "", "", "")
		}
		validatePlacementAssetPath(&result, placement, opts)
	}

	for _, marker := range def.Markers {
		validateLevelUniqueID(&result, seenIDs, marker.ID, "", "", marker.ID, "")
		if strings.TrimSpace(marker.Name) == "" {
			result.addError("empty_marker_name", "level marker name is required", "", "", "", "", marker.ID, "", "")
		}
		if strings.TrimSpace(marker.Kind) == "" {
			result.addError("empty_marker_kind", "level marker kind is required", "", "", "", "", marker.ID, "", "")
		}
	}

	for _, volume := range def.PlacementVolumes {
		validateLevelVolumeUniqueID(&result, seenIDs, volume.ID)
		validatePlacementVolume(&result, volume, opts)
	}

	validateLevelTerrain(&result, def, opts)
	validateLevelBaseWorld(&result, def, opts)
	validateShooterLevelRequirements(&result, def, opts)

	return result
}

func (r *LevelValidationResult) addError(code string, message string, placementID string, placementPath string, volumeID string, volumePath string, markerID string, brushID string, baseWorldPath string) {
	r.Issues = append(r.Issues, LevelValidationIssue{
		Severity:      LevelValidationSeverityError,
		Code:          code,
		Message:       message,
		PlacementID:   placementID,
		PlacementPath: placementPath,
		VolumeID:      volumeID,
		VolumePath:    volumePath,
		MarkerID:      markerID,
		BrushID:       brushID,
		BaseWorldPath: baseWorldPath,
	})
	r.HardErrorCount++
}

func validateLevelUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string, placementPath string, volumeID string, markerID string, brushID string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_id", fmt.Sprintf("duplicate id %s", id), id, placementPath, volumeID, "", markerID, brushID, "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelVolumeUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_volume_id", fmt.Sprintf("duplicate placement volume id %s", id), "", "", id, "", "", "", "")
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
		result.addError("missing_asset_file", fmt.Sprintf("missing asset file %s", placement.AssetPath), placement.ID, placement.AssetPath, "", "", "", "", "")
	}
}

func validatePlacementVolume(result *LevelValidationResult, volume PlacementVolumeDef, opts LevelValidationOptions) {
	if !isValidPlacementVolumeKind(volume.Kind) {
		result.addError("invalid_volume_kind", fmt.Sprintf("unsupported placement volume kind %q", volume.Kind), "", "", volume.ID, "", "", "", "")
	}
	switch volume.Kind {
	case PlacementVolumeKindSphere:
		if volume.Radius <= 0 {
			result.addError("invalid_volume_radius", "sphere placement volume radius must be positive", "", "", volume.ID, "", "", "", "")
		}
	case PlacementVolumeKindBox:
		if volume.Extents[0] <= 0 || volume.Extents[1] <= 0 || volume.Extents[2] <= 0 {
			result.addError("invalid_volume_extents", "box placement volume extents must be positive", "", "", volume.ID, "", "", "", "")
		}
	}
	if !isValidPlacementVolumeRuleMode(volume.Rule.Mode) {
		result.addError("invalid_volume_rule_mode", fmt.Sprintf("unsupported placement volume rule mode %q", volume.Rule.Mode), "", "", volume.ID, "", "", "", "")
	}
	if volume.Rule.Mode == PlacementVolumeRuleModeCount && volume.Rule.Count <= 0 {
		result.addError("invalid_volume_count", "placement volume count must be positive", "", "", volume.ID, "", "", "", "")
	}
	if volume.Rule.Mode == PlacementVolumeRuleModeDensity && volume.Rule.DensityPer1000Volume <= 0 {
		result.addError("invalid_volume_density", "placement volume density_per_1000_volume must be positive", "", "", volume.ID, "", "", "", "")
	}
	if volume.ShadowMaxDistance < 0 {
		result.addError("invalid_volume_shadow_max_distance", "placement volume shadow_max_distance must be >= 0", "", "", volume.ID, "", "", "", "")
	}
	if volume.MaxShadowCasters < 0 {
		result.addError("invalid_volume_max_shadow_casters", "placement volume max_shadow_casters must be >= 0", "", "", volume.ID, "", "", "", "")
	}

	hasAssetPath := strings.TrimSpace(volume.AssetPath) != ""
	hasAssetSetPath := strings.TrimSpace(volume.AssetSetPath) != ""
	switch {
	case hasAssetPath && hasAssetSetPath:
		result.addError("invalid_volume_source", "placement volume must not define both asset_path and asset_set_path", "", "", volume.ID, "", "", "", "")
	case !hasAssetPath && !hasAssetSetPath:
		result.addError("missing_volume_source", "placement volume must define asset_path or asset_set_path", "", "", volume.ID, "", "", "", "")
	}

	if hasAssetPath && opts.DocumentPath != "" {
		resolvedPath := ResolveDocumentPath(volume.AssetPath, opts.DocumentPath)
		if _, err := os.Stat(resolvedPath); err != nil {
			result.addError("missing_volume_asset_file", fmt.Sprintf("missing asset file %s", volume.AssetPath), "", "", volume.ID, volume.AssetPath, "", "", "")
		}
	}

	if hasAssetSetPath && opts.DocumentPath != "" {
		resolvedPath := ResolveDocumentPath(volume.AssetSetPath, opts.DocumentPath)
		if _, err := os.Stat(resolvedPath); err != nil {
			result.addError("missing_asset_set_file", fmt.Sprintf("missing asset set file %s", volume.AssetSetPath), "", "", volume.ID, volume.AssetSetPath, "", "", "")
			return
		}
		assetSet, err := LoadAssetSet(resolvedPath)
		if err != nil {
			result.addError("invalid_asset_set", fmt.Sprintf("failed to load asset set %s: %v", volume.AssetSetPath, err), "", "", volume.ID, volume.AssetSetPath, "", "", "")
			return
		}
		validation := ValidateAssetSet(assetSet, AssetSetValidationOptions{DocumentPath: resolvedPath})
		for _, issue := range validation.Issues {
			result.addError("invalid_asset_set", issue.Message, "", "", volume.ID, volume.AssetSetPath, "", "", "")
		}
	}
}

func validateLevelMaterial(result *LevelValidationResult, material LevelMaterialDef) {
	if material.Roughness < 0 || material.Roughness > 1 {
		result.addError("invalid_material_payload", "level material roughness must be between 0 and 1", "", "", "", "", "", "", "")
	}
	if material.Metallic < 0 || material.Metallic > 1 {
		result.addError("invalid_material_payload", "level material metallic must be between 0 and 1", "", "", "", "", "", "", "")
	}
	if material.Emissive < 0 {
		result.addError("invalid_material_payload", "level material emissive must be >= 0", "", "", "", "", "", "", "")
	}
	if material.IOR <= 0 {
		result.addError("invalid_material_payload", "level material ior must be > 0", "", "", "", "", "", "", "")
	}
	if material.Transparency < 0 || material.Transparency > 1 {
		result.addError("invalid_material_payload", "level material transparency must be between 0 and 1", "", "", "", "", "", "", "")
	}
}

func validateLevelBrush(result *LevelValidationResult, brush LevelBrushDef, materialIDs map[string]struct{}) {
	if strings.TrimSpace(brush.Primitive) == "" {
		result.addError("invalid_brush_payload", "level brush primitive is required", "", "", "", "", "", brush.ID, "")
		return
	}
	if strings.TrimSpace(brush.MaterialID) != "" {
		if _, ok := materialIDs[brush.MaterialID]; !ok {
			result.addError("missing_material_reference", fmt.Sprintf("missing material %s", brush.MaterialID), "", "", "", "", "", brush.ID, "")
		}
	}
	switch EffectiveLevelBrushOperation(brush) {
	case AssetShapeOperationAdd, AssetShapeOperationSubtract:
	default:
		result.addError("invalid_brush_payload", fmt.Sprintf("unsupported brush operation %q", brush.Operation), "", "", "", "", "", brush.ID, "")
	}

	spec, ok := ProceduralPrimitiveSpecFor(brush.Primitive)
	if !ok {
		result.addError("invalid_brush_payload", fmt.Sprintf("unsupported level brush primitive %q", brush.Primitive), "", "", "", "", "", brush.ID, "")
		return
	}
	for _, key := range spec.Params {
		value, ok := brush.Params[key]
		if !ok {
			result.addError("invalid_brush_payload", fmt.Sprintf("level brush %q requires param %s", brush.Primitive, key), "", "", "", "", "", brush.ID, "")
			continue
		}
		if value <= 0 {
			result.addError("invalid_brush_payload", fmt.Sprintf("level brush %q param %s must be > 0", brush.Primitive, key), "", "", "", "", "", brush.ID, "")
		}
	}
}

func validateLevelTerrain(result *LevelValidationResult, level *LevelDef, opts LevelValidationOptions) {
	if level == nil || level.Terrain == nil {
		return
	}
	terrain := level.Terrain
	if terrain.Kind != TerrainKindHeightfield {
		result.addError("invalid_terrain_kind", fmt.Sprintf("unsupported terrain kind %q", terrain.Kind), "", "", "", "", "", "", "")
	}
	if strings.TrimSpace(terrain.SourcePath) == "" {
		result.addError("empty_terrain_source_path", "terrain source_path is required", "", "", "", "", "", "", "")
		return
	}

	resolvedPath := ResolveDocumentPath(terrain.SourcePath, opts.DocumentPath)
	if strings.ToLower(filepath.Ext(resolvedPath)) != ".gkterrain" {
		result.addError("invalid_terrain_source_path", fmt.Sprintf("terrain source_path must point to a .gkterrain: %s", terrain.SourcePath), "", "", "", "", "", "", "")
		return
	}

	if _, err := os.Stat(resolvedPath); err != nil {
		result.addError("missing_terrain_source_file", fmt.Sprintf("missing terrain source file %s", terrain.SourcePath), "", "", "", "", "", "", "")
		return
	}

	terrainDef, err := LoadTerrainSource(resolvedPath)
	if err != nil {
		result.addError("invalid_terrain_source", fmt.Sprintf("failed to load terrain source %s: %v", terrain.SourcePath, err), "", "", "", "", "", "", "")
		return
	}
	terrainValidation := ValidateTerrainSource(terrainDef, TerrainValidationOptions{DocumentPath: resolvedPath})
	for _, issue := range terrainValidation.Issues {
		result.addError("invalid_terrain_source", issue.Message, "", "", "", "", "", "", "")
	}
	if level.ChunkSize > 0 && terrainDef.ChunkSize != level.ChunkSize {
		result.addError("terrain_chunk_size_mismatch", fmt.Sprintf("terrain chunk size %d does not match level chunk size %d", terrainDef.ChunkSize, level.ChunkSize), "", "", "", "", "", "", "")
	}
	if level.VoxelResolution > 0 && absLevelFloat32(terrainDef.VoxelResolution-level.VoxelResolution) > 1e-4 {
		result.addError("terrain_voxel_resolution_mismatch", fmt.Sprintf("terrain voxel size %.4f does not match level voxel size %.4f", terrainDef.VoxelResolution, level.VoxelResolution), "", "", "", "", "", "", "")
	}
}

func validateLevelBaseWorld(result *LevelValidationResult, def *LevelDef, opts LevelValidationOptions) {
	if def == nil || def.BaseWorld == nil {
		return
	}
	baseWorld := def.BaseWorld
	if baseWorld.Kind != ImportedWorldKindVoxelWorld {
		result.addError("invalid_base_world_kind", fmt.Sprintf("unsupported base world kind %q", baseWorld.Kind), "", "", "", "", "", "", baseWorld.ManifestPath)
	}
	if strings.TrimSpace(baseWorld.ManifestPath) == "" {
		result.addError("empty_base_world_manifest_path", "base world manifest_path is required", "", "", "", "", "", "", baseWorld.ManifestPath)
		return
	}
	resolvedPath := ResolveDocumentPath(baseWorld.ManifestPath, opts.DocumentPath)
	if strings.ToLower(filepath.Ext(resolvedPath)) != ".gkworld" {
		result.addError("invalid_base_world_manifest_path", fmt.Sprintf("base world manifest_path must point to a .gkworld: %s", baseWorld.ManifestPath), "", "", "", "", "", "", baseWorld.ManifestPath)
		return
	}
	if _, err := os.Stat(resolvedPath); err != nil {
		result.addError("missing_base_world_manifest", fmt.Sprintf("missing base world manifest %s", baseWorld.ManifestPath), "", "", "", "", "", "", baseWorld.ManifestPath)
		return
	}
	importedWorld, err := LoadImportedWorld(resolvedPath)
	if err != nil {
		result.addError("invalid_base_world_manifest", fmt.Sprintf("failed to load base world manifest %s: %v", baseWorld.ManifestPath, err), "", "", "", "", "", "", baseWorld.ManifestPath)
		return
	}
	validation := ValidateImportedWorld(importedWorld, ImportedWorldValidationOptions{DocumentPath: resolvedPath})
	for _, issue := range validation.Issues {
		result.addError(issue.Code, issue.Message, "", "", "", "", "", "", baseWorld.ManifestPath)
	}
	if def.ChunkSize > 0 && importedWorld.ChunkSize != def.ChunkSize {
		result.addError("base_world_chunk_size_mismatch", fmt.Sprintf("base world chunk size %d does not match level chunk size %d", importedWorld.ChunkSize, def.ChunkSize), "", "", "", "", "", "", baseWorld.ManifestPath)
	}
	if def.VoxelResolution > 0 && absLevelFloat32(importedWorld.VoxelResolution-def.VoxelResolution) > 1e-4 {
		result.addError("base_world_voxel_resolution_mismatch", fmt.Sprintf("base world voxel size %.4f does not match level voxel size %.4f", importedWorld.VoxelResolution, def.VoxelResolution), "", "", "", "", "", "", baseWorld.ManifestPath)
	}
}

func validateShooterLevelRequirements(result *LevelValidationResult, def *LevelDef, opts LevelValidationOptions) {
	if def == nil || !levelNeedsShooterValidation(def) {
		return
	}
	if def.BaseWorld == nil || strings.TrimSpace(def.BaseWorld.ManifestPath) == "" {
		result.addError("missing_shooter_base_world", "shooter level requires an imported base world", "", "", "", "", "", "", "")
		return
	}
	if _, ok := FindLevelMarkerByKind(def.Markers, LevelMarkerKindPlayerSpawn); !ok {
		result.addError("missing_player_spawn", "shooter level requires a player_spawn marker", "", "", "", "", "", "", "")
	}
	validateShooterMarkerPlacement(result, def, opts)
}

func levelNeedsShooterValidation(def *LevelDef) bool {
	if def == nil {
		return false
	}
	for _, tag := range def.Tags {
		if strings.EqualFold(strings.TrimSpace(tag), LevelTagShooter) {
			return true
		}
	}
	for _, marker := range def.Markers {
		switch marker.Kind {
		case LevelMarkerKindPlayerSpawn, LevelMarkerKindAISpawn, LevelMarkerKindPatrolPoint, LevelMarkerKindObjective, LevelMarkerKindExtract:
			return true
		}
	}
	return false
}

func validateShooterMarkerPlacement(result *LevelValidationResult, def *LevelDef, opts LevelValidationOptions) {
	if def == nil || def.BaseWorld == nil || opts.DocumentPath == "" {
		return
	}
	manifestPath := ResolveDocumentPath(def.BaseWorld.ManifestPath, opts.DocumentPath)
	manifest, err := LoadImportedWorld(manifestPath)
	if err != nil {
		return
	}
	entries := make(map[TerrainChunkCoordDef]ImportedWorldChunkEntryDef, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		entries[entry.Coord] = entry
	}
	chunkWorldSize := float32(manifest.ChunkSize) * manifest.VoxelResolution
	voxelWorldSize := manifest.VoxelResolution
	if chunkWorldSize <= 0 || voxelWorldSize <= 0 {
		return
	}
	chunkCache := make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef)
	for _, marker := range def.Markers {
		position := marker.Transform.Position
		chunkCoord := TerrainChunkCoordDef{
			X: int(floorLevelFloat32(position[0] / chunkWorldSize)),
			Y: int(floorLevelFloat32(position[1] / chunkWorldSize)),
			Z: int(floorLevelFloat32(position[2] / chunkWorldSize)),
		}
		entry, ok := entries[chunkCoord]
		if !ok {
			result.addError("marker_out_of_bounds", fmt.Sprintf("marker %s sits outside imported base-world bounds", marker.ID), "", "", "", "", marker.ID, "", def.BaseWorld.ManifestPath)
			continue
		}
		chunk := chunkCache[chunkCoord]
		if chunk == nil && entry.NonEmptyVoxelCount > 0 {
			chunkPath := ResolveImportedWorldChunkPath(entry, manifestPath)
			chunk, err = LoadImportedWorldChunk(chunkPath)
			if err != nil {
				continue
			}
			chunkCache[chunkCoord] = chunk
		}
		if chunk == nil {
			continue
		}
		localX := int(floorLevelFloat32((position[0] - float32(chunkCoord.X)*chunkWorldSize) / voxelWorldSize))
		localY := int(floorLevelFloat32((position[1] - float32(chunkCoord.Y)*chunkWorldSize) / voxelWorldSize))
		localZ := int(floorLevelFloat32((position[2] - float32(chunkCoord.Z)*chunkWorldSize) / voxelWorldSize))
		if localX < 0 || localY < 0 || localZ < 0 || localX >= chunk.ChunkSize || localY >= chunk.ChunkSize || localZ >= chunk.ChunkSize {
			result.addError("marker_out_of_bounds", fmt.Sprintf("marker %s resolves outside chunk bounds", marker.ID), "", "", "", "", marker.ID, "", def.BaseWorld.ManifestPath)
			continue
		}
		if importedWorldChunkHasVoxel(chunk, localX, localY, localZ) {
			result.addError("marker_inside_solid", fmt.Sprintf("marker %s is placed inside solid imported geometry", marker.ID), "", "", "", "", marker.ID, "", def.BaseWorld.ManifestPath)
		}
	}
}

func importedWorldChunkHasVoxel(chunk *ImportedWorldChunkDef, x, y, z int) bool {
	if chunk == nil {
		return false
	}
	for _, voxel := range chunk.Voxels {
		if voxel.Value != 0 && voxel.X == x && voxel.Y == y && voxel.Z == z {
			return true
		}
	}
	return false
}

func FindLevelMarkerByKind(markers []LevelMarkerDef, kind string) (LevelMarkerDef, bool) {
	for _, marker := range markers {
		if marker.Kind == kind {
			return marker, true
		}
	}
	return LevelMarkerDef{}, false
}

func absLevelFloat32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func floorLevelFloat32(v float32) float32 {
	return float32(math.Floor(float64(v)))
}
