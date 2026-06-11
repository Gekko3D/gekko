package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const DefaultNavTileDirSuffix = "_navtiles"
const DefaultNavClearanceSourceTileDirSuffix = "_navsources"

type NavBakeOptions struct {
	NavID                        string
	LevelID                      string
	BuilderVersion               string
	TileDirectoryName            string
	ClearanceSourceDirectoryName string
	AgentProfiles                []NavAgentProfileDef
	BuildSourcePath              string
	BuildSource                  *NavBuildSourceDef
	// BuildSourcePrimary lets BuildSource replace voxel-derived regions for tiles where
	// source geometry exists. Voxel-derived regions remain the fallback.
	BuildSourcePrimary bool
	// BuildWorkers controls parallel build_intermediate tile work. Values <= 0 use GOMAXPROCS.
	BuildWorkers int
	Progress     NavBakeProgressFunc
}

type NavBakeResult struct {
	Manifest             *NavManifestDef
	Tiles                map[string]*NavTileDef
	ClearanceSourceTiles map[string]*NavClearanceSourceTileDef
}

type NavBakeProgressFunc func(NavBakeProgress)

type NavBakeProgress struct {
	Stage          string
	Current        int
	Total          int
	HasCoord       bool
	Coord          TerrainChunkCoordDef
	AgentProfileID string
	TilePath       string
	Duration       time.Duration
	Polygons       int
	Portals        int
	Stats          NavTileBuildStats
}

type navBakeIntermediateJob struct {
	Coord   TerrainChunkCoordDef
	Chunk   *ImportedWorldChunkDef
	Profile NavAgentProfileDef
}

type navBakeIntermediateResult struct {
	Job          navBakeIntermediateJob
	Intermediate navTileBuildIntermediate
	Duration     time.Duration
}

const (
	NavBakeProgressStageLoadChunk       = "load_chunk"
	NavBakeProgressStageIntermediate    = "build_intermediate"
	NavBakeProgressStageBuildTile       = "build_tile"
	NavBakeProgressStageBuildSourceTile = "build_source_tile"
	NavBakeProgressStageSkipTile        = "skip_tile"
	NavBakeProgressStagePortalStitch    = "portal_stitch"
	NavBakeProgressStageSaveSourceTile  = "save_source_tile"
	NavBakeProgressStageSaveTile        = "save_tile"
	NavBakeProgressStageSaveManifest    = "save_manifest"
)

func DefaultNavManifestPath(importedWorldManifestPath string) string {
	base := filepath.Base(importedWorldManifestPath)
	if base == "" {
		base = "world.gkworld"
	}
	return filepath.Join(filepath.Dir(importedWorldManifestPath), trimKnownSuffix(base, ".gkworld")+".gknav")
}

func DefaultNavTileDir(navManifestPath string) string {
	base := filepath.Base(navManifestPath)
	if base == "" {
		base = "nav.gknav"
	}
	return filepath.Join(filepath.Dir(navManifestPath), trimKnownSuffix(base, ".gknav")+DefaultNavTileDirSuffix)
}

func DefaultNavClearanceSourceTileDir(navManifestPath string) string {
	base := filepath.Base(navManifestPath)
	if base == "" {
		base = "nav.gknav"
	}
	return filepath.Join(filepath.Dir(navManifestPath), trimKnownSuffix(base, ".gknav")+DefaultNavClearanceSourceTileDirSuffix)
}

func BakeNavFromImportedWorldManifestPath(importedWorldManifestPath string, navManifestPath string, opts NavBakeOptions) (NavBakeResult, error) {
	importedWorldManifestPath = strings.TrimSpace(importedWorldManifestPath)
	if importedWorldManifestPath == "" {
		return NavBakeResult{}, fmt.Errorf("imported world manifest path is empty")
	}
	if strings.TrimSpace(navManifestPath) == "" {
		navManifestPath = DefaultNavManifestPath(importedWorldManifestPath)
	}
	world, err := LoadImportedWorld(importedWorldManifestPath)
	if err != nil {
		return NavBakeResult{}, err
	}
	if opts.BuildSource == nil && strings.TrimSpace(opts.BuildSourcePath) != "" {
		buildSourcePath := ResolveDocumentPath(opts.BuildSourcePath, importedWorldManifestPath)
		source, err := LoadNavBuildSource(buildSourcePath)
		if err != nil {
			return NavBakeResult{}, err
		}
		opts.BuildSource = source
	}
	loadTotal := importedWorldBuildableEntryCount(world.Entries, opts.BuildSource, world.ChunkSize, world.VoxelResolution)
	loaded := 0
	chunks := make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef, len(world.Entries))
	for _, entry := range world.Entries {
		hasSourceGeometry := navBuildSourceHasTileGeometry(opts.BuildSource, entry.Coord, world.ChunkSize, world.VoxelResolution)
		if entry.NonEmptyVoxelCount == 0 && !hasSourceGeometry {
			continue
		}
		if entry.NonEmptyVoxelCount > 0 {
			chunk, err := LoadImportedWorldChunk(ResolveImportedWorldChunkPath(entry, importedWorldManifestPath))
			if err != nil {
				return NavBakeResult{}, err
			}
			chunks[entry.Coord] = chunk
		} else {
			chunks[entry.Coord] = &ImportedWorldChunkDef{
				WorldID:            world.WorldID,
				SchemaVersion:      CurrentImportedWorldChunkSchemaVersion,
				Coord:              entry.Coord,
				ChunkSize:          world.ChunkSize,
				VoxelResolution:    world.VoxelResolution,
				PayloadKind:        world.ChunkPayloadKind,
				NonEmptyVoxelCount: 0,
			}
		}
		loaded++
		reportNavBakeProgress(opts, NavBakeProgress{
			Stage:    NavBakeProgressStageLoadChunk,
			Current:  loaded,
			Total:    loadTotal,
			HasCoord: true,
			Coord:    entry.Coord,
		})
	}
	return BakeNavFromImportedWorld(world, chunks, navManifestPath, opts)
}

