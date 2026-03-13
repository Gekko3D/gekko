package gekko

import (
	"fmt"
	"reflect"
	"sync"

	rooteecs "github.com/gekko3d/gekko/ecs"
)

type EntityId = rooteecs.EntityID
type archetypeId uint64
type archetypeKey []componentId
type componentId uint32
type typedStorage any
type row int
type set[T comparable] = map[T]struct{}

type ecsStorage struct {
	archetypes  map[archetypeId]*archetype
	entityIndex map[EntityId]archetypeId
}

type Ecs struct {
	storage *ecsStorage

	// Compatibility mirrors (same underlying maps as storage).
	archetypes  map[archetypeId]*archetype
	entityIndex map[EntityId]archetypeId

	idGeneratorLock sync.Mutex
	entityIdCounter EntityId

	componentIdCounterLock sync.Mutex
	componentIdCounter     componentId
	componentTypeIdMap     map[reflect.Type]componentId
	componentIdTypeMap     map[componentId]reflect.Type
}

func MakeEcs() Ecs {
	storage := &ecsStorage{
		archetypes:  make(map[archetypeId]*archetype),
		entityIndex: make(map[EntityId]archetypeId),
	}

	return Ecs{
		storage: storage,

		// Compatibility mirrors
		archetypes:  storage.archetypes,
		entityIndex: storage.entityIndex,
		//idGeneratorLock: make(sync.Mutex),
		entityIdCounter: EntityId(0),
		//componentIdCounterLock: make(sync.Mutex),
		componentIdCounter: componentId(0),
		componentTypeIdMap: make(map[reflect.Type]componentId),
		componentIdTypeMap: make(map[componentId]reflect.Type),
	}
}

type archetype struct {
	id            archetypeId
	key           archetypeKey
	entities      map[EntityId]row
	componentData map[componentId]any // typed slices via reflection
	recycled      []row
}

func (ecs *Ecs) addEntity(components ...any) EntityId {
	entityId := ecs.nextEntityId()
	return ecs.insertEntity(entityId, components...)
}

func (ecs *Ecs) insertEntity(entityId EntityId, components ...any) EntityId {
	archId, _, arch := ecs.archetypeFromComponents(components...)

	row := ecs.archetypeReserveRow(arch)
	arch.entities[entityId] = row
	for _, component := range components {
		ecs.writeComponent(arch, row, component)
	}

	ecs.storage.entityIndex[entityId] = archId

	return entityId
}

func (ecs *Ecs) removeEntity(entityId EntityId) {
	ecs.recycleEntity(entityId)
}

func (ecs *Ecs) addComponents(entityId EntityId, components ...any) {
	srcArchId := ecs.storage.entityIndex[entityId]
	srcArch := ecs.storage.archetypes[srcArchId]
	srcRow := srcArch.entities[entityId]

	dstArchId, _, dstArch := ecs.archetypeFromExtraComponents(srcArch, components...)
	dstRow := ecs.archetypeReserveRow(dstArch)

	ecs.moveComponents(srcArch, srcRow, dstArch, dstRow)
	for _, component := range components {
		ecs.writeComponent(dstArch, dstRow, component)
	}

	ecs.recycleEntity(entityId)

	dstArch.entities[entityId] = dstRow
	ecs.storage.entityIndex[entityId] = dstArchId
}

func (ecs *Ecs) removeComponents(entityId EntityId, components ...any) {
	srcArchId := ecs.storage.entityIndex[entityId]
	srcArch := ecs.storage.archetypes[srcArchId]
	srcRow := srcArch.entities[entityId]

	// Find the subset of components to keep
	removeSet := make(set[componentId])
	for _, c := range components {
		cType := reflect.TypeOf(c)
		if cType.Kind() == reflect.Pointer {
			cType = cType.Elem()
		}
		removeSet[ecs.getComponentId(cType)] = struct{}{}
	}

	var dstKey archetypeKey
	for _, compId := range srcArch.key {
		if _, shouldRemove := removeSet[compId]; !shouldRemove {
			dstKey = append(dstKey, compId)
		}
	}

	dstArchId, dstArch := ecs.getOrMakeArchetype(dstKey)
	dstRow := ecs.archetypeReserveRow(dstArch)

	ecs.moveComponents(srcArch, srcRow, dstArch, dstRow)
	ecs.recycleEntity(entityId)

	dstArch.entities[entityId] = dstRow
	ecs.storage.entityIndex[entityId] = dstArchId
}

