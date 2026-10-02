package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s4bState(t *testing.T) (*App, *Commands, *StreamedLevelRuntimeState) {
	t.Helper()
	app, cmd, state := s4aState(t)
	state.Initialized, state.Generation = true, 1
	state.BaseWorldID = "world"
	state.BaseNavManifest = &content.NavGraphManifestDef{NavID: "nav", SourceWorldID: "world", ChunkSize: 16, VoxelResolution: 1}
	state.navigationLoadedGen, state.navigationRequestedGen = 1, 1
	t.Cleanup(func() { state.jobs.Wait() })
	return app, cmd, state
}

func s4bSnapshot(coord content.TerrainChunkCoordDef, value uint8) *content.ImportedWorldChunkDef {
	return &content.ImportedWorldChunkDef{
		SchemaVersion: content.CurrentImportedWorldChunkSchemaVersion, WorldID: "world", Coord: coord,
		ChunkSize: 16, VoxelResolution: 1, NonEmptyVoxelCount: 1,
		Voxels: []content.ImportedWorldVoxelDef{{Value: value}},
	}
}

func s4bQueue(state *StreamedLevelRuntimeState, snapshot *content.ImportedWorldChunkDef) {
	queueStreamedNavigationEditAnalysis(state, streamedNavigationEditAnalysisItem{
		WorldID: snapshot.WorldID, Coord: snapshot.Coord, ChunkSize: snapshot.ChunkSize,
		VoxelResolution: snapshot.VoxelResolution, Snapshot: snapshot,
	})
}

// Hold a completed actual worker result until the main thread chooses to commit
// it. This models delayed completion without sleeps or persistence test hooks.
func s4bAnalyze(t *testing.T, state *StreamedLevelRuntimeState) streamedNavigationEditAnalysisResult {
	t.Helper()
	startStreamedNavigationEditAnalysis(state)
	state.jobs.Wait()
	select {
	case result := <-state.navigationEditAnalyses:
		return result
	default:
		t.Fatal("latest captured edit did not produce an analysis result")
		return streamedNavigationEditAnalysisResult{}
	}
}

func s4bCommit(t *testing.T, state *StreamedLevelRuntimeState, result streamedNavigationEditAnalysisResult) {
	t.Helper()
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	state.navigationEditAnalyses <- result
	commitStreamedNavigationEditAnalysis(state)
	if err := saveStreamedWorldDeltaNow(state); err != nil {
		t.Fatal(err)
	}
}

func s4bDurableSnapshot(t *testing.T, state *StreamedLevelRuntimeState, coord content.TerrainChunkCoordDef) *content.ImportedWorldChunkDef {
	t.Helper()
	for _, override := range s4aLoadDelta(t, state.WorldDeltaPath).ImportedWorldChunkOverrides {
		if override.WorldID == "world" && override.ChunkCoord == coord {
			chunk, err := content.LoadImportedWorldChunk(content.ResolveDocumentPath(override.SnapshotPath, state.WorldDeltaPath))
			if err != nil {
				t.Fatal(err)
			}
			return chunk
		}
	}
	t.Fatalf("missing durable imported override for %v", coord)
	return nil
}

func s4bValue(t *testing.T, snapshot *content.ImportedWorldChunkDef, want uint8) {
	t.Helper()
	if snapshot == nil {
		t.Fatal("missing immutable navigation snapshot")
	}
	s4aVoxel(t, ImportedWorldChunkToXBrickMap(snapshot), 0, want)
}

func s4bQueued(t *testing.T, state *StreamedLevelRuntimeState, coord content.TerrainChunkCoordDef, value uint8) *content.ImportedWorldChunkDef {
	t.Helper()
	edit, ok := state.navigationQueuedEdits[coord]
	if !ok || state.Metrics.NavigationRebuildRequestedRevision == 0 {
		t.Fatalf("latest imported edit did not reach navigation rebuild input: coord=%v metrics=%+v", coord, state.Metrics)
	}
	s4bValue(t, edit.Snapshot, value)
	return edit.Snapshot
}

