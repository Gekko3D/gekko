package content

import (
	"fmt"
	"sort"
)

type NavVoxelHeightfieldDebugOptions struct {
	NeighborChunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef
	BuildCache     *NavTileBuildCache
}

type NavVoxelHeightfieldDebugDef struct {
	WorldID             string                              `json:"world_id,omitempty"`
	BuilderVersion      string                              `json:"builder_version,omitempty"`
	Coord               TerrainChunkCoordDef                `json:"coord"`
	ChunkSize           int                                 `json:"chunk_size"`
	VoxelResolution     float32                             `json:"voxel_resolution"`
	AgentProfile        NavAgentProfileDef                  `json:"agent_profile"`
	Metrics             NavVoxelHeightfieldMetricsDebugDef  `json:"metrics"`
	Summary             NavVoxelHeightfieldSummaryDebugDef  `json:"summary"`
	Spans               []NavVoxelHeightfieldSpanDebugDef   `json:"spans,omitempty"`
	CompactCells        []NavVoxelHeightfieldCellDebugDef   `json:"compact_cells,omitempty"`
	Regions             []NavVoxelHeightfieldRegionDebugDef `json:"regions,omitempty"`
	LoadedNeighborTiles []TerrainChunkCoordDef              `json:"loaded_neighbor_tiles,omitempty"`
}

type NavVoxelHeightfieldMetricsDebugDef struct {
	Horizontal   float32 `json:"horizontal"`
	Vertical     float32 `json:"vertical"`
	WorldAligned bool    `json:"world_aligned,omitempty"`
	OriginX      float32 `json:"origin_x,omitempty"`
	OriginZ      float32 `json:"origin_z,omitempty"`
}

type NavVoxelHeightfieldSummaryDebugDef struct {
	OccupiedVoxels int `json:"occupied_voxels"`
	CandidateSpans int `json:"candidate_spans"`
	AcceptedSpans  int `json:"accepted_spans"`
	RejectedSpans  int `json:"rejected_spans"`
	CompactCells   int `json:"compact_cells"`
	Regions        int `json:"regions"`
}

type NavVoxelHeightfieldSpanDebugDef struct {
	X            int    `json:"x"`
	SolidY       int    `json:"solid_y"`
	FloorY       int    `json:"floor_y"`
	Z            int    `json:"z"`
	WorldFloor   Vec3   `json:"world_floor"`
	Walkable     bool   `json:"walkable"`
	RejectReason string `json:"reject_reason,omitempty"`
}

type NavVoxelHeightfieldCellDebugDef struct {
	X           int      `json:"x"`
	Y           int      `json:"y"`
	Z           int      `json:"z"`
	WorldCenter Vec3     `json:"world_center"`
	PolygonID   string   `json:"polygon_id,omitempty"`
	Neighbors   []string `json:"neighbors,omitempty"`
}

type NavVoxelHeightfieldRegionDebugDef struct {
	ID        string                             `json:"id,omitempty"`
	MinX      int                                `json:"min_x"`
	MaxX      int                                `json:"max_x"`
	MinZ      int                                `json:"min_z"`
	MaxZ      int                                `json:"max_z"`
	BaseY     int                                `json:"base_y"`
	SlopeX    int                                `json:"slope_x,omitempty"`
	SlopeZ    int                                `json:"slope_z,omitempty"`
	HasPlane  bool                               `json:"has_plane,omitempty"`
	PlaneBase float32                            `json:"plane_base,omitempty"`
	PlaneX    float32                            `json:"plane_x,omitempty"`
	PlaneZ    float32                            `json:"plane_z,omitempty"`
	Area      string                             `json:"area,omitempty"`
	Contour   []NavVoxelHeightfieldPointDebugDef `json:"contour,omitempty"`
	CellCount int                                `json:"cell_count"`
	Neighbors []string                           `json:"neighbors,omitempty"`
}

type NavVoxelHeightfieldPointDebugDef struct {
	X int `json:"x"`
	Z int `json:"z"`
}

