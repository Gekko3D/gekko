package content

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

type NavGraphValidationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type NavGraphValidationResult struct {
	Issues         []NavGraphValidationIssue `json:"issues,omitempty"`
	HardErrorCount int                       `json:"hard_error_count"`
}

func (r NavGraphValidationResult) HasErrors() bool { return r.HardErrorCount > 0 }

func (r NavGraphValidationResult) Error() string {
	if len(r.Issues) == 0 {
		return ""
	}
	return r.Issues[0].Message
}

func (r *NavGraphValidationResult) addError(code, message string) {
	r.Issues = append(r.Issues, NavGraphValidationIssue{Code: code, Message: message})
	r.HardErrorCount++
}

func ValidateNavGraphManifest(def *NavGraphManifestDef) NavGraphValidationResult {
	result := NavGraphValidationResult{}
	if def == nil {
		result.addError("nil_manifest", "navigation graph manifest is nil")
		return result
	}
	validateNavHeader(&result, def.NavID, def.SchemaVersion, CurrentNavGraphManifestSchemaVersion, def.BuilderVersion)
	if def.ChunkSize <= 0 {
		result.addError("invalid_chunk_size", "navigation graph chunk_size must be positive")
	}
	validatePositive(&result, "invalid_voxel_resolution", "navigation graph voxel_resolution", def.VoxelResolution)

	profiles := make(map[string]struct{}, len(def.AgentProfiles))
	for _, profile := range def.AgentProfiles {
		if strings.TrimSpace(profile.ID) == "" {
			result.addError("empty_agent_profile_id", "navigation agent profile id is required")
		} else if _, exists := profiles[profile.ID]; exists {
			result.addError("duplicate_agent_profile_id", fmt.Sprintf("duplicate navigation agent profile id %q", profile.ID))
		}
		profiles[profile.ID] = struct{}{}
		validatePositive(&result, "invalid_agent_radius", "navigation agent radius", profile.Radius)
		validatePositive(&result, "invalid_agent_height", "navigation agent height", profile.Height)
		validateNonNegative(&result, "invalid_agent_step_height", "navigation agent step_height", profile.StepHeight)
		if !finite(profile.MaxSlopeDegrees) || profile.MaxSlopeDegrees < 0 || profile.MaxSlopeDegrees >= 90 {
			result.addError("invalid_agent_max_slope", "navigation agent max_slope_degrees must be finite and in [0, 90)")
		}
		for _, limit := range []struct {
			label string
			value float32
		}{
			{"max_drop_height", profile.MaxDropHeight},
			{"max_jump_distance", profile.MaxJumpDistance},
			{"max_jump_rise", profile.MaxJumpRise},
			{"max_vault_height", profile.MaxVaultHeight},
			{"max_mantle_height", profile.MaxMantleHeight},
			{"jump_speed", profile.JumpSpeed},
			{"jump_horizontal_speed", profile.JumpHorizontalSpeed},
			{"gravity", profile.Gravity},
		} {
			validateNonNegative(&result, "invalid_agent_"+limit.label, "navigation agent "+limit.label, limit.value)
		}
		if !navCapabilitiesValid(profile.Capabilities) {
			result.addError("invalid_agent_capabilities", "navigation agent capabilities must contain sorted, unique, non-empty values")
		}
	}
	for i, ladder := range def.LadderVolumes {
		if strings.TrimSpace(ladder.ID) == "" {
			result.addError("empty_ladder_id", "navigation ladder id is required")
		}
		if i > 0 && def.LadderVolumes[i-1].ID >= ladder.ID {
			result.addError("unsorted_ladders", "navigation ladders must be sorted by unique id")
		}
		if !validVec3(ladder.BoundsCenter) || !validVec3(ladder.BoundsHalfExtents) || ladder.BoundsHalfExtents[0] <= 0 || ladder.BoundsHalfExtents[1] <= 0 || ladder.BoundsHalfExtents[2] <= 0 {
			result.addError("invalid_ladder_bounds", fmt.Sprintf("navigation ladder %q requires finite positive bounds", ladder.ID))
		}
		if !finite(ladder.ClimbSpeed) || ladder.ClimbSpeed < 0 {
			result.addError("invalid_ladder_speed", fmt.Sprintf("navigation ladder %q climb_speed must be finite and non-negative", ladder.ID))
		}
		if !finite(ladder.Health) || ladder.Health < 0 {
			result.addError("invalid_ladder_health", fmt.Sprintf("navigation ladder %q health must be finite and non-negative", ladder.ID))
		}
		if (ladder.MountBottom == nil) != (ladder.MountTop == nil) {
			result.addError("incomplete_ladder_mounts", fmt.Sprintf("navigation ladder %q must provide both mounts", ladder.ID))
		} else if ladder.MountBottom != nil && (!validVec3(*ladder.MountBottom) || !validVec3(*ladder.MountTop) || (*ladder.MountTop)[1] <= (*ladder.MountBottom)[1]) {
			result.addError("invalid_ladder_mounts", fmt.Sprintf("navigation ladder %q top mount must be finite and above bottom mount", ladder.ID))
		}
	}
	for i, door := range def.Doors {
		if strings.TrimSpace(door.ID) == "" {
			result.addError("empty_door_id", "navigation door id is required")
		}
		if i > 0 && def.Doors[i-1].ID >= door.ID {
			result.addError("unsorted_doors", "navigation doors must be sorted by unique id")
		}
		if !validVec3(door.BoundsCenter) || !validVec3(door.BoundsHalfExtents) || door.BoundsHalfExtents[0] <= 0 || door.BoundsHalfExtents[1] <= 0 || door.BoundsHalfExtents[2] <= 0 {
			result.addError("invalid_door_bounds", fmt.Sprintf("navigation door %q requires finite positive bounds", door.ID))
		}
		if !validVec3(door.OpenOffset) {
			result.addError("invalid_door_open_offset", fmt.Sprintf("navigation door %q requires a finite open offset", door.ID))
		}
	}
	for i, carrier := range def.Carriers {
		if strings.TrimSpace(carrier.ID) == "" {
			result.addError("empty_carrier_id", "navigation carrier id is required")
		}
		if i > 0 && def.Carriers[i-1].ID >= carrier.ID {
			result.addError("unsorted_carriers", "navigation carriers must be sorted by unique id")
		}
		if !validVec3(carrier.BoundsHalfExtents) || carrier.BoundsHalfExtents[0] <= 0 || carrier.BoundsHalfExtents[1] <= 0 || carrier.BoundsHalfExtents[2] <= 0 {
			result.addError("invalid_carrier_bounds", fmt.Sprintf("navigation carrier %q requires finite positive bounds", carrier.ID))
		}
		if !finite(carrier.Speed) || carrier.Speed < 0 {
			result.addError("invalid_carrier_speed", fmt.Sprintf("navigation carrier %q speed must be finite and non-negative", carrier.ID))
		}
		if len(carrier.Stops) < 2 {
			result.addError("invalid_carrier_stops", fmt.Sprintf("navigation carrier %q requires at least two stops", carrier.ID))
		}
		for stopIndex, stop := range carrier.Stops {
			if strings.TrimSpace(stop.ID) == "" || stopIndex > 0 && carrier.Stops[stopIndex-1].ID >= stop.ID {
				result.addError("invalid_carrier_stop_id", fmt.Sprintf("navigation carrier %q stops must have sorted unique ids", carrier.ID))
			}
			if !validVec3(stop.BoundsCenter) {
				result.addError("invalid_carrier_stop", fmt.Sprintf("navigation carrier %q stop %q requires a finite position", carrier.ID, stop.ID))
			}
			for controllerIndex, controller := range stop.Controllers {
				if strings.TrimSpace(controller.ID) == "" || controllerIndex > 0 && stop.Controllers[controllerIndex-1].ID >= controller.ID {
					result.addError("invalid_carrier_controller_id", fmt.Sprintf("navigation carrier %q stop %q controllers must have sorted unique ids", carrier.ID, stop.ID))
				}
				if !validVec3(controller.BoundsCenter) || !validVec3(controller.BoundsHalfExtents) ||
					controller.BoundsHalfExtents[0] <= 0 || controller.BoundsHalfExtents[1] <= 0 || controller.BoundsHalfExtents[2] <= 0 {
					result.addError("invalid_carrier_controller", fmt.Sprintf("navigation carrier %q controller %q requires finite positive bounds", carrier.ID, controller.ID))
				}
			}
		}
	}

	seenSources := map[TerrainChunkCoordDef]struct{}{}
	for _, entry := range def.SourceTiles {
		if _, exists := seenSources[entry.Coord]; exists {
			result.addError("duplicate_source_tile", fmt.Sprintf("duplicate navigation source tile %s", TerrainChunkKey(entry.Coord)))
		}
		seenSources[entry.Coord] = struct{}{}
		validateNavEntry(&result, entry.TilePath, NavSourceTileExtension, entry.SourceHash, entry.DependencyHash, entry.ContentHash, entry.ByteSize)
	}

	type graphKey struct {
		Coord   TerrainChunkCoordDef
		Profile string
	}
	seenGraphs := map[graphKey]struct{}{}
	for _, entry := range def.GraphTiles {
		key := graphKey{Coord: entry.Coord, Profile: entry.AgentProfileID}
		if _, exists := seenGraphs[key]; exists {
			result.addError("duplicate_graph_tile", fmt.Sprintf("duplicate navigation graph tile %s for profile %q", TerrainChunkKey(entry.Coord), entry.AgentProfileID))
		}
		seenGraphs[key] = struct{}{}
		if _, exists := profiles[entry.AgentProfileID]; !exists {
			result.addError("missing_agent_profile", fmt.Sprintf("navigation graph tile references missing profile %q", entry.AgentProfileID))
		}
		if _, exists := seenSources[entry.Coord]; !exists {
			result.addError("missing_source_tile", fmt.Sprintf("navigation graph tile references missing source tile %s", TerrainChunkKey(entry.Coord)))
		}
		validateNavEntry(&result, entry.TilePath, NavGraphTileExtension, entry.SourceHash, entry.DependencyHash, entry.ContentHash, entry.ByteSize)
	}
	return result
}

