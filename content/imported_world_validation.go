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
	seenCoords := map[string]struct{}{}
	for _, entry := range def.Entries {
		key := TerrainChunkKey(entry.Coord)
		if _, ok := seenCoords[key]; ok {
			result.addError("duplicate_chunk_coord", fmt.Sprintf("duplicate imported world chunk coord %s", key))
			continue
		}
		seenCoords[key] = struct{}{}
		if strings.TrimSpace(entry.ChunkPath) == "" {
			result.addError("empty_chunk_path", fmt.Sprintf("imported world chunk path is required for coord %s", key))
			continue
		}
		if strings.ToLower(filepath.Ext(entry.ChunkPath)) != ".gkchunk" {
			result.addError("invalid_chunk_path", fmt.Sprintf("imported world chunk_path must point to a .gkchunk: %s", entry.ChunkPath))
			continue
		}
		if opts.DocumentPath != "" {
			resolvedPath := ResolveImportedWorldChunkPath(entry, opts.DocumentPath)
			if _, err := os.Stat(resolvedPath); err != nil {
				result.addError("missing_chunk_file", fmt.Sprintf("missing imported world chunk %s", entry.ChunkPath))
			}
		}
	}
	return result
}

func (r *ImportedWorldValidationResult) addError(code string, message string) {
	r.Issues = append(r.Issues, ImportedWorldValidationIssue{
		Code:    code,
		Message: message,
	})
	r.HardErrorCount++
}
