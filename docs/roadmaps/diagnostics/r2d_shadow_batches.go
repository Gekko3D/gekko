//go:build ignore

// Native mixed-resolution shadow publication regression. Run from engine cwd.
// Reference buckets are submitted independently; candidate calls share one encoder.
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
func readMap(a *app.App, layer, resolution uint32) []byte {
	row := resolution * 16
	size := uint64(row * resolution)
	b, e := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: "R2d map readback", Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	must(e)
	defer b.Release()
	enc, e := a.Device.CreateCommandEncoder(nil)
	must(e)
	defer enc.Release()
	enc.CopyTextureToBuffer(&wgpu.ImageCopyTexture{Texture: a.BufferManager.ShadowMapArray, Origin: wgpu.Origin3D{Z: layer}}, &wgpu.ImageCopyBuffer{Buffer: b, Layout: wgpu.TextureDataLayout{BytesPerRow: row, RowsPerImage: resolution}}, &wgpu.Extent3D{Width: resolution, Height: resolution, DepthOrArrayLayers: 1})
	submit(a, enc)
	return mapped(a, b, size)
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
	tex, e := a.Device.CreateTexture(&wgpu.TextureDescriptor{Label: "R2d diagnostic shadow map", Size: wgpu.Extent3D{Width: 1024, Height: 1024, DepthOrArrayLayers: m.ShadowMapLayers}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA32Float, Usage: wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopySrc | wgpu.TextureUsageCopyDst})
	must(e)
	view, e := tex.CreateView(&wgpu.TextureViewDescriptor{Format: wgpu.TextureFormatRGBA32Float, Dimension: wgpu.TextureViewDimension2DArray, ArrayLayerCount: m.ShadowMapLayers, MipLevelCount: 1})
	must(e)
	m.ShadowMapView.Release()
	m.ShadowMapArray.Release()
	m.ShadowMapArray, m.ShadowMapView = tex, view
	m.CreateShadowBindGroups()
}

