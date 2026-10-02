# S3c: Structural revisions and voxel candidate inventory

Date: 2026-10-02. Status: implemented and verified; S3 remains partial.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S3.
Prerequisites: S3a/S3b.

## Scope and confidence

Owners: committed ECS structural mutation and core voxel candidate inventory. Consumers: scene residency and per-frame object/material/LOD/ticket processing. Confidence: High after storage, queries, flushes, hierarchy and bridge inspection. Independent Sol 6.1 review confirmed permanent extraction prerequisite. SME alignment: No. Missing `base/skills-manifest.md`; workflow/owning docs read.

Known: `archetypeGeneration` tracks new archetypes, not membership/row relocation. Query callbacks expose mutable fields; hierarchy, animation and brushes write without automatic changed-component contract. Bridge keeps stable objects but discovers matching entities each frame.

Unknown: performance benefit. Reuse membership discovery; still visit every candidate and process live values. No complete value notifications, hierarchy/culling bypass, camera LOD change or future layer selection.

## Architecture and invariants

1. Add committed `uint64` structural revision to `ecsStorage`, which is shared
   by value-copied `Ecs` wrappers. Expose nil-safe main-thread
   `Ecs.StructuralRevision()` and `Commands.StructuralRevision()` reads. This is
   storage invalidation stamp, not component-value version or event count
   consumers should use for semantic deltas.
2. Advance revision once after each successful outer insertion, removal,
   component addition/replacement or component removal. Component replacement
   counts even in same archetype and with equal values because row storage
   can relocate. Do not increment internal recycle operation again during
   migration. Missing-entity commands do not advance it. Enqueueing, empty
   flushes, reads/type registration, field writes and sanitized empty public
   component commands do not advance it.
3. Preserve existing mutation/flush behavior. Removing absent component type
   from live entity currently migrates its row; this conservative committed
   churn advances stamp. Do not turn this slice into no-op migration
   refactor. Structural changes become visible at existing stage flush.
4. `VoxelRtState` owns private candidate inventory keyed by `*ecsStorage`
   identity and committed revision. Rebuild it from all live entities that have
   both `TransformComponent` and `VoxelModelComponent`, including hidden,
   missing-geometry, streamed and sprite-LOD candidates. Never derive it only
   from `instanceMap`, since excluded candidates can become renderable without
   structural changes.
5. Group entity-ID/row pairs by matching archetype. Resolve component IDs for
   current owner on rebuild. Cache neither component pointers nor typed
   component-slice aliases. Each iteration reacquires current typed columns
   once per batch and passes transient row addresses to existing bridge
   body. Insertions, migrations, replacements, removals and recycled-row reuse
   invalidate all row locations. This avoids per-entity reflective lookups.
6. Keep this helper private and main-thread owned. Field writes and buffered
   commands inside iteration remain supported. Immediate structural mutation
   or manual flush during iteration is unsupported, as with current queries;
   bridge callback does neither. Query order remains unspecified.
7. Every candidate still runs current geometry resolution, direct transform and
   model metadata checks, elapsed-time palette/material processing, LOD/sprite
   work, hidden residency and streamed-ticket adoption. Camera/light extraction
   and begin/end streamed sync remain unconditional. Existing ancestor changes,
   animated children and moving brushes reach current `PreRender` boundary.
8. Rebuilds discard obsolete candidate IDs and clear obsolete archetype
   references, including capacity tails. Empty inventories release aggregate
   capacity; new state starts empty. Equal revision from another storage
   owner must not reuse previous inventory or component IDs. Nonempty
   capacity can retain its peak; no byte ceiling is introduced.

Expose operational counters on `VoxelRtState`:

- `VoxelCandidateInventoryBuildCount uint64`: cumulative actual membership
  rebuilds, including first/changed empty inventories.
- `VoxelCandidateCount int`: current raw Transform+VoxelModel candidates,
  including candidates excluded from renderer residency.

Counters describe ownership/work, not benchmarks or total extraction cost.

Revision alone gives no bridge reuse; per-ID reflection adds hot-loop cost; snapshots duplicate comparisons without structural ownership. Complete events need engine/editor/gameplay migration. Revision-keyed typed inventory preserves public pointer mutation compatibility.

## Functionality tests and corner cases

User-authorized Sol 6.1 tests-first/review/implementation/review/commit. Add focused tests; existing tests unchanged. Assert public revisions/counters and real bridge results, never private layout/rows/fetch order or timings.

- Revision visibility: enqueue vs flush, insertion/removal within existing
  archetypes, component migration/replacement, ignored missing entities, reads,
  direct writes, empty commands/flushes, and shared-storage wrapper reads.
- First/idle inventory: exact candidate membership and renderer object identity;
  unchanged frames and direct field writes add no membership rebuilds.
- Structural admission/removal: add/remove either required component, replace
  same-archetype component, grow existing typed slices, recycle removed rows,
  and reuse existing empty archetype. Stale rows must never assign one
  entity's geometry/transform to another. Verify before/after flush behavior.
- Value compatibility: live transform/model changes, source geometry swaps,
  late geometry resolution and palette changes reach real renderer objects
  while structural stamp and inventory build count remain unchanged.
