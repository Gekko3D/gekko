# Dynamic Destructible Navigation Rework Plan

## Status

This document is the authoritative long-term plan for reworking Gekko's voxel
navigation storage, runtime indexes, dynamic overlays, and route queries after
the pure-Go graph replacement.

The rework is intentionally breaking:

- no backward-compatible sidecar schemas
- no legacy JSON tile loader
- no migration command
- no dual-read or dual-write period
- no compatibility adapters around old query types
- no preservation of generated navigation bundles

All existing navigation bundles are deleted and rebaked. The small navigation
manifest may remain JSON, but source and graph tiles become new compact binary
formats.

This is a long-term architecture step, not a tactical bridge.

Related architecture:

- [Pure-Go Voxel Navigation Graph Replacement Plan](pure-go-voxel-navigation-generation-plan.md)
- [Streaming and Worlds](../content/streaming-and-worlds.md)

## Confidence

- Confidence: **High**
- Primary owner: engine content and streamed navigation runtime
- Affected consumers:
  - Actiongame NPC navigation and tactical queries
  - streamed voxel destruction and world deltas
  - navigation bake and diagnostic tools
  - editor or command-line workflows that generate navigation bundles
- Key assumptions:
  - effective voxel occupancy remains authoritative for ordinary support and obstruction
  - every shipped level can rebake its navigation bundle
  - runtime navigation tiles are immutable after publication
  - dynamic edits are localized by chunk plus a deterministic dependency halo
- Remaining measurement gaps:
  - route latency distribution with live Crossfire carriers
  - peak resident navigation heap after query construction
  - single-tile rebuild latency on target gameplay hardware
- SME alignment required: **No for implementation direction; yes for final gameplay budgets**

## Executive Decision

Build one immutable, revisioned navigation snapshot from compact tile data.
Represent dense tile-local topology with arrays and CSR adjacency, not maps of
verbose transition objects. Resolve temporary blockers by splitting only the
baked regions they affect. Rebuild destructed voxel tiles asynchronously and
publish the complete dependency batch atomically.

```text
effective voxel occupancy
  -> compact integer surface spans
  -> profile acceptance bitset + packed local adjacency
  -> region graph + explicit exceptional traversals
  -> immutable resident snapshot
  -> blocker-aware region components
  -> hierarchical route + local span refinement
  -> physics-driven locomotion
```

Runtime destruction uses a conservative two-stage update:

```text
voxel edit
  -> install immediate temporary blocker
  -> snapshot affected voxel chunks
  -> rebuild source/profile tiles and boundary dependencies off-thread
  -> validate the complete replacement batch
  -> atomically publish one new navigation revision
  -> remove the temporary blocker covered by that committed edit
  -> replan only routes whose tile dependencies changed
```

## Why Rework The Current Graph

The current graph is functionally capable but its detailed topology is stored
and indexed too literally.

Crossfire's current generated bundle is approximately:

- `1.2 GB` total
- `950 MB` graph tiles
- `254 MB` source tiles
- `782,760` accepted spans across the 17 dense graph tiles
- `3,086,292` span transitions across those tiles
- `685 MB` of graph JSON in the dense center `3 x 3` tile footprint alone

One representative graph tile is approximately `127 MB` as indented JSON. The
same document is approximately `66 MB` as compact JSON and `3.4 MB` through
gzip. Whitespace is therefore an immediate cost, but JSON object repetition and
the in-memory object graph remain the larger long-term problems.

The current runtime also has a destructive-environment scaling cliff:

- ordinary routes use tile, region, then local span search
- any blocker that overlaps a span enables a global resident-span graph
- all subsequent routes use global span A*
- carrier swept footprints are blockers
- Crossfire contains four carriers

The result is that a feature required by the shooter can disable the hierarchy
that makes dense voxel navigation affordable.

## Goals

1. Keep navigation correct while voxel support and obstruction change at runtime.
2. Bound disk, decode, resident-memory, query-build, and route costs.
3. Make temporary blockers and carriers first-class hierarchical overlays.
4. Rebuild only edited tiles and their true dependency halo.
5. Publish coherent tile batches without mixed old/new seam transitions.
6. Avoid global replan storms after a localized edit.
7. Preserve deterministic generation, validation, diagnostics, and replay behavior.
8. Keep route solving off the frame-critical path.

