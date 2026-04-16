package gekko

import (
	"math"
	"sort"
	"time"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/google/uuid"
)

func (mod VoxelRtModule) Install(app *App, cmd *Commands) {
	windowState := createWindowState(mod.WindowWidth, mod.WindowHeight, mod.WindowTitle)
	cmd.AddResources(windowState)
	RtApp := app_rt.NewApp(windowState.windowGlfw)
	RtApp.DebugMode = mod.DebugMode
	RtApp.RenderMode = uint32(mod.RenderMode)
	RtApp.QualityPreset = mod.QualityPreset
	RtApp.LightingQuality = mod.LightingQuality
	RtApp.OcclusionMode = mod.OcclusionMode
	RtApp.FontPath = mod.FontPath
	RtApp.UIFontSize = mod.UIFontSize
	if mod.FeatureConfig != nil {
		RtApp.FeatureConfig = *mod.FeatureConfig
	}
	if err := RtApp.Init(); err != nil {
		panic(err)
	}

	state := &VoxelRtState{
		RtApp:              RtApp,
		loadedModels:       make(map[AssetId]*core.VoxelObject),
		instanceMap:        make(map[EntityId]*core.VoxelObject),
		lastMaterialKeys:   make(map[*core.VoxelObject]materialTableCacheKey),
		materialTableCache: make(map[materialTableCacheKey][]core.Material),
		caVolumeMap:        make(map[EntityId]*core.VoxelObject),
		objectToEntity:     make(map[*core.VoxelObject]EntityId),
		skyboxLayers:       make(map[EntityId]SkyboxLayerComponent),
	}
	cmd.AddResources(state)
	cmd.AddResources(&WaterInteractionState{})
	cmd.AddResources(&WaterBodyResolutionState{})

	cmd.AddResources(&Profiler{})

	app.UseSystem(
		System(voxelRtDebugSystem).
			InStage(Update).
			RunAlways(),
	)
	// Voxel edit application system (M3) REMOVED - moved to client

	// Cellular automaton step system (low Hz via TickRate in component)
	app.UseSystem(
		System(caStepSystem).
			InStage(Update).
			RunAlways(),
	)

	app.UseSystem(
		System(waterBodyResolutionSystem).
			InStage(Update).
			RunAlways(),
	)

	app.UseSystem(
		System(waterInteractionSystem).
			InStage(Update).
			RunAlways(),
	)

	app.UseSystem(
		System(waterInteractionCleanupSystem).
			InStage(PreRender).
			RunAlways(),
	)

	app.UseSystem(
		System(voxelRtPreludeSystem).
			InStage(Prelude).
			RunAlways(),
	)

	app.UseSystem(
		System(voxelRtSystem).
			InStage(PreRender).
			RunAlways(),
	)

	app.UseSystem(
		System(voxelRtUpdateSystem).
			InStage(PreRender).
			RunAlways(),
	)

	app.UseSystem(
		System(voxelRtRenderSystem).
			InStage(Render).
			RunAlways(),
	)
}

func voxelRtPreludeSystem(input *Input, state *VoxelRtState) {
	if state == nil || state.RtApp == nil {
		return
	}
	state.RtApp.MouseX = input.MouseX
	state.RtApp.MouseY = input.MouseY
	state.RtApp.MouseCaptured = input.MouseCaptured

	state.RtApp.ClearText()

	// Begin batching updates for this frame
	if state.RtApp.BufferManager != nil {
		state.RtApp.BufferManager.BeginBatch()
	}
}

