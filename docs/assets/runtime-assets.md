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

Streamed placement commits use a private creation callback to record every
entity before internal flushes or later spawn failures, including collapsed
composites. Only voxel-backed items receive per-item snapshot ownership; group,
light, emitter and marker entities retain teardown ownership. Public spawn APIs
and collapse eligibility remain unchanged. Collapsed composites do not gain a
new per-item persistence identity or disk format.

### Compiled ordinary asset preparation

`LoadAndPrepareAuthoredAsset` and `LoadAndSpawnAuthoredAsset` explicitly select
compiled input for an exact lowercase `.gkassetc` suffix. Other paths retain
legacy JSON behavior, and corrupt selected compiled input never falls back.
Compiled preparation returns the existing `PreparedAuthoredAsset`; prepared
spawning keeps the existing hierarchy, material, animation and ECS contracts.
Ordinary direct and expanded level placements use the same path, including
streamed commits and their existing ownership callbacks, shadows and rollback.
Streamed workers prepare compiled ordinary CPU packets per resolved asset path.
Main-thread placement commits publish their geometry and palettes. Legacy JSON
ordinary preparation retains its existing commit path. Direct level and streamed
startup NPCs use compiled preparation with the existing multipart asset root under
the NPC entity. Health, animation bindings and creation-before-load-error behavior
are unchanged. Moving brushes, chargers, breakables and pickups still require
authoring JSON.

Preparation verifies every referenced shape's frame identity/sizes, effective
lattice and original base identity, and resolves animations before publishing
geometry. Initial compiled preparation uses default E2 logical base limits.
Larger custom-profile frames remain supported by typed IO but need separate
runtime admission work; legacy authored preparation keeps its existing limits.
Temporary independent loader scopes protect previously accepted caller pins and
reject a closed originating scope. Accepted decoded frames can still be verified
after their borrowed codec closes.

Prepared metadata is an owned copy of the immutable cached header. Compiled
animation-set references resolve relative to the header, and rig references
relative to the selected set, without cwd fallback. Both require portable
relative paths. Emitter texture paths become absolute in the prepared copy;
existing texture decoding remains unchanged. Authoring sources are unnecessary.

Canonical bricks already include `ModelScale`; dense construction does not
resample or form voxel JSON cache keys. Existing `AssetServer` geometry storage
shares a namespaced C1 content identity, including lattice, across compiled
paths. Palettes remain independent material bindings. Verified original-base
metadata never derives from a mutable warm geometry entry. Public mutable
geometry access and managed edit isolation retain their existing contracts.
With a nil asset server, preparation still verifies input and resolves metadata
without constructing or registering geometry.

Private compiled adoption accepts independently verified C1 content/base
identities and the supported source lattice. A cold publication transfers a
separate P5 single-use registration copy into the ordinary shared geometry key;
the worker source remains independent. Key and original-base metadata publish
together under the server lock. A warm publication releases the unused copy and
reuses the existing ID. Verified metadata may fill missing original provenance;
conflicting recorded provenance fails without replacement or reading mutable
warm geometry. Public registration keeps defensive copying.

These IDs retain ordinary `AssetServer` lifetime. Packet release never deletes
adopted geometry, and imported prepared-cache eviction/leases do not own these
IDs. If public deletion removes a shared ID after its packet registration was
consumed, publication rebuilds an independent copy from the owned packet source
through the same atomic adoption boundary. Conflicting warm provenance still
fails; publication never rereads source or frame files.

Private CPU packets own metadata, resolved animations and unique dense sources
with separate single-use registration copies. Duplicate C1 identities share one
packet shape while part palette metadata remains independent. Complete shape and
animation verification precedes dense construction. Child decoded scopes close
after preparation; caller pins survive failures and cancellation. No decoded
definitions or published asset IDs escape into the packet. Group-only assets
produce packets without geometry.

