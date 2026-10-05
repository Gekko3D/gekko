package gpu

import (
	"fmt"
	"math"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func r2lClassificationCount(m *GpuBufferManager) uint64 {
	return m.ShadowPointMembershipClassificationCount
}

func r2lClassificationDelta(t *testing.T, m *GpuBufferManager, before, want uint64) {
	t.Helper()
	if got := m.ShadowPointMembershipClassificationCount - before; got != want {
		t.Fatalf("point caster/light classifications=%d, want %d", got, want)
	}
}

// Publish the upstream selection explicitly: classification must visit every
// selected index, including distant objects and duplicate pointer occurrences.
func r2lFixture(t *testing.T, count int, lights ...core.Light) (*GpuBufferManager, *core.Scene, *core.CameraState) {
	t.Helper()
	objects := make([]*core.VoxelObject, count)
	for i := range objects {
		objects[i] = r2iObject(uint32(i+1), r2iAxes[0])
	}
	m, _ := scheduleFixture(t, objects...)
	s := core.NewScene()
	s.Objects = objects
	if len(lights) == 0 {
		lights = []core.Light{{Position: [4]float32{0, 0, 0, 1}, Params: [4]float32{20, .8, float32(core.LightTypePoint), 1}}}
	}
	s.Lights = lights
	c := core.NewCameraState()
	c.Position = mgl32.Vec3{-10, 0, 0}
	s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{CameraPosition: c.Position})
	s.ShadowObjects = append([]*core.VoxelObject(nil), objects...)
	m.UpdateLights(s, c, 1)
	return m, s, c
}

func r2lMoveBounds(object *core.VoxelObject, center mgl32.Vec3) {
	object.WorldAABB = &[2]mgl32.Vec3{center.Sub(mgl32.Vec3{.5, .5, .5}), center.Add(mgl32.Vec3{.5, .5, .5})}
}

func r2lChangeFaceResolution(m *GpuBufferManager, s *core.Scene, face uint32) {
	layer := &m.ShadowLayerParams[s.Lights[0].ShadowMeta[0]+face]
	if layer.EffectiveResolution == 128 {
		layer.EffectiveResolution = 256
	} else {
		layer.EffectiveResolution = 128
	}
}

func TestR2lColdClassificationOncePerSelectedIndex(t *testing.T) {
	for _, count := range []int{32, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			m, s, c := r2lFixture(t, count)
			r2lClassificationDelta(t, m, 0, uint64(count))
			r2jIntersectionDelta(t, m, 0, 6*uint64(count))
			r2iWarm(t, m, s, c)
			r2lClassificationDelta(t, m, 0, uint64(count))
			before := r2lClassificationCount(m)
			for frame := uint64(1000); frame < 1003; frame++ {
				if u := m.BuildShadowUpdates(s, c, frame, false); len(u) != 0 || !r2eReady(m, s, 0) {
					t.Fatalf("idle point owner woke or lost readiness: %+v", u)
				}
			}
			r2lClassificationDelta(t, m, before, 0)
		})
	}
}

func TestR2lScalarOnlyPreparationsDoNotClassify(t *testing.T) {
	for _, change := range []string{"revision", "metadata", "matrix", "local bounds", "selected map", "geometry upload", "opacity upload", "unknown upload"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2lFixture(t, 7)
			r2iWarm(t, m, s, c)
			o := s.Objects[3]
			before := r2lClassificationCount(m)
			intersections := m.ShadowMembershipIntersectionCount
			faces := []uint32{0}
			switch change {
			case "revision":
				o.XBrickMap.Revision++
			case "metadata":
				o.EmitterLinkID++
			case "matrix":
				o.Transform.Position[0] += .01
				o.Transform.Dirty = true
			case "local bounds":
				o.XBrickMap.CachedMax[0] += .01
				o.XBrickMap.AABBDirty = false
			case "selected map":
				o.XBrickMap = c3h9Map(9, [3]int{})
				o.XBrickMap.ClearDirty()
			case "geometry upload":
				o.XBrickMap.DirtyBricks[[6]int{}] = true
				scheduleRun(t, m, s)
				if m.VoxelBricksUploaded != 1 || m.VoxelUploadBytes == 0 {
					t.Fatal("fixture did not publish one geometry upload")
				}
			case "opacity upload":
				o.MaterialTable[1].Transparency = .5
				m.MaterialBufferGeneration++
				scheduleRun(t, m, s)
				if m.VoxelMaterialsUploaded == 0 || m.VoxelUploadBytes == 0 {
					t.Fatal("fixture did not publish opacity upload")
				}
			case "unknown upload":
				m.VoxelUploadRevision++
				faces = []uint32{0, 1, 2, 3, 4, 5}
			}
			// Preserve selected world bounds; these edits affect scalar identity.
			r2iDrain(t, m, s, c, 1000, faces...)
			r2lClassificationDelta(t, m, before, 0)
			r2jIntersectionDelta(t, m, intersections, 0)
		})
	}
}

