package content

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNavGraphDeltaRebuildMatchesFullBakeAndSuppressesEmptyStaticTile(t *testing.T) {
	const chunkSize = 4
	coords := []TerrainChunkCoordDef{{}, {X: 1}}
	world := &ImportedWorldDef{
		WorldID: "delta-test", SchemaVersion: CurrentImportedWorldSchemaVersion,
		Kind: ImportedWorldKindVoxelWorld, ChunkSize: chunkSize, VoxelResolution: 1,
	}
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
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.4, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	base, err := BakeNavGraphWorld(world, chunks, []NavAgentProfileDef{profile})
	if err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(t.TempDir(), "base"+NavGraphManifestExtension)
	if err := SaveNavGraphBake(basePath, &base); err != nil {
		t.Fatal(err)
	}

	effective := append([]ImportedWorldChunkDef(nil), chunks...)
	effective[0].Voxels = nil
	effective[0].NonEmptyVoxelCount = 0
	full, err := BakeNavGraphWorld(world, effective, []NavAgentProfileDef{profile})
	if err != nil {
		t.Fatal(err)
	}
	effectiveByCoord := map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		effective[0].Coord: &effective[0],
		effective[1].Coord: &effective[1],
	}
	deltaPath := filepath.Join(t.TempDir(), "level.gkworlddelta")
	delta := &WorldDeltaDef{SchemaVersion: CurrentWorldDeltaSchemaVersion, LevelID: "level"}
	result, err := SaveNavGraphDeltaForImportedWorldChunks(deltaPath, delta, &base.Manifest, basePath, effectiveByCoord, []TerrainChunkCoordDef{coords[0]})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SourceOverrides) != 2 || len(result.GraphOverrides) != 2 {
		t.Fatalf("expected two affected source/graph overrides, got %+v", result)
	}
	leftSource, err := LoadEffectiveNavSourceTile(&base.Manifest, basePath, delta, deltaPath, coords[0])
	if err != nil || !leftSource.Found || !leftSource.Empty {
		t.Fatalf("removed floor did not suppress static source: lookup=%+v err=%v", leftSource, err)
	}
	leftGraph, err := LoadEffectiveNavGraphTile(&base.Manifest, basePath, delta, deltaPath, coords[0], profile.ID)
	if err != nil || !leftGraph.Found || !leftGraph.Empty {
		t.Fatalf("removed floor did not suppress static graph: lookup=%+v err=%v", leftGraph, err)
	}
	rightGraph, err := LoadEffectiveNavGraphTile(&base.Manifest, basePath, delta, deltaPath, coords[1], profile.ID)
	if err != nil || !rightGraph.Found || rightGraph.Empty {
		t.Fatalf("neighbor graph override missing: lookup=%+v err=%v", rightGraph, err)
	}
	deltaJSON, _ := json.Marshal(rightGraph.Tile)
	fullJSON, _ := json.Marshal(full.GraphTiles[1])
	if string(deltaJSON) != string(fullJSON) {
		t.Fatal("delta neighbor graph differs from full bake for identical effective input")
	}
	if err := SaveWorldDelta(deltaPath, delta); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadWorldDelta(deltaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(delta.NavigationSourceOverrides, loaded.NavigationSourceOverrides) || !reflect.DeepEqual(delta.NavigationGraphOverrides, loaded.NavigationGraphOverrides) {
		t.Fatal("navigation graph overrides did not round-trip with world delta")
	}
	findRoute := func() NavRouteResult {
		var sources []NavSourceTileDef
		var graphs []NavGraphTileDef
		for _, coord := range coords {
			source, err := LoadEffectiveNavSourceTile(&base.Manifest, basePath, delta, deltaPath, coord)
			if err != nil {
				t.Fatal(err)
			}
			graph, err := LoadEffectiveNavGraphTile(&base.Manifest, basePath, delta, deltaPath, coord, profile.ID)
			if err != nil {
				t.Fatal(err)
			}
			if source.Found && !source.Empty && graph.Found && !graph.Empty {
				sources = append(sources, *source.Tile)
				graphs = append(graphs, *graph.Tile)
			}
		}
		route, err := FindNavGraphRoute(sources, graphs, chunkSize, 1, Vec3{0.5, 1, 0.5}, Vec3{7.5, 1, 0.5})
		if err != nil && len(graphs) > 0 {
			t.Fatal(err)
		}
		return route
	}

	effective[0] = chunks[0]
	if _, err := SaveNavGraphDeltaForImportedWorldChunks(deltaPath, delta, &base.Manifest, basePath, effectiveByCoord, []TerrainChunkCoordDef{coords[0]}); err != nil {
		t.Fatal(err)
	}
	if route := findRoute(); !route.Found {
		t.Fatalf("restored floor did not restore route: %+v", route)
	}
	for z := range chunkSize {
		for y := 1; y <= 2; y++ {
			effective[0].Voxels = append(effective[0].Voxels, ImportedWorldVoxelDef{X: 2, Y: y, Z: z, Value: 1})
		}
	}
	if _, err := SaveNavGraphDeltaForImportedWorldChunks(deltaPath, delta, &base.Manifest, basePath, effectiveByCoord, []TerrainChunkCoordDef{coords[0]}); err != nil {
		t.Fatal(err)
	}
	if route := findRoute(); route.Found {
		t.Fatalf("voxel blocker did not remove route: %+v", route)
	}
}
