package content

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func SaveNavGraphManifest(path string, def *NavGraphManifestDef) error {
	if def != nil && def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentNavGraphManifestSchemaVersion
	}
	if result := ValidateNavGraphManifest(def); result.HasErrors() {
		return fmt.Errorf("invalid navigation graph manifest: %s", result.Error())
	}
	return saveNavGraphJSON(path, def)
}

func LoadNavGraphManifest(path string) (*NavGraphManifestDef, error) {
	var def NavGraphManifestDef
	if err := loadNavGraphJSON(path, &def); err != nil {
		return nil, err
	}
	if result := ValidateNavGraphManifest(&def); result.HasErrors() {
		return nil, fmt.Errorf("invalid navigation graph manifest: %s", result.Error())
	}
	return &def, nil
}

func SaveNavSourceTile(path string, def *NavSourceTileDef) error {
	if def != nil && def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentNavSourceTileSchemaVersion
	}
	if result := ValidateNavSourceTile(def); result.HasErrors() {
		return fmt.Errorf("invalid navigation source tile: %s", result.Error())
	}
	return saveNavGraphJSON(path, def)
}

func LoadNavSourceTile(path string) (*NavSourceTileDef, error) {
	var def NavSourceTileDef
	if err := loadNavGraphJSON(path, &def); err != nil {
		return nil, err
	}
	if result := ValidateNavSourceTile(&def); result.HasErrors() {
		return nil, fmt.Errorf("invalid navigation source tile: %s", result.Error())
	}
	return &def, nil
}

func SaveNavGraphTile(path string, def *NavGraphTileDef) error {
	if def != nil && def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentNavGraphTileSchemaVersion
	}
	if result := ValidateNavGraphTile(def); result.HasErrors() {
		return fmt.Errorf("invalid navigation graph tile: %s", result.Error())
	}
	return saveNavGraphJSON(path, def)
}

func LoadNavGraphTile(path string) (*NavGraphTileDef, error) {
	var def NavGraphTileDef
	if err := loadNavGraphJSON(path, &def); err != nil {
		return nil, err
	}
	if result := ValidateNavGraphTile(&def); result.HasErrors() {
		return nil, fmt.Errorf("invalid navigation graph tile: %s", result.Error())
	}
	return &def, nil
}

func saveNavGraphJSON(path string, def any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func loadNavGraphJSON(path string, def any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, def)
}
