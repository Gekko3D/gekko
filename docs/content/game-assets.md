# Game Assets

This document explains how game-facing assets are authored, loaded, and turned into runtime entities in `gekko`.

There are two layers to keep distinct:

- authored content files in `gekko/content`
  - JSON documents such as `.gkasset`, `.gkset`, and level files
- runtime assets in `AssetServer`
  - voxel models, palettes, textures, materials, samplers, and meshes stored under engine-owned `AssetID` values

Authored files are the source of truth for gameplay content. Runtime assets are the transient engine-side representation used for rendering and simulation.

## Main File Types

### `.gkasset`

An authored asset document describing a reusable object hierarchy.

It can contain:

- `parts`
  - visible voxel-backed parts or transform-only groups
- `skeleton`
  - optional imported/authored bone metadata for tools and diagnostics
- `animation_clips`
  - optional local-space rigid animation tracks for spawned authored items
- `lights`
  - point, directional, spot, or ambient lights
- `emitters`
  - particle emitters
- `markers`
  - named attachment or gameplay anchor points such as `muzzle` or `spawn_anchor`

See [`asset-format.md`](asset-format.md) for the exact schema.

### `.gkset`

A weighted asset set used by placement volumes.

Each entry contains:

- `asset_path`
- `weight`
- optional tags

Use this when a level should spawn one of several authored assets with deterministic weighted selection.

### Level Documents

Levels reference assets rather than embedding them inline.

For the full level model, including placement volumes, terrain, base worlds, and markers, see [`levels.md`](levels.md).

The main asset-bearing fields are:

- `placements[].asset_path`
  - one explicit authored asset per placement
- `placement_volumes[].asset_path`
  - one authored asset repeated across sampled positions
- `placement_volumes[].asset_set_path`
  - one weighted asset-set file used to choose the spawned asset per sampled instance

## Authored Asset Model

An authored asset is a reusable ECS hierarchy template.

At spawn time:

1. the asset file is loaded and validated
2. a root entity is created with `AuthoredAssetRootComponent`
3. parts, lights, emitters, and markers are created as child entities
4. parent-child links are attached from authored `parent_id` references
5. transforms are resolved through the normal hierarchy system

Each spawned item gets `AuthoredAssetRefComponent` so runtime code can map entities back to authored item IDs.

Animated authored assets are rigid hierarchies. Runtime playback samples
`animation_clips` into item `LocalTransformComponent` values, then
`HierarchyModule` resolves the world transforms. The engine does not skin or
deform voxel geometry; importers should split characters and machinery into
rigid voxel parts under transform-only `group` pivots.

### Baking Source-Engine Animation

`cmd/sourcemdlanim` imports Source MDL v48 animation data into an existing
`.gkasset` rig. It is animation-only: meshes and materials remain owned by the
target asset. Both the Source MDL bind skeleton and the target asset's explicit
`skeleton:rest_basis` metadata are used to calculate bone-basis corrections;
no idle clip or sequence frame is used as calibration. Regenerate older HL1
rigid assets once so their previously flattened bind rotations are preserved.
For unambiguous single-child chains, the baker also aligns source and target
bind-segment directions to construct an automatic retarget rest pose. This
handles differing limb rest poses without model-specific arm adjustments.
Source sequence bone weights are applied before retargeting. The supported
subset is deliberately explicit: blended,
additive/delta, layered, IK-driven, local-hierarchy, and
external-animation-block sequences are rejected instead of being approximated.
Generated clips default to locked root translation, so a character controller
stays authoritative.

## Source Kinds for Parts

A part's `source.kind` controls how geometry is produced:

- `group`
  - transform-only node with no geometry
- `vox_model`
  - load a specific model from a `.vox` file by `path` and `model_index`
- `vox_scene_node`
  - resolve geometry from a named VOX scene node subtree
- `voxel_shape`
  - inline explicit voxel payload
  - uses `transform.pivot` as the voxel renderer pivot, defaulting to the local voxel origin
- `procedural_primitive`
  - generate a primitive such as `cube`, `sphere`, `cone`, or `pyramid`

Voxel-backed parts become `VoxelModelComponent` entities during spawn. Group parts still participate in hierarchy and parenting but do not create geometry.

Pivot rules:

- `voxel_shape` is explicit local voxel data. Its `transform.pivot` becomes the
  renderer pivot, defaulting to local voxel origin `[0, 0, 0]`.
- Other voxel-backed sources keep the runtime renderer's default pivot behavior
  unless a source-specific loader defines otherwise.
- A parent part's renderer pivot never changes child transforms. Use `group`
  parts when an asset needs stable gameplay or animation pivots.

