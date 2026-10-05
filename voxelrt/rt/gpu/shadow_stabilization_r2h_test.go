package gpu

import (
	"fmt"
	"math"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func r2hCamera() *core.CameraState {
	c := core.NewCameraState()
	c.Position = mgl32.Vec3{0, 4, 45}
	c.LookAt = mgl32.Vec3{0, 4, 0}
	return c
}
func r2hMove(c *core.CameraState, delta mgl32.Vec3) {
	c.Position = c.Position.Add(delta)
	c.LookAt = c.LookAt.Add(delta)
}
func r2hScene(dir mgl32.Vec3) (*GpuBufferManager, *core.Scene, *core.CameraState) {
	m := &GpuBufferManager{}
	s := core.NewScene()
	c := r2hCamera()
	s.Lights = []core.Light{{Direction: [4]float32{dir[0], dir[1], dir[2], 0}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}}}
	m.UpdateLights(s, c, 1)
	u := m.BuildShadowUpdates(s, c, 0, false)
	m.RecordShadowUpdates(u, 0, s.ShadowRevision())
	return m, s, c
}
func r2hWantUpdates(t *testing.T, m *GpuBufferManager, s *core.Scene, c *core.CameraState, frame uint64, want ...uint32) []core.ShadowUpdate {
	t.Helper()
	u := m.BuildShadowUpdates(s, c, frame, false)
	got := map[uint32]bool{}
	for _, update := range u {
		if update.Kind != core.ShadowUpdateKindDirectional {
			t.Fatal("unexpected local work")
		}
		got[update.CascadeIndex] = true
	}
	if len(u) != len(want) {
		t.Errorf("scheduled %d cascades want %v", len(u), want)
	}
	for _, i := range want {
		if !got[i] {
			t.Errorf("cascade %d missing from %+v", i, u)
		}
	}
	return u
}

func TestR2hInsideWorldTexelCellPreservesExactCascadeInputsAndCache(t *testing.T) {
	for _, dir := range []mgl32.Vec3{{0, -1, 0}, {0, -1, -.25}} {
		t.Run(fmt.Sprint(dir), func(t *testing.T) {
			m, s, c := r2hScene(dir.Normalize())
			before := s.Lights[0].DirectionalCascades
			// World X is perpendicular to both fixture light directions and starts at
			// the grid's zero coordinate, safely away from the rounding boundaries.
			step := float32(.005)
			if step >= before[0].Params[1]*.5 || step >= before[1].Params[1]*.5 {
				t.Fatal("fixture movement must stay within both light-space texel cells")
			}
			r2hMove(c, mgl32.Vec3{step, 0, 0})
			m.UpdateLights(s, c, 1)
			for i, cascade := range s.Lights[0].DirectionalCascades {
				if cascade != before[i] {
					t.Errorf("cascade %d changed exact projection/inverse/params inside world texel cell", i)
				}
			}
			r2hWantUpdates(t, m, s, c, 1000)
		})
	}
}

func TestR2hNearGridCrossingRefreshesNearCascadeWithoutWakingFar(t *testing.T) {
	for _, dir := range []mgl32.Vec3{{0, -1, 0}, {0, -1, -.25}} {
		t.Run(fmt.Sprint(dir), func(t *testing.T) {
			m, s, c := r2hScene(dir.Normalize())
			before := s.Lights[0].DirectionalCascades
			step := before[0].Params[1] * .6
			if step <= before[0].Params[1]*.5 || step >= before[1].Params[1]*.5 {
				t.Fatal("fixture must cross only near grid boundary")
			}
			r2hMove(c, mgl32.Vec3{step, 0, 0})
			m.UpdateLights(s, c, 1)
			if s.Lights[0].DirectionalCascades[0] == before[0] {
				t.Error("near boundary did not change near projection")
			}
			if s.Lights[0].DirectionalCascades[1] != before[1] {
				t.Error("near boundary changed far projection")
			}
			u := r2hWantUpdates(t, m, s, c, 1000, 0)
			m.RecordShadowUpdates(u, 1000, s.ShadowRevision())
			r2hWantUpdates(t, m, s, c, 2000)
			// Crossing the far grid from origin also moves through several near cells.
			m, s, c = r2hScene(dir.Normalize())
			r2hMove(c, mgl32.Vec3{before[1].Params[1] * .6, 0, 0})
			m.UpdateLights(s, c, 1)
			r2hWantUpdates(t, m, s, c, 1000, 0, 1)
		})
	}
}

