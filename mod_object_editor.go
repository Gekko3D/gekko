package gekko

import (
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/gekko3d/gekko/voxelrt/rt/editor"
)

// EditorSelectedComponent marks an entity as selected by the object editor
type EditorSelectedComponent struct{}

// EditorGizmoTag marks an entity as part of the editor gizmos (so it's not selectable)
type EditorGizmoTag struct {
	Axis  mgl32.Vec3
	Type  GizmoInteractionType
	Scale float32
}

// EditorUiTag marks an entity as part of the editor UI
type EditorUiTag struct {
	Type string
}

type GizmoInteractionType int

const (
	GizmoTranslate GizmoInteractionType = iota
	GizmoRotate
)

// ObjectEditorComponent holds the state of the object editor
type ObjectEditorComponent struct {
	Enabled bool

	// Internal state for dragging
	activeGizmo                EntityId
	isFreeDragging             bool
	dragStartHitT              float32
	dragStartHitVec            mgl32.Vec3
	dragPlaneNormal            mgl32.Vec3
	dragPlanePoint             mgl32.Vec3
	dragEntityInitialTransform TransformComponent
	dragPivotPoint             mgl32.Vec3 // The center of the object (AABB center) at start

	// Hierarchy and Preset state
	ParentingMode  bool
	PresetFilename string
	SaveRequested  bool
	LoadRequested  bool

	// Voxel Editing state
	VoxelEditMode bool
	BrushRadius   float32
	BrushValue    uint8  // Material index to place (0 = erase)
	EditMode      string // "add" or "remove"
	voxelEditor   *editor.Editor
}

type ObjectEditorModule struct{}

func (m ObjectEditorModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(EditorSelectionSystem).
			InStage(PreUpdate).
			RunAlways(),
	)
	app.UseSystem(
		System(EditorGizmoSyncSystem).
			InStage(Update).
			RunAlways(),
	)
	app.UseSystem(
		System(EditorInteractionSystem).
			InStage(PreUpdate).
			RunAlways(),
	)
	app.UseSystem(
		System(EditorUiSystem).
			InStage(Update).
			RunAlways(),
	)
}

func EditorSelectionSystem(cmd *Commands, input *Input, state *VoxelRtState) {
	if !input.JustPressed[MouseButtonLeft] || input.MouseCaptured {
		return
	}

	var camera *CameraComponent
	MakeQuery1[CameraComponent](cmd).Map(func(eid EntityId, cam *CameraComponent) bool {
		camera = cam
		return false
	})
	if camera == nil {
		return
	}

	var editorComp *ObjectEditorComponent
	MakeQuery1[ObjectEditorComponent](cmd).Map(func(eid EntityId, ec *ObjectEditorComponent) bool {
		editorComp = ec
		return false
	})
	if editorComp != nil && editorComp.VoxelEditMode {
		return
	}

	origin, dir := state.ScreenToWorldRay(float64(input.MouseX), float64(input.MouseY), camera)

	// Don't select if we click a gizmo
	if hitGizmo(cmd, origin, dir) != 0 {
		return
	}

	hit := state.Raycast(origin, dir, 1000.0)

	if hit.Hit {
		// If hit something already selected (or child of selected), keep it
		if findSelectedAncestor(cmd, hit.Entity) != 0 {
			return
		}

		// Otherwise, clear everything
		MakeQuery1[EditorSelectedComponent](cmd).Map(func(eid EntityId, s *EditorSelectedComponent) bool {
			cmd.RemoveComponents(eid, EditorSelectedComponent{})
			return true
		})

		// Select the new one (the specific entity hit)
		cmd.AddComponents(hit.Entity, EditorSelectedComponent{})
	} else {
		// Clicked empty space, clear all
		MakeQuery1[EditorSelectedComponent](cmd).Map(func(eid EntityId, s *EditorSelectedComponent) bool {
			cmd.RemoveComponents(eid, EditorSelectedComponent{})
			return true
		})
	}
}