func TestR2lBoundsDeltasAreLazyAndFreshAcrossPreparations(t *testing.T) {
	m, s, c := r2lFixture(t, 7)
	r2iWarm(t, m, s, c)
	before, intersections := r2lClassificationCount(m), m.ShadowMembershipIntersectionCount
	r2lMoveBounds(s.Objects[0], r2iAxes[5])
	first := m.BuildShadowUpdates(s, c, 1000, false)
	if len(first) != 2 || r2eReady(m, s, 0) {
		t.Fatalf("moved caster must leave old/new faces pending: %+v", first)
	}
	for frame := uint64(1001); frame < 1004; frame++ {
		if u := m.BuildShadowUpdates(s, c, frame, false); len(u) != 2 || r2eReady(m, s, 0) {
			t.Fatalf("Build acknowledged unrecorded movement: %+v", u)
		}
	}
	r2lClassificationDelta(t, m, before, 1)
	r2jIntersectionDelta(t, m, intersections, 6)
	// A second preparation before Record must replace the footprint for this
	// same index. The unrecorded intermediate -Z membership remains pending.
	r2lMoveBounds(s.Objects[0], r2iAxes[2])
	r2iDrain(t, m, s, c, 2000, 0, 2, 5)
	r2lClassificationDelta(t, m, before, 2)
	r2jIntersectionDelta(t, m, intersections, 12)
	before, intersections = r2lClassificationCount(m), m.ShadowMembershipIntersectionCount
	for _, index := range []int{1, 3, 6} {
		r2lMoveBounds(s.Objects[index], r2iAxes[4])
	}
	r2iDrain(t, m, s, c, 3000, 0, 4)
	r2lClassificationDelta(t, m, before, 3)
	r2jIntersectionDelta(t, m, intersections, 18)
	s.Objects[0].EmitterLinkID++
	r2iDrain(t, m, s, c, 4000, 2)
	r2lClassificationDelta(t, m, before, 3)
}

func TestR2lFullFallbacksShareClassificationWithoutRepeatingBuildWork(t *testing.T) {
	for _, change := range []string{"origin", "light position", "light key", "one face key", "reorder", "replace", "append", "remove"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2lFixture(t, 7)
			r2iWarm(t, m, s, c)
			before, intersections := r2lClassificationCount(m), m.ShadowMembershipIntersectionCount
			n := uint64(len(s.ShadowObjects))
			wantIntersections := 6 * n
			switch change {
			case "origin":
				m.RenderOrigin = mgl32.Vec3{1, 2, 3}
			case "light position":
				s.Lights[0].Position[0] = 16
			case "light key":
				s.Lights[0].ShadowMeta[3]++
			case "one face key":
				r2lChangeFaceResolution(m, s, 5)
				wantIntersections = n
			case "reorder":
				s.ShadowObjects[0], s.ShadowObjects[6] = s.ShadowObjects[6], s.ShadowObjects[0]
			case "replace":
				o := r2iObject(99, r2iAxes[2])
				r2lMoveBounds(o, r2iAxes[2])
				s.ShadowObjects[3] = o
			case "append":
				s.ShadowObjects = append(s.ShadowObjects, s.Objects[0])
				n++
				wantIntersections = 6 * n
			case "remove":
				s.ShadowObjects = s.ShadowObjects[1:]
				n--
				wantIntersections = 6 * n
			}
			m.BuildShadowUpdates(s, c, 1000, false)
			m.BuildShadowUpdates(s, c, 1001, false)
			r2lClassificationDelta(t, m, before, n)
			r2jIntersectionDelta(t, m, intersections, wantIntersections)
			if change == "one face key" {
				r2iDrain(t, m, s, c, 1002, 5)
			} else {
				r2eWarm(t, m, s, c)
			}
			if change == "light position" {
				s.Objects[0].EmitterLinkID++
				r2iDrain(t, m, s, c, 2000, 1)
			}
			r2lClassificationDelta(t, m, before, n)
		})
	}
}

