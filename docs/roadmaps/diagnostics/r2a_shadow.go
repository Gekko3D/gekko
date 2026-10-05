//go:build ignore

// Native local-shadow dependency and byte-parity probe. Run from the engine module.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"runtime"
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
	b, e := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: "R2a readback", Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
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
	f, e := os.Create("/tmp/gekko-r2a-native-" + name + ".png")
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
	win, e := glfw.CreateWindow(640, 480, "R2a native shadow readback", nil, nil)
	must(e)
	defer win.Destroy()
	a := app.NewApp(win)
	a.FeatureConfig = app.AppFeatureConfig{AutoRegisterDefaults: false}
	must(a.Init())
	defer a.Shutdown()
	a.Camera.Position = mgl32.Vec3{0, 4, 45}
	a.Camera.LookAt = mgl32.Vec3{0, 1, 0}
	a.Camera.Far = 100
	a.OcclusionMode = core.OcclusionOff
	left, right := cube(-20), cube(20)
	a.Scene.AddObject(left)
	a.Scene.AddObject(right)
	for _, x := range []float32{-19, 21} {
		a.Scene.Lights = append(a.Scene.Lights, core.Light{Position: [4]float32{x, 5, 1, 0}, Direction: [4]float32{0, -1, 0, 0}, Color: [4]float32{1, 1, 1, 1}, Params: [4]float32{8, float32(math.Cos(math.Pi / 4)), float32(core.LightTypeSpot), 1}})
	}
	updates := update(a)
	require(len(updates) == 2, "expected two cold spot updates")
	m := a.BufferManager
	// Add readback usage only to the disposable diagnostic output texture.
	tex, e := a.Device.CreateTexture(&wgpu.TextureDescriptor{Label: "R2a diagnostic shadow map", Size: wgpu.Extent3D{Width: 1024, Height: 1024, DepthOrArrayLayers: m.ShadowMapLayers}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA32Float, Usage: wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopySrc})
	must(e)
	view, e := tex.CreateView(&wgpu.TextureViewDescriptor{Format: wgpu.TextureFormatRGBA32Float, Dimension: wgpu.TextureViewDimension2DArray, ArrayLayerCount: m.ShadowMapLayers, MipLevelCount: 1})
	must(e)
	m.ShadowMapView.Release()
	m.ShadowMapArray.Release()
	m.ShadowMapArray, m.ShadowMapView = tex, view
	m.CreateShadowBindGroups()
	dispatch(a, updates)
	res := m.ShadowLayerParams[0].EffectiveResolution
	beforeL, beforeR := read(a, 0, res), read(a, 1, res)
	save("before-left", beforeL, res)
	save("before-right", beforeR, res)
	require(len(update(a)) == 0, "idle local shadows refreshed")
	remote := cube(0)
	a.Scene.AddObject(remote)
	updates = update(a)
	require(m.VoxelUploadBytes > 0, "remote didn't upload")
	fmt.Printf("remote: updates=%+v sectors=%d bricks=%d materials=%d bytes=%d\n", updates, m.VoxelSectorsUploaded, m.VoxelBricksUploaded, m.VoxelMaterialsUploaded, m.VoxelUploadBytes)
	require(len(updates) == 0, "unrelated upload refreshed local shadows")
	require(bytes.Equal(beforeL, read(a, 0, res)) && bytes.Equal(beforeR, read(a, 1, res)), "unrelated shadow bytes changed")
	volume.Cube(left.XBrickMap, mgl32.Vec3{0, 4, 0}, mgl32.Vec3{7, 7, 7}, 0)
	updates = update(a)
	require(len(updates) == 1 && updates[0].LightIndex == 0, "left carve must refresh only left spot")
	dispatch(a, updates)
	afterL := read(a, 0, res)
	save("after-carve-left", afterL, res)
	require(!bytes.Equal(beforeL, afterL), "carve didn't change native shadow")
	require(bytes.Equal(beforeR, read(a, 1, res)), "carve changed unrelated native shadow")
	// Force every spot layer against identical current geometry for byte parity.
	forced := []core.ShadowUpdate{}
	for _, p := range m.ShadowLayerParams {
		forced = append(forced, core.ShadowUpdate{LightIndex: p.LightIndex, ShadowLayer: p.Layer, CascadeIndex: p.CascadeIndex, Kind: p.Kind, Tier: p.Tier, Resolution: p.EffectiveResolution})
	}
	dispatch(a, forced)
	require(bytes.Equal(afterL, read(a, 0, res)) && bytes.Equal(beforeR, read(a, 1, res)), "cached shadows differ from forced rebuild")
	left.MaterialTable = append([]core.Material(nil), left.MaterialTable...)
	left.MaterialTable[1].Transparency = 1
	updates = update(a)
	require(len(updates) == 1 && updates[0].LightIndex == 0, "transparency upload must refresh only left spot")
	dispatch(a, updates)
	transparentL := read(a, 0, res)
	save("transparent-left", transparentL, res)
	require(!bytes.Equal(afterL, transparentL), "transparency upload didn't change shadow")
	require(bytes.Equal(beforeR, read(a, 1, res)), "transparency changed unrelated shadow")
	dispatch(a, forced)
	require(bytes.Equal(transparentL, read(a, 0, res)), "transparent cache differs from forced rebuild")
	fmt.Printf("PASS: native remote upload skipped 2 spot updates; carve and transparency each refreshed 1; unrelated bytes exact; forced rebuild parity. Resolution %d.\n", res)
}
