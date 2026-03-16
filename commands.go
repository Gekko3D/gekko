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
	cmd.app.pendingAdditions = append(cmd.app.pendingAdditions, pendingAdd{
		eid:        eid,
		components: components,
	})
	return eid
}

func (cmd *Commands) AddComponents(entityId EntityId, components ...any) {
	cmd.app.cmdMutex.Lock()
	defer cmd.app.cmdMutex.Unlock()
	cmd.app.pendingCompAdds = append(cmd.app.pendingCompAdds, pendingCompAdd{
		eid:        entityId,
		components: components,
	})
}

func (cmd *Commands) RemoveComponents(entityId EntityId, components ...any) {
	cmd.app.cmdMutex.Lock()
	defer cmd.app.cmdMutex.Unlock()
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