## Non-Goals

- polygon navigation or Recast
- crowd simulation
- using other NPCs as graph blockers
- strict preservation of old sidecars or Go APIs
- global flow fields for tactical bots
- globally minimal waypoint count
- retaining a path through unloaded navigation tiles
- mutating a published graph in place
- making distant proxy terrain destructible or navigable at full fidelity

## Non-Negotiable Invariants

### Geometry Authority

- Effective voxel occupancy is authoritative for ordinary support and obstruction.
- Source metadata may classify or remove traversal but may not invent unsupported floor.
- Stacked floors in one X/Z column remain distinct spans.
- Topology keys use integer voxel coordinates, not float equality.

### Dynamic Safety

- A destructive edit becomes conservatively non-traversable no later than the next navigation update.
- Removed obstruction may remain conservatively closed until its rebuild commits.
- Removed support may never remain routable while the rebuild is pending.
- A route never combines graph tiles or seam edges from incompatible build batches.

### Publication

- Source tiles, profile graph tiles, boundary edges, blocker overlays, and tile epochs publish as one immutable snapshot.
- Background work may be abandoned when stale; stale work may never publish.
- Readers hold a snapshot pointer and do not lock individual graph operations.
- Published slices, bitsets, and maps are never mutated.

### Routing

- Route reachability is exact for the active resident graph and overlay.
- Route costs are deterministic and strictly positive.
- Normal routing never expands the entire resident span graph.
- Production correctness does not depend on a global span-A* fallback.
- Physics remains authoritative for executing movement and traversal actions.

## Deliberate Optimality Policy

The target is deterministic, valid, low-cost shooter routes—not proof of the
shortest path across every resident span.

- Region/component A* chooses the coarse route.
- A route state represents a concrete entry portal, not merely a region center.
- Local A* refines between selected portals using the active span bitsets.
- The heuristic is scaled from the minimum legal edge cost per distance and must remain admissible.
- Greedy visibility string-pulling may simplify walk-only waypoints.
- Tactical costs may include expected traversal time and authored penalties.

Strict whole-graph shortest paths are rejected because they turn every blocker,
carrier, and edit into a dense global search problem. If route quality later
fails a named gameplay case, improve portal selection or bounded corridor
widening for that case; do not make global span search the default.

## Target Persistent Format

### File Set

- `.gknav`: compact JSON manifest; small, inspectable, and diffable
- `.gkns`: gzip-compressed binary profile-independent source tile
- `.gkng`: gzip-compressed binary profile graph tile

Old `.gknavsource` and `.gknavgraph` files are unsupported and deleted.

Each binary file contains:

- fixed magic and file-kind bytes
- schema version
- little-endian marker
- builder version/hash references
- chunk coordinate and metrics
- uncompressed payload length
- deterministic ordered sections
- gzip checksum validation

Use only Go standard-library encoding and compression. One tile is the unit of
I/O, validation, decompression, and replacement; random access inside a tile is
not required.

### Source Tile Layout

Source spans have contiguous tile-local IDs and store integer topology:

- local X/Z column
- integer support and ceiling coordinates
- clearance in voxel units
- area/flag class index

World-space support heights are derived from integer coordinates and voxel
resolution. String tables store repeated area and flag values once per tile.
Solid and blocker occupancy remains run-length encoded.

The persisted source must contain enough occupancy context to rebuild every
profile graph without reopening the original BSP, VOX, or imported-world source.

### Profile Graph Layout

Store:

- accepted-span bitset indexed by source span ID
- CSR offsets for ordinary tile-local adjacency
- packed ordinary edges containing target span ID and movement class
- deterministic region membership runs
- region bounds and compact portal transitions
- explicit cross-tile edges
- explicit jumps, drops, ladders, doors, carriers, gates, and authored links

