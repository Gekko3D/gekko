package content

import (
	"fmt"
	"path/filepath"
)

const CurrentAssetLibrarySchemaVersion = 1

// AssetLibraryDef gives authored content stable names for .gkasset documents.
// It deliberately has no game, importer, or presentation semantics.
type AssetLibraryDef struct {
	ID            string                 `json:"id"`
	SchemaVersion int                    `json:"schema_version"`
	Name          string                 `json:"name"`
	Entries       []AssetLibraryEntryDef `json:"entries,omitempty"`
}

type AssetLibraryEntryDef struct {
	Key       string                    `json:"key"`
	AssetPath string                    `json:"asset_path"`
	Tags      []string                  `json:"tags,omitempty"`
	Character *CharacterPresentationDef `json:"character,omitempty"`
}

func NewAssetLibraryDef(name string) *AssetLibraryDef {
	def := &AssetLibraryDef{ID: newID(), SchemaVersion: CurrentAssetLibrarySchemaVersion, Name: name}
	EnsureAssetLibraryDefaults(def)
	return def
}

func EnsureAssetLibraryDefaults(def *AssetLibraryDef) {
	if def == nil {
		return
	}
	if def.ID == "" {
		def.ID = newID()
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentAssetLibrarySchemaVersion
	}
}

func FindAssetLibraryEntry(def *AssetLibraryDef, key string) (AssetLibraryEntryDef, bool) {
	if def == nil || key == "" {
		return AssetLibraryEntryDef{}, false
	}
	for _, entry := range def.Entries {
		if entry.Key == key {
			return entry, true
		}
	}
	return AssetLibraryEntryDef{}, false
}

func ResolveAssetLibraryPath(def *AssetLibraryDef, documentPath, key string) (string, error) {
	entry, ok := FindAssetLibraryEntry(def, key)
	if !ok {
		return "", fmt.Errorf("asset library key %q not found", key)
	}
	if entry.AssetPath == "" {
		return "", fmt.Errorf("asset library key %q has no asset path", key)
	}
	if filepath.IsAbs(entry.AssetPath) {
		return filepath.Clean(entry.AssetPath), nil
	}
	return ResolveDocumentPath(entry.AssetPath, documentPath), nil
}
