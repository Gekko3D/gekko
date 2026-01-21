package gekko

import (
	"time"
)

type Time struct {
	Time       time.Time
	Duration   time.Duration
	Dt         float64
	FrameCount uint64
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

	dur := now.Sub(timeResource.Time)
	dt := dur.Seconds()
	// Clamp dt to 10fps minimum to prevent physics from exploding during hitches/startup
	if dt > 0.1 {
		dt = 0.1
	}

	timeResource.Duration = dur
	timeResource.Dt = dt
	timeResource.Time = now
	timeResource.FrameCount++
}
