package gekko

import (
	"container/heap"
	"fmt"
	"time"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

// Active payloads stay in the bounded ready owner. This cursor owns partial
// entities until whole-chunk publication or durable cancellation/Stop removal.
type streamedChunkCommitTransaction struct {
	chunk                            *streamedLoadedChunk
	phase, cursor                    int
	readyID, workID                  uint64
	resumable, collision, cancelling bool
	fatal                            error
	duration                         time.Duration
	entities                         int
	entryGeneration                  uint64
	entryInitialized, entryCaptured  bool
	breakdown                        streamedChunkCommitBreakdown
}

func newStreamedChunkCommitTransaction(resumable bool) *streamedChunkCommitTransaction {
	return &streamedChunkCommitTransaction{resumable: resumable, chunk: &streamedLoadedChunk{
		TerrainEntities: make(map[EntityId]struct{}), ImportedWorldEntities: make(map[EntityId]struct{}),
		PlacementRoots: make(map[string]EntityId), OwnedEntities: make(map[EntityId]struct{}), ObjectEntities: make(map[string]EntityId),
	}}
}

func (tx *streamedChunkCommitTransaction) live(state *StreamedLevelRuntimeState) bool {
	return state.Generation == tx.entryGeneration && state.Initialized == tx.entryInitialized
}

// Advance one terrain/imported/placement atomic unit. Empty stages do not consume
// placement capacity. Publication belongs only to the final successful unit.
func advanceStreamedChunkCommit(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, prepared streamedPreparedChunk, tx *streamedChunkCommitTransaction) (entityCount int, placementUnit, done bool, err error) {
	if !tx.entryCaptured {
		tx.entryCaptured, tx.entryGeneration, tx.entryInitialized = true, state.Generation, state.Initialized
	}
	start := time.Now()
	if tx.resumable {
		tx.breakdown.restore(state)
	}
	defer func() {
		if !tx.resumable || !tx.live(state) {
			return
		}
		duration := time.Since(start)
		tx.breakdown = captureStreamedChunkCommitBreakdown(state)
		state.Metrics.LastCommitCoord = prepared.Coord
		tx.duration += duration
		tx.entities += entityCount
		state.Metrics.LastCommitDuration = tx.duration
		state.Metrics.LastCommitEntityCount = tx.entities
		state.Metrics.TotalCommitDuration += duration
	}()
	chunk := tx.chunk
	for {
		switch tx.phase {
		case 0:
			tx.phase++

			if prepared.TerrainChunk != nil && prepared.TerrainChunk.NonEmptyVoxelCount > 0 {
				terrainStart := time.Now()
				terrainID := terrainIDForPreparedChunk(state, prepared.TerrainChunk)
				backingRemoval := state.voxelBackingRemovalFor(content.VoxelBackingOwnerTerrain, terrainID, prepared.TerrainChunk.Coord)
				preparedAssetID := AssetId{}
				adopted := false
				if backingRemoval == nil {
					preparedAssetID, adopted = assets.adoptStreamedVoxelGeometry(prepared.terrainRegistration, prepared.preparedTerrainGeometry)
					if adopted {
						state.Metrics.PreparedGeometryAssetAdoptions++
					}
				} else {
					prepared.terrainRegistration.release()
				}
				entity := spawnAuthoredTerrainChunkEntityWithPreparedAsset(cmd, assets, state.LevelRoot, state.TerrainPalette, AuthoredTerrainSpawnDef{
					LevelID:        state.LevelID,
					TerrainID:      terrainID,
					TerrainGroupID: terrainGroupIDForStreamedState(state),
					Chunk:          prepared.TerrainChunk,
					BackingRemoval: backingRemoval,
				}, preparedAssetID)
				if adopted {
					state.retainStreamedTerrainGeometryAsset(entity, assets, preparedAssetID)
				}
				state.Metrics.LastCommitTerrainDuration += time.Since(terrainStart)
				chunk.TerrainEntities[entity] = struct{}{}
				chunk.OwnedEntities[entity] = struct{}{}
				stageStreamedRenderTarget(cmd, state, entity, prepared.Coord, streamedRenderTerrain)
				recordStreamedCommitFlush(cmd, state)
				entityCount++
				if !tx.live(state) {
					return entityCount, false, false, nil
				}
				clearEntityVoxelDirty(cmd, entity)
				for _, hook := range state.Config.TerrainHooks {
					invalidateStreamedRenderTicketFloor(state)
					hook(cmd, PostSpawnTerrainContext{
						ChunkCoord: prepared.Coord,
						LevelID:    state.LevelID,
						TerrainID:  terrainIDForPreparedChunk(state, prepared.TerrainChunk),
						RootEntity: entity,
					})
					invalidateStreamedRenderTicketFloor(state)
					if !tx.live(state) {
						return entityCount, false, false, nil
					}
					if tx.resumable && state.InitErr != nil {
						return entityCount, false, false, state.InitErr
					}
				}
			}

			if prepared.TerrainChunk != nil && prepared.TerrainChunk.NonEmptyVoxelCount > 0 {
				break
			}
			continue
		case 1:
			tx.phase++

			if prepared.ImportedWorldChunk != nil && (prepared.ImportedWorldChunk.NonEmptyVoxelCount > 0 || state.BaseWorldBacking != nil) {
				worldStart := time.Now()
				collisionEnabled := state.streamedImportedWorldChunkCollisionEnabled(prepared.Coord)
				destructionEnabled := state.streamedImportedWorldChunkDestructionEnabled(prepared.Coord)
				backingRemoval := state.voxelBackingRemovalFor(content.VoxelBackingOwnerImportedWorld, importedWorldIDForPreparedChunk(state, prepared.ImportedWorldChunk), prepared.ImportedWorldChunk.Coord)
				var backingProvider VoxelBackingProvider
				if destructionEnabled || backingRemoval != nil {
					backingProvider = state.BaseWorldBacking
				}
				privateGeometry := destructionEnabled || backingProvider != nil
				spawnTiming := AuthoredImportedWorldSpawnTiming{}
				geometryAssetStart := time.Now()
				preparedGeometryAsset := AssetId{}
				preparedGeometry := prepared.PreparedImportedWorldGeometry
				if prepared.geometrySource != nil && state.BaseWorldBacking != nil {
					preparedGeometry = state.PreparedGeometryCache.densePreparedSource(prepared.PreparedImportedWorldGeometryCacheKey, prepared.geometrySource)
				}
				if state.BaseWorldBacking != nil {
					// Backing may have changed since the worker captured its job. Keep the
					// existing registration/spawn path whenever live backing is present.
					prepared.registration.release()
				}
				if backingProvider == nil {
					if state.BaseWorldBacking == nil {
						var adopted bool
						if prepared.geometrySource != nil {
							preparedGeometryAsset, _, adopted = state.PreparedGeometryCache.acquirePreparedSourceAsset(assets, prepared.PreparedImportedWorldGeometryCacheKey, prepared.geometrySource, prepared.registration)
						} else {
							preparedGeometryAsset, _, adopted = state.PreparedGeometryCache.acquirePreparedAsset(assets, prepared.PreparedImportedWorldGeometryCacheKey, preparedGeometry, prepared.registration)
						}
						if adopted {
							state.Metrics.PreparedGeometryAssetAdoptions++
						}
					} else {
						preparedGeometryAsset, _ = state.PreparedGeometryCache.acquireAsset(assets, prepared.PreparedImportedWorldGeometryCacheKey, preparedGeometry)
					}
				}
				geometryAssetDuration := time.Since(geometryAssetStart)
				entity := spawnAuthoredImportedWorldChunkEntity(cmd, state.LevelRoot, state.BaseWorldPalette, AuthoredImportedWorldSpawnDef{
					LevelID:                state.LevelID,
					WorldID:                importedWorldIDForPreparedChunk(state, prepared.ImportedWorldChunk),
					ShadowGroupID:          importedWorldGroupIDForStreamedState(state),
					Chunk:                  prepared.ImportedWorldChunk,
					CollisionEnabled:       collisionEnabled,
					DestructionEnabled:     destructionEnabled,
					ShareTerrainGeometry:   !privateGeometry,
					RetainRendererGeometry: !privateGeometry,
					PreparedGeometry:       preparedGeometry,
					PreparedGeometryAsset:  preparedGeometryAsset,
					BackingProvider:        backingProvider,
					BackingSourceHash:      state.BaseWorldBackingSourceHash,
					BackingRemoval:         backingRemoval,
					Timing:                 &spawnTiming,
				})
				recordImportedWorldSpawnTiming(state, spawnTiming)
				state.Metrics.LastCommitWorldRegisterDuration += geometryAssetDuration
				state.Metrics.LastCommitWorldDuration += time.Since(worldStart)
				chunk.ImportedWorldEntities[entity] = struct{}{}
				chunk.OwnedEntities[entity] = struct{}{}
				if preparedGeometryAsset != (AssetId{}) {
					chunk.ImportedWorldGeometryAssets = append(chunk.ImportedWorldGeometryAssets, streamedGeometryAssetLease{ID: preparedGeometryAsset, Server: assets})
				}
				tx.collision = collisionEnabled
				stageStreamedRenderTarget(cmd, state, entity, prepared.Coord, streamedRenderImported)
				recordStreamedCommitFlush(cmd, state)
				entityCount++
				if !tx.live(state) {
					return entityCount, false, false, nil
				}
				clearEntityVoxelDirty(cmd, entity)
			}
			if prepared.ImportedWorldChunk != nil && prepared.ImportedWorldChunk.NonEmptyVoxelCount == 0 && len(prepared.ImportedWorldChunk.Voxels) == 0 && state.BaseWorldBacking == nil && prepared.Generation == state.Generation {
				chunk.importedEmptyGeneration = prepared.Generation
			}

			if prepared.ImportedWorldChunk != nil && (prepared.ImportedWorldChunk.NonEmptyVoxelCount > 0 || state.BaseWorldBacking != nil) {
				break
			}
			continue
		case 2:
			if tx.cursor == len(prepared.PlacementItems) {
				break
			}
			placementUnit = true
			placement := prepared.PlacementItems[tx.cursor]
			tx.cursor++ // Fatal units never retry, even if they fail after spawning IDs.
			if tx.resumable {
				if _, deleted := state.deletedPlacementIDs[placement.PlacementID]; deleted {
					break
				}
				placement.Transform = effectiveLevelTransform(placement.PlacementID, placement.Transform, state.placementOverrideMap)
				position := placement.Transform.Position
				if ChunkCoordFromPosition(mgl32.Vec3{position[0], position[1], position[2]}, state.ChunkSize) != prepared.Coord {
					break
				}
			}
			loader := state.Loader
			if prepared.loadScope != nil {
				loader = prepared.loadScope.Loader()
			}
			placementStart := time.Now()
			spawnResult, spawnErr := spawnAuthoredLevelPlacementWithOwnership(cmd, assets, loader, state.LevelRoot, state.LevelID, state.LevelPath, AuthoredPlacementSpawnDef{
				PlacementID: placement.PlacementID, VolumeID: placement.VolumeID, AssetPath: placement.AssetPath, Transform: placement.Transform, Tags: append([]string(nil), placement.Tags...),
			}, func(entity EntityId, item string, root, voxel bool) {
				chunk.OwnedEntities[entity] = struct{}{}
				if tx.resumable || root || item != "" {
					entityCount++
				}
				if root {
					chunk.PlacementRoots[placement.PlacementID] = entity
				}
				// Collapsed composites retain their existing unsupported per-item snapshot
				// authority; all IDs still belong to this transaction for teardown.
				if item != "" && voxel {
					key := voxelObjectRuntimeKey(placement.PlacementID, item)
					chunk.ObjectEntities[key] = entity
					state.ObjectChunk[key] = prepared.Coord
				}
			})
			state.Metrics.LastCommitPlacementDuration += time.Since(placementStart)
			if !tx.live(state) {
				return entityCount, placementUnit, false, nil
			}
			if spawnErr != nil {
				return entityCount, placementUnit, false, spawnErr
			}
			recordStreamedCommitFlush(cmd, state)
			if !tx.live(state) {
				return entityCount, placementUnit, false, nil
			}
			for itemID, entity := range spawnResult.EntitiesByAssetID {
				key := voxelObjectRuntimeKey(placement.PlacementID, itemID)
				snapshot := prepared.ObjectSnapshots[key]
				if tx.resumable {
					snapshot = nil
					if override, exists := state.voxelOverrideMap[key]; exists {
						snapshot, err = content.LoadVoxelObjectSnapshot(content.ResolveDocumentPath(override.SnapshotPath, state.WorldDeltaPath))
						if err != nil {
							return entityCount, placementUnit, false, err
						}
					}
				}
				if snapshot != nil {
					snapshotStart := time.Now()
					if err = applyStreamedVoxelObjectSnapshotToEntity(cmd, state, entity, snapshot, prepared.objectSnapshotGeometry[key]); err != nil {
						return entityCount, placementUnit, false, err
					}
					state.Metrics.LastCommitPlacementDuration += time.Since(snapshotStart)
					recordStreamedCommitFlush(cmd, state)
					if !tx.live(state) {
						return entityCount, placementUnit, false, nil
					}
				}
				if entityHasVoxelModel(cmd, entity) {
					clearEntityVoxelDirty(cmd, entity)
				} else {
					delete(chunk.ObjectEntities, key)
					delete(state.ObjectChunk, key)
				}
			}
			clearEntityVoxelDirty(cmd, spawnResult.RootEntity)
			for _, hook := range state.Config.PlacementHooks {
				invalidateStreamedRenderTicketFloor(state)
				hook(cmd, PostSpawnPlacementContext{ChunkCoord: prepared.Coord, LevelID: state.LevelID,
					Placement:  AuthoredPlacementSpawnDef{PlacementID: placement.PlacementID, VolumeID: placement.VolumeID, AssetPath: placement.AssetPath, Transform: placement.Transform, Tags: append([]string(nil), placement.Tags...)},
					RootEntity: spawnResult.RootEntity, SpawnResult: spawnResult,
				})
				invalidateStreamedRenderTicketFloor(state)
				if !tx.live(state) {
					return entityCount, placementUnit, false, nil
				}
				if tx.resumable && state.InitErr != nil {
					return entityCount, placementUnit, false, state.InitErr
				}
			}
		}
		break
	}
	if tx.phase == 2 && tx.cursor == len(prepared.PlacementItems) {
		if !tx.live(state) {
			return entityCount, placementUnit, false, nil
		}
		state.LoadedChunks[prepared.Coord] = chunk
		delete(state.prepareScheduler.waiting, streamedPrepareIdentity{coord: prepared.Coord, kind: streamedPrepareFull})
		state.Metrics.CommittedChunkCount++
		state.Metrics.FullChunkCommitCount++
		if tx.collision {
			state.Metrics.CollisionChunkCommitCount++
		}
		return entityCount, placementUnit, true, nil
	}
	return entityCount, placementUnit, false, nil
}

func activeStreamedChunkCommit(state *StreamedLevelRuntimeState, coord ChunkCoord) *streamedChunkCommitTransaction {
	id := state.readyCommits.activeChunks[coord]
	return state.readyCommits.results[id].active
}

// Only persistence and teardown may view a partial owner as a loaded chunk.
func streamedLoadedOrActiveChunk(state *StreamedLevelRuntimeState, coord ChunkCoord) *streamedLoadedChunk {
	if loaded := state.LoadedChunks[coord]; loaded != nil {
		return loaded
	}
	if active := activeStreamedChunkCommit(state, coord); active != nil {
		return active.chunk
	}
	return nil
}

func (tx *streamedChunkCommitTransaction) bindWork(state *StreamedLevelRuntimeState) func() {
	owner := &state.streamingWork
	previous, committing := owner.active, owner.committing
	owner.active, owner.committing = tx.workID, true
	return func() { owner.active, owner.committing = previous, committing }
}

func finishStreamedActiveChunkCommit(state *StreamedLevelRuntimeState, tx *streamedChunkCommitTransaction) {
	result, present := state.readyCommits.take(tx.readyID)
	if !present {
		return
	}
	p := result.chunk
	acknowledgeStreamedChunkPreparation(state, *p)
	finishStreamedWorkAttempt(state, p.Generation, p.prepareCancel)
	if item := state.streamingWork.items[tx.workID]; item != nil {
		item.cpu = false
		state.streamingWork.reap(tx.workID)
	}
	p.release()
}

func finishStreamedActiveChunkSynchronously(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, tx *streamedChunkCommitTransaction) error {
	defer beginStreamedRenderTicketBatch(state)()
	if tx.fatal != nil {
		return tx.fatal
	}
	result := state.readyCommits.results[tx.readyID]
	if result.chunk == nil {
		return nil
	}
	if !streamedActiveCommitCurrent(state, result.chunk) {
		tx.cancelling = true
	}
	if tx.cancelling {
		if err := persistChunkOverrides(cmd, state, result.chunk.Coord, tx.chunk); err != nil {
			return err
		}
		return fmt.Errorf("chunk commit is awaiting durable cancellation")
	}
	for {
		restore := tx.bindWork(state)
		_, _, done, err := advanceStreamedChunkCommit(cmd, assets, state, *result.chunk, tx)
		restore()
		if !tx.live(state) {
			return nil
		}
		if err != nil {
			tx.fatal = err
			state.InitErr = err
			return err
		}
		if done {
			finishStreamedActiveChunkCommit(state, tx)
			refreshStreamedRuntimeMetricsCounts(state)
			return nil
		}
	}
}

func streamedCommitPlacementNext(tx *streamedChunkCommitTransaction, p *streamedPreparedChunk, state *StreamedLevelRuntimeState) bool {
	if tx.phase == 0 && p.TerrainChunk != nil && p.TerrainChunk.NonEmptyVoxelCount > 0 {
		return false
	}
	if tx.phase <= 1 && p.ImportedWorldChunk != nil && (p.ImportedWorldChunk.NonEmptyVoxelCount > 0 || state.BaseWorldBacking != nil) {
		return false
	}
	return tx.cursor < len(p.PlacementItems)
}

func serviceStreamedPlacementCommitFrontier(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, queue streamedReadyQueue, start time.Time, generation uint64) {
	ids := make([]uint64, 0, len(queue))
	for queue.Len() > 0 {
		ids = append(ids, heap.Pop(&queue).(streamedReadyCandidate).id)
	}
	advanced := make(map[streamedPrepareIdentity]struct{})
	units := 0
	placementLimited := state.renderManaged && state.Config.MaxPlacementCommitUnitsPerFrame > 0
	for {
		progress := false
		for _, id := range ids {
			if !state.Initialized || state.Generation != generation || state.InitErr != nil {
				return
			}
			if millis := state.Config.MaxStreamingCommitMillis; millis > 0 && time.Since(start) >= time.Duration(millis)*time.Millisecond {
				state.Metrics.CommitBudgetHitLastFrame, state.Metrics.CommitBudgetReason = true, "time"
				return
			}
			result, present := state.readyCommits.results[id]
			if !present {
				continue
			}
			if result.active != nil {
				if !streamedActiveCommitCurrent(state, result.chunk) {
					result.active.cancelling = true
				}
				if result.active.cancelling || result.active.fatal != nil {
					continue
				}
			}
			if result.active == nil && (streamedReadyPriority(state, result) == -1 ||
				result.proxy != nil && result.proxy.retryCost > 0 || result.chunk != nil && result.chunk.retryCost > 0) {
				state.readyCommits.take(id)
				if result.proxy != nil {
					consumeStreamedPreparedProxy(cmd, assets, state, *result.proxy)
				} else {
					consumeStreamedPreparedChunk(cmd, assets, state, *result.chunk)
				}
				progress = true
				continue
			}
			if _, seen := advanced[result.identity]; !seen && state.Config.MaxChunkCommitsPerFrame > 0 && len(advanced) >= state.Config.MaxChunkCommitsPerFrame {
				state.Metrics.CommitBudgetHitLastFrame, state.Metrics.CommitBudgetReason = true, "chunk_count"
				continue
			}
			if result.proxy != nil {
				state.readyCommits.take(id)
				consumeStreamedPreparedProxy(cmd, assets, state, *result.proxy)
				advanced[result.identity] = struct{}{}
				progress = true
				continue
			}
			p := result.chunk
			if result.active == nil && !placementLimited {
				state.readyCommits.take(id)
				consumeStreamedPreparedChunk(cmd, assets, state, *p)
				advanced[result.identity] = struct{}{}
				progress = true
				continue
			}
			tx := result.active
			if tx == nil {
				if activeStreamedChunkCommit(state, p.Coord) != nil {
					continue
				}
				tx = newStreamedChunkCommitTransaction(true)
			}
			if placementLimited && streamedCommitPlacementNext(tx, p, state) && units >= state.Config.MaxPlacementCommitUnitsPerFrame {
				state.Metrics.CommitBudgetHitLastFrame, state.Metrics.CommitBudgetReason = true, "placement_count"
				continue
			}
			if result.active == nil {
				tx.readyID = id
				tx.workID = state.streamingWork.attempts[p.prepareCancel]
				if item := state.streamingWork.items[tx.workID]; item == nil || item.generation != p.Generation {
					tx.workID = state.streamingWork.create(p.Generation)
				}
				if state.readyCommits.activeChunks == nil {
					state.readyCommits.activeChunks = make(map[ChunkCoord]uint64)
				}
				state.readyCommits.activeChunks[p.Coord] = id
				result.active = tx
				state.readyCommits.results[id] = result
				recordPreparedStreamedChunkMetrics(state, *p)
				delete(state.pendingChunkCostHints, p.Coord)
			}
			for {
				if millis := state.Config.MaxStreamingCommitMillis; millis > 0 && time.Since(start) >= time.Duration(millis)*time.Millisecond {
					state.Metrics.CommitBudgetHitLastFrame, state.Metrics.CommitBudgetReason = true, "time"
					return
				}
				if placementLimited && streamedCommitPlacementNext(tx, p, state) && units >= state.Config.MaxPlacementCommitUnitsPerFrame {
					break
				}

				restore := tx.bindWork(state)
				entities, placement, done, err := advanceStreamedChunkCommit(cmd, assets, state, *p, tx)
				restore()
				if !state.Initialized || state.Generation != generation {
					return
				}
				advanced[result.identity] = struct{}{}
				progress = true
				state.Metrics.EntitiesCommittedLastFrame += entities
				if placement {
					units++
					state.Metrics.PlacementCommitUnitsLastFrame = units
				}
				if err != nil {
					tx.fatal = err
					state.Metrics.CommitErrorCount++
					if state.InitErr == nil {
						state.InitErr = err
					}
					return
				}
				if done {
					state.Metrics.ChunksCommittedLastFrame++
					state.Metrics.FullChunksCommittedLastFrame++
					if tx.collision {
						state.Metrics.CollisionChunksCommittedLastFrame++
					}
					finishStreamedActiveChunkCommit(state, tx)
					if sector, exists := state.ImportedChunkSector[p.Coord]; exists {
						reconcileStreamedSectorProxyAfterFullCommit(cmd, state, sector)
					}
				} else if retained, exists := state.readyCommits.results[id]; exists {
					retained.birth = state.readyCommits.sequence
					state.readyCommits.results[id] = retained
				}
				if placement || done {
					break
				}
			}

		}
		if !progress {
			if maxCommits := state.Config.MaxChunkCommitsPerFrame; maxCommits > 0 && len(advanced) >= maxCommits {
				state.Metrics.CommitBudgetHitLastFrame, state.Metrics.CommitBudgetReason = true, "chunk_count"
			} else if millis := state.Config.MaxStreamingCommitMillis; millis > 0 && time.Since(start) >= time.Duration(millis)*time.Millisecond {
				state.Metrics.CommitBudgetHitLastFrame, state.Metrics.CommitBudgetReason = true, "time"
			}
			return
		}
	}
}

func streamedActiveCommitCurrent(state *StreamedLevelRuntimeState, prepared *streamedPreparedChunk) bool {
	_, desired := state.DesiredChunks[prepared.Coord]
	return desired && prepared.Generation == state.Generation && state.chunkPrepareCancels[prepared.Coord] == prepared.prepareCancel && !streamedPreparationCancelled(prepared.prepareCancel)
}

func cancelStreamedActiveChunkPreparation(state *StreamedLevelRuntimeState, prepared *streamedPreparedChunk) {
	if prepared.Generation == state.Generation && state.chunkPrepareCancels[prepared.Coord] == prepared.prepareCancel {
		cancelStreamedPreparation(state.chunkPrepareCancels[prepared.Coord])
	}
}

// These scalar diagnostics follow one cursor across interleaved chunk service.
type streamedChunkCommitBreakdown struct {
	terrain, world, build, register, entity, placement, flush time.Duration
	voxels, flushes                                           int
}

func captureStreamedChunkCommitBreakdown(state *StreamedLevelRuntimeState) streamedChunkCommitBreakdown {
	m := &state.Metrics
	return streamedChunkCommitBreakdown{terrain: m.LastCommitTerrainDuration, world: m.LastCommitWorldDuration,
		build: m.LastCommitWorldBuildDuration, register: m.LastCommitWorldRegisterDuration, entity: m.LastCommitWorldEntityDuration,
		placement: m.LastCommitPlacementDuration, flush: m.LastCommitFlushDuration, voxels: m.LastCommitWorldVoxelCount, flushes: m.LastCommitFlushCount}
}

func (b streamedChunkCommitBreakdown) restore(state *StreamedLevelRuntimeState) {
	m := &state.Metrics
	m.LastCommitTerrainDuration, m.LastCommitWorldDuration = b.terrain, b.world
	m.LastCommitWorldBuildDuration, m.LastCommitWorldRegisterDuration, m.LastCommitWorldEntityDuration = b.build, b.register, b.entity
	m.LastCommitPlacementDuration, m.LastCommitFlushDuration = b.placement, b.flush
	m.LastCommitWorldVoxelCount, m.LastCommitFlushCount = b.voxels, b.flushes
}
