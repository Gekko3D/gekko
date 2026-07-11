# Pure-Go Voxel Navigation Graph Replacement Plan

## Status

This document is the authoritative implementation plan for replacing Gekko's
current polygon-navmesh navigation stack.

The replacement is intentionally breaking:

- no backward-compatible schemas
- no compatibility adapters
- no old sidecar migration
- no parallel legacy/new builders
- no polygon-navmesh fallback
- no requirement to preserve existing pathfinding or gameplay APIs

Navigation functionality may be temporarily absent while phases are completed.
The workspace must still compile after each phase; broken functionality is
acceptable, a permanently broken build is not.

This is a long-term architecture replacement, not a tactical bridge.

## Decision Summary

Gekko will use a **voxel navigation graph**, not a polygon navmesh.

```text
effective voxel occupancy
  -> profile-independent surface spans
  -> profile-filtered directed span graph
  -> compact region graph
  -> cross-tile transitions
  -> hierarchical route
  -> local span refinement
  -> physics-driven locomotion
```

Polygons are not generated, stored, queried, rendered, or used for movement.
There is no optional polygon phase.

## Why Replace The Current Stack

The current system has two generation paths:

- CGO/Recast (`voxel_recast_v1`)
- the custom Go polygon builder (`voxel_nav_v28`)

The costs are no longer justified:

- Recast requires C++, CGO, and platform-specific build tooling.
- The custom builder has produced incomplete coverage and polygon artifacts.
- Polygonization mixes geometry approximation with reachability, allowing a
  contour or merge defect to remove valid routes or invent walkable space.
- Existing pathfinding, portals, smoothing, debug tools, and gameplay helpers
  carry polygon IDs throughout the stack.
- Preserving those contracts would force the new graph into an old navmesh
  shape and keep complexity that no longer serves the project.

The replacement separates two concerns:

- navigation decides where an agent can fit and which transitions it can make;
- locomotion and physics decide how the agent follows the local surface.

## Known Facts

- Effective voxel occupancy is the authority for ordinary support and
  obstruction.
- Navigation must support stacked floors in one X/Z column.
- The world is chunked and streamed.
- Runtime voxel edits must invalidate and rebuild only affected navigation.
- Shooter NPCs need stairs, steps, drops, jumps, doors, ladders, water, and
  moving-platform transitions over time.
- Long paths must not expand every surface voxel.
- Crossfire, Gasworks, and a dedicated navigation lab are the manual acceptance
  environments.
- Source/BSP metadata can annotate or remove traversal, but it must not invent
  ordinary walkable support absent from effective voxel geometry.

## Explicitly Discarded Contracts

The following are not preserved:

- `NavPolygonDef`
- tile vertex/polygon arrays
- polygon IDs in route steps, portals, links, probes, or debug state
- `NavTileQuery`
- polygon lookup and point projection
- polygon-neighbor pathfinding
- polygon-derived cross-tile portals
- funnel or polygon-corridor smoothing
- Recast debug output
- `voxel_recast_v1` and `voxel_nav_v28`
- current `.gknavtile` payload compatibility
- current navigation tests whose only purpose is preserving those contracts

Existing generated navigation assets are deleted and regenerated later.

## Concepts Worth Retaining

These ideas remain useful, but their current APIs and schemas are not protected:

- agent profiles
- chunk/tile coordinates
- shared profile-independent source data
- deterministic builder versions and hashes
- generated sidecars
- static/delta override ownership
- dirty-chunk dependency expansion
- asynchronous rebuild scheduling
- navigation revisions
- sector-level coarse routing
- dynamic obstacle/traversal overlays
- NPC movement intent

Code implementing these concepts may be reused only after it is shown to fit
the new graph directly. Do not keep an old type merely because callers exist.

## Non-Goals For The First Working Graph

- polygon generation of any kind
- optimal or visually smooth paths
- crowd simulation
- tactical cover selection
- flying navigation
- automatic jump-link discovery
- perfect dynamic avoidance
- binary sidecars
- editor authoring UI
- preservation of current navigation debug controls

## Authority And Ownership

