package gekko

import (
	"math"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/go-gl/mathgl/mgl32"
)

// BeamComponent is a camera-facing, depth-aware world-space beam.
type BeamComponent struct {
	Enabled      bool
	Start        mgl32.Vec3
	End          mgl32.Vec3
	Width        float32
	CoreFraction float32
	CoreColor    [4]float32
	HaloColor    [4]float32
}

func beamsSync(cmd *Commands) []app_rt.BeamInstanceInput {
	if cmd == nil {
		return nil
	}
	instances := make([]app_rt.BeamInstanceInput, 0, 8)
	MakeQuery1[BeamComponent](cmd).Map(func(_ EntityId, beam *BeamComponent) bool {
		if !beamDrawable(beam) {
			return true
		}
		coreFraction := beam.CoreFraction
		if coreFraction <= 0 {
			coreFraction = 0.25
		}
		coreFraction = min(1, coreFraction)
		instances = append(instances, app_rt.BeamInstanceInput{
			StartWidth:      [4]float32{beam.Start.X(), beam.Start.Y(), beam.Start.Z(), beam.Width},
			EndCoreFraction: [4]float32{beam.End.X(), beam.End.Y(), beam.End.Z(), coreFraction},
			CoreColor:       beam.CoreColor,
			HaloColor:       beam.HaloColor,
		})
		return true
	})
	return instances
}

func beamDrawable(beam *BeamComponent) bool {
	if beam == nil || !beam.Enabled || beam.Width <= 0 || beam.End.Sub(beam.Start).LenSqr() <= 1e-8 {
		return false
	}
	if beam.CoreColor[3] <= 0 && beam.HaloColor[3] <= 0 {
		return false
	}
	for _, value := range []float32{
		beam.Start.X(), beam.Start.Y(), beam.Start.Z(),
		beam.End.X(), beam.End.Y(), beam.End.Z(),
		beam.Width, beam.CoreFraction,
	} {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return false
		}
	}
	for _, color := range [][4]float32{beam.CoreColor, beam.HaloColor} {
		for _, value := range color {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return false
			}
		}
	}
	return true
}
