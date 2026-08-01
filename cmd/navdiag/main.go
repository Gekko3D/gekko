package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gekko3d/gekko/content"
)

type vec3Flag struct {
	value content.Vec3
	set   bool
}

type navMeasurement struct {
	BundleBytes          int64   `json:"bundle_bytes"`
	SourceBytes          int64   `json:"source_bytes"`
	GraphBytes           int64   `json:"graph_bytes"`
	LoadMilliseconds     float64 `json:"load_milliseconds"`
	IndexMilliseconds    float64 `json:"index_milliseconds"`
	RetainedHeapBytes    uint64  `json:"retained_heap_bytes"`
	RouteSamples         int     `json:"route_samples,omitempty"`
	RouteP50Milliseconds float64 `json:"route_p50_milliseconds,omitempty"`
	RouteP95Milliseconds float64 `json:"route_p95_milliseconds,omitempty"`
	RouteP99Milliseconds float64 `json:"route_p99_milliseconds,omitempty"`
}

func (v *vec3Flag) String() string {
	return fmt.Sprintf("%g,%g,%g", v.value[0], v.value[1], v.value[2])
}

func (v *vec3Flag) Set(value string) error {
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return fmt.Errorf("want x,y,z")
	}
	for i := range parts {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 32)
		if err != nil {
			return err
		}
		v.value[i] = float32(parsed)
	}
	v.set = true
	return nil
}

func main() {
	var navPath, profileID string
	var jsonOutput bool
	var measureRuns int
	var start, end vec3Flag
	flag.StringVar(&navPath, "nav", "", "input .gknav manifest")
	flag.StringVar(&profileID, "profile", "", "agent profile id; defaults to first profile")
	flag.BoolVar(&jsonOutput, "json", false, "print JSON")
	flag.IntVar(&measureRuns, "measure-runs", 0, "measure load/index and repeat an optional route this many times")
	flag.Var(&start, "start", "optional route start x,y,z")
	flag.Var(&end, "end", "optional route end x,y,z")
	flag.Parse()
	if navPath == "" {
		fatalf("-nav is required")
	}
	if measureRuns < 0 {
		fatalf("-measure-runs must be non-negative")
	}
	if start.set != end.set {
		fatalf("-start and -end must be provided together")
	}
	var measurement *navMeasurement
	var bake *content.NavGraphBakeResult
	var err error
	if measureRuns > 0 {
		bake, measurement, err = measureNavGraphBake(navPath, profileID, start, end, measureRuns)
	} else {
		bake, err = content.LoadNavGraphBake(navPath)
	}
	if err != nil {
		fatalf("load: %v", err)
	}
	summary := content.DiagnoseNavGraphBake(bake)
	var route *content.NavRouteResult
	if start.set {
		if profileID == "" {
			profileID = bake.Manifest.AgentProfiles[0].ID
		}
		query, queryErr := navGraphDiagnosticQuery(bake, profileID)
		if queryErr != nil {
			fatalf("route query: %v", queryErr)
		}
		result, err := query.FindRoute(start.value, end.value)
		if err != nil {
			fatalf("route: %v", err)
		}
		route = &result
	}
	if jsonOutput {
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			Summary     content.NavGraphBakeSummary `json:"summary"`
			Route       *content.NavRouteResult     `json:"route,omitempty"`
			Measurement *navMeasurement             `json:"measurement,omitempty"`
		}{summary, route, measurement}); err != nil {
			fatalf("encode: %v", err)
		}
		return
	}
	fmt.Printf("source_tiles=%d graph_tiles=%d spans=%d accepted=%d regions=%d span_transitions=%d region_transitions=%d hard_errors=%d\n",
		summary.SourceTiles, summary.GraphTiles, summary.Spans, summary.AcceptedSpans, summary.Regions, summary.SpanTransitions, summary.RegionTransitions, summary.Validation.HardErrorCount)
	if route != nil {
		fmt.Printf("route_found=%t steps=%d waypoints=%d failure=%q failure_tile=%s\n", route.Found, len(route.Steps), len(route.Waypoints), route.FailureReason, content.TerrainChunkKey(route.FailureTile))
	}
	if measurement != nil {
		fmt.Printf("bundle_bytes=%d source_bytes=%d graph_bytes=%d load_ms=%.3f index_ms=%.3f retained_heap_bytes=%d route_samples=%d route_p50_ms=%.3f route_p95_ms=%.3f route_p99_ms=%.3f\n",
			measurement.BundleBytes, measurement.SourceBytes, measurement.GraphBytes, measurement.LoadMilliseconds,
			measurement.IndexMilliseconds, measurement.RetainedHeapBytes, measurement.RouteSamples,
			measurement.RouteP50Milliseconds, measurement.RouteP95Milliseconds, measurement.RouteP99Milliseconds,
		)
	}
}

