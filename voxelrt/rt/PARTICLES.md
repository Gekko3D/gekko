# VoxelRT Particles: Design and Integration

This document describes the current particle system implemented in VoxelRT, including ECS components, CPU simulation, data structures, GPU upload, render pipeline, and the optional Cellular Automaton (CA) bridge.

The goal is a “cheap” and ECS-first solution:
- CPU-only simulation (no GPU compute).
- No per-particle ECS entities; gameplay manipulates only emitter/volume components.
- One instanced draw after deferred lighting with additive blending.
- Optional CA simulation that feeds particles (for smoke/fire-like volumetric behavior).

## Overview

- CPU simulation:
  - One pool per emitter (SoA arrays), no allocation each frame.
  - O(N) integration with gravity, drag, lifetime.
  - Emission in a cone aligned with emitter rotation.
- GPU render:
  - Instanced camera-facing billboards.
  - Manual depth testing against GBuffer depth.
  - Additive blending after deferred lighting blit.
- Optional CA:
  - Small 3D grid (e.g. 32–64) updated at low Hz.
  - Emits “puffs” via the same billboard rendering path.

## Key Files

- ECS and simulation:
  - gekko/particles_ecs.go
  - gekko/ca_ecs.go
  - gekko/mod_vox_rt.go (wires systems and uploads to the renderer)
- Renderer:
  - gekko/voxelrt/rt/app/app.go (pipeline creation, pass ordering)
  - gekko/voxelrt/rt/gpu/manager.go (GPU upload of particle instances)
  - gekko/voxelrt/rt/shaders/particles_billboard.wgsl (VS/FS for particles)
- Demo scene (example usage):
  - actiongame/src/modules/playing/playing.go

## ECS Components

### ParticleEmitterComponent

Location: gekko/particles_ecs.go

Fields:
- Enabled: bool
- MaxParticles: int
- SpawnRate: float32 (particles/sec)
- LifetimeRange: [2]float32
- StartSpeedRange: [2]float32
- StartSizeRange: [2]float32
- StartColorMin/Max: [4]float32 (RGBA 0..1)
- Gravity: float32
- Drag: float32
- ConeAngleDegrees: float32 (spread around emitter’s “up” axis)

Emitters are discovered per-frame via ECS query. The simulation is done in one function that packs all alive particles into a single slice of instances for rendering.

### CellularVolumeComponent (Optional CA)

Location: gekko/ca_ecs.go

Fields:
- Resolution: [3]int (grid size, e.g. {32,48,32})
- Type: CellularSmoke | CellularFire | CellularSand | CellularWater
- TickRate: float32 (Hz)
- Diffusion, Buoyancy, Cooling, Dissipation: float32
- BridgeToParticles: bool (if true, emits billboards for active cells)

Internals:
- _density []float32: density per cell
- _temp []float32: temperature (for fire)
- _accum float32: time accumulator
- _inited bool: initialized guard

The CA is stepped at low Hz by caStepSystem. If BridgeToParticles is true, “active” cells are sampled and appended into the same ParticleInstance array used by emitters.

## Systems

### caStepSystem

Location: gekko/ca_ecs.go

- Runs in Update stage (registered in mod_vox_rt.go).
- Accumulates dt and steps the grid at ~TickRate Hz.
- Current rules:
  - Smoke/Fire: 6-neighbor diffusion with buoyancy, plus dissipation; fire optionally cools over time.
  - Sand/Water: placeholders for future work.
- A small seed plume is injected periodically to keep smoke/fire alive in demos.

### particlesCollect

Location: gekko/particles_ecs.go

- Queries ParticleEmitterComponent + TransformComponent.
- Ensures a per-emitter pool (SoA arrays) exists and has correct capacity.
- Spawns new particles based on SpawnRate and Lifetime/Speed/Size/Color ranges.
- Integrates gravity and drag and performs lifetime culling via swap-remove.
- Packs alive particles into []core.ParticleInstance.
- Also calls bridgeCellsToParticles to append CA-driven instances (cap is tunable).

### voxelRtSystem (integration point)

Location: gekko/mod_vox_rt.go

- Batches voxel updates.
- Calls particlesCollect(state, time, cmd).
- Uploads instances to GPU via BufferManager.UpdateParticles.
- Proceeds to rtApp.Update()/Render().

## Data Structures

### Per-emitter pool (SoA)

In gekko/particles_ecs.go:
- pos []mgl32.Vec3
- vel []mgl32.Vec3
- age []float32
- life []float32
- size []float32
- color [][4]float32
- alive int, spawnAcc float32, capacity int

Benefits:
- Tight arrays for CPU integration (cache friendly).
- No per-particle ECS entities; only emitters are in ECS.

### GPU instance layout

In gekko/voxelrt/rt/core/particle.go:
- ParticleInstance { vec3 pos; float size; vec4 color } (32 bytes)

This is the structure uploaded to GPU each frame.

## GPU Upload

Location: gekko/voxelrt/rt/gpu/manager.go

- UpdateParticles(instances []core.ParticleInstance):
  - Ensures/re-allocates a storage buffer for instance data (min size 32 bytes to keep bindings valid).
  - Writes instance data once per frame.
  - Stores ParticleCount.

- CreateParticlesBindGroups(pipeline *wgpu.RenderPipeline):
  - Group 0: Camera uniform + instances storage.
  - Group 1: GBuffer depth texture view (RGBA32Float).
  - Note: The depth texture is sampled as UnfilterableFloat, so pipeline/bind group layout matches that sample type.

