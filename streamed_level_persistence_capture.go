package gekko

import (
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"unsafe"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// These are flat, owned worker inputs. Live map identities stay in the coordinator.
type streamedPersistenceBrick struct {
	Coord   [3]int
	Payload [volume.BrickSize][volume.BrickSize][volume.BrickSize]uint8
}
type streamedPersistenceInput struct {
	Navigation        bool
	Kind, Owner, Item string
	Coord             content.TerrainChunkCoordDef
	ChunkSize         int
	Resolution        float32
	Bricks            []streamedPersistenceBrick
	Min, Max          mgl32.Vec3
	Removal           *content.VoxelBackingRemovalDef
}
type streamedPersistenceEntity struct {
	Entity   EntityId
	Map      *volume.XBrickMap
	Exists   bool
	Geometry AssetId
	Input    streamedPersistenceInput
	Capture  streamedImportedCapture
}
type streamedPersistenceClass struct {
	Backing           bool
	Kind, Owner, Item string
	Coord             content.TerrainChunkCoordDef
}

type streamedPersistenceIntent struct {
	Loaded   *streamedLoadedChunk
	Entities map[EntityId]streamedPersistenceClass
}

func persistenceAdd(total *int64, count, size int64) bool {
	if count < 0 || size < 0 || count != 0 && size > (math.MaxInt64-*total)/count {
		return false
	}
	*total += count * size
	return true
}

// Count owned slice backings and strings before a clone. This walks metadata only.
func persistenceValueBytes(v reflect.Value, total *int64) bool {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return true
		}
		return persistenceAdd(total, 1, int64(v.Elem().Type().Size())) && persistenceValueBytes(v.Elem(), total)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !persistenceValueBytes(v.Field(i), total) {
				return false
			}
		}
	case reflect.Slice:
		if !persistenceAdd(total, int64(v.Cap()), int64(v.Type().Elem().Size())) {
			return false
		}
		for i := 0; i < v.Len(); i++ {
			if !persistenceValueBytes(v.Index(i), total) {
				return false
			}
		}
	case reflect.String:
		return persistenceAdd(total, int64(v.Len()), 1)
	}
	return true
}
func persistenceManifestBytes(delta *content.WorldDeltaDef) (int64, error) {
	var n int64
	if !persistenceValueBytes(reflect.ValueOf(delta), &n) {
		return 0, fmt.Errorf("unsafe persistence manifest size")
	}
	return n, nil
}
func persistenceMapBrickCount(xbm *volume.XBrickMap) int {
	n := 0
	if xbm != nil {
		for _, s := range xbm.Sectors {
			for i := 0; i < 64; i++ {
				if s.BrickMask64&(uint64(1)<<i) != 0 && s.GetBrick(i%4, i/4%4, i/16) != nil {
					n++
				}
			}
		}
	}
	return n
}
func persistenceCaptureBricks(xbm *volume.XBrickMap, count int) []streamedPersistenceBrick {
	bricks := make([]streamedPersistenceBrick, count)
	j := 0
	if xbm != nil {
		for key, s := range xbm.Sectors {
			for i := 0; i < 64; i++ {
				if s.BrickMask64&(uint64(1)<<i) == 0 {
					continue
				}
				b := s.GetBrick(i%4, i/4%4, i/16)
				if b == nil {
					continue
				}
				bricks[j] = streamedPersistenceBrick{Coord: [3]int{key[0]*volume.SectorSize + i%4*volume.BrickSize, key[1]*volume.SectorSize + i/4%4*volume.BrickSize, key[2]*volume.SectorSize + i/16*volume.BrickSize}, Payload: b.Payload}
				j++
			}
		}
	}
	return bricks
}
func persistenceBricksMatch(xbm *volume.XBrickMap, bricks []streamedPersistenceBrick) bool {
	if persistenceMapBrickCount(xbm) != len(bricks) {
		return false
	}
	for _, b := range bricks {
		// Sector coordinates use floor division, including negative authored geometry.
		key := [3]int{floorDiv(b.Coord[0], volume.SectorSize), floorDiv(b.Coord[1], volume.SectorSize), floorDiv(b.Coord[2], volume.SectorSize)}
		s := xbm.Sectors[key]
		if s == nil {
			return false
		}
		live := s.GetBrick((b.Coord[0]-key[0]*volume.SectorSize)/volume.BrickSize, (b.Coord[1]-key[1]*volume.SectorSize)/volume.BrickSize, (b.Coord[2]-key[2]*volume.SectorSize)/volume.BrickSize)
		if live == nil || live.Payload != b.Payload {
			return false
		}
	}
	return true
}
func persistenceInputMap(input streamedPersistenceInput) *volume.XBrickMap {
	xbm := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		for _, b := range input.Bricks {
			for x := 0; x < volume.BrickSize; x++ {
				for y := 0; y < volume.BrickSize; y++ {
					for z := 0; z < volume.BrickSize; z++ {
						if value := b.Payload[x][y][z]; value != 0 {
							if !yield(volume.VoxelWrite{X: b.Coord[0] + x, Y: b.Coord[1] + y, Z: b.Coord[2] + z, Value: value}) {
								return
							}
						}
					}
				}
			}
		}
	})
	// Terrain serialization uses the AABB observed at capture, including its cache.
	xbm.CachedMin, xbm.CachedMax, xbm.AABBDirty = input.Min, input.Max, false
	return xbm
}

