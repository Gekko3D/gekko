package gekko

import "testing"

func TestBuildAstronomicalBodyHostsSortsAndLimitsDeterministically(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&AstronomicalVisualComponent{
		MaxRenderedBodies: 2,
		Visuals: []AstronomicalVisualRecord{
			{
				BodyID:             "far-large",
				Kind:               AstronomicalVisualRockyPlanet,
				DirectionViewSpace: [3]float32{0, 0, -1},
				AngularRadiusRad:   0.3,
				DistanceMeters:     200,
				BodyTint:           [3]float32{0.1, 0.2, 0.3},
				PhaseLight01:       0.5,
			},
			{
				BodyID:             "near-large",
				Kind:               AstronomicalVisualStar,
				DirectionViewSpace: [3]float32{0, 0, -2},
				AngularRadiusRad:   0.3,
				DistanceMeters:     100,
				Seed:               99,
				BodyTint:           [3]float32{1, 0.9, 0.7},
				EmissionStrength:   1.5,
			},
			{
				BodyID:             "small",
				Kind:               AstronomicalVisualMoon,
				DirectionViewSpace: [3]float32{0, 0, -1},
				AngularRadiusRad:   0.1,
				DistanceMeters:     50,
				BodyTint:           [3]float32{0.5, 0.5, 0.5},
			},
		},
	})
	app.FlushCommands()

	hosts := buildAstronomicalBodyHosts(cmd)
	if len(hosts) != 2 {
		t.Fatalf("expected two limited hosts, got %d", len(hosts))
	}
	if hosts[0].Kind != uint32(AstronomicalVisualStar) {
		t.Fatalf("expected nearer equal-angular body first, got kind %d", hosts[0].Kind)
	}
	if hosts[1].Kind != uint32(AstronomicalVisualRockyPlanet) {
		t.Fatalf("expected farther equal-angular body second, got kind %d", hosts[1].Kind)
	}
	if hosts[0].DirectionViewSpace[0] != 0 || hosts[0].DirectionViewSpace[1] != 0 || hosts[0].DirectionViewSpace[2] != -1 {
		t.Fatalf("expected normalized view direction, got %v", hosts[0].DirectionViewSpace)
	}
}

func TestBuildAstronomicalBodyHostsKeepsSelectedPriorityWhenLimited(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&AstronomicalVisualComponent{
		MaxRenderedBodies: 2,
		Visuals: []AstronomicalVisualRecord{
			{
				BodyID:             "large-a",
				Kind:               AstronomicalVisualRockyPlanet,
				DirectionViewSpace: [3]float32{0, 0, -1},
				AngularRadiusRad:   0.3,
				DistanceMeters:     100,
				BodyTint:           [3]float32{0.1, 0.2, 0.3},
			},
			{
				BodyID:             "large-b",
				Kind:               AstronomicalVisualGasGiant,
				DirectionViewSpace: [3]float32{0, 0, -1},
				AngularRadiusRad:   0.2,
				DistanceMeters:     100,
				BodyTint:           [3]float32{0.6, 0.5, 0.4},
			},
			{
				BodyID:             "selected-tiny",
				Kind:               AstronomicalVisualMoon,
				DirectionViewSpace: [3]float32{0, 0, -1},
				AngularRadiusRad:   0.001,
				DistanceMeters:     1000,
				Seed:               99,
				BodyTint:           [3]float32{0.5, 0.5, 0.5},
				OcclusionPriority:  200,
			},
		},
	})
	app.FlushCommands()

	hosts := buildAstronomicalBodyHosts(cmd)
	if len(hosts) != 2 {
		t.Fatalf("expected two limited hosts, got %d", len(hosts))
	}
	foundSelected := false
	for _, host := range hosts {
		if host.Seed == 99 && host.OcclusionPriority == 200 {
			foundSelected = true
			break
		}
	}
	if !foundSelected {
		t.Fatalf("expected selected high-priority body to remain in GPU host list, got %#v", hosts)
	}
}

func TestBuildAstronomicalBodyHostsRejectsInvalidRecords(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&AstronomicalVisualComponent{
		Visuals: []AstronomicalVisualRecord{
			{
				BodyID:             "valid",
				Kind:               AstronomicalVisualStar,
				DirectionViewSpace: [3]float32{0, 0, -1},
				AngularRadiusRad:   0.01,
				DistanceMeters:     1000,
				BodyTint:           [3]float32{1, 1, 1},
			},
			{
				BodyID:             "zero-dir",
				Kind:               AstronomicalVisualMoon,
				DirectionViewSpace: [3]float32{},
				AngularRadiusRad:   0.01,
				DistanceMeters:     1000,
			},
			{
				BodyID:             "negative-radius",
				Kind:               AstronomicalVisualMoon,
				DirectionViewSpace: [3]float32{0, 0, -1},
				AngularRadiusRad:   -1,
				DistanceMeters:     1000,
			},
		},
	})
	app.FlushCommands()

	hosts := buildAstronomicalBodyHosts(cmd)
	if len(hosts) != 1 {
		t.Fatalf("expected one valid host, got %d", len(hosts))
	}
	if hosts[0].Kind != uint32(AstronomicalVisualStar) {
		t.Fatalf("expected valid star host, got kind %d", hosts[0].Kind)
	}
}
