//go:build ignore

// Native point-face dependency/scheduling regression. Run from engine cwd.
// Real App.Update uploads, manager dispatch, serialized readiness and GPU pixels.
// Uses the established R2e/R2h native map readback helpers.
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
	b, e := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: "R2i map readback", Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
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
	tex, e := a.Device.CreateTexture(&wgpu.TextureDescriptor{Label: "R2i diagnostic shadow map", Size: wgpu.Extent3D{Width: 1024, Height: 1024, DepthOrArrayLayers: m.ShadowMapLayers}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA32Float, Usage: wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopySrc | wgpu.TextureUsageCopyDst})
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

func point(updates []core.ShadowUpdate) []core.ShadowUpdate {
	return subset(updates, func(u core.ShadowUpdate) bool { return u.LightIndex == 0 && u.Kind == core.ShadowUpdateKindPoint })
}

func capture(a *app.App, layers []core.ShadowUpdate) map[uint32][]byte {
	out := make(map[uint32][]byte)
	for _, u := range layers {
		out[u.CascadeIndex] = readMap(a, u.ShadowLayer, u.Resolution)
	}
	return out
}

func ready(a *app.App, layers []core.ShadowUpdate, expected bool) {
	// Publish through the production serializer, then inspect its actual GPU bytes.
	a.BufferManager.PrepareShadowLights(a.Scene, layers)
	b, err := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: "R2i readiness readback", Size: 80, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	must(err)
	defer b.Release()
	enc, err := a.Device.CreateCommandEncoder(nil)
	must(err)
	defer enc.Release()
	enc.CopyBufferToBuffer(a.BufferManager.LightsBuf, 0, b, 0, 80)
	submit(a, enc)
	data := mapped(a, b, 80)
	enabled := math.Float32frombits(binary.LittleEndian.Uint32(data[60:64]))
	count := binary.LittleEndian.Uint32(data[68:72])
	if expected {
		require(enabled == 1 && count == 6, fmt.Sprintf("six current faces must serialize ready: enabled=%g layers=%d", enabled, count))
	} else {
		require(enabled == 0 && count == 0, fmt.Sprintf("pending face must disable sampling: enabled=%g layers=%d", enabled, count))
	}
}

func faceSet(faces ...uint32) map[uint32]bool {
	out := make(map[uint32]bool)
	for _, f := range faces {
		out[f] = true
	}
	return out
}

func expectFaces(updates []core.ShadowUpdate, want map[uint32]bool, name string) {
	got := faceSet()
	for _, u := range point(updates) {
		require(!got[u.CascadeIndex], name+": duplicate face")
		got[u.CascadeIndex] = true
	}
	fmt.Printf("%s: point faces=%v expected=%v\n", name, got, want)
	require(len(got) == len(want), name+": scheduler must dispatch only dirty faces")
	for f := range got {
		require(want[f], fmt.Sprintf("%s: clean face %d scheduled", name, f))
	}
}

func idle(a *app.App, frame uint64, layers []core.ShadowUpdate) {
	expectFaces(a.BufferManager.BuildShadowUpdates(a.Scene, a.Camera, frame, false), faceSet(), "idle")
	ready(a, layers, true)
}

func parity(a *app.App, layers []core.ShadowUpdate, before map[uint32][]byte, affected map[uint32]bool, frame uint64) {
	cached := capture(a, layers)
	changed := false
	for _, u := range layers {
		equal := bytes.Equal(cached[u.CascadeIndex], before[u.CascadeIndex])
		if affected[u.CascadeIndex] {
			changed = changed || !equal
		} else {
			require(equal, fmt.Sprintf("untouched native face %d changed", u.CascadeIndex))
		}
	}
	require(changed, "dependent change must alter at least one genuine native pixel")
	// Manual all-six records bypass scheduler filtering, so a fresh reference
	// cannot accidentally share the same selective-invalidation bug.
	clearActive(a, layers)
	dispatch(a, layers, frame)
	for _, u := range layers {
		require(bytes.Equal(cached[u.CascadeIndex], readMap(a, u.ShadowLayer, u.Resolution)), fmt.Sprintf("cached face %d differs from freshly traced native reference", u.CascadeIndex))
	}
}

func drain(a *app.App, layers []core.ShadowUpdate, pending map[uint32]bool, frame *uint64) {
	for len(pending) > 0 {
		ready(a, layers, false)
		updates := a.BufferManager.BuildShadowUpdates(a.Scene, a.Camera, *frame, false)
		p := point(updates)
		require(len(p) > 0 && len(p) <= 3, "hero dirty work must respect three-face budget and make progress")
		for _, u := range p {
			require(pending[u.CascadeIndex], fmt.Sprintf("drain repeated clean face %d", u.CascadeIndex))
			delete(pending, u.CascadeIndex)
		}
		dispatch(a, updates, *frame)
		*frame++
		ready(a, layers, len(pending) == 0)
	}
	idle(a, *frame, layers)
}

