# Authored Asset Format

`.gkasset` is the shared authored asset document format used by both the editor and runtime asset-spawn path.

For the broader authored-asset model, asset sets, level references, and runtime `AssetServer` relationship, see [`game-assets.md`](game-assets.md).

## Schema

- Top-level fields:
  - `id`
  - `schema_version`
  - `name`
  - `tags`
  - `parts`
  - `lights`
  - `emitters`
  - `markers`
- Current schema version: `1`
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
- `procedural_primitive`
  - authored primitive with `primitive` and flat numeric `params`

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
