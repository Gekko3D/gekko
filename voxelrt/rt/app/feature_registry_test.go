package app

import (
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
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

type graphOwnedTestFeature struct {
	*testFeature
	nodes        []string
	commandStage []FeatureCommandStage
	passStage    []FeaturePassStage
	screenStage  []FeatureScreenStage
}

func (f *graphOwnedTestFeature) GraphNodeNames() []string {
	return f.nodes
}

func (f *graphOwnedTestFeature) GraphCommandStages() []FeatureCommandStage {
	return f.commandStage
}

func (f *graphOwnedTestFeature) GraphPassStages() []FeaturePassStage {
	return f.passStage
}

func (f *graphOwnedTestFeature) GraphScreenStages() []FeatureScreenStage {
	return f.screenStage
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
	foundWater := false
	foundAstronomical := false
	foundFarPlanetRing := false
	foundDebrisMidfield := false
	foundPlanetBodies := false
	foundAnalyticMedia := false
	astronomicalIndex := -1
	farPlanetRingIndex := -1
	debrisMidfieldIndex := -1
	planetBodiesIndex := -1
	analyticMediaIndex := -1
	for _, feature := range app.features {
		if feature != nil && feature.Name() == "water" {
			foundWater = true
		}
		if feature != nil && feature.Name() == "astronomical" {
			foundAstronomical = true
			astronomicalIndex = indexOfFeature(app.features, feature.Name())
		}
		if feature != nil && feature.Name() == "far_planet_ring" {
			foundFarPlanetRing = true
			farPlanetRingIndex = indexOfFeature(app.features, feature.Name())
		}
		if feature != nil && feature.Name() == "debris_midfield" {
			foundDebrisMidfield = true
			debrisMidfieldIndex = indexOfFeature(app.features, feature.Name())
		}
		if feature != nil && feature.Name() == "planet-bodies" {
			foundPlanetBodies = true
			planetBodiesIndex = indexOfFeature(app.features, feature.Name())
		}
		if feature != nil && feature.Name() == "analytic-media" {
			foundAnalyticMedia = true
			analyticMediaIndex = indexOfFeature(app.features, feature.Name())
		}
	}
	if !foundWater {
		t.Fatal("expected water feature to be registered by default")
	}
	if !foundPlanetBodies {
		t.Fatal("expected planet body feature to be registered by default")
	}
	if !foundAstronomical {
		t.Fatal("expected astronomical feature to be registered by default")
	}
	if !foundFarPlanetRing {
		t.Fatal("expected far planet-ring feature to be registered by default")
	}
	if !foundDebrisMidfield {
		t.Fatal("expected debris-midfield feature to be registered by default")
	}
	if !foundAnalyticMedia {
		t.Fatal("expected analytic media feature to be registered by default")
	}
	if astronomicalIndex < 0 || planetBodiesIndex < 0 || astronomicalIndex > planetBodiesIndex {
		t.Fatalf("expected astronomical feature before planet bodies, got astronomical=%d planet-bodies=%d", astronomicalIndex, planetBodiesIndex)
	}
	if astronomicalIndex < 0 || farPlanetRingIndex < 0 || astronomicalIndex > farPlanetRingIndex {
		t.Fatalf("expected far planet-ring feature after astronomical feature, got astronomical=%d far-ring=%d", astronomicalIndex, farPlanetRingIndex)
	}
	if planetBodiesIndex < 0 || farPlanetRingIndex < 0 || planetBodiesIndex > farPlanetRingIndex {
		t.Fatalf("expected far planet-ring feature after planet bodies for correct composition, got planet-bodies=%d far-ring=%d", planetBodiesIndex, farPlanetRingIndex)
	}
	if farPlanetRingIndex < 0 || debrisMidfieldIndex < 0 || farPlanetRingIndex > debrisMidfieldIndex {
		t.Fatalf("expected debris-midfield feature after far planet-ring feature, got far-ring=%d debris-midfield=%d", farPlanetRingIndex, debrisMidfieldIndex)
	}
	if planetBodiesIndex < 0 || analyticMediaIndex < 0 || planetBodiesIndex > analyticMediaIndex {
		t.Fatalf("expected planet bodies before analytic media, got planet-bodies=%d analytic-media=%d", planetBodiesIndex, analyticMediaIndex)
	}

	noDefaults := &App{FeatureConfig: AppFeatureConfig{AutoRegisterDefaults: false}}
	noDefaults.ensureDefaultFeatures()
	if len(noDefaults.features) != 0 {
		t.Fatalf("expected no default features, got %d", len(noDefaults.features))
	}
}

func TestFarPlanetRingFeaturePassStageGating(t *testing.T) {
	feature := &FarPlanetRingFeature{}
	app := &App{}
	if feature.HasCommandStage(app, FeatureCommandStagePostLighting) {
		t.Fatal("expected far planet-ring feature to no longer use post-lighting command stage")
	}
	if feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected far planet-ring feature to gate off without renderer resources")
	}
	app.BufferManager = nil
	if feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected far planet-ring feature to gate off without buffer manager")
	}

	app = &App{
		FarPlanetRingResources: &FarPlanetRingResources{Pipeline: &wgpu.RenderPipeline{}},
		BufferManager:          testFarPlanetRingReadyManager(),
	}
	if !feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected far planet-ring feature to gate on with resources and contribution")
	}
}

