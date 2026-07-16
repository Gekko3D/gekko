package content

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
)

const CurrentNavGraphBuilderVersion = "voxel_graph_v5"

type NavGraphBakeDiagnosticCount struct {
	Coord          TerrainChunkCoordDef `json:"coord"`
	AgentProfileID string               `json:"agent_profile_id,omitempty"`
	Stage          string               `json:"stage"`
	Code           string               `json:"code"`
	Count          int                  `json:"count"`
}

type NavGraphBakeResult struct {
	Manifest    NavGraphManifestDef           `json:"manifest"`
	SourceTiles []NavSourceTileDef            `json:"source_tiles,omitempty"`
	GraphTiles  []NavGraphTileDef             `json:"graph_tiles,omitempty"`
	Diagnostics []NavGraphBakeDiagnosticCount `json:"diagnostics,omitempty"`
}

// BakeLevelNavGraph loads level-owned traversal metadata and the level's base
// voxel world, then runs one deterministic graph bake.
func BakeLevelNavGraph(levelPath string, profiles []NavAgentProfileDef) (NavGraphBakeResult, error) {
	level, err := LoadLevel(levelPath)
	if err != nil {
		return NavGraphBakeResult{}, err
	}
	if validation := ValidateLevel(level, LevelValidationOptions{DocumentPath: levelPath}); validation.HasErrors() {
		return NavGraphBakeResult{}, fmt.Errorf("invalid level: %s", validation.Error())
	}
	if level.BaseWorld == nil || level.BaseWorld.ManifestPath == "" {
		return NavGraphBakeResult{}, fmt.Errorf("navigation graph level requires a base world manifest")
	}
	result, err := BakeImportedWorldNavGraph(ResolveDocumentPath(level.BaseWorld.ManifestPath, levelPath), profiles)
	if err != nil {
		return NavGraphBakeResult{}, err
	}
	if level.ChunkSize > 0 && level.ChunkSize != result.Manifest.ChunkSize || level.VoxelResolution > 0 && level.VoxelResolution != result.Manifest.VoxelResolution {
		return NavGraphBakeResult{}, fmt.Errorf("level and base world chunk metrics do not match")
	}
	if err := ApplyNavGraphLadders(&result, level.LadderVolumes); err != nil {
		return NavGraphBakeResult{}, err
	}
	return result, nil
}

// ApplyNavGraphLadders stores generic authored ladder facts in the manifest and
// links each capable profile. Delta rebuilds reuse the same linker.
func ApplyNavGraphLadders(bake *NavGraphBakeResult, ladders []LevelLadderVolumeDef) error {
	if bake == nil {
		return fmt.Errorf("navigation graph bake is required")
	}
	ladders = append([]LevelLadderVolumeDef(nil), ladders...)
	sort.Slice(ladders, func(i, j int) bool { return ladders[i].ID < ladders[j].ID })
	bake.Manifest.LadderVolumes = ladders
	for _, profile := range bake.Manifest.AgentProfiles {
		var graphs []NavGraphTileDef
		for _, graph := range bake.GraphTiles {
			if graph.AgentProfileID == profile.ID {
				graphs = append(graphs, graph)
			}
		}
		linked, diagnostics, err := ConnectNavGraphLadders(bake.SourceTiles, graphs, ladders, profile, bake.Manifest.ChunkSize, bake.Manifest.VoxelResolution)
		if err != nil {
			return err
		}
		byCoord := make(map[TerrainChunkCoordDef]NavGraphTileDef, len(linked))
		for _, graph := range linked {
			byCoord[graph.Coord] = graph
		}
		for i := range bake.GraphTiles {
			if bake.GraphTiles[i].AgentProfileID == profile.ID {
				bake.GraphTiles[i] = byCoord[bake.GraphTiles[i].Coord]
			}
		}
		for _, diagnostic := range diagnostics {
			bake.Diagnostics = append(bake.Diagnostics, NavGraphBakeDiagnosticCount{AgentProfileID: profile.ID, Stage: "ladder", Code: diagnostic.Code, Count: 1})
		}
	}
	if validation := ValidateNavGraphBake(bake); validation.HasErrors() {
		return fmt.Errorf("invalid ladder-linked navigation graph bake: %s", validation.Error())
	}
	return nil
}

// BakeImportedWorldNavGraph loads one complete imported voxel world and runs
// the same pure-Go span and graph builders used by focused generation tests.
func BakeImportedWorldNavGraph(worldPath string, profiles []NavAgentProfileDef) (NavGraphBakeResult, error) {
	world, err := LoadImportedWorld(worldPath)
	if err != nil {
		return NavGraphBakeResult{}, err
	}
	if validation := ValidateImportedWorld(world, ImportedWorldValidationOptions{DocumentPath: worldPath}); validation.HasErrors() {
		return NavGraphBakeResult{}, fmt.Errorf("invalid imported world: %s", validation.Error())
	}
	chunks := make([]ImportedWorldChunkDef, 0, len(world.Entries))
	for _, entry := range world.Entries {
		chunk, err := LoadImportedWorldChunk(ResolveImportedWorldChunkPath(entry, worldPath))
		if err != nil {
			return NavGraphBakeResult{}, fmt.Errorf("load imported world chunk %s: %w", TerrainChunkKey(entry.Coord), err)
		}
		chunks = append(chunks, *chunk)
	}
	return BakeNavGraphWorld(world, chunks, profiles)
}

