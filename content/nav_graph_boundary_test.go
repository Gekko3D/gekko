package content

import (
	"reflect"
	"testing"
)

func TestConnectNavGraphTiles(t *testing.T) {
	const chunkSize = 2
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.5, Height: 1.5, StepHeight: 0.6, MaxSlopeDegrees: 30}
	makeSource := func(coord TerrainChunkCoordDef, axis uint8, boundary int, clearance float32, sourceHash string) NavSourceTileDef {
		spans := make([]NavSpanDef, 0, chunkSize)
		for row := 0; row < chunkSize; row++ {
			x, z := boundary, row
			if axis == 1 {
				x, z = row, boundary
			}
			spans = append(spans, NavSpanDef{
				ID: uint32(row), X: x, Z: z, SupportHeight: 0, CeilingHeight: 3,
				Headroom: 3, ClearanceRadius: clearance, Area: "ground",
			})
		}
		return NavSourceTileDef{
			NavID: "test", SchemaVersion: CurrentNavSourceTileSchemaVersion, Coord: coord,
			BuilderVersion: "test", SourceHash: sourceHash, DependencyHash: "pending", ChunkSize: chunkSize, Spans: spans,
		}
	}
	buildGraph := func(source NavSourceTileDef) NavGraphTileDef {
		t.Helper()
		built, err := BuildNavSpanGraph(source, profile, 1)
		if err != nil {
			t.Fatalf("build failed: %v", err)
		}
		return built.Graph
	}
	findSource := func(sources []NavSourceTileDef, coord TerrainChunkCoordDef) NavSourceTileDef {
		t.Helper()
		for _, source := range sources {
			if source.Coord == coord {
				return source
			}
		}
		t.Fatalf("missing source %s", TerrainChunkKey(coord))
		return NavSourceTileDef{}
	}
	findGraph := func(graphs []NavGraphTileDef, coord TerrainChunkCoordDef) NavGraphTileDef {
		t.Helper()
		for _, graph := range graphs {
			if graph.Coord == coord {
				return graph
			}
		}
		t.Fatalf("missing graph %s", TerrainChunkKey(coord))
		return NavGraphTileDef{}
	}
	setDependencies := func(sources ...NavSourceTileDef) []NavSourceTileDef {
		index := make(map[TerrainChunkCoordDef]string, len(sources))
		for _, source := range sources {
			index[source.Coord] = source.SourceHash
		}
		for i := range sources {
			sources[i].DependencyHash = navGraphDependencyHash(sources[i].Coord, func(coord TerrainChunkCoordDef) (string, bool) {
				hash, known := index[coord]
				return hash, known
			})
		}
		return sources
	}
	externalSpanTransitions := func(graph NavGraphTileDef) []NavSpanTransitionDef {
		var result []NavSpanTransitionDef
		for _, transition := range graph.SpanTransitions {
			if transition.To.Tile != graph.Coord {
				result = append(result, transition)
			}
		}
		return result
	}
	externalRegionTransitions := func(graph NavGraphTileDef) []NavRegionTransitionDef {
		var result []NavRegionTransitionDef
		for _, transition := range graph.Transitions {
			if transition.ToTile != graph.Coord {
				result = append(result, transition)
			}
		}
		return result
	}

	directions := []struct {
		name       string
		dx, dz     int
		axis       uint8
		centerSide int
		otherSide  int
		start, end Vec3
	}{
		{name: "east", dx: 1, axis: 0, centerSide: 1, otherSide: 0, start: Vec3{2, 0, 0}, end: Vec3{2, 0, 2}},
		{name: "west", dx: -1, axis: 0, centerSide: 0, otherSide: 1, start: Vec3{0, 0, 0}, end: Vec3{0, 0, 2}},
		{name: "south", dz: 1, axis: 1, centerSide: 1, otherSide: 0, start: Vec3{0, 0, 2}, end: Vec3{2, 0, 2}},
		{name: "north", dz: -1, axis: 1, centerSide: 0, otherSide: 1, start: Vec3{0, 0, 0}, end: Vec3{2, 0, 0}},
	}
	for _, direction := range directions {
		t.Run(direction.name, func(t *testing.T) {
			centerCoord := TerrainChunkCoordDef{}
			otherCoord := TerrainChunkCoordDef{X: direction.dx, Z: direction.dz}
			pair := setDependencies(
				makeSource(centerCoord, direction.axis, direction.centerSide, 1, "center"),
				makeSource(otherCoord, direction.axis, direction.otherSide, 1, "other"),
			)
			center, other := findSource(pair, centerCoord), findSource(pair, otherCoord)
			graphs := []NavGraphTileDef{buildGraph(center), buildGraph(other)}

			sources, connected, err := ConnectNavGraphTiles([]NavSourceTileDef{other, center}, []NavGraphTileDef{graphs[1], graphs[0]}, profile, chunkSize, 1)
			if err != nil {
				t.Fatalf("connect failed: %v", err)
			}
			for _, coord := range []TerrainChunkCoordDef{centerCoord, otherCoord} {
				graph := findGraph(connected, coord)
				if got := externalSpanTransitions(graph); len(got) != chunkSize || got[0].To.Tile == coord {
					t.Fatalf("want %d cross-tile span transitions at %s, got %+v", chunkSize, TerrainChunkKey(coord), got)
				}
				got := externalRegionTransitions(graph)
				if len(got) != 1 || got[0].Width != chunkSize || got[0].CrossingStart != direction.start || got[0].CrossingEnd != direction.end {
					t.Fatalf("cross-tile region run mismatch at %s: %+v", TerrainChunkKey(coord), got)
				}
			}
			if findSource(sources, centerCoord).DependencyHash == "pending" {
				t.Fatal("dependency hash was not built")
			}

			reSources, reGraphs, err := ConnectNavGraphTiles(sources, connected, profile, chunkSize, 1)
			if err != nil || !reflect.DeepEqual(reSources, sources) || !reflect.DeepEqual(reGraphs, connected) {
				t.Fatalf("connection is not deterministic: err=%v", err)
			}
		})
	}

	t.Run("unknown and blocked seams stay closed", func(t *testing.T) {
		center := setDependencies(makeSource(TerrainChunkCoordDef{}, 0, 1, 1, "center"))[0]
		centerGraph := buildGraph(center)
		unknownSources, unknownGraphs, err := ConnectNavGraphTiles([]NavSourceTileDef{center}, []NavGraphTileDef{centerGraph}, profile, chunkSize, 1)
		if err != nil {
			t.Fatalf("unknown seam connect failed: %v", err)
		}
		if len(externalSpanTransitions(unknownGraphs[0])) != 0 || len(externalRegionTransitions(unknownGraphs[0])) != 0 {
			t.Fatalf("unknown seam opened: %+v", unknownGraphs[0])
		}

		blockedPair := setDependencies(
			makeSource(TerrainChunkCoordDef{}, 0, 1, 1, "center"),
			makeSource(TerrainChunkCoordDef{X: 1}, 0, 0, 0.25, "blocked"),
		)
		blockedCenter, blocked := findSource(blockedPair, TerrainChunkCoordDef{}), findSource(blockedPair, TerrainChunkCoordDef{X: 1})
		_, blockedGraphs, err := ConnectNavGraphTiles(blockedPair, []NavGraphTileDef{buildGraph(blockedCenter), buildGraph(blocked)}, profile, chunkSize, 1)
		if err != nil {
			t.Fatalf("blocked seam connect failed: %v", err)
		}
		if len(externalSpanTransitions(findGraph(blockedGraphs, center.Coord))) != 0 {
			t.Fatal("blocked seam opened")
		}

		haloSources := setDependencies(
			makeSource(TerrainChunkCoordDef{}, 0, 1, 1, "center"),
			makeSource(TerrainChunkCoordDef{X: 1, Z: 1}, 0, 0, 1, "diagonal"),
		)
		haloCenter := findSource(haloSources, TerrainChunkCoordDef{})
		withHalo, _, err := ConnectNavGraphTiles(haloSources, []NavGraphTileDef{buildGraph(haloCenter)}, profile, chunkSize, 1)
		if err != nil {
			t.Fatalf("halo hash connect failed: %v", err)
		}
		if findSource(unknownSources, center.Coord).DependencyHash == findSource(withHalo, center.Coord).DependencyHash {
			t.Fatal("diagonal halo source did not affect dependency hash")
		}
	})
}
