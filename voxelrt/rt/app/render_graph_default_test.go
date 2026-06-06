package app

import (
	"strings"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

func TestDefaultRenderGraphOrderMatchesRuntimeFrameSequence(t *testing.T) {
	graph := NewDefaultRenderGraph()
	ordered, err := graph.Compile()
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	got := renderNodeNames(ordered)
	want := []string{
		RenderNodeFeatureParticlesSim,
		RenderNodeFeaturePreGBuffer,
		RenderNodeFeatureCAVolumesSim,
		RenderNodeFeaturePreGBufferVolumes,
		RenderNodeCoreGBuffer,
		RenderNodeCoreHiZ,
		RenderNodeFeaturePostGBuffer,
		RenderNodeCoreShadows,
		RenderNodeFeaturePreLighting,
		RenderNodeFeatureSkyboxUpdate,
		RenderNodeCoreTiledLightCull,
		RenderNodeCoreLighting,
		RenderNodeFeaturePostLighting,
		RenderNodeFeatureCAVolumesRender,
		RenderNodeFeatureAstronomical,
		RenderNodeFeaturePlanetBodies,
		RenderNodeFeatureAnalyticMedia,
		RenderNodeCoreDebugScene,
		RenderNodeCoreAccumulation,
		RenderNodeFeaturePreResolve,
		RenderNodeCoreResolve,
		RenderNodeFeatureTextOverlay,
		RenderNodeFeatureGizmosOverlay,
		RenderNodeFeaturePostResolve,
	}
	if !sameStrings(got, want) {
		t.Fatalf("default graph order = %v, want %v", got, want)
	}
}

func TestRuntimeRenderGraphNodeSequenceMatchesDefaultGraphOrder(t *testing.T) {
	graph := NewDefaultRenderGraph()
	ordered, err := graph.Compile()
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	got := runtimeRenderGraphNodeSequence()
	want := renderNodeNames(ordered)
	if !sameStrings(got, want) {
		t.Fatalf("runtime graph node sequence = %v, want %v", got, want)
	}
}

func TestAppRecordRenderGraphUsesCompiledGraphOrder(t *testing.T) {
	var calls []string
	graph := NewRenderGraph()
	graph.Register(RenderNodeSpec{Name: "gbuffer", Node: &testRenderNode{name: "gbuffer", enabled: true, calls: &calls}})
	graph.Register(RenderNodeSpec{Name: "lighting", After: []string{"gbuffer"}, Node: &testRenderNode{name: "lighting", enabled: true, calls: &calls}})
	graph.Register(RenderNodeSpec{Name: "resolve", After: []string{"lighting"}, Node: &testRenderNode{name: "resolve", enabled: true, calls: &calls}})

	app := &App{RenderGraph: graph}
	app.recordRenderGraph(nil, nil)

	want := []string{"gbuffer", "lighting", "resolve"}
	if !sameStrings(calls, want) {
		t.Fatalf("recordRenderGraph calls = %v, want %v", calls, want)
	}
}

func TestAppRecordRenderFrameMetricsHandlesMissingSceneResources(t *testing.T) {
	app := &App{Profiler: core.NewProfiler()}

	app.recordRenderFrameMetrics()

	if got := app.Profiler.Counts["LocalLights"]; got != 0 {
		t.Fatalf("LocalLights = %d, want 0", got)
	}
	if got := app.Profiler.Counts["SceneLights"]; got != 0 {
		t.Fatalf("SceneLights = %d, want 0", got)
	}
	if got := app.Profiler.Counts["AccumulationActive"]; got != 0 {
		t.Fatalf("AccumulationActive = %d, want 0", got)
	}
}

func TestNewAppInitializesDefaultRenderGraph(t *testing.T) {
	app := NewApp(nil)
	if app.RenderGraph == nil {
		t.Fatal("expected NewApp to initialize RenderGraph")
	}
	ordered, err := app.RenderGraph.Compile()
	if err != nil {
		t.Fatalf("default RenderGraph did not compile: %v", err)
	}
	if len(ordered) != len(defaultRenderGraphSpecs()) {
		t.Fatalf("default RenderGraph node count = %d, want %d", len(ordered), len(defaultRenderGraphSpecs()))
	}
}

func TestDefaultRenderGraphFeatureStageNodesWrapFeatureRegistry(t *testing.T) {
	var calls []string
	feature := &renderGraphStageRecordingFeature{
		name:    "stage-recorder",
		enabled: true,
		commandStages: map[FeatureCommandStage]string{
			FeatureCommandStagePreGBuffer:   "pre-gbuffer",
			FeatureCommandStagePostLighting: "post-lighting",
			FeatureCommandStagePreResolve:   "pre-resolve",
		},
		screenStages: map[FeatureScreenStage]string{
			FeatureScreenStagePostResolve: "post-resolve",
		},
		calls: &calls,
	}
	app := &App{
		RenderGraph: NewDefaultRenderGraph(),
		features:    []Feature{feature},
	}
	frame := &FrameContext{SwapchainView: &wgpu.TextureView{}}

	for _, nodeName := range []string{
		RenderNodeFeaturePreGBuffer,
		RenderNodeFeaturePostLighting,
		RenderNodeFeaturePreResolve,
		RenderNodeFeaturePostResolve,
	} {
		if err := app.RenderGraph.RecordNode(nodeName, app, nil, frame); err != nil {
			t.Fatalf("RecordNode(%q) returned error: %v", nodeName, err)
		}
	}

	want := []string{
		"stage-recorder:pre-gbuffer",
		"stage-recorder:post-lighting",
		"stage-recorder:pre-resolve",
		"stage-recorder:post-resolve",
	}
	if !sameStrings(calls, want) {
		t.Fatalf("feature stage calls = %v, want %v", calls, want)
	}
}

func TestDefaultRenderGraphParticlesSimulationNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeFeatureParticlesSim)
	if node.Enabled(nil) {
		t.Fatal("expected particles simulation node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected particles simulation node to be disabled without particles feature and resources")
	}

	app.features = []Feature{&ParticlesFeature{}}
	if node.Enabled(app) {
		t.Fatal("expected particles simulation node to be disabled without particle resources")
	}

	app.BufferManager = &gpu.GpuBufferManager{ParticleSystemActive: true}
	app.ParticleResources = testReadyParticleResources()
	if !node.Enabled(app) {
		t.Fatal("expected particles simulation node to be enabled with particles feature and ready resources")
	}
}

