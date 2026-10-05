package gpu

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

const r2fIdleFrame = uint64(1000000)

func TestR2fUnchangedDirectionalCascadesStayCachedAfterLongIdle(t *testing.T) {
	m, s, c := r2bFixture(t)
	for _, frame := range []uint64{r2bFrame + 1, r2bFrame + 2, r2fIdleFrame, r2fIdleFrame + 1000000} {
		r2aCommit(m, s, c)
		u := r2bAssert(t, m, s, c, frame, nil)
		if len(u) != 0 {
			t.Errorf("unchanged mixed scene scheduled %d updates at frame %d", len(u), frame)
		}
		if !r2eReady(m, s, 1) {
			t.Fatal("idle remote local shadow became disabled")
		}
	}
}

func TestR2fRemoteUploadDoesNotWakeIdleDirectionalCascades(t *testing.T) {
	m, s, c := r2bFixture(t)
	s.Objects[2].XBrickMap.DirtyBricks[[6]int{}] = true
	scheduleRun(t, m, s)
	if m.VoxelUploadBytes == 0 {
		t.Fatal("fixture did not successfully upload remote geometry")
	}
	r2aCommit(m, s, c)
	u := r2bAssert(t, m, s, c, r2fIdleFrame, nil)
	if len(u) != 1 || u[0].Kind != core.ShadowUpdateKindSpot || u[0].LightIndex != 1 {
		t.Errorf("remote upload must refresh only its local light, got %+v", u)
	}
	m.RecordShadowUpdates(u, r2fIdleFrame, s.ShadowRevision())
	if !r2eReady(m, s, 1) {
		t.Fatal("remote local shadow failed to become ready")
	}
	r2bAssert(t, m, s, c, r2fIdleFrame+1000000, nil)
}

func TestR2fLongIdleCasterChangesScheduleExactCascades(t *testing.T) {
	for _, change := range []string{"near revision", "far revision", "far upload", "far remove", "far move across", "far opacity upload"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2bFixture(t)
			r2bAssert(t, m, s, c, r2fIdleFrame, nil)
			o := s.Objects[1]
			want := []uint32{1}
			switch change {
			case "near revision":
				s.Objects[0].XBrickMap.Revision++
				want = []uint32{0, 1}
			case "far revision":
				o.XBrickMap.Revision++
			case "far upload":
				o.XBrickMap.DirtyBricks[[6]int{}] = true
				scheduleRun(t, m, s)
				if m.VoxelUploadBytes == 0 {
					t.Fatal("fixture did not upload far geometry")
				}
			case "far remove":
				s.Objects = append(s.Objects[:1], s.Objects[2:]...)
			case "far move across":
				o.Transform.Position = s.Objects[0].Transform.Position
				o.Transform.Dirty = true
				want = []uint32{0, 1}
			case "far opacity upload":
				o.MaterialTable[1].Transparency = 1
				m.MaterialBufferGeneration++
				scheduleRun(t, m, s)
				if m.VoxelMaterialsUploaded != 3 {
					t.Fatal("fixture did not refresh palettes")
				}
			}
			r2aCommit(m, s, c)
			u := r2bAssert(t, m, s, c, r2fIdleFrame+1, want)
			for _, update := range u {
				if update.Kind != core.ShadowUpdateKindDirectional {
					t.Error("unrelated local shadow refreshed")
				}
			}
			m.RecordShadowUpdates(u, r2fIdleFrame+1, s.ShadowRevision())
			r2bAssert(t, m, s, c, r2fIdleFrame+1000000, nil)
		})
	}
}

func TestR2fLongIdleProjectionAndMetadataChangesRefreshCurrentInputs(t *testing.T) {
	for _, change := range []string{"near projection", "far inverse", "far params", "near resolution", "emitter link", "direction", "light color"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2bFixture(t)
			r2bAssert(t, m, s, c, r2fIdleFrame, nil)
			want := []uint32{0, 1}
			switch change {
			case "near projection":
				s.Lights[0].DirectionalCascades[0].ViewProj[12] += .001
				want = []uint32{0}
			case "far inverse":
				s.Lights[0].DirectionalCascades[1].InvViewProj[12] += .001
				want = []uint32{1}
			case "far params":
				s.Lights[0].DirectionalCascades[1].Params[2] += .001
				want = []uint32{1}
			case "near resolution":
				m.ShadowLayerParams[s.Lights[0].ShadowMeta[0]].EffectiveResolution /= 2
				want = []uint32{0}
			case "emitter link":
				s.Lights[0].ShadowMeta[3]++
			case "direction":
				s.Lights[0].Direction[0] = .1
				r2aCommit(m, s, c)
			case "light color":
				s.Lights[0].Color[0] = .5
				want = nil
			}
			u := r2bAssert(t, m, s, c, r2fIdleFrame+1, want)
			m.RecordShadowUpdates(u, r2fIdleFrame+1, s.ShadowRevision())
			// Keep caller-supplied matrices unchanged; UpdateLights would replace them.
			r2bAssert(t, m, s, c, r2fIdleFrame+1000000, nil)
		})
	}
}

