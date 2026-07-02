# Pure-Go Voxel Navigation Generation Plan

## Status And Intent

This document defines the long-term replacement for both navigation generation
backends currently present in Gekko:

- the CGO/Recast production bake path (`voxel_recast_v1`)
- the custom Go polygon builder (`voxel_nav_v28`)

The replacement is a pure-Go, direct voxel-to-navigation pipeline. It keeps the
existing navigation ownership boundaries, persistence model, runtime services,
world-delta integration, NPC integration, and diagnostic tools. It replaces the
untrusted generation core rather than rebuilding the entire navigation stack.

This is a long-term architecture step. It is not a tactical fallback and must
not be enabled as the production default until its correctness gates pass.

Provisional builder version: `voxel_span_v1`.

## Why A New Generator Is Needed

The current choices both have unacceptable long-term costs:

- Recast introduces CGO, C++ toolchain, cross-compilation, distribution, and
  runtime rebuild integration problems.
- The existing Go builder has produced incomplete navigation and visible
  polygon artifacts during manual testing.
- The existing Go implementation mixes span extraction, profile filtering,
  region merging, contour generation, fallback polygon creation, border
  stitching, and source-surface handling in one large implementation. A defect
  in later polygonization can therefore hide valid walkable space or invent
  walkable coverage.
- Current production baking and runtime rebuilding can select different builder
  paths. Long-term correctness requires the same generator for full bake and
  effective-world delta rebuilds.

The new design makes voxel-derived spans and their connectivity authoritative.
Regions, routes, portals, and movement projection are derived from those facts.
Polygon generation is not allowed to change reachability.

## What Remains Valid

The following systems are retained unless a phase below proves a concrete
contract defect:

- `.gklevel` navigation references
- `.gknav` manifests
- generated navigation sidecars and document-relative paths
- navigation builder versioning and build hashes
- static/delta tile resolution
- streamed navigation residency and revisions
- world-delta dirty tracking and async rebuild scheduling
- hierarchical sector routing
- dynamic obstacle/traversal overlays
- runtime navigation service APIs
- NPC navigation intent and movement integration
- `navbake`, `navdiag`, the navmesh lab, and actiongame debug workflows

Generated sidecars are disposable. Existing baked files do not need migration;
they can be invalidated by builder version and regenerated.

## Known, Unknown, And Required Alignment

### Known From Current Code And Docs

- Effective voxel occupancy is the canonical geometry source.
- A shared clearance-source sidecar already exists and can represent multiple
  vertical spans in one X/Z column.
- Clearance-source path queries and compact local regions already exist.
- Cross-tile portals, delta overrides, route diagnostics, and real-map fixture
  tests already exist.
- Source/BSP geometry is optional metadata. It must not replace effective voxel
  occupancy as the authority for ordinary walkability.
- Crossfire, Gasworks, and `examples/navmesh_lab` are the current manual and
  real-map verification targets.

### Missing Evidence

- The exact locations, camera captures, and route pairs for every previously
  observed incomplete area or artifact have not been recorded as fixtures.
- There is no accepted coverage comparison between effective collision space
  and generated navigation on representative full maps.
- Performance budgets for bake time, sidecar size, and runtime rebuild latency
  have not been fixed from measured baselines.
- Smooth ramp projection may need optional imported collision metadata because
  voxel occupancy alone represents a smooth ramp as steps.

### Alignment Gates

Do not make the new builder the default until these decisions are confirmed by
tests and manual review:

1. Cell/span connectivity, not polygon adjacency, is authoritative.
2. Source metadata may remove walkability or annotate traversal, but cannot add
   ordinary support where effective voxel occupancy has none.
3. Initial movement projection may use cell/region height samples instead of a
   fully simplified polygon mesh.
4. Higher polygon count is acceptable initially; correctness is preferred over
   contour simplification.
5. Old baked sidecars may be deleted and rebuilt instead of migrated.

Confidence in the architecture direction is high. Confidence in generator
correctness is intentionally medium until Phase 0 captures the observed map
failures and Phases 1-3 prove the authoritative span graph.

## Non-Goals

