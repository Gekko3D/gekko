package gekko

import (
	"fmt"
	"reflect"
	"slices"
	"unsafe"

	"github.com/gekko3d/gekko/content"
)

type streamedPersistenceTransaction struct {
	Generation          uint64
	Coord               ChunkCoord
	Loaded              *streamedLoadedChunk
	Entities            []streamedPersistenceEntity
	Bytes               int64
	Manifest            bool
	Candidate, Baseline *content.WorldDeltaDef
	Paths               []string
	Navigation          []*content.ImportedWorldChunkDef
	Results             chan streamedPersistenceResult
}
type streamedPersistenceResult struct {
	Navigation []*content.ImportedWorldChunkDef
	Paths      []string
	Err        error
}

func streamedPersistenceWriter(state *StreamedLevelRuntimeState) func(string, *content.WorldDeltaDef) error {
	if state.worldDeltaWriter != nil {
		return state.worldDeltaWriter
	}
	return content.SaveWorldDelta
}
func refreshStreamedPersistenceMetrics(state *StreamedLevelRuntimeState) {
	if state == nil {
		return
	}
	max := state.Config.MaxPendingPersistenceBytes
	if max == 0 {
		max = 128 << 20
	}
	state.Metrics.PendingPersistenceMaxBytes = max
	state.Metrics.PendingPersistenceBytes = state.persistenceBytes
	state.Metrics.PendingPersistenceCount = 0
	if state.persistenceTransaction != nil || state.worldDeltaSaveActive {
		state.Metrics.PendingPersistenceCount = 1
	}
	state.Metrics.PendingPersistenceOverBudgetBytes = 0
	if state.persistenceBytes > max {
		state.Metrics.PendingPersistenceOverBudgetBytes = state.persistenceBytes - max
	}
	state.Metrics.DirtyPinnedChunkCount = len(state.persistenceIntents)
}
func admitStreamedPersistenceBytes(state *StreamedLevelRuntimeState, n int64) {
	state.persistenceBytes = n
	max := state.Config.MaxPendingPersistenceBytes
	if max == 0 {
		max = 128 << 20
	}
	if n > max {
		state.Metrics.PendingPersistenceOversizedAdmissions++
	}
	refreshStreamedPersistenceMetrics(state)
}
func persistenceNormalDemand(cmd *Commands, state *StreamedLevelRuntimeState, coord ChunkCoord) bool {
	if streamedLoadedChunkNeedsResidencyUpgrade(cmd, state, coord) {
		return true
	}
	if _, keep := state.KeepChunks[coord]; keep {
		return false
	}
	return !streamedChunkNeedsRenderProxyBeforeUnload(cmd, state, coord)
}
func requestStreamedChunkPersistence(cmd *Commands, state *StreamedLevelRuntimeState, coord ChunkCoord) {
	loaded := state.LoadedChunks[coord]
	if loaded == nil {
		return
	}
	intent := collectStreamedPersistenceIntent(cmd, state, coord, loaded)
	if intent == nil {
		// Even an obsolete captured owner keeps its chunk until the worker result
		// is discarded. Removing now would release leases before its IO barrier.
		if tx := state.persistenceTransaction; tx != nil && tx.Coord == coord && tx.Loaded == loaded {
			refreshStreamedPersistenceMetrics(state)
			return
		}
		removeStreamedChunk(cmd, state, coord)
		return
	}
	if state.persistenceTransaction != nil || state.worldDeltaSaveActive {
		state.Metrics.PendingPersistenceAdmissionRetries++
		refreshStreamedPersistenceMetrics(state)
		return
	}
	n, err := preflightStreamedPersistence(cmd, state, intent)
	if err != nil {
		state.Metrics.PendingPersistenceAdmissionRetries++
		state.Metrics.PersistenceLastError = err.Error()
		refreshStreamedPersistenceMetrics(state)
		return
	}
	entities := captureStreamedPersistence(cmd, state, intent)
	if len(entities) == 0 {
		delete(state.persistenceIntents, coord)
		removeStreamedChunk(cmd, state, coord)
		refreshStreamedPersistenceMetrics(state)
		return
	}
	tx := &streamedPersistenceTransaction{Generation: state.Generation, Coord: coord, Loaded: loaded, Entities: entities, Bytes: n, Results: make(chan streamedPersistenceResult, 1)}
	state.persistenceTransaction = tx
	admitStreamedPersistenceBytes(state, n)
	inputs := make([]streamedPersistenceInput, len(entities))
	for i, e := range entities {
		inputs[i] = e.Input
	}
	dir, deltaPath := state.WorldDataDir, state.WorldDeltaPath
	mu := &state.runtimeEditPersistenceMu
	results, jobs := tx.Results, &state.jobs
	state.jobs.Add(1)
	go func() {
		defer jobs.Done()
		result := streamedPersistenceResult{Paths: make([]string, len(inputs)), Navigation: make([]*content.ImportedWorldChunkDef, len(inputs))}
		for i, input := range inputs {
			if input.Removal != nil && !input.Navigation {
				continue
			}
			xbm := persistenceInputMap(input)
			var imported *content.ImportedWorldChunkDef
			if input.Kind == "imported" || input.Navigation {
				world, coord := input.Owner, input.Coord
				if input.Removal != nil {
					world, coord = input.Removal.OwnerID, input.Removal.ChunkCoord
				}
				imported = importedWorldChunkDefFromXBrickMap(world, coord, input.ChunkSize, input.Resolution, xbm)
				if input.Navigation {
					imported.Voxels = clonePersistenceSlice(imported.Voxels)
					imported.Tags = clonePersistenceSlice(imported.Tags)
					result.Navigation[i] = imported
				}
			}
			if input.Removal != nil {
				continue
			}
			var name string
			var serialize func(string) error
			switch input.Kind {
			case "terrain":
				snapshot := terrainChunkDefFromXBrickMap(input.Owner, input.Coord, input.ChunkSize, input.Resolution, xbm)
				name = fmt.Sprintf("terrain_%s_%d_%d_%d.gkchunk", sanitizePathSegment(input.Owner), input.Coord.X, input.Coord.Y, input.Coord.Z)
				serialize = func(path string) error { return content.SaveTerrainChunk(path, snapshot) }
			case "imported":
				snapshot := imported
				name = fmt.Sprintf("imported_%s_%d_%d_%d.gkchunk", sanitizePathSegment(input.Owner), input.Coord.X, input.Coord.Y, input.Coord.Z)
				serialize = func(path string) error { return content.SaveImportedWorldChunk(path, snapshot) }
			default:
				snapshot := VoxelObjectSnapshotFromXBrickMap(xbm)
				name = fmt.Sprintf("object_%s_%s.gkvoxobj", sanitizePathSegment(input.Owner), sanitizePathSegment(input.Item))
				serialize = func(path string) error { return content.SaveVoxelObjectSnapshot(path, snapshot) }
			}
			mu.Lock()
			path, err := writeStreamedLevelPayload(dir, name, serialize)
			mu.Unlock()
			if err != nil {
				result.Err = err
				break
			}
			result.Paths[i] = content.AuthorDocumentPath(path, deltaPath)
		}
		results <- result
	}()
}

