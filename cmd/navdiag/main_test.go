package main

import "testing"

func TestNavPercentile(t *testing.T) {
	values := []float64{9, 1, 5, 3, 7}
	if got := navPercentile(values, 0.50); got != 5 {
		t.Fatalf("p50 = %g, want 5", got)
	}
	if got := navPercentile(values, 0.95); got != 9 {
		t.Fatalf("p95 = %g, want 9", got)
	}
	if got := navPercentile(nil, 0.50); got != 0 {
		t.Fatalf("empty percentile = %g, want 0", got)
	}
}
