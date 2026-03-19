package content

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func SaveTerrainChunkManifest(path string, def *TerrainChunkManifestDef) error {
	if def == nil {
		return fmt.Errorf("terrain chunk manifest is nil")
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentTerrainChunkManifestSchemaVersion
	}
	if def.SchemaVersion != CurrentTerrainChunkManifestSchemaVersion {
		return fmt.Errorf("unsupported terrain chunk manifest schema version %d", def.SchemaVersion)
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

func LoadTerrainChunkManifest(path string) (*TerrainChunkManifestDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var def TerrainChunkManifestDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentTerrainChunkManifestSchemaVersion
	}
	if def.SchemaVersion != CurrentTerrainChunkManifestSchemaVersion {
		return nil, fmt.Errorf("unsupported terrain chunk manifest schema version %d", def.SchemaVersion)
	}
	return &def, nil
}

func SaveTerrainChunk(path string, def *TerrainChunkDef) error {
	if def == nil {
		return fmt.Errorf("terrain chunk is nil")
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentTerrainChunkSchemaVersion
	}
	if def.SchemaVersion != CurrentTerrainChunkSchemaVersion {
		return fmt.Errorf("unsupported terrain chunk schema version %d", def.SchemaVersion)
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

func LoadTerrainChunk(path string) (*TerrainChunkDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var def TerrainChunkDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentTerrainChunkSchemaVersion
	}
	if def.SchemaVersion != CurrentTerrainChunkSchemaVersion {
		return nil, fmt.Errorf("unsupported terrain chunk schema version %d", def.SchemaVersion)
	}
	if def.SolidValue == 0 {
		def.SolidValue = DefaultTerrainChunkSolidValue
	}
	return &def, nil
}
