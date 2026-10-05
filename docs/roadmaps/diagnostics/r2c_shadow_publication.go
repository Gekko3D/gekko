//go:build ignore

// Native shadow-update publication regression. Run from the engine module.
// Uses one resolution to isolate update-buffer capacity and bind-group lifetime.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
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
func readBuffer(a *app.App, size uint64) []byte {
	b, e := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: "R2c update readback", Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	must(e)
	defer b.Release()
	enc, e := a.Device.CreateCommandEncoder(nil)
	must(e)
	defer enc.Release()
	enc.CopyBufferToBuffer(a.BufferManager.ShadowUpdatesBuf, 0, b, 0, size)
	submit(a, enc)
	return mapped(a, b, size)
}
func readMap(a *app.App, layer uint32) []byte {
	const resolution = uint32(1024)
	const row = resolution * 16
	const size = uint64(row * resolution)
	b, e := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: "R2c map readback", Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	must(e)
	defer b.Release()
	enc, e := a.Device.CreateCommandEncoder(nil)
	must(e)
	defer enc.Release()
	enc.CopyTextureToBuffer(&wgpu.ImageCopyTexture{Texture: a.BufferManager.ShadowMapArray, Origin: wgpu.Origin3D{Z: layer}}, &wgpu.ImageCopyBuffer{Buffer: b, Layout: wgpu.TextureDataLayout{BytesPerRow: row, RowsPerImage: resolution}}, &wgpu.Extent3D{Width: resolution, Height: resolution, DepthOrArrayLayers: 1})
	submit(a, enc)
	return mapped(a, b, size)
}
func encode(updates []core.ShadowUpdate) []byte {
	data := make([]byte, len(updates)*24)
	for i, u := range updates {
		for j, v := range []uint32{u.LightIndex, u.ShadowLayer, u.CascadeIndex, u.Kind, u.Tier, u.Resolution} {
			binary.LittleEndian.PutUint32(data[i*24+j*4:], v)
		}
	}
	return data
}
func assertRecords(a *app.App, updates []core.ShadowUpdate) {
	want := encode(updates)
	got := readBuffer(a, uint64(len(want)))
	if bytes.Equal(got, want) {
		return
	}
	fields := []string{"light index", "shadow layer", "cascade index", "kind", "tier", "resolution"}
	for i := range updates {
		for j, name := range fields {
			offset := i*24 + j*4
			g, w := binary.LittleEndian.Uint32(got[offset:]), binary.LittleEndian.Uint32(want[offset:])
			if g != w {
				panic(fmt.Sprintf("record %d %s: got %d, want %d", i, name, g, w))
			}
		}
	}
	panic("shadow update bytes changed")
}
func dispatch(a *app.App, updates []core.ShadowUpdate) {
	m := a.BufferManager
	enc, e := a.Device.CreateCommandEncoder(nil)
	must(e)
	defer enc.Release()
	m.RecordShadowUpdates(updates, 0, a.Scene.ShadowRevision())
	m.PrepareShadowLights(a.Scene, updates)
	m.DispatchShadowPass(enc, updates)
	submit(a, enc)
}
func installReadbackMap(a *app.App) {
	m := a.BufferManager
	tex, e := a.Device.CreateTexture(&wgpu.TextureDescriptor{Label: "R2c diagnostic shadow map", Size: wgpu.Extent3D{Width: 1024, Height: 1024, DepthOrArrayLayers: m.ShadowMapLayers}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA32Float, Usage: wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopySrc})
	must(e)
	view, e := tex.CreateView(&wgpu.TextureViewDescriptor{Format: wgpu.TextureFormatRGBA32Float, Dimension: wgpu.TextureViewDimension2DArray, ArrayLayerCount: m.ShadowMapLayers, MipLevelCount: 1})
	must(e)
	m.ShadowMapView.Release()
	m.ShadowMapArray.Release()
	m.ShadowMapArray, m.ShadowMapView = tex, view
	m.CreateShadowBindGroups()
}
func hasHit(data []byte) bool {
	// Directional shadow red stores clamped NDC depth; misses store 1.0.
	for offset := 0; offset < len(data); offset += 16 {
		v := math.Float32frombits(binary.LittleEndian.Uint32(data[offset:]))
		if v >= -1 && v < 1 {
			return true
		}
	}
	return false
}
func main() {
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	win, e := glfw.CreateWindow(640, 480, "R2c shadow publication regression", nil, nil)
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
	// Noncasting light 0 makes first queued directional LightIndex nonzero.
	a.Scene.Lights = []core.Light{{Params: [4]float32{10, 0, float32(core.LightTypePoint), 0}}, {Direction: [4]float32{0, -1, 0, 0}, Color: [4]float32{1, 1, 1, 1}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}}}
	glfw.PollEvents()
	a.Update()
	m := a.BufferManager
	installReadbackMap(a)
	cold := m.BuildShadowUpdates(a.Scene, a.Camera, 0, false)
	require(len(cold) == 2, "fixture must schedule both cold directional cascades")
	for _, u := range cold {
		require(u.Kind == core.ShadowUpdateKindDirectional && u.LightIndex == 1 && u.Resolution == 1024, "fixture must use directional pair with nonzero light index at one resolution")
	}
	failures := 0
	run := func(name string, fn func()) {
		func() {
			defer func() {
				if failure := recover(); failure != nil {
					failures++
					fmt.Printf("FAIL %s: %v\n", name, failure)
				}
			}()
			fn()
			fmt.Printf("PASS %s\n", name)
		}()
	}
	// No update-buffer replacement or preallocation: this is the real cold path.
	coldCapacity := m.ShadowUpdatesBuf.GetSize()
	dispatch(a, cold)
	run("ColdDirectionalDispatchPublishesRecords", func() {
		require(m.ShadowUpdatesBuf.GetSize() > coldCapacity, "cold fixture did not trigger actual update-buffer growth")
		assertRecords(a, cold)
	})
	run("ColdDirectionalMapMatchesForcedRebuild", func() {
		before := readMap(a, cold[0].ShadowLayer)
		hasColdHit := hasHit(before)
		forced := m.BuildShadowUpdates(a.Scene, a.Camera, 0, true)
		dispatch(a, forced)
		after := readMap(a, cold[0].ShadowLayer)
		require(hasHit(after), "opaque cube fixture has no hit even after forced rebuild")
		require(hasColdHit, "first cold map has no opaque cube hit")
		require(bytes.Equal(before, after), "first cold map differs from forced rebuild with identical inputs")
	})
	fmt.Printf("Cold update buffer capacity: %d -> %d bytes\n", coldCapacity, m.ShadowUpdatesBuf.GetSize())
	// Valid far record has six nonzero fields. Keep several complete rows queued.
	sentinel := []core.ShadowUpdate{cold[1], cold[0], cold[1]}
	first := sentinel[0]
	for _, field := range []uint32{first.LightIndex, first.ShadowLayer, first.CascadeIndex, first.Kind, first.Tier, first.Resolution} {
		require(field != 0, "far sentinel must have all six fields nonzero")
	}
	for _, name := range []string{"ExplicitRebindPreservesQueuedRows", "RepeatedRebindPreservesQueuedRows"} {
		run(name, func() {
			a.Queue.WriteBuffer(m.ShadowUpdatesBuf, 0, encode(sentinel))
			m.CreateShadowBindGroups()
			if name == "RepeatedRebindPreservesQueuedRows" {
				m.CreateShadowBindGroups()
				m.CreateShadowBindGroups()
			}
			assertRecords(a, sentinel)
		})
	}
	run("EmptyDispatchPreservesQueuedRows", func() {
		a.Queue.WriteBuffer(m.ShadowUpdatesBuf, 0, encode(sentinel))
		enc, e := a.Device.CreateCommandEncoder(nil)
		must(e)
		defer enc.Release()
		m.DispatchShadowPass(enc, nil)
		submit(a, enc)
		assertRecords(a, sentinel)
	})
	run("CapacityGrowthPreservesEveryPublishedField", func() {
		before := m.ShadowUpdatesBuf.GetSize()
		count := int(before/24) + 1
		updates := make([]core.ShadowUpdate, count)
		for i := range updates {
			updates[i] = cold[1-i%2]
		}
		dispatch(a, updates)
		require(m.ShadowUpdatesBuf.GetSize() > before, "fixture did not trigger actual update-buffer growth")
		assertRecords(a, updates)
	})
	run("SceneBufferRecreationPreservesPendingRows", func() {
		a.Queue.WriteBuffer(m.ShadowUpdatesBuf, 0, encode(sentinel))
		before := m.ShadowInstancesBuf
		for i := 0; i < 32; i++ {
			a.Scene.AddObject(cube(float32(200 + i*4)))
		}
		glfw.PollEvents()
		a.Update()
		require(m.ShadowInstancesBuf != before, "fixture did not recreate shadow scene buffer")
		m.CreateShadowBindGroups()
		assertRecords(a, sentinel)
	})
	run("MissingUpdateBufferCreatesUsablePublicationDestination", func() {
		old := m.ShadowUpdatesBuf
		m.ShadowUpdatesBuf = nil
		m.CreateShadowBindGroups()
		defer old.Release()
		require(m.ShadowUpdatesBuf != nil && m.ShadowUpdatesBuf.GetSize() >= 24, "missing update buffer was not created with usable capacity")
		a.Queue.WriteBuffer(m.ShadowUpdatesBuf, 0, encode(sentinel))
		assertRecords(a, sentinel)
	})
	if failures != 0 {
		fmt.Printf("FAIL: %d native publication regressions\n", failures)
		os.Exit(1)
	}
	fmt.Println("PASS: all native shadow publication regressions")
}
