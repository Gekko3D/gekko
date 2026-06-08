# Streaming and Worlds

This page documents the terrain, imported-world, world-delta, and streamed-level pieces that sit around authored levels.

Use it when working on:

- terrain baking and terrain chunk spawn
- imported voxel worlds
- streamed level runtime
- persistent world overrides and voxel snapshots

For the top-level level format, see [`levels.md`](levels.md).

## World Data Layers

The current stack has four related layers:

1. terrain sources
   - authorable heightfield data in `.gkterrain`
2. terrain chunk manifests and chunks
   - baked terrain runtime data
3. imported worlds
   - chunked voxel-world manifests and chunks in `.gkworld` plus `.gkchunk`
4. world deltas
   - persistent runtime overrides on top of a level

## Terrain

### Terrain source

`TerrainSourceDef` is the authored source model.

Important fields:

- `kind`
- `sample_width`
- `sample_height`
- `height_samples`
- `world_size`
- `height_scale`
- `voxel_resolution`
- `chunk_size`

Current terrain kind:

- `heightfield`

### Baking terrain

The main bake flow lives in `content/terrain_bake.go`.

Important helpers:

- `TerrainBakeSourceHash(...)`
- `BakeTerrainChunks(...)`
- `BakeTerrainChunkSet(...)`
- `DefaultTerrainManifestPath(...)`
- `DefaultTerrainChunkDir(...)`

Baking produces:

- one terrain manifest
  - `.gkterrainmanifest`
- many terrain chunks
  - `.gkchunk`

### Runtime terrain consumption

Eager authored-level spawn:

- loads the terrain manifest
- loads non-empty terrain chunks
- spawns chunk entities under the level root

Streamed runtime:

- keeps terrain chunk metadata in streamed state
- loads chunks on demand per active chunk observer

## Imported Worlds

Imported worlds are chunked voxel worlds, usually baked from VOX data.

### Manifest and chunks

Manifest:

- `ImportedWorldDef`
- stored as `.gkworld`

Chunks:

- `ImportedWorldChunkDef`
- stored as `.gkchunk`

Manifest entries point to chunk files relative to the manifest path.

### Baking from VOX

The main bake code lives in:

- `imported_world_baker.go`

Important helpers:

- `BakeImportedWorldFromVoxFile(...)`
- `BakeImportedWorldFromVox(...)`
- `SaveImportedWorldBake(...)`
- `BuildImportedWorldBakeReport(...)`

The baker:

- flattens source voxels
- optionally normalizes origin
- partitions voxels into chunks
- preserves palette information
- emits warnings for large masses, thin features, and chunk hotspots

### Runtime imported-world consumption

Imported worlds are mainly consumed by streamed runtime, not eager `SpawnAuthoredLevel(...)`.

The streamed runtime:

- loads the imported-world manifest from `level.BaseWorld.ManifestPath`
- tracks chunk entries by chunk coordinate
- loads chunk files on demand
- spawns chunk entities with optional collision
- reuses palette data from the imported-world manifest

Spawn helpers live in:

- `imported_world_spawn.go`

## Streamed Level Runtime

The streamed runtime is implemented in `streamed_level_runtime.go`.

It extends authored levels with:

- chunk observer driven loading
- prepared background chunk loads
- placement chunking
- terrain streaming
- imported base-world streaming
- world-delta override application

Important state:

- `StreamedLevelRuntimeState`
- desired, pending, and loaded chunk maps
- terrain entry map
- imported-world entry map
- placement chunk map
- world-delta and override maps

Important public entry point:

- `StartStreamedLevelRuntime(...)`

## Long-Term Streaming Plan

The current streamed runtime is chunk-radius based:

- an observer computes desired chunk coordinates
- chunk files are prepared on background goroutines
- prepared chunks are committed by spawning entities
- chunks outside the desired set are unloaded

This is a useful first runtime, but it has three known scaling problems on large
imported worlds:

- committing prepared chunks can still create main-thread/GPU upload spikes
- missing chunks create visible holes while their full-resolution data is not
  resident
- increasing chunk radius raises object count, memory use, collision work, and
  GPU upload pressure

The long-term goal is not "load more chunks." The goal is:

- no visible holes
- bounded main-thread commit cost
- full-resolution destruction only where gameplay needs it
- map-aware visibility for indoor imports
- clipmap-style distance handling for open worlds

