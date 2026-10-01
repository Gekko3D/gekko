package gekko

import (
	"github.com/gekko3d/gekko/content"
	"os"
	"path/filepath"
	"testing"
)

// This seam exposes byte admission and idempotent release only. It does not
// constrain the owner's container, queue layout, or implementation lock order.
func TestS2bPendingOwnerExactBoundarySharedCreditAndSoleOversize(t *testing.T) {
	owner := newStreamedPendingPreparedOwner(100)
	full, ok := owner.reserve(60)
	if !ok || full == nil {
		t.Fatal("fitting full payload was rejected")
	}
	proxy, ok := owner.reserve(40)
	if !ok || proxy == nil {
		t.Fatal("proxy could not use exact remaining shared credit")
	}
	if stats := owner.snapshot(); stats.Bytes != 100 || stats.OverBudgetBytes != 0 {
		t.Fatalf("full/proxy shared admission: %+v", stats)
	}
	if credit, ok := owner.reserve(1); ok || credit != nil {
		t.Fatal("admission retained excess payload under pressure")
	}
	full.release()
	full.release()
	if stats := owner.snapshot(); stats.Bytes != 40 {
		t.Fatalf("credit was released more than once: %+v", stats)
	}
	if credit, ok := owner.reserve(101); ok || credit != nil {
		t.Fatal("oversized result joined another charged payload")
	}
	proxy.release()
	large, ok := owner.reserve(101)
	if !ok || large == nil {
		t.Fatal("sole oversized result cannot make progress")
	}
	if stats := owner.snapshot(); stats.Bytes != 101 || stats.OverBudgetBytes != 1 || stats.OversizedAdmissions != 1 {
		t.Fatalf("oversize pressure missing: %+v", stats)
	}
	if credit, ok := owner.reserve(1); ok || credit != nil {
		t.Fatal("another payload joined sole oversized result")
	}
	large.release()
	if stats := owner.snapshot(); stats.Bytes != 0 || stats.OverBudgetBytes != 0 {
		t.Fatalf("release retained pressure: %+v", stats)
	}
}

func TestS2bPendingWorkerCreditCoversBlockedPublicationAndStopDrain(t *testing.T) {
	_, cmd, state, _ := s2bStartWorld(t, StreamedLevelRuntimeConfig{MaxDecodedContentCacheBytes: 1, MaxPendingPreparedBytes: 1})
	loader := state.Loader
	metadata := loader.Stats().PinnedBytes
	// Fill the public queue with existing-compatible synthetic results. Only the
	// real prepared worker below owns bytes and a content scope.
	for i := 0; i < cap(state.PreparedLoads); i++ {
		state.PreparedLoads <- streamedPreparedChunk{Generation: state.Generation, Coord: ChunkCoord{X: 100 + i}}
	}
	startStreamedChunkPrepareJob(state, buildStreamedChunkLoadJob(state, ChunkCoord{}))
	s2bUntil(t, func() bool { refreshStreamedRuntimeMetricsCounts(state); return state.Metrics.PendingPreparedBytes > 0 })
	if state.Metrics.PreparedChunkQueueDepth != cap(state.PreparedLoads) || state.Metrics.PendingPreparedOverBudgetBytes <= 0 || state.Metrics.PendingPreparedOversizedAdmissions != 1 {
		t.Fatalf("blocked publication did not retain reserved credit: %+v", state.Metrics)
	}
	if loader.Stats().PinnedBytes <= metadata {
		t.Fatal("blocked completed payload has no decoded lease")
	}
	if err := StopStreamedLevelRuntime(cmd); err != nil {
		t.Fatal(err)
	}
	if stats := loader.Stats(); stats.Bytes != 0 || stats.PinnedBytes != 0 {
		t.Fatalf("Stop drain leaked decoded leases: %+v", stats)
	}
	if state.Metrics.PendingPreparedBytes != 0 || state.Metrics.PendingPreparedOverBudgetBytes != 0 {
		t.Fatalf("Stop left stale pending pressure: %+v", state.Metrics)
	}
}

