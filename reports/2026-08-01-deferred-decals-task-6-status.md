# Deferred Decals Task 6 Status

## Metadata

- Owner: `gekko` VoxelRT renderer and ActionGame
- Date: 2026-08-01
- Project/Area: cross-module deferred decals
- Related links: [`deferred-decals-plan.md`](../docs/renderer/deferred-decals-plan.md), [`runtime.md`](../docs/renderer/runtime.md), [`overview.md`](../docs/renderer/overview.md)
- Reviewers requested: renderer and ActionGame owners

## Executive Summary

- Goal: document the live deferred-decal ownership, scheduling, resources, and verification handoff.
- Current state: the renderer provides an opt-in `DecalFeature`; ActionGame registers it and supplies its retained blood/soot records through the VoxelRT bridge.
- Progress: runtime, overview, and renderer resource inventory documentation now cover the feature.
- Risk/Blocker: cross-module automated, visual, and performance verification were not run for this documentation-only task.
- Ask: run the pending checks on the target GPU before merging the renderer and ActionGame changes.

## Scope

- Included: documentation and this cross-module handoff only.
- Not included: renderer or ActionGame code changes, new unit tests, live measurements, or GPU capture.

## Current Behavior

- `DecalFeature` is not registered by default. A consumer opts in through `VoxelRtModule.RenderFeatures`; the `decals` bridge then converts retained `DecalInstance` values into 80-byte GPU records and atlas batches.
- `feature-decals` runs after tiled light culling and before deferred lighting. It clears a full-resolution premultiplied `RGBA8Unorm` overlay and issues one instanced draw per non-empty atlas batch.
- Deferred lighting composites the overlay into base color before BRDF evaluation. Decals therefore use the opaque wall's existing lighting, AO, and shadows without changing voxel data.
- Without the feature, the bridge clears decal input, the graph records no decal pass, and lighting reads a transparent fallback instead of a full-resolution decal target.

## Resource Inventory

- App-owned: `DecalResources.Pipeline`.
- `GpuBufferManager`-owned: decal instance buffer, per-atlas bind groups, overlay texture/view, transparent fallback texture/view, and decal G-buffer bind group.
- Recreation: resize rebuilds the overlay and lighting binding; scene-buffer recreation rebuilds decal bind groups; feature shutdown releases decal-owned GPU resources and restores the fallback binding.

## Verification

No unit tests were created or run, per the documentation-only request.

- Not run: `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu ./voxelrt/rt/app ./voxelrt/rt/shaders`
- Not run: `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- Not run: `cd /Users/ddevidch/code/go/gekko3d/actiongame && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- Not run: `cd /Users/ddevidch/code/go/gekko3d/actiongame && env GOCACHE=/tmp/gekko3d-gocache go run .`
- Passed: `git -C /Users/ddevidch/code/go/gekko3d/gekko diff --check`

The live check must cover the plan's 0, 512, 2048, and 4096-mark cases at one fixed level, camera path, resolution, and lighting. Record frame time/FPS and `DecalCount`, `DecalAtlasBatches`, `DecalDrawCalls`, `DecalTargetReady`, `DecalBindingsReady`, and `DecalPassRecorded`; do not claim GPU-time savings without a GPU capture.

## Compatibility and Next Step

- The feature is opt-in and adds no serialized data or migration.
- Removing ActionGame feature registration restores the transparent fallback and no-pass path.
- Before merge, run the listed checks and manually verify resize, corner rejection, destruction invalidation, transparency exclusion, and 4096-mark frame budget.
