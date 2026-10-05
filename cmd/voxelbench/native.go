package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
	"github.com/go-gl/mathgl/mgl32"
)

type nativeBench struct {
	device             *wgpu.Device
	queue              *wgpu.Queue
	manager            *gpu.GpuBufferManager
	pipeline, lighting *wgpu.ComputePipeline
	scene              *core.Scene
	object             *core.VoxelObject
	camera             *core.CameraState
	cfg                settings
}

func makeFixture(workload string) (*core.Scene, *core.VoxelObject) {
	s := core.NewScene()
	o := core.NewVoxelObject()
	o.AllowOcclusionCulling = false
	o.CastsShadows = false
	o.MaterialTable = []core.Material{{}, core.NewMaterial([4]uint8{200, 110, 65, 255}, [4]uint8{}), core.NewMaterial([4]uint8{65, 145, 210, 255}, [4]uint8{})}
	for z := 0; z < 32; z++ {
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				occupied := z%8 == 0
				if workload == "dense" {
					occupied = !(x%8 == 7 && y%8 == 7 && z%8 == 7)
				}
				if occupied {
					o.XBrickMap.SetVoxel(x, y, z, uint8(1+(x+y+z)%2))
				}
			}
		}
	}
	s.AddObject(o)
	return s, o
}

// Every phase keeps the outer planes/bounds intact. Reshuffles retain occupancy;
// an interior adjacent brick is removed/reintroduced to exercise membership COW.
func editFixture(o *core.VoxelObject, sample int) {
	phase := (sample + 1) % 2
	// Move an exposed patch to one of two deterministic depths. Both differ
	// from the initial front plane, even after an even number of samples.
	for y := 8; y < 16; y++ {
		for x := 8; x < 16; x++ {
			for z := 22; z <= 24; z++ {
				v := uint8(0)
				if z == 22+phase {
					v = uint8(1 + (x+y+z)%2)
				}
				o.XBrickMap.SetVoxel(x, y, z, v)
			}
		}
	}
	for z := 8; z < 16; z++ {
		for y := 8; y < 16; y++ {
			for x := 8; x < 24; x++ {
				occupied := false
				if x < 16 {
					occupied = z == 8+phase
				} else if phase == 0 {
					occupied = z == 8
				}
				v := uint8(0)
				if occupied {
					v = uint8(1 + (x+y+z)%2)
				}
				o.XBrickMap.SetVoxel(x, y, z, v)
			}
		}
	}
	// Compensating adjacent brick admission keeps the total occupied count fixed.
	for z := 16; z < 24; z++ {
		for y := 8; y < 16; y++ {
			for x := 16; x < 24; x++ {
				v := uint8(0)
				if (phase == 0 && z == 16) || (phase == 1 && (z == 16 || z == 17)) {
					v = uint8(1 + (x+y+z)%2)
				}
				o.XBrickMap.SetVoxel(x, y, z, v)
			}
		}
	}
}
func geometry(o *core.VoxelObject) geometryStats {
	data := make([]byte, 0, 32*32*32)
	occupied := 0
	bricks := map[[3]int]bool{}
	for z := 0; z < 32; z++ {
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				occ, v := o.XBrickMap.GetVoxel(x, y, z)
				data = append(data, byte(v))
				if occ {
					occupied++
					bricks[[3]int{x / 8, y / 8, z / 8}] = true
				}
			}
		}
	}
	return geometryStats{occupied, len(bricks), fmt.Sprintf("%x", sha256.Sum256(data))}
}

func createPipeline(d *wgpu.Device, code, label string, layout *wgpu.PipelineLayout) (*wgpu.ComputePipeline, error) {
	mod, err := d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: label, WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: code}})
	if err != nil {
		return nil, err
	}
	defer mod.Release()
	return d.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Label: label, Layout: layout, Compute: wgpu.ProgrammableStageDescriptor{Module: mod, EntryPoint: "main"}})
}

