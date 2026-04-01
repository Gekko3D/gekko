# VoxelRT Runtime

This document describes the current renderer architecture and frame graph. It is the renderer source of truth for live behavior.

Related docs:

- [`overview.md`](overview.md)
- [`change-guide.md`](change-guide.md)
- [`editing.md`](editing.md)
- [`gbuffer-compaction-note.md`](gbuffer-compaction-note.md)
- [`particles.md`](particles.md)
- [`verification.md`](verification.md)

## Ownership Boundaries

- ECS bridge: `mod_voxelrt_client*.go`
  - creates and synchronizes renderer-side objects, lights, camera state, text, gizmos, particles, sprites, skybox inputs, and CA volumes
- `app.App`: `voxelrt/rt/app/`
  - owns WebGPU device and surface lifetime, render pipelines, resize flow, opaque storage output, and pass scheduling
- `gpu.GpuBufferManager`: `voxelrt/rt/gpu/`
  - owns G-buffer textures, WBOIT targets, shadow maps, Hi-Z, scene buffers, voxel atlas resources, and most cached bind groups
- `core.Scene` and friends: `voxelrt/rt/core/`
  - own CPU scene state, camera and light math, culling, raycast, gizmos, and text primitives
- `volume.XBrickMap`: `voxelrt/rt/volume/`
  - owns sparse voxel storage, edit semantics, dirty tracking, traversal, and compression

## Engine Stage Flow

The renderer participates in three engine stages:

1. `Prelude`
   - input sync
   - text clear
   - `BufferManager.BeginBatch()`
2. `PreRender`
   - ECS-to-renderer sync in `voxelRtSystem`
   - `RtApp.Update()`
3. `Render`
   - `RtApp.Render()`

That split matters because bridge sync and GPU uploads happen before render-pass execution.

## `App.Update()`

`Update()` is the per-frame CPU preparation step. It currently:

1. builds view and projection matrices
2. reads the previous Hi-Z snapshot
3. runs `Scene.Commit(...)` with frustum culling and optional Hi-Z occlusion
4. updates profiler counters
5. calls `BufferManager.UpdateScene(...)`
6. rebuilds dependent bind groups if GPU resources were recreated
7. updates camera uniforms
8. refreshes text and gizmo buffers
9. updates probe GI placement, dirty tracking, and bake budget state

Important details:

- Hi-Z uses previous-frame data and is disabled during fast camera motion.
- `Scene.Commit(...)` produces both `VisibleObjects` and `ShadowObjects`.
- `UpdateScene(...)` can grow shadow maps or scene buffers, which forces downstream bind-group recreation.
- Probe GI state is prepared during `Update()` and baked later during `Render()`.

## `App.Render()`

The current frame graph is:

1. particle simulation compute passes
2. CA volume simulation and bounds passes
3. G-buffer compute pass
4. Hi-Z generation compute pass
5. shadow pass
6. probe GI bake compute pass for a capped dirty-probe batch
7. deferred lighting compute pass
8. optional debug compute pass
9. accumulation render pass
   - CA volumes
   - transparent voxel overlay
   - particles
   - sprites
10. resolve render pass
   - resolve transparency over opaque lighting
   - text overlay
   - gizmos

The legacy fullscreen blit pipeline still exists in setup code, but the resolve path is the live compositor.

## Render Targets and Formats

### Opaque lighting output

- `App.StorageTexture`: `RGBA16Float`
- written by deferred lighting
- sampled by the resolve pass

### G-buffer

- depth: `RGBA32Float`
- normal: `RGBA16Float`
- material: `RGBA32Float`
- no dedicated position target
- `GBufferDepth.r` stores hit distance along the camera ray
- `GBufferDepth.gba` stores voxel-center world position for voxel-stable shadowing
- deferred lighting can reconstruct the exact visible hit position from screen UV, camera inverse matrices, and `GBufferDepth.r`
- live opaque shading evaluates view- and direct-light terms from the stored voxel-center world position so each visible voxel shades as one cell
- deferred shadowing also uses the stored voxel-center world position so each visible voxel can receive one stable shadow response per light

#### Voxel shading contract

The voxel renderer intentionally keeps a blocky albedo/material look while allowing voxelized shapes to read with volume under lighting.

- A visible voxel keeps one material identity.
  - Albedo, emissive, and PBR parameters still come from the voxel palette/material lookup. Do not introduce cross-voxel albedo blending to "smooth" the image.
- A visible voxel uses one lighting normal.
  - Hits inside the same voxel should resolve to the same normal so the voxel shades as one cell, more like a pixel block than a triangle surface with interpolated normals.
- The normal should come from local voxel occupancy first.
  - The intended look is faceless microvoxels with shape volume. The live rule is: estimate a normal from neighboring occupied/empty voxels, then fall back only if that gradient is degenerate.
- Do not use object-center or radial fallback normals for shading.
  - Those produce a blobby "inflated" read that is unrelated to the local voxel surface and drift badly on concave or thin shapes.
