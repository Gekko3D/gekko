//go:build ignore

// W3a native scene traversal regression. Uses production opaque main,
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

var packed, packedMaterials bool

func warm(a *app.App) {
	for frame := 0; frame < 512; frame++ {
		glfw.PollEvents()
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
	panic("W3a fixture never became ready")
}
func textureReadback(a *app.App, view *wgpu.TextureView) []byte {
	return textureReadbackSize(a, view, a.Config.Width, a.Config.Height)
}
func textureReadbackSize(a *app.App, view *wgpu.TextureView, width, height uint32) []byte {
	code := `@group(0) @binding(0) var source:texture_2d<f32>;
 @group(0) @binding(1) var<storage,read_write> pixels:array<vec4<f32>>;
 @compute @workgroup_size(8,8)
 fn w3a_pixels(@builtin(global_invocation_id) id:vec3<u32>) {
 let dims=textureDimensions(source);if(id.x>=dims.x||id.y>=dims.y){return;}
 pixels[id.y*dims.x+id.x]=textureLoad(source,vec2<i32>(id.xy),0);
 }`
	module, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "W3a textureLoad readback", WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: code}})
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
	pipeline, err := a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Layout: pipelineLayout, Compute: wgpu.ProgrammableStageDescriptor{Module: module, EntryPoint: "w3a_pixels"}})
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

// A single axis-aligned ray isolates the production shadow scene walk from
// unrelated shadow atlas projection/cadence. The production function is intact.
func shadowProbe(a *app.App, origin mgl32.Vec3) [4]float32 {
	origin = origin.Sub(a.BufferManager.RenderOrigin)
	code := shaders.ShadowMapWGSL + fmt.Sprintf(`
 @group(3) @binding(15) var<storage,read_write> result:array<vec4<f32>>;
 @compute @workgroup_size(1) fn w3a_shadow() {
 let d=vec3<f32>(0.0,0.0,-1.0);
 let ray=Ray(vec3<f32>(%f,%f,%f),d,vec3<f32>(1e20,1e20,-1.0));
 let h=traverse_scene(ray); result[0]=vec4<f32>(select(0.0,1.0,h.hit),h.t,h.hit_pos_ws.xy);
 }`, origin[0], origin[1], origin[2])
	module, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: code}})
	must(err)
	defer module.Release()
	pipeline, err := a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Compute: wgpu.ProgrammableStageDescriptor{Module: module, EntryPoint: "w3a_shadow"}})
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
func object(glass, hit bool) *core.VoxelObject {
	o := core.NewVoxelObject()
	o.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial([4]uint8{190, 110, 50, 255}, [4]uint8{})}
	if glass {
		o.MaterialTable[1].Transparency = .45
		o.MaterialTable[1].Transmission = .2
	}
	// Off-ray opposite corners keep every instance/node's broad bounds identical.
	o.XBrickMap.SetVoxel(0, 0, 0, 1)
	o.XBrickMap.SetVoxel(7, 7, 7, 1)
	if hit {
		for x := 3; x <= 4; x++ {
			for y := 3; y <= 4; y++ {
				o.XBrickMap.SetVoxel(x, y, 7, 1)
			}
		}
	}
	return o
}