// CreateGBufferBindGroups also builds an unused lighting scene group. This
// explicit provider supplies that group's production buffer types; no lighting
// or substitute traversal is recorded by the benchmark.
func lightingLayoutProvider(d *wgpu.Device) (*wgpu.ComputePipeline, error) {
	entries := []wgpu.BindGroupLayoutEntry{}
	for i := uint32(0); i < 3; i++ {
		typ := wgpu.BufferBindingTypeReadOnlyStorage
		if i == 0 {
			typ = wgpu.BufferBindingTypeUniform
		}
		entries = append(entries, wgpu.BindGroupLayoutEntry{Binding: i, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: typ}})
	}
	bgl, err := d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: entries})
	if err != nil {
		return nil, err
	}
	defer bgl.Release()
	layout, err := d.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{bgl}})
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	return createPipeline(d, "@compute @workgroup_size(1) fn main() {}", "unused lighting group layout provider", layout)
}
func (n *nativeBench) submit(e *wgpu.CommandEncoder) error {
	commands, err := e.Finish(nil)
	if err != nil {
		return err
	}
	defer commands.Release()
	index := n.queue.Submit(commands)
	n.manager.MarkRetiredBuffersSubmitted(n.queue, index)
	n.device.Poll(true, nil)
	n.manager.AdvanceRetiredBuffers()
	return nil
}

// A single real G-buffer compute pass holds the entire dispatch batch. Pass
// timestamps bracket GPU work without requiring native encoder-write features.
type timestampPair struct {
	queries *wgpu.QuerySet
	first   uint32
}

