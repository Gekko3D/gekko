package content

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type NavValidationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type NavValidationOptions struct {
	DocumentPath string
}

type NavValidationResult struct {
	Issues         []NavValidationIssue `json:"issues,omitempty"`
	HardErrorCount int                  `json:"hard_error_count"`
}

func (r NavValidationResult) HasErrors() bool {
	return r.HardErrorCount > 0
}

func (r NavValidationResult) Error() string {
	if len(r.Issues) == 0 {
		return ""
	}
	return r.Issues[0].Message
}

func ValidateNavManifest(def *NavManifestDef, opts NavValidationOptions) NavValidationResult {
	result := NavValidationResult{}
	if def == nil {
		result.addError("nil_nav_manifest", "nav manifest is nil")
		return result
	}
	EnsureNavManifestDefaults(def)
	if strings.TrimSpace(def.NavID) == "" {
		result.addError("empty_nav_id", "nav manifest nav_id is required")
	}
	if def.SchemaVersion != CurrentNavManifestSchemaVersion {
		result.addError("unsupported_schema_version", fmt.Sprintf("unsupported nav manifest schema version %d", def.SchemaVersion))
	}
	if strings.TrimSpace(def.BuilderVersion) == "" {
		result.addError("empty_builder_version", "nav manifest builder_version is required")
	}
	if def.ChunkSize <= 0 {
		result.addError("invalid_chunk_size", "nav manifest chunk_size must be positive")
	}
	if def.VoxelResolution <= 0 {
		result.addError("invalid_voxel_resolution", "nav manifest voxel_resolution must be positive")
	}
	if len(def.AgentProfiles) == 0 {
		result.addError("empty_agent_profiles", "nav manifest requires at least one agent profile")
	}
	profileIDs := map[string]struct{}{}
	for _, profile := range def.AgentProfiles {
		validateNavAgentProfile(&result, profile, profileIDs)
	}
	sectorCoords := map[TerrainChunkCoordDef]struct{}{}
	for _, sector := range def.Sectors {
		if _, ok := sectorCoords[sector.Coord]; ok {
			result.addError("duplicate_sector_coord", fmt.Sprintf("duplicate nav sector coord %s", TerrainChunkKey(sector.Coord)))
			continue
		}
		sectorCoords[sector.Coord] = struct{}{}
		if !navBoundsValid(sector.BoundsMin, sector.BoundsMax) {
			result.addError("invalid_sector_bounds", fmt.Sprintf("nav sector %s has invalid bounds", TerrainChunkKey(sector.Coord)))
		}
	}
	for _, sector := range def.Sectors {
		validateNavSectorRefs(&result, sector, sectorCoords)
	}
	tileKeys := map[string]struct{}{}
	for _, entry := range def.Tiles {
		validateNavTileEntry(&result, entry, profileIDs, tileKeys, opts)
	}
	return result
}

func ValidateNavTile(def *NavTileDef) NavValidationResult {
	result := NavValidationResult{}
	if def == nil {
		result.addError("nil_nav_tile", "nav tile is nil")
		return result
	}
	EnsureNavTileDefaults(def)
	if strings.TrimSpace(def.NavID) == "" {
		result.addError("empty_nav_id", "nav tile nav_id is required")
	}
	if def.SchemaVersion != CurrentNavTileSchemaVersion {
		result.addError("unsupported_tile_schema_version", fmt.Sprintf("unsupported nav tile schema version %d", def.SchemaVersion))
	}
	if strings.TrimSpace(def.AgentProfileID) == "" {
		result.addError("empty_agent_profile_id", "nav tile agent_profile_id is required")
	}
	if strings.TrimSpace(def.BuilderVersion) == "" {
		result.addError("empty_builder_version", "nav tile builder_version is required")
	}
	if def.PayloadKind != NavTilePayloadJSONV1 {
		result.addError("invalid_payload_kind", fmt.Sprintf("unsupported nav tile payload kind %q", def.PayloadKind))
	}
	if !navBoundsValid(def.BoundsMin, def.BoundsMax) {
		result.addError("invalid_tile_bounds", "nav tile bounds are invalid")
	}
	for i, polygon := range def.Polygons {
		if len(polygon.Vertices) < 3 {
			result.addError("invalid_polygon_vertices", fmt.Sprintf("nav polygon %d requires at least three vertices", i))
			continue
		}
		for _, vertexIndex := range polygon.Vertices {
			if vertexIndex < 0 || vertexIndex >= len(def.Vertices) {
				result.addError("invalid_polygon_vertex_ref", fmt.Sprintf("nav polygon %d references missing vertex %d", i, vertexIndex))
			}
		}
	}
	for _, link := range def.OffMeshLinks {
		validateNavOffMeshLink(&result, link)
	}
	return result
}

