# Dynamic Shooter Navigation Plan

This plan describes the long-term movement and pathfinding architecture for a
shooter game with editable voxel geometry, multiple NPC types, dynamic doors,
breakables, and real-time combat movement.

It builds on the existing voxel-first navigation direction from
[`npc-navigation-navmesh-plan.md`](npc-navigation-navmesh-plan.md). The goal is
not to keep patching the current raw cell walker. The goal is to turn it into a
budgeted, dynamic, shooter-ready navigation stack.

## Current Assessment

The current system is a useful prototype, not the final shooter navigation
architecture.

What is good:

- Navigation is moving toward voxel-first source data instead of HL1 BSP-only
  extraction.
- Shared clearance source data can support multiple NPC profiles without baking
  a full navmesh per NPC type.
- Static nav sidecars and world-delta overrides fit the existing level/content
  model.
- NPC route state and locomotion are separated enough to evolve.
- Runtime route lookup can prefer effective static/delta nav data.

What is not good enough yet:

- Dynamic voxel edits do not yet have a complete async dirty-chunk nav rebuild
  pipeline.
- Long paths through raw clearance cells can be too expensive.
- The sector graph is still mostly a coarse hint, not a full hierarchical route
  product.
- Dynamic doors, movers, breakables, and temporary blockers need a cheap
  obstacle overlay rather than full nav rebuilds.
- Shooter NPCs need tactical movement goals, not only "walk to point."
- Route planning, replanning, debug sampling, and local movement need strict
  per-frame budgets.

## Target Architecture

Use five layers.

```text
Voxel world / world delta
  -> canonical clearance source per chunk
  -> compact local nav regions and portals
  -> coarse sector/region graph
  -> dynamic obstacle and traversal overlay
  -> shooter locomotion and tactical movement
```

The canonical source is voxel data. Derived nav structures are disposable and
rebuildable.

## Design Principles

- Keep nav data chunked and streamable.
- Treat generated nav as sidecar data, not authored level truth.
- Rebuild only dirty chunks and their dependency margin.
- Never rebuild one global navmesh during gameplay.
- Store shared clearance once and filter by agent profile at query time.
- Use coarse graphs for distance and local refinement for movement.
- Keep dynamic obstacles separate from static voxel nav.
- Budget all runtime path work.
- Let shooter behavior choose goals; pathfinding only solves traversability.

## System Ownership

Engine/content owns:

- Navigation content schemas.
- Static and delta nav sidecar resolution.
- Voxel clearance source generation.
- Local region/portal graph generation.
- Hierarchical route query APIs.
- Dynamic nav rebuild job inputs and outputs.

Runtime/streaming owns:

- Dirty chunk detection.
- Async nav rebuild scheduling.
- Nav tile/source residency.
- Navigation revisioning.
- Swapping rebuilt nav into the active service.

Actiongame owns:

- NPC intent, tactical goals, and combat movement choices.
- Local steering and collision response.
- Temporary gameplay obstacle registration.
- Door/breakable traversal semantics.
- Debug overlays and gameplay-level diagnostics.

## Data Model

### Clearance Source

The clearance source is the canonical per-chunk walkability substrate.

Each source tile stores walkable cells/spans with:

- position
- cell coordinate
- clearance radius
- headroom
- slope
- area type
- traversal flags
- source world/delta hash

This data is shared across NPC profiles. A small agent and a large agent query
the same source tile but filter cells differently.

Acceptance criteria:

- One source tile can answer support for multiple profiles.
- Edited chunks can replace source tiles through world-delta nav overrides.
- Source tile lookup is cached and indexed by cell coordinate.

### Local Region And Portal Layer

Raw clearance cells are too fine for shooter-scale pathfinding. Build a compact
local graph from the clearance source.

Each local tile should contain:

- regions made from connected walkable cells
- region bounds and representative center
- border portals to neighbor tiles
- intra-tile portals
- traversal metadata for stairs, ramps, drops, ladders, jumps, doors, water
- optional simplified polygons for debug and movement projection

Acceptance criteria:

- Flat areas collapse into a small number of regions.
- Uneven walkable surfaces remain connected when step/slope rules allow it.
- Ramps produce continuous regions or explicit ramp traversal metadata.
- Cross-tile portals are deterministic and symmetric when traversal is
  bidirectional.