func hitGizmo(cmd *Commands, origin, dir mgl32.Vec3) EntityId {
	var bestGizmo EntityId = 0
	minT := float32(1000.0)

	MakeQuery2[EditorGizmoTag, TransformComponent](cmd).Map(func(eid EntityId, tag *EditorGizmoTag, tr *TransformComponent) bool {
		if tag.Type == GizmoTranslate {
			p1 := tr.Position
			v1 := tr.Rotation.Rotate(tag.Axis)
			t, s, d := closestPoints(origin, dir, p1, v1)
			if t > 0 && s >= 0 && s <= 2.2*tag.Scale && d < 0.25*tag.Scale {
				if t < minT {
					minT = t
					bestGizmo = eid
				}
			}
		} else if tag.Type == GizmoRotate {
			// The rotation axis is the normal of the circle (local Z)
			wAxis := tr.Rotation.Rotate(mgl32.Vec3{0, 0, 1})

			denom := dir.Dot(wAxis)
			if math.Abs(float64(denom)) > 1e-6 {
				t := (tr.Position.Sub(origin)).Dot(wAxis) / denom
				if t > 0 {
					hitPos := origin.Add(dir.Mul(t))
					dist := hitPos.Sub(tr.Position).Len()
					if math.Abs(float64(dist-2.0*tag.Scale)) < float64(0.4*tag.Scale) {
						if t < minT {
							minT = t
							bestGizmo = eid
						}
					}
				}
			}
		}
		return true
	})
	return bestGizmo
}

func findSelectedAncestor(cmd *Commands, eid EntityId) EntityId {
	curr := eid
	for curr != 0 {
		found := false
		MakeQuery1[EditorSelectedComponent](cmd).Map(func(seid EntityId, s *EditorSelectedComponent) bool {
			if seid == curr {
				found = true
				return false
			}
			return true
		})
		if found {
			return curr
		}

		parent := EntityId(0)
		MakeQuery1[Parent](cmd).Map(func(ceid EntityId, p *Parent) bool {
			if ceid == curr {
				parent = p.Entity
				return false
			}
			return true
		})
		curr = parent
	}
	return 0
}

func closestPoints(ro, rd, ao, ad mgl32.Vec3) (float32, float32, float32) {
	r := ro.Sub(ao)
	a := rd.Dot(rd)
	b := rd.Dot(ad)
	e := ad.Dot(ad)
	f := ad.Dot(r)

	det := a*e - b*b
	if det < 1e-6 {
		return 0, 0, r.Len()
	}

	c := rd.Dot(r)
	t := (b*f - c*e) / det
	s := (a*f - b*c) / det

	p1 := ro.Add(rd.Mul(t))
	p2 := ao.Add(ad.Mul(s))
	return t, s, p1.Sub(p2).Len()
}

func EditorGizmoSyncSystem(cmd *Commands, state *VoxelRtState) {
	var selectedEntity EntityId = 0
	var selectedPos mgl32.Vec3
	var selectedRot mgl32.Quat = mgl32.QuatIdent()
	var gizmoScale float32 = 1.0

	MakeQuery2[EditorSelectedComponent, TransformComponent](cmd).Map(func(eid EntityId, s *EditorSelectedComponent, tr *TransformComponent) bool {
		selectedEntity = eid
		selectedPos = tr.Position
		selectedRot = tr.Rotation

		obj := state.GetVoxelObject(eid)
		if obj != nil {
			size := obj.WorldAABB[1].Sub(obj.WorldAABB[0])
			maxDim := float32(math.Max(float64(size.X()), math.Max(float64(size.Y()), float64(size.Z()))))
			gizmoScale = maxDim * 0.5
			if gizmoScale < 1.0 {
				gizmoScale = 1.0
			}
			selectedPos = obj.WorldAABB[0].Add(obj.WorldAABB[1]).Mul(0.5)
		}
		return false
	})

	if selectedEntity == 0 {
		MakeQuery1[EditorGizmoTag](cmd).Map(func(eid EntityId, tag *EditorGizmoTag) bool {
			cmd.RemoveEntity(eid)
			return true
		})
		return
	}

	gizmosExist := false
	MakeQuery2[EditorGizmoTag, TransformComponent](cmd).Map(func(eid EntityId, tag *EditorGizmoTag, gtr *TransformComponent) bool {
		gizmosExist = true
		gtr.Position = selectedPos
		gtr.Scale = mgl32.Vec3{gizmoScale, gizmoScale, gizmoScale}
		tag.Scale = gizmoScale

		// Align gizmo with model rotation
		if tag.Type == GizmoTranslate {
			gtr.Rotation = selectedRot
		} else if tag.Type == GizmoRotate {
			// Base rotations for circles
			var baseRot mgl32.Quat
			if tag.Axis.Sub(mgl32.Vec3{1, 0, 0}).Len() < 0.1 { // X axis
				baseRot = mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0})
			} else if tag.Axis.Sub(mgl32.Vec3{0, 1, 0}).Len() < 0.1 { // Y axis
				baseRot = mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{1, 0, 0})
			} else { // Z axis
				baseRot = mgl32.QuatIdent()
			}
			gtr.Rotation = selectedRot.Mul(baseRot)
		}
		return true
	})

	if !gizmosExist {
		createGizmos(cmd, selectedPos, selectedRot, gizmoScale)
	}
}