func BuildNavVoxelHeightfieldDebugFromImportedWorldManifestPath(importedWorldManifestPath string, coord TerrainChunkCoordDef, profile NavAgentProfileDef) (*NavVoxelHeightfieldDebugDef, error) {
	world, err := LoadImportedWorld(importedWorldManifestPath)
	if err != nil {
		return nil, err
	}
	EnsureImportedWorldDefaults(world)
	EnsureNavAgentProfileDefaults(&profile)
	if world.ChunkSize <= 0 {
		return nil, fmt.Errorf("imported world chunk_size must be positive")
	}
	if world.VoxelResolution <= 0 {
		return nil, fmt.Errorf("imported world voxel_resolution must be positive")
	}
	entriesByCoord := importedWorldEntriesByCoord(world.Entries)
	entry, ok := entriesByCoord[coord]
	if !ok {
		return nil, fmt.Errorf("imported world has no chunk entry at %s", TerrainChunkKey(coord))
	}
	reach := navBuildChunkReachForProfile(world.ChunkSize, world.VoxelResolution, profile)
	chunks := make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef)
	for candidateCoord, candidateEntry := range entriesByCoord {
		if !navBuildCoordWithinReach(coord, candidateCoord, reach) {
			continue
		}
		chunk, err := loadNavHeightfieldDebugChunk(importedWorldManifestPath, world, candidateEntry)
		if err != nil {
			return nil, err
		}
		chunks[candidateCoord] = chunk
	}
	chunk := chunks[coord]
	if chunk == nil {
		chunk, err = loadNavHeightfieldDebugChunk(importedWorldManifestPath, world, entry)
		if err != nil {
			return nil, err
		}
		chunks[coord] = chunk
	}
	return BuildNavVoxelHeightfieldDebug(chunk, profile, NavVoxelHeightfieldDebugOptions{
		NeighborChunks: chunks,
	})
}

func BuildNavVoxelHeightfieldDebug(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, opts NavVoxelHeightfieldDebugOptions) (*NavVoxelHeightfieldDebugDef, error) {
	if chunk == nil {
		return nil, fmt.Errorf("imported world chunk is nil")
	}
	if chunk.ChunkSize <= 0 {
		return nil, fmt.Errorf("imported world chunk_size must be positive")
	}
	if chunk.VoxelResolution <= 0 {
		return nil, fmt.Errorf("imported world voxel_resolution must be positive")
	}
	EnsureNavAgentProfileDefaults(&profile)
	if profile.Radius <= 0 || profile.Height <= 0 || profile.StepHeight <= 0 {
		return nil, fmt.Errorf("nav agent profile radius, height, and step_height must be positive")
	}

	data := navTileBuildChunkData(opts.BuildCache, chunk)
	occupancy := importedWorldChunkOccupancy(chunk)
	sampler := newImportedWorldNavOccupancySampler(opts.BuildCache, chunk, data, opts.NeighborChunks, profile)
	spans := buildNavVoxelHeightfieldDebugSpans(chunk, profile, occupancy, sampler)
	intermediate := navTileBuildIntermediateForChunk(opts.BuildCache, chunk, profile, opts.NeighborChunks)
	origin := importedWorldChunkWorldOrigin(chunk)

	out := &NavVoxelHeightfieldDebugDef{
		WorldID:         chunk.WorldID,
		BuilderVersion:  DefaultNavBuilderVersion,
		Coord:           chunk.Coord,
		ChunkSize:       chunk.ChunkSize,
		VoxelResolution: chunk.VoxelResolution,
		AgentProfile:    profile,
		Metrics: NavVoxelHeightfieldMetricsDebugDef{
			Horizontal:   intermediate.Metrics.Horizontal,
			Vertical:     intermediate.Metrics.Vertical,
			WorldAligned: intermediate.Metrics.WorldAligned,
			OriginX:      intermediate.Metrics.OriginX,
			OriginZ:      intermediate.Metrics.OriginZ,
		},
		Spans:        spans,
		CompactCells: navVoxelHeightfieldDebugCells(origin, intermediate.Metrics, intermediate.Cells),
		Regions:      navVoxelHeightfieldDebugRegions(intermediate.Regions),
	}
	out.LoadedNeighborTiles = navVoxelHeightfieldDebugNeighborCoords(chunk.Coord, opts.NeighborChunks)
	for _, span := range spans {
		if span.Walkable {
			out.Summary.AcceptedSpans++
		} else {
			out.Summary.RejectedSpans++
		}
	}
	out.Summary.OccupiedVoxels = len(occupancy)
	out.Summary.CandidateSpans = len(spans)
	out.Summary.CompactCells = len(out.CompactCells)
	out.Summary.Regions = len(out.Regions)
	return out, nil
}

func loadNavHeightfieldDebugChunk(importedWorldManifestPath string, world *ImportedWorldDef, entry ImportedWorldChunkEntryDef) (*ImportedWorldChunkDef, error) {
	if entry.NonEmptyVoxelCount <= 0 {
		return &ImportedWorldChunkDef{
			WorldID:            world.WorldID,
			SchemaVersion:      CurrentImportedWorldChunkSchemaVersion,
			Coord:              entry.Coord,
			ChunkSize:          world.ChunkSize,
			VoxelResolution:    world.VoxelResolution,
			PayloadKind:        world.ChunkPayloadKind,
			NonEmptyVoxelCount: 0,
		}, nil
	}
	chunk, err := LoadImportedWorldChunk(ResolveImportedWorldChunkPath(entry, importedWorldManifestPath))
	if err != nil {
		return nil, fmt.Errorf("load imported world chunk %s: %w", TerrainChunkKey(entry.Coord), err)
	}
	return chunk, nil
}

