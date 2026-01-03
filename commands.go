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
	return cmd.app.ecs.addEntity(components...)
}

func (cmd *Commands) AddComponents(entityId EntityId, components ...any) {
	cmd.app.ecs.addComponents(entityId, components...)
}

func (cmd *Commands) RemoveEntity(entityId EntityId) {
	cmd.app.ecs.removeEntity(entityId)
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
