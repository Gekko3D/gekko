# Runtime Assets

This page documents the engine-side asset layer owned by `AssetServer`.

Use this when you need to understand:

- how authored files become runtime assets
- what `AssetID` values refer to
- where voxel models, palettes, textures, and materials are created
- what the renderer or gameplay systems actually consume at runtime

For authored asset documents, see [`../content/game-assets.md`](../content/game-assets.md).

## Two Layers

Keep these separate:

- authored content
  - `.gkasset`, `.gkset`, `.gklevel`, `.gkterrain`, `.gkworld`
- runtime assets
  - `AssetServer` records keyed by `AssetID`

Authored content is persistent and path-based.
Runtime assets are process-local and ID-based.

## `AssetServer`

`AssetServer` is installed by `AssetServerModule` and stored as a resource.

It owns maps for:

- meshes
- materials
- textures
- samplers
- voxel models
- voxel palettes
- raw VOX files

The server is protected by an internal RW mutex, so asset creation and reads are synchronized at the map level.

## Main Runtime Asset Types

The most important record types are:

- `VoxelModelAsset`
  - voxel data, brick size, optional source path
- `VoxelPaletteAsset`
  - palette colors, optional material data, optional PBR-style metadata, optional source path
- `VoxelFileAsset`
  - stored VOX file handle
- `TextureAsset`
  - raw texels plus width, height, depth, dimension, and format
- `MaterialAsset`
  - shader name, shader source listing, and vertex type
- `MeshAsset`
  - vertex and index data
- `SamplerAsset`
  - sampler identity record

Public handles such as `Mesh` and `Material` are thin wrappers around `AssetID`.

## Common Creation Paths

### Voxel models and palettes

The main creation helpers live in:

- `asset_vox_model.go`
- `asset_procedural_primitives.go`
- `asset_vox_scene.go`

Typical flows:

- `CreateVoxelModelFromSource(...)`
  - stores a voxel model, optionally scaling it first
- `CreateVoxelPaletteFromSource(...)`
  - stores the palette and VOX material data
- `CreatePBRPalette(...)`
  - creates a synthetic palette with engine-side material metadata
- `CreateVoxelFile(...)`
  - registers the raw loaded VOX file

Gameplay/runtime material helpers:

- `GameplaySeeThroughMaterial(baseColor, transparency)`
  - creates a transparent material for readability helpers without optical glass behavior
- `ApplyGameplaySeeThroughMaterial(&mat, transparency)`
  - converts an existing material to non-refractive gameplay transparency in place

Authored asset spawning uses these helpers when resolving:

- `vox_model`
- `vox_scene_node`
- `procedural_primitive`

### Textures

Texture creation helpers live in `asset_texture.go`:

- `CreateTexture(filename)`
  - decodes a PNG and stores it as a 2D RGBA texture asset
- `CreateTextureFromTexels(...)`
  - registers raw texture data directly
- `CreateVoxelBasedTexture(...)`
  - builds a 3D texture from a voxel model plus palette

### Materials and meshes

Also in `asset_texture.go`:

- `CreateMaterial(filename, vertexType)`
  - reads shader source and stores a material asset
- `CreateMesh(vertices, indexes)`
  - stores mesh buffers
- `CreateSampler()`
  - stores a sampler record

## How Authored Content Turns Into Runtime Assets

When `SpawnAuthoredAssetWithOptions(...)` resolves a part source:

- `group`
  - creates no runtime geometry asset
- `vox_model`
  - loads a VOX file, then creates a `VoxelModelAsset` and `VoxelPaletteAsset`
- `vox_scene_node`
  - loads a VOX file, resolves the node to a model index, then creates a model and palette asset
- `procedural_primitive`
  - creates a generated voxel model and a default palette

The spawned ECS entity then references those runtime assets through `VoxelModelComponent`.

## Source Paths and Provenance

Several runtime asset records carry `SourcePath`.

That field is useful for:

- debugging where a model or palette came from
- checking whether a runtime asset was imported from disk or created procedurally

It is metadata only. It is not a canonical deduplication key.

## Important Constraints

- `AssetID` values are process-local identities, not stable authored references.
- The asset server does not currently behave like a content-addressed cache.
- Repeated loading of the same authored source can create additional runtime assets unless higher-level code reuses them.
- The renderer and gameplay systems should treat `AssetID` as opaque.
- Palette alpha alone is not the right tool for gameplay readability transparency.
  - Runtime palette-alpha inference currently opts into thin surface-glass behavior with transmission/refraction.
  - For “see through this wall/object so the player can read the scene,” prefer `GameplaySeeThroughMaterial(...)` or `ApplyGameplaySeeThroughMaterial(...)`.

## Ownership Boundaries

- authored files own persistent identity through string IDs and paths
- `AssetServer` owns runtime asset instances
- ECS components own references to runtime assets
- renderer bridge code converts runtime assets into renderer-native objects and GPU resources

If a bug is “the wrong geometry was authored,” start in content.
If a bug is “the right authored data produced the wrong runtime model or palette,” start in asset spawn and `AssetServer`.
If a bug is “the right runtime asset rendered incorrectly,” start in the renderer bridge or renderer internals.

## What Is Missing Today

Agents should be aware of the current limitations:

- there is no documented eviction or lifetime policy for runtime assets
- there is no central deduplication layer for repeated authored references
- material and texture workflows are thinner and less documented than voxel asset workflows

That means code changes here should be conservative and explicit about ownership and reuse.