func TestR2lFullFallbackAndBoundsDeltasShareSelectedIndexClassification(t *testing.T) {
	for _, change := range []string{"one face key", "origin"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2lFixture(t, 7)
			r2iWarm(t, m, s, c)
			before, intersections := r2lClassificationCount(m), m.ShadowMembershipIntersectionCount
			for _, index := range []int{0, 3, 6} {
				r2lMoveBounds(s.Objects[index], r2iAxes[2])
			}
			faces := []uint32{0, 2}
			wantIntersections := uint64(6 * 7)
			if change == "one face key" {
				// Five owners need only the three deltas. The final face needs
				// all N indices, reusing classifications already needed by peers.
				r2lChangeFaceResolution(m, s, 5)
				faces = append(faces, 5)
				wantIntersections = 7 + 5*3
			} else {
				m.RenderOrigin = mgl32.Vec3{1, 2, 3}
			}
			m.BuildShadowUpdates(s, c, 1000, false)
			m.BuildShadowUpdates(s, c, 1001, false)
			r2lClassificationDelta(t, m, before, 7)
			r2jIntersectionDelta(t, m, intersections, wantIntersections)
			r2iDrain(t, m, s, c, 1002, faces...)
			r2lClassificationDelta(t, m, before, 7)
		})
	}
}

func TestR2lDuplicateIndicesAndReorderKeepDistinctWorkAndLiveFaces(t *testing.T) {
	a, b := r2iObject(1, r2iAxes[0]), r2iObject(2, r2iAxes[5])
	m, s, c := r2iFixture(t, 10, a, b)
	before := r2lClassificationCount(m)
	s.ShadowObjects = []*core.VoxelObject{a, b, a}
	r2iWarm(t, m, s, c)
	r2lClassificationDelta(t, m, before, 3)
	before = r2lClassificationCount(m)
	s.ShadowObjects = []*core.VoxelObject{a, a, b}
	r2iDrain(t, m, s, c, 1000)
	r2lClassificationDelta(t, m, before, 3)
	before = r2lClassificationCount(m)
	r2lMoveBounds(a, r2iAxes[2])
	r2iDrain(t, m, s, c, 2000, 0, 2)
	r2lClassificationDelta(t, m, before, 2)
	b.EmitterLinkID++
	r2iDrain(t, m, s, c, 3000, 5)
	a.EmitterLinkID++
	r2iDrain(t, m, s, c, 4000, 2)
	r2lClassificationDelta(t, m, before, 2)
}

// Drain an independently specified set of point (light, face) pairs. Repeated
// Builds never certify readiness; only recording all pending faces can do so.
func r2lDrainLights(t *testing.T, m *GpuBufferManager, s *core.Scene, c *core.CameraState, frame uint64, pending map[[2]uint32]bool) {
	t.Helper()
	for n := 0; n < 24 && len(pending) > 0; n++ {
		u := m.BuildShadowUpdates(s, c, frame+uint64(n), false)
		if len(u) == 0 {
			t.Fatal("pending point faces were not scheduled")
		}
		for _, update := range u {
			// Mixed-family fixtures also refresh ordinary directional owners.
			if update.Kind != core.ShadowUpdateKindPoint {
				continue
			}
			pair := [2]uint32{update.LightIndex, update.CascadeIndex}
			if !pending[pair] {
				t.Fatalf("scheduled unexpected point face %+v; pending %v", update, pending)
			}
			if r2eReady(m, s, int(update.LightIndex)) {
				t.Fatal("point light ready before pending Record")
			}
			delete(pending, pair)
		}
		m.RecordShadowUpdates(u, frame+uint64(n), s.ShadowRevision())
	}
	if len(pending) != 0 {
		t.Fatalf("point faces did not drain: %v", pending)
	}
	for i, light := range s.Lights {
		if uint32(light.Params[2]) == core.LightTypePoint && !r2eReady(m, s, i) {
			t.Fatalf("point light %d not ready after all Records", i)
		}
	}
	if u := m.BuildShadowUpdates(s, c, frame+1000, false); len(u) != 0 {
		t.Fatalf("recorded point faces woke: %+v", u)
	}
}