func SaveNavBakeForImportedWorldManifest(importedWorldManifestPath string, navManifestPath string, opts NavBakeOptions) (NavBakeResult, error) {
	result, err := BakeNavFromImportedWorldManifestPath(importedWorldManifestPath, navManifestPath, opts)
	if err != nil {
		return NavBakeResult{}, err
	}
	if result.Manifest == nil {
		return NavBakeResult{}, fmt.Errorf("nav bake manifest is nil")
	}
	sourceSaveTotal := len(result.ClearanceSourceTiles)
	sourceSaved := 0
	sourcePaths := make([]string, 0, len(result.ClearanceSourceTiles))
	for path := range result.ClearanceSourceTiles {
		sourcePaths = append(sourcePaths, path)
	}
	sort.Strings(sourcePaths)
	for _, path := range sourcePaths {
		tile := result.ClearanceSourceTiles[path]
		if err := SaveNavClearanceSourceTile(path, tile); err != nil {
			return NavBakeResult{}, fmt.Errorf("save nav clearance source tile %s: %w", path, err)
		}
		sourceSaved++
		progress := NavBakeProgress{
			Stage:    NavBakeProgressStageSaveSourceTile,
			Current:  sourceSaved,
			Total:    sourceSaveTotal,
			TilePath: path,
		}
		if tile != nil {
			progress.HasCoord = true
			progress.Coord = tile.Coord
			progress.Stats = NavTileBuildStats{
				CompactCells: len(tile.Cells),
			}
		}
		reportNavBakeProgress(opts, progress)
	}
	saveTotal := len(result.Tiles)
	saved := 0
	paths := make([]string, 0, len(result.Tiles))
	for path := range result.Tiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		tile := result.Tiles[path]
		if err := SaveNavTile(path, tile); err != nil {
			return NavBakeResult{}, fmt.Errorf("save nav tile %s: %w", path, err)
		}
		saved++
		progress := NavBakeProgress{
			Stage:    NavBakeProgressStageSaveTile,
			Current:  saved,
			Total:    saveTotal,
			TilePath: path,
		}
		if tile != nil {
			progress.HasCoord = true
			progress.Coord = tile.Coord
			progress.AgentProfileID = tile.AgentProfileID
			progress.Polygons = len(tile.Polygons)
			progress.Portals = len(tile.Portals)
			progress.Stats = NavTileBuildStats{
				Polygons: len(tile.Polygons),
				Portals:  len(tile.Portals),
			}
		}
		reportNavBakeProgress(opts, progress)
	}
	if err := SaveNavManifest(navManifestPath, result.Manifest); err != nil {
		return NavBakeResult{}, err
	}
	reportNavBakeProgress(opts, NavBakeProgress{
		Stage:    NavBakeProgressStageSaveManifest,
		Current:  1,
		Total:    1,
		TilePath: navManifestPath,
	})
	return result, nil
}

