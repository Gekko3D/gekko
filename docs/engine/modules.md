# Engine Modules

This page maps the major `gekko` modules to their resources, systems, and main responsibilities.

It is meant for agent-driven changes: find the owning module first, then read that module's systems before changing lower-level code.

For the runtime model those modules plug into, see [`runtime.md`](runtime.md).

## Core Infrastructure Modules

### `TimeModule`

- File: `mod_time.go`
- Resources:
  - `*Time`
- Systems:
  - `timeSystem` in `Prelude`
- Owns:
  - frame delta and wall-clock time

### `InputModule`

- File: `mod_input.go`
- Resources:
  - `*Input`
- Systems:
  - `inputSystem` in `PreUpdate`
- Owns:
  - keyboard, mouse, scroll, text input, window dimensions, cursor capture
- Depends on:
  - `*WindowState` from a rendering/window module

### `HierarchyModule`

- File: `mod_hierarchy.go`
- Resources:
  - none
- Systems:
  - `TransformHierarchySystem` in `PostUpdate`
- Owns:
  - propagation from `LocalTransformComponent` plus `Parent` to `TransformComponent`
- Important:
  - parent voxel pivot and voxel resolution affect child world transforms

## Spatial and Streaming Support

### `SpatialGridModule`

- File: `mod_spatialgrid.go`
- Resources:
  - `*SpatialHashGrid`
- Systems:
  - `UpdateAABBsSystem` in `PreUpdate`
  - `UpdateSpatialGridSystem` in `PreUpdate`
- Owns:
  - broadphase AABB grid for queries and neighborhood lookups
- Important:
  - install this module whenever ECS systems need `*SpatialHashGrid` through dependency injection
  - `PhysicsModule` also uses a spatial grid internally, but that simulator-owned grid is not registered as an ECS resource
  - if game code performs same-frame local-space rebases after `PreUpdate`, it may need to refresh AABBs/grid state immediately after the reprojected transform jump instead of waiting for the next frame

### `ChunkObserverModule`

- File: `mod_chunking.go`
- Resources:
  - `*ChunkTrackerResource`
- Systems:
  - `UpdateChunkObserversSystem` in `PreUpdate`
- Owns:
  - generic chunk observer bookkeeping and callback-driven chunk load/unload decisions

### `StreamedLevelRuntimeModule`

- File: `streamed_level_runtime.go`
- Resources:
  - `*StreamedLevelRuntimeState`
- Systems:
  - `updateStreamedLevelObserverSystem` in `PreUpdate`
  - `commitPreparedStreamedChunksSystem` in `Update`
- Owns:
  - chunked level loading, terrain streaming, imported base-world streaming, placement chunking, world-delta application
- Best paired with:
  - `ChunkObserverModule`
  - content-loading and authored-level code paths

## Asset and Content Modules

### `AssetServerModule`

- File: `mod_assets.go`
- Resources:
  - `*AssetServer`
- Systems:
  - none
- Owns:
  - runtime asset registries for voxel models, palettes, textures, materials, meshes, samplers, and VOX files

### Authored Asset and Level Spawn Paths

These are not separate `Module` implementations, but they are major integration surfaces:

- `asset_content_spawn.go`
  - spawns `.gkasset` hierarchies
- `level_content_spawn.go`
  - eager whole-level spawn from `.gklevel`
- `runtime_content_loader.go`
  - cached loading of authored content files

For their data model, see:

- [`../content/game-assets.md`](../content/game-assets.md)
- [`../content/levels.md`](../content/levels.md)
- [`../content/streaming-and-worlds.md`](../content/streaming-and-worlds.md)

## Physics and Gameplay Modules

### `PhysicsModule`

- File: `mod_physics_module.go`
- Resources:
  - `*PhysicsWorld`
  - `*PhysicsProxy`
  - `*PhysicsSimulator` in synchronous mode
- Systems:
  - `PhysicsPullSystem` in `PreUpdate`
  - `PhysicsPushSystem` in `PostUpdate` for async mode
  - `SynchronousPhysicsSystem` in `PhysicsUpdate` for sync mode
- Owns:
  - physics snapshot bridge between ECS state and simulation state
- Important:
  - async mode runs simulation in its own goroutine
  - sync mode steps physics inside the fixed-update schedule
  - ECS-facing state is synchronized through snapshots and results, not direct mutation
  - physics owns local simulation only; large-world authoritative coordinates must remain above the engine and be projected into ECS space by the game/runtime layer

