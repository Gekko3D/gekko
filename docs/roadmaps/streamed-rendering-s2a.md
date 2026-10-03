# S2a: Byte-budget prepared geometry cache

Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S2.
Prerequisite: [S1c](streamed-rendering-s1c.md).

## Scope, owner and confidence

Owner: existing streamed prepared geometry cache. Consumers: imported full chunks, sector proxies, AssetServer and renderer source resolution. Long-term ownership step within existing runtime.

Known: immutable prepared maps; separate deep AssetServer copy; live chunks/proxies acquire cache references. Entry eviction does not bound bytes; concurrent misses duplicate builds; Stop leaves warm assets owned by discarded cache. Renderer retention has separate owner/budget.

Unknown: scene-dependent tuning and total process memory. Exclude decoded loader content, pending results, private editable/collision maps, navigation snapshots and GPU retention. No heap/RSS ceiling or S2 completion claim; no compression/collision changes. Preserve every acquired user, including hidden fallbacks and collision-bearing imported chunks.

Confidence: high after prepare, registration, unload, Stop, stages and XBrickMap copy inspection. No SME alignment needed. Entry-only eviction cannot bound heterogeneous geometry; new residency service duplicates cache/AssetServer ownership. Extend existing owners.

Files: `streamed_level_geometry_cache.go`, focused storage accounting if useful, `streamed_level_runtime.go`, new functional tests, roadmap and canonical streaming/runtime-assets docs. No shader, format, RuntimeContentLoader or pending-queue changes.

## Budget and storage charge

Add `StreamedLevelRuntimeConfig.MaxPreparedGeometryCacheBytes int64`. Zero: 128 MiB; negative: disable warm retention. Entry ceiling remains secondary: zero: 256; negative: disable warm retention. Initial defaults, not measured optima. Preserve one-argument cache construction; optional private byte argument follows zero/default and negative/disabled semantics.

Charge XBrickMap/Sector/Brick structs, logical sector/revision/dirty entries, packed-brick pointer capacity and auxiliary byte capacity. Uniform GPU compression still allocates dense CPU payload. Charge actual registered copies, not assumed double size. Exclude borrowed GPU-manager objects.

This is admission-time estimated storage charge: Go map bucket slack,
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
delete asset because one of several users unloads.
Remember each asset's registering server: cleanup must use that owner even if
cache operation receives different server argument. App resource replacement
is unsupported; runtime tests must not invent it by mutating resource plumbing.

Result larger than byte ceiling is returned to its caller without warm
prepared retention. Concurrent callers still share its one build. Asset
registration may make previously fitting prepared entry oversized: retain it
while acquired, then delete its asset and prepared ownership on final
release. Disabled retention follows same lease cleanup, with no warm hits.
Empty-key registration must also release its owned asset; it cannot leak or
share unrelated empty-key geometry. Track acquired asset IDs in runtime load
records if needed; preserve existing keyed release seam for current tests.
Nil-cache/optional-runtime paths must retain their existing usable behavior.

Ceiling is soft only for acquired users and deferred main-thread deletion.
Expose that pressure explicitly. Newly prepared unpinned result cannot keep
cache above its ceiling merely because it was latest insertion.
Workers may evict prepared-only entries, but never mutate AssetServer/ECS. If
true LRU victim owns asset, defer that eviction to engine thread;
do not discard newer prepared result merely to avoid pending deletion.
Provide `trim(assets)` on engine thread and invoke it each streaming frame,
even when no further chunk commits occur. Deferred warm asset victims must be
trimmed without requiring later acquire/release. Once only unpinned storage
remains and main-thread maintenance runs, charge must fit both ceilings.

After successful Stop joins/drains workers and removes entities, close cache and delete cache-owned assets; preserve unrelated AssetServer assets. Start creates fresh cache/budget. Close prevents outstanding builds repopulating abandoned cache; Stop already joins before close. Persistence failure before teardown must retain live ownership. Preserve S1c renderer-retention/ticket retirement order. Successful Stop immediately publishes zero cache ownership metrics.

## Per-key build suppression

Install per-key in-flight record under mutex; build outside lock. Waiters share result, including oversized uncached results; distinct keys build concurrently. Nil results/builders create no entry and allow retry. Panic wakes waiters, removes record and propagates; later retry remains possible. No global lock during build/wait. Blank keys/nil cache bypass suppression; disabled warm retention may coalesce overlapping builds.

## Functionality tests and review checklist

User authorizes test-first workflow and GPT-6.1 sol subagents. Add tests only; preserve existing tests. Assert results, public metrics, asset lookup, live geometry/collision usability and cleanup, not private containers.

1. Config defaults/disable and byte metrics; aux/pointer capacity affects charge;
   uniform CPU payload still costs storage; deep asset copy adds its actual cost.
