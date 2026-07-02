# NPC Navigation And Navmesh Plan

> Generation direction update (2026-07-01):
> [`pure-go-voxel-navigation-generation-plan.md`](pure-go-voxel-navigation-generation-plan.md)
> supersedes this document's builder-hardening implementation path. The content,
> streaming, delta, query, and NPC ownership described here remain valid. Recast
> remains the current production bake backend only until the pure-Go plan reaches
> its cutover gate.

This document defines the long-term navigation plan for NPCs in voxel-backed
`gklevel` worlds.

The target architecture is:

```text
.gklevel
  -> .gkworld sectors/chunks
  -> baked .gknav manifest
  -> streamed .gknavtile local navmesh tiles
  -> runtime dirty/rebuilt nav tiles beside .gkworlddelta
  -> hierarchical sector graph for far NPC movement
```

This is a long-term architecture step. It is not a tactical pathfinding patch
and not a throwaway diagnostic.

## Goals

- Give NPCs reliable navigation over voxel levels.
- Precompute static navigation during import or level creation.
- Rebuild only affected navigation tiles after voxel edits.
- Persist rebuilt runtime navigation beside `.gkworlddelta`.
- Support multiple NPC agent profiles over time.
- Support walking, jumping, crouching, ladders, swimming, and later flying.
- Let NPCs plan through openable dynamic obstacles such as doors.
- Provide cheap large-level movement when NPCs are outside the player's active
  bubble.
- Keep the design extensible for source metadata, tactical reasoning, and richer
  AI without replacing the storage/runtime model.

## Non-Goals For The First Implementation

- Full flying navigation.
- Full tactical combat behavior.
- Source-BSP-native navigation generation.
- Perfect dynamic crowd avoidance.
- Rebuilding a single global navmesh after every edit.
- Embedding large generated nav blobs directly in `.gklevel`.

## Current Project Fit

Existing systems already point toward this shape:

- `.gklevel` is the top-level authored assembly document.
- Imported base worlds are chunked as `.gkworld` plus `.gkchunk`.
- Imported worlds already have sectors, LODs, visibility refs, adjacency refs,
  and payload hashes.
- Streamed runtime already loads chunks/sectors around observers.
- Prepared geometry already has hash-keyed runtime caching.
- World deltas already persist voxel changes and chunk overrides.
- NPCs can now own opt-in navigation intent, route state, and local waypoint
  following through `NPCNavigationComponent` and
  `NPCNavigationMovementComponent`.

Navigation should follow those boundaries:

- Authored level references nav data.
- Static generated nav lives in sidecars.
- Runtime-modified nav lives beside world deltas.
- NPC behavior consumes a navigation service instead of embedding pathfinding in
  `NPCComponent`.

## Core Architecture

Use two connected navigation layers.

### Local Layer: Tiled Recast-Style Navmesh

The local layer is the accurate movement layer for active or near-active NPCs.

It uses tiled navmesh data derived from voxel occupancy. The builder should keep
moving toward a Recast-style shape while staying in-engine and voxel-first:

1. Convert solid voxel occupancy into a walkable heightfield for each nav tile.
2. Filter spans by agent clearance, step height, slope, and ledge rules.
3. Build compact walkable regions.
4. Build polygonal or cell-region nav tiles.
5. Add off-mesh links for ladders, jumps, doors, water transitions, and special
   authored traversal.
6. Store the result as deterministic tile sidecars.

The runtime should stream local nav tiles with the same broad residency model as
world chunks. NPCs inside the active bubble should refine coarse routes into
local tile paths.

The local query layer started intentionally small:

- `NewNavTileQuery(...)`
- point-to-polygon lookup on a single tile
- polygon-center waypoint output
- breadth-first traversal across same-tile polygon neighbor IDs

It has since grown into an effective tiled path query:

- effective static/delta tile lookup
- same-tile polygon neighbor traversal
- stored cross-tile portal traversal
- geometry fallback for compatible adjacent tile polygons
- endpoint snapping with start/end snap diagnostics
- hierarchical route refinement through `RuntimeNavigationService`

Off-mesh links, funnel/string-pull smoothing, and richer traversal actions are
still separate follow-ups.

### Global Layer: Hierarchical Sector Graph

The global layer is the cheap movement layer for large levels and unloaded
areas.

It should be built from imported-world sectors plus authored/link metadata:

- sector bounds
- sector adjacency
- visibility/PVS metadata when available
- doors and moving brushes
- ladder/water/changer volumes
- authored path nodes
- coarse portals between sectors
- optional sector traversal cost estimates