- Degenerate gradients should still resolve to one per-voxel normal.
  - If the occupancy gradient is degenerate, derive a deterministic fallback from the voxel's exposed-face mask so thin symmetric features do not become view-dependent.
  - Use the hit face / ray entry direction only as a last resort when the occupancy-based fallback is still ambiguous.
- Single-voxel-thick features need two-sided direct lighting.
  - When a voxel is exposed on both sides of an axis, keep its normal deterministic, but evaluate direct point and spot lighting as two-sided so planes and rods still react to local lights from either side.
- Normal transforms must be consistent across traversal paths.
  - `XBrickMap` microvoxels, solid-brick fast paths, and `tree64` LOD hits must all use the same object-space-to-world-space normal rule. Use the inverse-transpose-style transform, especially when non-uniform scale is possible.
- Lighting may vary voxel-to-voxel, but color identity should remain voxel-stable.
  - The renderer can show shape through per-voxel lighting, AO, and shadows, but it should not smear voxel colors into gradients across neighboring voxels.

Current implementation notes:

- The live normal-generation path is in `voxelrt/rt/shaders/gbuffer.wgsl`.
- Neighbor-derived normals are computed from 6-neighbor occupancy samples, terrain chunks included across chunk boundaries.
- The degenerate fallback path is occupancy-based and deterministic per voxel; face-entry is only a last resort.
- Degenerate thin voxels carry a two-sided direct-light flag through the G-buffer so deferred and transparent lighting agree on planes and rods.
- Deferred lighting consumes the stored G-buffer normal directly; the albedo/material lookup stays palette-driven.
- Opaque deferred point and spot lights evaluate attenuation from the stored voxel center, matching the transparent overlay path.
- Shadow softness is controlled separately for directional and spot lights through `LightingQualityConfig.Shadow`.
  - Lower values push toward harder voxel-block shadows.
  - Higher values keep more of the filtered penumbra look.

### Transparency / WBOIT

- accumulation: `RGBA16Float`
- weight: `R16Float`

### Other major resources

- shadow maps: 2D array textures managed by `GpuBufferManager`
- Hi-Z: `R32Float` mip chain built from the G-buffer depth texture at half resolution
- voxel payload atlas:
  - 4 fixed 3D `R8Uint` texture pages
  - each page is capped by `MaxTextureDimension3D` and aligned down to `volume.BrickSize`
  - brick payload records now store packed `atlas_offset` plus `atlas_page`, so voxel consumers must bind all four payload pages together
- probe GI: uniform metadata plus storage buffers for per-probe data, hash-grid lookup, and dirty-bake dispatch

## Scene and Culling Model

- `Scene.Objects` is the authoritative CPU-side object list.
- `Scene.Commit(...)` updates `WorldAABB` values, runs frustum culling, then optionally applies Hi-Z occlusion.
- `VisibleObjects` drives main scene buffers and the camera-facing BVH.
- `ShadowObjects` drives a broader shadow BVH so off-screen casters can still affect visible receivers.

## Specialized Subsystems

### Shadows

- directional shadows use cascades
- spot and directional shadow refresh are scheduled rather than fully rebuilt every frame
- shadow resources live under `voxelrt/rt/gpu/manager_shadow.go`

### Particles

- emitters are authored from ECS
- simulation is GPU-driven
- rendering happens in the accumulation pass
- details are in [`particles.md`](particles.md)

### CA volumes

- simulation and bounds updates run before the main opaque passes
- CA volume rendering happens during accumulation

### Sprites, text, and gizmos

- sprites render during accumulation
- text and gizmos are resolve-pass overlays
- text is frame-lifetime data and must be resubmitted every frame

### Probe GI

- probe GI is live when enabled in config
- active probes are derived from scene regions near the camera
- dirty probes are rebaked with a capped per-frame budget
- deferred lighting samples probe data through a hash-grid lookup

## Resource Recreation Rules

### Resize

`App.Resize()` must recreate or refresh all resources that depend on the surface size or views derived from it. That includes:

- surface configuration
- opaque storage texture
- G-buffer textures
- debug and fullscreen bind groups
- G-buffer, lighting, and shadow bind groups
- probe GI bake bind groups
- transparent-overlay bind groups
- particle, sprite, CA volume, and resolve pipelines

### Scene-resource growth

When `UpdateScene(...)` recreates buffers, `App.Update()` must rebuild dependent bind groups. Renderer bugs after object-count growth or shadow-capacity growth are usually stale-bind-group issues.

Voxel payload uploads follow the same rule. The brick table now uses 20-byte records, and any bind group that reads voxel payload data must be recreated if payload pages or voxel-table resources were recreated.

## Common Sources of Drift

- prose docs describing the old fullscreen blit path as the live compositor
- forgetting that picking and editing still use CPU-side scene data
- changing resize-sensitive resources without updating `Resize()`
- changing scene-buffer layouts without rebuilding dependent bind groups
- changing voxel payload page bindings or `BrickRecord` layout in one pass but not the other voxel consumers
