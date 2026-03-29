# Levels

This document explains how authored levels are represented, validated, and spawned in `gekko`.

Levels are the top-level world-assembly format. They do not embed detailed object geometry directly. Instead they reference authored assets, terrain sources, and optional imported base worlds.

## Main Role of a Level

A level describes:

- world-scale defaults such as `chunk_size` and `voxel_resolution`
- explicit asset placements
- procedural placement volumes
- optional terrain
- optional imported base-world data
- environment preset selection
- gameplay markers such as player spawns or objectives

Use a level when you want to assemble a playable space from reusable authored assets and world data.

## Main File Type

### `.gklevel`

This is the authored level document loaded by `content.LoadLevel(...)`.

The top-level `LevelDef` contains:

- `id`
- `schema_version`
- `name`
- `tags`
- `chunk_size`
- `voxel_resolution`
- `terrain`
- `base_world`
- `placements`
- `placement_volumes`
- `environment`
- `markers`

Schema version is currently `1`.

## Core Sections

### Placements

`placements[]` is the explicit placement list.

Each placement contains:

- `id`
- `asset_path`
- `transform`
- `placement_mode`
- optional tags

Current placement modes:

- `surface_snap`
- `plane_snap`
- `free_3d`

Each placement spawns one authored asset from the referenced `.gkasset`.

### Placement Volumes

`placement_volumes[]` is the procedural placement layer.

Each volume defines:

- `id`
- `kind`
  - `sphere` or `box`
- exactly one of:
  - `asset_path`
  - `asset_set_path`
- `transform`
- volume shape parameters
  - `radius` for spheres
  - `extents` for boxes
- `rule`
  - `count` or `density`
- `random_seed`
- optional tags

Placement volumes are expanded into concrete placement instances before spawn. The expansion is deterministic for a given volume definition and seed.

### Terrain

`terrain` points to a baked or authorable terrain source.

Current level validation expects:

- `kind == "heightfield"`
- `source_path` pointing to a `.gkterrain`

During basic authored-level spawn, terrain chunk manifests are loaded and chunk entities are spawned under the level root.

### Base World

`base_world` points to imported voxel-world data.

Current validation expects:

- `kind == "imported_voxel_world"`
- `manifest_path` pointing to a `.gkworld`

Important runtime distinction:

- `SpawnAuthoredLevel(...)`
  - validates `base_world` but does not directly spawn imported base-world chunks
- streamed runtime
  - loads and streams imported base-world chunks from the referenced manifest

So `base_world` is part of the authored level contract, but it is mainly consumed by the streamed-level runtime path.

### Environment

`environment` currently selects a preset-driven lighting and sky setup.

The field is intentionally small:

- `preset`
- optional tags

`applyLevelEnvironment(...)` converts the preset into ambient light, directional light, sky ambient, sun configuration, and skybox layers.

Known presets in current tests include:

- `orbit`
- `daylight`
- `fullmoonNight`
- `fullmoonnight_gi`

### Markers

`markers[]` are authored gameplay anchors in level space.

Each marker contains:

- `id`
- `name`
- `kind`
- `transform`
- optional tags

Markers are useful for:

- player spawn positions
- AI spawns
- patrol points
- objectives
- extraction points

The level code exposes helpers for finding markers by kind, and spawned marker entities retain authored metadata.

## Path Resolution Rules

Level references are document-relative by default through `content.ResolveDocumentPath(...)`.

That applies to:

- `placements[].asset_path`
- `placement_volumes[].asset_path`
- `placement_volumes[].asset_set_path`
- `terrain.source_path`
- `base_world.manifest_path`

Prefer keeping paths relative to the `.gklevel` file so levels remain portable across tools and modules.

## Validation Rules

`content.ValidateLevel(...)` currently checks:

- level name is present
- placement IDs are unique
- placement `asset_path` values exist
- placement modes are supported
- placement-volume IDs are unique
- placement volumes define exactly one source
- placement-volume shape and rule parameters are valid
- referenced asset sets load and validate
- terrain kind, extension, file existence, and chunk-size or voxel-size compatibility
- base-world kind, extension, file existence, and chunk-size or voxel-size compatibility

