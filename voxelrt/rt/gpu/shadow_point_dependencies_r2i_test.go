package gpu

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

var r2iAxes = []mgl32.Vec3{{8, 0, 0}, {-8, 0, 0}, {0, 8, 0}, {0, -8, 0}, {0, 0, 8}, {0, 0, -8}}

func r2iObject(id uint32, center mgl32.Vec3) *core.VoxelObject {
	o := r2aObject(id, 0)
	o.Transform.Position = center.Sub(mgl32.Vec3{.5, .5, .5})
	o.Transform.Dirty = true
	return o
}

func r2iFixture(t *testing.T, distance float32, objects ...*core.VoxelObject) (*GpuBufferManager, *core.Scene, *core.CameraState) {
	t.Helper()
	m, _ := scheduleFixture(t, objects...)
	s := core.NewScene()
	s.Objects = objects
	s.Lights = []core.Light{{Position: [4]float32{0, 0, 0, 1}, Params: [4]float32{20, .8, float32(core.LightTypePoint), 1}}}
	c := core.NewCameraState()
	c.Position = mgl32.Vec3{-distance, 0, 0}
	r2aCommit(m, s, c)
	return m, s, c
}

// Expected face membership comes from independent geometric fixtures. Only
// scheduled work and the serialized light boundary certify acknowledgement.
func r2iDrain(t *testing.T, m *GpuBufferManager, s *core.Scene, c *core.CameraState, frame uint64, want ...uint32) {
	t.Helper()
	pending := map[uint32]bool{}
	for _, face := range want {
		pending[face] = true
	}
	for n := 0; n < 12 && len(pending) > 0; n++ {
		u := m.BuildShadowUpdates(s, c, frame+uint64(n), false)
		if r2eReady(m, s, 0) {
			t.Fatal("point light ready with current faces still unrecorded")
		}
		budget := 0
		for _, tier := range r2eTiers {
			if tier.tier == m.ShadowLayerParams[s.Lights[0].ShadowMeta[0]].Tier {
				budget = tier.faces
			}
		}
		if budget == 0 {
			t.Fatal("unrecognized tier in fixture")
		}
		if len(u) != min(budget, len(pending)) {
			t.Fatalf("scheduled %d faces, want %d pending-only faces: %+v", len(u), min(budget, len(pending)), u)
		}
		for _, update := range u {
			if update.Kind != core.ShadowUpdateKindPoint || update.LightIndex != 0 || !pending[update.CascadeIndex] {
				t.Fatalf("scheduled clean/unexpected face %+v; pending %v", update, pending)
			}
			delete(pending, update.CascadeIndex)
		}
		m.RecordShadowUpdates(u, frame+uint64(n), s.ShadowRevision())
		if r2eReady(m, s, 0) != (len(pending) == 0) {
			t.Fatalf("GPU point readiness disagrees with independently current six faces; pending %v", pending)
		}
	}
	if len(pending) > 0 {
		t.Fatalf("faces failed to drain: %v", pending)
	}
	if !r2eReady(m, s, 0) {
		t.Fatal("six current faces did not publish GPU-ready point light")
	}
	if u := m.BuildShadowUpdates(s, c, frame+1000000, false); len(u) != 0 {
		t.Fatalf("clean point faces woke after long idle: %+v", u)
	}
}

func r2iWarm(t *testing.T, m *GpuBufferManager, s *core.Scene, c *core.CameraState) {
	t.Helper()
	r2iDrain(t, m, s, c, 1, 0, 1, 2, 3, 4, 5)
}

