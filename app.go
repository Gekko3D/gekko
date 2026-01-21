package gekko

import (
	"fmt"
	"reflect"
	"runtime"
)

type systemFn any

type App struct {
	stateful           bool
	stateTransitioning bool
	initialState       State
	finalState         State
	nextState          State
	state              State
	modules            []Module
	stages             []Stage
	systems            map[string]map[State]map[statePhase][]systemFn
	systemsStateless   map[string][]systemFn
	resources          map[reflect.Type]any
	ecs                *Ecs

	// Command Buffering
	pendingAdditions []pendingAdd
	pendingRemovals  []EntityId
	pendingCompAdds  []pendingCompAdd
}

type pendingAdd struct {
	eid        EntityId
	components []any
}

type pendingCompAdd struct {
	eid        EntityId
	components []any
}

func (app *App) Commands() *Commands {
	return &Commands{
		app: app,
	}
}

func (app *App) Run() {
	app.build()

	if app.stateful {
		app.Logger().Infof("Running in stateful mode...")

		app.state = app.initialState
		app.callSystems(app.state, enter)
	} else {
		app.Logger().Infof("Running in stateless mode...")
	}

	for {
		app.callSystems(app.state, execute)

		if app.stateful {
			if app.stateTransitioning {
				app.stateTransitioning = false
				app.executeChangeState(app.nextState)
			}

			if app.state == app.finalState {
				app.callSystems(app.state, exit)
				break
			}
		}
	}
}

func (app *App) callSystems(state State, phase statePhase) {
	for _, stage := range app.stages {
		// On execute, call stateless/always run systems first
		if execute == phase {
			for _, system := range app.systemsStateless[stage.Name] {
				app.callSystem(system)
			}
		}

		// Call stateful systems, if required
		if app.stateful {
			if systemsInStage, ok := app.systems[stage.Name]; ok {
				if systemsInState, ok := systemsInStage[state]; ok {
					if systemsInPhase, ok := systemsInState[phase]; ok {
						for _, system := range systemsInPhase {
							app.callSystem(system)
						}
					}
				}
			}
		}
		app.FlushCommands()
	}
}

func (app *App) changeState(newState State) {
	app.nextState = newState
	app.stateTransitioning = true
}

func (app *App) executeChangeState(newState State) {
	app.callSystems(app.state, exit)
	app.state = newState
	app.callSystems(app.state, enter)
}

func (app *App) addResources(resources ...any) *App {
	for _, resource := range resources {
		resourceType := reflect.TypeOf(resource)
		if resourceType == nil {
			panic("cannot add nil resource")
		}
		if resourceType.Kind() != reflect.Ptr {
			panic(fmt.Sprintf("resources must be pointers; got %s", resourceType))
		}
		elem := resourceType.Elem()
		if _, ok := app.resources[elem]; ok {
			panic(fmt.Sprintf("%s is already in resources", elem))
		}
		app.resources[elem] = resource
	}
	return app
}

func (app *App) callSystem(system systemFn) {
	// start := time.Now()

	app.callSystemInternal(system)

	// fmt.Println(
	// 	"system ",
	// 	runtime.FuncForPC(reflect.ValueOf(system).Pointer()).Name(),
	// 	": ",
	// 	time.Since(start).Milliseconds(),
	// 	"ms",
	// )
}

var typeOfCommands = reflect.TypeOf(Commands{})

func (app *App) callSystemInternal(system systemFn) {
	systemType := reflect.TypeOf(system)
	systemValue := reflect.ValueOf(system)

	args := make([]reflect.Value, systemType.NumIn())

	for i := 0; i < systemType.NumIn(); i++ {
		argType := systemType.In(i)
		underlyingType := argType.Elem()

		if underlyingType == typeOfCommands {
			args[i] = reflect.ValueOf(&Commands{app: app})
			//} else if isQueryArgument(underlyingType) {
			//	queryPtr := this.generateQueryObject(underlyingType)
			//	args[i] = queryPtr
		} else if resource, argIsResource := app.resources[underlyingType]; argIsResource {
			resourceVal := reflect.ValueOf(resource)
			typedResourceVal := reflect.NewAt(underlyingType, resourceVal.UnsafePointer())

			args[i] = typedResourceVal
		} else {
			msg := fmt.Sprintf("Unable to resolve System dependency.\nSystem: %s\nSystem type: %s\nDependency: %s",
				runtime.FuncForPC(systemValue.Pointer()).Name(),
				fmt.Sprint(systemType),
				fmt.Sprint(argType),
			)
			app.Logger().Errorf("%s", msg)
			panic(msg)
		}
	}
	systemValue.Call(args)
}

func (app *App) FlushCommands() {
	if len(app.pendingAdditions) == 0 && len(app.pendingRemovals) == 0 && len(app.pendingCompAdds) == 0 {
		return
	}

	// 1. Process Removals first (so we don't add to dead entities)
	for _, eid := range app.pendingRemovals {
		app.Logger().Debugf("FLUSH: Removing entity %v", eid)
		app.ecs.removeEntity(eid)
	}
	app.pendingRemovals = app.pendingRemovals[:0]

	// 2. Process Additions
	for _, add := range app.pendingAdditions {
		// fmt.Printf("FLUSH: Adding entity %v\n", add.eid)
		app.ecs.insertEntity(add.eid, add.components...)
	}
	app.pendingAdditions = app.pendingAdditions[:0]

	// 3. Process Component Additions
	for _, add := range app.pendingCompAdds {
		app.ecs.addComponents(add.eid, add.components...)
	}
	app.pendingCompAdds = app.pendingCompAdds[:0]
}
