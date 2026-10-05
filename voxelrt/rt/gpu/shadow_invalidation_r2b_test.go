package gpu

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
	"testing"
)

// Scheduling at the recording frame isolates invalidation from periodic work.
// Following frames separately check that unchanged cascades remain cached.
const r2bFrame = uint64(32)

func r2bObject(id uint32, p mgl32.Vec3) *core.VoxelObject {
	o := r2aObject(id, p[0])
	o.Transform.Position = p
	o.Transform.Dirty = true
	return o
}

func r2bFixture(t *testing.T) (*GpuBufferManager, *core.Scene, *core.CameraState) {
	t.Helper()
	objects := []*core.VoxelObject{r2bObject(1, mgl32.Vec3{0, 0, -20}), r2bObject(2, mgl32.Vec3{70, 0, -100}), r2bObject(3, mgl32.Vec3{600, 0, -20})}
	m, _ := scheduleFixture(t, objects...)
	scene := core.NewScene()
	scene.Objects = objects
	scene.Lights = []core.Light{{Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}}, r2aLight(core.LightTypeSpot, 600)}
	scene.Lights[1].Position[2] = -20
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{}
	camera.LookAt = mgl32.Vec3{0, 0, -1}
	r2bWarm(t, m, scene, camera)
	// Check actual selected casters, as opposed to camera-visible objects.
	for _, o := range objects {
		r2bSelected(t, scene, o, true)
	}
	near := mgl32.Mat4(scene.Lights[0].DirectionalCascades[0].ViewProj)
	far := mgl32.Mat4(scene.Lights[0].DirectionalCascades[1].ViewProj)
	for _, tc := range []struct {
		o         *core.VoxelObject
		near, far bool
	}{{objects[0], true, true}, {objects[1], false, true}, {objects[2], false, false}} {
		p := tc.o.RenderWorldBounds()[0].Add(tc.o.RenderWorldBounds()[1]).Mul(.5).Vec4(1)
		n, f := near.Mul4x1(p), far.Mul4x1(p)
		inside := func(v mgl32.Vec4) bool { return v[0] >= -1 && v[0] <= 1 && v[1] >= -1 && v[1] <= 1 }
		if inside(n) != tc.near || inside(f) != tc.far {
			t.Fatalf("fixture cascade XY membership for %v: near=%v far=%v", tc.o.Transform.Position, inside(n), inside(f))
		}
	}
	return m, scene, camera
}

func r2bSelected(t *testing.T, s *core.Scene, o *core.VoxelObject, want bool) {
	t.Helper()
	got := false
	for _, p := range s.ShadowObjects {
		got = got || p == o
	}
	if got != want {
		t.Fatalf("caster %v selected=%v want %v", o.Transform.Position, got, want)
	}
}
func r2bWarm(t *testing.T, m *GpuBufferManager, s *core.Scene, c *core.CameraState) {
	t.Helper()
	r2aCommit(m, s, c)
	u := m.BuildShadowUpdates(s, c, r2bFrame, true)
	m.RecordShadowUpdates(u, r2bFrame, s.ShadowRevision())
	r2bAssert(t, m, s, c, r2bFrame, nil)
}
func r2bAssert(t *testing.T, m *GpuBufferManager, s *core.Scene, c *core.CameraState, frame uint64, want []uint32) []core.ShadowUpdate {
	t.Helper()
	expected := map[uint32]bool{}
	for _, i := range want {
		expected[i] = true
	}
	updates := m.BuildShadowUpdates(s, c, frame, false)
	actual := map[uint32]bool{}
	for _, u := range updates {
		if u.Kind == core.ShadowUpdateKindDirectional && u.LightIndex == 0 {
			if actual[u.CascadeIndex] {
				t.Errorf("duplicate cascade %d", u.CascadeIndex)
			}
			actual[u.CascadeIndex] = true
		}
	}
	for i := uint32(0); i < core.DirectionalShadowCascadeCount; i++ {
		if actual[i] != expected[i] {
			t.Errorf("cascade %d scheduled=%v want %v at frame %d", i, actual[i], expected[i], frame)
		}
	}
	return updates
}