### Target Model

Use a hierarchy:

1. **Chunk**
   - Small storage/edit/destruction unit.
   - Keeps existing `.gkchunk` semantics where practical.
   - Imported-world chunks support readable JSON payloads and compact
     `dense_rle_binary_v1` payloads; runtime loading auto-detects both.

2. **Sector/Page**
   - Larger streaming and render-residency unit.
   - Owns many chunks or references many chunks.
   - Typical target size should be measured, but a useful first range is
     `12-25m` world-space pages for imported HL1-style levels.

3. **LOD/Proxy**
   - Coarse representation that can be resident before full-resolution chunks.
   - Used to prevent holes.
   - Not necessarily editable or destructible.

4. **Visibility Provider**
   - Decides which sectors are needed.
   - Radius is only the fallback provider.
   - HL1/BSP imports should use room/leaf/PVS-style visibility.
   - Minecraft/open worlds should use clipmap rings.

5. **Commit Scheduler**
   - Applies prepared sector/chunk data to ECS and renderer under a per-frame
     budget.
   - Prevents spikes when many prepared loads complete together.

### Residency States

A streamed world region should move through explicit states:

```text
absent
  -> proxy_requested
  -> proxy_ready
  -> proxy_committed
  -> full_requested
  -> full_ready
  -> full_committed
```

Rules:

- A region may show a proxy while full data is loading.
- Full data may replace a proxy only after the full representation is ready.
- Full data must not be unloaded until a proxy or parent LOD is committed.
- Collision/destruction may be absent for proxy-only regions.
- Near gameplay regions should require full data before interaction.

This is the "no-hole" contract.

### Step-By-Step Implementation Plan

#### Step 1: Add Streaming Observability

Before changing architecture, add counters and traces around the current runtime:

- desired chunk count
- pending load count
- prepared queue depth
- loaded chunk count
- chunks committed per frame
- time spent in `prepareStreamedChunkLoad(...)`
- time spent in `commitPreparedStreamedChunk(...)`
- entity count created by each commit
- GPU upload/structure revision pressure if available

Verification:

- Run an imported HL1 map.
- Move through the level and record where freezes happen.
- Confirm whether spikes are from file decode, ECS spawn, geometry registration,
  collision setup, GPU upload, or command flush.

#### Step 2: Budget Main-Thread Commit

Change `commitPreparedStreamedChunksSystem(...)` so it does not drain all
prepared work in one frame.

Add runtime config such as:

- `MaxChunkCommitsPerFrame`
- `MaxStreamingCommitMillis`
- optional priority class: player chunk, visible chunk, prefetch chunk

Rules:

- Commit the player/current chunk first.
- Commit visible chunks before prefetch chunks.
- Stop committing when the frame budget is spent.
- Keep prepared data queued for later frames.

Verification:

- Existing streamed runtime tests still pass.
- Add a test where several prepared chunks are queued and only a budgeted number
  commits in one update.
- Manual check: movement through `gasworks` no longer produces large commit
  spikes, even if holes may still exist at this step.

Implementation note, 2026-06-08:

- Prepare-side imported-world geometry staging was attempted by building
  `XBrickMap` geometry and bounds on streaming worker goroutines before commit.
- Direct runtime-owned map adoption made `commit_last_ms` tiny, but crashed
  during `gasworks_128` testing with an invalid wgpu bind group.
- A copied prepared-map registration path and then atomic `XBrickMap` ID
  allocation were also tested; both still crashed under the same
  `gasworks_128` renderer load.
- Prepared imported-world geometry staging is disabled. Imported-world full
  chunks and sector proxies again build their voxel geometry during entity
  spawn on the stable renderer path.
- Atomic `XBrickMap` ID allocation remains because concurrent map construction
  is independently possible and IDs are uploaded into GPU scene data.
- The next commit/upload optimization should be renderer-owned staging or GPU
  upload scheduling, not publishing worker-built `XBrickMap` instances into
  live voxel model assets.

#### Step 3: Add Hysteresis And Prefetch Rings

Separate load and unload decisions:

- load radius
- keep radius
- prefetch radius

Rules:

- Do not unload a region as soon as it leaves the visible/load radius.
- Keep recently used chunks for a short grace period or larger keep radius.
- Prefetch likely next chunks ahead of the observer.

Verification:

- Move back and forth across a chunk boundary.
- Confirm chunks do not unload/reload repeatedly.
- Confirm memory stays bounded.

#### Step 4: Introduce Sector/Page Metadata

Add a manifest layer above chunks:

```text
.gkworld
  sectors:
    coord
    bounds
    lod_paths
    full_chunk_refs
    visibility_id
```

At first, a sector can be a grouping of existing chunk entries. This avoids
rewriting all chunk payloads immediately.

Runtime state should gain:

- desired sectors
- pending sectors
- loaded sectors
- sector-to-chunk mapping
- sector LOD state

Verification:

- Schema v1 `.gkworld` files are re-imported to schema v2 before runtime use.
- Sector manifests load through the sector path.
- Sector grouping does not change world scale or chunk placement.

Implementation note, 2026-06-08:

- `.gkworld` schema v2 adds `sectors`.
- Chunk `entries` remain the file catalog.
- Sector `full_chunk_refs` are now the runtime-facing imported-world grouping.
- The streamed runtime indexes imported-world chunks through sectors before
  falling back to chunk preparation/commit.
- Backward compatibility with schema v1 manifests is intentionally not
  preserved; re-import old worlds to regenerate schema v2 metadata.

#### Step 5: Generate Coarse Proxy LODs At Import/Bake Time

For imported worlds, generate proxy payloads during import:

- `lod0`: full-resolution chunks
- `lod1`: simplified voxel sector or merged lower-resolution chunks
- `lod2`: very coarse silhouette/occluder/proxy

Proxy data should preserve:

- approximate occupied shape
- dominant material colors
- water/transparent exclusions where needed
- emissive surfaces if visually important

Proxy data does not need:

- full destruction
- exact per-voxel material detail
- gameplay collision accuracy

Verification:

- Load a map with only `lod1`/`lod2` enabled.
- Confirm the level has no holes from normal viewing distances.
- Confirm full-res data can replace proxy data without transform seams.

Implementation note, 2026-06-08:

- Sector `lods` metadata now supports proxy chunk references.
- Import/bake emits one `lod1` `voxel_proxy` chunk per sector under `lods/`.
- Proxy chunks downsample full sector voxels by 4x and preserve the dominant
  material in each coarse cell.
- Runtime and editor preview can consume proxy LODs as cheap visual fallback
  data. Proxy LODs are still not collision-accurate, destructible, or editable.

#### Step 6: Implement No-Hole Replacement

Change runtime residency so a sector always has a visible fallback before full
data is unloaded.

Rules:

- If full data is missing, show best available proxy.
- If full data becomes ready, atomically replace or hide the proxy after full
  commit succeeds.
- If full data unloads, restore proxy first.
- Never let desired-but-not-ready full chunks appear as empty world.

Verification:

- Artificially slow chunk loading.
- Move quickly through the level.
- Confirm visible holes do not appear.

Implementation note, 2026-06-08:

- The streamed runtime now derives desired/keep sectors from desired/keep
  imported-world chunks.
- Sector `lod1` proxy chunks are prepared and committed as visual-only entities
  before full chunk commits when available.
- Proxy entities do not get collision components.
- A sector proxy is hidden after all full non-empty chunks referenced by the
  sector are loaded.
- Sector proxy visual residency is now separate from full chunk visual
  residency. Proxy sectors can be kept as a cheap far-world fallback, hidden
  while all full chunks for the sector are resident, and shown before full
  chunks unload so the base world does not disappear at distance.
- Streaming metrics now include loadable desired/keep chunk counts,
  collision-resident chunk counts, desired/keep sector counts, proxy pending
  and prepared queues, and loaded proxy counts. The original `desired` and
  `keep` counters still include empty radius coordinates for compatibility.
- Runtime visibility can consume imported-world sector metadata; radius
  streaming remains the fallback when sector visibility metadata is absent.

#### Step 7: Split Render, Collision, And Destruction Residency

A sector can have separate residency for:

- visual proxy
- full visual voxels
- collision
- destructible/editable data

Near sectors:

- full visual
- collision
- destruction

Middle sectors:

- full or mid visual
- optional simplified collision
- no destruction

Far sectors:

- proxy visual
- no collision
- no destruction

Verification:

- Player cannot interact with proxy-only sectors.
- Destruction works after a sector reaches full residency.
- Far sectors remain visible but cheap.

