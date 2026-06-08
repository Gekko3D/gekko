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

func TestAlignGpuBufferAllocationSizeAlignsGeometricGrowthResult(t *testing.T) {
	if got := alignGpuBufferAllocationSize(39366); got != 39424 {
		t.Fatalf("alignGpuBufferAllocationSize(39366) = %d, want 39424", got)
	}
	if got := alignGpuBufferAllocationSize(0); got != 256 {
		t.Fatalf("alignGpuBufferAllocationSize(0) = %d, want 256", got)
	}
	if got := alignGpuBufferAllocationSize(26244); got != 26368 {
		t.Fatalf("alignGpuBufferAllocationSize(26244) = %d, want 26368", got)
	}
}

func TestLiveSceneBindGroupsPinSourceBuffers(t *testing.T) {
	camera := &wgpu.Buffer{}
	instances := &wgpu.Buffer{}
	bvh := &wgpu.Buffer{}
	unpinned := &wgpu.Buffer{}
	manager := &GpuBufferManager{
		gBufferBG0Camera:    camera,
		gBufferBG0Instances: instances,
		gBufferBG0BVHNodes:  bvh,
	}

	for _, buffer := range []*wgpu.Buffer{camera, instances, bvh} {
		if !manager.bufferReferencedByLiveBindGroup(buffer) {
			t.Fatalf("expected live g-buffer source %p to be pinned", buffer)
		}
	}
	if manager.bufferReferencedByLiveBindGroup(unpinned) {
		t.Fatal("expected unrelated buffer to be unpinned")
	}
}

func TestAdvanceRetiredBuffersKeepsLiveSceneSourceBuffer(t *testing.T) {
	camera := &wgpu.Buffer{}
	manager := &GpuBufferManager{
		gBufferBG0Camera: camera,
		retiredBuffers: []retiredBuffer{
			{Buffer: camera, FramesLeft: 0},
		},
	}

	manager.advanceRetiredBuffers()

	if len(manager.retiredBuffers) != 1 || manager.retiredBuffers[0].Buffer != camera {
		t.Fatalf("expected live scene source buffer to remain retired but unreleased, got %+v", manager.retiredBuffers)
	}
}

func TestAdvanceRetiredBindGroupsKeepsLiveSceneBindGroup(t *testing.T) {
	bg := &wgpu.BindGroup{}
	manager := &GpuBufferManager{
		GBufferBindGroup0: bg,
		retiredBindGroups: []retiredBindGroup{
			{BindGroup: bg, FramesLeft: 0},
		},
	}

	manager.advanceRetiredBindGroups()

	if len(manager.retiredBindGroups) != 1 || manager.retiredBindGroups[0].BindGroup != bg {
		t.Fatalf("expected live scene bind group to remain retired but unreleased, got %+v", manager.retiredBindGroups)
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
