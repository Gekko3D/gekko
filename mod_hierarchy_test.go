package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestTransformHierarchy(t *testing.T) {
	app := NewApp()
	app.UseModules(HierarchyModule{})

	cmd := app.Commands()

	// Create Parent
	parent := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{10, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
	)

	// Create Child
	child := cmd.AddEntity(
		&Parent{Entity: parent},
		&LocalTransformComponent{
			Position: mgl32.Vec3{0, 5, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&TransformComponent{},
	)

	// Create Grandchild
	grandchild := cmd.AddEntity(
		&Parent{Entity: child},
		&LocalTransformComponent{
			Position: mgl32.Vec3{0, 0, 2},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&TransformComponent{},
	)

	app.FlushCommands()

	// Run systems manually
	TransformHierarchySystem(cmd)

	// Check results
	var childWorld *TransformComponent
	var grandchildWorld *TransformComponent

	allChild := cmd.GetAllComponents(child)
	for _, c := range allChild {
		if tr, ok := c.(TransformComponent); ok {
			childWorld = &tr
		}
	}

	allGrand := cmd.GetAllComponents(grandchild)
	for _, c := range allGrand {
		if tr, ok := c.(TransformComponent); ok {
			grandchildWorld = &tr
		}
	}

	if childWorld.Position != (mgl32.Vec3{10, 5, 0}) {
		t.Errorf("Child position incorrect: expected (10, 5, 0), got %v", childWorld.Position)
	}

	if grandchildWorld.Position != (mgl32.Vec3{10, 5, 2}) {
		t.Errorf("Grandchild position incorrect: expected (10, 5, 2), got %v", grandchildWorld.Position)
	}

	// Test Rotation propagation
	// Rotate parent 90 deg around Y
	parentTr := &TransformComponent{}
	for _, c := range cmd.GetAllComponents(parent) {
		if tr, ok := c.(TransformComponent); ok {
			*parentTr = tr
		}
	}
	parentTr.Rotation = mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0})
	cmd.AddComponents(parent, *parentTr) // Update component
	app.FlushCommands()

	TransformHierarchySystem(cmd)

	// Refresh world transforms
	for _, c := range cmd.GetAllComponents(child) {
		if tr, ok := c.(TransformComponent); ok {
			childWorld = &tr
		}
	}
	for _, c := range cmd.GetAllComponents(grandchild) {
		if tr, ok := c.(TransformComponent); ok {
			grandchildWorld = &tr
		}
	}

	// Child was at (0, 5, 0) relative to parent. Parent rotated 90 deg.
	// New world pos should be ParentPos + Rot * (0, 5, 0) = (10, 0, 0) + (0, 5, 0) = (10, 5, 0)
	// Wait, rotation around Y doesn't affect (0, 5, 0).
	// Let's use (5, 0, 0) as local pos for rotation test.

	childLocal := &LocalTransformComponent{}
	for _, c := range cmd.GetAllComponents(child) {
		if l, ok := c.(LocalTransformComponent); ok {
			*childLocal = l
		}
	}
	childLocal.Position = mgl32.Vec3{5, 0, 0}
	cmd.AddComponents(child, *childLocal)
	app.FlushCommands()

	TransformHierarchySystem(cmd)

	// Refresh
	for _, c := range cmd.GetAllComponents(child) {
		if tr, ok := c.(TransformComponent); ok {
			childWorld = &tr
		}
	}

	// Parent at (10, 0, 0), Rot 90deg Y. Child local (5, 0, 0).
	// WorldPos = (10, 0, 0) + RotY(90) * (5, 0, 0)
	// RotY(90) * (5, 0, 0) = (0, 0, -5)
	// WorldPos = (10, 0, -5)

	expectedPos := mgl32.Vec3{10, 0, -5}
	if childWorld.Position.Sub(expectedPos).Len() > 0.001 {
		t.Errorf("Child position after rotation incorrect: expected %v, got %v", expectedPos, childWorld.Position)
	}
}
