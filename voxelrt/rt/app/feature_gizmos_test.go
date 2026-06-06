package app

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func TestSetGizmoOverlayItemsCopiesItems(t *testing.T) {
	app := NewApp(nil)
	matrix := mgl32.Translate3D(1, 2, 3)
	items := []GizmoOverlayItem{
		{
			Type:        core.GizmoSphere,
			Color:       [4]float32{1, 0, 0, 1},
			ModelMatrix: matrix,
		},
	}

	app.SetGizmoOverlayItems(items)
	items[0].Type = core.GizmoLine
	items[0].Color = [4]float32{0, 1, 0, 1}

	if got := len(app.Scene.Gizmos); got != 1 {
		t.Fatalf("expected one gizmo, got %d", got)
	}
	if app.Scene.Gizmos[0].Type != core.GizmoSphere {
		t.Fatalf("expected copied gizmo type, got %v", app.Scene.Gizmos[0].Type)
	}
	if app.Scene.Gizmos[0].Color != [4]float32{1, 0, 0, 1} {
		t.Fatalf("expected copied gizmo color, got %+v", app.Scene.Gizmos[0].Color)
	}
	if app.Scene.Gizmos[0].ModelMatrix != matrix {
		t.Fatalf("expected copied gizmo matrix")
	}
}

func TestClearGizmoOverlayItemsClearsSceneGizmos(t *testing.T) {
	app := NewApp(nil)
	app.Scene.Gizmos = []core.Gizmo{{Type: core.GizmoLine}}

	app.ClearGizmoOverlayItems()

	if got := len(app.Scene.Gizmos); got != 0 {
		t.Fatalf("expected no gizmos after clear, got %d", got)
	}
}
