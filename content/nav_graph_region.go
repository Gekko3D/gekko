package content

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type navRegionEdgeClass struct {
	kind  string
	flags string
}

type navRegionNeighbor struct {
	span  uint32
	class navRegionEdgeClass
}

// CompressNavGraphRegions groups mutually reachable spans without crossing
// area or traversal-class boundaries, then emits directed boundary runs.
func CompressNavGraphRegions(source NavSourceTileDef, graph NavGraphTileDef, voxelResolution float32) (NavGraphTileDef, error) {
	return compressNavGraphRegions(source, graph, voxelResolution, nil)
}

func compressNavGraphRegions(source NavSourceTileDef, graph NavGraphTileDef, voxelResolution float32, partition func(NavSpanDef) string) (NavGraphTileDef, error) {
	if validation := ValidateNavSourceTile(&source); validation.HasErrors() {
		return NavGraphTileDef{}, fmt.Errorf("invalid navigation source tile: %s", validation.Error())
	}
	if validation := ValidateNavGraphTile(&graph); validation.HasErrors() {
		return NavGraphTileDef{}, fmt.Errorf("invalid navigation graph tile: %s", validation.Error())
	}
	if !finite(voxelResolution) || voxelResolution <= 0 {
		return NavGraphTileDef{}, fmt.Errorf("navigation graph voxel resolution must be finite and positive")
	}
	if source.NavID != graph.NavID || source.Coord != graph.Coord || source.BuilderVersion != graph.BuilderVersion || source.SourceHash != graph.SourceHash || source.DependencyHash != graph.DependencyHash {
		return NavGraphTileDef{}, fmt.Errorf("navigation source and graph tile metadata do not match")
	}

	spans := make(map[uint32]NavSpanDef, len(source.Spans))
	for _, span := range source.Spans {
		spans[span.ID] = span
	}
	accepted := make(map[uint32]struct{}, len(graph.SpanIDs))
	spanClasses := make(map[uint32]string, len(graph.SpanIDs))
	for _, id := range graph.SpanIDs {
		span, ok := spans[id]
		if !ok {
			return NavGraphTileDef{}, fmt.Errorf("navigation graph references missing source span %d", id)
		}
		accepted[id] = struct{}{}
		spanClasses[id] = navRegionPartitionClass(span, partition)
	}

	type pair struct{ from, to uint32 }
	edgeClasses := make(map[pair]map[navRegionEdgeClass]struct{})
	for _, edge := range graph.SpanTransitions {
		if edge.To.Tile != graph.Coord {
			continue
		}
		kind := edge.Kind
		if kind == NavTransitionWalk || kind == NavTransitionStair || kind == NavTransitionStep {
			kind = NavTransitionWalk
		}
		class := navRegionEdgeClass{kind: kind, flags: navRegionFlagsKey(edge.RequiresFlags)}
		classes := edgeClasses[pair{edge.From, edge.To.Span}]
		if classes == nil {
			classes = make(map[navRegionEdgeClass]struct{})
			edgeClasses[pair{edge.From, edge.To.Span}] = classes
		}
		classes[class] = struct{}{}
	}
	neighbors := make(map[uint32][]navRegionNeighbor)
	for edge, classes := range edgeClasses {
		for class := range classes {
			if _, reciprocal := edgeClasses[pair{edge.to, edge.from}][class]; reciprocal {
				neighbors[edge.from] = append(neighbors[edge.from], navRegionNeighbor{span: edge.to, class: class})
			}
		}
	}
	for id := range neighbors {
		sort.Slice(neighbors[id], func(i, j int) bool {
			a, b := neighbors[id][i], neighbors[id][j]
			if a.class.kind != b.class.kind {
				return a.class.kind < b.class.kind
			}
			if a.class.flags != b.class.flags {
				return a.class.flags < b.class.flags
			}
			return a.span < b.span
		})
	}

	graph.Regions = nil
	graph.Transitions = nil
	spanRegions := make(map[uint32]uint32, len(graph.SpanIDs))
	for _, seed := range graph.SpanIDs {
		if _, assigned := spanRegions[seed]; assigned {
			continue
		}
		regionID := uint32(len(graph.Regions))
		spanRegions[seed] = regionID
		members := []uint32{seed}
		queue := []uint32{seed}
		var regionClass *navRegionEdgeClass
		seedClass := spanClasses[seed]
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			for _, neighbor := range neighbors[current] {
				if _, ok := accepted[neighbor.span]; !ok || spanClasses[neighbor.span] != seedClass {
					continue
				}
				if regionClass != nil && neighbor.class != *regionClass {
					continue
				}
				if _, assigned := spanRegions[neighbor.span]; assigned {
					continue
				}
				if regionClass == nil {
					class := neighbor.class
					regionClass = &class
				}
				spanRegions[neighbor.span] = regionID
				members = append(members, neighbor.span)
				queue = append(queue, neighbor.span)
			}
		}
		graph.Regions = append(graph.Regions, buildNavRegion(regionID, members, spans, voxelResolution))
	}

	transitions, err := buildNavRegionTransitions(graph, spans, spanRegions, voxelResolution)
	if err != nil {
		return NavGraphTileDef{}, err
	}
	graph.Transitions = transitions
	if validation := ValidateNavGraphTile(&graph); validation.HasErrors() {
		return NavGraphTileDef{}, fmt.Errorf("invalid compressed navigation graph tile: %s", validation.Error())
	}
	return graph, nil
}

