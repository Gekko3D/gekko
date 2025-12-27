package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

type RenderMode uint32

const (
	RenderModeLit RenderMode = iota
	RenderModeAlbedo
	RenderModeNormals
	RenderModeGBuffer
)

type VoxelRtModule struct {
	WindowWidth  int
	WindowHeight int
	WindowTitle  string
	AmbientLight mgl32.Vec3
	DebugMode    bool
	RenderMode   RenderMode
}

type VoxelRtState struct {
	rtApp         *app_rt.App
	loadedModels  map[AssetId]*core.VoxelObject
	instanceMap   map[EntityId]*core.VoxelObject
	particlePools map[EntityId]*particlePool
	caVolumeMap   map[EntityId]*core.VoxelObject
}

func (mod VoxelRtModule) Install(app *App, cmd *Commands) {
	windowState := createWindowState(mod.WindowWidth, mod.WindowHeight, mod.WindowTitle)
	cmd.AddResources(windowState)

	rtApp := app_rt.NewApp(windowState.windowGlfw)
	rtApp.AmbientLight = [3]float32{mod.AmbientLight.X(), mod.AmbientLight.Y(), mod.AmbientLight.Z()}
	rtApp.DebugMode = mod.DebugMode
	rtApp.RenderMode = uint32(mod.RenderMode)
	if err := rtApp.Init(); err != nil {
		panic(err)
	}

	state := &VoxelRtState{
		rtApp:        rtApp,
		loadedModels: make(map[AssetId]*core.VoxelObject),
		instanceMap:  make(map[EntityId]*core.VoxelObject),
		caVolumeMap:  make(map[EntityId]*core.VoxelObject),
	}
	cmd.AddResources(state)

	app.UseSystem(
		System(voxelRtDebugSystem).
			InStage(Update).
			RunAlways(),
	)
	// Cellular automaton step system (low Hz via TickRate in component)
	app.UseSystem(
		System(caStepSystem).
			InStage(Update).
			RunAlways(),
	)
	app.UseSystem(
		System(voxelRtSystem).
			InStage(PostUpdate).
			RunAlways(),
	)

	app.UseSystem(
		System(voxelRtRenderSystem).
			InStage(Render).
			RunAlways(),
	)
}