func TestR2bDirectionalCasterDependencies(t *testing.T) {
	for _, change := range []string{"far revision", "near revision", "remote revision", "insert far", "remove far", "move within far", "move across", "disable casting", "disable rendering", "selected map", "map identity", "transform", "metadata", "geometry allocation", "material allocation", "removed dependency"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2bFixture(t)
			o := s.Objects[1]
			want := []uint32{1}
			switch change {
			case "far revision":
				o.XBrickMap.Revision++
			case "near revision":
				s.Objects[0].XBrickMap.Revision++
				want = []uint32{0, 1}
			case "remote revision":
				s.Objects[2].XBrickMap.Revision++
				want = nil
			case "insert far":
				s.Objects = append(s.Objects, r2bObject(4, mgl32.Vec3{72, 0, -100}))
			case "remove far":
				s.Objects = append(s.Objects[:1], s.Objects[2:]...)
			case "move within far":
				o.Transform.Position[0]++
				o.Transform.Dirty = true
			case "move across":
				o.Transform.Position = mgl32.Vec3{1, 0, -20}
				o.Transform.Dirty = true
				want = []uint32{0, 1}
			case "disable casting":
				o.CastsShadows = false
			case "disable rendering":
				o.RenderEnabled = false
			case "selected map":
				o.XBrickMap = c3h9Map(9, [3]int{})
				o.XBrickMap.ClearDirty()
			case "map identity":
				o.XBrickMap.ID++
			case "transform":
				o.Transform.Rotation = mgl32.QuatRotate(1.5707963, mgl32.Vec3{0, 1, 0})
				o.Transform.Dirty = true
			case "metadata":
				o.EmitterLinkID++
			case "geometry allocation":
				a := *m.Allocations[o.XBrickMap]
				m.Allocations[o.XBrickMap] = &a
			case "material allocation":
				a := *m.MaterialAllocations[o]
				m.MaterialAllocations[o] = &a
			case "removed dependency":
				s.Objects = append(s.Objects[:1], s.Objects[2:]...)
				r2bWarm(t, m, s, c)
				o.XBrickMap.Revision++
				want = nil
			}
			r2aCommit(m, s, c)
			r2bAssert(t, m, s, c, r2bFrame, want)
		})
	}
}

func TestR2bDirectionalUploadDependencies(t *testing.T) {
	for _, change := range []string{"far geometry", "remote geometry", "near geometry", "shared geometry", "progressive", "failed", "skipped", "unknown", "unknown then tracked", "unknown during tracked"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2bFixture(t)
			o := s.Objects[1]
			want := []uint32{1}
			if change == "remote geometry" {
				o = s.Objects[2]
				want = nil
			}
			if change == "near geometry" {
				o = s.Objects[0]
				want = []uint32{0, 1}
			}
			if change == "shared geometry" {
				s.Objects[0].XBrickMap = o.XBrickMap
				r2bWarm(t, m, s, c)
				want = []uint32{0, 1}
			}
			if change == "unknown" || change == "unknown then tracked" {
				m.VoxelUploadRevision++
				want = []uint32{0, 1}
			}
			if change != "unknown" {
				o.XBrickMap.DirtyBricks[[6]int{}] = true
			}
			if change == "skipped" {
				m.SetVoxelUploadBudget(VoxelUploadBudget{})
			}
			before := m.VoxelUploadRevision
			calls := 0
			m.serviceVoxelUploads(s, func(w voxelUploadWork) bool {
				calls++
				if change == "unknown during tracked" {
					m.VoxelUploadRevision++
					want = []uint32{0, 1}
				}
				return change != "failed"
			})
			if change == "failed" || change == "skipped" {
				want = nil
				if m.VoxelUploadRevision != before {
					t.Fatal("failed/skipped upload published a revision")
				}
			} else if change != "unknown" && (calls == 0 || m.VoxelUploadBytes == 0 || m.VoxelUploadRevision == before) {
				t.Fatal("fixture did not publish successful upload")
			}
			r2aCommit(m, s, c)
			u := r2bAssert(t, m, s, c, r2bFrame, want)
			if change == "progressive" {
				m.RecordShadowUpdates(u, r2bFrame, s.ShadowRevision())
				o.XBrickMap.DirtyBricks[[6]int{}] = true
				scheduleRun(t, m, s)
				r2aCommit(m, s, c)
				r2bAssert(t, m, s, c, r2bFrame, []uint32{1})
			}
		})
	}
}