func TestDefaultRenderGraphSkyboxUpdateNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeFeatureSkyboxUpdate)
	if node.Enabled(nil) {
		t.Fatal("expected skybox update node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected skybox update node to be disabled without skybox feature")
	}

	app.features = []Feature{&SkyboxFeature{}}
	if !node.Enabled(app) {
		t.Fatal("expected skybox update node to be enabled with skybox feature ownership")
	}
}

func TestRecordParticlesSimulationPassRequiresEncoder(t *testing.T) {
	app := &App{
		Profiler: core.NewProfiler(),
		features: []Feature{&ParticlesFeature{}},
		BufferManager: &gpu.GpuBufferManager{
			ParticleSystemActive: true,
		},
		ParticleResources: testReadyParticleResourcesWithSpawnCount(3),
	}

	if app.hasCommandStageWork(FeatureCommandStagePreGBuffer) {
		t.Fatal("expected legacy pre-gbuffer command stage to skip graph-owned particle simulation")
	}

	err := app.recordParticlesSimulationPass(nil)
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["ParticlesSimGraphNode"]; got != 1 {
		t.Fatalf("ParticlesSimGraphNode = %d, want 1", got)
	}
	if got := app.Profiler.Counts["ParticlesSpawnCount"]; got != 3 {
		t.Fatalf("ParticlesSpawnCount = %d, want 3", got)
	}
}

func testReadyParticleResources() *ParticleResources {
	return &ParticleResources{
		InitPipeline:     &wgpu.ComputePipeline{},
		SimPipeline:      &wgpu.ComputePipeline{},
		SpawnPipeline:    &wgpu.ComputePipeline{},
		FinalizePipeline: &wgpu.ComputePipeline{},
	}
}

func testReadyParticleResourcesWithSpawnCount(spawnCount uint32) *ParticleResources {
	resources := testReadyParticleResources()
	resources.SpawnCount = spawnCount
	return resources
}

func TestDefaultRenderGraphAnalyticMediumNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeFeatureAnalyticMedia)
	if node.Enabled(nil) {
		t.Fatal("expected analytic media node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected analytic media node to be disabled without feature and resources")
	}

	app.features = []Feature{&AnalyticMediumFeature{}}
	if node.Enabled(app) {
		t.Fatal("expected analytic media node to be disabled without analytic media resources")
	}

	app.AnalyticMediumResources = testReadyAnalyticMediumResources()
	app.BufferManager = &gpu.GpuBufferManager{}
	app.BufferManager.VolumetricView[app.BufferManager.VolumetricRenderIdx] = &wgpu.TextureView{}
	app.BufferManager.VolumetricDepthView[app.BufferManager.VolumetricRenderIdx] = &wgpu.TextureView{}
	if !node.Enabled(app) {
		t.Fatal("expected analytic media node to be enabled with feature and ready targets")
	}
}

func TestDefaultRenderGraphPlanetBodiesNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeFeaturePlanetBodies)
	if node.Enabled(nil) {
		t.Fatal("expected planet bodies node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected planet bodies node to be disabled without feature and resources")
	}

	app.features = []Feature{&PlanetBodyFeature{}}
	if node.Enabled(app) {
		t.Fatal("expected planet bodies node to be disabled without planet body resources")
	}

	app.PlanetBodyResources = testReadyPlanetBodyResources()
	app.StorageView = &wgpu.TextureView{}
	app.BufferManager = &gpu.GpuBufferManager{PlanetDepthView: &wgpu.TextureView{}}
	if !node.Enabled(app) {
		t.Fatal("expected planet bodies node to be enabled with feature and ready targets")
	}
}

func TestDefaultRenderGraphAstronomicalNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeFeatureAstronomical)
	if node.Enabled(nil) {
		t.Fatal("expected astronomical node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected astronomical node to be disabled without feature and resources")
	}

	app.features = []Feature{&AstronomicalFeature{}}
	if node.Enabled(app) {
		t.Fatal("expected astronomical node to be disabled without astronomical resources")
	}

	app.AstronomicalResources = testReadyAstronomicalResources()
	app.StorageView = &wgpu.TextureView{}
	app.BufferManager = &gpu.GpuBufferManager{DepthView: &wgpu.TextureView{}}
	if !node.Enabled(app) {
		t.Fatal("expected astronomical node to be enabled with feature and ready targets")
	}
}

func TestDefaultRenderGraphCAVolumeRenderNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeFeatureCAVolumesRender)
	if node.Enabled(nil) {
		t.Fatal("expected CA volume render node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected CA volume render node to be disabled without feature and resources")
	}

	app.features = []Feature{&CAVolumeFeature{}}
	if node.Enabled(app) {
		t.Fatal("expected CA volume render node to be disabled without CA volume render targets")
	}

	app.BufferManager = &gpu.GpuBufferManager{
		CAVolumeColorView:    &wgpu.TextureView{},
		CAVolumeDepthView:    &wgpu.TextureView{},
		CAVolumeVisibleCount: 1,
	}
	if !node.Enabled(app) {
		t.Fatal("expected CA volume render node to be enabled with feature and visible CA volume")
	}

	app.BufferManager.CAVolumeVisibleCount = 0
	app.SetHadCAVolumePass(true)
	if !node.Enabled(app) {
		t.Fatal("expected CA volume render node to be enabled to clear stale prior pass")
	}
}

func TestRecordCAVolumeRenderPassRequiresEncoder(t *testing.T) {
	app := &App{
		Profiler: core.NewProfiler(),
		features: []Feature{&CAVolumeFeature{}},
		BufferManager: &gpu.GpuBufferManager{
			CAVolumeColorView:    &wgpu.TextureView{},
			CAVolumeDepthView:    &wgpu.TextureView{},
			CAVolumeVisibleCount: 1,
		},
	}

	if app.hasCommandStageWork(FeatureCommandStagePostLighting) {
		t.Fatal("expected legacy post-lighting command stage to skip graph-owned CA volume render")
	}

	err := app.recordCAVolumeRenderPass(nil)
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["CAVolumeRenderGraphNode"]; got != 1 {
		t.Fatalf("CAVolumeRenderGraphNode = %d, want 1", got)
	}
}

func TestRecordAstronomicalPassRequiresEncoder(t *testing.T) {
	app := &App{
		Profiler:              core.NewProfiler(),
		features:              []Feature{&AstronomicalFeature{}},
		AstronomicalResources: testReadyAstronomicalResources(),
		StorageView:           &wgpu.TextureView{},
		BufferManager:         &gpu.GpuBufferManager{DepthView: &wgpu.TextureView{}},
	}

	if app.hasCommandStageWork(FeatureCommandStagePostLighting) {
		t.Fatal("expected legacy post-lighting command stage to skip graph-owned astronomical feature")
	}

	err := app.recordAstronomicalPass(nil)
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["AstronomicalGraphNode"]; got != 1 {
		t.Fatalf("AstronomicalGraphNode = %d, want 1", got)
	}
}

func testReadyAstronomicalResources() *AstronomicalResources {
	return &AstronomicalResources{Pipeline: &wgpu.RenderPipeline{}}
}

