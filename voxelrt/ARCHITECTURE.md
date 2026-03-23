# VoxelRT Documentation

VoxelRT is the voxel renderer used by `gekko`. The renderer is split across an ECS bridge, a WebGPU app layer, a GPU resource manager, CPU-side scene/culling code, sparse voxel storage, and WGSL shader modules.

## Start Here

| Task | Start with | Then inspect |
| --- | --- | --- |
| Understand renderer ownership and safe edit zones | `rt/AGENT_GUIDE.md` | `rt/RENDERER.md` |
| Follow the current frame graph | `rt/RENDERER.md` | `rt/app/app_frame.go` |
| Change ECS-to-renderer sync or picking/edit APIs | `rt/EDITOR.md` | `../mod_voxelrt_client.go`, `../mod_voxelrt_client_systems.go` |
| Change particles, atlases, or emitter upload | `rt/PARTICLES.md` | `rt/app/app_particles.go`, `rt/gpu/manager_particles.go` |
| Verify a renderer change | `rt/VERIFY.md` | package tests under `rt/core`, `rt/gpu`, `rt/volume`, `rt/bvh` |
| Change shader bindings or pass-specific logic | `rt/shaders/*.wgsl` | `rt/app/app_pipelines.go`, `rt/shaders/shaders.go` |

## Core Docs

- `rt/AGENT_GUIDE.md`: agent-oriented map of ownership boundaries, edit-safe areas, invariants, and failure modes.
- `rt/RENDERER.md`: authoritative current renderer architecture and frame graph.
- `rt/VERIFY.md`: targeted verification commands for renderer work.
- `rt/EDITOR.md`: picking, raycast, and voxel-editing behavior exposed through the bridge.
- `rt/PARTICLES.md`: current hybrid particle pipeline and bridge/runtime constraints.
- `rt/RENDERER_ANALYSIS.md`: analysis and roadmap notes, not the source of truth for current behavior.

## Shader Sources

The WGSL files plus the pipeline setup code are now the source of truth. The stale per-shader markdown docs were removed to avoid future drift.

- `rt/shaders/*.wgsl`
- `rt/shaders/shaders.go`
- `rt/app/app_pipelines.go`
- `rt/app/app_ca.go`
- `rt/app/app_surface_text.go`

## Code Layout

- `../mod_voxelrt_client*.go`: ECS bridge, renderer-side identity maps, helper APIs, and per-frame sync.
- `rt/app/`: WebGPU app lifetime, pass scheduling, resize handling, and render loop orchestration.
- `rt/gpu/`: GPU buffers, textures, bind groups, allocators, Hi-Z, shadows, particles, sprites, skybox, and CA volumes.
- `rt/core/`: scene model, lights, camera, culling, raycast, gizmos, and text primitives.
- `rt/volume/`: sparse voxel storage (`XBrickMap`), editing, compression, and traversal.
- `rt/bvh/`: CPU TLAS builder used by the renderer.

## Current Reality Checks

- The opaque lighting output is `RGBA16Float`, not `RGBA8`.
- The runtime accumulation pass currently includes CA volumes, transparent voxels, particles, and sprites.
- Picking and voxel edits still use CPU-side `Scene` and `XBrickMap` data even when GPU-side rendering is active.
- `ARCHITECTURE.md` is the landing page only. For actual runtime behavior, prefer `rt/RENDERER.md`.