func TestR2bDirectionalOpacityUploads(t *testing.T) {
	for _, change := range []string{"identical", "color only", "opacity", "failed opacity"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2bFixture(t)
			o := s.Objects[1]
			want := []uint32(nil)
			if change == "color only" {
				o.MaterialTable[1].BaseColor[0] = 64
			}
			if change == "opacity" || change == "failed opacity" {
				o.MaterialTable[1].Transparency = 1
				if change == "opacity" {
					want = []uint32{1}
				}
			}
			m.MaterialBufferGeneration++
			calls := 0
			m.serviceVoxelUploads(s, func(w voxelUploadWork) bool {
				calls++
				if w.kind != voxelUploadMaterial {
					t.Fatal("palette-only fixture requested geometry")
				}
				return change != "failed opacity"
			})
			if calls != len(s.Objects) {
				t.Fatalf("palette refresh attempted %d objects want %d", calls, len(s.Objects))
			}
			r2aCommit(m, s, c)
			r2bAssert(t, m, s, c, r2bFrame, want)
		})
	}
}

func TestR2bExactDirectionalInputs(t *testing.T) {
	for _, change := range []string{"direction", "emitter link", "ViewProj near", "ViewProj far", "InvViewProj near", "InvViewProj far", "Params near", "Params far", "resolution near", "resolution far"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2bFixture(t)
			want := []uint32{0, 1}
			index := 0
			if change == "direction" {
				s.Lights[0].Direction[0] = .1
				r2aCommit(m, s, c)
			} else if change == "emitter link" {
				s.Lights[0].ShadowMeta[3]++
			} else {
				if change == "ViewProj far" || change == "InvViewProj far" || change == "Params far" || change == "resolution far" {
					index = 1
				}
				want = []uint32{uint32(index)}
				cascade := &s.Lights[0].DirectionalCascades[index]
				switch change {
				case "ViewProj near", "ViewProj far":
					cascade.ViewProj[12] += .0001
				case "InvViewProj near", "InvViewProj far":
					cascade.InvViewProj[12] += .0001
				case "Params near", "Params far":
					cascade.Params[2] += .0001
				case "resolution near", "resolution far":
					m.ShadowLayerParams[s.Lights[0].ShadowMeta[0]+uint32(index)].EffectiveResolution /= 2
				}
			}
			r2bAssert(t, m, s, c, r2bFrame, want)
		})
	}
}

func TestR2bDirectionalReuseAndForce(t *testing.T) {
	m, s, c := r2bFixture(t)
	s.Objects[2].XBrickMap.DirtyBricks[[6]int{}] = true
	scheduleRun(t, m, s)
	r2aCommit(m, s, c)
	r2bAssert(t, m, s, c, r2bFrame, nil)
	r2bAssert(t, m, s, c, r2bFrame+1, nil)
	r2bAssert(t, m, s, c, r2bFrame+2, nil)
	u := m.BuildShadowUpdates(s, c, r2bFrame, true)
	seen := map[uint32]bool{}
	for _, v := range u {
		if v.Kind == core.ShadowUpdateKindDirectional {
			seen[v.CascadeIndex] = true
		}
	}
	if !seen[0] || !seen[1] {
		t.Fatal("force refresh omitted a cascade")
	}
}

func TestR2bDirectionalRemoteSpotCasterRayCoverage(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    mgl32.Vec3
		want []uint32
	}{
		{"outside XY", mgl32.Vec3{600, -1000, -20}, nil},
		// Directional traversal is unbounded downstream, even though stored depth
		// is clamped; selected local-light casters remain dependencies on those rays.
		{"beyond far plane", mgl32.Vec3{0, -1000, -20}, []uint32{0, 1}},
		{"upstream offscreen", mgl32.Vec3{0, 100, -20}, []uint32{0, 1}},
		{"before ray origin", mgl32.Vec3{0, 1000, -20}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, s, c := r2bFixture(t)
			o := s.Objects[2]
			o.Transform.Position = tc.p
			o.Transform.Dirty = true
			s.Lights[1].Position = [4]float32{tc.p[0], tc.p[1] + 10, tc.p[2], 1}
			r2bWarm(t, m, s, c)
			r2bSelected(t, s, o, true)
			o.XBrickMap.DirtyBricks[[6]int{}] = true
			scheduleRun(t, m, s)
			r2aCommit(m, s, c)
			r2bAssert(t, m, s, c, r2bFrame, tc.want)
		})
	}
}

