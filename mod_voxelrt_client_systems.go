package gekko

import (
	"math"
	"sort"
	"time"

	"github.com/go-gl/mathgl/mgl32"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func (mod VoxelRtModule) Install(app *App, cmd *Commands) {
	windowState := createWindowState(mod.WindowWidth, mod.WindowHeight, mod.WindowTitle)
	cmd.AddResources(windowState)
	RtApp := app_rt.NewApp(windowState.windowGlfw)
	RtApp.DebugMode = mod.DebugMode
	RtApp.RenderMode = uint32(mod.RenderMode)
	RtApp.FontPath = mod.FontPath
	if err := RtApp.Init(); err != nil {
		panic(err)
	}

	state := &VoxelRtState{
		RtApp:          RtApp,
		loadedModels:   make(map[AssetId]*core.VoxelObject),
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		caVolumeMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
		skyboxLayers:   make(map[EntityId]SkyboxLayerComponent),
	}
	cmd.AddResources(state)

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

func voxelRtSystem(input *Input, state *VoxelRtState, server *AssetServer, t *Time, cmd *Commands) {
	if state == nil || state.RtApp == nil {
		return
	}
	// Sync instances
	state.RtApp.Profiler.BeginScope("Sync Instances")
	currentEntities := make(map[EntityId]bool)

	// Collect instances from models
	MakeQuery2[TransformComponent, VoxelModelComponent](cmd).Map(func(entityId EntityId, transform *TransformComponent, vox *VoxelModelComponent) bool {
		currentEntities[entityId] = true

		obj, exists := state.instanceMap[entityId]
		if !exists {
			// Create new object for this entity
			modelTemplate, ok := state.loadedModels[vox.VoxelModel]
			if !ok {
				// Load model from Gekko assets
				gekkoModel := server.voxModels[vox.VoxelModel]
				gekkoPalette := server.voxPalettes[vox.VoxelPalette]

				xbm := volume.NewXBrickMap()
				for _, v := range gekkoModel.VoxModel.Voxels {
					xbm.SetVoxel(int(v.X), int(v.Y), int(v.Z), v.ColorIndex)
				}

				modelTemplate = core.NewVoxelObject()
				modelTemplate.XBrickMap = xbm
				modelTemplate.MaterialTable = state.buildMaterialTable(&gekkoPalette)
				state.loadedModels[vox.VoxelModel] = modelTemplate
			}

			obj = core.NewVoxelObject()

			if vox.CustomMap != nil {
				obj.XBrickMap = vox.CustomMap.Copy()
				gekkoPalette := server.voxPalettes[vox.VoxelPalette]
				obj.MaterialTable = state.buildMaterialTable(&gekkoPalette)
			} else {
				obj.XBrickMap = modelTemplate.XBrickMap.Copy()
				gekkoPalette := server.voxPalettes[vox.VoxelPalette]
				obj.MaterialTable = state.buildMaterialTable(&gekkoPalette)
			}

			state.RtApp.Scene.AddObject(obj)
			state.instanceMap[entityId] = obj
			state.objectToEntity[obj] = entityId
		}

		// Sync Transform to Renderer
		obj.Transform.Position = transform.Position
		obj.Transform.Rotation = transform.Rotation

		// SYNC MAP if CustomMap changed in ECS
		if vox.CustomMap != nil && vox.CustomMap != obj.XBrickMap {
			obj.XBrickMap = vox.CustomMap
			obj.XBrickMap.StructureDirty = true
			state.RtApp.Scene.StructureRevision++ // Force hash grid rebuild
		}

		vSize := VoxelSize

		scale := transform.Scale
		obj.Transform.Scale = mgl32.Vec3{vSize * scale.X(), vSize * scale.Y(), vSize * scale.Z()}

		// Compute and apply Pivot
		switch vox.PivotMode {
		case PivotModeCenter:
			if obj.XBrickMap != nil {
				minB, maxB := obj.XBrickMap.ComputeAABB()
				transform.Pivot = minB.Add(maxB).Mul(0.5)
			}
		case PivotModeCustom:
			transform.Pivot = vox.CustomPivot
		case PivotModeCorner:
			fallthrough
		default:
			transform.Pivot = mgl32.Vec3{0, 0, 0}
		}

		obj.Transform.Pivot = transform.Pivot

		obj.Transform.Dirty = true

		return true
	})

	for eid, obj := range state.instanceMap {
		if !currentEntities[eid] {
			state.RtApp.Scene.RemoveObject(obj)
			delete(state.instanceMap, eid)
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

	state.RtApp.Profiler.BeginScope("Sync Lights")
	MakeQuery1[CameraComponent](cmd).Map(func(entityId EntityId, camera *CameraComponent) bool {
		state.RtApp.Camera.Position = camera.Position
		state.RtApp.Camera.Yaw = mgl32.DegToRad(camera.Yaw)
		state.RtApp.Camera.Pitch = mgl32.DegToRad(camera.Pitch)
		return false
	})
	// Sync text
	MakeQuery1[TextComponent](cmd).Map(func(entityId EntityId, text *TextComponent) bool {
		state.RtApp.DrawText(text.Text, text.Position[0], text.Position[1], text.Scale, text.Color)
		return true
	})

	// Sync lights
	state.RtApp.Scene.Lights = state.RtApp.Scene.Lights[:0]
	ambientAccum := mgl32.Vec3{0, 0, 0}
	ambientFound := false
	type pendingLight struct {
		entityID  EntityId
		lightType LightType
		intensity float32
		gpu       core.Light
	}
	pendingLights := make([]pendingLight, 0, 8)

	MakeQuery1[LightComponent](cmd).Map(func(entityId EntityId, light *LightComponent) bool {
		if light.Type == LightTypeAmbient {
			ambientAccum = ambientAccum.Add(mgl32.Vec3(light.Color).Mul(light.Intensity))
			ambientFound = true
			return true
		}

		// Positional lights (Point, Directional, Spot)
		var pos mgl32.Vec3
		var rot mgl32.Quat = mgl32.QuatIdent()
		found := false

		// Exhaustive check for spatial data (Value/Pointer x World/Local)
		for _, c := range cmd.GetAllComponents(entityId) {
			switch t := c.(type) {
			case TransformComponent:
				if !found {
					pos, rot, found = t.Position, t.Rotation, true
				}
			case *TransformComponent:
				if !found {
					pos, rot, found = t.Position, t.Rotation, true
				}
			}
		}

		if !found {
			return true
		}

		// Convert ECS light to GPU light
		gpuLight := core.Light{}
		gpuLight.Position = [4]float32{pos.X(), pos.Y(), pos.Z(), 1.0}

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

		gpuLight.Params = [4]float32{light.Range, cosAngle, float32(light.Type), 0.0}
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
	}

	state.RtApp.Profiler.EndScope("Sync Lights")

	state.RtApp.Profiler.BeginScope("Sync Gizmos")
	state.RtApp.Scene.Gizmos = state.RtApp.Scene.Gizmos[:0]
	MakeQuery2[GizmoComponent, TransformComponent](cmd).Map(func(eid EntityId, g *GizmoComponent, tr *TransformComponent) bool {
		// core.Gizmo match
		rtGizmo := core.Gizmo{
			Type:  core.GizmoType(g.Type),
			Color: g.Color,
		}

		if g.Type == GizmoGrid {
			// A grid is special: we expand it into multiple lines centered at tr.Position
			// tr.Rotation applies to the whole grid.
			steps := g.Steps
			if steps <= 0 {
				steps = 10
			}
			stepSize := g.Size / float32(steps)
			halfSize := g.Size * 0.5

			for i := 0; i <= steps; i++ {
				offset := float32(i)*stepSize - halfSize

				// Line along Z (moving along X)
				lx := mgl32.Translate3D(offset, 0, -halfSize)
				sz := mgl32.Scale3D(1, 1, g.Size)
				rtLineZ := core.Gizmo{Type: core.GizmoLine, Color: g.Color}
				rtLineZ.ModelMatrix = tr.ObjectToWorld().Mul4(lx).Mul4(sz)
				state.RtApp.Scene.Gizmos = append(state.RtApp.Scene.Gizmos, rtLineZ)

				// Line along X (moving along Z)
				lz := mgl32.Translate3D(-halfSize, 0, offset)
				rx := mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0}).Mat4()
				rtLineX := core.Gizmo{Type: core.GizmoLine, Color: g.Color}
				rtLineX.ModelMatrix = tr.ObjectToWorld().Mul4(lz).Mul4(rx).Mul4(sz)
				state.RtApp.Scene.Gizmos = append(state.RtApp.Scene.Gizmos, rtLineX)
			}
			return true
		}

		// Construct Model Matrix from TransformComponent
		t := mgl32.Translate3D(tr.Position.X(), tr.Position.Y(), tr.Position.Z())
		r := tr.Rotation.Mat4()
		s := mgl32.Scale3D(tr.Scale.X(), tr.Scale.Y(), tr.Scale.Z())

		if g.Type == GizmoLine {
			// Unit line is (0,0,0) to (0,0,1). Scale Z by Size.
			s = s.Mul4(mgl32.Scale3D(1, 1, g.Size))
		} else if g.Size > 0 {
			// For Sphere, Cube, Circle, Rect, Size acts as a uniform multiplier.
			s = s.Mul4(mgl32.Scale3D(g.Size, g.Size, g.Size))
		}

		rtGizmo.ModelMatrix = t.Mul4(r).Mul4(s)

		state.RtApp.Scene.Gizmos = append(state.RtApp.Scene.Gizmos, rtGizmo)
		return true
	})
	state.RtApp.Profiler.EndScope("Sync Gizmos")

	state.RtApp.Profiler.BeginScope("GPU Batch")
	// End batching and process all accumulated updates
	if state.RtApp.BufferManager != nil {
		state.RtApp.BufferManager.EndBatch()
	}
	state.RtApp.Profiler.EndScope("GPU Batch")

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
		for atlasID, texAsset := range server.textures {
			if spriteAtlasKey(atlasID) != batch.AtlasKey {
				continue
			}
			if state.RtApp.BufferManager != nil {
				state.RtApp.BufferManager.SetSpriteAtlas(batch.AtlasKey, texAsset.Texels, texAsset.Width, texAsset.Height, texAsset.Version)
			}
			break
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
		mode := state.RtApp.Camera.DebugMode
		state.RtApp.Camera.DebugMode = (mode + 1) % 3
	}
}
