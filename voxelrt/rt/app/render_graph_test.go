package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
)

type testRenderNode struct {
	name    string
	enabled bool
	calls   *[]string
}

func (n *testRenderNode) Name() string {
	return n.name
}

func (n *testRenderNode) Enabled(*App) bool {
	return n.enabled
}

func (n *testRenderNode) Setup(*App) error {
	return nil
}

func (n *testRenderNode) Resize(*App, uint32, uint32) error {
	return nil
}

func (n *testRenderNode) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (n *testRenderNode) Update(*App) error {
	return nil
}

func (n *testRenderNode) Record(*App, *wgpu.CommandEncoder, *FrameContext) error {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name)
	}
	return nil
}

func (n *testRenderNode) Shutdown(*App) {}

type lifecycleTestRenderNode struct {
	name        string
	enabled     bool
	calls       *[]string
	setupErr    error
	resizeErr   error
	recreateErr error
	updateErr   error
}

func (n *lifecycleTestRenderNode) Name() string {
	return n.name
}

func (n *lifecycleTestRenderNode) Enabled(*App) bool {
	return n.enabled
}

func (n *lifecycleTestRenderNode) Setup(*App) error {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name+":setup")
	}
	return n.setupErr
}

func (n *lifecycleTestRenderNode) Resize(*App, uint32, uint32) error {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name+":resize")
	}
	return n.resizeErr
}

func (n *lifecycleTestRenderNode) OnSceneBuffersRecreated(*App) error {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name+":recreate")
	}
	return n.recreateErr
}

func (n *lifecycleTestRenderNode) Update(*App) error {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name+":update")
	}
	return n.updateErr
}

func (n *lifecycleTestRenderNode) Record(*App, *wgpu.CommandEncoder, *FrameContext) error {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name+":record")
	}
	return nil
}

func (n *lifecycleTestRenderNode) Shutdown(*App) {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name+":shutdown")
	}
}

func TestRenderGraphCompileOrdersDependencies(t *testing.T) {
	graph := NewRenderGraph()
	graph.Register(RenderNodeSpec{Name: "lighting", After: []string{"gbuffer", "shadows"}, Node: &testRenderNode{name: "lighting", enabled: true}})
	graph.Register(RenderNodeSpec{Name: "gbuffer", Node: &testRenderNode{name: "gbuffer", enabled: true}})
	graph.Register(RenderNodeSpec{Name: "shadows", After: []string{"gbuffer"}, Node: &testRenderNode{name: "shadows", enabled: true}})

	ordered, err := graph.Compile()
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	got := renderNodeNames(ordered)
	want := []string{"gbuffer", "shadows", "lighting"}
	if !sameStrings(got, want) {
		t.Fatalf("compiled order = %v, want %v", got, want)
	}
}

func TestRenderGraphCompileRejectsDuplicateNames(t *testing.T) {
	graph := NewRenderGraph()
	graph.Register(RenderNodeSpec{Name: "gbuffer", Node: &testRenderNode{name: "a", enabled: true}})
	graph.Register(RenderNodeSpec{Name: "gbuffer", Node: &testRenderNode{name: "b", enabled: true}})

	_, err := graph.Compile()
	if err == nil || !strings.Contains(err.Error(), "registered more than once") {
		t.Fatalf("expected duplicate-name error, got %v", err)
	}
}

func TestRenderGraphCompileRejectsMissingDependencies(t *testing.T) {
	graph := NewRenderGraph()
	graph.Register(RenderNodeSpec{Name: "lighting", After: []string{"gbuffer"}, Node: &testRenderNode{name: "lighting", enabled: true}})

	_, err := graph.Compile()
	if err == nil || !strings.Contains(err.Error(), "depends on missing node") {
		t.Fatalf("expected missing-dependency error, got %v", err)
	}
}

