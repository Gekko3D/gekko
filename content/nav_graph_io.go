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
	return saveNavManifestJSON(path, def)
}

func LoadNavGraphManifest(path string) (*NavGraphManifestDef, error) {
	var def NavGraphManifestDef
	if err := loadNavManifestJSON(path, &def); err != nil {
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
	data, err := encodeNavSourceTile(def)
	if err != nil {
		return err
	}
	return saveNavBinary(path, data)
}

func LoadNavSourceTile(path string) (*NavSourceTileDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	def, err := decodeNavSourceTile(data)
	if err != nil {
		return nil, err
	}
	if result := ValidateNavSourceTile(def); result.HasErrors() {
		return nil, fmt.Errorf("invalid navigation source tile: %s", result.Error())
	}
	return def, nil
}

func SaveNavGraphTile(path string, def *NavGraphTileDef) error {
	if def != nil && def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentNavGraphTileSchemaVersion
	}
	if result := ValidateNavGraphTile(def); result.HasErrors() {
		return fmt.Errorf("invalid navigation graph tile: %s", result.Error())
	}
	data, err := encodeNavGraphTile(def)
	if err != nil {
		return err
	}
	return saveNavBinary(path, data)
}

func LoadNavGraphTile(path string) (*NavGraphTileDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	def, err := decodeNavGraphTile(data)
	if err != nil {
		return nil, err
	}
	if result := ValidateNavGraphTile(def); result.HasErrors() {
		return nil, fmt.Errorf("invalid navigation graph tile: %s", result.Error())
	}
	return def, nil
}

func saveNavBinary(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func saveNavManifestJSON(path string, def any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func loadNavManifestJSON(path string, def any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, def)
}