func BakeNavFromImportedWorld(world *ImportedWorldDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, navManifestPath string, opts NavBakeOptions) (NavBakeResult, error) {
	if world == nil {
		return NavBakeResult{}, fmt.Errorf("imported world is nil")
	}
	EnsureImportedWorldDefaults(world)
	if world.WorldID == "" {
		return NavBakeResult{}, fmt.Errorf("imported world world_id is required")
	}
	if world.ChunkSize <= 0 {
		return NavBakeResult{}, fmt.Errorf("imported world chunk_size must be positive")
	}
	if world.VoxelResolution <= 0 {
		return NavBakeResult{}, fmt.Errorf("imported world voxel_resolution must be positive")
	}
	if strings.TrimSpace(navManifestPath) == "" {
		return NavBakeResult{}, fmt.Errorf("nav manifest path is empty")
	}
	opts = normalizeNavBakeOptions(world, navManifestPath, opts)
	manifest := &NavManifestDef{
		NavID:           opts.NavID,
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		LevelID:         opts.LevelID,
		SourceWorldID:   world.WorldID,
		SourceLevelHash: world.SourceHash,
		BuilderVersion:  opts.BuilderVersion,
		ChunkSize:       world.ChunkSize,
		VoxelResolution: world.VoxelResolution,
		AgentProfiles:   append([]NavAgentProfileDef(nil), opts.AgentProfiles...),
		Sectors:         navSectorsFromImportedWorld(world.Sectors),
		Tags:            []string{"source:imported_world", "generated"},
	}
	EnsureNavManifestDefaults(manifest)

	if chunks == nil {
		chunks = make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef)
	}
	entriesByCoord := importedWorldEntriesByCoord(world.Entries)
	ensureNavBakeBuildSourceChunks(world, chunks, entriesByCoord, opts.BuildSource)
	coords := sortedImportedWorldEntryCoords(entriesByCoord)
	ensureNavBakeNeighborChunks(world, chunks, entriesByCoord, coords, opts.AgentProfiles)

	tiles := make(map[string]*NavTileDef)
	clearanceSourceTiles := make(map[string]*NavClearanceSourceTileDef)
	buildCache := &NavTileBuildCache{TrustPayloadHash: true}
	buildTotal := importedWorldBuildableCoordCount(entriesByCoord, coords, opts.BuildSource, world.ChunkSize, world.VoxelResolution) * len(opts.AgentProfiles)
	sourceBuildTotal := importedWorldNonEmptyCoordCount(entriesByCoord, coords)
	maxClearanceRadius := navBakeMaxAgentRadius(opts.AgentProfiles)
	intermediateJobs := make([]navBakeIntermediateJob, 0, buildTotal)
	for _, coord := range coords {
		entry := entriesByCoord[coord]
		if entry.NonEmptyVoxelCount == 0 && !navBuildSourceHasTileGeometry(opts.BuildSource, coord, world.ChunkSize, world.VoxelResolution) {
			continue
		}
		chunk := chunks[coord]
		if chunk == nil {
			return NavBakeResult{}, fmt.Errorf("missing imported world chunk %s", TerrainChunkKey(coord))
		}
		for _, profile := range opts.AgentProfiles {
			EnsureNavAgentProfileDefaults(&profile)
			intermediateJobs = append(intermediateJobs, navBakeIntermediateJob{
				Coord:   coord,
				Chunk:   chunk,
				Profile: profile,
			})
		}
	}
	if err := prebuildNavBakeIntermediates(buildCache, chunks, intermediateJobs, opts); err != nil {
		return NavBakeResult{}, err
	}
	built := 0
	for _, coord := range coords {
		entry := entriesByCoord[coord]
		if entry.NonEmptyVoxelCount == 0 && !navBuildSourceHasTileGeometry(opts.BuildSource, coord, world.ChunkSize, world.VoxelResolution) {
			continue
		}
		chunk := chunks[coord]
		if chunk == nil {
			return NavBakeResult{}, fmt.Errorf("missing imported world chunk %s", TerrainChunkKey(coord))
		}
		sourceHash := firstNonEmptyNavString(entry.PayloadHash, chunk.PayloadHash, importedWorldChunkNavSourceHash(chunk))
		combinedSourceHash := navCombinedSourceHash(sourceHash, navBuildSourceHash(opts.BuildSource))
		var clearanceSourceForCoord *NavClearanceSourceTileDef
		if entry.NonEmptyVoxelCount > 0 {
			sourceBuildStarted := time.Now()
			sourceResult, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
				NavID:              opts.NavID,
				BuilderVersion:     opts.BuilderVersion,
				SourceDeltaHash:    "",
				NavBuildHash:       navClearanceSourceBuildHash(opts.BuilderVersion, sourceHash),
				NeighborChunks:     chunks,
				BuildCache:         buildCache,
				MaxClearanceRadius: maxClearanceRadius,
			})
			if err != nil {
				return NavBakeResult{}, err
			}
			if sourceResult.Tile != nil && len(sourceResult.Tile.Cells) > 0 {
				sourceResult.Tile.SourcePayloadHash = sourceHash
				clearanceSourceForCoord = sourceResult.Tile
				sourcePath := navClearanceSourceTilePath(navManifestPath, opts.ClearanceSourceDirectoryName, coord)
				clearanceSourceTiles[sourcePath] = sourceResult.Tile
				manifest.ClearanceSourceTiles = append(manifest.ClearanceSourceTiles, NavClearanceSourceTileEntryDef{
					Coord:              coord,
					TilePath:           AuthorDocumentPath(sourcePath, navManifestPath),
					PayloadKind:        sourceResult.Tile.PayloadKind,
					SourcePayloadHash:  sourceResult.Tile.SourcePayloadHash,
					SourceDeltaHash:    sourceResult.Tile.SourceDeltaHash,
					NavBuildHash:       sourceResult.Tile.NavBuildHash,
					BoundsMin:          sourceResult.Tile.BoundsMin,
					BoundsMax:          sourceResult.Tile.BoundsMax,
					MaxClearanceRadius: sourceResult.Tile.MaxClearanceRadius,
					Tags:               []string{"generated", "profile_neutral"},
				})
				reportNavBakeProgress(opts, NavBakeProgress{
					Stage:    NavBakeProgressStageBuildSourceTile,
					Current:  len(clearanceSourceTiles),
					Total:    sourceBuildTotal,
					HasCoord: true,
					Coord:    coord,
					TilePath: sourcePath,
					Duration: time.Since(sourceBuildStarted),
					Stats: NavTileBuildStats{
						CandidateSpans: sourceResult.CandidateSpans,
						AcceptedSpans:  sourceResult.AcceptedSpans,
						CompactCells:   len(sourceResult.Tile.Cells),
					},
				})
			}
		}
		for _, profile := range opts.AgentProfiles {
			EnsureNavAgentProfileDefaults(&profile)
			buildStarted := time.Now()
			tileResult, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
				NavID:              opts.NavID,
				BuilderVersion:     opts.BuilderVersion,
				NavBuildHash:       navBuildHash(opts.BuilderVersion, profile, combinedSourceHash),
				NeighborChunks:     chunks,
				BuildCache:         buildCache,
				BuildSource:        opts.BuildSource,
				ClearanceSource:    clearanceSourceForCoord,
				BuildSourcePrimary: opts.BuildSourcePrimary,
			})
			if err != nil {
				return NavBakeResult{}, err
			}
			buildDuration := time.Since(buildStarted)
			built++
			tile := tileResult.Tile
			if tile == nil || len(tile.Polygons) == 0 {
				reportNavBakeProgress(opts, NavBakeProgress{
					Stage:          NavBakeProgressStageSkipTile,
					Current:        built,
					Total:          buildTotal,
					HasCoord:       true,
					Coord:          coord,
					AgentProfileID: profile.ID,
					Duration:       buildDuration,
					Stats:          tileResult.Stats,
				})
				continue
			}
			if validation := ValidateNavTile(tile); validation.HasErrors() {
				return NavBakeResult{}, fmt.Errorf("built nav tile %s profile %s failed validation: %s", TerrainChunkKey(coord), profile.ID, validation.Error())
			}
			tile.SourcePayloadHash = sourceHash
			tilePath := navTilePath(navManifestPath, opts.TileDirectoryName, coord, profile.ID)
			tiles[tilePath] = tile
			reportNavBakeProgress(opts, NavBakeProgress{
				Stage:          NavBakeProgressStageBuildTile,
				Current:        built,
				Total:          buildTotal,
				HasCoord:       true,
				Coord:          coord,
				AgentProfileID: profile.ID,
				TilePath:       tilePath,
				Duration:       buildDuration,
				Polygons:       len(tile.Polygons),
				Portals:        len(tile.Portals),
				Stats:          tileResult.Stats,
			})
			manifest.Tiles = append(manifest.Tiles, NavTileEntryDef{
				Coord:             coord,
				AgentProfileID:    profile.ID,
				TilePath:          AuthorDocumentPath(tilePath, navManifestPath),
				PayloadKind:       tile.PayloadKind,
				SourcePayloadHash: tile.SourcePayloadHash,
				SourceDeltaHash:   tile.SourceDeltaHash,
				NavBuildHash:      tile.NavBuildHash,
				BoundsMin:         tile.BoundsMin,
				BoundsMax:         tile.BoundsMax,
				Tags:              []string{"generated"},
			})
		}
	}
	sort.Slice(manifest.Tiles, func(i, j int) bool {
		if manifest.Tiles[i].AgentProfileID != manifest.Tiles[j].AgentProfileID {
			return manifest.Tiles[i].AgentProfileID < manifest.Tiles[j].AgentProfileID
		}
		return terrainChunkCoordLess(manifest.Tiles[i].Coord, manifest.Tiles[j].Coord)
	})
	sort.Slice(manifest.ClearanceSourceTiles, func(i, j int) bool {
		return terrainChunkCoordLess(manifest.ClearanceSourceTiles[i].Coord, manifest.ClearanceSourceTiles[j].Coord)
	})
	portalStarted := time.Now()
	tileSlice := navBakeTileSlice(tiles)
	applyNavTilePortals(tileSlice, navAgentProfilesByID(opts.AgentProfiles))
	portalCount := 0
	for _, tile := range tileSlice {
		if tile != nil {
			portalCount += len(tile.Portals)
		}
	}
	reportNavBakeProgress(opts, NavBakeProgress{
		Stage:    NavBakeProgressStagePortalStitch,
		Current:  1,
		Total:    1,
		Duration: time.Since(portalStarted),
		Portals:  portalCount,
		Stats: NavTileBuildStats{
			Portals: portalCount,
		},
	})
	return NavBakeResult{Manifest: manifest, Tiles: tiles, ClearanceSourceTiles: clearanceSourceTiles}, nil
}