func TestS2bPendingRealFullAndProxyShareBudgetAndReleaseOnCommit(t *testing.T) {
	_, cmd, state, assets := s2bStartWorld(t, StreamedLevelRuntimeConfig{MaxDecodedContentCacheBytes: 1, MaxPendingPreparedBytes: 1, MaxChunkCommitsPerFrame: 1})
	metadata := state.Loader.Stats().PinnedBytes
	coord := ChunkCoord{}
	state.DesiredChunks[coord] = struct{}{}
	state.DesiredProxySectors[coord] = struct{}{}
	state.KeepChunks[coord] = struct{}{}
	state.PendingLoads[coord] = struct{}{}
	startStreamedChunkPrepareJob(state, buildStreamedChunkLoadJob(state, coord))
	s2bUntil(t, func() bool { return len(state.PreparedLoads) == 1 })
	state.jobs.Wait()
	refreshStreamedRuntimeMetricsCounts(state)
	fullBytes := state.Metrics.PendingPreparedBytes
	if fullBytes <= 1 {
		t.Fatalf("full result was not charged: %+v", state.Metrics)
	}
	minimum := state.Loader.Stats().PinnedBytes - metadata + state.PreparedGeometryCache.snapshot().PreparedBytes
	if fullBytes < minimum {
		t.Fatalf("pending charge omitted decoded or prepared storage: bytes=%d minimum=%d", fullBytes, minimum)
	}
	state.PendingProxyLoads[coord] = struct{}{}
	startStreamedSectorProxyPrepareJob(state, buildStreamedSectorProxyLoadJob(state, coord, state.ImportedWorldSectors[coord].LODs[0]))
	s2bUntil(t, func() bool { return len(state.PreparedProxyLoads) == 1 })
	state.jobs.Wait()
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Metrics.PendingPreparedBytes != fullBytes || state.Metrics.PendingPreparedAdmissionRetries != 1 {
		t.Fatalf("proxy did not share the full queue's byte owner: %+v", state.Metrics)
	}
	if state.Loader.Stats().PinnedBytes <= metadata {
		t.Fatal("queued full result lost decoded scope")
	}
	commitPreparedStreamedChunksSystem(cmd, assets, state)
	cmd.app.FlushCommands()
	if state.LoadedChunks[coord] == nil {
		t.Fatalf("sole oversized full demand failed to commit: %v", state.InitErr)
	}
	if state.Metrics.PendingPreparedBytes != 0 || state.Loader.Stats().PinnedBytes != metadata {
		t.Fatalf("commit did not release payload credit and transient scope: %+v loader=%+v", state.Metrics, state.Loader.Stats())
	}
	loaded := state.LoadedChunks[coord]
	if len(loaded.ImportedWorldEntities) != 1 {
		t.Fatal("full commit did not publish imported entity")
	}
	for entity := range loaded.ImportedWorldEntities {
		model := mustVoxelModelComponentForLevelTest(t, cmd, entity)
		geometry, ok := ResolveVoxelGeometryMap(assets, &model)
		if !ok || geometry == nil || geometry.GetVoxelCount() == 0 {
			t.Fatal("commit depended on released decoded data")
		}
	}
}

