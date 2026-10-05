package gpu

import (
	"errors"
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

// Disabled preserves atomic unlimited loading. Enabled zero limits pause that
// kind of work. Creates are indivisible; copies are always four-byte aligned.
type VoxelGPUWorkBudget struct {
	Enabled        bool
	MaxCreateBytes uint64
	MaxCreates     uint32
	MaxCopyBytes   uint64
}

type VoxelGPUWorkStats struct {
	CreatedBytes, CopiedBytes uint64
	Creates, OversizedCreates uint32
	Pending                   bool
}

func (m *GpuBufferManager) SetVoxelGPUWorkBudget(budget VoxelGPUWorkBudget) {
	if m != nil {
		m.voxelGPUWorkBudget = budget
	}
}
func (m *GpuBufferManager) VoxelGPUWorkBudget() VoxelGPUWorkBudget {
	if m == nil {
		return VoxelGPUWorkBudget{}
	}
	return m.voxelGPUWorkBudget
}
func (m *GpuBufferManager) VoxelGPUWorkStats() VoxelGPUWorkStats {
	if m == nil {
		return VoxelGPUWorkStats{}
	}
	return m.voxelGPUWorkStats
}

var errVoxelGPUWorkPending = errors.New("voxel resource migration pending")
var errVoxelGPUStagePublished = errors.New("voxel resource generation published")

// A stage pins exactly one admitted physical capacity generation, independent
// of live logical demand. No scene, object, map or replay payload is retained.
type voxelGPUStage struct {
	backend voxelNativeBackend
	sources [7]*wgpu.Buffer
	buffers [7]*wgpu.Buffer
	sizes   [7]uint64
	old     [7]uint64
	copied  [7]uint64
	used    [7]bool
}

func (m *GpuBufferManager) stagingVoxelGPUBytes() uint64 {
	var total uint64
	if stage := m.voxelGrowth; stage != nil {
		for i, buffer := range stage.buffers {
			if buffer != nil {
				total = addRetainedVoxelBytes(total, stage.sizes[i])
			}
		}
	}
	return total
}

func (m *GpuBufferManager) startVoxelGPUStage(next voxelGPUResources) error {
	if m.voxelGrowth == nil {
		stage := &voxelGPUStage{backend: m.voxelNative, sizes: voxelResourceSizes(next)}
		for i, destination := range m.voxelBufferDestinations() {
			stage.sources[i] = *destination
			stage.old[i] = stage.backend.BufferSize(*destination)
		}
		m.voxelGrowth = stage
	}
	if m.voxelWorkAdvanced {
		return errVoxelGPUWorkPending
	}
	_, err := m.advanceVoxelGPUStage()
	return err
}

func (m *GpuBufferManager) canCreateVoxelBuffer(size uint64) (bool, bool) {
	budget, stats := m.voxelGPUWorkBudget, m.voxelGPUWorkStats
	if !budget.Enabled {
		return true, false
	}
	if budget.MaxCreates == 0 || budget.MaxCreateBytes == 0 || stats.Creates >= budget.MaxCreates || stats.OversizedCreates != 0 {
		return false, false
	}
	if size <= budget.MaxCreateBytes-min(budget.MaxCreateBytes, stats.CreatedBytes) {
		return true, false
	}
	// An oversized create may be the update's sole creation, never a follow-on
	// to ordinary work, and never an exception to an enabled zero limit.
	return stats.Creates == 0 && size > budget.MaxCreateBytes, stats.Creates == 0 && size > budget.MaxCreateBytes
}

// Copies are submitted before this update's content/lookup writes. Every later
// write to a copied or not-yet-copied replacement is mirrored, so future chunks
// cannot overwrite an acknowledged newer edit with stale source bytes.
func (m *GpuBufferManager) advanceVoxelGPUStage() (bool, error) {
	stage := m.voxelGrowth
	if stage == nil {
		return false, nil
	}
	m.voxelWorkAdvanced = true
	for i, size := range stage.sizes {
		if size <= stage.old[i] || stage.buffers[i] != nil {
			continue
		}
		allowed, oversized := m.canCreateVoxelBuffer(size)
		if !allowed {
			break
		}
		buffer, err := stage.backend.CreateBuffer(voxelBufferLabels[i], size, i == 6)
		if err != nil {
			m.abortVoxelGPUStage()
			return false, fmt.Errorf("voxel %s allocation: %w", voxelBufferLabels[i], err)
		}
		stage.buffers[i] = buffer
		m.voxelGPUWorkStats.Creates++
		m.voxelGPUWorkStats.CreatedBytes = addRetainedVoxelBytes(m.voxelGPUWorkStats.CreatedBytes, size)
		if oversized {
			m.voxelGPUWorkStats.OversizedCreates++
		}
	}
	remaining := ^uint64(0) &^ uint64(3)
	if budget := m.voxelGPUWorkBudget; budget.Enabled {
		remaining = (budget.MaxCopyBytes - min(budget.MaxCopyBytes, m.voxelGPUWorkStats.CopiedBytes)) &^ uint64(3)
	}
	var copies []voxelBufferCopy
	var copied uint64
	for i, buffer := range stage.buffers {
		if buffer == nil || stage.sources[i] == nil || stage.copied[i] == stage.old[i] || remaining == 0 {
			continue
		}
		size := min(stage.old[i]-stage.copied[i], remaining)
		copies = append(copies, voxelBufferCopy{stage.sources[i], buffer, stage.copied[i], stage.copied[i], size})
		stage.used[i] = true
		remaining -= size
		copied = addRetainedVoxelBytes(copied, size)
	}
	if len(copies) != 0 {
		if err := stage.backend.CopyBuffers(copies); err != nil {
			m.abortVoxelGPUStage()
			return false, err
		}
		for i, buffer := range stage.buffers {
			for _, copy := range copies {
				if copy.dst == buffer {
					stage.copied[i] += copy.size
				}
			}
		}
		m.voxelGPUWorkStats.CopiedBytes = addRetainedVoxelBytes(m.voxelGPUWorkStats.CopiedBytes, copied)
	}
	for i, size := range stage.sizes {
		if size > stage.old[i] && (stage.buffers[i] == nil || stage.copied[i] < stage.old[i]) {
			return false, errVoxelGPUWorkPending
		}
	}
	// Publish all pointers together, then callers rerun admission from current
	// live requests. The captured capacity plan never assigns logical ownership.
	for i, destination := range m.voxelBufferDestinations() {
		if buffer := stage.buffers[i]; buffer != nil {
			m.retireVoxelBuffer(stage.sources[i], stage.old[i], stage.backend)
			*destination = buffer
			if i == 3 {
				m.advanceMaterialBufferGeneration(stage.sources[i] != nil && stage.copied[i] == stage.old[i])
			}
		}
	}
	m.voxelGrowth = nil
	return true, nil
}

func (m *GpuBufferManager) retireVoxelBuffer(buffer *wgpu.Buffer, size uint64, backend voxelNativeBackend) {
	if buffer != nil {
		m.retiredBuffers = append(m.retiredBuffers, retiredBuffer{Buffer: buffer, VoxelBytes: size, bufferRelease: backend.ReleaseBuffer, FramesLeft: RetiredBufferFrameDelay})
	}
}

func (m *GpuBufferManager) abortVoxelGPUStage() {
	stage := m.voxelGrowth
	if stage == nil {
		return
	}
	for i, buffer := range stage.buffers {
		if buffer == nil {
			continue
		}
		if stage.used[i] {
			m.retireVoxelBuffer(buffer, stage.sizes[i], stage.backend)
		} else {
			stage.backend.ReleaseBuffer(buffer)
		}
	}
	m.voxelGrowth = nil
}
