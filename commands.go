package gekko

type Commands struct {
	app *App
}

type stateSchedule int

const (
	enter   stateSchedule = 0
	execute stateSchedule = 1
	exit    stateSchedule = 2
)

type systemSchedule struct {
	state    State
	schedule stateSchedule
}

func OnEnter(state State) systemSchedule {
	return systemSchedule{state: state, schedule: enter}
}

func OnExecute(state State) systemSchedule {
	return systemSchedule{state: state, schedule: execute}
}

func OnExit(state State) systemSchedule {
	return systemSchedule{state: state, schedule: exit}
}

func (cmd *Commands) UseSystem(system System, when ...systemSchedule) *Commands {
	// Stateless apps use state=0
	if 0 == len(when) && !cmd.app.stateful {
		when = []systemSchedule{{state: STATELESS_STATE, schedule: execute}}
	}

	// Ensure the state exists in the map
	for _, schedule := range when {
		if _, ok := cmd.app.scheduledSystems[schedule.state]; !ok {
			cmd.app.scheduledSystems[schedule.state] = make(map[stateSchedule][]System)
		}

		// Schedule the system for the specific state and event
		cmd.app.scheduledSystems[schedule.state][schedule.schedule] = append(
			cmd.app.scheduledSystems[schedule.state][schedule.schedule], system,
		)
	}

	return cmd
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
