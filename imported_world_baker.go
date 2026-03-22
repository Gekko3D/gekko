package gekko

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	DefaultImportedWorldBakeChunkSize        = 32
	DefaultImportedWorldBakeVoxelResolution  = 1.0
	DefaultImportedWorldBakeBuildVersion     = "vox_import_v1"
	DefaultImportedWorldBakeChunkDirName     = "chunks"
	importedWorldBakeHotspotThresholdDefault = 28000
)

type ImportedWorldBakeConfig struct {
	WorldID               string
	ChunkSize             int
	VoxelResolution       float32
	ScaleMultiplier       float32
	SourceBuildVersion    string
	NormalizeToOrigin     bool
	ChunkDirectoryName    string
	ConnectedMassWarnSize int
	ThinFeatureWarnCount  int
	ChunkHotspotWarnCount int
	VerticalSpanWarnSize  int
}

type ImportedWorldBakeWarning struct {
	Code    string
	Message string
}

type ImportedWorldBakeResult struct {
	Manifest        *content.ImportedWorldDef
	Chunks          map[ChunkCoord]*content.ImportedWorldChunkDef
	Warnings        []ImportedWorldBakeWarning
	TotalVoxelCount int
	BoundsMin       [3]int
	BoundsMax       [3]int
}

type ImportedWorldBakeProgress struct {
	Phase     string
	Message   string
	Completed int
	Total     int
	Fraction  float32
}

type ImportedWorldBakeProgressFunc func(ImportedWorldBakeProgress)

type importedBakeVoxel struct {
	X     int
	Y     int
	Z     int
	Value uint8
}

type importedWorldBakeReport struct {
	WorldID         string                      `json:"world_id"`
	TotalVoxelCount int                         `json:"total_voxel_count"`
	BoundsMin       [3]int                      `json:"bounds_min"`
	BoundsMax       [3]int                      `json:"bounds_max"`
	Warnings        []ImportedWorldBakeWarning  `json:"warnings,omitempty"`
	Chunks          []importedWorldBakeChunkLog `json:"chunks,omitempty"`
}

type importedWorldBakeChunkLog struct {
	Coord              [3]int `json:"coord"`
	NonEmptyVoxelCount int    `json:"non_empty_voxel_count"`
}

func BakeImportedWorldFromVoxFile(path string, cfg ImportedWorldBakeConfig) (ImportedWorldBakeResult, error) {
	return BakeImportedWorldFromVoxFileWithProgress(path, cfg, nil)
}

func BakeImportedWorldFromVoxFileWithProgress(path string, cfg ImportedWorldBakeConfig, progress ImportedWorldBakeProgressFunc) (ImportedWorldBakeResult, error) {
	emitImportedWorldBakeProgress(progress, "load", "Loading source .vox", 0, 1, 0)
	voxFile, err := LoadVoxFile(path)
	if err != nil {
		return ImportedWorldBakeResult{}, err
	}
	emitImportedWorldBakeProgress(progress, "load", "Loaded source .vox", 1, 1, 0.05)
	if cfg.WorldID == "" {
		cfg.WorldID = trimImportedWorldID(filepath.Base(path))
	}
	if cfg.SourceBuildVersion == "" {
		cfg.SourceBuildVersion = DefaultImportedWorldBakeBuildVersion
	}
	if hash, err := importedWorldSourceHash(path); err == nil && hash != "" {
		cfg.SourceBuildVersion = strings.TrimSpace(cfg.SourceBuildVersion)
		result, bakeErr := BakeImportedWorldFromVoxWithProgress(voxFile, cfg, progress)
		if bakeErr != nil {
			return ImportedWorldBakeResult{}, bakeErr
		}
		if result.Manifest != nil {
			result.Manifest.SourceHash = hash
		}
		return result, nil
	}
	return BakeImportedWorldFromVoxWithProgress(voxFile, cfg, progress)
}

func BakeImportedWorldFromVox(voxFile *VoxFile, cfg ImportedWorldBakeConfig) (ImportedWorldBakeResult, error) {
	return BakeImportedWorldFromVoxWithProgress(voxFile, cfg, nil)
}

