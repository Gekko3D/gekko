package content

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	DefaultNavCarrierSpeed    float32 = 2
	DefaultNavCarrierUseRange float32 = 2.2

	NavCarrierRejectedStops  = "stops_unsupported"
	NavCarrierRejectedSource = "source_uncontrolled"
	NavCarrierSkippedRegion  = "same_region"
)

type NavCarrierDiagnostic struct {
	CarrierID string
	Code      string
}

type navCarrierMount struct {
	Point NavPointResult
	Board Vec3
}

// BuildNavCarriers derives discrete two-stop carriers from the current
// moving-brush contract. The manifest/runtime representation supports more
// stops; path movers can populate it when their stop authoring is available.
func BuildNavCarriers(brushes []LevelMovingBrushDef, triggers []LevelUseTriggerDef) []NavCarrierDef {
	carriers := make([]NavCarrierDef, 0)
	for _, brush := range brushes {
		if strings.TrimSpace(brush.NavigationRole) != NavigationRoleCarrier ||
			!strings.EqualFold(strings.TrimSpace(brush.MotionKind), "linear") {
			continue
		}
		offset := navCarrierOpenOffset(brush)
		if navVec3Distance(Vec3{}, offset) <= 1e-4 {
			continue
		}
		carrier := NavCarrierDef{
			ID: brush.ID, Group: brush.TargetName,
			BoundsHalfExtents: brush.BoundsHalfExtents,
			Speed:             brush.Speed,
			Wait:              brush.Wait,
			Stops: []NavCarrierStopDef{
				{ID: "closed", BoundsCenter: brush.BoundsCenter},
				{ID: "open", BoundsCenter: Vec3{
					brush.BoundsCenter[0] + offset[0],
					brush.BoundsCenter[1] + offset[1],
					brush.BoundsCenter[2] + offset[2],
				}},
			},
		}
		for _, trigger := range triggers {
			if carrier.Group == "" || trigger.Target != carrier.Group {
				continue
			}
			controller := NavCarrierControllerDef{
				ID: trigger.ID, BoundsCenter: trigger.BoundsCenter,
				BoundsHalfExtents: trigger.BoundsHalfExtents,
			}
			best := 0
			bestBoard := Vec3{
				carrier.Stops[0].BoundsCenter[0],
				carrier.Stops[0].BoundsCenter[1] + carrier.BoundsHalfExtents[1],
				carrier.Stops[0].BoundsCenter[2],
			}
			bestDistance := navCarrierPointAABBDistance(bestBoard, trigger.BoundsCenter, trigger.BoundsHalfExtents)
			for stop := 1; stop < len(carrier.Stops); stop++ {
				board := Vec3{
					carrier.Stops[stop].BoundsCenter[0],
					carrier.Stops[stop].BoundsCenter[1] + carrier.BoundsHalfExtents[1],
					carrier.Stops[stop].BoundsCenter[2],
				}
				distance := navCarrierPointAABBDistance(board, trigger.BoundsCenter, trigger.BoundsHalfExtents)
				if distance < bestDistance {
					best, bestDistance = stop, distance
				}
			}
			carrier.Stops[best].Controllers = append(carrier.Stops[best].Controllers, controller)
		}
		for stop := range carrier.Stops {
			sort.Slice(carrier.Stops[stop].Controllers, func(i, j int) bool {
				return carrier.Stops[stop].Controllers[i].ID < carrier.Stops[stop].Controllers[j].ID
			})
		}
		sort.Slice(carrier.Stops, func(i, j int) bool { return carrier.Stops[i].ID < carrier.Stops[j].ID })
		carriers = append(carriers, carrier)
	}
	sort.Slice(carriers, func(i, j int) bool { return carriers[i].ID < carriers[j].ID })
	return carriers
}

func ConnectNavGraphCarriers(sources []NavSourceTileDef, graphs []NavGraphTileDef, carriers []NavCarrierDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32) ([]NavGraphTileDef, []NavCarrierDiagnostic, error) {
	return connectNavGraphCarriers(sources, graphs, carriers, profile, chunkSize, voxelResolution, true)
}

