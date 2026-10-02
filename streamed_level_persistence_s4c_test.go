package gekko

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s4cDrive(t *testing.T, cmd *Commands, state *StreamedLevelRuntimeState, done func() bool) {
	t.Helper()
	for frame := 0; frame < 20; frame++ {
		updateStreamedLevelObserverSystem(cmd, state)
		state.jobs.Wait()
		cmd.app.FlushCommands()
		if done() {
			return
		}
	}
	t.Fatalf("persistence did not finish: loaded=%d metrics=%+v error=%v", len(state.LoadedChunks), state.Metrics, state.InitErr)
}

func s4cLive(t *testing.T, cmd *Commands, assets *AssetServer, entity EntityId) *volume.XBrickMap {
	t.Helper()
	model := mustVoxelModelComponentForLevelTest(t, cmd, entity)
	live, ok := ResolveVoxelGeometryMap(assets, &model)
	if !ok {
		t.Fatal("fixture geometry missing")
	}
	return live
}

func s4cAliasEdit(t *testing.T, live *volume.XBrickMap, value uint8) {
	t.Helper()
	for _, sector := range live.Sectors {
		if brick := sector.GetBrick(0, 0, 0); brick != nil {
			brick.Payload[0][0][0] = value
			live.ClearDirty()
			return
		}
	}
	t.Fatal("fixture brick missing")
}

func s4cDurableValue(t *testing.T, state *StreamedLevelRuntimeState, kind string, value uint8) {
	t.Helper()
	delta := s4aLoadDelta(t, state.WorldDeltaPath)
	s4aVoxel(t, s4aLoadPayload(t, kind, s4aPayloadPath(t, delta, kind, state.WorldDeltaPath)), 0, value)
}

// Advance the observer until the real IO dependency is entered. Worker dispatch
// may span updates; never join a job while the dependency intentionally holds it.
func s4cDriveUntilWriterEntered(t *testing.T, cmd *Commands, state *StreamedLevelRuntimeState, entered <-chan struct{}) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-entered:
			return
		case <-deadline.C:
			t.Fatal("observer did not dispatch manifest publication")
		case <-tick.C:
			updateStreamedLevelObserverSystem(cmd, state)
		}
	}
}

// Only one test goroutine owns ECS while the caller waits. The mutex holds the
// real imported payload writer; time is only a guard against a blocked observer.
func s4cHoldImportedWriter(t *testing.T, cmd *Commands, state *StreamedLevelRuntimeState) func() {
	t.Helper()
	state.runtimeEditPersistenceMu.Lock()
	var once sync.Once
	release := func() { once.Do(state.runtimeEditPersistenceMu.Unlock) }
	t.Cleanup(release)
	done := make(chan struct{})
	go func() {
		updateStreamedLevelObserverSystem(cmd, state)
		close(done)
	}()
	select {
	case <-done:
		return release
	case <-time.After(time.Second):
		release()
		<-done
		t.Fatal("observer unload blocked on imported payload IO")
		return release
	}
}