func (n *nativeBench) dispatch(e *wgpu.CommandEncoder, batch int, queries ...timestampPair) error {
	var desc *wgpu.ComputePassDescriptor
	if len(queries) > 0 {
		desc = &wgpu.ComputePassDescriptor{TimestampWrites: &wgpu.ComputePassTimestampWrites{QuerySet: queries[0].queries, BeginningOfPassWriteIndex: queries[0].first, EndOfPassWriteIndex: queries[0].first + 1}}
	}
	p := e.BeginComputePass(desc)
	p.SetPipeline(n.pipeline)
	p.SetBindGroup(0, n.manager.GBufferBindGroup0, nil)
	p.SetBindGroup(1, n.manager.GBufferBindGroup, nil)
	p.SetBindGroup(2, n.manager.GBufferBindGroup2, nil)
	for i := 0; i < batch; i++ {
		p.DispatchWorkgroups(uint32((n.cfg.Width+7)/8), uint32((n.cfg.Height+7)/8), 1)
	}
	err := p.End()
	p.Release()
	return err
}
func (n *nativeBench) timedBatch(queries *wgpu.QuerySet, firstQuery uint32, resolved, readback *wgpu.Buffer) ([]uint64, error) {
	e, err := n.device.CreateCommandEncoder(nil)
	if err != nil {
		return nil, err
	}
	err = n.dispatch(e, n.cfg.Batch, timestampPair{queries, firstQuery})
	if err == nil {
		err = n.submit(e)
	}
	e.Release()
	if err != nil {
		return nil, err
	}
	// Native query results become available after the timed pass completes.
	// Resolve/readback uses a fresh submission, outside both timestamp bounds.
	resolveEncoder, err := n.device.CreateCommandEncoder(nil)
	if err != nil {
		return nil, err
	}
	defer resolveEncoder.Release()
	err = resolveEncoder.ResolveQuerySet(queries, firstQuery, 2, resolved, 0)
	if err == nil {
		err = resolveEncoder.CopyBufferToBuffer(resolved, 0, readback, 0, 16)
	}
	if err == nil {
		err = n.submit(resolveEncoder)
	}
	if err != nil {
		return nil, err
	}
	data, err := readBuffer(n.device, readback, 16)
	if err != nil {
		return nil, err
	}
	return []uint64{binary.LittleEndian.Uint64(data), binary.LittleEndian.Uint64(data[8:])}, nil
}
func (n *nativeBench) update() (cpuCost, error) {
	var cost cpuCost
	aspect := float32(n.cfg.Width) / float32(n.cfg.Height)
	view := n.camera.GetViewMatrix()
	proj := n.camera.ProjectionMatrix(aspect)
	vp := proj.Mul4(view)
	for attempt := 0; attempt < 512; attempt++ {
		start := time.Now()
		n.scene.Commit(n.camera.ExtractFrustum(vp), core.SceneCommitOptions{OcclusionMode: core.OcclusionOff, CameraPosition: n.camera.Position})
		recreated := n.manager.UpdateScene(n.scene, n.camera, aspect, mgl32.Vec3{})
		cost.UpdateNS += time.Since(start).Nanoseconds()
		cost.NormalBakeNS += n.manager.VoxelRuntimeNormalBakeDuration.Nanoseconds()
		cost.UploadBytes += n.manager.VoxelUploadBytes
		if recreated || n.manager.GBufferBindGroup0 == nil || !n.manager.GBufferSceneBindGroupCurrent() {
			n.manager.CreateGBufferBindGroups(n.pipeline, n.lighting)
		}
		n.manager.UpdateCamera(vp, view.Inv(), proj.Inv(), n.camera.Position, mgl32.Vec3{30, 60, 50}, mgl32.Vec3{.2, .2, .2}, mgl32.Vec3{}, 1, 0, n.camera.FarPlane(), 0, 0, 0, uint32(n.cfg.Width), uint32(n.cfg.Height), core.DefaultLightingQualityConfig())
		// Queue writes and internal growth copies require a real submission fence.
		e, err := n.device.CreateCommandEncoder(nil)
		if err != nil {
			return cost, err
		}
		err = n.submit(e)
		e.Release()
		if err != nil {
			return cost, err
		}
		ready, _, _ := n.manager.RenderVoxelObjectReady(n.object, n.object.XBrickMap, n.object.XBrickMap.Revision)
		if ready && !n.manager.VoxelGPUWorkStats().Pending && n.manager.VoxelDirtySectorsPending == 0 && n.manager.VoxelDirtyBricksPending == 0 {
			return cost, nil
		}
	}
	return cost, fmt.Errorf("voxel admission did not drain within 512 updates")
}
func readBuffer(d *wgpu.Device, b *wgpu.Buffer, size uint64) ([]byte, error) {
	done := false
	var status wgpu.BufferMapAsyncStatus
	if err := b.MapAsync(wgpu.MapModeRead, 0, size, func(s wgpu.BufferMapAsyncStatus) { status = s; done = true }); err != nil {
		return nil, err
	}
	for i := 0; i < 512 && !done; i++ {
		d.Poll(true, nil)
	}
	if !done || status != wgpu.BufferMapAsyncStatusSuccess {
		return nil, fmt.Errorf("GPU readback failed: status %v", status)
	}
	data := append([]byte(nil), b.GetMappedRange(0, uint(size))...)
	b.Unmap()
	return data, nil
}

const captureWGSL = `@group(0) @binding(0) var src: texture_2d<f32>;
@group(0) @binding(1) var<storage,read_write> dst: array<vec4<f32>>;
@compute @workgroup_size(8,8) fn main(@builtin(global_invocation_id) id: vec3<u32>) {
let dim=textureDimensions(src); if id.x>=dim.x || id.y>=dim.y{return;}
dst[id.y*dim.x+id.x]=textureLoad(src,vec2<i32>(id.xy),0);}`

