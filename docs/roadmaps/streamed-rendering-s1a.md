# S1a: Staged voxel residency and upload readiness

Parent: [streamed rendering optimization](streamed-rendering-content-optimization.md), S1.
Contract: [island renderer residency](../content/island-streaming.md#renderer-residency-contract).

## Scope and architecture

Implement the prerequisite for bounded uploads: a streamed entity can remain
hidden while its geometry and materials upload. Expose a revision-qualified
ticket status so the streaming owner can retain existing coverage until the
replacement is ready.

Primary owner: renderer bridge, with scene visibility and GPU allocation support.
Affected consumers: streamed runtime, editor and voxel applications. This is a
long-term architecture step using the existing main-thread bridge. Confidence:
high. The island design specifies the API and frame order; source inspection
confirms that ordinary hidden entities currently lose renderer residency.

Alternatives: removing hidden entities prevents background uploads; rendering
partially uploaded entities breaks coverage. Keep residency separate from render
visibility as specified in the island design. No callbacks, GPU fences, ECS
events, or second upload owner are needed.

This slice adds the residency marker, status lifecycle, scheduling metadata and
readiness predicate. S1b will implement global upload budgets and queue ordering.
Page selection, automatic parent/child handoff, caches, compression, collision
representation and destruction algorithms remain later steps. Existing upload
limits retain their current behavior in S1a.

## Contracts

1. `core.NewVoxelObject()` is render-enabled. Ordinary hidden ECS entities still
   leave `Scene.Objects`. A hidden entity with `StreamedVoxelRenderComponent`
   stays resident and uploadable, with `RenderEnabled = false`.
2. Disabled resident objects never enter visible, transparent, shadow or render
   BVH lists, nor contribute to visibility/Hi-Z statistics. Changing visibility
   takes effect on the next scene commit, without replacing geometry. CPU voxel
   collision and ray queries remain independent of this render flag.
3. Use the priority, component, status enum and public methods specified in the
   island design. Copy priority and ticket to object scheduling metadata.
   Non-streamed objects use visible priority and map ID for stable order.
4. A ticket is unknown before the bridge observes its component. The bridge
   adopts it through `PendingBridge` into `Uploading`, capturing the actual
   object geometry pointer, map ID and revision. Staged entities use their
   requested voxel geometry, bypassing automatic entity proxy/impostor LOD.
5. Generation belongs to the component owner. Return that generation unchanged;
   consumers must compare it with their current streaming generation. Changing
   the component generation or ticket cancels an unfinished old ticket. Tickets
   are nonzero, unique and monotonically increasing within their owner.
   An observed ticket cannot be rebound to another entity or generation.
6. Before readiness, replacing the object geometry or changing its revision
   cancels the ticket. Removing its entity, marker or required transform/model
   also cancels it. A new ticket is required for a new target. Missing geometry
   or a missing explicitly referenced palette fails adoption with a diagnostic.
   A marker lacking its required transform/model on initial observation also
   fails diagnostically. A zero palette reference retains the existing
   default-material behavior.
7. Refresh uploading status immediately after `RtApp.Update()`. The GPU predicate
   requires the captured map/revision, complete allocation counts, clean
   structure and dirty queues, a current material allocation, and current sector
   lookup topology. Missing renderer allocation state never reports ready.
   Pending sector/brick counts describe outstanding dirty entries. Conservative
   counts may include entries awaiting normal queue cleanup. Whole-sector
   uploads consume covered dirty-brick entries, including cleared records.
8. Readiness inspection is constant-time per ticket, without scanning map
   sectors. `Ready` means queue writes precede the next render; it does not mean
   GPU completion. A missing GPU manager keeps a valid ticket uploading.
9. `Ready` is terminal and latched. Later ordinary voxel edits do not regress it.
   Cancelled and failed tickets also remain terminal. `ForgetStreamedVoxel`
   deletes only terminal records; unknown and unfinished tickets are unaffected.
   The owner cleans up a terminal marker before forgetting its record. Terminal
   records release captured geometry handles while keeping value diagnostics.

## Functional tests and adversarial cases

Tests protect public ticket behavior and the shared residency/visibility
invariant. Headless GPU fixtures may seed allocation state to test the readiness
predicate; assertions must concern readiness, not private field layout.

- **Visibility:** default object renders; disabled opaque/transparent residents
  retain scene ownership but produce no render/shadow lists or visibility counts;
  enabling, disabling and re-enabling rebuild the appropriate BVHs. Ordinary
  hidden entities still lose residency. CPU ray queries still use authoritative
  resident geometry.
- **Bridge adoption:** hidden marked entity stays resident; unhide preserves
  object identity and target; priority/order propagate; no pre-observation
  status; map ID/revision/generation match the adopted geometry; staged entity
  does not become a sprite or simplified automatic LOD.
- **Readiness:** missing or partial allocation, dirty topology, dirty sector,
  dirty brick, stale material identity/generation and stale lookup topology each
  prevent readiness. Clean complete targets become ready. Counts remain useful.
  Empty geometry and multiple objects sharing one map have explicit coverage.
  A different map or revision never satisfies an old target.
- **Lifecycle:** no manager cannot cause false readiness; initial uploading can
  become ready; ready stays latched after edits; geometry/revision changes cancel
  unfinished tickets; entity/marker/component removal cancels; generation and
  ticket changes cannot revive stale work; missing assets fail diagnostically;
  duplicate tickets cannot steal ownership; terminal forgetting is safe.
- **Coverage handoff:** one command flush can hide a resident parent and reveal
  a resident child, then one bridge/scene commit exposes only the final set.
  This tests the primitive; automatic page handoff is outside this slice.

## Files and verification

Expected production changes: `mod_voxelrt_client.go`,
`mod_voxelrt_client_systems.go`, a focused streamed-render bridge file,
`voxelrt/rt/core/scene.go`, `voxelrt/rt/gpu/manager_voxel.go`, and a GPU readiness file.
Tests belong to the engine bridge, core scene and GPU manager packages.

Workflow: GPT-6.1 sol writes tests and stops at red; root reviews functionality
and architecture; GPT-6.1 sol implements code to green; root reviews again and
commits the slice. Test changes are explicitly authorized by the user.

Verification commands, from `gekko/`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . ./voxelrt/rt/core ./voxelrt/rt/gpu
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Compile the voxel demo and editor consumers after bridge changes. No format or
shader layout changes are expected. Automated tests cannot establish real GPU
queue ordering or visual continuity; a desktop smoke check should stage hidden
geometry, wait for readiness and reveal it while retaining parent coverage.

## Execution record

- Baseline: `go test ./voxelrt/rt/gpu ./voxelrt/rt/core ./voxelrt/rt/volume` passed.
- Root baseline: `go test .` failed before S1a code existed in
  `TestMovingBrushCarriesSupportedPlayerAndNPC` and
  `TestStreamedNavigationPublishesResidencyWhileOverlayMoves`. Record these
  separately from S1a regressions during final verification.
- Test phase: GPT-6.1 sol added three focused suites; root independently confirmed
  compile-red on the absent residency/readiness API.
- Test review, round 1: reacquire ECS component pointers after archetype-changing
  command flushes; make completed-upload fixtures reflect actual sector brick
  pointers; cover malformed initial adoption. Production code remains untouched.
- Test review, round 2: fixes inspected; functional coverage approved. Tests
  retained compile-red on the absent production API.
- Follow-up test review during compilation: corrected existing asset ID and
  `SharedGeometry` API usage. Atomic handoff uses an ECS light fixture so bridge
  sync cannot erase the fixture's shadow light. Behavioral assertions remained
  intact.
- Implementation: GPT-6.1 sol completed the slice and stopped at green.
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

Scoped tests and full core/GPU suites passed. The engine sweep still fails only
in the two root baseline cases named above; all other engine packages passed.
The unrelated baseline failures were reproduced separately before S1a code.

Consumer build commands, each from its own module, passed:

```sh
# examples/testing-vox/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1a-testing-vox .
# gekko-editor/
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-s1a-editor .
```

Document validation checked 45 local links. No physical GPU/desktop smoke check
or performance benchmark was run. Tests establish the headless state contract;
visual continuity and actual GPU queue behavior remain manual verification.
