package gekko

// Selection belongs to the main-thread streaming owner. Workers consume load
// jobs, never these counts or the published demand views.
const (
	streamedSelectionCurrent = iota
	streamedSelectionPrefetch
	streamedSelectionKeep
	streamedSelectionCollision
	streamedSelectionDestruction
	streamedSelectionVolumeCount
)

type streamedSelectionCounts map[ChunkCoord]int

func (counts streamedSelectionCounts) add(coord ChunkCoord, delta int) {
	if next := counts[coord] + delta; next > 0 {
		counts[coord] = next
	} else {
		delete(counts, coord)
	}
}

func (counts streamedSelectionCounts) set() map[ChunkCoord]struct{} {
	out := make(map[ChunkCoord]struct{}, len(counts))
	for coord := range counts {
		out[coord] = struct{}{}
	}
	return out
}

type streamedObserverSelectionKey struct {
	center     ChunkCoord
	radii      [streamedSelectionVolumeCount]int
	chunkSize  float32
	generation uint64
	revision   uint64
}

type streamedObserverSelection struct {
	key            streamedObserverSelectionKey
	seen           bool
	volumeSectors  [3]streamedSelectionCounts
	desiredSectors map[ChunkCoord]struct{}
	keepSectors    map[ChunkCoord]struct{}
}

type streamedSelectionSector struct {
	hasVisibility bool
	visible       map[ChunkCoord]struct{}
	fullChunks    map[ChunkCoord]struct{}
}

type streamedObserverSelectionOwner struct {
	generation uint64
	revision   uint64
	observers  map[EntityId]*streamedObserverSelection
	// Current demand only feeds each observer's sector policy. The other four
	// volumes also have global overlap counts for the raw chunk unions.
	raw            [streamedSelectionVolumeCount]streamedSelectionCounts
	desiredSectors streamedSelectionCounts
	keepSectors    streamedSelectionCounts
	sectors        map[ChunkCoord]streamedSelectionSector
	fallback       map[ChunkCoord]struct{}
	disableProxies bool
	// Base proxy demand is independent of the temporary working pins.
	desiredProxies map[ChunkCoord]struct{}
	keepProxies    map[ChunkCoord]struct{}
	// Only additions absent from base demand are recorded. Idle cleanup walks
	// these differences, never a copy of all published demand.
	temporaryCollision   map[ChunkCoord]struct{}
	temporaryDestruction map[ChunkCoord]struct{}
	temporaryProxy       map[ChunkCoord]struct{}
}

// InvalidateObserverSelection declares a main-thread selection metadata edit.
// Call after changing sector membership/PVS, placements, terrain occupancy or
// overrides, backing availability, or coordinates derived from future layers.
// Exported metadata maps do not track in-place edits automatically.
func (state *StreamedLevelRuntimeState) InvalidateObserverSelection() {
	if state != nil {
		state.observerSelectionRevision++
	}
}

func (state *StreamedLevelRuntimeState) releaseObserverSelection() {
	state.observerSelection = nil
	state.observerSelectionRevision = 0
	state.DesiredChunks, state.KeepChunks = nil, nil
	state.CollisionChunks, state.DestructionChunks = nil, nil
	state.DesiredSectors, state.KeepSectors = nil, nil
	state.DesiredProxySectors, state.KeepProxySectors = nil, nil
}

