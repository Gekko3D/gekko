package gekko

import (
	"time"
)

type Time struct {
	Time     time.Time
	Duration time.Duration
	Dt             float64
	Alpha          float32
	FixedStepCount int
}

type TimeModule struct {
}

func (mod TimeModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(&Time{
		Time: time.Now(),
		Dt:   0,
	})
}
