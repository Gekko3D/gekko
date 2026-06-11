package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gekko3d/gekko/content"
)

func main() {
	var navPath string
	var worldPath string
	var deltaPath string
	var coordText string
	var dirtyCoordsText string
	var heightfieldDumpPath string
	var rebuildDeltaNav bool
	var profileID string
	var pathStartText string
	var pathEndText string
	var pathSnapDistance float64
	var pathMaxTileRadius int
	var pathMaxTileLoads int
	var pathLocalOnly bool
	var pathCorridorFallback bool
	var maxIssues int
	var failOnError bool
	flag.StringVar(&navPath, "nav", "", "path to .gknav manifest")
	flag.StringVar(&worldPath, "world", "", "optional imported .gkworld manifest for heightfield dumps")
	flag.StringVar(&deltaPath, "delta", "", "optional .gkworlddelta to use for effective nav queries")
	flag.StringVar(&coordText, "coord", "", "tile/chunk coord for heightfield dumps, formatted as x:y:z")
	flag.StringVar(&dirtyCoordsText, "dirty-coords", "", "optional dirty tile coords for -rebuild-delta-nav, formatted as x:y:z;x:y:z")
	flag.StringVar(&heightfieldDumpPath, "dump-heightfield", "", "write voxel heightfield debug JSON to this path, or '-' for stdout")
	flag.BoolVar(&rebuildDeltaNav, "rebuild-delta-nav", false, "rebuild navigation tile overrides from imported-world chunk overrides in .gkworlddelta")
	flag.StringVar(&profileID, "profile", "", "optional agent profile id to validate")
	flag.StringVar(&pathStartText, "path-start", "", "optional path query start point, formatted as x:y:z")
	flag.StringVar(&pathEndText, "path-end", "", "optional path query end point, formatted as x:y:z")
	flag.Float64Var(&pathSnapDistance, "path-snap-distance", content.DefaultNavPathEndpointSnapDistance, "path endpoint snap distance")
	flag.IntVar(&pathMaxTileRadius, "path-max-tile-radius", content.DefaultNavPathMaxTileSearchRadius, "maximum local path tile search radius")
	flag.IntVar(&pathMaxTileLoads, "path-max-tile-loads", content.DefaultNavPathMaxTileLoads, "maximum local path tile loads")
	flag.BoolVar(&pathLocalOnly, "path-local-only", false, "run only local tiled path query, bypassing sector graph")
	flag.BoolVar(&pathCorridorFallback, "path-corridor-fallback", false, "retry local path without coarse sector corridor if corridor filtering blocks refinement")
	flag.IntVar(&maxIssues, "max-issues", 50, "maximum issue messages to print")
	flag.BoolVar(&failOnError, "fail-on-error", true, "exit with non-zero status when validation issues are found")
	flag.Parse()

	if strings.TrimSpace(navPath) == "" && strings.TrimSpace(heightfieldDumpPath) == "" && !rebuildDeltaNav {
		fatalf("-nav is required unless -dump-heightfield is used")
	}
	if rebuildDeltaNav {
		if strings.TrimSpace(navPath) == "" {
			fatalf("-nav is required with -rebuild-delta-nav")
		}
		if strings.TrimSpace(worldPath) == "" {
			fatalf("-world is required with -rebuild-delta-nav")
		}
		if strings.TrimSpace(deltaPath) == "" {
			fatalf("-delta is required with -rebuild-delta-nav")
		}
	}
	if maxIssues < 0 {
		fatalf("-max-issues must be non-negative")
	}
	pathQueryEnabled := strings.TrimSpace(pathStartText) != "" || strings.TrimSpace(pathEndText) != ""
	if pathQueryEnabled && (strings.TrimSpace(pathStartText) == "" || strings.TrimSpace(pathEndText) == "") {
		fatalf("-path-start and -path-end must be provided together")
	}
	if pathSnapDistance < 0 {
		fatalf("-path-snap-distance must be non-negative")
	}
	if pathMaxTileRadius < 0 {
		fatalf("-path-max-tile-radius must be non-negative")
	}
	if pathMaxTileLoads < 0 {
		fatalf("-path-max-tile-loads must be non-negative")
	}

	var manifest *content.NavManifestDef
	var totalIssues int
	if strings.TrimSpace(navPath) != "" {
		loaded, err := content.LoadNavManifest(navPath)
		if err != nil {
			fatalf("load nav manifest: %v", err)
		}
		manifest = loaded
		if rebuildDeltaNav {
			if err := runNavDeltaRebuild(worldPath, navPath, manifest, deltaPath, dirtyCoordsText, profileID); err != nil {
				fatalf("rebuild delta nav: %v", err)
			}
		}
		totalIssues = runNavValidation(navPath, manifest, profileID, maxIssues)
	}

	if strings.TrimSpace(heightfieldDumpPath) != "" {
		if strings.TrimSpace(worldPath) == "" {
			fatalf("-world is required with -dump-heightfield")
		}
		coord, err := parseNavDiagCoord(coordText)
		if err != nil {
			fatalf("parse -coord: %v", err)
		}
		profile, err := navDiagHeightfieldProfile(profileID, manifest)
		if err != nil {
			fatalf("%v", err)
		}
		debug, err := content.BuildNavVoxelHeightfieldDebugFromImportedWorldManifestPath(worldPath, coord, profile)
		if err != nil {
			fatalf("build heightfield dump: %v", err)
		}
		if err := writeNavDiagHeightfieldDump(heightfieldDumpPath, debug); err != nil {
			fatalf("write heightfield dump: %v", err)
		}
		if heightfieldDumpPath != "-" {
			fmt.Printf("heightfield_dump: %s\n", heightfieldDumpPath)
		}
	}

	if pathQueryEnabled {
		if manifest == nil {
			fatalf("-nav is required with -path-start/-path-end")
		}
		if err := runNavPathQuery(navPath, manifest, deltaPath, profileID, pathStartText, pathEndText, float32(pathSnapDistance), pathMaxTileRadius, pathMaxTileLoads, pathLocalOnly, pathCorridorFallback); err != nil {
			fatalf("path query: %v", err)
		}
	}

	if totalIssues > 0 && failOnError {
		os.Exit(1)
	}
}

