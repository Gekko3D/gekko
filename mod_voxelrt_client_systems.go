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
	RtApp.Camera.DepthMode = core.DepthMode(mod.DepthMode).Normalized()
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
		RtApp:               RtApp,
		loadedModels:        make(map[AssetId]*core.VoxelObject),
		instanceMap:         make(map[EntityId]*core.VoxelObject),
		entityLODSelections: make(map[EntityId]EntityLODSelection),
		lastMaterialKeys:    make(map[*core.VoxelObject]materialTableCacheKey),
		materialTableCache:  make(map[materialTableCacheKey][]core.Material),
		caVolumeMap:         make(map[EntityId]*core.VoxelObject),
		objectToEntity:      make(map[*core.VoxelObject]EntityId),
		skyboxLayers:        make(map[EntityId]SkyboxLayerComponent),
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
		System(entityLODSelectionSystem).
			InStage(PostUpdate).
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

type caBudgetCameraView struct {
	Position mgl32.Vec3
	Forward  mgl32.Vec3
	Valid    bool
}

type caVolumeBudgetCandidate struct {
	host              gpu_rt.CAVolumeHost
	volume            *CellularVolumeComponent
	rawSteps          uint32
	scheduledSteps    uint32
	distance          float32
	visible           bool
	behindCamera      bool
	resolutionClamped bool
	stepDeferred      bool
	suspended         bool
	dropped           bool
	priority          float32
}

func readEntityLODCameraPosition(cmd *Commands, fallback *core.CameraState) (mgl32.Vec3, bool) {
	if cmd != nil {
		found := false
		position := mgl32.Vec3{}
		MakeQuery1[CameraComponent](cmd).Map(func(entityId EntityId, camera *CameraComponent) bool {
			if camera == nil {
				return true
			}
			position = camera.Position
			found = true
			return false
		})
		if found {
			return position, true
		}
	}
	if fallback != nil {
		return fallback.Position, true
	}
	return mgl32.Vec3{}, false
}

func entityLODSelectionSystem(cmd *Commands, state *VoxelRtState) {
	cameraState := (*core.CameraState)(nil)
	if state != nil && state.RtApp != nil {
		cameraState = state.RtApp.Camera
	}
	cameraPosition, ok := readEntityLODCameraPosition(cmd, cameraState)
	MakeQuery2[TransformComponent, EntityLODComponent](cmd).Map(func(entityId EntityId, transform *TransformComponent, lod *EntityLODComponent) bool {
		if lod == nil || !lod.Enabled() || !ok {
			if lod != nil {
				lod.ClearRuntimeSelection()
			}
			return true
		}
		selection, err := SelectEntityLOD(cameraPosition, transform, lod)
		if err != nil {
			lod.ClearRuntimeSelection()
			return true
		}
		lod.ApplySelection(selection)
		return true
	})
}

func readCAVolumeBudgetCamera(cmd *Commands, fallback *core.CameraState) caBudgetCameraView {
	view := caBudgetCameraView{}
	if cmd != nil {
		MakeQuery1[CameraComponent](cmd).Map(func(entityId EntityId, camera *CameraComponent) bool {
			if camera == nil {
				return true
			}
			view.Position = camera.Position
			forward := camera.LookAt.Sub(camera.Position)
			if forward.LenSqr() <= 1e-6 {
				yaw := mgl32.DegToRad(camera.Yaw)
				pitch := mgl32.DegToRad(camera.Pitch)
				forward = mgl32.Vec3{
					float32(math.Sin(float64(yaw)) * math.Cos(float64(pitch))),
					float32(math.Sin(float64(pitch))),
					float32(-math.Cos(float64(yaw)) * math.Cos(float64(pitch))),
				}
			}
			if forward.LenSqr() > 1e-6 {
				view.Forward = forward.Normalize()
				view.Valid = true
				return false
			}
			return true
		})
	}
	if !view.Valid && fallback != nil {
		view.Position = fallback.Position
		forward := fallback.GetForward()
		if forward.LenSqr() > 1e-6 {
			view.Forward = forward.Normalize()
			view.Valid = true
		}
	}
	return view
}