// Reproduce only decoded tree candidate ordering, not voxel traversal. For the
// equal AABB entry distances production pushes left then right (right first).
// Return the leaf reached LAST and its exact 1-based visit position.
func lastLeaf(data []byte) (leaf, visits, maxStack int) {
	require(len(data) >= 64 && len(data)%64 == 0, "invalid fixture tree")
	stack := []int{0}
	seen := map[int]bool{}
	for len(stack) > 0 {
		if len(stack) > maxStack {
			maxStack = len(stack)
		}
		index := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		require(index >= 0 && index < len(data)/64 && !seen[index], "invalid/cyclic fixture tree")
		seen[index] = true
		visits++
		n := data[index*64:]
		left := int(int32(binary.LittleEndian.Uint32(n[32:])))
		right := int(int32(binary.LittleEndian.Uint32(n[36:])))
		count := int(int32(binary.LittleEndian.Uint32(n[44:])))
		if count > 0 {
			require(count == 1, "fixture needs one-item leaves")
			leaf = int(int32(binary.LittleEndian.Uint32(n[40:])))
		} else {
			stack = append(stack, left, right)
		}
	}
	require(len(seen) == len(data)/64, "fixture does not visit complete tree")
	return
}
func proveInstances(a *app.App, buffer *wgpu.Buffer, count int) {
	raw := readBuffer(a, buffer, 0, uint64(count*208))
	for i := 0; i < count; i++ {
		require(binary.LittleEndian.Uint32(raw[i*208+192:]) == uint32(i), "uploaded instance identity mismatch")
	}
}
func reset(a *app.App, objects []*core.VoxelObject) {
	for len(a.Scene.Objects) > 0 {
		a.Scene.RemoveObject(a.Scene.Objects[len(a.Scene.Objects)-1])
	}
	for _, o := range objects {
		a.Scene.AddObject(o)
	}
	warm(a)
}

type capture struct{ Depth, Material, Albedo, Transparent, Shadow [4]float32 }

func captureNow(a *app.App, origin mgl32.Vec3) capture {
	c := capture{Depth: center(a, a.BufferManager.DepthView), Material: center(a, a.BufferManager.MaterialView), Transparent: center(a, a.BufferManager.TransparentAccumView), Shadow: shadowProbe(a, origin)}
	if c.Depth[0] < 20 {
		c.Albedo = decodePixel(readBuffer(a, a.BufferManager.MaterialBuf, uint64(c.Material[3])*16, 16))
	}
	return c
}

// Prove actual uploaded live node prefix matches the scene-built topology;
// physical buffer capacity is deliberately excluded from the live node count.
func uploadedProof(a *app.App, source *wgpu.Buffer, scene []byte, label string, output string) (int, int, int) {
	raw := readBuffer(a, source, 0, uint64(len(scene)))
	for at := 0; at < len(scene); at += 64 {
		for field := 32; field < 48; field += 4 {
			require(binary.LittleEndian.Uint32(raw[at+field:]) == binary.LittleEndian.Uint32(scene[at+field:]), label+" uploaded topology/leaf indices drifted")
		}
	}
	// Identical actual GPU bounds prove every child has the same entry
	// distance, so production's equality branch really traverses right first.
	for at := 0; at < len(raw); at += 64 {
		for _, base := range []int{0, 16} {
			for axis := 0; axis < 3; axis++ {
				require(binary.LittleEndian.Uint32(raw[at+base+axis*4:]) == binary.LittleEndian.Uint32(raw[base+axis*4:]), label+" bounds are not identical; tie-order proof invalid")
			}
		}
	}
	rayZ := float32(20) - a.BufferManager.RenderOrigin[2]
	rootMinZ := math.Float32frombits(binary.LittleEndian.Uint32(raw[8:]))
	rootMaxZ := math.Float32frombits(binary.LittleEndian.Uint32(raw[24:]))
	require(rootMinZ < rootMaxZ && rootMaxZ < rayZ, label+" bounds are not ahead of ray")
	leaf, visits, stack := lastLeaf(raw)
	// Every broad-phase node contains the fixed central render-relative ray.
	x := float32(3.5) - a.BufferManager.RenderOrigin[0]
	y := float32(3.5) - a.BufferManager.RenderOrigin[1]
	for at := 0; at < len(raw); at += 64 {
		p := decodePixel(raw[at:])
		q := decodePixel(raw[at+16:])
		require(p[0] <= x && x <= q[0] && p[1] <= y && y <= q[1], label+" node fails central ray overlap")
	}
	must(os.WriteFile(filepath.Join(output, label+"-bvh.bin"), raw, 0644))
	return leaf, visits, stack
}

type outcome struct {
	Name                           string
	Pass                           bool
	Detail                         string
	Control, Actual                capture
	Visits, MaxStack, SelectedLeaf int
}

