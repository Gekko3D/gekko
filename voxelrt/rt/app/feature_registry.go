package app

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

type noOpFeature struct {
	name string
}

func (f noOpFeature) Name() string {
	if f.name == "" {
		return "noop"
	}
	return f.name
}

func (f noOpFeature) Enabled(*App) bool { return true }

func (f noOpFeature) Setup(*App) error { return nil }

func (f noOpFeature) Resize(*App, uint32, uint32) error { return nil }

func (f noOpFeature) OnSceneBuffersRecreated(*App) error { return nil }

func (f noOpFeature) Update(*App) error { return nil }

func (f noOpFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error { return nil }

func (f noOpFeature) Shutdown(*App) {}

func (a *App) ensureDefaultFeatures() {
	if a == nil || a.defaultFeaturesRegistered {
		return
	}
	cfg := a.FeatureConfig
	if !cfg.AutoRegisterDefaults {
		a.defaultFeaturesRegistered = true
		return
	}

	defaults := a.defaultFeatureList(cfg.Defaults)
	if len(defaults) > 0 {
		// Keep historical ordering where defaults are ahead of any custom pre-init registrations.
		a.features = append(defaults, a.features...)
	}

	a.defaultFeaturesRegistered = true
}

func (a *App) defaultFeatureList(flags AppFeatureFlags) []Feature {
	defaults := make([]Feature, 0, 8)
	if flags.Text {
		defaults = append(defaults, &TextFeature{})
	}
	if flags.Gizmos {
		defaults = append(defaults, &GizmoFeature{})
	}
	if flags.Skybox {
		defaults = append(defaults, &SkyboxFeature{})
	}
	if flags.CAVolumes {
		defaults = append(defaults, &CAVolumeFeature{})
	}
	if flags.Transparency {
		defaults = append(defaults, &TransparencyFeature{})
	}
	if flags.Particles {
		defaults = append(defaults, &ParticlesFeature{})
	}
	if flags.Sprites {
		defaults = append(defaults, &SpriteFeature{})
	}
	if flags.CelestialBodies {
		defaults = append(defaults, &CelestialBodiesFeature{})
	}
	defaults = append(defaults, noOpFeature{name: "lifecycle-noop"})
	return defaults
}

func (a *App) RegisterFeature(feature Feature) {
	if a == nil || feature == nil {
		return
	}
	a.features = append(a.features, feature)
}

func (a *App) setupFeatures() error {
	if a == nil {
		return nil
	}
	for _, feature := range a.features {
		if feature == nil || !feature.Enabled(a) {
			continue
		}
		if err := feature.Setup(a); err != nil {
			return fmt.Errorf("feature %q setup failed: %w", feature.Name(), err)
		}
	}
	return nil
}

func (a *App) resizeFeatures(width, height uint32) error {
	if a == nil {
		return nil
	}
	for _, feature := range a.features {
		if feature == nil || !feature.Enabled(a) {
			continue
		}
		if err := feature.Resize(a, width, height); err != nil {
			return fmt.Errorf("feature %q resize failed: %w", feature.Name(), err)
		}
	}
	return nil
}

func (a *App) sceneBuffersRecreatedFeatures() error {
	if a == nil {
		return nil
	}
	for _, feature := range a.features {
		if feature == nil || !feature.Enabled(a) {
			continue
		}
		if err := feature.OnSceneBuffersRecreated(a); err != nil {
			return fmt.Errorf("feature %q scene-buffer recreation failed: %w", feature.Name(), err)
		}
	}
	return nil
}

func (a *App) updateFeatures() error {
	if a == nil {
		return nil
	}
	for _, feature := range a.features {
		if feature == nil || !feature.Enabled(a) {
			continue
		}
		if err := feature.Update(a); err != nil {
			return fmt.Errorf("feature %q update failed: %w", feature.Name(), err)
		}
	}
	return nil
}

func (a *App) renderFeatures(encoder *wgpu.CommandEncoder, target *wgpu.TextureView) error {
	if a == nil {
		return nil
	}
	for _, feature := range a.features {
		if feature == nil || !feature.Enabled(a) {
			continue
		}
		if err := feature.Render(a, encoder, target); err != nil {
			return fmt.Errorf("feature %q render failed: %w", feature.Name(), err)
		}
	}
	return nil
}

func (a *App) dispatchCommandStage(stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if a == nil {
		return nil
	}
	for _, feature := range a.features {
		if feature == nil || !feature.Enabled(a) {
			continue
		}
		stageFeature, ok := feature.(FeatureCommandStageHandler)
		if !ok {
			continue
		}
		if err := stageFeature.DispatchCommandStage(a, stage, encoder); err != nil {
			return fmt.Errorf("feature %q command stage %d failed: %w", feature.Name(), stage, err)
		}
	}
	return nil
}

func (a *App) hasCommandStageWork(stage FeatureCommandStage) bool {
	if a == nil {
		return false
	}
	for _, feature := range a.features {
		if feature == nil || !feature.Enabled(a) {
			continue
		}
		contributor, ok := feature.(FeatureCommandStageContributor)
		if ok && contributor.HasCommandStage(a, stage) {
			return true
		}
	}
	return false
}

func (a *App) renderPassStage(stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if a == nil {
		return nil
	}
	for _, feature := range a.features {
		if feature == nil || !feature.Enabled(a) {
			continue
		}
		stageFeature, ok := feature.(FeaturePassStageHandler)
		if !ok {
			continue
		}
		if err := stageFeature.RenderPassStage(a, stage, pass); err != nil {
			return fmt.Errorf("feature %q pass stage %d failed: %w", feature.Name(), stage, err)
		}
	}
	return nil
}

func (a *App) hasPassStageWork(stage FeaturePassStage) bool {
	if a == nil {
		return false
	}
	for _, feature := range a.features {
		if feature == nil || !feature.Enabled(a) {
			continue
		}
		contributor, ok := feature.(FeaturePassStageContributor)
		if ok && contributor.HasPassStage(a, stage) {
			return true
		}
	}
	return false
}

func (a *App) renderScreenStage(stage FeatureScreenStage, encoder *wgpu.CommandEncoder, target *wgpu.TextureView) error {
	switch stage {
	case FeatureScreenStagePostResolve:
		return a.renderFeatures(encoder, target)
	default:
		return nil
	}
}

func (a *App) hasScreenStageWork(stage FeatureScreenStage) bool {
	if a == nil {
		return false
	}
	for _, feature := range a.features {
		if feature == nil || !feature.Enabled(a) {
			continue
		}
		contributor, ok := feature.(FeatureScreenStageContributor)
		if ok && contributor.HasScreenStage(a, stage) {
			return true
		}
	}
	return false
}

func (a *App) shutdownFeatures() {
	if a == nil {
		return
	}
	for i := len(a.features) - 1; i >= 0; i-- {
		feature := a.features[i]
		if feature == nil {
			continue
		}
		feature.Shutdown(a)
	}
}