Far NPCs can move sector-to-sector using simplified simulation. When an NPC
enters or approaches the player's bubble, its sector route is refined into local
navmesh paths.

The first version is implemented as a simple sector graph:

- route between sectors using adjacency
- estimate travel time by center-to-center distance and agent speed
- defer tactical reasoning
- require local refinement only near active gameplay

Runtime route queries can now optionally fall back to unconstrained local
refinement when a coarse sector corridor is too restrictive. That preserves the
sector graph as a far-routing hint without letting an imperfect coarse path
hide visibly connected local navmesh portals.

## Content Schema

Add navigation as a referenced generated content type, not as embedded level
state.

Recommended level extension:

```go
type LevelDef struct {
    Navigation *LevelNavigationDef `json:"navigation,omitempty"`
}

type LevelNavigationDef struct {
    ManifestPath string   `json:"manifest_path,omitempty"`
    Tags         []string `json:"tags,omitempty"`
}
```

Recommended generated manifest:

```go
type NavManifestDef struct {
    NavID           string
    SchemaVersion   int
    LevelID         string
    SourceWorldID   string
    SourceLevelHash string
    BuilderVersion  string
    ChunkSize       int
    VoxelResolution float32
    AgentProfiles   []NavAgentProfileDef
    Tiles           []NavTileEntryDef
    Sectors         []NavSectorEntryDef
}
```

Recommended local tile entry:

```go
type NavTileEntryDef struct {
    Coord             TerrainChunkCoordDef
    AgentProfileID    string
    TilePath          string
    SourcePayloadHash string
    SourceDeltaHash   string
    NavBuildHash      string
    BoundsMin         [3]float32
    BoundsMax         [3]float32
}
```

Recommended agent profile:

```go
type NavAgentProfileDef struct {
    ID              string
    Radius          float32
    Height          float32
    CrouchHeight    float32
    StepHeight      float32
    MaxSlopeDegrees float32
    MaxDropHeight   float32
    MaxJumpUp       float32
    MaxJumpDown     float32
    MaxJumpDistance float32
    CanCrouch       bool
    CanClimb        bool
    CanSwim         bool
    CanFly          bool
}
```

Start with one HL1-like profile, matching the current player defaults:

- height: `1.8288m`
- radius: `0.4064m`
- step height: `0.4572m`
- eye height is not needed for nav generation

Then add more profiles without changing the manifest shape.

## Tile Storage

Use sidecar files:

- `.gknav`: manifest
- `.gknavtile`: local nav tile payload
- optional `.gknavgraph`: global sector graph payload if it becomes too large
  for the manifest

Static baked nav should live near the level/imported-world data.

Runtime rebuilt nav should live under the world-delta data directory, for
example:

```text
level.gkworlddelta_data/nav/<nav-id>/<agent-profile>/<tile-key>.gknavtile
```

The runtime should prefer delta nav tiles when present and valid. Otherwise it
uses the baked static tile.

## Cache And Invalidation

Nav tile cache keys must include:

- builder version
- nav schema version
- agent profile hash
- source chunk payload hash
- source aux/collision policy hash when relevant
- source world-delta hash for edited chunks
- tile coordinate and tile size

Voxel edits should mark dirty nav tiles by overlap:

- edited imported-world chunk
- edited terrain chunk
- edited voxel object if it contributes collision
- neighboring nav tiles inside the agent radius and step/ledge margin

Dirty tiles should rebuild asynchronously. Until rebuild completes, the runtime
can use one of these policies:

- keep old tile and mark paths through it stale
- use conservative local grid fallback inside the dirty tile
- block traversal through the dirty tile for agents that require correctness

The default should be conservative for gameplay-critical NPCs:

- keep current path if already inside the tile and still locally valid
- avoid starting new long paths through dirty tiles unless no alternative exists
- rebuild under the same streaming/commit budget philosophy as world chunks

## Recommended Walkability Rules

Walkability should be profile-driven and conservative.

For the first walking profile:

- A walkable span needs enough vertical clearance for agent height.
- A crouch span needs enough clearance for crouch height and `CanCrouch`.
- Neighbor spans are connected by walking when vertical delta is less than or
  equal to `StepHeight`.
- A surface is walkable when its estimated slope is less than or equal to
  `MaxSlopeDegrees`.
- Default `MaxSlopeDegrees` should be `45`.
- One-voxel lips are walkable only when the world-space height is within
  `StepHeight`.
- Drops are not ordinary walking edges. They become one-way drop links when
  within `MaxDropHeight`.
- Jumps are off-mesh links, not normal polygon adjacency.
- Ladders are off-mesh links generated from `ladder_volumes`.
- Swimming uses water volumes and a separate traversal area flag.
- Flying should be deferred into a different navigation volume/profile path.

