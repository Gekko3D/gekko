package content

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	NavDoorSkippedUnsupported = "unsupported"
	NavDoorSkippedSameRegion  = "same_region"
	NavDoorClosedCostPenalty  = float32(1)
)

type NavDoorDiagnostic struct {
	DoorID string
	Code   string
}

type navDoorSpan struct {
	Ref   NavSpanRef
	Span  NavSpanDef
	Point Vec3
}

type navDoorLaneKey struct {
	Tangent int
	Height  uint32
}

type navDoorLane struct {
	Negative navDoorSpan
	Positive navDoorSpan
}

// ConnectNavGraphDoors links vertical door portals, then composes horizontal
// hatch gates with movement transitions already present in the graph.
func ConnectNavGraphDoors(sources []NavSourceTileDef, graphs []NavGraphTileDef, doors []NavDoorDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32) ([]NavGraphTileDef, []NavDoorDiagnostic, error) {
	linked, diagnostics, err := connectNavGraphDoors(sources, graphs, doors, profile, chunkSize, voxelResolution, true)
	if err != nil {
		return nil, diagnostics, err
	}
	linked, gateDiagnostics, err := connectNavGraphDoorGates(sources, linked, doors, profile, chunkSize, voxelResolution, true)
	return linked, append(diagnostics, gateDiagnostics...), err
}

