package content

import (
	"fmt"
	"math"
	"sort"

	"github.com/gekko3d/gekko/content/recastnav"
)

type navRecastInputMesh struct {
	Vertices      []float32
	Triangles     []int32
	TriangleAreas []uint8
}

func buildRecastNavTileFromImportedWorldChunk(chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, opts NavTileBuildOptions) (NavTileBuildResult, error) {
	origin := importedWorldChunkWorldOrigin(chunk)
	worldSize := float32(chunk.ChunkSize) * chunk.VoxelResolution
	navID := opts.NavID
	if navID == "" {
		navID = chunk.WorldID
	}
	if navID == "" {
		navID = newID()
	}
	buildBoundsMin, buildBoundsMax := navRecastExpandedBuildBounds(origin, worldSize, profile)
	tile := &NavTileDef{
		NavID:             navID,
		SchemaVersion:     CurrentNavTileSchemaVersion,
		Coord:             chunk.Coord,
		AgentProfileID:    profile.ID,
		BuilderVersion:    NavBuilderVersionVoxelRecastV1,
		PayloadKind:       NavTilePayloadJSONV1,
		SourcePayloadHash: chunk.PayloadHash,
		SourceDeltaHash:   opts.SourceDeltaHash,
		NavBuildHash:      opts.NavBuildHash,
		BoundsMin:         [3]float32{origin[0], origin[1], origin[2]},
		BoundsMax:         [3]float32{origin[0] + worldSize, origin[1] + worldSize, origin[2] + worldSize},
	}
	input := navRecastInputMeshForChunk(chunk, opts.NeighborChunks, opts.BuildSource, buildBoundsMin, buildBoundsMax)
	stats := NavTileBuildStats{OccupiedVoxels: navTileBuildChunkData(opts.BuildCache, chunk).Occupancy.Count}
	if len(input.Vertices) == 0 || len(input.Triangles) == 0 {
		EnsureNavTileDefaults(tile)
		return NavTileBuildResult{Tile: tile, Stats: stats}, nil
	}
	cfg := navRecastConfigForTile(tile, chunk, profile, buildBoundsMin, buildBoundsMax)
	mesh, err := recastnav.BuildWithTriangleAreas(input.Vertices, input.Triangles, input.TriangleAreas, cfg)
	if err != nil {
		return NavTileBuildResult{}, err
	}
	appendRecastMeshToNavTile(tile, mesh, profile)
	applyNavSurfaceNeighbors(tile, 0, profile)
	appendRecastTileBorderSpans(tile, profile)
	EnsureNavTileDefaults(tile)
	stats.Polygons = len(tile.Polygons)
	return NavTileBuildResult{Tile: tile, Stats: stats}, nil
}

func navRecastExpandedBuildBounds(origin [3]float32, worldSize float32, profile NavAgentProfileDef) ([3]float32, [3]float32) {
	pad := float32(navRecastBorderSize(profile)) * navRecastCellSize(profile)
	return [3]float32{origin[0] - pad, origin[1], origin[2] - pad}, [3]float32{origin[0] + worldSize + pad, origin[1] + worldSize, origin[2] + worldSize + pad}
}

func navRecastCellSize(profile NavAgentProfileDef) float32 {
	cellSize := profile.NavCellSize
	if cellSize <= 0 {
		cellSize = DefaultNavCellSize
	}
	if profile.Radius > 0 {
		cellSize = minNavFloat32(cellSize, maxNavFloat32(0.05, profile.Radius*0.5))
	}
	return cellSize
}

func navRecastBorderSize(profile NavAgentProfileDef) int {
	cellSize := navRecastCellSize(profile)
	return maxNavInt(0, navRecastCeil(profile.Radius/cellSize)+3)
}