func TestS4cObserverUnloadDoesNotWaitForImportedPayloadIO(t *testing.T) {
	_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{MaxPreparedGeometryCacheBytes: 1, MaxPendingPersistenceBytes: 1})
	entity, geometry := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "s4c-first")
	second, secondGeometry := s2aCommitImported(t, cmd, assets, state, ChunkCoord{X: 1}, "s4c-second")
	MarkVoxelEntityPersistenceDirty(cmd, entity)
	MarkVoxelEntityPersistenceDirty(cmd, second)
	cmd.app.FlushCommands()
	release := s4cHoldImportedWriter(t, cmd, state)
	defer release()
	if state.LoadedChunks[ChunkCoord{}] == nil || !cmd.EntityExists(entity) {
		t.Fatal("held persistence released live imported chunk/entity ownership")
	}
	s2aAssertAsset(t, assets, geometry, true)
	s2aAssertAsset(t, assets, secondGeometry, true)
	m := state.Metrics
	if !cmd.EntityExists(second) || len(state.LoadedChunks) != 2 || m.DirtyPinnedChunkCount != 2 || m.PendingPersistenceCount != 1 || m.PendingPersistenceBytes <= 1 || m.PendingPersistenceMaxBytes != 1 || m.PendingPersistenceOverBudgetBytes != m.PendingPersistenceBytes-1 || m.PendingPersistenceOversizedAdmissions != 1 {
		t.Fatalf("held transaction did not bound snapshots while pinning both dirty chunks: %+v", m)
	}
	updateStreamedLevelObserverSystem(cmd, state)
	if state.Metrics.PendingPersistenceCount != 1 || state.Metrics.PendingPersistenceBytes != m.PendingPersistenceBytes || state.Metrics.PendingPersistenceAdmissionRetries == 0 {
		t.Fatalf("busy unload allocated another snapshot or lost admission pressure: %+v", state.Metrics)
	}
	release()
	s4cDrive(t, cmd, state, func() bool { return len(state.LoadedChunks) == 0 })
	if state.Metrics.PendingPersistenceCount != 0 || state.Metrics.PendingPersistenceBytes != 0 || state.Metrics.DirtyPinnedChunkCount != 0 {
		t.Fatalf("completed unload retained persistence ownership: %+v", state.Metrics)
	}
	s2aAssertAsset(t, assets, geometry, false)
	s2aAssertAsset(t, assets, secondGeometry, false)
	delta := s4aLoadDelta(t, state.WorldDeltaPath)
	if len(delta.ImportedWorldChunkOverrides) != 2 {
		t.Fatalf("missing independently saved chunks: %+v", delta)
	}
	for _, ref := range delta.ImportedWorldChunkOverrides {
		s4aVoxel(t, s4aLoadPayload(t, "imported", content.ResolveDocumentPath(ref.SnapshotPath, state.WorldDeltaPath)), 0, 1)
	}
}

func TestS4cPersistenceBudgetConfiguration(t *testing.T) {
	_, _, state, _ := s2aStartRuntime(t, StreamedLevelRuntimeConfig{})
	if state.Metrics.PendingPersistenceMaxBytes != 128<<20 || state.Metrics.PendingPersistenceCount != 0 || state.Metrics.PendingPersistenceBytes != 0 {
		t.Fatalf("default persistence budget: %+v", state.Metrics)
	}
	app, cmd, _ := newStreamedRuntimeHarness(t)
	path := filepath.Join(t.TempDir(), "negative.gklevel")
	if err := content.SaveLevel(path, content.NewLevelDef("negative")); err != nil {
		t.Fatal(err)
	}
	if err := StartStreamedLevelRuntime(cmd, assetServerFromApp(app), StreamedLevelRuntimeConfig{LevelPath: path, MaxPendingPersistenceBytes: -1}); err == nil {
		t.Fatal("negative persistence budget accepted")
	}
}