func BakeImportedWorldFromVoxWithProgress(voxFile *VoxFile, cfg ImportedWorldBakeConfig, progress ImportedWorldBakeProgressFunc) (ImportedWorldBakeResult, error) {
	cfg = normalizeImportedWorldBakeConfig(cfg)
	if voxFile == nil {
		return ImportedWorldBakeResult{}, fmt.Errorf("vox file is nil")
	}

	worldVoxels, warnings, err := flattenImportedWorldBakeVoxelsWithProgress(voxFile, cfg, progress)
	if err != nil {
		return ImportedWorldBakeResult{}, err
	}
	if len(worldVoxels) == 0 {
		return ImportedWorldBakeResult{}, fmt.Errorf("vox source contains no voxels")
	}

	if cfg.NormalizeToOrigin {
		emitImportedWorldBakeProgress(progress, "normalize", "Normalizing world origin", 0, 1, 0.46)
		shiftImportedBakeVoxelsToOrigin(worldVoxels)
	}
	emitImportedWorldBakeProgress(progress, "sort", "Sorting baked voxels", 0, 1, 0.5)
	sortImportedBakeVoxels(worldVoxels)

	chunks, boundsMin, boundsMax := buildImportedWorldChunksWithProgress(worldVoxels, cfg, progress)
	warnings = append(warnings, buildImportedWorldChunkWarningsWithProgress(chunks, cfg, progress)...)
	warnings = append(warnings, buildImportedWorldTopologyWarningsWithProgress(worldVoxels, cfg, progress)...)

	entries := make([]content.ImportedWorldChunkEntryDef, 0, len(chunks))
	chunkCoords := make([]ChunkCoord, 0, len(chunks))
	for coord := range chunks {
		chunkCoords = append(chunkCoords, coord)
	}
	sort.Slice(chunkCoords, func(i, j int) bool {
		if chunkCoords[i].X != chunkCoords[j].X {
			return chunkCoords[i].X < chunkCoords[j].X
		}
		if chunkCoords[i].Y != chunkCoords[j].Y {
			return chunkCoords[i].Y < chunkCoords[j].Y
		}
		return chunkCoords[i].Z < chunkCoords[j].Z
	})

	chunkDir := cfg.ChunkDirectoryName
	emitImportedWorldBakeProgress(progress, "manifest", "Building manifest entries", 0, len(chunkCoords), 0.98)
	for _, coord := range chunkCoords {
		chunk := chunks[coord]
		entries = append(entries, content.ImportedWorldChunkEntryDef{
			Coord: content.TerrainChunkCoordDef{X: coord.X, Y: coord.Y, Z: coord.Z},
			ChunkPath: filepath.ToSlash(filepath.Join(
				chunkDir,
				fmt.Sprintf("%s_%d_%d_%d.gkchunk", cfg.WorldID, coord.X, coord.Y, coord.Z),
			)),
			NonEmptyVoxelCount: chunk.NonEmptyVoxelCount,
		})
		emitImportedWorldBakeProgress(progress, "manifest", "Building manifest entries", len(entries), len(chunkCoords), 0.98+0.01*progressFraction(len(entries), len(chunkCoords)))
	}

	result := ImportedWorldBakeResult{
		Manifest: &content.ImportedWorldDef{
			WorldID:            cfg.WorldID,
			SchemaVersion:      content.CurrentImportedWorldSchemaVersion,
			Kind:               content.ImportedWorldKindVoxelWorld,
			ChunkSize:          cfg.ChunkSize,
			VoxelResolution:    cfg.VoxelResolution,
			Palette:            importedWorldPaletteFromVox(voxFile),
			SourceBuildVersion: cfg.SourceBuildVersion,
			Entries:            entries,
		},
		Chunks:          chunks,
		Warnings:        warnings,
		TotalVoxelCount: len(worldVoxels),
		BoundsMin:       boundsMin,
		BoundsMax:       boundsMax,
	}
	emitImportedWorldBakeProgress(progress, "complete", "Bake data prepared", 1, 1, 1)
	return result, nil
}

func SaveImportedWorldBake(manifestPath string, bake ImportedWorldBakeResult) error {
	return SaveImportedWorldBakeWithProgress(manifestPath, bake, nil)
}