func connectNavGraphDoors(sources []NavSourceTileDef, graphs []NavGraphTileDef, doors []NavDoorDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32, strict bool) ([]NavGraphTileDef, []NavDoorDiagnostic, error) {
	graphs = append([]NavGraphTileDef(nil), graphs...)
	doors = append([]NavDoorDef(nil), doors...)
	sort.Slice(doors, func(i, j int) bool { return doors[i].ID < doors[j].ID })
	verticalDoors := make([]NavDoorDef, 0, len(doors))
	for _, door := range doors {
		if !navDoorHorizontal(door) {
			verticalDoors = append(verticalDoors, door)
		}
	}
	if len(verticalDoors) == 0 {
		return graphs, nil, nil
	}

	sourceIndex := make(map[TerrainChunkCoordDef]NavSourceTileDef, len(sources))
	spanIndex := make(map[NavSpanRef]navDoorSpan)
	accepted := make(map[NavSpanRef]struct{})
	for _, graph := range graphs {
		for _, spanID := range graph.SpanIDs {
			accepted[NavSpanRef{Tile: graph.Coord, Span: spanID}] = struct{}{}
		}
	}
	for _, source := range sources {
		sourceIndex[source.Coord] = source
		for _, span := range source.Spans {
			ref := NavSpanRef{Tile: source.Coord, Span: span.ID}
			spanIndex[ref] = navDoorSpan{Ref: ref, Span: span, Point: navDoorSpanCenter(ref.Tile, span, chunkSize, voxelResolution)}
		}
	}

	lanes := make(map[string][]navDoorLane, len(verticalDoors))
	for _, door := range verticalDoors {
		lanes[door.ID] = navDoorLanes(door, spanIndex, accepted, profile, chunkSize, voxelResolution)
	}
	for i := range graphs {
		kept := make([]NavSpanTransitionDef, 0, len(graphs[i].SpanTransitions))
		for _, edge := range graphs[i].SpanTransitions {
			if edge.Kind == NavTransitionLadder || edge.Kind == NavTransitionDrop || edge.Gate != nil {
				continue
			}
			from, fromOK := spanIndex[NavSpanRef{Tile: graphs[i].Coord, Span: edge.From}]
			to, toOK := spanIndex[edge.To]
			blocked := false
			if fromOK && toOK {
				for _, door := range verticalDoors {
					if navDoorCutsEdge(door, from, to, profile) {
						blocked = true
						break
					}
				}
			}
			if !blocked {
				kept = append(kept, edge)
			}
		}
		graphs[i].SpanTransitions = kept
		source, ok := sourceIndex[graphs[i].Coord]
		if !ok {
			return nil, nil, fmt.Errorf("navigation door graph tile %s has no source", TerrainChunkKey(graphs[i].Coord))
		}
		compressed, err := compressNavGraphRegions(source, graphs[i], voxelResolution, navDoorRegionPartition(graphs[i].Coord, verticalDoors, chunkSize, voxelResolution))
		if err != nil {
			return nil, nil, fmt.Errorf("compress navigation door graph tile %s: %w", TerrainChunkKey(graphs[i].Coord), err)
		}
		graphs[i] = compressed
	}

	navDoorAppendExternalRegionTransitions(graphs, spanIndex)
	graphIndex := make(map[TerrainChunkCoordDef]int, len(graphs))
	regions := make(map[TerrainChunkCoordDef]map[uint32]uint32, len(graphs))
	for i := range graphs {
		graphIndex[graphs[i].Coord] = i
		regions[graphs[i].Coord] = navGraphSpanRegions(graphs[i])
	}

	var diagnostics []NavDoorDiagnostic
	for _, door := range verticalDoors {
		linked := false
		for _, lane := range lanes[door.ID] {
			negativeGraph, negativeOK := graphIndex[lane.Negative.Ref.Tile]
			positiveGraph, positiveOK := graphIndex[lane.Positive.Ref.Tile]
			negativeRegion, negativeRegionOK := regions[lane.Negative.Ref.Tile][lane.Negative.Ref.Span]
			positiveRegion, positiveRegionOK := regions[lane.Positive.Ref.Tile][lane.Positive.Ref.Span]
			if !negativeOK || !positiveOK || !negativeRegionOK || !positiveRegionOK {
				continue
			}
			if lane.Negative.Ref.Tile == lane.Positive.Ref.Tile && negativeRegion == positiveRegion {
				continue
			}
			width := min(voxelResolution, 2*min(lane.Negative.Span.ClearanceRadius, lane.Positive.Span.ClearanceRadius))
			if width <= 0 {
				width = voxelResolution
			}
			headroom := min(lane.Negative.Span.Headroom, lane.Positive.Span.Headroom)
			clearance := min(lane.Negative.Span.ClearanceRadius, lane.Positive.Span.ClearanceRadius)
			cost := navVec3Distance(lane.Negative.Point, lane.Positive.Point) + NavDoorClosedCostPenalty
			appendNavDoorDirection(&graphs[negativeGraph], door.ID, lane.Negative, negativeRegion, lane.Positive, positiveRegion, width, headroom, clearance, cost)
			appendNavDoorDirection(&graphs[positiveGraph], door.ID, lane.Positive, positiveRegion, lane.Negative, negativeRegion, width, headroom, clearance, cost)
			linked = true
		}
		if !linked {
			code := NavDoorSkippedUnsupported
			if len(lanes[door.ID]) != 0 {
				code = NavDoorSkippedSameRegion
			}
			diagnostics = append(diagnostics, NavDoorDiagnostic{DoorID: door.ID, Code: code})
			if strict && code == NavDoorSkippedUnsupported {
				return nil, diagnostics, fmt.Errorf("navigation door %q: %s", door.ID, code)
			}
		}
	}
	for i := range graphs {
		for id := range graphs[i].Transitions {
			graphs[i].Transitions[id].ID = uint32(id)
		}
		if validation := ValidateNavGraphTile(&graphs[i]); validation.HasErrors() {
			return nil, diagnostics, fmt.Errorf("invalid door-linked navigation graph tile %s: %s", TerrainChunkKey(graphs[i].Coord), validation.Error())
		}
	}
	return graphs, diagnostics, nil
}

func navDoorRegionPartition(coord TerrainChunkCoordDef, doors []NavDoorDef, chunkSize int, voxelResolution float32) func(NavSpanDef) string {
	minX := float32(coord.X*chunkSize) * voxelResolution
	maxX := float32((coord.X+1)*chunkSize) * voxelResolution
	minZ := float32(coord.Z*chunkSize) * voxelResolution
	maxZ := float32((coord.Z+1)*chunkSize) * voxelResolution
	var relevant []NavDoorDef
	for _, door := range doors {
		normal, _ := navDoorAxes(door)
		plane := door.BoundsCenter[normal]
		if normal == 0 && plane >= minX && plane <= maxX || normal == 2 && plane >= minZ && plane <= maxZ {
			relevant = append(relevant, door)
		}
	}
	if len(relevant) == 0 {
		return nil
	}
	return func(span NavSpanDef) string {
		point := navDoorSpanCenter(coord, span, chunkSize, voxelResolution)
		var result strings.Builder
		result.Grow(2 * len(relevant))
		for _, door := range relevant {
			normal, _ := navDoorAxes(door)
			result.WriteByte(byte('0' + normal))
			if point[normal] <= door.BoundsCenter[normal] {
				result.WriteByte('-')
			} else {
				result.WriteByte('+')
			}
		}
		return result.String()
	}
}

