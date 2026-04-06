package gekko

import (
	"sort"

	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

func buildWaterSurfaceHosts(cmd *Commands, interactions *WaterInteractionState) ([]gpu_rt.WaterSurfaceHost, []gpu_rt.WaterRippleHost) {
	if cmd == nil {
		return nil, nil
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

	indexByEntity := make(map[EntityId]uint32, len(hosts))
	for i := range hosts {
		indexByEntity[EntityId(hosts[i].EntityID)] = uint32(i)
	}

	rippleCap := 0
	if interactions != nil {
		rippleCap = len(interactions.activeRipples)
	}
	ripples := make([]gpu_rt.WaterRippleHost, 0, rippleCap)
	if interactions != nil {
		for _, ripple := range interactions.activeRipples {
			waterIndex, ok := indexByEntity[ripple.WaterEntity]
			if !ok || ripple.Age >= ripple.Lifetime {
				continue
			}
			ripples = append(ripples, gpu_rt.WaterRippleHost{
				WaterIndex: waterIndex,
				Position:   ripple.Position,
				Strength:   ripple.Strength,
				Age:        ripple.Age,
				Lifetime:   ripple.Lifetime,
			})
		}
		sort.Slice(ripples, func(i, j int) bool {
			if ripples[i].WaterIndex == ripples[j].WaterIndex {
				return ripples[i].Age < ripples[j].Age
			}
			return ripples[i].WaterIndex < ripples[j].WaterIndex
		})
	}

	return hosts, ripples
}
