//go:build ignore

// CPU-only shadow membership diagnostic. It measures the real manager path,
// including live scalar capture and scheduling; it makes no GPU/FPS claim.
// Run from gekko: env GOCACHE=/tmp/gekko3d-gocache go run docs/roadmaps/diagnostics/r2j_shadow_membership_cpu.go
package main

import (
	"flag"
	"fmt"
	"runtime"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

type fixture struct {
	scene   *core.Scene
	camera  *core.CameraState
	manager gpu.GpuBufferManager
	model   *volume.XBrickMap
	objects []*core.VoxelObject
	frame   uint64
	moved   bool
}

func newFixture(count int) *fixture {
	f := &fixture{scene: core.NewScene(), camera: core.NewCameraState(), model: volume.NewXBrickMap()}
	f.camera.Position = mgl32.Vec3{0, 2, 30}
	f.camera.LookAt = mgl32.Vec3{0, 2, 0}
	f.model.SetVoxel(0, 0, 0, 1)
	f.model.ClearDirty()
	// Axis-facing objects, near/far cascade objects and remote spot members
	// produce both members and nonmembers in the selected input for every layer.
	positions := []mgl32.Vec3{{8, 0, 0}, {-8, 0, 0}, {0, 8, 0}, {0, -8, 0}, {0, 0, 8}, {0, 0, -8}, {70, 0, -100}, {600, 0, -20}}
	for i := 0; i < count; i++ {
		o := core.NewVoxelObject()
		o.XBrickMap = f.model
		o.Transform.Position = positions[i%len(positions)].Add(mgl32.Vec3{float32(i/len(positions)%4) * .125, 0, 0})
		o.Transform.Dirty = true
		o.UpdateWorldAABB()
		f.objects = append(f.objects, o)
	}
	f.scene.Objects = f.objects
	// Deliberately preserve exact selected order instead of calling Scene.Commit:
	// selection, BVH rebuild and identity sorting are outside this diagnostic.
	f.scene.ShadowObjects = append([]*core.VoxelObject(nil), f.objects...)
	f.scene.Lights = []core.Light{
		{Position: [4]float32{0, 0, 0, 1}, Params: [4]float32{40, .8, float32(core.LightTypePoint), 1}},
		{Position: [4]float32{0, 10, -20, 1}, Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{40, .8, float32(core.LightTypeSpot), 1}},
		{Position: [4]float32{600, 10, -20, 1}, Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{40, .8, float32(core.LightTypeSpot), 1}},
		{Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}},
	}
	f.manager.UpdateLights(f.scene, f.camera, 1)
	if len(f.manager.ShadowLayerParams) != 10 {
		panic("fixture must have six point faces, two spots and two directional cascades")
	}
	// Acknowledge through actual scheduling/recording until tier budgets drain.
	for i := 0; i < 64; i++ {
		updates := f.step()
		if updates == 0 {
			return f
		}
	}
	panic("fixture failed to warm")
}

func (f *fixture) step() int {
	f.frame++
	f.manager.UpdateLights(f.scene, f.camera, 1)
	updates := f.manager.BuildShadowUpdates(f.scene, f.camera, f.frame, false)
	f.manager.RecordShadowUpdates(updates, f.frame, f.scene.ShadowRevision())
	return len(updates)
}

func (f *fixture) change(workload string) {
	f.moved = !f.moved
	switch workload {
	case "idle":
	case "scalar_revision":
		// Shared map revision changes scalar snapshots without changing bounds.
		f.model.Revision++
	case "one_bounds_move":
		x := float32(8)
		if f.moved {
			x += .125
		}
		f.objects[0].Transform.Position[0] = x
		f.objects[0].Transform.Dirty = true
		if !f.objects[0].UpdateWorldAABB() {
			panic("bounds move was not published")
		}
	case "structural_reorder":
		// Stable count with changed ordered identities must take full fallback.
		f.scene.ShadowObjects[0], f.scene.ShadowObjects[1] = f.scene.ShadowObjects[1], f.scene.ShadowObjects[0]
	case "moving_origin":
		// Only point membership depends on the render-relative origin.
		if f.moved {
			f.manager.RenderOrigin = mgl32.Vec3{.125, 0, 0}
		} else {
			f.manager.RenderOrigin = mgl32.Vec3{}
		}
	default:
		panic("unknown workload")
	}
}

func main() {
	testing.Init()
	flag.Parse() // Allows -test.benchtime=1s (the default) for repeatable runs.
	runtime.GOMAXPROCS(1)
	fmt.Println("CPU-only: real UpdateLights + BuildShadowUpdates + RecordShadowUpdates; 10 shadow volumes; explicit ordered selection; no GPU/FPS claim")
	for _, count := range []int{32, 1000} {
		for _, workload := range []string{"idle", "scalar_revision", "one_bounds_move", "structural_reorder", "moving_origin"} {
			// Construct once outside the benchmark; calibration and timed iterations
			// continue the same warmed state and toggle every changed input.
			f := newFixture(count)
			result := testing.Benchmark(func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					f.change(workload)
					updates := f.step()
					if workload == "idle" && updates != 0 {
						panic("idle shadow dependencies changed")
					}
				}
			})
			fmt.Printf("selected=%d workload=%s: %s %s member_storage_bytes=%d\n", count, workload, result.String(), result.MemString(), f.manager.ShadowDependencyMemberStorageBytes())
		}
	}
}
