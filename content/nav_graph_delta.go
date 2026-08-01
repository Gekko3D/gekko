package content

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
)

const DefaultWorldDeltaNavGraphDirName = "nav_graph"

type NavSourceTileLookupResult struct {
	Found bool
	Empty bool
	Tile  *NavSourceTileDef
}

type NavGraphTileLookupResult struct {
	Found bool
	Empty bool
	Tile  *NavGraphTileDef
}

type NavGraphDeltaBakeResult struct {
	SourceOverrides []NavigationSourceOverrideDef
	GraphOverrides  []NavigationGraphOverrideDef
	SourceTiles     []NavSourceTileDef
	GraphTiles      []NavGraphTileDef
}

func ExpandNavGraphDirtyTileCoords(modified []TerrainChunkCoordDef) []TerrainChunkCoordDef {
	return ExpandNavGraphDirtyTileCoordsForProfiles(modified, nil, 1, 1)
}

// ExpandNavGraphDirtyTileCoordsForProfiles returns every tile whose generated
// support, clearance, or exceptional traversal can depend on a modified tile.
func ExpandNavGraphDirtyTileCoordsForProfiles(modified []TerrainChunkCoordDef, profiles []NavAgentProfileDef, chunkSize int, voxelResolution float32) []TerrainChunkCoordDef {
	tileSize := float32(chunkSize) * voxelResolution
	if tileSize <= 0 || math.IsNaN(float64(tileSize)) || math.IsInf(float64(tileSize), 0) {
		tileSize = 1
	}
	horizontal, vertical := tileSize, tileSize
	for _, profile := range profiles {
		horizontal = maxNavDeltaFloat(horizontal, profile.Radius, profile.MaxJumpDistance)
		vertical = maxNavDeltaFloat(vertical, profile.Height, profile.StepHeight, profile.MaxDropHeight, profile.MaxJumpRise, profile.MaxVaultHeight, profile.MaxMantleHeight)
	}
	horizontalTiles := maxNavDeltaInt(1, int(math.Ceil(float64(horizontal/tileSize))))
	verticalTiles := maxNavDeltaInt(1, int(math.Ceil(float64(vertical/tileSize))))
	seen := make(map[TerrainChunkCoordDef]struct{}, len(modified)*27)
	for _, coord := range modified {
		for y := -verticalTiles; y <= verticalTiles; y++ {
			for x := -horizontalTiles; x <= horizontalTiles; x++ {
				for z := -horizontalTiles; z <= horizontalTiles; z++ {
					seen[TerrainChunkCoordDef{X: coord.X + x, Y: coord.Y + y, Z: coord.Z + z}] = struct{}{}
				}
			}
		}
	}
	return sortedNavGraphCoords(seen)
}