func TestS4cObserverDurableContentRoundTrips(t *testing.T) {
	for _, kind := range []string{"terrain", "imported", "object", "missing_object", "backing"} {
		t.Run(kind, func(t *testing.T) {
			app, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{})
			state.Level.ChunkSize = 16
			geometry := volume.NewXBrickMap()
			geometry.SetVoxel(0, 0, 0, 9)
			if kind == "terrain" {
				geometry.SetVoxel(0, 2, 0, 9)
			}
			model := VoxelModelComponent{VoxelModel: assets.RegisterSharedVoxelGeometry(geometry, ""), VoxelResolution: 1, TerrainChunkSize: 16}
			components := []any{model, VoxelPersistenceDirtyComponent{}}
			loaded := &streamedLoadedChunk{}
			if kind == "terrain" {
				components = append(components, AuthoredTerrainChunkRefComponent{LevelID: state.LevelID, TerrainID: "terrain"})
			}
			if kind == "imported" || kind == "backing" {
				components = append(components, AuthoredImportedWorldChunkRefComponent{LevelID: state.LevelID, WorldID: "world"})
			}
			entity := cmd.AddEntity(components...)
			app.FlushCommands()
			if kind == "terrain" {
				live := s4cLive(t, cmd, assets, entity)
				live.ComputeAABB()
				live.Sectors[[3]int{}].GetBrick(0, 0, 0).Payload[0][6][0] = 9
			}
			loaded.OwnedEntities = map[EntityId]struct{}{entity: {}}
			payloadKind := kind
			switch kind {
			case "terrain":
				loaded.TerrainEntities = map[EntityId]struct{}{entity: {}}
			case "imported", "backing":
				loaded.ImportedWorldEntities = map[EntityId]struct{}{entity: {}}
			default:
				loaded.ObjectEntities = map[string]EntityId{voxelObjectRuntimeKey("placement", "item"): entity}
				payloadKind = "object"
			}
			state.LoadedChunks[ChunkCoord{}] = loaded
			var removal *content.VoxelBackingRemovalDef
			if kind == "backing" {
				removal = &content.VoxelBackingRemovalDef{OwnerKind: content.VoxelBackingOwnerImportedWorld, OwnerID: "world", SourceHash: "source", Bricks: []content.VoxelBackingRemovalBrickDef{{Bits: [16]uint32{1}, Material: 9}}}
				provider := NewTerrainColumnVoxelBacking(&content.TerrainChunkDef{ChunkSize: 16, VoxelResolution: 1, SolidValue: 9, Columns: []content.TerrainChunkColumnDef{{FilledVoxels: 1}}})
				backing := NewVoxelBackingComponent(content.VoxelBackingOwnerImportedWorld, "world", "source", [3]int{}, 16, provider, removal)
				backing.Dirty = true
				cmd.AddComponents(entity, backing)
				s4cLive(t, cmd, assets, entity).SetVoxel(0, 0, 0, 0)
			}
			if kind == "missing_object" {
				cmd.RemoveEntity(entity)
			}
			app.FlushCommands()
			s4cDrive(t, cmd, state, func() bool { return len(state.LoadedChunks) == 0 })
			if cmd.EntityExists(entity) {
				t.Fatal("durable unload retained owned entity")
			}
			delta := s4aLoadDelta(t, state.WorldDeltaPath)
			if kind == "backing" {
				if len(delta.ImportedWorldChunkOverrides) != 0 || !reflect.DeepEqual(delta.VoxelBackingRemovals, []content.VoxelBackingRemovalDef{*removal}) {
					t.Fatalf("backing removal changed representation: %+v", delta)
				}
				return
			}
			value := uint8(9)
			if kind == "missing_object" {
				value = 0
			}
			payload := s4aLoadPayload(t, payloadKind, s4aPayloadPath(t, delta, payloadKind, state.WorldDeltaPath))
			s4aVoxel(t, payload, 0, value)
			if kind == "terrain" {
				if found, color := payload.GetVoxel(0, 2, 0); !found || color != 9 {
					t.Fatal("terrain capture lost observed column height")
				}
				if found, _ := payload.GetVoxel(0, 6, 0); found {
					t.Fatal("terrain capture expanded the serializer's observed AABB")
				}
			}
		})
	}
}

func TestS4cUntrackedEditAndGeometryReplacementRecaptureAfterUpload(t *testing.T) {
	for _, kind := range []string{"payload_alias", "geometry_identity", "legacy_dirty_queue"} {
		t.Run(kind, func(t *testing.T) {
			_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{})
			entity, _ := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "recapture")
			if kind == "legacy_dirty_queue" {
				s4cLive(t, cmd, assets, entity).SetVoxel(0, 0, 0, 2)
			} else {
				MarkVoxelEntityPersistenceDirty(cmd, entity)
				cmd.app.FlushCommands()
			}
			release := s4cHoldImportedWriter(t, cmd, state)
			live := s4cLive(t, cmd, assets, entity)
			if kind == "geometry_identity" {
				geometry := volume.NewXBrickMap()
				geometry.SetVoxel(0, 0, 0, 9)
				s3cComponent[VoxelModelComponent](t, cmd, entity).OverrideGeometry = assets.RegisterSharedVoxelGeometry(geometry, "")
				live = s4cLive(t, cmd, assets, entity)
				live.ClearDirty()
			} else {
				s4cAliasEdit(t, live, 9)
			}
			if kind != "legacy_dirty_queue" && !VoxelEntityPersistenceDirty(cmd, entity) {
				t.Fatal("upload consumed persistence dirty ownership")
			}
			release()
			s4cDrive(t, cmd, state, func() bool { return len(state.LoadedChunks) == 0 })
			s4cDurableValue(t, state, "imported", 9)
		})
	}
}

