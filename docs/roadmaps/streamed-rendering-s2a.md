# S2a: Byte-budget prepared geometry cache

Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S2.
Prerequisite: [S1c](streamed-rendering-s1c.md).

## Scope, owner and confidence

Primary owner: streamed runtime's existing prepared geometry cache. Consumers:
imported full chunks, sector proxies, AssetServer, renderer source resolution.
This is a long-term cache ownership step in the existing runtime.

Known: prepared maps are immutable; AssetServer registration makes a separate
deep copy; live chunks/proxies acquire cache references. Current eviction counts
entries, concurrent misses duplicate work, and Stop leaves warm assets owned by
the discarded cache. Renderer retention has a separate owner and budget.

Unknown: practical scene-dependent budget tuning and total process memory.
Decoded loader content, pending worker results, private editable/collision maps,
navigation snapshots and GPU-retained data are outside this slice. Do not claim
a heap/RSS ceiling or S2 completion. Do not change compression or collision
algorithms. Cache eviction must preserve every acquired user, including hidden
fallbacks and collision-bearing imported chunks.

Confidence: high after tracing prepare, registration, unload, Stop, runtime
stages and XBrickMap copy paths. No SME alignment needed. Retaining entry-only
eviction cannot bound heterogeneous geometry. A new global residency service
would duplicate documented cache/AssetServer ownership. Extend those owners.

Files: `streamed_level_geometry_cache.go`, a focused cache storage-accounting
file if useful, `streamed_level_runtime.go`, new functional test files, this
roadmap and canonical streaming/runtime-assets docs. No shader, content format,
RuntimeContentLoader or pending-queue changes.

## Budget and storage charge

Add `StreamedLevelRuntimeConfig.MaxPreparedGeometryCacheBytes int64`.
Zero selects 128 MiB; negative disables warm retention. Existing entry config
remains a secondary ceiling: zero selects 256, negative disables warm retention.
These defaults are initial policy, not measured scene optima. Preserve existing
one-argument cache construction; an optional byte argument is a private test and
owner seam, with zero/default and negative/disabled semantics.

Charge CPU geometry storage: XBrickMap/Sector/Brick structs, logical entries in
sector/revision/dirty maps, packed-brick pointer capacity, and auxiliary byte
capacity. Dense brick payload is allocated even when GPU material compression
marks a brick uniform. Count actual registered copies rather than assuming they
cost exactly twice the prepared map. Borrowed GPU-manager objects are excluded.

This is an admission-time estimated storage charge: Go map bucket slack,
allocator overhead, cache bookkeeping, key strings, later renderer dirty-map
churn and allocations held only by other owners are excluded. Arbitrary
manually overlapping slice backings are conservatively
charged; production prepared/copy paths do not create them. Charge shared map,
sector and brick objects once across keys, including immutable derivatives.
Keep accounting incremental on immutable object admission/removal; per-frame
metrics must not rescan every voxel payload. Expose total, prepared, asset-copy,
pinned, budget and over-budget bytes, plus coalesced-build waits and oversized
bypasses. Byte arithmetic uses int64.

## Admission, eviction and asset lifetime

Enforce both byte and entry ceilings by evicting least-recently-used entries
with no acquired users. Hits and releases refresh LRU. Protect every live lease;
multiple leases on one key share one asset and balance independently. Never
delete an asset because one of several users unloads.
Remember each asset's registering server: cleanup must use that owner even if a
cache operation receives a different server argument. App resource replacement
is unsupported; runtime tests must not invent it by mutating resource plumbing.

A result larger than the byte ceiling is returned to its caller without warm
prepared retention. Concurrent callers still share its one build. Asset
registration may make a previously fitting prepared entry oversized: retain it
while acquired, then delete its asset and prepared ownership on the final
release. Disabled retention follows the same lease cleanup, with no warm hits.
Empty-key registration must also release its owned asset; it cannot leak or
share unrelated empty-key geometry. Track acquired asset IDs in runtime load
records if needed; preserve the existing keyed release seam for current tests.
Nil-cache/optional-runtime paths must retain their existing usable behavior.

The ceiling is soft only for acquired users and deferred main-thread deletion.
Expose that pressure explicitly. A newly prepared unpinned result cannot keep
the cache above its ceiling merely because it was the latest insertion.
Workers may evict prepared-only entries, but never mutate AssetServer/ECS. If
the true LRU victim owns an asset, defer that eviction to the engine thread;
do not discard a newer prepared result merely to avoid the pending deletion.
Provide `trim(assets)` on the engine thread and invoke it each streaming frame,
even when no further chunk commits occur. Deferred warm asset victims must be
trimmed without requiring a later acquire/release. Once only unpinned storage
remains and main-thread maintenance runs, charge must fit both ceilings.

After successful Stop joins/drains workers and removes resident entities, close
the cache and delete all cache-owned assets, preserving unrelated AssetServer
assets. Start uses a fresh cache/budget. Close prevents an outstanding build from
repopulating the old cache; runtime Stop already joins workers before closing.
Do not clear live cache ownership when persistence fails before teardown.
Keep renderer-retention and ticket retirement ordering from S1c intact.
Successful Stop immediately publishes zero cache ownership in public metrics.

## Per-key build suppression

Install an in-flight record under the cache mutex; execute builders outside it.
Same-key waiters receive the same result, including an oversized uncached result.
Distinct keys build concurrently. Nil results and nil builders create no cached
entry; a later request can retry. Panicking builders must wake waiters, remove
the in-flight record and propagate the panic, allowing a later retry. No global
mutex may remain locked during build/wait. Blank keys and nil cache bypass build
suppression. Disabled warm retention may still coalesce overlapping keyed builds.

