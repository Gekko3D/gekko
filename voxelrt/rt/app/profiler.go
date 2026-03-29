package app

import (
	"fmt"
	"sort"
	"strings"
)

type Profiler struct {
	Counts map[string]int
}

func NewProfiler() *Profiler {
	return &Profiler{
		Counts: make(map[string]int),
	}
}

func (p *Profiler) BeginScope(name string) {}

func (p *Profiler) EndScope(name string) {}

func (p *Profiler) SetCount(name string, count int) {
	p.Counts[name] = count
}

func (p *Profiler) Reset() {}

func (p *Profiler) GetStatsString() string {
	var sb strings.Builder

	sb.WriteString("Stats:\n")
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
