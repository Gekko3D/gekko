# VoxelRT Modularization Checklist

This checklist turns the modularization roadmap into implementation-ready work items with concrete file targets.

Use it as the execution order for the refactor. The phases are intentionally incremental so the renderer can stay working after each phase.

## Phase 1: Baseline Mapping

Goal:

- capture the current hardwired frame graph and feature ownership before changing code

Files to inspect and annotate:

- `gekko/voxelrt/rt/app/app.go`
- `gekko/voxelrt/rt/app/app_frame.go`
- `gekko/voxelrt/rt/app/app_pipelines.go`
- `gekko/voxelrt/rt/app/app_particles.go`
- `gekko/voxelrt/rt/app/app_ca.go`
- `gekko/voxelrt/rt/gpu/manager_render_setup.go`
- `gekko/voxelrt/rt/gpu/manager.go`
- `gekko/mod_voxelrt_client.go`
- `gekko/mod_voxelrt_client_systems.go`

Tasks:

- document the current setup order in `App.Init()`
- document the current resize rebuild order in `App.Resize()`
- document the current `Update()` ordering in `app_frame.go`
- document the current `Render()` ordering in `app_frame.go`
- classify each pass as core or optional
- identify which passes depend on scene-buffer recreation
- identify which passes depend on resize only
- identify which passes depend on both

Suggested output:

- extend `gekko/docs/renderer/runtime.md` with a feature inventory table
- optionally add a short dependency table to `gekko/docs/renderer/change-guide.md`

Definition of done:

- every current pass is labeled `core` or `feature`
- every pass has an explicit owner file
- every pass has explicit rebuild triggers

## Phase 2: Introduce Feature Abstractions

Goal:

- add a small feature API without changing rendering behavior

New files to add:

- `gekko/voxelrt/rt/app/feature.go`
- `gekko/voxelrt/rt/app/feature_registry.go`

Files to edit:

- `gekko/voxelrt/rt/app/app.go`
- `gekko/voxelrt/rt/app/app_frame.go`

Tasks:

- define a renderer feature interface in `feature.go`
- define a small `FeatureContext` or use `*App` directly
- add feature registration storage on `App`
- add ordered feature registration in `App.Init()`
- add lifecycle dispatch helpers:
  - setup
  - resize
  - scene-buffer recreation
  - update
  - render
  - shutdown
- add one no-op or debug feature as the first adopter

Recommended interface shape:

```go
type Feature interface {
    Name() string
    Enabled(*App) bool
    Setup(*App) error
    Resize(*App, uint32, uint32) error
    OnSceneBuffersRecreated(*App) error
    Update(*App) error
    Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error
    Shutdown(*App)
}
```

Definition of done:

- `App` can own a list of features
- lifecycle methods can be called without changing visual output
- the renderer still passes existing tests

## Phase 3: Separate Core App Wiring from Feature Wiring

Goal:

- stop scattering feature setup through `App.Init()`, `Resize()`, and `Update()`

Files to edit:

- `gekko/voxelrt/rt/app/app.go`
- `gekko/voxelrt/rt/app/app_frame.go`

Tasks:

- isolate core setup in `App.Init()`:
  - device
  - storage target
  - camera
  - scene upload
  - G-buffer
  - lighting
  - final resolve or presentation path
- replace direct optional setup calls with feature dispatch
- isolate core resize logic from feature resize logic
- replace direct scene-buffer recreation hooks with feature dispatch
- make render ordering explicit via named stages or ordered feature slices

Definition of done:

- the app core no longer knows feature-specific setup details
- adding a feature no longer requires editing unrelated resize code

## Phase 4: Extract Gizmos

Goal:

- migrate the lowest-risk optional pass first

New files to add:

- `gekko/voxelrt/rt/app/feature_gizmos.go`

Files to edit:

- `gekko/voxelrt/rt/app/app.go`
- `gekko/voxelrt/rt/app/app_frame.go`
- `gekko/voxelrt/rt/gpu/gizmo_pass.go` if small access helpers are needed

Tasks:

- move gizmo setup into a `GizmoFeature`
- move gizmo resize/bind-group rebuild handling into the feature
- move gizmo render dispatch into the feature
- remove hardcoded gizmo setup from `App.Init()`
- remove hardcoded gizmo update/render paths from `app_frame.go`

Definition of done:

- gizmos can be disabled without touching core rendering
- gizmo rendering is owned by one feature file

## Phase 5: Extract Text Overlay

Goal:

- move text/debug overlay ownership into a feature

New files to add:

- `gekko/voxelrt/rt/app/feature_text.go`

Files to edit:

- `gekko/voxelrt/rt/app/app.go`
- `gekko/voxelrt/rt/app/app_frame.go`
- text-related app helper files if needed

Tasks:

- move text renderer setup into the feature
- move debug text rendering into feature update/render
- leave `DrawText(...)` as a public `App` helper if desired

Definition of done:

- text overlay is optional
- debug overlay stops being hardcoded app logic

## Phase 6: Extract Sprites

Goal:

- move billboard sprite rendering into an optional feature

New files to add:

- `gekko/voxelrt/rt/app/feature_sprites.go`

Files to edit:

- `gekko/voxelrt/rt/app/app_pipelines.go`
- `gekko/voxelrt/rt/app/app.go`
- `gekko/voxelrt/rt/app/app_frame.go`
- `gekko/voxelrt/rt/gpu/manager_render_setup.go` if bind-group helpers are split

Tasks:

- move sprite pipeline setup into the feature
- move sprite bind-group recreation into the feature
- move sprite render scheduling into the feature

Definition of done:

- sprite pipeline creation is not hardcoded in `App.Init()`
- sprite feature is skipped when no sprite batches exist

