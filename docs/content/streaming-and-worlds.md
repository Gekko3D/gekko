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

Terrain geometry conversion uses the
[uniform column builder](../renderer/editing.md#fresh-uniform-column-construction)
to fill brick-sized runs directly. Full column coverage, source records,
ordered revisions and normal dirty coverage remain unchanged. This reduces
construction work without changing collision, backing/removal or streaming
publication authority. Distant surface-band geometry is separate policy work.

Workers prepare terrain registration geometry. Main commits retain live removal
authority, terrain backing and synchronous hooks. See
[terrain asset ownership](../assets/runtime-assets.md#streamed-terrain-registration)
for adoption, pending charges and unload/Stop cleanup.

### Tiled terrain source content (v3)

The approved [island terrain split](island-streaming.md#terrain-payloads) starts
with source content. Explicit schema-3 `.gkterrainmanifest` entries reference
`height_u16_binary_v1` `.gkchunk` tiles. Legacy defaults and column manifests
remain schema 2. The current voxel-terrain runtime rejects schema 3 until resident
height collision and edit-patch ownership are implemented; content tooling can
save and load it. Render-page hierarchy, collision and excavation handoffs follow
this source-content step.

`TerrainHeightTileDef` stores up to 128 by 128 row-major `uint16` samples. A
sample center is `WorldOrigin + (local + 0.5) * SampleSpacing` in X/Z; its world
height is `HeightOffset + sample / 65535 * HeightScale`. An omitted surface mask
means every sample is valid. Otherwise one row-major bit means present natural
ground, and unused tail bits are zero. These validity bits reserve the content
representation for cutouts; this step does not activate POI replacements.

The binary frame is `GKHTIL1\n`, a little-endian uint32 metadata length, JSON
metadata, then little-endian heights, optional validity bytes and optional
outdoor-navigation exclusion bytes. Metadata is bounded to 64 KiB; dimensions,
exact body lengths and SHA-256 are checked before a tile is returned. Payload
hash and size describe the body, excluding frame and metadata. Manifest-entry
loading verifies the complete decoded identity and lattice against the reference.
Optional navigation data uses the island contract: a 128 by 128 tile at 2 m,
1 m navigation cells and exactly 8,192 exclusion bytes. No navigation policy is
inferred when the mask is omitted.

`BakeTerrainHeightTiles` resamples immutable legacy heightfields onto a signed
global tile lattice. Options default to 128 samples, 2 m spacing and a 4,096-tile
allocation limit; callers may set that limit explicitly. Entries are ordered
by Z then X and use manifest-relative paths. Bilinear sampling retains legacy
float32 arithmetic; encoding rounds `(height / HeightScale) * 65535` to the
nearest uint16. Centers outside the half-open source extent have zero height and
an invalid bit. Every geometrically intersecting tile is emitted, including tiles
whose centers all fall outside a small source extent. Source hashes retain the
legacy source identity; entry dimensions, spacing and payload hashes identify
the tiled representation.

### Resident height query foundation

`TerrainHeightField` owns decoded source tiles independently from voxel geometry.
Construction copies validated v3 manifest references. `LoadTile` resolves a
manifest-relative file; `PublishTile` validates the complete reference and copies
height, validity and navigation arrays into private storage. Producers must not
mutate input during publication. Failed publication preserves existing residency
and generation. `MaxResidentTiles` defaults to 256; replacement is allowed at
capacity, while new tiles require explicit removal of another resident tile.

`SampleGroundXZ` interpolates physical heights on the global cell-centered
lattice and returns an analytic bilinear normal. Signed floor division resolves
neighbors across tile seams. Only corners contributing to height or its gradient
are dependencies: at an exact sample center, the forward X/Z neighbors are needed
for the normal, but the diagonal is not. Required masked corners never supply
interpolated natural ground. No exterior extrapolation or missing-neighbor
clamping occurs.

Nonfinite coordinates or normalized coordinates with magnitude at least `2^52`
return invalid before the float64 half-cell offset can lose precision. Platform
integer and stencil bounds are checked separately before indexing.

Queries distinguish invalid input, outside declared coverage, declared but
nonresident data, masked ground and present ground. Required-corner failures use
outside, then nonresident, then masked precedence. Non-present results have zero
height/normal. `ProbeGroundXZ` retains that status while checking an inclusive
vertical interval, so missing data cannot appear as a ready collision miss.

Publication/removal and queries synchronize through the owner; each query sees
one generation. Successful publication and resident removal advance generation.
Immutable snapshots retain their original resident pointer map and generation
across later replacement/removal. Snapshot holders own those retained lifetimes;
the residency limit bounds the current owner, not caller-retained snapshots.

This CPU query foundation does not install a physics or character-controller
provider and does not enable v3 levels. Live height collision still requires
readiness holds, terrain/POI replacement and edit-patch ownership handoffs.

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

### Streaming page contracts and legacy normalization

`StreamPageDef` and `StreamPagePayloadDef` share manifest-local page and leaf
indices. `ValidateStreamPageForest` checks supported payload metadata, finite
bounds, strict level descent, containment, unique parents/leaf owners and
reachability from distinct roots. It returns parent and leaf-owner indexes,
using `-1` for roots and unowned empty leaves; failures return no partial index.
Page bounds have positive volume. Caller-qualified payload/leaf bounds may be
flat, including constant-height surfaces. The owning compiler or decoder supplies
that coverage; the validator performs no payload I/O and cannot infer height Y
coverage from sample spacing. Cross-layer coverage-group readiness remains a
later level/runtime responsibility.

`LoadImportedWorld` accepts schema 1, schema 2 and missing-version legacy files,
normalizes them to the current v2 defaults and computes a nonserialized,
read-only `PageIndex`. `NormalizeImportedWorldPages` offers the same metadata
conversion without mutating its input. Each sector becomes one flat root/leaf
page; full chunk, PVS and adjacency references become array indices, preserving
source order. Existing first-LOD eligibility and proxy placement are preserved.
Optional legacy hashes, byte sizes and grids stay optional; missing proxy grids
never acquire fabricated coverage. Page bounds union known chunk/proxy coverage,
while indexed sectors retain authored bounds. Entirely zero legacy sector
bounds retain their existing unspecified-metadata meaning; page coverage then
starts from the first referenced chunk, preserving signed placement without
extending coverage to the origin. Other collapsed/inverted or nonfinite bounds,
invalid ownership/references and unsafe grid arithmetic fail before publication.

This compatibility index owns its slices/maps and always has `LegacyDistance`
set. Proxy-less sectors retain distance/PVS behavior; these pages do not promise
v3 fallback coverage. Cached callers must treat the loaded definition and index
as read-only, or recompute the index after authoring mutations. The decoded cache
accounts for the derived storage. Writers remain schema 2 and `.gkchunk` remains
schema 1. Imported-world v3 admission, page baking and live page selection are
still pending [I06 and later island slices](island-streaming.md#commit-sized-delivery-slices).

### Compiled imported chunks

Select `ImportedWorldChunkPayloadBrickZstdBinaryV1` in
`ImportedWorldChunkSaveOptions` to write an independent checksummed frame.
Default saving remains JSON. Existing manifests accept the explicit new kind
without a schema change. Loading restores ordinary voxel records in global
x-fast order, retaining raw palette and material bytes and source lattice.
Runtime prepared geometry, collision and navigation use their existing paths;
this adapter does not yet decode directly into resident compact bricks.

`SaveImportedWorldChunkCompiledWithCodec` and
`LoadImportedWorldChunkWithCodec` accept a caller-owned codec; nil uses the
bounded dictionary-free default. A dictionary frame requires the matching
explicit profile. The runtime loader retains its existing cache and lease
ownership; configure `RuntimeContentLoaderOptions.ImportedWorldCodec` and inject
the loader into streamed runtime. See [decoded ownership](../assets/runtime-assets.md#decoded-content-lifetime).
For embedded fitted normals, compute
`ImportedWorldChunkCompiledGeometryIdentity`, bake against that source, and call
`SaveImportedWorldChunkCompiledWithAux`. Existing compiled saves retain decoded
`EmbeddedAux`; explicit nil removes it. Embedded records take priority in public
conversion and full/proxy preparation. Chunks without them keep aux sidecar fallback.
Common imported emission supports `ImportedWorldSaveOptions.EmbedNormals` with
the explicit compiled kind and an optional borrowed `ChunkCodec`. Full normals
reuse unchanged neighborhoods; proxy normals reuse unchanged local geometry.
The [compiler contract](compiled-voxels.md#imported-compiler-emission) defines
invalidation, counters and generated-level sidecar checks.
See [compiled frames](compiled-voxels.md) for identity, limits, range reads and
dictionary compatibility. Legacy JSON/RLE/aux acceptance remains unchanged.

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

Runtime and offline normal-bake conversion share the
[fresh ordered voxel builder](../renderer/editing.md#fresh-ordered-voxel-construction).
Both skip records whose source `Value` is zero and use a nonzero `MaterialValue`
override, preserving decoded record order. Runtime conversion attaches copied
embedded auxiliary packets after geometry construction. Offline conversion does
not adopt those packets and clears initial dirtiness for nonnil chunks; its nil
input retains the fresh empty-map state.

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

Each chunk job selects voxel-object override references in one pass over world
keys using its placement IDs. Selection preserves the original full prefix
rule, including embedded NUL IDs and nonempty item suffixes. No placements
means no world-key scan. `VoxelOverrideSelectionKeyVisitsLastJob` reports keys
visited by the latest chunk job build; each build resets it, idle frames do not.
Snapshot loading/errors and current commit authority remain unchanged.

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
   - Imported-world chunks support readable JSON, both existing dense RLE kinds
     and opt-in lossless `brick_zstd_binary_v1`; runtime loading auto-detects them.

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

- Schema v1 and missing-version `.gkworld` files normalize through the v2 compatibility path.
- Sector manifests load through the sector path.
- Sector grouping does not change world scale or chunk placement.

Implementation note, 2026-06-08:

- `.gkworld` schema v2 adds `sectors`.
- Chunk `entries` remain the file catalog.
- Sector `full_chunk_refs` are now the runtime-facing imported-world grouping.
- The streamed runtime indexes imported-world chunks through sectors before
  falling back to chunk preparation/commit.
- I05 restores schema v1/missing-version compatibility through load-time v2
  normalization; the writer continues to emit v2.

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
- `CompactPreparedGeometry` opts into compact private imported prepared sources,
  trading worker reconstruction work for retained memory. False preserves dense
  preparation. Public assets and live maps remain dense; see
  [compact source ownership](../assets/runtime-assets.md#compact-private-prepared-sources).
- `MaxPreparedGeometryCacheBytes` bounds the estimated CPU geometry storage
  owned by this cache. Zero selects 128 MiB; negative disables warm retention.
  `MaxPreparedGeometryCacheEntries` remains a secondary ceiling: zero selects
  256, negative disables warm retention. Both ceilings apply together.
- The charge includes geometry structs, logical map entries, packed-brick
  pointer capacity and auxiliary byte capacity. Shared immutable maps, sectors
  and bricks count once across cache keys. Registered copies count separately.
  Charges are captured at admission. Go map bucket slack, allocator overhead,
  cache bookkeeping and later renderer dirty-map churn are excluded.
  Eligible full/proxy workers carry finished registration storage descriptions
  into the cache; pending admission charges their retained metadata separately.
  `PreparedGeometryCacheStorageCaptureVisits` reports cache node description work.
  See [prepared geometry ownership](../assets/runtime-assets.md#streamed-prepared-geometry-lifetime).
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
Workers also prepare [snapshot registration copies](../assets/runtime-assets.md#streamed-voxel-object-snapshot-registration);
adoption preserves these authority and atomic-publication rules.

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
- Built-in plane-tree and terrain-column shell materialization uses synchronous
  [ordered edit streams](../renderer/editing.md#ordered-edit-streams). Material
  hints, shell-offset order, removal history and surface-support activation
  retain their existing timing. Arbitrary public backing providers retain
  sequential writes, so classifier callbacks observe finalized material state
  and can reenter editing. No cross-frame publication is introduced.

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

Snapshot payloads are stored separately as `VoxelObjectSnapshotDef`. Opt-in
schema-2 full/base-delta and schema-3 hybrid payloads are defined by the
[voxel-object override contract](compiled-voxels.md#ordinary-voxel-object-override-payloads).
Runtime loading supports schemas 2 and 3 for individual authored `voxel_shape`
parts. Eligible managed owners save schema-2 deltas by default; hybrid writing
is explicit opt-in. Other origins retain legacy full snapshots.

### Ordinary object override loading

Workers decode v2 full/base-delta and v3 hybrid payloads at the existing `SnapshotPath`, bind
explicit placement/item IDs to the selected owner, and validate the effective
lattice and `gekko-voxel-shape-v1` rasterization version. Delta bases use one
canonical input boundary. Legacy JSON uses the owning RuntimeContentLoader's
immutable authored definition and existing shape conversion/`ModelScale`
resampling. An exact lowercase `.gkassetc` path explicitly selects a compiled
header; corrupt compiled input never falls back to JSON. Other suffixes and the
public JSON loading APIs keep their existing contracts.

Compiled bases verify the selected part, referenced frame identity and sizes,
effective lattice and original base identity. The default E2 logical limits
apply before dense construction. Canonical bricks already contain post-scale
geometry, so construction does not resample them. A short independent loader
scope releases only its own pins on success or failure; a closed originating
scope cannot start or complete verification. Previously decoded valid frames
remain usable after a borrowed codec closes. Returned geometry is independently
owned. Full replacement payloads need header metadata and lattice only, without
loading an unused base frame. Mutable live geometry caches are never canonical
bases; no additional base cache is introduced.

Bound payloads reject missing/non-shape parts and authored collapse requests. Legacy v1
keeps ordered, unbound snapshot acceptance and existing origins, including
collapsed per-item limitations. Resolved snapshots use independent P5c worker
registrations and existing pending byte admission, cancellation and asset leases;
no additional resident base cache is introduced.

Resumable placement commits resolve current actual-item overrides before adopting
any override or invoking hooks, including replacement at the same path. Exact
resolved snapshot matches permit prepared adoption; stale packets use the existing
independent fallback. Explicit owner IDs keep embedded-NUL placement IDs separate.
Prepared v2/v3 or a current actual-item bound payload also activates validation of
current sibling references. Pure legacy placements retain direct item lookups.
An otherwise unused override added to a legacy placement remains ignored when
no bound validation activates. Bound sibling validation scans current override metadata;
this does not provide a hard time bound for a placement unit.

Fatal partial transactions keep their existing persistence pins until successful
Stop/cancellation cleanup. Loaded geometry currently retains dense snapshot
ownership. Explicit managed enable of actual placement items adopts its private
override into the same streaming geometry lease, including full-fallback origins;
unload and successful Stop release it independently of current authored refs.
[Authored-owner qualification](../renderer/editing.md#authored-shape-base-provenance)
is captured once and queried without geometry reconstruction.

At explicit managed Enable, the current selected schema-2 `base_delta` or
schema-3 `hybrid_delta` can restore original canonical tracking, regardless of
the hybrid writer flag. Proof requires the exact existing snapshot
lease and an unmanaged source, actual owner membership, selected authored path
and matching effective lattice. Enable decodes the current file, resolves it
against a fresh loader-owned canonical shape, and independently compares the
isolated live primary geometry with that result. The current payload is
authoritative, including a matching replacement at the same path. Current Aux,
revisions and pivot headers survive; the old leased asset is deleted only after
the new owner has copied current data.

Any failed proof preserves successful ordinary managed Enable with an unbound
construction base and full persistence fallback. Current v1/full payloads,
foreign or already-managed sources and primary geometry drift cannot restore
canonical history. Repeating Enable or correcting an initially wrong lattice
does not requalify that owner. Proof runs once at Enable, including synchronous
placement hooks; save and acknowledgement do not rebuild the base.

### Ordinary object override persistence

Explicitly enabled, sealed authored shapes with a verified
[authored owner](../renderer/editing.md#authored-shape-base-provenance) save
construction-relative final assignments as schema-2 `base_delta` payloads by default.
Captures include explicit owner IDs, canonical base identity and lattice; zero
assignments remove cells and reverted cells are omitted. An empty delta preserves
the base, while complete removal never resurrects it. Placement and item IDs are
kept explicit through asynchronous intent, capture and manifest publication,
including embedded NULs.

`StreamedLevelRuntimeConfig.EnableHybridVoxelObjectDeltas` enables schema-3
selection for these eligible owners. Zero configuration retains schema 2. Each
replacement must use fewer voxel records than its assignments, then save logical
C1 bytes including canonical selector metadata. Total document savings must be
strict; ties or insufficient metadata savings retain assignments/schema 2.
This policy bounds capture memory growth; it does not promise smaller compressed
files or faster processing for every history.

Planning reads maintained changed-brick counts and only potentially profitable
target payloads. Count/lower-bound filters avoid target scans when replacement
cannot qualify; zero-selection plans avoid metadata temporaries. Selected full
geometry and explicit empty clears remain relative to the original authored base,
with no delta chain. Typed content metadata sizing owns JSON escaping and scalar
costs; integer selector lengths are counted without allocating a selector list.

Preflight reads the tracked count and visits only changed assignments to check
portable coordinates. Admission charges chosen voxel records, replacement selector
backing arrays and binding strings before capture. Capture reruns deterministic selection after
admission, visits selected changed bricks and filters unselected assignments
without scanning each unselected target. Delta capture and workers do not scan
untouched geometry, copy base bricks or reconstruct a dense map. Existing soft byte admission,
sole oversized transactions, dirty pins, cancellation and durable publication
remain authoritative. Blocking unload/Stop uses the same payload selection.

V1 full fallback covers exposed, unbound, resealed or unsupported origins and
assignments outside signed int32 or valid payload metadata. Sparse selection
conservatively requires assignment count no greater than the default codec's
brick and voxel limits. Enable also records canonical base brick/voxel counts
and decoded size during its existing validation. Admission checks merged brick
and voxel limits through exact current counts maintained by the managed owner;
paint and removal at the brick limit no longer imply overflow. The original
base proof and conservative bound of at most 592 decoded bytes of growth per
assignment remain required. This covers new bricks and uniform-to-mixed paint
without an unload-time base scan. Unknown proof or exceeded bounds uses legacy
full capture. Larger histories or decoded-size estimates may still fall back
even when C1 could fit them with a tighter calculation. Codec adapter temporaries
remain outside the retained S4 byte charge. Terrain, imported worlds, backing removals, placement
changes and navigation retain their existing representations.

Exact capture freshness checks managed authority, generation, provenance and
owner/lattice metadata before clearing pins or removing entities. Durable
baseline applicability is separate: same-owner edits or exposure after manifest
launch can retain saved A as the baseline while unsaved B remains pinned for
recapture. Changed owner/path/lattice suppresses an incompatible runtime reference
acknowledgement. Failures preserve existing references and retry ownership.

Loaded delta geometry uses independent dense registration until explicit Enable
restores tracking under the loading contract above. Further edits and reverts
remain relative to the original canonical base across eviction/reload. Cheaper
full compact selection remains a follow-up; no delta chain or persistent base
cache is introduced.

### Payload and manifest publication

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
