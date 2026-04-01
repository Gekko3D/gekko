# VoxelRT Quality-Preserving Optimization Plan

This document is a practical plan for improving VoxelRT performance without changing the rendered look.

Use this plan when the goal is:

- lower frame time
- lower frame-to-frame spikes
- keep the current voxel shading contract intact

For live renderer behavior, also read:

- [`runtime.md`](runtime.md)
- [`change-guide.md`](change-guide.md)
- [`verification.md`](verification.md)

## Non-Goals

This plan does not target optimizations that would visibly change the image, including:

- lowering internal render resolution
- smoothing voxel colors across neighboring voxels
- replacing per-voxel normals with mesh-like interpolated normals
- replacing voxel-center lighting or shadowing with hit-position smoothing
- adding softer point-light shadow filtering that breaks voxel-stable shadow response

Those changes may be valid experiments, but they are outside this plan.

## Quality Constraints

Any implementation work under this plan should preserve the current renderer contract:

- palette-driven voxel material identity remains stable per voxel
- one lighting normal is used per visible voxel cell
- normals remain occupancy-driven first, with deterministic fallback behavior
- direct-light response for thin voxels stays consistent between opaque and transparent paths
- voxel-center-based shading and shadowing stays stable per visible voxel

See [`runtime.md`](runtime.md) for the current contract and rationale.

## Current Cost Structure

The current frame flow is:

1. CPU update and scene commit
2. voxel/material uploads
3. G-buffer compute
4. Hi-Z generation
5. shadow pass
6. tiled light cull
7. deferred lighting
8. transparent accumulation
9. resolve

The full-screen G-buffer and lighting passes are load-bearing for image quality, so the first optimization targets should be wasted work around them:

- fewer visible objects
- fewer shadow updates
- less upload churn
- fewer passes when they cannot contribute

## Priority Order

1. Directional shadow caching and cadence
2. Stronger visibility reduction before GPU work
3. Voxel upload churn reduction
4. Pass skipping when a subsystem has no contribution
5. Tiled-light pressure reduction
6. Only after measurement, consider deeper bandwidth work

## Task 1: Directional Shadow Caching

### Why first

The current renderer already applies cadence and invalidation logic to local-light shadows, but directional shadow layers are still refreshed unconditionally. That is a high-cost path with low visual upside when nothing relevant changed.

### Goal

Make directional shadow updates follow the same cache-driven rules already used for other shadow layers.

### Expected win

- lower shadow pass cost in steady camera/light conditions
- lower frame spikes in scenes with heavy directional shadow coverage
- no image change when cached results are valid

### Implementation outline

- stop appending all directional layers by default in `voxelrt/rt/gpu/shadow_schedule.go`
- evaluate directional layers through signature, scene revision, and voxel upload revision invalidation
- use cadence for directional cascades just like local-light layers
- honor `forceDirectionalRefresh` only when camera motion or cascade reprojection actually requires it
- keep existing shadow cache state and cached cascade upload path

### Files to inspect

- `voxelrt/rt/gpu/shadow_schedule.go`
- `voxelrt/rt/gpu/shadow_metadata.go`
- `voxelrt/rt/gpu/manager_scene.go`
- `voxelrt/rt/app/app_frame.go`

### Risks

- stale cascades after scene edits or voxel uploads if invalidation is incomplete
- visible cascade lag during camera motion if cadence is too aggressive

### Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- visual smoke test with a directional-lit scene and moving camera

## Task 2: Stronger Visibility Reduction

### Why second

The renderer already uses frustum culling and previous-frame Hi-Z occlusion. More reliable object exclusion reduces work across BVH upload, G-buffer traversal, shadow selection, and lighting.

### Goal

Reduce the number of objects entering `VisibleObjects` and `ShadowObjects` without dropping valid contributors.

### Expected win

- lower CPU scene commit cost
- lower G-buffer traversal cost
- lower shadow caster count
- no image change if culling stays conservative

### Implementation outline

- audit which object types still have `AllowOcclusionCulling` disabled unnecessarily
- tighten `ShadowMaxDistance` usage for authored/runtime voxel objects
- use shadow caster grouping more aggressively for dense authored sets
- ensure terrain and large static structures still benefit from Hi-Z when safe
- preserve hysteresis so culling does not introduce popping

### Files to inspect

- `voxelrt/rt/core/scene.go`
- `mod_voxelrt_client.go`
- `mod_voxelrt_client_systems.go`
- authored asset spawn paths that set shadow metadata

### Risks

- over-culling shadow casters can remove off-screen shadows on visible receivers
- aggressive occlusion eligibility can cause temporal popping if object bounds or reprojection assumptions are wrong

### Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- compare `FrustumVisible`, `HiZEligible`, `HiZCulled`, and `ShadowCasters` counters in the debug overlay

## Task 3: Voxel Upload Churn Reduction

### Why third

GPU upload churn does not change the image, but it can dominate frame spikes during edits, destruction, terrain changes, or streaming.

### Goal

Reduce CPU work and queue writes in `UpdateVoxelData` while preserving exact GPU content.

### Expected win

- lower frame spikes during voxel edits and streaming
- less CPU work in the update stage
- less unnecessary queue traffic

### Implementation outline

- prioritize near-camera or visible dirty sectors before off-screen dirty work
- keep sector and brick budgets, but schedule them by relevance instead of map iteration order
- avoid full-sector brick dirtying unless the sector truly requires complete re-upload
- keep material upload invalidation strict but minimal
- add profiling around upload categories if current counters are not sufficient

### Files to inspect

- `voxelrt/rt/gpu/manager_voxel.go`
- `voxelrt/rt/volume/xbrickmap*.go`
- bridge code that triggers voxel dirtiness

### Risks

- partial upload ordering bugs can leave CPU and GPU content briefly out of sync
- prioritization can starve far dirty regions if no fairness policy exists

### Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume`
- compare `VoxelSecUp`, `VoxelBrkUp`, `VoxelSecPend`, and `VoxelBrkPend` under a repeatable edit workload

## Task 4: Skip Non-Contributing Passes

### Why fourth

Some passes are currently encoded every frame even when they have no work or no visible contribution.

### Goal

Avoid command recording and GPU execution for passes whose inputs are empty.

### Expected win

- lower command encoding overhead
- lower GPU time in scenes without transparency, particles, or local lights
- no image change

### Implementation outline

- skip accumulation when there are no CA volumes, transparent voxel overlays, particles, or sprites
- skip tiled-light cull when only directional lights are present or when there are no local lights
- keep resolve because it composites opaque output and overlays
- ensure debug and overlay behavior remains intact

### Files to inspect

- `voxelrt/rt/app/app_frame.go`
- `voxelrt/rt/gpu/manager_particles.go`
- `voxelrt/rt/gpu/manager_ca.go`
- transparent overlay setup code

### Risks

- stale transparent targets if a skipped pass depends on explicit clears
- edge cases where a subsystem is logically present but has zero draw count only after GPU-side simulation

### Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- visual smoke test for transitions between empty and non-empty transparent content

## Task 5: Tiled-Light Pressure Reduction

### Why fifth

The tiled-light path is already the right structure, but its cost still depends on how many lights spill into many tiles or fall back to fullscreen coverage.

### Goal

Reduce tile list density without changing lighting results.

### Expected win

- lower tiled-light cull cost
- lower deferred-lighting inner-loop cost
- no image change

### Implementation outline

- reduce unnecessary fullscreen classification for local lights
- improve screen-space bounds estimation where current projection checks are too conservative
- tighten authored light range values where scenes use oversized ranges
- monitor `LightListEntriesAvg` and `LightListEntriesMax` as the primary counters

### Files to inspect

- `voxelrt/rt/gpu/manager_tiled_lighting.go`
- light authoring and bridge sync code

### Risks

- incorrect tile bounds can cause lights to disappear at screen edges
- authored-range tightening may be a content migration task, not just a renderer task

### Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- compare light-list counters and visual coverage for near-screen-edge lights

## Task 6: Deeper Bandwidth Work Only After Measurement

### Why last

The renderer has already completed one conservative G-buffer compaction step. More aggressive bandwidth changes are possible, but they are more likely to interact with the visual contract.

### Candidates

- shadow map format review
- more compact material packing
- alternative AO sample layouts with identical quality targets

### Rule

Do not start here unless profiling shows that visibility, shadow cadence, uploads, and pass skipping are no longer the main bottlenecks.

## Measurement Protocol

Use a fixed workload for before/after comparisons:

- same module and scene
- same resolution
- same camera path or stationary camera
- same debug mode state
- same warmup period
- same measured duration or frame count

At minimum, compare:

- CPU scope timings from the renderer profiler
- `Visible`
- `ShadowCasters`
- `ShadowUpdates`
- `VoxelSecUp`
- `VoxelBrkUp`
- `LightListEntriesAvg`
- `LightListEntriesMax`

If a desktop visual test is not available, limit claims to:

- skipped updates
- reduced uploads
- reduced encoded work
- lower profiler scope times

## Recommended First Execution Slice

If only one change should be attempted first, do this:

1. implement directional shadow invalidation and cadence
2. verify no visible regression during camera motion and voxel edits
3. measure shadow update count and shadow pass CPU time

That is the highest-probability improvement with the lowest risk to image quality.