### Engine Content

Owns:

- graph schemas and validation
- pure-Go span and graph generation
- save/load helpers
- static and delta sidecar resolution
- deterministic build hashes
- topology diagnostics

### Runtime And Streaming

Owns:

- graph-tile residency
- dirty-chunk tracking
- asynchronous rebuild scheduling
- revision swaps
- route request budgeting

### Actiongame

Owns:

- navigation intent
- tactical goal selection
- local steering
- physics/capsule movement
- door and breakable behavior
- gameplay debug controls

Pathfinding reports traversability and required actions. It does not own combat
movement or physics.

## Target Data Model

Names below are proposed contracts, not compatibility aliases. Final naming can
change during Phase 1 before persistence code depends on it.

### Shared Surface Span

A span describes one supported open interval in a voxel column.

```go
type NavSpanDef struct {
    ID              uint32
    X               int
    Y               int
    Z               int
    SupportHeight   float32
    CeilingHeight   float32
    Headroom        float32
    ClearanceRadius float32
    Area            string
    Flags           []string
}
```

Requirements:

- spans are sorted deterministically by X, Z, then Y
- one X/Z column may contain multiple spans
- IDs are tile-local and derived from sorted order
- topology uses integer coordinates
- world-space floats are descriptive values, not connectivity keys

### Span Reference

```go
type NavSpanRef struct {
    Tile TerrainChunkCoordDef
    Span uint32
}
```

### Directed Span Transition

```go
type NavSpanTransitionDef struct {
    From            uint32
    To              NavSpanRef
    Kind            string
    StepDelta       float32
    Width           float32
    MinHeadroom     float32
    MinClearance    float32
    Cost            float32
    RequiresFlags   []string
}
```

Transitions are directed because step-up, drop, jump, door, and capability
rules are not necessarily symmetric.

### Compact Region

```go
type NavRegionDef struct {
    ID              uint32
    SpanRuns        []NavSpanRunDef
    BoundsMin       Vec3
    BoundsMax       Vec3
    Center          Vec3
    HeightMin       float32
    HeightMax       float32
    Area            string
}
```

A region compresses spans already connected by compatible transitions. It may
never introduce connectivity missing from the span graph.

### Region Transition

```go
type NavRegionTransitionDef struct {
    ID              uint32
    FromRegion      uint32
    ToTile          TerrainChunkCoordDef
    ToRegion        uint32
    Kind            string
    CrossingStart   Vec3
    CrossingEnd     Vec3
    Width           float32
    MinHeadroom     float32
    MinClearance    float32
    Cost            float32
    RequiresFlags   []string
}
```

The crossing segment is a waypoint/clearance fact derived from matching span
edges. It is not a polygon portal.

### Graph Tile

One source tile is profile-independent. A graph tile may be profile-specific so
unsupported spans and transitions are removed at bake/query time without
duplicating source extraction.

```go
type NavGraphTileDef struct {
    NavID             string
    Coord             TerrainChunkCoordDef
    AgentProfileID    string
    BuilderVersion    string
    SourceHash        string
    DependencyHash    string
    Regions           []NavRegionDef
    Transitions       []NavRegionTransitionDef
}
```

The persisted schema may also retain compact span membership required for
local refinement. Do not duplicate full source spans inside every profile tile
unless measurements show it is cheaper than loading the shared source tile.

### Route Result

```go
type NavRouteStep struct {
    Tile              TerrainChunkCoordDef
    Region            uint32
    EnterTransition   uint32
    Target            Vec3
    RequiredAction    string
}

type NavRouteResult struct {
    Found             bool
    Steps             []NavRouteStep
    Waypoints         []Vec3
    FailureReason     string
    FailureTile       TerrainChunkCoordDef
    NavigationRevision uint64
}
```

There are no polygon IDs.

## Surface Following Without Polygons

Navigation does not need to reproduce every bump in route geometry.

The route supplies region crossings and short local span anchors. Locomotion:

1. moves toward the next anchor;
2. uses capsule collision and downward support queries;
3. samples span or region support height when a navigation hint is useful;
4. keeps the character controller responsible for contact and step handling;
5. requests repair/replan when progress or support validation fails.

