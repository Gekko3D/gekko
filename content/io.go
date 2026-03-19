package content

import (
	"encoding/json"
	"fmt"
	"os"
)

func SaveAsset(path string, def *AssetDef) error {
	if def == nil {
		return fmt.Errorf("asset definition is nil")
	}
	EnsureAssetIDs(def)
	if def.SchemaVersion != CurrentAssetSchemaVersion {
		return fmt.Errorf("unsupported schema version %d", def.SchemaVersion)
	}

	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadAsset(path string) (*AssetDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var def AssetDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}

	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentAssetSchemaVersion
	}
	if def.SchemaVersion != CurrentAssetSchemaVersion {
		return nil, fmt.Errorf("unsupported schema version %d", def.SchemaVersion)
	}
	EnsureAssetIDs(&def)

	return &def, nil
}
