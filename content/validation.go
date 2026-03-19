package content

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AssetValidationSeverity string

const (
	AssetValidationSeverityError AssetValidationSeverity = "error"
)

type AssetValidationIssue struct {
	Severity AssetValidationSeverity `json:"severity"`
	Code     string                  `json:"code"`
	Message  string                  `json:"message"`
	ItemID   string                  `json:"item_id,omitempty"`
	ItemName string                  `json:"item_name,omitempty"`
	ItemKind string                  `json:"item_kind,omitempty"`
}

type AssetValidationOptions struct {
	DocumentPath string
}

type AssetValidationResult struct {
	Issues         []AssetValidationIssue `json:"issues,omitempty"`
	HardErrorCount int                    `json:"hard_error_count"`
}

func (r AssetValidationResult) HasErrors() bool {
	return r.HardErrorCount > 0
}

func (r AssetValidationResult) FirstError() (AssetValidationIssue, bool) {
	for _, issue := range r.Issues {
		if issue.Severity == AssetValidationSeverityError {
			return issue, true
		}
	}
	return AssetValidationIssue{}, false
}

func (r AssetValidationResult) Error() string {
	if issue, ok := r.FirstError(); ok {
		return issue.Message
	}
	return ""
}

func ValidateAsset(def *AssetDef, opts AssetValidationOptions) AssetValidationResult {
	result := AssetValidationResult{}
	if def == nil {
		result.addError("nil_asset", "asset definition is nil", "", "", "")
		return result
	}

	if strings.TrimSpace(def.Name) == "" {
		result.addError("empty_name", "asset name is required", def.ID, def.Name, "asset")
	}

	seenIDs := map[string]struct{}{}
	allItemIDs := map[string]string{}
	if def.ID != "" {
		seenIDs[def.ID] = struct{}{}
	}

	partIDs := make(map[string]struct{}, len(def.Parts))
	partParentByID := make(map[string]string, len(def.Parts))

	for _, part := range def.Parts {
		validateUniqueID(&result, seenIDs, part.ID, part.Name, "part")
		validateName(&result, part.ID, part.Name, "part")
		validateSource(&result, part.ID, part.Name, "part", part.Source, opts)
		partIDs[part.ID] = struct{}{}
		partParentByID[part.ID] = part.ParentID
		allItemIDs[part.ID] = "part"
	}
	for _, light := range def.Lights {
		validateUniqueID(&result, seenIDs, light.ID, light.Name, "light")
		validateName(&result, light.ID, light.Name, "light")
		allItemIDs[light.ID] = "light"
	}
	for _, emitter := range def.Emitters {
		validateUniqueID(&result, seenIDs, emitter.ID, emitter.Name, "emitter")
		validateName(&result, emitter.ID, emitter.Name, "emitter")
		allItemIDs[emitter.ID] = "emitter"
	}
	for _, marker := range def.Markers {
		validateUniqueID(&result, seenIDs, marker.ID, marker.Name, "marker")
		validateName(&result, marker.ID, marker.Name, "marker")
		allItemIDs[marker.ID] = "marker"
		if strings.TrimSpace(marker.Kind) == "" {
			result.addError("empty_marker_kind", "marker kind is required", marker.ID, marker.Name, "marker")
		}
	}

	for _, part := range def.Parts {
		if part.ParentID == "" {
			continue
		}
		if part.ParentID == part.ID {
			result.addError("hierarchy_cycle", fmt.Sprintf("part %s cannot parent itself", part.ID), part.ID, part.Name, "part")
			continue
		}
		if _, ok := partIDs[part.ParentID]; ok {
			continue
		}
		if kind, exists := allItemIDs[part.ParentID]; exists && kind != "part" {
			result.addError("unsupported_parent_target", fmt.Sprintf("parent %s must reference a part", part.ParentID), part.ID, part.Name, "part")
			continue
		}
		if _, ok := partIDs[part.ParentID]; !ok {
			result.addError("broken_parent_reference", fmt.Sprintf("missing parent %s", part.ParentID), part.ID, part.Name, "part")
		}
	}
	for _, light := range def.Lights {
		validatePartParent(&result, partIDs, allItemIDs, light.ParentID, light.ID, light.Name, "light")
	}
	for _, emitter := range def.Emitters {
		validatePartParent(&result, partIDs, allItemIDs, emitter.ParentID, emitter.ID, emitter.Name, "emitter")
	}
	for _, marker := range def.Markers {
		validatePartParent(&result, partIDs, allItemIDs, marker.ParentID, marker.ID, marker.Name, "marker")
	}

	visiting := make(map[string]bool, len(partParentByID))
	visited := make(map[string]bool, len(partParentByID))
	var visit func(string) bool
	visit = func(id string) bool {
		if id == "" || visited[id] {
			return false
		}
		if visiting[id] {
			return true
		}
		visiting[id] = true
		parentID := partParentByID[id]
		if _, ok := partIDs[parentID]; ok && visit(parentID) {
			return true
		}
		visiting[id] = false
		visited[id] = true
		return false
	}
	for _, part := range def.Parts {
		if visit(part.ID) {
			result.addError("hierarchy_cycle", fmt.Sprintf("hierarchy cycle detected at %s", part.ID), part.ID, part.Name, "part")
			break
		}
	}

	return result
}

