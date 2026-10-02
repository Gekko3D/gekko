# S3d: Incremental hierarchy propagation

Date: 2026-10-02. Status: implemented and verified; S3 remains partial.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S3.
Prerequisite: S3c committed structural revisions.

## Scope and confidence

Owners: ECS storage and `TransformHierarchySystem`. Consumers: animation, attachments, roots, spatial/gameplay transforms and voxel extraction. Confidence: High after storage, flushes, hierarchy, direct-call helpers and bridge timing inspection. SME alignment: No. Missing `base/skills-manifest.md`; workflow/owning docs read.

Known: hierarchy rebuilds world/child/resolution maps and composes every resolvable child each call. Animation/consumers write local/world/parent fields directly; attachment helpers invoke hierarchy repeatedly per frame. S3c invalidates membership, not component values.

Compare live inputs at hierarchy owner; reuse topology/composition. Permanent improvement, not general changed-component API or extraction bypass. Read all relevant values each invocation. Full notifications/incremental extraction remain S3; speedup and peak capacity unknown.

## Architecture and invariants

1. Preserve public `TransformHierarchySystem(*Commands)` signature, module
   installation without another resource, `PostUpdate` stage and immediate
   direct calls. Do not flush commands. Enqueued changes become visible only
   at existing stage flush.
2. Private hierarchy state belongs to shared `ecsStorage`, so copied ECS wrappers
   share one owner while independent storages cannot share cached membership,
   type IDs, topology, inputs or counters. Expose nil-safe main-thread
   `Ecs.TransformHierarchyStats()` and `Commands.TransformHierarchyStats()`.
   Return value `TransformHierarchyStats` with cumulative `uint64`
   `TopologyBuildCount` and `CompositionCount`, and current `int`
   `TransformCount`. Reads do not trigger preparation. First/changed empty
   topology builds count; composing resolvable child counts once even if its
   output happens to equal its previous value. Root mirroring does not count.
3. Cache every entity with `TransformComponent`, including parent-only entities,
   by committed structural revision. Local/Parent presence determines existing
   roles. Child requires all three components. Entity with Parent and
   world but no local is still valid world-space parent. World+local entity
   without Parent mirrors authoritative world TRS into local on every call.
4. Keep only entity identities, row locations, archetype references and value
   snapshots between calls. Reacquire current typed columns each invocation;
   retain no component pointers or typed-slice aliases. Structural migrations,
   replacements, same-archetype growth and recycled rows invalidate locations.
5. Scan live Parent values each call. Any child-parent identity change rebuilds
   resolution order without requiring structural revision. Detect every edge
   change before composition. Memoize valid and invalid paths once during
   rebuilding; use iterative traversal rather than unbounded recursion. Build order
   where each resolvable ancestor precedes descendants. Missing parents, cycles
   (including self-parenting), and their descendants keep their current world
   TRS unchanged. Repairing link or committed membership resumes propagation
   in same invocation. Do not use stale pre-composition parent snapshot:
   each child's comparison/composition sees its parent's newly resolved world.
6. Compare exact float bits of parent world TRS, child local TRS and last
   produced world TRS. Skip composition only when all match and child is
   currently resolvable. Repair direct child-world TRS writes even when inputs
   have not changed. Clear validity for unresolved children so later recovery
   cannot reuse stale output ownership. Membership/topology changes may
   conservatively invalidate composition snapshots.
7. Preserve current formula and arithmetic order: scale local position by
   parent scale, rotate it, add parent position; multiply and normalize
   rotations; multiply scales componentwise. Parent and child render Pivot are
   excluded from hierarchy inputs. Never change Pivot while composing TRS.
   Compare bits to distinguish signed zero and avoid repeated work for unchanged
   NaN bit patterns; do not add transform sanitization or change numerical policy.
8. Release obsolete entity and archetype references, including backing-array
   tails. Empty topology releases aggregate capacity. Nonempty storage may
   retain peak capacity; this is not memory ceiling. No worker or new lock
   mutates this owner. Existing main-thread query mutation rules apply.

