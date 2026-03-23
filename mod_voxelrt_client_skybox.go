package gekko

import (
	"sort"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

func (s *VoxelRtState) syncSkybox(cmd *Commands, time *Time) {
	s.RtApp.Profiler.BeginScope("Sync Skybox")
	defer s.RtApp.Profiler.EndScope("Sync Skybox")

	layersChanged := false
	currentLayers := make(map[EntityId]SkyboxLayerComponent)
	currentSun := SkyboxSunComponent{
		Direction:              s.SunDirection,
		Intensity:              s.SunIntensity,
		HaloColor:              mgl32.Vec3{1.0, 0.9, 0.7},
		CoreGlowStrength:       2.0,
		CoreGlowExponent:       1000.0,
		AtmosphereExponent:     100.0,
		AtmosphereGlowStrength: 0.5,
		DiskColor:              mgl32.Vec3{1.5, 1.4, 1.2},
		DiskStrength:           1.0,
		DiskStart:              0.9998,
		DiskEnd:                0.9999,
	}

	dt := float32(time.Dt)

	MakeQuery1[SkyboxLayerComponent](cmd).Map(func(eid EntityId, layer *SkyboxLayerComponent) bool {
		// Update animation offset
		if layer.WindSpeed.LenSqr() > 0 {
			layer.Offset = layer.Offset.Add(layer.WindSpeed.Mul(dt))
			// Always trigger rebuild for animated layers
			layersChanged = true
		}

		prev, exists := s.skyboxLayers[eid]
		if !exists || prev != *layer || layer._dirty {
			layersChanged = true
			layer._dirty = false
		}
		currentLayers[eid] = *layer
		return true
	})

	MakeQuery1[SkyboxSunComponent](cmd).Map(func(_ EntityId, sun *SkyboxSunComponent) bool {
		if sun != nil {
			currentSun = *sun
		}
		return false
	})

	// Detect deletions
	if len(currentLayers) != len(s.skyboxLayers) {
		layersChanged = true
	}
	if s.skyboxSun != currentSun {
		layersChanged = true
	}

	// Always rebuild if sun changed or any layer changed
	if layersChanged {
		s.skyboxLayers = currentLayers
		s.skyboxSun = currentSun
		s.rebuildSkybox()
	}
}

func (s *VoxelRtState) rebuildSkybox() {
	// Sort layers by priority
	type layerInfo struct {
		eid   EntityId
		layer SkyboxLayerComponent
	}
	sorted := make([]layerInfo, 0, len(s.skyboxLayers))
	for eid, l := range s.skyboxLayers {
		sorted = append(sorted, layerInfo{eid, l})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].layer.Priority < sorted[j].layer.Priority
	})

	if len(sorted) == 0 {
		return
	}

	width, height := 1024, 512
	for _, li := range sorted {
		if li.layer.Resolution[0] > 0 && li.layer.Resolution[1] > 0 {
			width, height = li.layer.Resolution[0], li.layer.Resolution[1]
			break
		}
	}

	gpuLayers := make([]gpu.GpuSkyboxLayer, 0, len(sorted))
	for _, li := range sorted {
		l := li.layer

		invert := uint32(0)
		if l.Invert {
			invert = 1
		}

		gpuLayers = append(gpuLayers, gpu.GpuSkyboxLayer{
			ColorA:      [4]float32{l.ColorA.X(), l.ColorA.Y(), l.ColorA.Z(), l.Threshold},
			ColorB:      [4]float32{l.ColorB.X(), l.ColorB.Y(), l.ColorB.Z(), l.Opacity},
			Offset:      [4]float32{l.Offset.X(), l.Offset.Y(), l.Offset.Z(), l.Scale},
			Persistence: l.Persistence,
			Lacunarity:  l.Lacunarity,
			Seed:        int32(l.Seed),
			Octaves:     int32(l.Octaves),
			BlendMode:   uint32(l.BlendMode),
			Invert:      invert,
			LayerType:   uint32(l.LayerType),
		})
	}

	smooth := true
	for _, li := range sorted {
		if !li.layer.Smooth {
			smooth = false
			break
		}
	}

	sunDir := [4]float32{s.skyboxSun.Direction.X(), s.skyboxSun.Direction.Y(), s.skyboxSun.Direction.Z(), s.skyboxSun.Intensity}
	sunColor := [4]float32{s.skyboxSun.HaloColor.X(), s.skyboxSun.HaloColor.Y(), s.skyboxSun.HaloColor.Z(), s.skyboxSun.CoreGlowStrength}
	sunParams := [4]float32{s.skyboxSun.CoreGlowExponent, s.skyboxSun.AtmosphereExponent, s.skyboxSun.AtmosphereGlowStrength, 0}
	diskColor := [4]float32{s.skyboxSun.DiskColor.X(), s.skyboxSun.DiskColor.Y(), s.skyboxSun.DiskColor.Z(), s.skyboxSun.DiskStrength}
	diskParams := [4]float32{s.skyboxSun.DiskStart, s.skyboxSun.DiskEnd, 0, 0}
	s.RtApp.BufferManager.UpdateSkyboxGPU(uint32(width), uint32(height), gpuLayers, sunDir, sunColor, sunParams, diskColor, diskParams, smooth, s.RtApp.LightingPipeline, s.RtApp.StorageView)
}