func (n *nativeBench) capture() (capture, error) {
	var result capture
	// RGBA16Float normal is read as float32, preserving exact half-float values.
	size := uint64(n.cfg.Width) * uint64(n.cfg.Height) * 16
	storage, err := n.device.CreateBuffer(&wgpu.BufferDescriptor{Size: size, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc})
	if err != nil {
		return result, err
	}
	defer storage.Release()
	readback, err := n.device.CreateBuffer(&wgpu.BufferDescriptor{Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	if err != nil {
		return result, err
	}
	defer readback.Release()
	bgl, err := n.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []wgpu.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageCompute, Texture: wgpu.TextureBindingLayout{SampleType: wgpu.TextureSampleTypeUnfilterableFloat, ViewDimension: wgpu.TextureViewDimension2D}}, {Binding: 1, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeStorage}}}})
	if err != nil {
		return result, err
	}
	defer bgl.Release()
	layout, err := n.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{bgl}})
	if err != nil {
		return result, err
	}
	defer layout.Release()
	pipeline, err := createPipeline(n.device, captureWGSL, "parity texture readback", layout)
	if err != nil {
		return result, err
	}
	defer pipeline.Release()
	var depth, normal []byte
	for i, view := range []*wgpu.TextureView{n.manager.DepthView, n.manager.NormalView, n.manager.MaterialView} {
		bg, err := n.device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: bgl, Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: view}, {Binding: 1, Buffer: storage, Size: wgpu.WholeSize}}})
		if err != nil {
			return result, err
		}
		e, err := n.device.CreateCommandEncoder(nil)
		if err != nil {
			bg.Release()
			return result, err
		}
		if i == 0 {
			if err = n.dispatch(e, 1); err != nil {
				e.Release()
				bg.Release()
				return result, err
			}
		}
		p := e.BeginComputePass(nil)
		p.SetPipeline(pipeline)
		p.SetBindGroup(0, bg, nil)
		p.DispatchWorkgroups(uint32((n.cfg.Width+7)/8), uint32((n.cfg.Height+7)/8), 1)
		err = p.End()
		p.Release()
		if err == nil {
			err = e.CopyBufferToBuffer(storage, 0, readback, 0, size)
		}
		if err == nil {
			err = n.submit(e)
		}
		e.Release()
		bg.Release()
		if err != nil {
			return result, err
		}
		data, err := readBuffer(n.device, readback, size)
		if err != nil {
			return result, err
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(data))
		switch i {
		case 0:
			result.Depth = hash
			depth = data
		case 1:
			result.Normal = hash
			normal = data
		case 2:
			result.Material = hash
		}
	}
	for offset := 0; offset < len(depth); offset += 16 {
		t := math.Float32frombits(binary.LittleEndian.Uint32(depth[offset:]))
		if !math.IsNaN(float64(t)) && !math.IsInf(float64(t), 0) && t > 0 && t < n.camera.FarPlane() {
			length := float64(0)
			valid := true
			for j := 0; j < 3; j++ {
				v := float64(math.Float32frombits(binary.LittleEndian.Uint32(normal[offset+j*4:])))
				valid = valid && !math.IsNaN(v) && !math.IsInf(v, 0)
				length += v * v
			}
			if !valid || length == 0 {
				return result, fmt.Errorf("hit pixel has invalid normal")
			}
			result.HitPixels++
		}
	}
	if result.HitPixels == 0 {
		return result, fmt.Errorf("G-buffer capture has no valid hit pixels")
	}
	return result, nil
}

// Release current public manager resources, including texture arrays. Retired
// resources have already been completed/advanced before this runs.
func releaseManager(m *gpu.GpuBufferManager) {
	seen := map[any]bool{}
	var visit func(reflect.Value)
	visit = func(v reflect.Value) {
		if !v.CanInterface() {
			return
		}
		if v.Kind() == reflect.Array {
			for i := 0; i < v.Len(); i++ {
				visit(v.Index(i))
			}
			return
		}
		if v.Kind() != reflect.Pointer || v.IsNil() {
			return
		}
		x := v.Interface()
		if seen[x] {
			return
		}
		switch r := x.(type) {
		case *wgpu.Buffer:
			r.Release()
		case *wgpu.Texture:
			r.Release()
		case *wgpu.TextureView:
			r.Release()
		case *wgpu.BindGroup:
			r.Release()
		case *wgpu.Sampler:
			r.Release()
		default:
			return
		}
		seen[x] = true
	}
	v := reflect.ValueOf(m).Elem()
	for i := 0; i < v.NumField(); i++ {
		visit(v.Field(i))
	}
}