func TestR2fBuildAndPartialRecordKeepEachCascadePending(t *testing.T) {
	for _, ackCascade := range []uint32{0, 1} {
		t.Run(fmt.Sprint(ackCascade), func(t *testing.T) {
			m, s, c := r2bFixture(t)
			s.Objects[0].XBrickMap.Revision++
			r2aCommit(m, s, c)
			first := r2bAssert(t, m, s, c, r2fIdleFrame, []uint32{0, 1})
			second := r2bAssert(t, m, s, c, r2fIdleFrame+1, []uint32{0, 1})
			if !reflect.DeepEqual(first, second) {
				t.Fatal("Build without Record changed pending cascade work")
			}
			var recorded []core.ShadowUpdate
			for _, update := range first {
				if update.Kind == core.ShadowUpdateKindDirectional && update.CascadeIndex == ackCascade {
					recorded = append(recorded, update)
				}
			}
			if len(recorded) != 1 {
				t.Fatal("fixture failed to schedule acknowledged cascade")
			}
			m.RecordShadowUpdates(recorded, r2fIdleFrame+1, s.ShadowRevision())
			pending := r2bAssert(t, m, s, c, r2fIdleFrame+1000, []uint32{1 - ackCascade})
			m.RecordShadowUpdates(pending, r2fIdleFrame+1000, s.ShadowRevision())
			r2bAssert(t, m, s, c, r2fIdleFrame+1000000, nil)
		})
	}
}

func TestR2fUnknownRevisionAfterLongIdleInvalidatesAllCurrentLayers(t *testing.T) {
	m, s, c := r2bFixture(t)
	r2bAssert(t, m, s, c, r2fIdleFrame, nil)
	m.VoxelUploadRevision++
	r2aCommit(m, s, c)
	if r2eReady(m, s, 1) {
		t.Fatal("unknown upload left local shadow ready")
	}
	u := r2bAssert(t, m, s, c, r2fIdleFrame+1, []uint32{0, 1})
	if len(u) != 3 {
		t.Errorf("unknown upload should invalidate both cascades and local light, got %d", len(u))
	}
	m.RecordShadowUpdates(u, r2fIdleFrame+1, s.ShadowRevision())
	idle := r2bAssert(t, m, s, c, r2fIdleFrame+1000000, nil)
	if len(idle) != 0 {
		t.Error("unknown epoch stayed pending after Record")
	}
}

func TestR2fExplicitForceAndCameraProjectionRefreshWithoutWakingLocals(t *testing.T) {
	for _, change := range []string{"explicit force", "camera projection"} {
		t.Run(change, func(t *testing.T) {
			m, s, c := r2bFixture(t)
			r2bAssert(t, m, s, c, r2fIdleFrame, nil)
			force := change == "explicit force"
			if !force {
				c.Position[0] += 2
				c.LookAt[0] += 2
				r2aCommit(m, s, c)
			}
			u := m.BuildShadowUpdates(s, c, r2fIdleFrame+1, force)
			seen := map[uint32]bool{}
			for _, update := range u {
				if update.Kind != core.ShadowUpdateKindDirectional {
					t.Error("directional refresh woke valid local shadow")
					continue
				}
				seen[update.CascadeIndex] = true
			}
			if len(u) != 2 || !seen[0] || !seen[1] {
				t.Errorf("%s must refresh both cascades, got %+v", change, u)
			}
			m.RecordShadowUpdates(u, r2fIdleFrame+1, s.ShadowRevision())
			idle := r2bAssert(t, m, s, c, r2fIdleFrame+1000000, nil)
			if len(idle) != 0 {
				t.Error("recorded mixed-scene shadows did not remain cached")
			}
		})
	}
}

func TestR2fPartialForcedRecordDoesNotAcknowledgeOtherChangedProjection(t *testing.T) {
	m, s, c := r2bFixture(t)
	s.Lights[0].DirectionalCascades[0].ViewProj[12] += .001
	s.Lights[0].DirectionalCascades[1].ViewProj[12] += .001
	forced := m.BuildShadowUpdates(s, c, r2fIdleFrame, true)
	if len(forced) != 2 {
		t.Fatal("fixture must force both changed projections")
	}
	var near []core.ShadowUpdate
	for _, update := range forced {
		if update.Kind == core.ShadowUpdateKindDirectional && update.CascadeIndex == 0 {
			near = append(near, update)
		}
	}
	if len(near) != 1 {
		t.Fatal("fixture did not schedule near projection")
	}
	m.RecordShadowUpdates(near, r2fIdleFrame, s.ShadowRevision())
	pending := r2bAssert(t, m, s, c, r2fIdleFrame+1000, []uint32{1})
	m.RecordShadowUpdates(pending, r2fIdleFrame+1000, s.ShadowRevision())
	r2bAssert(t, m, s, c, r2fIdleFrame+1000000, nil)
}
