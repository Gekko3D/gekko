# Deferred Decals Implementation Plan

Status: proposed  
Date: 2026-08-01  
Primary owner: `gekko` VoxelRT renderer  
First consumer: `actiongame`

## Goal

Add persistent blood stains and explosion soot to opaque voxel walls without
editing voxel data, creating one ECS entity per mark, or issuing one draw call
per mark.

The durable design is a generic deferred-decal feature in `gekko`. ActionGame
owns the gameplay rules and a bounded CPU mark pool; the renderer receives a
flat list and draws the visible result as one instanced decal-volume batch per
atlas.

## Decision Summary

Implement an optional `DecalFeature` with this frame flow:

1. ActionGame turns completed `ActionGameImpactEvent` values into retained
   decal records.
2. The existing VoxelRT bridge groups those records by texture atlas and uploads
   one compact instance buffer.
3. A render-graph node after the G-buffer and before deferred lighting clears a
   full-resolution `RGBA8Unorm` decal-color target and rasterizes oriented box
   volumes.
4. The decal fragment shader reconstructs the opaque surface position from
   G-buffer depth, rejects unrelated surfaces, samples the atlas, and writes
   premultiplied color and opacity.
5. Deferred lighting composites that color into the base material before BRDF
   evaluation, so blood and soot receive the wall's existing lighting, AO, and
   shadows.

This is a long-term architecture step, not a temporary sprite approximation.

## Confidence Gate

- Confidence: High
- Why:
  - impact events already contain position, normal, target, material facts, and
    `SurfaceMarkRadius`;
  - the G-buffer already stores hit distance and receiver normal;
  - deferred lighting already reconstructs exact world-space hit position;
  - the renderer already has optional feature lifecycle, explicit graph nodes,
    instanced batches, texture-atlas caching, and resize/bind-group rebuild
    patterns to reuse.
- Key assumptions:
  - the first receiver scope is static opaque world geometry;
  - color and opacity are enough for the first blood/soot result;
  - a hard cap of 4096 retained ActionGame marks is a reasonable starting
    calibration value, to be confirmed by a live benchmark.
- What would raise confidence further:
  - representative blood and soot atlas art;
  - live captures at 0, 512, 2048, and 4096 marks on the target GPU.
- SME alignment required?: No before implementation; request renderer review
  before merging the G-buffer/lighting binding change.

## Current Evidence

- `actiongame/src/modules/startup/impact.go`
  - `ActionGameImpactEvent` already carries `Position`, `Normal`, `Target`,
    `Surface`, `Explosive`, and `SurfaceMarkRadius`.
  - impact events are frame-lifetime and cleared at the start of the ActionGame
    actuation stage, so decals need a separate retained state.
  - explosion and ordinary impact particles already share one procedural atlas.
- `voxelrt/rt/shaders/gbuffer.wgsl`
  - `out_depth.r` stores exact hit distance;
  - `out_depth.gba` stores voxel-center position for voxel-stable lighting;
  - `out_normal.xyz` stores the receiver normal.
- `voxelrt/rt/shaders/deferred_lighting.wgsl`
  - exact hit position is reconstructed from screen UV and hit distance;
  - base color is resolved before lighting, which is the correct decal
    composition point.
- `voxelrt/rt/app/render_graph_default.go`
  - the graph has an explicit dependency chain from G-buffer through lighting;
  - a built-in optional decal node can be inserted without a game-owned render
    loop branch.
- `voxelrt/rt/gpu/manager_sprites.go`
  - the sprite atlas map, sampler, mip generation, asset versioning, and grouped
    per-atlas bind-group behavior can be reused.

## Alternatives Considered

### Paint voxel materials

Reject for this feature.

- dirties CPU-authoritative `XBrickMap` bricks and triggers voxel uploads;
- couples appearance to destruction and persistence data;
- limits stain resolution to voxel resolution;
- cannot cheaply represent soft soot or irregular blood edges.

### Spawn a quad, sprite, or ECS entity per stain

Reject as the long-term path.

- current fixed sprites are not aligned by an arbitrary receiver normal;
- cards suffer from clipping, depth bias, and corner bleed;
- per-mark entities and draw submission are avoidable;
- the transparent accumulation path shades them as cards, not as modified wall
  material.

### Tiled or clustered decal lists in deferred lighting

Defer until measurement requires it.

- scales to very large projector counts;
- adds culling buffers, another compute stage, per-tile limits, and shader list
  traversal;
- the instanced volume pass shades only projected decal bounds and is the
  smaller durable solution for a capped arena-game pool.

## Scope

### Included