func testFarPlanetRingReadyManager() *gpu.GpuBufferManager {
	return &gpu.GpuBufferManager{
		DepthView:             &wgpu.TextureView{},
		PlanetDepthView:       &wgpu.TextureView{},
		TransparentAccumView:  &wgpu.TextureView{},
		TransparentWeightView: &wgpu.TextureView{},
		FarPlanetRingCount:    1,
		FarPlanetRingBG0:      &wgpu.BindGroup{},
		FarPlanetRingBG1:      &wgpu.BindGroup{},
		FarPlanetRingBG2:      &wgpu.BindGroup{},
	}
}

func TestDebrisMidfieldFeaturePassStageGating(t *testing.T) {
	feature := &DebrisMidfieldFeature{}
	app := &App{}
	if feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected debris-midfield feature to gate off without renderer resources")
	}
	app.BufferManager = nil
	if feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected debris-midfield feature to gate off without buffer manager")
	}

	app = &App{
		DebrisMidfieldResources: &DebrisMidfieldResources{Pipeline: &wgpu.RenderPipeline{}},
		BufferManager:           testDebrisMidfieldReadyManager(),
	}
	if !feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected debris-midfield feature to gate on with resources and contribution")
	}
}

func testDebrisMidfieldReadyManager() *gpu.GpuBufferManager {
	return &gpu.GpuBufferManager{
		DepthView:           &wgpu.TextureView{},
		PlanetDepthView:     &wgpu.TextureView{},
		DebrisMidfieldCount: 1,
		DebrisMidfieldBG0:   &wgpu.BindGroup{},
		DebrisMidfieldBG1:   &wgpu.BindGroup{},
		DebrisMidfieldBG2:   &wgpu.BindGroup{},
	}
}

