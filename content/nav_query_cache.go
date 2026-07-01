package content

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type NavRuntimeQueryCacheKey struct {
	BaseNavManifestPath string
	WorldDeltaPath      string
	NavigationRevision  int64
}

type NavRuntimeQueryCache struct {
	mu sync.Mutex

	generation           uint64
	key                  NavRuntimeQueryCacheKey
	sectorGraphs         map[string]NavSectorGraph
	clearanceTileRegions map[string]navRuntimeClearanceTileRegionCacheEntry
	clearanceGraphs      map[string]navRuntimeClearanceRegionGraphCacheEntry
}

type NavRuntimeQueryCachePrewarmOptions struct {
	NavigationRevision   int64
	AgentProfileIDs      []string
	MaxTileSearchRadius  int
	MaxTileLoads         int
	EndpointSnapDistance float32
}

type navRuntimeClearanceTileRegionCacheEntry struct {
	Coord  TerrainChunkCoordDef
	Tile   *NavClearanceSourceTileDef
	Result NavClearanceLocalRegionBuildResult
}

type navRuntimeClearanceRegionGraphCacheEntry struct {
	Results      map[TerrainChunkCoordDef]*NavClearanceLocalRegionBuildResult
	Tiles        map[TerrainChunkCoordDef]*NavClearanceSourceTileDef
	Graph        NavClearanceCoarseGraph
	RegionByCell map[navClearanceRegionCellRef]NavClearanceRegionPathStep
}

func NewNavRuntimeQueryCache() *NavRuntimeQueryCache {
	cache := &NavRuntimeQueryCache{}
	cache.resetLocked(NavRuntimeQueryCacheKey{})
	return cache
}

func (cache *NavRuntimeQueryCache) Prepare(key NavRuntimeQueryCacheKey) {
	if cache == nil {
		return
	}
	key.BaseNavManifestPath = strings.TrimSpace(key.BaseNavManifestPath)
	key.WorldDeltaPath = strings.TrimSpace(key.WorldDeltaPath)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.key == key && cache.sectorGraphs != nil && cache.clearanceTileRegions != nil && cache.clearanceGraphs != nil {
		return
	}
	cache.resetLocked(key)
}

func (cache *NavRuntimeQueryCache) resetLocked(key NavRuntimeQueryCacheKey) {
	cache.generation++
	cache.key = key
	cache.sectorGraphs = map[string]NavSectorGraph{}
	cache.clearanceTileRegions = map[string]navRuntimeClearanceTileRegionCacheEntry{}
	cache.clearanceGraphs = map[string]navRuntimeClearanceRegionGraphCacheEntry{}
}

func (cache *NavRuntimeQueryCache) SectorGraph(manifest *NavManifestDef, opts NavSectorGraphOptions) (NavSectorGraph, error) {
	return cache.sectorGraphForGeneration(manifest, opts, 0)
}

func (cache *NavRuntimeQueryCache) sectorGraphForGeneration(manifest *NavManifestDef, opts NavSectorGraphOptions, expectedGeneration uint64) (NavSectorGraph, error) {
	if cache == nil {
		return BuildNavSectorGraph(manifest, opts)
	}
	key := fmt.Sprintf("bounds=%t", opts.DisableBoundsAdjacencyInference)
	cache.mu.Lock()
	if expectedGeneration != 0 && cache.generation != expectedGeneration {
		cache.mu.Unlock()
		return BuildNavSectorGraph(manifest, opts)
	}
	if graph, ok := cache.sectorGraphs[key]; ok {
		cache.mu.Unlock()
		return graph, nil
	}
	generation := cache.generation
	cache.mu.Unlock()

	graph, err := BuildNavSectorGraph(manifest, opts)
	if err != nil {
		return NavSectorGraph{}, err
	}
	cache.mu.Lock()
	if cache.generation != generation {
		cache.mu.Unlock()
		return graph, nil
	}
	if cached, ok := cache.sectorGraphs[key]; ok {
		cache.mu.Unlock()
		return cached, nil
	}
	cache.sectorGraphs[key] = graph
	cache.mu.Unlock()
	return graph, nil
}

