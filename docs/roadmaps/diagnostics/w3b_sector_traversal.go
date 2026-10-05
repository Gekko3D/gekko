//go:build ignore

// W3b native sector traversal regression. Uses production opaque main,
// transparent fragment and shadow traverse_scene without changing their bodies.
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"math"
	"os"
	"path/filepath"
	"runtime"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func require(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func submit(a *app.App, encoder *wgpu.CommandEncoder) {
	command, err := encoder.Finish(nil)
	must(err)
	a.Queue.Submit(command)
	command.Release()
	a.Device.Poll(true, nil)
}
func mapped(a *app.App, buffer *wgpu.Buffer, size uint64) []byte {
	done := false
	buffer.MapAsync(wgpu.MapModeRead, 0, size, func(status wgpu.BufferMapAsyncStatus) {
		require(status == wgpu.BufferMapAsyncStatusSuccess, "readback map failed")
		done = true
	})
	a.Device.Poll(true, nil)
	require(done, "readback did not finish")
	data := append([]byte(nil), buffer.GetMappedRange(0, uint(size))...)
	buffer.Unmap()
	return data
}
func readBuffer(a *app.App, source *wgpu.Buffer, offset, size uint64) []byte {
	buffer, err := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	must(err)
	defer buffer.Release()
	encoder, err := a.Device.CreateCommandEncoder(nil)
	must(err)
	defer encoder.Release()
	encoder.CopyBufferToBuffer(source, offset, buffer, 0, size)
	submit(a, encoder)
	return mapped(a, buffer, size)
}

func warm(a *app.App) {
	for frame := 0; frame < 512; frame++ {
		glfw.PollEvents()
		// GLFW's 65 logical pixels can become 130 physical pixels on HiDPI.
		// An odd physical target places the center pixel exactly on cameraForward.
		if a.Config.Width != 65 || a.Config.Height != 65 {
			a.Resize(65, 65)
		}
		require(a.Config.Width == 65 && a.Config.Height == 65, "fixture render dimensions must stay odd physical pixels")
		a.Update()

		a.Render()
		a.Device.Poll(true, nil)
		ready := true
		for _, o := range a.Scene.Objects {
			ok, _, _ := a.BufferManager.RenderVoxelObjectReady(o, o.RenderVoxelMap(), o.RenderVoxelMap().Revision)
			ready = ready && ok
		}
		if ready && !a.BufferManager.VoxelGPUWorkStats().Pending {
			return
		}
	}
	panic("W3b fixture never became ready")
}
func textureReadback(a *app.App, view *wgpu.TextureView) []byte {
	return textureReadbackSize(a, view, a.Config.Width, a.Config.Height)
}
func textureReadbackSize(a *app.App, view *wgpu.TextureView, width, height uint32) []byte {
	code := `@group(0) @binding(0) var source:texture_2d<f32>;
 @group(0) @binding(1) var<storage,read_write> pixels:array<vec4<f32>>;
 @compute @workgroup_size(8,8)
 fn w3b_pixels(@builtin(global_invocation_id) id:vec3<u32>) {
 let dims=textureDimensions(source);if(id.x>=dims.x||id.y>=dims.y){return;}
 pixels[id.y*dims.x+id.x]=textureLoad(source,vec2<i32>(id.xy),0);
 }`
	module, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "W3b textureLoad readback", WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: code}})
	must(err)
	defer module.Release()
	layout, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []wgpu.BindGroupLayoutEntry{
		{Binding: 0, Visibility: wgpu.ShaderStageCompute, Texture: wgpu.TextureBindingLayout{SampleType: wgpu.TextureSampleTypeUnfilterableFloat, ViewDimension: wgpu.TextureViewDimension2D}},
		{Binding: 1, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeStorage}},
	}})
	must(err)
	defer layout.Release()
	pipelineLayout, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{layout}})
	must(err)
	defer pipelineLayout.Release()
	pipeline, err := a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Layout: pipelineLayout, Compute: wgpu.ProgrammableStageDescriptor{Module: module, EntryPoint: "w3b_pixels"}})
	must(err)
	defer pipeline.Release()
	size := uint64(width) * uint64(height) * 16
	output, err := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Size: size, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc})
	must(err)
	defer output.Release()
	group, err := a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: layout, Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: view}, {Binding: 1, Buffer: output, Size: wgpu.WholeSize}}})
	must(err)
	defer group.Release()
	encoder, err := a.Device.CreateCommandEncoder(nil)
	must(err)
	defer encoder.Release()
	pass := encoder.BeginComputePass(nil)
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, group, nil)
	pass.DispatchWorkgroups((width+7)/8, (height+7)/8, 1)
	pass.End()
	pass.Release()
	submit(a, encoder)
	return readBuffer(a, output, 0, size)
}

