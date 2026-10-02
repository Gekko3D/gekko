# S3d: Incremental hierarchy propagation

Date: 2026-10-02. Status: implemented and verified; S3 remains partial.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S3.
Prerequisite: S3c committed structural revisions.

## Scope and confidence

Primary owner: ECS storage and `TransformHierarchySystem`. Consumers: authored
animation, attachments, moving roots, spatial/gameplay transforms and voxel
extraction. Confidence: High after tracing storage, command flushing, hierarchy,
direct-call attachment helpers and bridge timing. SME alignment required: No.
`base/skills-manifest.md` is absent; workflow and owning docs were read.

Known: hierarchy currently rebuilds world/child/resolution maps and composes
every resolvable child on every call. Animation and consumers write public
local/world/parent fields directly. Attachment helpers call hierarchy repeatedly
within a frame. S3c supplies committed membership invalidation, but no automatic
component-value change contract.

Use exact live-input comparison at the existing hierarchy owner to reuse
topology and child composition. This is a permanent hierarchy improvement;
it does not introduce a general changed-component API or permit skipping
renderer extraction. All relevant values remain read on every invocation.
Full value notifications and incremental bridge extraction remain later S3.
Workload-dependent speedup and retained peak capacity are unknown.

## Architecture and invariants

1. Preserve the public `TransformHierarchySystem(*Commands)` signature, module
   installation without another resource, `PostUpdate` stage and immediate
   direct calls. Do not flush commands. Enqueued changes become visible only
   at the existing stage flush.
2. Private hierarchy state belongs to shared `ecsStorage`, so copied ECS wrappers
   share one owner while independent storages cannot share cached membership,
   type IDs, topology, inputs or counters. Expose nil-safe main-thread
   `Ecs.TransformHierarchyStats()` and `Commands.TransformHierarchyStats()`.
   Return a value `TransformHierarchyStats` with cumulative `uint64`
   `TopologyBuildCount` and `CompositionCount`, and current `int`
   `TransformCount`. Reads do not trigger preparation. First/changed empty
   topology builds count; composing a resolvable child counts once even if its
   output happens to equal its previous value. Root mirroring does not count.
3. Cache every entity with `TransformComponent`, including parent-only entities,
   by committed structural revision. Local/Parent presence determines existing
   roles. A child requires all three components. An entity with Parent and
   world but no local is still a valid world-space parent. A world+local entity
   without Parent mirrors authoritative world TRS into local on every call.
4. Keep only entity identities, row locations, archetype references and value
   snapshots between calls. Reacquire current typed columns each invocation;
   retain no component pointers or typed-slice aliases. Structural migrations,
   replacements, same-archetype growth and recycled rows invalidate locations.
5. Scan live Parent values each call. Any child-parent identity change rebuilds
   resolution order without requiring a structural revision. Detect every edge
   change before composition. Memoize valid and invalid paths once during
   rebuilding; use iterative traversal rather than unbounded recursion. Build an order
   where each resolvable ancestor precedes descendants. Missing parents, cycles
   (including self-parenting), and their descendants keep their current world
   TRS unchanged. Repairing the link or committed membership resumes propagation
   in the same invocation. Do not use a stale pre-composition parent snapshot:
   each child's comparison/composition sees its parent's newly resolved world.
6. Compare exact float bits of parent world TRS, child local TRS and the last
   produced world TRS. Skip composition only when all match and the child is
   currently resolvable. Repair direct child-world TRS writes even when inputs
   have not changed. Clear validity for unresolved children so later recovery
   cannot reuse stale output ownership. Membership/topology changes may
   conservatively invalidate composition snapshots.
7. Preserve the current formula and arithmetic order: scale local position by
   parent scale, rotate it, add parent position; multiply and normalize
   rotations; multiply scales componentwise. Parent and child render Pivot are
   excluded from hierarchy inputs. Never change Pivot while composing TRS.
   Compare bits to distinguish signed zero and avoid repeated work for unchanged
   NaN bit patterns; do not add transform sanitization or change numerical policy.
8. Release obsolete entity and archetype references, including backing-array
   tails. Empty topology releases aggregate capacity. Nonempty storage may
   retain peak capacity; this is not a memory ceiling. No worker or new lock
   mutates this owner. Existing main-thread query mutation rules apply.

