package gekko

import (
	"os"
	"reflect"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// This smoke checks real submission and bounded publication through the engine
// worker/commit/bridge. Production G-buffer pixel parity is owned by voxelbench.
func TestS1nNativeWorkerBridgePublishesBoundedGeometryAndLookup(t *testing.T) {
	s1nNativeWorkerBridgePublishesBoundedGeometryAndLookup(t, nil)
}

func TestS1oNativeStreamingConfigWorkerBridgePublishesUnderFiniteNativeWork(t *testing.T) {
	config := DefaultVoxelRtStreamingConfig()
	config.ManagedFrame = gpu.ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1}
	config.SectorLookup = gpu.SectorLookupFrameBudget{Enabled: true, MaxEntries: 64, MaxUploadBytes: 256, MaxStageBytes: 128 << 20}
	config.NativeWork = &gpu.VoxelGPUWorkBudget{Enabled: true, MaxCreateBytes: 1 << 20, MaxCreates: 1, MaxCopyBytes: 64 << 10}
	s1nNativeWorkerBridgePublishesBoundedGeometryAndLookup(t, &config)
}

func s1nNativeWorkerBridgePublishesBoundedGeometryAndLookup(t *testing.T, config *VoxelRtStreamingConfig) {
	t.Helper()
	expected := config
	var nativeCreates uint64
	if os.Getenv("GEKKO_NATIVE_S1N") != "1" {
		t.Skip("set GEKKO_NATIVE_S1N=1 for real-device streamed managed publication")
	}
	instance := wgpu.CreateInstance(nil)
	defer instance.Release()
	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{PowerPreference: wgpu.PowerPreferenceHighPerformance})
	if err != nil {
		t.Fatalf("native adapter: %v", err)
	}
	defer adapter.Release()
	t.Logf("native adapter: %v", adapter.GetInfo())
	device, err := adapter.RequestDevice(&wgpu.DeviceDescriptor{RequiredLimits: &wgpu.RequiredLimits{Limits: adapter.GetLimits().Limits}})
	if err != nil {
		t.Fatalf("native device: %v", err)
	}
	defer device.Release()
	queue := device.GetQueue()
	defer queue.Release()
	manager := gpu.NewGpuBufferManager(device, core.NewProfiler())
	// Release exported native resources exactly once, matching voxelbench cleanup.
	defer func() {
		seen := map[any]bool{}
		var release func(reflect.Value)
		release = func(value reflect.Value) {
			if !value.CanInterface() {
				return
			}
			if value.Kind() == reflect.Array {
				for i := 0; i < value.Len(); i++ {
					release(value.Index(i))
				}
				return
			}
			if value.Kind() != reflect.Pointer || value.IsNil() {
				return
			}
			resource := value.Interface()
			if seen[resource] {
				return
			}
			switch native := resource.(type) {
			case *wgpu.Buffer:
				native.Release()
			case *wgpu.Texture:
				native.Release()
			case *wgpu.TextureView:
				native.Release()
			case *wgpu.BindGroup:
				native.Release()
			case *wgpu.Sampler:
				native.Release()
			default:
				return
			}
			seen[resource] = true
		}
		value := reflect.ValueOf(manager).Elem()
		for i := 0; i < value.NumField(); i++ {
			release(value.Field(i))
		}
	}()
	if config == nil {
		manager.SetVoxelUploadBudget(gpu.DefaultVoxelUploadBudget())
		manager.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
		manager.SetManagedGeometryFrameBudget(gpu.ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
		manager.SetSectorLookupFrameBudget(gpu.SectorLookupFrameBudget{Enabled: true, MaxEntries: 64, MaxUploadBytes: 256, MaxStageBytes: 128 << 20})
	} else {
		config.Apply(manager)
		s1oAssertStreamingConfig(t, manager, *config)
	}
	f, _ := s1nRuntime(t, true, 1)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	eid := s1nBody(t, f, 0)
	state := s1nRenderer(f)
	state.RtApp.BufferManager = manager
	s1nBridge(f, state)
	scene, camera := state.RtApp.Scene, state.RtApp.Camera
	camera.Position, camera.LookAt, camera.Far = mgl32.Vec3{4, 3, 12}, mgl32.Vec3{1, 1, 1}, 160
	obj := state.GetVoxelObject(eid)
	input := s1l6Input(t, obj)
	if f.assets.PreparedVoxelRendererCopyStats().Adoptions != 1 {
		t.Fatal("native bridge rebuilt first derivative instead of consuming prepared storage")
	}
	manager.PrepareManagedGeometryFrame(scene)
	if obj.RenderVoxelMap() != nil {
		t.Fatal("unuploaded managed geometry is visible before first native frame")
	}
	frame := func() {
		t.Helper()
		s1nBridge(f, state)
		manager.PrepareManagedGeometryFrame(scene)
		if stats := manager.ManagedGeometryFrameStats(); stats.AttemptedEntries > 1 {
			t.Fatalf("managed entry cap exceeded: %+v", stats)
		}
		viewProjection := camera.ProjectionMatrix(1).Mul4(camera.GetViewMatrix())
		scene.Commit(camera.ExtractFrustum(viewProjection), core.SceneCommitOptions{OcclusionMode: core.OcclusionOff, CameraPosition: camera.Position})
		manager.UpdateScene(scene, camera, 1, mgl32.Vec3{})
		if expected != nil {
			s1oAssertStreamingConfig(t, manager, *expected)
			stats := manager.VoxelGPUWorkStats()
			nativeCreates += uint64(stats.Creates)
			if stats.Creates > expected.NativeWork.MaxCreates || stats.CopiedBytes > expected.NativeWork.MaxCopyBytes {
				t.Fatalf("native creation/copy cap exceeded: %+v", stats)
			}
			// Sole indivisible oversized creates remain legal and separately reported.
			if stats.CreatedBytes > expected.NativeWork.MaxCreateBytes && stats.OversizedCreates == 0 {
				t.Fatalf("unreported oversized native creation: %+v", stats)
			}
		}
		if stats := manager.SectorLookupFrameStats(); stats.AttemptedEntries > 64 || stats.UploadedBytes > 256 {
			t.Fatalf("lookup cap exceeded: %+v", stats)
		}
		encoder, err := device.CreateCommandEncoder(nil)
		if err != nil {
			t.Fatal(err)
		}
		commands, err := encoder.Finish(nil)
		encoder.Release()
		if err != nil {
			t.Fatal(err)
		}
		submission := queue.Submit(commands)
		commands.Release()
		manager.MarkRetiredBuffersSubmitted(queue, submission)
		device.Poll(true, nil)
		manager.AdvanceRetiredBuffers()
	}
	drain := func(generation uint64) int {
		t.Helper()
		for i := 0; i < 1024; i++ {
			frame()
			status, known := manager.ManagedGeometryGPUStatus(obj)
			geometry := obj.RenderVoxelMap()
			if !known || !status.CurrentInput.SameSource(input) || status.CurrentGeneration != generation || status.Pending || geometry == nil {
				continue
			}
			ready, _, _ := manager.RenderVoxelObjectReady(obj, geometry, geometry.Revision)
			if ready && !manager.SectorLookupFrameStats().Pending {
				return i + 1
			}
		}
		t.Fatalf("managed generation %d did not complete native content and committed lookup coverage", generation)
		return 0
	}
	if frames := drain(input.Generation()); frames < 2 {
		t.Fatal("native bounded publication never exercised multiple pending frames")
	}
	if config != nil {
		if nativeCreates == 0 {
			t.Fatal("configured native smoke never exercised finite creation service")
		}
		// Runtime setters remain authoritative after installation, including edits
		// and successor publication through ordinary bridge/frame updates.
		live := *config
		work := *config.NativeWork
		work.MaxCreates, work.MaxCopyBytes = 2, 128<<10
		live.NativeWork = &work
		expected = &live
		manager.SetVoxelGPUWorkBudget(work)
	}
	if err := ApplyManagedVoxelWrites(f.cmd, f.assets, eid, p1dWrites(volume.VoxelWrite{Value: 7}, volume.VoxelWrite{X: 40, Value: 3})); err != nil {
		t.Fatal(err)
	}
	edited := s1l6Input(t, obj)
	if !input.SameSource(edited) || edited.Generation() <= input.Generation() {
		t.Fatal("native engine edit replaced the qualified producer")
	}
	before, known := manager.ManagedGeometryGPUStatus(obj)
	if !known || before.CurrentGeneration != input.Generation() || obj.RenderVoxelMap() == nil {
		t.Fatal("topology edit discarded the certified previous display")
	}
	if found, _ := obj.RenderVoxelMap().GetVoxel(40, 0, 0); found {
		t.Fatal("new topology became visible before successor native coverage")
	}
	drain(edited.Generation())
	p1dVoxel(t, obj.RenderVoxelMap(), 0, 7)
	p1dVoxel(t, obj.RenderVoxelMap(), 40, 3)
	if err := unloadStreamedChunk(f.cmd, f.runtime, ChunkCoord{}); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	frame()
	if state.GetVoxelObject(eid) != nil {
		t.Fatal("unloaded entity retained its renderer object")
	}
	for i := 0; i < 1024; i++ {
		if _, known := manager.ManagedGeometryGPUStatus(obj); !known {
			break
		}
		frame()
	}
	if _, known := manager.ManagedGeometryGPUStatus(obj); known {
		t.Fatal("unloaded managed producer failed to retire under bounded service")
	}
	for _, resident := range scene.Objects {
		if resident == obj {
			t.Fatal("unloaded object remained scene-resident")
		}
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	frame()
	if state.GetVoxelObject(eid) != nil {
		t.Fatal("Stop reintroduced unloaded renderer object")
	}
}