func navDoorLanes(door NavDoorDef, spans map[NavSpanRef]navDoorSpan, accepted map[NavSpanRef]struct{}, profile NavAgentProfileDef, chunkSize int, voxelResolution float32) []navDoorLane {
	normal, tangent := navDoorAxes(door)
	expandedNormal := door.BoundsHalfExtents[normal] + profile.Radius
	expandedTangent := door.BoundsHalfExtents[tangent] + profile.Radius
	negative := make(map[navDoorLaneKey]navDoorSpan)
	positive := make(map[navDoorLaneKey]navDoorSpan)
	for ref, candidate := range spans {
		if _, ok := accepted[ref]; !ok || !navDoorBlocksAgentAtHeight(door, candidate.Point[1], profile.Height) {
			continue
		}
		if absFloat32(candidate.Point[tangent]-door.BoundsCenter[tangent]) > expandedTangent {
			continue
		}
		globalTangent := candidate.Span.Z + candidate.Ref.Tile.Z*chunkSize
		if tangent == 0 {
			globalTangent = candidate.Span.X + candidate.Ref.Tile.X*chunkSize
		}
		key := navDoorLaneKey{Tangent: globalTangent, Height: math.Float32bits(candidate.Point[1])}
		offset := candidate.Point[normal] - door.BoundsCenter[normal]
		switch {
		case offset <= -expandedNormal:
			if current, ok := negative[key]; !ok || candidate.Point[normal] > current.Point[normal] || candidate.Point[normal] == current.Point[normal] && navSpanRefLess(candidate.Ref, current.Ref) {
				negative[key] = candidate
			}
		case offset >= expandedNormal:
			if current, ok := positive[key]; !ok || candidate.Point[normal] < current.Point[normal] || candidate.Point[normal] == current.Point[normal] && navSpanRefLess(candidate.Ref, current.Ref) {
				positive[key] = candidate
			}
		}
	}
	keys := make([]navDoorLaneKey, 0, len(negative))
	for key := range negative {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Height != keys[j].Height {
			return keys[i].Height < keys[j].Height
		}
		return keys[i].Tangent < keys[j].Tangent
	})
	maxDistance := max(2*(expandedNormal+2*voxelResolution), profile.Height+2*profile.Radius)
	result := make([]navDoorLane, 0, len(keys))
	seen := make(map[[2]NavSpanRef]struct{})
	for _, key := range keys {
		a := negative[key]
		var b navDoorSpan
		found := false
		bestHeightDelta := float32(math.Inf(1))
		for positiveKey, candidate := range positive {
			if positiveKey.Tangent != key.Tangent {
				continue
			}
			heightDelta := absFloat32(candidate.Point[1] - a.Point[1])
			if heightDelta > profile.StepHeight+voxelResolution || heightDelta > bestHeightDelta || heightDelta == bestHeightDelta && found && !navSpanRefLess(candidate.Ref, b.Ref) {
				continue
			}
			b, bestHeightDelta, found = candidate, heightDelta, true
		}
		pair := [2]NavSpanRef{a.Ref, b.Ref}
		if found && absFloat32(b.Point[normal]-a.Point[normal]) <= maxDistance {
			if _, duplicate := seen[pair]; !duplicate {
				seen[pair] = struct{}{}
				result = append(result, navDoorLane{Negative: a, Positive: b})
			}
		}
	}
	return result
}

func navDoorCutsEdge(door NavDoorDef, from, to navDoorSpan, profile NavAgentProfileDef) bool {
	if !navDoorBlocksAgentAtHeight(door, from.Point[1], profile.Height) || !navDoorBlocksAgentAtHeight(door, to.Point[1], profile.Height) {
		return false
	}
	normal, tangent := navDoorAxes(door)
	a, b, plane := from.Point[normal], to.Point[normal], door.BoundsCenter[normal]
	if !((a <= plane && b > plane) || (b <= plane && a > plane)) {
		return false
	}
	fraction := (plane - a) / (b - a)
	crossing := from.Point[tangent] + fraction*(to.Point[tangent]-from.Point[tangent])
	return absFloat32(crossing-door.BoundsCenter[tangent]) <= door.BoundsHalfExtents[tangent]+profile.Radius
}