func SaveImportedWorldBakeWithProgress(manifestPath string, bake ImportedWorldBakeResult, progress ImportedWorldBakeProgressFunc) error {
	if bake.Manifest == nil {
		return fmt.Errorf("bake manifest is nil")
	}
	if strings.TrimSpace(manifestPath) == "" {
		return fmt.Errorf("manifest path is empty")
	}
	manifestDir := filepath.Dir(manifestPath)
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		return err
	}

	entriesByCoord := make(map[ChunkCoord]content.ImportedWorldChunkEntryDef, len(bake.Manifest.Entries))
	for _, entry := range bake.Manifest.Entries {
		entriesByCoord[ChunkCoord{X: entry.Coord.X, Y: entry.Coord.Y, Z: entry.Coord.Z}] = entry
	}
	coords := make([]ChunkCoord, 0, len(bake.Chunks))
	for coord := range bake.Chunks {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		if coords[i].X != coords[j].X {
			return coords[i].X < coords[j].X
		}
		if coords[i].Y != coords[j].Y {
			return coords[i].Y < coords[j].Y
		}
		return coords[i].Z < coords[j].Z
	})
	for i, coord := range coords {
		chunk := bake.Chunks[coord]
		entry, ok := entriesByCoord[coord]
		if !ok {
			return fmt.Errorf("missing manifest entry for chunk %s", coord.String())
		}
		chunkPath := content.ResolveDocumentPath(entry.ChunkPath, manifestPath)
		emitImportedWorldBakeProgress(progress, "save_chunks", fmt.Sprintf("Saving chunk %d of %d", i+1, len(coords)), i, len(coords), progressFraction(i, len(coords)))
		if err := content.SaveImportedWorldChunk(chunkPath, chunk); err != nil {
			return err
		}
	}
	emitImportedWorldBakeProgress(progress, "save_manifest", "Saving world manifest", len(coords), len(coords)+1, progressFraction(len(coords), len(coords)+1))
	if err := content.SaveImportedWorld(manifestPath, bake.Manifest); err != nil {
		return err
	}
	emitImportedWorldBakeProgress(progress, "save_complete", "Saved manifest and chunks", 1, 1, 1)
	return nil
}

func BuildImportedWorldBakeReport(bake ImportedWorldBakeResult) importedWorldBakeReport {
	report := importedWorldBakeReport{
		Warnings:        append([]ImportedWorldBakeWarning(nil), bake.Warnings...),
		TotalVoxelCount: bake.TotalVoxelCount,
		BoundsMin:       bake.BoundsMin,
		BoundsMax:       bake.BoundsMax,
	}
	if bake.Manifest != nil {
		report.WorldID = bake.Manifest.WorldID
	}
	chunkCoords := make([]ChunkCoord, 0, len(bake.Chunks))
	for coord := range bake.Chunks {
		chunkCoords = append(chunkCoords, coord)
	}
	sort.Slice(chunkCoords, func(i, j int) bool {
		if chunkCoords[i].X != chunkCoords[j].X {
			return chunkCoords[i].X < chunkCoords[j].X
		}
		if chunkCoords[i].Y != chunkCoords[j].Y {
			return chunkCoords[i].Y < chunkCoords[j].Y
		}
		return chunkCoords[i].Z < chunkCoords[j].Z
	})
	for _, coord := range chunkCoords {
		chunk := bake.Chunks[coord]
		report.Chunks = append(report.Chunks, importedWorldBakeChunkLog{
			Coord:              [3]int{coord.X, coord.Y, coord.Z},
			NonEmptyVoxelCount: chunk.NonEmptyVoxelCount,
		})
	}
	return report
}

