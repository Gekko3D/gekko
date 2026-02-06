package gekko

type GizmoType int

const (
	GizmoLine GizmoType = iota
	GizmoCube
	GizmoSphere
	GizmoRect   // Wireframe rectangle
	GizmoCircle // Wireframe circle
	GizmoGrid   // Wireframe grid
)

// GizmoComponent allows an entity to be visualized as a 3D gizmo.
// Gizmos are rendered as wireframes.
type GizmoComponent struct {
	Type  GizmoType
	Color [4]float32
	Size  float32
	Steps int // For GizmoGrid: number of subdivisions
}

func NewGizmoLine(size float32, color [4]float32) GizmoComponent {
	return GizmoComponent{
		Type:  GizmoLine,
		Size:  size,
		Color: color,
	}
}

func NewGizmoGrid(size float32, steps int, color [4]float32) GizmoComponent {
	return GizmoComponent{
		Type:  GizmoGrid,
		Size:  size,
		Steps: steps,
		Color: color,
	}
}

func NewGizmoCube(size float32, color [4]float32) GizmoComponent {
	return GizmoComponent{
		Type:  GizmoCube,
		Size:  size,
		Color: color,
	}
}

func NewGizmoSphere(radius float32, color [4]float32) GizmoComponent {
	return GizmoComponent{
		Type:  GizmoSphere,
		Size:  radius,
		Color: color,
	}
}
