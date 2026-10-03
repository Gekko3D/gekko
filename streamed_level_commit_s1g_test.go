package gekko

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type s1gFixture struct {
	*streamedRenderHarness
	observer EntityId
	hooks    map[string]int
}

func s1gID(chunk, placement int) string { return fmt.Sprintf("p%d-%d", chunk, placement) }

func s1gRuntime(t *testing.T, placements []int, units int, managed, geometry bool, hooks ...PostSpawnPlacementHook) *s1gFixture {
	t.Helper()
	path := s2bWorldPath(t)
	level, err := content.LoadLevel(path)
	if err != nil {
		t.Fatal(err)
	}
	level.Markers, level.Lights, level.Placements = nil, nil, nil
	if !geometry {
		level.Terrain, level.BaseWorld = nil, nil
	}
	assetPath := filepath.Join(filepath.Dir(path), "placement.gkasset")
	asset := content.NewAssetDef("s1g-asset")
	asset.ID = "s1g-asset"
	asset.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: false}
	asset.Parts = []content.AssetPartDef{{ID: "body", Name: "body", Source: testProceduralPartSource(), Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}}}
	if err := content.SaveAsset(assetPath, asset); err != nil {
		t.Fatal(err)
	}
	for chunk, count := range placements {
		for index := 0; index < count; index++ {
			level.Placements = append(level.Placements, content.LevelPlacementDef{ID: s1gID(chunk, index), AssetPath: "placement.gkasset", Transform: content.LevelTransformDef{
				Position: content.Vec3{float32(chunk*16 + index + 1), 1, 1}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1},
			}})
		}
	}
	if err := content.SaveLevel(path, level); err != nil {
		t.Fatal(err)
	}
	if validation := content.ValidateLevel(level, content.LevelValidationOptions{DocumentPath: path}); validation.HasErrors() {
		t.Fatalf("invalid S1g authored fixture: %s", validation.Error())
	}
	app, cmd, state := newStreamedRuntimeHarness(t)
	assets := assetServerFromApp(app)
	f := &s1gFixture{streamedRenderHarness: &streamedRenderHarness{t: t, app: app, cmd: cmd, runtime: state, assets: assets}, hooks: make(map[string]int)}
	if managed {
		f.renderer = newVoxelRtStateTest()
		cmd.AddResources(f.renderer)
	}
	observe := func(cmd *Commands, context PostSpawnPlacementContext) {
		f.hooks[context.Placement.PlacementID]++
		body := context.SpawnResult.EntitiesByAssetID["body"]
		if !cmd.EntityExists(context.RootEntity) || !cmd.EntityExists(body) {
			t.Fatal("placement hook did not see its atomic flushed entities")
		}
		cmd.AddComponents(body, &ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{.5, .5, .5}})
	}
	config := StreamedLevelRuntimeConfig{
		LevelPath: path, StreamingRadius: 2, StreamingKeepRadius: 2, StreamingPrefetchRadius: 2,
		StreamingCollisionRadius: 2, StreamingDestructionRadius: 2,
		MaxPrepareJobs: 2, MaxChunkCommitsPerFrame: 3, MaxStreamingWorkItems: 4,
		MaxPlacementCommitUnitsPerFrame: units, PlacementHooks: append([]PostSpawnPlacementHook{observe}, hooks...),
	}
	if err := StartStreamedLevelRuntime(cmd, assets, config); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := StopStreamedLevelRuntime(cmd); err != nil {
			t.Errorf("S1g cleanup: %v", err)
		}
	})
	f.observer = s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
	app.FlushCommands()
	updateStreamedObserverSelection(cmd, state)
	return f
}

func s1gPlacement(t *testing.T, f *s1gFixture, id string) (EntityId, EntityId) {
	t.Helper()
	root := placementEntityByIDForStreamedTest(f.cmd, id)
	body := placementItemEntityByIDForStreamedTest(f.cmd, id, "body")
	if root == 0 || body == 0 || f.hooks[id] != 1 || !hasComponentOfType[ColliderComponent](f.cmd, body) {
		t.Fatalf("placement %s did not finish one observable atomic unit: root=%d body=%d hooks=%d", id, root, body, f.hooks[id])
	}
	if live := s4cLive(t, f.cmd, f.assets, body); live == nil || live.GetVoxelCount() == 0 {
		t.Fatal("placement unit published unusable geometry")
	}
	return root, body
}

func s1gMetrics(t *testing.T, f *s1gFixture, active, units int) {
	t.Helper()
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	if f.runtime.Metrics.ActiveChunkCommitCount != active || f.runtime.Metrics.PlacementCommitUnitsLastFrame != units {
		t.Fatalf("placement service metrics: active=%d units=%d, want %d/%d", f.runtime.Metrics.ActiveChunkCommitCount, f.runtime.Metrics.PlacementCommitUnitsLastFrame, active, units)
	}
}

