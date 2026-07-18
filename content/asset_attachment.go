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
	ID             string `json:"id"`
	Name           string `json:"name"`
	ParentAssetRef string `json:"parent_asset_ref"`
	ParentMarkerID string `json:"parent_marker_id"`
	ChildAssetRef  string `json:"child_asset_ref"`
	// AssemblyRef links sibling mounts that make up one presentation assembly.
	// A character may mount a held tool and body-worn support gear together.
	AssemblyRef string                        `json:"assembly_ref,omitempty"`
	Transform   AssetTransformDef             `json:"transform"`
	Aim         *AssetAttachmentAimFrameDef   `json:"aim,omitempty"`
	// AimOffset is applied after a presentation asset has been aimed. Its
	// local -Z direction is forward, matching AssetAttachmentAimFrameDef.
	AimOffset   *Vec3                         `json:"aim_offset,omitempty"`
	GripFrames  []AssetAttachmentGripFrameDef `json:"grip_frames,omitempty"`
	Tags        []string                      `json:"tags,omitempty"`
}

// AssetAttachmentAimFrameDef is a calibrated aim frame relative to a stable
// child marker. Its local -Z axis is the rendered aim direction.
type AssetAttachmentAimFrameDef struct {
	MarkerID string            `json:"marker_id"`
	Frame    AssetTransformDef `json:"frame"`
}

// AssetAttachmentGripFrameDef calibrates a hand target relative to a stable
// child grip marker. It is attachment-owned so import regeneration never
// overwrites per-avatar presentation tuning.
type AssetAttachmentGripFrameDef struct {
	MarkerID string            `json:"marker_id"`
	Frame    AssetTransformDef `json:"frame"`
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