func sentinelPixels() []byte {
	data := make([]byte, 1024*1024*16)
	for offset := 0; offset < len(data); offset += 16 {
		for j, v := range []float32{-.75, 13, 17, 19} {
			binary.LittleEndian.PutUint32(data[offset+j*4:], math.Float32bits(v))
		}
	}
	return data
}
func clearMaps(a *app.App, sentinel []byte) {
	for layer := uint32(0); layer < a.BufferManager.ShadowMapLayers; layer++ {
		must(a.Queue.WriteTexture(&wgpu.ImageCopyTexture{Texture: a.BufferManager.ShadowMapArray, Origin: wgpu.Origin3D{Z: layer}}, sentinel, &wgpu.TextureDataLayout{BytesPerRow: 1024 * 16, RowsPerImage: 1024}, &wgpu.Extent3D{Width: 1024, Height: 1024, DepthOrArrayLayers: 1}))
	}
}
func readPixel(a *app.App, layer, x, y uint32) []byte {
	b, e := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: "R2d guard readback", Size: 256, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	must(e)
	defer b.Release()
	enc, e := a.Device.CreateCommandEncoder(nil)
	must(e)
	defer enc.Release()
	enc.CopyTextureToBuffer(&wgpu.ImageCopyTexture{Texture: a.BufferManager.ShadowMapArray, Origin: wgpu.Origin3D{X: x, Y: y, Z: layer}}, &wgpu.ImageCopyBuffer{Buffer: b, Layout: wgpu.TextureDataLayout{BytesPerRow: 256, RowsPerImage: 1}}, &wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1})
	submit(a, enc)
	return mapped(a, b, 256)[:16]
}
func requireOutsideSentinel(a *app.App, sentinel []byte, res, layer uint32) {
	if res == 1024 {
		return
	}
	for _, p := range [][2]uint32{{res, 0}, {0, res}, {res, res}, {1023, 1023}} {
		require(bytes.Equal(readPixel(a, layer, p[0], p[1]), sentinel[:16]), fmt.Sprintf("layer %d changed outside viewport %d at %v", layer, res, p))
	}
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
func reference(a *app.App, updates []core.ShadowUpdate, sentinel []byte) map[uint32][]byte {
	clearMaps(a, sentinel)
	for _, resolution := range []uint32{512, 256, 128, 1024} {
		var bucket []core.ShadowUpdate
		for _, u := range updates {
			if u.Resolution == resolution {
				bucket = append(bucket, u)
			}
		}
		if len(bucket) > 0 {
			dispatch(a, bucket)
		}
	}
	result := map[uint32][]byte{}
	for _, u := range updates {
		active := readMap(a, u.ShadowLayer, u.Resolution)
		requireOutsideSentinel(a, sentinel, u.Resolution, u.ShadowLayer)
		requireHit(active, u, a)
		result[u.ShadowLayer] = active
	}
	return result
}
func compare(a *app.App, updates []core.ShadowUpdate, want map[uint32][]byte, sentinel []byte) {
	selected := map[uint32]core.ShadowUpdate{}
	for _, u := range updates {
		if u.Resolution == 512 || u.Resolution == 256 || u.Resolution == 128 || u.Resolution == 1024 {
			selected[u.ShadowLayer] = u
		}
	}
	for layer := uint32(0); layer < a.BufferManager.ShadowMapLayers; layer++ {
		u, active := selected[layer]
		if !active {
			for _, p := range [][2]uint32{{0, 0}, {64, 64}, {512, 512}, {1023, 1023}} {
				require(bytes.Equal(readPixel(a, layer, p[0], p[1]), sentinel[:16]), fmt.Sprintf("untouched layer %d changed at %v", layer, p))
			}
			continue
		}
		pixels := readMap(a, layer, u.Resolution)
		require(bytes.Equal(pixels, want[layer]), fmt.Sprintf("layer %d light %d resolution %d differs from separately submitted baseline", layer, u.LightIndex, u.Resolution))
		requireOutsideSentinel(a, sentinel, u.Resolution, layer)
	}
}
func together(a *app.App, calls [][]core.ShadowUpdate, rebind bool) {
	enc, e := a.Device.CreateCommandEncoder(nil)
	must(e)
	defer enc.Release()
	for _, updates := range calls {
		a.BufferManager.PrepareShadowLights(a.Scene, updates)
		a.BufferManager.DispatchShadowPass(enc, updates)
		if rebind {
			a.BufferManager.CreateShadowBindGroups()
		}
	}
	// Submission happens only after every Dispatch has returned, exercising packet lifetime.
	submit(a, enc)
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
	installReadbackMap(a)
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
	sentinel := sentinelPixels()
	// Independently submitted one-resolution baseline also proves every caster hit.
	baseline := reference(a, updates, sentinel)
	fmt.Printf("Fixture has valid caster hits: 1024=%d 512=%d 256=%d 128=%d layers; atlas capacity %d\n", counts[1024], counts[512], counts[256], counts[128], m.ShadowMapLayers)
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
	run("AllFourBucketsShuffledUnequalCounts", func() {
		clearMaps(a, sentinel)
		shuffled := []core.ShadowUpdate{updates[6], updates[2], updates[0], updates[4], updates[3], updates[1], updates[5]}
		together(a, [][]core.ShadowUpdate{shuffled}, false)
		compare(a, updates, baseline, sentinel)
	})
	run("OmittedBucketsAndUnsupportedResolutionLeaveLayersUntouched", func() {
		selected := subset(updates, func(u core.ShadowUpdate) bool { return u.Resolution == 512 || u.Resolution == 128 })
		unsupported := updates[0]
		unsupported.ShadowLayer = m.ShadowMapLayers - 1
		unsupported.Resolution = 64
		selected = append(selected, unsupported)
		clearMaps(a, sentinel)
		together(a, [][]core.ShadowUpdate{selected}, false)
		compare(a, selected, baseline, sentinel)
	})
	run("SameEncoderSameResolutionDisjointCallsAndEmptyCalls", func() {
		hero := subset(updates, func(u core.ShadowUpdate) bool { return u.Resolution == 512 })
		require(len(hero) == 2, "fixture needs disjoint same-resolution lights")
		clearMaps(a, sentinel)
		together(a, [][]core.ShadowUpdate{nil, hero[:1], nil, hero[1:], nil}, false)
		compare(a, hero, baseline, sentinel)
	})
	run("CallerSliceReusePreservesEachDispatchSnapshot", func() {
		hero := subset(updates, func(u core.ShadowUpdate) bool { return u.Resolution == 512 })
		caller := []core.ShadowUpdate{hero[0]}
		clearMaps(a, sentinel)
		enc, e := a.Device.CreateCommandEncoder(nil)
		must(e)
		defer enc.Release()
		m.DispatchShadowPass(enc, caller)
		caller[0] = hero[1]
		m.DispatchShadowPass(enc, caller)
		caller[0] = updates[0]
		submit(a, enc)
		compare(a, hero, baseline, sentinel)
	})

	run("SameEncoderMixedCallsKeepEarlierPacketsAlive", func() {
		first := subset(updates, func(u core.ShadowUpdate) bool { return u.Resolution == 512 || u.Resolution == 256 })
		second := subset(updates, func(u core.ShadowUpdate) bool { return u.Resolution == 128 || u.Resolution == 1024 })
		clearMaps(a, sentinel)
		together(a, [][]core.ShadowUpdate{first, nil, second}, false)
		compare(a, updates, baseline, sentinel)
	})
	run("CapacityGrowthAndExplicitRebindingPreserveMixedPackets", func() {
		old := m.ShadowUpdatesBuf
		m.ShadowUpdatesBuf = nil
		m.CreateShadowBindGroups()
		defer old.Release()
		before := m.ShadowUpdatesBuf.GetSize()
		clearMaps(a, sentinel)
		together(a, [][]core.ShadowUpdate{updates}, true)
		require(m.ShadowUpdatesBuf.GetSize() > before, "fixture did not trigger update-buffer capacity growth")
		compare(a, updates, baseline, sentinel)
	})
	run("GrowthAfterEarlierEncodedDispatchKeepsOldDestinationAlive", func() {
		hero := subset(updates, func(u core.ShadowUpdate) bool { return u.Resolution == 512 })
		clearMaps(a, sentinel)
		enc, e := a.Device.CreateCommandEncoder(nil)
		must(e)
		defer enc.Release()
		m.DispatchShadowPass(enc, hero[:1])
		before := m.ShadowUpdatesBuf.GetSize()
		repeats := make([]core.ShadowUpdate, int(before/24)+1)
		for i := range repeats {
			repeats[i] = hero[1]
		}
		m.DispatchShadowPass(enc, repeats)
		require(m.ShadowUpdatesBuf.GetSize() > before, "fixture did not grow storage after earlier dispatch was encoded")
		submit(a, enc)
		compare(a, hero, baseline, sentinel)
	})
	run("UnsupportedOnlyCallBetweenValidCallsIsNoOp", func() {
		hero := subset(updates, func(u core.ShadowUpdate) bool { return u.Resolution == 512 })
		unsupported := updates[0]
		unsupported.ShadowLayer = m.ShadowMapLayers - 1
		unsupported.Resolution = 64
		clearMaps(a, sentinel)
		together(a, [][]core.ShadowUpdate{hero[:1], {unsupported}, hero[1:]}, false)
		compare(a, hero, baseline, sentinel)
	})

	run("ConsecutiveSubmitsDoNotReuseObsoleteRows", func() {
		full := subset(updates, func(u core.ShadowUpdate) bool { return u.Resolution == 512 })
		dispatch(a, full)
		only := full[1:]
		clearMaps(a, sentinel)
		together(a, [][]core.ShadowUpdate{only}, false)
		compare(a, only, baseline, sentinel)
	})
	run("GeometryChangeMixedBatchMatchesFreshIndependentReference", func() {
		for _, o := range a.Scene.Objects {
			volume.Cube(o.XBrickMap, mgl32.Vec3{0, 4, 0}, mgl32.Vec3{7, 7, 7}, 0)
		}
		glfw.PollEvents()
		a.Update()
		require(m.VoxelUploadBytes > 0, "fixture geometry change did not upload")
		changed := reference(a, updates, sentinel)
		for _, u := range updates {
			require(!bytes.Equal(changed[u.ShadowLayer], baseline[u.ShadowLayer]), fmt.Sprintf("fixture geometry change did not alter layer %d", u.ShadowLayer))
		}
		// Restore warm old maps, so missed buckets retain a meaningful stale image.
		clearMaps(a, sentinel)
		for _, u := range updates {
			must(a.Queue.WriteTexture(&wgpu.ImageCopyTexture{Texture: m.ShadowMapArray, Origin: wgpu.Origin3D{Z: u.ShadowLayer}}, baseline[u.ShadowLayer], &wgpu.TextureDataLayout{BytesPerRow: u.Resolution * 16, RowsPerImage: u.Resolution}, &wgpu.Extent3D{Width: u.Resolution, Height: u.Resolution, DepthOrArrayLayers: 1}))
		}
		together(a, [][]core.ShadowUpdate{updates}, false)
		compare(a, updates, changed, sentinel)
	})
	if failures != 0 {
		fmt.Printf("FAIL: %d native mixed-resolution publication regressions\n", failures)
		os.Exit(1)
	}
	fmt.Println("PASS: all native mixed-resolution shadow publication regressions")
}