func navDoorBlocksAgentAtHeight(door NavDoorDef, support, height float32) bool {
	minY := door.BoundsCenter[1] - door.BoundsHalfExtents[1]
	maxY := door.BoundsCenter[1] + door.BoundsHalfExtents[1]
	return support < maxY && support+height > minY
}

func navDoorAxes(door NavDoorDef) (normal, tangent int) {
	if door.BoundsHalfExtents[0] <= door.BoundsHalfExtents[2] {
		return 0, 2
	}
	return 2, 0
}

func navDoorSpanCenter(tile TerrainChunkCoordDef, span NavSpanDef, chunkSize int, voxelResolution float32) Vec3 {
	return Vec3{
		(float32(tile.X*chunkSize+span.X) + 0.5) * voxelResolution,
		span.SupportHeight,
		(float32(tile.Z*chunkSize+span.Z) + 0.5) * voxelResolution,
	}
}

func navDoorAppendExternalRegionTransitions(graphs []NavGraphTileDef, spans map[NavSpanRef]navDoorSpan) {
	regions := make(map[TerrainChunkCoordDef]map[uint32]uint32, len(graphs))
	for _, graph := range graphs {
		regions[graph.Coord] = navGraphSpanRegions(graph)
	}
	for i := range graphs {
		for _, edge := range graphs[i].SpanTransitions {
			if edge.To.Tile == graphs[i].Coord {
				continue
			}
			fromRegion, fromOK := regions[graphs[i].Coord][edge.From]
			toRegion, toOK := regions[edge.To.Tile][edge.To.Span]
			from, hasFrom := spans[NavSpanRef{Tile: graphs[i].Coord, Span: edge.From}]
			to, hasTo := spans[edge.To]
			if !fromOK || !toOK || !hasFrom || !hasTo {
				continue
			}
			graphs[i].Transitions = append(graphs[i].Transitions, NavRegionTransitionDef{
				ID: uint32(len(graphs[i].Transitions)), FromRegion: fromRegion,
				ToTile: edge.To.Tile, ToRegion: toRegion, Kind: edge.Kind,
				CrossingStart: from.Point, CrossingEnd: to.Point, Width: edge.Width,
				MinHeadroom: edge.MinHeadroom, MinClearance: edge.MinClearance, Cost: edge.Cost,
				RequiresFlags: append([]string(nil), edge.RequiresFlags...), Traversal: cloneNavTraversal(edge.Traversal), Gate: cloneNavTransitionGate(edge.Gate),
			})
		}
	}
}

func appendNavDoorDirection(graph *NavGraphTileDef, doorID string, from navDoorSpan, fromRegion uint32, to navDoorSpan, toRegion uint32, width, headroom, clearance, cost float32) {
	linkID := navTraversalLinkID(NavTransitionWalk, doorID, from.Ref, to.Ref)
	spanTraversal := &NavTraversalDef{ID: doorID, LinkID: linkID, OwnerID: doorID, Start: from.Point, End: to.Point}
	graph.SpanTransitions = append(graph.SpanTransitions, NavSpanTransitionDef{
		From: from.Ref.Span, To: to.Ref, Kind: NavTransitionWalk,
		StepDelta: to.Point[1] - from.Point[1], Width: width,
		MinHeadroom: headroom, MinClearance: clearance, Cost: cost, Traversal: spanTraversal,
		Gate: &NavTransitionGateDef{Kind: NavGateDoor, ID: doorID},
	})
	regionTraversal := &NavTraversalDef{ID: doorID, LinkID: linkID, OwnerID: doorID, Start: from.Point, End: to.Point}
	graph.Transitions = append(graph.Transitions, NavRegionTransitionDef{
		ID: uint32(len(graph.Transitions)), FromRegion: fromRegion,
		ToTile: to.Ref.Tile, ToRegion: toRegion, Kind: NavTransitionWalk,
		CrossingStart: from.Point, CrossingEnd: to.Point, Width: width,
		MinHeadroom: headroom, MinClearance: clearance, Cost: cost, Traversal: regionTraversal,
		Gate: &NavTransitionGateDef{Kind: NavGateDoor, ID: doorID},
	})
}

func navDoorHorizontal(door NavDoorDef) bool {
	return door.BoundsHalfExtents[1] <= min(door.BoundsHalfExtents[0], door.BoundsHalfExtents[2])
}

type navDoorMovementKey struct {
	Kind      string
	Traversal string
	Start     Vec3
	End       Vec3
}