func s1gEdit(t *testing.T, f *s1gFixture, entity EntityId, point [3]int, value uint8) {
	t.Helper()
	model := mustVoxelModelComponentForLevelTest(t, f.cmd, entity)
	live := s4cLive(t, f.cmd, f.assets, entity).Copy()
	live.SetVoxel(point[0], point[1], point[2], value)
	model.OverrideGeometry = f.assets.RegisterSharedVoxelGeometry(live, "")
	f.cmd.AddComponents(entity, &model)
	MarkVoxelEntityPersistenceDirty(f.cmd, entity)
	f.app.FlushCommands()
}

func s1gValue(t *testing.T, f *s1gFixture, entity EntityId, point [3]int, want uint8) {
	t.Helper()
	occupied, value := s4cLive(t, f.cmd, f.assets, entity).GetVoxel(point[0], point[1], point[2])
	if !occupied || value != want {
		t.Fatalf("saved partial voxel %v = occupied %t value %d, want %d", point, occupied, value, want)
	}
}

func TestS1gManagedPlacementBudgetSharesRoundsRetainsAttemptAndAllowsProxyProgress(t *testing.T) {
	f := s1gRuntime(t, []int{3, 2}, 2, true, true)
	metadataBytes := f.runtime.Loader.Stats().PinnedBytes
	for _, coord := range []ChunkCoord{{}, {X: 1}} {
		s1fPrepared(t, f.streamedRenderHarness, coord, false)
	}
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	bytes := f.runtime.Metrics.PendingPreparedBytes
	f.commitStage()
	s1gMetrics(t, f, 2, 2)
	s1gPlacement(t, f, s1gID(0, 0))
	s1gPlacement(t, f, s1gID(1, 0))
	if placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 1)) != 0 || len(f.runtime.LoadedChunks) != 0 || f.runtime.Metrics.CommittedChunkCount != 0 ||
		f.runtime.Metrics.ChunksCommittedLastFrame != 0 || f.runtime.Metrics.EntitiesCommittedLastFrame != 6 ||
		f.runtime.Metrics.PendingPreparedBytes != bytes || f.runtime.Metrics.PendingLoadCount != 2 || f.runtime.Metrics.DecodedContentCachePinnedBytes <= metadataBytes {
		t.Fatal("shared placement round published completion or released paused ownership")
	}
	s1eWork(t, f.runtime, 2, 4, 0, 0)
	terrain := terrainChunkEntityByCoordForStreamedTest(f.cmd, [3]int{})
	imported := importedWorldChunkEntityByCoordForStreamedTest(f.cmd, [3]int{})
	f.status(terrain, StreamedVoxelRenderReady)
	f.status(imported, StreamedVoxelRenderReady)
	// The distinct-chunk gate applies to advancing an existing transaction.
	f.runtime.Config.MaxChunkCommitsPerFrame = 1
	f.commitStage()
	s1gMetrics(t, f, 1, 2)
	for index := 0; index < 3; index++ {
		s1gPlacement(t, f, s1gID(0, index))
	}
	if placementEntityByIDForStreamedTest(f.cmd, s1gID(1, 1)) != 0 || f.runtime.LoadedChunks[ChunkCoord{}] == nil || f.runtime.LoadedChunks[ChunkCoord{X: 1}] != nil ||
		f.runtime.Metrics.FullChunksCommittedLastFrame != 1 || f.runtime.Metrics.EntitiesCommittedLastFrame != 4 || f.runtime.Metrics.PendingLoadCount != 1 {
		t.Fatal("distinct-chunk gate or completion-only chunk metrics changed")
	}
	s1eWork(t, f.runtime, 1, 4, 0, 0)
	f.runtime.Config.MaxChunkCommitsPerFrame = 3
	f.runtime.Config.MaxPlacementCommitUnitsPerFrame = 1
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, true)
	f.commitStage()
	s1gMetrics(t, f, 0, 1)
	s1gPlacement(t, f, s1gID(1, 1))
	proxy := f.runtime.LoadedSectorProxies[ChunkCoord{}]
	if proxy == nil || len(f.runtime.LoadedChunks) != 2 || f.runtime.Metrics.ProxyChunksCommittedLastFrame != 1 ||
		f.runtime.Metrics.FullChunksCommittedLastFrame != 1 || f.runtime.Metrics.EntitiesCommittedLastFrame != 3 || f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.PendingLoadCount != 0 {
		t.Fatal("proxy and active placement progress did not share the ready frontier")
	}
	f.status(proxy.Entity, StreamedVoxelRenderReady)
	f.commitStage()
	s1eWork(t, f.runtime, 0, 4, 0, 0)
}

