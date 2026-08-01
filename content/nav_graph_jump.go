package content

import (
	"fmt"
	"math"
	"sort"
)

const AutoNavJumpOwnerID = "__auto_jump"

type navJumpSpan struct {
	Ref    NavSpanRef
	Span   NavSpanDef
	Point  Vec3
	Region uint32
}

type navJumpColumn struct{ X, Z int }

type navJumpRegionPair struct {
	From navRouteNode
	To   navRouteNode
}

type navJumpCandidate struct {
	From      navJumpSpan
	To        navJumpSpan
	Traversal NavTraversalDef
	Cost      float32
}

type navJumpVoxelInterval struct{ Start, End int }

type navJumpOccupancy struct {
	ChunkSize int
	Known     map[TerrainChunkCoordDef]struct{}
	Columns   map[navJumpColumn][]navJumpVoxelInterval
}

// ConnectNavGraphJumps discovers gap and upward jumps from exposed
// graph boundaries. Candidates are deduplicated before sparse voxel arc checks.
func ConnectNavGraphJumps(sources []NavSourceTileDef, graphs []NavGraphTileDef, profile NavAgentProfileDef, chunkSize int, voxelResolution float32) ([]NavGraphTileDef, error) {
	graphs = append([]NavGraphTileDef(nil), graphs...)
	for i := range graphs {
		graphs[i].SpanTransitions = removeAutoNavJumpSpanTransitions(graphs[i].SpanTransitions)
		graphs[i].Transitions = removeAutoNavJumpRegionTransitions(graphs[i].Transitions)
	}
	if !navProfileHasCapability(profile, NavCapabilityJump) || profile.MaxJumpDistance <= 0 ||
		profile.JumpSpeed <= 0 || profile.JumpHorizontalSpeed <= 0 || profile.Gravity <= 0 ||
		voxelResolution <= 0 || chunkSize <= 0 {
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

	columns := make(map[navJumpColumn][]navJumpSpan)
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
			candidate := navJumpSpan{
				Ref: ref, Span: span, Region: region,
				Point: Vec3{
					(float32(ref.Tile.X*chunkSize+span.X) + 0.5) * voxelResolution,
					span.SupportHeight,
					(float32(ref.Tile.Z*chunkSize+span.Z) + 0.5) * voxelResolution,
				},
			}
			key := navJumpColumn{ref.Tile.X*chunkSize + span.X, ref.Tile.Z*chunkSize + span.Z}
			columns[key] = append(columns[key], candidate)
		}
	}
	for key := range columns {
		sort.Slice(columns[key], func(i, j int) bool { return navSpanRefLess(columns[key][i].Ref, columns[key][j].Ref) })
	}

	type direction struct{ X, Z int }
	directions := [...]direction{
		{X: -1}, {X: -1, Z: -1}, {Z: -1}, {X: 1, Z: -1},
		{X: 1}, {X: 1, Z: 1}, {Z: 1}, {X: -1, Z: 1},
	}
	insetSteps := max(1, int(math.Ceil(float64((profile.Radius+voxelResolution*0.5)/voxelResolution))))
	maxSteps := int(math.Floor(float64(profile.MaxJumpDistance / voxelResolution)))
	best := make(map[navJumpRegionPair]navJumpCandidate)
	for boundaryColumn, boundarySpans := range columns {
		for _, boundary := range boundarySpans {
			fromNode := navRouteNode{Tile: boundary.Ref.Tile, Region: boundary.Region}
			for _, direction := range directions {
				if navJumpHasSupportNear(columns[navJumpColumn{boundaryColumn.X + direction.X, boundaryColumn.Z + direction.Z}], boundary.Span.SupportHeight, profile.StepHeight) {
					continue
				}
				startColumn := navJumpColumn{boundaryColumn.X - direction.X*insetSteps, boundaryColumn.Z - direction.Z*insetSteps}
				from, ok := navJumpSpanInNode(columns[startColumn], fromNode, boundary.Span.SupportHeight, profile.StepHeight)
				if !ok {
					continue
				}
				for distance := 1; distance+2*insetSteps <= maxSteps; distance++ {
					landingColumn := navJumpColumn{boundaryColumn.X + direction.X*distance, boundaryColumn.Z + direction.Z*distance}
					landing, found := navJumpLanding(columns[landingColumn], from.Span.SupportHeight, profile)
					if !found {
						continue
					}
					toNode := navRouteNode{Tile: landing.Ref.Tile, Region: landing.Region}
					if fromNode == toNode {
						continue
					}
					endColumn := navJumpColumn{landingColumn.X + direction.X*insetSteps, landingColumn.Z + direction.Z*insetSteps}
					to, found := navJumpSpanInNode(columns[endColumn], toNode, landing.Span.SupportHeight, profile.StepHeight)
					if !found {
						continue
					}
					traversal, reachable := navJumpTraversal(profile, from.Point, to.Point)
					if !reachable {
						continue
					}
					cost := navVec3Distance(from.Point, to.Point) + 2 + max(to.Point[1]-from.Point[1], 0)*2
					key := navJumpRegionPair{From: fromNode, To: toNode}
					candidate := navJumpCandidate{From: from, To: to, Traversal: traversal, Cost: cost}
					if previous, exists := best[key]; !exists || navJumpCandidateLess(candidate, previous) {
						best[key] = candidate
					}
					break
				}
			}
		}
	}

	occupancy := buildNavJumpOccupancy(sources, chunkSize)
	candidates := make([]navJumpCandidate, 0, len(best))
	for _, candidate := range best {
		if navBallisticArcClear(occupancy, candidate.Traversal, profile, voxelResolution) {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return navJumpCandidateLess(candidates[i], candidates[j]) })
	for _, candidate := range candidates {
		graph := &graphs[graphIndex[candidate.From.Ref.Tile]]
		linkID := navTraversalLinkID(NavTransitionJump, AutoNavJumpOwnerID, candidate.From.Ref, candidate.To.Ref)
		candidate.Traversal.LinkID = linkID
		candidate.Traversal.OwnerID = AutoNavJumpOwnerID
		width := min(voxelResolution, 2*min(candidate.From.Span.ClearanceRadius, candidate.To.Span.ClearanceRadius))
		if width <= 0 {
			width = voxelResolution
		}
		headroom := min(candidate.From.Span.Headroom, candidate.To.Span.Headroom)
		clearance := min(candidate.From.Span.ClearanceRadius, candidate.To.Span.ClearanceRadius)
		traversal := candidate.Traversal
		graph.SpanTransitions = append(graph.SpanTransitions, NavSpanTransitionDef{
			From: candidate.From.Ref.Span, To: candidate.To.Ref, Kind: NavTransitionJump,
			StepDelta: candidate.To.Point[1] - candidate.From.Point[1], Width: width,
			MinHeadroom: headroom, MinClearance: clearance, Cost: candidate.Cost,
			Traversal: &traversal,
		})
		regionTraversal := candidate.Traversal
		graph.Transitions = append(graph.Transitions, NavRegionTransitionDef{
			ID: uint32(len(graph.Transitions)), FromRegion: candidate.From.Region,
			ToTile: candidate.To.Ref.Tile, ToRegion: candidate.To.Region, Kind: NavTransitionJump,
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
			return nil, fmt.Errorf("invalid jump-linked navigation graph tile %s: %s", TerrainChunkKey(graphs[i].Coord), validation.Error())
		}
	}
	return graphs, nil
}

