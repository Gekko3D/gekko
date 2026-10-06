# Gekko Island Streaming Plan

## Metadata

- Owner: `gekko` streaming/renderer and `actiongame` integration
- Target: one playable 15,000 x 15,000 m finite island containing villages,
  refineries, factories, bunkers, and other Gasworks-sized points of interest
  (POIs)
- Status: implementation plan; terrain/`.gkworld` v3, renderer-residency,
  harness, and reference acceptance contracts finalized below
- Generation dependency: none; the runtime and content contract should be ready
  before island generation exists
- Companion gameplay plan:
  [`actiongame/docs/island-strategy-navigation.md`](../../../actiongame/docs/island-strategy-navigation.md)

## Goal

Render a finite island without terrain or POI masses appearing out of empty
space as the player approaches. Streaming may improve detail, collision,
destruction, navigation, and interiors, but it must never reveal previously
missing world coverage.

The core invariant is:

> Streaming refines an already-visible parent representation; it never replaces
> empty space with a newly loaded level part.

The runtime must also remain bounded while the player traverses the whole island:

- bounded main-thread commit and GPU upload work per frame
- bounded decoded/prepared CPU memory
- bounded retained GPU memory
- bounded preparation queues
- local collision, destruction, navigation, AI, and interior residency

## Non-goals

- Island or terrain generation itself
- Infinite-world streaming
- An engine-wide ECS rewrite
- A generic streaming framework unrelated to the island use case
- LOD cross-fading in the first implementation
- A new imported-world voxel payload format before profiling shows that its
  decode or I/O is limiting. A tiled height payload for 15 km terrain is in
  scope because the existing filled-column JSON representation is not viable at
  this scale.

## Architecture Decision

Keep ECS as the control plane:

- observers and POI roots are ECS components
- loaded render pages/chunks have ECS ownership and lifetime
- the main thread performs the final world/entity commit

Keep bulk work outside ECS entity iteration:

- one `StreamedLevelRuntimeState` owns selection, priority, queues, generations,
  caches, and budgets
- workers load/decode/build immutable geometry
- the renderer owns GPU upload progress and readiness
- one ECS entity represents a render page or detailed chunk, never a voxel

Treat island terrain and POIs as independent streaming layers:

- terrain uses tiled height data and multi-resolution sparse surface pages
- POIs retain their existing high-resolution imported-world voxel chunks
- each layer keeps its own chunk size, resolution, material set, page forest,
  collision source, and save identity
- selection and priority operate on world-space bounds, never by assuming that
  terrain and POI chunk coordinates are interchangeable

No replacement streaming architecture is required. The existing worker/commit,
generation-check, proxy/full, and split-residency paths are the foundation. The
selector, proxy granularity, budgets, and renderer handoff need targeted rework.

Global squad movement is not part of this renderer/content scheduler. Actiongame
owns one always-resident strategic road/site graph and persistent squad records.
Gekko owns only the terrain, collision, POI, and tactical-navigation residency
needed when a squad becomes local. The exact handoff is specified in the
[companion navigation plan](../../../actiongame/docs/island-strategy-navigation.md).

Keep page geometry in page-local coordinates and reuse the renderer's existing
camera-relative render origin. At the padded +/-8,192 m bounds, `float32`
world-position precision remains far below the 0.1 m POI voxel size; a new
double-precision ECS coordinate system is not required for this island target.

## 15 Km Scale Decision

The playable land bounds are 15,000 x 15,000 m, centered at the world origin.
The page forest is padded with ocean/background coverage to a 16,384 x 16,384 m
square so every hierarchy level divides cleanly. The camera may see the whole
island silhouette from elevated or offshore viewpoints.

The island cannot be one 0.1 m voxel world. A 15 km square contains 150,000 x
150,000 source columns at that resolution: 22.5 billion columns before terrain
depth, bricks, normals, collision, or POIs. Chunking makes residency local but
does not fix the asset/manifest size of that representation.

Use this split instead:

- **Terrain source:** 128 x 128 `uint16` height tiles at 2 m sample spacing;
  each source tile spans 256 m and is independently loadable.
- **Terrain rendering:** workers interpolate height tiles into sparse surface
  `XBrickMap` pages. Distant pages never materialize solid volume below the
  visible surface.
- **Terrain collision:** query resident height tiles directly except where an
  active POI replacement or edited voxel patch owns the surface.
- **Outdoor navigation source:** store one compact 1 m exclusion bit per
  playable terrain cell; build bounded tactical walk tiles from heights and
  exclusions only around active gameplay.
- **Editable terrain:** materialize a private 0.5 m voxel patch only on the first
  edit inside destruction residency, then persist it through the existing
  world-delta ownership model.
- **POIs:** keep authored/imported geometry such as Gasworks at 0.1 m resolution
  in `.gkworld` pages. POI collision, destruction, interiors, and PVS remain
  independent from terrain residency.

The current startup checks that require `level`, terrain, and imported-world
chunk sizes/resolutions to match must be removed. Replace them with per-layer
validation plus world-space bounds validation. The current heightfield source
may remain an authoring input, but runtime streaming consumes tiled payloads;
it must not parse one giant JSON `height_samples` array.

At the padded bounds, the source grid is exactly 64 x 64 tiles. Its uncompressed
height body is 128 MiB, plus at most 8 MiB if every tile has a replacement mask
and 32 MiB for complete 1 m outdoor-navigation exclusion masks. The complete
default root/macro/regional height hierarchy adds about 162 MiB, excluding small
headers and uncommon masks. There are at most 64 root, 1,024 macro, and 16,384
regional page records, but only roots and camera-local rings are decoded/rendered.
Roughly 330 MiB of raw terrain heights/masks is ordinary finite asset data; it is
not proportional to terrain depth and is not all runtime-resident.

At the 50 m/s profile limit, the observer crosses a 128 m regional page no more
often than every 2.56 seconds and a 256 m source tile every 5.12 seconds. The
multi-kilometre prefetch rings therefore provide substantial scheduling lead
time during continuous travel; teleporting remains a separate gated case.

### Terrain And Multi-level POIs

The height layer owns natural ground only. It is not used for factory floors,
roofs, pipe racks, warehouse interiors, basements, tunnels, caves, or bunker
rooms. Those remain ordinary full 3D `.gkworld` content and may extend above and
below several terrain tiles.

At close range, the height layer provides grade and landscape continuity. POI
voxels provide foundations, curbs, stairs, loading bays, floors, catwalks,
pipework, roofs, and every stacked walkable level. Interpolating a 2 m height
source to 0.5 m rendering makes the surface smoother; it does not invent those
features.

Each POI uses one of two terrain interfaces:

- `overlay`: the island bake applies the POI's cut/fill/flatten stamp to height
  tiles. Terrain supplies the final ground pad; the POI supplies structures and
  interior floors. This is the default for villages, factories, warehouses, and
  refineries.
- `replace_surface`: the height-tile mask removes natural ground inside an
  excavation/entrance footprint. The POI supplies all replacement visible ground
  and collision in that footprint. Use this only for bunkers, tunnels, pits, and
  other geometry that crosses the terrain surface. Its replacement ground
  overlaps the mask boundary by at least one 2 m source sample so filtering and
  voxelization cannot expose a seam.

