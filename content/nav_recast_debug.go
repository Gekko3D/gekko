package content

import (
	"fmt"
	"sort"

	"github.com/gekko3d/gekko/content/recastnav"
)

type NavRecastDebugOptions struct {
	NeighborChunks   map[TerrainChunkCoordDef]*ImportedWorldChunkDef
	BuildCache       *NavTileBuildCache
	BuildSource      *NavBuildSourceDef
	IncludeInputMesh bool
}

type NavRecastDebugDef struct {
	WorldID             string                      `json:"world_id,omitempty"`
	BuilderVersion      string                      `json:"builder_version,omitempty"`
	Coord               TerrainChunkCoordDef        `json:"coord"`
	ChunkSize           int                         `json:"chunk_size"`
	VoxelResolution     float32                     `json:"voxel_resolution"`
	AgentProfile        NavAgentProfileDef          `json:"agent_profile"`
	BuildBoundsMin      [3]float32                  `json:"build_bounds_min"`
	BuildBoundsMax      [3]float32                  `json:"build_bounds_max"`
	TileBoundsMin       [3]float32                  `json:"tile_bounds_min"`
	TileBoundsMax       [3]float32                  `json:"tile_bounds_max"`
	Config              NavRecastConfigDebugDef     `json:"config"`
	Summary             NavRecastSummaryDebugDef    `json:"summary"`
	InputMesh           *NavRecastInputMeshDebugDef `json:"input_mesh,omitempty"`
	PolyMesh            NavRecastPolyMeshDebugDef   `json:"poly_mesh"`
	DetailMesh          NavRecastDetailMeshDebugDef `json:"detail_mesh"`
	Contours            []NavRecastContourDebugDef  `json:"contours,omitempty"`
	ConvertedTile       *NavTileDef                 `json:"converted_tile,omitempty"`
	LoadedNeighborTiles []TerrainChunkCoordDef      `json:"loaded_neighbor_tiles,omitempty"`
}

type NavRecastConfigDebugDef struct {
	CellSize               float32 `json:"cell_size"`
	CellHeight             float32 `json:"cell_height"`
	WalkableSlopeAngle     float32 `json:"walkable_slope_angle"`
	WalkableHeight         int     `json:"walkable_height"`
	WalkableClimb          int     `json:"walkable_climb"`
	WalkableRadius         int     `json:"walkable_radius"`
	BorderSize             int     `json:"border_size"`
	MaxEdgeLen             int     `json:"max_edge_len"`
	MaxSimplificationError float32 `json:"max_simplification_error"`
	DetailSampleDist       float32 `json:"detail_sample_dist"`
	DetailSampleMaxError   float32 `json:"detail_sample_max_error"`
	MinRegionArea          int     `json:"min_region_area"`
	MergeRegionArea        int     `json:"merge_region_area"`
	MaxVertsPerPoly        int     `json:"max_verts_per_poly"`
}

type NavRecastSummaryDebugDef struct {
	OccupiedVoxels       int `json:"occupied_voxels"`
	InputVertices        int `json:"input_vertices"`
	InputTriangles       int `json:"input_triangles"`
	InputAutoTriangles   int `json:"input_auto_triangles"`
	InputWalkableTris    int `json:"input_walkable_triangles"`
	InputBlockedTris     int `json:"input_blocked_triangles"`
	PolyMeshVertices     int `json:"poly_mesh_vertices"`
	PolyMeshPolygons     int `json:"poly_mesh_polygons"`
	DetailMeshVertices   int `json:"detail_mesh_vertices"`
	DetailMeshTriangles  int `json:"detail_mesh_triangles"`
	Contours             int `json:"contours"`
	ConvertedPolygons    int `json:"converted_polygons"`
	ConvertedBorderSpans int `json:"converted_border_spans"`
}

type NavRecastInputMeshDebugDef struct {
	Vertices      []Vec3  `json:"vertices,omitempty"`
	Triangles     [][]int `json:"triangles,omitempty"`
	TriangleAreas []uint8 `json:"triangle_areas,omitempty"`
}

type NavRecastPolyMeshDebugDef struct {
	Vertices []Vec3                     `json:"vertices,omitempty"`
	Polygons []NavRecastPolygonDebugDef `json:"polygons,omitempty"`
}

type NavRecastPolygonDebugDef struct {
	Vertices  []int `json:"vertices,omitempty"`
	Neighbors []int `json:"neighbors,omitempty"`
	Area      uint8 `json:"area,omitempty"`
}

type NavRecastDetailMeshDebugDef struct {
	SubMeshes []NavRecastDetailSubMeshDebugDef `json:"sub_meshes,omitempty"`
	Vertices  []Vec3                           `json:"vertices,omitempty"`
	Triangles [][]int                          `json:"triangles,omitempty"`
}

type NavRecastDetailSubMeshDebugDef struct {
	VertexBase    int `json:"vertex_base"`
	VertexCount   int `json:"vertex_count"`
	TriangleBase  int `json:"triangle_base"`
	TriangleCount int `json:"triangle_count"`
}