Stairs are graph transitions across stepped spans. Smooth ramps may use
optional source-normal/height metadata for movement hints, but graph
connectivity still comes from effective voxel support and clearance.

## Pathfinding Shape

Use three levels:

```text
sector graph
  -> compact region graph
  -> short span-graph refinement around current/final transitions
```

- Sector A* limits long-distance search.
- Region A* is the normal local route.
- Span A* is limited to start/end connection, local repair, and a small active
  corridor.
- Other NPCs are local avoidance inputs, not baked graph blockers.
- Dynamic doors and movers change transition state/cost without rebuilding
  static spans.
- Voxel destruction/addition rebuilds affected source and graph tiles.

Initial waypoint generation is deliberately simple: region crossing centers
plus the final target. Add line-of-travel simplification only when movement
shows a real need. Do not add a funnel algorithm without polygons.

## Debugging Without Polygons

The replacement debug view renders:

- accepted span points or small boxes
- rejected spans by reason
- directed span edges near the selected NPC
- region bounds and IDs
- cross-region and cross-tile transitions
- route steps and waypoints
- dirty/rebuilding/revision state
- dynamic transition state

Cursor targeting resolves the collision hit to the nearest supported span. NPC
teleport and move commands use that span/region reference. Filled navigation
surfaces are unnecessary.

## Non-Negotiable Invariants

### Source

- Every emitted span has known solid support and a known open interval.
- Every valid vertical span is preserved.
- Unknown halo data never becomes empty space.
- Identical effective inputs produce deterministic output.

### Agent Filtering

- Accepted spans meet height and radius requirements.
- A larger compatible agent cannot gain clearance-only spans absent for a
  smaller agent.
- Modifier hints may subtract ordinary traversal but cannot invent support.

### Graph

- Every transition references real supported spans or regions.
- Every ordinary transition satisfies step, headroom, and clearance limits.
- Region compression preserves span-graph reachable components.
- Cross-tile transitions are backed by matching boundary span facts.
- Bidirectional transitions are reciprocal; one-way transitions are explicit.

### Runtime Edits

- Full build and delta rebuild use the same generator.
- Dependency hashes include every halo chunk that affects output.
- Removed support removes affected nodes and transitions.
- Empty delta results suppress stale static data.
- Revision swaps never combine incompatible old/new neighboring transitions.

## Lean Verification Policy

Follow the workspace testing policy:

- test stable navigation facts immediately;
- manually test unstable presentation and gameplay integration;
- add regression tests only for deterministic real bugs;
- delete tests with the legacy APIs they protected.

The minimum durable automated suite is:

1. one table-driven span/clearance test covering flat ground, stacked floors,
   holes, headroom, radius, steps, and unknown halo;
2. one graph test covering reachable/unreachable components and directed
   transitions;
3. one cross-tile transition test;
4. one full-build/delta-rebuild parity test;
5. one persistence round-trip test after the schema stabilizes.

Do not add tests for every helper, transient API, exact ordering beyond required
determinism, debug formatting, or temporary gameplay wiring.

## Implementation Rules For AI Agents

- Implement one numbered phase or named subphase at a time.
- Read this entire plan and the phase's owner files first.
- State known facts, uncertainty, touched files, and verification before edits.
- No CGO, C++, Recast, or new dependency.
- No polygon schema, generator, query, fallback, or debug adapter.
- No compatibility layer for old sidecars or APIs.
- Prefer deletion to deprecation.
- Keep the workspace compiling after every phase.
- It is acceptable for navigation gameplay to be unavailable between phases.
- Do not preserve a caller by inventing a temporary abstraction; remove or
  disable the caller until the new graph reaches it.
- Reuse old logic only after proving it implements a new invariant directly.
- Run the smallest relevant compile/test command and report manual checks still
  required.

## Phase 0: Remove The Polygon Navigation Stack

Purpose: remove architectural gravity before implementing replacement types.

Split this phase into compile-clean substeps.

### Phase 0A: Remove Recast

