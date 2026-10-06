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
Static palettes can also certify immutable GPU sharing; mutable per-instance rows
remain independent. See [material ownership](../renderer/runtime.md#immutable-gpu-material-blocks).

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

Authored sphere, cube, cone, pyramid, cylinder, Z-axis capsule and ramp
constructors share pure private model builders with the offline preparation
boundary. Each build owns its voxel slice and preserves declared dimensions,
voxel order, fractional truncation, color index and empty-model conventions.
Public constructors still register through `CreateVoxelGeometry(..., 1.0)`;
frame and Y-axis capsule generation remain separate.

Authored material and procedural palettes also have pure private data builders.
VOX palette construction preserves the original colors/materials and derives
surface facts from original unscaled samples. Its materials slice and property
maps remain borrowed, matching public creators; normalized surface tags own their
storage. Offline callers must own or copy VOX inputs before publication. Public
wrappers retain their existing registration APIs, key domains, errors and
procedural nil-server precedence.

### Textures

Texture creation helpers live in `asset_texture.go`:

- `CreateTexture(filename)`
  - decodes a PNG and stores it as a 2D RGBA texture asset
- `CreateTextureFromTexels(...)`
  - registers raw texture data directly
- `CreateVoxelBasedTexture(...)`
  - builds a 3D texture from a voxel model plus palette

#### Streamed emitter textures (P5o)

Streamed legacy and compiled placements prepare emitter PNGs on workers when
the runtime starts with an asset server, including CPU-only streaming. Each
chunk owns one decoded source per clean absolute filename and an independent
publication copy for every placement/emitter pair. Main-thread emitter spawning
transfers that copy into a fresh texture ID. Pixels, premultiplied RGBA conversion,
dimensions, format, emitter settings and hierarchy preserve direct spawning.
Disabled emitters still prepare their textures. Legacy paths retain cwd-relative
semantics; compiled references use their resolved absolute paths.

Bindings require the selected packet domain, exact definition identity, emitter
ID and texture path. A changed selection or definition uses direct spawning;
consumed or released matching handles fail rather than rebuild. Direct prepared
and selected-part APIs retain delayed texture loading, `CreateTexture` retains
its PNG-error panic, and `CreateTextureFromTexels` still borrows caller pixels.
A runtime started without an asset server skips worker PNG access; a later
server uses the direct path. Streamed PNG failures instead return preparation
errors before live publication, validating alpha before each emitter's file access.

Pending admission charges decoded sources once, handle metadata and every future
publication copy before workers build those copies. Transfers remove pixels from
the envelope's actual charge; reserved credit stays conservative until final
drain. Cancellation, error, pressure retry, stale results and Stop release unused
storage idempotently. Published textures retain ordinary global lifetime across
unload and Stop. PNG decode/conversion remains atomic and precedes reservation;
this accounting excludes allocator/map slack and does not bound total process
memory. GPU atlas selection and upload retain the existing renderer path.

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

### Legacy ordinary worker preparation (P5l)

Streamed expanded `.gkasset` placements prepare owned CPU packets on workers,
independently of renderer presence or `EnableManagedPreparedAssets`. Workers
clone the cached definition before validation and normalization, resolve
animations, and build group, inline shape, procedural, VOX model and scene-node
parts without publishing `AssetServer` IDs or ECS entities. Assets with
`Runtime.CollapseVoxelParts` also retain expanded fallback data and use the
[worker collapse contract](#authored-voxel-collapse-reuse).
Direct preparation, eager spawning and startup NPC preparation are unchanged.

Packets preserve the public ordinary geometry and palette cache namespaces,
including the exact resolved document/source path spelling. Two spellings of
one physical document can therefore produce different VOX keys. Inline shape
keys and their key-valued `SourcePath` remain unchanged. VOX/procedural geometry
retains its complete scaled `VoxModel`, declared bounds and brick metadata;
VOX material override facts still use the original unscaled samples.

Each unique geometry and palette owns an immutable worker source and an
independent single-use publication copy. Cold publication transfers the copy under
the server lock without source loading, geometry reconstruction, key serialization
or another full copy. Warm commit drains unused copies and preserves existing
mutable geometry, palette data, bounds and IDs. Inline original-base identities
are derived from canonical worker sources for each valid lattice and only fill
missing provenance; edited warm geometry never supplies an original base.
Legacy publication grants no compiled certificate or automatic managed owner.

Repeated placements reuse a packet for the exact selected document spelling.
A changed selection uses the existing selected loader. If public deletion
removes an adopted shared ID between publications, recovery may create another
independent publication copy from the packet source, without rereading files.
Entity ownership, animation bindings, shadows, callbacks and rollback retain
the existing prepared spawning contracts. Published IDs keep ordinary global
lifetime across chunk unload and Stop.

Pending admission conservatively charges owned definitions, animations, keys, model rows,
geometry sources, palettes and independent registration storage, including
retained handle metadata after transfer. Cancellation checks surround loads
and whole-part builds; individual decoders, animation resolution and builders
remain atomic. Error, pressure retry, cancellation, stale results, partial Stop
and completion release unused handles and loader pins idempotently. Packet
construction precedes full-worker final-geometry reservation, so its temporary
allocations remain outside the pending ceiling and concurrency is bounded by
the existing worker count. Packet aliases are charged once; separate scalar
graph estimates may count shared string backing more than once. Accounting
excludes allocator and map bucket slack and is not a total process-memory bound.
Selected-path resolution retains its filesystem metadata checks. ECS spawning
remains main-thread work; emitter PNG preparation follows the
[streamed texture contract](#streamed-emitter-textures-p5o).

### Compiled ordinary asset preparation

`LoadAndPrepareAuthoredAsset` and `LoadAndSpawnAuthoredAsset` explicitly select
compiled input for exact lowercase `.gkassetc` (inline header) or `.gkmodelassetc`
(model header) suffixes. Other paths retain
legacy JSON behavior, and corrupt selected compiled input never falls back.
Compiled preparation returns the existing `PreparedAuthoredAsset`; prepared
spawning keeps the existing hierarchy, material, animation and ECS contracts.
Ordinary direct and expanded level placements use the same path, including
streamed commits and their existing ownership callbacks, shadows and rollback.
Streamed workers prepare compiled ordinary CPU packets per resolved asset path.
Main-thread placement commits publish their geometry and palettes. Expanded legacy
JSON placements use the [legacy worker preparation contract](#legacy-ordinary-worker-preparation-p5l).
Direct level and streamed
startup NPCs use compiled preparation with the existing multipart asset root under
the NPC entity. Health, animation bindings and creation-before-load-error behavior
are unchanged. Moving brushes, chargers, breakables and pickups select only the
first authored part, including a group that produces no model. Compiled inputs
verify the complete closure before registering that part alone; unused parts
never publish geometry or palettes. Corner pivots and gameplay components remain
unchanged. Pickups tolerate a missing selected input, but a present compiled
header with missing or corrupt dependencies fails.

Preparation verifies every referenced shape's frame identity/sizes, effective
lattice and original base identity, and resolves animations before publishing
geometry. Every declared LOD reference also loads and verifies its frame identity,
sizes and source/reduction binding, followed by exact consistency against the
authenticated source. This includes unused parts and metadata-only preparation.
Session-local authenticated identity pairs reuse only the source coverage proof;
every physical reference still loads and checks independently. Cancellation and
origin-scope checks surround derivative loads and proofs, including memo hits.
Failure releases only provisional child pins and publishes no geometry or palettes.
Selected-header load failures alone retain the missing-input classification.
Consumers return authoritative level-0 geometry; explicitly declared derivatives
can additionally populate private ordinary asset availability. Initial compiled
preparation uses default E2 logical base limits.
Larger custom-profile frames remain supported by typed IO but need separate
runtime admission work; legacy authored preparation keeps its existing limits.
Temporary independent loader scopes protect previously accepted caller pins and
reject a closed originating scope. Accepted decoded frames can still be verified
after their borrowed codec closes.

Prepared metadata is an owned copy of the immutable cached header. Compiled
animation-set references resolve relative to the header, and rig references
relative to the selected set, without cwd fallback. Both require portable
relative paths. Emitter texture paths become absolute in the prepared copy;
direct texture decoding remains unchanged, while streamed placements follow the
[worker texture contract](#streamed-emitter-textures-p5o). Authoring sources are unnecessary.

Canonical bricks already include `ModelScale`; dense construction does not
resample or form voxel JSON cache keys. After typed/profile validation, compiled
conversion fills fresh sectors and bricks directly, preserving signed coordinates,
packed rank, primary values, material flags, occupied bounds and ordered revision
provenance. It owns all mutable storage and finishes clean. Construction skips
transient edit halos because no auxiliary normals or live neighbors exist yet;
subsequent public edits retain normal invalidation and dirty propagation.
Existing `AssetServer` geometry storage shares a namespaced C1 content identity, including lattice, across compiled
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


Private compiled palette preparation builds the existing authored tables without
publication and seals the exact existing full JSON cache key. Registration owns
an independent copy of all nested maps, slices and UV-scroll pointers, preserving
concrete scalar material properties and nil/empty distinctions. Unsupported
mutable property values and JSON encoding failures are rejected at this private
boundary. Public `CreateVoxelPaletteAsset` keeps its existing alias and stale-key
behavior; the private path uses the same ordinary palette namespace.

A live registration validates its sealed key and exact source before cold
transfer or warm drain. Cold publication transfers prepared storage under the
server lock without another deep copy. Warm reuse preserves the existing mutable
palette. Consumed or released handles clear source/copy references and permit
only sealed-key warm reuse, never cold publication. Callers prove the immutable
source/key. Aliased release is idempotent and never deletes adopted palettes.
Copy charge becomes zero after transfer/release; pending envelopes must retain
charge for sealed JSON keys and handle metadata until drain. Publication-copy
accounting specializes the constructor-owned clone: mutable descendants are
independent, while string backing, length and concrete type determine shared
storage. Its byte estimate matches the general decoded-storage estimator; map
slack and allocator overhead remain excluded.

These IDs retain ordinary `AssetServer` lifetime. Packet release never deletes
adopted geometry, and imported prepared-cache eviction/leases do not own these
IDs. If public deletion removes a shared ID after its packet registration was
consumed, publication rebuilds an independent copy from the owned packet source
through the same atomic adoption boundary. Conflicting warm provenance still
fails; publication never rereads source or frame files.

Private CPU packets own metadata, resolved animations and unique dense sources
with separate single-use registration copies. Duplicate C1 identities share one
packet shape. Ordered part bindings map to palettes deduplicated by the exact
existing full JSON key. Workers own each immutable palette source and separate
publication copy; main-thread publication adopts these copies without palette
construction or key serialization. Complete shape and animation verification
precedes geometry and palette construction. Child decoded scopes close
after preparation; caller pins survive failures and cancellation. No decoded
definitions or published asset IDs escape into the packet. Group-only assets
produce packets without geometry or palettes.

Model packets use the same envelope and registrations after complete mixed-closure
verification. Model CIDs include declared dimensions; global keys use
`compiled-asset-model:` rather than the inline namespace. Cold adoption sets
`VoxModel.SizeX/Y/Z` without raw sample rows and uses zero minimum/declared maximum
when any dimension is nonzero. All-zero dimensions keep occupied bounds. Full
primary geometry remains collision/edit authority. Warm geometry, palette and
bounds edits survive reuse. Model base provenance uses its exact model raster
version and does not qualify inline E2 edits. Model LOD remains unsupported.
Static baked palettes own material maps and surface tag slices, with an empty
runtime SourcePath; original authoring paths are provenance only. Existing palette
keys and single-use publication copies retain their ownership rules. Pending
charges include model dimensions and retain each source/handle once. Direct
preparation/spawning, level placements/NPCs, first-part consumers and streamed
worker commits select model headers explicitly. First-part consumers authenticate
the whole closure before publishing only the selected part; group-first and nil
AssetServer behavior remains unchanged. Model source kinds retain center pivots;
inline parts retain custom pivots and declared inline LOD intent. Missing input
headers keep existing pickup tolerance; missing dependencies remain errors.

Cancellation is cooperative before header/shape loads and animation resolution,
between frames, whole-shape builds and palette preparation, and at completion; it does not interrupt
codec, animation-resolver or dense-builder internals. Failed
or canceled preparation releases built handles and checks the originating scope
before returning usable output. Aliased packet release is idempotent. Metadata
and source storage remain owned by the envelope until drain.

Existing pending admission charges packet metadata, unique geometry/palette
sources and registration storage once, including retained palette keys and
handle metadata after publication copies are consumed; repeated placements reuse one packet for a selected
resolved path. Deferred, canceled, stale, failed and completed results release
unused handles through existing result cleanup. A packet must match the current
canonical selected path; an unrelated packet uses the existing selected loader.
Placement publication follows
current generation, deletion and movement checks. Latest object overrides still
resolve at the placement commit, and existing callbacks, rollback and Stop own
every created entity. Palette/texture publication stays on the main thread;
normal result release never evicts ordinary global geometry or palettes. A
consumed palette copy can be rebuilt from its immutable packet source only when
its ordinary key is absent. Stale existing keys still fail through
the same adoption boundary; no source or frame reread occurs.

### Compiled LOD source validation

The private `validateCompiledAssetLODSource` boundary accepts structurally valid
C1-decoded derivative geometry and an authenticated `verifiedCompiledAssetShape`
borrowed within its verification session. It matches ordinary shape ContentID,
lattice, actual source count, occupied bounds and sole value. Occupied-bit coverage
in both directions proves exact zero-anchored occupancy OR, including a source
witness among eight fine cells for every coarse cell. Signed coordinates use
`int64` intermediates. Sparse geometry never triggers a bounding-volume scan.

Validation borrows immutable inputs, retains no geometry and builds only temporary
brick indexes. It performs no IO, rehash, reduction rebuild or runtime publication.
Common compiled preparation invokes this proof before any runtime publication.
Derivative frames remain borrowed within the existing independent child scope;
no cached definition escapes into prepared assets or CPU packets. For declared verified derivatives, worker packets retain exact coarse geometry
and separate private immutable full/coarse proof baselines. Full baselines share
only by authenticated source identity within a packet; derivative sources,
registrations and proofs share by derivative identity. Mutable source and
registration maps never alias proof storage or decoded frames. Construction
occurs before the verification scope closes; it never regenerates reduction.
Part-to-derivative membership remains per part even when full geometry is shared.
Legacy and nonLOD packets allocate no derivative tables or baselines.

Packet release drains coarse registration copies idempotently while immutable
proof/source storage remains owned until envelope drain. Pending admission counts
unique full/coarse sources and proof maps, registration storage and retained fixed
geometry-handle metadata once; scalar proof metadata excludes volume pointers
from reflection. Consuming a handle removes its publication storage charge,
not retained source/proof or fixed handle metadata. Ordinary AssetServer adoption installs independent private full/coarse proof
copies with captured scalar retained-storage accounting; packet proof ownership
never moves implicitly. Coarse geometry uses the separate `compiled-asset-lod:`
namespace in ordinary geometry storage and receives no E2 base identity. Full
geometry keeps its original identity and CPU interaction authority. Warm full/
coarse edits remain untouched; repeated matching publication reuses the private
association. Stale keys or conflicting association identity/lattice/value fail
without replacing existing assets or proof counters. Consumed full/coarse
registrations rebuild from the private authenticated packet baseline, never from
potentially edited packet sources or reread files.

Full deletion removes its association; coarse deletion removes every association
referencing it. The surviving full or coarse asset keeps independent ordinary
lifetime. Scalar proof statistics and association changes use the AssetServer
lock; public mutable geometry is not scanned under that lock. Current-map/material
qualification remains the render owner's responsibility.

Direct and worker LOD preparation use the same owned publication boundary. The
first-part consumer verifies the complete closure but constructs/publishes only
its selected first part. Nil-server verification and nonLOD warm preparation
keep their existing paths. Prepared parts retain explicit derivative membership
only for declared eligible parts; geometry availability shared by content identity
never grants opt-in to another part or input. Returned model IDs remain full.
Prepared part spawning attaches a private ephemeral ECS intent with the declared
full/coarse asset pair before ownership callbacks and command flushes. Asset
roots, groups, nondeclared parts and collapsed output receive no individual-part
intent. Full model/palette/pivot/resolution and hierarchy remain unchanged.
Overrides retain the original declared pair; they do not establish new opt-in or
own proof storage. Ordinary entity/stream cleanup owns only the entity, not its
shared assets. The render owner must qualify the current effective model against
this construction-relative intent.

The first-part loader retains selected membership through a private extended
result; its existing tuple wrapper still returns full model/palette/resolution.
Moving-brush, charger, breakable and pickup spawns attach only selected declared
intent. Pickup tolerance still applies only to a missing selected input, never a
missing dependency of a present compiled header. The renderer consumes intent
through [current qualification and staged handoff](../renderer/runtime.md#authoritative-geometry-and-render-representations);
intent alone certifies neither current geometry nor GPU readiness.
It qualifies the original decoded source only; exposed or edited warm geometry,
current material opacity and GPU readiness require their own runtime checks.

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
`AssetServer.AuthoredVoxelCollapseStats()` reports cumulative `Builds` (live cold
collapse attempts, including worker-prepared publication), `Hits` (successful
validated warm reuse) and `WorkerAdoptions` (independent worker composite copies
transferred into cold global entries). Workers and discarded packets do not
increment live counters. Reads are scalar and nil-server reads return zero;
these counters do not measure frame time.

Cold authored collapse and level-brush composition stream accepted rasterized
writes through [ordered edits](../renderer/editing.md#ordered-edit-streams), once
per part. Source/sample order, transformed voxel-center tests, epsilon, material
assignment and add/subtract order are unchanged. Validation precedes writes;
material flags finalize before the next part. Existing sample arrays remain,
without an additional write list or a change to warm cache authority.

P5m streamed legacy packets prepare a canonical composite and an independent
single-use publication copy on workers, without live assets, temporary ECS or
GPU access. Pure hierarchy resolution uses the engine's asset-local parent
position/rotation/scale composition, including forward parent references, then
applies each part's existing collapse pivot. Rasterization retains the public
collapse ordering, sample and palette semantics and the exact document-spelled
composite cache key. Expanded packet data remains available for automatic
ineligibility, including animated assets.

Cold adoption requires cold publication of every input geometry and palette,
and a cold composite entry. Warm keys or pointer equality cannot certify raw
mutable inputs. Warm inputs or an existing composite use current live prepared
part IDs for eligibility, validation and baking/reuse, without rereading VOX
or animation sources. This preserves edited model rows, maps, nested palettes
and cached composites; cold rebuilding from warm inputs remains main-thread
work. Direct public legacy collapse behavior is unchanged. Faster warm
preparation requires a separate certification or immutable capture contract.

P5n adds explicit [schema-3 inline compiled collapse](../content/compiled-voxels.md#compiled-ordinary-asset-headers).
Full streamed packets use the same pure hierarchy and ordered bake rules, with
independent composite source, publication geometry and output palette. Their
private composite key binds ordered authenticated shape identities and lattice,
asset metadata and the clean absolute document path. Relative/absolute aliases
share this domain; changing referenced geometry at the same path changes the key.
The public legacy collapse key remains unchanged.

Compiled publication holds one AssetServer lock across source geometry, palettes,
LOD associations and candidate acceptance. Only actual cold input transfers and
a cold composite entry qualify worker adoption; warm inputs use their current
published IDs for live validation/baking or validated composite reuse, without
compiled frame or header rereads. Direct public preparation creates no worker
candidate and counts no `WorkerAdoptions`; it retains published IDs and the key,
so edits between preparation and spawning remain visible. Selected-first-part
consumers verify the entire closure but never prepare a whole-asset composite.
Declared inline LODs retain full-resolution authority and intent only on expanded
parts; collapsed output receives no part LOD intent, edit-base identity or compiled
certificate. Model-header collapse remains unsupported.

Both legacy and compiled packets charge candidate geometry, independent publication
storage, palette, keys and metadata through their existing conservative pending
owner and idempotent cleanup.
Unused candidates drain on warm fallback, pressure, cancellation, stale results,
errors and Stop. Published composites retain ordinary global lifetime and do
not acquire managed ownership or per-part persistence identity. Construction
and rasterization remain atomic worker jobs before final pending admission;
no aggregate construction-memory or frame-time bound is implied.

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

<a id="worker-prepared-ordinary-managed-assets-s1n"></a>

### Worker-prepared ordinary managed assets (S1n/S1p/S1q)

Games can install the corresponding GPU policies through
`VoxelRtModule.StreamingConfig` and `DefaultVoxelRtStreamingConfig()`; see
[streaming policy installation](../renderer/runtime.md#streaming-policy-installation).
The renderer configuration and `EnableManagedPreparedAssets` remain separate
opt-ins. A nil renderer configuration preserves the manager's existing defaults.

`StreamedLevelRuntimeConfig.EnableManagedPreparedAssets` opts compiled ordinary
placements into worker-prepared managed ownership. The zero value keeps existing
publication. Each eligible placement/part owns a single-use sealed owner,
independent CPU authority snapshot and independent first renderer map. Workers
publish neither asset IDs nor ECS/GPU state. Their pending reservation includes
these inputs, metadata and construction peak before creating owners; cancellation,
stale results and unused handles drain idempotently. Existing shared pending-byte
policy, including its sole oversized-result exception, remains unchanged.

Verified cold compiled registration creates a private certificate for its global
ID, content namespace, lattice, base identity, dimensions, bounds and registered
source. Untouched registrations retain eligibility across siblings, chunks and
runtime Stop/Start. Actual managed transfer rechecks that certificate and the
current key/ID and source metadata under the AssetServer lock; it does not trust
an earlier publication result. Each placement still receives independent owners,
authority and renderer storage.

Every public or internal mutable geometry borrow permanently revokes that ID's
certificate, even for read-only use. Direct full/coarse LOD binding also revokes
eligibility. Warm preparation never recreates a revoked certificate from current
content, provenance, pointer or revision. Borrowed globals keep their current
mutable geometry and ordinary compatibility path; palette IDs and mutable palette
storage retain their existing semantics. Deletion removes the certificate; a
fresh verified ID may qualify without sharing storage with old aliases. Legacy
authored inputs, declared LODs and collapsed composites retain
their existing preparation. Compiled restored snapshots use the independent
[snapshot path below](#worker-prepared-restored-snapshots-s1q). This
consumer does not migrate terrain, imported-world, retained, planet or backing
ownership.

The compiled publication APIs' warm metadata behavior is unchanged: they preserve
the registered global asset and its mutable values. The S1p certificate only
decides whether an independent worker candidate can supply a managed override.
Certificate metadata follows the ordinary global asset lifetime; it is not a
separate bounded cache or an aggregate live-memory limit.

Main-thread adoption transfers prepared authority into an entity-owned
`OverrideGeometry` before spawn callbacks. It preserves the ordinary shared ID,
palettes, source lattice and authored pivot bounds. Provisional overrides are
removed unless the exact chunk lease claims them. Qualified shapes retain verified
construction provenance and persistence counts/bytes from worker validation;
models keep existing full persistence fallback. Unload, failed partial commits
and Stop release owned overrides without revoking ordinary global geometry.

The renderer consumes the prepared map once under exact sealed-entry, source and
generation qualification, without a whole-map comparison or copy. Its existing
`PreparedVoxelRendererCopyStats` accounts for retained candidates and transfers.
Tracked edits before attachment or public exposure discard obsolete candidates
and preserve current authority through the existing compatibility path. If a
placement hook installs unsupported ownership, the exact automatically adopted
override becomes ordinary authority while preserving its edits, ID, bounds and
lease; explicit hook replacement overrides are untouched.

Managed edit callbacks feed the existing bounded renderer pipeline. CPU picking,
collision and saves continue to use authority. This flag does not enable GPU
admission, managed frame service or bounded lookup budgets. Those remain separate
opt-in policies; see [managed publication](../renderer/runtime.md#managed-gpu-publication-s1l14)
and [lookup publication](../renderer/runtime.md#bounded-sector-lookup-publication-s1m).
Worker construction is still one finite job. Metadata publication, placement hooks
and compatibility copies remain main-thread work. Pending charges end at transfer;
live CPU asset storage has its existing lifetime and is not an aggregate heap cap.

<a id="worker-prepared-restored-snapshots-s1q"></a>

### Worker-prepared restored snapshots (S1q)

The same `EnableManagedPreparedAssets` flag also prepares private managed owners
for worker-captured snapshots on compiled ordinary placements. Each item owns a
sealed owner, independent CPU authority and first renderer derivative. Restored
content is independent of the ordinary shared geometry certificate: a borrowed
or edited global source cannot invalidate its private restored data.

Schema-2 `base_delta` and schema-3 `hybrid_delta` use the authenticated compiled
shape as the original construction base and the resolved snapshot as current
geometry. Original identity, lattice, counts and decoded bytes survive later
edits and reverts. Full and legacy v1 snapshots receive unbound tracking and keep
full-save fallback. Snapshot bounds remain the restored bounds. Existing typed
payload origin restrictions, declared LOD/collapse exclusions and special engine
geometry ownership remain unchanged.

Adoption requires the exact item tuple, selected asset path, effective lattice
and chunk membership, plus the current fully validated payload identity and
metadata. Unchanged identities reuse worker resolution without canonical map
reconstruction. Changed, removed or late snapshots use the existing current
resolver and compatibility registration, including equal geometry with changed
full/delta metadata. This freshness check also applies to whole-chunk commits
when a worker-prepared restored candidate is present. Legacy v1 uses ordered
record equality and never infers original-base provenance.

Initial pending admission reserves every ordinary source/registration and terrain
component before dense construction. Before each restored owner is built,
conservative constructor preflight adds its complete owner/authority/renderer
construction peak to that reservation. Bound deltas use dual-source preflight,
including original/current geometry, assignment and
changed-brick tracking. Future component reservations remain held until final
envelope reconciliation. Pressure refusal releases the whole result for retry;
overflow fails. Release is idempotent, and unload, partial rollback
and Stop own the exact adopted lease. Hooks may demote automatic ownership while
preserving current content and its lease; replacement hooks retire only the old
automatic owner. Renderer derivative consumption and GPU policy opt-ins follow
the ordinary managed contracts above.

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

`RuntimeContentLoader` bounds warm decoded definitions across all supported content
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
profile for explicit compiled ordinary headers, shapes, models and LOD derivatives.
`LoadCompiledAssetHeader`, `LoadCompiledAssetShape` and `LoadCompiledAssetLOD`
and `LoadCompiledAssetModelHeader` / `LoadCompiledAssetModel` return shared
immutable definitions and frame `Info` by value. They use the same scoped cache, singleflight, decoded storage charge
and eviction rules. Shapes retain owned canonical bricks without voxel-record
expansion. Derivatives retain only actual coarse bricks and source metadata,
without expanding the source count into geometry. Typed kinds have separate keys
even at the same path. Warm entries remain fixed until eviction or Clear; file
replacement does not refresh a warm entry. Model frames retain declared dimensions
and canonical bricks. Header/model/derivative loading does not follow references or
register geometry. Derivative reads prove frame structure only;
[exact source validation](#compiled-lod-source-validation) and current runtime
source/material qualification remain separate boundaries.
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
