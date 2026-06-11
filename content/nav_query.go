package content

import (
	"container/heap"
	"fmt"
	"math"
)

type NavTileQuery struct {
	Tile           *NavTileDef
	polygonByID    map[string]int
	polygonCenters map[string]Vec3
	polygonLinks   map[string][]string
}

type NavTilePathResult struct {
	Found      bool
	PolygonIDs []string
	Waypoints  []Vec3
}

type NavPolygonSnapResult struct {
	Polygon            NavPolygonDef
	Point              Vec3
	Distance           float32
	HorizontalDistance float32
	VerticalDistance   float32
}

func NewNavTileQuery(tile *NavTileDef) (*NavTileQuery, error) {
	if tile == nil {
		return nil, fmt.Errorf("nav tile is nil")
	}
	if validation := ValidateNavTile(tile); validation.HasErrors() {
		return nil, validation
	}
	query := &NavTileQuery{
		Tile:           tile,
		polygonByID:    make(map[string]int, len(tile.Polygons)),
		polygonCenters: make(map[string]Vec3, len(tile.Polygons)),
		polygonLinks:   make(map[string][]string, len(tile.Polygons)),
	}
	for i, polygon := range tile.Polygons {
		query.polygonByID[polygon.ID] = i
		query.polygonCenters[polygon.ID] = navPolygonCenter(tile, polygon)
	}
	query.buildInferredPolygonLinks()
	return query, nil
}

func (q *NavTileQuery) FindPolygonAt(point Vec3) (NavPolygonDef, bool) {
	if q == nil || q.Tile == nil {
		return NavPolygonDef{}, false
	}
	bestIndex := -1
	bestDistance := float32(0)
	for i, polygon := range q.Tile.Polygons {
		if !navPolygonContainsXZ(q.Tile, polygon, point) {
			continue
		}
		height, ok := navPolygonHeightAtXZ(q.Tile, polygon, point)
		if !ok {
			center := q.polygonCenters[polygon.ID]
			height = center[1]
		}
		distance := absNavFloat32(point[1] - height)
		if bestIndex == -1 || distance < bestDistance {
			bestIndex = i
			bestDistance = distance
		}
	}
	if bestIndex == -1 {
		return NavPolygonDef{}, false
	}
	return q.Tile.Polygons[bestIndex], true
}

func (q *NavTileQuery) SnapPointToPolygon(point Vec3, polygonID string) (NavPolygonSnapResult, bool) {
	polygon, ok := q.FindPolygonByID(polygonID)
	if !ok {
		return NavPolygonSnapResult{}, false
	}
	return navSnapPointToPolygon(q.Tile, q.polygonCenters, polygon, point)
}

func (q *NavTileQuery) FindNearestPolygon(point Vec3, maxDistance float32) (NavPolygonSnapResult, bool) {
	if q == nil || q.Tile == nil || maxDistance < 0 {
		return NavPolygonSnapResult{}, false
	}
	best := NavPolygonSnapResult{}
	found := false
	for _, polygon := range q.Tile.Polygons {
		snap, ok := navSnapPointToPolygon(q.Tile, q.polygonCenters, polygon, point)
		if !ok {
			continue
		}
		if snap.Distance > maxDistance {
			continue
		}
		if !found || snap.Distance < best.Distance {
			best = snap
			found = true
		}
	}
	return best, found
}

func (q *NavTileQuery) FindPolygonByID(id string) (NavPolygonDef, bool) {
	if q == nil || q.Tile == nil {
		return NavPolygonDef{}, false
	}
	index, ok := q.polygonByID[id]
	if !ok {
		return NavPolygonDef{}, false
	}
	return q.Tile.Polygons[index], true
}

func (q *NavTileQuery) PolygonCenter(id string) (Vec3, bool) {
	if q == nil {
		return Vec3{}, false
	}
	center, ok := q.polygonCenters[id]
	return center, ok
}

