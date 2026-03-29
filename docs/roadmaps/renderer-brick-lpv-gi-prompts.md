# Brick-Level LPV GI — AI Implementation Prompts

This document turns
`docs/roadmaps/renderer-brick-lpv-gi.md`
into agent-ready implementation prompts.

Use these prompts only after reading the roadmap sections on:

- architecture and hard rules
- authored/runtime GI ownership
- imported-world default policy
- terrain-group sync semantics
- host-side LPV extraction
- verification and implementation phases

These prompts are intentionally strict. They are meant to prevent an agent from
quietly drifting back to the older visible-object GI assumptions or from
shipping a partial renderer-only implementation that is unusable end to end.

---

## How To Use These Prompts

- Run the prompts in order.
- Do not skip Phase 0. The authored/runtime ownership work is required before
  the renderer work is complete.
- Stop after each prompt and validate before moving on.
- Keep the roadmap authoritative if the codebase still contains older GI ideas.
- When a prompt touches the editor, imported-world content, and runtime spawn
  chain together, prefer one agent or clearly disjoint file ownership.
- Do not run two write agents against the same files.

Suggested parallelization:

- Prompt 0A and Prompt 0B may be split across separate agents if file ownership
  stays disjoint.
- Prompts 1 through 6 should be treated as sequential because each phase depends
  on verified behavior from the prior phase.

---

## Global Rules For Every Agent

1. Do not index LPV through `scene.VisibleObjects`.
2. Do not use `InstancesBuf` or `ObjectParamsBuf` to discover LPV participants.
3. Build LPV participation from `Scene.Objects` and explicit authored/runtime
   GI ownership.
4. Keep the LPV hash single-entry:
   `world_sector_coord -> sector_slot`.
5. Overlapping LPV contributors are illegal in Phase 0 through Phase E.
   When multiple contributors map to the same world-sector coordinate, reject
   all colliding entries deterministically, mark them overlapped, omit them
   from `LPVSpatialHashBuf`, and increment overlap counters.
6. Imported worlds must be GI-enabled by default:
   new authored/baked data defaults to `ParticipatesInGI=true`, and legacy
   imported-world manifests must migrate to `true` during defaulting.
7. `LPVWorldTerrainGroupID == 0` is not a normal runtime fallback.
   Terrain is included only when:
   - `LPVWorldTerrainGroupID != 0` and terrain group matches, or
   - `LPVAllTerrainGroupsDebug == true` and `LPVWorldTerrainGroupID == 0`
8. If terrain-group sync is invalid
   (`LPVWorldTerrainGroupID == 0 && !LPVAllTerrainGroupsDebug`), do not include
   all terrain. Exclude terrain LPV participation, log/debug-warn, and
   increment the missing-group counter.
9. Do not accept rotated or non-uniformly scaled LPV contributors in Phase A-E.
10. Stop after the requested scope, report changed files, report tests run, and
    call out unresolved assumptions instead of silently extending the phase.

---

## Shared Validation Scenes And Cases

Before or during the sequence below, prepare validation content that covers:

1. An enclosed room with solid walls.
2. One window or roof opening for sun bounce.
3. A bright point light inside the room.
4. A one-voxel-thick wall and a two-voxel-thick wall case for leak testing.
5. A layered wall where white plaster is the outward visible layer and red brick
   exists immediately behind it, so hidden-layer color contamination is easy to
   spot.
6. A static GI prop overlapping terrain or another static voxel contributor on
   at least one world-sector coordinate, to validate deterministic rejection.
7. A legacy imported-world manifest that lacks `ParticipatesInGI`, to validate
   migration/default behavior.
8. A streamed terrain scene where `LPVWorldTerrainGroupID` is intentionally left
   unset once, to validate that terrain GI is rejected and surfaced instead of
   silently merging all loaded terrain.

---

## Verification Guidance

Ask the agent to run the smallest relevant verification step for the files it
changed.

Useful checks in this workspace:

