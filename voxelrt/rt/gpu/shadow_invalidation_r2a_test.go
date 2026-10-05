package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

const r2aWarmFrame = uint64(32)

func r2aObject(id uint32, x float32) *core.VoxelObject {
	obj := scheduleObject(id)
	obj.MaterialTable = append(obj.MaterialTable, core.DefaultMaterial())
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	obj.XBrickMap.ClearDirty()
	obj.Transform.Position = mgl32.Vec3{x, 0, 0}
	return obj
}

func r2aLight(kind uint32, x float32) core.Light {
	return core.Light{Position: [4]float32{x, 10, 0, 1}, Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{20, .8, float32(kind), 1}}
}

func r2aCommit(m *GpuBufferManager, scene *core.Scene, camera *core.CameraState) {
	scene.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{CameraPosition: camera.Position})
	m.UpdateLights(scene, camera, 1)
}

func r2aWarm(t *testing.T, m *GpuBufferManager, scene *core.Scene, camera *core.CameraState) {
	t.Helper()
	r2aCommit(m, scene, camera)
	// Warm through real scheduling/recording. Re-record the scheduled union at
	// one frame so every tier is tested strictly before its next cadence.
	seen := make(map[uint32]core.ShadowUpdate)
	for frame := uint64(0); frame < 12; frame++ {
		updates := m.BuildShadowUpdates(scene, camera, frame, false)
		for _, update := range updates {
			seen[update.ShadowLayer] = update
		}
		m.RecordShadowUpdates(updates, frame, scene.ShadowRevision())
	}
	all := make([]core.ShadowUpdate, 0, len(seen))
	for _, update := range seen {
		all = append(all, update)
	}
	m.RecordShadowUpdates(all, r2aWarmFrame, scene.ShadowRevision())
	r2aAssert(t, m, scene, camera, r2aWarmFrame+1, nil)
}

func r2aFixture(t *testing.T, kind uint32) (*GpuBufferManager, *core.Scene, *core.CameraState) {
	t.Helper()
	objects := []*core.VoxelObject{r2aObject(1, 40), r2aObject(2, 100)}
	m, _ := scheduleFixture(t, objects...)
	scene := core.NewScene()
	scene.Objects = objects
	scene.Lights = []core.Light{r2aLight(kind, 40), r2aLight(kind, 100)}
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{}
	r2aWarm(t, m, scene, camera)
	return m, scene, camera
}

// Check serialization before scheduling: a scheduler-side fix alone must not
// leave stale shadows enabled in the same frame's GPU light data.
func r2aAssert(t *testing.T, m *GpuBufferManager, scene *core.Scene, camera *core.CameraState, frame uint64, invalid []int) []core.ShadowUpdate {
	t.Helper()
	want := make(map[int]bool)
	for _, index := range invalid {
		want[index] = true
	}
	data := m.buildLightsDataForGPU(scene.Lights, scene.ShadowRevision())
	for index, light := range scene.Lights {
		if uint32(light.Params[2]) == core.LightTypeDirectional {
			continue
		}
		ready := lightParamsWFromData(data, index) == 1 && lightShadowLayerCountFromData(data, index) == light.ShadowMeta[1]
		if ready == want[index] {
			t.Errorf("light %d GPU readiness=%v, want invalidated=%v", index, ready, want[index])
		}
	}
	updates := m.BuildShadowUpdates(scene, camera, frame, false)
	got := make(map[int]bool)
	for _, update := range updates {
		if update.Kind != core.ShadowUpdateKindDirectional {
			got[int(update.LightIndex)] = true
		}
	}
	for index, light := range scene.Lights {
		if uint32(light.Params[2]) != core.LightTypeDirectional && got[index] != want[index] {
			t.Errorf("light %d scheduled=%v, want %v before cadence", index, got[index], want[index])
		}
	}
	return updates
}

