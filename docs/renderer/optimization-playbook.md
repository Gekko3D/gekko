# VoxelRT Optimization Playbook

This document is a historical optimization plan for the VoxelRT renderer.

It is kept as:

- the rationale behind the optimization pass
- the original task breakdown and review checklist
- a record of deferred work that may still matter later

It is not the live source of truth for current renderer behavior. Use these docs for current behavior instead:

- [`runtime.md`](runtime.md)
- [`overview.md`](overview.md)
- [`gbuffer-compaction-note.md`](gbuffer-compaction-note.md)

The original plan was written to support parallel agent work with low merge risk. Each task includes:

- goal
- expected impact
- file ownership
- constraints
- verification
- acceptance criteria

## Status

Completed:

- Task A: Implement real CPU renderer profiling
- Task B: Remove unconditional dirtying and material rebuilds
- Task C: Skip redundant material GPU uploads
- Task D: Cache transform matrices
- Task E: Reduce scene packing allocation churn
- Task F: Make BVH rebuilds conditional
- Task G: Reduce bind-group churn
- Task H: Remove bridge-side linear scans
- Task I: Investigate G-buffer compaction

Deferred / not done:

- Task A2: Add optional GPU timestamp profiling

If future optimization work resumes, treat this file as historical context and start from the current renderer docs above, not from the plan text below.

## Goal

Reduce frame time and frame-to-frame CPU/GPU churn in the renderer without changing visible behavior.

Priorities:

1. remove unnecessary per-frame CPU work
2. reduce redundant GPU uploads and bind-group recreation
3. improve observability so optimization work is measurable
4. only then attempt larger renderer architecture changes

## Important Constraints

- The renderer spans multiple layers:
  - ECS bridge: `mod_voxelrt_client*.go`
  - app orchestration: `voxelrt/rt/app`
  - GPU resource manager: `voxelrt/rt/gpu`
  - CPU scene and culling: `voxelrt/rt/core`
  - voxel storage: `voxelrt/rt/volume`
  - shaders: `voxelrt/rt/shaders`
- Picking and voxel edits are still CPU-authoritative through `Scene` and `XBrickMap`.
- Resize and buffer-growth invalidation are load-bearing. If a buffer or texture can be recreated, all dependent bind groups must stay correct afterward.
- Avoid changing multiple major subsystems in one task.
- Prefer one agent per disjoint write set.
- If two tasks share any file, they are not parallel-safe until the write sets are split explicitly.
- If a task skips work by caching, the task must name the invalidation key or source of truth up front.

## Baseline Findings

These are the current likely optimization targets.

### P0: Instrumentation gap

- `voxelrt/rt/app/profiler.go`
- The current profiler does not record timing. `BeginScope`, `EndScope`, and `Reset` are stubs.
- This blocks measured optimization work.
- Important distinction:
  - CPU timing is possible immediately in engine code.
  - GPU timing is a separate task because command recording/submission is asynchronous with actual GPU execution.
  - True GPU pass timings should use timestamp queries when supported by the active adapter/device.

### P0: Unconditional per-frame dirtying in bridge sync

- `mod_voxelrt_client_systems.go`
- Renderer objects are marked dirty every frame even when transform values did not change.
- Material tables are rebuilt every frame.
- This forces extra scene commit work, matrix work, and GPU uploads.

### P0: Repeated material serialization and upload

- `mod_voxelrt_client_materials.go`
- `voxelrt/rt/gpu/manager_voxel.go`
- Material tables are rebuilt and re-uploaded even when palette/material inputs did not change.

### P1: Repeated transform matrix recomputation

- `voxelrt/rt/core/transform.go`
- `voxelrt/rt/core/scene.go`
- `voxelrt/rt/gpu/manager_scene.go`
- `ObjectToWorld()` and `WorldToObject()` are recomputed repeatedly instead of cached behind dirty state.

### P1: Full BVH rebuilds every frame

- `voxelrt/rt/core/scene.go`
- Visible and shadow BVHs are rebuilt every commit even when scene structure and visibility sets are effectively unchanged.

