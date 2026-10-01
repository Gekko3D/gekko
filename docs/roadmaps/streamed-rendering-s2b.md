# S2b: Bound decoded content and pending prepared results

Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S2.
Prerequisite: [S2a](streamed-rendering-s2a.md).

## Scope and confidence

Primary owners: RuntimeContentLoader and the streamed runtime's full/proxy
preparation queues. Consumers: world metadata/backing providers, chunk and
proxy preparation, synchronous collision startup, navigation source batches.
This is a long-term ownership step using the current runtime boundaries.

Known: the loader has eight unbounded decoded maps, duplicates concurrent
decodes and keys by raw path. Full/proxy channels each hold 256 results with
heterogeneous payloads. Commit/discard/drain currently have no release handles.
Geometry preparation already has a separate byte budget. Commit copies chunk
data into geometry/heightmaps; entities do not retain decoded chunk/aux records.
Level/manifests/backing providers and shallow metadata maps retain decoded data
for the world session. Navigation source batches retain it through baking.

Unknown: scene-specific budget tuning and temporary decoder/build allocation
peaks. Public Load methods return Go pointers with no lifetime declaration;
external callers may keep or normalize them after cache eviction. Cache charge
cannot track those borrowers or promise total process memory. No buffer reuse
or mutation of evicted definitions is permitted.

Confidence: High after tracing these consumers and Stop/error paths. No SME
alignment required. Entry-only bounds cannot handle heterogeneous data. A new
global residency service would duplicate current owners. Extend the loader and
queue owners with explicit leases/admission instead. A worker waiting for byte
credit while retaining its completed payload defeats queue bounds; reject its
payload and publish a small retry completion instead.

Files: runtime_content_loader.go, focused content charge/cache files, streamed
runtime and focused pending-admission files, navigation_graph_runtime.go, new
functional test files, this roadmap, canonical streaming/runtime-assets docs.
No content codecs, collision algorithms, renderer shaders, GPU residency budget
or navigation publication algorithm changes. Separate IO/navigation queues,
mid-decode cancellation and global physical backing accounting remain S2 work.

## Decoded loader contract

Preserve NewRuntimeContentLoader() and all eight Load methods, including nil
receiver passthrough. Add optional RuntimeContentLoaderOptions{MaxCacheBytes}:
zero selects 128 MiB; negative disables warm retention. Add Stats() returning
RuntimeContentLoaderStats with Entries, Bytes, PinnedBytes, MaxBytes,
OverBudgetBytes, Hits, Misses, Evictions, LoadWaits and OversizedBypasses.

One budget/LRU covers all content kinds. Identity includes content kind and a
lexically cleaned absolute path; relative aliases coalesce. Do not resolve
symlinks or require a cache-hit file to still exist. Type kinds stay separate.
Charge decoded graph storage at admission: struct sizes, slice capacity,
nested pointers/slices/maps, string bytes and logical map entries. Deduplicate
identical backing identities within a graph; conservatively charge arbitrary
overlapping slice/string views. Skip element traversal when their types contain
no referenced storage. RLE payload size is not a decoded-memory estimate.
Exclude allocator/map-bucket/cache bookkeeping, decoder temporary buffers,
derived backing/navigation indexes and separate prepared geometry. This is an
admission-time estimate, since public definitions can be normalized by callers.

Unpinned LRU entries must fit the byte ceiling. Oversized/disabled warm results
remain usable and may still share a concurrent decode. Add NewScope(), returning
a RuntimeContentLoadScope with Loader() and idempotent Close(). Its derived
loader shares the cache and pins every loaded entry atomically on publication.
Repeated loads in one scope acquire one pin; distinct scopes balance separately.
Pinned bytes count each entry once, even with multiple scopes. Pins may exceed
the ceiling, reported as pressure; final release trims unpinned storage. Closing
a scope during decode must prevent late pinning; loads on closed scopes fail.
If all scoped requesters close before decode completes and no raw requester
remains, completion must not create warm ownership on their behalf. Other live
requesters still receive and retain the shared result normally.
Returned pointers remain valid after release/eviction. Clear() drops unpinned
warm ownership and preserves active scopes; it must not let an earlier in-flight
raw load repopulate warm ownership without a new request. An active scope may
still acquire its in-flight result; that declared lease remains protected.

Per kind/path singleflight runs decoding outside the cache mutex. Same-key
waiters share success, including uncached results. Different keys progress
independently. Errors/nil results are not cached; panics wake waiters and permit
retry. Avoid locks across decode or waiting and avoid lost waiter pins.