func navJumpHasSupportNear(spans []navJumpSpan, height, tolerance float32) bool {
	for _, span := range spans {
		if absFloat32(span.Span.SupportHeight-height) <= tolerance+1e-4 {
			return true
		}
	}
	return false
}

func navJumpSpanInNode(spans []navJumpSpan, node navRouteNode, height, tolerance float32) (navJumpSpan, bool) {
	var result navJumpSpan
	found := false
	for _, candidate := range spans {
		if candidate.Ref.Tile != node.Tile || candidate.Region != node.Region || absFloat32(candidate.Span.SupportHeight-height) > tolerance+1e-4 {
			continue
		}
		if !found || absFloat32(candidate.Span.SupportHeight-height) < absFloat32(result.Span.SupportHeight-height) ||
			candidate.Span.SupportHeight == result.Span.SupportHeight && navSpanRefLess(candidate.Ref, result.Ref) {
			result, found = candidate, true
		}
	}
	return result, found
}

func navJumpLanding(spans []navJumpSpan, fromHeight float32, profile NavAgentProfileDef) (navJumpSpan, bool) {
	var result navJumpSpan
	found := false
	for _, candidate := range spans {
		rise := candidate.Span.SupportHeight - fromHeight
		if rise < -profile.StepHeight-1e-4 || profile.MaxJumpRise > 0 && rise > profile.MaxJumpRise+1e-4 {
			continue
		}
		if !found || absFloat32(rise) < absFloat32(result.Span.SupportHeight-fromHeight) ||
			absFloat32(rise) == absFloat32(result.Span.SupportHeight-fromHeight) && navSpanRefLess(candidate.Ref, result.Ref) {
			result, found = candidate, true
		}
	}
	return result, found
}

