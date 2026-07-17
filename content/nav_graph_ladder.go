package content

import (
	"fmt"
	"math"
	"sort"
)

const DefaultNavLadderClimbSpeed float32 = 3

const (
	NavLadderSkippedSameRegion = "same_region"
	NavLadderRejectedBottom    = "bottom_mount_unsupported"
	NavLadderRejectedTop       = "top_mount_unsupported"
	NavLadderRejectedAmbiguous = "mount_ambiguous"
)

type NavLadderDiagnostic struct {
	LadderID string
	Code     string
}

// ConnectNavGraphLadders resolves authored ladder mounts to supported spans
// and appends deterministic directed transitions for capable profiles.
func ConnectNavGraphLadders(sources []NavSourceTileDef, graphs []NavGraphTileDef, ladders []LevelLadderVolumeDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32) ([]NavGraphTileDef, []NavLadderDiagnostic, error) {
	return connectNavGraphLadders(sources, graphs, ladders, profile, chunkSize, voxelResolution, true)
}

func connectNavGraphLadders(sources []NavSourceTileDef, graphs []NavGraphTileDef, ladders []LevelLadderVolumeDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32, strict bool) ([]NavGraphTileDef, []NavLadderDiagnostic, error) {
	graphs = append([]NavGraphTileDef(nil), graphs...)
	for i := range graphs {
		graphs[i].SpanTransitions = removeNavLadderSpanTransitions(graphs[i].SpanTransitions)
		graphs[i].Transitions = removeNavLadderRegionTransitions(graphs[i].Transitions)
	}
	if !navProfileHasCapability(profile, NavCapabilityClimbLadder) || len(ladders) == 0 {
		return graphs, nil, nil
	}
	query, err := NewNavGraphQuery(sources, graphs, chunkSize, voxelResolution)
	if err != nil {
		return nil, nil, err
	}
	graphIndex := make(map[TerrainChunkCoordDef]int, len(graphs))
	for i := range graphs {
		graphIndex[graphs[i].Coord] = i
	}
	ladders = append([]LevelLadderVolumeDef(nil), ladders...)
	sort.Slice(ladders, func(i, j int) bool { return ladders[i].ID < ladders[j].ID })
	var diagnostics []NavLadderDiagnostic
	for _, ladder := range ladders {
		bottomMount, topMount := navLadderMountPoints(ladder)
		bottom, bottomCode := resolveNavLadderMount(query, bottomMount, profile, voxelResolution, NavLadderRejectedBottom)
		top, topCode := resolveNavLadderMount(query, topMount, profile, voxelResolution, NavLadderRejectedTop)
		if bottomCode != "" || topCode != "" {
			code := bottomCode
			if code == "" {
				code = topCode
			}
			diagnostics = append(diagnostics, NavLadderDiagnostic{LadderID: ladder.ID, Code: code})
			if strict {
				return nil, diagnostics, fmt.Errorf("navigation ladder %q: %s", ladder.ID, code)
			}
			continue
		}
		if top.Point[1] <= bottom.Point[1] || bottom.Ref == top.Ref {
			diagnostics = append(diagnostics, NavLadderDiagnostic{LadderID: ladder.ID, Code: NavLadderRejectedTop})
			if strict {
				return nil, diagnostics, fmt.Errorf("navigation ladder %q top mount does not resolve above bottom mount", ladder.ID)
			}
			continue
		}
		bottomRegion := navRouteNode{Tile: bottom.Ref.Tile, Region: bottom.Region}
		topRegion := navRouteNode{Tile: top.Ref.Tile, Region: top.Region}
		if bottomRegion == topRegion {
			diagnostics = append(diagnostics, NavLadderDiagnostic{LadderID: ladder.ID, Code: NavLadderSkippedSameRegion})
			continue
		}
		width := 2 * max(ladder.BoundsHalfExtents[0], ladder.BoundsHalfExtents[2])
		bottomSpan := query.query.spans[bottom.Ref.Tile][bottom.Ref.Span]
		topSpan := query.query.spans[top.Ref.Tile][top.Ref.Span]
		headroom := min(bottomSpan.Headroom, topSpan.Headroom)
		clearance := min(bottomSpan.ClearanceRadius, topSpan.ClearanceRadius)
		cost := navLadderTraversalCost(bottom.Point, top.Point, ladder.ClimbSpeed)
		appendNavLadderDirection(&graphs[graphIndex[bottom.Ref.Tile]], ladder.ID, bottom, top, width, headroom, clearance, cost)
		appendNavLadderDirection(&graphs[graphIndex[top.Ref.Tile]], ladder.ID, top, bottom, width, headroom, clearance, cost)
	}
	for i := range graphs {
		for id := range graphs[i].Transitions {
			graphs[i].Transitions[id].ID = uint32(id)
		}
		if validation := ValidateNavGraphTile(&graphs[i]); validation.HasErrors() {
			return nil, diagnostics, fmt.Errorf("invalid ladder-linked navigation graph tile %s: %s", TerrainChunkKey(graphs[i].Coord), validation.Error())
		}
	}
	return graphs, diagnostics, nil
}