Do not serialize a full `NavSpanTransitionDef` for ordinary four-neighbor
movement. Derive ordinary width, clearance, step delta, and geometric cost from
the source spans and profile. Any edge with exceptional cost, flags, gate,
traversal payload, or cross-tile target remains explicit.

### Determinism

- Sections are written in fixed order.
- Spans, edges, regions, and transitions use deterministic sorting.
- Repeated generation from identical effective input produces identical bytes.
- The manifest stores the exact content hash and byte size of every sidecar.
- Load rejects corruption, wrong kind, wrong version, incompatible metrics, and hash mismatch.

## Target Resident Representation

### Tile Index

Use one coordinate map to find resident tiles. Inside a tile, use dense slices:

- `[]NavSpan`
- accepted and blocked bitsets
- `[]uint32` CSR offsets
- packed `[]NavLocalEdge`
- `[]uint32` span-to-region membership with an unassigned sentinel
- dense column offsets plus stacked span IDs
- `[]NavRegion`
- CSR region and overlay-component edges

Span and region IDs are contiguous by contract. Nested `map[uint32]...` indexes
are removed.

### Snapshot

The runtime owns one immutable `NavSnapshot` containing:

- global publication revision
- resident tiles by coordinate and profile
- per-tile topology epoch
- current overlay component graph
- traversal/gate state already applied to affected tiles
- source and manifest metadata needed by queries

Publishing replaces the snapshot pointer. Old snapshots remain reachable only
by in-flight jobs and are reclaimed normally after those jobs complete.

### Route Dependencies

Every route records the tile epochs it depends on. A global publication
revision remains useful for tracing, but it does not invalidate unrelated
routes.

After a publication:

- unchanged dependency epochs: route remains valid
- changed dependency epoch: route enters the replan queue
- removed/unloaded dependency tile: route fails or replans immediately
- live support overlaps a new blocker: locomotion stops before replan

This prevents a wall destroyed on one side of the map from restarting every bot.

## Blocker-Aware Hierarchy

### Overlay Construction

For each profile:

1. Convert blocker AABBs directly to overlapping tile and column ranges.
2. Mark overlapping accepted spans in a blocked bitset.
3. Identify baked regions containing newly blocked or unblocked spans.
4. Flood-fill only those affected regions through active CSR edges.
5. Represent each resulting connected component as an overlay node.
6. Reconnect active local, boundary, gate, and traversal edges to those nodes.
7. Reuse one unchanged overlay node for every unaffected baked region.

Cost is proportional to blocker overlap plus affected-region size, not all
resident spans times all blockers.

### Route Query

1. Locate start and goal through the dense tile column index.
2. Resolve their active region/component nodes.
3. Run A* over resident region/component portals.
4. Refine each selected region segment through active local CSR edges.
5. Emit explicit traversal steps and simplified walk waypoints.

The same overlay handles:

- carrier swept footprints
- live breakables
- temporary destruction blockers
- runtime navigation blocker entities
- doors or gates that truly remove span connectivity

Door cost/state changes that do not block floor update only their explicit
transition edges and affected tile epochs.

### Global Span Search

Remove global resident-span A* from the production query path. Keep a slow
Dijkstra implementation only in tests and diagnostics as a correctness oracle
for small generated graphs.

## Destruction And Rebuild Transactions

### Dirty Bounds

Every voxel mutation reports:

- world-space edit AABB
- owning world and chunk
- edit generation
- whether voxels were added, removed, or replaced

Navigation expands dirty bounds by the maximum relevant profile radius, height,
step, jump/drop discovery range, and boundary halo. Expansion returns an exact
set of source and profile tiles to rebuild.

### Immediate Conservative Overlay

Before background baking starts:

- added voxels block their expanded footprint
- removed floor blocks the affected former support
- removed wall remains closed until rebuilt
- mixed edits block the union

The temporary blocker carries the edit generation. It is removed only by a
published rebuild batch that includes that generation or a later one.

### Background Batch

The rebuild job:

1. captures immutable effective voxel snapshots for every dependency tile
2. rebuilds source spans for directly dirty tiles
3. rebuilds profile graphs for dirty and dependency tiles
4. reconnects every internal and external boundary in the batch
5. validates topology, hashes, and complete seam reciprocity
6. writes replacement delta sidecars to temporary paths
7. returns one publishable batch tagged with runtime and edit generations

