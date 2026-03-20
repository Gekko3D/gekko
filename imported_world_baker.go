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
	voxFile, err := LoadVoxFile(path)
	if err != nil {
		return ImportedWorldBakeResult{}, err
	}
	if cfg.WorldID == "" {
		cfg.WorldID = trimImportedWorldID(filepath.Base(path))
	}
	if cfg.SourceBuildVersion == "" {
		cfg.SourceBuildVersion = DefaultImportedWorldBakeBuildVersion
	}
	if hash, err := importedWorldSourceHash(path); err == nil && hash != "" {
		cfg.SourceBuildVersion = strings.TrimSpace(cfg.SourceBuildVersion)
		result, bakeErr := BakeImportedWorldFromVox(voxFile, cfg)
		if bakeErr != nil {
			return ImportedWorldBakeResult{}, bakeErr
		}
		if result.Manifest != nil {
			result.Manifest.SourceHash = hash
		}
		return result, nil
	}
	return BakeImportedWorldFromVox(voxFile, cfg)
}

func BakeImportedWorldFromVox(voxFile *VoxFile, cfg ImportedWorldBakeConfig) (ImportedWorldBakeResult, error) {
	cfg = normalizeImportedWorldBakeConfig(cfg)
	if voxFile == nil {
		return ImportedWorldBakeResult{}, fmt.Errorf("vox file is nil")
	}

	worldVoxels, warnings, err := flattenImportedWorldBakeVoxels(voxFile)
	if err != nil {
		return ImportedWorldBakeResult{}, err
	}
	if len(worldVoxels) == 0 {
		return ImportedWorldBakeResult{}, fmt.Errorf("vox source contains no voxels")
	}

	if cfg.NormalizeToOrigin {
		shiftImportedBakeVoxelsToOrigin(worldVoxels)
	}
	sortImportedBakeVoxels(worldVoxels)

	chunks, boundsMin, boundsMax := buildImportedWorldChunks(worldVoxels, cfg)
	warnings = append(warnings, buildImportedWorldChunkWarnings(chunks, cfg)...)
	warnings = append(warnings, buildImportedWorldTopologyWarnings(worldVoxels, cfg)...)

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
	return result, nil
}

func SaveImportedWorldBake(manifestPath string, bake ImportedWorldBakeResult) error {
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
	for coord, chunk := range bake.Chunks {
		entry, ok := entriesByCoord[coord]
		if !ok {
			return fmt.Errorf("missing manifest entry for chunk %s", coord.String())
		}
		chunkPath := content.ResolveDocumentPath(entry.ChunkPath, manifestPath)
		if err := content.SaveImportedWorldChunk(chunkPath, chunk); err != nil {
			return err
		}
	}
	return content.SaveImportedWorld(manifestPath, bake.Manifest)
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
	voxels := make(map[[3]int]uint8)
	warnings := []ImportedWorldBakeWarning{}

	if len(voxFile.Nodes) == 0 {
		for _, model := range voxFile.Models {
			for _, voxel := range model.Voxels {
				key := [3]int{int(voxel.X), int(voxel.Y), int(voxel.Z)}
				voxels[key] = voxel.ColorIndex
			}
		}
		if len(voxFile.Models) > 1 {
			warnings = append(warnings, ImportedWorldBakeWarning{
				Code:    "multi_model_overlay",
				Message: fmt.Sprintf("VOX source contains %d root models without scene nodes; models were overlaid at the origin", len(voxFile.Models)),
			})
		}
		return importedBakeVoxelsFromMap(voxels), warnings, nil
	}

	inspection := ExtractVoxHierarchy(voxFile, 1.0)
	for _, instance := range inspection {
		if instance.ModelIndex < 0 || instance.ModelIndex >= len(voxFile.Models) {
			continue
		}
		model := voxFile.Models[instance.ModelIndex]
		mx := instance.Transform.ObjectToWorld()
		for _, voxel := range model.Voxels {
			pos := transformImportedBakeVoxel(mx, int(voxel.X), int(voxel.Y), int(voxel.Z))
			voxels[[3]int{pos[0], pos[1], pos[2]}] = voxel.ColorIndex
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
	scaled := float64(v)
	const eps = 1e-4
	if scaled >= 0 {
		return int(math.Floor(scaled + eps))
	}
	return int(math.Floor(scaled - eps))
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
	chunks := make(map[ChunkCoord]*content.ImportedWorldChunkDef)
	boundsMin := [3]int{0, 0, 0}
	boundsMax := [3]int{0, 0, 0}
	first := true
	for _, voxel := range voxels {
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
	for _, coord := range coords {
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
	}
	return warnings
}

func buildImportedWorldTopologyWarnings(voxels []importedBakeVoxel, cfg ImportedWorldBakeConfig) []ImportedWorldBakeWarning {
	if len(voxels) == 0 {
		return nil
	}
	occupancy := make(map[[3]int]struct{}, len(voxels))
	for _, voxel := range voxels {
		occupancy[[3]int{voxel.X, voxel.Y, voxel.Z}] = struct{}{}
	}

	visited := make(map[[3]int]struct{}, len(voxels))
	largestComponent := 0
	thinFeatureCount := 0
	for _, voxel := range voxels {
		key := [3]int{voxel.X, voxel.Y, voxel.Z}
		if _, ok := visited[key]; !ok {
			size := importedWorldComponentSize(key, occupancy, visited)
			if size > largestComponent {
				largestComponent = size
			}
		}
		if importedWorldVoxelLooksThin(key, occupancy) {
			thinFeatureCount++
		}
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
	queue := [][3]int{start}
	visited[start] = struct{}{}
	size := 0
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		size++
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
	if chunkSize <= 0 {
		return 0
	}
	visited := make(map[[3]int]struct{}, chunkSize*chunkSize)
	queue := make([][3]int, 0, chunkSize*chunkSize)
	for x := 0; x < chunkSize; x++ {
		for y := 0; y < chunkSize; y++ {
			for z := 0; z < chunkSize; z++ {
				if x != 0 && y != 0 && z != 0 && x != chunkSize-1 && y != chunkSize-1 && z != chunkSize-1 {
					continue
				}
				key := [3]int{x, y, z}
				if _, solid := occupancy[key]; solid {
					continue
				}
				if _, ok := visited[key]; ok {
					continue
				}
				visited[key] = struct{}{}
				queue = append(queue, key)
			}
		}
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, offset := range importedWorldNeighborOffsets {
			next := [3]int{curr[0] + offset[0], curr[1] + offset[1], curr[2] + offset[2]}
			if next[0] < 0 || next[1] < 0 || next[2] < 0 || next[0] >= chunkSize || next[1] >= chunkSize || next[2] >= chunkSize {
				continue
			}
			if _, solid := occupancy[next]; solid {
				continue
			}
			if _, ok := visited[next]; ok {
				continue
			}
			visited[next] = struct{}{}
			queue = append(queue, next)
		}
	}

	enclosed := 0
	for x := 0; x < chunkSize; x++ {
		for y := 0; y < chunkSize; y++ {
			for z := 0; z < chunkSize; z++ {
				key := [3]int{x, y, z}
				if _, solid := occupancy[key]; solid {
					continue
				}
				if _, reachable := visited[key]; reachable {
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
