package app

import (
	"strings"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

func r2gObject(position mgl32.Vec3) *core.VoxelObject {
	o := core.NewVoxelObject()
	o.Transform.Position = position
	o.Transform.Dirty = true
	o.MaterialTable = []core.Material{core.DefaultMaterial(), core.DefaultMaterial()}
	o.XBrickMap.SetVoxel(0, 0, 0, 1)
	o.XBrickMap.ClearDirty()
	return o
}
func r2gCommit(a *App, aspect float32) {
	a.Scene.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{CameraPosition: a.Camera.Position})
	a.BufferManager.UpdateLights(a.Scene, a.Camera, aspect)
}
func r2gFixture(t *testing.T) *App {
	t.Helper()
	a := NewApp(nil)
	a.BufferManager = &gpu.GpuBufferManager{}
	a.Camera.Position = mgl32.Vec3{0, 4, 45}
	a.Camera.LookAt = mgl32.Vec3{0, 4, 0}
	a.Scene.Objects = []*core.VoxelObject{r2gObject(mgl32.Vec3{0, 0, 0}), r2gObject(mgl32.Vec3{70, 0, -60})}
	a.Scene.Lights = []core.Light{
		{Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}},
		{Position: [4]float32{600, 10, 0, 0}, Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{20, .8, float32(core.LightTypeSpot), 1}},
	}
	r2gCommit(a, 1)
	if len(a.Scene.ShadowObjects) != 2 {
		t.Fatal("fixture did not select both opaque casters")
	}
	a.RenderFrameIndex = 32
	updates := a.BufferManager.BuildShadowUpdates(a.Scene, a.Camera, a.RenderFrameIndex, false)
	if len(updates) != 3 {
		t.Fatalf("fixture must initialize two cascades and one local light, got %+v", updates)
	}
	a.BufferManager.RecordShadowUpdates(updates, a.RenderFrameIndex, a.Scene.ShadowRevision())
	// The actual manager boundary must already consider all warmed dependencies idle.
	if pending := a.BufferManager.BuildShadowUpdates(a.Scene, a.Camera, 33, false); len(pending) != 0 {
		t.Fatalf("fixture manager did not warm all layers, got %+v", pending)
	}
	a.RenderFrameIndex = 1000000
	return a
}
func r2gAssertPass(t *testing.T, a *App, wantDirectional, wantMotion int) {
	t.Helper()
	err := a.recordShadowPass(nil)
	if wantDirectional == 0 {
		if err != nil {
			t.Errorf("unchanged shadow maps requested encoder: %v", err)
		}
	} else if err == nil || !strings.Contains(err.Error(), "shadow command encoder is nil") {
		t.Errorf("dirty shadow maps must require an encoder, got %v", err)
	}
	for key, want := range map[string]int{"ShadowUpdates": wantDirectional, "ShadowDirectionalUpdates": wantDirectional, "ShadowSpotUpdates": 0, "ShadowPointUpdates": 0, "ShadowCameraMotion": wantMotion} {
		if got := a.Profiler.Counts[key]; got != want {
			t.Errorf("%s=%d want %d", key, got, want)
		}
	}
}

func TestR2gMotionMetadataDoesNotWakeUnchangedShadowDependencies(t *testing.T) {
	for _, change := range []string{"no prior camera state", "yaw metadata", "pitch metadata", "stale previous position"} {
		t.Run(change, func(t *testing.T) {
			a := r2gFixture(t)
			before := a.Scene.Lights[0].DirectionalCascades
			a.HasLastCameraState = true
			a.LastCameraPos = a.Camera.Position
			a.LastCameraYaw = a.Camera.Yaw
			a.LastCameraPitch = a.Camera.Pitch
			switch change {
			case "no prior camera state":
				a.HasLastCameraState = false
			case "yaw metadata":
				a.Camera.Yaw += .2
			case "pitch metadata":
				a.Camera.Pitch += .2
			case "stale previous position":
				a.LastCameraPos[0] += 10
			}
			r2gCommit(a, 1)
			if a.Scene.Lights[0].DirectionalCascades != before {
				t.Fatal("metadata-only fixture changed actual directional projections")
			}
			r2gAssertPass(t, a, 0, 1)
			a.RenderFrameIndex += 1000
			r2gAssertPass(t, a, 0, 1)
		})
	}
}