func (ecs *Ecs) moveComponents(srcArch *archetype, srcRow row, dstArch *archetype, dstRow row) {
	// We should make sure to always copy only the smallest subset of the components
	// E.g when removing component(s) we only want to move those that can fit into the destination Archetype
	var key archetypeKey
	if len(srcArch.key) <= len(dstArch.key) {
		key = srcArch.key
	} else {
		key = dstArch.key
	}

	for _, componentId := range key {
		srcValue := reflectSliceGet(srcArch.componentData[componentId], int(srcRow))
		reflectSliceSet(dstArch.componentData[componentId], int(dstRow), srcValue)
	}
}

func (ecs *Ecs) writeComponent(dstArch *archetype, dstRow row, component any) {
	componentType := reflect.TypeOf(component)
	if componentType.Kind() != reflect.Struct && componentType.Kind() == reflect.Pointer && componentType.Elem().Kind() != reflect.Struct {
		panic(fmt.Errorf("expected Component to be a struct or a pointer to a struct, got %s", componentType.Kind()))
	}

	reflectValue := reflect.ValueOf(component)
	if componentType.Kind() == reflect.Pointer {
		componentType = componentType.Elem()
		reflectValue = reflectValue.Elem()
	}

	componentId := ecs.getComponentId(componentType)
	reflectSliceSet(dstArch.componentData[componentId], int(dstRow), reflectValue)
}

func (ecs *Ecs) recycleEntity(entityId EntityId) {
	archId := ecs.storage.entityIndex[entityId]
	arch := ecs.storage.archetypes[archId]

	row := arch.entities[entityId]
	arch.recycled = append(arch.recycled, row)

	delete(arch.entities, entityId)
	delete(ecs.storage.entityIndex, entityId)
}

func (ecs *Ecs) archetypeFromComponents(components ...any) (archetypeId, archetypeKey, *archetype) {
	archKey := ecs.getArchetypeKey(components...)
	archId, arch := ecs.getOrMakeArchetype(archKey)
	return archId, archKey, arch
}

func (ecs *Ecs) archetypeFromExtraComponents(srcArch *archetype, components ...any) (archetypeId, archetypeKey, *archetype) {
	dstArchKey := combineArchetypeKeys(
		srcArch.key,
		ecs.getArchetypeKey(components...),
	)

	dstArchId, dstArch := ecs.getOrMakeArchetype(dstArchKey)
	return dstArchId, dstArchKey, dstArch
}

func (ecs *Ecs) getOrMakeArchetype(key archetypeKey) (archetypeId, *archetype) {
	id := getArchetypeId(key)

	if arch, ok := ecs.storage.archetypes[id]; ok {
		return id, arch
	}

	arch := &archetype{
		id:            id,
		key:           key,
		entities:      make(map[EntityId]row),
		componentData: make(map[componentId]any),
		recycled:      make([]row, 0),
	}
	for _, componentId := range arch.key {
		arch.componentData[componentId] = reflectSliceMake(
			ecs.componentIdTypeMap[componentId],
		)
	}

	ecs.storage.archetypes[id] = arch
	return id, arch
}

func (ecs *Ecs) archetypeReserveRow(arch *archetype) row {
	if len(arch.recycled) > 0 {
		row := arch.recycled[len(arch.recycled)-1]
		arch.recycled = arch.recycled[:len(arch.recycled)-1]
		return row
	}

	row := row(len(arch.entities))
	for _, componentId := range arch.key {
		arch.componentData[componentId] = reflectSliceAppend(
			arch.componentData[componentId],
			reflect.Zero(ecs.componentIdTypeMap[componentId]),
		)
	}
	return row
}

