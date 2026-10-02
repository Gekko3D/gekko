# Streamed Rendering and Content Optimization Proposals

Date: 2026-10-02. Status: staged implementation; S1a/S1b/S1c, S2a/S2b, and S3a/S3b/S3c/S3d/S3e/S3f/S3g/S3h/S3i/S3j complete. S2 and S3 remain partial; other sections are proposals.

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
- [Caching](../../runtime_content_loader.go): S2b adds decoded-content byte budgets, scoped leases, in-flight suppression, and shared pending-result admission. [Prepared geometry](../../streamed_level_geometry_cache.go) has S2a byte accounting and pinned users. Live decoded leases and one sole oversized pending result expose pressure exceptions; GPU retention and other owner budgets remain further S2 work. These are not total process memory limits.
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

## 5. Streaming changes

### S1. Global budgets, priority queues, and renderer readiness

Finish island readiness/uploads before more detail. Global per-frame bytes and brick/record limits span all objects; S1b implements content caps. Allocation/migration/lookup remain outside cap.

Budget commits by bytes, bounded units and time. Between-chunk timer cannot bound one huge commit. Stage hidden chunks; resume uploads across frames.

Use island ticket/generation/revision readiness: geometry, materials, lookup and all required uploads. Retain parents/proxies until complete replacement cohort/group ready. Hidden-for-upload differs from excluded-from-upload.

Deterministic prepare/commit/upload/retry/retain/retire queues with stable ties/aging. Reserve collision/detail progress; proxy-first drain must not starve gameplay/detail. Atlas/pool pressure causes backpressure/eviction, never atlas-full panic.

Owners: streamed runtime, renderer bridge, `app_voxel_residency.go`, GPU upload manager. Acceptance: global limits hold for many objects; delayed/failed uploads and teleports retain valid coverage and safe collision.

### S2. Bound caches and worker throughput by bytes

Budget decoded content, prepared geometry, pending results, retained GPU data, normals, materials and editable patches by bytes. Bound `RuntimeContentLoader`; geometry eviction alone leaves source arrays resident.

Pin live render/collision/navigation/fallback users; evict unreferenced detail first. Count shared backing once globally; retain per-user admission costs.

Suppress per-key concurrent IO/decode/build. Normalize paths; compiled data uses content IDs. Separate bounded IO, decode/generation and navigation queues. Cancel obsolete demand; discard stale generations.

Retain explicit empty-page metadata. Empty skipping must honor placements, backing, overrides and collision interest. No-IO emptiness requires every relevant source empty.

Acceptance: total cache/pending memory stays bounded while traveling; concurrent requests reuse one result; gameplay-only chunks need no GPU admission.

### S3. Incremental selection and scene gathering

Status: partial. S3a–S3j implement selection, GPU records, ECS inventories, hierarchy reuse and component/hierarchy/helper/animation publication. Commits/designs: [delivery record](#completed-work). Contracts: [streaming docs](../content/streaming-and-worlds.md), [renderer runtime](../renderer/runtime.md), [ECS docs](../engine/ecs.md).

Physics, gameplay and other renderer-input notifications, bounded entity worklists and incremental extraction remain S3 work. Preserve compatibility for untracked public-field writes. Hierarchy/renderer still read live values. Nonempty caches may retain peak capacity; no general byte ceiling or frame-time gain.

Cache by spatial bucket, radii, layer transform/topology and PVS. Update shells; recompute after teleports, observer/radius changes, edits and visibility. Count overlapping demand so one observer cannot evict another's content.

Stable entity/object table: extend comparisons with dirty registration, hierarchy/removal/visibility notifications. Separate camera culling/LOD from content changes.

No automatic Bevy `Changed<T>` in Gekko. Publish versions/events at mutation owners before incremental extraction. Preserve hierarchy/flush order for animation/brushes. Rebuild GPU records/BVH only for relevant changes.

Acceptance: idle observers do not rebuild residency sets; idle geometry does not rebuild records; movement and ancestor changes appear at current stage boundary.

### S4. Remove synchronous persistence from unload

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
| S3j | This commit | Authored animation final pose publication | [ECS contract](../engine/ecs.md#authored-animation-publication) |

S2/S3 partial. S1c covers v2; v3 selection/cross-layer groups separate. S2 allows live leases/sole oversized pending pressure; temporary builds/other owners remain open. Producer notifications/incremental extraction remain S3. Next: physics World publication in `SynchronousPhysicsSystem` and `PhysicsPullSystem` (`mod_physics_module.go`).

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

This commit: `feat(assets): publish authored animation poses`. Sol 6.1 TDD and
both adversarial reviews completed. Eight focused test groups, root tests,
focused race, ActionGame compilation and editor build passed:

```sh
# gekko/
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3j' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(AuthoredAssetAnimation|NPCAnimation|S3i|TransformHierarchy)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S3i|S3j|AuthoredAssetAnimation|NPCAnimation|TransformHierarchy)' -count=1
```

Consumer commands for these steps:

```sh
# actiongame/
env GOCACHE=/tmp/gekko3d-gocache go test ./... -run '^$'
# gekko-editor/
env GOCACHE=/tmp/gekko3d-gocache go build ./...
```

Existing tests preserved. macOS linker/module stat-cache warnings exited successfully. Notification-only changes needed no new windowed smoke/engine sweep. Unrelated editor/sample baselines not rerun; see S3c/S3d. Publication/compilation verified; incomplete producers still require live extraction.

Verify each slice with smallest relevant build/check and manual scene: fixed-view parity, edited seams, instance isolation, delayed handoff, save/reload and edited locomotion. User authorized functionality tests/tests-first subagents for implementation. Other test changes follow [workspace instructions](/Users/ddevidch/code/go/gekko3d/AGENTS.md).

Related Gekko plans: [island streaming](../content/island-streaming.md), [XBrickMap hot path](../renderer/xbrickmap-hotpath-optimization-plan.md), [uniform materials](../renderer/xbrickmap-uniform-material-plan.md), [quality-preserving optimization](../renderer/quality-preserving-optimization-plan.md), and [renderer change guide](../renderer/change-guide.md).
