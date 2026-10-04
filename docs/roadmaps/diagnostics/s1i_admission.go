//go:build ignore

// Interactive GPU admission check. Build in release mode from the engine module:
// env GOCACHE=/tmp/gekko3d-gocache go build -trimpath -ldflags='-s -w' -o /tmp/gekko-s1i-admission docs/roadmaps/diagnostics/s1i_admission.go
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

func plate() (*core.VoxelObject, *volume.XBrickMap) {
	obj := core.NewVoxelObject()
	obj.Transform.Scale = mgl32.Vec3{0.1, 0.1, 0.1}
	obj.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial([4]uint8{210, 145, 65, 255}, [4]uint8{})}
	full, coarse := obj.XBrickMap, volume.NewXBrickMap()
	// Keep IDs unique while exercising valid map ID zero in the coarse pass.
	full.ID, coarse.ID = coarse.ID, full.ID
	// Explicit empty sectors exercise capacity growth without a dense solid.
	for x := 0; x < 8; x++ {
		for y := 0; y < 4; y++ {
			for z := 0; z < 2; z++ {
				full.Sectors[[3]int{x, y, z}] = volume.NewSector(x, y, z)
			}
		}
	}
	for x := 0; x < 256; x++ {
		for y := 0; y < 128; y++ {
			if x < 96 || x >= 160 || y < 40 || y >= 88 {
				full.SetVoxel(x, y, 0, 1)
			}
			// Deliberately filled coarse opening makes promotion visible.
			coarse.SetVoxel(x/2, y/2, 0, 1)
		}
	}
	full.StructureDirty = true
	require(obj.SetRenderLOD2(coarse), "coarse representation rejected")
	return obj, coarse
}

func cube(color [4]uint8) *core.VoxelObject {
	obj := core.NewVoxelObject()
	obj.Transform.Position = mgl32.Vec3{29, 4, 0}
	obj.Transform.Scale = mgl32.Vec3{0.3, 0.3, 0.3}
	obj.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial(color, [4]uint8{})}
	for x := 0; x < 16; x++ {
		for y := 0; y < 16; y++ {
			for z := 0; z < 16; z++ {
				obj.XBrickMap.SetVoxel(x, y, z, 1)
			}
		}
	}
	return obj
}

