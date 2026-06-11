package content

import (
	"fmt"
	"strings"
)

const (
	NavTileLookupSourceStatic = "static"
	NavTileLookupSourceDelta  = "delta"
)

type NavTileLookupResult struct {
	Tile          *NavTileDef
	TilePath      string
	Source        string
	Found         bool
	Empty         bool
	StaticEntry   *NavTileEntryDef
	DeltaOverride *NavigationTileOverrideDef
}

type NavClearanceSourceTileLookupResult struct {
	Tile        *NavClearanceSourceTileDef
	TilePath    string
	Found       bool
	StaticEntry *NavClearanceSourceTileEntryDef
}

func LoadEffectiveNavTile(baseNav *NavManifestDef, baseNavPath string, delta *WorldDeltaDef, deltaPath string, coord TerrainChunkCoordDef, agentProfileID string) (NavTileLookupResult, error) {
	resolved, err := ResolveEffectiveNavTile(baseNav, baseNavPath, delta, deltaPath, coord, agentProfileID)
	if err != nil || !resolved.Found || resolved.Empty {
		return resolved, err
	}
	tile, err := LoadNavTile(resolved.TilePath)
	if err != nil {
		return NavTileLookupResult{}, err
	}
	resolved.Tile = tile
	return resolved, nil
}

func ResolveEffectiveNavTile(baseNav *NavManifestDef, baseNavPath string, delta *WorldDeltaDef, deltaPath string, coord TerrainChunkCoordDef, agentProfileID string) (NavTileLookupResult, error) {
	if baseNav == nil {
		return NavTileLookupResult{}, fmt.Errorf("base nav manifest is nil")
	}
	EnsureNavManifestDefaults(baseNav)
	agentProfileID = strings.TrimSpace(agentProfileID)
	if agentProfileID == "" {
		agentProfileID = DefaultNavAgentProfileID
	}
	if delta != nil {
		if override, ok := findNavigationTileOverride(delta.NavigationTileOverrides, baseNav.NavID, agentProfileID, coord); ok {
			result := NavTileLookupResult{
				Source: NavTileLookupSourceDelta,
				Found:  true,
				Empty:  override.Empty,
			}
			overrideCopy := override
			result.DeltaOverride = &overrideCopy
			if override.Empty {
				return result, nil
			}
			if strings.TrimSpace(override.TilePath) == "" {
				return NavTileLookupResult{}, fmt.Errorf("navigation tile override %s/%s/%s has empty tile_path", baseNav.NavID, agentProfileID, TerrainChunkKey(coord))
			}
			result.TilePath = ResolveNavigationTileOverridePath(override, deltaPath)
			return result, nil
		}
	}
	if entry, ok := findNavTileEntry(baseNav.Tiles, agentProfileID, coord); ok {
		if strings.TrimSpace(entry.TilePath) == "" {
			return NavTileLookupResult{}, fmt.Errorf("nav tile %s/%s has empty tile_path", agentProfileID, TerrainChunkKey(coord))
		}
		entryCopy := entry
		return NavTileLookupResult{
			TilePath:    ResolveNavTilePath(entry, baseNavPath),
			Source:      NavTileLookupSourceStatic,
			Found:       true,
			StaticEntry: &entryCopy,
		}, nil
	}
	return NavTileLookupResult{}, nil
}

func LoadNavClearanceSourceTileForCoord(baseNav *NavManifestDef, baseNavPath string, coord TerrainChunkCoordDef) (NavClearanceSourceTileLookupResult, error) {
	resolved, err := ResolveNavClearanceSourceTileForCoord(baseNav, baseNavPath, coord)
	if err != nil || !resolved.Found {
		return resolved, err
	}
	tile, err := LoadNavClearanceSourceTile(resolved.TilePath)
	if err != nil {
		return NavClearanceSourceTileLookupResult{}, err
	}
	resolved.Tile = tile
	return resolved, nil
}

func ResolveNavClearanceSourceTileForCoord(baseNav *NavManifestDef, baseNavPath string, coord TerrainChunkCoordDef) (NavClearanceSourceTileLookupResult, error) {
	if baseNav == nil {
		return NavClearanceSourceTileLookupResult{}, fmt.Errorf("base nav manifest is nil")
	}
	EnsureNavManifestDefaults(baseNav)
	for _, entry := range baseNav.ClearanceSourceTiles {
		if entry.Coord != coord {
			continue
		}
		if strings.TrimSpace(entry.TilePath) == "" {
			return NavClearanceSourceTileLookupResult{}, fmt.Errorf("nav clearance source tile %s has empty tile_path", TerrainChunkKey(coord))
		}
		entryCopy := entry
		return NavClearanceSourceTileLookupResult{
			TilePath:    ResolveNavClearanceSourceTilePath(entry, baseNavPath),
			Found:       true,
			StaticEntry: &entryCopy,
		}, nil
	}
	return NavClearanceSourceTileLookupResult{}, nil
}

func findNavigationTileOverride(overrides []NavigationTileOverrideDef, navID string, agentProfileID string, coord TerrainChunkCoordDef) (NavigationTileOverrideDef, bool) {
	for _, override := range overrides {
		if override.NavID == navID && override.AgentProfileID == agentProfileID && override.ChunkCoord == coord {
			return override, true
		}
	}
	return NavigationTileOverrideDef{}, false
}

func findNavTileEntry(entries []NavTileEntryDef, agentProfileID string, coord TerrainChunkCoordDef) (NavTileEntryDef, bool) {
	for _, entry := range entries {
		if entry.AgentProfileID == agentProfileID && entry.Coord == coord {
			return entry, true
		}
	}
	return NavTileEntryDef{}, false
}