- static opaque voxel receivers;
- blood and soot color/opacity decals;
- oriented box projection with atlas UVs;
- one renderer instance buffer and one draw per atlas;
- bounded ActionGame retention and oldest-mark eviction;
- deterministic blood and soot placement raycasts;
- removal when the owning entity disappears, the level resets, or destruction
  carves through a mark;
- renderer counters and a live performance comparison.

### Not included

- normal-map, roughness, metalness, or emissive decal channels;
- decals on transparent voxels, water, particles, sprites, or analytic bodies;
- stable receiver IDs in the G-buffer;
- attachment to moving actors, doors, platforms, or detached destruction
  fragments;
- save-game persistence or restoration after a streamed chunk unloads;
- decal-to-decal baking, virtual textures, tile lists, or compute culling;
- a new texture system or third-party dependency.

Add a compact receiver-ID target and target-local anchors only when moving or
parallel overlapping receivers become a real requirement. Add PBR targets only
when authored decal materials need them. Add tiled lists only after the volume
pass is measured as the bottleneck.

## Invariants

- CPU voxel storage remains authoritative and unchanged by a visual decal.
- The underlying palette/material index, normal, AO, and shadow metadata remain
  unchanged.
- Decals are an optional surface overlay; consumers that do not register the
  feature do not allocate the full-resolution decal target or record the pass.
- A missing feature, empty mark list, resize, or scene-buffer recreation cannot
  leave stale decals visible.
- Transparent geometry continues through the existing transparency path and
  does not receive decals in this phase.
- Atlas textures are uploaded and cached through the existing sprite-atlas
  resource path.
- No decal creates an ECS entity, voxel edit, physics body, or individual draw
  call.

## Public CPU Contract

Add a small renderer-facing value in the root `gekko` package. It is a retained
render instance, not an ECS component:

```go
type DecalInstance struct {
    Position     mgl32.Vec3
    Rotation     mgl32.Quat
    HalfExtents  [3]float32
    Color        [4]float32
    NormalCutoff float32

    Texture     AssetId
    SpriteIndex uint32
    AtlasCols   uint32
    AtlasRows   uint32
}
```

Contract details:

- local `+Z` is the projector direction and matches the intended receiver
  normal;
- `HalfExtents.X/Y` define mark width and height;
- `HalfExtents.Z` is a shallow projection depth, initially `0.05` world units;
- `Color` is a linear tint and opacity multiplier;
- `NormalCutoff` rejects surfaces whose normal differs too much from projector
  `+Z`; default to `0.5`;
- atlas fields follow the existing sprite-atlas indexing convention;
- invalid size, non-finite values, zero alpha, or missing atlas dimensions are
  rejected or normalized at the bridge boundary.

Expose replacement semantics on `VoxelRtState`:

```go
func (state *VoxelRtState) SetRuntimeDecals(decals []DecalInstance)
func (state *VoxelRtState) ClearRuntimeDecals()
```

ActionGame remains authoritative for retention and replaces the renderer list
after it processes impacts. Copy the slice at the API boundary so later game
mutation cannot race or alias renderer sync.

## GPU Contract

Use one 80-byte, 16-byte-aligned record:

```go
type DecalInstanceInput struct {
    Position     [4]float32 // xyz center
    Rotation     [4]float32 // quaternion xyzw
    HalfExtents  [4]float32 // xyz bounds, w normal cutoff
    Color        [4]float32 // linear tint, opacity
    Atlas        [4]uint32  // tile, columns, rows, reserved
}
```

The shader must:

1. generate a unit cube from `vertex_index`, with 36 vertices per instance;
2. scale and rotate the cube into the projector volume;
3. reconstruct the receiver hit position from `GBufferDepth.r`;
4. rotate `hitPosition - decalCenter` by the conjugate decal quaternion;
5. reject pixels outside the local half-extents;
6. reject pixels when `dot(receiverNormal, projectorNormal) < NormalCutoff`;
7. derive UV from local `X/Y`, sample the atlas tile, and use nearest/hard-edged
   sampling consistent with the renderer's block style;
8. output `rgb * alpha, alpha` into an `RGBA8Unorm` target with premultiplied
   source-over blending.

Use a shallow Z extent and the normal test to prevent projection around corners.
Do not add a depth bias because the pass samples the opaque G-buffer instead of
competing with it as raster geometry.

Deferred lighting reads the premultiplied overlay and applies:

```wgsl
base_color = decal.rgb + base_color * (1.0 - decal.a);
```

