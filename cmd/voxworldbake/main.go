package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gekko3d/gekko"
)

func main() {
	var (
		sourcePath       string
		manifestPath     string
		worldID          string
		chunkSize        int
		voxelResolution  float64
		reportPath       string
		normalizeOrigin  bool
		buildVersion     string
	)

	flag.StringVar(&sourcePath, "source", "", "path to the source .vox file")
	flag.StringVar(&manifestPath, "out", "", "path to the output .gkworld manifest")
	flag.StringVar(&worldID, "world-id", "", "optional authored world id")
	flag.IntVar(&chunkSize, "chunk-size", gekko.DefaultImportedWorldBakeChunkSize, "chunk size in authored voxels")
	flag.Float64Var(&voxelResolution, "voxel-resolution", gekko.DefaultImportedWorldBakeVoxelResolution, "voxel resolution multiplier")
	flag.StringVar(&reportPath, "report", "", "optional path to write a bake report JSON")
	flag.BoolVar(&normalizeOrigin, "normalize-origin", true, "shift baked voxels so the minimum occupied coordinate becomes 0,0,0")
	flag.StringVar(&buildVersion, "build-version", gekko.DefaultImportedWorldBakeBuildVersion, "source build version tag stored in the manifest")
	flag.Parse()

	if sourcePath == "" || manifestPath == "" {
		fmt.Fprintln(os.Stderr, "usage: voxworldbake -source source.vox -out assets/worlds/station.gkworld")
		flag.PrintDefaults()
		os.Exit(2)
	}

	result, err := gekko.BakeImportedWorldFromVoxFile(sourcePath, gekko.ImportedWorldBakeConfig{
		WorldID:            worldID,
		ChunkSize:          chunkSize,
		VoxelResolution:    float32(voxelResolution),
		SourceBuildVersion: buildVersion,
		NormalizeToOrigin:  normalizeOrigin,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bake failed: %v\n", err)
		os.Exit(1)
	}

	if err := gekko.SaveImportedWorldBake(manifestPath, result); err != nil {
		fmt.Fprintf(os.Stderr, "save failed: %v\n", err)
		os.Exit(1)
	}

	if reportPath != "" {
		if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "report mkdir failed: %v\n", err)
			os.Exit(1)
		}
		data, err := json.MarshalIndent(gekko.BuildImportedWorldBakeReport(result), "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "report encode failed: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(reportPath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "report write failed: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("wrote %s with %d chunk(s), %d voxel(s), %d warning(s)\n", manifestPath, len(result.Chunks), result.TotalVoxelCount, len(result.Warnings))
	for _, warning := range result.Warnings {
		fmt.Printf("- [%s] %s\n", warning.Code, warning.Message)
	}
}
