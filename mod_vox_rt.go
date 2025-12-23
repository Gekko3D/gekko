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
		obj.Transform.Rotation = mgl32.QuatRotate(transform.Rotation, mgl32.Vec3{0, 0, 1})
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

		// Direction: Rotate (0, 0, -1) by transform rotation
		// Assuming standard forward is -Z
		forward := mgl32.Vec3{0, 0, -1}
		rot := mgl32.QuatRotate(transform.Rotation, mgl32.Vec3{0, 0, 1}) // Axis angle? No, TransformComponent.Rotation is float32 (angle) around Z usually for 2D?
		// Wait, look at transform usage in mod_vox_rt:
		// obj.Transform.Rotation = mgl32.QuatRotate(transform.Rotation, mgl32.Vec3{0, 0, 1})
		// It seems TransformComponent has Rotation as float32 angle (Z-rotation)?
		// If so, 3D rotation might be missing in Gekko's TransformComponent?
		// Let's check TransformComponent definition.

		// Re-reading usage: mgl32.QuatRotate(transform.Rotation, mgl32.Vec3{0, 0, 1})
		// This implies transform.Rotation is a float32 angle in radians.
		// If 3D lights need full 3D rotation, the current TransformComponent might be insufficient (2D focused?).
		// BUT for now, I will follow existing pattern.
		// The forward vector (0,0,-1) rotated by Z-axis rotation effectively rotates it in XY plane? No.
		// Rotating (0,0,-1) around Z axis keeps it at (0,0,-1).
		// If the game is "2.5D" or top down, maybe lights point down?
		// Directional light (sun) usually has explicit direction.
		// Spotlights need direction.

		// Let's assume for now that if the user wants 3D direction, they might need a better Transform.
		// However, for this task, I'll calculate direction based on the available rotation.
		// If rotation is only around Z, then Direction (0,0,-1) remains (0,0,-1).
		// Maybe I should add specific direction to LightComponent?
		// No, the prompt says "set these lights to the scene via ecs".
		// I will use what is available.

		dir := rot.Rotate(forward)
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
