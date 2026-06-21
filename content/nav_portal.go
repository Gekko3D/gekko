package content

import (
	"fmt"
	"math"
	"sort"
)

func applyNavTilePortals(tiles []*NavTileDef, profiles map[string]NavAgentProfileDef) {
	for _, tile := range tiles {
		if tile != nil {
			tile.Portals = nil
		}
	}
	for i := 0; i < len(tiles); i++ {
		for j := i + 1; j < len(tiles); j++ {
			a := tiles[i]
			b := tiles[j]
			dir, ok := navPortalTileDirection(a, b)
			if !ok {
				continue
			}
			profile := navPortalProfileForTile(a, profiles)
			applyNavTilePairPortals(a, b, dir, profile)
			applyNavTilePairPortals(b, a, TerrainChunkCoordDef{X: -dir.X, Y: -dir.Y, Z: -dir.Z}, profile)
		}
	}
	for _, tile := range tiles {
		if tile == nil {
			continue
		}
		sort.Slice(tile.Portals, func(i, j int) bool {
			return tile.Portals[i].ID < tile.Portals[j].ID
		})
	}
}

func navPortalProfileForTile(tile *NavTileDef, profiles map[string]NavAgentProfileDef) NavAgentProfileDef {
	if tile != nil {
		if profile, ok := profiles[tile.AgentProfileID]; ok {
			EnsureNavAgentProfileDefaults(&profile)
			return profile
		}
	}
	profile := DefaultHL1NavAgentProfile()
	EnsureNavAgentProfileDefaults(&profile)
	return profile
}

func navPortalTileDirection(a *NavTileDef, b *NavTileDef) (TerrainChunkCoordDef, bool) {
	if a == nil || b == nil || a.AgentProfileID != b.AgentProfileID {
		return TerrainChunkCoordDef{}, false
	}
	dx := b.Coord.X - a.Coord.X
	dy := b.Coord.Y - a.Coord.Y
	dz := b.Coord.Z - a.Coord.Z
	horizontal := absNavIntAsInt(dx) + absNavIntAsInt(dz)
	switch {
	case horizontal == 1 && absNavIntAsInt(dy) <= 1:
		return TerrainChunkCoordDef{X: dx, Y: dy, Z: dz}, true
	case horizontal == 0 && absNavIntAsInt(dy) == 1:
		return TerrainChunkCoordDef{X: dx, Y: dy, Z: dz}, true
	default:
		return TerrainChunkCoordDef{}, false
	}
}

func navPortalNeighborOffsets() []TerrainChunkCoordDef {
	return []TerrainChunkCoordDef{
		{X: 1}, {X: -1},
		{Y: 1}, {Y: -1},
		{Z: 1}, {Z: -1},
		{X: 1, Y: 1}, {X: 1, Y: -1},
		{X: -1, Y: 1}, {X: -1, Y: -1},
		{Z: 1, Y: 1}, {Z: 1, Y: -1},
		{Z: -1, Y: 1}, {Z: -1, Y: -1},
	}
}

func applyNavTilePairPortals(tileA *NavTileDef, tileB *NavTileDef, dir TerrainChunkCoordDef, profile NavAgentProfileDef) {
	if tileA == nil || tileB == nil {
		return
	}
	for _, polygonA := range tileA.Polygons {
		if polygonA.ID == "" {
			continue
		}
		for _, polygonB := range tileB.Polygons {
			if polygonB.ID == "" || !navPolygonsCanPortalAcrossTileBoundary(tileA, polygonA, tileB, polygonB, dir) {
				continue
			}
			segments := navPolygonsBoundaryPortalSegments(tileA, polygonA, tileB, polygonB, dir, profile)
			for i, segment := range segments {
				tileA.Portals = append(tileA.Portals, NavPortalDef{
					ID:            navPortalID(tileA.Coord, polygonA.ID, tileB.Coord, polygonB.ID, i),
					FromPolygonID: polygonA.ID,
					ToTileCoord:   tileB.Coord,
					ToPolygonID:   polygonB.ID,
					Start:         segment.Start,
					End:           segment.End,
					Area:          firstNonEmptyNavString(polygonA.Area, NavTraversalWalk),
				})
			}
		}
	}
}

type navBoundaryPortalSegment struct {
	Start Vec3
	End   Vec3
}