### P1: High CPU allocation churn in packing/upload paths

- `voxelrt/rt/gpu/manager_scene.go`
- `voxelrt/rt/gpu/manager_voxel.go`
- Several hot paths build fresh byte slices and temporary maps every frame.

### P1: Bind-group churn

- `voxelrt/rt/gpu/manager_hiz.go`
- `voxelrt/rt/gpu/manager_sprites.go`
- Some bind groups are recreated every frame even when underlying resources have not changed.

### P2: G-buffer bandwidth is likely high

- `voxelrt/rt/gpu/manager_render_setup.go`
- `voxelrt/rt/shaders/gbuffer.wgsl`
- `voxelrt/rt/shaders/deferred_lighting.wgsl`
- Current G-buffer writes multiple large textures, including a world-position target.
- This is a larger architectural optimization and should come after instrumentation and CPU/upload cleanup.

### P2: Shadow map resource format is heavy

- `voxelrt/rt/gpu/manager_shadow.go`
- `voxelrt/rt/shaders/shadow_map.wgsl`
- Shadow maps currently use `RGBA32Float`.
- This may be larger than needed for the data actually consumed.

## Recommended Execution Order

Do the work in this order.

1. Add real CPU profiling and timing output.
2. Remove unconditional dirtying and material rebuilds.
3. Cache transform matrices and reduce repeated CPU-side serialization.
4. Reduce bind-group churn and bridge-side linear scans.
5. Make BVH rebuilds conditional.
6. Add optional GPU timestamp profiling if adapter/device support is available.
7. Only then investigate G-buffer and shadow format changes.

## Benchmark Protocol

Use one reproducible workload for all before/after performance claims.

- Before assigning parallel optimization tasks, pin the workload in the task or PR:
  - app/module and cwd
  - scene, level, or sample content
  - camera position/path and whether the camera is stationary
  - resolution
  - render mode and debug mode
  - warmup frame count
  - measured frame count or capture duration
- Suggested fallback workload when no project-specific scene is pinned:
  - `examples/testing-vox`
  - 1280x720
  - stationary camera after content load
  - 120 warmup frames
  - 300 measured frames
- Until Task A is complete, frame-time claims are provisional.
- If a desktop run is unavailable, the agent must say so and limit claims to skipped CPU work, skipped queue writes, allocation reduction, or bind-group churn reduction.
- Every before/after comparison must use the same workload, resolution, and camera state.
- Every report must say whether the timing source is CPU scope timing, GPU timestamps, or neither.

## Task Breakdown For Agents

Each task below is meant to be assigned to one agent.

### Task A: Implement real CPU renderer profiling

Goal:
- Replace the current counter-only profiler with actual CPU scoped timing data.

Expected impact:
- No direct perf win.
- Enables reliable before/after measurement for all other optimization work.

Own these files:
- `voxelrt/rt/app/profiler.go`
- `voxelrt/rt/app/app_frame.go`

Do not touch:
- shaders
- GPU resource layouts
- scene culling logic
- bridge sync files

Requirements:
- Keep existing counters.
- Add CPU timing per named scope.
- Surface scope timings in the debug overlay string.
- Avoid large heap churn in the profiler itself.
- Make it explicit in code/comments/output that these timings are CPU-side and do not represent actual GPU pass execution time.

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Acceptance criteria:
- Debug overlay shows per-scope CPU timings in milliseconds.
- Existing counters still work.
- No behavior change outside debug/profiling output.

Prompt template:

```text
Implement real CPU scope timing for the VoxelRT profiler.

Scope:
- Own only `voxelrt/rt/app/profiler.go` and `voxelrt/rt/app/app_frame.go`.
- Keep current counter reporting.
- Add low-overhead scoped timing with `BeginScope`/`EndScope`.
- Expose timings in the stats string used by the debug overlay.
- Make it clear in naming and output that this is CPU-side timing around update/encoding/submission, not true GPU pass timing.

Constraints:
- Do not change renderer behavior.
- Do not change resource layouts, shader bindings, or pass ordering.
- Avoid unnecessary allocations in the profiler.
- Do not modify bridge sync call sites for this task. Use the existing scope sites already present in renderer and bridge code.

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Return:
- files changed
- what CPU timings are now reported
- any residual limitations
```

