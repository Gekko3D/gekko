package app

import "github.com/cogentcore/webgpu/wgpu"

const (
	RenderNodeFeatureParticlesSim      = "feature-particles-sim"
	RenderNodeFeaturePreGBuffer        = "feature-pre-gbuffer"
	RenderNodeFeatureCAVolumesSim      = "feature-ca-volumes-sim"
	RenderNodeFeaturePreGBufferVolumes = "feature-pre-gbuffer-volumes"
	RenderNodeCoreGBuffer              = "core-gbuffer"
	RenderNodeCoreHiZ                  = "core-hiz"
	RenderNodeFeaturePostGBuffer       = "feature-post-gbuffer"
	RenderNodeCoreShadows              = "core-shadows"
	RenderNodeFeaturePreLighting       = "feature-pre-lighting"
	RenderNodeFeatureSkyboxUpdate      = "feature-skybox-update"
	RenderNodeCoreTiledLightCull       = "core-tiled-light-cull"
	RenderNodeCoreLighting             = "core-lighting"
	RenderNodeFeaturePostLighting      = "feature-post-lighting"
	RenderNodeFeatureCAVolumesRender   = "feature-ca-volumes-render"
	RenderNodeFeatureAstronomical      = "feature-astronomical"
	RenderNodeFeaturePlanetBodies      = "feature-planet-bodies"
	RenderNodeFeatureAnalyticMedia     = "feature-analytic-media"
	RenderNodeCoreDebugScene           = "core-debug-scene"
	RenderNodeCoreAccumulation         = "core-accumulation"
	RenderNodeFeaturePreResolve        = "feature-pre-resolve"
	RenderNodeCoreResolve              = "core-resolve"
	RenderNodeFeatureTextOverlay       = "feature-text-overlay"
	RenderNodeFeatureGizmosOverlay     = "feature-gizmos-overlay"
	RenderNodeFeaturePostResolve       = "feature-post-resolve"
)

// NewDefaultRenderGraph declares the current App.Render order as graph nodes.
// Core compatibility nodes are no-ops until their passes migrate out of
// App.Render; feature-stage nodes already wrap the existing feature registry.
func NewDefaultRenderGraph() *RenderGraph {
	graph := NewRenderGraph()
	for _, spec := range defaultRenderGraphSpecs() {
		graph.Register(spec)
	}
	return graph
}

func defaultRenderGraphSpecs() []RenderNodeSpec {
	return []RenderNodeSpec{
		defaultRenderGraphSpec(RenderNodeFeatureParticlesSim),
		defaultRenderGraphSpec(RenderNodeFeaturePreGBuffer, RenderNodeFeatureParticlesSim),
		defaultRenderGraphSpec(RenderNodeFeatureCAVolumesSim, RenderNodeFeaturePreGBuffer),
		defaultRenderGraphSpec(RenderNodeFeaturePreGBufferVolumes, RenderNodeFeatureCAVolumesSim),
		defaultRenderGraphSpec(RenderNodeCoreGBuffer, RenderNodeFeaturePreGBufferVolumes),
		defaultRenderGraphSpec(RenderNodeCoreHiZ, RenderNodeCoreGBuffer),
		defaultRenderGraphSpec(RenderNodeFeaturePostGBuffer, RenderNodeCoreHiZ),
		defaultRenderGraphSpec(RenderNodeCoreShadows, RenderNodeFeaturePostGBuffer),
		defaultRenderGraphSpec(RenderNodeFeaturePreLighting, RenderNodeCoreShadows),
		defaultRenderGraphSpec(RenderNodeFeatureSkyboxUpdate, RenderNodeFeaturePreLighting),
		defaultRenderGraphSpec(RenderNodeCoreTiledLightCull, RenderNodeFeatureSkyboxUpdate),
		defaultRenderGraphSpec(RenderNodeCoreLighting, RenderNodeCoreTiledLightCull),
		defaultRenderGraphSpec(RenderNodeFeaturePostLighting, RenderNodeCoreLighting),
		defaultRenderGraphSpec(RenderNodeFeatureCAVolumesRender, RenderNodeFeaturePostLighting),
		defaultRenderGraphSpec(RenderNodeFeatureAstronomical, RenderNodeFeatureCAVolumesRender),
		defaultRenderGraphSpec(RenderNodeFeaturePlanetBodies, RenderNodeFeatureAstronomical),
		defaultRenderGraphSpec(RenderNodeFeatureAnalyticMedia, RenderNodeFeaturePlanetBodies),
		defaultRenderGraphSpec(RenderNodeCoreDebugScene, RenderNodeFeatureAnalyticMedia),
		defaultRenderGraphSpec(RenderNodeCoreAccumulation, RenderNodeCoreDebugScene),
		defaultRenderGraphSpec(RenderNodeFeaturePreResolve, RenderNodeCoreAccumulation),
		defaultRenderGraphSpec(RenderNodeCoreResolve, RenderNodeFeaturePreResolve),
		defaultRenderGraphSpec(RenderNodeFeatureTextOverlay, RenderNodeCoreResolve),
		defaultRenderGraphSpec(RenderNodeFeatureGizmosOverlay, RenderNodeFeatureTextOverlay),
		defaultRenderGraphSpec(RenderNodeFeaturePostResolve, RenderNodeFeatureGizmosOverlay),
	}
}

