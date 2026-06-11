package content

import (
	"fmt"
	"math"
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
}

type NavDeltaBakeResult struct {
	Overrides []NavigationTileOverrideDef
	Tiles     map[string]*NavTileDef
}

type NavDirtyTileExpansionOptions struct {
	AgentProfiles []NavAgentProfileDef
}

func ExpandNavDirtyTileCoords(modified []TerrainChunkCoordDef, chunkSize int, voxelResolution float32, opts NavDirtyTileExpansionOptions) []TerrainChunkCoordDef {
	if len(modified) == 0 {
		return nil
	}
	includeHorizontalNeighbors, includeVerticalNeighbors := navDirtyNeighborAxes(chunkSize, voxelResolution, opts.AgentProfiles)
	xzOffsets := []int{0}
	if includeHorizontalNeighbors {
		xzOffsets = []int{-1, 0, 1}
	}
	yOffsets := []int{0}
	if includeVerticalNeighbors {
		yOffsets = []int{-1, 0, 1}
	}
	seen := make(map[TerrainChunkCoordDef]struct{}, len(modified))
	for _, coord := range modified {
		for _, dx := range xzOffsets {
			for _, dy := range yOffsets {
				for _, dz := range xzOffsets {
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

func navDirtyNeighborAxes(chunkSize int, voxelResolution float32, profiles []NavAgentProfileDef) (bool, bool) {
	if chunkSize <= 0 || voxelResolution <= 0 {
		return false, false
	}
	if len(profiles) == 0 {
		profiles = []NavAgentProfileDef{DefaultHL1NavAgentProfile()}
	}
	maxHorizontalCells := 0
	maxVerticalCells := 0
	for _, profile := range profiles {
		EnsureNavAgentProfileDefaults(&profile)
		horizontalCells := int(math.Ceil(float64(profile.Radius / voxelResolution)))
		verticalReach := maxNavDirtyProfileVerticalReach(profile)
		verticalCells := int(math.Ceil(float64(verticalReach / voxelResolution)))
		if horizontalCells > maxHorizontalCells {
			maxHorizontalCells = horizontalCells
		}
		if verticalCells > maxVerticalCells {
			maxVerticalCells = verticalCells
		}
	}
	return maxHorizontalCells > 0, maxVerticalCells > 0
}

func maxNavDirtyProfileVerticalReach(profile NavAgentProfileDef) float32 {
	maxReach := profile.Height
	if profile.CrouchHeight > maxReach {
		maxReach = profile.CrouchHeight
	}
	if profile.StepHeight > maxReach {
		maxReach = profile.StepHeight
	}
	if profile.MaxDropHeight > maxReach {
		maxReach = profile.MaxDropHeight
	}
	if profile.MaxJumpUp > maxReach {
		maxReach = profile.MaxJumpUp
	}
	if profile.MaxJumpDown > maxReach {
		maxReach = profile.MaxJumpDown
	}
	return maxReach
}

func DefaultWorldDeltaNavTilePath(deltaPath string, navID string, agentProfileID string, coord TerrainChunkCoordDef) string {
	if strings.TrimSpace(agentProfileID) == "" {
		agentProfileID = DefaultNavAgentProfileID
	}
	agentProfileID = sanitizeNavPathToken(agentProfileID)
	return filepath.Join(DefaultWorldDeltaNavTileDir(deltaPath, navID, agentProfileID), fmt.Sprintf("%s_%d_%d_%d.gknavtile", agentProfileID, coord.X, coord.Y, coord.Z))
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
	overrides := append([]NavigationTileOverrideDef(nil), delta.NavigationTileOverrides...)
	for _, coord := range coords {
		chunk := chunks[coord]
		if chunk == nil {
			return NavDeltaBakeResult{}, fmt.Errorf("missing imported world chunk %s", TerrainChunkKey(coord))
		}
		sourceHash := importedWorldChunkNavSourceHash(chunk)
		for _, profile := range opts.AgentProfiles {
			EnsureNavAgentProfileDefaults(&profile)
			buildHash := navBuildHash(opts.BuilderVersion, profile, sourceHash)
			tileResult, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
				NavID:           baseNav.NavID,
				BuilderVersion:  opts.BuilderVersion,
				SourceDeltaHash: sourceHash,
				NavBuildHash:    buildHash,
				NeighborChunks:  chunks,
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
				if err := SaveNavTile(tilePath, tile); err != nil {
					return NavDeltaBakeResult{}, err
				}
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
	delta.NavigationTileOverrides = overrides
	return result, nil
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