### Task A2: Add optional GPU timestamp profiling

Goal:
- Add true GPU pass timing using timestamp queries when supported by the active adapter/device.

Expected impact:
- No direct perf win.
- Enables measured GPU-side optimization work.

Own these files:
- `voxelrt/rt/app/app.go`
- `voxelrt/rt/app/app_frame.go`
- `voxelrt/rt/app/profiler.go`
- any minimal support code under `voxelrt/rt/gpu` if needed for query/readback ownership

Do not touch:
- renderer algorithms
- shader bindings unless strictly necessary

Requirements:
- Detect adapter support for `timestamp-query`.
- Request the feature only when supported.
- Gracefully fall back to CPU-only profiling when unavailable.
- Record timestamps around major render passes in `Render()`.
- Resolve and read back query data safely.
- Avoid blocking the CPU every frame if a buffered readback approach is possible.
- Keep the debug overlay honest about whether a timing is CPU or GPU derived.
- Before implementation, verify that the current `wgpu` binding exposes the needed feature query, query-set, resolve, and readback APIs. If that support is missing, stop after documenting the feasibility gap instead of forcing a partial implementation.

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Acceptance criteria:
- On supported hardware, major render passes report GPU timings.
- On unsupported hardware, renderer still works and reports CPU-only profiling.
- There is no hard requirement for `timestamp-query` support.

Prompt template:

```text
Add optional GPU timestamp profiling to the VoxelRT renderer.

Scope:
- Own `voxelrt/rt/app/app.go`, `voxelrt/rt/app/app_frame.go`, `voxelrt/rt/app/profiler.go`, and minimal related support code if needed.
- You are not alone in the codebase. Do not revert unrelated edits.

Requirements:
- Detect whether the adapter supports `timestamp-query`.
- Request that feature only when supported.
- Add GPU timestamp query creation, resolve, readback, and reporting for major render passes.
- Preserve a clean fallback to CPU-only profiling when the feature is unavailable.
- Keep output explicit about CPU vs GPU timings.
- Verify first that the current `wgpu` binding exposes the required feature-query and timestamp-query APIs. If it does not, return a feasibility note instead of a fake or partial implementation.

Do not change:
- render algorithms
- visible behavior
- pass ordering unless required for timestamp placement

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Return:
- files changed
- how feature detection works
- which passes now report GPU timings
- fallback behavior on unsupported systems
- residual risks
```

### Task B: Remove unconditional dirtying and material rebuilds

Goal:
- Stop bridge sync from marking transforms/materials dirty when values are unchanged.

Expected impact:
- High CPU-side win.
- May also reduce downstream GPU upload work.

Own these files:
- `mod_voxelrt_client_systems.go`
- `mod_voxelrt_client_materials.go`

Do not touch:
- `voxelrt/rt/gpu`
- shaders

Requirements:
- Only mark transform dirty when position, rotation, scale, or pivot actually changed.
- Cache or reuse material tables with an explicit invalidation key.
  - Preferred key: the `vox.VoxelPalette` `AssetId` used by bridge sync.
  - If the asset server can mutate a palette in place under the same `AssetId`, include a structural fingerprint of `VoxelPaletteAsset` contents in the key.
  - Do not invent a fake revision field unless the task also adds and wires that field end to end.
- Preserve visible behavior and existing content semantics.

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Acceptance criteria:
- `obj.Transform.Dirty` is not set every frame for unchanged objects.
- Material table rebuilds are skipped when palette inputs are unchanged.
- No rendering regressions in normal content flow.

Prompt template:

```text
Optimize the ECS-to-renderer bridge to avoid unconditional per-frame dirtying and material-table rebuilds.

Scope:
- Own `mod_voxelrt_client_systems.go` and `mod_voxelrt_client_materials.go`.
- You are not alone in the codebase. Do not revert unrelated edits.

Requirements:
- Detect real transform changes before setting renderer transform dirty.
- Avoid rebuilding material tables every frame when palette/material inputs are unchanged.
- Use an explicit invalidation key for material-table caching.
  - Prefer `vox.VoxelPalette` `AssetId`.
  - If same-`AssetId` palette mutation is possible, include a structural fingerprint of palette contents.
- Keep behavior identical from the game's point of view.

Do not change:
- GPU manager files
- shaders
- public renderer APIs unless strictly necessary

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Return:
- files changed
- what state is now cached
- what work is skipped in steady state
```

### Task C: Skip redundant material GPU uploads

Goal:
- Avoid reserializing and reuploading material data unless the material table changed.

Expected impact:
- Medium to high CPU and queue-write reduction in scenes with many stable objects.

Own these files:
- `voxelrt/rt/gpu/manager_voxel.go`
- any minimal related type updates in `voxelrt/rt/gpu`

Do not touch:
- ECS bridge
- shaders

Requirements:
- Add change tracking for per-object material allocations.
- Preserve buffer growth/recreation correctness.
- Do not break material offset/layout assumptions.

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Acceptance criteria:
- Stable objects do not rewrite material buffer contents every frame.
- Buffer recreation still repopulates correct data.

### Task D: Cache transform matrices

Goal:
- Cache `ObjectToWorld` and `WorldToObject` results behind dirty state.

Expected impact:
- Medium CPU win across scene commit, instance packing, and interaction.

Own these files:
- `voxelrt/rt/core/transform.go`
- minimal updates in `voxelrt/rt/core/scene.go`
- minimal updates in `voxelrt/rt/gpu/manager_scene.go`

Do not touch:
- shaders
- bridge sync

Requirements:
- Preserve current transform semantics.
- Ensure caches invalidate on position, rotation, scale, or pivot changes.

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:
- Matrix results are cached and reused within steady-state frames.
- No change to visible transforms, raycast behavior, or AABB correctness.

### Task E: Reduce scene packing allocation churn

Goal:
- Reduce byte-slice growth and temporary allocations in scene upload paths.

Expected impact:
- Medium CPU win in large scenes.

Own these files:
- `voxelrt/rt/gpu/manager_scene.go`

Do not touch:
- shader layouts
- scene semantics

Requirements:
- Pre-size buffers where sizes are known.
- Avoid repeated tiny allocations like per-instance `idBuf`.
- Keep binary layout identical.

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:
- Binary layout stays unchanged.
- Heap churn is reduced in steady state.

### Task F: Make BVH rebuilds conditional

Goal:
- Rebuild visible and shadow BVHs only when needed.

Expected impact:
- Potentially large CPU win in scenes with mostly static visibility.

Own these files:
- `voxelrt/rt/core/scene.go`
- optionally `voxelrt/rt/bvh` only if absolutely necessary

Do not touch:
- shaders
- GPU resource formats

Requirements:
- Preserve correctness for:
  - object add/remove
  - transform changes
  - voxel structure/AABB changes
  - visibility set changes
  - shadow caster set changes
