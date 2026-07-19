package content

const CurrentAnimationRigSchemaVersion = 1

type AnimationRigDef struct {
	ID            string                 `json:"id"`
	SchemaVersion int                    `json:"schema_version"`
	Name          string                 `json:"name"`
	Joints        []AnimationRigJointDef `json:"joints"`
}

type AnimationRigJointDef struct {
	ID        string            `json:"id"`
	ParentID  string            `json:"parent_id,omitempty"`
	Transform AssetTransformDef `json:"transform"`
}

func NewAnimationRigDef(name string) *AnimationRigDef {
	return &AnimationRigDef{ID: newID(), SchemaVersion: CurrentAnimationRigSchemaVersion, Name: name}
}
