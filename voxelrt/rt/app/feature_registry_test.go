package app

import (
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
)

type testFeature struct {
	name            string
	enabled         bool
	hasCommandStage map[FeatureCommandStage]bool
	hasPassStage    map[FeaturePassStage]bool
	hasScreenStage  map[FeatureScreenStage]bool
	calls           *[]string
}

func (f *testFeature) Name() string {
	return f.name
}

func (f *testFeature) Enabled(*App) bool {
	return f.enabled
}

func (f *testFeature) Setup(*App) error {
	if f.calls != nil {
		*f.calls = append(*f.calls, f.name+":setup")
	}
	return nil
}

func (f *testFeature) Resize(*App, uint32, uint32) error {
	return nil
}

func (f *testFeature) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (f *testFeature) Update(*App) error {
	return nil
}

func (f *testFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	if f.calls != nil {
		*f.calls = append(*f.calls, f.name+":render")
	}
	return nil
}

func (f *testFeature) Shutdown(*App) {}

func (f *testFeature) DispatchCommandStage(*App, FeatureCommandStage, *wgpu.CommandEncoder) error {
	if f.calls != nil {
		*f.calls = append(*f.calls, f.name+":command")
	}
	return nil
}

func (f *testFeature) RenderPassStage(*App, FeaturePassStage, *wgpu.RenderPassEncoder) error {
	if f.calls != nil {
		*f.calls = append(*f.calls, f.name+":pass")
	}
	return nil
}

func (f *testFeature) HasCommandStage(_ *App, stage FeatureCommandStage) bool {
	return f.hasCommandStage[stage]
}

func (f *testFeature) HasPassStage(_ *App, stage FeaturePassStage) bool {
	return f.hasPassStage[stage]
}

func (f *testFeature) HasScreenStage(_ *App, stage FeatureScreenStage) bool {
	return f.hasScreenStage[stage]
}

func TestEnsureDefaultFeaturesRespectsConfig(t *testing.T) {
	app := &App{FeatureConfig: DefaultFeatureConfig()}
	app.ensureDefaultFeatures()

	if !app.defaultFeaturesRegistered {
		t.Fatal("expected default features to be marked registered")
	}
	if len(app.features) == 0 {
		t.Fatal("expected default features to be registered")
	}

	noDefaults := &App{FeatureConfig: AppFeatureConfig{AutoRegisterDefaults: false}}
	noDefaults.ensureDefaultFeatures()
	if len(noDefaults.features) != 0 {
		t.Fatalf("expected no default features, got %d", len(noDefaults.features))
	}
}

func TestEnsureDefaultFeaturesPrependsCustomRegistrations(t *testing.T) {
	custom := &testFeature{name: "custom", enabled: true}
	app := &App{FeatureConfig: DefaultFeatureConfig()}
	app.RegisterFeature(custom)
	app.ensureDefaultFeatures()

	if len(app.features) < 2 {
		t.Fatalf("expected defaults plus custom feature, got %d entries", len(app.features))
	}
	if got := app.features[len(app.features)-1].Name(); got != "custom" {
		t.Fatalf("expected custom feature last after defaults, got %q", got)
	}
}

func TestFeatureStageQueriesRespectEnabledState(t *testing.T) {
	active := &testFeature{
		name:            "active",
		enabled:         true,
		hasCommandStage: map[FeatureCommandStage]bool{FeatureCommandStagePostLighting: true},
		hasPassStage:    map[FeaturePassStage]bool{FeaturePassStageAccumulation: true},
		hasScreenStage:  map[FeatureScreenStage]bool{FeatureScreenStagePostResolve: true},
	}
	disabled := &testFeature{
		name:            "disabled",
		enabled:         false,
		hasCommandStage: map[FeatureCommandStage]bool{FeatureCommandStagePostLighting: true},
		hasPassStage:    map[FeaturePassStage]bool{FeaturePassStageAccumulation: true},
		hasScreenStage:  map[FeatureScreenStage]bool{FeatureScreenStagePostResolve: true},
	}
	app := &App{features: []Feature{disabled, active}}

	if !app.hasCommandStageWork(FeatureCommandStagePostLighting) {
		t.Fatal("expected enabled feature to activate post-lighting stage")
	}
	if !app.hasPassStageWork(FeaturePassStageAccumulation) {
		t.Fatal("expected enabled feature to activate accumulation stage")
	}
	if !app.hasScreenStageWork(FeatureScreenStagePostResolve) {
		t.Fatal("expected enabled feature to activate post-resolve stage")
	}
}

func TestFeatureLifecycleDispatchSkipsDisabledFeatures(t *testing.T) {
	var calls []string
	disabled := &testFeature{name: "disabled", enabled: false, calls: &calls}
	active := &testFeature{name: "active", enabled: true, calls: &calls}
	app := &App{features: []Feature{disabled, active}}

	if err := app.setupFeatures(); err != nil {
		t.Fatalf("setupFeatures returned error: %v", err)
	}
	if err := app.dispatchCommandStage(FeatureCommandStagePreGBuffer, nil); err != nil {
		t.Fatalf("dispatchCommandStage returned error: %v", err)
	}
	if err := app.renderPassStage(FeaturePassStageAccumulation, nil); err != nil {
		t.Fatalf("renderPassStage returned error: %v", err)
	}
	if err := app.renderScreenStage(FeatureScreenStagePostResolve, nil, nil); err != nil {
		t.Fatalf("renderScreenStage returned error: %v", err)
	}

	expected := []string{
		"active:setup",
		"active:command",
		"active:pass",
		"active:render",
	}
	if len(calls) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(calls), calls)
	}
	for i, want := range expected {
		if calls[i] != want {
			t.Fatalf("call %d = %q, want %q", i, calls[i], want)
		}
	}
}
