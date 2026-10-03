# Streaming and Worlds

This page documents the terrain, imported-world, world-delta, and streamed-level pieces that sit around authored levels.

Use it when working on:

- terrain baking and terrain chunk spawn
- imported voxel worlds
- streamed level runtime
- persistent world overrides and voxel snapshots

For the top-level level format, see [`levels.md`](levels.md).

For the 15 km island target, use the
[`island-streaming.md`](island-streaming.md) implementation plan. Its global
squad/offline-to-tactical navigation companion is
[`actiongame/docs/island-strategy-navigation.md`](../../../actiongame/docs/island-strategy-navigation.md).

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

Workers prepare terrain registration geometry. Main commits retain live removal
authority, terrain backing and synchronous hooks. See
[terrain asset ownership](../assets/runtime-assets.md#streamed-terrain-registration)
for adoption, pending charges and unload/Stop cleanup.

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
- carries manifest material kind/tags into the runtime voxel palette, allowing
  the same raycast surface-material query used by ordinary authored assets

Spawn helpers live in:

- `imported_world_spawn.go`

## Streamed Level Runtime

The streamed runtime is implemented in `streamed_level_runtime.go`, with observer
demand selection in `streamed_level_selection.go` and renderer ticket ownership
and visibility handoff in `streamed_level_render_residency.go`.

It extends authored levels with:

- chunk observer driven loading
- prepared background chunk loads
- placement chunking
- terrain streaming
- imported base-world streaming
- world-delta override application
- observer-local voxel navigation graph residency
- asynchronous navigation rebuilds after imported-world voxel edits

Important state:

- `StreamedLevelRuntimeState`
- desired, pending, and loaded chunk maps
- terrain entry map
- imported-world entry map
- placement chunk map
- world-delta and override maps
- resident navigation source/graph tiles and atomic navigation revision

Important public entry point:

- `StartStreamedLevelRuntime(...)`

Observer selection is owned by the main thread. Each tick scans live ECS
observers, keyed by entity, world-space chunk coordinate, effective radii, chunk
size, runtime generation, and selection revision. Unchanged keys reuse demand.
Changed cubes update disjoint entering/exiting slabs; teleports visit bounded
footprints. Overlap counts preserve the other observers' demand when one moves
or disappears. Raw prefetch/keep demand and sector demand are unioned across all
observers before imported filtering and full-sector expansion. Collision and
destruction remain raw radius cubes. The existing PVS/adjacency, non-imported
content exceptions, backed empty-entry expansion, and global proxy fallback
policies are preserved.

Call `state.InvalidateObserverSelection()` on the main thread after changing
selection metadata in place: imported membership/PVS/adjacency, placements,
terrain occupancy/overrides, backing availability, or a future layer transform
that changes selection coordinates. Exported maps do not detect such edits.
Runtime terrain override publication/removal invalidates at the mutation;
generation, radii, chunk size, and proxy enablement are checked directly. Start
resets selection; Stop releases it when teardown begins, including late cleanup
errors. Persistence failures before teardown retain resumable state.

Published demand maps are runtime-owned read-only views. Startup gameplay demand
and temporary proxy pins are tracked as working additions and cleared on the
next tick without copying the whole base selection. Reusing selection still
runs render refresh/reconciliation, navigation demand, unloads/upgrades, hint
pruning, and prepare scheduling/retry/admission. Manifest visibility, valid
full-chunk expansion, and fallback derivations are cached until invalidated.

`ObserverSelectionBuildCount` counts changed observer demand evaluations,
including additions/removals; `ObserverSelectionChunkVisitCount` counts actual
coordinates visited while building/updating radius volumes. Both are cumulative
`uint64` metrics reset at Start. Live histories/count memberships are bounded by
active observer footprints and indexed metadata, with no historical input keys.
Empty count/history maps and Stop release their capacity; nonempty Go maps may
retain peak capacity. This is not a selection byte budget or measured frame-time
gain. [S3a is complete](../roadmaps/streamed-rendering-s3a.md); renderer scene
gathering and future layer selection remain separate S3 work.

## Long-Term Streaming Plan

The finite open-world implementation sequence and terrain/`.gkworld` v3 page
contracts are defined in [Island Streaming](island-streaming.md). This section
records the general streaming model and the existing sector/proxy baseline.

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
- Prepared imported-world geometry staging was disabled at that point. Full
  chunks and sector proxies returned to geometry construction during entity
  spawn.
- Atomic `XBrickMap` ID allocation remains because concurrent map construction
  is independently possible and IDs are uploaded into GPU scene data.
- The recommendation then was renderer-owned staging or GPU upload scheduling.

Current path, 2026-10-03: S2a restored immutable worker preparation with defensive
asset copies. P5a moves that independent copy and its bounds calculation onto
workers, then transfers it once through a private handle. Cached source maps
remain separate from renderer mutation; public registration remains defensive.
See [prepared geometry ownership](../assets/runtime-assets.md#streamed-prepared-geometry-lifetime).
Native two-child and 64-child pressure checks passed. The historical
`gasworks_128` scene was not available for re-verification; those checks do not
establish that its crash is resolved. Renderer staging and upload scheduling
remain necessary to bound all work in a large commit.

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
- With a renderer installed, a sector proxy is hidden only after all required
  imported full targets have current renderer-ready tickets. CPU-only runtimes
  retain the original loaded-chunk behavior. See the S1c update below.
- Sector proxy visual residency is now separate from full chunk visual
  residency. Proxy sectors can be kept as a cheap far-world fallback, hidden
  while all full targets for the sector are renderer-ready, and shown before full
  chunks unload so the base world does not disappear at distance.
- Streaming metrics now include loadable desired/keep chunk counts,
  collision-resident chunk counts, desired/keep sector counts, proxy pending
  and prepared queues, and loaded proxy counts. The original `desired` and
  `keep` counters still include empty radius coordinates for compatibility.
- Runtime visibility can consume imported-world sector metadata; radius
  streaming remains the fallback when sector visibility metadata is absent.

Renderer readiness update, S1c:

- Runtime-owned terrain, imported full chunks and sector proxies receive
  generation-qualified tickets and a hidden marker before their first spawn
  flush. Hidden targets remain renderer-resident and upload within global GPU
  content budgets. Authored placement visuals keep their existing owner.
- A ready proxy covers the sector while any required full target is unfinished.
  All required full targets reveal together and the proxy hides in one completed
  ECS flush. A hidden ready proxy stays resident as fallback. Proxy-less terrain
  and imported targets reveal independently after their own tickets settle.
- Distance unloading retains full coverage until the required proxy has a
  matching ready ticket. Missing, failed or stale status is not readiness.
  Marker recovery renews only the affected target; externally removed proxies
  can be prepared again instead of blocking replacement with a stale load entry.
- Required coverage honors imported backing and overrides. A current, explicitly
  empty prepared result can complete coverage without an entity; a missing entity
  or a zero payload count with effective backing cannot supply that proof.
  That proof applies to the resident full cohort. Baked proxies still use their
  source geometry; propagating fine edits to coarse proxies remains D2 in the
  [optimization roadmap](../roadmaps/streamed-rendering-content-optimization.md).
- Streaming owns tickets and retires them after marker removal has flushed.
  Unfinished tickets wait for renderer cancellation; terminal retirement sweeps
  also run while streaming is stopped or has a preparation error.
- Without a `VoxelRtState` resource, CPU-only behavior remains available. Late
  renderer installation stages existing managed targets. Once managed, resource
  disappearance cannot authorize CPU-ready handoff within that world. A new
  world determines managed residency from its installed renderer.
- Collision/destruction components and CPU readiness do not wait for GPU uploads.
  Existing residency upgrades still use unload/reload. Render handoff does not
  modify published navigation data or revisions.

This applies to the existing v2 sector/proxy index, not the future v3 page forest,
root startup gate or cross-layer coverage groups. V2 still requests usable sector
proxies globally. Implementation scope and verification:
[S1c](../roadmaps/streamed-rendering-s1c.md).

Prepared geometry cache update, S2a:

- Imported full chunks and sector proxies reuse immutable worker-prepared maps.
  Registration creates a separate AssetServer copy. This describes the current
  path; the earlier disabled-staging note records a historical investigation.
- `MaxPreparedGeometryCacheBytes` bounds the estimated CPU geometry storage
  owned by this cache. Zero selects 128 MiB; negative disables warm retention.
  `MaxPreparedGeometryCacheEntries` remains a secondary ceiling: zero selects
  256, negative disables warm retention. Both ceilings apply together.
- The charge includes geometry structs, logical map entries, packed-brick
  pointer capacity and auxiliary byte capacity. Shared immutable maps, sectors
  and bricks count once across cache keys. Registered copies count separately.
  Charges are captured at admission. Go map bucket slack, allocator overhead,
  cache bookkeeping and later renderer dirty-map churn are excluded.
- Live full/proxy users pin their assets, including hidden fallbacks and CPU
  collision users. Their bytes may exceed the ceiling; metrics expose total,
  prepared, asset-copy, pinned, maximum and over-budget bytes. Eviction cannot
  invalidate an acquired user. Unreferenced LRU entries leave first.
- Oversized results remain usable without warm retention. Final release drops
  oversized/disabled entries and their assets. Empty keys also have exact asset
  lifetime tracking. Stop joins workers, removes users and closes cache ownership;
  restart creates a fresh cache. Failed persistence preserves live ownership.
- Same-key overlapping builds share one result, even when warm retention is
  disabled or the result is oversized. Failure wakes waiters and allows retry.
  Distinct keys proceed independently. Workers never delete AssetServer assets;
  engine-thread maintenance trims deferred victims even on no-commit frames.
- This is a cache ownership budget, not a process memory ceiling. Decoded
  RuntimeContentLoader data and pending results have separate S2b owners below.
  Editable/collision copies, navigation snapshots and renderer-retained data
  remain later S2 work. Formats, compression and collision algorithms are
  unchanged. Scope and verification:
  [S2a](../roadmaps/streamed-rendering-s2a.md).

Decoded content and pending preparation, S2b:

- `RuntimeContentLoader` uses one LRU byte budget across its eight decoded content
  kinds. `RuntimeContentLoaderOptions.MaxCacheBytes` defaults to 128 MiB when
  zero; negative disables warm retention. Kind plus cleaned absolute path is
  identity; lexical aliases coalesce, symlinks remain distinct. Concurrent loads
  of one identity share decoding, including uncached oversized results.
- Charge is estimated decoded storage at admission: structs, slice capacity,
  nested references, string bytes and logical map entries. Encoded RLE size does
  not substitute for decoded voxel capacity. Allocator/map bucket overhead,
  temporary decode buffers and derived indexes are excluded. Public `Load*`
  pointers remain valid after eviction; their external lifetime and later
  mutation are outside owned-cache accounting.
- `NewScope().Loader()` declares a decoded lease. Close the scope after use;
  repeated loads within one scope share a pin. Active scopes survive eviction
  pressure and `Clear()`. Final release trims warm storage; pinned data can
  exceed the ceiling, exposed by `Stats()`.
- `MaxDecodedContentCacheBytes` config applies to runtime-created loaders.
  Supplied loaders keep their own budget. World level/manifests/backing data
  stay leased until successful Stop. Each preparation leases its decoded data
  through commit/discard; entities retain independent geometry/heightmaps.
  Synchronous collision startup and navigation source baking use transient
  scopes. Failed persistence keeps live world ownership usable.
- `MaxPendingPreparedBytes` defaults to 128 MiB when zero; negative is invalid.
  Full/proxy workers share one payload budget, including blocked publication.
  Rejected payloads release their leases and send a small retry completion;
  required-cost hints delay rebuilding until capacity returns. One oversized
  payload can proceed alone. Existing count/time commit budgets still apply.
- Pending charge is per-result admission cost, including decoded records,
  snapshots, placements and prepared geometry. Shared data may also be charged
  by its loader or geometry-cache owner; do not sum these metrics as physical
  memory. Retry envelopes remain bounded by existing queue/job counts. Active
  decode/build allocations remain outside the pending ceiling and are bounded
  by `MaxPrepareJobs` in count.
- Metrics expose `DecodedContentCache*` and `PendingPrepared*` bytes, budgets,
  pressure and reuse/retry counts. Stop drains and releases every result,
  clears shallow metadata references and clears a runtime-created loader's
  warm ownership. Supplied/shared loader leases survive. Restart creates fresh
  pending credits. Scope, review and verification:
  [S2b](../roadmaps/streamed-rendering-s2b.md).

Preparation dispatch priority, S1d:

One main-thread heap selects full/proxy work from current desired membership.
Fresh order is fallback proxy, collision/destruction full chunk, visible detail,
then prefetch. Current radius and imported current/PVS sector demand identify
visible detail; prefetch-only expansion stays prefetch. Keep-only interest retains
existing content. Stable ties use waiting age, signed X/Y/Z and kind. Every eight
waiting observer updates promotes one priority level, up to fallback priority.
Age survives pending attempts and byte-cost retries; current satisfaction,
withdrawal, Stop or restart releases it. An older cancelled completion cannot
erase renewed demand's age. Known byte-blocked work does not block fitting work.

`PrepareDispatchCount`, `LastPrepareDispatchCoord` and `LastPrepareDispatchKind`
(`full` or `proxy`) report actual dispatch independently of completion timing.
`MaxPrepareJobs` continues to bound active workers and S2b bounds pending payload
bytes. S1e below adds combined admission; remaining stage queues are separate.
[S1d decision](../roadmaps/streamed-rendering-s1b.md#s1d-deterministic-preparation-priority).

Combined streaming admission, S1e:

`MaxStreamingWorkItems` bounds current-world asynchronous full/proxy work from
dispatch through queued CPU results and initial GPU completion. Zero selects 32;
positive values including one are valid; negative is invalid. Worker count and
pending bytes retain their separate limits. One full item covers its terrain and
imported targets. Individually Ready targets can finish an item while a larger
cohort stays hidden. Qualified terminal failure finishes initial work without
proving coverage. CPU-only and proven empty commits finish immediately.

Cancelled preparation holds its item through acknowledgement. Live stale or
cancelled render targets follow their replacement ticket; removed unfinished
targets wait for marker removal and renderer retirement. Partial commit failures
retain already staged work. Synchronous gameplay, late renderer adoption and
failed-source repair remain usable under pressure; their GPU work is accounted
and can exceed the ceiling, preventing further asynchronous dispatch. Ordinary
edits after initial Ready remain with renderer budgets. Placement rendering and
total process memory are outside this allowance.

`StreamingWorkCount`, `StreamingWorkMaxCount`, `StreamingWorkOverBudgetCount` and
`StreamingWorkCarryoverCount` expose current admission and old retirement debt.
`StreamingWorkAdmissionBlockedCount` counts observer updates with otherwise
eligible dispatch blocked by the allowance. Diagnostics do not control admission.
Failed Stop preserves current GPU work. Successful Stop drains CPU work and moves
unfinished retiring GPU work to separately reported carryover; old debt cannot
block a new generation or prove its readiness. A paused renderer backpressures
asynchronous preparation; existing fallback coverage and synchronous collision
behavior remain.
[S1e decision](../roadmaps/streamed-rendering-s1b.md#s1e-combined-streaming-admission).

Prepared commit scheduling, S1f:

The main thread transfers a fixed, nonblocking transport frontier into a bounded
ready owner. Retained results keep decoded scopes, pending bytes and streaming
admission until actual consumption or Stop drain. Public `PreparedChunkQueueDepth`,
`PreparedProxyQueueDepth` and `PreparedQueueDepth` include transport and retained
results; physical channel length no longer reports total deferred work.

Commit order uses live S1d priority, with one promotion per eight waiting commit
updates, then first queued update, signed X/Y/Z and proxy/full kind. Stale,
cancelled, obsolete, duplicate and error results use a cleanup prefix; wanted
byte-cost retries retain demand priority. New captures share one update birth.
Arrivals during commit wait for a later frontier. Count/time budgets and existing
acknowledgement/error policy remain. Failed Stop retains ready ownership;
successful Stop drains it. The opt-in S1g budget below bounds placement admission
inside a chunk; individual large units remain atomic.
[Capacity and channel compatibility](../roadmaps/streamed-rendering-s1b.md#s1f-deterministic-ready-commit-queue).

Resumable placement commits, S1g:

Positive `MaxPlacementCommitUnitsPerFrame` enables a shared placement budget in
managed runtimes. Nonpositive values and CPU-only runtimes stay synchronous.
One placement, snapshots and hooks form one atomic unit; deleted or moved-away
identities also consume a unit. Candidates share rounds with at most one
placement each. Terrain/imported prefixes run once, with time checks between
atomic phases. `MaxChunkCommitsPerFrame` also limits distinct full/proxy candidates
advanced that frame. Cleanup and byte-cost retries do not spend this chunk
allowance; cleanup also runs after it is spent, subject to the time limit. Eligible
proxies and cleanup can proceed after the placement cap. One expensive asset,
snapshot or hook can still exceed the time budget.

Partial placements publish their visuals/colliders and run hooks once. The
bounded ready owner retains the exact envelope, decoded scope, pending charge
and CPU admission until completion or cleanup, even if terrain/imported targets
are already Ready. `LoadedChunks` and chunk counters publish only whole chunks.
`ActiveChunkCommitCount` and `PlacementCommitUnitsLastFrame` expose progress;
entity frame counts report actual admitted entities. Commit timing/breakdowns
aggregate serviced work in the transaction, excluding waiting between frames.

Cancellation and Stop save partial terrain/imported/placement edits before
removal; failed saves pin ownership for retry. Cancelled active-only imported
targets have not established whole-cohort coverage, so durable cleanup does not
wait for proxy admission. Completed loaded owners retain their proxy gate;
unfinished GPU retirement retains its admission debt. Fatal spawn/snapshot or
hook-signalled errors retain partial ownership without retrying units. Existing
hook panic behavior remains. Stop/restart during a hook cuts off publication.
Remaining units use current deletion, transform and snapshot authority; completed
units retain live edits. Synchronous same-coordinate loading finishes the active
transaction without duplicate spawns and may exceed the frame budget.
[S1g ownership decision](../roadmaps/streamed-rendering-s1b.md#s1g-opt-in-resumable-placement-commits).

Obsolete preparation cancellation, S2e:

Full/proxy preparation has one cancellation owner per dispatch. Observer
processing cancels obsolete demand after rebuilding temporary fallback pins;
Stop cancels before persistence barriers. A loaded replacement cancels its
outstanding dispatch before upgrade/unload removes that replacement.
Cancellation is terminal for that
dispatch even if demand returns. Its coordinate remains pending until the
completion is consumed, then renewed demand can start a fresh job. Runtime
generation and dispatch identity protect newer pending ownership.

Workers check between preparation phases and before pending admission. They
release cancelled consumers' scopes and payloads; shared decodes/cache builds
finish normally for other users. Queued results recheck cancellation before
errors or commit and release their existing credits on consumption. Cancellation
does not publish geometry/entities, set `InitErr`, create cost retry hints or
increment prepare errors. `PrepareCancelledCount` reports terminal cancellation.
Synchronous gameplay preparation retains immediate readiness.

Failed Stop retains loaded entities, generation and leases; subsequent observer
processing can re-admit required preparation. This is cooperative cancellation
at phase boundaries, not mid-decode preemption or a worker temporary-memory cap.
Separate IO, generation and navigation queues remain open.
[S2e decision](../roadmaps/streamed-rendering-s2b.md#s2e-obsolete-preparation-cancellation).

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
- Weapon carves edit the renderer-active private chunk map in place. Collision
  observes the map revision without consuming GPU dirty markers, and sub-brick
  edits upload only touched bricks/normal halos rather than whole sectors.
- Streaming logs include `collision_loadable`, `proxy_committed_frame`,
  `destruction_loadable`, `proxy_committed_frame`, `full_committed_frame`,
  `collision_committed_frame`, and matching total counters so visual, proxy,
  collision, and destruction residency can be compared.
- Already-loaded full chunks are not promoted/demoted in place yet; mutating
  renderer-active chunk entities caused stale GPU bind-group validation failures
  during visual testing.
- Imported-world destruction without a voxel backing persists dirty chunks to
  `ImportedWorldChunkOverrides` in the world delta. The imported source world
  remains immutable; runtime loads the saved override chunk before falling back
  to the imported manifest entry.
- Imported worlds with an immutable voxel backing and terrain column chunks use
  `VoxelBackingRemovals` instead. Destruction materializes only touched 8x8x8
  bricks into the live `XBrickMap`, then stores one 512-bit removal mask per
  edited brick. Repeated materialization applies the mask before publishing the
  brick, so unloaded or previously implicit matter is never restored.
- The backing provider contract is source-neutral. HL1 imports emit a compact
  plane-tree `.gkvoxelbacking` containing bounded exact classifiers for the
  world and baked static brush models; terrain columns implement the same
  runtime provider directly. Optional thin-surface support is edit-scoped: it
  activates only from the authored surface or after that band's removal history
  was seeded at the surface. The renderer, collision, raycast, and navigation
  paths continue to consume `XBrickMap` rather than the backing.

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
  proxy entities now skip terrain renderer metadata and voxel adjacency metadata,
  staying out of terrain lookup and cross-chunk normal-seam systems while
  remaining visual voxel fallback objects.
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
- Proxies are visual-only and hide when their required full cohort is
  renderer-ready. CPU-only runtimes use loaded-chunk readiness.
- Full chunks are not distance-unloaded until a sector proxy is renderer-ready
  when the sector has usable proxy LOD metadata.
- Runtime edits to streamed imported chunks persist into full world-delta chunk
  overrides when no backing exists, or sparse `VoxelBackingRemovals` when the
  imported world has immutable backing support. Terrain removals use the same
  sparse contract, preserving caves that cannot be represented as columns.
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
- imported-world chunk overrides
- navigation source-tile overrides
- navigation profile graph-tile overrides
- voxel object overrides
- voxel backing removals

Snapshot payloads are stored separately as `VoxelObjectSnapshotDef`.

Runtime terrain, imported-world and voxel-object edit snapshots use unique
payload names inside `<delta file>_data`, preserving `.gkchunk`/`.gkvoxobj` and
existing reader semantics. Navigation's imported edit analysis uses the same
writer. It writes an owned temporary file, sets `0644`, syncs and closes, renames
onto its owned unique reservation, then syncs and closes the directory before
returning a reference. Imported writers keep their existing serialization mutex.
Previously referenced payloads are never overwritten or removed by a new save.

Asynchronous manifest/navigation captures own all `WorldDeltaDef` slices and
nested backing-removal brick slices. Ordinary manifest requests coalesce as
uncaptured intent and clone the latest state upon admission. Synchronous saves
join publication before saving the latest state. Failed manifest publication
preserves the previous durable manifest and its payloads. Existing navigation
and standalone payload helpers can still stage RAM references before ordinary
manifest publication.

Imported navigation analysis shares capture-order ownership with blocking saves
and backing removals. New captures invalidate older pending/active captures;
stale results publish no payload references or navigation rebuild inputs. Current
items in mixed results retain their work and merge ignored removals only for
their coordinates. Graph-generation retries retain capture ownership. Terminal
completion/failure and worker-barrier discards release outstanding ownership,
preserving successors.
Successors inherit conservative uncommitted edit impact from pending and active
captures until current analysis commits; rejecting older captures loses no impact.

Imported persistence queues its latest owned snapshot for the configured
navigation source world before unload removes the entity, without superseding
newer capture ownership. Backed imports queue latest removal analysis without a
competing full-snapshot override. Blocking helpers copy caller voxel/tag slices;
normal transactions transfer worker-owned arrays at acknowledgement. Coalesced
unknown impact remains conservative so later bounded edits cannot suppress the
required rebuild.

Normal dirty unload and residency upgrades use one asynchronous persistence
transaction. Main-thread capture owns compact brick payloads, metadata and
backing removals. Workers convert and durably write unique payloads. Main-thread
validation then creates a fresh complete candidate manifest; an exclusive worker
publishes it atomically. Only successful acknowledgement publishes normal
transaction references in RAM. Reference and related backing-removal fields must
still match their captured baseline; newer publications survive. A saved checkpoint
becomes the baseline even if the observer returns or newer live edits have no
reference yet, so a later ordinary save cannot restore pre-edit references.

Dirty unload intent pins existing live chunk/entity/geometry ownership while
publication is pending. Busy requests retain uncaptured entity/class intent.
Already observed dirtiness survives upload queue clearing. Exact live payload,
geometry identity and persisted metadata checks reject changed captures; newly
dirty siblings cannot be discarded by another entity's checkpoint. Admission and
retry require current unload or residency-upgrade demand. Keep demand defers
successor captures; an admitted checkpoint still completes. Fresh keep, upgrade
and proxy coverage gate removal. Persistence never clears renderer/physics dirty
queues or the explicit persistence flag.
Disappeared or unowned terrain/imported entities release obsolete capture intent,
matching blocking helper semantics. A missing object whose key remains owned
still saves an explicit empty snapshot, preventing base geometry resurrection.

`MaxPendingPersistenceBytes` defaults to 128 MiB when zero; negative is invalid.
One exclusive owner covers ordinary asynchronous manifest clones and transaction
captures/results/candidate baselines through acknowledgement. Finite sole-owner
oversized work may proceed as visible pressure; unsafe counts retain dirty data
without allocation. Retained navigation result arrays remain charged until
handoff. Worker-local conversion/codec/IO temporaries, navigation caches and
allocator overhead are separate owners; this is not a process-memory ceiling.
Metrics expose `PendingPersistenceCount`, `PendingPersistenceBytes`,
`PendingPersistenceMaxBytes`, `PendingPersistenceOverBudgetBytes`,
`PendingPersistenceAdmissionRetries`, `PendingPersistenceOversizedAdmissions`,
`DirtyPinnedChunkCount`, `PersistenceFailureCount` and `PersistenceLastError`.
Private accounting remains authoritative.

Normal transaction failures retain prior references and dirty intent for retry;
they report persistence metrics without setting a self-blocking runtime error.
Other runtime/navigation errors keep their existing behavior. Blocking unload
helpers and Stop join publication and retain immediate durable save/reload
contracts. Stop saves latest edits before teardown; failure preserves the running
generation, entities and leases. Successful Stop drains acknowledgements before
releasing ownership. Old successful payloads, successful unpublished attempts
and post-rename sync failures can leave unreferenced files. Garbage collection has
separate reference ownership. [Persistence decision](../roadmaps/streamed-rendering-s4.md).

`VoxelBackingRemovals` are inline removal-only deltas relative to a provider's
`source_hash`. Each record identifies the generic owner kind/id and chunk, then
stores 16 `uint32` words per edited 8x8x8 brick. Additive edits still require an
explicit snapshot; this backing contract intentionally covers digging and
tunnelling through immutable base matter.

Navigation delta files live below `<delta file>_data/nav_graph`. Dirty imported
world chunks expand through profile and generator halo dependencies. While its
background build runs, a removal keeps the last valid graph unchanged; grounded
NPC movement continues to validate collision, landing support, and step height
against the live voxel world. An addition installs a generation-tagged blocker
over the edited world bounds. Ground, wall, and detail removals therefore do not
pause bots or invalidate paths speculatively. Before queueing a removal, the
runtime compares the removed source solids with effective live occupancy
(explicit voxels plus immutable backing minus removals) for every agent profile.
A removal queues navigation work only when a formerly accepted span loses the
grounded motor's radius-and-step support footprint, or when locally re-evaluated
capsule clearance produces a walkable bridge between graph spans that are not
already mutually reachable. Boundary erosion that only expands an existing
reachable area keeps the old graph, even if the new fringe touches it on
multiple sides.
Removing one body voxel is not itself sufficient. This is cumulative: repeated
small holes retain their exact removed cells, but each new edit evaluates only
the connected damage component it touches. They keep using the old graph while
physically supported, then queue one exact rebuild when the combined live edit
changes traversal.
Newer edits coalesce
behind a 100 ms quiet window, with a 250 ms maximum wait so continuous
destruction cannot starve publication, and make older results ineligible to
publish. The rebuilt graph's existing profile-bounded gap-jump generation keeps
crossable holes connected; wider holes become real topology changes. Source and
neighboring graph overrides use immutable generation-qualified paths and publish
together only after temp-write, sync, close, rename, and reciprocal seam
validation; explicit empty overrides suppress stale static tiles. If rebuilt
resident spans and graph topology are unchanged, runtime keeps the existing
query and retires the covered edit overlays without a residency reload.
Otherwise it keeps the old resident graph plus those local overlays until the
complete replacement set loads, then swaps the immutable query, retires only
covered overlays, and increments `NavigationRevision` once. Voxel edits that
change no voxel value do not enqueue a navigation revision.
Each immutable query snapshot also carries a topology epoch per resident tile.
Routes record the sorted tile/epoch dependencies selected by their region and
span corridor. A later publication retains routes whose dependency epochs still
match, while an unloaded or changed dependency stops locomotion immediately and
requests a prioritized replan. Asynchronous results are accepted only when the
actor request still matches and the result's dependency epochs remain current.
Profile-bounded, voxel-validated automatic drop and jump links are regenerated
in that same background delta build; moving carrier position only affects
traversal execution and does not dirty graph tiles.

`RuntimeNavigationService` snapshots resident graph data for `FindRoute` and
`ProjectPoint`. Point projection returns the nearest supported span and region;
runtime targeting does not reconstruct polygon IDs or filled navigation
surfaces. Residency loads also prepare the immutable route-query indexes on the
background loader; all requests for that navigation revision reuse them.

Runtime overlays for blockers, doors, and disabled traversals build one complete
immutable query snapshot at a time in the background. A successful build
publishes its query, captured restrictions, residency, and revision together when
its runtime generation and topology are still current, even if newer overlay
intent arrived during the build. Desired overlay maps remain independent from
the published maps, so the next build captures the latest intent. This lets
residency and overlay publications progress while overlay requests continue to
change.
Published snapshots can lag newer intent until a later build completes; physical
movement continues to check live collision. Previously returned service values
retain their captured query and restrictions.

Results from an obsolete runtime generation, residency load generation, or
resident graph revision cannot replace the published snapshot. A failed build
whose overlay request has been superseded is ignored and the latest desired
overlay is retried; a failure for the current request sets the runtime's
initialization error. A replacement residency publication retires only edit
blockers whose generation is covered by that load, leaving newer edit blockers
queued. Query indexes remain background work throughout this process.

`FindRoute` accepts endpoint support only within one navigation voxel
vertically, with a small floating-point boundary epsilon. A route start may
additionally project within one voxel in 3D to absorb motor drift; an exact
destination never binds to another stacked floor.
Each returned waypoint has a same-index `WaypointSpans` entry. Route followers
must treat these as checkpoint identities, not as every span crossed by a
string-pulled route. `RuntimeNavigationService.IsRouteSupport` validates live
motor support against the swept active steering segment and its
capsule-scale recent tail. Final arrival still requires the exact goal span,
which prevents horizontal proximity from binding a stacked surface.

`StreamedLevelRuntimeConfig.NavigationManifestPath` may override the level's
`navigation.manifest_path` for development runs. Actiongame exposes this as
`GEKKO_NAV_GRAPH`; authored levels should keep the path in the level document.

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