func TestS4cCheckpointAckKeepsLiveSuccessorAndSerializesFreshOrdinarySave(t *testing.T) {
	for _, kind := range []string{"successor_without_reference", "published_successor_and_backing_alias", "unchanged_return_after_io"} {
		t.Run(kind, func(t *testing.T) {
			published := kind == "published_successor_and_backing_alias"
			successor := kind != "unchanged_return_after_io"
			app, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{})
			entity, _ := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "checkpoint")
			cmd.AddComponents(entity, StreamedDestructionResidentComponent{})
			MarkVoxelEntityPersistenceDirty(cmd, entity)
			app.FlushCommands()
			if published {
				removal := &content.VoxelBackingRemovalDef{OwnerKind: content.VoxelBackingOwnerImportedWorld, OwnerID: "other-world", SourceHash: "source", Bricks: []content.VoxelBackingRemovalBrickDef{{Bits: [16]uint32{1}, Material: 1}}}
				provider := NewTerrainColumnVoxelBacking(&content.TerrainChunkDef{ChunkSize: 16, VoxelResolution: 1, SolidValue: 1, Columns: []content.TerrainChunkColumnDef{{FilledVoxels: 1}}})
				state.recordVoxelBackingRemoval(NewVoxelBackingComponent(content.VoxelBackingOwnerImportedWorld, "other-world", "source", [3]int{}, 16, provider, removal))
			}
			if err := persistChunkOverrides(cmd, state, ChunkCoord{}, state.LoadedChunks[ChunkCoord{}]); err != nil {
				t.Fatal(err)
			}
			live := s4cLive(t, cmd, assets, entity)
			live.SetVoxel(0, 0, 0, 9)
			entered, gate := make(chan struct{}), make(chan struct{})
			var once, releaseOnce sync.Once
			var active, peak atomic.Int32
			release := func() { releaseOnce.Do(func() { close(gate) }) }
			t.Cleanup(func() { release(); state.jobs.Wait(); state.worldDeltaWriter = nil })
			state.worldDeltaWriter = func(path string, delta *content.WorldDeltaDef) error {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old; old = peak.Load() {
					if peak.CompareAndSwap(old, n) {
						break
					}
				}
				once.Do(func() { close(entered); <-gate })
				return content.SaveWorldDelta(path, delta)
			}
			releasePayload := s4cHoldImportedWriter(t, cmd, state)
			releasePayload()
			state.jobs.Wait()
			s4cDriveUntilWriterEntered(t, cmd, state, entered)
			var observer EntityId
			if successor {
				observer = cmd.AddEntity(TransformComponent{}, StreamedLevelObserverComponent{Radius: 1, KeepRadius: 1})
				app.FlushCommands()
				s4cAliasEdit(t, live, 7)
			}
			if published {
				snapshot := s4bSnapshot(content.TerrainChunkCoordDef{}, 7)
				snapshot.WorldID = state.BaseWorldID
				if err := persistImportedWorldRuntimeEditSnapshots(state, []*content.ImportedWorldChunkDef{snapshot}); err != nil {
					t.Fatal(err)
				}
				state.WorldDelta.VoxelBackingRemovals[0].Bricks[0].Bits[0] = 3
			}
			state.WorldDelta.NavigationSourceOverrides = []content.NavigationSourceOverrideDef{{NavID: "nav", SourceHash: "earlier", Empty: true}}
			requestStreamedWorldDeltaSave(state)
			state.WorldDelta.NavigationSourceOverrides[0].SourceHash = "latest"
			requestStreamedWorldDeltaSave(state)
			if successor {
				updateStreamedLevelObserverSystem(cmd, state)
			}
			if !cmd.EntityExists(entity) || state.LoadedChunks[ChunkCoord{}] == nil {
				t.Fatal("held manifest released live successor")
			}
			release()
			state.jobs.Wait()
			// IO completed, but its acknowledgement has not run. The alias remains newer
			// than the durable checkpoint and must survive the next observer update.
			wantLive := uint8(9)
			if successor {
				wantLive = 7
				s4cAliasEdit(t, live, 7)
			} else {
				observer = cmd.AddEntity(TransformComponent{}, StreamedLevelObserverComponent{Radius: 1, KeepRadius: 1})
				app.FlushCommands()
			}
			s4cDrive(t, cmd, state, func() bool {
				delta, err := content.LoadWorldDelta(state.WorldDeltaPath)
				return err == nil && len(delta.NavigationSourceOverrides) == 1 && delta.NavigationSourceOverrides[0].SourceHash == "latest" && state.Metrics.PendingPersistenceCount == 0
			})
			if peak.Load() != 1 {
				t.Fatalf("runtime manifest publications overlapped: peak=%d", peak.Load())
			}
			if !cmd.EntityExists(entity) || state.LoadedChunks[ChunkCoord{}] == nil || !VoxelEntityPersistenceDirty(cmd, entity) {
				t.Fatal("checkpoint acknowledgement discarded demanded live successor")
			}
			s4aVoxel(t, live, 0, wantLive)
			wantCheckpoint := uint8(9)
			if published {
				wantCheckpoint = 7
				delta := s4aLoadDelta(t, state.WorldDeltaPath)
				if len(delta.VoxelBackingRemovals) != 1 || delta.VoxelBackingRemovals[0].Bricks[0].Bits[0] != 3 || state.WorldDelta.VoxelBackingRemovals[0].Bricks[0].Bits[0] != 3 {
					t.Fatal("checkpoint ACK overwrote newer nested backing-removal publication")
				}
			}
			s4cDurableValue(t, state, "imported", wantCheckpoint)
			s4aVoxel(t, s4aLoadPayload(t, "imported", s4aPayloadPath(t, state.WorldDelta, "imported", state.WorldDeltaPath)), 0, wantCheckpoint)
			cmd.RemoveEntity(observer)
			app.FlushCommands()
			s4cDrive(t, cmd, state, func() bool { return len(state.LoadedChunks) == 0 })
			s4cDurableValue(t, state, "imported", wantLive)
		})
	}
}

