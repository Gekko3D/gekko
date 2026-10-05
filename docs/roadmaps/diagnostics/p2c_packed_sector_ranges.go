//go:build ignore

// Native P2c packed sector range diagnostic, with dense baseline mode.
// From engine cwd: env GOCACHE=/tmp/gekko3d-gocache go run ./docs/roadmaps/diagnostics/p2c_packed_sector_ranges.go [-baseline]
// Requires a desktop/WebGPU session. Baseline relaxes packed-layout/savings/base
// assertions; both modes read native data and execute all four shaders' samples.
// This does not measure FPS or prove fragment blending, complete shadow maps,
// world-space particle integration, or every main ray-traversal branch.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"math"
	"math/bits"
	"runtime"
	"strings"
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

var uploadedBytes, uploadedRecords uint64
var requirePacked bool

func warm(a *app.App) {
	for frame := 0; frame < 256; frame++ {
		glfw.PollEvents()
		a.Update()
		uploadedBytes += a.BufferManager.VoxelUploadBytes
		uploadedRecords += uint64(a.BufferManager.VoxelBricksUploaded)
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
	panic("auxiliary fixture never became ready")
}
func verify(a *app.App, o *core.VoxelObject) {
	m := a.BufferManager
	lo, hi := o.XBrickMap.ComputeAABB()
	opts := volume.VoxelNormalBakeOptions{BoundsMin: lo, BoundsMax: hi, HasBounds: true, SampleOccupancy: func(p [3]int) bool { occupied, _ := o.XBrickMap.GetVoxel(p[0], p[1], p[2]); return occupied }}
	for coord, s := range o.XBrickMap.Sectors {
		info, ok := m.SectorToInfo[s]
		require(ok, "missing sector header")
		header := readBuffer(a, m.SectorTableBuf, uint64(info.SlotIndex)*32, 32)
		for axis := 0; axis < 3; axis++ {
			require(int32(binary.LittleEndian.Uint32(header[axis*4:])) == int32(coord[axis]*32), "GPU sector origin differs from CPU authority")
		}
		require(binary.LittleEndian.Uint32(header[16:]) == info.BrickTableIndex, "GPU sector base differs from assigned range")
		mask := uint64(binary.LittleEndian.Uint32(header[20:])) | uint64(binary.LittleEndian.Uint32(header[24:]))<<32
		require(mask == s.BrickMask64, "GPU sector mask differs from CPU authority")
		layout := binary.LittleEndian.Uint32(header[28:])
		require(layout == 0 || layout == 1, "unknown native sector layout")
		if requirePacked {
			require(layout == 1, "managed sector did not publish packed layout flag")
		}
		count := 64
		if layout == 1 {
			count = bits.OnesCount64(mask)
		}
		if count == 0 {
			continue
		}
		records := readBuffer(a, m.BrickTableBuf, uint64(info.BrickTableIndex)*gpu.BrickRecordSize, uint64(count)*gpu.BrickRecordSize)
		for i := 0; i < 64; i++ {
			b := s.GetBrick(i%4, (i/4)%4, i/16)
			if b == nil {
				continue
			}
			recordIndex := i
			if layout == 1 {
				recordIndex = bits.OnesCount64(mask & ((uint64(1) << i) - 1))
			}
			record := records[recordIndex*int(gpu.BrickRecordSize):]
			if b.Flags&(volume.BrickFlagSolid|volume.BrickFlagUniformMaterial) != 0 {
				require(binary.LittleEndian.Uint32(record) == b.AtlasOffset, fmt.Sprintf("ranked GPU material differs: sector=%v local=%d rank=%d base=%d mask=%016x GPU=%d CPU=%d flagsGPU=%x flagsCPU=%x", coord, i, recordIndex, info.BrickTableIndex, mask, binary.LittleEndian.Uint32(record), b.AtlasOffset, binary.LittleEndian.Uint32(record[20:]), b.Flags))
			}
			require(binary.LittleEndian.Uint32(record[20:]) == b.Flags, "ranked GPU record flags differ from CPU brick")
			word := binary.LittleEndian.Uint32(record[24:])
			require(word != gpu.VoxelAuxInvalidWordBase, "occupied brick has no auxiliary offset")
			require(uint64(word)*4+volume.VoxelAuxRecordBytes <= m.DenseOccupancyBuf.GetSize(), "native auxiliary offset exceeds physical buffer")
			origin := [3]int{coord[0]*32 + i%4*8, coord[1]*32 + (i/4)%4*8, coord[2]*32 + i/16*8}
			got := readBuffer(a, m.DenseOccupancyBuf, uint64(word)*4, volume.VoxelAuxRecordBytes)
			want := volume.BuildVoxelAuxBytes(b, origin, opts)
			if !bytes.Equal(got, want) {
				for index := range got {
					if got[index] != want[index] {
						panic(fmt.Sprintf("GPU auxiliary differs at sector %v brick %d byte %d: got=%d want=%d offset=%d", coord, i, index, got[index], want[index], word*4))
					}
				}
			}
		}
	}
}

// Execute the actual particle shader's local collision and baked-normal helpers
// against the manager's native published buffers. This isolates rank lookup from
// random spawning and time-dependent particle integration.
func particleProbe(a *app.App, o *core.VoxelObject) {
	points := [][3]int{{0, 0, 0}, {24, 24, 8}, {0, 0, 16}, {24, 24, 24}, {25, 24, 8}, {16, 0, 0}}
	literals := make([]string, len(points))
	for i, p := range points {
		literals[i] = fmt.Sprintf("vec3<f32>(%.1f, %.1f, %.1f)", float64(p[0])+.5, float64(p[1])+.5, float64(p[2])+.5)
	}
	code := shaders.ParticlesSimWGSL + fmt.Sprintf(`
 @group(3) @binding(0) var<storage, read_write> probe_results: array<vec4<f32>>;
 @compute @workgroup_size(1)
 fn p2c_probe(@builtin(global_invocation_id) id: vec3<u32>) {
   let points = array<vec3<f32>, %d>(%s);
   if (id.x >= %du) { return; }
   let p = points[id.x];
   let op = object_params[0];
   let hit = check_voxel_occupancy(p, op);
   let n = load_baked_voxel_normal(p, op);
   probe_results[id.x] = vec4<f32>(select(0.0,1.0,hit),n);
 }`, len(points), strings.Join(literals, ","), len(points))
	module, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "P2c actual particle helpers probe", WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: code}})
	must(err)
	defer module.Release()
	pipeline, err := a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Label: "P2c particle occupancy/normal readback", Compute: wgpu.ProgrammableStageDescriptor{Module: module, EntryPoint: "p2c_probe"}})
	must(err)
	defer pipeline.Release()
	output, err := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Size: uint64(len(points) * 16), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc})
	must(err)
	defer output.Release()
	m := a.BufferManager
	entries := []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize}, {Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
		{Binding: 7, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize}, {Binding: 9, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
		{Binding: 10, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize}, {Binding: 11, Buffer: m.DirectSectorLookupBuf, Size: wgpu.WholeSize},
		{Binding: 13, Buffer: m.DenseOccupancyBuf, Size: wgpu.WholeSize},
	}
	geometry, err := a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(2), Entries: entries})
	must(err)
	defer geometry.Release()
	results, err := a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(3), Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: output, Size: wgpu.WholeSize}}})
	must(err)
	defer results.Release()
	encoder, err := a.Device.CreateCommandEncoder(nil)
	must(err)
	defer encoder.Release()
	pass := encoder.BeginComputePass(nil)
	pass.SetPipeline(pipeline)
	for group := uint32(0); group < 2; group++ {
		empty, err := a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(group)})
		must(err)
		pass.SetBindGroup(group, empty, nil)
		empty.Release()
	}
	pass.SetBindGroup(2, geometry, nil)
	pass.SetBindGroup(3, results, nil)
	pass.DispatchWorkgroups(uint32(len(points)), 1, 1)
	pass.End()
	pass.Release()
	submit(a, encoder)
	got := readBuffer(a, output, 0, uint64(len(points)*16))
	lo, hi := o.XBrickMap.ComputeAABB()
	opts := volume.VoxelNormalBakeOptions{BoundsMin: lo, BoundsMax: hi, HasBounds: true, SampleOccupancy: func(p [3]int) bool { yes, _ := o.XBrickMap.GetVoxel(p[0], p[1], p[2]); return yes }}
	for i, p := range points {
		hit, _ := o.XBrickMap.GetVoxel(p[0], p[1], p[2])
		value := func(j int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(got[i*16+j*4:])) }
		require((value(0) == 1) == hit, fmt.Sprintf("particle collision lookup differs at %v", p))
		if hit {
			want, valid, _ := volume.BakedVoxelNormal(opts, p)
			require(valid, "particle fixture normal not valid")
			n := mgl32.Vec3{value(1), value(2), value(3)}
			require(n.Dot(want) > .999, fmt.Sprintf("particle decoded normal differs at %v: GPU%v CPU%v", p, n, want))
		}
	}
	fmt.Println("PASS: actual particle collision/normal helper compute readback at0/31/32/63 and empty samples")
}