func validateNavAgentProfile(result *NavValidationResult, profile NavAgentProfileDef, seen map[string]struct{}) {
	id := strings.TrimSpace(profile.ID)
	if id == "" {
		result.addError("empty_agent_profile_id", "nav agent profile id is required")
		return
	}
	if _, ok := seen[id]; ok {
		result.addError("duplicate_agent_profile_id", fmt.Sprintf("duplicate nav agent profile id %s", id))
		return
	}
	seen[id] = struct{}{}
	if profile.Radius <= 0 {
		result.addError("invalid_agent_radius", fmt.Sprintf("nav agent profile %s radius must be positive", id))
	}
	if profile.Height <= 0 {
		result.addError("invalid_agent_height", fmt.Sprintf("nav agent profile %s height must be positive", id))
	}
	if profile.CanCrouch && profile.CrouchHeight <= 0 {
		result.addError("invalid_agent_crouch_height", fmt.Sprintf("nav agent profile %s crouch_height must be positive when crouch is enabled", id))
	}
	if profile.CrouchHeight > 0 && profile.CrouchHeight > profile.Height {
		result.addError("invalid_agent_crouch_height", fmt.Sprintf("nav agent profile %s crouch_height must not exceed height", id))
	}
	if profile.StepHeight <= 0 {
		result.addError("invalid_agent_step_height", fmt.Sprintf("nav agent profile %s step_height must be positive", id))
	}
	if profile.MaxSlopeDegrees <= 0 || profile.MaxSlopeDegrees > 89 {
		result.addError("invalid_agent_slope", fmt.Sprintf("nav agent profile %s max_slope_degrees must be > 0 and <= 89", id))
	}
	if profile.MaxDropHeight < 0 || profile.MaxJumpUp < 0 || profile.MaxJumpDown < 0 || profile.MaxJumpDistance < 0 {
		result.addError("invalid_agent_link_limits", fmt.Sprintf("nav agent profile %s drop and jump limits must be non-negative", id))
	}
}

func validateNavTileEntry(result *NavValidationResult, entry NavTileEntryDef, profileIDs map[string]struct{}, seen map[string]struct{}, opts NavValidationOptions) {
	key := entry.AgentProfileID + "|" + TerrainChunkKey(entry.Coord)
	if _, ok := seen[key]; ok {
		result.addError("duplicate_tile_entry", fmt.Sprintf("duplicate nav tile entry %s", key))
		return
	}
	seen[key] = struct{}{}
	if strings.TrimSpace(entry.AgentProfileID) == "" {
		result.addError("empty_tile_agent_profile_id", fmt.Sprintf("nav tile %s agent_profile_id is required", TerrainChunkKey(entry.Coord)))
	} else if _, ok := profileIDs[entry.AgentProfileID]; !ok {
		result.addError("missing_tile_agent_profile", fmt.Sprintf("nav tile %s references missing agent profile %s", TerrainChunkKey(entry.Coord), entry.AgentProfileID))
	}
	if strings.TrimSpace(entry.TilePath) == "" {
		result.addError("empty_tile_path", fmt.Sprintf("nav tile %s tile_path is required", TerrainChunkKey(entry.Coord)))
	} else if strings.ToLower(filepath.Ext(entry.TilePath)) != ".gknavtile" {
		result.addError("invalid_tile_path", fmt.Sprintf("nav tile path must point to a .gknavtile: %s", entry.TilePath))
	} else if opts.DocumentPath != "" {
		if _, err := os.Stat(ResolveNavTilePath(entry, opts.DocumentPath)); err != nil {
			result.addError("missing_tile_file", fmt.Sprintf("missing nav tile %s", entry.TilePath))
		}
	}
	if entry.PayloadKind != "" && entry.PayloadKind != NavTilePayloadJSONV1 {
		result.addError("invalid_tile_payload_kind", fmt.Sprintf("unsupported nav tile payload kind %q", entry.PayloadKind))
	}
	if !navBoundsValid(entry.BoundsMin, entry.BoundsMax) {
		result.addError("invalid_tile_bounds", fmt.Sprintf("nav tile %s has invalid bounds", TerrainChunkKey(entry.Coord)))
	}
}