func createGizmos(cmd *Commands, pos mgl32.Vec3, rot mgl32.Quat, scale float32) {
	// Translation gizmos (aligned with model axis)
	cmd.AddEntity(
		&TransformComponent{Position: pos, Rotation: rot, Scale: mgl32.Vec3{scale, scale, scale}},
		&EditorGizmoTag{Axis: mgl32.Vec3{1, 0, 0}, Type: GizmoTranslate, Scale: scale},
		NewGizmoLine(mgl32.Vec3{0, 0, 0}, mgl32.Vec3{2, 0, 0}, [4]float32{1, 0, 0, 1}),
	)
	cmd.AddEntity(
		&TransformComponent{Position: pos, Rotation: rot, Scale: mgl32.Vec3{scale, scale, scale}},
		&EditorGizmoTag{Axis: mgl32.Vec3{0, 1, 0}, Type: GizmoTranslate, Scale: scale},
		NewGizmoLine(mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 2, 0}, [4]float32{0, 1, 0, 1}),
	)
	cmd.AddEntity(
		&TransformComponent{Position: pos, Rotation: rot, Scale: mgl32.Vec3{scale, scale, scale}},
		&EditorGizmoTag{Axis: mgl32.Vec3{0, 0, 1}, Type: GizmoTranslate, Scale: scale},
		NewGizmoLine(mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 0, 2}, [4]float32{0, 0, 1, 1}),
	)

	// Rotation gizmos (circles aligned with model planes)
	// Base orientation: X circle is in YZ plane, Y circle is in XZ plane, Z circle in XY plane
	baseRX := mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0})
	baseRY := mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{1, 0, 0})
	baseRZ := mgl32.QuatIdent()

	// Green (Y)
	cmd.AddEntity(
		&TransformComponent{
			Position: pos,
			Rotation: rot.Mul(baseRY),
			Scale:    mgl32.Vec3{scale, scale, scale},
		},
		&EditorGizmoTag{Axis: mgl32.Vec3{0, 1, 0}, Type: GizmoRotate, Scale: scale},
		GizmoComponent{Type: GizmoCircle, Radius: 2.0, Color: [4]float32{0, 1, 0, 1}},
	)
	// Red (X)
	cmd.AddEntity(
		&TransformComponent{
			Position: pos,
			Rotation: rot.Mul(baseRX),
			Scale:    mgl32.Vec3{scale, scale, scale},
		},
		&EditorGizmoTag{Axis: mgl32.Vec3{1, 0, 0}, Type: GizmoRotate, Scale: scale},
		GizmoComponent{Type: GizmoCircle, Radius: 2.0, Color: [4]float32{1, 0, 0, 1}},
	)
	// Blue (Z)
	cmd.AddEntity(
		&TransformComponent{
			Position: pos,
			Rotation: rot.Mul(baseRZ),
			Scale:    mgl32.Vec3{scale, scale, scale},
		},
		&EditorGizmoTag{Axis: mgl32.Vec3{0, 0, 1}, Type: GizmoRotate, Scale: scale},
		GizmoComponent{Type: GizmoCircle, Radius: 2.0, Color: [4]float32{0, 0, 1, 1}},
	)
}

