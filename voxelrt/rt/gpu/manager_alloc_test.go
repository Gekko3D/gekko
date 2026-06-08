package gpu

import (
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
)

func TestMarkRetiredBuffersSubmittedAssociatesUnsubmittedRetirements(t *testing.T) {
	manager := &GpuBufferManager{
		retiredBuffers: []retiredBuffer{
			{FramesLeft: RetiredBufferFrameDelay},
			{FramesLeft: RetiredBufferFrameDelay, Queue: &wgpu.Queue{}, SubmissionIndex: 3},
		},
		retiredBindGroups: []retiredBindGroup{
			{FramesLeft: RetiredBufferFrameDelay},
			{FramesLeft: RetiredBufferFrameDelay, Queue: &wgpu.Queue{}, SubmissionIndex: 5},
		},
	}
	queue := &wgpu.Queue{}

	manager.MarkRetiredBuffersSubmitted(queue, 42)

	if manager.retiredBuffers[0].Queue != queue || manager.retiredBuffers[0].SubmissionIndex != 42 {
		t.Fatalf("expected first retired buffer to receive submission fence, got %+v", manager.retiredBuffers[0])
	}
	if manager.retiredBuffers[1].Queue == queue || manager.retiredBuffers[1].SubmissionIndex != 3 {
		t.Fatalf("expected existing submission fence to be preserved, got %+v", manager.retiredBuffers[1])
	}
	if manager.retiredBindGroups[0].Queue != queue || manager.retiredBindGroups[0].SubmissionIndex != 42 {
		t.Fatalf("expected first retired bind group to receive submission fence, got %+v", manager.retiredBindGroups[0])
	}
	if manager.retiredBindGroups[1].Queue == queue || manager.retiredBindGroups[1].SubmissionIndex != 5 {
		t.Fatalf("expected existing bind group submission fence to be preserved, got %+v", manager.retiredBindGroups[1])
	}
}

func TestRetiredBindGroupPinsSourceBuffers(t *testing.T) {
	pinned := &wgpu.Buffer{}
	unpinned := &wgpu.Buffer{}
	manager := &GpuBufferManager{}

	manager.retireBindGroupWithBuffers(&wgpu.BindGroup{}, nil, pinned)

	if len(manager.retiredBindGroups) != 1 {
		t.Fatalf("retired bind groups = %d, want 1", len(manager.retiredBindGroups))
	}
	if len(manager.retiredBindGroups[0].Buffers) != 1 || manager.retiredBindGroups[0].Buffers[0] != pinned {
		t.Fatalf("retired bind group buffers = %+v, want only pinned buffer", manager.retiredBindGroups[0].Buffers)
	}
	if !manager.bufferPinnedByRetiredBindGroup(pinned) {
		t.Fatal("expected pinned source buffer to be retained by retired bind group")
	}
	if manager.bufferPinnedByRetiredBindGroup(unpinned) {
		t.Fatal("expected unrelated source buffer to be unpinned")
	}
}

func TestTransparentOverlaySceneBindGroupCurrentTracksSourceBuffers(t *testing.T) {
	camera := &wgpu.Buffer{}
	instances := &wgpu.Buffer{}
	bvh := &wgpu.Buffer{}
	lights := &wgpu.Buffer{}
	shadowParams := &wgpu.Buffer{}
	manager := &GpuBufferManager{
		TransparentBG0:                 &wgpu.BindGroup{},
		CameraBuf:                      camera,
		TransparentInstancesBuf:        instances,
		TransparentBVHNodesBuf:         bvh,
		LightsBuf:                      lights,
		ShadowLayerParamsBuf:           shadowParams,
		transparentBG0Camera:           camera,
		transparentBG0Instances:        instances,
		transparentBG0BVHNodes:         bvh,
		transparentBG0Lights:           lights,
		transparentBG0ShadowLayerParam: shadowParams,
	}

	if !manager.TransparentOverlaySceneBindGroupCurrent() {
		t.Fatal("expected matching transparent scene bind group sources to be current")
	}

	manager.TransparentInstancesBuf = &wgpu.Buffer{}
	if manager.TransparentOverlaySceneBindGroupCurrent() {
		t.Fatal("expected changed transparent instance buffer to stale the scene bind group")
	}
}

func TestGBufferSceneBindGroupCurrentTracksSourceBuffers(t *testing.T) {
	camera := &wgpu.Buffer{}
	instances := &wgpu.Buffer{}
	bvh := &wgpu.Buffer{}
	manager := &GpuBufferManager{
		GBufferBindGroup0:   &wgpu.BindGroup{},
		CameraBuf:           camera,
		InstancesBuf:        instances,
		BVHNodesBuf:         bvh,
		gBufferBG0Camera:    camera,
		gBufferBG0Instances: instances,
		gBufferBG0BVHNodes:  bvh,
	}

	if !manager.GBufferSceneBindGroupCurrent() {
		t.Fatal("expected matching g-buffer scene bind group sources to be current")
	}

	manager.InstancesBuf = &wgpu.Buffer{}
	if manager.GBufferSceneBindGroupCurrent() {
		t.Fatal("expected changed g-buffer instance buffer to stale the scene bind group")
	}
}
