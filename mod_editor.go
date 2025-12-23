package gekko

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/editor"
)

// EditorComponent enables voxel editing on entities
type EditorComponent struct {
	Enabled     bool
	BrushRadius float32
	BrushValue  uint8  // Material index to place (0 = erase)
	Mode        string // "add" or "remove"

	// Internal editor instance
	editor *editor.Editor
}

// NewEditorComponent creates a new editor component with default values
func NewEditorComponent() *EditorComponent {
	return &EditorComponent{
		Enabled:     false,
		BrushRadius: 2.0,
		BrushValue:  1,
		Mode:        "add",
		editor:      editor.NewEditor(),
	}
}

// editorSystem handles voxel editing based on mouse input
func editorSystem(cmd *Commands, input *Input, state *VoxelRtState) {
	// Find editor component
	var editorComp *EditorComponent
	MakeQuery1[EditorComponent](cmd).Map(func(eid EntityId, ec *EditorComponent) bool {
		editorComp = ec
		return false // Only process first editor
	})

	if editorComp == nil || !editorComp.Enabled {
		return
	}

	// Update editor settings
	editorComp.editor.BrushRadius = editorComp.BrushRadius
	editorComp.editor.BrushValue = editorComp.BrushValue

	// Handle brush size controls
	if input.JustPressed[KeyEqual] || input.JustPressed[KeyKPPlus] {
		editorComp.BrushRadius += 1.0
		if editorComp.BrushRadius > 10.0 {
			editorComp.BrushRadius = 10.0
		}
	}
	if input.JustPressed[KeyMinus] || input.JustPressed[KeyKPMinus] {
		editorComp.BrushRadius -= 1.0
		if editorComp.BrushRadius < 1.0 {
			editorComp.BrushRadius = 1.0
		}
	}

	// Get camera for raycasting
	var camera *CameraComponent
	MakeQuery1[CameraComponent](cmd).Map(func(eid EntityId, cam *CameraComponent) bool {
		camera = cam
		return false
	})

	if camera == nil {
		return
	}

	// Convert camera to voxelrt camera state
	camState := &core.CameraState{
		Position: camera.Position,
		Yaw:      camera.Yaw,
		Pitch:    camera.Pitch,
	}

	// Handle mouse clicks for editing
	leftClick := input.JustPressed[MouseButtonLeft]
	rightClick := input.JustPressed[MouseButtonRight]

	if !leftClick && !rightClick {
		return
	}

	// Set brush mode
	if leftClick {
		editorComp.Mode = "add"
		editorComp.editor.BrushValue = editorComp.BrushValue
	} else {
		editorComp.Mode = "remove"
		editorComp.editor.BrushValue = 0
	}

	// Get mouse position
	mouseX, mouseY := input.MouseX, input.MouseY
	width, height := input.WindowWidth, input.WindowHeight

	// Create pick ray
	ray := editorComp.editor.GetPickRay(float64(mouseX), float64(mouseY), width, height, camState)

	// Pick voxel from scene
	if state.rtApp == nil || state.rtApp.Scene == nil {
		return
	}

	hit := editorComp.editor.Pick(state.rtApp.Scene, ray)
	if hit == nil {
		return
	}

	// Apply brush to hit object
	editorComp.editor.ApplyBrush(hit.Object, hit.Coord, hit.Normal)
}

// InstallEditorSystem registers the editor system with the app
func InstallEditorSystem(app *App) {
	app.UseSystem(
		System(editorSystem).
			InStage(Update).
			RunAlways(),
	)
}
