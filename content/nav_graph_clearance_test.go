package content

import (
	"reflect"
	"testing"
)

func TestNavSpanClearanceAndProfileSupport(t *testing.T) {
	tests := []struct {
		name            string
		solid           [][3]int
		blocked         [][3]int
		wantSourceSpans int
		wantClearance   float32
		small           NavAgentProfileDef
		large           NavAgentProfileDef
		wantSmall       []uint32
		wantLarge       []uint32
		wantLargeReason string
	}{
		{
			name:  "open span",
			solid: [][3]int{{2, 0, 2}}, wantSourceSpans: 1, wantClearance: 2.5,
			small:     NavAgentProfileDef{ID: "small", Radius: 0.5, Height: 1},
			large:     NavAgentProfileDef{ID: "large", Radius: 2, Height: 3},
			wantSmall: []uint32{0}, wantLarge: []uint32{0},
		},
		{
			name:  "solid wall",
			solid: [][3]int{{2, 0, 2}, {3, 1, 2}, {3, 2, 2}, {3, 3, 2}, {3, 4, 2}}, wantSourceSpans: 1, wantClearance: 0.5,
			small:     NavAgentProfileDef{ID: "small", Radius: 0.5, Height: 1},
			large:     NavAgentProfileDef{ID: "large", Radius: 0.6, Height: 1},
			wantSmall: []uint32{0}, wantLargeReason: NavSpanRejectedClearance,
		},
		{
			name:  "metadata blocker subtracts only",
			solid: [][3]int{{2, 0, 2}}, blocked: [][3]int{{3, 1, 2}}, wantSourceSpans: 1, wantClearance: 0.5,
			small:     NavAgentProfileDef{ID: "small", Radius: 0.5, Height: 1},
			large:     NavAgentProfileDef{ID: "large", Radius: 0.6, Height: 1},
			wantSmall: []uint32{0}, wantLargeReason: NavSpanRejectedClearance,
		},
		{
			name:  "low ceiling",
			solid: [][3]int{{2, 0, 2}, {2, 3, 2}}, wantSourceSpans: 2, wantClearance: 2.5,
			small:     NavAgentProfileDef{ID: "small", Radius: 0.5, Height: 2},
			large:     NavAgentProfileDef{ID: "large", Radius: 0.5, Height: 2.1},
			wantSmall: []uint32{0}, wantLargeReason: NavSpanRejectedHeadroom,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			built, err := BuildNavSourceSpans(NavSpanBuildInput{
				NavID: "test", BuilderVersion: "test", SourceHash: "source",
				ChunkSize: 5, VoxelResolution: 1,
				Center: NavSpanBuildChunk{Known: true, SolidVoxels: test.solid, BlockedVoxels: test.blocked},
			})
			if err != nil {
				t.Fatalf("build failed: %v", err)
			}
			if len(built.Source.Spans) != test.wantSourceSpans {
				t.Fatalf("source spans: want %d, got %+v", test.wantSourceSpans, built.Source.Spans)
			}
			if got := built.Source.Spans[0].ClearanceRadius; got != test.wantClearance {
				t.Fatalf("clearance: want %v, got %v", test.wantClearance, got)
			}

			// Isolate target floor when fixture also emits a supported ceiling top.
			built.Source.Spans = built.Source.Spans[:1]
			small, err := FilterNavSpansForProfile(built.Source, test.small)
			if err != nil {
				t.Fatalf("small profile failed: %v", err)
			}
			large, err := FilterNavSpansForProfile(built.Source, test.large)
			if err != nil {
				t.Fatalf("large profile failed: %v", err)
			}
			if !reflect.DeepEqual(small.Graph.SpanIDs, test.wantSmall) {
				t.Fatalf("small spans: want %v, got %v", test.wantSmall, small.Graph.SpanIDs)
			}
			if !reflect.DeepEqual(large.Graph.SpanIDs, test.wantLarge) {
				t.Fatalf("large spans: want %v, got %v", test.wantLarge, large.Graph.SpanIDs)
			}
			if test.wantLargeReason != "" && !reflect.DeepEqual(large.Diagnostics, []NavSpanProfileDiagnostic{{Code: test.wantLargeReason, Span: 0}}) {
				t.Fatalf("large rejection: want %s, got %+v", test.wantLargeReason, large.Diagnostics)
			}
			for _, id := range large.Graph.SpanIDs {
				if !containsNavSpanID(small.Graph.SpanIDs, id) {
					t.Fatalf("larger profile gained span %d absent from smaller profile", id)
				}
			}
		})
	}
}

func containsNavSpanID(ids []uint32, want uint32) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