- [x] Delete `content/recastnav`.
- [x] Delete `third_party/recastnavigation`.
- [x] Delete `content/nav_build_recast.go` and its test.
- [x] Delete `content/nav_recast_debug.go`.
- [x] Remove Recast builder constants, dispatch, flags, docs, and tests.
- [x] Confirm `CGO_ENABLED=0` content compilation no longer fails because of
  navigation.

### Phase 0B: Remove Polygon Contracts And Algorithms

- [x] Delete polygon fields and polygon-ID references from navigation schemas.
- [x] Delete `NavTileQuery` and query cache.
- [x] Delete polygon pathfinding and polygon smoothing.
- [x] Delete polygon-derived portal generation and validation.
- [x] Delete the old Go polygon builder rather than extracting a compatibility
  path from it.
- [x] Delete tests whose assertions are specifically about polygons, contours,
  polygon neighbors, polygon portals, or old builder versions.

Likely owner files:

- `content/nav.go`
- `content/nav_build.go`
- `content/nav_query.go`
- `content/nav_query_cache.go`
- `content/nav_path.go`
- `content/nav_path_smooth.go`
- `content/nav_portal.go`
- `content/nav_validation.go`
- their associated tests

### Phase 0C: Remove Consumers

- [x] Remove polygon-dependent actiongame navigation systems and debug code.
- [x] Remove polygon-dependent runtime navigation service paths.
- [x] Delete `examples/navmesh_lab`; a graph lab is created later.
- [x] Remove old generated navigation assets and importer bake hooks.
- [x] Remove or temporarily disable NPC navigation registration until the new
  route service exists.
- [x] Keep unrelated world streaming, NPC components, physics, and voxel
  editing compiling.

### Phase 0 Gate

- no Recast/CGO navigation code remains;
- no polygon navigation type or API remains;
- no old navigation builder remains;
- engine and directly affected modules compile;
- navigation functionality may be absent.

## Phase 1: Define The New Graph Contracts

Purpose: introduce only the minimal types needed by subsequent phases.

- [x] Define source span, span reference, directed transition, region, region
  transition, graph tile, manifest, and route result.
- [x] Define agent profile fields actually required for walking.
- [x] Define validation for IDs, bounds, references, and numeric constraints.
- [x] Define new sidecar extensions/names only if generic `.gknav` naming is no
  longer clear; no compatibility requirement exists.
- [x] Add save/load only after the in-memory shape is accepted.
- [x] Add one persistence test because persisted schemas are stable contracts.

Suggested files:

- `content/nav_graph.go`
- `content/nav_graph_io.go`
- `content/nav_graph_validation.go`

Gate: the new schema is small, polygon-free, deterministic, and has no runtime
or generator logic.

## Phase 2: Extract Profile-Independent Surface Spans

Purpose: convert effective voxel occupancy into trustworthy supported open
intervals.

- [ ] Define build input with center chunk plus explicit known/unknown halo.
- [ ] Build deterministic occupancy columns.
- [ ] Extract every solid-to-empty support transition and open ceiling.
- [ ] Preserve multiple vertical spans per X/Z column.
- [ ] Calculate descriptive support position and headroom.
- [ ] Sort spans and assign stable local IDs.
- [ ] Expose rejection/unknown-context diagnostics.

Suggested files:

- `content/nav_graph_build_span.go`
- `content/nav_graph_build_span_test.go`

Gate: the table-driven source test passes for flat ground, hole, stacked floors,
and unknown halo. No graph regions or pathfinding exist yet.

## Phase 3: Calculate Clearance And Profile Support

Purpose: determine which spans and local transitions support an agent.

- [ ] Compute conservative horizontal clearance.
- [ ] Filter by agent height and radius.
- [ ] Apply blocker metadata as subtraction only.
- [ ] Record stable rejection reasons.
- [ ] Verify small/large profile monotonicity.
- [ ] Keep the first algorithm simple and correct; optimize only after profiling.

Suggested files:

- `content/nav_graph_clearance.go`
- `content/nav_graph_clearance_test.go`