func EditorInteractionSystem(cmd *Commands, input *Input, state *VoxelRtState) {
	var editorComp *ObjectEditorComponent
	MakeQuery1[ObjectEditorComponent](cmd).Map(func(eid EntityId, ec *ObjectEditorComponent) bool {
		editorComp = ec
		return false
	})
	if editorComp == nil {
		cmd.AddEntity(&ObjectEditorComponent{Enabled: true})
		return
	}
	if !editorComp.Enabled {
		return
	}

	var camera *CameraComponent
	MakeQuery1[CameraComponent](cmd).Map(func(eid EntityId, cam *CameraComponent) bool {
		camera = cam
		return false
	})
	if camera == nil {
		return
	}

	origin, dir := state.ScreenToWorldRay(float64(input.MouseX), float64(input.MouseY), camera)

	// Calculate camera forward for plane dragging
	yawRad := mgl32.DegToRad(camera.Yaw)
	pitchRad := mgl32.DegToRad(camera.Pitch)
	forward := mgl32.Vec3{
		float32(math.Sin(float64(yawRad)) * math.Cos(float64(pitchRad))),
		float32(math.Sin(float64(pitchRad))),
		float32(-math.Cos(float64(yawRad)) * math.Cos(float64(pitchRad))),
	}.Normalize()

	// Initialize voxel editor if needed
	if editorComp.voxelEditor == nil {
		editorComp.voxelEditor = editor.NewEditor()
		editorComp.BrushRadius = 2.0
		editorComp.BrushValue = 1
		editorComp.EditMode = "add"
	}

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

	// VOXEL EDITING MODE
	if editorComp.VoxelEditMode {
		leftClick := input.JustPressed[MouseButtonLeft] || input.Pressed[MouseButtonLeft]
		rightClick := input.JustPressed[MouseButtonRight] || input.Pressed[MouseButtonRight]

		if leftClick || rightClick {
			// Update editor settings
			val := editorComp.BrushValue
			if rightClick {
				val = 0 // Erase
			}

			// Use engine's raycasting for consistency
			origin, dir := state.ScreenToWorldRay(float64(input.MouseX), float64(input.MouseY), camera)
			hit := state.Raycast(origin, dir, 1000.0)

			if hit.Hit {
				state.RtApp.Profiler.BeginScope("Editor Apply")

				// Calculate world-space center for the brush
				worldHitCenter := origin.Add(dir.Mul(hit.T))

				// If adding, shift slightly outward to avoid z-fighting/stuck voxels
				if val != 0 {
					// We can use the hit normal to shift by half a voxel or so
					// to make sure we are mostly in the "next" empty space
					shift := hit.Normal.Mul(0.1) // Small shift
					worldHitCenter = worldHitCenter.Add(shift)
				}

				state.VoxelSphereEdit(hit.Entity, worldHitCenter, editorComp.BrushRadius, val)
				state.RtApp.Profiler.EndScope("Editor Apply")
			}
		}
		return // Don't perform object-level selection/dragging in voxel edit mode
	}

	// MOUSE DOWN: Capture Start State
	if input.JustPressed[MouseButtonLeft] {
		if editorComp.ParentingMode {
			hit := state.Raycast(origin, dir, 1000.0)
			if hit.Hit {
				selectedEid := findSelectedAncestor(cmd, 0) // Helper to get currently selected
				MakeQuery1[EditorSelectedComponent](cmd).Map(func(eid EntityId, s *EditorSelectedComponent) bool {
					selectedEid = eid
					return false
				})

				if selectedEid != 0 && selectedEid != hit.Entity {
					// Set hit.Entity as parent of selectedEid
					setParent(cmd, state, selectedEid, hit.Entity)
					fmt.Printf("Set Entity %d as parent of Entity %d\n", hit.Entity, selectedEid)
					editorComp.ParentingMode = false
				}
			} else {
				// Clicked empty space, cancel parenting mode
				editorComp.ParentingMode = false
			}
			return
		}

		gid := hitGizmo(cmd, origin, dir)
		if gid != 0 {
			editorComp.activeGizmo = gid
			editorComp.isFreeDragging = false

			MakeQuery2[EditorSelectedComponent, TransformComponent](cmd).Map(func(eid EntityId, s *EditorSelectedComponent, tr *TransformComponent) bool {
				editorComp.dragEntityInitialTransform = *tr

				// Pivot is the center (gizmo position)
				obj := state.GetVoxelObject(eid)
				if obj != nil {
					editorComp.dragPivotPoint = obj.WorldAABB[0].Add(obj.WorldAABB[1]).Mul(0.5)
				} else {
					editorComp.dragPivotPoint = tr.Position
				}

				MakeQuery2[EditorGizmoTag, TransformComponent](cmd).Map(func(geid EntityId, tag *EditorGizmoTag, gtr *TransformComponent) bool {
					if geid == gid {
						wAxis := gtr.Rotation.Rotate(tag.Axis)
						if tag.Type == GizmoRotate {
							wAxis = gtr.Rotation.Rotate(mgl32.Vec3{0, 0, 1})
						}

						if tag.Type == GizmoTranslate {
							_, s, _ := closestPoints(origin, dir, gtr.Position, wAxis)
							editorComp.dragStartHitT = s
						} else if tag.Type == GizmoRotate {
							denom := dir.Dot(wAxis)
							if math.Abs(float64(denom)) > 1e-6 {
								t := (gtr.Position.Sub(origin)).Dot(wAxis) / denom
								hitPos := origin.Add(dir.Mul(t))
								editorComp.dragStartHitVec = hitPos.Sub(gtr.Position).Normalize()
							}
						}
					}
					return true
				})
				return false
			})
		} else {
			// Free Dragging check
			hit := state.Raycast(origin, dir, 1000.0)
			if hit.Hit {
				selectedEid := findSelectedAncestor(cmd, hit.Entity)

				if selectedEid != 0 {
					editorComp.activeGizmo = 0
					editorComp.isFreeDragging = true

					// Setup Plane (Facing the camera at hit depth)
					editorComp.dragPlaneNormal = forward.Mul(-1.0)
					editorComp.dragPlanePoint = origin.Add(dir.Mul(hit.T))

					MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, tr *TransformComponent) bool {
						if eid == selectedEid {
							editorComp.dragEntityInitialTransform = *tr
						}
						return true
					})
				}
			}
		}
	}

	// DRAGGING
	if input.Pressed[MouseButtonLeft] {
		if editorComp.activeGizmo != 0 {
			// Gizmo Dragging Logic
			var gizmoTag *EditorGizmoTag
			var gizmoTr TransformComponent
			foundGizmo := false
			MakeQuery2[EditorGizmoTag, TransformComponent](cmd).Map(func(eid EntityId, tag *EditorGizmoTag, tr *TransformComponent) bool {
				if eid == editorComp.activeGizmo {
					gizmoTag = tag
					gizmoTr = *tr
					foundGizmo = true
					return false
				}
				return true
			})

			if foundGizmo {
				MakeQuery2[EditorSelectedComponent, TransformComponent](cmd).Map(func(eid EntityId, s *EditorSelectedComponent, tr *TransformComponent) bool {
					if gizmoTag.Type == GizmoTranslate {
						wAxis := gizmoTr.Rotation.Rotate(gizmoTag.Axis)

						// Use stable line intersection
						r := origin.Sub(gizmoTr.Position)
						a := dir.Dot(dir)
						b := dir.Dot(wAxis)
						e := wAxis.Dot(wAxis)
						f := wAxis.Dot(r)
						det := a*e - b*b

						if det > 0.01 {
							c := dir.Dot(r)
							s := (a*f - b*c) / det

							deltaT := s - editorComp.dragStartHitT
							deltaPos := wAxis.Mul(deltaT)

							newPos := editorComp.dragEntityInitialTransform.Position.Add(deltaPos)
							updateTransform(cmd, eid, tr, newPos, tr.Rotation)
						}
					} else if gizmoTag.Type == GizmoRotate {
						wAxis := gizmoTr.Rotation.Rotate(mgl32.Vec3{0, 0, 1})

						denom := dir.Dot(wAxis)
						if math.Abs(float64(denom)) > 1e-6 {
							t := (gizmoTr.Position.Sub(origin)).Dot(wAxis) / denom
							if t > 0 {
								hitPos := origin.Add(dir.Mul(t))
								currentVec := hitPos.Sub(gizmoTr.Position).Normalize()

								cosTheta := mgl32.Clamp(currentVec.Dot(editorComp.dragStartHitVec), -1.0, 1.0)
								angle := float32(math.Acos(float64(cosTheta)))

								cross := editorComp.dragStartHitVec.Cross(currentVec)
								if cross.Dot(wAxis) < 0 {
									angle = -angle
								}

								// Orbit Rotation
								rot := mgl32.QuatRotate(angle, wAxis)

								// 1. New Rotation
								newRot := rot.Mul(editorComp.dragEntityInitialTransform.Rotation).Normalize()

								// 2. New Position (Orbit initial position around pivot)
								pivot := editorComp.dragPivotPoint
								p0 := editorComp.dragEntityInitialTransform.Position
								pRelative := p0.Sub(pivot)
								pRotated := rot.Rotate(pRelative)
								newPos := pivot.Add(pRotated)

								updateTransform(cmd, eid, tr, newPos, newRot)
							}
						}
					}
					return true
				})
			}
		} else if editorComp.isFreeDragging {
			// Free Dragging Logic
			denom := dir.Dot(editorComp.dragPlaneNormal)
			if math.Abs(float64(denom)) > 1e-6 {
				t := (editorComp.dragPlanePoint.Sub(origin)).Dot(editorComp.dragPlaneNormal) / denom
				currentPos := origin.Add(dir.Mul(t))
				delta := currentPos.Sub(editorComp.dragPlanePoint)

				newPos := editorComp.dragEntityInitialTransform.Position.Add(delta)

				MakeQuery2[EditorSelectedComponent, TransformComponent](cmd).Map(func(eid EntityId, s *EditorSelectedComponent, tr *TransformComponent) bool {
					updateTransform(cmd, eid, tr, newPos, tr.Rotation)
					return true
				})
			}
		}
	}

	if input.JustReleased[MouseButtonLeft] {
		editorComp.activeGizmo = 0
		editorComp.isFreeDragging = false
	}
}

