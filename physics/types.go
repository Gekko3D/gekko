package physics

import "github.com/go-gl/mathgl/mgl32"

type ColliderShape int

const (
	ShapeBox ColliderShape = iota
	ShapeSphere
)

type RigidBodyComponent struct {
	Velocity           mgl32.Vec3
	AngularVelocity    mgl32.Vec3
	Mass               float32
	GravityScale       float32
	LinearDamping      float32
	AngularDamping     float32
	IsStatic           bool
	Sleeping           bool
	IdleTime           float32
	LastPulledPos      mgl32.Vec3
	LastPulledRot      mgl32.Quat
	PreviousPhysicsPos mgl32.Vec3
	PreviousPhysicsRot mgl32.Quat
	CurrentPhysicsPos  mgl32.Vec3
	CurrentPhysicsRot  mgl32.Quat
	LastPhysicsTick    uint64
	AccumulatedImpulse mgl32.Vec3
	AccumulatedTorque  mgl32.Vec3
}

func (rb *RigidBodyComponent) Wake() {
	rb.Sleeping = false
	rb.IdleTime = 0
}

func (rb *RigidBodyComponent) ApplyImpulse(impulse mgl32.Vec3) {
	rb.Wake()
	rb.AccumulatedImpulse = rb.AccumulatedImpulse.Add(impulse)
}

func (rb *RigidBodyComponent) ApplyTorque(torque mgl32.Vec3) {
	rb.Wake()
	rb.AccumulatedTorque = rb.AccumulatedTorque.Add(torque)
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

type VoxelGrid interface {
	GetVoxel(gx, gy, gz int) (bool, uint8)
	GetAABBMin() mgl32.Vec3
	GetAABBMax() mgl32.Vec3
	VoxelSize() float32
}

// PhysicsModel is a generic component that describes the object's physics model.
// It is agnostic of the renderer.
type PhysicsModel struct {
	Boxes        []CollisionBox
	CenterOffset mgl32.Vec3 // Global offset for the whole model (e.g. for AABB pre-calc)
	Grid         VoxelGrid  // Voxel grid for narrow-phase collision
	// KeyPoints will contain corner and edge key-points in future phases.
	KeyPoints []mgl32.Vec3
}
