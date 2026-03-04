# VoxelRT Particles: Current Pipeline

This document describes the current particle flow after the renderer/module refactor.

## Overview

- ECS controls emitters via `ParticleEmitterComponent`.
- CPU builds emitter parameter buffers and spawn requests each frame.
- GPU compute simulates particle state in global pools.
- GPU render draws alive particles as billboards into the WBOIT accumulation targets.
- Resolve pass composites opaque + transparent layers.

## Key Files

- ECS and bridge:
  - `gekko/particles_ecs.go`
  - `gekko/mod_voxelrt_client_systems.go`
- Renderer app:
  - `gekko/voxelrt/rt/app/app.go`
  - `gekko/voxelrt/rt/app/app_frame.go`
  - `gekko/voxelrt/rt/app/app_pipelines.go`
  - `gekko/voxelrt/rt/app/app_particles.go`
- GPU manager:
  - `gekko/voxelrt/rt/gpu/manager.go`
  - `gekko/voxelrt/rt/gpu/manager_particles.go`
- Shaders:
  - `gekko/voxelrt/rt/shaders/particles_sim.wgsl`
  - `gekko/voxelrt/rt/shaders/particles_billboard.wgsl`
  - `gekko/voxelrt/rt/shaders/resolve_transparency.wgsl`

## ECS Side

`particlesSync(...)` in `particles_ecs.go`:

- queries `TransformComponent + ParticleEmitterComponent`
- computes per-emitter spawn counts from `SpawnRate * dt`
- packs `EmitterParams` into a byte buffer
- emits spawn requests as emitter indices
- returns `(spawnRequests, emitterBytes, emitterCount, atlasAssetId)`

The bridge in `voxelRtSystem(...)` then:

- updates particle atlas texture (when changed)
- writes sim params (`UpdateParticleParams`)
- ensures particle buffers (`UpdateParticles(maxCount, emitters)`)
- uploads spawn requests (`UpdateSpawnRequests`)
- recreates particle bind groups when required

## GPU Side

`GpuBufferManager` owns:

- particle pools and counters
- particle indirect draw args
- spawn request + emitter buffers
- bind groups for sim and render pipelines

Compute dispatch order per frame:

1. init (`init_draw_args`)
2. simulate (`simulate`)
3. spawn (`spawn`) if requests exist
4. finalize (`finalize_draw_args`)

Then render uses indirect draw from alive particle count.

## Render Integration

Frame order in `App.Render()`:

1. G-Buffer
2. Shadow
3. Deferred lighting
4. Optional debug
5. Transparent accumulation:
   - transparent voxel overlay
   - particle billboards
6. Resolve transparency to swapchain
7. Text overlay

Particles write weighted contributions into:

- `TransparentAccumTex` (`RGBA16Float`)
- `TransparentWeightTex` (`R16Float`)

## Notes

- Particle simulation is GPU-driven, not CPU-integrated per particle.
- `ParticleEmitterComponent.Texture` selects an atlas asset; first active emitter atlas is currently used as the runtime atlas source.
- `BridgeToParticles` fields in CA components exist, but current production path is emitter-driven GPU simulation.