func newStreamedObserverSelectionOwner(state *StreamedLevelRuntimeState) *streamedObserverSelectionOwner {
	owner := &streamedObserverSelectionOwner{
		generation: state.Generation, revision: state.observerSelectionRevision,
		observers:      make(map[EntityId]*streamedObserverSelection),
		desiredSectors: make(streamedSelectionCounts), keepSectors: make(streamedSelectionCounts),
		sectors: make(map[ChunkCoord]streamedSelectionSector), fallback: make(map[ChunkCoord]struct{}),
		disableProxies:     state.Config.DisableSectorProxies,
		temporaryCollision: make(map[ChunkCoord]struct{}), temporaryDestruction: make(map[ChunkCoord]struct{}), temporaryProxy: make(map[ChunkCoord]struct{}),
	}
	for volume := streamedSelectionPrefetch; volume < streamedSelectionVolumeCount; volume++ {
		owner.raw[volume] = make(streamedSelectionCounts)
	}
	// Snapshot policy derivations once per declared metadata revision. Keep the
	// distinction between membership filtering and valid FullChunkRefs expansion.
	for coord, sector := range state.ImportedWorldSectors {
		derived := streamedSelectionSector{
			hasVisibility: len(sector.VisibleSectorRefs) > 0 || len(sector.AdjacentSectorRefs) > 0 || len(sector.SourceLeafIDs) > 0,
			visible:       streamedVisibleImportedSectorsForCurrentSector(state, coord),
			fullChunks:    make(map[ChunkCoord]struct{}),
		}
		for _, ref := range sector.FullChunkRefs {
			chunk := chunkCoordFromTerrain(ref)
			if entry, ok := state.ImportedWorldEntries[chunk]; ok && (entry.NonEmptyVoxelCount > 0 || state.BaseWorldBacking != nil) {
				derived.fullChunks[chunk] = struct{}{}
			}
		}
		owner.sectors[coord] = derived
		if len(sector.LODs) > 0 {
			owner.fallback[coord] = struct{}{}
		}
	}
	return owner
}

func streamedObserverSelectionInputs(state *StreamedLevelRuntimeState, transform *TransformComponent, observer *StreamedLevelObserverComponent) streamedObserverSelectionKey {
	load, keep, prefetch := observer.Radius, observer.KeepRadius, observer.PrefetchRadius
	if load <= 0 {
		load = state.StreamingRadius
	}
	if keep <= 0 {
		keep = state.StreamingKeepRadius
	}
	keep = max(keep, load)
	if prefetch <= 0 {
		prefetch = state.StreamingPrefetchRadius
	}
	prefetch = max(prefetch, load)
	collision := observer.CollisionRadius
	if collision <= 0 {
		collision = state.StreamingCollisionRadius
	}
	if collision <= 0 {
		collision = load
	}
	destruction := observer.DestructionRadius
	if destruction <= 0 {
		destruction = state.StreamingDestructionRadius
	}
	if destruction <= 0 {
		destruction = collision
	}
	return streamedObserverSelectionKey{
		center:    ChunkCoordFromPosition(transform.Position, state.ChunkSize),
		radii:     [streamedSelectionVolumeCount]int{max(0, load), max(0, prefetch), max(0, keep), max(0, collision), max(0, destruction)},
		chunkSize: state.ChunkSize, generation: state.Generation, revision: state.observerSelectionRevision,
	}
}

// Cubes and their intersections are inclusive. Subtracting one box from
// another yields at most six disjoint slabs: X faces, Y faces within the common
// X range, then Z faces within the common X/Y range. A teleport visits at most
// the two footprints, independent of distance traveled.
type streamedSelectionBox struct{ min, max ChunkCoord }

func streamedSelectionCube(center ChunkCoord, radius int) streamedSelectionBox {
	return streamedSelectionBox{
		min: ChunkCoord{center.X - radius, center.Y - radius, center.Z - radius},
		max: ChunkCoord{center.X + radius, center.Y + radius, center.Z + radius},
	}
}

func (box streamedSelectionBox) visit(visit func(ChunkCoord)) {
	for x := box.min.X; x <= box.max.X; x++ {
		for y := box.min.Y; y <= box.max.Y; y++ {
			for z := box.min.Z; z <= box.max.Z; z++ {
				visit(ChunkCoord{x, y, z})
			}
		}
	}
}

