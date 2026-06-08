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
	for _, light := range def.Lights {
		validateLevelUniqueID(&result, seenIDs, light.ID, light.Name, "", "", "")
		if !isValidLevelLightType(light.Type) {
			result.addError("invalid_light_type", fmt.Sprintf("unsupported light type %q", light.Type), "", "", "", "", "", "", "")
		}
		if light.Intensity < 0 {
			result.addError("invalid_light_intensity", "light intensity must be non-negative", "", "", "", "", "", "", "")
		}
		if isLocalLevelLightType(light.Type) {
			if light.Range <= 0 {
				result.addError("invalid_light_range", "point and spot light range must be > 0", "", "", "", "", "", "", "")
			}
		} else if light.Range < 0 {
			result.addError("invalid_light_range", "light range must be non-negative", "", "", "", "", "", "", "")
		}
		if light.Type == LevelLightTypeSpot && (light.ConeAngle <= 0 || light.ConeAngle >= 180) {
			result.addError("invalid_light_cone_angle", "spot light cone angle must be > 0 and < 180", "", "", "", "", "", "", "")
		}
		if light.SourceRadius < 0 {
			result.addError("invalid_light_source_radius", "light source radius must be non-negative", "", "", "", "", "", "", "")
		}
	}

	for _, volume := range def.PlacementVolumes {
		validateLevelVolumeUniqueID(&result, seenIDs, volume.ID)
		validatePlacementVolume(&result, volume, opts)
	}
	for _, water := range def.WaterBodies {
		validateLevelWaterBodyUniqueID(&result, seenIDs, water.ID)
		validateLevelWaterBody(&result, water)
	}
	for _, ladder := range def.LadderVolumes {
		validateLevelLadderVolumeUniqueID(&result, seenIDs, ladder.ID)
		validateLevelLadderVolume(&result, ladder)
	}
	for _, brush := range def.MovingBrushes {
		validateLevelMovingBrushUniqueID(&result, seenIDs, brush.ID)
		validateLevelMovingBrush(&result, brush, opts)
	}
	for _, node := range def.PathNodes {
		validateLevelPathNodeUniqueID(&result, seenIDs, node.ID)
		validateLevelPathNode(&result, node)
	}
	for _, trigger := range def.UseTriggers {
		validateLevelUseTriggerUniqueID(&result, seenIDs, trigger.ID)
		validateLevelUseTrigger(&result, trigger)
	}
	for _, trigger := range def.TriggerVolumes {
		validateLevelTriggerVolumeUniqueID(&result, seenIDs, trigger.ID)
		validateLevelTriggerVolume(&result, trigger)
	}
	for _, volume := range def.DamageVolumes {
		validateLevelDamageVolumeUniqueID(&result, seenIDs, volume.ID)
		validateLevelDamageVolume(&result, volume)
	}
	for _, change := range def.ChangeLevels {
		validateLevelChangeLevelUniqueID(&result, seenIDs, change.ID)
		validateLevelChangeLevel(&result, change)
	}
	for _, charger := range def.Chargers {
		validateLevelChargerUniqueID(&result, seenIDs, charger.ID)
		validateLevelCharger(&result, charger)
	}
	for _, relay := range def.TargetRelays {
		validateLevelTargetRelayUniqueID(&result, seenIDs, relay.ID)
		validateLevelTargetRelay(&result, relay)
	}
	for _, multi := range def.MultiTargets {
		validateLevelMultiTargetUniqueID(&result, seenIDs, multi.ID)
		validateLevelMultiTarget(&result, multi)
	}
	for _, breakable := range def.Breakables {
		validateLevelBreakableUniqueID(&result, seenIDs, breakable.ID)
		validateLevelBreakable(&result, breakable, opts)
	}
	for _, pickup := range def.Pickups {
		validateLevelPickupUniqueID(&result, seenIDs, pickup.ID)
		validateLevelPickup(&result, pickup, opts)
	}

	validateLevelTerrain(&result, def, opts)
	validateLevelBaseWorld(&result, def, opts)
	validateShooterLevelRequirements(&result, def, opts)

	return result
}

func isValidLevelLightType(lightType LevelLightType) bool {
	switch lightType {
	case LevelLightTypePoint, LevelLightTypeDirectional, LevelLightTypeSpot, LevelLightTypeAmbient:
		return true
	default:
		return false
	}
}

