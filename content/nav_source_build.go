package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type NavBuildSourceBuildOptions struct {
	SourceID         string
	AgentProfile     NavAgentProfileDef
	ExplicitSurfaces []NavBuildExplicitSurfaceInput
	Tags             []string
}

func BuildNavBuildSourceFromImportedWorldManifestPath(importedWorldManifestPath string, opts NavBuildSourceBuildOptions) (*NavBuildSourceDef, error) {
	importedWorldManifestPath = strings.TrimSpace(importedWorldManifestPath)
	if importedWorldManifestPath == "" {
		return nil, fmt.Errorf("imported world manifest path is empty")
	}
	world, err := LoadImportedWorld(importedWorldManifestPath)
	if err != nil {
		return nil, err
	}
	chunks := make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef, len(world.Entries))
	for _, entry := range world.Entries {
		if entry.NonEmptyVoxelCount == 0 {
			continue
		}
		chunk, err := LoadImportedWorldChunk(ResolveImportedWorldChunkPath(entry, importedWorldManifestPath))
		if err != nil {
			return nil, err
		}
		chunks[entry.Coord] = chunk
	}
	return BuildNavBuildSourceFromImportedWorld(world, chunks, opts)
}

func SaveNavBuildSourceForImportedWorldManifest(importedWorldManifestPath string, navBuildSourcePath string, opts NavBuildSourceBuildOptions) (*NavBuildSourceDef, error) {
	if strings.TrimSpace(navBuildSourcePath) == "" {
		navBuildSourcePath = DefaultNavBuildSourcePath(importedWorldManifestPath)
	}
	source, err := BuildNavBuildSourceFromImportedWorldManifestPath(importedWorldManifestPath, opts)
	if err != nil {
		return nil, err
	}
	if err := SaveNavBuildSource(navBuildSourcePath, source); err != nil {
		return nil, err
	}
	return source, nil
}

func BuildNavBuildSourceFromImportedWorld(world *ImportedWorldDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, opts NavBuildSourceBuildOptions) (*NavBuildSourceDef, error) {
	if world == nil {
		return nil, fmt.Errorf("imported world is nil")
	}
	EnsureImportedWorldDefaults(world)
	if world.WorldID == "" {
		return nil, fmt.Errorf("imported world world_id is required")
	}
	if world.ChunkSize <= 0 {
		return nil, fmt.Errorf("imported world chunk_size must be positive")
	}
	if world.VoxelResolution <= 0 {
		return nil, fmt.Errorf("imported world voxel_resolution must be positive")
	}
	profile := opts.AgentProfile
	EnsureNavAgentProfileDefaults(&profile)
	if profile.Radius <= 0 || profile.Height <= 0 || profile.StepHeight <= 0 {
		return nil, fmt.Errorf("nav agent profile radius, height, and step_height must be positive")
	}
	chunks = cloneImportedWorldChunkMap(chunks)
	entriesByCoord := importedWorldEntriesByCoord(world.Entries)
	ensureNavBakeNeighborChunks(world, chunks, entriesByCoord, sortedImportedWorldChunkCoords(chunks), []NavAgentProfileDef{profile})

	sourceID := strings.TrimSpace(opts.SourceID)
	if sourceID == "" {
		sourceID = world.WorldID + "_nav_source"
	}
	source := &NavBuildSourceDef{
		SourceID:      sourceID,
		SchemaVersion: CurrentNavBuildSourceSchemaVersion,
		Kind:          NavBuildSourceKindGeneric,
		SourceWorldID: world.WorldID,
		SourceHash:    navImportedWorldBuildSourceHash(world, chunks, profile, opts.ExplicitSurfaces),
		Tags:          append([]string{"source:imported_world", "generated", "primary"}, opts.Tags...),
	}

	if len(opts.ExplicitSurfaces) > 0 {
		source.Surfaces = append(source.Surfaces, buildNavSourceSurfacesFromExplicitSurfaceInputs(opts.ExplicitSurfaces, profile)...)
	} else {
		coords := sortedImportedWorldChunkCoords(chunks)

		for _, coord := range coords {
			chunk := chunks[coord]
			if chunk == nil {
				continue
			}
			EnsureImportedWorldChunkDefaults(chunk)
			if chunk.NonEmptyVoxelCount == 0 {
				continue
			}
			if chunk.ChunkSize != world.ChunkSize || chunk.VoxelResolution != world.VoxelResolution {
				return nil, fmt.Errorf("chunk %s does not match imported world chunk geometry", TerrainChunkKey(coord))
			}
			surfaces := buildNavSourceSurfacesFromImportedWorldChunk(chunk, chunks, profile)
			source.Surfaces = append(source.Surfaces, surfaces...)
		}
	}
	navBuildSourceUpdateBounds(source)
	EnsureNavBuildSourceDefaults(source)
	return source, nil
}

func cloneImportedWorldChunkMap(chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef) map[TerrainChunkCoordDef]*ImportedWorldChunkDef {
	out := make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef, len(chunks))
	for coord, chunk := range chunks {
		out[coord] = chunk
	}
	return out
}