type navPortalBoundarySpan struct {
	Min float32
	Max float32
}

func navPolygonsCanPortalAcrossTileBoundary(tileA *NavTileDef, polygonA NavPolygonDef, tileB *NavTileDef, polygonB NavPolygonDef, dir TerrainChunkCoordDef) bool {
	if polygonA.ID != "" && polygonA.ID == polygonB.ID && navPolygonsSameSourceOverlapAcrossTiles(tileA, polygonA, tileB, polygonB, dir) {
		return true
	}
	a, ok := navPolygonBoundsForTile(tileA, polygonA)
	if !ok {
		return false
	}
	b, ok := navPolygonBoundsForTile(tileB, polygonB)
	if !ok {
		return false
	}
	const epsilon = float32(1e-4)
	switch {
	case dir.X == 1:
		return navPolygonTouchesPortalPlane(tileA, polygonA, a, tileA.BoundsMax[0], 0, epsilon) &&
			navPolygonTouchesPortalPlane(tileB, polygonB, b, tileB.BoundsMin[0], 0, epsilon) &&
			navAlmostEqual(tileA.BoundsMax[0], tileB.BoundsMin[0], epsilon) &&
			navRangesOverlapPositive(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	case dir.X == -1:
		return navPolygonTouchesPortalPlane(tileA, polygonA, a, tileA.BoundsMin[0], 0, epsilon) &&
			navPolygonTouchesPortalPlane(tileB, polygonB, b, tileB.BoundsMax[0], 0, epsilon) &&
			navAlmostEqual(tileA.BoundsMin[0], tileB.BoundsMax[0], epsilon) &&
			navRangesOverlapPositive(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	case dir.Y == 1 || dir.Y == -1:
		return navPolygonsTouchAcrossTileBoundary(tileA, polygonA, tileB, polygonB, dir)
	case dir.Z == 1:
		return navPolygonTouchesPortalPlane(tileA, polygonA, a, tileA.BoundsMax[2], 2, epsilon) &&
			navPolygonTouchesPortalPlane(tileB, polygonB, b, tileB.BoundsMin[2], 2, epsilon) &&
			navAlmostEqual(tileA.BoundsMax[2], tileB.BoundsMin[2], epsilon) &&
			navRangesOverlapPositive(a.min[0], a.max[0], b.min[0], b.max[0], epsilon)
	case dir.Z == -1:
		return navPolygonTouchesPortalPlane(tileA, polygonA, a, tileA.BoundsMin[2], 2, epsilon) &&
			navPolygonTouchesPortalPlane(tileB, polygonB, b, tileB.BoundsMax[2], 2, epsilon) &&
			navAlmostEqual(tileA.BoundsMin[2], tileB.BoundsMax[2], epsilon) &&
			navRangesOverlapPositive(a.min[0], a.max[0], b.min[0], b.max[0], epsilon)
	default:
		return false
	}
}

func navPolygonTouchesPortalPlane(tile *NavTileDef, polygon NavPolygonDef, bounds navPolygonBounds, plane float32, axis int, epsilon float32) bool {
	if navPolygonBoundsCrossesPlane(bounds, plane, axis, epsilon) {
		return true
	}
	if polygon.ID == "" {
		return false
	}
	spans, ok := navPolygonBorderSpansAtPlane(tile, polygon.ID, axis, plane)
	return ok && len(spans) > 0
}

func navPolygonsBoundaryPortalSegments(tileA *NavTileDef, polygonA NavPolygonDef, tileB *NavTileDef, polygonB NavPolygonDef, dir TerrainChunkCoordDef, profile NavAgentProfileDef) []navBoundaryPortalSegment {
	a, ok := navPolygonBoundsForTile(tileA, polygonA)
	if !ok {
		return nil
	}
	b, ok := navPolygonBoundsForTile(tileB, polygonB)
	if !ok {
		return nil
	}
	const epsilon = float32(1e-4)
	switch {
	case dir.X == 1 || dir.X == -1:
		return navPolygonsHorizontalBoundaryPortalSegments(tileA, polygonA, tileB, polygonB, dir, profile)
	case dir.Z == 1 || dir.Z == -1:
		return navPolygonsHorizontalBoundaryPortalSegments(tileA, polygonA, tileB, polygonB, dir, profile)
	case dir.Y == 1 || dir.Y == -1:
		y := tileA.BoundsMax[1]
		if dir.Y == -1 {
			y = tileA.BoundsMin[1]
		}
		x0 := maxNavFloat32(a.min[0], b.min[0])
		x1 := minNavFloat32(a.max[0], b.max[0])
		z0 := maxNavFloat32(a.min[2], b.min[2])
		z1 := minNavFloat32(a.max[2], b.max[2])
		if x1-x0 <= epsilon || z1-z0 <= epsilon {
			return nil
		}
		return []navBoundaryPortalSegment{{Start: Vec3{x0, y, z0}, End: Vec3{x1, y, z1}}}
	default:
		return nil
	}
}

func navPolygonsHorizontalBoundaryPortalSegments(tileA *NavTileDef, polygonA NavPolygonDef, tileB *NavTileDef, polygonB NavPolygonDef, dir TerrainChunkCoordDef, profile NavAgentProfileDef) []navBoundaryPortalSegment {
	normalAxis, spanAxis, coord, ok := navPortalHorizontalBoundaryPlane(tileA, dir)
	if !ok {
		return nil
	}
	_, _, neighborCoord, ok := navPortalHorizontalBoundaryPlane(tileB, TerrainChunkCoordDef{X: -dir.X, Y: -dir.Y, Z: -dir.Z})
	if !ok || !navAlmostEqual(coord, neighborCoord, 1e-3) {
		return nil
	}
	aSpans := navPolygonBoundarySpansAtPlane(tileA, polygonA, normalAxis, spanAxis, coord)
	bSpans := navPolygonBoundarySpansAtPlane(tileB, polygonB, normalAxis, spanAxis, coord)
	if borderASpans, aOK := navPolygonBorderSpansAtPlane(tileA, polygonA.ID, normalAxis, coord); aOK {
		if borderBSpans, bOK := navPolygonBorderSpansAtPlane(tileB, polygonB.ID, normalAxis, coord); bOK {
			aSpans = borderASpans
			bSpans = borderBSpans
		}
	}
	if len(aSpans) == 0 || len(bSpans) == 0 {
		return nil
	}
	segments := make([]navBoundaryPortalSegment, 0)
	for _, aSpan := range aSpans {
		for _, bSpan := range bSpans {
			overlapMin := maxNavFloat32(aSpan.Min, bSpan.Min)
			overlapMax := minNavFloat32(aSpan.Max, bSpan.Max)
			if overlapMax-overlapMin <= 1e-4 {
				continue
			}
			area := navPortalTraversalArea(polygonA, polygonB)
			segments = append(segments, navPortalSegmentsAlongInterval(overlapMin, overlapMax, profile, area, func(span float32) (Vec3, bool) {
				x, z := navPortalHorizontalBoundaryXZ(normalAxis, coord, span)
				height, ok := navPortalHeightAt(tileA, polygonA, tileB, polygonB, x, z, profile)
				if !ok {
					return Vec3{}, false
				}
				return Vec3{x, height, z}, true
			})...)
		}
	}
	return segments
}

func navPolygonBorderSpansAtPlane(tile *NavTileDef, polygonID string, normalAxis int, coord float32) ([]navPortalBoundarySpan, bool) {
	if tile == nil || polygonID == "" || len(tile.BorderSpans) == 0 {
		return nil, false
	}
	edge, ok := navBorderEdgeForPlane(tile, normalAxis, coord)
	if !ok {
		return nil, false
	}
	spans := make([]navPortalBoundarySpan, 0)
	for _, span := range tile.BorderSpans {
		if span.PolygonID != polygonID || span.Edge != edge {
			continue
		}
		spans = appendNavPortalBoundarySpan(spans, span.Min, span.Max)
	}
	if len(spans) == 0 {
		return nil, false
	}
	return navPortalMergeBoundarySpans(spans), true
}

func navBorderEdgeForPlane(tile *NavTileDef, normalAxis int, coord float32) (string, bool) {
	if tile == nil {
		return "", false
	}
	const epsilon = float32(1e-3)
	switch normalAxis {
	case 0:
		if navAlmostEqual(coord, tile.BoundsMin[0], epsilon) {
			return NavBorderEdgeMinX, true
		}
		if navAlmostEqual(coord, tile.BoundsMax[0], epsilon) {
			return NavBorderEdgeMaxX, true
		}
	case 2:
		if navAlmostEqual(coord, tile.BoundsMin[2], epsilon) {
			return NavBorderEdgeMinZ, true
		}
		if navAlmostEqual(coord, tile.BoundsMax[2], epsilon) {
			return NavBorderEdgeMaxZ, true
		}
	}
	return "", false
}

func navPortalHorizontalBoundaryPlane(tile *NavTileDef, dir TerrainChunkCoordDef) (int, int, float32, bool) {
	if tile == nil {
		return 0, 0, 0, false
	}
	switch {
	case dir.X > 0:
		return 0, 2, tile.BoundsMax[0], true
	case dir.X < 0:
		return 0, 2, tile.BoundsMin[0], true
	case dir.Z > 0:
		return 2, 0, tile.BoundsMax[2], true
	case dir.Z < 0:
		return 2, 0, tile.BoundsMin[2], true
	default:
		return 0, 0, 0, false
	}
}

func navPortalHorizontalBoundaryXZ(normalAxis int, coord float32, span float32) (float32, float32) {
	if normalAxis == 0 {
		return coord, span
	}
	return span, coord
}

func navPolygonBoundarySpansAtPlane(tile *NavTileDef, polygon NavPolygonDef, normalAxis int, spanAxis int, coord float32) []navPortalBoundarySpan {
	if tile == nil || len(polygon.Vertices) < 2 {
		return nil
	}
	const epsilon = float32(1e-4)
	spans := make([]navPortalBoundarySpan, 0)
	intersections := make([]float32, 0)
	strictIntersections := make([]float32, 0)
	for edgeIndex := range polygon.Vertices {
		a, b, ok := navPolygonEdge(tile, polygon, edgeIndex)
		if !ok {
			continue
		}
		aDist := a[normalAxis] - coord
		bDist := b[normalAxis] - coord
		aOn := absNavFloat32(aDist) <= epsilon
		bOn := absNavFloat32(bDist) <= epsilon
		if aOn && bOn {
			spans = appendNavPortalBoundarySpan(spans, a[spanAxis], b[spanAxis])
			continue
		}
		crosses := navPortalEdgeCrossesPlane(aDist, bDist, epsilon)
		strictlyCrosses := navPortalEdgeStrictlyCrossesPlane(aDist, bDist, epsilon)
		if !crosses {
			continue
		}
		denominator := b[normalAxis] - a[normalAxis]
		if absNavFloat32(denominator) <= epsilon {
			continue
		}
		t := (coord - a[normalAxis]) / denominator
		if t < -epsilon || t > 1+epsilon {
			continue
		}
		t = clampNavFloat32(t, 0, 1)
		span := a[spanAxis] + (b[spanAxis]-a[spanAxis])*t
		intersections = append(intersections, span)
		if strictlyCrosses {
			strictIntersections = append(strictIntersections, span)
		}
	}
	if len(strictIntersections) >= 2 {
		spans = appendNavPortalBoundarySpansFromIntersections(spans, strictIntersections, epsilon)
	} else if len(spans) == 0 && len(intersections) >= 2 {
		spans = appendNavPortalBoundarySpansFromIntersections(spans, intersections, epsilon)
	}
	return navPortalMergeBoundarySpans(spans)
}

func appendNavPortalBoundarySpansFromIntersections(spans []navPortalBoundarySpan, intersections []float32, epsilon float32) []navPortalBoundarySpan {
	sort.Slice(intersections, func(i, j int) bool {
		return intersections[i] < intersections[j]
	})
	intersections = navPortalDedupeFloat32(intersections, epsilon)
	for i := 0; i+1 < len(intersections); i += 2 {
		spans = appendNavPortalBoundarySpan(spans, intersections[i], intersections[i+1])
	}
	return spans
}

func navPortalEdgeCrossesPlane(aDist float32, bDist float32, epsilon float32) bool {
	return (aDist < -epsilon && bDist > epsilon) ||
		(aDist > epsilon && bDist < -epsilon) ||
		absNavFloat32(aDist) <= epsilon ||
		absNavFloat32(bDist) <= epsilon
}

func navPortalEdgeStrictlyCrossesPlane(aDist float32, bDist float32, epsilon float32) bool {
	return (aDist < -epsilon && bDist > epsilon) ||
		(aDist > epsilon && bDist < -epsilon)
}

func appendNavPortalBoundarySpan(spans []navPortalBoundarySpan, a float32, b float32) []navPortalBoundarySpan {
	minValue := minNavFloat32(a, b)
	maxValue := maxNavFloat32(a, b)
	if maxValue-minValue <= 1e-4 {
		return spans
	}
	return append(spans, navPortalBoundarySpan{Min: minValue, Max: maxValue})
}

func navPortalMergeBoundarySpans(spans []navPortalBoundarySpan) []navPortalBoundarySpan {
	if len(spans) < 2 {
		return spans
	}
	sort.Slice(spans, func(i, j int) bool {
		if navAlmostEqual(spans[i].Min, spans[j].Min, 1e-4) {
			return spans[i].Max < spans[j].Max
		}
		return spans[i].Min < spans[j].Min
	})
	out := make([]navPortalBoundarySpan, 0, len(spans))
	for _, span := range spans {
		if len(out) == 0 || span.Min > out[len(out)-1].Max+1e-4 {
			out = append(out, span)
			continue
		}
		if span.Max > out[len(out)-1].Max {
			out[len(out)-1].Max = span.Max
		}
	}
	return out
}

func navPortalDedupeFloat32(values []float32, epsilon float32) []float32 {
	if len(values) < 2 {
		return values
	}
	out := values[:0]
	for _, value := range values {
		if len(out) > 0 && navAlmostEqual(out[len(out)-1], value, epsilon) {
			continue
		}
		out = append(out, value)
	}
	return out
}

func navPortalSegmentsAlongInterval(minValue, maxValue float32, profile NavAgentProfileDef, area string, pointAt func(float32) (Vec3, bool)) []navBoundaryPortalSegment {
	if pointAt == nil {
		return nil
	}
	length := maxValue - minValue
	if length <= 1e-4 {
		return nil
	}
	sampleStep := navPortalSampleStep(profile)
	samples := int(math.Ceil(float64(length / sampleStep)))
	if samples < 1 {
		samples = 1
	}
	if samples > 256 {
		samples = 256
	}
	interval := length / float32(samples)
	segments := make([]navBoundaryPortalSegment, 0, 1)
	inRun := false
	runStart := minValue
	lastPoint := Vec3{}
	hasLastPoint := false
	for i := 0; i < samples; i++ {
		cellStart := minValue + interval*float32(i)
		cellEnd := minValue + interval*float32(i+1)
		mid := (cellStart + cellEnd) * 0.5
		point, ok := pointAt(mid)
		if ok && inRun && hasLastPoint && !navPortalSamplePointsConnect(lastPoint, point, profile, area) {
			segments = appendNavPortalSegmentForRun(segments, runStart, cellStart, pointAt)
			inRun = false
			hasLastPoint = false
		}
		if ok && !inRun {
			inRun = true
			runStart = cellStart
		}
		if ok {
			lastPoint = point
			hasLastPoint = true
		}
		if !ok && inRun {
			segments = appendNavPortalSegmentForRun(segments, runStart, cellStart, pointAt)
			inRun = false
			hasLastPoint = false
		}
	}
	if inRun {
		segments = appendNavPortalSegmentForRun(segments, runStart, maxValue, pointAt)
	}
	return segments
}

func appendNavPortalSegmentForRun(segments []navBoundaryPortalSegment, startValue, endValue float32, pointAt func(float32) (Vec3, bool)) []navBoundaryPortalSegment {
	if endValue-startValue <= 1e-4 {
		return segments
	}
	start, startOK := pointAt(startValue)
	end, endOK := pointAt(endValue)
	if !startOK {
		start, startOK = pointAt(startValue + (endValue-startValue)*0.05)
	}
	if !endOK {
		end, endOK = pointAt(endValue - (endValue-startValue)*0.05)
	}
	if !startOK || !endOK || navVec3Distance(start, end) <= 1e-4 {
		return segments
	}
	return append(segments, navBoundaryPortalSegment{Start: start, End: end})
}

func navPortalSamplePointsConnect(a Vec3, b Vec3, profile NavAgentProfileDef, area string) bool {
	EnsureNavAgentProfileDefaults(&profile)
	delta := absNavFloat32(a[1] - b[1])
	const epsilon = float32(1e-4)
	if delta > profile.StepHeight+epsilon {
		return false
	}
	if navTraversalAreaUsesStepContinuity(area) {
		return true
	}
	if profile.MaxSlopeDegrees <= 0 {
		return true
	}
	dx := b[0] - a[0]
	dz := b[2] - a[2]
	horizontal := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if horizontal <= epsilon {
		return delta <= epsilon
	}
	maxRise := float32(math.Tan(float64(profile.MaxSlopeDegrees)*math.Pi/180.0)) * horizontal
	return delta <= maxRise+epsilon
}

func navPortalTraversalArea(a NavPolygonDef, b NavPolygonDef) string {
	if navTraversalAreaUsesStepContinuity(a.Area) {
		return a.Area
	}
	if navTraversalAreaUsesStepContinuity(b.Area) {
		return b.Area
	}
	return firstNonEmptyNavString(a.Area, b.Area, NavTraversalWalk)
}

func navTraversalAreaUsesStepContinuity(area string) bool {
	return area == NavTraversalStair || area == NavTraversalStep
}

func navPortalSampleStep(profile NavAgentProfileDef) float32 {
	EnsureNavAgentProfileDefaults(&profile)
	if profile.NavCellSize > 0 {
		return profile.NavCellSize
	}
	return DefaultNavCellSize
}

func navPortalHeightAt(tileA *NavTileDef, polygonA NavPolygonDef, tileB *NavTileDef, polygonB NavPolygonDef, x, z float32, profile NavAgentProfileDef) (float32, bool) {
	point := Vec3{x, 0, z}
	aHeight, aOK := navPortalPolygonHeightAt(tileA, polygonA, point)
	bHeight, bOK := navPortalPolygonHeightAt(tileB, polygonB, point)
	if !aOK || !bOK || !navPortalHeightsConnect(aHeight, bHeight, profile) {
		return 0, false
	}
	return (aHeight + bHeight) * 0.5, true
}

func navPortalPolygonHeightAt(tile *NavTileDef, polygon NavPolygonDef, point Vec3) (float32, bool) {
	if height, ok := navPolygonHeightAtXZ(tile, polygon, point); ok {
		return height, true
	}
	bounds, ok := navPolygonBoundsForTile(tile, polygon)
	if ok && navAlmostEqual(bounds.min[1], bounds.max[1], 1e-4) && navPolygonContainsXZ(tile, polygon, point) {
		return bounds.min[1], true
	}
	return 0, false
}

func navPortalHeightsConnect(aHeight, bHeight float32, profile NavAgentProfileDef) bool {
	EnsureNavAgentProfileDefaults(&profile)
	delta := absNavFloat32(aHeight - bHeight)
	const epsilon = float32(1e-4)
	return delta <= profile.StepHeight+epsilon
}

func navPortalID(from TerrainChunkCoordDef, fromPolygonID string, to TerrainChunkCoordDef, toPolygonID string, segmentIndex int) string {
	return fmt.Sprintf("portal:%s:%s->%s:%s:%d", TerrainChunkKey(from), fromPolygonID, TerrainChunkKey(to), toPolygonID, segmentIndex)
}

func navPolygonsPortalSegment(tileA *NavTileDef, a NavPolygonDef, tileB *NavTileDef, b NavPolygonDef) (Vec3, Vec3, bool) {
	if tileA == nil || tileB == nil || len(a.Vertices) < 2 || len(b.Vertices) < 2 {
		return Vec3{}, Vec3{}, false
	}
	for ai := range a.Vertices {
		a0, a1, ok := navPolygonEdge(tileA, a, ai)
		if !ok {
			continue
		}
		for bi := range b.Vertices {
			b0, b1, ok := navPolygonEdge(tileB, b, bi)
			if !ok {
				continue
			}
			start, end, ok := navEdgeOverlapSegment(a0, a1, b0, b1)
			if ok {
				return start, end, true
			}
		}
	}
	return Vec3{}, Vec3{}, false
}

func navEdgeOverlapSegment(a0 Vec3, a1 Vec3, b0 Vec3, b1 Vec3) (Vec3, Vec3, bool) {
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
	if absNavFloat32(aEnd-aStart) <= epsilon {
		return Vec3{}, Vec3{}, false
	}

	overlapMin := maxNavFloat32(minNavFloat32(aStart, aEnd), minNavFloat32(bStart, bEnd))
	overlapMax := minNavFloat32(maxNavFloat32(aStart, aEnd), maxNavFloat32(bStart, bEnd))
	if overlapMax-overlapMin <= epsilon {
		return Vec3{}, Vec3{}, false
	}

	start := navOverlapPoint(a0, a1, b0, b1, useX, overlapMin)
	end := navOverlapPoint(a0, a1, b0, b1, useX, overlapMax)
	if navVec3Distance(start, end) <= epsilon {
		return Vec3{}, Vec3{}, false
	}
	return start, end, true
}

func navOverlapPoint(a0 Vec3, a1 Vec3, b0 Vec3, b1 Vec3, useX bool, position float32) Vec3 {
	aPoint := navPointOnEdge(a0, a1, useX, position)
	bPoint := navPointOnEdge(b0, b1, useX, position)
	return Vec3{
		(aPoint[0] + bPoint[0]) * 0.5,
		(aPoint[1] + bPoint[1]) * 0.5,
		(aPoint[2] + bPoint[2]) * 0.5,
	}
}

func navPointOnEdge(a Vec3, b Vec3, useX bool, position float32) Vec3 {
	start := a[2]
	end := b[2]
	if useX {
		start = a[0]
		end = b[0]
	}
	t := float32(0)
	if denominator := end - start; absNavFloat32(denominator) > 1e-4 {
		t = (position - start) / denominator
	}
	return Vec3{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
	}
}

func navPortalSegmentTouchesTileBoundary(start Vec3, end Vec3, tileA *NavTileDef, tileB *NavTileDef, dir TerrainChunkCoordDef) bool {
	const epsilon = float32(1e-3)
	switch {
	case dir.X == 1:
		return navAlmostEqual(start[0], tileA.BoundsMax[0], epsilon) && navAlmostEqual(end[0], tileA.BoundsMax[0], epsilon) && navAlmostEqual(start[0], tileB.BoundsMin[0], epsilon) && navAlmostEqual(end[0], tileB.BoundsMin[0], epsilon)
	case dir.X == -1:
		return navAlmostEqual(start[0], tileA.BoundsMin[0], epsilon) && navAlmostEqual(end[0], tileA.BoundsMin[0], epsilon) && navAlmostEqual(start[0], tileB.BoundsMax[0], epsilon) && navAlmostEqual(end[0], tileB.BoundsMax[0], epsilon)
	case dir.Y == 1:
		return navAlmostEqual(start[1], tileA.BoundsMax[1], epsilon) && navAlmostEqual(end[1], tileA.BoundsMax[1], epsilon) && navAlmostEqual(start[1], tileB.BoundsMin[1], epsilon) && navAlmostEqual(end[1], tileB.BoundsMin[1], epsilon)
	case dir.Y == -1:
		return navAlmostEqual(start[1], tileA.BoundsMin[1], epsilon) && navAlmostEqual(end[1], tileA.BoundsMin[1], epsilon) && navAlmostEqual(start[1], tileB.BoundsMax[1], epsilon) && navAlmostEqual(end[1], tileB.BoundsMax[1], epsilon)
	case dir.Z == 1:
		return navAlmostEqual(start[2], tileA.BoundsMax[2], epsilon) && navAlmostEqual(end[2], tileA.BoundsMax[2], epsilon) && navAlmostEqual(start[2], tileB.BoundsMin[2], epsilon) && navAlmostEqual(end[2], tileB.BoundsMin[2], epsilon)
	case dir.Z == -1:
		return navAlmostEqual(start[2], tileA.BoundsMin[2], epsilon) && navAlmostEqual(end[2], tileA.BoundsMin[2], epsilon) && navAlmostEqual(start[2], tileB.BoundsMax[2], epsilon) && navAlmostEqual(end[2], tileB.BoundsMax[2], epsilon)
	default:
		return false
	}
}

func navVec3Distance(a Vec3, b Vec3) float32 {
	dx := a[0] - b[0]
	dy := a[1] - b[1]
	dz := a[2] - b[2]
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}