func TestS1gAgedActivePlacementAllowsLaterProxyAfterUnitCap(t *testing.T) {
	var f *s1gFixture
	finalFrame, placementBeforeProxy := false, false
	f = s1gRuntime(t, []int{2}, 1, true, true, func(_ *Commands, context PostSpawnPlacementContext) {
		if context.Placement.PlacementID != s1gID(0, 1) {
			return
		}
		if !finalFrame || f.runtime.LoadedSectorProxies[ChunkCoord{}] != nil {
			t.Fatal("aged placement did not run before the final competing proxy")
		}
		placementBeforeProxy = true
	})
	f.runtime.Config.MaxChunkCommitsPerFrame = 1
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	s1gPlacement(t, f, s1gID(0, 0))
	s1gMetrics(t, f, 1, 1)
	bytes := f.runtime.Metrics.PendingPreparedBytes
	if bytes <= 0 {
		t.Fatal("active placement did not retain actual prepared bytes")
	}
	f.status(terrainChunkEntityByCoordForStreamedTest(f.cmd, [3]int{}), StreamedVoxelRenderReady)
	f.status(importedWorldChunkEntityByCoordForStreamedTest(f.cmd, [3]int{}), StreamedVoxelRenderReady)
	// Fresh fallback work wins until the untouched active unit ages to priority zero.
	for range 7 {
		s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, true)
		f.commitStage()
		s1gMetrics(t, f, 1, 0)
		proxy := f.runtime.LoadedSectorProxies[ChunkCoord{}]
		if proxy == nil || placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 1)) != 0 ||
			f.runtime.LoadedChunks[ChunkCoord{}] != nil || f.runtime.Metrics.ProxyChunksCommittedLastFrame != 1 ||
			f.runtime.Metrics.FullChunksCommittedLastFrame != 0 || f.runtime.Metrics.PendingPreparedBytes != bytes || f.runtime.Metrics.PendingLoadCount != 1 {
			t.Fatal("fresh fallback frame advanced or released the waiting active placement")
		}
		f.status(proxy.Entity, StreamedVoxelRenderReady)
		refreshStreamedRenderResidency(f.cmd, f.runtime)
		unloadStreamedSectorProxy(f.cmd, f.runtime, ChunkCoord{})
		f.app.FlushCommands()
	}
	f.runtime.Config.MaxChunkCommitsPerFrame = 3
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, true)
	finalFrame = true
	f.commitStage()
	s1gMetrics(t, f, 0, 1)
	s1gPlacement(t, f, s1gID(0, 1))
	if !placementBeforeProxy || f.runtime.LoadedChunks[ChunkCoord{}] == nil || f.runtime.LoadedSectorProxies[ChunkCoord{}] == nil ||
		f.runtime.Metrics.FullChunksCommittedLastFrame != 1 || f.runtime.Metrics.ProxyChunksCommittedLastFrame != 1 ||
		f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.PendingLoadCount != 0 {
		t.Fatal("proxy did not progress after the aged placement exhausted the shared unit cap")
	}
}

func TestS1gDefaultCPUOnlyAndSynchronousSameCoordinateKeepAtomicCompatibility(t *testing.T) {
	for _, mode := range []struct {
		name    string
		units   int
		managed bool
	}{{"default", 0, true}, {"nonpositive", -1, true}, {"CPU-only", 1, false}} {
		t.Run(mode.name, func(t *testing.T) {
			f := s1gRuntime(t, []int{3}, mode.units, mode.managed, false)
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			if f.runtime.LoadedChunks[ChunkCoord{}] == nil || f.runtime.Metrics.ActiveChunkCommitCount != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("default/nonpositive/CPU-only commit stopped being synchronous")
			}
			for index := 0; index < 3; index++ {
				s1gPlacement(t, f, s1gID(0, index))
			}
		})
	}
	f := s1gRuntime(t, []int{3}, 1, true, false)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	root, body := s1gPlacement(t, f, s1gID(0, 0))
	s1gMetrics(t, f, 1, 1)
	prepared := f.runtime.Metrics.PreparedChunkCount
	if err := ensureStreamedChunkLoadedForPosition(f.cmd, f.assets, f.runtime, content.Vec3{1, 1, 1}); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	if f.runtime.LoadedChunks[ChunkCoord{}] == nil || f.runtime.Metrics.ActiveChunkCommitCount != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 ||
		f.runtime.Metrics.PreparedChunkCount != prepared || placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 0)) != root || placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "body") != body {
		t.Fatal("synchronous same-coordinate finish duplicated preparation or existing entities")
	}
	for index := 0; index < 3; index++ {
		s1gPlacement(t, f, s1gID(0, index))
	}
	s1eWork(t, f.runtime, 0, 4, 0, 0)
}