func voxelRtSystem(state *VoxelRtState, server *AssetServer, time *Time, cmd *Commands) {
	state.rtApp.ClearText()

	// Begin batching updates for this frame
	state.rtApp.BufferManager.BeginBatch()

	// Sync instances
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
					xbm.SetVoxel(int(v.X), int(v.Z), int(v.Y), v.ColorIndex)
				}

				modelTemplate = core.NewVoxelObject()
				modelTemplate.XBrickMap = xbm
				modelTemplate.MaterialTable = make([]core.Material, 256)

				matMap := make(map[int]VoxMaterial)
				for _, m := range gekkoPalette.Materials {
					matMap[m.ID] = m
				}

				for i, color := range gekkoPalette.VoxPalette {
					mat := core.DefaultMaterial()
					mat.BaseColor = color

					if vMat, ok := matMap[i]; ok {
						if r, ok := vMat.Property["_rough"].(float32); ok {
							mat.Roughness = r
						}
						if m, ok := vMat.Property["_metal"].(float32); ok {
							mat.Metalness = m
						}
						if ior, ok := vMat.Property["_ior"].(float32); ok {
							mat.IOR = ior
						}
						if trans, ok := vMat.Property["_trans"].(float32); ok {
							mat.Transparency = trans
						}
						if emit, ok := vMat.Property["_emit"].(float32); ok {
							flux := float32(1.0)
							if f, ok := vMat.Property["_flux"].(float32); ok {
								flux = f
							}
							power := emit * flux
							mat.Emissive = [4]uint8{
								uint8(min(255, float32(color[0])*power)),
								uint8(min(255, float32(color[1])*power)),
								uint8(min(255, float32(color[2])*power)),
								255,
							}
						}
					}

					if gekkoPalette.IsPBR {
						mat.Roughness = gekkoPalette.Roughness
						mat.Metalness = gekkoPalette.Metalness
						mat.IOR = gekkoPalette.IOR
						if gekkoPalette.Emission > 0 {
							power := gekkoPalette.Emission
							mat.Emissive = [4]uint8{
								uint8(min(255, float32(color[0])*power)),
								uint8(min(255, float32(color[1])*power)),
								uint8(min(255, float32(color[2])*power)),
								255,
							}
						}
					}

					modelTemplate.MaterialTable[i] = mat
				}
				state.loadedModels[vox.VoxelModel] = modelTemplate
			}

			obj = core.NewVoxelObject()
			obj.XBrickMap = modelTemplate.XBrickMap.Copy()
			obj.MaterialTable = modelTemplate.MaterialTable
			state.rtApp.Scene.AddObject(obj)
			state.instanceMap[entityId] = obj
		}

		obj.Transform.Position = transform.Position
		obj.Transform.Rotation = transform.Rotation
		obj.Transform.Scale = transform.Scale
		obj.Transform.Dirty = true

		return true
	})

	for eid, obj := range state.instanceMap {
		if !currentEntities[eid] {
			state.rtApp.Scene.RemoveObject(obj)
			delete(state.instanceMap, eid)
		}
	}

	// CA voxel bridging (render CA density as voxels; runs at CA tick rate via _dirty flag)
	currentCA := make(map[EntityId]bool)
	MakeQuery2[TransformComponent, CellularVolumeComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, cv *CellularVolumeComponent) bool {
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
			// Smoke
			mats[1] = core.NewMaterial([4]uint8{180, 180, 180, 255}, [4]uint8{0, 0, 0, 0})
			mats[1].Roughness = 0.8
			mats[1].Metalness = 0.0
			// Fire (emissive)
			mats[2] = core.NewMaterial([4]uint8{255, 180, 80, 255}, [4]uint8{255, 120, 40, 255})
			mats[2].Roughness = 0.3
			mats[2].Metalness = 0.0

			obj.MaterialTable = mats
			state.rtApp.Scene.AddObject(obj)
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
			if cv._prevMask == nil || len(cv._prevMask) != total || cv._prevStride != stride || cv._prevThreshold != thr {
				cv._prevMask = make([]byte, total)
				cv._prevStride = stride
				cv._prevThreshold = thr
				fullRebuild = true
			}

			// Ensure XBrickMap exists (core.NewVoxelObject creates one by default, but be safe)
			if obj.XBrickMap == nil {
				obj.XBrickMap = volume.NewXBrickMap()
			}

			if fullRebuild {
				// Clear by re-creating the map for simplicity on config change
				obj.XBrickMap = volume.NewXBrickMap()
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
		obj.Transform.Scale = tr.Scale
		obj.Transform.Dirty = true

		return true
	})
	// Cleanup CA voxel objects for entities no longer present or with bridging disabled
	for eid, obj := range state.caVolumeMap {
		if !currentCA[eid] {
			state.rtApp.Scene.RemoveObject(obj)
			delete(state.caVolumeMap, eid)
		}
	}

	MakeQuery1[CameraComponent](cmd).Map(func(entityId EntityId, camera *CameraComponent) bool {
		state.rtApp.Camera.Position = camera.Position
		state.rtApp.Camera.Yaw = camera.Yaw
		state.rtApp.Camera.Pitch = camera.Pitch
		return false
	})
	// Sync text
	MakeQuery1[TextComponent](cmd).Map(func(entityId EntityId, text *TextComponent) bool {
		state.rtApp.DrawText(text.Text, text.Position[0], text.Position[1], text.Scale, text.Color)
		return true
	})

	// Sync lights
	state.rtApp.Scene.Lights = state.rtApp.Scene.Lights[:0]
	MakeQuery2[TransformComponent, LightComponent](cmd).Map(func(entityId EntityId, transform *TransformComponent, light *LightComponent) bool {
		// Convert ECS light to GPU light
		gpuLight := core.Light{}

		// Position/Direction from transform
		// Position
		gpuLight.Position = [4]float32{transform.Position.X(), transform.Position.Y(), transform.Position.Z(), 1.0}

		// Direction: Rotate base direction by transform rotation
		// Point lights don't care about direction.
		// Directional and Spotlights do.

		baseForward := mgl32.Vec3{0, 0, -1}
		if light.Type == LightTypeDirectional {
			// For directional sunlight, use a slanted base so Z-rotation makes it orbit
			baseForward = mgl32.Vec3{1, -1, 0}.Normalize()
		} else if light.Type == LightTypeSpot {
			// For spot lights, pointing -Y (Down) allows Z-rotation to steer them in a cone or circle
			baseForward = mgl32.Vec3{0, -1, 0}
		}

		dir := transform.Rotation.Rotate(baseForward)
		gpuLight.Direction = [4]float32{dir.X(), dir.Y(), dir.Z(), 0.0}

		gpuLight.Color = [4]float32{light.Color[0], light.Color[1], light.Color[2], light.Intensity}

		// Params: Range, ConeAngle, Type, Padding
		// Cone angle passed as cosine for shader optimization
		cosAngle := float32(0.0)
		if light.Type == LightTypeSpot {
			cosAngle = float32(math.Cos(float64(light.ConeAngle) * math.Pi / 180.0 / 2.0))
		}

		gpuLight.Params = [4]float32{light.Range, cosAngle, float32(light.Type), 0.0}

		state.rtApp.Scene.Lights = append(state.rtApp.Scene.Lights, gpuLight)
		return true
	})

	// End batching and process all accumulated updates
	state.rtApp.BufferManager.EndBatch()

	// CPU-simulate and upload particle instances
	instances := particlesCollect(state, time, cmd)
	state.rtApp.BufferManager.UpdateParticles(instances)

	state.rtApp.Update()
}

func voxelRtRenderSystem(state *VoxelRtState) {
	state.rtApp.Render()
}

func (s *VoxelRtState) CycleRenderMode() {
	if s != nil && s.rtApp != nil {
		s.rtApp.RenderMode = (s.rtApp.RenderMode + 1) % 4
	}
}

func voxelRtDebugSystem(input *Input, state *VoxelRtState) {
	if input.JustPressed[KeyF2] {
		mode := state.rtApp.Camera.DebugMode
		state.rtApp.Camera.DebugMode = (mode + 1) % 3
	}
}