- Reimplement Recast line-for-line in Go.
- Preserve byte compatibility with Recast or `voxel_nav_v28` output.
- Use polygons as the canonical reachability graph.
- Add crowd simulation, tactical cover selection, or combat behavior.
- Solve flying navigation in the first implementation.
- Infer ladders, jumps, doors, water, or moving platforms before ordinary
  walking is proven correct.
- Optimize generated data before correctness and profiling justify it.
- Add a binary sidecar format before the schema and topology stabilize.

## Target Architecture

```text
effective voxel chunks + dependency halo
  -> deterministic occupancy columns
  -> profile-independent support spans
  -> clearance source tile
  -> per-profile accepted walk spans
  -> directed cell-edge graph with traversal facts
  -> compact regions and region-edge runs
  -> cross-tile portals from border cell edges
  -> hierarchical/local route queries
  -> cell/region movement projection
  -> optional debug/projection polygons
```

The pipeline has two different products:

1. **Authoritative navigation facts**: spans, accepted cells, directed edges,
   regions, and portals. These decide whether a path exists.
2. **Derived presentation geometry**: rectangles, contours, or polygons used
   for debug rendering and optional projection acceleration. Failure here must
   not create or remove a route.

## Core Design Decisions

### 1. Stay On An Integer Grid

All topology decisions use integer voxel or raster coordinates:

- tile coordinate
- local X/Y/Z support coordinate
- floor and ceiling indices
- step delta in cells
- clearance in cells
- border edge and border interval

Convert to `float32` world coordinates only at schema/API boundaries and when
sampling movement height. This avoids epsilon-driven connectivity differences
between neighboring tiles.

### 2. Preserve Multiple Vertical Spans

One X/Z column may contain floors, bridges, vents, or stacked rooms. The source
tile must preserve every valid support/air interval. No stage may collapse a
column to only its highest floor.

Each profile-independent source span needs at least:

- local X/Z column
- support Y
- open ceiling Y or explicit open-height count
- world support position
- maximum conservative horizontal clearance
- optional source normal/height refinement metadata
- area and traversal annotations
- rejection/debug flags

The existing `NavClearanceSourceCellDef` may be extended only when a phase
proves that the current fields cannot represent an invariant. Do not create a
parallel persisted source schema speculatively.

### 3. Filter Profiles After Source Extraction

Source extraction is performed once per effective voxel tile. Agent profiles
then filter spans using:

- height/headroom
- radius/clearance
- maximum slope for smooth movement
- step-up and step-down limits
- traversal capabilities

This keeps source data shared while allowing derived local tiles or cached
region views per profile.

### 4. Edges Are Explicit And Directed

Accepted neighboring cells do not imply unconditional movement. Each directed
edge records the facts required to filter or classify it:

- height delta
- available transition headroom
- minimum clearance along the transition
- traversal kind (`walk`, `step`, `stair`, later `drop`, and so on)
- capability requirements
- cost contribution

Normal walking starts with four horizontal neighbors. Diagonal movement is
deferred until corner-cut prevention and clearance rules are explicitly tested.

### 5. Regions Compress; They Do Not Repair

A region groups already-connected cells. Region creation may never connect
cells that have no authoritative edge. Regions should be split when traversal
kind or projection error exceeds a documented bound.

If region building fails, routing can fall back to the accepted cell graph for
that tile. It must not create a bounding rectangle over the cells.

### 6. Portals Come From Border Edges

Cross-tile portals are formed by intersecting compatible authoritative border
cell edges. They are not inferred from polygon bounds.

For a bidirectional transition, both sides must independently produce matching
border facts. Missing or unknown neighbor context fails closed. One-way drops
are explicit traversal links, not malformed ordinary portals.

### 7. Polygons Cannot Affect Reachability

The first correct projection output may be one quad per accepted cell or a
conservative greedy merge of cells with identical support height and traversal
classification. This preserves holes by construction.

Contour tracing, hole bridging, triangulation, and convex decomposition are
deferred until a measured polygon-count or rendering problem requires them.
No fallback may fill a failed contour with its bounding rectangle.

### 8. Full Bake And Delta Rebuild Share One Function

Both workflows must call the same pure generation entry point with:

- effective center chunk
- explicit dependency halo
- builder settings
- source modifiers
- requested profile set

