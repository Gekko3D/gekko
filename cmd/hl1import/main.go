package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	var exportProfile string
	var progress bool
	flag.StringVar(&opts.GameDir, "game-dir", "", "Half-Life game directory")
	flag.StringVar(&opts.MapName, "map", "", "HL1 map name, for example c1a0")
	flag.StringVar(&opts.BSPPath, "bsp", "", "explicit BSP path; overrides -game-dir/-map lookup")
	flag.StringVar(&opts.OutputRoot, "out", "../actiongame/assets/levels", "generated content output root")
	flag.IntVar(&opts.ChunkSize, "chunk-size", hl1.DefaultImportedWorldChunkSize, "imported-world chunk size")
	opts.ChunkPayloadKind = hl1.DefaultChunkPayloadKind
	flag.StringVar(&opts.ChunkPayloadKind, "chunk-payload", hl1.DefaultChunkPayloadKind, "imported-world chunk payload: sparse_json_v1, dense_rle_binary_v1, or dense_rle_material_binary_v1")
	flag.StringVar(&exportProfile, "export-profile", "", "named export profile: default or rusty_voxelrt_interop_v1")
	flag.Int64Var(&opts.MaxSolidSampleCells, "max-solid-sample-cells", hl1.DefaultImportedMaxSampledCells, "maximum BSP solid voxel sample cells")
	flag.IntVar(&opts.SolidBandDepth, "solid-band-depth", hl1.DefaultImportedSolidBandDepth, "solid debug mode fill depth in voxels from reachable playable empty space")
	flag.Var((*hl1LightModeFlag)(&opts.LightMode), "light-mode", "HL1 light import mode: faithful or point-proxy")
	flag.BoolVar(&opts.BakeStaticLightmaps, "bake-static-lightmaps", false, "diagnostic: bake HL1 static face lightmaps into voxel albedo")
	flag.BoolVar(&opts.EmitLightFixtures, "emit-light-fixtures", false, "write tiny emissive fixture assets and placements for imported HL1 lights")
	opts.EmitEmissiveSurfaceLights = true
	flag.BoolVar(&opts.EmitEmissiveSurfaceLights, "emit-emissive-surface-lights", true, "synthesize point lights from imported emissive surface clusters")
	flag.IntVar(&opts.MaxEmissiveSurfaceLights, "max-emissive-surface-lights", hl1.DefaultMaxEmissiveSurfaceLights, "maximum synthesized emissive surface lights")
	flag.BoolVar(&opts.EmitGameAssets, "emit-game-assets", false, "copy/catalog HL1 WAD/model/sprite/sound assets referenced by the map")
	flag.BoolVar(&opts.SkipNavigationBake, "skip-navigation-bake", false, "skip generated .gknav/.gknavtile sidecars during level save")
	opts.VoxelResolution = hl1.DefaultImportedVoxelResolution
	opts.VoxelResolutionPolicy = hl1.DefaultHL1VoxelResolutionPolicy()
	flag.Var((*float32Flag)(&opts.VoxelResolution), "voxel-resolution", "world voxel resolution")
	flag.Var((*float32Flag)(&opts.VoxelResolutionPolicy.BrushModel), "brush-model-voxel-resolution", "voxel resolution for imported HL1 moving/breakable brush model assets")
	flag.Var((*float32Flag)(&opts.VoxelResolutionPolicy.Fixture), "fixture-voxel-resolution", "voxel resolution for imported HL1 fixture assets such as lamps")
	flag.Var((*float32Flag)(&opts.VoxelResolutionPolicy.StaticProp), "static-prop-voxel-resolution", "voxel resolution for imported HL1 non-pickup model/sprite assets")
	flag.Var((*float32Flag)(&opts.VoxelResolutionPolicy.NPC), "npc-voxel-resolution", "voxel resolution for imported HL1 NPC/monster model assets")
	flag.Var((*float32Flag)(&opts.VoxelResolutionPolicy.Pickup), "pickup-voxel-resolution", "voxel resolution for imported HL1 weapon/ammo/pickup assets")
	flag.Var((*float32Flag)(&opts.GameAssetVoxelResolution), "game-asset-voxel-resolution", "deprecated alias for -static-prop-voxel-resolution")
	flag.StringVar(&reportPath, "report", "", "report output path")
	flag.BoolVar(&emitDebugWorld, "emit-debug-world", false, "write debug .gkworld/.gkchunk output")
	flag.StringVar(&debugWorldMode, "debug-world-mode", string(hl1.DebugWorldModeSurface), "debug world mode: surface or solid")
	flag.BoolVar(&emitLevel, "emit-level", false, "write generated .gklevel pointing at emitted debug world")
	flag.BoolVar(&progress, "progress", false, "print timestamped import progress while running")
	flag.Parse()

	opts.ExportProfile = hl1.HL1ExportProfile(exportProfile)
	var profileErr error
	opts, profileErr = hl1.ApplyHL1ExportProfile(opts)
	if profileErr != nil {
		fatalf("%v", profileErr)
	}
	if opts.MapName == "" && opts.BSPPath == "" {
		fatalf("-map or -bsp is required")
	}
	if opts.GameDir == "" && opts.BSPPath == "" {
		fatalf("-game-dir is required unless -bsp is provided")
	}
	if opts.VoxelResolution <= 0 {
		fatalf("-voxel-resolution must be positive")
	}
	policy := hl1.EffectiveHL1VoxelResolutionPolicy(opts)
	if policy.BrushModel <= 0 {
		fatalf("-brush-model-voxel-resolution must be positive")
	}
	if policy.Fixture <= 0 {
		fatalf("-fixture-voxel-resolution must be positive")
	}
	if policy.StaticProp <= 0 {
		fatalf("-static-prop-voxel-resolution must be positive")
	}
	if policy.NPC <= 0 {
		fatalf("-npc-voxel-resolution must be positive")
	}
	if policy.Pickup <= 0 {
		fatalf("-pickup-voxel-resolution must be positive")
	}
	if _, err := content.NormalizeImportedWorldChunkPayloadKind(opts.ChunkPayloadKind); err != nil {
		fatalf("%v", err)
	}
	progressPrinter := newHL1ImportProgressPrinter(progress)
	opts.Progress = progressPrinter.Event
	if emitLevel {
		emitDebugWorld = true
	}
	progressPrinter.Start(hl1.ImportProgressStageBuildSummary, "")
	summary, err := hl1.BuildImportSummary(opts)
	if err != nil {
		fatalf("%v", err)
	}
	progressPrinter.Done(hl1.ImportProgressStageBuildSummary, summary.Report.Source.BSPPath)
	var debugResult hl1.DebugWorldEmissionResult
	if emitDebugWorld {
		progressPrinter.Start(hl1.ImportProgressStageBuildDebugWorld, "")
		debugResult, err = hl1.BuildDebugWorld(opts, hl1.DebugWorldMode(debugWorldMode))
		if err != nil {
			fatalf("build debug world: %v", err)
		}
		progressPrinter.Done(hl1.ImportProgressStageBuildDebugWorld, debugResult.ManifestPath)
		summary.Report.GeneratedWorldPath = debugResult.ManifestPath
		summary.Report.ChunkCount = len(debugResult.Emission.Chunks)
		summary.Report.NonEmptyVoxelCount = debugResult.Emission.TotalVoxelCount
		summary.Report.Diagnostics = append(summary.Report.Diagnostics, debugResult.Diagnostics...)
		hl1.PopulateMaterialAnimationReport(&summary.Report, debugResult.Emission.Manifest)
	}
	var gameAssets hl1.GameAssetImportResult
	if opts.EmitGameAssets {
		progressPrinter.Start(hl1.ImportProgressStageBuildGameAssets, "")
		gameAssets, err = hl1.BuildGameAssetImport(opts, summary)
		if err != nil {
			fatalf("build game assets: %v", err)
		}
		progressPrinter.Done(hl1.ImportProgressStageBuildGameAssets, gameAssets.ManifestPath)
		summary.Report.Diagnostics = append(summary.Report.Diagnostics, gameAssets.Manifest.Diagnostics...)
	}
	var levelResult hl1.GeneratedLevelResult
	if emitLevel {
		progressPrinter.Start(hl1.ImportProgressStageBuildLevel, "")
		if opts.EmitGameAssets {
			levelResult, err = hl1.BuildGeneratedLevelWithGameAssets(opts, summary, debugResult.ManifestPath, gameAssets, debugResult.Voxelize)
		} else {
			levelResult, err = hl1.BuildGeneratedLevel(opts, summary, debugResult.ManifestPath, debugResult.Voxelize)
		}
		if err != nil {
			fatalf("build level: %v", err)
		}
		progressPrinter.Done(hl1.ImportProgressStageBuildLevel, levelResult.LevelPath)
		summary.Report.GeneratedLevelPath = levelResult.LevelPath
	}
	var debugSaveStats importcommon.ImportedWorldSaveStats
	if emitDebugWorld {
		progressPrinter.Start(hl1.ImportProgressStageSaveDebugWorld, debugResult.ManifestPath)
		debugSaveStats, err = hl1.SaveDebugWorldWithStats(debugResult)
		if err != nil {
			fatalf("save debug world: %v", err)
		}
		progressPrinter.Done(hl1.ImportProgressStageSaveDebugWorld, debugResult.ManifestPath)
	}
	if emitLevel {
		progressPrinter.Start(hl1.ImportProgressStageSaveLevel, levelResult.LevelPath)
		if err := hl1.SaveGeneratedLevel(levelResult); err != nil {
			fatalf("save level: %v", err)
		}
		progressPrinter.Done(hl1.ImportProgressStageSaveLevel, levelResult.LevelPath)
	}
	if opts.EmitGameAssets {
		progressPrinter.Start(hl1.ImportProgressStageSaveGameAssets, gameAssets.ManifestPath)
		if err := hl1.SaveGameAssetImport(gameAssets); err != nil {
			fatalf("save game assets: %v", err)
		}
		progressPrinter.Done(hl1.ImportProgressStageSaveGameAssets, gameAssets.ManifestPath)
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
	progressPrinter.Event(hl1.ImportProgress{Stage: hl1.ImportProgressStageSaveReport, Current: 1, Total: 1, Path: reportPath})
	fmt.Printf("HL1 import report written: %s\n", reportPath)
	fmt.Printf("BSP: %s\n", summary.Report.Source.BSPPath)
	fmt.Printf("materials: %d\n", summary.Report.MaterialCount)
	if summary.Report.MaterialAnimationCount > 0 {
		fmt.Printf("material animations: %d (%d animated material values, %d frame records)\n", summary.Report.MaterialAnimationCount, summary.Report.AnimatedMaterialCount, summary.Report.MaterialAnimationFrameCount)
	}
	fmt.Printf("world faces: %d (sky: %d)\n", summary.Report.FaceCount, summary.Report.SkyFaceCount)
	fmt.Printf("entities: %d class(es)\n", len(summary.Report.EntityCounts))
	fmt.Printf("diagnostics: %d\n", len(summary.Report.Diagnostics))
	if emitDebugWorld {
		fmt.Printf("debug world written: %s\n", debugResult.ManifestPath)
		fmt.Printf("debug world mode: %s\n", debugResult.Mode)
		fmt.Printf("debug world chunk payload: %s\n", debugWorldPayloadKind(debugResult))
		fmt.Printf("aux sidecars: %d\n", importedWorldAuxSidecarCount(debugResult.Emission.Manifest))
		fmt.Printf("incremental save: %s\n", hl1ImportSaveStatsString(debugSaveStats))
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
		fmt.Printf("charger assets: %d\n", len(levelResult.ChargerAssets))
		fmt.Printf("trigger volumes: %d\n", len(levelResult.Level.TriggerVolumes))
		fmt.Printf("damage volumes: %d\n", len(levelResult.Level.DamageVolumes))
		fmt.Printf("changelevel volumes: %d\n", len(levelResult.Level.ChangeLevels))
		fmt.Printf("multi-targets: %d\n", len(levelResult.Level.MultiTargets))
		fmt.Printf("target relays: %d\n", len(levelResult.Level.TargetRelays))
		fmt.Printf("breakables: %d\n", len(levelResult.Level.Breakables))
		fmt.Printf("pickups: %d\n", len(levelResult.Level.Pickups))
		fmt.Printf("npcs: %d\n", len(levelResult.Level.NPCs))
		fmt.Printf("light fixture assets: %d\n", len(levelResult.LightFixtureAssets))
		fmt.Printf("moving brush assets: %d\n", len(levelResult.MovingBrushAssets))
		fmt.Printf("breakable assets: %d\n", len(levelResult.BreakableAssets))
	}
	if opts.EmitGameAssets {
		fmt.Printf("game asset manifest written: %s\n", gameAssets.ManifestPath)
		fmt.Printf("game assets: %d\n", len(gameAssets.Manifest.Assets))
	}
	if opts.ExportProfile != hl1.HL1ExportProfileDefault {
		fmt.Printf("export profile: %s\n", opts.ExportProfile)
	}
}

