package gekko

import rootphysics "github.com/gekko3d/gekko/physics"

type ColliderShape = rootphysics.ColliderShape

const (
	ShapeBox     = rootphysics.ShapeBox
	ShapeSphere  = rootphysics.ShapeSphere
	ShapeCapsule = rootphysics.ShapeCapsule
)

const (
	DefaultCollisionLayer uint32 = rootphysics.DefaultCollisionLayer
	AllCollisionLayers    uint32 = rootphysics.AllCollisionLayers
)

type RigidBodyComponent = rootphysics.RigidBodyComponent
type CollisionBox = rootphysics.CollisionBox
type ColliderComponent = rootphysics.ColliderComponent
type PhysicsModel = rootphysics.PhysicsModel
type PhysicsWorld = rootphysics.PhysicsWorld

func NewPhysicsWorld() *PhysicsWorld {
	return rootphysics.NewPhysicsWorld()
}

func dampingRetentionFactor(configured, defaultRetention float32) float32 {
	if configured <= 0 {
		return defaultRetention
	}
	if configured >= 1 {
		return 0
	}
	// Support both common interpretations:
	// - low values such as 0.02 as "2% damping" -> keep 98%
	// - high values such as 0.99 as direct retention multipliers
	if configured < 0.5 {
		return 1 - configured
	}
	return configured
}