The stamp and replacement mask are generator/baker inputs, not a second runtime
terrain system. A masked height tile stores one validity bit per height sample;
unmasked tiles pay no mask cost. Pages participating in a replacement share a
`coverage_group` ID. The scheduler makes terrain-cutout visibility, replacement
POI visibility, and their collision ownership active in one atomic handoff after
every member is ready. Until then the uncut parent ground remains visible and
collidable.

This keeps height collision authoritative outside POI replacement footprints and
voxel collision authoritative inside them. It also prevents a bunker entrance
from opening onto empty space while its detailed POI is still uploading.

## Target Residency Model

Use independent finite two-dimensional page forests over the island's X/Z
plane. POIs keep current storage chunks as leaves; terrain leaves are height
tiles or local editable patches.

```text
root pages: whole-island mass + coastline + POI landmark silhouettes
        | always resident before gameplay starts
        v
macro pages: mountains, coast, settlements and large POI mass
        | streamed around the camera over several kilometres
        v
regional pages: terrain form and POI exterior shape
        | streamed by distance and predicted movement
        v
local terrain surfaces + full POI visual chunks
        | near the player and selected POIs only
        v
collision -> destruction -> navigation/interiors/gameplay
        progressively smaller local residency sets
```

The `island_reference` profile uses these exact starting spans; the schema still
allows other powers-of-two page trees:

| Tier | Span / visual resolution | Residency | Contents |
| --- | ---: | --- | --- |
| Root | 2,048 m / 16 m | All 64 roots before gameplay | Whole-island terrain, coastline and POI silhouettes |
| Macro | 512 m / 4 m | Multi-kilometre ring | Mountains, coast, villages and large POI mass |
| Regional | 128 m / 1 m | Near visual ring | Terrain form and POI exteriors |
| Local terrain | 64 m / 0.5 m | Generated only near gameplay | Fine surface, collision/edit patch as required |
| Full POI | 25.6 m / 0.1 m | Near/visible POIs | Existing authored/imported source geometry |
| Gameplay | At or inside full range | Local only | Collision, editable voxels, nav and interiors |

Default terrain payload grids are 128 x 128 at 16 m for roots, 128 x 128 at 4 m
for macro pages, and 64 x 64 at 2 m for regional pages. Regional workers
interpolate to the 1 m visual grid; local workers interpolate source tiles to the
0.5 m visual grid. The interpolation improves surface continuity, not source
detail.

Do not create one permanently resident proxy for every local or POI chunk.
Aggregate leaves into 512 m and 2,048 m pages. Also avoid one giant island voxel
object: 64 sparse root surfaces allow culling and keep individual uploads
bounded.

If root pages exceed the 15-second startup gate or 128 MiB pinned-root GPU
budget in the `island_reference` profile, make the root representation coarser.
Removing root coverage is not an acceptable optimization because it
reintroduces distant appearance.

## Shared Page Content Contract

Use the existing two manifests rather than introducing an island container:

- `.gkterrainmanifest` v3 owns height tiles and terrain render pages.
- `.gkworld` v3 owns POI voxel chunks, POI render pages, materials, backing, and
  optional indoor/PVS sectors.

Both manifests use one small page record so selection, validation, and handoff
do not need layer-specific hierarchy code. Pages use world-space bounds and
origins because the terrain and POI grids have different resolutions.

```go
const (
    StreamPageLevelLeaf uint8 = iota
    StreamPageLevelRegional
    StreamPageLevelMacro
    StreamPageLevelRoot
)

type StreamPageDef struct {
    Level            uint8                `json:"level"`
    BoundsMin        [3]float32           `json:"bounds_min"`
    BoundsMax        [3]float32           `json:"bounds_max"`
    Payload          StreamPagePayloadDef `json:"payload"`
    ChildPageIndices []uint32             `json:"child_page_indices,omitempty"`
    LeafEntryIndices []uint32             `json:"leaf_entry_indices,omitempty"`
    CoverageGroup    string               `json:"coverage_group,omitempty"`
    Tags             []string             `json:"tags,omitempty"`
}

type StreamPagePayloadDef struct {
    Kind                string     `json:"kind"`
    Path                string     `json:"path"`
    WorldOrigin         [3]float32 `json:"world_origin"`
    ChunkSize           int        `json:"chunk_size"`
    VoxelResolution     float32    `json:"voxel_resolution"`
    SampleSpacing       float32    `json:"sample_spacing,omitempty"`
    HeightOffset        float32    `json:"height_offset,omitempty"`
    HeightScale         float32    `json:"height_scale,omitempty"`
    PayloadHash         string     `json:"payload_hash"`
    PayloadSizeBytes    int        `json:"payload_size_bytes"`
    OccupiedSectorCount int        `json:"occupied_sector_count,omitempty"`
    OccupiedBrickCount  int        `json:"occupied_brick_count,omitempty"`
    Aux                 *ImportedWorldChunkAuxRefDef `json:"aux,omitempty"`
}

type ImportedWorldDef struct {
    // Existing world identity, material, backing, and source fields remain.
    Entries         []ImportedWorldChunkEntryDef `json:"entries,omitempty"`
    Sectors         []ImportedWorldSectorDef     `json:"sectors,omitempty"` // legacy API; custom wire dispatch
    IndexedSectors  []ImportedWorldSectorV3Def   `json:"-"` // v3 wire: sectors
    Pages           []StreamPageDef              `json:"pages,omitempty"`
    RootPageIndices []uint32                     `json:"root_page_indices,omitempty"`
}

type TerrainChunkManifestDef struct {
    // Existing terrain identity fields remain; grid fields describe shared source tiles.
    Entries         []TerrainChunkEntryDef `json:"entries,omitempty"`
    Pages           []StreamPageDef        `json:"pages,omitempty"`
    RootPageIndices []uint32               `json:"root_page_indices,omitempty"`
}

type ImportedWorldSectorV3Def struct {
    Coord                 TerrainChunkCoordDef `json:"coord"`
    BoundsMin             [3]float32          `json:"bounds_min"`
    BoundsMax             [3]float32          `json:"bounds_max"`
    FullChunkIndices      []uint32            `json:"full_chunk_indices,omitempty"`
    VisibilityID          string              `json:"visibility_id,omitempty"`
    SourceLeafIDs         []int               `json:"source_leaf_ids,omitempty"`
    VisibleSectorIndices  []uint32            `json:"visible_sector_indices,omitempty"`
    AdjacentSectorIndices []uint32            `json:"adjacent_sector_indices,omitempty"`
    Tags                  []string            `json:"tags,omitempty"`
}
```

`ImportedWorldChunkEntryDef`, `ImportedWorldLODDef`, and
`TerrainChunkEntryDef` gain the same optional `occupied_sector_count` and
`occupied_brick_count` cost fields. Terrain entries also gain `world_origin`,
`payload_kind`, `payload_hash`, and `payload_size_bytes`; their existing
`chunk_size` and `voxel_resolution` mean sample width and spacing in v3. Array
positions are compact manifest-local IDs. They are valid only with that
manifest's source hash and are never stored in saves or world deltas.

### Terrain Payloads