func ValidateNavSourceTile(def *NavSourceTileDef) NavGraphValidationResult {
	result := NavGraphValidationResult{}
	if def == nil {
		result.addError("nil_source_tile", "navigation source tile is nil")
		return result
	}
	validateNavHeader(&result, def.NavID, def.SchemaVersion, CurrentNavSourceTileSchemaVersion, def.BuilderVersion)
	validateHashes(&result, def.SourceHash, def.DependencyHash)
	if def.ChunkSize <= 0 {
		result.addError("invalid_chunk_size", "navigation source tile chunk_size must be positive")
	}
	if def.VoxelResolution != 0 {
		validatePositive(&result, "invalid_voxel_resolution", "navigation source tile voxel_resolution", def.VoxelResolution)
	}
	validateNavVoxelRuns(&result, "solid", def.SolidRuns, def.ChunkSize)
	validateNavVoxelRuns(&result, "blocked", def.BlockedRuns, def.ChunkSize)
	for i, span := range def.Spans {
		if span.ID != uint32(i) {
			result.addError("invalid_span_id", fmt.Sprintf("navigation span at index %d must have id %d", i, i))
		}
		if i > 0 && navSpanLess(span, def.Spans[i-1]) {
			result.addError("unsorted_spans", "navigation spans must be sorted by x, z, then y")
		}
		if !finite(span.SupportHeight) || !finite(span.CeilingHeight) || span.CeilingHeight <= span.SupportHeight {
			result.addError("invalid_span_bounds", fmt.Sprintf("navigation span %d must have finite ceiling_height above support_height", span.ID))
		}
		validatePositive(&result, "invalid_span_headroom", fmt.Sprintf("navigation span %d headroom", span.ID), span.Headroom)
		validateNonNegative(&result, "invalid_span_clearance", fmt.Sprintf("navigation span %d clearance_radius", span.ID), span.ClearanceRadius)
	}
	return result
}

