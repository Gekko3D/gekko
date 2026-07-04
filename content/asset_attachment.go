package content

const CurrentAssetAttachmentLibrarySchemaVersion = 1

// AssetAttachmentLibraryDef is an authored, reusable graph of child asset
// mounts. Asset refs are caller-defined stable IDs; a game or tool resolves
// them to concrete .gkasset paths before spawning.
type AssetAttachmentLibraryDef struct {
	ID            string               `json:"id"`
	SchemaVersion int                  `json:"schema_version"`
	Name          string               `json:"name"`
	Tags          []string             `json:"tags,omitempty"`
	Attachments   []AssetAttachmentDef `json:"attachments,omitempty"`
}

type AssetAttachmentDef struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	ParentAssetRef string            `json:"parent_asset_ref"`
	ParentMarkerID string            `json:"parent_marker_id"`
	ChildAssetRef  string            `json:"child_asset_ref"`
	Transform      AssetTransformDef `json:"transform"`
	Tags           []string          `json:"tags,omitempty"`
}

func NewAssetAttachmentLibraryDef(name string) *AssetAttachmentLibraryDef {
	def := &AssetAttachmentLibraryDef{ID: newID(), SchemaVersion: CurrentAssetAttachmentLibrarySchemaVersion, Name: name}
	EnsureAssetAttachmentLibraryDefaults(def)
	return def
}

func EnsureAssetAttachmentLibraryDefaults(def *AssetAttachmentLibraryDef) {
	if def == nil {
		return
	}
	if def.ID == "" {
		def.ID = newID()
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentAssetAttachmentLibrarySchemaVersion
	}
}

func FindAssetAttachment(def *AssetAttachmentLibraryDef, id string) (AssetAttachmentDef, bool) {
	if def == nil || id == "" {
		return AssetAttachmentDef{}, false
	}
	for _, attachment := range def.Attachments {
		if attachment.ID == id {
			return attachment, true
		}
	}
	return AssetAttachmentDef{}, false
}