func replacePersistenceTerrain(delta *content.WorldDeltaDef, value content.TerrainChunkOverrideDef, remove bool) {
	index := slices.IndexFunc(delta.TerrainChunkOverrides, func(v content.TerrainChunkOverrideDef) bool {
		return v.TerrainID == value.TerrainID && v.ChunkCoord == value.ChunkCoord
	})
	if index >= 0 {
		if remove {
			delta.TerrainChunkOverrides = slices.Delete(delta.TerrainChunkOverrides, index, index+1)
		} else {
			delta.TerrainChunkOverrides[index] = value
		}
	} else if !remove {
		delta.TerrainChunkOverrides = append(delta.TerrainChunkOverrides, value)
	}
}
func replacePersistenceImported(delta *content.WorldDeltaDef, value content.ImportedWorldChunkOverrideDef, remove bool) {
	index := slices.IndexFunc(delta.ImportedWorldChunkOverrides, func(v content.ImportedWorldChunkOverrideDef) bool {
		return v.WorldID == value.WorldID && v.ChunkCoord == value.ChunkCoord
	})
	if index >= 0 {
		if remove {
			delta.ImportedWorldChunkOverrides = slices.Delete(delta.ImportedWorldChunkOverrides, index, index+1)
		} else {
			delta.ImportedWorldChunkOverrides[index] = value
		}
	} else if !remove {
		delta.ImportedWorldChunkOverrides = append(delta.ImportedWorldChunkOverrides, value)
	}
}
func replacePersistenceObject(delta *content.WorldDeltaDef, value content.VoxelObjectOverrideDef) {
	index := slices.IndexFunc(delta.VoxelObjectOverrides, func(v content.VoxelObjectOverrideDef) bool {
		return v.PlacementID == value.PlacementID && v.ItemID == value.ItemID
	})
	if index >= 0 {
		delta.VoxelObjectOverrides[index] = value
	} else {
		delta.VoxelObjectOverrides = append(delta.VoxelObjectOverrides, value)
	}
}
func replacePersistenceRemoval(delta *content.WorldDeltaDef, value content.VoxelBackingRemovalDef) {
	index := slices.IndexFunc(delta.VoxelBackingRemovals, func(v content.VoxelBackingRemovalDef) bool {
		return v.OwnerKind == value.OwnerKind && v.OwnerID == value.OwnerID && v.ChunkCoord == value.ChunkCoord
	})
	copy := value
	copy.Bricks = clonePersistenceSlice(value.Bricks)
	if index >= 0 {
		delta.VoxelBackingRemovals[index] = copy
	} else {
		delta.VoxelBackingRemovals = append(delta.VoxelBackingRemovals, copy)
	}
}
func patchStreamedPersistenceCandidate(delta *content.WorldDeltaDef, tx *streamedPersistenceTransaction) {
	for i, e := range tx.Entities {
		input := e.Input
		if input.Removal != nil {
			def := *input.Removal
			replacePersistenceRemoval(delta, def)
			if def.OwnerKind == content.VoxelBackingOwnerTerrain {
				replacePersistenceTerrain(delta, content.TerrainChunkOverrideDef{TerrainID: def.OwnerID, ChunkCoord: def.ChunkCoord}, true)
			}
			if def.OwnerKind == content.VoxelBackingOwnerImportedWorld {
				replacePersistenceImported(delta, content.ImportedWorldChunkOverrideDef{WorldID: def.OwnerID, ChunkCoord: def.ChunkCoord}, true)
			}
			continue
		}
		switch input.Kind {
		case "terrain":
			replacePersistenceTerrain(delta, content.TerrainChunkOverrideDef{TerrainID: input.Owner, ChunkCoord: input.Coord, SnapshotPath: tx.Paths[i]}, false)
		case "imported":
			replacePersistenceImported(delta, content.ImportedWorldChunkOverrideDef{WorldID: input.Owner, ChunkCoord: input.Coord, SnapshotPath: tx.Paths[i]}, false)
		default:
			replacePersistenceObject(delta, content.VoxelObjectOverrideDef{PlacementID: input.Owner, ItemID: input.Item, SnapshotPath: tx.Paths[i]})
		}
	}
}
func releaseStreamedPersistence(state *StreamedLevelRuntimeState, tx *streamedPersistenceTransaction) {
	for _, e := range tx.Entities {
		finishStreamedImportedCapture(state, e.Capture)
	}
	if state.persistenceTransaction == tx {
		state.persistenceTransaction = nil
		state.persistenceBytes = 0
	}
	refreshStreamedPersistenceMetrics(state)
}
func failStreamedPersistence(state *StreamedLevelRuntimeState, tx *streamedPersistenceTransaction, err error) {
	state.Metrics.PersistenceFailureCount++
	state.Metrics.PersistenceLastError = err.Error()
	releaseStreamedPersistence(state, tx)
}

