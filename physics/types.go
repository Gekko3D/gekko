package physics

import "github.com/go-gl/mathgl/mgl32"

type ColliderShape int

const (
	ShapeBox ColliderShape = iota
	ShapeSphere
)

type RigidBodyComponent struct {
	Velocity        mgl32.Vec3
	AngularVelocity mgl32.Vec3
	Mass            float32
	GravityScale    float32
	IsStatic        bool
	Sleeping        bool
	IdleTime        float32
}

func (rb *RigidBodyComponent) Wake() {
	rb.Sleeping = false
	rb.IdleTime = 0
}

func (rb *RigidBodyComponent) ApplyImpulse(impulse mgl32.Vec3) {
	rb.Wake()
	if rb.Mass > 0 {
		rb.Velocity = rb.Velocity.Add(impulse.Mul(1.0 / rb.Mass))
	} else {
		rb.Velocity = rb.Velocity.Add(impulse)
	}
}

func (rb *RigidBodyComponent) ApplyTorque(torque mgl32.Vec3) {
	rb.Wake()
	// Simplified: no moment of inertia calculation for now, just apply directly
	// In a real engine, we'd scale by inverse inertia tensor.
	rb.AngularVelocity = rb.AngularVelocity.Add(torque.Mul(1.0 / rb.Mass))
}

type CollisionBox struct {
	HalfExtents mgl32.Vec3
	LocalOffset mgl32.Vec3 // Offset relative to the body's local origin
}

type ColliderComponent struct {
	Shape           ColliderShape
	HalfExtents     mgl32.Vec3 // For Box
	Radius          float32    // For Sphere
	Friction        float32
	Restitution     float32
	AABBHalfExtents mgl32.Vec3 // Cached or calculated total half extents
}

// PhysicsModel is a generic component that describes the object's physics model.
// It is agnostic of the renderer.
type PhysicsModel struct {
	Boxes        []CollisionBox
	CenterOffset mgl32.Vec3 // Global offset for the whole model (e.g. for AABB pre-calc)
	// KeyPoints will contain corner and edge key-points in future phases.
	KeyPoints []mgl32.Vec3
}