func TestRecordPlanetBodiesPassRequiresEncoder(t *testing.T) {
	app := &App{
		Profiler:            core.NewProfiler(),
		features:            []Feature{&PlanetBodyFeature{}},
		PlanetBodyResources: testReadyPlanetBodyResources(),
		StorageView:         &wgpu.TextureView{},
		BufferManager:       &gpu.GpuBufferManager{PlanetDepthView: &wgpu.TextureView{}},
	}

	if app.hasCommandStageWork(FeatureCommandStagePostLighting) {
		t.Fatal("expected legacy post-lighting command stage to skip graph-owned planet bodies")
	}

	err := app.recordPlanetBodiesPass(nil)
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["PlanetBodiesGraphNode"]; got != 1 {
		t.Fatalf("PlanetBodiesGraphNode = %d, want 1", got)
	}
}

func testReadyPlanetBodyResources() *PlanetBodyResources {
	return &PlanetBodyResources{Pipeline: &wgpu.RenderPipeline{}}
}

func TestRecordAnalyticMediumPassRequiresEncoder(t *testing.T) {
	app := &App{
		Profiler:                core.NewProfiler(),
		features:                []Feature{&AnalyticMediumFeature{}},
		AnalyticMediumResources: testReadyAnalyticMediumResources(),
		BufferManager:           &gpu.GpuBufferManager{},
	}
	app.BufferManager.VolumetricView[app.BufferManager.VolumetricRenderIdx] = &wgpu.TextureView{}
	app.BufferManager.VolumetricDepthView[app.BufferManager.VolumetricRenderIdx] = &wgpu.TextureView{}

	if app.hasCommandStageWork(FeatureCommandStagePostLighting) {
		t.Fatal("expected legacy post-lighting command stage to skip graph-owned analytic media")
	}

	err := app.recordAnalyticMediumPass(nil)
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["AnalyticMediaGraphNode"]; got != 1 {
		t.Fatalf("AnalyticMediaGraphNode = %d, want 1", got)
	}
}

func testReadyAnalyticMediumResources() *AnalyticMediumResources {
	return &AnalyticMediumResources{Pipeline: &wgpu.RenderPipeline{}}
}

func TestDefaultRenderGraphCAVolumeSimulationNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeFeatureCAVolumesSim)
	if node.Enabled(nil) {
		t.Fatal("expected CA volume simulation node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected CA volume simulation node to be disabled without feature and resources")
	}

	app.features = []Feature{&CAVolumeFeature{}}
	if node.Enabled(app) {
		t.Fatal("expected CA volume simulation node to be disabled without CA volume resources")
	}

	app.CAVolumeResources = testReadyCAVolumeResources()
	app.BufferManager = &gpu.GpuBufferManager{CAVolumeCount: 1}
	if !node.Enabled(app) {
		t.Fatal("expected CA volume simulation node to be enabled with feature and ready resources")
	}
}

func TestRecordCAVolumeSimulationPassRequiresEncoder(t *testing.T) {
	app := &App{
		Profiler:          core.NewProfiler(),
		features:          []Feature{&CAVolumeFeature{}},
		CAVolumeResources: testReadyCAVolumeResources(),
		BufferManager:     &gpu.GpuBufferManager{CAVolumeCount: 1},
	}

	if app.hasCommandStageWork(FeatureCommandStagePreGBufferVolumes) {
		t.Fatal("expected legacy pre-gbuffer-volumes command stage to skip graph-owned CA simulation")
	}

	err := app.recordCAVolumeSimulationPass(nil)
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["CAVolumeSimGraphNode"]; got != 1 {
		t.Fatalf("CAVolumeSimGraphNode = %d, want 1", got)
	}
}

func testReadyCAVolumeResources() *CAVolumeResources {
	return &CAVolumeResources{
		SimPipeline:    &wgpu.ComputePipeline{},
		BoundsPipeline: &wgpu.ComputePipeline{},
	}
}

func TestDefaultRenderGraphPostResolveNodeRequiresSwapchainView(t *testing.T) {
	var calls []string
	feature := &renderGraphStageRecordingFeature{
		name:    "screen-recorder",
		enabled: true,
		screenStages: map[FeatureScreenStage]string{
			FeatureScreenStagePostResolve: "post-resolve",
		},
		calls: &calls,
	}
	app := &App{
		RenderGraph: NewDefaultRenderGraph(),
		features:    []Feature{feature},
	}

	if err := app.RenderGraph.RecordNode(RenderNodeFeaturePostResolve, app, nil, &FrameContext{}); err != nil {
		t.Fatalf("RecordNode returned error: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("expected no post-resolve calls without swapchain view, got %v", calls)
	}
}

func TestDefaultRenderGraphTextOverlayNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeFeatureTextOverlay)
	if node.Enabled(nil) {
		t.Fatal("expected text overlay node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected text overlay node to be disabled without text feature and resources")
	}

	app.features = []Feature{&TextFeature{}}
	if node.Enabled(app) {
		t.Fatal("expected text overlay node to be disabled without text resources")
	}

	app.TextResources = testReadyTextResources()
	if !node.Enabled(app) {
		t.Fatal("expected text overlay node to be enabled with text feature and ready resources")
	}
}

