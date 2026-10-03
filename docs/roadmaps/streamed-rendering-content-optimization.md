# Streamed Rendering and Content Optimization Proposals

Date: 2026-10-03. Status: staged implementation; S1a–S1h, S2a–S2k, S3a–S3v, S4a–S4c and P5a–P5d complete. S1/S2/S3/P5 remain partial; other sections are proposals.

Source: [rusty-voxelrt roadmap](/Users/ddevidch/code/rust/rusty-voxelrt/docs/roadmaps/OPEN_WORLD_STREAMED_RENDERING.md). Optimization proposals only; measurement phase/status excluded. Gekko inspected at `1f7a281`, including working-tree content. Rust targets/ratios are not Gekko predictions.

## 1. Recommendation and architectural boundary

Apply compact bricks, packed GPU records, immutable sharing, bounded streaming, compiled assets and brick deltas. Extend existing Gekko prerequisites.

Preserve [island streaming architecture](../content/island-streaming.md): separate terrain/POI layers, layer resolution, height collision outside voxel replacements, camera-relative rendering, ECS lifetime and main-thread commits. Shared codec/scheduler needs no global voxel lattice.

Two Rust choices do not transfer: Gekko retains fitted encoded normals, not 6-bit neighbor normals; island plan retains manifests/payload compatibility. Deleting all legacy readers is not approved migration policy.

Owners: renderer storage/upload and streamed runtime. Consumers: physics, navigation, assets, editor, importers, ActionGame and SpaceSim. Confidence: high for applicability/structure; medium for gains/formats. Individual designs required; no terrain architecture change approved.

## 2. Current Gekko foundations and gaps

- [CPU brick storage](../../voxelrt/rt/volume/xbrickmap.go): 8³ bricks, 32³ sectors, 2³ micro masks. `Sector.PackedBricks` already uses popcount indexing. `Brick.Payload` still stores 512 bytes for every allocated brick, including uniform bricks.
- [GPU upload](../../voxelrt/rt/gpu/manager_voxel.go): each sector reserves 64 brick records. Shader indexing uses `sector.brick_table_index + brick_idx_local`, not CPU popcount indexing. Mixed bricks use paged `R8Uint` atlas; uniform bricks already skip color payload.
- [Auxiliary data](../../voxelrt/rt/volume/voxel_aux.go): 64 bytes of occupancy plus 1,024 bytes of encoded normals per brick. Normal encoding includes validity and two-sided lighting. [G-buffer traversal](../../voxelrt/rt/shaders/gbuffer.wgsl) already rejects empty voxels before loading their material.
- [Streaming](../../streamed_level_runtime.go): worker preparation, generation checks, indexed chunk maps, proxy/full handoff, and separate collision/destruction interest already exist. [S3a selection](../../streamed_level_selection.go) caches idle demand and updates cube differences. Commit limits count chunks and elapsed time; global GPU content budgets are implemented in S1b.
- [Caching](../../runtime_content_loader.go): S2b adds decoded-content byte budgets, scoped leases, in-flight suppression, and shared pending-result admission. [Prepared geometry](../../streamed_level_geometry_cache.go) has S2a byte accounting and pinned users. S2c bounds CPU material tables; S2d bounds retained assigned GPU geometry slots. Live users/sole oversized pending results expose pressure exceptions; physical GPU capacity and other owners remain separate. These are not total process memory limits.
- [Imported payloads](../../content/imported_world_chunk_binary.go): binary RLE already exists, with JSON metadata and SHA-256 verification. Decoding expands into voxel records, then [spawning](../../imported_world_spawn.go) builds `XBrickMap` through per-voxel writes. Terrain chunks remain JSON.
- [Assets](../../assets/voxel_assets.go): geometry and palette assets are separate; shared geometry and copy-on-edit already exist. There is no equivalent to Rust's compiled fragment package in this path. [Geometry registration](../../asset_vox_model.go) deep-copies prepared maps. [Inline shapes](../../asset_voxel_shape.go) serialize geometry into JSON cache key. [Entity LOD assets](../../entity_lod_runtime_assets.go) already generate simplified geometry and impostors at runtime; offline compilation can remove that work.
- [Physics](../../mod_vox_physics.go): collision already reads voxel geometry; asset grids are shared. `CopyChangedSectors` supplies immutable changed-sector snapshots to physics and [navigation](../../navigation_graph_runtime.go). There is no equivalent bulk JSON collision-box package to eliminate.
- [Destruction](../../mod_destruction.go): queue already holds sphere events, grouped per entity. [Sphere application](../../voxelrt/rt/volume/primitives.go) still calls `SetVoxel` per voxel. Connectivity already uses brick components, but scans whole map when splitting runs.
- [Scene sync](../../mod_voxelrt_client_systems.go): transform/material comparisons and conditional BVH rebuilds already exist. ECS gathering still scans entities. [Shadow scheduling](../../voxelrt/rt/gpu/shadow_schedule.go) already budgets local lights, but global scene/upload revisions invalidate unrelated layers. [Directional cascades](../../voxelrt/rt/gpu/shadow_metadata.go) already follow camera slices and snap to texels.

## 3. Transfer map

| Rust proposals | Gekko disposition | Changes below |
| --- | --- | --- |
| Q1 | Optional quality setting; coordinate mapping needs broader feature audit | R1 |
| Q2 | Adapt traversal bounds; terrain-first only after separate accelerator exists | W1, W3 |
| Q3, Q4 | Extend existing change checks and queues; preserve ECS stage semantics | S3 |
| Q5 | No CA side table to move; compact inline CPU payload instead | P1 |
| Q6 | Direct decode useful; empty skipping partly exists; no Bevy extraction bridge | P5, S2 |
| Q7, D2 | No live probe snapshot path to fix; extend existing immutable snapshots | P1, P5 |
| Q8 | Camera-focused cascades already present | R2 |
| B0 | CPU palettes partly shared; GPU materials allocated per object | P4 |
| B1 | CPU complete; GPU still reserves 64 records per sector | P2 |
| B2 | GPU dense occupancy already present | P1, P3 |
| B3, B4, B7 | Packed payload useful; retain Gekko normal encoding; atlas removal conditional | P3 |
| B5 | Compact read-only CPU storage strongly applicable | P1 |
| B6 | Do not replace fitted normals with six-neighbor normals | P3 |
| F1–F4, F6 | Adapt brick codec/compression; retain established manifest compatibility | C1 |
| F5 | Region-scoped metadata useful; binary manifest/region packs deferred by island plan | C2 |
| W1–W3 | Use accelerators per layer; existing keys already contain Y | W1 |
| W4–W6 | Use LOD and direct brick generation per layer; no unified 10 cm terrain volume | W2, W4 |
| W7 | Optional far terrain representation; preserve layer coverage | W2 |
| R1–R3 | Conservative prepass optional; extend existing shadow scheduler | R2, R3 |
| D3, D4 | Global byte budgets and incremental planning applicable now | S1–S3 |
| K1–K4 | Extend existing voxel collision/snapshots; retain height terrain collision | W1, E3 |
| K5 | Approximate far queries only through explicit API | W2 |
| E1–E3 | Event queue partly present; bulk brick application and deltas missing | E1–E3 |
| E4 | Optional removal preview after packed allocation and revision ordering | E5 |
| A0 | Definition cache already exists; suppress concurrent duplicates | S2 |
| A1–A3 | Add compiled runtime assets and shared tables; reuse residency infrastructure | C1–C3 |
| A4, A5 | Asset LOD, compact collision sources, touched-brick copy-on-edit | C3, P1, E2 |

## 4. CPU and GPU data changes

### P1. Compact immutable CPU bricks

Add compact storage behind voxel read/iteration/raycast/edit APIs; keep dense editable representation. Compact fields: coordinates, 512-bit occupancy, material mode, packed values and optional packed normals. Retain sector masks/packed indexing.

Uniform: one material; mixed: one byte per occupied voxel. Expand touched bricks only; repack at publication. Decode static chunks directly. Migrate direct `Payload` readers before storage changes.

Share immutable payloads across assets, rendering, physics and navigation. Go pointers/slices need explicit immutable ownership/copy-on-write; Rust `Arc` unnecessary. Preserve `Revision`, `SectorRevisions`, upload dirtiness and snapshots. No shared live GPU offsets.

Benefit: less static memory/allocation/copying. Risk: rank lookup may cost more on heavily edited bricks; retain dense path. Occupancy-only consumers need not retain render normals.

Owners: `volume/xbrickmap*.go`, `mod_vox_physics.go`, `navigation_graph_runtime.go`, `assets/voxel_assets.go`, imported-world preparation. Acceptance: identical authoritative voxels, material queries, raycasts, and unchanged asset instances after editing one instance.

### P2. Pack GPU brick records by sector occupancy

Replace fixed 64-record sector blocks with occupied records. Every shader uses `base + popcount(mask below brick index)`. Existing WGSL helper is starting point, not current indexing contract.

Use sector allocations with capacity classes/slack. Membership changes rebuild touched range only. Upload records before publishing base/mask together. Retire old ranges after prior submissions can no longer reference them. Never pair new mask with old packed table.

Decouple auxiliary capacity from `requiredBricks = sector slots × 64`; size by live allocation. Avoid 1,088 auxiliary bytes per potential record.

Tradeoff: shader popcount and membership relocation. Retain dense table option for highly occupied/frequently edited sectors if packing loses benefit.

Owners: `gpu/manager_voxel.go`, allocation metadata, scene bindings. Update `gbuffer.wgsl`, `shadow_map.wgsl`, `transparent_overlay.wgsl`, and `particles_sim.wgsl` together. Acceptance: identical hits across empty/new/removed bricks, correct slot reuse, and records proportional to occupied bricks.

### P3. Pack mixed materials and existing normals

