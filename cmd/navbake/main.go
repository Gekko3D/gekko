package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/gekko3d/gekko/content"
)

func main() {
	var worldPath string
	var navPath string
	var navID string
	var tileDir string
	var buildSourcePath string
	var builderVersion string
	var cpuProfilePath string
	var buildWorkers int
	var progress bool
	flag.StringVar(&worldPath, "world", "", "path to imported .gkworld manifest")
	flag.StringVar(&navPath, "nav", "", "output .gknav manifest path; defaults next to -world")
	flag.StringVar(&navID, "nav-id", "", "optional generated nav_id")
	flag.StringVar(&tileDir, "tile-dir", "", "optional tile directory name relative to nav manifest")
	flag.StringVar(&buildSourcePath, "build-source", "", "optional .gknavsource path")
	flag.StringVar(&builderVersion, "builder-version", "", "nav builder version; defaults to current engine builder")
	flag.StringVar(&cpuProfilePath, "cpuprofile", "", "optional path to write Go CPU profile")
	flag.IntVar(&buildWorkers, "build-workers", 0, "parallel build_intermediate workers; <=0 uses GOMAXPROCS")
	flag.BoolVar(&progress, "progress", false, "print timestamped nav bake progress")
	flag.Parse()

	if strings.TrimSpace(worldPath) == "" {
		fatalf("-world is required")
	}
	if strings.TrimSpace(cpuProfilePath) != "" {
		stopCPUProfile := startCPUProfile(cpuProfilePath)
		defer stopCPUProfile()
	}
	if strings.TrimSpace(builderVersion) == "" {
		builderVersion = content.DefaultNavBakeBuilderVersion
	}

	opts := content.NavBakeOptions{
		NavID:              navID,
		TileDirectoryName:  tileDir,
		BuilderVersion:     builderVersion,
		BuildSourcePath:    buildSourcePath,
		BuildSourcePrimary: strings.TrimSpace(buildSourcePath) != "",
		BuildWorkers:       buildWorkers,
	}
	if progress {
		opts.Progress = printNavBakeProgress
	}
	result, err := content.SaveNavBakeForImportedWorldManifest(worldPath, navPath, opts)
	if err != nil {
		fatalf("bake nav: %v", err)
	}
	if navPath == "" {
		navPath = content.DefaultNavManifestPath(worldPath)
	}
	fmt.Printf("nav written: %s\n", navPath)
	if result.Manifest != nil {
		fmt.Printf("nav_id: %s\n", result.Manifest.NavID)
		fmt.Printf("builder_version: %s\n", result.Manifest.BuilderVersion)
		fmt.Printf("agent_profiles: %d\n", len(result.Manifest.AgentProfiles))
		fmt.Printf("manifest_tiles: %d\n", len(result.Manifest.Tiles))
	}
	fmt.Printf("saved_tiles: %d\n", len(result.Tiles))
}

func printNavBakeProgress(progress content.NavBakeProgress) {
	timestamp := time.Now().Format("15:04:05")
	parts := []string{
		fmt.Sprintf("[%s]", timestamp),
		"progress",
		"stage=" + progress.Stage,
	}
	if progress.Total > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d", progress.Current, progress.Total))
	}
	if progress.HasCoord {
		parts = append(parts, "coord="+content.TerrainChunkKey(progress.Coord))
	}
	if progress.AgentProfileID != "" {
		parts = append(parts, "profile="+progress.AgentProfileID)
	}
	if progress.Polygons > 0 {
		parts = append(parts, fmt.Sprintf("polys=%d", progress.Polygons))
	}
	if progress.Portals > 0 {
		parts = append(parts, fmt.Sprintf("portals=%d", progress.Portals))
	}
	if progress.Duration > 0 {
		parts = append(parts, "duration="+progress.Duration.Round(time.Millisecond).String())
	}
	if progress.Stats.OccupiedVoxels > 0 {
		parts = append(parts, fmt.Sprintf("occupied=%d", progress.Stats.OccupiedVoxels))
	}
	if progress.Stats.CandidateSpans > 0 {
		parts = append(parts, fmt.Sprintf("candidate_spans=%d", progress.Stats.CandidateSpans))
	}
	if progress.Stats.AcceptedSpans > 0 {
		parts = append(parts, fmt.Sprintf("accepted_spans=%d", progress.Stats.AcceptedSpans))
	}
	if progress.Stats.CompactCells > 0 {
		parts = append(parts, fmt.Sprintf("compact_cells=%d", progress.Stats.CompactCells))
	}
	if progress.Stats.Regions > 0 {
		parts = append(parts, fmt.Sprintf("regions=%d", progress.Stats.Regions))
	}
	if progress.TilePath != "" {
		parts = append(parts, "path="+progress.TilePath)
	}
	fmt.Println(strings.Join(parts, " "))
}

func startCPUProfile(path string) func() {
	file, err := os.Create(path)
	if err != nil {
		fatalf("create cpu profile: %v", err)
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		_ = file.Close()
		fatalf("start cpu profile: %v", err)
	}
	return func() {
		pprof.StopCPUProfile()
		if err := file.Close(); err != nil {
			fatalf("close cpu profile: %v", err)
		}
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "navbake: "+format+"\n", args...)
	os.Exit(1)
}