func connectNavGraphCarriers(sources []NavSourceTileDef, graphs []NavGraphTileDef, carriers []NavCarrierDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32, strict bool) ([]NavGraphTileDef, []NavCarrierDiagnostic, error) {
	graphs = append([]NavGraphTileDef(nil), graphs...)
	for i := range graphs {
		graphs[i].SpanTransitions = removeNavCarrierSpanTransitions(graphs[i].SpanTransitions)
		graphs[i].Transitions = removeNavCarrierRegionTransitions(graphs[i].Transitions)
	}
	if len(carriers) == 0 {
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
	carriers = append([]NavCarrierDef(nil), carriers...)
	sort.Slice(carriers, func(i, j int) bool { return carriers[i].ID < carriers[j].ID })
	var diagnostics []NavCarrierDiagnostic
	for _, carrier := range carriers {
		mounts := make(map[string][]navCarrierMount, len(carrier.Stops))
		for _, stop := range carrier.Stops {
			mounts[stop.ID] = resolveNavCarrierMounts(query, carrier, stop, profile, voxelResolution)
		}
		if len(carrier.Stops) < 2 {
			diagnostics = append(diagnostics, NavCarrierDiagnostic{CarrierID: carrier.ID, Code: NavCarrierRejectedStops})
			continue
		}
		linked := false
		for _, fromStop := range carrier.Stops {
			fromMounts := mounts[fromStop.ID]
			if len(fromMounts) == 0 {
				continue
			}
			for _, toStop := range carrier.Stops {
				if toStop.ID == fromStop.ID {
					continue
				}
				for _, from := range fromMounts {
					if dropBoard, drop := navCarrierDropBoard(carrier, fromStop, toStop, from, profile, voxelResolution); drop {
						for _, to := range mounts[toStop.ID] {
							fromNode := navRouteNode{Tile: from.Point.Ref.Tile, Region: from.Point.Region}
							toNode := navRouteNode{Tile: to.Point.Ref.Tile, Region: to.Point.Region}
							if fromNode == toNode {
								continue
							}
							appendNavCarrierDirection(&graphs[graphIndex[from.Point.Ref.Tile]], carrier, fromStop, toStop, from, to, "", "", NavCarrierBoardDrop, dropBoard, query)
							linked = true
						}
						continue
					}
					controllerID := navCarrierControllerAtPoint(fromStop, from.Board, profile.Height)
					callControllerID := navCarrierControllerAtPoint(fromStop, from.Point.Point, profile.Height)
					autoServed := fromStop.ID == "closed" && carrier.Wait > 0
					if carrier.Group != "" && (controllerID == "" || callControllerID == "" && !autoServed) {
						diagnostics = append(diagnostics, NavCarrierDiagnostic{CarrierID: carrier.ID, Code: NavCarrierRejectedSource})
						continue
					}
					for _, to := range mounts[toStop.ID] {
						fromNode := navRouteNode{Tile: from.Point.Ref.Tile, Region: from.Point.Region}
						toNode := navRouteNode{Tile: to.Point.Ref.Tile, Region: to.Point.Region}
						if fromNode == toNode {
							diagnostics = append(diagnostics, NavCarrierDiagnostic{CarrierID: carrier.ID, Code: NavCarrierSkippedRegion})
							continue
						}
						appendNavCarrierDirection(&graphs[graphIndex[from.Point.Ref.Tile]], carrier, fromStop, toStop, from, to, callControllerID, controllerID, "", from.Board, query)
						linked = true
					}
				}
			}
		}
		if !linked && strict {
			return nil, diagnostics, fmt.Errorf("navigation carrier %q has no executable station links", carrier.ID)
		}
	}
	for i := range graphs {
		for id := range graphs[i].Transitions {
			graphs[i].Transitions[id].ID = uint32(id)
		}
		if validation := ValidateNavGraphTile(&graphs[i]); validation.HasErrors() {
			return nil, diagnostics, fmt.Errorf("invalid carrier-linked navigation graph tile %s: %s", TerrainChunkKey(graphs[i].Coord), validation.Error())
		}
	}
	return graphs, diagnostics, nil
}

func resolveNavCarrierMounts(query *NavGraphQuery, carrier NavCarrierDef, stop NavCarrierStopDef, profile NavAgentProfileDef, voxelResolution float32) []navCarrierMount {
	if query == nil || query.query == nil {
		return nil
	}
	top := stop.BoundsCenter[1] + carrier.BoundsHalfExtents[1]
	maxGap := max(voxelResolution*1.5, profile.Radius*0.5)
	maxVertical := max(voxelResolution*1.5, profile.StepHeight)
	type mountChoice struct {
		mount    navCarrierMount
		distance float32
	}
	byRegion := make(map[navRouteNode]mountChoice)
	for coord, graph := range query.query.graphs {
		for _, spanID := range graph.SpanIDs {
			span := query.query.spans[coord][spanID]
			if absFloat32(span.SupportHeight-top) > maxVertical {
				continue
			}
			minX := float32(coord.X*query.query.chunkSize+span.X) * query.query.voxelResolution
			minZ := float32(coord.Z*query.query.chunkSize+span.Z) * query.query.voxelResolution
			maxX, maxZ := minX+query.query.voxelResolution, minZ+query.query.voxelResolution
			centerX, centerZ := (minX+maxX)*0.5, (minZ+maxZ)*0.5
			if navCarrierInsideFootprint(centerX, centerZ, stop.BoundsCenter, carrier.BoundsHalfExtents) {
				continue
			}
			gap := float32(math.Hypot(
				float64(navCarrierIntervalGap(minX, maxX, stop.BoundsCenter[0]-carrier.BoundsHalfExtents[0], stop.BoundsCenter[0]+carrier.BoundsHalfExtents[0])),
				float64(navCarrierIntervalGap(minZ, maxZ, stop.BoundsCenter[2]-carrier.BoundsHalfExtents[2], stop.BoundsCenter[2]+carrier.BoundsHalfExtents[2])),
			))
			if gap > maxGap {
				continue
			}
			point := Vec3{
				navCarrierClamp(stop.BoundsCenter[0], minX, maxX),
				span.SupportHeight,
				navCarrierClamp(stop.BoundsCenter[2], minZ, maxZ),
			}
			ref := NavSpanRef{Tile: coord, Span: spanID}
			node := navRouteNode{Tile: coord, Region: query.query.spanRegions[coord][spanID]}
			choice := mountChoice{
				mount: navCarrierMount{
					Point: NavPointResult{Found: true, Ref: ref, Region: node.Region, Point: point},
					Board: Vec3{stop.BoundsCenter[0], top, stop.BoundsCenter[2]},
				},
				distance: gap + absFloat32(span.SupportHeight-top),
			}
			if previous, found := byRegion[node]; !found || choice.distance < previous.distance ||
				choice.distance == previous.distance && navSpanRefLess(choice.mount.Point.Ref, previous.mount.Point.Ref) {
				byRegion[node] = choice
			}
		}
	}
	result := make([]navCarrierMount, 0, len(byRegion))
	for _, choice := range byRegion {
		result = append(result, choice.mount)
	}
	sort.Slice(result, func(i, j int) bool { return navSpanRefLess(result[i].Point.Ref, result[j].Point.Ref) })
	return result
}

func appendNavCarrierDirection(graph *NavGraphTileDef, carrier NavCarrierDef, fromStop, toStop NavCarrierStopDef, from, to navCarrierMount, callControllerID, controllerID, boardMode string, board Vec3, query *NavGraphQuery) {
	linkID := navTraversalLinkID(NavTransitionCarrier, carrier.ID, from.Point.Ref, to.Point.Ref)
	duration := navVec3Distance(fromStop.BoundsCenter, toStop.BoundsCenter) / navCarrierSpeed(carrier.Speed)
	carrierTraversal := &NavCarrierTraversalDef{
		CarrierID: carrier.ID, FromStop: fromStop.ID, ToStop: toStop.ID,
		Board: board, BoardMode: boardMode, CallControllerID: callControllerID, ControllerID: controllerID,
	}
	fromSpan := query.query.spans[from.Point.Ref.Tile][from.Point.Ref.Span]
	toSpan := query.query.spans[to.Point.Ref.Tile][to.Point.Ref.Span]
	width := 2 * min(carrier.BoundsHalfExtents[0], carrier.BoundsHalfExtents[2])
	headroom := min(fromSpan.Headroom, toSpan.Headroom)
	clearance := min(fromSpan.ClearanceRadius, toSpan.ClearanceRadius)
	cost := navVec3Distance(from.Point.Point, board) +
		navVec3Distance(fromStop.BoundsCenter, toStop.BoundsCenter)*max(float32(1), DefaultNavCarrierSpeed/navCarrierSpeed(carrier.Speed)) +
		navVec3Distance(Vec3{toStop.BoundsCenter[0], toStop.BoundsCenter[1] + carrier.BoundsHalfExtents[1], toStop.BoundsCenter[2]}, to.Point.Point)
	if boardMode == NavCarrierBoardDrop {
		duration = 0
		cost = navVec3Distance(from.Point.Point, board) + navVec3Distance(board, to.Point.Point) + (from.Point.Point[1]-board[1])*0.5
	}
	spanTraversal := &NavTraversalDef{
		ID: carrier.ID, LinkID: linkID, OwnerID: carrier.ID,
		Start: from.Point.Point, End: to.Point.Point, Duration: duration,
		Carrier: carrierTraversal,
	}
	graph.SpanTransitions = append(graph.SpanTransitions, NavSpanTransitionDef{
		From: from.Point.Ref.Span, To: to.Point.Ref, Kind: NavTransitionCarrier,
		StepDelta: to.Point.Point[1] - from.Point.Point[1], Width: width,
		MinHeadroom: headroom, MinClearance: clearance, Cost: cost, Traversal: spanTraversal,
	})
	regionCarrier := *carrierTraversal
	graph.Transitions = append(graph.Transitions, NavRegionTransitionDef{
		ID: uint32(len(graph.Transitions)), FromRegion: from.Point.Region,
		ToTile: to.Point.Ref.Tile, ToRegion: to.Point.Region, Kind: NavTransitionCarrier,
		CrossingStart: from.Point.Point, CrossingEnd: to.Point.Point, Width: width,
		MinHeadroom: headroom, MinClearance: clearance, Cost: cost,
		Traversal: &NavTraversalDef{
			ID: carrier.ID, LinkID: linkID, OwnerID: carrier.ID,
			Start: from.Point.Point, End: to.Point.Point, Duration: duration,
			Carrier: &regionCarrier,
		},
	})
}

func navCarrierDropBoard(carrier NavCarrierDef, fromStop, toStop NavCarrierStopDef, from navCarrierMount, profile NavAgentProfileDef, voxelResolution float32) (Vec3, bool) {
	if carrier.Wait <= 0 || toStop.ID != "closed" {
		return Vec3{}, false
	}
	fromTop := fromStop.BoundsCenter[1] + carrier.BoundsHalfExtents[1]
	toTop := toStop.BoundsCenter[1] + carrier.BoundsHalfExtents[1]
	if fromTop-toTop <= profile.StepHeight ||
		absFloat32(fromStop.BoundsCenter[0]-toStop.BoundsCenter[0]) > voxelResolution ||
		absFloat32(fromStop.BoundsCenter[2]-toStop.BoundsCenter[2]) > voxelResolution {
		return Vec3{}, false
	}
	insetX := max(carrier.BoundsHalfExtents[0]-profile.Radius-voxelResolution, 0)
	insetZ := max(carrier.BoundsHalfExtents[2]-profile.Radius-voxelResolution, 0)
	board := Vec3{
		navCarrierClamp(from.Point.Point[0], toStop.BoundsCenter[0]-insetX, toStop.BoundsCenter[0]+insetX),
		toTop,
		navCarrierClamp(from.Point.Point[2], toStop.BoundsCenter[2]-insetZ, toStop.BoundsCenter[2]+insetZ),
	}
	if !NavTraversalSupportedByProfile(profile, NavTransitionDrop, &NavTraversalDef{Start: from.Point.Point, End: board}) {
		return Vec3{}, false
	}
	return board, true
}

func navCarrierOpenOffset(brush LevelMovingBrushDef) Vec3 {
	direction := brush.MoveDirection
	length := navVec3Distance(Vec3{}, direction)
	if length <= 1e-6 {
		return Vec3{}
	}
	for axis := range direction {
		direction[axis] /= length
	}
	distance := brush.MoveDistance
	if distance <= 0 {
		extent := absFloat32(direction[0])*brush.BoundsHalfExtents[0] +
			absFloat32(direction[1])*brush.BoundsHalfExtents[1] +
			absFloat32(direction[2])*brush.BoundsHalfExtents[2]
		distance = max(2*extent-brush.Lip, 0)
	}
	return Vec3{direction[0] * distance, direction[1] * distance, direction[2] * distance}
}

func navCarrierControllerAtPoint(stop NavCarrierStopDef, point Vec3, height float32) string {
	interaction := point
	interaction[1] += min(height*0.8, 1.6)
	bestID := ""
	bestDistance := float32(math.Inf(1))
	for _, controller := range stop.Controllers {
		distance := navCarrierPointAABBDistance(interaction, controller.BoundsCenter, controller.BoundsHalfExtents)
		if distance <= DefaultNavCarrierUseRange+1e-4 &&
			(distance < bestDistance || distance == bestDistance && controller.ID < bestID) {
			bestID, bestDistance = controller.ID, distance
		}
	}
	return bestID
}

func navCarrierPointAABBDistance(point, center, half Vec3) float32 {
	delta := Vec3{}
	for axis := range delta {
		delta[axis] = max(absFloat32(point[axis]-center[axis])-absFloat32(half[axis]), 0)
	}
	return navVec3Distance(Vec3{}, delta)
}

func navCarrierInsideFootprint(x, z float32, center, half Vec3) bool {
	const epsilon = float32(1e-4)
	return x > center[0]-half[0]+epsilon && x < center[0]+half[0]-epsilon &&
		z > center[2]-half[2]+epsilon && z < center[2]+half[2]-epsilon
}

func navCarrierIntervalGap(aMin, aMax, bMin, bMax float32) float32 {
	if aMax < bMin {
		return bMin - aMax
	}
	if bMax < aMin {
		return aMin - bMax
	}
	return 0
}

func navCarrierClamp(value, low, high float32) float32 {
	return min(max(value, low), high)
}

func navCarrierSpeed(speed float32) float32 {
	if speed <= 0 {
		return DefaultNavCarrierSpeed
	}
	return speed
}

// NavCarrierBlockers removes the swept carrier footprint from ordinary static
// routing while leaving station spans outside the footprint available.
func NavCarrierBlockers(carriers []NavCarrierDef, profile NavAgentProfileDef) []NavBlockerDef {
	result := make([]NavBlockerDef, 0, len(carriers))
	for _, carrier := range carriers {
		if len(carrier.Stops) < 2 {
			continue
		}
		minimum := Vec3{float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1))}
		maximum := Vec3{float32(math.Inf(-1)), float32(math.Inf(-1)), float32(math.Inf(-1))}
		for _, stop := range carrier.Stops {
			minimum[0] = min(minimum[0], stop.BoundsCenter[0]-carrier.BoundsHalfExtents[0])
			minimum[1] = min(minimum[1], stop.BoundsCenter[1]+carrier.BoundsHalfExtents[1]-0.05)
			minimum[2] = min(minimum[2], stop.BoundsCenter[2]-carrier.BoundsHalfExtents[2])
			maximum[0] = max(maximum[0], stop.BoundsCenter[0]+carrier.BoundsHalfExtents[0])
			maximum[1] = max(maximum[1], stop.BoundsCenter[1]+carrier.BoundsHalfExtents[1]+0.05)
			maximum[2] = max(maximum[2], stop.BoundsCenter[2]+carrier.BoundsHalfExtents[2])
		}
		minimum[0] += profile.Radius
		minimum[2] += profile.Radius
		maximum[0] -= profile.Radius
		maximum[2] -= profile.Radius
		if minimum[0] > maximum[0] || minimum[2] > maximum[2] {
			continue
		}
		result = append(result, NavBlockerDef{ID: "__carrier:" + carrier.ID, Min: minimum, Max: maximum})
	}
	return result
}