func navRecastConfigForTile(tile *NavTileDef, chunk *ImportedWorldChunkDef, profile NavAgentProfileDef, buildBoundsMin [3]float32, buildBoundsMax [3]float32) recastnav.Config {
	cellSize := navRecastCellSize(profile)
	cellHeight := chunk.VoxelResolution
	if cellHeight <= 0 {
		cellHeight = maxNavFloat32(0.05, cellSize/3)
	} else if cellHeight > cellSize {
		cellHeight = minNavFloat32(maxNavFloat32(0.05, cellSize/3), cellHeight)
	}
	worldSize := tile.BoundsMax[0] - tile.BoundsMin[0]
	regionCells := recastnav.CellCount(maxNavFloat32(cellSize, profile.Radius*2), cellSize)
	if regionCells < 1 {
		regionCells = 1
	}
	simplificationError := maxNavFloat32(cellSize*0.75, 0.02)
	if chunk.VoxelResolution > 0 {
		simplificationError = maxNavFloat32(simplificationError, minNavFloat32(chunk.VoxelResolution*0.5, cellSize))
	}
	return recastnav.Config{
		BoundsMin:              buildBoundsMin,
		BoundsMax:              buildBoundsMax,
		CellSize:               cellSize,
		CellHeight:             cellHeight,
		WalkableSlopeAngle:     profile.MaxSlopeDegrees,
		WalkableHeight:         navRecastCeil(profile.Height / cellHeight),
		WalkableClimb:          navRecastCeil(profile.StepHeight / cellHeight),
		WalkableRadius:         navRecastCeil(profile.Radius / cellSize),
		BorderSize:             navRecastBorderSize(profile),
		MaxEdgeLen:             maxNavInt(0, recastnav.CellCount(worldSize, cellSize)/4),
		MaxSimplificationError: simplificationError,
		DetailSampleDist:       maxNavFloat32(0.9, cellSize*6),
		DetailSampleMaxError:   maxNavFloat32(cellHeight, 0.05),
		MinRegionArea:          regionCells * regionCells,
		MergeRegionArea:        regionCells * regionCells * 4,
		MaxVertsPerPoly:        6,
	}
}

func navRecastCeil(value float32) int {
	if value <= 0 {
		return 0
	}
	return int(math.Ceil(float64(value)))
}

func navRecastInputMeshForChunk(chunk *ImportedWorldChunkDef, neighbors map[TerrainChunkCoordDef]*ImportedWorldChunkDef, source *NavBuildSourceDef, boundsMin [3]float32, boundsMax [3]float32) navRecastInputMesh {
	solid := navRecastSolidVoxelSet(chunk, neighbors)
	mesh := navRecastInputMesh{}
	origin := importedWorldChunkWorldOrigin(chunk)
	resolution := chunk.VoxelResolution
	keys := make([][3]int, 0, len(solid))
	for key := range solid {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		if keys[i][1] != keys[j][1] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][2] < keys[j][2]
	})
	for _, key := range keys {
		if !navRecastVoxelIntersectsBounds(origin, resolution, key[0], key[1], key[2], boundsMin, boundsMax) {
			continue
		}
		navRecastAppendVoxelFaces(&mesh, solid, origin, resolution, key[0], key[1], key[2])
	}
	navRecastAppendBuildSourceSurfaces(&mesh, source, boundsMin, boundsMax, !navRecastChunkHasSolidVoxels(chunk))
	return mesh
}

func navRecastChunkHasSolidVoxels(chunk *ImportedWorldChunkDef) bool {
	if chunk == nil {
		return false
	}
	for _, voxel := range chunk.Voxels {
		if voxel.Value != 0 {
			return true
		}
	}
	return false
}

func navRecastVoxelIntersectsBounds(origin [3]float32, resolution float32, x, y, z int, boundsMin [3]float32, boundsMax [3]float32) bool {
	minX := origin[0] + float32(x)*resolution
	minY := origin[1] + float32(y)*resolution
	minZ := origin[2] + float32(z)*resolution
	maxX := minX + resolution
	maxY := minY + resolution
	maxZ := minZ + resolution
	return maxX >= boundsMin[0] && minX <= boundsMax[0] &&
		maxY >= boundsMin[1] && minY <= boundsMax[1] &&
		maxZ >= boundsMin[2] && minZ <= boundsMax[2]
}

