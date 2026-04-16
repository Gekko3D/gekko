package gekko

import "github.com/go-gl/mathgl/mgl32"

type WaterBodyMode string

const (
	WaterBodyModeExplicitRect WaterBodyMode = "ExplicitRect"
	WaterBodyModeFitBounds    WaterBodyMode = "FitBounds"
)

type WaterFitSource string

const (
	WaterFitSourceStaticCollider WaterFitSource = "StaticCollider"
	WaterFitSourceVoxelOccupancy WaterFitSource = "VoxelOccupancy"
)

type WaterFitSourcePriority string

const (
	WaterFitSourcePriorityColliderThenVoxel WaterFitSourcePriority = "ColliderThenVoxel"
)

type WaterPatchKind string

const (
	WaterPatchKindSurface WaterPatchKind = "Surface"
	WaterPatchKindSkirt   WaterPatchKind = "Skirt"
)

type WaterBodyResolutionStatus string

const (
	WaterBodyResolutionStatusUnresolved       WaterBodyResolutionStatus = "Unresolved"
	WaterBodyResolutionStatusResolved         WaterBodyResolutionStatus = "Resolved"
	WaterBodyResolutionStatusFallbackExplicit WaterBodyResolutionStatus = "FallbackExplicit"
	WaterBodyResolutionStatusFailed           WaterBodyResolutionStatus = "ResolutionFailed"
)

const (
	DefaultWaterBodyInset         float32 = 0.08
	DefaultWaterBodyOverlap       float32 = 0.05
	DefaultWaterBodyMinCellSize   float32 = 0.2
	DefaultWaterBodyMaxPatchCount uint32  = 16
)

// WaterBodyComponent is the public authored API for water bodies. V1 supports
// explicit rectangles or deterministic fit-bounds authoring that will resolve
// into one or more axis-aligned rectangular runtime patches.
type WaterBodyComponent struct {
	Disabled bool

	Mode WaterBodyMode

	SurfaceY float32
	Depth    float32

	RectHalfExtents   [2]float32
	BoundsCenter      mgl32.Vec3
	BoundsHalfExtents mgl32.Vec3

	Inset       float32
	Overlap     float32
	MinCellSize float32

	SourceTag string

	EnableSkirt   *bool
	MaxPatchCount uint32
	DebugName     string

	Color           [3]float32
	AbsorptionColor [3]float32

	Opacity    float32
	Roughness  float32
	Refraction float32

	FlowDirection [2]float32
	FlowSpeed     float32
	WaveAmplitude float32
}

func (w *WaterBodyComponent) Enabled() bool {
	return w != nil && !w.Disabled && w.NormalizedDepth() > 0 && w.NormalizedSurfaceY() == w.NormalizedSurfaceY() && w.HasValidShapeForMode()
}

func (w *WaterBodyComponent) NormalizedMode() WaterBodyMode {
	if w == nil {
		return WaterBodyModeExplicitRect
	}
	switch w.Mode {
	case WaterBodyModeExplicitRect, WaterBodyModeFitBounds:
		return w.Mode
	}
	if w.RectHalfExtents[0] > 0 && w.RectHalfExtents[1] > 0 {
		return WaterBodyModeExplicitRect
	}
	if w.BoundsHalfExtents.X() > 0 && w.BoundsHalfExtents.Y() > 0 && w.BoundsHalfExtents.Z() > 0 {
		return WaterBodyModeFitBounds
	}
	return WaterBodyModeExplicitRect
}

func (w *WaterBodyComponent) NormalizedSurfaceY() float32 {
	if w == nil {
		return 0
	}
	return w.SurfaceY
}

func (w *WaterBodyComponent) NormalizedDepth() float32 {
	if w == nil || w.Depth <= 0 {
		return 0
	}
	return w.Depth
}

func (w *WaterBodyComponent) NormalizedRectHalfExtents() [2]float32 {
	if w == nil {
		return [2]float32{}
	}
	ext := w.RectHalfExtents
	if ext[0] < 0 {
		ext[0] = 0
	}
	if ext[1] < 0 {
		ext[1] = 0
	}
	return ext
}

func (w *WaterBodyComponent) NormalizedBoundsHalfExtents() mgl32.Vec3 {
	if w == nil {
		return mgl32.Vec3{}
	}
	ext := w.BoundsHalfExtents
	if ext[0] < 0 {
		ext[0] = 0
	}
	if ext[1] < 0 {
		ext[1] = 0
	}
	if ext[2] < 0 {
		ext[2] = 0
	}
	return ext
}

func (w *WaterBodyComponent) NormalizedBoundsCenter() mgl32.Vec3 {
	if w == nil {
		return mgl32.Vec3{}
	}
	return w.BoundsCenter
}

func (w *WaterBodyComponent) NormalizedInset() float32 {
	if w == nil || w.Inset < 0 {
		return DefaultWaterBodyInset
	}
	return w.Inset
}

func (w *WaterBodyComponent) NormalizedOverlap() float32 {
	if w == nil || w.Overlap < 0 {
		return DefaultWaterBodyOverlap
	}
	return w.Overlap
}

func (w *WaterBodyComponent) NormalizedMinCellSize() float32 {
	if w == nil || w.MinCellSize <= 0 {
		return DefaultWaterBodyMinCellSize
	}
	return w.MinCellSize
}

func (w *WaterBodyComponent) NormalizedEnableSkirt() bool {
	if w == nil || w.EnableSkirt == nil {
		return true
	}
	return *w.EnableSkirt
}

func (w *WaterBodyComponent) NormalizedMaxPatchCount() uint32 {
	if w == nil || w.MaxPatchCount == 0 {
		return DefaultWaterBodyMaxPatchCount
	}
	return w.MaxPatchCount
}

