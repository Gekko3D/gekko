package gekko

import (
	"time"

	"github.com/go-gl/mathgl/mgl32"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func voxelSphereEditWithTransform(xbm *volume.XBrickMap, tr *core.Transform, worldCenter mgl32.Vec3, radius float32, val uint8) {
	if xbm == nil || tr == nil {
		return
	}
	w2o := tr.WorldToObject()
	voxelCenter := w2o.Mul4x1(worldCenter.Vec4(1.0)).Vec3()

	scale := tr.Scale
	avgScale := (scale.X() + scale.Y() + scale.Z()) / 3.0
	if avgScale == 0 {
		avgScale = 1.0
	}
	volume.Sphere(xbm, voxelCenter, radius/avgScale, val)
}

type RaycastHit struct {
	Hit    bool
	T      float32
	Pos    [3]int
	Normal mgl32.Vec3
	Entity EntityId
}

type RenderMode uint32

const (
	RenderModeLit RenderMode = iota
	RenderModeAlbedo
	RenderModeNormals
	RenderModeGBuffer
	RenderModeDirect
	RenderModeIndirect
	RenderModeLightDensity
	RenderModeCount
)

func (m RenderMode) String() string {
	switch m {
	case RenderModeLit:
		return "Lit"
	case RenderModeAlbedo:
		return "Albedo"
	case RenderModeNormals:
		return "Normals"
	case RenderModeGBuffer:
		return "G-Buffer"
	case RenderModeDirect:
		return "Direct"
	case RenderModeIndirect:
		return "Indirect"
	case RenderModeLightDensity:
		return "Light Density"
	default:
		return "Unknown"
	}
}

type LightingQualityConfig = core.LightingQualityConfig
type LightingQualityPreset = core.LightingQualityPreset
type VoxelRtDebugMode = core.DebugMode

const (
	LightingQualityPerformance = core.LightingQualityPresetPerformance
	LightingQualityBalanced    = core.LightingQualityPresetBalanced
	LightingQualityQuality     = core.LightingQualityPresetQuality
	VoxelRtDebugModeOff        = core.DebugModeOff
	VoxelRtDebugModeScene      = core.DebugModeScene
)

type VoxelRtModule struct {
	WindowWidth     int
	WindowHeight    int
	WindowTitle     string
	DebugMode       bool
	HideDebugGizmos bool
	RenderMode      RenderMode
	QualityPreset   LightingQualityPreset
	LightingQuality LightingQualityConfig
	OcclusionMode   core.OcclusionMode
	FontPath        string
}

type VoxelRtState struct {
	RtApp             *app_rt.App
	HideDebugGizmos   bool
	loadedModels      map[AssetId]*core.VoxelObject
	instanceMap       map[EntityId]*core.VoxelObject
	particlePools     map[EntityId]*particlePool
	caVolumeMap       map[EntityId]*core.VoxelObject
	objectToEntity    map[*core.VoxelObject]EntityId
	skyboxLayers      map[EntityId]SkyboxLayerComponent // Stored values to detect changes
	skyboxSun         SkyboxSunComponent
	lastSkyboxVer     int64 // To track if any layer changed
	SunDirection      mgl32.Vec3
	SunIntensity      float32
	lastParticleAtlas AssetId
	lastSpriteAtlas   AssetId
}

func (s *VoxelRtState) WindowSize() (int, int) {
	if s == nil || s.RtApp == nil {
		return 0, 0
	}
	return int(s.RtApp.Config.Width), int(s.RtApp.Config.Height)
}

func (s *VoxelRtState) FPS() float64 {
	if s == nil || s.RtApp == nil {
		return 0
	}
	return s.RtApp.FPS
}

func (s *VoxelRtState) ProfilerStats() string {
	if s == nil || s.RtApp == nil {
		return ""
	}
	return s.RtApp.Profiler.GetStatsString()
}

func (s *VoxelRtState) IsDebug() bool {
	if s == nil || s.RtApp == nil {
		return false
	}
	return s.RtApp.DebugMode
}

func (s *VoxelRtState) DrawText(text string, x, y float32, scale float32, color [4]float32) {
	if s != nil && s.RtApp != nil {
		s.RtApp.DrawText(text, x, y, scale, color)
	}
}

func (s *VoxelRtState) MeasureText(text string, scale float32) (float32, float32) {
	if s == nil || s.RtApp == nil {
		return 0, 0
	}
	return s.RtApp.MeasureText(text, scale)
}

func (s *VoxelRtState) GetLineHeight(scale float32) float32 {
	if s == nil || s.RtApp == nil {
		return 0
	}
	return s.RtApp.GetLineHeight(scale)
}

func (s *VoxelRtState) SetParticleAtlas(data []byte, w, h uint32) {
	if s != nil && s.RtApp != nil {
		s.RtApp.SetParticleAtlas(data, w, h)
	}
}

func (s *VoxelRtState) Counter(name string) int {
	if s == nil || s.RtApp == nil {
		return 0
	}
	return s.RtApp.Profiler.Counts[name]
}

func (s *VoxelRtState) SetDebugMode(enabled bool) {
	if s != nil && s.RtApp != nil {
		s.RtApp.DebugMode = enabled
	}
}

func (s *VoxelRtState) DebugOverlayMode() VoxelRtDebugMode {
	if s == nil || s.RtApp == nil {
		return VoxelRtDebugModeOff
	}
	return VoxelRtDebugMode(s.RtApp.Camera.DebugMode)
}

func (s *VoxelRtState) SetDebugOverlayMode(mode VoxelRtDebugMode) {
	if s == nil || s.RtApp == nil {
		return
	}
	s.RtApp.Camera.DebugMode = uint32(mode)
}

func (s *VoxelRtState) CycleDebugOverlayMode() {
	if s == nil || s.RtApp == nil {
		return
	}
	s.RtApp.Camera.DebugMode = (s.RtApp.Camera.DebugMode + 1) % uint32(core.DebugModeCount)
}

func (s *VoxelRtState) SetLightingQualityPreset(preset LightingQualityPreset) {
	if s != nil && s.RtApp != nil {
		s.RtApp.QualityPreset = preset
	}
}

func (s *VoxelRtState) SetLightingQuality(cfg LightingQualityConfig) {
	if s != nil && s.RtApp != nil {
		s.RtApp.LightingQuality = cfg
	}
}

func (s *VoxelRtState) SetDebugGizmos(enabled bool) {
	if s != nil {
		s.HideDebugGizmos = !enabled
	}
}

func (s *VoxelRtState) IsDebugGizmosEnabled() bool {
	if s == nil {
		return false
	}
	return !s.HideDebugGizmos
}

func (s *VoxelRtState) GetVoxelObject(eid EntityId) *core.VoxelObject {
	if obj, ok := s.instanceMap[eid]; ok {
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
	obj := s.GetVoxelObject(eid)
	if obj == nil || obj.XBrickMap == nil {
		return
	}
	voxelSphereEditWithTransform(obj.XBrickMap, obj.Transform, worldCenter, radius, val)
}

func (s *VoxelRtState) IsEntityEmpty(eid EntityId) bool {
	if s == nil {
		return true
	}
	obj := s.GetVoxelObject(eid)
	if obj == nil || obj.XBrickMap == nil {
		return true
	}
	// Check internal counters or compute
	return obj.XBrickMap.GetVoxelCount() == 0
}

func (s *VoxelRtState) Project(pos mgl32.Vec3, camera *CameraComponent) (float32, float32, bool) {
	if s == nil || s.RtApp == nil || camera == nil {
		return 0, 0, false
	}

	camState := cameraStateFromComponent(camera)
	view := camState.GetViewMatrix()

	sw, sh := 1280, 720
	if s.RtApp.Window != nil {
		sw, sh = s.RtApp.Window.GetSize()
	}
	w, h := float32(sw), float32(sh)
	aspect := w / h
	if aspect == 0 {
		aspect = 1.0
	}
	proj := camState.ProjectionMatrix(aspect)
	vp := proj.Mul4(view)

	clip := vp.Mul4x1(pos.Vec4(1.0))

	// Clip points behind far or too close to near plane
	if clip.W() < 0.1 {
		return 0, 0, false
	}

	ndc := clip.Vec3().Mul(1.0 / clip.W())

	// NDC to Screen
	x := (ndc.X()*0.5 + 0.5) * w
	y := (1.0 - (ndc.Y()*0.5 + 0.5)) * h

	// Final bounds check
	if x < 0 || x > w || y < 0 || y > h {
		return x, y, false
	}

	return x, y, true
}

func (s *VoxelRtState) ScreenToWorldRay(mouseX, mouseY float64, camera *CameraComponent) (mgl32.Vec3, mgl32.Vec3) {
	if s == nil || s.RtApp == nil || s.RtApp.Window == nil || camera == nil {
		return mgl32.Vec3{}, mgl32.Vec3{}
	}

	sw, sh := s.RtApp.Window.GetSize()
	if sw == 0 || sh == 0 {
		return camera.Position, mgl32.Vec3{0, 0, -1}
	}

	camState := cameraStateFromComponent(camera)
	ray := camState.ScreenToWorldRay(mouseX, mouseY, sw, sh)
	return ray.Origin, ray.Direction
}

func cameraStateFromComponent(camera *CameraComponent) core.CameraState {
	camState := core.CameraState{}
	if camera == nil {
		return camState
	}
	camState.Position = camera.Position
	camState.Yaw = mgl32.DegToRad(camera.Yaw)
	camState.Pitch = mgl32.DegToRad(camera.Pitch)
	camState.Fov = camera.Fov
	camState.Near = camera.Near
	camState.Far = camera.Far
	return camState
}

func (s *VoxelRtState) Raycast(origin, dir mgl32.Vec3, tMax float32) RaycastHit {
	if s == nil || s.RtApp == nil {
		return RaycastHit{}
	}

	ray := core.Ray{Origin: origin, Direction: dir}
	res := s.RtApp.Scene.Raycast(ray, tMax)

	if res != nil {
		// Find EntityId for this object
		var hitEid EntityId = 0
		if eid, ok := s.objectToEntity[res.Object]; ok {
			hitEid = eid
		} else {
			// Fallback: search instanceMap and caVolumeMap (defensive)
			for eid, obj := range s.instanceMap {
				if obj == res.Object {
					hitEid = eid
					break
				}
			}
			if hitEid == 0 {
				for eid, obj := range s.caVolumeMap {
					if obj == res.Object {
						hitEid = eid
						break
					}
				}
			}
		}

		return RaycastHit{
			Hit:    true,
			T:      res.T,
			Pos:    res.Coord,
			Normal: res.Normal,
			Entity: hitEid,
		}
	}

	return RaycastHit{}
}

func (s *VoxelRtState) RaycastSubstepped(origin, dir mgl32.Vec3, distance float32, substeps int) RaycastHit {
	if substeps <= 1 {
		return s.Raycast(origin, dir, distance)
	}

	subDt := distance / float32(substeps)
	for i := 0; i < substeps; i++ {
		subOrigin := origin.Add(dir.Mul(float32(i) * subDt))
		hit := s.Raycast(subOrigin, dir, subDt)
		if hit.Hit {
			// Offset T by the distance already traveled
			hit.T += float32(i) * subDt
			return hit
		}
	}
	return RaycastHit{}
}

type Profiler struct {
	NavBakeTime   time.Duration
	EditTime      time.Duration
	StreamingTime time.Duration
	AABBTime      time.Duration
	RenderTime    time.Duration
}

func (p *Profiler) Reset() {
	p.NavBakeTime = 0
	p.EditTime = 0
	p.StreamingTime = 0
	p.AABBTime = 0
	p.RenderTime = 0
}
