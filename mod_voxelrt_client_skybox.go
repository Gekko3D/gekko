package gekko

import (
	"sort"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/go-gl/mathgl/mgl32"
)

func (s *VoxelRtState) syncSkybox(cmd *Commands, time *Time) {
	if s == nil || s.RtApp == nil {
		return
	}
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

	dt := float32(0)
	if time != nil {
		dt = float32(time.Dt)
	}

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
		if input, ok := buildSkyboxBridgeInput(s.skyboxLayers, s.skyboxSun); ok {
			s.RtApp.SetSkyboxInput(input)
		} else {
			s.RtApp.ClearSkyboxInput()
		}
	}
}

func (s *VoxelRtState) clearSkybox() {
	if s == nil {
		return
	}
	if s.skyboxLayers == nil {
		s.skyboxLayers = make(map[EntityId]SkyboxLayerComponent)
	} else {
		for eid := range s.skyboxLayers {
			delete(s.skyboxLayers, eid)
		}
	}
	s.skyboxSun = SkyboxSunComponent{}
	if s.RtApp != nil {
		s.RtApp.ClearSkyboxInput()
	}
}

func buildSkyboxBridgeInput(layerMap map[EntityId]SkyboxLayerComponent, sun SkyboxSunComponent) (app_rt.SkyboxInput, bool) {
	if len(layerMap) == 0 {
		return app_rt.SkyboxInput{}, false
	}
	sorted := make([]skyboxBridgeLayer, 0, len(layerMap))
	for eid, layer := range layerMap {
		sorted = append(sorted, skyboxBridgeLayer{entityID: eid, layer: layer})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].layer.Priority != sorted[j].layer.Priority {
			return sorted[i].layer.Priority < sorted[j].layer.Priority
		}
		return sorted[i].entityID < sorted[j].entityID
	})

	width, height := 1024, 512
	for _, li := range sorted {
		if li.layer.Resolution[0] > 0 && li.layer.Resolution[1] > 0 {
			width, height = li.layer.Resolution[0], li.layer.Resolution[1]
			break
		}
	}

	layerInputs := make([]app_rt.SkyboxLayerInput, 0, len(sorted))
	for _, li := range sorted {
		l := li.layer

		layerInputs = append(layerInputs, app_rt.SkyboxLayerInput{
			ColorA:      [3]float32{l.ColorA.X(), l.ColorA.Y(), l.ColorA.Z()},
			ColorB:      [3]float32{l.ColorB.X(), l.ColorB.Y(), l.ColorB.Z()},
			Offset:      [3]float32{l.Offset.X(), l.Offset.Y(), l.Offset.Z()},
			Threshold:   l.Threshold,
			Opacity:     l.Opacity,
			Scale:       l.Scale,
			Persistence: l.Persistence,
			Lacunarity:  l.Lacunarity,
			Seed:        int32(l.Seed),
			Octaves:     int32(l.Octaves),
			BlendMode:   uint32(l.BlendMode),
			Invert:      l.Invert,
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

	sunDir := [4]float32{sun.Direction.X(), sun.Direction.Y(), sun.Direction.Z(), sun.Intensity}
	sunColor := [4]float32{sun.HaloColor.X(), sun.HaloColor.Y(), sun.HaloColor.Z(), sun.CoreGlowStrength}
	sunParams := [4]float32{sun.CoreGlowExponent, sun.AtmosphereExponent, sun.AtmosphereGlowStrength, 0}
	diskColor := [4]float32{sun.DiskColor.X(), sun.DiskColor.Y(), sun.DiskColor.Z(), sun.DiskStrength}
	diskParams := [4]float32{sun.DiskStart, sun.DiskEnd, 0, 0}
	return app_rt.SkyboxInput{
		Width:      uint32(width),
		Height:     uint32(height),
		Layers:     layerInputs,
		SunDir:     sunDir,
		SunColor:   sunColor,
		SunParams:  sunParams,
		DiskColor:  diskColor,
		DiskParams: diskParams,
		Smooth:     smooth,
	}, true
}

type skyboxBridgeLayer struct {
	entityID EntityId
	layer    SkyboxLayerComponent
}
