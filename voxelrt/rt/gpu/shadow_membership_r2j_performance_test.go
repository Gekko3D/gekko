package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

// The public cumulative diagnostic counts actual caster/volume predicate
// evaluations, independently of helper layout or the number of preparations.
func r2jIntersectionDelta(t *testing.T, m *GpuBufferManager, before, want uint64) {
	t.Helper()
	if got := m.ShadowMembershipIntersectionCount - before; got != want {
		t.Fatalf("membership intersection evaluations=%d, want %d", got, want)
	}
}

func r2jMixedFixture(t *testing.T) (*GpuBufferManager, *core.Scene, *core.CameraState) {
	t.Helper()
	objects := make([]*core.VoxelObject, 7)
	for i := range objects {
		objects[i] = r2iObject(uint32(i+1), mgl32.Vec3{8 + float32(i)*.1, 0, 0})
	}
	m, _ := scheduleFixture(t, objects...)
	s := core.NewScene()
	s.Objects = objects
	s.Lights = []core.Light{
		r2aLight(core.LightTypeSpot, 8),
		{Position: [4]float32{0, 0, 0, 1}, Params: [4]float32{20, .8, float32(core.LightTypePoint), 1}},
		{Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}},
	}
	c := core.NewCameraState()
	c.Position = mgl32.Vec3{-10, 0, 0}
	r2aCommit(m, s, c)
	if len(s.ShadowObjects) != len(objects) {
		t.Fatal("fixture did not select every caster")
	}
	return m, s, c
}

func TestR2jMembershipIntersectionWorkTracksBoundsDeltas(t *testing.T) {
	m, s, c := r2jMixedFixture(t)
	n, l := uint64(len(s.ShadowObjects)), uint64(len(m.ShadowLayerParams))
	if n < 3 || l != 1+6+core.DirectionalShadowCascadeCount {
		t.Fatal("fixture must cover spot, six point faces and directional cascades")
	}
	r2jIntersectionDelta(t, m, 0, n*l)
	r2eWarm(t, m, s, c)
	before := m.ShadowMembershipIntersectionCount
	r2aCommit(m, s, c)
	m.BuildShadowUpdates(s, c, 1000, false)
	r2jIntersectionDelta(t, m, before, 0)
	object := s.Objects[3]
	object.Transform.Position[0] += .01
	object.Transform.Dirty = true
	r2aCommit(m, s, c)
	r2jIntersectionDelta(t, m, before, l)
	// Builds neither repeat membership work nor acknowledge pending maps.
	for frame := uint64(1001); frame < 1004; frame++ {
		if u := m.BuildShadowUpdates(s, c, frame, false); len(u) == 0 {
			t.Fatal("Build acknowledged unrecorded bounds edit")
		}
		if r2eReady(m, s, 0) || r2eReady(m, s, 1) {
			t.Fatal("unrecorded bounds edit remained serialized ready")
		}
	}
	r2jIntersectionDelta(t, m, before, l)
	object.Transform.Position[0] += .01
	object.Transform.Dirty = true
	r2aCommit(m, s, c)
	r2jIntersectionDelta(t, m, before, 2*l)
	m.BuildShadowUpdates(s, c, 1004, false)
	r2jIntersectionDelta(t, m, before, 2*l)
	r2eWarm(t, m, s, c)
	for _, change := range []string{"revision", "metadata", "selected map", "geometry upload", "opacity upload", "unknown upload"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2jMixedFixture(t)
			r2eWarm(t, m, s, c)
			object := s.Objects[3]
			before := m.ShadowMembershipIntersectionCount
			switch change {
			case "revision":
				object.XBrickMap.Revision++
			case "metadata":
				object.EmitterLinkID++
			case "selected map":
				object.XBrickMap = c3h9Map(9, [3]int{})
				object.XBrickMap.ClearDirty()
			case "geometry upload":
				object.XBrickMap.DirtyBricks[[6]int{}] = true
				scheduleRun(t, m, s)
				if m.VoxelBricksUploaded != 1 || m.VoxelUploadBytes == 0 {
					t.Fatal("fixture did not successfully upload geometry")
				}
			case "opacity upload":
				object.MaterialTable[1].Transparency = .5
				m.MaterialBufferGeneration++
				scheduleRun(t, m, s)
				if m.VoxelMaterialsUploaded == 0 || m.VoxelUploadBytes == 0 {
					t.Fatal("fixture did not successfully upload opacity")
				}
			case "unknown upload":
				m.VoxelUploadRevision++
			}
			r2aCommit(m, s, c)
			u := m.BuildShadowUpdates(s, c, 5000, false)
			if len(u) == 0 || r2eReady(m, s, 0) || r2eReady(m, s, 1) {
				t.Fatal("scalar edit did not invalidate dependent maps")
			}
			r2jIntersectionDelta(t, m, before, 0)
			r2eWarm(t, m, s, c)
		})
	}
}

