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

### `AudioModule`

- File: `mod_audio.go`
- Resources:
  - `*AudioState`
- Systems:
  - queued one-shot playback in `PreRender`
- Owns:
  - cached PCM WAV decoding and sample-rate conversion
  - camera-listener distance attenuation
  - optional voxel line-of-sight attenuation
- Important:
  - install after `VoxelRtModule` when `Occlusion` is enabled
  - the engine accepts source-neutral paths, positions, and gains; games own
    cue selection, event timing, and material-to-sound policy
  - playback failures disable only the affected clip or audio device; gameplay
    and AI sensing must not depend on audible output

### `HierarchyModule`

- File: `mod_hierarchy.go`
- Resources:
  - none
- Systems:
  - `TransformHierarchySystem` in `PostUpdate`
- Owns:
  - propagation from `LocalTransformComponent` plus `Parent` to `TransformComponent`
- Important:
  - hierarchy composition is pure entity transform math: parent position,
    rotation, and scale affect children
  - voxel renderer pivots do not affect child world transforms; pivots only
    affect how a voxel model is drawn around its entity origin
  - if a voxel-authored asset needs a separate visual origin, express it as a
    child voxel part under a `group` pivot rather than relying on renderer pivot
    side effects

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
  - `*VoxelWorldDirtyChunks`
- Systems:
  - `updateStreamedLevelObserverSystem` in `PreUpdate`
  - `commitPreparedStreamedChunksSystem` in `Update`
  - `streamedLevelNavigationSystem` in `Update`
  - `streamedLevelRuntimeEditedNavigationSystem` in `PostUpdate`
- Owns:
  - chunked level loading, terrain streaming, imported base-world streaming, placement chunking, world-delta application, voxel graph residency, and revisioned delta rebuilds
    - explicit `StartStreamedLevelRuntime`, `StopStreamedLevelRuntime`, and
      `RestartStreamedLevelRuntime` lifecycle; stop drains background prepare and
      navigation work before owned entities are removed
  - `StreamedLevelDeltaPersistent` for the level's normal `.gkworlddelta`, or
    `StreamedLevelDeltaFresh` for an engine-owned temporary session delta that
    is removed on stop without touching persistent user data
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
  - palette-owned source-neutral surface metadata used by raycast material lookup

### Authored Asset and Level Spawn Paths

These are not separate `Module` implementations, but they are major integration surfaces:

- `asset_content_spawn.go`
  - spawns `.gkasset` hierarchies
- `level_content_spawn.go`
  - eager whole-level spawn from `.gklevel`
- `runtime_content_loader.go`
  - cached loading of authored content files
- `asset_animation.go`
  - advances authored `.gkasset` animation clips through
    `AnimationPlayerComponent`
  - applies sampled keys to `LocalTransformComponent` targets identified by
    authored item IDs

### `AnimationModule`

- File: `asset_animation.go`
- Resources:
  - none
- Systems:
  - `npcAnimationSystem` in `Update`
  - `assetAnimationSystem` in `Update`
- Owns:
  - semantic NPC animation state selection for attached authored asset visuals
  - authored asset clip playback for spawned `.gkasset` hierarchies
  - bind-pose base-clip sampling plus ordered masked override/additive layers
    for local position, rotation, and scale keys
- Important:
  - NPC animation states such as `idle`, `walk`, `run`, `attack`, `pain`, and
    `death` resolve to the best available imported clip by stable name/tag
    matching, with fallback to the authored default clip
  - animation tracks are local-space authored transforms
  - layers use authored item-ID masks and can lock root position keys for
    controller-driven actors; they do not provide an animation graph or
    runtime retargeting
  - omitted channels keep the asset's bind transform
  - `HierarchyModule` resolves the resulting local transforms to world
    transforms after animation has run

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
  - runtime collision grids rebuild immutable copy-on-write sector snapshots
    from map revisions; physics must not clear renderer dirty-brick markers
  - the physics bridge can bootstrap fallback voxel models/pivots on first tick, but that is a robustness path, not a replacement for the cache

### `DestructionModule`

- File: `mod_destruction.go`
- Resources:
  - `*DestructionQueue`
- Systems:
  - `destructionSystem`
- Owns:
  - queued voxel destruction operations
  - batches same-entity edits so pellet weapons produce one runtime revision
  - carves streamed imported chunks without whole-chunk connectivity splitting
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

### `GroundedCharacterMotorModule` and `GroundedPlayerControllerModule`

- File: `mod_grounded_player.go`
- Resources:
  - optional `*GroundedPlayerControllerDefaults`
- Systems:
  - `GroundedCharacterMotorModule`: camera-free grounded motor and authored
    trigger/brush integration
  - `GroundedPlayerControllerModule`: compatibility keyboard/mouse and local
    camera adapter around the same motor
- Owns:
  - actor-neutral movement from `GroundedCharacterIntentComponent`; a motor
    entity does not require or acquire a `CameraComponent`
  - ladder movement requires volume overlap unless authored traversal sets
    `ForceLadder`; ordinary forward/back input never forces vertical movement
  - local first-person input behavior, including held `Ctrl` crouch
    (clearance-checked standing recovery) and water-volume swimming (`Space`
    rises, `Ctrl` descends)
  - walking contact through the shared kinematic character helpers: slide,
    step-up, landing snap, and vertical sweep
  - `ScriptedMovement` keeps controller camera/look ownership while a gameplay
    traversal action advances the capsule through those same collision helpers
- Depends on:
  - `*Time`
  - `*VoxelRtState`
  - `*Input` only for `GroundedPlayerControllerModule`

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