func TestR2aCasterDependencies(t *testing.T) {
	for _, kind := range []uint32{core.LightTypeSpot, core.LightTypePoint} {
		name := map[uint32]string{core.LightTypeSpot: "spot", core.LightTypePoint: "point"}[kind]
		for _, change := range []string{"insert", "move inside", "remove", "move across", "geometry revision", "selected map", "transform", "object metadata", "disable rendering", "disable casting", "geometry allocation", "material allocation", "source radius", "emitter link"} {
			t.Run(name+"/"+change, func(t *testing.T) {
				m, scene, camera := r2aFixture(t, kind)
				obj := scene.Objects[0]
				invalid := []int{0}
				switch change {
				case "insert":
					scene.Objects = append(scene.Objects, r2aObject(3, 41))
				case "move inside":
					obj.Transform.Position[0]++
					obj.Transform.Dirty = true
				case "remove":
					scene.Objects = scene.Objects[1:]
				case "move across":
					obj.Transform.Position[0] = 101
					obj.Transform.Dirty = true
					invalid = []int{0, 1}
				case "geometry revision":
					obj.XBrickMap.Revision++ // unchanged occupied bounds
				case "selected map":
					copyMap := c3h9Map(3, [3]int{})
					copyMap.ClearDirty()
					obj.XBrickMap = copyMap
				case "transform":
					// Rotate a unit cube around its centre: same world bounds,
					// different matrices consumed by the shadow renderer.
					obj.Transform.Rotation = mgl32.QuatRotate(3.14159265/2, mgl32.Vec3{0, 1, 0})
					obj.Transform.Position[2] = 1
					obj.Transform.Dirty = true
				case "object metadata":
					obj.ShadowGroupID++ // public scalar, no dirty notification
				case "disable rendering":
					obj.RenderEnabled = false
				case "disable casting":
					obj.CastsShadows = false
				case "geometry allocation":
					copyAllocation := *m.Allocations[obj.XBrickMap]
					m.Allocations[obj.XBrickMap] = &copyAllocation
				case "material allocation":
					copyAllocation := *m.MaterialAllocations[obj]
					m.MaterialAllocations[obj] = &copyAllocation
				case "source radius":
					scene.Lights[0].Position[3]++
				case "emitter link":
					scene.Lights[0].ShadowMeta[3]++
				}
				r2aCommit(m, scene, camera)
				r2aAssert(t, m, scene, camera, r2aWarmFrame+1, invalid)
			})
		}
	}
}

func TestR2aCasterGroupSelectionChangesDependency(t *testing.T) {
	m, scene, camera := r2aFixture(t, core.LightTypeSpot)
	original := scene.Objects[0]
	replacement := r2aObject(3, 40)
	replacement.Transform.Position[2] = 2
	for _, obj := range []*core.VoxelObject{original, replacement} {
		obj.ShadowCasterGroupID = 7
		obj.ShadowCasterGroupLimit = 1
	}
	scene.Objects = append(scene.Objects, replacement)
	r2aWarm(t, m, scene, camera)
	contains := func(target *core.VoxelObject) bool {
		for _, obj := range scene.ShadowObjects {
			if obj == target {
				return true
			}
		}
		return false
	}
	if !contains(original) || contains(replacement) {
		t.Fatal("fixture failed to select original grouped caster")
	}
	camera.Position[2] = 4 // Both light tiers stay unchanged.
	r2aCommit(m, scene, camera)
	if contains(original) || !contains(replacement) {
		t.Fatal("camera movement failed to replace grouped caster selection")
	}
	r2aAssert(t, m, scene, camera, r2aWarmFrame+1, []int{0})
}

func TestR2aUnrelatedUploadPreservesCachedShadow(t *testing.T) {
	m, scene, camera := r2aFixture(t, core.LightTypeSpot)
	scene.Objects[0].XBrickMap.DirtyBricks[[6]int{}] = true
	scheduleRun(t, m, scene)
	r2aCommit(m, scene, camera)
	updates := r2aAssert(t, m, scene, camera, r2aWarmFrame+1, []int{0})
	m.RecordShadowUpdates(updates, r2aWarmFrame+1, scene.ShadowRevision())
	cadence := m.ShadowLayerParams[scene.Lights[1].ShadowMeta[0]].CadenceFrames
	if cadence < 2 {
		t.Fatal("fixture must give unaffected light a delayed cadence")
	}
	for _, frame := range []uint64{r2aWarmFrame + uint64(cadence) - 1, r2aWarmFrame + uint64(cadence), r2aWarmFrame + uint64(cadence)*1000} {
		found := false
		for _, update := range m.BuildShadowUpdates(scene, camera, frame, false) {
			found = found || update.LightIndex == 1
		}
		if found {
			t.Errorf("unaffected cached light scheduled at frame %d", frame)
		}
	}
}