func (r *AssetValidationResult) addError(code string, message string, itemID string, itemName string, itemKind string) {
	r.Issues = append(r.Issues, AssetValidationIssue{
		Severity: AssetValidationSeverityError,
		Code:     code,
		Message:  message,
		ItemID:   itemID,
		ItemName: itemName,
		ItemKind: itemKind,
	})
	r.HardErrorCount++
}

func validateUniqueID(result *AssetValidationResult, seenIDs map[string]struct{}, id string, name string, kind string) {
	if id == "" {
		return
	}
	if _, ok := seenIDs[id]; ok {
		result.addError("duplicate_id", fmt.Sprintf("duplicate id %s", id), id, name, kind)
		return
	}
	seenIDs[id] = struct{}{}
}

func validateName(result *AssetValidationResult, id string, name string, kind string) {
	if strings.TrimSpace(name) == "" {
		result.addError("empty_name", fmt.Sprintf("%s name is required", kind), id, name, kind)
	}
}

func validatePartParent(result *AssetValidationResult, partIDs map[string]struct{}, allItemIDs map[string]string, parentID string, itemID string, itemName string, itemKind string) {
	if parentID == "" {
		return
	}
	if _, ok := partIDs[parentID]; ok {
		return
	}
	if kind, exists := allItemIDs[parentID]; exists && kind != "part" {
		result.addError("unsupported_parent_target", fmt.Sprintf("parent %s must reference a part", parentID), itemID, itemName, itemKind)
		return
	}
	result.addError("broken_parent_reference", fmt.Sprintf("missing parent %s", parentID), itemID, itemName, itemKind)
}

func validateSource(result *AssetValidationResult, itemID string, itemName string, itemKind string, source AssetSourceDef, opts AssetValidationOptions) {
	switch source.Kind {
	case AssetSourceKindVoxModel:
		if strings.TrimSpace(source.Path) == "" {
			result.addError("invalid_source_payload", "vox_model source requires path", itemID, itemName, itemKind)
		}
		if source.ModelIndex < 0 {
			result.addError("invalid_source_payload", "vox_model source requires model_index >= 0", itemID, itemName, itemKind)
		}
		validateSourceFile(result, itemID, itemName, itemKind, source.Path, opts)
	case AssetSourceKindVoxSceneNode:
		if strings.TrimSpace(source.Path) == "" {
			result.addError("invalid_source_payload", "vox_scene_node source requires path", itemID, itemName, itemKind)
		}
		if strings.TrimSpace(source.NodeName) == "" {
			result.addError("invalid_source_payload", "vox_scene_node source requires node_name", itemID, itemName, itemKind)
		}
		validateSourceFile(result, itemID, itemName, itemKind, source.Path, opts)
	case AssetSourceKindProceduralPrimitive:
		validateProceduralPrimitive(result, itemID, itemName, itemKind, source)
	default:
		result.addError("invalid_source_payload", fmt.Sprintf("unsupported source kind %q", source.Kind), itemID, itemName, itemKind)
	}
}

func validateProceduralPrimitive(result *AssetValidationResult, itemID string, itemName string, itemKind string, source AssetSourceDef) {
	required := []string(nil)
	switch source.Primitive {
	case "cube":
		required = []string{"sx", "sy", "sz"}
	case "sphere":
		required = []string{"radius"}
	case "cone":
		required = []string{"radius", "height"}
	case "pyramid":
		required = []string{"size", "height"}
	default:
		result.addError("invalid_source_payload", fmt.Sprintf("unsupported procedural primitive %q", source.Primitive), itemID, itemName, itemKind)
		return
	}
	for _, key := range required {
		if _, ok := source.Params[key]; !ok {
			result.addError("invalid_source_payload", fmt.Sprintf("procedural primitive %q requires param %s", source.Primitive, key), itemID, itemName, itemKind)
		}
	}
}

func validateSourceFile(result *AssetValidationResult, itemID string, itemName string, itemKind string, sourcePath string, opts AssetValidationOptions) {
	if strings.TrimSpace(sourcePath) == "" {
		return
	}
	if pathExists(ResolveDocumentPath(sourcePath, opts.DocumentPath)) {
		return
	}
	if filepath.IsAbs(sourcePath) {
		result.addError("missing_source_file", fmt.Sprintf("missing source file %s", sourcePath), itemID, itemName, itemKind)
		return
	}
	result.addError("missing_source_file", fmt.Sprintf("missing source file %s", sourcePath), itemID, itemName, itemKind)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