func TestRecordTextOverlayPassRequiresEncoder(t *testing.T) {
	app := &App{
		Profiler:      core.NewProfiler(),
		features:      []Feature{&TextFeature{}},
		TextResources: testReadyTextResources(),
	}

	err := app.recordTextOverlayPass(nil, &FrameContext{SwapchainView: &wgpu.TextureView{}})
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["TextGraphNode"]; got != 1 {
		t.Fatalf("TextGraphNode = %d, want 1", got)
	}
	if got := app.Profiler.Counts["TextVertices"]; got != 6 {
		t.Fatalf("TextVertices = %d, want 6", got)
	}
}

func testReadyTextResources() *TextResources {
	return &TextResources{
		Items:        []core.TextItem{{Text: "hello"}},
		VertexCount:  6,
		VertexBuffer: &wgpu.Buffer{},
		Pipeline:     &wgpu.RenderPipeline{},
		BindGroup:    &wgpu.BindGroup{},
	}
}

func TestDefaultRenderGraphGizmosOverlayNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeFeatureGizmosOverlay)
	if node.Enabled(nil) {
		t.Fatal("expected gizmos overlay node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected gizmos overlay node to be disabled without gizmo feature and resources")
	}

	app.features = []Feature{&GizmoFeature{}}
	if node.Enabled(app) {
		t.Fatal("expected gizmos overlay node to be disabled without gizmo resources")
	}

	app.Scene = &core.Scene{Gizmos: []core.Gizmo{{Type: core.GizmoLine}}}
	app.GizmoResources = &GizmoResources{
		Pass: &gpu.GizmoRenderPass{
			BindGroup:      &wgpu.BindGroup{},
			DepthBindGroup: &wgpu.BindGroup{},
		},
	}
	if !node.Enabled(app) {
		t.Fatal("expected gizmos overlay node to be enabled with gizmo feature and ready resources")
	}
}

func TestRecordGizmosOverlayPassRequiresEncoder(t *testing.T) {
	app := &App{
		Profiler: core.NewProfiler(),
		features: []Feature{&GizmoFeature{}},
		Scene: &core.Scene{
			Gizmos: []core.Gizmo{{Type: core.GizmoLine}},
		},
		GizmoResources: &GizmoResources{
			Pass: &gpu.GizmoRenderPass{
				BindGroup:      &wgpu.BindGroup{},
				DepthBindGroup: &wgpu.BindGroup{},
			},
		},
	}

	err := app.recordGizmosOverlayPass(nil, &FrameContext{SwapchainView: &wgpu.TextureView{}})
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["GizmosGraphNode"]; got != 1 {
		t.Fatalf("GizmosGraphNode = %d, want 1", got)
	}
	if got := app.Profiler.Counts["Gizmos"]; got != 1 {
		t.Fatalf("Gizmos = %d, want 1", got)
	}
}

func TestDefaultRenderGraphDebugSceneNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeCoreDebugScene)
	if node.Enabled(nil) {
		t.Fatal("expected debug-scene node to be disabled without app")
	}

	app := &App{Camera: core.NewCameraState()}
	if node.Enabled(app) {
		t.Fatal("expected debug-scene node to be disabled when debug mode is off")
	}

	app.DebugMode = true
	if node.Enabled(app) {
		t.Fatal("expected debug-scene node to be disabled until camera debug mode requests scene debug")
	}

	app.Camera.DebugMode = uint32(core.DebugModeScene)
	if !node.Enabled(app) {
		t.Fatal("expected debug-scene node to be enabled for scene debug mode")
	}
}

func TestDefaultRenderGraphTiledLightCullNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeCoreTiledLightCull)
	if node.Enabled(nil) {
		t.Fatal("expected tiled-light-cull node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected tiled-light-cull node to be disabled without buffer manager")
	}

	app.BufferManager = &gpu.GpuBufferManager{}
	if !node.Enabled(app) {
		t.Fatal("expected tiled-light-cull node to be enabled with buffer manager")
	}
}

func TestDefaultRenderGraphGBufferNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeCoreGBuffer)
	if node.Enabled(nil) {
		t.Fatal("expected g-buffer node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected g-buffer node to be disabled without buffer manager")
	}

	app.BufferManager = &gpu.GpuBufferManager{}
	if !node.Enabled(app) {
		t.Fatal("expected g-buffer node to be enabled with buffer manager")
	}
}

