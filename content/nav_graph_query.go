package content

import (
	"fmt"
	"math"
)

const (
	NavRouteStartUnsupported = "start_unsupported"
	NavRouteGoalUnsupported  = "goal_unsupported"
	NavRouteNoRoute          = "no_route"
)

type navRouteNode struct {
	Tile   TerrainChunkCoordDef
	Region uint32
}

type navResolvedSpan struct {
	Ref       NavSpanRef
	Region    navRouteNode
	Projected Vec3
}

type navGraphQuery struct {
	chunkSize       int
	voxelResolution float32
	sources         map[TerrainChunkCoordDef]NavSourceTileDef
	graphs          map[TerrainChunkCoordDef]NavGraphTileDef
	spans           map[TerrainChunkCoordDef]map[uint32]NavSpanDef
	spanRegions     map[TerrainChunkCoordDef]map[uint32]uint32
}

func newNavGraphQuery(sources []NavSourceTileDef, graphs []NavGraphTileDef, chunkSize int, voxelResolution float32) (*navGraphQuery, error) {
	if chunkSize <= 0 {
		return nil, fmt.Errorf("navigation graph chunk size must be positive")
	}
	if !finite(voxelResolution) || voxelResolution <= 0 {
		return nil, fmt.Errorf("navigation graph voxel resolution must be finite and positive")
	}
	if len(graphs) == 0 {
		return nil, fmt.Errorf("navigation route requires graph tiles")
	}

	query := &navGraphQuery{
		chunkSize: chunkSize, voxelResolution: voxelResolution,
		sources:     make(map[TerrainChunkCoordDef]NavSourceTileDef, len(sources)),
		graphs:      make(map[TerrainChunkCoordDef]NavGraphTileDef, len(graphs)),
		spans:       make(map[TerrainChunkCoordDef]map[uint32]NavSpanDef, len(sources)),
		spanRegions: make(map[TerrainChunkCoordDef]map[uint32]uint32, len(graphs)),
	}
	for _, source := range sources {
		if _, exists := query.sources[source.Coord]; exists {
			return nil, fmt.Errorf("duplicate navigation source tile %s", TerrainChunkKey(source.Coord))
		}
		if validation := ValidateNavSourceTile(&source); validation.HasErrors() {
			return nil, fmt.Errorf("invalid navigation source tile %s: %s", TerrainChunkKey(source.Coord), validation.Error())
		}
		query.sources[source.Coord] = source
		query.spans[source.Coord] = make(map[uint32]NavSpanDef, len(source.Spans))
		for _, span := range source.Spans {
			query.spans[source.Coord][span.ID] = span
		}
	}

	var navID, builderVersion, profileID string
	for _, graph := range graphs {
		if _, exists := query.graphs[graph.Coord]; exists {
			return nil, fmt.Errorf("duplicate navigation graph tile %s", TerrainChunkKey(graph.Coord))
		}
		if validation := ValidateNavGraphTile(&graph); validation.HasErrors() {
			return nil, fmt.Errorf("invalid navigation graph tile %s: %s", TerrainChunkKey(graph.Coord), validation.Error())
		}
		source, exists := query.sources[graph.Coord]
		if !exists {
			return nil, fmt.Errorf("navigation graph tile %s has no source tile", TerrainChunkKey(graph.Coord))
		}
		if source.NavID != graph.NavID || source.BuilderVersion != graph.BuilderVersion || source.SourceHash != graph.SourceHash || source.DependencyHash != graph.DependencyHash {
			return nil, fmt.Errorf("navigation source and graph tile metadata do not match at %s", TerrainChunkKey(graph.Coord))
		}
		if navID == "" {
			navID, builderVersion, profileID = graph.NavID, graph.BuilderVersion, graph.AgentProfileID
		} else if graph.NavID != navID || graph.BuilderVersion != builderVersion || graph.AgentProfileID != profileID {
			return nil, fmt.Errorf("navigation route graph tiles have incompatible metadata")
		}
		regions := navGraphSpanRegions(graph)
		if len(regions) != len(graph.SpanIDs) {
			return nil, fmt.Errorf("navigation graph tile %s is not region-compressed", TerrainChunkKey(graph.Coord))
		}
		for _, spanID := range graph.SpanIDs {
			if _, exists := query.spans[graph.Coord][spanID]; !exists {
				return nil, fmt.Errorf("navigation graph tile %s references missing source span %d", TerrainChunkKey(graph.Coord), spanID)
			}
		}
		query.graphs[graph.Coord] = graph
		query.spanRegions[graph.Coord] = regions
	}
	return query, nil
}

func (q *navGraphQuery) resolve(point Vec3) (navResolvedSpan, TerrainChunkCoordDef, bool) {
	cellX := int(math.Floor(float64(point[0]) / float64(q.voxelResolution)))
	cellY := int(math.Floor(float64(point[1]) / float64(q.voxelResolution)))
	cellZ := int(math.Floor(float64(point[2]) / float64(q.voxelResolution)))
	tile := TerrainChunkCoordDef{
		X: floorDivNavSpan(cellX, q.chunkSize),
		Y: floorDivNavSpan(cellY, q.chunkSize),
		Z: floorDivNavSpan(cellZ, q.chunkSize),
	}
	localX, localZ := positiveModNavSpan(cellX, q.chunkSize), positiveModNavSpan(cellZ, q.chunkSize)

	bestDistance := float32(math.Inf(1))
	var best navResolvedSpan
	found := false
	for coord, graph := range q.graphs {
		if coord.X != tile.X || coord.Z != tile.Z {
			continue
		}
		for _, spanID := range graph.SpanIDs {
			span := q.spans[coord][spanID]
			if span.X != localX || span.Z != localZ {
				continue
			}
			distance := absFloat32(span.SupportHeight - point[1])
			better := !found || distance < bestDistance || distance == bestDistance && (terrainCoordLess(coord, best.Ref.Tile) || coord == best.Ref.Tile && spanID < best.Ref.Span)
			if !better {
				continue
			}
			bestDistance = distance
			best = navResolvedSpan{
				Ref:       NavSpanRef{Tile: coord, Span: spanID},
				Region:    navRouteNode{Tile: coord, Region: q.spanRegions[coord][spanID]},
				Projected: Vec3{point[0], span.SupportHeight, point[2]},
			}
			found = true
		}
	}
	return best, tile, found
}

func (q *navGraphQuery) spanCenter(ref NavSpanRef) Vec3 {
	span := q.spans[ref.Tile][ref.Span]
	return Vec3{
		(float32(ref.Tile.X*q.chunkSize+span.X) + 0.5) * q.voxelResolution,
		span.SupportHeight,
		(float32(ref.Tile.Z*q.chunkSize+span.Z) + 0.5) * q.voxelResolution,
	}
}

func (q *navGraphQuery) regionCenter(node navRouteNode) Vec3 {
	center := q.graphs[node.Tile].Regions[node.Region].Center
	center[0] += float32(node.Tile.X*q.chunkSize) * q.voxelResolution
	center[2] += float32(node.Tile.Z*q.chunkSize) * q.voxelResolution
	return center
}

func navVec3Distance(a, b Vec3) float32 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
}

func navRouteNodeLess(a, b navRouteNode) bool {
	if a.Tile != b.Tile {
		return terrainCoordLess(a.Tile, b.Tile)
	}
	return a.Region < b.Region
}

func absFloat32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