- engine content/schema changes:
  `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- engine-wide renderer/runtime compile sweep:
  `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- editor when authored/editor surfaces changed:
  `cd /Users/ddevidch/code/go/gekko3d/gekko-editor && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- voxel demo compile check when voxel runtime changed:
  `cd /Users/ddevidch/code/go/gekko3d/examples/testing-vox && env GOCACHE=/tmp/gekko3d-gocache go test ./...`

If a prompt says "stop here", do not continue to the next phase even if the
implementation appears straightforward.

---

## Prompt 0A: Authored Schema, Defaults, Spawn Chain, Validation, And Editor Ownership

**Goal:** Make GI participation a complete end-to-end authored/runtime feature
before any LPV buffer work begins.

```text
Please implement the authored/runtime ownership part of Phase 0 from
`docs/roadmaps/renderer-brick-lpv-gi.md`.

Scope:
- content schema/defaulting
- authored spawn/runtime copy chain
- validation
- imported-world bake/import defaults
- editor/UI/tooling surfaces

Do not implement GPU buffers, shaders, or render-loop work yet.

Requirements:
1. Add authored GI ownership fields:
   - `content.AssetPartDef.ParticipatesInGI bool`
   - `gekko.VoxelObjectDef.ParticipatesInGI bool`
   - `content.ImportedWorldDef.ParticipatesInGI bool`
   - `AuthoredImportedWorldSpawnDef.ParticipatesInGI bool`
   - `VoxelModelComponent.ParticipatesInGI bool`
2. Wire the authored/runtime spawn chain so authored GI intent reaches
   `VoxelModelComponent`:
   - `scene.go`
   - `asset_content_spawn.go`
   - `imported_world_spawn.go`
   - `level_content_spawn.go`
3. Imported-world policy must match the roadmap exactly:
   - new imported worlds default to `ParticipatesInGI=true`
   - baked/imported worlds written by `imported_world_baker.go` explicitly set
     `ParticipatesInGI=true`
   - legacy manifests migrate to `ParticipatesInGI=true` during defaulting/load
4. Update content validation and imported-world validation so the new fields are
   recognized and the authored side can surface invalid LPV usage.
5. Update editor/import/tooling surfaces so designers can actually create and
   edit the new GI fields:
   - `gekko-editor/src/modules/asset_editor/asset_vox_import.go`
   - `gekko-editor/src/modules/asset_editor/asset_inspector.go`
   - `gekko-editor/src/modules/asset_editor/asset_workflow.go`
   - `gekko-editor/src/modules/level_editor/base_world_import.go`
   - `gekko-editor/src/modules/level_editor/base_world_ui.go`
6. Keep terrain behavior unchanged for authoring in Phase A-E:
   terrain chunks are GI-active when their runtime terrain group is active;
   there is no new author-facing terrain toggle.
7. Do not add a per-placement GI override in Phase 0. Level placements inherit
   from the asset/imported-world content they instantiate.

Implementation notes:
- The roadmap is explicit that the plan is not complete unless authored tooling
  surfaces are updated together with runtime/schema changes.
- If you need to bump imported-world schema versioning or default helpers,
  do so now.

Verification:
- Run the smallest relevant engine content/schema tests.
- Run editor tests if editor files change.
- Summarize exact files changed and confirm the default behavior for newly baked
  and legacy imported worlds.

Stop here after Phase 0 authored/runtime ownership is complete.
```

---

## Prompt 0B: Renderer Sync, Terrain-Group Semantics, And Phase-0 Runtime Plumbing

**Goal:** Finish the non-shader runtime rules that the renderer depends on
before LPV buffer construction begins.

```text
Please implement the runtime/renderer-sync part of Phase 0 from
`docs/roadmaps/renderer-brick-lpv-gi.md`.

Scope:
- renderer-facing scene/runtime state
- GI/static flags synced into voxel RT objects
- terrain-group sync semantics
- debug/observability hooks for invalid terrain-group sync

Do not implement LPV inject/propagate shaders yet.

Requirements:
1. Synchronize `obj.ParticipatesInGI` and `obj.IsStatic` from authored/runtime
   ECS state into the voxel RT scene path.
2. Add the renderer scene fields required by the roadmap:
   - `LPVWorldTerrainGroupID uint32`
   - `LPVAllTerrainGroupsDebug bool`