// Compare captured RAM publication fields, independently from live geometry.
// A durable A becomes the baseline when live B has no reference yet.
func acknowledgeStreamedPersistence(state *StreamedLevelRuntimeState, tx *streamedPersistenceTransaction) {
	base, current := tx.Baseline, state.WorldDelta
	for i, e := range tx.Entities {
		input := e.Input
		relatedKind, relatedOwner, relatedCoord := input.Kind, input.Owner, input.Coord
		if input.Removal != nil {
			relatedOwner, relatedCoord = input.Removal.OwnerID, input.Removal.ChunkCoord
			if input.Removal.OwnerKind == content.VoxelBackingOwnerImportedWorld {
				relatedKind = "imported"
			} else {
				relatedKind = "terrain"
			}
		}
		removalKind := content.VoxelBackingOwnerTerrain
		if relatedKind == "imported" {
			removalKind = content.VoxelBackingOwnerImportedWorld
		}
		findRemoval := func(delta *content.WorldDeltaDef) *content.VoxelBackingRemovalDef {
			for j := range delta.VoxelBackingRemovals {
				v := &delta.VoxelBackingRemovals[j]
				if v.OwnerKind == removalKind && v.OwnerID == relatedOwner && v.ChunkCoord == relatedCoord {
					return v
				}
			}
			return nil
		}
		publicationMatches := reflect.DeepEqual(findRemoval(base), findRemoval(current))
		if relatedKind == "terrain" || relatedKind == "imported" {
			value, present := state.voxelBackingRemovalMap[voxelBackingRemovalRuntimeKey(removalKind, relatedOwner, relatedCoord)]
			var published *content.VoxelBackingRemovalDef
			if present {
				published = &value
			}
			publicationMatches = publicationMatches && reflect.DeepEqual(findRemoval(base), published)
		}
		if relatedKind == "terrain" {
			find := func(delta *content.WorldDeltaDef) content.TerrainChunkOverrideDef {
				for _, v := range delta.TerrainChunkOverrides {
					if v.TerrainID == relatedOwner && v.ChunkCoord == relatedCoord {
						return v
					}
				}
				return content.TerrainChunkOverrideDef{}
			}
			publicationMatches = publicationMatches && find(base) == find(current) && find(base) == state.terrainOverrideMap[terrainChunkRuntimeKey(relatedOwner, relatedCoord)]
		}
		if relatedKind == "imported" {
			find := func(delta *content.WorldDeltaDef) content.ImportedWorldChunkOverrideDef {
				for _, v := range delta.ImportedWorldChunkOverrides {
					if v.WorldID == relatedOwner && v.ChunkCoord == relatedCoord {
						return v
					}
				}
				return content.ImportedWorldChunkOverrideDef{}
			}
			publicationMatches = publicationMatches && find(base) == find(current) && find(base) == state.importedWorldOverrideMap[importedWorldChunkRuntimeKey(relatedOwner, relatedCoord)]
		}

		if input.Removal != nil {
			def := *input.Removal
			find := func(delta *content.WorldDeltaDef) *content.VoxelBackingRemovalDef {
				for j := range delta.VoxelBackingRemovals {
					v := &delta.VoxelBackingRemovals[j]
					if v.OwnerKind == def.OwnerKind && v.OwnerID == def.OwnerID && v.ChunkCoord == def.ChunkCoord {
						return v
					}
				}
				return nil
			}
			if publicationMatches && reflect.DeepEqual(find(base), find(current)) {
				replacePersistenceRemoval(current, def)
				copy := def
				copy.Bricks = clonePersistenceSlice(def.Bricks)
				state.voxelBackingRemovalMap[voxelBackingRemovalRuntimeKey(def.OwnerKind, def.OwnerID, def.ChunkCoord)] = copy
			}
		}
		kind, owner, coord := input.Kind, input.Owner, input.Coord
		if input.Removal != nil {
			owner, coord = input.Removal.OwnerID, input.Removal.ChunkCoord
			if input.Removal.OwnerKind == content.VoxelBackingOwnerImportedWorld {
				kind = "imported"
			} else {
				kind = "terrain"
			}
		}
		switch kind {
		case "terrain":
			find := func(delta *content.WorldDeltaDef) content.TerrainChunkOverrideDef {
				for _, v := range delta.TerrainChunkOverrides {
					if v.TerrainID == owner && v.ChunkCoord == coord {
						return v
					}
				}
				return content.TerrainChunkOverrideDef{}
			}
			if publicationMatches && find(base) == find(current) {
				v := content.TerrainChunkOverrideDef{TerrainID: owner, ChunkCoord: coord, SnapshotPath: tx.Paths[i]}
				replacePersistenceTerrain(current, v, input.Removal != nil)
				key := terrainChunkRuntimeKey(owner, coord)
				if input.Removal != nil {
					delete(state.terrainOverrideMap, key)
				} else {
					state.terrainOverrideMap[key] = v
				}
			}
		case "imported":
			find := func(delta *content.WorldDeltaDef) content.ImportedWorldChunkOverrideDef {
				for _, v := range delta.ImportedWorldChunkOverrides {
					if v.WorldID == owner && v.ChunkCoord == coord {
						return v
					}
				}
				return content.ImportedWorldChunkOverrideDef{}
			}
			if publicationMatches && find(base) == find(current) {
				v := content.ImportedWorldChunkOverrideDef{WorldID: owner, ChunkCoord: coord, SnapshotPath: tx.Paths[i]}
				replacePersistenceImported(current, v, input.Removal != nil)
				key := importedWorldChunkRuntimeKey(owner, coord)
				if input.Removal != nil {
					delete(state.importedWorldOverrideMap, key)
				} else {
					state.importedWorldOverrideMap[key] = v
				}
			}
		default:
			find := func(delta *content.WorldDeltaDef) content.VoxelObjectOverrideDef {
				for _, v := range delta.VoxelObjectOverrides {
					if v.PlacementID == owner && v.ItemID == input.Item {
						return v
					}
				}
				return content.VoxelObjectOverrideDef{}
			}
			if publicationMatches && find(base) == find(current) {
				v := content.VoxelObjectOverrideDef{PlacementID: owner, ItemID: input.Item, SnapshotPath: tx.Paths[i]}
				replacePersistenceObject(current, v)
				state.voxelOverrideMap[voxelObjectRuntimeKey(owner, input.Item)] = v
			}
		}
		if e.Capture.Token != nil && currentStreamedImportedCapture(state, e.Capture) {
			finishStreamedImportedCapture(state, e.Capture)
			if snapshot := tx.Navigation[i]; snapshot != nil {
				// Transfer owned sparse data directly; navigation owns any removal lookup
				// tables built by its handoff helper. No geometry reconstruction at ACK.
				queueOwnedSavedStreamedImportedAnalysis(state, snapshot, input.Removal, input.ChunkSize, e.Capture.Token.Edit)
				tx.Navigation[i] = nil
			}
		}
	}
	state.InvalidateObserverSelection()
}