func BuildNavBuildSourceFromExplicitSurfaces(sourceWorldID string, surfaces []NavBuildExplicitSurfaceInput, opts NavBuildSourceBuildOptions) (*NavBuildSourceDef, error) {
	sourceWorldID = strings.TrimSpace(sourceWorldID)
	if sourceWorldID == "" {
		sourceWorldID = "explicit_surface_world"
	}
	profile := opts.AgentProfile
	EnsureNavAgentProfileDefaults(&profile)
	if profile.Radius <= 0 || profile.Height <= 0 || profile.StepHeight <= 0 {
		return nil, fmt.Errorf("nav agent profile radius, height, and step_height must be positive")
	}
	sourceID := strings.TrimSpace(opts.SourceID)
	if sourceID == "" {
		sourceID = sourceWorldID + "_nav_source"
	}
	source := &NavBuildSourceDef{
		SourceID:      sourceID,
		SchemaVersion: CurrentNavBuildSourceSchemaVersion,
		Kind:          NavBuildSourceKindGeneric,
		SourceWorldID: sourceWorldID,
		SourceHash:    navExplicitSurfaceInputsHash(surfaces, profile),
		Surfaces:      buildNavSourceSurfacesFromExplicitSurfaceInputs(surfaces, profile),
		Tags:          append([]string{"source:explicit_surfaces", "generated", "primary"}, opts.Tags...),
	}
	navBuildSourceUpdateBounds(source)
	EnsureNavBuildSourceDefaults(source)
	return source, nil
}

func buildNavSourceSurfacesFromExplicitSurfaceInputs(inputs []NavBuildExplicitSurfaceInput, profile NavAgentProfileDef) []NavBuildSurfaceDef {
	surfaces := make([]NavBuildSurfaceDef, 0, len(inputs))
	for i, input := range inputs {
		if len(input.Vertices) < 3 {
			continue
		}
		kind := strings.TrimSpace(input.Kind)
		if kind == "" {
			kind = NavBuildSurfaceWalkable
		}
		if kind == NavBuildSurfaceWalkable && !navBuildSurfacePolygonSlopeWalkable(input.Vertices, input.Normal, profile) {
			continue
		}
		area := input.Area
		if area == "" {
			area = NavTraversalWalk
		}
		id := strings.TrimSpace(input.ID)
		if id == "" {
			id = "explicit_surface_" + itoa(i)
		}
		vertices := append([]Vec3(nil), input.Vertices...)
		surface := NavBuildSurfaceDef{
			ID:        id,
			Kind:      kind,
			Vertices:  vertices,
			Normal:    input.Normal,
			Area:      area,
			Flags:     append([]string(nil), input.Flags...),
			SourceTag: input.SourceTag,
			Tags:      append([]string{"source:explicit_surface", "agent_profile:" + profile.ID}, input.Tags...),
		}
		if navVec3Length(surface.Normal) == 0 {
			surface.Normal = navSurfaceNormal(vertices)
		}
		surfaces = append(surfaces, surface)
	}
	return surfaces
}

func buildNavSourceSurfacesFromImportedWorldChunk(chunk *ImportedWorldChunkDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, profile NavAgentProfileDef) []NavBuildSurfaceDef {
	data := navTileBuildChunkData(nil, chunk)
	sampler := newImportedWorldNavOccupancySampler(nil, chunk, data, chunks, profile)
	cells, metrics, _, _ := buildNavWalkableCellsForProfile(chunk, profile, data.CandidateSpans, sampler)
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].X != cells[j].X {
			return cells[i].X < cells[j].X
		}
		if cells[i].Z != cells[j].Z {
			return cells[i].Z < cells[j].Z
		}
		return cells[i].Y < cells[j].Y
	})
	regions := mergeNavWalkableCells(cells, metrics, profile)
	origin := importedWorldChunkWorldOrigin(chunk)
	surfaces := make([]NavBuildSurfaceDef, 0, len(regions))
	for i := range regions {
		vertices := navRegionSurfaceVertices(chunk, origin, metrics, &regions[i])
		if len(vertices) < 3 {
			continue
		}
		surface := NavBuildSurfaceDef{
			ID:        navImportedWorldRegionSurfaceID(chunk.Coord, regions[i]),
			Kind:      NavBuildSurfaceWalkable,
			Vertices:  vertices,
			Normal:    navSurfaceNormal(vertices),
			Area:      NavTraversalWalk,
			SourceTag: "imported_world:" + TerrainChunkKey(chunk.Coord),
			Tags:      []string{"source:imported_world", "agent_profile:" + profile.ID},
		}
		surfaces = append(surfaces, surface)
	}
	return surfaces
}

