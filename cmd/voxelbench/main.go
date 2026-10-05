package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"sort"

	"github.com/cogentcore/webgpu/wgpu"
)

type sampleSummary struct {
	Count       int     `json:"count"`
	MedianTicks float64 `json:"median_ticks"`
	P95Ticks    float64 `json:"p95_ticks"`
}

// timestampQueryLayout bounds query storage for independently owned cohorts.
const timestampCohortSize = 128

func timestampQueryLayout(samples int) (cohorts int, maxQueries uint32) {
	if samples <= 0 {
		return 0, 0
	}
	cohorts = 1 + (samples-1)/timestampCohortSize
	return cohorts, uint32(2 * (min(samples, timestampCohortSize) + 3))
}

// requiredPassTimestampFeatures forwards the adapter's discovered basic query
// capability, which is sufficient for compute-pass boundary timestamps.
func requiredPassTimestampFeatures(features map[string]wgpu.FeatureName) []wgpu.FeatureName {
	feature, ok := features["timestamp-query"]
	if !ok {
		return nil
	}
	return []wgpu.FeatureName{feature}
}

func summarizeTimestamps(ticks []uint64, batch uint32) (sampleSummary, error) {
	if batch == 0 || len(ticks) < 6 || len(ticks)%2 != 0 {
		return sampleSummary{}, errors.New("need at least three timestamp pairs and positive batch")
	}
	durations := make([]float64, len(ticks)/2)
	for i := range durations {
		a, b := ticks[2*i], ticks[2*i+1]
		if b <= a || (i > 0 && a < ticks[2*i-1]) {
			return sampleSummary{}, errors.New("invalid or reset GPU timestamp sequence")
		}
		durations[i] = float64(b-a) / float64(batch)
	}
	sort.Float64s(durations)
	n := len(durations)
	median := durations[n/2]
	if n%2 == 0 {
		median = (durations[n/2-1] + median) / 2
	}
	return sampleSummary{n, median, durations[int(math.Ceil(.95*float64(n)))-1]}, nil
}

