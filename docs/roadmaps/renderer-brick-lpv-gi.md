# Brick-Level Light Propagation Volume (Brick LPV) - Implementation Spec

GPU-only local GI for the voxel renderer, built on the existing XBrickMap sector
and brick storage. This document replaces the older draft that mixed good LPV
direction with incorrect assumptions about current scene buffers and bind-group
ownership.

The target game is a fast FPS with highly destructible voxel environments in the
style of Teardown. The design therefore prioritizes:

- fast local response after voxel uploads land on the GPU
- low CPU cost per frame
- bounded, blocky indirect light that preserves voxel readability
- explicit exclusion of transforms and content classes the renderer does not yet
  represent safely in a unified world-voxel GI field

## 0. Current Baseline and Migration Rule

The repository currently contains stale documentation about probe GI, while the
live renderer code and render modes no longer expose a probe-GI user path in the
same way. This roadmap treats Brick LPV as the only production GI path to ship.

Before or alongside implementation:

- remove or update stale probe-GI references in `docs/renderer/*.md`
- update `docs/roadmaps/renderer-lighting.md` to make LPV the canonical local GI
  plan
- keep the existing `RenderModeIndirect` debug mode, but redefine it to show
  `sky ambient + LPV` during rollout and optionally add a temporary LPV-only
  debug flag

Do not build or maintain two competing production GI architectures in parallel.

## 1. Design Goals

| Requirement | Target |
| --- | --- |
| Convergence | 1 inject pass + 2-4 propagation passes in the same frame |
| Destruction response | limited only by voxel upload budget, not GI dirty-region baking |
| CPU involvement | per-frame sector metadata rebuild + hash rebuild only |
| Resolution | per brick (`8 x 8 x 8` voxels) |
| Memory | 16 bytes per brick slot per irradiance buffer + 32 bytes per sector slot for LPV metadata |
| Style | tightly bounded, blocky local bounce; no wide blur or filmic wash |
| Integration | additive local indirect term in `deferred_lighting.wgsl` |

## 2. Explicit Non-Goals for Phase A-E

Not supported in the first shipping LPV path:

- rotated GI contributors
- non-uniformly scaled GI contributors
- dynamic/moving GI contributors
- dynamic/moving GI occluders in LPV traversal
- skeletal meshes, sprites, particles, or CA volumes injecting into LPV
- soft temporal denoising or multi-frame GI crawl

Dynamic objects still receive GI in deferred lighting. They simply do not inject
or occlude the LPV field in the initial implementation.

## 3. Canonical Architecture

### 3.1 World-Space LPV, Not Visible-Object GI

Brick LPV operates in world voxel space.

The critical ownership rule is:

- `Scene.Objects` is the authoritative CPU source for LPV participants
- `VisibleObjects`, `InstancesBuf`, and `ObjectParamsBuf` remain camera-facing
  render data and must not be reused as LPV authority

This is non-negotiable because LPV must include off-screen terrain and static GI
contributors, while `InstancesBuf` and `ObjectParamsBuf` are currently built
from `scene.VisibleObjects`.

### 3.2 Dedicated LPV Sector Metadata

LPV does not index back into visible-object buffers.

Instead, the host builds a new per-sector buffer keyed by `sector_slot`:

`LPVSectorMetaBuf[sector_slot]`

Each record stores:

- the sector's world-space voxel origin
- LPV participation flags
- the material-table base needed for future emissive injection
- a dense LPV participant index for debugging only

This removes the invalid dependency on visible-object indices and allows LPV to
stay correct for off-screen streamed chunks.

### 3.3 Dedicated LPV Spatial Hash

LPV uses its own hash:

`world_sector_coord -> sector_slot`

Do not reuse `TerrainChunkLookupBuf`.

`TerrainChunkLookupBuf` is intentionally keyed to the G-buffer's visible-object
layout. Rebuilding it for off-screen GI would corrupt regular rendering.

### 3.4 Overlap Policy for the Unified World Field

The LPV hash remains single-entry:

`world_sector_coord -> sector_slot`

That means Phase A-E does **not** support multiple LPV contributors occupying
the same world-sector coordinate.

This is now an explicit rule:

- overlapping LPV contributors are illegal at world-sector granularity
- the host must detect overlap while building LPV sector metadata
- colliding sectors must not be inserted into `LPVSpatialHashBuf`
- all colliding sectors must be marked rejected for LPV injection and LPV
  traversal in that frame

Do not keep "first writer wins" behavior. That would make GI depend on scene
iteration order.

Required collision handling:

1. build a temporary CPU map: `world_sector_coord -> []sector_slot`
2. if a coord has exactly one claimant, keep it
3. if a coord has multiple claimants:
   - omit that coord from `LPVSpatialHashBuf`
   - clear `LPV_WORLD` for every colliding sector at that coord
   - set a new `LPV_OVERLAPPED` flag in `LPVSectorMeta`
   - increment overlap counters

This produces a deterministic "GI hole" rather than undefined winner selection.
That is acceptable because overlapping contributors are considered invalid
content for the single-entry LPV architecture.

Required reporting:

- runtime counters:
  - `LPVOverlappedSectorCoords`
  - `LPVOverlappedSectorsRejected`
- warning logs in debug builds
- content/editor validation where source asset identity is known

Phase F or later can revisit this with a multi-entry hash or merge strategy, but
that is explicitly out of scope for the initial shipping path.

### 3.5 GI Participation Rules

| Object type | Injects into LPV | Occludes LPV ray tests | Receives LPV in deferred | Notes |
| --- | --- | --- | --- | --- |
| Terrain chunk in active terrain group | Yes | Yes | Yes | Primary world GI source |
| Imported-world chunk with `ParticipatesInGI=true` and valid transform | Yes | Yes | Yes | Static world geometry; same overlap rules as terrain/props |
| Static prop with `ParticipatesInGI=true` and valid transform | Yes | Yes | Yes | Must pass transform validation |
| Dynamic prop or actor | No | No | Yes | Samples world GI only |
| Rotated or non-uniformly scaled voxel object | No | No | Yes | Explicitly rejected in Phase A-E |
| Static prop outside active terrain group semantics | configurable | configurable | Yes | For non-terrain props, terrain group is irrelevant |

### 3.6 Unified World Field

All accepted LPV contributors share one field:

- streamed terrain chunks in `Scene.LPVWorldTerrainGroupID`
- imported-world chunks with `ParticipatesInGI=true`
- static authored voxel props with `ParticipatesInGI=true`

This enables:

- chunk-to-chunk propagation
- terrain-to-prop propagation
- prop-to-prop propagation

without consulting `VisibleObjects`.

## 4. Data Model and Source of Truth

### 4.1 Scene Fields

`core.Scene` needs:

- `LPVWorldTerrainGroupID uint32`
- `LPVAllTerrainGroupsDebug bool`

Do not add `IsStatic` or `ParticipatesInGI` to `core.VoxelObject`; they already
exist there.

### 4.2 Authored / ECS Fields

The missing source-of-truth field is on the gameplay/editor side. It must be
added to the actual authored schemas that produce voxel entities, not only to
`VoxelModelComponent`.

Canonical authored fields to add:

- `content.AssetPartDef.ParticipatesInGI bool`
- `gekko.VoxelObjectDef.ParticipatesInGI bool`
- `content.ImportedWorldDef.ParticipatesInGI bool`

Add to `VoxelModelComponent`:

- `ParticipatesInGI bool`

Terrain chunks do not need an authored opt-in flag in Phase A-E. They are always
treated as LPV contributors when they are in the active terrain group.

Imported-world chunks should inherit a world-level authored default from
`content.ImportedWorldDef.ParticipatesInGI`. Phase A-E does not add per-chunk GI
overrides for imported worlds.

### 4.3 Imported-World Default Policy

Phase 0 must make baked/imported worlds GI-enabled by default.

Concrete rule:

- `ImportedWorldDef.ParticipatesInGI` defaults to `true`

This applies to:

- newly baked imported worlds from `imported_world_baker.go`
- imported-world manifests created or rewritten through editor import flows
- older imported-world manifests loaded from disk that predate the new field

Implementation rule:

- bump the imported-world schema version
- in `EnsureImportedWorldDefaults`, if a manifest loads from an older schema
  version, set `ParticipatesInGI = true`
- for current-schema manifests, preserve the authored boolean as written
- in `BakeImportedWorldFromVox*`, initialize the manifest with
  `ParticipatesInGI = true`

This avoids a migration where legacy or newly baked worlds silently become
GI-dark because the new field decodes as the Go zero value.

Populate renderer object state during sync:

- `obj.ParticipatesInGI = vox.ParticipatesInGI`
- `obj.IsStatic = voxelObjectIsStatic(cmd, entityId, vox)`

`voxelObjectIsStatic(...)` should follow these rules:

1. terrain chunks are always static
2. if a rigid body exists, use `RigidBodyComponent.IsStatic`
3. otherwise default authored voxel placements to static

This keeps GI participation explicit and avoids inferring "static" from random
runtime behavior later.

### 4.4 Authored Spawn Chain That Must Copy the Flag

The roadmap now names the actual spawn sites that must carry authored GI intent
into `VoxelModelComponent`:

- `spawnVoxelObject(...)` in `scene.go`
  - copy `VoxelObjectDef.ParticipatesInGI`
- `spawnAuthoredPart(...)` in `asset_content_spawn.go`
  - copy `content.AssetPartDef.ParticipatesInGI`
- `spawnAuthoredImportedWorldChunkEntity(...)` in `imported_world_spawn.go`
  - copy `AuthoredImportedWorldSpawnDef.ParticipatesInGI`
- `spawnAuthoredTerrainChunkEntity(...)` in `level_content_spawn.go`
  - set `VoxelModelComponent.ParticipatesInGI = true`

Level placements and placement volumes inherit GI participation from the asset
parts they instantiate. Phase 0 does not add a per-placement GI override.

To support imported-world chunks, add:

- `ParticipatesInGI bool` to `AuthoredImportedWorldSpawnDef`

and populate it from the imported-world manifest/load path before chunk entity
spawn.

### 4.5 Editor, Validation, and Bake Ownership

The editor/content layer must expose and validate the new authored fields:

- asset authoring/editor:
  - expose `AssetPartDef.ParticipatesInGI`
- scene / code-defined spawn path:
  - expose `VoxelObjectDef.ParticipatesInGI`
- imported-world authoring:
  - expose `ImportedWorldDef.ParticipatesInGI`
- terrain:
  - no author-facing toggle in Phase A-E; terrain is always GI-active when its
    terrain group is active

Validation rules:

- warn when `ParticipatesInGI=true` is combined with an invalid LPV transform
- warn when LPV contributors overlap at world-sector granularity
- surface overlap warnings in authored asset/level tooling when source identity
  is known

Required implementation surfaces:

- content validation:
  - `content/validation.go`
  - `content/imported_world_validation.go`
- imported-world defaults and bake path:
  - `content/imported_world.go`
  - `imported_world_baker.go`
- asset editor create/import/edit paths:
  - `gekko-editor/src/modules/asset_editor/asset_vox_import.go`
  - `gekko-editor/src/modules/asset_editor/asset_inspector.go`
  - `gekko-editor/src/modules/asset_editor/asset_workflow.go`
- level editor imported-world import/edit paths:
  - `gekko-editor/src/modules/level_editor/base_world_import.go`
  - `gekko-editor/src/modules/level_editor/base_world_ui.go`

The roadmap is not complete unless these authored-tooling surfaces are updated
alongside runtime spawn and renderer sync.

### 4.6 Active Terrain Group Source

Do not introduce a new `LPVConfigComponent`.

The streamed runtime already knows the active terrain group. Synchronize it
directly from `StreamedLevelRuntimeState` into `state.RtApp.Scene` in a small
renderer-sync helper, for example:

- `syncVoxelRtGIState(state, cmd)`

Normal behavior:

- streamed terrain scenes must always set an explicit non-zero
  `LPVWorldTerrainGroupID`
- this should come from the same stable terrain-group resolution already used by
  streamed runtime setup

`LPVWorldTerrainGroupID == 0` is **not** the normal fallback. It means the GI
terrain-group sync is unset or failed unless a deliberate debug override is
enabled.

Debug / compatibility behavior:

- `LPVWorldTerrainGroupID == 0 && LPVAllTerrainGroupsDebug == true`
  means "include all loaded terrain chunks"

Error behavior:

- `LPVWorldTerrainGroupID == 0 && LPVAllTerrainGroupsDebug == false`
  means "terrain LPV selection is invalid/unset"
- in that case, do not silently merge all terrains into one GI field
- instead:
  - exclude terrain chunks from LPV participation for that frame
  - log a warning in debug builds
  - increment a renderer counter such as `LPVMissingTerrainGroupFrames`

This keeps wiring bugs visible instead of papering over them by merging
unrelated loaded terrains into one GI field.

## 5. Transform Acceptance Rules

LPV contributors must map cleanly into world voxel space.

An object is accepted as an LPV injector only if all of the following are true:

1. rotation is identity within epsilon
2. `scale.x == scale.y == scale.z == scene.TargetVoxelSize` within epsilon
3. `position / scene.TargetVoxelSize` is integer-snapped within epsilon
4. `pivot` is integer-snapped in voxel units within epsilon

Recommended epsilon:

- `1e-4` for scale
- `1e-4` for voxel-space snapping
- quaternion acceptance:
  - `abs(x) <= 1e-4`
  - `abs(y) <= 1e-4`
  - `abs(z) <= 1e-4`
  - `abs(w - 1.0) <= 1e-4`

World voxel origin for accepted objects:

```text
world_object_origin_vox =
    round(position / voxel_scale) - round(pivot)
```

For a sector at local sector coord `(sx, sy, sz)`:

```text
world_sector_origin_vox =
    world_object_origin_vox + vec3i(sx, sy, sz) * 32
```

This handles centered pivots correctly. Do not derive LPV world origins from
`Transform.Position` alone.

Rejected contributors:

- do not inject
- do not occlude LPV traversal
- still receive LPV during deferred lighting

## 6. GPU Data Layout

### 6.1 Brick Irradiance

Two ping-pong storage buffers indexed by:

```text
brick_global = sector_slot * 64 + brick_local
```

```wgsl
struct BrickIrradiance {
    r: f32,
    g: f32,
    b: f32,
    flags: u32, // bit 0: has_energy
};
```

Stride: `16` bytes.

Buffers:

- `BrickIrradianceABuf`
- `BrickIrradianceBBuf`

### 6.2 LPV Config Uniform

```wgsl
struct LPVConfig {
    camera_pos: vec3<f32>,       // 0
    gi_radius: f32,              // 12
    propagation_atten: f32,      // 16
    direct_scale: f32,           // 20
    emissive_scale: f32,         // 24
    voxel_scale: f32,            // 28
    sector_count: u32,           // 32
    light_count: u32,            // 36
    frame_index: u32,            // 40
    active_terrain_group: u32,   // 44
    hash_grid_size: u32,         // 48
    hash_grid_mask: u32,         // 52
    propagation_steps: u32,      // 56
    debug_flags: u32,            // 60
};
```

Size: `64` bytes.

### 6.3 LPV Sector Metadata

```wgsl
struct LPVSectorMeta {
    world_origin_vox: vec4<i32>, // xyz = world voxel origin, w = flags
    material_table_base: u32,    // byte offset into MaterialBuf
    participant_index: u32,      // dense LPV participant index, debug only
    reserved0: u32,
    reserved1: u32,
};
```

Flags in `world_origin_vox.w`:

- bit 0: `LPV_WORLD`
- bit 1: `LPV_ALLOW_EMISSIVE`
- bit 2: `LPV_TERRAIN`
- bit 3: `LPV_STATIC_PROP`
- bit 4: `LPV_OVERLAPPED`

Stride: `32` bytes.

### 6.4 LPV Spatial Hash Entry

```wgsl
struct LPVSpatialHashEntry {
    coords: vec4<i32>; // xyz = world sector coord, w = sector_slot or -1
};
```

Stride: `16` bytes.

### 6.5 Existing Buffers Reused by LPV

- `SectorTableBuf`
- `BrickTableBuf`
- `LightsBuf`
- `ShadowLayerParamsBuf`
- `ShadowMapView`
- `MaterialBuf`
- voxel payload pages for future emissive injection

LPV does not depend on:

- `InstancesBuf`
- `ObjectParamsBuf`
- `TerrainChunkLookupBuf`

for its core world-field logic.

## 7. Host-Side Build Pipeline

Create `gpu/manager_lpv.go`.

### 7.1 New Fields in `GpuBufferManager`

Add:

- `BrickIrradianceABuf *wgpu.Buffer`
- `BrickIrradianceBBuf *wgpu.Buffer`
- `LPVConfigBuf *wgpu.Buffer`
- `LPVSectorMetaBuf *wgpu.Buffer`
- `LPVSpatialHashBuf *wgpu.Buffer`
- `LPVHashGridSize uint32`
- `LPVHashGridMask uint32`

Add profiler / observability counters:

- `LPVContributingObjects`
- `LPVContributingSectors`
- `LPVRejectedObjects`
- `LPVOverlappedSectorCoords`
- `LPVOverlappedSectorsRejected`
- `LPVMissingTerrainGroupFrames`
- `LPVHashMaxProbe`
- `LPVHashInsertCollisions`

### 7.2 CPU-Only LPV Participant Extraction

Build a dense temporary list every `UpdateScene()`:

```go
type lpvParticipant struct {
    Object            *core.VoxelObject
    ParticipantIndex  uint32
    MaterialTableBase uint32
    WorldOriginVox    [3]int32
    Flags             uint32
}
```

Participant extraction rules:

1. iterate `scene.Objects`
2. skip nil or missing `XBrickMap`
3. terrain chunk:
   - include iff:
     - `scene.LPVAllTerrainGroupsDebug && scene.LPVWorldTerrainGroupID == 0`, or
     - `scene.LPVWorldTerrainGroupID != 0 && obj.TerrainGroupID == scene.LPVWorldTerrainGroupID`
   - if `scene.LPVWorldTerrainGroupID == 0 && !scene.LPVAllTerrainGroupsDebug`,
     terrain chunks are rejected and the missing-group counter is incremented