func hl1ImportSaveStatsString(stats importcommon.ImportedWorldSaveStats) string {
	return fmt.Sprintf("chunks written=%d skipped=%d aux written=%d skipped=%d reused=%d proxies written=%d skipped=%d proxy_aux written=%d skipped=%d reused=%d",
		stats.ChunksWritten,
		stats.ChunksSkipped,
		stats.ChunkAuxWritten,
		stats.ChunkAuxSkipped,
		stats.ChunkAuxReused,
		stats.ProxyChunksWritten,
		stats.ProxyChunksSkipped,
		stats.ProxyAuxWritten,
		stats.ProxyAuxSkipped,
		stats.ProxyAuxReused,
	)
}

func importedWorldAuxSidecarCount(def *content.ImportedWorldDef) int {
	if def == nil {
		return 0
	}
	count := 0
	for _, entry := range def.Entries {
		if entry.Aux != nil && entry.Aux.AuxPath != "" {
			count++
		}
	}
	for _, sector := range def.Sectors {
		for _, lod := range sector.LODs {
			if lod.Aux != nil && lod.Aux.AuxPath != "" {
				count++
			}
		}
	}
	return count
}

func debugWorldPayloadKind(result hl1.DebugWorldEmissionResult) string {
	if result.Emission.Manifest != nil && result.Emission.Manifest.ChunkPayloadKind != "" {
		return result.Emission.Manifest.ChunkPayloadKind
	}
	return result.PayloadKind
}