func prebuildNavBakeIntermediates(buildCache *NavTileBuildCache, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, jobs []navBakeIntermediateJob, opts NavBakeOptions) error {
	if len(jobs) == 0 {
		return nil
	}
	workerCount := navBakeBuildWorkerCount(opts.BuildWorkers, len(jobs))
	if workerCount <= 1 {
		for i, job := range jobs {
			started := time.Now()
			intermediate := navTileBuildIntermediateForChunk(buildCache, job.Chunk, job.Profile, chunks)
			reportNavBakeProgress(opts, NavBakeProgress{
				Stage:          NavBakeProgressStageIntermediate,
				Current:        i + 1,
				Total:          len(jobs),
				HasCoord:       true,
				Coord:          job.Coord,
				AgentProfileID: job.Profile.ID,
				Duration:       time.Since(started),
				Stats:          intermediate.Stats,
			})
		}
		return nil
	}

	jobCh := make(chan navBakeIntermediateJob)
	resultCh := make(chan navBakeIntermediateResult, workerCount)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer workers.Done()
			for job := range jobCh {
				started := time.Now()
				resultCh <- navBakeIntermediateResult{
					Job:          job,
					Intermediate: navTileBuildIntermediateForChunk(buildCache, job.Chunk, job.Profile, chunks),
					Duration:     time.Since(started),
				}
			}
		}()
	}
	go func() {
		for _, job := range jobs {
			jobCh <- job
		}
		close(jobCh)
		workers.Wait()
		close(resultCh)
	}()

	completed := 0
	for result := range resultCh {
		completed++
		reportNavBakeProgress(opts, NavBakeProgress{
			Stage:          NavBakeProgressStageIntermediate,
			Current:        completed,
			Total:          len(jobs),
			HasCoord:       true,
			Coord:          result.Job.Coord,
			AgentProfileID: result.Job.Profile.ID,
			Duration:       result.Duration,
			Stats:          result.Intermediate.Stats,
		})
	}
	return nil
}

