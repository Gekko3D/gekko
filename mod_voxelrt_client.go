package gekko

import (
	"math"
	"time"

	"github.com/go-gl/mathgl/mgl32"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func voxelSphereEditWithTransform(xbm *volume.XBrickMap, tr *core.Transform, worldCenter mgl32.Vec3, radius float32, val uint8) bool {
	if xbm == nil || tr == nil {
		return false
	}
	revision := xbm.Revision
	w2o := tr.WorldToObject()
	voxelCenter := w2o.Mul4x1(worldCenter.Vec4(1.0)).Vec3()

	scale := tr.Scale
	avgScale := (scale.X() + scale.Y() + scale.Z()) / 3.0
	if avgScale == 0 {
		avgScale = 1.0
	}
	volume.Sphere(xbm, voxelCenter, radius/avgScale, val)
	return xbm.Revision != revision
}

type RaycastHit struct {
	Hit          bool
	T            float32
	Pos          [3]int
	Normal       mgl32.Vec3
	Entity       EntityId
	PaletteIndex uint8
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
type VoxelRtDepthMode = core.DepthMode
type VoxelAmbientOcclusionMode = core.AmbientOcclusionMode
type VoxelRtDebugMode = core.DebugMode
type VoxelRtFeatureFlags = app_rt.AppFeatureFlags
type VoxelRtFeatureConfig = app_rt.AppFeatureConfig
type VoxelRtRenderFeature = app_rt.Feature
type VoxelRtRenderNode = app_rt.RenderNode
type VoxelRtRenderNodeSpec = app_rt.RenderNodeSpec

const (
	LightingQualityPerformance = core.LightingQualityPresetPerformance
	LightingQualityBalanced    = core.LightingQualityPresetBalanced
	LightingQualityQuality     = core.LightingQualityPresetQuality
	VoxelRtDepthModeStandard   = core.DepthModeStandard
	VoxelRtDepthModeReverseZ   = core.DepthModeReverseZ
	VoxelAOInherited           = core.AmbientOcclusionModeDefault
	VoxelAOEnabled             = core.AmbientOcclusionModeEnabled
	VoxelAODisabled            = core.AmbientOcclusionModeDisabled
	VoxelRtDebugModeOff        = core.DebugModeOff
	VoxelRtDebugModeScene      = core.DebugModeScene
)

func DefaultVoxelRtFeatureConfig() VoxelRtFeatureConfig {
	return app_rt.DefaultFeatureConfig()
}

func ParseVoxelRtDepthMode(raw string) (VoxelRtDepthMode, error) {
	return core.ParseDepthMode(raw)
}

type VoxelRtModule struct {
	WindowWidth      int
	WindowHeight     int
	WindowTitle      string
	DebugMode        bool
	DepthMode        VoxelRtDepthMode
	RenderMode       RenderMode
	QualityPreset    LightingQualityPreset
	LightingQuality  LightingQualityConfig
	OcclusionMode    core.OcclusionMode
	FontPath         string
	UIFontSize       float64
	FeatureConfig    *VoxelRtFeatureConfig
	BridgeFeatures   []VoxelRtBridgeFeatureRegistration
	RenderFeatures   []VoxelRtRenderFeature
	RenderGraphNodes []VoxelRtRenderNodeSpec
}

type VoxelRtState struct {
	RtApp                          *app_rt.App
	loadedModels                   map[AssetId]*core.VoxelObject
	instanceMap                    map[EntityId]*core.VoxelObject
	instanceGeometrySources        map[EntityId]*volume.XBrickMap
	instanceObjectScopedGeometry   map[EntityId]bool
	runtimeEditedVoxelEntities     map[EntityId]struct{}
	runtimeEditedVoxelRevisions    map[EntityId]uint64
	runtimeEditedVoxelEdits        map[EntityId]runtimeVoxelEdit
	nextRuntimeEditedVoxelRevision uint64
	entityLODSelections            map[EntityId]EntityLODSelection
	runtimeSprites                 []SpriteComponent
	runtimeDecals                  []DecalInstance
	lastMaterialKeys               map[*core.VoxelObject]materialTableCacheKey
	materialTableCache             map[materialTableCacheKey][]core.Material
	particlePools                  map[EntityId]*particlePool
	objectToEntity                 map[*core.VoxelObject]EntityId
	skyboxLayers                   map[EntityId]SkyboxLayerComponent // Stored values to detect changes
	skyboxSun                      SkyboxSunComponent
	SunDirection                   mgl32.Vec3
	SunIntensity                   float32
	lastParticleAtlas              AssetId
	lastSpriteAtlas                AssetId
	underwaterInput                app_rt.UnderwaterInput
	underwaterStrength             float32
	bridgeFeatures                 voxelRtBridgeRegistry
}

type runtimeVoxelEdit struct {
	Valid, Added bool
	Min, Max     mgl32.Vec3
}

func runtimeVoxelSphereEdit(center mgl32.Vec3, radius float32, val uint8) runtimeVoxelEdit {
	edit := runtimeVoxelEdit{Valid: true, Added: val != 0}
	if !edit.Added {
		return edit
	}
	extent := mgl32.Vec3{radius, radius, radius}
	edit.Min, edit.Max = center.Sub(extent), center.Add(extent)
	return edit
}

func (e *runtimeVoxelEdit) include(other runtimeVoxelEdit) {
	if !other.Valid {
		return
	}
	e.Valid = true
	if !other.Added {
		return
	}
	if !e.Added {
		e.Added, e.Min, e.Max = true, other.Min, other.Max
		return
	}
	for axis := 0; axis < 3; axis++ {
		e.Min[axis] = min(e.Min[axis], other.Min[axis])
		e.Max[axis] = max(e.Max[axis], other.Max[axis])
	}
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

func (s *VoxelRtState) RuntimeSpriteCount() int {
	if s == nil {
		return 0
	}
	return len(s.runtimeSprites)
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

func (s *VoxelRtState) DrawRect(x, y, w, h float32, color [4]float32) {
	if s != nil && s.RtApp != nil {
		s.RtApp.DrawRect(x, y, w, h, color)
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

func (s *VoxelRtState) GetTextAscent(scale float32) float32 {
	if s == nil || s.RtApp == nil {
		return 0
	}
	return s.RtApp.GetTextAscent(scale)
}

func (s *VoxelRtState) LightingQuality() LightingQualityConfig {
	if s == nil || s.RtApp == nil {
		return LightingQualityConfig{}
	}
	return s.RtApp.EffectiveLightingQuality()
}

func (s *VoxelRtState) SetDirectionalShadowSoftness(softness float32) {
	if s == nil || s.RtApp == nil {
		return
	}
	cfg := s.RtApp.LightingQuality
	cfg.Shadow.DirectionalShadowSoftness = softness
	s.RtApp.LightingQuality = cfg
}

func (s *VoxelRtState) SetSpotShadowSoftness(softness float32) {
	if s == nil || s.RtApp == nil {
		return
	}
	cfg := s.RtApp.LightingQuality
	cfg.Shadow.SpotShadowSoftness = softness
	s.RtApp.LightingQuality = cfg
}

func (s *VoxelRtState) DirectionalShadowSoftness() float32 {
	return s.LightingQuality().Shadow.DirectionalShadowSoftness
}

func (s *VoxelRtState) SpotShadowSoftness() float32 {
	return s.LightingQuality().Shadow.SpotShadowSoftness
}

func (s *VoxelRtState) GetVoxelObject(eid EntityId) *core.VoxelObject {
	if obj, ok := s.instanceMap[eid]; ok {
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
	if voxelSphereEditWithTransform(obj.XBrickMap, obj.Transform, worldCenter, radius, val) {
		s.markRuntimeEditedVoxelEntity(eid, runtimeVoxelSphereEdit(worldCenter, radius, val))
	}
}

func (s *VoxelRtState) markRuntimeEditedVoxelEntity(eid EntityId, edits ...runtimeVoxelEdit) {
	if s == nil {
		return
	}
	if s.runtimeEditedVoxelEntities == nil {
		s.runtimeEditedVoxelEntities = make(map[EntityId]struct{})
	}
	if s.runtimeEditedVoxelRevisions == nil {
		s.runtimeEditedVoxelRevisions = make(map[EntityId]uint64)
	}
	if s.runtimeEditedVoxelEdits == nil {
		s.runtimeEditedVoxelEdits = make(map[EntityId]runtimeVoxelEdit)
	}
	edit := s.runtimeEditedVoxelEdits[eid]
	for _, next := range edits {
		edit.include(next)
	}
	s.runtimeEditedVoxelEdits[eid] = edit
	s.nextRuntimeEditedVoxelRevision++
	s.runtimeEditedVoxelEntities[eid] = struct{}{}
	s.runtimeEditedVoxelRevisions[eid] = s.nextRuntimeEditedVoxelRevision
}

func (s *VoxelRtState) clearRuntimeEditedVoxelEntity(eid EntityId) {
	if s == nil || s.runtimeEditedVoxelEntities == nil {
		return
	}
	delete(s.runtimeEditedVoxelEntities, eid)
	if s.runtimeEditedVoxelRevisions != nil {
		delete(s.runtimeEditedVoxelRevisions, eid)
	}
	delete(s.runtimeEditedVoxelEdits, eid)
}

func (s *VoxelRtState) runtimeEditedVoxelEntity(eid EntityId) bool {
	if s == nil || s.runtimeEditedVoxelEntities == nil {
		return false
	}
	_, ok := s.runtimeEditedVoxelEntities[eid]
	return ok
}

func (s *VoxelRtState) runtimeEditedVoxelRevision(eid EntityId) (uint64, bool) {
	if s == nil || s.runtimeEditedVoxelRevisions == nil {
		return 0, false
	}
	revision, ok := s.runtimeEditedVoxelRevisions[eid]
	return revision, ok
}

func (s *VoxelRtState) runtimeEditedVoxelEdit(eid EntityId) (uint64, runtimeVoxelEdit, bool) {
	revision, ok := s.runtimeEditedVoxelRevision(eid)
	if !ok {
		return 0, runtimeVoxelEdit{}, false
	}
	return revision, s.runtimeEditedVoxelEdits[eid], true
}

func (s *VoxelRtState) clearRuntimeEditedVoxelEdit(eid EntityId, revision uint64) {
	if current, ok := s.runtimeEditedVoxelRevision(eid); ok && current == revision {
		delete(s.runtimeEditedVoxelEdits, eid)
	}
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
	if !camState.ClipPointVisible(clip) {
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
	camState.LookAt = camera.LookAt
	camState.Up = camera.Up
	camState.Yaw = mgl32.DegToRad(camera.Yaw)
	camState.Pitch = mgl32.DegToRad(camera.Pitch)
	camState.Fov = camera.Fov
	camState.Near = camera.Near
	camState.Far = camera.Far
	camState.DepthMode = camera.DepthMode.Normalized()
	return camState
}

func (s *VoxelRtState) Raycast(origin, dir mgl32.Vec3, tMax float32) RaycastHit {
	return s.RaycastFiltered(origin, dir, tMax, nil)
}

func (s *VoxelRtState) RaycastFiltered(origin, dir mgl32.Vec3, tMax float32, acceptEntity func(EntityId, bool) bool) RaycastHit {
	if s == nil || s.RtApp == nil {
		return RaycastHit{}
	}

	ray := core.Ray{Origin: origin, Direction: dir}
	res := s.RtApp.Scene.RaycastFiltered(ray, tMax, func(obj *core.VoxelObject) bool {
		if acceptEntity == nil {
			return true
		}
		eid, ok := s.entityForVoxelObject(obj)
		return acceptEntity(eid, ok)
	})

	if res != nil {
		hitEid, _ := s.entityForVoxelObject(res.Object)

		paletteIndex := uint8(0)
		if res.Object != nil && res.Object.XBrickMap != nil {
			_, paletteIndex = res.Object.XBrickMap.GetVoxel(res.Coord[0], res.Coord[1], res.Coord[2])
		}

		return RaycastHit{
			Hit:          true,
			T:            res.T,
			Pos:          res.Coord,
			Normal:       res.Normal,
			Entity:       hitEid,
			PaletteIndex: paletteIndex,
		}
	}

	return RaycastHit{}
}

// RaycastVoxelExit finds where a ray that entered one voxel object returns to
// empty space. The returned distance is measured in world units from entry.
func (s *VoxelRtState) RaycastVoxelExit(entity EntityId, entry, direction mgl32.Vec3, maxDistance float32) (mgl32.Vec3, float32, bool) {
	if s == nil || direction.LenSqr() <= 1e-8 || maxDistance <= 0 {
		return mgl32.Vec3{}, 0, false
	}
	obj := s.GetVoxelObject(entity)
	if obj == nil || obj.XBrickMap == nil || obj.Transform == nil {
		return mgl32.Vec3{}, 0, false
	}
	direction = direction.Normalize()
	worldToObject := obj.Transform.WorldToObject()
	localEntry := worldToObject.Mul4x1(entry.Vec4(1)).Vec3()
	localDirection := worldToObject.Mul4x1(direction.Vec4(0)).Vec3()
	localPerWorldUnit := localDirection.Len()
	if localPerWorldUnit <= 1e-8 {
		return mgl32.Vec3{}, 0, false
	}
	// ponytail: quarter-voxel stepping is sufficient for rare penetration shots;
	// replace it with empty-boundary DDA only if profiling makes this measurable.
	step := min(maxDistance, 0.25/localPerWorldUnit)
	solidAt := func(distance float32) bool {
		point := localEntry.Add(localDirection.Mul(distance))
		solid, _ := obj.XBrickMap.GetVoxel(
			int(math.Floor(float64(point.X()))),
			int(math.Floor(float64(point.Y()))),
			int(math.Floor(float64(point.Z()))),
		)
		return solid
	}

	enteredSolid := false
	lastSolidDistance := float32(0)
	for distance := step; ; distance = min(maxDistance, distance+step) {
		if solidAt(distance) {
			enteredSolid = true
			lastSolidDistance = distance
		} else if enteredSolid {
			low, high := lastSolidDistance, distance
			for range 8 {
				mid := (low + high) * 0.5
				if solidAt(mid) {
					low = mid
				} else {
					high = mid
				}
			}
			return entry.Add(direction.Mul(high)), high, true
		}
		if distance >= maxDistance {
			break
		}
	}
	return mgl32.Vec3{}, 0, false
}

func (s *VoxelRtState) entityForVoxelObject(obj *core.VoxelObject) (EntityId, bool) {
	if s == nil || obj == nil {
		return 0, false
	}
	if eid, ok := s.objectToEntity[obj]; ok {
		return eid, true
	}
	for eid, candidate := range s.instanceMap {
		if candidate == obj {
			return eid, true
		}
	}
	return 0, false
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
	EditTime      time.Duration
	StreamingTime time.Duration
	AABBTime      time.Duration
	RenderTime    time.Duration
}

func (p *Profiler) Reset() {
	p.EditTime = 0
	p.StreamingTime = 0
	p.AABBTime = 0
	p.RenderTime = 0
}
