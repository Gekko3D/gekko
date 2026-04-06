# VoxelRT Media

This page documents the bounded volumetric media path used by `gekko` for analytic atmosphere and fog-style rendering.

Use this document when you need to understand:

- which ECS-facing types author bounded media
- how analytic media differs from transparent voxels, CA volumes, and water
- where the half-resolution temporal volumetric path lives
- what is generic versus what is still atmosphere-oriented

## Why "Analytic Medium"

The name is deliberate:

- `analytic`
  - density and bounds are defined procedurally from math and simple shapes such as spheres and boxes
  - this path does not raymarch voxel-by-voxel transparent shells and does not depend on a simulated volume field
- `medium`
  - the thing being rendered is a participating volume that absorbs and scatters light through space
  - in renderer terms, this means fog, atmosphere, haze, mist, smoke-like media, or similar volumetric light-transport substances

This is why the ECS type is named `AnalyticMediumComponent` instead of `AtmosphereComponent` or `FogComponent`.
It is meant to cover both atmosphere shells and bounded fog volumes without pretending they are different renderer subsystems.

## Scope

The live analytic media system is for bounded volumetric effects such as:

- planet atmosphere shells
- local fog patches
- box-shaped mist volumes
- room or corridor fog
- soft smoke-like media where a simple analytic density function is sufficient

It is not the right path for:

- water surface rendering
- refractive glass surfaces
- infinite/global height fog
- simulated volumetric fluids

Water surfaces still need a separate surface renderer. Analytic media can still be reused for underwater haze or water-adjacent fog.

## ECS Authoring Surface

The ECS-facing component is `AnalyticMediumComponent` in `analytic_medium_ecs.go`.

Important fields:

- shape:
  - `AnalyticMediumShapeSphere`
  - `AnalyticMediumShapeBox`
- sphere bounds:
  - `OuterRadius`
  - `InnerRadius`
- box bounds:
  - `BoxExtents`
- transport and look:
  - `Density`
  - `Falloff`
  - `EdgeSoftness`
  - `PhaseG`
  - `LightStrength`
  - `AmbientStrength`
- optional atmosphere-style shaping:
  - `LimbStrength`
  - `LimbExponent`
  - `DiskHazeStrength`
  - `DiskHazeTintMix`
- background-dependent tuning:
  - `OpaqueExtinctionScale`
  - `BackgroundExtinctionScale`
  - `OpaqueAlphaScale`
  - `BackgroundAlphaScale`
  - `OpaqueRevealScale`
  - `BackgroundRevealScale`
- boundary shaping:
  - `BoundaryFadeStart`
  - `BoundaryFadeEnd`
- variation and cost:
  - `NoiseScale`
  - `NoiseStrength`
  - `SampleCount`

Normalization helpers in the same file are load-bearing. If new fields are added, update normalization and the ECS-to-GPU upload path together.

## Presets

Reusable constructors live in `analytic_medium_presets.go`.

Current helpers:

- `NewAtmosphereMedium(...)`
- `NewFogSphereMedium(...)`
- `NewFogBoxMedium(...)`

These are the intended starting points for gameplay/content code. Prefer extending presets over copy-pasting one-off component tuning into demos.

## Renderer Path

The bridge upload happens in `mod_voxelrt_client_systems.go`.

The live renderer path is:

1. ECS sync builds `gpu.AnalyticMediumHost` records.
2. `GpuBufferManager.UpdateAnalyticMedia(...)` packs per-medium GPU records and history uniforms.
3. `AnalyticMediumFeature` runs after deferred lighting in a dedicated half-resolution render pass.
4. The analytic media shader raymarches the current frame, reprojects previous history, and writes:
   - half-resolution volumetric color
   - half-resolution volumetric front depth
5. The resolve pass upsamples that result with full-resolution depth-aware filtering and composites it over opaque lighting before WBOIT transparency.

Live code locations:

- feature orchestration:
  - `voxelrt/rt/app/feature_analytic_medium.go`
- pipeline setup:
  - `voxelrt/rt/app/app_medium.go`
- GPU resources and bind groups:
  - `voxelrt/rt/gpu/manager.go`
  - `voxelrt/rt/gpu/manager_medium.go`
  - `voxelrt/rt/gpu/manager_render_setup.go`
- shaders:
  - `voxelrt/rt/shaders/analytic_medium.wgsl`
  - `voxelrt/rt/shaders/resolve_transparency.wgsl`

## Relationship To Other Transparency Systems

There are now three different categories to keep straight:

- transparent voxel overlay:
  - surface-oriented transparency and see-through materials
  - contributes through WBOIT accumulation
- dedicated water surfaces:
  - stylized horizontal water bodies with blocky stepped highlights/refraction
  - contributes through WBOIT accumulation as its own feature
- analytic media:
  - bounded volumetric fog/atmosphere
  - half-resolution temporal volumetric path
- CA volumes:
  - simulated volumetrics
  - separate half-resolution volume path and resolve integration

Do not force all four through the same abstraction. They solve different rendering problems.

## Current Quality Model

The live analytic media path uses:

- half-resolution rendering
- adaptive sample budgeting
- temporal reprojection against previous volumetric history
- depth-aware resolve upsampling

This is the current production-oriented path for bounded fog and atmosphere work in the engine.

## Current Limits

The system is reusable, but it still has real limits:

- primary-light-driven shading only
- no dedicated volumetric shadow integration yet
- no infinite height fog model
- no water surface model
- no author-facing debug visualization for history or upsample state

If a new effect needs large-scale participating media across the whole scene, extend this path carefully rather than falling back to transparent voxels.

## Editing Rules

When changing analytic media behavior:

- update ECS normalization, bridge upload, GPU packing, and WGSL structs together
- update `app_medium.go` bind group layouts if shader bindings change
- update `app_pipelines.go` resolve bindings if volumetric resolve inputs change
- update `runtime.md` when frame order, targets, or compositing behavior changes

If you only tweak atmosphere appearance, prefer preset changes first.