For voxel-derived slope:

- Prefer a local heightfield/occupancy-gradient estimate.
- Reject unstable single-voxel spikes unless an agent can step onto them with
  radius clearance.
- Use radius clearance, not only center-point occupancy, so NPC hulls do not
  path through narrow gaps.
- Add a one-tile neighbor halo while building so tile boundaries produce the
  same result on both sides.

Later, imported source metadata can improve these rules:

- BSP plane normals for slopes.
- Source contents flags for ladders, water, clips, and monster clips.
- Door/entity metadata for traversal action costs.
- Authored nav modifiers in the editor.

## Voxel-First Navmesh Hardening Plan

The current implementation is already voxel-first and has a useful tiled
navigation pipeline. It uses dense occupancy/candidate span data, region and
contour generation, convex polygon output, tile portals, delta tile sidecars,
and debug/diagnostic tooling. It is still an intermediate custom pipeline, so
further work should harden it toward a Recast-style heightfield builder with
stronger invariants and stable border-span connectivity.

The source of navigation truth must be voxel occupancy:

- Voxel data is canonical for whether an NPC can stand, move, or fit.
- BSP/source surfaces are optional hints only. They can improve normals,
  metadata, water, ladders, doors, or authored traversal, but nav correctness
  must not require BSP geometry.
- Procedural voxel worlds and `.vox` imported worlds must be able to build nav
  without any BSP-derived surface source.
- Runtime voxel edits must rebuild nav from the effective voxel world state plus
  the required neighbor context.

The desired build shape is:

```text
voxel occupancy
  -> walkable spans
  -> clearance-filtered spans
  -> compact heightfield cells
  -> connected regions
  -> contours
  -> simplified contours
  -> convex polygons
  -> border spans
  -> portals
```

### Hardening Principles

- Keep nav tiles as derived sidecar data.
- Rebuild every tile from voxel data, agent profile, builder version, and
  neighbor context.
- Use one shared agent reach model for dirty expansion, full bake context,
  delta bake context, occupancy sampling, cache hashes, and border stitching.
- Keep portals adjacent-tile only unless a future off-mesh link explicitly
  crosses farther.
- Treat polygon output as the pathfinding surface, but derive connectivity from
  walkable cell and border-span facts.
- Convert every observed bug into a deterministic synthetic fixture.

### Stage 1: Invariants And Regression Fixtures

Add synthetic fixtures before deeper refactors:

- flat fields
- large open fields crossing chunk boundaries
- holes and islands
- concave regions
- ramps
- stairs
- one-voxel lips
- ledges and drops
- narrow gaps for different agent radii
- uneven tile seams
- large agents on small chunks
- edited delta tiles with stale static neighbors
- empty-neighbor and blocked-neighbor context

Add invariant checks:

- generated polygons are convex
- polygon vertices stay inside tile bounds
- polygons do not unintentionally overlap
- holes are not filled by fallback rectangles
- every walkable cell maps to a reachable polygon
- blocked/unknown cells do not become walkable
- every portal references real polygons
- every portal segment lies on the shared tile boundary
- connected walkable cells remain connected by polygon neighbors or portals

Current checkpoint:

- Many synthetic unit tests exist for flat fields, chunk seams, ramps, stairs,
  large agents, portal topology, endpoint snapping, and sector-corridor
  fallback.
- Delta-nav fixtures now cover edited holes in large flat imported-world
  fields, including streamed runtime rebuild and effective path lookup.
- The remaining gap is not generic unit coverage, but named regression
  fixtures for real maps and bug cases observed during manual testing,
  especially Crossfire and Gasworks path pairs.

### Stage 2: Canonical Voxel Heightfield

Make the intermediate heightfield explicit.

Deliverables:

- walkable span data structures
- clearance-filtered compact cells
- per-cell traversal classification inputs
- debug dumps for occupancy, spans, compact cells, and rejected cells
- tests for span extraction, agent clearance, radius filtering, and unknown
  neighbor handling

The builder should no longer need generic source/BSP surfaces to cover ramps,
stairs, or normal walkable floors.

Current checkpoint:

- Voxel occupancy is the source of truth for nav generation.
- Dense bitset occupancy and exposed solid/air candidate spans are used for
  fast full-world bakes.
- Heightfield debug dumps are available through `cmd/navdiag`.
- Generic/BSP surface sources should now be treated as optional hints, not as
  required navigation input.

### Stage 3: Traversal Classification

Separate traversal types instead of treating every connected surface as ordinary
walk.

Initial area types:

- `walk`
- `ramp`
- `stair`
- `step`
- `drop`

Rules:

- Smooth ramp polygons must obey `MaxSlopeDegrees`.
- Stair/step traversal can use `StepHeight` even when a fitted smooth ramp would
  exceed `MaxSlopeDegrees`.
- Drops should become one-way traversal links when within `MaxDropHeight`.
- Jumps should become explicit off-mesh links later, not normal polygon
  adjacency.
- Crouch, ladder, swim, and fly should extend this model instead of creating a
  separate unrelated navigation system.

This fixes the current weakness where stepped voxel surfaces can be merged into
smooth `walk` polygons.

Current implementation checkpoint:

- Voxel-derived polygons now carry `walk`, `ramp`, `stair`, or `step` area
  classifications.
- `ramp` is used only when the fitted region slope is within
  `MaxSlopeDegrees`.
- `stair` and `step` regions keep `StepHeight`-based connectivity so stepped
  surfaces do not have to masquerade as smooth ramps.
- `drop` is still deferred until off-mesh/drop-link generation.

### Stage 4: Robust Regions, Contours, And Holes

Replace ad hoc region fallbacks with robust heightfield region processing.

Deliverables:

- connected components over compact walkable cells
- support for multiple contour loops
- support for holes
- deterministic contour tracing
- contour simplification with bounded error
- deterministic splitting of concave contours into convex polygons
- tests proving holes remain blocked and islands remain separate

The goal is fewer skinny/fragmented polygons while preserving topology.

Current checkpoint:

- Contour generation, simplification, and convex polygon splitting exist and
  have improved large flat areas, ramps, and stairways.
- Remaining hardening should focus on holes, islands, concave complex regions,
  and fixtures that prove blocked cells are never filled by simplification or
  fallback region logic.

### Stage 5: Border-Span Portal Generation

Current portal generation is no longer simple polygon-bounds guessing. Voxel
nav tiles now preserve cell-derived `border_spans` for tile-edge walkable spans
and portal generation prefers those spans when both neighboring tiles provide
them. Polygon boundary spans remain as a fallback for older tiles and authored
source polygons. Portal generation splits seam gaps, supports ramps and steps,
validates reciprocal portals, and has tests for several uneven seam cases.

Desired process:

```text
current tile border walkable spans
  -> neighbor tile border walkable spans
  -> overlap and height/area classification
  -> portal segments
```

This should handle:

- flat seams
- ramps crossing tile borders
- stair borders
- partial overlaps
- uneven but step-valid seams
- edited neighbor tiles
- different polygonization on each side of the seam

Polygon shapes may change over time; heightfield border-span connectivity
should stay stable. Because explicit off-mesh link refs and cross-tile drop
links changed generated tile payloads after border spans, the current builder
version is `voxel_nav_v24`.

### Stage 6: Delta Rebuild Correctness

Runtime rebuilds must behave like full bake over the effective edited world.

Checklist:

- dirty tile expansion uses shared agent reach
- rebuild context includes all required neighbor chunks
- missing horizontal context is synthesized only when it is known empty or
  safely outside authored world data
- missing vertical context remains conservative unless known empty
- cache hashes include every neighbor chunk that can affect clearance
- empty delta overrides suppress stale static portals
- regenerated tiles do not keep stale portals
- changed tiles save beside `.gkworlddelta`

Current checkpoint:

- Dirty tile expansion, multi-chunk rebuild context, empty overrides, stale
  static-neighbor suppression, edited holes, and delta nav save/load have
  content-level regression coverage.
- Edited imported-world chunk snapshots and rebuilt nav overrides are now tested
  together across `.gkworlddelta` reload, including path rejection inside a
  reloaded edited hole.
- Edited seam gaps are tested through the full imported-world delta rebuild path
  so persisted delta nav portals follow voxel border spans instead of stale
  polygon guesses.

### Stage 7: Debug And Diagnostics

Improve debug tools so nav bugs can be diagnosed without guessing.

Debug UI should show:

- selected tile coordinate and agent profile
- selected polygon id, area type, flags, and neighbors
- selected portal id, target tile, target polygon, and segment endpoints
- isolated polygon highlight
- path start/end snapped polygons
- path failure reason and failed tile coordinate
- border-span visualization
- traversal-type colors
- depth-tested and overlay rendering modes

Current checkpoint:

- Actiongame nav debug can draw tile edges, portals, seams, isolated polygons,
  route lines, route corridors, waypoint/target markers, selected NPC handles,
  snapped route endpoints, snap lines, and path failure markers.
