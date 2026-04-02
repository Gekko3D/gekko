package gpu

import (
	"math"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
)

func (m *GpuBufferManager) UpdateParticles(maxCount uint32, emitters []byte) bool {
	recreated := false

	if maxCount == 0 {
		maxCount = 1024
	}

	if m.MaxParticleCount != maxCount {
		m.MaxParticleCount = maxCount
		// Reallocate all buffers
		m.ensureBuffer("ParticlePoolBuf", &m.ParticlePoolBuf, nil, wgpu.BufferUsageStorage, int(maxCount)*80)
		m.ensureBuffer("ParticleDeadPoolBuf", &m.ParticleDeadPoolBuf, nil, wgpu.BufferUsageStorage, int(maxCount)*4)
		m.ensureBuffer("ParticleAliveListBuf", &m.ParticleAliveListBuf, nil, wgpu.BufferUsageStorage, int(maxCount)*4)
		m.ensureBuffer("ParticleCountersBuf", &m.ParticleCountersBuf, nil, wgpu.BufferUsageStorage, 64)
		m.ensureBuffer("ParticleIndirectBuf", &m.ParticleIndirectBuf, nil, wgpu.BufferUsageStorage|wgpu.BufferUsageIndirect, 64)
		m.ensureBuffer("ParticleParamsBuf", &m.ParticleParamsBuf, nil, wgpu.BufferUsageUniform, 64)
		m.ensureBuffer("ParticleSpawnBuf", &m.ParticleSpawnBuf, nil, wgpu.BufferUsageStorage, int(maxCount)*8) // Space for spawn requests

		// Initialize DeadPool with all indices
		deadIndices := make([]uint32, maxCount)
		for i := uint32(0); i < maxCount; i++ {
			deadIndices[i] = i
		}
		deadBytes := unsafe.Slice((*byte)(unsafe.Pointer(&deadIndices[0])), maxCount*4)
		m.Device.GetQueue().WriteBuffer(m.ParticleDeadPoolBuf, 0, deadBytes)

		// Initialize Counters
		counters := make([]uint32, 4)
		counters[0] = maxCount // dead_count = max
		counters[1] = 0        // alive_count = 0
		countersBytes := unsafe.Slice((*byte)(unsafe.Pointer(&counters[0])), 16)
		m.Device.GetQueue().WriteBuffer(m.ParticleCountersBuf, 0, countersBytes)

		// Initialize Indirect Args
		indirectArgs := []uint32{6, 0, 0, 0} // vertex_count=6, instance_count=0
		indirectBytes := unsafe.Slice((*byte)(unsafe.Pointer(&indirectArgs[0])), 16)
		m.Device.GetQueue().WriteBuffer(m.ParticleIndirectBuf, 0, indirectBytes)

		recreated = true
	}

	// Update Emitters
	if len(emitters) > 0 {
		m.ParticleSystemActive = true
		if m.ensureBuffer("ParticleEmittersBuf", &m.ParticleEmittersBuf, emitters, wgpu.BufferUsageStorage, 0) {
			recreated = true
		}
	} else {
		// Ensure it exists
		if m.ensureBuffer("ParticleEmittersBuf", &m.ParticleEmittersBuf, make([]byte, 64), wgpu.BufferUsageStorage, 0) {
			recreated = true
		}
	}

	return recreated
}

// CreateParticlesBindGroups wires camera + pool + alive_list (group 0) and gbuffer depth (group 1)
func (m *GpuBufferManager) CreateParticlesBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil {
		return
	}
	var err error
	m.ParticlesBindGroup0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.ParticlePoolBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.ParticleAliveListBuf, Size: wgpu.WholeSize},
			{Binding: 3, TextureView: m.ParticleAtlasView},
			{Binding: 4, Sampler: m.ParticleAtlasSampler},
		},
	})
	if err != nil {
		panic(err)
	}
	m.ParticlesBindGroup1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
		},
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) CreateParticleSimBindGroups() {
	if m.ParticleSimPipeline == nil {
		return
	}
	var err error
	m.ParticleSimBG0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.ParticleSimPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.ParticleParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.ParticlePoolBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.ParticleDeadPoolBuf, Size: wgpu.WholeSize},
			{Binding: 3, Buffer: m.ParticleAliveListBuf, Size: wgpu.WholeSize},
			{Binding: 4, Buffer: m.ParticleCountersBuf, Size: wgpu.WholeSize},
			{Binding: 5, Buffer: m.ParticleIndirectBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
	m.ParticleSimBG1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.ParticleSimPipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.ParticleEmittersBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.ParticleSpawnBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.ParticleSimBG2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.ParticleSimPipeline.GetBindGroupLayout(2),
		Entries: m.appendVoxelPayloadEntries([]wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
			{Binding: 6, Buffer: m.MaterialBuf, Size: wgpu.WholeSize},
			{Binding: 7, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize},
			{Binding: 8, Buffer: m.InstancesBuf, Size: wgpu.WholeSize},
			{Binding: 9, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
			{Binding: 10, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize},
		}, 2),
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) UpdateParticleParams(dt, invVsize float32, seed uint32, emitterCount uint32) {
	data := make([]uint32, 8)
	data[0] = math.Float32bits(dt)
	data[1] = seed
	data[2] = m.MaxParticleCount
	data[3] = emitterCount
	data[4] = math.Float32bits(invVsize)
	// padding 4,5,6,7 remains 0

	bytes := unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), 32)
	m.Device.GetQueue().WriteBuffer(m.ParticleParamsBuf, 0, bytes)
}