func TestR2iAxisCasterChangesOnlyItsFace(t *testing.T) {
	for face, axis := range r2iAxes {
		for _, change := range []string{"revision", "metadata", "transform", "geometry allocation", "material allocation", "opacity upload", "selected map", "disable casting", "disable rendering"} {
			t.Run(fmt.Sprintf("face%d/%s", face, change), func(t *testing.T) {
				o := r2iObject(1, axis)
				m, s, c := r2iFixture(t, 10, o)
				r2iWarm(t, m, s, c)
				switch change {
				case "revision":
					o.XBrickMap.Revision++
				case "metadata":
					o.EmitterLinkID++
				case "transform":
					o.Transform.Position = o.Transform.Position.Add(axis.Mul(.01))
					o.Transform.Dirty = true
				case "geometry allocation":
					a := *m.Allocations[o.XBrickMap]
					m.Allocations[o.XBrickMap] = &a
				case "material allocation":
					a := *m.MaterialAllocations[o]
					m.MaterialAllocations[o] = &a
				case "selected map":
					o.XBrickMap = c3h9Map(9, [3]int{})
					o.XBrickMap.ClearDirty()
				case "disable casting":
					o.CastsShadows = false
				case "disable rendering":
					o.RenderEnabled = false
				case "opacity upload":
					o.MaterialTable[1].Transparency = .5
					m.MaterialBufferGeneration++
					scheduleRun(t, m, s)
					if m.VoxelMaterialsUploaded != 1 {
						t.Fatal("fixture did not upload opacity")
					}
				}
				r2aCommit(m, s, c)
				r2iDrain(t, m, s, c, 1000, uint32(face))
			})
		}
	}
}

func TestR2iSeamAndConservativeBounds(t *testing.T) {
	cases := []struct {
		name   string
		bounds *[2]mgl32.Vec3
		faces  []uint32
	}{
		{"zero-size seam contact", &[2]mgl32.Vec3{{8, 8, 0}, {8, 8, 0}}, []uint32{0, 2}},
		{"XY seam", &[2]mgl32.Vec3{{7.5, 7.5, -.5}, {8.5, 8.5, .5}}, []uint32{0, 2}},
		{"XYZ corner", &[2]mgl32.Vec3{{7.5, 7.5, 7.5}, {8.5, 8.5, 8.5}}, []uint32{0, 2, 4}},
		{"boundary contact", &[2]mgl32.Vec3{{8, 7, -.5}, {9, 8, .5}}, []uint32{0, 2}},
		{"numerical guard", &[2]mgl32.Vec3{{8, 7, -.5}, {9, math.Nextafter32(8, 0), .5}}, []uint32{0, 2}},
		{"origin straddle", &[2]mgl32.Vec3{{-1, -1, -1}, {1, 1, 1}}, []uint32{0, 1, 2, 3, 4, 5}},
		{"nil", nil, []uint32{0, 1, 2, 3, 4, 5}},
		{"reversed", &[2]mgl32.Vec3{{2, 0, 0}, {1, 1, 1}}, []uint32{0, 1, 2, 3, 4, 5}},
		{"nan", &[2]mgl32.Vec3{{float32(math.NaN()), 0, 0}, {1, 1, 1}}, []uint32{0, 1, 2, 3, 4, 5}},
		{"infinity", &[2]mgl32.Vec3{{0, 0, 0}, {float32(math.Inf(1)), 1, 1}}, []uint32{0, 1, 2, 3, 4, 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := r2iObject(1, r2iAxes[0])
			m, s, c := r2iFixture(t, 10, o)
			// Scene selection is an upstream owner. Publish a selected caster with the
			// exact bounds under test, including unknown bounds, without culling it away.
			o.WorldAABB = tc.bounds
			s.ShadowObjects = []*core.VoxelObject{o}
			r2iWarm(t, m, s, c)
			o.ShadowGroupID++
			r2iDrain(t, m, s, c, 1000, tc.faces...)
		})
	}
}

func TestR2iMovementInsertionRemovalTrackOldAndNewFaces(t *testing.T) {
	for _, change := range []string{"move", "insert", "remove"} {
		t.Run(change, func(t *testing.T) {
			o := r2iObject(1, r2iAxes[0])
			m, s, c := r2iFixture(t, 10, o)
			r2iWarm(t, m, s, c)
			want := []uint32{0}
			switch change {
			case "move":
				o.Transform.Position = r2iAxes[5].Sub(mgl32.Vec3{.5, .5, .5})
				o.Transform.Dirty = true
				want = []uint32{0, 5}
			case "insert":
				s.Objects = append(s.Objects, r2iObject(2, r2iAxes[5]))
				want = []uint32{5}
			case "remove":
				s.Objects = nil
			}
			r2aCommit(m, s, c)
			r2iDrain(t, m, s, c, 1000, want...)
			if change == "remove" {
				o.XBrickMap.Revision++
				if u := m.BuildShadowUpdates(s, c, 2000, false); len(u) != 0 {
					t.Fatal("removed caster retained dependency")
				}
			}
		})
	}
}

