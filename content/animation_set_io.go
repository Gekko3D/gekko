package content

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func SaveAnimationSet(path string, def *AnimationSetDef) error {
	if err := ValidateAnimationSet(def); err != nil {
		return err
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadAnimationSet(path string) (*AnimationSetDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def AnimationSetDef
	if err := unmarshalStrictJSON(data, &def); err != nil {
		return nil, err
	}
	if err := ValidateAnimationSet(&def); err != nil {
		return nil, err
	}
	return &def, nil
}

func ValidateAnimationSet(def *AnimationSetDef) error {
	if def == nil {
		return fmt.Errorf("animation set is nil")
	}
	if def.SchemaVersion < 1 || def.SchemaVersion > CurrentAnimationSetSchemaVersion {
		return fmt.Errorf("unsupported animation set schema version %d", def.SchemaVersion)
	}
	if strings.TrimSpace(def.ID) == "" || strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("animation set id and name are required")
	}
	if (strings.TrimSpace(def.RigPath) == "") == (strings.TrimSpace(def.TargetAssetID) == "") {
		return fmt.Errorf("animation set %q requires exactly one of rig_path or target_asset_id", def.Name)
	}
	if len(def.Clips) == 0 {
		return fmt.Errorf("animation set %q has no clips", def.Name)
	}
	result := AssetValidationResult{}
	validateAnimationClips(&result, def.Clips, nil, false)
	if def.SchemaVersion == 1 {
		for _, clip := range def.Clips {
			if clip.Blend1D != nil {
				return fmt.Errorf("animation set %q schema v1 cannot contain blend_1d", def.Name)
			}
		}
	}
	if result.HasErrors() {
		return fmt.Errorf("animation set %q is invalid: %s", def.Name, result.Error())
	}
	return nil
}
