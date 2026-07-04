package content

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

func SaveAssetAttachmentLibrary(path string, def *AssetAttachmentLibraryDef) error {
	if def == nil {
		return fmt.Errorf("asset attachment library is nil")
	}
	EnsureAssetAttachmentLibraryDefaults(def)
	if def.SchemaVersion != CurrentAssetAttachmentLibrarySchemaVersion {
		return fmt.Errorf("unsupported asset attachment library schema version %d", def.SchemaVersion)
	}
	if err := ValidateAssetAttachmentLibrary(def); err != nil {
		return err
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadAssetAttachmentLibrary(path string) (*AssetAttachmentLibraryDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def AssetAttachmentLibraryDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	EnsureAssetAttachmentLibraryDefaults(&def)
	if def.SchemaVersion != CurrentAssetAttachmentLibrarySchemaVersion {
		return nil, fmt.Errorf("unsupported asset attachment library schema version %d", def.SchemaVersion)
	}
	if err := ValidateAssetAttachmentLibrary(&def); err != nil {
		return nil, err
	}
	return &def, nil
}

func ValidateAssetAttachmentLibrary(def *AssetAttachmentLibraryDef) error {
	if def == nil {
		return fmt.Errorf("asset attachment library is nil")
	}
	if def.Name == "" {
		return fmt.Errorf("asset attachment library name is required")
	}
	seen := map[string]struct{}{}
	edges := make(map[string][]string)
	for _, attachment := range def.Attachments {
		if attachment.ID == "" || attachment.Name == "" || attachment.ParentAssetRef == "" || attachment.ParentMarkerID == "" || attachment.ChildAssetRef == "" {
			return fmt.Errorf("attachment requires id, name, parent asset ref, parent marker id, and child asset ref")
		}
		if attachment.Aim != nil && attachment.Aim.MarkerID == "" {
			return fmt.Errorf("attachment %q aim frame requires marker id", attachment.ID)
		}
		grips := map[string]struct{}{}
		for _, grip := range attachment.GripFrames {
			if grip.MarkerID == "" {
				return fmt.Errorf("attachment %q grip frame requires marker id", attachment.ID)
			}
			if _, exists := grips[grip.MarkerID]; exists {
				return fmt.Errorf("attachment %q has duplicate grip frame marker %q", attachment.ID, grip.MarkerID)
			}
			grips[grip.MarkerID] = struct{}{}
		}
		if _, ok := seen[attachment.ID]; ok {
			return fmt.Errorf("duplicate attachment id %q", attachment.ID)
		}
		seen[attachment.ID] = struct{}{}
		edges[attachment.ParentAssetRef] = append(edges[attachment.ParentAssetRef], attachment.ChildAssetRef)
	}
	if assetAttachmentLibraryHasCycle(edges) {
		return fmt.Errorf("asset attachment library contains a cycle")
	}
	return nil
}

func assetAttachmentLibraryHasCycle(edges map[string][]string) bool {
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var visit func(string) bool
	visit = func(assetRef string) bool {
		if visiting[assetRef] {
			return true
		}
		if visited[assetRef] {
			return false
		}
		visiting[assetRef] = true
		children := append([]string(nil), edges[assetRef]...)
		sort.Strings(children)
		for _, child := range children {
			if visit(child) {
				return true
			}
		}
		visiting[assetRef] = false
		visited[assetRef] = true
		return false
	}
	for assetRef := range edges {
		if visit(assetRef) {
			return true
		}
	}
	return false
}
