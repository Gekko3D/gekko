package gpu

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

// This diagnostic counts evaluated XYZ affine row dot-products, not membership
// calls or matrix preparations. Eight legacy corners cost 24; two endpoints
// per row cost six, even when a lateral row would already prove exclusion.
func r2mProjectionCount(m *GpuBufferManager) uint64 {
	return m.ShadowDirectionalBoundsProjectionCount
}

func r2mProjectionDelta(t *testing.T, m *GpuBufferManager, before, want uint64) {
	t.Helper()
	if got := r2mProjectionCount(m) - before; got != want {
		t.Fatalf("directional affine row projections=%d, want %d", got, want)
	}
}

func r2mFixture(t *testing.T, n int) (*GpuBufferManager, *core.Scene, *core.CameraState) {
	t.Helper()
	objects := make([]*core.VoxelObject, n)
	for i := range objects {
		objects[i] = r2bObject(uint32(i+1), mgl32.Vec3{0, 0, -20})
		// Shared content keeps this work-count fixture cheap; selected occurrences
		// still have independently live scalar inputs and bounds.
		if i > 0 {
			objects[i].XBrickMap = objects[0].XBrickMap
		}
	}
	m, _ := scheduleFixture(t, objects...)
	s := core.NewScene()
	s.Objects = objects
	s.Lights = []core.Light{{Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}}}
	c := core.NewCameraState()
	c.LookAt = mgl32.Vec3{0, 0, -1}
	r2aCommit(m, s, c)
	if len(s.ShadowObjects) != n || len(m.ShadowLayerParams) != core.DirectionalShadowCascadeCount {
		t.Fatalf("fixture selected %d/%d casters and %d layers", len(s.ShadowObjects), n, len(m.ShadowLayerParams))
	}
	return m, s, c
}

func r2mSchedule(t *testing.T, m *GpuBufferManager, s *core.Scene, c *core.CameraState, frame uint64, want []uint32) []core.ShadowUpdate {
	t.Helper()
	u := r2bAssert(t, m, s, c, frame, want)
	if len(u) != len(want) {
		t.Fatalf("updates=%+v, want only cascades %v", u, want)
	}
	return u
}

// The reference intentionally evaluates all eight corners. Standard float64
// matrix inversion is the oracle, rather than production prism construction or
// its validation rules. Its inputs here are known supported affine matrices.
func r2mCornerMembership(inverse [16]float32, bounds [2]mgl32.Vec3) bool {
	var matrix mgl64.Mat4
	for i, v := range inverse {
		matrix[i] = float64(v) / float64(inverse[15])
	}
	projection := matrix.Inv()
	low := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	high := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	guard := [3]float64{1e-4, 1e-4, 1e-4}
	for corner := 0; corner < 8; corner++ {
		p := [4]float64{float64(bounds[corner&1][0]), float64(bounds[(corner>>1)&1][1]), float64(bounds[(corner>>2)&1][2]), 1}
		for row := 0; row < 3; row++ {
			// Preserve the reference's left-associated XYZ then translation arithmetic.
			value := projection[row]*p[0] + projection[4+row]*p[1] + projection[8+row]*p[2] + projection[12+row]
			magnitude := 0.0
			for column := 0; column < 4; column++ {
				magnitude += math.Abs(projection[4*column+row] * p[column])
			}
			low[row] = math.Min(low[row], value)
			high[row] = math.Max(high[row], value)
			guard[row] = math.Max(guard[row], magnitude*1e-4)
		}
	}
	return high[0] >= -1-guard[0] && low[0] <= 1+guard[0] && high[1] >= -1-guard[1] && low[1] <= 1+guard[1] && high[2] >= -1-guard[2]
}

func r2mCheckMembership(t *testing.T, inverses [2][16]float32, bounds [2]mgl32.Vec3, explicit []uint32, checkExplicit bool) {
	t.Helper()
	m, s, c := r2mFixture(t, 1)
	// Scene already owns and selected this caster. Replace public cascade inverses
	// after UpdateLights, and do not re-commit/re-cull against the custom geometry.
	s.Objects[0].WorldAABB = &bounds
	for i := range inverses {
		s.Lights[0].DirectionalCascades[i].InvViewProj = inverses[i]
	}
	u := m.BuildShadowUpdates(s, c, 32, true)
	if len(u) != 2 {
		t.Fatalf("cold custom projection updates=%+v", u)
	}
	m.RecordShadowUpdates(u, 32, s.ShadowRevision())
	r2mSchedule(t, m, s, c, 33, nil)
	want := []uint32{}
	for i := range inverses {
		if r2mCornerMembership(s.Lights[0].DirectionalCascades[i].InvViewProj, bounds) {
			want = append(want, uint32(i))
		}
	}
	if checkExplicit {
		if fmt.Sprint(want) != fmt.Sprint(explicit) {
			t.Fatalf("oracle=%v, explicit geometry expectation=%v", want, explicit)
		}
	}
	// A scalar edit probes cached membership through actual scheduled maps.
	s.Objects[0].EmitterLinkID++
	u = r2mSchedule(t, m, s, c, 1000, want)
	r2mSchedule(t, m, s, c, 1001, want)
	if len(u) > 0 {
		m.RecordShadowUpdates(u[:1], 1001, s.ShadowRevision())
		remaining := []uint32{}
		for _, index := range want {
			if index != u[0].CascadeIndex {
				remaining = append(remaining, index)
			}
		}
		r2mSchedule(t, m, s, c, 1002, remaining)
		m.RecordShadowUpdates(u[1:], 1002, s.ShadowRevision())
	}
	r2mSchedule(t, m, s, c, 1003, nil)
}