2. Heterogeneous geometry evicts by bytes below entry limit; exact boundary
   fits; genuine LRU hits/releases choose victim. Shared maps/sectors/bricks
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
   collision contracts under tiny budget. Successful Stop removes cache assets
   and bytes, keeps unrelated assets; restart honors new config. Persistence
   failure retains still-active world's assets. Close during blocked build
   cannot repopulate abandoned ownership.

Root adversarially reviews RED; GPT-6.1 sol implements. Review GREEN for lifetime gaps, hidden assumptions, lock order, duplicate charges, worker asset mutation and hot-path cost. Concrete contract gaps repeat RED review loop.

## Verification

- Baseline: existing cache reuse/entry eviction and S1c render residency tests.
- New functional tests, existing streamed runtime/renderer bridge tests.
- Targeted race run for cache concurrency; volume and renderer packages.
- Engine sweep: distinguish two recorded pre-existing runtime failures.
- Compile directly affected actiongame, editor and testing-vox consumers.
- Native GPU smoke only if changes affect renderer/ticket ordering or source
  lifetime beyond existing owner contract; CPU tests cannot certify pixels.

## Execution record

GPT-6.1 sol implemented after root scope/design and adversarial test review. Nineteen new test functions protect contracts; existing tests unchanged. Initial RED: missing budget API. Root fixed new fixture pointer-method expression and rejected unsupported App resource-swap fixture. Original-server cleanup remains covered at cache boundary.

Post-GREEN review: warm reacquisition omitted registered-copy pin. New sol regression reached RED: 2672 total bytes, only 1336 pinned. After review, sol fixed zero-to-one lease transition. Warm reloads pin both graphs; balanced release returns zero pinned bytes. Final review: alias removal, source ID cleanup, lock/channel publication, worker/main-thread deletion and hot-path cost. Snapshot reads totals; maintenance scans entry metadata only.

Passed, from `gekko/`, with `GOCACHE=/tmp/gekko3d-gocache`:

```sh
go test . -run '^TestS2a' -count=1
go test . -run '^Test(Streamed|StartStreamed|RuntimeContentLoader|VoxelRt|VoxelRT|VoxelRender)' -skip '^TestStreamedNavigationPublishesResidencyWhileOverlayMoves$' -count=1
go test -race . -run '^TestS2aPreparedCache' -count=1
git diff --check
```

`go test ./...` passed every other engine package and failed only two
previously recorded root-package failures:

- `TestMovingBrushCarriesSupportedPlayerAndNPC`: player `[0 0.1 0]`, missing carry.
- `TestStreamedNavigationPublishesResidencyWhileOverlayMoves`: pending=2,
  loaded=1, load=false, overlay=true.

Consumer builds passed with same cache environment, from each module:

```sh
# actiongame/
go build -o /tmp/gekko-s2a-actiongame .
# gekko-editor/
go build -o /tmp/gekko-s2a-editor .
# examples/testing-vox/
go build -o /tmp/gekko-s2a-testing-vox .
```

Actiongame/editor emitted sandbox stat-cache warnings but exited zero. Race
linking emitted native linker warning; race execution passed.

Native GPU smoke: saved v2 content, **one-byte / one-entry cache**, real tickets and bounded uploads. Verified four-second paused fallback, partial full readiness with both children hidden, atomic refinement, far-observer coarsening, source asset deletion/zero cache bytes on Stop and two seconds post-Stop renderer cleanup. Final: **417 frames, 14.310 seconds, exit 0**. Establishes native source lifetime/handoff, not pixel parity or performance. No screenshot captured before exit.

Diagnostic artifacts: `/tmp/gekko-s2a-smoke.go`,
`/tmp/GekkoS2aSmoke.app`, `/tmp/gekko-s2a-native-smoke.log`,
`/tmp/gekko-s2a-engine-sweep.log`. No diagnostic assets entered repository.

Next: S2b decoded-content/pending byte bounds and per-key loading. GPU retention, private/editable geometry and cross-owner memory accounting remain separate S2 work. No codec or collision representation change.

## S2c: CPU material-table cache ownership decision

Extend the existing `VoxelRtState.materialTableCache`, preserving fingerprint,
table construction and active sharing. Charge each retained key/table backing
once plus conservative entry metadata. Default retention budget: 16 MiB, an
initial unmeasured setting. Main-thread budget configuration uses zero for the
default and negative for disabled warm retention, matching prepared-cache policy.

After complete instance sync, pin distinct keys referenced by current objects,
including hidden streamed objects. Refresh active usage and evict inactive LRU
entries until total charge fits. Apply changed budgets at this boundary even
without new table builds. If pinned tables alone exceed the budget, retain them
and expose pinned pressure. Temporary builds and retained external references are
outside this cache-owned accounting; no process/GPU-memory ceiling is claimed.

Eviction removes cache references only. Never clear or recycle material backing:
object, renderer and caller-held slices remain valid. Rebuild pruned maps and
release empty cache ownership. Exact ties need no deterministic admission promise.
Public value stats report entries, bytes, pinned bytes, effective maximum, pressure,
builds, hits and evictions; private accounting controls retention.