type navDoorGroup struct {
	Doors []NavDoorDef
}

type navDoorDropCandidate struct {
	Span  navDoorSpan
	Score float32
}

// connectNavGraphDoorGates runs after authored movement links exist. A hatch
// gates an intersecting ladder/jump/drop; only when none exists does it infer
// a directed drop to the highest supported landing below the opening.
func connectNavGraphDoorGates(sources []NavSourceTileDef, graphs []NavGraphTileDef, doors []NavDoorDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32, strict bool) ([]NavGraphTileDef, []NavDoorDiagnostic, error) {
	graphs = append([]NavGraphTileDef(nil), graphs...)
	horizontal := make([]NavDoorDef, 0, len(doors))
	for _, door := range doors {
		if navDoorHorizontal(door) {
			horizontal = append(horizontal, door)
		}
	}
	sort.Slice(horizontal, func(i, j int) bool { return horizontal[i].ID < horizontal[j].ID })
	if len(horizontal) == 0 {
		return graphs, nil, nil
	}

	horizontalIDs := make(map[string]struct{}, len(horizontal))
	for _, door := range horizontal {
		horizontalIDs[door.ID] = struct{}{}
	}
	for i := range graphs {
		graphs[i].SpanTransitions = resetHorizontalDoorSpanGates(graphs[i].SpanTransitions, horizontalIDs)
		graphs[i].Transitions = resetHorizontalDoorRegionGates(graphs[i].Transitions, horizontalIDs)
	}

	spanIndex := make(map[NavSpanRef]navDoorSpan)
	accepted := make(map[NavSpanRef]struct{})
	for _, graph := range graphs {
		for _, spanID := range graph.SpanIDs {
			accepted[NavSpanRef{Tile: graph.Coord, Span: spanID}] = struct{}{}
		}
	}
	for _, source := range sources {
		for _, span := range source.Spans {
			ref := NavSpanRef{Tile: source.Coord, Span: span.ID}
			spanIndex[ref] = navDoorSpan{Ref: ref, Span: span, Point: navDoorSpanCenter(ref.Tile, span, chunkSize, voxelResolution)}
		}
	}
	graphIndex := make(map[TerrainChunkCoordDef]int, len(graphs))
	regions := make(map[TerrainChunkCoordDef]map[uint32]uint32, len(graphs))
	for i := range graphs {
		graphIndex[graphs[i].Coord] = i
		regions[graphs[i].Coord] = navGraphSpanRegions(graphs[i])
	}

	var diagnostics []NavDoorDiagnostic
	// ponytail: scan movement links per physical hatch group; add a spatial
	// index only if maps with many hatches make this measurable at bake time.
	for _, group := range navDoorGroups(horizontal) {
		gated := make(map[navDoorMovementKey]NavTransitionGateDef)
		for i := range graphs {
			for edge := range graphs[i].SpanTransitions {
				transition := &graphs[i].SpanTransitions[edge]
				if transition.Gate != nil || !navDoorGateableMovement(transition.Kind) || transition.Traversal == nil {
					continue
				}
				door, found := navDoorCrossingHorizontalTraversal(group.Doors, transition.Traversal, profile.Radius)
				if !found {
					continue
				}
				gate := NavTransitionGateDef{Kind: NavGateDoor, ID: door.ID}
				transition.Gate, transition.Cost = &gate, transition.Cost+NavDoorClosedCostPenalty
				gated[navDoorMovementKey{Kind: transition.Kind, Traversal: transition.Traversal.StableLinkID(), Start: transition.Traversal.Start, End: transition.Traversal.End}] = gate
			}
		}
		if len(gated) != 0 {
			for i := range graphs {
				for edge := range graphs[i].Transitions {
					transition := &graphs[i].Transitions[edge]
					if transition.Traversal == nil {
						continue
					}
					key := navDoorMovementKey{Kind: transition.Kind, Traversal: transition.Traversal.StableLinkID(), Start: transition.Traversal.Start, End: transition.Traversal.End}
					if gate, found := gated[key]; found {
						transition.Gate, transition.Cost = &NavTransitionGateDef{Kind: gate.Kind, ID: gate.ID}, transition.Cost+NavDoorClosedCostPenalty
					}
				}
			}
			continue
		}

		linked := false
		seen := make(map[[2]NavSpanRef]struct{})
		for _, door := range group.Doors {
			for _, lane := range navHorizontalDoorDropLanes(door, spanIndex, accepted, profile, chunkSize, voxelResolution) {
				if navDoorOpenPoseBlocksStanding(group.Doors, lane.Positive.Point, profile) {
					continue
				}
				pair := [2]NavSpanRef{lane.Negative.Ref, lane.Positive.Ref}
				if _, duplicate := seen[pair]; duplicate {
					continue
				}
				seen[pair] = struct{}{}
				fromGraph, fromOK := graphIndex[lane.Negative.Ref.Tile]
				fromRegion, fromRegionOK := regions[lane.Negative.Ref.Tile][lane.Negative.Ref.Span]
				toRegion, toRegionOK := regions[lane.Positive.Ref.Tile][lane.Positive.Ref.Span]
				if !fromOK || !fromRegionOK || !toRegionOK || lane.Negative.Ref.Tile == lane.Positive.Ref.Tile && fromRegion == toRegion {
					continue
				}
				width := min(voxelResolution, 2*min(lane.Negative.Span.ClearanceRadius, lane.Positive.Span.ClearanceRadius))
				if width <= 0 {
					width = voxelResolution
				}
				headroom := min(lane.Negative.Span.Headroom, lane.Positive.Span.Headroom)
				clearance := min(lane.Negative.Span.ClearanceRadius, lane.Positive.Span.ClearanceRadius)
				cost := navVec3Distance(lane.Negative.Point, lane.Positive.Point) + NavDoorClosedCostPenalty
				appendNavDoorDropDirection(&graphs[fromGraph], door.ID, lane.Negative, fromRegion, lane.Positive, toRegion, width, headroom, clearance, cost)
				linked = true
			}
		}
		if !linked {
			for _, door := range group.Doors {
				diagnostics = append(diagnostics, NavDoorDiagnostic{DoorID: door.ID, Code: NavDoorSkippedUnsupported})
			}
			if strict {
				return nil, diagnostics, fmt.Errorf("navigation hatch %q: %s", group.Doors[0].ID, NavDoorSkippedUnsupported)
			}
		}
	}
	for i := range graphs {
		for id := range graphs[i].Transitions {
			graphs[i].Transitions[id].ID = uint32(id)
		}
		if validation := ValidateNavGraphTile(&graphs[i]); validation.HasErrors() {
			return nil, diagnostics, fmt.Errorf("invalid door-gated navigation graph tile %s: %s", TerrainChunkKey(graphs[i].Coord), validation.Error())
		}
	}
	return graphs, diagnostics, nil
}

