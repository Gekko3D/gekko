package gpu

import (
	"fmt"
	"math"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

// Exercise insertion into cached membership through later live scalar edits:
// no ordering or retained-index implementation details are asserted.
func TestR2jBoundsDeltaInsertionAndRemovalPreserveEveryMember(t *testing.T) {
	for _, index := range []int{0, 2, 4} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			objects := make([]*core.VoxelObject, 5)
			for i := range objects {
				objects[i] = r2iObject(uint32(i+1), r2iAxes[0])
			}
			objects[index].Transform.Position = r2iAxes[5].Sub(mgl32.Vec3{.5, .5, .5})
			m, s, c := r2iFixture(t, 10, objects...)
			r2iWarm(t, m, s, c)
			objects[index].Transform.Position = r2iAxes[0].Sub(mgl32.Vec3{.5, .5, .5})
			objects[index].Transform.Dirty = true
			r2aCommit(m, s, c)
			r2iDrain(t, m, s, c, 1000, 0, 5)
			for i, object := range objects {
				object.EmitterLinkID++
				r2aCommit(m, s, c)
				r2iDrain(t, m, s, c, 2000+uint64(i)*2000, 0)
			}
			objects[index].Transform.Position = r2iAxes[5].Sub(mgl32.Vec3{.5, .5, .5})
			objects[index].Transform.Dirty = true
			r2aCommit(m, s, c)
			r2iDrain(t, m, s, c, 20000, 0, 5)
			objects[index].XBrickMap.Revision++
			r2aCommit(m, s, c)
			r2iDrain(t, m, s, c, 22000, 5)
		})
	}
}

