package content

import (
	"encoding/json"
	"fmt"
	"os"
)

func SaveAssetLibrary(path string, def *AssetLibraryDef) error {
	if def == nil {
		return fmt.Errorf("asset library is nil")
	}
	EnsureAssetLibraryDefaults(def)
	if def.SchemaVersion != CurrentAssetLibrarySchemaVersion {
		return fmt.Errorf("unsupported asset library schema version %d", def.SchemaVersion)
	}
	if err := ValidateAssetLibrary(def); err != nil {
		return err
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadAssetLibrary(path string) (*AssetLibraryDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def AssetLibraryDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	EnsureAssetLibraryDefaults(&def)
	if def.SchemaVersion != CurrentAssetLibrarySchemaVersion {
		return nil, fmt.Errorf("unsupported asset library schema version %d", def.SchemaVersion)
	}
	if err := ValidateAssetLibrary(&def); err != nil {
		return nil, err
	}
	return &def, nil
}

func ValidateAssetLibrary(def *AssetLibraryDef) error {
	if def == nil || def.Name == "" {
		return fmt.Errorf("asset library name is required")
	}
	seen := map[string]struct{}{}
	for _, entry := range def.Entries {
		if entry.Key == "" || entry.AssetPath == "" {
			return fmt.Errorf("asset library entries require key and asset path")
		}
		if _, ok := seen[entry.Key]; ok {
			return fmt.Errorf("duplicate asset library key %q", entry.Key)
		}
		seen[entry.Key] = struct{}{}
	}
	return nil
}