func navRecastSolidVoxelSet(chunk *ImportedWorldChunkDef, neighbors map[TerrainChunkCoordDef]*ImportedWorldChunkDef) map[[3]int]struct{} {
	out := make(map[[3]int]struct{}, len(chunk.Voxels))
	addChunk := func(candidate *ImportedWorldChunkDef) {
		if candidate == nil || candidate.ChunkSize != chunk.ChunkSize || candidate.VoxelResolution != chunk.VoxelResolution {
			return
		}
		dx := (candidate.Coord.X - chunk.Coord.X) * chunk.ChunkSize
		dy := (candidate.Coord.Y - chunk.Coord.Y) * chunk.ChunkSize
		dz := (candidate.Coord.Z - chunk.Coord.Z) * chunk.ChunkSize
		for _, voxel := range candidate.Voxels {
			if voxel.Value == 0 {
				continue
			}
			out[[3]int{voxel.X + dx, voxel.Y + dy, voxel.Z + dz}] = struct{}{}
		}
	}
	addChunk(chunk)
	for _, candidate := range neighbors {
		addChunk(candidate)
	}
	return out
}

func navRecastAppendVoxelFaces(mesh *navRecastInputMesh, solid map[[3]int]struct{}, origin [3]float32, resolution float32, x, y, z int) {
	_, solidMinX := solid[[3]int{x - 1, y, z}]
	_, solidMaxX := solid[[3]int{x + 1, y, z}]
	_, solidMinY := solid[[3]int{x, y - 1, z}]
	_, solidMaxY := solid[[3]int{x, y + 1, z}]
	_, solidMinZ := solid[[3]int{x, y, z - 1}]
	_, solidMaxZ := solid[[3]int{x, y, z + 1}]
	x0 := origin[0] + float32(x)*resolution
	x1 := x0 + resolution
	y0 := origin[1] + float32(y)*resolution
	y1 := y0 + resolution
	z0 := origin[2] + float32(z)*resolution
	z1 := z0 + resolution
	if !solidMaxY {
		navRecastAppendQuad(mesh, Vec3{x0, y1, z0}, Vec3{x1, y1, z1}, Vec3{x1, y1, z0}, Vec3{x0, y1, z1}, recastnav.AreaAuto)
	}
	if !solidMinY {
		navRecastAppendQuad(mesh, Vec3{x0, y0, z0}, Vec3{x1, y0, z0}, Vec3{x1, y0, z1}, Vec3{x0, y0, z1}, recastnav.AreaAuto)
	}
	if !solidMinX {
		navRecastAppendQuad(mesh, Vec3{x0, y0, z0}, Vec3{x0, y1, z0}, Vec3{x0, y1, z1}, Vec3{x0, y0, z1}, recastnav.AreaAuto)
	}
	if !solidMaxX {
		navRecastAppendQuad(mesh, Vec3{x1, y0, z0}, Vec3{x1, y0, z1}, Vec3{x1, y1, z1}, Vec3{x1, y1, z0}, recastnav.AreaAuto)
	}
	if !solidMinZ {
		navRecastAppendQuad(mesh, Vec3{x0, y0, z0}, Vec3{x1, y0, z0}, Vec3{x1, y1, z0}, Vec3{x0, y1, z0}, recastnav.AreaAuto)
	}
	if !solidMaxZ {
		navRecastAppendQuad(mesh, Vec3{x0, y0, z1}, Vec3{x0, y1, z1}, Vec3{x1, y1, z1}, Vec3{x1, y0, z1}, recastnav.AreaAuto)
	}
}

func navRecastAppendQuad(mesh *navRecastInputMesh, a, b, c, d Vec3, area uint8) {
	base := int32(len(mesh.Vertices) / 3)
	mesh.Vertices = append(mesh.Vertices,
		a[0], a[1], a[2],
		b[0], b[1], b[2],
		c[0], c[1], c[2],
		d[0], d[1], d[2],
	)
	mesh.Triangles = append(mesh.Triangles, base, base+1, base+2, base, base+3, base+1)
	mesh.TriangleAreas = append(mesh.TriangleAreas, area, area)
}

