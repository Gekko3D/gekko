package gpu

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

var r2eTiers = []struct {
	name          string
	x             float32
	tier          uint32
	lights, faces int
}{
	{"hero", 10, core.ShadowTierHero, 4, 3}, {"near", 40, core.ShadowTierNear, 3, 2}, {"medium", 80, core.ShadowTierMedium, 2, 1}, {"far", 160, core.ShadowTierFar, 1, 1},
}

func r2eFixture(t *testing.T, kind uint32, x float32, count int) (*GpuBufferManager, *core.Scene, *core.CameraState) {
	t.Helper()
	o := r2aObject(1, x)
	m, _ := scheduleFixture(t, o)
	s := core.NewScene()
	s.Objects = []*core.VoxelObject{o}
	for i := 0; i < count; i++ {
		s.Lights = append(s.Lights, r2aLight(kind, x))
	}
	c := core.NewCameraState()
	c.Position = mgl32.Vec3{}
	r2aCommit(m, s, c)
	return m, s, c
}

// Readiness is checked at the serialized GPU light boundary, not cache internals.
func r2eReady(m *GpuBufferManager, s *core.Scene, index int) bool {
	data := m.buildLightsDataForGPU(s.Lights, s.ShadowRevision())
	return lightParamsWFromData(data, index) == 1 && lightShadowLayerCountFromData(data, index) == s.Lights[index].ShadowMeta[1]
}
func r2eWarm(t *testing.T, m *GpuBufferManager, s *core.Scene, c *core.CameraState) {
	t.Helper()
	for frame := uint64(0); frame < 64; frame++ {
		u := m.BuildShadowUpdates(s, c, frame, false)
		m.RecordShadowUpdates(u, frame, s.ShadowRevision())
		all := true
		for i := range s.Lights {
			all = all && r2eReady(m, s, i)
		}
		if all {
			return
		}
	}
	t.Fatal("fixture failed to record every current local layer")
}
func r2eIdle(t *testing.T, m *GpuBufferManager, s *core.Scene, c *core.CameraState, frame uint64) {
	t.Helper()
	r2aCommit(m, s, c)
	if u := m.BuildShadowUpdates(s, c, frame, false); len(u) != 0 {
		t.Errorf("valid unchanged local shadows scheduled %d updates at frame %d", len(u), frame)
	}
	for i := range s.Lights {
		if !r2eReady(m, s, i) {
			t.Errorf("cached local light %d disabled at frame %d", i, frame)
		}
	}
}

func TestR2eUnchangedLocalShadowsRemainCachedAcrossAllTiers(t *testing.T) {
	for _, kind := range []uint32{core.LightTypeSpot, core.LightTypePoint} {
		for _, tier := range r2eTiers {
			t.Run(fmt.Sprintf("kind%d/%s", kind, tier.name), func(t *testing.T) {
				m, s, c := r2eFixture(t, kind, tier.x, 1)
				if m.ShadowLayerParams[0].Tier != tier.tier {
					t.Fatal("fixture classified incorrect tier")
				}
				r2eWarm(t, m, s, c)
				for _, frame := range []uint64{1024, 1025, 1000000} {
					r2eIdle(t, m, s, c, frame)
				}
			})
		}
	}
}

func TestR2eInitialAndInvalidatedSpotBacklogsDrainWithinExistingBudgets(t *testing.T) {
	for _, tier := range r2eTiers {
		for _, initial := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/initial%v", tier.name, initial), func(t *testing.T) {
				count := tier.lights + 2
				m, s, c := r2eFixture(t, core.LightTypeSpot, tier.x, count)
				if !initial {
					r2eWarm(t, m, s, c)
					m.VoxelUploadRevision++
					r2aCommit(m, s, c)
				}
				seen := map[uint32]bool{}
				for frame := uint64(1000); len(seen) < count && frame < 1010; frame++ {
					u := m.BuildShadowUpdates(s, c, frame, false)
					remaining := count - len(seen)
					want := min(remaining, tier.lights)
					if len(u) != want {
						t.Errorf("pending %d spot lights scheduled %d, want budget-limited %d", remaining, len(u), want)
					}
					for _, update := range u {
						if update.Kind != core.ShadowUpdateKindSpot {
							t.Fatal("unexpected nonspot work")
						}
						if seen[update.LightIndex] {
							t.Errorf("already recorded light %d repeated while backlog remained", update.LightIndex)
						}
						seen[update.LightIndex] = true
					}
					m.RecordShadowUpdates(u, frame, s.ShadowRevision())
					for index := range s.Lights {
						if r2eReady(m, s, index) != seen[uint32(index)] {
							t.Errorf("light %d readiness does not match recorded work", index)
						}
					}
				}
				if len(seen) != count {
					t.Fatalf("backlog failed to drain: %d/%d lights", len(seen), count)
				}
				r2eIdle(t, m, s, c, 1000000)
			})
		}
	}
}