Multiple edits coalesce before work begins. Edits arriving during a build
advance the requested generation; stale results are discarded and never
published.

### Atomic Commit

Commit under one short runtime critical section:

- verify runtime and edit generations
- install every source and graph tile from the batch
- rebuild only affected resident indexes and overlay nodes
- increment affected tile epochs
- assign one global publication revision
- swap the snapshot pointer
- commit delta manifest references
- retire covered temporary blockers

Disk persistence must use write-temp, sync/close, and rename. A crash may leave
unused temporary files but may not leave a manifest pointing at a partial batch.

## Route Scheduling And Locomotion

### Scheduling

- Route requests stay asynchronous.
- One worker remains the starting implementation.
- Requests are prioritized: current-path invalidation, new combat route, tactical probe.
- Duplicate pending requests for one actor collapse to the newest target/revision.
- Completed stale requests are dropped by request ID and dependency epochs.
- Add workers only when queue latency exceeds the measured budget.

### Replan Policy

- Hard-invalid current support or segment: stop and enqueue immediately.
- Changed route dependency: enqueue under the per-frame replan budget.
- Unchanged dependencies: keep following the current route.
- Moving target below refresh distance: keep the route.
- Repeated failure uses the existing bounded repair/quarantine policy.

### Local Avoidance

Other actors, physics debris, and short-lived projectiles remain local steering
or collision concerns. They do not rebuild the navigation graph and do not
become persistent blockers unless gameplay explicitly marks them as such.

## Tactical Queries

`ReachableSpans` and tactical sampling use the same active component graph as
route queries. They must not launch one route per candidate.

- resolve the actor's overlay component once
- traverse component reachability once
- classify all candidate spans from that traversal
- reuse profile/tile sampling caches until a depended-on tile epoch changes

This keeps cover, flank, retreat, and pickup selection compatible with dynamic
destruction without multiplying pathfinding work.

## Observability And Budgets

Add counters and timings before replacing behavior:

- sidecar compressed and uncompressed bytes by tile
- tile read, decompress, decode, validate, and index-build duration
- resident source, graph, index, and overlay bytes
- blocker count, blocked spans, affected regions, and overlay components
- route queue depth and wait time
- sector/region/component/local span expansions per route
- route solve p50/p95/p99 and failure reason
- destruction-to-blocker latency
- rebuild queue, coalesced edits, stale jobs, build duration, and commit duration
- routes retained versus replanned per publication
- diagnostic global-oracle mismatches

Initial Crossfire acceptance budgets on the agreed reference machine:

- complete navigation bundle: at most `150 MB`
- dense center resident navigation heap: at most `256 MB`
- main-thread navigation commit: below `0.5 ms` p95
- ordinary route solve: below `2 ms` p95
- blocker/carrier route solve: below `5 ms` p95
- immediate blocker installation: within one navigation update
- localized edit must not replan actors whose route dependencies are unchanged
- production global span-search count: exactly zero

Treat these as gates, not constants embedded in gameplay code. Record hardware
and map state with every benchmark result.

## Implementation Phases

### Phase 0: Baseline And Correctness Gate

- [x] Fix the current `TestCompressNavGraphRegions/required_action_splits_regions` fixture so the content suite is green.
- [x] Add synthetic benchmarks for dense flat, stacked, multi-region, carrier-blocked, and locally destroyed graphs.
- [x] Record Crossfire bundle size, load/decode/index heap, route latency, rebuild latency, and replan counts.
- [x] Add the slow small-graph Dijkstra oracle.

Gate: all current navigation correctness tests pass and the baseline report is reproducible.

### Phase 1: Replace Persistent Sidecars