func TestR2lDifferentPointLightsAndReorderedFacesCannotShareMasks(t *testing.T) {
	point := core.Light{Position: [4]float32{0, 0, 0, 1}, Params: [4]float32{20, .8, float32(core.LightTypePoint), 1}}
	second := point
	second.Position[0] = 16
	directional := core.Light{Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}}
	m, s, c := r2lFixture(t, 7, point, r2aLight(core.LightTypeSpot, 100), second, directional)
	r2lClassificationDelta(t, m, 0, 14)
	r2eWarm(t, m, s, c)
	r2lClassificationDelta(t, m, 0, 14)
	before, intersections := r2lClassificationCount(m), m.ShadowMembershipIntersectionCount
	for _, lightIndex := range []int{0, 2} {
		base := s.Lights[lightIndex].ShadowMeta[0]
		// Public face assignments inside each block are deliberately permuted.
		m.ShadowLayerParams[base].CascadeIndex, m.ShadowLayerParams[base+5].CascadeIndex = 5, 0
	}
	m.BuildShadowUpdates(s, c, 1000, false)
	r2lClassificationDelta(t, m, before, 14)
	r2jIntersectionDelta(t, m, intersections, 4*7)
	r2eWarm(t, m, s, c)
	before = r2lClassificationCount(m)
	s.Objects[3].EmitterLinkID++
	r2lDrainLights(t, m, s, c, 2000, map[[2]uint32]bool{{0, 0}: true, {2, 1}: true})
	r2lClassificationDelta(t, m, before, 0)
	r2lMoveBounds(s.Objects[3], mgl32.Vec3{8, 8, 0})
	// Both lights add +Y independently, with different signed-X dependencies.
	r2lDrainLights(t, m, s, c, 3000, map[[2]uint32]bool{{0, 0}: true, {0, 2}: true, {2, 1}: true, {2, 2}: true})
	r2lClassificationDelta(t, m, before, 2)
}

func TestR2lOriginFootprintIsFreshBetweenPreparations(t *testing.T) {
	o := r2iObject(1, mgl32.Vec3{1e6 + 12.5, 1e6 + 14.5, 0})
	m, s, c := r2iFixture(t, 10, o)
	s.Lights[0].Position = [4]float32{1e6, 1e6, 0, 1}
	c.Position = mgl32.Vec3{1e6 - 10, 1e6, 0}
	r2aCommit(m, s, c)
	o.WorldAABB = &[2]mgl32.Vec3{{1e6 + 12, 1e6 + 14, -.5}, {1e6 + 13, 1e6 + 15, .5}}
	s.ShadowObjects = []*core.VoxelObject{o}
	r2iWarm(t, m, s, c)
	before := r2lClassificationCount(m)
	m.RenderOrigin = mgl32.Vec3{-1e8, 1e6, 0}
	r2iDrain(t, m, s, c, 1000, 0)
	r2lClassificationDelta(t, m, before, 1)
	m.RenderOrigin = mgl32.Vec3{}
	r2iDrain(t, m, s, c, 2000, 0)
	r2lClassificationDelta(t, m, before, 2)
	o.EmitterLinkID++
	r2iDrain(t, m, s, c, 3000, 2)
	r2lClassificationDelta(t, m, before, 2)
}

func TestR2lInvalidBoundsDeltaAndPackedOverflowRetainAllFaces(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bounds *[2]mgl32.Vec3
		origin mgl32.Vec3
	}{
		{"nil", nil, mgl32.Vec3{}},
		{"reversed", &[2]mgl32.Vec3{{2, 0, 0}, {1, 1, 1}}, mgl32.Vec3{}},
		{"nan", &[2]mgl32.Vec3{{float32(math.NaN()), 0, 0}, {1, 1, 1}}, mgl32.Vec3{}},
		{"infinity", &[2]mgl32.Vec3{{0, 0, 0}, {float32(math.Inf(1)), 1, 1}}, mgl32.Vec3{}},
		{"packed overflow", &[2]mgl32.Vec3{{math.MaxFloat32, 0, 0}, {math.MaxFloat32, 1, 1}}, mgl32.Vec3{-math.MaxFloat32, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, s, c := r2lFixture(t, 1)
			r2iWarm(t, m, s, c)
			before := r2lClassificationCount(m)
			s.Objects[0].WorldAABB = tc.bounds
			m.RenderOrigin = tc.origin
			r2iDrain(t, m, s, c, 1000, 0, 1, 2, 3, 4, 5)
			r2lClassificationDelta(t, m, before, 1)
			s.Objects[0].EmitterLinkID++
			r2iDrain(t, m, s, c, 2000, 0, 1, 2, 3, 4, 5)
			r2lClassificationDelta(t, m, before, 1)
		})
	}
}

func TestR2lUnsupportedFaceAssignmentConservativelyRetainsCaster(t *testing.T) {
	m, s, c := r2lFixture(t, 1)
	r2iWarm(t, m, s, c)
	m.ShadowLayerParams[s.Lights[0].ShadowMeta[0]+5].CascadeIndex = 99
	r2iDrain(t, m, s, c, 1000, 99)
	s.Objects[0].EmitterLinkID++
	r2iDrain(t, m, s, c, 2000, 0, 99)
}
