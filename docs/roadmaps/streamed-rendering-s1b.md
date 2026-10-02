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