- [x] Define the `.gkns` and `.gkng` binary contracts and hard version checks.
- [x] Implement deterministic streaming writers and readers with `encoding/binary` and `compress/gzip`.
- [x] Store source spans as integer records with per-tile string tables.
- [x] Store profile acceptance bitsets, CSR local edges, regions, and explicit exceptional transitions.
- [x] Change bake, delta, diagnostics, and manifest validation to the new files.
- [x] Delete JSON source/graph tile readers, writers, extensions, and tests.
- [x] Delete every generated old navigation bundle.

Gate: deterministic byte round-trips, corruption rejection, and Crossfire bundle size at or below budget.

Result: Crossfire rebaked deterministically to `30 MB` (`5,778,340` source
bytes, `22,844,964` graph bytes, and a `264,603` byte manifest), below the
`150 MB` gate.

### Phase 2: Replace Resident Query Indexes

- [x] Introduce immutable resident tile and snapshot types.
- [x] Replace nested span/region maps with dense slices, bitsets, dense column offsets, and CSR adjacency.
- [x] Build world/tile lookup once per published snapshot.
- [x] Move ordinary edge properties to deterministic derivation from spans and profile.
- [x] Remove query-time reconstruction of per-region visibility maps.
- [x] Publish snapshots by pointer swap.

Gate: route results match the small-graph oracle and dense-center resident heap meets budget.

Result: the Crossfire baseline snapshot retains `217,517,928` bytes on the
reference Apple M4 Pro, below the `256 MB` gate. Its resident index builds in
`1,224.642 ms`; ordinary route reachability matches the independent small-graph
Dijkstra oracle. Blocker and carrier overlay storage remains Phase 3 scope.

### Phase 3: Replace Blocker Routing

- [x] Build blocker overlap from tile/column ranges.
- [x] Split only affected baked regions into overlay components.
- [x] Route over baseline regions plus overlay components.
- [x] Refine locally through active CSR edges.
- [x] Move carriers, breakables, and runtime blockers to the shared overlay.
- [x] Delete production global resident-span A* and its global edge index.
- [x] Move active-degree and reachability queries to overlay components.

Gate: blocker/carrier routes match the oracle on generated cases and meet the route budget on Crossfire.

Result: generated blocker routes match the independent Dijkstra oracle. A
Crossfire `lift2` carrier route measures `0.018667 ms` p50, `0.027084 ms` p95,
and `0.048042 ms` p99 on the reference Apple M4 Pro, below the `5 ms` gate.
The carrier-overlay snapshot retains `225,413,800` bytes, below the `256 MB`
resident budget.

### Phase 4: Transactional Destruction Rebuilds

- [x] Define edit generations and profile-aware dirty-bound expansion.
- [x] Install conservative temporary blockers before scheduling work.
- [x] Coalesce edits and snapshot complete dependency batches.
- [x] Build and validate source, graph, and boundary replacements off-thread.
- [x] Persist delta sidecars transactionally.
- [x] Publish a complete immutable batch and retire covered blockers.
- [x] Reject every stale rebuild result.

Gate: removed floor, removed wall, added obstruction, repeated edit, seam edit, and edit-during-build tests never expose unsafe or mixed topology.

Result: streamed destruction now installs generation-tagged conservative chunk
blockers, coalesces newer snapshots while one rebuild runs, and rejects stale
runtime or edit generations. Rebuilt sidecars use immutable generation-qualified
paths and temp-write, sync, close, and rename persistence; the world delta and
resident query publish only after the complete batch succeeds, and covered
blockers retire only after that residency revision is installed.

### Phase 5: Route Dependency Epochs

- [x] Add per-tile topology epochs to snapshots.
- [x] Record route tile dependencies and epochs.
- [x] Retain unaffected routes across global publications.
- [x] Prioritize hard-invalid routes and budget soft replans.
- [x] Drop stale asynchronous route results by request and dependency version.
- [x] Update Actiongame traces and debug HUD with dependency/replan reasons.

Gate: a localized Crossfire edit replans only affected routes and locomotion never follows a newly blocked segment.

Result: immutable query publication now advances epochs only for tiles whose
effective resident topology changed, including seam degrees and runtime
overlays. Routes carry sorted tile/epoch dependencies; Actiongame retains
unaffected routes, stops and prioritizes hard-invalid routes before traversal,
budgets ordinary replans, and rejects stale async results by actor request plus
dependency status. Navigation HUD and bot traces expose dependency counts and
replan reasons.