func TestS4cHeldOrdinaryManifestSharesAdmissionWithDirtyUnload(t *testing.T) {
	_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{MaxPendingPersistenceBytes: 1})
	entered, gate := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	t.Cleanup(func() { release(); state.jobs.Wait(); state.worldDeltaWriter = nil })
	state.worldDeltaWriter = func(path string, delta *content.WorldDeltaDef) error {
		once.Do(func() { close(entered); <-gate })
		return content.SaveWorldDelta(path, delta)
	}
	requestStreamedWorldDeltaSave(state)
	s4cDriveUntilWriterEntered(t, cmd, state, entered)
	updateStreamedLevelObserverSystem(cmd, state)
	m := state.Metrics
	if m.PendingPersistenceCount != 1 || m.PendingPersistenceBytes <= 1 || m.PendingPersistenceMaxBytes != 1 || m.PendingPersistenceOverBudgetBytes != m.PendingPersistenceBytes-1 {
		t.Fatalf("ordinary manifest lacks sole-owner retained byte accounting: %+v", m)
	}
	entity, geometry := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "ordinary-busy")
	s4cLive(t, cmd, assets, entity).SetVoxel(0, 0, 0, 9)
	MarkVoxelEntityPersistenceDirty(cmd, entity)
	cmd.app.FlushCommands()
	done := make(chan struct{})
	go func() { updateStreamedLevelObserverSystem(cmd, state); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		release()
		<-done
		t.Fatal("dirty unload waited for ordinary manifest IO")
	}
	if !cmd.EntityExists(entity) || state.LoadedChunks[ChunkCoord{}] == nil || state.Metrics.DirtyPinnedChunkCount != 1 || state.Metrics.PendingPersistenceCount != 1 || state.Metrics.PendingPersistenceBytes != m.PendingPersistenceBytes {
		t.Fatalf("ordinary publication and dirty unload retained independent snapshots or lost live ownership: %+v", state.Metrics)
	}
	asset, ok := assets.GetVoxelGeometry(geometry)
	if !ok || asset.XBrickMap == nil {
		t.Fatal("held ordinary save lost usable live geometry")
	}
	s4aVoxel(t, asset.XBrickMap, 0, 9)
	release()
	s4cDrive(t, cmd, state, func() bool { return len(state.LoadedChunks) == 0 && state.Metrics.PendingPersistenceCount == 0 })
	s4cDurableValue(t, state, "imported", 9)
}