There is also shooter-specific validation:

- shooter-tagged levels require an imported base world
- shooter levels require a `player_spawn` marker
- shooter markers are checked against imported base-world bounds and solid voxels

## Runtime Paths

There are two main ways levels are consumed.

### 1. Direct authored-level spawn

`LoadAndSpawnAuthoredLevel(...)` and `SpawnAuthoredLevel(...)`:

- load and validate the level
- create one level root entity
- spawn explicit placements as authored assets
- expand placement volumes and spawn the resulting authored assets
- spawn marker entities
- spawn terrain chunks when terrain is configured
- apply the environment preset

This is the simplest whole-level runtime path.

### 2. Streamed level runtime

`StartStreamedLevelRuntime(...)` adds chunk-based loading on top of the same authored level data.

It uses the level to drive:

- chunk-local placement spawning
- terrain chunk streaming
- imported base-world chunk streaming
- world-delta and override application
- optional automatic player spawning at markers

Use this path when the level is large enough that full eager spawning is the wrong model.

## Spawn Metadata Added at Runtime

Spawned level content carries authored provenance components so gameplay and tools can recover where entities came from.

Important ones include:

- `AuthoredLevelRootComponent`
  - root entity for the spawned level
- `AuthoredLevelPlacementRefComponent`
  - identifies the placement or expanded volume instance that produced an asset root
- `AuthoredLevelItemRefComponent`
  - identifies the level placement and authored item for a spawned child entity
- `AuthoredLevelMarkerRefComponent`
  - identifies spawned level markers
- `AuthoredTerrainChunkRefComponent`
  - identifies spawned terrain chunks
- `AuthoredImportedWorldChunkRefComponent`
  - identifies streamed imported base-world chunks

## Relationship to Other Content Types

- Levels reference reusable authored assets documented in [`game-assets.md`](game-assets.md).
- Placement volumes can reference weighted asset sets from the same asset system.
- Terrain and imported worlds are separate authored data formats that levels assemble into one runtime world.

## Recommended Authoring Conventions

- Keep `chunk_size` and `voxel_resolution` aligned with any referenced terrain or imported base world.
- Use explicit placements for intentional authored objects and placement volumes for bulk scatter.
- Keep marker kinds consistent across gameplay code and authoring tools.
- Store asset, terrain, and world references relative to the level document.
- Do not assume `base_world` appears in the simple eager spawn path; if you need imported chunk streaming, use streamed runtime.

## Minimal Example

```json
{
  "id": "station-level",
  "schema_version": 1,
  "name": "Station",
  "chunk_size": 32,
  "voxel_resolution": 1,
  "placements": [
    {
      "id": "hangar-crate",
      "asset_path": "../assets/crate.gkasset",
      "placement_mode": "plane_snap",
      "transform": {
        "position": [4, 0, 12],
        "rotation": [0, 0, 0, 1],
        "scale": [1, 1, 1]
      }
    }
  ],
  "placement_volumes": [
    {
      "id": "rocks-near-gate",
      "kind": "sphere",
      "asset_set_path": "../assets/rocks.gkset",
      "transform": {
        "position": [32, 4, -8],
        "rotation": [0, 0, 0, 1],
        "scale": [1, 1, 1]
      },
      "radius": 12,
      "rule": {
        "mode": "count",
        "count": 24
      },
      "random_seed": 7
    }
  ],
  "environment": {
    "preset": "daylight"
  },
  "markers": [
    {
      "id": "player-start",
      "name": "player_start",
      "kind": "player_spawn",
      "transform": {
        "position": [0, 2, 0],
        "rotation": [0, 0, 0, 1],
        "scale": [1, 1, 1]
      }
    }
  ]
}
```

This level spawns one explicit asset placement, one deterministic scatter volume driven by an asset set, a preset environment, and one gameplay marker.
