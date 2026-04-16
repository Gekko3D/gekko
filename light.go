package gekko

type LightType uint32

const (
	LightTypePoint       LightType = 0
	LightTypeDirectional LightType = 1
	LightTypeSpot        LightType = 2
	LightTypeAmbient     LightType = 3
)

// LightComponent is the ECS component for lights
type LightComponent struct {
	Type         LightType  `gekko:"light" usage:"type"`
	Color        [3]float32 `gekko:"light" usage:"color"` // RGB
	Intensity    float32    `gekko:"light" usage:"intensity"`
	Range        float32    `gekko:"light" usage:"range"`         // For point/spot
	ConeAngle    float32    `gekko:"light" usage:"cone_angle"`    // Full cone angle in degrees (spot)
	CastsShadows bool       `gekko:"light" usage:"casts_shadows"` // Point lights default to unshadowed unless explicitly enabled
	// SourceRadius offsets shadow rays away from the center of local lights.
	// If this is zero and EmitterLinkID is set, the engine derives a default
	// radius from the linked emitter geometry's world-space AABB.
	SourceRadius float32
	// EmitterLinkID links this light to visible voxel emitter geometry with the same ID.
	// In the shadow pass, linked emitter geometry is ignored only for this light so
	// emissive shells like suns or bulbs do not block their own illumination.
	EmitterLinkID uint32
}