func collectStreamedPersistenceIntent(cmd *Commands, state *StreamedLevelRuntimeState, coord ChunkCoord, loaded *streamedLoadedChunk) *streamedPersistenceIntent {
	if state.persistenceIntents == nil {
		state.persistenceIntents = make(map[ChunkCoord]*streamedPersistenceIntent)
	}
	intent := state.persistenceIntents[coord]
	if intent != nil && intent.Loaded != loaded {
		delete(state.persistenceIntents, coord)
		intent = nil
	}

	// Terrain/imported persistence follows current authored ownership, as the
	// blocking helper does. Disappeared owners cannot keep an orphan retry alive.
	// An owned object key still persists an empty snapshot when its entity is gone.
	if intent != nil {
		for eid, class := range intent.Entities {
			owned := false
			switch class.Kind {
			case "terrain":
				_, member := loaded.TerrainEntities[eid]
				_, referenced := AuthoredTerrainChunkRefForEntity(cmd, eid)
				owned = member && referenced && cmd.EntityExists(eid)
			case "imported":
				_, member := loaded.ImportedWorldEntities[eid]
				_, referenced := AuthoredImportedWorldChunkRefForEntity(cmd, eid)
				owned = member && referenced && cmd.EntityExists(eid)
			case "object":
				current, member := loaded.ObjectEntities[voxelObjectRuntimeKey(class.Owner, class.Item)]
				owned = member && current == eid
			}
			if !owned {
				delete(intent.Entities, eid)
			}
		}
	}
	remember := func(eid EntityId, class streamedPersistenceClass, missing bool) {
		_, dirty, exists := currentVoxelMapForEntity(cmd, eid)
		if backing, ok := voxelBackingForEntity(cmd, eid); ok && backing.Dirty {
			dirty = true
			class.Backing = true
		}
		remembered := false
		if intent != nil {
			_, remembered = intent.Entities[eid]
		}
		if !dirty && !(missing && !exists) && !remembered {
			return
		}
		if intent == nil {
			intent = &streamedPersistenceIntent{Loaded: loaded, Entities: make(map[EntityId]streamedPersistenceClass)}
			state.persistenceIntents[coord] = intent
		}
		if previous, remembered := intent.Entities[eid]; remembered {
			class.Backing = previous.Backing || class.Backing
			intent.Entities[eid] = class
		} else {
			intent.Entities[eid] = class
		}
	}
	for eid := range loaded.TerrainEntities {
		if ref, ok := AuthoredTerrainChunkRefForEntity(cmd, eid); ok {
			remember(eid, streamedPersistenceClass{Kind: "terrain", Owner: ref.TerrainID, Coord: terrainCoordFromArray(ref.ChunkCoord)}, false)
		}
	}
	for eid := range loaded.ImportedWorldEntities {
		if ref, ok := AuthoredImportedWorldChunkRefForEntity(cmd, eid); ok {
			remember(eid, streamedPersistenceClass{Kind: "imported", Owner: ref.WorldID, Coord: terrainCoordFromArray(ref.ChunkCoord)}, false)
		}
	}
	for key, eid := range loaded.ObjectEntities {
		owner, item := splitVoxelObjectRuntimeKey(key)
		remember(eid, streamedPersistenceClass{Kind: "object", Owner: owner, Item: item}, true)
	}

	if intent != nil && len(intent.Entities) == 0 {
		delete(state.persistenceIntents, coord)
		intent = nil
	}
	return intent
}