func (q *NavTileQuery) FindPath(start Vec3, end Vec3) (NavTilePathResult, error) {
	startPolygon, ok := q.FindPolygonAt(start)
	if !ok {
		return NavTilePathResult{}, nil
	}
	endPolygon, ok := q.FindPolygonAt(end)
	if !ok {
		return NavTilePathResult{}, nil
	}
	return q.findPathBetweenPolygons(startPolygon.ID, endPolygon.ID, start, end, true)
}

func (q *NavTileQuery) FindPathBetweenPolygons(startID string, endID string) (NavTilePathResult, error) {
	start := Vec3{}
	end := Vec3{}
	if q != nil {
		start = q.polygonCenters[startID]
		end = q.polygonCenters[endID]
	}
	return q.findPathBetweenPolygons(startID, endID, start, end, false)
}

func (q *NavTileQuery) findPathBetweenPolygons(startID string, endID string, start Vec3, end Vec3, useEndpoints bool) (NavTilePathResult, error) {
	if q == nil || q.Tile == nil {
		return NavTilePathResult{}, fmt.Errorf("nav tile query is nil")
	}
	if _, ok := q.polygonByID[startID]; !ok {
		return NavTilePathResult{}, nil
	}
	if _, ok := q.polygonByID[endID]; !ok {
		return NavTilePathResult{}, nil
	}
	if startID == endID {
		waypoints := []Vec3{q.polygonCenters[startID]}
		if useEndpoints {
			waypoints = navDedupePathWaypoints([]Vec3{start, end})
		}
		return NavTilePathResult{
			Found:      true,
			PolygonIDs: []string{startID},
			Waypoints:  waypoints,
		}, nil
	}
	costs := map[string]float32{startID: 0}
	previous := map[string]string{}
	queue := &navTilePathPriorityQueue{}
	heap.Push(queue, &navTilePathQueueItem{PolygonID: startID})
	for queue.Len() > 0 {
		item := heap.Pop(queue).(*navTilePathQueueItem)
		current := item.PolygonID
		if item.Cost > costs[current]+1e-5 {
			continue
		}
		if current == endID {
			return q.buildPathResult(startID, endID, previous, start, end, useEndpoints), nil
		}
		polygon := q.Tile.Polygons[q.polygonByID[current]]
		neighbors := append([]string(nil), q.polygonLinks[polygon.ID]...)
		for _, neighbor := range q.offMeshNeighborPolygons(current) {
			neighbors = appendUniqueNavString(neighbors, neighbor)
		}
		for _, neighbor := range neighbors {
			if _, ok := q.polygonByID[neighbor]; !ok {
				continue
			}
			nextCost := costs[current] + navVec3Distance(q.pathCostPoint(current, startID, endID, start, end, useEndpoints), q.pathCostPoint(neighbor, startID, endID, start, end, useEndpoints))
			if existing, ok := costs[neighbor]; ok && existing <= nextCost {
				continue
			}
			costs[neighbor] = nextCost
			previous[neighbor] = current
			heap.Push(queue, &navTilePathQueueItem{PolygonID: neighbor, Cost: nextCost})
		}
	}
	return NavTilePathResult{}, nil
}

func (q *NavTileQuery) buildInferredPolygonLinks() {
	if q == nil || q.Tile == nil {
		return
	}
	for _, polygon := range q.Tile.Polygons {
		q.polygonLinks[polygon.ID] = append([]string(nil), polygon.Neighbors...)
	}
	profile := navPortalProfileForTile(q.Tile, nil)
	for i := 0; i < len(q.Tile.Polygons); i++ {
		for j := i + 1; j < len(q.Tile.Polygons); j++ {
			a := q.Tile.Polygons[i]
			b := q.Tile.Polygons[j]
			if !navPolygonsShareEdge(q.Tile, a, b) && !navPolygonsConnectAcrossStep(q.Tile, a, b, profile) {
				continue
			}
			q.polygonLinks[a.ID] = appendUniqueNavString(q.polygonLinks[a.ID], b.ID)
			q.polygonLinks[b.ID] = appendUniqueNavString(q.polygonLinks[b.ID], a.ID)
		}
	}
}