Mandatory setters need engine/editor/game migration; counters remove no work; structural revision misses local/Parent writes. Exact authoritative inputs preserve compatibility and remove repeated composition before broader notification migration.

## Functionality tests and corner cases

User-authorized Sol 6.1 tests-first, adversarial review, implementation, review and commit. Add focused tests; existing tests unchanged. Protect world/local behavior and public work/ownership counters, never private layout/rows/traversal order. Use independent expected transforms/literal results.

- First/idle and repeated same-frame calls: real deep chain plus independent
  branches composes correctly once; unchanged calls add no topology/composition
  work. Root local mirrors direct world changes and ignores local edits.
- Direct TRS edits: parent position/rotation/nonuniform scale, local TRS and
  descendant motion appear in same call. Value-only edits reuse topology;
  unchanged branches add no composition work. Repair direct child world writes.
- Pivot exclusion and exact bits: renderer Pivot changes leave child hierarchy
  outputs/work unchanged; signed-zero changes are observed; unchanged NaN input
  bits stabilize idle work without numerical sanitization.
- Parent semantics: direct reparenting rebuilds topology; preserving-world helper
  retains world pose. Transform-only parents and Parent+World without Local
  remain usable; absent parent/world/local semantics match current hierarchy.
- Invalid graphs: missing parent, self/multi-node cycle and descendants do not
  partially compose. Unrelated valid branches continue. Direct/committed repair,
  removal of parent and re-admission recover immediately without stale output.
- Structural boundaries: queued insertion/replacement/removal remains invisible
  before flush; same-archetype growth/replacement and recycled rows keep identity
  and source transforms correct. Empty/refilled owner reports exact tracked count.
- Ownership/nil: nil reads/calls safe, zero/independent ECS ownership isolated;
  copied wrappers read same counters without triggering builds.
- Integration: propagated descendant reaches real voxel bridge in
  current frame; existing animation, attachments and moving-brush tests remain
  authoritative for their producer semantics.

Missing stats may cause compile RED. Root/independent Sol 6.1 review tests before production, then code/tests after GREEN.

## Files and verification

Files: `mod_hierarchy.go`, private hierarchy owner, `ecs.go` storage/getter and `commands.go` getter. No component layout, serialization, renderer ABI, shader or physics-worker changes. Canonical modules/ECS, renderer timing and roadmaps updated.

Verify S3d RED/GREEN, hierarchy/animation/attachment/bridge/brush, engine sweep, races and consumer builds. Native scene: idle, ancestor/local motion and reparenting; inspect hierarchy/record counters, same-frame transforms and rendering. No pixel parity/frame-time claim. S3c editor test helper failures remain outside scope; build production.

## Execution record

Root scoped hierarchy propagation after S3c. Independent Sol 6.1 review added three refinements: newly resolved parent comparison, all-edge detection before composition and iterative invalid-path memoization. Existing hierarchy/animation/attachment/brush/S3c checks passed before new tests (1.015s), using pattern below.

Sol 6.1 authored eight tests in two files, three invalid-graph subcases. Root fixed assertion helper accepting NaNs; now rejects nonfinite outputs/non-unit rotations. Independent Sol 6.1 review requested simultaneous Parent edits reversing dependency. Root requested buffered Parent removal/restoration and authoritative root mirroring; Sol 6.1 added both. Existing tests/production unchanged during RED.

RED command: `env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3d'
-count=1`, redirected to `/tmp/gekko-s3d-red.log`. Exit 1 on missing
`Commands.TransformHierarchyStats`; functional assertions cannot execute until
new API exists. Command was rerun after review additions with same
compile RED.

Root/independent Sol 6.1 review approved simultaneous-edge/Parent-role coverage. Sol 6.1 implemented against frozen tests. GREEN (1.086s): `/tmp/gekko-s3d-green.log`. Both test files retained SHA-256 through implementation/verification; existing tests unchanged.

