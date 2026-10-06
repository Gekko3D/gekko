package gekko

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

// Full demand follows one observer. Proxy demand follows the supported global
// fallback policy, with the observer outside the full sector throughout.
// Both jobs use their real imported payload reader and valid authored metadata.
func s2eRuntime(t *testing.T, proxy bool) (*Commands, *StreamedLevelRuntimeState, *AssetServer, EntityId, string) {
	t.Helper()
	path := s2bWorldPath(t)
	level, err := content.LoadLevel(path)
	if err != nil {
		t.Fatal(err)
	}
	level.Terrain, level.Placements, level.Markers, level.Lights = nil, nil, nil, nil
	if err := content.SaveLevel(path, level); err != nil {
		t.Fatal(err)
	}
	worldPath := content.ResolveDocumentPath(level.BaseWorld.ManifestPath, path)
	world, err := content.LoadImportedWorld(worldPath)
	if err != nil {
		t.Fatal(err)
	}
	world.Backing = nil
	if err := content.SaveImportedWorld(worldPath, world); err != nil {
		t.Fatal(err)
	}
	if validation := content.ValidateImportedWorld(world, content.ImportedWorldValidationOptions{DocumentPath: worldPath}); validation.HasErrors() {
		t.Fatalf("invalid cancellation world fixture: %s", validation.Error())
	}
	if validation := content.ValidateLevel(level, content.LevelValidationOptions{DocumentPath: path}); validation.HasErrors() {
		t.Fatalf("invalid cancellation level fixture: %s", validation.Error())
	}
	_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{
		LevelPath: path, Loader: NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: 1}),
		MaxPrepareJobs: 1, DisableSectorProxies: !proxy,
		MaxPreparedGeometryCacheBytes: 1, MaxPendingPreparedBytes: 1,
	})
	position := mgl32.Vec3{1, 1, 1}
	if proxy {
		position[0] = 1600
	}
	observer := s3aObserver(cmd, position, StreamedLevelObserverComponent{})
	cmd.app.FlushCommands()
	return cmd, state, assets, observer, content.ResolveImportedWorldChunkPath(world.Entries[0], worldPath)
}

type s2eSharedDecode struct {
	source *content.ImportedWorldChunkRLESource
	chunk  *content.ImportedWorldChunkDef
	err    error
}

func s2eSharedVoxelCount(result s2eSharedDecode) int {
	if result.source != nil {
		count := 0
		for range result.source.Voxels() {
			count++
		}
		return count
	}
	if result.chunk != nil {
		return len(result.chunk.Voxels)
	}
	return 0
}

func s2eHoldDecode(t *testing.T, loader *RuntimeContentLoader, path string, failure error, rle ...bool) (*RuntimeContentLoadScope, func(), <-chan s2eSharedDecode) {
	t.Helper()
	scope := loader.NewScope()
	t.Cleanup(scope.Close)
	held, release := s2bBarrier(t)
	entered := make(chan struct{})
	done := make(chan s2eSharedDecode, 1)
	go func() {
		if len(rle) != 0 && rle[0] {
			source, err := loadRuntimeContent(scope.Loader(), "imported-chunk-rle", path, func(path string) (*content.ImportedWorldChunkRLESource, error) {
				close(entered)
				<-held
				if failure != nil {
					return nil, failure
				}
				return content.LoadImportedWorldChunkRLESource(path)
			})
			done <- s2eSharedDecode{source: source, err: err}
			return
		}
		chunk, err := loadRuntimeContent(scope.Loader(), "imported-chunk", path, func(path string) (*content.ImportedWorldChunkDef, error) {
			close(entered)
			<-held
			if failure != nil {
				return nil, failure
			}
			return content.LoadImportedWorldChunk(path)
		})
		done <- s2eSharedDecode{chunk: chunk, err: err}
	}()
	s2bWait(t, entered)
	return scope, release, done
}

