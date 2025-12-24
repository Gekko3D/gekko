package gpu

import (
	"encoding/binary"
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

// ============== GPU VOXEL EDITING ==============

// QueueEdit queues a voxel edit command for GPU processing
func (m *GpuBufferManager) QueueEdit(x, y, z int, val uint8) {
	cmd := EditCommand{
		Position: [3]int32{int32(x), int32(y), int32(z)},
		Value:    uint32(val),
	}
	m.PendingEdits = append(m.PendingEdits, cmd)

	// Auto-flush if we exceed max edits per frame
	if len(m.PendingEdits) >= m.MaxEditsPerFrame {
		m.FlushEdits(0) // Object ID 0 for now
	}
}

// FlushEdits processes all pending edit commands on the GPU
func (m *GpuBufferManager) FlushEdits(objectID uint32) {
	if len(m.PendingEdits) == 0 {
		return
	}

	// Serialize edit commands
	editData := make([]byte, len(m.PendingEdits)*16)
	for i, cmd := range m.PendingEdits {
		offset := i * 16
		binary.LittleEndian.PutUint32(editData[offset+0:], uint32(cmd.Position[0]))
		binary.LittleEndian.PutUint32(editData[offset+4:], uint32(cmd.Position[1]))
		binary.LittleEndian.PutUint32(editData[offset+8:], uint32(cmd.Position[2]))
		binary.LittleEndian.PutUint32(editData[offset+12:], cmd.Value)
	}

	// Ensure edit command buffer
	neededSize := uint64(len(editData))
	if neededSize%4 != 0 {
		neededSize += 4 - (neededSize % 4)
	}

	if m.EditCommandBuf == nil || m.EditCommandBuf.GetSize() < neededSize {
		if m.EditCommandBuf != nil {
			m.EditCommandBuf.Release()
		}
		desc := &wgpu.BufferDescriptor{
			Label: "EditCommandBuf",
			Size:  neededSize,
			Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
		}
		var err error
		m.EditCommandBuf, err = m.Device.CreateBuffer(desc)
		if err != nil {
			panic(err)
		}
	}
	m.Device.GetQueue().WriteBuffer(m.EditCommandBuf, 0, editData)

	// Update edit params
	paramsData := make([]byte, 16)
	binary.LittleEndian.PutUint32(paramsData[0:4], uint32(len(m.PendingEdits)))
	binary.LittleEndian.PutUint32(paramsData[4:8], objectID)

	if m.EditParamsBuf == nil {
		desc := &wgpu.BufferDescriptor{
			Label: "EditParamsBuf",
			Size:  16,
			Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		}
		var err error
		m.EditParamsBuf, err = m.Device.CreateBuffer(desc)
		if err != nil {
			panic(err)
		}
	}
	m.Device.GetQueue().WriteBuffer(m.EditParamsBuf, 0, paramsData)

	// Dispatch compute shader
	if m.EditPipeline != nil && m.EditBindGroup0 != nil && m.EditBindGroup1 != nil {
		encoder, err := m.Device.CreateCommandEncoder(nil)
		if err != nil {
			panic(err)
		}

		computePass := encoder.BeginComputePass(nil)
		computePass.SetPipeline(m.EditPipeline)
		computePass.SetBindGroup(0, m.EditBindGroup0, nil)
		computePass.SetBindGroup(1, m.EditBindGroup1, nil)
		if m.EditBindGroup2 != nil {
			computePass.SetBindGroup(2, m.EditBindGroup2, nil)
		}

		// Calculate workgroups (64 threads per workgroup)
		workgroups := (uint32(len(m.PendingEdits)) + 63) / 64
		computePass.DispatchWorkgroups(workgroups, 1, 1)
		computePass.End()

		cmdBuf, err := encoder.Finish(nil)
		if err != nil {
			panic(err)
		}

		m.Device.GetQueue().Submit(cmdBuf)
	}

	// Clear pending edits
	m.PendingEdits = m.PendingEdits[:0]
}

// CreateEditPipeline creates the compute pipeline for GPU voxel editing
func (m *GpuBufferManager) CreateEditPipeline(shaderCode string) error {
	// Create shader module
	shaderDesc := &wgpu.ShaderModuleDescriptor{
		Label: "VoxelEditShader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{
			Code: shaderCode,
		},
	}
	shaderModule, err := m.Device.CreateShaderModule(shaderDesc)
	if err != nil {
		return fmt.Errorf("failed to create edit shader module: %w", err)
	}
	defer shaderModule.Release()

	// Create compute pipeline
	pipelineDesc := &wgpu.ComputePipelineDescriptor{
		Label: "VoxelEditPipeline",
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     shaderModule,
			EntryPoint: "edit_voxels",
		},
	}
	m.EditPipeline, err = m.Device.CreateComputePipeline(pipelineDesc)
	if err != nil {
		return fmt.Errorf("failed to create edit pipeline: %w", err)
	}

	return nil
}

