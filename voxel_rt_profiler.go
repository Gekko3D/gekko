package gekko

import "time"

type Profiler struct {
	NavBakeTime   time.Duration
	EditTime      time.Duration
	StreamingTime time.Duration
	AABBTime      time.Duration
	RenderTime    time.Duration
}

func (p *Profiler) Reset() {
	p.NavBakeTime = 0
	p.EditTime = 0
	p.StreamingTime = 0
	p.AABBTime = 0
	p.RenderTime = 0
}