func TestR2gActualCameraProjectionChangesStillScheduleBothCascades(t *testing.T) {
	for _, change := range []string{"translation", "look direction", "Fov", "aspect"} {
		t.Run(change, func(t *testing.T) {
			a := r2gFixture(t)
			before := a.Scene.Lights[0].DirectionalCascades
			a.HasLastCameraState = true
			a.LastCameraPos = a.Camera.Position
			a.LastCameraYaw = a.Camera.Yaw
			a.LastCameraPitch = a.Camera.Pitch
			aspect := float32(1)
			motion := 0
			switch change {
			case "translation":
				a.Camera.Position[0] += 2
				a.Camera.LookAt[0] += 2
				motion = 1
			case "look direction":
				a.Camera.LookAt[0] += 6
			case "Fov":
				a.Camera.Fov += 5
			case "aspect":
				aspect = 1.3
			}
			r2gCommit(a, aspect)
			for i, cascade := range a.Scene.Lights[0].DirectionalCascades {
				if cascade == before[i] {
					t.Fatalf("fixture failed to change cascade %d", i)
				}
			}
			r2gAssertPass(t, a, 2, motion)
			// A failed app recording attempt must not acknowledge either pending layer.
			a.RenderFrameIndex++
			r2gAssertPass(t, a, 2, motion)
		})
	}
}

func TestR2gOneChangedCascadeStaysIndependentDespiteMotionMetadata(t *testing.T) {
	for _, index := range []int{0, 1} {
		t.Run(string(rune('0'+index)), func(t *testing.T) {
			a := r2gFixture(t)
			a.HasLastCameraState = false
			a.Scene.Lights[0].DirectionalCascades[index].ViewProj[12] += .001
			r2gAssertPass(t, a, 1, 1)
			a.RenderFrameIndex++
			r2gAssertPass(t, a, 1, 1)
			updates := a.BufferManager.BuildShadowUpdates(a.Scene, a.Camera, a.RenderFrameIndex, false)
			if len(updates) != 1 || updates[0].CascadeIndex != uint32(index) {
				t.Fatalf("only changed cascade %d should remain pending, got %+v", index, updates)
			}
			a.BufferManager.RecordShadowUpdates(updates, a.RenderFrameIndex, a.Scene.ShadowRevision())
			a.RenderFrameIndex++
			r2gAssertPass(t, a, 0, 1)
		})
	}
}

func TestR2gFarCasterEditSchedulesOneCascadeThroughApp(t *testing.T) {
	a := r2gFixture(t)
	a.HasLastCameraState = false
	a.Scene.Objects[1].XBrickMap.Revision++
	r2gCommit(a, 1)
	r2gAssertPass(t, a, 1, 1)
	a.RenderFrameIndex += 100
	r2gAssertPass(t, a, 1, 1)
	updates := a.BufferManager.BuildShadowUpdates(a.Scene, a.Camera, a.RenderFrameIndex, false)
	if len(updates) != 1 || updates[0].Kind != core.ShadowUpdateKindDirectional || updates[0].CascadeIndex != 1 {
		t.Fatalf("far edit must remain pending only in far cascade, got %+v", updates)
	}
	a.BufferManager.RecordShadowUpdates(updates, a.RenderFrameIndex, a.Scene.ShadowRevision())
	a.RenderFrameIndex++
	r2gAssertPass(t, a, 0, 1)
}

func TestR2gExplicitManagerForceStillRefreshesBothCascades(t *testing.T) {
	a := r2gFixture(t)
	a.HasLastCameraState = false
	forced := a.BufferManager.BuildShadowUpdates(a.Scene, a.Camera, a.RenderFrameIndex, true)
	seen := map[uint32]bool{}
	for _, update := range forced {
		if update.Kind != core.ShadowUpdateKindDirectional {
			t.Error("explicit directional force woke a valid local shadow")
		}
		seen[update.CascadeIndex] = true
	}
	if len(forced) != 2 || !seen[0] || !seen[1] {
		t.Fatalf("explicit manager force must retain both cascades, got %+v", forced)
	}
	// Requesting a forced schedule cannot itself make unchanged dependencies dirty.
	r2gAssertPass(t, a, 0, 1)
}
