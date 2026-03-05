package physics

import "github.com/go-gl/mathgl/mgl32"

type PhysicsWorld struct {
	Gravity         mgl32.Vec3
	VoxelSize       float32
	SleepThreshold  float32
	SleepTime       float32
	Threads         int
	UpdateFrequency float32 // Hz
}

func NewPhysicsWorld() *PhysicsWorld {
	return &PhysicsWorld{
		Gravity:         mgl32.Vec3{0, -9.81, 0},
		VoxelSize:       0.1,
		SleepThreshold:  0.05,
		SleepTime:       1.0,
		UpdateFrequency: 60.0,
	}
}
