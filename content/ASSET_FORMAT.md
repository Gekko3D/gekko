# Authored Asset Format

`.gkasset` is the shared authored asset document format used by both the editor and runtime-facing asset spawn path.

## Finalized schema

- Top level fields:
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
- Child transforms are authored relative to the child item's parent part.
- `pivot` is stored in the same authored space as the item's transform and must round-trip unchanged.

## Supported source kinds

- `vox_model`
  - file-backed VOX model reference with `path` and `model_index`
- `vox_scene_node`
  - schema-supported VOX scene-node reference with `path` and `node_name`
- `procedural_primitive`
  - authored primitive with `primitive` and flat numeric `params`

To add a new source kind later:

1. Add the new enum value and JSON fields in `content.AssetSourceDef`.
2. Extend `content.ValidateAsset` for the new kind's required payload.
3. Extend the shared spawn/import path for runtime or editor behavior.
4. Add round-trip and validation coverage plus at least one representative fixture or synthetic test.

## Current limitations

- Only parts may be authored parents.
- `vox_scene_node` is part of the schema and editor-authored data, but runtime/editor support remains intentionally limited to the behavior added in earlier prompts.
- Markers are authored, spawned, and metadata-bearing, but richer gameplay/rendering semantics remain future work.
- Editor-only convenience flags such as `hide`, `lock`, and `solo` are not serialized into `.gkasset`.
- Old prototype JSON compatibility is intentionally out of scope.