// CreateEditBindGroups creates bind groups for the edit pipeline
func (m *GpuBufferManager) CreateEditBindGroups() error {
	if m.EditPipeline == nil {
		return fmt.Errorf("edit pipeline not created")
	}

	// Ensure buffers exist
	if m.EditCommandBuf == nil {
		// Create dummy buffer
		desc := &wgpu.BufferDescriptor{
			Label: "EditCommandBuf",
			Size:  64,
			Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
		}
		var err error
		m.EditCommandBuf, err = m.Device.CreateBuffer(desc)
		if err != nil {
			return err
		}
	}

	if m.EditParamsBuf == nil {
		desc := &wgpu.BufferDescriptor{
			Label: "EditParamsBuf",
			Size:  16,
			Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		}
		var err error
		m.EditParamsBuf, err = m.Device.CreateBuffer(desc)
		if err != nil {
			return err
		}
	}

	// Ensure sector grid and voxel buffers
	if m.SectorGridBuf == nil {
		m.ensureBuffer("SectorGridBuf", &m.SectorGridBuf, make([]byte, 64), wgpu.BufferUsageStorage, 0)
	}
	if m.SectorGridParamsBuf == nil {
		m.ensureBuffer("SectorGridParamsBuf", &m.SectorGridParamsBuf, make([]byte, 16), wgpu.BufferUsageStorage, 0)
	}
	if m.SectorTableBuf == nil {
		m.ensureBuffer("SectorTableBuf", &m.SectorTableBuf, make([]byte, 64), wgpu.BufferUsageStorage, 0)
	}
	if m.BrickTableBuf == nil {
		m.ensureBuffer("BrickTableBuf", &m.BrickTableBuf, make([]byte, 64), wgpu.BufferUsageStorage, 0)
	}
	if m.VoxelPayloadBuf == nil {
		m.ensureBuffer("VoxelPayloadBuf", &m.VoxelPayloadBuf, make([]byte, 512), wgpu.BufferUsageStorage, 0)
	}
	if m.ObjectParamsBuf == nil {
		m.ensureBuffer("ObjectParamsBuf", &m.ObjectParamsBuf, make([]byte, 64), wgpu.BufferUsageStorage, 0)
	}

	// Group 0: Edit commands
	entries0 := []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.EditCommandBuf, Size: wgpu.WholeSize},
		{Binding: 1, Buffer: m.EditParamsBuf, Size: wgpu.WholeSize},
	}
	desc0 := &wgpu.BindGroupDescriptor{
		Layout:  m.EditPipeline.GetBindGroupLayout(0),
		Entries: entries0,
	}
	var err error
	m.EditBindGroup0, err = m.Device.CreateBindGroup(desc0)
	if err != nil {
		return fmt.Errorf("failed to create edit bind group 0: %w", err)
	}

	// Group 1: Voxel data (reuse existing buffers)
	entries1 := []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize},
		{Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
		{Binding: 2, Buffer: m.VoxelPayloadBuf, Size: wgpu.WholeSize},
		{Binding: 3, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize},
		{Binding: 4, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
		{Binding: 5, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize},
	}
	desc1 := &wgpu.BindGroupDescriptor{
		Layout:  m.EditPipeline.GetBindGroupLayout(1),
		Entries: entries1,
	}
	m.EditBindGroup1, err = m.Device.CreateBindGroup(desc1)
	if err != nil {
		return fmt.Errorf("failed to create edit bind group 1: %w", err)
	}

	// Group 2: Brick pool (if initialized)
	if m.BrickPoolParamsBuf != nil {
		entries2 := []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.BrickPoolParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.BrickPoolBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.BrickPoolPayloadBuf, Size: wgpu.WholeSize},
			{Binding: 3, Buffer: m.SectorExpansionBuf, Size: wgpu.WholeSize},
			{Binding: 4, Buffer: m.ExpansionCounterBuf, Size: wgpu.WholeSize},
		}
		desc2 := &wgpu.BindGroupDescriptor{
			Layout:  m.EditPipeline.GetBindGroupLayout(2),
			Entries: entries2,
		}
		m.EditBindGroup2, err = m.Device.CreateBindGroup(desc2)
		if err != nil {
			return fmt.Errorf("failed to create edit bind group 2: %w", err)
		}
	}

	return nil
}