func s4bImportedEntity(t *testing.T, app *App, cmd *Commands, state *StreamedLevelRuntimeState) (EntityId, *volume.XBrickMap) {
	t.Helper()
	assets := assetServerFromApp(app)
	xbm := volume.NewXBrickMap()
	xbm.SetVoxel(0, 0, 0, 1)
	model := VoxelModelComponent{VoxelModel: assets.RegisterSharedVoxelGeometry(xbm, ""), VoxelResolution: 1, TerrainChunkSize: 16}
	entity := cmd.AddEntity(model, VoxelPersistenceDirtyComponent{}, AuthoredImportedWorldChunkRefComponent{LevelID: state.LevelID, WorldID: "world"})
	app.FlushCommands()
	live, ok := ResolveVoxelGeometryMap(assets, &model)
	if !ok {
		t.Fatal("fixture imported geometry missing")
	}
	state.LoadedChunks[ChunkCoord{}] = &streamedLoadedChunk{
		ImportedWorldEntities: map[EntityId]struct{}{entity: {}}, OwnedEntities: map[EntityId]struct{}{entity: {}},
	}
	return entity, live
}

func TestS4bDelayedAnalysisCannotReplaceNewerUnloadAndLatestEditProgresses(t *testing.T) {
	app, cmd, state := s4bState(t)
	entity, live := s4bImportedEntity(t, app, cmd, state)
	coord := content.TerrainChunkCoordDef{}
	s4bQueue(state, s4bSnapshot(coord, 1))
	older := s4bAnalyze(t, state)
	live.SetVoxel(0, 0, 0, 9)
	if err := unloadStreamedChunk(cmd, state, ChunkCoord{}); err != nil {
		t.Fatal(err)
	}
	app.FlushCommands()
	if cmd.EntityExists(entity) {
		t.Fatal("blocking unload did not remove imported entity")
	}
	s4bValue(t, s4bDurableSnapshot(t, state, coord), 9)
	live.SetVoxel(0, 0, 0, 7) // Caller geometry cannot alter the already-saved capture.
	s4bCommit(t, state, older)
	s4bValue(t, s4bDurableSnapshot(t, state, coord), 9)
	s4bCommit(t, state, s4bAnalyze(t, state))
	s4bQueued(t, state, coord, 9)
	s4bValue(t, s4bDurableSnapshot(t, state, coord), 9)
}

func TestS4bMixedCompletionKeepsCurrentItemsAndCoordinateLocalIgnoredState(t *testing.T) {
	_, _, state := s4bState(t)
	a, b, unrelated := content.TerrainChunkCoordDef{}, content.TerrainChunkCoordDef{X: 1}, content.TerrainChunkCoordDef{X: 2}
	set := func(x int) map[navigationRemovedVoxel]struct{} {
		return map[navigationRemovedVoxel]struct{}{{X: x}: {}}
	}
	state.navigationIgnoredRemovals = map[content.TerrainChunkCoordDef]map[navigationRemovedVoxel]struct{}{a: set(1), b: set(2), unrelated: set(3)}
	s4bQueue(state, s4bSnapshot(a, 1))
	s4bQueue(state, s4bSnapshot(b, 2))
	older := s4bAnalyze(t, state)
	latest := s4bSnapshot(a, 9)
	latest.Tags = []string{"captured"}
	if err := persistImportedWorldRuntimeEditSnapshots(state, []*content.ImportedWorldChunkDef{latest}); err != nil {
		t.Fatal(err)
	}
	if err := saveStreamedWorldDeltaNow(state); err != nil {
		t.Fatal(err)
	}
	latest.Voxels[0].Value, latest.Tags[0] = 7, "caller edit"
	state.navigationIgnoredRemovals[a], state.navigationIgnoredRemovals[unrelated] = set(10), set(30)
	s4bCommit(t, state, older)
	s4bValue(t, s4bDurableSnapshot(t, state, a), 9)
	s4bValue(t, s4bDurableSnapshot(t, state, b), 2)
	s4bQueued(t, state, b, 2)
	if !reflect.DeepEqual(state.navigationIgnoredRemovals[a], set(10)) || !reflect.DeepEqual(state.navigationIgnoredRemovals[unrelated], set(30)) || len(state.navigationIgnoredRemovals[b]) != 0 {
		t.Fatal("mixed result replaced stale/unrelated ignored state or lost current coordinate update")
	}
	s4bCommit(t, state, s4bAnalyze(t, state))
	snapshot := s4bQueued(t, state, a, 9)
	if !reflect.DeepEqual(snapshot.Tags, []string{"captured"}) {
		t.Fatal("saved snapshot tags remained aliased to caller input")
	}
}