Prototype paged storage-buffer pool alongside atlas. Index with `payload base + rank(occupancy, voxel index)`. Pack bytes into `u32`, extract WGSL lanes; never one `u32` per byte. Respect adapter binding-size/storage limits. [WGSL integer types](https://www.w3.org/TR/WGSL/#integer-types) define portable representation.

Pack current 16-bit normal words per occupied voxel. Preserve octahedral values, validity and two-sided bits exactly. Uniform material does not imply uniform normals. Retain micro mask for traversal; optional lane prefixes trade header bytes for fewer popcounts.

Rust 6-bit replacement changes fitted surfaces, thin walls and lighting. Mask-derived normals remain separate visual experiment. Retain full Gekko fitting halo, including extended surface fitting, not only six neighbors.

Excluding slack, sector tables and page overhead: current mixed brick `32 + 512 + 64 + 1024 = 1632` bytes; packed with `n` occupied voxels approximately `32 + 64 + 3*n`. At `n = 64`: `288` bytes. Layout estimates, not measured savings. Uniform GPU bricks already skip material payload; remaining normals are target.

Prototype both paths behind one accessor. Remove atlas only after sparse/dense visual parity and favorable traversal. Buffers may lose texture locality; reject slower default. CPU/disk compaction can ship independently.

Owners: voxel pool allocator, upload/bind groups, traversal shaders, normal builder, app pipeline layouts. Acceptance: identical normals, AO, transparency, shadows, particles, and edited seams; no stale allocation reads.

### P4. Share immutable GPU material blocks

Intern by canonical material content/semantics; one GPU block per distinct table, not per `VoxelObject`. Reuse CPU palettes/compiled IDs; remove repeated identity serialization.

Retain 256-entry local addressing. Many tables allowed; never force level into one `uint8` palette. Identity includes gameplay tags, transparency, emission and animation bindings, not color alone.

Keep animated palettes/overrides private or copy shared block on first mutation. Reference-count and safely retire blocks. Acceptance: sharing follows distinct tables; animation/recolor never changes other instances.

Owners: material allocation in `manager_voxel.go`, bridge material sync, `AssetServer`, compiled asset tables.

### P5. Build immutable upload packets on workers

Replace RLE/JSON voxel structs plus `SetVoxel` reconstruction with direct brick decode. Prepare occupancy, normals, tight bounds and uploads off main thread. Importers/terrain generators use bulk builders too.

Transfer slice ownership through worker channels; share immutable backing only. No mutable live maps, renderer allocations or decoder temporaries in snapshots. Reuse changed-sector snapshots, not whole-scene clones.

Add immutable-adoption registration. `RegisterSharedVoxelGeometryWithCacheKey` currently calls `xbm.Copy()` for worker geometry. Retain defensive copies for mutable callers; immutable prepared payloads avoid second copy.

Keep `XBrickMap`, CPU snapshots, GPU allocations and dirty queues separately owned. Targeted ownership change, not Bevy parallel ECS replacement. Workers never mutate ECS or call WebGPU.

Acceptance: runtime admission registers prepared geometry without rebuilding it voxel by voxel; no duplicate source representation remains pinned after its consumers release it.

#### P5a: Worker-owned registration copies

First prerequisite for smaller main-thread commits: imported full/proxy workers
prepare an independent registration copy, bounds and byte charge. Each result
owns a private single-use handle. Main-thread asset registration adopts that
copy; shared prepared source maps remain immutable and separate from renderer
mutation. Existing registration APIs retain defensive copying. Cache reuse drops
the unused handle. Cancellation, retries, deferred commits and Stop retain or
release its S2b pending charge with the envelope.

Files: `asset_vox_model.go`, `streamed_level_runtime.go`,
`streamed_level_geometry_cache.go`, `streamed_level_pending.go` and a focused
payload owner. Preserve cache accounting, auxiliary bytes, source geometry,
chunk publication and synchronous gameplay readiness. Workers never register
assets, access live backing/removal maps, mutate ECS or call WebGPU.

Confidence is High after ownership review; no SME alignment is needed. Use the
full separate-agent workflow. Cover actual full/proxy adoption and isolation,
warm reuse, deferred byte ownership/cancellation and the unchanged defensive
registration contract. Run focused race, engine/consumer checks and native
handoff verification. Terrain/backed removals, placements, cache ledger scans
and renderer staging remain later work; this batch does not bound all commits.

#### P5b: Worker-prepared terrain registration

Build terrain voxel geometry, bounds and an independent P5a registration copy
from immutable decoded columns on workers. S2b charges source and copy until
consumption or drain. Main commits adopt only without a current backing removal;
any live removal uses the original build/removal/defensive registration path.
Keep terrain backing construction, hooks, transforms, adjacency, object-scoped
renderer geometry and synchronous chunk publication unchanged.

Use a private prepared-asset spawn seam; preserve the public spawn definition.
Do not intern terrain assets: their OverrideGeometry can be edited in place.
A runtime owner keyed by spawned terrain entity records exact asset ID/server
before flush/hooks. Normal unload releases owned IDs after persistence; successful
Stop also releases partial commits that never reached LoadedChunks. Failed Stop
retains ownership. Deleting an asset unregisters its ID, without clearing maps
held by renderer/physics. Existing fallback asset ownership stays unchanged.

Files: `streamed_level_runtime.go`, `streamed_level_pending.go`,
`level_content_spawn.go` and the existing registration owner. Confidence is High
after independent ownership review; no human choice remains. Use separate tests
and implementation agents with independent PRE/POST reviews. Cover actual
terrain adoption/geometry/backing, current-removal fallback, deferred cancellation,
partial failure and successful/failed Stop cleanup. Run focused race, full
engine/consumer checks and a native terrain handoff. Backing setup, renderer
copies, placements and other large units remain outside this batch's bound.

#### P5c: Worker-prepared voxel-object snapshot registration

Build voxel-object snapshot geometry, bounds and an independent P5a registration
copy during existing worker preparation. Charge source and copy in the pending
envelope; release unused handles on rejection, cancellation, drain and Stop.
Adopt through a private streamed apply seam; keep public defensive registration.
Track adopted assets by entity with exact ID/server before flush. Release after
durable removal or successful Stop, including partial commits; failed persistence
retains ownership. No snapshot cache or mutable cross-entity sharing.

S1g resumable commits still reread the current authoritative snapshot. Adopt only
when that decoded definition exactly matches the captured worker definition;
changed/new content uses the current synchronous fallback, removed overrides skip
application and read errors remain errors. This also preserves externally authored
same-path replacements. Legacy synchronous commits keep their captured-snapshot
semantics. Placement, snapshot, collision publication, flush and hooks remain one
atomic unit. Removing rereads based only on path identity is deferred.

Files: `streamed_level_runtime.go`, `streamed_level_commit_transaction.go`,
`streamed_level_pending.go` and `streamed_level_registration.go`. Confidence is
High for the established P5a/P5b ownership pattern and content-equality authority
check. Use separate tests/implementation and independent PRE/POST reviews. Cover
actual adoption/isolation, current and same-path changed/removed authority, pending
ownership and exact asset lifetime through partial/failed Stop; focused race,
engine/consumer checks and native readiness/edit/reload verification. Snapshot
reading/comparison and placement spawning remain atomic main-thread work.

#### P5d: Worker-prepared first terrain renderer copy

For jobs captured with managed rendering, prepare a third independent terrain
map from finished registration geometry using exact `XBrickMap.Copy()` semantics.
Keep its fresh structure dirtiness. Atomic registration take transfers asset,
candidate and worker-calculated charge into a private asset-owned single-use
sidecar bound to exact asset ID/source. Other registration paths stay unchanged.

First bridge admission detaches the sidecar and validates the current registered
source pointer plus every value copied by `Copy()`: cached bounds by float bits,
`AABBDirty`, root/sector revisions with explicit membership, sector keys/coordinates/masks,
ordered bricks, payload/occupancy/flags/atlas fields and auxiliary bytes. Ignore
only fields `Copy()` resets. Reject malformed pointers and nonnil zero-length
auxiliary slices; live defensive copying preserves those existing semantics.
Raw pre-sync edits or changed scope discard the visited candidate and retain
current copying/sharing. Entity source replacement uses its current asset;
unused original candidates stay with their exact asset until admission/deletion.
Post-admission source behavior stays unchanged.

Pending credits charge all three maps. After adoption, the asset owns unused
candidates until first bridge admission or deletion; detach/deletion clears
references and scalar charge. Failed persistence keeps live asset ownership.
`AssetServer.PreparedVoxelRendererCopyStats()` exposes retained `Entries`, `Bytes`
and cumulative successful `Adoptions`; reads do no geometry traversal. These bytes
exclude live source/runtime geometry and temporary validation. No new byte ceiling.

Files: streamed job/registration owner, `asset_vox_model.go`, `mod_assets.go`,
bridge geometry admission and owning docs. Confidence is High after independent
design inspection; exact validation preserves public mutable access. Use separate
tests/implementation and independent PRE/POST reviews. Cover real adoption,
runtime/source/sibling isolation, raw pre-sync payload/auxiliary edits, replacement
and scope fallback, single use, pending/asset cleanup and runtime-map readiness.
Run focused race, engine/consumer checks and native terrain/edit/reload checks.
One full validation read remains; pointer/revision alone cannot replace it.
Late renderer installation, backed removals and ordinary snapshots keep fallback.

## 5. Streaming changes

### S1. Global budgets, priority queues, and renderer readiness

Finish island readiness/uploads before more detail. Global per-frame bytes and brick/record limits span all objects; S1b implements content caps. Allocation/migration/lookup remain outside cap.

Budget commits by bytes, bounded units and time. Between-chunk timer cannot bound one huge commit. Stage hidden chunks; resume uploads across frames.

Use island ticket/generation/revision readiness: geometry, materials, lookup and all required uploads. Retain parents/proxies until complete replacement cohort/group ready. Hidden-for-upload differs from excluded-from-upload.

Deterministic prepare/commit/upload/retry/retain/retire queues with stable ties/aging. Reserve collision/detail progress; proxy-first drain must not starve gameplay/detail. Atlas/pool pressure causes backpressure/eviction, never atlas-full panic.

Owners: streamed runtime, renderer bridge, `app_voxel_residency.go`, GPU upload manager. Acceptance: global limits hold for many objects; delayed/failed uploads and teleports retain valid coverage and safe collision.

Completed commit-bound step: [S1g opt-in resumable placements](streamed-rendering-s1b.md#s1g-opt-in-resumable-placement-commits),
approved 2026-10-03. Preserve default synchronous behavior; enabled managed
runtimes admit whole placement units across frames and persist all partial edits.
The bounded ready owner retains partial lifetime, exact persistence ownership
and synchronous loading. One expensive placement/hook remains unbounded.

#### S1h: Capacity planning for structural inputs

Completed renderer batch: count new sector capacity only for new/structurally dirty
maps. Preserve allocator-tail sizing, pointer deduplication and hidden uploads;
clean resident geometry contributes no sector walk during another map's arrival.
[Scope and verification](streamed-rendering-s1b.md#s1h-capacity-planning-for-structural-inputs).
The arriving map's own structural work remains potentially large.

### S2. Bound caches and worker throughput by bytes

Budget decoded content, prepared geometry, pending results, retained GPU data, normals, materials and editable patches by bytes. Bound `RuntimeContentLoader`; geometry eviction alone leaves source arrays resident.

Pin live render/collision/navigation/fallback users; evict unreferenced detail first. Count shared backing once globally; retain per-user admission costs.

Suppress per-key concurrent IO/decode/build. Normalize paths; compiled data uses content IDs. Separate bounded IO, decode/generation and navigation queues. Cancel obsolete demand; discard stale generations.

Retain explicit empty-page metadata. Empty skipping must honor placements, backing, overrides and collision interest. No-IO emptiness requires every relevant source empty.

Acceptance: total cache/pending memory stays bounded while traveling; concurrent requests reuse one result; gameplay-only chunks need no GPU admission.

#### S2f: Direct unpinned eviction order

Completed: the prepared cache maintains exact unpinned LRU order so
pressure selects one victim without rescanning every live/warm entry. Preserve
byte accounting, all acquired users and worker deferral at the true oldest asset.
[Owner and verification](streamed-rendering-s2a.md#s2f-direct-unpinned-eviction-order).
Storage graph admission/removal and other cache owners remain separate work.

#### S2g: Storage reference presence transitions

Completed: physical shared-storage accounting propagates
references only on per-kind presence changes. Repeated aliases avoid redundant
child walks; first/last ownership and cold admission remain potentially large.
[Owner and verification](streamed-rendering-s2a.md#s2g-storage-reference-presence-transitions).

#### S2h: Direct decoded-cache eviction candidates

Completed: byte-pressure selection skips pinned decoded entries.
An unpinned heap preserves last-load recency when scopes close, shared pins and
Clear/singleflight semantics. [Owner and verification](streamed-rendering-s2b.md#s2h-direct-decoded-cache-eviction-candidates).
Decode and storage estimation remain separate potentially large work.

#### S2i: Direct retained-GPU eviction candidates

Completed: retain an inactive-map heap keyed by existing usage recency. Preserve S2d
pins, assigned-slot accounting and release; pressure selects victims directly
without rebuilding/sorting all inactive candidates. Owner maintenance and stats
scans remain. [Scope and verification](streamed-rendering-s2a.md#s2i-direct-retained-gpu-eviction-candidates).

#### S2j: Direct inactive CPU material-table candidates

Completed: index inactive CPU material keys by existing recency. Preserve S2c pins,
capacity charges, deferred budget maintenance, borrowed backing and map pruning.
Pressure selects victims directly, avoiding another owner scan and full inactive
sort. Complete owner/instance pin scans remain. Use separate tests/implementation
and independent PRE/POST reviews; public candidate visits protect the work bound.
[Scope](streamed-rendering-s2a.md#s2j-direct-inactive-cpu-material-table-candidates).

#### S2k: Worker-prepared registered storage descriptions

Completed: retain eligible full/proxy worker descriptions of fresh registration copies and
transfer them into the existing physical cache ledger. Charge pending metadata,
preserve actual-object sharing/pins and use cached standalone charges only for
qualified distinct copies. Generic admissions keep existing traversal. This removes
main-thread node recapture/allocation and repeated retention union calculation;
identity installation and first/last ownership walks remain.
[Ownership decision](streamed-rendering-s2a.md#s2k-worker-prepared-registered-storage-descriptions).

### S3. Incremental selection and scene gathering

Status: partial. S3a–S3v implement selection, GPU records, ECS inventories, hierarchy reuse, core component publication, a bounded publication journal, live-validated material fingerprint reuse, one-pass linked-emitter aggregation, bounded lookup reuse, lazy normal neighbor preparation and one-pass saved object selection. Commits/designs: [delivery record](#completed-work). Contracts: [streaming docs](../content/streaming-and-worlds.md), [renderer runtime](../renderer/runtime.md), [ECS docs](../engine/ecs.md).

Gameplay and other renderer-input notifications, bounded entity worklists and incremental extraction remain S3 work. Preserve compatibility for untracked public-field writes. Hierarchy/renderer still read live values. Nonempty caches may retain peak capacity; no general byte ceiling or frame-time gain.

Cache by spatial bucket, radii, layer transform/topology and PVS. Update shells; recompute after teleports, observer/radius changes, edits and visibility. Count overlapping demand so one observer cannot evict another's content.

Stable entity/object table: extend comparisons with dirty registration, hierarchy/removal/visibility notifications. Separate camera culling/LOD from content changes.

No automatic Bevy `Changed<T>` in Gekko. Publish versions/events at mutation owners before incremental extraction. Preserve hierarchy/flush order for animation/brushes. Rebuild GPU records/BVH only for relevant changes.

Acceptance: idle observers do not rebuild residency sets; idle geometry does not rebuild records; movement and ancestor changes appear at current stage boundary.

#### S3s: Aggregate linked emitter radii per sync

Replace repeated object scans for automatic linked-light radii with one
invocation-local aggregate over requested emitter groups. Preserve maximum
world-AABB half-diagonal, explicit radii, light order and same-pass live inputs.
Update bounds only for requested groups. Retain no cross-frame cache or component
pointers; this does not authorize dirty-only extraction.

Files: `mod_voxelrt_client.go`, `mod_voxelrt_client_systems.go` and focused tests.
Confidence is High: the light sync owns current inputs and aggregation changes
no lifetime boundary. Use the routine tests-first workflow. Cover shared/distinct
groups, direct scale/link/geometry changes, removal and zero work without eligible
lights. A public last-sync object-visit counter verifies the scan bound. Run
focused bridge checks, full engine tests and affected consumer builds. No shader
or GPU allocation changes require a new native check.

#### S3t: Reuse visible object lookup preparation

`UpdateScene` currently builds terrain lookup once and planet lookup twice per
frame. Capture eligible terrain/planet rows in one live visible-object pass,
including current object indices. Reuse encoded tables only when exact scalar
inputs match. Preserve dual eligibility, ordered duplicates, hash collisions,
negative/int32 coordinates, visible reordering and nonempty empty-table headers.
Keep current `ensureBuffer` calls and GPU writes; this is CPU preparation reuse.

Retain no object/Scene/component pointers. Borrowed byte views are read-only until
the next preparation/reset. Bound all owned key and encoded-byte capacities by
`ObjectLookupCacheBudgetBytes` (default 4 MiB, unmeasured). Nonpositive budgets
disable retention; oversized or lowered-budget entries release owned storage.
Temporary preparation and existing GPU buffer ownership remain separate.

Files: GPU terrain/planet lookup helpers, `manager_scene.go`, `manager.go` and a
focused cache owner. Confidence is High after independent architecture review;
no human choice remains. Use the full workflow for cache/lookup compatibility.
Cover shader-visible lookup results/bytes, live metadata/visibility changes,
duplicates/collisions, idle reuse and capacity/bypass/reset boundaries. Public
build/visit/byte diagnostics verify work and retention. Run focused GPU checks,
full engine/consumer checks and native mixed lookup rendering. No dirty-only
extraction, shader layout change or GPU publication latch is approved here.

#### S3u: Build normal neighbor context only when needed

Keep one invocation-local lazy context for normal processing. After structural
dirty preparation, capture original dirty-brick snapshots before halo propagation.
Build the full live `Scene.Objects` neighbor maps once only for qualifying dirty
adjacency/planet halo work or actual runtime auxiliary baking. Propagation still
runs with a zero upload budget. Idle, precomputed-only and material-only work
need no context unless a dirty cross-object source requires propagation.

Preserve last-duplicate ownership, explicit adjacency over terrain fallback,
planet overlap, direct metadata writes, packed normal bytes and upload completion.
Keep the existing pure context/normal helpers. Retain no cross-frame pointers,
cache or dirty-only neighbor extraction. Public build/visit diagnostics describe
actual context construction, not all renderer scans.

Files: `manager_voxel.go`, `manager_voxel_normals.go`, the execution seam and
manager diagnostics. Confidence is High after independent architecture review;
no human choice remains. Use separate tests/implementation and independent
PRE/POST reviews for the deferred extraction boundary. Cover idle/precomputed
bypass, real same-frame seam bytes/halo propagation with a paused upload budget,
original-snapshot noncascade and live neighbor metadata/duplicate changes.
Run focused GPU checks, engine/consumer checks and native mixed lookup rendering.

#### S3v: Select saved object overrides once per job

Completed: build an invocation-local set of selected placement IDs, then enumerate world
voxel-object override keys once. Preserve the existing prefix predicate exactly:
placement ID, NUL separator and at least one trailing byte. Embedded NUL IDs,
overlapping prefixes, duplicates and original key/value authority remain valid.
No selected placements means no override enumeration. Workers still receive a
copied selection; snapshot loading, errors and commit authority stay unchanged.

Files: `streamed_level_runtime.go` and focused tests. Confidence is High: this
extends S3 invocation-local aggregation without a persistent index or new
invalidation/lifetime boundary. Use the routine tests-first workflow. Cover real
snapshot application, exclusion of unrelated broken references, selected errors,
prefix edge cases and public `VoxelOverrideSelectionKeyVisitsLastJob`. Reset that
metric each job build; count world keys visited, with zero for no placements.
Run focused streaming checks and the engine/consumer boundary. No GPU changes
require a native check. One world-key pass and selected snapshot I/O still remain.

### S4. Remove synchronous persistence from unload

Status: complete. S4a captures immutable manifests and publishes unique durable
payloads; S4b orders imported captures and preserves navigation impact. S4c adds
bounded asynchronous normal unload, dirty pins and durable acknowledgements.
[Ownership decision](streamed-rendering-s4.md).

`persistChunkOverrides` saves whole snapshots during unload. Main thread snapshots immutable edits; bounded worker encodes/compresses/writes. Publish override references only after successful atomic replacement.

Pin dirty data until durable publication. Failed saves retain old references/retry; eviction cannot lose unpublished edits. Preserve navigation's immutable generation publication. Useful before compact deltas.

## 6. Asset and level compression

### C1. Shared compact brick codec with independent zstd frames

One shared lossless CPU/disk schema for compiled chunks, assets and deltas: sorted occupied brick coordinates, occupancy, uniform/mixed values and optional normal/material layers. GPU packets may add alignment/pages; persisted bytes never contain live GPU offsets.

Independent checksummed zstd frames per page/chunk or bounded group. Include compressed/decoded lengths, kind, identity, codec/normal bake versions and dictionary ID. Validate allocation/coordinate bounds, mask/value counts, references and trailing/truncated data before admission.

zstd content checksum detects corruption; retain SHA-256 for offline identity/build caches. Remove runtime SHA-256 only on new checksummed path after equivalent checks exist. TOC enables random access across independent frames; one whole-level stream does not. [Zstandard format](https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md).

Evaluate pure-Go `github.com/klauspost/compress/zstd`; pin Go-compatible version. Reuse decoders, bound memory/concurrency, verify checksums. Begin without dictionaries; optional dictionaries need stable IDs. Deterministic bytes need pinned encoder/version/options; content IDs derive from canonical uncompressed data. [Go zstd documentation](https://github.com/klauspost/compress/tree/master/zstd).

Preserve manifest/payload compatibility. Add explicit versions; never reinterpret kinds or delete v2 readers because Rust did. `.gkasset`, `.gklevel` and editor JSON remain authoring inputs. Compact runtime shipping requires compiled path.

Owners: `content/imported_world_chunk_binary.go`, terrain IO, auxiliary IO, world delta IO, importers, baker, runtime loader. Acceptance: bounded range decode reconstructs identical authoritative geometry, normals, and material semantics; malformed blobs fail before publication.

### C2. Regional metadata and packs: conditional follow-up

If global startup metadata limits scale, load regional placements/lights/assets lazily; keep discovery/index global. Reuse island page forest. Preserve PVS, backing, replacement footprints, stable placement IDs and navigation references.

Packs reduce opens/combine frame ranges. Sorted TOC: layer/content ID, `(x,y,z)`, LOD, kind. Bound open-file cache; range-read, never decompress whole region for one chunk.

Island plan defers binary manifests, generic packs and extra hierarchy formats until observed bottleneck. Rust F5/F1 packaging needs separate architecture decision. Brick compression is independent; retain JSON manifests and avoid second page index.

### C3. Compile heavy assets once; add asset LOD

Offline runtime compiler emits compact geometry, bounds, hierarchy, pivots, collision references, palettes/materials, markers and animation/rig references. Runtime consumes compiled sections, avoiding voxel JSON/VOX parsing and spawn builds.

Deduplicate by content identity, source lattice/resolution, LOD, normal bake version and material mapping. Instance owns transforms/animation. Share compiled headers/tables; load on demand, release after final unpinned user. Reuse `AssetServer`/streamed caches, not parallel residency service.

Compile 2× LOD for useful large assets; integrate `EntityLODComponent`/runtime bindings. Retain coarse fallback during fine uploads. Start with instance distance selection; per-ray LOD remains later shader work.

Collision/navigation/edits use authoritative level-0/source masks; render resampling cannot replace collision. Keep animated parts independent; static collapse follows eligibility rules. Remove source material channel only after proven one-to-one mapping: imported `Value` and `MaterialValue` may differ.

Acceptance: repeated placements share compiled geometry; spawn avoids JSON geometry keys and runtime collapse work; editing one placement copies only touched bricks; distant assets retain coverage.

## 7. World traversal, LOD, and terrain

### W1. Accelerators per layer, dynamic BVH retained

Keep terrain, POIs, edited patches and dynamic props as separate authority layers. Existing 3D chunks and `(group,x,y,z)` terrain adjacency lookup are not world-grid DDA accelerator.

Static page/grid accelerator per compatible layer/aligned POI group: traverse occupied tight cells front-to-back, then bricks. Retain dynamic/rotated/animated/off-grid BVH. Never combine terrain/POI coordinates without layer origin/voxel scale.

Merge accelerator/BVH entries by distance; propagate nearest-hit bound. Retain replacement masks and terrain/POI groups. CPU raycasts use equivalent candidates and authoritative gameplay resolution.

Terrain collision: island height tiles outside edits/POI replacements; voxel masks inside. Add range-limited sector lookup/bit scans to voxel queries. Preserve transformed prop precision and fail-closed readiness.

Adapt Rust W1–W3/K1–K4 without unified 10 cm voxel world or redundant Y coordinate.

### W2. LOD chains per layer and conservative coverage

Use 2× voxel chains for static POIs/assets; island height/surface hierarchy for terrain. Select by projected voxel size, bounds, camera scale and layer policy; retain indoor PVS.

Occupancy OR is conservative but thickens walls/closes openings. Preserve materials/transparency; rebake normals per LOD. Edits stay authoritative at level 0. Approximate coarse collision needs explicit API and must never drive player collision.

Start page/instance selection with atomic readiness. Later per-ray cone-footprint selection uses resident levels/finest resident coarser fallback. Preserve depth, AO, shadow, transparency and hit-material semantics across passes.

Far heightfields/impostors optional; cannot cover POI interiors or replace edit/collision sources. Retain replacement coverage until alternative ready.

### W3. Remove traversal truncation as a scaling failure

CPU BVH uses median splits. G-buffer: 64-entry stack, 512-node cap. Balanced depth does not bound visits below 512.

Prove generated-tree stack bounds; complete traversal within structural limits. Remove silent dropping at arbitrary caps/guards. Audit shadow/transparent/particle/CPU traversal. Broader leaves require contiguous shader leaf instance ranges.

Keep existing near-first child ordering. Rust's exact 30-depth/32-stack constants do not transfer. Acceptance: dense overlapping scenes render every candidate; no correctness dependency on visit limits.

### W4. Generate surface bricks directly

Replace `SetVoxel` loops with bulk masks/values for baked/imported chunks and terrain pages. Distant natural terrain materializes surface bands, not full columns; height tiles retain collision authority.

Apply patch deltas before publication. Retain implicit backing so excavation exposes valid interiors/frontiers. POI caves/overhangs retain full 3D voxel authority, not height data.

## 8. Destruction changes

### E1. Apply operations per brick

Extend event queue with deterministic sequence IDs and sphere/box/capsule/stamp operations. Resolve affected entities/chunks/bricks once; apply brick-local shape masks and changed occupancy/material values in bulk.

Publish dirty masks, membership, bounds and revisions per changed brick/batch. Preserve last-write order, voxel-center shapes, copy-on-edit, backing materialization, persistence marking and navigation edit notification. Optimize synchronous voxel work; Rust 64-write queue/radius cap are not Gekko limits.

Budget touched bricks/fitted-normal halos. Resume deterministic brick order, never partial brick. Store unresident edits by stable content/chunk IDs; apply before visibility. Decide progressive/atomic cross-chunk publication before gameplay latency changes.

Owners: destruction module, primitive/edit helpers, backing, streamed runtime, dirty tracking. Acceptance: bulk sphere results match current voxel-center inclusion and operation order; no missed edits across chunk/backing boundaries.

### E2. Persist changed bricks instead of whole geometry

Key deltas by base content identity, chunk/layer or stable placement/part ID and authoritative lattice. Encode changed-voxel mask/final values, including explicit zero removals. Use sparse/uniform forms; switch to full compact brick when cheaper.

Compress deltas with C1. Preserve backing removals, excavation/frontier state, placement transforms/deletions and navigation ownership. Combine immutable base/deltas before render/collision/LOD derivatives. Reject mismatched base identity.

Track dirtiness continuously; never scan remaining voxels on unload. Save via S4 atomically. Acceptance: edits survive eviction/reload; paint/carve stays last-write-wins; empty overrides never resurrect base.

### E3. Update derivatives only where changed

Share one changed-brick set across normal halos, physics, navigation and coarse updates. Rebuild covering coarse bricks; stop upward propagation only if identical. Retain fine coverage until proxy ready.

Reuse revision-based physics/navigation snapshots. Preserve exact queries/readiness; same-frame async physics needs publication redesign. Keep local overlays until complete graph tiles publish, including reciprocal boundary dependencies.

Acceptance: untouched geometry and navigation tiles remain shared; stale worker results cannot overwrite newer edits; craters remain visible through LOD transitions.

### E4. Bound connectivity and fragment work

Retain existing connectivity splitting, `CarveOnly` and backed-world restrictions. Geometry-only small carves need no whole-map split.

Cache component labels/boundary face masks by brick revision; recompute changed bricks and affected connections. Bridge removal may split large component; local checks cannot prove connectivity. Search immutable worker snapshot/bounded queue; commit only at matching source revision.

Preserve largest-component retention, transforms, palette, mass, collision and fragment entities. Fragment admission also uses S1 budgets. Acceptance: no stale commits, lost voxels or changed split outcomes. No new structural-integrity gameplay.

### E5. GPU removal preview: optional last step

After P3/E1/E2, optional compute removal preview. Compact occupancy, materials and normals consistently; mask-only changes corrupt rank indexing. Update halos or invalidate normals for final CPU rebake.

Tag preview by sequence/source revision. Reject older CPU uploads; replace with authoritative CPU bytes. Add/build remains CPU-owned for growing allocations. Collision/navigation remain CPU authority; hold movement when preview outruns safe collision publication.

Requires writable GPU buffers, render-graph ordering and eviction/device recovery. Existing editing hook does not prove feature complete. Defer until CPU bulk edits/uploads cannot meet interaction needs.

## 9. Additional rendering proposals

### R1. Optional internal render scale

Default-1.0 render scale: one sizing/mapping contract for targets, dispatches, rays, Hi-Z, lighting, transparent depth, decals, water and temporal media. Upscale through resolve; text/gizmos may stay output-resolution.

Audit particles, sprites, beams, planet/astronomical features, picking, resize and reverse-Z. Reset incompatible histories after resize; CPU picking authoritative. Scale 0.5 quarters pixels, not necessarily frame time. Explicit quality tradeoff, not lossless storage optimization.

Owners: app resources/frame graph, GPU render setup, resolve and feature shaders. Acceptance: scale 1.0 preserves current output; lower scales keep every feature aligned through resize and depth reconstruction.

### R2. Narrow shadow invalidation

Retain camera-focused cascades/local-light budgets. Track changed bounds/revisions; dirty intersecting caster volumes only, including removal/streamed activation.

Prevent `VoxelUploadRevision` invalidating unrelated lights. Retain cached cascade transforms until map rebuild. Later consider scrolling clipmaps/dirty texels. Per-pixel sun rays need separate visual/cost review.

Acceptance: edits update affected shadows, unrelated streaming preserves cached shadows, and delayed layers retain coherent transforms.

### R3. Conservative coarse beam prepass

Consider after static accelerators/LOD. 8×8 block needs conservative nearest-geometry lower bound across whole cone. Center-ray/coarse hit is unsafe.

Use expanded bounds/occupancy minus conservative margin. Include required dynamic/transparent surfaces. CPU gameplay raycasts unchanged. Reject if overhead dominates or thin objects disappear.

## 10. Delivery order and decisions

1. **Streaming safety first:** S1/S2, then S3 and S4. These fit current island plan and cap work/memory before content density increases.
2. **Bulk content/edit work:** P5 and E1/E3; begin with existing dense bricks. Preserve old semantics while removing per-voxel build/edit overhead.
3. **Compact authority and persistence:** P1, C1, E2. Ship CPU/disk gains even if GPU atlas storage remains preferable.
4. **GPU memory:** P2 and P4, then P3 behind one storage accessor. Finalize allocation retirement, normal layout, and shader parity before switching defaults.
5. **Compiled content and distance:** C3, W2/W4, then W1 where static traversal justifies it. Extend existing layer/page ownership.
6. **Remaining frame work:** R2 and E4. R1 is independently opt-in; R3/E5 remain later experiments.
7. **Packaging:** C2 only when current manifest/file organization demonstrably limits scale and its architecture decision changes.

### Implementation workflow

From 2026-10-02, use this workflow for remaining delivery. It replaces the earlier
separate-agent review sequence for routine changes; technical order and acceptance
criteria above remain unchanged.

- Scope one coherent batch around a shared observable contract and ownership
  boundary. Group related publication writers that use the established pattern.
  Keep extraction architecture and other ownership changes in separate batches.
- Root names the design, affected files, invariants, corner cases and smallest
  useful coverage before editing. Read the owning docs and inspect current code.
- With explicit test and delegation authorization, use one `gpt-6.1-sol` subagent.
  It writes minimal functionality tests and stops at a relevant RED result with
  production unchanged. Root reviews coverage and assumptions, then resumes the
  same agent to implement against the reviewed, frozen tests until GREEN.
- Root reviews the final diff for missed behavior, ownership violations and
  compatibility. Iterate on concrete findings; do not require a second agent for
  routine application of an established pattern.
- Retain the full workflow for changes to cache ownership, concurrency, streaming
  cancellation, codecs/format compatibility, collision behavior or extraction
  architecture: separate test and implementation agents, with independent
  adversarial reviews before and after implementation. Use `gpt-6.1-sol` for all
  subagents.
- Run focused checks while developing. Run full root tests and affected consumer
  builds once at the batch boundary. Add race or visual/GPU checks when the changed
  contract warrants them; repeat broader checks only after relevant changes.
- Update lasting contracts once in canonical docs. Add a brief roadmap entry with
  the commit, result, commands and limits, then commit the completed batch. Do not
  create per-writer reports or duplicate test matrices/review transcripts.

Test changes remain subject to [workspace instructions](/Users/ddevidch/code/go/gekko3d/AGENTS.md).
This workflow does not independently authorize tests, delegation or commits.

### Completed work

| Step | Commit | Change | Design or canonical contract |
| --- | --- | --- | --- |
| S1a | `e3f11cf` | Hidden residency and readiness tickets | [Design](streamed-rendering-s1a.md) |
| S1b | `a539257` | Global content budgets, ordering and atlas backpressure | [Design](streamed-rendering-s1b.md) |
| S1c | `1c9e7d6` | Renderer-qualified v2 sector/proxy handoff | [Design](streamed-rendering-s1c.md) |
| S2a | `896e1eb` | Prepared geometry byte budgets and build suppression | [Design](streamed-rendering-s2a.md) |
| S2b | `1504a7c` | Decoded leases and pending-result admission | [Design](streamed-rendering-s2b.md) |
| S3a | `d93f3ac` | Incremental v2 observer selection | [Design](streamed-rendering-s3a.md) |
| S3b | `f9911af` | Incremental GPU scene records | [Design](streamed-rendering-s3b.md) |
| S3c | `6115a81` | Structural revisions and cached voxel membership | [Design](streamed-rendering-s3c.md) |
| S3d | `f0e7726` | Incremental hierarchy propagation | [Design](streamed-rendering-s3d.md) |
| S3e | `782ca99` | Aggregate component publication API | [Ownership decision](streamed-rendering-s3e.md) |
| S3f | `53e8203` | Hierarchy output publication | [ECS contract](../engine/ecs.md#hierarchy-ownership) |
| S3g | `e6d40e8` | Reparent/grip helper input publication | [ECS contract](../engine/ecs.md#transform-helper-publication) |
| S3h | `8250177` | Independent attach/surface publication | [ECS contract](../engine/ecs.md#authored-attach-and-surface-publication) |
| S3i | `bce33d8` | Authored aim rotation publication | [ECS contract](../engine/ecs.md#authored-aim-publication) |
| S3j | `977786e` | Authored animation final pose publication | [ECS contract](../engine/ecs.md#authored-animation-publication) |
| S3k | `e825681` | Physics World publication | [ECS contract](../engine/ecs.md#physics-world-publication) |
| S3l | `21aff15` | Accepted moving brush World/Local publication | [ECS contract](../engine/ecs.md#moving-brush-motion-publication) |
| S3m | `11af4d9` | Shared grounded actor World/Local publication | [ECS contract](../engine/ecs.md#grounded-actor-publication) |
| S3n | `1c702e8` | Direct-child ground visual Local publication | [ECS contract](../engine/ecs.md#ground-visual-publication) |
| S3o | `a1e315b` | Geometry-reference normalization and derived Pivot publication | [ECS contract](../engine/ecs.md#voxel-bridge-publication) |
| S3p | `65f4ac5` | Core Camera and derived EntityLOD publication | [ECS contract](../engine/ecs.md#camera-and-entitylod-publication) |
| S3q | `d748040` | Entity/type invalidations, independent cursors and explicit resync | [ECS contract](../engine/ecs.md#bounded-component-publication-journal) |
| S3r | `195575d` | Bounded owned snapshots with exact live input comparison | [Renderer contract](../renderer/runtime.md#effective-palette-fingerprints) |
| S2c | `60b4477` | Capacity accounting, live pins and inactive LRU eviction | [Renderer contract](../renderer/runtime.md#cpu-material-table-cache) |
| S2d | `7ccd9ca` | Assigned-byte accounting, active pressure and inactive LRU eviction | [Renderer contract](../renderer/runtime.md#retained-gpu-geometry-budget) |
| S4a | `13a9e1e` | Complete worker captures and unique durable payload paths | [Persistence decision](streamed-rendering-s4.md) |
| S4b | `6a9f3b7` | Capture-order publication and conservative navigation progress | [Persistence decision](streamed-rendering-s4.md#s4b-imported-capture-order-publication) |
| S4c | `f045d01` | Exclusive byte-accounted transactions, dirty pins and durable checkpoints | [Persistence decision](streamed-rendering-s4.md#s4c-bounded-asynchronous-normal-unload) |
| S2e | `d425c88` | Terminal dispatch cancellation, shared leases and failed Stop recovery | [Cancellation decision](streamed-rendering-s2b.md#s2e-obsolete-preparation-cancellation) |
| S1d | `e07dba5` | Shared full/proxy ordering, waiting age and current/PVS classification | [Priority decision](streamed-rendering-s1b.md#s1d-deterministic-preparation-priority) |
| S1e | `1c72078` | Combined CPU/GPU admission, compatibility pressure and separate retirement debt | [Admission decision](streamed-rendering-s1b.md#s1e-combined-streaming-admission) |
| S1f | `953c577` | Bounded ready ownership, live commit ordering and aging | [Ready-queue decision](streamed-rendering-s1b.md#s1f-deterministic-ready-commit-queue) |
| P5a | `33bc039` | Independent worker registration copies with single-use adoption | [Asset ownership](../assets/runtime-assets.md#streamed-prepared-geometry-lifetime) |
| S3s | `6d35be0` | Shared current scan for automatic linked-light radii | [Renderer contract](../renderer/runtime.md#linked-emitter-source-radii) |
| P5b | `9463579` | Worker terrain geometry with exact adopted asset cleanup | [Terrain ownership](../assets/runtime-assets.md#streamed-terrain-registration) |
| S3t | `ba02753` | One-pass live lookup capture with bounded CPU table reuse | [Renderer contract](../renderer/runtime.md#terrain-and-planet-lookup-preparation) |
| S3u | `de44f7c` | Invocation-local normal context builds only for halo/bake work | [Normal contract](../renderer/runtime.md#normal-neighbor-preparation) |
| S1g | `cd8f2b9` | Opt-in placement units with durable partial ownership | [Commit contract](../content/streaming-and-worlds.md#streamed-level-runtime) |
| S2f | `b973c62` | Direct unpinned prepared-cache eviction order | [Cache contract](../assets/runtime-assets.md#streamed-prepared-geometry-lifetime) |
| S2g | `175c1ae` | Per-kind storage reference presence propagation | [Cache contract](../assets/runtime-assets.md#streamed-prepared-geometry-lifetime) |
| S2h | `b84878b` | Direct decoded-cache candidates with preserved load recency | [Loader contract](../assets/runtime-assets.md#decoded-content-lifetime) |
| S1h | `011de7d` | Structural-input voxel capacity planning | [Renderer contract](../renderer/runtime.md#voxel-capacity-planning) |
| P5c | `1d93aab` | Worker-prepared snapshot registration with current authority | [Snapshot contract](../assets/runtime-assets.md#streamed-voxel-object-snapshot-registration) |
| S2i | `7c0f5fa` | Direct inactive retained-GPU eviction candidates | [Renderer contract](../renderer/runtime.md#retained-gpu-geometry-budget) |
| S3v | `0289f9f` | One-pass saved object override selection per chunk job | [Streaming contract](../content/streaming-and-worlds.md#streamed-level-runtime) |
| S2j | `4f2164e` | Direct inactive CPU material-table eviction candidates | [Renderer contract](../renderer/runtime.md#cpu-material-table-cache) |
| P5d | `d149455` | Worker-prepared first terrain renderer copy with current validation | [Terrain ownership](../assets/runtime-assets.md#streamed-terrain-registration) |
| S2k | This commit | Worker-prepared registered storage descriptions and cached standalone policy charge | [Cache ownership](../assets/runtime-assets.md#streamed-prepared-geometry-lifetime) |

S1/S2/S3 partial. S1c covers v2; v3 selection/cross-layer groups separate. S2 allows live leases/sole oversized pending pressure; temporary builds/other owners remain open. Producer notifications/incremental extraction remain S3.

Next: remaining S1 scheduling/commit bounds, then S2 queues/cache owners and S3 notifications/extraction
in delivery order. No dirty-only extraction is approved. Preserve public mutation
compatibility and conditional proposals.

On 2026-10-03, the user prioritized large single-chunk stalls and recurring CPU
scans. [P5a](#p5a-worker-owned-registration-copies) removes main-thread
registration copying first; P5b moves eligible terrain construction to workers.
S3s reduces recurring emitter scans. S3t/S3u reduce lookup and normal-context rebuilding;
S1g spreads placement work across frames. S1h skips clean resident sector planning;
P5c moves snapshot reconstruction/registration preparation to workers; P5d moves
eligible first terrain renderer allocations/copying to workers. Remaining
cache maintenance and individual atomic units are next.

S1f uses the approved [private ready queue](streamed-rendering-s1b.md#s1f-deterministic-ready-commit-queue).
On 2026-10-03, the user authorized its ownership decision and migration of existing
deferred-commit channel-depth assertions to total prepared-depth/ownership checks.

Decisions to settle before dependent implementation:

- Compact CPU API, editable representation, and immutable ownership; preserve direct consumer access until migrated.
- Pool layout, prefix counts, capacities, retirement, and exact preservation of normal bits.
- Codec dependency/version, checksums, payload versions, and compatibility with existing Gekko content contracts.
- Asset LOD material/transparency rules and collision/source lattice references.
- Progressive versus atomic large-edit publication and asynchronous physics readiness.
- Any binary manifest/region pack or unified-world-grid proposal that changes island plan.

## 11. Verification and review limits

Proposal review checked source symbols, existing plans, byte-layout arithmetic
and document links. Earlier substantial designs retain their historical verification:
[S1a](streamed-rendering-s1a.md#execution-record),
[S1b](streamed-rendering-s1b.md#verification-and-execution-record),
[S1c](streamed-rendering-s1c.md#execution-record),
[S2a](streamed-rendering-s2a.md#execution-record),
[S2b](streamed-rendering-s2b.md#execution-record),
[S3a](streamed-rendering-s3a.md#execution-record),
[S3b](streamed-rendering-s3b.md#execution-record),
[S3c](streamed-rendering-s3c.md#execution-record) and
[S3d](streamed-rendering-s3d.md#execution-record).
Routine implementation records belong here. Canonical docs own lasting contracts;
separate documents retain substantial architectural rationale. Native smoke checks
establish recorded rendering/streaming contracts, not performance gains or pixel parity.

### S3e: Publication API

Commit `782ca99`. Sol 6.1 TDD and both adversarial reviews completed. Focused
checks, root tests, focused race, ActionGame compilation and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3e' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(Ecs_|EcsReflect_|Query_|S3c|S3d|TransformHierarchy|ReparentPreservingWorldTransform|VoxelRtSystem|StreamedVoxel|AuthoredAssetAnimation|NPCAnimation|MovingBrush)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(Ecs_|EcsReflect_|Query_|S3c|S3d|S3e|TransformHierarchy|ReparentPreservingWorldTransform|VoxelRtSystem|StreamedVoxel|AuthoredAssetAnimation|NPCAnimation|MovingBrush)' -count=1
```

### S3f: Hierarchy output publication

Commit `53e8203`. Sol 6.1 TDD and both adversarial reviews completed, including
unchanged NaN recomposition and direct-world repair coverage. Focused checks,
root tests, focused race, ActionGame compilation and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3f' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(S3c|S3d|S3e|TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAssetAnimation|NPCAnimation|MovingBrush)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S3c|S3d|S3e|S3f|TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAssetAnimation|NPCAnimation|MovingBrush)' -count=1
```

### S3g: Helper input publication

Commit `e6d40e8`. Sol 6.1 TDD
and both adversarial reviews completed. Sixteen focused tests protect committed
input publication, rollback, exact bits, unresolved/cyclic hosts, partial IK
failure and explicit flush boundaries. Focused checks, root tests, focused race,
ActionGame compilation and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3g' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestTransformHierarchy|TestReparentPreservingWorldTransform|TestS3dHierarchy|TestAttachAuthoredAssetRootUsesAttachmentTransform)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S3c|S3d|S3e|S3f|S3g|TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAsset|AttachAuthoredAsset)' -count=1
```

### S3h: Authored attach and surface publication

Commit `8250177`. Sol 6.1 TDD
and both adversarial reviews completed. Eleven focused tests, root tests,
focused race, ActionGame compilation and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3h' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS3g|TestAttachAuthoredAssetRootUsesAttachmentTransform$|TestTransformHierarchy$|TestTransformHierarchyIgnoresParentRenderPivot$|TestTransformHierarchyResolvesDeepChainInOnePass$)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S3f|S3g|S3h|TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAsset|AttachAuthoredAsset)' -count=1
```

### S3i: Authored aim rotation publication

Commit `bce33d8`. Sol 6.1 TDD and
both adversarial reviews completed. Eight focused test groups, root tests,
focused race, ActionGame compilation and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3i' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestTransformHierarchy|TestTransformHierarchyIgnoresParentRenderPivot|TestTransformHierarchyResolvesDeepChainInOnePass|TestAttachAuthoredAssetRootUsesAttachmentTransform)$' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S3g|S3h|S3i|TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAsset|AttachAuthoredAsset)' -count=1
```

### S3j: Authored animation final pose publication

Commit `977786e`. Sol 6.1 TDD and
both adversarial reviews completed. Eight focused test groups, root tests,
focused race, ActionGame compilation and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3j' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(AuthoredAssetAnimation|NPCAnimation|S3i|TransformHierarchy)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S3i|S3j|AuthoredAssetAnimation|NPCAnimation|TransformHierarchy)' -count=1
```

### S3k: Physics World publication

Commit `e825681`. Both result writers
publish actual changed World poses; the [ECS contract](../engine/ecs.md#physics-world-publication)
owns the details. Eight focused test groups, root tests, focused race, ActionGame
compilation and editor build passed. Sol 6.1 TDD and both adversarial reviews
completed.

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3k' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(Physics|SynchronousPhysics|SphereBoxCollisionResolves|CapsuleBoxCollisionResolves|RenderToPhysics|ScaledPivotWorld|TransformHierarchy)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S3k|Physics|SynchronousPhysics|SphereBoxCollisionResolves|CapsuleBoxCollisionResolves|RenderToPhysics|ScaledPivotWorld|TransformHierarchy)' -count=1
```

### S3l: Accepted moving brush publication

Commit `21aff15`. Accepted motion
publishes changed brush World and existing Local independently; the
[ECS contract](../engine/ecs.md#moving-brush-motion-publication) owns the details.
Six focused test groups, root tests, focused race, ActionGame compilation and
editor build passed. Sol 6.1 TDD and both adversarial reviews completed.

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3l' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(MovingBrush|GroundedPlayerLandsOnMovingBrush|TransformHierarchy)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S3k|S3l|MovingBrush|GroundedPlayerLandsOnMovingBrush|TransformHierarchy)' -count=1
```

### S3m: Shared grounded actor publication

Commit `11af4d9`: `feat(grounding): publish actor poses`. The shared writer publishes
changed actor World and existing Local independently; the
[ECS contract](../engine/ecs.md#grounded-actor-publication) owns the details.
Six focused test groups, root tests, focused race, ActionGame compilation and
editor build passed. Sol 6.1 TDD and both adversarial reviews completed.

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3m' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(Grounded|MovingBrush|Character|S3l|TransformHierarchy)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S3l|S3m|Grounded|MovingBrush|Character|TransformHierarchy)' -count=1
```

### S3n: Direct-child ground visual publication

Commit `1c702e8`. The helper publishes
changed Local Y bits; the [ECS contract](../engine/ecs.md#ground-visual-publication)
owns the details. Sol 6.1 RED/GREEN and root coverage/code reviews completed.
Four focused test groups, existing grounding/hierarchy checks, full root tests,
ActionGame compilation and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3n' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestUpdateCharacterGroundVisualY|TestCharacterGround|TestApplyCharacterVisualGroundOffsetToChildren|TestTransformHierarchy|TestS3f)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
```

Existing tests preserved. This main-thread notification change needed no new race
or visual/GPU check. Editor's module stat-cache warning exited successfully.
No extraction bypass or performance claim; remaining producers stay live.

### S3o: Voxel bridge assignment publication

Commit `a1e315b`. Geometry-reference
normalization and derived Pivot publish at their existing committed writes;
the [ECS contract](../engine/ecs.md#voxel-bridge-publication) owns the details.
Sol 6.1 RED/GREEN and root coverage/code reviews completed. Five focused test
groups, existing voxel/hierarchy checks, full engine tests, ActionGame and
`examples/testing-vox` compilation, and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3o' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(S3c|VoxelRtSystem|VoxPhysics|VoxelGeometry|EntityLOD|TransformHierarchy)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
# examples/testing-vox/
env GOCACHE=/tmp/gekko3d-gocache go test ./... -run '^$'
```

Existing tests preserved. Main-thread notifications change no concurrency,
collision or rendered numerical behavior; no new race or visual/GPU check was
needed. Editor's module stat-cache warning exited successfully. Live extraction
and ownerless pointer normalization remain; no performance claim.

### S3p: Core camera and EntityLOD publication

Commit `65f4ac5`. Core camera controllers
and EntityLOD selection publish changed outputs; the
[ECS contract](../engine/ecs.md#camera-and-entitylod-publication) owns the details.
Sol 6.1 RED/GREEN and root coverage/code reviews completed. Six focused test
groups, existing grounded/LOD/publication checks, full engine tests, ActionGame,
SpaceGame and voxel sample compilation, and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3p' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(S3m|S3n|S3o|EntityLOD|GroundedPlayer|GroundedCharacterMotor|S3cVoxelInventoryCameraDependentLOD)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
# spacegame_go/ and examples/testing-vox/
env GOCACHE=/tmp/gekko3d-gocache go test ./... -run '^$'
```

Existing tests preserved. Main-thread notification changes needed no new race
or visual/GPU check. Editor's module stat-cache warning exited successfully.
Consumer writers and mutable asset aliases remain untracked; live extraction
continues and no performance gain is claimed.

### S3q: Bounded component publication journal

Commit `d748040`. The
[ECS contract](../engine/ecs.md#bounded-component-publication-journal) owns the API;
[S3q decision](streamed-rendering-s3e.md#s3q-bounded-publication-journal-decision)
owns the rationale. Separate Sol 6.1 test/implementation agents, root reviews and
independent pre/post adversarial reviews completed. Six focused test groups,
existing ECS/publication checks, full engine tests, focused race, ActionGame,
SpaceGame and voxel sample compilation, and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(S3q|S3e|Ecs_|EcsReflect_|Query_|S3c|S3d)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S3q|S3e|Ecs_|EcsReflect_|Query_|S3c|S3d)' -count=1
```

Existing tests preserved. macOS linker/module stat-cache warnings exited
successfully. Metadata-only main-thread changes needed no visual/GPU check.
History cap excludes returned batches and total ECS memory; structural fallback
and live extraction remain required. No frame-time gain claimed.

### S3r: Effective-palette fingerprint reuse

Commit subject `perf(renderer): reuse effective palette fingerprints`.
[Renderer contract](../renderer/runtime.md#effective-palette-fingerprints) owns the
behavior; [S3r decision](streamed-rendering-s3c.md#s3r-effective-palette-fingerprint-reuse-decision)
owns the rationale. Separate Sol 6.1 test/implementation agents, root and
independent pre/post reviews completed. Review caught public diagnostics controlling
admission; private accounting now owns it. Eight focused groups, existing bridge,
material/LOD/streamed checks, full engine tests, focused race, ActionGame, SpaceGame
and voxel sample compilation, and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(S3r|S3c|VoxelRtSystem|StreamedVoxel|EntityLOD|BuildMaterialTable|EffectiveVoxelPaletteAt)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S3r|S3c|VoxelRtSystem|StreamedVoxel|EntityLOD|BuildMaterialTable|EffectiveVoxelPaletteAt)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS3rSmoke.app/Contents/MacOS/gekko-s3r-smoke /tmp/gekko-s3r-smoke.go
/tmp/GekkoS3rSmoke.app/Contents/MacOS/gekko-s3r-smoke > /tmp/gekko-s3r-smoke.log 2>&1
```

Native smoke exited 0: 193 frames/6.632s; three 60-frame idle holds at two snapshots,
9,459 accounted bytes, no additional idle hashes. Aliased property/frame edits,
elapsed animation, hierarchy/destination repair and diagnostic reset passed.
Zero upload budget held a hidden streamed target; two resumed upload frames reached
Ready, then reveal preserved object/map. CUA showed the rendered colored scene.
These establish continuity/work contracts, not pixel parity or measured speedup.
Existing tests preserved; macOS linker/module stat-cache warnings exited successfully.
Live extraction remains; the original material-table cache and other owners remain
outside the new snapshot budget.

### S2c: Retained CPU material tables

Commit `60b4477`. Separate Sol 6.1
test/implementation agents and root/independent pre/post reviews completed. Five
focused groups protect configuration, shared pins/pressure, recent-use eviction,
animated history, borrowed backing and hidden streamed users. Full engine tests,
focused race, ActionGame/SpaceGame/voxel sample compilation and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(S2c|S3r|S3c|VoxelRtSystem|StreamedVoxel|EntityLOD|BuildMaterialTable|EffectiveVoxelPaletteAt)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -race -run '^Test(S2c|S3r|S3c|VoxelRtSystem|StreamedVoxel|EntityLOD|BuildMaterialTable|EffectiveVoxelPaletteAt)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS2cSmoke.app/Contents/MacOS/gekko-s2c-smoke /tmp/gekko-s2c-smoke.go
/tmp/GekkoS2cSmoke.app/Contents/MacOS/gekko-s2c-smoke > /tmp/gekko-s2c-smoke.log 2>&1
```

Native smoke exited 0: 193 frames/6.633s, three 60-frame holds with no table work.
Disabled warm retention kept two active keys/20,992 accounted bytes pinned under
pressure; aliased edits and animation evicted four old keys. Hidden delayed
uploads reached Ready in two resumed frames, and reveal retained object/map.
These verify continuity and ownership, not pixel parity or a GPU/process ceiling.
Existing tests and unrelated changes preserved; macOS warnings exited successfully.

### S2d: Retained GPU geometry slots

Commit `perf(renderer): budget retained GPU geometry slots`. Separate Sol 6.1
test/implementation agents and root/independent pre/post reviews completed. Five
groups protect actual slot charge/release, mutable CPU snapshots, shared/hidden
pins, pressure/LRU and independent caps. Review caught repeated accounting scans:
private invalidated charges now preserve dedup and avoid idle stats allocation.
Full engine tests, GPU race, ActionGame/SpaceGame/SpaceSim/voxel sample compilation
and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -run '^Test(S2d|RetainedVoxelMap|ReleaseVoxelAuxSlot|PrepareVoxelStructureDirtyState)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race ./voxelrt/rt/gpu -count=1
env GOCACHE=/tmp/gekko3d-gocache go run /tmp/gekko-s2d-accounting.go
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS2dSmoke.app/Contents/MacOS/gekko-s2d-smoke /tmp/gekko-s2d-smoke.go
/tmp/GekkoS2dSmoke.app/Contents/MacOS/gekko-s2d-smoke > /tmp/gekko-s2d-smoke.log 2>&1
```

Manual accounting probe: 100 stats reads for 100 sectors/6,400 aux slots retained
the same 7,171,456-byte charge; allocation fell from 156,961 bytes/read to zero.
This narrow observation establishes no frame-time gain. Native smoke exited 0:
198 frames/6.801s; one-byte cap retained two active maps/13,280 bytes under pressure.
Hidden uploads refreshed charge by five aux slots, then reached Ready. Removal
evicted one map without changing CPU geometry; activation missed and two real
reupload frames reached Ready again. Physical buffers/pages remain outside budget.
Existing tests and unrelated changes preserved; macOS warnings exited successfully.

### S4a: Immutable persistence prerequisites

Commit `13a9e1e`. Separate Sol 6.1
test/implementation agents and root/independent pre/post reviews completed. Three
groups protect complete capture independence, failed-manifest preservation/retry
for terrain/imported/object payloads, and the actual navigation worker's isolated
payload attempt. Review tightened navigation coverage before freeze. Existing
persistence/Stop/navigation checks, full engine tests, focused race and affected
consumer builds passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS4a' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(StreamedRuntime(Persists|Stop)|S2aRuntime(FailedPersistence|Stop)|S2bRuntimeFailedStop|StreamedNavigation|NavigationEditBlockers|RuntimeNavigationBlocker|ConfigureStreamedNavigation)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S4a|StreamedRuntime(Persists|Stop)|S2aRuntime(FailedPersistence|Stop)|S2bRuntimeFailedStop|StreamedNavigation|NavigationEditBlockers|RuntimeNavigationBlocker|ConfigureStreamedNavigation)' -count=1
```

Disk roundtrips preserve old durable references through failed save and successful
retry. Formats/readers and blocking helper/Stop contracts remain. Normal unload
IO, in-memory transaction publication and byte admission remain S4b; unreferenced
successful payload files can accumulate. No GPU change needed a windowed check.
Existing tests and unrelated changes preserved; macOS warnings exited successfully.

### S4b: Imported capture-order safety

Commit `6a9f3b7`. Separate Sol 6.1
test/implementation agents and root/independent pre/post reviews completed.
Delayed navigation results cannot replace newer imported saves or backing
removals. Latest saved snapshots retain navigation progress after unload;
successors preserve conservative uncommitted impact. Review added pending/active
impact and cross-world retry coverage without changing existing tests. Focused
checks, full engine tests, focused race and affected consumer builds passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS4[ab]' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(StreamedRuntime(Persists|Stop|Restart)|S2aRuntime(FailedPersistence|Stop)|S2bRuntimeFailedStop|StreamedNavigation|NavigationEditBlockers|RuntimeNavigationBlocker|ConfigureStreamedNavigation)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S4[ab]|StreamedRuntime(Persists|Stop|Restart)|S2aRuntime(FailedPersistence|Stop)|S2bRuntimeFailedStop|StreamedNavigation|NavigationEditBlockers|RuntimeNavigationBlocker|ConfigureStreamedNavigation)' -count=1
```

Real-worker and disk roundtrips verify latest references, owned captures and
navigation rebuild inputs. Normal unload IO/byte admission remain S4c; graph
scheduling and error behavior remain. No rendering change required a windowed
check. Unrelated changes preserved; macOS warnings exited successfully.

### S4c: Bounded asynchronous persistence

Commit `f045d01`. Separate Sol 6.1
test/implementation agents, root reviews and independent pre/post reviews
completed. One exclusive byte-accounted owner covers normal dirty captures and
ordinary manifest saves. Dirty ownership survives delayed IO, upload clearing,
new sibling edits and retry; durable checkpoints preserve newer RAM publications.
Stop retains its blocking durability and failed-stop lifetime contracts.
Canonical behavior: [world deltas](../content/streaming-and-worlds.md#world-deltas).

Focused checks, full engine tests (root 14.061s), focused race (4.731s) and all
five consumer checks passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(S4[abc]|StreamedRuntime(Persists|Stop|Restart)|S2aRuntime(FailedPersistence|Stop)|S2bRuntimeFailedStop|StreamedNavigation|NavigationEditBlockers|RuntimeNavigationBlocker|ConfigureStreamedNavigation)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S4[abc]|StreamedRuntime(Persists|Stop|Restart)|S2aRuntime(FailedPersistence|Stop)|S2bRuntimeFailedStop|StreamedNavigation|NavigationEditBlockers|RuntimeNavigationBlocker|ConfigureStreamedNavigation)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS4cSmoke.app/Contents/MacOS/gekko-s4c-smoke /tmp/gekko-s4c-smoke.go
/bin/zsh -lc '/tmp/GekkoS4cSmoke.app/Contents/MacOS/gekko-s4c-smoke > /tmp/gekko-s4c-smoke.log 2>&1'
```

Disposable native smoke passed in 16 frames: real GPU Ready, dirty publication
pin, durable unload and latest reload Ready; one-byte budget exposed 2,048 retained
bytes. An initial harness schema error was corrected before the successful run.
This verifies lifetime/save-reload behavior, not pixel parity or speed. Worker
temporaries, navigation caches, allocator overhead and orphan payload collection
remain separate. Existing tests and unrelated changes preserved; macOS warnings
exited successfully.

### S2e: Obsolete preparation cancellation

Commit `d425c88`. Separate Sol 6.1
test/implementation agents and root/independent pre/post reviews completed.
Full/proxy dispatch cancellation remains terminal through renewed demand and
residency upgrades. Obsolete errors cannot poison the runtime; cancelled
consumers release scopes/credits while shared work and external leases survive.
Canonical behavior: [streaming](../content/streaming-and-worlds.md).

Focused checks, full engine tests (root 14.096s), focused race (7.555s) and five
consumer checks passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS2e' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS2[ab]|TestS3aSelection|TestStreamedRender|TestStreamedRuntime.*(Stop|SectorProxy|ImportedFullChunkUnload)|TestS4cStop)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestS2[abe]|TestS3a|TestStreamedRender|TestStreamedRuntime.*(Stop|SectorProxy|ImportedFullChunkUnload)|TestS4c)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS2eSmoke.app/Contents/MacOS/gekko-s2e-smoke /tmp/gekko-s2e-smoke.go
/bin/zsh -lc '/tmp/GekkoS2eSmoke.app/Contents/MacOS/gekko-s2e-smoke > /tmp/gekko-s2e-smoke.log 2>&1'
```

Disposable native smoke passed in 19 frames under one-byte CPU/pending budgets:
full/proxy Ready, visible fallback after dirty unload, latest full reload Ready
and retained hidden fallback. Visibility checks wait for the documented ECS
handoff after renderer readiness. CPU tests hold actual workers for cancellation;
native checks preserve fallback usability, not pixel parity or speed. Cancellation
does not preempt an active shared decode/build. Separate IO/generation/navigation
queues and total process memory remain open. Existing tests and unrelated changes
preserved; macOS warnings exited successfully.

### S1d: Deterministic preparation dispatch

Commit `e07dba5`. Separate
Sol 6.1 test/implementation agents and root/independent pre/post reviews completed.
One shared heap orders full/proxy admission with aging, stable ties and cached
current/PVS classification. Known byte-blocked work permits smaller work; renewed
demand retains its own age through an older cancellation acknowledgement.
Canonical behavior: [streaming](../content/streaming-and-worlds.md).

Focused checks, full engine tests (root 13.631s), focused race (6.546s) and five
consumer checks passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS1dPrepare' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS2b|TestS2e|TestS3aSelection|TestStreamedRuntimeStop|TestStreamedRender|TestStreamedVoxel|TestOrdinaryHiddenVoxel)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestS1dPrepare|TestS2[be]|TestS3aSelection|TestStreamedRuntimeStop|TestStreamedRender|TestS4c)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS2eSmoke.app/Contents/MacOS/gekko-s2e-smoke /tmp/gekko-s2e-smoke.go
/bin/zsh -lc '/tmp/GekkoS2eSmoke.app/Contents/MacOS/gekko-s2e-smoke > /tmp/gekko-s2e-smoke.log 2>&1'
```

Unchanged native fallback fixture passed in 19 frames/657ms: full/proxy Ready,
visible fallback after durable unload and latest full reload Ready. Peak pending
charge was 2,046 bytes under a one-byte budget. CPU tests establish dispatch
ordering; native checks establish usability, not pixel parity or speed. Active
worker semantics and GPU priority remain; combined stage admission and other S1
queues are separate. Existing tests and unrelated changes preserved; macOS
warnings exited successfully.

### S1e: Combined streaming admission

Commit `1c72078`. Separate Sol 6.1 test/implementation agents and
root/independent pre/post reviews completed. Admission spans actual full/proxy
dispatch, queued result and initial qualified renderer completion. Hidden Ready
children permit limit-one refinement. Compatibility work exposes pressure;
Stop/restart keeps retiring GPU debt separate. Review fixed captured-generation
cleanup and repeated accounting scans. Existing tests remain unchanged.
Canonical behavior: [streaming](../content/streaming-and-worlds.md).

Focused checks, full engine tests (root 14.375s), focused race (7.514s) and five
consumer checks passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS1eStreamingWork' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS1dPrepare|TestS2b|TestS2e|TestS3aSelection|TestStreamedRuntimeStop|TestStreamedRender|TestStreamedVoxel|TestOrdinaryHiddenVoxel)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestS1eStreamingWork|TestS1dPrepare|TestS2b|TestS2e|TestS3aSelection|TestS4c|TestStreamedRuntimeStop|TestStreamedRender|TestStreamedVoxel|TestOrdinaryHiddenVoxel)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke /tmp/gekko-s1e-smoke.go
/bin/zsh -c '/tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke > /tmp/gekko-s1e-smoke.log 2>&1'
```

Disposable native check passed in 41 frames/1.407s: allowance one, 30-frame upload
pause, visible fallback, hidden Ready child, complete two-child handoff and clean
Stop. No pixel parity, speed or total-memory claim. Synchronous gameplay/late
adoption/repair can exceed the allowance; main-thread commit units and remaining
stage queues still need bounds. Unrelated changes preserved; macOS warnings
exited successfully.

### S1f: Deterministic ready commits

Commit `953c577`, completed 2026-10-03 after approved ready-owner alignment. Bounded capture,
live priority/aging, deferred ownership and hook-Stop invalidation passed separate
Sol 6.1 tests/implementation and independent/root reviews. Only the approved
physical-depth assertions were migrated; S1e lifetime assertions remain intact.
Focused checks, full engine (root 15.395s), race (8.586s) and five consumer builds
passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS1fCommit' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS1fCommit|TestS1dPrepare|TestS1eStreamingWork|TestS2b|TestS2e|TestS3aSelection|TestStreamedRuntimeStop|TestStreamedRuntimeCommitBudget|TestStreamedRender|TestStreamedVoxel|TestOrdinaryHiddenVoxel)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestS1fCommit|TestS1eStreamingWork|TestS1dPrepare|TestS2b|TestS2e|TestS3aSelection|TestS4c|TestStreamedRuntimeStop|TestStreamedRender|TestStreamedVoxel|TestOrdinaryHiddenVoxel)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke /tmp/gekko-s1e-smoke.go
/bin/zsh -c '/tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke > /tmp/gekko-s1e-smoke.log 2>&1'
```

Unchanged native fixture passed in 41 frames/1.413s: allowance one, 30-frame
upload pause, visible fallback, hidden Ready child, complete refinement and Stop.
No pixel parity or performance claim. One large CPU commit remains unbounded.
Unrelated changes preserved; macOS warnings exited successfully.

### P5a: Worker-owned registration copies

Commit `33bc039`, completed 2026-10-03. Independent worker copies transfer once into assets;
immutable sources, public defensive registration, warm reuse and deferred/cancelled
byte ownership remain intact. Separate tests/implementation and independent/root
reviews passed. Focused checks, full engine (root 14.239s), focused race (5.038s)
and five consumer builds passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestP5a' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestP5a|TestS1fCommit|TestS1eStreamingWork|TestS1dPrepare|TestS2a|TestS2b|TestS2e|TestStreamedRuntime|TestStreamedRender|TestStreamedVoxel|TestOrdinaryHiddenVoxel)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestP5a|TestS1fCommit|TestS1eStreamingWork|TestS1dPrepare|TestS2a|TestS2b|TestS2e|TestStreamedRuntime|TestStreamedRender|TestStreamedVoxel)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke /tmp/gekko-s1e-smoke.go
/bin/zsh -c '/tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke > /tmp/gekko-s1e-smoke.log 2>&1'
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke /tmp/gekko-p5a-smoke.go
/bin/zsh -c '/tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke > /tmp/gekko-s1e-smoke.log 2>&1'
```

Unchanged two-child native check passed (41 frames/1.384s). The 64-child check
passed (132 frames/4.549s), with 65 adoptions, allowance eight, 30-frame upload
pause, fallback coverage, atomic handoff and clean Stop.
No FPS, pixel-parity or historical `gasworks_128` crash-resolution claim.
Terrain/backing edits, placements, storage-ledger traversal and renderer staging
remain; one large commit is still not fully bounded. Unrelated changes preserved.

### S3s: One-pass linked emitter aggregation

Commit `6d35be0`, completed 2026-10-03. Automatic linked-light radii share one current object scan,
with no scan when no eligible group exists. Direct unmarked scale/link/geometry
edits, removal, overrides and existing light/ambient/Sun outputs passed frozen
functionality tests and root review. Focused checks, full engine (root 14.826s)
and five consumer builds passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3s' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS3s|TestSyncVoxelRtLights|TestVoxelRtSystem|TestS3cVoxelInventory|TestS3o|TestS3r)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
```

The visit diagnostic confirms one pass instead of one pass per linked light.
No measured frame-time gain or dirty-only extraction claim. Local aggregation
changes no concurrency, GPU allocation or shader contract; no new race/native
check was needed. Existing tests and unrelated changes preserved.

### P5b: Worker terrain registration

Commit `9463579`, completed 2026-10-03. Eligible terrain construction and registration copying
moved to workers. Live removal fallback, private renderer geometry, pending byte
ownership and exact unload/partial Stop cleanup passed separate tests/implementation
and independent/root reviews. Focused checks, full engine (root 14.177s), focused
race (5.010s) and five consumer builds passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestP5b' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestP5a|TestP5b|TestS1fCommit|TestS1eStreamingWork|TestS1dPrepare|TestS2a|TestS2b|TestS2e|TestStreamedRuntime|TestSpawnAuthored|TestVoxelBacking)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestP5a|TestP5b|TestS1fCommit|TestS1eStreamingWork|TestS1dPrepare|TestS2a|TestS2b|TestS2e|TestStreamedRuntime|TestVoxelBacking)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke /tmp/gekko-p5b-smoke.go
/bin/zsh -c '/tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke > /tmp/gekko-s1e-smoke.log 2>&1'
```

Native check passed (40 frames/1.353s): two 32,768-voxel terrain chunks, two
adoptions, allowance one, 30-frame upload pause, Ready visibility and exact owned
asset cleanup. No measured FPS or total commit-time bound. Live-removal fallback,
backing setup, object-scoped renderer copies, placements and other large units
remain main-thread work. Existing tests and unrelated changes preserved.

### S3t: Bounded visible object lookup reuse

Commit `ba02753`, completed 2026-10-03. One live visible-object pass replaces three
lookup scans; exact scalar rows reuse terrain/planet tables on idle frames.
Retained key/byte capacities have a configurable 4 MiB default ceiling. Shader
bytes, duplicates/collisions, direct edits and existing per-frame GPU writes remain.
Separate tests/implementation and independent PRE/POST reviews passed.

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -run '^(TestS3t|TestS3b|TestBuildTerrainChunkLookup|TestBuildPlanetTileLookup)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race ./voxelrt/rt/gpu -run '^(TestS3t|TestS3b|TestBuildTerrainChunkLookup|TestBuildPlanetTileLookup)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke /tmp/gekko-s3t-smoke.go
/bin/zsh -c '/tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke > /tmp/gekko-s1e-smoke.log 2>&1'
```

Focused checks 0.543s, race 1.592s, engine root 14.618s; five consumer commands
below passed. Native mixed planet/terrain check passed: five lookup builds over
40 frames, 416 retained bytes, paused uploads, Ready visibility and Stop cleanup.
This is CPU work evidence, not pixel parity or a measured FPS gain. Lookup writes,
temporary builds, other renderer scans and large commit units remain.

### S3u: Lazy normal neighbor preparation

Commit `de44f7c`, completed 2026-10-03. Normal halo propagation and runtime aux baking
share one invocation-local lazy full-scene context. Idle/material/precomputed
work skips neighbor map construction when no dirty cross-object source needs it.
Original dirty snapshots, zero-budget halos, hidden neighbors and packed normal
bytes remain intact. Separate tests/implementation and independent PRE/POST
reviews passed. The user also approved the S1g opt-in publication contract.

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -run '^(TestS3u|TestBuildVoxelAuxBytes|TestMarkCrossObjectNormalHaloDirty|TestPrepareVoxelStructureDirtyState|TestS1b|TestS2d)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race ./voxelrt/rt/gpu -run '^(TestS3u|TestBuildVoxelAuxBytes|TestMarkCrossObjectNormalHaloDirty|TestPrepareVoxelStructureDirtyState|TestS1b|TestS2d)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke /tmp/gekko-s3u-smoke.go
/bin/zsh -c '/tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke > /tmp/gekko-s1e-smoke.log 2>&1'
```

Focused checks 0.465s, race 1.475s, engine root 15.211s; five consumer commands
below passed. Native mixed lookup/terrain check passed: 33 context builds over
50 frames, zero idle context visits, 30-frame upload pause, Ready visibility and
owned asset cleanup. No pixel parity/FPS claim. Dirty inspection, allocation and
upload-planning scans remain; resumable commits and individual large units are next.

### S1g: Opt-in resumable placement commits

Commit `cd8f2b9`, completed 2026-10-03. Managed runtimes may admit a configured
number of whole placement units per frame. Defaults and CPU-only commits stay
synchronous. The bounded ready owner retains partial entities, payloads, leases
and CPU admission until completion or durable cleanup. Current edits, exact
same-coordinate completion, hook Stop/restart and failed persistence preserve
ownership. Cleanup does not consume or wait for the chunk advance allowance.
Separate tests/implementation and independent PRE/POST reviews passed.

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS1g|TestS1fCommit|TestS1eStreamingWork|TestS2e|TestS4c)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestS1g|TestS1f|TestS1e|TestS2b|TestS2e|TestS4c|TestStreamedLevelRuntime)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke /tmp/gekko-s1g-smoke.go
/bin/zsh -c '/tmp/GekkoS1eSmoke.app/Contents/MacOS/gekko-s1e-smoke > /tmp/gekko-s1e-smoke.log 2>&1'
```

Focused checks 4.175s, race 6.635s and engine root 15.308s passed, along with
five consumer commands below. Native check passed (49 frames/1.682s):
48 collapsed placements capped at two units, two
32,768-voxel terrain adoptions, allowance two, 30-frame upload pause, Ready
visibility and exact Stop cleanup. No elapsed-time/FPS or pixel-parity claim.
Terrain/imported phases, individual assets, snapshots and hooks remain atomic.
Opt-in placements publish visuals/collision unit by unit; completed imported
cohorts retain the proxy handoff gate. Existing tests and unrelated changes preserved.

### S2f: Direct prepared-cache eviction order

Commit `b973c62`, completed 2026-10-03. The prepared cache selects its true oldest
unpinned entry directly under the existing mutex, preserving hit/final-release
ordering, live leases, worker deferral and original-server cleanup. Candidate
visits scale with victims or deferrals instead of all live/warm owners per victim.
The cumulative public counter exposes selection work. Separate tests/implementation
and independent PRE/POST reviews passed.

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS2f|TestS2a|TestP5a|TestS2e|TestS1g)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestS2f|TestS2a|TestP5a|TestS2e|TestS1g)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Focused checks 2.682s, race 3.715s, engine root 14.734s and five consumer commands
below passed. Byte charge, source lifetime, readiness and collision policy remain
unchanged; no new native check was needed. Storage graph admission/reference
traversals and other cache owners remain. No FPS or hard frame-time claim.
Existing tests and unrelated changes preserved; macOS warnings exited successfully.

### S2g: Storage reference presence transitions

Commit `175c1ae`, completed 2026-10-03. The immutable physical ledger propagates
references to children only when an owner kind becomes present or absent.
Repeated aliases and partial removal avoid redundant descendant walks. Physical
bytes, duplicate edges, prepared-preferred asset attribution and independent
pins remain exact. The cumulative reference counter publishes through scalar
reads. Separate tests/implementation and independent PRE/POST reviews passed.

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS2g|TestS2f|TestS2a|TestP5a|TestS2e|TestS1g)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestS2g|TestS2f|TestS2a|TestP5a|TestS2e|TestS1g)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Focused checks 2.739s, race 4.023s, engine root 16.862s and five consumer commands
below passed. No source/readiness/collision change needed a new native check.
Cold graph capture, standalone charge and first/last ownership may still traverse
large graphs; no FPS or total frame-time bound. Existing tests and unrelated
changes preserved; macOS warnings exited successfully.

### S2h: Direct decoded-cache eviction candidates

Commit `b84878b`, completed 2026-10-03. Byte-pressure trim indexes only unpinned
entries; last load/hit recency survives final scope release and rejected-value
release. Shared pins, Clear epochs, singleflight, pointer validity and byte policy
remain. Selection avoids scanning pinned owners; heap maintenance is logarithmic
in eligible entries. Candidate visits publish through read-only loader/runtime
stats. Separate tests/implementation and independent PRE/POST reviews passed.

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS2h|TestS2b|TestRuntimeContentLoader|TestS2e|TestS1g)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestS2h|TestS2b|TestRuntimeContentLoader|TestS2e|TestS1g)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Focused checks 3.045s, race 4.605s, engine root 16.905s and five consumer commands
below passed. No source/readiness/collision change needed a new native check.
Decode, graph estimation, explicit Clear and rare recency rebasing remain outside
the pressure-selection bound. No FPS/process-memory ceiling. Existing tests and
unrelated changes preserved; macOS warnings exited successfully.

### S1h: Capacity planning for structural inputs

2026-10-03, `011de7d`. Capacity planning skips clean allocated maps' sectors,
including during another map's arrival. Exact record requirements, pointer
sharing, hidden uploads and buffer headroom remain. The diagnostic counts only
sector entries actually examined. Root review also removed idle dedup writes
for clean maps; frozen functionality tests and existing tests remain unchanged.

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -run '^(TestS1h|TestPrepareVoxelStructureDirtyState|TestVoxelUpload|TestVoxelObjectReady|TestS2d|TestS3u)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1h-smoke /tmp/gekko-s1h-smoke.go
/tmp/gekko-s1h-smoke > /tmp/gekko-s1h-smoke.log 2>&1
```

Focused 0.955s, engine root 16.542s and all five consumer commands below passed.
Native Metal smoke passed in 278ms: 512 resident sectors; a three-sector arrival
visited three entries; replacement visited only its 32-sector map; a 600-sector
arrival grew capacity; hidden readiness, idle zero visits and removal held.
The disposable fixture checks native buffers/writes, not visual parity or FPS.
Other scene scans and a large new/dirty map's own work remain. Unrelated changes
were preserved; macOS module stat-cache warnings exited successfully.

### P5c: Worker-prepared voxel-object snapshot registration

2026-10-03, `1d93aab`. Workers prepare snapshot geometry, bounds and separate
registration copies. Matching snapshots adopt without main-thread reconstruction,
copying or bounds scans. Resumable commits retain authoritative rereads, exact
ordered-content checks and changed/removed/error fallback behavior. Legacy
captured authority, atomic publication, hooks and collision remain. Pending
charges and exact adopted-asset leases cover partial commits and durable cleanup.
Separate tests/implementation and independent PRE/POST reviews passed.

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestP5c|TestP5b|TestP5a|TestS1g|TestS2e)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestP5c|TestP5b|TestP5a|TestS1g|TestS2e)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-p5c-smoke /tmp/gekko-p5c-smoke.go
/tmp/gekko-p5c-smoke > /tmp/gekko-p5c-smoke.log 2>&1
```

Focused 2.555s, race 4.020s, engine root 16.508s and all five consumer commands
below passed. Native smoke passed: two 32³ snapshots, readiness, voxel collision,
edit isolation, durable reload, hooks and exact owned-asset removal; 29 frames,
649ms. Its macOS caller locks the main thread before setup. This verifies native
behavior, not pixel parity or an FPS gain. File reads/comparison, changed-content
fallbacks, source spawning and worker temporary memory remain outside this
improvement. Existing tests and unrelated changes preserved; macOS warnings
exited successfully.

### S2i: Direct retained-GPU eviction candidates

2026-10-03, `7c0f5fa`. Pressure selects inactive GPU owners from existing
recency directly. Hidden/active pins, exact assigned charges, recency updates,
saturated pressure and allocation release remain unchanged. Candidate visits
exclude reads, no-pressure maintenance, all-pinned pressure and explicit release.
Separate tests/implementation and independent PRE/POST reviews passed.

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -run '^(TestS2i|TestS2d|TestVoxelUpload|TestVoxelObjectReady|TestPrepareVoxelStructureDirtyState|TestS1h)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race ./voxelrt/rt/gpu -run '^(TestS2i|TestS2d|TestVoxelUpload|TestVoxelObjectReady|TestS1h)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Focused 1.009s, race 1.935s, engine root 16.462s and all five consumer commands
below passed. Existing assigned-slot/readiness coverage verifies the unchanged
release path; no new native check was needed. Owner maintenance/stats scans and
charge refresh work remain. Removed heap refs clear, but nonempty heaps may
retain peak capacity; no total VRAM ceiling or measured FPS gain. Existing tests
and unrelated changes preserved; macOS warnings exited successfully.

### S3v: One-pass saved object override selection

2026-10-03, `0289f9f`. Chunk job setup visits each world object override key
once, with no scan for an empty placement selection. Full-prefix matching,
embedded NUL/overlapping IDs, original key/value authority, applied saved voxels
and selected-file errors are preserved. Public last-job visits expose this bound.
Root reviewed and froze the minimal tests before the same agent implemented.

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS3v|TestP5c|TestS1g|TestS2e)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Focused 2.540s, engine root 16.212s and all five consumer commands below passed.
No concurrency, GPU or lifetime change required new race/native checks. World
key enumeration, prefix comparisons and selected snapshot I/O remain; no measured
frame-time gain. Existing tests and unrelated changes preserved; macOS module
stat-cache warnings exited successfully.

### S2j: Direct inactive CPU material-table candidates

2026-10-03, `4f2164e`. Material-cache pressure selects inactive keys directly
by existing age. Completed maintenance gathers current keys and uses one owner
pass; active/hidden pins, saved unpin age, capacity charges, saturated cleanup,
map pruning and borrowed backing remain. Public visits count actual victims.
Separate tests/implementation and independent PRE/POST reviews passed.

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS2j|TestS2c|TestS3r|TestVoxelRtSystem|TestStreamedVoxel)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestS2j|TestS2c|TestS3r|TestVoxelRtSystem|TestStreamedVoxel)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Focused 1.221s, race 2.709s, engine root 16.470s and all five consumer commands
below passed. No GPU behavior changed; existing hidden-streamed coverage suffices
without new native verification. Owner/instance scans, temporary current-key
collection, hashing/building and post-eviction map pruning remain. Nonempty heap
capacity may remain; no measured frame-time gain. Existing tests and unrelated
changes preserved; macOS warnings exited successfully.

### P5d: Worker-prepared first terrain renderer copy

`d149455` transfers eligible managed terrain renderer copies through an exact
asset-owned single-use candidate. Current content/scope validation preserves raw
public edits, object isolation and ordinary fallbacks. Pending admission charges
the third map; asset statistics retain unused candidate charge until admission
or deletion. See the [canonical contract](../assets/runtime-assets.md#streamed-terrain-registration).

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestP5d|TestP5c|TestP5b|TestP5a|TestS1g|TestS2e|TestStreamedVoxel|TestVoxelRtSystemUsesObjectScoped|TestVoxelRtSystemSharesTerrain)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestP5d|TestP5c|TestP5b|TestP5a|TestS1g|TestS2e|TestStreamedVoxel|TestVoxelRtSystemUsesObjectScoped|TestVoxelRtSystemSharesTerrain)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-p5d-smoke /tmp/gekko-p5d-smoke.go
/tmp/gekko-p5d-smoke > /tmp/gekko-p5d-smoke.log 2>&1
```

Focused 2.975s, race 4.181s, engine root 16.634s and all five consumer commands
below passed. Native smoke passed with two adjacent 32,768-voxel terrain maps:
worker adoption, runtime-map readiness, collision, isolated surface edit, durable
terrain snapshot reload and exact owned cleanup (16 frames). Surface editing uses
the existing heightfield snapshot contract. One full main-thread validation read,
backing setup, extra candidate residency and other atomic work remain; no measured
frame-time claim. Frozen tests and unrelated changes preserved.

### S2k: Worker-prepared registered storage descriptions

This commit transfers exact fresh full/proxy registration descriptions into the
existing physical ledger, avoiding main-thread node recapture/allocation. Lazy
standalone charge reuse skips retention union work only for qualified independent
copies. Pending metadata, interior aliases, attribution, pins and terminal
cleanup retain their owners. [Canonical contract](../assets/runtime-assets.md#streamed-prepared-geometry-lifetime).

Verification passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestS2k|TestS2a|TestS2f|TestS2g|TestS2b|TestS2e|TestP5a|TestP5b|TestP5c|TestP5d|TestS1g)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^(TestS2k|TestS2a|TestS2f|TestS2g|TestS2b|TestS2e|TestP5a|TestP5b|TestP5c|TestP5d|TestS1g)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Focused 4.236s, race 5.877s, engine root 16.491s and all five consumer commands
below passed. Frozen tests and unrelated changes preserved. GPU admission and
source lifetime are unchanged; no new native smoke needed. Flat identity loops,
first/last reference walks and generic graph admission remain. Descriptor metadata
adds pending storage; cache bookkeeping remains excluded from cache charge.
No measured frame-time claim; macOS warnings exited successfully.

Consumer commands for these steps:

```sh
# actiongame/
env GOCACHE=/tmp/gekko3d-gocache go test ./... -run '^$'
# gekko-editor/
env GOCACHE=/tmp/gekko3d-gocache go build ./...
# spacegame_go/, spacesim/, examples/testing-vox/
env GOCACHE=/tmp/gekko3d-gocache go test ./... -run '^$'
```

Existing tests preserved. macOS linker/module stat-cache warnings exited successfully. Notification-only changes needed no new windowed smoke/engine sweep. Unrelated editor/sample baselines not rerun; see S3c/S3d. Publication/compilation verified; incomplete producers still require live extraction.

Verify each batch with the smallest relevant checks. Select manual scenes for the
changed contract: fixed-view parity, edited seams, instance isolation, delayed
handoff, save/reload or edited locomotion. Apply the [implementation workflow](#implementation-workflow).
User authorized functionality tests/tests-first subagents for this implementation.
Other test changes follow [workspace instructions](/Users/ddevidch/code/go/gekko3d/AGENTS.md).

Related Gekko plans: [island streaming](../content/island-streaming.md), [XBrickMap hot path](../renderer/xbrickmap-hotpath-optimization-plan.md), [uniform materials](../renderer/xbrickmap-uniform-material-plan.md), [quality-preserving optimization](../renderer/quality-preserving-optimization-plan.md), and [renderer change guide](../renderer/change-guide.md).
