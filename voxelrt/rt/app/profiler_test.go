package app

import (
	"strings"
	"testing"
	"time"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func TestProfilerReportsCountsAndCPUTimings(t *testing.T) {
	profiler := core.NewProfiler()

	profiler.SetCount("Visible", 7)
	profiler.BeginScope("Scene Commit")
	time.Sleep(2 * time.Millisecond)
	profiler.EndScope("Scene Commit")

	stats := profiler.GetStatsString()
	if !strings.Contains(stats, "Counts:\n") {
		t.Fatalf("expected counts section, got %q", stats)
	}
	if !strings.Contains(stats, "CPU Timings (ms):\n") {
		t.Fatalf("expected CPU timings section, got %q", stats)
	}
	if !strings.Contains(stats, "Visible") {
		t.Fatalf("expected count name in stats, got %q", stats)
	}
	if !strings.Contains(stats, "Scene Commit") {
		t.Fatalf("expected scope name in stats, got %q", stats)
	}

	profiler.Reset()
	resetStats := profiler.GetStatsString()
	if !strings.Contains(resetStats, "Scene Commit") {
		t.Fatalf("expected scope to remain listed after reset, got %q", resetStats)
	}
	if !strings.Contains(resetStats, "Scene Commit   : 0.000") && !strings.Contains(resetStats, "Scene Commit    : 0.000") {
		t.Fatalf("expected reset timing to be zeroed, got %q", resetStats)
	}
}
