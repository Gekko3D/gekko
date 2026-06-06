package app

type FeatureResourceOwner string

const (
	FeatureResourceOwnerCore          FeatureResourceOwner = "core"
	FeatureResourceOwnerFeatureHooks  FeatureResourceOwner = "feature-hooks"
	FeatureResourceOwnerBufferManager FeatureResourceOwner = "buffer-manager"
	FeatureResourceOwnerSharedPass    FeatureResourceOwner = "shared-pass"
)

type FeatureResourceInventoryItem struct {
	FeatureName        string
	Owner              FeatureResourceOwner
	AppFields          []string
	BufferManagerState []string
	NextStep           string
}

func DefaultFeatureResourceInventory() []FeatureResourceInventoryItem {
	return []FeatureResourceInventoryItem{
		{
			FeatureName: "core",
			Owner:       FeatureResourceOwnerCore,
			AppFields: []string{
				"DebugComputePipeline",
				"RenderPipeline",
				"ResolvePipeline",
				"ResolveBG",
				"GBufferPipeline",
				"TiledLightCullPipeline",
				"LightingPipeline",
				"StorageTexture",
				"StorageView",
				"Sampler",
				"BindGroup1Debug",
				"RenderBG",
				"BufferManager",
				"Scene",
				"Camera",
			},
			NextStep: "keep under core app or buffer manager",
		},
		{
			FeatureName: "text",
			Owner:       FeatureResourceOwnerFeatureHooks,
			AppFields:   []string{"TextResources"},
			NextStep:    "renderer, atlas, pipeline, bind group, vertex buffer, queued overlay items, and vertex count moved behind a resource holder",
		},
		{
			FeatureName: "gizmos",
			Owner:       FeatureResourceOwnerFeatureHooks,
			AppFields:   []string{"GizmoResources"},
			NextStep:    "first raw App pipeline/pass relocation; keep graph access through the resource holder",
		},
		{
			FeatureName:        "skybox",
			Owner:              FeatureResourceOwnerBufferManager,
			AppFields:          []string{"SkyboxResources"},
			BufferManagerState: []string{"SkyboxGenPipeline"},
			NextStep:           "renderer-side input handoff moved behind a resource holder; GPU application is graph-owned while texture/pipeline resources remain in BufferManager",
		},
		{
			FeatureName: "ca-volumes",
			Owner:       FeatureResourceOwnerFeatureHooks,
			AppFields:   []string{"CAVolumeResources"},
			BufferManagerState: []string{
				"CAVolumeSimPipeline",
				"CAVolumeBoundsPipeline",
				"CAVolume* buffers/textures/bind groups",
			},
			NextStep: "render/sim/bounds pipelines and previous-pass state moved behind a resource holder; buffers, targets, counters, bind groups, and mirrored compute pipelines remain in BufferManager",
		},
		{
			FeatureName:        "analytic-media",
			Owner:              FeatureResourceOwnerFeatureHooks,
			AppFields:          []string{"AnalyticMediumResources"},
			BufferManagerState: []string{"AnalyticMedium* buffers/textures/bind groups"},
			NextStep:           "pipeline moved behind a resource holder; media buffers, targets, bind groups, and contribution readiness remain in BufferManager",
		},
		{
			FeatureName:        "astronomical",
			Owner:              FeatureResourceOwnerFeatureHooks,
			AppFields:          []string{"AstronomicalResources"},
			BufferManagerState: []string{"Astronomical* buffers/bind groups"},
			NextStep:           "pipeline moved behind a resource holder; astronomical buffers, bind groups, and contribution readiness remain in BufferManager",
		},
		{
			FeatureName:        "planet-bodies",
			Owner:              FeatureResourceOwnerFeatureHooks,
			AppFields:          []string{"PlanetBodyResources"},
			BufferManagerState: []string{"PlanetBody* buffers/bind groups"},
			NextStep:           "pipeline moved behind a resource holder; planet-body buffers, bind groups, depth target, and contribution readiness remain in BufferManager",
		},
		{
			FeatureName:        "far_planet_ring",
			Owner:              FeatureResourceOwnerFeatureHooks,
			AppFields:          []string{"FarPlanetRingResources"},
			BufferManagerState: []string{"FarPlanetRing* buffers/bind groups"},
			NextStep:           "pipeline moved behind a resource holder; bind groups and contribution readiness remain in BufferManager",
		},
		{
			FeatureName:        "debris_midfield",
			Owner:              FeatureResourceOwnerFeatureHooks,
			AppFields:          []string{"DebrisMidfieldResources"},
			BufferManagerState: []string{"DebrisMidfield* buffers/bind groups"},
			NextStep:           "pipeline moved behind a resource holder; bind groups and contribution readiness remain in BufferManager",
		},
		{
			FeatureName:        "water",
			Owner:              FeatureResourceOwnerFeatureHooks,
			AppFields:          []string{"WaterResources"},
			BufferManagerState: []string{"Water* buffers/bind groups"},
			NextStep:           "pipeline moved behind a resource holder; bind groups and contribution readiness remain in BufferManager",
		},
		{
			FeatureName:        "transparency",
			Owner:              FeatureResourceOwnerSharedPass,
			AppFields:          []string{"AccumulationResources"},
			BufferManagerState: []string{"Transparent overlay bind groups", "WBOIT targets"},
			NextStep:           "transparent pipeline and previous-pass state moved behind a shared accumulation resource holder; WBOIT targets and overlay bind groups remain in BufferManager",
		},
		{
			FeatureName:        "particles",
			Owner:              FeatureResourceOwnerFeatureHooks,
			AppFields:          []string{"ParticleResources"},
			BufferManagerState: []string{"Particle* buffers/bind groups", "ParticleSimPipeline"},
			NextStep:           "render/sim pipelines, spawn count, and atlas bootstrap state moved behind a resource holder; buffers and bind groups remain split",
		},
		{
			FeatureName:        "sprites",
			Owner:              FeatureResourceOwnerFeatureHooks,
			AppFields:          []string{"SpriteResources"},
			BufferManagerState: []string{"Sprite atlas/cache/batches/bind groups"},
			NextStep:           "pipeline moved behind a resource holder; atlas, batches, and contribution readiness remain in BufferManager",
		},
	}
}
