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
			DepthMode:   core.GizmoDepthModeAlwaysVisible,
		},
	}

	app.SetGizmoOverlayItems(items)
	items[0].Type = core.GizmoLine
	items[0].Color = [4]float32{0, 1, 0, 1}
	items[0].DepthMode = core.GizmoDepthModeSceneOccluded

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
	if app.Scene.Gizmos[0].DepthMode != core.GizmoDepthModeAlwaysVisible {
		t.Fatalf("expected copied depth mode, got %v", app.Scene.Gizmos[0].DepthMode)
	}
}

func TestAppendGizmoOverlayItemsPreservesExistingGizmos(t *testing.T) {
	app := NewApp(nil)
	app.Scene.Gizmos = []core.Gizmo{{Type: core.GizmoLine, Color: [4]float32{1, 1, 1, 1}}}

	app.AppendGizmoOverlayItems([]GizmoOverlayItem{{
		Type:        core.GizmoSphere,
		Color:       [4]float32{0, 1, 0, 1},
		ModelMatrix: mgl32.Translate3D(1, 2, 3),
	}})

	if got := len(app.Scene.Gizmos); got != 2 {
		t.Fatalf("expected existing and appended gizmos, got %d", got)
	}
	if app.Scene.Gizmos[0].Type != core.GizmoLine || app.Scene.Gizmos[1].Type != core.GizmoSphere {
		t.Fatalf("unexpected gizmo order/types: %+v", app.Scene.Gizmos)
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