func TestS1gRemainingUnitsResolveCurrentDeletionMoveTransformAndSnapshot(t *testing.T) {
	f := s1gRuntime(t, []int{4}, 1, true, false)
	oldPath, newPath := filepath.Join(t.TempDir(), "old.gkvoxobj"), filepath.Join(t.TempDir(), "new.gkvoxobj")
	for path, value := range map[string]uint8{oldPath: 3, newPath: 9} {
		if err := content.SaveVoxelObjectSnapshot(path, &content.VoxelObjectSnapshotDef{Voxels: []content.VoxelObjectVoxelDef{{Value: value}}}); err != nil {
			t.Fatal(err)
		}
	}
	key := voxelObjectRuntimeKey(s1gID(0, 3), "body")
	f.runtime.voxelOverrideMap[key] = content.VoxelObjectOverrideDef{PlacementID: s1gID(0, 3), ItemID: "body", SnapshotPath: oldPath}
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	root, completed := s1gPlacement(t, f, s1gID(0, 0))
	s1gEdit(t, f, completed, [3]int{}, 6)
	f.runtime.deletedPlacementIDs[s1gID(0, 1)] = struct{}{}
	move := content.LevelTransformDef{Position: content.Vec3{1601, 1, 1}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}
	f.runtime.placementOverrideMap[s1gID(0, 2)] = move
	current := content.LevelTransformDef{Position: content.Vec3{7, 2, 3}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{2, 1, 1}}
	f.runtime.placementOverrideMap[s1gID(0, 3)] = current
	f.runtime.voxelOverrideMap[key] = content.VoxelObjectOverrideDef{PlacementID: s1gID(0, 3), ItemID: "body", SnapshotPath: newPath}
	for skipped := 0; skipped < 2; skipped++ {
		f.commitStage()
		s1gMetrics(t, f, 1, 1)
		if len(f.runtime.LoadedChunks) != 0 || f.runtime.Metrics.EntitiesCommittedLastFrame != 0 || len(f.hooks) != 1 {
			t.Fatal("skipped identity bypassed the unit bound or respawned stale placement authority")
		}
	}
	f.commitStage()
	s1gMetrics(t, f, 0, 1)
	remainingRoot, remaining := s1gPlacement(t, f, s1gID(0, 3))
	if placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 1)) != 0 || placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 2)) != 0 ||
		placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 0)) != root || f.runtime.LoadedChunks[ChunkCoord{}] == nil {
		t.Fatal("remaining cursor units ignored deletion/movement or reapplied a completed unit")
	}
	transform := s3cComponent[TransformComponent](t, f.cmd, remainingRoot)
	if transform.Position != (mgl32.Vec3{7, 2, 3}) || transform.Scale != (mgl32.Vec3{2, 1, 1}) {
		t.Fatal("remaining placement used its stale captured transform")
	}
	s1gValue(t, f, remaining, [3]int{}, 9)
	s1gValue(t, f, completed, [3]int{}, 6)
}