4. non-terrain prop:
   - include iff `obj.IsStatic && obj.ParticipatesInGI`
5. validate transform using Section 5
6. compute `MaterialTableBase` from `GpuBufferManager.Allocations[obj.XBrickMap]`
7. compute `WorldOriginVox`

Imported-world chunks participate through `obj.ParticipatesInGI`, not through
terrain-group membership.

### 7.3 LPV Sector Metadata Upload

For each accepted participant and each local sector:

1. look up `sector_slot` via `m.SectorToInfo[sector].SlotIndex`
2. compute `world_sector_origin_vox`
3. write `LPVSectorMetaBuf[sector_slot]`

For all non-participating sector slots, write zeroed metadata with `flags = 0`.

Before finalizing metadata, run overlap detection from Section 3.4. Any sector
marked `LPV_OVERLAPPED` must have `LPV_WORLD` cleared before shader-visible
buffers are uploaded.

Important:

- size this buffer to `max(1, m.SectorAlloc.Tail) * 32`
- rebuild it after voxel/sector allocations are current

### 7.4 LPV Spatial Hash Upload

Build from the metadata buffer, not from object indices.

Only sectors with:

- `LPV_WORLD != 0`
- `LPV_OVERLAPPED == 0`

may be inserted into the hash.

Hash key:

```text
hash(x, y, z) = x * 73856093 ^ y * 19349663 ^ z * 83492791
```

Sizing:

- next power of two >= `accepted_sector_count * 4`
- minimum `16`

Insertion:

- linear probing
- write `-1` tombstones into empty entries
- track `LPVHashMaxProbe` and `LPVHashInsertCollisions` on the CPU

### 7.5 Buffer Allocation Timing

Call LPV setup at the end of `UpdateScene()` after:

- `UpdateVoxelData(scene)`
- `updateSectorGrid(scene)`

This guarantees:

- `SectorAlloc.Tail` is correct
- `SectorToInfo` is correct
- `ObjectGpuAllocation.MaterialOffset` is available

## 8. Final Bind Group Layouts

Do not force inject and propagate to share an identical bind-group layout if the
resources differ. That optimization created ambiguity in the earlier draft.

### 8.1 Inject Pass

Group 0:

| Binding | Resource |
| --- | --- |
| 0 | `LPVConfigBuf` |
| 1 | `BrickIrradianceWrite` |
| 2 | `LightsBuf` |
| 3 | `ShadowLayerParamsBuf` |
| 4 | `ShadowMapView` |

Group 1:

| Binding | Resource |
| --- | --- |
| 0 | `SectorTableBuf` |
| 1 | `BrickTableBuf` |
| 2 | `LPVSectorMetaBuf` |
| 3 | `LPVSpatialHashBuf` |
| 4 | `MaterialBuf` |
| 5 | `VoxelPayloadView[0]` |
| 6 | `VoxelPayloadView[1]` |
| 7 | `VoxelPayloadView[2]` |
| 8 | `VoxelPayloadView[3]` |

Phase A-B can leave emissive logic disabled even though the bindings exist.

### 8.2 Propagate Pass

Group 0:

| Binding | Resource |
| --- | --- |
| 0 | `LPVConfigBuf` |
| 1 | `BrickIrradianceRead` |
| 2 | `BrickIrradianceWrite` |

Group 1:

| Binding | Resource |
| --- | --- |
| 0 | `SectorTableBuf` |
| 1 | `BrickTableBuf` |
| 2 | `LPVSectorMetaBuf` |
| 3 | `LPVSpatialHashBuf` |

### 8.3 Deferred Lighting Group 2

Current group 2 only binds `materials` at binding `3`.

After LPV integration, group 2 becomes:

| Binding | Resource |
| --- | --- |
| 0 | `LPVConfigBuf` |
| 1 | `BrickIrradianceFinalBuf` |
| 2 | `SectorTableBuf` |
| 3 | `MaterialBuf` |
| 4 | `LPVSectorMetaBuf` |
| 5 | `LPVSpatialHashBuf` |

This deliberately avoids `ObjectParamsBuf` and `InstancesBuf`.

## 9. Shader Logic

### 9.1 Inject Pass

Each thread handles one brick slot:

1. clear output irradiance
2. reject if sector metadata has no `LPV_WORLD`
3. reject if brick does not exist or is fully solid
4. compute brick center from `LPVSectorMeta.world_origin_vox`
5. early-out if outside `gi_radius`
6. inject direct light from point / spot / directional lights

Brick center world position:

```wgsl
let brick_center_vox = vec3<f32>(
    f32(meta.world_origin_vox.x) + f32(brick_local % 4u) * 8.0 + 4.0,
    f32(meta.world_origin_vox.y) + f32((brick_local / 4u) % 4u) * 8.0 + 4.0,
    f32(meta.world_origin_vox.z) + f32(brick_local / 16u) * 8.0 + 4.0,
);
let brick_center_ws = brick_center_vox * lpv_config.voxel_scale;
```

Phase A:

- point + spot lights
- no shadow-map dependency required yet
- nearest-brick sampling in deferred

Phase C:

- directional injection
- shadow visibility
- face-openness attenuation

### 9.2 Point-Light Occlusion Rule

Point-light injection should use a world-space DDA or stepped segment test over
the LPV world field using `get_lpv_sector_slot()`.

That traversal only considers LPV contributors:

- terrain
- imported-world chunks accepted into LPV
- accepted static GI props

This is acceptable for the target FPS. Dynamic actors are not LPV occluders in
Phase A-E.

### 9.3 Propagation Pass

For each brick:

1. read injected/base energy from input
2. inspect 6 axis neighbors
3. resolve neighbor sector via `LPVSpatialHashBuf`
4. reject neighbors whose metadata lacks `LPV_WORLD`
5. reject neighbors marked `LPV_OVERLAPPED`
6. apply face-blocking and attenuation
7. write accumulated result

Use `LPVSectorMeta.world_origin_vox` and world-sector coordinates for
cross-object propagation. Do not use `base_idx` from the camera-facing sector
grid for LPV world traversal.

### 9.4 Deferred Sampling

LPV sampling uses only:

- hit world position from the G-buffer
- world normal
- `LPVSpatialHashBuf`
- `LPVSectorMetaBuf`
- `SectorTableBuf`
- `BrickIrradianceFinalBuf`

Pseudo flow:

1. convert `hit_pos_ws` to world voxel position
2. offset slightly along the normal
3. compute world sector coord via `floor(voxel_pos / 32.0)`
4. look up `sector_slot` in `LPVSpatialHashBuf`
5. compute local voxel coord using `LPVSectorMeta.world_origin_vox`
6. if no slot is found because the coord is absent or overlapped, return zero LPV
7. read nearest brick in Phase A-C
8. add trilinear later in Phase E

Integration into `deferred_lighting.wgsl`:

```wgsl
let ambient_light = sample_directional_sky_ambient(normal, ambient_occlusion);
let ambient_term = (ambient_kd * base_color + ambient_fresnel) * ambient_light;
var indirect_color = ambient_term * ambient_occlusion;

let lpv_irradiance = sample_brick_gi(hit_pos_ws, normal);
let lpv_diffuse = (ambient_kd * base_color) * lpv_irradiance;
indirect_color += lpv_diffuse * ambient_occlusion;
```

Do not replace sky ambient with LPV. LPV is a local bounce term, not the whole
ambient model.

## 10. Render-Loop Integration

`App.Update()`:

1. commit scene
2. `BufferManager.UpdateScene(...)`
3. if LPV buffers recreated, rebuild LPV and lighting bind groups
4. update camera uniforms

`App.Render()`:

1. particle sim
2. CA sim
3. G-buffer
4. Hi-Z
5. shadows
6. tile light cull
7. upload LPV config
8. LPV inject
9. LPV propagate `N` steps
10. deferred lighting
11. debug / accumulation / resolve

Insert LPV after tile-light cull and before deferred lighting.

Add profiler scopes:

- `LPV Inject`
- `LPV Propagate`

## 11. Destruction and Upload Policy

LPV itself is stateless with respect to edits:

- when updated voxel occupancy reaches GPU memory, LPV immediately adapts

The real latency bottleneck is `UpdateVoxelData()` upload scheduling.

This roadmap therefore includes upload policy work, not just GI shaders.

### 11.1 Required Upload Prioritization

Modify `manager_voxel.go` so dirty sector upload is prioritized by LPV relevance:

1. sectors inside `gi_radius` around the camera
2. sectors belonging to visible objects
3. remaining dirty sectors by distance

Recommended implementation:

- gather dirty sectors into a temporary slice with distance and priority class
- stable sort
- upload up to `SectorsPerFrame`

### 11.2 Optional Burst Budget

Add a temporary budget boost path for heavy destruction events:

- base budget stays unchanged during normal frames
- explosions or scripted collapse events can temporarily raise
  `SectorsPerFrame`

This should be driven by gameplay/event code later, but the upload path should
support it.

## 12. Observability and Debugging

Add or expose:

- profiler times for LPV inject and propagate
- `LPVContributingObjects`
- `LPVContributingSectors`
- `LPVRejectedObjects`
- `LPVOverlappedSectorCoords`
- `LPVOverlappedSectorsRejected`
- `LPVMissingTerrainGroupFrames`
- `LPVHashInsertCollisions`
- `LPVHashMaxProbe`
- `VoxelDirtySectorsPending`
- `VoxelDirtyBricksPending`

Add temporary debug toggles:

- LPV enabled/disabled
- LPV radius
- propagation steps
- LPV-only indirect contribution visualization

Re-use existing runtime tuning key patterns rather than inventing a separate UI.

## 13. Verification

### 13.1 Unit / Package Checks

Run from `gekko/`:

- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Add tests for:

- LPV transform validation
- world-origin voxel computation with non-zero pivot
- LPV spatial-hash lookup and collision handling
- LPV sector metadata build for mixed terrain + static props
- overlap rejection when terrain / imported-world / static-prop sectors collide
- authored GI flag propagation from asset parts, scene defs, imported worlds, and terrain spawn
- imported-world schema migration keeps legacy baked worlds GI-enabled by default
- content validation covers the new asset/imported-world GI fields
- `LPVWorldTerrainGroupID == 0` excludes terrain unless the explicit debug-all-terrain override is enabled

### 13.2 Visual Smoke Checks

1. Indoor point light:
   - indirect blob visible in `RenderModeIndirect`
2. Hallway corner:
   - bounce reaches around one corner after propagation
3. Sunlit room with window:
   - directional bounce appears after Phase C
4. Destruction hole:
   - indirect spill changes as soon as edited sectors upload
5. Static prop beside terrain chunk:
   - propagation crosses terrain/prop boundary without seams
6. Dynamic actor in lit room:
   - receives LPV even though it does not inject
7. Intentional terrain/prop overlap case:
   - overlapped coord is reported and rejected deterministically rather than depending on insertion order
8. Broken terrain-group sync case:
   - with `LPVWorldTerrainGroupID == 0` and no debug override, terrain GI is disabled and the warning/counter fires

## 14. Implementation Phases

### Phase 0: Baseline Cleanup and Data Ownership

1. update stale GI docs to point at LPV
2. add authored `ParticipatesInGI` fields to `content.AssetPartDef`, `VoxelObjectDef`, and `content.ImportedWorldDef`
3. bump imported-world schema version and set legacy imported-world manifests to
   default `ParticipatesInGI=true` during load/defaulting
4. update bake/import defaults so newly baked imported worlds set
   `ParticipatesInGI=true`
5. add `ParticipatesInGI` to `VoxelModelComponent`
6. wire spawn sites:
   - `scene.go`
   - `asset_content_spawn.go`
   - `imported_world_spawn.go`
   - `level_content_spawn.go`
7. update content validation and editor/tooling surfaces:
   - `content/validation.go`
   - `content/imported_world_validation.go`
   - `imported_world_baker.go`
   - `gekko-editor/src/modules/asset_editor/asset_vox_import.go`
   - `gekko-editor/src/modules/asset_editor/asset_inspector.go`
   - `gekko-editor/src/modules/asset_editor/asset_workflow.go`
   - `gekko-editor/src/modules/level_editor/base_world_import.go`
   - `gekko-editor/src/modules/level_editor/base_world_ui.go`
8. add renderer sync for `obj.ParticipatesInGI` and `obj.IsStatic`
9. add explicit `Scene.LPVWorldTerrainGroupID` sync from streamed runtime and do
   not rely on `0 == include all`
10. implement LPV transform validation helpers and overlap detection tests

Exit criteria:

- no ambiguity about GI source of truth
- no plan text still relying on visible-object indices
- authored content has a canonical way to opt in to GI
- baked and legacy imported worlds are GI-enabled by default unless explicitly disabled in the new schema
- streamed terrain scenes always drive a non-zero LPV terrain group in the normal path
- overlap behavior is deterministic and documented

### Phase A: Minimal LPV Path

1. add LPV buffers and bind groups
2. implement `LPVSectorMetaBuf` build and `LPVSpatialHashBuf`
3. implement inject pass for point / spot lights only
4. sample nearest brick in deferred lighting
5. keep propagation disabled

Exit criteria:

- point light creates stable per-brick indirect contribution
- off-screen streamed terrain chunks still contribute correctly
- overlapping contributors are rejected deterministically instead of aliasing by insertion order

### Phase B: Propagation

1. implement neighbor resolution via LPV spatial hash
2. add 2-4 propagation passes
3. tune attenuation and face blocking

Exit criteria:

- indirect light spreads around corners
- propagation crosses terrain-to-prop boundaries

