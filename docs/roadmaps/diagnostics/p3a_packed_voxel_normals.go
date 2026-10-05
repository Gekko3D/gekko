//go:build ignore

// Native P3a opt-in packed fitted-normal diagnostic.
// Run dense and packed in separate desktop processes with -output and -compare.
// Uses production shader helpers and textureLoad readback; no FPS/timing claim.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
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
	"math/bits"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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

var packed bool
var outputDir, compareDir string
var uploadedBytes uint64

func save(name string, data []byte) {
	must(os.WriteFile(filepath.Join(outputDir, name), data, 0644))
	if compareDir != "" {
		prior, err := os.ReadFile(filepath.Join(compareDir, name))
		must(err)
		if !bytes.Equal(prior, data) {
			index := 0
			for index < len(prior) && index < len(data) && prior[index] == data[index] {
				index++
			}
			panic(fmt.Sprintf("paired parity failed: %s first byte %d lengths %d/%d", name, index, len(prior), len(data)))
		}
	}
}
func warm(a *app.App) {
	for frame := 0; frame < 512; frame++ {
		glfw.PollEvents()
		a.Update()
		uploadedBytes += a.BufferManager.VoxelUploadBytes
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
	panic("P3a fixture never became ready")
}
func floorDiv(x, n int) int {
	q := x / n
	if x < 0 && x%n != 0 {
		q--
	}
	return q
}
func expectedAux(a *app.App, o *core.VoxelObject, b *volume.Brick, origin [3]int) []byte {
	if len(b.PrecomputedAux) == volume.VoxelAuxRecordBytes {
		return b.PrecomputedAux
	}
	lo, hi := o.XBrickMap.ComputeAABB()
	sample := func(p [3]int) bool {
		target := o
		if o.VoxelAdjacencyGroupID != 0 {
			size := o.VoxelAdjacencyChunkSize
			delta := [3]int{floorDiv(p[0], size), floorDiv(p[1], size), floorDiv(p[2], size)}
			if delta != [3]int{} {
				target = nil
				for _, other := range a.Scene.Objects {
					if other.VoxelAdjacencyGroupID == o.VoxelAdjacencyGroupID && other.VoxelAdjacencyChunkCoord == [3]int{o.VoxelAdjacencyChunkCoord[0] + delta[0], o.VoxelAdjacencyChunkCoord[1] + delta[1], o.VoxelAdjacencyChunkCoord[2] + delta[2]} {
						target = other
						break
					}
				}
				if target == nil {
					return false
				}
				for axis := range p {
					p[axis] -= delta[axis] * size
				}
			}
		}
		yes, _ := target.XBrickMap.GetVoxel(p[0], p[1], p[2])
		return yes
	}
	return volume.BuildVoxelAuxBytes(b, origin, volume.VoxelNormalBakeOptions{BoundsMin: lo, BoundsMax: hi, HasBounds: true, SampleOccupancy: sample})
}

type probeSample struct {
	record, voxel, object uint32
	position              [3]int
	encoded               uint16
	occupied              bool
}
type measurements struct {
	PhysicalAuxBytes, LogicalAuxBytes, UploadBytes, Records uint64
	Mode                                                    string
	Timing                                                  string
}

// Read physical publication and independently reconstruct its dense auxiliary
// authority. Requests are sorted by authored coordinates, never physical offsets.
func samples(a *app.App) ([]probeSample, measurements) {
	m := a.BufferManager
	stats := measurements{PhysicalAuxBytes: m.DenseOccupancyBuf.GetSize(), UploadBytes: uploadedBytes, Timing: "dense traversal GPU timing unverified"}
	if packed {
		stats.Mode = "packed"
	} else {
		stats.Mode = "dense"
	}
	params := readBuffer(a, m.ObjectParamsBuf, 0, m.ObjectParamsBuf.GetSize())
	requests := []probeSample{}
	for _, o := range a.Scene.Objects {
		objectIndex := -1
		for offset := 0; offset+128 <= len(params); offset += 128 {
			if binary.LittleEndian.Uint32(params[offset:]) == o.XBrickMap.ID {
				objectIndex = offset / 128
				break
			}
		}
		require(objectIndex >= 0, "fixture object missing from published visible object params")
		coords := make([][3]int, 0, len(o.XBrickMap.Sectors))
		for c := range o.XBrickMap.Sectors {
			coords = append(coords, c)
		}
		sort.Slice(coords, func(i, j int) bool {
			for axis := 0; axis < 3; axis++ {
				if coords[i][axis] != coords[j][axis] {
					return coords[i][axis] < coords[j][axis]
				}
			}
			return false
		})
		for _, c := range coords {
			sector := o.XBrickMap.Sectors[c]
			info := m.SectorToInfo[sector]
			header := readBuffer(a, m.SectorTableBuf, uint64(info.SlotIndex)*32, 32)
			mask := uint64(binary.LittleEndian.Uint32(header[20:])) | uint64(binary.LittleEndian.Uint32(header[24:]))<<32
			require(mask == sector.BrickMask64, "published sector mask differs")
			base := binary.LittleEndian.Uint32(header[16:])
			sectorLayout := binary.LittleEndian.Uint32(header[28:])
			for index := 0; index < 64; index++ {
				b := sector.GetBrick(index%4, index/4%4, index/16)
				if b == nil {
					continue
				}
				recordIndex := base + uint32(index)
				if sectorLayout == 1 {
					recordIndex = base + uint32(bits.OnesCount64(mask&((uint64(1)<<index)-1)))
				}
				record := readBuffer(a, m.BrickTableBuf, uint64(recordIndex)*gpu.BrickRecordSize, gpu.BrickRecordSize)
				auxBase := binary.LittleEndian.Uint32(record[24:])
				layout := binary.LittleEndian.Uint32(record[28:])
				require(layout == uint32(map[bool]int{false: 0, true: 1}[packed]), "normal layout disagrees with init-only policy")
				origin := [3]int{c[0]*32 + index%4*8, c[1]*32 + index/4%4*8, c[2]*32 + index/16*8}
				dense := expectedAux(a, o, b, origin)
				occupiedCount := 0
				for i := 0; i < 16; i++ {
					occupiedCount += bits.OnesCount32(binary.LittleEndian.Uint32(dense[i*4:]))
				}
				size := uint64(volume.VoxelAuxRecordBytes)
				if packed {
					size = 64 + uint64((occupiedCount+1)/2)*4
				}
				require(uint64(auxBase)*4+size <= m.DenseOccupancyBuf.GetSize(), "published aux exceeds native capacity")
				got := readBuffer(a, m.DenseOccupancyBuf, uint64(auxBase)*4, size)
				require(bytes.Equal(got[:64], dense[:64]), "native occupancy differs from dense authority")
				rank := 0
				for vi := 0; vi < 512; vi++ {
					occ := (binary.LittleEndian.Uint32(dense[(vi/32)*4:]) & (1 << uint(vi%32))) != 0
					encoded := binary.LittleEndian.Uint16(dense[64+vi*2:])
					if !packed || occ {
						normalIndex := vi
						if packed {
							normalIndex = rank
						}
						require(binary.LittleEndian.Uint16(got[64+normalIndex*2:]) == encoded, "native raw normal differs")
					}
					if occ {
						rank++
					}
					requests = append(requests, probeSample{record: recordIndex, voxel: uint32(vi), object: uint32(objectIndex), position: [3]int{origin[0] + vi%8, origin[1] + vi/8%8, origin[2] + vi/64}, encoded: encoded, occupied: occ})
				}
				stats.LogicalAuxBytes += size
				stats.Records++
			}
		}
	}
	return requests, stats
}

// Actual three production normal consumers plus the production shadow occupancy
// helper. No replacement implementation of rank lookup is injected into WGSL.
func shaderProbes(a *app.App, phase string, requests []probeSample) {
	for _, consumer := range []struct {
		name, source, sample string
		group                uint32
		kind                 int
	}{
		{"gbuffer", shaders.GBufferWGSL, "sample_occupancy_local", 2, 0},
		{"transparent", shaders.TransparentOverlayWGSL, "sample_occupancy", 1, 1},
		{"particles", shaders.ParticlesSimWGSL, "check_voxel_occupancy", 2, 2},
		{"shadow", shaders.ShadowMapWGSL, "sample_occupancy_local", 2, 3},
	} {
		occupancy := fmt.Sprintf("u32(%s(p,op))", consumer.sample)
		normal := `let encoded=load_baked_voxel_normal_encoded_from_brick(brick,r.voxel);
   let n=load_baked_voxel_normal_local(p,op); probe_results[id.x].encoded=encoded;
   probe_results[id.x].valid=select(0u,1u,n.valid);
   probe_results[id.x].two_sided=u32(n.two_sided_lighting);
   probe_results[id.x].normal=vec4<f32>(n.normal,0.0);`
		if consumer.kind == 1 {
			normal = strings.Replace(normal, "u32(n.two_sided_lighting)", "select(0u,1u,n.two_sided_lighting)", 1)
		}
		if consumer.kind == 2 {
			occupancy = "select(0u,1u,check_voxel_occupancy(vec3<f32>(p)+vec3<f32>(0.5),op))"
			normal = `let encoded=load_baked_voxel_normal_encoded_from_brick(brick,r.voxel);
    probe_results[id.x].encoded=encoded;
    probe_results[id.x].valid=select(0u,1u,(encoded&VOXEL_NORMAL_VALID_BIT)!=0u);
    probe_results[id.x].two_sided=select(0u,1u,(encoded&0x8000u)!=0u);
    probe_results[id.x].normal=vec4<f32>(load_baked_voxel_normal(vec3<f32>(p)+vec3<f32>(0.5),op),0.0);`
		}
		if consumer.kind == 3 {
			normal = ""
		}
		code := consumer.source + fmt.Sprintf(`
 struct P3aProbeResult {record:u32,voxel:u32,object:u32,pad:u32,position:vec4<i32>,encoded:u32,occupied:u32,valid:u32,two_sided:u32,normal:vec4<f32>};
 @group(3) @binding(15) var<storage,read_write> probe_results:array<P3aProbeResult>;
 @compute @workgroup_size(64)
 fn p3a_probe(@builtin(global_invocation_id) id:vec3<u32>) {
  if(id.x >= %du){return;}
  let r=probe_results[id.x];let p=r.position.xyz;let op=object_params[r.object];let brick=bricks[r.record];
  probe_results[id.x].occupied=%s; %s
 }`, len(requests), occupancy, normal)
		module, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "P3a actual " + consumer.name, WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: code}})
		must(err)
		pipeline, err := a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Label: "P3a " + consumer.name + " probes", Compute: wgpu.ProgrammableStageDescriptor{Module: module, EntryPoint: "p3a_probe"}})
		must(err)
		data := make([]byte, len(requests)*64)
		for i, r := range requests {
			for j, v := range []uint32{r.record, r.voxel, r.object, 0, uint32(r.position[0]), uint32(r.position[1]), uint32(r.position[2])} {
				binary.LittleEndian.PutUint32(data[i*64+j*4:], v)
			}
		}
		output, err := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Size: uint64(len(data)), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc | wgpu.BufferUsageCopyDst})
		must(err)
		must(a.Queue.WriteBuffer(output, 0, data))
		m := a.BufferManager
		geometry, err := a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(consumer.group), Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize}, {Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
			{Binding: 7, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize}, {Binding: 9, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
			{Binding: 10, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize}, {Binding: 11, Buffer: m.DirectSectorLookupBuf, Size: wgpu.WholeSize}, {Binding: 13, Buffer: m.DenseOccupancyBuf, Size: wgpu.WholeSize},
		}})
		must(err)
		results, err := a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(3), Entries: []wgpu.BindGroupEntry{{Binding: 15, Buffer: output, Size: wgpu.WholeSize}}})
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
		pass.SetBindGroup(3, results, nil)
		pass.DispatchWorkgroups(uint32((len(requests)+63)/64), 1, 1)
		pass.End()
		pass.Release()
		submit(a, encoder)
		got := readBuffer(a, output, 0, uint64(len(data)))
		canonical := []byte{}
		for i, r := range requests {
			row := got[i*64 : (i+1)*64]
			u := func(offset int) uint32 { return binary.LittleEndian.Uint32(row[offset:]) }
			require((u(36) != 0) == r.occupied, fmt.Sprintf("%s production occupancy differs at %v", consumer.name, r.position))
			if consumer.kind != 3 {
				want := uint32(r.encoded)
				if packed && !r.occupied {
					want = 0
				}
				require(u(32) == want, fmt.Sprintf("%s encoded normal differs at %v", consumer.name, r.position))
				if r.occupied {
					require(u(40) == uint32((r.encoded>>14)&1), "decoded validity differs")
					if consumer.kind == 2 || r.encoded&volume.VoxelNormalValidBit != 0 {
						require(u(44) == uint32(r.encoded>>15), "two-sided bits differ")
					}
					if consumer.kind == 2 && r.encoded&volume.VoxelNormalValidBit == 0 {
						require(bytes.Equal(row[48:60], []byte{0, 0, 0, 0, 0, 0, 128, 63, 0, 0, 0, 0}), "particle invalid-normal fallback changed")
					}
				}
			}
			// Empty dense normal bytes remain authoritative but are deliberately absent
			// from packed storage; paired occupied results retain raw bits and decoding.
			if r.occupied {
				canonical = append(canonical, row[32:64]...)
			} else {
				canonical = append(canonical, row[36:40]...)
			}
		}
		save(phase+"-"+consumer.name+".bin", canonical)
		encoder.Release()
		results.Release()
		geometry.Release()
		output.Release()
		pipeline.Release()
		module.Release()
		fmt.Printf("PASS %s: %s actual helpers, %d voxel requests\n", phase, consumer.name, len(requests))
	}
}

