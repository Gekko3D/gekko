# S1b: Global voxel upload budgets and deterministic scheduling

Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S1.
Prerequisite: [S1a residency/readiness](streamed-rendering-s1a.md).
Contract: [island residency](../content/island-streaming.md#renderer-residency-contract).

## Scope and confidence

Primary owner: GPU voxel resource manager. Affected consumers: renderer app,
streamed bridge/readiness, editor and voxel demo. This is the long-term upload
scheduler required by the island design. Confidence is high after inspecting
the existing writers, allocation preparation, readiness and frame order.

Known: sector and brick limits reset inside the object loop; materials bypass
budgets; dirty-map iteration is unordered; shared geometry must upload once.
S1a already separates hidden residency from rendering and supplies scheduling
metadata. No missing architecture decision requires user alignment.

Use one scheduler per `UpdateVoxelData` invocation, which normally occurs once
per renderer frame. Keep main-thread ownership and existing writers. A common
service function selects work and completes dirty/material bookkeeping; its
executor queues GPU writes. Headless tests supply an executor to this same
service path, protecting scheduling and completion behavior without a GPU.

Independent per-object queues cannot enforce global limits. A FIFO without
aging can starve collision/detail or retained pages under sustained arrivals.
Use a deduplicated deterministic queue with aging and hard admission limits.

This slice budgets **voxel content writes**: sector/brick records, material
records, auxiliary occupancy/normals, and mixed payload texture data. Buffer
allocation/migration, sector lookup rebuilding, scene instance/BVH uploads,
queue construction, and normal-halo discovery remain outside this cap. Their
CPU/memory bounds belong to S2/S3 and later allocator work. Do not present this
as a cap on every GPU transfer or all main-thread rendering work.

No content formats, shaders, collision algorithms, page selection, worker
queues, cache eviction policy or automatic parent/child handoff change here.
Large streamed arrivals use S1a hidden tickets. Ordinary visible objects keep
their existing progressive-upload behavior; this slice adds no startup gate.

## Budget API and semantics

Add a GPU-owned public configuration value and manager setter/getter:

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

Keep `SectorsPerFrame` as the sector limit for existing callers, now global.
Store byte and brick limits in `VoxelUploadBytesPerFrame` and
`VoxelUploadBricksPerFrame`. Constructor defaults: 4 MiB, the existing
`MaxUpdatesPerFrame` sector value (1,024), and 65,536 brick records. The island
profile can explicitly choose 4 MiB, 16 sectors and 1,024 brick records; do not
install an island game profile in the GPU library.

Zero means pause that resource, not unlimited. A zero sector limit still allows
standalone brick edits and materials; a zero brick limit still allows materials;
a zero byte limit pauses all content writes. Use overflow-safe remaining-budget
comparisons. Never reset consumption inside an object/map loop.

Every full-sector unit consumes one sector and **64 brick records**, including
cleared records. A standalone brick unit consumes one brick record. Materials
consume bytes only. Counters describe all admitted writes; brick counters
include records covered by sectors. Deferred work consumes no budget.

Whole-sector units remain atomic within this service call. If an atomic unit
cannot fit the configured full-frame limits, retain it dirty and service other
eligible work. Never silently exceed the byte/work cap to force progress. The
caller must raise undersized limits; the default can fit the largest sector.

## Exact costs and completion

- Sector header: 32 bytes. Each of its 64 brick records: 32 bytes.
- Occupied solid/uniform-sparse brick: 1,088 auxiliary bytes plus its record.
- Mixed brick: another 512 payload bytes, totaling 1,632 bytes including record.
- Empty/cleared brick: record only, 32 bytes.
- Sector: header plus 64 record costs and sidecars/payloads for occupied bricks.
  Maximum current-format unit: 104,480 bytes.
- Material: 64 bytes per emitted row; empty table emits 256 zero rows; longer
  tables retain the existing cap of 256 emitted rows. Readiness uses this same
  capacity rule while preserving identity/length tracking of the source table.

Estimate from the same upload modes and layout constants used by the writers,
before baking/encoding or writing. Verify estimated content bytes equal emitted
content bytes for valid writer inputs. A failed/deferred executor must not clear
dirty work or publish current material metadata.

Allocate material slots as needed, but new/deferred material allocations must
remain distinguishable from uploaded ones, including empty tables and generation
zero. Material allocation metadata becomes current only after successful writes.

Successful sector completion consumes its sector entry and every covered dirty
brick entry. Never schedule covered standalone bricks independently, even when
their sector is deferred. Clean false/orphan dirty entries without writing.
Successful standalone completion consumes only its own dirty entry. Edits on
later frames rebuild work from current geometry rather than retained payload
snapshots. Removed objects/maps leave no scheduler ownership or age records.

Atlas pressure causes admission backpressure before any writes for a unit.
Preflight available/new/releasable payload slots and preserve dirty work when
insufficient. A replacement or cleared brick releases obsolete payload/aux slots
within a successful unit; sector replacement must match standalone replacement
semantics. No partial sector write followed by an atlas-full panic. Cache
eviction and full buffer-capacity admission remain S2 work.

## Queue order and fairness

Centralize the existing five priority values in renderer core, retaining the
public streamed enum values. New core objects default to visible priority.
The bridge keeps its existing ticket/map order semantics.

Deduplicate geometry by map pointer and work coordinate. Choose the best
priority/order among instances sharing a map. Material work stays per object.
Use map ID when object upload order is zero. Hidden resident objects participate.

Within a fresh queue: priority, upload order, map ID, work kind (material before
sector before standalone brick), then signed lexicographic sector/brick
coordinate. Equivalent per-object material ties preserve scene slice order;
streamed tickets give distinct stable order. No pointer-address ordering.

Retain the first queued frame for each outstanding work identity. Promote one
priority level every 8 waiting service frames, down to fallback priority. On
equal effective priority, older work wins before ordinary stable ties. New work
starts unaged; completion/removal drops age state. An age record keeps only
identity/time, not prepared payloads. Fit-capable work eventually receives
service under sustained higher-priority arrivals. Oversized or capacity-blocked
units cannot claim this guarantee and must not block smaller fit-capable work.

Expose `VoxelUploadBytes` and `VoxelMaterialsUploaded` beside existing upload
counters; pending sector/brick counters count shared maps once. Existing S1a
readiness remains false until geometry, material and lookup state are current.

## Functional tests and adversarial review

Tests assert service decisions, written byte/work totals, dirty/material
completion and readiness. Do not assert private container layout, helper order,
or GPU call plumbing. The private service/executor seam may use these names:
`serviceVoxelUploads(scene, execute func(voxelUploadWork) bool)`, with work kind,
object/map, coordinates and byte/work costs. Implementation may choose small
supporting types; the service must be the production scheduling path.

- **Global limits:** many maps/objects; exact boundary, one-byte deficit, zero
  limits, maximum numeric limits; sectors and covered/standalone bricks count
  together; materials share the same byte limit; fresh consumption next frame.
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
  atlas capacity blocks an atomic unit before execution; clearing/replacement
  can recover slots; object/map removal drops stale scheduler work.
- **Readiness:** deferred empty material never reports ready at generation zero;
  partial frames remain uploading; completed content plus current lookup settles;
  existing S1a terminal, ownership and visibility contracts still pass.

Expected files: GPU manager/voxel writer, a focused scheduling file, core upload
priority definitions/default, streamed enum aliases, focused GPU tests and docs.
Existing physical writers must use the service path; test-only scheduling code
is not acceptable.

## Verification and execution record

Workflow: GPT-6.1 sol writes tests and stops at red; root adversarial test review;
GPT-6.1 sol implements to green; root adversarial production review; commit.
User explicitly authorized tests and subagents. Keep unrelated working changes.

From `gekko/`, run targeted scheduling/readiness suites, full GPU/core/app suites,
S1a bridge suites, then `go test ./...` when bridge aliases change. Use
`env GOCACHE=/tmp/gekko3d-gocache`. Build editor and voxel demo consumers. A
desktop GPU smoke check is required to establish actual visual continuity;
headless tests establish scheduling and state invariants only.

- Baseline GPU/core/app suites passed on `e3f11cf`.
- Two root baseline failures remain tracked from S1a:
  `TestMovingBrushCarriesSupportedPlayerAndNPC` (player carry) and
  `TestStreamedNavigationPublishesResidencyWhileOverlayMoves` (residency).
- Tests: GPT-6.1 sol added focused GPU scheduling and core priority suites.
  Root reviewed boundary costs, shared geometry, age/removal behavior, refusal,
  empty generation-zero materials and atomic atlas recycling. The review removed
  an allocation-wide equality assertion that constrained private bookkeeping.
- Red confirmed by root with
  `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu ./voxelrt/rt/core`:
  missing budget/service APIs and priority constants, with no production changes.
- Implementation: GPT-6.1 sol completed production code to green. No existing
  tests were changed. The production service follows structural preparation and
  normal-halo propagation; its executor performs the actual content writes.
- Production review: root checked the global remaining-budget arithmetic,
  emitted costs, material invalid/current transitions, deduplicated geometry and
  pending counts, deterministic aging and removal, atlas preflight, unit-wide
  reclamation and bridge priority compatibility.
- Review finding fixed by GPT-6.1 sol: full-sector completion scanned the entire
  dirty-brick map per sector. It now deletes exactly the 64 covered coordinates.
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

Scoped suites passed. The engine sweep failed only in the two root baseline
cases listed above; every other engine package passed. Tests establish the
service/admission/state contract; headless executors substitute for native writes.

Consumer builds, each from its own module, passed:

```sh
# gekko-editor/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1b-editor .
# examples/testing-vox/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1b-testing-vox .
```

The editor build emitted a nonfatal module-cache stat warning under the sandbox
and exited successfully. Document validation checked 83 local file links.

Physical GPU verification uses the demo's existing timed-exit mode, from
`examples/testing-vox/`:

```sh
env GEKKO_BENCH_WARMUP_SECONDS=5 GEKKO_BENCH_SECONDS=2 GEKKO_BENCH_LABEL=s1b-final-smoke /tmp/gekko-s1b-testing-vox
```

An initial 20-second warmup smoke completed 100 captured frames with no reported
native write errors and zero pending voxel sectors/bricks at exit. The final
rebuilt artifact completed 116 captured frames with the shorter command above,
zero pending voxel work, no reported write errors and exit code zero. Launch
needs macOS desktop access; a sandboxed launch stalled before window creation and was
terminated. UI automation could not bind the unbundled demo executable, so
rendered-pixel parity, edited seams and a visible parent/child handoff remain
unverified. The smoke establishes native queue/frame operation, not performance
gains or complete visual continuity.