func s2eDispatchHeld(t *testing.T, cmd *Commands, state *StreamedLevelRuntimeState) {
	t.Helper()
	waits := state.Loader.Stats().LoadWaits
	updateStreamedLevelObserverSystem(cmd, state)
	s2bUntil(t, func() bool { return state.Loader.Stats().LoadWaits > waits })
}

func s2eChangeDemand(t *testing.T, cmd *Commands, state *StreamedLevelRuntimeState, observer EntityId, proxy, away bool) {
	t.Helper()
	position := mgl32.Vec3{1, 1, 1}
	if proxy || away {
		position[0] = 1600
	}
	if proxy {
		state.Config.DisableSectorProxies = away
	}
	s3aMove(cmd, observer, position)
	updateStreamedLevelObserverSystem(cmd, state)
	if away {
		demand := state.DesiredChunks
		if proxy {
			demand = state.DesiredProxySectors
		}
		if _, desired := demand[ChunkCoord{}]; desired {
			t.Fatal("fixture did not remove current preparation demand")
		}
	}
}

func s2eWaitPrepared(t *testing.T, state *StreamedLevelRuntimeState, proxy bool) {
	t.Helper()
	s2bUntil(t, func() bool {
		if proxy {
			return len(state.PreparedProxyLoads) != 0
		}
		return len(state.PreparedLoads) != 0
	})
}

func s2eLoaded(state *StreamedLevelRuntimeState, proxy bool) bool {
	if proxy {
		return state.LoadedSectorProxies[ChunkCoord{}] != nil
	}
	return state.LoadedChunks[ChunkCoord{}] != nil
}

func s2eAssertCancelled(t *testing.T, state *StreamedLevelRuntimeState, count int) {
	t.Helper()
	if state.InitErr != nil || state.Metrics.PrepareErrorCount != 0 {
		t.Fatalf("obsolete preparation poisoned runtime: InitErr=%v prepare_errors=%d", state.InitErr, state.Metrics.PrepareErrorCount)
	}
	if state.Metrics.PendingPreparedBytes != 0 || state.Metrics.PendingPreparedAdmissionRetries != 0 {
		t.Fatalf("cancelled preparation retained bytes or requested byte retry: %+v", state.Metrics)
	}
	// Check the new public field at runtime so the pre-S2e baseline reaches the
	// behavioral failure instead of failing solely because the field is absent.
	metric := reflect.ValueOf(state.Metrics).FieldByName("PrepareCancelledCount")
	if !metric.IsValid() || metric.Kind() != reflect.Int || metric.Int() != int64(count) {
		t.Fatalf("PrepareCancelledCount must report %d terminal cancellations", count)
	}
}

func TestS2eObsoleteWorkerErrorCannotPoisonRuntime(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		name := "full"
		if proxy {
			name = "proxy-policy"
		}
		t.Run(name, func(t *testing.T) {
			cmd, state, assets, observer, path := s2eRuntime(t, proxy)
			failure := errors.New("obsolete shared decode failed")
			_, release, done := s2eHoldDecode(t, state.Loader, path, failure, proxy)
			s2eDispatchHeld(t, cmd, state)
			s2eChangeDemand(t, cmd, state, observer, proxy, true)
			release()
			if result := s2bWait(t, done); !errors.Is(result.err, failure) {
				t.Fatalf("independent shared decode lost its error: %v", result.err)
			}
			s2eWaitPrepared(t, state, proxy)
			commitPreparedStreamedChunksSystem(cmd, assets, state)
			if s2eLoaded(state, proxy) {
				t.Fatal("obsolete error published an entity")
			}
			s2eAssertCancelled(t, state, 1)
		})
	}
}

