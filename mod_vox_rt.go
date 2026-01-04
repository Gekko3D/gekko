package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

type RaycastHit struct {
	Hit    bool
	T      float32
	Pos    [3]int
	Normal mgl32.Vec3
	Entity EntityId
}

type DebugRay struct {
	Origin   mgl32.Vec3
	Dir      mgl32.Vec3
	Color    [4]float32
	Duration float32
}

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
	FontPath     string
}

type VoxelRtState struct {
	rtApp         *app_rt.App
	loadedModels  map[AssetId]*core.VoxelObject
	instanceMap   map[EntityId]*core.VoxelObject
	particlePools map[EntityId]*particlePool
	caVolumeMap   map[EntityId]*core.VoxelObject
	worldMap      map[EntityId]*core.VoxelObject

	// Debug rays
	debugRays []DebugRay
}

func (s *VoxelRtState) WindowSize() (int, int) {
	if s == nil || s.rtApp == nil {
		return 0, 0
	}
	return int(s.rtApp.Config.Width), int(s.rtApp.Config.Height)
}

func (s *VoxelRtState) FPS() float64 {
	if s == nil || s.rtApp == nil {
		return 0
	}
	return s.rtApp.FPS
}

func (s *VoxelRtState) ProfilerStats() string {
	if s == nil || s.rtApp == nil {
		return ""
	}
	return s.rtApp.Profiler.GetStatsString()
}

func (s *VoxelRtState) IsDebug() bool {
	if s == nil || s.rtApp == nil {
		return false
	}
	return s.rtApp.DebugMode
}

func (s *VoxelRtState) DrawText(text string, x, y float32, scale float32, color [4]float32) {
	if s != nil && s.rtApp != nil {
		s.rtApp.DrawText(text, x, y, scale, color)
	}
}

func (s *VoxelRtState) Counter(name string) int {
	if s == nil || s.rtApp == nil {
		return 0
	}
	return s.rtApp.Profiler.Counts[name]
}

func (s *VoxelRtState) SetDebugMode(enabled bool) {
	if s != nil && s.rtApp != nil {
		s.rtApp.DebugMode = enabled
	}
}

func (s *VoxelRtState) getVoxelObject(eid EntityId) *core.VoxelObject {
	if obj, ok := s.instanceMap[eid]; ok {
		return obj
	}
	if obj, ok := s.worldMap[eid]; ok {
		return obj
	}
	if obj, ok := s.caVolumeMap[eid]; ok {
		return obj
	}
	return nil
}

func (s *VoxelRtState) VoxelSphereEdit(eid EntityId, worldCenter mgl32.Vec3, radius float32, val uint8) {
	if s == nil {
		return
	}
	obj := s.getVoxelObject(eid)
	if obj == nil || obj.XBrickMap == nil {
		return
	}

	// Transform center to local space
	w2o := obj.Transform.WorldToObject()
	localCenter := w2o.Mul4x1(worldCenter.Vec4(1.0)).Vec3()

	// Scale radius by object scale (approximate by avg scale)
	scale := obj.Transform.Scale
	avgScale := (scale.X() + scale.Y() + scale.Z()) / 3.0
	if avgScale == 0 {
		avgScale = 1.0
	}
	localRadius := radius / avgScale

	volume.Sphere(obj.XBrickMap, localCenter, localRadius, val)
}

func (s *VoxelRtState) DrawDebugRay(origin, dir mgl32.Vec3, color [4]float32, duration float32) {
	if s == nil {
		return
	}
	s.debugRays = append(s.debugRays, DebugRay{
		Origin:   origin,
		Dir:      dir,
		Color:    color,
		Duration: duration,
	})
}

func (s *VoxelRtState) Project(pos mgl32.Vec3) (float32, float32, bool) {
	if s == nil || s.rtApp == nil {
		return 0, 0, false
	}
	// Use current camera to build projection
	view := s.rtApp.Camera.GetViewMatrix()
	aspect := float32(s.rtApp.Config.Width) / float32(s.rtApp.Config.Height)
	if aspect == 0 {
		aspect = 1.0
	}

	// GET ACTUAL FOV FROM CAMERA COMPONENT
	fov := float32(45.0) // Default
	// We need a way to get the true FOV. For now matching playing.go
	proj := mgl32.Perspective(mgl32.DegToRad(fov), aspect, 0.1, 1000.0)
	vp := proj.Mul4(view)

	clip := vp.Mul4x1(pos.Vec4(1.0))

	// Clip points behind far or too close to near plane
	if clip.W() < 0.1 {
		return 0, 0, false
	}

	ndc := clip.Vec3().Mul(1.0 / clip.W())

	// NDC to Screen (USE PIXEL DIMENSIONS)
	w, h := float32(s.rtApp.Config.Width), float32(s.rtApp.Config.Height)
	x := (ndc.X()*0.5 + 0.5) * w
	y := (1.0 - (ndc.Y()*0.5 + 0.5)) * h

	// Final bounds check
	if x < 0 || x > w || y < 0 || y > h {
		return x, y, false
	}

	return x, y, true
}