## Runtime lease lifetimes

MaxDecodedContentCacheBytes config applies when the runtime creates its loader;
a supplied Loader keeps its own options. A metadata scope leases the level,
terrain/world manifests and voxel backing through the world session. Publish
the scope with runtime ownership: failures before publication close it, while
partially initialized sessions retain it until successful Stop. State.Loader is
the base loader, so worker loads cannot accidentally join the metadata scope.

Each full/proxy preparation uses its own scope, transferred with its result and
closed after commit or every discard/drain path. Synchronous collision startup
uses a transient scope through immediate commit. Committed geometry/heightmaps
stay valid after decoded leases release. A sidecar rejected by version/hash/
source metadata releases its unused preparation lease immediately; it cannot
hide behind a release handle excluded from pending payload charge. Other scopes
on the same sidecar remain protected. Navigation uses a batch scope through
source loading and baking, releasing it before blocking result publication;
errors release it too. Navigation snapshots already owned elsewhere stay with
their current owner.

Successful Stop joins/drains workers, removes entities and clears all shallow
metadata maps before releasing metadata leases. Clear a runtime-created loader's
warm ownership; preserve supplied/shared loader users. Failed persistence keeps
the current generation and metadata ownership usable. Restart has fresh queue
credits; stale results cannot remove a new generation's pending marker. Keep
S1c ticket retirement and fallback handoff ordering unchanged.

## Pending full/proxy admission

Add MaxPendingPreparedBytes: zero selects 128 MiB, negative is invalid. One
thread-safe owner covers both channels for a session. Preserve public channel
types, entry capacities, existing count/time commit budgets and optional result
handles so existing synthetic channel fixtures remain usable.

After preparation, charge the result's decoded records/aux, snapshot and
placement graphs, geometry and envelope/key strings. Deduplicate within the
result. This is a per-result admission cost; separate results and loader/cache
owners may conservatively charge shared data again. Do not add these metrics
together as physical memory. No estimator may follow borrowed GPU managers,
cache owners, errors or release handles. Reuse the geometry storage estimator
through a temporary ledger rather than retaining roots in the live cache ledger.

Admit fitting results by reserving bytes before channel publication. Credit
covers blocked publication as well as queued payload. If it cannot fit, release
the decoded scope and all large payload references; publish only a small retry
completion carrying generation/coordinate/required cost. Keep a cost hint so
the observer waits for sufficient capacity before rebuilding that demand.
Clear hints when demand/session ends. A single oversized payload may be admitted
when no other charged payload is outstanding, preventing valid large pages from
starving. Report over-budget/oversized admission explicitly. Every consumption
branch and Stop drain releases credit exactly once. Retry metadata is bounded
by existing channel/job counts and excluded from payload byte charge.

Active decoding/building is outside this retained-result ceiling and remains
bounded by MaxPrepareJobs. This slice does not claim hard decode allocation or
RSS limits. Main-thread obsolete-demand checks discard results safely; true IO
cancellation and queue partitioning are later S2 work.

Expose decoded cache and pending payload bytes/budget/pressure plus admission
retry/oversized counts in StreamedLevelRuntimeMetrics. Update them without
rescanning decoded/geometry payloads every frame. Existing queue-depth and
commit-count semantics remain unchanged.

Metric prefixes: DecodedContentCache with the loader Stats suffixes above;
PendingPrepared with Bytes, MaxBytes, OverBudgetBytes, AdmissionRetries and
OversizedAdmissions.

## Functionality tests and adversarial review

Use the user-authorized GPT-6.1 sol RED/review/GREEN/review/commit workflow.
Add new tests; preserve existing test files. Protect externally observable
loader, budget, lease and runtime contracts rather than container layout or
helper call order. Relative measured fixture charges avoid ABI assumptions.

- All eight content kinds share a byte ceiling; nested aux/backing/asset/level
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
  persistence preserving the usable world. Navigation source batch lifetime.
- Rejected auxiliary sidecars release their unused leases before publication,
  retaining fallback geometry, chunk/terrain leases and independently scoped
  sidecar users.

Root reviews tests for missing ownership branches, unsupported fixtures,
concurrency determinism and architectural fit before production delegation.
Review GREEN code for scope/credit leaks, lock ordering, idle-worker accounting,
stale pending-marker deletion, queue retries, Stop failures and hot-path scans.
Concrete missing contracts return through RED tests and another review.