// Publication progresses even when an unrelated runtime error prevents selection.
func commitStreamedPersistence(cmd *Commands, state *StreamedLevelRuntimeState, allowRemoval bool) error {
	tx := state.persistenceTransaction
	if tx == nil {
		return nil
	}
	select {
	case result := <-tx.Results:
		if result.Err != nil {
			failStreamedPersistence(state, tx, result.Err)
			return result.Err
		}
		if !tx.Manifest {
			if !streamedPersistenceEntitiesCurrent(cmd, state, tx) {
				releaseStreamedPersistence(state, tx)
				return nil
			}
			tx.Paths = result.Paths
			tx.Navigation = result.Navigation
			// The worker has released its transient serializer structures. Count actual
			// retained flat arrays, identities, removal values and result strings now.
			actual := int64(unsafe.Sizeof(streamedPersistenceTransaction{}))
			safe := persistenceAdd(&actual, int64(cap(tx.Entities)), int64(unsafe.Sizeof(streamedPersistenceEntity{}))) && persistenceValueBytes(reflect.ValueOf(tx.Paths), &actual) && persistenceValueBytes(reflect.ValueOf(tx.Navigation), &actual)
			for _, e := range tx.Entities {
				safe = safe && persistenceValueBytes(reflect.ValueOf(e.Input), &actual) && persistenceAdd(&actual, int64(len(e.Capture.Key.WorldID)), 1)
				if e.Capture.Token != nil {
					safe = safe && persistenceAdd(&actual, 1, int64(unsafe.Sizeof(streamedImportedCaptureToken{})))
				}
			}
			if !safe {
				err := fmt.Errorf("unsafe persistence result size")
				failStreamedPersistence(state, tx, err)
				return err
			}
			tx.Bytes = actual
			n, err := persistenceManifestBytes(state.WorldDelta)
			if err != nil {
				failStreamedPersistence(state, tx, err)
				return err
			}
			total := tx.Bytes
			// Reserve both complete fresh baseline and candidate, including patch growth.
			if !persistenceAdd(&total, 2, n) {
				err = fmt.Errorf("unsafe persistence candidate size")
				failStreamedPersistence(state, tx, err)
				return err
			}

			// Exact-capacity candidate arrays reserve at most one new entry per entity.
			growth := int64(unsafe.Sizeof(content.TerrainChunkOverrideDef{})) + int64(unsafe.Sizeof(content.ImportedWorldChunkOverrideDef{})) + int64(unsafe.Sizeof(content.VoxelObjectOverrideDef{})) + int64(unsafe.Sizeof(content.VoxelBackingRemovalDef{}))
			if !persistenceAdd(&total, int64(len(tx.Entities)), growth) {
				err = fmt.Errorf("unsafe persistence candidate capacity")
				failStreamedPersistence(state, tx, err)
				return err
			}
			for i, e := range tx.Entities {
				var extra int64
				if !persistenceValueBytes(reflect.ValueOf(e.Input.Removal), &extra) || !persistenceAdd(&total, 1, extra) || !persistenceAdd(&total, int64(len(tx.Paths[i])), 1) || !persistenceAdd(&total, int64(len(e.Input.Owner)), 1) || !persistenceAdd(&total, int64(len(e.Input.Item)), 1) {
					err = fmt.Errorf("unsafe persistence candidate growth")
					failStreamedPersistence(state, tx, err)
					return err
				}
			}
			baseline := copyWorldDeltaForNav(state.WorldDelta)
			candidate := copyWorldDeltaForPersistence(state.WorldDelta, len(tx.Entities))
			tx.Baseline, tx.Candidate = &baseline, &candidate
			patchStreamedPersistenceCandidate(&candidate, tx)
			tx.Manifest = true
			tx.Bytes = total
			state.persistenceBytes = total
			refreshStreamedPersistenceMetrics(state)
			writer, path := streamedPersistenceWriter(state), state.WorldDeltaPath
			results, jobs := tx.Results, &state.jobs
			state.jobs.Add(1)
			go func() {
				defer jobs.Done()
				results <- streamedPersistenceResult{Err: writer(path, &candidate)}
			}()
			return nil
		}
		current := streamedPersistenceEntitiesCurrent(cmd, state, tx)
		if tx.Generation == state.Generation {
			acknowledgeStreamedPersistence(state, tx)
			if current {
				delete(state.persistenceIntents, tx.Coord)
			}
			if current && allowRemoval && persistenceNormalDemand(cmd, state, tx.Coord) {
				removeStreamedChunk(cmd, state, tx.Coord)
			}
		}
		releaseStreamedPersistence(state, tx)
	default:
	}
	return nil
}
func joinStreamedPersistence(cmd *Commands, state *StreamedLevelRuntimeState) error {
	for state.persistenceTransaction != nil {
		tx := state.persistenceTransaction
		result := <-tx.Results
		tx.Results <- result
		if err := commitStreamedPersistence(cmd, state, false); err != nil {
			return err
		}
	}
	return nil
}

