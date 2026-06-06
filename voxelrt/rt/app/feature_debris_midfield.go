package app

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

type DebrisMidfieldFeature struct{}

type DebrisMidfieldResources struct {
	Pipeline *wgpu.RenderPipeline
}

type DebrisMidfieldInput struct {
	BandID               string
	CellID               string
	AsteroidID           string
	RadialIndex          int32
	AngularIndex         int32
	VerticalIndex        int32
	PositionViewSpace    mgl32.Vec3
	PlaneNormalViewSpace mgl32.Vec3
	InnerRadiusMeters    float32
	OuterRadiusMeters    float32
	Seed                 uint32
	Tint                 [3]float32
	Opacity              float32
	DensityScale         float32
	ApproachFade         float32
	DistanceMeters       float32
	GapInnerRadius       float32
	GapOuterRadius       float32
	LightDirViewSpace    mgl32.Vec3
	ActiveHandoff        bool
	HandoffExact         bool
	HandoffRadiusMeters  float32
}

func (f *DebrisMidfieldFeature) Name() string {
	return "debris_midfield"
}

func (f *DebrisMidfieldFeature) GraphNodeNames() []string {
	return []string{RenderNodeCoreAccumulation}
}

func (f *DebrisMidfieldFeature) GraphPassStages() []FeaturePassStage {
	return []FeaturePassStage{FeaturePassStageAccumulation}
}

func (f *DebrisMidfieldFeature) Enabled(a *App) bool {
	if a == nil {
		return false
	}
	return a.FeatureConfig.Defaults.Transparency
}

func (f *DebrisMidfieldFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupDebrisMidfieldPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *DebrisMidfieldFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupDebrisMidfieldPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *DebrisMidfieldFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *DebrisMidfieldFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.DebrisMidfieldBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *DebrisMidfieldFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *DebrisMidfieldFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.DebrisMidfieldResources = nil
}

func (f *DebrisMidfieldFeature) HasPassStage(a *App, stage FeaturePassStage) bool {
	pipeline := a.debrisMidfieldPipeline()
	return stage == FeaturePassStageAccumulation &&
		a != nil &&
		a.BufferManager != nil &&
		pipeline != nil &&
		a.BufferManager.DepthView != nil &&
		a.BufferManager.PlanetDepthView != nil &&
		a.BufferManager.DebrisMidfieldCount > 0 &&
		a.BufferManager.DebrisMidfieldBG0 != nil &&
		a.BufferManager.DebrisMidfieldBG1 != nil &&
		a.BufferManager.DebrisMidfieldBG2 != nil
}

func (f *DebrisMidfieldFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	if a == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	pipeline := a.debrisMidfieldPipeline()
	if pipeline == nil || a.BufferManager.DebrisMidfieldCount == 0 || a.BufferManager.DepthView == nil || a.BufferManager.PlanetDepthView == nil {
		return nil
	}
	if a.BufferManager.DebrisMidfieldBG0 == nil || a.BufferManager.DebrisMidfieldBG1 == nil || a.BufferManager.DebrisMidfieldBG2 == nil {
		return nil
	}

	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, a.BufferManager.DebrisMidfieldBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.DebrisMidfieldBG1, nil)
	pass.SetBindGroup(2, a.BufferManager.DebrisMidfieldBG2, nil)

	pass.Draw(6, a.BufferManager.DebrisMidfieldCount*64, 0, 0)
	return nil
}

func (f *DebrisMidfieldFeature) rebuildBindGroups(a *App) {
	pipeline := a.debrisMidfieldPipeline()
	if pipeline == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.CreateDebrisMidfieldBindGroups(pipeline)
}

func (a *App) debrisMidfieldPipeline() *wgpu.RenderPipeline {
	if a == nil || a.DebrisMidfieldResources == nil {
		return nil
	}
	return a.DebrisMidfieldResources.Pipeline
}

func (a *App) ApplyDebrisMidfieldInput(cells []DebrisMidfieldInput) {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.UpdateDebrisMidfieldCells(debrisMidfieldGPUHosts(cells))
}

func (a *App) ClearDebrisMidfieldInput() {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.DebrisMidfieldCount = 0
}

func debrisMidfieldGPUHosts(cells []DebrisMidfieldInput) []gpu.DebrisMidfieldHost {
	hosts := make([]gpu.DebrisMidfieldHost, 0, len(cells))
	for _, cell := range cells {
		hosts = append(hosts, gpu.DebrisMidfieldHost{
			BandID:               cell.BandID,
			CellID:               cell.CellID,
			AsteroidID:           cell.AsteroidID,
			RadialIndex:          cell.RadialIndex,
			AngularIndex:         cell.AngularIndex,
			VerticalIndex:        cell.VerticalIndex,
			PositionViewSpace:    cell.PositionViewSpace,
			PlaneNormalViewSpace: cell.PlaneNormalViewSpace,
			InnerRadiusMeters:    cell.InnerRadiusMeters,
			OuterRadiusMeters:    cell.OuterRadiusMeters,
			Seed:                 cell.Seed,
			Tint:                 cell.Tint,
			Opacity:              cell.Opacity,
			DensityScale:         cell.DensityScale,
			ApproachFade:         cell.ApproachFade,
			DistanceMeters:       cell.DistanceMeters,
			GapInnerRadius:       cell.GapInnerRadius,
			GapOuterRadius:       cell.GapOuterRadius,
			LightDirViewSpace:    cell.LightDirViewSpace,
			ActiveHandoff:        cell.ActiveHandoff,
			HandoffExact:         cell.HandoffExact,
			HandoffRadiusMeters:  cell.HandoffRadiusMeters,
		})
	}
	return hosts
}