func TestS2eRenewedDemandCannotReviveHeldWorkerAndSharedLeaseSurvives(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		name := "full"
		if proxy {
			name = "proxy-policy"
		}
		t.Run(name, func(t *testing.T) {
			cmd, state, assets, observer, path := s2eRuntime(t, proxy)
			metadataBytes := state.Loader.Stats().PinnedBytes
			geometryMisses := state.Metrics.PreparedGeometryCacheMisses
			scope, release, done := s2eHoldDecode(t, state.Loader, path, nil, proxy)
			s2eDispatchHeld(t, cmd, state)
			s2eChangeDemand(t, cmd, state, observer, proxy, true)
			s2eChangeDemand(t, cmd, state, observer, proxy, false)
			if state.Metrics.ActivePrepareJobCount != 1 || state.Metrics.PendingLoadCount+state.Metrics.PendingProxyLoadCount != 1 || state.Loader.Stats().LoadWaits != 1 {
				t.Fatal("return dispatched overlapping work before terminal acknowledgement")
			}
			release()
			shared := s2bWait(t, done)
			if shared.err != nil || s2eSharedVoxelCount(shared) == 0 {
				t.Fatalf("cancelled consumer invalidated independent shared decode: %+v", shared)
			}
			s2eWaitPrepared(t, state, proxy)
			refreshStreamedRuntimeMetricsCounts(state)
			if state.Metrics.PendingPreparedBytes != 0 || state.Metrics.PreparedGeometryCachePreparedBytes != 0 || state.Metrics.PreparedGeometryCacheMisses != geometryMisses {
				t.Fatal("cancelled worker continued into geometry or pending-byte publication")
			}
			if state.Loader.Stats().PinnedBytes <= metadataBytes {
				t.Fatal("independent shared scope lost its decoded pin")
			}
			if proxy {
				source, err := scope.Loader().LoadImportedWorldChunkRLESource(path)
				if err != nil || source != shared.source || source == nil || source.Metadata().NonEmptyVoxelCount != s2eSharedVoxelCount(shared) || len(source.Metadata().Voxels) != 0 {
					t.Fatalf("independent scope cannot reuse shared encoded result: %v", err)
				}
			} else if chunk, err := scope.Loader().LoadImportedWorldChunk(path); err != nil || chunk != shared.chunk || len(chunk.Voxels) == 0 {
				t.Fatalf("independent scope cannot reuse shared result: %v", err)
			}
			scope.Close()
			if state.Loader.Stats().PinnedBytes != metadataBytes {
				t.Fatal("cancelled worker retained decoded lease after independent scope closed")
			}
			// Consume the obsolete terminal before driving any fresh dispatch.
			commitPreparedStreamedChunksSystem(cmd, assets, state)
			if s2eLoaded(state, proxy) {
				t.Fatal("renewed demand revived obsolete completion")
			}
			s2eAssertCancelled(t, state, 1)
			s2bUntil(t, func() bool {
				updateStreamedLevelObserverSystem(cmd, state)
				commitPreparedStreamedChunksSystem(cmd, assets, state)
				cmd.app.FlushCommands()
				return s2eLoaded(state, proxy) || state.InitErr != nil
			})
			if !s2eLoaded(state, proxy) || state.InitErr != nil {
				t.Fatalf("renewed demand could not load fresh geometry: %v", state.InitErr)
			}
			var entity EntityId
			if proxy {
				entity = state.LoadedSectorProxies[ChunkCoord{}].Entity
			} else {
				for id := range state.LoadedChunks[ChunkCoord{}].ImportedWorldEntities {
					entity = id
				}
			}
			model := mustVoxelModelComponentForLevelTest(t, cmd, entity)
			if geometry, ok := ResolveVoxelGeometryMap(assets, &model); !ok || geometry == nil || geometry.GetVoxelCount() == 0 {
				t.Fatal("fresh dispatch published unusable geometry")
			}
		})
	}
}