## Render Pipeline

Location: gekko/voxelrt/rt/app/app.go

- setupParticlesPipeline:
  - Creates shader module for particles_billboard.wgsl.
  - Explicit bind group layouts:
    - BGL0: camera uniform (binding 0, VS/FS visibility), instances storage (binding 1, VS).
    - BGL1: depth texture (binding 0, FS) with SampleType = UnfilterableFloat (matches RGBA32Float GBuffer depth).
  - Render pipeline:
    - Triangle list, additive blend (src=one, dst=one), writes only color.

- Render order in Render():
  1) GBuffer compute pass
  2) Shadow pass
  3) Deferred lighting pass → out_color (screen)
  4) Debug pass (optional)
  5) Fullscreen blit to swapchain
  6) Particles pass (additive, uses GBuffer depth for manual test)
  7) Text pass

The particles pass binds Groups 0 and 1 each frame and draws 6 vertices per instance (two triangles per quad).

## Shader

Location: gekko/voxelrt/rt/shaders/particles_billboard.wgsl

- Vertex:
  - Takes instance i, expands a camera-facing quad in world-space using camera.inv_view to compute right/up.
  - Outputs color, world_pos, and quad_uv.

- Fragment:
  - Reconstructs camera ray direction from pixel position and camera matrices.
  - Computes t_particle via dot(world_pos - cam_pos, dir).
  - Loads scene t from GBuffer depth (RGBA32Float) and compares.
  - Discards if particle is behind scene geometry (t_particle > t_scene - epsilon).
  - Uses circular mask and soft edge (smoothstep) to shape the billboard.
  - Emits rgb premultiplied for additive blending; alpha is 0.

Tuning:
- Edge softness: adjust smoothstep thresholds in FS.
- Depth epsilon: increase slightly if particles disappear at grazing angles.

## CA → Particles Bridge

Location: gekko/ca_ecs.go (bridgeCellsToParticles)

- For each enabled CA volume:
  - Samples grid cells based on a density threshold and a stride (downsampling).
  - Computes world position from Transform (unit cell size × max scale component).
  - Appends ParticleInstance with a chosen size and color (smoke/fire presets).
- Controls:
  - threshold: density threshold to consider a cell active.
  - stride: sub-sampling step across the grid.
  - cap: maximum appended per-frame to prevent spikes.

## Example ECS Usage

Spawn a spark emitter:
```go
cmd.AddEntity(
  &TransformComponent{ Position: mgl32.Vec3{0,25,60} },
  &ParticleEmitterComponent{
    Enabled: true,
    MaxParticles: 2000,
    SpawnRate: 600,
    LifetimeRange: [2]float32{0.7,1.4},
    StartSpeedRange: [2]float32{6,14},
    StartSizeRange: [2]float32{0.25,0.6},
    StartColorMin: [4]float32{1.0, 0.6, 0.2, 1.0},
    StartColorMax: [4]float32{1.0, 0.2, 0.0, 1.0},
    Gravity: 9.8,
    Drag: 0.15,
    ConeAngleDegrees: 20,
  },
)
```

Spawn a CA smoke volume:
```go
cmd.AddEntity(
  &TransformComponent{ Position: mgl32.Vec3{0,40,60}, Scale: mgl32.Vec3{1,1,1} },
  &CellularVolumeComponent{
    Resolution: [3]int{32,48,32},
    Type: CellularSmoke,
    TickRate: 15,
    Diffusion: 0.25, Buoyancy: 0.6, Dissipation: 0.02,
    BridgeToParticles: true,
  },
)
```

## Troubleshooting

- Particles not visible at some camera angles:
  - Increase depth epsilon in the FS shader (discard test margin).
  - Ensure particles are drawn after deferred lighting and blit.
  - Increase billboard size or edge softness (smoothstep) to avoid thin silhouettes.
  - Verify BGL1 for particles uses SampleType = UnfilterableFloat to sample RGBA32Float depth.

- Overdraw too heavy:
  - Reduce emitter SpawnRate, MaxParticles.
  - Increase lifetime slightly instead of spawn rate to maintain presence.
  - Increase CA bridge stride or threshold to reduce billboards.

- Visual artifacts (hard edges, halo):
  - Adjust the circular mask and smoothstep range in the fragment shader.
  - Slightly premultiply color differently, e.g. clamp color values < 1.0 for smoke.

## Performance Notes

- CPU:
  - SoA pool per emitter, O(N) per frame, minimal branching.
  - CA steps at low Hz (TickRate) and uses a small grid.
- GPU:
  - One instanced draw for all particles; additive blend is simple.
  - No per-particle shadows/lighting.

## Current Limitations

- Additive-only transparency (no sorting).
- Manual depth test; tune epsilon per-scene scale.
- CA currently implements smoke/fire rules; sand/water are placeholders.

## Debug aids

- The earlier hardcoded cube in app.Init was removed to keep ECS-only scenes. All content is now spawned from ECS (e.g., via actiongame playing module). If you need a debug primitive again, use an ECS entity instead of hardcoding in the renderer.

## Roadmap (Optional)

- Size/color over lifetime curves.
- Ground collision/bounce for sparks.
- Sand/water CA rules.
- Frustum culling per emitter and CA volume.
- Optional low-res emissive voxel volume render for volumetrics.