func runNavValidation(navPath string, manifest *content.NavManifestDef, profileID string, maxIssues int) int {
	manifestValidation := content.ValidateNavManifest(manifest, content.NavValidationOptions{})
	tiles, loadIssues := loadNavDiagTiles(manifest, navPath, profileID)
	builderIssues := navDiagBuilderVersionIssues(manifest, tiles)
	topologyValidation := content.ValidateNavTileTopology(tiles, navDiagProfilesByID(manifest.AgentProfiles))

	totalIssues := manifestValidation.HardErrorCount + topologyValidation.HardErrorCount + len(loadIssues) + len(builderIssues)
	fmt.Printf("nav: %s\n", navPath)
	fmt.Printf("nav_id: %s\n", manifest.NavID)
	fmt.Printf("builder_version: %s\n", manifest.BuilderVersion)
	fmt.Printf("current_builder_version: %s\n", content.DefaultNavBuilderVersion)
	fmt.Printf("profiles: %s\n", navDiagProfileList(manifest.AgentProfiles))
	if profileID != "" {
		fmt.Printf("profile_filter: %s\n", profileID)
	}
	fmt.Printf("manifest_tiles: %d\n", len(manifest.Tiles))
	fmt.Printf("loaded_tiles: %d\n", len(tiles))
	fmt.Printf("manifest_validation_issues: %d\n", manifestValidation.HardErrorCount)
	fmt.Printf("tile_load_issues: %d\n", len(loadIssues))
	fmt.Printf("builder_version_issues: %d\n", len(builderIssues))
	fmt.Printf("topology_validation_issues: %d\n", topologyValidation.HardErrorCount)
	fmt.Printf("total_issues: %d\n", totalIssues)

	issues := make([]content.NavValidationIssue, 0, manifestValidation.HardErrorCount+topologyValidation.HardErrorCount+len(loadIssues)+len(builderIssues))
	issues = append(issues, manifestValidation.Issues...)
	issues = append(issues, loadIssues...)
	issues = append(issues, builderIssues...)
	issues = append(issues, topologyValidation.Issues...)
	navDiagPrintIssueSummary(issues)
	navDiagPrintIssues(issues, maxIssues)

	return totalIssues
}