func TestRecordGBufferPassRequiresEncoder(t *testing.T) {
	app := &App{
		Profiler:      core.NewProfiler(),
		BufferManager: &gpu.GpuBufferManager{},
	}

	err := app.recordGBufferPass(nil, &FrameContext{Width: 8, Height: 8})
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["GBufferGraphNode"]; got != 1 {
		t.Fatalf("GBufferGraphNode = %d, want 1", got)
	}
	if got := app.Profiler.Counts["GBufferPipelineReady"]; got != 0 {
		t.Fatalf("GBufferPipelineReady = %d, want 0", got)
	}
	if got := app.Profiler.Counts["GBufferBG0Ready"]; got != 0 {
		t.Fatalf("GBufferBG0Ready = %d, want 0", got)
	}
}

func TestRecordTiledLightCullPassClearsStateWithoutLocalLights(t *testing.T) {
	app := &App{
		Profiler: core.NewProfiler(),
		BufferManager: &gpu.GpuBufferManager{
			TileLightAvgCount: 7,
			TileLightMaxCount: 13,
		},
		Scene: &core.Scene{
			Lights: []core.Light{
				{Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 0}},
			},
		},
	}

	if err := app.recordTiledLightCullPass(nil); err != nil {
		t.Fatalf("recordTiledLightCullPass returned error: %v", err)
	}
	if app.BufferManager.TileLightAvgCount != 0 {
		t.Fatalf("avg count = %d, want 0", app.BufferManager.TileLightAvgCount)
	}
	if app.BufferManager.TileLightMaxCount != 0 {
		t.Fatalf("max count = %d, want 0", app.BufferManager.TileLightMaxCount)
	}
	if got := app.Profiler.Counts["TiledCullGraphNode"]; got != 1 {
		t.Fatalf("TiledCullGraphNode = %d, want 1", got)
	}
	if got := app.Profiler.Counts["TiledCullLocalLights"]; got != 0 {
		t.Fatalf("TiledCullLocalLights = %d, want 0", got)
	}
}

func TestRecordTiledLightCullPassRequiresEncoderForLocalLights(t *testing.T) {
	app := &App{
		Profiler:      core.NewProfiler(),
		BufferManager: &gpu.GpuBufferManager{},
		Scene: &core.Scene{
			Lights: []core.Light{
				{Params: [4]float32{0, 0, float32(core.LightTypePoint), 0}},
			},
		},
	}

	err := app.recordTiledLightCullPass(nil)
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["TiledCullGraphNode"]; got != 1 {
		t.Fatalf("TiledCullGraphNode = %d, want 1", got)
	}
	if got := app.Profiler.Counts["TiledCullLocalLights"]; got != 1 {
		t.Fatalf("TiledCullLocalLights = %d, want 1", got)
	}
}

func TestDefaultRenderGraphHiZNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeCoreHiZ)
	if node.Enabled(nil) {
		t.Fatal("expected hi-z node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected hi-z node to be disabled without buffer manager")
	}

	app.BufferManager = &gpu.GpuBufferManager{}
	if !node.Enabled(app) {
		t.Fatal("expected hi-z node to be enabled with buffer manager")
	}
}

func TestRecordHiZPassRequiresEncoder(t *testing.T) {
	app := &App{
		Profiler:      core.NewProfiler(),
		BufferManager: &gpu.GpuBufferManager{},
	}

	err := app.recordHiZPass(nil)
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["HiZGraphNode"]; got != 1 {
		t.Fatalf("HiZGraphNode = %d, want 1", got)
	}
	if got := app.Profiler.Counts["HiZPipelineReady"]; got != 0 {
		t.Fatalf("HiZPipelineReady = %d, want 0", got)
	}
	if got := app.Profiler.Counts["HiZDepthReady"]; got != 0 {
		t.Fatalf("HiZDepthReady = %d, want 0", got)
	}
}

func TestDefaultRenderGraphShadowsNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeCoreShadows)
	if node.Enabled(nil) {
		t.Fatal("expected shadows node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected shadows node to be disabled without buffer manager")
	}

	app.BufferManager = &gpu.GpuBufferManager{}
	if !node.Enabled(app) {
		t.Fatal("expected shadows node to be enabled with buffer manager")
	}
}

func TestRecordShadowPassAllowsNoScheduledUpdatesWithoutEncoder(t *testing.T) {
	app := &App{
		Profiler:      core.NewProfiler(),
		BufferManager: &gpu.GpuBufferManager{},
		Scene:         &core.Scene{},
	}

	if err := app.recordShadowPass(nil); err != nil {
		t.Fatalf("recordShadowPass returned error: %v", err)
	}
	if got := app.Profiler.Counts["ShadowGraphNode"]; got != 1 {
		t.Fatalf("ShadowGraphNode = %d, want 1", got)
	}
	if got := app.Profiler.Counts["ShadowUpdates"]; got != 0 {
		t.Fatalf("ShadowUpdates = %d, want 0", got)
	}
	if got := app.ShadowUpdateSummary; got != "none" {
		t.Fatalf("ShadowUpdateSummary = %q, want %q", got, "none")
	}
}