### Phase 6: Rebuild Consumers And Assets

- [x] Update `navbake`, `navdiag`, navigation graph lab, and debug overlays.
- [x] Update Actiongame route solving, tactical reachability, carrier planning, and failure tracing.
- [x] Update streamed world-delta save/load paths.
- [x] Rebuild Crossfire, Gasworks, and navigation lab bundles.
- [x] Delete dead compatibility types, loaders, and generated assets.
- [x] Update canonical content/runtime documentation.

Gate: the engine and Actiongame compile and all automatic and manual acceptance cases pass using only the new formats and query path.

Result: graph manifest/tile schema v5 and builder `voxel_graph_v14` remove the
legacy traversal `id` field; special traversals now use only stable `link_id`
and optional `owner_id`. Crossfire, Gasworks, and the navigation lab were
rebaked to `.gkns`/`.gkng` with zero hard validation errors. The lab has a
headless bake command and automatic binary/dependency coverage, while
Actiongame's overlay distinguishes runtime-blocked spans. Content, Actiongame,
and lab tests pass; the engine root gate remains blocked by the unrelated
renderer-bridge audit at `mod_voxelrt_client_systems.go:745`, and windowed
Crossfire/Gasworks/lab acceptance remains manual.

### Phase 7: Tune Only Measured Bottlenecks

- [x] Compare budgets with recorded baselines.
- [x] Increase route workers only if queue wait, not solve time, misses budget.
- [x] Tune gzip level only if bake/load trade-offs are measurable.
- [x] Pool route scratch slices only if allocation profiles show material GC cost.
- [x] Consider coarser navigation resolution only if packed storage and indexes still miss budgets.

Gate: every retained optimization has a benchmark demonstrating its value.

Result: schema v5 Crossfire measures `28,887,737` bundle bytes and
`225,428,592` retained heap bytes. Ordinary route p95 is `0.001250 ms`; a live
carrier route p95 is `0.027666 ms`. All remain far below their gates, so the
single route worker, default gzip level, unpooled route scratch, and `0.1 m`
navigation resolution remain unchanged. Measurement exposed one real miss:
exact per-tile epoch preparation took `282.821 ms` on the main thread. It now
runs in the existing overlay worker against the immutable previous snapshot;
the main-thread pointer commit benchmarks at `7.893 ns/op` with zero
allocations, while exact epoch preparation remains observable through
`navdiag` as `epoch_milliseconds`.

## Expected File Ownership

### Engine Content

Primary files likely replaced or heavily changed:

- `content/nav_graph.go`
- `content/nav_graph_io.go`
- `content/nav_graph_bake_io.go`
- `content/nav_graph_query.go`
- `content/nav_graph_route.go`
- `content/nav_graph_search_span.go`
- `content/nav_graph_blocker.go`
- `content/nav_graph_delta.go`
- `content/nav_graph_validation.go`

Builder files should change only where the new packed contracts require a
different emitted representation. Geometry and traversal behavior are not
rewritten merely to optimize storage.

### Engine Runtime

- `navigation_graph_runtime.go`
- `streamed_level_runtime.go`
- streamed navigation and world-delta tests

### Actiongame

- `src/modules/startup/navigation_route_async.go`
- `src/modules/startup/navigation_locomotion.go`
- `src/modules/startup/bot_plan.go`
- `src/modules/startup/navigation_debug.go`
- navigation, tactical, carrier, and destruction tests

## Verification Plan

### Unit And Property Cases

- deterministic binary round-trip
- truncated/corrupt/wrong-kind/wrong-version rejection
- dense and stacked columns
- same-height walk, step, stair, jump, drop, ladder, door, and carrier edges
- cross-tile seam reciprocity
- blocker splitting one region into multiple components
- blocker removal merging components
- start or goal blocked
- affected versus unaffected route dependency epochs
- stale load, overlay, rebuild, and route results
- repeated edits to the same tile before commit
- simultaneous edits across a seam
- full bake and dirty-batch rebuild equivalence
- hierarchical route reachability checked against the Dijkstra oracle

