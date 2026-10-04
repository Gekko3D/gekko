//go:build ignore

// User-run release check for staged voxel-buffer migration. No image capture.
// env GOCACHE=/tmp/gekko3d-gocache go build -trimpath -ldflags='-s -w' -o /tmp/gekko-s1k-migration docs/roadmaps/diagnostics/s1k_migration.go
package main

import (
	"fmt"
	"runtime"
	"time"

	"github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
)

func require(ok bool, message string) {
	if !ok {
		panic(message)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func panel(color [4]uint8, arriving bool) *core.VoxelObject {
	obj := core.NewVoxelObject()
	obj.Transform.Scale = mgl32.Vec3{0.2, 0.2, 0.2}
	obj.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial(color, [4]uint8{})}
	if arriving {
		obj.Transform.Position = mgl32.Vec3{16, 0, 0}
		obj.RenderEnabled, obj.VoxelGPUAdmissionOptional = false, true
		// Empty sectors exercise pool growth without a large visible scene.
		for x := 0; x < 8; x++ {
			for y := 0; y < 4; y++ {
				for z := 0; z < 2; z++ {
					key := [3]int{x, y, z}
					obj.XBrickMap.Sectors[key] = volume.NewSector(x, y, z)
				}
			}
		}
		obj.XBrickMap.StructureDirty = true
	}
	for x := 0; x < 64; x++ {
		for y := 0; y < 32; y++ {
			obj.XBrickMap.SetVoxel(x, y, 0, 1)
		}
	}
	return obj
}

func main() {
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	window, err := glfw.CreateWindow(1280, 800, "S1k migration", nil, nil)
	must(err)
	defer window.Destroy()
	a := app.NewApp(window)
	a.FeatureConfig = app.AppFeatureConfig{AutoRegisterDefaults: false}
	// Resolve samples volumetric transmittance even when the scene has no media.
	// This feature clears that input to the neutral alpha-one value each frame.
	a.RegisterFeature(&app.AnalyticMediumFeature{})
	must(a.Init())
	defer a.Shutdown()
	window.SetFramebufferSizeCallback(func(_ *glfw.Window, w, h int) {
		if w > 0 && h > 0 {
			a.Resize(w, h)
		}
	})
	a.OcclusionMode, a.RenderMode = core.OcclusionOff, 1
	a.Camera.Position, a.Camera.LookAt = mgl32.Vec3{14, 3.2, 38}, mgl32.Vec3{14, 3.2, 0}
	a.Camera.Far = 100
	left := panel([4]uint8{230, 150, 50, 255}, false)
	a.Scene.AddObject(left)
	a.BufferManager.SetVoxelUploadBudget(gpu.VoxelUploadBudget{MaxBytes: 4 << 20, MaxSectors: 2, MaxBricks: 128})
	phase, advance := 0, false
	reported := false
	animationStart, lastAnimation := time.Now(), -1
	var side *core.VoxelObject
	var cancelled *volume.XBrickMap
	var baselineBytes, peakRetired, copyUpdates uint64
	lastLog := time.Time{}
	labels := []string{
		"Loading left panel",
		"Left panel animates. SPACE starts paused migration",
		"Migration paused: left animates; right empty. SPACE cancels blue request and resumes with green",
		"Copying across frames: left must keep animating; right stays empty until green is ready",
		"Green panel ready; left still animates. Wait for PASS, resize, then ESC",
	}
	fmt.Println("S1k: the orange/purple left panel must be visible before pressing SPACE. An empty window is a failed visual check.")
	window.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if action != glfw.Press {
			return
		}
		if key == glfw.KeyEscape {
			window.SetShouldClose(true)
		}
		if key == glfw.KeySpace {
			advance = true
		}
	})
	for !window.ShouldClose() {
		glfw.PollEvents()
		animation := int(time.Since(animationStart) / (500 * time.Millisecond))
		if phase > 0 && animation != lastAnimation {
			lastAnimation = animation
			open := animation%2 == 0
			color, value := [4]uint8{230, 150, 50, 255}, uint8(1)
			if open {
				color, value = [4]uint8{180, 70, 220, 255}, 0
			}
			left.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial(color, [4]uint8{})}
			for x := 24; x < 32; x++ {
				for y := 8; y < 16; y++ {
					left.XBrickMap.SetVoxel(x, y, 0, value)
				}
			}
		}
		a.Update()
		a.Render()
		s, work := a.BufferManager.VoxelGPUAdmissionStats(), a.BufferManager.VoxelGPUWorkStats()
		peakRetired = max(peakRetired, s.RetiredBufferBytes)
		if phase >= 2 {
			require(work.Creates <= 2 && work.CopiedBytes <= 8<<10, "frame work exceeded budget")
			if work.OversizedCreates != 0 {
				require(work.OversizedCreates == 1 && work.Creates == 1, "oversized creation was not sole creation")
			}
			require(s.AllocationFailures == 0, "native growth failed")
			if work.CopiedBytes > 0 {
				copyUpdates++
			}
		}
		previousPhase := phase
		switch phase {
		case 0:
			if ready, _, _ := a.BufferManager.VoxelObjectReady(left, left.XBrickMap, left.XBrickMap.Revision); ready {
				baselineBytes, phase = s.CurrentBufferBytes, 1
			}
		case 1:
			if advance {
				a.BufferManager.SetVoxelGPUWorkBudget(gpu.VoxelGPUWorkBudget{Enabled: true, MaxCreateBytes: 4 << 10, MaxCreates: 2})
				side = panel([4]uint8{70, 110, 235, 255}, true)
				a.Scene.AddObject(side)
				phase = 2
			}
		case 2:
			require(work.CopiedBytes == 0, "paused copy budget queued a copy")
			if advance && work.Pending && s.StagingBytes > 0 && work.Creates == 0 {
				cancelled = side.XBrickMap
				a.Scene.RemoveObject(side)
				side = panel([4]uint8{70, 220, 105, 255}, true)
				a.Scene.AddObject(side)
				a.BufferManager.SetVoxelGPUWorkBudget(gpu.VoxelGPUWorkBudget{Enabled: true, MaxCreateBytes: 4 << 10, MaxCreates: 2, MaxCopyBytes: 8 << 10})
				phase = 3
			}
		case 3:
			require(a.BufferManager.Allocations[cancelled] == nil, "cancelled demand acquired geometry")
			if ready, _, _ := a.BufferManager.VoxelObjectReady(side, side.XBrickMap, side.XBrickMap.Revision); ready && !work.Pending {
				require(s.CurrentBufferBytes > baselineBytes && copyUpdates > 1 && peakRetired > 0, "fixture did not exercise staged replacement")
				side.RenderEnabled, phase = true, 4
			}
		}
		if previousPhase != 2 || phase != 2 {
			advance = false
		}
		window.SetTitle("S1k: " + labels[phase])
		if time.Since(lastLog) >= time.Second {
			leftReady, _, _ := a.BufferManager.VoxelObjectReady(left, left.XBrickMap, left.XBrickMap.Revision)
			rightReady := false
			if side != nil {
				rightReady, _, _ = a.BufferManager.VoxelObjectReady(side, side.XBrickMap, side.XBrickMap.Revision)
			}
			fmt.Printf("phase=%d frames=%d visible=%d bvh=%d leftReady=%t rightReady=%t bindingsCurrent=%t pending=%t creates=%d oversized=%d created=%d copied=%d current=%d staging=%d retired=%d failures=%d error=%q\n", phase, a.RenderFrameIndex, len(a.Scene.VisibleObjects), len(a.Scene.BVHNodesBytes), leftReady, rightReady, a.BufferManager.GBufferSceneBindGroupCurrent(), work.Pending, work.Creates, work.OversizedCreates, work.CreatedBytes, work.CopiedBytes, s.CurrentBufferBytes, s.StagingBytes, s.RetiredBufferBytes, s.AllocationFailures, s.LastError)
			if phase == 4 && !reported && s.StagingBytes == 0 && s.RetiredBufferBytes == 0 {
				fmt.Println("PASS: bounded migration, changed demand and native retirement. Confirm continuous animation and resize before reporting.")
				reported = true
			}
			lastLog = time.Now()
		}
	}
}