func updateTransform(cmd *Commands, eid EntityId, worldTransform *TransformComponent, newPos mgl32.Vec3, newRot mgl32.Quat) {
	isChild := false
	MakeQuery1[Parent](cmd).Map(func(ceid EntityId, p *Parent) bool {
		if ceid == eid {
			isChild = true
			var parentTransform TransformComponent
			found := false
			for _, c := range cmd.GetAllComponents(p.Entity) {
				if t, ok := c.(TransformComponent); ok {
					parentTransform = t
					found = true
					break
				}
			}

			if found {
				diff := newPos.Sub(parentTransform.Position)
				localPos := parentTransform.Rotation.Conjugate().Rotate(diff)
				localPos = mgl32.Vec3{
					localPos.X() / (parentTransform.Scale.X() + 1e-6),
					localPos.Y() / (parentTransform.Scale.Y() + 1e-6),
					localPos.Z() / (parentTransform.Scale.Z() + 1e-6),
				}
				localRot := parentTransform.Rotation.Conjugate().Mul(newRot).Normalize()

				MakeQuery1[LocalTransformComponent](cmd).Map(func(leid EntityId, local *LocalTransformComponent) bool {
					if leid == eid {
						local.Position = localPos
						local.Rotation = localRot
					}
					return true
				})
			}
		}
		return true
	})

	if !isChild {
		worldTransform.Position = newPos
		worldTransform.Rotation = newRot
	}
}