### `VoxPhysicsModule`

- File: `mod_vox_physics.go`
- Resources:
  - `*VoxelGridCache`
- Systems:
  - `VoxPhysicsPreCalcSystem`
- Owns:
  - voxel-aware physics preparation and collision helpers
- Important:
  - remains the authoritative cached voxel-physics preparation path
  - the physics bridge can bootstrap fallback voxel models/pivots on first tick, but that is a robustness path, not a replacement for the cache

### `DestructionModule`

- File: `mod_destruction.go`
- Resources:
  - `*DestructionQueue`
- Systems:
  - `destructionSystem`
- Owns:
  - queued voxel destruction operations
- Depends heavily on:
  - `*VoxelRtState`
  - `*AssetServer`

### `LifecycleModule`

- File: `mod_lifecycle.go`
- Resources:
  - none
- Systems:
  - `lifetimeSystem`
  - `debrisCleanupSystem`
- Owns:
  - time-based cleanup and entity lifetime expiration

### `GroundedPlayerControllerModule`

- File: `mod_grounded_player.go`
- Resources:
  - optional `*GroundedPlayerControllerDefaults`
- Systems:
  - `groundedPlayerInputSystem`
  - `groundedPlayerControlSystem`
- Owns:
  - grounded first-person controller behavior
- Depends on:
  - `*Input`
  - `*Time`
  - `*VoxelRtState`

### `FlyingCameraModule`

- File: `mod_flying_camera.go`
- Resources:
  - none
- Systems:
  - `FlyingCameraInputSystem`
  - `FlyingCameraControlSystem`
- Owns:
  - free-fly camera movement and look controls

## UI Modules

### `UiModule`

- Files:
  - `mod_ui.go`
  - `mod_ui_retained.go`
- Resources:
  - `*UiRuntime`
- Systems:
  - `uiPanelInputSystem` in `PreUpdate`
  - `uiPanelRenderSystem` in `PostUpdate`
- Owns:
  - retained-mode UI runtime, hit testing, and panel drawing
- Depends on:
  - `*VoxelRtState`
  - `*Input`

## Rendering Modules

### `VoxelRtModule`

- Files:
  - `mod_voxelrt_client.go`
  - `mod_voxelrt_client_systems.go`
- Resources:
  - `*WindowState`
  - `*VoxelRtState`
  - `*WaterInteractionState`
  - `*Profiler`
- Systems:
  - `voxelRtDebugSystem`
  - `caStepSystem`
  - `waterInteractionSystem`
  - `waterInteractionCleanupSystem`
  - `voxelRtPreludeSystem`
  - `voxelRtSystem`
  - `voxelRtUpdateSystem`
  - `voxelRtRenderSystem`
- Owns:
  - the main modern renderer bridge and renderer lifetime

### `WaterEffectsModule`

- Files:
  - `mod_water_effects.go`
- Resources:
  - `*WaterEffectsState`
- Systems:
  - `waterSplashEffectsSystem` in `PostUpdate`
- Owns:
  - optional presentation-layer reactions to `WaterImpactEvent`
  - default splash particle spawning for water surfaces
- Depends on:
  - `*WaterInteractionState`

Read next:

- [`../renderer/overview.md`](../renderer/overview.md)
- [`../renderer/change-guide.md`](../renderer/change-guide.md)

### Legacy Render Paths

These still exist in the tree but are not the main path documented elsewhere:

- `ClientModule` in `mod_client.go`
  - older generic WebGPU render path
- `ServerModule` in `mod_server.go`
  - currently empty

Agents should prefer `VoxelRtModule` unless they are explicitly working on legacy rendering code.

## Choosing Where To Edit

When a behavior crosses subsystems:

- first identify which module owns the ECS-facing system
- then inspect the lower-level package it delegates to
- only then touch shared runtime code

Examples:

- renderer visuals wrong
  - start in `VoxelRtModule` bridge code before touching `voxelrt/rt/...`
- transformed child entities wrong
  - start in `HierarchyModule`
- chunk streaming wrong
  - start in `ChunkObserverModule` or `StreamedLevelRuntimeModule`
- authored asset spawn wrong
  - start in `asset_content_spawn.go`, not the renderer