type NavRecastContourDebugDef struct {
	Area     uint8  `json:"area,omitempty"`
	Vertices []Vec3 `json:"vertices,omitempty"`
}

func BuildNavRecastDebugFromImportedWorldManifestPath(importedWorldManifestPath string, coord TerrainChunkCoordDef, profile NavAgentProfileDef, opts NavRecastDebugOptions) (*NavRecastDebugDef, error) {
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
		chunk, err := loadNavRecastDebugChunk(importedWorldManifestPath, world, candidateEntry)
		if err != nil {
			return nil, err
		}
		chunks[candidateCoord] = chunk
	}
	chunk := chunks[coord]
	if chunk == nil {
		chunk, err = loadNavRecastDebugChunk(importedWorldManifestPath, world, entry)
		if err != nil {
			return nil, err
		}
		chunks[coord] = chunk
	}
	opts.NeighborChunks = chunks
	return BuildNavRecastDebug(chunk, profile, opts)
}

func BuildNavRecastDebug(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, opts NavRecastDebugOptions) (*NavRecastDebugDef, error) {
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
	origin := importedWorldChunkWorldOrigin(chunk)
	worldSize := float32(chunk.ChunkSize) * chunk.VoxelResolution
	buildBoundsMin, buildBoundsMax := navRecastExpandedBuildBounds(origin, worldSize, profile)
	tile := &NavTileDef{
		NavID:          firstNonEmptyNavString(chunk.WorldID, "recast-debug"),
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          chunk.Coord,
		AgentProfileID: profile.ID,
		BuilderVersion: NavBuilderVersionVoxelRecastV1,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{origin[0], origin[1], origin[2]},
		BoundsMax:      [3]float32{origin[0] + worldSize, origin[1] + worldSize, origin[2] + worldSize},
	}
	input := navRecastInputMeshForChunk(chunk, opts.NeighborChunks, opts.BuildSource, buildBoundsMin, buildBoundsMax)
	cfg := navRecastConfigForTile(tile, chunk, profile, buildBoundsMin, buildBoundsMax)
	mesh, err := recastnav.BuildWithTriangleAreas(input.Vertices, input.Triangles, input.TriangleAreas, cfg)
	if err != nil {
		return nil, err
	}
	appendRecastMeshToNavTile(tile, mesh, profile)
	applyNavSurfaceNeighbors(tile, 0, profile)
	appendRecastTileBorderSpans(tile, profile)
	EnsureNavTileDefaults(tile)

	debug := &NavRecastDebugDef{
		WorldID:             chunk.WorldID,
		BuilderVersion:      NavBuilderVersionVoxelRecastV1,
		Coord:               chunk.Coord,
		ChunkSize:           chunk.ChunkSize,
		VoxelResolution:     chunk.VoxelResolution,
		AgentProfile:        profile,
		BuildBoundsMin:      buildBoundsMin,
		BuildBoundsMax:      buildBoundsMax,
		TileBoundsMin:       tile.BoundsMin,
		TileBoundsMax:       tile.BoundsMax,
		Config:              navRecastDebugConfig(cfg),
		Summary:             navRecastDebugSummary(chunk, input, mesh, tile),
		PolyMesh:            navRecastDebugPolyMesh(mesh),
		DetailMesh:          navRecastDebugDetailMesh(mesh.DetailMesh),
		Contours:            navRecastDebugContours(mesh.Contours),
		ConvertedTile:       tile,
		LoadedNeighborTiles: navVoxelHeightfieldDebugNeighborCoords(chunk.Coord, opts.NeighborChunks),
	}
	if opts.IncludeInputMesh {
		debug.InputMesh = navRecastDebugInputMesh(input)
	}
	return debug, nil
}

