package content

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ImportedWorldValidationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ImportedWorldValidationOptions struct {
	DocumentPath string
}

type ImportedWorldValidationResult struct {
	Issues         []ImportedWorldValidationIssue `json:"issues,omitempty"`
	HardErrorCount int                            `json:"hard_error_count"`
}

func (r ImportedWorldValidationResult) HasErrors() bool {
	return r.HardErrorCount > 0
}

func (r ImportedWorldValidationResult) Error() string {
	if len(r.Issues) == 0 {
		return ""
	}
	return r.Issues[0].Message
}

func ValidateImportedWorld(def *ImportedWorldDef, opts ImportedWorldValidationOptions) ImportedWorldValidationResult {
	result := ImportedWorldValidationResult{}
	if def == nil {
		result.addError("nil_imported_world", "imported world definition is nil")
		return result
	}
	if def.WorldID == "" {
		result.addError("empty_world_id", "imported world world_id is required")
	}
	if def.Kind != ImportedWorldKindVoxelWorld {
		result.addError("invalid_world_kind", fmt.Sprintf("unsupported imported world kind %q", def.Kind))
	}
	if def.ChunkSize <= 0 {
		result.addError("invalid_chunk_size", "imported world chunk_size must be positive")
	}
	if def.VoxelResolution <= 0 {
		result.addError("invalid_voxel_resolution", "imported world voxel_resolution must be positive")
	}
	if _, err := NormalizeImportedWorldChunkPayloadKind(def.ChunkPayloadKind); err != nil {
		result.addError("invalid_chunk_payload_kind", err.Error())
	}
	seenCoords := map[TerrainChunkCoordDef]ImportedWorldChunkEntryDef{}
	for _, entry := range def.Entries {
		key := TerrainChunkKey(entry.Coord)
		if _, ok := seenCoords[entry.Coord]; ok {
			result.addError("duplicate_chunk_coord", fmt.Sprintf("duplicate imported world chunk coord %s", key))
			continue
		}
		seenCoords[entry.Coord] = entry
		if strings.TrimSpace(entry.ChunkPath) == "" {
			result.addError("empty_chunk_path", fmt.Sprintf("imported world chunk path is required for coord %s", key))
			continue
		}
		if strings.ToLower(filepath.Ext(entry.ChunkPath)) != ".gkchunk" {
			result.addError("invalid_chunk_path", fmt.Sprintf("imported world chunk_path must point to a .gkchunk: %s", entry.ChunkPath))
			continue
		}
		if _, err := NormalizeImportedWorldChunkPayloadKind(entry.PayloadKind); err != nil {
			result.addError("invalid_chunk_payload_kind", err.Error())
			continue
		}
		if opts.DocumentPath != "" {
			resolvedPath := ResolveImportedWorldChunkPath(entry, opts.DocumentPath)
			if _, err := os.Stat(resolvedPath); err != nil {
				result.addError("missing_chunk_file", fmt.Sprintf("missing imported world chunk %s", entry.ChunkPath))
			}
		}
	}
	if len(def.Entries) > 0 && len(def.Sectors) == 0 {
		result.addError("empty_sectors", "imported world sectors are required for non-empty worlds")
	}
	seenSectorCoords := map[TerrainChunkCoordDef]struct{}{}
	duplicateSectorCoords := map[TerrainChunkCoordDef]struct{}{}
	for _, sector := range def.Sectors {
		sectorKey := TerrainChunkKey(sector.Coord)
		if _, ok := seenSectorCoords[sector.Coord]; ok {
			result.addError("duplicate_sector_coord", fmt.Sprintf("duplicate imported world sector coord %s", sectorKey))
			duplicateSectorCoords[sector.Coord] = struct{}{}
			continue
		}
		seenSectorCoords[sector.Coord] = struct{}{}
	}
	referencedChunkCoords := map[TerrainChunkCoordDef]struct{}{}
	for _, sector := range def.Sectors {
		sectorKey := TerrainChunkKey(sector.Coord)
		if _, duplicate := duplicateSectorCoords[sector.Coord]; duplicate {
			continue
		}
		if len(sector.FullChunkRefs) == 0 {
			result.addError("empty_sector_chunks", fmt.Sprintf("imported world sector %s has no full chunk refs", sectorKey))
			continue
		}
		if sector.BoundsMax[0] <= sector.BoundsMin[0] || sector.BoundsMax[1] <= sector.BoundsMin[1] || sector.BoundsMax[2] <= sector.BoundsMin[2] {
			result.addError("invalid_sector_bounds", fmt.Sprintf("imported world sector %s has invalid bounds", sectorKey))
		}
		for _, ref := range sector.FullChunkRefs {
			if _, ok := seenCoords[ref]; !ok {
				result.addError("missing_sector_chunk_ref", fmt.Sprintf("imported world sector %s references missing chunk coord %s", sectorKey, TerrainChunkKey(ref)))
				continue
			}
			if _, ok := referencedChunkCoords[ref]; ok {
				result.addError("duplicate_sector_chunk_ref", fmt.Sprintf("imported world chunk coord %s is referenced by more than one sector", TerrainChunkKey(ref)))
				continue
			}
			referencedChunkCoords[ref] = struct{}{}
		}
		validateImportedWorldSectorRefs(&result, sectorKey, "visible_sector_refs", sector.VisibleSectorRefs, seenSectorCoords)
		validateImportedWorldSectorRefs(&result, sectorKey, "adjacent_sector_refs", sector.AdjacentSectorRefs, seenSectorCoords)
		for _, lod := range sector.LODs {
			if lod.Level <= 0 {
				result.addError("invalid_sector_lod_level", fmt.Sprintf("imported world sector %s has invalid lod level %d", sectorKey, lod.Level))
			}
			if strings.TrimSpace(lod.Kind) == "" {
				result.addError("empty_sector_lod_kind", fmt.Sprintf("imported world sector %s has an empty lod kind", sectorKey))
			}
			if strings.TrimSpace(lod.ChunkPath) == "" {
				result.addError("empty_sector_lod_path", fmt.Sprintf("imported world sector %s lod path is required", sectorKey))
				continue
			}
			if strings.ToLower(filepath.Ext(lod.ChunkPath)) != ".gkchunk" {
				result.addError("invalid_sector_lod_path", fmt.Sprintf("imported world sector lod path must point to a .gkchunk: %s", lod.ChunkPath))
				continue
			}
			if _, err := NormalizeImportedWorldChunkPayloadKind(lod.PayloadKind); err != nil {
				result.addError("invalid_sector_lod_payload_kind", err.Error())
				continue
			}
			if opts.DocumentPath != "" {
				resolvedPath := ResolveDocumentPath(lod.ChunkPath, opts.DocumentPath)
				if _, err := os.Stat(resolvedPath); err != nil {
					result.addError("missing_sector_lod_file", fmt.Sprintf("missing imported world sector lod chunk %s", lod.ChunkPath))
				}
			}
		}
	}
	for coord := range seenCoords {
		if _, ok := referencedChunkCoords[coord]; !ok {
			result.addError("unreferenced_chunk_entry", fmt.Sprintf("imported world chunk coord %s is not referenced by a sector", TerrainChunkKey(coord)))
		}
	}
	return result
}

func validateImportedWorldSectorRefs(result *ImportedWorldValidationResult, sectorKey string, field string, refs []TerrainChunkCoordDef, sectorCoords map[TerrainChunkCoordDef]struct{}) {
	seenRefs := map[TerrainChunkCoordDef]struct{}{}
	for _, ref := range refs {
		if _, ok := sectorCoords[ref]; !ok {
			result.addError("missing_sector_ref", fmt.Sprintf("imported world sector %s %s references missing sector %s", sectorKey, field, TerrainChunkKey(ref)))
			continue
		}
		if _, ok := seenRefs[ref]; ok {
			result.addError("duplicate_sector_ref", fmt.Sprintf("imported world sector %s %s references sector %s more than once", sectorKey, field, TerrainChunkKey(ref)))
			continue
		}
		seenRefs[ref] = struct{}{}
	}
}

func (r *ImportedWorldValidationResult) addError(code string, message string) {
	r.Issues = append(r.Issues, ImportedWorldValidationIssue{
		Code:    code,
		Message: message,
	})
	r.HardErrorCount++
}