func navRecastAppendBuildSourceSurfaces(mesh *navRecastInputMesh, source *NavBuildSourceDef, boundsMin [3]float32, boundsMax [3]float32, includeWalkable bool) {
	if source == nil {
		return
	}
	EnsureNavBuildSourceDefaults(source)
	for _, surface := range source.Surfaces {
		if surface.Kind == NavBuildSurfaceWalkable && !includeWalkable {
			continue
		}
		if surface.Kind != NavBuildSurfaceWalkable && surface.Kind != NavBuildSurfaceClearanceBlocker {
			continue
		}
		if len(surface.Vertices) < 3 || !navRecastSurfaceIntersectsBounds(surface, boundsMin, boundsMax) {
			continue
		}
		if len(surface.Indices) >= 3 {
			for i := 0; i+2 < len(surface.Indices); i += 3 {
				a, b, c := surface.Indices[i], surface.Indices[i+1], surface.Indices[i+2]
				if a < 0 || a >= len(surface.Vertices) || b < 0 || b >= len(surface.Vertices) || c < 0 || c >= len(surface.Vertices) {
					continue
				}
				navRecastAppendTriangle(mesh, surface.Vertices[a], surface.Vertices[b], surface.Vertices[c], navRecastSurfaceArea(surface))
			}
			continue
		}
		for i := 1; i+1 < len(surface.Vertices); i++ {
			navRecastAppendTriangle(mesh, surface.Vertices[0], surface.Vertices[i], surface.Vertices[i+1], navRecastSurfaceArea(surface))
		}
	}
}

func navRecastSurfaceArea(surface NavBuildSurfaceDef) uint8 {
	if surface.Kind == NavBuildSurfaceClearanceBlocker {
		return recastnav.AreaNull
	}
	if surface.Kind == NavBuildSurfaceWalkable {
		return recastnav.AreaWalkable
	}
	return recastnav.AreaAuto
}

func navRecastSurfaceIntersectsBounds(surface NavBuildSurfaceDef, boundsMin [3]float32, boundsMax [3]float32) bool {
	if len(surface.Vertices) == 0 {
		return false
	}
	minV := surface.Vertices[0]
	maxV := surface.Vertices[0]
	for _, vertex := range surface.Vertices[1:] {
		for axis := 0; axis < 3; axis++ {
			if vertex[axis] < minV[axis] {
				minV[axis] = vertex[axis]
			}
			if vertex[axis] > maxV[axis] {
				maxV[axis] = vertex[axis]
			}
		}
	}
	return maxV[0] >= boundsMin[0] && minV[0] <= boundsMax[0] &&
		maxV[1] >= boundsMin[1] && minV[1] <= boundsMax[1] &&
		maxV[2] >= boundsMin[2] && minV[2] <= boundsMax[2]
}

func navRecastAppendTriangle(mesh *navRecastInputMesh, a, b, c Vec3, area uint8) {
	base := int32(len(mesh.Vertices) / 3)
	mesh.Vertices = append(mesh.Vertices,
		a[0], a[1], a[2],
		b[0], b[1], b[2],
		c[0], c[1], c[2],
	)
	mesh.Triangles = append(mesh.Triangles, base, base+1, base+2)
	mesh.TriangleAreas = append(mesh.TriangleAreas, area)
}

func appendRecastMeshToNavTile(tile *NavTileDef, mesh recastnav.Mesh, profile NavAgentProfileDef) {
	if tile == nil {
		return
	}
	polygonIDs := make([][]string, len(mesh.Polys))
	for i, poly := range mesh.Polys {
		if len(poly) < 3 {
			continue
		}
		vertices := make([]Vec3, 0, len(poly))
		for _, index := range poly {
			if index < 0 || index >= len(mesh.Vertices) {
				continue
			}
			v := mesh.Vertices[index]
			vertices = append(vertices, Vec3{v[0], v[1], v[2]})
		}
		vertices = navClipNavBuildSourcePolygonToTile(vertices, tile.BoundsMin, tile.BoundsMax)
		if len(vertices) < 3 {
			continue
		}
		polygonIDs[i] = appendNavPolygonVertices(tile, fmt.Sprintf("recast:%d", i), vertices, navRecastPolygonArea(vertices, profile), nil)
		appendRecastPolygonPartNeighbors(tile, polygonIDs[i])
	}
	appendRecastMeshNeighbors(tile, polygonIDs, mesh.Neighbors)
}

