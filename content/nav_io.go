package content

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func SaveNavManifest(path string, def *NavManifestDef) error {
	if def == nil {
		return fmt.Errorf("nav manifest is nil")
	}
	EnsureNavManifestDefaults(def)
	if def.SchemaVersion != CurrentNavManifestSchemaVersion {
		return fmt.Errorf("unsupported nav manifest schema version %d", def.SchemaVersion)
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

func LoadNavManifest(path string) (*NavManifestDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def NavManifestDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	EnsureNavManifestDefaults(&def)
	if def.SchemaVersion != CurrentNavManifestSchemaVersion {
		return nil, fmt.Errorf("unsupported nav manifest schema version %d", def.SchemaVersion)
	}
	return &def, nil
}

func SaveNavTile(path string, def *NavTileDef) error {
	if def == nil {
		return fmt.Errorf("nav tile is nil")
	}
	EnsureNavTileDefaults(def)
	if def.SchemaVersion != CurrentNavTileSchemaVersion {
		return fmt.Errorf("unsupported nav tile schema version %d", def.SchemaVersion)
	}
	if validation := ValidateNavTile(def); validation.HasErrors() {
		return validation
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

func LoadNavTile(path string) (*NavTileDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def NavTileDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	EnsureNavTileDefaults(&def)
	if def.SchemaVersion != CurrentNavTileSchemaVersion {
		return nil, fmt.Errorf("unsupported nav tile schema version %d", def.SchemaVersion)
	}
	return &def, nil
}

func SaveNavClearanceSourceTile(path string, def *NavClearanceSourceTileDef) error {
	if def == nil {
		return fmt.Errorf("nav clearance source tile is nil")
	}
	EnsureNavClearanceSourceTileDefaults(def)
	if def.SchemaVersion != CurrentNavClearanceSourceTileSchemaVersion {
		return fmt.Errorf("unsupported nav clearance source tile schema version %d", def.SchemaVersion)
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

func LoadNavClearanceSourceTile(path string) (*NavClearanceSourceTileDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def NavClearanceSourceTileDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	EnsureNavClearanceSourceTileDefaults(&def)
	if def.SchemaVersion != CurrentNavClearanceSourceTileSchemaVersion {
		return nil, fmt.Errorf("unsupported nav clearance source tile schema version %d", def.SchemaVersion)
	}
	return &def, nil
}

func ResolveNavTilePath(entry NavTileEntryDef, manifestPath string) string {
	return ResolveDocumentPath(entry.TilePath, manifestPath)
}

func ResolveNavClearanceSourceTilePath(entry NavClearanceSourceTileEntryDef, manifestPath string) string {
	return ResolveDocumentPath(entry.TilePath, manifestPath)
}
