package physics

import "github.com/go-gl/mathgl/mgl32"

type PhysicsWorld struct {
	Gravity                  mgl32.Vec3
	SleepThreshold           float32
	SleepTime                float32
	Threads                  int
	UpdateFrequency          float32 // Hz
	CollisionSlop            float32
	VelocityZeroThreshold    float32
	WakeThreshold            float32
	RestitutionThreshold     float32
	SpatialGridCellSize      float32
	PointInOBBEpsilon        float32
	PositionCorrection       float32
	GroundedAngularThreshold float32
	GroundedSleepTime        float32
	SolverIterations         int
}

func NewPhysicsWorld() *PhysicsWorld {
	return &PhysicsWorld{
		Gravity:                  mgl32.Vec3{0, -9.81, 0},
		SleepThreshold:           0.05,
		SleepTime:                1.0,
		UpdateFrequency:          60.0,
		CollisionSlop:            0.02,
		VelocityZeroThreshold:    0.01,
		WakeThreshold:            0.1,
		RestitutionThreshold:     -0.5,
		SpatialGridCellSize:      10.0,
		PointInOBBEpsilon:        0.01,
		PositionCorrection:       0.2,
		GroundedAngularThreshold: 0.1,
		GroundedSleepTime:        0.25,
		SolverIterations:         12,
	}
}