func main() {
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	window, err := glfw.CreateWindow(1280, 800, "S1i GPU admission", nil, nil)
	must(err)
	defer window.Destroy()
	a := app.NewApp(window)
	a.FeatureConfig = app.AppFeatureConfig{AutoRegisterDefaults: false}
	must(a.Init())
	defer a.Shutdown()
	window.SetFramebufferSizeCallback(func(_ *glfw.Window, w, h int) {
		if w > 0 && h > 0 {
			a.Resize(w, h)
		}
	})
	a.OcclusionMode, a.RenderMode = core.OcclusionOff, 1
	a.Camera.Position, a.Camera.LookAt = mgl32.Vec3{16, 6.4, 38}, mgl32.Vec3{16, 6.4, 0}
	a.Camera.Far = 100
	obj, coarse := plate()
	full, revision := obj.XBrickMap, obj.XBrickMap.Revision
	a.Scene.AddObject(obj)
	a.BufferManager.SetVoxelUploadBudget(gpu.VoxelUploadBudget{MaxBytes: 4 << 20, MaxSectors: 1, MaxBricks: 64})
	phase, advance, lastLog := 0, false, time.Time{}
	var side *core.VoxelObject
	var deniedAlias *core.VoxelObject
	var reuseBytes uint64
	var coarseBytes uint64
	var beforeRequiredBytes uint64
	var peakRetired uint64
	labels := []string{
		"Loading coarse plate",
		"Deferred fine detail: solid plate stays visible; right side stays empty. SPACE permits growth",
		"Uploading fine detail: solid plate must stay visible",
		"Fine ready, coarse still visible. SPACE promotes full detail",
		"Full plate has rectangular opening. SPACE loads required red cube above cap",
		"Required red cube stays visible under pressure. SPACE replaces it with optional green cube",
		"Optional green cube reuses slots under pressure. Wait for retirement; ESC closes",
	}
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
		a.Update()
		a.Render()
		s := a.BufferManager.VoxelGPUAdmissionStats()
		peakRetired = max(peakRetired, s.RetiredBufferBytes)
		coarseReady, _, _ := a.BufferManager.RenderVoxelObjectReady(obj, coarse, coarse.Revision)
		fineReady, _, _ := a.BufferManager.PendingFullVoxelObjectReady(obj, full, revision)
		if phase >= 1 && phase <= 3 {
			require(obj.RenderVoxelMap() == coarse && coarseReady, "coarse fallback lost during fine admission")
		}
		require(full.Revision == revision, "GPU admission changed CPU geometry")
		switch phase {
		case 0:
			if coarseReady {
				coarseBytes = s.CurrentBufferBytes
				a.BufferManager.SetVoxelGPUAdmissionBudget(gpu.VoxelGPUAdmissionBudget{MaxBytes: s.TotalBytes})
				require(obj.SetPendingFullUpload(), "pending full request rejected")
				// Visible direct callers can defer materials on already shared
				// geometry. Their GPU row must reject lookup until admission.
				deniedAlias = core.NewVoxelObject()
				deniedAlias.XBrickMap = coarse
				deniedAlias.Transform.Position = mgl32.Vec3{29, 0, 0}
				deniedAlias.Transform.Scale = mgl32.Vec3{0.2, 0.2, 0.2}
				deniedAlias.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial([4]uint8{60, 110, 230, 255}, [4]uint8{})}
				deniedAlias.VoxelGPUAdmissionOptional = true
				a.Scene.AddObject(deniedAlias)
				phase = 1
			}
		case 1:
			if s.DeferredMaps > 0 {
				require(a.BufferManager.Allocations[full] == nil && full.StructureDirty, "deferred fine geometry acquired slots")
				require(a.BufferManager.MaterialAllocations[deniedAlias] == nil, "deferred alias acquired material slots")
				if advance {
					a.Scene.RemoveObject(deniedAlias)
					a.BufferManager.SetVoxelGPUAdmissionBudget(gpu.VoxelGPUAdmissionBudget{})
					peakRetired = 0
					phase = 2
				}
			}
		case 2:
			if fineReady {
				require(s.CurrentBufferBytes > coarseBytes && peakRetired > 0, "fine fixture failed to exercise buffer replacement")
				phase = 3
			}
		case 3:
			if advance {
				obj.ClearRenderRepresentation()
				phase = 4
			}
		case 4:
			if advance {
				beforeRequiredBytes = s.CurrentBufferBytes
				a.BufferManager.SetVoxelGPUAdmissionBudget(gpu.VoxelGPUAdmissionBudget{MaxBytes: 1})
				side = cube([4]uint8{230, 55, 45, 255})
				a.Scene.AddObject(side)
				phase = 5
			}
		case 5:
			ready, _, _ := a.BufferManager.VoxelObjectReady(side, side.XBrickMap, side.XBrickMap.Revision)
			if ready {
				require(s.PressureBytes > 0 && s.CurrentBufferBytes > beforeRequiredBytes, "required growth did not expose pinned pressure")
				if advance {
					reuseBytes = s.CurrentBufferBytes
					a.Scene.RemoveObject(side)
					side = cube([4]uint8{65, 220, 100, 255})
					side.VoxelGPUAdmissionOptional, side.RenderEnabled = true, false
					a.Scene.AddObject(side)
					phase = 6
				}
			}
		case 6:
			ready, _, _ := a.BufferManager.VoxelObjectReady(side, side.XBrickMap, side.XBrickMap.Revision)
			if ready {
				require(s.CurrentBufferBytes == reuseBytes, "optional reuse grew physical buffers under pressure")
				side.RenderEnabled = true
			}
		}
		advance = false
		window.SetTitle("S1i: " + labels[phase])
		if time.Since(lastLog) >= time.Second {
			fmt.Printf("phase=%d current=%d retired=%d atlas=%d total=%d cap=%d pressure=%d deferred=%d hard=%d failures=%d error=%q\n", phase, s.CurrentBufferBytes, s.RetiredBufferBytes, s.AtlasBytes, s.TotalBytes, s.MaxBytes, s.PressureBytes, s.DeferredMaps, s.HardLimitDeferredMaps, s.AllocationFailures, s.LastError)
			if phase == 6 && side.RenderEnabled && peakRetired > 0 && s.RetiredBufferBytes == 0 {
				fmt.Println("PASS: fallback, growth, pinned pressure, optional reuse and native retirement. Confirm visuals before reporting.")
			}
			lastLog = time.Now()
		}
	}
}
