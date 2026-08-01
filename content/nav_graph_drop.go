package content

import (
	"fmt"
	"math"
	"sort"
)

const AutoNavDropOwnerID = "__auto_drop"

type navDropSpan struct {
	Ref    NavSpanRef
	Span   NavSpanDef
	Point  Vec3
	Region uint32
}

type navDropRegionPair struct {
	From navRouteNode
	To   navRouteNode
}

type navDropCandidate struct {
	From      navDropSpan
	To        navDropSpan
	Traversal NavTraversalDef
	Cost      float32
}

// ConnectNavGraphDrops discovers bounded one-way ledge drops from exposed
// graph boundaries. Work stays in full/delta graph generation, never per bot.
func ConnectNavGraphDrops(sources []NavSourceTileDef, graphs []NavGraphTileDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32) ([]NavGraphTileDef, error) {
	graphs = append([]NavGraphTileDef(nil), graphs...)
	for i := range graphs {
		graphs[i].SpanTransitions = removeAutoNavDropSpanTransitions(graphs[i].SpanTransitions)
		graphs[i].Transitions = removeAutoNavDropRegionTransitions(graphs[i].Transitions)
	}
	if profile.MaxDropHeight <= profile.StepHeight || voxelResolution <= 0 || chunkSize <= 0 {
		return graphs, nil
	}

	graphIndex := make(map[TerrainChunkCoordDef]int, len(graphs))
	regions := make(map[TerrainChunkCoordDef]map[uint32]uint32, len(graphs))
	accepted := make(map[NavSpanRef]struct{})
	for i := range graphs {
		graphIndex[graphs[i].Coord] = i
		regions[graphs[i].Coord] = navGraphSpanRegions(graphs[i])
		for _, spanID := range graphs[i].SpanIDs {
			accepted[NavSpanRef{Tile: graphs[i].Coord, Span: spanID}] = struct{}{}
		}
	}

	type column struct{ X, Z int }
	columns := make(map[column][]navDropSpan)
	spans := make(map[NavSpanRef]navDropSpan, len(accepted))
	for _, source := range sources {
		for _, span := range source.Spans {
			ref := NavSpanRef{Tile: source.Coord, Span: span.ID}
			if _, ok := accepted[ref]; !ok {
				continue
			}
			region, ok := regions[ref.Tile][ref.Span]
			if !ok {
				continue
			}
			candidate := navDropSpan{
				Ref: ref, Span: span, Region: region,
				Point: Vec3{
					(float32(ref.Tile.X*chunkSize+span.X) + 0.5) * voxelResolution,
					span.SupportHeight,
					(float32(ref.Tile.Z*chunkSize+span.Z) + 0.5) * voxelResolution,
				},
			}
			spans[ref] = candidate
			key := column{ref.Tile.X*chunkSize + span.X, ref.Tile.Z*chunkSize + span.Z}
			columns[key] = append(columns[key], candidate)
		}
	}

	type direction struct{ X, Z int }
	directions := [...]direction{{X: -1}, {Z: -1}, {Z: 1}, {X: 1}}
	landingSteps := max(1, int(math.Ceil(float64((profile.Radius+voxelResolution*0.5)/voxelResolution))))
	best := make(map[navDropRegionPair]navDropCandidate)
	for _, from := range spans {
		fromColumn := column{from.Ref.Tile.X*chunkSize + from.Span.X, from.Ref.Tile.Z*chunkSize + from.Span.Z}
		for _, direction := range directions {
			if navDropHasUpperSupport(columns[column{fromColumn.X + direction.X, fromColumn.Z + direction.Z}], from.Span.SupportHeight, profile.StepHeight) {
				continue
			}
			landingColumn := columns[column{fromColumn.X + direction.X*landingSteps, fromColumn.Z + direction.Z*landingSteps}]
			to, found := navDropHighestLanding(landingColumn, from, profile)
			if !found {
				continue
			}
			fromNode := navRouteNode{Tile: from.Ref.Tile, Region: from.Region}
			toNode := navRouteNode{Tile: to.Ref.Tile, Region: to.Region}
			if fromNode == toNode {
				continue
			}
			traversal := NavTraversalDef{Start: from.Point, End: to.Point}
			if !NavTraversalSupportedByProfile(profile, NavTransitionDrop, &traversal) {
				continue
			}
			cost := navVec3Distance(from.Point, to.Point) + (from.Point[1]-to.Point[1])*0.5
			key := navDropRegionPair{From: fromNode, To: toNode}
			candidate := navDropCandidate{From: from, To: to, Traversal: traversal, Cost: cost}
			if previous, ok := best[key]; !ok || navDropCandidateLess(candidate, previous) {
				best[key] = candidate
			}
		}
	}

	occupancy := buildNavJumpOccupancy(sources, chunkSize)
	candidates := make([]navDropCandidate, 0, len(best))
	for _, candidate := range best {
		if navDropPathClear(occupancy, candidate.Traversal, profile, voxelResolution) {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return navDropCandidateLess(candidates[i], candidates[j]) })
	for _, candidate := range candidates {
		graph := &graphs[graphIndex[candidate.From.Ref.Tile]]
		linkID := navTraversalLinkID(NavTransitionDrop, AutoNavDropOwnerID, candidate.From.Ref, candidate.To.Ref)
		width := min(voxelResolution, 2*min(candidate.From.Span.ClearanceRadius, candidate.To.Span.ClearanceRadius))
		if width <= 0 {
			width = voxelResolution
		}
		headroom := min(candidate.From.Span.Headroom, candidate.To.Span.Headroom)
		clearance := min(candidate.From.Span.ClearanceRadius, candidate.To.Span.ClearanceRadius)
		traversal := candidate.Traversal
		traversal.LinkID, traversal.OwnerID = linkID, AutoNavDropOwnerID
		graph.SpanTransitions = append(graph.SpanTransitions, NavSpanTransitionDef{
			From: candidate.From.Ref.Span, To: candidate.To.Ref, Kind: NavTransitionDrop,
			StepDelta: candidate.To.Point[1] - candidate.From.Point[1], Width: width,
			MinHeadroom: headroom, MinClearance: clearance, Cost: candidate.Cost,
			Traversal: &traversal,
		})
		regionTraversal := traversal
		graph.Transitions = append(graph.Transitions, NavRegionTransitionDef{
			ID: uint32(len(graph.Transitions)), FromRegion: candidate.From.Region,
			ToTile: candidate.To.Ref.Tile, ToRegion: candidate.To.Region, Kind: NavTransitionDrop,
			CrossingStart: candidate.From.Point, CrossingEnd: candidate.To.Point, Width: width,
			MinHeadroom: headroom, MinClearance: clearance, Cost: candidate.Cost,
			Traversal: &regionTraversal,
		})
	}
	for i := range graphs {
		for id := range graphs[i].Transitions {
			graphs[i].Transitions[id].ID = uint32(id)
		}
		if validation := ValidateNavGraphTile(&graphs[i]); validation.HasErrors() {
			return nil, fmt.Errorf("invalid drop-linked navigation graph tile %s: %s", TerrainChunkKey(graphs[i].Coord), validation.Error())
		}
	}
	return graphs, nil
}