func edit(a *app.App, obj *core.VoxelObject) {
	volume.Cube(obj.XBrickMap, mgl32.Vec3{0, 4, 0}, mgl32.Vec3{7, 7, 7}, 0)
	a.Update()
	require(a.BufferManager.VoxelUploadBytes > 0, "geometry edit did not reach real GPU upload")
}

func main() {
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	win, err := glfw.CreateWindow(640, 480, "R2i point face reuse", nil, nil)
	must(err)
	defer win.Destroy()
	a := app.NewApp(win)
	a.FeatureConfig = app.AppFeatureConfig{AutoRegisterDefaults: false}
	must(a.Init())
	defer a.Shutdown()
	a.Camera.Position = mgl32.Vec3{0, 0, 15}
	a.Camera.LookAt = mgl32.Vec3{}
	a.OcclusionMode = core.OcclusionOff
	// Shader face directions: 0 +X, 1 -X, 2 +Y, 3 -Y, 4 +Z, 5 -Z.
	centers := []mgl32.Vec3{{8, 0, 0}, {-8, 0, 0}, {0, 8, 0}, {0, -8, 0}, {0, 0, 8}, {0, 0, -8}}
	var casters []*core.VoxelObject
	for _, center := range centers {
		o := cube(0)
		o.Transform.Position = center.Sub(mgl32.Vec3{1, 1, 1})
		o.Transform.Dirty = true
		casters = append(casters, o)
		a.Scene.AddObject(o)
	}
	// Another light selects a remote caster beyond point range. Point rays are
	// unbounded across the complete selected ShadowObjects set, not range-capped.
	remote := cube(0)
	remote.Transform.Position = mgl32.Vec3{23, 5, -1}
	remote.Transform.Dirty = true
	a.Scene.AddObject(remote)
	a.Scene.Lights = []core.Light{
		{Color: [4]float32{1, 1, 1, 1}, Params: [4]float32{20, 0, float32(core.LightTypePoint), 1}},
		{Position: [4]float32{24, 12, 0, 0}, Direction: [4]float32{0, -1, 0, 0}, Color: [4]float32{1, 1, 1, 1}, Params: [4]float32{12, .8, float32(core.LightTypeSpot), 1}},
	}
	glfw.PollEvents()
	a.Update()
	m := a.BufferManager
	installReadbackMap(a)
	var layers []core.ShadowUpdate
	for _, p := range m.ShadowLayerParams {
		if p.LightIndex == 0 {
			require(p.Kind == core.ShadowUpdateKindPoint && p.Tier == core.ShadowTierHero, "fixture needs hero point faces")
			layers = append(layers, core.ShadowUpdate{LightIndex: p.LightIndex, ShadowLayer: p.Layer, CascadeIndex: p.CascadeIndex, Kind: p.Kind, Tier: p.Tier, Resolution: p.EffectiveResolution})
		}
	}
	require(len(layers) == 6 && len(a.Scene.ShadowObjects) == 7, "fixture requires six point faces and all seven selected casters")
	frame := uint64(0)
	drain(a, layers, faceSet(0, 1, 2, 3, 4, 5), &frame)
	for _, u := range layers {
		requireHit(readMap(a, u.ShadowLayer, u.Resolution), u, a)
	}
	baseline := capture(a, layers)
	fmt.Println("Warm fixture: all six point maps have real axis-caster hits; native readiness true; idle zero")
	// The remote angular footprint misses the near +X cube; assert genuine
	// beyond-range hits so selection/dependency coverage is not merely synthetic.
	remoteHit := false
	for offset := 0; offset < len(baseline[0]); offset += 16 {
		depth := math.Float32frombits(binary.LittleEndian.Uint32(baseline[0][offset:]))
		remoteHit = remoteHit || (depth > 20 && depth < 30)
	}
	require(remoteHit, "point shader fixture lacks selected beyond-range caster hit")

	edit(a, casters[0])
	ready(a, layers, false)
	updates := m.BuildShadowUpdates(a.Scene, a.Camera, frame, false)
	// Baseline RED: global per-light generation schedules budget faces here.
	expectFaces(updates, faceSet(0), "+X edit")
	dispatch(a, updates, frame)
	frame++
	idle(a, frame, layers)
	parity(a, layers, baseline, faceSet(0), frame)

	for _, index := range []int{5, 1, 3, 4, 2} {
		before := capture(a, layers)
		if index == 2 {
			// +Y rays hit the near local-Y slab first. Removing the far
			// half leaves every first-hit voxel unchanged in this fixture.
			volume.Cube(casters[index].XBrickMap, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{7, 3, 7}, 0)
			a.Update()
			require(m.VoxelUploadBytes > 0, "+Y near-half geometry edit did not upload")
		} else {
			edit(a, casters[index])
		}
		ready(a, layers, false)
		updates = m.BuildShadowUpdates(a.Scene, a.Camera, frame, false)
		expectFaces(updates, faceSet(uint32(index)), fmt.Sprintf("axis face%d edit", index))
		dispatch(a, updates, frame)
		frame++
		idle(a, frame, layers)
		parity(a, layers, before, faceSet(uint32(index)), frame)
	}
	before := capture(a, layers)
	edit(a, remote)
	ready(a, layers, false)
	updates = m.BuildShadowUpdates(a.Scene, a.Camera, frame, false)
	expectFaces(updates, faceSet(0), "selected beyond-range edit")
	dispatch(a, updates, frame)
	frame++
	idle(a, frame, layers)
	parity(a, layers, before, faceSet(0), frame)

	// Move to the +X/+Y seam, then across it. Old and new footprints both
	// invalidate; geometry changes within the seam affect both intersecting faces.
	for index, center := range []mgl32.Vec3{{8, 8, 0}, {0, 8, 0}} {
		before = capture(a, layers)
		casters[0].Transform.Position = center.Sub(mgl32.Vec3{1, 1, 1})
		casters[0].Transform.Dirty = true
		a.Update()
		ready(a, layers, false)
		updates = m.BuildShadowUpdates(a.Scene, a.Camera, frame, false)
		expectFaces(updates, faceSet(0, 2), "seam crossing old/new footprint")
		dispatch(a, updates, frame)
		frame++
		idle(a, frame, layers)
		parity(a, layers, before, faceSet(0, 2), frame)
		if index == 0 {
			before = capture(a, layers)
			volume.Cube(casters[0].XBrickMap, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{7, 3, 3}, 0)
			a.Update()
			require(m.VoxelUploadBytes > 0, "seam geometry edit did not upload")
			ready(a, layers, false)
			updates = m.BuildShadowUpdates(a.Scene, a.Camera, frame, false)
			expectFaces(updates, faceSet(0, 2), "geometry edit spanning +X/+Y seam")
			dispatch(a, updates, frame)
			frame++
			idle(a, frame, layers)
			parity(a, layers, before, faceSet(0, 2), frame)
		}
	}
	// Restore the isolated +X fixture before full-light and pending-edit cases.
	casters[0].Transform.Position = centers[0].Sub(mgl32.Vec3{1, 1, 1})
	casters[0].Transform.Dirty = true
	a.Update()
	drain(a, layers, faceSet(0, 2), &frame)
	for _, mutate := range []func(){
		func() { a.Scene.Lights[0].ShadowMeta[3] = 42 },
		func() { a.Scene.Lights[0].Position[3] = .25 },
		func() { a.Scene.Lights[0].Position[0] += .25 },
	} {
		mutate()
		a.Update()
		drain(a, layers, faceSet(0, 1, 2, 3, 4, 5), &frame)
		cached := capture(a, layers)
		clearActive(a, layers)
		dispatch(a, layers, frame)
		for _, u := range layers {
			require(bytes.Equal(cached[u.CascadeIndex], readMap(a, u.ShadowLayer, u.Resolution)), "full-light budget refresh differs from fresh reference")
		}
	}
	// Light mutation dirties six; record one budget, then edit an already
	// refreshed caster. Only its face and the remaining pending faces may run.
	a.Scene.Lights[0].Position[3] = .5
	a.Update()
	ready(a, layers, false)
	updates = m.BuildShadowUpdates(a.Scene, a.Camera, frame, false)
	p := point(updates)
	require(len(p) == 3, "pending fixture needs one hero budget")
	pending := faceSet(0, 1, 2, 3, 4, 5)
	for _, u := range p {
		delete(pending, u.CascadeIndex)
	}
	dispatch(a, updates, frame)
	frame++
	ready(a, layers, false)
	f := p[0].CascadeIndex
	// Use a different nonempty voxel slab after the first edit.
	volume.Cube(casters[f].XBrickMap, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{3, 3, 7}, 0)
	a.Update()
	require(m.VoxelUploadBytes > 0, "second pending edit did not upload")
	pending[f] = true
	drain(a, layers, pending, &frame)
	cached := capture(a, layers)
	clearActive(a, layers)
	dispatch(a, layers, frame)
	for _, u := range layers {
		require(bytes.Equal(cached[u.CascadeIndex], readMap(a, u.ShadowLayer, u.Resolution)), "second pending edit native parity failed")
	}
	fmt.Println("PASS: independently current point faces; exact selective work, native readiness and six-map fresh parity")
}
