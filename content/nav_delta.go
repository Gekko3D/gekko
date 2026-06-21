package content

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	DefaultWorldDeltaNavDirName           = "nav"
	NavSourceOverrideKindImportedWorld    = "imported_world"
	DefaultNavDeltaTileSourceOverrideKind = NavSourceOverrideKindImportedWorld
)

type NavDeltaBakeOptions struct {
	BuilderVersion     string
	AgentProfiles      []NavAgentProfileDef
	SourceOverrideKind string
	BaseNavPath        string
}

type NavDeltaBakeResult struct {
	Overrides []NavigationTileOverrideDef
	Tiles     map[string]*NavTileDef
}

type NavImportedWorldDeltaBakeOptions struct {
	DirtyCoords        []TerrainChunkCoordDef
	BuilderVersion     string
	AgentProfiles      []NavAgentProfileDef
	SourceOverrideKind string
}

type NavDirtyTileExpansionOptions struct {
	AgentProfiles []NavAgentProfileDef
}

func ExpandNavDirtyTileCoords(modified []TerrainChunkCoordDef, chunkSize int, voxelResolution float32, opts NavDirtyTileExpansionOptions) []TerrainChunkCoordDef {
	if len(modified) == 0 {
		return nil
	}
	reach := navBuildChunkReachForProfiles(chunkSize, voxelResolution, opts.AgentProfiles)
	seen := make(map[TerrainChunkCoordDef]struct{}, len(modified))
	for _, coord := range modified {
		for dx := -reach.Horizontal; dx <= reach.Horizontal; dx++ {
			for dy := -reach.Vertical; dy <= reach.Vertical; dy++ {
				for dz := -reach.Horizontal; dz <= reach.Horizontal; dz++ {
					seen[TerrainChunkCoordDef{X: coord.X + dx, Y: coord.Y + dy, Z: coord.Z + dz}] = struct{}{}
				}
			}
		}
	}
	out := make([]TerrainChunkCoordDef, 0, len(seen))
	for coord := range seen {
		out = append(out, coord)
	}
	sort.Slice(out, func(i, j int) bool {
		return terrainChunkCoordLess(out[i], out[j])
	})
	return out
}

func DefaultWorldDeltaNavTileDir(deltaPath string, navID string, agentProfileID string) string {
	if strings.TrimSpace(navID) == "" {
		navID = "nav"
	}
	navID = sanitizeNavPathToken(navID)
	if strings.TrimSpace(agentProfileID) == "" {
		agentProfileID = DefaultNavAgentProfileID
	}
	agentProfileID = sanitizeNavPathToken(agentProfileID)
	return filepath.Join(DefaultWorldDeltaDataDir(deltaPath), DefaultWorldDeltaNavDirName, navID, agentProfileID)
}

func DefaultWorldDeltaNavTilePath(deltaPath string, navID string, agentProfileID string, coord TerrainChunkCoordDef) string {
	if strings.TrimSpace(agentProfileID) == "" {
		agentProfileID = DefaultNavAgentProfileID
	}
	agentProfileID = sanitizeNavPathToken(agentProfileID)
	return filepath.Join(DefaultWorldDeltaNavTileDir(deltaPath, navID, agentProfileID), fmt.Sprintf("%s_%d_%d_%d.gknavtile", agentProfileID, coord.X, coord.Y, coord.Z))
}