func caVolumeCellCount(resolution [3]uint32) uint64 {
	return uint64(resolution[0]) * uint64(resolution[1]) * uint64(resolution[2])
}

func clampCAVolumeResolution(resolution [3]uint32, cfg gpu_rt.CAVolumeBudgetConfig) ([3]uint32, bool) {
	clamped := false
	for axis := range resolution {
		if resolution[axis] == 0 {
			resolution[axis] = 1
			clamped = true
		}
		if resolution[axis] > uint32(cfg.MaxResolutionAxis) {
			resolution[axis] = uint32(cfg.MaxResolutionAxis)
			clamped = true
		}
	}
	if caVolumeCellCount(resolution) <= uint64(cfg.MaxCellsPerVolume) {
		return resolution, clamped
	}

	scale := math.Cbrt(float64(cfg.MaxCellsPerVolume) / float64(caVolumeCellCount(resolution)))
	for axis := range resolution {
		scaled := uint32(math.Floor(float64(resolution[axis]) * scale))
		if scaled < 1 {
			scaled = 1
		}
		if scaled != resolution[axis] {
			clamped = true
			resolution[axis] = scaled
		}
	}
	for caVolumeCellCount(resolution) > uint64(cfg.MaxCellsPerVolume) {
		largestAxis := 0
		for axis := 1; axis < len(resolution); axis++ {
			if resolution[axis] > resolution[largestAxis] {
				largestAxis = axis
			}
		}
		if resolution[largestAxis] <= 1 {
			break
		}
		resolution[largestAxis]--
		clamped = true
	}
	return resolution, clamped
}

func scheduleCAVolumeSteps(raw uint32, distance float32, behindCamera bool, cfg gpu_rt.CAVolumeBudgetConfig) (scheduled uint32, deferred bool, suspended bool) {
	scheduled = min(raw, uint32(cfg.MaxStepsPerVolume))
	deferred = scheduled < raw
	if scheduled == 0 {
		return 0, deferred, false
	}

	if behindCamera && distance >= cfg.StepSuspendDistance {
		return 0, true, true
	}
	if distance >= cfg.StepSuspendDistance {
		scheduled = min(scheduled, 1)
	}
	if distance >= cfg.StepReduceDistance {
		scheduled = min(scheduled, 1)
	}
	if behindCamera {
		scheduled = min(scheduled, 1)
	}
	if scheduled < raw {
		deferred = true
	}
	suspended = raw > 0 && scheduled == 0
	return scheduled, deferred, suspended
}

func caVolumeBudgetPriority(candidate caVolumeBudgetCandidate) float32 {
	priority := candidate.host.Intensity * 100.0
	if candidate.visible {
		priority += 1000.0
	}
	if !candidate.behindCamera {
		priority += 500.0
	}
	priority += max(0.0, 256.0-candidate.distance)
	priority += float32(candidate.rawSteps) * 16.0
	return priority
}

