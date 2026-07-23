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

	// Segments defaults to one. Animated beams use multiple segments and a
	// procedural center-line wave; straight beams pay no fragment cost for the
	// unused segment slots.
	Segments        uint32
	WaveAmplitude   float32
	WaveFrequency   float32 // cycles over the beam length
	WaveSpeed       float32 // cycles per second
	WavePhase       float32 // cycles
	ScrollSpeed     float32 // longitudinal cycles per second
	ScrollStrength  float32 // 0..1 brightness modulation
	PatternPixels   uint32  // samples per longitudinal cycle; zero keeps it smooth
	WidthPixels     uint32  // samples across the beam; zero keeps it smooth
	ScreenPixelSize float32 // screen-space sample block size; zero keeps native resolution
}

func beamsSync(cmd *Commands) []app_rt.BeamInstanceInput {
	return beamsSyncAtTime(cmd, 0)
}

func beamsSyncAtTime(cmd *Commands, elapsed float32) []app_rt.BeamInstanceInput {
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
		segments := beam.Segments
		if segments == 0 {
			segments = 1
		}
		segments = min(segments, app_rt.MaxBeamSegments)
		instances = append(instances, app_rt.BeamInstanceInput{
			StartWidth:      [4]float32{beam.Start.X(), beam.Start.Y(), beam.Start.Z(), beam.Width},
			EndCoreFraction: [4]float32{beam.End.X(), beam.End.Y(), beam.End.Z(), coreFraction},
			CoreColor:       beam.CoreColor,
			HaloColor:       beam.HaloColor,
			Motion:          [4]float32{max(0, beam.WaveAmplitude), beam.WaveFrequency, beam.WaveSpeed, beam.WavePhase},
			Render:          [4]float32{float32(segments), beam.ScrollSpeed, min(1, max(0, beam.ScrollStrength)), elapsed},
			Pixelation:      [4]float32{float32(beam.PatternPixels), float32(beam.WidthPixels), max(0, beam.ScreenPixelSize)},
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
		beam.Width, beam.CoreFraction, beam.WaveAmplitude, beam.WaveFrequency,
		beam.WaveSpeed, beam.WavePhase, beam.ScrollSpeed, beam.ScrollStrength,
		beam.ScreenPixelSize,
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