func SaveNavDeltaTilesForImportedWorldDelta(importedWorldManifestPath string, baseNavPath string, deltaPath string, opts NavImportedWorldDeltaBakeOptions) (NavDeltaBakeResult, error) {
	importedWorldManifestPath = strings.TrimSpace(importedWorldManifestPath)
	if importedWorldManifestPath == "" {
		return NavDeltaBakeResult{}, fmt.Errorf("imported world manifest path is empty")
	}
	baseNavPath = strings.TrimSpace(baseNavPath)
	if baseNavPath == "" {
		return NavDeltaBakeResult{}, fmt.Errorf("base nav manifest path is empty")
	}
	deltaPath = strings.TrimSpace(deltaPath)
	if deltaPath == "" {
		return NavDeltaBakeResult{}, fmt.Errorf("world delta path is empty")
	}
	world, err := LoadImportedWorld(importedWorldManifestPath)
	if err != nil {
		return NavDeltaBakeResult{}, err
	}
	baseNav, err := LoadNavManifest(baseNavPath)
	if err != nil {
		return NavDeltaBakeResult{}, err
	}
	EnsureNavManifestDefaults(baseNav)
	if baseNav.SourceWorldID != "" && world.WorldID != "" && baseNav.SourceWorldID != world.WorldID {
		return NavDeltaBakeResult{}, fmt.Errorf("base nav source world %q does not match imported world %q", baseNav.SourceWorldID, world.WorldID)
	}
	if baseNav.ChunkSize > 0 && world.ChunkSize > 0 && baseNav.ChunkSize != world.ChunkSize {
		return NavDeltaBakeResult{}, fmt.Errorf("base nav chunk size %d does not match imported world chunk size %d", baseNav.ChunkSize, world.ChunkSize)
	}
	if baseNav.VoxelResolution > 0 && world.VoxelResolution > 0 && navDeltaAbsf(baseNav.VoxelResolution-world.VoxelResolution) > 1e-4 {
		return NavDeltaBakeResult{}, fmt.Errorf("base nav voxel resolution %.4f does not match imported world voxel resolution %.4f", baseNav.VoxelResolution, world.VoxelResolution)
	}
	delta, err := LoadWorldDelta(deltaPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return NavDeltaBakeResult{}, err
		}
		delta = &WorldDeltaDef{LevelID: baseNav.LevelID}
	}
	if delta.LevelID == "" {
		delta.LevelID = baseNav.LevelID
	}
	bakeOpts := NavDeltaBakeOptions{
		BuilderVersion:     opts.BuilderVersion,
		AgentProfiles:      opts.AgentProfiles,
		SourceOverrideKind: opts.SourceOverrideKind,
		BaseNavPath:        baseNavPath,
	}
	bakeOpts = normalizeNavDeltaBakeOptions(baseNav, bakeOpts)
	dirtyCoords := append([]TerrainChunkCoordDef(nil), opts.DirtyCoords...)
	if len(dirtyCoords) == 0 {
		dirtyCoords = importedWorldDeltaOverrideCoords(delta, world.WorldID)
	}
	if len(dirtyCoords) == 0 {
		return NavDeltaBakeResult{Tiles: map[string]*NavTileDef{}}, nil
	}
	expandedCoords := ExpandNavDirtyTileCoords(dirtyCoords, world.ChunkSize, world.VoxelResolution, NavDirtyTileExpansionOptions{
		AgentProfiles: bakeOpts.AgentProfiles,
	})
	chunks, err := loadEffectiveImportedWorldDeltaChunks(importedWorldManifestPath, world, deltaPath, delta, expandedCoords)
	if err != nil {
		return NavDeltaBakeResult{}, err
	}
	if len(chunks) == 0 {
		return NavDeltaBakeResult{Tiles: map[string]*NavTileDef{}}, nil
	}
	result, err := SaveNavDeltaTilesForImportedWorldChunks(deltaPath, delta, baseNav, chunks, bakeOpts)
	if err != nil {
		return NavDeltaBakeResult{}, err
	}
	if err := SaveWorldDelta(deltaPath, delta); err != nil {
		return NavDeltaBakeResult{}, err
	}
	return result, nil
}