func navDoorGroups(doors []NavDoorDef) []navDoorGroup {
	used := make([]bool, len(doors))
	result := make([]navDoorGroup, 0, len(doors))
	for seed := range doors {
		if used[seed] {
			continue
		}
		used[seed] = true
		group := navDoorGroup{Doors: []NavDoorDef{doors[seed]}}
		groupID := strings.TrimSpace(doors[seed].Group)
		for next := 0; next < len(group.Doors); next++ {
			for candidate := seed + 1; candidate < len(doors); candidate++ {
				if used[candidate] || groupID == "" || strings.TrimSpace(doors[candidate].Group) != groupID || !navHorizontalDoorsTouch(group.Doors[next], doors[candidate]) {
					continue
				}
				used[candidate] = true
				group.Doors = append(group.Doors, doors[candidate])
			}
		}
		result = append(result, group)
	}
	return result
}

func navHorizontalDoorsTouch(a, b NavDoorDef) bool {
	const epsilon = float32(1e-3)
	for _, axis := range []int{0, 1, 2} {
		if absFloat32(a.BoundsCenter[axis]-b.BoundsCenter[axis]) > a.BoundsHalfExtents[axis]+b.BoundsHalfExtents[axis]+epsilon {
			return false
		}
	}
	return true
}

func navDoorGateableMovement(kind string) bool {
	return kind != NavTransitionWalk && kind != NavTransitionStep && kind != NavTransitionStair
}

