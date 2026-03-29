# Editor Integration

This page documents the boundary between `gekko` and `gekko-editor`.

It is not a full editor architecture doc. Its purpose is narrower:

- show how the editor reuses engine content schemas
- show which modules are installed in the editor app
- show where engine changes tend to break editor workflows

## Main Relationship

`gekko-editor` is a consumer of `gekko`, not a separate schema authority.

The editor:

- reuses `gekko/content` types through thin alias layers in `src/formats`
- uses engine modules for time, input, rendering, hierarchy, UI, and chunk observation
- adds editor-only modules for selection, gizmos, asset authoring, level authoring, and import workflows

## Editor App Composition

The editor main entry point is `gekko-editor/main.go`.

It installs:

- `TimeModule`
- `AssetServerModule`
- `InputModule`
- `VoxelRtModule`
- `HierarchyModule`
- `FlyingCameraModule`
- `LifecycleModule`
- `UiModule`
- editor core modules
- asset editor modules
- level editor module
- `ChunkObserverModule`

That means engine changes to these modules are editor changes by default.

## Schema Boundary

The key adapter layer is:

- `gekko-editor/src/formats/game_asset.go`
- `gekko-editor/src/formats/level.go`

Those files mostly alias engine content types:

- `content.AssetDef`
- `content.LevelDef`
- related enums and helper functions

This is a strong signal:

- if you change `gekko/content` schemas, defaults, or validation, you are also changing editor document behavior
- the editor is intentionally thin at the schema level

## Asset Editor Boundary

The asset editor modules:

- load and save `.gkasset` documents through `content.LoadAsset` and `content.SaveAsset`
- build authored parts from VOX inspection and import flows
- respawn a preview hierarchy using the engine spawn path

Important integration points:

- `asset_workflow.go`
- `asset_vox_import.go`
- `asset_preview_helpers.go`

If an engine change affects:

- authored transform semantics
- spawn hierarchy behavior
- source-path resolution
- marker or emitter semantics

expect asset editor preview and save/load round trips to be affected too.

## Level Editor Boundary

The level editor modules:

- load and save `.gklevel` documents through `content.LoadLevel` and `content.SaveLevel`
- use engine authored-level spawn and preview paths
- integrate terrain authoring, base-world import, markers, and placement volumes

Important integration points:

- `level_workflow.go`
- `level_preview.go`
- `terrain_authoring.go`
- `base_world_import.go`
- `placement_volumes.go`

If an engine change affects:

- level validation
- placement-volume expansion
- terrain chunk baking or loading
- imported-world manifests or chunk spawn
- marker semantics

expect level editor workflows to be affected.

## Selection and Preview Coupling

The editor relies on engine-side runtime state for preview and picking:

- `SelectionState`
- transform gizmo systems
- `VoxelRtState` raycast and preview rendering
- authored preview respawn helpers

So changes to:

- hierarchy propagation
- marker parenting
- renderer picking
- spawned metadata components

often show up first as editor selection or preview regressions.

## Practical Rule for Agents

When changing engine code that touches authored content or preview/runtime hierarchy:

1. update engine docs or contracts first
2. inspect the matching `gekko-editor/src/formats` file
3. inspect the relevant editor workflow or preview module
4. run editor tests if the engine-side change is shared

## Verification

Editor compile/test pass:

- `cd /Users/ddevidch/code/go/gekko3d/gekko-editor && env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Use that whenever:

- `gekko/content` types change
- authored spawn behavior changes
- renderer picking or preview behavior changes
- runtime asset provenance changes