func voxelRtSystem(input *Input, state *VoxelRtState, server *AssetServer, t *Time, cmd *Commands, waterInteractions *WaterInteractionState) {
	if state == nil || state.RtApp == nil {
		return
	}
	state.ensureMaterialCaches()
	// Sync instances
	state.RtApp.Profiler.BeginScope("Sync Instances")
	currentEntities := make(map[EntityId]bool, len(state.instanceMap))
	frameMaterialKeys := make(map[AssetId]materialTableCacheKey)

	// Collect instances from models
	MakeQuery2[TransformComponent, VoxelModelComponent](cmd).Map(func(entityId EntityId, transform *TransformComponent, vox *VoxelModelComponent) bool {
		if server == nil {
			return true
		}
		currentEntities[entityId] = true
		vox.NormalizeGeometryRefs()

		geometryID, geometryAsset, ok := ResolveVoxelGeometry(server, vox)
		if !ok || geometryAsset == nil || geometryAsset.XBrickMap == nil {
			return true
		}

		materialKey, hasKey := frameMaterialKeys[vox.VoxelPalette]
		if !hasKey {
			gekkoPalette := server.voxPalettes[vox.VoxelPalette]
			materialKey = state.materialTableKey(vox.VoxelPalette, &gekkoPalette)
			frameMaterialKeys[vox.VoxelPalette] = materialKey
		}

		obj, exists := state.instanceMap[entityId]
		if !exists {
			modelTemplate, hasTemplate := state.loadedModels[geometryID]
			if !hasTemplate {
				modelTemplate = core.NewVoxelObject()
				modelTemplate.XBrickMap = geometryAsset.XBrickMap
				state.loadedModels[geometryID] = modelTemplate
			}

			obj = core.NewVoxelObject()
			obj.XBrickMap = modelTemplate.XBrickMap
			gekkoPalette := server.voxPalettes[vox.VoxelPalette]
			obj.MaterialTable = state.buildMaterialTable(materialKey, &gekkoPalette)
			state.RtApp.Scene.AddObject(obj)
			state.instanceMap[entityId] = obj
			state.objectToEntity[obj] = entityId
			state.lastMaterialKeys[obj] = materialKey
		}

		if geometryAsset.XBrickMap != obj.XBrickMap {
			obj.XBrickMap = geometryAsset.XBrickMap
			obj.XBrickMap.StructureDirty = true
			state.RtApp.Scene.StructureRevision++ // Force hash grid rebuild
		}
		scale := EffectiveVoxelScale(vox, transform)

		// Compute and apply Pivot
		pivot := mgl32.Vec3{0, 0, 0}
		switch vox.PivotMode {
		case PivotModeCenter:
			if obj.XBrickMap != nil {
				minB, maxB := obj.XBrickMap.ComputeAABB()
				pivot = minB.Add(maxB).Mul(0.5)
			}
		case PivotModeCustom:
			pivot = vox.CustomPivot
		case PivotModeCorner:
			fallthrough
		default:
			pivot = mgl32.Vec3{0, 0, 0}
		}
		transform.Pivot = pivot

		transformChanged := false
		if obj.Transform.Position != transform.Position {
			obj.Transform.Position = transform.Position
			transformChanged = true
		}
		if obj.Transform.Rotation != transform.Rotation {
			obj.Transform.Rotation = transform.Rotation
			transformChanged = true
		}
		if obj.Transform.Scale != scale {
			obj.Transform.Scale = scale
			transformChanged = true
		}
		if obj.Transform.Pivot != pivot {
			obj.Transform.Pivot = pivot
			transformChanged = true
		}
		if transformChanged {
			obj.Transform.Dirty = true
		}

		if lastKey, ok := state.lastMaterialKeys[obj]; !ok || lastKey != materialKey {
			gekkoPalette := server.voxPalettes[vox.VoxelPalette]
			obj.MaterialTable = state.buildMaterialTable(materialKey, &gekkoPalette)
			state.lastMaterialKeys[obj] = materialKey
		}

		obj.CastsShadows = !vox.DisableShadows
		obj.ShadowMaxDistance = vox.ShadowMaxDistance
		obj.ShadowCasterGroupID = vox.ShadowCasterGroupID
		obj.ShadowCasterGroupLimit = vox.ShadowCasterGroupLimit
		obj.ShadowGroupID = vox.ShadowGroupID
		obj.EmitterLinkID = vox.EmitterLinkID
		obj.AmbientOcclusionMode = core.AmbientOcclusionMode(vox.AmbientOcclusionMode)
		obj.ShadowSeamWorldEpsilon = vox.ShadowSeamWorldEpsilon
		obj.AllowOcclusionCulling = voxelObjectAllowsOcclusion(cmd, entityId, vox)
		obj.IsTerrainChunk = vox.IsTerrainChunk
		obj.TerrainGroupID = vox.TerrainGroupID
		obj.TerrainChunkCoord = vox.TerrainChunkCoord
		obj.TerrainChunkSize = vox.TerrainChunkSize
		obj.IsPlanetTile = vox.IsPlanetTile
		obj.PlanetTileGroupID = vox.PlanetTileGroupID
		obj.PlanetTileFace = vox.PlanetTileFace
		obj.PlanetTileLevel = vox.PlanetTileLevel
		obj.PlanetTileX = vox.PlanetTileX
		obj.PlanetTileY = vox.PlanetTileY

		return true
	})

	for eid, obj := range state.instanceMap {
		if !currentEntities[eid] {
			state.RtApp.Scene.RemoveObject(obj)
			delete(state.instanceMap, eid)
			delete(state.lastMaterialKeys, obj)
			delete(state.objectToEntity, obj)
		}
	}
	state.RtApp.Profiler.EndScope("Sync Instances")

	// Init CA presets
	if state.RtApp.BufferManager != nil {
		state.RtApp.BufferManager.UpdateCAPresets()
	}

	// CA volumetrics: smoke/fire are simulated on GPU and rendered as raymarched volumes.
	state.RtApp.Profiler.BeginScope("Sync CA")
	currentCA := make(map[EntityId]bool)
	gpuVolumes := make([]gpu_rt.CAVolumeHost, 0, 8)
	MakeQuery2[TransformComponent, CellularVolumeComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, cv *CellularVolumeComponent) bool {
		if cv == nil || !cv.UsesGPUVolume() {
			return true
		}
		currentCA[eid] = true

		if obj, exists := state.caVolumeMap[eid]; exists {
			state.RtApp.Scene.RemoveObject(obj)
			delete(state.caVolumeMap, eid)
			delete(state.objectToEntity, obj)
		}

		scatterColor := [3]float32{0.72, 0.72, 0.72}
		shadowTint := [3]float32{0.45, 0.45, 0.46}
		absorptionColor := [3]float32{0.28, 0.29, 0.31}
		extinction := float32(1.35)
		emission := float32(0.0)
		if cv.Type == CellularFire {
			scatterColor = [3]float32{1.0, 0.48, 0.1}
			shadowTint = [3]float32{0.62, 0.18, 0.04}
			absorptionColor = [3]float32{0.54, 0.12, 0.03}
			extinction = 0.5
			emission = 5.5
		}
		switch cv.Preset {
		case CAVolumePresetTorch:
			scatterColor = [3]float32{0.78, 0.38, 0.12}
			shadowTint = [3]float32{0.42, 0.12, 0.04}
			absorptionColor = [3]float32{0.34, 0.08, 0.02}
			extinction = 0.3
			emission = 10.5
		case CAVolumePresetCampfire:
			if cv.Type == CellularFire {
				scatterColor = [3]float32{1.0, 0.42, 0.1}
				shadowTint = [3]float32{0.54, 0.14, 0.04}
				absorptionColor = [3]float32{0.42, 0.1, 0.03}
				extinction = 0.42
				emission = 7.2
			} else {
				scatterColor = [3]float32{0.34, 0.35, 0.38}
				shadowTint = [3]float32{0.2, 0.18, 0.16}
				absorptionColor = [3]float32{0.14, 0.11, 0.09}
				extinction = 0.72
			}
		case CAVolumePresetJetFlame:
			scatterColor = [3]float32{0.16, 0.22, 0.34}
			shadowTint = [3]float32{0.08, 0.12, 0.2}
			absorptionColor = [3]float32{0.05, 0.08, 0.16}
			extinction = 0.12
			emission = 10.8
		case CAVolumePresetExplosion:
			scatterColor = [3]float32{0.58, 0.52, 0.46}
			shadowTint = [3]float32{0.24, 0.18, 0.14}
			absorptionColor = [3]float32{0.1, 0.08, 0.06}
			extinction = 0.82
			emission = 24.0
		}
		if cv.UseAppearanceOverride {
			scatterColor = cv.ScatterColor
			extinction = cv.Extinction
			emission = cv.Emission
		}
		if cv.UseShadowTintOverride {
			shadowTint = cv.ShadowTint
		}
		if cv.UseAbsorptionOverride {
			absorptionColor = cv.AbsorptionColor
		}

		gpuVolumes = append(gpuVolumes, gpu_rt.CAVolumeHost{
			EntityID: uint32(eid),
			Type:     uint32(cv.Type),
			Preset:   uint32(cv.Preset),
			Resolution: [3]uint32{
				uint32(max(1, cv.Resolution[0])),
				uint32(max(1, cv.Resolution[1])),
				uint32(max(1, cv.Resolution[2])),
			},
			Position:        cv.VolumeOrigin(tr),
			Rotation:        tr.Rotation,
			VoxelScale:      mgl32.Vec3{VoxelSize * tr.Scale.X(), VoxelSize * tr.Scale.Y(), VoxelSize * tr.Scale.Z()},
			Intensity:       cv.CurrentIntensity(),
			Diffusion:       cv.Diffusion,
			Buoyancy:        cv.Buoyancy,
			Cooling:         cv.Cooling,
			Dissipation:     cv.Dissipation,
			Extinction:      extinction,
			Emission:        emission,
			StepsPending:    float32(cv._gpuStepsPending),
			StepDt:          1.0 / max(cv.TickRate, 1.0),
			ScatterColor:    scatterColor,
			ShadowTint:      shadowTint,
			AbsorptionColor: absorptionColor,
		})
		cv._gpuStepsPending = 0
		cv._dirty = false

		return true
	})
	for eid, obj := range state.caVolumeMap {
		if !currentCA[eid] {
			state.RtApp.Scene.RemoveObject(obj)
			delete(state.caVolumeMap, eid)
			delete(state.objectToEntity, obj)
		}
	}
	sort.Slice(gpuVolumes, func(i, j int) bool {
		return gpuVolumes[i].EntityID < gpuVolumes[j].EntityID
	})
	if state.RtApp.BufferManager != nil {
		state.RtApp.BufferManager.UpdateCAVolumes(gpuVolumes)
		state.RtApp.BufferManager.UpdateCAParams(float32(t.Dt))
	}
	state.RtApp.Profiler.EndScope("Sync CA")

	state.RtApp.Profiler.BeginScope("Sync Media")
	gpuMedia := make([]gpu_rt.AnalyticMediumHost, 0, 4)
	MakeQuery2[TransformComponent, AnalyticMediumComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, medium *AnalyticMediumComponent) bool {
		if medium == nil || tr == nil || !medium.Enabled() {
			return true
		}

		gpuMedia = append(gpuMedia, gpu_rt.AnalyticMediumHost{
			EntityID:                  uint32(eid),
			Shape:                     uint32(medium.NormalizedShape()),
			Position:                  medium.WorldCenter(tr),
			Rotation:                  medium.WorldRotation(tr),
			OuterRadius:               medium.NormalizedOuterRadius(),
			InnerRadius:               medium.NormalizedInnerRadius(),
			BoxExtents:                medium.NormalizedBoxExtents(),
			Density:                   medium.Density,
			Falloff:                   medium.NormalizedFalloff(),
			EdgeSoftness:              medium.NormalizedEdgeSoftness(),
			PhaseG:                    medium.NormalizedPhaseG(),
			LightStrength:             medium.NormalizedLightStrength(),
			AmbientStrength:           medium.NormalizedAmbientStrength(),
			LimbStrength:              medium.NormalizedLimbStrength(),
			LimbExponent:              medium.NormalizedLimbExponent(),
			DiskHazeStrength:          medium.NormalizedDiskHazeStrength(),
			DiskHazeTintMix:           medium.NormalizedDiskHazeTintMix(),
			OpaqueExtinctionScale:     medium.NormalizedOpaqueExtinctionScale(),
			BackgroundExtinctionScale: medium.NormalizedBackgroundExtinctionScale(),
			BoundaryFadeStart:         medium.NormalizedBoundaryFadeStart(),
			BoundaryFadeEnd:           medium.NormalizedBoundaryFadeEnd(),
			OpaqueAlphaScale:          medium.NormalizedOpaqueAlphaScale(),
			BackgroundAlphaScale:      medium.NormalizedBackgroundAlphaScale(),
			OpaqueRevealScale:         medium.NormalizedOpaqueRevealScale(),
			BackgroundRevealScale:     medium.NormalizedBackgroundRevealScale(),
			Color:                     medium.NormalizedColor(),
			AbsorptionColor:           medium.NormalizedAbsorptionColor(),
			EmissionColor:             medium.NormalizedEmissionColor(),
			NoiseScale:                medium.NormalizedNoiseScale(),
			NoiseStrength:             medium.NormalizedNoiseStrength(),
			SampleCount:               uint32(medium.NormalizedSampleCount()),
			CloudBlockSize:            medium.CloudBlockSize,
			CloudThreshold:            medium.CloudThreshold,
			CloudTime:                 float32(t.Elapsed) * medium.CloudSpeed,
			CloudAltitudeSteps:        medium.CloudAltitudeSteps,
		})
		return true
	})
	sort.Slice(gpuMedia, func(i, j int) bool {
		return gpuMedia[i].EntityID < gpuMedia[j].EntityID
	})
	if state.RtApp.BufferManager != nil {
		state.RtApp.BufferManager.UpdateAnalyticMedia(gpuMedia)
	}
	state.RtApp.Profiler.EndScope("Sync Media")

	state.RtApp.Profiler.BeginScope("Sync Planet Bodies")
	if state.RtApp.BufferManager != nil {
		state.RtApp.BufferManager.UpdatePlanetBodies(buildPlanetBodyHosts(cmd))
	}
	state.RtApp.Profiler.EndScope("Sync Planet Bodies")

	state.RtApp.Profiler.BeginScope("Sync Water")
	if state.RtApp.BufferManager != nil {
		waterHosts, rippleHosts := buildWaterSurfaceHosts(cmd, waterInteractions)
		state.RtApp.BufferManager.UpdateWaterSurfaces(waterHosts, rippleHosts, float32(t.Dt))
	}
	state.RtApp.Profiler.EndScope("Sync Water")

	state.RtApp.Profiler.BeginScope("Sync Lights")
	MakeQuery1[CameraComponent](cmd).Map(func(entityId EntityId, camera *CameraComponent) bool {
		state.RtApp.Camera.Position = camera.Position
		state.RtApp.Camera.LookAt = camera.LookAt
		state.RtApp.Camera.Up = camera.Up
		state.RtApp.Camera.Yaw = mgl32.DegToRad(camera.Yaw)
		state.RtApp.Camera.Pitch = mgl32.DegToRad(camera.Pitch)
		state.RtApp.Camera.Fov = camera.Fov
		state.RtApp.Camera.Near = camera.Near
		state.RtApp.Camera.Far = camera.Far
		return false
	})
	// Sync text
	MakeQuery1[TextComponent](cmd).Map(func(entityId EntityId, text *TextComponent) bool {
		state.RtApp.DrawText(text.Text, text.Position[0], text.Position[1], text.Scale, text.Color)
		return true
	})

	syncVoxelRtLights(state, cmd)

	state.RtApp.Profiler.BeginScope("Sync Gizmos")
	syncVoxelRtGizmos(state, cmd)
	state.RtApp.Profiler.EndScope("Sync Gizmos")

	state.RtApp.Profiler.BeginScope("GPU Batch")
	// End batching and process all accumulated updates
	if state.RtApp.BufferManager != nil {
		state.RtApp.BufferManager.EndBatch()
	}
	state.RtApp.Profiler.EndScope("GPU Batch")

	state.RtApp.Profiler.BeginScope("Sync Particles")
	// Sync GPU emitters and spawn requests
	spawnReqs, emitters, emitterCount, atlasId := particlesSync(state, t, cmd)

	// Update Particle Atlas if provided by user code and changed
	if atlasId != (AssetId{}) && atlasId != state.lastParticleAtlas {
		if texAsset, ok := server.textures[atlasId]; ok {
			state.RtApp.SetParticleAtlas(texAsset.Texels, texAsset.Width, texAsset.Height)
			state.lastParticleAtlas = atlasId
		}
	}

	vSize := VoxelSize
	invVsize := 1.0 / vSize
	state.RtApp.ParticleSpawnCount = uint32(len(spawnReqs))
	if state.RtApp.BufferManager != nil {
		state.RtApp.BufferManager.UpdateParticleParams(float32(t.Dt), float32(invVsize), uint32(time.Now().UnixNano()), emitterCount)
		pRecreated := state.RtApp.BufferManager.UpdateParticles(1000000, emitters) // Pass max count
		state.RtApp.BufferManager.UpdateSpawnRequests(spawnReqs)
		if pRecreated || state.RtApp.BufferManager.ParticlesBindGroup0 == nil || state.RtApp.BufferManager.ParticleSimBG0 == nil {
			state.RtApp.BufferManager.CreateParticleSimBindGroups()
			state.RtApp.BufferManager.CreateParticlesBindGroups(state.RtApp.ParticlesPipeline)
		}
	}

	state.RtApp.Profiler.BeginScope("Sync Sprites")
	// Sync GPU sprites
	spriteBytes, spriteCount, spriteBatches := spritesSync(state, cmd)
	seenSpriteAtlases := make(map[string]struct{}, len(spriteBatches))
	for _, batch := range spriteBatches {
		if batch.AtlasKey == "" {
			continue
		}
		if _, seen := seenSpriteAtlases[batch.AtlasKey]; seen {
			continue
		}
		seenSpriteAtlases[batch.AtlasKey] = struct{}{}
		if texAsset, ok := spriteAtlasTexture(server, batch.AtlasKey); ok && state.RtApp.BufferManager != nil {
			state.RtApp.BufferManager.SetSpriteAtlas(batch.AtlasKey, texAsset.Texels, texAsset.Width, texAsset.Height, texAsset.Version)
		}
	}
	if state.RtApp.BufferManager != nil {
		state.RtApp.BufferManager.UpdateSprites(spriteBytes, spriteCount)
	}
	gpuSpriteBatches := make([]gpu_rt.SpriteBatchDesc, 0, len(spriteBatches))
	for _, batch := range spriteBatches {
		gpuSpriteBatches = append(gpuSpriteBatches, gpu_rt.SpriteBatchDesc{
			AtlasKey:      batch.AtlasKey,
			FirstInstance: batch.FirstInstance,
			InstanceCount: batch.InstanceCount,
		})
	}
	if state.RtApp.BufferManager != nil {
		state.RtApp.BufferManager.SyncSpriteBatches(state.RtApp.SpritesPipeline, gpuSpriteBatches)
	}
	state.RtApp.Profiler.EndScope("Sync Sprites")
}

