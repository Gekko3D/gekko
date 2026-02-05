package gekko

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"reflect"
	"slices"
	"sync"
)

type EntityId uint64
type archetypeId uint64
type archetypeKey []componentId
type componentId uint32
type typedStorage any
type row int
type set[T comparable] = map[T]struct{}

type Ecs struct {
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
	return Ecs{
		archetypes:  make(map[archetypeId]*archetype),
		entityIndex: make(map[EntityId]archetypeId),
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

	ecs.entityIndex[entityId] = archId

	return entityId
}

func (ecs *Ecs) removeEntity(entityId EntityId) {
	ecs.recycleEntity(entityId)
}

func (ecs *Ecs) addComponents(entityId EntityId, components ...any) {
	srcArchId := ecs.entityIndex[entityId]
	srcArch := ecs.archetypes[srcArchId]
	srcRow := srcArch.entities[entityId]

	dstArchId, _, dstArch := ecs.archetypeFromExtraComponents(srcArch, components...)
	dstRow := ecs.archetypeReserveRow(dstArch)

	ecs.moveComponents(srcArch, srcRow, dstArch, dstRow)
	for _, component := range components {
		ecs.writeComponent(dstArch, dstRow, component)
	}

	ecs.recycleEntity(entityId)

	dstArch.entities[entityId] = dstRow
	ecs.entityIndex[entityId] = dstArchId
}

func (ecs *Ecs) removeComponents(entityId EntityId, components ...any) {
	srcArchId := ecs.entityIndex[entityId]
	srcArch := ecs.archetypes[srcArchId]
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
	ecs.entityIndex[entityId] = dstArchId
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
	archId := ecs.entityIndex[entityId]
	arch := ecs.archetypes[archId]

	row := arch.entities[entityId]
	arch.recycled = append(arch.recycled, row)

	delete(arch.entities, entityId)
	delete(ecs.entityIndex, entityId)
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

	if arch, ok := ecs.archetypes[id]; ok {
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

	ecs.archetypes[id] = arch
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
	return dedupAndSortArchetypeKey(append(a, b...))
}

func dedupAndSortArchetypeKey(key archetypeKey) archetypeKey {
	dedup := make(set[componentId])

	for _, v := range key {
		dedup[v] = struct{}{}
	}

	res := make(archetypeKey, 0, len(dedup))
	for k, _ := range dedup {
		res = append(res, k)
	}

	slices.Sort(res)
	return res
}

func getArchetypeId(key archetypeKey) archetypeId {
	hash := fnv.New64a()
	for _, componentId := range key {
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, uint64(componentId))
		hash.Write(b)
	}
	return archetypeId(hash.Sum64())
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
