# S3c: Structural revisions and voxel candidate inventory

Date: 2026-10-02. Status: implemented and verified; S3 remains partial.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S3.
Prerequisites: S3a/S3b.

## Scope and confidence

Primary owner: committed ECS structural mutation and the core voxel bridge's
candidate inventory. Consumers: voxel scene residency and current per-frame
object, material, LOD and streamed-ticket processing. Confidence: High after
tracing ECS storage, typed queries, command flushing, hierarchy and bridge code.
Independent Sol 6.1 architecture review confirmed this permanent prerequisite
for incremental extraction. SME alignment required: No.
`base/skills-manifest.md` is absent; agent workflow and owning docs were read.

Known: `archetypeGeneration` tracks newly created archetypes, not entity
membership or row relocation within existing archetypes. Query callbacks expose
mutable public component fields. Hierarchy, animation and moving brushes write
these fields without an automatic changed-component contract. The bridge keeps
stable renderer objects, but discovers all matching entities each frame.

Unknown: workload-dependent performance benefit. This slice reuses membership
discovery; it still visits all candidates and performs live value processing.
It does not introduce complete value-dirty notifications, skip hierarchy or
culling, change camera-dependent LOD, or implement future layer selection.

## Architecture and invariants

1. Add a committed `uint64` structural revision to `ecsStorage`, which is shared
   by value-copied `Ecs` wrappers. Expose nil-safe main-thread
   `Ecs.StructuralRevision()` and `Commands.StructuralRevision()` reads. This is
   a storage invalidation stamp, not a component-value version or event count
   consumers should use for semantic deltas.
2. Advance the revision once after each successful outer insertion, removal,
   component addition/replacement or component removal. Component replacement
   counts even in the same archetype and with equal values because row storage
   can relocate. Do not increment the internal recycle operation again during
   migration. Missing-entity commands do not advance it. Enqueueing, empty
   flushes, reads/type registration, field writes and sanitized empty public
   component commands do not advance it.
3. Preserve existing mutation/flush behavior. Removing an absent component type
   from a live entity currently migrates its row; this conservative committed
   churn advances the stamp. Do not turn this slice into a no-op migration
   refactor. Structural changes become visible at the existing stage flush.
4. `VoxelRtState` owns a private candidate inventory keyed by `*ecsStorage`
   identity and committed revision. Rebuild it from all live entities that have
   both `TransformComponent` and `VoxelModelComponent`, including hidden,
   missing-geometry, streamed and sprite-LOD candidates. Never derive it only
   from `instanceMap`, since excluded candidates can become renderable without
   structural changes.
5. Group entity-ID/row pairs by matching archetype. Resolve component IDs for
   the current owner on rebuild. Cache neither component pointers nor typed
   component-slice aliases. Each iteration reacquires current typed columns
   once per batch and passes transient row addresses to the existing bridge
   body. Insertions, migrations, replacements, removals and recycled-row reuse
   invalidate all row locations. This avoids per-entity reflective lookups.
6. Keep this helper private and main-thread owned. Field writes and buffered
   commands inside iteration remain supported. An immediate structural mutation
   or manual flush during iteration is unsupported, as with current queries;
   the bridge callback does neither. Query order remains unspecified.
7. Every candidate still runs current geometry resolution, direct transform and
   model metadata checks, elapsed-time palette/material processing, LOD/sprite
   work, hidden residency and streamed-ticket adoption. Camera/light extraction
   and begin/end streamed sync remain unconditional. Existing ancestor changes,
   animated children and moving brushes reach the current `PreRender` boundary.
8. Rebuilds discard obsolete candidate IDs and clear obsolete archetype
   references, including capacity tails. Empty inventories release aggregate
   capacity; a new state starts empty. An equal revision from another storage
   owner must not reuse the previous inventory or component IDs. Nonempty
   capacity can retain its peak; no byte ceiling is introduced.

Expose operational counters on `VoxelRtState`:

- `VoxelCandidateInventoryBuildCount uint64`: cumulative actual membership
  rebuilds, including first/changed empty inventories.
- `VoxelCandidateCount int`: current raw Transform+VoxelModel candidates,
  including candidates excluded from renderer residency.

Counters describe ownership/work, not benchmarks or total extraction cost.

Alternatives: revision alone supplies no bridge reuse; per-ID reflection adds
hot-loop lookup cost; value snapshots duplicate current comparisons without
solving structural ownership. Complete value events require a separate engine,
editor and gameplay mutation migration. A revision-keyed typed inventory is the
smallest permanent step that preserves public pointer mutation compatibility.

## Functionality tests and corner cases

The continuing Sol 6.1 tests-first/review/implementation/review/commit workflow
explicitly authorizes focused new functionality tests. Existing tests remain
unchanged. Tests inspect public revision/counters and real bridge results,
never cache layout, private row handles, component-fetch order or timings.

- Revision visibility: enqueue vs flush, insertion/removal within existing
  archetypes, component migration/replacement, ignored missing entities, reads,
  direct writes, empty commands/flushes, and shared-storage wrapper reads.
- First/idle inventory: exact candidate membership and renderer object identity;
  unchanged frames and direct field writes add no membership rebuilds.
- Structural admission/removal: add/remove either required component, replace a
  same-archetype component, grow existing typed slices, recycle removed rows,
  and reuse an existing empty archetype. Stale rows must never assign one
  entity's geometry/transform to another. Verify before/after flush behavior.
- Value compatibility: live transform/model changes, source geometry swaps,
  late geometry resolution and palette changes reach real renderer objects
  while the structural stamp and inventory build count remain unchanged.
- Excluded candidates: ordinary hidden entities, hidden streamed residency,
  directly changed streamed tickets/priorities, and sprite/voxel LOD transitions
  remain discoverable. Reuse existing detailed streamed/LOD tests, adding only
  focused cached-iteration integration where needed.
- Timing: real ancestor/local/reparent propagation reaches the bridge in the
  same stage without structural notification. Existing animation/moving-brush
  tests protect producer semantics; one integration case connects propagation
  to cached extraction.
- Ownership: another ECS storage with equal revision and different membership
  rebuilds; empty/shrunk inventory releases candidate ownership; nil revision
  reads are safe. No private cache capacity assertions.

Missing declarations may produce compile-failure RED. Root and independent
Sol 6.1 review tests before implementation; repeat the review after GREEN.

## Files and verification

Expected production: `ecs.go`, `commands.go`, new
`mod_voxelrt_client_inventory.go`, state fields in `mod_voxelrt_client.go`, and
the instance iterator in `mod_voxelrt_client_systems.go`. Hierarchy, animation,
brushes, command stage order, renderer buffers and shaders need no changes.
Canonical docs: ECS/runtime ownership, renderer runtime, parent roadmap and
this execution record.

Run focused S3c RED/GREEN, existing ECS/bridge/hierarchy/animation/brush and
streaming tests, final engine sweep and focused race, then affected consumer
compilation. ActionGame bot test failures reproduced at prior HEAD in S3b;
use affected checks rather than rerunning that unrelated suite.

A disposable native harness must exercise stationary frames, hierarchy motion,
same-archetype growth/replacement/removal, hidden/refined streamed handoff and
camera/light continuity. Observe inventory counters and S3b record counters;
inspect rendered geometry. Native checks establish continuity and recorded
work contracts, not pixel parity or frame-time gains.

## Execution record

Root extracted this scope after S3b. Independent Sol 6.1 architecture review
confirmed storage-owned revision and complete candidate membership. Review
identified the per-ID reflection cost; design uses cached row locations and
fresh typed columns instead, while retaining no component pointers or aliases.

Sol 6.1 authored twelve focused tests in two new files: three revision tests
(ten committed-mutation subcases) and nine bridge tests (two required-component
subcases). Root and an independent Sol 6.1 reviewer found no actionable fixture
or contract blockers. Tests use committed stamps, public inventory counters and
real bridge results; no existing tests or production files changed during RED.