Implementation note, 2026-06-08:

- Imported-world full visual chunk residency is now separate from collision
  residency.
- Observer `PrefetchRadius` requests full visual chunks.
- Observer `Radius` feeds the current-sector/visibility seed.
- Observer `CollisionRadius`, or `GEKKO_STREAMING_COLLISION_RADIUS` in
  `actiongame`, requests near collision residency. When unset, collision
  residency follows `Radius` to preserve existing behavior.
- Observer `DestructionRadius`, or `GEKKO_STREAMING_DESTRUCTION_RADIUS` in
  `actiongame`, requests near destruction/editability residency. When unset,
  destruction residency follows collision residency.
- Full imported-world chunks outside collision residency are committed without
  rigid body, collider, or AABB components.
- Full imported-world chunks inside collision residency are committed with
  collision components.
- Full imported-world chunks outside destruction residency ignore destruction
  events. Full imported-world chunks inside destruction residency receive an
  explicit residency marker and use the existing voxel destruction path.
- Streaming logs include `collision_loadable`, `proxy_committed_frame`,
  `destruction_loadable`, `proxy_committed_frame`, `full_committed_frame`,
  `collision_committed_frame`, and matching total counters so visual, proxy,
  collision, and destruction residency can be compared.
- Already-loaded full chunks are not promoted/demoted in place yet; mutating
  renderer-active chunk entities caused stale GPU bind-group validation failures
  during visual testing.
- Imported-world destruction persistence writes dirty imported chunks to
  `ImportedWorldChunkOverrides` in the world delta. The imported source world
  remains immutable; runtime loads the saved override chunk before falling back
  to the imported manifest entry.

#### Step 8: Add HL1/BSP Visibility Provider

For HL1 imports, radius streaming should become a fallback. The importer should
emit a visibility map derived from BSP structure:

- leaf or room id per sector
- sector bounds
- PVS/portal-style visible sector set
- adjacency/prefetch links

Runtime should request:

- current sector
- sectors visible from current sector
- adjacent sectors likely to be entered soon
- proxy fallback for nearby but currently hidden sectors

Verification:

- In corridor maps, loaded full-res sectors follow rooms/corridors instead of a
  sphere.
- Looking through doorways or around corners has required sectors ready.
- Hidden rooms do not cost full-resolution rendering/collision.

Implementation note, 2026-06-08:

- HL1 BSP imports now retain the raw visibility lump and can decode compressed
  PVS bitsets for playable leafs.
- Imported-world sectors now carry `source_leaf_ids`,
  `visible_sector_refs`, and `adjacent_sector_refs` in addition to
  `visibility_id`.
- The HL1 debug world importer annotates sectors by overlapping sector bounds
  with playable BSP leaf bounds, then expands visible sector refs through PVS
  and neighbor adjacency.
- `gasworks_128` re-import produced 32 sectors, 32 proxy LODs, 31 sectors with
  source leaf IDs, and visible refs for every sector.
- Runtime streaming now uses the observer's current imported-world sectors as
  visibility seeds when sector refs exist.
- Full imported-world chunk desire expands through sector
  `visible_sector_refs` and `adjacent_sector_refs`.
- Imported-world chunks inside the old prefetch sphere but outside the visible
  sector set are filtered out unless they also contain terrain/placements.
- Radius/keep/prefetch remains the fallback for worlds without sector
  visibility metadata, and collision residency still follows the near radius.
- `gasworks_128` visual checkpoint passed with no crashes and no visible holes,
  but this map's broad sector PVS still produced `desired=3375`; the remaining
  high-water mark is commit/upload cost rather than visibility selection.
- `gasworks_128` later reproduced the same invalid wgpu bind-group crash even
  after prepare-side geometry staging was disabled. Runtime diagnostic knobs now
  allow sector proxy scheduling to be disabled, or proxy removal to be retained,
  so proxy lifetime can be isolated from full-chunk streaming and renderer
  upload behavior.
- Disabling sector proxies avoided the crash; retaining proxies still crashed
  after the world finished loading, so proxy removal is not the trigger. Sector
  proxy entities now skip terrain renderer metadata and stay out of terrain
  lookup/normal-seam systems while remaining visual voxel fallback objects.