func validateNavVoxelRuns(result *NavGraphValidationResult, label string, runs []NavVoxelRunDef, chunkSize int) {
	for i, run := range runs {
		if run.X < 0 || run.Y < 0 || run.Z < 0 || run.X >= chunkSize || run.Y >= chunkSize || run.Z >= chunkSize || run.Count <= 0 || run.Count > chunkSize-run.Y {
			result.addError("invalid_voxel_run", fmt.Sprintf("navigation %s voxel run %d is outside chunk", label, i))
		}
		if i == 0 {
			continue
		}
		previous := runs[i-1]
		if run.X < previous.X || run.X == previous.X && (run.Z < previous.Z || run.Z == previous.Z && run.Y <= previous.Y+previous.Count) {
			result.addError("unsorted_voxel_runs", fmt.Sprintf("navigation %s voxel runs must be sorted, disjoint, and merged", label))
		}
	}
}

func ValidateNavGraphTile(def *NavGraphTileDef) NavGraphValidationResult {
	result := NavGraphValidationResult{}
	if def == nil {
		result.addError("nil_graph_tile", "navigation graph tile is nil")
		return result
	}
	validateNavHeader(&result, def.NavID, def.SchemaVersion, CurrentNavGraphTileSchemaVersion, def.BuilderVersion)
	if strings.TrimSpace(def.AgentProfileID) == "" {
		result.addError("empty_agent_profile_id", "navigation graph tile agent_profile_id is required")
	}
	validateHashes(&result, def.SourceHash, def.DependencyHash)

	spans := make(map[uint32]struct{}, len(def.SpanIDs))
	for i, id := range def.SpanIDs {
		if i > 0 && id <= def.SpanIDs[i-1] {
			result.addError("unsorted_span_ids", "navigation graph span_ids must be strictly increasing")
		}
		spans[id] = struct{}{}
	}
	for i, transition := range def.SpanTransitions {
		if _, exists := spans[transition.From]; !exists {
			result.addError("missing_transition_from_span", fmt.Sprintf("span transition %d references missing from span %d", i, transition.From))
		}
		if transition.To.Tile == def.Coord {
			if _, exists := spans[transition.To.Span]; !exists {
				result.addError("missing_transition_to_span", fmt.Sprintf("span transition %d references missing local to span %d", i, transition.To.Span))
			}
		}
		validateTransitionNumbers(&result, fmt.Sprintf("span transition %d", i), transition.Kind, transition.StepDelta, transition.Width, transition.MinHeadroom, transition.MinClearance, transition.Cost)
		validateTransitionTraversal(&result, fmt.Sprintf("span transition %d", i), transition.Kind, transition.Traversal)
		validateTransitionGate(&result, fmt.Sprintf("span transition %d", i), transition.Gate, transition.Traversal)
	}

	regions := make(map[uint32]struct{}, len(def.Regions))
	assignedSpans := map[uint32]uint32{}
	for i, region := range def.Regions {
		if region.ID != uint32(i) {
			result.addError("invalid_region_id", fmt.Sprintf("navigation region at index %d must have id %d", i, i))
		}
		regions[region.ID] = struct{}{}
		validateRegionBounds(&result, region)
		if len(region.SpanRuns) == 0 {
			result.addError("empty_region", fmt.Sprintf("navigation region %d must contain spans", region.ID))
		}
		for _, run := range region.SpanRuns {
			if run.Count == 0 || run.Start > math.MaxUint32-(run.Count-1) {
				result.addError("invalid_span_run", fmt.Sprintf("navigation region %d has invalid span run", region.ID))
				continue
			}
			for offset := uint32(0); offset < run.Count; offset++ {
				id := run.Start + offset
				if _, exists := spans[id]; !exists {
					result.addError("missing_region_span", fmt.Sprintf("navigation region %d references missing span %d", region.ID, id))
				}
				if other, exists := assignedSpans[id]; exists {
					result.addError("duplicate_region_span", fmt.Sprintf("navigation span %d belongs to regions %d and %d", id, other, region.ID))
				}
				assignedSpans[id] = region.ID
			}
		}
	}
	for i, transition := range def.Transitions {
		if transition.ID != uint32(i) {
			result.addError("invalid_region_transition_id", fmt.Sprintf("navigation region transition at index %d must have id %d", i, i))
		}
		if _, exists := regions[transition.FromRegion]; !exists {
			result.addError("missing_transition_from_region", fmt.Sprintf("region transition %d references missing from region %d", transition.ID, transition.FromRegion))
		}
		if transition.ToTile == def.Coord {
			if _, exists := regions[transition.ToRegion]; !exists {
				result.addError("missing_transition_to_region", fmt.Sprintf("region transition %d references missing local to region %d", transition.ID, transition.ToRegion))
			}
		}
		if !validVec3(transition.CrossingStart) || !validVec3(transition.CrossingEnd) {
			result.addError("invalid_transition_crossing", fmt.Sprintf("region transition %d crossing must be finite", transition.ID))
		}
		validateTransitionNumbers(&result, fmt.Sprintf("region transition %d", transition.ID), transition.Kind, 0, transition.Width, transition.MinHeadroom, transition.MinClearance, transition.Cost)
		validateTransitionTraversal(&result, fmt.Sprintf("region transition %d", transition.ID), transition.Kind, transition.Traversal)
		validateTransitionGate(&result, fmt.Sprintf("region transition %d", transition.ID), transition.Gate, transition.Traversal)
	}
	return result
}