func navRegionSurfaceVertices(chunk *ImportedWorldChunkDef, origin [3]float32, metrics navBuildCellMetrics, region *navBuildWalkableRegion) []Vec3 {
	if chunk == nil || region == nil {
		return nil
	}
	horizontal := metrics.Horizontal
	if horizontal <= 0 {
		horizontal = chunk.VoxelResolution
	}
	metrics.Horizontal = horizontal
	vertical := metrics.Vertical
	if vertical <= 0 {
		vertical = chunk.VoxelResolution
	}
	if len(region.Contour) >= 3 {
		vertices := make([]Vec3, 0, len(region.Contour))
		for _, point := range region.Contour {
			gridX := float32(point.X)
			gridZ := float32(point.Z)
			vertices = append(vertices, Vec3{
				navRegionGridToWorldX(origin, metrics, gridX),
				origin[1] + navRegionHeightAtGrid(*region, gridX, gridZ)*vertical,
				navRegionGridToWorldZ(origin, metrics, gridZ),
			})
		}
		return vertices
	}
	x0 := navRegionGridToWorldX(origin, metrics, float32(region.MinX))
	x1 := navRegionGridToWorldX(origin, metrics, float32(region.MaxX+1))
	z0 := navRegionGridToWorldZ(origin, metrics, float32(region.MinZ))
	z1 := navRegionGridToWorldZ(origin, metrics, float32(region.MaxZ+1))
	y00 := origin[1] + navRegionCornerY(*region, region.MinX, region.MinZ)*vertical
	y10 := origin[1] + navRegionCornerY(*region, region.MaxX+1, region.MinZ)*vertical
	y11 := origin[1] + navRegionCornerY(*region, region.MaxX+1, region.MaxZ+1)*vertical
	y01 := origin[1] + navRegionCornerY(*region, region.MinX, region.MaxZ+1)*vertical
	return []Vec3{
		{x0, y00, z0},
		{x1, y10, z0},
		{x1, y11, z1},
		{x0, y01, z1},
	}
}

func navImportedWorldRegionSurfaceID(coord TerrainChunkCoordDef, region navBuildWalkableRegion) string {
	return "imported_world:" + TerrainChunkKey(coord) + ":" + navRegionPolygonID(region)
}

func navSurfaceNormal(vertices []Vec3) Vec3 {
	if len(vertices) < 3 {
		return Vec3{}
	}
	normal := navVec3Cross(navVec3Sub(vertices[1], vertices[0]), navVec3Sub(vertices[2], vertices[0]))
	if normal[1] < 0 {
		normal[0] = -normal[0]
		normal[1] = -normal[1]
		normal[2] = -normal[2]
	}
	return normal
}

func navBuildSourceUpdateBounds(source *NavBuildSourceDef) {
	if source == nil || len(source.Surfaces) == 0 {
		return
	}
	hasBounds := false
	var min Vec3
	var max Vec3
	for _, surface := range source.Surfaces {
		for _, vertex := range surface.Vertices {
			if !hasBounds {
				min = vertex
				max = vertex
				hasBounds = true
				continue
			}
			for axis := 0; axis < 3; axis++ {
				if vertex[axis] < min[axis] {
					min[axis] = vertex[axis]
				}
				if vertex[axis] > max[axis] {
					max[axis] = vertex[axis]
				}
			}
		}
	}
	if hasBounds {
		source.BoundsMin = min
		source.BoundsMax = max
	}
}

func navImportedWorldBuildSourceHash(world *ImportedWorldDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, profile NavAgentProfileDef, explicitSurfaces []NavBuildExplicitSurfaceInput) string {
	hashes := make([]string, 0, len(chunks)+3)
	if world != nil {
		hashes = append(hashes, world.SourceHash)
	}
	hashes = append(hashes, navBuildHash(DefaultNavBuilderVersion, profile, "nav_source_imported_world"))
	hashes = append(hashes, navExplicitSurfaceInputsHash(explicitSurfaces, profile))
	coords := make([]TerrainChunkCoordDef, 0, len(chunks))
	for coord := range chunks {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		return terrainChunkCoordLess(coords[i], coords[j])
	})
	for _, coord := range coords {
		chunk := chunks[coord]
		if chunk == nil {
			continue
		}
		hashes = append(hashes, firstNonEmptyNavString(chunk.PayloadHash, importedWorldChunkNavSourceHash(chunk)))
	}
	return navCombinedSourceHash(hashes...)
}

func navExplicitSurfaceInputsHash(inputs []NavBuildExplicitSurfaceInput, profile NavAgentProfileDef) string {
	if len(inputs) == 0 {
		return ""
	}
	h := sha256.New()
	writeStringHash(h, "nav_explicit_surfaces_v1")
	data, _ := json.Marshal(profile)
	_, _ = h.Write(data)
	inputsCopy := append([]NavBuildExplicitSurfaceInput(nil), inputs...)
	sort.Slice(inputsCopy, func(i, j int) bool {
		if inputsCopy[i].ID != inputsCopy[j].ID {
			return inputsCopy[i].ID < inputsCopy[j].ID
		}
		return inputsCopy[i].SourceTag < inputsCopy[j].SourceTag
	})
	data, _ = json.Marshal(inputsCopy)
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