func navBakeBuildWorkerCount(requested int, totalJobs int) int {
	if totalJobs <= 1 {
		return totalJobs
	}
	workers := requested
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers < 1 {
		workers = 1
	}
	if workers > totalJobs {
		workers = totalJobs
	}
	return workers
}

func ensureNavBakeNeighborChunks(world *ImportedWorldDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, entriesByCoord map[TerrainChunkCoordDef]ImportedWorldChunkEntryDef, coords []TerrainChunkCoordDef, profiles []NavAgentProfileDef) {
	if world == nil || chunks == nil || world.ChunkSize <= 0 || world.VoxelResolution <= 0 {
		return
	}
	reach := navBuildChunkReachForProfiles(world.ChunkSize, world.VoxelResolution, profiles)
	for _, coord := range coords {
		if _, ok := chunks[coord]; !ok {
			continue
		}
		for dx := -reach.Horizontal; dx <= reach.Horizontal; dx++ {
			for dy := -reach.Vertical; dy <= reach.Vertical; dy++ {
				for dz := -reach.Horizontal; dz <= reach.Horizontal; dz++ {
					if dx == 0 && dy == 0 && dz == 0 {
						continue
					}
					neighborCoord := TerrainChunkCoordDef{X: coord.X + dx, Y: coord.Y + dy, Z: coord.Z + dz}
					if _, ok := chunks[neighborCoord]; ok {
						continue
					}
					entry, hasEntry := entriesByCoord[neighborCoord]
					if hasEntry && entry.NonEmptyVoxelCount > 0 {
						continue
					}
					sameHorizontalLayer := dy == 0
					knownEmptyNeighbor := hasEntry && entry.NonEmptyVoxelCount == 0
					if !sameHorizontalLayer && !knownEmptyNeighbor {
						continue
					}
					chunks[neighborCoord] = emptyNavBakeImportedWorldChunk(world, neighborCoord)
				}
			}
		}
	}
}

func ensureNavBakeBuildSourceChunks(world *ImportedWorldDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, entriesByCoord map[TerrainChunkCoordDef]ImportedWorldChunkEntryDef, source *NavBuildSourceDef) {
	if world == nil || chunks == nil || entriesByCoord == nil {
		return
	}
	for _, coord := range navBuildSourceTileCoords(source, world.ChunkSize, world.VoxelResolution) {
		entry, hasEntry := entriesByCoord[coord]
		if !hasEntry {
			entry = ImportedWorldChunkEntryDef{
				Coord:              coord,
				NonEmptyVoxelCount: 0,
			}
			entriesByCoord[coord] = entry
		}
		if _, ok := chunks[coord]; ok {
			continue
		}
		if entry.NonEmptyVoxelCount == 0 {
			chunks[coord] = emptyNavBakeImportedWorldChunk(world, coord)
		}
	}
}

func emptyNavBakeImportedWorldChunk(world *ImportedWorldDef, coord TerrainChunkCoordDef) *ImportedWorldChunkDef {
	payloadKind := world.ChunkPayloadKind
	if payloadKind == "" {
		payloadKind = ImportedWorldChunkPayloadSparseJSONV1
	}
	return &ImportedWorldChunkDef{
		WorldID:         world.WorldID,
		SchemaVersion:   CurrentImportedWorldChunkSchemaVersion,
		Coord:           coord,
		ChunkSize:       world.ChunkSize,
		VoxelResolution: world.VoxelResolution,
		PayloadKind:     payloadKind,
	}
}

func navAgentProfilesByID(profiles []NavAgentProfileDef) map[string]NavAgentProfileDef {
	out := make(map[string]NavAgentProfileDef, len(profiles))
	for _, profile := range profiles {
		EnsureNavAgentProfileDefaults(&profile)
		out[profile.ID] = profile
	}
	return out
}