func TestR2ePartialPointFacesStayDirtyUntilSixRecordedThenIdle(t *testing.T) {
	for _, tier := range r2eTiers {
		t.Run(tier.name, func(t *testing.T) {
			m, s, c := r2eFixture(t, core.LightTypePoint, tier.x, 1)
			r2eWarm(t, m, s, c)
			m.VoxelUploadRevision++ // Unknown uploads invalidate the entire cubemap.
			r2aCommit(m, s, c)
			seen := map[uint32]bool{}
			for frame := uint64(1000); len(seen) < core.PointShadowFaceCount && frame < 1010; frame++ {
				if r2eReady(m, s, 0) {
					t.Fatal("point shadow enabled before six current faces recorded")
				}
				u := m.BuildShadowUpdates(s, c, frame, false)
				if len(u) != tier.faces {
					t.Fatalf("point scheduled %d faces, want tier budget %d", len(u), tier.faces)
				}
				for _, update := range u {
					if seen[update.CascadeIndex] {
						t.Errorf("face %d repeated before all current faces recorded", update.CascadeIndex)
					}
					seen[update.CascadeIndex] = true
				}
				m.RecordShadowUpdates(u, frame, s.ShadowRevision())
				if r2eReady(m, s, 0) != (len(seen) == core.PointShadowFaceCount) {
					t.Fatal("point readiness does not track six-face completion")
				}
			}
			if len(seen) != core.PointShadowFaceCount {
				t.Fatal("point faces failed to drain")
			}
			r2eIdle(t, m, s, c, 1000000)
		})
	}
}

func TestR2eBuildWithoutRecordCannotAcknowledgeDirtyWork(t *testing.T) {
	for _, kind := range []uint32{core.LightTypeSpot, core.LightTypePoint} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			m, s, c := r2eFixture(t, kind, 10, 1)
			r2eWarm(t, m, s, c)
			m.VoxelUploadRevision++ // Unknown uploads invalidate the entire cubemap.
			r2aCommit(m, s, c)
			first := m.BuildShadowUpdates(s, c, 1000, false)
			second := m.BuildShadowUpdates(s, c, 1000, false)
			if len(first) == 0 || !reflect.DeepEqual(first, second) {
				t.Fatal("Build without Record advanced dirty shadow work")
			}
			if r2eReady(m, s, 0) {
				t.Fatal("Build enabled dirty shadow before Record")
			}
			m.RecordShadowUpdates(first, 1000, s.ShadowRevision())
			if kind == core.LightTypePoint && r2eReady(m, s, 0) {
				t.Fatal("partial point recording enabled incomplete shadow")
			}
			for frame := uint64(1001); frame <= 1010 && !r2eReady(m, s, 0); frame++ {
				updates := m.BuildShadowUpdates(s, c, frame, false)
				m.RecordShadowUpdates(updates, frame, s.ShadowRevision())
			}
			if !r2eReady(m, s, 0) {
				t.Fatal("dirty work did not finish after monotonic recording")
			}
			r2eIdle(t, m, s, c, 1000000)
		})
	}
}

