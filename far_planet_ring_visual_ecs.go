package gekko

import (
	"sort"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	FarPlanetRingRadialProfileSampleCount = 32
	DefaultFarPlanetRingMaxRendered       = gpu_rt.MaxFarPlanetRings

	DefaultFarPlanetRingDustHazeMaxAlpha               = float32(0.16)
	DefaultFarPlanetRingDustHazeThicknessScale         = float32(80)
	DefaultFarPlanetRingDustHazeMinHalfThicknessMeters = float32(40000)
	DefaultFarPlanetRingDustHazeRadialEdgeFadeFraction = float32(0.035)
	DefaultFarPlanetRingDustHazeVerticalCoreFraction   = float32(0.18)
	DefaultFarPlanetRingDustHazeSampleCount            = float32(5)
	DefaultFarPlanetRingDustHazeForwardScatterStrength = float32(0.35)
	DefaultFarPlanetRingDustHazeShadowStrength         = float32(0.45)
)

type FarPlanetRingVisualRecord struct {
	BandID                           string
	ParentBodyID                     string
	CenterCameraRelativeMeters       [3]float32
	NormalCameraRelative             [3]float32
	TangentUCameraRelative           [3]float32
	TangentVCameraRelative           [3]float32
	InnerRadiusMeters                float32
	OuterRadiusMeters                float32
	HalfThicknessMeters              float32
	Tint                             [3]float32
	Opacity                          float32
	DustHazeOpacity                  float32
	DustHazeMaxAlpha                 float32
	DustHazeThicknessScale           float32
	DustHazeMinHalfThicknessMeters   float32
	DustHazeRadialEdgeFadeFraction   float32
	DustHazeVerticalCoreFraction     float32
	DustHazeSampleCount              float32
	DustHazeForwardScatterStrength   float32
	DustHazeShadowStrength           float32
	Seed                             uint32
	RadialOpacityProfile             [FarPlanetRingRadialProfileSampleCount]float32
	ParentCenterCameraRelativeMeters [3]float32
	ParentRadiusMeters               float32
	ParentDepthMeters                float32
	LightDirectionViewSpace          [3]float32
}

type FarPlanetRingVisualComponent struct {
	Disabled         bool
	MaxRenderedRings int
	Rings            []FarPlanetRingVisualRecord
}

func (c *FarPlanetRingVisualComponent) Enabled() bool {
	return c != nil && !c.Disabled
}

type farPlanetRingCandidate struct {
	entityID EntityId
	record   FarPlanetRingVisualRecord
}