func defaultRenderGraphSpec(name string, after ...string) RenderNodeSpec {
	return RenderNodeSpec{
		Name:  name,
		After: append([]string(nil), after...),
		Node:  defaultRenderGraphNode(name),
	}
}

func defaultRenderGraphNode(name string) RenderNode {
	switch name {
	case RenderNodeFeatureParticlesSim:
		return particlesSimulationRenderNode{name: name}
	case RenderNodeFeaturePreGBuffer:
		return featureCommandStageRenderNode{
			name:  name,
			scope: "Feature Pre-GBuffer",
			stage: FeatureCommandStagePreGBuffer,
		}
	case RenderNodeFeatureCAVolumesSim:
		return caVolumesSimulationRenderNode{name: name}
	case RenderNodeFeaturePreGBufferVolumes:
		return featureCommandStageRenderNode{
			name:  name,
			scope: "Feature Pre-GBuffer Volumes",
			stage: FeatureCommandStagePreGBufferVolumes,
		}
	case RenderNodeCoreGBuffer:
		return coreGBufferRenderNode{name: name}
	case RenderNodeFeaturePostGBuffer:
		return featureCommandStageRenderNode{
			name:  name,
			scope: "Feature Post-GBuffer",
			stage: FeatureCommandStagePostGBuffer,
		}
	case RenderNodeCoreHiZ:
		return coreHiZRenderNode{name: name}
	case RenderNodeCoreShadows:
		return coreShadowsRenderNode{name: name}
	case RenderNodeFeaturePreLighting:
		return featureCommandStageRenderNode{
			name:  name,
			scope: "Feature Pre-Lighting",
			stage: FeatureCommandStagePreLighting,
		}
	case RenderNodeFeatureSkyboxUpdate:
		return skyboxUpdateRenderNode{name: name}
	case RenderNodeCoreTiledLightCull:
		return coreTiledLightCullRenderNode{name: name}
	case RenderNodeCoreLighting:
		return coreLightingRenderNode{name: name}
	case RenderNodeFeaturePostLighting:
		return featureCommandStageRenderNode{
			name:  name,
			scope: "Feature Post-Lighting",
			stage: FeatureCommandStagePostLighting,
		}
	case RenderNodeFeatureCAVolumesRender:
		return caVolumesRenderNode{name: name}
	case RenderNodeFeatureAstronomical:
		return astronomicalRenderNode{name: name}
	case RenderNodeFeaturePlanetBodies:
		return planetBodiesRenderNode{name: name}
	case RenderNodeFeatureAnalyticMedia:
		return analyticMediumRenderNode{name: name}
	case RenderNodeCoreDebugScene:
		return coreDebugSceneRenderNode{name: name}
	case RenderNodeCoreAccumulation:
		return coreAccumulationRenderNode{name: name}
	case RenderNodeFeaturePreResolve:
		return featureCommandStageRenderNode{
			name:  name,
			scope: "Feature Pre-Resolve",
			stage: FeatureCommandStagePreResolve,
		}
	case RenderNodeCoreResolve:
		return coreResolveRenderNode{name: name}
	case RenderNodeFeatureTextOverlay:
		return textOverlayRenderNode{name: name}
	case RenderNodeFeatureGizmosOverlay:
		return gizmosOverlayRenderNode{name: name}
	case RenderNodeFeaturePostResolve:
		return featureScreenStageRenderNode{
			name:  name,
			scope: "Feature Post-Resolve",
			stage: FeatureScreenStagePostResolve,
		}
	default:
		return renderGraphNoOpNode{name: name}
	}
}

type renderGraphNoOpNode struct {
	name string
}

