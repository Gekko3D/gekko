package gekko

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestStreamedNavigationResidencyRevisionSwapFollowsDelta(t *testing.T) {
	const chunkSize = 4
	coords := []content.TerrainChunkCoordDef{{}, {X: 1}}
	world := &content.ImportedWorldDef{
		WorldID: "runtime-nav", SchemaVersion: content.CurrentImportedWorldSchemaVersion,
		Kind: content.ImportedWorldKindVoxelWorld, ChunkSize: chunkSize, VoxelResolution: 1,
	}
	chunks := make([]content.ImportedWorldChunkDef, 2)
	for i, coord := range coords {
		voxels := make([]content.ImportedWorldVoxelDef, 0, chunkSize*chunkSize)
		for x := range chunkSize {
			for z := range chunkSize {
				voxels = append(voxels, content.ImportedWorldVoxelDef{X: x, Z: z, Value: 1})
			}
		}
		world.Entries = append(world.Entries, content.ImportedWorldChunkEntryDef{Coord: coord, ChunkPath: coord.String() + ".gkchunk", NonEmptyVoxelCount: len(voxels)})
		chunks[i] = content.ImportedWorldChunkDef{WorldID: world.WorldID, Coord: coord, ChunkSize: chunkSize, VoxelResolution: 1, Voxels: voxels}
	}
	content.EnsureImportedWorldSectors(world)
	profile := content.NavAgentProfileDef{ID: "walker", Radius: 0.4, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	bake, err := content.BakeNavGraphWorld(world, chunks, []content.NavAgentProfileDef{profile})
	if err != nil {
		t.Fatal(err)
	}
	query, err := buildRuntimeNavigationQuery(bake.SourceTiles, bake.GraphTiles, bake.Manifest.ChunkSize, bake.Manifest.VoxelResolution)
	if err != nil {
		t.Fatal(err)
	}
	prepared := RuntimeNavigationService{NavigationRevision: 7, query: query}
	preparedRoute, err := prepared.FindRoute(content.Vec3{0.5, 1, 0.5}, content.Vec3{7.5, 1, 0.5})
	if err != nil || !preparedRoute.Found || preparedRoute.NavigationRevision != 7 {
		t.Fatalf("prepared runtime query failed: route=%+v err=%v", preparedRoute, err)
	}
	basePath := filepath.Join(t.TempDir(), "base"+content.NavGraphManifestExtension)
	if err := content.SaveNavGraphBake(basePath, &bake); err != nil {
		t.Fatal(err)
	}
	deltaPath := filepath.Join(t.TempDir(), "level.gkworlddelta")
	delta := &content.WorldDeltaDef{SchemaVersion: content.CurrentWorldDeltaSchemaVersion, LevelID: "level"}
	desired := map[content.TerrainChunkCoordDef]struct{}{coords[0]: {}, coords[1]: {}}
	sources, graphs, err := loadStreamedNavigationResidency(&bake.Manifest, basePath, delta, deltaPath, desired)
	if err != nil {
		t.Fatal(err)
	}
	state := &StreamedLevelRuntimeState{
		Initialized: true, BaseNavManifest: &bake.Manifest, navigationRequestedGen: 1, navigationLoadActive: true,
		navigationLoads: make(chan streamedNavigationLoadResult, 1), navigationRebuilds: make(chan streamedNavigationRebuildResult, 1),
	}
	state.navigationLoads <- streamedNavigationLoadResult{Generation: 1, Sources: sources, Graphs: graphs}
	streamedLevelNavigationSystem(state)
	before := RuntimeNavigationServiceFromStreamedLevelState(state)
	route, err := before.FindRoute(content.Vec3{0.5, 1, 0.5}, content.Vec3{7.5, 1, 0.5})
	if err != nil || !route.Found || route.NavigationRevision != 1 {
		t.Fatalf("initial streamed route failed: route=%+v err=%v", route, err)
	}

	chunks[0].Voxels = nil
	effective := map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{coords[0]: &chunks[0], coords[1]: &chunks[1]}
	if _, err := content.SaveNavGraphDeltaForImportedWorldChunks(deltaPath, delta, &bake.Manifest, basePath, effective, []content.TerrainChunkCoordDef{coords[0]}); err != nil {
		t.Fatal(err)
	}
	sources, graphs, err = loadStreamedNavigationResidency(&bake.Manifest, basePath, delta, deltaPath, desired)
	if err != nil {
		t.Fatal(err)
	}
	state.navigationRequestedGen = 2
	state.navigationLoadActive = true
	state.navigationLoads <- streamedNavigationLoadResult{Generation: 2, Sources: sources, Graphs: graphs}
	streamedLevelNavigationSystem(state)
	after := RuntimeNavigationServiceFromStreamedLevelState(state)
	route, err = after.FindRoute(content.Vec3{0.5, 1, 0.5}, content.Vec3{7.5, 1, 0.5})
	if err != nil || route.Found || route.NavigationRevision != 2 {
		t.Fatalf("delta revision did not remove route atomically: route=%+v err=%v", route, err)
	}
}

func TestVoxelWorldDirtyNotificationPreservesEmptyChunk(t *testing.T) {
	app := NewApp()
	app.resources[reflect.TypeOf(VoxelWorldDirtyChunks{})] = &VoxelWorldDirtyChunks{Imported: make(map[voxelWorldDirtyChunkKey]*content.ImportedWorldChunkDef)}
	cmd := app.Commands()
	entity := cmd.AddEntity(
		&VoxelModelComponent{TerrainChunkSize: 4, VoxelResolution: 1},
		&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&AuthoredImportedWorldChunkRefComponent{WorldID: "world", ChunkCoord: [3]int{2, 0, -1}},
	)
	app.FlushCommands()
	notifyImportedWorldChunkDirty(cmd, entity, volume.NewXBrickMap())
	snapshots := takeVoxelWorldDirtyChunks(app, "world")
	if len(snapshots) != 1 || snapshots[0].Coord != (content.TerrainChunkCoordDef{X: 2, Z: -1}) || snapshots[0].NonEmptyVoxelCount != 0 {
		t.Fatalf("empty dirty chunk notification lost: %+v", snapshots)
	}
}

func TestConfigureStreamedNavigationManifestUsesValidatedRuntimeOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime"+content.NavGraphManifestExtension)
	manifest := &content.NavGraphManifestDef{
		NavID: "runtime-nav", SchemaVersion: content.CurrentNavGraphManifestSchemaVersion,
		SourceWorldID: "world", BuilderVersion: content.CurrentNavGraphBuilderVersion,
		ChunkSize: 16, VoxelResolution: 0.25,
	}
	if err := content.SaveNavGraphManifest(path, manifest); err != nil {
		t.Fatal(err)
	}
	level := &content.LevelDef{ChunkSize: 16, VoxelResolution: 0.25, Navigation: &content.LevelNavigationDef{ManifestPath: "missing.gknav"}}
	state := &StreamedLevelRuntimeState{BaseWorldID: "world"}
	if err := configureStreamedNavigationManifest(state, level, StreamedLevelRuntimeConfig{LevelPath: filepath.Join(filepath.Dir(path), "level.gklevel"), NavigationManifestPath: path}); err != nil {
		t.Fatal(err)
	}
	if state.BaseNavManifestPath != path || state.BaseNavManifest == nil || state.BaseNavManifest.NavID != manifest.NavID {
		t.Fatalf("runtime navigation override was not selected: %+v", state.BaseNavManifest)
	}
	state.BaseWorldID = "other-world"
	if err := configureStreamedNavigationManifest(state, level, StreamedLevelRuntimeConfig{LevelPath: filepath.Join(filepath.Dir(path), "level.gklevel"), NavigationManifestPath: path}); err == nil {
		t.Fatal("navigation override with mismatched source world was accepted")
	}
}
