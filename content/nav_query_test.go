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
	if len(path.PolygonIDs) != len(want) || len(path.Waypoints) != len(want) {
		t.Fatalf("unexpected path lengths: %+v", path)
	}
	for i := range want {
		if path.PolygonIDs[i] != want[i] {
			t.Fatalf("unexpected path ids, want=%+v got=%+v", want, path.PolygonIDs)
		}
	}
	if path.Waypoints[0] != (Vec3{0.5, 1, 0.5}) || path.Waypoints[2] != (Vec3{2.5, 1, 0.5}) {
		t.Fatalf("unexpected path waypoints: %+v", path.Waypoints)
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
	tile.Polygons[1].Neighbors = nil
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