func navDropPathClear(occupancy navJumpOccupancy, traversal NavTraversalDef, profile NavAgentProfileDef, voxelResolution float32) bool {
	horizontal := float32(math.Hypot(float64(traversal.End[0]-traversal.Start[0]), float64(traversal.End[2]-traversal.Start[2])))
	horizontalSamples := max(1, int(math.Ceil(float64(horizontal/(voxelResolution*0.5)))))
	for sample := 0; sample <= horizontalSamples; sample++ {
		fraction := float32(sample) / float32(horizontalSamples)
		base := Vec3{
			traversal.Start[0] + (traversal.End[0]-traversal.Start[0])*fraction,
			traversal.Start[1],
			traversal.Start[2] + (traversal.End[2]-traversal.Start[2])*fraction,
		}
		if !navJumpCapsuleClear(occupancy, base, profile.Radius, profile.Height, voxelResolution) {
			return false
		}
	}
	vertical := traversal.Start[1] - traversal.End[1]
	verticalSamples := max(1, int(math.Ceil(float64(vertical/(voxelResolution*0.5)))))
	for sample := 0; sample <= verticalSamples; sample++ {
		fraction := float32(sample) / float32(verticalSamples)
		base := Vec3{traversal.End[0], traversal.Start[1] - vertical*fraction, traversal.End[2]}
		if !navJumpCapsuleClear(occupancy, base, profile.Radius, profile.Height, voxelResolution) {
			return false
		}
	}
	return true
}

func navDropHasUpperSupport(spans []navDropSpan, height, stepHeight float32) bool {
	for _, span := range spans {
		if absFloat32(span.Span.SupportHeight-height) <= stepHeight+1e-4 {
			return true
		}
	}
	return false
}

func navDropHighestLanding(spans []navDropSpan, from navDropSpan, profile NavAgentProfileDef) (navDropSpan, bool) {
	var result navDropSpan
	found := false
	for _, candidate := range spans {
		drop := from.Span.SupportHeight - candidate.Span.SupportHeight
		traversal := NavTraversalDef{Start: from.Point, End: candidate.Point}
		if drop <= profile.StepHeight+1e-4 || !NavTraversalSupportedByProfile(profile, NavTransitionDrop, &traversal) {
			continue
		}
		if !found || candidate.Span.SupportHeight > result.Span.SupportHeight ||
			candidate.Span.SupportHeight == result.Span.SupportHeight && navSpanRefLess(candidate.Ref, result.Ref) {
			result, found = candidate, true
		}
	}
	return result, found
}

func navDropCandidateLess(a, b navDropCandidate) bool {
	if a.Cost != b.Cost {
		return a.Cost < b.Cost
	}
	if a.From.Ref != b.From.Ref {
		return navSpanRefLess(a.From.Ref, b.From.Ref)
	}
	return navSpanRefLess(a.To.Ref, b.To.Ref)
}

func removeAutoNavDropSpanTransitions(transitions []NavSpanTransitionDef) []NavSpanTransitionDef {
	result := make([]NavSpanTransitionDef, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Traversal == nil || transition.Traversal.Owner() != AutoNavDropOwnerID {
			result = append(result, transition)
		}
	}
	return result
}

func removeAutoNavDropRegionTransitions(transitions []NavRegionTransitionDef) []NavRegionTransitionDef {
	result := make([]NavRegionTransitionDef, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Traversal == nil || transition.Traversal.Owner() != AutoNavDropOwnerID {
			transition.ID = uint32(len(result))
			result = append(result, transition)
		}
	}
	return result
}
