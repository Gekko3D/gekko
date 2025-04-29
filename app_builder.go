package gekko

import (
	"reflect"
)

type AppBuilder struct {
	app     *App
	modules []Module
}

func NewAppBuilder() *AppBuilder {
	ecs := MakeEcs()
	return &AppBuilder{app: &App{
		resources:        make(map[reflect.Type]any),
		scheduledSystems: make(map[State]map[stateSchedule][]System),
		stateful:         false,
		ecs:              &ecs,
	}}
}

func (b *AppBuilder) UseStates(initialState State, finalState State) *AppBuilder {
	b.app.stateful = true
	b.app.initialState = initialState
	b.app.finalState = finalState

	return b
}

func (b *AppBuilder) UseModule(modules ...Module) *AppBuilder {
	b.modules = append(b.modules, modules...)

	return b
}

func (b *AppBuilder) Build() *App {
	app := b.app
	commands := &Commands{app: app}

	for _, module := range b.modules {
		module.Install(app, commands)
	}

	return app
}