func navBakeTileSlice(tiles map[string]*NavTileDef) []*NavTileDef {
	out := make([]*NavTileDef, 0, len(tiles))
	for _, tile := range tiles {
		if tile != nil {
			out = append(out, tile)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AgentProfileID != out[j].AgentProfileID {
			return out[i].AgentProfileID < out[j].AgentProfileID
		}
		return terrainChunkCoordLess(out[i].Coord, out[j].Coord)
	})
	return out
}

func reportNavBakeProgress(opts NavBakeOptions, progress NavBakeProgress) {
	if opts.Progress != nil {
		opts.Progress(progress)
	}
}

func importedWorldEntriesByCoord(entries []ImportedWorldChunkEntryDef) map[TerrainChunkCoordDef]ImportedWorldChunkEntryDef {
	entriesByCoord := make(map[TerrainChunkCoordDef]ImportedWorldChunkEntryDef, len(entries))
	for _, entry := range entries {
		entriesByCoord[entry.Coord] = entry
	}
	return entriesByCoord
}

func sortedImportedWorldEntryCoords(entriesByCoord map[TerrainChunkCoordDef]ImportedWorldChunkEntryDef) []TerrainChunkCoordDef {
	coords := make([]TerrainChunkCoordDef, 0, len(entriesByCoord))
	for coord := range entriesByCoord {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		return terrainChunkCoordLess(coords[i], coords[j])
	})
	return coords
}

func sortedImportedWorldChunkCoords(chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef) []TerrainChunkCoordDef {
	coords := make([]TerrainChunkCoordDef, 0, len(chunks))
	for coord := range chunks {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		return terrainChunkCoordLess(coords[i], coords[j])
	})
	return coords
}

func navBuildSourceTileCoords(source *NavBuildSourceDef, chunkSize int, voxelResolution float32) []TerrainChunkCoordDef {
	if source == nil || chunkSize <= 0 || voxelResolution <= 0 {
		return nil
	}
	seen := make(map[TerrainChunkCoordDef]struct{})
	for surfaceIndex := range source.Surfaces {
		surface := source.Surfaces[surfaceIndex]
		EnsureNavBuildSurfaceDefaults(&surface)
		if surface.Kind != NavBuildSurfaceWalkable {
			continue
		}
		if len(surface.Indices) >= 3 && len(surface.Indices)%3 == 0 {
			for tri := 0; tri < len(surface.Indices); tri += 3 {
				a := surface.Indices[tri]
				b := surface.Indices[tri+1]
				c := surface.Indices[tri+2]
				if a < 0 || b < 0 || c < 0 || a >= len(surface.Vertices) || b >= len(surface.Vertices) || c >= len(surface.Vertices) {
					continue
				}
				navBuildSourceAddPolygonTileCoords(seen, []Vec3{surface.Vertices[a], surface.Vertices[b], surface.Vertices[c]}, chunkSize, voxelResolution)
			}
			continue
		}
		if len(surface.Vertices) >= 3 {
			navBuildSourceAddPolygonTileCoords(seen, surface.Vertices, chunkSize, voxelResolution)
		}
	}
	coords := make([]TerrainChunkCoordDef, 0, len(seen))
	for coord := range seen {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		return terrainChunkCoordLess(coords[i], coords[j])
	})
	return coords
}

func navBuildSourceAddPolygonTileCoords(seen map[TerrainChunkCoordDef]struct{}, vertices []Vec3, chunkSize int, voxelResolution float32) {
	if seen == nil || len(vertices) < 3 || chunkSize <= 0 || voxelResolution <= 0 {
		return
	}
	minBounds, maxBounds := navBuildSourcePolygonBounds(vertices)
	tileWorldSize := float32(chunkSize) * voxelResolution
	if tileWorldSize <= 0 {
		return
	}
	const epsilon = float32(0.0001)
	minY, maxY := navBuildSourceYTileCoordRange(minBounds[1], maxBounds[1], tileWorldSize)
	minCoord := TerrainChunkCoordDef{
		X: navWorldCoordToTileCoord(minBounds[0]-epsilon, tileWorldSize),
		Y: minY,
		Z: navWorldCoordToTileCoord(minBounds[2]-epsilon, tileWorldSize),
	}
	maxCoord := TerrainChunkCoordDef{
		X: navWorldCoordToTileCoord(maxBounds[0]+epsilon, tileWorldSize),
		Y: maxY,
		Z: navWorldCoordToTileCoord(maxBounds[2]+epsilon, tileWorldSize),
	}
	for x := minCoord.X; x <= maxCoord.X; x++ {
		for y := minCoord.Y; y <= maxCoord.Y; y++ {
			for z := minCoord.Z; z <= maxCoord.Z; z++ {
				coord := TerrainChunkCoordDef{X: x, Y: y, Z: z}
				boundsMin, boundsMax := navTileBoundsForCoord(coord, chunkSize, voxelResolution)
				if len(navClipNavBuildSourcePolygonToTile(vertices, boundsMin, boundsMax)) >= 3 {
					seen[coord] = struct{}{}
				}
			}
		}
	}
}