func main() {
	output := flag.String("output", "/tmp/w3a-scene-traversal", "native results directory")
	flag.Parse()
	must(os.MkdirAll(*output, 0755))
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	window, err := glfw.CreateWindow(65, 65, "W3a native traversal", nil, nil)
	must(err)
	defer window.Destroy()
	a := app.NewApp(window)
	a.FeatureConfig = app.AppFeatureConfig{AutoRegisterDefaults: false}
	a.RegisterFeature(&app.TransparencyFeature{})
	must(a.Init())
	defer a.Shutdown()
	a.Scene.Lights = []core.Light{{Position: [4]float32{3.5, 3.5, 20, 0}, Direction: [4]float32{0, 0, -1, 0}, Color: [4]float32{1, 1, 1, 1}, Params: [4]float32{70, .4, float32(core.LightTypeSpot), 1}}}
	a.OcclusionMode = core.OcclusionOff
	origin := mgl32.Vec3{3.5, 3.5, 20}
	a.Camera.Position = origin
	a.Camera.LookAt = mgl32.Vec3{3.5, 3.5, 0}
	results := []outcome{}
	for _, glass := range []bool{false, true} {
		name := "opaque-shadow"
		if glass {
			name = "transparent"
		}
		reset(a, []*core.VoxelObject{object(glass, true)})
		control := captureNow(a, origin)
		fmt.Printf("CONTROL glass=%v %+v\n", glass, control)
		if glass {
			require(control.Transparent[3] > 0, "transparent positive control produced no contribution")
		} else {
			require(control.Depth[0] > 0 && control.Depth[0] < 20 && control.Shadow[0] == 1, "opaque/shadow positive control failed")
		}
		objects := make([]*core.VoxelObject, 300)
		for i := range objects {
			objects[i] = object(glass, false)
		}
		reset(a, objects)
		data := a.Scene.BVHNodesBytes
		if glass {
			data = a.Scene.TransparentBVHNodesBytes
		}
		buf := a.BufferManager.BVHNodesBuf
		if glass {
			buf = a.BufferManager.TransparentBVHNodesBuf
		}
		leaf, visits, stack := uploadedProof(a, buf, data, name, *output)
		selectedObjects := a.Scene.VisibleObjects
		instbuf := a.BufferManager.InstancesBuf
		if glass {
			selectedObjects = a.Scene.TransparentVisibleObjects
			instbuf = a.BufferManager.TransparentInstancesBuf
		}
		require(len(selectedObjects) == 300, "pass candidate count differs")
		for i, o := range objects {
			require(selectedObjects[i] == o, "visible/transparent instance order differs")
		}
		proveInstances(a, instbuf, 300)

		if !glass {
			sl, sv, ss := uploadedProof(a, a.BufferManager.ShadowBVHNodesBuf, a.Scene.ShadowBVHNodesBytes, "shadow", *output)
			require(sl == leaf && sv == visits && ss == stack && len(a.Scene.ShadowObjects) == len(objects), "shadow tree candidate order differs")
			proveInstances(a, a.BufferManager.ShadowInstancesBuf, 300)
			for i, o := range objects {
				require(a.Scene.ShadowObjects[i] == o, "shadow instance order differs")
			}
		}
		require(visits == 599 && visits > 512, "large fixture does not exceed cap")
		allmiss := captureNow(a, origin)
		missOK := allmiss.Depth[0] > 20 && allmiss.Shadow[0] == 0 && allmiss.Transparent[3] == 0
		results = append(results, outcome{Name: name + "-all-miss", Pass: missOK, Actual: allmiss, Visits: visits, MaxStack: stack})
		// Replace only the chosen last leaf with occupied central cells. Bounds and
		// centroid remain exactly equal, so candidate order is unchanged.
		for x := 3; x <= 4; x++ {
			for y := 3; y <= 4; y++ {
				objects[leaf].XBrickMap.SetVoxel(x, y, 7, 1)
			}
		}
		warm(a)
		data = a.Scene.BVHNodesBytes
		if glass {
			data = a.Scene.TransparentBVHNodesBytes
		}
		again, v, s := uploadedProof(a, buf, data, name+"-hit", *output)
		require(again == leaf && v == visits && s == stack, "hit edit changed tree order")
		actual := captureNow(a, origin)
		if glass {
			ok := actual.Transparent == control.Transparent
			results = append(results, outcome{Name: "transparent-late-hit", Pass: ok, Detail: "production transparent fragment matches small-tree accumulation", Control: control, Actual: actual, Visits: visits, MaxStack: stack, SelectedLeaf: leaf})
		} else {
			results = append(results, outcome{Name: "opaque-late-hit", Pass: actual.Depth == control.Depth && actual.Albedo == control.Albedo && actual.Material[0] == control.Material[0] && actual.Material[1] == control.Material[1] && actual.Material[2] == control.Material[2], Detail: "production G-buffer depth, material metadata and palette albedo match small-tree control", Control: control, Actual: actual, Visits: visits, MaxStack: stack, SelectedLeaf: leaf})
			results = append(results, outcome{Name: "shadow-late-hit", Pass: actual.Shadow == control.Shadow, Detail: "production shadow traverse_scene matches small-tree hit and distance", Control: control, Actual: actual, Visits: visits, MaxStack: stack, SelectedLeaf: leaf})
		}
	}
	// A near hit must win even when farther hit geometry is first in input.
	reset(a, []*core.VoxelObject{object(false, true)})
	nearControl := captureNow(a, origin)
	far := object(false, true)
	far.Transform.Position = mgl32.Vec3{0, 0, -8}
	reset(a, []*core.VoxelObject{far, object(false, true)})
	nearActual := captureNow(a, origin)
	results = append(results, outcome{Name: "near-first-and-nearest-bound", Pass: nearActual.Depth == nearControl.Depth && nearActual.Shadow == nearControl.Shadow && nearActual.Albedo == nearControl.Albedo, Control: nearControl, Actual: nearActual})
	// Exercise the zero sentinel after large allocation and aim through its point.
	capacities := []uint64{a.BufferManager.BVHNodesBuf.GetSize(), a.BufferManager.TransparentBVHNodesBuf.GetSize(), a.BufferManager.ShadowBVHNodesBuf.GetSize()}
	reset(a, nil)
	for i, buffer := range []*wgpu.Buffer{a.BufferManager.BVHNodesBuf, a.BufferManager.TransparentBVHNodesBuf, a.BufferManager.ShadowBVHNodesBuf} {
		require(buffer.GetSize() == capacities[i] && buffer.GetSize() > 64, "empty fixture did not retain prior capacity")
		raw := readBuffer(a, buffer, 0, 64)
		for _, v := range raw {
			require(v == 0, "empty uploaded root is not zero sentinel")
		}
	}
	fmt.Printf("EMPTY retained BVH bytes opaque/transparent/shadow=%v\n", capacities)
	origin = mgl32.Vec3{0, 0, 10}
	a.Camera.Position = origin
	a.Camera.LookAt = mgl32.Vec3{0, 0, 0}
	warm(a)
	empty := captureNow(a, origin)
	results = append(results, outcome{Name: "retained-capacity-empty-origin-ray", Pass: empty.Depth[0] > 20 && empty.Transparent[3] == 0 && empty.Shadow[0] == 0, Actual: empty})
	raw, err := json.MarshalIndent(results, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(*output, "results.json"), raw, 0644))
	failed := false
	for _, r := range results {
		status := "PASS"
		if !r.Pass {
			status = "FAIL"
			failed = true
		}
		fmt.Printf("%s %s visits=%d stack=%d leaf=%d depth=%g shadow=%v transparency=%v\n", status, r.Name, r.Visits, r.MaxStack, r.SelectedLeaf, r.Actual.Depth[0], r.Actual.Shadow, r.Actual.Transparent)
	}
	if failed {
		os.Exit(1)
	}
}
