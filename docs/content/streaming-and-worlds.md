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