Apply this before ambient, direct, and specular lighting. `RenderModeLit` and
`RenderModeAlbedo` should show decal color; normal and diagnostic material modes
should keep their existing meaning.

## Renderer Ownership And Files

### Root bridge

- `decal_runtime.go` — new public `DecalInstance`, validation, atlas grouping,
  and `VoxelRtState` replacement API.
- `mod_voxelrt_client.go` — retain the current frame's runtime decal slice and
  export the renderer feature alias if needed by ActionGame.
- `mod_voxelrt_bridge_registry.go` — add `VoxelRtBridgeFeatureDecals` gated by
  feature name `decals` and graph node `feature-decals`.
- `mod_voxelrt_client_systems.go` — upload required atlases through the existing
  sprite-atlas cache and call `ApplyDecalInput` before renderer update.
- matching root-package tests — validation, replacement-copy behavior, grouping,
  feature gating, and clear behavior.

### Renderer app and graph

- `voxelrt/rt/app/feature_decals.go` — feature lifecycle, input conversion,
  pass readiness, recording, counters, and cleanup.
- `voxelrt/rt/app/app_decals.go` — pipeline layout and creation if keeping setup
  separate makes the feature file clearer.
- `voxelrt/rt/app/app.go` — add `DecalResources`; do not enable the feature in
  default feature flags.
- `voxelrt/rt/app/render_graph_default.go` — add `feature-decals` after tiled
  light culling and make core lighting depend on it.
- graph and feature tests — verify ordering, disabled behavior, lifecycle, and
  empty-input clearing.

The default graph node exists for stable dependency ordering, but remains
disabled unless `DecalFeature` is registered. ActionGame explicitly registers
the feature through `VoxelRtModule.RenderFeatures`; other consumers keep the
transparent 1x1 fallback and pay no full-resolution target or pass cost.

### GPU manager

- `voxelrt/rt/gpu/manager.go` — decal buffer, count, batches, target/view,
  fallback view, and bind-group state.
- `voxelrt/rt/gpu/manager_decals.go` — buffer upload, atlas-batch bind groups,
  target creation, fallback creation, and release/rebuild helpers.
- `voxelrt/rt/gpu/manager_render_setup.go` — include the active decal view when
  building the lighting bind group.
- `voxelrt/rt/gpu/manager_alloc.go` — release new owned GPU objects with the
  existing manager shutdown path.
- GPU tests — record size, buffer growth, atlas batch reuse, transparent
  fallback, resize rebuild, and stale bind-group prevention.

Reuse `SpriteAtlases`, `SpriteAtlasSampler`, `SetSpriteAtlas`, mip generation,
and atlas version tracking. Do not introduce a second atlas cache.

### Shaders

- `voxelrt/rt/shaders/decals.wgsl` — instanced volume projection.
- `voxelrt/rt/shaders/shaders.go` — embed the new shader.
- `voxelrt/rt/shaders/deferred_lighting.wgsl` — sample and composite the decal
  target before lighting.
- `voxelrt/rt/shaders/shaders_test.go` — assert the new binding and projection
  contract where a source-level regression test is useful.

## Resource Lifecycle

- When the feature is absent:
  - lighting binds a transparent 1x1 fallback;
  - no full-resolution decal texture exists;
  - no decal node records work.
- On feature setup and resize:
  - create/recreate one framebuffer-sized `RGBA8Unorm` texture with
    `RenderAttachment | TextureBinding` usage;
  - recreate decal and lighting bind groups that reference its view.
- When the current frame has decals:
  - begin one decal render pass with a transparent clear;
  - draw each non-empty atlas batch.
- When the list transitions from non-empty to empty:
  - record one clear-only pass so the previous frame cannot leak;
  - skip the pass on later empty frames until decals return.
- On scene buffer recreation:
  - rebuild bindings that reference camera, G-buffer, or instance buffers.
- On feature shutdown:
  - release the target, view, instance buffer, and decal-owned bind groups;
  - restore the lighting fallback binding.

Follow existing retired-buffer handling when instance-buffer growth replaces a
buffer that may still be used by an in-flight frame.

## ActionGame Ownership And Files

- `actiongame/src/modules/startup/decals.go` — retained pool, pending explosion
  projections, impact ingestion, ray placement, carve invalidation, and renderer
  list update.
- `actiongame/src/modules/startup/decals_test.go` — capacity, deterministic ray
  directions, classification, projection filtering, and removal.
- `actiongame/src/modules/startup/startup.go` — install the state and one decal
  system after all impact-producing actuation systems.
- `actiongame/src/modules/startup/impact.go` — extend the existing impact atlas
  with blood and soot tiles while preserving current particle tile indices.