func TestS1gPartialCancellationPersistenceFailurePinsOwnersUntilSuccessfulStopAndReload(t *testing.T) {
	f := s1gRuntime(t, []int{3}, 1, true, true)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	s1gMetrics(t, f, 1, 1)
	_, placement := s1gPlacement(t, f, s1gID(0, 0))
	terrain := terrainChunkEntityByCoordForStreamedTest(f.cmd, [3]int{})
	imported := importedWorldChunkEntityByCoordForStreamedTest(f.cmd, [3]int{})
	terrainModel := mustVoxelModelComponentForLevelTest(t, f.cmd, terrain)
	originalTerrainAsset := terrainModel.GeometryAsset()
	entities := []EntityId{terrain, imported, placement}
	points := [][3]int{{1, 0, 1}, {}, {}}
	values := []uint8{9, 8, 7}
	for index, entity := range entities {
		s1gEdit(t, f, entity, points[index], values[index])
	}
	f.status(terrain, StreamedVoxelRenderReady)
	f.status(imported, StreamedVoxelRenderReady)
	refreshStreamedRenderResidency(f.cmd, f.runtime)
	s1eWork(t, f.runtime, 1, 4, 0, 0)
	bytes := f.runtime.Metrics.PendingPreparedBytes
	config, generation := f.runtime.Config, f.runtime.Generation
	f.runtime.Config.MaxStreamingWorkItems = 1
	s3aMove(f.cmd, f.observer, mgl32.Vec3{1600, 1, 1})
	f.runtime.WorldDelta.SchemaVersion = -1
	t.Cleanup(func() {
		if f.runtime.WorldDelta != nil {
			f.runtime.WorldDelta.SchemaVersion = content.CurrentWorldDeltaSchemaVersion
		}
	})
	release := s4cHoldImportedWriter(t, f.cmd, f.runtime)
	defer release()
	if f.runtime.Metrics.PendingPersistenceCount != 1 || f.runtime.Metrics.DirtyPinnedChunkCount != 1 || f.runtime.Metrics.PendingPersistenceBytes == 0 || len(f.runtime.LoadedChunks) != 0 {
		t.Fatal("cancelled partial owner was not captured and pinned without LoadedChunks publication")
	}
	f.commitStage()
	if f.runtime.Metrics.ActiveChunkCommitCount != 1 || f.runtime.Metrics.PendingPreparedBytes != bytes || len(f.hooks) != 1 {
		t.Fatal("held cancellation released its envelope or advanced a remaining placement")
	}
	release()
	s4cDrive(t, f.cmd, f.runtime, func() bool { return f.runtime.Metrics.PersistenceFailureCount > 0 })
	f.runtime.Config.MaxStreamingWorkItems = 4
	for index, entity := range entities {
		if !f.cmd.EntityExists(entity) {
			t.Fatal("failed cancellation persistence discarded a partial entity")
		}
		s1gValue(t, f, entity, points[index], values[index])
	}
	if f.runtime.InitErr != nil || f.runtime.Metrics.ActiveChunkCommitCount != 1 || f.runtime.Metrics.PendingPreparedBytes != bytes || f.runtime.Metrics.PendingLoadCount != 1 {
		t.Fatal("failed cancellation lost retryable partial admission or set a fatal runtime error")
	}
	if err := StopStreamedLevelRuntime(f.cmd); err == nil {
		t.Fatal("invalid manifest did not reject Stop")
	}
	if !f.runtime.Initialized || f.runtime.Generation != generation || f.runtime.Metrics.PendingPreparedBytes != bytes || f.runtime.Metrics.ActiveChunkCommitCount != 1 {
		t.Fatal("failed Stop drained an unpersisted active placement transaction")
	}
	s1eWork(t, f.runtime, 1, 4, 0, 0)
	f.runtime.WorldDelta.SchemaVersion = content.CurrentWorldDeltaSchemaVersion
	deltaPath := f.runtime.WorldDeltaPath
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	for _, entity := range entities {
		if f.cmd.EntityExists(entity) {
			t.Fatal("successful Stop retained partial world entities")
		}
	}
	if _, exists := f.assets.GetVoxelGeometry(originalTerrainAsset); exists {
		t.Fatal("successful Stop leaked the exact adopted partial terrain asset")
	}
	if f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.ActiveChunkCommitCount != 0 || f.runtime.Metrics.PreparedQueueDepth != 0 {
		t.Fatal("successful Stop retained active prepared ownership")
	}
	delta := s4aLoadDelta(t, deltaPath)
	if len(delta.TerrainChunkOverrides) != 1 || len(delta.ImportedWorldChunkOverrides) != 1 || len(delta.VoxelObjectOverrides) != 1 {
		t.Fatalf("partial Stop did not durably classify all three edited kinds: %+v", delta)
	}
	if err := StartStreamedLevelRuntime(f.cmd, f.assets, config); err != nil {
		t.Fatal(err)
	}
	s3aMove(f.cmd, f.observer, mgl32.Vec3{1, 1, 1})
	f.app.FlushCommands()
	updateStreamedObserverSelection(f.cmd, f.runtime)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	reloaded := []EntityId{terrainChunkEntityByCoordForStreamedTest(f.cmd, [3]int{}), importedWorldChunkEntityByCoordForStreamedTest(f.cmd, [3]int{}), placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "body")}
	for index, entity := range reloaded {
		s1gValue(t, f, entity, points[index], values[index])
	}
}