func TestR2bDirectionalSelectedGroupReplacement(t *testing.T) {
	m, s, c := r2bFixture(t)
	original := s.Objects[1]
	replacement := r2bObject(4, mgl32.Vec3{71, 0, -100})
	for _, o := range []*core.VoxelObject{original, replacement} {
		o.ShadowCasterGroupID = 7
		o.ShadowCasterGroupLimit = 1
	}
	s.Objects = append(s.Objects, replacement)
	r2bWarm(t, m, s, c)
	r2bSelected(t, s, original, true)
	r2bSelected(t, s, replacement, false)
	// A selected member disappearing publishes the existing group's next choice.
	original.ShadowMaxDistance = 1
	r2aCommit(m, s, c)
	r2bSelected(t, s, original, false)
	r2bSelected(t, s, replacement, true)
	r2bAssert(t, m, s, c, r2bFrame, []uint32{1})
	u := m.BuildShadowUpdates(s, c, r2bFrame, false)
	m.RecordShadowUpdates(u, r2bFrame, s.ShadowRevision())
	original.XBrickMap.Revision++
	r2aCommit(m, s, c)
	r2bAssert(t, m, s, c, r2bFrame, nil)
}

func TestR2bDirectionalCacheAssignment(t *testing.T) {
	for _, change := range []string{"reshuffle", "same projection new owner", "disable reenable", "remove readd", "reset"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2bFixture(t)
			switch change {
			case "reshuffle":
				other := s.Lights[0]
				other.Direction = [4]float32{.2, -1, 0, 0}
				s.Lights = append(s.Lights, other)
				r2bWarm(t, m, s, c)
				s.Lights[0], s.Lights[2] = s.Lights[2], s.Lights[0]
			case "same projection new owner":
				other := s.Lights[0]
				other.ShadowMeta[3] = 42
				s.Lights = append(s.Lights, other)
				r2bWarm(t, m, s, c)
				s.Lights[0], s.Lights[2] = s.Lights[2], s.Lights[0]
			case "disable reenable":
				s.Lights[0].Params[3] = 0
				r2aCommit(m, s, c)
				s.Lights[0].Params[3] = 1
			case "remove readd":
				lights := s.Lights
				s.Lights = s.Lights[1:]
				r2aCommit(m, s, c)
				s.Lights = lights
			case "reset":
				m.invalidateShadowCache()
			}
			r2aCommit(m, s, c)
			r2bAssert(t, m, s, c, r2bFrame, []uint32{0, 1})
		})
	}
}

func TestR2bDirectionalPartialRecordPreservesPendingCascade(t *testing.T) {
	m, s, c := r2bFixture(t)
	s.Objects[0].XBrickMap.Revision++
	r2aCommit(m, s, c)
	u := r2bAssert(t, m, s, c, r2bFrame, []uint32{0, 1})
	var far []core.ShadowUpdate
	for _, update := range u {
		if update.Kind == core.ShadowUpdateKindDirectional && update.CascadeIndex == 1 {
			far = append(far, update)
		}
	}
	if len(far) != 1 {
		t.Fatal("fixture must schedule one far cascade")
	}
	m.RecordShadowUpdates(far, r2bFrame, s.ShadowRevision())
	r2bAssert(t, m, s, c, r2bFrame, []uint32{0})
}

func TestR2bDirectionalMalformedProjectionFallback(t *testing.T) {
	for _, name := range []string{"singular inverse", "perspective inverse"} {
		t.Run(name, func(t *testing.T) {
			m, s, c := r2bFixture(t)
			// Public matrices can be replaced in place without UpdateLights. An inverse
			// that cannot describe the supported orthographic prism needs safe membership.
			if name == "singular inverse" {
				s.Lights[0].DirectionalCascades[1].InvViewProj = [16]float32{}
			} else {
				s.Lights[0].DirectionalCascades[1].InvViewProj = [16]float32(mgl32.Perspective(1, 1, .1, 100).Inv())
			}
			u := r2bAssert(t, m, s, c, r2bFrame, []uint32{1})
			m.RecordShadowUpdates(u, r2bFrame, s.ShadowRevision())
			s.Objects[2].XBrickMap.Revision++
			// Commit updates authoritative selected casters; keep the caller's matrix.
			s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{CameraPosition: c.Position})
			r2bSelected(t, s, s.Objects[2], true)
			r2bAssert(t, m, s, c, r2bFrame, []uint32{1})
		})
	}
}
