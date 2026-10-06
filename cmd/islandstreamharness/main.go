package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/derived"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("islandstreamharness", flag.ContinueOnError)
	flags.SetOutput(stderr)
	gasworks := flags.String("gasworks", "", "existing Gasworks .gkworld (read-only)")
	out := flags.String("out", "", "harness output directory")
	playable := flags.Float64("playable-span", 15000, "playable X/Z span in meters")
	coverage := flags.Float64("coverage-span", 16384, "padded X/Z coverage in meters")
	root := flags.Float64("root-span", 2048, "root page span in meters")
	macro := flags.Float64("macro-span", 512, "macro page span in meters")
	regional := flags.Float64("regional-span", 128, "regional page span in meters")
	tile := flags.Float64("height-tile-span", 256, "backing height tile span in meters")
	spacing := flags.Float64("height-sample-spacing", 2, "backing height sample spacing in meters")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	if *gasworks == "" || *out == "" {
		fmt.Fprintln(stderr, "-gasworks and -out are required")
		return 2
	}
	for _, v := range []float64{*playable, *coverage, *root, *macro, *regional, *tile, *spacing} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || math.IsInf(float64(float32(v)), 0) || float32(v) <= 0 {
			fmt.Fprintln(stderr, "spans and spacing must be finite positive float32 values")
			return 2
		}
	}
	options := derived.IslandStreamHarnessOptions{PlayableSpan: float32(*playable), CoverageSpan: float32(*coverage), RootSpan: float32(*root), MacroSpan: float32(*macro), RegionalSpan: float32(*regional), HeightTileSpan: float32(*tile), HeightSampleSpacing: float32(*spacing)}
	source, err := content.LoadImportedWorld(*gasworks)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err = derived.ValidateIslandStreamHarnessOutput(*out, *gasworks, source); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	entryIndices := make(map[content.TerrainChunkCoordDef]uint32, len(source.Entries))
	for i, e := range source.Entries {
		entryIndices[e.Coord] = uint32(i)
	}
	load := func(entry content.ImportedWorldChunkEntryDef) (*content.ImportedWorldChunkDef, error) {
		index, ok := entryIndices[entry.Coord]
		if !ok {
			return nil, fmt.Errorf("unknown source coordinate")
		}
		return content.LoadImportedWorldChunkEntry(source, *gasworks, index)
	}
	harness, err := derived.BuildIslandStreamHarness(source, load, options)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err = derived.SaveIslandStreamHarness(*out, *gasworks, harness); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	roots, bytes := 0, 0
	for _, p := range harness.Terrain.Pages {
		if p.Level == content.StreamPageLevelRoot {
			roots++
			bytes += p.Payload.PayloadSizeBytes
		}
	}
	fmt.Fprintf(stdout, "Published %s: %d terrain roots, %d root height body bytes, %d source tiles, %d borrowed FULL entries. GPU residency is unmeasured.\n", *out, roots, bytes, len(harness.SourceTiles), len(harness.POI.Entries))
	return 0
}
