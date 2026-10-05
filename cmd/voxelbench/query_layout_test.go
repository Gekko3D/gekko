package main

import (
	"math"
	"testing"
)

func TestTimestampQueryLayoutBoundsCohortAllocation(t *testing.T) {
	for _, tt := range []struct {
		samples int
		cohorts int
		queries uint32
	}{
		{3, 1, 12},
		{128, 1, 262},
		{129, 2, 262},
		{10000, 79, 262},
	} {
		gotCohorts, gotQueries := timestampQueryLayout(tt.samples)
		if gotCohorts != tt.cohorts || gotQueries != tt.queries {
			t.Errorf("samples=%d: cohorts=%d maxQueries=%d, want cohorts=%d maxQueries=%d", tt.samples, gotCohorts, gotQueries, tt.cohorts, tt.queries)
		}
	}
}

func TestTimestampQueryLayoutRejectsNonpositiveSamples(t *testing.T) {
	for _, samples := range []int{0, -1, math.MinInt} {
		cohorts, queries := timestampQueryLayout(samples)
		if cohorts != 0 || queries != 0 {
			t.Errorf("samples=%d allocated cohorts=%d maxQueries=%d", samples, cohorts, queries)
		}
	}
}

func TestTimestampQueryLayoutDoesNotOverflowAtMaxInt(t *testing.T) {
	cohorts, queries := timestampQueryLayout(math.MaxInt)
	if cohorts <= 0 || queries != 262 {
		t.Fatalf("MaxInt samples: cohorts=%d maxQueries=%d, want positive cohort count and bounded 262-query allocation", cohorts, queries)
	}
}
