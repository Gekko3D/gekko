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
	clearanceSourceTileKeys := map[string]struct{}{}
	for _, entry := range def.ClearanceSourceTiles {
		validateNavClearanceSourceTileEntry(&result, entry, clearanceSourceTileKeys, opts)
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
	polygonIDs := map[string]struct{}{}
	for i, polygon := range def.Polygons {
		if len(polygon.Vertices) < 3 {
			result.addError("invalid_polygon_vertices", fmt.Sprintf("nav polygon %d requires at least three vertices", i))
			continue
		}
		if strings.TrimSpace(polygon.ID) != "" {
			if _, ok := polygonIDs[polygon.ID]; ok {
				result.addError("duplicate_polygon_id", fmt.Sprintf("nav polygon id %s is duplicated", polygon.ID))
			}
			polygonIDs[polygon.ID] = struct{}{}
		}
		for _, vertexIndex := range polygon.Vertices {
			if vertexIndex < 0 || vertexIndex >= len(def.Vertices) {
				result.addError("invalid_polygon_vertex_ref", fmt.Sprintf("nav polygon %d references missing vertex %d", i, vertexIndex))
				continue
			}
			if !navTileVertexWithinBounds(def.Vertices[vertexIndex], def.BoundsMin, def.BoundsMax) {
				result.addError("invalid_polygon_vertex_bounds", fmt.Sprintf("nav polygon %d references vertex %d outside tile bounds: vertex=%v bounds_min=%v bounds_max=%v", i, vertexIndex, def.Vertices[vertexIndex], def.BoundsMin, def.BoundsMax))
			}
		}
	}
	validateNavTilePolygonOverlaps(&result, def)
	for _, span := range def.BorderSpans {
		validateNavBorderSpan(&result, span, polygonIDs)
	}
	for _, portal := range def.Portals {
		validateNavPortal(&result, portal, polygonIDs)
	}
	for _, link := range def.OffMeshLinks {
		validateNavOffMeshLink(&result, def.Coord, link, polygonIDs)
	}
	return result
}

func validateNavTilePolygonOverlaps(result *NavValidationResult, tile *NavTileDef) {
	if result == nil || tile == nil || len(tile.Polygons) < 2 {
		return
	}
	for i := 0; i < len(tile.Polygons); i++ {
		a := tile.Polygons[i]
		if len(a.Vertices) < 3 {
			continue
		}
		for j := i + 1; j < len(tile.Polygons); j++ {
			b := tile.Polygons[j]
			if len(b.Vertices) < 3 || !navPolygonsOverlapSameSurfaceXZ(tile, a, b) {
				continue
			}
			result.addError("overlapping_nav_polygons", fmt.Sprintf("nav polygons %s and %s overlap on the same walkable surface", firstNonEmptyNavString(a.ID, itoa(i)), firstNonEmptyNavString(b.ID, itoa(j))))
		}
	}
}

func validateNavBorderSpan(result *NavValidationResult, span NavBorderSpanDef, polygonIDs map[string]struct{}) {
	if result == nil {
		return
	}
	if strings.TrimSpace(span.PolygonID) == "" {
		result.addError("empty_border_span_polygon", "nav border span polygon_id is required")
	} else if _, ok := polygonIDs[span.PolygonID]; !ok {
		result.addError("invalid_border_span_polygon_ref", fmt.Sprintf("nav border span references missing polygon %s", span.PolygonID))
	}
	switch span.Edge {
	case NavBorderEdgeMinX, NavBorderEdgeMaxX, NavBorderEdgeMinZ, NavBorderEdgeMaxZ:
	default:
		result.addError("invalid_border_span_edge", fmt.Sprintf("nav border span edge %q is invalid", span.Edge))
	}
	if span.Max-span.Min <= 1e-4 {
		result.addError("invalid_border_span_range", fmt.Sprintf("nav border span range is invalid: min=%f max=%f", span.Min, span.Max))
	}
}

