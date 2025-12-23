package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

type VoxelRtModule struct {
	WindowWidth  int
	WindowHeight int
	WindowTitle  string
	AmbientLight mgl32.Vec3
	DebugMode    bool
}

type VoxelRtState struct {
	rtApp        *app_rt.App
	loadedModels map[AssetId]*core.VoxelObject
	instanceMap  map[EntityId]*core.VoxelObject
}

func (mod VoxelRtModule) Install(app *App, cmd *Commands) {
	windowState := createWindowState(mod.WindowWidth, mod.WindowHeight, mod.WindowTitle)
	cmd.AddResources(windowState)

	rtApp := app_rt.NewApp(windowState.windowGlfw)
	rtApp.AmbientLight = [3]float32{mod.AmbientLight.X(), mod.AmbientLight.Y(), mod.AmbientLight.Z()}
	rtApp.DebugMode = mod.DebugMode
	if err := rtApp.Init(); err != nil {
		panic(err)
	}

	state := &VoxelRtState{
		rtApp:        rtApp,
		loadedModels: make(map[AssetId]*core.VoxelObject),
		instanceMap:  make(map[EntityId]*core.VoxelObject),
	}
	cmd.AddResources(state)

	app.UseSystem(
		System(voxelRtDebugSystem).
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

func voxelRtSystem(state *VoxelRtState, server *AssetServer, cmd *Commands) {
	state.rtApp.ClearText()

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
			obj.XBrickMap = modelTemplate.XBrickMap
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

	state.rtApp.Update()
}

func voxelRtRenderSystem(state *VoxelRtState) {
	state.rtApp.Render()
}

func voxelRtDebugSystem(input *Input, state *VoxelRtState) {
	if input.JustPressed[KeyF1] {
		state.rtApp.DebugMode = !state.rtApp.DebugMode
	}
}
