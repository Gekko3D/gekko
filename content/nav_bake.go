package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const DefaultNavTileDirSuffix = "_navtiles"

type NavBakeOptions struct {
	NavID             string
	LevelID           string
	BuilderVersion    string
	TileDirectoryName string
	AgentProfiles     []NavAgentProfileDef
	Progress          NavBakeProgressFunc
}

type NavBakeResult struct {
	Manifest *NavManifestDef
	Tiles    map[string]*NavTileDef
}

type NavBakeProgressFunc func(NavBakeProgress)

type NavBakeProgress struct {
	Stage          string
	Current        int
	Total          int
	Coord          TerrainChunkCoordDef
	AgentProfileID string
	TilePath       string
	Polygons       int
}

const (
	NavBakeProgressStageLoadChunk    = "load_chunk"
	NavBakeProgressStageBuildTile    = "build_tile"
	NavBakeProgressStageSkipTile     = "skip_tile"
	NavBakeProgressStageSaveTile     = "save_tile"
	NavBakeProgressStageSaveManifest = "save_manifest"
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
	loadTotal := importedWorldNonEmptyEntryCount(world.Entries)
	loaded := 0
	chunks := make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef, len(world.Entries))
	for _, entry := range world.Entries {
		if entry.NonEmptyVoxelCount == 0 {
			continue
		}
		chunk, err := LoadImportedWorldChunk(ResolveImportedWorldChunkPath(entry, importedWorldManifestPath))
		if err != nil {
			return NavBakeResult{}, err
		}
		chunks[entry.Coord] = chunk
		loaded++
		reportNavBakeProgress(opts, NavBakeProgress{
			Stage:   NavBakeProgressStageLoadChunk,
			Current: loaded,
			Total:   loadTotal,
			Coord:   entry.Coord,
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
	saveTotal := len(result.Tiles)
	saved := 0
	for path, tile := range result.Tiles {
		if err := SaveNavTile(path, tile); err != nil {
			return NavBakeResult{}, err
		}
		saved++
		progress := NavBakeProgress{
			Stage:    NavBakeProgressStageSaveTile,
			Current:  saved,
			Total:    saveTotal,
			TilePath: path,
		}
		if tile != nil {
			progress.Coord = tile.Coord
			progress.AgentProfileID = tile.AgentProfileID
			progress.Polygons = len(tile.Polygons)
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

	entriesByCoord := make(map[TerrainChunkCoordDef]ImportedWorldChunkEntryDef, len(world.Entries))
	coords := make([]TerrainChunkCoordDef, 0, len(world.Entries))
	for _, entry := range world.Entries {
		entriesByCoord[entry.Coord] = entry
		coords = append(coords, entry.Coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		return terrainChunkCoordLess(coords[i], coords[j])
	})

	tiles := make(map[string]*NavTileDef)
	buildTotal := importedWorldNonEmptyCoordCount(entriesByCoord, coords) * len(opts.AgentProfiles)
	built := 0
	for _, coord := range coords {
		entry := entriesByCoord[coord]
		if entry.NonEmptyVoxelCount == 0 {
			continue
		}
		chunk := chunks[coord]
		if chunk == nil {
			return NavBakeResult{}, fmt.Errorf("missing imported world chunk %s", TerrainChunkKey(coord))
		}
		sourceHash := firstNonEmptyNavString(entry.PayloadHash, chunk.PayloadHash, importedWorldChunkNavSourceHash(chunk))
		for _, profile := range opts.AgentProfiles {
			EnsureNavAgentProfileDefaults(&profile)
			tileResult, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
				NavID:          opts.NavID,
				BuilderVersion: opts.BuilderVersion,
				NavBuildHash:   navBuildHash(opts.BuilderVersion, profile, sourceHash),
				NeighborChunks: chunks,
			})
			if err != nil {
				return NavBakeResult{}, err
			}
			built++
			tile := tileResult.Tile
			if tile == nil || len(tile.Polygons) == 0 {
				reportNavBakeProgress(opts, NavBakeProgress{
					Stage:          NavBakeProgressStageSkipTile,
					Current:        built,
					Total:          buildTotal,
					Coord:          coord,
					AgentProfileID: profile.ID,
				})
				continue
			}
			tile.SourcePayloadHash = sourceHash
			tilePath := navTilePath(navManifestPath, opts.TileDirectoryName, coord, profile.ID)
			tiles[tilePath] = tile
			reportNavBakeProgress(opts, NavBakeProgress{
				Stage:          NavBakeProgressStageBuildTile,
				Current:        built,
				Total:          buildTotal,
				Coord:          coord,
				AgentProfileID: profile.ID,
				TilePath:       tilePath,
				Polygons:       len(tile.Polygons),
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
	return NavBakeResult{Manifest: manifest, Tiles: tiles}, nil
}

func reportNavBakeProgress(opts NavBakeOptions, progress NavBakeProgress) {
	if opts.Progress != nil {
		opts.Progress(progress)
	}
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

func importedWorldNonEmptyCoordCount(entriesByCoord map[TerrainChunkCoordDef]ImportedWorldChunkEntryDef, coords []TerrainChunkCoordDef) int {
	count := 0
	for _, coord := range coords {
		if entriesByCoord[coord].NonEmptyVoxelCount > 0 {
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
