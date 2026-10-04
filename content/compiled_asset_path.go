package content

import (
	"fmt"
	"path/filepath"
)

// ResolveCompiledAssetReference resolves a canonical relative slash path against
// its owning document. It never searches the process working directory for files.
func ResolveCompiledAssetReference(reference, documentPath string) (string, error) {
	if documentPath == "" || !compiledAssetReferencePath(reference) {
		return "", fmt.Errorf("invalid compiled asset reference or document path")
	}
	document, err := filepath.Abs(documentPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(document), filepath.FromSlash(reference)), nil
}
