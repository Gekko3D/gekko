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

## Source Kinds for Parts

A part's `source.kind` controls how geometry is produced:

- `group`
  - transform-only node with no geometry
- `vox_model`
  - load a specific model from a `.vox` file by `path` and `model_index`
- `vox_scene_node`
  - resolve geometry from a named VOX scene node subtree
- `procedural_primitive`
  - generate a primitive such as `cube`, `sphere`, `cone`, or `pyramid`

Voxel-backed parts become `VoxelModelComponent` entities during spawn. Group parts still participate in hierarchy and parenting but do not create geometry.

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
- Prefer stable, descriptive names and tags even though IDs are the real identity.
- Use `.gkset` only when a level needs weighted variety; do not duplicate near-identical placement volumes with hardcoded asset paths.

## Minimal Example

```json
{
  "id": "crate-asset",
  "schema_version": 2,
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
