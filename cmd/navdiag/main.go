package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gekko3d/gekko/content"
)

type vec3Flag struct {
	value content.Vec3
	set   bool
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
	var start, end vec3Flag
	flag.StringVar(&navPath, "nav", "", "input .gknav manifest")
	flag.StringVar(&profileID, "profile", "", "agent profile id; defaults to first profile")
	flag.BoolVar(&jsonOutput, "json", false, "print JSON")
	flag.Var(&start, "start", "optional route start x,y,z")
	flag.Var(&end, "end", "optional route end x,y,z")
	flag.Parse()
	if navPath == "" {
		fatalf("-nav is required")
	}
	bake, err := content.LoadNavGraphBake(navPath)
	if err != nil {
		fatalf("load: %v", err)
	}
	summary := content.DiagnoseNavGraphBake(bake)
	var route *content.NavRouteResult
	if start.set != end.set {
		fatalf("-start and -end must be provided together")
	}
	if start.set {
		if profileID == "" {
			profileID = bake.Manifest.AgentProfiles[0].ID
		}
		graphs := make([]content.NavGraphTileDef, 0, len(bake.SourceTiles))
		for _, graph := range bake.GraphTiles {
			if graph.AgentProfileID == profileID {
				graphs = append(graphs, graph)
			}
		}
		result, err := content.FindNavGraphRoute(bake.SourceTiles, graphs, bake.Manifest.ChunkSize, bake.Manifest.VoxelResolution, start.value, end.value)
		if err != nil {
			fatalf("route: %v", err)
		}
		route = &result
	}
	if jsonOutput {
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			Summary content.NavGraphBakeSummary `json:"summary"`
			Route   *content.NavRouteResult     `json:"route,omitempty"`
		}{summary, route}); err != nil {
			fatalf("encode: %v", err)
		}
		return
	}
	fmt.Printf("source_tiles=%d graph_tiles=%d spans=%d accepted=%d regions=%d span_transitions=%d region_transitions=%d hard_errors=%d\n",
		summary.SourceTiles, summary.GraphTiles, summary.Spans, summary.AcceptedSpans, summary.Regions, summary.SpanTransitions, summary.RegionTransitions, summary.Validation.HardErrorCount)
	if route != nil {
		fmt.Printf("route_found=%t steps=%d waypoints=%d failure=%q failure_tile=%s\n", route.Found, len(route.Steps), len(route.Waypoints), route.FailureReason, content.TerrainChunkKey(route.FailureTile))
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "navdiag: "+format+"\n", args...)
	os.Exit(1)
}