func TestR2jInactiveOwnerCannotApplyOnlyLatestBoundsDelta(t *testing.T) {
	m, s, c := r2jMixedFixture(t)
	r2eWarm(t, m, s, c)
	meta := s.Lights[0].ShadowMeta
	s.Lights[0].ShadowMeta[1] = 0
	m.BuildShadowUpdates(s, c, 1000, false)
	for _, index := range []int{1, 4} {
		s.Objects[index].Transform.Position[0] += .01
		s.Objects[index].Transform.Dirty = true
		s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{CameraPosition: c.Position})
		m.BuildShadowUpdates(s, c, 1001+uint64(index), false)
	}
	s.Lights[0].ShadowMeta = meta
	before := m.ShadowMembershipIntersectionCount
	u := m.BuildShadowUpdates(s, c, 2000, false)
	r2jIntersectionDelta(t, m, before, uint64(len(s.ShadowObjects)))
	if r2eReady(m, s, 0) {
		t.Fatal("reactivated owner acknowledged unrecorded current inputs")
	}
	found := false
	for _, update := range u {
		found = found || update.LightIndex == 0
	}
	if !found {
		t.Fatal("reactivated spot was not scheduled")
	}
}

func TestR2jMembershipFallbackScansCurrentSelectedCasters(t *testing.T) {
	for _, change := range []string{"replace", "reorder", "append", "remove", "light key", "point origin", "point face resolution", "directional cascade key"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2jMixedFixture(t)
			r2eWarm(t, m, s, c)
			before := m.ShadowMembershipIntersectionCount
			n, l := uint64(len(s.ShadowObjects)), uint64(len(m.ShadowLayerParams))
			want := n * l
			switch change {
			case "replace":
				o := r2iObject(99, r2iAxes[0])
				s.Objects[2] = o
				scheduleRun(t, m, s)
				r2aCommit(m, s, c)
			case "reorder":
				s.ShadowObjects[0], s.ShadowObjects[4] = s.ShadowObjects[4], s.ShadowObjects[0]
			case "append":
				s.Objects = append(s.Objects, r2iObject(99, r2iAxes[0]))
				scheduleRun(t, m, s)
				r2aCommit(m, s, c)
				want = (n + 1) * l
			case "remove":
				s.Objects = s.Objects[1:]
				r2aCommit(m, s, c)
				want = (n - 1) * l
			case "light key":
				s.Lights[0].ShadowMeta[3]++
				want = n
			case "point origin":
				m.RenderOrigin = mgl32.Vec3{1, 2, 3}
				want = 6 * n
			case "point face resolution":
				m.ShadowLayerParams[s.Lights[1].ShadowMeta[0]+5].EffectiveResolution /= 2
				want = n
			case "directional cascade key":
				s.Lights[2].DirectionalCascades[1].Params[2] += .001
				want = n
			}
			m.BuildShadowUpdates(s, c, 1000, false)
			r2jIntersectionDelta(t, m, before, want)
			m.BuildShadowUpdates(s, c, 1001, false)
			r2jIntersectionDelta(t, m, before, want)
		})
	}
}

func TestR2jMultipleBoundsDeltasAndForcedVolumesCountEachEvaluationOnce(t *testing.T) {
	for _, change := range []string{"bounds only", "spot key", "point origin"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2jMixedFixture(t)
			r2eWarm(t, m, s, c)
			n, l := uint64(len(s.ShadowObjects)), uint64(len(m.ShadowLayerParams))
			before := m.ShadowMembershipIntersectionCount
			// Three discontiguous selected indices change in the same preparation.
			for _, index := range []int{0, 3, 6} {
				bounds := *s.Objects[index].WorldAABB
				bounds[0][0] += .01
				bounds[1][0] += .01
				s.Objects[index].WorldAABB = &bounds
			}
			want := 3 * l
			switch change {
			case "spot key":
				s.Lights[0].ShadowMeta[3]++
				want = n + 3*(l-1)
			case "point origin":
				m.RenderOrigin = mgl32.Vec3{1, 2, 3}
				want = 6*n + 3*(l-6)
			}
			m.BuildShadowUpdates(s, c, 1000, false)
			r2jIntersectionDelta(t, m, before, want)
			m.BuildShadowUpdates(s, c, 1001, false)
			r2jIntersectionDelta(t, m, before, want)
		})
	}
}