func (cache *NavRuntimeQueryCache) ClearanceRegionGraph(baseNav *NavManifestDef, baseNavPath string, delta *WorldDeltaDef, deltaPath string, coords []TerrainChunkCoordDef, profile NavAgentProfileDef) (navRuntimeClearanceRegionGraphCacheEntry, error) {
	if cache == nil {
		return buildNavClearanceRegionGraphForCoordsUncached(baseNav, baseNavPath, delta, deltaPath, coords, profile)
	}
	key := navRuntimeClearanceRegionGraphKey(coords, profile)
	cache.mu.Lock()
	if cached, ok := cache.clearanceGraphs[key]; ok {
		cache.mu.Unlock()
		return cached, nil
	}
	generation := cache.generation
	cache.mu.Unlock()

	results := make(map[TerrainChunkCoordDef]*NavClearanceLocalRegionBuildResult, len(coords))
	tiles := make(map[TerrainChunkCoordDef]*NavClearanceSourceTileDef, len(coords))
	for _, coord := range coords {
		entry, err := cache.clearanceTileRegionForGeneration(baseNav, baseNavPath, delta, deltaPath, coord, profile, generation)
		if err != nil {
			return navRuntimeClearanceRegionGraphCacheEntry{}, err
		}
		if entry.Tile == nil {
			continue
		}
		result := cloneNavClearanceLocalRegionBuildResult(entry.Result)
		results[coord] = &result
		tiles[coord] = entry.Tile
	}
	if err := BuildNavClearanceCrossTileRegionPortals(results, tiles, NavClearanceRegionPortalBuildOptions{
		AgentProfile: profile,
		ChunkSize:    baseNav.ChunkSize,
	}); err != nil {
		return navRuntimeClearanceRegionGraphCacheEntry{}, err
	}
	graph, err := BuildNavClearanceCoarseGraph(results)
	if err != nil {
		return navRuntimeClearanceRegionGraphCacheEntry{}, err
	}
	entry := navRuntimeClearanceRegionGraphCacheEntry{
		Results:      results,
		Tiles:        tiles,
		Graph:        graph,
		RegionByCell: buildNavClearanceRegionCellIndex(results),
	}
	cache.mu.Lock()
	if cache.generation != generation {
		cache.mu.Unlock()
		return entry, nil
	}
	if cached, ok := cache.clearanceGraphs[key]; ok {
		cache.mu.Unlock()
		return cached, nil
	}
	cache.clearanceGraphs[key] = entry
	cache.mu.Unlock()
	return entry, nil
}

func (cache *NavRuntimeQueryCache) clearanceTileRegion(baseNav *NavManifestDef, baseNavPath string, delta *WorldDeltaDef, deltaPath string, coord TerrainChunkCoordDef, profile NavAgentProfileDef) (navRuntimeClearanceTileRegionCacheEntry, error) {
	return cache.clearanceTileRegionForGeneration(baseNav, baseNavPath, delta, deltaPath, coord, profile, 0)
}

func (cache *NavRuntimeQueryCache) clearanceTileRegionForGeneration(baseNav *NavManifestDef, baseNavPath string, delta *WorldDeltaDef, deltaPath string, coord TerrainChunkCoordDef, profile NavAgentProfileDef, expectedGeneration uint64) (navRuntimeClearanceTileRegionCacheEntry, error) {
	key := navRuntimeClearanceTileRegionKey(coord, profile)
	cache.mu.Lock()
	if expectedGeneration != 0 && cache.generation != expectedGeneration {
		cache.mu.Unlock()
		return buildNavRuntimeClearanceTileRegionEntry(baseNav, baseNavPath, delta, deltaPath, coord, profile)
	}
	if cached, ok := cache.clearanceTileRegions[key]; ok {
		cache.mu.Unlock()
		return cached, nil
	}
	generation := cache.generation
	cache.mu.Unlock()

	lookup, err := LoadEffectiveNavClearanceSourceTile(baseNav, baseNavPath, delta, deltaPath, coord)
	if err != nil {
		return navRuntimeClearanceTileRegionCacheEntry{}, err
	}
	entry := navRuntimeClearanceTileRegionCacheEntry{Coord: coord}
	if lookup.Found && !lookup.Empty && lookup.Tile != nil {
		result, err := BuildNavClearanceLocalRegions(lookup.Tile, NavClearanceRegionBuildOptions{AgentProfile: profile})
		if err != nil {
			return navRuntimeClearanceTileRegionCacheEntry{}, err
		}
		entry.Tile = lookup.Tile
		entry.Result = result
	}
	cache.mu.Lock()
	if cache.generation != generation {
		cache.mu.Unlock()
		return entry, nil
	}
	if cached, ok := cache.clearanceTileRegions[key]; ok {
		cache.mu.Unlock()
		return cached, nil
	}
	cache.clearanceTileRegions[key] = entry
	cache.mu.Unlock()
	return entry, nil
}