- A retained-proxy run without terrain metadata still crashed while moving the
  camera during heavy startup loading. Sector proxies now also opt out of
  shadows and occlusion culling to isolate camera-driven visibility/shadow work
  from proxy voxel upload.
- The crash was later isolated to a stale feature-owned transparent bind group:
  wgpu reported `Transparent Scene BG0` as invalid after scene/voxel buffer
  recreation. The renderer now labels core scene/voxel bind groups, tracks a
  scene binding revision so the transparency feature refreshes its bind groups
  before rendering if they are behind the current scene buffers, and releases
  retired scene buffers only after the render queue submission associated with
  the retirement has completed.
- A later gasworks stress run still reported the same transparent bind group
  invalid after many full-chunk commits. The transparent overlay now also
  tracks the actual source buffers used to build `Transparent Scene BG0`, so a
  missed buffer pointer change forces a rebuild even if the coarse scene
  binding revision appears current.
- Replaced transparent overlay bind groups are also retired under the same
  queue-submission fence as buffers. This keeps old bind-group handles owned by
  the renderer until command buffers that may reference them have completed.
- `gasworks_128` visual checkpoint then passed while moving immediately during
  startup; sector proxies were replaced by detailed chunks without crashes or
  persistent low-LOD objects.
- Later GPU stress runs exposed the same validation class in `GBuffer Scene
  BG0`. Source-buffer tracking, pass-boundary freshness checks, retired
  bind-group pinning, and temporary lifetime instrumentation were used to
  isolate the issue.
- The final crash cause was an allocator bug, not streaming lifetime itself:
  geometric buffer growth could pick an unaligned final size after the
  requested size had already been aligned. The observed bad `InstancesBuf`
  allocation was `39366` bytes, then a fresh `GBuffer Scene BG0` was created
  from that buffer and failed validation at submit. Managed GPU buffer
  allocation sizes are now aligned after geometric growth as well.
- After the allocator alignment fix, `gasworks_128` ran without the previous
  `Transparent Scene BG0` / `GBuffer Scene BG0` validation crashes.

#### Step 9: Add Clipmap Provider For Open Worlds

For Minecraft-style imports, add clipmap rings:

```text
ring 0: full-res destructible
ring 1: mid-res voxel
ring 2: coarse voxel/mesh proxy
ring 3: far impostor/terrain proxy
```

The clipmap provider should use world-space rings, not small individual chunk
radius alone.

Verification:

- Large open map keeps stable frame time while moving.
- No holes appear when crossing ring boundaries.
- Ring transitions are visually acceptable.

#### Step 10: Compact Payloads And Async Decode

Once the residency model is correct, optimize payload format:

- compact/binary `.gkchunk` payloads
- compressed sector/proxy payloads
- async decode into runtime-ready structures
- optional worker-side `XBrickMap` construction where safe

Do this after the no-hole/sector model, because compact payloads alone do not
solve visible holes or main-thread commit spikes.

Verification:

- Existing JSON chunk files still load.
- New compact payloads load through the same runtime abstraction.
- Import report shows payload sizes and decode times.

### Finalized Implementation Status, 2026-06-08

The current implemented baseline is:

- Imported worlds are schema v2, sector-driven, and should be re-imported if
  old manifests lack sector or LOD metadata.
- Import/bake emits full chunks plus sector `lod1` proxy chunks.
- Runtime visual residency has separate full and proxy paths.
- Runtime collision and destruction residency are separate from visual
  residency.
- Proxies are visual-only and are hidden when their referenced full chunks are
  resident.
- Full chunks are not unloaded until a sector proxy is available when the
  sector has proxy LOD metadata.
- Runtime edits to streamed imported chunks persist into world-delta imported
  chunk overrides. The imported source world remains immutable.
- `actiongame` exposes runtime tuning through environment variables:
  - `GEKKO_STREAMING_RADIUS`
  - `GEKKO_STREAMING_PREFETCH_RADIUS`
  - `GEKKO_STREAMING_KEEP_RADIUS`
  - `GEKKO_STREAMING_COLLISION_RADIUS`
  - `GEKKO_STREAMING_DESTRUCTION_RADIUS`
  - `GEKKO_STREAMING_MAX_COMMITS_PER_FRAME`
  - `GEKKO_STREAMING_MAX_COMMIT_MS`
  - `GEKKO_STREAMING_METRICS_INTERVAL_MS`
