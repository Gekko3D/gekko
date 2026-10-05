package main

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

// Invalid requests must report an actionable input error without needing a GPU.
func TestRunRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"mode", []string{"-mode", "automatic"}, "mode"},
		{"workload", []string{"-workload", "random"}, "workload"},
		{"zero width", []string{"-width", "0"}, "width"},
		{"negative width", []string{"-width", "-1"}, "width"},
		{"excessive width", []string{"-width", "4097"}, "width"},
		{"width beyond uint32", []string{"-width", "4294967296"}, "width"},
		{"zero height", []string{"-height", "0"}, "height"},
		{"negative height", []string{"-height", "-1"}, "height"},
		{"excessive height", []string{"-height", "4097"}, "height"},
		{"too few samples", []string{"-samples", "2"}, "samples"},
		{"negative samples", []string{"-samples", "-1"}, "samples"},
		{"excessive samples", []string{"-samples", "10001"}, "samples"},
		{"negative warmup", []string{"-warmup", "-1"}, "warmup"},
		{"excessive warmup", []string{"-warmup", "1001"}, "warmup"},
		{"zero batch", []string{"-batch", "0"}, "batch"},
		{"negative batch", []string{"-batch", "-1"}, "batch"},
		{"excessive batch", []string{"-batch", "1025"}, "batch"},
		{"batch beyond uint32", []string{"-batch", "4294967296"}, "batch"},
		{"noninteger samples", []string{"-samples", "three"}, "samples"},
		{"unexpected positional", []string{"scene.vox"}, "unexpected"},
		{"unknown flag", []string{"-unrecognized"}, "unrecognized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			err := run(tt.args, &output)
			if err == nil {
				t.Fatal("invalid input accepted")
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("error = %q, want input diagnostic containing %q", err, tt.want)
			}
		})
	}
}

func TestRunHelpWithoutGPU(t *testing.T) {
	for _, flag := range []string{"-h", "-help"} {
		t.Run(flag, func(t *testing.T) {
			var output bytes.Buffer
			if err := run([]string{flag}, &output); err != nil {
				t.Fatalf("help failed: %v", err)
			}
			for _, want := range []string{"Usage", "-mode", "-workload", "-width", "-height", "-samples", "-warmup", "-batch", "-output", "-compare"} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("help missing %q: %s", want, output.String())
				}
			}
		})
	}
}

func TestSummarizeTimestampsRejectsUnusableSamples(t *testing.T) {
	tests := []struct {
		name  string
		ticks []uint64
		batch uint32
	}{
		{"no samples", nil, 1},
		{"fewer than three pairs", []uint64{1, 2, 3, 4}, 1},
		{"unpaired timestamp", []uint64{1, 2, 3, 4, 5, 6, 7}, 1},
		{"zero elapsed", []uint64{1, 2, 3, 3, 4, 5}, 1},
		{"inverted pair", []uint64{1, 2, 4, 3, 5, 6}, 1},
		{"clock reset between pairs", []uint64{100, 110, 1, 2, 3, 4}, 1},
		{"zero batch", []uint64{1, 2, 3, 4, 5, 6}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := summarizeTimestamps(tt.ticks, tt.batch); err == nil {
				t.Fatalf("unusable timestamps accepted: %+v", got)
			}
		})
	}
}

func TestSummarizeTimestampsReportsPerDispatchGPUTicks(t *testing.T) {
	tests := []struct {
		name       string
		ticks      []uint64
		batch      uint32
		wantCount  int
		wantMedian float64
		wantP95    float64
	}{
		{
			name:       "unsorted durations divided by batch",
			ticks:      []uint64{100, 112, 200, 204, 300, 308, 400, 420, 500, 516},
			batch:      4,
			wantCount:  5,
			wantMedian: 3,
			wantP95:    5,
		},
		{
			name:       "fractional dispatch time and even median",
			ticks:      []uint64{1, 2, 10, 12, 20, 23, 30, 34},
			batch:      4,
			wantCount:  4,
			wantMedian: 0.625,
			wantP95:    1,
		},
		{
			name:       "poll and readback gaps excluded",
			ticks:      []uint64{1, 11, 1_000_000, 1_000_020, 9_000_000, 9_000_030},
			batch:      1,
			wantCount:  3,
			wantMedian: 20,
			wantP95:    30,
		},
		{
			name: "large timestamp epoch retains small differences",
			ticks: []uint64{
				math.MaxUint64 - 100, math.MaxUint64 - 99,
				math.MaxUint64 - 90, math.MaxUint64 - 88,
				math.MaxUint64 - 80, math.MaxUint64 - 77,
			},
			batch:      1,
			wantCount:  3,
			wantMedian: 2,
			wantP95:    3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := summarizeTimestamps(tt.ticks, tt.batch)
			if err != nil {
				t.Fatalf("valid timestamps rejected: %v", err)
			}
			if got.Count != tt.wantCount || got.MedianTicks != tt.wantMedian || got.P95Ticks != tt.wantP95 {
				t.Fatalf("summary = %+v, want count=%d median=%g ticks p95=%g ticks", got, tt.wantCount, tt.wantMedian, tt.wantP95)
			}
		})
	}
}

func TestSummarizeTimestampsP95UsesNearestRank(t *testing.T) {
	// Twenty descending durations make the 95th percentile 19, not the max
	// or an interpolated percentile. Timestamp pairs remain chronological.
	ticks := make([]uint64, 0, 40)
	for duration := uint64(20); duration > 0; duration-- {
		start := uint64(len(ticks)+1) * 100
		ticks = append(ticks, start, start+duration)
	}
	got, err := summarizeTimestamps(ticks, 1)
	if err != nil {
		t.Fatalf("valid timestamps rejected: %v", err)
	}
	if got.Count != 20 || got.MedianTicks != 10.5 || got.P95Ticks != 19 {
		t.Fatalf("summary = %+v, want count=20 median=10.5 ticks p95=19 ticks", got)
	}
}