func measureNavGraphBake(navPath, profileID string, start, end vec3Flag, runs int) (*content.NavGraphBakeResult, *navMeasurement, error) {
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	loadStart := time.Now()
	bake, err := content.LoadNavGraphBake(navPath)
	if err != nil {
		return nil, nil, err
	}
	measurement := &navMeasurement{LoadMilliseconds: millisecondsSince(loadStart)}
	measurement.BundleBytes, measurement.SourceBytes, measurement.GraphBytes, err = navGraphBundleBytes(navPath, &bake.Manifest)
	if err != nil {
		return nil, nil, err
	}
	indexStart := time.Now()
	query, err := navGraphDiagnosticQuery(bake, profileID)
	if err != nil {
		return nil, nil, err
	}
	measurement.IndexMilliseconds = millisecondsSince(indexStart)
	if start.set {
		durations := make([]float64, 0, runs)
		for range runs {
			routeStart := time.Now()
			route, err := query.FindRoute(start.value, end.value)
			if err != nil {
				return nil, nil, err
			}
			if !route.Found {
				return nil, nil, fmt.Errorf("measured route failed: %s", route.FailureReason)
			}
			durations = append(durations, millisecondsSince(routeStart))
		}
		measurement.RouteSamples = len(durations)
		measurement.RouteP50Milliseconds = navPercentile(durations, 0.50)
		measurement.RouteP95Milliseconds = navPercentile(durations, 0.95)
		measurement.RouteP99Milliseconds = navPercentile(durations, 0.99)
	}
	bake.SourceTiles, bake.GraphTiles = nil, nil
	runtime.GC()
	runtime.KeepAlive(query)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if after.HeapAlloc > before.HeapAlloc {
		measurement.RetainedHeapBytes = after.HeapAlloc - before.HeapAlloc
	}
	bake, err = content.LoadNavGraphBake(navPath)
	return bake, measurement, err
}

func navGraphDiagnosticQuery(bake *content.NavGraphBakeResult, profileID string) (*content.NavGraphQuery, error) {
	if bake == nil || len(bake.Manifest.AgentProfiles) == 0 {
		return nil, fmt.Errorf("navigation manifest has no agent profiles")
	}
	if profileID == "" {
		profileID = bake.Manifest.AgentProfiles[0].ID
	}
	var profile content.NavAgentProfileDef
	for _, candidate := range bake.Manifest.AgentProfiles {
		if candidate.ID == profileID {
			profile = candidate
			break
		}
	}
	if profile.ID == "" {
		return nil, fmt.Errorf("navigation profile %q is not present", profileID)
	}
	graphs := make([]content.NavGraphTileDef, 0, len(bake.SourceTiles))
	for _, graph := range bake.GraphTiles {
		if graph.AgentProfileID == profileID {
			graphs = append(graphs, graph)
		}
	}
	if len(bake.Manifest.Carriers) != 0 {
		return content.NewNavGraphQueryWithBlockers(
			bake.SourceTiles, graphs, bake.Manifest.ChunkSize, bake.Manifest.VoxelResolution,
			profile, content.NavCarrierBlockers(bake.Manifest.Carriers, profile),
		)
	}
	return content.NewNavGraphQuery(bake.SourceTiles, graphs, bake.Manifest.ChunkSize, bake.Manifest.VoxelResolution)
}

func navGraphBundleBytes(navPath string, manifest *content.NavGraphManifestDef) (total, source, graph int64, err error) {
	add := func(path string) (int64, error) {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return 0, statErr
		}
		return info.Size(), nil
	}
	manifestBytes, err := add(navPath)
	if err != nil {
		return 0, 0, 0, err
	}
	total = manifestBytes
	for _, entry := range manifest.SourceTiles {
		size, sizeErr := add(content.ResolveDocumentPath(entry.TilePath, navPath))
		if sizeErr != nil {
			return 0, 0, 0, sizeErr
		}
		source += size
		total += size
	}
	for _, entry := range manifest.GraphTiles {
		size, sizeErr := add(content.ResolveDocumentPath(entry.TilePath, navPath))
		if sizeErr != nil {
			return 0, 0, 0, sizeErr
		}
		graph += size
		total += size
	}
	return total, source, graph, nil
}

func navPercentile(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	values = append([]float64(nil), values...)
	sort.Float64s(values)
	index := int(math.Ceil(percentile*float64(len(values)))) - 1
	return values[max(0, min(index, len(values)-1))]
}

func millisecondsSince(start time.Time) float64 {
	return float64(time.Since(start)) / float64(time.Millisecond)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "navdiag: "+format+"\n", args...)
	os.Exit(1)
}
