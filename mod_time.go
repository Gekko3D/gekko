package gekko

import (
	"time"
)

type Time struct {
	Time     time.Time
	Duration time.Duration
	Dt       float64
}

type TimeModule struct {
}

func (mod TimeModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(timeSystem).
			InStage(Prelude).
			RunAlways(),
	)

	cmd.AddResources(&Time{
		Time: time.Now(),
		Dt:   0,
	})
}

func timeSystem(timeResource *Time) {
	now := time.Now()

	timeResource.Duration = now.Sub(timeResource.Time)
	timeResource.Dt = timeResource.Duration.Seconds()
	timeResource.Time = now
}