// Archetype's "Canonical" Key - a list of *sorted* ComponentIDs that make the archetype
// ArchetypeID is a value derived from they key (a hash)
// ArchetypeID is faster to lookup and compare but is prone to hash collisions
// Archetype Key is truly unique but is more cumbersom to deal with
func (ecs *Ecs) getArchetypeKey(components ...any) archetypeKey {
	var res archetypeKey

	for _, component := range components {
		compType := reflect.TypeOf(component)
		if compType.Kind() == reflect.Pointer {
			compType = compType.Elem()
		}
		if compType.Kind() != reflect.Struct {
			panic("component should be a struct")
		}

		res = append(res, ecs.getComponentId(compType))
	}

	return dedupAndSortArchetypeKey(res)
}

func combineArchetypeKeys(a archetypeKey, b archetypeKey) archetypeKey {
	aa := make([]uint32, len(a))
	for i, v := range a {
		aa[i] = uint32(v)
	}
	bb := make([]uint32, len(b))
	for i, v := range b {
		bb[i] = uint32(v)
	}
	cc := rooteecs.CombineKeys(aa, bb)
	res := make(archetypeKey, len(cc))
	for i, v := range cc {
		res[i] = componentId(v)
	}
	return res
}

func dedupAndSortArchetypeKey(key archetypeKey) archetypeKey {
	kk := make([]uint32, len(key))
	for i, v := range key {
		kk[i] = uint32(v)
	}
	canonical := rooteecs.CanonicalizeKey(kk)
	res := make(archetypeKey, len(canonical))
	for i, v := range canonical {
		res[i] = componentId(v)
	}
	return res
}

func getArchetypeId(key archetypeKey) archetypeId {
	kk := make([]uint32, len(key))
	for i, v := range key {
		kk[i] = uint32(v)
	}
	return archetypeId(rooteecs.ArchetypeID(kk))
}

func (ecs *Ecs) nextEntityId() EntityId {
	ecs.idGeneratorLock.Lock()
	defer ecs.idGeneratorLock.Unlock()

	id := ecs.entityIdCounter
	ecs.entityIdCounter += 1

	return id
}

func (ecs *Ecs) getComponentId(componentType reflect.Type) componentId {
	ecs.componentIdCounterLock.Lock()
	defer ecs.componentIdCounterLock.Unlock()

	if id, ok := ecs.componentTypeIdMap[componentType]; ok {
		return id
	} else {
		id = ecs.componentIdCounter
		ecs.componentIdCounter += 1

		ecs.componentTypeIdMap[componentType] = id
		ecs.componentIdTypeMap[id] = componentType

		return id
	}
}

func (ecs *Ecs) getComponentType(componentId componentId) reflect.Type {
	if t, ok := ecs.componentIdTypeMap[componentId]; ok {
		return t
	}
	panic("ComponentID not registered")
}

func (ecs *Ecs) getAllComponents(entityId EntityId) []any {
	archID := ecs.storage.entityIndex[entityId]
	arch := ecs.storage.archetypes[archID]
	r := arch.entities[entityId]

	res := make([]any, 0, len(arch.componentData))
	for _, componentsSlice := range arch.componentData {
		val := reflectSliceGet(componentsSlice, int(r))
		res = append(res, val.Interface())
	}
	return res
}

func (ecs *Ecs) hasComponent(entityId EntityId, componentType reflect.Type) bool {
	archID, ok := ecs.storage.entityIndex[entityId]
	if !ok {
		return false
	}
	arch := ecs.storage.archetypes[archID]
	compId := ecs.getComponentId(componentType)
	_, has := arch.componentData[compId]
	return has
}

func (ecs *Ecs) archetypeViews() []rooteecs.ArchetypeView {
	res := make([]rooteecs.ArchetypeView, 0, len(ecs.storage.archetypes))
	for _, arch := range ecs.storage.archetypes {
		res = append(res, rootArchetypeView{arch: arch})
	}
	return res
}
