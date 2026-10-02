# S1a: Staged voxel residency and upload readiness

Parent: [streamed rendering optimization](streamed-rendering-content-optimization.md), S1.
Contract: [island renderer residency](../content/island-streaming.md#renderer-residency-contract).

## Scope and architecture

Prerequisite for bounded uploads: keep streamed entities hidden while geometry and materials upload. Expose revision-qualified ticket status; retain existing coverage until replacement is ready.

Owner: renderer bridge, scene visibility and GPU allocation. Consumers: streamed runtime, editor and voxel applications. Long-term step through existing main-thread bridge. Confidence: high. Island design specifies API/frame order; ordinary hidden entities currently lose renderer residency.

Removing hidden entities prevents background uploads; rendering partial uploads breaks coverage. Separate residency from visibility per island design. No callbacks, GPU fences, ECS events or second upload owner needed.

Add residency marker, status lifecycle, scheduling metadata and readiness predicate. S1b adds global upload budgets and queue ordering. Page selection, automatic parent/child handoff, caches, compression, collision representation and destruction remain later steps. S1a preserves current upload limits.

## Contracts

1. `core.NewVoxelObject()` is render-enabled. Ordinary hidden ECS entities still
   leave `Scene.Objects`. Hidden entity with `StreamedVoxelRenderComponent`
   stays resident and uploadable, with `RenderEnabled = false`.
2. Disabled resident objects never enter visible, transparent, shadow or render
   BVH lists, nor contribute to visibility/Hi-Z statistics. Changing visibility
   takes effect on next scene commit, without replacing geometry. CPU voxel
   collision and ray queries remain independent of this render flag.
3. Use priority, component, status enum and public methods specified in
   island design. Copy priority and ticket to object scheduling metadata.
   Non-streamed objects use visible priority and map ID for stable order.
4. Ticket is unknown before bridge observes its component. Bridge
   adopts it through `PendingBridge` into `Uploading`, capturing actual
   object geometry pointer, map ID and revision. Staged entities use their
   requested voxel geometry, bypassing automatic entity proxy/impostor LOD.
5. Generation belongs to component owner. Return that generation unchanged;
   consumers must compare it with their current streaming generation. Changing
   component generation or ticket cancels unfinished old ticket. Tickets
   are nonzero, unique and monotonically increasing within their owner.
   Observed ticket cannot be rebound to another entity or generation.
6. Before readiness, replacing object geometry or changing its revision
   cancels ticket. Removing its entity, marker or required transform/model
   also cancels it. New ticket is required for new target. Missing geometry
   or missing explicitly referenced palette fails adoption with diagnostic.
   Marker lacking its required transform/model on initial observation also
   fails diagnostically. Zero palette reference retains existing
   default-material behavior.
7. Refresh uploading status immediately after `RtApp.Update()`. GPU predicate
   requires captured map/revision, complete allocation counts, clean
   structure and dirty queues, current material allocation, and current sector
   lookup topology. Missing renderer allocation state never reports ready.
   Pending sector/brick counts describe outstanding dirty entries. Conservative
   counts may include entries awaiting normal queue cleanup. Whole-sector
   uploads consume covered dirty-brick entries, including cleared records.
8. Readiness inspection is constant-time per ticket, without scanning map
   sectors. `Ready` means queue writes precede next render; it does not mean
   GPU completion. Missing GPU manager keeps valid ticket uploading.
9. `Ready` is terminal and latched. Later ordinary voxel edits do not regress it.
   Cancelled and failed tickets also remain terminal. `ForgetStreamedVoxel`
   deletes only terminal records; unknown and unfinished tickets are unaffected.
   Owner cleans up terminal marker before forgetting its record. Terminal
   records release captured geometry handles while keeping value diagnostics.

## Functional tests and adversarial cases

Tests protect public tickets and shared residency/visibility invariant. Headless GPU fixtures may seed allocation state; assert readiness, not private layout.

- **Visibility:** default object renders; disabled opaque/transparent residents
  retain scene ownership but produce no render/shadow lists or visibility counts;
  enabling, disabling and re-enabling rebuild appropriate BVHs. Ordinary
  hidden entities still lose residency. CPU ray queries still use authoritative
  resident geometry.
- **Bridge adoption:** hidden marked entity stays resident; unhide preserves
  object identity and target; priority/order propagate; no pre-observation
  status; map ID/revision/generation match adopted geometry; staged entity
  does not become sprite or simplified automatic LOD.
- **Readiness:** missing or partial allocation, dirty topology, dirty sector,
  dirty brick, stale material identity/generation and stale lookup topology each
  prevent readiness. Clean complete targets become ready. Counts remain useful.
  Empty geometry and multiple objects sharing one map have explicit coverage.
  Different map or revision never satisfies old target.
- **Lifecycle:** no manager cannot cause false readiness; initial uploading can
  become ready; ready stays latched after edits; geometry/revision changes cancel
  unfinished tickets; entity/marker/component removal cancels; generation and
  ticket changes cannot revive stale work; missing assets fail diagnostically;
  duplicate tickets cannot steal ownership; terminal forgetting is safe.
- **Coverage handoff:** one command flush can hide resident parent and reveal
  resident child, then one bridge/scene commit exposes only final set.
  This tests primitive; automatic page handoff is outside this slice.

## Files and verification

Files: `mod_voxelrt_client.go`, `mod_voxelrt_client_systems.go`, focused streamed-render bridge, `voxelrt/rt/core/scene.go`, `voxelrt/rt/gpu/manager_voxel.go`, GPU readiness. Tests: engine bridge, core scene and GPU manager.

Workflow: GPT-6.1 sol tests to red; root reviews functionality/architecture; GPT-6.1 sol code to green; root reviews and commits. User explicitly authorized tests.

Verification commands, from `gekko/`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . ./voxelrt/rt/core ./voxelrt/rt/gpu
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Build voxel demo and editor after bridge changes. No format/shader layout changes expected. Automated tests cannot establish native GPU queue order or visual continuity. Desktop smoke: stage hidden geometry, wait for readiness, reveal while retaining parent coverage.

## Execution record

- Baseline: `go test ./voxelrt/rt/gpu ./voxelrt/rt/core ./voxelrt/rt/volume` passed.
- Root baseline: `go test .` failed before S1a code existed in
  `TestMovingBrushCarriesSupportedPlayerAndNPC` and
  `TestStreamedNavigationPublishesResidencyWhileOverlayMoves`. Record these
  separately from S1a regressions during final verification.
- Test phase: GPT-6.1 sol added three focused suites; root independently confirmed
  compile-red on absent residency/readiness API.
- Test review, round 1: reacquire ECS component pointers after archetype-changing
  command flushes; make completed-upload fixtures reflect actual sector brick
  pointers; cover malformed initial adoption. Production code remains untouched.
- Test review, round 2: fixes inspected; functional coverage approved. Tests
  retained compile-red on absent production API.
- Follow-up test review during compilation: corrected existing asset ID and
  `SharedGeometry` API usage. Atomic handoff uses ECS light fixture so bridge
  sync cannot erase fixture's shadow light. Behavioral assertions remained
  intact.
- Implementation: GPT-6.1 sol completed slice and stopped at green.
- Production review: root checked hidden residency, all render lists, CPU query
  authority, captured ownership, terminal latching, exact map/revision checks,
  constant-time readiness, upload bookkeeping and refresh after renderer update.
  One lifetime finding was fixed: terminal records release captured geometry.
- Final review: no remaining S1a blockers. Global budgeting and page-runtime
  adoption remain S1b and later work.
- Status: S1a complete, committed as `e3f11cf`.

Final commands from `gekko/`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . ./voxelrt/rt/core ./voxelrt/rt/gpu -run '^(TestStreamedVoxel|TestOrdinaryHiddenVoxel|TestRenderResidency|TestRenderDisabledResident|TestVoxelObjectReady)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core ./voxelrt/rt/gpu
env GOCACHE=/tmp/gekko3d-gocache go test ./...
git diff --check
```

Scoped and full core/GPU suites passed. Engine sweep fails only two named root baseline cases; other packages passed. Both failures reproduced before S1a code.

Consumer build commands, each from its own module, passed:

```sh
# examples/testing-vox/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1a-testing-vox .
# gekko-editor/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1a-editor .
```

45 local links validated. No physical GPU/desktop smoke or performance benchmark run. Headless state contract verified; visual continuity and native GPU queue behavior remain manual checks.