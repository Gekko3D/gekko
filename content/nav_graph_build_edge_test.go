package content

import (
	"reflect"
	"testing"
)

func TestNavSpanGraphReachability(t *testing.T) {
	span := func(id uint32, x, z int, support, ceiling, clearance float32) NavSpanDef {
		return NavSpanDef{
			ID: id, X: x, Y: int(support * 10), Z: z,
			SupportHeight: support, CeilingHeight: ceiling, Headroom: ceiling - support, ClearanceRadius: clearance,
		}
	}
	type edge struct {
		from, to uint32
		kind     string
		delta    float32
	}
	tests := []struct {
		name               string
		spans              []NavSpanDef
		profile            NavAgentProfileDef
		wantSpanIDs        []uint32
		wantEdges          []edge
		wantSpanDiag       []NavSpanProfileDiagnostic
		wantTransitionDiag []NavSpanTransitionDiagnostic
		wantRoute          []uint32
		wantFailure        string
	}{
		{
			name: "walk and stair route",
			spans: []NavSpanDef{
				span(0, 0, 0, 0, 3, 1),
				span(1, 1, 0, 0, 3, 1),
				span(2, 2, 0, 0.5, 3, 1),
			},
			profile:     NavAgentProfileDef{ID: "walker", Radius: 0.5, Height: 1.5, StepHeight: 0.6, MaxSlopeDegrees: 30},
			wantSpanIDs: []uint32{0, 1, 2},
			wantEdges: []edge{
				{0, 1, NavTransitionWalk, 0},
				{1, 0, NavTransitionWalk, 0},
				{1, 2, NavTransitionStair, 0.5},
				{2, 1, NavTransitionStair, -0.5},
			},
			wantRoute: []uint32{0, 1, 2},
		},
		{
			name: "steep supported rise is step",
			spans: []NavSpanDef{
				span(0, 0, 0, 0, 3, 1),
				span(1, 1, 0, 0.5, 3, 1),
			},
			profile:     NavAgentProfileDef{ID: "walker", Radius: 0.5, Height: 1.5, StepHeight: 0.6, MaxSlopeDegrees: 20},
			wantSpanIDs: []uint32{0, 1},
			wantEdges: []edge{
				{0, 1, NavTransitionStep, 0.5},
				{1, 0, NavTransitionStep, -0.5},
			},
			wantRoute: []uint32{0, 1},
		},
		{
			name: "A star chooses lower cost route",
			spans: []NavSpanDef{
				span(0, 0, 0, 0, 3, 1),
				span(1, 0, 1, 0.5, 3, 1),
				span(2, 1, 0, 0, 3, 1),
				span(3, 1, 1, 0, 3, 1),
			},
			profile:     NavAgentProfileDef{ID: "walker", Radius: 0.5, Height: 1.5, StepHeight: 0.6, MaxSlopeDegrees: 20},
			wantSpanIDs: []uint32{0, 1, 2, 3},
			wantEdges: []edge{
				{0, 1, NavTransitionStep, 0.5},
				{0, 2, NavTransitionWalk, 0},
				{1, 0, NavTransitionStep, -0.5},
				{1, 3, NavTransitionStep, -0.5},
				{2, 0, NavTransitionWalk, 0},
				{2, 3, NavTransitionWalk, 0},
				{3, 1, NavTransitionStep, 0.5},
				{3, 2, NavTransitionWalk, 0},
			},
			wantRoute: []uint32{0, 2, 3},
		},
		{
			name: "rise above step limit disconnects",
			spans: []NavSpanDef{
				span(0, 0, 0, 0, 3, 1),
				span(1, 1, 0, 0.5, 3, 1),
			},
			profile:     NavAgentProfileDef{ID: "walker", Radius: 0.5, Height: 1.5, StepHeight: 0.4, MaxSlopeDegrees: 30},
			wantSpanIDs: []uint32{0, 1},
			wantTransitionDiag: []NavSpanTransitionDiagnostic{
				{Code: NavSpanTransitionRejectedStep, From: 0, To: 1},
				{Code: NavSpanTransitionRejectedStep, From: 1, To: 0},
			},
			wantFailure: NavSpanSearchNoRoute,
		},
		{
			name: "transition overlap lacks headroom",
			spans: []NavSpanDef{
				span(0, 0, 0, 0, 2.1, 1),
				span(1, 1, 0, 0.4, 2.4, 1),
			},
			profile:     NavAgentProfileDef{ID: "walker", Radius: 0.5, Height: 2, StepHeight: 0.5, MaxSlopeDegrees: 30},
			wantSpanIDs: []uint32{0, 1},
			wantTransitionDiag: []NavSpanTransitionDiagnostic{
				{Code: NavSpanTransitionRejectedHeadroom, From: 0, To: 1},
				{Code: NavSpanTransitionRejectedHeadroom, From: 1, To: 0},
			},
			wantFailure: NavSpanSearchNoRoute,
		},
		{
			name: "unsupported neighbor reports clearance",
			spans: []NavSpanDef{
				span(0, 0, 0, 0, 3, 1),
				span(1, 1, 0, 0, 3, 0.4),
			},
			profile:      NavAgentProfileDef{ID: "walker", Radius: 0.5, Height: 1.5, StepHeight: 0.5, MaxSlopeDegrees: 30},
			wantSpanIDs:  []uint32{0},
			wantSpanDiag: []NavSpanProfileDiagnostic{{Code: NavSpanRejectedClearance, Span: 1}},
			wantTransitionDiag: []NavSpanTransitionDiagnostic{
				{Code: NavSpanTransitionRejectedClearance, From: 0, To: 1},
			},
			wantFailure: NavSpanSearchGoalMissing,
		},
		{
			name: "diagonal spans stay disconnected",
			spans: []NavSpanDef{
				span(0, 0, 0, 0, 3, 1),
				span(1, 1, 1, 0, 3, 1),
			},
			profile:     NavAgentProfileDef{ID: "walker", Radius: 0.5, Height: 1.5, StepHeight: 0.5, MaxSlopeDegrees: 30},
			wantSpanIDs: []uint32{0, 1},
			wantFailure: NavSpanSearchNoRoute,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := NavSourceTileDef{
				NavID: "test", SchemaVersion: CurrentNavSourceTileSchemaVersion, BuilderVersion: "test",
				SourceHash: "source", DependencyHash: "dependencies", Spans: test.spans,
			}
			built, err := BuildNavSpanGraph(source, test.profile, 1)
			if err != nil {
				t.Fatalf("build failed: %v", err)
			}
			if !reflect.DeepEqual(built.Graph.SpanIDs, test.wantSpanIDs) {
				t.Fatalf("span ids: want %v, got %v", test.wantSpanIDs, built.Graph.SpanIDs)
			}
			if !reflect.DeepEqual(built.SpanDiagnostics, test.wantSpanDiag) {
				t.Fatalf("span diagnostics: want %+v, got %+v", test.wantSpanDiag, built.SpanDiagnostics)
			}
			if !reflect.DeepEqual(built.TransitionDiagnostics, test.wantTransitionDiag) {
				t.Fatalf("transition diagnostics: want %+v, got %+v", test.wantTransitionDiag, built.TransitionDiagnostics)
			}
			var gotEdges []edge
			for _, transition := range built.Graph.SpanTransitions {
				gotEdges = append(gotEdges, edge{transition.From, transition.To.Span, transition.Kind, transition.StepDelta})
				if transition.To.Tile != source.Coord || transition.MinHeadroom < test.profile.Height || transition.MinClearance < test.profile.Radius || transition.Width <= 0 || transition.Cost <= 0 {
					t.Fatalf("invalid accepted transition: %+v", transition)
				}
			}
			if !reflect.DeepEqual(gotEdges, test.wantEdges) {
				t.Fatalf("edges: want %+v, got %+v", test.wantEdges, gotEdges)
			}

			route, err := FindNavSpanRoute(source, built.Graph, 1, test.spans[0].ID, test.spans[len(test.spans)-1].ID)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if !reflect.DeepEqual(route.Spans, test.wantRoute) || route.FailureReason != test.wantFailure || route.Found != (test.wantFailure == "") {
				t.Fatalf("route mismatch: want spans=%v failure=%q, got %+v", test.wantRoute, test.wantFailure, route)
			}
		})
	}
}
