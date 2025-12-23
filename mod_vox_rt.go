package gekko

import (
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
					// MagicaVoxel: X=right, Y=forward, Z=up
					// Gekko: X=right, Y=up, Z=depth
					// Map MV.Z to Y (Height) and MV.Y to Z (Depth)
					xbm.SetVoxel(int(v.X), int(v.Z), int(v.Y), v.ColorIndex)
				}

				modelTemplate = core.NewVoxelObject()
				modelTemplate.XBrickMap = xbm
				// Convert palette and materials
				modelTemplate.MaterialTable = make([]core.Material, 256)

				// Create a lookup for materials by ID (palette index)
				matMap := make(map[int]VoxMaterial)
				for _, m := range gekkoPalette.Materials {
					matMap[m.ID] = m
				}

				for i, color := range gekkoPalette.VoxPalette {
					mat := core.DefaultMaterial()
					mat.BaseColor = color

					// Check if we have MATL overrides
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
							// For emissive, we scale the base color or use a separate emissive color prompt?
							// MV usually uses emission weight on the material color.
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

					// Apply PBR overrides from asset if it's a procedural PBR palette
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

			// Instantiate
			obj = core.NewVoxelObject()
			obj.XBrickMap = modelTemplate.XBrickMap
			obj.MaterialTable = modelTemplate.MaterialTable
			state.rtApp.Scene.AddObject(obj)
			state.instanceMap[entityId] = obj
		}

		// Update Transform
		obj.Transform.Position = transform.Position
		obj.Transform.Rotation = mgl32.QuatRotate(transform.Rotation, mgl32.Vec3{0, 0, 1})
		obj.Transform.Scale = transform.Scale
		obj.Transform.Dirty = true

		return true
	})

	// Remove old entities
	for eid, obj := range state.instanceMap {
		if !currentEntities[eid] {
			state.rtApp.Scene.RemoveObject(obj)
			delete(state.instanceMap, eid)
		}
	}

	// Sync camera
	MakeQuery1[CameraComponent](cmd).Map(func(entityId EntityId, camera *CameraComponent) bool {
		state.rtApp.Camera.Position = camera.Position
		state.rtApp.Camera.Yaw = camera.Yaw
		state.rtApp.Camera.Pitch = camera.Pitch
		return false // Only one camera for now
	})

	state.rtApp.Update()
}

func voxelRtRenderSystem(state *VoxelRtState) {
	state.rtApp.Render()
}