func buildFarPlanetRingInputs(cmd *Commands) []app_rt.FarPlanetRingInput {
	if cmd == nil {
		return nil
	}
	maxRendered := 0
	candidates := make([]farPlanetRingCandidate, 0)
	MakeQuery1[FarPlanetRingVisualComponent](cmd).Map(func(eid EntityId, component *FarPlanetRingVisualComponent) bool {
		if !component.Enabled() {
			return true
		}
		componentMax := component.MaxRenderedRings
		if componentMax <= 0 {
			componentMax = DefaultFarPlanetRingMaxRendered
		}
		if maxRendered == 0 || componentMax < maxRendered {
			maxRendered = componentMax
		}
		for _, record := range component.Rings {
			if farPlanetRingRecordRenderable(record) {
				candidates = append(candidates, farPlanetRingCandidate{entityID: eid, record: record})
			}
		}
		return true
	})
	sortFarPlanetRingCandidates(candidates)
	if maxRendered <= 0 {
		maxRendered = DefaultFarPlanetRingMaxRendered
	}
	if maxRendered > gpu_rt.MaxFarPlanetRings {
		maxRendered = gpu_rt.MaxFarPlanetRings
	}
	if len(candidates) > maxRendered {
		candidates = candidates[:maxRendered]
	}
	inputs := make([]app_rt.FarPlanetRingInput, 0, len(candidates))
	for _, candidate := range candidates {
		r := candidate.record
		haze := farPlanetRingDustHazeSettingsWithDefaults(r)
		inputs = append(inputs, app_rt.FarPlanetRingInput{
			BandID:                           r.BandID,
			ParentBodyID:                     r.ParentBodyID,
			CenterCameraRelativeMeters:       mgl32.Vec3{r.CenterCameraRelativeMeters[0], r.CenterCameraRelativeMeters[1], r.CenterCameraRelativeMeters[2]},
			NormalCameraRelative:             normalizedOr(r.NormalCameraRelative, mgl32.Vec3{0, 1, 0}),
			TangentUCameraRelative:           normalizedOr(r.TangentUCameraRelative, mgl32.Vec3{1, 0, 0}),
			TangentVCameraRelative:           normalizedOr(r.TangentVCameraRelative, mgl32.Vec3{0, 0, -1}),
			InnerRadiusMeters:                r.InnerRadiusMeters,
			OuterRadiusMeters:                r.OuterRadiusMeters,
			HalfThicknessMeters:              r.HalfThicknessMeters,
			Tint:                             r.Tint,
			Opacity:                          r.Opacity,
			DustHazeOpacity:                  r.DustHazeOpacity,
			DustHazeMaxAlpha:                 haze.maxAlpha,
			DustHazeThicknessScale:           haze.thicknessScale,
			DustHazeMinHalfThicknessMeters:   haze.minHalfThicknessMeters,
			DustHazeRadialEdgeFadeFraction:   haze.radialEdgeFadeFraction,
			DustHazeVerticalCoreFraction:     haze.verticalCoreFraction,
			DustHazeSampleCount:              haze.sampleCount,
			DustHazeForwardScatterStrength:   haze.forwardScatterStrength,
			DustHazeShadowStrength:           haze.shadowStrength,
			Seed:                             r.Seed,
			RadialOpacityProfile:             r.RadialOpacityProfile,
			ParentCenterCameraRelativeMeters: mgl32.Vec3{r.ParentCenterCameraRelativeMeters[0], r.ParentCenterCameraRelativeMeters[1], r.ParentCenterCameraRelativeMeters[2]},
			ParentRadiusMeters:               r.ParentRadiusMeters,
			ParentDepthMeters:                r.ParentDepthMeters,
			LightDirectionViewSpace:          normalizedOr(r.LightDirectionViewSpace, mgl32.Vec3{0, 1, 0}),
		})
	}
	return inputs
}

func sortFarPlanetRingCandidates(candidates []farPlanetRingCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a := candidates[i].record
		b := candidates[j].record
		if a.ParentDepthMeters != b.ParentDepthMeters {
			return a.ParentDepthMeters < b.ParentDepthMeters
		}
		if a.ParentBodyID != b.ParentBodyID {
			return a.ParentBodyID < b.ParentBodyID
		}
		if a.BandID != b.BandID {
			return a.BandID < b.BandID
		}
		return candidates[i].entityID < candidates[j].entityID
	})
}

func farPlanetRingRecordRenderable(record FarPlanetRingVisualRecord) bool {
	if record.BandID == "" || record.ParentBodyID == "" {
		return false
	}
	if record.InnerRadiusMeters < 0 || record.OuterRadiusMeters <= record.InnerRadiusMeters || record.HalfThicknessMeters < 0 {
		return false
	}
	if record.ParentRadiusMeters < 0 || !isFiniteFloat32(record.ParentDepthMeters) {
		return false
	}
	if record.Opacity < 0 || record.DustHazeOpacity < 0 || !isFiniteFloat32(record.Opacity) || !isFiniteFloat32(record.DustHazeOpacity) {
		return false
	}
	if !farPlanetRingDustHazeSettingsFinite(record) {
		return false
	}
	for _, values := range [][]float32{
		record.CenterCameraRelativeMeters[:],
		record.NormalCameraRelative[:],
		record.TangentUCameraRelative[:],
		record.TangentVCameraRelative[:],
		record.Tint[:],
		record.RadialOpacityProfile[:],
		record.ParentCenterCameraRelativeMeters[:],
		record.LightDirectionViewSpace[:],
	} {
		for _, value := range values {
			if !isFiniteFloat32(value) {
				return false
			}
		}
	}
	if vectorLenSqr(record.NormalCameraRelative) <= 1e-8 ||
		vectorLenSqr(record.TangentUCameraRelative) <= 1e-8 ||
		vectorLenSqr(record.TangentVCameraRelative) <= 1e-8 {
		return false
	}
	return isFiniteFloat32(record.InnerRadiusMeters) &&
		isFiniteFloat32(record.OuterRadiusMeters) &&
		isFiniteFloat32(record.HalfThicknessMeters) &&
		isFiniteFloat32(record.ParentRadiusMeters)
}

