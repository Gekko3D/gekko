package gpu

import (
	"encoding/binary"
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

// ============== COMPRESSION MANAGEMENT ==============

// CreateCompressionPipeline creates the compute pipeline for brick compression
func (m *GpuBufferManager) CreateCompressionPipeline(shaderCode string) error {
	// Create shader module
	shaderDesc := &wgpu.ShaderModuleDescriptor{
		Label: "CompressionShader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{
			Code: shaderCode,
		},
	}
	shaderModule, err := m.Device.CreateShaderModule(shaderDesc)
	if err != nil {
		return fmt.Errorf("failed to create compression shader module: %w", err)
	}
	defer shaderModule.Release()

	// Create compute pipeline
	pipelineDesc := &wgpu.ComputePipelineDescriptor{
		Label: "CompressionPipeline",
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     shaderModule,
			EntryPoint: "compress_bricks",
		},
	}
	m.CompressionPipeline, err = m.Device.CreateComputePipeline(pipelineDesc)
	if err != nil {
		return fmt.Errorf("failed to create compression pipeline: %w", err)
	}

	return nil
}

// InitializeCompressionBuffers creates buffers for compression system
func (m *GpuBufferManager) InitializeCompressionBuffers() error {
	var err error

	// Compression params buffer (16 bytes header + 1024 u32 indices = 4112 bytes)
	m.CompressionParamsBuf, err = m.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "CompressionParams",
		Size:  4112,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("failed to create compression params buffer: %w", err)
	}

	// Payload free queue (16 bytes × 1024 entries = 16KB)
	m.PayloadFreeQueueBuf, err = m.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "PayloadFreeQueue",
		Size:  16 * 1024,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst | wgpu.BufferUsageCopySrc | wgpu.BufferUsageMapRead,
	})
	if err != nil {
		return fmt.Errorf("failed to create payload free queue buffer: %w", err)
	}

	// Free queue counter
	m.FreeQueueCounterBuf, err = m.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "FreeQueueCounter",
		Size:  4,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst | wgpu.BufferUsageCopySrc | wgpu.BufferUsageMapRead,
	})
	if err != nil {
		return fmt.Errorf("failed to create free queue counter buffer: %w", err)
	}

	// Initialize counter to 0
	zero := make([]byte, 4)
	m.Device.GetQueue().WriteBuffer(m.FreeQueueCounterBuf, 0, zero)

	return nil
}

// CreateCompressionBindGroups creates bind groups for compression pipeline
func (m *GpuBufferManager) CreateCompressionBindGroups() error {
	if m.CompressionPipeline == nil {
		return fmt.Errorf("compression pipeline not created")
	}

	var err error

	// Group 0: Compression parameters
	entries0 := []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.CompressionParamsBuf, Size: wgpu.WholeSize},
	}
	desc0 := &wgpu.BindGroupDescriptor{
		Layout:  m.CompressionPipeline.GetBindGroupLayout(0),
		Entries: entries0,
	}
	m.CompressionBindGroup0, err = m.Device.CreateBindGroup(desc0)
	if err != nil {
		return fmt.Errorf("failed to create compression bind group 0: %w", err)
	}

	// Group 1: Brick data (reuse existing buffers)
	entries1 := []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
		{Binding: 1, Buffer: m.BrickPoolPayloadBuf, Size: wgpu.WholeSize},
	}
	desc1 := &wgpu.BindGroupDescriptor{
		Layout:  m.CompressionPipeline.GetBindGroupLayout(1),
		Entries: entries1,
	}
	m.CompressionBindGroup1, err = m.Device.CreateBindGroup(desc1)
	if err != nil {
		return fmt.Errorf("failed to create compression bind group 1: %w", err)
	}

	// Group 2: Free queue
	entries2 := []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.PayloadFreeQueueBuf, Size: wgpu.WholeSize},
		{Binding: 1, Buffer: m.FreeQueueCounterBuf, Size: wgpu.WholeSize},
	}
	desc2 := &wgpu.BindGroupDescriptor{
		Layout:  m.CompressionPipeline.GetBindGroupLayout(2),
		Entries: entries2,
	}
	m.CompressionBindGroup2, err = m.Device.CreateBindGroup(desc2)
	if err != nil {
		return fmt.Errorf("failed to create compression bind group 2: %w", err)
	}

	return nil
}

// CompressBricks runs compression pass on specified brick indices
func (m *GpuBufferManager) CompressBricks(brickIndices []uint32) {
	if len(brickIndices) == 0 || m.CompressionPipeline == nil {
		return
	}

	// Limit to 1024 bricks per pass
	count := len(brickIndices)
	if count > 1024 {
		count = 1024
	}

	// Prepare compression params
	paramsData := make([]byte, 4112)
	binary.LittleEndian.PutUint32(paramsData[0:4], uint32(count))

	// Write brick indices (starting at offset 16)
	for i := 0; i < count; i++ {
		offset := 16 + i*4
		binary.LittleEndian.PutUint32(paramsData[offset:], brickIndices[i])
	}

	m.Device.GetQueue().WriteBuffer(m.CompressionParamsBuf, 0, paramsData)

	// Reset free queue counter
	zero := make([]byte, 4)
	m.Device.GetQueue().WriteBuffer(m.FreeQueueCounterBuf, 0, zero)

	// Dispatch compression shader
	encoder, err := m.Device.CreateCommandEncoder(nil)
	if err != nil {
		return
	}

	computePass := encoder.BeginComputePass(nil)
	computePass.SetPipeline(m.CompressionPipeline)
	computePass.SetBindGroup(0, m.CompressionBindGroup0, nil)
	computePass.SetBindGroup(1, m.CompressionBindGroup1, nil)
	computePass.SetBindGroup(2, m.CompressionBindGroup2, nil)

	// Calculate workgroups (64 threads per workgroup)
	workgroups := (uint32(count) + 63) / 64
	computePass.DispatchWorkgroups(workgroups, 1, 1)
	computePass.End()

	cmdBuf, err := encoder.Finish(nil)
	if err != nil {
		return
	}

	m.Device.GetQueue().Submit(cmdBuf)

	// TODO: Process free queue (async readback)
	// For now, freed payloads are not recycled
}

// TrackDirtyBrick adds a brick index to the dirty list for compression
func (m *GpuBufferManager) TrackDirtyBrick(brickIdx uint32) {
	// Avoid duplicates (simple linear search for small lists)
	for _, idx := range m.DirtyBrickIndices {
		if idx == brickIdx {
			return
		}
	}
	m.DirtyBrickIndices = append(m.DirtyBrickIndices, brickIdx)
}

// FlushCompression runs compression on all dirty bricks and clears the list
func (m *GpuBufferManager) FlushCompression() {
	if len(m.DirtyBrickIndices) > 0 {
		m.CompressBricks(m.DirtyBrickIndices)
		m.DirtyBrickIndices = m.DirtyBrickIndices[:0]
	}
}