func (s *VoxelRtState) Raycast(origin, dir mgl32.Vec3, tMax float32) RaycastHit {
	if s == nil {
		return RaycastHit{}
	}

	bestHit := RaycastHit{T: tMax + 1.0}

	// 1. Check all instances (models, CA, etc.)
	checkMap := func(m map[EntityId]*core.VoxelObject) {
		for eid, obj := range m {
			if obj.XBrickMap == nil {
				continue
			}

			// Transform ray to object space
			w2o := obj.Transform.WorldToObject()
			localOrigin := w2o.Mul4x1(origin.Vec4(1.0)).Vec3()

			// Direction transformation
			localDirUnnorm := w2o.Mul4x1(dir.Vec4(0.0)).Vec3()
			scaleFactor := localDirUnnorm.Len()
			// Avoid division by zero
			if scaleFactor < 1e-6 {
				continue
			}
			localDir := localDirUnnorm.Mul(1.0 / scaleFactor)

			localTMax := tMax * scaleFactor

			hit, t, pos, normal := obj.XBrickMap.RayMarch(localOrigin, localDir, 0, localTMax)
			if hit {
				// We need to convert t back to world space distance.
				// World distance = t * |ObjDir| where ObjDir is the untransformed local direction.
				// Since we normalized localDir, we need the original scale factor.

				// Actually, a better way: hitPointWorld = o2w * hitPointLocal.
				// tWorld = |hitPointWorld - origin|

				o2w := obj.Transform.ObjectToWorld()
				localHitPos := localOrigin.Add(localDir.Mul(t))
				worldHitPos := o2w.Mul4x1(localHitPos.Vec4(1.0)).Vec3()
				worldT := worldHitPos.Sub(origin).Len()

				if worldT < bestHit.T {
					bestHit.Hit = true
					bestHit.T = worldT
					bestHit.Pos = pos

					// Transform normal to world space
					// Normal transform: transpose(inverse(M))
					worldNormal := o2w.Mul4x1(normal.Vec4(0.0)).Vec3().Normalize()
					bestHit.Normal = worldNormal
					bestHit.Entity = eid
				}
			}
		}
	}

	checkMap(s.instanceMap)
	checkMap(s.caVolumeMap)
	checkMap(s.worldMap)

	if bestHit.Hit {
		return bestHit
	}
	return RaycastHit{}
}