func buildNavRuntimeClearanceTileRegionEntry(baseNav *NavManifestDef, baseNavPath string, delta *WorldDeltaDef, deltaPath string, coord TerrainChunkCoordDef, profile NavAgentProfileDef) (navRuntimeClearanceTileRegionCacheEntry, error) {
	lookup, err := LoadEffectiveNavClearanceSourceTile(baseNav, baseNavPath, delta, deltaPath, coord)
	if err != nil {
		return navRuntimeClearanceTileRegionCacheEntry{}, err
	}
	entry := navRuntimeClearanceTileRegionCacheEntry{Coord: coord}
	if lookup.Found && !lookup.Empty && lookup.Tile != nil {
		result, err := BuildNavClearanceLocalRegions(lookup.Tile, NavClearanceRegionBuildOptions{AgentProfile: profile})
		if err != nil {
			return navRuntimeClearanceTileRegionCacheEntry{}, err
		}
		entry.Tile = lookup.Tile
		entry.Result = result
	}
	return entry, nil
}

func PrewarmNavRuntimeQueryCache(baseNav *NavManifestDef, baseNavPath string, delta *WorldDeltaDef, deltaPath string, cache *NavRuntimeQueryCache, opts NavRuntimeQueryCachePrewarmOptions) error {
	if baseNav == nil || cache == nil {
		return nil
	}
	EnsureNavManifestDefaults(baseNav)
	cache.Prepare(NavRuntimeQueryCacheKey{
		BaseNavManifestPath: baseNavPath,
		WorldDeltaPath:      deltaPath,
		NavigationRevision:  opts.NavigationRevision,
	})
	cache.mu.Lock()
	generation := cache.generation
	cache.mu.Unlock()
	if _, err := cache.sectorGraphForGeneration(baseNav, NavSectorGraphOptions{}, generation); err != nil {
		return err
	}
	if !navPathShouldUseClearanceSource(baseNav, delta) {
		return nil
	}
	profiles := navRuntimeQueryCachePrewarmProfiles(baseNav, opts.AgentProfileIDs)
	for _, profile := range profiles {
		normalized, err := normalizeNavClearancePathOptions(baseNav, NavClearancePathOptions{
			AgentProfileID:       profile.ID,
			AgentProfile:         profile,
			MaxTileSearchRadius:  opts.MaxTileSearchRadius,
			MaxTileLoads:         opts.MaxTileLoads,
			EndpointSnapDistance: opts.EndpointSnapDistance,
		})
		if err != nil {
			return err
		}
		ctx := newEffectiveNavClearancePathContext(baseNav, baseNavPath, delta, deltaPath, normalized)
		for _, coord := range navRuntimeQueryCachePrewarmClearanceCoords(ctx) {
			if _, err := cache.clearanceTileRegionForGeneration(baseNav, baseNavPath, delta, deltaPath, coord, normalized.AgentProfile, generation); err != nil {
				return err
			}
		}
	}
	return nil
}

func navRuntimeQueryCachePrewarmProfiles(manifest *NavManifestDef, profileIDs []string) []NavAgentProfileDef {
	if manifest == nil {
		return nil
	}
	profilesByID := map[string]NavAgentProfileDef{}
	for _, profile := range manifest.AgentProfiles {
		EnsureNavAgentProfileDefaults(&profile)
		profilesByID[profile.ID] = profile
	}
	out := make([]NavAgentProfileDef, 0, len(profilesByID))
	if len(profileIDs) > 0 {
		seen := map[string]struct{}{}
		for _, id := range profileIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				id = DefaultNavAgentProfileID
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			if profile, ok := profilesByID[id]; ok {
				out = append(out, profile)
			}
		}
		return out
	}
	for _, profile := range manifest.AgentProfiles {
		EnsureNavAgentProfileDefaults(&profile)
		out = append(out, profile)
	}
	if len(out) == 0 {
		profile := NavAgentProfileDef{ID: DefaultNavAgentProfileID}
		EnsureNavAgentProfileDefaults(&profile)
		out = append(out, profile)
	}
	return out
}