func (n renderGraphNoOpNode) Name() string {
	return n.name
}

func (n renderGraphNoOpNode) Enabled(*App) bool {
	return true
}

func (n renderGraphNoOpNode) Setup(*App) error {
	return nil
}

func (n renderGraphNoOpNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n renderGraphNoOpNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n renderGraphNoOpNode) Update(*App) error {
	return nil
}

func (n renderGraphNoOpNode) Record(*App, *wgpu.CommandEncoder, *FrameContext) error {
	return nil
}

func (n renderGraphNoOpNode) Shutdown(*App) {}

type coreGBufferRenderNode struct {
	name string
}

type skyboxUpdateRenderNode struct {
	name string
}

func (n skyboxUpdateRenderNode) Name() string {
	return n.name
}

func (n skyboxUpdateRenderNode) Enabled(a *App) bool {
	return a != nil && a.hasFeatureGraphNode(RenderNodeFeatureSkyboxUpdate)
}

func (n skyboxUpdateRenderNode) Setup(*App) error {
	return nil
}

func (n skyboxUpdateRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n skyboxUpdateRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n skyboxUpdateRenderNode) Update(a *App) error {
	a.ApplySkyboxInput()
	return nil
}

func (n skyboxUpdateRenderNode) Record(*App, *wgpu.CommandEncoder, *FrameContext) error {
	return nil
}

func (n skyboxUpdateRenderNode) Shutdown(*App) {}

func (n coreGBufferRenderNode) Name() string {
	return n.name
}

func (n coreGBufferRenderNode) Enabled(a *App) bool {
	return a.gBufferPassEnabled()
}

func (n coreGBufferRenderNode) Setup(*App) error {
	return nil
}

func (n coreGBufferRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n coreGBufferRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n coreGBufferRenderNode) Update(*App) error {
	return nil
}

func (n coreGBufferRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	return a.recordGBufferPass(encoder, frame)
}

func (n coreGBufferRenderNode) Shutdown(*App) {}

type coreTiledLightCullRenderNode struct {
	name string
}

func (n coreTiledLightCullRenderNode) Name() string {
	return n.name
}

func (n coreTiledLightCullRenderNode) Enabled(a *App) bool {
	return a.tiledLightCullPassEnabled()
}

func (n coreTiledLightCullRenderNode) Setup(*App) error {
	return nil
}

func (n coreTiledLightCullRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n coreTiledLightCullRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n coreTiledLightCullRenderNode) Update(*App) error {
	return nil
}

func (n coreTiledLightCullRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, _ *FrameContext) error {
	return a.recordTiledLightCullPass(encoder)
}

func (n coreTiledLightCullRenderNode) Shutdown(*App) {}

type coreHiZRenderNode struct {
	name string
}

func (n coreHiZRenderNode) Name() string {
	return n.name
}

func (n coreHiZRenderNode) Enabled(a *App) bool {
	return a.hiZPassEnabled()
}

func (n coreHiZRenderNode) Setup(*App) error {
	return nil
}

func (n coreHiZRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n coreHiZRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n coreHiZRenderNode) Update(*App) error {
	return nil
}

func (n coreHiZRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, _ *FrameContext) error {
	return a.recordHiZPass(encoder)
}

func (n coreHiZRenderNode) Shutdown(*App) {}

type coreShadowsRenderNode struct {
	name string
}

func (n coreShadowsRenderNode) Name() string {
	return n.name
}

func (n coreShadowsRenderNode) Enabled(a *App) bool {
	return a.shadowPassEnabled()
}

func (n coreShadowsRenderNode) Setup(*App) error {
	return nil
}

func (n coreShadowsRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n coreShadowsRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n coreShadowsRenderNode) Update(*App) error {
	return nil
}

func (n coreShadowsRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, _ *FrameContext) error {
	return a.recordShadowPass(encoder)
}

func (n coreShadowsRenderNode) Shutdown(*App) {}

type coreLightingRenderNode struct {
	name string
}

func (n coreLightingRenderNode) Name() string {
	return n.name
}

func (n coreLightingRenderNode) Enabled(a *App) bool {
	return a.lightingPassEnabled()
}

func (n coreLightingRenderNode) Setup(*App) error {
	return nil
}

func (n coreLightingRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n coreLightingRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n coreLightingRenderNode) Update(*App) error {
	return nil
}

func (n coreLightingRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	return a.recordLightingPass(encoder, frame)
}

func (n coreLightingRenderNode) Shutdown(*App) {}

type coreAccumulationRenderNode struct {
	name string
}