func TestRenderGraphCompileRejectsCycles(t *testing.T) {
	graph := NewRenderGraph()
	graph.Register(RenderNodeSpec{Name: "a", After: []string{"b"}, Node: &testRenderNode{name: "a", enabled: true}})
	graph.Register(RenderNodeSpec{Name: "b", After: []string{"a"}, Node: &testRenderNode{name: "b", enabled: true}})

	_, err := graph.Compile()
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestRenderGraphRecordSkipsDisabledNodes(t *testing.T) {
	var calls []string
	graph := NewRenderGraph()
	graph.Register(RenderNodeSpec{Name: "gbuffer", Node: &testRenderNode{name: "gbuffer", enabled: true, calls: &calls}})
	graph.Register(RenderNodeSpec{Name: "debug", After: []string{"gbuffer"}, Node: &testRenderNode{name: "debug", enabled: false, calls: &calls}})
	graph.Register(RenderNodeSpec{Name: "resolve", After: []string{"debug"}, Node: &testRenderNode{name: "resolve", enabled: true, calls: &calls}})

	if err := graph.Record(nil, nil, nil); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	want := []string{"gbuffer", "resolve"}
	if !sameStrings(calls, want) {
		t.Fatalf("record calls = %v, want %v", calls, want)
	}
}

func TestRenderGraphRecordNodeRunsOnlyRequestedNode(t *testing.T) {
	var calls []string
	graph := NewRenderGraph()
	graph.Register(RenderNodeSpec{Name: "gbuffer", Node: &testRenderNode{name: "gbuffer", enabled: true, calls: &calls}})
	graph.Register(RenderNodeSpec{Name: "lighting", After: []string{"gbuffer"}, Node: &testRenderNode{name: "lighting", enabled: true, calls: &calls}})
	graph.Register(RenderNodeSpec{Name: "resolve", After: []string{"lighting"}, Node: &testRenderNode{name: "resolve", enabled: true, calls: &calls}})

	if err := graph.RecordNode("lighting", nil, nil, nil); err != nil {
		t.Fatalf("RecordNode returned error: %v", err)
	}
	want := []string{"lighting"}
	if !sameStrings(calls, want) {
		t.Fatalf("record calls = %v, want %v", calls, want)
	}
}

func TestRenderGraphRecordNodeRejectsMissingNode(t *testing.T) {
	graph := NewRenderGraph()
	graph.Register(RenderNodeSpec{Name: "gbuffer", Node: &testRenderNode{name: "gbuffer", enabled: true}})

	err := graph.RecordNode("lighting", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "is not registered") {
		t.Fatalf("expected missing-node error, got %v", err)
	}
}

func TestRenderGraphLifecycleSkipsDisabledNodesInDependencyOrder(t *testing.T) {
	var calls []string
	graph := NewRenderGraph()
	graph.Register(RenderNodeSpec{Name: "a", Node: &lifecycleTestRenderNode{name: "a", enabled: true, calls: &calls}})
	graph.Register(RenderNodeSpec{Name: "b", After: []string{"a"}, Node: &lifecycleTestRenderNode{name: "b", enabled: false, calls: &calls}})
	graph.Register(RenderNodeSpec{Name: "c", After: []string{"b"}, Node: &lifecycleTestRenderNode{name: "c", enabled: true, calls: &calls}})

	if err := graph.Setup(nil); err != nil {
		t.Fatalf("Setup returned error: %v", err)
	}
	if err := graph.Resize(nil, 640, 480); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}
	if err := graph.OnSceneBuffersRecreated(nil); err != nil {
		t.Fatalf("OnSceneBuffersRecreated returned error: %v", err)
	}
	if err := graph.Update(nil); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	want := []string{
		"a:setup",
		"c:setup",
		"a:resize",
		"c:resize",
		"a:recreate",
		"c:recreate",
		"a:update",
		"c:update",
	}
	if !sameStrings(calls, want) {
		t.Fatalf("lifecycle calls = %v, want %v", calls, want)
	}
}

func TestRenderGraphLifecycleWrapsNodeErrors(t *testing.T) {
	graph := NewRenderGraph()
	graph.Register(RenderNodeSpec{
		Name: "bad",
		Node: &lifecycleTestRenderNode{
			name:     "bad",
			enabled:  true,
			setupErr: errors.New("boom"),
		},
	})

	err := graph.Setup(nil)
	if err == nil || !strings.Contains(err.Error(), `render graph node "bad" setup failed`) {
		t.Fatalf("expected wrapped setup error, got %v", err)
	}
}

func TestRenderGraphShutdownRunsReverseDependencyOrder(t *testing.T) {
	var calls []string
	graph := NewRenderGraph()
	graph.Register(RenderNodeSpec{Name: "a", Node: &lifecycleTestRenderNode{name: "a", enabled: true, calls: &calls}})
	graph.Register(RenderNodeSpec{Name: "b", After: []string{"a"}, Node: &lifecycleTestRenderNode{name: "b", enabled: false, calls: &calls}})
	graph.Register(RenderNodeSpec{Name: "c", After: []string{"b"}, Node: &lifecycleTestRenderNode{name: "c", enabled: true, calls: &calls}})

	graph.Shutdown(nil)

	want := []string{"c:shutdown", "b:shutdown", "a:shutdown"}
	if !sameStrings(calls, want) {
		t.Fatalf("shutdown calls = %v, want %v", calls, want)
	}
}

func renderNodeNames(specs []RenderNodeSpec) []string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
