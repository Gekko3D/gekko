package gekko

import (
	"sort"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

func (s *VoxelRtState) syncSkybox(cmd *Commands, time *Time) {
	s.RtApp.Profiler.BeginScope("Sync Skybox")
	defer s.RtApp.Profiler.EndScope("Sync Skybox")

	layersChanged := false
	currentLayers := make(map[EntityId]SkyboxLayerComponent)

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

	// Detect deletions
	if len(currentLayers) != len(s.skyboxLayers) {
		layersChanged = true
	}

	// Always rebuild if sun changed or any layer changed
	if layersChanged {
		s.skyboxLayers = currentLayers
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

	sunDir := [4]float32{s.SunDirection.X(), s.SunDirection.Y(), s.SunDirection.Z(), s.SunIntensity}
	s.RtApp.BufferManager.UpdateSkyboxGPU(uint32(width), uint32(height), gpuLayers, sunDir, smooth, s.RtApp.LightingPipeline, s.RtApp.StorageView)
}