## Functionality tests and review checklist

User authorizes the test-first workflow and GPT-6.1 sol subagents. Add new tests
only; preserve existing tests. Assert cache results, public metrics, asset lookup,
runtime entity geometry/collision usability and cleanup, not private containers.

1. Config defaults/disable and byte metrics; aux/pointer capacity affects charge;
   uniform CPU payload still costs storage; deep asset copy adds its actual cost.
2. Heterogeneous geometry evicts by bytes below the entry limit; exact boundary
   fits; genuine LRU hits/releases choose the victim. Shared maps/sectors/bricks
   are charged once; distinct asset copies are separate.
3. Acquired users survive pressure, two leases release independently; pressure
   clears on final release. Oversized build bypass and post-registration
   oversize do not persist warm memory. Disabled/empty-key/nil-cache paths do not
   leak registered assets. Repeated travel remains bounded after maintenance.
4. Worker-built pressure defers asset deletion until `trim(assets)`; actual
   runtime maintenance trims even when no chunk commits occur.
5. Barrier-controlled concurrent same-key callers build once; distinct keys make
   progress concurrently. Nil/panic paths wake waiters and permit retry. Avoid
   timing sleeps; bounded timeouts serve only as deadlock guards.
6. Actual imported full/proxy commit/unload preserve live geometry and CPU
   collision contracts under a tiny budget. Successful Stop removes cache assets
   and bytes, keeps unrelated assets; restart honors new config. Persistence
   failure retains the still-active world's assets. Close during a blocked build
   cannot repopulate abandoned ownership.

Root reviews RED tests adversarially, then delegates production to GPT-6.1 sol.
Review GREEN code for missing lifetime paths, hidden assumptions, lock order,
duplicate physical charges, worker-side asset mutation and hot-path cost. Add
tests for concrete contract gaps through the same RED review loop.

## Verification

- Baseline: existing cache reuse/entry eviction and S1c render residency tests.
- New functional tests, existing streamed runtime/renderer bridge tests.
- Targeted race run for cache concurrency; volume and renderer packages.
- Engine sweep: distinguish the two recorded pre-existing runtime failures.
- Compile directly affected actiongame, editor and testing-vox consumers.
- Native GPU smoke only if changes affect renderer/ticket ordering or source
  lifetime beyond the existing owner contract; CPU tests cannot certify pixels.

## Execution record

Implemented by GPT-6.1 sol after root's scope/design and adversarial test review.
Nineteen new functional test functions protect the contracts above. Existing
tests were unchanged. Initial RED lacked the new budget API; root corrected a
new fixture's pointer-method expression and rejected an unsupported App
resource-swap fixture rather than changing resource registration semantics.
Original-server cleanup remains covered at the cache boundary.

Post-GREEN review found warm asset reacquisition omitted the registered copy's
pin. A new sol-written regression reached behavioral RED: 2672 total bytes but
only 1336 pinned. After test review, sol fixed the zero-to-one lease transition.
Repeated warm reloads now pin both graphs and return to zero pinned bytes after
balanced releases. Final review checked shared alias removal, source ID cleanup,
lock/channel publication, worker/main-thread deletion separation and hot-path
cost. Snapshot uses accumulated totals; maintenance scans entry metadata only.

Passed, from `gekko/`, with `GOCACHE=/tmp/gekko3d-gocache`:

```sh
go test . -run '^TestS2a' -count=1
go test . -run '^Test(Streamed|StartStreamed|RuntimeContentLoader|VoxelRt|VoxelRT|VoxelRender)' -skip '^TestStreamedNavigationPublishesResidencyWhileOverlayMoves$' -count=1
go test -race . -run '^TestS2aPreparedCache' -count=1
git diff --check
```

`go test ./...` passed every other engine package and failed only the two
previously recorded root-package failures:

- `TestMovingBrushCarriesSupportedPlayerAndNPC`: player `[0 0.1 0]`, missing carry.
- `TestStreamedNavigationPublishesResidencyWhileOverlayMoves`: pending=2,
  loaded=1, load=false, overlay=true.

Consumer builds passed with the same cache environment, from each module:

```sh
# actiongame/
go build -o /tmp/gekko-s2a-actiongame .
# gekko-editor/
go build -o /tmp/gekko-s2a-editor .
# examples/testing-vox/
go build -o /tmp/gekko-s2a-testing-vox .
```

Actiongame/editor emitted sandbox stat-cache warnings but exited zero. Race
linking emitted a native linker warning; race execution passed.

Native GPU smoke used saved v2 world content, a **one-byte / one-entry cache**,
real renderer tickets and the existing bounded upload path. It verified a
four-second paused-upload fallback hold, partial full readiness with both
children hidden, atomic full refinement, observer-far coarsening, source asset
deletion on Stop, zero public cache bytes and two seconds of post-Stop renderer
cleanup. Final run: **417 frames, 14.310 seconds, exit 0**. This verifies source
lifetime and handoff on the real GPU; it does not establish pixel parity or a
performance improvement. No screenshot was captured before the run exited.

Diagnostic artifacts: `/tmp/gekko-s2a-smoke.go`,
`/tmp/GekkoS2aSmoke.app`, `/tmp/gekko-s2a-native-smoke.log`,
`/tmp/gekko-s2a-engine-sweep.log`. No diagnostic assets entered the repository.

Next slice: S2b, byte bounds and per-key loading for decoded content and pending
results. GPU retention, private/editable geometry and global cross-owner memory
accounting still require their own S2 work. This slice adds no content codec or
collision representation change.