The only difference is where the returned sidecars are saved. Given identical
effective inputs, a full bake and delta rebuild must produce identical
authoritative output and hashes.

### 9. Unknown Context Fails Closed

The input distinguishes:

- known occupied
- known empty
- outside authored world and therefore known empty
- unknown/unloaded

Unknown cells must not be treated as empty. A build requiring unknown halo data
returns a diagnostic or conservatively rejects affected spans.

### 10. Every Stage Is Inspectable

`navdiag` must be able to dump:

- occupancy columns
- extracted spans
- span rejection reasons
- accepted profile cells
- directed edges
- regions
- intra-tile region boundaries
- border edges and portals
- optional projection geometry

The dump must identify the first stage where expected coverage disappears.

## Non-Negotiable Invariants

These invariants apply before performance or polygon quality:

### Source Invariants

- Same input and builder version produce byte-stable authoritative output.
- Every emitted span has solid support and a known open interval above it.
- Every valid vertical span in a column is preserved.
- No emitted coordinate lies outside its tile.
- Unknown dependency data never becomes walkable.

### Profile Invariants

- An accepted cell has enough headroom and radius for the profile.
- A larger/taller profile cannot gain cells that a compatible smaller/shorter
  profile lacks solely because of clearance filtering.
- Blocker hints may remove cells but cannot create them.
- Step, slope, and capability checks are applied identically in bake and query
  paths.

### Connectivity Invariants

- Every graph edge connects real accepted cells.
- Every ordinary edge satisfies the profile's transition constraints.
- Region connectivity is a compression of cell connectivity.
- Removing regions and routing on cells produces the same reachable components.
- Every portal is backed by compatible border edges.
- Every bidirectional portal is reciprocal.
- Holes and blocked spans remain unreachable.

### Projection Invariants

- Projection geometry covers no rejected or blocked cell.
- Every accepted cell has a movement-height sample even if it has no polygon.
- Projection failure does not alter graph connectivity.
- Debug geometry clearly distinguishes authoritative cells from optional
  projection polygons.

### Delta Invariants

- Rebuilding an unchanged effective chunk reproduces the static result.
- A removed floor removes its source span, graph node, region membership, and
  affected portals.
- An added blocker cannot leave a stale static route.
- Empty delta results explicitly suppress stale static tiles.
- Dependency hashes include every halo chunk that can affect the result.

## Implementation Rules For AI Agents

Each agent task must implement exactly one numbered phase or one explicitly
named subtask from a phase.

Before editing, the agent must:

1. Read this document.
2. Read the listed owner files for that phase.
3. State the invariant being added or protected.
4. State whether the change is schema, generation, runtime, tooling, or test
   work.
5. Name the exact verification command and any manual check.

During implementation:

- Do not change the production default before Phase 8.
- Do not delete old builders before Phase 10.
- Do not add future traversal types to an earlier phase.
- Do not fix a failed invariant with a permissive fallback.
- Do not copy logic from `nav_build.go` without a test proving that logic is
  correct.
- Prefer new focused `nav_gen_*.go` files in the existing `content` package over
  another package or interface hierarchy.
- Keep a single concrete implementation. Add an interface only if a second
  runtime implementation actually exists.
- Convert every reproduced artifact into a deterministic fixture.
- Update the phase checklist only after its gate passes.

After implementation, the agent must report:

- files changed
- invariant proven
- tests run and exact result
- manual checks performed or still required
- deferred work and why it is outside the current phase

## Phase 0: Capture Failures And Freeze Contracts

Purpose: make the reported incompleteness and artifacts reproducible before
writing another generator.

### Tasks

- [ ] Record each known Crossfire/Gasworks failure as a named start/end route
  pair, expected reachable area, or blocked area.
- [ ] Save screenshots or debug dumps for visually incomplete/artifacted areas.
- [ ] Add synthetic fixtures for:
  - flat field
  - hole in a floor
  - isolated island
  - concave walkable area
  - stacked floors in one X/Z column
  - stairs within step height
  - ledge outside step height
  - narrow passage for two agent radii
  - four tile seams with partial border coverage
  - unknown neighbor halo
  - removed-floor and added-blocker delta edits
- [ ] Add fixture assertions against collision/occupancy facts, not existing
  polygon output.