func TestR2iSelectedCasterBeyondLightRangeStillInvalidatesFace(t *testing.T) {
	o := r2iObject(1, mgl32.Vec3{200, 0, 0})
	m, s, c := r2iFixture(t, 10, o)
	if len(s.ShadowObjects) != 0 {
		t.Fatal("fixture should be outside upstream local range selection")
	}
	s.ShadowObjects = []*core.VoxelObject{o}
	r2iWarm(t, m, s, c)
	o.EmitterLinkID++
	r2iDrain(t, m, s, c, 1000, 0)
}

func TestR2iSharedMapUploadInvalidatesBothPlacementsFaces(t *testing.T) {
	a, b := r2iObject(1, r2iAxes[0]), r2iObject(2, r2iAxes[5])
	b.XBrickMap = a.XBrickMap
	m, s, c := r2iFixture(t, 10, a, b)
	r2iWarm(t, m, s, c)
	a.XBrickMap.DirtyBricks[[6]int{}] = true
	scheduleRun(t, m, s)
	if m.VoxelBricksUploaded != 1 {
		t.Fatalf("shared fixture uploaded %d bricks, want one", m.VoxelBricksUploaded)
	}
	r2aCommit(m, s, c)
	r2iDrain(t, m, s, c, 1000, 0, 5)
}

func TestR2iBuildWithoutRecordAndSecondEditPreserveOtherAcknowledgements(t *testing.T) {
	a, b := r2iObject(1, r2iAxes[0]), r2iObject(2, r2iAxes[5])
	m, s, c := r2iFixture(t, 160, a, b)
	r2iWarm(t, m, s, c)
	a.XBrickMap.Revision++
	b.XBrickMap.Revision++
	r2aCommit(m, s, c)
	first := m.BuildShadowUpdates(s, c, 1000, false)
	second := m.BuildShadowUpdates(s, c, 2000, false)
	if len(first) != 1 || first[0].CascadeIndex != 0 || !reflect.DeepEqual(first, second) {
		t.Fatalf("Build without Record must keep pending oldest dirty face 0: %+v / %+v", first, second)
	}
	if r2eReady(m, s, 0) {
		t.Fatal("Build acknowledged unrecorded point work")
	}
	m.RecordShadowUpdates(first, 2000, s.ShadowRevision())
	if r2eReady(m, s, 0) {
		t.Fatal("unrecorded face 5 became ready")
	}
	b.EmitterLinkID++
	r2aCommit(m, s, c)
	// Face 0 was acknowledged and its inputs did not change. A second edit to
	// face 5 must not discard that independent acknowledgement.
	r2iDrain(t, m, s, c, 2001, 5)
}

func TestR2iColdAndGlobalChangesRespectEveryTierBudget(t *testing.T) {
	for _, tier := range r2eTiers {
		for _, change := range []string{"cold", "unknown upload", "light emitter", "source radius", "light position", "light range", "tier transition"} {
			t.Run(tier.name+"/"+change, func(t *testing.T) {
				m, s, c := r2iFixture(t, tier.x, r2iObject(1, r2iAxes[0]))
				if m.ShadowLayerParams[0].Tier != tier.tier {
					t.Fatal("incorrect tier fixture")
				}
				if change != "cold" {
					r2iWarm(t, m, s, c)
				}
				switch change {
				case "unknown upload":
					m.VoxelUploadRevision++
				case "light emitter":
					s.Lights[0].ShadowMeta[3]++
				case "source radius":
					s.Lights[0].Position[3]++
				case "light position":
					s.Lights[0].Position[0] += .01
				case "light range":
					s.Lights[0].Params[0]++
				case "tier transition":
					if tier.tier == core.ShadowTierHero {
						c.Position[0] = -160
					} else {
						c.Position[0] = -10
					}
				}
				r2aCommit(m, s, c)
				r2iDrain(t, m, s, c, 1000, 0, 1, 2, 3, 4, 5)
			})
		}
	}
}