func SaveNavDeltaTilesForImportedWorldChunks(deltaPath string, delta *WorldDeltaDef, baseNav *NavManifestDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, opts NavDeltaBakeOptions) (NavDeltaBakeResult, error) {
	if strings.TrimSpace(deltaPath) == "" {
		return NavDeltaBakeResult{}, fmt.Errorf("world delta path is empty")
	}
	if delta == nil {
		return NavDeltaBakeResult{}, fmt.Errorf("world delta is nil")
	}
	if baseNav == nil {
		return NavDeltaBakeResult{}, fmt.Errorf("base nav manifest is nil")
	}
	EnsureNavManifestDefaults(baseNav)
	if baseNav.NavID == "" {
		return NavDeltaBakeResult{}, fmt.Errorf("base nav manifest nav_id is required")
	}
	opts = normalizeNavDeltaBakeOptions(baseNav, opts)
	coords := make([]TerrainChunkCoordDef, 0, len(chunks))
	for coord := range chunks {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		return terrainChunkCoordLess(coords[i], coords[j])
	})

	result := NavDeltaBakeResult{
		Tiles: make(map[string]*NavTileDef),
	}
	buildCache := &NavTileBuildCache{}
	buildSources := make(map[string]*NavBuildSourceDef, len(opts.AgentProfiles))
	for _, profile := range opts.AgentProfiles {
		EnsureNavAgentProfileDefaults(&profile)
		source, err := buildNavDeltaSourceForImportedWorldChunks(baseNav, chunks, profile)
		if err != nil {
			return NavDeltaBakeResult{}, err
		}
		buildSources[profile.ID] = source
	}
	overrides := append([]NavigationTileOverrideDef(nil), delta.NavigationTileOverrides...)
	for _, coord := range coords {
		chunk := chunks[coord]
		if chunk == nil {
			return NavDeltaBakeResult{}, fmt.Errorf("missing imported world chunk %s", TerrainChunkKey(coord))
		}
		sourceHash := importedWorldChunkNavSourceHash(chunk)
		for _, profile := range opts.AgentProfiles {
			EnsureNavAgentProfileDefaults(&profile)
			buildSource := buildSources[profile.ID]
			combinedSourceHash := navCombinedSourceHash(sourceHash, navBuildSourceHash(buildSource))
			buildHash := navBuildHash(opts.BuilderVersion, profile, combinedSourceHash)
			tileResult, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
				NavID:           baseNav.NavID,
				BuilderVersion:  opts.BuilderVersion,
				SourceDeltaHash: sourceHash,
				NavBuildHash:    buildHash,
				NeighborChunks:  chunks,
				BuildCache:      buildCache,
				BuildSource:     buildSource,
			})
			if err != nil {
				return NavDeltaBakeResult{}, err
			}
			override := NavigationTileOverrideDef{
				NavID:              baseNav.NavID,
				AgentProfileID:     profile.ID,
				ChunkCoord:         coord,
				SourceDeltaHash:    sourceHash,
				NavBuildHash:       buildHash,
				SourceOverrideKind: opts.SourceOverrideKind,
				Tags:               []string{"generated", "runtime_delta"},
			}
			tile := tileResult.Tile
			if tile == nil || len(tile.Polygons) == 0 {
				override.Empty = true
			} else {
				tile.SourceDeltaHash = sourceHash
				tile.NavBuildHash = buildHash
				tilePath := DefaultWorldDeltaNavTilePath(deltaPath, baseNav.NavID, profile.ID, coord)
				override.TilePath = authorPathRelativeToDocument(tilePath, deltaPath)
				result.Tiles[tilePath] = tile
			}
			overrides = upsertNavigationTileOverride(overrides, override)
			result.Overrides = append(result.Overrides, override)
		}
	}
	sort.Slice(overrides, func(i, j int) bool {
		if overrides[i].NavID != overrides[j].NavID {
			return overrides[i].NavID < overrides[j].NavID
		}
		if overrides[i].AgentProfileID != overrides[j].AgentProfileID {
			return overrides[i].AgentProfileID < overrides[j].AgentProfileID
		}
		return terrainChunkCoordLess(overrides[i].ChunkCoord, overrides[j].ChunkCoord)
	})
	effectiveDelta := *delta
	effectiveDelta.NavigationTileOverrides = overrides
	if err := applyNavDeltaTilePortals(result.Tiles, baseNav, opts.BaseNavPath, &effectiveDelta, deltaPath, opts.AgentProfiles); err != nil {
		return NavDeltaBakeResult{}, err
	}
	for tilePath, tile := range result.Tiles {
		if err := SaveNavTile(tilePath, tile); err != nil {
			return NavDeltaBakeResult{}, err
		}
	}
	delta.NavigationTileOverrides = overrides
	return result, nil
}

