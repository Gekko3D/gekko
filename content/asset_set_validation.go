package content

import (
	"fmt"
	"os"
	"strings"
)

type AssetSetValidationSeverity string

const (
	AssetSetValidationSeverityError AssetSetValidationSeverity = "error"
)

type AssetSetValidationIssue struct {
	Severity  AssetSetValidationSeverity `json:"severity"`
	Code      string                     `json:"code"`
	Message   string                     `json:"message"`
	EntryPath string                     `json:"entry_path,omitempty"`
}

type AssetSetValidationOptions struct {
	DocumentPath string
}

type AssetSetValidationResult struct {
	Issues         []AssetSetValidationIssue `json:"issues,omitempty"`
	HardErrorCount int                       `json:"hard_error_count"`
}

func (r AssetSetValidationResult) HasErrors() bool {
	return r.HardErrorCount > 0
}

func (r AssetSetValidationResult) FirstError() (AssetSetValidationIssue, bool) {
	for _, issue := range r.Issues {
		if issue.Severity == AssetSetValidationSeverityError {
			return issue, true
		}
	}
	return AssetSetValidationIssue{}, false
}

func (r AssetSetValidationResult) Error() string {
	if issue, ok := r.FirstError(); ok {
		return issue.Message
	}
	return ""
}

func ValidateAssetSet(def *AssetSetDef, opts AssetSetValidationOptions) AssetSetValidationResult {
	result := AssetSetValidationResult{}
	if def == nil {
		result.addError("nil_asset_set", "asset set definition is nil", "")
		return result
	}

	if strings.TrimSpace(def.Name) == "" {
		result.addError("empty_name", "asset set name is required", "")
	}
	if len(def.Entries) == 0 {
		result.addError("empty_entries", "asset set must contain at least one entry", "")
	}

	for _, entry := range def.Entries {
		if entry.Weight <= 0 {
			result.addError("invalid_weight", fmt.Sprintf("asset set entry %s weight must be positive", entry.AssetPath), entry.AssetPath)
		}
		if strings.TrimSpace(entry.AssetPath) == "" {
			result.addError("empty_asset_path", "asset set entry asset_path is required", "")
			continue
		}
		if strings.TrimSpace(opts.DocumentPath) == "" {
			continue
		}
		resolvedPath := ResolveDocumentPath(entry.AssetPath, opts.DocumentPath)
		if _, err := os.Stat(resolvedPath); err != nil {
			result.addError("missing_asset_file", fmt.Sprintf("missing asset file %s", entry.AssetPath), entry.AssetPath)
		}
	}

	return result
}

func (r *AssetSetValidationResult) addError(code string, message string, entryPath string) {
	r.Issues = append(r.Issues, AssetSetValidationIssue{
		Severity:  AssetSetValidationSeverityError,
		Code:      code,
		Message:   message,
		EntryPath: entryPath,
	})
	r.HardErrorCount++
}