func (m *GpuBufferManager) DispatchParticleSim(encoder *wgpu.CommandEncoder, initPipe, simPipe *wgpu.ComputePipeline) {
	if initPipe == nil || simPipe == nil || m.ParticleSimBG0 == nil {
		return
	}

	// 1. Reset counters (alive count)
	pass := encoder.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "Particle Init"})
	pass.SetPipeline(initPipe)
	pass.SetBindGroup(0, m.ParticleSimBG0, nil)
	pass.SetBindGroup(1, m.ParticleSimBG1, nil)
	if m.ParticleSimBG2 != nil {
		pass.SetBindGroup(2, m.ParticleSimBG2, nil)
	}
	pass.DispatchWorkgroups(1, 1, 1) // EntryPoint: init_draw_args
	pass.End()

	// 2. Simulate
	pass = encoder.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "Particle Sim"})
	pass.SetPipeline(simPipe)
	pass.SetBindGroup(0, m.ParticleSimBG0, nil)
	pass.SetBindGroup(1, m.ParticleSimBG1, nil)
	if m.ParticleSimBG2 != nil {
		pass.SetBindGroup(2, m.ParticleSimBG2, nil)
	}
	wgCount := (m.MaxParticleCount + 63) / 64
	pass.DispatchWorkgroups(wgCount, 1, 1) // EntryPoint: simulate
	pass.End()
}

func (m *GpuBufferManager) DispatchParticleSpawn(encoder *wgpu.CommandEncoder, spawnPipe, finalizePipe *wgpu.ComputePipeline, spawnCount uint32) {
	if spawnPipe == nil || finalizePipe == nil || m.ParticleSimBG0 == nil {
		return
	}

	if spawnCount > 0 {
		pass := encoder.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "Particle Spawn"})
		pass.SetPipeline(spawnPipe)
		pass.SetBindGroup(0, m.ParticleSimBG0, nil)
		pass.SetBindGroup(1, m.ParticleSimBG1, nil)
		if m.ParticleSimBG2 != nil {
			pass.SetBindGroup(2, m.ParticleSimBG2, nil)
		}
		wgCount := (spawnCount + 63) / 64
		pass.DispatchWorkgroups(wgCount, 1, 1) // EntryPoint: spawn
		pass.End()
	}

	// Finalize draw args
	pass := encoder.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "Particle Finalize"})
	pass.SetPipeline(finalizePipe)
	pass.SetBindGroup(0, m.ParticleSimBG0, nil)
	pass.SetBindGroup(1, m.ParticleSimBG1, nil)
	if m.ParticleSimBG2 != nil {
		pass.SetBindGroup(2, m.ParticleSimBG2, nil)
	}
	pass.DispatchWorkgroups(1, 1, 1) // EntryPoint: finalize_draw_args
	pass.End()
}

func (m *GpuBufferManager) UpdateSpawnRequests(requests []uint32) {
	if len(requests) == 0 {
		// Zero the spawn_request_count in hardware counters
		zero := uint32(0)
		m.Device.GetQueue().WriteBuffer(m.ParticleCountersBuf, 8, unsafe.Slice((*byte)(unsafe.Pointer(&zero)), 4))
		return
	}

	m.ParticleSystemActive = true
	count := uint32(len(requests))
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(&requests[0])), count*4)
	m.Device.GetQueue().WriteBuffer(m.ParticleSpawnBuf, 0, bytes)

	// Update spawn_request_count in counters (offset 8)
	m.Device.GetQueue().WriteBuffer(m.ParticleCountersBuf, 8, unsafe.Slice((*byte)(unsafe.Pointer(&count)), 4))
}

func (m *GpuBufferManager) CreateDefaultParticleAtlas() {
	tex, err := m.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "Default Particle Atlas",
		Usage: wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
		Size: wgpu.Extent3D{
			Width:              1,
			Height:             1,
			DepthOrArrayLayers: 1,
		},
		Format:        wgpu.TextureFormatRGBA8Unorm,
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
	})
	if err == nil {
		m.Device.GetQueue().WriteTexture(tex.AsImageCopy(), []byte{255, 255, 255, 255}, &wgpu.TextureDataLayout{
			BytesPerRow:  4,
			RowsPerImage: 1,
		}, &wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1})

		m.ParticleAtlasTex = tex
		m.ParticleAtlasView, _ = tex.CreateView(nil)
		m.ParticleAtlasSampler, _ = m.Device.CreateSampler(&wgpu.SamplerDescriptor{
			MagFilter:     wgpu.FilterModeLinear,
			MinFilter:     wgpu.FilterModeLinear,
			MipmapFilter:  wgpu.MipmapFilterModeLinear,
			AddressModeU:  wgpu.AddressModeClampToEdge,
			AddressModeV:  wgpu.AddressModeClampToEdge,
			LodMinClamp:   0,
			LodMaxClamp:   0,
			MaxAnisotropy: 1,
		})
	}
}