func setParent(cmd *Commands, state *VoxelRtState, child, parent EntityId) {
	var childWorld TransformComponent
	foundChild := false
	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, tr *TransformComponent) bool {
		if eid == child {
			childWorld = *tr
			foundChild = true
		}
		return true
	})

	var parentWorld TransformComponent
	foundParent := false
	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, tr *TransformComponent) bool {
		if eid == parent {
			parentWorld = *tr
			foundParent = true
		}
		return true
	})

	if foundChild && foundParent {
		// Calculate local transform
		diff := childWorld.Position.Sub(parentWorld.Position)
		localPos := parentWorld.Rotation.Conjugate().Rotate(diff)
		localPos = mgl32.Vec3{
			localPos.X() / (parentWorld.Scale.X() + 1e-6),
			localPos.Y() / (parentWorld.Scale.Y() + 1e-6),
			localPos.Z() / (parentWorld.Scale.Z() + 1e-6),
		}
		localRot := parentWorld.Rotation.Conjugate().Mul(childWorld.Rotation).Normalize()

		cmd.AddComponents(child, &Parent{Entity: parent}, &LocalTransformComponent{
			Position: localPos,
			Rotation: localRot,
			Scale:    mgl32.Vec3{childWorld.Scale.X() / (parentWorld.Scale.X() + 1e-6), childWorld.Scale.Y() / (parentWorld.Scale.Y() + 1e-6), childWorld.Scale.Z() / (parentWorld.Scale.Z() + 1e-6)},
		})
	}
}