func TestS4cObserverFailureRetainsDurableBaselineAndRetries(t *testing.T) {
	for _, failure := range []string{"payload", "manifest"} {
		t.Run(failure, func(t *testing.T) {
			_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{MaxPreparedGeometryCacheBytes: 1})
			entity, geometry := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "failure")
			MarkVoxelEntityPersistenceDirty(cmd, entity)
			cmd.app.FlushCommands()
			if err := persistChunkOverrides(cmd, state, ChunkCoord{}, state.LoadedChunks[ChunkCoord{}]); err != nil {
				t.Fatal(err)
			}
			old := s4aLoadDelta(t, state.WorldDeltaPath)
			oldPath := s4aPayloadPath(t, old, "imported", state.WorldDeltaPath)
			oldBytes := s4aRead(t, oldPath)
			s4cLive(t, cmd, assets, entity).SetVoxel(0, 0, 0, 9)
			dataDir := state.WorldDataDir
			restore := func() {
				state.WorldDataDir = dataDir
				state.WorldDelta.SchemaVersion = content.CurrentWorldDeltaSchemaVersion
			}
			t.Cleanup(restore)
			if failure == "manifest" {
				state.WorldDelta.SchemaVersion = -1
			} else {
				blocker := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(blocker, []byte("block directory creation"), 0600); err != nil {
					t.Fatal(err)
				}
				state.WorldDataDir = filepath.Join(blocker, "payloads")
			}
			s4cDrive(t, cmd, state, func() bool { return state.Metrics.PersistenceFailureCount > 0 })
			if state.InitErr != nil || state.Metrics.PersistenceLastError == "" || state.Metrics.DirtyPinnedChunkCount != 1 || !cmd.EntityExists(entity) || state.LoadedChunks[ChunkCoord{}] == nil {
				t.Fatalf("failed async save lost retryable live ownership: metrics=%+v error=%v", state.Metrics, state.InitErr)
			}
			asset, ok := assets.GetVoxelGeometry(geometry)
			if !ok || asset.XBrickMap == nil {
				t.Fatal("failed save lost usable live geometry")
			}
			s4aVoxel(t, asset.XBrickMap, 0, 9)
			if !reflect.DeepEqual(s4aLoadDelta(t, state.WorldDeltaPath), old) || !bytes.Equal(s4aRead(t, oldPath), oldBytes) || !reflect.DeepEqual(state.WorldDelta.ImportedWorldChunkOverrides, old.ImportedWorldChunkOverrides) {
				t.Fatal("failed async save changed durable or RAM checkpoint references")
			}
			restore()
			s4cDrive(t, cmd, state, func() bool { return len(state.LoadedChunks) == 0 })
			s4cDurableValue(t, state, "imported", 9)
		})
	}
}

func TestS4cStopJoinsInflightSaveAndFailureKeepsOwnership(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "durable_barrier", true: "failed_barrier"}[fail], func(t *testing.T) {
			_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{MaxPreparedGeometryCacheBytes: 1})
			entity, geometry := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "stop")
			MarkVoxelEntityPersistenceDirty(cmd, entity)
			cmd.app.FlushCommands()
			s4cLive(t, cmd, assets, entity).SetVoxel(0, 0, 0, 9)
			generation := state.Generation
			deltaPath := state.WorldDeltaPath
			if fail {
				state.WorldDelta.SchemaVersion = -1
			}
			t.Cleanup(func() {
				if state.WorldDelta != nil {
					state.WorldDelta.SchemaVersion = content.CurrentWorldDeltaSchemaVersion
				}
			})
			release := s4cHoldImportedWriter(t, cmd, state)
			stopped := make(chan error, 1)
			go func() { stopped <- StopStreamedLevelRuntime(cmd) }()
			select {
			case err := <-stopped:
				release()
				t.Fatalf("Stop returned before held persistence IO completed: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			release()
			err := <-stopped
			if fail {
				if err == nil || !state.Initialized || state.Generation != generation || !cmd.EntityExists(entity) || state.LoadedChunks[ChunkCoord{}] == nil {
					t.Fatalf("failed Stop tore down live ownership: error=%v initialized=%v generation=%d", err, state.Initialized, state.Generation)
				}
				asset, ok := assets.GetVoxelGeometry(geometry)
				if !ok || asset.XBrickMap == nil {
					t.Fatal("failed Stop lost usable live geometry")
				}
				s4aVoxel(t, asset.XBrickMap, 0, 9)
				state.WorldDelta.SchemaVersion = content.CurrentWorldDeltaSchemaVersion
				if err := StopStreamedLevelRuntime(cmd); err != nil {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			cmd.app.FlushCommands()
			if state.Initialized || cmd.EntityExists(entity) {
				t.Fatal("successful Stop retained runtime/entity ownership")
			}
			s2aAssertAsset(t, assets, geometry, false)
			delta := s4aLoadDelta(t, deltaPath)
			s4aVoxel(t, s4aLoadPayload(t, "imported", s4aPayloadPath(t, delta, "imported", deltaPath)), 0, 9)
		})
	}
}

