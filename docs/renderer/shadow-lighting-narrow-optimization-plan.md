# Shadow And Lighting Narrow Optimization Plan

This document is the live execution plan for the next narrow shadow and lighting optimizations in `voxelrt`.

It is written for agent execution. Each task includes:

- goal
- expected impact
- file ownership
- constraints
- verification
- acceptance criteria
- a prompt seed you can hand to an agent

## Implementation Status

Current state in the repo:

- Task 1: implemented
- Task 2: implemented
- Task 3: implemented
- Task 4: not started and still optional

Validated package tests after implementation:

- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu ./voxelrt/rt/app ./voxelrt/rt/core`

Implemented files:

- Task 1:
  - `voxelrt/rt/app/app_frame.go`
  - `voxelrt/rt/gpu/pass_contribution.go`
  - `voxelrt/rt/app/app_frame_test.go`
  - `voxelrt/rt/gpu/pass_contribution_test.go`
- Task 2:
  - `voxelrt/rt/gpu/manager_tiled_lighting.go`
  - `voxelrt/rt/gpu/manager_tiled_lighting_test.go`
  - `voxelrt/rt/shaders/tiled_light_cull.wgsl`
- Task 3:
  - `voxelrt/rt/core/scene.go`
  - `voxelrt/rt/core/scene_test.go`
  - `voxelrt/rt/gpu/shadow_metadata.go`
  - `voxelrt/rt/gpu/shadow_metadata_test.go`

Remaining gap:

- package tests pass, but a live runtime smoke test is still the right final check for WebGPU behavior on the target machine

## Opening Context

System:

- `voxelrt` shadow, tiled-light, and deferred-lighting pipeline

Current state:

- `XBrickMap` hot-path lookup/traversal work has already been optimized.
- Directional shadow scheduling is no longer the first priority.
- The next likely wins are narrow cases where the renderer still records or executes work that is unnecessary for the current frame.

Observed opportunities:

- shadow-pass CPU submission still re-buckets and reserializes updates every frame

Resolved in this phase:

- tiled-light cull no longer runs for directional-only or zero-local-light scenes
- tiled-light GPU and CPU coverage logic now use the same tighter conservative local-light bounds
- local-light-only scenes now reduce `ShadowObjects` conservatively before GPU shadow work

Still open:

- shadow-pass CPU submission still re-buckets and reserializes updates every frame

Goal:

- reduce renderer cost further without changing the current visual contract
- keep work narrow and measurable
- prioritize wins that remove whole passes or shrink per-frame work

Explicit v1 boundaries:

- do not redesign the renderer architecture
- do not replace tiled lighting with a new lighting structure
- do not change voxel shading, normal generation, or shadow response semantics
- do not introduce quality reductions disguised as optimizations
- do not rely on GPU timestamp queries

## Confidence Gate

Confidence: High

Why:

- the target areas are already isolated in the current frame flow
- the first changes are pass gating and conservative work reduction, not redesign
- profiler counters already exist for most of the key decision points

Key assumptions:

- directional-only scenes exist often enough that skipping tiled-light cull is worthwhile
- `LightListEntriesAvg`, `LightListEntriesMax`, and `ShadowCasters` are sufficient first-order metrics
- the visual contract must remain unchanged, so all wins must come from less wasted work rather than altered shading

What would raise confidence further:

- before/after captures from two or three representative scenes:
  - directional-only scene
  - few local lights
  - many local lights
  - dense shadow-caster scene

Consult SME dev required?: No

## Technology Stack

- Language: Go
- GPU API: WebGPU via `github.com/cogentcore/webgpu/wgpu`
- Core modules in scope:
  - `voxelrt/rt/app`
  - `voxelrt/rt/gpu`
  - `voxelrt/rt/core` only if shadow-caster selection needs small metadata adjustments

## Global Contracts And Guardrails

Apply to all tasks in this plan:

- preserve current rendered output
- preserve voxel-stable shading and shadow behavior
- preserve deterministic pass scheduling for the same scene and camera state
- prefer skipping work over replacing algorithms
- keep instrumentation low overhead and removable
- do not claim shader-time wins from CPU timings alone

End-of-phase commands:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/app`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`

## Measurement Protocol

Use the same workload before and after each task:

- same scene
- same camera path or stationary camera
- same resolution
- same lighting setup
- same debug mode state

At minimum, compare:

- profiler scope timings:
  - `Tile Light Cull`
  - `Shadows`
  - `Lighting`
- counters:
  - `LocalLights`
  - `SceneLights`
  - `ShadowUpdates`
  - `ShadowDirectionalUpdates`
  - `ShadowSpotUpdates`
  - `ShadowPointUpdates`
  - `ShadowCasters`
  - `LightListEntriesAvg`
  - `LightListEntriesMax`

If runtime testing is available, use:

- `cd /Users/ddevidch/code/go/gekko3d/examples/testing-vox`
- `env GOCACHE=/tmp/gekko3d-gocache go run .`

If only code-level validation is available, limit claims to:

- skipped passes
- lower encoded work
- lower candidate counts
- lower list density

## Recommended Execution Order

Original execution order:

1. Skip tiled-light cull when there are no local lights.
2. Reduce tiled-light pressure without changing lighting results.
3. Reduce shadow caster count before the shadow pass.
4. Optimize shadow-pass CPU submission only if the first three are not enough.

Current recommendation:

- leave the renderer as-is unless profiling still shows meaningful CPU cost in shadow update submission
- only start Task 4 if measurement shows CPU submission overhead still matters

## Parallelization Rules

Historical execution notes:

- Tasks 1 to 3 were completed in parallel with disjoint ownership.
- Task 4 was intentionally left unstarted.

If work resumes:

- Task 4 can run alone.
- No further parallel split is needed for this document unless the plan expands.

## Task Breakdown For Agents

### Task 1: Skip tiled-light cull for directional-only scenes

Status:

- Implemented

Goal:

- avoid encoding and dispatching the tiled-light cull pass when no point or spot lights are present

Expected impact:

- removes a whole compute pass from directional-only scenes
- lowers CPU command encoding cost
- lowers GPU work in scenes where tiled-light lists are irrelevant

Own these files:

- `voxelrt/rt/app/app_frame.go`
- `voxelrt/rt/gpu/pass_contribution.go`
- minimal related tests

Do not touch:

- lighting shader behavior
- shadow scheduling
- light data layout

Requirements:

- gate tiled-light cull on local-light presence, not merely total scene-light count
- ensure tile-light buffers cannot leak stale local-light data into later frames
- keep deferred lighting behavior unchanged for:
  - directional-only scenes
  - mixed directional and local-light scenes
  - zero-light scenes
- add or update tests for the gating logic

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/app`