func TestS1gFatalPartialSpawnAndHookFailureRetainTransactionWithoutRetry(t *testing.T) {
	for _, failure := range []string{"partial-spawn", "hook-signalled", "partial-spawn-group"} {
		t.Run(failure, func(t *testing.T) {
			var f *s1gFixture
			hook := func(_ *Commands, _ PostSpawnPlacementContext) {
				if failure == "hook-signalled" {
					f.runtime.InitErr = fmt.Errorf("S1g hook failure")
				}
			}
			f = s1gRuntime(t, []int{2}, 1, true, false, hook)
			if failure == "partial-spawn" || failure == "partial-spawn-group" {
				path := filepath.Join(filepath.Dir(f.runtime.LevelPath), "placement.gkasset")
				asset, err := content.LoadAsset(path)
				if err != nil {
					t.Fatal(err)
				}
				vox := filepath.Join(filepath.Dir(path), "valid.vox")
				writeNamedSceneVoxFixture(t, vox)
				if failure == "partial-spawn-group" {
					asset.Parts = append(asset.Parts, content.AssetPartDef{ID: "group", Name: "group", Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup}, Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}})
				}
				asset.Parts = append(asset.Parts, content.AssetPartDef{ID: "bad-later-part", Name: "bad later part", Source: content.AssetSourceDef{Kind: content.AssetSourceKindVoxModel, Path: "valid.vox", ModelIndex: 99}, Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}})
				if validation := content.ValidateAsset(asset, content.AssetValidationOptions{DocumentPath: path}); validation.HasErrors() {
					t.Fatalf("invalid partial spawn fixture: %s", validation.Error())
				}
				if err := content.SaveAsset(path, asset); err != nil {
					t.Fatal(err)
				}
			}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			bytes := f.runtime.Metrics.PendingPreparedBytes
			commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
			f.app.FlushCommands()
			var roots, parts []EntityId
			MakeQuery1[AuthoredAssetRootComponent](f.cmd).Map(func(entity EntityId, ref *AuthoredAssetRootComponent) bool {
				if ref.AssetID == "s1g-asset" {
					roots = append(roots, entity)
				}
				return true
			})
			MakeQuery1[AuthoredAssetRefComponent](f.cmd).Map(func(entity EntityId, ref *AuthoredAssetRefComponent) bool {
				if ref.AssetID == "s1g-asset" && ref.ItemID == "body" {
					parts = append(parts, entity)
				}
				return true
			})
			if f.runtime.InitErr == nil || len(roots) != 1 || len(parts) != 1 || len(f.runtime.LoadedChunks) != 0 || f.runtime.Metrics.ActiveChunkCommitCount != 1 || f.runtime.Metrics.PendingPreparedBytes != bytes {
				t.Fatal("fatal unit did not pin its real partially spawned ownership")
			}
			s1eWork(t, f.runtime, 1, 4, 0, 0)
			hooks := f.hooks[s1gID(0, 0)]
			commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
			f.app.FlushCommands()
			if f.hooks[s1gID(0, 0)] != hooks || placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 1)) != 0 {
				t.Fatal("fatal placement was blindly retried or completed hooks reran")
			}
			if failure == "partial-spawn-group" {
				var group EntityId
				MakeQuery1[AuthoredAssetRefComponent](f.cmd).Map(func(entity EntityId, ref *AuthoredAssetRefComponent) bool {
					if ref.AssetID == "s1g-asset" && ref.ItemID == "group" {
						group = entity
					}
					return true
				})
				if group == 0 || hasComponentOfType[VoxelModelComponent](f.cmd, group) {
					t.Fatal("partial group fixture did not produce an ordinary nonvoxel entity")
				}
				f.cmd.RemoveEntity(group)
				f.app.FlushCommands()
			}
			s1gEdit(t, f, parts[0], [3]int{}, 7)
			deltaPath := f.runtime.WorldDeltaPath
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			if f.cmd.EntityExists(roots[0]) || f.cmd.EntityExists(parts[0]) || f.runtime.Metrics.ActiveChunkCommitCount != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("fatal transaction Stop leaked partial spawn IDs or prepared ownership")
			}
			s1eWork(t, f.runtime, 0, 4, 0, 0)
			if delta := s4aLoadDelta(t, deltaPath); len(delta.VoxelObjectOverrides) != 1 || delta.VoxelObjectOverrides[0].PlacementID != s1gID(0, 0) || delta.VoxelObjectOverrides[0].ItemID != "body" {
				t.Fatal("partial spawn error lost placement/item persistence classification")
			}
		})
	}
}

func TestS1gHookStopDrainsActiveAndReadyOwnershipWithoutRepublishing(t *testing.T) {
	var f *s1gFixture
	stops := 0
	hook := func(cmd *Commands, _ PostSpawnPlacementContext) {
		stops++
		if err := StopStreamedLevelRuntime(cmd); err != nil {
			t.Fatal(err)
		}
	}
	f = s1gRuntime(t, []int{2, 2}, 2, true, false, hook)
	for _, coord := range []ChunkCoord{{}, {X: 1}} {
		s1fPrepared(t, f.streamedRenderHarness, coord, false)
	}
	commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
	f.app.FlushCommands()
	if stops != 1 || f.runtime.Initialized || len(f.runtime.LoadedChunks) != 0 || f.runtime.Metrics.ActiveChunkCommitCount != 0 ||
		f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.PreparedQueueDepth != 0 || f.runtime.Metrics.PendingLoadCount != 0 {
		t.Fatal("hook Stop did not cut off publication and drain the captured frontier")
	}
	for chunk := 0; chunk < 2; chunk++ {
		for placement := 0; placement < 2; placement++ {
			if placementEntityByIDForStreamedTest(f.cmd, s1gID(chunk, placement)) != 0 {
				t.Fatal("outer placement service respawned entities after hook Stop")
			}
		}
	}
	s1eWork(t, f.runtime, 0, 4, 0, 0)
}