// Probe production occupancy/sample helpers at the two mask-word boundary and
// highest brick. These are the samples used by ray traversal/normal fitting;
// this does not replace full rendered image/blend or shadow-map integration.
func rendererSampleProbes(a *app.App, o *core.VoxelObject) {
	points := [][3]int{{0, 0, 0}, {24, 24, 8}, {0, 0, 16}, {24, 24, 24}, {25, 24, 8}, {16, 0, 0}}
	literals := make([]string, len(points))
	for i, p := range points {
		literals[i] = fmt.Sprintf("vec3<i32>(%d,%d,%d)", p[0], p[1], p[2])
	}
	for _, consumer := range []struct {
		name, source, sample string
		group                uint32
		normal               bool
	}{
		{"gbuffer", shaders.GBufferWGSL, "sample_occupancy_local", 2, true},
		{"shadow", shaders.ShadowMapWGSL, "sample_occupancy_local", 2, false},
		{"transparent", shaders.TransparentOverlayWGSL, "sample_occupancy", 1, true},
	} {
		normalCode := "let n = vec3<f32>(0.0);"
		if consumer.normal {
			normalCode = "let n = load_baked_voxel_normal_local(p,op).normal;"
		}
		code := consumer.source + fmt.Sprintf(`
 @group(3) @binding(15) var<storage,read_write> p2c_results: array<vec4<f32>>;
 @compute @workgroup_size(1)
 fn p2c_samples(@builtin(global_invocation_id) id:vec3<u32>) {
 let points=array<vec3<i32>,%d>(%s);
 if(id.x >= %du){return;}
 let p=points[id.x];let op=object_params[0];
 let occupied=%s(p,op); %s
 p2c_results[id.x]=vec4<f32>(occupied,n);
 }`, len(points), strings.Join(literals, ","), len(points), consumer.sample, normalCode)
		module, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "P2c " + consumer.name + " actual samples", WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: code}})
		must(err)
		pipeline, err := a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Label: "P2c " + consumer.name + " samples", Compute: wgpu.ProgrammableStageDescriptor{Module: module, EntryPoint: "p2c_samples"}})
		must(err)
		output, err := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Size: uint64(len(points) * 16), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc})
		must(err)
		m := a.BufferManager
		geometry, err := a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(consumer.group), Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize}, {Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
			{Binding: 7, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize}, {Binding: 9, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
			{Binding: 10, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize}, {Binding: 11, Buffer: m.DirectSectorLookupBuf, Size: wgpu.WholeSize},
			{Binding: 13, Buffer: m.DenseOccupancyBuf, Size: wgpu.WholeSize}}})
		must(err)
		resultGroup, err := a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(3), Entries: []wgpu.BindGroupEntry{{Binding: 15, Buffer: output, Size: wgpu.WholeSize}}})
		must(err)
		encoder, err := a.Device.CreateCommandEncoder(nil)
		must(err)
		pass := encoder.BeginComputePass(nil)
		pass.SetPipeline(pipeline)
		for group := uint32(0); group < 3; group++ {
			if group == consumer.group {
				continue
			}
			empty, err := a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(group)})
			must(err)
			pass.SetBindGroup(group, empty, nil)
			empty.Release()
		}
		pass.SetBindGroup(consumer.group, geometry, nil)
		pass.SetBindGroup(3, resultGroup, nil)
		pass.DispatchWorkgroups(uint32(len(points)), 1, 1)
		pass.End()
		pass.Release()
		submit(a, encoder)
		got := readBuffer(a, output, 0, uint64(len(points)*16))
		lo, hi := o.XBrickMap.ComputeAABB()
		opts := volume.VoxelNormalBakeOptions{BoundsMin: lo, BoundsMax: hi, HasBounds: true, SampleOccupancy: func(p [3]int) bool { hit, _ := o.XBrickMap.GetVoxel(p[0], p[1], p[2]); return hit }}
		for i, p := range points {
			hit, _ := o.XBrickMap.GetVoxel(p[0], p[1], p[2])
			value := func(j int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(got[i*16+j*4:])) }
			require((value(0) == 1) == hit, fmt.Sprintf("%s actual sample occupancy differs at%v", consumer.name, p))
			if hit && consumer.normal {
				want, valid, _ := volume.BakedVoxelNormal(opts, p)
				require(valid, "renderer probe normal invalid")
				n := mgl32.Vec3{value(1), value(2), value(3)}
				require(n.Dot(want) > .999, fmt.Sprintf("%s decoded normal differs at%v", consumer.name, p))
			}
		}
		encoder.Release()
		resultGroup.Release()
		geometry.Release()
		output.Release()
		pipeline.Release()
		module.Release()
		fmt.Printf("PASS: actual %s occupied/empty samples at 0/31/32/63\n", consumer.name)
	}
}

