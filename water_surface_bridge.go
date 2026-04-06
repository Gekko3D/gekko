package gekko

import (
	"sort"

	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

func buildWaterSurfaceHosts(cmd *Commands) []gpu_rt.WaterSurfaceHost {
	if cmd == nil {
		return nil
	}
	hosts := make([]gpu_rt.WaterSurfaceHost, 0, 4)
	MakeQuery2[TransformComponent, WaterSurfaceComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, water *WaterSurfaceComponent) bool {
		if water == nil || tr == nil || !water.Enabled() {
			return true
		}
		hosts = append(hosts, gpu_rt.WaterSurfaceHost{
			EntityID:        uint32(eid),
			Position:        water.WorldCenter(tr),
			HalfExtents:     water.WorldHalfExtents(tr),
			Depth:           water.WorldDepth(tr),
			Color:           water.NormalizedColor(),
			AbsorptionColor: water.NormalizedAbsorptionColor(),
			Opacity:         water.NormalizedOpacity(),
			Roughness:       water.NormalizedRoughness(),
			Refraction:      water.NormalizedRefraction(),
			FlowDirection:   water.NormalizedFlowDirection(),
			FlowSpeed:       water.NormalizedFlowSpeed(),
			WaveAmplitude:   water.NormalizedWaveAmplitude(),
		})
		return true
	})
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].EntityID < hosts[j].EntityID
	})
	return hosts
}
