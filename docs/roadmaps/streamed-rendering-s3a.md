# S3a: Incremental observer demand selection

Date: 2026-10-01. Status: implemented and verified; parent S3 remains partial.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S3.
Prerequisites: S1a/S1b/S1c and S2a/S2b.

## Scope and confidence

Primary owner: streamed runtime observer selection. Consumers: full/proxy
preparation, collision/destruction residency, navigation residency, render
handoff, and ActionGame observers. Confidence: High after tracing selection,
metadata mutation, synchronous startup demand, and temporary proxy retention.
SME alignment required: No. This is a permanent ownership improvement within
the current v2 selection policy. `base/skills-manifest.md` is absent; the agent
workflow, runtime/module documentation, island plan, and owning code were read.

Cache observer demand and update cube differences without enumerating unchanged
radius volumes every frame. Preserve the current Chebyshev cubes, effective
radius defaults/clamping, imported-sector PVS/adjacency behavior, non-imported
content exceptions, full-sector expansion, and global proxy fallback policy.
Containing-sector-only PVS, gameplay-only observer profiles, queue priorities,
scene gathering, and v3/layer selection remain separate work.

Before S3a: `updateStreamedLevelObserverSystem` reconstructed six aggregate
sets and each observer's five radius volumes. It also schedules/retries work,
upgrades and unloads residency, and reconciles render tickets each frame.
Selection currently uses world-space transform positions and `ChunkSize`; it
does not map observers through a mutable layer transform.

Unknown: workload-dependent speedups and selection metadata memory costs. Live
selection entries are bounded by indexed manifest metadata and active observer
footprints, without historical observer/radius entries. Empty count/history
maps and Stop release capacity; nonempty Go maps may retain peak capacity.
This is not a selection byte budget, total process memory ceiling, or measured
frame-time gain.

## Ownership and invalidation

1. Add a private selection owner to `StreamedLevelRuntimeState`. Cache each
   observer by entity identity, chunk coordinate, effective visual/keep/prefetch/
   collision/destruction radii, chunk size, and runtime/selection revision.
   Scan observer components each frame so additions, removals, transformed
   ancestors after normal hierarchy propagation, and radius changes remain
   visible at the existing command-flush/stage boundary.
2. Maintain demand counts for overlapping observers. Removing or changing one
   observer removes only that observer's contributions. Track sector expansion
   and PVS contributions per observer; avoid conflating chunk and sector demand.
   Union raw chunk demand and sector demand across observers before imported
   filtering and whole-sector expansion. One observer's raw chunk can survive
   because another observer supplies its sector visibility; filtering observers
   independently would change existing policy.
3. Update entering/exiting rectangular shells for moves with overlapping cubes.
   Enumerate disjoint slabs of the old/new cube difference; handle diagonal and
   negative-coordinate moves without duplicates. Teleports and radius changes
   may rebuild bounded observer volumes. Do not iterate the traveled distance.
4. Reuse aggregate selection when all observer keys and metadata are unchanged.
   Cache imported-sector derivation and the all-LOD fallback set so idle
   selection does not rescan the manifest. Rebuild derived aggregate selection
   only after demand or relevant metadata changes.
5. Add a nil-safe public, main-thread method
   `(*StreamedLevelRuntimeState).InvalidateObserverSelection()`. Call it after
   mutating selection metadata such as imported sector/chunk membership, PVS or
   adjacency, placements, terrain occupancy/overrides, backing availability, or
   a future layer transform that changes selection coordinates. Existing startup
   and Stop/Restart reset the owner. Runtime-owned terrain override publication
   and removal call invalidation at their exact mutation boundaries, including
   partial persistence followed by an error. External callers
   must invalidate after in-place metadata mutations; exported maps do not
   provide automatic change tracking. Radii, chunk size, generation, and proxy
   enablement are checked directly.
6. Keep the cache independent from transient published demand. Synchronous
   startup can add collision/destruction demand; unloading can add temporary
   proxy pins. Restore these working memberships from cached base demand on the
   next update using tracked differences, not full per-frame map copies. Such
   mutations must never contaminate the base cache or become permanent pins.
   Published selection maps remain runtime-owned read-only views for consumers.
7. Continue ticket refresh/reconciliation, prepare retries/admission, navigation
   residency requests, unloads, upgrades, and pending-hint pruning on idle ticks.
   Selection reuse must not become an early return from streaming progress.
8. Release observer histories, demand counts, cached metadata, and public demand
   views when Stop enters completed teardown; Restart starts with fresh selection.
   A persistence failure before teardown retains a resumable runtime and cache.
   A later cleanup error after `Initialized` becomes false still releases the
   selection owner. No worker may mutate selection state or require a new lock
   across preparation/IO.

Expose two cumulative operational counters on existing runtime metrics:
`ObserverSelectionBuildCount` counts observer demand evaluations after changed
inputs, including additions/removals; `ObserverSelectionChunkVisitCount` counts
chunk coordinates visited to build or update radius volumes. Both use `uint64`
and reset with runtime metrics at Start.
Counters describe actual selection work and support the idle/shell contract;
they are not a new benchmark phase or a substitute for functional parity.

Alternatives: returning early from the whole observer system would stop retries
and render readiness; hashing entire manifests each frame defeats idle reuse;
map-length or pointer-only invalidation misses in-place PVS changes. Explicit
owner revisions plus small observer keys match the engine's mutation model.

## TDD coverage by functionality

The user explicitly requested the existing Sol 6.1 TDD workflow. Tests protect
public demand, readiness/lifecycle behavior, and operational work contracts;
avoid private cache layouts, exact helper order, map identities, timing limits,
or allocation thresholds.

- Idle and within-cell movement: identical demanded memberships, no further
  radius-volume visits/evaluations after warmup, with frame progress still active.