- `actiongame/main.go` — register the generic decal renderer feature.

Do not add an ActionGame renderer implementation. Game code decides what marks
exist; `gekko` decides how marks render.

## ActionGame Retention Policy

Add `actionGameDecalState` with:

- a maximum of 4096 retained marks;
- monotonically increasing insertion order;
- a compact slice or ring index for oldest-mark replacement;
- pending explosion projections that wait until the next frame's destruction
  update has edited geometry.

When full, replace the oldest mark. A linear scan for invalidation and renderer
list construction is acceptable under this hard cap:

```go
// ponytail: linear scans are bounded by 4096 marks; add spatial bins only if profiling shows this system is hot.
```

Remove a retained mark when:

- its target entity no longer exists;
- its center intersects a carve on the same target, including the mark's
  largest half-extent;
- the arena enters loading, menu, match reset, or shutdown;
- it is selected for oldest-first capacity eviction.

Marks intentionally disappear with an unloaded streamed entity in this phase.
Do not serialize them into world deltas.

## Placement Rules

### Blood

- Reuse `actionGameResolveImpactTargets` and `actionGameImpactResponseFor` to
  identify flesh impacts; do not add a second material classifier.
- Starting just beyond the flesh hit, cast a deterministic three-ray cone in
  the incoming projectile direction.
- Ignore the struck actor/NPC, projectile visuals, and other non-geometry
  receivers with the existing weapon raycast-filter helpers.
- Create a blood decal only for an opaque geometry hit.
- Align local `+Z` to the geometry hit normal.
- Use `SurfaceMarkRadius` as the primary size and deterministic seed variation
  for aspect ratio, rotation around the normal, atlas tile, and tint.

### Explosion soot

- Queue explosion projection until the next Update has processed the current
  destruction queue.
- Cast a fixed six-direction ray set from the blast plus the original contact
  direction when present.
- Create soot only on opaque geometry within the explosion radius.
- Size marks from explosion radius with a conservative clamp so one large blast
  cannot fill the screen with a single overdraw-heavy projector.
- Use deterministic atlas/tint/rotation variation; do not use global random
  state.

The starting constants—4096 retained marks, three blood rays, six soot rays,
and `0.05` projection depth—are calibration knobs. Change them from measured
visual/performance evidence, not by adding a general settings framework.

## Atlas Plan

Keep the current four particle tiles in their existing indices. Expand the
procedural atlas by rows and add at least:

- two blood splatter silhouettes;
- two soot/scorch silhouettes.

Use the existing `AssetServer.CreateTextureFromTexels` path and atlas upload
cache. This proves the complete runtime without adding asset-loading work.
Authored PNG art can replace the generated texels later without changing the
decal API, GPU layout, or gameplay policy.

## Implementation Tasks

### Task 1: Freeze the bridge and GPU record contracts

Goal:

- add the root `DecalInstance` value, replacement API, renderer input record,
  atlas batch descriptor, validation, and tests.

Acceptance:

- invalid records do not reach the GPU;
- replacement copies caller data;
- clear produces zero renderer instances;
- atlas groups are stable and reuse existing atlas keys;
- `unsafe.Sizeof(DecalInstanceInput{}) == 80`.

Verification:

```sh
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test . ./voxelrt/rt/app
```

### Task 2: Add decal GPU resources and the instanced pass

Goal:

- implement the feature, target, fallback, instance buffer, grouped bind groups,
  pipeline, volume shader, and lifecycle rebuilds.

Acceptance:

- one draw per non-empty atlas batch;
- empty enabled frames clear stale output;
- disabled consumers allocate no full-resolution decal target;
- resize and instance-buffer growth leave all bindings current;
- nil/not-ready resources skip safely and expose profiler readiness counters.

Verification:

```sh
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu ./voxelrt/rt/app ./voxelrt/rt/shaders
```

### Task 3: Integrate graph ordering and deferred lighting

Goal:

- schedule `feature-decals` immediately before core lighting and composite the
  decal overlay into base color.

Acceptance:

- compiled graph order is G-buffer before decals before lighting;
- feature absence uses transparent fallback and preserves prior output;
- blood/soot receive ambient, direct, AO, and shadow response from the wall;
- normal/material debug modes remain unchanged;
- atlas and output color spaces are handled explicitly: sRGB atlas samples to
  linear, linear `RGBA8Unorm` overlay, linear deferred-lighting composition.

Verification:

```sh
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/app ./voxelrt/rt/gpu ./voxelrt/rt/shaders
```

### Task 4: Add the ActionGame retained pool and feature registration