func EditorUiSystem(cmd *Commands, server *AssetServer, state *VoxelRtState) {
	var editorComp *ObjectEditorComponent
	MakeQuery1[ObjectEditorComponent](cmd).Map(func(eid EntityId, ec *ObjectEditorComponent) bool {
		editorComp = ec
		return false
	})
	if editorComp == nil || !editorComp.Enabled {
		return
	}

	// 1. Ensure UI entities exist
	uiExists := false
	MakeQuery1[EditorUiTag](cmd).Map(func(eid EntityId, tag *EditorUiTag) bool {
		uiExists = true
		return false
	})

	if !uiExists {
		createEditorUi(cmd, editorComp)
	}

	// 2. Update UI state
	MakeQuery2[EditorUiTag, UiButton](cmd).Map(func(eid EntityId, tag *EditorUiTag, btn *UiButton) bool {
		if tag.Type == "parent" {
			if editorComp.ParentingMode {
				btn.Label = "> PICK PARENT <"
				btn.Highlighted = true
			} else {
				btn.Label = "SET PARENT"
				btn.Highlighted = false
			}
		} else if tag.Type == "vox_edit" {
			if editorComp.VoxelEditMode {
				btn.Label = "> VOXEL EDIT <"
				btn.Highlighted = true
			} else {
				btn.Label = "VOXEL EDIT"
				btn.Highlighted = false
			}
		} else if tag.Type == "vox_edit_controls" {
			if btn.Label == "BRUSH +" || strings.HasPrefix(btn.Label, "BRUSH +") {
				btn.Label = fmt.Sprintf("BRUSH + (%.1f)", editorComp.BrushRadius)
			}
		}
		return true
	})

	// 3. Update Hierarchy List
	MakeQuery2[EditorUiTag, UiList](cmd).Map(func(eid EntityId, tag *EditorUiTag, list *UiList) bool {
		if tag.Type == "hierarchy" {
			list.Items = buildHierarchyList(cmd)
		}
		return true
	})

	// 4. Execute Save/Load if requested
	if editorComp.SaveRequested {
		filename := "presets/" + editorComp.PresetFilename + ".json"
		os.MkdirAll("presets", 0755)
		if err := SavePreset(cmd, server, filename); err != nil {
			fmt.Printf("Error saving preset: %v\n", err)
		} else {
			fmt.Printf("Preset saved to %s\n", filename)
		}
		editorComp.SaveRequested = false
	}

	if editorComp.LoadRequested {
		filename := "presets/" + editorComp.PresetFilename + ".json"
		if _, err := LoadPreset(cmd, server, filename); err != nil {
			fmt.Printf("Error loading preset: %v\n", err)
		} else {
			fmt.Printf("Preset loaded from %s\n", filename)
		}
		editorComp.LoadRequested = false
	}
}