Gate: accepted spans cannot intersect occupied/blocked volume under the chosen
capsule or conservative cylinder model.

## Phase 4: Build The Directed Span Graph

Purpose: establish correct local reachability before compression.

- [ ] Create four-neighbor transition candidates.
- [ ] Validate step delta, transition headroom, and clearance.
- [ ] Emit directed `walk`, `step`, and `stair` transitions.
- [ ] Keep drops, jumps, ladders, water, doors, and movers out of the first
  graph.
- [ ] Implement minimal span-graph A* for verification and later local repair.
- [ ] Return explicit no-route/rejection diagnostics.

Suggested files:

- `content/nav_graph_build_edge.go`
- `content/nav_graph_search_span.go`
- one table-driven graph test

Gate: fixture reachability matches voxel/agent facts without regions, sectors,
or persistence.

## Phase 5: Compress Spans Into Regions

Purpose: make ordinary routing cheap without changing reachability.

- [ ] Group only spans connected by already-valid compatible transitions.
- [ ] Split regions when area or traversal class changes.
- [ ] Retain compact span membership for local refinement.
- [ ] Build contiguous transition runs between regions.
- [ ] Compare region-graph components with span-graph components.
- [ ] Store center, bounds, and height range for heuristic and locomotion hints.

Suggested files:

- `content/nav_graph_region.go`
- `content/nav_graph_region_test.go`

Gate: region compression preserves reachable components and substantially
reduces nodes on a flat field.

## Phase 6: Connect Graph Tiles

Purpose: create deterministic graph transitions across chunk boundaries.

- [ ] Match opposing boundary span facts in world-grid coordinates.
- [ ] Validate height, headroom, clearance, and direction on both sides.
- [ ] Merge contiguous compatible matches into region transitions.
- [ ] Emit reciprocal transitions for bidirectional movement.
- [ ] Fail closed for unknown neighbor context.
- [ ] Include dependency halo in build hashes.

Suggested files:

- `content/nav_graph_boundary.go`
- `content/nav_graph_boundary_test.go`

Gate: the minimal cross-tile test passes in all four horizontal directions and
rejects blocked/unknown seams.

## Phase 7: Implement Hierarchical Routing

Purpose: replace the old polygon pathfinder with one graph route API.

- [ ] Resolve start/end world points to supported spans.
- [ ] Run region A* as the normal local route.
- [ ] Refine start/end and a small active corridor through the span graph.
- [ ] Add sector-graph restriction for long paths.
- [ ] Return region steps, transitions, simple waypoints, actions, and reasons.
- [ ] Add route budgets and cancellation only when runtime integration needs
  them.
- [ ] Do not add polygon corridors or funnel smoothing.

Suggested files:

- `content/nav_graph_query.go`
- `content/nav_graph_route.go`

Gate: synthetic and manual map routes use region search for distance and span
search only in bounded local windows.

## Phase 8: Add Persistence, Baking, And Diagnostics

Purpose: generate, inspect, save, and reload graph data.

- [ ] Implement one pure-Go full-world graph bake.
- [ ] Save source and graph tiles deterministically.
- [ ] Validate before writing; do not save partial invalid graphs.
- [ ] Replace `navbake` and `navdiag` around the new graph contracts.
- [ ] Create `examples/navigation_graph_lab`.
- [ ] Render spans, regions, transitions, routes, and failure reasons.
- [ ] Bake Crossfire and Gasworks into temporary output.

Gate: save/load preserves topology, diagnostics show no hard graph errors, and
manual graph coverage is accepted on the lab and representative maps.

## Phase 9: Restore Runtime Streaming And Delta Rebuilds

Purpose: make graph navigation follow streamed and edited voxel worlds.

- [ ] Stream source/graph tiles near navigation observers.
- [ ] Reuse world dirty-chunk notifications.
- [ ] Expand dirty dependencies by required halo.
- [ ] Run full bake and delta rebuild through the same generator.
- [ ] Persist delta graph overrides beside world delta data.
- [ ] Swap revisions atomically across affected neighboring transitions.
- [ ] Suppress stale static data with explicit empty overrides.