- Prefer a conservative invalidation strategy over an aggressive risky one.

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/bvh`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Acceptance criteria:
- No stale BVH usage.
- Stable frames skip rebuild work when scene state is unchanged.

### Task G: Reduce bind-group churn

Goal:
- Cache and reuse bind groups that are currently recreated every frame.

Expected impact:
- Medium CPU driver/API overhead reduction.

Own these files:
- `voxelrt/rt/gpu/manager_hiz.go`
- `voxelrt/rt/gpu/manager_sprites.go`
- minimal related invalidation hooks in `voxelrt/rt/app/app_frame.go` or `voxelrt/rt/app/app_pipelines.go` only if needed to keep cache lifetime correct

Do not touch:
- pass order
- shader layouts

Requirements:
- Hi-Z pass 0 bind-group creation should not happen every frame if source resources are unchanged.
- Sprite batch bind groups should be reused where possible instead of full release/recreate every frame.
- Respect resize and buffer recreation invalidation.
- Name the invalidation events explicitly in code/comments.
  - Hi-Z: source depth view change, Hi-Z texture/view recreation, pipeline recreation.
  - Sprites: atlas version/view change, sprite buffer recreation, depth view recreation, pipeline recreation.
- Do not cache bind groups across pipeline layout changes.

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:
- No per-frame bind-group churn for unchanged sprite atlas/depth resources.
- Hi-Z remains correct across resize.
- No stale bind groups after resize, atlas swap, sprite-buffer growth, depth-view recreation, or pipeline recreation.

### Task H: Remove bridge-side linear scans

Goal:
- Eliminate obviously avoidable O(n) scans in bridge sync.

Expected impact:
- Small to medium CPU win, especially with many textures/lights/entities.

Own these files:
- `mod_voxelrt_client_systems.go`

Do not touch:
- GPU manager
- shaders

Requirements:
- Replace repeated sprite atlas scans over `server.textures` with a direct cache/index.
- Avoid `GetAllComponents` walk per light if a more direct query or lookup is possible.

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Acceptance criteria:
- Bridge no longer performs repeated texture-table full scans and per-light component scans in steady state.

### Task I: Investigate G-buffer compaction

Goal:
- Reduce G-buffer bandwidth, especially by removing or shrinking expensive targets.

Expected impact:
- Potentially large GPU win.

Own these files:
- `voxelrt/rt/gpu/manager_render_setup.go`
- `voxelrt/rt/shaders/gbuffer.wgsl`
- `voxelrt/rt/shaders/deferred_lighting.wgsl`
- relevant pipeline setup in `voxelrt/rt/app`

Do not start this until:
- Tasks A through H are done or measured
- Preferably after Task A2 if GPU timing support is available

Requirements:
- First produce a short design note before code changes.
- Prefer reconstructing world position in lighting from depth + camera matrices instead of storing full position.
- Keep image quality and debug modes correct.

Verification:
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- manual smoke check in app if available

Acceptance criteria:
- Documented before/after memory format.
- Reduced render-target bandwidth.
- No visible regressions in standard lit mode.

## Parallelization Plan

Safe parallel combinations:

- Task A with Task B
- Task A2 with Task B
- Task C with Task D
- Task G with Task H

Do not run in parallel:

- Task B with Task H
  - both own `mod_voxelrt_client_systems.go`
- Task B with Task C
  - both depend on material state assumptions
- Task D with Task F
  - both affect CPU scene/update behavior
- Task A2 with Task I
  - both affect profiling and measurement strategy for GPU-heavy work
- Task I with anything else
  - too cross-cutting

## Agent Output Format

Require every agent to return:

- files changed
- what was optimized
- what steady-state work is now skipped
- what invalidation path was added
- exact workload used for measurement
- whether numbers are CPU timings, GPU timings, or structural-only claims without runtime timing
- before/after numbers if timing was available
- verification commands run
- residual risks

## Review Checklist

Before accepting an optimization patch, check:

- Does it preserve resize correctness?
- Does it preserve buffer-growth correctness?
- Does it avoid stale bind groups?
- Does it preserve CPU-authoritative picking and edits?
- Does it preserve shader/buffer layout compatibility?
- Does it skip work only when inputs are truly unchanged?
- Is the invalidation logic obvious and testable?
- Does the patch name the cache key or invalidation trigger clearly enough for the next agent to extend it safely?

## Minimal Verification Commands

Use the smallest command set that matches the change:

- Renderer GPU package:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- Renderer core package:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`
- Voxel storage:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume`
- BVH builder:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/bvh`
- Broad renderer/bridge changes:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

## Current Status

As of this document:

- renderer package tests pass:
  - `./voxelrt/rt/core`
  - `./voxelrt/rt/gpu`
  - `./voxelrt/rt/volume`
  - `./voxelrt/rt/bvh`
- the highest-priority missing piece is real CPU profiling
- GPU timestamp profiling is a separate optional task
- no canonical benchmark workload is pinned yet, so all perf claims must include the exact workload details listed above
- the highest-probability low-risk wins are:
  - stop unconditional dirtying
  - stop redundant material rebuild/upload
  - cache transform matrices
  - reduce bind-group churn