func removeNavCarrierSpanTransitions(transitions []NavSpanTransitionDef) []NavSpanTransitionDef {
	result := make([]NavSpanTransitionDef, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Kind != NavTransitionCarrier {
			result = append(result, transition)
		}
	}
	return result
}

func removeNavCarrierRegionTransitions(transitions []NavRegionTransitionDef) []NavRegionTransitionDef {
	result := make([]NavRegionTransitionDef, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Kind != NavTransitionCarrier {
			transition.ID = uint32(len(result))
			result = append(result, transition)
		}
	}
	return result
}

func expandNavGraphCoordsForCarriers(coords map[TerrainChunkCoordDef]struct{}, carriers []NavCarrierDef, chunkSize int, voxelResolution float32) map[TerrainChunkCoordDef]struct{} {
	result := make(map[TerrainChunkCoordDef]struct{}, len(coords))
	for coord := range coords {
		result[coord] = struct{}{}
	}
	for _, carrier := range carriers {
		var stopCoords []TerrainChunkCoordDef
		for _, stop := range carrier.Stops {
			top := stop.BoundsCenter
			top[1] += carrier.BoundsHalfExtents[1]
			stopCoords = append(stopCoords, navLadderMountTileCoords(top, chunkSize, voxelResolution)...)
		}
		touches := false
		for _, coord := range stopCoords {
			if _, ok := result[coord]; ok {
				touches = true
				break
			}
		}
		if touches {
			for _, coord := range stopCoords {
				result[coord] = struct{}{}
			}
		}
	}
	return result
}