func preflightStreamedPersistence(cmd *Commands, state *StreamedLevelRuntimeState, intent *streamedPersistenceIntent) (int64, error) {
	n := int64(unsafe.Sizeof(streamedPersistenceTransaction{}))
	if !persistenceAdd(&n, int64(len(intent.Entities)), int64(unsafe.Sizeof(streamedPersistenceEntity{}))+int64(unsafe.Sizeof(streamedPersistenceInput{}))+int64(unsafe.Sizeof(streamedImportedCaptureToken{}))) {
		return 0, fmt.Errorf("unsafe persistence capture size")
	}
	for eid, class := range intent.Entities {
		xbm, _, _ := currentVoxelMapForEntity(cmd, eid)
		// Bound codec allocation counts as well as retained brick capture counts.
		var codecBytes int64
		if !persistenceAdd(&codecBytes, int64(persistenceMapBrickCount(xbm)), volume.BrickSize*volume.BrickSize*volume.BrickSize*int64(unsafe.Sizeof(content.ImportedWorldVoxelDef{}))) {
			return 0, fmt.Errorf("unsafe persistence codec size")
		}
		if class.Kind == "terrain" {
			model, _ := voxelModelComponentForEntity(cmd, eid)
			size := model.TerrainChunkSize
			if size <= 0 {
				size = state.Level.ChunkSize
			}
			var columns int64
			if size < 0 || !persistenceAdd(&columns, int64(size), int64(size)) || !persistenceAdd(&codecBytes, columns, int64(unsafe.Sizeof(content.TerrainChunkColumnDef{}))) {
				return 0, fmt.Errorf("unsafe persistence terrain size")
			}
		}

		if !persistenceAdd(&n, int64(persistenceMapBrickCount(xbm)), int64(unsafe.Sizeof(streamedPersistenceBrick{}))) {
			return 0, fmt.Errorf("unsafe persistence brick size")
		}
		if backing, ok := voxelBackingForEntity(cmd, eid); ok && (backing.Dirty || class.Backing) {
			if !persistenceAdd(&n, int64(len(backing.Removals)), int64(unsafe.Sizeof(content.VoxelBackingRemovalBrickDef{}))) {
				return 0, fmt.Errorf("unsafe persistence removal size")
			}
		}

		worldID := class.Owner
		if backing, ok := voxelBackingForEntity(cmd, eid); ok && (backing.Dirty || class.Backing) && backing.OwnerKind == content.VoxelBackingOwnerImportedWorld {
			worldID = backing.OwnerID
		}
		if (class.Kind == "imported" || class.Backing) && streamedPersistenceNeedsNavigation(state, worldID) {
			voxels := int64(0)
			if xbm != nil {
				for _, sector := range xbm.Sectors {
					for i := 0; i < 64; i++ {
						if sector.BrickMask64&(uint64(1)<<i) == 0 {
							continue
						}
						brick := sector.GetBrick(i%4, i/4%4, i/16)
						if brick == nil {
							continue
						}
						for x := 0; x < volume.BrickSize; x++ {
							for y := 0; y < volume.BrickSize; y++ {
								for z := 0; z < volume.BrickSize; z++ {
									if brick.VoxelValue(x, y, z) != 0 {
										voxels++
									}
								}
							}
						}
					}
				}
			}
			if !persistenceAdd(&n, voxels, int64(unsafe.Sizeof(content.ImportedWorldVoxelDef{}))) || !persistenceAdd(&n, 1, int64(unsafe.Sizeof(content.ImportedWorldChunkDef{}))) || !persistenceAdd(&n, int64(len(worldID)), 1) {
				return 0, fmt.Errorf("unsafe persistence navigation result size")
			}
		}
		if !persistenceAdd(&n, 1, int64(unsafe.Sizeof((*content.ImportedWorldChunkDef)(nil)))) {
			return 0, fmt.Errorf("unsafe persistence navigation result table")
		}
		// Immutable names are retained, and unique result paths reserve a finite bound.
		// CreateTemp's random suffix fits 32 bytes; relative authoring may add delta directory components.
		for _, text := range []string{class.Kind, class.Owner, class.Item, state.WorldDataDir, state.WorldDeltaPath} {
			if !persistenceAdd(&n, int64(len(text)), 1) {
				return 0, fmt.Errorf("unsafe persistence metadata size")
			}
		}
		name := streamedPersistencePayloadName(streamedPersistenceInput{Kind: class.Kind, Owner: class.Owner, Item: class.Item, Coord: class.Coord})
		if !persistenceAdd(&n, int64(len(name)), 1) || !persistenceAdd(&n, int64(len(state.WorldDataDir)), 1) || !persistenceAdd(&n, int64(len(filepath.Dir(state.WorldDeltaPath))), 1) || !persistenceAdd(&n, 1, 32+int64(unsafe.Sizeof(""))) {
			return 0, fmt.Errorf("unsafe persistence result path size")
		}
		if backing, ok := voxelBackingForEntity(cmd, eid); ok && (backing.Dirty || class.Backing) {
			if !persistenceAdd(&n, 1, int64(unsafe.Sizeof(content.VoxelBackingRemovalDef{}))) {
				return 0, fmt.Errorf("unsafe persistence removal metadata size")
			}
			for _, text := range []string{backing.OwnerKind, backing.OwnerID, backing.SourceHash} {
				if !persistenceAdd(&n, int64(len(text)), 1) {
					return 0, fmt.Errorf("unsafe persistence removal metadata size")
				}
			}
		}

	}
	return n, nil
}

