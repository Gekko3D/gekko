package gekko

import (
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/go-gl/mathgl/mgl32"
)

func TestBeamsSyncPacksOnlyDrawableBeams(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&BeamComponent{
		Enabled:         true,
		Start:           mgl32.Vec3{1, 2, 3},
		End:             mgl32.Vec3{4, 5, 6},
		Width:           0.2,
		CoreFraction:    0.4,
		CoreColor:       [4]float32{4, 3, 2, 1},
		HaloColor:       [4]float32{1, 0.5, 0.1, 0.5},
		Segments:        99,
		WaveAmplitude:   0.3,
		WaveFrequency:   2,
		WaveSpeed:       1.5,
		WavePhase:       0.25,
		ScrollSpeed:     -3,
		ScrollStrength:  2,
		PatternPixels:   12,
		WidthPixels:     7,
		ScreenPixelSize: 3,
	})
	cmd.AddEntity(&BeamComponent{
		Enabled:   true,
		Start:     mgl32.Vec3{1, 1, 1},
		End:       mgl32.Vec3{1, 1, 1},
		Width:     0.2,
		CoreColor: [4]float32{1, 1, 1, 1},
	})
	cmd.AddEntity(&BeamComponent{
		Enabled:   true,
		Start:     mgl32.Vec3{},
		End:       mgl32.Vec3{1, 0, 0},
		CoreColor: [4]float32{1, 1, 1, 1},
	})
	app.FlushCommands()

	instances := beamsSyncAtTime(cmd, 12.5)
	if len(instances) != 1 {
		t.Fatalf("beam instances = %d, want 1", len(instances))
	}
	got := instances[0]
	if got.StartWidth != [4]float32{1, 2, 3, 0.2} ||
		got.EndCoreFraction != [4]float32{4, 5, 6, 0.4} ||
		got.CoreColor != [4]float32{4, 3, 2, 1} ||
		got.HaloColor != [4]float32{1, 0.5, 0.1, 0.5} ||
		got.Motion != [4]float32{0.3, 2, 1.5, 0.25} ||
		got.Render != [4]float32{float32(app_rt.MaxBeamSegments), -3, 1, 12.5} ||
		got.Pixelation != [4]float32{12, 7, 3, 0} {
		t.Fatalf("packed beam = %+v", got)
	}
}
