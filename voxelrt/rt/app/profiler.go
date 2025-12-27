package app

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Profiler struct {
	Scopes     map[string]time.Duration
	StartTimes map[string]time.Time
	Counts     map[string]int
	Order      []string
}

func NewProfiler() *Profiler {
	return &Profiler{
		Scopes:     make(map[string]time.Duration),
		StartTimes: make(map[string]time.Time),
		Counts:     make(map[string]int),
		Order:      make([]string, 0),
	}
}

func (p *Profiler) BeginScope(name string) {
	p.StartTimes[name] = time.Now()
	// Maintain insertion order for consistent display if not already present
	found := false
	for _, n := range p.Order {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		p.Order = append(p.Order, name)
	}
}

func (p *Profiler) EndScope(name string) {
	if start, ok := p.StartTimes[name]; ok {
		p.Scopes[name] = time.Since(start)
	}
}

func (p *Profiler) SetCount(name string, count int) {
	p.Counts[name] = count
}

func (p *Profiler) Reset() {
	// Keep Order, reset times
	for k := range p.Scopes {
		p.Scopes[k] = 0
	}
}

func (p *Profiler) GetStatsString() string {
	var sb strings.Builder

	// 1. Timers
	sb.WriteString("Timings (CPU):\n")
	for _, name := range p.Order {
		dur := p.Scopes[name]
		ms := float64(dur.Microseconds()) / 1000.0
		sb.WriteString(fmt.Sprintf("  %-15s: %.2f ms\n", name, ms))
	}

	// 2. Counters
	sb.WriteString("\nStats:\n")
	// Sort counts keys
	keys := make([]string, 0, len(p.Counts))
	for k := range p.Counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("  %-15s: %d\n", k, p.Counts[k]))
	}

	return sb.String()
}