Goal:

- retain bounded marks outside frame-lifetime impact state and feed the generic
  bridge after all current-frame impacts are resolved.

Acceptance:

- impacts are copied before the next actuation-stage clear;
- the 4097th mark evicts the oldest;
- missing targets, carves, and arena reset remove marks;
- no decal entities or voxel edits are created;
- current particle atlas indices and impact effects remain unchanged.

Verification:

```sh
cd /Users/ddevidch/code/go/gekko3d/actiongame
env GOCACHE=/tmp/gekko3d-gocache go test ./src/modules/startup
```

### Task 5: Add deterministic blood and soot placement

Goal:

- translate flesh and explosion impacts into correctly aligned wall projectors.

Acceptance:

- identical impacts generate identical ray directions and atlas choices;
- blood rays cannot immediately hit the source actor again;
- soot raycasts run after destructive geometry changes;
- only opaque geometry receives marks;
- marks do not visibly wrap across perpendicular wall corners;
- destruction removes unsupported marks instead of projecting them through a
  new hole.

Verification:

```sh
cd /Users/ddevidch/code/go/gekko3d/actiongame
env GOCACHE=/tmp/gekko3d-gocache go test ./src/modules/startup
```

### Task 6: Cross-module verification and documentation

Goal:

- validate integration, measure the fixed-cap design, and document live
  renderer behavior.

Required automated checks:

```sh
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu ./voxelrt/rt/app ./voxelrt/rt/shaders
env GOCACHE=/tmp/gekko3d-gocache go test ./...

cd /Users/ddevidch/code/go/gekko3d/actiongame
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Required live check:

```sh
cd /Users/ddevidch/code/go/gekko3d/actiongame
env GOCACHE=/tmp/gekko3d-gocache go run .
```

Update `docs/renderer/runtime.md`, `docs/renderer/overview.md`, and the renderer
resource inventory after behavior is live. Write the required cross-module
status report under `gekko/reports/` with exact commands and results.

## Performance Measurement

Use the same ActionGame level, camera position/path, resolution, and lighting
for each sample. Capture:

- 0 decals;
- 512 small blood decals;
- 2048 mixed decals;
- the 4096 cap with overlapping soot in view.

Add profiler counters:

- `DecalCount`;
- `DecalAtlasBatches`;
- `DecalDrawCalls`;
- `DecalTargetReady`;
- `DecalBindingsReady`;
- `DecalPassRecorded`.

Compare frame time/FPS and visible stutter during mark insertion and camera
movement. Existing CPU profiler scopes cannot prove GPU shader savings, so do
not claim GPU-time improvements without an external GPU capture. The pass is
acceptable when:

- zero decals on a non-participating consumer records no pass and owns no
  full-resolution target;
- ActionGame with the feature enabled has no stale-clear or resize artifacts;
- insertion does not cause recurring buffer or bind-group recreation below the
  current capacity;
- the 4096-mark scene remains within the agreed frame budget on the target GPU;
- overdraw, not CPU list handling, is measured before tiled culling is proposed.

## Visual Acceptance Checklist

- Blood remains fixed to a wall while the camera translates and rotates.
- Soot follows wall lighting and does not look like a camera-facing card.
- A mark near a 90-degree corner does not wrap onto the adjacent face.
- Marks do not appear on sky, glass, water, actors, or particles.
- Two overlapping marks composite without a black or white halo.
- Distant marks use the shared mip chain without dark fringes.
- Resizing the window does not stretch, lose, or resurrect marks.
- Carving a marked wall removes marks whose supporting surface was destroyed.
- Existing impact smoke, sparks, debris, transparency, shadows, and render
  modes remain correct.

## Rollback And Compatibility

- The feature is opt-in and adds no serialized data.
- Removing feature registration from ActionGame restores the transparent
  fallback path and records no decal pass.
- Removing ActionGame mark generation leaves the generic renderer feature
  harmlessly empty.
- No migration or backfill is required.
- If the lighting binding causes a backend regression, keep the public/game
  pool work but disable feature registration until the binding issue is fixed;
  do not fall back to per-stain sprites as permanent behavior.

## Completion Definition

The work is complete when:

- the generic renderer feature and ActionGame policy are separated as described;
- blood and explosion soot visibly persist on static opaque walls;
- the renderer uses no voxel edits, decal entities, or per-mark draw calls;
- capacity, lifecycle, graph-order, shader, bridge, and gameplay tests pass;
- live resize, destruction, corner, and performance checks pass;
- canonical renderer docs and the cross-module status report record the final
  behavior and measured limits.
