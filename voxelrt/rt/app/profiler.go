package app

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type profilerScope struct {
	name  string
	start time.Time
}

type Profiler struct {
	Counts     map[string]int
	ScopeTimes map[string]time.Duration
	scopeStack []profilerScope
}

func NewProfiler() *Profiler {
	return &Profiler{
		Counts:     make(map[string]int),
		ScopeTimes: make(map[string]time.Duration),
		scopeStack: make([]profilerScope, 0, 16),
	}
}

func (p *Profiler) BeginScope(name string) {
	if p == nil || name == "" {
		return
	}
	p.scopeStack = append(p.scopeStack, profilerScope{
		name:  name,
		start: time.Now(),
	})
}

func (p *Profiler) EndScope(name string) {
	if p == nil || name == "" || len(p.scopeStack) == 0 {
		return
	}

	for i := len(p.scopeStack) - 1; i >= 0; i-- {
		scope := p.scopeStack[i]
		if scope.name != name {
			continue
		}
		p.ScopeTimes[name] += time.Since(scope.start)
		copy(p.scopeStack[i:], p.scopeStack[i+1:])
		p.scopeStack = p.scopeStack[:len(p.scopeStack)-1]
		return
	}
}

func (p *Profiler) SetCount(name string, count int) {
	p.Counts[name] = count
}

func (p *Profiler) Reset() {
	if p == nil {
		return
	}
	for name := range p.ScopeTimes {
		p.ScopeTimes[name] = 0
	}
	p.scopeStack = p.scopeStack[:0]
}

func (p *Profiler) GetStatsString() string {
	var sb strings.Builder

	sb.WriteString("Stats:\n")
	if len(p.Counts) > 0 {
		sb.WriteString("Counts:\n")
		keys := make([]string, 0, len(p.Counts))
		for k := range p.Counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString(fmt.Sprintf("  %-15s: %d\n", k, p.Counts[k]))
		}
	}

	if len(p.ScopeTimes) > 0 {
		sb.WriteString("CPU Timings (ms):\n")
		keys := make([]string, 0, len(p.ScopeTimes))
		for k := range p.ScopeTimes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString(fmt.Sprintf("  %-15s: %.3f\n", k, p.ScopeTimes[k].Seconds()*1000.0))
		}
	}

	return sb.String()
}
