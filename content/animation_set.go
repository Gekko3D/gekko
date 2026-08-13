package content

const CurrentAnimationSetSchemaVersion = 2

type AnimationSetDef struct {
	ID            string                  `json:"id"`
	SchemaVersion int                     `json:"schema_version"`
	Name          string                  `json:"name"`
	RigPath       string                  `json:"rig_path,omitempty"`
	TargetAssetID string                  `json:"target_asset_id,omitempty"`
	Clips         []AssetAnimationClipDef `json:"clips"`
}

func NewAnimationSetDef(name string) *AnimationSetDef {
	return &AnimationSetDef{ID: newID(), SchemaVersion: CurrentAnimationSetSchemaVersion, Name: name}
}