func SaveImportedWorldBakeReport(reportPath string, bake ImportedWorldBakeResult) error {
	if strings.TrimSpace(reportPath) == "" {
		return fmt.Errorf("report path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(BuildImportedWorldBakeReport(bake), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(reportPath, data, 0644)
}

func normalizeImportedWorldBakeConfig(cfg ImportedWorldBakeConfig) ImportedWorldBakeConfig {
	if strings.TrimSpace(cfg.WorldID) == "" {
		cfg.WorldID = "imported_world"
	}
	cfg.WorldID = trimImportedWorldID(cfg.WorldID)
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = DefaultImportedWorldBakeChunkSize
	}
	if cfg.VoxelResolution <= 0 {
		cfg.VoxelResolution = DefaultImportedWorldBakeVoxelResolution
	}
	if cfg.ScaleMultiplier <= 0 {
		cfg.ScaleMultiplier = 1
	}
	if strings.TrimSpace(cfg.SourceBuildVersion) == "" {
		cfg.SourceBuildVersion = DefaultImportedWorldBakeBuildVersion
	}
	if strings.TrimSpace(cfg.ChunkDirectoryName) == "" {
		cfg.ChunkDirectoryName = DefaultImportedWorldBakeChunkDirName
	}
	if cfg.ConnectedMassWarnSize <= 0 {
		cfg.ConnectedMassWarnSize = 65536
	}
	if cfg.ThinFeatureWarnCount <= 0 {
		cfg.ThinFeatureWarnCount = 2048
	}
	if cfg.ChunkHotspotWarnCount <= 0 {
		cfg.ChunkHotspotWarnCount = importedWorldBakeHotspotThresholdDefault
	}
	if cfg.VerticalSpanWarnSize <= 0 {
		cfg.VerticalSpanWarnSize = cfg.ChunkSize * 2
	}
	return cfg
}

func flattenImportedWorldBakeVoxels(voxFile *VoxFile) ([]importedBakeVoxel, []ImportedWorldBakeWarning, error) {
	return flattenImportedWorldBakeVoxelsWithProgress(voxFile, ImportedWorldBakeConfig{}, nil)
}

func flattenImportedWorldBakeVoxelsWithProgress(voxFile *VoxFile, cfg ImportedWorldBakeConfig, progress ImportedWorldBakeProgressFunc) ([]importedBakeVoxel, []ImportedWorldBakeWarning, error) {
	voxels := make(map[[3]int]uint8)
	warnings := []ImportedWorldBakeWarning{}
	scaleMultiplier := cfg.ScaleMultiplier
	if scaleMultiplier <= 0 {
		scaleMultiplier = 1
	}
	scaledFile := scaleImportedWorldBakeVoxFile(voxFile, scaleMultiplier)

	if len(scaledFile.Nodes) == 0 {
		total := 0
		for _, model := range scaledFile.Models {
			total += len(model.Voxels)
		}
		processed := 0
		for _, model := range scaledFile.Models {
			for _, voxel := range model.Voxels {
				key := [3]int{int(voxel.X), int(voxel.Y), int(voxel.Z)}
				voxels[key] = voxel.ColorIndex
				processed++
				maybeEmitImportedWorldBakeProgress(progress, "flatten", "Flattening source voxels", processed, total, 0.05, 0.45)
			}
		}
		if len(scaledFile.Models) > 1 {
			warnings = append(warnings, ImportedWorldBakeWarning{
				Code:    "multi_model_overlay",
				Message: fmt.Sprintf("VOX source contains %d root models without scene nodes; models were overlaid at the origin", len(scaledFile.Models)),
			})
		}
		return importedBakeVoxelsFromMap(voxels), warnings, nil
	}

	inspection := ExtractVoxHierarchy(scaledFile, 1.0)
	total := 0
	for _, instance := range inspection {
		if instance.ModelIndex < 0 || instance.ModelIndex >= len(scaledFile.Models) {
			continue
		}
		total += len(scaledFile.Models[instance.ModelIndex].Voxels)
	}
	processed := 0
	for _, instance := range inspection {
		if instance.ModelIndex < 0 || instance.ModelIndex >= len(scaledFile.Models) {
			continue
		}
		model := scaledFile.Models[instance.ModelIndex]
		if scaleMultiplier != 1 {
			instance.Transform.Position = instance.Transform.Position.Mul(scaleMultiplier)
		}
		mx := instance.Transform.ObjectToWorld()
		for _, voxel := range model.Voxels {
			pos := transformImportedBakeVoxel(mx, int(voxel.X), int(voxel.Y), int(voxel.Z))
			voxels[[3]int{pos[0], pos[1], pos[2]}] = voxel.ColorIndex
			processed++
			maybeEmitImportedWorldBakeProgress(progress, "flatten", "Flattening source voxels", processed, total, 0.05, 0.45)
		}
	}
	return importedBakeVoxelsFromMap(voxels), warnings, nil
}

func transformImportedBakeVoxel(mx mgl32.Mat4, x, y, z int) [3]int {
	// Sample at voxel centers, then map back to authored cell indices.
	// Using voxel corners here creates half-voxel rounding drift for scene-pivoted VOX models,
	// which can turn touching scene pieces into gaps after baking.
	world := mx.Mul4x1(mgl32.Vec4{
		(float32(x) + 0.5) * VoxelSize,
		(float32(y) + 0.5) * VoxelSize,
		(float32(z) + 0.5) * VoxelSize,
		1,
	})
	return [3]int{
		importedWorldBakeCell(world.X() / VoxelSize),
		importedWorldBakeCell(world.Y() / VoxelSize),
		importedWorldBakeCell(world.Z() / VoxelSize),
	}
}

func importedWorldBakeCell(v float32) int {
	// Scene-baked voxel centers should land on the authored half-cell lattice
	// (integers or x.5) after rotation. Snap back to that lattice before
	// converting to a corner-based cell index so small floating-point drift does
	// not push negative coordinates into the previous cell.
	scaled := math.Round(float64(v)*2) / 2
	return int(math.Floor(scaled))
}

func importedWorldPaletteFromVox(voxFile *VoxFile) []content.ImportedWorldPaletteColor {
	if voxFile == nil {
		return nil
	}
	palette := make([]content.ImportedWorldPaletteColor, len(voxFile.Palette))
	for i, color := range voxFile.Palette {
		palette[i] = content.ImportedWorldPaletteColor{color[0], color[1], color[2], color[3]}
	}
	return palette
}

func scaleImportedWorldBakeVoxFile(voxFile *VoxFile, scale float32) *VoxFile {
	if voxFile == nil || scale <= 0 || scale == 1 {
		return voxFile
	}
	scaled := *voxFile
	if len(voxFile.Models) > 0 {
		scaled.Models = make([]VoxModel, len(voxFile.Models))
		for i, model := range voxFile.Models {
			scaled.Models[i] = ScaleVoxModel(model, scale)
		}
	}
	return &scaled
}

func importedBakeVoxelsFromMap(src map[[3]int]uint8) []importedBakeVoxel {
	out := make([]importedBakeVoxel, 0, len(src))
	for key, value := range src {
		out = append(out, importedBakeVoxel{
			X:     key[0],
			Y:     key[1],
			Z:     key[2],
			Value: value,
		})
	}
	sortImportedBakeVoxels(out)
	return out
}

func shiftImportedBakeVoxelsToOrigin(voxels []importedBakeVoxel) {
	if len(voxels) == 0 {
		return
	}
	minX, minY, minZ := voxels[0].X, voxels[0].Y, voxels[0].Z
	for _, voxel := range voxels[1:] {
		if voxel.X < minX {
			minX = voxel.X
		}
		if voxel.Y < minY {
			minY = voxel.Y
		}
		if voxel.Z < minZ {
			minZ = voxel.Z
		}
	}
	for i := range voxels {
		voxels[i].X -= minX
		voxels[i].Y -= minY
		voxels[i].Z -= minZ
	}
}

func sortImportedBakeVoxels(voxels []importedBakeVoxel) {
	sort.Slice(voxels, func(i, j int) bool {
		if voxels[i].X != voxels[j].X {
			return voxels[i].X < voxels[j].X
		}
		if voxels[i].Y != voxels[j].Y {
			return voxels[i].Y < voxels[j].Y
		}
		if voxels[i].Z != voxels[j].Z {
			return voxels[i].Z < voxels[j].Z
		}
		return voxels[i].Value < voxels[j].Value
	})
}

func buildImportedWorldChunks(voxels []importedBakeVoxel, cfg ImportedWorldBakeConfig) (map[ChunkCoord]*content.ImportedWorldChunkDef, [3]int, [3]int) {
	return buildImportedWorldChunksWithProgress(voxels, cfg, nil)
}

func buildImportedWorldChunksWithProgress(voxels []importedBakeVoxel, cfg ImportedWorldBakeConfig, progress ImportedWorldBakeProgressFunc) (map[ChunkCoord]*content.ImportedWorldChunkDef, [3]int, [3]int) {
	chunks := make(map[ChunkCoord]*content.ImportedWorldChunkDef)
	boundsMin := [3]int{0, 0, 0}
	boundsMax := [3]int{0, 0, 0}
	first := true
	for i, voxel := range voxels {
		if first {
			boundsMin = [3]int{voxel.X, voxel.Y, voxel.Z}
			boundsMax = [3]int{voxel.X, voxel.Y, voxel.Z}
			first = false
		} else {
			boundsMin[0] = min(boundsMin[0], voxel.X)
			boundsMin[1] = min(boundsMin[1], voxel.Y)
			boundsMin[2] = min(boundsMin[2], voxel.Z)
			boundsMax[0] = max(boundsMax[0], voxel.X)
			boundsMax[1] = max(boundsMax[1], voxel.Y)
			boundsMax[2] = max(boundsMax[2], voxel.Z)
		}

		coord := ChunkCoord{
			X: floorDiv(voxel.X, cfg.ChunkSize),
			Y: floorDiv(voxel.Y, cfg.ChunkSize),
			Z: floorDiv(voxel.Z, cfg.ChunkSize),
		}
		chunk := chunks[coord]
		if chunk == nil {
			chunk = &content.ImportedWorldChunkDef{
				WorldID:         cfg.WorldID,
				SchemaVersion:   content.CurrentImportedWorldChunkSchemaVersion,
				Coord:           content.TerrainChunkCoordDef{X: coord.X, Y: coord.Y, Z: coord.Z},
				ChunkSize:       cfg.ChunkSize,
				VoxelResolution: cfg.VoxelResolution,
			}
			chunks[coord] = chunk
		}
		chunk.Voxels = append(chunk.Voxels, content.ImportedWorldVoxelDef{
			X:     voxel.X - coord.X*cfg.ChunkSize,
			Y:     voxel.Y - coord.Y*cfg.ChunkSize,
			Z:     voxel.Z - coord.Z*cfg.ChunkSize,
			Value: voxel.Value,
		})
		maybeEmitImportedWorldBakeProgress(progress, "chunk", "Partitioning voxels into chunks", i+1, len(voxels), 0.5, 0.78)
	}

	for _, chunk := range chunks {
		sort.Slice(chunk.Voxels, func(i, j int) bool {
			if chunk.Voxels[i].X != chunk.Voxels[j].X {
				return chunk.Voxels[i].X < chunk.Voxels[j].X
			}
			if chunk.Voxels[i].Y != chunk.Voxels[j].Y {
				return chunk.Voxels[i].Y < chunk.Voxels[j].Y
			}
			if chunk.Voxels[i].Z != chunk.Voxels[j].Z {
				return chunk.Voxels[i].Z < chunk.Voxels[j].Z
			}
			return chunk.Voxels[i].Value < chunk.Voxels[j].Value
		})
		chunk.NonEmptyVoxelCount = len(chunk.Voxels)
	}
	return chunks, boundsMin, boundsMax
}

func buildImportedWorldChunkWarnings(chunks map[ChunkCoord]*content.ImportedWorldChunkDef, cfg ImportedWorldBakeConfig) []ImportedWorldBakeWarning {
	return buildImportedWorldChunkWarningsWithProgress(chunks, cfg, nil)
}

func buildImportedWorldChunkWarningsWithProgress(chunks map[ChunkCoord]*content.ImportedWorldChunkDef, cfg ImportedWorldBakeConfig, progress ImportedWorldBakeProgressFunc) []ImportedWorldBakeWarning {
	warnings := []ImportedWorldBakeWarning{}
	coords := make([]ChunkCoord, 0, len(chunks))
	for coord := range chunks {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		if coords[i].X != coords[j].X {
			return coords[i].X < coords[j].X
		}
		if coords[i].Y != coords[j].Y {
			return coords[i].Y < coords[j].Y
		}
		return coords[i].Z < coords[j].Z
	})
	for i, coord := range coords {
		chunk := chunks[coord]
		if chunk == nil {
			continue
		}
		if chunk.NonEmptyVoxelCount >= cfg.ChunkHotspotWarnCount {
			warnings = append(warnings, ImportedWorldBakeWarning{
				Code:    "dense_chunk_hotspot",
				Message: fmt.Sprintf("chunk %s contains %d voxels; consider splitting or simplifying dense source geometry", coord.String(), chunk.NonEmptyVoxelCount),
			})
		}
		minY, maxY := 0, 0
		first := true
		occupancy := make(map[[3]int]struct{}, len(chunk.Voxels))
		for _, voxel := range chunk.Voxels {
			occupancy[[3]int{voxel.X, voxel.Y, voxel.Z}] = struct{}{}
			if first {
				minY, maxY = voxel.Y, voxel.Y
				first = false
			} else {
				minY = min(minY, voxel.Y)
				maxY = max(maxY, voxel.Y)
			}
		}
		if !first && (maxY-minY+1) >= cfg.VerticalSpanWarnSize {
			warnings = append(warnings, ImportedWorldBakeWarning{
				Code:    "chunk_vertical_span",
				Message: fmt.Sprintf("chunk %s spans %d voxels vertically; consider reducing vertical density or reauthoring chunking assumptions", coord.String(), maxY-minY+1),
			})
		}
		enclosed := countEnclosedVoidCells(occupancy, chunk.ChunkSize)
		if enclosed > 0 {
			warnings = append(warnings, ImportedWorldBakeWarning{
				Code:    "enclosed_void_cells",
				Message: fmt.Sprintf("chunk %s contains %d enclosed empty cells; these spaces may be inaccessible for traversal or nav baking", coord.String(), enclosed),
			})
		}
		maybeEmitImportedWorldBakeProgress(progress, "warnings", "Analyzing chunk warnings", i+1, len(coords), 0.78, 0.88)
	}
	return warnings
}

