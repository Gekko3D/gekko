# VoxelRT Overview

VoxelRT is the voxel renderer used by `gekko`. The renderer is split across an ECS bridge, an app layer that owns WebGPU lifetime and pass scheduling, a GPU resource manager, CPU-side scene and culling code, sparse voxel storage, and WGSL shaders.

This page is the renderer landing page. For current runtime behavior, use [`runtime.md`](runtime.md).

## Start Here

| Task | Read first | Then inspect |
| --- | --- | --- |
| Understand renderer ownership and safe edit zones | [`change-guide.md`](change-guide.md) | [`runtime.md`](runtime.md) |
| Follow the current frame graph | [`runtime.md`](runtime.md) | `voxelrt/rt/app/app_frame.go` |
| Change voxel normals, voxel lighting style, or shaded voxel look | [`runtime.md`](runtime.md) | [`change-guide.md`](change-guide.md), `voxelrt/rt/shaders/gbuffer.wgsl`, `voxelrt/rt/shaders/deferred_lighting.wgsl` |
| Change ECS-to-renderer sync or picking/edit APIs | [`editing.md`](editing.md) | `mod_voxelrt_client.go`, `mod_voxelrt_client_systems.go` |
| Change atmosphere, fog, or bounded volumetric media | [`media.md`](media.md) | `analytic_medium_ecs.go`, `analytic_medium_presets.go`, `mod_voxelrt_client_systems.go`, `voxelrt/rt/app/feature_analytic_medium.go` |
| Change particles, atlases, or emitter upload | [`particles.md`](particles.md) | `voxelrt/rt/app/app_particles.go`, `voxelrt/rt/gpu/manager_particles.go` |
| Verify a renderer change | [`verification.md`](verification.md) | package tests under `voxelrt/rt/core`, `voxelrt/rt/gpu`, `voxelrt/rt/volume`, `voxelrt/rt/bvh` |
| Plan larger lighting or GI work | [`../roadmaps/renderer-lighting.md`](../roadmaps/renderer-lighting.md) | `voxelrt/rt/app`, `voxelrt/rt/gpu`, `voxelrt/rt/shaders` |

## Core Docs

- [`runtime.md`](runtime.md)
  - authoritative frame flow, resource ownership, and live renderer behavior
- [`gbuffer-compaction-note.md`](gbuffer-compaction-note.md)
  - Task I design note for the current compacted G-buffer layout and position reconstruction path
- [`change-guide.md`](change-guide.md)
  - ownership boundaries, invalidation rules, and common failure modes when editing renderer code
- [`editing.md`](editing.md)
  - picking, raycast, and voxel-edit behavior exposed through the bridge
- [`media.md`](media.md)
  - bounded volumetric atmosphere and fog path, presets, limits, and integration points
- [`particles.md`](particles.md)
  - current hybrid particle pipeline and bridge/runtime constraints
- [`verification.md`](verification.md)
  - targeted test and smoke-check commands

## Code Layout

- `mod_voxelrt_client*.go`
  - ECS bridge, renderer-facing APIs, identity maps, and per-frame sync
- `voxelrt/rt/app/`
  - WebGPU app lifetime, pass scheduling, resize handling, and render loop orchestration
- `voxelrt/rt/gpu/`
  - GPU buffers, textures, bind groups, paged voxel payload atlases, shadows, Hi-Z, particles, sprites, analytic media, CA volumes, and probe GI resources
- `voxelrt/rt/core/`
  - scene model, camera, lights, culling, raycast, gizmos, and text primitives
- `voxelrt/rt/volume/`
  - sparse voxel storage, editing, compression, and traversal
- `voxelrt/rt/bvh/`
  - CPU-side TLAS builder used by the renderer
- `voxelrt/rt/shaders/`
  - WGSL shader sources; when pipeline details disagree with prose, prefer shader code plus `app/app_pipelines.go`

## Reality Checks

- The opaque lighting target is `RGBA16Float`.
- The live compositor is the resolve path, not the legacy fullscreen blit pipeline.
- Picking and voxel edits are still CPU-authoritative through `Scene` and `XBrickMap`.
- Probe GI is implemented and participates in the live frame graph when enabled.
- Bounded atmosphere and fog use the analytic media path, not transparent voxels.
