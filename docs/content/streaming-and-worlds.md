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

- Existing `.gkworld` files without sectors still load through the old path.
- New sector manifests load through the sector path.
- Sector grouping does not change world scale or chunk placement.

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

### Tactical Work That Is Still Worth Doing

These are useful before the full sector system:

- budget `commitPreparedStreamedChunksSystem(...)`
- add load/unload hysteresis
- prefetch the observer's current chunk synchronously before player spawn
- use larger import chunk sizes for maps where edit granularity permits it
- reduce command flushes inside `commitPreparedStreamedChunk(...)`
- add profiler counters for streaming commit, geometry registration, and GPU
  upload churn

These are tactical bridges, not the final architecture.

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