func navRuntimeQueryCachePrewarmClearanceCoords(ctx *navClearancePathContext) []TerrainChunkCoordDef {
	if ctx == nil {
		return nil
	}
	seen := map[TerrainChunkCoordDef]struct{}{}
	for _, entry := range ctx.sourceByKey {
		if _, stale := ctx.editedWithoutSourceByKey[TerrainChunkKey(entry.Coord)]; stale {
			continue
		}
		seen[entry.Coord] = struct{}{}
	}
	for _, override := range ctx.deltaSourceByKey {
		seen[override.ChunkCoord] = struct{}{}
	}
	return sortedNavRegionPathCoords(seen)
}

func buildNavClearanceRegionGraphForCoordsUncached(baseNav *NavManifestDef, baseNavPath string, delta *WorldDeltaDef, deltaPath string, coords []TerrainChunkCoordDef, profile NavAgentProfileDef) (navRuntimeClearanceRegionGraphCacheEntry, error) {
	results, tiles, err := buildNavClearanceRegionResultsForCoords(baseNav, baseNavPath, delta, deltaPath, coords, profile, nil)
	if err != nil {
		return navRuntimeClearanceRegionGraphCacheEntry{}, err
	}
	if err := BuildNavClearanceCrossTileRegionPortals(results, tiles, NavClearanceRegionPortalBuildOptions{
		AgentProfile: profile,
		ChunkSize:    baseNav.ChunkSize,
	}); err != nil {
		return navRuntimeClearanceRegionGraphCacheEntry{}, err
	}
	graph, err := BuildNavClearanceCoarseGraph(results)
	if err != nil {
		return navRuntimeClearanceRegionGraphCacheEntry{}, err
	}
	return navRuntimeClearanceRegionGraphCacheEntry{
		Results:      results,
		Tiles:        tiles,
		Graph:        graph,
		RegionByCell: buildNavClearanceRegionCellIndex(results),
	}, nil
}

func navRuntimeClearanceRegionGraphKey(coords []TerrainChunkCoordDef, profile NavAgentProfileDef) string {
	ordered := append([]TerrainChunkCoordDef(nil), coords...)
	sort.Slice(ordered, func(i, j int) bool {
		return terrainChunkCoordLess(ordered[i], ordered[j])
	})
	parts := make([]string, 0, len(ordered)+1)
	parts = append(parts, navRuntimeAgentProfileKey(profile))
	for _, coord := range ordered {
		parts = append(parts, TerrainChunkKey(coord))
	}
	return strings.Join(parts, "|")
}

func navRuntimeClearanceTileRegionKey(coord TerrainChunkCoordDef, profile NavAgentProfileDef) string {
	return navRuntimeAgentProfileKey(profile) + "|" + TerrainChunkKey(coord)
}

func navRuntimeAgentProfileKey(profile NavAgentProfileDef) string {
	EnsureNavAgentProfileDefaults(&profile)
	return fmt.Sprintf("%s:r%.4f:h%.4f:step%.4f:slope%.4f", profile.ID, profile.Radius, profile.Height, profile.StepHeight, profile.MaxSlopeDegrees)
}

func cloneNavClearanceLocalRegionBuildResult(in NavClearanceLocalRegionBuildResult) NavClearanceLocalRegionBuildResult {
	out := in
	if len(in.Regions) > 0 {
		out.Regions = make([]NavClearanceLocalRegionDef, len(in.Regions))
		for i, region := range in.Regions {
			out.Regions[i] = region
			out.Regions[i].Cells = append([]NavClearancePathStep(nil), region.Cells...)
			out.Regions[i].Portals = append([]NavClearanceRegionPortalDef(nil), region.Portals...)
		}
	}
	return out
}

func buildNavClearanceRegionCellIndex(results map[TerrainChunkCoordDef]*NavClearanceLocalRegionBuildResult) map[navClearanceRegionCellRef]NavClearanceRegionPathStep {
	out := make(map[navClearanceRegionCellRef]NavClearanceRegionPathStep)
	for coord, result := range results {
		if result == nil {
			continue
		}
		for _, region := range result.Regions {
			step := NavClearanceRegionPathStep{
				Coord:      coord,
				RegionID:   region.ID,
				Area:       region.Area,
				BoundsMin:  region.BoundsMin,
				BoundsMax:  region.BoundsMax,
				Centroid:   region.Centroid,
				Projection: region.Projection,
			}
			for _, cell := range region.Cells {
				out[navClearanceRegionCellRef{Coord: coord, X: cell.X, Y: cell.Y, Z: cell.Z}] = step
			}
		}
	}
	return out
}