func (w *WaterBodyComponent) NormalizedColor() [3]float32 {
	if w == nil {
		return (&WaterSurfaceComponent{}).NormalizedColor()
	}
	return (&WaterSurfaceComponent{Color: w.Color}).NormalizedColor()
}

func (w *WaterBodyComponent) NormalizedAbsorptionColor() [3]float32 {
	if w == nil {
		return (&WaterSurfaceComponent{}).NormalizedAbsorptionColor()
	}
	return (&WaterSurfaceComponent{AbsorptionColor: w.AbsorptionColor}).NormalizedAbsorptionColor()
}

func (w *WaterBodyComponent) NormalizedOpacity() float32 {
	if w == nil {
		return (&WaterSurfaceComponent{}).NormalizedOpacity()
	}
	return (&WaterSurfaceComponent{Opacity: w.Opacity}).NormalizedOpacity()
}

func (w *WaterBodyComponent) NormalizedRoughness() float32 {
	if w == nil {
		return (&WaterSurfaceComponent{}).NormalizedRoughness()
	}
	return (&WaterSurfaceComponent{Roughness: w.Roughness}).NormalizedRoughness()
}

func (w *WaterBodyComponent) NormalizedRefraction() float32 {
	if w == nil {
		return (&WaterSurfaceComponent{}).NormalizedRefraction()
	}
	return (&WaterSurfaceComponent{Refraction: w.Refraction}).NormalizedRefraction()
}

func (w *WaterBodyComponent) NormalizedFlowDirection() [2]float32 {
	if w == nil {
		return (&WaterSurfaceComponent{}).NormalizedFlowDirection()
	}
	return (&WaterSurfaceComponent{FlowDirection: w.FlowDirection}).NormalizedFlowDirection()
}

func (w *WaterBodyComponent) NormalizedFlowSpeed() float32 {
	if w == nil {
		return (&WaterSurfaceComponent{}).NormalizedFlowSpeed()
	}
	return (&WaterSurfaceComponent{FlowSpeed: w.FlowSpeed}).NormalizedFlowSpeed()
}

func (w *WaterBodyComponent) NormalizedWaveAmplitude() float32 {
	if w == nil {
		return (&WaterSurfaceComponent{}).NormalizedWaveAmplitude()
	}
	return (&WaterSurfaceComponent{WaveAmplitude: w.WaveAmplitude}).NormalizedWaveAmplitude()
}

func (w *WaterBodyComponent) HasValidShapeForMode() bool {
	switch w.NormalizedMode() {
	case WaterBodyModeFitBounds:
		ext := w.NormalizedBoundsHalfExtents()
		return ext.X() > 0 && ext.Y() > 0 && ext.Z() > 0
	default:
		ext := w.NormalizedRectHalfExtents()
		return ext[0] > 0 && ext[1] > 0
	}
}

func (w *WaterBodyComponent) ValidationIssues() []string {
	if w == nil {
		return []string{"water body component is nil"}
	}
	var issues []string
	if w.Disabled {
		return nil
	}
	if w.NormalizedDepth() <= 0 {
		issues = append(issues, "depth must be greater than zero")
	}
	switch w.NormalizedMode() {
	case WaterBodyModeFitBounds:
		ext := w.NormalizedBoundsHalfExtents()
		if ext.X() <= 0 || ext.Y() <= 0 || ext.Z() <= 0 {
			issues = append(issues, "fit-bounds mode requires positive bounds half extents")
		}
	default:
		ext := w.NormalizedRectHalfExtents()
		if ext[0] <= 0 || ext[1] <= 0 {
			issues = append(issues, "explicit-rect mode requires positive rect half extents")
		}
	}
	if w.Inset < 0 {
		issues = append(issues, "inset must be greater than or equal to zero")
	}
	if w.Overlap < 0 {
		issues = append(issues, "overlap must be greater than or equal to zero")
	}
	if w.MinCellSize < 0 {
		issues = append(issues, "min cell size must be greater than or equal to zero")
	}
	return issues
}

type ResolvedWaterPatchComponent struct {
	Disabled bool

	Owner      EntityId
	PatchIndex uint32
	Kind       WaterPatchKind

	Center      mgl32.Vec3
	HalfExtents [2]float32
	Depth       float32

	Color           [3]float32
	AbsorptionColor [3]float32

	Opacity    float32
	Roughness  float32
	Refraction float32

	FlowDirection [2]float32
	FlowSpeed     float32
	WaveAmplitude float32

	Source       WaterFitSource
	DebugInset   float32
	DebugOverlap float32
}

func (p *ResolvedWaterPatchComponent) Enabled() bool {
	if p == nil || p.Disabled {
		return false
	}
	if p.Owner == 0 {
		return false
	}
	if p.Kind != WaterPatchKindSurface && p.Kind != WaterPatchKindSkirt {
		return false
	}
	return p.HalfExtents[0] > 0 && p.HalfExtents[1] > 0 && p.Depth > 0
}

type WaterBodyResolvedRecord struct {
	Status         WaterBodyResolutionStatus
	PatchCount     uint32
	PrimarySource  WaterFitSource
	Warnings       []string
	FallbackReason string
}

type WaterBodyResolutionState struct {
	ByEntity             map[EntityId]WaterBodyResolvedRecord
	PatchEntitiesByOwner map[EntityId][]EntityId
}

func (s *WaterBodyResolutionState) ensureMaps() {
	if s.ByEntity == nil {
		s.ByEntity = make(map[EntityId]WaterBodyResolvedRecord)
	}
	if s.PatchEntitiesByOwner == nil {
		s.PatchEntitiesByOwner = make(map[EntityId][]EntityId)
	}
}
