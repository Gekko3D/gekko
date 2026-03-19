package content

import (
	"encoding/json"
	"fmt"
	"os"
)

func SaveTerrainSource(path string, def *TerrainSourceDef) error {
	if def == nil {
		return fmt.Errorf("terrain definition is nil")
	}
	EnsureTerrainSourceDefaults(def)
	if def.SchemaVersion != CurrentTerrainSchemaVersion {
		return fmt.Errorf("unsupported schema version %d", def.SchemaVersion)
	}

	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadTerrainSource(path string) (*TerrainSourceDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var def TerrainSourceDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}

	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentTerrainSchemaVersion
	}
	if def.SchemaVersion != CurrentTerrainSchemaVersion {
		return nil, fmt.Errorf("unsupported schema version %d", def.SchemaVersion)
	}
	EnsureTerrainSourceDefaults(&def)

	return &def, nil
}