3. Source the active terrain group from streamed runtime state instead of
   inventing a new GI ECS component.
4. Normal path must drive an explicit non-zero
   `Scene.LPVWorldTerrainGroupID`.
5. `LPVWorldTerrainGroupID == 0` is not "include all terrain" by default.
   Implement the exact policy:
   - if `LPVWorldTerrainGroupID == 0 && LPVAllTerrainGroupsDebug == true`,
     allow all loaded terrain chunks
   - if `LPVWorldTerrainGroupID == 0 && LPVAllTerrainGroupsDebug == false`,
     exclude terrain LPV participation for that frame and surface the problem
     through warning/debug counters such as
     `LPVMissingTerrainGroupFrames`
6. Keep the sync helper small and explicit. The roadmap suggests a helper like
   `syncVoxelRtGIState(...)`; use the existing structure if there is a better
   fit, but preserve the same ownership.
7. Do not add a new `LPVConfigComponent`.

Verification:
- Run the smallest relevant engine tests/compile checks.
- Confirm the normal streamed-terrain path now sets a non-zero terrain group.
- Confirm the invalid/unset case no longer silently merges all loaded terrain.

Stop here after the runtime sync and terrain-group semantics match the roadmap.
```

---

## Prompt 1: Phase A Host-Side LPV Extraction, Sector Metadata, Spatial Hash, And Overlap Rejection

**Goal:** Build the host-side LPV world field correctly before injection or
propagation math is allowed to grow.

```text
Please implement the host-side LPV extraction and metadata/hash work for
Phase A from `docs/roadmaps/renderer-brick-lpv-gi.md`.

Do not implement propagation yet. Injection may remain stubbed if needed while
we validate the host-side world field.

Requirements:
1. Create the LPV manager scaffolding and buffers described by the roadmap,
   including:
   - `LPVSectorMetaBuf`
   - `LPVSpatialHashBuf`
   - any related counters/CPU staging structures needed for upload
2. Build LPV participants from `scene.Objects`, not from
   `VisibleObjects`, `InstancesBuf`, or `ObjectParamsBuf`.
3. Apply the roadmap extraction rules exactly:
   - terrain uses active terrain-group matching or the explicit debug-all mode
   - non-terrain props require `obj.IsStatic && obj.ParticipatesInGI`
   - imported-world chunks participate via `obj.ParticipatesInGI`, not terrain
     group membership
4. Add or use transform-validation helpers that reject rotated or non-uniformly
   scaled contributors in Phase A-E.
5. Implement `LPVSectorMetaBuf` from sector allocation data after voxel/sector
   allocations are current.
6. Build `LPVSpatialHashBuf` from LPV sector metadata using the roadmap hash and
   linear probing rules.
7. Enforce the single-entry overlap policy:
   - world-sector collisions are illegal
   - colliding sectors must be rejected deterministically
   - overlapped sectors must not be inserted into the hash
   - overlapped sectors must surface through counters/debug info
   - do not use first-writer-wins behavior
8. If there is no existing low-friction visualization, add a minimal LPV debug
   visualization mode or debug output that makes sector placement and overlap
   rejection easy to inspect.
9. Add or update tests for:
   - transform acceptance/rejection
   - overlap rejection
   - terrain-group invalid/unset behavior where relevant

Important constraints:
- Size and rebuild the metadata/hash buffers the way the roadmap specifies.
- Keep the LPV hash keyed by world-sector coordinates.
- Build the hash from metadata, not from object indices.
- Off-screen streamed terrain chunks still need to be representable because LPV
  is world-space, not camera-visible-object-space.

Verification:
- Run the smallest relevant engine tests/compile checks.
- Validate that the debug output aligns with terrain and GI-enabled props.
- Validate that an intentional overlap case is rejected deterministically.
- Validate that an unset terrain group does not quietly include all terrain.

Stop here. Do not move on to the real inject shader until the host-side LPV
world field is correct.
```

---

## Prompt 2: Phase A Injection For Point/Spot Lights And Nearest-Brick Deferred Sampling

**Goal:** Get stable raw indirect energy into the LPV without propagation.

```text
The host-side LPV field is now correct. Please implement the remaining Phase A
work from `docs/roadmaps/renderer-brick-lpv-gi.md`.