W4c1 implements the source tile codec, legacy heightfield-to-tile bake and v3
manifest tooling. The current runtime still rejects v3 until height collision
and edit-patch ownership are ready. See the
[implemented source contract](streaming-and-worlds.md#tiled-terrain-source-content-v3).
I07 adds the [terrain page bake](streaming-and-worlds.md#terrain-v3-page-baking),
with 256 m source tiles separate from the terminal 128 m regional visual pages.
W4c2 adds the [resident query foundation](streaming-and-worlds.md#resident-height-query-foundation);
it does not activate or complete I13/I14.

Terrain v3 adds one required payload kind,
`height_u16_binary_v1`. Reuse the existing binary-chunk framing: 8-byte
`GKHTIL1\n` magic, little-endian `uint32` JSON-metadata length, JSON metadata,
then the hashed payload. Metadata contains schema version, terrain ID, source
hash, tile coordinate, world origin, sample width/height, sample spacing,
height offset/scale, payload hash/size, `surface_mask_bytes`,
`outdoor_nav_cell_size`, and `outdoor_nav_exclusion_mask_bytes`.

The payload starts with `sample_width * sample_height` row-major, little-endian
`uint16` heights; both dimensions are positive and at most 128. Samples are
cell-centered; a surface builder reads the adjacent payload's edge sample when
filtering a boundary. This is followed by either zero bytes or exactly
`ceil(sample_width * sample_height / 8)` row-major validity-mask bytes; one means
terrain surface is present. Source tiles are 128 x 128 at 2 m. A source tile is
then followed by exactly 8,192 row-major outdoor-navigation exclusion bytes when
`outdoor_nav_exclusion_mask_bytes` is non-zero: one bit for each 1 m cell in its
256 x 256 m span, where one means infantry navigation is excluded by water,
void, static non-height geometry, an authored no-nav volume, or POI surface
ownership. Root/macro/regional render payloads set both outdoor-navigation fields
to zero. The `island_reference` profile requires a 1 m exclusion mask for every
playable source tile; non-island terrain may omit it. The file remains a
`.gkchunk`; payload kind selects its decoder.

Terrain root, macro, and regional page payloads use that same height format at
their own sample spacing. Workers convert a selected page into an immutable
sparse-surface `XBrickMap` at its target `voxel_resolution`; no terrain LOD is
stored as a filled volume. Local 0.5 m surfaces are worker-built and need not be
baked for the whole island.

The existing `.gkterrain` heightfield remains an authoring/legacy source. A v3
runtime level references the tiled manifest and does not load one island-wide
`height_samples` array.

`LevelTerrainDef.manifest_path` is required for v3 runtime terrain;
`source_path` becomes optional authoring provenance in that case. A legacy level
with only `source_path` keeps current eager/bake behavior. The fields already
exist in `.gklevel` v3, so this validation change does not require a level-schema
bump. Top-level level `chunk_size` and `voxel_resolution` remain defaults for
legacy/inline content; each v3 streamed manifest is authoritative for its layer.

### Page And Sector Rules

- A page ID is its index in `pages`; a leaf ID is its index in that manifest's
  `entries` for POI visual chunks. Terrain entries are shared source backing,
  separate from the visual forest.
- Every page has a payload, including roots. This is the coarser fallback used by
  the no-hole handoff.
- A POI page has either `child_page_indices` or `leaf_entry_indices`, never both.
  Terrain pages have no source-entry leaf references: root → macro → regional
  terminates at regional render payloads. Local surfaces are generated later.
- Parent levels are strictly coarser than child levels.
- Every page is reachable from exactly one listed root. Cycles, shared children,
  duplicate roots, and unowned non-empty POI visual leaves are invalid.
- Page bounds contain their payload coverage and every referenced child/leaf.
- Required root payloads together cover their layer's visible island content and
  are pinned after the startup gate.
- A layer may omit empty ocean branches, but its root payload must already show
  the coastline/background expected in that area.
- Non-empty `coverage_group` values identify the minimal cross-layer terrain/POI
  replacement set. No member changes visibility or collision ownership until all
  selected members are ready for the same streaming generation.
- Coverage-group IDs use the stable POI ID. Level validation requires at least
  one terrain member and one POI member with overlapping bounds; a group present
  in only one manifest is invalid.
- Sectors exist only in `.gkworld`. They select POI visibility; pages retain
  ownership of full visual chunks.
- Sector references use integer indices. PVS is limited to interiors and spaces
  where it rejects useful work.

### Versioning And Compatibility

- The `.gkworld` and `.gkterrainmanifest` schema versions advance independently
  to v3. Their writers switch only after their complete v3 paths validate.
- Readers continue to accept current checked-in v2 manifests.
- A v2 `.gkworld` normalizes into one root/leaf runtime page per sector. Its
  first existing LOD is the fallback when present; proxy-less sectors retain
  current full-chunk radius/PVS behavior.
- A v2 terrain manifest normalizes each non-empty entry as a legacy leaf. It has
  no guaranteed parent coverage, so it keeps current distance streaming rather
  than claiming the v3 no-hole contract.
- Version-1 or missing-version legacy imported worlds first take the current v2
  compatibility path. A missing terrain version retains current terrain-loader
  behavior. Missing versions are never promoted to a new serialized version.
- Newer unknown versions fail with an explicit unsupported-version error.
- Existing imported-world `.gkchunk` schema v1 and payload kinds stay unchanged.
  `height_u16_binary_v1` is a separate terrain payload kind with its own v1
  header; manifest versions never imply payload versions.
- Validation runs first on the serialized version and again on the normalized
  page forest. Optional cost counters may be added without a manifest bump;
  changing reference meaning or ownership requires one.

Do not add a binary manifest, visibility sidecar, generic asset pack, or second
page hierarchy format. Add one only if a 15 km harness proves manifest parsing
or file-open count is a measured bottleneck.

## Contract For The Future Island Generator

The future generator is only a producer of these existing layer contracts. It
must emit:

1. 256 m terrain height tiles at 2 m sample spacing
2. 1 m outdoor-navigation exclusion masks for every playable source tile
3. terrain regional, macro, and root height payloads
4. 0.1 m `.gkworld` leaves for authored/generated POIs, plus their page forest
5. explicit landmark proxy source for chimneys, towers, cranes, and other narrow
   POI silhouettes that generic downsampling would erase

It must not bake 0.5 m or 0.1 m terrain over the complete 225 km² footprint.
Local terrain surfaces/edit patches are runtime-generated only where gameplay
requires them. Terrain roots show terrain/coast; POI roots show landmark mass.
Both root sets are loaded before gameplay, without duplicating geometry between
the layers.

Gasworks is the first POI payload in the temporary streaming harness. The
harness builds simple deterministic terrain and explicit page metadata; island
generation remains out of scope for streaming preparation.

## No-hole Handoff Contract

A committed ECS entity is not proof that its voxels are renderable. Track page
progress through:

```text
absent -> preparing -> CPU ready -> GPU uploading -> GPU ready -> visible
```

Refinement:

1. Keep the parent visible.
2. Prepare and commit all selected children.
3. Wait until the renderer reports all child coverage as GPU-ready.
4. Publish child visibility and parent hiding in the same render snapshot.
5. Keep the parent resident as a fallback until the children enter keep
   residency.

Unloading is the reverse:

1. Ensure the parent already has a current GPU-ready ticket.
2. Show the parent and hide/remove the children in one ECS command flush.
3. Let the later same-thread renderer bridge consume that completed command set.

If I/O, decode, preparation, or GPU upload misses its deadline, retain the
coarser parent. The failure mode is reduced detail, never a hole. LOD snapping
is acceptable for the first version; add dither/cross-fade only if the completed
handoff is still visually distracting.

[S1c](../roadmaps/streamed-rendering-s1c.md) applies this readiness handoff to the
existing v2 imported sector/proxy runtime. Its cohort uses `FullChunkRefs`; the
v3 forest, startup root gate and cross-layer groups described here remain future
work. CPU-only runtimes retain their existing residency behavior.

## Renderer Residency Contract

### Why The Current Hidden State Is Insufficient

`VoxelRenderHiddenComponent` currently causes `voxelRtSystem` to skip the ECS
entity and remove any existing `core.VoxelObject` from the renderer scene. That
is correct for ordinary hidden objects, but a streamed child cannot upload while
hidden if it is absent from `Scene.Objects`.

Keep that existing behavior for ordinary entities. A streamed page/chunk gains
a separate residency marker which means:

- keep the voxel object in `Scene.Objects`
- allow `UpdateVoxelData` to allocate/upload it
- exclude it from visible, transparent, shadow, occlusion, and BVH lists while
  `VoxelRenderHiddenComponent` is present

Add these generic scheduling fields to `core.VoxelObject`:

```go
RenderEnabled      bool   // true by default
VoxelUploadPriority uint8 // lower uploads first
VoxelUploadOrder    uint64 // stable tie-breaker
```

`Scene.Commit` skips render-disabled objects when building all
visible/shadow/BVH lists, while GPU voxel residency continues to iterate the
complete `Scene.Objects` collection. The bridge copies component priority and
ticket ID into the upload fields. Ordinary non-streamed objects use visible
priority and then map ID as their stable order. The global queue orders work by
priority, upload order, map ID, and sector coordinate.

### Minimal ECS And Renderer API

The streamed runtime owns monotonically increasing ticket IDs. It adds this
component when committing a page or full chunk hidden for upload:

```go
type StreamedVoxelPriority uint8

const (
    StreamedVoxelPriorityFallback StreamedVoxelPriority = iota
    StreamedVoxelPriorityCollision
    StreamedVoxelPriorityVisible
    StreamedVoxelPriorityPrefetch
    StreamedVoxelPriorityKeep
)

type StreamedVoxelRenderComponent struct {
    Ticket     uint64
    Generation uint64
    Priority   StreamedVoxelPriority
}
```

The component remains for the entity's streamed lifetime. It is not a general
rendering component and is not serialized into content.

`VoxelRtState` owns ticket status:

```go
type StreamedVoxelRenderState uint8

const (
    StreamedVoxelRenderPendingBridge StreamedVoxelRenderState = iota
    StreamedVoxelRenderUploading
    StreamedVoxelRenderReady
    StreamedVoxelRenderCancelled
    StreamedVoxelRenderFailed
)

type StreamedVoxelRenderStatus struct {
    State          StreamedVoxelRenderState
    Entity         EntityId
    Generation     uint64
    MapID          uint32
    TargetRevision uint64
    PendingSectors int
    PendingBricks  int
    Failure        string
}

func (s *VoxelRtState) StreamedVoxelStatus(ticket uint64) (StreamedVoxelRenderStatus, bool)
func (s *VoxelRtState) ForgetStreamedVoxel(ticket uint64)
```

No callback, future, channel, or renderer-facing ECS event is needed. Streaming
polls the status resource on the following frame. `Ready` is latched for that
ticket; later destruction edits use the existing dirty-upload path and do not
regress initial residency.

Lower priority values upload first; ticket ID is the stable tie-breaker. The
GPU manager exposes one narrow predicate used by `VoxelRtState` after
`RtApp.Update()`:

```go
func (m *GpuBufferManager) VoxelObjectReady(
    obj *core.VoxelObject,
    xbm *volume.XBrickMap,
    targetRevision uint64,
) (ready bool, pendingSectors int, pendingBricks int)
```

The predicate is constant-time: it reads allocation state, dirty-work counts,
material state, and topology revisions. It does not scan the scene, manifest,
or all map sectors.

### Ownership And Frame Order

There is one writer for ticket transitions: the voxel renderer bridge/update
path on the main thread. Streaming owns ticket IDs and ECS components, reads the
prior frame's renderer status in `PreUpdate`, and only deletes terminal entries
through `ForgetStreamedVoxel`.

1. In `Update`, streaming commits the entity hidden with a new ticket ID. There
   is deliberately no status entry until the renderer sees the component.
2. In `PreRender`, `voxelRtSystem` creates the `PendingBridge` entry, adopts the
   entity even though it is hidden, creates/reuses its `core.VoxelObject`, sets
   `RenderEnabled = false`, captures the map ID/revision, and changes the ticket
   to `Uploading`.
3. Later in `PreRender`, `voxelRtUpdateSystem` calls `RtApp.Update()`. Scene
   commit omits render-disabled objects from render/shadow/BVH lists, while
   `UpdateVoxelData` services them through the global upload queue.
4. Immediately after `RtApp.Update()`, `voxelRtUpdateSystem` evaluates
   `VoxelObjectReady` for uploading tickets and latches successful ones as
   `Ready`.
5. On the next frame, streaming may perform the atomic visibility swap. The
   later `voxelRtSystem` sets the children render-enabled and the parent
   render-disabled before that frame's scene commit and render.

This ordering needs no channel, callback, mutex, GPU fence, or new ECS system.

### Exact Meaning Of GPU-ready

A ticket changes to `Ready` only after one renderer update has established all
of the following:

1. The ECS entity maps to a `core.VoxelObject` using the same `XBrickMap` and
   target revision captured when the bridge adopted the ticket.
2. `GpuBufferManager.Allocations[xbm]` exists.
3. The map is no longer structure-dirty, and its allocation counts cover the
   target map's sector and brick-pointer tables.
4. No target dirty sector or dirty brick remains in the global upload queue.
5. The object's material allocation is current.
6. `lastSectorGridTopologyRevision == sectorTopologyRevision`, so the uploaded
   sectors are addressable by rendering.

When a whole sector upload also uploads all its bricks, it must clear the
covered dirty-brick entries. Otherwise the same bricks are uploaded again and
the readiness predicate cannot settle.

`Ready` means all required `Queue.WriteBuffer` operations and lookup updates
have been queued before the next visibility change. It does not wait for a GPU
completion fence: the child remains render-disabled during that update, and
WebGPU queue ordering guarantees those writes precede the later frame that makes
the child visible.

Staged geometry is immutable. If its geometry pointer or revision changes
before `Ready`, mark the ticket `Cancelled` and issue a new ticket. Missing
geometry/material data marks it `Failed`; streaming logs the failure and keeps
the parent visible.

### Ticket State Transitions

```text
ECS commit hidden
    -> no renderer status
    -> PendingBridge  (bridge observes component)
    -> Uploading
    -> Ready          (latched; visibility may toggle while resident)

PendingBridge/Uploading
    -> Cancelled  (generation changed, entity removed, geometry replaced)
    -> Failed     (asset/material/renderer adoption failed)

Ready/Cancelled/Failed
    -> forgotten  (streaming calls ForgetStreamedVoxel after cleanup)
```

End-to-end page state remains:

```text
absent
  -> preparing
  -> CPU ready
  -> committed hidden
  -> PendingBridge
  -> Uploading
  -> Ready hidden
  -> visible
```

### Atomic Parent/Child Swap

Refinement:

1. Commit every selected child with both `VoxelRenderHiddenComponent` and
   `StreamedVoxelRenderComponent`.
2. Wait until every required child ticket is `Ready` with the current streaming
   generation.
3. In one ECS command flush, remove `VoxelRenderHiddenComponent` from all
   children and add it to the parent.
4. `voxelRtSystem` applies the final visibility set before the next
   `Scene.Commit`; there is no render between individual ECS commands.
5. Keep the ready parent staged and hidden until its keep residency expires.

Coarsening uses the same operation in reverse. The parent must already have a
current `Ready` ticket. One ECS command flush shows the parent and hides or
removes the children. No additional renderer acknowledgement is required because
the bridge and render update run later on the same main thread and see only the
completed command set.

Unknown, pending, or uploading children simply keep the parent visible. If a
child becomes cancelled, failed, or stale, clean up that ticket and reschedule
only its missing coverage. Never partially swap a page.

Startup uses the same contract without a parent: commit all required root pages
hidden, wait for their current tickets to become `Ready`, then unhide the roots
and open the gameplay gate in one ECS command flush.

## Named Harness, Commands, And Pass Gates

The names and values in this section are implementation defaults, not examples.
The offline harness builder exists. Live v3 admission and the marker-driven
Actiongame benchmark runner remain pending; the benchmark commands below are
acceptance recipes until those runtime pieces exist. The existing Gasworks
baseline command works now.

### Fixture Assets

Keep the existing Gasworks asset unchanged and generate this deterministic
fixture on demand. Check in the builder and fixture recipe, not the generated
15 km payload tree:

```text
actiongame/assets/levels/island_streaming_harness/
  island_streaming_harness.gklevel
  terrain/<generation>/island_streaming_harness.gkterrainmanifest
  terrain/tiles/<generation>/ # 2 m heights plus 1 m exclusions along benchmark routes
  terrain/pages/<generation>/ # root, macro, and regional height payloads
  worlds/poi/<generation>/island_streaming_harness.gkworld
  worlds/poi/pages/<generation>/ # regional/macro/root opaque proxies
```

The fixture has 15,000 x 15,000 m playable bounds in a 16,384 m hierarchy. It
contains all 64 terrain root pages, one Gasworks POI, and macro/regional/height
branches along every benchmark route and destination. Unvisited areas remain at
root detail; the fixture does not need to duplicate a generated island just to
exercise scale, traversal, eviction, and no-hole handoff. Full POI entries
reference the existing payloads under
`actiongame/assets/levels/gasworks/worlds`; do not copy that asset tree.

The `.gklevel` owns benchmark markers. Marker kind is
`stream_benchmark_waypoint`; tags encode `route:<name>`, `order:<NNN>`, and,
where needed, `speed:<meters-per-second>`. This keeps route coordinates in the
fixture rather than in Actiongame code.

Run the deterministic fixture builder from `gekko/`:

```sh
go run ./cmd/islandstreamharness \
  -gasworks ../actiongame/assets/levels/gasworks/worlds/gasworks.gkworld \
  -out ../actiongame/assets/levels/island_streaming_harness \
  -playable-span 15000 \
  -coverage-span 16384 \
  -root-span 2048 \
  -macro-span 512 \
  -regional-span 128 \
  -height-tile-span 256 \
  -height-sample-spacing 2
```

The builder may write only the named harness directory. Identical input must
produce byte-identical manifests, payloads, IDs, and route markers. It is a
temporary fixture builder, not the future island generator.

`content/derived.BuildIslandStreamHarness` owns this recipe. It emits flat
height zero inside the playable square and height -16 outside it, with height
calibration offset -64 and scale 128. Source tiles cover 640 m corridors around
continuous routes, teleport destinations, and the POI footprint. Every refined
root has all 16 macro children; every refined macro has all 16 regional children.
Unvisited regions therefore retain a complete fallback partition. Each source
tile carries 1 m navigation exclusions for the padded exterior and full POI
entry footprints. Gasworks uses overlay ownership; the fixture does not infer
a replacement surface mask or cross-layer coverage group.

Original voxel centers are aggregated independently at each POI tier, retaining
opaque value/material pairs with deterministic dominance and tie breaking.
Water and transparent materials are excluded from proxies. The builder streams
one original chunk at a time, requires source cells and chunk spans to align
with the page grids, and caps source tiles and input entries at 4,096 each,
combined terrain/POI pages at 32,768, and declared nonempty input cells at
64 million. Terrain planning checks the page budget before allocating the full
partition list. Original FULL payloads, auxiliary files and optional solid
backing remain borrowed references. Publication qualifies the backing metadata
and rebases its path to the original file.

`content.LoadImportedWorldChunkEntry` qualifies a single binary entry and its
auxiliary data against owner, coordinate, bounded grid, hash, size and count
before expansion. `SaveIslandStreamHarness` repeats source qualification,
rejects output overlap with borrowed files, symlinks inside the output, draft
mutation and immutable-file collisions. Generated files publish under their
generation; the fixed root level publishes last. A failed rebuild may leave
unreferenced new files but cannot rewrite the previous generation.

Markers also encode `hold:10`, `yaw:360`, `teleport:true`, and
`destination_wait:true` where required. The level carries
`fixture_recipe:island_stream_harness_v1`; marker transforms use an identity
rotation. Native walkability at the POI destination and route execution still
need the gameplay runner. Root height body size is an offline storage count;
it does not establish the pinned-root GPU memory budget.

### Reference Island Profile

Add an Actiongame profile named `island_reference`. Its minimum reference class
is 8 modern CPU cores, 16 GiB system memory, 8 GiB GPU-accessible or unified
memory, and an SSD. Benchmark results record the exact machine identity; a
release hardware profile may tighten or replace these provisional development
budgets later.

| Setting | Value |
| --- | ---: |
| Target frame rate / viewport | 60 Hz / 1,920 x 1,080 |
| Depth mode / camera far distance | Reverse-Z / 25,000 m |
| Maximum continuous route speed | 50 m/s |
| Macro desired / keep / forward-prefetch distance | 6,144 / 7,168 / 8,192 m |
| Regional desired / keep / forward-prefetch distance | 1,536 / 2,048 / 2,560 m |
| Local-terrain desired / keep distance | 512 / 640 m |
| Full-POI desired / keep distance | 384 / 512 m |
| Collision / destruction distance | 192 / 64 m |
| Outdoor/detailed nav desired / keep / forward-prefetch distance | 384 / 512 / 640 m |
| Preparation jobs | 6 |
| Page/chunk commits per frame | 1 |
| Main-thread streaming commit budget | 2 ms/frame |
| GPU voxel upload budget | 16 sectors and 4 MiB/frame globally |
| CPU-ready/prepared queue budget | 128 MiB |
| Decoded plus prepared cache budget | 512 MiB |
| Outdoor navigation cache budget | 128 MiB within total CPU budget |
| Total streaming-owned CPU budget | 768 MiB |
| Retained voxel GPU budget | 1,024 MiB |
| Pinned root-page GPU budget | 128 MiB |

All roots remain pinned. Regional/full cache use may shrink under pressure.
Changing these numbers is a profile change recorded with benchmark results; it
does not permit a coverage hole, partial LOD swap, or unready collision.

### Commands

Capture the current implementation baseline from `actiongame/` with Gasworks:

```sh
env \
  GEKKO_ACTIONGAME_LEVEL=assets/levels/gasworks/gasworks.gklevel \
  GEKKO_STREAMING_MAX_PREPARE_JOBS=4 \
  GEKKO_STREAMING_MAX_COMMITS_PER_FRAME=1 \
  GEKKO_STREAMING_MAX_COMMIT_MS=2 \
  GEKKO_STREAMING_METRICS_INTERVAL_MS=250 \
  GEKKO_SLOW_FRAME_MS=16.67 \
  go run . 2>&1 | tee /tmp/gekko-stream-gasworks-baseline.log
```

Step 0 adds `streaming_harness.go`, which follows fixture markers, records one
JSON result, prints `ACTIONGAME_STREAMING_BENCHMARK_RESULT`, requests the normal
`Quit` state, and is otherwise inactive. After the harness exists, run from
`actiongame/`:

```sh
env \
  GEKKO_ACTIONGAME_LEVEL=assets/levels/island_streaming_harness/island_streaming_harness.gklevel \
  GEKKO_STREAMING_PROFILE=island_reference \
  GEKKO_STREAMING_BENCHMARK_ROUTE=scale_crossing \
  GEKKO_STREAMING_BENCHMARK_RESULT=/tmp/gekko-stream-scale-crossing.json \
  GEKKO_STREAMING_BENCHMARK_EXIT=1 \
  GEKKO_STREAMING_METRICS_INTERVAL_MS=250 \
  GEKKO_SLOW_FRAME_MS=16.67 \
  go run .
```

Run that command once for each route, changing the route and result filename:

| Route | Required behavior |
| --- | --- |
| `boot_pan` | Hold the startup gate, then rotate 360 degrees from the high west viewpoint. |
| `scale_crossing` | Cross the full 15 km playable span at 50 m/s, then settle for 10 seconds. |
| `regional_loop` | Follow a 4 km loop through repeated macro/regional refinement and settle. |
| `poi_roundtrip` | Approach Gasworks from 3 km, enter it at player speed, return, and settle. |
| `boundary_ping_pong` | Cross the same regional/full boundaries ten times to exercise hysteresis. |
| `teleport_return` | Teleport at least 12 km and back; hold control until destination collision is ready. |

The benchmark runner may move the benchmark camera/player only. It must use the
same observer, streaming, collision, and renderer paths as normal gameplay.

### Result Measurements

Each result records the git revision, Go/OS/CPU/GPU identity, viewport, profile
values, route, and sample count, plus:

- frame, streaming observer/commit, renderer voxel-upload, and sector-grid
  durations: p50, p95, p99, and maximum
- startup-gate duration, per-handoff duration, oldest uploading-ticket age, and
  teleport destination-gate duration
- sectors and bytes uploaded per frame; upload- and commit-budget hit counts
- desired/resident root, macro, regional, local-terrain, full-POI, height-tile,
  collision, and destruction counts
- active/running/CPU-ready/GPU-uploading queue depths and bytes
- decoded, prepared, total streaming CPU, retained GPU, and pinned-root bytes:
  peak and post-route settled values
- ticket counts, failures, cancellations, cache hits/evictions, and save backlog
- coverage misses, parent-hidden-before-child-ready attempts, partial swaps,
  rendered-unready objects, root eviction attempts, and collision wait events

Instrumentation must be allocation-free per frame after its result slices are
preallocated. Percentiles exclude startup and a five-second post-gate warmup;
startup duration is reported separately.

### Pass Thresholds

Every route must satisfy all applicable gates:

| Gate | Pass threshold |
| --- | ---: |
| Coverage misses, premature parent hides, partial swaps, rendered-unready objects, root evictions | 0 |
| Ticket/prepare/commit/save failures | 0 |
| Collision wait events on non-teleport routes | 0 |
| Startup gate | <= 15 s |
| Teleport destination gate | <= 5 s |
| Streaming observer + commit CPU | <= 2 ms p99; <= 4 ms maximum |
| Renderer voxel-upload + sector-grid CPU | <= 2 ms p99; <= 4 ms maximum |
| Total frame work after warmup | <= 16.67 ms p99; <= 33.3 ms maximum |
| Normal-movement parent-to-child handoff | <= 500 ms p95; <= 1,500 ms maximum |
| Per-frame commit/upload work | Never exceeds the profile's count, time, or byte limits |
| Prepared queue, streaming CPU, retained GPU, pinned roots | Never exceeds the profile's byte limits |
| Post-route settle after 10 s stationary | No pending prepare/upload tickets; memory is within profile limits |

Correctness counters are hard gates on every machine. Performance values are the
`island_reference` target, not claims about unsupported hardware. If the target
machine cannot meet them, add a separately named hardware profile and retain the
same correctness gates; do not silently widen this result's thresholds.

## Implementation Steps

### Step 0: Freeze The Profile And Capture Baseline

- Freeze the serialized fields, payload framing, renderer ticket states, page
  spans, selection distances, memory budgets, fixture paths, routes, and pass
  gates in this document.
- Add `island_reference`: 1,920 x 1,080, reverse-Z, 25 km far plane, and every
  explicit budget above. Zero config values must not silently inherit the full
  visual distance.
- Add the dormant marker-driven benchmark runner and JSON result. It is active
  only when `GEKKO_STREAMING_BENCHMARK_ROUTE` is set.
- Run the named Gasworks baseline before scheduler or renderer changes. Record
  exact hardware, build, profile, and unavailable future counters.

Exit: ordinary Actiongame startup is unchanged; the baseline and exact
development machine identity are recorded; the result schema can represent all
acceptance counters.

### Step 1: Make Current Scheduling Deterministic

- Recalculate residency only after a selection-cell crossing, observer/profile
  change, or relevant visibility change.
- Seed indoor PVS from the observer's containing sector, not every sector in the
  load cube. Outdoor terrain never uses PVS.
- Replace Go-map iteration order with one `container/heap` priority queue:
  fallback coverage, player collision, visible detail, movement prefetch, then
  keep/cache work. Existing maps remain membership indexes.
- Count running, CPU-ready, and GPU-uploading work in the same bounded allowance;
  cancel stale generations before prepare and commit.
- Let camera/player observers request visuals. Ordinary NPCs request only shared
  local gameplay residency.
- Refresh expensive metrics only when emitted, using indexed desired sets.

Exit: Gasworks PVS has one containing-sector seed; nearby collision always wins;
a stationary observer does not rebuild sets or scan manifests every frame.

### Step 2: Bound Renderer Work And Define Readiness

- Implement the `StreamedVoxelRenderComponent`, `VoxelRtState` ticket table,
  `RenderEnabled`, and `VoxelObjectReady` contract above.
- Preserve current hide/remove behavior for non-streamed entities. A staged
  streamed object uploads while excluded from render, shadow, occlusion, and BVH
  lists.
- Apply one global per-frame sector/byte upload allowance across every object,
  ordered by scheduler priority and stable ticket ID. Stop between sectors.
- Clear brick dirtiness covered by a whole-sector upload and report `Ready` only
  after allocation, material, target uploads, and sector-grid topology settle.
- Share immutable visual geometry; allocate a private copy only on first edit.
- Batch each page/chunk ECS commit and coalesce renderer topology changes once per
  frame. Make the topology index incremental only if the coalesced rebuild still
  fails its measured budget.

Exit: one chunk cannot consume an unlimited frame; staged children become ready
without rendering; a parent/child visibility swap observes one completed ECS
command set.

### Step 3: Add Versioned Layer Pages

#### Step 3A: Add Shared Pages And `.gkworld` v3

- Add the shared page records and forest validator.
- Dispatch imported-world loading by schema version and normalize v1/v2/v3 into
  one indexed runtime form. Keep the writer on v2 until a complete v3 bake
  validates.
- Convert each v2 sector into one compatibility page, retaining its first proxy
  and current proxy-less behavior.
- Build entry, sector, bounds, child/parent, and leaf-owner indexes once at load.

Exit: Gasworks, Crossfire, and Subtransit v2 assets load unchanged; invalid
indices, cycles, shared children, duplicate roots, uncovered entries, and bounds
violations fail explicitly.

#### Step 3B: Add Terrain v3 And Independent Layer Grids

- Implement `height_u16_binary_v1` read/write/hash validation and direct tile
  decode, including the optional 1 m outdoor exclusion mask. Keep terrain
  manifest/chunk v2 readers unchanged.
- Add terrain v3 page fields and v2 normalization.
- Remove validation that terrain, `.gkworld`, and level chunk sizes/resolutions
  match. Validate each layer independently, transform all bounds into level world
  space, and reject only invalid/non-finite/out-of-level coverage.
- When terrain manifest v3 is present, require `manifest_path` and treat
  `.gkterrain` `source_path` as optional authoring provenance. Preserve the
  source-only legacy path.
- Keep island-wide `.gkterrain` JSON authoring-only; runtime v3 loads height tiles
  through the manifest.

Exit: a 2 m terrain tile and 0.1 m Gasworks chunks coexist in one level and map
to correct world bounds; malformed headers, hashes, masks, and layer bounds fail
before gameplay.

#### Step 3C: Make Bakers Emit v3 And Build The Harness

- Reuse current imported-world proxy baking to emit leaf, regional, macro, and
  root POI pages. Sort all arrays before assigning indices.
- Extend terrain baking to emit 256 m height tiles and regional, macro, and root
  height payloads. Do not emit island-wide local terrain voxels.
- Emit hashes, byte sizes, occupied-sector/brick counts, POI terrain stamps,
  replacement masks, outdoor-navigation exclusions, and coverage groups.
- Switch each writer to v3 only after its full output validates.
- Add `cmd/islandstreamharness` with the exact CLI above. Generate the fixture on
  demand and reference, rather than copy, existing Gasworks payloads.

Exit: repeated builds are byte-stable; the fixture has a 15 km playable span,
all 64 root terrain pages, complete route branches, Gasworks silhouette coverage,
and at most 128 MiB of pinned roots.

#### Step 3D: Adopt Pages In The Runtime

- Reuse the current prepare/commit/generation paths for every page level.
- Load all required terrain and POI roots through staged renderer tickets. Open
  gameplay only after root coverage and spawn collision are ready.
- Select macro, regional, local terrain, and full POI children using the profile's
  distance, hysteresis, and velocity-prefetch values. Add projected screen error
  only if aircraft/high-view captures prove distance selection insufficient.
- Keep active parents until every selected child or coverage-group member is
  ready. Reverse the atomic handoff when coarsening.
- Pin roots; evict local, regional, then macro detail. Remove the legacy global
  “proxy for every sector” fallback only for v3 page content.

Exit: every normal viewpoint retains GPU-ready terrain and POI mass; all 64 root
terrain objects remain pinned; LOD changes detail but never reveal empty space.

### Step 4: Bound Streaming Memory

- Decode existing imported-world RLE directly into the brick builder rather than
  retaining one `ImportedWorldVoxelDef` per voxel. Keep JSON decode for legacy
  editable/diagnostic content.
- Keep manifests and shared definitions cached; make streamed payloads and
  prepared geometry byte-budgeted LRUs.
- Count result channels and GPU-uploading pages in backpressure. Admit work by
  bytes before decode/build instead of using two fixed 256-result buffers.
- Pin visible pages, active parents, required collision, and dirty edits. Evict
  unreferenced local, regional, then macro detail; never evict roots during play.
- Apply the same byte/sector policy to the existing retained GPU-map cache.

Exit: every route stays below 768 MiB streaming CPU and 1,024 MiB retained voxel
GPU memory; queues cannot grow when rendering falls behind; settled memory drops
after returning from a 15 km traversal.

### Step 5: Add Scalable Terrain Runtime

- Cache decoded height arrays (at most 32 KiB each) independently from voxel
  geometry. Build sparse 0.5 m surface pages only inside local-terrain residency.
- Use bilinear height queries for ordinary ground collision. Load/prefetch the
  needed height tiles before control reaches them; do not create filled voxel
  columns for collision.
- Keep an edit-ready tile reference inside destruction residency, but create a
  private 0.5 m voxel patch only on the first terrain edit. Persist that patch
  through current world-delta ownership.
- Apply baked overlay pads directly in the height tiles. For `replace_surface`,
  activate the masked terrain, replacement POI geometry, and collision ownership
  as one coverage-group handoff.
- Demote clean local surfaces and collision tiles independently of root/regional
  visuals. Dirty patches remain resident until their snapshot is safely queued.

Exit: natural ground is seamless at tile boundaries; factory pads have no terrain
penetration; a bunker cutout cannot appear before its replacement; terrain edits
survive leave/return without an island-wide voxel allocation.

### Step 6: Split POI And Gameplay Residency

- Treat each POI as landmark proxy, exterior pages, and interior/gameplay data.
  Multi-storey geometry remains full 3D `.gkworld` content.
- Select exteriors by page distance. Use PVS/portals only for interiors where they
  reject work.
- Keep collision local, destruction smaller, and editable backing smaller still;
  demote each while visual pages may remain cached.
- Add owner-scoped, world-space collision/navigation/gameplay residency requests
  with generation-specific readiness. Requests use each layer's independent grid
  and never derive nav coordinates from visual chunk coordinates.
- Apply navigation changes as tile additions/removals, retain unchanged tile
  objects, and keep the previous immutable query active until its replacement is
  complete. Load bunker/factory interiors near an entrance or visibility
  transition, not because a distant proxy is visible.
- Share one local gameplay request among nearby squads. Member NPC entities do
  not add visual observers or separate nav ranges; abstract off-screen squads do
  not request terrain, collision, POI, or tactical-nav residency at all.
- Generate bounded 1 m outdoor walk tiles from resident height/exclusion data;
  stream detailed 0.25 m graphs only for POIs and stitch them at validated seams.
- Implement the strategic/tactical authority handoff and exact activation
  distances through the
  [companion navigation plan](../../../actiongame/docs/island-strategy-navigation.md).

Exit: a refinery silhouette survives at island distance; approach refines it
without a hole; floors/interiors/collision are ready before interaction; leaving
demotes gameplay without removing the silhouette. A squad cannot become tactical
until its shared collision/nav/gameplay request reports ready.

### Step 7: Remove Remaining Synchronous Latency

- Snapshot dirty edits minimally and serialize/write them on a bounded worker
  queue. If admission is unsafe/full, retain the dirty patch instead of blocking
  or losing it. Drain on orderly shutdown and surface failures.
- Batch navigation and scene changes once per frame.
- Add an incremental sector-grid or another imported-world payload only when the
  named measurements still fail after batching, budgeting, and direct decode.

Exit: no normal dirty unload writes files on the frame path; failures cannot lose
edits silently; measured navigation/topology work stays inside profile budgets.

### Step 8: Prove The 15 Km Target

Run `boot_pan`, `scale_crossing`, `regional_loop`, `poi_roundtrip`,
`boundary_ping_pong`, and `teleport_return`. Keep each JSON result with exact
build/hardware identity. Then manually inspect the highest long view, Gasworks
entry/exit and multiple floors, one bunker replacement, and an edit/leave/return
cycle.

Final acceptance:

- Every route satisfies every applicable pass threshold.
- No terrain, landmark mass, or replacement surface appears from empty space.
- Late work preserves a coarser parent; collision is ready before control.
- Streaming work and memory stay within `island_reference` budgets.
- Multi-level POI, terrain, collision, destruction, interior, and nav ownership
  refine and demote independently without stale geometry or lost edits.

## Commit-sized Delivery Slices

Each row is one reviewable behavior slice. A mechanical rename may be separated,
but do not split a row so that it leaves its external contract half-active.

| ID | Depends on | Deliverable and check |
| --- | --- | --- |
| I01 | - | Profile, counters, dormant runner, and recorded Gasworks baseline. |
| I02 | I01 | Stable scheduler, containing-sector PVS, event-driven observer refresh. |
| I03 | I01 | Staged-hidden renderer object and ticket state/query API. |
| I04 | I03 | Global byte/sector upload queue and exact readiness predicate. |
| I05 | - | Complete: shared page types/validator and imported-world v1/v2 normalization; see [runtime contract](streaming-and-worlds.md#streaming-page-contracts-and-legacy-normalization). |
| I06 | I05 | Complete: explicit `.gkworld` v3 tooling and deterministic POI page bake; live runtime remains gated. See [bake contract](streaming-and-worlds.md#imported-world-v3-page-baking). |
| I07 | I05 | Complete: terrain v3 page tooling, independent source backing and deterministic height-page publication; live runtime remains gated. See [bake contract](streaming-and-worlds.md#terrain-v3-page-baking). |
| I08 | I06, I07 | Complete: independent terrain/POI world-space indexes and authored optional streaming bounds; content accepts independent grids, live runtime remains gated. See [assembly contract](streaming-and-worlds.md#independent-level-layer-indexes). |
| I09 | I06, I07 | Complete: deterministic offline 15 km harness builder, qualified borrowed FULL references and immutable publication. See [fixture contract](#fixture-assets). Runtime route execution remains pending. |
| I10 | I02, I04, I08, I09 | Control plane complete: pinned-root demand, distance/hysteresis/velocity page selection and generation-qualified startup eligibility. Live activation waits for I11/I13/I14. See [control-plane contract](streaming-and-worlds.md#independent-page-control-plane). |
| I11 | I10 | Control-plane transactions: complete-cohort refine/coarsen, coupled terminal coverage groups and qualified collision proof. Native publication waits for I13/I14. See [handoff contract](streaming-and-worlds.md#page-handoff-transactions). |
| I12 | I04, I10 | Byte-budgeted decode/prepared/GPU caches and admission backpressure. |
| I13 | I07, I12 | Height-tile cache and sparse local terrain surface builder. |
| I14 | I11, I13 | Height collision, overlay/replace ownership, and COW edit patches. |
| I15 | I11, I14 | POI gameplay split plus on-demand outdoor/detailed nav, owner-scoped requests, readiness, tile diffs, and demotion. |
| I16 | I12, I15 | Bounded persistence plus measured navigation/topology batching. |
| I17 | I16 | All six benchmark results and manual 15 km acceptance record. |

I02 and I03 may proceed in parallel. I05 may also proceed while renderer work is
underway. The first island-generator implementation may start after I15; I16-I17
are still required before calling the target production-ready.

The navigation plan's N05-N09 are the detailed delivery breakdown of I15's
navigation portion, not a second implementation. N10-N13 consume that engine
contract to materialize/dematerialize Actiongame squads. N02-N04, which implement
offline strategy state, may proceed independently while I01-I14 are underway.

## Expected Files To Change

Keep implementation in current ownership boundaries:

- `gekko/content/stream_page.go`: shared serialized page records and validation
- `gekko/content/imported_world.go`, `imported_world_io.go`, and
  `imported_world_validation.go`: `.gkworld` v3 and compatibility normalization
- `gekko/content/terrain_chunk.go`, `terrain_chunk_io.go`, and
  `terrain_chunk_binary.go`: terrain v3 and height-tile payload
- `gekko/content/terrain_bake.go` and `gekko/imported_world_baker.go`: deterministic
  terrain/POI pages, stamps, masks, and cost metadata
- `gekko/content/level_validation.go`: independent layer validation
- `gekko/cmd/islandstreamharness/main.go`: deterministic temporary fixture
- `gekko/streamed_level_runtime.go`: indexes, selection, priority, tickets,
  coverage handoff, residency, persistence admission, and backpressure
- `gekko/level_content_spawn.go`: height-tile/sparse-surface spawn boundary
- `gekko/runtime_content_loader.go` and
  `gekko/streamed_level_geometry_cache.go`: byte-budgeted residency
- `gekko/asset_authored_components.go`, `mod_voxelrt_client.go`, and
  `mod_voxelrt_client_systems.go`: streamed residency marker, ticket status, and
  shared/COW geometry
- `gekko/voxelrt/rt/core/scene.go`, `voxelrt/rt/gpu/manager.go`, and
  `voxelrt/rt/gpu/manager_voxel.go`: staged objects, readiness, global uploads
- `gekko/voxelrt/rt/gpu/manager_scene.go`: incremental topology only if measured
- `gekko/navigation_graph_runtime.go`: tile diffs
- `gekko/content/outdoor_navigation.go`: bounded height/exclusion walk tiles
- `actiongame/main.go`: reference viewport/depth profile application
- `actiongame/src/modules/startup/startup_config.go`, `streaming_harness.go`, and
  `startup_install.go`: profile, routes, results, and registration
- `actiongame/src/modules/startup/npc_motor.go`: shared/local NPC residency

Do not add a generic streaming package, island asset container, binary manifest,
or second page scheduler. Reuse the runtime, manifest, and renderer boundaries
until measurement proves one inadequate.

## Verification And Test Policy

This plan changes no tests. Repository policy makes test changes opt-in. Before
implementation coverage is added, request permission for the smallest stable
contracts:

- parent coverage remains until all children or coverage-group members are ready
- staged-hidden objects upload but do not render
- readiness includes allocation, uploads, material, and sector-grid topology
- global upload/backpressure limits cannot be exceeded
- height-tile boundaries sample continuously, outdoor exclusions align, and
  masked replacement is atomic
- indoor PVS starts at the containing sector
- byte caches preserve pinned coverage while evicting unreferenced detail
- v2 compatibility remains unchanged and v3 rejects invalid ownership

Existing tests may run unchanged. Each slice also has its named manual/harness
check; visual gameplay tuning remains manual until its contract stabilizes.

## Implementation Readiness

This document is ready to start I01-I17. It fixes the 15 km footprint, layer
ownership, page/tile sizes, serialized fields and versions, height payload,
renderer readiness, default budgets, fixture commands, routes, measurements,
thresholds, dependencies, and exit gates.

For the described squad-capture game, implementation readiness also includes the
[companion navigation plan](../../../actiongame/docs/island-strategy-navigation.md).
It preserves the current tactical AI and specifies the added road graph, sparse
detailed POI bake, on-demand outdoor navigation, shared residency, and
strategic/tactical authority handoff.

It is not evidence that the target already performs: the current filled-column,
single-resolution runtime cannot meet it. “15 km production-ready” is earned only
when I17 passes on a named release hardware profile. The remaining deliberate
unknowns do not block implementation: final island art/generation, exact release
hardware, optional LOD cross-fade, and optimizations triggered only by measured
failures.