func main() {
	baseline := flag.Bool("baseline", false, "exercise dense baseline without requiring packed layout or savings")
	flag.Parse()
	requirePacked = !*baseline
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	window, err := glfw.CreateWindow(320, 240, "P2c packed sector ranges", nil, nil)
	must(err)
	defer window.Destroy()
	a := app.NewApp(window)
	a.FeatureConfig = app.AppFeatureConfig{AutoRegisterDefaults: false}
	a.RegisterFeature(&app.TransparencyFeature{})
	a.RegisterFeature(&app.ParticlesFeature{})
	must(a.Init())
	defer a.Shutdown()
	require(a.GBufferPipeline != nil && a.BufferManager.ShadowPipeline != nil, "native gbuffer/shadow pipeline missing")
	require(a.AccumulationResources != nil && a.AccumulationResources.TransparentPipeline != nil, "native transparency pipeline missing")
	require(a.ParticleResources != nil && a.ParticleResources.SimPipeline != nil, "native particle pipeline missing")
	fmt.Println("Native gbuffer, shadow, transparency and particle simulation pipelines compiled")
	a.OcclusionMode = core.OcclusionOff
	a.Camera.Position, a.Camera.LookAt = mgl32.Vec3{0, 1, 14}, mgl32.Vec3{0, 1, 0}
	o := core.NewVoxelObject()
	o.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial([4]uint8{190, 110, 50, 255}, [4]uint8{})}
	volume.Cube(o.XBrickMap, mgl32.Vec3{}, mgl32.Vec3{7, 7, 7}, 1)
	for i := 1; i < 127; i++ {
		o.XBrickMap.SetVoxel(i*32, 0, 0, 1)
	}
	// Keep object bounds fixed during growth/removal. Isolated-voxel normal
	// tie breaking uses the object midpoint; clean records retain their baked
	// values when distant edits move that midpoint, independently of migration.
	o.XBrickMap.SetVoxel(4095*32, 0, 0, 1)
	a.Scene.AddObject(o)
	warm(a)
	verify(a, o)
	old := a.BufferManager.DenseOccupancyBuf.GetSize()
	initialTable := a.BufferManager.BrickTableBuf.GetSize()
	initialHeader := readBuffer(a, a.BufferManager.SectorTableBuf, uint64(a.BufferManager.SectorToInfo[o.XBrickMap.Sectors[[3]int{}]].SlotIndex)*32, 32)
	packed := binary.LittleEndian.Uint32(initialHeader[28:]) == 1
	fmt.Printf("P2c initial: packed=%t brick-table capacity=%d bytes, sparse sectors=128\n", packed, initialTable)
	fmt.Printf("128 sparse sectors: content bytes=%d, brick records=%d, auxiliary capacity=%d bytes\n", uploadedBytes, uploadedRecords, old)
	if !*baseline {
		require(packed, "P2c requires packed publication headers (use -baseline on dense code)")
		require(initialTable <= 2*128*gpu.BrickRecordSize, "packed table capacity still scales with64 potential records")
	}
	require(uploadedRecords == 128, "sparse sectors still upload inactive brick records")
	require(uploadedBytes == 128+128*(32+gpu.BrickRecordSize+volume.VoxelAuxRecordBytes), "sparse sector byte charges differ from actual occupied records")
	require(old <= 2*128*volume.VoxelAuxRecordBytes, "auxiliary capacity still scales with 64 potential sector records")
	o.MaterialTable = append(o.MaterialTable,
		core.NewMaterial([4]uint8{70, 190, 90, 255}, [4]uint8{}),
		core.NewMaterial([4]uint8{60, 90, 210, 255}, [4]uint8{}),
		core.NewMaterial([4]uint8{210, 190, 40, 255}, [4]uint8{}))
	// Deferred membership edit leaves the complete committed header readable.
	sector0 := o.XBrickMap.Sectors[[3]int{}]
	originalInfo := a.BufferManager.SectorToInfo[sector0]
	originalHeader := readBuffer(a, a.BufferManager.SectorTableBuf, uint64(originalInfo.SlotIndex)*32, 32)
	o.XBrickMap.SetVoxel(8, 0, 0, 1)
	a.BufferManager.SetVoxelUploadBudget(gpu.VoxelUploadBudget{})
	a.Update()
	a.Render()
	a.Device.Poll(true, nil)
	deferredHeader := readBuffer(a, a.BufferManager.SectorTableBuf, uint64(originalInfo.SlotIndex)*32, 32)
	require(bytes.Equal(originalHeader, deferredHeader), "deferred membership edit changed committed base/mask")
	a.BufferManager.SetVoxelUploadBudget(gpu.DefaultVoxelUploadBudget())
	warm(a)
	verify(a, o)
	replacementInfo := a.BufferManager.SectorToInfo[sector0]
	if packed {
		require(replacementInfo.BrickTableIndex != originalInfo.BrickTableIndex, "membership growth overwrote committed packed range")
	}
	// Recolor within the same occupied brick keeps membership and packed base.
	o.XBrickMap.SetVoxel(8, 0, 0, 2)
	warm(a)
	verify(a, o)
	if packed {
		require(a.BufferManager.SectorToInfo[sector0].BrickTableIndex == replacementInfo.BrickTableIndex, "stable membership unnecessarily relocated packed range")
	}
	// Exercise both mask words and the highest brick index, then clear one.
	for n, index := range []int{31, 32, 63} {
		o.XBrickMap.SetVoxel(index%4*8, (index/4)%4*8, index/16*8, uint8(n+2))
	}
	warm(a)
	verify(a, o)
	particleProbe(a, o)
	rendererSampleProbes(a, o)
	o.XBrickMap.SetVoxel(0, 0, 16, 0)
	warm(a)
	verify(a, o)
	// Replacement preserves the final occupancy count and recycles safe slots.
	o.XBrickMap.SetVoxel(32, 0, 0, 0)
	o.XBrickMap.SetVoxel(40, 0, 0, 1)
	warm(a)
	verify(a, o)
	// Demand beyond current capacity must grow and preserve all previous bytes.
	count := int(a.BufferManager.DenseOccupancyBuf.GetSize()/volume.VoxelAuxRecordBytes) + 16
	require(count < 4096, "unexpected diagnostic auxiliary capacity")
	for i := 128; i < 128+count; i++ {
		o.XBrickMap.SetVoxel(i*32, 0, 0, 1)
	}
	warm(a)
	require(a.BufferManager.DenseOccupancyBuf.GetSize() > old, "fixture did not force auxiliary growth")
	if packed {
		require(a.BufferManager.BrickTableBuf.GetSize() > initialTable, "fixture did not force packed table growth")
	}
	verify(a, o)
	// Removal does not shrink physical capacity; surviving normals remain exact.
	for i := 128; i < 128+count; i++ {
		o.XBrickMap.SetVoxel(i*32, 0, 0, 0)
	}
	warm(a)
	verify(a, o)
	// Reuse a previously published slot with content writes paused. Its old GPU
	// header remains physically present, but the new sector must be unreachable.
	m := a.BufferManager
	previous := m.SectorToInfo[o.XBrickMap.Sectors[[3]int{1, 0, 0}]]
	o.XBrickMap.SetVoxel(40, 0, 0, 0)
	o.XBrickMap.SetVoxel(4094*32, 0, 0, 1)
	m.SetVoxelUploadBudget(gpu.VoxelUploadBudget{})
	a.Update()
	a.Render()
	a.Device.Poll(true, nil)
	fresh := m.SectorToInfo[o.XBrickMap.Sectors[[3]int{4094, 0, 0}]]
	fmt.Printf("sector slot replacement %d -> %d (record ranges may remain quarantined)\n", previous.SlotIndex, fresh.SlotIndex)
	grid := readBuffer(a, m.SectorGridBuf, 0, m.SectorGridBuf.GetSize())
	for i := 0; i+32 <= len(grid); i += 32 {
		if binary.LittleEndian.Uint32(grid[i+20:]) != ^uint32(0) && binary.LittleEndian.Uint32(grid[i+16:]) == o.XBrickMap.ID && int32(binary.LittleEndian.Uint32(grid[i:])) == 4094 {
			panic("deferred reused sector exposes stale GPU header through native hash lookup")
		}
	}
	m.SetVoxelUploadBudget(gpu.DefaultVoxelUploadBudget())
	warm(a)
	verify(a, o)
	grid = readBuffer(a, m.SectorGridBuf, 0, m.SectorGridBuf.GetSize())
	found := false
	for i := 0; i+32 <= len(grid); i += 32 {
		if binary.LittleEndian.Uint32(grid[i+20:]) == fresh.SlotIndex && binary.LittleEndian.Uint32(grid[i+16:]) == o.XBrickMap.ID && int32(binary.LittleEndian.Uint32(grid[i:])) == 4094 {
			found = true
		}
	}
	require(found, "completed sector header did not become reachable through native hash lookup")
	fmt.Printf("PASS: native layout packed=%t, exact upload charges, ranked materials/normals, membership replacement, stable edits, table %d -> %d bytes, auxiliary %d -> %d bytes and deferred slot publication\n", packed, initialTable, m.BrickTableBuf.GetSize(), old, m.DenseOccupancyBuf.GetSize())
}
