//go:build ignore

// Native directional-shadow dependency and byte-parity probe. Run from the engine module.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
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
func read(a *app.App, layer, res uint32) []byte {
	row := res * 16
	size := uint64(row * res)
	b, e := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: "R2b readback", Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	must(e)
	defer b.Release()
	enc, e := a.Device.CreateCommandEncoder(nil)
	must(e)
	defer enc.Release()
	enc.CopyTextureToBuffer(&wgpu.ImageCopyTexture{Texture: a.BufferManager.ShadowMapArray, Origin: wgpu.Origin3D{Z: layer}}, &wgpu.ImageCopyBuffer{Buffer: b, Layout: wgpu.TextureDataLayout{BytesPerRow: row, RowsPerImage: res}}, &wgpu.Extent3D{Width: res, Height: res, DepthOrArrayLayers: 1})
	cmd, e := enc.Finish(nil)
	must(e)
	a.Queue.Submit(cmd)
	cmd.Release()
	done := false
	b.MapAsync(wgpu.MapModeRead, 0, size, func(status wgpu.BufferMapAsyncStatus) {
		require(status == wgpu.BufferMapAsyncStatusSuccess, "map failed")
		done = true
	})
	a.Device.Poll(true, nil)
	require(done, "map did not complete")
	out := append([]byte(nil), b.GetMappedRange(0, uint(size))...)
	b.Unmap()
	return out
}
func save(name string, b []byte, res uint32) {
	im := image.NewGray(image.Rect(0, 0, int(res), int(res)))
	for i := 0; i < int(res*res); i++ {
		v := math.Float32frombits(binary.LittleEndian.Uint32(b[i*16:]))
		c := uint8(math.Min(255, math.Max(0, float64(v/8*255))))
		im.SetGray(i%int(res), i/int(res), color.Gray{Y: c})
	}
	f, e := os.Create("/tmp/gekko-r2b-native-" + name + ".png")
	must(e)
	must(png.Encode(f, im))
	must(f.Close())
}
func update(a *app.App) []core.ShadowUpdate {
	glfw.PollEvents()
	a.Update()
	// Hold cadence time fixed to isolate dependency invalidation, using actual
	// tier budgets, shader data and native upload execution.
	return a.BufferManager.BuildShadowUpdates(a.Scene, a.Camera, 0, false)
}
func dispatch(a *app.App, updates []core.ShadowUpdate) {
	m := a.BufferManager
	enc, e := a.Device.CreateCommandEncoder(nil)
	must(e)
	defer enc.Release()
	m.RecordShadowUpdates(updates, 0, a.Scene.ShadowRevision())
	m.PrepareShadowLights(a.Scene, updates)
	m.DispatchShadowPass(enc, updates)
	cmd, e := enc.Finish(nil)
	must(e)
	a.Queue.Submit(cmd)
	cmd.Release()
	a.Device.Poll(true, nil)
}
func main() {
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	win, e := glfw.CreateWindow(640, 480, "R2b native shadow readback", nil, nil)
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
	near, far := cube(0), cube(60)
	a.Scene.AddObject(near)
	a.Scene.AddObject(far)
	a.Scene.Lights = []core.Light{{Direction: [4]float32{0, -1, 0, 0}, Color: [4]float32{1, 1, 1, 1}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}}}
	updates := update(a)
	require(len(updates) == 2, "expected two cold cascades")
	m := a.BufferManager
	// Add readback usage only to the disposable diagnostic output texture.
	tex, e := a.Device.CreateTexture(&wgpu.TextureDescriptor{Label: "R2b diagnostic shadow map", Size: wgpu.Extent3D{Width: 1024, Height: 1024, DepthOrArrayLayers: m.ShadowMapLayers}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA32Float, Usage: wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopySrc})
	must(e)
	view, e := tex.CreateView(&wgpu.TextureViewDescriptor{Format: wgpu.TextureFormatRGBA32Float, Dimension: wgpu.TextureViewDimension2DArray, ArrayLayerCount: m.ShadowMapLayers, MipLevelCount: 1})
	must(e)
	m.ShadowMapView.Release()
	m.ShadowMapArray.Release()
	m.ShadowMapArray, m.ShadowMapView = tex, view
	// Isolate invalidation from the existing update-buffer growth issue:
	// CreateShadowBindGroups writes a zero header when it recreates groups.
	// Provision capacity before dispatch so it cannot overwrite the first update.
	m.ShadowUpdatesBuf.Release()
	m.ShadowUpdatesBuf, e = a.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: "R2b diagnostic updates", Size: 4096, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
	must(e)
	m.CreateShadowBindGroups()
	dispatch(a, updates)
	res := m.ShadowLayerParams[0].EffectiveResolution
	beforeN, beforeF := read(a, 0, res), read(a, 1, res)
	save("before-near", beforeN, res)
	save("before-far", beforeF, res)
	require(len(update(a)) == 0, "idle directional shadows refreshed")
	// Upload a resident object beyond both cascade prisms.
	remote := cube(500)
	a.Scene.AddObject(remote)
	updates = update(a)
	require(m.VoxelUploadBytes > 0, "remote did not upload")
	require(len(updates) == 0, "unrelated upload refreshed cascades")
	require(bytes.Equal(beforeN, read(a, 0, res)) && bytes.Equal(beforeF, read(a, 1, res)), "remote changed cached bytes")
	volume.Cube(far.XBrickMap, mgl32.Vec3{0, 4, 0}, mgl32.Vec3{7, 7, 7}, 0)
	updates = update(a)
	require(len(updates) == 1 && updates[0].CascadeIndex == 1, "far carve must refresh only second cascade")
	dispatch(a, updates)
	afterF := read(a, 1, res)
	save("after-carve-far", afterF, res)
	require(!bytes.Equal(beforeF, afterF), "far carve did not change native shadow")
	require(bytes.Equal(beforeN, read(a, 0, res)), "far carve changed near shadow")
	forced := m.BuildShadowUpdates(a.Scene, a.Camera, 0, true)
	dispatch(a, forced)
	forcedN, forcedF := read(a, 0, res), read(a, 1, res)
	save("forced-near", forcedN, res)
	save("forced-far", forcedF, res)
	for idx, pair := range [][2][]byte{{beforeN, forcedN}, {afterF, forcedF}} {
		changed := 0
		for i := 0; i < len(pair[0]); i += 16 {
			if !bytes.Equal(pair[0][i:i+16], pair[1][i:i+16]) {
				if changed < 8 {
					fmt.Printf("layer%d pixel%d old=%v new=%v\n", idx, i/16, math.Float32frombits(binary.LittleEndian.Uint32(pair[0][i:])), math.Float32frombits(binary.LittleEndian.Uint32(pair[1][i:])))
				}
				changed++
			}
		}
		fmt.Printf("layer%d changed %d pixels\n", idx, changed)
	}
	require(bytes.Equal(beforeN, forcedN) && bytes.Equal(afterF, forcedF), "cached directional maps differ from forced rebuild")
	far.MaterialTable = append([]core.Material(nil), far.MaterialTable...)
	far.MaterialTable[1].Transparency = 1
	updates = update(a)
	require(len(updates) == 1 && updates[0].CascadeIndex == 1, "far opacity must refresh only second cascade")
	dispatch(a, updates)
	transparentF := read(a, 1, res)
	save("transparent-far", transparentF, res)
	require(!bytes.Equal(afterF, transparentF), "opacity did not change native shadow")
	dispatch(a, m.BuildShadowUpdates(a.Scene, a.Camera, 0, true))
	require(bytes.Equal(beforeN, read(a, 0, res)) && bytes.Equal(transparentF, read(a, 1, res)), "transparent directional cache differs from forced rebuild")
	fmt.Printf("PASS: remote upload skipped 2 cascades; far carve and opacity refreshed only second; near bytes unchanged; forced rebuild parity. Resolution %d.\n", res)
}
