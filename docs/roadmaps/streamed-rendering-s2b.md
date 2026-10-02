# S2b: Bound decoded content and pending prepared results

Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S2.
Prerequisite: [S2a](streamed-rendering-s2a.md).

## Scope and confidence

Owners: RuntimeContentLoader and full/proxy preparation queues. Consumers: world metadata/backing, chunk/proxy preparation, synchronous collision startup and navigation source batches. Long-term ownership step at current runtime boundaries.

Known: eight unbounded decoded maps; duplicate concurrent decodes; raw-path keys. Full/proxy channels each hold 256 heterogeneous results. Commit/discard/drain lack release handles. Geometry preparation already has byte budget. Commit copies into geometry/heightmaps; entities retain no decoded chunk/aux records. Level/manifests/backing and shallow metadata retain decoded data for session; navigation batches retain it through baking.

Unknown: scene tuning and temporary decode/build allocation peaks. Public Load pointers declare no lifetime; external callers may retain/normalize after eviction. Charge cannot track borrowers or promise total process memory. Never reuse buffers or mutate evicted definitions.

Confidence: High after consumer and Stop/error inspection. No SME alignment required. Entry-only limits cannot bound heterogeneous data; new residency service duplicates owners. Add leases/admission to existing loader/queues. Byte-credit wait retaining completed payload defeats bounds; release payload and publish small retry completion.

Files: runtime_content_loader.go, focused content charge/cache, streamed runtime/pending admission, navigation_graph_runtime.go, new functional tests, roadmap and canonical streaming/runtime-assets docs. No codec, collision algorithm, shader, GPU residency budget or navigation publication changes. Separate IO/navigation queues, mid-decode cancellation and physical backing accounting remain S2 work.

## Decoded loader contract

Preserve NewRuntimeContentLoader() and all eight Load methods, including nil
receiver passthrough. Add optional RuntimeContentLoaderOptions{MaxCacheBytes}:
zero selects 128 MiB; negative disables warm retention. Add Stats() returning
RuntimeContentLoaderStats with Entries, Bytes, PinnedBytes, MaxBytes,
OverBudgetBytes, Hits, Misses, Evictions, LoadWaits and OversizedBypasses.

One budget/LRU covers all content kinds. Identity includes content kind and
lexically cleaned absolute path; relative aliases coalesce. Do not resolve
symlinks or require cache-hit file to still exist. Type kinds stay separate.
Charge decoded graph storage at admission: struct sizes, slice capacity,
nested pointers/slices/maps, string bytes and logical map entries. Deduplicate
identical backing identities within graph; conservatively charge arbitrary
overlapping slice/string views. Skip element traversal when their types contain
no referenced storage. RLE payload size is not decoded-memory estimate.
Exclude allocator/map-bucket/cache bookkeeping, decoder temporary buffers,
derived backing/navigation indexes and separate prepared geometry. This is
admission-time estimate, since public definitions can be normalized by callers.

Unpinned LRU entries must fit byte ceiling. Oversized/disabled warm results
remain usable and may still share concurrent decode. Add NewScope(), returning
RuntimeContentLoadScope with Loader() and idempotent Close(). Its derived
loader shares cache and pins every loaded entry atomically on publication.
Repeated loads in one scope acquire one pin; distinct scopes balance separately.
Pinned bytes count each entry once, even with multiple scopes. Pins may exceed
ceiling, reported as pressure; final release trims unpinned storage. Closing
scope during decode must prevent late pinning; loads on closed scopes fail.
If all scoped requesters close before decode completes and no raw requester
remains, completion must not create warm ownership on their behalf. Other live
requesters still receive and retain shared result normally.
Returned pointers remain valid after release/eviction. Clear() drops unpinned
warm ownership and preserves active scopes; it must not let earlier in-flight
raw load repopulate warm ownership without new request. Active scope may
still acquire its in-flight result; that declared lease remains protected.

Per kind/path singleflight decodes outside mutex. Same-key waiters share success, including uncached results; different keys progress independently. Errors/nil results are uncached. Panic wakes waiters and allows retry. No locks across decode/wait; no lost waiter pins.