Cancellation is cooperative before header/shape loads and animation resolution,
between frames and whole-shape builds, and at completion; it does not interrupt
codec, animation-resolver or dense-builder internals. Failed
or canceled preparation releases built handles and checks the originating scope
before returning usable output. Aliased packet release is idempotent. Metadata
and source storage remain owned by the envelope until drain.

Existing pending admission charges packet metadata, unique dense sources and
registration storage once; repeated placements reuse one packet for a selected
resolved path. Deferred, canceled, stale, failed and completed results release
unused handles through existing result cleanup. A packet must match the current
canonical selected path; an unrelated packet uses the existing selected loader.
Placement publication follows
current generation, deletion and movement checks. Latest object overrides still
resolve at the placement commit, and existing callbacks, rollback and Stop own
every created entity. Palette/texture publication stays on the main thread;
normal result release never evicts ordinary global geometry.

### Authored voxel collapse reuse

Repeated eligible static collapses reuse the existing composite before
rasterization, after current source/palette resolution and ordered part
validation. Warm reuse preserves matching resolution, empty-part skipping,
sample-error-before-zero-scale precedence and the nonempty additive-input rule.
Sample existence uses model voxels (including zero colors) or nonzero raw payload
in mask-selected bricks; occupancy flags/count alone are insufficient.

Cache keys, cold baking, palette/part IDs and automatic fallback/forced errors
are unchanged. Fully subtracted empty composites remain valid. Public edits to
the cached composite remain visible on reuse; deletion permits cold rebuilding.
`AssetServer.AuthoredVoxelCollapseStats()` reports cumulative `Builds` (cold
rasterization attempts after source/palette/key resolution) and `Hits` (successful
validated warm reuse). Reads are scalar and nil-server reads return zero. Source
loading, temporary hierarchy resolution, palette/key work and live validation
remain; these counters do not measure frame time.

