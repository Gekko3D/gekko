package content

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveDocumentPath interprets relative authored paths relative to the
// document that contains them when one is known.
func ResolveDocumentPath(authoredPath string, documentPath string) string {
	authoredPath = strings.TrimSpace(authoredPath)
	if authoredPath == "" {
		return ""
	}
	if filepath.IsAbs(authoredPath) || strings.TrimSpace(documentPath) == "" {
		return authoredPath
	}
	if _, err := os.Stat(authoredPath); err == nil {
		return authoredPath
	}
	return filepath.Clean(filepath.Join(filepath.Dir(documentPath), authoredPath))
}
