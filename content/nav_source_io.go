package content

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func DefaultNavBuildSourcePath(importedWorldManifestPath string) string {
	base := filepath.Base(importedWorldManifestPath)
	if base == "" {
		base = "world.gkworld"
	}
	return filepath.Join(filepath.Dir(importedWorldManifestPath), trimKnownSuffix(base, ".gkworld")+".gknavsrc")
}

func SaveNavBuildSource(path string, def *NavBuildSourceDef) error {
	if def == nil {
		return fmt.Errorf("nav build source is nil")
	}
	EnsureNavBuildSourceDefaults(def)
	if def.SchemaVersion != CurrentNavBuildSourceSchemaVersion {
		return fmt.Errorf("unsupported nav build source schema version %d", def.SchemaVersion)
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

func LoadNavBuildSource(path string) (*NavBuildSourceDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def NavBuildSourceDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	EnsureNavBuildSourceDefaults(&def)
	if def.SchemaVersion != CurrentNavBuildSourceSchemaVersion {
		return nil, fmt.Errorf("unsupported nav build source schema version %d", def.SchemaVersion)
	}
	return &def, nil
}
