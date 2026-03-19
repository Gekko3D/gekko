package content

import (
	"encoding/json"
	"fmt"
	"os"
)

func SaveLevel(path string, def *LevelDef) error {
	if def == nil {
		return fmt.Errorf("level definition is nil")
	}
	EnsureLevelIDs(def)
	if def.SchemaVersion != CurrentLevelSchemaVersion {
		return fmt.Errorf("unsupported schema version %d", def.SchemaVersion)
	}

	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadLevel(path string) (*LevelDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var def LevelDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}

	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentLevelSchemaVersion
	}
	if def.SchemaVersion != CurrentLevelSchemaVersion {
		return nil, fmt.Errorf("unsupported schema version %d", def.SchemaVersion)
	}
	EnsureLevelIDs(&def)

	return &def, nil
}