func runNavDeltaRebuild(worldPath string, navPath string, manifest *content.NavManifestDef, deltaPath string, dirtyCoordsText string, profileID string) error {
	world, err := content.LoadImportedWorld(worldPath)
	if err != nil {
		return fmt.Errorf("load imported world: %w", err)
	}
	delta, err := content.LoadWorldDelta(deltaPath)
	if err != nil {
		return fmt.Errorf("load delta: %w", err)
	}
	profiles, err := navDiagRebuildProfiles(manifest, profileID)
	if err != nil {
		return err
	}
	dirtyCoords, err := parseNavDiagDirtyCoords(dirtyCoordsText)
	if err != nil {
		return err
	}
	if len(dirtyCoords) == 0 {
		dirtyCoords = navDiagImportedWorldDeltaCoords(delta, world.WorldID)
	}
	expandedCoords := content.ExpandNavDirtyTileCoords(dirtyCoords, world.ChunkSize, world.VoxelResolution, content.NavDirtyTileExpansionOptions{
		AgentProfiles: profiles,
	})
	fmt.Println("delta_nav_rebuild:")
	fmt.Printf("  world: %s\n", worldPath)
	fmt.Printf("  nav: %s\n", navPath)
	fmt.Printf("  delta: %s\n", deltaPath)
	if profileID != "" {
		fmt.Printf("  profile_filter: %s\n", profileID)
	}
	fmt.Printf("  dirty_coords: %d\n", len(dirtyCoords))
	printNavDiagCoordList("  dirty_coord", dirtyCoords)
	fmt.Printf("  expanded_coords: %d\n", len(expandedCoords))
	if len(dirtyCoords) == 0 {
		fmt.Println("  rebuilt_overrides: 0")
		fmt.Println("  written_tiles: 0")
		return nil
	}
	result, err := content.SaveNavDeltaTilesForImportedWorldDelta(worldPath, navPath, deltaPath, content.NavImportedWorldDeltaBakeOptions{
		DirtyCoords:   dirtyCoords,
		AgentProfiles: profiles,
	})
	if err != nil {
		return err
	}
	emptyOverrides := 0
	for _, override := range result.Overrides {
		if override.Empty {
			emptyOverrides++
		}
	}
	fmt.Printf("  rebuilt_overrides: %d\n", len(result.Overrides))
	fmt.Printf("  empty_overrides: %d\n", emptyOverrides)
	fmt.Printf("  written_tiles: %d\n", len(result.Tiles))
	printNavDiagTilePathList("  tile", result.Tiles)
	return nil
}

func loadNavDiagTiles(manifest *content.NavManifestDef, navPath string, profileID string) ([]*content.NavTileDef, []content.NavValidationIssue) {
	if manifest == nil {
		return nil, nil
	}
	tiles := make([]*content.NavTileDef, 0, len(manifest.Tiles))
	issues := make([]content.NavValidationIssue, 0)
	for _, entry := range manifest.Tiles {
		if profileID != "" && entry.AgentProfileID != profileID {
			continue
		}
		path := content.ResolveNavTilePath(entry, navPath)
		tile, err := content.LoadNavTile(path)
		if err != nil {
			issues = append(issues, content.NavValidationIssue{
				Code:    "load_nav_tile_failed",
				Message: fmt.Sprintf("failed to load nav tile %s: %v", entry.TilePath, err),
			})
			continue
		}
		tiles = append(tiles, tile)
	}
	return tiles, issues
}

func navDiagRebuildProfiles(manifest *content.NavManifestDef, profileID string) ([]content.NavAgentProfileDef, error) {
	if manifest == nil {
		return nil, fmt.Errorf("nav manifest is nil")
	}
	if strings.TrimSpace(profileID) == "" {
		profiles := append([]content.NavAgentProfileDef(nil), manifest.AgentProfiles...)
		for i := range profiles {
			content.EnsureNavAgentProfileDefaults(&profiles[i])
		}
		return profiles, nil
	}
	for _, profile := range manifest.AgentProfiles {
		if profile.ID != profileID {
			continue
		}
		content.EnsureNavAgentProfileDefaults(&profile)
		return []content.NavAgentProfileDef{profile}, nil
	}
	return nil, fmt.Errorf("profile %q not found in nav manifest", profileID)
}

func navDiagImportedWorldDeltaCoords(delta *content.WorldDeltaDef, worldID string) []content.TerrainChunkCoordDef {
	if delta == nil {
		return nil
	}
	seen := make(map[content.TerrainChunkCoordDef]struct{}, len(delta.ImportedWorldChunkOverrides))
	for _, override := range delta.ImportedWorldChunkOverrides {
		if worldID != "" && override.WorldID != worldID {
			continue
		}
		seen[override.ChunkCoord] = struct{}{}
	}
	coords := make([]content.TerrainChunkCoordDef, 0, len(seen))
	for coord := range seen {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		return navDiagCoordLess(coords[i], coords[j])
	})
	return coords
}