func benchmark(mode, materials string, cfg settings) (r report, err error) {
	r = report{Version: 2, Mode: mode, Materials: materials, Settings: cfg, GPUUnit: "raw_gpu_ticks", Shader: fmt.Sprintf("%x", sha256.Sum256([]byte(shaders.GBufferWGSL)))}
	r.GPUTimingMethod = "compute_pass_boundaries_completed_resolve_v1"
	r.QueryCohortSize = timestampCohortSize
	r.QueryCohortCount, r.QueryMaxCount = timestampQueryLayout(cfg.Samples)
	instance := wgpu.CreateInstance(nil)
	defer instance.Release()
	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{PowerPreference: wgpu.PowerPreferenceHighPerformance})
	if err != nil {
		return r, fmt.Errorf("headless adapter: %w", err)
	}
	defer adapter.Release()
	info := adapter.GetInfo()
	r.Adapter = fmt.Sprintf("%v", info)
	r.Backend = fmt.Sprintf("%v", info.BackendType)
	limits := adapter.GetLimits().Limits
	scratch := uint64(cfg.Width) * uint64(cfg.Height) * 16
	if uint32(cfg.Width) > limits.MaxTextureDimension2D || uint32(cfg.Height) > limits.MaxTextureDimension2D || scratch > uint64(limits.MaxStorageBufferBindingSize) || scratch > limits.MaxBufferSize {
		return r, fmt.Errorf("resolution exceeds adapter texture or parity storage limits")
	}
	availableFeatures := make(map[string]wgpu.FeatureName)
	for _, feature := range adapter.EnumerateFeatures() {
		availableFeatures[feature.String()] = feature
	}
	features := requiredPassTimestampFeatures(availableFeatures)
	device, err := adapter.RequestDevice(&wgpu.DeviceDescriptor{RequiredLimits: &wgpu.RequiredLimits{Limits: limits}, RequiredFeatures: features})
	if err != nil {
		return r, err
	}
	defer device.Release()
	queue := device.GetQueue()
	defer queue.Release()
	manager := gpu.NewGpuBufferManager(device, core.NewProfiler())
	defer releaseManager(manager)
	if err = manager.SetPackedVoxelNormals(mode == "packed"); err != nil {
		return r, err
	}
	if err = manager.SetPackedVoxelMaterials(materials == "packed"); err != nil {
		return r, err
	}
	manager.CreateGBufferTextures(uint32(cfg.Width), uint32(cfg.Height))
	pipeline, err := createPipeline(device, shaders.GBufferWGSL, "production G-buffer", nil)
	if err != nil {
		return r, err
	}
	defer pipeline.Release()
	lighting, err := lightingLayoutProvider(device)
	if err != nil {
		return r, err
	}
	defer lighting.Release()
	scene, obj := makeFixture(cfg.Workload)
	r.Fixture = "voxelbench-v1-32cube-edit-frontpatch-adjacent-v1"
	r.InitialGeometry = geometry(obj)
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{16, 16, 68}
	camera.LookAt = mgl32.Vec3{16, 16, 16}
	camera.Far = 160
	n := nativeBench{device, queue, manager, pipeline, lighting, scene, obj, camera, cfg}
	r.Initial, err = n.update()
	if err != nil {
		return r, err
	}
	for i := 0; i < cfg.Warmup; i++ {
		e, eerr := device.CreateCommandEncoder(nil)
		if eerr != nil {
			return r, eerr
		}
		err = n.dispatch(e, cfg.Batch)
		if err == nil {
			err = n.submit(e)
		}
		e.Release()
		if err != nil {
			return r, err
		}
	}
	r.InitialCapture, err = n.capture()
	if err != nil {
		return r, err
	}
	var queries *wgpu.QuerySet
	defer func() {
		if queries != nil {
			queries.Release()
		}
	}()
	var resolved, readback *wgpu.Buffer
	if len(features) > 0 {
		resolved, err = device.CreateBuffer(&wgpu.BufferDescriptor{Size: 256, Usage: wgpu.BufferUsageQueryResolve | wgpu.BufferUsageCopySrc})
		if err == nil {
			defer resolved.Release()
			readback, err = device.CreateBuffer(&wgpu.BufferDescriptor{Size: 16, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
		}
		if err == nil {
			defer readback.Release()
			r.GPUAvailable = true
		} else {
			r.Warning = fmt.Sprintf("GPU timestamp resources unavailable: %v", err)
			err = nil
		}
	} else {
		r.Warning = "adapter lacks timestamp-query; GPU timing unavailable"
	}
	for sample := 0; sample < cfg.Samples; sample++ {
		// Each cohort owns fresh query slots; no slot is rewritten. The previous
		// cohort's last submit/readback completed before its query set is released.
		if r.GPUAvailable && sample%timestampCohortSize == 0 {
			if queries != nil {
				queries.Release()
				queries = nil
			}
			_, queryCount := timestampQueryLayout(min(cfg.Samples-sample, timestampCohortSize))
			queries, err = device.CreateQuerySet(&wgpu.QuerySetDescriptor{Type: wgpu.QueryTypeTimestamp, Count: queryCount})
			if err != nil {
				r.GPUAvailable = false
				r.Warning = fmt.Sprintf("GPU timestamp cohort unavailable: %v", err)
				err = nil
			} else {
				calibrated := false
				for attempt := 0; attempt < 3; attempt++ {
					pair, eerr := n.timedBatch(queries, uint32(attempt*2), resolved, readback)
					if eerr != nil {
						return r, eerr
					}
					r.CalibrationCount++
					r.CalibrationTicks = append(r.CalibrationTicks, pair...)
					if pair[1] > pair[0] {
						calibrated = true
						break
					}
				}
				if !calibrated {
					r.GPUAvailable = false
					r.Warning = "GPU timing unavailable: no positive timestamp pair in three cohort calibration attempts"
				}
			}
		}
		if cfg.Workload == "edited" {
			editFixture(obj, sample)
		}
		cost, eerr := n.update()
		if eerr != nil {
			return r, eerr
		}
		r.CPU = append(r.CPU, cost)
		if r.GPUAvailable {
			pair, eerr := n.timedBatch(queries, uint32(6+2*(sample%timestampCohortSize)), resolved, readback)
			if eerr != nil {
				return r, eerr
			}
			r.RawTicks = append(r.RawTicks, pair...)
		} else {
			e, eerr := device.CreateCommandEncoder(nil)
			if eerr != nil {
				return r, eerr
			}
			err = n.dispatch(e, cfg.Batch)
			if err == nil {
				err = n.submit(e)
			}
			e.Release()
			if err != nil {
				return r, err
			}
		}
	}
	if r.GPUAvailable {
		summary, eerr := summarizeTimestamps(r.RawTicks, uint32(cfg.Batch))
		if eerr != nil {
			r.GPUAvailable = false
			r.Warning = fmt.Sprintf("GPU timing unavailable: %v", eerr)
		} else {
			r.GPU = &summary
		}
	}
	r.FinalCapture, err = n.capture()
	if err != nil {
		return r, err
	}
	if cfg.Workload == "edited" && r.FinalCapture.Depth == r.InitialCapture.Depth && r.FinalCapture.Normal == r.InitialCapture.Normal {
		return r, fmt.Errorf("edited fixture did not change rendered depth or normals")
	}
	r.AuxiliaryCapacityBytes = manager.DenseOccupancyBuf.GetSize()
	// R8Uint payload textures retain their physical capacity even when packed
	// material records avoid assigning atlas slots. Count all bound textures,
	// including mandatory placeholder pages, using their actual dimensions.
	for _, texture := range manager.VoxelPayloadTex {
		if texture != nil {
			r.PayloadAtlasCapacityBytes += uint64(texture.GetWidth()) * uint64(texture.GetHeight()) * uint64(texture.GetDepthOrArrayLayers())
		}
	}
	r.PayloadAssignedBytes = uint64(len(manager.BrickToSlot)) * 512
	r.FinalGeometry = geometry(obj)
	device.Poll(true, nil)
	manager.AdvanceRetiredBuffers()
	return r, nil
}
