package content

const (
	CharacterPresentationSupported   = "supported"
	CharacterPresentationUnsupported = "unsupported"
	CharacterLocomotionFaceTravel    = "face_travel"
	CharacterBackwardReverseForward  = "reverse_forward"
	CharacterWeaponAttachmentAim     = "aim"
	CharacterWeaponAttachmentMount   = "mount"
)

// CharacterPresentationDef is source-neutral animation and anchor metadata
// for an authored character asset. The engine stores it; games choose which
// declared clips and anchors they need.
type CharacterPresentationDef struct {
	HeadMarkerID          string                            `json:"head_marker_id"`
	RightHandMarkerID     string                            `json:"right_hand_marker_id"`
	LeftHandMarkerID      string                            `json:"left_hand_marker_id,omitempty"`
	UpperBodyMarkerID     string                            `json:"upper_body_marker_id"`
	AimMarkerIDs          []string                          `json:"aim_marker_ids,omitempty"`
	AimRig                CharacterAimRigDef                `json:"aim_rig,omitempty"`
	CrouchGait            CharacterCrouchGaitDef            `json:"crouch_gait"`
	WeaponPresentation    CharacterWeaponPresentationDef    `json:"weapon_presentation"`
	DirectionalLocomotion CharacterDirectionalLocomotionDef `json:"directional_locomotion"`
}

// CharacterAimRigDef describes a character's camera-targeted upper-body rig.
// ForwardAxis and UpAxis are expressed in the character-root authored basis;
// pitch rotates around ForwardAxis × UpAxis. Runtime converts those axes into
// each aim bone's current parent space. Markers identify the spine chain; held
// assets provide the calibrated aim frame.
type CharacterAimRigDef struct {
	Status            string    `json:"status,omitempty"`
	MuzzleMarkerKind  string    `json:"muzzle_marker_kind,omitempty"`
	ForwardAxis       Vec3      `json:"forward_axis,omitempty"`
	UpAxis            Vec3      `json:"up_axis,omitempty"`
	YawLimitDegrees   float32   `json:"yaw_limit_degrees,omitempty"`
	PitchLimitDegrees float32   `json:"pitch_limit_degrees,omitempty"`
	BoneWeights       []float32 `json:"bone_weights,omitempty"`
}

type CharacterCrouchGaitDef struct {
	Status           string                       `json:"status"`
	CrouchClipID     string                       `json:"crouch_clip_id,omitempty"`
	CrouchIdleClipID string                       `json:"crouch_idle_clip_id,omitempty"`
	Locomotion       CharacterCrouchLocomotionDef `json:"locomotion,omitempty"`
	BaseStances      []CharacterStanceClipDef     `json:"base_stances,omitempty"`
	BoneMask         []string                     `json:"bone_mask,omitempty"`
	Diagnostic       string                       `json:"diagnostic,omitempty"`
}

type CharacterStanceClipDef struct {
	Stance string `json:"stance"`
	ClipID string `json:"clip_id"`
}
type CharacterWeaponPresentationDef struct {
	Status             string                     `json:"status"`
	UnarmedHandsClipID string                     `json:"unarmed_hands_clip_id,omitempty"`
	Stances            []CharacterWeaponStanceDef `json:"stances,omitempty"`
	UpperBodyMask      []string                   `json:"upper_body_mask,omitempty"`
	Diagnostic         string                     `json:"diagnostic,omitempty"`
}
type CharacterWeaponStanceDef struct {
	Stance             string   `json:"stance"`
	AimClipID          string   `json:"aim_clip_id,omitempty"`
	SprintClipID       string   `json:"sprint_clip_id,omitempty"`
	RecoilClipID       string   `json:"recoil_clip_id,omitempty"`
	CrouchAimClipID    string   `json:"crouch_aim_clip_id,omitempty"`
	CrouchRecoilClipID string   `json:"crouch_recoil_clip_id,omitempty"`
	BoneMask           []string `json:"bone_mask,omitempty"`
	// AttachmentMode controls the held prop for this stance. "mount" keeps it
	// rigid relative to its authored parent marker; the empty default uses
	// procedural aiming.
	AttachmentMode string `json:"attachment_mode,omitempty"`
}
type CharacterCrouchLocomotionDef struct {
	Directional      CharacterDirectionalClipSetDef `json:"directional,omitempty"`
	DefaultClipID    string                         `json:"default_clip_id,omitempty"`
	Fallback         string                         `json:"fallback"`
	BackwardFallback string                         `json:"backward_fallback,omitempty"`
}
type CharacterDirectionalLocomotionDef struct {
	Walk             CharacterDirectionalClipSetDef `json:"walk,omitempty"`
	Run              CharacterDirectionalClipSetDef `json:"run,omitempty"`
	Sprint           CharacterDirectionalClipSetDef `json:"sprint,omitempty"`
	TurnInPlace      *CharacterTurnInPlaceDef       `json:"turn_in_place,omitempty"`
	Fallback         string                         `json:"fallback"`
	BackwardFallback string                         `json:"backward_fallback,omitempty"`
}
type CharacterTurnInPlaceDef struct {
	LeftClipID               string  `json:"left_clip_id,omitempty"`
	RightClipID              string  `json:"right_clip_id,omitempty"`
	StartAngleDegrees        float32 `json:"start_angle_degrees,omitempty"`
	StopAngleDegrees         float32 `json:"stop_angle_degrees,omitempty"`
	TurnRateDegreesPerSecond float32 `json:"turn_rate_degrees_per_second,omitempty"`
	PlaybackSpeed            float32 `json:"playback_speed,omitempty"`
}
type CharacterDirectionalClipSetDef struct {
	Forward       string `json:"forward,omitempty"`
	Backward      string `json:"backward,omitempty"`
	Left          string `json:"left,omitempty"`
	Right         string `json:"right,omitempty"`
	ForwardLeft   string `json:"forward_left,omitempty"`
	ForwardRight  string `json:"forward_right,omitempty"`
	BackwardLeft  string `json:"backward_left,omitempty"`
	BackwardRight string `json:"backward_right,omitempty"`
}
