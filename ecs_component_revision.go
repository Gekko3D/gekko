package gekko

import "reflect"

// ComponentRevision reads the aggregate publication sequence for a component
// type in this storage owner. Use it on the main thread. Direct field writes
// require an explicit MarkComponentChanged; this is not automatic dirty tracking.
func (ecs *Ecs) ComponentRevision(componentType reflect.Type) uint64 {
	componentType = canonicalPublicationType(componentType)
	if ecs == nil || ecs.storage == nil || componentType == nil {
		return 0
	}
	return ecs.storage.componentRevisions[componentType]
}

// MarkComponentChanged immediately publishes a currently committed component
// on the main thread, including an explicit publication of an unchanged value.
// It does not mutate values, enqueue commands or flush pending work.
func (ecs *Ecs) MarkComponentChanged(entityId EntityId, componentType reflect.Type) bool {
	componentType = canonicalPublicationType(componentType)
	if ecs == nil || ecs.storage == nil || componentType == nil {
		return false
	}
	compId, registered := ecs.componentTypeIdMap[componentType]
	if !registered {
		return false
	}
	archId, live := ecs.storage.entityIndex[entityId]
	if !live {
		return false
	}
	arch := ecs.storage.archetypes[archId]
	if arch == nil {
		return false
	}
	if _, present := arch.entities[entityId]; !present {
		return false
	}
	if _, present := arch.componentData[compId]; !present {
		return false
	}
	ecs.publishComponentType(entityId, componentType)
	return true
}

func canonicalPublicationType(componentType reflect.Type) reflect.Type {
	if componentType == nil {
		return nil
	}
	if componentType.Kind() == reflect.Pointer {
		componentType = componentType.Elem()
	}
	if componentType.Kind() != reflect.Struct {
		return nil
	}
	return componentType
}

func (ecs *Ecs) publishComponentType(entityId EntityId, componentType reflect.Type) {
	if ecs.storage.componentRevisions == nil {
		ecs.storage.componentRevisions = make(map[reflect.Type]uint64)
	}
	ecs.storage.componentRevisions[componentType]++
	ecs.storage.componentPublications.append(ComponentPublication{Entity: entityId, ComponentType: componentType})
}

// Archetype keys already contain each present type once. No row data or
// archetype references are retained by the publication owner.
func (ecs *Ecs) publishComponentKey(entityId EntityId, key archetypeKey) {
	for _, compId := range key {
		ecs.publishComponentType(entityId, ecs.componentIdTypeMap[compId])
	}
}

func (ecs *Ecs) publishSuppliedComponents(entityId EntityId, components []any) {
	seen := make(set[reflect.Type], len(components))
	for _, component := range components {
		componentType := canonicalPublicationType(reflect.TypeOf(component))
		if _, published := seen[componentType]; published {
			continue
		}
		seen[componentType] = struct{}{}
		ecs.publishComponentType(entityId, componentType)
	}
}
