package content

import (
	"path/filepath"
	"testing"
)

var (
	navGraphBenchmarkQuery *NavGraphQuery
	navGraphBenchmarkRoute NavRouteResult
	navGraphBenchmarkDelta NavGraphDeltaBakeResult
)

func BenchmarkNavGraphPhase0(b *testing.B) {
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.2, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	denseSource, denseGraph := benchmarkNavGraph(b, 48, profile, false, false)
	stackedSource, stackedGraph := benchmarkNavGraph(b, 32, profile, true, false)
	multiRegionSource, multiRegionGraph := benchmarkNavGraph(b, 48, profile, false, true)

	b.Run("dense_flat/query_index", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			query, err := NewNavGraphQuery([]NavSourceTileDef{denseSource}, []NavGraphTileDef{denseGraph}, denseSource.ChunkSize, 1)
			if err != nil {
				b.Fatal(err)
			}
			navGraphBenchmarkQuery = query
		}
	})
	b.Run("dense_flat/route", func(b *testing.B) {
		query, err := NewNavGraphQuery([]NavSourceTileDef{denseSource}, []NavGraphTileDef{denseGraph}, denseSource.ChunkSize, 1)
		if err != nil {
			b.Fatal(err)
		}
		start, goal := Vec3{0.5, 0, 0.5}, Vec3{47.5, 0, 47.5}
		b.ReportAllocs()
		for b.Loop() {
			route, err := query.FindRoute(start, goal)
			if err != nil || !route.Found {
				b.Fatalf("route=%+v err=%v", route, err)
			}
			navGraphBenchmarkRoute = route
		}
	})
	b.Run("stacked/query_index", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			query, err := NewNavGraphQuery([]NavSourceTileDef{stackedSource}, []NavGraphTileDef{stackedGraph}, stackedSource.ChunkSize, 1)
			if err != nil {
				b.Fatal(err)
			}
			navGraphBenchmarkQuery = query
		}
	})
	b.Run("multi_region/route", func(b *testing.B) {
		query, err := NewNavGraphQuery([]NavSourceTileDef{multiRegionSource}, []NavGraphTileDef{multiRegionGraph}, multiRegionSource.ChunkSize, 1)
		if err != nil {
			b.Fatal(err)
		}
		start, goal := Vec3{0.5, 0, 0.5}, Vec3{47.5, 0, 47.5}
		b.ReportAllocs()
		for b.Loop() {
			route, err := query.FindRoute(start, goal)
			if err != nil || !route.Found {
				b.Fatalf("route=%+v err=%v", route, err)
			}
			navGraphBenchmarkRoute = route
		}
	})
	b.Run("carrier_blocked/route", func(b *testing.B) {
		carriers := []NavCarrierDef{{
			ID: "benchmark-lift", BoundsHalfExtents: Vec3{0.5, 0.5, 3},
			Stops: []NavCarrierStopDef{{ID: "low", BoundsCenter: Vec3{24, -0.5, 24}}, {ID: "high", BoundsCenter: Vec3{24, -0.5, 24}}},
		}}
		query, err := NewNavGraphQueryWithBlockers(
			[]NavSourceTileDef{denseSource}, []NavGraphTileDef{denseGraph}, denseSource.ChunkSize, 1, profile,
			NavCarrierBlockers(carriers, profile),
		)
		if err != nil {
			b.Fatal(err)
		}
		start, goal := Vec3{0.5, 0, 24.5}, Vec3{47.5, 0, 24.5}
		b.ReportAllocs()
		for b.Loop() {
			route, err := query.FindRoute(start, goal)
			if err != nil || !route.Found {
				b.Fatalf("route=%+v err=%v", route, err)
			}
			navGraphBenchmarkRoute = route
		}
	})
	b.Run("local_destruction/rebuild", func(b *testing.B) {
		basePath, base, effective, dirty := benchmarkNavDeltaFixture(b, profile)
		deltaPath := filepath.Join(b.TempDir(), "benchmark.gkworlddelta")
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			delta := &WorldDeltaDef{SchemaVersion: CurrentWorldDeltaSchemaVersion, LevelID: "benchmark"}
			result, err := SaveNavGraphDeltaForImportedWorldChunks(deltaPath, delta, &base.Manifest, basePath, effective, dirty)
			if err != nil {
				b.Fatal(err)
			}
			navGraphBenchmarkDelta = result
		}
	})
}

