package gekko

import "reflect"

func streamedLevelNPCNavigationSystem(cmd *Commands, state *StreamedLevelRuntimeState) {
	if cmd == nil || state == nil {
		return
	}
	UpdateNPCNavigationRoutes(cmd, RuntimeNavigationServiceFromStreamedLevelState(state))
}

func streamedLevelNPCNavigationMovementSystem(cmd *Commands) {
	time := npcNavigationRuntimeTime(cmd)
	if time == nil {
		return
	}
	UpdateNPCNavigationMovement(cmd, time)
}

func npcNavigationRuntimeTime(cmd *Commands) *Time {
	if cmd == nil || cmd.app == nil {
		return nil
	}
	cmd.app.cmdMutex.Lock()
	defer cmd.app.cmdMutex.Unlock()
	resource, ok := cmd.app.resources[reflect.TypeOf(Time{})]
	if !ok {
		return nil
	}
	time, _ := resource.(*Time)
	return time
}