func (box streamedSelectionBox) difference(other streamedSelectionBox, visit func(ChunkCoord)) {
	intersection := streamedSelectionBox{
		min: ChunkCoord{max(box.min.X, other.min.X), max(box.min.Y, other.min.Y), max(box.min.Z, other.min.Z)},
		max: ChunkCoord{min(box.max.X, other.max.X), min(box.max.Y, other.max.Y), min(box.max.Z, other.max.Z)},
	}
	if intersection.min.X > intersection.max.X || intersection.min.Y > intersection.max.Y || intersection.min.Z > intersection.max.Z {
		box.visit(visit)
		return
	}
	streamedSelectionBox{box.min, ChunkCoord{intersection.min.X - 1, box.max.Y, box.max.Z}}.visit(visit)
	streamedSelectionBox{ChunkCoord{intersection.max.X + 1, box.min.Y, box.min.Z}, box.max}.visit(visit)
	streamedSelectionBox{ChunkCoord{intersection.min.X, box.min.Y, box.min.Z}, ChunkCoord{intersection.max.X, intersection.min.Y - 1, box.max.Z}}.visit(visit)
	streamedSelectionBox{ChunkCoord{intersection.min.X, intersection.max.Y + 1, box.min.Z}, ChunkCoord{intersection.max.X, box.max.Y, box.max.Z}}.visit(visit)
	streamedSelectionBox{ChunkCoord{intersection.min.X, intersection.min.Y, box.min.Z}, ChunkCoord{intersection.max.X, intersection.max.Y, intersection.min.Z - 1}}.visit(visit)
	streamedSelectionBox{ChunkCoord{intersection.min.X, intersection.min.Y, intersection.max.Z + 1}, ChunkCoord{intersection.max.X, intersection.max.Y, box.max.Z}}.visit(visit)
}

func (owner *streamedObserverSelectionOwner) updateVolumes(state *StreamedLevelRuntimeState, observer *streamedObserverSelection, next *streamedObserverSelectionKey, previous *streamedObserverSelectionKey) {
	for volume := 0; volume < streamedSelectionVolumeCount; volume++ {
		apply := func(delta int) func(ChunkCoord) {
			return func(coord ChunkCoord) {
				state.Metrics.ObserverSelectionChunkVisitCount++
				if volume != streamedSelectionCurrent {
					owner.raw[volume].add(coord, delta)
				}
				if volume <= streamedSelectionKeep {
					if sector, ok := state.ImportedChunkSector[coord]; ok {
						observer.volumeSectors[volume].add(sector, delta)
					}
				}
			}
		}
		if next == nil {
			streamedSelectionCube(previous.center, previous.radii[volume]).visit(apply(-1))
		} else if previous == nil {
			streamedSelectionCube(next.center, next.radii[volume]).visit(apply(1))
		} else {
			oldCube := streamedSelectionCube(previous.center, previous.radii[volume])
			newCube := streamedSelectionCube(next.center, next.radii[volume])
			oldCube.difference(newCube, apply(-1))
			newCube.difference(oldCube, apply(1))
		}
	}
}

func (owner *streamedObserverSelectionOwner) addObserverSectors(observer *streamedObserverSelection, delta int) {
	for coord := range observer.desiredSectors {
		owner.desiredSectors.add(coord, delta)
	}
	for coord := range observer.keepSectors {
		owner.keepSectors.add(coord, delta)
	}
}

func (owner *streamedObserverSelectionOwner) deriveObserverSectors(observer *streamedObserverSelection) {
	for volume, counts := range observer.volumeSectors {
		if len(counts) == 0 {
			observer.volumeSectors[volume] = make(streamedSelectionCounts)
		}
	}
	desired := make(map[ChunkCoord]struct{})
	keep := observer.volumeSectors[streamedSelectionKeep].set()
	hasVisibility := false
	for coord := range observer.volumeSectors[streamedSelectionCurrent] {
		if owner.sectors[coord].hasVisibility {
			hasVisibility = true
			break
		}
	}
	if hasVisibility {
		for coord := range observer.volumeSectors[streamedSelectionCurrent] {
			desired[coord] = struct{}{}
			mergeChunkCoordSet(desired, owner.sectors[coord].visible)
		}
		mergeChunkCoordSet(keep, desired)
	} else {
		desired = observer.volumeSectors[streamedSelectionPrefetch].set()
	}
	observer.desiredSectors, observer.keepSectors = desired, keep
}

