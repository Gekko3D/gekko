package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cogentcore/webgpu/wgpu"
)

// RenderGraph stores graph node declarations and compiles them into a stable
// dependency order.
type RenderGraph struct {
	specs    []RenderNodeSpec
	compiled []RenderNodeSpec
	dirty    bool
}

func NewRenderGraph() *RenderGraph {
	return &RenderGraph{dirty: true}
}

func (g *RenderGraph) Register(spec RenderNodeSpec) {
	if g == nil {
		return
	}
	g.specs = append(g.specs, spec)
	g.dirty = true
}

func (g *RenderGraph) Specs() []RenderNodeSpec {
	if g == nil {
		return nil
	}
	out := make([]RenderNodeSpec, len(g.specs))
	copy(out, g.specs)
	return out
}

func (g *RenderGraph) Compile() ([]RenderNodeSpec, error) {
	if g == nil {
		return nil, nil
	}
	if !g.dirty {
		out := make([]RenderNodeSpec, len(g.compiled))
		copy(out, g.compiled)
		return out, nil
	}

	byName := make(map[string]RenderNodeSpec, len(g.specs))
	registrationOrder := make([]string, 0, len(g.specs))
	for i, spec := range g.specs {
		if spec.Name == "" {
			return nil, fmt.Errorf("render graph node at index %d has empty name", i)
		}
		if spec.Node == nil {
			return nil, fmt.Errorf("render graph node %q has nil implementation", spec.Name)
		}
		if _, exists := byName[spec.Name]; exists {
			return nil, fmt.Errorf("render graph node %q registered more than once", spec.Name)
		}
		byName[spec.Name] = spec
		registrationOrder = append(registrationOrder, spec.Name)
	}

	indegree := make(map[string]int, len(g.specs))
	dependents := make(map[string][]string, len(g.specs))
	for _, name := range registrationOrder {
		indegree[name] = 0
	}
	for _, spec := range g.specs {
		for _, dep := range spec.After {
			if dep == "" {
				return nil, fmt.Errorf("render graph node %q has empty dependency", spec.Name)
			}
			if _, exists := byName[dep]; !exists {
				return nil, fmt.Errorf("render graph node %q depends on missing node %q", spec.Name, dep)
			}
			indegree[spec.Name]++
			dependents[dep] = append(dependents[dep], spec.Name)
		}
	}

	ready := make([]string, 0, len(g.specs))
	for _, name := range registrationOrder {
		if indegree[name] == 0 {
			ready = append(ready, name)
		}
	}

	ordered := make([]RenderNodeSpec, 0, len(g.specs))
	for len(ready) > 0 {
		name := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byName[name])
		for _, dependent := range dependents[name] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}

	if len(ordered) != len(g.specs) {
		remaining := make([]string, 0)
		for _, name := range registrationOrder {
			if indegree[name] > 0 {
				remaining = append(remaining, name)
			}
		}
		sort.Strings(remaining)
		return nil, fmt.Errorf("render graph has dependency cycle involving: %s", strings.Join(remaining, ", "))
	}

	g.compiled = ordered
	g.dirty = false
	out := make([]RenderNodeSpec, len(g.compiled))
	copy(out, g.compiled)
	return out, nil
}

func (g *RenderGraph) Record(a *App, encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	ordered, err := g.Compile()
	if err != nil {
		return err
	}
	for _, spec := range ordered {
		if spec.Node == nil || !spec.Node.Enabled(a) {
			continue
		}
		if err := spec.Node.Record(a, encoder, frame); err != nil {
			return fmt.Errorf("render graph node %q record failed: %w", spec.Name, err)
		}
	}
	return nil
}

func (g *RenderGraph) Setup(a *App) error {
	ordered, err := g.Compile()
	if err != nil {
		return err
	}
	for _, spec := range ordered {
		if spec.Node == nil || !spec.Node.Enabled(a) {
			continue
		}
		if err := spec.Node.Setup(a); err != nil {
			return fmt.Errorf("render graph node %q setup failed: %w", spec.Name, err)
		}
	}
	return nil
}

func (g *RenderGraph) Resize(a *App, width, height uint32) error {
	ordered, err := g.Compile()
	if err != nil {
		return err
	}
	for _, spec := range ordered {
		if spec.Node == nil || !spec.Node.Enabled(a) {
			continue
		}
		if err := spec.Node.Resize(a, width, height); err != nil {
			return fmt.Errorf("render graph node %q resize failed: %w", spec.Name, err)
		}
	}
	return nil
}

func (g *RenderGraph) OnSceneBuffersRecreated(a *App) error {
	ordered, err := g.Compile()
	if err != nil {
		return err
	}
	for _, spec := range ordered {
		if spec.Node == nil || !spec.Node.Enabled(a) {
			continue
		}
		if err := spec.Node.OnSceneBuffersRecreated(a); err != nil {
			return fmt.Errorf("render graph node %q scene-buffer recreation failed: %w", spec.Name, err)
		}
	}
	return nil
}

func (g *RenderGraph) Update(a *App) error {
	ordered, err := g.Compile()
	if err != nil {
		return err
	}
	for _, spec := range ordered {
		if spec.Node == nil || !spec.Node.Enabled(a) {
			continue
		}
		if err := spec.Node.Update(a); err != nil {
			return fmt.Errorf("render graph node %q update failed: %w", spec.Name, err)
		}
	}
	return nil
}

func (g *RenderGraph) Shutdown(a *App) {
	ordered, err := g.Compile()
	if err != nil {
		return
	}
	for i := len(ordered) - 1; i >= 0; i-- {
		spec := ordered[i]
		if spec.Node == nil {
			continue
		}
		spec.Node.Shutdown(a)
	}
}

func (g *RenderGraph) RecordNode(name string, a *App, encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	if name == "" {
		return fmt.Errorf("render graph node name is empty")
	}
	ordered, err := g.Compile()
	if err != nil {
		return err
	}
	for _, spec := range ordered {
		if spec.Name != name {
			continue
		}
		if spec.Node == nil || !spec.Node.Enabled(a) {
			return nil
		}
		if err := spec.Node.Record(a, encoder, frame); err != nil {
			return fmt.Errorf("render graph node %q record failed: %w", spec.Name, err)
		}
		return nil
	}
	return fmt.Errorf("render graph node %q is not registered", name)
}
