package content

import (
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
	}

	seenSources := map[TerrainChunkCoordDef]struct{}{}
	for _, entry := range def.SourceTiles {
		if _, exists := seenSources[entry.Coord]; exists {
			result.addError("duplicate_source_tile", fmt.Sprintf("duplicate navigation source tile %s", TerrainChunkKey(entry.Coord)))
		}
		seenSources[entry.Coord] = struct{}{}
		validateNavEntry(&result, entry.TilePath, NavSourceTileExtension, entry.SourceHash, entry.DependencyHash)
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
		validateNavEntry(&result, entry.TilePath, NavGraphTileExtension, entry.SourceHash, entry.DependencyHash)
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
	}
	return result
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

func validateNavEntry(result *NavGraphValidationResult, path, extension, sourceHash, dependencyHash string) {
	if strings.TrimSpace(path) == "" {
		result.addError("empty_tile_path", "navigation tile_path is required")
	} else if !strings.EqualFold(filepath.Ext(path), extension) {
		result.addError("invalid_tile_path", fmt.Sprintf("navigation tile_path must end in %s: %s", extension, path))
	}
	validateHashes(result, sourceHash, dependencyHash)
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