func buildImportedWorldTopologyWarnings(voxels []importedBakeVoxel, cfg ImportedWorldBakeConfig) []ImportedWorldBakeWarning {
	return buildImportedWorldTopologyWarningsWithProgress(voxels, cfg, nil)
}

func buildImportedWorldTopologyWarningsWithProgress(voxels []importedBakeVoxel, cfg ImportedWorldBakeConfig, progress ImportedWorldBakeProgressFunc) []ImportedWorldBakeWarning {
	if len(voxels) == 0 {
		return nil
	}
	occupancy := make(map[[3]int]struct{}, len(voxels))
	for _, voxel := range voxels {
		occupancy[[3]int{voxel.X, voxel.Y, voxel.Z}] = struct{}{}
	}

	visited := make(map[[3]int]struct{}, len(voxels))
	largestComponent := 0
	componentVisits := 0
	for _, voxel := range voxels {
		key := [3]int{voxel.X, voxel.Y, voxel.Z}
		if _, ok := visited[key]; !ok {
			size := importedWorldComponentSizeWithProgress(key, occupancy, visited, func() {
				componentVisits++
				maybeEmitImportedWorldBakeProgress(progress, "topology", "Analyzing connected voxel masses", componentVisits, len(voxels), 0.88, 0.94)
			})
			if size > largestComponent {
				largestComponent = size
			}
		}
	}

	thinFeatureCount := 0
	for i, voxel := range voxels {
		key := [3]int{voxel.X, voxel.Y, voxel.Z}
		if importedWorldVoxelLooksThin(key, occupancy) {
			thinFeatureCount++
		}
		maybeEmitImportedWorldBakeProgress(progress, "topology", "Scanning thin traversal features", i+1, len(voxels), 0.94, 0.98)
	}

	warnings := []ImportedWorldBakeWarning{}
	if largestComponent >= cfg.ConnectedMassWarnSize {
		warnings = append(warnings, ImportedWorldBakeWarning{
			Code:    "large_connected_mass",
			Message: fmt.Sprintf("largest connected solid mass contains %d voxels; consider separating structural shells and props before runtime destruction", largestComponent),
		})
	}
	if thinFeatureCount >= cfg.ThinFeatureWarnCount {
		warnings = append(warnings, ImportedWorldBakeWarning{
			Code:    "thin_feature_count",
			Message: fmt.Sprintf("detected %d paper-thin voxels; expect fragile floors or walls for traversal and nav bake", thinFeatureCount),
		})
	}
	return warnings
}

