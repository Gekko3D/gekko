package content

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func SaveImportedWorld(path string, def *ImportedWorldDef) error {
	if def == nil {
		return fmt.Errorf("imported world is nil")
	}
	EnsureImportedWorldDefaults(def)
	if def.SchemaVersion != CurrentImportedWorldSchemaVersion {
		return fmt.Errorf("unsupported imported world schema version %d", def.SchemaVersion)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadImportedWorld(path string) (*ImportedWorldDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def ImportedWorldDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	EnsureImportedWorldDefaults(&def)
	if def.SchemaVersion != CurrentImportedWorldSchemaVersion {
		return nil, fmt.Errorf("unsupported imported world schema version %d", def.SchemaVersion)
	}
	return &def, nil
}

func SaveImportedWorldChunk(path string, def *ImportedWorldChunkDef) error {
	if def == nil {
		return fmt.Errorf("imported world chunk is nil")
	}
	EnsureImportedWorldChunkDefaults(def)
	if def.SchemaVersion != CurrentImportedWorldChunkSchemaVersion {
		return fmt.Errorf("unsupported imported world chunk schema version %d", def.SchemaVersion)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadImportedWorldChunk(path string) (*ImportedWorldChunkDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def ImportedWorldChunkDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	EnsureImportedWorldChunkDefaults(&def)
	if def.SchemaVersion != CurrentImportedWorldChunkSchemaVersion {
		return nil, fmt.Errorf("unsupported imported world chunk schema version %d", def.SchemaVersion)
	}
	return &def, nil
}

func ResolveImportedWorldChunkPath(entry ImportedWorldChunkEntryDef, manifestPath string) string {
	return ResolveDocumentPath(entry.ChunkPath, manifestPath)
}