func (mod VoxelRtModule) Install(app *App, cmd *Commands) {
	windowState := createWindowState(mod.WindowWidth, mod.WindowHeight, mod.WindowTitle)
	cmd.AddResources(windowState)
	rtApp := app_rt.NewApp(windowState.windowGlfw)
	rtApp.AmbientLight = [3]float32{mod.AmbientLight.X(), mod.AmbientLight.Y(), mod.AmbientLight.Z()}
	rtApp.DebugMode = mod.DebugMode
	rtApp.RenderMode = uint32(mod.RenderMode)
	rtApp.FontPath = mod.FontPath
	if err := rtApp.Init(); err != nil {
		panic(err)
	}

	state := &VoxelRtState{
		rtApp:        rtApp,
		loadedModels: make(map[AssetId]*core.VoxelObject),
		instanceMap:  make(map[EntityId]*core.VoxelObject),
		caVolumeMap:  make(map[EntityId]*core.VoxelObject),
		worldMap:     make(map[EntityId]*core.VoxelObject),
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
		System(WorldStreamingSystem).
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
	state.rtApp.Profiler.BeginScope("Sync Instances")
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
	state.rtApp.Profiler.EndScope("Sync Instances")

	// CA voxel bridging (render CA density as voxels; runs at CA tick rate via _dirty flag)
	state.rtApp.Profiler.BeginScope("Sync CA")
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
				obj.XBrickMap.BrickAtlasMap = make(map[[6]int]uint32)
				obj.XBrickMap.NextAtlasOffset = 0
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
	state.rtApp.Profiler.EndScope("Sync CA")

	state.rtApp.Profiler.BeginScope("Sync World")
	currentWorlds := make(map[EntityId]bool)
	MakeQuery1[WorldComponent](cmd).Map(func(eid EntityId, world *WorldComponent) bool {
		currentWorlds[eid] = true
		obj, exists := state.worldMap[eid]
		if !exists {
			obj = core.NewVoxelObject()
			// Default material table for world
			mats := make([]core.Material, 256)
			for i := range mats {
				mats[i] = core.DefaultMaterial()
				mats[i].BaseColor = [4]uint8{120, 120, 120, 255}
				if i == 0 {
					mats[i].Transparency = 1.0 // Air is transparent!
				}
			}
			// Ground color (index 1)
			mats[1].BaseColor = [4]uint8{100, 255, 100, 255}

			obj.MaterialTable = mats
			state.rtApp.Scene.AddObject(obj)
			state.worldMap[eid] = obj
		}

		// Use the XBM from the world component
		obj.XBrickMap = world.GetXBrickMap()

		// World is usually stationary at origin
		obj.Transform.Position = mgl32.Vec3{0, 0, 0}
		obj.Transform.Rotation = mgl32.QuatIdent()
		obj.Transform.Scale = mgl32.Vec3{1, 1, 1}
		obj.Transform.Dirty = true

		return true
	})
	for eid, obj := range state.worldMap {
		if !currentWorlds[eid] {
			state.rtApp.Scene.RemoveObject(obj)
			delete(state.worldMap, eid)
		}
	}
	state.rtApp.Profiler.EndScope("Sync World")

	state.rtApp.Profiler.BeginScope("Sync Lights")
	MakeQuery1[CameraComponent](cmd).Map(func(entityId EntityId, camera *CameraComponent) bool {
		state.rtApp.Camera.Position = camera.Position
		state.rtApp.Camera.Yaw = mgl32.DegToRad(camera.Yaw)
		state.rtApp.Camera.Pitch = mgl32.DegToRad(camera.Pitch)
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
	state.rtApp.Profiler.EndScope("Sync Lights")

	state.rtApp.Profiler.BeginScope("GPU Batch")
	// End batching and process all accumulated updates
	state.rtApp.BufferManager.EndBatch()
	state.rtApp.Profiler.EndScope("GPU Batch")

	// CPU-simulate and upload particle instances
	instances := particlesCollect(state, time, cmd)
	pRecreated := state.rtApp.BufferManager.UpdateParticles(instances)
	if pRecreated || state.rtApp.BufferManager.ParticlesBindGroup0 == nil {
		state.rtApp.BufferManager.CreateParticlesBindGroups(state.rtApp.ParticlesPipeline)
	}

	state.rtApp.Profiler.BeginScope("RT Update")

	// Process debug rays BEFORE Update() so DrawText is captured
	dt := float32(time.Dt)
	if dt <= 0 {
		dt = 1.0 / 60.0
	}
	remainingRays := state.debugRays[:0]
	for _, ray := range state.debugRays {
		// Calculate hit point for visualization
		hit := state.Raycast(ray.Origin.Add(ray.Dir.Mul(0.1)), ray.Dir, 1000.0)
		dist := float32(100.0)
		if hit.Hit {
			dist = hit.T + 0.1
			// Draw marker at hit
			if x, y, ok := state.Project(ray.Origin.Add(ray.Dir.Mul(dist))); ok {
				state.rtApp.DrawText("*", x-8, y-16, 2.0, ray.Color)
			}
		}

		// Draw path
		steps := 50
		for i := 1; i <= steps; i++ {
			t := (dist / float32(steps)) * float32(i)
			if x, y, ok := state.Project(ray.Origin.Add(ray.Dir.Mul(t))); ok {
				// Fade alpha based on distance for cooler look
				alpha := 1.0 - (t/dist)*0.8
				color := ray.Color
				color[3] *= alpha
				state.rtApp.DrawText(".", x-6, y-12, 1.4, color)
			}
		}

		ray.Duration -= dt
		if ray.Duration > 0 {
			remainingRays = append(remainingRays, ray)
		}
	}
	state.debugRays = remainingRays

	state.rtApp.Update()

	state.rtApp.Profiler.EndScope("RT Update")
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
