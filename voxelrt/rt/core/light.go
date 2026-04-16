package core

const (
	LightTypePoint uint32 = iota
	LightTypeDirectional
	LightTypeSpot
)

const DirectionalShadowCascadeCount = 2
const PointShadowFaceCount = 6

const (
	ShadowUpdateKindSpot uint32 = iota
	ShadowUpdateKindDirectional
	ShadowUpdateKindPoint
)

const (
	ShadowTierHero uint32 = iota
	ShadowTierNear
	ShadowTierMedium
	ShadowTierFar
)

type DirectionalShadowCascade struct {
	ViewProj    [16]float32
	InvViewProj [16]float32
	Params      [4]float32 // x: split_far, y: texel_world_size, z: depth_scale_to_ndc, w: reserved
}

// Light is the GPU representation of a light
type Light struct {
	Position            [4]float32  // xyz, source radius
	Direction           [4]float32  // xyz, pad
	Color               [4]float32  // rgb, intensity
	Params              [4]float32  // range, cone_angle_cos, type, casts_shadows (0.0 or 1.0)
	ShadowMeta          [4]uint32   // x: first shadow layer, y: shadow layer count, z: directional cascade count, w: emitter link id
	ViewProj            [16]float32 // Spot light shadow matrix
	InvViewProj         [16]float32 // Spot light inverse shadow matrix
	DirectionalCascades [DirectionalShadowCascadeCount]DirectionalShadowCascade
}

type ShadowUpdate struct {
	LightIndex   uint32
	ShadowLayer  uint32
	CascadeIndex uint32
	Kind         uint32
	Tier         uint32
	Resolution   uint32
}
