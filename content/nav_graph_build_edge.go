package content

import (
	"fmt"
	"math"
)

const (
	NavSpanTransitionRejectedStep      = "step_delta_exceeds_limit"
	NavSpanTransitionRejectedHeadroom  = "insufficient_transition_headroom"
	NavSpanTransitionRejectedClearance = "insufficient_transition_clearance"
)

type NavSpanTransitionDiagnostic struct {
	Code string
	From uint32
	To   uint32
}

type NavSpanGraphBuildResult struct {
	Graph                 NavGraphTileDef
	SpanDiagnostics       []NavSpanProfileDiagnostic
	TransitionDiagnostics []NavSpanTransitionDiagnostic
}

// BuildNavSpanGraph filters source spans for profile, then connects supported
// spans in four-neighbor columns within one tile.
func BuildNavSpanGraph(source NavSourceTileDef, profile NavAgentProfileDef, voxelResolution float32) (NavSpanGraphBuildResult, error) {
	return buildNavSpanGraph(source, profile, voxelResolution)
}

// BuildNavSpanGraphWithContext derives profile clearance from persisted source
// occupancy, including known neighboring tiles, before building the graph.
func BuildNavSpanGraphWithContext(source NavSourceTileDef, context []NavSourceTileDef, profile NavAgentProfileDef, voxelResolution float32) (NavSpanGraphBuildResult, error) {
	if err := validateNavGraphProfile(profile, voxelResolution); err != nil {
		return NavSpanGraphBuildResult{}, err
	}
	profiled, err := navSourceWithProfileClearance(source, context, profile, voxelResolution)
	if err != nil {
		return NavSpanGraphBuildResult{}, err
	}
	return buildNavSpanGraph(profiled, profile, voxelResolution)
}

func buildNavSpanGraph(source NavSourceTileDef, profile NavAgentProfileDef, voxelResolution float32) (NavSpanGraphBuildResult, error) {
	if err := validateNavGraphProfile(profile, voxelResolution); err != nil {
		return NavSpanGraphBuildResult{}, err
	}

	filtered, err := FilterNavSpansForProfile(source, profile)
	if err != nil {
		return NavSpanGraphBuildResult{}, err
	}
	result := NavSpanGraphBuildResult{Graph: filtered.Graph, SpanDiagnostics: filtered.Diagnostics}
	accepted := make(map[uint32]struct{}, len(result.Graph.SpanIDs))
	for _, id := range result.Graph.SpanIDs {
		accepted[id] = struct{}{}
	}
	type column struct{ x, z int }
	columns := make(map[column][]NavSpanDef)
	spans := make(map[uint32]NavSpanDef, len(source.Spans))
	for _, span := range source.Spans {
		columns[column{span.X, span.Z}] = append(columns[column{span.X, span.Z}], span)
		spans[span.ID] = span
	}
	directions := [...]column{{x: -1}, {z: -1}, {z: 1}, {x: 1}}
	for _, fromID := range result.Graph.SpanIDs {
		from := spans[fromID]
		for _, direction := range directions {
			for _, to := range columns[column{from.X + direction.x, from.Z + direction.z}] {
				transition, code := buildNavSpanTransition(from, to, source.Coord, profile, voxelResolution)
				if code != "" {
					result.TransitionDiagnostics = append(result.TransitionDiagnostics, NavSpanTransitionDiagnostic{Code: code, From: from.ID, To: to.ID})
					continue
				}
				if _, ok := accepted[to.ID]; !ok {
					continue
				}
				result.Graph.SpanTransitions = append(result.Graph.SpanTransitions, transition)
			}
		}
	}
	result.Graph, err = CompressNavGraphRegions(source, result.Graph, voxelResolution)
	if err != nil {
		return NavSpanGraphBuildResult{}, err
	}
	if validation := ValidateNavGraphTile(&result.Graph); validation.HasErrors() {
		return NavSpanGraphBuildResult{}, fmt.Errorf("invalid navigation graph tile: %s", validation.Error())
	}
	return result, nil
}

func validateNavGraphProfile(profile NavAgentProfileDef, voxelResolution float32) error {
	if !finite(voxelResolution) || voxelResolution <= 0 {
		return fmt.Errorf("navigation graph voxel resolution must be finite and positive")
	}
	if !finite(profile.StepHeight) || profile.StepHeight < 0 {
		return fmt.Errorf("navigation agent step height must be finite and non-negative")
	}
	if !finite(profile.Height) || profile.Height <= 0 {
		return fmt.Errorf("navigation agent height must be finite and positive")
	}
	if !finite(profile.Radius) || profile.Radius <= 0 {
		return fmt.Errorf("navigation agent radius must be finite and positive")
	}
	if !finite(profile.MaxSlopeDegrees) || profile.MaxSlopeDegrees < 0 || profile.MaxSlopeDegrees >= 90 {
		return fmt.Errorf("navigation agent max slope must be finite and in [0, 90)")
	}
	return nil
}

func buildNavSpanTransition(from, to NavSpanDef, toTile TerrainChunkCoordDef, profile NavAgentProfileDef, voxelResolution float32) (NavSpanTransitionDef, string) {
	stepDelta := to.SupportHeight - from.SupportHeight
	step := float32(math.Abs(float64(stepDelta)))
	headroom := min(from.CeilingHeight, to.CeilingHeight) - max(from.SupportHeight, to.SupportHeight)
	clearance := min(from.ClearanceRadius, to.ClearanceRadius)
	switch {
	case step > profile.StepHeight:
		return NavSpanTransitionDef{}, NavSpanTransitionRejectedStep
	case headroom < profile.Height:
		return NavSpanTransitionDef{}, NavSpanTransitionRejectedHeadroom
	case clearance < profile.Radius:
		return NavSpanTransitionDef{}, NavSpanTransitionRejectedClearance
	}

	kind := NavTransitionWalk
	if step > 0 {
		kind = NavTransitionStep
		if float32(math.Atan2(float64(step), float64(voxelResolution))*180/math.Pi) <= profile.MaxSlopeDegrees {
			kind = NavTransitionStair
		}
	}
	return NavSpanTransitionDef{
		From: from.ID, To: NavSpanRef{Tile: toTile, Span: to.ID}, Kind: kind, StepDelta: stepDelta,
		Width: voxelResolution, MinHeadroom: headroom, MinClearance: clearance,
		Cost: float32(math.Hypot(float64(voxelResolution), float64(step))),
	}, ""
}
