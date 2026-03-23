# VoxelRT Particles: Current Pipeline

This document describes the current hybrid particle path.

## Overview

- ECS controls emitters through `ParticleEmitterComponent`.
- CPU builds emitter parameter buffers and spawn requests each frame.
- GPU compute simulates per-particle state in global pools.
- GPU render draws alive particles as billboards into the WBOIT accumulation targets.
- Resolve composites the opaque lighting output with transparent accumulation.

## Key Files

- ECS and bridge:
  - `gekko/particles_ecs.go`
  - `gekko/mod_voxelrt_client_systems.go`
- Renderer app:
  - `gekko/voxelrt/rt/app/app.go`
  - `gekko/voxelrt/rt/app/app_frame.go`
  - `gekko/voxelrt/rt/app/app_particles.go`
  - `gekko/voxelrt/rt/app/app_pipelines.go`
- GPU manager:
  - `gekko/voxelrt/rt/gpu/manager.go`
  - `gekko/voxelrt/rt/gpu/manager_particles.go`
- Shaders:
  - `gekko/voxelrt/rt/shaders/particles_sim.wgsl`
  - `gekko/voxelrt/rt/shaders/particles_billboard.wgsl`
  - `gekko/voxelrt/rt/shaders/resolve_transparency.wgsl`

## ECS And Bridge Side

`particlesSync(...)` in `particles_ecs.go`:

- queries `TransformComponent + ParticleEmitterComponent`
- computes per-emitter spawn counts from `SpawnRate * dt`
- packs `EmitterParams`
- emits spawn requests as emitter indices
- returns `(spawnRequests, emitterBytes, emitterCount, atlasAssetId)`

The bridge then:

- updates the particle atlas if the active atlas changes
- writes sim params
- ensures particle buffers exist with `UpdateParticles(...)`
- uploads spawn requests
- keeps particle bind groups valid across resource changes

Current practical constraints:

- emitter distance cull is currently 200 world units
- per-emitter spawn burst is capped to 1024 per frame
- bridge-side pool provisioning currently uses `UpdateParticles(1000000, ...)`
- the first active emitter atlas wins for the frame

## GPU Side

`GpuBufferManager` owns:

- particle pools and counters
- indirect draw args
- emitter and spawn-request buffers
- sim bind groups
- render bind groups

Compute dispatch order per frame:

1. `init_draw_args`
2. `simulate`
3. `spawn` when requests exist
4. `finalize_draw_args`

Render then uses indirect draw arguments from the current alive-particle state.

## Important Coupling

Particle sim is not isolated from the rest of the renderer. The sim path also binds current scene voxel resources, including sector, brick, payload, material, object-param, instance, and sector-grid data.

If scene buffers are recreated, particle sim bindings may need to be recreated too.

## Render Integration

Particles run inside the current renderer frame as:

1. particle sim
2. CA sim
3. G-buffer
4. Hi-Z
5. shadows
6. deferred lighting
7. optional debug
8. accumulation:
   - CA volumes
   - transparent voxel overlay
   - particles
   - sprites
9. resolve
10. text and gizmos

Particles write weighted contributions into:

- `TransparentAccumTex` (`RGBA16Float`)
- `TransparentWeightTex` (`R16Float`)

## Notes

- Per-particle simulation is GPU-driven.
- Atlas selection is currently one-atlas-per-frame through the bridge.
- `BridgeToParticles` fields in CA components exist, but the production path is emitter-driven GPU simulation.