### Automated Commands

From `gekko/`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./content/...
env GOCACHE=/tmp/gekko3d-gocache go test .
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

From `actiongame/`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Run the broad engine sweep only at phase gates that cross content/runtime
boundaries.

### Benchmarks

Add runnable Go benchmarks for:

- source/graph encode and decode
- resident index construction
- ordinary hierarchical route
- carrier/blocker route
- overlay rebuild for one small and one large affected region
- one-tile and seam-batch destruction rebuild
- route dependency validation after localized publication

Benchmark fixtures are generated in memory. Do not commit a second giant map
bundle merely for performance tests.

### Manual Acceptance

Crossfire:

- bots route through ordinary combat paths without visible planning stalls
- lifts remain callable, boardable, rideable, and avoidable
- closed/open doors update route cost and required actions
- destroy walls, floors, and obstacles during combat
- observe immediate conservative blocking and later topology opening
- verify distant bots retain routes after a localized edit

Gasworks:

- stacked floors and long routes remain correct
- seam destruction does not produce mixed boundary links
- jumps, drops, and ladders remain executable

Navigation graph lab:

- visualize spans, regions, overlay components, blockers, route dependencies, dirty batches, and revision publication
- compare hierarchical results with the diagnostic oracle on small graphs

## Rejected Alternatives

### Compact JSON Only

Compact JSON is a useful measurement and would halve current files, but it
retains repeated object keys, slow decoding, large raw buffers, and verbose
in-memory transitions. It is not the long-term format.

### Compressed Existing JSON

Gzip dramatically reduces disk bytes but still reconstructs the same expensive
Go object graph. Use gzip around the new binary tile format instead.

### Global Span A* With More Workers

More workers multiply memory bandwidth and allocations without removing the
blocker-induced scaling cliff. Preserve the hierarchy first.

### Mutable In-Place Graph Patches

Patching resident graphs in place makes readers observe mixed revisions and
complicates cancellation, rollback, and seam correctness. Immutable batch
replacement is simpler and safer.

### Route Cache Or Flow Field

Shooter bots use changing tactical goals and destruction invalidates topology.
Cache invalidation would dominate. Add a goal-shared flow field only for a
future measured swarm case.

### Coarser Navigation Resolution First

A coarser grid risks losing narrow doors, ledges, and traversal mounts. Pack the
redundant graph and fix indexes first. Revisit resolution only if measured
budgets still fail.

## Risks And Mitigations

- Packed storage drops edge semantics. Mitigation: retain explicit exceptional transitions and compare generated routes with the oracle.
- Temporary blockers over-constrain removed walls. Mitigation: prioritize rebuilds near active actors; safety wins while pending.
- Large affected regions make overlay splitting expensive. Mitigation: bake regions with blocker-sensitive partition boundaries only when profiles prove this case hot.
- Frequent edits starve rebuild publication. Mitigation: coalesce generations, prioritize actor-near batches, and expose queue age.
- Snapshot retention spikes memory. Mitigation: bound in-flight jobs, drop stale work promptly, and measure retained snapshot count.
- Per-tile epochs miss an external dependency. Mitigation: derive dependency tiles centrally and test seam, jump, door, ladder, and carrier links.
- Binary corruption becomes opaque. Mitigation: strict magic/version/length/hash validation plus `navdiag` decoding and summaries.

## Completion Criteria

The rework is complete when:

- old JSON source/graph tile code and generated bundles are deleted
- all runtime navigation uses compact binary source and graph tiles
- resident span and region indexes use dense arrays, bitsets, and CSR adjacency
- blockers and carriers route through overlay components, not global span A*
- voxel edits install immediate conservative blockers and publish exact rebuilt batches atomically
- routes retain or replan from per-tile dependency epochs
- stale asynchronous work cannot publish
- Crossfire meets disk, resident-memory, route, and commit budgets on the reference machine
- Crossfire, Gasworks, and navigation graph lab pass automatic and manual acceptance
- canonical content and runtime docs describe only the new architecture