func loadNavRecastDebugChunk(importedWorldManifestPath string, world *ImportedWorldDef, entry ImportedWorldChunkEntryDef) (*ImportedWorldChunkDef, error) {
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

func navRecastDebugConfig(cfg recastnav.Config) NavRecastConfigDebugDef {
	return NavRecastConfigDebugDef{
		CellSize:               cfg.CellSize,
		CellHeight:             cfg.CellHeight,
		WalkableSlopeAngle:     cfg.WalkableSlopeAngle,
		WalkableHeight:         cfg.WalkableHeight,
		WalkableClimb:          cfg.WalkableClimb,
		WalkableRadius:         cfg.WalkableRadius,
		BorderSize:             cfg.BorderSize,
		MaxEdgeLen:             cfg.MaxEdgeLen,
		MaxSimplificationError: cfg.MaxSimplificationError,
		DetailSampleDist:       cfg.DetailSampleDist,
		DetailSampleMaxError:   cfg.DetailSampleMaxError,
		MinRegionArea:          cfg.MinRegionArea,
		MergeRegionArea:        cfg.MergeRegionArea,
		MaxVertsPerPoly:        cfg.MaxVertsPerPoly,
	}
}

func navRecastDebugSummary(chunk *ImportedWorldChunkDef, input navRecastInputMesh, mesh recastnav.Mesh, tile *NavTileDef) NavRecastSummaryDebugDef {
	out := NavRecastSummaryDebugDef{
		OccupiedVoxels:       navTileBuildChunkData(nil, chunk).Occupancy.Count,
		InputVertices:        len(input.Vertices) / 3,
		InputTriangles:       len(input.Triangles) / 3,
		PolyMeshVertices:     len(mesh.Vertices),
		PolyMeshPolygons:     len(mesh.Polys),
		DetailMeshVertices:   len(mesh.DetailMesh.Vertices),
		DetailMeshTriangles:  len(mesh.DetailMesh.Triangles),
		Contours:             len(mesh.Contours),
		ConvertedPolygons:    len(tile.Polygons),
		ConvertedBorderSpans: len(tile.BorderSpans),
	}
	for _, area := range input.TriangleAreas {
		switch area {
		case recastnav.AreaWalkable:
			out.InputWalkableTris++
		case recastnav.AreaNull:
			out.InputBlockedTris++
		default:
			out.InputAutoTriangles++
		}
	}
	return out
}

func navRecastDebugInputMesh(input navRecastInputMesh) *NavRecastInputMeshDebugDef {
	out := &NavRecastInputMeshDebugDef{
		Vertices:      make([]Vec3, 0, len(input.Vertices)/3),
		Triangles:     make([][]int, 0, len(input.Triangles)/3),
		TriangleAreas: append([]uint8(nil), input.TriangleAreas...),
	}
	for i := 0; i+2 < len(input.Vertices); i += 3 {
		out.Vertices = append(out.Vertices, Vec3{input.Vertices[i], input.Vertices[i+1], input.Vertices[i+2]})
	}
	for i := 0; i+2 < len(input.Triangles); i += 3 {
		out.Triangles = append(out.Triangles, []int{int(input.Triangles[i]), int(input.Triangles[i+1]), int(input.Triangles[i+2])})
	}
	return out
}

func navRecastDebugPolyMesh(mesh recastnav.Mesh) NavRecastPolyMeshDebugDef {
	out := NavRecastPolyMeshDebugDef{
		Vertices: make([]Vec3, 0, len(mesh.Vertices)),
		Polygons: make([]NavRecastPolygonDebugDef, 0, len(mesh.Polys)),
	}
	for _, vertex := range mesh.Vertices {
		out.Vertices = append(out.Vertices, Vec3{vertex[0], vertex[1], vertex[2]})
	}
	for i, polygon := range mesh.Polys {
		debugPolygon := NavRecastPolygonDebugDef{
			Vertices: append([]int(nil), polygon...),
		}
		if i < len(mesh.Neighbors) {
			debugPolygon.Neighbors = append([]int(nil), mesh.Neighbors[i]...)
			sort.Ints(debugPolygon.Neighbors)
		}
		if i < len(mesh.Areas) {
			debugPolygon.Area = mesh.Areas[i]
		}
		out.Polygons = append(out.Polygons, debugPolygon)
	}
	return out
}

func navRecastDebugDetailMesh(mesh recastnav.DetailMesh) NavRecastDetailMeshDebugDef {
	out := NavRecastDetailMeshDebugDef{
		SubMeshes: make([]NavRecastDetailSubMeshDebugDef, 0, len(mesh.SubMeshes)),
		Vertices:  make([]Vec3, 0, len(mesh.Vertices)),
		Triangles: make([][]int, 0, len(mesh.Triangles)),
	}
	for _, subMesh := range mesh.SubMeshes {
		out.SubMeshes = append(out.SubMeshes, NavRecastDetailSubMeshDebugDef{
			VertexBase:    subMesh.VertexBase,
			VertexCount:   subMesh.VertexCount,
			TriangleBase:  subMesh.TriangleBase,
			TriangleCount: subMesh.TriangleCount,
		})
	}
	for _, vertex := range mesh.Vertices {
		out.Vertices = append(out.Vertices, Vec3{vertex[0], vertex[1], vertex[2]})
	}
	for _, triangle := range mesh.Triangles {
		out.Triangles = append(out.Triangles, append([]int(nil), triangle...))
	}
	return out
}

func navRecastDebugContours(contours []recastnav.Contour) []NavRecastContourDebugDef {
	out := make([]NavRecastContourDebugDef, 0, len(contours))
	for _, contour := range contours {
		debugContour := NavRecastContourDebugDef{
			Area:     contour.Area,
			Vertices: make([]Vec3, 0, len(contour.Vertices)),
		}
		for _, vertex := range contour.Vertices {
			debugContour.Vertices = append(debugContour.Vertices, Vec3{vertex[0], vertex[1], vertex[2]})
		}
		out = append(out, debugContour)
	}
	return out
}