func TestS4bGraphRetryPreservesCurrentCaptureAndNewerQueuedSuccessor(t *testing.T) {
	for _, newerQueued := range []bool{false, true} {
		t.Run(map[bool]string{false: "current_retry", true: "newer_capture"}[newerQueued], func(t *testing.T) {
			_, _, state := s4bState(t)
			coord := content.TerrainChunkCoordDef{}
			s4bQueue(state, s4bSnapshot(coord, 1))
			older := s4bAnalyze(t, state)
			want := uint8(1)
			if newerQueued {
				want = 9
				s4bQueue(state, s4bSnapshot(coord, want))
			}
			state.navigationLoadedGen++
			s4bCommit(t, state, older)
			s4bCommit(t, state, s4bAnalyze(t, state))
			s4bQueued(t, state, coord, want)
			s4bValue(t, s4bDurableSnapshot(t, state, coord), want)
		})
	}
}

func TestS4bBackingUnloadRejectsDelayedFullSnapshotAndKeepsAnalysis(t *testing.T) {
	app, cmd, state := s4bState(t)
	entity, live := s4bImportedEntity(t, app, cmd, state)
	coord := content.TerrainChunkCoordDef{}
	s4bQueue(state, s4bSnapshot(coord, 1))
	older := s4bAnalyze(t, state)
	provider := NewTerrainColumnVoxelBacking(&content.TerrainChunkDef{ChunkSize: 16, VoxelResolution: 1, SolidValue: 1, Columns: []content.TerrainChunkColumnDef{{FilledVoxels: 1}}})
	removal := &content.VoxelBackingRemovalDef{OwnerKind: content.VoxelBackingOwnerImportedWorld, OwnerID: "world", SourceHash: "source", Bricks: []content.VoxelBackingRemovalBrickDef{{Bits: [16]uint32{1}, Material: 1}}}
	backing := NewVoxelBackingComponent(content.VoxelBackingOwnerImportedWorld, "world", "source", [3]int{}, 16, provider, removal)
	backing.Dirty = true
	cmd.AddComponents(entity, backing)
	app.FlushCommands()
	live.SetVoxel(0, 0, 0, 0)
	if err := unloadStreamedChunk(cmd, state, ChunkCoord{}); err != nil {
		t.Fatal(err)
	}
	app.FlushCommands()
	s4bCommit(t, state, older)
	delta := s4aLoadDelta(t, state.WorldDeltaPath)
	if len(delta.ImportedWorldChunkOverrides) != 0 || len(delta.VoxelBackingRemovals) != 1 || !reflect.DeepEqual(delta.VoxelBackingRemovals[0], *removal) {
		t.Fatalf("delayed full snapshot resurrected geometry over durable backing removal: %+v", delta)
	}
	s4bCommit(t, state, s4bAnalyze(t, state))
	s4bQueued(t, state, coord, 0)
	if len(s4aLoadDelta(t, state.WorldDeltaPath).ImportedWorldChunkOverrides) != 0 {
		t.Fatal("latest backing analysis published a competing full snapshot")
	}
}

func TestS4bTerminalAnalysisErrorAllowsFailedAndUnprocessedCaptureRetries(t *testing.T) {
	_, _, state := s4bState(t)
	a, b := content.TerrainChunkCoordDef{}, content.TerrainChunkCoordDef{X: 1}
	invalid := s4bSnapshot(a, 1)
	invalid.SchemaVersion = -1
	s4bQueue(state, invalid)
	s4bQueue(state, s4bSnapshot(b, 2))
	failed := s4bAnalyze(t, state)
	if failed.Err == nil {
		t.Fatal("invalid first snapshot must fail actual analysis batch")
	}
	// Successors arrive before terminal error handling; neither may be discarded
	// while releasing ownership of failed or unprocessed older captures.
	s4bQueue(state, s4bSnapshot(a, 9))
	s4bQueue(state, s4bSnapshot(b, 4))
	state.navigationEditAnalyses <- failed
	commitStreamedNavigationEditAnalysis(state)
	if state.InitErr == nil {
		t.Fatal("terminal analysis error lost existing runtime diagnostic")
	}
	state.InitErr = nil // Retry after the caller handles the existing error.
	s4bCommit(t, state, s4bAnalyze(t, state))
	s4bQueued(t, state, a, 9)
	s4bQueued(t, state, b, 4)
	s4bValue(t, s4bDurableSnapshot(t, state, a), 9)
	s4bValue(t, s4bDurableSnapshot(t, state, b), 4)
}