func (q *NavTileQuery) pathCostPoint(polygonID string, startID string, endID string, start Vec3, end Vec3, useEndpoints bool) Vec3 {
	if useEndpoints {
		if polygonID == startID {
			return start
		}
		if polygonID == endID {
			return end
		}
	}
	return q.polygonCenters[polygonID]
}

func (q *NavTileQuery) offMeshNeighborPolygons(polygonID string) []string {
	if q == nil || q.Tile == nil || polygonID == "" || len(q.Tile.OffMeshLinks) == 0 {
		return nil
	}
	out := make([]string, 0)
	for _, link := range q.Tile.OffMeshLinks {
		if link.FromPolygonID != "" {
			if link.FromPolygonID != polygonID {
				continue
			}
			if link.ToTileCoord == q.Tile.Coord && link.ToPolygonID != "" {
				out = appendUniqueNavString(out, link.ToPolygonID)
				continue
			}
		}
		startPolygon, startOK := q.FindPolygonAt(link.Start)
		endPolygon, endOK := q.FindPolygonAt(link.End)
		if startOK && startPolygon.ID == polygonID && endOK {
			out = appendUniqueNavString(out, endPolygon.ID)
		}
		if link.Bidirectional && endOK && endPolygon.ID == polygonID && startOK {
			out = appendUniqueNavString(out, startPolygon.ID)
		}
	}
	return out
}

func (q *NavTileQuery) buildPathResult(startID string, endID string, previous map[string]string, start Vec3, end Vec3, useEndpoints bool) NavTilePathResult {
	reversed := []string{endID}
	for current := endID; current != startID; {
		current = previous[current]
		reversed = append(reversed, current)
	}
	ids := make([]string, len(reversed))
	for i := range reversed {
		ids[i] = reversed[len(reversed)-1-i]
	}
	waypoints := make([]Vec3, 0, len(ids))
	if useEndpoints {
		segments := q.pathPortalSegments(ids)
		if len(segments) == len(ids)-1 {
			waypoints = navBuildStringPulledWaypoints(start, end, segments)
		}
	}
	if len(waypoints) == 0 {
		for _, id := range ids {
			waypoints = append(waypoints, q.polygonCenters[id])
		}
	}
	return NavTilePathResult{
		Found:      true,
		PolygonIDs: ids,
		Waypoints:  waypoints,
	}
}

func (q *NavTileQuery) pathPortalSegments(ids []string) []navPathPortalSegment {
	if q == nil || q.Tile == nil || len(ids) < 2 {
		return nil
	}
	segments := make([]navPathPortalSegment, 0, len(ids)-1)
	for i := 1; i < len(ids); i++ {
		a, aOK := q.FindPolygonByID(ids[i-1])
		b, bOK := q.FindPolygonByID(ids[i])
		if !aOK || !bOK {
			return nil
		}
		segment, ok := navSameTilePortalSegment(q.Tile, a, b)
		if !ok {
			return nil
		}
		segments = append(segments, segment)
	}
	return segments
}

type navTilePathQueueItem struct {
	PolygonID string
	Cost      float32
	index     int
}

type navTilePathPriorityQueue []*navTilePathQueueItem

func (q navTilePathPriorityQueue) Len() int { return len(q) }

func (q navTilePathPriorityQueue) Less(i, j int) bool { return q[i].Cost < q[j].Cost }

func (q navTilePathPriorityQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *navTilePathPriorityQueue) Push(x any) {
	item := x.(*navTilePathQueueItem)
	item.index = len(*q)
	*q = append(*q, item)
}