func TestR2iFaceResolutionAndHighIndexDirtyFaceIgnoreOlderCleanFaces(t *testing.T) {
	for _, tier := range r2eTiers {
		for _, change := range []string{"resolution", "caster"} {
			t.Run(tier.name+"/"+change, func(t *testing.T) {
				o := r2iObject(1, r2iAxes[5])
				m, s, c := r2iFixture(t, tier.x, o)
				r2iWarm(t, m, s, c)
				if change == "resolution" {
					layer := &m.ShadowLayerParams[s.Lights[0].ShadowMeta[0]+5]
					if layer.EffectiveResolution == 128 {
						layer.EffectiveResolution = 256
					} else {
						layer.EffectiveResolution = 128
					}
				} else {
					o.XBrickMap.Revision++
					r2aCommit(m, s, c)
				}
				// Older clean faces must be filtered before the existing rotation/budget.
				// Build is needed to publish a caller-supplied layer resolution change.
				u := m.BuildShadowUpdates(s, c, 1000, false)
				if len(u) != 1 || u[0].CascadeIndex != 5 {
					t.Fatalf("high face dirty alone must schedule immediately, got %+v", u)
				}
				if r2eReady(m, s, 0) {
					t.Fatal("changed face remained GPU-ready before Record")
				}
				m.RecordShadowUpdates(u, 1000, s.ShadowRevision())
				r2iDrain(t, m, s, c, 1001)
			})
		}
	}
}

func TestR2iLightRemovalReassignmentAndReaddCannotReuseRetiredFaces(t *testing.T) {
	m, s, c := r2iFixture(t, 10, r2iObject(1, r2iAxes[0]))
	r2iWarm(t, m, s, c)
	original := s.Lights[0]
	s.Lights = nil
	r2aCommit(m, s, c)
	if u := m.BuildShadowUpdates(s, c, 1000, false); len(u) != 0 {
		t.Fatal("removed light scheduled work")
	}
	// Reuse atlas layers with a spot owner, then re-add the original point owner.
	spot := r2aLight(core.LightTypeSpot, 0)
	s.Lights = []core.Light{spot}
	r2aCommit(m, s, c)
	r2eWarm(t, m, s, c)
	s.Lights = []core.Light{original}
	r2aCommit(m, s, c)
	r2iDrain(t, m, s, c, 1001, 0, 1, 2, 3, 4, 5)
}

func TestR2iPointFaceChangePreservesMixedLightReadiness(t *testing.T) {
	o := r2iObject(1, r2iAxes[5])
	m, s, c := r2iFixture(t, 10, o)
	spot := r2aLight(core.LightTypeSpot, 100)
	directional := core.Light{Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}}
	s.Lights = append(s.Lights, spot, directional)
	r2aCommit(m, s, c)
	r2eWarm(t, m, s, c)
	m.ShadowLayerParams[s.Lights[0].ShadowMeta[0]+5].EffectiveResolution /= 2
	u := m.BuildShadowUpdates(s, c, 1000, false)
	if len(u) != 1 || u[0].LightIndex != 0 || u[0].CascadeIndex != 5 {
		t.Fatalf("face-only resolution change woke mixed lights: %+v", u)
	}
	if !r2eReady(m, s, 1) || !r2eReady(m, s, 2) {
		t.Fatal("unaffected mixed lights lost serialized readiness")
	}
	m.RecordShadowUpdates(u, 1000, s.ShadowRevision())
	if !r2eReady(m, s, 0) {
		t.Fatal("point did not become ready after only changed face recorded")
	}
}