type farPlanetRingDustHazeSettings struct {
	maxAlpha               float32
	thicknessScale         float32
	minHalfThicknessMeters float32
	radialEdgeFadeFraction float32
	verticalCoreFraction   float32
	sampleCount            float32
	forwardScatterStrength float32
	shadowStrength         float32
}

func farPlanetRingDustHazeSettingsWithDefaults(record FarPlanetRingVisualRecord) farPlanetRingDustHazeSettings {
	settings := farPlanetRingDustHazeSettings{
		maxAlpha:               record.DustHazeMaxAlpha,
		thicknessScale:         record.DustHazeThicknessScale,
		minHalfThicknessMeters: record.DustHazeMinHalfThicknessMeters,
		radialEdgeFadeFraction: record.DustHazeRadialEdgeFadeFraction,
		verticalCoreFraction:   record.DustHazeVerticalCoreFraction,
		sampleCount:            record.DustHazeSampleCount,
		forwardScatterStrength: record.DustHazeForwardScatterStrength,
		shadowStrength:         record.DustHazeShadowStrength,
	}
	if settings.maxAlpha <= 0 {
		settings.maxAlpha = DefaultFarPlanetRingDustHazeMaxAlpha
	}
	if settings.thicknessScale <= 0 {
		settings.thicknessScale = DefaultFarPlanetRingDustHazeThicknessScale
	}
	if settings.minHalfThicknessMeters <= 0 {
		settings.minHalfThicknessMeters = DefaultFarPlanetRingDustHazeMinHalfThicknessMeters
	}
	if settings.radialEdgeFadeFraction <= 0 {
		settings.radialEdgeFadeFraction = DefaultFarPlanetRingDustHazeRadialEdgeFadeFraction
	}
	if settings.verticalCoreFraction <= 0 {
		settings.verticalCoreFraction = DefaultFarPlanetRingDustHazeVerticalCoreFraction
	}
	if settings.sampleCount <= 0 {
		settings.sampleCount = DefaultFarPlanetRingDustHazeSampleCount
	}
	if settings.forwardScatterStrength <= 0 {
		settings.forwardScatterStrength = DefaultFarPlanetRingDustHazeForwardScatterStrength
	}
	if settings.shadowStrength <= 0 {
		settings.shadowStrength = DefaultFarPlanetRingDustHazeShadowStrength
	}
	return settings
}

func farPlanetRingDustHazeSettingsFinite(record FarPlanetRingVisualRecord) bool {
	for _, value := range []float32{
		record.DustHazeMaxAlpha,
		record.DustHazeThicknessScale,
		record.DustHazeMinHalfThicknessMeters,
		record.DustHazeRadialEdgeFadeFraction,
		record.DustHazeVerticalCoreFraction,
		record.DustHazeSampleCount,
		record.DustHazeForwardScatterStrength,
		record.DustHazeShadowStrength,
	} {
		if !isFiniteFloat32(value) {
			return false
		}
	}
	return true
}

func normalizedOr(v [3]float32, fallback mgl32.Vec3) mgl32.Vec3 {
	vec := mgl32.Vec3{v[0], v[1], v[2]}
	if vec.LenSqr() <= 1e-8 {
		return fallback
	}
	return vec.Normalize()
}

func vectorLenSqr(v [3]float32) float32 {
	return v[0]*v[0] + v[1]*v[1] + v[2]*v[2]
}