type hl1ImportProgressPrinter struct {
	enabled bool
	stages  map[string]time.Time
}

func newHL1ImportProgressPrinter(enabled bool) *hl1ImportProgressPrinter {
	return &hl1ImportProgressPrinter{
		enabled: enabled,
		stages:  make(map[string]time.Time),
	}
}

func (p *hl1ImportProgressPrinter) Start(stage string, path string) {
	if p == nil || !p.enabled {
		return
	}
	p.stages[stage] = time.Now()
	p.print("start", hl1.ImportProgress{
		Stage: stage,
		Path:  path,
	})
}

func (p *hl1ImportProgressPrinter) Done(stage string, path string) {
	if p == nil || !p.enabled {
		return
	}
	detail := "done"
	if started, ok := p.stages[stage]; ok {
		detail = fmt.Sprintf("done elapsed=%s", time.Since(started).Round(time.Millisecond))
		delete(p.stages, stage)
	}
	p.print(detail, hl1.ImportProgress{
		Stage:   stage,
		Current: 1,
		Total:   1,
		Path:    path,
	})
}

func (p *hl1ImportProgressPrinter) Event(event hl1.ImportProgress) {
	if p == nil || !p.enabled {
		return
	}
	p.print("progress", event)
}

func (p *hl1ImportProgressPrinter) print(kind string, event hl1.ImportProgress) {
	if p == nil || !p.enabled {
		return
	}
	parts := []string{
		fmt.Sprintf("[%s]", time.Now().Format("15:04:05")),
		"progress",
		kind,
		"stage=" + event.Stage,
	}
	if event.Total > 0 {
		if event.Current > 0 {
			parts = append(parts, fmt.Sprintf("%d/%d", event.Current, event.Total))
		} else {
			parts = append(parts, fmt.Sprintf("total=%d", event.Total))
		}
	}
	if progressEventHasCoord(event) {
		parts = append(parts, "coord="+content.TerrainChunkKey(event.Coord))
	}
	if event.AgentProfileID != "" {
		parts = append(parts, "profile="+event.AgentProfileID)
	}
	if event.Polygons > 0 {
		parts = append(parts, fmt.Sprintf("polys=%d", event.Polygons))
	}
	if event.Path != "" {
		parts = append(parts, "path="+event.Path)
	}
	fmt.Println(strings.Join(parts, " "))
}

func progressEventHasCoord(event hl1.ImportProgress) bool {
	if event.Coord != (content.TerrainChunkCoordDef{}) {
		return true
	}
	stage := event.Stage
	return strings.Contains(stage, "load_chunk") ||
		strings.Contains(stage, "build_tile") ||
		strings.Contains(stage, "skip_tile") ||
		strings.Contains(stage, "save_tile")
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
