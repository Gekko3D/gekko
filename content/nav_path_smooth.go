package content

type navPathPortalSegment struct {
	Start     Vec3
	End       Vec3
	Mandatory bool
}

func navBuildStringPulledWaypoints(start Vec3, end Vec3, segments []navPathPortalSegment) []Vec3 {
	if len(segments) == 0 {
		return navDedupePathWaypoints([]Vec3{start, end})
	}
	waypoints := make([]Vec3, 0, len(segments)+2)
	currentStart := start
	pending := make([]navPathPortalSegment, 0, len(segments))
	for _, segment := range segments {
		if !segment.Mandatory {
			pending = append(pending, segment)
			continue
		}
		mandatoryPoint := navSegmentMidpoint(segment)
		waypoints = appendNavPathWaypoints(waypoints, navBuildStringPulledWaypointRun(currentStart, mandatoryPoint, pending)...)
		currentStart = mandatoryPoint
		pending = pending[:0]
	}
	waypoints = appendNavPathWaypoints(waypoints, navBuildStringPulledWaypointRun(currentStart, end, pending)...)
	return navDedupePathWaypoints(waypoints)
}

func navBuildStringPulledWaypointRun(start Vec3, end Vec3, segments []navPathPortalSegment) []Vec3 {
	if len(segments) == 0 {
		return navDedupePathWaypoints([]Vec3{start, end})
	}
	portals := make([]navFunnelPortal, 0, len(segments)+2)
	portals = append(portals, navFunnelPortal{Left: start, Right: start})
	apex := start
	for i, segment := range segments {
		next := end
		if i+1 < len(segments) {
			next = navSegmentMidpoint(segments[i+1])
		}
		left, right := navOrientPortalForFunnel(apex, next, segment.Start, segment.End)
		portals = append(portals, navFunnelPortal{Left: left, Right: right})
		apex = navSegmentMidpoint(segment)
	}
	portals = append(portals, navFunnelPortal{Left: end, Right: end})
	return navDedupePathWaypoints(navStringPull(portals))
}

func appendNavPathWaypoints(out []Vec3, points ...Vec3) []Vec3 {
	for _, point := range points {
		if len(out) > 0 && navVec2AlmostEqualXZ(out[len(out)-1], point, 1e-4) && navAlmostEqual(out[len(out)-1][1], point[1], 1e-3) {
			continue
		}
		out = append(out, point)
	}
	return out
}

type navFunnelPortal struct {
	Left  Vec3
	Right Vec3
}

func navStringPull(portals []navFunnelPortal) []Vec3 {
	if len(portals) == 0 {
		return nil
	}
	apex := portals[0].Left
	left := portals[0].Left
	right := portals[0].Right
	apexIndex := 0
	leftIndex := 0
	rightIndex := 0
	path := []Vec3{apex}
	for i := 1; i < len(portals); i++ {
		newLeft := portals[i].Left
		newRight := portals[i].Right
		if navFunnelArea2(apex, right, newRight) <= 0 {
			if navVec2AlmostEqualXZ(apex, right, 1e-5) || navFunnelArea2(apex, left, newRight) > 0 {
				right = newRight
				rightIndex = i
			} else {
				path = append(path, left)
				apex = left
				apexIndex = leftIndex
				left = apex
				right = apex
				leftIndex = apexIndex
				rightIndex = apexIndex
				i = apexIndex
				continue
			}
		}
		if navFunnelArea2(apex, left, newLeft) >= 0 {
			if navVec2AlmostEqualXZ(apex, left, 1e-5) || navFunnelArea2(apex, right, newLeft) < 0 {
				left = newLeft
				leftIndex = i
			} else {
				path = append(path, right)
				apex = right
				apexIndex = rightIndex
				left = apex
				right = apex
				leftIndex = apexIndex
				rightIndex = apexIndex
				i = apexIndex
				continue
			}
		}
	}
	path = append(path, portals[len(portals)-1].Left)
	return path
}

func navOrientPortalForFunnel(apex Vec3, next Vec3, a Vec3, b Vec3) (Vec3, Vec3) {
	dirX := next[0] - apex[0]
	dirZ := next[2] - apex[2]
	aCross := navVec2Cross(dirX, dirZ, a[0]-apex[0], a[2]-apex[2])
	bCross := navVec2Cross(dirX, dirZ, b[0]-apex[0], b[2]-apex[2])
	if aCross <= bCross {
		return a, b
	}
	return b, a
}

func navFunnelArea2(a Vec3, b Vec3, c Vec3) float32 {
	return navVec2Cross(b[0]-a[0], b[2]-a[2], c[0]-a[0], c[2]-a[2])
}

func navSegmentMidpoint(segment navPathPortalSegment) Vec3 {
	return Vec3{
		(segment.Start[0] + segment.End[0]) * 0.5,
		(segment.Start[1] + segment.End[1]) * 0.5,
		(segment.Start[2] + segment.End[2]) * 0.5,
	}
}

func navDedupePathWaypoints(points []Vec3) []Vec3 {
	out := make([]Vec3, 0, len(points))
	for _, point := range points {
		if len(out) > 0 && navVec2AlmostEqualXZ(out[len(out)-1], point, 1e-4) && navAlmostEqual(out[len(out)-1][1], point[1], 1e-3) {
			continue
		}
		out = append(out, point)
	}
	return out
}

