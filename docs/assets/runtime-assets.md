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
  - palette colors, optional material data, optional palette/material animations, optional PBR-style metadata, optional source path
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

`GetVoxelPalette` returns a value whose nested maps, slices and animation pointers
can alias server storage. Palette IDs alone therefore establish no immutable
content revision. Core renderer sync preserves these mutable inputs with live
comparison against independently owned, bounded fingerprint snapshots; see
[renderer ownership](../renderer/runtime.md#effective-palette-fingerprints).
Mutate aliased palette data on the main thread, not concurrently with extraction.

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
- `CreateVoxelPaletteAsset(...)`
  - stores a full palette asset, including material animation metadata when
    imported or authored content needs palette/material color sequences
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

## Streamed Prepared Geometry Lifetime

The streamed runtime owns a bounded cache for imported full/proxy geometry.
The cache charges both its prepared backing and the actual registered copy.
`RegisterSharedVoxelGeometry` retains defensive copying for mutable callers.
P5a workers create a separate registration copy behind a private single-use
handle; eligible main-thread commits adopt it without another deep copy.
Workers never register assets. Cached source maps remain immutable and separate
from live renderer geometry. Warm reuse and ineligible/live-backed commits drop
unused handles; the cache's current source remains authoritative.

S2b pending admission charges the extra copy until consumption or drain, including
deferred/cancelled results. `PreparedGeometryAssetAdoptions` counts successful
worker-payload registrations; ordinary registrations and warm reuse do not
increment it. Cache ledger traversal remains main-thread work.

Acquired runtime users pin that copy. Warm entries share an LRU byte/entry policy; live users may
exceed the byte ceiling and expose pressure metrics. Oversized or disabled warm
entries delete their registered assets after the final release.

Cleanup uses the original registering AssetServer and exact acquired asset ID,
including uncached/empty-key registrations. Successful streamed Stop releases
all assets owned by that cache and preserves unrelated server assets. Renderer
retention and private mutable geometry have separate lifetime owners. This
policy does not add eviction to authored models, palettes, textures or all
other AssetServer records. See
[S2a](../roadmaps/streamed-rendering-s2a.md) for the storage-charge definition,
defaults and remaining memory bounds.

### Streamed terrain registration

Terrain workers also build geometry, bounds and a separate registration copy.
Pending admission charges both maps. Main commits adopt only without a current
backing removal; otherwise they keep the ordinary build/removal/defensive path.
Terrain backing and object-scoped renderer copies keep their existing behavior.
These editable assets are not interned in the prepared-geometry cache.

The runtime records each adopted terrain asset's exact ID and registering server
before entity flush/hooks. Normal unload releases it after persistence/removal;
successful Stop also releases partial commits absent from `LoadedChunks`. Failed
Stop retains ownership. Deletion unregisters IDs without clearing maps held by
renderer/physics. Eager and fallback registrations keep their existing lifetime.
`PreparedGeometryAssetAdoptions` includes actual terrain transfers as well as
imported full/proxy transfers. Backing setup and renderer copies remain main-thread
work; this does not bound all work in a large chunk.

## Decoded Content Lifetime

`RuntimeContentLoader` bounds warm decoded definitions across all eight content
kinds with one byte LRU and per-path concurrent decode suppression. Its charge
estimates decoded structs and referenced storage at admission. Raw `Load*`
pointers remain usable after eviction; arbitrary external borrowers and later
normalization are outside cache-owned accounting. Eviction never clears or
recycles returned definitions.

Use `loader.NewScope()`, load through `scope.Loader()` and call `scope.Close()`
when the consumer releases decoded data. Active scopes pin each shared entry
once and may exceed the budget. Streaming metadata/backing providers hold a
world-session scope; prepared results and navigation source batches use shorter
scopes. Live entity geometry/heightmaps have their own storage after commit.
Successful Stop clears runtime-created loader ownership and preserves supplied
loader users. See [S2b](../roadmaps/streamed-rendering-s2b.md) for defaults,
pending-result admission and accounting limits.

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

- runtime asset eviction is scoped to the streamed prepared-geometry cache;
  other asset creation paths have no general eviction policy
- there is no central deduplication layer for repeated authored references
- material and texture workflows are thinner and less documented than voxel asset workflows

That means code changes here should be conservative and explicit about ownership and reuse.