func importedWorldComponentSize(start [3]int, occupancy map[[3]int]struct{}, visited map[[3]int]struct{}) int {
	return importedWorldComponentSizeWithProgress(start, occupancy, visited, nil)
}

func importedWorldComponentSizeWithProgress(start [3]int, occupancy map[[3]int]struct{}, visited map[[3]int]struct{}, onVisit func()) int {
	queue := [][3]int{start}
	visited[start] = struct{}{}
	size := 0
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		size++
		if onVisit != nil {
			onVisit()
		}
		for _, offset := range importedWorldNeighborOffsets {
			next := [3]int{curr[0] + offset[0], curr[1] + offset[1], curr[2] + offset[2]}
			if _, ok := occupancy[next]; !ok {
				continue
			}
			if _, ok := visited[next]; ok {
				continue
			}
			visited[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	return size
}

func emitImportedWorldBakeProgress(progress ImportedWorldBakeProgressFunc, phase string, message string, completed int, total int, fraction float32) {
	if progress == nil {
		return
	}
	progress(ImportedWorldBakeProgress{
		Phase:     phase,
		Message:   message,
		Completed: completed,
		Total:     total,
		Fraction:  clampImportedWorldBakeFraction(fraction),
	})
}

func maybeEmitImportedWorldBakeProgress(progress ImportedWorldBakeProgressFunc, phase string, message string, completed int, total int, start float32, end float32) {
	if progress == nil || total <= 0 {
		return
	}
	if completed != total && completed != 1 && completed%8192 != 0 {
		return
	}
	fraction := start + (end-start)*progressFraction(completed, total)
	emitImportedWorldBakeProgress(progress, phase, message, completed, total, fraction)
}

func progressFraction(completed int, total int) float32 {
	if total <= 0 {
		return 1
	}
	value := float32(completed) / float32(total)
	return clampImportedWorldBakeFraction(value)
}

func clampImportedWorldBakeFraction(value float32) float32 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func importedWorldVoxelLooksThin(key [3]int, occupancy map[[3]int]struct{}) bool {
	axes := [][2][3]int{
		{{1, 0, 0}, {-1, 0, 0}},
		{{0, 1, 0}, {0, -1, 0}},
		{{0, 0, 1}, {0, 0, -1}},
	}
	for _, axis := range axes {
		front := [3]int{key[0] + axis[0][0], key[1] + axis[0][1], key[2] + axis[0][2]}
		back := [3]int{key[0] + axis[1][0], key[1] + axis[1][1], key[2] + axis[1][2]}
		_, frontSolid := occupancy[front]
		_, backSolid := occupancy[back]
		if !frontSolid && !backSolid {
			return true
		}
	}
	return false
}

var importedWorldNeighborOffsets = [][3]int{
	{1, 0, 0},
	{-1, 0, 0},
	{0, 1, 0},
	{0, -1, 0},
	{0, 0, 1},
	{0, 0, -1},
}

func countEnclosedVoidCells(occupancy map[[3]int]struct{}, chunkSize int) int {
	if chunkSize <= 0 || len(occupancy) == 0 {
		return 0
	}
	minX, minY, minZ := chunkSize-1, chunkSize-1, chunkSize-1
	maxX, maxY, maxZ := 0, 0, 0
	for key := range occupancy {
		minX = min(minX, key[0])
		minY = min(minY, key[1])
		minZ = min(minZ, key[2])
		maxX = max(maxX, key[0])
		maxY = max(maxY, key[1])
		maxZ = max(maxZ, key[2])
	}

	searchMinX := max(minX-1, 0)
	searchMinY := max(minY-1, 0)
	searchMinZ := max(minZ-1, 0)
	searchMaxX := min(maxX+1, chunkSize-1)
	searchMaxY := min(maxY+1, chunkSize-1)
	searchMaxZ := min(maxZ+1, chunkSize-1)

	sizeX := searchMaxX - searchMinX + 1
	sizeY := searchMaxY - searchMinY + 1
	sizeZ := searchMaxZ - searchMinZ + 1
	if sizeX <= 0 || sizeY <= 0 || sizeZ <= 0 {
		return 0
	}

	visited := make([]bool, sizeX*sizeY*sizeZ)
	indexOf := func(x, y, z int) int {
		return ((x-searchMinX)*sizeY+(y-searchMinY))*sizeZ + (z - searchMinZ)
	}

	queue := make([][3]int, 0, sizeX*sizeY+sizeX*sizeZ+sizeY*sizeZ)
	enqueueIfBoundaryEmpty := func(x, y, z int) {
		key := [3]int{x, y, z}
		if _, solid := occupancy[key]; solid {
			return
		}
		idx := indexOf(x, y, z)
		if visited[idx] {
			return
		}
		visited[idx] = true
		queue = append(queue, key)
	}

	for x := searchMinX; x <= searchMaxX; x++ {
		for y := searchMinY; y <= searchMaxY; y++ {
			for z := searchMinZ; z <= searchMaxZ; z++ {
				if x != searchMinX && y != searchMinY && z != searchMinZ && x != searchMaxX && y != searchMaxY && z != searchMaxZ {
					continue
				}
				enqueueIfBoundaryEmpty(x, y, z)
			}
		}
	}

	for head := 0; head < len(queue); head++ {
		curr := queue[head]
		for _, offset := range importedWorldNeighborOffsets {
			next := [3]int{curr[0] + offset[0], curr[1] + offset[1], curr[2] + offset[2]}
			if next[0] < searchMinX || next[1] < searchMinY || next[2] < searchMinZ || next[0] > searchMaxX || next[1] > searchMaxY || next[2] > searchMaxZ {
				continue
			}
			if _, solid := occupancy[next]; solid {
				continue
			}
			idx := indexOf(next[0], next[1], next[2])
			if visited[idx] {
				continue
			}
			visited[idx] = true
			queue = append(queue, next)
		}
	}

	enclosed := 0
	for x := searchMinX; x <= searchMaxX; x++ {
		for y := searchMinY; y <= searchMaxY; y++ {
			for z := searchMinZ; z <= searchMaxZ; z++ {
				key := [3]int{x, y, z}
				if _, solid := occupancy[key]; solid {
					continue
				}
				if visited[indexOf(x, y, z)] {
					continue
				}
				enclosed++
			}
		}
	}
	return enclosed
}

func floorDiv(v, divisor int) int {
	if divisor == 0 {
		return 0
	}
	if v >= 0 {
		return v / divisor
	}
	return -(((-v) + divisor - 1) / divisor)
}

func trimImportedWorldID(input string) string {
	base := strings.TrimSpace(strings.TrimSuffix(input, filepath.Ext(input)))
	if base == "" {
		return "imported_world"
	}
	base = strings.ReplaceAll(base, " ", "_")
	return base
}

func importedWorldSourceHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:]), nil
}
