# NPC Navigation And Navmesh Plan

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
- NPCs exist as level-spawned components, but do not yet own navigation.

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

It should use tiled navmesh data derived from voxel occupancy. The builder can
be Recast-style even if the first implementation is a conservative in-engine
builder:

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

The first local query layer is intentionally small:

- `NewNavTileQuery(...)`
- point-to-polygon lookup on a single tile
- polygon-center waypoint output
- breadth-first traversal across same-tile polygon neighbor IDs

Cross-tile stitching and off-mesh traversal should build on this instead of
duplicating tile parsing in NPC logic.

The first cross-tile path layer should stay conservative: use effective tile
lookup, traverse same-tile polygon neighbors, and connect adjacent tiles only
when polygon bounds touch across the shared tile face. Off-mesh links, richer
portals, and funnel/string-pull smoothing are separate follow-ups.

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

The first version can be simple:

- route between sectors using adjacency
- estimate travel time by center-to-center distance and agent speed
- defer tactical reasoning
- require local refinement only near active gameplay

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

Verification:

- `cd gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`

### Phase 2: Voxel Occupancy Walkability Prototype

- Build a deterministic nav-tile generator from voxel occupancy.
- Start with one HL1-like walking profile.
- Generate walkable spans with clearance, step, slope, and radius filtering.
- Add tests for flat floor, wall, stairs, ledge, one-voxel lip, and narrow gap.
- Do not integrate NPC behavior yet.

Verification:

- `cd gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./content/... . -run 'TestNav|TestNavigation'`

### Phase 3: Static Bake And Runtime Load

- Bake `.gknav` and `.gknavtile` sidecars during level/imported-world creation.
- Add runtime loading of nav manifest.
- Load nav tiles beside streamed world chunks.
- Add debug queries for tile presence and basic point projection.

Verification:

- `cd gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./content/... ./importers/hl1/... . -run 'TestNav|TestNavigation|TestStreamedRuntime'`

Manual check:

- Run an imported HL1-style map and draw nav tile bounds/walkable regions.

### Phase 4: Query API And NPC Local Pathing

- Add `NavigationModule`.
- Add path request/result API.
- Add basic NPC navigation agent components.
- Support local path queries inside loaded tiles.
- Add simple path following for walking NPCs.

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

Verification:

- tests for dirty tile selection
- tests for delta nav save/load
- streamed runtime test where edited chunk reloads with rebuilt nav

Manual check:

- Edit or destroy voxels, watch dirty overlay, confirm NPC path changes after
  rebuild.

### Phase 6: Off-Mesh Links And Dynamic Obstacles

- Generate ladder links from `ladder_volumes`.
- Add door/moving-brush traversal links.
- Plan through closed openable doors with action costs.
- Add drop links.
- Add authored link/modifier support.

Verification:

- tests for door edge state/cost
- tests for ladder link generation
- tests for one-way drop links

Manual check:

- NPC routes through an openable door and climbs a ladder.

### Phase 7: Hierarchical Sector Graph

- Build graph from imported-world sectors and adjacency.
- Add sector route query.
- Connect sector routes to local tile refinement.
- Simulate far NPC movement with simple sector traversal and travel-time
  estimates.

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
- Local refinement is constrained to tile coordinates overlapped by the selected
  sector corridor, so detailed paths do not wander through sectors excluded by
  the coarse route.
- Route results now expose refinement status and reason metadata, including
  disabled local refinement, missing local tiles, delta-empty tiles, and
  corridor-disallowed tiles.
- `RuntimeNavigationService` wraps loaded runtime nav state so NPC/runtime code
  can request routes through one stable API instead of calling content route
  functions directly.
- `NPCNavigationComponent` stores opt-in NPC navigation intent, route result,
  route status, and request revisions without moving entities yet.
- `StreamedLevelRuntimeModule` now runs a no-movement NPC navigation route
  refresh system that feeds `NPCNavigationComponent` from current streamed
  runtime nav state.
- Runtime navigation state now carries a monotonic navigation revision; NPC
  route components replan when nav/delta data changes even if the target is
  unchanged.
- `NPCNavigationMovementComponent` adds opt-in waypoint following for refined
  local routes, moving transforms conservatively without physics, avoidance, or
  animation coupling.
- `actiongame` now exposes first-pass navigation debugging: the existing NPC HUD
  reports navigation status, target, profile, route revisions, refinement
  reason, failure tile, waypoint counts, and movement status; scene/nav debug
  mode can also draw route lines, waypoint spheres, and target markers through
  the VoxelRT gizmo overlay.

Verification:

- graph connectivity tests
- route tests with unloaded local tiles
- refinement tests when local tiles become resident

Manual check:

- NPC far from the player moves sector-to-sector, then refines into a local path
  when the player approaches.

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

## Open Decisions Before Code

These should be decided before broad implementation:

- Whether to use a Go Recast binding, port a subset, or write a conservative
  voxel-heightfield builder first.
- Exact `.gknavtile` binary payload layout.
- Tile size: same as world chunk, sector subdivision, or configurable nav tile
  size.
- Whether static nav bake happens in `content`, importers, a command, or all of
  them through shared helpers.
- Filled navmesh polygon rendering ownership after route/waypoint gizmos are no
  longer enough.
- First NPC behavior scenario in `actiongame`.

## Recommended First Slice

Start with the smallest vertical slice that proves the architecture:

1. Schema for `.gknav` and `.gknavtile`.
2. One HL1-like agent profile.
3. Voxel-occupancy walkability builder for one tile.
4. Load/query point projection for a static tile.
5. Tests for flat floor, stairs, wall, ledge, and narrow gap.

Do not start with NPC combat behavior, global routing, or dynamic doors. Those
features should sit on top of the proven tile contract.