## Runtime lease lifetimes

MaxDecodedContentCacheBytes config applies to runtime-created loader; supplied Loader retains own options. Metadata scope leases level, terrain/world manifests and voxel backing for session. Publish scope with runtime ownership. Pre-publication failures close scope; published partial sessions retain it until successful Stop. State.Loader remains base loader; workers cannot accidentally join metadata scope.

Each full/proxy preparation uses its own scope, transferred with its result and
closed after commit or every discard/drain path. Synchronous collision startup
uses transient scope through immediate commit. Committed geometry/heightmaps
stay valid after decoded leases release. Sidecar rejected by version/hash/
source metadata releases its unused preparation lease immediately; it cannot
hide behind release handle excluded from pending payload charge. Other scopes
on same sidecar remain protected. Navigation uses batch scope through
source loading and baking, releasing it before blocking result publication;
errors release it too. Navigation snapshots already owned elsewhere stay with
their current owner.

Successful Stop joins/drains workers, removes entities, clears shallow metadata, then releases metadata leases. Clear runtime-created warm ownership; preserve supplied/shared loader users. Persistence failure retains usable generation/metadata. Restart gets fresh credits; stale results cannot clear new generation pending markers. Preserve S1c ticket retirement/fallback ordering.

## Pending full/proxy admission

Add MaxPendingPreparedBytes: zero selects 128 MiB, negative is invalid. One
thread-safe owner covers both channels for session. Preserve public channel
types, entry capacities, existing count/time commit budgets and optional result
handles so existing synthetic channel fixtures remain usable.

After preparation, charge result's decoded records/aux, snapshot and
placement graphs, geometry and envelope/key strings. Deduplicate within
result. This is per-result admission cost; separate results and loader/cache
owners may conservatively charge shared data again. Do not add these metrics
together as physical memory. No estimator may follow borrowed GPU managers,
cache owners, errors or release handles. Reuse geometry storage estimator
through temporary ledger rather than retaining roots in live cache ledger.

Admit fitting results by reserving bytes before channel publication. Credit
covers blocked publication and queued payload. If it cannot fit, release
decoded scope and all large payload references; publish only small retry
completion carrying generation/coordinate/required cost. Keep cost hint so
observer waits for sufficient capacity before rebuilding that demand.
Clear hints when demand/session ends. Single oversized payload may be admitted
when no other charged payload is outstanding, preventing valid large pages from
starving. Report over-budget/oversized admission explicitly. Every consumption
branch and Stop drain releases credit exactly once. Retry metadata is bounded
by existing channel/job counts and excluded from payload byte charge.

Active decode/build is outside retained-result ceiling, bounded by MaxPrepareJobs. No hard decode allocation or RSS limit claimed. Main thread safely discards obsolete demand; IO cancellation and queue partitioning remain later S2 work.

Expose decoded/pending bytes, budget and pressure plus admission retry/oversized counts in StreamedLevelRuntimeMetrics. Read totals without per-frame payload rescans. S1f prepared-depth metrics include transport and retained ready results; credits and scopes survive both owners until consumption or drain. Commit-count semantics remain unchanged.

Metric prefixes: DecodedContentCache with loader Stats suffixes above;
PendingPrepared with Bytes, MaxBytes, OverBudgetBytes, AdmissionRetries and
OversizedAdmissions.

## Functionality tests and adversarial review

Use user-authorized GPT-6.1 sol RED/review/GREEN/review/commit workflow. Add tests; preserve existing files. Protect observable loader, budget, lease and runtime contracts; avoid private layout/helper order. Relative fixture charges avoid ABI assumptions.

- All eight content kinds share byte ceiling; nested aux/backing/asset/level
  storage and decoded RLE data contribute. LRU, exact boundary, disabled warm
  retention, oversized bypass and returned-pointer survival after eviction.
- Path aliases, deleted-file cache hits, kind separation, nil compatibility;
  same-key concurrent success/error/nil/panic and independent-key progress.
