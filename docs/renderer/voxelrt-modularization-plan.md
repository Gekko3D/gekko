# VoxelRT Modularization Plan

This document describes a staged refactor plan to make `voxelrt` modular without destabilizing the current renderer.

The immediate goal is not renderer-backend abstraction. The goal is to turn the current hardwired pass graph into a feature-oriented architecture where optional rendering capabilities can be added, removed, and evolved independently.

## Why This Refactor

Today `voxelrt` is internally separated into bridge, app, GPU manager, core scene, and shaders, but pass setup and execution are still mostly hardcoded inside the app layer.

That creates 3 problems:

- optional features are harder to add cleanly
- game-specific renderer extensions risk leaking into the core
- resize, bind-group rebuild, and frame-order logic become harder to maintain as features grow

The modularization effort should solve those issues while preserving the existing renderer as the stable base.

## Scope

In scope:

- modular renderer features
- optional pass registration
- per-feature setup, resize, update, and render hooks
- per-feature GPU resources and invalidation handling
- migration of existing optional renderer capabilities into feature units

Out of scope for this plan:

- replacing `voxelrt` with another renderer backend
- redesigning the opaque voxel core from scratch
- changing ECS semantics unrelated to rendering

## Current Pain Points

The main hardcoded integration points are:

- pipeline creation and feature setup in `voxelrt/rt/app/app.go`
- resize-time resource rebuilds in `voxelrt/rt/app/app_frame.go`
- frame sequencing in `voxelrt/rt/app/app_frame.go`
- feature-specific bind-group invalidation spread across shared renderer code

These are the first places to reduce coupling.

## Desired End State

The target architecture has:

- one stable renderer core for window, device, scene upload, camera, G-buffer, lighting, and final resolve
- many optional renderer features layered on top
- a small feature interface with lifecycle hooks
- renderer passes enabled only when the feature is installed and active
- clear ownership of GPU resources and bind groups per feature

Examples of feature candidates:

- skybox
- sprites
- particles
- transparent overlay
- CA volumes
- gizmos
- text overlay
- celestial bodies

## Guiding Principles

- keep opaque voxel rendering as the core baseline
- modularize optional systems first
- preserve current visuals while refactoring
- centralize shared-frame data, decentralize feature-specific logic
- avoid introducing a giant generic abstraction that hides important rendering details

## Refactor Phases

## Phase 1: Inventory and Boundaries

Goal:

- identify which parts are renderer core and which are optional features

Tasks:

- map the current frame graph in detail
- classify all existing passes as core or optional
- document shared resources used across passes
- document current invalidation paths on resize and scene-buffer recreation

Deliverables:

- updated frame graph notes
- feature inventory table
- ownership table for buffers, textures, bind groups, and shaders

Acceptance criteria:

- every existing pass is explicitly classified as core or optional
- every existing resource rebuild path is documented

## Phase 2: Define Feature Interface

Goal:

- introduce a small renderer feature contract without changing behavior

Tasks:

- define a feature interface for lifecycle hooks
- define a renderer context object passed to features
- separate core app responsibilities from feature responsibilities
- add feature registration to `RtApp`

Suggested hook shape:

- `Name() string`
- `Enabled(*App) bool`
- `Setup(*App) error`
- `Resize(*App, width, height uint32) error`
- `OnSceneBuffersRecreated(*App) error`
- `Update(*App) error`
- `Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error`
- `Shutdown(*App)`

Acceptance criteria:

- renderer still behaves the same
- no feature is migrated yet
- the new interface exists and is exercised by at least one trivial feature

## Phase 3: Extract Non-Core Features

Goal:

- move optional systems out of hardcoded app logic

Recommended extraction order:

1. gizmos
2. text overlay
3. sprites
4. particles
5. transparent overlay
6. CA volumes
7. skybox

Tasks per feature:

- move setup code into a feature unit
- move resize-time rebuild logic into the feature
- move render scheduling into the feature
- move bind-group rebuild handling behind the feature boundary
- keep shared GPU manager helpers only where genuinely shared

Acceptance criteria:

- each extracted feature can be enabled or disabled independently
- core voxel rendering still works with all optional features disabled
- existing feature tests still pass or are updated with equivalent coverage

## Phase 4: Stabilize Shared Core APIs

Goal:

- reduce accidental coupling between features and the renderer core

Tasks:

- define explicit shared services exposed by `App`
- reduce direct feature access to internal fields where possible
- centralize common frame inputs such as camera matrices, scene state, and main targets
- make feature ordering explicit rather than implicit in `Render()`

Acceptance criteria:

- feature execution order is declared in one place
- new features can be added without editing unrelated feature code

## Phase 5: Add Optional Feature Configuration

Goal:

- make modularity visible at engine and game level

Tasks:

- extend `VoxelRtModule` config to enable or disable features
- add sensible defaults preserving current behavior
- allow games to register custom features
- ensure unused features do not allocate resources

Acceptance criteria:

- a game can disable selected features at startup
- a game can install a custom feature without patching core pass logic

## Phase 6: First New Feature on the New Architecture

Goal:

- prove the architecture by adding a new feature cleanly

Recommended first client:

- `CelestialBodiesFeature`

Why:

- it is optional
- it should not pollute core voxel rendering
- it exercises setup, GPU upload, update, render ordering, and depth-aware composition

Acceptance criteria:

- celestial bodies can be added without modifying unrelated features
- games not using celestial bodies incur effectively zero cost

## Proposed Core vs Feature Split

Core renderer responsibilities:

- device and swapchain lifetime
- camera state
- scene upload
- voxel scene structures
- G-buffer generation
- lighting
- final resolve / presentation
- common resize coordination

Feature responsibilities:

- feature-specific pipelines
- feature-specific bind groups
- feature-specific GPU buffers or textures
- optional render passes
- optional ECS-to-renderer synced data

## Risks

- hidden coupling through shared `App` fields
- resize and scene-buffer recreation bugs
- feature ordering regressions
- duplicated GPU resource helpers if extraction is rushed
- over-abstracting render passes until debugging becomes harder

## Risk Mitigation

- migrate one feature at a time
- keep existing visuals as regression targets
- add smoke tests for “all optional features off”
- add smoke tests for selective feature combinations
- document feature dependencies explicitly

## Testing Strategy

- preserve current package tests under `voxelrt/rt/core`, `voxelrt/rt/gpu`, and bridge code
- add renderer feature registration tests
- add bind-group recreation tests for extracted features
- add resize smoke tests
- add scene-buffer growth tests to catch stale resource bindings

## Concrete Milestones

Milestone 1:

- feature interface exists
- one trivial feature extracted

Milestone 2:

- gizmos, text, sprites, and particles extracted

Milestone 3:

- transparent overlay, CA volumes, and skybox extracted

Milestone 4:

- feature configuration exposed through `VoxelRtModule`

Milestone 5:

- celestial bodies implemented as the first net-new modular feature

## Recommended First Step

Start with Phase 1 and Phase 2 only.

Do not begin by extracting all passes at once. First define:

- what is core
- what is optional
- how a feature plugs in
- how resize and scene-buffer invalidation are routed

That gives a stable foundation for all later renderer work, including celestial bodies.
