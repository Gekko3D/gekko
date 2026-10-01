# Streamed Rendering and Content Optimization Proposals

Date: 2026-10-02. Status: staged implementation; S1a/S1b/S1c, S2a/S2b, and S3a/S3b/S3c complete. S2 and S3 remain partial; other sections are proposals.

Source: [rusty-voxelrt roadmap](/Users/ddevidch/code/rust/rusty-voxelrt/docs/roadmaps/OPEN_WORLD_STREAMED_RENDERING.md). This review covers optimization proposals, excluding the measurement phase and its status records. Gekko code inspected at `1f7a281`, including current working-tree content. Rust performance targets and compression ratios are not Gekko predictions.

## 1. Recommendation and architectural boundary

Apply compact bricks, packed GPU records, immutable payload sharing, bounded streaming, compiled asset data, and brick edit deltas. Several prerequisites already exist in Gekko. Adapt those systems rather than replacing them wholesale.

Preserve the [island streaming architecture](../content/island-streaming.md): separate terrain and POI layers, layer-specific resolution, height collision outside voxel replacements, camera-relative rendering, ECS lifetime, and main-thread commits. A common brick codec and scheduler do not require one global voxel lattice.

Two Rust choices do not transfer directly. Gekko uses fitted, encoded normals rather than 6-bit neighbor normals. Gekko's island plan also preserves current manifests and payloads; Rust's deletion of every legacy reader is not an approved Gekko migration policy.

Primary owners: renderer storage/upload and streamed content runtime. Consumers: physics, navigation, assets, editor, importers, ActionGame, and SpaceSim. Confidence: high for applicability and current structure; medium for runtime gains and final formats. Implementation requires the individual designs below; this document does not approve a terrain architecture change.

## 2. Current Gekko foundations and gaps

- [CPU brick storage](../../voxelrt/rt/volume/xbrickmap.go): 8³ bricks, 32³ sectors, 2³ micro masks. `Sector.PackedBricks` already uses popcount indexing. `Brick.Payload` still stores 512 bytes for every allocated brick, including uniform bricks.
- [GPU upload](../../voxelrt/rt/gpu/manager_voxel.go): each sector reserves 64 brick records. Shader indexing uses `sector.brick_table_index + brick_idx_local`, not CPU popcount indexing. Mixed bricks use a paged `R8Uint` atlas; uniform bricks already skip the color payload.
- [Auxiliary data](../../voxelrt/rt/volume/voxel_aux.go): 64 bytes of occupancy plus 1,024 bytes of encoded normals per brick. The normal encoding includes validity and two-sided lighting. [G-buffer traversal](../../voxelrt/rt/shaders/gbuffer.wgsl) already rejects empty voxels before loading their material.
- [Streaming](../../streamed_level_runtime.go): worker preparation, generation checks, indexed chunk maps, proxy/full handoff, and separate collision/destruction interest already exist. [S3a selection](../../streamed_level_selection.go) caches idle demand and updates cube differences. Commit limits count chunks and elapsed time; global GPU content budgets are implemented in S1b.
- [Caching](../../runtime_content_loader.go): S2b adds decoded-content byte budgets, scoped leases, in-flight suppression, and shared pending-result admission. [Prepared geometry](../../streamed_level_geometry_cache.go) has S2a byte accounting and pinned users. Live decoded leases and one sole oversized pending result expose pressure exceptions; GPU retention and other owner budgets remain further S2 work. These are not total process memory limits.
- [Imported payloads](../../content/imported_world_chunk_binary.go): binary RLE already exists, with JSON metadata and SHA-256 verification. Decoding expands into voxel records, then [spawning](../../imported_world_spawn.go) builds `XBrickMap` through per-voxel writes. Terrain chunks remain JSON.
- [Assets](../../assets/voxel_assets.go): geometry and palette assets are separate; shared geometry and copy-on-edit already exist. There is no equivalent to Rust's compiled fragment package in this path. [Geometry registration](../../asset_vox_model.go) deep-copies prepared maps. [Inline shapes](../../asset_voxel_shape.go) serialize geometry into a JSON cache key. [Entity LOD assets](../../entity_lod_runtime_assets.go) already generate simplified geometry and impostors at runtime; offline compilation can remove that work.
- [Physics](../../mod_vox_physics.go): collision already reads voxel geometry; asset grids are shared. `CopyChangedSectors` supplies immutable changed-sector snapshots to physics and [navigation](../../navigation_graph_runtime.go). There is no equivalent bulk JSON collision-box package to eliminate.
- [Destruction](../../mod_destruction.go): the queue already holds sphere events, grouped per entity. [Sphere application](../../voxelrt/rt/volume/primitives.go) still calls `SetVoxel` per voxel. Connectivity already uses brick components, but scans the whole map when splitting runs.
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

