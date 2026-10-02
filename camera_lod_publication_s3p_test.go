package gekko

import (
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

var s3pTypes = [2]reflect.Type{reflect.TypeOf(CameraComponent{}), reflect.TypeOf(EntityLODComponent{})}

// Revisions are opaque publication stamps, not event counts.
func s3pStep(t *testing.T, cmd *Commands, call func(), cameraChanged, lodChanged bool) {
	t.Helper()
	before := [2]uint64{cmd.ComponentRevision(s3pTypes[0]), cmd.ComponentRevision(s3pTypes[1])}
	transforms, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	call()
	for i, want := range [2]bool{cameraChanged, lodChanged} {
		if got := cmd.ComponentRevision(s3pTypes[i]) != before[i]; got != want {
			t.Errorf("%v publication changed=%v, want %v", s3pTypes[i], got, want)
		}
	}
	s3gPublications(t, cmd, transforms, structural, [4]int{})
}

func s3pCamera() CameraComponent {
	return CameraComponent{LookAt: mgl32.Vec3{0, 0, -1}, Up: mgl32.Vec3{0, 1, 0},
		Fov: math.Float32frombits(0x7fc01234), Aspect: 1.5, Near: 0.1, Far: 200, DepthMode: core.DepthModeReverseZ}
}

type s3pCameraValue struct {
	fields [15]uint32
	depth  core.DepthMode
}

func s3pCameraBits(cam *CameraComponent) s3pCameraValue {
	fields := []float32{cam.Position[0], cam.Position[1], cam.Position[2], cam.LookAt[0], cam.LookAt[1], cam.LookAt[2],
		cam.Up[0], cam.Up[1], cam.Up[2], cam.Yaw, cam.Pitch, cam.Fov, cam.Aspect, cam.Near, cam.Far}
	bits := s3pCameraValue{depth: cam.DepthMode}
	for i, value := range fields {
		bits.fields[i] = math.Float32bits(value)
	}
	return bits
}

func s3pProjection(t *testing.T, cam *CameraComponent, before s3pCameraValue) {
	t.Helper()
	after := s3pCameraBits(cam)
	for i := 11; i < len(before.fields); i++ {
		if after.fields[i] != before.fields[i] {
			t.Error("camera writer changed projection fields")
		}
	}
	if after.depth != before.depth {
		t.Error("camera writer changed projection depth mode")
	}
}

func TestS3pFlyingCameraPublication(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	id := cmd.AddEntity(s3pCamera(), FlyingCameraComponent{}, s3gUnrelated{7})
	app.FlushCommands()
	cam := s3cComponent[CameraComponent](t, cmd, id)
	fly := s3cComponent[FlyingCameraComponent](t, cmd, id)
	initial := s3pCameraBits(cam)
	run := func() { FlyingCameraControlSystem(cmd, &Time{Dt: 1}) }
	s3pStep(t, cmd, run, false, false)
	if s3pCameraBits(cam) != initial || fly.Speed != 5 || fly.Sensitivity != 0.1 {
		t.Fatal("quiet controls changed camera or lost existing defaults")
	}
	fly.Move = mgl32.Vec3{0, 0, 1}
	s3pStep(t, cmd, run, true, false)
	if cam.Position != (mgl32.Vec3{0, 0, -5}) || cam.LookAt != (mgl32.Vec3{0, 0, -6}) {
		t.Fatalf("flying movement changed: %+v", cam)
	}
	fly.Move, fly.Look = mgl32.Vec3{}, mgl32.Vec2{10, -1000}
	s3pStep(t, cmd, run, true, false)
	if cam.Yaw != 1 || cam.Pitch != 89 {
		t.Fatalf("flying look/default sensitivity/pitch clamp changed: %+v", cam)
	}
	fly.Look = mgl32.Vec2{}
	s3pStep(t, cmd, run, false, false)
	cam.LookAt = mgl32.Vec3{999, 999, 999}
	cam.Up = mgl32.Vec3{1, 0, 0}
	s3pStep(t, cmd, run, true, false)
	if cam.LookAt == (mgl32.Vec3{999, 999, 999}) || cam.Up != (mgl32.Vec3{0, 1, 0}) {
		t.Fatal("idle camera pose was not repaired")
	}
	s3pStep(t, cmd, run, false, false)
	s3pProjection(t, cam, initial)
	before := s3pCameraBits(cam)
	fly.Look, fly.Move = mgl32.Vec2{40, 40}, mgl32.Vec3{1, 1, 1}
	for _, dt := range []float64{0, -1} {
		s3pStep(t, cmd, func() { FlyingCameraControlSystem(cmd, &Time{Dt: dt}) }, false, false)
		if s3pCameraBits(cam) != before {
			t.Fatal("nonpositive dt changed camera")
		}
	}
}

func TestS3pGroundedLookPublishesIndependentlyOfPose(t *testing.T) {
	for _, intent := range []bool{false, true} {
		name := "legacy look"
		if intent {
			name = "intent skips legacy look"
		}
		t.Run(name, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			camera := s3pCamera()
			camera.Position = mgl32.Vec3{1, 3.5, 3}
			camera.LookAt = mgl32.Vec3{1, 3.5, 2}
			camera.Yaw = math.Float32frombits(0x80000000)
			components := []any{camera, GroundedPlayerControllerComponent{EyeHeight: 1.5, ScriptedMovement: true},
				TransformComponent{Position: mgl32.Vec3{1, 2, 3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
				LocalTransformComponent{Position: mgl32.Vec3{1, 2, 3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}}
			if intent {
				components = append(components, GroundedCharacterIntentComponent{})
			}
			id := cmd.AddEntity(components...)
			app.FlushCommands()
			cam := s3cComponent[CameraComponent](t, cmd, id)
			before := s3pCameraBits(cam)
			run := func() { groundedPlayerControlSystem(cmd, &Time{Dt: 1}, nil, nil) }
			// Signed-zero yaw changes while the subsequent pose assignment is identical.
			s3pStep(t, cmd, run, !intent, false)
			want := uint32(0)
			if intent {
				want = 0x80000000
			}
			if math.Float32bits(cam.Yaw) != want {
				t.Fatal("legacy look ownership changed")
			}
			after := s3pCameraBits(cam)
			after.fields[9] = before.fields[9]
			if after != before {
				t.Fatal("signed-zero look changed camera pose/projection")
			}
			s3pStep(t, cmd, run, false, false)
			if !intent {
				ctrl := s3cComponent[GroundedPlayerControllerComponent](t, cmd, id)
				ctrl.LookInput = mgl32.Vec2{10, -1000}
				s3pStep(t, cmd, run, true, false)
				if cam.Yaw != 1 || cam.Pitch != 89 {
					t.Fatal("grounded look/default/clamp changed")
				}
			}
			s3pProjection(t, cam, before)
			frozen := s3pCameraBits(cam)
			for _, time := range []*Time{nil, {Dt: 0}, {Dt: -1}} {
				s3pStep(t, cmd, func() { groundedPlayerControlSystem(cmd, time, nil, nil) }, false, false)
				if s3pCameraBits(cam) != frozen {
					t.Fatal("guarded grounded system changed camera")
				}
			}
		})
	}
}

func TestS3pGroundedPoseBitsAndPointerOwnership(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	id := cmd.AddEntity(s3pCamera())
	other := cmd.AddEntity(s3pCamera())
	app.FlushCommands()
	ctrl := &GroundedPlayerControllerComponent{EyeHeight: 1.5}
	cam := s3cComponent[CameraComponent](t, cmd, id)
	projection := s3pCameraBits(cam)
	base := mgl32.Vec3{1, 2, 3}
	run := func() { groundedPlayerApplyTransform(cmd, id, cam, ctrl, base) }
	s3pStep(t, cmd, run, true, false)
	if cam.Position != (mgl32.Vec3{1, 3.5, 3}) || cam.LookAt != (mgl32.Vec3{1, 3.5, 2}) || cam.Up != (mgl32.Vec3{0, 1, 0}) {
		t.Fatal("grounded pose changed")
	}
	s3pStep(t, cmd, run, false, false)
	cam.Up[0] = math.Float32frombits(0x80000000)
	s3pStep(t, cmd, run, true, false)
	if math.Float32bits(cam.Up[0]) != 0 {
		t.Fatal("signed-zero Up repair was lost")
	}
	base[0] = math.Float32frombits(0x7fc01234)
	s3pStep(t, cmd, run, true, false)
	nonfinite := s3pCameraBits(cam)
	s3pStep(t, cmd, run, false, false)
	if s3pCameraBits(cam) != nonfinite {
		t.Fatal("unchanged NaN pose bits drifted")
	}
	cam.LookAt[0] = math.Float32frombits(0x7fc05678)
	s3pStep(t, cmd, run, true, false)
	if s3pCameraBits(cam) != nonfinite {
		t.Fatal("actual nonfinite pose edit was not repaired")
	}
	s3pProjection(t, cam, projection)
	for _, kind := range []string{"detached", "wrong entity", "missing target"} {
		t.Run(kind, func(t *testing.T) {
			detached := s3pCamera()
			supplied, target := &detached, id
			if kind == "wrong entity" {
				supplied = s3cComponent[CameraComponent](t, cmd, other)
			}
			if kind == "missing target" {
				target = EntityId(999999)
			}
			s3pStep(t, cmd, func() { groundedPlayerApplyTransform(cmd, target, supplied, ctrl, mgl32.Vec3{8, 9, 10}) }, false, false)
			if supplied.Position != (mgl32.Vec3{8, 10.5, 10}) || supplied.LookAt != (mgl32.Vec3{8, 10.5, 9}) {
				t.Fatal("arbitrary supplied camera pointer lost prior writes")
			}
			if s3pCameraBits(cam) != nonfinite {
				t.Fatal("unowned pointer changed target camera")
			}
		})
	}
	for _, call := range []func(){
		func() { groundedPlayerApplyTransform(nil, id, cam, ctrl, mgl32.Vec3{}) },
		func() { groundedPlayerApplyTransform(cmd, id, cam, nil, mgl32.Vec3{}) },
		func() { groundedPlayerApplyTransform(cmd, id, nil, ctrl, mgl32.Vec3{}) },
	} {
		s3pStep(t, cmd, call, false, false)
	}
	if s3pCameraBits(cam) != nonfinite {
		t.Fatal("nil guards changed camera")
	}
}

func TestS3pFlyingCameraQueuedBoundaries(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	replace := cmd.AddEntity(CameraComponent{}, FlyingCameraComponent{})
	remove := cmd.AddEntity(CameraComponent{}, FlyingCameraComponent{})
	add := cmd.AddEntity(FlyingCameraComponent{})
	app.FlushCommands()
	replacement := s3pCamera()
	replacement.Position = mgl32.Vec3{10, 20, 30}
	cmd.AddComponents(replace, replacement)
	cmd.RemoveComponents(remove, CameraComponent{})
	cmd.AddComponents(add, CameraComponent{})
	run := func() { FlyingCameraControlSystem(cmd, &Time{Dt: 1}) }
	s3pStep(t, cmd, run, true, false)
	for _, id := range []EntityId{replace, remove} {
		cam := s3cComponent[CameraComponent](t, cmd, id)
		if cam.Position != (mgl32.Vec3{}) || cam.LookAt != (mgl32.Vec3{0, 0, -1}) {
			t.Fatal("queued camera change leaked before flush")
		}
	}
	if HasComponent[CameraComponent](cmd, add) {
		t.Fatal("queued camera addition became visible")
	}
	s3pStep(t, cmd, run, false, false)
	app.FlushCommands()
	if HasComponent[CameraComponent](cmd, remove) || !HasComponent[CameraComponent](cmd, add) {
		t.Fatal("camera membership did not commit")
	}
	s3pStep(t, cmd, run, true, false)
	cam := s3cComponent[CameraComponent](t, cmd, replace)
	if cam.Position != replacement.Position || cam.LookAt != (mgl32.Vec3{10, 20, 29}) {
		t.Fatal("replacement camera was not reacquired")
	}
	if s3cComponent[CameraComponent](t, cmd, add).LookAt != (mgl32.Vec3{0, 0, -1}) {
		t.Fatal("newly committed camera was not repaired")
	}
	s3pStep(t, cmd, run, false, false)
}

func s3pLOD() EntityLODComponent {
	return EntityLODComponent{Bands: []EntityLODBand{{MaxDistance: 10, Representation: EntityLODRepresentationFullVoxel},
		{MaxDistance: 0, Representation: EntityLODRepresentationDot}}}
}

func s3pSelection(t *testing.T, lod *EntityLODComponent, valid bool, distance float32, band int, max float32, representation EntityLODRepresentation) {
	t.Helper()
	if lod.SelectionValid != valid || math.Float32bits(lod.ActiveDistance) != math.Float32bits(distance) || lod.ActiveBandIndex != band ||
		math.Float32bits(lod.ActiveMaxDistance) != math.Float32bits(max) || lod.ActiveRepresentation != representation {
		t.Fatalf("LOD selection=%+v", lod)
	}
}

func TestS3pLODSelectionPublicationAndCameraPrecedence(t *testing.T) {
	f := newS3cVoxelFixture(t)
	f.state.RtApp.Camera.Position = mgl32.Vec3{100, 0, 0}
	camera := f.cmd.AddEntity(CameraComponent{})
	id := f.cmd.AddEntity(TransformComponent{Position: mgl32.Vec3{3, 0, 0}}, s3pLOD())
	f.app.FlushCommands()
	lod := s3cComponent[EntityLODComponent](t, f.cmd, id)
	authored := append([]EntityLODBand(nil), lod.Bands...)
	run := func() { entityLODSelectionSystem(f.cmd, f.state) }
	s3pStep(t, f.cmd, run, false, true)
	s3pSelection(t, lod, true, 3, 0, 10, EntityLODRepresentationFullVoxel)
	s3pStep(t, f.cmd, run, false, false)
	s3cComponent[CameraComponent](t, f.cmd, camera).Position[0] = 1
	s3pStep(t, f.cmd, run, false, true)
	s3pSelection(t, lod, true, 2, 0, 10, EntityLODRepresentationFullVoxel)
	s3cComponent[CameraComponent](t, f.cmd, camera).Position[0] = 30
	s3pStep(t, f.cmd, run, false, true)
	s3pSelection(t, lod, true, 27, 1, 0, EntityLODRepresentationDot)
	lod.DistanceMetric = EntityLODDistanceMetric(999)
	s3pStep(t, f.cmd, run, false, false)
	if !reflect.DeepEqual(lod.Bands, authored) || lod.Disabled || lod.DistanceMetric != 999 {
		t.Fatal("selection changed authored LOD inputs")
	}
	f.cmd.RemoveComponents(camera, CameraComponent{})
	s3pStep(t, f.cmd, run, false, false)
	f.app.FlushCommands()
	lod = s3cComponent[EntityLODComponent](t, f.cmd, id)
	s3pStep(t, f.cmd, run, false, true)
	s3pSelection(t, lod, true, 97, 1, 0, EntityLODRepresentationDot)
	s3pStep(t, f.cmd, run, false, false)
}

func TestS3pLODSelectionClearRecoveryAndExactBits(t *testing.T) {
	for _, cause := range []string{"disabled", "no camera", "invalid bands"} {
		t.Run(cause, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			camera := cmd.AddEntity(CameraComponent{})
			id := cmd.AddEntity(TransformComponent{Position: mgl32.Vec3{3, 0, 0}}, s3pLOD())
			app.FlushCommands()
			lod := s3cComponent[EntityLODComponent](t, cmd, id)
			run := func() { entityLODSelectionSystem(cmd, nil) }
			s3pStep(t, cmd, run, false, true)
			switch cause {
			case "disabled":
				lod.Disabled = true
			case "no camera":
				cmd.RemoveEntity(camera)
				app.FlushCommands()
			case "invalid bands":
				lod.Bands = nil
			}
			s3pStep(t, cmd, run, false, true)
			s3pSelection(t, lod, false, 0, -1, 0, EntityLODRepresentationFullVoxel)
			s3pStep(t, cmd, run, false, false)
			// A semantic zero still differs from the actual destination bits.
			lod.ActiveDistance = math.Float32frombits(0x80000000)
			s3pStep(t, cmd, run, false, true)
			s3pSelection(t, lod, false, 0, -1, 0, EntityLODRepresentationFullVoxel)
			switch cause {
			case "disabled":
				lod.Disabled = false
			case "no camera":
				cmd.AddEntity(CameraComponent{})
				app.FlushCommands()
			case "invalid bands":
				lod.Bands = s3pLOD().Bands
			}
			s3pStep(t, cmd, run, false, true)
			s3pSelection(t, lod, true, 3, 0, 10, EntityLODRepresentationFullVoxel)
			s3pStep(t, cmd, run, false, false)
		})
	}
	t.Run("unchanged NaN distance and signed-zero maximum", func(t *testing.T) {
		app := NewApp()
		cmd := app.Commands()
		cmd.AddEntity(CameraComponent{Position: mgl32.Vec3{math.Float32frombits(0x7fc01234), 0, 0}})
		definition := EntityLODComponent{Bands: []EntityLODBand{{MaxDistance: math.Float32frombits(0x80000000), Representation: EntityLODRepresentationDot}}}
		id := cmd.AddEntity(TransformComponent{}, definition)
		app.FlushCommands()
		lod := s3cComponent[EntityLODComponent](t, cmd, id)
		run := func() { entityLODSelectionSystem(cmd, nil) }
		s3pStep(t, cmd, run, false, true)
		if !math.IsNaN(float64(lod.ActiveDistance)) || math.Float32bits(lod.ActiveMaxDistance) != 0x80000000 {
			t.Fatal("nonfinite selection or authored signed-zero maximum changed")
		}
		bits := math.Float32bits(lod.ActiveDistance)
		s3pStep(t, cmd, run, false, false)
		if math.Float32bits(lod.ActiveDistance) != bits {
			t.Fatal("unchanged NaN distance bits drifted")
		}
		lod.ActiveMaxDistance = 0
		s3pStep(t, cmd, run, false, true)
		if math.Float32bits(lod.ActiveMaxDistance) != 0x80000000 {
			t.Fatal("signed-zero selection repair lost")
		}
		before := cmd.ComponentRevision(s3pTypes[1])
		selection, err := SelectEntityLOD(mgl32.Vec3{}, &TransformComponent{}, lod)
		if err != nil {
			t.Fatal(err)
		}
		lod.ApplySelection(selection)
		lod.ClearRuntimeSelection()
		if cmd.ComponentRevision(s3pTypes[1]) != before {
			t.Fatal("ownerless LOD helpers published")
		}
	})
}