func navDiagProfilesByID(profiles []content.NavAgentProfileDef) map[string]content.NavAgentProfileDef {
	out := make(map[string]content.NavAgentProfileDef, len(profiles))
	for _, profile := range profiles {
		if strings.TrimSpace(profile.ID) == "" {
			continue
		}
		content.EnsureNavAgentProfileDefaults(&profile)
		out[profile.ID] = profile
	}
	return out
}

func navDiagBuilderVersionIssues(manifest *content.NavManifestDef, tiles []*content.NavTileDef) []content.NavValidationIssue {
	if manifest == nil {
		return nil
	}
	issues := make([]content.NavValidationIssue, 0)
	if manifest.BuilderVersion != content.DefaultNavBuilderVersion {
		issues = append(issues, content.NavValidationIssue{
			Code:    "stale_nav_builder_version",
			Message: fmt.Sprintf("nav manifest was built with %s; current builder is %s", manifest.BuilderVersion, content.DefaultNavBuilderVersion),
		})
	}
	for _, tile := range tiles {
		if tile == nil || tile.BuilderVersion == "" || tile.BuilderVersion == content.DefaultNavBuilderVersion {
			continue
		}
		issues = append(issues, content.NavValidationIssue{
			Code:    "stale_nav_tile_builder_version",
			Message: fmt.Sprintf("nav tile %s/%s was built with %s; current builder is %s", tile.AgentProfileID, content.TerrainChunkKey(tile.Coord), tile.BuilderVersion, content.DefaultNavBuilderVersion),
		})
	}
	return issues
}

func navDiagProfileList(profiles []content.NavAgentProfileDef) string {
	ids := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if strings.TrimSpace(profile.ID) == "" {
			continue
		}
		ids = append(ids, profile.ID)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return "(none)"
	}
	return strings.Join(ids, ",")
}

func navDiagPrintIssueSummary(issues []content.NavValidationIssue) {
	if len(issues) == 0 {
		return
	}
	counts := make(map[string]int)
	codes := make([]string, 0)
	for _, issue := range issues {
		if _, ok := counts[issue.Code]; !ok {
			codes = append(codes, issue.Code)
		}
		counts[issue.Code]++
	}
	sort.Strings(codes)
	fmt.Println("issue_summary:")
	for _, code := range codes {
		fmt.Printf("  %s: %d\n", code, counts[code])
	}
}

func navDiagPrintIssues(issues []content.NavValidationIssue, maxIssues int) {
	if len(issues) == 0 || maxIssues == 0 {
		return
	}
	limit := maxIssues
	if limit > len(issues) {
		limit = len(issues)
	}
	fmt.Println("issues:")
	for i := 0; i < limit; i++ {
		fmt.Printf("  [%s] %s\n", issues[i].Code, issues[i].Message)
	}
	if limit < len(issues) {
		fmt.Printf("  ... %d more issue(s)\n", len(issues)-limit)
	}
}