- Excluded candidates: ordinary hidden entities, hidden streamed residency,
  directly changed streamed tickets/priorities, and sprite/voxel LOD transitions
  remain discoverable. Reuse existing detailed streamed/LOD tests, adding only
  focused cached-iteration integration where needed.
- Timing: real ancestor/local/reparent propagation reaches bridge in
  same stage without structural notification. Existing animation/moving-brush
  tests protect producer semantics; one integration case connects propagation
  to cached extraction.
- Ownership: another ECS storage with equal revision and different membership
  rebuilds; empty/shrunk inventory releases candidate ownership; nil revision
  reads are safe. No private cache capacity assertions.

Missing declarations may cause compile RED. Root/independent Sol 6.1 review before production and after GREEN.

## Files and verification

Files: `ecs.go`, `commands.go`, new `mod_voxelrt_client_inventory.go`, `mod_voxelrt_client.go` state and `mod_voxelrt_client_systems.go` iterator. No hierarchy, animation, brush, stage, buffer or shader changes needed. Canonical ECS/runtime/renderer docs and roadmaps updated.

Verify S3c RED/GREEN, ECS/bridge/hierarchy/animation/brush/streaming, engine sweep, races and consumers. S3b reproduced ActionGame bot failures at prior HEAD; use affected checks.

Disposable native harness: idle, hierarchy motion, same-archetype growth/replacement/removal, hidden/refined handoff and camera/light continuity. Observe inventory/S3b counters and rendered geometry. Establish continuity/work contracts, not pixel parity or frame-time gain.

## Execution record

Root scoped after S3b. Independent Sol 6.1 review confirmed storage revision/complete membership and identified per-ID reflection cost. Cache row locations, reacquire typed columns; retain no component pointers/aliases.

Sol 6.1 authored twelve tests in two new files: three revision tests with ten committed-mutation subcases, nine bridge tests with two required-component subcases. Root/independent Sol 6.1 review found no fixture/contract blocker. Assert committed stamps, public counters and bridge results. Existing tests/production unchanged during RED.

RED command: `env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3c'
-count=1`. Exit 1 on missing `Ecs.StructuralRevision()` and
`Commands.StructuralRevision()` declarations; functional assertions could not
execute yet. Log: `/tmp/gekko-s3c-red.log`.

Sol 6.1 implemented with tests unchanged; focused GREEN (1.029s). Root/independent Sol 6.1 review: outer mutation ownership, fresh typed columns, no retained aliases, tail cleanup, no iterator flush and same-ID geometry replacement. No blocker. Docs corrected: animation in `Update`, hierarchy in `PostUpdate`, bridge in `PreRender`.

Verification, using `GOCACHE=/tmp/gekko3d-gocache`:

- Existing ECS/query/voxel/streaming/hierarchy/animation/moving-brush checks:
  `go test . -run '^Test(Ecs_|EcsReflect_|Query_|VoxelRt|SyncVoxelRt|StreamedVoxel|StreamedRender|TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAssetAnimation|NPCAnimation|MovingBrush|GroundedPlayerLandsOnMovingBrush)' -count=1` passed (0.447s).
- Engine `go test ./...` passed; root package 10.305s. Log:
  `/tmp/gekko-s3c-engine.log`.
- Focused race command used same pattern with `S3c|` added and `-race`;
  passed (2.298s). Log: `/tmp/gekko-s3c-race.log`. Linker emitted its known
  `LC_DYSYMTAB` warning; no race report occurred.
- ActionGame `go test ./... -run '^$'` and destruction-derby `go test ./...`
  passed. Logs: `/tmp/gekko-s3c-actiongame-compile.log` and
  `/tmp/gekko-s3c-example-compile.log`.
- Editor production `go build ./...` exited 0. Its test compilation failed on
  missing `collectUIButtonLabelsForTest` and `containsButtonOrLabel` helpers in
  `base_world_import_test.go`. Reproduced same failure with engine prior
  HEAD `f9911af`, archived checkout and disposable complete workspace;
  this is existing editor test issue. Logs:
  `/tmp/gekko-s3c-editor-build.log`, `/tmp/gekko-s3c-editor-compile.log` and
  `/tmp/gekko-s3c-editor-baseline.log`. Successful build also logged
  denied module stat-cache write; no repository files needed alteration.

Real-GPU `/tmp/gekko-s3c-smoke.go`, `/tmp/GekkoS3cSmoke.app`: exit 0, 909 frames in 31.676s. Eleven idle holds: 63–64 frames, at least 2.2s, unchanged inventory/revision/preparation/publication/binding counters. Each frame checked raw membership including hidden, unresolved and streamed entities; camera/light data and frame progress stayed live.

Native checks: ancestor motion, 64 same-archetype additions, replacement/removal/recycled rows, direct writes, late geometry, hiding, destination replacement and invalidation. Value changes update objects without inventory rebuilds. Growth: 8→72 candidates, one rebuild. Streamed proxy refined/coarsened; final revision 97, builds 12, candidates 69. Buffer replacement republishes/refreshes bindings without CPU rebuild; invalidation rebuilds/publishes without membership changes. CUA showed final coarsened rendering. Log: `/tmp/gekko-s3c-smoke.log`.

Established membership/value compatibility and native continuity, not pixel parity, frame-time gain or memory ceiling. All candidates still process live values. Complete value notifications, incremental extraction and future layer/transform selection remain S3 work.