func TestRecordShadowPassRequiresEncoderForScheduledUpdates(t *testing.T) {
	app := &App{
		Profiler: core.NewProfiler(),
		BufferManager: &gpu.GpuBufferManager{
			ShadowLayerParams: []gpu.ShadowLayerParams{
				{
					Layer:               0,
					LightIndex:          0,
					CascadeIndex:        2,
					Kind:                core.ShadowUpdateKindDirectional,
					EffectiveResolution: 512,
				},
			},
		},
		Scene: &core.Scene{StructureRevision: 3},
	}

	err := app.recordShadowPass(nil)
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["ShadowGraphNode"]; got != 1 {
		t.Fatalf("ShadowGraphNode = %d, want 1", got)
	}
	if got := app.Profiler.Counts["ShadowUpdates"]; got != 1 {
		t.Fatalf("ShadowUpdates = %d, want 1", got)
	}
	if got := app.Profiler.Counts["ShadowDirectionalUpdates"]; got != 1 {
		t.Fatalf("ShadowDirectionalUpdates = %d, want 1", got)
	}
	if got := app.ShadowUpdateSummary; got != "D0:2@512" {
		t.Fatalf("ShadowUpdateSummary = %q, want %q", got, "D0:2@512")
	}
}

func TestDefaultRenderGraphLightingNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeCoreLighting)
	if node.Enabled(nil) {
		t.Fatal("expected lighting node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected lighting node to be disabled without buffer manager")
	}

	app.BufferManager = &gpu.GpuBufferManager{}
	if !node.Enabled(app) {
		t.Fatal("expected lighting node to be enabled with buffer manager")
	}
}

func TestRecordLightingPassRequiresEncoder(t *testing.T) {
	app := &App{
		Profiler:      core.NewProfiler(),
		BufferManager: &gpu.GpuBufferManager{},
	}

	err := app.recordLightingPass(nil, &FrameContext{Width: 8, Height: 8})
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["LightingGraphNode"]; got != 1 {
		t.Fatalf("LightingGraphNode = %d, want 1", got)
	}
	if got := app.Profiler.Counts["LightingPipelineReady"]; got != 0 {
		t.Fatalf("LightingPipelineReady = %d, want 0", got)
	}
}

func TestDefaultRenderGraphAccumulationNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeCoreAccumulation)
	if node.Enabled(nil) {
		t.Fatal("expected accumulation node to be disabled without app")
	}

	app := &App{}
	if node.Enabled(app) {
		t.Fatal("expected accumulation node to be disabled without buffer manager")
	}

	app.BufferManager = &gpu.GpuBufferManager{}
	if !node.Enabled(app) {
		t.Fatal("expected accumulation node to be enabled with buffer manager")
	}
}

func TestWaterFeatureAccumulationReadinessUsesResources(t *testing.T) {
	feature := &WaterFeature{}
	manager := &gpu.GpuBufferManager{
		WaterCount: 1,
		WaterBG0:   &wgpu.BindGroup{},
		WaterBG1:   &wgpu.BindGroup{},
		WaterBG2:   &wgpu.BindGroup{},
	}
	app := &App{
		BufferManager: manager,
	}

	if feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected water pass stage to require water resources")
	}

	app.WaterResources = &WaterResources{Pipeline: &wgpu.RenderPipeline{}}
	if !feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected water pass stage with resource pipeline and contribution")
	}
}

func TestRecordAccumulationPassSkipsWhenInactive(t *testing.T) {
	app := &App{
		Profiler:      core.NewProfiler(),
		BufferManager: &gpu.GpuBufferManager{},
	}

	if err := app.recordAccumulationPass(nil); err != nil {
		t.Fatalf("recordAccumulationPass returned error: %v", err)
	}
	if app.hadAccumulationPass() {
		t.Fatal("expected HadAccumulationPass to remain false")
	}
	if got := app.Profiler.Counts["AccumulationGraphNode"]; got != 1 {
		t.Fatalf("AccumulationGraphNode = %d, want 1", got)
	}
	if got := app.Profiler.Counts["AccumulationActive"]; got != 0 {
		t.Fatalf("AccumulationActive = %d, want 0", got)
	}
}