func persistenceSliceWithGrowth[T any](source []T, growth int) []T {
	target := make([]T, len(source), len(source)+growth)
	copy(target, source)
	return target
}
func copyWorldDeltaForPersistence(source *content.WorldDeltaDef, growth int) content.WorldDeltaDef {
	delta := *source
	delta.PlacementTransformOverrides = clonePersistenceSlice(source.PlacementTransformOverrides)
	delta.PlacementDeletions = clonePersistenceSlice(source.PlacementDeletions)
	delta.TerrainChunkOverrides = persistenceSliceWithGrowth(source.TerrainChunkOverrides, growth)
	delta.ImportedWorldChunkOverrides = persistenceSliceWithGrowth(source.ImportedWorldChunkOverrides, growth)
	delta.VoxelObjectOverrides = persistenceSliceWithGrowth(source.VoxelObjectOverrides, growth)
	delta.VoxelBackingRemovals = persistenceSliceWithGrowth(source.VoxelBackingRemovals, growth)
	for i := range delta.VoxelBackingRemovals {
		delta.VoxelBackingRemovals[i].Bricks = clonePersistenceSlice(source.VoxelBackingRemovals[i].Bricks)
	}
	delta.NavigationSourceOverrides = clonePersistenceSlice(source.NavigationSourceOverrides)
	delta.NavigationGraphOverrides = clonePersistenceSlice(source.NavigationGraphOverrides)
	return delta
}