RED command: `env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3c'
-count=1`. Exit 1 on missing `Ecs.StructuralRevision()` and
`Commands.StructuralRevision()` declarations; functional assertions could not
execute yet. Log: `/tmp/gekko-s3c-red.log`.

Sol 6.1 implemented production code with the reviewed tests unchanged. The
focused command above passed GREEN (1.029s). Root and an independent Sol 6.1
reviewer checked outer mutation ownership, fresh typed columns without retained
component pointers/aliases, capacity-tail cleanup, no flush inside iteration,
and same-ID geometry replacement compatibility. No code blocker remained.
Review corrected one documentation statement: animation runs in `Update` and
hierarchy in `PostUpdate`; their results reach the existing `PreRender` boundary.

Verification, using `GOCACHE=/tmp/gekko3d-gocache`:

- Existing ECS/query/voxel/streaming/hierarchy/animation/moving-brush checks:
  `go test . -run '^Test(Ecs_|EcsReflect_|Query_|VoxelRt|SyncVoxelRt|StreamedVoxel|StreamedRender|TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAssetAnimation|NPCAnimation|MovingBrush|GroundedPlayerLandsOnMovingBrush)' -count=1` passed (0.447s).
- Engine `go test ./...` passed; root package 10.305s. Log:
  `/tmp/gekko-s3c-engine.log`.
- Focused race command used the same pattern with `S3c|` added and `-race`;
  passed (2.298s). Log: `/tmp/gekko-s3c-race.log`. The linker emitted its known
  `LC_DYSYMTAB` warning; no race report occurred.
- ActionGame `go test ./... -run '^$'` and destruction-derby `go test ./...`
  passed. Logs: `/tmp/gekko-s3c-actiongame-compile.log` and
  `/tmp/gekko-s3c-example-compile.log`.
- Editor production `go build ./...` exited 0. Its test compilation failed on
  missing `collectUIButtonLabelsForTest` and `containsButtonOrLabel` helpers in
  `base_world_import_test.go`. Reproduced the same failure with engine prior
  HEAD `f9911af`, an archived checkout and a disposable complete workspace;
  this is an existing editor test issue. Logs:
  `/tmp/gekko-s3c-editor-build.log`, `/tmp/gekko-s3c-editor-compile.log` and
  `/tmp/gekko-s3c-editor-baseline.log`. The successful build also logged a
  denied module stat-cache write; no repository files needed alteration.

Disposable real-GPU harness `/tmp/gekko-s3c-smoke.go` built and ran from
`/tmp/GekkoS3cSmoke.app`; exit 0. It passed 909 frames in 31.676s. Eleven idle
holds each completed 63–64 frames over at least 2.2s with unchanged inventory,
ECS revision, scene preparation, publication and binding counters. It checked
complete raw candidate counts against actual ECS membership each frame,
including hidden, unresolved and streamed entities, while camera/light data
and renderer frames remained live.

The native run checked ancestor motion, 64 additions in an existing archetype,
same-archetype component replacement, removal/recycled-row reuse, direct field
writes, late geometry resolution, hiding, GPU destination replacement and
explicit scene-record invalidation. Ancestor/direct value changes updated
renderer objects without membership rebuilds. Growth changed raw candidates
8→72 and rebuilt the inventory once. A real streamed proxy refined and later
coarsened; final counters were revision 97, inventory builds 12 and candidates
69. Buffer replacement republished records/refreshed bindings without CPU
record rebuilds; explicit invalidation rebuilt/published records while leaving
candidate membership unchanged. CUA inspection showed the final coarsened
geometry rendered. Log: `/tmp/gekko-s3c-smoke.log`.

These checks establish membership/value compatibility and native continuity,
not pixel parity, frame-time gains or a memory ceiling. All candidates still
receive live processing. Complete component-value notifications, incremental
value extraction and future layer/transform selection remain further S3 work.
