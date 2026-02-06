package core

import "github.com/go-gl/mathgl/mgl32"

type GizmoType int

const (
	GizmoLine GizmoType = iota
	GizmoCube
	GizmoSphere
	GizmoRect
	GizmoCircle
)

// Gizmo represents a debug shape to be drawn.
type Gizmo struct {
	Type        GizmoType
	Color       [4]float32
	ModelMatrix mgl32.Mat4
}
