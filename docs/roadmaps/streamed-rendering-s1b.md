# S1b: Global voxel upload budgets and deterministic scheduling

Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S1.
Prerequisite: [S1a residency/readiness](streamed-rendering-s1a.md).
Contract: [island residency](../content/island-streaming.md#renderer-residency-contract).

## Scope and confidence

Owner: GPU voxel resource manager. Consumers: renderer app, streamed bridge/readiness, editor and voxel demo. Long-term island upload scheduler. Confidence: high after writer, allocation, readiness and frame-order inspection.

Known: sector/brick limits reset per object; materials bypass budgets; dirty-map order is unstable; shared geometry must upload once. S1a separates hidden residency from rendering and supplies scheduling metadata. No missing architecture decision needs alignment.

One scheduler per `UpdateVoxelData` invocation, normally once per renderer frame. Retain main-thread ownership and writers. Common service selects work and completes dirty/material bookkeeping; executor queues GPU writes. Headless tests substitute executor on production service path.

Per-object queues cannot enforce global limits. FIFO without aging can starve collision/detail or retained pages. Use deduplicated deterministic queue, aging and hard admission limits.

Budget **voxel content writes**: sector/brick/material records, auxiliary occupancy/normals and mixed payload textures. Exclude buffer allocation/migration, sector lookup rebuilding, scene instance/BVH uploads, queue construction and normal-halo discovery. Their CPU/memory bounds remain S2/S3 and allocator work. No cap on all GPU transfers or main-thread rendering.

No changes to formats, shaders, collision algorithms, page selection, workers, eviction or automatic parent/child handoff. Large arrivals use S1a hidden tickets. Visible objects retain progressive uploads; no startup gate added.

## Budget API and semantics

Add GPU-owned public configuration value and manager setter/getter:

```go
type VoxelUploadBudget struct {
    MaxBytes   uint64
    MaxSectors uint32
    MaxBricks  uint32
}

func DefaultVoxelUploadBudget() VoxelUploadBudget
func (m *GpuBufferManager) SetVoxelUploadBudget(budget VoxelUploadBudget)
func (m *GpuBufferManager) VoxelUploadBudget() VoxelUploadBudget
```

Keep `SectorsPerFrame` as sector limit for existing callers, now global.
Store byte and brick limits in `VoxelUploadBytesPerFrame` and
`VoxelUploadBricksPerFrame`. Constructor defaults: 4 MiB, existing
`MaxUpdatesPerFrame` sector value (1,024), and 65,536 brick records. Island
profile can explicitly choose 4 MiB, 16 sectors and 1,024 brick records; do not
install island game profile in GPU library.

Zero means pause that resource, not unlimited. Zero sector limit still allows
standalone brick edits and materials; zero brick limit still allows materials;
zero byte limit pauses all content writes. Use overflow-safe remaining-budget
comparisons. Never reset consumption inside object/map loop.

Every full-sector unit consumes one sector and **64 brick records**, including
cleared records. Standalone brick unit consumes one brick record. Materials
consume bytes only. Counters describe all admitted writes; brick counters
include records covered by sectors. Deferred work consumes no budget.

Whole-sector service units remain atomic. If unit exceeds full-frame limits, retain dirty work and service other eligible units. Never exceed caps to force progress. Caller must raise undersized limits; default fits largest sector.

## Exact costs and completion

- Sector header: 32 bytes. Each of its 64 brick records: 32 bytes.
- Occupied solid/uniform-sparse brick: 1,088 auxiliary bytes plus its record.
- Mixed brick: another 512 payload bytes, totaling 1,632 bytes including record.
- Empty/cleared brick: record only, 32 bytes.
- Sector: header plus 64 record costs and sidecars/payloads for occupied bricks.
  Maximum current-format unit: 104,480 bytes.
- Material: 64 bytes per emitted row; empty table emits 256 zero rows; longer
  tables retain existing cap of 256 emitted rows. Readiness uses this same
  capacity rule while preserving identity/length tracking of source table.

Estimate before baking/encoding/writing from writer upload modes and layout constants. Estimated bytes must equal emitted bytes for valid inputs. Failed/deferred executor must not clear dirty work or publish current material metadata.

Allocate material slots as needed, but new/deferred material allocations must
remain distinguishable from uploaded ones, including empty tables and generation
zero. Material allocation metadata becomes current only after successful writes.

Successful sector consumes its entry and all covered dirty bricks. Never schedule covered bricks separately, even for deferred sectors. Clean false/orphan entries without writes. Standalone success consumes only its entry. Later frames rebuild from live geometry, not retained payloads. Removal drops scheduler ownership and age records.

Atlas pressure blocks admission before any unit writes. Preflight available/new/releasable slots; retain dirty work if insufficient. Successful replacement/clear releases obsolete payload/aux slots; sector and standalone replacement semantics must match. No partial sector followed by atlas-full panic. Cache eviction and full buffer admission remain S2 work.

## Queue order and fairness

Centralize existing five priority values in renderer core, retaining
public streamed enum values. New core objects default to visible priority.
Bridge keeps its existing ticket/map order semantics.

Deduplicate geometry by map pointer and work coordinate. Choose best
priority/order among instances sharing map. Material work stays per object.
Use map ID when object upload order is zero. Hidden resident objects participate.

Within fresh queue: priority, upload order, map ID, work kind (material before
sector before standalone brick), then signed lexicographic sector/brick
coordinate. Equivalent per-object material ties preserve scene slice order;
streamed tickets give distinct stable order. No pointer-address ordering.

Retain first queued frame for each outstanding work identity. Promote one
priority level every 8 waiting service frames, down to fallback priority. On
equal effective priority, older work wins before ordinary stable ties. New work
starts unaged; completion/removal drops age state. Age record keeps only
identity/time, not prepared payloads. Fit-capable work eventually receives
service under sustained higher-priority arrivals. Oversized or capacity-blocked
units cannot claim this guarantee and must not block smaller fit-capable work.

Expose `VoxelUploadBytes` and `VoxelMaterialsUploaded` beside existing upload
counters; pending sector/brick counters count shared maps once. Existing S1a
readiness remains false until geometry, material and lookup state are current.

## Functional tests and adversarial review

Assert service decisions, byte/work totals, dirty/material completion and readiness; never private layout, helper order or GPU plumbing. Private seam may use `serviceVoxelUploads(scene, execute func(voxelUploadWork) bool)` with kind, object/map, coordinates and costs. Supporting types are implementation choices; service must be production path.

- **Global limits:** many maps/objects; exact boundary, one-byte deficit, zero
  limits, maximum numeric limits; sectors and covered/standalone bricks count
  together; materials share same byte limit; fresh consumption next frame.
- **Costs:** mixed/solid/uniform-sparse/empty bricks, partially occupied sector,
  maximum sector, short/empty/capped material tables; costs match emitted sizes.
- **Order:** priorities, ticket/map order fallback, signed coordinates, shuffled
  object/map insertion, shared map choosing best instance, materials before
  geometry on equivalent keys; hidden residents remain eligible.
- **Deduplication:** shared geometry once, per-instance materials separately,
  covered brick work never duplicates or bypasses deferred sector work; pending
  counters count physical map queues once.
- **Progress:** drain over several frames; edit/replacement between frames uses
  current work; continuous high-priority arrivals cannot starve older work;
  oversized first item does not block smaller eligible work.
- **Failure/lifetime:** executor refusal leaves dirty/material state unchanged;
  atlas capacity blocks atomic unit before execution; clearing/replacement
  can recover slots; object/map removal drops stale scheduler work.
- **Readiness:** deferred empty material never reports ready at generation zero;
  partial frames remain uploading; completed content plus current lookup settles;
  existing S1a terminal, ownership and visibility contracts still pass.

Expected files: GPU manager/voxel writer, focused scheduling file, core upload
priority definitions/default, streamed enum aliases, focused GPU tests and docs.
Existing physical writers must use service path; test-only scheduling code
is not acceptable.

## S1d: Deterministic preparation priority

Use one main-thread `container/heap` queue for eligible full/proxy dispatches.
Existing desired/pending/loaded maps remain membership owners; workers receive
captured jobs. Queue items contain kind, coordinate, priority and first waiting
observer-update sequence, never decoded data or geometry. Rebuild scheduling
views from current eligibility, retaining only current waiting identities.
Preserve `MaxPrepareJobs` as the active-worker limit and existing pending-byte
admission. A combined CPU-ready/GPU-uploading allowance is a separate decision.

Fresh order follows the island policy: fallback proxies, collision/destruction
full chunks, visible full detail, then prefetch. Existing keep-only demand retains
content without initiating preparation. Visible detail is current observer-radius
demand or imported full detail in a current/PVS-visible sector; prefetch-only
sector expansion remains prefetch. Reuse selection's incremental overlap counts
and cached per-observer sector derivation to classify this distinction. No new
per-frame footprint enumeration or layer/global lattice is introduced.

Promote one priority level per eight waiting observer updates, capped at fallback.
Compare effective priority, first waiting sequence, signed lexicographic X/Y/Z,
then kind (proxy before full on otherwise equal keys). No pointer, insertion or
goroutine-completion ordering. Persist waiting age through a pending attempt or
byte-cost retry. Age belongs to current unmet demand; acknowledgement of an older
cancelled dispatch cannot erase a renewed request's fresh birth. Release age when
demand disappears, proxy policy removes eligibility, or a matching loaded owner
satisfies demand. Observe satisfaction before persistence/upgrade can remove that
owner. Stop entry/restart resets scheduling age without changing failed-Stop
world ownership. Sequence wrap starts a fresh scheduling era. Fit-capable
waiting detail eventually wins against continually
new fallback arrivals; blocked IO and capacity cannot promise bounded latency.

Check current loaded/pending state, proxy enablement/commit need, loadable content
and known pending-cost fit before dispatch. A byte-blocked item retains age but
cannot block smaller eligible work. Build the job only after selection, using
fresh override state. S2e cancellation and terminal acknowledgement remain the
dispatch lifetime owner; returning demand cannot revive a cancelled attempt.

Expose cumulative `PrepareDispatchCount` and last dispatched coordinate/kind
as `LastPrepareDispatchCoord` and `LastPrepareDispatchKind` (`full` or `proxy`).
These report actual admission, independent of completion timing. Existing prepared
and commit metrics keep their meanings.

Owners/files: `streamed_level_runtime.go`, a private preparation scheduler,
`streamed_level_selection.go` for incremental current/PVS classification, and
runtime metric publication. GPU upload scheduling, readiness, codecs, collision
and persistence remain owned by their existing systems. Confidence: High after
dispatch loops, selection derivation and S1b upload ordering inspection; no SME
alignment required. Sorting separate proxy/full lists preserves proxy starvation;
one shared queue with aging follows the approved island direction.

Minimal coverage exercises actual dispatch/commit and public metrics: stable
signed ties under shuffled insertion, shared proxy/full priority, current/PVS
detail before prefetch, aging under sustained fresh fallbacks, renewed demand and
byte-pressure progress. Existing selection/cancellation/pending/Stop checks remain
unchanged. Focused checks and independent reviews precede full engine/consumer
and race checks at the batch boundary. Native fallback/readiness verification
supplements CPU scheduling checks; no frame-time or total-memory claim.

## S1e: Combined streaming admission

Add `MaxStreamingWorkItems`: zero selects 32, positive values including one are
valid, negative is invalid. One main-thread admission owner spans asynchronous
full/proxy dispatch, queued CPU result and initial renderer completion. Preserve
the separate active-worker and pending-byte limits. A work item is one full chunk
or proxy attempt; a full chunk can own both terrain and imported render targets.
The owner retains scalar identities and tickets, never ECS rows or payloads.

Acquire before dispatch. Cancellation holds the item through matching terminal
acknowledgement; retries, obsolete results and preparation errors release it.
Successful commit transfers the item to its actual staged runtime-owned targets,
including targets flushed before a later commit failure. CPU-only commits and
proven empty results release immediately. A GPU item finishes after every initial
target is individually Ready or terminally Failed, or safely retired. Ready must
match live marker/entity/generation ownership. Cohort visibility is independent:
a Ready child can release its item while a sibling still keeps the cohort hidden.
Failure releases admission without proving coverage or changing error policy.
Observe qualified terminal completion before automatic reticketing, and latch
each initial target's completion so delayed siblings cannot recharge it.

Live stale/cancelled targets follow their replacement ticket within the same
unfinished item. Scalar fences for old unfinished tickets remain in that item
until the existing retirement owner proves completion; a new Ready ticket cannot
hide an older Uploading capture. After terminal failure, a repaired-source reticket is new
compatibility work. Removed unfinished targets hold their item until the old
marker is absent and renderer retirement proves terminal or forgotten status.
Polling/reticket integration must preserve that fence and qualify current-world
readiness independently from older tickets.

Synchronous gameplay commits, late renderer adoption and repaired sources retain
their existing latency/lifetime behavior. Their initial GPU work is accounted but
can exceed the admission ceiling; expose that pressure and stop new asynchronous
dispatch until capacity returns. Ordinary edits of already Ready geometry remain
with renderer upload budgets and do not recharge a completed load. Authored
placement rendering remains with its existing owner. This bounds the admitted
streaming frontier, not total renderer work or process memory.

Keep current-world GPU items through failed Stop. Successful Stop drains CPU
items and moves unfinished retiring GPU items to scalar carryover debt. Expose
that debt separately until the existing retirement sweep proves completion; it
cannot block a new generation or establish its readiness. Missing renderer
resources cannot prove old retirement. Current-world managed targets retain
their existing renderer-required behavior.
At successful Start, determine managed residency anew from the installed renderer.
Resource disappearance within an active world cannot downgrade that world;
the previous world's latch cannot force a fresh headless world into GPU staging.

One shared allowance, including limit one, follows current readiness topology:
individual targets upload independently of cohort members or fallback preparation.
A working renderer frees capacity before coarsening needs a new fallback. A
stalled renderer backpressures asynchronous work; S1d aging restores waiting
detail priority when capacity returns. No extra partition or recovery overflow
is needed. Preserve synchronous collision readiness and existing proxy coverage.

Publish `StreamingWorkCount`, `StreamingWorkMaxCount`,
`StreamingWorkOverBudgetCount`, `StreamingWorkCarryoverCount` and cumulative
`StreamingWorkAdmissionBlockedCount`. The first four are `int`; the last is
`uint64` and counts observer updates with otherwise eligible dispatch blocked by
this allowance. Private accounting owns admission; diagnostics never control it.

Owners/files: a private admission owner, preparation dispatch and result/commit
lifecycle in `streamed_level_runtime.go`, and ticket staging/retirement in
`streamed_level_render_residency.go`. Workers, codecs, collision algorithms,
ordinary placement visibility and the renderer readiness predicate stay owned by
their existing systems. Explicit attempt records preserve initial completion and
retirement; a derived pending/target union would recharge edits or lose removed
unfinished targets. Confidence: High after runtime and independent topology
review; no SME alignment required.

Minimal tests protect actual running/queued/GPU backpressure, shared full/proxy
credit and hidden-cohort progress at limit one, qualified readiness, cancellation
and retry release, compatibility pressure/diagnostic independence, failure repair
and retirement across Stop/restart. Use existing cancellation, readiness, pending
byte and failed-Stop coverage where it already protects the contract. Separate
Sol 6.1 test/implementation agents and independent pre/post reviews precede focused
race, full engine/consumer checks and a native paused-upload/resume fixture.

## S1f: Deterministic ready commit queue

The former proxy-first channel drain could starve full detail and inherited
worker completion order. S1f orders the ready frontier by live
S1d priority, first queued age and signed coordinate/kind ties, while retaining
count/time budgets, exact cancellation acknowledgements and payload leases.
The captured frontier stops after shutdown or generation change; drained scalar
IDs cannot consume ownership again.
Arrivals after the captured frontier wait for the next update. This does not
bound one large chunk's main-thread commit.

Approved architecture: transfer completed results into one main-thread owned
ready queue. Workers retain their existing buffered publication channels; queued
results keep pending-byte and S1e admission ownership until consumed or drained.
Stop drains both channels and the ready queue. Public prepared-depth metrics count
both owners. Queue records never retain ECS rows; workers never mutate the queue.
This also provides an owner for later resumable commit work.

Ready capacity is `max(2, cap(PreparedLoads) + cap(PreparedProxyLoads))`, with a
saturating sum. Stop capturing when retained ownership is full; alternate source
kinds when only part of the transport frontier fits. This preserves backpressure
for uncredited raw channel producers as well as S1e runtime attempts. Capture the
entry channel lengths, bounded by free ready capacity. Nil channels contribute
nothing; unbuffered channels permit at most one nonblocking rendezvous per kind
per update. Ordering covers captured results, not uncaptured sends. Runtime
workers keep their current publication behavior. Every receive is nonblocking;
concurrent channel consumption cannot make capture wait. A smaller transport
capacity preserves retained results and stops new capture until they fit again.

Rebuild one scalar heap from the retained frontier each commit update. Stale,
cancelled, obsolete, duplicate and error envelopes use a deterministic
cleanup prefix and their existing consumption handlers; capture never
acknowledges them. Wanted byte-cost retries retain live demand priority and age;
their acknowledgement, cost hint and release behavior remain unchanged.
Otherwise reuse live S1d priority. Promote one priority level
per eight waiting commit updates, capped at fallback. Compare first queued update,
signed X/Y/Z and kind after effective priority; proxy wins equivalent kind ties.
Each result retains its own birth, so a replacement token cannot inherit an old
attempt's age. All results newly captured in an update share that update's birth;
capture order cannot precede coordinate ties. Consumption/drain releases age;
sequence wrap starts fresh births.
Recheck generation, token, demand and loaded ownership before actual publication.
Preserve count/time checks before every consumed result and existing error policy.

On 2026-10-03, the user approved this queue and migration of the existing
deferred-commit channel-depth assertions to public total prepared depths, retained
credit and actual residency checks. This includes
`TestStreamedRuntimeCommitBudgetLeavesPreparedChunksQueued` and S2b/S1d pressure
fixtures. Preserve their other assertions and the remaining tests.

Alternative: keep payloads in the channels and reorder a fixed buffered frontier
under a publication mutex. A condition variable can preserve queue depths and
wake runtime publishers after consumption, but it requires restricting direct
channel access to quiescent main-thread use. Arbitrary direct receives do not
notify blocked publishers; unbuffered replacements cannot supply a sortable
frontier. No repository consumer currently accesses these channels, but their
public exposure leaves that compatibility boundary unspecified. Introducing the
restriction without alignment would turn an implementation choice into a new
API contract.

Root confidence is High after ownership inspection and explicit user alignment;
no additional SME alignment is required. Use separate Sol 6.1
test/implementation agents and independent pre/post reviews. Minimal coverage
should protect real full/proxy ordering, sustained-fallback fairness, cancellation
and ownership through deferred commits/Stop, followed by focused race and the
usual engine/consumer boundary checks.

## S1g: Opt-in resumable placement commits

Status: approved by the user on 2026-10-03 after independent architecture review;
implementation complete. The user approved opt-in gradual placement visibility
and collision, durable partial edits and unchanged default behavior. This resolves
the Human Alignment Gate for the cross-frame publication contract. P5a/P5b reduce
geometry registration work; a loop over many placements can still stall one frame.

Approved contract:

- Add `MaxPlacementCommitUnitsPerFrame` for managed runtimes: positive values
  enable the shared placement-unit budget; nonpositive values and CPU-only
  commits keep current synchronous behavior. One placement, its snapshots and
  hooks remain atomic; the frame limit is checked between units across chunks.
  Terrain/imported work remains atomic. This cannot bound one expensive asset,
  snapshot or user hook by elapsed time.
- In the opt-in mode, `MaxChunkCommitsPerFrame` also limits distinct chunks
  advanced/started that frame. Cleanup and byte-cost retries do not spend that
  allowance; cleanup remains eligible after it is spent. Check time between every
  atomic unit. Service a shared fixed frontier of ready envelopes and active cursors in rounds, at most
  one placement per candidate per round with live S1d priority/aging. Serve its
  terrain/imported prefix first, checking time between those atomic phases too.
  Reset waiting age after service. Exhausted placement capacity skips placement
  units while eligible proxies, cleanup and placement-free completions can proceed.
- A private main-thread transaction owns generation, cursor, prepared envelope,
  loader scope, S2b pending charge and S1e admission until completion or cleanup.
  Track every partial entity/asset before flush or hooks. `LoadedChunks` and its
  completion metrics publish only when the whole chunk finishes.
  Keep active payloads inside the bounded ready owner, with an exact coordinate
  index for lookup; no second unbounded prepared list. Pending preparation tokens
  remain owned until terminal completion or cleanup. Temporary target grouping
  must not finish the S1e CPU item at each pause.
- In the opt-in mode, ordinary placement visuals/colliders become observable as
  each placement finishes; hooks run once at that boundary. Existing managed
  terrain/imported readiness and proxy coverage remain in force.
- Cancellation/unload and Stop capture all partial terrain/imported/placement
  edits before removal. S4c qualifies the active transaction as an exact owner
  without inserting it into `LoadedChunks`. Persistence failure pins those
  owners for retry; completion transfers ownership once to the loaded chunk.
  Cancelled active-only owners may retire after durability without waiting for
  an imported proxy: their imported targets have never established whole-cohort
  coverage. Completed loaded owners retain the existing proxy-before-removal
  gate. This avoids waiting for a proxy whose admission needs the active owner's
  slot. Unfinished renderer retirement still retains its admission debt.
- Spawn/snapshot errors and hook-signalled failures keep the fatal `InitErr` policy and
  retain the failed transaction for persistence and teardown. Capture partial
  spawn IDs as they are created, including error paths. Never blindly retry a
  failed placement or rerun completed hooks.
  Existing hook panic behavior is unchanged; no recovery policy is introduced.
- Before each remaining unit, resolve its stable placement identity against
  current transform/deletion and snapshot authority. Skip deleted/moved-away
  placements and apply current snapshots. Keep completed cursors/hooks intact;
  stale captured items cannot respawn a deleted placement or overwrite a live edit.
  Each evaluated placement identity consumes a unit even when skipped, so deleted
  inputs cannot bypass the scan bound.
- Synchronous same-coordinate loading completes the active transaction rather
  than creating duplicate entities; it may exceed the frame budget as today.

Alternative: preserve whole-chunk publication everywhere and continue worker
preparation. That reduces individual costs but does not bound placement admission
per frame. Hiding only placement rendering does not isolate gameplay queries or
collision, so it cannot preserve atomic gameplay publication.

Owners/files: `streamed_level_runtime.go`, ready/pending/admission owners and S4c
persistence, plus streaming contracts. Focused coverage protects partial lifetime,
cancellation, hook counts, failed persistence, synchronous completion and defaults.
The full workflow, race, engine/consumer and native handoff checks passed; see the
[parent delivery record](streamed-rendering-content-optimization.md#s1g-opt-in-resumable-placement-commits).
Individual large-unit bounds remain later work; no hard elapsed-time guarantee.

## S1h: Capacity planning for structural inputs

Completed 2026-10-03. GPU buffer capacity planning enumerates sectors only for new maps or maps
with `StructureDirty`, matching structural preparation's existing eligibility.
A small arrival no longer causes every clean resident map's sector graph to be
visited. Exact map/sector-pointer deduplication, allocator tails, required record
arithmetic and buffer headroom remain. Hidden upload candidates stay eligible.
This changes planning work within the existing manager, not allocation ownership.

Files: `voxelrt/rt/gpu/manager_voxel.go`, manager diagnostics and focused GPU tests.
The pure `voxelAllocationRequirements(scene)` seam returns required
sector/brick records from current allocator tails plus eligible missing sectors.
`VoxelCapacityPlanningSectorVisitsLastUpdate` counts sector entries visited by
that planning invocation; clean allocated maps contribute zero. Preparation,
normal halos, content limits and readiness remain in their current order.

Confidence is High after independent code/architecture audit. No human choice,
shader, format, collision or gameplay publication change. Frozen coverage protects
many clean allocated maps with one arrival, same-count pointer replacement,
shared map/sector pointers, nil inputs and hidden candidates. Focused GPU checks,
full engine/consumer checks and a native GPU buffer/readiness smoke passed. One large
new/dirty map's structural work and other scene scans remain unbounded.

## S1i: Physical voxel-resource growth admission

The user approved a configurable soft allocation budget with pinned pressure on
2026-10-04. This is a permanent extension of the existing GPU manager, separate
from upload-write limits and assigned-slot retention. Zero disables the soft cap
to preserve ordinary loading; hard device limits remain mandatory.

Count current sector, brick, auxiliary, material and sector-lookup buffers,
their unreleased replacements, and the fixed payload atlas. Replacement admission
counts the peak while both buffers exist. Other renderer resources and driver
overhead remain outside this budget. The fixed atlas can already occupy 4 GiB;
no generic island-sized default or total VRAM guarantee is introduced.

New hidden streamed targets explicitly allow optional admission. Pending full
detail is also optional. Ordinary objects, including temporarily hidden compiled
startup objects, preserve required admission. Existing map and material owners
remain pinned; any required shared instance makes geometry required. New optional
material users still require their own admission. Required growth may exceed the
soft budget and exposes pressure. Optional targets may reuse existing capacity
under pressure, but cannot grow it beyond the budget. Fit-capable smaller work
continues past a blocked target, with deterministic priority and stable ties.

Plan checked capacities before assigning slots or publishing lookup/material
metadata. Respect storage, uniform and buffer limits; reduce headroom/geometric
growth when the required content fits. Resource replacement publishes only after
all allocation and migration preparation succeeds. Refusal preserves dirty work,
CPU authority and existing readiness. Hard-blocked structural edits suspend all
uploads for that map; lookup retains its allocated sector snapshot. Never publish
a deferred map through another map's shared sector pointer.

Slot reuse and inactive eviction use existing owners. Release a shared sector or
brick only after its final allocation reference disappears. Buffer capacities
and allocator high-water marks do not shrink; a lower cap therefore reports
pressure while allowing reuse. Retired bytes remain charged until actual release.

Files: GPU admission/accounting, allocation/retirement, voxel structure/service,
sector lookup, core object policy and bridge assignment. Use separate test and
implementation agents with independent PRE/POST reviews. Minimal coverage protects
budget boundaries, fixed/retired pressure, required/shared users, deferred
readiness, fitting retries, hard limits, allocation refusal and alias lifetime.
Run focused GPU/bridge checks and race checks, then engine tests and affected
consumer builds. Native fallback, buffer replacement and retirement require a
user-run release check before completing the batch.

Completed 2026-10-04. Existing tests remain unchanged. Admission covers geometry,
per-object materials and lookup, including retained reactivation and shared-slot
lifetime. Engine tests, focused race checks and five consumer builds passed.
The user reported the release visual check passed after receiving the expected
phase sequence. Allocation/migration and lookup work remain outside per-frame caps.

Commands passed from `gekko/`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . ./voxelrt/rt/gpu -run '^(TestS1i|TestS1h|TestVoxelUpload|TestS2d|TestS2i|TestC3h9|TestC3h10)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . ./voxelrt/rt/gpu -run '^(TestS1i|TestS1h|TestVoxelUpload|TestS2d|TestS2i|TestC3h9|TestC3h10)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Consumer builds passed from each module under `/Users/ddevidch/code/go/gekko3d`:

```sh
# gekko-editor/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1i-build/editor/ ./...
# actiongame/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1i-build/actiongame/ ./...
# spacegame_go/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1i-build/spacegame/ ./...
# spacesim/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1i-build/spacesim/ ./...
# examples/testing-vox/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1i-build/testing-vox/ ./...
```

The release diagnostic build also passed. Run it from `gekko/`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go build -trimpath -ldflags='-s -w' -o /tmp/gekko-s1i-admission docs/roadmaps/diagnostics/s1i_admission.go
/tmp/gekko-s1i-admission
```

Follow the window title and advance with SPACE only after checking each state.
The coarse plate must stay solid during deferral and fine upload, with no blue
alias on the right. Resize during both states. After promotion, the full plate
shows a rectangular opening. The required red cube appears despite pressure;
its optional green replacement reuses capacity. Wait for console `PASS`, zero
retired bytes and no allocation errors, then confirm visual continuity. ESC exits.
This check uses no screenshots or video. Native allocation, migration, shader
execution and release are not established by the automated tests.

## S1j: Deferred optional admission aging

Completed 2026-10-04. Optional geometry and materials now share the established
aged order instead of indefinitely losing to fresh higher-priority demand.
Required ownership, joint preflight, hard/peak limits and current request identity
remain. [Canonical contract](../renderer/runtime.md#physical-voxel-gpu-admission).

Files: GPU admission, private scheduling state and four focused behavior tests.
Existing tests remain unchanged. Root and independent reviews passed; no new
native check was required because allocation, shader and publication paths are
unchanged from S1i. There is no progress guarantee without fitting capacity, and
structural/migration/lookup work remains outside per-frame caps.

Commands passed from `gekko/`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -run '^(TestS1j|TestS1i|TestVoxelUpload)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race ./voxelrt/rt/gpu -run '^(TestS1j|TestS1i|TestVoxelUpload)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Consumer builds passed from each module under `/Users/ddevidch/code/go/gekko3d`:

```sh
# gekko-editor/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1j-build/editor/ ./...
# actiongame/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1j-build/actiongame/ ./...
# spacegame_go/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1j-build/spacegame/ ./...
# spacesim/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1j-build/spacesim/ ./...
# examples/testing-vox/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1j-build/testing-vox/ ./...
```

## Next alignment: Frame-bounded voxel publication

Whole-map structure preparation and global lookup rebuilding remain atomic;
S1k bounds native buffer migration. Spreading the remaining work across updates introduces current and
staging generations, with coherent publication and existing coverage retained.
This is a permanent ownership change; S1i/S1j do not authorize it implicitly.

Approved by the user on 2026-10-04:

1. Continue visible edits and material animation through mirrored writes into
   staging. Growing pools are shared; pausing them would delay existing owners
   beyond the arriving map. Preserve CPU authority, dirty work and fallback pins.
2. Native buffer/texture creation is indivisible. Allow one reported oversized
   allocation as the only creation operation in that update so loading progresses;
   physical admission and hard device limits still apply.
   Resumable copy/lookup work cannot establish a driver elapsed-time bound.

The completed S1k batch bounds native buffer creation and migration after
renderer bootstrap. One fixed physical generation advances independently of
live demand. Mirror current writes into created replacements; charge duplicate
content bytes to the existing upload cap. Publish all replacements together and
rerun live admission, never captured logical ownership. Demand changes cannot
restart copying indefinitely. Queued staging uses retain safe retirement and
physical charge on failure. Preserve synchronous bootstrap and default loading;
enabled zero work limits pause their resource. Existing material-generation
invalidation can require material reupload at publication.

Owners: GPU admission/native growth, content and lookup writers, and app resource
recreation. Focused tests cover budgeted continuation, live edits, changed demand,
coherent bytes and failure lifetime. A user release check must confirm publication
and resource lifetime. Whole-map structural capture and global lookup rebuilding
remain separate follow-ups; this batch does not claim a total frame-time bound.

## S1k verification

2026-10-04: focused and race checks, full engine tests, five consumer builds and
the user release check passed. The diagnostic enables the existing media pass's
neutral transmittance clear required by resolve. The user confirmed continuous
animation, staged publication, resize and native retirement. Contract:
[voxel buffer creation and migration](../renderer/runtime.md#voxel-buffer-creation-and-migration).

From `gekko/`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -run '^TestS1k' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race ./voxelrt/rt/gpu -run '^(TestS1k|TestS1j|TestS1i|TestVoxelUpload)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Consumer builds passed from their respective module directories under
`/Users/ddevidch/code/go/gekko3d`:

```sh
# gekko-editor/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1k-build/editor/ ./...
# actiongame/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1k-build/actiongame/ ./...
# spacegame_go/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1k-build/spacegame/ ./...
# spacesim/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1k-build/spacesim/ ./...
# examples/testing-vox/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1k-build/testing-vox/ ./...
```

The release diagnostic builds successfully. Run it from the engine directory:

```sh
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go build -trimpath -ldflags='-s -w' -o /tmp/gekko-s1k-migration docs/roadmaps/diagnostics/s1k_migration.go
/tmp/gekko-s1k-migration
```

The left panel alternates orange/purple and opens/closes a small hole every
half-second. Press SPACE to start migration with copying paused; it must keep
animating while the right side stays empty. Resize during this pause. Press
SPACE again to replace unadmitted blue demand with green demand and resume
bounded copying. The left panel must keep animating through publication; the
green panel appears only when ready. Wait for console `PASS` and resize again.
The user reported `PASS` after this check. Automated checks establish scheduling
and byte coherence; the release check establishes this fixture's native
publication, visual continuity and retirement. No general frame-time or visual
parity claim.

## Verification and execution record

Workflow: GPT-6.1 sol tests to red; root adversarial test review; GPT-6.1 sol code to green; root adversarial production review; commit. User authorized tests/subagents. Preserve unrelated changes.

From `gekko/`: targeted scheduling/readiness, full GPU/core/app and S1a bridge suites; `go test ./...` after bridge alias changes. Use `env GOCACHE=/tmp/gekko3d-gocache`. Build editor and voxel demo. Desktop smoke establishes visual continuity; headless tests establish scheduling/state only.

- Baseline GPU/core/app suites passed on `e3f11cf`.
- Two root baseline failures remain tracked from S1a:
  `TestMovingBrushCarriesSupportedPlayerAndNPC` (player carry) and
  `TestStreamedNavigationPublishesResidencyWhileOverlayMoves` (residency).
- Tests: GPT-6.1 sol added focused GPU scheduling and core priority suites.
  Root reviewed boundary costs, shared geometry, age/removal behavior, refusal,
  empty generation-zero materials and atomic atlas recycling. Review removed
  allocation-wide equality assertion that constrained private bookkeeping.
- Red confirmed by root with
  `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu ./voxelrt/rt/core`:
  missing budget/service APIs and priority constants, with no production changes.
- Implementation: GPT-6.1 sol completed production code to green. No existing
  tests were changed. Production service follows structural preparation and
  normal-halo propagation; its executor performs actual content writes.
- Production review: root checked global remaining-budget arithmetic,
  emitted costs, material invalid/current transitions, deduplicated geometry and
  pending counts, deterministic aging and removal, atlas preflight, unit-wide
  reclamation and bridge priority compatibility.
- Review finding fixed by GPT-6.1 sol: full-sector completion scanned entire
  dirty-brick map per sector. It now deletes exactly 64 covered coordinates.
  Service owns counter resets, avoiding duplicate bookkeeping in its caller.
- Native queue write errors fail fast before service publishes completion.
  Earlier writes in that unit may already be queued; no GPU rollback is promised.
- Final review: no remaining S1b blockers. This provides bounded content writes;
  allocation/migration, CPU preparation and lookup work still need later bounds.
- Status: S1b complete, verified and reviewed.

Final commands from `gekko/`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu ./voxelrt/rt/core ./voxelrt/rt/app
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestStreamedVoxel|TestOrdinaryHiddenVoxel)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...
git diff --check
```

Scoped suites passed. Engine sweep failed only two listed root baseline cases; other packages passed. Headless executors substitute native writes, verifying service/admission/state contract.

Consumer builds, each from its own module, passed:

```sh
# gekko-editor/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1b-editor .
# examples/testing-vox/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1b-testing-vox .
```

Editor build succeeded with nonfatal sandbox module-cache stat warning. 83 local file links validated.

Physical GPU verification uses demo's existing timed-exit mode, from
`examples/testing-vox/`:

```sh
env GEKKO_BENCH_WARMUP_SECONDS=5 GEKKO_BENCH_SECONDS=2 GEKKO_BENCH_LABEL=s1b-final-smoke /tmp/gekko-s1b-testing-vox
```

Initial 20-second warmup: 100 captured frames, no reported native write errors, zero pending voxel sectors/bricks at exit. Final rebuilt artifact: 116 frames with shorter command above, zero pending work/errors, exit code zero. macOS desktop access required; sandbox launch stalled before window creation and was terminated. UI automation could not bind unbundled demo. Rendered-pixel parity, edited seams and visible parent/child handoff remain unverified. Smoke establishes native queue/frame operation, not performance gains or complete continuity.