func navBuildSourceYTileCoordRange(minY, maxY float32, tileWorldSize float32) (int, int) {
	if tileWorldSize <= 0 {
		return 0, -1
	}
	const epsilon = float32(0.0001)
	if absNavFloat32(maxY-minY) <= epsilon {
		coord := navWorldCoordToTileCoord(minY, tileWorldSize)
		return coord, coord
	}
	minCoord := navWorldCoordToTileCoord(minY, tileWorldSize)
	maxCoord := navWorldCoordToTileCoord(maxY-epsilon, tileWorldSize)
	if maxCoord < minCoord {
		maxCoord = minCoord
	}
	return minCoord, maxCoord
}

func navBuildSourcePolygonBounds(vertices []Vec3) (Vec3, Vec3) {
	minBounds := vertices[0]
	maxBounds := vertices[0]
	for _, vertex := range vertices[1:] {
		minBounds, maxBounds, _ = navExpandBuildSourceBounds(minBounds, maxBounds, true, vertex)
	}
	return minBounds, maxBounds
}

func navWorldCoordToTileCoord(value float32, tileWorldSize float32) int {
	return int(math.Floor(float64(value / tileWorldSize)))
}

func navBuildSourceGeometryBounds(source *NavBuildSourceDef) (Vec3, Vec3, bool) {
	if source == nil {
		return Vec3{}, Vec3{}, false
	}
	var minBounds Vec3
	var maxBounds Vec3
	hasBounds := false
	for _, surface := range source.Surfaces {
		EnsureNavBuildSurfaceDefaults(&surface)
		if surface.Kind != NavBuildSurfaceWalkable || len(surface.Vertices) < 3 {
			continue
		}
		if len(surface.Indices) >= 3 {
			for _, index := range surface.Indices {
				if index < 0 || index >= len(surface.Vertices) {
					continue
				}
				minBounds, maxBounds, hasBounds = navExpandBuildSourceBounds(minBounds, maxBounds, hasBounds, surface.Vertices[index])
			}
			continue
		}
		for _, vertex := range surface.Vertices {
			minBounds, maxBounds, hasBounds = navExpandBuildSourceBounds(minBounds, maxBounds, hasBounds, vertex)
		}
	}
	return minBounds, maxBounds, hasBounds
}

func navExpandBuildSourceBounds(minBounds Vec3, maxBounds Vec3, hasBounds bool, vertex Vec3) (Vec3, Vec3, bool) {
	if !hasBounds {
		return vertex, vertex, true
	}
	if vertex[0] < minBounds[0] {
		minBounds[0] = vertex[0]
	}
	if vertex[1] < minBounds[1] {
		minBounds[1] = vertex[1]
	}
	if vertex[2] < minBounds[2] {
		minBounds[2] = vertex[2]
	}
	if vertex[0] > maxBounds[0] {
		maxBounds[0] = vertex[0]
	}
	if vertex[1] > maxBounds[1] {
		maxBounds[1] = vertex[1]
	}
	if vertex[2] > maxBounds[2] {
		maxBounds[2] = vertex[2]
	}
	return minBounds, maxBounds, true
}

func importedWorldNonEmptyEntryCount(entries []ImportedWorldChunkEntryDef) int {
	count := 0
	for _, entry := range entries {
		if entry.NonEmptyVoxelCount > 0 {
			count++
		}
	}
	return count
}

func importedWorldBuildableEntryCount(entries []ImportedWorldChunkEntryDef, source *NavBuildSourceDef, chunkSize int, voxelResolution float32) int {
	count := 0
	for _, entry := range entries {
		if entry.NonEmptyVoxelCount > 0 || navBuildSourceHasTileGeometry(source, entry.Coord, chunkSize, voxelResolution) {
			count++
		}
	}
	return count
}

func importedWorldNonEmptyCoordCount(entriesByCoord map[TerrainChunkCoordDef]ImportedWorldChunkEntryDef, coords []TerrainChunkCoordDef) int {
	count := 0
	for _, coord := range coords {
		if entriesByCoord[coord].NonEmptyVoxelCount > 0 {
			count++
		}
	}
	return count
}

func importedWorldBuildableCoordCount(entriesByCoord map[TerrainChunkCoordDef]ImportedWorldChunkEntryDef, coords []TerrainChunkCoordDef, source *NavBuildSourceDef, chunkSize int, voxelResolution float32) int {
	count := 0
	for _, coord := range coords {
		entry := entriesByCoord[coord]
		if entry.NonEmptyVoxelCount > 0 || navBuildSourceHasTileGeometry(source, coord, chunkSize, voxelResolution) {
			count++
		}
	}
	return count
}