Cold authored collapse and level-brush composition stream accepted rasterized
writes through [ordered edits](../renderer/editing.md#ordered-edit-streams), once
per part. Source/sample order, transformed voxel-center tests, epsilon, material
assignment and add/subtract order are unchanged. Validation precedes writes;
material flags finalize before the next part. Existing sample arrays remain,
without an additional write list or a change to warm cache authority.

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
increment it. Eligible full/proxy workers retain the finished registration copy's
storage description: actual object identities, child edges and standalone charge.
Pending admission also charges retained descriptor metadata. Single-use adoption
passes the description directly into the existing cache ledger before exposing
the asset ID; cancellation, unused handles and warm reuse release it.

The cache checks every object identity before installing a fresh description.
Other admissions keep ordinary capture. Later shared aliases find those same
physical nodes and retain existing attribution/pin rules. Prepared standalone
charges are cached on first policy use; only qualified distinct registration
copies can use their sum for retention policy. Generic shared graphs retain union
calculation. `PreparedGeometryCacheStorageCaptureVisits` counts new descriptions
created by the cache ledger, including worker source admission. Prebuilt
installation and reads do not advance it. Flat identity installation and
first/last ownership traversal remain main-thread work.

Acquired runtime users pin that copy. Warm entries share an LRU byte/entry policy; live users may
exceed the byte ceiling and expose pressure metrics. Oversized or disabled warm
entries delete their registered assets after the final release. Unpinned entries
maintain exact LRU order; eviction selects the oldest directly. Workers defer
when that oldest entry owns an asset, leaving deletion to engine-thread trim.
`PreparedGeometryCacheEvictionCandidateVisits` counts nonnil victims examined
under pressure, including worker deferrals; reads and no-pressure maintenance do
not advance it. Selection avoids scanning pinned/warm owners; storage graph
removal retains its existing charge/lifetime work. Reference updates propagate
to children only when that owner kind becomes present or absent, preserving
physical sharing, prepared-preferred attribution and independent pinned bytes.
`PreparedGeometryCacheStorageReferenceVisits` counts nonnil reference adjustments;
reads do not advance it. Repeated shared references avoid child walks, while
first/last ownership and standalone charge calculation can still traverse a graph.

Cleanup uses the original registering AssetServer and exact acquired asset ID,
including uncached/empty-key registrations. Successful streamed Stop releases
all assets owned by that cache and preserves unrelated server assets. Renderer
retention and private mutable geometry have separate lifetime owners. This
policy does not add eviction to authored models, palettes, textures or all
other AssetServer records. See
[S2a](../roadmaps/streamed-rendering-s2a.md) for the storage-charge definition,
defaults and remaining memory bounds.

### Compact private prepared sources

Opt-in `StreamedLevelRuntimeConfig.CompactPreparedGeometry` qualifies fresh
immutable imported full/proxy worker sources for compact retention in this same
cache. The default dense path, public `Brick.Payload`, mutable asset access and
live renderer/physics/navigation maps retain their contracts. Captured backing
keeps dense preparation; a backing change before commit uses current dense
fallback and the existing removal path.

Each entry owns either dense or compact prepared authority. Compact storage
preserves raw cell values, masks, material metadata, revisions/tombstones, bounds
and auxiliary bytes; use dense fallback when it has the lower policy charge.
Workers create independent dense registration copies. Normal adoption and warm
asset reuse avoid reconstructing a redundant dense prepared source on commit.

Generic dense exposure permanently promotes that entry to retained dense
authority. Later raw writes have no stale compact shadow. Pending older compact
captures stay independently retained and charged, but cannot adopt a candidate
over a promoted or rebuilt current source. Existing registered assets keep their
ID and copy isolation during promotion. Source-qualified adoption also rejects
candidates captured after dense exposure; current defensive copying preserves
later raw edits. Exposure metadata survives eviction and re-admission. Generic
asset-only acquisition keeps the winning source compact and copies its current
contents. Without a retained entry, compact captures remain independent sealed
snapshots; an uncached dense exposure does not repopulate the cache.

Compact storage, dense registration copies and pending captures use the existing
physical ledger, pins, admission and terminal cleanup. Build/promotion waits run
outside the cache mutex; same-key callers retain singleflight and generic dense
pointer identity. These are policy charges, excluding the documented temporary
build/allocator limits, not a process-memory ceiling. This private layout is not
a persisted codec or a new residency service. Reconstruction adds worker CPU
work; enable the option when retained-memory savings justify that cost. See the
[ownership design](../roadmaps/streamed-rendering-p1b.md).

### Streamed terrain registration

Terrain workers build geometry, bounds and a separate registration copy. Jobs
captured with managed rendering also prepare an independent first renderer copy,
retaining its fresh structural dirtiness. Pending admission charges all owned
maps. Main commits adopt only without a current backing removal; otherwise they
keep the ordinary build/removal/defensive path. Terrain backing keeps its existing
behavior. These editable assets are not interned in the prepared-geometry cache.

The asset server owns the renderer candidate under the exact adopted ID/source
until first bridge admission or deletion. Admission detaches it once and compares
all current values preserved by `XBrickMap.Copy()`, including raw payload edits,
cached bounds by float bits, explicit revision membership and auxiliary bytes.
Only fields reset by `Copy()` are ignored. Malformed pointers and nonnil empty
auxiliary slices retain ordinary copy semantics. Changed content or sharing scope
uses current copying/sharing; component source replacement leaves the unused
candidate with its original asset. Later source edits retain existing behavior.
CPU-only jobs, late renderer installation and other registration paths use
ordinary renderer copying.

`AssetServer.PreparedVoxelRendererCopyStats()` reports retained candidate
`Entries`/`Bytes` and cumulative successful `Adoptions` without traversal. Bytes
exclude live source/runtime geometry and temporary validation. There is no new
byte ceiling; unused candidates remain charged until admission/deletion.

The runtime records each adopted terrain asset's exact ID and registering server
before entity flush/hooks. Normal unload releases it after persistence/removal;
successful Stop also releases partial commits absent from `LoadedChunks`. Failed
Stop retains ownership. Deletion unregisters IDs without clearing maps held by
renderer/physics. Eager and fallback registrations keep their existing lifetime.
`PreparedGeometryAssetAdoptions` includes actual terrain transfers as well as
imported full/proxy transfers. Backing setup and a full candidate validation read
remain main-thread work; this does not bound all work in a large chunk.

### Streamed voxel-object snapshot registration

Snapshot workers prepare geometry, bounds and an independent single-use
registration copy per object. Pending admission charges both maps. Main commits
can adopt the copy when the snapshot definition matches the worker capture:
schema version and ordered voxel coordinates/values. Resumable placements still
reread the current snapshot, preserving same-path edits, missing-file errors and
removed overrides. Changed/new content uses ordinary reconstruction; legacy
synchronous commits retain their captured-snapshot behavior.

Each adopted asset has its own editable map and exact entity/server/ID lease,
recorded before flush/hooks. Durable unload and successful Stop release it;
failed persistence retains ownership, including partial commits. Unused handles
release with their prepared envelope. Fallback/eager assets retain existing
lifetimes. `PreparedGeometryAssetAdoptions` includes snapshot transfers. Reading,
comparison, spawning and publication remain within the atomic placement unit.

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

Decoded pressure selection indexes only unpinned entries by last load/hit recency.
Final scope release preserves that recency; it does not refresh the entry as
newest. Shared leases stay protected until the last release. Loader stats
`EvictionCandidateVisits` and runtime `DecodedContentCacheEvictionCandidateVisits`
count pressure victims; all-pinned pressure, reads, no-pressure maintenance and
explicit Clear add no candidate visits. Decode and graph estimation remain
potentially large work outside this selection bound.
`Misses` counts requests that miss the warm cache, including singleflight
waiters; `LoadWaits` counts requests joining an existing decode.
Successful Stop clears runtime-created loader ownership and preserves supplied
loader users. See [S2b](../roadmaps/streamed-rendering-s2b.md) for defaults,
pending-result admission and accounting limits.

`RuntimeContentLoaderOptions.ImportedWorldCodec` optionally borrows a compiled
imported-chunk codec. The constructor fixes that profile on the shared owner;
scopes inherit it and changing profiles requires a new loader. Existing keys,
singleflight and byte charges remain. Nil uses the dictionary-free default;
legacy JSON/RLE loading ignores the profile. Keep the codec alive through
outstanding jobs/scopes. Clear, scope Close and runtime Stop never close it.
Codec working storage remains outside decoded-residency estimates. Inject this
owner through the existing `StreamedLevelRuntimeConfig.Loader` field.
Compiled `EmbeddedAux` records belong to the decoded chunk graph and share its
charge and leases. Prepared/live geometry copies own their bytes; selecting the
embedded layer creates no separate sidecar cache entry. See
[compiled frames](../content/compiled-voxels.md#imported-embedded-normals).

`RuntimeContentLoaderOptions.CompiledAssetCodec` separately fixes the borrowed
profile for explicit compiled ordinary headers and shapes. `LoadCompiledAssetHeader`
and `LoadCompiledAssetShape` return shared immutable definitions and frame `Info`
by value. They use the same scoped cache, singleflight, decoded storage charge
and eviction rules. Shapes retain owned canonical bricks without voxel-record
expansion. Header loading does not follow references or register geometry.
Rejected definitions release their scope lease through the existing ownership
boundary; other scopes remain protected. Nil loaders use direct bounded default
IO. Failure returns nil data and zero `Info`, without a retained cache entry.
Clear and scope Close never close the codec. Accepted warm hits may survive codec
closure; a fresh compiled decode requires an open codec. Existing `LoadAsset`
remains strict authoring JSON and ignores this profile. Runtime adoption must
verify header/frame links before geometry publication.

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
