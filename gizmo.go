package gekko

import "github.com/go-gl/mathgl/mgl32"

type GizmoType int

const (
	GizmoLine GizmoType = iota
	GizmoCube
	GizmoSphere
	GizmoRect   // Wireframe rectangle
	GizmoCircle // Wireframe circle
)

// GizmoComponent allows an entity to be visualized as a 3D gizmo.
// Gizmos are rendered as wireframes.
type GizmoComponent struct {
	Type  GizmoType
	Color [4]float32

	// Specifics
	LineEnd mgl32.Vec3 // For GizmoLine, defines the end point in Local space.
	Radius  float32    // For Sphere/Circle. If Scale is used, Radius is a multiplier.
}

func NewGizmoLine(start, end mgl32.Vec3, color [4]float32) GizmoComponent {
	return GizmoComponent{
		Type:    GizmoLine,
		LineEnd: end, // For GizmoLine, start is implicit (0,0,0) in local space, but we keep LineEnd as a local vector
		// Actually, let's keep it flexible. If we have a Line, P1=Position, P2=LineEnd.
		// If we use TransformComponent, Position is the start.
		Color: color,
	}
}

func NewGizmoCube(color [4]float32) GizmoComponent {
	return GizmoComponent{
		Type:  GizmoCube,
		Color: color,
	}
}

func NewGizmoSphere(radius float32, color [4]float32) GizmoComponent {
	return GizmoComponent{
		Type:   GizmoSphere,
		Radius: radius,
		Color:  color,
	}
}