- Shells and teleports: compare every public demand set against independently
  enumerated expected cubes across axis/diagonal/negative movement, larger
  jumps, radius/default changes, and chunk-size changes. Small moves visit
  boundary work rather than complete unchanged volumes; do not assert a
  particular slab decomposition or exact visit count.
- Multiple observers: overlapping/disjoint visual, keep, collision, destruction,
  and PVS-expanded demands survive movement/removal of one observer; removal of
  the final observer clears its demand at the existing flush boundary.
- Imported/content policy: current PVS/adjacency, hidden-sector filtering,
  non-imported placement/terrain exceptions, full-sector expansion, backing,
  proxy fallback, and proxy enablement preserve existing selection semantics.
- Invalidation: in-place visibility/membership/content changes with stationary
  observers take effect after the declared invalidation boundary; lifecycle and
  generation changes do not reuse obsolete selections.
- Progress: idle observers still schedule after worker slots/byte credits free,
  apply readiness transitions, retire/unload content, and honor collision and
  destruction upgrades. Reuse existing S1/S2 integration fixtures where useful.
- Transient demand: startup collision/destruction additions and temporary proxy
  handoff pins do not contaminate base demand. Startup additions clear on later
  idle updates while true observer demand remains. Under current global fallback
  policy every valid proxy pin already belongs to base fallback; verify proxy
  memberships clear when proxies are disabled instead of inventing an idle-only
  extra proxy membership that the current policy cannot produce.
- Lifecycle: Stop/Restart releases selection ownership and a new session derives
  its own metadata/demand. Verify resumability if exercising failed Stop.

## Expected changes and verification

Files: `streamed_level_runtime.go`, a focused `streamed_level_selection.go`,
focused S3a tests, this roadmap, parent roadmap status, and canonical
`docs/content/streaming-and-worlds.md`. New field/API declarations may make the
first test-only RED a compile failure; production begins only after root reviews
test contracts. Then require functional GREEN and adversarial code review.

Run focused S3a tests, existing streamed/navigation/render-residency tests, engine
root tests, focused race checks, and affected ActionGame checks/build. Selection
does not change shaders, uploads, or GPU layouts; automated render-readiness
fixtures verify continued handoff. Use a native smoke only if review exposes a
remaining visual coverage risk that those fixtures cannot resolve.

## Execution record

### Tests and design review

- Root extracted this scope and reviewed the existing selection, renderer,
  content, and lifecycle owners before production edits.
- Sol 6.1 design review clarified global observer union before filtering,
  terrain invalidation at partial-persistence boundaries, and pre/post-teardown
  failure semantics. These findings are reflected above.
- Sol 6.1 wrote eleven functional tests in
  `streamed_level_selection_s3a_test.go`; existing tests remained unchanged.
- Root and an independent Sol 6.1 reviewer inspected the test contracts.
  Review found a fixture assumption: the empty level uses 32-unit chunks, so
  shell moves now derive from `state.ChunkSize`. A second review tightened the
  work bound: five equal radii could otherwise share one full cube and still
  pass. Radius-20 unit moves must visit fewer coordinates than one complete
  unique cube, while allowing independent shell traversal for all five radii.
  The author repeated RED after these contract corrections.
- RED command: `env GOCACHE=/tmp/gekko3d-gocache go test . -run
  '^TestS3aSelection' -count=1`. Exit 1 from the missing public selection metric
  declarations; functional assertions could not execute at this phase.
  Local log: `/tmp/gekko-s3a-red.log`.
- The first implementation run passed nine tests; two stopped before selection
  because the imported fixture left three authored chunks without sector
  references. The test author added valid startup references, then trimmed the
  test's runtime references at its declared metadata mutation boundary. Root
  checked that the raw-coordinate/global-filtering assertion still cannot be
  satisfied by full-sector expansion. All eleven focused tests then passed;
  local log: `/tmp/gekko-s3a-fixture-green.log`.

Automatic terrain override publication/removal invalidation is an explicit
production-review gate. The focused tests do not independently exercise those
mutation hooks after cache warmup.

### Implementation and adversarial review

Sol 6.1 implemented the approved scope with tests frozen. The selection owner
uses global overlap counts, three per-observer sector-volume counts, cached
visibility/full-sector/fallback derivations, and disjoint cube differences.
Changed selection republishes aggregates; idle selection scans observers and
tracked temporary additions without rebuilding radius volumes. Aggregate
publication can still traverse the current demand sets after an input change;
the shell counters do not measure all streaming work.

Root and an independent Sol 6.1 reviewer checked counts, global filtering,
sector policy, slabs, transient restoration, ECS boundaries, worker isolation,
and teardown. No actionable implementation finding remained. Both confirmed
terrain override invalidation immediately after publication/removal, before
later persistence failures. Start and completed teardown release the owner;
pre-teardown failure retains it. Collision/destruction and navigation ownership
remain independent of GPU readiness.

Canonical docs and parent roadmap now describe the implemented API, read-only
demand views, invalidation boundary, counters, and memory limits. S3 scene
gathering and future layer selection remain separate work. Remaining S2 owner
budgets, work queues, and decode cancellation are unchanged.

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
`/tmp/gekko-s3a-actiongame-build.log`. Race emitted the existing macOS
`LC_DYSYMTAB` linker warning; ActionGame build emitted a module stat-cache
permission warning. Both exited 0.

No native/GPU smoke was run. This slice changes selection ownership and preserves
the existing renderer interface; headless readiness/cohort fixtures cover
continued handoff. No rendered pixel parity or measured speedup is claimed.
Automatic terrain invalidation and late cleanup-error release were verified by
code review rather than new dedicated failure-injection tests.
