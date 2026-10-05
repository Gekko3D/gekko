//go:build ignore

// Native CPU shadow recording diagnostic. Run from engine cwd.
// Measures DispatchShadowPass only; recorded GPU passes are discarded.
// Seven valid maps across four resolutions; 30 warmups and 300 samples.
package main

import (
	"fmt"
	"runtime"
	"sort"
	"time"

	"github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func require(ok bool, msg string) {
	if !ok {
		panic(msg)
	}
}
func cube(x float32) *core.VoxelObject {
	o := core.NewVoxelObject()
	o.Transform.Position = mgl32.Vec3{x, 0, 0}
	o.Transform.Scale = mgl32.Vec3{.25, .25, .25}
	o.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial([4]uint8{180, 120, 60, 255}, [4]uint8{})}
	volume.Cube(o.XBrickMap, mgl32.Vec3{}, mgl32.Vec3{7, 7, 7}, 1)
	return o
}
func main() {
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	win, e := glfw.CreateWindow(640, 480, "R2d mixed-resolution shadow batches", nil, nil)
	must(e)
	defer win.Destroy()
	a := app.NewApp(win)
	a.FeatureConfig = app.AppFeatureConfig{AutoRegisterDefaults: false}
	must(a.Init())
	defer a.Shutdown()
	a.Camera.Position = mgl32.Vec3{0, 4, 45}
	a.Camera.LookAt = mgl32.Vec3{0, 4, 0}
	a.Camera.Far = 200
	a.OcclusionMode = core.OcclusionOff
	a.Scene.AddObject(cube(0))
	a.Scene.Lights = []core.Light{{Direction: [4]float32{0, -1, 0, 0}, Color: [4]float32{1, 1, 1, 1}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}}}
	// Distances populate two hero spots, one near spot, and two lower-tier spots.
	for _, x := range []float32{0, 12, 40, 85, 160} {
		o := cube(x)
		o.Transform.Position[2] = 45
		o.Transform.Scale = mgl32.Vec3{.5, .5, .5}
		o.Transform.Dirty = true
		a.Scene.AddObject(o)
		a.Scene.Lights = append(a.Scene.Lights, core.Light{Position: [4]float32{x, 10, 45, 0}, Direction: [4]float32{0, -1, 0, 0}, Color: [4]float32{1, 1, 1, 1}, Params: [4]float32{20, .8, float32(core.LightTypeSpot), 1}})
	}
	glfw.PollEvents()
	a.Update()
	m := a.BufferManager
	var updates []core.ShadowUpdate
	counts := map[uint32]int{}
	for _, p := range m.ShadowLayerParams {
		require(p.LightIndex < uint32(len(a.Scene.Lights)), "fixture invalid light index")
		l := a.Scene.Lights[p.LightIndex]
		require(p.Layer >= l.ShadowMeta[0] && p.Layer < l.ShadowMeta[0]+l.ShadowMeta[1], "fixture metadata does not own scheduled layer")
		u := core.ShadowUpdate{LightIndex: p.LightIndex, ShadowLayer: p.Layer, CascadeIndex: p.CascadeIndex, Kind: p.Kind, Tier: p.Tier, Resolution: p.EffectiveResolution}
		updates = append(updates, u)
		counts[u.Resolution]++
	}
	require(counts[1024] == 2 && counts[512] == 2 && counts[256] == 1 && counts[128] == 2, fmt.Sprintf("fixture bucket counts mismatch: %v", counts))
	require(m.ShadowMapLayers > uint32(len(updates)), "fixture needs untouched atlas layer")
	runtime.GOMAXPROCS(1)
	var samples []float64
	for i := 0; i < 330; i++ {
		enc, e := a.Device.CreateCommandEncoder(nil)
		must(e)
		start := time.Now()
		m.DispatchShadowPass(enc, updates)
		elapsed := float64(time.Since(start).Nanoseconds()) / 1000
		enc.Release()
		a.Queue.Submit()
		a.Device.Poll(true, nil)
		if i >= 30 {
			samples = append(samples, elapsed)
		}
	}
	sort.Float64s(samples)
	mean := 0.0
	for _, sample := range samples {
		mean += sample
	}
	fmt.Printf("CPU shadow recording only: n=%d mean=%.2fus median=%.2fus p95=%.2fus; encoded GPU passes discarded\n", len(samples), mean/float64(len(samples)), samples[len(samples)/2], samples[len(samples)*95/100])
}
