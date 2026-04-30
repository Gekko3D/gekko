package gekko

import (
	"math"
	"sort"

	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

const DefaultAstronomicalMaxRenderedBodies = 64

type AstronomicalVisualKind uint32

const (
	AstronomicalVisualUnknown AstronomicalVisualKind = iota
	AstronomicalVisualStar
	AstronomicalVisualRockyPlanet
	AstronomicalVisualGasGiant
	AstronomicalVisualMoon
	AstronomicalVisualRingOrBelt
)

type AstronomicalVisualRecord struct {
	BodyID                    string
	Kind                      AstronomicalVisualKind
	DirectionViewSpace        [3]float32
	AngularRadiusRad          float32
	DistanceMeters            float64
	Seed                      uint32
	VisualProfile             string
	BodyTint                  [3]float32
	EmissionStrength          float32
	GlowAngularRadiusRad      float32
	RingInnerAngularRadiusRad float32
	RingOuterAngularRadiusRad float32
	PhaseLight01              float32
	OcclusionPriority         int
}

type AstronomicalVisualComponent struct {
	Disabled          bool
	MaxRenderedBodies int
	Visuals           []AstronomicalVisualRecord
}

func (c *AstronomicalVisualComponent) Enabled() bool {
	return c != nil && !c.Disabled
}

func normalizedAstronomicalMaxRenderedBodies(maxRendered int) int {
	if maxRendered <= 0 {
		return DefaultAstronomicalMaxRenderedBodies
	}
	return maxRendered
}

type astronomicalVisualCandidate struct {
	entityID EntityId
	visual   AstronomicalVisualRecord
}

func buildAstronomicalBodyHosts(cmd *Commands) []gpu_rt.AstronomicalBodyHost {
	hosts := make([]gpu_rt.AstronomicalBodyHost, 0, DefaultAstronomicalMaxRenderedBodies)
	if cmd == nil {
		return hosts
	}

	maxRendered := 0
	candidates := make([]astronomicalVisualCandidate, 0, DefaultAstronomicalMaxRenderedBodies)
	MakeQuery1[AstronomicalVisualComponent](cmd).Map(func(eid EntityId, component *AstronomicalVisualComponent) bool {
		if !component.Enabled() {
			return true
		}
		componentMax := normalizedAstronomicalMaxRenderedBodies(component.MaxRenderedBodies)
		if maxRendered == 0 || componentMax < maxRendered {
			maxRendered = componentMax
		}
		for _, visual := range component.Visuals {
			if !astronomicalVisualRecordRenderable(visual) {
				continue
			}
			candidates = append(candidates, astronomicalVisualCandidate{entityID: eid, visual: visual})
		}
		return true
	})

	sortAstronomicalVisualCandidatesForRender(candidates)

	if maxRendered <= 0 {
		maxRendered = DefaultAstronomicalMaxRenderedBodies
	}
	if maxRendered > gpu_rt.MaxAstronomicalBodies {
		maxRendered = gpu_rt.MaxAstronomicalBodies
	}
	candidates = limitAstronomicalVisualCandidatesForRender(candidates, maxRendered)

	hosts = make([]gpu_rt.AstronomicalBodyHost, 0, len(candidates))
	for _, candidate := range candidates {
		visual := candidate.visual
		dir := mgl32.Vec3{
			visual.DirectionViewSpace[0],
			visual.DirectionViewSpace[1],
			visual.DirectionViewSpace[2],
		}
		if dir.LenSqr() > 1e-6 {
			dir = dir.Normalize()
		}
		hosts = append(hosts, gpu_rt.AstronomicalBodyHost{
			Kind:                      uint32(visual.Kind),
			DirectionViewSpace:        dir,
			AngularRadiusRad:          visual.AngularRadiusRad,
			GlowAngularRadiusRad:      visual.GlowAngularRadiusRad,
			RingInnerAngularRadiusRad: visual.RingInnerAngularRadiusRad,
			RingOuterAngularRadiusRad: visual.RingOuterAngularRadiusRad,
			PhaseLight01:              visual.PhaseLight01,
			BodyTint:                  visual.BodyTint,
			EmissionStrength:          visual.EmissionStrength,
			Seed:                      visual.Seed,
			OcclusionPriority:         int32(visual.OcclusionPriority),
		})
	}
	return hosts
}

func sortAstronomicalVisualCandidatesForRender(candidates []astronomicalVisualCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		vi := candidates[i].visual
		vj := candidates[j].visual
		if vi.AngularRadiusRad != vj.AngularRadiusRad {
			return vi.AngularRadiusRad > vj.AngularRadiusRad
		}
		if vi.DistanceMeters != vj.DistanceMeters {
			return vi.DistanceMeters < vj.DistanceMeters
		}
		if vi.BodyID != vj.BodyID {
			return vi.BodyID < vj.BodyID
		}
		return candidates[i].entityID < candidates[j].entityID
	})
}

func limitAstronomicalVisualCandidatesForRender(candidates []astronomicalVisualCandidate, maxRendered int) []astronomicalVisualCandidate {
	if maxRendered <= 0 || len(candidates) <= maxRendered {
		return candidates
	}
	for i := 0; i < maxRendered; i++ {
		if candidates[i].visual.OcclusionPriority >= 200 {
			return candidates[:maxRendered]
		}
	}
	selectedIndex := -1
	for i := maxRendered; i < len(candidates); i++ {
		if candidates[i].visual.OcclusionPriority >= 200 && (selectedIndex < 0 || candidates[i].visual.BodyID < candidates[selectedIndex].visual.BodyID) {
			selectedIndex = i
		}
	}
	if selectedIndex >= 0 {
		candidates[maxRendered-1] = candidates[selectedIndex]
		sortAstronomicalVisualCandidatesForRender(candidates[:maxRendered])
	}
	return candidates[:maxRendered]
}

func astronomicalVisualRecordRenderable(visual AstronomicalVisualRecord) bool {
	if visual.AngularRadiusRad < 0 || !isFiniteFloat32(visual.AngularRadiusRad) {
		return false
	}
	if visual.AngularRadiusRad == 0 && visual.RingOuterAngularRadiusRad <= 0 {
		return false
	}
	if !isFiniteFloat64(visual.DistanceMeters) || visual.DistanceMeters < 0 {
		return false
	}
	for _, component := range visual.DirectionViewSpace {
		if !isFiniteFloat32(component) {
			return false
		}
	}
	dir := mgl32.Vec3{
		visual.DirectionViewSpace[0],
		visual.DirectionViewSpace[1],
		visual.DirectionViewSpace[2],
	}
	return dir.LenSqr() > 1e-8
}

func isFiniteFloat32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

func isFiniteFloat64(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