- HUD diagnostics show route status, refinement reason, failure tile, snap
  distance, path shape, corridor-fallback usage, cursor target tile source
  (`static`/`delta`), empty-delta state, and navigation revision.
- Actiongame nav debug tile loading is navigation-revision aware, so overwritten
  delta tile paths are reloaded after runtime rebuilds instead of drawing stale
  cached debug geometry.
- `cmd/navdiag` validates nav manifests, exports heightfield dumps, and can run
  path queries with optional delta data, local-only mode, endpoint snapping, and
  sector-corridor fallback.
- `cmd/navdiag -rebuild-delta-nav` can regenerate `.gkworlddelta` navigation
  tile overrides from persisted imported-world voxel chunk overrides, either for
  all dirty chunks in the delta or for explicit `-dirty-coords`.
- Runtime dirty-nav rebuilds are exposed through `StreamedLevelRuntimeMetrics`
  and the actiongame NPC/nav debug HUD `nav_delta(...)` summary, including
  pending debounced runtime edits.

CLI/debug exports should include:

- one-tile nav summary
- failed path trace
- heightfield dump (`cmd/navdiag -world ... -coord ... -dump-heightfield ...`)
- per-tile bake timing and span/cell/region/polygon stats
- explicit `build_intermediate` timing for voxel heightfield/clearance/region
  work before polygon emission
- CPU profile capture for slow full-world bakes (`cmd/navbake -cpuprofile ...`)
- parallel full-world bake control (`cmd/navbake -build-workers ...`)
- delta-nav rebuild/repair (`cmd/navdiag -rebuild-delta-nav ...`)
- region dump
- contour dump
- border-span dump

Current bake performance status:

- Voxel occupancy is the source of truth for nav generation.
- Full imported-world bakes use persisted chunk payload hashes for cache keys.
- Nav build uses dense bitset occupancy plus exposed solid/air candidate spans.
- The `build_intermediate` stage runs in parallel by default using
  `GOMAXPROCS`; use `-build-workers 1` for single-threaded comparisons.
- Gasworks has been manually verified to bake quickly and correctly with the
  optimized path. Parallel `build_intermediate` progress can arrive out of
  coordinate order; this is expected and does not affect deterministic tile
  save/manifest output.

### Stage 8: Hierarchical And Far Navigation

Keep global routing layered above local voxel nav.

Long-term shape:

```text
voxel-derived local nav tiles
  -> tile connectivity
  -> sector graph
  -> far route
  -> local refinement near active gameplay
```

Start with sector adjacency and approximate costs. Later add openable doors,
danger/cost fields, tactical routing, and group movement.

### Recommended Next Implementation Order

1. Add named regression fixtures for Crossfire/Gasworks path pairs and the
   specific manual bugs already found: seam paths, sector-corridor fallback,
   endpoint snapping near portals, ramps, stairs, and large open chunk fields.
2. Add remaining invariant tests for holes, islands, concave regions, ledges,
   drops, and narrow gaps across multiple agent radii.
3. Harden contour support for holes and complex regions.
4. Preserve voxel heightfield border spans and use them to harden portal
   generation beyond the current polygon-boundary-span implementation.
5. Complete runtime dirty-nav integration: edit tracking, async rebuild queue,
   runtime debug overlay, and persisted `.gkworlddelta` reload checks.
6. Add off-mesh traversal links for drops, ladders, openable doors, jumps,
   water transitions, and moving platforms.
7. Extend multiple-profile/capability coverage and far NPC sector simulation.

## Dynamic Obstacles And Openable Doors

Openable dynamic obstacles should be planned through when the agent can operate
or wait for them.

Represent them as nav modifiers and traversal links:

- closed, openable door: traversable edge with an action cost
- closed, locked door: blocked edge unless the NPC has the required condition
- moving platform/train: time-dependent link
- breakable obstacle: traversable only for agents allowed to break it, with
  action cost
- temporary physics debris: local dynamic obstacle avoidance, not baked nav

The global sector graph should also know about door-like connectors so far NPCs
can route through openable sectors without requiring local nav tile residency.

## Movement Capability Layers

Use capability-specific traversal, not separate unrelated navigation systems.

Walking:

- primary local navmesh traversal
- profile-specific radius, height, step, slope, and drop limits

Crouching:

- alternate area flag on spans with crouch clearance
- higher traversal cost unless crouch is tactically desired

Jumping:

- explicit/generated off-mesh links
- generated from ledges only after walking nav is reliable

Ladders:

- generated from level ladder volumes
- two-way vertical links for climb-capable agents

Swimming:

- water-volume navigation areas
- transitions between walkable ground and swim volume

Flying:

- defer
- later use navigation volumes or sparse 3D waypoint/sector graph, not the
  walking navmesh tile format

## Runtime Navigation Service

Add a navigation module/resource rather than putting routing logic directly on
NPC components.

Suggested engine-facing pieces:

- `NavigationModule`
- `NavigationWorldState`
- `NavAgentProfile`
- `NavTileCache`
- `NavQuery`
- `NavPathRequest`
- `NavPathResult`
- `NPCNavigationAgentComponent`
- `NPCPathFollowComponent`

NPC behavior should ask the navigation service for paths. The service decides
whether to answer from:

- local tile navmesh
- sector graph
- hybrid sector route plus local refinement
- temporary fallback while tiles rebuild

## Streaming Integration

Nav residency should track world residency but remain independently budgeted.

Near the player:

- keep local nav tiles loaded for active/collision/destruction radius
- rebuild dirty tiles needed by active NPCs first
- keep enough neighboring tiles for path continuity

Far from the player:

- keep only the sector graph and lightweight NPC simulation state
- do not require full local nav tile residency
- refine to local nav only when an NPC approaches active gameplay

Metrics should include:

- loaded nav tile count
- pending nav tile load count
- dirty nav tile count
- nav rebuild queue depth
- nav tile cache hits/misses
- path requests per frame
- path failures by reason
- sector routes per frame
- local refinement failures

## Implementation Phases

### Phase 1: Schema And Docs

- Add nav content schema.
- Add load/save/validation helpers.
- Add `LevelNavigationDef`.
- Document generated nav sidecar layout.
- Add tests for round trip, defaults, validation, and document-relative paths.

Status: implemented.

Verification:

- `cd gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`

### Phase 2: Voxel Occupancy Walkability Prototype

- Build a deterministic nav-tile generator from voxel occupancy.
- Start with one HL1-like walking profile.
- Generate walkable spans with clearance, step, slope, and radius filtering.
- Add tests for flat floor, wall, stairs, ledge, one-voxel lip, and narrow gap.
- Do not integrate NPC behavior yet.

Status: implemented and then hardened. The builder now uses voxel occupancy as
the canonical source, with optimized dense occupancy, candidate spans, contour
regions, traversal classification for walk/ramp/stair/step, and generated
one-way drop links for ledges within `MaxDropHeight`, including cross-tile
links with merged ledge spans.

Verification:

- `cd gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./content/... . -run 'TestNav|TestNavigation'`

### Phase 3: Static Bake And Runtime Load

- Bake `.gknav` and `.gknavtile` sidecars during level/imported-world creation.
- Add runtime loading of nav manifest.
- Load nav tiles beside streamed world chunks.
- Add debug queries for tile presence and basic point projection.

Status: implemented. Static import/bake writes `.gknav`/`.gknavtile` sidecars,
runtime state loads nav manifests, and `cmd/navbake`/`cmd/navdiag` provide bake
and inspection workflows.

Verification:

- `cd gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./content/... ./importers/hl1/... . -run 'TestNav|TestNavigation|TestStreamedRuntime'`
- `cd gekko && go run ./cmd/navdiag -nav ../actiongame/assets/levels/gasworks/worlds/gasworks.gknav -fail-on-error=false`
- `cd gekko && go run ./cmd/navdiag -nav /tmp/gasworks-current.gknav -world ../actiongame/assets/levels/gasworks/worlds/gasworks.gkworld -coord=-2:-1:-2 -profile hl1_standing -dump-heightfield /tmp/gasworks-heightfield-debug.json -fail-on-error=false`
- `cd gekko && go run ./cmd/navbake -world ../actiongame/assets/levels/gasworks/worlds/gasworks.gkworld -nav /tmp/gasworks-current.gknav -progress -cpuprofile /tmp/gasworks-navbake.pprof`
- `cd gekko && go run ./cmd/navbake -world ../actiongame/assets/levels/gasworks/worlds/gasworks.gkworld -nav /tmp/gasworks-current.gknav -progress -build-workers 1`

Manual check:

- Run an imported HL1-style map and draw nav tile bounds/walkable regions.
- Re-run Gasworks after bake performance changes and compare `build_intermediate`
  totals; default parallel bake should be quick enough for normal testing.

### Phase 4: Query API And NPC Local Pathing

- Add navigation service/API.
- Add path request/result API.
- Add basic NPC navigation agent components.
- Support local path queries inside loaded tiles.
- Add simple path following for walking NPCs.

Status: mostly implemented through `RuntimeNavigationService`,
`NPCNavigationComponent`, `NPCNavigationMovementComponent`, endpoint snapping,
effective static/delta local paths, hierarchical refinement, and conservative
waypoint following. A dedicated `NavigationModule` abstraction is still optional
future cleanup rather than a blocker.

