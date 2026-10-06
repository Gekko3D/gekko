package content

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Both manifests are resolved before choosing grid policy. The context is
// request-local: tooling validation never publishes it into the runtime cache.
type levelStreamingValidationContext struct {
	terrain                        *TerrainChunkManifestDef
	poi                            *ImportedWorldDef
	terrainPath, poiPath           string
	terrainErr, poiErr             error
	terrainReferenced, independent bool
}

func validateLevelStreamingContent(result *LevelValidationResult, level *LevelDef, opts LevelValidationOptions) {
	layers := resolveLevelStreamingValidation(result, level, opts)
	validateLevelTerrain(result, level, opts, &layers)
	validateLevelBaseWorld(result, level, opts, &layers)
	validateLevelNavigation(result, level, opts, &layers)
	if level.StreamingBounds != nil && level.Terrain != nil && !layers.terrainReferenced {
		result.addError("unknown_terrain_streaming_bounds", "legacy terrain source without a manifest has no qualified padded streaming coverage", "", "", "", "", "", "", "")
	}
	if layers.terrainErr == nil && layers.poiErr == nil && (layers.independent || level.StreamingBounds != nil) {
		if _, err := BuildLevelStreamingIndex(level, layers.terrain, layers.poi); err != nil {
			result.addError("invalid_level_streaming_index", err.Error(), "", "", "", "", "", "", "")
		}
	}
}

func resolveLevelStreamingValidation(result *LevelValidationResult, level *LevelDef, opts LevelValidationOptions) levelStreamingValidationContext {
	var c levelStreamingValidationContext
	if level.Terrain != nil && strings.TrimSpace(level.Terrain.ManifestPath) != "" {
		c.terrainReferenced = true
		c.terrainPath = ResolveDocumentPath(level.Terrain.ManifestPath, opts.DocumentPath)
		code := "invalid_terrain_manifest"
		if strings.ToLower(filepath.Ext(c.terrainPath)) != ".gkterrainmanifest" {
			code = "invalid_terrain_manifest_path"
			c.terrainErr = fmt.Errorf("terrain manifest_path must point to a .gkterrainmanifest: %s", level.Terrain.ManifestPath)
		} else if _, err := os.Stat(c.terrainPath); err != nil {
			code = "missing_terrain_manifest"
			c.terrainErr = err
		} else {
			c.terrain, c.terrainErr = LoadTerrainChunkManifest(c.terrainPath)
		}
		if c.terrainErr != nil {
			result.addError(code, fmt.Sprintf("failed to load terrain manifest %s: %v", level.Terrain.ManifestPath, c.terrainErr), "", "", "", "", "", "", "")
		}
	}
	if level.BaseWorld != nil && strings.TrimSpace(level.BaseWorld.ManifestPath) != "" {
		c.poiPath = ResolveDocumentPath(level.BaseWorld.ManifestPath, opts.DocumentPath)
		code := "invalid_base_world_manifest"
		if strings.ToLower(filepath.Ext(c.poiPath)) != ".gkworld" {
			code = "invalid_base_world_manifest_path"
			c.poiErr = fmt.Errorf("base world manifest_path must point to a .gkworld: %s", level.BaseWorld.ManifestPath)
		} else if _, err := os.Stat(c.poiPath); err != nil {
			code = "missing_base_world_manifest"
			c.poiErr = err
		} else {
			c.poi, c.poiErr = LoadImportedWorld(c.poiPath)
		}
		if c.poiErr != nil {
			result.addError(code, fmt.Sprintf("failed to load base world manifest %s: %v", level.BaseWorld.ManifestPath, c.poiErr), "", "", "", "", "", "", level.BaseWorld.ManifestPath)
		}
	}
	c.independent = c.terrain != nil && c.terrain.SchemaVersion == TerrainHeightTileManifestSchemaVersion || c.poi != nil && c.poi.SchemaVersion == ImportedWorldPageSchemaVersion
	return c
}

func validateLevelHeightTileFiles(result *LevelValidationResult, manifest *TerrainChunkManifestDef, path string) {
	// LoadTerrainChunkManifest has validated the forest. Each decoder is bounded
	// to one tile; no full-volume expansion or repeated forest validation occurs.
	for _, entry := range manifest.Entries {
		if _, err := LoadTerrainHeightTileEntry(entry, path); err != nil {
			result.addError("invalid_terrain_height_tile", fmt.Sprintf("terrain source tile %s: %v", entry.ChunkPath, err), "", "", "", "", "", "", "")
		}
	}
	for i := range manifest.Pages {
		if _, err := loadValidatedTerrainHeightPagePayload(manifest, path, uint32(i)); err != nil {
			result.addError("invalid_terrain_height_page", fmt.Sprintf("terrain visual page %s: %v", manifest.Pages[i].Payload.Path, err), "", "", "", "", "", "", "")
		}
	}
}
