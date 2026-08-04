package gekko

import (
	"time"
)

type Time struct {
	Time           time.Time
	Duration       time.Duration
	Dt             float64
	Elapsed        float64
	Alpha          float32
	FixedStepCount int
}

// FrameProfile contains the previous frame's measured work when slow-frame
// profiling is enabled. Categories is reused in place each frame.
type FrameProfile struct {
	Work       time.Duration
	RawDelta   time.Duration
	Categories map[string]time.Duration
}

type TimeModule struct {
}

func (mod TimeModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(&Time{
		Time: time.Now(),
		Dt:   0,
	}, &FrameProfile{Categories: make(map[string]time.Duration)})
}
