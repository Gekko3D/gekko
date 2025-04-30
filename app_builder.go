package gekko

import (
	"reflect"
)

func NewApp() *App {
	ecs := MakeEcs()
	return &App{
		resources:        make(map[reflect.Type]any),
		stateful:         false,
		systems:          make(map[string]map[State]map[statePhase][]systemFn),
		systemsStateless: make(map[string][]systemFn),
		ecs:              &ecs,
		modules:          make([]Module, 0),
	}
}

func (app *App) UseStates(initialState State, finalState State) *App {
	app.stateful = true
	app.initialState = initialState
	app.finalState = finalState

	return app
}

func (app *App) UseModules(modules ...Module) *App {
	app.modules = append(app.modules, modules...)

	return app
}

func (app *App) build() {
	app.stages = append(app.stages, Prelude)
	app.stages = append(app.stages, PreUpdate)
	app.stages = append(app.stages, Update)
	app.stages = append(app.stages, PostUpdate)
	app.stages = append(app.stages, PreRender)
	app.stages = append(app.stages, Render)
	app.stages = append(app.stages, PostRender)
	app.stages = append(app.stages, Finale)
	for _, stage := range app.stages {
		app.initStatefulStage(stage)
	}

	commands := &Commands{app: app}

	for _, module := range app.modules {
		module.Install(app, commands)
	}
}