Gate: removed floor, restored floor, blocker, and seam edits update routes after
revision swap; full and delta generation agree for identical effective input.

## Phase 10: Restore Actiongame Locomotion

Purpose: move shooter NPCs using graph routes and physics.

- [ ] Restore navigation intent against the new route API.
- [ ] Follow region crossing/waypoint targets.
- [ ] Use character-controller collision and support queries for unevenness.
- [ ] Detect stalled, unsupported, or invalidated routes.
- [ ] Request bounded local repair or replan.
- [ ] Restore cursor targeting by collision hit -> nearest supported span.
- [ ] Restore debug selection, teleport, and move commands without polygon IDs.

Gate: selected NPCs traverse flat ground, stairs, uneven surfaces, and tile
seams on the lab, Crossfire, and Gasworks without polygon data.

## Phase 11: Add Shooter Traversal And Dynamic State

Implement one observed gameplay need at a time:

- drops
- authored/validated jumps
- ladders
- water
- doors
- breakables
- moving platforms
- temporary blockers and local avoidance

Use explicit directed transitions and runtime state/cost overlays. Do not
rebuild static spans for a door opening or another NPC moving.

## Phase 12: Optimize Measured Bottlenecks

Only after correctness and manual acceptance:

- bitset or distance-field clearance
- cached profile-independent source work
- parallel tile builds with deterministic save order
- compact/binary serialization
- region clustering for very large open worlds
- incremental within-tile rebuilds
- flow fields for many NPCs sharing a goal

Every optimization must preserve the stable invariant tests and manual route
behavior.

## Verification Commands

Use the smallest command matching the phase.

Content work:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./content/...
```

Pure-Go boundary after Phase 0:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env CGO_ENABLED=0 GOCACHE=/tmp/gekko3d-gocache-nocgo go test ./content/...
```

Runtime integration:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test . ./content/...
```

Actiongame integration:

```bash
cd /Users/ddevidch/code/go/gekko3d/actiongame
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

These commands are compile/regression checks, not a request to recreate the old
large test suite.

## Manual Acceptance

At graph, runtime, and locomotion milestones:

- inspect flat fields, holes, stacked floors, stairs, ledges, narrow gaps, and
  all four chunk seams;
- compare accepted spans against visible/collision geometry;
- run named start/end routes through Crossfire and Gasworks;
- confirm large agents reject narrow passages;
- confirm locomotion stays supported without floating or oscillation;
- edit/remove/restore voxels and confirm revisioned route changes;
- record map, coordinates, graph version, and outcome for every real failure.

Presentation quality remains manual during prototyping. Stable topology bugs
receive the smallest deterministic regression fixture.

## Reusable AI-Agent Prompt

```text
Implement only Phase <N> / subphase <name> from
gekko/docs/roadmaps/pure-go-voxel-navigation-generation-plan.md.

Before editing:
- read the whole plan and listed owner files
- state known facts, uncertainty, whether this is deletion/schema/generation/
  runtime/gameplay work, exact files, and verification
- stop if the proposed work reintroduces polygons or compatibility code

Constraints:
- pure Go, no new dependency
- no polygon navmesh types, generation, query, fallback, or debug adapter
- effective voxel occupancy is canonical
- break old APIs instead of preserving them
- remove/disable old consumers so the workspace still compiles
- implement no later phase
- add only the smallest stable invariant test required by the lean test policy

After editing:
- run the smallest relevant compile/test command
- report automatic checks, manual checks, and intentionally untested behavior
- mark a checkbox only when its gate is satisfied
```

## Completion Criteria

The replacement is complete when:

- no Recast, CGO, C++, polygon navmesh, polygon query, or legacy builder remains;
- one pure-Go span/region graph serves full bake and delta rebuild;
- long routes use sector/region search rather than raw span expansion;
- local refinement and locomotion work without polygon data;
- graph streaming and revision swaps follow voxel edits;
- Crossfire, Gasworks, and navigation graph lab pass manual coverage and route
  checks;
- the small stable invariant suite passes;
- navigation APIs and docs describe spans, regions, transitions, and routes,
  never polygons.