func appendNavLadderDirection(graph *NavGraphTileDef, ladderID string, from, to NavPointResult, width, headroom, clearance, cost float32) {
	spanTraversal := &NavTraversalDef{ID: ladderID, Start: from.Point, End: to.Point}
	graph.SpanTransitions = append(graph.SpanTransitions, NavSpanTransitionDef{
		From: from.Ref.Span, To: to.Ref, Kind: NavTransitionLadder,
		StepDelta: to.Point[1] - from.Point[1], Width: width,
		MinHeadroom: headroom, MinClearance: clearance, Cost: cost,
		RequiresFlags: []string{NavCapabilityClimbLadder}, Traversal: spanTraversal,
	})
	regionTraversal := &NavTraversalDef{ID: ladderID, Start: from.Point, End: to.Point}
	graph.Transitions = append(graph.Transitions, NavRegionTransitionDef{
		ID: uint32(len(graph.Transitions)), FromRegion: from.Region,
		ToTile: to.Ref.Tile, ToRegion: to.Region, Kind: NavTransitionLadder,
		CrossingStart: from.Point, CrossingEnd: to.Point, Width: width,
		MinHeadroom: headroom, MinClearance: clearance, Cost: cost,
		RequiresFlags: []string{NavCapabilityClimbLadder}, Traversal: regionTraversal,
	})
}

func navLadderMountPoints(ladder LevelLadderVolumeDef) (Vec3, Vec3) {
	if ladder.MountBottom != nil && ladder.MountTop != nil {
		return *ladder.MountBottom, *ladder.MountTop
	}
	bottom, top := ladder.BoundsCenter, ladder.BoundsCenter
	bottom[1] -= ladder.BoundsHalfExtents[1]
	top[1] += ladder.BoundsHalfExtents[1]
	return bottom, top
}

func resolveNavLadderMount(query *NavGraphQuery, point Vec3, profile NavAgentProfileDef, voxelResolution float32, unsupportedCode string) (NavPointResult, string) {
	maxDistance := max(voxelResolution*1.5, profile.StepHeight+profile.Radius, profile.Height+profile.Radius)
	best := NavPointResult{Distance: float32(math.Inf(1))}
	ambiguous := false
	for coord, graph := range query.query.graphs {
		for _, spanID := range graph.SpanIDs {
			span := query.query.spans[coord][spanID]
			minX := float32(coord.X*query.query.chunkSize+span.X) * query.query.voxelResolution
			minZ := float32(coord.Z*query.query.chunkSize+span.Z) * query.query.voxelResolution
			projected := Vec3{
				navClampToSpanAxis(point[0], minX, query.query.voxelResolution),
				span.SupportHeight,
				navClampToSpanAxis(point[2], minZ, query.query.voxelResolution),
			}
			distance := navVec3Distance(point, projected)
			if distance > maxDistance {
				continue
			}
			candidate := NavPointResult{Found: true, Ref: NavSpanRef{Tile: coord, Span: spanID}, Region: query.query.spanRegions[coord][spanID], Point: projected, Distance: distance}
			if !best.Found || distance < best.Distance-1e-4 {
				best, ambiguous = candidate, false
				continue
			}
			if absFloat32(distance-best.Distance) <= 1e-4 {
				candidateNode := navRouteNode{Tile: candidate.Ref.Tile, Region: candidate.Region}
				bestNode := navRouteNode{Tile: best.Ref.Tile, Region: best.Region}
				if !navLadderLandingRegionsConnected(query, candidateNode, bestNode) {
					ambiguous = true
				}
				if navSpanRefLess(candidate.Ref, best.Ref) {
					best = candidate
				}
			}
		}
	}
	if !best.Found {
		return NavPointResult{}, unsupportedCode
	}
	if ambiguous {
		return NavPointResult{}, NavLadderRejectedAmbiguous
	}
	return best, ""
}