func validateTransitionTraversal(result *NavGraphValidationResult, label, kind string, traversal *NavTraversalDef) {
	if navTransitionRequiresTraversal(kind) && traversal == nil {
		result.addError("missing_transition_traversal", label+" "+kind+" traversal is required")
		return
	}
	if traversal == nil {
		return
	}
	if strings.TrimSpace(traversal.StableLinkID()) == "" {
		result.addError("empty_transition_traversal_id", label+" traversal id is required")
	}
	if !validVec3(traversal.Start) || !validVec3(traversal.End) || traversal.Start == traversal.End {
		result.addError("invalid_transition_traversal", label+" traversal endpoints must be finite and distinct")
	}
	if traversal.Apex != (Vec3{}) && !validVec3(traversal.Apex) {
		result.addError("invalid_transition_traversal_apex", label+" traversal apex must be finite")
	}
	if !finite(traversal.Duration) || traversal.Duration < 0 {
		result.addError("invalid_transition_traversal_duration", label+" traversal duration must be finite and non-negative")
	}
	if !finite(traversal.Speed) || traversal.Speed < 0 {
		result.addError("invalid_transition_traversal_speed", label+" traversal speed must be finite and non-negative")
	}
	if !finite(traversal.LaunchSpeed) || traversal.LaunchSpeed < 0 {
		result.addError("invalid_transition_traversal_launch_speed", label+" traversal launch speed must be finite and non-negative")
	}
	if kind == NavTransitionCarrier {
		carrier := traversal.Carrier
		if carrier == nil {
			result.addError("missing_carrier_traversal", label+" carrier traversal metadata is required")
		} else if strings.TrimSpace(carrier.CarrierID) == "" || strings.TrimSpace(carrier.FromStop) == "" ||
			strings.TrimSpace(carrier.ToStop) == "" || carrier.FromStop == carrier.ToStop ||
			!validVec3(carrier.Board) {
			result.addError("invalid_carrier_traversal", label+" carrier traversal requires a carrier, distinct stops, and a finite boarding point")
		}
	} else if traversal.Carrier != nil {
		result.addError("unexpected_carrier_traversal", label+" carrier metadata requires a carrier transition")
	}
	if support := traversal.LandingSupport; support != nil {
		if kind != NavTransitionDrop {
			result.addError("unexpected_landing_support", label+" landing support metadata requires a drop transition")
		} else if strings.TrimSpace(support.ID) == "" || strings.TrimSpace(support.Stop) == "" || !validVec3(support.Point) {
			result.addError("invalid_landing_support", label+" landing support requires an id, stop, and finite landing point")
		}
	}
}