func TestS2bPendingConsumptionBranchesReleasePayload(t *testing.T) {
	for _, kind := range []string{"full", "proxy"} {
		for _, branch := range []string{"obsolete", "duplicate", "stale", "prepare error", "commit error", "drain"} {
			if kind == "proxy" && branch == "commit error" {
				branch = "empty commit"
			}
			t.Run(kind+"/"+branch, func(t *testing.T) {
				_, cmd, state, assets := s2bStartWorld(t, StreamedLevelRuntimeConfig{MaxDecodedContentCacheBytes: 1, MaxPendingPreparedBytes: 1 << 30})
				coord := ChunkCoord{}
				metadata := state.Loader.Stats().PinnedBytes
				state.DesiredChunks[coord] = struct{}{}
				state.DesiredProxySectors[coord] = struct{}{}
				if kind == "full" {
					job := buildStreamedChunkLoadJob(state, coord)
					if branch == "stale" {
						job.Generation--
					}
					if branch == "prepare error" {
						job.VoxelOverrides = map[string]content.VoxelObjectOverrideDef{"missing": {SnapshotPath: "missing"}}
					}
					startStreamedChunkPrepareJob(state, job)
					s2bUntil(t, func() bool { return len(state.PreparedLoads) == 1 })
				} else {
					job := buildStreamedSectorProxyLoadJob(state, coord, state.ImportedWorldSectors[coord].LODs[0])
					if branch == "stale" {
						job.Generation--
					}
					if branch == "prepare error" {
						job.LOD.ChunkPath = "missing"
					}
					startStreamedSectorProxyPrepareJob(state, job)
					s2bUntil(t, func() bool { return len(state.PreparedProxyLoads) == 1 })
				}
				state.jobs.Wait()
				refreshStreamedRuntimeMetricsCounts(state)
				if branch == "prepare error" {
					if state.Metrics.PendingPreparedBytes != 0 || state.Loader.Stats().PinnedBytes != metadata {
						t.Fatalf("error completion retained partial payload/scope before publication: %+v loader=%+v", state.Metrics, state.Loader.Stats())
					}
				} else if state.Metrics.PendingPreparedBytes <= 0 {
					t.Fatal("fixture did not own pending payload bytes")
				}
				switch branch {
				case "obsolete":
					delete(state.DesiredChunks, coord)
					delete(state.DesiredProxySectors, coord)
				case "duplicate":
					if kind == "full" {
						state.LoadedChunks[coord] = &streamedLoadedChunk{}
					} else {
						state.LoadedSectorProxies[coord] = &streamedLoadedSectorProxy{}
					}
				case "stale":
					state.PendingLoads[coord] = struct{}{}
					state.PendingProxyLoads[coord] = struct{}{}
				case "commit error", "empty commit":
					if kind == "full" {
						prepared := <-state.PreparedLoads
						prepared.PlacementItems = []streamedPlacementInstance{{PlacementID: "broken", AssetPath: "missing"}}
						state.PreparedLoads <- prepared
					} else {
						prepared := <-state.PreparedProxyLoads
						prepared.PreparedGeometry = nil
						prepared.Chunk = nil
						state.PreparedProxyLoads <- prepared
					}
				}
				if branch == "drain" {
					drainStreamedPreparedResults(state)
					refreshStreamedRuntimeMetricsCounts(state)
				} else {
					commitPreparedStreamedChunksSystem(cmd, assets, state)
				}
				if state.Metrics.PendingPreparedBytes != 0 || state.Loader.Stats().PinnedBytes != metadata {
					t.Fatalf("%s consumption leaked ownership: %+v loader=%+v", branch, state.Metrics, state.Loader.Stats())
				}
				if branch == "prepare error" && (state.Metrics.PrepareErrorCount != 1 || state.InitErr == nil) {
					t.Fatal("error fixture did not exercise prepare-error consumption")
				}
				if branch == "commit error" && (state.Metrics.CommitErrorCount != 1 || state.InitErr == nil) {
					t.Fatal("missing placement did not exercise commit-error consumption")
				}
				if branch == "empty commit" && (len(state.LoadedSectorProxies) != 0 || state.Metrics.EntitiesCommittedLastFrame != 0) {
					t.Fatal("empty proxy unexpectedly spawned an entity")
				}
				if branch == "stale" {
					if _, ok := state.PendingLoads[coord]; !ok {
						t.Fatal("stale result removed new-generation full pending marker")
					}
					if _, ok := state.PendingProxyLoads[coord]; !ok {
						t.Fatal("stale result removed new-generation proxy pending marker")
					}
				}
				// Empty synthetic duplicate entries have no runtime entity/asset ownership.
				if branch == "duplicate" {
					delete(state.LoadedChunks, coord)
					delete(state.LoadedSectorProxies, coord)
				}
			})
		}
	}
}

