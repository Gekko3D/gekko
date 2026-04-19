package gekko

import "reflect"

type Commands struct {
	app *App
}

func (cmd *Commands) ChangeState(newState State) *Commands {
	cmd.app.changeState(newState)
	return cmd
}

func (cmd *Commands) AddResources(resources ...any) *Commands {
	cmd.app.cmdMutex.Lock()
	defer cmd.app.cmdMutex.Unlock()
	cmd.app.addResources(resources...)
	return cmd
}

func (cmd *Commands) AddEntity(components ...any) EntityId {
	cmd.app.cmdMutex.Lock()
	defer cmd.app.cmdMutex.Unlock()
	eid := cmd.app.ecs.nextEntityId()
	components = sanitizeComponents(components)
	cmd.app.pendingAdditions = append(cmd.app.pendingAdditions, pendingAdd{
		eid:        eid,
		components: components,
	})
	return eid
}

func (cmd *Commands) AddEntityInGroup(group EntityGroupKey, components ...any) EntityId {
	return cmd.AddEntityInGroups([]EntityGroupKey{group}, components...)
}

func (cmd *Commands) AddEntityInGroups(groups []EntityGroupKey, components ...any) EntityId {
	cmd.app.cmdMutex.Lock()
	defer cmd.app.cmdMutex.Unlock()

	eid := cmd.app.ecs.nextEntityId()
	components = sanitizeComponents(components)
	components = mergeEntityGroupMembershipIntoComponents(groups, components)
	cmd.app.pendingAdditions = append(cmd.app.pendingAdditions, pendingAdd{
		eid:        eid,
		components: components,
	})
	return eid
}

func (cmd *Commands) AddComponents(entityId EntityId, components ...any) {
	cmd.app.cmdMutex.Lock()
	defer cmd.app.cmdMutex.Unlock()
	components = sanitizeComponents(components)
	if len(components) == 0 {
		return
	}
	cmd.app.pendingCompAdds = append(cmd.app.pendingCompAdds, pendingCompAdd{
		eid:        entityId,
		components: components,
	})
}

func (cmd *Commands) RemoveComponents(entityId EntityId, components ...any) {
	cmd.app.cmdMutex.Lock()
	defer cmd.app.cmdMutex.Unlock()
	components = sanitizeComponents(components)
	if len(components) == 0 {
		return
	}
	cmd.app.pendingCompRemovals = append(cmd.app.pendingCompRemovals, pendingCompRemoval{
		eid:        entityId,
		components: components,
	})
}

func (cmd *Commands) RemoveEntity(entityId EntityId) {
	cmd.app.cmdMutex.Lock()
	defer cmd.app.cmdMutex.Unlock()
	cmd.app.pendingRemovals = append(cmd.app.pendingRemovals, entityId)
}

func (cmd *Commands) RemoveEntitiesInGroup(key EntityGroupKey) []EntityId {
	entities := cmd.app.ecs.getEntitiesInGroup(key)
	if len(entities) == 0 {
		return nil
	}

	cmd.app.cmdMutex.Lock()
	defer cmd.app.cmdMutex.Unlock()
	cmd.app.pendingRemovals = append(cmd.app.pendingRemovals, entities...)
	return entities
}

func (cmd *Commands) GetAllComponents(entityId EntityId) []any {
	return cmd.app.ecs.getAllComponents(entityId)
}

func (cmd *Commands) GetComponent(entityId EntityId, componentType reflect.Type) any {
	return cmd.app.ecs.getComponent(entityId, componentType)
}

func (cmd *Commands) GetEntitiesInGroup(key EntityGroupKey) []EntityId {
	return cmd.app.ecs.getEntitiesInGroup(key)
}

func (cmd *Commands) GetEntityGroups(entityId EntityId) []EntityGroupKey {
	return cmd.app.ecs.getEntityGroups(entityId)
}

func (cmd *Commands) HasGroup(entityId EntityId, key EntityGroupKey) bool {
	return cmd.app.ecs.hasGroup(entityId, key)
}

func sanitizeComponents(components []any) []any {
	if len(components) == 0 {
		return components
	}
	sanitized := make([]any, 0, len(components))
	for _, component := range components {
		if component == nil {
			continue
		}
		value := reflect.ValueOf(component)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				continue
			}
		}
		sanitized = append(sanitized, component)
	}
	return sanitized
}

func mergeEntityGroupMembershipIntoComponents(groups []EntityGroupKey, components []any) []any {
	mergedGroups := EntityGroupMembershipComponent{
		Groups: append([]EntityGroupKey(nil), groups...),
	}
	mergedComponents := make([]any, 0, len(components)+1)

	for _, component := range components {
		switch typed := component.(type) {
		case EntityGroupMembershipComponent:
			mergedGroups.Groups = append(mergedGroups.Groups, typed.Groups...)
		case *EntityGroupMembershipComponent:
			if typed != nil {
				mergedGroups.Groups = append(mergedGroups.Groups, typed.Groups...)
			}
		default:
			mergedComponents = append(mergedComponents, component)
		}
	}

	mergedGroups = canonicalizeEntityGroupMembership(mergedGroups)
	if len(mergedGroups.Groups) > 0 {
		mergedComponents = append(mergedComponents, &mergedGroups)
	}
	return mergedComponents
}