func (q *navTilePathPriorityQueue) Pop() any {
	old := *q
	item := old[len(old)-1]
	old[len(old)-1] = nil
	item.index = -1
	*q = old[:len(old)-1]
	return item
}

func navPolygonCenter(tile *NavTileDef, polygon NavPolygonDef) Vec3 {
	if tile == nil || len(polygon.Vertices) == 0 {
		return Vec3{}
	}
	var out Vec3
	count := float32(0)
	for _, index := range polygon.Vertices {
		if index < 0 || index >= len(tile.Vertices) {
			continue
		}
		vertex := tile.Vertices[index]
		out[0] += vertex[0]
		out[1] += vertex[1]
		out[2] += vertex[2]
		count++
	}
	if count == 0 {
		return Vec3{}
	}
	return Vec3{out[0] / count, out[1] / count, out[2] / count}
}

func navSnapPointToPolygon(tile *NavTileDef, centers map[string]Vec3, polygon NavPolygonDef, point Vec3) (NavPolygonSnapResult, bool) {
	closest, horizontalDistance, ok := navPolygonClosestPointXZ(tile, polygon, point)
	if !ok {
		return NavPolygonSnapResult{}, false
	}
	height, ok := navPolygonHeightAtXZ(tile, polygon, Vec3{closest[0], point[1], closest[2]})
	if !ok {
		center, centerOK := centers[polygon.ID]
		if !centerOK {
			center = navPolygonCenter(tile, polygon)
		}
		height = center[1]
	}
	snapped := Vec3{closest[0], height, closest[2]}
	verticalDistance := absNavFloat32(point[1] - height)
	distance := float32(math.Sqrt(float64(horizontalDistance*horizontalDistance + verticalDistance*verticalDistance)))
	return NavPolygonSnapResult{
		Polygon:            polygon,
		Point:              snapped,
		Distance:           distance,
		HorizontalDistance: horizontalDistance,
		VerticalDistance:   verticalDistance,
	}, true
}

func navPolygonClosestPointXZ(tile *NavTileDef, polygon NavPolygonDef, point Vec3) (Vec3, float32, bool) {
	if tile == nil || len(polygon.Vertices) < 3 {
		return Vec3{}, 0, false
	}
	if navPolygonContainsXZ(tile, polygon, point) {
		return Vec3{point[0], 0, point[2]}, 0, true
	}
	best := Vec3{}
	bestDistanceSq := float32(0)
	found := false
	for i := range polygon.Vertices {
		aIndex := polygon.Vertices[i]
		bIndex := polygon.Vertices[(i+1)%len(polygon.Vertices)]
		if aIndex < 0 || aIndex >= len(tile.Vertices) || bIndex < 0 || bIndex >= len(tile.Vertices) {
			return Vec3{}, 0, false
		}
		candidate := navClosestPointOnSegmentXZ(point, tile.Vertices[aIndex], tile.Vertices[bIndex])
		dx := point[0] - candidate[0]
		dz := point[2] - candidate[2]
		distanceSq := dx*dx + dz*dz
		if !found || distanceSq < bestDistanceSq {
			best = candidate
			bestDistanceSq = distanceSq
			found = true
		}
	}
	if !found {
		return Vec3{}, 0, false
	}
	return best, float32(math.Sqrt(float64(bestDistanceSq))), true
}

func navClosestPointOnSegmentXZ(point Vec3, a Vec3, b Vec3) Vec3 {
	abX := b[0] - a[0]
	abZ := b[2] - a[2]
	lengthSq := abX*abX + abZ*abZ
	if lengthSq <= 1e-8 {
		return Vec3{a[0], 0, a[2]}
	}
	t := ((point[0]-a[0])*abX + (point[2]-a[2])*abZ) / lengthSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return Vec3{a[0] + abX*t, 0, a[2] + abZ*t}
}