func navJumpTraversal(profile NavAgentProfileDef, start, end Vec3) (NavTraversalDef, bool) {
	deltaY := end[1] - start[1]
	discriminant := profile.JumpSpeed*profile.JumpSpeed - 2*profile.Gravity*deltaY
	if discriminant < 0 {
		return NavTraversalDef{}, false
	}
	duration := (profile.JumpSpeed + float32(math.Sqrt(float64(discriminant)))) / profile.Gravity
	horizontal := float32(math.Hypot(float64(end[0]-start[0]), float64(end[2]-start[2])))
	if duration <= 0 || horizontal <= 0 {
		return NavTraversalDef{}, false
	}
	speed := horizontal / duration
	if speed > profile.JumpHorizontalSpeed+1e-4 {
		return NavTraversalDef{}, false
	}
	apexTime := profile.JumpSpeed / profile.Gravity
	fraction := min(max(apexTime/duration, 0), 1)
	traversal := NavTraversalDef{
		Start: start,
		Apex: Vec3{
			start[0] + (end[0]-start[0])*fraction,
			start[1] + profile.JumpSpeed*profile.JumpSpeed/(2*profile.Gravity),
			start[2] + (end[2]-start[2])*fraction,
		},
		End: end, Speed: speed, LaunchSpeed: profile.JumpSpeed, Duration: duration,
	}
	return traversal, NavTraversalSupportedByProfile(profile, NavTransitionJump, &traversal)
}

func buildNavJumpOccupancy(sources []NavSourceTileDef, chunkSize int) navJumpOccupancy {
	result := navJumpOccupancy{
		ChunkSize: chunkSize,
		Known:     make(map[TerrainChunkCoordDef]struct{}, len(sources)),
		Columns:   make(map[navJumpColumn][]navJumpVoxelInterval),
	}
	for _, source := range sources {
		result.Known[source.Coord] = struct{}{}
		add := func(run NavVoxelRunDef) {
			column := navJumpColumn{source.Coord.X*chunkSize + run.X, source.Coord.Z*chunkSize + run.Z}
			start := source.Coord.Y*chunkSize + run.Y
			result.Columns[column] = append(result.Columns[column], navJumpVoxelInterval{Start: start, End: start + run.Count})
		}
		for _, run := range source.SolidRuns {
			add(run)
		}
		for _, run := range source.BlockedRuns {
			add(run)
		}
	}
	return result
}

func navBallisticArcClear(occupancy navJumpOccupancy, traversal NavTraversalDef, profile NavAgentProfileDef, voxelResolution float32) bool {
	horizontal := float32(math.Hypot(float64(traversal.End[0]-traversal.Start[0]), float64(traversal.End[2]-traversal.Start[2])))
	apexRise := float32(0)
	if traversal.LaunchSpeed > 0 {
		apexRise = traversal.LaunchSpeed * traversal.LaunchSpeed / (2 * profile.Gravity)
	}
	verticalTravel := 2*apexRise + absFloat32(traversal.End[1]-traversal.Start[1])
	samples := max(2, int(math.Ceil(float64(max(horizontal, verticalTravel)/(voxelResolution*0.5)))))
	for sample := 0; sample <= samples; sample++ {
		fraction := float32(sample) / float32(samples)
		t := traversal.Duration * fraction
		base := Vec3{
			traversal.Start[0] + (traversal.End[0]-traversal.Start[0])*fraction,
			traversal.Start[1] + traversal.LaunchSpeed*t - 0.5*profile.Gravity*t*t,
			traversal.Start[2] + (traversal.End[2]-traversal.Start[2])*fraction,
		}
		if !navJumpCapsuleClear(occupancy, base, profile.Radius, profile.Height, voxelResolution) {
			return false
		}
	}
	return true
}