func TestRecordAccumulationPassActivatesForGraphOwnedContributor(t *testing.T) {
	feature := &graphOwnedTestFeature{
		testFeature: &testFeature{
			name:         "graph-owned",
			enabled:      true,
			hasPassStage: map[FeaturePassStage]bool{FeaturePassStageAccumulation: true},
		},
		nodes:     []string{RenderNodeCoreAccumulation},
		passStage: []FeaturePassStage{FeaturePassStageAccumulation},
	}
	app := &App{
		Profiler:      core.NewProfiler(),
		BufferManager: &gpu.GpuBufferManager{},
		features:      []Feature{feature},
	}

	if app.hasPassStageWork(FeaturePassStageAccumulation) {
		t.Fatal("expected legacy pass-stage query to skip graph-owned contributor")
	}
	if !app.hasPassStageWorkForRenderGraph(FeaturePassStageAccumulation) {
		t.Fatal("expected render graph pass-stage query to include graph-owned contributor")
	}

	err := app.recordAccumulationPass(nil)
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error for active accumulation, got %v", err)
	}
	if got := app.Profiler.Counts["AccumulationActive"]; got != 1 {
		t.Fatalf("AccumulationActive = %d, want 1", got)
	}
}

func TestRecordAccumulationPassRequiresEncoderForPreviousPassClear(t *testing.T) {
	app := &App{
		Profiler:              core.NewProfiler(),
		BufferManager:         &gpu.GpuBufferManager{},
		AccumulationResources: &AccumulationResources{HadPass: true},
	}

	err := app.recordAccumulationPass(nil)
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if app.hadAccumulationPass() {
		t.Fatal("expected HadAccumulationPass to reset after inactive frame")
	}
	if got := app.Profiler.Counts["AccumulationHadPrevious"]; got != 1 {
		t.Fatalf("AccumulationHadPrevious = %d, want 1", got)
	}
}

func TestDefaultRenderGraphResolveNodeGating(t *testing.T) {
	node := defaultRenderGraphNode(RenderNodeCoreResolve)
	if node.Enabled(nil) {
		t.Fatal("expected resolve node to be disabled without app")
	}

	app := &App{}
	if !node.Enabled(app) {
		t.Fatal("expected resolve node to be enabled with app")
	}
}

func TestRecordResolvePassRequiresEncoder(t *testing.T) {
	app := &App{
		Profiler: core.NewProfiler(),
	}

	err := app.recordResolvePass(nil, &FrameContext{SwapchainView: &wgpu.TextureView{}})
	if err == nil || !strings.Contains(err.Error(), "command encoder is nil") {
		t.Fatalf("expected nil-encoder error, got %v", err)
	}
	if got := app.Profiler.Counts["ResolveGraphNode"]; got != 1 {
		t.Fatalf("ResolveGraphNode = %d, want 1", got)
	}
	if got := app.Profiler.Counts["ResolvePipelineReady"]; got != 0 {
		t.Fatalf("ResolvePipelineReady = %d, want 0", got)
	}
	if got := app.Profiler.Counts["ResolveBGReady"]; got != 0 {
		t.Fatalf("ResolveBGReady = %d, want 0", got)
	}
}

type renderGraphStageRecordingFeature struct {
	name          string
	enabled       bool
	commandStages map[FeatureCommandStage]string
	screenStages  map[FeatureScreenStage]string
	calls         *[]string
}

func (f *renderGraphStageRecordingFeature) Name() string {
	return f.name
}

func (f *renderGraphStageRecordingFeature) Enabled(*App) bool {
	return f.enabled
}

func (f *renderGraphStageRecordingFeature) Setup(*App) error {
	return nil
}

func (f *renderGraphStageRecordingFeature) Resize(*App, uint32, uint32) error {
	return nil
}

func (f *renderGraphStageRecordingFeature) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (f *renderGraphStageRecordingFeature) Update(*App) error {
	return nil
}

func (f *renderGraphStageRecordingFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	if f.calls != nil {
		*f.calls = append(*f.calls, f.name+":"+f.screenStages[FeatureScreenStagePostResolve])
	}
	return nil
}

func (f *renderGraphStageRecordingFeature) Shutdown(*App) {}

func (f *renderGraphStageRecordingFeature) DispatchCommandStage(_ *App, stage FeatureCommandStage, _ *wgpu.CommandEncoder) error {
	if label, ok := f.commandStages[stage]; ok && f.calls != nil {
		*f.calls = append(*f.calls, f.name+":"+label)
	}
	return nil
}

func (f *renderGraphStageRecordingFeature) HasCommandStage(_ *App, stage FeatureCommandStage) bool {
	return f.commandStages[stage] != ""
}

func (f *renderGraphStageRecordingFeature) HasScreenStage(_ *App, stage FeatureScreenStage) bool {
	return f.screenStages[stage] != ""
}