func navDoorCrossingHorizontalTraversal(doors []NavDoorDef, traversal *NavTraversalDef, radius float32) (NavDoorDef, bool) {
	if traversal == nil {
		return NavDoorDef{}, false
	}
	for _, door := range doors {
		a, b := traversal.Start[1]-door.BoundsCenter[1], traversal.End[1]-door.BoundsCenter[1]
		if a == b || a < 0 && b < 0 || a > 0 && b > 0 {
			continue
		}
		fraction := a / (a - b)
		x := traversal.Start[0] + fraction*(traversal.End[0]-traversal.Start[0])
		z := traversal.Start[2] + fraction*(traversal.End[2]-traversal.Start[2])
		if absFloat32(x-door.BoundsCenter[0]) <= door.BoundsHalfExtents[0]+radius && absFloat32(z-door.BoundsCenter[2]) <= door.BoundsHalfExtents[2]+radius {
			return door, true
		}
	}
	return NavDoorDef{}, false
}

func resetHorizontalDoorSpanGates(transitions []NavSpanTransitionDef, doorIDs map[string]struct{}) []NavSpanTransitionDef {
	result := make([]NavSpanTransitionDef, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Gate == nil {
			result = append(result, transition)
			continue
		}
		if _, horizontal := doorIDs[transition.Gate.ID]; !horizontal {
			result = append(result, transition)
			continue
		}
		if transition.Kind == NavTransitionDrop && transition.Traversal != nil && transition.Traversal.Owner() == transition.Gate.ID {
			continue
		}
		transition.Gate = nil
		transition.Cost = max(transition.Cost-NavDoorClosedCostPenalty, 0.001)
		result = append(result, transition)
	}
	return result
}

func resetHorizontalDoorRegionGates(transitions []NavRegionTransitionDef, doorIDs map[string]struct{}) []NavRegionTransitionDef {
	result := make([]NavRegionTransitionDef, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Gate != nil {
			if _, horizontal := doorIDs[transition.Gate.ID]; horizontal {
				if transition.Kind == NavTransitionDrop && transition.Traversal != nil && transition.Traversal.Owner() == transition.Gate.ID {
					continue
				}
				transition.Gate = nil
				transition.Cost = max(transition.Cost-NavDoorClosedCostPenalty, 0.001)
			}
		}
		transition.ID = uint32(len(result))
		result = append(result, transition)
	}
	return result
}

func navHorizontalDoorDropLanes(door NavDoorDef, spans map[NavSpanRef]navDoorSpan, accepted map[NavSpanRef]struct{}, profile NavAgentProfileDef, chunkSize int, voxelResolution float32) []navDoorLane {
	top := door.BoundsCenter[1] + door.BoundsHalfExtents[1]
	bottom := door.BoundsCenter[1] - door.BoundsHalfExtents[1]
	var result []navDoorLane
	seen := make(map[[2]NavSpanRef]struct{})
	for _, normal := range []int{0, 2} {
		tangent := 2 - normal
		for _, side := range []float32{-1, 1} {
			upper := make(map[int]navDoorDropCandidate)
			lower := make(map[int]navDoorDropCandidate)
			for ref, candidate := range spans {
				if _, ok := accepted[ref]; !ok || absFloat32(candidate.Point[tangent]-door.BoundsCenter[tangent]) > door.BoundsHalfExtents[tangent]+voxelResolution {
					continue
				}
				globalTangent := candidate.Span.Z + candidate.Ref.Tile.Z*chunkSize
				if tangent == 0 {
					globalTangent = candidate.Span.X + candidate.Ref.Tile.X*chunkSize
				}
				offset := (candidate.Point[normal] - door.BoundsCenter[normal]) * side
				if absFloat32(candidate.Point[1]-top) <= profile.StepHeight+2*voxelResolution && offset >= door.BoundsHalfExtents[normal]-voxelResolution/2 && offset-door.BoundsHalfExtents[normal] <= profile.Radius+2*voxelResolution {
					score := absFloat32(offset - door.BoundsHalfExtents[normal] - profile.Radius)
					if current, ok := upper[globalTangent]; !ok || score < current.Score || score == current.Score && navSpanRefLess(candidate.Ref, current.Span.Ref) {
						upper[globalTangent] = navDoorDropCandidate{Span: candidate, Score: score}
					}
				}
				if candidate.Point[1] > bottom-profile.StepHeight || absFloat32(candidate.Point[normal]-door.BoundsCenter[normal]) > door.BoundsHalfExtents[normal]+voxelResolution/2 {
					continue
				}
				desired := max(door.BoundsHalfExtents[normal]-profile.Radius, 0)
				score := absFloat32(offset - desired)
				if current, ok := lower[globalTangent]; !ok || candidate.Point[1] > current.Span.Point[1] || candidate.Point[1] == current.Span.Point[1] && (score < current.Score || score == current.Score && navSpanRefLess(candidate.Ref, current.Span.Ref)) {
					lower[globalTangent] = navDoorDropCandidate{Span: candidate, Score: score}
				}
			}
			keys := make([]int, 0, len(upper))
			for key := range upper {
				keys = append(keys, key)
			}
			sort.Ints(keys)
			for _, key := range keys {
				landing, ok := lower[key]
				if !ok {
					continue
				}
				entry := upper[key].Span
				if !NavTraversalSupportedByProfile(profile, NavTransitionDrop, &NavTraversalDef{Start: entry.Point, End: landing.Span.Point}) {
					continue
				}
				pair := [2]NavSpanRef{entry.Ref, landing.Span.Ref}
				if _, duplicate := seen[pair]; duplicate || navVec3Distance(entry.Point, landing.Span.Point) <= profile.StepHeight {
					continue
				}
				seen[pair] = struct{}{}
				result = append(result, navDoorLane{Negative: entry, Positive: landing.Span})
			}
		}
	}
	return result
}