func buildNavDeltaSourceForImportedWorldChunks(baseNav *NavManifestDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, profile NavAgentProfileDef) (*NavBuildSourceDef, error) {
	if baseNav == nil || len(chunks) == 0 {
		return nil, nil
	}
	worldID := strings.TrimSpace(baseNav.SourceWorldID)
	if worldID == "" {
		for _, chunk := range chunks {
			if chunk != nil && strings.TrimSpace(chunk.WorldID) != "" {
				worldID = chunk.WorldID
				break
			}
		}
	}
	if worldID == "" {
		worldID = "delta_world"
	}
	world := &ImportedWorldDef{
		WorldID:         worldID,
		SchemaVersion:   CurrentImportedWorldSchemaVersion,
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       baseNav.ChunkSize,
		VoxelResolution: baseNav.VoxelResolution,
	}
	for coord, chunk := range chunks {
		if chunk == nil {
			continue
		}
		nonEmpty := chunk.NonEmptyVoxelCount
		if nonEmpty == 0 {
			nonEmpty = len(chunk.Voxels)
		}
		world.Entries = append(world.Entries, ImportedWorldChunkEntryDef{
			Coord:              coord,
			NonEmptyVoxelCount: nonEmpty,
		})
	}
	sort.Slice(world.Entries, func(i, j int) bool {
		return terrainChunkCoordLess(world.Entries[i].Coord, world.Entries[j].Coord)
	})
	source, err := BuildNavBuildSourceFromImportedWorld(world, chunks, NavBuildSourceBuildOptions{
		SourceID:     baseNav.NavID + "_delta_nav_source",
		AgentProfile: profile,
		Tags:         []string{"runtime_delta"},
	})
	if err != nil {
		return nil, err
	}
	return source, nil
}

type navTileProfileCoordKey struct {
	Coord          TerrainChunkCoordDef
	AgentProfileID string
}

func applyNavDeltaTilePortals(tiles map[string]*NavTileDef, baseNav *NavManifestDef, baseNavPath string, delta *WorldDeltaDef, deltaPath string, profiles []NavAgentProfileDef) error {
	if len(tiles) == 0 {
		return nil
	}
	combined := navBakeTileSlice(tiles)
	if strings.TrimSpace(baseNavPath) == "" {
		applyNavTilePortals(combined, navAgentProfilesByID(profiles))
		return nil
	}
	seen := make(map[navTileProfileCoordKey]struct{}, len(combined))
	for _, tile := range combined {
		if tile == nil {
			continue
		}
		seen[navTileProfileCoordKey{Coord: tile.Coord, AgentProfileID: tile.AgentProfileID}] = struct{}{}
	}
	for _, tile := range navBakeTileSlice(tiles) {
		if tile == nil {
			continue
		}
		for _, offset := range navPortalNeighborOffsets() {
			coord := TerrainChunkCoordDef{X: tile.Coord.X + offset.X, Y: tile.Coord.Y + offset.Y, Z: tile.Coord.Z + offset.Z}
			key := navTileProfileCoordKey{Coord: coord, AgentProfileID: tile.AgentProfileID}
			if _, ok := seen[key]; ok {
				continue
			}
			lookup, err := LoadEffectiveNavTile(baseNav, baseNavPath, delta, deltaPath, coord, tile.AgentProfileID)
			if err != nil {
				return err
			}
			if !lookup.Found || lookup.Empty || lookup.Tile == nil {
				continue
			}
			seen[key] = struct{}{}
			combined = append(combined, lookup.Tile)
		}
	}
	applyNavTilePortals(combined, navAgentProfilesByID(profiles))
	return nil
}

func ResolveNavigationTileOverridePath(override NavigationTileOverrideDef, deltaPath string) string {
	return ResolveDocumentPath(override.TilePath, deltaPath)
}

func normalizeNavDeltaBakeOptions(baseNav *NavManifestDef, opts NavDeltaBakeOptions) NavDeltaBakeOptions {
	if strings.TrimSpace(opts.BuilderVersion) == "" {
		opts.BuilderVersion = baseNav.BuilderVersion
	}
	if strings.TrimSpace(opts.BuilderVersion) == "" {
		opts.BuilderVersion = DefaultNavBuilderVersion
	}
	if len(opts.AgentProfiles) == 0 {
		opts.AgentProfiles = append([]NavAgentProfileDef(nil), baseNav.AgentProfiles...)
	}
	if len(opts.AgentProfiles) == 0 {
		opts.AgentProfiles = []NavAgentProfileDef{DefaultHL1NavAgentProfile()}
	}
	for i := range opts.AgentProfiles {
		EnsureNavAgentProfileDefaults(&opts.AgentProfiles[i])
	}
	if strings.TrimSpace(opts.SourceOverrideKind) == "" {
		opts.SourceOverrideKind = DefaultNavDeltaTileSourceOverrideKind
	}
	return opts
}