func navRegionPartitionClass(span NavSpanDef, partition func(NavSpanDef) string) string {
	class := navRegionSpanClass(span)
	if partition != nil {
		class += "\x00" + partition(span)
	}
	return class
}

func navRegionSpanClass(span NavSpanDef) string {
	return span.Area + "\x00" + navRegionFlagsKey(span.Flags)
}

func navRegionFlagsKey(flags []string) string {
	flags = append([]string(nil), flags...)
	sort.Strings(flags)
	return strings.Join(flags, "\x00")
}

func navRegionFlags(key string) []string {
	if key == "" {
		return nil
	}
	return strings.Split(key, "\x00")
}

func buildNavRegion(id uint32, members []uint32, spans map[uint32]NavSpanDef, voxelResolution float32) NavRegionDef {
	sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
	region := NavRegionDef{ID: id, Area: spans[members[0]].Area}
	var center [3]float64
	for _, spanID := range members {
		if len(region.SpanRuns) == 0 || spanID != region.SpanRuns[len(region.SpanRuns)-1].Start+region.SpanRuns[len(region.SpanRuns)-1].Count {
			region.SpanRuns = append(region.SpanRuns, NavSpanRunDef{Start: spanID, Count: 1})
		} else {
			region.SpanRuns[len(region.SpanRuns)-1].Count++
		}
		span := spans[spanID]
		minX, maxX := float32(span.X)*voxelResolution, float32(span.X+1)*voxelResolution
		minZ, maxZ := float32(span.Z)*voxelResolution, float32(span.Z+1)*voxelResolution
		if spanID == members[0] {
			region.BoundsMin = Vec3{minX, span.SupportHeight, minZ}
			region.BoundsMax = Vec3{maxX, span.SupportHeight, maxZ}
			region.HeightMin, region.HeightMax = span.SupportHeight, span.SupportHeight
		} else {
			region.BoundsMin[0] = min(region.BoundsMin[0], minX)
			region.BoundsMin[1] = min(region.BoundsMin[1], span.SupportHeight)
			region.BoundsMin[2] = min(region.BoundsMin[2], minZ)
			region.BoundsMax[0] = max(region.BoundsMax[0], maxX)
			region.BoundsMax[1] = max(region.BoundsMax[1], span.SupportHeight)
			region.BoundsMax[2] = max(region.BoundsMax[2], maxZ)
			region.HeightMin = min(region.HeightMin, span.SupportHeight)
			region.HeightMax = max(region.HeightMax, span.SupportHeight)
		}
		center[0] += float64((float32(span.X) + 0.5) * voxelResolution)
		center[1] += float64(span.SupportHeight)
		center[2] += float64((float32(span.Z) + 0.5) * voxelResolution)
	}
	count := float64(len(members))
	region.Center = Vec3{float32(center[0] / count), float32(center[1] / count), float32(center[2] / count)}
	return region
}

type navRegionTransitionKey struct {
	from, to               uint32
	kind, flags, traversal string
	gateKind, gateID       string
	axis                   uint8
	line                   int
	height                 float32
}

type navRegionTransitionSegment struct {
	start, end                int
	headroom, clearance, cost float32
	traversal                 *NavTraversalDef
	gate                      *NavTransitionGateDef
}