func TestS1gCancelledActiveChunkReleasesAfterDurableSaveAtAllowanceOne(t *testing.T) {
	f := s1gRuntime(t, []int{2}, 1, true, true)
	f.runtime.Config.MaxStreamingWorkItems = 1
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	root, body := s1gPlacement(t, f, s1gID(0, 0))
	terrain := terrainChunkEntityByCoordForStreamedTest(f.cmd, [3]int{})
	imported := importedWorldChunkEntityByCoordForStreamedTest(f.cmd, [3]int{})
	f.status(terrain, StreamedVoxelRenderReady)
	f.status(imported, StreamedVoxelRenderReady)
	refreshStreamedRenderResidency(f.cmd, f.runtime)
	reconcileStreamedRenderResidency(f.cmd, f.runtime)
	f.app.FlushCommands()
	f.visibility(imported, true)
	s1gMetrics(t, f, 1, 1)
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	if len(f.runtime.LoadedChunks) != 0 || len(f.runtime.LoadedSectorProxies) != 0 {
		t.Fatal("partial imported coverage was published before its placement cohort completed")
	}
	for index, entity := range []EntityId{terrain, imported, body} {
		s1gEdit(t, f, entity, [][3]int{{1, 0, 1}, {}, {}}[index], uint8(9-index))
	}
	s3aMove(f.cmd, f.observer, mgl32.Vec3{1600, 1, 1})
	f.app.FlushCommands()
	for range 20 {
		f.observerStage()
		f.app.FlushCommands()
		if f.runtime.Metrics.ActiveChunkCommitCount == 0 {
			break
		}
		f.runtime.jobs.Wait()
		f.app.FlushCommands()
		f.commitStage()
	}
	delta := s4aLoadDelta(t, f.runtime.WorldDeltaPath)
	if len(delta.TerrainChunkOverrides) != 1 || len(delta.ImportedWorldChunkOverrides) != 1 || len(delta.VoxelObjectOverrides) != 1 {
		t.Fatal("cancelled partial edits did not reach the durable checkpoint")
	}
	if f.runtime.Metrics.ActiveChunkCommitCount != 0 || f.runtime.Metrics.PendingLoadCount != 0 || len(f.runtime.LoadedSectorProxies) != 0 {
		t.Fatalf("durably saved active chunk still waits for fallback capacity: active=%d pending=%d proxies=%d",
			f.runtime.Metrics.ActiveChunkCommitCount, f.runtime.Metrics.PendingLoadCount, len(f.runtime.LoadedSectorProxies))
	}
	for _, entity := range []EntityId{terrain, imported, root, body} {
		if f.cmd.EntityExists(entity) {
			t.Fatal("durably cancelled partial chunk retained an owned entity")
		}
	}
	if f.hooks[s1gID(0, 0)] != 1 || f.hooks[s1gID(0, 1)] != 0 || placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 1)) != 0 {
		t.Fatal("cancellation reran or advanced placement hooks")
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	if f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.PendingPersistenceCount != 0 {
		t.Fatal("Stop did not drain work admitted after automatic active cleanup")
	}
	s1eWork(t, f.runtime, 0, 1, 0, 0)
}

func TestS1gLateFailedStopRetainsCleanActiveEnvelopeUntilRetry(t *testing.T) {
	f := s1gRuntime(t, []int{2, 1}, 2, true, false)
	for _, coord := range []ChunkCoord{{}, {X: 1}} {
		s1fPrepared(t, f.streamedRenderHarness, coord, false)
	}
	f.commitStage()
	partialRoot, partialBody := s1gPlacement(t, f, s1gID(0, 0))
	completedRoot, completedBody := s1gPlacement(t, f, s1gID(1, 0))
	s1gMetrics(t, f, 1, 2)
	bytes := f.runtime.Metrics.PendingPreparedBytes
	if bytes <= 0 || f.runtime.Metrics.PendingLoadCount != 1 || f.runtime.LoadedChunks[ChunkCoord{}] != nil ||
		f.runtime.LoadedChunks[ChunkCoord{X: 1}] == nil || placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 1)) != 0 {
		t.Fatal("late Stop fixture did not retain one clean active chunk beside a completed chunk")
	}
	s1eWork(t, f.runtime, 1, 4, 0, 0)
	s1gEdit(t, f, completedBody, [3]int{}, 7)
	dataDir, deltaPath, generation := f.runtime.WorldDataDir, f.runtime.WorldDeltaPath, f.runtime.Generation
	blocker := filepath.Join(t.TempDir(), "payload-blocker")
	if err := os.WriteFile(blocker, []byte("regular file"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.runtime.WorldDataDir = blocker
	t.Cleanup(func() { f.runtime.WorldDataDir = dataDir })
	if err := StopStreamedLevelRuntime(f.cmd); err == nil {
		t.Fatal("completed edited payload did not reject a regular file as its data directory")
	}
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	if !f.runtime.Initialized || f.runtime.Generation != generation || f.runtime.Metrics.ActiveChunkCommitCount != 1 ||
		f.runtime.Metrics.PendingLoadCount != 1 || f.runtime.Metrics.PendingPreparedBytes != bytes || f.runtime.LoadedChunks[ChunkCoord{X: 1}] == nil {
		t.Fatal("late failed Stop released the clean active chunk's exact pending ownership")
	}
	entities := []EntityId{partialRoot, partialBody, completedRoot, completedBody}
	for _, entity := range entities {
		if !f.cmd.EntityExists(entity) {
			t.Fatal("late failed Stop discarded an original active or completed entity")
		}
	}
	s1eWork(t, f.runtime, 1, 4, 0, 0)
	f.runtime.WorldDataDir = dataDir
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	for _, entity := range entities {
		if f.cmd.EntityExists(entity) {
			t.Fatal("successful Stop retry retained an active or completed entity")
		}
	}
	if f.runtime.Metrics.ActiveChunkCommitCount != 0 || f.runtime.Metrics.PendingLoadCount != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("successful Stop retry did not drain the retained active envelope")
	}
	s1eWork(t, f.runtime, 0, 4, 0, 0)
	delta := s4aLoadDelta(t, deltaPath)
	if len(delta.VoxelObjectOverrides) != 1 || delta.VoxelObjectOverrides[0].PlacementID != s1gID(1, 0) || delta.VoxelObjectOverrides[0].ItemID != "body" {
		t.Fatal("successful Stop retry did not save the completed chunk's edit")
	}
	s4aVoxel(t, s4aLoadPayload(t, "object", s4aPayloadPath(t, delta, "object", deltaPath)), 0, 7)
}