func upsertNavigationTileOverride(overrides []NavigationTileOverrideDef, override NavigationTileOverrideDef) []NavigationTileOverrideDef {
	for i := range overrides {
		if navigationTileOverrideSameKey(overrides[i], override) {
			overrides[i] = override
			return overrides
		}
	}
	return append(overrides, override)
}

func navigationTileOverrideSameKey(a, b NavigationTileOverrideDef) bool {
	return a.NavID == b.NavID && a.AgentProfileID == b.AgentProfileID && a.ChunkCoord == b.ChunkCoord
}

func navDeltaAbsf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func importedWorldDeltaOverrideCoords(delta *WorldDeltaDef, worldID string) []TerrainChunkCoordDef {
	if delta == nil {
		return nil
	}
	worldID = strings.TrimSpace(worldID)
	seen := make(map[TerrainChunkCoordDef]struct{}, len(delta.ImportedWorldChunkOverrides))
	for _, override := range delta.ImportedWorldChunkOverrides {
		if worldID != "" && strings.TrimSpace(override.WorldID) != worldID {
			continue
		}
		seen[override.ChunkCoord] = struct{}{}
	}
	out := make([]TerrainChunkCoordDef, 0, len(seen))
	for coord := range seen {
		out = append(out, coord)
	}
	sort.Slice(out, func(i, j int) bool {
		return terrainChunkCoordLess(out[i], out[j])
	})
	return out
}

func importedWorldDeltaOverridesByCoord(delta *WorldDeltaDef, worldID string) map[TerrainChunkCoordDef]ImportedWorldChunkOverrideDef {
	out := make(map[TerrainChunkCoordDef]ImportedWorldChunkOverrideDef)
	if delta == nil {
		return out
	}
	worldID = strings.TrimSpace(worldID)
	for _, override := range delta.ImportedWorldChunkOverrides {
		if worldID != "" && strings.TrimSpace(override.WorldID) != worldID {
			continue
		}
		out[override.ChunkCoord] = override
	}
	return out
}

func loadEffectiveImportedWorldDeltaChunks(importedWorldManifestPath string, world *ImportedWorldDef, deltaPath string, delta *WorldDeltaDef, coords []TerrainChunkCoordDef) (map[TerrainChunkCoordDef]*ImportedWorldChunkDef, error) {
	if world == nil {
		return nil, fmt.Errorf("imported world is nil")
	}
	entriesByCoord := importedWorldEntriesByCoord(world.Entries)
	overridesByCoord := importedWorldDeltaOverridesByCoord(delta, world.WorldID)
	chunks := make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef, len(coords))
	for _, coord := range coords {
		if override, ok := overridesByCoord[coord]; ok {
			if strings.TrimSpace(override.SnapshotPath) == "" {
				chunks[coord] = emptyNavBakeImportedWorldChunk(world, coord)
				continue
			}
			chunk, err := LoadImportedWorldChunk(ResolveDocumentPath(override.SnapshotPath, deltaPath))
			if err != nil {
				return nil, err
			}
			chunks[coord] = chunk
			continue
		}
		entry, ok := entriesByCoord[coord]
		if !ok {
			continue
		}
		if entry.NonEmptyVoxelCount == 0 {
			chunks[coord] = emptyNavBakeImportedWorldChunk(world, coord)
			continue
		}
		chunk, err := LoadImportedWorldChunk(ResolveImportedWorldChunkPath(entry, importedWorldManifestPath))
		if err != nil {
			return nil, err
		}
		chunks[coord] = chunk
	}
	return chunks, nil
}

func authorPathRelativeToDocument(targetPath string, documentPath string) string {
	if strings.TrimSpace(documentPath) == "" {
		return filepath.Clean(targetPath)
	}
	rel, err := filepath.Rel(filepath.Dir(documentPath), targetPath)
	if err != nil {
		return filepath.Clean(targetPath)
	}
	return filepath.Clean(rel)
}