func (owner *streamedObserverSelectionOwner) publish(state *StreamedLevelRuntimeState) {
	// Drop capacity when a count/history map becomes empty. Nonempty Go maps
	// can retain their peak allocation; this owner is not a byte budget.
	for volume := streamedSelectionPrefetch; volume < streamedSelectionVolumeCount; volume++ {
		if len(owner.raw[volume]) == 0 {
			owner.raw[volume] = make(streamedSelectionCounts)
		}
	}
	if len(owner.observers) == 0 {
		owner.observers = make(map[EntityId]*streamedObserverSelection)
	}
	if len(owner.desiredSectors) == 0 {
		owner.desiredSectors = make(streamedSelectionCounts)
	}
	if len(owner.keepSectors) == 0 {
		owner.keepSectors = make(streamedSelectionCounts)
	}
	desiredSectors, keepSectors := owner.desiredSectors.set(), owner.keepSectors.set()
	// Filter after the global unions: one observer can provide visibility for
	// another observer's raw coordinate even when it is absent from FullChunkRefs.
	desired := streamedFilterImportedChunksBySectors(state, owner.raw[streamedSelectionPrefetch].set(), desiredSectors)
	keep := streamedFilterImportedChunksBySectors(state, owner.raw[streamedSelectionKeep].set(), keepSectors)
	for coord := range desiredSectors {
		mergeChunkCoordSet(desired, owner.sectors[coord].fullChunks)
	}
	for coord := range keepSectors {
		mergeChunkCoordSet(keep, owner.sectors[coord].fullChunks)
	}
	owner.desiredProxies, owner.keepProxies = copyChunkCoordSet(desiredSectors), copyChunkCoordSet(keepSectors)
	if !owner.disableProxies {
		mergeChunkCoordSet(owner.desiredProxies, owner.fallback)
		mergeChunkCoordSet(owner.keepProxies, owner.fallback)
	}
	state.DesiredChunks, state.KeepChunks = desired, keep
	state.CollisionChunks, state.DestructionChunks = owner.raw[streamedSelectionCollision].set(), owner.raw[streamedSelectionDestruction].set()
	state.DesiredSectors, state.KeepSectors = desiredSectors, keepSectors
	state.DesiredProxySectors, state.KeepProxySectors = copyChunkCoordSet(owner.desiredProxies), copyChunkCoordSet(owner.keepProxies)
}

func (owner *streamedObserverSelectionOwner) clearTemporaryDemand(state *StreamedLevelRuntimeState) {
	for coord := range owner.temporaryCollision {
		delete(state.CollisionChunks, coord)
	}
	for coord := range owner.temporaryDestruction {
		delete(state.DestructionChunks, coord)
	}
	for coord := range owner.temporaryProxy {
		if _, base := owner.desiredProxies[coord]; !base {
			delete(state.DesiredProxySectors, coord)
		}
		if _, base := owner.keepProxies[coord]; !base {
			delete(state.KeepProxySectors, coord)
		}
	}
	if len(owner.temporaryCollision) > 0 {
		owner.temporaryCollision = make(map[ChunkCoord]struct{})
	}
	if len(owner.temporaryDestruction) > 0 {
		owner.temporaryDestruction = make(map[ChunkCoord]struct{})
	}
	if len(owner.temporaryProxy) > 0 {
		owner.temporaryProxy = make(map[ChunkCoord]struct{})
	}
}

func updateStreamedObserverSelection(cmd *Commands, state *StreamedLevelRuntimeState) {
	owner := state.observerSelection
	dirty := owner == nil || owner.generation != state.Generation || owner.revision != state.observerSelectionRevision
	if dirty {
		owner = newStreamedObserverSelectionOwner(state)
		state.observerSelection = owner
	} else {
		owner.clearTemporaryDemand(state)
	}
	if owner.disableProxies != state.Config.DisableSectorProxies {
		owner.disableProxies = state.Config.DisableSectorProxies
		dirty = true
	}
	for _, observer := range owner.observers {
		observer.seen = false
	}
	MakeQuery2[TransformComponent, StreamedLevelObserverComponent](cmd).Map(func(id EntityId, transform *TransformComponent, component *StreamedLevelObserverComponent) bool {
		if transform == nil || component == nil {
			return true
		}
		key := streamedObserverSelectionInputs(state, transform, component)
		observer := owner.observers[id]
		if observer != nil && observer.key == key {
			observer.seen = true
			return true
		}
		state.Metrics.ObserverSelectionBuildCount++
		dirty = true
		var previous *streamedObserverSelectionKey
		if observer == nil {
			observer = &streamedObserverSelection{}
			for volume := range observer.volumeSectors {
				observer.volumeSectors[volume] = make(streamedSelectionCounts)
			}
			owner.observers[id] = observer
		} else {
			previous = &observer.key
			owner.addObserverSectors(observer, -1)
		}
		owner.updateVolumes(state, observer, &key, previous)
		owner.deriveObserverSectors(observer)
		owner.addObserverSectors(observer, 1)
		observer.key, observer.seen = key, true
		return true
	})
	for id, observer := range owner.observers {
		if observer.seen {
			continue
		}
		state.Metrics.ObserverSelectionBuildCount++
		owner.updateVolumes(state, observer, nil, &observer.key)
		owner.addObserverSectors(observer, -1)
		delete(owner.observers, id)
		dirty = true
	}
	if dirty {
		owner.publish(state)
	}
}

