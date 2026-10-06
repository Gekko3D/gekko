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
- optional voxel navigation graph manifest
- optional player controller defaults
- environment preset selection
- authored lights and water bodies
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
- optional `streaming_bounds`
- `terrain`
- `base_world`
- `navigation`
- `player`
- `placements`
- `placement_volumes`
- `environment`
- `lights`
- `water_bodies`
- `npcs`
- `markers`

Schema version is currently `3`.

`streaming_bounds` declares padded streaming coverage in world meters, using
finite `bounds_min` and `bounds_max` arrays with positive extent on every axis.
It includes source tiles and coarser fallback pages, rather than only playable
land. Omission preserves legacy levels without a containment guarantee. Explicit
bounds reject out-of-range or unqualified layer coverage. See
[independent layer indexes](streaming-and-worlds.md#independent-level-layer-indexes).

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

When `manifest_path` references terrain v3, that manifest is authoritative and
`source_path` is optional authoring provenance; validation never reads it or
falls back to it after a manifest error. Referenced height payloads are qualified
against their metadata. If either terrain or imported-world manifest is v3,
each referenced layer owns its chunk size and resolution, independently of the
level defaults. All-legacy assemblies retain the existing equality checks.

Source-only legacy terrain without a tiled manifest cannot establish padded
coverage for explicit `streaming_bounds`; bake a manifest first.

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

An imported-world manifest may also reference an optional immutable
`.gkvoxelbacking` classifier. Backing-only chunks contain no eager volume;
destruction materializes touched bricks into their normal `XBrickMap` and saves
sparse removal masks. The same runtime contract is used by authored terrain
columns, so future destructive terrain renderers do not need an imported-world
or HL1-specific editing path.

### Navigation

`navigation.manifest_path` points to pure-Go voxel graph data in a `.gknav`
manifest. Validation checks source-world identity, chunk size, and voxel
resolution against level/base-world contracts. Streamed runtime loads source
and profile graph tiles near existing level observers; no polygon data or
fallback exists.

Navigation source schema v3 stores compact solid and blocker runs so agent
clearance is derived above each profile's reachable step envelope. Current
builder `voxel_graph_v15` merges ordinary walk/stair/step surfaces into ground
regions and adds bounded directed drops plus gap/upward jumps at exposed region
boundaries. Drop paths and jump arcs are checked against sparse source occupancy
before links are emitted. Older graph bundles must be rebaked.

Special graph links use explicit `drop`, `jump`, `ladder`, `vault`, `mantle`,
or `carrier` transitions. Each link separates stable `link_id` from optional
gameplay `owner_id`, carries entry/exit points, and may carry an apex,
horizontal speed, launch speed, and duration. Carrier links additionally name
the moving support, directed source and destination stops, boarding point, and
controller. Drop links may name a dynamic landing support, required stop, and
landing point. Runtime waits for and holds that support, executes the normal
ballistic drop, verifies live motor contact with the support entity, then exits
onto the destination static span. This keeps auto-return shaft drops separate
from ordinary elevators that are boarded and ridden.
Carrier exit handoff accepts any active static span in the intended destination
region; an off-lane landing completes the handoff and replans from the actual
span instead of steering against the carrier edge.
Agent profiles can store drop, jump, vault, mantle, horizontal-speed,
launch-speed, and gravity limits enforced during link generation, bake
validation, and runtime graph filtering.

### Player

`player` stores optional level-authored defaults for automatic player spawning.

Fields:

- `spawn_kind`
- `height`
- `eye_height`
- `radius`
- `speed`
- `sprint_multiplier`
- `sensitivity`
- `jump_speed`
- `gravity`
- `step_height`
- `ground_probe`
- optional tags

Streamed runtime uses this block only when `AutoSpawnPlayer` is enabled and the
caller did not pass an explicit `StreamedLevelRuntimeConfig.PlayerConfig`.
Explicit runtime config always wins.

Imported maps should use this for source-game player hulls. For example, HL1
levels use the standing player hull:

- height: `72 HU = 1.8288m`
- eye height: `64 HU = 1.6256m`
- radius: `16 HU = 0.4064m`
- step height: `18 HU = 0.4572m`

### Environment

`environment` currently selects a preset-driven lighting and sky setup.

The field is intentionally small:

- `preset`
- optional `directional_casts_shadows`, which requests shadow maps for the
  preset directional light
- optional tags

`applyLevelEnvironment(...)` converts the preset into ambient light, directional light, sky ambient, sun configuration, and skybox layers.

Known presets in current tests include:

- `orbit`
- `daylight`
- `fullmoonNight`
- `fullmoonnight_gi`

### Water Bodies

`water_bodies[]` are authored rectangular or fit-bounds water volumes that
spawn runtime `WaterBodyComponent` entities. They can render as independent
box-shaped water volumes or as a connected surface footprint when several
patches share a `continuity_group`.

Each explicit rectangle contains:

- `id`
- `mode: "ExplicitRect"`
- `surface_y`
- `depth`
- `rect_half_extents`
- `transform`
- optional `source_tag`
- optional `continuity_group`
- optional visual fields such as `color`, `absorption_color`, `opacity`,
  `roughness`, `refraction`, `flow_direction`, `flow_speed`, and
  `wave_amplitude`

Streamed runtime spawns water bodies at level load; water patches then resolve
through the normal water body system and render through the dedicated water
surface feature.

`continuity_group` is the long-term path for imported or authored water that is
made from multiple side-by-side rectangles. The resolver keeps the group on each
runtime patch, and the renderer treats grouped patches as one horizontal surface
footprint with shared edge continuity. This avoids drawing internal vertical
sides between adjacent patches while still using each patch's depth for
absorption and underwater thickness. Leave the field empty for isolated water
boxes that should keep visible sides.

### Ladder Volumes

`ladder_volumes[]` are authored gameplay volumes in level space. They spawn
runtime `LadderVolumeComponent` entities and are consumed directly by the
grounded player controller.

Each ladder volume contains:

- `id`
- `bounds_center`
- `bounds_half_extents`
- optional `climb_speed`
- optional `source_tag`
- optional tags

Overlap only reports ladder availability. W/S explicitly enters ladder
traversal, A/D can move sideways, and jump exits. An active drop, jump,
mantle, or vault cannot be captured by an overlapping ladder volume.

### Moving Brushes And Use Triggers

`moving_brushes[]` describe level-owned dynamic brush intent, such as doors
and discrete-stop carriers.
`use_triggers[]` describe player-use volumes, such as buttons.

Moving brushes contain:

- `id`
- `kind`
- optional `asset_path`
- `bounds_center`
- `bounds_half_extents`
- optional `visual_origin`
- optional `move_direction`, `move_distance`, `speed`, `wait`, and `lip`
- optional `spawn_flags`
- optional `target_name` and `target`
- optional `source_tag` and tags

`speed` and explicit `move_distance` are non-negative. `wait` and `lip` may be
negative to preserve imported mover semantics from formats such as Half-Life 1,
where negative wait usually means stay open and negative lip can mean overtravel.
For non-path movers, a positive `wait` holds the open position for that many
seconds before returning to the closed position; zero and negative waits stay open.
Imported HL1 `func_button` records honor spawn flag `1` (`Don't move`) while
still firing their target.

Use triggers contain:

- `id`
- `kind`
- `bounds_center`
- `bounds_half_extents`
- optional `target_name` and `target`
- optional `source_tag` and tags

The grounded player controller can activate a nearby use trigger or moving brush
with E. Activation toggles the matching `MovingBrushComponent` state through
`target`/`target_name` links. If `asset_path` is present, runtime spawns that
voxel asset on the moving-brush entity and moves it between closed/open targets.
Accepted brush motion publishes changed World and existing Local poses through
the [ECS publication contract](../engine/ecs.md#moving-brush-motion-publication).
Moving-brush bounds also participate in character collision and ground probes;
characters with `GroundedCharacterMotorComponent` inherit the brush's movement
only while their grounded contacts identify that brush as their support. The
motor probes support before brush motion; carrying updates the character's
transform, support contact, and player camera when present. Pose writes follow
the [grounded actor publication contract](../engine/ecs.md#grounded-actor-publication).
Airborne characters, characters outside the support, and characters supported by
another entity do not become riders through nearby coordinates. Players and NPCs
share this motor ownership; `NPCComponent` alone supplies identity and spawn
metadata, without locomotion or carrying.

### NPCs

`npcs[]` preserves imported NPC identity and spawn data for the game runtime.
Along with class, model, transform, health, target, and spawn flags, an NPC may
carry its source `squad_name` and integer `weapons` bitmask. The engine copies
those source-neutral values into `NPCComponent`; each game owns their concrete
squad and equipment semantics.

### Markers

`markers[]` are authored gameplay anchors in level space.

Each marker contains:

- `id`
- `name`
- `kind`
- `transform`
- optional tags

`navigation_role: "door"` treats the closed brush footprint as a dynamic gate.
Navigation bakes also retain the linear open offset so inferred hatch drops are
omitted when the fully open brush would obstruct the destination actor capsule.
`navigation_role: "carrier"` keeps the moving brush out of static occupancy
and bakes directed station-to-station links. A carrier route remains owned by
the carrier traversal while the actor is supported by the moving brush, then
returns to static localization only after the actor reaches the destination
span. The current moving-brush adapter emits `closed` and `open` stops; the
navigation manifest and route contract support additional named stops for
future path movers.

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
- `navigation.manifest_path`

Prefer keeping paths relative to the `.gklevel` file so levels remain portable across tools and modules.

### Navigation bundle files

Navigation bundles use one inspectable `.gknav` JSON manifest plus compact
gzip-compressed binary sidecars:

- `.gkns` for profile-independent source occupancy and integer surface spans
- `.gkng` for profile acceptance bitsets, CSR local edges, regions, and
  exceptional transitions

Each sidecar has fixed magic, format and schema versions, an endian marker,
and an uncompressed payload length. The manifest records its exact SHA-256 and
byte size. Loads reject old extensions, incompatible versions, truncation,
gzip checksum failures, and manifest content mismatches.

## Validation Rules

`content.ValidateLevel(...)` currently checks:

- level name is present
- placement IDs are unique
- placement `asset_path` values exist
- placement modes are supported
- placement-volume IDs are unique
- placement volumes define exactly one source
- placement-volume shape and rule parameters are valid
- ladder-volume IDs are unique
- ladder-volume bounds are positive
- moving-brush IDs are unique and bounds/speeds are valid
- use-trigger IDs are unique and bounds are valid
- referenced asset sets load and validate
- terrain kind, extension, file existence, and chunk-size or voxel-size compatibility
- base-world kind, extension, file existence, and chunk-size or voxel-size compatibility
- navigation manifest extension, validity, source-world identity, and chunk-size or voxel-size compatibility

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
- spawn lights, water bodies, ladder volumes, moving brushes, and use triggers
- spawn terrain chunks when terrain is configured
- apply the environment preset

This is the simplest whole-level runtime path.

### 2. Streamed level runtime

`StartStreamedLevelRuntime(...)` adds chunk-based loading on top of the same authored level data.

It uses the level to drive:

- chunk-local placement spawning
- terrain chunk streaming
- imported base-world chunk streaming
- voxel navigation source/graph tile streaming
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
