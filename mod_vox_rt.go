package gekko

import (
	"reflect"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

type VoxelRtModule struct {
	WindowWidth  int
	WindowHeight int
	WindowTitle  string
	AmbientLight mgl32.Vec3
	DebugMode    bool
	RenderMode   RenderMode
	FontPath     string
}

func (mod VoxelRtModule) Install(app *App, cmd *Commands) {
	ensureSingleRenderer(app, "voxelrt")
	var windowState *WindowState
	t := reflect.TypeOf((*WindowState)(nil)).Elem()
	res, ok := app.resources[t]
	if !ok {
		panic("Renderer 'voxelrt' requires a WindowState. Install PlatformWindowModule first, e.g., app.UseModules(NewPlatformWindow(1280, 720, \"Gekko\"))")
	}
	windowState = res.(*WindowState)
	RtApp := app_rt.NewApp(windowState.windowGlfw)
	RtApp.AmbientLight = [3]float32{mod.AmbientLight.X(), mod.AmbientLight.Y(), mod.AmbientLight.Z()}
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
		worldMap:     make(map[EntityId]*core.VoxelObject),
	}
	cmd.AddResources(state)
	cmd.AddResources(NewNavigationSystem())
	cmd.AddResources(&Profiler{})

	app.UseSystem(
		System(voxelRtDebugSystem).
			InStage(Update).
			RunAlways(),
	)
	// Voxel edit application system (M3)
	app.UseSystem(
		System(VoxelAppliedEditSystem).
			InStage(PostUpdate).
			RunAlways(),
	)
	// Cellular automaton step system (low Hz via TickRate in component)
	app.UseSystem(
		System(caStepSystem).
			InStage(Update).
			RunAlways(),
	)
	app.UseSystem(
		System(WorldStreamingSystem).
			InStage(Update).
			RunAlways(),
	)
	app.UseSystem(
		System(TransformHierarchySystem).
			InStage(Update).
			RunAlways(),
	)
	app.UseSystem(
		System(voxelRtSystem).
			InStage(PostUpdate).
			RunAlways(),
	)
	app.UseSystem(
		System(IncrementalNavBakeSystem).
			InStage(PostUpdate).
			RunAlways(),
	)

	app.UseSystem(
		System(voxelRtRenderSystem).
			InStage(Render).
			RunAlways(),
	)
}

func voxelRtSystem(input *Input, state *VoxelRtState, server *AssetServer, time *Time, cmd *Commands) {
	state.RtApp.MouseX = input.MouseX
	state.RtApp.MouseY = input.MouseY
	state.RtApp.MouseCaptured = input.MouseCaptured

	if input.JustPressed[MouseButtonRight] {
		state.RtApp.HandleClick(int(glfw.MouseButtonRight), int(glfw.Press))
	}

	if input.Pressed[KeyEqual] || input.Pressed[KeyKPPlus] {
		state.RtApp.Editor.ScaleSelected(state.RtApp.Scene, 1.05, glfw.GetTime())
	}
	if input.Pressed[KeyMinus] || input.Pressed[KeyKPMinus] {
		state.RtApp.Editor.ScaleSelected(state.RtApp.Scene, 0.95, glfw.GetTime())
	}

	state.RtApp.ClearText()

	// Begin batching updates for this frame
	state.RtApp.BufferManager.BeginBatch()

	// Sync instances
	voxelSyncInstances(state, server, cmd)

	// CA voxel bridging (render CA density as voxels; runs at CA tick rate via _dirty flag)
	// CA voxel bridging (render CA density as voxels; runs at CA tick rate via _dirty flag)
	voxelSyncCA(state, cmd)

	state.RtApp.Profiler.BeginScope("Sync World")
	voxelSyncWorld(state, cmd)
	state.RtApp.Profiler.EndScope("Sync World")

	voxelSyncLightsAndUI(state, cmd)

	voxelRtTick(state, time, cmd)
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

func (s *VoxelRtState) buildMaterialTable(gekkoPalette *VoxelPaletteAsset) []core.Material {
	materialTable := make([]core.Material, 256)

	matMap := make(map[int]VoxMaterial)
	for _, m := range gekkoPalette.Materials {
		matMap[m.ID] = m
	}

	for i, color := range gekkoPalette.VoxPalette {
		mat := core.DefaultMaterial()
		mat.BaseColor = color
		if i == 0 {
			mat.Transparency = 1.0 // Air is transparent!
		}

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

		// Infer transparency from palette alpha channel if not explicitly provided
		if color[3] < 255 {
			a := float32(color[3]) / 255.0
			t := float32(1.0) - a
			if t > mat.Transparency {
				mat.Transparency = t
			}
		}

		materialTable[i] = mat
	}
	return materialTable
}