func ValidateNavTileTopology(tiles []*NavTileDef, profiles map[string]NavAgentProfileDef) NavValidationResult {
	result := NavValidationResult{}
	tileByKey := make(map[string]*NavTileDef, len(tiles))
	for _, tile := range tiles {
		if tile == nil {
			result.addError("nil_nav_topology_tile", "nav topology contains a nil tile")
			continue
		}
		if validation := ValidateNavTile(tile); validation.HasErrors() {
			for _, issue := range validation.Issues {
				result.addError(issue.Code, fmt.Sprintf("nav tile %s/%s: %s", tile.AgentProfileID, TerrainChunkKey(tile.Coord), issue.Message))
			}
			continue
		}
		key := navTopologyTileKey(tile.AgentProfileID, tile.Coord)
		if _, ok := tileByKey[key]; ok {
			result.addError("duplicate_topology_tile", fmt.Sprintf("duplicate nav topology tile %s", key))
			continue
		}
		tileByKey[key] = tile
	}
	for _, tile := range tileByKey {
		validateNavTilePortalTopology(&result, tile, tileByKey, profiles)
	}
	return result
}

func navTileVertexWithinBounds(vertex Vec3, boundsMin [3]float32, boundsMax [3]float32) bool {
	const epsilon = float32(1e-4)
	return vertex[0] >= boundsMin[0]-epsilon && vertex[0] <= boundsMax[0]+epsilon &&
		vertex[1] >= boundsMin[1]-epsilon && vertex[1] <= boundsMax[1]+epsilon &&
		vertex[2] >= boundsMin[2]-epsilon && vertex[2] <= boundsMax[2]+epsilon
}

func navPolygonsOverlapSameSurfaceXZ(tile *NavTileDef, a NavPolygonDef, b NavPolygonDef) bool {
	aBounds, ok := navPolygonBoundsForTile(tile, a)
	if !ok {
		return false
	}
	bBounds, ok := navPolygonBoundsForTile(tile, b)
	if !ok {
		return false
	}
	const epsilon = float32(1e-4)
	if !navRangesOverlapPositive(aBounds.min[0], aBounds.max[0], bBounds.min[0], bBounds.max[0], epsilon) ||
		!navRangesOverlapPositive(aBounds.min[2], aBounds.max[2], bBounds.min[2], bBounds.max[2], epsilon) ||
		!navRangesOverlapOrTouch(aBounds.min[1], aBounds.max[1], bBounds.min[1], bBounds.max[1], epsilon) {
		return false
	}
	for _, sample := range navPolygonOverlapCandidateSamples(tile, a, b, aBounds, bBounds) {
		if !navPolygonContainsXZStrict(tile, a, sample) || !navPolygonContainsXZStrict(tile, b, sample) {
			continue
		}
		aY, aOK := navPolygonHeightAtXZ(tile, a, sample)
		bY, bOK := navPolygonHeightAtXZ(tile, b, sample)
		if aOK && bOK && navAlmostEqual(aY, bY, 1e-3) {
			return true
		}
	}
	return false
}

func navPolygonOverlapCandidateSamples(tile *NavTileDef, a NavPolygonDef, b NavPolygonDef, aBounds navPolygonBounds, bBounds navPolygonBounds) []Vec3 {
	minX := maxNavFloat32(aBounds.min[0], bBounds.min[0])
	maxX := minNavFloat32(aBounds.max[0], bBounds.max[0])
	minZ := maxNavFloat32(aBounds.min[2], bBounds.min[2])
	maxZ := minNavFloat32(aBounds.max[2], bBounds.max[2])
	samples := []Vec3{{(minX + maxX) * 0.5, 0, (minZ + maxZ) * 0.5}}
	samples = appendNavPolygonVerticesInsideBoundsXZ(samples, tile, a, minX, maxX, minZ, maxZ)
	samples = appendNavPolygonVerticesInsideBoundsXZ(samples, tile, b, minX, maxX, minZ, maxZ)
	samples = append(samples,
		Vec3{minX, 0, minZ},
		Vec3{maxX, 0, minZ},
		Vec3{maxX, 0, maxZ},
		Vec3{minX, 0, maxZ},
	)
	return samples
}