### Coarse Graph

The coarse graph is used for long-distance planning.

It should include:

- sector nodes from imported-world sectors
- optional region-cluster nodes from local nav
- links through portals, doors, ladders, drops, water transitions
- traversal cost estimates
- dynamic link state from obstacle overlay

Acceptance criteria:

- Long-distance pathfinding does not expand raw clearance cells.
- A coarse path can be refined incrementally near the active NPC.
- If the coarse corridor is too restrictive, local fallback is allowed but
  measured and logged.

### Dynamic Obstacle Overlay

Dynamic obstacles should not force full static nav rebuilds.

Overlay entries include:

- doors
- moving brushes
- breakables
- temporary blockers
- disabled areas
- other NPCs for local avoidance only
- scripted traversal gates

Each overlay entry can:

- block a portal
- increase traversal cost
- require an action such as opening a door
- mark an area temporarily unsafe

Acceptance criteria:

- Opening/closing a door updates graph traversal without rebuilding source
  tiles.
- Destroying terrain uses dirty chunk rebuild; moving obstacles use overlay.
- NPC path queries can return required traversal actions.

### Shooter Locomotion

Pathfinding should return a corridor and traversal instructions, not micromanage
combat movement.

Shooter movement consumes:

- current corridor
- next portal or local target
- final tactical goal
- desired stance/speed
- local collision and avoidance state
- line-of-sight constraints

Shooter movement is responsible for:

- stopping distance
- strafing
- facing/aiming while moving
- peeking
- cover approach
- retreating
- short local sidesteps
- unstuck recovery

Acceptance criteria:

- NPC can move to a debug target without frame spikes.
- NPC can follow a corridor while aiming independently.
- NPC can abandon/replan a route when blocked.
- NPC does not run full pathfinding every frame.

## Runtime Pipeline

### Static Bake

1. Load voxel/imported-world chunks.
2. Build clearance source tiles.
3. Build local regions and portals from source tiles.
4. Build coarse graph.
5. Save `.gknav`, `.gknavsource`, local nav tiles, and optional graph sidecars.
6. Validate topology and emit diagnostics.

### Dynamic Edit

1. Voxel edit mutates chunk data.
2. Runtime marks affected chunks dirty.
3. Dirty set expands by agent/query dependency margin.
4. Async job rebuilds clearance source for dirty chunks.
5. Async job rebuilds local regions and portals for dirty chunks.
6. Coarse graph patches affected links.
7. New nav revision becomes available.
8. NPCs keep old routes until a new route is ready.

### Route Query

1. Snap start and end to valid source/local regions.
2. Find coarse route.
3. Refine the near corridor into local regions/portals.
4. Return a corridor, waypoints, portal metadata, required traversal actions,
   and diagnostics.
5. If dynamic edits invalidate the route, mark route stale and request replan
   within budget.

### Movement Frame

1. Consume current route corridor.
2. Sample cached local surface/region for movement projection.
3. Apply local steering and collision.
4. Track progress and stuck state.
5. Request replan only when thresholds or revisions require it.
6. Do not run full route planning from the movement loop unless explicitly
   budgeted.

## Implementation Phases

### Phase 1: Stabilize Current Runtime

Purpose: make the current system usable while the architecture evolves.

Tasks:

- [x] Cache current per-frame nav source lookups used by NPC movement.
- [x] Replace current source-cell scans used by NPC movement with indexed
  lookup.
- [x] Add counters/timing for route planning, source tile loads, surface
  sampling, replans, and local avoidance.
  - Current NPC movement hot paths emit route/replan, clearance source load,
    surface sampling, and local avoidance timings through runtime metrics and
    the NPC debug HUD.
- [x] Throttle selected-NPC debug diagnostics.
  - Cursor route probes and selected-NPC clearance diagnostics reuse cached
    results between budgeted refreshes.
- [x] Add route failure text that distinguishes no route, no local refinement,
  stale nav, blocked portal, and missing dynamic source.
  - Selected-NPC HUD, nav trace, and F9 command reason share one diagnosis
    classifier.

Done when:

- Selecting and moving one NPC does not cause frame collapse.
- Replanning is observable and bounded.
- Debug overlays are optional and cheap when disabled.

### Phase 2: Dirty Chunk Nav Rebuild

Purpose: make edited voxel geometry update navigation.

Tasks:

- [x] Define nav dirty flags on edited chunks.
- [x] Add nav rebuild job inputs from effective chunk data.
- [x] Rebuild clearance source tiles asynchronously.
- [x] Store rebuilt source tiles beside `.gkworlddelta`.
- [x] Swap rebuilt source tiles by navigation revision.
  - Runtime rebuild results now apply both nav tile overrides and clearance
    source tile overrides before bumping navigation revision.
- [x] Add tests for edited floor, removed floor, added blocker, and uneven
  surface.
  - Covered: removed-floor/hole, added-blocker, restored-floor, and uneven
    runtime rebuilds; content delta tests cover uneven-source generation.

Done when:

- Editing ground changes navigation after an async rebuild.
- NPCs keep their current route until replacement nav is available.
- Stale base nav is not used for edited chunks without a matching source
  override.

### Phase 3: Compact Local Regions

Purpose: stop using raw cells as the primary path graph.

Tasks:

- [x] Build connected regions from clearance source cells.
  - Added an in-memory content builder first; serialization can follow once
    routing consumes the region graph.
- [x] Generate intra-tile portals.
  - Area boundaries such as walk/ramp now produce reciprocal local-region
    portals with clearance metadata.
- [x] Generate cross-tile portals.
  - Adjacent boundary regions now stitch through reciprocal portals using the
    same wrapped neighbor rules as clearance-source pathing.
- [x] Store simplified polygons or projection data per region.
  - Local regions now carry projection metadata with bounds, centroid, height
    range, and a fitted height plane for movement sampling.
- [x] Convert raw cell paths into region/portal paths.
  - Raw clearance paths can now be compressed into region steps plus portal
    crossings while preserving raw cells for repair/debug.
- [x] Keep raw cells available only for local repair and debug.
  - Effective clearance paths now expose compact region steps as the normal
    route and retain raw cells under `RegionPath` for repair/debug detail.

Done when:

- Flat fields route as a handful of regions, not hundreds of cells.
- Uneven but walkable surfaces remain connected.
- Ramps have connected coverage.
- Movement projection uses region/polygon data, not repeated source scans.

### Phase 4: Hierarchical Routing

Purpose: make long routes cheap and robust.

Tasks:

- [x] Build a coarse graph from sectors plus local region clusters.
  - Added an in-memory clearance coarse graph over local regions and portal
    edges. Live route planning still needs to consume it in a later step.
- [x] Include traversal costs and link metadata.
  - Coarse region edges now carry portal traversal kind, cost, width,
    required width, clearance, endpoint coords, and source metadata. Effective
    clearance routing tries the region graph before falling back to raw cells.
- [x] Refine only a local window around the NPC.
  - Compact region path steps now carry projection metadata, and actiongame
    samples only the current/neighbor corridor window from that metadata before
    falling back to raw cell or legacy polygon sampling.
- [x] Add corridor widening/fallback rules.
  - Local avoidance now accepts short repair candidates within a conservative
    agent-radius margin around the current corridor window, while projection
    still clamps movement back onto the sampled nav surface.
- [x] Add path request budgets and priorities.
  - Automatic NPC replans now pass through a per-frame route budget with
    blocked/stalled/portal priorities and deferred metrics. Explicit debug/F9
    move commands remain immediate.

Done when:

- Long Crossfire routes do not expand source cells across the map.
- Far NPC route requests are cheap.
- Active NPCs receive local corridors around their current position.

### Phase 5: Dynamic Obstacle Overlay

Purpose: handle shooter-world changes without unnecessary nav rebuilds.

Tasks:

- [x] Add runtime obstacle/link overlay API.
  - Added a dynamic nav overlay model for link, traversal-kind, and sector-pair
    entries. Sector routing now applies overlay blocking/cost changes and
    emits traversal action markers.
- [x] Represent doors as portal state/cost/action entries.
  - Sector-link overlays can now match nav link target names. Actiongame
    exports closed moving-brush doors as dynamic traversal action entries so
    NPCs can plan through openable links and request the door before crossing.
