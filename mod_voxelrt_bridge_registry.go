package gekko

import app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"

type VoxelRtBridgeFeature string

const (
	VoxelRtBridgeFeatureText           VoxelRtBridgeFeature = "text"
	VoxelRtBridgeFeatureGizmos         VoxelRtBridgeFeature = "gizmos"
	VoxelRtBridgeFeatureParticles      VoxelRtBridgeFeature = "particles"
	VoxelRtBridgeFeatureWater          VoxelRtBridgeFeature = "water"
	VoxelRtBridgeFeatureAnalyticMedia  VoxelRtBridgeFeature = "analytic-media"
	VoxelRtBridgeFeatureFarPlanetRings VoxelRtBridgeFeature = "far-planet-rings"
	VoxelRtBridgeFeatureDebrisMidfield VoxelRtBridgeFeature = "debris-midfield"
	VoxelRtBridgeFeaturePlanetBodies   VoxelRtBridgeFeature = "planet-bodies"
	VoxelRtBridgeFeatureAstronomical   VoxelRtBridgeFeature = "astronomical"
	VoxelRtBridgeFeatureCAVolumes      VoxelRtBridgeFeature = "ca-volumes"
	VoxelRtBridgeFeatureSprites        VoxelRtBridgeFeature = "sprites"
	VoxelRtBridgeFeatureSkybox         VoxelRtBridgeFeature = "skybox"
)

const (
	voxelRtBridgeFeatureText           = VoxelRtBridgeFeatureText
	voxelRtBridgeFeatureGizmos         = VoxelRtBridgeFeatureGizmos
	voxelRtBridgeFeatureParticles      = VoxelRtBridgeFeatureParticles
	voxelRtBridgeFeatureWater          = VoxelRtBridgeFeatureWater
	voxelRtBridgeFeatureAnalyticMedia  = VoxelRtBridgeFeatureAnalyticMedia
	voxelRtBridgeFeatureFarPlanetRings = VoxelRtBridgeFeatureFarPlanetRings
	voxelRtBridgeFeatureDebrisMidfield = VoxelRtBridgeFeatureDebrisMidfield
	voxelRtBridgeFeaturePlanetBodies   = VoxelRtBridgeFeaturePlanetBodies
	voxelRtBridgeFeatureAstronomical   = VoxelRtBridgeFeatureAstronomical
	voxelRtBridgeFeatureCAVolumes      = VoxelRtBridgeFeatureCAVolumes
	voxelRtBridgeFeatureSprites        = VoxelRtBridgeFeatureSprites
	voxelRtBridgeFeatureSkybox         = VoxelRtBridgeFeatureSkybox
)

// VoxelRtBridgeFeatureRegistration declares when an ECS-to-renderer bridge is owned
// by an enabled renderer feature. AppFeatureName is optional for features with a
// unique graph node; use it for shared graph nodes such as core accumulation.
type VoxelRtBridgeFeatureRegistration struct {
	Feature                    VoxelRtBridgeFeature
	AppFeatureName             string
	RequiredGraphNodes         []string
	PreRenderBatchedSystem     any
	PreRenderAfterBatchSystem  any
	PreRenderSystem            any
	PreRenderAfterUpdateSystem any
}

type voxelRtBridgeRegistry map[VoxelRtBridgeFeature]VoxelRtBridgeFeatureRegistration

