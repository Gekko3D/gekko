package gekko

import (
	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

// DebugRay holds transient debug ray visualization parameters.
type DebugRay struct {
	Origin   mgl32.Vec3
	Dir      mgl32.Vec3
	Color    [4]float32
	Duration float32
}

// RenderMode mirrors the runtime render mode enumeration.
type RenderMode uint32

const (
	RenderModeLit RenderMode = iota
	RenderModeAlbedo
	RenderModeNormals
	RenderModeGBuffer
)

// VoxelRtState aggregates runtime-side state exposed to ECS systems.
type VoxelRtState struct {
	RtApp         *app_rt.App
	loadedModels  map[AssetId]*core.VoxelObject
	instanceMap   map[EntityId]*core.VoxelObject
	particlePools map[EntityId]*particlePool
	caVolumeMap   map[EntityId]*core.VoxelObject
	worldMap      map[EntityId]*core.VoxelObject

	// Debug rays
	debugRays []DebugRay

	// Splitting queue
	splitQueue map[EntityId]bool
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

func (s *VoxelRtState) CycleRenderMode() {
	if s != nil && s.RtApp != nil {
		s.RtApp.RenderMode = (s.RtApp.RenderMode + 1) % 4
	}
}