func voxelObjectAllowsOcclusion(cmd *Commands, entityId EntityId, vox *VoxelModelComponent) bool {
	return true
}

func buildPlanetBodyHosts(cmd *Commands) []gpu_rt.PlanetBodyHost {
	hosts := make([]gpu_rt.PlanetBodyHost, 0, 4)
	if cmd == nil {
		return hosts
	}
	MakeQuery2[TransformComponent, PlanetBodyComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, planet *PlanetBodyComponent) bool {
		if planet == nil || tr == nil || !planet.Enabled() {
			return true
		}
		bakedSurfaceSamples := make([]gpu_rt.PlanetBakedSurfaceSampleHost, len(planet.BakedSurfaceSamples))
		for i, sample := range planet.BakedSurfaceSamples {
			bakedSurfaceSamples[i] = gpu_rt.PlanetBakedSurfaceSampleHost{
				Height:       sample.Height,
				NormalOctX:   sample.NormalOctX,
				NormalOctY:   sample.NormalOctY,
				MaterialBand: sample.MaterialBand,
			}
		}
		hosts = append(hosts, gpu_rt.PlanetBodyHost{
			EntityID:               uint32(eid),
			Seed:                   planet.Seed,
			Position:               planet.WorldCenter(tr),
			Rotation:               planet.WorldRotation(tr),
			Radius:                 planet.WorldRadius(tr),
			OceanRadius:            planet.WorldOceanRadius(tr),
			AtmosphereRadius:       planet.WorldAtmosphereRadius(tr),
			AtmosphereRimWidth:     planet.WorldAtmosphereRimWidth(tr),
			HeightAmplitude:        planet.WorldHeightAmplitude(tr),
			NoiseScale:             planet.NormalizedNoiseScale(),
			BlockSize:              planet.WorldBlockSize(tr),
			HeightSteps:            uint32(planet.NormalizedHeightSteps()),
			HandoffNearAlt:         planet.WorldHandoffNearAlt(tr),
			HandoffFarAlt:          planet.WorldHandoffFarAlt(tr),
			BiomeMix:               planet.NormalizedBiomeMix(),
			BakedSurfaceResolution: uint32(planet.NormalizedBakedSurfaceResolution()),
			BakedSurfaceSamples:    bakedSurfaceSamples,
			BandColors:             planet.NormalizedBandColors(),
			AmbientStrength:        planet.NormalizedAmbientStrength(),
			DiffuseStrength:        planet.NormalizedDiffuseStrength(),
			SpecularStrength:       planet.NormalizedSpecularStrength(),
			RimStrength:            planet.NormalizedRimStrength(),
			TerrainLowColor:        planet.NormalizedTerrainLowColor(),
			TerrainHighColor:       planet.NormalizedTerrainHighColor(),
			RockColor:              planet.NormalizedRockColor(),
			OceanDeepColor:         planet.NormalizedOceanDeepColor(),
			OceanShallowColor:      planet.NormalizedOceanShallowColor(),
			AtmosphereColor:        planet.NormalizedAtmosphereTintColor(),
		})
		return true
	})
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].EntityID < hosts[j].EntityID
	})
	return hosts
}