func normalizeNavBakeOptions(world *ImportedWorldDef, navManifestPath string, opts NavBakeOptions) NavBakeOptions {
	if strings.TrimSpace(opts.NavID) == "" {
		if world != nil && strings.TrimSpace(world.WorldID) != "" {
			opts.NavID = world.WorldID + "_nav"
		} else {
			opts.NavID = "nav"
		}
	}
	if strings.TrimSpace(opts.BuilderVersion) == "" {
		opts.BuilderVersion = DefaultNavBuilderVersion
	}
	if strings.TrimSpace(opts.TileDirectoryName) == "" {
		opts.TileDirectoryName = filepath.Base(DefaultNavTileDir(navManifestPath))
	}
	if strings.TrimSpace(opts.ClearanceSourceDirectoryName) == "" {
		opts.ClearanceSourceDirectoryName = filepath.Base(DefaultNavClearanceSourceTileDir(navManifestPath))
	}
	if len(opts.AgentProfiles) == 0 {
		opts.AgentProfiles = []NavAgentProfileDef{DefaultHL1NavAgentProfile()}
	}
	for i := range opts.AgentProfiles {
		EnsureNavAgentProfileDefaults(&opts.AgentProfiles[i])
	}
	return opts
}

func navSectorsFromImportedWorld(sectors []ImportedWorldSectorDef) []NavSectorEntryDef {
	out := make([]NavSectorEntryDef, 0, len(sectors))
	for _, sector := range sectors {
		out = append(out, NavSectorEntryDef{
			Coord:              sector.Coord,
			BoundsMin:          sector.BoundsMin,
			BoundsMax:          sector.BoundsMax,
			AdjacentSectorRefs: append([]TerrainChunkCoordDef(nil), sector.AdjacentSectorRefs...),
			VisibleSectorRefs:  append([]TerrainChunkCoordDef(nil), sector.VisibleSectorRefs...),
			Tags:               append([]string(nil), sector.Tags...),
		})
	}
	return out
}

func navTilePath(navManifestPath string, tileDirectoryName string, coord TerrainChunkCoordDef, profileID string) string {
	name := strings.TrimSpace(profileID)
	if name == "" {
		name = DefaultNavAgentProfileID
	}
	name = sanitizeNavPathToken(name)
	fileName := fmt.Sprintf("%s_%d_%d_%d.gknavtile", name, coord.X, coord.Y, coord.Z)
	return filepath.Join(filepath.Dir(navManifestPath), tileDirectoryName, fileName)
}

func navClearanceSourceTilePath(navManifestPath string, tileDirectoryName string, coord TerrainChunkCoordDef) string {
	fileName := fmt.Sprintf("source_%d_%d_%d.gknavsource", coord.X, coord.Y, coord.Z)
	return filepath.Join(filepath.Dir(navManifestPath), tileDirectoryName, fileName)
}

func navBakeMaxAgentRadius(profiles []NavAgentProfileDef) float32 {
	maxRadius := float32(0)
	for _, profile := range profiles {
		EnsureNavAgentProfileDefaults(&profile)
		if profile.Radius > maxRadius {
			maxRadius = profile.Radius
		}
	}
	if maxRadius <= 0 {
		defaults := DefaultHL1NavAgentProfile()
		maxRadius = defaults.Radius
	}
	return maxRadius
}

func navClearanceSourceBuildHash(builderVersion string, sourceHash string) string {
	return navCombinedSourceHash("clearance_source", builderVersion, sourceHash)
}

func sanitizeNavPathToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "profile"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func importedWorldChunkNavSourceHash(chunk *ImportedWorldChunkDef) string {
	if chunk == nil {
		return ""
	}
	voxels := append([]ImportedWorldVoxelDef(nil), chunk.Voxels...)
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
		if voxels[i].Value != voxels[j].Value {
			return voxels[i].Value < voxels[j].Value
		}
		return voxels[i].MaterialValue < voxels[j].MaterialValue
	})
	h := sha256.New()
	writeStringHash(h, chunk.WorldID)
	writeIntHash(h, chunk.Coord.X)
	writeIntHash(h, chunk.Coord.Y)
	writeIntHash(h, chunk.Coord.Z)
	writeIntHash(h, chunk.ChunkSize)
	writeFloat32Hash(h, chunk.VoxelResolution)
	for _, voxel := range voxels {
		writeIntHash(h, voxel.X)
		writeIntHash(h, voxel.Y)
		writeIntHash(h, voxel.Z)
		_, _ = h.Write([]byte{voxel.Value, ImportedWorldVoxelMaterialValue(voxel)})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func navBuildHash(builderVersion string, profile NavAgentProfileDef, sourceHash string) string {
	h := sha256.New()
	writeStringHash(h, builderVersion)
	writeStringHash(h, sourceHash)
	data, _ := json.Marshal(profile)
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func navBuildSourceHash(source *NavBuildSourceDef) string {
	if source == nil {
		return ""
	}
	clone := *source
	clone.Surfaces = append([]NavBuildSurfaceDef(nil), source.Surfaces...)
	clone.Volumes = append([]NavBuildVolumeDef(nil), source.Volumes...)
	clone.Connectors = append([]NavBuildConnectorDef(nil), source.Connectors...)
	EnsureNavBuildSourceDefaults(&clone)
	h := sha256.New()
	data, _ := json.Marshal(clone)
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func navCombinedSourceHash(values ...string) string {
	h := sha256.New()
	wrote := false
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		writeStringHash(h, value)
		wrote = true
	}
	if !wrote {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func terrainChunkCoordLess(a, b TerrainChunkCoordDef) bool {
	if a.X != b.X {
		return a.X < b.X
	}
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.Z < b.Z
}

func firstNonEmptyNavString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