func navTransitionRequiresTraversal(kind string) bool {
	switch kind {
	case NavTransitionDrop, NavTransitionJump, NavTransitionLadder, NavTransitionVault, NavTransitionMantle, NavTransitionCarrier:
		return true
	default:
		return false
	}
}

func validateTransitionGate(result *NavGraphValidationResult, label string, gate *NavTransitionGateDef, traversal *NavTraversalDef) {
	if gate == nil {
		return
	}
	if gate.Kind != NavGateDoor {
		result.addError("unsupported_transition_gate", label+" gate kind must be door")
	}
	if strings.TrimSpace(gate.ID) == "" {
		result.addError("empty_transition_gate_id", label+" gate id is required")
	}
	if traversal == nil {
		result.addError("missing_gated_transition_traversal", label+" gated transition requires entry and exit points")
	}
}

func navCapabilitiesValid(capabilities []string) bool {
	for i, capability := range capabilities {
		if strings.TrimSpace(capability) == "" || i > 0 && capabilities[i-1] >= capability {
			return false
		}
	}
	return true
}

func validateNavHeader(result *NavGraphValidationResult, navID string, gotVersion, wantVersion int, builderVersion string) {
	if strings.TrimSpace(navID) == "" {
		result.addError("empty_nav_id", "navigation graph nav_id is required")
	}
	if gotVersion != wantVersion {
		result.addError("unsupported_schema_version", fmt.Sprintf("unsupported navigation schema version %d", gotVersion))
	}
	if strings.TrimSpace(builderVersion) == "" {
		result.addError("empty_builder_version", "navigation graph builder_version is required")
	}
}