func spriteAtlasTexture(server *AssetServer, atlasKey string) (TextureAsset, bool) {
	if server == nil || atlasKey == "" {
		return TextureAsset{}, false
	}
	parsed, err := uuid.Parse(atlasKey)
	if err != nil {
		return TextureAsset{}, false
	}
	texAsset, ok := server.textures[AssetId{UUID: parsed}]
	return texAsset, ok
}

func syncVoxelRtLights(state *VoxelRtState, cmd *Commands) {
	if state == nil || state.RtApp == nil || state.RtApp.Scene == nil || cmd == nil {
		return
	}

	state.RtApp.Profiler.BeginScope("Sync Lights")
	defer state.RtApp.Profiler.EndScope("Sync Lights")

	state.RtApp.Scene.Lights = state.RtApp.Scene.Lights[:0]
	defaultAmbient := mgl32.Vec3{0.2, 0.2, 0.2}
	defaultSkyAmbientMix := float32(0.60)
	ambientAccum := mgl32.Vec3{0, 0, 0}
	ambientFound := false
	skyAmbientMix := defaultSkyAmbientMix
	state.SunDirection = mgl32.Vec3{}
	state.SunIntensity = 0

	MakeQuery1[SkyAmbientComponent](cmd).Map(func(_ EntityId, ambient *SkyAmbientComponent) bool {
		if ambient != nil {
			skyAmbientMix = ambient.SkyMix
		}
		return false
	})

	type pendingLight struct {
		entityID  EntityId
		lightType LightType
		intensity float32
		gpu       core.Light
	}
	pendingLights := make([]pendingLight, 0, 8)

	MakeQuery1[LightComponent](cmd).Map(func(_ EntityId, light *LightComponent) bool {
		if light.Type != LightTypeAmbient {
			return true
		}
		ambientAccum = ambientAccum.Add(mgl32.Vec3(light.Color).Mul(light.Intensity))
		ambientFound = true
		return true
	})

	MakeQuery2[LightComponent, TransformComponent](cmd).Map(func(entityId EntityId, light *LightComponent, tr *TransformComponent) bool {
		if light.Type == LightTypeAmbient {
			return true
		}

		pos := tr.Position
		rot := tr.Rotation

		gpuLight := core.Light{}
		sourceRadius := light.SourceRadius
		if sourceRadius < 0 {
			sourceRadius = 0
		}
		if sourceRadius == 0 && light.EmitterLinkID != 0 {
			sourceRadius = derivedEmitterSourceRadius(state, light.EmitterLinkID)
		}
		gpuLight.Position = [4]float32{pos.X(), pos.Y(), pos.Z(), sourceRadius}

		baseForward := mgl32.Vec3{0, 0, -1}
		if light.Type == LightTypeDirectional {
			baseForward = mgl32.Vec3{1, -1, 0}.Normalize()
		} else if light.Type == LightTypeSpot {
			baseForward = mgl32.Vec3{0, -1, 0}
		}

		dir := rot.Rotate(baseForward)
		gpuLight.Direction = [4]float32{dir.X(), dir.Y(), dir.Z(), 0.0}
		if light.Type == LightTypeDirectional {
			state.SunDirection = dir
			state.SunIntensity = light.Intensity
		}
		gpuLight.Color = [4]float32{light.Color[0], light.Color[1], light.Color[2], light.Intensity}

		cosAngle := float32(0.0)
		if light.Type == LightTypeSpot {
			cosAngle = float32(math.Cos(float64(light.ConeAngle) * math.Pi / 180.0 / 2.0))
		}

		var castsShadows float32
		if light.CastsShadows {
			castsShadows = 1.0
		}
		gpuLight.Params = [4]float32{light.Range, cosAngle, float32(light.Type), castsShadows}
		gpuLight.ShadowMeta[3] = light.EmitterLinkID
		pendingLights = append(pendingLights, pendingLight{
			entityID:  entityId,
			lightType: light.Type,
			intensity: light.Intensity,
			gpu:       gpuLight,
		})
		return true
	})

	sort.Slice(pendingLights, func(i, j int) bool {
		li := pendingLights[i]
		lj := pendingLights[j]

		ranki := 2
		rankj := 2
		if li.lightType == LightTypeDirectional {
			ranki = 0
		} else if li.lightType == LightTypeSpot {
			ranki = 1
		}
		if lj.lightType == LightTypeDirectional {
			rankj = 0
		} else if lj.lightType == LightTypeSpot {
			rankj = 1
		}

		if ranki != rankj {
			return ranki < rankj
		}
		if li.lightType == lj.lightType && li.intensity != lj.intensity {
			return li.intensity > lj.intensity
		}
		return li.entityID < lj.entityID
	})

	for _, pl := range pendingLights {
		state.RtApp.Scene.Lights = append(state.RtApp.Scene.Lights, pl.gpu)
	}

	if ambientFound {
		state.RtApp.Scene.AmbientLight = ambientAccum
	} else {
		state.RtApp.Scene.AmbientLight = defaultAmbient
	}
	state.RtApp.Scene.SkyAmbientMix = skyAmbientMix
}

