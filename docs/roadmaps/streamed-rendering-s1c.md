# S1c: Renderer-qualified streamed sector handoff

Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S1.
Prerequisites: [S1a](streamed-rendering-s1a.md), [S1b](streamed-rendering-s1b.md).
Contract: [island handoff](../content/island-streaming.md#no-hole-handoff-contract).

## Scope, ownership and confidence

Primary owner: `StreamedLevelRuntimeState` and its existing observer/commit
systems. The renderer remains the sole writer of ticket transitions. Streaming
owns ticket IDs, entity markers, visibility decisions and ticket retirement.
Affected consumers: streamed imported worlds/terrain, actiongame and renderer.

Known: existing v2 sector proxies hide after full chunks are ECS-loaded;
coarsening waits only for a proxy entity. Neither condition proves GPU readiness.
S1a provides hidden upload residency and revision-qualified tickets; S1b bounds
their content uploads. There is no implemented v3 page selector/forest here.

This is the long-term readiness integration for the existing sector/proxy
runtime, not a second renderer or a temporary visibility approximation.
Confidence: high after reading spawn, selection, unload, ticket and stage owners.
The viable alternative of continuing to equate ECS load with readiness breaks
coverage under bounded uploads. A v3 forest implementation requires separate
content/index/selection dependencies and is not this slice.

Manage runtime-owned terrain entities, imported full-chunk entities and sector
proxy entities. Authored placement visuals retain their existing asset/gameplay
owner. Do not implement v3 root startup gates, multi-level page selection,
cross-layer coverage groups, new worker/cache bounds, compression or shaders.
Old worlds without usable proxies keep distance streaming: this slice cannot
create fallback coverage that their content does not contain.

Required invariant: a ready fallback stays visible until its complete required
full cohort is renderer-ready; full detail stays resident until a required
fallback is renderer-ready. Visibility commands never remove collision or
navigation components/data. CPU residency/physics readiness remains distinct
from render readiness.

## Optional renderer and target ownership

- If no `VoxelRtState` resource is installed, retain the current CPU-only runtime
  behavior. Existing server/headless uses and their tests must still work.
- If the resource exists, even without an initialized renderer app/GPU manager,
  use ticket staging. Unknown status never means ready. Once the runtime has
  entered renderer-managed residency, missing renderer state cannot silently
  revert it to CPU-ready behavior.
- If a renderer is installed after CPU-only loading, adopt already resident
  runtime-owned terrain/imported/proxy targets before its first bridge pass.
  Stage them hidden until their new tickets qualify coverage.
- At commit, create a unique nonzero ticket with the current runtime generation,
  plus `StreamedVoxelRenderComponent` and `VoxelRenderHiddenComponent`, before
  the entity's first commit flush. Keep the exact requested voxel geometry.
- Ticket allocation stays monotonic across stop/start of this runtime resource.
  Avoid IDs already used by existing markers or known renderer statuses. Do not
  reset the counter with the world generation.
- Proxies use fallback priority. Full chunks requested for collision use
  collision priority; desired visual chunks use visible priority; kept-only
  chunks use keep priority. Priority-only changes preserve the ticket.
- Readiness requires a live owned entity, its matching marker/ticket/generation,
  and `Ready` status for that same entity/current generation. A stale or
  mismatched terminal status cannot prove coverage. Track each managed entity;
  a loaded chunk containing no adopted target is not sufficient proof. A
  generation-qualified, explicitly empty prepared imported result is valid
  proof of empty coverage; never infer that proof from a missing entity.
- Keep ready tickets/markers while their entities are resident. Later ordinary
  destruction edits retain S1a terminal latching and the existing dirty path.

Use a focused runtime file and small ownership records. A common private
`reconcileStreamedRenderResidency(cmd, state)` seam may serve headless functional
tests and production. Tests must not constrain the private record layout.

## Refinement and coarsening

For an enabled sector with a usable proxy LOD, the required full cohort is its
actual imported full coverage, using the existing `FullChunkRefs` index. Honor
backing/override content when deciding whether a source-empty entry is required.
Ignore genuinely empty refs. Missing loaded chunks, missing managed entities,
or missing/unfinished tickets keep the cohort unready. An authoritative empty
override/prepared result can complete formerly nonempty source coverage without
an entity. Preserve that explicit empty result instead of waiting forever or
showing old proxy mass while its resident empty replacement completes handoff.
This proof belongs to the resident full cohort. Updating baked coarse proxies
after fine edits, including after edited full coverage unloads, remains D2.

Until every required imported full entity is ready, keep full render entities
hidden and show only a ready proxy. Do not expose individual ready children
beside a partial cohort. When all are ready, unhide the full cohort and hide the
proxy in one completed ECS command flush before the next renderer bridge. Keep
the hidden ready proxy resident while its coverage is still required. Terrain
and proxy-less full chunks reveal independently after their own tickets settle.

On coarsening, if the content requires a proxy, defer full-chunk removal while
the proxy is absent, unknown, uploading, cancelled, failed or stale. Continue
requesting the missing fallback. Once ready, publish proxy visibility and
full-cohort hiding/removal together. Required ready fallback coverage cannot be
evicted merely because it is outside a stale keep set while full detail is
unready. Preserve existing keep/retain options where safe.

The existing loaded predicate remains a CPU-residency predicate for callers and
metrics. Use a distinct renderer-qualified decision for handoff/admission. Do
not make collision readiness depend on GPU progress. Existing collision and
destruction demand/upgrade semantics remain; no asynchronous physics algorithm
or navigation publication protocol is introduced here.

## Stage and flush constraints

The observer runs in `PreUpdate`; prepared commits run in `Update`; the bridge
adopts and updates tickets in `PreRender`. Streaming reads the previous renderer
update and queues visibility for the later bridge. No callbacks/fences/new
renderer transition writer are needed.

`FlushCommands` processes component removals before component additions. Its
queues do not represent arbitrary alternating visibility operations. Separate
status/priority refresh from final visibility publication. Publish each cohort's
final decision once per streaming stage, after that stage's selection/removal
or commit decisions. Avoid an early reveal followed by a conflicting hide
before the same flush. Internal spawn flushes must see new targets hidden.
Tests must exercise the actual observer/commit call paths, not only an isolated
helper. Retain the existing stage-end flush and bridge ordering.

## Failure, cancellation and lifetime

Unknown/pending/uploading targets remain hidden. A cancelled or stale owned
target gets a fresh ticket for its current source, without reloading an entire
healthy cohort. A live target with a removed marker retains streaming ownership
and restores a fresh marker. An externally removed proxy clears its stale load
record so a newly prepared fallback can be admitted while full coverage stays.
Failed adoption keeps fallback coverage and can retry when its
required geometry, palette and components are valid; do not issue endless
tickets each frame for an invalid target. Missing targets never count as ready.
The existing CPU preparation error policy is unchanged.

Retire tickets when an entity/marker is replaced, unloaded, removed or the
runtime stops. First remove/change the marker through ECS. Forget terminal
statuses only after that change has flushed. Unfinished statuses must be left
for the bridge to cancel; sweep retirement on subsequent runtime frames,
including while stopped or in an error state. Never recreate a forgotten ticket
from a still-live old marker. Keep only scalar retirement IDs, not entity/map
handles. Stop/start cannot let a delayed status activate the next world.

## Functionality tests and adversarial cases

User authorizes tests and GPT-6.1 sol delegation. Tests protect visibility,
ticket ownership/lifetime and CPU/GPU separation using actual ECS flushes and
renderer status fixtures. Do not assert private containers/helper call order.

- **Staging:** full/proxy/terrain commits with renderer installed are hidden
  before first bridge observation and carry unique generation-qualified tickets;
  headless CPU behavior stays compatible; incomplete renderer remains pending.
- **Refinement:** two required full chunks, one ready/one unknown or uploading,
  retain ready proxy and hide both children; all ready exposes only full cohort
  after one flush/scene commit; proxy remains a hidden upload resident.
- **Coverage:** empty refs may be ignored; missing required entities and
  source-empty backing/override refs cannot be ignored. A proven empty prepared
  override differs from a missing target and completes its coverage without
  respawning old source geometry. No-proxy/disabled-proxy chunks and terrain
  reveal only their own ready targets.
- **Coarsening:** loaded-but-unready proxy cannot authorize full removal;
  current ready proxy allows final reverse handoff. Test observer movement,
  fallback requesting, and a retained/pinned parent outside keep demand.
- **Faults:** unknown, failed, cancelled, wrong entity and wrong generation
  statuses cannot expose a cohort/hide its fallback. Valid changed targets
  retry with fresh IDs; invalid failed sources do not churn tickets. Priority
  refresh does not reticket healthy work.
- **Ownership:** unload/stop/restart clean markers before forgetting terminal
  status; uploading retirement waits for bridge cancellation; detached or
  externally removed entities cannot prove readiness. IDs remain nonzero,
  monotonic and avoid existing marker/status collisions.
- **Integration:** exercise observer and prepared-commit systems with explicit
  command flushes, then the actual bridge/scene lists. No early publication or
  intermediate spawn flush can expose a partial cohort. Near full chunks retain
  collision/destruction components and collision readiness while render-hidden;
  visibility handoff leaves navigation data/revision unchanged.

## Files and verification

Expected production: `streamed_level_runtime.go`, a focused streamed render
residency file, owning content/renderer docs. Reuse S1a ticket API; shader/layout
and content schema changes are not expected. New tests belong to the engine
runtime/bridge boundary; leave existing tests unchanged.

Workflow: GPT-6.1 sol writes tests to red; root adversarial test review; GPT-6.1
sol implements to green; root adversarial code review; verification; commit.

From `gekko/`, use `env GOCACHE=/tmp/gekko3d-gocache` with targeted S1c,
streamed-runtime/S1a suites, GPU/core/app suites, then the engine sweep. Build
actiongame, editor and voxel demo consumers. A real streamed GPU handoff is the
visual check; a non-streamed demo only establishes native renderer operation.
Document exactly which manual checks can run; do not claim pixel parity from
headless status fixtures.

## Execution record

- Baseline targeted streamed runtime, S1a and collision-readiness suites pass
  on `a539257`.
- Prior full engine sweeps have two unrelated baseline failures:
  `TestMovingBrushCarriesSupportedPlayerAndNPC` and
  `TestStreamedNavigationPublishesResidencyWhileOverlayMoves`.
- GPT-6.1 sol added 22 contract tests. The suite compiles and fails at the
  missing generation-qualified marker before the first spawn flush. CPU-only
  and authored-placement compatibility checks pass.
- Root test review added committed backing-only coverage and unknown/stale
  reverse handoff cases. Coarsening fixtures do not regress latched `Ready`
  tickets to unfinished states. Empty prepared results cannot stand in for
  effective backing content.
- V2 currently requests every usable sector proxy globally. The stale-keep
  test therefore covers final fallback visibility, not an unreachable proxy
  eviction branch. Actual imported commits create one entity per chunk.
- Root implementation review found missing-marker recovery, stale proxy load
  records after external removal, and late renderer adoption. Three additional
  tests reached RED before those fixes. A partial-commit lifetime test also
  covers imported spawning followed by placement failure. The stale-keep fixture
  now requests destruction residency before spawning, avoiding an unrelated CPU
  residency upgrade during observer verification.
- All 26 S1c functionality tests and the existing streamed/S1a/collision suites
  pass. GPU/core/app suites pass. The full engine sweep reports only the same two
  baseline failures listed above; every other package passes.
- Actiongame, editor and voxel example builds pass. Go emitted sandbox stat-cache
  warnings for the first two builds; both exited successfully.
- Ticket allocation reuses a stage marker floor, scans queued marker additions
  incrementally, and invalidates its floor around configured hooks. This avoids
  scanning all resident targets for every ordinary prepared commit.
- Final native verification passes on the reviewed production code: 3,074 frames
  over 105.269 seconds, process exit 0. With a 4 MiB / 1 sector / 64 record
  budget, the proxy becomes ready first. Uploads pause for 45 seconds while two
  loaded full targets stay hidden and unfinished. After resuming, one full target
  is ready while its sibling remains unfinished; both stay hidden. The complete
  cohort then replaces the proxy, which stays resident and hidden. Moving the
  observer to x=512 removes both full chunks and restores the ready proxy.
- CUA screenshot inspection confirms the full red/cyan cohort in the first run
  and the yellow fallback during the final paused run. Final refinement and
  coarsening also assert completed native scene lists every frame. No pixel
  parity or performance improvement is inferred from these checks.
- Root reviewed the recovery fixes, allocator queue/hook boundaries, stage
  publication, optional renderer behavior and scalar retirement ownership.
- Status: S1c implemented and verified.

Verification commands (all Go commands use
`env GOCACHE=/tmp/gekko3d-gocache`):

```sh
# From gekko/
go test . -run '^(TestStreamedRender|TestStreamedRuntime|TestStreamedVoxel|TestOrdinaryHiddenVoxel|TestStreamedLevelCollision)' -count=1
go test ./voxelrt/rt/gpu ./voxelrt/rt/core ./voxelrt/rt/app
go test ./...

# From each corresponding consumer module
go build -o /tmp/gekko-s1c-actiongame .
go build -o /tmp/gekko-s1c-editor .
go build -o /tmp/gekko-s1c-testing-vox .
```

Native verification uses a disposable public-API harness at
`/tmp/gekko-s1c-smoke.go` and bundle `/tmp/GekkoS1cSmoke.app`. It creates saved v2
content with two full chunks and one proxy, then reads actual renderer tickets
and completed scene lists. It does not seed status or simulate GPU writes.
The harness is a local verification artifact, not a runtime API or committed
example. The first run passed fallback/refinement/coarsening in 851 frames over
29.217 seconds. The final run passes after the recovery fixes and uses 45-second
fallback/full holds plus a 15-second coarse hold.

```sh
# From gekko/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS1cSmoke.app/Contents/MacOS/GekkoS1cSmoke /tmp/gekko-s1c-smoke.go
/tmp/GekkoS1cSmoke.app/Contents/MacOS/GekkoS1cSmoke > /tmp/gekko-s1c-final-smoke.log 2>&1
```

Limits: this slice does not establish performance gains or pixel parity. CPU
selection/readiness still scans the v2 resident sets; incremental planning is S3.
Prepared worker/cache byte bounds remain S2. Baked proxies retain their source
geometry until the edit-aware proxy work in D2. Existing collision/destruction
upgrades still unload/reload. V3 startup/forest/layer-group behavior is unimplemented.