- [ ] Record current bake time, source-cell count, tile count, sidecar size, and
  route results for Crossfire, Gasworks, and navmesh lab.
- [ ] Confirm which old sidecars may be deleted and regenerated.

### Likely Files

- `content/nav_build_test.go`
- `content/nav_source_build_test.go`
- `content/nav_real_maps_test.go`
- `content/nav_delta_test.go`
- `examples/navmesh_lab/main_test.go`
- `cmd/navdiag/main.go` only if an existing dump cannot capture the failure

### Gate

Do not begin topology implementation until at least one previously observed
failure is reproduced by a deterministic fixture or precisely documented
manual checkpoint.

## Phase 1: New Profile-Independent Span Extractor

Purpose: produce trustworthy support/open-space facts directly from effective
voxel occupancy.

### Tasks

- [ ] Add provisional `voxel_span_v1` builder selection without changing the
  default.
- [ ] Define a small in-memory build input containing the center chunk and
  explicit known/unknown halo state.
- [ ] Build sorted occupancy columns using integer coordinates.
- [ ] Extract every solid-to-empty support transition and its open ceiling.
- [ ] Preserve multiple vertical spans per X/Z column.
- [ ] Emit deterministic source cells in stable X/Z/Y order.
- [ ] Record rejected candidates with reason codes.
- [ ] Convert to existing clearance-source schema at the outer boundary.
- [ ] Add a debug dump for columns and raw spans.

### New Focused Files

- `content/nav_gen_span.go`
- `content/nav_gen_span_test.go`

### Existing Integration Files

- `content/nav.go`
- `content/nav_build.go`
- `content/nav_heightfield_debug.go`

### Gate

- Every synthetic occupancy fixture produces the expected exact span set.
- Stacked-floor fixtures preserve all floors.
- Randomized small voxel grids match a slow test-only reference extractor.
- Output is deterministic across repeated builds and map iteration order.

## Phase 2: Clearance And Profile Filtering

Purpose: decide where a particular agent can stand without constructing
regions or polygons.

### Tasks

- [ ] Compute headroom exactly from source floor/ceiling intervals.
- [ ] Implement a slow, obviously correct radius-clearance oracle for tests.
- [ ] Implement the production clearance calculation.
- [ ] Compare production results to the oracle on deterministic randomized
  fixtures.
- [ ] Filter cells by height, radius, and supported traversal capability.
- [ ] Apply clearance-blocker hints as subtraction only.
- [ ] Record rejection reasons: headroom, radius, blocker, slope/capability,
  and unknown context.
- [ ] Prove monotonicity across small and large profiles.

### New Focused Files

- `content/nav_gen_clearance.go`
- `content/nav_gen_clearance_test.go`

### Gate

- No accepted cell intersects occupied/blocked volume for the profile capsule or
  conservative cylinder model selected by the existing profile contract.
- Small/large profile fixtures and randomized oracle comparisons pass.
- No regions, contours, or polygons are introduced in this phase.

## Phase 3: Authoritative Directed Cell Graph

Purpose: establish correct local reachability before any compression.

### Tasks

- [ ] Give accepted cells stable coordinate-derived IDs.
- [ ] Create four-neighbor directed candidate edges.
- [ ] Validate step-up, step-down, transition headroom, and transition
  clearance.
- [ ] Classify ordinary edges as walk/step/stair using explicit rules.
- [ ] Keep drops and jumps absent until a later traversal-link phase.
- [ ] Implement cell-graph reachability and shortest path for tests.
- [ ] Dump cells and directed edges through `navdiag`.
- [ ] Add connectivity invariants comparing expected fixture components.

### New Focused Files

- `content/nav_gen_graph.go`
- `content/nav_gen_graph_test.go`

### Gate

- All Phase 0 reachability fixtures pass on the cell graph.
- Holes, ledges, blocked spans, and unknown borders remain disconnected.
- Cross-tile behavior is not implemented here; tile-border edges are only
  recorded as candidates.

## Phase 4: Compact Regions Without Contours

Purpose: compress the correct cell graph while preserving exactly the same
reachable components.

### Tasks

- [ ] Group cells only across existing compatible graph edges.
- [ ] Split regions when traversal class changes.
- [ ] Split regions when a single bounded-error height/projection model is not
  valid.
