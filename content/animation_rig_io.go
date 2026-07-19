package content

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func SaveAnimationRig(path string, def *AnimationRigDef) error {
	if err := ValidateAnimationRig(def); err != nil {
		return err
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadAnimationRig(path string) (*AnimationRigDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def AnimationRigDef
	if err := unmarshalStrictJSON(data, &def); err != nil {
		return nil, err
	}
	if err := ValidateAnimationRig(&def); err != nil {
		return nil, err
	}
	return &def, nil
}

func ValidateAnimationRig(def *AnimationRigDef) error {
	if def == nil {
		return fmt.Errorf("animation rig is nil")
	}
	if def.SchemaVersion != CurrentAnimationRigSchemaVersion {
		return fmt.Errorf("unsupported animation rig schema version %d", def.SchemaVersion)
	}
	if strings.TrimSpace(def.ID) == "" || strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("animation rig id and name are required")
	}
	if len(def.Joints) == 0 {
		return fmt.Errorf("animation rig %q has no joints", def.Name)
	}
	parents := make(map[string]string, len(def.Joints))
	for _, joint := range def.Joints {
		if strings.TrimSpace(joint.ID) == "" {
			return fmt.Errorf("animation rig %q contains an empty joint id", def.Name)
		}
		if _, exists := parents[joint.ID]; exists {
			return fmt.Errorf("animation rig %q contains duplicate joint %q", def.Name, joint.ID)
		}
		parents[joint.ID] = joint.ParentID
	}
	for id, parent := range parents {
		if parent == id {
			return fmt.Errorf("animation rig joint %q cannot parent itself", id)
		}
		if parent != "" {
			if _, ok := parents[parent]; !ok {
				return fmt.Errorf("animation rig joint %q references missing parent %q", id, parent)
			}
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visited[id] {
			return false
		}
		if visiting[id] {
			return true
		}
		visiting[id] = true
		if parent := parents[id]; parent != "" && visit(parent) {
			return true
		}
		visiting[id] = false
		visited[id] = true
		return false
	}
	for id := range parents {
		if visit(id) {
			return fmt.Errorf("animation rig contains a hierarchy cycle at %q", id)
		}
	}
	return nil
}
