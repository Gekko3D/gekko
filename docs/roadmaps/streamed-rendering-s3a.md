# S3a: Incremental observer demand selection

Date: 2026-10-01. Status: implemented and verified; parent S3 remains partial.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S3.
Prerequisites: S1a/S1b/S1c and S2a/S2b.

## Scope and confidence

Owner: streamed observer selection. Consumers: full/proxy preparation, collision/destruction, navigation, render handoff and ActionGame observers. Confidence: High after selection, metadata, startup demand and proxy retention inspection. SME alignment: No. Permanent improvement within v2 policy. Missing `base/skills-manifest.md`; workflow, runtime/module docs, island plan and owning code read.

Cache observer demand; update cube differences without rebuilding unchanged volumes each frame. Preserve Chebyshev cubes, radius defaults/clamping, PVS/adjacency, non-imported exceptions, full-sector expansion and global proxy fallback. Containing-sector-only PVS, gameplay-only profiles, priorities, scene gathering and v3/layer selection remain separate.

Before S3a: `updateStreamedLevelObserverSystem` rebuilds six aggregate sets and five radius volumes per observer. It also schedules/retries, upgrades/unloads and reconciles tickets each frame. Selection uses world-space positions and `ChunkSize`, not mutable layer transforms.

Unknown: speedups and metadata memory cost. Live entries bounded by indexed metadata and active footprints; no historical observer/radius entries. Empty count/history maps and Stop release capacity; nonempty Go maps may retain peak capacity. No byte budget, process ceiling or measured frame-time gain.

## Ownership and invalidation

1. Add private selection owner to `StreamedLevelRuntimeState`. Cache each
   observer by entity identity, chunk coordinate, effective visual/keep/prefetch/
   collision/destruction radii, chunk size, and runtime/selection revision.
   Scan observer components each frame so additions, removals, transformed
   ancestors after normal hierarchy propagation, and radius changes remain
   visible at existing command-flush/stage boundary.
2. Maintain demand counts for overlapping observers. Removing or changing one
   observer removes only that observer's contributions. Track sector expansion
   and PVS contributions per observer; avoid conflating chunk and sector demand.
   Union raw chunk demand and sector demand across observers before imported
   filtering and whole-sector expansion. One observer's raw chunk can survive
   because another observer supplies its sector visibility; filtering observers
   independently would change existing policy.
3. Update entering/exiting rectangular shells for moves with overlapping cubes.
   Enumerate disjoint slabs of old/new cube difference; handle diagonal and
   negative-coordinate moves without duplicates. Teleports and radius changes
   may rebuild bounded observer volumes. Do not iterate traveled distance.
4. Reuse aggregate selection when all observer keys and metadata are unchanged.
   Cache imported-sector derivation and all-LOD fallback set so idle
   selection does not rescan manifest. Rebuild derived aggregate selection
   only after demand or relevant metadata changes.
5. Add nil-safe public, main-thread method
   `(*StreamedLevelRuntimeState).InvalidateObserverSelection()`. Call it after
   mutating selection metadata such as imported sector/chunk membership, PVS or
   adjacency, placements, terrain occupancy/overrides, backing availability, or
   future layer transform that changes selection coordinates. Existing startup
   and Stop/Restart reset owner. Runtime-owned terrain override publication
   and removal call invalidation at their exact mutation boundaries, including
   partial persistence followed by error. External callers
   must invalidate after in-place metadata mutations; exported maps do not
   provide automatic change tracking. Radii, chunk size, generation, and proxy
   enablement are checked directly.
6. Keep cache independent from transient published demand. Synchronous
   startup can add collision/destruction demand; unloading can add temporary
   proxy pins. Restore these working memberships from cached base demand on
   next update using tracked differences, not full per-frame map copies. Such
   mutations must never contaminate base cache or become permanent pins.
   Published selection maps remain runtime-owned read-only views for consumers.
7. Continue ticket refresh/reconciliation, prepare retries/admission, navigation
   residency requests, unloads, upgrades, and pending-hint pruning on idle ticks.
   Selection reuse must not become early return from streaming progress.
8. Release observer histories, demand counts, cached metadata, and public demand
   views when Stop enters completed teardown; Restart starts with fresh selection.
   Persistence failure before teardown retains resumable runtime and cache.
   Later cleanup error after `Initialized` becomes false still releases
   selection owner. No worker may mutate selection state or require new lock
   across preparation/IO.

Expose two cumulative counters: `ObserverSelectionBuildCount` counts changed-input demand evaluations, including additions/removals; `ObserverSelectionChunkVisitCount` counts radius build/update coordinates. Both `uint64`, reset at Start. Describe actual selection work/idle shells, not benchmarks or functional parity substitutes.

Early return stops retries/readiness. Per-frame manifest hashing defeats idle reuse; length/pointer checks miss in-place PVS changes. Explicit owner revisions and small observer keys match mutation model.

## TDD coverage by functionality

User requested Sol 6.1 TDD. Protect public demand, readiness/lifecycle and operational work contracts. Avoid private layouts, helper order, map identity, timing or allocation thresholds.

- Idle and within-cell movement: identical demanded memberships, no further
  radius-volume visits/evaluations after warmup, with frame progress still active.
- Shells and teleports: compare every public demand set against independently
  enumerated expected cubes across axis/diagonal/negative movement, larger
  jumps, radius/default changes, and chunk-size changes. Small moves visit
  boundary work rather than complete unchanged volumes; do not assert
  particular slab decomposition or exact visit count.
- Multiple observers: overlapping/disjoint visual, keep, collision, destruction,
  and PVS-expanded demands survive movement/removal of one observer; removal of
  final observer clears its demand at existing flush boundary.