Requirements:
1. Allocate and bind:
   - `BrickIrradianceABuf`
   - `BrickIrradianceBBuf`
   - `LPVConfigBuf`
2. Implement `brick_lpv_inject.wgsl` for Phase A:
   - point lights
   - spot lights
   - no directional injection yet
   - no emissive injection yet
3. Follow the roadmap bind-group layout and data sources.
4. Update the frame/render setup so the inject pass runs before deferred
   lighting.
5. In `deferred_lighting.wgsl`, add LPV sampling that:
   - uses world-space lookup through `LPVSpatialHashBuf` and `LPVSectorMetaBuf`
   - samples the nearest brick only in Phase A
   - adds LPV as a local bounce term on top of existing sky ambient
6. Implement point/spot occlusion conservatively against the LPV world field,
   using LPV participant traversal rather than camera-visible object buffers.
   Use a world-space DDA or stepped segment test as described by the roadmap.
   If the segment from the light to the brick center crosses solid LPV world
   occupancy, injection for that light must be rejected rather than partially
   leaking through.
7. Keep propagation disabled in this prompt.

Important constraints:
- Do not replace sky ambient with LPV.
- Do not use `InstancesBuf` or `ObjectParamsBuf` to resolve LPV space.
- If a sector lookup fails because the coordinate is absent or overlapped,
  deferred LPV sampling must return zero contribution.

Verification:
- Run the smallest relevant engine compile/tests.
- In the room scene, confirm raw injected light shows up in the expected bricks.
- Confirm one-voxel and two-voxel walls block direct LPV injection from
  point/spot lights.
- Confirm off-screen streamed terrain can still contribute.
- Confirm overlapped sectors remain absent from LPV sampling.

Stop here after point/spot injection and nearest-brick sampling are stable.
```

---

## Prompt 3: Phase B Propagation With Conservative Face Blocking

**Goal:** Spread LPV energy without leaking through solid voxel boundaries.

```text
Phase A injection is working. Please implement Phase B propagation from
`docs/roadmaps/renderer-brick-lpv-gi.md`.

Requirements:
1. Implement `brick_lpv_propagate.wgsl`.
2. Dispatch 2-4 ping-pong propagation passes using the LPV irradiance buffers.
3. Resolve neighbors strictly through `LPVSpatialHashBuf` plus
   `LPVSectorMetaBuf`.
4. Reject neighbors that:
   - are absent from the hash
   - do not have `LPV_WORLD`
   - are marked overlapped / invalid for sampling
5. Implement conservative face blocking and attenuation so solid voxel
   boundaries stop propagation instead of allowing leaks.
   Treat boundary blocking as a hard constraint:
   - if the shared face / boundary plane between two bricks is blocked by solid
     LPV world occupancy, propagation in that direction must be `0`
   - do not rely on soft attenuation to "mostly" fix a blocked face
   - do not accept a solution that still leaks through one-voxel-thick walls
6. Keep the `gi_radius` budget behavior in place.
7. Update deferred lighting to read from the final propagation output buffer.

Important constraints:
- Do not traverse via camera-visible buffers.
- Use LPV world-space sector metadata for cross-object propagation.
- Preserve deterministic behavior around rejected overlaps.

Verification:
- Run the smallest relevant engine compile/tests.
- In the enclosed-room scene, confirm indirect light spreads within the room.
- Confirm one-voxel and two-voxel walls stop propagation instead of letting
  outdoor energy bleed through.
- Confirm terrain-to-prop propagation works across accepted world-space
  adjacency.

Stop here after propagation is stable and leak-resistant.
```

---

## Prompt 4: Phase C Directional Injection And Exposed-Face Weighting

**Goal:** Add sun bounce without averaging hidden interior voxels into the LPV.

```text
Phase B propagation is stable. Please implement Phase C from
`docs/roadmaps/renderer-brick-lpv-gi.md`.

