package gekko

import (
	"fmt"
	"slices"
)

type State int

type UpdateType int

const (
	FixedUpdate UpdateType = iota
	DynamicUpdate
)

type Stage struct {
	Name       string
	UpdateType UpdateType
}

var (
	Prelude    = Stage{Name: "Prelude", UpdateType: DynamicUpdate}
	PreUpdate  = Stage{Name: "PreUpdate", UpdateType: DynamicUpdate}
	Update     = Stage{Name: "Update", UpdateType: DynamicUpdate}
	PostUpdate = Stage{Name: "PostUpdate", UpdateType: DynamicUpdate}
	PreRender  = Stage{Name: "PreRender", UpdateType: DynamicUpdate}
	Render     = Stage{Name: "Render", UpdateType: DynamicUpdate}
	PostRender = Stage{Name: "PostRender", UpdateType: DynamicUpdate}
	Finale     = Stage{Name: "Finale", UpdateType: DynamicUpdate}
)

type systemScheduleBuilder struct {
	inStage       Stage
	runAlways     bool
	inState       State
	inStatePhase  statePhase
	system        systemFn
	stateProvided bool
}

type stateScheduleBuilder struct {
	state  State
	phase  statePhase
	always bool
}

type statePhase int

const (
	enter   statePhase = 0
	execute statePhase = 1
	exit    statePhase = 2
)

func OnEnter(state State) stateScheduleBuilder {
	return stateScheduleBuilder{state: state, phase: enter, always: false}
}

func OnExecute(state State) stateScheduleBuilder {
	return stateScheduleBuilder{state: state, phase: execute, always: false}
}

func OnExit(state State) stateScheduleBuilder {
	return stateScheduleBuilder{state: state, phase: exit, always: false}
}

func Always() stateScheduleBuilder {
	return stateScheduleBuilder{always: true}
}

func (sched systemScheduleBuilder) InStage(s Stage) systemScheduleBuilder {
	return systemScheduleBuilder{
		system:        sched.system,
		inStage:       s,
		runAlways:     sched.runAlways,
		inState:       sched.inState,
		inStatePhase:  sched.inStatePhase,
		stateProvided: sched.stateProvided,
	}
}

func (sched systemScheduleBuilder) InState(s stateScheduleBuilder) systemScheduleBuilder {
	return systemScheduleBuilder{
		system:        sched.system,
		inStage:       sched.inStage,
		runAlways:     s.always,
		inState:       s.state,
		inStatePhase:  s.phase,
		stateProvided: true,
	}
}

func (sched systemScheduleBuilder) RunAlways() systemScheduleBuilder {
	return systemScheduleBuilder{
		system:        sched.system,
		inStage:       sched.inStage,
		runAlways:     true,
		inState:       sched.inState,
		inStatePhase:  sched.inStatePhase,
		stateProvided: sched.stateProvided,
	}
}

func (sched systemScheduleBuilder) InAnyState() systemScheduleBuilder {
	return sched.RunAlways()
}

func System(system systemFn) systemScheduleBuilder {
	return systemScheduleBuilder{
		system:    system,
		inStage:   Update,
		runAlways: false,
		//inState:      STATELESS_STATE,
		//inStatePhase: STATELESS_PHASE,
		stateProvided: false,
	}

}

type stagePosition int

const (
	stageBefore stagePosition = iota
	stageAfter
)

type stagePositionBuilder struct {
	position stagePosition
	target   Stage
}

func BeforeStage(s Stage) stagePositionBuilder {
	return stagePositionBuilder{
		position: stageBefore,
		target:   s,
	}
}

func AfterStage(s Stage) stagePositionBuilder {
	return stagePositionBuilder{
		position: stageAfter,
		target:   s,
	}
}

func (app *App) UseStage(stage Stage, where stagePositionBuilder) *App {
	var stageIdx int = -1
	for i, s := range app.stages {
		if s.Name == where.target.Name {
			stageIdx = i
			break
		}
	}
	if -1 == stageIdx {
		panic(fmt.Sprintf("Stage %v not found", where.target.Name))
	}

	var insertAt int
	if stageBefore == where.position {
		insertAt = stageIdx
	} else {
		insertAt = stageIdx + 1
	}

	app.stages = slices.Insert(app.stages, insertAt, stage)
	app.initStatefulStage(stage)

	return app
}

func (app *App) UseSystem(system systemScheduleBuilder) *App {
	if system.runAlways || !system.stateProvided {
		if _, ok := app.systemsStateless[system.inStage.Name]; ok {
			app.systemsStateless[system.inStage.Name] = append(app.systemsStateless[system.inStage.Name], system.system)
			return app
		}
	} else {
		if !app.stateful {
			panic("Trying to use a stateful system in a stateless app.")
		}

		if systemsInStage, ok := app.systems[system.inStage.Name]; ok {
			phase := system.inStatePhase

			if systemsInState, ok := systemsInStage[system.inState]; ok {
				if _, ok := systemsInState[phase]; !ok {
					systemsInState[phase] = make([]systemFn, 0, 1)
				}

				systemsInState[phase] = append(systemsInState[phase], system.system)
				return app
			}
			panic(fmt.Sprintf("State %v doesn't exist", system.inState))
		}
	}
	panic(fmt.Sprintf("Stage %v doesn't exist", system.inStage.Name))
}

func (app *App) initStatefulStage(stage Stage) {
	app.systemsStateless[stage.Name] = make([]systemFn, 0)

	if app.stateful {
		app.systems[stage.Name] = make(map[State]map[statePhase][]systemFn)
		for state := app.initialState; state <= app.finalState; state += 1 {
			app.systems[stage.Name][state] = make(map[statePhase][]systemFn)
			app.systems[stage.Name][state][enter] = make([]systemFn, 0)
			app.systems[stage.Name][state][execute] = make([]systemFn, 0)
			app.systems[stage.Name][state][exit] = make([]systemFn, 0)
		}
	}
}