func navPolygonContainsXZStrict(tile *NavTileDef, polygon NavPolygonDef, point Vec3) bool {
	if tile == nil || len(polygon.Vertices) < 3 {
		return false
	}
	inside := false
	j := len(polygon.Vertices) - 1
	const epsilon = float32(1e-5)
	for i := range polygon.Vertices {
		aIndex := polygon.Vertices[i]
		bIndex := polygon.Vertices[j]
		if aIndex < 0 || aIndex >= len(tile.Vertices) || bIndex < 0 || bIndex >= len(tile.Vertices) {
			return false
		}
		a := tile.Vertices[aIndex]
		b := tile.Vertices[bIndex]
		if navPointOnSegmentXZ(point, a, b, epsilon) {
			return false
		}
		intersects := (a[2] > point[2]) != (b[2] > point[2])
		if intersects {
			x := (b[0]-a[0])*(point[2]-a[2])/(b[2]-a[2]) + a[0]
			if point[0] < x {
				inside = !inside
			}
		}
		j = i
	}
	return inside
}

func appendNavPolygonVerticesInsideBoundsXZ(samples []Vec3, tile *NavTileDef, polygon NavPolygonDef, minX, maxX, minZ, maxZ float32) []Vec3 {
	const epsilon = float32(1e-4)
	for _, index := range polygon.Vertices {
		if index < 0 || index >= len(tile.Vertices) {
			continue
		}
		vertex := tile.Vertices[index]
		if vertex[0] < minX-epsilon || vertex[0] > maxX+epsilon || vertex[2] < minZ-epsilon || vertex[2] > maxZ+epsilon {
			continue
		}
		samples = append(samples, Vec3{vertex[0], 0, vertex[2]})
	}
	return samples
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
	if profile.NavCellSize <= 0 {
		result.addError("invalid_agent_nav_cell_size", fmt.Sprintf("nav agent profile %s nav_cell_size must be positive", id))
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
		tilePath := ResolveNavTilePath(entry, opts.DocumentPath)
		if _, err := os.Stat(tilePath); err != nil {
			result.addError("missing_tile_file", fmt.Sprintf("missing nav tile %s", entry.TilePath))
		} else {
			tile, err := LoadNavTile(tilePath)
			if err != nil {
				result.addError("invalid_tile_file", fmt.Sprintf("failed to load nav tile %s: %v", entry.TilePath, err))
			} else if validation := ValidateNavTile(tile); validation.HasErrors() {
				for _, issue := range validation.Issues {
					result.addError(issue.Code, fmt.Sprintf("nav tile %s: %s", entry.TilePath, issue.Message))
				}
			}
		}
	}
	if entry.PayloadKind != "" && entry.PayloadKind != NavTilePayloadJSONV1 {
		result.addError("invalid_tile_payload_kind", fmt.Sprintf("unsupported nav tile payload kind %q", entry.PayloadKind))
	}
	if !navBoundsValid(entry.BoundsMin, entry.BoundsMax) {
		result.addError("invalid_tile_bounds", fmt.Sprintf("nav tile %s has invalid bounds", TerrainChunkKey(entry.Coord)))
	}
}

