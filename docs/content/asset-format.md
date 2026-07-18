# Authored Asset Format

`.gkasset` is the shared authored asset document format used by both the editor and runtime asset-spawn path.

For the broader authored-asset model, asset sets, level references, and runtime `AssetServer` relationship, see [`game-assets.md`](game-assets.md).

## Schema

- Top-level fields:
  - `id`
  - `schema_version`
  - `name`
  - `tags`
  - `runtime`
  - `skeleton`
  - `animation_clips`
  - `parts`
  - `lights`
  - `emitters`
  - `markers`
- Current schema version: `3`
- Authored IDs are stable UUID-like strings serialized directly in JSON.
- Root transforms are authored relative to the asset root.
- Child transforms are authored relative to the parent part.
- `pivot` is stored in authored space and must round-trip unchanged.

## Supported Source Kinds

- `vox_model`
  - file-backed VOX model reference with `path` and `model_index`
- `group`
  - transform-only authored node with no render source
  - useful for imported hierarchy parents and empty pivots
- `vox_scene_node`
  - VOX scene-node reference with `path` and `node_name`
  - `node_name` must resolve to exactly one named VOX scene node or subtree
  - `model_index`, when present, disambiguates the model inside that subtree
  - if `model_index` is omitted, the named subtree must contain exactly one model
- `voxel_shape`
  - inline explicit voxel payload
  - voxel coordinates are authored in the part's local voxel space
  - the part's `transform.pivot` is used as the renderer pivot; if omitted, the pivot is the local voxel origin `[0, 0, 0]`
- `procedural_primitive`
  - authored primitive with `primitive` and flat numeric `params`

## Runtime And Animation Contracts

`runtime.collapse_voxel_parts` may collapse static voxel parts into one runtime
voxel model when the asset is eligible. Animated assets must keep authored
voxel parts uncollapsed so each part can move independently.

`skeleton` is descriptive authored metadata. It records imported or authored
bone IDs, names, parent IDs, bind transforms, and tags. Runtime animation does
not require a separate skinning component: clips target authored item IDs
directly. Assets tagged `skeleton:rest_basis` preserve complete local bind
rotations suitable for offline animation retargeting; importers must not add
that tag when bone rotations have been flattened into render geometry.

`animation_clips` contain local-space tracks:

- `target_id` references a spawned authored item ID such as a part, light,
  emitter, or marker. Skeleton bones are metadata; importers that want bone
  tracks to play should emit matching transform-only `group` parts for the
  animated bone pivots.
- `position_keys`, `rotation_keys`, and `scale_keys` replace the target's local
  channel when present.
- omitted channels keep the target's bind transform from the spawned asset.
- key values are local to the authored parent, not world-space values.

Runtime playback starts from bind pose, applies one base clip, then applies
ordered optional `AnimationLayer` overlays. A layer declares its clip, time,
speed, weight, override or additive mode, authored item-ID mask, and root-motion
policy. Masks use generated/authored item IDs; consumers must not derive them
from display names at runtime. `locked` root motion ignores position keys on
root targets, leaving controller movement authoritative.

The current engine animation path is rigid-part animation. It does not skin or
deform voxel geometry. To animate imported character models, split the source
model into rigid voxel parts attached to transform-only `group` pivots, then
animate those group pivots.

For explicit `voxel_shape` parts, the local voxel origin is meaningful. Runtime
spawning copies `transform.pivot` into the renderer pivot with custom pivot
mode. Generated importers that localize voxels to a min corner should usually
leave `transform.pivot` omitted or `[0, 0, 0]`; otherwise the renderer will draw
the chunk around a different visual origin.

## Extension Checklist

When adding a new source kind:

1. Add the new enum value and JSON fields in `content.AssetSourceDef`.
2. Extend `content.ValidateAsset` with the new kind's required payload.
3. Extend the shared spawn/import path so editor and runtime agree on behavior.
4. Add validation and round-trip coverage plus at least one representative fixture or synthetic test.

## Current Constraints

- Only parts may be authored parents.
- `vox_scene_node` requires unique scene-node names inside the source `.vox`; duplicates are rejected instead of guessed.
- Markers are authored and spawned, but richer gameplay or rendering semantics are still open-ended.
- Editor-only convenience flags such as `hide`, `lock`, and `solo` are intentionally not serialized into `.gkasset`.
- Old prototype JSON compatibility is out of scope.