Requirements:
1. Extend `brick_lpv_inject.wgsl` to support directional light injection.
2. Add the required shadow visibility sampling for directional injection.
3. Apply top-face / face-openness style weighting so sunlight bounce is biased
   toward exposed surfaces rather than buried interior volume.
4. When evaluating brick color contribution for directional bounce, do not
   blindly average all voxels in the brick.
5. Hidden/interior voxels must not contribute to directional injection.
   At minimum:
   - fully enclosed voxels contribute `0`
   - voxels hidden behind another solid layer along the incoming light
     direction contribute `0`
   - outward-visible surface voxels on the incident-light side are the ones
     that should drive the injected color/energy
6. Do not let a buried red brick layer tint LPV sunlight bounce when a white
   plaster layer is the first visible exterior surface.

Important constraints:
- Keep the implementation aligned with the roadmap's "face-openness /
  exposed-surface" rule.
- Do not overreach into final emissive polish yet unless it naturally shares the
  same helper logic.

Verification:
- Run the smallest relevant engine compile/tests.
- In the window/open-roof scene, confirm sunlight creates readable bounce.
- Confirm the white-plaster / red-brick layered wall bounces light using the
  visible white exterior instead of the hidden red layer.

Stop here after directional bounce is working and the exposed-surface rule is
in place.
```

---

## Prompt 5: Phase D Destruction Response And Observability

**Goal:** Make LPV responsive under destruction without hiding performance or
sync failures.

```text
Phase C is working. Please implement Phase D from
`docs/roadmaps/renderer-brick-lpv-gi.md`.

Requirements:
1. Update dirty sector upload scheduling so LPV-relevant sectors are prioritized
   first, especially within `gi_radius`.
2. Preserve the upload budget. Destruction-heavy scenes must remain bounded by
   the configured upload limits instead of allowing LPV work to explode.
3. Add the roadmap observability/debugging counters and toggles, including:
   - overlap-related counters
   - hash probe/collision counters
   - missing terrain-group counter / invalid terrain sync visibility
   - LPV enable/disable and tuning toggles that the roadmap expects
4. Ensure the destruction/update path continues to work with the overlap
   rejection policy and strict terrain-group semantics already implemented.

Verification:
- Run the smallest relevant engine compile/tests.
- Simulate or inspect a high-destruction case and confirm upload work is still
  budgeted.
- Confirm the new counters make overlap rejection, hash behavior, and missing
  terrain-group frames visible.

Stop here after destruction response and observability are usable.
```

---

## Prompt 6: Phase E Conservative Quality Polish, Emissive Injection, And Final Sampling

**Goal:** Finish LPV quality work without destroying the voxel look or
reintroducing leaks.

```text
Please implement Phase E from `docs/roadmaps/renderer-brick-lpv-gi.md`.

Requirements:
1. Upgrade deferred LPV sampling from nearest-brick to conservative trilinear
   style sampling.
2. Do not blindly blend across walls. If a neighboring brick sample is not
   meaningfully visible from the surface point because solid voxel occupancy
   blocks it, reject or heavily suppress that weight.
3. Add emissive injection using `MaterialBuf` and voxel payload data.
4. Emissive injection should follow the same exposed-surface rule used for
   directional bounce:
   hidden interior voxels should not contribute LPV energy just because they
   share a brick with an exposed surface.
   Keep the same rule of thumb:
   - outward-visible / exposed voxels may contribute
   - fully buried voxels contribute `0`
   - layers hidden behind another visible outer shell must not drive the brick
     color seen by GI
5. Add the runtime tuning controls needed for final LPV parameter tuning.
6. Keep the visual result appropriate for a destructible voxel FPS:
   improve quality, but do not smooth the scene into a generic soft GI look
   that erases readable voxel structure.

Verification:
- Run the smallest relevant engine compile/tests.
- Confirm emissive voxels contribute indirect light without obvious blowout.
- Confirm conservative interpolation reduces block popping without leaking
  outdoor energy through one-voxel and two-voxel walls.
- Confirm the layered white-plaster / red-brick wall still reflects the visible
  outer layer rather than hidden interior color.
- Summarize final remaining risks, if any.

Stop here and report the final Phase E code changes.
```