func budgetCAVolumes(candidates []caVolumeBudgetCandidate, cfg gpu_rt.CAVolumeBudgetConfig) ([]gpu_rt.CAVolumeHost, uint32, uint32, uint32, uint32) {
	cfg = cfg.WithDefaults()
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority == candidates[j].priority {
			return candidates[i].host.EntityID < candidates[j].host.EntityID
		}
		return candidates[i].priority > candidates[j].priority
	})

	selected := make([]*caVolumeBudgetCandidate, 0, min(len(candidates), cfg.MaxManagedVolumes))
	currentMaxX := uint32(1)
	currentMaxY := uint32(1)
	currentDepth := uint32(1)
	dropped := uint32(0)

	for i := range candidates {
		candidate := &candidates[i]
		if len(selected) >= cfg.MaxManagedVolumes {
			candidate.dropped = true
			dropped++
			continue
		}
		nextMaxX := max(currentMaxX, candidate.host.Resolution[0])
		nextMaxY := max(currentMaxY, candidate.host.Resolution[1])
		nextDepth := currentDepth + candidate.host.Resolution[2]
		nextCells := uint64(nextMaxX) * uint64(nextMaxY) * uint64(nextDepth)
		if nextCells > uint64(cfg.MaxAtlasCells) {
			candidate.dropped = true
			dropped++
			continue
		}
		selected = append(selected, candidate)
		currentMaxX = nextMaxX
		currentMaxY = nextMaxY
		currentDepth = nextDepth
	}

	remainingSteps := cfg.MaxTotalStepsPerFrame
	deferredCount := uint32(0)
	suspendedCount := uint32(0)
	totalSteps := uint32(0)
	for _, candidate := range selected {
		if candidate.scheduledSteps == 0 {
			candidate.host.StepsPending = 0
			if candidate.stepDeferred {
				deferredCount++
			}
			if candidate.suspended {
				suspendedCount++
			}
			continue
		}
		allowed := min(int(candidate.scheduledSteps), remainingSteps)
		if allowed < int(candidate.scheduledSteps) {
			candidate.stepDeferred = true
			if allowed == 0 && candidate.rawSteps > 0 {
				candidate.suspended = true
			}
		}
		candidate.scheduledSteps = uint32(allowed)
		candidate.host.StepsPending = float32(candidate.scheduledSteps)
		remainingSteps -= allowed
		totalSteps += candidate.scheduledSteps
		if candidate.stepDeferred {
			deferredCount++
		}
		if candidate.suspended {
			suspendedCount++
		}
	}

	for i := range candidates {
		remaining := candidates[i].rawSteps
		if !candidates[i].dropped && candidates[i].scheduledSteps < remaining {
			remaining -= candidates[i].scheduledSteps
		} else if !candidates[i].dropped {
			remaining = 0
		}
		candidates[i].volume._gpuStepsPending = remaining
	}

	hosts := make([]gpu_rt.CAVolumeHost, 0, len(selected))
	for _, candidate := range selected {
		hosts = append(hosts, candidate.host)
	}
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].EntityID < hosts[j].EntityID
	})
	return hosts, dropped, deferredCount, suspendedCount, totalSteps
}

