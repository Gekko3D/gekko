package gekko

import (
	"fmt"
	"math"
	"time"

	"github.com/go-gl/mathgl/mgl32"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
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
		RtApp:        RtApp,
		loadedModels: make(map[AssetId]*core.VoxelObject),
		instanceMap:  make(map[EntityId]*core.VoxelObject),
		caVolumeMap:  make(map[EntityId]*core.VoxelObject),
		skyboxLayers: make(map[EntityId]SkyboxLayerComponent),
	}
	cmd.AddResources(state)
	cmd.AddResources(&VoxelEditQueue{BudgetPerFrame: 5000})

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
	state.RtApp.BufferManager.BeginBatch()
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
		}

		// Sync Transform to Renderer
		obj.Transform.Position = transform.Position
		obj.Transform.Rotation = transform.Rotation

		// Metric system: Renderer Scale is ALWAYS TargetVoxelSize.
		vSize := state.RtApp.Scene.TargetVoxelSize
		if !exists {
			if _, ok := server.voxPalettes[vox.VoxelPalette]; !ok {
				fmt.Printf("PALETTE NOT FOUND! %v\n", vox.VoxelPalette)
			}
		}
		if vSize == 0 {
			vSize = 0.1
		}

		scale := transform.Scale
		obj.Transform.Scale = mgl32.Vec3{vSize * scale.X(), vSize * scale.Y(), vSize * scale.Z()}
		obj.Transform.Dirty = true

		return true
	})

	for eid, obj := range state.instanceMap {
		if !currentEntities[eid] {
			state.RtApp.Scene.RemoveObject(obj)
			delete(state.instanceMap, eid)
		}
	}
	state.RtApp.Profiler.EndScope("Sync Instances")

	// CA voxel bridging (render CA density as voxels; runs at CA tick rate via _dirty flag)
	state.RtApp.Profiler.BeginScope("Sync CA")
	currentCA := make(map[EntityId]bool)
	MakeQuery2[TransformComponent, CellularVolumeComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, cv *CellularVolumeComponent) bool {
		vSize := state.RtApp.Scene.TargetVoxelSize
		if vSize == 0 {
			vSize = 0.1
		}

		if cv == nil || !cv.BridgeToVoxels || cv._density == nil {
			return true
		}
		currentCA[eid] = true

		obj, exists := state.caVolumeMap[eid]
		if !exists {
			obj = core.NewVoxelObject()
			// Initialize a small material table with defaults; indices 1=smoke, 2=fire
			mats := make([]core.Material, 256)
			for i := range mats {
				mats[i] = core.DefaultMaterial()
			}
			// Smoke (semi-transparent)
			mats[1] = core.NewMaterial([4]uint8{180, 180, 180, 255}, [4]uint8{0, 0, 0, 0})
			mats[1].Roughness = 0.8
			mats[1].Transparency = 0.5
			mats[1].Metalness = 0.0
			// Fire (emissive)
			mats[2] = core.NewMaterial([4]uint8{255, 180, 80, 255}, [4]uint8{255, 120, 40, 255})
			mats[2].Roughness = 0.3
			mats[2].Metalness = 0.0

			obj.MaterialTable = mats
			state.RtApp.Scene.AddObject(obj)
			state.caVolumeMap[eid] = obj
		}

		// Rebuild or delta-update CA voxel volume when CA step marked dirty
		if cv._dirty {
			nx, ny, nz := cv.Resolution[0], cv.Resolution[1], cv.Resolution[2]
			thr := cv.VoxelThreshold
			if thr <= 0 {
				thr = 0.10
			}
			stride := cv.VoxelStride
			if stride <= 0 {
				stride = 1
			}
			var pal uint8 = 1
			if cv.Type == CellularFire {
				pal = 2
			}
			total := nx * ny * nz

			// Ensure previous mask storage matches current configuration
			fullRebuild := false
			if cv._prevMask == nil || len(cv._prevMask) != total || cv._prevStride != stride || cv._prevThreshold != thr || cv.Type != cv._prevType {
				cv._prevMask = make([]byte, total)
				cv._prevStride = stride
				cv._prevThreshold = thr
				cv._prevType = cv.Type
				fullRebuild = true
			}

			// Ensure XBrickMap exists
			if obj.XBrickMap == nil {
				obj.XBrickMap = volume.NewXBrickMap()
			}

			if fullRebuild {
				// Clear existing map instead of creating new one to keep GPU allocation stable
				obj.XBrickMap.ClearDirty()
				obj.XBrickMap.Sectors = make(map[[3]int]*volume.Sector)
				obj.XBrickMap.StructureDirty = true

				for z := 0; z < nz; z += stride {
					for y := 0; y < ny; y += stride {
						for x := 0; x < nx; x += stride {
							i := idx3(x, y, z, nx, ny, nz)
							if i >= 0 && cv._density[i] >= thr {
								obj.XBrickMap.SetVoxel(x, y, z, pal)
								cv._prevMask[i] = 1
							} else if i >= 0 {
								cv._prevMask[i] = 0
							}
						}
					}
				}
			} else {
				// Delta update: only change voxels that flipped state
				for z := 0; z < nz; z += stride {
					for y := 0; y < ny; y += stride {
						for x := 0; x < nx; x += stride {
							i := idx3(x, y, z, nx, ny, nz)
							if i < 0 {
								continue
							}
							active := cv._density[i] >= thr
							prev := cv._prevMask[i] != 0
							if active != prev {
								if active {
									obj.XBrickMap.SetVoxel(x, y, z, pal)
									cv._prevMask[i] = 1
								} else {
									obj.XBrickMap.SetVoxel(x, y, z, 0)
									cv._prevMask[i] = 0
								}
							}
						}
					}
				}
			}

			cv._dirty = false
		}

		// Transform sync
		obj.Transform.Position = tr.Position
		obj.Transform.Rotation = tr.Rotation
		obj.Transform.Scale = mgl32.Vec3{vSize * tr.Scale.X(), vSize * tr.Scale.Y(), vSize * tr.Scale.Z()}
		obj.Transform.Dirty = true

		return true
	})
	// Cleanup CA voxel objects for entities no longer present or with bridging disabled
	for eid, obj := range state.caVolumeMap {
		if !currentCA[eid] {
			state.RtApp.Scene.RemoveObject(obj)
			delete(state.caVolumeMap, eid)
		}
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
	pointCount := 0

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
		state.RtApp.Scene.Lights = append(state.RtApp.Scene.Lights, gpuLight)
		pointCount++
		return true
	})

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

			tBase := mgl32.Translate3D(tr.Position.X(), tr.Position.Y(), tr.Position.Z())
			rBase := tr.Rotation.Mat4()

			for i := 0; i <= steps; i++ {
				offset := float32(i)*stepSize - halfSize

				// Line along Z (moving along X)
				lx := mgl32.Translate3D(offset, 0, -halfSize)
				sz := mgl32.Scale3D(1, 1, g.Size)
				rtLineZ := core.Gizmo{Type: core.GizmoLine, Color: g.Color}
				rtLineZ.ModelMatrix = tBase.Mul4(rBase).Mul4(lx).Mul4(sz)
				state.RtApp.Scene.Gizmos = append(state.RtApp.Scene.Gizmos, rtLineZ)

				// Line along X (moving along Z)
				lz := mgl32.Translate3D(-halfSize, 0, offset)
				rx := mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0}).Mat4()
				rtLineX := core.Gizmo{Type: core.GizmoLine, Color: g.Color}
				rtLineX.ModelMatrix = tBase.Mul4(rBase).Mul4(lz).Mul4(rx).Mul4(sz)
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
	state.RtApp.BufferManager.EndBatch()
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

	vSize := state.RtApp.Scene.TargetVoxelSize
	if vSize == 0 {
		vSize = 0.1
	}
	invVsize := 1.0 / vSize
	state.RtApp.ParticleSpawnCount = uint32(len(spawnReqs))
	state.RtApp.BufferManager.UpdateParticleParams(float32(t.Dt), float32(invVsize), uint32(time.Now().UnixNano()), emitterCount)
	pRecreated := state.RtApp.BufferManager.UpdateParticles(1000000, emitters) // Pass max count
	state.RtApp.BufferManager.UpdateSpawnRequests(spawnReqs)
	if pRecreated || state.RtApp.BufferManager.ParticlesBindGroup0 == nil || state.RtApp.BufferManager.ParticleSimBG0 == nil {
		state.RtApp.BufferManager.CreateParticleSimBindGroups()
		state.RtApp.BufferManager.CreateParticlesBindGroups(state.RtApp.ParticlesPipeline)
	}

	// Sync GPU sprites
	spriteBytes, spriteCount, spriteAtlasId := spritesSync(state, cmd)
	if spriteAtlasId != (AssetId{}) && spriteAtlasId != state.lastSpriteAtlas {
		if texAsset, ok := server.textures[spriteAtlasId]; ok {
			state.RtApp.SetSpriteAtlas(texAsset.Texels, texAsset.Width, texAsset.Height)
			state.lastSpriteAtlas = spriteAtlasId
		}
	}
	sRecreated := state.RtApp.BufferManager.UpdateSprites(spriteBytes, spriteCount)
	if sRecreated || state.RtApp.BufferManager.SpriteAtlasDirty || state.RtApp.BufferManager.SpritesBindGroup0 == nil {
		state.RtApp.BufferManager.CreateSpritesBindGroups(state.RtApp.SpritesPipeline)
		state.RtApp.BufferManager.SpriteAtlasDirty = false
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