type settings struct {
	Workload string `json:"workload"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Samples  int    `json:"samples"`
	Warmup   int    `json:"warmup"`
	Batch    int    `json:"batch"`
}
type cpuCost struct {
	UpdateNS     int64  `json:"update_ns"`
	NormalBakeNS int64  `json:"normal_bake_ns"`
	UploadBytes  uint64 `json:"content_upload_bytes"`
}
type capture struct {
	Depth     string `json:"depth_sha256"`
	Normal    string `json:"normal_sha256"`
	Material  string `json:"material_sha256"`
	HitPixels int    `json:"hit_pixels"`
}
type geometryStats struct {
	OccupiedVoxels int    `json:"occupied_voxels"`
	OccupiedBricks int    `json:"occupied_bricks"`
	ContentSHA256  string `json:"content_sha256"`
}
type report struct {
	GPUTimingMethod                  string         `json:"gpu_timing_method"`
	QueryCohortSize                  int            `json:"gpu_query_cohort_size"`
	QueryCohortCount                 int            `json:"gpu_query_cohort_count"`
	QueryMaxCount                    uint32         `json:"gpu_query_max_count"`
	CalibrationCount                 int            `json:"gpu_calibration_count"`
	CalibrationTicks                 []uint64       `json:"gpu_calibration_timestamp_pairs,omitempty"`
	InitialGeometry                  geometryStats  `json:"initial_geometry"`
	FinalGeometry                    geometryStats  `json:"final_geometry"`
	Version                          int            `json:"version"`
	Mode                             string         `json:"mode"`
	Materials                        string         `json:"material_storage"`
	Settings                         settings       `json:"settings"`
	Adapter                          string         `json:"adapter"`
	Backend                          string         `json:"backend"`
	Fixture                          string         `json:"fixture"`
	Shader                           string         `json:"shader_sha256"`
	GPUAvailable                     bool           `json:"gpu_timing_available"`
	GPUUnit                          string         `json:"gpu_timing_unit"`
	Warning                          string         `json:"warning,omitempty"`
	RawTicks                         []uint64       `json:"raw_timestamp_pairs,omitempty"`
	GPU                              *sampleSummary `json:"gpu_per_dispatch,omitempty"`
	Initial                          cpuCost        `json:"initial_admission"`
	CPU                              []cpuCost      `json:"cpu_updates"`
	AuxiliaryCapacityBytes           uint64         `json:"auxiliary_physical_capacity_bytes"`
	PayloadAtlasCapacityBytes        uint64         `json:"payload_atlas_physical_capacity_bytes"`
	PayloadAssignedBytes             uint64         `json:"payload_assigned_bytes"`
	InitialCapture                   capture        `json:"initial_capture"`
	FinalCapture                     capture        `json:"final_capture"`
	PackedToDenseMedianRatio         *float64       `json:"packed_to_dense_median_ratio,omitempty"`
	PackedMaterialToAtlasMedianRatio *float64       `json:"packed_material_to_atlas_median_ratio,omitempty"`
}

func reportMaterials(r report) (string, error) {
	if r.Version == 1 && (r.Materials == "" || r.Materials == "atlas") {
		return "atlas", nil
	}
	if r.Version == 2 && (r.Materials == "atlas" || r.Materials == "packed") {
		return r.Materials, nil
	}
	return "", errors.New("incomparable reports: unsupported version or material storage policy")
}

func compareReports(a, b report) (*float64, error) {
	am, err := reportMaterials(a)
	if err != nil {
		return nil, err
	}
	bm, err := reportMaterials(b)
	if err != nil {
		return nil, err
	}
	// Exactly one storage axis must differ; changing both obscures the cause.
	if a.GPUTimingMethod != b.GPUTimingMethod || a.QueryCohortSize != b.QueryCohortSize || a.QueryCohortCount != b.QueryCohortCount || a.QueryMaxCount != b.QueryMaxCount || a.Version != b.Version || (a.Mode == b.Mode) == (am == bm) || (a.Mode != "packed" && a.Mode != "dense") || (b.Mode != "packed" && b.Mode != "dense") || a.Adapter != b.Adapter || a.Backend != b.Backend || a.Settings != b.Settings || a.Fixture != b.Fixture || a.InitialGeometry != b.InitialGeometry || a.FinalGeometry != b.FinalGeometry || a.Shader != b.Shader || a.InitialCapture != b.InitialCapture || a.FinalCapture != b.FinalCapture || a.InitialCapture.HitPixels <= 0 || a.FinalCapture.HitPixels <= 0 {
		return nil, errors.New("incomparable reports: modes, adapter/backend, settings, fixture, shader, or G-buffer parity differ")
	}
	if !a.GPUAvailable || !b.GPUAvailable {
		return nil, nil
	}
	// Recompute summaries from raw pairs so a hand-edited summary cannot imply a ratio.
	x, err := summarizeTimestamps(a.RawTicks, uint32(a.Settings.Batch))
	if err != nil {
		return nil, err
	}
	y, err := summarizeTimestamps(b.RawTicks, uint32(b.Settings.Batch))
	if err != nil {
		return nil, err
	}
	if len(a.RawTicks) != 2*a.Settings.Samples || len(b.RawTicks) != 2*b.Settings.Samples || a.GPUUnit != "raw_gpu_ticks" || b.GPUUnit != a.GPUUnit || a.GPU == nil || b.GPU == nil || !reflect.DeepEqual(x, *a.GPU) || !reflect.DeepEqual(y, *b.GPU) {
		return nil, errors.New("incomparable GPU samples or units")
	}
	if (a.Mode != b.Mode && a.Mode == "dense") || (a.Mode == b.Mode && am == "atlas") {
		x, y = y, x
	}
	ratio := x.MedianTicks / y.MedianTicks
	return &ratio, nil
}
func run(args []string, output io.Writer) error {
	fs := flag.NewFlagSet("voxelbench", flag.ContinueOnError)
	fs.SetOutput(output)
	mode := fs.String("mode", "dense", "normal storage: dense or packed")
	materials := fs.String("materials", "atlas", "material storage: atlas or packed")
	var cfg settings
	fs.StringVar(&cfg.Workload, "workload", "sparse", "deterministic sparse, dense, or edited fixture")
	fs.IntVar(&cfg.Width, "width", 640, "render width (1..4096)")
	fs.IntVar(&cfg.Height, "height", 480, "render height (1..4096)")
	fs.IntVar(&cfg.Samples, "samples", 30, "timestamp batches (3..10000)")
	fs.IntVar(&cfg.Warmup, "warmup", 10, "warmup batches (0..1000)")
	fs.IntVar(&cfg.Batch, "batch", 8, "dispatches per batch (1..1024)")
	path := fs.String("output", "", "optional JSON report path")
	comparison := fs.String("compare", "", "JSON report with exactly one different storage policy to validate and compare")
	fs.Usage = func() { fmt.Fprintln(output, "Usage: voxelbench [flags]"); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *mode != "dense" && *mode != "packed" {
		return errors.New("mode must be dense or packed")
	}
	if *materials != "atlas" && *materials != "packed" {
		return errors.New("materials must be atlas or packed")
	}
	if cfg.Workload != "sparse" && cfg.Workload != "dense" && cfg.Workload != "edited" {
		return errors.New("workload must be sparse, dense or edited")
	}
	for _, v := range []struct {
		name        string
		v, min, max int
	}{{"width", cfg.Width, 1, 4096}, {"height", cfg.Height, 1, 4096}, {"samples", cfg.Samples, 3, 10000}, {"warmup", cfg.Warmup, 0, 1000}, {"batch", cfg.Batch, 1, 1024}} {
		if v.v < v.min || v.v > v.max {
			return fmt.Errorf("%s must be in %d..%d", v.name, v.min, v.max)
		}
	}
	var previous report
	if *comparison != "" {
		data, err := os.ReadFile(*comparison)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(data, &previous); err != nil {
			return fmt.Errorf("compare report: %w", err)
		}
	}
	r, err := benchmark(*mode, *materials, cfg)
	if err != nil {
		return err
	}
	if *comparison != "" {
		ratio, err := compareReports(r, previous)
		if err != nil {
			return err
		}
		if r.Mode == previous.Mode {
			r.PackedMaterialToAtlasMedianRatio = ratio
		} else {
			r.PackedToDenseMedianRatio = ratio
		}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if *path != "" {
		if err = os.WriteFile(*path, data, 0644); err != nil {
			return err
		}
	}
	_, err = output.Write(data)
	return err
}
func main() {
	// This single-process CLI owns diagnostic routing; run itself leaves globals
	// untouched so callers and tests retain their own output streams.
	reportOutput := os.Stdout
	os.Stdout = os.Stderr
	if err := run(os.Args[1:], reportOutput); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
