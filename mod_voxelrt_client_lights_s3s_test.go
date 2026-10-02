package gekko

import (
	"math"
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func s3sVisits(t *testing.T, state *VoxelRtState, want int) {
	t.Helper()
	field := reflect.ValueOf(state).Elem().FieldByName("VoxelEmitterRadiusObjectVisitsLastSync")
	if !field.IsValid() || field.Kind() != reflect.Int {
		t.Fatal("public diagnostic VoxelEmitterRadiusObjectVisitsLastSync must have type int")
	}
	if got := int(field.Int()); got != want {
		t.Fatalf("emitter-radius object visits = %d, want %d", got, want)
	}
}

func s3sRadius(x, y, z float32) float32 {
	return float32(math.Sqrt(float64(x*x+y*y+z*z))) * 0.5
}

func s3sLightRadius(t *testing.T, state *VoxelRtState, x, want float32) {
	t.Helper()
	for _, light := range state.RtApp.Scene.Lights {
		if light.Position[0] == x {
			got := light.Position[3]
			if math.IsNaN(float64(want)) {
				if !math.IsNaN(float64(got)) {
					t.Fatalf("light at x=%g source radius = %g, want NaN", x, got)
				}
			} else if math.IsNaN(float64(got)) || math.Abs(float64(got-want)) > 0.0001 {
				t.Fatalf("light at x=%g source radius = %g, want %g", x, got, want)
			}
			return
		}
	}
	t.Fatalf("missing renderer light at x=%g", x)
}

func TestS3sEmitterRadiusSharedGroupsObserveDirectCoreBridgeEdits(t *testing.T) {
	f := newS3cVoxelFixture(t)
	addObject := func(x float32, model AssetId, group uint32, scale mgl32.Vec3) EntityId {
		transform := s3cTransform(x)
		transform.Scale = scale
		return f.cmd.AddEntity(transform, VoxelModelComponent{VoxelModel: model, VoxelPalette: f.palette, VoxelResolution: 1, PivotMode: PivotModeCorner, EmitterLinkID: group})
	}
	addObject(0, f.model, 77, mgl32.Vec3{1, 1, 1})
	large := addObject(10, f.model, 77, mgl32.Vec3{2, 1, 1})
	other := addObject(20, f.server.CreateCubeModel(2, 6, 8, 1), 88, mgl32.Vec3{1, 1, 1})
	addObject(30, f.model, 300, mgl32.Vec3{20, 20, 20})
	addLight := func(x float32, kind LightType, intensity, radius float32, group uint32) EntityId {
		transform := s3cTransform(x)
		return f.cmd.AddEntity(transform, &LightComponent{Type: kind, Color: [3]float32{1, 1, 1}, Intensity: intensity, Range: 20, SourceRadius: radius, EmitterLinkID: group})
	}
	addLight(1, LightTypePoint, 2, 0, 77)
	addLight(2, LightTypePoint, 5, 0, 77)
	addLight(3, LightTypeSpot, 3, -5, 88)
	addLight(4, LightTypePoint, 1, 0.75, 300)
	addLight(5, LightTypePoint, 0.5, 0, 999)
	addLight(6, LightTypeDirectional, 1, -1, 0)
	addLight(7, LightTypePoint, 0.25, float32(math.NaN()), 77)
	f.cmd.AddEntity(&LightComponent{Type: LightTypeAmbient, Color: [3]float32{0.1, 0.2, 0.3}, Intensity: 2, EmitterLinkID: 300})
	f.cmd.AddEntity(&LightComponent{Type: LightTypePoint, Color: [3]float32{1, 1, 1}, Intensity: 100, EmitterLinkID: 300})
	f.app.FlushCommands()
	f.sync()
	if len(f.state.RtApp.Scene.Objects) != 4 || len(f.state.RtApp.Scene.Lights) != 7 {
		t.Fatal("fixture did not publish four voxel objects and seven transformed nonambient lights")
	}
	group77 := s3sRadius(8, 4, 4)
	group88 := s3sRadius(2, 6, 8)
	s3sLightRadius(t, f.state, 1, group77)
	s3sLightRadius(t, f.state, 2, group77)
	s3sLightRadius(t, f.state, 3, group88)
	s3sLightRadius(t, f.state, 4, 0.75)
	s3sLightRadius(t, f.state, 5, 0)
	s3sLightRadius(t, f.state, 6, 0)
	s3sLightRadius(t, f.state, 7, float32(math.NaN()))
	for index, x := range []float32{6, 3, 2, 1, 4, 5, 7} {
		if f.state.RtApp.Scene.Lights[index].Position[0] != x {
			t.Fatal("emitter aggregation changed directional/spot/intensity light ordering")
		}
	}
	if f.state.RtApp.Scene.AmbientLight != (mgl32.Vec3{0.2, 0.4, 0.6}) || f.state.SunIntensity != 1 || f.state.SunDirection.Len() == 0 {
		t.Fatal("emitter aggregation changed ambient or Sun extraction")
	}
	s3sVisits(t, f.state, 4)

	// Ordinary component field assignments have no publication marker. The
	// next core pass must still observe current bounds and linked membership.
	s3cComponent[TransformComponent](t, f.cmd, large).Scale = mgl32.Vec3{1, 4, 1}
	f.sync()
	group77 = s3sRadius(4, 16, 4)
	s3sLightRadius(t, f.state, 1, group77)
	s3sLightRadius(t, f.state, 2, group77)
	s3sVisits(t, f.state, 4)

	s3cComponent[VoxelModelComponent](t, f.cmd, large).EmitterLinkID = 88
	s3cComponent[VoxelModelComponent](t, f.cmd, other).OverrideGeometry = f.server.CreateCubeModel(20, 2, 2, 1)
	f.sync()
	s3sLightRadius(t, f.state, 1, s3sRadius(4, 4, 4))
	s3sLightRadius(t, f.state, 2, s3sRadius(4, 4, 4))
	s3sLightRadius(t, f.state, 3, s3sRadius(20, 2, 2))
	s3sVisits(t, f.state, 4)

	f.cmd.RemoveEntity(other)
	f.app.FlushCommands()
	f.sync()
	s3sLightRadius(t, f.state, 3, group77)
	s3sVisits(t, f.state, 3)

	MakeQuery2[LightComponent, TransformComponent](f.cmd).Map(func(_ EntityId, light *LightComponent, _ *TransformComponent) bool {
		if light.Type != LightTypeAmbient && !math.IsNaN(float64(light.SourceRadius)) {
			light.SourceRadius = 1
		}
		return true
	})
	// Ambient and missing-transform linked lights cannot request a scan;
	// explicit positive and NaN radii bypass automatic derivation.
	f.sync()
	s3sVisits(t, f.state, 0)
	s3sLightRadius(t, f.state, 1, 1)
}

func TestS3sEmitterRadiusVisitDiagnosticResetsWhenInputsUnavailable(t *testing.T) {
	f := newS3cVoxelFixture(t)
	model := f.voxelModel()
	model.EmitterLinkID = 77
	f.cmd.AddEntity(s3cTransform(0), model)
	f.cmd.AddEntity(s3cTransform(1), &LightComponent{Type: LightTypePoint, Intensity: 1, Color: [3]float32{1, 1, 1}, EmitterLinkID: 77})
	f.app.FlushCommands()
	renderer := f.state.RtApp
	scene := renderer.Scene
	for _, missing := range []string{"renderer", "scene", "commands"} {
		f.sync()
		s3sVisits(t, f.state, 1)
		switch missing {
		case "renderer":
			f.state.RtApp = nil
			syncVoxelRtLights(f.state, f.cmd)
			f.state.RtApp = renderer
		case "scene":
			renderer.Scene = nil
			syncVoxelRtLights(f.state, f.cmd)
			renderer.Scene = scene
		case "commands":
			syncVoxelRtLights(f.state, nil)
		}
		s3sVisits(t, f.state, 0)
	}
	syncVoxelRtLights(nil, f.cmd)
}