func runNavPathQuery(navPath string, manifest *content.NavManifestDef, deltaPath string, profileID string, startText string, endText string, snapDistance float32, maxTileRadius int, maxTileLoads int, localOnly bool, corridorFallback bool) error {
	start, err := parseNavDiagVec3(startText)
	if err != nil {
		return fmt.Errorf("parse -path-start: %w", err)
	}
	end, err := parseNavDiagVec3(endText)
	if err != nil {
		return fmt.Errorf("parse -path-end: %w", err)
	}
	var delta *content.WorldDeltaDef
	if strings.TrimSpace(deltaPath) != "" {
		loaded, err := content.LoadWorldDelta(deltaPath)
		if err != nil {
			return fmt.Errorf("load delta: %w", err)
		}
		delta = loaded
	}
	if strings.TrimSpace(profileID) == "" {
		profileID = content.DefaultNavAgentProfileID
	}
	opts := content.NavPathOptions{
		AgentProfileID:       profileID,
		MaxTileSearchRadius:  maxTileRadius,
		MaxTileLoads:         maxTileLoads,
		EndpointSnapDistance: snapDistance,
	}
	fmt.Println("path_query:")
	fmt.Printf("  nav: %s\n", navPath)
	if strings.TrimSpace(deltaPath) != "" {
		fmt.Printf("  delta: %s\n", deltaPath)
	}
	fmt.Printf("  profile: %s\n", profileID)
	fmt.Printf("  start: %s\n", navDiagVec3String(start))
	fmt.Printf("  end: %s\n", navDiagVec3String(end))
	fmt.Printf("  snap_distance: %.3f\n", snapDistance)
	fmt.Printf("  max_tile_radius: %d\n", maxTileRadius)
	fmt.Printf("  max_tile_loads: %d\n", maxTileLoads)
	fmt.Printf("  corridor_fallback: %t\n", corridorFallback)
	if localOnly {
		path, err := content.FindEffectiveNavPath(manifest, navPath, delta, deltaPath, start, end, opts)
		if err != nil {
			return err
		}
		printNavDiagLocalPath(path, "  ")
		return nil
	}
	route, err := content.FindHierarchicalNavRoute(manifest, navPath, delta, deltaPath, start, end, content.NavHierarchicalRouteOptions{
		LocalPath:                  opts,
		AllowLocalCorridorFallback: corridorFallback,
	})
	if err != nil {
		return err
	}
	printNavDiagRoute(route)
	return nil
}

func printNavDiagRoute(route content.NavHierarchicalRouteResult) {
	fmt.Printf("  found: %t\n", route.Found)
	fmt.Printf("  refined: %t\n", route.Refined)
	fmt.Printf("  local_corridor_fallback: %t\n", route.LocalPathCorridorFallback)
	fmt.Printf("  refinement_status: %s\n", navDiagOptionalString(route.RefinementStatus))
	fmt.Printf("  refinement_reason: %s\n", navDiagOptionalString(route.RefinementReason))
	if route.RefinementTile != (content.TerrainChunkCoordDef{}) {
		fmt.Printf("  refinement_tile: %s\n", content.TerrainChunkKey(route.RefinementTile))
	}
	if route.Found {
		fmt.Printf("  start_sector: %s\n", content.TerrainChunkKey(route.StartSector))
		fmt.Printf("  end_sector: %s\n", content.TerrainChunkKey(route.EndSector))
		fmt.Printf("  sector_count: %d\n", len(route.SectorPath.SectorCoords))
		fmt.Printf("  sector_edges: %d\n", len(route.SectorPath.Edges))
		fmt.Printf("  sector_cost: %.3f\n", route.SectorPath.Cost)
		fmt.Printf("  estimated_travel_seconds: %.3f\n", route.SectorPath.EstimatedTravelSeconds)
	}
	printNavDiagLocalPath(route.LocalPath, "  ")
}

func printNavDiagLocalPath(path content.NavPathResult, indent string) {
	fmt.Printf("%slocal_found: %t\n", indent, path.Found)
	if path.FailureReason != "" {
		fmt.Printf("%slocal_failure_reason: %s\n", indent, path.FailureReason)
		fmt.Printf("%slocal_failure_tile: %s\n", indent, content.TerrainChunkKey(path.FailureCoord))
	}
	if path.Found {
		fmt.Printf("%slocal_steps: %d\n", indent, len(path.Steps))
		fmt.Printf("%slocal_waypoints: %d\n", indent, len(path.Waypoints))
		fmt.Printf("%sstart_point: %s\n", indent, navDiagVec3String(path.StartPoint))
		fmt.Printf("%send_point: %s\n", indent, navDiagVec3String(path.EndPoint))
		fmt.Printf("%sstart_snapped: %t\n", indent, path.StartSnapped)
		fmt.Printf("%send_snapped: %t\n", indent, path.EndSnapped)
		fmt.Printf("%sstart_snap_distance: %.3f\n", indent, path.StartSnapDistance)
		fmt.Printf("%send_snap_distance: %.3f\n", indent, path.EndSnapDistance)
		printNavDiagPathSteps(path.Steps, indent)
	}
}

func printNavDiagPathSteps(steps []content.NavPathStep, indent string) {
	if len(steps) == 0 {
		return
	}
	fmt.Printf("%spath_steps:\n", indent)
	for i, step := range steps {
		fmt.Printf("%s  - index=%d tile=%s polygon=%s source=%s\n", indent, i, content.TerrainChunkKey(step.Coord), step.PolygonID, navDiagOptionalString(step.Source))
	}
}