func indexOfFeature(features []Feature, name string) int {
	for i, feature := range features {
		if feature != nil && feature.Name() == name {
			return i
		}
	}
	return -1
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

func TestHasFeatureRespectsEnabledState(t *testing.T) {
	active := &testFeature{name: "active", enabled: true}
	disabled := &testFeature{name: "disabled", enabled: false}
	app := &App{features: []Feature{disabled, active}}

	if !app.HasFeature("active") {
		t.Fatal("expected enabled feature to be discoverable")
	}
	if app.HasFeature("disabled") {
		t.Fatal("expected disabled feature to be hidden")
	}
	if app.HasFeature("missing") {
		t.Fatal("expected missing feature to be hidden")
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

func TestFeatureStageCompatibilitySkipsGraphOwnedFeatures(t *testing.T) {
	var calls []string
	graphOwned := &graphOwnedTestFeature{
		testFeature: &testFeature{
			name:            "graph-owned",
			enabled:         true,
			hasCommandStage: map[FeatureCommandStage]bool{FeatureCommandStagePostLighting: true},
			hasPassStage:    map[FeaturePassStage]bool{FeaturePassStageAccumulation: true},
			hasScreenStage:  map[FeatureScreenStage]bool{FeatureScreenStagePostResolve: true},
			calls:           &calls,
		},
		nodes:        []string{"feature-graph-owned"},
		commandStage: []FeatureCommandStage{FeatureCommandStagePostLighting},
		passStage:    []FeaturePassStage{FeaturePassStageAccumulation},
		screenStage:  []FeatureScreenStage{FeatureScreenStagePostResolve},
	}
	legacy := &testFeature{
		name:            "legacy",
		enabled:         true,
		hasCommandStage: map[FeatureCommandStage]bool{FeatureCommandStagePostLighting: true},
		hasPassStage:    map[FeaturePassStage]bool{FeaturePassStageAccumulation: true},
		hasScreenStage:  map[FeatureScreenStage]bool{FeatureScreenStagePostResolve: true},
		calls:           &calls,
	}
	app := &App{features: []Feature{graphOwned, legacy}}

	if !app.hasFeatureGraphNode("feature-graph-owned") {
		t.Fatal("expected graph-owned feature node to be discoverable")
	}
	if err := app.dispatchCommandStage(FeatureCommandStagePostLighting, nil); err != nil {
		t.Fatalf("dispatchCommandStage returned error: %v", err)
	}
	if err := app.renderPassStage(FeaturePassStageAccumulation, nil); err != nil {
		t.Fatalf("renderPassStage returned error: %v", err)
	}
	if err := app.renderScreenStage(FeatureScreenStagePostResolve, nil, nil); err != nil {
		t.Fatalf("renderScreenStage returned error: %v", err)
	}

	expected := []string{
		"legacy:command",
		"legacy:pass",
		"legacy:render",
	}
	if !sameStrings(calls, expected) {
		t.Fatalf("compatibility calls = %v, want %v", calls, expected)
	}
}

func TestRenderGraphPassStageIncludesGraphOwnedPassContributors(t *testing.T) {
	var calls []string
	graphOwned := &graphOwnedTestFeature{
		testFeature: &testFeature{
			name:         "graph-owned",
			enabled:      true,
			hasPassStage: map[FeaturePassStage]bool{FeaturePassStageAccumulation: true},
			calls:        &calls,
		},
		nodes:     []string{RenderNodeCoreAccumulation},
		passStage: []FeaturePassStage{FeaturePassStageAccumulation},
	}
	legacy := &testFeature{
		name:         "legacy",
		enabled:      true,
		hasPassStage: map[FeaturePassStage]bool{FeaturePassStageAccumulation: true},
		calls:        &calls,
	}
	app := &App{features: []Feature{graphOwned, legacy}}

	if !app.hasPassStageWorkForRenderGraph(FeaturePassStageAccumulation) {
		t.Fatal("expected render graph pass dispatch to see graph-owned accumulation work")
	}
	if err := app.renderPassStageForRenderGraph(FeaturePassStageAccumulation, nil); err != nil {
		t.Fatalf("renderPassStageForRenderGraph returned error: %v", err)
	}

	expected := []string{
		"graph-owned:pass",
		"legacy:pass",
	}
	if !sameStrings(calls, expected) {
		t.Fatalf("render graph pass calls = %v, want %v", calls, expected)
	}
}

func TestRenderGraphPassStageSkipsInactiveGraphOwnedContributors(t *testing.T) {
	var calls []string
	graphOwned := &graphOwnedTestFeature{
		testFeature: &testFeature{
			name:         "graph-owned",
			enabled:      true,
			hasPassStage: map[FeaturePassStage]bool{FeaturePassStageAccumulation: false},
			calls:        &calls,
		},
		nodes:     []string{RenderNodeCoreAccumulation},
		passStage: []FeaturePassStage{FeaturePassStageAccumulation},
	}
	legacy := &testFeature{
		name:         "legacy",
		enabled:      true,
		hasPassStage: map[FeaturePassStage]bool{FeaturePassStageAccumulation: true},
		calls:        &calls,
	}
	app := &App{features: []Feature{graphOwned, legacy}}

	if err := app.renderPassStageForRenderGraph(FeaturePassStageAccumulation, nil); err != nil {
		t.Fatalf("renderPassStageForRenderGraph returned error: %v", err)
	}

	expected := []string{"legacy:pass"}
	if !sameStrings(calls, expected) {
		t.Fatalf("render graph pass calls = %v, want %v", calls, expected)
	}
}

func TestGraphOwnedPassContributorCanKeepLegacyCommandStage(t *testing.T) {
	var calls []string
	graphOwnedPass := &graphOwnedTestFeature{
		testFeature: &testFeature{
			name:            "graph-owned-pass",
			enabled:         true,
			hasCommandStage: map[FeatureCommandStage]bool{FeatureCommandStagePreGBuffer: true},
			hasPassStage:    map[FeaturePassStage]bool{FeaturePassStageAccumulation: true},
			calls:           &calls,
		},
		nodes:     []string{RenderNodeCoreAccumulation},
		passStage: []FeaturePassStage{FeaturePassStageAccumulation},
	}
	app := &App{features: []Feature{graphOwnedPass}}

	if !app.hasCommandStageWork(FeatureCommandStagePreGBuffer) {
		t.Fatal("expected legacy command stage to remain active for graph-owned pass feature")
	}
	if app.hasPassStageWork(FeaturePassStageAccumulation) {
		t.Fatal("expected legacy pass-stage query to skip graph-owned pass feature")
	}
	if !app.hasPassStageWorkForRenderGraph(FeaturePassStageAccumulation) {
		t.Fatal("expected render graph pass-stage query to include graph-owned pass feature")
	}
	if err := app.dispatchCommandStage(FeatureCommandStagePreGBuffer, nil); err != nil {
		t.Fatalf("dispatchCommandStage returned error: %v", err)
	}
	if err := app.renderPassStage(FeaturePassStageAccumulation, nil); err != nil {
		t.Fatalf("renderPassStage returned error: %v", err)
	}

	expected := []string{"graph-owned-pass:command"}
	if !sameStrings(calls, expected) {
		t.Fatalf("calls = %v, want %v", calls, expected)
	}
}

func TestSpriteFeatureIsGraphOwnedByAccumulationNode(t *testing.T) {
	feature := &SpriteFeature{}
	nodes := feature.GraphNodeNames()
	if !sameStrings(nodes, []string{RenderNodeCoreAccumulation}) {
		t.Fatalf("sprite graph nodes = %v, want %v", nodes, []string{RenderNodeCoreAccumulation})
	}
}

func TestSpriteFeaturePassStageGating(t *testing.T) {
	feature := &SpriteFeature{}
	app := &App{}
	if feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected sprite feature to gate off without renderer resources")
	}
	app.BufferManager = testSpriteReadyManager()
	if feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected sprite feature to gate off without sprite resources")
	}
	app.SpriteResources = &SpriteResources{Pipeline: &wgpu.RenderPipeline{}}
	if !feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected sprite feature to gate on with resources and contribution")
	}
}