func appendRecastPolygonPartNeighbors(tile *NavTileDef, ids []string) {
	if tile == nil || len(ids) < 2 {
		return
	}
	appendRecastNeighborIDs(tile, ids, ids)
}

func appendRecastMeshNeighbors(tile *NavTileDef, polygonIDs [][]string, neighbors [][]int) {
	if tile == nil || len(polygonIDs) == 0 || len(neighbors) == 0 {
		return
	}
	for i, ids := range polygonIDs {
		if len(ids) == 0 || i >= len(neighbors) {
			continue
		}
		for _, neighbor := range neighbors[i] {
			if neighbor < 0 || neighbor >= len(polygonIDs) || len(polygonIDs[neighbor]) == 0 {
				continue
			}
			appendRecastNeighborIDs(tile, ids, polygonIDs[neighbor])
		}
	}
	for i := range tile.Polygons {
		sort.Strings(tile.Polygons[i].Neighbors)
	}
}

func appendRecastNeighborIDs(tile *NavTileDef, aIDs []string, bIDs []string) {
	if tile == nil {
		return
	}
	indices := make(map[string]int, len(tile.Polygons))
	for i, polygon := range tile.Polygons {
		indices[polygon.ID] = i
	}
	for _, aID := range aIDs {
		aIndex, ok := indices[aID]
		if !ok {
			continue
		}
		for _, bID := range bIDs {
			bIndex, ok := indices[bID]
			if !ok || aIndex == bIndex {
				continue
			}
			tile.Polygons[aIndex].Neighbors = appendUniqueNavString(tile.Polygons[aIndex].Neighbors, bID)
			tile.Polygons[bIndex].Neighbors = appendUniqueNavString(tile.Polygons[bIndex].Neighbors, aID)
		}
	}
}

func navRecastPolygonArea(vertices []Vec3, profile NavAgentProfileDef) string {
	normal, ok := navPolygonNormal(vertices)
	if !ok {
		return NavTraversalWalk
	}
	upness := clampNavFloat32(normal[1], -1, 1)
	angle := float32(math.Acos(float64(upness)) * 180.0 / math.Pi)
	if profile.MaxSlopeDegrees > 0 && angle > 1 && angle <= profile.MaxSlopeDegrees+1e-4 {
		return NavTraversalRamp
	}
	return NavTraversalWalk
}

func navPolygonNormal(vertices []Vec3) (Vec3, bool) {
	if len(vertices) < 3 {
		return Vec3{}, false
	}
	for i := 1; i+1 < len(vertices); i++ {
		a := Vec3{
			vertices[i][0] - vertices[0][0],
			vertices[i][1] - vertices[0][1],
			vertices[i][2] - vertices[0][2],
		}
		b := Vec3{
			vertices[i+1][0] - vertices[0][0],
			vertices[i+1][1] - vertices[0][1],
			vertices[i+1][2] - vertices[0][2],
		}
		normal := Vec3{
			a[1]*b[2] - a[2]*b[1],
			a[2]*b[0] - a[0]*b[2],
			a[0]*b[1] - a[1]*b[0],
		}
		length := float32(math.Sqrt(float64(normal[0]*normal[0] + normal[1]*normal[1] + normal[2]*normal[2])))
		if length <= 1e-6 {
			continue
		}
		return Vec3{normal[0] / length, normal[1] / length, normal[2] / length}, true
	}
	return Vec3{}, false
}

