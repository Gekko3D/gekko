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

	// Common Transform (used if Entity doesn't have TransformComponent, or as local modifier)
	// For Cube, Sphere, Rect, Circle: Position is center. Scale dimensions.
	// For Line: Position is Start.
	Position mgl32.Vec3
	Rotation mgl32.Quat
	Scale    mgl32.Vec3 // Default {1,1,1}

	// Specifics
	LineEnd mgl32.Vec3 // For GizmoLine, defines the end point in World space (if no parent) or Local space.
	Radius  float32    // For Sphere/Circle. If Scale is used, Radius is a multiplier.
}

func NewGizmoLine(start, end mgl32.Vec3, color [4]float32) GizmoComponent {
	return GizmoComponent{
		Type:     GizmoLine,
		Position: start,
		LineEnd:  end,
		Color:    color,
		Scale:    mgl32.Vec3{1, 1, 1},
		Rotation: mgl32.QuatIdent(),
	}
}

func NewGizmoCube(center mgl32.Vec3, size mgl32.Vec3, color [4]float32) GizmoComponent {
	return GizmoComponent{
		Type:     GizmoCube,
		Position: center,
		Scale:    size,
		Color:    color,
		Rotation: mgl32.QuatIdent(),
	}
}

func NewGizmoSphere(center mgl32.Vec3, radius float32, color [4]float32) GizmoComponent {
	return GizmoComponent{
		Type:     GizmoSphere,
		Position: center,
		Radius:   radius,
		Scale:    mgl32.Vec3{1, 1, 1},
		Color:    color,
		Rotation: mgl32.QuatIdent(),
	}
}
