package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gekko3d/gekko/content"
)

func main() {
	var worldPath, outputPath, profileID string
	var radius, height, stepHeight, maxSlope float64
	flag.StringVar(&worldPath, "world", "", "input .gkworld manifest")
	flag.StringVar(&outputPath, "out", "", "output .gknav manifest")
	flag.StringVar(&profileID, "profile", "walker", "agent profile id")
	flag.Float64Var(&radius, "radius", 0.3, "agent radius")
	flag.Float64Var(&height, "height", 1.8, "agent height")
	flag.Float64Var(&stepHeight, "step-height", 0.5, "agent step height")
	flag.Float64Var(&maxSlope, "max-slope", 50, "agent maximum slope in degrees")
	flag.Parse()
	if worldPath == "" || outputPath == "" {
		fatalf("-world and -out are required")
	}
	profile := content.NavAgentProfileDef{ID: profileID, Radius: float32(radius), Height: float32(height), StepHeight: float32(stepHeight), MaxSlopeDegrees: float32(maxSlope)}
	bake, err := content.BakeImportedWorldNavGraph(worldPath, []content.NavAgentProfileDef{profile})
	if err != nil {
		fatalf("bake: %v", err)
	}
	if err := content.SaveNavGraphBake(outputPath, &bake); err != nil {
		fatalf("save: %v", err)
	}
	summary := content.DiagnoseNavGraphBake(&bake)
	fmt.Printf("nav graph written: %s\n", outputPath)
	fmt.Printf("source_tiles=%d graph_tiles=%d spans=%d accepted=%d regions=%d span_transitions=%d region_transitions=%d hard_errors=%d\n",
		summary.SourceTiles, summary.GraphTiles, summary.Spans, summary.AcceptedSpans, summary.Regions, summary.SpanTransitions, summary.RegionTransitions, summary.Validation.HardErrorCount)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "navbake: "+format+"\n", args...)
	os.Exit(1)
}