func navSharedPortalSegment(tileA *NavTileDef, polygonA NavPolygonDef, tileB *NavTileDef, polygonB NavPolygonDef, profile NavAgentProfileDef) (navPathPortalSegment, bool) {
	if tileA == nil || tileB == nil {
		return navPathPortalSegment{}, false
	}
	if tileA == tileB || tileA.Coord == tileB.Coord {
		return navSameTilePortalSegment(tileA, polygonA, polygonB)
	}
	dir := TerrainChunkCoordDef{
		X: tileB.Coord.X - tileA.Coord.X,
		Y: tileB.Coord.Y - tileA.Coord.Y,
		Z: tileB.Coord.Z - tileA.Coord.Z,
	}
	segments := navPolygonsBoundaryPortalSegments(tileA, polygonA, tileB, polygonB, dir, profile)
	return navLargestPortalSegment(segments, profile)
}

func navSameTilePortalSegment(tile *NavTileDef, polygonA NavPolygonDef, polygonB NavPolygonDef) (navPathPortalSegment, bool) {
	var best navPathPortalSegment
	bestLength := float32(0)
	found := false
	for ai := range polygonA.Vertices {
		a0, a1, ok := navPolygonEdge(tile, polygonA, ai)
		if !ok {
			continue
		}
		for bi := range polygonB.Vertices {
			b0, b1, ok := navPolygonEdge(tile, polygonB, bi)
			if !ok {
				continue
			}
			start, end, ok := navEdgeOverlapSegmentXZ(a0, a1, b0, b1)
			if !ok {
				continue
			}
			length := navVec3Distance(start, end)
			if !found || length > bestLength {
				best = navPathPortalSegment{Start: start, End: end, Mandatory: navPathTransitionRequiresWaypoint(polygonA.Area, polygonB.Area, NavTraversalWalk, start, end)}
				bestLength = length
				found = true
			}
		}
	}
	return best, found
}

func navLargestPortalSegment(segments []navBoundaryPortalSegment, profile NavAgentProfileDef) (navPathPortalSegment, bool) {
	var best navPathPortalSegment
	bestLength := float32(0)
	found := false
	for _, segment := range segments {
		if !navPortalSegmentHasAgentClearance(segment.Start, segment.End, profile) {
			continue
		}
		length := navVec3Distance(segment.Start, segment.End)
		if length <= 1e-4 {
			continue
		}
		if !found || length > bestLength {
			best = navPathPortalSegment{Start: segment.Start, End: segment.End}
			bestLength = length
			found = true
		}
	}
	return best, found
}

func navEdgeOverlapSegmentXZ(a0 Vec3, a1 Vec3, b0 Vec3, b1 Vec3) (Vec3, Vec3, bool) {
	const epsilon = float32(1e-4)
	aDX := a1[0] - a0[0]
	aDZ := a1[2] - a0[2]
	bDX := b1[0] - b0[0]
	bDZ := b1[2] - b0[2]
	aLength := navVec2Length(aDX, aDZ)
	bLength := navVec2Length(bDX, bDZ)
	if aLength <= epsilon || bLength <= epsilon {
		return Vec3{}, Vec3{}, false
	}
	if !navSegmentsCollinearXZ(a0, aDX, aDZ, aLength, b0, bDX, bDZ, bLength, epsilon) {
		return Vec3{}, Vec3{}, false
	}
	useX := absNavFloat32(aDX) >= absNavFloat32(aDZ)
	aStart := a0[2]
	aEnd := a1[2]
	bStart := b0[2]
	bEnd := b1[2]
	if useX {
		aStart = a0[0]
		aEnd = a1[0]
		bStart = b0[0]
		bEnd = b1[0]
	}
	denominator := aEnd - aStart
	if absNavFloat32(denominator) <= epsilon {
		return Vec3{}, Vec3{}, false
	}
	overlapMin := maxNavFloat32(minNavFloat32(aStart, aEnd), minNavFloat32(bStart, bEnd))
	overlapMax := minNavFloat32(maxNavFloat32(aStart, aEnd), maxNavFloat32(bStart, bEnd))
	if overlapMax-overlapMin <= epsilon {
		return Vec3{}, Vec3{}, false
	}
	startT := (overlapMin - aStart) / denominator
	endT := (overlapMax - aStart) / denominator
	start := Vec3{a0[0] + aDX*startT, a0[1] + (a1[1]-a0[1])*startT, a0[2] + aDZ*startT}
	end := Vec3{a0[0] + aDX*endT, a0[1] + (a1[1]-a0[1])*endT, a0[2] + aDZ*endT}
	return start, end, true
}

func navPathTransitionRequiresWaypoint(fromArea string, toArea string, linkKind string, start Vec3, end Vec3) bool {
	for _, area := range []string{fromArea, toArea, linkKind} {
		switch area {
		case NavTraversalRamp, NavTraversalStair, NavTraversalStep, NavTraversalDrop:
			return true
		}
	}
	return absNavFloat32(start[1]-end[1]) > 0.2
}