Hard eviction of active keys can duplicate shared tables on new-object admission.
Pinning follows S2's existing live-user policy while bounding historical phases.
Hash semantics, mutable asset compatibility, streaming readiness, GPU allocation
and table mutation behavior remain unchanged. Use the full cache-ownership workflow;
canonical behavior belongs in renderer runtime docs.

## S2d: Retained GPU map byte ownership decision

Extend the existing `GpuBufferManager` retained-map owner with
`RetainedVoxelMapBudgetBytes int64`. The constructor selects
`DefaultRetainedVoxelMapBudgetBytes` (128 MiB, initially unmeasured). Nonpositive
values disable this cap independently of the existing sector cap. Keep legacy
sector configuration and request/hit/miss/eviction semantics.

Charge each retained map once: 256 bytes of retention-entry metadata, assigned
32-byte sector records, assigned 64-record brick-table blocks, and actual assigned
auxiliary/payload slots. Use allocation snapshots and slot mappings, not mutable
CPU sector/brick counts. Uniform bricks without payload slots cost no payload
bytes. Unallocated/empty retained maps still have metadata charge. Exact shared
`XBrickMap` users reuse one allocation and entry. Sharing nested sector/brick
pointers across distinct maps gains no new ownership guarantee.

During complete voxel updates, pin retained maps present in all `Scene.Objects`,
including hidden staged uploads. Refresh active use without incrementing cache
hits. Evict inactive LRU entries when either enabled cap is exceeded; preserve
active excess and expose byte pressure. Trim before orphan cleanup to reclaim
slots before uploads, and after uploads to account newly assigned active slots.
Reuse the existing allocation release path and rebuild pruned retention maps.

Add value stats `Bytes`, `PinnedBytes`, `MaxBytes` and `PressureBytes`; private
ownership controls policy. Stats reads never trim or advance work counters.
`MaxBytes` follows current configuration; pins describe the last maintenance
boundary. Disabled byte caps report zero maximum
and pressure. Configuration applies at the next update. Retention eviction does
not shrink GPU buffers or atlas pages. Buffer headroom/free capacity, lookup and
object/material buffers, CPU geometry/snapshots and temporary accounting are
excluded. This is an assigned-slot retention budget, not a VRAM/process ceiling.

Capture private per-entry charges on retain. Structure processing and geometry
uploads invalidate the touched map's charge; completed maintenance refreshes it
once using exact assigned-slot deduplication. Inactive and unchanged active maps
reuse their charge. Stats observe completed manager assignments through scalar
accounting; arbitrary direct writes to GPU plumbing maps are not tracked producers.
This avoids rescanning all warm bricks and allocating accounting sets on reads.

This additive owner policy follows S2 live-user pinning. Capping physical buffer
capacity would require different allocation/admission architecture. Shader layouts,
normal bytes, upload readiness, collision and content formats remain unchanged.
Use separate test/implementation agents and independent pre/post reviews.

## S2f: Direct unpinned eviction order

Status: implemented 2026-10-03; focused/race, full engine and five consumer checks
passed. See the [delivery record](streamed-rendering-content-optimization.md#s2f-direct-prepared-cache-eviction-order).

Scope: replace the prepared cache's full-owner scan per eviction with a private
ordered list of unpinned entries under its existing mutex. This is an extension
of S2a's owner, byte charge and LRU contract; no new residency service or budget.
Confidence is High after inspecting admission, hits, acquisition, release, worker
builds and terminal cleanup. No human architecture choice is required.

Admission, unpinned hits and final releases append/refresh the entry as newest.
Acquisition removes it from eviction eligibility; removal and close unlink it.
Select only the oldest unpinned entry. Workers must defer when that exact victim
owns an asset, without skipping it to discard newer prepared data. Main-thread
trim deletes through the original registering server. Disabled, oversized,
empty-key and multiple-lease paths retain their existing lifetime rules.
A clock/heap or repeated scan adds work without changing this exact policy.

Files: `streamed_level_geometry_cache.go`, runtime metrics and focused coverage.
Expose cumulative `PreparedGeometryCacheEvictionCandidateVisits` (cache stats
`EvictionCandidateVisits`): each nonnil victim examined under pressure counts,
including a worker-deferred victim. Reads and no-pressure maintenance do not
advance it. Selection examines at most one candidate per eviction or deferral;
removal still traverses the immutable storage ledger as required by S2a.

Freeze minimal functionality coverage for multi-victim byte pressure, live pins,
real hit/final-release ordering and worker deferral; reuse existing S2a lifetime
and concurrency tests. Use separate test/implementation agents, independent
PRE/POST reviews, focused race checks and engine/consumer boundary verification.
Existing readiness, collision, formats and renderer retention remain unchanged.