- [ ] Generate explicit contiguous edge runs between neighboring regions.
- [ ] Retain member-cell references for validation and local repair.
- [ ] Compare region-graph connected components with cell-graph components.
- [ ] Route across regions, then locally refine through cells around the active
  corridor.
- [ ] Do not create contours or polygons.

### New Focused Files

- `content/nav_gen_region.go`
- `content/nav_gen_region_test.go`

### Existing Reference Files

- `content/nav_clearance_region.go`
- `content/nav_clearance_region_test.go`

Existing region code is a behavior reference, not an implementation that must
be copied. In particular, a single connected component must not receive one
height plane when its projection error is unbounded.

### Gate

- Cell and region graphs report identical reachable components.
- Flat open fixtures collapse substantially without filling holes.
- Multilevel fixtures remain separate.
- Region projection error stays within the documented bound.

## Phase 5: Deterministic Cross-Tile Portals

Purpose: connect tiles using authoritative border facts, independent of polygon
shape.

### Tasks

- [ ] Assign world-grid keys to candidate border cell edges.
- [ ] Match opposing edges from adjacent tiles.
- [ ] Validate height delta, headroom, clearance, and direction on both sides.
- [ ] Merge only contiguous compatible matches into portal runs.
- [ ] Emit reciprocal portals for bidirectional movement.
- [ ] Map portal runs to region IDs and retain backing cell-edge references in
  debug output.
- [ ] Test different region partitioning on each side of the same seam.
- [ ] Test static/delta and known-empty/unknown neighbor combinations.

### New Focused Files

- `content/nav_gen_portal.go`
- `content/nav_gen_portal_test.go`

### Existing Integration Files

- `content/nav_portal.go`
- `content/nav_validation.go`

### Gate

- All four seam directions pass reciprocal topology validation.
- Portal existence is unchanged when optional projection geometry changes.
- Missing or unknown neighbor context never creates a portal.

## Phase 6: Runtime Queries Use The Authoritative Graph

Purpose: make pathfinding and movement work without depending on polygon
generation.

### Tasks

- [ ] Resolve start/end points to accepted source cells.
- [ ] Use region/portal graph routing as the normal local path.
- [ ] Use cell routing only for local refinement, repair, and diagnostics.
- [ ] Return a corridor containing region IDs, portals, traversal facts, and
  source-cell anchors.
- [ ] Project movement height from local source cells/region samples.
- [ ] Remove polygon adjacency from the correctness-critical route path.
- [ ] Keep legacy polygon queries only for old sidecars during the transition.
- [ ] Preserve current failure reasons or replace them with a documented mapping.

### Existing Files

- `content/nav_clearance_query.go`
- `content/nav_clearance_region.go`
- `content/nav_coarse_graph.go`
- `content/nav_path.go`
- `content/nav_route.go`
- runtime navigation service files in `gekko/`
- actiongame NPC navigation consumers

### Gate

- Every Phase 0 route fixture succeeds or fails for the expected occupancy
  reason with projection polygons disabled.
- Movement remains on valid support through stairs, seams, and multilevel
  fixtures.
- Long routes use regions/coarse graph rather than expanding the full cell map.

## Phase 7: Minimal Projection And Debug Geometry

Purpose: restore useful filled debug surfaces and optional projection
acceleration without allowing geometry simplification to alter navigation.

### Tasks

- [ ] Start with one exact quad per accepted cell.
- [ ] Greedily merge only cells with compatible support height, height model,
  region, and traversal class.
- [ ] Validate merged rectangles against the member-cell set before emission.
- [ ] Preserve holes and multilevel surfaces by construction.
- [ ] Render authoritative cells/edges separately from merged projection quads.
- [ ] Measure polygon count before considering contour tracing.
- [ ] Do not implement arbitrary contour simplification unless measured output
  size or rendering cost requires it.

### New Focused Files

- `content/nav_gen_projection.go`
- `content/nav_gen_projection_test.go`

### Gate

- Projection coverage is an exact subset/equivalent representation of accepted
  cells within the documented height tolerance.
- Deleting all generated polygons does not change route results.
- Previously observed visual artifacts are absent in navmesh lab, Crossfire,
  and Gasworks.