func appendRecastTileBorderSpans(tile *NavTileDef, profile NavAgentProfileDef) {
	if tile == nil || len(tile.Polygons) == 0 {
		return
	}
	type spanKey struct {
		polygonID string
		edge      string
		area      string
	}
	tolerance := maxNavFloat32(profile.NavCellSize*1.5, 0.05)
	if tolerance <= 0 {
		tolerance = DefaultNavCellSize * 1.5
	}
	spans := make(map[spanKey][]navPortalBoundarySpan)
	for _, polygon := range tile.Polygons {
		if polygon.ID == "" || len(polygon.Vertices) < 3 {
			continue
		}
		area := firstNonEmptyNavString(polygon.Area, NavTraversalWalk)
		if spanValues := navRecastPolygonBoundarySpansAtPlane(tile, polygon, 0, 2, tile.BoundsMin[0], tolerance); len(spanValues) > 0 {
			key := spanKey{polygonID: polygon.ID, edge: NavBorderEdgeMinX, area: area}
			spans[key] = append(spans[key], spanValues...)
		}
		if spanValues := navRecastPolygonBoundarySpansAtPlane(tile, polygon, 0, 2, tile.BoundsMax[0], tolerance); len(spanValues) > 0 {
			key := spanKey{polygonID: polygon.ID, edge: NavBorderEdgeMaxX, area: area}
			spans[key] = append(spans[key], spanValues...)
		}
		if spanValues := navRecastPolygonBoundarySpansAtPlane(tile, polygon, 2, 0, tile.BoundsMin[2], tolerance); len(spanValues) > 0 {
			key := spanKey{polygonID: polygon.ID, edge: NavBorderEdgeMinZ, area: area}
			spans[key] = append(spans[key], spanValues...)
		}
		if spanValues := navRecastPolygonBoundarySpansAtPlane(tile, polygon, 2, 0, tile.BoundsMax[2], tolerance); len(spanValues) > 0 {
			key := spanKey{polygonID: polygon.ID, edge: NavBorderEdgeMaxZ, area: area}
			spans[key] = append(spans[key], spanValues...)
		}
	}
	if len(spans) == 0 {
		return
	}
	keys := make([]spanKey, 0, len(spans))
	for key := range spans {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].polygonID != keys[j].polygonID {
			return keys[i].polygonID < keys[j].polygonID
		}
		if keys[i].edge != keys[j].edge {
			return keys[i].edge < keys[j].edge
		}
		return keys[i].area < keys[j].area
	})
	tile.BorderSpans = tile.BorderSpans[:0]
	for _, key := range keys {
		for _, span := range navPortalMergeBoundarySpans(spans[key]) {
			tile.BorderSpans = append(tile.BorderSpans, NavBorderSpanDef{
				PolygonID: key.polygonID,
				Edge:      key.edge,
				Min:       span.Min,
				Max:       span.Max,
				Area:      key.area,
			})
		}
	}
}

func navRecastPolygonBoundarySpansAtPlane(tile *NavTileDef, polygon NavPolygonDef, normalAxis int, spanAxis int, coord float32, tolerance float32) []navPortalBoundarySpan {
	spans := navPolygonBoundarySpansAtPlane(tile, polygon, normalAxis, spanAxis, coord)
	if tile == nil || len(polygon.Vertices) < 2 || tolerance <= 0 {
		return navPortalMergeBoundarySpans(spans)
	}
	const epsilon = float32(1e-4)
	for edgeIndex := range polygon.Vertices {
		a, b, ok := navPolygonEdge(tile, polygon, edgeIndex)
		if !ok {
			continue
		}
		if absNavFloat32(a[normalAxis]-coord) > tolerance || absNavFloat32(b[normalAxis]-coord) > tolerance {
			continue
		}
		if absNavFloat32(a[normalAxis]-b[normalAxis]) > tolerance {
			continue
		}
		if absNavFloat32(a[spanAxis]-b[spanAxis]) <= epsilon {
			continue
		}
		spans = appendNavPortalBoundarySpan(spans, a[spanAxis], b[spanAxis])
	}
	return navPortalMergeBoundarySpans(spans)
}
