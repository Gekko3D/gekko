package gekko

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestBrokenLadderDisablesRuntimeTraversalWithoutRebake(t *testing.T) {
	const chunkSize = 4
	source := content.NavSourceTileDef{
		NavID: "ladder-runtime", SchemaVersion: content.CurrentNavSourceTileSchemaVersion,
		BuilderVersion: content.CurrentNavGraphBuilderVersion, ChunkSize: chunkSize,
		SourceHash: "source", DependencyHash: "dependency",
		Spans: []content.NavSpanDef{
			{ID: 0, X: 0, Z: 0, SupportHeight: 0, CeilingHeight: 2, Headroom: 2, ClearanceRadius: 1, Area: "ground"},
			{ID: 1, X: 0, Y: 3, Z: 0, SupportHeight: 3, CeilingHeight: 6, Headroom: 3, ClearanceRadius: 1, Area: "ground"},
		},
	}
	profile := content.NavAgentProfileDef{
		ID: "climber", Radius: 0.4, Height: 1.5, StepHeight: 0.5, MaxSlopeDegrees: 45,
		Capabilities: []string{content.NavCapabilityClimbLadder},
	}
	built, err := content.BuildNavSpanGraph(source, profile, 1)
	if err != nil {
		t.Fatal(err)
	}
	bottom, top := content.Vec3{0.5, 0, 0.5}, content.Vec3{0.5, 3, 0.5}
	ladder := content.LevelLadderVolumeDef{
		ID: "ladder-1", BoundsCenter: content.Vec3{0.5, 1.5, 0.5}, BoundsHalfExtents: content.Vec3{0.5, 1.5, 0.5},
		MountBottom: &bottom, MountTop: &top, Health: 50,
	}
	graphs, _, err := content.ConnectNavGraphLadders([]content.NavSourceTileDef{source}, []content.NavGraphTileDef{built.Graph}, []content.LevelLadderVolumeDef{ladder}, profile, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	query, err := buildRuntimeNavigationQuery([]content.NavSourceTileDef{source}, graphs, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	state := &StreamedLevelRuntimeState{
		Initialized: true, LevelID: "level", BaseNavManifest: &content.NavGraphManifestDef{
			ChunkSize: chunkSize, VoxelResolution: 1, LadderVolumes: []content.LevelLadderVolumeDef{ladder},
		},
		NavigationSources: []content.NavSourceTileDef{source}, NavigationGraphs: graphs,
		NavigationRevision: 1, navigationQuery: query, navigationDisabled: make(map[string]struct{}),
	}
	app := NewApp()
	cmd := app.Commands()
	entity := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0.5, 1.5, 0.5}},
		&LadderVolumeComponent{},
		&AuthoredLevelLadderVolumeRefComponent{LevelID: "level", LadderVolumeID: ladder.ID},
		&BreakableComponent{Kind: "ladder", Health: ladder.Health, MaxHealth: ladder.Health},
	)
	app.FlushCommands()
	before, err := RuntimeNavigationServiceFromStreamedLevelState(state).FindRoute(bottom, top)
	if err != nil || !before.Found {
		t.Fatalf("expected live ladder route: route=%+v err=%v", before, err)
	}
	if handled, broken := DamageBreakableEntity(cmd, entity, 100, 0); !handled || !broken {
		t.Fatalf("ladder damage was not handled: handled=%v broken=%v", handled, broken)
	}
	app.FlushCommands()
	streamedLevelNavigationOverlaySystem(cmd, state)
	after, err := RuntimeNavigationServiceFromStreamedLevelState(state).FindRoute(bottom, top)
	if err != nil || after.Found || after.FailureReason != content.NavRouteNoRoute || after.NavigationRevision != 2 {
		t.Fatalf("broken ladder traversal remained enabled: route=%+v err=%v", after, err)
	}
}

func TestRuntimeNavigationBlockerAddMoveRemove(t *testing.T) {
	const chunkSize = 7
	profile := content.NavAgentProfileDef{ID: "walker", Radius: 0.1, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	source := content.NavSourceTileDef{
		NavID: "blocker-runtime", SchemaVersion: content.CurrentNavSourceTileSchemaVersion,
		BuilderVersion: content.CurrentNavGraphBuilderVersion, ChunkSize: chunkSize,
		SourceHash: "source", DependencyHash: "dependency",
	}
	for x := 0; x < chunkSize; x++ {
		for z := 0; z < chunkSize; z++ {
			source.Spans = append(source.Spans, content.NavSpanDef{
				ID: uint32(len(source.Spans)), X: x, Z: z, SupportHeight: 0, CeilingHeight: 3,
				Headroom: 3, ClearanceRadius: 1, Area: "ground",
			})
		}
	}
	built, err := content.BuildNavSpanGraph(source, profile, 1)
	if err != nil {
		t.Fatal(err)
	}
	query, err := buildRuntimeNavigationQuery([]content.NavSourceTileDef{source}, []content.NavGraphTileDef{built.Graph}, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	state := &StreamedLevelRuntimeState{
		Initialized: true, BaseNavManifest: &content.NavGraphManifestDef{
			ChunkSize: chunkSize, VoxelResolution: 1, AgentProfiles: []content.NavAgentProfileDef{profile},
		},
		NavigationSources: []content.NavSourceTileDef{source}, NavigationGraphs: []content.NavGraphTileDef{built.Graph},
		NavigationRevision: 1, navigationQuery: query, navigationDisabled: make(map[string]struct{}), navigationBlockers: make(map[string]content.NavBlockerDef),
	}
	start, goal := content.Vec3{0.1, 0.2, 2.5}, content.Vec3{6.9, 0.2, 2.5}
	app := NewApp()
	cmd := app.Commands()
	entity := cmd.AddEntity(
		&AABBComponent{Min: mgl32.Vec3{3.25, 0, 2.25}, Max: mgl32.Vec3{3.75, 1, 2.75}},
		&NavigationBlockerComponent{ID: "crate"},
	)
	app.FlushCommands()
	streamedLevelNavigationOverlaySystem(cmd, state)
	route, err := RuntimeNavigationServiceFromStreamedLevelState(state).FindRoute(start, goal)
	if err != nil || !route.Found || route.NavigationRevision != 2 || len(route.Waypoints) < 2 {
		t.Fatalf("placed blocker did not reroute: route=%+v err=%v", route, err)
	}

	MakeQuery1[AABBComponent](cmd).Map(func(_ EntityId, bounds *AABBComponent) bool {
		bounds.Min, bounds.Max = mgl32.Vec3{3.25, 0, 0}, mgl32.Vec3{3.75, 1, 7}
		return true
	})
	streamedLevelNavigationOverlaySystem(cmd, state)
	route, err = RuntimeNavigationServiceFromStreamedLevelState(state).FindRoute(start, goal)
	if err != nil || route.Found || route.FailureReason != content.NavRouteNoRoute || route.NavigationRevision != 3 {
		t.Fatalf("moved blocker did not split route: route=%+v err=%v", route, err)
	}

	cmd.RemoveEntity(entity)
	app.FlushCommands()
	streamedLevelNavigationOverlaySystem(cmd, state)
	route, err = RuntimeNavigationServiceFromStreamedLevelState(state).FindRoute(start, goal)
	if err != nil || !route.Found || route.NavigationRevision != 4 || len(route.Waypoints) != 1 {
		t.Fatalf("removed blocker did not restore route: route=%+v err=%v", route, err)
	}
}

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