func addStreamedTemporaryGameplayDemand(state *StreamedLevelRuntimeState, coord ChunkCoord) {
	if state.CollisionChunks == nil {
		state.CollisionChunks = make(map[ChunkCoord]struct{})
	}
	if state.DestructionChunks == nil {
		state.DestructionChunks = make(map[ChunkCoord]struct{})
	}
	if owner := state.observerSelection; owner != nil {
		if _, present := state.CollisionChunks[coord]; !present {
			owner.temporaryCollision[coord] = struct{}{}
		}
		if _, present := state.DestructionChunks[coord]; !present {
			owner.temporaryDestruction[coord] = struct{}{}
		}
	}
	state.CollisionChunks[coord], state.DestructionChunks[coord] = struct{}{}, struct{}{}
}

func addStreamedTemporaryProxyDemand(state *StreamedLevelRuntimeState, coord ChunkCoord) {
	if owner := state.observerSelection; owner != nil {
		_, desired := owner.desiredProxies[coord]
		_, keep := owner.keepProxies[coord]
		if !desired || !keep {
			owner.temporaryProxy[coord] = struct{}{}
		}
	}
	state.DesiredProxySectors[coord], state.KeepProxySectors[coord] = struct{}{}, struct{}{}
}

func streamedVisibleImportedSectorsForCurrentSector(state *StreamedLevelRuntimeState, sectorCoord ChunkCoord) map[ChunkCoord]struct{} {
	out := map[ChunkCoord]struct{}{sectorCoord: {}}
	if state == nil {
		return out
	}
	sector, ok := state.ImportedWorldSectors[sectorCoord]
	if !ok {
		return out
	}
	for _, ref := range sector.VisibleSectorRefs {
		out[chunkCoordFromTerrain(ref)] = struct{}{}
	}
	for _, ref := range sector.AdjacentSectorRefs {
		out[chunkCoordFromTerrain(ref)] = struct{}{}
	}
	return out
}

func streamedFilterImportedChunksBySectors(state *StreamedLevelRuntimeState, chunks map[ChunkCoord]struct{}, sectors map[ChunkCoord]struct{}) map[ChunkCoord]struct{} {
	if state == nil || len(chunks) == 0 || len(state.ImportedChunkSector) == 0 {
		return chunks
	}
	out := make(map[ChunkCoord]struct{}, len(chunks))
	for coord := range chunks {
		sectorCoord, isImportedWorldChunk := state.ImportedChunkSector[coord]
		if !isImportedWorldChunk {
			out[coord] = struct{}{}
			continue
		}
		if _, ok := sectors[sectorCoord]; ok {
			out[coord] = struct{}{}
			continue
		}
		if streamedChunkHasNonImportedLoadableContent(state, coord) {
			out[coord] = struct{}{}
		}
	}
	return out
}

func streamedChunkHasNonImportedLoadableContent(state *StreamedLevelRuntimeState, coord ChunkCoord) bool {
	if state == nil {
		return false
	}
	if entry, ok := state.TerrainEntries[coord]; ok && entry.NonEmptyVoxelCount > 0 {
		return true
	}
	if state.TerrainID != "" {
		if _, ok := state.terrainOverrideMap[terrainChunkRuntimeKey(state.TerrainID, terrainCoordFromChunk(coord))]; ok {
			return true
		}
	}
	if len(state.PlacementsByChunk[coord]) > 0 {
		return true
	}
	return false
}

func mergeChunkCoordSet(dst, src map[ChunkCoord]struct{}) {
	for coord := range src {
		dst[coord] = struct{}{}
	}
}

func copyChunkCoordSet(src map[ChunkCoord]struct{}) map[ChunkCoord]struct{} {
	out := make(map[ChunkCoord]struct{}, len(src))
	for coord := range src {
		out[coord] = struct{}{}
	}
	return out
}
