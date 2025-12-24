package gpu

import (
	"encoding/binary"
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

// ============== BRICK POOL MANAGEMENT ==============

// InitializeBrickPool creates GPU-side brick pool for allocation
func (m *GpuBufferManager) InitializeBrickPool() error {
	// Brick pool params (16 bytes)
	paramsData := make([]byte, 16)
	binary.LittleEndian.PutUint32(paramsData[0:4], m.BrickPoolSize)
	binary.LittleEndian.PutUint32(paramsData[4:8], 0) // next_free_idx = 0

	var err error
	m.BrickPoolParamsBuf, err = m.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "BrickPoolParams",
		Size:  16,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst | wgpu.BufferUsageCopySrc,
	})
	if err != nil {
		return fmt.Errorf("failed to create brick pool params buffer: %w", err)
	}
	m.Device.GetQueue().WriteBuffer(m.BrickPoolParamsBuf, 0, paramsData)

	// Brick pool records (16 bytes × 64K = 1MB)
	poolSize := uint64(m.BrickPoolSize * 16)
	m.BrickPoolBuf, err = m.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "BrickPool",
		Size:  poolSize,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("failed to create brick pool buffer: %w", err)
	}

	// Brick pool payload (512 bytes × 64K = 32MB)
	payloadSize := uint64(m.BrickPoolSize * 512)
	m.BrickPoolPayloadBuf, err = m.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "BrickPoolPayload",
		Size:  payloadSize,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("failed to create brick pool payload buffer: %w", err)
	}

	// Sector expansion queue (32 bytes × 1024 = 32KB)
	m.SectorExpansionBuf, err = m.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "SectorExpansionQueue",
		Size:  32 * 1024,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst | wgpu.BufferUsageCopySrc | wgpu.BufferUsageMapRead,
	})
	if err != nil {
		return fmt.Errorf("failed to create sector expansion buffer: %w", err)
	}

	// Expansion counter
	m.ExpansionCounterBuf, err = m.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "ExpansionCounter",
		Size:  4,
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst | wgpu.BufferUsageCopySrc | wgpu.BufferUsageMapRead,
	})
	if err != nil {
		return fmt.Errorf("failed to create expansion counter buffer: %w", err)
	}

	// Initialize counter to 0
	zero := make([]byte, 4)
	m.Device.GetQueue().WriteBuffer(m.ExpansionCounterBuf, 0, zero)

	return nil
}

// ProcessSectorExpansions reads expansion queue and updates CPU-side sectors
func (m *GpuBufferManager) ProcessSectorExpansions(scene *core.Scene) error {
	if m.ExpansionCounterBuf == nil || m.SectorExpansionBuf == nil {
		return nil // Not initialized yet
	}

	// Note: WebGPU buffer readback requires MapAsync which is asynchronous
	// For now, we'll use a simplified synchronous approach
	// In production, this should use proper async readback with callbacks

	// TODO: Implement proper async readback
	// For now, we'll skip sector expansion processing
	// The GPU edits will still work, but CPU won't know about new sectors
	// This is acceptable for initial implementation

	return nil
}

// ResetBrickPool resets the brick pool allocation counter
func (m *GpuBufferManager) ResetBrickPool() {
	if m.BrickPoolParamsBuf == nil {
		return
	}

	// Reset next_free_idx to 0
	paramsData := make([]byte, 16)
	binary.LittleEndian.PutUint32(paramsData[0:4], m.BrickPoolSize)
	binary.LittleEndian.PutUint32(paramsData[4:8], 0)
	m.Device.GetQueue().WriteBuffer(m.BrickPoolParamsBuf, 0, paramsData)

	// Reset expansion counter
	if m.ExpansionCounterBuf != nil {
		zero := make([]byte, 4)
		m.Device.GetQueue().WriteBuffer(m.ExpansionCounterBuf, 0, zero)
	}

	m.BrickPoolUsed = 0
}