func TestR2mExhaustiveCornerGeometry(t *testing.T) {
	identity := [16]float32(mgl32.Ident4())
	// Different far cascade geometry catches accidental shared classification.
	shifted := identity
	shifted[12] = 3
	negzero := float32(math.Copysign(0, -1))
	cases := []struct {
		name   string
		bounds [2]mgl32.Vec3
		want   []uint32
	}{
		{"interior", [2]mgl32.Vec3{{-.2, -.3, -.4}, {.2, .3, .4}}, []uint32{0}},
		{"positive X boundary", [2]mgl32.Vec3{{1, 0, 0}, {1, 0, 0}}, []uint32{0}},
		{"negative X boundary", [2]mgl32.Vec3{{-1, 0, 0}, {-1, 0, 0}}, []uint32{0}},
		{"positive Y boundary", [2]mgl32.Vec3{{0, 1, 0}, {0, 1, 0}}, []uint32{0}},
		{"negative Y boundary", [2]mgl32.Vec3{{0, -1, 0}, {0, -1, 0}}, []uint32{0}},
		{"near plane contact", [2]mgl32.Vec3{{0, 0, -1}, {0, 0, -1}}, []uint32{0}},
		{"near plane guard", [2]mgl32.Vec3{{0, 0, -1.00005}, {0, 0, -1.00005}}, []uint32{0}},
		{"before near plane", [2]mgl32.Vec3{{0, 0, -1.001}, {0, 0, -1.001}}, nil},
		{"lateral guard", [2]mgl32.Vec3{{1.00005, 0, 0}, {1.00005, 0, 0}}, []uint32{0}},
		{"outside lateral guard", [2]mgl32.Vec3{{1.001, 0, 0}, {1.001, 0, 0}}, nil},
		{"unbounded downstream", [2]mgl32.Vec3{{0, 0, 1e8}, {.01, .01, 1e8}}, []uint32{0}},
		{"off prism downstream", [2]mgl32.Vec3{{10, 0, 1e8}, {11, 1, 1e8}}, nil},
		{"far cascade only", [2]mgl32.Vec3{{3, 0, 0}, {3.1, .1, .1}}, []uint32{1}},
		{"spans both cascades", [2]mgl32.Vec3{{-2, -.1, 0}, {4, .1, .1}}, []uint32{0, 1}},
		{"negative coordinates", [2]mgl32.Vec3{{-2, -2, -2}, {-.9, -.9, -.9}}, []uint32{0}},
		{"signed zero extent", [2]mgl32.Vec3{{negzero, negzero, negzero}, {0, 0, 0}}, []uint32{0}},
		{"thin bounds", [2]mgl32.Vec3{{-.25, -1.00005, 0}, {.25, -1.000049, 1e-8}}, []uint32{0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { r2mCheckMembership(t, [2][16]float32{identity, shifted}, tc.bounds, tc.want, true) })
	}
	rotated := mgl32.HomogRotate3DZ(.71).Mul4(mgl32.Scale3D(2, .4, 3))
	rotated[4] += .6 // shear, mixed-sign rows and nonuniform axes
	rotated[12], rotated[13], rotated[14] = -7, -3, 2
	cancellation := mgl32.Ident4()
	cancellation[12], cancellation[13], cancellation[14] = 1e8, -1e8, 1e8
	// At this translation float32 position steps are eight: the cancellation
	// guard deliberately retains a zero-width box well outside unguarded XY.
	t.Run("large translation cancellation", func(t *testing.T) {
		b := [2]mgl32.Vec3{{1e8 + 64, -1e8, 1e8}, {1e8 + 64, -1e8, 1e8}}
		r2mCheckMembership(t, [2][16]float32{[16]float32(cancellation), [16]float32(cancellation)}, b, []uint32{0, 1}, true)
	})
	t.Run("rotation shear and negative translation", func(t *testing.T) {
		b := [2]mgl32.Vec3{{-8, -4, 1}, {-6, -2, 3}}
		r2mCheckMembership(t, [2][16]float32{[16]float32(rotated), identity}, b, nil, false)
	})
	t.Run("negative scale asymmetric rows", func(t *testing.T) {
		inverse := rotated.Mul4(mgl32.Scale3D(-1, 2, -.5))
		b := [2]mgl32.Vec3{{-8, -4, 1}, {-6, -2, 3}}
		r2mCheckMembership(t, [2][16]float32{[16]float32(inverse), identity}, b, nil, false)
	})
	t.Run("tiny supported axes", func(t *testing.T) {
		inverse := mgl32.Scale3D(1e-12, 1e-12, 1e-12)
		b := [2]mgl32.Vec3{{0, 0, 0}, {5e-13, 5e-13, 5e-13}}
		r2mCheckMembership(t, [2][16]float32{[16]float32(inverse), [16]float32(inverse)}, b, []uint32{0, 1}, true)
	})
	t.Run("negative homogeneous W", func(t *testing.T) {
		negative := rotated
		for i := range negative {
			negative[i] *= -2
		}
		b := [2]mgl32.Vec3{{-8, -4, 1}, {-6, -2, 3}}
		r2mCheckMembership(t, [2][16]float32{[16]float32(rotated), [16]float32(negative)}, b, nil, false)
	})
	t.Run("constant homogeneous W", func(t *testing.T) {
		doubled := rotated
		for i := range doubled {
			doubled[i] *= 2
		}
		b := [2]mgl32.Vec3{{-8, -4, 1}, {-6, -2, 3}}
		r2mCheckMembership(t, [2][16]float32{[16]float32(rotated), [16]float32(doubled)}, b, nil, false)
	})
}

func TestR2mExhaustiveCornerDeterministicAffineCases(t *testing.T) {
	rng := rand.New(rand.NewSource(0x52326d))
	for i := 0; i < 128; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			inverse := mgl32.HomogRotate3DZ(float32(rng.Float64() * 6)).Mul4(mgl32.HomogRotate3DY(float32(rng.Float64() * 6)))
			shear := mgl32.Ident4()
			shear[4], shear[8], shear[9] = float32(rng.Float64()-.5), float32(rng.Float64()-.5), float32(rng.Float64()-.5)
			inverse = inverse.Mul4(shear).Mul4(mgl32.Scale3D(float32(.2+rng.Float64()*3), float32(.2+rng.Float64()*3), float32(.2+rng.Float64()*3)))
			inverse[12], inverse[13], inverse[14] = float32(rng.Float64()*100-50), float32(rng.Float64()*100-50), float32(rng.Float64()*100-50)
			ray := mgl32.Vec4{float32(rng.Float64()*4 - 2), float32(rng.Float64()*4 - 2), float32(rng.Float64()*6 - 2), 1}
			if i%3 == 0 {
				// Deterministically sample both sides of lateral/near guard boundaries.
				axis := (i / 3) % 3
				ray[axis] = float32(1 + (rng.Float64()-.5)*.0005)
				if axis == 2 || i%2 == 0 {
					ray[axis] = -ray[axis]
				}
			}
			p := inverse.Mul4x1(ray).Vec3()
			extent := mgl32.Vec3{float32(rng.Float64() * .2), float32(rng.Float64() * .2), float32(rng.Float64() * .2)}
			if i%3 == 0 {
				extent = mgl32.Vec3{}
			}
			bounds := [2]mgl32.Vec3{p.Sub(extent), p.Add(extent)}
			other := inverse
			if i%2 == 0 {
				other[12] += 3
				other[13] -= 2
			}
			r2mCheckMembership(t, [2][16]float32{[16]float32(inverse), [16]float32(other)}, bounds, nil, false)
		})
	}
}