Verification:

- focused engine tests for path request lifecycle
- actiongame compile/runtime check when integrated

Manual check:

- Spawn an NPC in a small level and confirm it walks around static obstacles.

### Phase 5: Dirty Tiles And World Delta Persistence

- Track voxel edits that affect nav.
- Mark overlapping tiles dirty with neighbor halo.
- Rebuild dirty tiles asynchronously.
- Save rebuilt tiles beside `.gkworlddelta`.
- Prefer valid delta nav tiles over baked tiles on reload.

Status: implemented for imported-world voxel edits in the streamed runtime.
Dirty expansion, delta nav tile save/load, effective tile lookup, stale portal
suppression, shared agent-reach context, runtime dirty detection, background
rebuild jobs, main-thread completion apply, persisted delta nav overrides, and
HUD/log metrics are in place. Runtime voxel edits on loaded imported-world
chunks now enqueue active delta-nav rebuilds before chunk unload. Runtime edit
rebuilds are debounced and repeated edits are coalesced while a rebuild is
queued or active, so sculpting waits for the latest settled voxel state instead
of piling up redundant or partial-state rebuild jobs. Runtime edited
imported-world chunk geometry is persisted to `.gkworlddelta` alongside the live
delta-nav rebuild, so nav and geometry reload together. Explicit save/shutdown
draining is available via
`DrainStreamedLevelNavigationRebuilds`. The level editor Base World panel can
manually rebuild delta nav from persisted imported-world chunk overrides.
The editor also has a base-world chunk delta persistence helper and shows the
persisted imported-world delta override count. Base World edit mode can expose
full-preview chunks for editor interaction and save dirty edited chunk geometry
to `.gkworlddelta`. The first dedicated base-world sculpt path can now edit
imported preview chunks directly with the shared voxel sculpt controls, mark
chunks dirty, and leave persistence to the existing save-edits action. Base
World sculpting also has imported-material palette selection/feedback in the
panel and sculpt toolbar. Unsaved edited chunks now get scene-occluded dirty
wire overlays, and the panel distinguishes unsaved chunk edits from stale or
current delta-nav rebuild state. A combined `save + rebuild nav` editor action
composes the save-edits and delta-nav rebuild flow while keeping both separate
manual actions available.

Verification:

- tests for dirty tile selection
- tests for delta nav save/load
- tests for edited geometry and rebuilt nav reloading together
- tests for edited seam border spans driving delta portals
- streamed runtime test where edited chunk reloads with rebuilt nav

Manual check:

- Edit or destroy voxels, watch dirty overlay, confirm NPC path changes after
  rebuild.

### Phase 6: Off-Mesh Links And Dynamic Obstacles

- Generate one-way intra-tile and cross-tile drop links from voxel ledges within
  `MaxDropHeight`, with explicit source/target refs and merged ledge spans.
- Generate ladder links from `ladder_volumes`.
- Add door/moving-brush traversal links.
- Plan through closed openable doors with action costs.
- Add richer drop-link debug visualization and movement/action handling.
- Add authored link/modifier support.

Status: not implemented beyond schema/traversal area foundations. This is the
next major capability layer after regression fixtures and navmesh hardening.

Verification:

- tests for door edge state/cost
- tests for ladder link generation
- tests for one-way intra-tile and cross-tile drop links

Manual check:

- NPC routes through an openable door and climbs a ladder.

### Phase 7: Hierarchical Sector Graph

- Build graph from imported-world sectors and adjacency.
- Add sector route query.
- Connect sector routes to local tile refinement.
- Simulate far NPC movement with simple sector traversal and travel-time
  estimates.

Status: partially implemented. Sector graph routing and local refinement are
implemented; far NPC simulation is still future work.

Initial implementation status:

- `BuildNavSectorGraph(...)` derives a coarse graph from nav manifest sectors.
- Sector graph edges come from `adjacent_sector_refs`, authored sector links,
  and inferred face-touching sector bounds.
- `FindNavSectorPath(...)` and `FindNavSectorPathBetweenPoints(...)` provide
  unloaded-area sector routes with simple cost and travel-time estimates.
- Required link tags and allowed traversal kinds are filterable at query time.
- `FindHierarchicalNavRoute(...)` now computes a sector route first and
  opportunistically refines the route into an effective local tile path when
  resident/static/delta nav tiles are available.
- Local refinement is normally constrained to tile coordinates overlapped by the
  selected sector corridor, so detailed paths do not wander through sectors
  excluded by the coarse route.