func isLocalLevelLightType(lightType LevelLightType) bool {
	return lightType == LevelLightTypePoint || lightType == LevelLightTypeSpot
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

func validateLevelWaterBodyUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_water_body_id", fmt.Sprintf("duplicate water body id %s", id), "", "", "", "", "", "", "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelLadderVolumeUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_ladder_volume_id", fmt.Sprintf("duplicate ladder volume id %s", id), "", "", "", "", "", "", "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelMovingBrushUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_moving_brush_id", fmt.Sprintf("duplicate moving brush id %s", id), "", "", "", "", "", "", "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelPathNodeUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_path_node_id", fmt.Sprintf("duplicate path node id %s", id), "", "", "", "", "", "", "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelUseTriggerUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_use_trigger_id", fmt.Sprintf("duplicate use trigger id %s", id), "", "", "", "", "", "", "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelTriggerVolumeUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_trigger_volume_id", fmt.Sprintf("duplicate trigger volume id %s", id), "", "", "", "", "", "", "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelDamageVolumeUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_damage_volume_id", fmt.Sprintf("duplicate damage volume id %s", id), "", "", "", "", "", "", "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelChangeLevelUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_change_level_id", fmt.Sprintf("duplicate changelevel volume id %s", id), "", "", "", "", "", "", "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelChargerUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_charger_id", fmt.Sprintf("duplicate charger id %s", id), "", "", "", "", "", "", "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelMultiTargetUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_multi_target_id", fmt.Sprintf("duplicate multi-target id %s", id), "", "", "", "", "", "", "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelTargetRelayUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_target_relay_id", fmt.Sprintf("duplicate target relay id %s", id), "", "", "", "", "", "", "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelBreakableUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_breakable_id", fmt.Sprintf("duplicate breakable id %s", id), "", "", "", "", "", "", "")
		return
	}
	seen[id] = struct{}{}
}