func textureReadback(a *app.App, view *wgpu.TextureView) []byte {
	code := `@group(0) @binding(0) var source:texture_2d<f32>;
 @group(0) @binding(1) var<storage,read_write> pixels:array<vec4<f32>>;
 @compute @workgroup_size(8,8)
 fn p3a_pixels(@builtin(global_invocation_id) id:vec3<u32>) {
 let dims=textureDimensions(source);if(id.x>=dims.x||id.y>=dims.y){return;}
 pixels[id.y*dims.x+id.x]=textureLoad(source,vec2<i32>(id.xy),0);
 }`
	module, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "P3a textureLoad readback", WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: code}})
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
	pipeline, err := a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Layout: pipelineLayout, Compute: wgpu.ProgrammableStageDescriptor{Module: module, EntryPoint: "p3a_pixels"}})
	must(err)
	defer pipeline.Release()
	size := uint64(a.Config.Width) * uint64(a.Config.Height) * 16
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
	pass.DispatchWorkgroups((a.Config.Width+7)/8, (a.Config.Height+7)/8, 1)
	pass.End()
	pass.Release()
	submit(a, encoder)
	return readBuffer(a, output, 0, size)
}
func phase(a *app.App, name string) measurements {
	warm(a)
	requests, stats := samples(a)
	shaderProbes(a, name, requests)
	m := a.BufferManager
	for _, target := range []struct {
		name string
		view *wgpu.TextureView
	}{
		{"depth", m.DepthView}, {"normal", m.NormalView}, {"material", m.MaterialView},
		{"transparency", m.TransparentAccumView}, {"transparency-weight", m.TransparentWeightView},
	} {
		data := textureReadback(a, target.view)
		save(name+"-"+target.name+".bin", data)
	}
	raw, err := json.MarshalIndent(stats, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(outputDir, name+"-measurements.json"), raw, 0644))
	fmt.Printf("%s mode=%s physical auxiliary=%d logical=%d upload bytes=%d records=%d; dense traversal timing unverified\n", name, stats.Mode, stats.PhysicalAuxBytes, stats.LogicalAuxBytes, stats.UploadBytes, stats.Records)
	return stats
}
func newObject(a *app.App) *core.VoxelObject {
	o := core.NewVoxelObject()
	o.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial([4]uint8{190, 110, 50, 255}, [4]uint8{}), core.NewMaterial([4]uint8{70, 190, 90, 255}, [4]uint8{}), core.NewMaterial([4]uint8{60, 90, 210, 255}, [4]uint8{})}
	a.Scene.AddObject(o)
	return o
}
func main() {
	mode := flag.String("mode", "dense", "dense default or packed opt-in")
	flag.StringVar(&outputDir, "output", "/tmp/p3a-dense", "native result directory")
	flag.StringVar(&compareDir, "compare", "", "dense result directory for exact paired parity")
	flag.Parse()
	require(*mode == "dense" || *mode == "packed", "unknown mode")
	packed = *mode == "packed"
	must(os.MkdirAll(outputDir, 0755))
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	window, err := glfw.CreateWindow(320, 240, "P3a packed fitted normals", nil, nil)
	must(err)
	defer window.Destroy()
	a := app.NewApp(window)
	a.FeatureConfig = app.AppFeatureConfig{AutoRegisterDefaults: false}
	a.RegisterFeature(&app.TransparencyFeature{})
	a.RegisterFeature(&app.ParticlesFeature{})
	must(a.Init())
	defer a.Shutdown()
	must(a.BufferManager.SetPackedVoxelNormals(packed))
	require(a.GBufferPipeline != nil && a.BufferManager.ShadowPipeline != nil, "native voxel pipelines missing")
	require(a.AccumulationResources != nil && a.AccumulationResources.TransparentPipeline != nil, "native transparency pipeline missing")
	require(a.ParticleResources != nil && a.ParticleResources.SimPipeline != nil, "native particles pipeline missing")
	a.OcclusionMode = core.OcclusionOff
	a.Camera.Position = mgl32.Vec3{26, 29, 58}
	a.Camera.LookAt = mgl32.Vec3{18, 10, 5}
	o := newObject(a)
	// Sparse boundary samples, odd counts and raw normal validity/flag fixtures.
	for _, vi := range []int{0, 1, 31, 32, 63, 64, 511} {
		o.XBrickMap.SetVoxel(vi%8, vi/8%8, vi/64, 1)
	}
	// Thin wall, rod, fitted 3:1 staircase, mixed payload and a fully solid brick.
	for y := 0; y < 16; y++ {
		for z := 0; z < 16; z++ {
			o.XBrickMap.SetVoxel(12, y, z, 1+uint8((y+z)%3))
		}
		o.XBrickMap.SetVoxel(24, y, 0, 2)
	}
	for x := 0; x < 18; x++ {
		for z := 0; z < 8; z++ {
			o.XBrickMap.SetVoxel(x, 20+x/3, z, 1)
		}
	}
	volume.Cube(o.XBrickMap, mgl32.Vec3{32, 0, 0}, mgl32.Vec3{39, 7, 7}, 3)
	o.Transform.Scale = mgl32.Vec3{1.1, .85, 1.2}
	// Sparse sectors dominate memory; fixed distant endpoint preserves bake bounds.
	for i := 2; i < 130; i++ {
		o.XBrickMap.SetVoxel(i*32, 0, 0, 1)
	}
	o.XBrickMap.SetVoxel(4095*32, 0, 0, 1)
	authority := o.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0)
	authority.PrecomputedAux = volume.BuildVoxelAuxBytes(authority, [3]int{}, volume.VoxelNormalBakeOptions{})
	for n, vi := range []int{0, 1, 31, 32, 63, 64, 511} {
		value := uint16(0x4000 | uint16(n*123))
		if n%2 == 0 {
			value |= 0x8000
		}
		if vi == 63 {
			value = 0x9234
		}
		binary.LittleEndian.PutUint16(authority.PrecomputedAux[64+vi*2:], value)
	}
	binary.LittleEndian.PutUint16(authority.PrecomputedAux[64+2*2:], 0xffff) // dense empty raw authority must remain readable
	glass := newObject(a)
	glass.MaterialTable[1].Transparency = .45
	glass.MaterialTable[1].Transmission = .2
	volume.Cube(glass.XBrickMap, mgl32.Vec3{}, mgl32.Vec3{7, 7, 0}, 1)
	glass.Transform.Position = mgl32.Vec3{18, 2, 24}
	glass.Transform.Scale = mgl32.Vec3{.8, 1.3, .6}
	seams := []*core.VoxelObject{}
	for chunk := 0; chunk < 2; chunk++ {
		s := newObject(a)
		s.VoxelAdjacencyGroupID = 17
		s.VoxelAdjacencyChunkCoord = [3]int{chunk, 0, 0}
		s.VoxelAdjacencyChunkSize = 8
		for x := 0; x < 8; x++ {
			for z := 0; z < 8; z++ {
				s.XBrickMap.SetVoxel(x, 3, z, 2)
			}
		}
		s.Transform.Position = mgl32.Vec3{float32(chunk * 8), -8, 0}
		seams = append(seams, s)
	}
	initial := phase(a, "initial")
	// Paused membership and voxel-count growth preserve published rendered data.
	oldRender := textureReadback(a, a.BufferManager.NormalView)
	o.XBrickMap.SetVoxel(8, 0, 0, 1)
	o.XBrickMap.SetVoxel(24, 0, 1, 2)
	a.BufferManager.SetVoxelUploadBudget(gpu.VoxelUploadBudget{})
	a.Update()
	a.Render()
	a.Device.Poll(true, nil)
	require(bytes.Equal(oldRender, textureReadback(a, a.BufferManager.NormalView)), "deferred unit changed published normals")
	save("deferred-normal.bin", textureReadback(a, a.BufferManager.NormalView))
	a.BufferManager.SetVoxelUploadBudget(gpu.DefaultVoxelUploadBudget())
	phase(a, "membership")
	// Same-count occupancy reshuffle and seam edit cause rank and normal-halo work.
	o.XBrickMap.SetVoxel(24, 0, 1, 0)
	o.XBrickMap.SetVoxel(24, 0, 2, 2)
	seams[1].XBrickMap.SetVoxel(0, 3, 0, 0)
	phase(a, "reshape-halo")
	// Fixed paired demand beyond initial physical capacity, followed by removal.
	for i := 130; i < 650; i++ {
		o.XBrickMap.SetVoxel(i*32, 0, 0, 1)
	}
	grown := phase(a, "growth")
	require(grown.PhysicalAuxBytes > initial.PhysicalAuxBytes, "fixture did not force auxiliary growth")
	for i := 130; i < 650; i++ {
		o.XBrickMap.SetVoxel(i*32, 0, 0, 0)
	}
	removed := phase(a, "removal")
	require(removed.PhysicalAuxBytes >= grown.PhysicalAuxBytes, "removal unexpectedly shrank physical capacity")
	if packed {
		require(initial.LogicalAuxBytes < initial.Records*volume.VoxelAuxRecordBytes, "sparse fixture did not save logical bytes")
		if compareDir != "" {
			raw, err := os.ReadFile(filepath.Join(compareDir, "initial-measurements.json"))
			must(err)
			var dense measurements
			must(json.Unmarshal(raw, &dense))
			require(initial.PhysicalAuxBytes < dense.PhysicalAuxBytes, "sparse fixture did not reduce native physical capacity")
			fmt.Printf("Sparse paired initial physical auxiliary %d -> %d bytes; logical %d -> %d; upload %d -> %d\n", dense.PhysicalAuxBytes, initial.PhysicalAuxBytes, dense.LogicalAuxBytes, initial.LogicalAuxBytes, dense.UploadBytes, initial.UploadBytes)
		}
	}
	fmt.Println("PASS P3a native occupancy/raw normal/decoded flags/rendered depth-normal-material/transparency, edits, halos, deferral, growth and removal; GPU traversal timing unverified")
}