func navJumpCapsuleClear(occupancy navJumpOccupancy, base Vec3, radius, height, voxelResolution float32) bool {
	minX := int(math.Floor(float64((base[0] - radius) / voxelResolution)))
	maxX := int(math.Floor(float64((base[0] + radius) / voxelResolution)))
	minZ := int(math.Floor(float64((base[2] - radius) / voxelResolution)))
	maxZ := int(math.Floor(float64((base[2] + radius) / voxelResolution)))
	minY := int(math.Floor(float64((base[1] + voxelResolution*0.05) / voxelResolution)))
	maxY := int(math.Floor(float64((base[1] + height - voxelResolution*0.05) / voxelResolution)))
	for z := minZ; z <= maxZ; z++ {
		cellMinZ, cellMaxZ := float32(z)*voxelResolution, float32(z+1)*voxelResolution
		dz := max(cellMinZ-base[2], 0, base[2]-cellMaxZ)
		for x := minX; x <= maxX; x++ {
			cellMinX, cellMaxX := float32(x)*voxelResolution, float32(x+1)*voxelResolution
			dx := max(cellMinX-base[0], 0, base[0]-cellMaxX)
			if dx*dx+dz*dz > radius*radius {
				continue
			}
			if !navJumpColumnKnown(occupancy, x, z, minY, maxY) || navJumpColumnOccupied(occupancy.Columns[navJumpColumn{X: x, Z: z}], minY, maxY+1) {
				return false
			}
		}
	}
	return true
}

func navJumpColumnKnown(occupancy navJumpOccupancy, x, z, minY, maxY int) bool {
	coord := TerrainChunkCoordDef{X: floorDivNavSpan(x, occupancy.ChunkSize), Z: floorDivNavSpan(z, occupancy.ChunkSize)}
	for y := minY; y <= maxY; {
		coord.Y = floorDivNavSpan(y, occupancy.ChunkSize)
		if _, known := occupancy.Known[coord]; !known {
			return false
		}
		y = (coord.Y + 1) * occupancy.ChunkSize
	}
	return true
}

func navJumpColumnOccupied(intervals []navJumpVoxelInterval, start, end int) bool {
	for _, interval := range intervals {
		if interval.Start < end && interval.End > start {
			return true
		}
	}
	return false
}

func navJumpCandidateLess(a, b navJumpCandidate) bool {
	if a.Traversal.Speed != b.Traversal.Speed {
		return a.Traversal.Speed < b.Traversal.Speed
	}
	if a.Cost != b.Cost {
		return a.Cost < b.Cost
	}
	if a.From.Ref != b.From.Ref {
		return navSpanRefLess(a.From.Ref, b.From.Ref)
	}
	return navSpanRefLess(a.To.Ref, b.To.Ref)
}

func removeAutoNavJumpSpanTransitions(transitions []NavSpanTransitionDef) []NavSpanTransitionDef {
	result := make([]NavSpanTransitionDef, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Traversal == nil || transition.Traversal.Owner() != AutoNavJumpOwnerID {
			result = append(result, transition)
		}
	}
	return result
}

func removeAutoNavJumpRegionTransitions(transitions []NavRegionTransitionDef) []NavRegionTransitionDef {
	result := make([]NavRegionTransitionDef, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Traversal == nil || transition.Traversal.Owner() != AutoNavJumpOwnerID {
			transition.ID = uint32(len(result))
			result = append(result, transition)
		}
	}
	return result
}
