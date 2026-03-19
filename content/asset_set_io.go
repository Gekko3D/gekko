package content

import (
	"encoding/json"
	"fmt"
	"os"
)

func SaveAssetSet(path string, def *AssetSetDef) error {
	if def == nil {
		return fmt.Errorf("asset set definition is nil")
	}
	EnsureAssetSetDefaults(def)
	if def.SchemaVersion != CurrentAssetSetSchemaVersion {
		return fmt.Errorf("unsupported asset set schema version %d", def.SchemaVersion)
	}

	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadAssetSet(path string) (*AssetSetDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var def AssetSetDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}

	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentAssetSetSchemaVersion
	}
	if def.SchemaVersion != CurrentAssetSetSchemaVersion {
		return nil, fmt.Errorf("unsupported asset set schema version %d", def.SchemaVersion)
	}
	EnsureAssetSetDefaults(&def)
	return &def, nil
}