func captureStreamedPersistence(cmd *Commands, state *StreamedLevelRuntimeState, intent *streamedPersistenceIntent) []streamedPersistenceEntity {
	entities := make([]streamedPersistenceEntity, 0, len(intent.Entities))
	capture := func(eid EntityId, kind, owner, item string, coord content.TerrainChunkCoordDef) {
		if _, dirty := intent.Entities[eid]; !dirty {
			return
		}
		xbm, _, exists := currentVoxelMapForEntity(cmd, eid)
		model, _ := voxelModelComponentForEntity(cmd, eid)
		size := model.TerrainChunkSize
		if size <= 0 {
			size = state.Level.ChunkSize
		}
		input := streamedPersistenceInput{Kind: kind, Owner: owner, Item: item, Coord: coord, ChunkSize: size, Resolution: voxelResolutionForEntity(cmd, eid)}
		if xbm != nil {
			input.Min, input.Max = xbm.ComputeAABB()
		}
		input.Bricks = persistenceCaptureBricks(xbm, persistenceMapBrickCount(xbm))
		if backing, ok := voxelBackingForEntity(cmd, eid); ok && (backing.Dirty || intent.Entities[eid].Backing) {
			def := capturePersistenceRemoval(backing)
			input.Removal = &def
		}

		navWorld := owner
		if input.Removal != nil && input.Removal.OwnerKind == content.VoxelBackingOwnerImportedWorld {
			navWorld = input.Removal.OwnerID
		}
		input.Navigation = (kind == "imported" || input.Removal != nil && input.Removal.OwnerKind == content.VoxelBackingOwnerImportedWorld) && streamedPersistenceNeedsNavigation(state, navWorld)
		entity := streamedPersistenceEntity{Entity: eid, Map: xbm, Exists: exists, Geometry: model.GeometryAsset(), Input: input}
		worldID := owner
		if input.Removal != nil && input.Removal.OwnerKind == content.VoxelBackingOwnerImportedWorld {
			worldID = input.Removal.OwnerID
		}
		if kind == "imported" || input.Removal != nil && input.Removal.OwnerKind == content.VoxelBackingOwnerImportedWorld {
			key := voxelWorldDirtyChunkKey{WorldID: worldID, Coord: coord}
			edit := runtimeVoxelEdit{}
			if previous := state.importedEditCaptures[key]; previous != nil {
				edit = mergeStreamedImportedCaptureEdits(previous.Edit, edit)
			}
			token := &streamedImportedCaptureToken{Edit: edit}
			if state.importedEditCaptures == nil {
				state.importedEditCaptures = make(map[voxelWorldDirtyChunkKey]*streamedImportedCaptureToken)
			}
			state.importedEditCaptures[key] = token
			entity.Capture = streamedImportedCapture{Key: key, Token: token}
		}
		entities = append(entities, entity)
	}
	for eid, class := range intent.Entities {
		capture(eid, class.Kind, class.Owner, class.Item, class.Coord)
	}
	return entities
}
func streamedPersistenceEntitiesCurrent(cmd *Commands, state *StreamedLevelRuntimeState, tx *streamedPersistenceTransaction) bool {
	if tx.Generation != state.Generation || streamedLoadedOrActiveChunk(state, tx.Coord) != tx.Loaded {
		return false
	}

	// Capture current dirty siblings before any checkpoint can clear the intent.
	// Remembered legacy edits still matter after renderer upload clears its flags.
	intent := collectStreamedPersistenceIntent(cmd, state, tx.Coord, tx.Loaded)
	if intent != nil {
		for eid, class := range intent.Entities {
			covered := false
			for _, e := range tx.Entities {
				if e.Entity == eid && e.Input.Kind == class.Kind && e.Input.Owner == class.Owner && e.Input.Item == class.Item && e.Input.Coord == class.Coord {
					covered = true
					break
				}
			}
			if !covered {
				return false
			}
		}
	}

	for _, e := range tx.Entities {
		xbm, _, exists := currentVoxelMapForEntity(cmd, e.Entity)
		model, _ := voxelModelComponentForEntity(cmd, e.Entity)
		if exists != e.Exists || xbm != e.Map || model.GeometryAsset() != e.Geometry || !persistenceBricksMatch(xbm, e.Input.Bricks) {
			return false
		}

		if e.Input.Kind != "object" {
			chunkSize := model.TerrainChunkSize
			if chunkSize <= 0 {
				chunkSize = state.Level.ChunkSize
			}
			if chunkSize != e.Input.ChunkSize || voxelResolutionForEntity(cmd, e.Entity) != e.Input.Resolution {
				return false
			}
		}
		switch e.Input.Kind {
		case "terrain":
			if xbm != nil {
				min, max := xbm.ComputeAABB()
				if min != e.Input.Min || max != e.Input.Max {
					return false
				}
			}
			if _, owned := tx.Loaded.TerrainEntities[e.Entity]; !owned {
				return false
			}
			ref, ok := AuthoredTerrainChunkRefForEntity(cmd, e.Entity)
			if !ok || ref.TerrainID != e.Input.Owner || terrainCoordFromArray(ref.ChunkCoord) != e.Input.Coord {
				return false
			}
		case "imported":
			if _, owned := tx.Loaded.ImportedWorldEntities[e.Entity]; !owned {
				return false
			}
			ref, ok := AuthoredImportedWorldChunkRefForEntity(cmd, e.Entity)
			if !ok || ref.WorldID != e.Input.Owner || terrainCoordFromArray(ref.ChunkCoord) != e.Input.Coord {
				return false
			}
		case "object":
			if tx.Loaded.ObjectEntities[voxelObjectRuntimeKey(e.Input.Owner, e.Input.Item)] != e.Entity {
				return false
			}
		}
		backing, ok := voxelBackingForEntity(cmd, e.Entity)
		if e.Input.Removal != nil {
			if !ok || !reflect.DeepEqual(backing.RemovalDef(), *e.Input.Removal) {
				return false
			}
		} else if ok && backing.Dirty {
			return false
		}
		if e.Capture.Token != nil && !currentStreamedImportedCapture(state, e.Capture) {
			return false
		}
	}
	return true
}