func (n coreAccumulationRenderNode) Name() string {
	return n.name
}

func (n coreAccumulationRenderNode) Enabled(a *App) bool {
	return a.accumulationPassEnabled()
}

func (n coreAccumulationRenderNode) Setup(*App) error {
	return nil
}

func (n coreAccumulationRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n coreAccumulationRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n coreAccumulationRenderNode) Update(*App) error {
	return nil
}

func (n coreAccumulationRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, _ *FrameContext) error {
	return a.recordAccumulationPass(encoder)
}

func (n coreAccumulationRenderNode) Shutdown(*App) {}

type coreResolveRenderNode struct {
	name string
}

func (n coreResolveRenderNode) Name() string {
	return n.name
}

func (n coreResolveRenderNode) Enabled(a *App) bool {
	return a.resolvePassEnabled()
}

func (n coreResolveRenderNode) Setup(*App) error {
	return nil
}

func (n coreResolveRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n coreResolveRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n coreResolveRenderNode) Update(*App) error {
	return nil
}

func (n coreResolveRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	return a.recordResolvePass(encoder, frame)
}

func (n coreResolveRenderNode) Shutdown(*App) {}

type coreDebugSceneRenderNode struct {
	name string
}

func (n coreDebugSceneRenderNode) Name() string {
	return n.name
}

func (n coreDebugSceneRenderNode) Enabled(a *App) bool {
	return a.debugScenePassEnabled()
}

func (n coreDebugSceneRenderNode) Setup(*App) error {
	return nil
}

func (n coreDebugSceneRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n coreDebugSceneRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n coreDebugSceneRenderNode) Update(*App) error {
	return nil
}

func (n coreDebugSceneRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	return a.recordDebugScenePass(encoder, frame)
}

func (n coreDebugSceneRenderNode) Shutdown(*App) {}

type textOverlayRenderNode struct {
	name string
}

func (n textOverlayRenderNode) Name() string {
	return n.name
}

func (n textOverlayRenderNode) Enabled(a *App) bool {
	return a.textOverlayGraphNodeEnabled()
}

func (n textOverlayRenderNode) Setup(*App) error {
	return nil
}

func (n textOverlayRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n textOverlayRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n textOverlayRenderNode) Update(*App) error {
	return nil
}

func (n textOverlayRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	return a.recordTextOverlayPass(encoder, frame)
}

func (n textOverlayRenderNode) Shutdown(*App) {}

type gizmosOverlayRenderNode struct {
	name string
}

func (n gizmosOverlayRenderNode) Name() string {
	return n.name
}

func (n gizmosOverlayRenderNode) Enabled(a *App) bool {
	return a.gizmosOverlayGraphNodeEnabled()
}

func (n gizmosOverlayRenderNode) Setup(*App) error {
	return nil
}

func (n gizmosOverlayRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n gizmosOverlayRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n gizmosOverlayRenderNode) Update(*App) error {
	return nil
}

func (n gizmosOverlayRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	return a.recordGizmosOverlayPass(encoder, frame)
}

func (n gizmosOverlayRenderNode) Shutdown(*App) {}

type particlesSimulationRenderNode struct {
	name string
}

func (n particlesSimulationRenderNode) Name() string {
	return n.name
}

func (n particlesSimulationRenderNode) Enabled(a *App) bool {
	return a.particlesSimulationGraphNodeEnabled()
}

func (n particlesSimulationRenderNode) Setup(*App) error {
	return nil
}

func (n particlesSimulationRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n particlesSimulationRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n particlesSimulationRenderNode) Update(*App) error {
	return nil
}

func (n particlesSimulationRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, _ *FrameContext) error {
	return a.recordParticlesSimulationPass(encoder)
}

func (n particlesSimulationRenderNode) Shutdown(*App) {}

type caVolumesSimulationRenderNode struct {
	name string
}

func (n caVolumesSimulationRenderNode) Name() string {
	return n.name
}

func (n caVolumesSimulationRenderNode) Enabled(a *App) bool {
	return a.caVolumesSimulationGraphNodeEnabled()
}

func (n caVolumesSimulationRenderNode) Setup(*App) error {
	return nil
}

func (n caVolumesSimulationRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n caVolumesSimulationRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n caVolumesSimulationRenderNode) Update(*App) error {
	return nil
}

func (n caVolumesSimulationRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, _ *FrameContext) error {
	return a.recordCAVolumeSimulationPass(encoder)
}

func (n caVolumesSimulationRenderNode) Shutdown(*App) {}