func TestR2hLightXYTranslationsAreResolutionAwareWholeTexels(t *testing.T) {
	for _, resolution := range []uint32{2, 256, 512, 1024} {
		for _, dir := range []mgl32.Vec3{{0, -1, 0}, {0, -1, -.25}} {
			t.Run(fmt.Sprintf("res%d/%v", resolution, dir), func(t *testing.T) {
				dir = dir.Normalize()
				base, volume := buildDirectionalShadowCascade(r2hCamera(), 1, dir, 0, 48, resolution)
				texel := base.Params[1]
				axis := -1
				for row := 0; row < 2; row++ {
					if math.Abs(float64(volume.View.At(row, 0))) > .99 {
						axis = row
					}
				}
				if axis < 0 {
					t.Fatal("fixture world X must align with one light XY axis")
				}
				sign := volume.View.At(axis, 0)
				for _, cells := range []float32{-.49, -.5, -.6, -1.5, -1.6, .49, .5, .6, 1.5, 1.6} {
					c := r2hCamera()
					r2hMove(c, mgl32.Vec3{cells * texel, 0, 0})
					cascade, current := buildDirectionalShadowCascade(c, 1, dir, 0, 48, resolution)
					if cascade.Params != base.Params {
						t.Errorf("translated fit changed params at %v texels", cells)
					}
					delta := current.View.At(axis, 3) - volume.View.At(axis, 3)
					want := -sign * float32(math.Round(float64(cells))) * texel
					if math.Abs(float64(delta-want)) > float64(texel)*1e-4 {
						t.Errorf("%v texel movement: light XY translation=%v want snapped %v (round away from zero)", cells, delta, want)
					}
				}
			})
		}
	}
}

func TestR2hFitExtentDoesNotDependOnWorldTranslation(t *testing.T) {
	for _, dir := range []mgl32.Vec3{{0, -1, 0}, {0, -1, -.25}, {.3, -1, .2}} {
		t.Run(fmt.Sprint(dir), func(t *testing.T) {
			dir = dir.Normalize()
			base, baseVolume := buildDirectionalShadowCascade(r2hCamera(), 16.0/9, dir, 0, 48, 1024)
			for _, origin := range []mgl32.Vec3{{1000, 0, 0}, {-1000, 0, 0}, {1000, 1000, -1000}, {-1000, -1000, 1000}} {
				c := r2hCamera()
				r2hMove(c, origin)
				cascade, volume := buildDirectionalShadowCascade(c, 16.0/9, dir, 0, 48, 1024)
				if cascade.Params != base.Params || volume.HalfExtent != baseVolume.HalfExtent {
					t.Errorf("world origin %v changed translation-independent fit: params %v vs %v, extent %v vs %v", origin, cascade.Params, base.Params, volume.HalfExtent, baseVolume.HalfExtent)
				}
			}
		})
	}
}

func TestR2hLightDepthMotionIsNotSnappedOrCached(t *testing.T) {
	for _, dir := range []mgl32.Vec3{{0, -1, 0}, {0, -1, -.25}} {
		t.Run(fmt.Sprint(dir), func(t *testing.T) {
			dir = dir.Normalize()
			m, s, c := r2hScene(dir)
			before := s.Lights[0].DirectionalCascades
			r2hMove(c, dir.Mul(.005))
			m.UpdateLights(s, c, 1)
			for i, cascade := range s.Lights[0].DirectionalCascades {
				if cascade == before[i] {
					t.Errorf("cascade %d incorrectly snapped light-depth movement", i)
				}
			}
			r2hWantUpdates(t, m, s, c, 1000, 0, 1)
		})
	}
}