func streamedPersistencePayloadName(input streamedPersistenceInput) string {
	switch input.Kind {
	case "terrain", "imported":
		return fmt.Sprintf("%s_%s_%d_%d_%d.gkchunk", input.Kind, sanitizePathSegment(input.Owner), input.Coord.X, input.Coord.Y, input.Coord.Z)
	default:
		return fmt.Sprintf("object_%s_%s.gkvoxobj", sanitizePathSegment(input.Owner), sanitizePathSegment(input.Item))
	}
}
func capturePersistenceRemoval(backing *VoxelBackingComponent) content.VoxelBackingRemovalDef {
	n := 0
	for _, bits := range backing.Removals {
		if !voxelRemovalBitsEmpty(bits) {
			n++
		}
	}
	def := content.VoxelBackingRemovalDef{OwnerKind: backing.OwnerKind, OwnerID: backing.OwnerID, SourceHash: backing.SourceHash, ChunkCoord: terrainCoordFromArray(backing.ChunkCoord), Bricks: make([]content.VoxelBackingRemovalBrickDef, n)}
	i := 0
	for coord, bits := range backing.Removals {
		if voxelRemovalBitsEmpty(bits) {
			continue
		}
		def.Bricks[i] = content.VoxelBackingRemovalBrickDef{Coord: coord, Bits: bits, Material: backing.Materials[coord]}
		i++
	}
	sort.Slice(def.Bricks, func(i, j int) bool {
		return def.Bricks[i].Coord[0] < def.Bricks[j].Coord[0] || def.Bricks[i].Coord[0] == def.Bricks[j].Coord[0] && (def.Bricks[i].Coord[1] < def.Bricks[j].Coord[1] || def.Bricks[i].Coord[1] == def.Bricks[j].Coord[1] && def.Bricks[i].Coord[2] < def.Bricks[j].Coord[2])
	})
	return def
}
func clonePersistenceSlice[T any](source []T) []T {
	if source == nil {
		return nil
	}
	target := make([]T, len(source))
	copy(target, source)
	return target
}

func streamedPersistenceNeedsNavigation(state *StreamedLevelRuntimeState, worldID string) bool {
	if state.BaseNavManifest == nil {
		return false
	}
	source := state.BaseNavManifest.SourceWorldID
	if source == "" {
		source = state.BaseWorldID
	}
	return source == worldID
}
