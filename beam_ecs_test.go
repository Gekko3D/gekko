package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestBeamsSyncPacksOnlyDrawableBeams(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&BeamComponent{
		Enabled:      true,
		Start:        mgl32.Vec3{1, 2, 3},
		End:          mgl32.Vec3{4, 5, 6},
		Width:        0.2,
		CoreFraction: 0.4,
		CoreColor:    [4]float32{4, 3, 2, 1},
		HaloColor:    [4]float32{1, 0.5, 0.1, 0.5},
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

	instances := beamsSync(cmd)
	if len(instances) != 1 {
		t.Fatalf("beam instances = %d, want 1", len(instances))
	}
	got := instances[0]
	if got.StartWidth != [4]float32{1, 2, 3, 0.2} ||
		got.EndCoreFraction != [4]float32{4, 5, 6, 0.4} ||
		got.CoreColor != [4]float32{4, 3, 2, 1} ||
		got.HaloColor != [4]float32{1, 0.5, 0.1, 0.5} {
		t.Fatalf("packed beam = %+v", got)
	}
}