func buildNavVoxelHeightfieldDebugSpans(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, occupancy map[[3]int]struct{}, sampler importedWorldNavOccupancySampler) []NavVoxelHeightfieldSpanDebugDef {
	keys := sortedNavOccupancyKeys(occupancy)
	origin := importedWorldChunkWorldOrigin(chunk)
	spans := make([]NavVoxelHeightfieldSpanDebugDef, 0, len(keys))
	for _, key := range keys {
		x := key[0]
		solidY := key[1]
		z := key[2]
		floorY := solidY + 1
		rejectReason := ""
		walkable := false
		if floorY < 0 || floorY > chunk.ChunkSize {
			rejectReason = "floor_outside_chunk"
		} else {
			rejectReason = navCellAgentClearanceRejectReason(chunk, profile, sampler, x, floorY, z)
			walkable = rejectReason == ""
		}
		spans = append(spans, NavVoxelHeightfieldSpanDebugDef{
			X:            x,
			SolidY:       solidY,
			FloorY:       floorY,
			Z:            z,
			WorldFloor:   Vec3{origin[0] + (float32(x)+0.5)*chunk.VoxelResolution, origin[1] + float32(floorY)*chunk.VoxelResolution, origin[2] + (float32(z)+0.5)*chunk.VoxelResolution},
			Walkable:     walkable,
			RejectReason: rejectReason,
		})
	}
	return spans
}

func sortedNavOccupancyKeys(occupancy map[[3]int]struct{}) [][3]int {
	keys := make([][3]int, 0, len(occupancy))
	for key := range occupancy {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		if keys[i][2] != keys[j][2] {
			return keys[i][2] < keys[j][2]
		}
		return keys[i][1] < keys[j][1]
	})
	return keys
}

func navVoxelHeightfieldDebugCells(origin [3]float32, metrics navBuildCellMetrics, cells []NavBuildWalkableCell) []NavVoxelHeightfieldCellDebugDef {
	out := make([]NavVoxelHeightfieldCellDebugDef, 0, len(cells))
	for _, cell := range cells {
		out = append(out, NavVoxelHeightfieldCellDebugDef{
			X:           cell.X,
			Y:           cell.Y,
			Z:           cell.Z,
			WorldCenter: Vec3{origin[0] + (float32(cell.X)+0.5)*metrics.Horizontal, origin[1] + float32(cell.Y)*metrics.Vertical, origin[2] + (float32(cell.Z)+0.5)*metrics.Horizontal},
			PolygonID:   cell.PolygonID,
			Neighbors:   append([]string(nil), cell.Neighbors...),
		})
	}
	return out
}

func navVoxelHeightfieldDebugRegions(regions []navBuildWalkableRegion) []NavVoxelHeightfieldRegionDebugDef {
	out := make([]NavVoxelHeightfieldRegionDebugDef, 0, len(regions))
	for _, region := range regions {
		debugRegion := NavVoxelHeightfieldRegionDebugDef{
			ID:        region.PolygonID,
			MinX:      region.MinX,
			MaxX:      region.MaxX,
			MinZ:      region.MinZ,
			MaxZ:      region.MaxZ,
			BaseY:     region.BaseY,
			SlopeX:    region.SlopeX,
			SlopeZ:    region.SlopeZ,
			HasPlane:  region.HasPlane,
			PlaneBase: region.PlaneBase,
			PlaneX:    region.PlaneX,
			PlaneZ:    region.PlaneZ,
			Area:      firstNonEmptyNavString(region.Area, NavTraversalWalk),
			CellCount: len(region.Cells),
			Neighbors: append([]string(nil), region.Neighbors...),
		}
		for _, point := range region.Contour {
			debugRegion.Contour = append(debugRegion.Contour, NavVoxelHeightfieldPointDebugDef{X: point.X, Z: point.Z})
		}
		out = append(out, debugRegion)
	}
	return out
}

func navVoxelHeightfieldDebugNeighborCoords(center TerrainChunkCoordDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef) []TerrainChunkCoordDef {
	if len(chunks) == 0 {
		return nil
	}
	coords := make([]TerrainChunkCoordDef, 0, len(chunks))
	for coord := range chunks {
		if coord == center {
			continue
		}
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		return terrainChunkCoordLess(coords[i], coords[j])
	})
	return coords
}