// BakeNavGraphWorld builds deterministic source and profile graph tiles from
// effective non-zero voxel occupancy. Missing halo chunks remain unknown.
func BakeNavGraphWorld(world *ImportedWorldDef, chunks []ImportedWorldChunkDef, profiles []NavAgentProfileDef) (NavGraphBakeResult, error) {
	if validation := ValidateImportedWorld(world, ImportedWorldValidationOptions{}); validation.HasErrors() {
		return NavGraphBakeResult{}, fmt.Errorf("invalid imported world: %s", validation.Error())
	}
	profiles = append([]NavAgentProfileDef(nil), profiles...)
	for i := range profiles {
		profiles[i].Capabilities = append([]string(nil), profiles[i].Capabilities...)
		sort.Strings(profiles[i].Capabilities)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	if len(profiles) == 0 {
		return NavGraphBakeResult{}, fmt.Errorf("navigation graph bake requires an agent profile")
	}
	manifest := NavGraphManifestDef{
		NavID: world.WorldID, SchemaVersion: CurrentNavGraphManifestSchemaVersion,
		SourceWorldID: world.WorldID, BuilderVersion: CurrentNavGraphBuilderVersion,
		ChunkSize: world.ChunkSize, VoxelResolution: world.VoxelResolution, AgentProfiles: profiles,
	}
	if validation := ValidateNavGraphManifest(&manifest); validation.HasErrors() {
		return NavGraphBakeResult{}, fmt.Errorf("invalid navigation graph bake options: %s", validation.Error())
	}

	entries := make(map[TerrainChunkCoordDef]ImportedWorldChunkEntryDef, len(world.Entries))
	for _, entry := range world.Entries {
		entries[entry.Coord] = entry
	}
	buildChunks := make(map[TerrainChunkCoordDef]NavSpanBuildChunk, len(chunks))
	for _, chunk := range chunks {
		if _, exists := buildChunks[chunk.Coord]; exists {
			return NavGraphBakeResult{}, fmt.Errorf("duplicate imported world chunk %s", TerrainChunkKey(chunk.Coord))
		}
		if _, exists := entries[chunk.Coord]; !exists {
			return NavGraphBakeResult{}, fmt.Errorf("imported world chunk %s has no manifest entry", TerrainChunkKey(chunk.Coord))
		}
		if chunk.WorldID != "" && chunk.WorldID != world.WorldID {
			return NavGraphBakeResult{}, fmt.Errorf("imported world chunk %s belongs to world %q", TerrainChunkKey(chunk.Coord), chunk.WorldID)
		}
		if chunk.ChunkSize != world.ChunkSize || chunk.VoxelResolution != world.VoxelResolution {
			return NavGraphBakeResult{}, fmt.Errorf("imported world chunk %s metadata does not match manifest", TerrainChunkKey(chunk.Coord))
		}
		solids, err := navGraphSolidVoxels(chunk, world.ChunkSize)
		if err != nil {
			return NavGraphBakeResult{}, err
		}
		buildChunks[chunk.Coord] = NavSpanBuildChunk{Coord: chunk.Coord, Known: true, SourceHash: navGraphOccupancyHash(chunk.Coord, solids), SolidVoxels: solids}
	}
	if len(buildChunks) != len(entries) {
		return NavGraphBakeResult{}, fmt.Errorf("imported world has %d entries but %d chunks were provided", len(entries), len(buildChunks))
	}

	coords := make([]TerrainChunkCoordDef, 0, len(buildChunks))
	for coord := range buildChunks {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool { return terrainCoordLess(coords[i], coords[j]) })
	result := NavGraphBakeResult{Manifest: manifest}
	for _, coord := range coords {
		center := buildChunks[coord]
		halo := make([]NavSpanBuildChunk, 0, 26)
		for _, other := range buildChunks {
			if other.Coord != coord && absNavSpanInt(other.Coord.X-coord.X) <= 1 && absNavSpanInt(other.Coord.Y-coord.Y) <= 1 && absNavSpanInt(other.Coord.Z-coord.Z) <= 1 {
				halo = append(halo, other)
			}
		}
		sort.Slice(halo, func(i, j int) bool { return terrainCoordLess(halo[i].Coord, halo[j].Coord) })
		built, err := BuildNavSourceSpans(NavSpanBuildInput{
			NavID: world.WorldID, BuilderVersion: CurrentNavGraphBuilderVersion,
			SourceHash: center.SourceHash, ChunkSize: world.ChunkSize, VoxelResolution: world.VoxelResolution,
			Center: center, Halo: halo,
		})
		if err != nil {
			return NavGraphBakeResult{}, fmt.Errorf("build navigation source tile %s: %w", TerrainChunkKey(coord), err)
		}
		result.SourceTiles = append(result.SourceTiles, built.Source)
		result.Diagnostics = appendNavGraphDiagnosticCounts(result.Diagnostics, coord, "", "span", func(add func(string)) {
			for _, diagnostic := range built.Diagnostics {
				add(diagnostic.Code)
			}
		})
	}

	for profileIndex, profile := range profiles {
		graphs := make([]NavGraphTileDef, 0, len(result.SourceTiles))
		for _, source := range result.SourceTiles {
			built, err := BuildNavSpanGraphWithContext(source, result.SourceTiles, profile, world.VoxelResolution)
			if err != nil {
				return NavGraphBakeResult{}, fmt.Errorf("build navigation graph tile %s for %q: %w", TerrainChunkKey(source.Coord), profile.ID, err)
			}
			graphs = append(graphs, built.Graph)
			result.Diagnostics = appendNavGraphDiagnosticCounts(result.Diagnostics, source.Coord, profile.ID, "profile", func(add func(string)) {
				for _, diagnostic := range built.SpanDiagnostics {
					add(diagnostic.Code)
				}
			})
			result.Diagnostics = appendNavGraphDiagnosticCounts(result.Diagnostics, source.Coord, profile.ID, "transition", func(add func(string)) {
				for _, diagnostic := range built.TransitionDiagnostics {
					add(diagnostic.Code)
				}
			})
		}
		_, graphs, err := ConnectNavGraphTilesWithContext(result.SourceTiles, graphs, profile, world.ChunkSize, world.VoxelResolution)
		if err != nil {
			return NavGraphBakeResult{}, fmt.Errorf("connect navigation graph tiles for %q: %w", profile.ID, err)
		}
		for _, graph := range graphs {
			result.GraphTiles = append(result.GraphTiles, graph)
			result.Manifest.GraphTiles = append(result.Manifest.GraphTiles, NavGraphTileEntryDef{
				Coord: graph.Coord, AgentProfileID: profile.ID,
				TilePath:   filepath.ToSlash(filepath.Join("graphs", fmt.Sprintf("%d", profileIndex), navGraphCoordFilename(graph.Coord)+NavGraphTileExtension)),
				SourceHash: graph.SourceHash, DependencyHash: graph.DependencyHash,
			})
		}
	}
	for _, source := range result.SourceTiles {
		result.Manifest.SourceTiles = append(result.Manifest.SourceTiles, NavSourceTileEntryDef{
			Coord: source.Coord, TilePath: filepath.ToSlash(filepath.Join("sources", navGraphCoordFilename(source.Coord)+NavSourceTileExtension)),
			SourceHash: source.SourceHash, DependencyHash: source.DependencyHash,
		})
	}
	if validation := ValidateNavGraphBake(&result); validation.HasErrors() {
		return NavGraphBakeResult{}, fmt.Errorf("invalid navigation graph bake: %s", validation.Error())
	}
	return result, nil
}

func navGraphSolidVoxels(chunk ImportedWorldChunkDef, chunkSize int) ([][3]int, error) {
	set := make(map[[3]int]struct{}, len(chunk.Voxels))
	for _, voxel := range chunk.Voxels {
		if voxel.Value == 0 {
			continue
		}
		coord := [3]int{voxel.X, voxel.Y, voxel.Z}
		if voxel.X < 0 || voxel.Y < 0 || voxel.Z < 0 || voxel.X >= chunkSize || voxel.Y >= chunkSize || voxel.Z >= chunkSize {
			return nil, fmt.Errorf("imported world voxel %v is outside chunk %s", coord, TerrainChunkKey(chunk.Coord))
		}
		set[coord] = struct{}{}
	}
	result := make([][3]int, 0, len(set))
	for voxel := range set {
		result = append(result, voxel)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i][0] != result[j][0] {
			return result[i][0] < result[j][0]
		}
		if result[i][1] != result[j][1] {
			return result[i][1] < result[j][1]
		}
		return result[i][2] < result[j][2]
	})
	return result, nil
}

func navGraphOccupancyHash(coord TerrainChunkCoordDef, solids [][3]int) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "nav-graph-occupancy-v1\n%d:%d:%d\n", coord.X, coord.Y, coord.Z)
	for _, voxel := range solids {
		_, _ = fmt.Fprintf(hash, "%d:%d:%d\n", voxel[0], voxel[1], voxel[2])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func appendNavGraphDiagnosticCounts(dst []NavGraphBakeDiagnosticCount, coord TerrainChunkCoordDef, profile, stage string, collect func(func(string))) []NavGraphBakeDiagnosticCount {
	counts := map[string]int{}
	collect(func(code string) { counts[code]++ })
	codes := make([]string, 0, len(counts))
	for code := range counts {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		dst = append(dst, NavGraphBakeDiagnosticCount{Coord: coord, AgentProfileID: profile, Stage: stage, Code: code, Count: counts[code]})
	}
	return dst
}

func navGraphCoordFilename(coord TerrainChunkCoordDef) string {
	return fmt.Sprintf("%d_%d_%d", coord.X, coord.Y, coord.Z)
}
