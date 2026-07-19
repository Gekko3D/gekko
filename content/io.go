package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func SaveAsset(path string, def *AssetDef) error {
	if def == nil {
		return fmt.Errorf("asset definition is nil")
	}
	NormalizeAssetDef(def)
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
	if err := unmarshalStrictJSON(data, &def); err != nil {
		return nil, err
	}

	if def.SchemaVersion != CurrentAssetSchemaVersion {
		return nil, fmt.Errorf("unsupported schema version %d", def.SchemaVersion)
	}
	NormalizeAssetDef(&def)

	return &def, nil
}

func unmarshalStrictJSON(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON documents")
		}
		return err
	}
	return nil
}
