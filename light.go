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
	Type      LightType  `gekko:"light" usage:"type"`
	Color     [3]float32 `gekko:"light" usage:"color"` // RGB
	Intensity float32    `gekko:"light" usage:"intensity"`
	Range     float32    `gekko:"light" usage:"range"`      // For point/spot
	ConeAngle float32    `gekko:"light" usage:"cone_angle"` // Full cone angle in degrees (spot)
}