func maxNavDeltaFloat(values ...float32) float32 {
	var result float32
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func maxNavDeltaInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func DefaultWorldDeltaNavSourceTilePath(deltaPath, navID string, coord TerrainChunkCoordDef) string {
	return filepath.Join(DefaultWorldDeltaDataDir(deltaPath), DefaultWorldDeltaNavGraphDirName, navGraphPathToken(navID), "sources", navGraphCoordFilename(coord)+NavSourceTileExtension)
}

func DefaultWorldDeltaNavGraphTilePath(deltaPath, navID, profileID string, coord TerrainChunkCoordDef) string {
	return filepath.Join(DefaultWorldDeltaDataDir(deltaPath), DefaultWorldDeltaNavGraphDirName, navGraphPathToken(navID), "graphs", navGraphPathToken(profileID), navGraphCoordFilename(coord)+NavGraphTileExtension)
}

func worldDeltaNavBatchPath(path, batchID string) string {
	if batchID == "" {
		return path
	}
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + "." + navGraphPathToken(batchID) + ext
}

func LoadEffectiveNavSourceTile(manifest *NavGraphManifestDef, manifestPath string, delta *WorldDeltaDef, deltaPath string, coord TerrainChunkCoordDef) (NavSourceTileLookupResult, error) {
	if manifest == nil {
		return NavSourceTileLookupResult{}, nil
	}
	for _, override := range deltaNavigationSourceOverrides(delta) {
		if override.NavID != manifest.NavID || override.ChunkCoord != coord {
			continue
		}
		if override.Empty {
			return NavSourceTileLookupResult{Found: true, Empty: true, Tile: &NavSourceTileDef{
				NavID: manifest.NavID, SchemaVersion: CurrentNavSourceTileSchemaVersion, Coord: coord,
				BuilderVersion: manifest.BuilderVersion, SourceHash: override.SourceHash, DependencyHash: override.DependencyHash,
				ChunkSize: manifest.ChunkSize, VoxelResolution: manifest.VoxelResolution,
			}}, nil
		}
		tile, err := LoadNavSourceTile(ResolveDocumentPath(override.TilePath, deltaPath))
		return NavSourceTileLookupResult{Found: err == nil, Tile: tile}, err
	}
	for _, entry := range manifest.SourceTiles {
		if entry.Coord != coord {
			continue
		}
		tile, err := LoadNavSourceTile(ResolveDocumentPath(entry.TilePath, manifestPath))
		return NavSourceTileLookupResult{Found: err == nil, Tile: tile}, err
	}
	return NavSourceTileLookupResult{}, nil
}

func LoadEffectiveNavGraphTile(manifest *NavGraphManifestDef, manifestPath string, delta *WorldDeltaDef, deltaPath string, coord TerrainChunkCoordDef, profileID string) (NavGraphTileLookupResult, error) {
	if manifest == nil {
		return NavGraphTileLookupResult{}, nil
	}
	for _, override := range deltaNavigationGraphOverrides(delta) {
		if override.NavID != manifest.NavID || override.AgentProfileID != profileID || override.ChunkCoord != coord {
			continue
		}
		if override.Empty {
			return NavGraphTileLookupResult{Found: true, Empty: true, Tile: &NavGraphTileDef{
				NavID: manifest.NavID, SchemaVersion: CurrentNavGraphTileSchemaVersion, Coord: coord,
				AgentProfileID: profileID, BuilderVersion: manifest.BuilderVersion,
				SourceHash: override.SourceHash, DependencyHash: override.DependencyHash,
			}}, nil
		}
		tile, err := LoadNavGraphTile(ResolveDocumentPath(override.TilePath, deltaPath))
		return NavGraphTileLookupResult{Found: err == nil, Tile: tile}, err
	}
	for _, entry := range manifest.GraphTiles {
		if entry.Coord != coord || entry.AgentProfileID != profileID {
			continue
		}
		tile, err := LoadNavGraphTile(ResolveDocumentPath(entry.TilePath, manifestPath))
		return NavGraphTileLookupResult{Found: err == nil, Tile: tile}, err
	}
	return NavGraphTileLookupResult{}, nil
}

// SaveNavGraphDeltaForImportedWorldChunks rebuilds source tiles whose one-chunk
// occupancy halo contains a dirty chunk, then republishes every neighboring
// graph edge as one override revision. chunks must contain effective occupancy
// for rebuilt centers and their known halo.
func SaveNavGraphDeltaForImportedWorldChunks(deltaPath string, delta *WorldDeltaDef, base *NavGraphManifestDef, basePath string, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, dirty []TerrainChunkCoordDef) (NavGraphDeltaBakeResult, error) {
	return SaveNavGraphDeltaBatchForImportedWorldChunks(deltaPath, delta, base, basePath, chunks, dirty, "")
}

// SaveNavGraphDeltaBatchForImportedWorldChunks writes one generation-qualified
// replacement batch. Its sidecars are immutable once written, so a stale build
// cannot overwrite files referenced by the currently published world delta.
func SaveNavGraphDeltaBatchForImportedWorldChunks(deltaPath string, delta *WorldDeltaDef, base *NavGraphManifestDef, basePath string, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, dirty []TerrainChunkCoordDef, batchID string) (NavGraphDeltaBakeResult, error) {
	if strings.TrimSpace(deltaPath) == "" || delta == nil || base == nil {
		return NavGraphDeltaBakeResult{}, fmt.Errorf("navigation graph delta requires delta path, world delta, and base manifest")
	}
	if len(dirty) == 0 {
		return NavGraphDeltaBakeResult{}, nil
	}
	if validation := ValidateNavGraphManifest(base); validation.HasErrors() {
		return NavGraphDeltaBakeResult{}, fmt.Errorf("invalid base navigation graph manifest: %s", validation.Error())
	}

	newSources := make(map[TerrainChunkCoordDef]NavSourceTileDef)
	sourceCoords := make(map[TerrainChunkCoordDef]struct{})
	for _, coord := range ExpandNavGraphDirtyTileCoords(dirty) {
		center, supplied := chunks[coord]
		lookup, err := LoadEffectiveNavSourceTile(base, basePath, delta, deltaPath, coord)
		if err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		if !supplied && !lookup.Found {
			continue
		}
		if !supplied || center == nil {
			return NavGraphDeltaBakeResult{}, fmt.Errorf("effective navigation chunk %s is missing", TerrainChunkKey(coord))
		}
		buildCenter, err := navGraphBuildChunk(*center, base.ChunkSize)
		if err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		halo := make([]NavSpanBuildChunk, 0, 26)
		for otherCoord, other := range chunks {
			if other == nil || otherCoord == coord || absNavSpanInt(otherCoord.X-coord.X) > 1 || absNavSpanInt(otherCoord.Y-coord.Y) > 1 || absNavSpanInt(otherCoord.Z-coord.Z) > 1 {
				continue
			}
			buildChunk, err := navGraphBuildChunk(*other, base.ChunkSize)
			if err != nil {
				return NavGraphDeltaBakeResult{}, err
			}
			halo = append(halo, buildChunk)
		}
		sort.Slice(halo, func(i, j int) bool { return terrainCoordLess(halo[i].Coord, halo[j].Coord) })
		built, err := BuildNavSourceSpans(NavSpanBuildInput{
			NavID: base.NavID, BuilderVersion: base.BuilderVersion, SourceHash: buildCenter.SourceHash,
			ChunkSize: base.ChunkSize, VoxelResolution: base.VoxelResolution, Center: buildCenter, Halo: halo,
		})
		if err != nil {
			return NavGraphDeltaBakeResult{}, fmt.Errorf("rebuild navigation source tile %s: %w", TerrainChunkKey(coord), err)
		}
		newSources[coord] = built.Source
		sourceCoords[coord] = struct{}{}
	}

	profileCoords := make(map[TerrainChunkCoordDef]struct{})
	for _, coord := range ExpandNavGraphDirtyTileCoordsForProfiles(dirty, base.AgentProfiles, base.ChunkSize, base.VoxelResolution) {
		profileCoords[coord] = struct{}{}
	}
	graphCoords := expandNavGraphCoordsForDoors(
		expandNavGraphCoordsForCarriers(
			expandNavGraphCoordsForLadders(expandNavGraphHorizontal(profileCoords), base.LadderVolumes, base.ChunkSize, base.VoxelResolution),
			base.Carriers, base.ChunkSize, base.VoxelResolution,
		),
		base.Doors, base.ChunkSize, base.VoxelResolution,
	)
	contextGraphCoords := expandNavGraphHorizontal(graphCoords)
	contextSourceCoords := expandNavGraphCube(contextGraphCoords)
	sources := make(map[TerrainChunkCoordDef]NavSourceTileDef, len(contextSourceCoords))
	for coord := range contextSourceCoords {
		if source, ok := newSources[coord]; ok {
			sources[coord] = source
			continue
		}
		lookup, err := LoadEffectiveNavSourceTile(base, basePath, delta, deltaPath, coord)
		if err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		if lookup.Found && lookup.Tile != nil {
			sources[coord] = *lookup.Tile
		}
	}
	for coord := range graphCoords {
		if _, ok := sources[coord]; !ok {
			delete(graphCoords, coord)
		}
	}

	result := NavGraphDeltaBakeResult{}
	for _, coord := range sortedNavGraphCoords(sourceCoords) {
		source := newSources[coord]
		override := NavigationSourceOverrideDef{NavID: base.NavID, ChunkCoord: coord, SourceHash: source.SourceHash, DependencyHash: source.DependencyHash, Empty: len(source.Spans) == 0}
		if !override.Empty {
			result.SourceTiles = append(result.SourceTiles, source)
		}
		result.SourceOverrides = append(result.SourceOverrides, override)
	}

	sourceSlice := make([]NavSourceTileDef, 0, len(sources))
	for _, coord := range sortedNavGraphCoords(contextSourceCoords) {
		if source, ok := sources[coord]; ok {
			sourceSlice = append(sourceSlice, source)
		}
	}
	for _, profile := range base.AgentProfiles {
		graphs := make([]NavGraphTileDef, 0, len(contextGraphCoords))
		for _, coord := range sortedNavGraphCoords(contextGraphCoords) {
			source, ok := sources[coord]
			if !ok {
				continue
			}
			if _, output := graphCoords[coord]; output {
				built, err := BuildNavSpanGraphWithContext(source, sourceSlice, profile, base.VoxelResolution)
				if err != nil {
					return NavGraphDeltaBakeResult{}, err
				}
				graphs = append(graphs, built.Graph)
				continue
			}
			lookup, err := LoadEffectiveNavGraphTile(base, basePath, delta, deltaPath, coord, profile.ID)
			if err != nil {
				return NavGraphDeltaBakeResult{}, err
			}
			if lookup.Found && lookup.Tile != nil {
				graphs = append(graphs, *lookup.Tile)
			}
		}
		_, connected, err := ConnectNavGraphTilesWithContext(sourceSlice, graphs, profile, base.ChunkSize, base.VoxelResolution)
		if err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		connected, _, err = connectNavGraphDoors(sourceSlice, connected, base.Doors, profile, base.ChunkSize, base.VoxelResolution, false)
		if err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		connected, err = ConnectNavGraphDrops(sourceSlice, connected, profile, base.ChunkSize, base.VoxelResolution)
		if err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		connected, err = ConnectNavGraphJumps(sourceSlice, connected, profile, base.ChunkSize, base.VoxelResolution)
		if err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		connected, _, err = connectNavGraphLadders(sourceSlice, connected, base.LadderVolumes, profile, base.ChunkSize, base.VoxelResolution, false)
		if err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		connected, _, err = connectNavGraphCarriers(sourceSlice, connected, base.Carriers, profile, base.ChunkSize, base.VoxelResolution, false)
		if err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		connected, _, err = connectNavGraphDoorGates(sourceSlice, connected, base.Doors, profile, base.ChunkSize, base.VoxelResolution, false)
		if err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		if err := validateNavGraphConnectedBatch(sources, connected); err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		for _, graph := range connected {
			if _, output := graphCoords[graph.Coord]; !output {
				continue
			}
			override := NavigationGraphOverrideDef{
				NavID: base.NavID, AgentProfileID: profile.ID, ChunkCoord: graph.Coord,
				SourceHash: graph.SourceHash, DependencyHash: graph.DependencyHash,
				Empty: len(graph.SpanIDs) == 0,
			}
			if !override.Empty {
				result.GraphTiles = append(result.GraphTiles, graph)
			}
			result.GraphOverrides = append(result.GraphOverrides, override)
		}
	}
	for i := range result.SourceOverrides {
		override := &result.SourceOverrides[i]
		if override.Empty {
			continue
		}
		path := worldDeltaNavBatchPath(DefaultWorldDeltaNavSourceTilePath(deltaPath, base.NavID, override.ChunkCoord), batchID)
		source := newSources[override.ChunkCoord]
		if err := SaveNavSourceTile(path, &source); err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		override.TilePath = AuthorDocumentPath(path, deltaPath)
	}
	type graphKey struct {
		coord   TerrainChunkCoordDef
		profile string
	}
	graphByKey := make(map[graphKey]*NavGraphTileDef, len(result.GraphTiles))
	for i := range result.GraphTiles {
		graph := &result.GraphTiles[i]
		graphByKey[graphKey{graph.Coord, graph.AgentProfileID}] = graph
	}
	for i := range result.GraphOverrides {
		override := &result.GraphOverrides[i]
		if override.Empty {
			continue
		}
		graph := graphByKey[graphKey{override.ChunkCoord, override.AgentProfileID}]
		if graph == nil {
			return NavGraphDeltaBakeResult{}, fmt.Errorf("navigation graph replacement %s for profile %q is missing", TerrainChunkKey(override.ChunkCoord), override.AgentProfileID)
		}
		path := worldDeltaNavBatchPath(DefaultWorldDeltaNavGraphTilePath(deltaPath, base.NavID, override.AgentProfileID, override.ChunkCoord), batchID)
		if err := SaveNavGraphTile(path, graph); err != nil {
			return NavGraphDeltaBakeResult{}, err
		}
		override.TilePath = AuthorDocumentPath(path, deltaPath)
	}

	delta.NavigationSourceOverrides = mergeNavigationSourceOverrides(delta.NavigationSourceOverrides, result.SourceOverrides)
	delta.NavigationGraphOverrides = mergeNavigationGraphOverrides(delta.NavigationGraphOverrides, result.GraphOverrides)
	return result, nil
}

func validateNavGraphConnectedBatch(sources map[TerrainChunkCoordDef]NavSourceTileDef, graphs []NavGraphTileDef) error {
	graphByCoord := make(map[TerrainChunkCoordDef]NavGraphTileDef, len(graphs))
	for _, graph := range graphs {
		if validation := ValidateNavGraphTile(&graph); validation.HasErrors() {
			return fmt.Errorf("invalid navigation graph replacement %s: %s", TerrainChunkKey(graph.Coord), validation.Error())
		}
		source, ok := sources[graph.Coord]
		if !ok || source.NavID != graph.NavID || source.BuilderVersion != graph.BuilderVersion || source.SourceHash != graph.SourceHash || source.DependencyHash != graph.DependencyHash {
			return fmt.Errorf("navigation source and graph replacement metadata do not match at %s", TerrainChunkKey(graph.Coord))
		}
		graphByCoord[graph.Coord] = graph
	}
	for _, graph := range graphs {
		for _, transition := range graph.SpanTransitions {
			if transition.To.Tile == graph.Coord || transition.Kind != NavTransitionWalk && transition.Kind != NavTransitionStep && transition.Kind != NavTransitionStair {
				continue
			}
			target, ok := graphByCoord[transition.To.Tile]
			if !ok {
				return fmt.Errorf("navigation seam %s/%d targets missing tile %s", TerrainChunkKey(graph.Coord), transition.From, TerrainChunkKey(transition.To.Tile))
			}
			reciprocal := false
			for _, reverse := range target.SpanTransitions {
				if reverse.From == transition.To.Span && reverse.To.Tile == graph.Coord && reverse.To.Span == transition.From {
					reciprocal = true
					break
				}
			}
			if !reciprocal {
				return fmt.Errorf("navigation seam %s/%d to %s/%d is not reciprocal", TerrainChunkKey(graph.Coord), transition.From, TerrainChunkKey(transition.To.Tile), transition.To.Span)
			}
		}
	}
	return nil
}

func navGraphBuildChunk(chunk ImportedWorldChunkDef, chunkSize int) (NavSpanBuildChunk, error) {
	solids, err := navGraphSolidVoxels(chunk, chunkSize)
	if err != nil {
		return NavSpanBuildChunk{}, err
	}
	return NavSpanBuildChunk{Coord: chunk.Coord, Known: true, SourceHash: navGraphOccupancyHash(chunk.Coord, solids), SolidVoxels: solids}, nil
}

func deltaNavigationSourceOverrides(delta *WorldDeltaDef) []NavigationSourceOverrideDef {
	if delta == nil {
		return nil
	}
	return delta.NavigationSourceOverrides
}

func deltaNavigationGraphOverrides(delta *WorldDeltaDef) []NavigationGraphOverrideDef {
	if delta == nil {
		return nil
	}
	return delta.NavigationGraphOverrides
}

func mergeNavigationSourceOverrides(current, updates []NavigationSourceOverrideDef) []NavigationSourceOverrideDef {
	for _, update := range updates {
		found := false
		for i := range current {
			if current[i].NavID == update.NavID && current[i].ChunkCoord == update.ChunkCoord {
				current[i], found = update, true
				break
			}
		}
		if !found {
			current = append(current, update)
		}
	}
	sort.Slice(current, func(i, j int) bool {
		if current[i].NavID != current[j].NavID {
			return current[i].NavID < current[j].NavID
		}
		return terrainCoordLess(current[i].ChunkCoord, current[j].ChunkCoord)
	})
	return current
}

func mergeNavigationGraphOverrides(current, updates []NavigationGraphOverrideDef) []NavigationGraphOverrideDef {
	for _, update := range updates {
		found := false
		for i := range current {
			if current[i].NavID == update.NavID && current[i].AgentProfileID == update.AgentProfileID && current[i].ChunkCoord == update.ChunkCoord {
				current[i], found = update, true
				break
			}
		}
		if !found {
			current = append(current, update)
		}
	}
	sort.Slice(current, func(i, j int) bool {
		if current[i].NavID != current[j].NavID {
			return current[i].NavID < current[j].NavID
		}
		if current[i].AgentProfileID != current[j].AgentProfileID {
			return current[i].AgentProfileID < current[j].AgentProfileID
		}
		return terrainCoordLess(current[i].ChunkCoord, current[j].ChunkCoord)
	})
	return current
}

func expandNavGraphHorizontal(input map[TerrainChunkCoordDef]struct{}) map[TerrainChunkCoordDef]struct{} {
	out := make(map[TerrainChunkCoordDef]struct{}, len(input)*5)
	for coord := range input {
		out[coord] = struct{}{}
		for _, offset := range [...]TerrainChunkCoordDef{{X: -1}, {X: 1}, {Z: -1}, {Z: 1}} {
			out[TerrainChunkCoordDef{X: coord.X + offset.X, Y: coord.Y, Z: coord.Z + offset.Z}] = struct{}{}
		}
	}
	return out
}

func expandNavGraphCube(input map[TerrainChunkCoordDef]struct{}) map[TerrainChunkCoordDef]struct{} {
	out := make(map[TerrainChunkCoordDef]struct{}, len(input)*27)
	for coord := range input {
		for y := -1; y <= 1; y++ {
			for x := -1; x <= 1; x++ {
				for z := -1; z <= 1; z++ {
					out[TerrainChunkCoordDef{X: coord.X + x, Y: coord.Y + y, Z: coord.Z + z}] = struct{}{}
				}
			}
		}
	}
	return out
}

func sortedNavGraphCoords(set map[TerrainChunkCoordDef]struct{}) []TerrainChunkCoordDef {
	coords := make([]TerrainChunkCoordDef, 0, len(set))
	for coord := range set {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool { return terrainCoordLess(coords[i], coords[j]) })
	return coords
}

func navGraphPathToken(value string) string {
	if value == "" {
		return "nav"
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, value)
}