func navPolygonContainsXZ(tile *NavTileDef, polygon NavPolygonDef, point Vec3) bool {
	if tile == nil || len(polygon.Vertices) < 3 {
		return false
	}
	inside := false
	j := len(polygon.Vertices) - 1
	const epsilon = float32(1e-5)
	for i := range polygon.Vertices {
		aIndex := polygon.Vertices[i]
		bIndex := polygon.Vertices[j]
		if aIndex < 0 || aIndex >= len(tile.Vertices) || bIndex < 0 || bIndex >= len(tile.Vertices) {
			return false
		}
		a := tile.Vertices[aIndex]
		b := tile.Vertices[bIndex]
		if navPointOnSegmentXZ(point, a, b, epsilon) {
			return true
		}
		intersects := (a[2] > point[2]) != (b[2] > point[2])
		if intersects {
			x := (b[0]-a[0])*(point[2]-a[2])/(b[2]-a[2]) + a[0]
			if point[0] < x {
				inside = !inside
			}
		}
		j = i
	}
	return inside
}

func navPolygonHeightAtXZ(tile *NavTileDef, polygon NavPolygonDef, point Vec3) (float32, bool) {
	if tile == nil || len(polygon.Vertices) < 3 {
		return 0, false
	}
	firstIndex := polygon.Vertices[0]
	if firstIndex < 0 || firstIndex >= len(tile.Vertices) {
		return 0, false
	}
	a := tile.Vertices[firstIndex]
	if navPolygonHorizontalY(tile, polygon, a[1]) {
		return a[1], true
	}
	for i := 1; i+1 < len(polygon.Vertices); i++ {
		bIndex := polygon.Vertices[i]
		cIndex := polygon.Vertices[i+1]
		if bIndex < 0 || bIndex >= len(tile.Vertices) || cIndex < 0 || cIndex >= len(tile.Vertices) {
			return 0, false
		}
		b := tile.Vertices[bIndex]
		c := tile.Vertices[cIndex]
		if y, ok := navTriangleHeightAtXZ(a, b, c, point); ok {
			return y, true
		}
	}
	return 0, false
}

func navPolygonHorizontalY(tile *NavTileDef, polygon NavPolygonDef, y float32) bool {
	for _, index := range polygon.Vertices {
		if index < 0 || index >= len(tile.Vertices) {
			return false
		}
		if !navAlmostEqual(tile.Vertices[index][1], y, 1e-4) {
			return false
		}
	}
	return true
}

func navTriangleHeightAtXZ(a, b, c, point Vec3) (float32, bool) {
	denominator := (b[2]-c[2])*(a[0]-c[0]) + (c[0]-b[0])*(a[2]-c[2])
	if absNavFloat32(denominator) <= 1e-6 {
		return 0, false
	}
	w1 := ((b[2]-c[2])*(point[0]-c[0]) + (c[0]-b[0])*(point[2]-c[2])) / denominator
	w2 := ((c[2]-a[2])*(point[0]-c[0]) + (a[0]-c[0])*(point[2]-c[2])) / denominator
	w3 := 1 - w1 - w2
	const epsilon = float32(1e-5)
	if w1 < -epsilon || w2 < -epsilon || w3 < -epsilon {
		return 0, false
	}
	return w1*a[1] + w2*b[1] + w3*c[1], true
}

func navPointOnSegmentXZ(point Vec3, a Vec3, b Vec3, epsilon float32) bool {
	cross := (point[2]-a[2])*(b[0]-a[0]) - (point[0]-a[0])*(b[2]-a[2])
	if float32(math.Abs(float64(cross))) > epsilon {
		return false
	}
	minX := minNavFloat32(a[0], b[0]) - epsilon
	maxX := maxNavFloat32(a[0], b[0]) + epsilon
	minZ := minNavFloat32(a[2], b[2]) - epsilon
	maxZ := maxNavFloat32(a[2], b[2]) + epsilon
	return point[0] >= minX && point[0] <= maxX && point[2] >= minZ && point[2] <= maxZ
}

func absNavFloat32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func minNavFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxNavFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
