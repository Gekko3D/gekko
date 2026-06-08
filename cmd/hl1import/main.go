package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
	"github.com/gekko3d/gekko/importers/hl1"
)

func main() {
	var opts hl1.ImportOptions
	var reportPath string
	var emitDebugWorld bool
	var emitLevel bool
	var debugWorldMode string
	flag.StringVar(&opts.GameDir, "game-dir", "", "Half-Life game directory")
	flag.StringVar(&opts.MapName, "map", "", "HL1 map name, for example c1a0")
	flag.StringVar(&opts.BSPPath, "bsp", "", "explicit BSP path; overrides -game-dir/-map lookup")
	flag.StringVar(&opts.OutputRoot, "out", "../actiongame/assets/levels", "generated content output root")
	flag.IntVar(&opts.ChunkSize, "chunk-size", hl1.DefaultImportedWorldChunkSize, "imported-world chunk size")
	opts.ChunkPayloadKind = hl1.DefaultChunkPayloadKind
	flag.StringVar(&opts.ChunkPayloadKind, "chunk-payload", hl1.DefaultChunkPayloadKind, "imported-world chunk payload: sparse_json_v1 or dense_rle_binary_v1")
	flag.Int64Var(&opts.MaxSolidSampleCells, "max-solid-sample-cells", hl1.DefaultImportedMaxSampledCells, "maximum BSP solid voxel sample cells")
	flag.IntVar(&opts.SolidBandDepth, "solid-band-depth", hl1.DefaultImportedSolidBandDepth, "solid debug mode fill depth in voxels from reachable playable empty space")
	flag.Var((*hl1LightModeFlag)(&opts.LightMode), "light-mode", "HL1 light import mode: faithful or point-proxy")
	flag.BoolVar(&opts.EmitLightFixtures, "emit-light-fixtures", false, "write tiny emissive fixture assets and placements for imported HL1 lights")
	opts.EmitEmissiveSurfaceLights = true
	flag.BoolVar(&opts.EmitEmissiveSurfaceLights, "emit-emissive-surface-lights", true, "synthesize point lights from imported emissive surface clusters")
	flag.IntVar(&opts.MaxEmissiveSurfaceLights, "max-emissive-surface-lights", hl1.DefaultMaxEmissiveSurfaceLights, "maximum synthesized emissive surface lights")
	flag.BoolVar(&opts.EmitGameAssets, "emit-game-assets", false, "copy/catalog HL1 WAD/model/sprite/sound assets referenced by the map")
	opts.VoxelResolution = hl1.DefaultImportedVoxelResolution
	opts.GameAssetVoxelResolution = hl1.DefaultGameAssetVoxelResolution
	opts.PickupVoxelResolution = hl1.DefaultPickupVoxelResolution
	flag.Var((*float32Flag)(&opts.VoxelResolution), "voxel-resolution", "world voxel resolution")
	flag.Var((*float32Flag)(&opts.GameAssetVoxelResolution), "game-asset-voxel-resolution", "voxel resolution for imported HL1 model/sprite assets")
	flag.Var((*float32Flag)(&opts.PickupVoxelResolution), "pickup-voxel-resolution", "voxel resolution for imported HL1 pickup/item assets")
	flag.StringVar(&reportPath, "report", "", "report output path")
	flag.BoolVar(&emitDebugWorld, "emit-debug-world", false, "write debug .gkworld/.gkchunk output")
	flag.StringVar(&debugWorldMode, "debug-world-mode", string(hl1.DebugWorldModeSurface), "debug world mode: surface or solid")
	flag.BoolVar(&emitLevel, "emit-level", false, "write generated .gklevel pointing at emitted debug world")
	flag.Parse()

	if opts.MapName == "" && opts.BSPPath == "" {
		fatalf("-map or -bsp is required")
	}
	if opts.GameDir == "" && opts.BSPPath == "" {
		fatalf("-game-dir is required unless -bsp is provided")
	}
	if opts.VoxelResolution <= 0 {
		fatalf("-voxel-resolution must be positive")
	}
	if opts.GameAssetVoxelResolution <= 0 {
		fatalf("-game-asset-voxel-resolution must be positive")
	}
	if opts.PickupVoxelResolution <= 0 {
		fatalf("-pickup-voxel-resolution must be positive")
	}
	if _, err := content.NormalizeImportedWorldChunkPayloadKind(opts.ChunkPayloadKind); err != nil {
		fatalf("%v", err)
	}
	if emitLevel {
		emitDebugWorld = true
	}
	summary, err := hl1.BuildImportSummary(opts)
	if err != nil {
		fatalf("%v", err)
	}
	var debugResult hl1.DebugWorldEmissionResult
	if emitDebugWorld {
		debugResult, err = hl1.BuildDebugWorld(opts, hl1.DebugWorldMode(debugWorldMode))
		if err != nil {
			fatalf("build debug world: %v", err)
		}
		summary.Report.GeneratedWorldPath = debugResult.ManifestPath
		summary.Report.ChunkCount = len(debugResult.Emission.Chunks)
		summary.Report.NonEmptyVoxelCount = debugResult.Emission.TotalVoxelCount
	}
	var gameAssets hl1.GameAssetImportResult
	if opts.EmitGameAssets {
		gameAssets, err = hl1.BuildGameAssetImport(opts, summary)
		if err != nil {
			fatalf("build game assets: %v", err)
		}
		summary.Report.Diagnostics = append(summary.Report.Diagnostics, gameAssets.Manifest.Diagnostics...)
	}
	var levelResult hl1.GeneratedLevelResult
	if emitLevel {
		if opts.EmitGameAssets {
			levelResult, err = hl1.BuildGeneratedLevelWithGameAssets(opts, summary, debugResult.ManifestPath, gameAssets, debugResult.Voxelize)
		} else {
			levelResult, err = hl1.BuildGeneratedLevel(opts, summary, debugResult.ManifestPath, debugResult.Voxelize)
		}
		if err != nil {
			fatalf("build level: %v", err)
		}
		summary.Report.GeneratedLevelPath = levelResult.LevelPath
	}
	if emitDebugWorld {
		if err := hl1.SaveDebugWorld(debugResult); err != nil {
			fatalf("save debug world: %v", err)
		}
	}
	if emitLevel {
		if err := hl1.SaveGeneratedLevel(levelResult); err != nil {
			fatalf("save level: %v", err)
		}
	}
	if opts.EmitGameAssets {
		if err := hl1.SaveGameAssetImport(gameAssets); err != nil {
			fatalf("save game assets: %v", err)
		}
	}
	if reportPath == "" {
		mapName := summary.Report.Source.MapName
		if mapName == "" {
			mapName = "hl1_map"
		}
		reportPath = filepath.Join(opts.OutputRoot, "worlds", mapName+"_import_report.json")
	}
	if err := importcommon.SaveImportReport(reportPath, summary.Report); err != nil {
		fatalf("save report: %v", err)
	}
	fmt.Printf("HL1 import report written: %s\n", reportPath)
	fmt.Printf("BSP: %s\n", summary.Report.Source.BSPPath)
	fmt.Printf("materials: %d\n", summary.Report.MaterialCount)
	fmt.Printf("world faces: %d (sky: %d)\n", summary.Report.FaceCount, summary.Report.SkyFaceCount)
	fmt.Printf("entities: %d class(es)\n", len(summary.Report.EntityCounts))
	fmt.Printf("diagnostics: %d\n", len(summary.Report.Diagnostics))
	if emitDebugWorld {
		fmt.Printf("debug world written: %s\n", debugResult.ManifestPath)
		fmt.Printf("debug world mode: %s\n", debugResult.Mode)
		fmt.Printf("debug world chunk payload: %s\n", debugResult.PayloadKind)
		fmt.Printf("debug voxels: %d surface, %d filled, %d chunks\n", debugResult.Voxelize.SurfaceCount, debugResult.Voxelize.FilledCount, len(debugResult.Emission.Chunks))
		if debugResult.Mode == hl1.DebugWorldModeSolid {
			fmt.Printf("sampled cells: %d, playable empty: %d, solid band depth: %d\n", debugResult.Voxelize.SampledCount, debugResult.Voxelize.PlayableEmptyCount, opts.SolidBandDepth)
			if debugResult.Voxelize.FloodSkipped {
				fmt.Printf("playable empty flood: skipped or empty; surface-guided solid band used when needed\n")
			}
		}
	}
	if emitLevel {
		fmt.Printf("level written: %s\n", levelResult.LevelPath)
		fmt.Printf("player spawn marker kind: %s\n", hl1.MarkerKindHL1PlayerSpawn)
		fmt.Printf("water bodies: %d\n", len(levelResult.Level.WaterBodies))
		fmt.Printf("ladder volumes: %d\n", len(levelResult.Level.LadderVolumes))
		fmt.Printf("moving brushes: %d\n", len(levelResult.Level.MovingBrushes))
		fmt.Printf("path nodes: %d\n", len(levelResult.Level.PathNodes))
		fmt.Printf("use triggers: %d\n", len(levelResult.Level.UseTriggers))
		fmt.Printf("chargers: %d\n", len(levelResult.Level.Chargers))
		fmt.Printf("trigger volumes: %d\n", len(levelResult.Level.TriggerVolumes))
		fmt.Printf("damage volumes: %d\n", len(levelResult.Level.DamageVolumes))
		fmt.Printf("changelevel volumes: %d\n", len(levelResult.Level.ChangeLevels))
		fmt.Printf("multi-targets: %d\n", len(levelResult.Level.MultiTargets))
		fmt.Printf("target relays: %d\n", len(levelResult.Level.TargetRelays))
		fmt.Printf("breakables: %d\n", len(levelResult.Level.Breakables))
		fmt.Printf("pickups: %d\n", len(levelResult.Level.Pickups))
		fmt.Printf("light fixture assets: %d\n", len(levelResult.LightFixtureAssets))
		fmt.Printf("moving brush assets: %d\n", len(levelResult.MovingBrushAssets))
		fmt.Printf("breakable assets: %d\n", len(levelResult.BreakableAssets))
	}
	if opts.EmitGameAssets {
		fmt.Printf("game asset manifest written: %s\n", gameAssets.ManifestPath)
		fmt.Printf("game assets: %d\n", len(gameAssets.Manifest.Assets))
	}
}

type float32Flag float32

func (f *float32Flag) Set(value string) error {
	parsed, err := strconv.ParseFloat(value, 32)
	if err != nil {
		return err
	}
	*f = float32Flag(float32(parsed))
	return nil
}

func (f *float32Flag) String() string {
	return fmt.Sprintf("%g", float32(*f))
}

type hl1LightModeFlag hl1.HL1LightMode

func (f *hl1LightModeFlag) Set(value string) error {
	mode := hl1.HL1LightMode(value)
	switch mode {
	case hl1.HL1LightModeFaithful, hl1.HL1LightModePointProxy:
		*f = hl1LightModeFlag(mode)
		return nil
	default:
		return fmt.Errorf("expected %q or %q, got %q", hl1.HL1LightModeFaithful, hl1.HL1LightModePointProxy, value)
	}
}

func (f *hl1LightModeFlag) String() string {
	if *f == "" {
		return string(hl1.HL1LightModeFaithful)
	}
	return string(*f)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "hl1import: "+format+"\n", args...)
	os.Exit(1)
}