- Scope shared pins, balanced releases, close during decode, cleared in-flight
  loads, active pointers/queries surviving eviction pressure.
- Shared full/proxy result admission, bytes during blocked publication, sole
  oversized progress, deferred retry without repeated rebuild under pressure,
  release on commit/error/obsolete/duplicate/stale/drain paths.
- World metadata/backing lifetime, transient chunk leases after commit,
  supplied-loader options and external scope survival, Stop/restart and failed
  persistence preserving usable world. Navigation source batch lifetime.
- Rejected auxiliary sidecars release their unused leases before publication,
  retaining fallback geometry, chunk/terrain leases and independently scoped
  sidecar users.

Root reviews ownership branches, fixture validity, deterministic concurrency and architecture before delegation. Review GREEN for scope/credit leaks, lock order, idle-worker accounting, stale marker deletion, retries, Stop failures and scans. Contract gaps repeat RED and review.

## Verification

Owner baseline passed. Verify new/existing streamed, loader and renderer contracts, focused races and engine sweep; compare two recorded root failures. Build actiongame/editor/voxel consumers. Native smoke with small loader/pending budgets checks handoff/Stop lifetime; CPU tests cannot certify pixels.

## Execution record

GPT-6.1 sol implemented after narrow design and adversarial RED review. Twenty-one new test functions; originals unchanged. Initial RED: missing options/Stats/scope API. Review fixed sparse-JSON fixture mislabeled RLE, proxy no-op mislabeled commit error, invalid paths/terrain, post-publication startup failure and vacuous query/entity assertions. Runtime fixtures pass content validation before budget checks.

Root's post-GREEN review returned three concrete gaps through behavioral RED
tests and separate sol implementation turns:

- Supplied-loader Stop reporting retained external ownership in runtime metrics:
  bytes=3532, pinned=809, entries=5. Detaching before final refresh now publishes
  zero runtime decoded ownership while preserving external users.
- Ended-session marker/light indexes and terrain/world IDs survived entity
  removal. Successful Stop now clears them; failed persistence preserves them.
- Rejected binary auxiliary sidecar remained pinned through queue residence,
  adding 1,048,914 bytes although result's Aux field was nil. Full/proxy
  version/hash mismatch tests reached RED. All four metadata rejection branches
  now release only that preparation's unused lease. Independent users survive.

Final review: singleflight/Close/Clear races, graph charge/overflow, shared full/proxy credit, blocked publication, discard/drain, retry hints, collision preparation, shallow teardown and supplied-loader ownership. Navigation uses one batch scope for loading/baking; releases on error and before blocked publication. Metrics read totals, not payload scans.

Passed from `gekko/` with `GOCACHE=/tmp/gekko3d-gocache`:

```sh
go test . -run '^TestS2b' -count=1
go test -race . -run '^TestS2b' -count=1
go test . -run '^Test(S2b|S2a|Streamed|StartStreamed|RuntimeContentLoader|VoxelRt|VoxelRT|VoxelRender|Navigation)' -skip '^TestStreamedNavigationPublishesResidencyWhileOverlayMoves$' -count=1
git diff --check
```

`go test ./...` passed every other engine package and failed only two
previously recorded root failures:

- TestMovingBrushCarriesSupportedPlayerAndNPC: player `[0 0.1 0]`, missing carry.
- TestStreamedNavigationPublishesResidencyWhileOverlayMoves: pending=2,
  loaded=1, load=false, overlay=true.