func testSpriteReadyManager() *gpu.GpuBufferManager {
	return &gpu.GpuBufferManager{
		SpriteCount:       1,
		SpritesBindGroup1: &wgpu.BindGroup{},
		SpriteBatches: []gpu.SpriteRenderBatch{
			{
				FirstInstance: 0,
				InstanceCount: 1,
				BindGroup0:    &wgpu.BindGroup{},
			},
		},
	}
}

func TestTransparencyFeatureIsGraphOwnedByAccumulationNode(t *testing.T) {
	feature := &TransparencyFeature{}
	nodes := feature.GraphNodeNames()
	if !sameStrings(nodes, []string{RenderNodeCoreAccumulation}) {
		t.Fatalf("transparency graph nodes = %v, want %v", nodes, []string{RenderNodeCoreAccumulation})
	}
}

func TestWaterFeatureIsGraphOwnedByAccumulationNode(t *testing.T) {
	feature := &WaterFeature{}
	nodes := feature.GraphNodeNames()
	if !sameStrings(nodes, []string{RenderNodeCoreAccumulation}) {
		t.Fatalf("water graph nodes = %v, want %v", nodes, []string{RenderNodeCoreAccumulation})
	}
}

func TestFarPlanetRingFeatureIsGraphOwnedByAccumulationNode(t *testing.T) {
	feature := &FarPlanetRingFeature{}
	nodes := feature.GraphNodeNames()
	if !sameStrings(nodes, []string{RenderNodeCoreAccumulation}) {
		t.Fatalf("far planet-ring graph nodes = %v, want %v", nodes, []string{RenderNodeCoreAccumulation})
	}
}

