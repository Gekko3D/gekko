package gekko

import (
	"fmt"
	"os"
	"sort"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
)

var waterSurfaceDebugPrinted bool

func buildWaterSurfaceInputs(cmd *Commands, interactions *WaterInteractionState) ([]app_rt.WaterSurfaceInput, []app_rt.WaterRippleInput) {
	if cmd == nil {
		return nil, nil
	}

	hosts := make([]app_rt.WaterSurfaceInput, 0, 4)
	MakeQuery2[TransformComponent, WaterSurfaceComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, water *WaterSurfaceComponent) bool {
		if water == nil || tr == nil || !water.Enabled() {
			return true
		}
		hosts = append(hosts, app_rt.WaterSurfaceInput{
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
	MakeQuery2[TransformComponent, ResolvedWaterPatchComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, patch *ResolvedWaterPatchComponent) bool {
		if tr == nil || patch == nil || !patch.Enabled() || patch.Kind != WaterPatchKindSurface {
			return true
		}
		hosts = append(hosts, app_rt.WaterSurfaceInput{
			EntityID:        uint32(eid),
			Position:        patch.Center,
			HalfExtents:     patch.HalfExtents,
			Depth:           patch.Depth,
			Color:           patch.Color,
			AbsorptionColor: patch.AbsorptionColor,
			Opacity:         patch.Opacity,
			Roughness:       patch.Roughness,
			Refraction:      patch.Refraction,
			FlowDirection:   patch.FlowDirection,
			FlowSpeed:       patch.FlowSpeed,
			WaveAmplitude:   patch.WaveAmplitude,
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
	ripples := make([]app_rt.WaterRippleInput, 0, rippleCap)
	if interactions != nil {
		for _, ripple := range interactions.activeRipples {
			waterIndex, ok := indexByEntity[ripple.WaterEntity]
			if !ok || ripple.Age >= ripple.Lifetime {
				continue
			}
			ripples = append(ripples, app_rt.WaterRippleInput{
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

	debugWaterSurfaceInputsOnce(hosts, ripples)
	return hosts, ripples
}

func debugWaterSurfaceInputsOnce(hosts []app_rt.WaterSurfaceInput, ripples []app_rt.WaterRippleInput) {
	if waterSurfaceDebugPrinted || os.Getenv("GEKKO_DEBUG_WATER_HOSTS") == "" {
		return
	}
	waterSurfaceDebugPrinted = true
	var largest app_rt.WaterSurfaceInput
	var largestArea float32
	for _, host := range hosts {
		area := host.HalfExtents[0] * host.HalfExtents[1] * 4
		if area > largestArea {
			largestArea = area
			largest = host
		}
	}
	fmt.Printf("DEBUG water hosts: count=%d ripples=%d largest_center=(%.3f, %.3f, %.3f) largest_half_extents=(%.3f, %.3f) largest_depth=%.3f\n",
		len(hosts),
		len(ripples),
		largest.Position.X(),
		largest.Position.Y(),
		largest.Position.Z(),
		largest.HalfExtents[0],
		largest.HalfExtents[1],
		largest.Depth,
	)
}