func derivedEmitterSourceRadius(state *VoxelRtState, emitterLinkID uint32) float32 {
	if state == nil || emitterLinkID == 0 {
		return 0
	}

	var radius float32
	for _, obj := range state.instanceMap {
		if obj == nil || obj.EmitterLinkID != emitterLinkID || obj.XBrickMap == nil {
			continue
		}
		obj.UpdateWorldAABB()
		if obj.WorldAABB == nil {
			continue
		}
		extent := obj.WorldAABB[1].Sub(obj.WorldAABB[0])
		candidate := extent.Len() * 0.5
		if candidate > radius {
			radius = candidate
		}
	}
	return radius
}

func voxelRtUpdateSystem(state *VoxelRtState, prof *Profiler, time *Time, cmd *Commands) {
	if state == nil || state.RtApp == nil {
		return
	}

	state.RtApp.Profiler.BeginScope("RT Update")
	state.RtApp.Update()
	state.RtApp.Profiler.EndScope("RT Update")

	// Skybox Sync & Generation
	state.syncSkybox(cmd, time)
}

func voxelRtRenderSystem(cmd *Commands, state *VoxelRtState, prof *Profiler) {
	if prof != nil {
		start := time.Now()
		defer func() { prof.RenderTime += time.Since(start) }()
	}
	if state == nil || state.RtApp == nil {
		return
	}
	state.RtApp.Render()
}