- Runtime/NPC/debug routes can enable local corridor fallback. If the only local
  failure is a corridor-disallowed tile, the route retries local refinement
  without the coarse corridor and reports `LocalPathCorridorFallback`.
- Route results now expose refinement status and reason metadata, including
  disabled local refinement, missing local tiles, delta-empty tiles, and
  corridor-disallowed tiles.
- `RuntimeNavigationService` wraps loaded runtime nav state so NPC/runtime code
  can request routes through one stable API instead of calling content route
  functions directly.
- `NPCNavigationComponent` stores opt-in NPC navigation intent, route result,
  route status, and request revisions.
- `StreamedLevelRuntimeModule` now runs a no-movement NPC navigation route
  refresh system that feeds `NPCNavigationComponent` from current streamed
  runtime nav state.
- Runtime navigation state now carries a monotonic navigation revision; NPC
  route components replan when nav/delta data changes even if the target is
  unchanged.
- `NPCNavigationMovementComponent` adds opt-in waypoint following for refined
  local routes, moving transforms conservatively without physics, avoidance, or
  animation coupling.
- `actiongame` exposes navigation debugging: the NPC HUD reports navigation
  status, target, profile, route revisions, refinement reason, failure tile,
  waypoint counts, snap data, corridor fallback, and movement status. Scene/nav
  debug mode can draw route lines, waypoint spheres, corridor polygons/portals,
  selected NPC handles, snapped endpoint markers, snap lines, and failure
  markers through the VoxelRT gizmo overlay.

Verification:

- graph connectivity tests
- route tests with unloaded local tiles
- refinement tests when local tiles become resident

Remaining manual check:

- NPC far from the player moves sector-to-sector in a simplified unloaded
  simulation, then refines into a local path when the player approaches.

### Phase 8: Multiple Profiles And Extended Capabilities

- Add multiple agent profiles.
- Add crouch areas.
- Add jump link generation.
- Add swimming traversal areas.
- Add source metadata enhancements.
- Defer flying until a separate 3D navigation volume plan is written.

Verification:

- profile-specific path tests
- mixed-size NPC routing tests
- crouch/swim/jump fixture tests

## Implementation Surfaces

Likely engine files:

- `content/nav.go`
- `content/nav_io.go`
- `content/nav_validation.go`
- `content/level.go`
- `content/level_validation.go`
- `streamed_level_runtime.go`
- `npc.go`
- `moving_brush.go`
- new `navmesh*.go` files
- new `mod_navigation*.go` files

Likely importer/editor surfaces:

- `imported_world_baker.go`
- `importers/hl1/emit_level.go`
- `gekko-editor/src/formats`
- level editor debug overlay code, later

Docs to update as implementation lands:

- `docs/content/levels.md`
- `docs/content/streaming-and-worlds.md`
- `docs/engine/modules.md`
- `docs/engine/verification.md`
- editor docs when authoring/debug UI exists

## Resolved Decisions

- Use an in-engine voxel-first builder rather than depending on source BSP
  geometry or an external Recast binding for the first working implementation.
- Store generated nav as `.gknav` manifests and `.gknavtile` sidecars.
- Use world chunk coordinates as nav tile coordinates for the current builder.
- Bake static nav through shared `content` helpers, importer integration, and
  `cmd/navbake`.
- Route NPCs through `RuntimeNavigationService` and
  `NPCNavigationComponent`, not by embedding pathfinding directly in
  `NPCComponent`.
- Use actiongame F6/cursor route probes and `navdiag path` as the first manual
  navigation debugging workflow.

## Remaining Decisions

- Whether `.gknavtile` should stay JSON-only for now or gain a compact binary
  payload after the topology stabilizes.
- Whether future nav tile size should remain tied to world chunk size or become
  configurable independently.
- Exact editor ownership for filled navmesh rendering, selected polygon/portal
  inspection, and authoring of nav modifiers.
- How much of far NPC simulation should live in generic engine systems versus
  actiongame-specific AI behavior.
- First concrete off-mesh-link gameplay slice: drop links, ladders, openable
  doors, or water transitions.

## Completed First Slice

The original proof slice is complete:

1. Schema for `.gknav` and `.gknavtile`.
2. One HL1-like agent profile.
3. Voxel-occupancy walkability builder for tiles.
4. Load/query point projection for static and effective static/delta tiles.
5. Tests and manual checks for flat floors, stairs, ramps, walls, ledges,
   chunk seams, endpoint snapping, and path refinement.

The next work should not restart this slice. It should harden the current
system with regression fixtures, robust contour/portal internals, dirty rebuild
integration, and off-mesh traversal.
