package gpu

import (
	"fmt"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

// These fixtures exercise the public scheduling/recording boundary. Each scalar
// case starts with fresh allocations and selected geometry, avoiding dependencies
// on an earlier case's representation or upload state.
func TestR2kExactLiveScalarChangesAcrossFamilies(t *testing.T) {
	for _, family := range []string{"spot", "point", "directional"} {
		for _, change := range []string{"matrices", "local bounds"} {
			t.Run(family+"/"+change, func(t *testing.T) {
				var m *GpuBufferManager
				var s *core.Scene
				var c *core.CameraState
				var o *core.VoxelObject
				switch family {
				case "spot":
					m, s, c = r2aFixture(t, core.LightTypeSpot)
					o = s.Objects[0]
				case "point":
					o = r2iObject(1, r2iAxes[0])
					m, s, c = r2iFixture(t, 10, o)
					r2iWarm(t, m, s, c)
				case "directional":
					m, s, c = r2bFixture(t)
					o = s.Objects[1] // far cascade only, unrelated remote spot
				}
				bounds := *o.RenderWorldBounds()
				beforeIntersections := m.ShadowMembershipIntersectionCount
				switch change {
				case "matrices":
					o.Transform.Position[0] += .01
					o.Transform.Dirty = true
				case "local bounds":
					o.XBrickMap.CachedMax[0] += .01
					o.XBrickMap.AABBDirty = false

				}
				// Publish live scalar inputs without recomputing authored world
				// bounds: all cases must invalidate without a membership delta.
				o.WorldAABB = &bounds
				switch family {
				case "point":
					faces := []uint32{0}
					if change == "unknown upload" {
						faces = []uint32{0, 1, 2, 3, 4, 5}
					}
					r2iDrain(t, m, s, c, 1000, faces...)
				case "spot":
					want := []int{0}
					if change == "unknown upload" {
						want = []int{0, 1}
					}
					m.BuildShadowUpdates(s, c, 1000, false)
					u := r2aAssert(t, m, s, c, 1001, want)
					m.RecordShadowUpdates(u, 1001, s.ShadowRevision())
					r2aAssert(t, m, s, c, 2000, nil)
				case "directional":
					want := []uint32{1}
					if change == "unknown upload" {
						want = []uint32{0, 1}
					}
					u := r2bAssert(t, m, s, c, 1000, want)
					if change != "unknown upload" && len(u) != 1 {
						t.Fatalf("unrelated map woke: %+v", u)
					}
					m.RecordShadowUpdates(u, 1000, s.ShadowRevision())
					r2bAssert(t, m, s, c, 2000, nil)
				}
				r2jIntersectionDelta(t, m, beforeIntersections, 0)
			})
		}
	}
}

func TestR2kObservedScalarABARemainsPending(t *testing.T) {
	for _, family := range []string{"spot", "point", "directional"} {
		t.Run(family, func(t *testing.T) {
			var m *GpuBufferManager
			var s *core.Scene
			var c *core.CameraState
			switch family {
			case "spot":
				m, s, c = r2aFixture(t, core.LightTypeSpot)
			case "point":
				m, s, c = r2iFixture(t, 10, r2iObject(1, r2iAxes[0]))
				r2iWarm(t, m, s, c)
			case "directional":
				m, s, c = r2bFixture(t)
			}
			o := s.Objects[0]
			original := o.EmitterLinkID
			o.EmitterLinkID++
			if u := m.BuildShadowUpdates(s, c, 1000, false); len(u) == 0 {
				t.Fatal("first scalar edit was not observed")
			}
			o.EmitterLinkID = original
			// The intervening B snapshot was prepared but never recorded. A
			// restored A value cannot acknowledge either pending preparation.
			switch family {
			case "spot":
				m.BuildShadowUpdates(s, c, 1001, false)
				u := r2aAssert(t, m, s, c, 1002, []int{0})
				m.RecordShadowUpdates(u, 1002, s.ShadowRevision())
				r2aAssert(t, m, s, c, 2000, nil)
			case "point":
				r2iDrain(t, m, s, c, 1001, 0)
			case "directional":
				u := r2bAssert(t, m, s, c, 1001, []uint32{0, 1})
				m.RecordShadowUpdates(u[:1], 1001, s.ShadowRevision())
				pending := u[1].CascadeIndex
				u = r2bAssert(t, m, s, c, 1002, []uint32{pending})
				m.RecordShadowUpdates(u, 1002, s.ShadowRevision())
				r2bAssert(t, m, s, c, 2000, nil)
			}
		})
	}
}

func TestR2kSharedMapPartialFacesAndIndependentSecondEdit(t *testing.T) {
	a, b, independent := r2iObject(1, r2iAxes[0]), r2iObject(2, r2iAxes[5]), r2iObject(3, r2iAxes[2])
	b.XBrickMap = a.XBrickMap
	m, s, c := r2iFixture(t, 160, a, b, independent)
	r2iWarm(t, m, s, c)
	a.XBrickMap.DirtyBricks[[6]int{}] = true
	scheduleRun(t, m, s)
	if m.VoxelBricksUploaded != 1 || m.VoxelUploadBytes == 0 {
		t.Fatal("shared-map upload fixture did not publish one geometry write")
	}
	u := m.BuildShadowUpdates(s, c, 1000, false)
	if len(u) != 1 || u[0].CascadeIndex != 0 {
		t.Fatalf("expected first shared placement face: %+v", u)
	}
	m.RecordShadowUpdates(u, 1000, s.ShadowRevision())
	if r2eReady(m, s, 0) {
		t.Fatal("partial point refresh enabled light")
	}
	independent.EmitterLinkID++
	// A second caster change must not discard the acknowledged shared-map
	// face or hide the unacknowledged peer placement's face.
	r2iDrain(t, m, s, c, 1001, 2, 5)
}

func TestR2kSelectedIdentitySlotReuseKeepsLiveDependencies(t *testing.T) {
	for _, change := range []string{"replace", "reorder", "remove and reappear"} {
		t.Run(change, func(t *testing.T) {
			a, b := r2iObject(1, r2iAxes[0]), r2iObject(2, r2iAxes[5])
			m, s, c := r2iFixture(t, 10, a, b)
			r2iWarm(t, m, s, c)
			switch change {
			case "replace":
				residentMap := a.XBrickMap
				a = r2iObject(3, r2iAxes[2])
				a.XBrickMap = residentMap
				s.Objects[0] = a
				scheduleRun(t, m, s)
				r2aCommit(m, s, c)
				r2iDrain(t, m, s, c, 1000, 0, 2)
			case "reorder":
				s.ShadowObjects[0], s.ShadowObjects[1] = s.ShadowObjects[1], s.ShadowObjects[0]
				r2iDrain(t, m, s, c, 1000)
			case "remove and reappear":
				s.ShadowObjects = []*core.VoxelObject{b}
				r2iDrain(t, m, s, c, 1000, 0)
				a.EmitterLinkID++
				s.ShadowObjects = []*core.VoxelObject{b, a}
				r2iDrain(t, m, s, c, 2000, 0)
			}
			b.EmitterLinkID++
			// Keep the published selection order; no producer notifications.
			r2iDrain(t, m, s, c, 3000, 5)
			a.EmitterLinkID++
			face := uint32(0)
			if change == "replace" {
				face = 2
			}
			r2iDrain(t, m, s, c, 4000, face)
		})
	}
}

func TestR2kUnchangedMemberSurvivesSourceIndexShift(t *testing.T) {
	for _, change := range []string{"reorder", "remove nonmember", "insert nonmember"} {
		t.Run(change, func(t *testing.T) {
			other, member := r2iObject(1, r2iAxes[5]), r2iObject(2, r2iAxes[0])
			m, s, c := r2iFixture(t, 10, other, member)
			r2iWarm(t, m, s, c)
			switch change {
			case "reorder":
				s.ShadowObjects = []*core.VoxelObject{member, other}
				r2iDrain(t, m, s, c, 1000)
			case "remove nonmember":
				s.ShadowObjects = []*core.VoxelObject{member}
				r2iDrain(t, m, s, c, 1000, 5)
			case "insert nonmember":
				added := r2iObject(3, r2iAxes[2])
				s.Objects = append(s.Objects, added)
				scheduleRun(t, m, s)
				r2aCommit(m, s, c)
				s.ShadowObjects = []*core.VoxelObject{added, other, member}
				r2iDrain(t, m, s, c, 1000, 2)
			}
			member.EmitterLinkID++
			r2iDrain(t, m, s, c, 2000, 0)
		})
	}
}

func TestR2kDuplicateSelectedOccurrencesRetainLiveBoundsAndScalars(t *testing.T) {
	a, b := r2iObject(1, r2iAxes[0]), r2iObject(2, r2iAxes[5])
	m, s, c := r2iFixture(t, 10, a, b)
	s.ShadowObjects = []*core.VoxelObject{a, b, a}
	r2iWarm(t, m, s, c)
	// Move duplicate occurrences around the unrelated face's source index.
	// Each face's member sequence is unchanged and stays current.
	s.ShadowObjects = []*core.VoxelObject{a, a, b}
	r2iDrain(t, m, s, c, 1000)
	a.EmitterLinkID++
	r2iDrain(t, m, s, c, 2000, 0)
	a.Transform.Position = r2iAxes[2].Sub(mgl32.Vec3{.5, .5, .5})
	a.Transform.Dirty = true
	s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{CameraPosition: c.Position})
	s.ShadowObjects = []*core.VoxelObject{a, a, b}
	r2iDrain(t, m, s, c, 3000, 0, 2)
	a.XBrickMap.Revision++
	r2iDrain(t, m, s, c, 4000, 2)
	// Dropping one occurrence changes the consuming face's selected members;
	// retaining the other must still preserve its later live dependency.
	s.ShadowObjects = []*core.VoxelObject{a, b}
	r2iDrain(t, m, s, c, 5000, 2)
	a.EmitterLinkID++
	r2iDrain(t, m, s, c, 6000, 2)
}

