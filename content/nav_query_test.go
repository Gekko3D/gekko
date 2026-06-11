package content

import "testing"

func TestNavTileQueryFindsPolygonAtPoint(t *testing.T) {
	tile := navQueryTestTile()
	query, err := NewNavTileQuery(tile)
	if err != nil {
		t.Fatalf("NewNavTileQuery failed: %v", err)
	}
	polygon, ok := query.FindPolygonAt(Vec3{0.25, 1, 0.25})
	if !ok || polygon.ID != "a" {
		t.Fatalf("expected point to resolve to polygon a, got ok=%t polygon=%+v", ok, polygon)
	}
	if _, ok := query.FindPolygonAt(Vec3{5, 1, 5}); ok {
		t.Fatal("did not expect point outside tile to resolve to a polygon")
	}
	center, ok := query.PolygonCenter("b")
	if !ok || center != (Vec3{1.5, 1, 0.5}) {
		t.Fatalf("unexpected polygon center: ok=%t center=%+v", ok, center)
	}
}

func TestNavTileQueryFindsNearestPolygon(t *testing.T) {
	tile := navQueryTestTile()
	query, err := NewNavTileQuery(tile)
	if err != nil {
		t.Fatalf("NewNavTileQuery failed: %v", err)
	}
	snap, ok := query.FindNearestPolygon(Vec3{3.25, 1.4, 0.5}, 0.6)
	if !ok {
		t.Fatal("expected point near polygon c to snap")
	}
	if snap.Polygon.ID != "c" {
		t.Fatalf("expected snap to polygon c, got %+v", snap.Polygon)
	}
	if !navAlmostEqual(snap.Point[0], 3, 1e-5) || !navAlmostEqual(snap.Point[1], 1, 1e-5) || !navAlmostEqual(snap.Point[2], 0.5, 1e-5) {
		t.Fatalf("unexpected snapped point: %+v", snap.Point)
	}
	if !navAlmostEqual(snap.HorizontalDistance, 0.25, 1e-5) || !navAlmostEqual(snap.VerticalDistance, 0.4, 1e-5) {
		t.Fatalf("unexpected snap distances: %+v", snap)
	}
	if _, ok := query.FindNearestPolygon(Vec3{3.25, 1.4, 0.5}, 0.2); ok {
		t.Fatal("did not expect snap beyond max distance")
	}
}

func TestNavTileQueryFindsPathAcrossNeighbors(t *testing.T) {
	query, err := NewNavTileQuery(navQueryTestTile())
	if err != nil {
		t.Fatalf("NewNavTileQuery failed: %v", err)
	}
	path, err := query.FindPath(Vec3{0.25, 1, 0.25}, Vec3{2.75, 1, 0.25})
	if err != nil {
		t.Fatalf("FindPath failed: %v", err)
	}
	if !path.Found {
		t.Fatal("expected path to be found")
	}
	want := []string{"a", "b", "c"}
	if len(path.PolygonIDs) != len(want) || len(path.Waypoints) != 2 {
		t.Fatalf("unexpected path lengths: %+v", path)
	}
	for i := range want {
		if path.PolygonIDs[i] != want[i] {
			t.Fatalf("unexpected path ids, want=%+v got=%+v", want, path.PolygonIDs)
		}
	}
	if path.Waypoints[0] != (Vec3{0.25, 1, 0.25}) || path.Waypoints[1] != (Vec3{2.75, 1, 0.25}) {
		t.Fatalf("unexpected path waypoints: %+v", path.Waypoints)
	}
}

func TestNavTileQueryInfersMissingSameTileNeighbors(t *testing.T) {
	tile := navQueryTestTile()
	tile.Polygons[0].Neighbors = nil
	tile.Polygons[1].Neighbors = nil
	tile.Polygons[2].Neighbors = nil
	query, err := NewNavTileQuery(tile)
	if err != nil {
		t.Fatalf("NewNavTileQuery failed: %v", err)
	}
	path, err := query.FindPath(Vec3{0.25, 1, 0.25}, Vec3{2.75, 1, 0.25})
	if err != nil {
		t.Fatalf("FindPath failed: %v", err)
	}
	if !path.Found || len(path.PolygonIDs) != 3 {
		t.Fatalf("expected inferred neighbor path across touching polygons, got %+v", path)
	}
}