func TestR2mConservativeUnsupportedInputs(t *testing.T) {
	for _, name := range []string{"nil bounds", "reversed bounds", "NaN bounds", "infinite bounds", "perspective", "singular", "nonfinite matrix", "poorly conditioned"} {
		t.Run(name, func(t *testing.T) {
			m, s, c := r2mFixture(t, 1)
			bounds := [2]mgl32.Vec3{{100, 100, 100}, {101, 101, 101}}
			s.Objects[0].WorldAABB = &bounds
			inverse := mgl32.Ident4()
			switch name {
			case "nil bounds":
				s.Objects[0].WorldAABB = nil
			case "reversed bounds":
				bounds[1][0] = 99
			case "NaN bounds":
				bounds[0][2] = float32(math.NaN())
			case "infinite bounds":
				bounds[1][2] = float32(math.Inf(1))
			case "perspective":
				inverse = mgl32.Perspective(1, 1, .1, 100).Inv()
			case "singular":
				inverse = mgl32.Mat4{}
			case "nonfinite matrix":
				inverse[0] = float32(math.NaN())
			case "poorly conditioned":
				inverse[4] = 1
				inverse[5] = 1e-7
			}
			for i := range s.Lights[0].DirectionalCascades {
				s.Lights[0].DirectionalCascades[i].InvViewProj = [16]float32(inverse)
			}
			u := m.BuildShadowUpdates(s, c, 32, true)
			m.RecordShadowUpdates(u, 32, s.ShadowRevision())
			s.Objects[0].EmitterLinkID++
			u = r2mSchedule(t, m, s, c, 1000, []uint32{0, 1})
			m.RecordShadowUpdates(u[:1], 1000, s.ShadowRevision())
			r2mSchedule(t, m, s, c, 1001, []uint32{u[1].CascadeIndex})
		})
	}
}