func TestR2hCameraAndSunInputChangesStillInvalidateDirectionalMaps(t *testing.T) {
	for _, change := range []string{"orientation", "Fov", "aspect", "near", "far", "sun direction"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2hScene(mgl32.Vec3{0, -1, 0})
			before := s.Lights[0].DirectionalCascades
			aspect := float32(1)
			switch change {
			case "orientation":
				c.LookAt[0] += 5
			case "Fov":
				c.Fov += 5
			case "aspect":
				aspect = 1.3
			case "near":
				c.Near = 2
			case "far":
				c.Far = 130
			case "sun direction":
				s.Lights[0].Direction[0] = .1
			}
			m.UpdateLights(s, c, aspect)
			want := []uint32{0, 1}
			if change == "near" {
				want = []uint32{0}
			} else if change == "far" {
				want = []uint32{1}
			}
			expected := map[uint32]bool{}
			for _, index := range want {
				expected[index] = true
			}
			for i, cascade := range s.Lights[0].DirectionalCascades {
				if (cascade != before[i]) != expected[uint32(i)] {
					t.Errorf("%s input changed cascade %d=%v, want %v", change, i, cascade != before[i], expected[uint32(i)])
				}
			}
			r2hWantUpdates(t, m, s, c, 1000, want...)
		})
	}
}

func r2hCheckCoverage(t *testing.T, c *core.CameraState, aspect float32, dir mgl32.Vec3, res uint32, near, far float32) {
	t.Helper()
	cascade, volume := buildDirectionalShadowCascade(c, aspect, dir.Normalize(), near, far, res)
	vp, inv := mgl32.Mat4(cascade.ViewProj), mgl32.Mat4(cascade.InvViewProj)
	for _, matrix := range []mgl32.Mat4{vp, inv, volume.View} {
		for _, value := range matrix {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				t.Fatal("cascade or cull matrix is nonfinite")
			}
		}
	}
	product := vp.Mul4(inv)
	identity := mgl32.Ident4()
	for i, v := range product {
		if math.Abs(float64(v-identity[i])) > 1e-3 {
			t.Errorf("projection inverse does not round trip element %d: %v", i, v)
		}
	}
	expectedVP := mgl32.Ortho(-volume.HalfExtent, volume.HalfExtent, -volume.HalfExtent, volume.HalfExtent, volume.NearPlane, volume.FarPlane).Mul4(volume.View)
	if expectedVP != vp {
		t.Error("cull view and render projection disagree")
	}
	for i, corner := range cameraSliceCorners(c, aspect, maxf(c.NearPlane(), near), minf(c.FarPlane(), far)) {
		clip := vp.Mul4x1(corner.Vec4(1))
		x, y := clip[0]/clip[3], clip[1]/clip[3]
		if math.Abs(float64(x)) > 1.00002 || math.Abs(float64(y)) > 1.00002 {
			t.Errorf("resolution %d aspect %v corner %d outside rendered XY: (%v,%v)", res, aspect, i, x, y)
		}
	}
	if cascade.Params[0] != far || cascade.Params[2] != 2/(directionalShadowDepth-directionalShadowNear) || volume.NearPlane != directionalShadowNear || volume.FarPlane != directionalShadowDepth {
		t.Error("split/depth/near semantics changed")
	}
}

func TestR2hSnappedProjectionConservativelyCoversFrustumCorners(t *testing.T) {
	for _, aspect := range []float32{.5, 1, 16.0 / 9, 3} {
		for _, dir := range []mgl32.Vec3{{0, -1, 0}, {.001, -1, .001}, {0, -1, -.25}, {.3, -1, .2}} {
			t.Run(fmt.Sprintf("aspect%v/dir%v", aspect, dir), func(t *testing.T) {
				c := r2hCamera()
				base, _ := buildDirectionalShadowCascade(c, aspect, dir.Normalize(), 0, 48, 1024)
				// Put the center almost half a cell from an anchor; each side must remain covered.
				r2hMove(c, mgl32.Vec3{base.Params[1] * .49, 0, 0})
				r2hCheckCoverage(t, c, aspect, dir, 1024, 0, 48)
				r2hCheckCoverage(t, c, aspect, dir, 1024, 48, 160)
			})
		}
	}
}