func navDoorOpenPoseBlocksStanding(doors []NavDoorDef, point Vec3, profile NavAgentProfileDef) bool {
	for _, door := range doors {
		if door.OpenOffset == (Vec3{}) {
			continue
		}
		center := Vec3{
			door.BoundsCenter[0] + door.OpenOffset[0],
			door.BoundsCenter[1] + door.OpenOffset[1],
			door.BoundsCenter[2] + door.OpenOffset[2],
		}
		if point[1]+profile.Height <= center[1]-door.BoundsHalfExtents[1] ||
			point[1] >= center[1]+door.BoundsHalfExtents[1] {
			continue
		}
		dx := max(absFloat32(point[0]-center[0])-door.BoundsHalfExtents[0], 0)
		dz := max(absFloat32(point[2]-center[2])-door.BoundsHalfExtents[2], 0)
		if dx*dx+dz*dz < profile.Radius*profile.Radius {
			return true
		}
	}
	return false
}

func appendNavDoorDropDirection(graph *NavGraphTileDef, doorID string, from navDoorSpan, fromRegion uint32, to navDoorSpan, toRegion uint32, width, headroom, clearance, cost float32) {
	linkID := navTraversalLinkID(NavTransitionDrop, doorID, from.Ref, to.Ref)
	traversal := &NavTraversalDef{ID: doorID, LinkID: linkID, OwnerID: doorID, Start: from.Point, End: to.Point}
	graph.SpanTransitions = append(graph.SpanTransitions, NavSpanTransitionDef{
		From: from.Ref.Span, To: to.Ref, Kind: NavTransitionDrop,
		StepDelta: to.Point[1] - from.Point[1], Width: width,
		MinHeadroom: headroom, MinClearance: clearance, Cost: cost, Traversal: traversal,
		Gate: &NavTransitionGateDef{Kind: NavGateDoor, ID: doorID},
	})
	graph.Transitions = append(graph.Transitions, NavRegionTransitionDef{
		ID: uint32(len(graph.Transitions)), FromRegion: fromRegion,
		ToTile: to.Ref.Tile, ToRegion: toRegion, Kind: NavTransitionDrop,
		CrossingStart: from.Point, CrossingEnd: to.Point, Width: width,
		MinHeadroom: headroom, MinClearance: clearance, Cost: cost,
		Traversal: &NavTraversalDef{ID: doorID, LinkID: linkID, OwnerID: doorID, Start: from.Point, End: to.Point},
		Gate:      &NavTransitionGateDef{Kind: NavGateDoor, ID: doorID},
	})
}

func expandNavGraphCoordsForDoors(coords map[TerrainChunkCoordDef]struct{}, doors []NavDoorDef, chunkSize int, voxelResolution float32) map[TerrainChunkCoordDef]struct{} {
	result := make(map[TerrainChunkCoordDef]struct{}, len(coords))
	for coord := range coords {
		result[coord] = struct{}{}
	}
	for _, door := range doors {
		center := navLadderMountTileCoords(door.BoundsCenter, chunkSize, voxelResolution)
		touches := false
		for _, coord := range center {
			if _, ok := result[coord]; ok {
				touches = true
				break
			}
		}
		if touches {
			for _, coord := range center {
				result[coord] = struct{}{}
			}
		}
	}
	return result
}