func r2kStorageFixture(t *testing.T, count int) (*GpuBufferManager, *core.Scene, *core.CameraState) {
	t.Helper()
	objects := make([]*core.VoxelObject, count)
	for i := range objects {
		objects[i] = r2iObject(uint32(i+1), mgl32.Vec3{})
	}
	m, _ := scheduleFixture(t, objects...)
	s := core.NewScene()
	s.Objects = objects
	s.Lights = []core.Light{r2aLight(core.LightTypeSpot, 0), {Position: [4]float32{0, 0, 0, 1}, Params: [4]float32{20, .8, float32(core.LightTypePoint), 1}}, {Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}}}
	c := core.NewCameraState()
	c.Position = mgl32.Vec3{-10, 0, 0}
	r2aCommit(m, s, c)
	if len(s.ShadowObjects) != count || len(m.ShadowLayerParams) != 9 {
		t.Fatal("fixture must select all casters across nine mixed volumes")
	}
	r2eWarm(t, m, s, c)
	return m, s, c
}

func TestR2kMemberStorageBudgetAndLifecycle(t *testing.T) {
	for _, count := range []int{32, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			m, s, c := r2kStorageFixture(t, count)
			budget := uint64(32 * count * len(m.ShadowLayerParams))
			bytes := m.ShadowDependencyMemberStorageBytes()
			if bytes == 0 || bytes > budget {
				t.Fatalf("retained member storage=%d bytes, want 0 < bytes <= %d (32 bytes per selected caster per volume)", bytes, budget)
			}
			for frame := uint64(1000); frame < 1003; frame++ {
				if u := m.BuildShadowUpdates(s, c, frame, false); len(u) != 0 {
					t.Fatalf("stable mixed owners woke: %+v", u)
				}
				if got := m.ShadowDependencyMemberStorageBytes(); got != bytes {
					t.Fatalf("stable preparation changed retained capacity: %d to %d", bytes, got)
				}
			}
			// Grow, shrink, retire. Capacity accounting may retain the high-water
			// allocation while owners live, but cannot retain retired owners.
			for i := 0; i < count; i++ {
				s.Objects = append(s.Objects, r2iObject(uint32(count+i+1), mgl32.Vec3{}))
			}
			scheduleRun(t, m, s)
			r2aCommit(m, s, c)
			r2eWarm(t, m, s, c)
			grown := m.ShadowDependencyMemberStorageBytes()
			if grown <= bytes || grown > 2*budget {
				t.Fatalf("grown storage=%d, original=%d, budget=%d", grown, bytes, 2*budget)
			}
			s.Objects = s.Objects[:1]
			r2aCommit(m, s, c)
			r2eWarm(t, m, s, c)
			if got := m.ShadowDependencyMemberStorageBytes(); got == 0 || got > grown {
				t.Fatalf("shrink storage=%d, high-water=%d", got, grown)
			}
			s.Lights = nil
			r2aCommit(m, s, c)
			m.BuildShadowUpdates(s, c, 4000, false)
			if got := m.ShadowDependencyMemberStorageBytes(); got != 0 {
				t.Fatalf("no active owners retain %d bytes", got)
			}
		})
	}
}