func TestS4cDelayedNavigationAnalysisCannotReplaceAsyncUnload(t *testing.T) {
	_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{})
	entity, _ := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "navigation")
	state.BaseNavManifest = &content.NavGraphManifestDef{NavID: "nav", SourceWorldID: state.BaseWorldID, ChunkSize: 16, VoxelResolution: 1}
	state.navigationLoadedGen, state.navigationRequestedGen = 1, 1
	snapshot := s4bSnapshot(content.TerrainChunkCoordDef{}, 1)
	snapshot.WorldID = state.BaseWorldID
	s4bQueue(state, snapshot)
	older := s4bAnalyze(t, state)
	s4cLive(t, cmd, assets, entity).SetVoxel(0, 0, 0, 9)
	MarkVoxelEntityPersistenceDirty(cmd, entity)
	cmd.app.FlushCommands()
	release := s4cHoldImportedWriter(t, cmd, state)
	state.navigationEditAnalyses <- older
	commitStreamedNavigationEditAnalysis(state)
	release()
	s4cDrive(t, cmd, state, func() bool { return len(state.LoadedChunks) == 0 })
	s4cDurableValue(t, state, "imported", 9)
	s4bCommit(t, state, s4bAnalyze(t, state))
	s4bQueued(t, state, content.TerrainChunkCoordDef{}, 9)
	s4cDurableValue(t, state, "imported", 9)
}

func TestS4cDirtyResidencyUpgradeDoesNotWaitForPayloadAndReloadsCheckpoint(t *testing.T) {
	app, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{})
	entity, _ := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "upgrade")
	state.BaseWorldCollisionEnabled = true
	path := filepath.Join(t.TempDir(), "original.gkchunk")
	chunk := s4bSnapshot(content.TerrainChunkCoordDef{}, 1)
	chunk.WorldID = state.BaseWorldID
	writeImportedWorldChunkForStreamedTest(t, path, chunk)
	state.ImportedWorldEntries[ChunkCoord{}] = content.ImportedWorldChunkEntryDef{ChunkPath: path, NonEmptyVoxelCount: 1}
	state.Level.BaseWorld = &content.LevelBaseWorldDef{ManifestPath: filepath.Join(t.TempDir(), "world.gkworld")}
	cmd.AddEntity(TransformComponent{}, StreamedLevelObserverComponent{Radius: 1, KeepRadius: 1, CollisionRadius: 1})
	MarkVoxelEntityPersistenceDirty(cmd, entity)
	app.FlushCommands()
	s4cLive(t, cmd, assets, entity).SetVoxel(0, 0, 0, 9)
	release := s4cHoldImportedWriter(t, cmd, state)
	if !cmd.EntityExists(entity) || state.LoadedChunks[ChunkCoord{}] == nil {
		t.Fatal("pending upgrade released dirty ownership")
	}
	release()
	driveStreamedRuntimeUntil(t, app, func() bool {
		loaded := state.LoadedChunks[ChunkCoord{}]
		if loaded == nil {
			return false
		}
		for current := range loaded.ImportedWorldEntities {
			if current == entity || !hasComponentOfType[ColliderComponent](cmd, current) {
				return false
			}
			s4aVoxel(t, s4cLive(t, cmd, assets, current), 0, 9)
			return true
		}
		return false
	})
	s4cDurableValue(t, state, "imported", 9)
}