// A single explicit ray isolates the production shadow scene walk from
// unrelated shadow atlas projection/cadence. The production function is intact.
func shadowProbe(a *app.App, origin mgl32.Vec3, direction mgl32.Vec3) [4]float32 {
	origin = origin.Sub(a.BufferManager.RenderOrigin)
	code := shaders.ShadowMapWGSL + fmt.Sprintf(`
 @group(3) @binding(15) var<storage,read_write> result:array<vec4<f32>>;
 @compute @workgroup_size(1) fn w3b_shadow() {
 let d=vec3<f32>(%.9g,%.9g,%.9g);
 let ray=Ray(vec3<f32>(%f,%f,%f),d,1.0/d);
 let h=traverse_scene(ray); result[0]=vec4<f32>(select(0.0,1.0,h.hit),h.t,h.hit_pos_ws.xy);
 }`, direction[0], direction[1], direction[2], origin[0], origin[1], origin[2])
	module, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: code}})
	must(err)
	defer module.Release()
	pipeline, err := a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Compute: wgpu.ProgrammableStageDescriptor{Module: module, EntryPoint: "w3b_shadow"}})
	must(err)
	defer pipeline.Release()
	output, err := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Size: 16, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc})
	must(err)
	defer output.Release()
	m := a.BufferManager
	entries := []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize}, {Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
		{Binding: 6, Buffer: m.MaterialBuf, Size: wgpu.WholeSize}, {Binding: 7, Buffer: m.ShadowObjectParamsBuf, Size: wgpu.WholeSize},
		{Binding: 9, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize}, {Binding: 10, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize},
		{Binding: 11, Buffer: m.DirectSectorLookupBuf, Size: wgpu.WholeSize}, {Binding: 13, Buffer: m.DenseOccupancyBuf, Size: wgpu.WholeSize},
	}
	for i := uint32(0); i < 4; i++ {
		entries = append(entries, wgpu.BindGroupEntry{Binding: 2 + i, TextureView: m.VoxelPayloadView[i]})
	}
	groups := make([]*wgpu.BindGroup, 4)
	groups[0], err = a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(0), Entries: []wgpu.BindGroupEntry{{Binding: 1, Buffer: m.ShadowInstancesBuf, Size: wgpu.WholeSize}, {Binding: 2, Buffer: m.ShadowBVHNodesBuf, Size: wgpu.WholeSize}}})
	must(err)
	groups[1], err = a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(1)})
	must(err)
	groups[2], err = a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(2), Entries: entries})
	must(err)
	groups[3], err = a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(3), Entries: []wgpu.BindGroupEntry{{Binding: 15, Buffer: output, Size: wgpu.WholeSize}}})
	must(err)
	for _, g := range groups {
		defer g.Release()
	}
	encoder, err := a.Device.CreateCommandEncoder(nil)
	must(err)
	defer encoder.Release()
	pass := encoder.BeginComputePass(nil)
	pass.SetPipeline(pipeline)
	for i, g := range groups {
		pass.SetBindGroup(uint32(i), g, nil)
	}
	pass.DispatchWorkgroups(1, 1, 1)
	pass.End()
	pass.Release()
	submit(a, encoder)
	raw := readBuffer(a, output, 0, 16)
	return decodePixel(raw)
}
func decodePixel(raw []byte) (p [4]float32) {
	for i := range p {
		p[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return
}
func center(a *app.App, view *wgpu.TextureView) [4]float32 {
	raw := textureReadback(a, view)
	offset := (int(a.Config.Height/2)*int(a.Config.Width) + int(a.Config.Width/2)) * 16
	return decodePixel(raw[offset:])
}

// Float32 world-distance parity; the same occupied cells and transforms are
// compared, but tight AABB entry and accumulated boundary arithmetic may differ.
const depthTolerance = float32(0.02)
const accumulationTolerance = float32(0.005)
const farSector = 600

type capture struct{ Depth, Material, Albedo, Transparent, Shadow [4]float32 }
type proof struct {
	LocalMin, LocalMax, Scale, Origin, LocalOrigin, LocalDirection mgl32.Vec3
	StartSector, HitSector, SectorsBeforeHit, ProvenOldZSteps      int
	LiveSectors                                                    int
	TreeDisabled                                                   bool
}
type outcome struct {
	Name            string
	Pass            bool
	Detail          string
	Control, Actual capture
	Proof           proof
}

func close(a, b float32, tolerance float32) bool {
	return !math.IsNaN(float64(a)) && !math.IsNaN(float64(b)) && !math.IsInf(float64(a), 0) && !math.IsInf(float64(b), 0) && float32(math.Abs(float64(a-b))) <= tolerance
}
func captureNow(a *app.App, origin mgl32.Vec3, direction mgl32.Vec3) capture {
	c := capture{Depth: center(a, a.BufferManager.DepthView), Material: center(a, a.BufferManager.MaterialView), Transparent: center(a, a.BufferManager.TransparentAccumView), Shadow: shadowProbe(a, origin, direction)}
	if c.Depth[0] > 0 && c.Depth[0] < a.Camera.Far {
		c.Albedo = decodePixel(readBuffer(a, a.BufferManager.MaterialBuf, uint64(c.Material[3])*16, 16))
	}
	return c
}
func reset(a *app.App, o *core.VoxelObject) {
	for len(a.Scene.Objects) > 0 {
		a.Scene.RemoveObject(a.Scene.Objects[len(a.Scene.Objects)-1])
	}
	a.Scene.AddObject(o)
	warm(a)
}
func sectorObject(glass, long, hit bool, direction int) *core.VoxelObject {
	o := core.NewVoxelObject()
	o.Transform.Scale = mgl32.Vec3{2, 1.5, 1.0 / 32}
	o.LODThreshold = 1e9
	o.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial([4]uint8{190, 110, 50, 255}, [4]uint8{})}
	if glass {
		o.MaterialTable[1].Transparency = .45
		o.MaterialTable[1].Transmission = .2
	}
	z := direction*farSector*32 + 7
	near := z - 7
	if long {
		near = 0
	}
	// Opposite off-ray corners create actual authored tight bounds, not synthetic
	// instance edits. Neither anchor overlaps the central X/Y ray.
	o.XBrickMap.SetVoxel(-8, -8, near, 1)
	o.XBrickMap.SetVoxel(-1, -1, z+7, 1)
	if hit {
		for x := -5; x <= -4; x++ {
			for y := -5; y <= -4; y++ {
				o.XBrickMap.SetVoxel(x, y, z, 1)
			}
		}
	}
	return o
}

// CPU coordinate proof is independent of the proposed shader DDA helper. The
// near-axis ray crosses every integer Z sector between actual uploaded local bounds
// and the occupied central plane; no sector lookup count is used as a substitute.
func uploadedProof(a *app.App, o *core.VoxelObject, glass bool, origin mgl32.Vec3, direction int) proof {
	m := a.BufferManager
	instances, params := m.InstancesBuf, m.ObjectParamsBuf
	selected := a.Scene.VisibleObjects
	if glass {
		instances, params = m.TransparentInstancesBuf, m.TransparentObjectParamsBuf
		selected = a.Scene.TransparentVisibleObjects
	}
	require(len(selected) == 1 && selected[0] == o, "single fixture was not selected")
	raw := readBuffer(a, instances, 0, 208)
	lmin, lmax := decodePixel(raw[160:]), decodePixel(raw[176:])
	expectedMin, expectedMax := o.RenderVoxelMap().ComputeAABB()
	p := proof{Scale: o.Transform.Scale, Origin: origin, LiveSectors: len(o.RenderVoxelMap().Sectors)}
	for i := 0; i < 3; i++ {
		p.LocalMin[i] = lmin[i]
		p.LocalMax[i] = lmax[i]
		require(lmin[i] == expectedMin[i] && lmax[i] == expectedMax[i], "uploaded local bounds differ from fixture")
	}
	// Decode the actual GPU inverse transform, including render-origin rebasing.
	var inverse mgl32.Mat4
	for i := range inverse {
		inverse[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[64+i*4:]))
	}
	relative := origin.Sub(m.RenderOrigin)
	p.LocalOrigin = inverse.Mul4x1(relative.Vec4(1)).Vec3()
	p.LocalDirection = inverse.Mul4x1(a.Camera.GetForward().Vec4(0)).Vec3()
	require(p.LocalDirection[0] > 1e-6 && p.LocalDirection[1] > 1e-6 && close(p.LocalDirection[2], float32(direction)*32, .001), "GPU ray transform differs or normalizes world distance")
	if !glass {
		require(len(a.Scene.ShadowObjects) == 1 && a.Scene.ShadowObjects[0] == o, "shadow fixture was not selected")
		shadowInstance := readBuffer(a, m.ShadowInstancesBuf, 0, 208)
		for i := 64; i < 192; i++ {
			require(shadowInstance[i] == raw[i], "shadow inverse transform or bounds differ from opaque fixture")
		}
		shadowParams := readBuffer(a, m.ShadowObjectParamsBuf, 0, 128)
		require(int(binary.LittleEndian.Uint32(shadowParams[24:])) == p.LiveSectors, "shadow GPU sector count differs")
	}
	parameterData := readBuffer(a, params, 0, 128)
	p.TreeDisabled = binary.LittleEndian.Uint32(parameterData[16:]) == 0xffffffff && math.Float32frombits(binary.LittleEndian.Uint32(parameterData[20:])) == o.LODThreshold
	require(p.TreeDisabled, "opaque XBrickMap dispatch not forced")
	require(int(binary.LittleEndian.Uint32(parameterData[24:])) == p.LiveSectors, "GPU sector count differs")
	allocation := m.Allocations[o.RenderVoxelMap()]
	require(allocation != nil && len(allocation.Sectors) == p.LiveSectors, "GPU admitted map differs")
	for key, sector := range o.RenderVoxelMap().Sectors {
		require(allocation.Sectors[key] == sector, "admitted sector snapshot differs")
		info, ok := m.SectorToInfo[sector]
		require(ok, "missing published sector")
		header := readBuffer(a, m.SectorTableBuf, uint64(info.SlotIndex)*32, 32)
		for axis := 0; axis < 3; axis++ {
			require(int32(binary.LittleEndian.Uint32(header[axis*4:])) == int32(key[axis]*32), "uploaded sector origin differs")
		}
	}
	// Start just inside the entry slab and target just inside the actual hit cell.
	entry := p.LocalMin[2] + .001
	if direction < 0 {
		entry = p.LocalMax[2] - .001
	}
	p.StartSector = int(math.Floor(float64(entry) / 32))
	p.HitSector = int(math.Floor(float64(direction*farSector*32+7) / 32))
	p.SectorsBeforeHit = int(math.Abs(float64(p.HitSector - p.StartSector)))
	// Reproduce only OLD coordinate next-axis selection to demonstrate these
	// regressions exceed the numeric caps rather than stalling on zero axes.
	tEntry := (entry - p.LocalOrigin[2]) / p.LocalDirection[2]
	at := p.LocalOrigin.Add(p.LocalDirection.Mul(tEntry))
	var cell [3]int
	var next, delta [3]float32
	for axis := 0; axis < 3; axis++ {
		cell[axis] = int(math.Floor(float64(at[axis]) / 32))
		upper := float32(32)
		if p.LocalDirection[axis] < 0 {
			upper = 0
		}
		next[axis] = (float32(cell[axis]*32) + upper - p.LocalOrigin[axis]) / p.LocalDirection[axis]
		delta[axis] = float32(math.Abs(float64(32 / p.LocalDirection[axis])))
	}
	for cell[2] != p.HitSector {
		axis := 2
		if next[0] < next[1] {
			if next[0] < next[2] {
				axis = 0
			}
		} else if next[1] < next[2] {
			axis = 1
		}
		require(axis == 2, "old arithmetic advances a stationary or transverse axis before target; cap proof invalid")
		cell[axis] += direction
		next[axis] += delta[axis]
		p.ProvenOldZSteps++
		require(p.ProvenOldZSteps < 1024, "coordinate proof did not terminate")
	}
	require(p.ProvenOldZSteps == p.SectorsBeforeHit, "coordinate proof differs from bounds crossing count")
	return p
}
func opaqueParity(c, a capture) bool {
	if !(c.Depth[0] > 0 && c.Depth[0] < 1000 && a.Depth[0] > 0 && a.Depth[0] < 1000 && close(c.Depth[0], a.Depth[0], depthTolerance) && c.Albedo == a.Albedo) {
		return false
	}
	for i := 0; i < 3; i++ {
		if !close(c.Material[i], a.Material[i], .001) {
			return false
		}
	}
	return true
}
func transparentParity(c, a capture) bool {
	if !(c.Transparent[3] > 0 && a.Transparent[3] > 0) {
		return false
	}
	for i := range c.Transparent {
		if !close(c.Transparent[i], a.Transparent[i], accumulationTolerance) {
			return false
		}
	}
	return true
}
func shadowParity(c, a capture) bool {
	return c.Shadow[0] == 1 && a.Shadow[0] == 1 && close(c.Shadow[1], a.Shadow[1], depthTolerance) && close(c.Shadow[2], a.Shadow[2], .001) && close(c.Shadow[3], a.Shadow[3], .001)
}
func main() {
	output := flag.String("output", "/tmp/w3b-sector-traversal", "native results directory")
	flag.Parse()
	must(os.MkdirAll(*output, 0755))
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	window, err := glfw.CreateWindow(65, 65, "W3b sector traversal", nil, nil)
	must(err)
	defer window.Destroy()
	a := app.NewApp(window)
	a.FeatureConfig = app.AppFeatureConfig{AutoRegisterDefaults: false}
	a.RegisterFeature(&app.TransparencyFeature{})
	must(a.Init())
	defer a.Shutdown()
	glfw.PollEvents()
	a.Resize(65, 65)
	a.OcclusionMode = core.OcclusionOff
	a.Camera.Far = 1000
	results := []outcome{}
	for _, direction := range []int{-1, 1} {
		origin := mgl32.Vec3{-9, -6.75, -float32(direction) * 20}
		a.Camera.Position = origin
		a.Camera.LookAt = origin.Add(mgl32.Vec3{.0001, .0001, float32(direction)})
		a.Scene.Lights = []core.Light{{Position: [4]float32{origin[0], origin[1], origin[2], 0}, Direction: [4]float32{0, 0, float32(direction), 0}, Color: [4]float32{1, 1, 1, 1}, Params: [4]float32{950, .4, float32(core.LightTypeSpot), 1}}}
		for _, glass := range []bool{false, true} {
			label := fmt.Sprintf("dir%+d", direction)
			if glass {
				label += "-transparent"
			} else {
				label += "-opaque"
			}
			short := sectorObject(glass, false, true, direction)
			reset(a, short)
			sp := uploadedProof(a, short, glass, origin, direction)
			control := captureNow(a, origin, a.Camera.GetForward())
			controlOK := control.Depth[0] > 0 && control.Depth[0] < 1000 && control.Shadow[0] == 1
			if glass {
				controlOK = control.Transparent[3] > 0
			}
			results = append(results, outcome{Name: label + "-short-positive-control", Pass: controlOK, Actual: control, Proof: sp})
			long := sectorObject(glass, true, true, direction)
			reset(a, long)
			lp := uploadedProof(a, long, glass, origin, direction)
			require(lp.SectorsBeforeHit > 512, "long fixture does not cross all production caps")
			actual := captureNow(a, origin, a.Camera.GetForward())
			if glass {
				results = append(results, outcome{Name: label + "-late-hit", Pass: transparentParity(control, actual), Detail: "Production transparent fragment WBOIT accumulation matches same-world-hit short control", Control: control, Actual: actual, Proof: lp})
			} else {
				results = append(results, outcome{Name: label + "-late-hit", Pass: opaqueParity(control, actual), Detail: "Production G-buffer depth/material metadata/palette match same-world-hit short control", Control: control, Actual: actual, Proof: lp})
				results = append(results, outcome{Name: fmt.Sprintf("dir%+d-shadow-late-hit", direction), Pass: shadowParity(control, actual), Detail: "Intact production shadow traverse_scene hit/distance/position match same-world-hit control", Control: control, Actual: actual, Proof: lp})
			}
			miss := sectorObject(glass, true, false, direction)
			reset(a, miss)
			mp := uploadedProof(a, miss, glass, origin, direction)
			empty := captureNow(a, origin, a.Camera.GetForward())
			missOK := empty.Depth[0] >= 1000 && empty.Shadow[0] == 0 && empty.Transparent[3] == 0
			results = append(results, outcome{Name: label + "-long-all-miss", Pass: missOK, Actual: empty, Proof: mp})
		}
	}
	// Exact zero transverse axes and an exact sector boundary are isolated
	// termination controls; the independent short hit below also checks nested
	// brick/micro traversal preserves the physical first hit.
	for _, glass := range []bool{false, true} {
		o := core.NewVoxelObject()
		o.Transform.Scale = mgl32.Vec3{2, 1.5, 1.0 / 32}
		o.LODThreshold = 1e9
		o.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial([4]uint8{190, 110, 50, 255}, [4]uint8{})}
		if glass {
			o.MaterialTable[1].Transparency = .45
			o.MaterialTable[1].Transmission = .2
		}
		o.XBrickMap.SetVoxel(-4, -4, 0, 1)
		o.XBrickMap.SetVoxel(3, 3, -farSector*32, 1)
		origin := mgl32.Vec3{0, 0, 20}
		a.Camera.Position = origin
		a.Camera.LookAt = mgl32.Vec3{0, 0, 0}
		reset(a, o)
		c := captureNow(a, origin, mgl32.Vec3{0, 0, -1})
		label := "opaque"
		if glass {
			label = "transparent"
		}
		results = append(results, outcome{Name: label + "-zero-axes-sector-boundary-long-miss", Pass: c.Depth[0] >= 1000 && c.Shadow[0] == 0 && c.Transparent[3] == 0, Detail: "Exact zero X/Y and X/Y=0 sector boundaries terminate and miss off-ray sparse anchors", Actual: c})
	}
	// This fixture has an analytically known front face at z=8. It must not
	// use another rendered fixture as its distance oracle: both controls could
	// otherwise share a stationary-axis traversal error and agree incorrectly.
	for _, glass := range []bool{false, true} {
		o := core.NewVoxelObject()
		o.LODThreshold = 1e9
		o.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial([4]uint8{190, 110, 50, 255}, [4]uint8{})}
		if glass {
			o.MaterialTable[1].Transparency = .45
			o.MaterialTable[1].Transmission = .2
		}
		o.XBrickMap.SetVoxel(0, 0, 0, 1)
		o.XBrickMap.SetVoxel(7, 7, 7, 1)
		for x := 3; x <= 4; x++ {
			for y := 3; y <= 4; y++ {
				o.XBrickMap.SetVoxel(x, y, 7, 1)
			}
		}
		origin := mgl32.Vec3{3.5, 3.5, 20}
		a.Camera.Position = origin
		a.Camera.LookAt = mgl32.Vec3{3.5, 3.5, 0}
		a.Scene.Lights = []core.Light{{Position: [4]float32{3.5, 3.5, 20, 0}, Direction: [4]float32{0, 0, -1, 0}, Color: [4]float32{1, 1, 1, 1}, Params: [4]float32{950, .4, float32(core.LightTypeSpot), 1}}}
		reset(a, o)
		c := captureNow(a, origin, mgl32.Vec3{0, 0, -1})
		if glass {
			finite := true
			for _, value := range c.Transparent {
				finite = finite && !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
			}
			results = append(results, outcome{Name: "transparent-zero-axes-short-analytic-hit", Pass: finite && c.Transparent[3] > 0 && c.Transparent[0]+c.Transparent[1]+c.Transparent[2] > 0, Detail: "Exact zero X/Y ray through authored z=7 plane produces finite nonempty production WBOIT accumulation", Actual: c})
		} else {
			allocation := a.BufferManager.MaterialAllocations[o]
			require(allocation != nil, "analytic fixture material palette was not published")
			// MaterialView stores metadata, with lane W indexing 16-byte
			// material words. Each authored material occupies four words.
			validSurface := c.Material[3] == float32(allocation.MaterialOffset*4+4)
			for i, channel := range o.MaterialTable[1].BaseColor {
				validSurface = validSurface && close(c.Albedo[i], float32(channel)/255, 1e-6)
			}
			results = append(results, outcome{Name: "opaque-shadow-zero-axes-short-analytic-hit", Pass: close(c.Depth[0], 12.001, .002) && validSurface && c.Shadow[0] == 1 && close(c.Shadow[1], 12, .002), Detail: "Independent front-face distance 20-(7+1)=12; production G-buffer depth is 12+EPS with correct published material word index and authored palette color, and shadow first hit is 12 within .002", Actual: c})
		}
	}
	raw, err := json.MarshalIndent(struct {
		DepthTolerance, AccumulationTolerance float32
		Results                               []outcome
	}{depthTolerance, accumulationTolerance, results}, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(*output, "results.json"), raw, 0644))
	failed := false
	for _, r := range results {
		status := "PASS"
		if !r.Pass {
			status = "FAIL"
			failed = true
		}
		fmt.Printf("%s %s sectorsBeforeHit=%d depth=%g shadow=%v WBOIT=%v\n", status, r.Name, r.Proof.SectorsBeforeHit, r.Actual.Depth[0], r.Actual.Shadow, r.Actual.Transparent)
	}
	if failed {
		os.Exit(1)
	}
}
