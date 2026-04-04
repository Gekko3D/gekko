package gekko

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestCelestialMotionPositionUsesOrbitPhase(t *testing.T) {
	pos, ok := CelestialMotionPosition(CelestialMotionComponent{
		OrbitCenter: mgl32.Vec3{4, -2, 7},
		OrbitAxis:   mgl32.Vec3{0, 1, 0},
		OrbitRadius: 10,
		OrbitPhase:  0,
	})
	if !ok {
		t.Fatal("expected orbit position to be computed")
	}
	want := mgl32.Vec3{14, -2, 7}
	if pos.Sub(want).Len() > 0.001 {
		t.Fatalf("unexpected orbit position %v, want %v", pos, want)
	}
}

func TestCelestialMotionRotationAppliesTiltAndSpin(t *testing.T) {
	rot, ok := CelestialMotionRotation(CelestialMotionComponent{
		SelfAxis:      mgl32.Vec3{0, 1, 0},
		SelfPhase:     float32(math.Pi * 0.5),
		AxialTiltDeg:  20,
		AxialTiltAxis: mgl32.Vec3{0, 0, 1},
	})
	if !ok {
		t.Fatal("expected celestial rotation to be computed")
	}
	forward := rot.Rotate(mgl32.Vec3{0, 0, -1}).Normalize()
	if math.Abs(float64(forward.X()+0.9396926)) > 0.02 {
		t.Fatalf("unexpected tilted spin forward vector %v", forward)
	}
	if math.Abs(float64(forward.Y()+0.34202015)) > 0.02 {
		t.Fatalf("unexpected tilted spin forward vector %v", forward)
	}
}

func TestCelestialMotionSystemAdvancesOrbitAndSpin(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	eid := cmd.AddEntity(
		&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&CelestialMotionComponent{
			OrbitAxis:         mgl32.Vec3{0, 1, 0},
			OrbitRadius:       12,
			OrbitAngularSpeed: float32(math.Pi),
			SelfAxis:          mgl32.Vec3{0, 1, 0},
			SelfAngularSpeed:  float32(math.Pi),
		},
	)
	app.FlushCommands()

	celestialMotionSystem(&Time{Dt: 0.5}, cmd)

	var tr *TransformComponent
	var motion *CelestialMotionComponent
	MakeQuery2[TransformComponent, CelestialMotionComponent](cmd).Map(func(id EntityId, gotTr *TransformComponent, gotMotion *CelestialMotionComponent) bool {
		if id == eid {
			tr = gotTr
			motion = gotMotion
			return false
		}
		return true
	})
	if tr == nil || motion == nil {
		t.Fatal("expected transform and motion components")
	}
	if math.Abs(float64(motion.OrbitPhase-float32(math.Pi*0.5))) > 0.001 {
		t.Fatalf("expected orbit phase pi/2, got %v", motion.OrbitPhase)
	}
	if math.Abs(float64(tr.Position.Z()+12)) > 0.02 {
		t.Fatalf("expected orbit position near -Z hemisphere, got %v", tr.Position)
	}
	forward := tr.Rotation.Rotate(mgl32.Vec3{0, 0, -1})
	if math.Abs(float64(forward.X()+1.0)) > 0.02 {
		t.Fatalf("expected self rotation to face -X, got %v", forward)
	}
}

func TestCelestialMotionSystemOrbitsAroundTargetEntity(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	center := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{5, -3, 8}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
	)
	orbiter := cmd.AddEntity(
		&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&CelestialMotionComponent{
			OrbitAroundEntity: true,
			OrbitCenterEntity: center,
			OrbitAxis:         mgl32.Vec3{0, 1, 0},
			OrbitRadius:       6,
			OrbitPhase:        0,
		},
	)
	app.FlushCommands()

	celestialMotionSystem(&Time{}, cmd)

	var tr *TransformComponent
	MakeQuery1[TransformComponent](cmd).Map(func(id EntityId, gotTr *TransformComponent) bool {
		if id == orbiter {
			tr = gotTr
			return false
		}
		return true
	})
	if tr == nil {
		t.Fatal("expected orbiter transform")
	}
	want := mgl32.Vec3{11, -3, 8}
	if tr.Position.Sub(want).Len() > 0.001 {
		t.Fatalf("unexpected orbiter position %v, want %v", tr.Position, want)
	}
}