func createEditorUi(cmd *Commands, editorComp *ObjectEditorComponent) {
	baseX := float32(1000)
	baseY := float32(50)

	cmd.AddEntity(
		&EditorUiTag{Type: "filename"},
		&UiTextBox{
			Label:    "Preset Name:",
			Text:     "my_preset",
			Position: [2]float32{baseX, baseY},
			Width:    300,
			OnSubmit: func(text string) {
				editorComp.PresetFilename = text
			},
		},
	)

	cmd.AddEntity(
		&EditorUiTag{Type: "save"},
		&UiButton{
			Label:    "SAVE PRESET",
			Position: [2]float32{baseX, baseY + 80},
			Width:    300,
			OnClick: func() {
				editorComp.SaveRequested = true
			},
		},
	)

	cmd.AddEntity(
		&EditorUiTag{Type: "load"},
		&UiButton{
			Label:    "LOAD PRESET",
			Position: [2]float32{baseX, baseY + 160},
			Width:    300,
			OnClick: func() {
				editorComp.LoadRequested = true
			},
		},
	)

	cmd.AddEntity(
		&EditorUiTag{Type: "parent"},
		&UiButton{
			Label:    "SET PARENT",
			Position: [2]float32{baseX, baseY + 240},
			Width:    300,
			OnClick: func() {
				editorComp.ParentingMode = !editorComp.ParentingMode
			},
		},
	)

	cmd.AddEntity(
		&EditorUiTag{Type: "vox_edit"},
		&UiButton{
			Label:    "VOXEL EDIT",
			Position: [2]float32{baseX, baseY + 320},
			Width:    300,
			OnClick: func() {
				editorComp.VoxelEditMode = !editorComp.VoxelEditMode
			},
		},
	)

	// Brush Size Controls
	cmd.AddEntity(
		&EditorUiTag{Type: "vox_edit_controls"},
		&UiButton{
			Label:    "BRUSH +",
			Position: [2]float32{baseX, baseY + 400},
			Width:    145,
			OnClick: func() {
				editorComp.BrushRadius += 1.0
				if editorComp.BrushRadius > 10.0 {
					editorComp.BrushRadius = 10.0
				}
			},
		},
	)
	cmd.AddEntity(
		&EditorUiTag{Type: "vox_edit_controls"},
		&UiButton{
			Label:    "BRUSH -",
			Position: [2]float32{baseX + 155, baseY + 400},
			Width:    145,
			OnClick: func() {
				editorComp.BrushRadius -= 1.0
				if editorComp.BrushRadius < 1.0 {
					editorComp.BrushRadius = 1.0
				}
			},
		},
	)

	cmd.AddEntity(
		&EditorUiTag{Type: "hierarchy"},
		&UiList{
			Title:    "SCENE HIERARCHY",
			Position: [2]float32{baseX, baseY + 480},
			Scale:    0.8,
		},
	)
}

func buildHierarchyList(cmd *Commands) []UiListItem {
	var roots []EntityId

	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, tr *TransformComponent) bool {
		allComps := cmd.GetAllComponents(eid)
		isInternal := false
		hasParent := false

		for _, c := range allComps {
			switch c.(type) {
			case EditorGizmoTag, *EditorGizmoTag, EditorUiTag, *EditorUiTag:
				isInternal = true
			case Parent, *Parent:
				hasParent = true
			}
		}

		if !isInternal && !hasParent {
			roots = append(roots, eid)
		}
		return true
	})

	// Sort roots for deterministic display
	slices.Sort(roots)

	var items []UiListItem
	visited := make(map[EntityId]bool)
	for _, eid := range roots {
		items = append(items, buildUiListItem(cmd, eid, visited))
	}
	return items
}

func buildUiListItem(cmd *Commands, eid EntityId, visited map[EntityId]bool) UiListItem {
	if visited[eid] {
		return UiListItem{Label: fmt.Sprintf("Entity %d (Cycle!)", eid)}
	}
	visited[eid] = true

	allComps := cmd.GetAllComponents(eid)
	isSelected := false
	for _, c := range allComps {
		// Use reflect for selection check to be safer with pointers vs values
		t := reflect.TypeOf(c)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		if t.Name() == "EditorSelectedComponent" {
			isSelected = true
		}
	}

	label := fmt.Sprintf("Entity %d", eid)
	if isSelected {
		label = "[*] " + label
	}

	item := UiListItem{Label: label}

	// Find children efficiently
	var children []EntityId
	MakeQuery1[Parent](cmd).Map(func(ceid EntityId, p *Parent) bool {
		if p.Entity == eid {
			children = append(children, ceid)
		}
		return true
	})

	// Sort children for deterministic display
	slices.Sort(children)

	for _, childId := range children {
		item.Children = append(item.Children, buildUiListItem(cmd, childId, visited))
	}
	return item
}