func validateLevelPickupUniqueID(result *LevelValidationResult, seen map[string]struct{}, id string) {
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_pickup_id", fmt.Sprintf("duplicate pickup id %s", id), "", "", "", "", "", "", "")
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

func isValidLevelWaterBodyMode(mode LevelWaterBodyMode) bool {
	switch mode {
	case LevelWaterBodyModeExplicitRect, LevelWaterBodyModeFitBounds:
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

func validateLevelWaterBody(result *LevelValidationResult, water LevelWaterBodyDef) {
	if strings.TrimSpace(water.ID) == "" {
		result.addError("empty_water_body_id", "water body id is required", "", "", "", "", "", "", "")
	}
	mode := water.Mode
	if mode == "" {
		mode = LevelWaterBodyModeExplicitRect
	}
	if !isValidLevelWaterBodyMode(mode) {
		result.addError("invalid_water_body_mode", fmt.Sprintf("unsupported water body mode %q", water.Mode), "", "", "", "", "", "", "")
	}
	if water.Depth <= 0 {
		result.addError("invalid_water_body_depth", "water body depth must be positive", "", "", "", "", "", "", "")
	}
	if water.DirectLightOcclusion != nil && (*water.DirectLightOcclusion < 0 || *water.DirectLightOcclusion > 1) {
		result.addError("invalid_water_body_direct_light_occlusion", "water body direct light occlusion must be between 0 and 1", "", "", "", "", "", "", "")
	}
	switch mode {
	case LevelWaterBodyModeFitBounds:
		if water.BoundsHalfExtents[0] <= 0 || water.BoundsHalfExtents[1] <= 0 || water.BoundsHalfExtents[2] <= 0 {
			result.addError("invalid_water_body_bounds", "fit-bounds water body requires positive bounds half extents", "", "", "", "", "", "", "")
		}
	default:
		if water.RectHalfExtents[0] <= 0 || water.RectHalfExtents[1] <= 0 {
			result.addError("invalid_water_body_rect", "explicit water body requires positive rect half extents", "", "", "", "", "", "", "")
		}
	}
}

func validateLevelLadderVolume(result *LevelValidationResult, ladder LevelLadderVolumeDef) {
	if strings.TrimSpace(ladder.ID) == "" {
		result.addError("empty_ladder_volume_id", "ladder volume id is required", "", "", "", "", "", "", "")
	}
	if ladder.BoundsHalfExtents[0] <= 0 || ladder.BoundsHalfExtents[1] <= 0 || ladder.BoundsHalfExtents[2] <= 0 {
		result.addError("invalid_ladder_volume_bounds", "ladder volume requires positive bounds half extents", "", "", "", "", "", "", "")
	}
	if ladder.ClimbSpeed < 0 {
		result.addError("invalid_ladder_climb_speed", "ladder climb speed must be non-negative", "", "", "", "", "", "", "")
	}
}

func validateLevelMovingBrush(result *LevelValidationResult, brush LevelMovingBrushDef, opts LevelValidationOptions) {
	if strings.TrimSpace(brush.ID) == "" {
		result.addError("empty_moving_brush_id", "moving brush id is required", "", "", "", "", "", "", "")
	}
	if brush.BoundsHalfExtents[0] <= 0 || brush.BoundsHalfExtents[1] <= 0 || brush.BoundsHalfExtents[2] <= 0 {
		result.addError("invalid_moving_brush_bounds", "moving brush requires positive bounds half extents", "", "", "", "", "", "", "")
	}
	if brush.Speed < 0 {
		result.addError("invalid_moving_brush_speed", "moving brush speed must be non-negative", "", "", "", "", "", "", "")
	}
	if brush.MoveDistance < 0 {
		result.addError("invalid_moving_brush_distance", "moving brush move distance must be non-negative", "", "", "", "", "", "", "")
	}
	if strings.TrimSpace(brush.AssetPath) != "" && opts.DocumentPath != "" {
		resolvedPath := ResolveDocumentPath(brush.AssetPath, opts.DocumentPath)
		if _, err := os.Stat(resolvedPath); err != nil {
			result.addError("missing_moving_brush_asset", fmt.Sprintf("missing moving brush asset %s", brush.AssetPath), "", "", "", "", "", "", "")
		}
	}
}

func validateLevelPathNode(result *LevelValidationResult, node LevelPathNodeDef) {
	if strings.TrimSpace(node.ID) == "" {
		result.addError("empty_path_node_id", "path node id is required", "", "", "", "", "", "", "")
	}
	if strings.TrimSpace(node.TargetName) == "" {
		result.addError("empty_path_node_target_name", "path node target_name is required", "", "", "", "", "", "", "")
	}
	if node.Wait < 0 {
		result.addError("invalid_path_node_wait", "path node wait must be non-negative", "", "", "", "", "", "", "")
	}
	if node.Speed < 0 {
		result.addError("invalid_path_node_speed", "path node speed must be non-negative", "", "", "", "", "", "", "")
	}
}

func validateLevelUseTrigger(result *LevelValidationResult, trigger LevelUseTriggerDef) {
	if strings.TrimSpace(trigger.ID) == "" {
		result.addError("empty_use_trigger_id", "use trigger id is required", "", "", "", "", "", "", "")
	}
	if trigger.BoundsHalfExtents[0] <= 0 || trigger.BoundsHalfExtents[1] <= 0 || trigger.BoundsHalfExtents[2] <= 0 {
		result.addError("invalid_use_trigger_bounds", "use trigger requires positive bounds half extents", "", "", "", "", "", "", "")
	}
}

func validateLevelTriggerVolume(result *LevelValidationResult, trigger LevelTriggerVolumeDef) {
	if strings.TrimSpace(trigger.ID) == "" {
		result.addError("empty_trigger_volume_id", "trigger volume id is required", "", "", "", "", "", "", "")
	}
	if trigger.BoundsHalfExtents[0] <= 0 || trigger.BoundsHalfExtents[1] <= 0 || trigger.BoundsHalfExtents[2] <= 0 {
		result.addError("invalid_trigger_volume_bounds", "trigger volume requires positive bounds half extents", "", "", "", "", "", "", "")
	}
	if trigger.Delay < 0 {
		result.addError("invalid_trigger_volume_delay", "trigger volume delay must be non-negative", "", "", "", "", "", "", "")
	}
	if trigger.Wait < 0 {
		result.addError("invalid_trigger_volume_wait", "trigger volume wait must be non-negative", "", "", "", "", "", "", "")
	}
}

func validateLevelDamageVolume(result *LevelValidationResult, volume LevelDamageVolumeDef) {
	if strings.TrimSpace(volume.ID) == "" {
		result.addError("empty_damage_volume_id", "damage volume id is required", "", "", "", "", "", "", "")
	}
	if volume.BoundsHalfExtents[0] <= 0 || volume.BoundsHalfExtents[1] <= 0 || volume.BoundsHalfExtents[2] <= 0 {
		result.addError("invalid_damage_volume_bounds", "damage volume requires positive bounds half extents", "", "", "", "", "", "", "")
	}
	if volume.Damage < 0 {
		result.addError("invalid_damage_volume_damage", "damage volume damage must be non-negative", "", "", "", "", "", "", "")
	}
	if volume.DamageInterval < 0 {
		result.addError("invalid_damage_volume_interval", "damage volume interval must be non-negative", "", "", "", "", "", "", "")
	}
	if volume.Delay < 0 {
		result.addError("invalid_damage_volume_delay", "damage volume delay must be non-negative", "", "", "", "", "", "", "")
	}
}

func validateLevelChangeLevel(result *LevelValidationResult, change LevelChangeLevelDef) {
	if strings.TrimSpace(change.ID) == "" {
		result.addError("empty_change_level_id", "changelevel volume id is required", "", "", "", "", "", "", "")
	}
	if change.BoundsHalfExtents[0] <= 0 || change.BoundsHalfExtents[1] <= 0 || change.BoundsHalfExtents[2] <= 0 {
		result.addError("invalid_change_level_bounds", "changelevel volume requires positive bounds half extents", "", "", "", "", "", "", "")
	}
	if strings.TrimSpace(change.TargetMap) == "" {
		result.addError("empty_change_level_target_map", "changelevel volume target_map is required", "", "", "", "", "", "", "")
	}
}

func validateLevelCharger(result *LevelValidationResult, charger LevelChargerDef) {
	if strings.TrimSpace(charger.ID) == "" {
		result.addError("empty_charger_id", "charger id is required", "", "", "", "", "", "", "")
	}
	if charger.BoundsHalfExtents[0] <= 0 || charger.BoundsHalfExtents[1] <= 0 || charger.BoundsHalfExtents[2] <= 0 {
		result.addError("invalid_charger_bounds", "charger requires positive bounds half extents", "", "", "", "", "", "", "")
	}
	switch strings.TrimSpace(charger.ChargeKind) {
	case "", "health", "armor":
	default:
		result.addError("invalid_charger_kind", "charger charge_kind must be health or armor", "", "", "", "", "", "", "")
	}
	if charger.Capacity < 0 {
		result.addError("invalid_charger_capacity", "charger capacity must be non-negative", "", "", "", "", "", "", "")
	}
	if charger.Rate < 0 {
		result.addError("invalid_charger_rate", "charger rate must be non-negative", "", "", "", "", "", "", "")
	}
}

func validateLevelMultiTarget(result *LevelValidationResult, multi LevelMultiTargetDef) {
	if strings.TrimSpace(multi.ID) == "" {
		result.addError("empty_multi_target_id", "multi-target id is required", "", "", "", "", "", "", "")
	}
	if strings.TrimSpace(multi.TargetName) == "" {
		result.addError("empty_multi_target_name", "multi-target target_name is required", "", "", "", "", "", "", "")
	}
	if multi.Delay < 0 {
		result.addError("invalid_multi_target_delay", "multi-target delay must be non-negative", "", "", "", "", "", "", "")
	}
	for _, event := range multi.Events {
		if strings.TrimSpace(event.Target) == "" {
			result.addError("empty_multi_target_event", "multi-target event target is required", "", "", "", "", "", "", "")
		}
		if event.Delay < 0 {
			result.addError("invalid_multi_target_event_delay", "multi-target event delay must be non-negative", "", "", "", "", "", "", "")
		}
	}
}

func validateLevelTargetRelay(result *LevelValidationResult, relay LevelTargetRelayDef) {
	if strings.TrimSpace(relay.ID) == "" {
		result.addError("empty_target_relay_id", "target relay id is required", "", "", "", "", "", "", "")
	}
	if strings.TrimSpace(relay.TargetName) == "" {
		result.addError("empty_target_relay_name", "target relay target_name is required", "", "", "", "", "", "", "")
	}
	if relay.Delay < 0 {
		result.addError("invalid_target_relay_delay", "target relay delay must be non-negative", "", "", "", "", "", "", "")
	}
	if relay.TriggerState < 0 || relay.TriggerState > 2 {
		result.addError("invalid_target_relay_state", "target relay trigger_state must be 0, 1, or 2", "", "", "", "", "", "", "")
	}
}

func validateLevelBreakable(result *LevelValidationResult, breakable LevelBreakableDef, opts LevelValidationOptions) {
	if strings.TrimSpace(breakable.ID) == "" {
		result.addError("empty_breakable_id", "breakable id is required", "", "", "", "", "", "", "")
	}
	if breakable.BoundsHalfExtents[0] <= 0 || breakable.BoundsHalfExtents[1] <= 0 || breakable.BoundsHalfExtents[2] <= 0 {
		result.addError("invalid_breakable_bounds", "breakable requires positive bounds half extents", "", "", "", "", "", "", "")
	}
	if breakable.Health < 0 {
		result.addError("invalid_breakable_health", "breakable health must be non-negative", "", "", "", "", "", "", "")
	}
	if breakable.Delay < 0 {
		result.addError("invalid_breakable_delay", "breakable delay must be non-negative", "", "", "", "", "", "", "")
	}
	if strings.TrimSpace(breakable.AssetPath) != "" && opts.DocumentPath != "" {
		resolvedPath := ResolveDocumentPath(breakable.AssetPath, opts.DocumentPath)
		if _, err := os.Stat(resolvedPath); err != nil {
			result.addError("missing_breakable_asset", fmt.Sprintf("missing breakable asset %s", breakable.AssetPath), "", "", "", "", "", "", "")
		}
	}
}

func validateLevelPickup(result *LevelValidationResult, pickup LevelPickupDef, opts LevelValidationOptions) {
	if strings.TrimSpace(pickup.ID) == "" {
		result.addError("empty_pickup_id", "pickup id is required", "", "", "", "", "", "", "")
	}
	if strings.TrimSpace(pickup.Category) == "" {
		result.addError("empty_pickup_category", "pickup category is required", "", "", "", "", "", "", "")
	}
	if strings.TrimSpace(pickup.Item) == "" {
		result.addError("empty_pickup_item", "pickup item is required", "", "", "", "", "", "", "")
	}
	if pickup.Amount < 0 {
		result.addError("invalid_pickup_amount", "pickup amount must be non-negative", "", "", "", "", "", "", "")
	}
	if strings.TrimSpace(pickup.AssetPath) != "" && opts.DocumentPath != "" {
		resolvedPath := ResolveDocumentPath(pickup.AssetPath, opts.DocumentPath)
		if _, err := os.Stat(resolvedPath); err != nil {
			result.addError("missing_pickup_asset", fmt.Sprintf("missing pickup asset %s", pickup.AssetPath), "", "", "", "", "", "", "")
		}
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

	switch EffectiveLevelBrushKind(brush) {
	case LevelBrushKindProcedural:
		if strings.TrimSpace(brush.Primitive) == "" {
			result.addError("invalid_brush_payload", "level brush primitive is required", "", "", "", "", "", brush.ID, "")
			return
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
	case LevelBrushKindVoxelShape:
		validateLevelBrushVoxelShape(result, brush, materialIDs)
	default:
		result.addError("invalid_brush_payload", fmt.Sprintf("unsupported level brush kind %q", brush.Kind), "", "", "", "", "", brush.ID, "")
		return
	}
}

func validateLevelBrushVoxelShape(result *LevelValidationResult, brush LevelBrushDef, materialIDs map[string]struct{}) {
	if result == nil {
		return
	}
	if brush.VoxelShape == nil {
		result.addError("invalid_brush_payload", "voxel_shape brush requires voxel_shape payload", "", "", "", "", "", brush.ID, "")
		return
	}

	paletteByValue := make(map[uint8]string, len(brush.VoxelShape.Palette))
	for _, entry := range brush.VoxelShape.Palette {
		if entry.Value == 0 {
			result.addError("invalid_brush_payload", "voxel_shape palette value 0 is reserved for empty voxels", "", "", "", "", "", brush.ID, "")
			continue
		}
		if _, exists := paletteByValue[entry.Value]; exists {
			result.addError("invalid_brush_payload", fmt.Sprintf("voxel_shape palette value %d must be unique", entry.Value), "", "", "", "", "", brush.ID, "")
			continue
		}
		if strings.TrimSpace(entry.MaterialID) == "" {
			result.addError("invalid_brush_payload", fmt.Sprintf("voxel_shape palette value %d requires material_id", entry.Value), "", "", "", "", "", brush.ID, "")
			continue
		}
		if _, ok := materialIDs[entry.MaterialID]; !ok {
			result.addError("missing_material_reference", fmt.Sprintf("missing material %s", entry.MaterialID), "", "", "", "", "", brush.ID, "")
			continue
		}
		paletteByValue[entry.Value] = entry.MaterialID
	}

	for _, voxel := range brush.VoxelShape.Voxels {
		if voxel.Value == 0 {
			result.addError("invalid_brush_payload", "voxel_shape voxels must use non-zero palette values", "", "", "", "", "", brush.ID, "")
			continue
		}
		if _, ok := paletteByValue[voxel.Value]; !ok {
			result.addError("invalid_brush_payload", fmt.Sprintf("voxel_shape voxel value %d is missing from palette", voxel.Value), "", "", "", "", "", brush.ID, "")
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
