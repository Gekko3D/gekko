package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/gekko3d/gekko/content"
)

func main() {
	var levelPath, worldPath, outputPath, profileID, capabilities string
	var radius, height, stepHeight, maxSlope, maxDrop, maxJump, maxJumpRise, maxVault, maxMantle, jumpSpeed, jumpHorizontalSpeed, gravity float64
	flag.StringVar(&levelPath, "level", "", "input .gklevel with authored traversal")
	flag.StringVar(&worldPath, "world", "", "input .gkworld manifest")
	flag.StringVar(&outputPath, "out", "", "output .gknav manifest")
	flag.StringVar(&profileID, "profile", "walker", "agent profile id")
	flag.Float64Var(&radius, "radius", 0.3, "agent radius")
	flag.Float64Var(&height, "height", 1.8, "agent height")
	flag.Float64Var(&stepHeight, "step-height", 0.5, "agent step height")
	flag.Float64Var(&maxSlope, "max-slope", 50, "agent maximum slope in degrees")
	flag.Float64Var(&maxDrop, "max-drop-height", 3, "agent maximum safe drop")
	flag.Float64Var(&maxJump, "max-jump-distance", 3, "agent maximum jump distance")
	flag.Float64Var(&maxJumpRise, "max-jump-rise", 1, "agent maximum jump rise")
	flag.Float64Var(&maxVault, "max-vault-height", 0.8, "agent maximum vault height")
	flag.Float64Var(&maxMantle, "max-mantle-height", 1.4, "agent maximum mantle height")
	flag.Float64Var(&jumpSpeed, "jump-speed", 5.5, "agent jump launch speed")
	flag.Float64Var(&jumpHorizontalSpeed, "jump-horizontal-speed", 4.5, "agent maximum horizontal jump speed")
	flag.Float64Var(&gravity, "gravity", 18, "agent gravity")
	flag.StringVar(&capabilities, "capabilities", strings.Join([]string{content.NavCapabilityClimbLadder, content.NavCapabilityJump, content.NavCapabilityMantle, content.NavCapabilityVault}, ","), "comma-separated traversal capabilities")
	flag.Parse()
	if outputPath == "" || (levelPath == "") == (worldPath == "") {
		fatalf("-out and exactly one of -level or -world are required")
	}
	profile := content.NavAgentProfileDef{
		ID: profileID, Radius: float32(radius), Height: float32(height), StepHeight: float32(stepHeight), MaxSlopeDegrees: float32(maxSlope),
		MaxDropHeight: float32(maxDrop), MaxJumpDistance: float32(maxJump), MaxJumpRise: float32(maxJumpRise),
		MaxVaultHeight: float32(maxVault), MaxMantleHeight: float32(maxMantle),
		JumpSpeed: float32(jumpSpeed), JumpHorizontalSpeed: float32(jumpHorizontalSpeed), Gravity: float32(gravity),
		Capabilities: splitCapabilities(capabilities),
	}
	var bake content.NavGraphBakeResult
	var err error
	if levelPath != "" {
		bake, err = content.BakeLevelNavGraph(levelPath, []content.NavAgentProfileDef{profile})
	} else {
		bake, err = content.BakeImportedWorldNavGraph(worldPath, []content.NavAgentProfileDef{profile})
	}
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

func splitCapabilities(value string) []string {
	seen := map[string]struct{}{}
	for _, capability := range strings.Split(value, ",") {
		if capability = strings.TrimSpace(capability); capability != "" {
			seen[capability] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for capability := range seen {
		result = append(result, capability)
	}
	sort.Strings(result)
	return result
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "navbake: "+format+"\n", args...)
	os.Exit(1)
}