func TestS1gCancelledPreparedCleanupDoesNotSpendOrWaitForChunkAllowance(t *testing.T) {
	for _, moment := range []string{"before-frontier", "after-placement"} {
		t.Run(moment, func(t *testing.T) {
			var f *s1gFixture
			hook := func(_ *Commands, context PostSpawnPlacementContext) {
				if moment == "after-placement" && context.Placement.PlacementID == s1gID(0, 0) {
					cancelStreamedPreparation(f.runtime.chunkPrepareCancels[ChunkCoord{X: 1}])
				}
			}
			f = s1gRuntime(t, []int{1, 1}, 1, true, false, hook)
			f.runtime.Config.MaxChunkCommitsPerFrame = 1
			for _, coord := range []ChunkCoord{{}, {X: 1}} {
				s1fPrepared(t, f.streamedRenderHarness, coord, false)
			}
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if f.runtime.Metrics.PendingLoadCount != 2 || f.runtime.Metrics.PendingPreparedBytes <= 0 {
				t.Fatal("cleanup fixture did not retain both actual prepared attempts")
			}
			if moment == "before-frontier" {
				cancelStreamedPreparation(f.runtime.chunkPrepareCancels[ChunkCoord{X: 1}])
			}
			f.commitStage()
			if f.runtime.LoadedChunks[ChunkCoord{}] == nil || f.runtime.LoadedChunks[ChunkCoord{X: 1}] != nil ||
				f.runtime.Metrics.FullChunksCommittedLastFrame != 1 || f.runtime.Metrics.PlacementCommitUnitsLastFrame != 1 ||
				f.runtime.Metrics.PendingLoadCount != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.ActiveChunkCommitCount != 0 {
				t.Fatalf("cancelled cleanup consumed or waited for the chunk allowance: completed=%d units=%d pending=%d bytes=%d active=%d",
					f.runtime.Metrics.FullChunksCommittedLastFrame, f.runtime.Metrics.PlacementCommitUnitsLastFrame,
					f.runtime.Metrics.PendingLoadCount, f.runtime.Metrics.PendingPreparedBytes, f.runtime.Metrics.ActiveChunkCommitCount)
			}
			s1gPlacement(t, f, s1gID(0, 0))
			if f.hooks[s1gID(1, 0)] != 0 || placementEntityByIDForStreamedTest(f.cmd, s1gID(1, 0)) != 0 {
				t.Fatal("cancelled prepared placement published entities or ran its hook")
			}
			s1eWork(t, f.runtime, 0, 4, 0, 0)
		})
	}
}

func TestS1gPlacementFreeCommitReportsExactChunkAllowanceSpent(t *testing.T) {
	f := s1gRuntime(t, []int{0}, 1, true, true)
	f.runtime.Config.MaxChunkCommitsPerFrame = 1
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	if f.runtime.LoadedChunks[ChunkCoord{}] == nil || f.runtime.Metrics.FullChunksCommittedLastFrame != 1 ||
		f.runtime.Metrics.PlacementCommitUnitsLastFrame != 0 || f.runtime.Metrics.ActiveChunkCommitCount != 0 || f.runtime.Metrics.PreparedQueueDepth != 0 ||
		!f.runtime.Metrics.CommitBudgetHitLastFrame || f.runtime.Metrics.CommitBudgetReason != "chunk_count" {
		t.Fatal("placement-free completion did not report the exact chunk allowance spent")
	}
	f.commitStage()
	if f.runtime.Metrics.CommitBudgetHitLastFrame || f.runtime.Metrics.CommitBudgetReason != "" || f.runtime.Metrics.PlacementCommitUnitsLastFrame != 0 {
		t.Fatal("empty next frontier retained the previous frame's budget diagnostic")
	}
}
