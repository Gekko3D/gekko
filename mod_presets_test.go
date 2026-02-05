package gekko

import (
	"os"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestPresetSerialization(t *testing.T) {
	app := NewApp()
	app.UseModules(AssetServerModule{}, HierarchyModule{})
	cmd := app.Commands()

	// Helper to get AssetServer
	getAssetServer := func(a *App) *AssetServer {
		for _, v := range a.resources {
			if as, ok := v.(*AssetServer); ok {
				return as
			}
		}
		return nil
	}
	server := getAssetServer(app)

	// Create a hierarchy
	parent := cmd.AddEntity(&TransformComponent{Position: mgl32.Vec3{1, 2, 3}, Scale: mgl32.Vec3{1, 1, 1}})
	_ = cmd.AddEntity(
		&TransformComponent{Scale: mgl32.Vec3{1, 1, 1}},
		&Parent{Entity: parent},
		&LocalTransformComponent{Position: mgl32.Vec3{0, 1, 0}, Scale: mgl32.Vec3{1, 1, 1}, Rotation: mgl32.QuatIdent()},
	)

	app.FlushCommands()
	TransformHierarchySystem(cmd)

	testFile := "test_preset.json"
	defer os.Remove(testFile)

	// Save
	if err := SavePreset(cmd, server, testFile); err != nil {
		t.Fatalf("Failed to save preset: %v", err)
	}

	// Inspect JSON
	jsonContent, _ := os.ReadFile(testFile)
	t.Logf("Saved JSON:\n%s", string(jsonContent))

	// Clear scene
	app2 := NewApp()
	app2.UseModules(AssetServerModule{}, HierarchyModule{})
	cmd2 := app2.Commands()
	server2 := getAssetServer(app2)

	// Load
	newEntities, err := LoadPreset(cmd2, server2, testFile)
	if err != nil {
		t.Fatalf("Failed to load preset: %v", err)
	}

	if len(newEntities) != 2 {
		t.Errorf("Expected 2 entities loaded, got %d", len(newEntities))
	}

	app2.FlushCommands()
	TransformHierarchySystem(cmd2)

	// Verify hierarchy
	// We expect the new entities to have preserved their relationships
	// Since LoadPreset keeps old ParentID mapping, we should verify it.
	foundParentComp := false
	MakeQuery1[Parent](cmd2).Map(func(eid EntityId, p *Parent) bool {
		foundParentComp = true
		return true
	})

	if !foundParentComp {
		t.Error("Child entity did not have a parent after load")
	}

	// Verify child position (propagated)
	var childWorldPos mgl32.Vec3
	foundChildPos := false
	MakeQuery1[Parent](cmd2).Map(func(eid EntityId, p *Parent) bool {
		// This IS the child
		MakeQuery1[TransformComponent](cmd2).Map(func(teid EntityId, tr *TransformComponent) bool {
			if teid == eid {
				childWorldPos = tr.Position
				foundChildPos = true
			}
			return true
		})
		return true
	})

	if !foundChildPos {
		t.Fatal("Could not find child position")
	}

	expectedPos := mgl32.Vec3{1, 3, 3} // (1,2,3) + (0,1,0)
	if childWorldPos.Sub(expectedPos).Len() > 0.001 {
		t.Errorf("Expected child world position %v, got %v", expectedPos, childWorldPos)
	}
}