## Phase 8: Bake Integration And Opt-In End-To-End Path

Purpose: exercise the new generator through real save/load workflows while old
builders remain available only as comparison baselines.

### Tasks

- [ ] Make `navbake -builder-version voxel_span_v1` run the full new pipeline.
- [ ] Save shared clearance source and derived profile tiles using existing
  sidecar ownership unless a proven schema gap requires a version bump.
- [ ] Validate every tile before saving; do not save partial invalid output.
- [ ] Make `navdiag` identify builder version and all new stage diagnostics.
- [ ] Bake navmesh lab, Crossfire, and Gasworks explicitly with
  `voxel_span_v1`.
- [ ] Compare coverage, routes, topology issues, bake time, and sidecar size to
  the Phase 0 baseline.
- [ ] Keep imports on the old default during this phase.

### Existing Files

- `content/nav_bake.go`
- `content/nav_io.go`
- `content/nav_validation.go`
- `cmd/navbake/main.go`
- `cmd/navdiag/main.go`
- `examples/navmesh_lab/main.go`

### Gate

- Full save/load round trips preserve authoritative topology.
- `navdiag` reports zero hard topology errors for all acceptance maps.
- Named real-map routes pass against freshly baked `voxel_span_v1` data.
- Manual debug review accepts coverage before the production default changes.

## Phase 9: Delta Rebuild Parity

Purpose: prove runtime edits use exactly the same generation semantics as full
bakes.

### Tasks

- [ ] Route full bake and delta rebuild through one generation entry point.
- [ ] Load the complete required effective-world halo for rebuild jobs.
- [ ] Include every dependency in build hashes.
- [ ] Produce explicit empty overrides when edits remove all navigation.
- [ ] Rebuild neighboring portal products affected by border changes.
- [ ] Compare full rebake and delta rebuild output for the same effective world.
- [ ] Verify async revision swap keeps the old route only until valid
  replacement data is committed.

### Existing Files

- `content/nav_delta.go`
- `content/nav_bake.go`
- streamed-level runtime navigation rebuild files and tests
- world-delta persistence files and tests

### Gate

- Full rebake and delta rebuild have equal authoritative hashes for identical
  effective inputs.
- Removed floor, restored floor, added blocker, and seam-edit fixtures pass
  through save, reload, lookup, and path query.
- No stale static tile or portal is reachable after a matching delta edit.

## Phase 10: Production Cutover And Deletion

Purpose: make the pure-Go generator the only production generator.

### Tasks

- [ ] Change `DefaultNavBakeBuilderVersion` to `voxel_span_v1`.
- [ ] Update importer defaults and generated fixture defaults.
- [ ] Rebuild committed/generated acceptance assets as needed.
- [ ] Remove Recast dispatch and debug APIs.
- [ ] Delete `content/recastnav`.
- [ ] Delete `third_party/recastnavigation`.
- [ ] Delete `nav_build_recast.go` and its tests.
- [ ] Remove `voxel_nav_v28` generation code after confirming there are no
  remaining callers or unique accepted fixtures.
- [ ] Keep only schema compatibility needed to recognize stale builder versions;
  do not keep dead generation implementations.
- [ ] Update navigation and HL1 import documentation.

### Gate

- `CGO_ENABLED=0 go test ./content/...` passes.
- Engine and actiongame verification passes with freshly generated nav.
- Crossfire, Gasworks, and navmesh lab pass manual coverage and route checks.
- Repository search finds no C++, CGO, Recast, or legacy-builder production
  dependency in navigation generation.

## Phase 11: Performance Work Only After Correctness

Purpose: optimize measured bottlenecks without weakening invariants.

Potential work, only when profiling selects it:

- bitset occupancy/clearance acceleration
- cached column extraction shared across profiles
- parallel tile generation with deterministic output ordering
- compact cell/edge serialization
- binary sidecars
- bounded contour/polygon simplification for debug rendering
- incremental region rebuild inside a tile

Every optimization must compare its output to the accepted reference pipeline
on synthetic and real-map fixtures.

## Verification Matrix

### Per-Phase Automated Checks

