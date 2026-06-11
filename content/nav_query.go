package content

import (
	"fmt"
	"math"
)

type NavTileQuery struct {
	Tile           *NavTileDef
	polygonByID    map[string]int
	polygonCenters map[string]Vec3
}

type NavTilePathResult struct {
	Found      bool
	PolygonIDs []string
	Waypoints  []Vec3
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
	}
	for i, polygon := range tile.Polygons {
		query.polygonByID[polygon.ID] = i
		query.polygonCenters[polygon.ID] = navPolygonCenter(tile, polygon)
	}
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
		center := q.polygonCenters[polygon.ID]
		distance := absNavFloat32(point[1] - center[1])
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
	return q.FindPathBetweenPolygons(startPolygon.ID, endPolygon.ID)
}

func (q *NavTileQuery) FindPathBetweenPolygons(startID string, endID string) (NavTilePathResult, error) {
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
		return NavTilePathResult{
			Found:      true,
			PolygonIDs: []string{startID},
			Waypoints:  []Vec3{q.polygonCenters[startID]},
		}, nil
	}
	visited := map[string]struct{}{startID: {}}
	previous := map[string]string{}
	queue := []string{startID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		polygon := q.Tile.Polygons[q.polygonByID[current]]
		for _, neighbor := range polygon.Neighbors {
			if _, ok := q.polygonByID[neighbor]; !ok {
				continue
			}
			if _, ok := visited[neighbor]; ok {
				continue
			}
			visited[neighbor] = struct{}{}
			previous[neighbor] = current
			if neighbor == endID {
				return q.buildPathResult(startID, endID, previous), nil
			}
			queue = append(queue, neighbor)
		}
	}
	return NavTilePathResult{}, nil
}

func (q *NavTileQuery) buildPathResult(startID string, endID string, previous map[string]string) NavTilePathResult {
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
	for _, id := range ids {
		waypoints = append(waypoints, q.polygonCenters[id])
	}
	return NavTilePathResult{
		Found:      true,
		PolygonIDs: ids,
		Waypoints:  waypoints,
	}
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