- Streaming metrics now report full/proxy/collision/destruction commit and
  residency counts so manual visual checks can be paired with logs.
- `gasworks_128` passed visual checkpoints for:
  - no holes while moving
  - no persistent low-LOD objects after full chunks loaded
  - destruction/edit persistence across chunk unload/reload
  - stable runs using upstream `cogentcore/webgpu` main

Known caveats:

- Full-detail chunk commit/upload spikes can still be high on integrated GPUs.
  The commit budget reduces per-frame churn, but explicit full-detail loading
  remains expensive by design.
- Already-loaded full chunks are not promoted/demoted in place between
  collision/destruction modes; residency transitions happen through unload and
  reload.
- Sector pinning/focused-detail selection in the editor is not implemented yet.
- Clipmap-style open-world handling is deferred.

### Editor Base-World Preview

`gekko-editor` uses its own base-world preview controller rather than the game
streamed runtime.

Current preview modes:

- `hybrid`
  - sector LOD proxies for the whole imported world when available
  - detailed full chunks around the editor camera
  - hides a proxy once its sector's full chunk refs are resident
- `lod`
  - proxy-only overview when sector LODs exist
  - falls back to normal chunk preview for old/no-proxy manifests
- `full`
  - explicitly loads every non-empty full imported-world chunk
  - useful for inspection, expensive for large maps

The Base World panel exposes preview mode and radius controls before a level or
base world is loaded. Choose `lod` before opening heavy maps when startup load
cost matters.

Editor proxy preview entities are locked preview objects, visual-only, shadow
disabled, and occlusion-culling disabled. They should not be used for
voxel-accurate edits; detailed chunks are still required for exact edit
operations.

### Remaining Useful Follow-Ups

These are useful after the sector/proxy baseline:

- prefetch the observer's current chunk synchronously before player spawn
- use larger import chunk sizes for maps where edit granularity permits it
- reduce command flushes inside `commitPreparedStreamedChunk(...)`
- keep extending profiler counters around renderer upload churn
- add editor sector pin/focused-detail controls
- persist editor preview mode across editor restarts if it becomes a regular
  workflow preference

These are refinements on top of the current architecture, not replacements for
the sector/proxy model.

### What Not To Do

Avoid these as long-term answers:

- only increasing view distance
- only increasing chunk size
- hiding holes with fog
- loading every full-resolution chunk at startup
- making all distant chunks destructible
- adding more goroutines without budgeting main-thread commit

They may help a single demo, but they do not solve large imported worlds.

## Eager Spawn vs Streamed Runtime

The distinction matters:

- `SpawnAuthoredLevel(...)`
  - explicit placements
  - expanded placement volumes
  - markers
  - terrain chunks
  - environment
- streamed runtime
  - chunk-local placements
  - terrain chunks
  - imported base-world chunks
  - world-delta backed overrides

If the bug mentions `base_world`, chunk unload/reload, or persistent voxel edits, start in streamed runtime.

## World Deltas

World deltas persist runtime modifications relative to a level.

Main file:

- `.gkworlddelta`

Related data directory:

- `<delta file>_data`

Main top-level fields:

- placement transform overrides
- placement deletions
- terrain chunk overrides
- voxel object overrides

Snapshot payloads are stored separately as `VoxelObjectSnapshotDef`.

Important helpers:

- `DefaultWorldDeltaPath(levelPath)`
- `DefaultWorldDeltaDataDir(deltaPath)`
- `SaveWorldDelta(...)`
- `LoadWorldDelta(...)`
- `SaveVoxelObjectSnapshot(...)`
- `LoadVoxelObjectSnapshot(...)`

## Where Agents Usually Need To Start

- terrain format or bake issue
  - `content/terrain.go`
  - `content/terrain_bake.go`
  - `content/terrain_*`
- imported-world manifest or chunk issue
  - `content/imported_world.go`
  - `imported_world_baker.go`
  - `imported_world_spawn.go`
- streamed loading issue
  - `streamed_level_runtime.go`
  - `mod_chunking.go`
- persistent override issue
  - `content/world_delta.go`
  - `streamed_level_runtime.go`

## Verification

For authored-data validation and bake logic:

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`

For runtime integration:

- `env GOCACHE=/tmp/gekko3d-gocache go test .`

For full guidance, see [`../engine/verification.md`](../engine/verification.md).
