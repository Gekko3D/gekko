package gekko

import (
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"time"
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
	targetFPS          int
	targetFrameTime    time.Duration
	fixedTimestep      time.Duration
	accumulator        time.Duration
	lastFrameTime      time.Time

	// Command Buffering
	cmdMutex            sync.Mutex
	pendingAdditions    []pendingAdd
	pendingRemovals     []EntityId
	pendingCompAdds     []pendingCompAdd
	pendingCompRemovals []pendingCompRemoval
}

type pendingAdd struct {
	eid        EntityId
	components []any
}

type pendingCompAdd struct {
	eid        EntityId
	components []any
}
type pendingCompRemoval struct {
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
		fmt.Println("Running in stateful mode...")

		app.state = app.initialState
		app.callSystems(app.state, enter, DynamicUpdate)
	} else {
		fmt.Println("Running in stateless mode...")
	}

	app.lastFrameTime = time.Now()
	if app.fixedTimestep == 0 {
		app.fixedTimestep = time.Second / 60
	}

	for {
		frameStart := time.Now()
		dt := frameStart.Sub(app.lastFrameTime)
		app.lastFrameTime = frameStart

		// Dynamic Dt clamping for safety
		dynamicDt := dt.Seconds()
		if dynamicDt > 0.1 {
			dynamicDt = 0.1
		}

		// 1. Update Time Resource for early Dynamic stages
		if t, ok := app.resources[reflect.TypeOf(Time{})]; ok {
			timeRes := t.(*Time)
			timeRes.Dt = dynamicDt
			timeRes.Duration = dt
			timeRes.Time = frameStart
			timeRes.Alpha = 0
		}

		// 2. Run Prelude (Captures Input)
		app.callStages(app.state, execute, DynamicUpdate, "Prelude")

		// 3. Fixed Update Loop
		app.accumulator += dt
		if app.accumulator > time.Second {
			app.accumulator = time.Second
		}

		numSteps := int(app.accumulator.Seconds() / app.fixedTimestep.Seconds())
		if t, ok := app.resources[reflect.TypeOf(Time{})]; ok {
			timeRes := t.(*Time)
			timeRes.FixedStepCount = numSteps
		}

		for app.accumulator >= app.fixedTimestep {
			if t, ok := app.resources[reflect.TypeOf(Time{})]; ok {
				timeRes := t.(*Time)
				timeRes.Dt = app.fixedTimestep.Seconds()
			}

			app.callSystems(app.state, execute, FixedUpdate)
			app.accumulator -= app.fixedTimestep
		}

		// 4. Update Time Resource for Render/Gameplay
		if t, ok := app.resources[reflect.TypeOf(Time{})]; ok {
			timeRes := t.(*Time)
			timeRes.Dt = dynamicDt
			timeRes.Alpha = float32(app.accumulator.Seconds() / app.fixedTimestep.Seconds())
		}

		// 5. Run remaining Dynamic stages
		app.callStagesExcluding(app.state, execute, DynamicUpdate, "Prelude")

		// 6. Clear Accumulated Mouse Input after all steps
		if i, ok := app.resources[reflect.TypeOf(Input{})]; ok {
			input := i.(*Input)
			input.AccumulatedMouseDeltaX = 0
			input.AccumulatedMouseDeltaY = 0
		}

		if app.stateful {
			if app.stateTransitioning {
				app.stateTransitioning = false
				app.executeChangeState(app.nextState)
			}
		}

		app.sleepForFramePacing(time.Since(frameStart))

		if app.stateful && app.state == app.finalState {
			app.callSystems(app.state, exit, DynamicUpdate)
			break
		}
	}
}

func (app *App) callStages(state State, phase statePhase, updateType UpdateType, stageNames ...string) {
	for _, name := range stageNames {
		var stage *Stage
		for i := range app.stages {
			if app.stages[i].Name == name {
				stage = &app.stages[i]
				break
			}
		}
		if stage == nil || stage.UpdateType != updateType {
			continue
		}
		app.callStage(state, phase, *stage)
	}
}

func (app *App) callStagesExcluding(state State, phase statePhase, updateType UpdateType, exclude ...string) {
	for _, stage := range app.stages {
		if stage.UpdateType != updateType {
			continue
		}
		shouldExclude := false
		for _, ex := range exclude {
			if stage.Name == ex {
				shouldExclude = true
				break
			}
		}
		if shouldExclude {
			continue
		}
		app.callStage(state, phase, stage)
	}
}

func (app *App) callStage(state State, phase statePhase, stage Stage) {
	// On execute, call stateless/always run systems first
	if execute == phase {
		for _, system := range app.systemsStateless[stage.Name] {
			app.callSystem(system)
		}
	}

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
	app.cmdMutex.Lock()
	app.FlushCommands()
	app.cmdMutex.Unlock()
}

func (app *App) callSystems(state State, phase statePhase, updateType UpdateType) {
	for _, stage := range app.stages {
		if stage.UpdateType != updateType {
			continue
		}

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
		app.cmdMutex.Lock()
		app.FlushCommands()
		app.cmdMutex.Unlock()
	}
}

func (app *App) changeState(newState State) {
	app.nextState = newState
	app.stateTransitioning = true
}

func (app *App) executeChangeState(newState State) {
	app.callSystems(app.state, exit, DynamicUpdate)
	app.state = newState
	app.callSystems(app.state, enter, DynamicUpdate)
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
		} else {
			app.cmdMutex.Lock()
			resource, argIsResource := app.resources[underlyingType]
			app.cmdMutex.Unlock()

			if argIsResource {
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
	}
	systemValue.Call(args)
}

func (app *App) FlushCommands() {
	if len(app.pendingAdditions) == 0 && len(app.pendingRemovals) == 0 && len(app.pendingCompAdds) == 0 && len(app.pendingCompRemovals) == 0 {
		return
	}

	// 1. Process Removals first (so we don't add to dead entities)
	for _, eid := range app.pendingRemovals {
		app.ecs.removeEntity(eid)
	}
	app.pendingRemovals = app.pendingRemovals[:0]

	// 2. Process Additions
	for _, add := range app.pendingAdditions {
		app.ecs.insertEntity(add.eid, add.components...)
	}
	app.pendingAdditions = app.pendingAdditions[:0]

	// 3. Process Component Removals first (so re-adding in same frame works)
	for _, rem := range app.pendingCompRemovals {
		app.ecs.removeComponents(rem.eid, rem.components...)
	}
	app.pendingCompRemovals = app.pendingCompRemovals[:0]

	// 4. Process Component Additions
	for _, add := range app.pendingCompAdds {
		app.ecs.addComponents(add.eid, add.components...)
	}
	app.pendingCompAdds = app.pendingCompAdds[:0]
}

func (app *App) sleepForFramePacing(frameElapsed time.Duration) {
	sleepDuration := computeFramePacingSleep(frameElapsed, app.targetFrameTime)
	if sleepDuration > 0 {
		time.Sleep(sleepDuration)
	}
}

func computeFramePacingSleep(frameElapsed time.Duration, targetFrameTime time.Duration) time.Duration {
	if targetFrameTime <= 0 || frameElapsed >= targetFrameTime {
		return 0
	}
	return targetFrameTime - frameElapsed
}