func validateNavSectorRefs(result *NavValidationResult, sector NavSectorEntryDef, sectorCoords map[TerrainChunkCoordDef]struct{}) {
	sectorKey := TerrainChunkKey(sector.Coord)
	validateNavSectorRefList(result, sectorKey, "adjacent_sector_refs", sector.AdjacentSectorRefs, sectorCoords)
	validateNavSectorRefList(result, sectorKey, "visible_sector_refs", sector.VisibleSectorRefs, sectorCoords)
	for _, link := range sector.Links {
		if _, ok := sectorCoords[link.To]; !ok {
			result.addError("missing_sector_link_ref", fmt.Sprintf("nav sector %s link references missing sector %s", sectorKey, TerrainChunkKey(link.To)))
		}
		if link.Cost < 0 {
			result.addError("invalid_sector_link_cost", fmt.Sprintf("nav sector %s link cost must be non-negative", sectorKey))
		}
	}
}

func validateNavSectorRefList(result *NavValidationResult, sectorKey string, field string, refs []TerrainChunkCoordDef, sectorCoords map[TerrainChunkCoordDef]struct{}) {
	seen := map[TerrainChunkCoordDef]struct{}{}
	for _, ref := range refs {
		if _, ok := sectorCoords[ref]; !ok {
			result.addError("missing_sector_ref", fmt.Sprintf("nav sector %s %s references missing sector %s", sectorKey, field, TerrainChunkKey(ref)))
			continue
		}
		if _, ok := seen[ref]; ok {
			result.addError("duplicate_sector_ref", fmt.Sprintf("nav sector %s %s references sector %s more than once", sectorKey, field, TerrainChunkKey(ref)))
			continue
		}
		seen[ref] = struct{}{}
	}
}

func validateNavOffMeshLink(result *NavValidationResult, link NavOffMeshLinkDef) {
	if strings.TrimSpace(link.ID) == "" {
		result.addError("empty_off_mesh_link_id", "nav off-mesh link id is required")
	}
	if strings.TrimSpace(link.Kind) == "" {
		result.addError("empty_off_mesh_link_kind", "nav off-mesh link kind is required")
	}
	if link.Radius < 0 {
		result.addError("invalid_off_mesh_link_radius", fmt.Sprintf("nav off-mesh link %s radius must be non-negative", link.ID))
	}
	if link.Cost < 0 {
		result.addError("invalid_off_mesh_link_cost", fmt.Sprintf("nav off-mesh link %s cost must be non-negative", link.ID))
	}
}

func navBoundsValid(min, max [3]float32) bool {
	return max[0] > min[0] && max[1] > min[1] && max[2] > min[2]
}

func (r *NavValidationResult) addError(code string, message string) {
	r.Issues = append(r.Issues, NavValidationIssue{
		Code:    code,
		Message: message,
	})
	r.HardErrorCount++
}