func buildNavRegionTransitions(graph NavGraphTileDef, spans map[uint32]NavSpanDef, spanRegions map[uint32]uint32, voxelResolution float32) ([]NavRegionTransitionDef, error) {
	groups := make(map[navRegionTransitionKey][]navRegionTransitionSegment)
	for _, edge := range graph.SpanTransitions {
		if edge.To.Tile != graph.Coord {
			continue
		}
		fromRegion, fromOK := spanRegions[edge.From]
		toRegion, toOK := spanRegions[edge.To.Span]
		if !fromOK || !toOK || fromRegion == toRegion {
			continue
		}
		from, to := spans[edge.From], spans[edge.To.Span]
		dx, dz := to.X-from.X, to.Z-from.Z
		key := navRegionTransitionKey{from: fromRegion, to: toRegion, kind: edge.Kind, flags: navRegionFlagsKey(edge.RequiresFlags), height: to.SupportHeight}
		if edge.Traversal != nil {
			key.traversal = edge.Traversal.StableLinkID()
		}
		if edge.Gate != nil {
			key.gateKind, key.gateID = edge.Gate.Kind, edge.Gate.ID
		}
		segment := navRegionTransitionSegment{
			headroom: edge.MinHeadroom, clearance: edge.MinClearance, cost: edge.Cost,
			traversal: cloneNavTraversal(edge.Traversal), gate: cloneNavTransitionGate(edge.Gate),
		}
		switch {
		case (dx == -1 || dx == 1) && dz == 0:
			key.line = max(from.X, to.X)
			segment.start, segment.end = from.Z, from.Z+1
		case (dz == -1 || dz == 1) && dx == 0:
			key.axis = 1
			key.line = max(from.Z, to.Z)
			segment.start, segment.end = from.X, from.X+1
		default:
			return nil, fmt.Errorf("navigation span transition %d -> %d is not four-neighbor", edge.From, edge.To.Span)
		}
		groups[key] = append(groups[key], segment)
	}

	keys := make([]navRegionTransitionKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.from != b.from {
			return a.from < b.from
		}
		if a.to != b.to {
			return a.to < b.to
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		if a.flags != b.flags {
			return a.flags < b.flags
		}
		if a.traversal != b.traversal {
			return a.traversal < b.traversal
		}
		if a.gateKind != b.gateKind {
			return a.gateKind < b.gateKind
		}
		if a.gateID != b.gateID {
			return a.gateID < b.gateID
		}
		if a.axis != b.axis {
			return a.axis < b.axis
		}
		if a.line != b.line {
			return a.line < b.line
		}
		return math.Float32bits(a.height) < math.Float32bits(b.height)
	})

	var result []NavRegionTransitionDef
	for _, key := range keys {
		segments := groups[key]
		sort.Slice(segments, func(i, j int) bool { return segments[i].start < segments[j].start })
		for i := 0; i < len(segments); {
			run := segments[i]
			i++
			for i < len(segments) && segments[i].start == run.end {
				run.end = segments[i].end
				run.headroom = min(run.headroom, segments[i].headroom)
				run.clearance = min(run.clearance, segments[i].clearance)
				run.cost = min(run.cost, segments[i].cost)
				i++
			}
			transition := NavRegionTransitionDef{
				ID: uint32(len(result)), FromRegion: key.from, ToTile: graph.Coord, ToRegion: key.to,
				Kind: key.kind, Width: float32(run.end-run.start) * voxelResolution,
				MinHeadroom: run.headroom, MinClearance: run.clearance, Cost: run.cost,
				RequiresFlags: navRegionFlags(key.flags), Traversal: cloneNavTraversal(run.traversal), Gate: cloneNavTransitionGate(run.gate),
			}
			if key.axis == 0 {
				transition.CrossingStart = Vec3{float32(key.line) * voxelResolution, key.height, float32(run.start) * voxelResolution}
				transition.CrossingEnd = Vec3{float32(key.line) * voxelResolution, key.height, float32(run.end) * voxelResolution}
			} else {
				transition.CrossingStart = Vec3{float32(run.start) * voxelResolution, key.height, float32(key.line) * voxelResolution}
				transition.CrossingEnd = Vec3{float32(run.end) * voxelResolution, key.height, float32(key.line) * voxelResolution}
			}
			result = append(result, transition)
		}
	}
	return result, nil
}