### Phase C: Directional Light and Shadows

1. directional injection
2. shadow visibility sampling
3. top-face / face-openness weighting

Exit criteria:

- windows and open roofs produce readable sun bounce

### Phase D: Destruction Response and Observability

1. prioritize dirty uploads by LPV relevance
2. add temporary burst upload budget support
3. expose LPV profiler counters and debug toggles

Exit criteria:

- destruction latency is dominated by configured upload budget, not GI logic
- LPV performance is inspectable frame to frame

### Phase E: Quality Polish

1. trilinear brick sampling
2. emissive injection using `MaterialBuf` and voxel payload pages
3. runtime tuning controls
4. final parameter tuning for FPS readability

Exit criteria:

- no obvious block popping at normal camera distances
- emissive voxels contribute without blowing out face readability

## 15. Files to Create

- `voxelrt/rt/gpu/manager_lpv.go`
- `voxelrt/rt/gpu/manager_lpv_test.go`
- `voxelrt/rt/shaders/brick_lpv_inject.wgsl`
- `voxelrt/rt/shaders/brick_lpv_propagate.wgsl`

## 16. Files to Modify

- `voxelrt/rt/gpu/manager.go`
  - add LPV buffers, counters, and bind-group handles
- `voxelrt/rt/gpu/manager_scene.go`
  - build/upload LPV metadata after voxel allocations are current
- `voxelrt/rt/gpu/manager_render_setup.go`
  - create LPV bind groups and expand deferred-lighting group 2
- `voxelrt/rt/gpu/manager_voxel.go`
  - add LPV-aware dirty upload prioritization
- `voxelrt/rt/app/app.go`
  - create LPV pipelines
- `voxelrt/rt/app/app_frame.go`
  - dispatch LPV passes before deferred lighting
- `voxelrt/rt/shaders/deferred_lighting.wgsl`
  - add LPV sampling and bindings
- `voxelrt/rt/shaders/shaders.go`
  - embed new LPV shaders
- `mod_voxelrt_client_systems.go`
  - sync `ParticipatesInGI`, `IsStatic`, `LPVWorldTerrainGroupID`, and the explicit all-terrain debug override
- `vox_rt_snapshot.go`
  - add `ParticipatesInGI` to `VoxelModelComponent`
- `scene.go`
  - add `ParticipatesInGI` to `VoxelObjectDef` and copy it in `spawnVoxelObject`
- `asset_content_spawn.go`
  - copy `content.AssetPartDef.ParticipatesInGI` into `VoxelModelComponent`
- `imported_world_spawn.go`
  - carry imported-world GI participation through spawn defs into `VoxelModelComponent`
- `level_content_spawn.go`
  - set terrain chunk `VoxelModelComponent.ParticipatesInGI = true`
- `voxelrt/rt/core/scene.go`
  - add `LPVWorldTerrainGroupID` and `LPVAllTerrainGroupsDebug`
- `streamed_level_runtime.go`
  - no new GI ECS component; expose active terrain group to renderer sync
- `content/asset.go`
  - add `AssetPartDef.ParticipatesInGI`
- `content/imported_world.go`
  - add `ImportedWorldDef.ParticipatesInGI` and imported-world default migration behavior
- `content/validation.go`
  - validate asset GI fields and surface transform/overlap-related authoring errors where possible
- `content/imported_world_validation.go`
  - validate imported-world GI field usage and migration/default expectations
- `imported_world_baker.go`
  - set baked imported worlds to `ParticipatesInGI=true` by default
- `docs/renderer/overview.md`
- `docs/renderer/runtime.md`
- `docs/roadmaps/renderer-lighting.md`
- `gekko-editor/src/modules/asset_editor/asset_vox_import.go`
  - initialize imported asset parts with an explicit GI participation default
- `gekko-editor/src/modules/asset_editor/asset_inspector.go`
  - expose per-part GI participation editing
- `gekko-editor/src/modules/asset_editor/asset_workflow.go`
  - ensure validation/save flows surface the new field cleanly
- `gekko-editor/src/modules/level_editor/base_world_import.go`
  - preserve baked imported-world GI defaults during import flow
- `gekko-editor/src/modules/level_editor/base_world_ui.go`
  - expose imported-world GI participation state in the level/base-world UI

## 17. Hard Rules for Implementers

- do not index LPV through `scene.VisibleObjects`
- do not reuse `TerrainChunkLookupBuf` for LPV
- do not compute LPV world origins from `Transform.Position` alone
- do not accept rotated or non-uniformly scaled GI contributors in Phase A-E
- do not replace sky ambient with LPV
- do not add a second production GI architecture beside LPV