func TestR2aNoncastersAndVolumeMembership(t *testing.T) {
	for _, change := range []string{"noncaster", "outside cone", "edge touching", "removed dependency"} {
		t.Run(change, func(t *testing.T) {
			m, scene, camera := r2aFixture(t, core.LightTypeSpot)
			obj := r2aObject(3, 40)
			invalid := []int(nil)
			switch change {
			case "noncaster":
				obj.CastsShadows = false
			case "outside cone":
				obj.Transform.Position[1] = 15 // behind the spot apex
			case "edge touching":
				// Apex-touching bounds must remain a conservative dependency.
				obj.Transform.Position[1] = 10
				invalid = []int{0}
			case "removed dependency":
				removed := scene.Objects[0]
				scene.Objects = scene.Objects[1:]
				r2aWarm(t, m, scene, camera)
				removed.XBrickMap.Revision++
				r2aCommit(m, scene, camera)
				r2aAssert(t, m, scene, camera, r2aWarmFrame+1, nil)
				return
			}
			scene.Objects = append(scene.Objects, obj)
			r2aCommit(m, scene, camera)
			r2aAssert(t, m, scene, camera, r2aWarmFrame+1, invalid)
		})
	}
}

func TestR2aSuccessfulUploadDependencies(t *testing.T) {
	for _, change := range []string{"geometry", "material", "shared map", "failed", "skipped", "progressive", "external", "external then tracked", "written then stale"} {
		t.Run(change, func(t *testing.T) {
			m, scene, camera := r2aFixture(t, core.LightTypeSpot)
			obj := scene.Objects[0]
			invalid := []int{0}
			if change == "shared map" || change == "written then stale" {
				scene.Objects[1].XBrickMap = obj.XBrickMap
				r2aWarm(t, m, scene, camera)
				invalid = []int{0, 1}
			}
			if change == "external" || change == "external then tracked" {
				m.VoxelUploadRevision++
				invalid = []int{0, 1}
			}
			if change == "material" {
				obj.MaterialTable = append(obj.MaterialTable, core.DefaultMaterial())
			} else if change != "external" {
				obj.XBrickMap.DirtyBricks[[6]int{}] = true
			}
			if change == "skipped" {
				m.SetVoxelUploadBudget(VoxelUploadBudget{})
			}
			before := m.VoxelUploadRevision
			calls := 0
			m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
				calls++
				if change == "written then stale" {
					// Bytes written to a shared map remain visible to the peer
					// placement even if this executor's owner detaches afterward.
					obj.XBrickMap = c3h9Map(9, [3]int{})
				}
				return change != "failed"
			})
			if change == "failed" || change == "skipped" || change == "external" {
				if m.VoxelUploadRevision != before {
					t.Fatal("failed/skipped execution advanced public upload revision")
				}
				if change != "external" {
					invalid = nil
				}
			} else if calls == 0 || m.VoxelUploadRevision == before || m.VoxelUploadBytes == 0 {
				t.Fatal("fixture did not execute and publish a successful upload")
			}
			r2aCommit(m, scene, camera)
			updates := r2aAssert(t, m, scene, camera, r2aWarmFrame+1, invalid)
			if change == "progressive" {
				m.RecordShadowUpdates(updates, r2aWarmFrame+1, scene.ShadowRevision())
				obj.XBrickMap.DirtyBricks[[6]int{}] = true // same CPU geometry revision
				scheduleRun(t, m, scene)
				r2aCommit(m, scene, camera)
				r2aAssert(t, m, scene, camera, r2aWarmFrame+1, []int{0})
			}
		})
	}
}