- [x] Represent temporary blockers as local cost/blocked areas.
  - Dynamic overlay entries can now describe world-space bounds. Sector
    routing blocks or reprices edges whose center segment intersects those
    bounds, and actiongame exports active temporary navigation obstacles as
    bounds overlays without rebuilding baked nav data.
- [x] Represent breakables as overlay until destroyed, then dirty chunk rebuild
  if geometry changes.
  - Breakables now carry explicit runtime navigation mode/cost metadata. Intact
    blocking/cost breakables are exported as dynamic bounds overlays, broken
    breakables disappear from the overlay immediately, and destructive voxel
    edits now mark runtime-edited geometry so the existing dirty nav rebuild
    pipeline can refresh affected imported-world chunks.
- [x] Expose traversal actions to NPC behavior.
  - Sector traversal actions now preserve link target names. NPC route-follow
    state consumes each action once per planned route when the NPC reaches the
    action's source sector; `open_door` forces the matching target open through
    the existing target system.

Done when:

- Door open/close changes NPC route cost/blocking immediately.
- NPCs can plan through openable doors with an action marker.
- Temporary blockers do not trigger static nav rebuild.

### Phase 6: Shooter Tactical Movement

Purpose: make NPC movement feel appropriate for combat.

Tasks:

- Add tactical movement goals: move to point, approach, retreat, hold distance,
  flank, seek cover, peek.
- Add corridor-aware strafing and facing.
- Add local path repair for short sidesteps.
- Add cover/line-of-sight query hooks.
- Add movement status useful to behavior trees/schedules.

Done when:

- NPCs can move while aiming.
- NPCs do not overshoot final tactical distance.
- NPCs can replan or locally recover from blockers.
- Behavior can decide when to abandon a target.

## Verification Plan

Automated tests:

- Clearance source supports multiple agent profiles.
- Source tile load/cache/index lookup is stable.
- Dynamic chunk edit emits source override.
- Edited chunk without source override is rejected.
- Local regions merge flat surfaces.
- Ramps and uneven surfaces remain connected when profile rules allow.
- Cross-tile portals are symmetric where appropriate.
- Coarse route refines into local corridor.
- Dynamic door overlay blocks/unblocks route.
- NPC selected move command creates route and movement intent.

Performance checks:

- Route query timing for short, medium, and long routes.
- Per-frame movement sampling count and time.
- Source tile load count per frame.
- Replan count per NPC per second.
- Dirty rebuild queue time and swap time.

Manual checks:

- Crossfire field movement.
- Uneven outdoor surface.
- Ramps.
- Door pathing.
- Live voxel edit: add floor, remove floor, add obstacle.
- Multiple NPCs moving at once.
- Selected NPC debug overlay enabled and disabled.

Suggested commands:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
GOCACHE=/private/tmp/gekko-go-build go test ./content/...
GOCACHE=/private/tmp/gekko-go-build go test ./...

cd /Users/ddevidch/code/go/gekko3d/actiongame
GOCACHE=/private/tmp/gekko-go-build go test ./...
```

## Milestone Checklist

- [x] Current movement hot paths are cached and indexed.
- [ ] Nav debug diagnostics are budgeted.
- [x] Edited voxel chunks mark nav dirty.
- [x] Dirty chunks rebuild clearance source asynchronously.
- [x] Runtime swaps nav source/tile revisions safely.
- [ ] Local compact regions replace raw cell paths for normal routing.
- [ ] Cross-tile portals are validated on real maps.
- [ ] Coarse graph drives long-distance route planning.
- [ ] Dynamic obstacle overlay handles doors/movers/blockers.
- [ ] Shooter tactical movement consumes corridors and traversal actions.
- [ ] Crossfire selected-NPC movement is smooth on representative routes.

## Open Questions

- Should local compact regions be stored as polygons, cell-runs, or both?
- Should dynamic obstacle overlay live entirely in engine runtime, or should
  actiongame own gameplay-specific traversal policies?
- How much path request scheduling belongs in the shared runtime versus the
  game module?
- What is the minimum tactical cover representation needed for the first shooter
  NPC milestone?
- What are the target route-planning and movement-frame budgets on the intended
  hardware?