func TestR2iTranslatedSignedAxesPreserveFaceDependencies(t *testing.T) {
	for _, origin := range []mgl32.Vec3{{1e6, -1e6, 1e6}, {-1e6, 1e6, -1e6}} {
		for face, axis := range r2iAxes {
			t.Run(fmt.Sprintf("origin%v/face%d", origin, face), func(t *testing.T) {
				o := r2iObject(1, origin.Add(axis))
				m, s, c := r2iFixture(t, 10, o)
				s.Lights[0].Position = [4]float32{origin[0], origin[1], origin[2], 1}
				c.Position = origin.Add(mgl32.Vec3{-10, 0, 0})
				r2aCommit(m, s, c)
				r2iWarm(t, m, s, c)
				o.ShadowGroupID++
				r2aCommit(m, s, c)
				r2iDrain(t, m, s, c, 1000, uint32(face))
			})
		}
	}
}

func TestR2iIdenticalOpacityAndFailedUploadKeepFacesReady(t *testing.T) {
	for _, change := range []string{"identical opacity", "failed geometry upload"} {
		t.Run(change, func(t *testing.T) {
			o := r2iObject(1, r2iAxes[5])
			m, s, c := r2iFixture(t, 10, o)
			r2iWarm(t, m, s, c)
			switch change {
			case "identical opacity":
				m.MaterialBufferGeneration++
				scheduleRun(t, m, s)
				if m.VoxelMaterialsUploaded != 1 {
					t.Fatal("fixture did not reupload identical opacity")
				}
			case "failed geometry upload":
				o.XBrickMap.DirtyBricks[[6]int{}] = true
				before := m.VoxelUploadRevision
				calls := 0
				m.serviceVoxelUploads(s, func(w voxelUploadWork) bool { calls++; return false })
				if calls == 0 || m.VoxelUploadRevision != before {
					t.Fatal("fixture did not preserve revision after failed upload")
				}
			}
			r2aCommit(m, s, c)
			r2iDrain(t, m, s, c, 1000)
		})
	}
}

func TestR2iSecondEditToAcknowledgedFaceRequiresItsNewRecord(t *testing.T) {
	a, b := r2iObject(1, r2iAxes[0]), r2iObject(2, r2iAxes[5])
	m, s, c := r2iFixture(t, 160, a, b)
	r2iWarm(t, m, s, c)
	a.XBrickMap.Revision++
	b.XBrickMap.Revision++
	r2aCommit(m, s, c)
	first := m.BuildShadowUpdates(s, c, 1000, false)
	if len(first) != 1 || first[0].CascadeIndex != 0 {
		t.Fatalf("fixture did not schedule first dirty face: %+v", first)
	}
	m.RecordShadowUpdates(first, 1000, s.ShadowRevision())
	a.EmitterLinkID++
	r2aCommit(m, s, c)
	r2iDrain(t, m, s, c, 1001, 0, 5)
}

func TestR2iRebasedPackedBoundsRetainWorldAndGPUFaces(t *testing.T) {
	o := r2iObject(1, mgl32.Vec3{1e6 + 12.5, 1e6 + 14.5, 0})
	m, s, c := r2iFixture(t, 10, o)
	s.Lights[0].Position = [4]float32{1e6, 1e6, 0, 1}
	c.Position = mgl32.Vec3{1e6 - 10, 1e6, 0}
	r2aCommit(m, s, c)
	o.WorldAABB = &[2]mgl32.Vec3{{1e6 + 12, 1e6 + 14, -.5}, {1e6 + 13, 1e6 + 15, .5}}
	s.ShadowObjects = []*core.VoxelObject{o}
	m.RenderOrigin = mgl32.Vec3{-1e8, 1e6, 0}
	// Independent fixture arithmetic matches the float32 values packed for GPU
	// instances/lights. The world-space box is strictly +Y, but rebasing rounds
	// its X endpoints to one coordinate 16 units from the packed light.
	packedLight := mgl32.Vec3{1e6, 1e6, 0}.Sub(m.RenderOrigin)
	packedMin := o.WorldAABB[0].Sub(m.RenderOrigin)
	packedMax := o.WorldAABB[1].Sub(m.RenderOrigin)
	if packedLight != (mgl32.Vec3{101000000, 0, 0}) || packedMin != (mgl32.Vec3{101000016, 14, -.5}) || packedMax != (mgl32.Vec3{101000016, 15, .5}) {
		t.Fatalf("fixture does not reproduce packed rounding: light%v bounds%v..%v", packedLight, packedMin, packedMax)
	}
	r2iWarm(t, m, s, c)
	o.EmitterLinkID++
	// Do not re-commit: preserve the explicitly selected authored world bounds.
	r2iDrain(t, m, s, c, 1000, 0, 2)
}

