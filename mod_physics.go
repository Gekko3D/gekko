package gekko

import rootphysics "github.com/gekko3d/gekko/physics"

type ColliderShape = rootphysics.ColliderShape

const (
	ShapeBox    = rootphysics.ShapeBox
	ShapeSphere = rootphysics.ShapeSphere
)

type RigidBodyComponent = rootphysics.RigidBodyComponent
type CollisionBox = rootphysics.CollisionBox
type ColliderComponent = rootphysics.ColliderComponent
type PhysicsModel = rootphysics.PhysicsModel
type PhysicsWorld = rootphysics.PhysicsWorld

func NewPhysicsWorld() *PhysicsWorld {
	return rootphysics.NewPhysicsWorld()
}
