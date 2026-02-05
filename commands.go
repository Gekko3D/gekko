package gekko

type Commands struct {
	app *App
}

func (cmd *Commands) ChangeState(newState State) *Commands {
	cmd.app.changeState(newState)
	return cmd
}

func (cmd *Commands) AddResources(resources ...any) *Commands {
	cmd.app.addResources(resources...)
	return cmd
}

func (cmd *Commands) AddEntity(components ...any) EntityId {
	eid := cmd.app.ecs.nextEntityId()
	cmd.app.pendingAdditions = append(cmd.app.pendingAdditions, pendingAdd{
		eid:        eid,
		components: components,
	})
	return eid
}

func (cmd *Commands) AddComponents(entityId EntityId, components ...any) {
	cmd.app.pendingCompAdds = append(cmd.app.pendingCompAdds, pendingCompAdd{
		eid:        entityId,
		components: components,
	})
}

func (cmd *Commands) RemoveComponents(entityId EntityId, components ...any) {
	cmd.app.pendingCompRemovals = append(cmd.app.pendingCompRemovals, pendingCompRemoval{
		eid:        entityId,
		components: components,
	})
}

func (cmd *Commands) RemoveEntity(entityId EntityId) {
	cmd.app.pendingRemovals = append(cmd.app.pendingRemovals, entityId)
}

func (cmd *Commands) GetAllComponents(entityId EntityId) []any {
	ecs := cmd.app.ecs
	archId := ecs.entityIndex[entityId]
	arch := ecs.archetypes[archId]

	row := arch.entities[entityId]

	var res []any
	for _, componentsSlice := range arch.componentData {
		val := reflectSliceGet(componentsSlice, int(row))
		res = append(res, val.Interface())
	}
	return res
}
