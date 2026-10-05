//go:build ignore

// Native local shadow reuse regression. Run from engine cwd.
// Reuses the R2d seven-map fixture and readback helpers.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"runtime"

	"github.com/cogentcore/webgpu/wgpu"
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

func mapped(a *app.App, b *wgpu.Buffer, size uint64) []byte {
	done := false
	b.MapAsync(wgpu.MapModeRead, 0, size, func(status wgpu.BufferMapAsyncStatus) {
		require(status == wgpu.BufferMapAsyncStatusSuccess, "native readback mapping failed")
		done = true
	})
	a.Device.Poll(true, nil)
	require(done, "native readback mapping did not complete")
	data := append([]byte(nil), b.GetMappedRange(0, uint(size))...)
	b.Unmap()
	return data
}

func submit(a *app.App, enc *wgpu.CommandEncoder) {
	cmd, e := enc.Finish(nil)
	must(e)
	a.Queue.Submit(cmd)
	cmd.Release()
	a.Device.Poll(true, nil)
}

func readMap(a *app.App, layer, resolution uint32) []byte {
	row := resolution * 16
	size := uint64(row * resolution)
	b, e := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: "R2e map readback", Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	must(e)
	defer b.Release()
	enc, e := a.Device.CreateCommandEncoder(nil)
	must(e)
	defer enc.Release()
	enc.CopyTextureToBuffer(&wgpu.ImageCopyTexture{Texture: a.BufferManager.ShadowMapArray, Origin: wgpu.Origin3D{Z: layer}}, &wgpu.ImageCopyBuffer{Buffer: b, Layout: wgpu.TextureDataLayout{BytesPerRow: row, RowsPerImage: resolution}}, &wgpu.Extent3D{Width: resolution, Height: resolution, DepthOrArrayLayers: 1})
	submit(a, enc)
	return mapped(a, b, size)
}

func installReadbackMap(a *app.App) {
	m := a.BufferManager
	tex, e := a.Device.CreateTexture(&wgpu.TextureDescriptor{Label: "R2e diagnostic shadow map", Size: wgpu.Extent3D{Width: 1024, Height: 1024, DepthOrArrayLayers: m.ShadowMapLayers}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA32Float, Usage: wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopySrc | wgpu.TextureUsageCopyDst})
	must(e)
	view, e := tex.CreateView(&wgpu.TextureViewDescriptor{Format: wgpu.TextureFormatRGBA32Float, Dimension: wgpu.TextureViewDimension2DArray, ArrayLayerCount: m.ShadowMapLayers, MipLevelCount: 1})
	must(e)
	m.ShadowMapView.Release()
	m.ShadowMapArray.Release()
	m.ShadowMapArray, m.ShadowMapView = tex, view
	m.CreateShadowBindGroups()
}

func requireHit(active []byte, u core.ShadowUpdate, a *app.App) {
	light := a.Scene.Lights[u.LightIndex]
	for offset := 0; offset < len(active); offset += 16 {
		depth := math.Float32frombits(binary.LittleEndian.Uint32(active[offset:]))
		g := math.Float32frombits(binary.LittleEndian.Uint32(active[offset+4:]))
		b := math.Float32frombits(binary.LittleEndian.Uint32(active[offset+8:]))
		alpha := math.Float32frombits(binary.LittleEndian.Uint32(active[offset+12:]))
		// Sentinel has nonzero G/B/A; shader output has zero group and zero alpha.
		if g != 0 || b != 0 || alpha != 0 {
			continue
		}
		if u.Kind == core.ShadowUpdateKindDirectional {
			if depth >= -1 && depth < 1 {
				return
			}
		} else if depth > 0 && depth < light.Params[0] {
			return
		}
	}
	panic(fmt.Sprintf("fixture layer %d light %d resolution %d has no caster hit", u.ShadowLayer, u.LightIndex, u.Resolution))
}

func subset(updates []core.ShadowUpdate, predicate func(core.ShadowUpdate) bool) []core.ShadowUpdate {
	var out []core.ShadowUpdate
	for _, u := range updates {
		if predicate(u) {
			out = append(out, u)
		}
	}
	return out
}

func dispatch(a *app.App, updates []core.ShadowUpdate, frame uint64) {
	if len(updates) == 0 {
		return
	}
	enc, err := a.Device.CreateCommandEncoder(nil)
	must(err)
	defer enc.Release()
	m := a.BufferManager
	m.PrepareShadowLights(a.Scene, updates)
	m.DispatchShadowPass(enc, updates)
	submit(a, enc)
	m.RecordShadowUpdates(updates, frame, a.Scene.ShadowRevision())
}

func captureLocal(a *app.App, updates []core.ShadowUpdate) map[uint32][]byte {
	maps := make(map[uint32][]byte)
	for _, u := range updates {
		if u.Kind == core.ShadowUpdateKindDirectional {
			continue
		}
		data := readMap(a, u.ShadowLayer, u.Resolution)
		requireHit(data, u, a)
		maps[u.ShadowLayer] = data
	}
	return maps
}

func clearActive(a *app.App, updates []core.ShadowUpdate) {
	for _, u := range updates {
		pixels := make([]byte, u.Resolution*u.Resolution*16)
		for offset := 0; offset < len(pixels); offset += 16 {
			for component, value := range [4]float32{12345, .125, .25, .5} {
				binary.LittleEndian.PutUint32(pixels[offset+component*4:], math.Float32bits(value))
			}
		}
		must(a.Queue.WriteTexture(&wgpu.ImageCopyTexture{Texture: a.BufferManager.ShadowMapArray, Origin: wgpu.Origin3D{Z: u.ShadowLayer}}, pixels, &wgpu.TextureDataLayout{BytesPerRow: u.Resolution * 16, RowsPerImage: u.Resolution}, &wgpu.Extent3D{Width: u.Resolution, Height: u.Resolution, DepthOrArrayLayers: 1}))
	}
}

