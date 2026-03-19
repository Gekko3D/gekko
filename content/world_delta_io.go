package content

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func SaveWorldDelta(path string, def *WorldDeltaDef) error {
	if def == nil {
		return fmt.Errorf("world delta is nil")
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentWorldDeltaSchemaVersion
	}
	if def.SchemaVersion != CurrentWorldDeltaSchemaVersion {
		return fmt.Errorf("unsupported world delta schema version %d", def.SchemaVersion)
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

func LoadWorldDelta(path string) (*WorldDeltaDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var def WorldDeltaDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentWorldDeltaSchemaVersion
	}
	if def.SchemaVersion != CurrentWorldDeltaSchemaVersion {
		return nil, fmt.Errorf("unsupported world delta schema version %d", def.SchemaVersion)
	}
	return &def, nil
}

func SaveVoxelObjectSnapshot(path string, def *VoxelObjectSnapshotDef) error {
	if def == nil {
		return fmt.Errorf("voxel object snapshot is nil")
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentVoxelObjectSnapshotSchemaVersion
	}
	if def.SchemaVersion != CurrentVoxelObjectSnapshotSchemaVersion {
		return fmt.Errorf("unsupported voxel object snapshot schema version %d", def.SchemaVersion)
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

func LoadVoxelObjectSnapshot(path string) (*VoxelObjectSnapshotDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var def VoxelObjectSnapshotDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentVoxelObjectSnapshotSchemaVersion
	}
	if def.SchemaVersion != CurrentVoxelObjectSnapshotSchemaVersion {
		return nil, fmt.Errorf("unsupported voxel object snapshot schema version %d", def.SchemaVersion)
	}
	return &def, nil
}
