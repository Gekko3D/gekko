package gekko

import (
	"fmt"
)

// LifetimeComponent allows an entity to automatically be removed after a set duration.
type LifetimeComponent struct {
	TimeLeft float32
}

type LifecycleModule struct{}

func (mod LifecycleModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(lifetimeSystem).
			InStage(PostUpdate).
			RunAlways(),
	)
}

func lifetimeSystem(time *Time, cmd *Commands) {
	dt := float32(time.Dt)
	if dt <= 0 {
		return
	}
	MakeQuery1[LifetimeComponent](cmd).Map(func(eid EntityId, lt *LifetimeComponent) bool {
		lt.TimeLeft -= dt
		if lt.TimeLeft <= 0 {
			fmt.Printf("ENGINE: Lifecycle marking entity %v for removal\n", eid)
			cmd.RemoveEntity(eid)
		}
		return true
	})
}