func TestR2mProjectionWork(t *testing.T) {
	for _, n := range []int{32, 1000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			m, s, c := r2mFixture(t, n)
			r2mProjectionDelta(t, m, 0, uint64(12*n))
			u := m.BuildShadowUpdates(s, c, 32, true)
			m.RecordShadowUpdates(u, 32, s.ShadowRevision())
			before := r2mProjectionCount(m)
			r2mSchedule(t, m, s, c, 1000, nil)
			r2mProjectionDelta(t, m, before, 0)
			s.Objects[n/2].EmitterLinkID++
			u = r2mSchedule(t, m, s, c, 1001, []uint32{0, 1})
			r2mProjectionDelta(t, m, before, 0)
			m.RecordShadowUpdates(u, 1001, s.ShadowRevision())
			bounds := *s.Objects[n/2].WorldAABB
			bounds[0][0] += .001
			bounds[1][0] += .001
			s.Objects[n/2].WorldAABB = &bounds
			u = r2mSchedule(t, m, s, c, 1002, []uint32{0, 1})
			r2mProjectionDelta(t, m, before, 12)
			for frame := uint64(1003); frame < 1006; frame++ {
				r2mSchedule(t, m, s, c, frame, []uint32{0, 1})
			}
			r2mProjectionDelta(t, m, before, 12)
			m.RecordShadowUpdates(u, 1006, s.ShadowRevision())
			before = r2mProjectionCount(m)
			s.Lights[0].DirectionalCascades[1].Params[2] += .001
			u = r2mSchedule(t, m, s, c, 1007, []uint32{1})
			r2mProjectionDelta(t, m, before, uint64(6*n))
			r2mSchedule(t, m, s, c, 1008, []uint32{1})
			r2mProjectionDelta(t, m, before, uint64(6*n))
			m.RecordShadowUpdates(u, 1008, s.ShadowRevision())
		})
	}
	// Invalid bounds/projections do no affine row work, while still conservatively
	// retaining the selected caster. Test off-X valid bounds too: all rows count.
	for _, name := range []string{"off X", "nil", "reversed", "NaN", "infinity", "perspective", "singular", "nonfinite", "ill conditioned"} {
		t.Run(name, func(t *testing.T) {
			m, s, c := r2mFixture(t, 1)
			b := [2]mgl32.Vec3{{10, 0, 0}, {11, 1, 1}}
			s.Objects[0].WorldAABB = &b
			inverse := mgl32.Ident4()
			want := uint64(0)
			switch name {
			case "off X":
				want = 12
			case "nil":
				s.Objects[0].WorldAABB = nil
			case "reversed":
				b[1][2] = -1
			case "NaN":
				b[0][1] = float32(math.NaN())
			case "infinity":
				b[1][2] = float32(math.Inf(1))
			case "perspective":
				inverse = mgl32.Perspective(1, 1, .1, 100).Inv()
			case "singular":
				inverse = mgl32.Mat4{}
			case "nonfinite":
				inverse[12] = float32(math.Inf(1))
			case "ill conditioned":
				inverse[4] = 1
				inverse[5] = 1e-7
			}
			for i := range s.Lights[0].DirectionalCascades {
				s.Lights[0].DirectionalCascades[i].InvViewProj = [16]float32(inverse)
			}
			before := r2mProjectionCount(m)
			m.BuildShadowUpdates(s, c, 32, false)
			r2mProjectionDelta(t, m, before, want)
			m.BuildShadowUpdates(s, c, 33, false)
			r2mProjectionDelta(t, m, before, want)
		})
	}
}