func TestS4bCaptureImpactSurvivesNewerNoImpactRemovalCapture(t *testing.T) {
	for _, impact := range []string{"unknown", "added"} {
		for _, timing := range []string{"pending", "active"} {
			t.Run(impact+"/"+timing, func(t *testing.T) {
				_, _, state := s4bState(t)
				coord := content.TerrainChunkCoordDef{}
				state.NavigationSources = []content.NavSourceTileDef{{NavID: "nav", Coord: coord, ChunkSize: 16, VoxelResolution: 1}}
				saved := s4bSnapshot(coord, 1)
				saved.Voxels = append(saved.Voxels, content.ImportedWorldVoxelDef{X: 1, Value: 1})
				saved.NonEmptyVoxelCount = 2
				if impact == "unknown" {
					if err := persistImportedWorldRuntimeEditSnapshots(state, []*content.ImportedWorldChunkDef{saved}); err != nil {
						t.Fatal(err)
					}
				} else {
					queueStreamedNavigationEditAnalysis(state, streamedNavigationEditAnalysisItem{
						Edit:    runtimeVoxelEdit{Valid: true, Added: true, Max: [3]float32{1, 1, 1}},
						WorldID: saved.WorldID, Coord: coord, ChunkSize: saved.ChunkSize,
						VoxelResolution: saved.VoxelResolution, Snapshot: saved,
					})
				}
				var older streamedNavigationEditAnalysisResult
				if timing == "active" {
					older = s4bAnalyze(t, state)
				}
				latest := s4bSnapshot(coord, 1)
				queueStreamedNavigationEditAnalysis(state, streamedNavigationEditAnalysisItem{
					Edit:    runtimeVoxelEdit{Valid: true, Min: [3]float32{1, 0, 0}, Max: [3]float32{2, 1, 1}},
					WorldID: latest.WorldID, Coord: coord, ChunkSize: latest.ChunkSize,
					VoxelResolution: latest.VoxelResolution, Snapshot: latest,
				})
				if timing == "active" {
					s4bCommit(t, state, older)
				}
				s4bCommit(t, state, s4bAnalyze(t, state))
				queued := s4bQueued(t, state, coord, 1)
				s4aVoxel(t, ImportedWorldChunkToXBrickMap(queued), 1, 0)
			})
		}
	}
}

func TestS4bCrossWorldGraphRetryPreservesNewerCaptureAtSameCoordinate(t *testing.T) {
	_, _, state := s4bState(t)
	state.BaseWorldID, state.BaseNavManifest.SourceWorldID = "world-a", "world-a"
	coord := content.TerrainChunkCoordDef{}
	olderSnapshot := s4bSnapshot(coord, 1)
	olderSnapshot.WorldID = "world-a"
	s4bQueue(state, olderSnapshot)
	older := s4bAnalyze(t, state)
	latest := s4bSnapshot(coord, 9)
	latest.WorldID = "world-b"
	s4bQueue(state, latest)
	state.navigationLoadedGen++
	s4bCommit(t, state, older)
	s4bCommit(t, state, s4bAnalyze(t, state))
	queued := s4bQueued(t, state, coord, 9)
	if queued.WorldID != "world-b" {
		t.Fatal("older world retry displaced latest navigation snapshot")
	}
	for _, override := range s4aLoadDelta(t, state.WorldDeltaPath).ImportedWorldChunkOverrides {
		if override.WorldID == "world-b" && override.ChunkCoord == coord {
			s4aVoxel(t, s4aLoadPayload(t, "imported", content.ResolveDocumentPath(override.SnapshotPath, state.WorldDeltaPath)), 0, 9)
			return
		}
	}
	t.Fatal("newer world capture did not publish its durable imported override")
}