Run the narrow tests added by the phase first, followed by:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./content/...
```

For runtime integration phases:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test . ./content/...

cd /Users/ddevidch/code/go/gekko3d/actiongame
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

After CGO removal:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env CGO_ENABLED=0 GOCACHE=/tmp/gekko3d-gocache-nocgo go test ./content/...
```

### Bake And Diagnostic Checks

Use explicit new-builder selection until Phase 10:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
go run ./cmd/navbake \
  -world ../actiongame/assets/levels/gasworks/worlds/gasworks.gkworld \
  -nav /tmp/gasworks-voxel-span.gknav \
  -builder-version voxel_span_v1 \
  -progress

go run ./cmd/navdiag \
  -nav /tmp/gasworks-voxel-span.gknav \
  -fail-on-error=true
```

Repeat for Crossfire and navmesh lab using their generated world/manifest
paths. Record timings and topology summaries instead of relying on subjective
impressions alone.

### Required Manual Visual/GPU Checks

Automated tests cannot prove that navigation coverage looks complete or that
movement projection is visually stable. Before Phases 8 and 10 pass:

- Run `examples/navmesh_lab` and inspect every fixture class.
- Run actiongame on Crossfire and Gasworks with navigation debug enabled.
- Inspect floors, holes, stairs, ramps, narrow passages, multilevel areas, and
  all visible chunk seams.
- Confirm debug cells show authoritative coverage independently of polygons.
- Command selected NPC routes through each recorded failure location.
- Confirm the NPC remains on the collision surface without snapping through a
  floor, floating, or oscillating at region/tile boundaries.
- Apply a runtime voxel edit, wait for revision swap, and confirm debug coverage
  and routing update without stale geometry.

Record the map, coordinates, builder version, and outcome for every manual
failure. A screenshot without coordinates and builder version is not a reusable
regression artifact.

## Suggested File Ownership

Keep the new algorithm in focused files under the existing `content` package:

```text
content/nav_gen_span.go          occupancy columns and source spans
content/nav_gen_clearance.go     clearance and profile filtering
content/nav_gen_graph.go         accepted-cell directed edges
content/nav_gen_region.go        compact regions and region edge runs
content/nav_gen_portal.go        cross-tile portal matching
content/nav_gen_projection.go    optional exact projection geometry
```

Avoid placing new implementation in the existing large `nav_build.go`. That
file remains the adapter/legacy owner during migration and should shrink during
Phase 10.

Schema and I/O stay in their current owners:

- `content/nav.go`
- `content/nav_source.go`
- `content/nav_io.go`
- `content/nav_source_io.go`
- `content/nav_validation.go`

Runtime consumers stay in their current owners unless an actual contract gap is
proven.

## Reusable AI-Agent Prompt

Use this prompt with one phase or subtask at a time:

```text
Implement only Phase <N>, subtask <name>, from
gekko/docs/roadmaps/pure-go-voxel-navigation-generation-plan.md.

Before editing:
- read the entire plan and the phase's listed files
- state the invariant this subtask protects
- state known facts, remaining uncertainty, files to touch, and verification
- stop for alignment if the phase gate or representation is ambiguous

Implementation constraints:
- pure Go; no CGO or new dependency
- effective voxel occupancy remains canonical
- do not change the production builder default before Phase 10
- do not let polygons affect connectivity
- do not implement later phases
- add the smallest deterministic test that fails before the change

After editing:
- run the phase-specific test and the smallest owning-package test
- report exact commands/results and manual checks still required
- update the plan checkbox only when the phase gate is actually satisfied
```

## Final Completion Criteria

The replacement is complete only when:

- one pure-Go generator serves static bake and delta rebuild
- source spans and graph connectivity are authoritative
- holes, multilevel floors, agent clearance, and tile seams pass deterministic
  fixtures
- named Crossfire/Gasworks failures are fixed and retained as regressions
- pathfinding works with projection polygons disabled
- optional polygons cannot add or remove reachability
- full and delta builds agree for identical effective worlds
- `CGO_ENABLED=0` content tests pass
- Recast and the old custom generator implementations are removed
- manual nav overlays and NPC routes are accepted on navmesh lab, Crossfire,
  and Gasworks

Anything less remains an opt-in experimental builder, not the production
navigation generator.