## Phase 7: Extract Particles

Goal:

- move billboard particle rendering and related setup into an optional feature

New files to add:

- `gekko/voxelrt/rt/app/feature_particles.go`

Files to edit:

- `gekko/voxelrt/rt/app/app_particles.go`
- `gekko/voxelrt/rt/app/app_pipelines.go`
- `gekko/voxelrt/rt/app/app.go`
- `gekko/voxelrt/rt/app/app_frame.go`
- `gekko/voxelrt/rt/gpu/manager_particles.go`

Tasks:

- move particle pipeline setup into the feature
- move atlas/bootstrap setup into the feature
- move update and render scheduling into the feature
- keep shared particle buffer helpers in GPU manager only if still reused

Definition of done:

- particles are fully feature-owned from app perspective

## Phase 8: Extract Transparent Overlay

Goal:

- move general transparent voxel composition into an optional feature

New files to add:

- `gekko/voxelrt/rt/app/feature_transparency.go`

Files to edit:

- `gekko/voxelrt/rt/app/app_pipelines.go`
- `gekko/voxelrt/rt/app/app.go`
- `gekko/voxelrt/rt/app/app_frame.go`
- `gekko/voxelrt/rt/gpu/manager_render_setup.go`

Tasks:

- move transparent overlay pipeline setup into the feature
- move accumulation target rebuild handling into the feature if practical
- move transparent-pass render scheduling into the feature
- move resolve dependency declarations into explicit render ordering

Definition of done:

- transparency is no longer wired inline through core app code

## Phase 9: Extract CA Volumes

Goal:

- move cellular volume rendering/simulation into a feature

New files to add:

- `gekko/voxelrt/rt/app/feature_ca_volumes.go`

Files to edit:

- `gekko/voxelrt/rt/app/app_ca.go`
- `gekko/voxelrt/rt/app/app.go`
- `gekko/voxelrt/rt/app/app_frame.go`
- `gekko/voxelrt/rt/gpu/manager_ca.go`

Tasks:

- move CA sim/bounds/render setup into the feature
- move CA update/render scheduling into the feature
- keep CA-specific GPU manager logic behind feature-owned calls

Definition of done:

- CA volumes are removable without editing core frame logic

## Phase 10: Extract Skybox

Goal:

- move skybox generation and related GPU state into a feature

New files to add:

- `gekko/voxelrt/rt/app/feature_skybox.go`

Files to edit:

- `gekko/voxelrt/rt/app/app.go`
- `gekko/voxelrt/rt/app/app_frame.go`
- `gekko/voxelrt/rt/gpu/manager_skybox.go`
- `gekko/mod_voxelrt_client_skybox.go`

Tasks:

- move skybox pipeline setup into the feature
- keep ECS sync in bridge code, but make runtime skybox generation feature-owned
- make lighting dependency on skybox texture explicit in the feature ordering

Definition of done:

- skybox generation is modular
- lighting still samples the skybox through a stable shared contract

## Phase 11: Expose Feature Configuration

Goal:

- make modularity visible to engine users

Files to edit:

- `gekko/mod_voxelrt_client.go`
- `gekko/mod_voxelrt_client_systems.go`
- `gekko/docs/renderer/overview.md`
- `gekko/docs/engine/modules.md`

Tasks:

- extend `VoxelRtModule` with feature toggles or feature list configuration
- provide a default config matching today’s behavior
- allow custom feature registration
- document which features are enabled by default

Possible config shape:

```go
type VoxelRtFeatureFlags struct {
    Gizmos       bool
    Text         bool
    Sprites      bool
    Particles    bool
    Transparency bool
    CAVolumes    bool
    Skybox       bool
}
```

Definition of done:

- games can disable selected features without patching engine code
- defaults preserve existing demos

## Phase 12: Add First Net-New Feature

Goal:

- validate the new architecture with a cleanly isolated addition

Recommended feature:

- any optional renderer pass with clear setup/update/render ownership

New files to add later:

- one feature implementation file under `gekko/voxelrt/rt/app/`
- renderer-side GPU helpers as needed
- engine-side bridge code only if the feature needs ECS data

Tasks:

- implement the feature only through the new feature interfaces
- avoid editing unrelated optional-feature code
- use `spacegame_go` as the first client

Definition of done:

- the new feature proves the modular architecture is real, not just renamed code

## Cross-Cutting Cleanup Tasks

Apply these throughout all phases:

- reduce direct feature access to unrelated `App` internals
- prefer explicit helper methods over ad-hoc field reach-through
- document feature dependencies
- keep feature-owned bind-group rebuild logic local
- add tests whenever a feature gains ownership of resize or scene-buffer recreation hooks

## Testing Checklist

After each phase:

- run renderer package tests
- run `spacegame_go` smoke tests
- verify resize does not break pipelines
- verify scene-buffer growth still rebuilds dependent bind groups
- verify the renderer still works with the extracted feature disabled

Add targeted tests for:

- feature registration and dispatch
- feature enable/disable behavior
- resize lifecycle dispatch
- scene-buffer recreation lifecycle dispatch

## Suggested Execution Order

Do not parallelize the first three phases.

Recommended sequence:

1. Phase 1
2. Phase 2
3. Phase 3
4. Phase 4
5. Phase 5
6. Phase 6
7. Phase 7
8. Phase 8
9. Phase 9
10. Phase 10
11. Phase 11
12. Phase 12

This order extracts the lowest-risk features first and leaves more specialized features until the feature model is already stable.

## Stop Conditions

Pause and reassess if:

- resize bugs start multiplying across extracted features
- bind-group recreation rules remain too implicit
- core and feature responsibilities are still unclear after Phase 3

If that happens, add one stabilization pass before extracting the next feature.