func TestR2jBoundsDeltaConservativelyAddsAndRemovesUnknownBounds(t *testing.T) {
	cases := []struct {
		name   string
		bounds *[2]mgl32.Vec3
	}{
		{"nil", nil},
		{"reversed", &[2]mgl32.Vec3{{2, 0, 0}, {1, 1, 1}}},
		{"nan", &[2]mgl32.Vec3{{float32(math.NaN()), 0, 0}, {1, 1, 1}}},
		{"infinity", &[2]mgl32.Vec3{{0, 0, 0}, {float32(math.Inf(1)), 1, 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := r2iObject(1, r2iAxes[0]), r2iObject(2, r2iAxes[5])
			m, s, c := r2iFixture(t, 10, a, b)
			r2iWarm(t, m, s, c)
			original := *a.WorldAABB
			// Keep the scene owner's selected list while publishing malformed bounds.
			a.WorldAABB = tc.bounds
			r2iDrain(t, m, s, c, 1000, 0, 1, 2, 3, 4, 5)
			a.WorldAABB = &original
			r2iDrain(t, m, s, c, 3000, 0, 1, 2, 3, 4, 5)
			b.EmitterLinkID++
			r2iDrain(t, m, s, c, 5000, 5)
		})
	}
}

func TestR2jSimultaneousBoundsDeltasPreserveMovedAndUntouchedCasters(t *testing.T) {
	objects := make([]*core.VoxelObject, 5)
	for i := range objects {
		objects[i] = r2iObject(uint32(i+1), r2iAxes[0])
	}
	m, s, c := r2iFixture(t, 10, objects...)
	r2iWarm(t, m, s, c)
	faces := []uint32{2, 0, 4, 0, 5}
	for _, index := range []int{0, 2, 4} {
		objects[index].Transform.Position = r2iAxes[faces[index]].Sub(mgl32.Vec3{.5, .5, .5})
		objects[index].Transform.Dirty = true
	}
	r2aCommit(m, s, c)
	r2iDrain(t, m, s, c, 1000, 0, 2, 4, 5)
	for index, object := range objects {
		object.XBrickMap.Revision++
		r2aCommit(m, s, c)
		r2iDrain(t, m, s, c, 3000+uint64(index)*2000, faces[index])
	}
}

func TestR2jCountStableReplacementAndReorderKeepLiveDependencies(t *testing.T) {
	for _, change := range []string{"replace", "reorder"} {
		t.Run(change, func(t *testing.T) {
			a, b := r2iObject(1, r2iAxes[0]), r2iObject(2, r2iAxes[5])
			m, s, c := r2iFixture(t, 10, a, b)
			r2iWarm(t, m, s, c)
			if change == "replace" {
				a = r2iObject(3, r2iAxes[2])
				s.Objects[0] = a
				scheduleRun(t, m, s)
				r2aCommit(m, s, c)
				r2iDrain(t, m, s, c, 1000, 0, 2)
			} else {
				// Selection order belongs to Scene; publish its new order directly.
				s.ShadowObjects[0], s.ShadowObjects[1] = s.ShadowObjects[1], s.ShadowObjects[0]
				r2iDrain(t, m, s, c, 1000)
			}
			b.XBrickMap.Revision++
			r2aCommit(m, s, c)
			r2iDrain(t, m, s, c, 3000, 5)
			a.RenderVoxelMap().Revision++
			r2aCommit(m, s, c)
			face := uint32(0)
			if change == "replace" {
				face = 2
			}
			r2iDrain(t, m, s, c, 5000, face)
		})
	}
}

func TestR2jBoundsDeltaSpotAndDirectionalMovesRetainExactMapSets(t *testing.T) {
	t.Run("spot", func(t *testing.T) {
		m, s, c := r2aFixture(t, core.LightTypeSpot)
		o := s.Objects[0]
		o.Transform.Position[0] = 101
		o.Transform.Dirty = true
		r2aCommit(m, s, c)
		u := r2aAssert(t, m, s, c, 1000, []int{0, 1})
		m.RecordShadowUpdates(u, 1000, s.ShadowRevision())
		r2aAssert(t, m, s, c, 2000, nil)
		o.XBrickMap.Revision++
		r2aCommit(m, s, c)
		r2aAssert(t, m, s, c, 3000, []int{1})
	})
	t.Run("directional", func(t *testing.T) {
		m, s, c := r2bFixture(t)
		o := s.Objects[1]
		o.Transform.Position = s.Objects[0].Transform.Position
		o.Transform.Dirty = true
		r2aCommit(m, s, c)
		u := r2bAssert(t, m, s, c, 1000, []uint32{0, 1})
		if len(u) != 2 {
			t.Fatalf("move woke unrelated light: %+v", u)
		}
		m.RecordShadowUpdates(u, 1000, s.ShadowRevision())
		r2bAssert(t, m, s, c, 2000, nil)
		o.XBrickMap.Revision++
		r2aCommit(m, s, c)
		r2bAssert(t, m, s, c, 3000, []uint32{0, 1})
		u = m.BuildShadowUpdates(s, c, 3001, false)
		m.RecordShadowUpdates(u, 3001, s.ShadowRevision())
		o.Transform.Position = mgl32.Vec3{70, 0, -100}
		o.Transform.Dirty = true
		r2aCommit(m, s, c)
		u = r2bAssert(t, m, s, c, 4000, []uint32{0, 1})
		if len(u) != 2 {
			t.Fatalf("move out woke unrelated light: %+v", u)
		}
		m.RecordShadowUpdates(u, 4000, s.ShadowRevision())
		o.XBrickMap.Revision++
		r2aCommit(m, s, c)
		r2bAssert(t, m, s, c, 5000, []uint32{1})
		u = m.BuildShadowUpdates(s, c, 5001, false)
		m.RecordShadowUpdates(u, 5001, s.ShadowRevision())
		o.Transform.Position = mgl32.Vec3{600, 0, -20}
		o.Transform.Dirty = true
		r2aCommit(m, s, c)
		u = r2bAssert(t, m, s, c, 6000, []uint32{1})
		if len(u) != 2 {
			t.Fatalf("remote move must invalidate far cascade and remote spot: %+v", u)
		}
		m.RecordShadowUpdates(u, 6000, s.ShadowRevision())
		o.XBrickMap.Revision++
		r2aCommit(m, s, c)
		u = r2bAssert(t, m, s, c, 7000, nil)
		if len(u) != 1 || u[0].LightIndex != 1 {
			t.Fatalf("remote scalar edit retained old cascade dependency: %+v", u)
		}
	})
}