func navLadderLandingRegionsConnected(query *NavGraphQuery, a, b navRouteNode) bool {
	if a == b {
		return true
	}
	for _, pair := range [][2]navRouteNode{{a, b}, {b, a}} {
		graph, ok := query.query.graphs[pair[0].Tile]
		if !ok {
			continue
		}
		for _, transition := range graph.Transitions {
			if transition.Kind != NavTransitionLadder && transition.FromRegion == pair[0].Region && transition.ToTile == pair[1].Tile && transition.ToRegion == pair[1].Region {
				return true
			}
		}
	}
	return false
}

func navProfileHasCapability(profile NavAgentProfileDef, capability string) bool {
	index := sort.SearchStrings(profile.Capabilities, capability)
	return index < len(profile.Capabilities) && profile.Capabilities[index] == capability
}

func navLadderClimbSpeed(speed float32) float32 {
	if speed <= 0 {
		return DefaultNavLadderClimbSpeed
	}
	return speed
}

func navLadderTraversalCost(start, end Vec3, speed float32) float32 {
	return navVec3Distance(start, end) * max(float32(1), DefaultNavLadderClimbSpeed/navLadderClimbSpeed(speed))
}

func removeNavLadderSpanTransitions(transitions []NavSpanTransitionDef) []NavSpanTransitionDef {
	result := make([]NavSpanTransitionDef, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Kind != NavTransitionLadder {
			result = append(result, transition)
		}
	}
	return result
}

func removeNavLadderRegionTransitions(transitions []NavRegionTransitionDef) []NavRegionTransitionDef {
	result := make([]NavRegionTransitionDef, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Kind != NavTransitionLadder {
			transition.ID = uint32(len(result))
			result = append(result, transition)
		}
	}
	return result
}

func expandNavGraphCoordsForLadders(coords map[TerrainChunkCoordDef]struct{}, ladders []LevelLadderVolumeDef, chunkSize int, voxelResolution float32) map[TerrainChunkCoordDef]struct{} {
	result := make(map[TerrainChunkCoordDef]struct{}, len(coords))
	for coord := range coords {
		result[coord] = struct{}{}
	}
	for _, ladder := range ladders {
		bottom, top := navLadderMountPoints(ladder)
		bottomCoords := navLadderMountTileCoords(bottom, chunkSize, voxelResolution)
		topCoords := navLadderMountTileCoords(top, chunkSize, voxelResolution)
		touches := false
		for _, coord := range append(append([]TerrainChunkCoordDef(nil), bottomCoords...), topCoords...) {
			if _, ok := result[coord]; ok {
				touches = true
				break
			}
		}
		if !touches {
			continue
		}
		for _, coord := range append(bottomCoords, topCoords...) {
			result[coord] = struct{}{}
		}
	}
	return result
}

func navLadderMountTileCoords(point Vec3, chunkSize int, voxelResolution float32) []TerrainChunkCoordDef {
	cellX := int(math.Floor(float64(point[0] / voxelResolution)))
	cellY := int(math.Floor(float64(point[1] / voxelResolution)))
	cellZ := int(math.Floor(float64(point[2] / voxelResolution)))
	center := TerrainChunkCoordDef{X: floorDivNavSpan(cellX, chunkSize), Y: floorDivNavSpan(cellY, chunkSize), Z: floorDivNavSpan(cellZ, chunkSize)}
	result := make([]TerrainChunkCoordDef, 0, 27)
	for y := -1; y <= 1; y++ {
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				result = append(result, TerrainChunkCoordDef{X: center.X + x, Y: center.Y + y, Z: center.Z + z})
			}
		}
	}
	return result
}