func voxelRtSystem(input *Input, state *VoxelRtState, server *AssetServer, t *Time, cmd *Commands, waterInteractions *WaterInteractionState) {
	if state == nil || state.RtApp == nil {
		return
	}
	state.ensureMaterialCaches()
	state.runtimeSprites = state.runtimeSprites[:0]
	// Sync instances
	state.RtApp.Profiler.BeginScope("Sync Instances")
	currentObjectEntities := make(map[EntityId]bool, len(state.instanceMap))
	currentVoxelEntities := make(map[EntityId]bool, len(state.entityLODSelections))
	frameMaterialKeys := make(map[AssetId]materialTableCacheKey)

	// Collect instances from models
	MakeQuery2[TransformComponent, VoxelModelComponent](cmd).Map(func(entityId EntityId, transform *TransformComponent, vox *VoxelModelComponent) bool {
		if server == nil {
			return true
		}
		currentVoxelEntities[entityId] = true
		if lod, ok := entityLODComponentForEntity(cmd, entityId); ok && lod.SelectionValid {
			state.entityLODSelections[entityId] = EntityLODSelection{
				Distance:       lod.ActiveDistance,
				BandIndex:      lod.ActiveBandIndex,
				MaxDistance:    lod.ActiveMaxDistance,
				Representation: lod.ActiveRepresentation,
			}
		} else {
			delete(state.entityLODSelections, entityId)
		}
		vox.NormalizeGeometryRefs()

		geometryID, geometryAsset, ok := ResolveVoxelGeometry(server, vox)
		if !ok || geometryAsset == nil || geometryAsset.XBrickMap == nil {
			return true
		}

		displayGeometryID := geometryID
		displayGeometryAsset := geometryAsset
		scaleAdjustX, scaleAdjustY, scaleAdjustZ := float32(1), float32(1), float32(1)
		if selection, ok := state.entityLODSelections[entityId]; ok {
			switch selection.Representation {
			case EntityLODRepresentationSimplifiedVoxel:
				simplifiedID, simplifiedAsset, simplifiedOK := server.entityLODSimplifiedGeometry(geometryID, vox.VoxelPalette, geometryAsset)
				if simplifiedOK && simplifiedAsset != nil && simplifiedAsset.XBrickMap != nil {
					displayGeometryID = simplifiedID
					displayGeometryAsset = simplifiedAsset
					scaleAdjustX, scaleAdjustY, scaleAdjustZ = entityLODProxyScaleAdjust(geometryAsset, simplifiedAsset)
				}
			case EntityLODRepresentationImpostor:
				sprite, spriteOK := buildEntityLODImpostorSprite(state, server, transform, vox, geometryID, geometryAsset)
				if !spriteOK {
					sprite, spriteOK = buildEntityLODDotSprite(state, server, transform, vox, geometryAsset)
				}
				if spriteOK {
					state.runtimeSprites = append(state.runtimeSprites, sprite)
					return true
				}
			case EntityLODRepresentationDot:
				sprite, spriteOK := buildEntityLODDotSprite(state, server, transform, vox, geometryAsset)
				if spriteOK {
					state.runtimeSprites = append(state.runtimeSprites, sprite)
					return true
				}
			}
		}

		currentObjectEntities[entityId] = true
		materialKey, hasKey := frameMaterialKeys[vox.VoxelPalette]
		if !hasKey {
			gekkoPalette := server.voxPalettes[vox.VoxelPalette]
			materialKey = state.materialTableKey(vox.VoxelPalette, &gekkoPalette)
			frameMaterialKeys[vox.VoxelPalette] = materialKey
		}

		obj, exists := state.instanceMap[entityId]
		if !exists {
			modelTemplate, hasTemplate := state.loadedModels[displayGeometryID]
			if !hasTemplate {
				modelTemplate = core.NewVoxelObject()
				modelTemplate.XBrickMap = displayGeometryAsset.XBrickMap
				state.loadedModels[displayGeometryID] = modelTemplate
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

		if displayGeometryAsset.XBrickMap != obj.XBrickMap {
			obj.XBrickMap = displayGeometryAsset.XBrickMap
			obj.XBrickMap.StructureDirty = true
			state.RtApp.Scene.StructureRevision++ // Force hash grid rebuild
		}
		scale := entityLODScaleVector(EffectiveVoxelScale(vox, transform), scaleAdjustX, scaleAdjustY, scaleAdjustZ)

		// Compute and apply Pivot
		pivot := entityLODRenderPivot(vox, geometryAsset, scaleAdjustX, scaleAdjustY, scaleAdjustZ)
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
		if !currentObjectEntities[eid] {
			state.RtApp.Scene.RemoveObject(obj)
			delete(state.instanceMap, eid)
			delete(state.lastMaterialKeys, obj)
			delete(state.objectToEntity, obj)
		}
	}
	for eid := range state.entityLODSelections {
		if !currentVoxelEntities[eid] {
			delete(state.entityLODSelections, eid)
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
	cameraView := readCAVolumeBudgetCamera(cmd, state.RtApp.Camera)
	caBudget := state.RtApp.FeatureConfig.CAVolumes.WithDefaults()
	caCandidates := make([]caVolumeBudgetCandidate, 0, 8)
	resolutionClampedCount := uint32(0)
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

		renderDefaults := gpu_rt.CAVolumeRenderDefaultsFor(uint32(cv.Preset), uint32(cv.Type))
		scatterColor := renderDefaults.ScatterColor
		shadowTint := renderDefaults.ShadowTint
		absorptionColor := renderDefaults.AbsorptionColor
		extinction := renderDefaults.Extinction
		emission := renderDefaults.Emission
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

		resolution := [3]uint32{
			uint32(max(1, cv.Resolution[0])),
			uint32(max(1, cv.Resolution[1])),
			uint32(max(1, cv.Resolution[2])),
		}
		resolution, resolutionClamped := clampCAVolumeResolution(resolution, caBudget)
		if resolutionClamped {
			resolutionClampedCount++
		}

		host := gpu_rt.CAVolumeHost{
			EntityID:        uint32(eid),
			Type:            uint32(cv.Type),
			Preset:          uint32(cv.Preset),
			Resolution:      resolution,
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
		}
		rawSteps := cv._gpuStepsPending
		scheduledSteps, stepDeferred, suspended := scheduleCAVolumeSteps(rawSteps, 0, false, caBudget)
		distance := float32(0)
		behindCamera := false
		if cameraView.Valid {
			offset := host.Position.Sub(cameraView.Position)
			distance = offset.Len()
			if distance > 0.001 {
				behindCamera = offset.Normalize().Dot(cameraView.Forward) < caBudget.BehindCameraDot
			}
			scheduledSteps, stepDeferred, suspended = scheduleCAVolumeSteps(rawSteps, distance, behindCamera, caBudget)
		}

		candidate := caVolumeBudgetCandidate{
			host:              host,
			volume:            cv,
			rawSteps:          rawSteps,
			scheduledSteps:    scheduledSteps,
			distance:          distance,
			visible:           host.Intensity > 0.001,
			behindCamera:      behindCamera,
			resolutionClamped: resolutionClamped,
			stepDeferred:      stepDeferred,
			suspended:         suspended,
		}
		candidate.priority = caVolumeBudgetPriority(candidate)
		caCandidates = append(caCandidates, candidate)
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
	gpuVolumes, droppedCount, deferredCount, suspendedCount, totalSteps := budgetCAVolumes(caCandidates, caBudget)
	if state.RtApp.BufferManager != nil {
		state.RtApp.BufferManager.CARequestedVolumeCount = uint32(len(caCandidates))
		state.RtApp.BufferManager.CAResolutionClampedCount = resolutionClampedCount
		state.RtApp.BufferManager.CADeferredStepVolumeCount = deferredCount
		state.RtApp.BufferManager.CASuspendedVolumeCount = suspendedCount
		state.RtApp.BufferManager.CADroppedVolumeCount = droppedCount
		state.RtApp.BufferManager.CATotalScheduledSteps = totalSteps
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
		state.RtApp.Camera.DepthMode = camera.DepthMode.Normalized()
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
			state.RtApp.BufferManager.SetSpriteAtlas(
				batch.AtlasKey,
				texAsset.Texels,
				texAsset.Width,
				texAsset.Height,
				texAsset.Version,
				assetTextureFormatToWGPU(texAsset.Format),
			)
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
			EmissionStrength:       planet.NormalizedEmissionStrength(),
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

func entityLODScaleVector(base mgl32.Vec3, adjustX, adjustY, adjustZ float32) mgl32.Vec3 {
	return mgl32.Vec3{
		base.X() * adjustX,
		base.Y() * adjustY,
		base.Z() * adjustZ,
	}
}

func entityLODLocalBounds(asset *VoxelGeometryAsset) (mgl32.Vec3, mgl32.Vec3) {
	if asset == nil {
		return mgl32.Vec3{}, mgl32.Vec3{}
	}
	minB, maxB := asset.LocalMin, asset.LocalMax
	if maxB.Sub(minB).LenSqr() <= 0 && asset.XBrickMap != nil {
		minB, maxB = asset.XBrickMap.ComputeAABB()
	}
	return minB, maxB
}

func entityLODSourcePivot(vox *VoxelModelComponent, source *VoxelGeometryAsset) mgl32.Vec3 {
	if vox == nil {
		return mgl32.Vec3{}
	}
	switch vox.PivotMode {
	case PivotModeCenter:
		minB, maxB := entityLODLocalBounds(source)
		return minB.Add(maxB).Mul(0.5)
	case PivotModeCustom:
		return vox.CustomPivot
	case PivotModeCorner:
		fallthrough
	default:
		return mgl32.Vec3{}
	}
}

func entityLODRenderPivot(vox *VoxelModelComponent, source *VoxelGeometryAsset, adjustX, adjustY, adjustZ float32) mgl32.Vec3 {
	sourcePivot := entityLODSourcePivot(vox, source)
	if adjustX == 0 {
		adjustX = 1
	}
	if adjustY == 0 {
		adjustY = 1
	}
	if adjustZ == 0 {
		adjustZ = 1
	}
	return mgl32.Vec3{
		sourcePivot.X() / adjustX,
		sourcePivot.Y() / adjustY,
		sourcePivot.Z() / adjustZ,
	}
}

func entityLODWorldPoint(transform *TransformComponent, localPoint, pivot, scale mgl32.Vec3) mgl32.Vec3 {
	if transform == nil {
		return mgl32.Vec3{}
	}
	offset := mgl32.Vec3{
		(localPoint.X() - pivot.X()) * scale.X(),
		(localPoint.Y() - pivot.Y()) * scale.Y(),
		(localPoint.Z() - pivot.Z()) * scale.Z(),
	}
	return transform.Position.Add(transform.Rotation.Rotate(offset))
}

func entityLODImpostorBaseSize(vox *VoxelModelComponent, transform *TransformComponent, source *VoxelGeometryAsset) float32 {
	baseScale := EffectiveVoxelScale(vox, transform)
	extentX, extentY, extentZ := entityLODGeometryExtents(source)
	worldX := float32(math.Abs(float64(baseScale.X()))) * extentX
	worldY := float32(math.Abs(float64(baseScale.Y()))) * extentY
	worldZ := float32(math.Abs(float64(baseScale.Z()))) * extentZ
	size := float32(math.Sqrt(float64(worldX*worldX+worldY*worldY+worldZ*worldZ))) * 1.1
	if size <= 0 {
		size = 2 * VoxelSize
	}
	return size
}

func entityLODImpostorSpriteSize(vox *VoxelModelComponent, transform *TransformComponent, source *VoxelGeometryAsset) [2]float32 {
	baseScale := EffectiveVoxelScale(vox, transform)
	extentX, extentY, _ := entityLODGeometryExtents(source)
	width := float32(math.Abs(float64(baseScale.X()))) * extentX * 1.1
	height := float32(math.Abs(float64(baseScale.Y()))) * extentY * 1.1
	if width <= 0 {
		width = 2 * VoxelSize
	}
	if height <= 0 {
		height = 2 * VoxelSize
	}
	return [2]float32{width, height}
}

func entityLODLuminance(rgb mgl32.Vec3) float32 {
	return rgb.X()*0.2126 + rgb.Y()*0.7152 + rgb.Z()*0.0722
}

func entityLODImpostorBrightnessTint(state *VoxelRtState, transform *TransformComponent) float32 {
	if state == nil || state.RtApp == nil || state.RtApp.Scene == nil || transform == nil {
		return 1
	}

	ambient := entityLODLuminance(state.RtApp.Scene.AmbientLight)
	brightness := ambient

	front := transform.Rotation.Rotate(mgl32.Vec3{0, 0, 1})
	if front.LenSqr() > 1e-6 {
		front = front.Normalize()
	}

	bestDirectional := float32(0)
	for i := range state.RtApp.Scene.Lights {
		light := state.RtApp.Scene.Lights[i]
		if uint32(light.Params[2]) != uint32(LightTypeDirectional) {
			continue
		}
		lightDir := mgl32.Vec3{light.Direction[0], light.Direction[1], light.Direction[2]}
		if lightDir.LenSqr() <= 1e-6 {
			continue
		}
		lightDir = lightDir.Normalize()
		facing := max(0, front.Dot(lightDir.Mul(-1)))
		intensity := entityLODLuminance(mgl32.Vec3{light.Color[0], light.Color[1], light.Color[2]}) * light.Color[3]
		contrib := facing * intensity
		if contrib > bestDirectional {
			bestDirectional = contrib
		}
	}
	brightness += bestDirectional * 0.85
	return clampF(brightness, 0.12, 1.0)
}

func entityLODDotBrightnessTint(state *VoxelRtState, transform *TransformComponent) float32 {
	return clampF(entityLODImpostorBrightnessTint(state, transform)*0.6, 0.08, 0.6)
}

func buildEntityLODImpostorSprite(state *VoxelRtState, server *AssetServer, transform *TransformComponent, vox *VoxelModelComponent, geometryID AssetId, source *VoxelGeometryAsset) (SpriteComponent, bool) {
	if server == nil || transform == nil || vox == nil || source == nil {
		return SpriteComponent{}, false
	}
	textureID, ok := server.entityLODImpostorTexture(geometryID, vox.VoxelPalette, source)
	if !ok || textureID == (AssetId{}) {
		return SpriteComponent{}, false
	}
	baseScale := EffectiveVoxelScale(vox, transform)
	sourcePivot := entityLODSourcePivot(vox, source)
	minB, maxB := entityLODLocalBounds(source)
	localCenter := minB.Add(maxB).Mul(0.5)
	size := entityLODImpostorSpriteSize(vox, transform, source)
	brightness := entityLODImpostorBrightnessTint(state, transform)
	return SpriteComponent{
		Enabled:       true,
		Position:      entityLODWorldPoint(transform, localCenter, sourcePivot, baseScale),
		Size:          size,
		Color:         [4]float32{brightness, brightness, brightness, 1},
		Texture:       textureID,
		BillboardMode: BillboardSpherical,
		Unlit:         false,
		AlphaMode:     SpriteAlphaTexture,
	}, true
}

func buildEntityLODDotSprite(state *VoxelRtState, server *AssetServer, transform *TransformComponent, vox *VoxelModelComponent, source *VoxelGeometryAsset) (SpriteComponent, bool) {
	if server == nil || transform == nil || vox == nil || source == nil {
		return SpriteComponent{}, false
	}
	textureID := server.entityLODDotTexture()
	if textureID == (AssetId{}) {
		return SpriteComponent{}, false
	}
	baseScale := EffectiveVoxelScale(vox, transform)
	sourcePivot := entityLODSourcePivot(vox, source)
	minB, maxB := entityLODLocalBounds(source)
	localCenter := minB.Add(maxB).Mul(0.5)
	size := max(entityLODImpostorBaseSize(vox, transform, source)*0.25, 2*VoxelSize)
	brightness := entityLODDotBrightnessTint(state, transform)
	return SpriteComponent{
		Enabled:       true,
		Position:      entityLODWorldPoint(transform, localCenter, sourcePivot, baseScale),
		Size:          [2]float32{size, size},
		Color:         [4]float32{brightness, brightness, brightness, 1},
		Texture:       textureID,
		BillboardMode: BillboardSpherical,
		Unlit:         true,
		AlphaMode:     SpriteAlphaTexture,
	}, true
}

func spriteAtlasTexture(server *AssetServer, atlasKey string) (TextureAsset, bool) {
	if server == nil || atlasKey == "" {
		return TextureAsset{}, false
	}
	parsed, err := uuid.Parse(atlasKey)
	if err != nil {
		return server.entityLODTextureByCacheKey(atlasKey)
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