Actiongame, editor and testing-vox `go build ./...` passed. First two emitted sandbox module stat-cache warnings; exit zero. Race builds emitted existing macOS LC_DYSYMTAB linker warning. Go 1.25.4, darwin/arm64. Reflection identity APIs checked against declared Go 1.24 minimum: [Go 1.24 standard-library source](https://github.com/golang/go/blob/go1.24.0/src/reflect/value.go#L1816).

Final native smoke: 415 frames / 14.245 seconds, exit zero; one-byte prepared-geometry/decoded/pending budgets, two workers, one commit/frame. Verified ready visible proxy through four-second upload pause, hidden partial readiness, atomic refinement, distance coarsening and Stop cleanup. Three sole oversized admissions, one retry; metadata pressure 2086 bytes. Stop removed source asset IDs and reported zero decoded/pending ownership; no world renderer objects after two seconds. Temporary source/log: `/tmp/gekko-s2b-smoke.go`, `/tmp/gekko-s2b-native-smoke.log`; no committed tests. No pixel parity or performance gain claimed.

Navigation batch leases/post-drain persistence recovery verified by production review and existing owner checks; no dedicated deterministic fixtures for those two timings. Pending lower bounds cover real decoded records/geometry; generic graph tests cover capacity/aliases. No heap/RSS ceiling, codec, physical accounting or remaining S2 owner/queue completion claimed.

## S2e: Obsolete preparation cancellation

Extend the existing bounded full/proxy preparation scheduler with per-dispatch
cancellation ownership. Main-thread owners close a job's cancellation channel
when its current full/proxy demand disappears, proxy preparation is disabled,
a loaded replacement makes the job unnecessary, or Stop begins. Global fallback
proxy demand remains needed even after an observer teleport. Managed full
readiness retains fallback proxies under the existing handoff policy.
Cancellation remains terminal even if
the observer returns before that job finishes. Keep the coordinate pending until
the terminal result is consumed; renewed demand then admits a fresh job. Generation
and dispatch identity prevent stale completions from clearing newer ownership.
Cancel already satisfied full/proxy dispatches before persistence acknowledgement
or upgrade/unload can remove their loaded owner. Scan for lost demand after the
loaded-chunk loop rebuilds temporary fallback proxy demand and before new
dispatch. Temporary gameplay/proxy demand retains its existing rules.

Workers receive only a read-only cancellation signal. Check before preparation,
between IO/aux/geometry/object-snapshot phases, after a phase returns (including
errors), and before pending-byte admission. Worker-observed cancellation retains
only identity and duration; release decoded scopes and payload references before
publication. Main-thread consumption rechecks cancellation before errors or
commit, covering queued results cancelled after worker completion. Cancellation
does not set `InitErr`, increment prepare errors, admit geometry/entities, or
create byte-cost retry hints. Report terminal cancellations in
`PrepareCancelledCount`; existing preparation counters describe completed work.

Do not cancel a shared loader flight or prepared-cache builder: other requesters
may need its result. An in-progress decode/build finishes normally, then the
cancelled consumer releases its scope and skips later phases. Warm data remains
subject to its existing cache budget. This bounds demand-owned continuation,
not single-phase latency, physical IO or worker temporary memory. Separate IO,
generation and navigation queues remain later S2 work.

Keep cancellation identity through error/retry compaction, and exclude its
owner from payload charge traversal. A queued result retains existing credits
until main-thread consumption; cancellation never revokes another user's backing.
Stop cancels preparation before persistence barriers can return an error. Stop
and generic drains release cancellation ownership after joining workers.
Failed Stop preserves loaded entities, generation and leases, and later observer
processing can re-admit required preparation. Synchronous gameplay preparation
keeps its immediate contract and has no observer-cancellable token.

Owners/files: `streamed_level_runtime.go`, `streamed_level_pending.go`, and a
private preparation cancellation file if useful. No content-loader API, codec,
cache publication, collider or renderer readiness change. Confidence: High after
dispatch, prepare, commit, shared-flight and Stop inspection. No SME alignment
required within S2. Eager coordinate deletion would allow overlapping dispatches;
canceling shared work would break other users. Retain terminal acknowledgement.

Minimal coverage uses real runtime workers and held shared decodes: obsolete
full/proxy errors cannot poison the runtime; proxy cancellation explicitly
changes policy because global fallback survives teleports. Renewed demand gets a fresh usable
result; cancelled consumers release pending bytes/scoped leases while independent
users remain usable; queued results and Stop cannot revive cancelled work or
strand future scheduling. Preserve existing tests. Verify focused streaming/cache
checks and races, then full engine and affected consumer checks once at the batch
boundary. Native fallback/readiness smoke supplements CPU ownership checks.