func voxelRtDebugSystem(input *Input, state *VoxelRtState) {
	if input.JustPressed[KeyF2] {
		state.CycleDebugOverlayMode()
	}
}

func syncVoxelRtGizmos(state *VoxelRtState, cmd *Commands) {
	if state == nil || state.RtApp == nil || state.RtApp.Scene == nil {
		return
	}

	state.RtApp.Scene.Gizmos = state.RtApp.Scene.Gizmos[:0]
	if state.DebugOverlayMode() == VoxelRtDebugModeScene {
		// Automatic light gizmos (engine helpers shown in Scene Debug mode)
		MakeQuery2[LightComponent, TransformComponent](cmd).Map(func(eid EntityId, l *LightComponent, tr *TransformComponent) bool {
			if l.Type == LightTypeAmbient {
				return true
			}
			color := [4]float32{l.Color[0], l.Color[1], l.Color[2], 0.8}
			rtGizmo := core.Gizmo{
				Type:  core.GizmoSphere,
				Color: color,
			}
			modelMat := mgl32.Translate3D(tr.Position.X(), tr.Position.Y(), tr.Position.Z()).Mul4(mgl32.Scale3D(1.0, 1.0, 1.0))
			rtGizmo.ModelMatrix = modelMat
			state.RtApp.Scene.Gizmos = append(state.RtApp.Scene.Gizmos, rtGizmo)
			return true
		})
	}

	// Always sync user-defined GizmoComponents
	MakeQuery2[GizmoComponent, TransformComponent](cmd).Map(func(eid EntityId, g *GizmoComponent, tr *TransformComponent) bool {
		rtGizmo := core.Gizmo{
			Type:  core.GizmoType(g.Type),
			Color: g.Color,
		}

		if g.Type == GizmoGrid {
			steps := g.Steps
			if steps <= 0 {
				steps = 10
			}
			stepSize := g.Size / float32(steps)
			halfSize := g.Size * 0.5

			for i := 0; i <= steps; i++ {
				offset := float32(i)*stepSize - halfSize

				lx := mgl32.Translate3D(offset, 0, -halfSize)
				sz := mgl32.Scale3D(1, 1, g.Size)
				rtLineZ := core.Gizmo{Type: core.GizmoLine, Color: g.Color}
				rtLineZ.ModelMatrix = tr.ObjectToWorld().Mul4(lx).Mul4(sz)
				state.RtApp.Scene.Gizmos = append(state.RtApp.Scene.Gizmos, rtLineZ)

				lz := mgl32.Translate3D(-halfSize, 0, offset)
				rx := mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0}).Mat4()
				rtLineX := core.Gizmo{Type: core.GizmoLine, Color: g.Color}
				rtLineX.ModelMatrix = tr.ObjectToWorld().Mul4(lz).Mul4(rx).Mul4(sz)
				state.RtApp.Scene.Gizmos = append(state.RtApp.Scene.Gizmos, rtLineX)
			}
			return true
		}

		modelMat := tr.ObjectToWorld()
		if g.Type == GizmoLine {
			modelMat = modelMat.Mul4(mgl32.Scale3D(1, 1, g.Size))
		} else if g.Size > 0 {
			modelMat = modelMat.Mul4(mgl32.Scale3D(g.Size, g.Size, g.Size))
		}

		rtGizmo.ModelMatrix = modelMat
		state.RtApp.Scene.Gizmos = append(state.RtApp.Scene.Gizmos, rtGizmo)
		return true
	})
}