func TestDebrisMidfieldFeatureIsGraphOwnedByAccumulationNode(t *testing.T) {
	feature := &DebrisMidfieldFeature{}
	nodes := feature.GraphNodeNames()
	if !sameStrings(nodes, []string{RenderNodeCoreAccumulation}) {
		t.Fatalf("debris midfield graph nodes = %v, want %v", nodes, []string{RenderNodeCoreAccumulation})
	}
}

func TestSkyboxFeatureIsGraphOwnedBySkyboxUpdateNode(t *testing.T) {
	feature := &SkyboxFeature{}
	nodes := feature.GraphNodeNames()
	if !sameStrings(nodes, []string{RenderNodeFeatureSkyboxUpdate}) {
		t.Fatalf("skybox graph nodes = %v, want %v", nodes, []string{RenderNodeFeatureSkyboxUpdate})
	}
}

func TestParticlesFeatureIsGraphOwnedByAccumulationNode(t *testing.T) {
	feature := &ParticlesFeature{}
	nodes := feature.GraphNodeNames()
	wantNodes := []string{RenderNodeFeatureParticlesSim, RenderNodeCoreAccumulation}
	if !sameStrings(nodes, wantNodes) {
		t.Fatalf("particles graph nodes = %v, want %v", nodes, wantNodes)
	}
	stages := feature.GraphPassStages()
	if !samePassStages(stages, []FeaturePassStage{FeaturePassStageAccumulation}) {
		t.Fatalf("particles graph pass stages = %v, want %v", stages, []FeaturePassStage{FeaturePassStageAccumulation})
	}
	if !sameCommandStages(feature.GraphCommandStages(), []FeatureCommandStage{FeatureCommandStagePreGBuffer}) {
		t.Fatalf("particles graph command stages = %v, want %v", feature.GraphCommandStages(), []FeatureCommandStage{FeatureCommandStagePreGBuffer})
	}
}

func TestParticlesFeaturePassStageGating(t *testing.T) {
	feature := &ParticlesFeature{}
	app := &App{}
	if feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected particles feature to gate off without renderer resources")
	}
	app.BufferManager = &gpu.GpuBufferManager{
		ParticleSystemActive: true,
		ParticlesBindGroup0:  &wgpu.BindGroup{},
		ParticlesBindGroup1:  &wgpu.BindGroup{},
	}
	if feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected particles feature to gate off without particle render resources")
	}
	app.ParticleResources = &ParticleResources{RenderPipeline: &wgpu.RenderPipeline{}}
	if !feature.HasPassStage(app, FeaturePassStageAccumulation) {
		t.Fatal("expected particles feature to gate on with render resources and contribution")
	}
}

func TestAnalyticMediumFeatureIsGraphOwnedByPostLightingNode(t *testing.T) {
	feature := &AnalyticMediumFeature{}
	nodes := feature.GraphNodeNames()
	if !sameStrings(nodes, []string{RenderNodeFeatureAnalyticMedia}) {
		t.Fatalf("analytic media graph nodes = %v, want %v", nodes, []string{RenderNodeFeatureAnalyticMedia})
	}
	if !sameCommandStages(feature.GraphCommandStages(), []FeatureCommandStage{FeatureCommandStagePostLighting}) {
		t.Fatalf("analytic media graph command stages = %v, want %v", feature.GraphCommandStages(), []FeatureCommandStage{FeatureCommandStagePostLighting})
	}
}