func DefaultVoxelRtBridgeFeatureRegistrations() []VoxelRtBridgeFeatureRegistration {
	return []VoxelRtBridgeFeatureRegistration{
		{Feature: VoxelRtBridgeFeatureText, RequiredGraphNodes: []string{app_rt.RenderNodeFeatureTextOverlay}, PreRenderSystem: voxelRtTextBridgeSystem},
		{Feature: VoxelRtBridgeFeatureGizmos, RequiredGraphNodes: []string{app_rt.RenderNodeFeatureGizmosOverlay}, PreRenderSystem: voxelRtGizmoBridgeSystem},
		{Feature: VoxelRtBridgeFeatureParticles, RequiredGraphNodes: []string{app_rt.RenderNodeFeatureParticlesSim}, PreRenderAfterBatchSystem: voxelRtParticlesBridgeSystem},
		{Feature: VoxelRtBridgeFeatureWater, AppFeatureName: "water", RequiredGraphNodes: []string{app_rt.RenderNodeCoreAccumulation}, PreRenderBatchedSystem: voxelRtWaterBridgeSystem},
		{Feature: VoxelRtBridgeFeatureAnalyticMedia, AppFeatureName: "analytic-media", RequiredGraphNodes: []string{app_rt.RenderNodeFeatureAnalyticMedia}, PreRenderBatchedSystem: voxelRtAnalyticMediaBridgeSystem},
		{Feature: VoxelRtBridgeFeatureFarPlanetRings, AppFeatureName: "far_planet_ring", RequiredGraphNodes: []string{app_rt.RenderNodeCoreAccumulation}, PreRenderBatchedSystem: voxelRtFarPlanetRingBridgeSystem},
		{Feature: VoxelRtBridgeFeatureDebrisMidfield, AppFeatureName: "debris_midfield", RequiredGraphNodes: []string{app_rt.RenderNodeCoreAccumulation}, PreRenderBatchedSystem: voxelRtDebrisMidfieldBridgeSystem},
		{Feature: VoxelRtBridgeFeaturePlanetBodies, AppFeatureName: "planet-bodies", RequiredGraphNodes: []string{app_rt.RenderNodeFeaturePlanetBodies}, PreRenderBatchedSystem: voxelRtPlanetBodyBridgeSystem},
		{Feature: VoxelRtBridgeFeatureAstronomical, AppFeatureName: "astronomical", RequiredGraphNodes: []string{app_rt.RenderNodeFeatureAstronomical}, PreRenderBatchedSystem: voxelRtAstronomicalBridgeSystem},
		{Feature: VoxelRtBridgeFeatureCAVolumes, AppFeatureName: "ca-volumes", RequiredGraphNodes: []string{app_rt.RenderNodeFeatureCAVolumesSim, app_rt.RenderNodeFeatureCAVolumesRender}, PreRenderBatchedSystem: voxelRtCAVolumeBridgeSystem},
		{Feature: VoxelRtBridgeFeatureSprites, AppFeatureName: "sprites", RequiredGraphNodes: []string{app_rt.RenderNodeCoreAccumulation}, PreRenderAfterBatchSystem: voxelRtSpritesBridgeSystem},
		{Feature: VoxelRtBridgeFeatureSkybox, AppFeatureName: "skybox", RequiredGraphNodes: []string{app_rt.RenderNodeFeatureSkyboxUpdate}, PreRenderSystem: voxelRtSkyboxBridgeSystem},
	}
}

func voxelRtBridgeRegistryFrom(registrations []VoxelRtBridgeFeatureRegistration) voxelRtBridgeRegistry {
	registry := make(voxelRtBridgeRegistry, len(registrations))
	for _, registration := range registrations {
		if registration.Feature == "" {
			continue
		}
		copied := registration
		if len(registration.RequiredGraphNodes) > 0 {
			copied.RequiredGraphNodes = append([]string(nil), registration.RequiredGraphNodes...)
		}
		registry[registration.Feature] = copied
	}
	return registry
}

func (registry voxelRtBridgeRegistry) enabled(app *app_rt.App, feature VoxelRtBridgeFeature) bool {
	if app == nil {
		return false
	}
	registration, ok := registry[feature]
	if !ok {
		return false
	}
	if registration.AppFeatureName != "" && !app.HasFeature(registration.AppFeatureName) {
		return false
	}
	for _, nodeName := range registration.RequiredGraphNodes {
		if !app.HasFeatureGraphNode(nodeName) {
			return false
		}
	}
	return true
}