func navDiagHeightfieldProfile(profileID string, manifest *content.NavManifestDef) (content.NavAgentProfileDef, error) {
	if manifest != nil {
		for _, profile := range manifest.AgentProfiles {
			if profileID == "" || profile.ID == profileID {
				content.EnsureNavAgentProfileDefaults(&profile)
				return profile, nil
			}
		}
		if strings.TrimSpace(profileID) != "" {
			return content.NavAgentProfileDef{}, fmt.Errorf("profile %q not found in nav manifest", profileID)
		}
	}
	profile := content.DefaultHL1NavAgentProfile()
	if strings.TrimSpace(profileID) != "" {
		profile.ID = profileID
	}
	content.EnsureNavAgentProfileDefaults(&profile)
	return profile, nil
}

func parseNavDiagVec3(value string) (content.Vec3, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return content.Vec3{}, fmt.Errorf("point is empty")
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ':' || r == ',' || r == ' ' || r == '\t'
	})
	if len(parts) != 3 {
		return content.Vec3{}, fmt.Errorf("expected x:y:z")
	}
	var out content.Vec3
	for i, part := range parts {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(part), 32)
		if err != nil {
			return content.Vec3{}, fmt.Errorf("invalid component %d: %w", i, err)
		}
		out[i] = float32(parsed)
	}
	return out, nil
}

func navDiagVec3String(value content.Vec3) string {
	return fmt.Sprintf("%.3f:%.3f:%.3f", value[0], value[1], value[2])
}

func navDiagOptionalString(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(none)"
	}
	return value
}

func parseNavDiagCoord(value string) (content.TerrainChunkCoordDef, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return content.TerrainChunkCoordDef{}, fmt.Errorf("coord is empty")
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ':' || r == ',' || r == ' ' || r == '\t'
	})
	if len(parts) != 3 {
		return content.TerrainChunkCoordDef{}, fmt.Errorf("expected x:y:z")
	}
	x, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return content.TerrainChunkCoordDef{}, fmt.Errorf("invalid x: %w", err)
	}
	y, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return content.TerrainChunkCoordDef{}, fmt.Errorf("invalid y: %w", err)
	}
	z, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil {
		return content.TerrainChunkCoordDef{}, fmt.Errorf("invalid z: %w", err)
	}
	return content.TerrainChunkCoordDef{X: x, Y: y, Z: z}, nil
}

func parseNavDiagDirtyCoords(value string) ([]content.TerrainChunkCoordDef, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ';' || r == '|' || r == '\n' || r == '\r'
	})
	coords := make([]content.TerrainChunkCoordDef, 0, len(parts))
	seen := make(map[content.TerrainChunkCoordDef]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		coord, err := parseNavDiagCoord(part)
		if err != nil {
			return nil, fmt.Errorf("parse dirty coord %q: %w", part, err)
		}
		if _, ok := seen[coord]; ok {
			continue
		}
		seen[coord] = struct{}{}
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		return navDiagCoordLess(coords[i], coords[j])
	})
	return coords, nil
}

func navDiagCoordLess(a content.TerrainChunkCoordDef, b content.TerrainChunkCoordDef) bool {
	if a.X != b.X {
		return a.X < b.X
	}
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.Z < b.Z
}

func printNavDiagCoordList(label string, coords []content.TerrainChunkCoordDef) {
	for _, coord := range coords {
		fmt.Printf("%s: %s\n", label, content.TerrainChunkKey(coord))
	}
}

func printNavDiagTilePathList(label string, tiles map[string]*content.NavTileDef) {
	if len(tiles) == 0 {
		return
	}
	paths := make([]string, 0, len(tiles))
	for path := range tiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		tile := tiles[path]
		if tile == nil {
			fmt.Printf("%s: %s\n", label, path)
			continue
		}
		fmt.Printf("%s: %s coord=%s profile=%s polys=%d portals=%d\n", label, path, content.TerrainChunkKey(tile.Coord), tile.AgentProfileID, len(tile.Polygons), len(tile.Portals))
	}
}

func writeNavDiagHeightfieldDump(path string, debug *content.NavVoxelHeightfieldDebugDef) error {
	data, err := json.MarshalIndent(debug, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0644)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "navdiag: "+format+"\n", args...)
	os.Exit(1)
}