func TestPlanetBodyFeatureIsGraphOwnedByPostLightingNode(t *testing.T) {
	feature := &PlanetBodyFeature{}
	nodes := feature.GraphNodeNames()
	if !sameStrings(nodes, []string{RenderNodeFeaturePlanetBodies}) {
		t.Fatalf("planet body graph nodes = %v, want %v", nodes, []string{RenderNodeFeaturePlanetBodies})
	}
	if !sameCommandStages(feature.GraphCommandStages(), []FeatureCommandStage{FeatureCommandStagePostLighting}) {
		t.Fatalf("planet body graph command stages = %v, want %v", feature.GraphCommandStages(), []FeatureCommandStage{FeatureCommandStagePostLighting})
	}
}

func TestAstronomicalFeatureIsGraphOwnedByPostLightingNode(t *testing.T) {
	feature := &AstronomicalFeature{}
	nodes := feature.GraphNodeNames()
	if !sameStrings(nodes, []string{RenderNodeFeatureAstronomical}) {
		t.Fatalf("astronomical graph nodes = %v, want %v", nodes, []string{RenderNodeFeatureAstronomical})
	}
	if !sameCommandStages(feature.GraphCommandStages(), []FeatureCommandStage{FeatureCommandStagePostLighting}) {
		t.Fatalf("astronomical graph command stages = %v, want %v", feature.GraphCommandStages(), []FeatureCommandStage{FeatureCommandStagePostLighting})
	}
}

func TestCAVolumeFeatureIsGraphOwnedBySimulationAndRenderNodes(t *testing.T) {
	feature := &CAVolumeFeature{}
	wantNodes := []string{RenderNodeFeatureCAVolumesSim, RenderNodeFeatureCAVolumesRender}
	if !sameStrings(feature.GraphNodeNames(), wantNodes) {
		t.Fatalf("CA volume graph nodes = %v, want %v", feature.GraphNodeNames(), wantNodes)
	}
	wantStages := []FeatureCommandStage{FeatureCommandStagePreGBufferVolumes, FeatureCommandStagePostLighting}
	if !sameCommandStages(feature.GraphCommandStages(), wantStages) {
		t.Fatalf("CA volume graph command stages = %v, want %v", feature.GraphCommandStages(), wantStages)
	}
}

func TestAppRenderGraphLifecycleDispatch(t *testing.T) {
	var calls []string
	graph := NewRenderGraph()
	graph.Register(RenderNodeSpec{Name: "a", Node: &lifecycleTestRenderNode{name: "a", enabled: true, calls: &calls}})
	graph.Register(RenderNodeSpec{Name: "b", After: []string{"a"}, Node: &lifecycleTestRenderNode{name: "b", enabled: true, calls: &calls}})
	app := &App{RenderGraph: graph}

	if err := app.setupRenderGraphNodes(); err != nil {
		t.Fatalf("setupRenderGraphNodes returned error: %v", err)
	}
	if err := app.resizeRenderGraphNodes(640, 480); err != nil {
		t.Fatalf("resizeRenderGraphNodes returned error: %v", err)
	}
	if err := app.sceneBuffersRecreatedRenderGraphNodes(); err != nil {
		t.Fatalf("sceneBuffersRecreatedRenderGraphNodes returned error: %v", err)
	}
	if err := app.updateRenderGraphNodes(); err != nil {
		t.Fatalf("updateRenderGraphNodes returned error: %v", err)
	}
	app.shutdownRenderGraphNodes()

	expected := []string{
		"a:setup",
		"b:setup",
		"a:resize",
		"b:resize",
		"a:recreate",
		"b:recreate",
		"a:update",
		"b:update",
		"b:shutdown",
		"a:shutdown",
	}
	if !sameStrings(calls, expected) {
		t.Fatalf("graph lifecycle calls = %v, want %v", calls, expected)
	}
}

func samePassStages(a, b []FeaturePassStage) bool {
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

func sameCommandStages(a, b []FeatureCommandStage) bool {
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
