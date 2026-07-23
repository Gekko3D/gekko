package gpu

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

func (m *GpuBufferManager) UpdateBeams(data []byte, count uint32) {
	if m == nil {
		return
	}
	m.BeamCount = count
	if count > 0 {
		m.ensureBuffer("BeamBuf", &m.BeamBuf, data, wgpu.BufferUsageStorage, 0)
	}
}

func (m *GpuBufferManager) SyncBeamBindGroups(pipeline *wgpu.RenderPipeline) {
	if m == nil || pipeline == nil || m.BeamCount == 0 || m.Device == nil ||
		m.CameraBuf == nil || m.BeamBuf == nil || m.DepthView == nil {
		return
	}
	if m.BeamsBindGroup0 == nil || m.beamsBGPipeline != pipeline ||
		m.beamsBGCamera != m.CameraBuf || m.beamsBGBuffer != m.BeamBuf {
		if m.BeamsBindGroup0 != nil {
			m.BeamsBindGroup0.Release()
		}
		var err error
		m.BeamsBindGroup0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Label:  "Beams BindGroup 0",
			Layout: pipeline.GetBindGroupLayout(0),
			Entries: []wgpu.BindGroupEntry{
				{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
				{Binding: 1, Buffer: m.BeamBuf, Size: wgpu.WholeSize},
			},
		})
		if err != nil {
			panic(fmt.Errorf("failed to create beams bind group 0: %v", err))
		}
		m.beamsBGCamera = m.CameraBuf
		m.beamsBGBuffer = m.BeamBuf
	}
	if m.BeamsBindGroup1 == nil || m.beamsBGPipeline != pipeline || m.beamsBGDepth != m.DepthView {
		if m.BeamsBindGroup1 != nil {
			m.BeamsBindGroup1.Release()
		}
		var err error
		m.BeamsBindGroup1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Label:   "Beams BindGroup 1",
			Layout:  pipeline.GetBindGroupLayout(1),
			Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: m.DepthView}},
		})
		if err != nil {
			panic(fmt.Errorf("failed to create beams bind group 1: %v", err))
		}
		m.beamsBGDepth = m.DepthView
	}
	m.beamsBGPipeline = pipeline
}

func (m *GpuBufferManager) RebuildBeamBindGroups(pipeline *wgpu.RenderPipeline) {
	if m == nil {
		return
	}
	m.beamsBGPipeline = nil
	m.SyncBeamBindGroups(pipeline)
}
