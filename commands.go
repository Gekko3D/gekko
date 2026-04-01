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

func (cmd *Commands) GetAllComponents(entityId EntityId) []any {
	return cmd.app.ecs.getAllComponents(entityId)
}

func (cmd *Commands) GetComponent(entityId EntityId, componentType reflect.Type) any {
	return cmd.app.ecs.getComponent(entityId, componentType)
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