func validateNavClearanceSourceTileEntry(result *NavValidationResult, entry NavClearanceSourceTileEntryDef, seen map[string]struct{}, opts NavValidationOptions) {
	key := TerrainChunkKey(entry.Coord)
	if _, ok := seen[key]; ok {
		result.addError("duplicate_clearance_source_tile_entry", fmt.Sprintf("duplicate nav clearance source tile entry %s", key))
		return
	}
	seen[key] = struct{}{}
	if strings.TrimSpace(entry.TilePath) == "" {
		result.addError("empty_clearance_source_tile_path", fmt.Sprintf("nav clearance source tile %s tile_path is required", TerrainChunkKey(entry.Coord)))
	} else if strings.ToLower(filepath.Ext(entry.TilePath)) != ".gknavsource" {
		result.addError("invalid_clearance_source_tile_path", fmt.Sprintf("nav clearance source tile path must point to a .gknavsource: %s", entry.TilePath))
	} else if opts.DocumentPath != "" {
		tilePath := ResolveNavClearanceSourceTilePath(entry, opts.DocumentPath)
		if _, err := os.Stat(tilePath); err != nil {
			result.addError("missing_clearance_source_tile_file", fmt.Sprintf("missing nav clearance source tile %s", entry.TilePath))
		} else {
			tile, err := LoadNavClearanceSourceTile(tilePath)
			if err != nil {
				result.addError("invalid_clearance_source_tile_file", fmt.Sprintf("failed to load nav clearance source tile %s: %v", entry.TilePath, err))
			} else if tile.Coord != entry.Coord {
				result.addError("clearance_source_tile_coord_mismatch", fmt.Sprintf("nav clearance source tile %s coord does not match manifest entry", entry.TilePath))
			}
		}
	}
	if entry.PayloadKind != "" && entry.PayloadKind != NavClearanceSourceTilePayloadJSONV1 {
		result.addError("invalid_clearance_source_tile_payload_kind", fmt.Sprintf("unsupported nav clearance source tile payload kind %q", entry.PayloadKind))
	}
	if !navBoundsValid(entry.BoundsMin, entry.BoundsMax) {
		result.addError("invalid_clearance_source_tile_bounds", fmt.Sprintf("nav clearance source tile %s has invalid bounds", TerrainChunkKey(entry.Coord)))
	}
	if entry.MaxClearanceRadius < 0 {
		result.addError("invalid_clearance_source_tile_radius", fmt.Sprintf("nav clearance source tile %s max_clearance_radius must be non-negative", TerrainChunkKey(entry.Coord)))
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

func validateNavOffMeshLink(result *NavValidationResult, tileCoord TerrainChunkCoordDef, link NavOffMeshLinkDef, polygonIDs map[string]struct{}) {
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
	if strings.TrimSpace(link.FromPolygonID) != "" {
		if _, ok := polygonIDs[link.FromPolygonID]; !ok {
			result.addError("invalid_off_mesh_from_polygon_ref", fmt.Sprintf("nav off-mesh link %s references missing from polygon %s", link.ID, link.FromPolygonID))
		}
	}
	if strings.TrimSpace(link.ToPolygonID) != "" && link.ToTileCoord == tileCoord {
		if _, ok := polygonIDs[link.ToPolygonID]; !ok {
			result.addError("invalid_off_mesh_to_polygon_ref", fmt.Sprintf("nav off-mesh link %s references missing to polygon %s", link.ID, link.ToPolygonID))
		}
	}
}

func validateNavPortal(result *NavValidationResult, portal NavPortalDef, polygonIDs map[string]struct{}) {
	if strings.TrimSpace(portal.ID) == "" {
		result.addError("empty_portal_id", "nav portal id is required")
	}
	if strings.TrimSpace(portal.FromPolygonID) == "" {
		result.addError("empty_portal_from_polygon", fmt.Sprintf("nav portal %s from_polygon_id is required", portal.ID))
	} else if _, ok := polygonIDs[portal.FromPolygonID]; !ok {
		result.addError("missing_portal_from_polygon", fmt.Sprintf("nav portal %s references missing polygon %s", portal.ID, portal.FromPolygonID))
	}
	if strings.TrimSpace(portal.ToPolygonID) == "" {
		result.addError("empty_portal_to_polygon", fmt.Sprintf("nav portal %s to_polygon_id is required", portal.ID))
	}
	if portal.Start == portal.End {
		result.addError("invalid_portal_segment", fmt.Sprintf("nav portal %s has zero-length segment", portal.ID))
	}
	if portal.Cost < 0 {
		result.addError("invalid_portal_cost", fmt.Sprintf("nav portal %s cost must be non-negative", portal.ID))
	}
}

func validateNavTilePortalTopology(result *NavValidationResult, tile *NavTileDef, tileByKey map[string]*NavTileDef, profiles map[string]NavAgentProfileDef) {
	polygons := navValidationPolygonsByID(tile)
	profile := navPortalProfileForTile(tile, profiles)
	for _, portal := range tile.Portals {
		target := tileByKey[navTopologyTileKey(tile.AgentProfileID, portal.ToTileCoord)]
		if target == nil {
			result.addError("missing_portal_target_tile", fmt.Sprintf("nav portal %s targets missing tile %s/%s", portal.ID, tile.AgentProfileID, TerrainChunkKey(portal.ToTileCoord)))
			continue
		}
		dir, ok := navPortalTileDirection(tile, target)
		if !ok {
			result.addError("invalid_portal_target_tile", fmt.Sprintf("nav portal %s targets non-neighbor tile %s/%s", portal.ID, target.AgentProfileID, TerrainChunkKey(target.Coord)))
			continue
		}
		fromPolygon, ok := polygons[portal.FromPolygonID]
		if !ok {
			continue
		}
		targetPolygon, ok := navValidationPolygonsByID(target)[portal.ToPolygonID]
		if !ok {
			result.addError("missing_portal_to_polygon", fmt.Sprintf("nav portal %s targets missing polygon %s in tile %s", portal.ID, portal.ToPolygonID, TerrainChunkKey(target.Coord)))
			continue
		}
		if !navPortalSegmentTouchesTileBoundary(portal.Start, portal.End, tile, target, dir) {
			result.addError("invalid_portal_boundary_segment", fmt.Sprintf("nav portal %s segment is not on the shared tile boundary", portal.ID))
		}
		if !navPortalSegmentHasAgentClearance(portal.Start, portal.End, profile) {
			result.addError("portal_too_narrow", fmt.Sprintf("nav portal %s segment is narrower than the agent diameter", portal.ID))
		}
		if !navValidationPortalHeightsValid(tile, fromPolygon, target, targetPolygon, portal, dir, profile) {
			result.addError("invalid_portal_height", fmt.Sprintf("nav portal %s segment height is not valid for both polygons/profile", portal.ID))
		}
		if !navValidationHasReciprocalPortal(target, tile, portal) {
			result.addError("missing_reciprocal_portal", fmt.Sprintf("nav portal %s has no reciprocal portal from tile %s", portal.ID, TerrainChunkKey(target.Coord)))
		}
	}
}

func navTopologyTileKey(profileID string, coord TerrainChunkCoordDef) string {
	return profileID + "|" + TerrainChunkKey(coord)
}

func navValidationPolygonsByID(tile *NavTileDef) map[string]NavPolygonDef {
	out := map[string]NavPolygonDef{}
	if tile == nil {
		return out
	}
	for _, polygon := range tile.Polygons {
		if strings.TrimSpace(polygon.ID) == "" {
			continue
		}
		out[polygon.ID] = polygon
	}
	return out
}

func navValidationPortalHeightsValid(tile *NavTileDef, polygon NavPolygonDef, target *NavTileDef, targetPolygon NavPolygonDef, portal NavPortalDef, dir TerrainChunkCoordDef, profile NavAgentProfileDef) bool {
	length := navVec2Length(portal.End[0]-portal.Start[0], portal.End[2]-portal.Start[2])
	step := navPortalEffectiveSampleStep(tile, target, profile)
	samples := 1
	if step > 1e-4 && length > step {
		samples = int(length / step)
		if float32(samples)*step < length {
			samples++
		}
	}
	if samples > 64 {
		samples = 64
	}
	for i := 0; i <= samples; i++ {
		t := float32(i) / float32(samples)
		point := Vec3{
			portal.Start[0] + (portal.End[0]-portal.Start[0])*t,
			portal.Start[1] + (portal.End[1]-portal.Start[1])*t,
			portal.Start[2] + (portal.End[2]-portal.Start[2])*t,
		}
		height, ok := navPortalHeightAt(tile, polygon, target, targetPolygon, point[0], point[2], profile)
		if !ok || !navAlmostEqual(point[1], height, 1e-3) {
			return false
		}
	}
	return true
}

func navValidationHasReciprocalPortal(target *NavTileDef, source *NavTileDef, portal NavPortalDef) bool {
	if target == nil || source == nil {
		return false
	}
	for _, candidate := range target.Portals {
		if candidate.FromPolygonID != portal.ToPolygonID ||
			candidate.ToTileCoord != source.Coord ||
			candidate.ToPolygonID != portal.FromPolygonID {
			continue
		}
		if navValidationSegmentsEquivalent(candidate.Start, candidate.End, portal.Start, portal.End) {
			return true
		}
	}
	return false
}

func navValidationSegmentsEquivalent(aStart, aEnd, bStart, bEnd Vec3) bool {
	const epsilon = float32(1e-3)
	return (navVec3Distance(aStart, bStart) <= epsilon && navVec3Distance(aEnd, bEnd) <= epsilon) ||
		(navVec3Distance(aStart, bEnd) <= epsilon && navVec3Distance(aEnd, bStart) <= epsilon)
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