Acceptance criteria:

- directional-only scenes skip tiled-light cull
- scenes with local lights still run tiled-light cull correctly
- no stale tile-light artifacts appear after transitioning between those scene types

Implementation notes:

- tiled-light cull is now gated on local-light presence in the frame loop
- tiled-light state is reset on skipped frames to avoid stale buffer contents and stale profiler metrics
- app and gpu package tests were added for the gating and reset behavior

Prompt seed:

```text
Implement a narrow renderer optimization in VoxelRT: skip tiled-light cull when there are no local lights.

Scope:
- Own only the tiled-light pass gating path and minimal tests.

Requirements:
- Gate the tiled-light cull on point/spot light presence, not total scene light count.
- Preserve directional-light-only rendering behavior.
- Prevent stale tile-light data from leaking across frames when the cull pass is skipped.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/app
```

### Task 2: Reduce tiled-light pressure without changing coverage

Status:

- Implemented

Goal:

- reduce tile-list density and conservative fullscreen classification while preserving lighting results

Expected impact:

- lower tiled-light cull cost
- lower deferred-lighting inner-loop cost
- less pressure on `TiledLightingMaxLightsPerTile`

Own these files:

- `voxelrt/rt/gpu/manager_tiled_lighting.go`
- minimal related tests
- only small bridge/light metadata helpers if strictly required

Do not touch:

- deferred lighting shading model
- material system
- scene-wide light authoring policy unless explicitly needed for metrics

Requirements:

- inspect why local lights fall back to fullscreen coverage
- tighten only conservative bounds or classification logic that is provably safe
- keep near-screen-edge lights correct
- keep current profiler counters usable:
  - `LightListEntriesAvg`
  - `LightListEntriesMax`
- if helpful, add one narrow counter for fullscreen-classified local lights

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:

- representative scenes show equal visual coverage
- `LightListEntriesAvg` and/or `LightListEntriesMax` improve on scenes with local lights
- no new edge clipping for lights near the frustum boundary

Implementation notes:

- the CPU-side estimator now rejects local lights that are fully behind the camera
- camera-containing lights still classify as fullscreen
- both the CPU estimator and the GPU tiled-light cull shader now use tighter conservative projected local-light bounds instead of the older three-sample sphere estimate
- focused tests cover behind-camera rejection, fullscreen camera-contained lights, and near-edge partial coverage

Prompt seed:

```text
Implement a narrow tiled-light optimization in VoxelRT.

Scope:
- Own only tiled-light coverage estimation/culling and minimal tests.

Requirements:
- Reduce unnecessary fullscreen classification for local lights.
- Preserve exact lighting coverage, especially near screen edges.
- Keep current profiler counters useful, and add a narrow diagnostic counter only if needed.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
```

### Task 3: Reduce shadow caster count before GPU shadow work

Status:

- Implemented

Goal:

- reduce the number of objects included in `ShadowObjects` without losing valid visible shadow contribution

Expected impact:

- lower shadow BVH size
- lower shadow traversal work
- lower shadow-pass cost in caster-dense scenes

Own these files:

- `voxelrt/rt/gpu/shadow_metadata.go`
- `voxelrt/rt/core/scene.go` only if required for conservative metadata propagation
- related tests

Do not touch:

- shadow map sampling rules
- shadow softness
- directional cascade math unless strictly needed for safer cull volumes

Requirements:

- keep the selection conservative
- preserve off-screen casters that still affect visible receivers
- prefer stronger use of existing cull volumes and authored grouping metadata before inventing new policy
- measure using:
  - `ShadowCasters`
  - `ShadowUpdates`
  - `Shadows` profiler scope

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`

Acceptance criteria:

- representative scenes show lower `ShadowCasters` where appropriate
- visible shadows remain intact
- no obvious popping from over-aggressive culling

Implementation notes:

- local-light-only scenes now drop objects that do not intersect any shadow-casting spot or point light volume
- any scene with a directional shadow light keeps the broader caster set by design
- helper and scene tests cover both local-light culling and the directional-light escape hatch

Prompt seed:

```text
Implement a narrow shadow optimization in VoxelRT: reduce shadow caster count before the GPU shadow pass.

Scope:
- Own only conservative shadow-caster selection and minimal tests.

Requirements:
- Preserve visible shadow contribution.
- Reduce `ShadowObjects` only where culling is provably safe.
- Prefer improving existing shadow volume filtering and grouping rather than adding a new shadow system.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core
```

### Task 4: Reduce shadow-pass CPU submission overhead

Status:

- Pending
- Optional

Goal:

- reduce CPU-side overhead in the shadow-pass submission path without changing rendered output

Expected impact:

- lower CPU time in frames with many shadow updates
- fewer temporary allocations or repeated bucketing costs

Own these files:

- `voxelrt/rt/gpu/manager_shadow.go`
- related tests

Do not touch:

- shadow shader logic
- update scheduling policy
- shadow atlas format

Requirements:

- avoid repeated per-frame bucketing/repacking work where possible
- keep update ordering and resolution bucketing semantics intact
- do not regress bind-group correctness or buffer lifetime rules
- only pursue this after measuring Tasks 1 to 3

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:

- shadow-pass CPU submission does less work without changing update contents
- no change in visible shadow behavior

Current recommendation:

- do not start this task unless profiling still shows meaningful CPU-side shadow submission cost after Tasks 1 to 3

Prompt seed:

```text
Implement a narrow CPU-side shadow submission optimization in VoxelRT.

Scope:
- Own only the shadow update packing/submission path and minimal tests.

Requirements:
- Reduce repeated CPU bucketing/serialization overhead.
- Preserve shadow update ordering and semantics.
- Do not change shader behavior or scheduling policy.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
```

## Recommended First Execution Slice

Historical first slice:

1. implement Task 1
2. verify directional-only scenes skip tiled-light cull cleanly
3. measure whether frame cost drops on those scenes

Current next slice:

1. run a live runtime smoke test on the target machine
2. profile shadow-heavy scenes
3. only if CPU shadow submission still matters, start Task 4