func TestR2kColdForceAndRetiredOwnersRequireRecords(t *testing.T) {
	for _, change := range []string{"cold", "force", "reset", "no lights and readd"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2jMixedFixture(t)
			if change != "cold" {
				r2eWarm(t, m, s, c)
			}
			switch change {
			case "reset":
				m.invalidateShadowCache()
			case "no lights and readd":
				lights := s.Lights
				s.Lights = nil
				r2aCommit(m, s, c)
				s.Objects[0].EmitterLinkID++
				s.Lights = lights
				r2aCommit(m, s, c)
			}
			u := m.BuildShadowUpdates(s, c, 1000, change == "force")
			if len(u) == 0 {
				t.Fatal("requested/cold owners scheduled no work")
			}
			if change == "force" {
				if len(u) != core.DirectionalShadowCascadeCount {
					t.Fatalf("force woke valid local maps: %+v", u)
				}
			} else if r2eReady(m, s, 0) || r2eReady(m, s, 1) {
				t.Fatal("unrecorded new owners serialized ready")
			}
			if pending := m.BuildShadowUpdates(s, c, 1001, change == "force"); len(pending) == 0 {
				t.Fatal("Build acknowledged pending work")
			}
			m.RecordShadowUpdates(u, 1000, s.ShadowRevision())
			r2eWarm(t, m, s, c)
			if pending := m.BuildShadowUpdates(s, c, 2000, false); len(pending) != 0 {
				t.Fatalf("recorded owners did not become stable: %+v", pending)
			}
		})
	}
}