func validateNavEntry(result *NavGraphValidationResult, path, extension, sourceHash, dependencyHash, contentHash string, byteSize int64) {
	if strings.TrimSpace(path) == "" {
		result.addError("empty_tile_path", "navigation tile_path is required")
	} else if !strings.EqualFold(filepath.Ext(path), extension) {
		result.addError("invalid_tile_path", fmt.Sprintf("navigation tile_path must end in %s: %s", extension, path))
	}
	validateHashes(result, sourceHash, dependencyHash)
	if decoded, err := hex.DecodeString(contentHash); err != nil || len(decoded) != sha256.Size {
		result.addError("invalid_content_hash", "navigation tile content_hash must be a SHA-256 hex digest")
	}
	if byteSize <= 0 {
		result.addError("invalid_byte_size", "navigation tile byte_size must be positive")
	}
}

func validateHashes(result *NavGraphValidationResult, sourceHash, dependencyHash string) {
	if strings.TrimSpace(sourceHash) == "" {
		result.addError("empty_source_hash", "navigation source_hash is required")
	}
	if strings.TrimSpace(dependencyHash) == "" {
		result.addError("empty_dependency_hash", "navigation dependency_hash is required")
	}
}

func validateRegionBounds(result *NavGraphValidationResult, region NavRegionDef) {
	if !validVec3(region.BoundsMin) || !validVec3(region.BoundsMax) || !validVec3(region.Center) {
		result.addError("invalid_region_bounds", fmt.Sprintf("navigation region %d bounds and center must be finite", region.ID))
		return
	}
	for axis := range region.BoundsMin {
		if region.BoundsMax[axis] < region.BoundsMin[axis] {
			result.addError("invalid_region_bounds", fmt.Sprintf("navigation region %d bounds_max must not be below bounds_min", region.ID))
		}
		if region.Center[axis] < region.BoundsMin[axis] || region.Center[axis] > region.BoundsMax[axis] {
			result.addError("invalid_region_center", fmt.Sprintf("navigation region %d center must be inside bounds", region.ID))
		}
	}
	if !finite(region.HeightMin) || !finite(region.HeightMax) || region.HeightMax < region.HeightMin {
		result.addError("invalid_region_height", fmt.Sprintf("navigation region %d height range is invalid", region.ID))
	}
}

func validateTransitionNumbers(result *NavGraphValidationResult, label, kind string, stepDelta, width, headroom, clearance, cost float32) {
	if strings.TrimSpace(kind) == "" {
		result.addError("empty_transition_kind", label+" kind is required")
	}
	if !finite(stepDelta) {
		result.addError("invalid_transition_step", label+" step_delta must be finite")
	}
	validatePositive(result, "invalid_transition_width", label+" width", width)
	validateNonNegative(result, "invalid_transition_headroom", label+" min_headroom", headroom)
	validateNonNegative(result, "invalid_transition_clearance", label+" min_clearance", clearance)
	validateNonNegative(result, "invalid_transition_cost", label+" cost", cost)
}

func validatePositive(result *NavGraphValidationResult, code, label string, value float32) {
	if !finite(value) || value <= 0 {
		result.addError(code, label+" must be finite and positive")
	}
}

func validateNonNegative(result *NavGraphValidationResult, code, label string, value float32) {
	if !finite(value) || value < 0 {
		result.addError(code, label+" must be finite and non-negative")
	}
}

func finite(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}

func validVec3(value Vec3) bool {
	return finite(value[0]) && finite(value[1]) && finite(value[2])
}

func navSpanLess(a, b NavSpanDef) bool {
	if a.X != b.X {
		return a.X < b.X
	}
	if a.Z != b.Z {
		return a.Z < b.Z
	}
	return a.Y < b.Y
}