Alternatives: replacing public fields with mandatory setters expands into an
engine/editor/game migration; counters alone remove no repeated work; using
only structural revisions misses local and Parent writes. Exact value inputs
at the authoritative hierarchy boundary preserve compatibility and remove
repeated composition before any broader notification migration.

## Functionality tests and corner cases

The continuing user-authorized Sol 6.1 tests-first, adversarial review,
implementation, review and commit workflow authorizes focused new tests.
Existing tests remain unchanged. Protect world/local behavior and public work
and ownership counters, never cache layout, private row handles or traversal
order. Use independent expected transforms or literal results rather than
copying the new caching implementation.

- First/idle and repeated same-frame calls: real deep chain plus independent
  branches composes correctly once; unchanged calls add no topology/composition
  work. Root local mirrors direct world changes and ignores local edits.
- Direct TRS edits: parent position/rotation/nonuniform scale, local TRS and
  descendant motion appear in the same call. Value-only edits reuse topology;
  unchanged branches add no composition work. Repair direct child world writes.
- Pivot exclusion and exact bits: renderer Pivot changes leave child hierarchy
  outputs/work unchanged; signed-zero changes are observed; unchanged NaN input
  bits stabilize idle work without numerical sanitization.
- Parent semantics: direct reparenting rebuilds topology; preserving-world helper
  retains world pose. Transform-only parents and Parent+World without Local
  remain usable; absent parent/world/local semantics match current hierarchy.
- Invalid graphs: missing parent, self/multi-node cycle and descendants do not
  partially compose. Unrelated valid branches continue. Direct/committed repair,
  removal of a parent and re-admission recover immediately without stale output.
- Structural boundaries: queued insertion/replacement/removal remains invisible
  before flush; same-archetype growth/replacement and recycled rows keep identity
  and source transforms correct. Empty/refilled owner reports exact tracked count.
- Ownership/nil: nil reads/calls safe, zero/independent ECS ownership isolated;
  copied wrappers read the same counters without triggering builds.
- Integration: a propagated descendant reaches the real voxel bridge in the
  current frame; existing animation, attachments and moving-brush tests remain
  authoritative for their producer semantics.

Missing stats declarations may produce compile-failure RED. Root and an
independent Sol 6.1 reviewer inspect tests before production implementation,
then review production and tests again after GREEN.

## Files and verification

Production: `mod_hierarchy.go`, a focused private hierarchy owner file,
`ecs.go` storage/getter and `commands.go` getter. No public component layout,
serialization, renderer ABI, shaders or physics-worker changes.
Canonical docs: engine modules/ECS, renderer timing note, parent roadmap and
this record.

Run focused S3d RED/GREEN, existing hierarchy/animation/attachment/bridge/brush
tests, engine sweep and focused race; compile affected consumer production.
Use a disposable native scene with an idle hold, ancestor/local motion and
reparenting. Check public hierarchy and scene-record counters, same-frame
renderer transforms and rendered continuity. Native checks do not prove pixel
parity or frame-time gains. Known editor test helper failures from S3c remain
outside this slice; build production instead.

## Execution record

Root narrowed S3 after S3c to hierarchy-owned propagation. Independent Sol 6.1
design review confirmed the permanent owner and identified three refinements:
compare newly resolved parent outputs, detect all changed edges before any
composition, and memoize invalid paths with iterative topology traversal.
The design includes those refinements. The existing focused hierarchy,
animation, attachment, moving-brush and S3c hierarchy checks passed before new
test files existed (1.015s), using the pattern recorded below.

Sol 6.1 authored eight focused tests in two new files, with three invalid-graph
subcases. Root review corrected a finite
TRS assertion helper that could accept NaNs; it now rejects nonfinite output
and non-unit rotations. Independent Sol 6.1 review identified simultaneous
Parent edits reversing a dependency as missing coverage. Root also requested
buffered Parent removal/restoration and authoritative root mirroring. Sol 6.1
added both cases. Existing tests and production remained unchanged during RED.

RED command: `env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3d'
-count=1`, redirected to `/tmp/gekko-s3d-red.log`. Exit 1 on missing
`Commands.TransformHierarchyStats`; functional assertions cannot execute until
the new API exists. The command was rerun after review additions with the same
compile RED.

Root and independent Sol 6.1 review confirmed the added simultaneous-edge and
Parent-role cases and closed the test loop. Sol 6.1 implemented production
against frozen tests. Focused GREEN passed (1.086s), log
`/tmp/gekko-s3d-green.log`. Both test files' SHA-256 values remained unchanged
through implementation and verification. No existing tests changed.