func TestS2eQueuedSuccessCannotCommitAfterDemandRenewal(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		name := "full"
		if proxy {
			name = "proxy-policy"
		}
		t.Run(name, func(t *testing.T) {
			cmd, state, assets, observer, _ := s2eRuntime(t, proxy)
			updateStreamedLevelObserverSystem(cmd, state)
			s2eWaitPrepared(t, state, proxy)
			refreshStreamedRuntimeMetricsCounts(state)
			if state.Metrics.PendingPreparedBytes == 0 {
				t.Fatal("fixture has no queued successful payload")
			}
			s2eChangeDemand(t, cmd, state, observer, proxy, true)
			s2eChangeDemand(t, cmd, state, observer, proxy, false)
			commitPreparedStreamedChunksSystem(cmd, assets, state)
			cmd.app.FlushCommands()
			if s2eLoaded(state, proxy) || state.Metrics.CommittedChunkCount != 0 {
				t.Fatal("queued obsolete success committed after demand renewal")
			}
			s2eAssertCancelled(t, state, 1)
		})
	}
}

func TestS2eFailedStopCancelsHeldWorkAndKeepsLoadedOwnership(t *testing.T) {
	cmd, state, assets, observer, path := s2eRuntime(t, false)
	entity, asset := s2aCommitImported(t, cmd, assets, state, ChunkCoord{X: 1}, "independent-live")
	// Keep the already loaded owner near the observer, without adding content
	// demand for another coordinate.
	s3aChangeRadii(cmd, observer, StreamedLevelObserverComponent{KeepRadius: 1})
	cache, generation := state.PreparedGeometryCache, state.Generation
	failure := errors.New("decode completed after failed Stop")
	_, release, done := s2eHoldDecode(t, state.Loader, path, failure)
	s2eDispatchHeld(t, cmd, state)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	original := state.WorldDeltaPath
	state.WorldDeltaPath = filepath.Join(blocker, "delta.gkworlddelta")
	defer func() { state.WorldDeltaPath = original }()
	if err := StopStreamedLevelRuntime(cmd); err == nil {
		t.Fatal("fixture did not fail Stop before joining worker")
	}
	state.WorldDeltaPath = original
	if !state.Initialized || state.Generation != generation || state.PreparedGeometryCache != cache || !hasComponentOfType[VoxelModelComponent](cmd, entity) {
		t.Fatal("failed Stop abandoned loaded ownership")
	}
	s2aAssertAsset(t, assets, asset, true)
	release()
	s2bWait(t, done)
	s2eWaitPrepared(t, state, false)
	commitPreparedStreamedChunksSystem(cmd, assets, state)
	s2eAssertCancelled(t, state, 1)
	s2bUntil(t, func() bool {
		updateStreamedLevelObserverSystem(cmd, state)
		commitPreparedStreamedChunksSystem(cmd, assets, state)
		return s2eLoaded(state, false) || state.InitErr != nil
	})
	if !s2eLoaded(state, false) || state.InitErr != nil {
		t.Fatalf("failed Stop stranded required scheduling: %v", state.InitErr)
	}
}

func TestS2eStopDrainsCancelledQueueAndIndependentScopeRemainsUsable(t *testing.T) {
	cmd, state, _, _, path := s2eRuntime(t, false)
	scope, release, done := s2eHoldDecode(t, state.Loader, path, nil)
	s2eDispatchHeld(t, cmd, state)
	release()
	shared := s2bWait(t, done)
	s2eWaitPrepared(t, state, false)
	if err := StopStreamedLevelRuntime(cmd); err != nil {
		t.Fatal(err)
	}
	if state.Initialized || len(state.PreparedLoads) != 0 || state.Metrics.PendingLoadCount != 0 || state.Metrics.ActivePrepareJobCount != 0 {
		t.Fatal("Stop retained queued work or scheduling ownership")
	}
	s2eAssertCancelled(t, state, 1)
	if chunk, err := scope.Loader().LoadImportedWorldChunk(path); err != nil || chunk != shared.chunk || chunk == nil || len(chunk.Voxels) == 0 {
		t.Fatalf("Stop invalidated independent shared decode: %v", err)
	}
	scope.Close()
	if scope.Loader().Stats().PinnedBytes != 0 {
		t.Fatal("stopped worker leaked decoded ownership after independent scope closed")
	}
}