func benchmarkNavGraph(b *testing.B, chunkSize int, profile NavAgentProfileDef, stacked, multiRegion bool) (NavSourceTileDef, NavGraphTileDef) {
	b.Helper()
	source := NavSourceTileDef{
		NavID: "benchmark", SchemaVersion: CurrentNavSourceTileSchemaVersion, BuilderVersion: "benchmark",
		ChunkSize: chunkSize, SourceHash: "benchmark", DependencyHash: "benchmark",
	}
	for x := range chunkSize {
		for z := range chunkSize {
			area := "ground"
			if multiRegion && x >= chunkSize/3 && x < 2*chunkSize/3 {
				area = "mud"
			}
			source.Spans = append(source.Spans, NavSpanDef{
				ID: uint32(len(source.Spans)), X: x, Z: z, SupportHeight: 0, CeilingHeight: 4,
				Headroom: 4, ClearanceRadius: 1, Area: area,
			})
			if stacked {
				source.Spans = append(source.Spans, NavSpanDef{
					ID: uint32(len(source.Spans)), X: x, Y: 3, Z: z, SupportHeight: 3, CeilingHeight: 7,
					Headroom: 4, ClearanceRadius: 1, Area: area,
				})
			}
		}
	}
	built, err := BuildNavSpanGraph(source, profile, 1)
	if err != nil {
		b.Fatal(err)
	}
	return source, built.Graph
}

func benchmarkNavDeltaFixture(b *testing.B, profile NavAgentProfileDef) (string, NavGraphBakeResult, map[TerrainChunkCoordDef]*ImportedWorldChunkDef, []TerrainChunkCoordDef) {
	b.Helper()
	const chunkSize = 8
	coords := []TerrainChunkCoordDef{{}, {X: 1}}
	world := &ImportedWorldDef{WorldID: "benchmark", SchemaVersion: CurrentImportedWorldSchemaVersion, Kind: ImportedWorldKindVoxelWorld, ChunkSize: chunkSize, VoxelResolution: 1}
	chunks := make([]ImportedWorldChunkDef, 0, len(coords))
	for _, coord := range coords {
		voxels := make([]ImportedWorldVoxelDef, 0, chunkSize*chunkSize)
		for x := range chunkSize {
			for z := range chunkSize {
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Z: z, Value: 1})
			}
		}
		world.Entries = append(world.Entries, ImportedWorldChunkEntryDef{Coord: coord, ChunkPath: navGraphCoordFilename(coord) + ".gkchunk", NonEmptyVoxelCount: len(voxels)})
		chunks = append(chunks, ImportedWorldChunkDef{WorldID: world.WorldID, Coord: coord, ChunkSize: chunkSize, VoxelResolution: 1, Voxels: voxels})
	}
	EnsureImportedWorldSectors(world)
	base, err := BakeNavGraphWorld(world, chunks, []NavAgentProfileDef{profile})
	if err != nil {
		b.Fatal(err)
	}
	basePath := filepath.Join(b.TempDir(), "base"+NavGraphManifestExtension)
	if err := SaveNavGraphBake(basePath, &base); err != nil {
		b.Fatal(err)
	}
	effective := make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef, len(chunks))
	for i := range chunks {
		copy := chunks[i]
		effective[copy.Coord] = &copy
	}
	for z := range chunkSize {
		for y := 1; y <= 2; y++ {
			effective[TerrainChunkCoordDef{}].Voxels = append(effective[TerrainChunkCoordDef{}].Voxels, ImportedWorldVoxelDef{X: chunkSize / 2, Y: y, Z: z, Value: 1})
		}
	}
	return basePath, base, effective, []TerrainChunkCoordDef{{}}
}