func unchangedLocal(a *app.App, updates []core.ShadowUpdate, before map[uint32][]byte, exceptLight uint32) {
	for _, u := range updates {
		if u.Kind == core.ShadowUpdateKindDirectional || u.LightIndex == exceptLight {
			continue
		}
		require(bytes.Equal(readMap(a, u.ShadowLayer, u.Resolution), before[u.ShadowLayer]), fmt.Sprintf("unaffected local map %d changed", u.ShadowLayer))
	}
}

func main() {
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	win, err := glfw.CreateWindow(640, 480, "R2e local shadow reuse", nil, nil)
	must(err)
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
	installReadbackMap(a)
	// Build/record through the real scheduler until all seven layers are warm.
	all := make(map[uint32]core.ShadowUpdate)
	for frame := uint64(0); frame < 4 && len(all) < 7; frame++ {
		updates := m.BuildShadowUpdates(a.Scene, a.Camera, frame, false)
		dispatch(a, updates, frame)
		for _, u := range updates {
			all[u.ShadowLayer] = u
		}
	}
	require(len(all) == 7, "scheduler failed to warm all seven maps")
	var layers []core.ShadowUpdate
	for _, p := range m.ShadowLayerParams {
		layers = append(layers, all[p.Layer])
	}
	for _, u := range layers {
		requireHit(readMap(a, u.ShadowLayer, u.Resolution), u, a)
	}
	baseline := captureLocal(a, layers)
	fmt.Println("Warm fixture: seven maps have genuine caster hits; five local, two directional")
	frames := []uint64{8, 1000000}
	counts := make([][2]int, len(frames))
	schedules := make([][]core.ShadowUpdate, len(frames))
	for index, frame := range frames {
		updates := m.BuildShadowUpdates(a.Scene, a.Camera, frame, false)
		local := subset(updates, func(u core.ShadowUpdate) bool { return u.Kind != core.ShadowUpdateKindDirectional })
		directional := len(updates) - len(local)
		counts[index] = [2]int{len(local), directional}
		schedules[index] = updates
		fmt.Printf("frame=%d local dispatch records=%d (prior periodic fixture=5), directional=%d\n", frame, len(local), directional)
	}
	// Print both nominal and long-idle counts before stopping at RED.
	for index, frame := range frames {
		require(counts[index][0] == 0, fmt.Sprintf("valid local maps scheduled at frame %d", frame))
		require(counts[index][1] == 2, fmt.Sprintf("directional cadence changed at frame %d", frame))
		dispatch(a, schedules[index], frame)
		unchangedLocal(a, layers, baseline, ^uint32(0))
	}
	// Force one local resolution bucket; its fresh output must match reused maps.
	bucket := subset(layers, func(u core.ShadowUpdate) bool {
		return u.Kind != core.ShadowUpdateKindDirectional && u.Resolution == 128
	})
	require(len(bucket) > 0, "fixture lacks force-reference local bucket")
	clearActive(a, bucket)
	dispatch(a, bucket, 1000000)
	unchangedLocal(a, layers, baseline, ^uint32(0))
	// Existing directional caster is remote from every local light. Its upload
	// must not schedule or alter any of the five local maps.
	volume.Cube(a.Scene.Objects[0].XBrickMap, mgl32.Vec3{0, 4, 0}, mgl32.Vec3{7, 7, 7}, 0)
	a.Update()
	require(m.VoxelUploadBytes > 0, "unrelated geometry edit did not upload")
	updates := m.BuildShadowUpdates(a.Scene, a.Camera, 2000000, false)
	require(len(subset(updates, func(u core.ShadowUpdate) bool { return u.Kind != core.ShadowUpdateKindDirectional })) == 0, "unrelated geometry scheduled local shadows")
	dispatch(a, updates, 2000000)
	unchangedLocal(a, layers, baseline, ^uint32(0))
	// Edit the x=85 caster after a long idle; only its spot should refresh.
	const affectedLight = uint32(4)
	volume.Cube(a.Scene.Objects[4].XBrickMap, mgl32.Vec3{0, 4, 0}, mgl32.Vec3{7, 7, 7}, 0)
	a.Update()
	require(m.VoxelUploadBytes > 0, "dependent geometry edit did not upload")
	updates = m.BuildShadowUpdates(a.Scene, a.Camera, 3000000, false)
	local := subset(updates, func(u core.ShadowUpdate) bool { return u.Kind != core.ShadowUpdateKindDirectional })
	require(len(local) == 1 && local[0].LightIndex == affectedLight, "dependent edit must schedule only affected spot")
	dispatch(a, updates, 3000000)
	changed := readMap(a, local[0].ShadowLayer, local[0].Resolution)
	requireHit(changed, local[0], a)
	require(!bytes.Equal(changed, baseline[local[0].ShadowLayer]), "dependent geometry edit did not alter native shadow map")
	unchangedLocal(a, layers, baseline, affectedLight)
	clearActive(a, local)
	dispatch(a, local, 3000000)
	require(bytes.Equal(changed, readMap(a, local[0].ShadowLayer, local[0].Resolution)), "scheduled dependent map differs from forced reference")
	updates = m.BuildShadowUpdates(a.Scene, a.Camera, 4000000, false)
	require(len(subset(updates, func(u core.ShadowUpdate) bool { return u.Kind != core.ShadowUpdateKindDirectional })) == 0, "refreshed dependent spot did not return to idle")
	fmt.Println("PASS: cached local maps remain exact; unrelated uploads idle; dependent edit matches forced native reference")
}
