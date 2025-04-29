package gekko

import (
	"time"
)

type Time struct {
	Time time.Time
	Dt   time.Duration
}

type TimeModule struct {
}

func (mod TimeModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(&Time{
		Time: time.Now(),
		Dt:   0,
	})
	cmd.UseSystem(timeSystem)
}

func timeSystem(timeResource *Time) {
	now := time.Now()

	timeResource.Dt = now.Sub(timeResource.Time)
	timeResource.Time = now
}
