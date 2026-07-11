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
// spans in four-neighbor columns. Cross-tile edges belong to Phase 6.
func BuildNavSpanGraph(source NavSourceTileDef, profile NavAgentProfileDef, voxelResolution float32) (NavSpanGraphBuildResult, error) {
	if !finite(voxelResolution) || voxelResolution <= 0 {
		return NavSpanGraphBuildResult{}, fmt.Errorf("navigation graph voxel resolution must be finite and positive")
	}
	if !finite(profile.StepHeight) || profile.StepHeight < 0 {
		return NavSpanGraphBuildResult{}, fmt.Errorf("navigation agent step height must be finite and non-negative")
	}
	if !finite(profile.MaxSlopeDegrees) || profile.MaxSlopeDegrees < 0 || profile.MaxSlopeDegrees >= 90 {
		return NavSpanGraphBuildResult{}, fmt.Errorf("navigation agent max slope must be finite and in [0, 90)")
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
				stepDelta := to.SupportHeight - from.SupportHeight
				step := float32(math.Abs(float64(stepDelta)))
				headroom := min(from.CeilingHeight, to.CeilingHeight) - max(from.SupportHeight, to.SupportHeight)
				clearance := min(from.ClearanceRadius, to.ClearanceRadius)
				code := ""
				switch {
				case step > profile.StepHeight:
					code = NavSpanTransitionRejectedStep
				case headroom < profile.Height:
					code = NavSpanTransitionRejectedHeadroom
				case clearance < profile.Radius:
					code = NavSpanTransitionRejectedClearance
				}
				if code != "" {
					result.TransitionDiagnostics = append(result.TransitionDiagnostics, NavSpanTransitionDiagnostic{Code: code, From: from.ID, To: to.ID})
					continue
				}
				if _, ok := accepted[to.ID]; !ok {
					continue
				}

				kind := NavTransitionWalk
				if step > 0 {
					slope := float32(math.Atan2(float64(step), float64(voxelResolution)) * 180 / math.Pi)
					kind = NavTransitionStep
					if slope <= profile.MaxSlopeDegrees {
						kind = NavTransitionStair
					}
				}
				result.Graph.SpanTransitions = append(result.Graph.SpanTransitions, NavSpanTransitionDef{
					From:         from.ID,
					To:           NavSpanRef{Tile: source.Coord, Span: to.ID},
					Kind:         kind,
					StepDelta:    stepDelta,
					Width:        voxelResolution,
					MinHeadroom:  headroom,
					MinClearance: clearance,
					Cost:         float32(math.Hypot(float64(voxelResolution), float64(step))),
				})
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