type analyticMediumRenderNode struct {
	name string
}

func (n analyticMediumRenderNode) Name() string {
	return n.name
}

func (n analyticMediumRenderNode) Enabled(a *App) bool {
	return a.analyticMediumGraphNodeEnabled()
}

func (n analyticMediumRenderNode) Setup(*App) error {
	return nil
}

func (n analyticMediumRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n analyticMediumRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n analyticMediumRenderNode) Update(*App) error {
	return nil
}

func (n analyticMediumRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, _ *FrameContext) error {
	return a.recordAnalyticMediumPass(encoder)
}

func (n analyticMediumRenderNode) Shutdown(*App) {}

type planetBodiesRenderNode struct {
	name string
}

func (n planetBodiesRenderNode) Name() string {
	return n.name
}

func (n planetBodiesRenderNode) Enabled(a *App) bool {
	return a.planetBodiesGraphNodeEnabled()
}

func (n planetBodiesRenderNode) Setup(*App) error {
	return nil
}

func (n planetBodiesRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n planetBodiesRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n planetBodiesRenderNode) Update(*App) error {
	return nil
}

func (n planetBodiesRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, _ *FrameContext) error {
	return a.recordPlanetBodiesPass(encoder)
}

func (n planetBodiesRenderNode) Shutdown(*App) {}

type astronomicalRenderNode struct {
	name string
}

func (n astronomicalRenderNode) Name() string {
	return n.name
}

func (n astronomicalRenderNode) Enabled(a *App) bool {
	return a.astronomicalGraphNodeEnabled()
}

func (n astronomicalRenderNode) Setup(*App) error {
	return nil
}

func (n astronomicalRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n astronomicalRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n astronomicalRenderNode) Update(*App) error {
	return nil
}

func (n astronomicalRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, _ *FrameContext) error {
	return a.recordAstronomicalPass(encoder)
}

func (n astronomicalRenderNode) Shutdown(*App) {}

type caVolumesRenderNode struct {
	name string
}

func (n caVolumesRenderNode) Name() string {
	return n.name
}

func (n caVolumesRenderNode) Enabled(a *App) bool {
	return a.caVolumesRenderGraphNodeEnabled()
}

func (n caVolumesRenderNode) Setup(*App) error {
	return nil
}

func (n caVolumesRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n caVolumesRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n caVolumesRenderNode) Update(*App) error {
	return nil
}

func (n caVolumesRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, _ *FrameContext) error {
	return a.recordCAVolumeRenderPass(encoder)
}

func (n caVolumesRenderNode) Shutdown(*App) {}

type featureCommandStageRenderNode struct {
	name  string
	scope string
	stage FeatureCommandStage
}

func (n featureCommandStageRenderNode) Name() string {
	return n.name
}

func (n featureCommandStageRenderNode) Enabled(a *App) bool {
	return a != nil && a.hasCommandStageWork(n.stage)
}

func (n featureCommandStageRenderNode) Setup(*App) error {
	return nil
}

func (n featureCommandStageRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n featureCommandStageRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n featureCommandStageRenderNode) Update(*App) error {
	return nil
}

func (n featureCommandStageRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, _ *FrameContext) error {
	if a == nil || !a.hasCommandStageWork(n.stage) {
		return nil
	}
	a.Profiler.BeginScope(n.scope)
	defer a.Profiler.EndScope(n.scope)
	return a.dispatchCommandStage(n.stage, encoder)
}

func (n featureCommandStageRenderNode) Shutdown(*App) {}

type featureScreenStageRenderNode struct {
	name  string
	scope string
	stage FeatureScreenStage
}

func (n featureScreenStageRenderNode) Name() string {
	return n.name
}

func (n featureScreenStageRenderNode) Enabled(a *App) bool {
	return a != nil && a.hasScreenStageWork(n.stage)
}

func (n featureScreenStageRenderNode) Setup(*App) error {
	return nil
}

func (n featureScreenStageRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n featureScreenStageRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n featureScreenStageRenderNode) Update(*App) error {
	return nil
}

func (n featureScreenStageRenderNode) Record(a *App, encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	if a == nil || frame == nil || frame.SwapchainView == nil || !a.hasScreenStageWork(n.stage) {
		return nil
	}
	a.Profiler.BeginScope(n.scope)
	defer a.Profiler.EndScope(n.scope)
	return a.renderScreenStage(n.stage, encoder, frame.SwapchainView)
}

func (n featureScreenStageRenderNode) Shutdown(*App) {}