func TestS2bPendingCommitBudgetPreservesCreditAndRetryHintPreventsRebuilds(t *testing.T) {
	path := s2bWorldPath(t)
	root := filepath.Dir(path)
	worldPath := filepath.Join(root, "imported-world.gkworld")
	world, err := content.LoadImportedWorld(worldPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := content.LoadImportedWorldChunk(filepath.Join(root, "imported-chunk.gkchunk"))
	if err != nil {
		t.Fatal(err)
	}
	second.Coord.X = 1
	secondPath := filepath.Join(root, "second.gkchunk")
	if err := content.SaveImportedWorldChunk(secondPath, second); err != nil {
		t.Fatal(err)
	}
	world.Entries = append(world.Entries, content.ImportedWorldChunkEntryDef{Coord: second.Coord, ChunkPath: "second.gkchunk", NonEmptyVoxelCount: second.NonEmptyVoxelCount})
	world.Sectors[0].FullChunkRefs = append(world.Sectors[0].FullChunkRefs, second.Coord)
	// An intentionally larger but valid proxy needs sole oversized admission.
	large := &content.ImportedWorldChunkDef{WorldID: world.WorldID, ChunkSize: 16, VoxelResolution: 1}
	for x := 0; x < 16; x++ {
		for y := 0; y < 16; y++ {
			for z := 0; z < 16; z++ {
				large.Voxels = append(large.Voxels, content.ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}
	large.NonEmptyVoxelCount = len(large.Voxels)
	if err := content.SaveImportedWorldChunk(filepath.Join(root, "large-proxy.gkchunk"), large); err != nil {
		t.Fatal(err)
	}
	world.Sectors[0].LODs[0].ChunkPath = "large-proxy.gkchunk"
	world.Sectors[0].LODs[0].NonEmptyVoxelCount = large.NonEmptyVoxelCount
	if err := content.SaveImportedWorld(worldPath, world); err != nil {
		t.Fatal(err)
	}
	config := StreamedLevelRuntimeConfig{LevelPath: path, MaxDecodedContentCacheBytes: 1, MaxPreparedGeometryCacheBytes: -1, MaxPendingPreparedBytes: 1 << 30, MaxChunkCommitsPerFrame: 1}
	_, probeCmd, probe, _ := s2bStartWorld(t, config)
	for _, coord := range []ChunkCoord{{}, {X: 1}} {
		startStreamedChunkPrepareJob(probe, buildStreamedChunkLoadJob(probe, coord))
		s2bUntil(t, func() bool { return streamedActivePrepareJobCounts(probe) == 0 })
	}
	probe.jobs.Wait()
	refreshStreamedRuntimeMetricsCounts(probe)
	budget := probe.Metrics.PendingPreparedBytes
	if budget <= 0 || len(probe.PreparedLoads) != 2 {
		t.Fatal("measurement fixture did not queue two full payloads")
	}
	if err := StopStreamedLevelRuntime(probeCmd); err != nil {
		t.Fatal(err)
	}
	config.MaxPendingPreparedBytes = budget
	_, cmd, state, assets := s2bStartWorld(t, config)
	metadata := state.Loader.Stats().PinnedBytes
	for _, coord := range []ChunkCoord{{}, {X: 1}} {
		state.DesiredChunks[coord] = struct{}{}
		state.KeepChunks[coord] = struct{}{}
		state.PendingLoads[coord] = struct{}{}
		state.CollisionChunks[coord] = struct{}{}
		state.DestructionChunks[coord] = struct{}{}
		startStreamedChunkPrepareJob(state, buildStreamedChunkLoadJob(state, coord))
		s2bUntil(t, func() bool { return streamedActivePrepareJobCounts(state) == 0 })
	}
	state.DesiredProxySectors[ChunkCoord{}] = struct{}{}
	state.PendingProxyLoads[ChunkCoord{}] = struct{}{}
	startStreamedSectorProxyPrepareJob(state, buildStreamedSectorProxyLoadJob(state, ChunkCoord{}, world.Sectors[0].LODs[0]))
	s2bUntil(t, func() bool { return len(state.PreparedProxyLoads) == 1 })
	state.jobs.Wait()
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Metrics.PendingPreparedBytes != budget || state.Metrics.PendingPreparedAdmissionRetries != 1 {
		t.Fatalf("pressure fixture did not defer proxy: %+v", state.Metrics)
	}
	commitPreparedStreamedChunksSystem(cmd, assets, state)
	cmd.app.FlushCommands()
	if len(state.PreparedLoads) != 1 || state.Metrics.PendingPreparedBytes <= 0 || state.Metrics.PendingPreparedBytes >= budget || state.Loader.Stats().PinnedBytes <= metadata {
		t.Fatalf("count commit budget dropped remaining queued ownership: %+v", state.Metrics)
	}
	observer := cmd.AddEntity(&TransformComponent{}, &StreamedLevelObserverComponent{})
	cmd.app.FlushCommands()
	misses := state.Loader.Stats().Misses
	retries := state.Metrics.PendingPreparedAdmissionRetries
	for range 8 {
		updateStreamedLevelObserverSystem(cmd, state)
	}
	state.jobs.Wait()
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Loader.Stats().Misses != misses || state.Metrics.PendingPreparedAdmissionRetries != retries || len(state.PreparedProxyLoads) != 0 {
		t.Fatalf("observer rebuilt retry demand before sufficient credit: %+v loader=%+v", state.Metrics, state.Loader.Stats())
	}
	commitPreparedStreamedChunksSystem(cmd, assets, state)
	cmd.app.FlushCommands()
	if state.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("second full commit did not restore credit")
	}
	// Move demand to the fallback-only session state through the normal ECS
	// observer lifetime. Full chunks remain until their fallback can commit.
	cmd.RemoveEntity(observer)
	cmd.app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	s2bUntil(t, func() bool { return len(state.PreparedProxyLoads) == 1 })
	state.jobs.Wait()
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Metrics.PendingPreparedBytes <= budget || state.Metrics.PendingPreparedOversizedAdmissions != 1 {
		t.Fatalf("retry demand could not make sole oversized progress: %+v", state.Metrics)
	}
	commitPreparedStreamedChunksSystem(cmd, assets, state)
	cmd.app.FlushCommands()
	if state.LoadedSectorProxies[ChunkCoord{}] == nil || state.Metrics.PendingPreparedBytes != 0 || state.Loader.Stats().PinnedBytes != metadata {
		t.Fatalf("retried proxy commit leaked ownership or did not publish: %+v", state.Metrics)
	}
}

func s2bLargeAuxSidecar(t *testing.T, state *StreamedLevelRuntimeState) (string, *content.ImportedWorldChunkAuxDef) {
	t.Helper()
	path := filepath.Join(filepath.Dir(state.LevelPath), "large-rejected.gkaux")
	aux := &content.ImportedWorldChunkAuxDef{
		WorldID: state.BaseWorldID, ChunkSize: state.Level.ChunkSize,
		VoxelResolution: state.Level.VoxelResolution,
		Records:         []content.ImportedWorldBrickAuxDef{{Bytes: make([]byte, 1<<20)}},
	}
	if err := content.SaveImportedWorldChunkAux(path, aux); err != nil {
		t.Fatal(err)
	}
	// The sidecar is a valid, hash-checked binary definition. Only the captured
	// job reference below is stale; the authored world remains valid.
	decoded, err := content.LoadImportedWorldChunkAux(path)
	if err != nil || decoded == nil || len(decoded.Records) != 1 || len(decoded.Records[0].Bytes) != 1<<20 || decoded.PayloadHash != aux.PayloadHash {
		t.Fatalf("invalid binary aux fixture: decoded=%v err=%v", decoded != nil, err)
	}
	return path, aux
}

func s2bQueueAuxPreparation(t *testing.T, state *StreamedLevelRuntimeState, kind string, ref *content.ImportedWorldChunkAuxRefDef) {
	t.Helper()
	coord := ChunkCoord{}
	if kind == "full" {
		job := buildStreamedChunkLoadJob(state, coord)
		if job.TerrainEntry == nil || job.ImportedWorldEntry == nil {
			t.Fatal("full fixture needs terrain and imported records")
		}
		job.ImportedWorldEntry.Aux = ref
		startStreamedChunkPrepareJob(state, job)
		s2bUntil(t, func() bool { return len(state.PreparedLoads) == 1 })
		state.jobs.Wait()
		prepared := <-state.PreparedLoads
		state.PreparedLoads <- prepared
		if prepared.Err != nil || prepared.TerrainChunk == nil || prepared.ImportedWorldChunk == nil || prepared.ImportedWorldAux != nil || prepared.ImportedWorldAuxHit || !prepared.ImportedWorldAuxMiss || prepared.PreparedImportedWorldGeometry == nil || prepared.PreparedImportedWorldGeometry.GetVoxelCount() == 0 {
			t.Fatalf("full preparation failed to preserve chunk/terrain and fallback geometry: err=%v aux=%v hit=%t miss=%t", prepared.Err, prepared.ImportedWorldAux != nil, prepared.ImportedWorldAuxHit, prepared.ImportedWorldAuxMiss)
		}
	} else {
		job := buildStreamedSectorProxyLoadJob(state, coord, state.ImportedWorldSectors[coord].LODs[0])
		job.LOD.Aux = ref
		startStreamedSectorProxyPrepareJob(state, job)
		s2bUntil(t, func() bool { return len(state.PreparedProxyLoads) == 1 })
		state.jobs.Wait()
		prepared := <-state.PreparedProxyLoads
		state.PreparedProxyLoads <- prepared
		if prepared.Err != nil || prepared.Chunk == nil || prepared.Aux != nil || prepared.AuxHit || !prepared.AuxMiss || prepared.PreparedGeometry == nil || prepared.PreparedGeometry.GetVoxelCount() == 0 {
			t.Fatalf("proxy preparation failed to preserve chunk and fallback geometry: err=%v aux=%v hit=%t miss=%t", prepared.Err, prepared.Aux != nil, prepared.AuxHit, prepared.AuxMiss)
		}
	}
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Metrics.PendingPreparedBytes <= 0 {
		t.Fatal("real preparation did not retain admitted payload ownership")
	}
}

func TestS2bPendingRejectedAuxReleasesDecodeLeaseBeforeConsumption(t *testing.T) {
	for _, kind := range []string{"full", "proxy"} {
		for _, mismatch := range []string{"normal version", "payload hash"} {
			t.Run(kind+"/"+mismatch, func(t *testing.T) {
				_, _, state, _ := s2bStartWorld(t, StreamedLevelRuntimeConfig{MaxDecodedContentCacheBytes: 1, MaxPendingPreparedBytes: 1 << 30})
				loader := state.Loader
				metadata := loader.Stats()
				s2bQueueAuxPreparation(t, state, kind, nil)
				withoutAux := loader.Stats()
				if withoutAux.PinnedBytes <= metadata.PinnedBytes {
					t.Fatal("measurement fixture did not lease queued chunk records")
				}
				drainStreamedPreparedResults(state)
				if loader.Stats().PinnedBytes != metadata.PinnedBytes {
					t.Fatal("baseline drain did not release transient chunk leases")
				}
				path, aux := s2bLargeAuxSidecar(t, state)
				ref := content.ImportedWorldChunkAuxRef(path, aux)
				if mismatch == "normal version" {
					ref.NormalBakeVersion = "stale-normal-version"
				} else {
					ref.PayloadHash = "stale-payload-hash"
				}
				s2bQueueAuxPreparation(t, state, kind, ref)
				// Aux=nil is the fallback result contract. Its decoded owner must
				// match the same queued preparation without an aux reference.
				rejected := loader.Stats()
				if rejected.PinnedBytes != withoutAux.PinnedBytes || rejected.Bytes != withoutAux.Bytes {
					t.Errorf("rejected sidecar retained a decoded lease while queued: bytes=%d pinned=%d; same preparation without aux bytes=%d pinned=%d", rejected.Bytes, rejected.PinnedBytes, withoutAux.Bytes, withoutAux.PinnedBytes)
				}
				drainStreamedPreparedResults(state)
				if after := loader.Stats(); after.Bytes != metadata.Bytes || after.PinnedBytes != metadata.PinnedBytes {
					t.Fatalf("result drain failed to release chunk records: metadata=%+v after=%+v", metadata, after)
				}
			})
		}
	}
}

func TestS2bPendingRejectedAuxPreservesIndependentScope(t *testing.T) {
	for _, kind := range []string{"full", "proxy"} {
		t.Run(kind, func(t *testing.T) {
			_, _, state, _ := s2bStartWorld(t, StreamedLevelRuntimeConfig{MaxDecodedContentCacheBytes: 1, MaxPendingPreparedBytes: 1 << 30})
			loader := state.Loader
			metadata := loader.Stats()
			path, aux := s2bLargeAuxSidecar(t, state)
			external := loader.NewScope()
			t.Cleanup(external.Close)
			borrowed, err := external.Loader().LoadImportedWorldChunkAux(path)
			if err != nil || borrowed == nil {
				t.Fatalf("external aux scope failed: %v", err)
			}
			externalStats := loader.Stats()
			if externalStats.PinnedBytes <= metadata.PinnedBytes {
				t.Fatal("external scope did not pin its decoded sidecar")
			}
			ref := content.ImportedWorldChunkAuxRef(path, aux)
			ref.NormalBakeVersion = "stale-normal-version"
			s2bQueueAuxPreparation(t, state, kind, ref)
			drainStreamedPreparedResults(state)
			if after := loader.Stats(); after.PinnedBytes != externalStats.PinnedBytes || after.Bytes != externalStats.Bytes {
				t.Fatalf("rejecting/draining a preparation dropped another scope's sidecar lease: external=%+v after=%+v", externalStats, after)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			got, err := external.Loader().LoadImportedWorldChunkAux(path)
			if err != nil || got != borrowed || len(got.Records[0].Bytes) != 1<<20 {
				t.Fatalf("independent sidecar scope became unusable after rejection/drain: %v", err)
			}
			external.Close()
			if after := loader.Stats(); after.PinnedBytes != metadata.PinnedBytes || after.Bytes != metadata.Bytes {
				t.Fatalf("external scope's final release retained sidecar storage: metadata=%+v after=%+v", metadata, after)
			}
		})
	}
}