func TestR2eLongIdleDependencyChangesStillInvalidate(t *testing.T) {
	for _, kind := range []uint32{core.LightTypeSpot, core.LightTypePoint} {
		for _, change := range []string{"light", "caster", "geometry upload", "remove", "opacity upload", "unknown revision", "tier transition"} {
			t.Run(fmt.Sprintf("kind%d/%s", kind, change), func(t *testing.T) {
				m, s, c := r2eFixture(t, kind, 40, 1)
				r2eWarm(t, m, s, c)
				// Do not Record an unchanged scheduling result: idle cannot acknowledge edits.
				m.BuildShadowUpdates(s, c, 1000000, false)
				o := s.Objects[0]
				oldResolution := m.ShadowLayerParams[0].EffectiveResolution
				switch change {
				case "light":
					s.Lights[0].ShadowMeta[3]++
				case "caster":
					o.EmitterLinkID++
				case "geometry upload":
					o.XBrickMap.DirtyBricks[[6]int{}] = true
					scheduleRun(t, m, s)
					if m.VoxelUploadBytes == 0 {
						t.Fatal("fixture did not upload geometry")
					}
				case "remove":
					s.Objects = nil
				case "opacity upload":
					o.MaterialTable[1].Transparency = 1
					m.MaterialBufferGeneration++
					scheduleRun(t, m, s)
					if m.VoxelMaterialsUploaded != 1 {
						t.Fatal("fixture did not upload changed opacity")
					}
				case "unknown revision":
					m.VoxelUploadRevision++
				case "tier transition":
					c.Position[0] = 40
				}
				r2aCommit(m, s, c)
				if r2eReady(m, s, 0) {
					t.Fatal("changed local dependency remained GPU-ready")
				}
				u := m.BuildShadowUpdates(s, c, 1000001, false)
				if len(u) == 0 {
					t.Fatal("changed local dependency did not schedule after long idle")
				}
				if change == "tier transition" && u[0].Resolution == oldResolution {
					t.Fatal("fixture did not transition resolution")
				}
				// Complete only through actual scheduling and Record acknowledgements.
				for frame := uint64(1000001); frame < 1000010 && !r2eReady(m, s, 0); frame++ {
					u = m.BuildShadowUpdates(s, c, frame, false)
					m.RecordShadowUpdates(u, frame, s.ShadowRevision())
				}
				if !r2eReady(m, s, 0) {
					t.Fatal("dirty local dependency failed to finish")
				}
				r2eIdle(t, m, s, c, 2000000)
			})
		}
	}
}

func TestR2eDirectionalReuseAndForceDoNotWakeValidLocals(t *testing.T) {
	m, s, c := r2eFixture(t, core.LightTypeSpot, 40, 1)
	s.Lights = append(s.Lights, core.Light{Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}})
	r2aCommit(m, s, c)
	u := m.BuildShadowUpdates(s, c, 1000, true)
	m.RecordShadowUpdates(u, 1000, s.ShadowRevision())
	for _, tc := range []struct {
		frame uint64
		force bool
		want  int
	}{{1001, false, 0}, {1002, false, 0}, {1002, true, 2}, {1000000, false, 0}} {
		u = m.BuildShadowUpdates(s, c, tc.frame, tc.force)
		if len(u) != tc.want {
			t.Errorf("frame %d force=%v scheduled %d, want %d directional only", tc.frame, tc.force, len(u), tc.want)
		}
		for _, update := range u {
			if update.Kind != core.ShadowUpdateKindDirectional {
				t.Errorf("valid local shadow woke at frame %d force=%v", tc.frame, tc.force)
			}
		}
	}
}

func TestR2eSecondEditDuringPartialPointRefreshRequiresNewSixFaces(t *testing.T) {
	for _, tier := range r2eTiers {
		t.Run(tier.name, func(t *testing.T) {
			m, s, c := r2eFixture(t, core.LightTypePoint, tier.x, 1)
			r2eWarm(t, m, s, c)
			m.VoxelUploadRevision++ // Unknown uploads invalidate the entire cubemap.
			r2aCommit(m, s, c)
			first := m.BuildShadowUpdates(s, c, 1000, false)
			if len(first) != tier.faces {
				t.Fatal("fixture did not begin partial point refresh")
			}
			m.RecordShadowUpdates(first, 1000, s.ShadowRevision())
			if r2eReady(m, s, 0) {
				t.Fatal("partial first generation became ready")
			}
			m.VoxelUploadRevision++ // A second global change invalidates all six again.
			r2aCommit(m, s, c)
			if r2eReady(m, s, 0) {
				t.Fatal("second edit reused earlier generation acknowledgements")
			}
			current := map[uint32]bool{}
			for frame := uint64(1001); len(current) < core.PointShadowFaceCount && frame < 1010; frame++ {
				u := m.BuildShadowUpdates(s, c, frame, false)
				if len(u) != tier.faces {
					t.Fatalf("new generation scheduled %d faces want budget %d", len(u), tier.faces)
				}
				for _, update := range u {
					current[update.CascadeIndex] = true
				}
				m.RecordShadowUpdates(u, frame, s.ShadowRevision())
				if r2eReady(m, s, 0) != (len(current) == core.PointShadowFaceCount) {
					t.Fatalf("new generation ready before six current faces, have %d", len(current))
				}
			}
			if len(current) != core.PointShadowFaceCount {
				t.Fatal("second generation failed to drain")
			}
			r2eIdle(t, m, s, c, 1000000)
		})
	}
}