Add compact brick storage behind voxel read, iteration, raycast, and edit interfaces. Keep dense editable bricks as a separate representation. Suggested compact fields: brick coordinate, 512-bit occupancy mask, material mode, packed occupied values, and optional packed normal data. Keep sector brick masks and packed sector indexing.

Uniform bricks store one material value. Mixed bricks store one byte per occupied voxel. Expand only touched bricks during edits; repack at edit publication. Decode static chunks directly into compact storage. Replace scattered direct `Payload` reads before changing the storage representation.

Share immutable compact payloads between assets, rendering, physics, and navigation. Go pointers/slices provide sharing, but require explicit immutable ownership and copy-on-write; importing Rust `Arc` is unnecessary. Preserve `Revision`, `SectorRevisions`, dirty upload state, and existing snapshot semantics. Keep GPU offsets out of shared content data.

Benefit: lower static memory and fewer allocations/copies. Risk: rank lookup can cost more than dense access on heavily edited bricks. Keep dense storage for that workload. Occupancy-only consumers should not retain rendering normals unnecessarily.

Owners: `volume/xbrickmap*.go`, `mod_vox_physics.go`, `navigation_graph_runtime.go`, `assets/voxel_assets.go`, imported-world preparation. Acceptance: identical authoritative voxels, material queries, raycasts, and unchanged asset instances after editing one instance.

### P2. Pack GPU brick records by sector occupancy

Replace each sector's fixed 64-record block with records for occupied bricks. Use `base + popcount(mask below brick index)` in every shader lookup. The existing WGSL popcount helper provides a starting point; it is not the current GPU indexing contract.

Use sector-sized allocations with capacity classes or limited slack. Rebuild only the touched sector range when brick membership changes. Upload replacement records before publishing the sector base/mask together. Retire old ranges only when prior GPU submissions cannot reference them. Do not expose a new mask with an old packed table.

Decouple auxiliary buffer capacity from `requiredBricks = sector slots × 64`. Size auxiliary storage from its own live allocation requirement. This avoids reserving 1,088 auxiliary bytes for every potential brick record.

Tradeoff: popcount adds shader work and packed membership changes require relocation. Keep a dense sector table option for highly occupied or frequently edited sectors if packing loses its benefit.

Owners: `gpu/manager_voxel.go`, allocation metadata, scene bindings. Update `gbuffer.wgsl`, `shadow_map.wgsl`, `transparent_overlay.wgsl`, and `particles_sim.wgsl` together. Acceptance: identical hits across empty/new/removed bricks, correct slot reuse, and records proportional to occupied bricks.

### P3. Pack mixed materials and existing normals

