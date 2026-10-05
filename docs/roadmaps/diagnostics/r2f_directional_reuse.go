//go:build ignore

// Native directional shadow reuse regression. Run from engine cwd.
// Reuses the validated R2d maps and R2b far-only caster geometry.
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
	b, e := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: "R2f map readback", Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
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
	tex, e := a.Device.CreateTexture(&wgpu.TextureDescriptor{Label: "R2f diagnostic shadow map", Size: wgpu.Extent3D{Width: 1024, Height: 1024, DepthOrArrayLayers: m.ShadowMapLayers}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA32Float, Usage: wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopySrc | wgpu.TextureUsageCopyDst})
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

func capture(a *app.App, layers []core.ShadowUpdate) map[uint32][]byte {
	maps := make(map[uint32][]byte)
	for _, u := range layers {
		maps[u.ShadowLayer] = readMap(a, u.ShadowLayer, u.Resolution)
		requireHit(maps[u.ShadowLayer], u, a)
	}
	return maps
}
func unchanged(a *app.App, layers []core.ShadowUpdate, before map[uint32][]byte) {
	for _, u := range layers {
		require(bytes.Equal(before[u.ShadowLayer], readMap(a, u.ShadowLayer, u.Resolution)), fmt.Sprintf("cached map %d changed", u.ShadowLayer))
	}
}
func main() {
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	win, err := glfw.CreateWindow(640, 480, "R2f directional shadow reuse", nil, nil)
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
	far, remote := cube(60), cube(600)
	// R2b verified x=60,z=0 intersects only cascade1 for this projection.
	// x=600 is remote sideways, not downstream of the uncapped light rays.
	a.Scene.AddObject(far)
	a.Scene.AddObject(remote)
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
	seen := make(map[uint32]core.ShadowUpdate)
	for frame := uint64(0); frame < 4 && len(seen) < 7; frame++ {
		updates := m.BuildShadowUpdates(a.Scene, a.Camera, frame, false)
		dispatch(a, updates, frame)
		for _, u := range updates {
			seen[u.ShadowLayer] = u
		}
	}
	require(len(seen) == 7, "scheduler failed to warm seven genuine maps")
	var layers []core.ShadowUpdate
	for _, p := range m.ShadowLayerParams {
		layers = append(layers, seen[p.Layer])
	}
	baseline := capture(a, layers)
	directional := subset(layers, func(u core.ShadowUpdate) bool { return u.Kind == core.ShadowUpdateKindDirectional })
	locals := subset(layers, func(u core.ShadowUpdate) bool { return u.Kind != core.ShadowUpdateKindDirectional })
	require(len(directional) == 2 && len(locals) == 5 && directional[0].CascadeIndex == 0 && directional[1].CascadeIndex == 1, "fixture must contain ordered near/far cascades and five local maps")
	fmt.Println("Warm fixture: seven maps have genuine caster hits; two directional, five local")
	frames := []uint64{1, 2, 1000000}
	counts := make([][2]int, len(frames))
	for i, frame := range frames {
		updates := m.BuildShadowUpdates(a.Scene, a.Camera, frame, false)
		dirs := subset(updates, func(u core.ShadowUpdate) bool { return u.Kind == core.ShadowUpdateKindDirectional })
		counts[i] = [2]int{len(dirs), len(updates) - len(dirs)}
		fmt.Printf("frame=%d directional dispatch records=%d (prior periodic fixture up to2), local=%d\n", frame, counts[i][0], counts[i][1])
	}
	for i, frame := range frames {
		require(counts[i] == [2]int{}, fmt.Sprintf("valid cached maps scheduled at frame %d: directional=%d local=%d", frame, counts[i][0], counts[i][1]))
		unchanged(a, layers, baseline)
	}
	// Force remains explicit and must truly regenerate both maps from sentinel.
	forced := m.BuildShadowUpdates(a.Scene, a.Camera, 1000000, true)
	require(len(forced) == 2, "explicit force must schedule both cascades and no locals")
	clearActive(a, forced)
	dispatch(a, forced, 1000000)
	unchanged(a, layers, baseline)
	volume.Cube(remote.XBrickMap, mgl32.Vec3{0, 4, 0}, mgl32.Vec3{7, 7, 7}, 0)
	a.Update()
	require(m.VoxelUploadBytes > 0, "remote geometry edit did not upload")
	updates := m.BuildShadowUpdates(a.Scene, a.Camera, 2000000, false)
	require(len(updates) == 0, "sideways remote upload scheduled cached maps")
	unchanged(a, layers, baseline)
	volume.Cube(far.XBrickMap, mgl32.Vec3{0, 4, 0}, mgl32.Vec3{7, 7, 7}, 0)
	a.Update()
	require(m.VoxelUploadBytes > 0, "far-only geometry edit did not upload")
	updates = m.BuildShadowUpdates(a.Scene, a.Camera, 3000000, false)
	require(len(updates) == 1 && updates[0].Kind == core.ShadowUpdateKindDirectional && updates[0].CascadeIndex == 1, "far-only edit must refresh only cascade1")
	dispatch(a, updates, 3000000)
	changed := readMap(a, updates[0].ShadowLayer, updates[0].Resolution)
	requireHit(changed, updates[0], a)
	require(!bytes.Equal(changed, baseline[updates[0].ShadowLayer]), "far-only edit did not alter native map")
	unchanged(a, locals, baseline)
	unchanged(a, directional[:1], baseline)
	clearActive(a, updates)
	dispatch(a, updates, 3000000)
	require(bytes.Equal(changed, readMap(a, updates[0].ShadowLayer, updates[0].Resolution)), "far-only scheduled map differs from fresh forced reference")
	require(len(m.BuildShadowUpdates(a.Scene, a.Camera, 4000000, false)) == 0, "refreshed cascade failed to return to idle")
	// Changing projection alters both ray grids without altering local inputs.
	beforeProjection := capture(a, directional)
	a.Camera.Fov = 65
	a.Update()
	updates = m.BuildShadowUpdates(a.Scene, a.Camera, 5000000, false)
	require(len(updates) == 2 && updates[0].Kind == core.ShadowUpdateKindDirectional && updates[1].Kind == core.ShadowUpdateKindDirectional, "camera projection change must schedule both cascades only")
	dispatch(a, updates, 5000000)
	projected := capture(a, directional)
	projectionChanged := false
	for _, u := range directional {
		projectionChanged = projectionChanged || !bytes.Equal(beforeProjection[u.ShadowLayer], projected[u.ShadowLayer])
	}
	require(projectionChanged, "fixture camera projection change did not alter either native cascade")
	forced = m.BuildShadowUpdates(a.Scene, a.Camera, 5000000, true)
	require(len(forced) == 2, "camera force must retain both cascade refreshes")
	clearActive(a, forced)
	dispatch(a, forced, 5000000)
	unchanged(a, directional, projected)
	unchanged(a, locals, baseline)
	require(len(m.BuildShadowUpdates(a.Scene, a.Camera, 6000000, false)) == 0, "camera-refreshed cascades failed to return to idle")
	fmt.Println("PASS: directional caches idle indefinitely; actual forced renders match; remote uploads idle; far-only edit and camera projection refresh match fresh reference")
}