Root/independent Sol 6.1 production review: shared storage, transient typed columns, all-edge scan, iterative topology, invalid-path recovery, resolved parents, float bits, child repair, root authority, pivot exclusion and cleanup. No blocker. Each call still allocates transient column descriptors and reads live values; nonempty capacity may retain peak.

Verification from owning module directories, using
`GOCACHE=/tmp/gekko3d-gocache`:

- Pre-change baseline: `go test . -run '^Test(TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAssetAnimation|NPCAnimation|AttachAuthoredAsset|AuthoredAssetAttachment|MovingBrush|GroundedPlayerLandsOnMovingBrush|S3cVoxelInventoryHierarchy)' -count=1`
  passed (1.015s). Log: `/tmp/gekko-s3d-baseline.log`.
- Focused new tests: `go test . -run '^TestS3d' -count=1` passed (1.086s).
- Existing integration: `go test . -run 'Test(TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAssetAnimation|NPCAnimation|.*Attachment|.*Brush|VoxelRtSystem|StreamedVoxel|S3c)' -count=1`
  passed (0.567s). Log: `/tmp/gekko-s3d-existing-green.log`.
- Engine `go test ./...` passed; root package 10.642s. Log:
  `/tmp/gekko-s3d-engine.log`.
- Focused race: `go test -race . -run '^Test(S3d|S3c|Ecs_|EcsReflect_|Query_|TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAsset|NPCAnimation|AttachAuthoredAsset|VoxelRt|SyncVoxelRt|StreamedVoxel|MovingBrush|GroundedPlayerLandsOnMovingBrush)' -count=1`
  passed (2.348s). Log: `/tmp/gekko-s3d-race.log`. Known macOS linker
  `LC_DYSYMTAB` warning occurred; no race report occurred.
- ActionGame `go test ./... -run '^$'`, editor production `go build ./...`, and
  hierarchy-orrery `go test ./...` passed. Logs:
  `/tmp/gekko-s3d-actiongame-compile.log`, `/tmp/gekko-s3d-editor-build.log`,
  `/tmp/gekko-s3d-hierarchy-orrery.log`. Editor build exited 0 despite its denied
  module stat-cache write. Its previously reproduced missing test helpers were
  not rerun; production was built.
- Hierarchy-man `go test ./...` failed at `main.go:364` because `AssetDef` no
  longer has `AnimationClips`. Reproduced identical compilation failure
  against prior engine HEAD `6115a81`, using Git archive and disposable
  complete workspace `/tmp/gekko-s3d-head.work`. This existing sample API usage
  is outside S3d. Logs: `/tmp/gekko-s3d-hierarchy-man.log` and
  `/tmp/gekko-s3d-hierarchy-man-baseline.log`. No consumer files changed.
- `gofmt -l` was empty for six changed/new Go files; `git diff --check`
  passed. User template deletions and neighboring work remain excluded.

Native `/tmp/gekko-s3d-smoke.go` built as `/tmp/GekkoS3dSmoke.app/Contents/MacOS/gekko-s3d-smoke`, exit 0. Five colored cubes: three-level chain/independent branch. Four idle holds: 76, 76, 77 and 117 frames (346 total), 2.605s, 2.602s, 2.631s and 4.013s. Hierarchy/candidate/scene preparation/publication counters stayed stable while frames advanced. Repeated calls/stats reads stayed idle.

Native run checked ECS/renderer transforms each frame. Root motion, local edits, preserving-world reparent, new-parent motion and direct child repair reached existing hierarchy/extraction boundary; pivot-only changes added no work. Final: topology builds 2, compositions 12, transforms 7, instance templates 14, parameter templates 5, BVHs 4, writes 18, candidate builds 1. CUA showed five rendered cubes. Logs: `/tmp/gekko-s3d-smoke-build.log`, `/tmp/gekko-s3d-smoke.log`.

Established skipped hierarchy work, ownership and native continuity, not pixel parity, frame-time gains or memory ceiling. Full notifications, incremental extraction and layer selection keep S3 partial.