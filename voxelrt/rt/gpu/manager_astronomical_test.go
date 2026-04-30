package gpu

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestBuildAstronomicalBodyRecordsPreservesStableLayout(t *testing.T) {
	records, params := buildAstronomicalBodyRecords([]AstronomicalBodyHost{
		{
			Kind:                      3,
			DirectionViewSpace:        mgl32.Vec3{0, 0, -2},
			AngularRadiusRad:          0.01,
			GlowAngularRadiusRad:      0.05,
			RingInnerAngularRadiusRad: 0.02,
			RingOuterAngularRadiusRad: 0.04,
			PhaseLight01:              0.25,
			BodyTint:                  [3]float32{0.2, 0.4, 0.8},
			EmissionStrength:          1.5,
			Seed:                      77,
			OcclusionPriority:         9,
		},
	})

	if params.BodyCount != 1 {
		t.Fatalf("expected body count 1, got %d", params.BodyCount)
	}
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}
	rec := records[0]
	if rec.DirectionKind != [4]float32{0, 0, -1, 3} {
		t.Fatalf("unexpected direction/kind packing: %+v", rec.DirectionKind)
	}
	if rec.Angular != [4]float32{0.01, 0.05, 0.02, 0.04} {
		t.Fatalf("unexpected angular packing: %+v", rec.Angular)
	}
	if rec.TintEmission != [4]float32{0.2, 0.4, 0.8, 1.5} {
		t.Fatalf("unexpected tint/emission packing: %+v", rec.TintEmission)
	}
	if rec.Meta[0] != 77 || rec.Meta[1] != 9 || rec.Meta[2] == 0 {
		t.Fatalf("unexpected metadata packing: %+v", rec.Meta)
	}
}

func TestBuildAstronomicalBodyRecordsEnforcesMaxCount(t *testing.T) {
	hosts := make([]AstronomicalBodyHost, MaxAstronomicalBodies+8)
	for i := range hosts {
		hosts[i] = AstronomicalBodyHost{
			Kind:               uint32(i),
			DirectionViewSpace: mgl32.Vec3{0, 0, -1},
			AngularRadiusRad:   0.01,
		}
	}

	records, params := buildAstronomicalBodyRecords(hosts)
	if params.BodyCount != MaxAstronomicalBodies {
		t.Fatalf("expected clamped body count %d, got %d", MaxAstronomicalBodies, params.BodyCount)
	}
	if len(records) != MaxAstronomicalBodies {
		t.Fatalf("expected clamped record count %d, got %d", MaxAstronomicalBodies, len(records))
	}
	if records[len(records)-1].DirectionKind[3] != float32(MaxAstronomicalBodies-1) {
		t.Fatalf("expected deterministic first-%d retention, got last kind %f", MaxAstronomicalBodies, records[len(records)-1].DirectionKind[3])
	}
}
