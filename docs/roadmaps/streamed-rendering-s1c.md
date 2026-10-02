# S1c: Renderer-qualified streamed sector handoff

Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S1.
Prerequisites: [S1a](streamed-rendering-s1a.md), [S1b](streamed-rendering-s1b.md).
Contract: [island handoff](../content/island-streaming.md#no-hole-handoff-contract).

## Scope, ownership and confidence

Owner: `StreamedLevelRuntimeState`, existing observer/commit systems. Renderer alone writes ticket transitions. Streaming owns ticket IDs, entity markers, visibility and retirement. Consumers: streamed imported worlds/terrain, actiongame and renderer.

Known: v2 sector proxies hide when full chunks are ECS-loaded; coarsening waits for proxy entity. Neither proves GPU readiness. S1a provides hidden upload residency and revision-qualified tickets; S1b bounds content uploads. No v3 page selector/forest implemented here.

Long-term readiness integration for existing sector/proxy runtime. No second renderer or temporary visibility approximation. Confidence: high after spawn, selection, unload, ticket and stage inspection. Equating ECS load with readiness breaks bounded-upload coverage. V3 forest needs separate content/index/selection dependencies.

Manage runtime-owned terrain, imported full chunks and sector proxies. Authored placement visuals retain asset/gameplay owner. Exclude v3 startup gates, multi-level selection, cross-layer coverage groups, worker/cache bounds, compression and shaders. Proxy-less worlds retain distance streaming; absent fallback content cannot gain coverage.

Invariant: ready fallback stays visible until complete required full cohort is renderer-ready. Retain full detail until required fallback is renderer-ready. Visibility never removes collision/navigation data or components. CPU residency/physics readiness stays separate from render readiness.

## Optional renderer and target ownership

- If no `VoxelRtState` resource is installed, retain current CPU-only runtime
  behavior. Existing server/headless uses and their tests must still work.
- If resource exists, even without initialized renderer app/GPU manager,
  use ticket staging. Unknown status never means ready. Once runtime has
  entered renderer-managed residency, missing renderer state cannot silently
  revert that active world to CPU-ready behavior. S1e determines this latch anew
  at successful Start; previous-world retirement does not force a new headless
  world into GPU staging.
- If renderer is installed after CPU-only loading, adopt already resident
  runtime-owned terrain/imported/proxy targets before its first bridge pass.
  Stage them hidden until their new tickets qualify coverage.
- At commit, create unique nonzero ticket with current runtime generation,
  plus `StreamedVoxelRenderComponent` and `VoxelRenderHiddenComponent`, before
  entity's first commit flush. Keep exact requested voxel geometry.
- Ticket allocation stays monotonic across stop/start of this runtime resource.
  Avoid IDs already used by existing markers or known renderer statuses. Do not
  reset counter with world generation.
- Proxies use fallback priority. Full chunks requested for collision use
  collision priority; desired visual chunks use visible priority; kept-only
  chunks use keep priority. Priority-only changes preserve ticket.
- Readiness requires live owned entity, its matching marker/ticket/generation,
  and `Ready` status for that same entity/current generation. Stale or
  mismatched terminal status cannot prove coverage. Track each managed entity;
  loaded chunk containing no adopted target is not sufficient proof.
  generation-qualified, explicitly empty prepared imported result is valid
  proof of empty coverage; never infer that proof from missing entity.
- Keep ready tickets/markers while their entities are resident. Later ordinary
  destruction edits retain S1a terminal latching and existing dirty path.

Use focused runtime file and small ownership records. Private `reconcileStreamedRenderResidency(cmd, state)` may serve production/headless tests. Tests must not constrain private layout.

## Refinement and coarsening

For enabled sector with usable proxy LOD, required full cohort is its
actual imported full coverage, using existing `FullChunkRefs` index. Honor
backing/override content when deciding whether source-empty entry is required.
Ignore genuinely empty refs. Missing loaded chunks, missing managed entities,
or missing/unfinished tickets keep cohort unready. Authoritative empty
override/prepared result can complete formerly nonempty source coverage without
entity. Preserve that explicit empty result instead of waiting forever or
showing old proxy mass while its resident empty replacement completes handoff.
This proof belongs to resident full cohort. Updating baked coarse proxies
after fine edits, including after edited full coverage unloads, remains D2.

Until every required imported full entity is ready, keep full render entities
hidden and show only ready proxy. Do not expose individual ready children
beside partial cohort. When all are ready, unhide full cohort and hide
proxy in one completed ECS command flush before next renderer bridge. Keep
hidden ready proxy resident while its coverage is still required. Terrain
and proxy-less full chunks reveal independently after their own tickets settle.

On coarsening, if content requires proxy, defer full-chunk removal while
proxy is absent, unknown, uploading, cancelled, failed or stale. Continue
requesting missing fallback. Once ready, publish proxy visibility and
full-cohort hiding/removal together. Required ready fallback coverage cannot be
evicted merely because it is outside stale keep set while full detail is
unready. Preserve existing keep/retain options where safe.

Loaded predicate remains CPU residency for callers/metrics. Handoff/admission uses distinct renderer-qualified decision. Collision readiness must not depend on GPU progress. Retain collision/destruction demand and upgrades; no new async physics or navigation publication protocol.

## Stage and flush constraints

Observer: `PreUpdate`; prepared commits: `Update`; bridge adoption/ticket updates: `PreRender`. Streaming reads previous renderer update and queues visibility for later bridge. No callbacks, fences or new ticket-transition writer needed.

`FlushCommands` processes component removals before component additions. Its
queues do not represent arbitrary alternating visibility operations. Separate
status/priority refresh from final visibility publication. Publish each cohort's
final decision once per streaming stage, after that stage's selection/removal
or commit decisions. Avoid early reveal followed by conflicting hide
before same flush. Internal spawn flushes must see new targets hidden.
Tests must exercise actual observer/commit call paths, not only isolated
helper. Retain existing stage-end flush and bridge ordering.

## Failure, cancellation and lifetime

Unknown/pending/uploading targets remain hidden. Cancelled or stale owned
target gets fresh ticket for its current source, without reloading entire
healthy cohort. Live target with removed marker retains streaming ownership
and restores fresh marker. Externally removed proxy clears its stale load
record so newly prepared fallback can be admitted while full coverage stays.
Failed adoption keeps fallback coverage and can retry when its
required geometry, palette and components are valid; do not issue endless
tickets each frame for invalid target. Missing targets never count as ready.
Existing CPU preparation error policy is unchanged.

Retire tickets when entity/marker is replaced, unloaded, removed or
runtime stops. First remove/change marker through ECS. Forget terminal
statuses only after that change has flushed. Unfinished statuses must be left
for bridge to cancel; sweep retirement on subsequent runtime frames,
including while stopped or in error state. Never recreate forgotten ticket
from still-live old marker. Keep only scalar retirement IDs, not entity/map
handles. Stop/start cannot let delayed status activate next world.

## Functionality tests and adversarial cases

User authorizes tests and GPT-6.1 sol delegation. Protect visibility, ticket ownership/lifetime and CPU/GPU separation through actual ECS flushes and renderer status fixtures. Never assert private containers/helper order.

- **Staging:** full/proxy/terrain commits with renderer installed are hidden
  before first bridge observation and carry unique generation-qualified tickets;
  headless CPU behavior stays compatible; incomplete renderer remains pending.
- **Refinement:** two required full chunks, one ready/one unknown or uploading,
  retain ready proxy and hide both children; all ready exposes only full cohort
  after one flush/scene commit; proxy remains hidden upload resident.
- **Coverage:** empty refs may be ignored; missing required entities and
  source-empty backing/override refs cannot be ignored. Proven empty prepared
  override differs from missing target and completes its coverage without
  respawning old source geometry. No-proxy/disabled-proxy chunks and terrain
  reveal only their own ready targets.
- **Coarsening:** loaded-but-unready proxy cannot authorize full removal;
  current ready proxy allows final reverse handoff. Test observer movement,
  fallback requesting, and retained/pinned parent outside keep demand.
- **Faults:** unknown, failed, cancelled, wrong entity and wrong generation
  statuses cannot expose cohort/hide its fallback. Valid changed targets
  retry with fresh IDs; invalid failed sources do not churn tickets. Priority
  refresh does not reticket healthy work.
- **Ownership:** unload/stop/restart clean markers before forgetting terminal
  status; uploading retirement waits for bridge cancellation; detached or
  externally removed entities cannot prove readiness. IDs remain nonzero,
  monotonic and avoid existing marker/status collisions.
- **Integration:** exercise observer and prepared-commit systems with explicit
  command flushes, then actual bridge/scene lists. No early publication or
  intermediate spawn flush can expose partial cohort. Near full chunks retain
  collision/destruction components and collision readiness while render-hidden;
  visibility handoff leaves navigation data/revision unchanged.

## Files and verification

Files: `streamed_level_runtime.go`, focused streamed render residency, owning content/renderer docs. Reuse S1a ticket API. No expected shader/layout/schema changes. New tests: runtime/bridge boundary; existing tests unchanged.

Workflow: GPT-6.1 sol tests to red; root adversarial test review; GPT-6.1 sol code to green; root adversarial code review; verification; commit.

From `gekko/`, use `env GOCACHE=/tmp/gekko3d-gocache`: targeted S1c, streamed-runtime/S1a, GPU/core/app, engine sweep. Build actiongame, editor and voxel demo. Real streamed GPU handoff checks visuals; non-streamed demo establishes native operation only. Record available manual checks; headless fixtures cannot prove pixel parity.

## Execution record

- Baseline targeted streamed runtime, S1a and collision-readiness suites pass
  on `a539257`.
- Prior full engine sweeps have two unrelated baseline failures:
  `TestMovingBrushCarriesSupportedPlayerAndNPC` and
  `TestStreamedNavigationPublishesResidencyWhileOverlayMoves`.
- GPT-6.1 sol added 22 contract tests. Suite compiles and fails at
  missing generation-qualified marker before first spawn flush. CPU-only
  and authored-placement compatibility checks pass.
- Root test review added committed backing-only coverage and unknown/stale
  reverse handoff cases. Coarsening fixtures do not regress latched `Ready`
  tickets to unfinished states. Empty prepared results cannot stand in for
  effective backing content.
- V2 currently requests every usable sector proxy globally. Stale-keep
  test therefore covers final fallback visibility, not unreachable proxy
  eviction branch. Actual imported commits create one entity per chunk.
- Root implementation review found missing-marker recovery, stale proxy load
  records after external removal, and late renderer adoption. Three additional
  tests reached RED before those fixes. Partial-commit lifetime test also
  covers imported spawning followed by placement failure. Stale-keep fixture
  now requests destruction residency before spawning, avoiding unrelated CPU
  residency upgrade during observer verification.
- All 26 S1c functionality tests and existing streamed/S1a/collision suites
  pass. GPU/core/app suites pass. Full engine sweep reports only same two
  baseline failures listed above; every other package passes.
- Actiongame, editor and voxel example builds pass. Go emitted sandbox stat-cache
  warnings for first two builds; both exited successfully.
- Ticket allocation reuses stage marker floor, scans queued marker additions
  incrementally, and invalidates its floor around configured hooks. This avoids
  scanning all resident targets for every ordinary prepared commit.
- Final native verification passes on reviewed production code: 3,074 frames
  over 105.269 seconds, process exit 0. With 4 MiB / 1 sector / 64 record
  budget, proxy becomes ready first. Uploads pause for 45 seconds while two
  loaded full targets stay hidden and unfinished. After resuming, one full target
  is ready while its sibling remains unfinished; both stay hidden. Complete
  cohort then replaces proxy, which stays resident and hidden. Moving
  observer to x=512 removes both full chunks and restores ready proxy.
- CUA screenshot inspection confirms full red/cyan cohort in first run
  and yellow fallback during final paused run. Final refinement and
  coarsening also assert completed native scene lists every frame. No pixel
  parity or performance improvement is inferred from these checks.
- Root reviewed recovery fixes, allocator queue/hook boundaries, stage
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

Native verification: disposable public-API harness `/tmp/gekko-s1c-smoke.go`, bundle `/tmp/GekkoS1cSmoke.app`. Saved v2 content has two full chunks and one proxy. Harness reads actual tickets/completed scene lists; no seeded status or simulated writes. Local artifact, not runtime API or committed example. First run passed fallback/refinement/coarsening: 851 frames, 29.217 seconds. Final run passes recovery fixes with 45-second fallback/full holds and 15-second coarse hold.

```sh
# From gekko/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS1cSmoke.app/Contents/MacOS/GekkoS1cSmoke /tmp/gekko-s1c-smoke.go
/tmp/GekkoS1cSmoke.app/Contents/MacOS/GekkoS1cSmoke > /tmp/gekko-s1c-final-smoke.log 2>&1
```

Limits: no performance gain or pixel parity established. CPU selection/readiness scans v2 resident sets; incremental planning remains S3. Worker/cache byte bounds remain S2. Baked proxies retain source geometry until D2 edit-aware proxy work. Collision/destruction upgrades still unload/reload. V3 startup/forest/layer-group behavior unimplemented.