Root and independent Sol 6.1 production review checked shared storage ownership,
fresh transient typed columns, all-edge scanning before iterative topology,
invalid-path memoization/recovery, newly composed parent reads, exact bit
snapshots, child-world repair, root authority, pivot exclusion and reference
cleanup. No actionable production blocker remained. Each invocation still
allocates transient batch column descriptors and reads live values; nonempty
owner capacity may retain its peak.

Verification from the owning module directories, using
`GOCACHE=/tmp/gekko3d-gocache`:

- Pre-change baseline: `go test . -run '^Test(TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAssetAnimation|NPCAnimation|AttachAuthoredAsset|AuthoredAssetAttachment|MovingBrush|GroundedPlayerLandsOnMovingBrush|S3cVoxelInventoryHierarchy)' -count=1`
  passed (1.015s). Log: `/tmp/gekko-s3d-baseline.log`.
- Focused new tests: `go test . -run '^TestS3d' -count=1` passed (1.086s).
- Existing integration: `go test . -run 'Test(TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAssetAnimation|NPCAnimation|.*Attachment|.*Brush|VoxelRtSystem|StreamedVoxel|S3c)' -count=1`
  passed (0.567s). Log: `/tmp/gekko-s3d-existing-green.log`.
- Engine `go test ./...` passed; root package 10.642s. Log:
  `/tmp/gekko-s3d-engine.log`.
- Focused race: `go test -race . -run '^Test(S3d|S3c|Ecs_|EcsReflect_|Query_|TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAsset|NPCAnimation|AttachAuthoredAsset|VoxelRt|SyncVoxelRt|StreamedVoxel|MovingBrush|GroundedPlayerLandsOnMovingBrush)' -count=1`
  passed (2.348s). Log: `/tmp/gekko-s3d-race.log`. The known macOS linker
  `LC_DYSYMTAB` warning occurred; no race report occurred.
- ActionGame `go test ./... -run '^$'`, editor production `go build ./...`, and
  hierarchy-orrery `go test ./...` passed. Logs:
  `/tmp/gekko-s3d-actiongame-compile.log`, `/tmp/gekko-s3d-editor-build.log`,
  `/tmp/gekko-s3d-hierarchy-orrery.log`. Editor build exited 0 despite its denied
  module stat-cache write. Its previously reproduced missing test helpers were
  not rerun; production was built.
- Hierarchy-man `go test ./...` failed at `main.go:364` because `AssetDef` no
  longer has `AnimationClips`. Reproduced the identical compilation failure
  against prior engine HEAD `6115a81`, using a Git archive and disposable
  complete workspace `/tmp/gekko-s3d-head.work`. This existing sample API usage
  is outside S3d. Logs: `/tmp/gekko-s3d-hierarchy-man.log` and
  `/tmp/gekko-s3d-hierarchy-man-baseline.log`. No consumer files changed.
- `gofmt -l` was empty for the six changed/new Go files; `git diff --check`
  passed. User template deletions and neighboring work remain excluded.

Disposable native harness `/tmp/gekko-s3d-smoke.go` built as
`/tmp/GekkoS3dSmoke.app/Contents/MacOS/gekko-s3d-smoke` and exited 0. It rendered
five colored cubes in a three-level chain and an independent branch. Four idle
holds completed 76, 76, 77 and 117 frames (346 total), over 2.605s, 2.602s,
2.631s and 4.013s. Hierarchy topology/composition, candidate discovery and S3b
scene preparation/publication counters remained stable in each hold, while
native frames advanced. Repeated hierarchy calls and stats reads were idle.

The native run observed both ECS and renderer transforms on every frame. Root
motion, local edits, preserving-world reparenting, new-parent motion and direct
child-world repair appeared at the next existing hierarchy/extraction boundary;
pivot-only changes added no hierarchy work. Final counters were topology builds
2, compositions 12, tracked transforms 7, instance templates 14, parameter
templates 5, BVH builds 4, record writes 18 and candidate inventory builds 1.
CUA inspection showed the five colored cubes rendered in the final scene.
Logs: `/tmp/gekko-s3d-smoke-build.log`, `/tmp/gekko-s3d-smoke.log`.

These checks establish skipped hierarchy operations, ownership and native
continuity, not pixel parity, frame-time gains or a memory ceiling. Full value
notifications, incremental bridge extraction and future layer selection keep
S3 partial.