- Imported/content policy: current PVS/adjacency, hidden-sector filtering,
  non-imported placement/terrain exceptions, full-sector expansion, backing,
  proxy fallback, and proxy enablement preserve existing selection semantics.
- Invalidation: in-place visibility/membership/content changes with stationary
  observers take effect after declared invalidation boundary; lifecycle and
  generation changes do not reuse obsolete selections.
- Progress: idle observers still schedule after worker slots/byte credits free,
  apply readiness transitions, retire/unload content, and honor collision and
  destruction upgrades. Reuse existing S1/S2 integration fixtures where useful.
- Transient demand: startup collision/destruction additions and temporary proxy
  handoff pins do not contaminate base demand. Startup additions clear on later
  idle updates while true observer demand remains. Under current global fallback
  policy every valid proxy pin already belongs to base fallback; verify proxy
  memberships clear when proxies are disabled instead of inventing idle-only
  extra proxy membership that current policy cannot produce.
- Lifecycle: Stop/Restart releases selection ownership and new session derives
  its own metadata/demand. Verify resumability if exercising failed Stop.

## Expected changes and verification

Files: `streamed_level_runtime.go`, focused `streamed_level_selection.go`, S3a tests, roadmap/parent status and canonical `docs/content/streaming-and-worlds.md`. Missing field/API may cause compile RED; production follows root test review. Require functional GREEN and adversarial code review.

Verify focused S3a, streamed/navigation/render-residency, engine root, races and ActionGame checks/build. No shader/upload/layout changes. Headless fixtures verify continued handoff; native smoke only for remaining visual coverage risk.

## Execution record

### Tests and design review

- Root extracted this scope and reviewed existing selection, renderer,
  content, and lifecycle owners before production edits.
- Sol 6.1 design review clarified global observer union before filtering,
  terrain invalidation at partial-persistence boundaries, and pre/post-teardown
  failure semantics. These findings are reflected above.
- Sol 6.1 wrote eleven functional tests in
  `streamed_level_selection_s3a_test.go`; existing tests remained unchanged.
- Root and independent Sol 6.1 reviewer inspected test contracts.
  Review found fixture assumption: empty level uses 32-unit chunks, so
  shell moves now derive from `state.ChunkSize`. Second review tightened
  work bound: five equal radii could otherwise share one full cube and still
  pass. Radius-20 unit moves must visit fewer coordinates than one complete
  unique cube, while allowing independent shell traversal for all five radii.
  Author repeated RED after these contract corrections.
- RED command: `env GOCACHE=/tmp/gekko3d-gocache go test . -run
  '^TestS3aSelection' -count=1`. Exit 1 from missing public selection metric
  declarations; functional assertions could not execute at this phase.
  Local log: `/tmp/gekko-s3a-red.log`.
- First implementation run passed nine tests; two stopped before selection
  because imported fixture left three authored chunks without sector
  references. Test author added valid startup references, then trimmed
  test's runtime references at its declared metadata mutation boundary. Root
  checked that raw-coordinate/global-filtering assertion still cannot be
  satisfied by full-sector expansion. All eleven focused tests then passed;
  local log: `/tmp/gekko-s3a-fixture-green.log`.

Production-review gate: automatic terrain override publication/removal invalidation. Focused tests do not independently exercise warmed-cache mutation hooks.

### Implementation and adversarial review

Sol 6.1 implemented with tests frozen: global overlap counts, three per-observer sector-volume counts, cached visibility/full-sector/fallback and disjoint cube differences. Changed input republishes aggregates; idle scans observers/temporary additions without rebuilding volumes. Aggregate publication may traverse current demand sets; shell counters do not measure all streaming work.

Root and independent Sol 6.1 review: counts, global filtering, sector policy, slabs, transient restoration, ECS boundaries, worker isolation and teardown. No actionable finding. Terrain override invalidates immediately after publication/removal, before persistence failures. Start/completed teardown release owner; pre-teardown failure retains it. Collision/destruction and navigation stay independent of GPU readiness.

Canonical docs/parent roadmap record API, read-only views, invalidation, counters and memory limits. Scene gathering and future layer selection remain S3 work; S2 budgets, queues and decode cancellation unchanged.

### Verification

All commands passed on Go 1.25.4, darwin/arm64. Engine commands run from `gekko/`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3aSelection' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run 'Test(Streamed|StartStreamed|RestartStreamed|StopStreamed|BuildEffectiveStreamed|Navigation|RuntimeNavigation|S2[ab])' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run 'Test(S3aSelection|Streamed|StartStreamed|RestartStreamed|StopStreamed|BuildEffectiveStreamed|Navigation|RuntimeNavigation|S2[ab])' -count=1
git diff --check
```

Affected-consumer commands run from `actiongame/`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./src/modules/startup -run 'Test(ActionGameStreamed|ActionGameNPCNavigation|ActionGameNavigation|ActionGameLevelSelection|ArenaLevelsStart|ActionGameNPCLocomotionSystemFreezes)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go build ./...
```

Local logs: `/tmp/gekko-s3a-engine-green.log`,
`/tmp/gekko-s3a-race-green.log`, `/tmp/gekko-s3a-actiongame-green.log`, and
`/tmp/gekko-s3a-actiongame-build.log`. Race emitted existing macOS
`LC_DYSYMTAB` linker warning; ActionGame build emitted module stat-cache
permission warning. Both exited 0.

No native/GPU smoke. Selection ownership changes preserve renderer interface; headless cohort/readiness fixtures cover handoff. No pixel parity or measured speedup claimed. Automatic terrain invalidation and late cleanup-error release verified by code review, not new failure-injection tests.