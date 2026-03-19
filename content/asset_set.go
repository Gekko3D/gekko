package content

const CurrentAssetSetSchemaVersion = 1

type AssetSetDef struct {
	ID            string             `json:"id"`
	SchemaVersion int                `json:"schema_version"`
	Name          string             `json:"name"`
	Tags          []string           `json:"tags,omitempty"`
	Entries       []AssetSetEntryDef `json:"entries,omitempty"`
}

type AssetSetEntryDef struct {
	AssetPath string   `json:"asset_path"`
	Weight    float32  `json:"weight,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

func NewAssetSetDef(name string) *AssetSetDef {
	def := &AssetSetDef{
		ID:            newID(),
		SchemaVersion: CurrentAssetSetSchemaVersion,
		Name:          name,
	}
	EnsureAssetSetDefaults(def)
	return def
}

func EnsureAssetSetDefaults(def *AssetSetDef) {
	if def == nil {
		return
	}
	if def.ID == "" {
		def.ID = newID()
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentAssetSetSchemaVersion
	}
}