func TestR2aPendingFullUploadDoesNotInvalidateDisplay(t *testing.T) {
	m, _, obj, coarse := c3h10PendingFixture(t)
	obj.Transform.Position = mgl32.Vec3{40, 0, 0}
	scene := core.NewScene()
	scene.Objects = []*core.VoxelObject{obj}
	scene.Lights = []core.Light{r2aLight(core.LightTypeSpot, 40)}
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{}
	m.prepareVoxelStructureDirtyState(scene)
	r2aWarm(t, m, scene, camera)
	work := scheduleRun(t, m, scene)
	if len(work) == 0 || obj.RenderVoxelMap() != coarse {
		t.Fatal("fixture failed to upload pending full while retaining selected display")
	}
	for _, w := range work {
		if w.targetMap() != obj.XBrickMap {
			t.Fatal("fixture uploaded selected display instead of pending full")
		}
	}
	r2aCommit(m, scene, camera)
	r2aAssert(t, m, scene, camera, r2aWarmFrame+1, nil)
}

func TestR2aPointFacesTrackWholeDependencyGeneration(t *testing.T) {
	m, scene, camera := r2aFixture(t, core.LightTypePoint)
	obj := scene.Objects[0]
	obj.ShadowGroupID++
	r2aCommit(m, scene, camera)
	updates := r2aAssert(t, m, scene, camera, r2aWarmFrame+1, []int{0})
	m.RecordShadowUpdates(updates, r2aWarmFrame+1, scene.ShadowRevision())
	seen := make(map[uint32]bool)
	for _, update := range updates {
		if update.LightIndex == 0 {
			seen[update.CascadeIndex] = true
		}
	}
	// An unrelated write must preserve acknowledgements for refreshed faces.
	scene.Objects[1].XBrickMap.DirtyBricks[[6]int{}] = true
	scheduleRun(t, m, scene)
	r2aCommit(m, scene, camera)
	for frame := r2aWarmFrame + 2; len(seen) < core.PointShadowFaceCount && frame < r2aWarmFrame+12; frame++ {
		data := m.buildLightsDataForGPU(scene.Lights, scene.ShadowRevision())
		if lightParamsWFromData(data, 0) != 0 {
			t.Fatal("partially refreshed point light enabled before all six current faces recorded")
		}
		updates = m.BuildShadowUpdates(scene, camera, frame, false)
		m.RecordShadowUpdates(updates, frame, scene.ShadowRevision())
		for _, update := range updates {
			if update.LightIndex == 0 {
				seen[update.CascadeIndex] = true
			}
		}
	}
	data := m.buildLightsDataForGPU(scene.Lights, scene.ShadowRevision())
	if len(seen) != core.PointShadowFaceCount || lightParamsWFromData(data, 0) != 1 {
		t.Fatal("point light failed to finish current faces across unrelated upload")
	}
	// A second edit must discard acknowledgements from the previous generation.
	obj.ShadowGroupID++
	r2aCommit(m, scene, camera)
	updates = m.BuildShadowUpdates(scene, camera, 48, false)
	m.RecordShadowUpdates(updates, 48, scene.ShadowRevision())
	obj.EmitterLinkID++
	r2aCommit(m, scene, camera)
	updates = m.BuildShadowUpdates(scene, camera, 49, false)
	m.RecordShadowUpdates(updates, 49, scene.ShadowRevision())
	if lightParamsWFromData(m.buildLightsDataForGPU(scene.Lights, scene.ShadowRevision()), 0) != 0 {
		t.Fatal("second edit reused earlier-generation point face acknowledgements")
	}
}

func TestR2aLightChangesAndCacheReset(t *testing.T) {
	for _, change := range []string{"movement", "range", "tier", "reorder", "reset", "remove and readd"} {
		t.Run(change, func(t *testing.T) {
			m, scene, camera := r2aFixture(t, core.LightTypeSpot)
			invalid := []int{0}
			switch change {
			case "movement":
				scene.Lights[0].Position[0]++
			case "range":
				scene.Lights[0].Params[0]++
			case "tier":
				camera.Position[0] = -30
			case "reorder":
				scene.Lights[0], scene.Lights[1] = scene.Lights[1], scene.Lights[0]
				invalid = []int{0, 1} // atlas layers now represent different lights
			case "reset":
				m.invalidateShadowCache() // same reset used on atlas recreation
				invalid = []int{0, 1}
			case "remove and readd":
				lights := scene.Lights
				scene.Lights = nil
				r2aCommit(m, scene, camera)
				scene.Lights = lights
				invalid = []int{0, 1}
			}
			r2aCommit(m, scene, camera)
			r2aAssert(t, m, scene, camera, r2aWarmFrame+1, invalid)
		})
	}
}
