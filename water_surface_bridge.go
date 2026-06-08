package gekko

import (
	"fmt"
	"os"
	"sort"
	"strings"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
)

var waterSurfaceDebugPrinted bool

const (
	waterContinuityMinXMask uint32  = 1 << 0
	waterContinuityMaxXMask uint32  = 1 << 1
	waterContinuityMinZMask uint32  = 1 << 2
	waterContinuityMaxZMask uint32  = 1 << 3
	waterContinuityEpsilon  float32 = 0.025
	waterShapeKindBox       uint32  = 0
	waterShapeKindFootprint uint32  = 1
)

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
			EntityID:             uint32(eid),
			ContinuityGroup:      strings.TrimSpace(water.ContinuityGroup),
			ShapeKind:            waterShapeKindForGroup(water.ContinuityGroup),
			Position:             water.WorldCenter(tr),
			HalfExtents:          water.WorldHalfExtents(tr),
			Depth:                water.WorldDepth(tr),
			Color:                water.NormalizedColor(),
			AbsorptionColor:      water.NormalizedAbsorptionColor(),
			Opacity:              water.NormalizedOpacity(),
			Roughness:            water.NormalizedRoughness(),
			Refraction:           water.NormalizedRefraction(),
			DirectLightOcclusion: water.NormalizedDirectLightOcclusion(),
			FlowDirection:        water.NormalizedFlowDirection(),
			FlowSpeed:            water.NormalizedFlowSpeed(),
			WaveAmplitude:        water.NormalizedWaveAmplitude(),
			VisualCellSize:       water.NormalizedVisualCellSize(),
		})
		return true
	})
	MakeQuery2[TransformComponent, ResolvedWaterPatchComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, patch *ResolvedWaterPatchComponent) bool {
		if tr == nil || patch == nil || !patch.Enabled() || patch.Kind != WaterPatchKindSurface {
			return true
		}
		hosts = append(hosts, app_rt.WaterSurfaceInput{
			EntityID:             uint32(eid),
			ContinuityGroup:      strings.TrimSpace(patch.ContinuityGroup),
			ShapeKind:            waterShapeKindForGroup(patch.ContinuityGroup),
			Position:             patch.Center,
			HalfExtents:          patch.HalfExtents,
			Depth:                patch.Depth,
			Color:                patch.Color,
			AbsorptionColor:      patch.AbsorptionColor,
			Opacity:              patch.Opacity,
			Roughness:            patch.Roughness,
			Refraction:           patch.Refraction,
			DirectLightOcclusion: patch.DirectLightOcclusion,
			FlowDirection:        patch.FlowDirection,
			FlowSpeed:            patch.FlowSpeed,
			WaveAmplitude:        patch.WaveAmplitude,
			VisualCellSize:       patch.VisualCellSize,
		})
		return true
	})
	applyWaterContinuityEdgeMasks(hosts)
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
				WaterIndex:         waterIndex,
				Position:           ripple.Position,
				Strength:           ripple.Strength,
				Age:                ripple.Age,
				Lifetime:           ripple.Lifetime,
				Radius:             ripple.Radius,
				HorizontalVelocity: ripple.HorizontalVelocity,
				Foam:               ripple.Foam,
				DisturbanceKind:    uint32(ripple.Kind),
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

func waterShapeKindForGroup(group string) uint32 {
	if strings.TrimSpace(group) == "" {
		return waterShapeKindBox
	}
	return waterShapeKindFootprint
}

func applyWaterContinuityEdgeMasks(hosts []app_rt.WaterSurfaceInput) {
	for i := range hosts {
		if strings.TrimSpace(hosts[i].ContinuityGroup) == "" {
			continue
		}
		for j := range hosts {
			if i == j || strings.TrimSpace(hosts[j].ContinuityGroup) != strings.TrimSpace(hosts[i].ContinuityGroup) {
				continue
			}
			if absWaterBridgeFloat(hosts[i].Position.Y()-hosts[j].Position.Y()) > waterContinuityEpsilon {
				continue
			}
			hosts[i].EdgeMask |= waterContinuityMaskCoveredBy(hosts[i], hosts[j])
		}
	}
}

func waterContinuityMaskCoveredBy(a, b app_rt.WaterSurfaceInput) uint32 {
	aMinX, aMaxX := waterHostRangeX(a)
	aMinZ, aMaxZ := waterHostRangeZ(a)
	bMinX, bMaxX := waterHostRangeX(b)
	bMinZ, bMaxZ := waterHostRangeZ(b)

	var mask uint32
	if waterBoundaryCovered(aMinX, bMinX, bMaxX) && waterRangesOverlapEnough(aMinZ, aMaxZ, bMinZ, bMaxZ) {
		mask |= waterContinuityMinXMask
	}
	if waterBoundaryCovered(aMaxX, bMinX, bMaxX) && waterRangesOverlapEnough(aMinZ, aMaxZ, bMinZ, bMaxZ) {
		mask |= waterContinuityMaxXMask
	}
	if waterBoundaryCovered(aMinZ, bMinZ, bMaxZ) && waterRangesOverlapEnough(aMinX, aMaxX, bMinX, bMaxX) {
		mask |= waterContinuityMinZMask
	}
	if waterBoundaryCovered(aMaxZ, bMinZ, bMaxZ) && waterRangesOverlapEnough(aMinX, aMaxX, bMinX, bMaxX) {
		mask |= waterContinuityMaxZMask
	}
	return mask
}

func waterBoundaryCovered(edge, otherMin, otherMax float32) bool {
	return edge >= otherMin-waterContinuityEpsilon && edge <= otherMax+waterContinuityEpsilon
}

func waterRangesOverlapEnough(aMin, aMax, bMin, bMax float32) bool {
	return minWaterBridgeFloat(aMax, bMax)-maxWaterBridgeFloat(aMin, bMin) > waterContinuityEpsilon
}

func waterHostRangeX(host app_rt.WaterSurfaceInput) (float32, float32) {
	return host.Position.X() - host.HalfExtents[0], host.Position.X() + host.HalfExtents[0]
}

func waterHostRangeZ(host app_rt.WaterSurfaceInput) (float32, float32) {
	return host.Position.Z() - host.HalfExtents[1], host.Position.Z() + host.HalfExtents[1]
}

func minWaterBridgeFloat(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxWaterBridgeFloat(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func absWaterBridgeFloat(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
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