func TestNavTileQueryStringPullsAcrossTriangleFan(t *testing.T) {
	tile := &NavTileDef{
		NavID:          "nav-fan",
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: "tiny",
		BuilderVersion: DefaultNavBuilderVersion,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{10, 2, 10},
		Vertices: []Vec3{
			{0, 1, 0},
			{10, 1, 0},
			{10, 1, 10},
			{0, 1, 10},
			{5, 1, 5},
		},
		Polygons: []NavPolygonDef{
			{ID: "bottom", Vertices: []int{0, 1, 4}, Area: NavTraversalWalk, Neighbors: []string{"left", "right"}},
			{ID: "right", Vertices: []int{1, 2, 4}, Area: NavTraversalWalk, Neighbors: []string{"bottom", "top"}},
			{ID: "top", Vertices: []int{2, 3, 4}, Area: NavTraversalWalk, Neighbors: []string{"left", "right"}},
			{ID: "left", Vertices: []int{3, 0, 4}, Area: NavTraversalWalk, Neighbors: []string{"bottom", "top"}},
		},
	}
	query, err := NewNavTileQuery(tile)
	if err != nil {
		t.Fatalf("NewNavTileQuery failed: %v", err)
	}
	start := Vec3{8, 1, 0.5}
	end := Vec3{8, 1, 9.5}

	path, err := query.FindPath(start, end)
	if err != nil {
		t.Fatalf("FindPath failed: %v", err)
	}

	if !path.Found || len(path.PolygonIDs) != 3 {
		t.Fatalf("expected fan corridor through three triangles, got %+v", path)
	}
	if len(path.Waypoints) != 2 || path.Waypoints[0] != start || path.Waypoints[1] != end {
		t.Fatalf("expected string-pulled direct movement waypoints, got %+v", path.Waypoints)
	}
}

func TestNavTileQueryFindPathBetweenSamePolygon(t *testing.T) {
	query, err := NewNavTileQuery(navQueryTestTile())
	if err != nil {
		t.Fatalf("NewNavTileQuery failed: %v", err)
	}
	path, err := query.FindPathBetweenPolygons("a", "a")
	if err != nil {
		t.Fatalf("FindPathBetweenPolygons failed: %v", err)
	}
	if !path.Found || len(path.PolygonIDs) != 1 || path.PolygonIDs[0] != "a" || len(path.Waypoints) != 1 {
		t.Fatalf("unexpected same-polygon path: %+v", path)
	}
}

func TestNavTileQueryReportsUnreachablePath(t *testing.T) {
	tile := navQueryTestTile()
	tile.BoundsMax[0] = 4
	for i := range tile.Polygons[2].Vertices {
		tile.Vertices[tile.Polygons[2].Vertices[i]][0] += 1
	}
	tile.Polygons[1].Neighbors = []string{"a"}
	tile.Polygons[2].Neighbors = nil
	query, err := NewNavTileQuery(tile)
	if err != nil {
		t.Fatalf("NewNavTileQuery failed: %v", err)
	}
	path, err := query.FindPathBetweenPolygons("a", "c")
	if err != nil {
		t.Fatalf("FindPathBetweenPolygons failed: %v", err)
	}
	if path.Found || len(path.PolygonIDs) != 0 {
		t.Fatalf("did not expect path through disconnected polygons, got %+v", path)
	}
}

func TestNavTileQueryReturnsNoPathForMissingPoint(t *testing.T) {
	query, err := NewNavTileQuery(navQueryTestTile())
	if err != nil {
		t.Fatalf("NewNavTileQuery failed: %v", err)
	}
	path, err := query.FindPath(Vec3{-1, 1, -1}, Vec3{0.25, 1, 0.25})
	if err != nil {
		t.Fatalf("FindPath failed: %v", err)
	}
	if path.Found {
		t.Fatalf("did not expect missing start point path, got %+v", path)
	}
}

func navQueryTestTile() *NavTileDef {
	return &NavTileDef{
		NavID:          "nav-query",
		SchemaVersion:  CurrentNavTileSchemaVersion,
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: "tiny",
		BuilderVersion: DefaultNavBuilderVersion,
		PayloadKind:    NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{3, 2, 1},
		Vertices: []Vec3{
			{0, 1, 0}, {1, 1, 0}, {1, 1, 1}, {0, 1, 1},
			{1, 1, 0}, {2, 1, 0}, {2, 1, 1}, {1, 1, 1},
			{2, 1, 0}, {3, 1, 0}, {3, 1, 1}, {2, 1, 1},
		},
		Polygons: []NavPolygonDef{
			{ID: "a", Vertices: []int{0, 1, 2, 3}, Area: NavTraversalWalk, Neighbors: []string{"b"}},
			{ID: "b", Vertices: []int{4, 5, 6, 7}, Area: NavTraversalWalk, Neighbors: []string{"a", "c"}},
			{ID: "c", Vertices: []int{8, 9, 10, 11}, Area: NavTraversalWalk, Neighbors: []string{"b"}},
		},
	}
}