Voxel resolution is per part. Imported or authored assets may mix resolutions
inside one scene: large static world geometry can stay coarse, props can use a
medium resolution, and pickups or character parts can use finer voxels. Runtime
spawn copies each part's `voxel_resolution` into its `VoxelModelComponent`.

## Path Resolution Rules

Authored paths are document-relative by default.

The engine resolves relative paths with `content.ResolveDocumentPath(...)`, which means:

- an absolute path is used as-is
- if the relative path already exists from the current working directory, it is accepted
- otherwise the path is resolved relative to the document that contains it

That rule applies to:

- asset source files such as `parts[].source.path`
- level placement `asset_path`
- placement volume `asset_path`
- placement volume `asset_set_path`
- asset-set entry `asset_path`

In practice, keep authored references relative to the containing document so content remains portable across modules and tools.

## Runtime Asset Layer

`AssetServer` owns runtime asset records keyed by engine `AssetID` values.

The main runtime record types are:

- `VoxelModelAsset`
- `VoxelPaletteAsset`
- `VoxelFileAsset`
- `TextureAsset`
- `MaterialAsset`
- `MeshAsset`
- `SamplerAsset`

These are not authored documents. They are created at runtime from authored content, imported voxel data, or procedural generation helpers.

Examples:

- a `.gkasset` part using `vox_model` causes the engine to load a VOX file and create a `VoxelModelAsset` plus `VoxelPaletteAsset`
- a `procedural_primitive` part creates a generated voxel model and a default palette
- emitter configuration may reference `texture_path`, which is gameplay-facing authored data even though the bound texture is a runtime GPU asset

## Asset Sets and Placement Volumes

Asset sets exist to support procedural or repeated placement in levels.

The flow is:

1. a level placement volume references either one `asset_path` or one `asset_set_path`
2. `ExpandPlacementVolumePreview(...)` resolves the candidate asset paths
3. if an asset set is used, weighted selection chooses which `.gkasset` path each instance will spawn
4. each sampled instance becomes a normal authored-asset spawn

This keeps randomization in the level layer while authored asset structure stays reusable and deterministic.

## Spawn Metadata Added to Entities

When assets are spawned through levels, the engine preserves authored provenance on the spawned entities.

Important components include:

- `AuthoredAssetRootComponent`
  - identifies the root entity for one spawned authored asset
- `AuthoredAssetRefComponent`
  - maps an entity back to an authored asset item ID and kind
- `AuthoredLevelPlacementRefComponent`
  - tracks which level placement produced the spawned asset root
- `AuthoredLevelItemRefComponent`
  - tracks which level placement produced an individual spawned item
- `AuthoredMarkerComponent`
  - exposes marker kind and tags on marker entities

This metadata is the bridge between authored content, gameplay logic, editor tooling, and streamed-level runtime management.

## Recommended Authoring Conventions

- Keep authored asset references relative to the containing document.
- Use `.gkasset` for reusable object hierarchies, not for whole levels.
- Use `group` parts for pivots and hierarchy organization instead of inventing fake geometry.
- Keep animated assets uncollapsed. Collapsing voxel parts is for static assets
  whose child geometry no longer needs independent transforms.
- Author animation tracks in local space. Do not bake renderer pivots or
  world-space parent transforms into clip keys.
- Prefer stable, descriptive names and tags even though IDs are the real identity.
- Use `.gkset` only when a level needs weighted variety; do not duplicate near-identical placement volumes with hardcoded asset paths.

## Minimal Example

```json
{
  "id": "crate-asset",
  "schema_version": 3,
  "name": "crate",
  "parts": [
    {
      "id": "body",
      "name": "body",
      "source": {
        "kind": "procedural_primitive",
        "primitive": "cube",
        "params": {
          "sx": 1,
          "sy": 1,
          "sz": 1
        }
      },
      "transform": {
        "position": [0, 0, 0],
        "rotation": [0, 0, 0, 1],
        "scale": [1, 1, 1],
        "pivot": [0, 0, 0]
      },
      "voxel_resolution": 0.1,
      "model_scale": 1
    }
  ],
  "markers": [
    {
      "id": "fx-anchor",
      "name": "impact_fx",
      "parent_id": "body",
      "transform": {
        "position": [0, 0.5, 0],
        "rotation": [0, 0, 0, 1],
        "scale": [1, 1, 1],
        "pivot": [0, 0, 0]
      },
      "kind": "effect_anchor"
    }
  ]
}
```

This yields one authored asset root entity, one spawned part entity with voxel geometry, and one child marker entity that gameplay code can query later.