Add a paged storage-buffer pool as an alternative to mixed-brick atlas slots. Index occupied values with `payload base + rank(occupancy, voxel index)`. Pack bytes into `u32` words and extract lanes in WGSL; do not allocate one `u32` per material byte. Respect adapter binding-size and storage-binding limits. [WGSL integer types](https://www.w3.org/TR/WGSL/#integer-types) define the portable integer representation.

Pack Gekko's current 16-bit normal words by occupied voxel. Preserve octahedral values, validity, and two-sided bits exactly. Uniform material does not imply uniform normals. Keep the micro mask as a traversal skip structure. Optional lane prefix counts trade a small header cost for fewer popcounts.

Rust's 6-bit normal replacement would change fitted surfaces, thin walls, and lighting. Mask-derived normals remain a separate visual design experiment. Boundary invalidation must retain Gekko's current fitting halo, including extended surface fitting, rather than only six immediate neighbors.

Storage arithmetic, excluding allocator slack, sector tables, and pool page overhead: a current mixed brick uses `32 + 512 + 64 + 1024 = 1632` bytes. A packed mixed brick with `n` occupied voxels uses approximately `32 + 64 + 3*n` bytes. At `n = 64`, that is `288` bytes. These are layout estimates, not measured savings. Uniform bricks save their material payload already; their remaining normal allocation is the target.

Prototype both storage paths behind one accessor. Delete the atlas only after visual parity and favorable traversal behavior on sparse and dense content. Packed buffers can lose texture locality; reject a slower default. Disk/CPU compaction can ship without replacing the atlas.

Owners: voxel pool allocator, upload/bind groups, traversal shaders, normal builder, app pipeline layouts. Acceptance: identical normals, AO, transparency, shadows, particles, and edited seams; no stale allocation reads.

### P4. Share immutable GPU material blocks

Intern immutable material tables by canonical material content and semantics. Allocate one GPU block per distinct table rather than per `VoxelObject`. Reuse cached CPU palette definitions and compiled table IDs; remove repeated runtime serialization from identity calculation.

Retain the 256-entry local palette addressing contract. A large level can have many tables; do not force all materials into one `uint8` palette. Color alone is not material identity: gameplay tags, transparency, emission, and animation bindings matter.

Keep instance-specific animated palettes and material overrides private, or copy the shared block on first mutation. Reference-count shared GPU blocks and retire them safely. Acceptance: sharing scales with distinct immutable tables; animated or recolored instances do not alter other instances.

Owners: material allocation in `manager_voxel.go`, bridge material sync, `AssetServer`, compiled asset tables.

### P5. Build immutable upload packets on workers

Replace RLE/JSON decode into voxel structs followed by `SetVoxel` reconstruction with direct brick decode. Prepare occupancy, normal bytes, tight bounds, and upload records off the main thread. Importers and terrain generators should use bulk brick builders too.

Transfer prepared slices by ownership through existing worker channels. Share only immutable backing storage. Mutable live maps, renderer allocation state, and temporary decoder buffers must not leak into worker snapshots. Reuse changed-sector snapshots rather than introducing whole-scene clones.

Add an explicit immutable-adoption registration path: `RegisterSharedVoxelGeometryWithCacheKey` currently calls `xbm.Copy()` even for worker-prepared geometry. Preserve its defensive-copy behavior for mutable callers; prepared immutable payloads need not pay that second geometry copy.

Keep `XBrickMap` content, CPU snapshots, GPU allocation metadata, and dirty-work queues as separate owners. This is a targeted ownership change, not a Bevy-style parallel ECS scheduler replacement. Workers must never mutate ECS or call WebGPU.

Acceptance: runtime admission registers prepared geometry without rebuilding it voxel by voxel; no duplicate source representation remains pinned after its consumers release it.

## 5. Streaming changes

### S1. Global budgets, priority queues, and renderer readiness

Finish the existing island plan's upload/readiness contract before adding more detail. Use global per-frame upload bytes and changed-brick/record limits across all objects. S1b implements these content-write limits, replacing per-object limits that allowed work to grow with object count. Allocation/migration and lookup work remain outside this cap.

Budget commits by estimated bytes and bounded work units as well as elapsed time. A timer checked between chunks cannot prevent one huge commit from stalling. Stage large chunks hidden and resume their uploads across frames.

Use the island plan's ticket/generation/revision readiness contract. Readiness includes geometry, materials, lookup topology, and every required upload. Keep parents or proxies visible until all replacement children or coverage-group members are ready. Hidden-for-upload must differ from excluded-from-upload.

Maintain deterministic queues for preparation, ready commits, uploads, retry, retention, and retirement. Use stable ties and aging. Reserve progress for collision and visible detail; the current proxy-first channel drain must not starve gameplay or detail indefinitely. Atlas/pool pressure causes backpressure and eviction, not an atlas-full panic.

Owners: streamed runtime, renderer bridge, `app_voxel_residency.go`, GPU upload manager. Acceptance: global limits hold for many objects; delayed/failed uploads and teleports retain valid coverage and safe collision.

### S2. Bound caches and worker throughput by bytes

Add byte accounting for decoded content, prepared geometry, pending results, retained GPU data, auxiliary normals, material tables, and editable patches. Replace entry-only limits with byte ceilings. Bound `RuntimeContentLoader` too; otherwise evicting geometry leaves source voxel arrays resident.

Pin live render, collision, navigation, and fallback users. Evict unreferenced detail first. Track shared backing storage once in global physical-memory accounting, while retaining per-user residency costs for admission.

Use per-key in-flight load/build suppression to prevent concurrent cache misses duplicating IO and decode. Normalize path identities; compiled data should use content IDs. Separate bounded IO, decode/generation, and navigation work queues so one workload cannot consume all workers. Cancel obsolete demand and discard stale generations.

Keep explicit empty-page metadata. Current empty-chunk skipping must still honor placement content, backing, edit overrides, and collision interest. No-IO emptiness is valid only when all relevant content is empty.

Acceptance: total cache/pending memory stays bounded while traveling; concurrent requests reuse one result; gameplay-only chunks need no GPU admission.

### S3. Incremental selection and scene gathering

Status: partial. [S3a](streamed-rendering-s3a.md) completes current v2 observer
demand selection: entity/chunk/effective-radius keys, overlap counts, disjoint
cube differences, cached imported visibility/full-sector/fallback derivation,
explicit main-thread metadata invalidation, and transient working-demand
cleanup. Idle selection reuse preserves per-frame streaming progress. Live
selection entries track active observer footprints and indexed metadata; empty
maps release capacity, while nonempty Go maps may retain peak capacity. No byte
ceiling or frame-time speedup is claimed.

[S3b](streamed-rendering-s3b.md) completes manager-owned incremental scene record
preparation and publication. Stable object templates feed separate ordered
visible, transparent and shadow arrays; exact input snapshots track actual
matrices/bounds, origin, encoded metadata and allocation/direct lookup state.
Idle geometry reuses compiled rows and relative BVHs, and unchanged record
buffers skip successful queue writes at the same published destination.
Preparation follows voxel/lookup maintenance. Replacement destinations refresh
bindings even with sufficient capacity; explicit invalidation forces preparation
and publication again. Ownership follows the current pass union, with removed
references and obsolete tails cleared and empty caches released. Nonempty cache
maps/slices can retain peak capacity, and no byte ceiling or frame-time gain is
claimed. Scene culling/LOD keep their current behavior.

[S3c](streamed-rendering-s3c.md) completes the committed ECS structural stamp
and voxel-state-owned candidate inventory, with implementation review and native
continuity verification complete. Membership discovery reuses grouped
entity-ID/row locations until the owner or stamp changes. Exact typed columns
are reacquired per batch on every pass, and all live candidate processing,
streamed adoption, camera/light extraction and hierarchy timing remain intact.
Hidden, unresolved and sprite-LOD candidates are included. Obsolete references
are cleared through capacity tails and empty aggregate storage is released;
nonempty capacity can retain its peak. Complete component-value notifications,
incremental value extraction and future layer/transform selection remain S3
work; membership counters do not establish a frame-time improvement.

Cache observer selection by spatial bucket, radii, layer transform/topology, and PVS state. Update entering/exiting shells instead of constructing all radius sets every frame. Recompute on teleports, observer additions/removals, radius changes, edits, and visibility changes. Merge multiple observers with demand counts so one observer cannot evict another's content.

Maintain a stable entity/object table. Extend current transform/material comparisons with dirty registrations, hierarchy changes, removals, and visibility notifications. Keep camera-dependent culling/LOD separate from content change tracking.

Gekko's ECS has no automatic Bevy `Changed<T>` contract. Add explicit versions/events at owning mutation boundaries before relying on incremental extraction. Preserve transform propagation and command-flush order, including animated children and moving brushes. Rebuild GPU records/BVH only when relevant inputs change.

Acceptance: idle observers do not rebuild residency sets; idle geometry does not rebuild records; movement and ancestor changes appear at the current stage boundary.

### S4. Remove synchronous persistence from unload

`persistChunkOverrides` serializes whole snapshots and saves during unload. Snapshot immutable changes on the main thread; encode/compress/write on a bounded persistence worker. Publish override references only after successful atomic file replacement.

Keep dirty data pinned until durable publication. Failed saves retain their old references and retry; eviction must not drop unpublished edits. Preserve navigation's existing immutable generation publication. This change remains useful before compact edit deltas exist.

## 6. Asset and level compression

### C1. Shared compact brick codec with independent zstd frames

Define one lossless CPU/disk brick schema used by compiled world chunks, asset geometry, and deltas. Encode sorted occupied brick coordinates, occupancy masks, uniform/mixed values, and optional normal/material layers. GPU packets may add alignment and page metadata; persisted data must not contain live GPU offsets.

Use independent checksummed zstd frames per page/chunk or bounded chunk group. Include compressed/decoded lengths, kind, content identity, codec version, normal bake version, and dictionary ID. Validate allocation bounds, coordinates, mask/value counts, references, and trailing/truncated data before admission.

The zstd content checksum detects corruption; SHA-256 can stay for offline content identity/build caches. Remove runtime SHA-256 only in the new checksummed path after equivalent integrity checks exist. A TOC provides random access between independent frames; one zstd stream for an entire level does not. [Zstandard format](https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md).

Evaluate `github.com/klauspost/compress/zstd` as a pure-Go codec candidate. Reuse decoders, bound memory/concurrency, and require checksum verification. Pin a version compatible with Gekko's Go toolchain. Optional dictionaries must ship with stable IDs; begin without them. Deterministic bytes require pinned encoder/version/options; derive content IDs from canonical uncompressed content. [Go zstd documentation](https://github.com/klauspost/compress/tree/master/zstd).

Preserve Gekko's current manifest/payload compatibility policy. Add explicit payload versions; do not reinterpret an existing payload kind or delete v2 readers because Rust did. Authored `.gkasset`, `.gklevel`, and editor JSON remain authoring inputs. Shipping runtime data becomes compact only where a compiled path exists.

Owners: `content/imported_world_chunk_binary.go`, terrain IO, auxiliary IO, world delta IO, importers, baker, runtime loader. Acceptance: bounded range decode reconstructs identical authoritative geometry, normals, and material semantics; malformed blobs fail before publication.

### C2. Regional metadata and packs: conditional follow-up

Move regional placements, lights, and asset demand into lazy page/region catalogs if global startup metadata becomes a scaling limit. Keep only discovery/index data global. Reuse the island page forest and preserve indoor PVS, backing, replacement footprints, stable placement IDs, and navigation references.

Region packs can reduce file opens and combine small independent frame ranges. A sorted TOC would key by layer/content ID, `(x,y,z)`, LOD, and payload kind. Use bounded open-file caches and range reads; never decompress a whole region to fetch one chunk.

The current island plan explicitly defers binary manifests, generic packs, and additional hierarchy formats until an observed bottleneck justifies them. Treat Rust F5/F1 packaging as conditional, requiring a separate architecture decision. Brick compression does not depend on region packs. Preserve JSON manifests initially; avoid an unnecessary second page index.

### C3. Compile heavy assets once; add asset LOD

Add an offline runtime asset compiler. Emit compact geometry sections, bounds, part hierarchy, pivots, collision source references, palette/material tables, marker bindings, and animation/rig references. Runtime loading consumes compiled sections instead of parsing voxel JSON/VOX and building geometry at spawn.

Use content identity plus source lattice, resolution, LOD, normal bake version, and material mapping for deduplication. Keep placement transforms and animation state on instances. Share compiled headers/tables across placements; load them on first demand and release after the final unpinned user. Reuse `AssetServer` and streamed cache ownership instead of a parallel residency service.

Compile 2× voxel LOD sections for large assets where useful. Integrate with existing `EntityLODComponent` and runtime asset bindings. Keep a resident coarse fallback while finer sections upload. Distance selection may start per instance; per-ray LOD is a later shader change.

Collision, navigation, and edits consume authoritative level-0/source masks. Render resampling cannot replace collision authority. Preserve independent parts on animated assets; static collapse remains subject to existing eligibility rules. Do not remove a source material channel unless its palette mapping is proven one-to-one; imported `Value` and `MaterialValue` can differ.

Acceptance: repeated placements share compiled geometry; spawn avoids JSON geometry keys and runtime collapse work; editing one placement copies only touched bricks; distant assets retain coverage.

## 7. World traversal, LOD, and terrain

### W1. Accelerators per layer, dynamic BVH retained

Keep terrain, imported POIs, edited terrain patches, and dynamic props as distinct authority layers. Gekko already has 3D chunk coordinates and a `(group,x,y,z)` terrain lookup used for adjacency. That lookup is not a scene-level world-grid DDA accelerator.

Add a static page/grid accelerator per compatible layer or aligned POI group. Traverse occupied cells front-to-back with tight bounds, then enter their brick maps. Keep dynamic, rotated, animated, and off-grid objects in BVH. Never concatenate terrain/POI integer coordinates without layer origin and voxel scale.

For closest hits, merge accelerator and BVH candidates by entry distance; propagate the nearest hit bound. Retain layer replacement masks and terrain/POI coverage groups. Existing CPU raycasts should use equivalent candidates, with authoritative resolution for gameplay.

Terrain collision follows the island plan: height tiles outside edited patches/POI replacements; voxel masks inside them. Existing voxel collision can gain range-limited sector lookup and bit scans. Preserve exact transformed prop collision and fail-closed readiness.

This adapts Rust W1–W3/K1–K4. It does not require revoxelizing all static content into one 10 cm world or adding a Y coordinate that Gekko already has.

### W2. LOD chains per layer and conservative coverage

Use 2× voxel chains for imported static POIs and assets; terrain uses the island plan's height/surface page hierarchy. Select by projected voxel size, bounds, camera scale, and layer policy. Keep PVS filtering for interiors.

For voxel LOD, occupancy OR is conservative but can thicken thin walls and close openings. Preserve material/transparency rules and compute normals at each level. Edits remain authoritative at level 0. Approximate coarse collision, if added, needs an explicit API and must never drive player collision.

Start with page/instance selection and atomic parent/child readiness. Later per-ray cone-footprint selection can choose among resident levels, with the finest resident coarser fallback. It requires consistent depth, AO, shadows, transparency, and hit-material semantics across passes.

Far heightfields/impostors remain optional terrain representations. They cannot cover POI interiors or replace editable/collision sources. Preserve replacement coverage until its alternative is ready.

### W3. Remove traversal truncation as a scaling failure

The CPU BVH builder already splits at the median. G-buffer traversal has a 64-entry stack and a 512-node iteration cap. A balanced tree protects stack depth; it does not guarantee fewer than 512 node visits.

Prove stack bounds against the generated tree and make traversal complete within valid structural bounds. Remove silent geometry dropping from arbitrary iteration caps and stack guards. Audit shadow, transparent, particle, and CPU traversal too. If broader leaves are used, leaf instance order must be contiguous with the shader's leaf range contract.

Keep existing near-first child ordering. Rust's exact 30-depth/32-stack constants do not transfer. Acceptance: dense overlapping scenes render every candidate; no correctness dependency on visit limits.

### W4. Generate surface bricks directly

Replace per-voxel `SetVoxel` loops with bulk mask/value construction for baked/imported chunks and terrain surface pages. For distant natural terrain, materialize visible surface bands rather than filled columns. Height tiles remain collision authority there.

Integrate edited patch deltas before publication. Preserve implicit backing so first excavation reveals valid interior material and new frontier surfaces. POI caves/overhangs remain full 3D voxel content; height data is not their source.

## 8. Destruction changes

### E1. Apply operations per brick

Keep the existing event queue and extend it with deterministic sequence IDs and shape operations: sphere, box, capsule, and stamp. Resolve affected entities/chunks/bricks once. Build a brick-local shape mask and apply changed occupancy/material values in bulk.

Publish dirty masks, sector membership, bounds, and revisions once per changed brick/batch. Preserve last-write order, voxel-center shape semantics, copy-on-edit, backing materialization, persistence marking, and navigation edit notification. Gekko's problem is repeated synchronous voxel work; Rust's 64-write queue and radius cap are not Gekko constraints.

Budget touched bricks and fitted-normal halo work. Resume large operations in deterministic brick order, never halfway through one brick. Keep unresident edits against stable content/chunk IDs and apply before the chunk becomes visible. Define whether cross-chunk edits publish progressively or atomically before changing gameplay latency.

Owners: destruction module, primitive/edit helpers, backing, streamed runtime, dirty tracking. Acceptance: bulk sphere results match current voxel-center inclusion and operation order; no missed edits across chunk/backing boundaries.

### E2. Persist changed bricks instead of whole geometry

Store each delta against base content identity, chunk/layer or stable placement/part ID, and authoritative lattice. Encode changed-voxel mask plus final values, including explicit zero removals. Keep sparse/uniform encodings and switch to a full compact brick when a dense delta is cheaper.

Serialize deltas with C1 compression. Preserve backing removals, excavation/frontier state, placement transforms/deletions, and navigation override ownership. Loading combines immutable base and current deltas before deriving render/collision/LOD data. Reject a mismatched base identity rather than applying edits to unrelated regenerated geometry.

Track dirty deltas continuously; unloading must not scan every remaining voxel to discover changes. Save through S4 atomic publication. Acceptance: destruction survives eviction and reload; overlapping paint/carve remains last-write-wins; empty overrides do not resurrect original content.

### E3. Update derivatives only where changed

Feed one changed-brick set to normal halo updates, physics snapshots, navigation invalidation, and coarse proxy/LOD updates. Rebuild only coarse bricks covering changed fine bricks; stop upward propagation only when the coarse result is identical. Keep edited fine coverage until the replacement proxy is ready.

Reuse current revision-based physics/navigation snapshots. Preserve exact collision query behavior and streamed readiness; do not promise same-frame asynchronous physics without changing its publication contract. Keep local navigation overlays active until complete replacement graph tiles publish, including reciprocal boundary dependencies.

Acceptance: untouched geometry and navigation tiles remain shared; stale worker results cannot overwrite newer edits; craters remain visible through LOD transitions.

### E4. Bound connectivity and fragment work

Gekko already performs connectivity splitting, unlike Rust's performance-only track. Preserve `CarveOnly` and backed-world restrictions. Do not run whole-map splitting after every small carve when the caller requests geometry-only editing.

Cache brick component labels and boundary face masks by brick revision. Recompute changed bricks and affected connections. Removing a bridge can split a large component, so local checks alone cannot prove global connectivity. Complete that graph search on an immutable worker snapshot or bounded work queue; commit only if its source revision still matches.

Keep current largest-component retention, transforms, palette, mass, collision, and fragment entity semantics. Fragment geometry/GPU admission needs S1 budgets too. Acceptance: no stale fragment commits, lost voxels, or changed split outcomes. This optimizes existing destruction behavior; it adds no new structural-integrity gameplay.

### E5. GPU removal preview: optional last step

After P3/E1/E2, a compute pass may preview removal against resident GPU bricks. Clear occupancy and compact both packed materials and packed normals consistently; changing only the mask would corrupt rank indexing. Update fitting halos or invalidate normals for the CPU's final rebake.

Tag previews with operation sequence and source revision. Reject older CPU uploads after newer previews, then replace previews with authoritative CPU bytes. Add/build operations remain CPU-owned because allocations can grow. Collision/navigation follow CPU authority; hold movement where a preview temporarily outruns safe collision publication.

This needs a GPU-owned writable buffer path, render-graph ordering, and recovery on eviction/device recreation. The current GPU editing hook is not evidence of this complete feature. Defer until CPU bulk edits and upload scheduling are insufficient for the intended interaction.

## 9. Additional rendering proposals

### R1. Optional internal render scale

Add a default-1.0 scene render scale. Derive scene targets, dispatches, camera rays, Hi-Z, lighting tiles, transparent depth, decals, water, and temporal media from one sizing/mapping contract. Upscale through the current resolve compositor; text/gizmos may remain output-resolution overlays.

Audit particles, sprites, beams, astronomical/planet features, picking, resize, and reverse-Z. Reset incompatible histories on size changes. CPU picking remains authoritative. At scale 0.5, scene pixel count becomes one quarter; frame time does not necessarily follow. Lower resolution is an explicit quality tradeoff, not a lossless storage optimization.

Owners: app resources/frame graph, GPU render setup, resolve and feature shaders. Acceptance: scale 1.0 preserves current output; lower scales keep every feature aligned through resize and depth reconstruction.

### R2. Narrow shadow invalidation

Retain current camera-focused directional cascades and local-light cadence budgets. Record changed world-space bounds and geometry revisions. Dirty only lights/cascades whose caster volumes intersect them, including removed geometry and streamed activation.

Prevent `VoxelUploadRevision` changes from invalidating every unrelated local layer. Preserve cached cascade transforms until each corresponding map is rebuilt. After that, consider scrolling clipmap cascades with dirty texel regions. Per-pixel sun rays are an alternative requiring separate visual/cost review, not an automatic replacement.

Acceptance: edits update affected shadows, unrelated streaming preserves cached shadows, and delayed layers retain coherent transforms.

### R3. Conservative coarse beam prepass

Consider only after static accelerators and LOD exist. An 8×8 ray block needs a conservative lower bound on the nearest geometry across its whole cone. A center-ray sample or ordinary coarse LOD hit cannot safely provide that bound.

Use expanded bounds/occupancy and subtract a conservative margin before primary traversal. Include dynamic objects and transparent surfaces where required. Keep CPU gameplay raycasts unchanged. Reject the prepass if overhead dominates or any thin object disappears.

## 10. Delivery order and decisions

1. **Streaming safety first:** S1/S2, then S3 and S4. These fit the current island plan and cap work/memory before content density increases.
2. **Bulk content/edit work:** P5 and E1/E3; begin with existing dense bricks. Preserve old semantics while removing per-voxel build/edit overhead.
3. **Compact authority and persistence:** P1, C1, E2. Ship CPU/disk gains even if GPU atlas storage remains preferable.
4. **GPU memory:** P2 and P4, then P3 behind one storage accessor. Finalize allocation retirement, normal layout, and shader parity before switching defaults.
5. **Compiled content and distance:** C3, W2/W4, then W1 where static traversal justifies it. Extend existing layer/page ownership.
6. **Remaining frame work:** R2 and E4. R1 is independently opt-in; R3/E5 remain later experiments.
7. **Packaging:** C2 only when current manifest/file organization demonstrably limits scale and its architecture decision changes.

Implementation starts with [S1a: staged voxel residency and upload readiness](streamed-rendering-s1a.md).
S1a is implemented: hidden residency, scheduling metadata and revision-qualified tickets.
[S1b](streamed-rendering-s1b.md) is implemented: global voxel content budgets,
deterministic ordering/aging, shared-map deduplication and atlas backpressure.
[S1c](streamed-rendering-s1c.md) integrates those tickets with existing v2
sector/proxy refinement and distance unloading. CPU collision/navigation stay
independent. V3 page selection and cross-layer groups remain separate work.
[S2a](streamed-rendering-s2a.md) is implemented for prepared geometry and
its registered asset copies: byte accounting, pinned users, LRU eviction,
per-key build suppression and Stop cleanup. Decoded-content/pending-result and
other owner byte bounds are split into further S2 slices.
[S2b](streamed-rendering-s2b.md) bounds decoded warm content and retained full/proxy
results with scoped leases, shared byte admission and deferred retry hints.
Live decoded leases and one sole oversized pending result expose explicit
pressure exceptions. Temporary decode/build memory and other owner byte bounds,
queue partitioning and mid-decode cancellation remain S2 work. These are owner
budgets, not a total process memory ceiling. The remaining proposals keep the
delivery order above.
[S3a](streamed-rendering-s3a.md) is complete for current v2 observer selection.
It preserves existing policy and main-thread stage ownership.
[S3b](streamed-rendering-s3b.md) completes incremental GPU scene records at the
manager boundary. [S3c](streamed-rendering-s3c.md) completes structural revisions
and cached bridge membership, including implementation review and native checks.
Complete value-dirty extraction and future layer selection keep S3 partial.

Decisions to settle before dependent implementation:

- Compact CPU API, editable representation, and immutable ownership; preserve direct consumer access until migrated.
- Pool layout, prefix counts, capacities, retirement, and exact preservation of normal bits.
- Codec dependency/version, checksums, payload versions, and compatibility with existing Gekko content contracts.
- Asset LOD material/transparency rules and collision/source lattice references.
- Progressive versus atomic large-edit publication and asynchronous physics readiness.
- Any binary manifest/region pack or unified-world-grid proposal that changes the island plan.

## 11. Verification and review limits

The proposal review checked source symbols, existing plans, byte-layout arithmetic,
and document links. Implementation verification is recorded per slice, starting
with [S1a](streamed-rendering-s1a.md#execution-record),
[S1b](streamed-rendering-s1b.md#verification-and-execution-record) and
[S1c](streamed-rendering-s1c.md#execution-record) and
[S2a](streamed-rendering-s2a.md#execution-record) and
[S2b](streamed-rendering-s2b.md#execution-record) and
[S3a](streamed-rendering-s3a.md#execution-record) and
[S3b](streamed-rendering-s3b.md#execution-record) and
[S3c](streamed-rendering-s3c.md#execution-record). Native smoke checks establish
the recorded rendering/streaming contracts; they do not establish performance
gains or rendered pixel parity.

Implementation slices should use the smallest existing build/check and manual scene relevant to their changed contract: fixed-view render parity, edited chunk seams, shared-instance isolation, delayed parent/child handoff, save/reload, and locomotion after edits. The user explicitly authorized functionality tests and the tests-first subagent workflow for this implementation. Other test changes still follow [workspace instructions](/Users/ddevidch/code/go/gekko3d/AGENTS.md).

Related Gekko plans: [island streaming](../content/island-streaming.md), [XBrickMap hot path](../renderer/xbrickmap-hotpath-optimization-plan.md), [uniform materials](../renderer/xbrickmap-uniform-material-plan.md), [quality-preserving optimization](../renderer/quality-preserving-optimization-plan.md), and [renderer change guide](../renderer/change-guide.md).
