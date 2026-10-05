//go:build ignore

// Native App.Render directional projection stability regression. Run from engine cwd.
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
	b, e := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: "R2h map readback", Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
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
	tex, e := a.Device.CreateTexture(&wgpu.TextureDescriptor{Label: "R2h diagnostic shadow map", Size: wgpu.Extent3D{Width: 1024, Height: 1024, DepthOrArrayLayers: m.ShadowMapLayers}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA32Float, Usage: wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopySrc | wgpu.TextureUsageCopyDst})
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

func render(a *app.App, name string, directional, total, motion int) {
	before := a.RenderFrameIndex
	// Avoid accepting counts retained from an earlier frame if graph recording
	// fails before reaching the shadow node.
	for _, key := range []string{"ShadowGraphNode", "ShadowUpdates", "ShadowDirectionalUpdates", "ShadowCameraMotion"} {
		a.Profiler.Counts[key] = -1
	}
	glfw.PollEvents()
	a.Render()
	a.Device.Poll(true, nil)
	require(a.RenderFrameIndex == before+1, "App.Render did not submit/present a full frame")
	require(a.Profiler.Counts["ShadowGraphNode"] == 1, "App.Render did not record the shadow graph node")
	gotDir, gotTotal, gotMotion := a.Profiler.Counts["ShadowDirectionalUpdates"], a.Profiler.Counts["ShadowUpdates"], a.Profiler.Counts["ShadowCameraMotion"]
	fmt.Printf("%s: directional=%d total=%d motion=%d\n", name, gotDir, gotTotal, gotMotion)
	require(gotDir == directional && gotTotal == total && gotMotion == motion, fmt.Sprintf("%s unexpected actual App.Render shadow work", name))
}
func forcedReference(a *app.App, directional []core.ShadowUpdate, expected map[uint32][]byte) {
	forced := a.BufferManager.BuildShadowUpdates(a.Scene, a.Camera, a.RenderFrameIndex, true)
	require(len(forced) == 2 && forced[0].Kind == core.ShadowUpdateKindDirectional && forced[1].Kind == core.ShadowUpdateKindDirectional, "explicit manager force no longer refreshes both cascades")
	clearActive(a, forced)
	dispatch(a, forced, a.RenderFrameIndex)
	unchanged(a, directional, expected)
}
func main() {
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	win, err := glfw.CreateWindow(640, 480, "R2h App.Render camera shadow reuse", nil, nil)
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
	// Far-only native pixel change is asserted below.
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

	// Actual App.Render owns cold scheduling, cache recording and dispatch.
	require(!a.HasLastCameraState, "fixture must begin with missing camera history")
	render(a, "cold missing history", 2, 7, 1)
	require(a.HasLastCameraState, "successful Render did not publish camera history")
	var layers []core.ShadowUpdate
	for _, p := range m.ShadowLayerParams {
		layers = append(layers, core.ShadowUpdate{LightIndex: p.LightIndex, ShadowLayer: p.Layer, CascadeIndex: p.CascadeIndex, Kind: p.Kind, Tier: p.Tier, Resolution: p.EffectiveResolution})
	}
	directional := subset(layers, func(u core.ShadowUpdate) bool { return u.Kind == core.ShadowUpdateKindDirectional })
	locals := subset(layers, func(u core.ShadowUpdate) bool { return u.Kind != core.ShadowUpdateKindDirectional })
	require(len(directional) == 2 && len(locals) == 5 && directional[0].CascadeIndex == 0 && directional[1].CascadeIndex == 1, "fixture must contain near/far cascades and five locals")
	baseline := capture(a, layers)
	fmt.Println("Cold App.Render produced seven genuine caster-hit maps")

	initialPosition, initialLookAt := a.Camera.Position, a.Camera.LookAt
	initialKeys := a.Scene.Lights[0].DirectionalCascades
	nearTexel, farTexel := initialKeys[0].Params[1], initialKeys[1].Params[1]
	require(nearTexel > 0 && farTexel > nearTexel && nearTexel*.1 > .001, "fixture needs ordered texels and measurable subcell camera motion")
	fmt.Printf("Native texels near=%.9f far=%.9f; subcell camera delta=%.9f\n", nearTexel, farTexel, nearTexel*.1)
	moveX := func(offset float32) {
		a.Camera.Position = initialPosition.Add(mgl32.Vec3{offset, 0, 0})
		a.Camera.LookAt = initialLookAt.Add(mgl32.Vec3{offset, 0, 0})
		a.Update()
	}
	moveX(nearTexel * .1)
	subcellKeys := a.Scene.Lights[0].DirectionalCascades
	fmt.Printf("Subcell exact cascade equality near=%v far=%v\n", subcellKeys[0] == initialKeys[0], subcellKeys[1] == initialKeys[1])
	// Exercise the actual app pass before checking the exact keys, so baseline
	// failure reports the unnecessary GPU work as well as projection drift.
	render(a, "subcell camera translation", 0, 0, 1)
	require(subcellKeys == initialKeys, "subcell translation changed exact directional projection inputs")
	unchanged(a, layers, baseline)
	forcedReference(a, directional, baseline)
	// Crossing zero within the same cell must not introduce a signed-zero
	// dependency difference in the scheduler's exact float-bit inputs.
	moveX(-nearTexel * .1)
	negativeSubcellKeys := a.Scene.Lights[0].DirectionalCascades
	render(a, "negative subcell camera translation", 0, 0, 1)
	require(negativeSubcellKeys == initialKeys, "negative subcell translation changed directional projection inputs")
	unchanged(a, layers, baseline)
	forcedReference(a, directional, baseline)
	moveX(nearTexel * .6)
	nearCrossKeys := a.Scene.Lights[0].DirectionalCascades
	require(nearCrossKeys[0] != initialKeys[0] && nearCrossKeys[1] == initialKeys[1], "near-cell crossing must change only near cascade projection inputs")
	render(a, "near-cell crossing", 1, 1, 1)
	nearCrossMaps := capture(a, directional)
	require(!bytes.Equal(nearCrossMaps[directional[0].ShadowLayer], baseline[directional[0].ShadowLayer]), "near-cell crossing did not change native near map")
	unchanged(a, directional[1:], baseline)
	unchanged(a, locals, baseline)
	forcedReference(a, directional, nearCrossMaps)
	moveX(farTexel * .6)
	farCrossKeys := a.Scene.Lights[0].DirectionalCascades
	require(farCrossKeys[0] != nearCrossKeys[0] && farCrossKeys[1] != nearCrossKeys[1], "far-cell fixture must cross both actual cascade grids")
	render(a, "far-cell crossing", 2, 2, 1)
	farCrossMaps := capture(a, directional)
	for _, u := range directional {
		require(!bytes.Equal(farCrossMaps[u.ShadowLayer], nearCrossMaps[u.ShadowLayer]), fmt.Sprintf("far-cell crossing did not change native cascade%d", u.CascadeIndex))
	}
	unchanged(a, locals, baseline)
	forcedReference(a, directional, farCrossMaps)
	// Caster geometry still invalidates current maps within a stable cell.
	volume.Cube(far.XBrickMap, mgl32.Vec3{0, 4, 0}, mgl32.Vec3{7, 7, 7}, 0)
	a.Update()
	require(m.VoxelUploadBytes > 0, "far-only geometry edit did not upload")
	require(a.Scene.Lights[0].DirectionalCascades == farCrossKeys, "caster edit changed projection inputs")
	render(a, "far-only caster edit in stable cell", 1, 1, 0)
	edited := capture(a, directional)
	require(bytes.Equal(edited[directional[0].ShadowLayer], farCrossMaps[directional[0].ShadowLayer]), "far-only edit changed near native map")
	require(!bytes.Equal(edited[directional[1].ShadowLayer], farCrossMaps[directional[1].ShadowLayer]), "far-only edit did not alter far native map")
	unchanged(a, locals, baseline)
	forcedReference(a, directional, edited)
	// Depth motion along the light direction remains unsnapped and changes
	// projection inputs; the current dependency path must observe both grids.
	a.Camera.Position[1] += 1
	a.Camera.LookAt[1] += 1
	a.Update()
	depthKeys := a.Scene.Lights[0].DirectionalCascades
	require(depthKeys[0] != farCrossKeys[0] && depthKeys[1] != farCrossKeys[1], "light-direction camera motion must change both cascade inputs")
	render(a, "camera depth motion", 2, 2, 1)
	depthMaps := capture(a, directional)
	changed := false
	for _, u := range directional {
		changed = changed || !bytes.Equal(depthMaps[u.ShadowLayer], edited[u.ShadowLayer])
	}
	require(changed, "light-direction camera motion did not alter native maps")
	forcedReference(a, directional, depthMaps)
	unchanged(a, locals, baseline)
	render(a, "stable after genuine changes", 0, 0, 0)
	unchanged(a, directional, depthMaps)
	fmt.Println("PASS: exact cascade keys and native cached maps survive subcell camera motion; cell crossings and genuine changes match fresh forced reference")
}