## Verification

Baseline owner tests passed before changes. Run new functional tests, existing
streamed/loader/renderer owner tests, targeted races and engine sweep. Compare
the two recorded pre-existing root failures. Compile actiongame/editor/voxel
example consumers. Native GPU smoke with small loader/pending budgets verifies
fallback/full handoff and Stop lifetime; CPU tests cannot certify pixels.

## Execution record

Implemented by GPT-6.1 sol after root's narrow design and adversarial RED test
review. Twenty-one new functional test functions protect the contracts above;
original tests were unchanged. Initial RED lacked the new options/Stats/scope
API. Test review corrected a sparse-JSON fixture labeled RLE, a proxy no-op
labeled commit error, invalid content paths/terrain source, a startup fixture
that failed after publication, and vacuous query/entity assertions. Valid
runtime fixtures explicitly pass content validation before testing budgets.

Root's post-GREEN review returned three concrete gaps through behavioral RED
tests and separate sol implementation turns:

- Supplied-loader Stop reporting retained external ownership in runtime metrics:
  bytes=3532, pinned=809, entries=5. Detaching before final refresh now publishes
  zero runtime decoded ownership while preserving external users.
- Ended-session marker/light indexes and terrain/world IDs survived entity
  removal. Successful Stop now clears them; failed persistence preserves them.
- A rejected binary auxiliary sidecar remained pinned through queue residence,
  adding 1,048,914 bytes although the result's Aux field was nil. Full/proxy
  version/hash mismatch tests reached RED. All four metadata rejection branches
  now release only that preparation's unused lease. Independent users survive.

Final review checked singleflight publication/Close/Clear races, source graph
accounting and overflow handling, full/proxy shared credit, blocked publication,
discard/drain branches, retry hints, synchronous collision preparation, shallow
metadata teardown and supplied-loader ownership. Navigation source loading and
baking use one batch scope, released on error and before blocked publication.
Metrics read accumulated owner totals rather than scanning payloads per frame.

Passed from `gekko/` with `GOCACHE=/tmp/gekko3d-gocache`:

```sh
go test . -run '^TestS2b' -count=1
go test -race . -run '^TestS2b' -count=1
go test . -run '^Test(S2b|S2a|Streamed|StartStreamed|RuntimeContentLoader|VoxelRt|VoxelRT|VoxelRender|Navigation)' -skip '^TestStreamedNavigationPublishesResidencyWhileOverlayMoves$' -count=1
git diff --check
```

`go test ./...` passed every other engine package and failed only the two
previously recorded root failures:

- TestMovingBrushCarriesSupportedPlayerAndNPC: player `[0 0.1 0]`, missing carry.
- TestStreamedNavigationPublishesResidencyWhileOverlayMoves: pending=2,
  loaded=1, load=false, overlay=true.

Actiongame, editor and testing-vox consumer `go build ./...` checks passed.
The first two emitted sandbox module stat-cache warnings; exit status was zero.
Race builds emitted the existing macOS linker LC_DYSYMTAB warning.
Verification used Go 1.25.4 on darwin/arm64. Reflection identity APIs were also
checked against the module's declared Go 1.24 minimum in the
[Go 1.24 standard-library source](https://github.com/golang/go/blob/go1.24.0/src/reflect/value.go#L1816).

Native GPU smoke passed on the final source: 415 frames / 14.245 seconds, exit
zero, one-byte prepared-geometry/decoded/pending budgets, two prepare workers
and one commit per frame. It observed a ready visible proxy through four seconds
of paused uploads, hidden partial full readiness, atomic full refinement,
distance coarsening and Stop cleanup. The pending owner recorded three sole
oversized admissions and one retry; retained metadata pressure was 2086 bytes.
Stop removed owned source asset IDs, reported zero decoded/pending ownership,
and left no world renderer objects after two seconds. Diagnostic source/log:
`/tmp/gekko-s2b-smoke.go`, `/tmp/gekko-s2b-native-smoke.log`. These are temporary
diagnostics, not committed tests. No pixel parity or performance gain claim.

Navigation batch lease paths and post-drain persistence recovery were reviewed
in production code and existing owner checks. Dedicated deterministic fixtures
for those two timings were not added. Pending graph lower bounds cover real
decoded records and geometry; generic nested-graph tests cover referenced
capacity/alias accounting. No heap/RSS ceiling, codec change, global physical
accounting or remaining S2 owner/queue completion is claimed.