func TestR2iOrdinaryRenderOriginChangesPreservePointCache(t *testing.T) {
	for _, origin := range []mgl32.Vec3{{1, 2, 3}, {-1, -2, -3}, {.01, .02, .03}} {
		t.Run(fmt.Sprint(origin), func(t *testing.T) {
			o := r2iObject(1, r2iAxes[0])
			m, s, c := r2iFixture(t, 10, o)
			r2iWarm(t, m, s, c)
			before := m.VoxelUploadRevision
			m.RenderOrigin = origin
			// Camera rebasing alone keeps world-owned maps reusable when the
			// conservative union of world and packed face membership is unchanged.
			r2iDrain(t, m, s, c, 1000)
			if m.VoxelUploadRevision != before {
				t.Fatal("origin fixture unexpectedly changed upload revision")
			}
		})
	}
}

func TestR2iOriginChangeDuringPartialRefreshPreservesAcknowledgements(t *testing.T) {
	a, b := r2iObject(1, r2iAxes[0]), r2iObject(2, r2iAxes[5])
	m, s, c := r2iFixture(t, 160, a, b)
	r2iWarm(t, m, s, c)
	a.EmitterLinkID++
	b.EmitterLinkID++
	r2aCommit(m, s, c)
	first := m.BuildShadowUpdates(s, c, 1000, false)
	if len(first) != 1 || first[0].CascadeIndex != 0 {
		t.Fatalf("fixture did not build pending original face: %+v", first)
	}
	m.RecordShadowUpdates(first, 1000, s.ShadowRevision())
	m.RenderOrigin = mgl32.Vec3{1, 2, 3}
	// Unchanged union membership must preserve face 0's acknowledgement and
	// all previously clean faces while the independent face 5 edit is pending.
	r2iDrain(t, m, s, c, 1001, 5)
}

func TestR2iLargeOriginTransitionRefreshesOnlyNewPackedMembership(t *testing.T) {
	o := r2iObject(1, mgl32.Vec3{1e6 + 12.5, 1e6 + 14.5, 0})
	m, s, c := r2iFixture(t, 10, o)
	s.Lights[0].Position = [4]float32{1e6, 1e6, 0, 1}
	c.Position = mgl32.Vec3{1e6 - 10, 1e6, 0}
	r2aCommit(m, s, c)
	o.WorldAABB = &[2]mgl32.Vec3{{1e6 + 12, 1e6 + 14, -.5}, {1e6 + 13, 1e6 + 15, .5}}
	s.ShadowObjects = []*core.VoxelObject{o}
	r2iWarm(t, m, s, c)
	// The world +Y face remains current. This origin introduces only the
	// rounded GPU +X footprint, as checked independently in the packed test.
	m.RenderOrigin = mgl32.Vec3{-1e8, 1e6, 0}
	r2iDrain(t, m, s, c, 1000, 0)
	// Moving the origin again within the same union footprint adds no work.
	m.RenderOrigin = mgl32.Vec3{-1e8, 1e6 + 1, 0}
	r2iDrain(t, m, s, c, 2000)
}

func TestR2iNonfiniteRebasingConservativelyRetainsEveryFace(t *testing.T) {
	for _, bad := range []float32{float32(math.Inf(1)), float32(math.NaN())} {
		t.Run(fmt.Sprint(bad), func(t *testing.T) {
			o := r2iObject(1, r2iAxes[0])
			m, s, c := r2iFixture(t, 10, o)
			m.RenderOrigin = mgl32.Vec3{bad, 0, 0}
			r2iWarm(t, m, s, c)
			o.EmitterLinkID++
			r2iDrain(t, m, s, c, 1000, 0, 1, 2, 3, 4, 5)
		})
	}
}
