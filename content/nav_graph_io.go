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
	return saveFileAtomically(path, data, 0644)
}

func saveNavManifestJSON(path string, def any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return saveFileAtomically(path, data, 0644)
}

func saveFileAtomically(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmp != nil {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpPath)
	}()
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	tmp = nil
	if err != nil {
		return err
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func loadNavManifestJSON(path string, def any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, def)
}