func TestR2hLowAndDefaultResolutionsStayFiniteAndConservative(t *testing.T) {
	for _, res := range []uint32{0, 1, 2} {
		for _, aspect := range []float32{.5, 1, 3} {
			t.Run(fmt.Sprintf("res%d/aspect%v", res, aspect), func(t *testing.T) {
				dir := mgl32.Vec3{0, -1, -.25}.Normalize()
				c := r2hCamera()
				base, _ := buildDirectionalShadowCascade(c, aspect, dir, 0, 48, res)
				r2hMove(c, mgl32.Vec3{base.Params[1] * .49, 0, 0})
				r2hCheckCoverage(t, c, aspect, dir, res, 0, 48)
				if res == 0 {
					explicit, _ := buildDirectionalShadowCascade(c, aspect, dir, 0, 48, 1024)
					defaulted, _ := buildDirectionalShadowCascade(c, aspect, dir, 0, 48, 0)
					if explicit != defaulted {
						t.Error("resolution zero did not retain default 1024")
					}
				}
				if res == 1 {
					moved, _ := buildDirectionalShadowCascade(c, aspect, dir, 0, 48, 1)
					if moved == base {
						t.Error("resolution one fallback incorrectly quantized unsupported grid")
					}
				}
			})
		}
	}
}

func TestR2hSignedZeroAndNegativeWorldCellsRetainExactCacheKeys(t *testing.T) {
	for _, dir := range []mgl32.Vec3{{0, -1, 0}, {0, -1, -.25}} {
		for _, negativeCell := range []bool{false, true} {
			t.Run(fmt.Sprintf("dir%v/negative%v", dir, negativeCell), func(t *testing.T) {
				m, s, c := r2hScene(dir.Normalize())
				if negativeCell {
					// Start well inside negative near and far cells, then warm actual keys there.
					offset := -4 * s.Lights[0].DirectionalCascades[0].Params[1]
					r2hMove(c, mgl32.Vec3{offset, 0, 0})
					m.UpdateLights(s, c, 1)
					u := m.BuildShadowUpdates(s, c, 1, false)
					m.RecordShadowUpdates(u, 1, s.ShadowRevision())
				}
				baseline := s.Lights[0].DirectionalCascades
				r2hMove(c, mgl32.Vec3{.005, 0, 0})
				m.UpdateLights(s, c, 1)
				r2hWantUpdates(t, m, s, c, 1000)
				r2hMove(c, mgl32.Vec3{-.01, 0, 0})
				m.UpdateLights(s, c, 1)
				// Numeric matrix equality alone cannot distinguish positive and negative zero;
				// Build compares the exact published scalar bits used by dependency keys.
				for i, cascade := range s.Lights[0].DirectionalCascades {
					if cascade != baseline[i] {
						t.Errorf("cascade %d changed inside unchanged signed world cell", i)
					}
				}
				r2hWantUpdates(t, m, s, c, 2000)
			})
		}
	}
}

func TestR2hOffAxisRolledCamerasKeepConservativeCoverageAtSignedOrigins(t *testing.T) {
	for _, dir := range []mgl32.Vec3{{0, -1, 0}, {.001, -1, .001}, {0, -1, -.25}, {.3, -1, .2}} {
		for _, aspect := range []float32{.5, 16.0 / 9, 3} {
			t.Run(fmt.Sprintf("aspect%v/dir%v", aspect, dir), func(t *testing.T) {
				for _, origin := range []mgl32.Vec3{{0, 0, 0}, {1000, 1000, -1000}, {-1000, -1000, 1000}} {
					for _, offset := range []mgl32.Vec3{{.005, .005, 0}, {-.005, -.005, 0}, {-.005, .005, 0}} {
						c := r2hCamera()
						c.LookAt = c.Position.Add(mgl32.Vec3{5, 2, -45})
						c.Up = mgl32.Vec3{.2, 1, .1}
						r2hMove(c, origin.Add(offset))
						t.Run(fmt.Sprintf("origin%v/offset%v", origin, offset), func(t *testing.T) {
							r2hCheckCoverage(t, c, aspect, dir, 1024, 0, 48)
							r2hCheckCoverage(t, c, aspect, dir, 1024, 48, 160)
						})
					}
				}
			})
		}
	}
}
