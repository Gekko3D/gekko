package gekko

import (
	"fmt"
	"reflect"
	"runtime"
	"time"
)

type State int
type System any

type App struct {
	stateful            bool
	stateMachineStarted bool
	stateTransitioning  bool
	initialState        State
	finalState          State
	nextState           State
	state               State
	scheduledSystems    map[State]map[stateSchedule][]System
	resources           map[reflect.Type]any
	ecs                 *Ecs
}

const STATELESS_STATE State = 0

type Module interface {
	Install(app *App, commands *Commands)
}

func (app *App) Commands() *Commands {
	return &Commands{
		app: app,
	}
}

func (app *App) Run() {
	if app.stateful {
		app.runStateful()
	} else {
		app.runStateless()
	}
}

func (app *App) runStateful() {
	fmt.Println("Running in stateful mode...")

	app.executeChangeState(app.initialState)

	for {
		app.callSystems(app.state, execute)

		if app.stateTransitioning {
			app.stateTransitioning = false
			app.executeChangeState(app.nextState)
		}

		if app.state == app.finalState {
			break
		}
	}

	app.callSystems(app.state, exit)
}

func (app *App) runStateless() {
	fmt.Println("Running in stateless mode...")

	for {
		app.callSystems(STATELESS_STATE, execute)
	}
}

func (app *App) changeState(newState State) {
	app.nextState = newState
	app.stateTransitioning = true
}

func (app *App) executeChangeState(newState State) {
	if !app.stateMachineStarted {
		app.stateMachineStarted = true

		app.state = newState
		app.callSystems(app.state, enter)
	} else {
		app.callSystems(app.state, exit)
		app.state = newState
		app.callSystems(app.state, enter)
	}
}

func (app *App) addResources(resources ...any) *App {
	for _, resource := range resources {
		resourceType := reflect.TypeOf(resource)
		if _, ok := app.resources[resourceType.Elem()]; ok {
			panic(fmt.Sprintf("%s is already in resources", resourceType))
		}

		app.resources[resourceType.Elem()] = resource
	}
	return app
}

func (app *App) callSystems(state State, schedule stateSchedule) {
	for _, system := range app.scheduledSystems[state][schedule] {
		app.callSystem(system)
	}
}

func (app *App) callSystem(system System) {
	start := time.Now()

	app.callSystemInternal(system)

	fmt.Println(
		"system ",
		runtime.FuncForPC(reflect.ValueOf(system).Pointer()).Name(),
		": ",
		time.Since(start).Milliseconds(),
		"ms",
	)
}

var typeOfCommands = reflect.TypeOf(Commands{})

func (app *App) callSystemInternal(system System) {
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
			println(msg)
			panic(msg)
		}
	}
	systemValue.Call(args)
}
