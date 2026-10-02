# S3f: Hierarchy output publications

Date: 2026-10-02. Status: implemented, both adversarial reviews closed, verified.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S3.
Prerequisite: [S3e component publication API](streamed-rendering-s3e.md).

## Scope and confidence

Primary owner: `transformHierarchyOwner.update` output writes. Consumers: future
component publication consumers, current hierarchy descendants and renderer
extraction. Confidence: High after reading the owner, S3d/S3e contracts,
hierarchy/reparent helpers and ECS/module/runtime verification docs. Independent
Sol 6.1 review confirmed this permanent producer migration step. SME alignment
required: No. `base/skills-manifest.md` remains absent.

Known: S3e commits publish structural writes, but direct producer writes still
need explicit publication. Hierarchy has two output boundaries: root world TRS
mirrored into LocalTransform and child local/parent TRS composed into world
Transform. S3d already compares exact bits to reuse composition and repair direct
child-world edits. A composition or topology build does not necessarily change
the destination value. Current public fields remain mutable and input owners
are not all migrated.

Migrate these two output boundaries only. Preserve live input reads and existing
arithmetic. No extraction work is removed and no performance gain is claimed.
Reparent and world-transform helpers belong to a subsequent input-owner slice:
they write Parent/local inputs and have validation/rollback contracts. Their
calls to hierarchy will receive output publication through this slice. A failed
helper can already have propagated legitimate outputs through its initial
hierarchy invocation; failure does not imply no hierarchy publication.

Alternatives: complete producer migration spans animation, physics, gameplay,
editor and external writers; input snapshots would duplicate dirty detection
and claim publication for writes owned elsewhere. Use the existing explicit
mark API for changed committed outputs at the current owner boundary.

## Architecture and invariants

1. Root mirroring publishes `LocalTransformComponent` only if its actual
   destination position, rotation or scale bits change. Root world remains
   authoritative and receives no publication for being read. Equal mirrors
   publish nothing, including identical NaN payload bits.
2. Valid child composition publishes `TransformComponent` only if its actual
   pre-write world TRS bits differ from the resulting world TRS bits. Compare
   the live destination before the write, not previous cached output, input
   changes, topology work or composition counters. A repaired direct child-world
   edit publishes even if the result equals the previous cached output.
3. Publish through the existing `MarkComponentChanged` contract immediately
   after a changed committed output is written. Consumers compare aggregate
   sequence equality; this slice does not establish an entity worklist or an
   entity-count interpretation for an invocation's sequence advance.
4. Preserve composition, normalization, counters, valid/invalid topology and
   current-column acquisition. Descendants read the new ancestor output in the
   same invocation. Missing parents, cycles and incomplete child roles keep
   their current behavior and publish no unchanged outputs. Valid unrelated
   branches and root mirrors remain active.
5. Input-only changes that produce identical output publish nothing through
   this owner. Direct child LocalTransform, Parent and source world changes
   remain unmarked by hierarchy reads; their owners must migrate separately.
   Changed derived outputs still publish. Public explicit marks remain valid
   even for equal values, as defined by S3e.
6. Use exact float bits. Signed zero changes count; identical NaN bits do not
   repeatedly publish. Pivot is excluded from hierarchy math and preserved.
   Pivot-only external edits are not hierarchy output publications.
7. Preserve scheduled and direct-call timing. Never flush or enqueue commands,
   migrate rows or advance the structural stamp. Pending additions/removals
   retain committed visibility rules. No new cache, journal, component layout,
   public API, concurrent access or retained pointer is introduced.
8. These publications cover hierarchy outputs only. Renderer and hierarchy live
   reads remain authoritative. Complete input/asset producer coverage is still
   required before skipping extraction using component revisions.

## Functionality tests and review

The continuing user-authorized Sol 6.1 tests-first workflow permits focused new
tests. Existing tests stay unchanged. Test public revisions, values and structural
visibility; avoid private owner fields, map keys, helper order or row layouts.
Check advancement/equality without interpreting aggregate increments as the
number of changed entities. Existing S3d tests retain topology/storage coverage.

- Child propagation and descendant motion publish world Transform immediately;
  child LocalTransform, Parent and unrelated types retain their revisions.
  Repeated idle calls publish nothing.
- Root mirroring publishes LocalTransform only for changed destination TRS,
  preserves authoritative world, and repairs later direct root-local edits.
- Initial output already correct, structural rebuild with equal outputs,
  equivalent-parent changes and input changes yielding equal output publish
  nothing. A direct child-world edit repaired to cached output still publishes.
- Missing/self/cyclic branches retain world values and publish nothing;
  recovery and valid unrelated outputs publish when actual values change.
- Signed zero and NaN mirror bits preserve exact publication semantics;
  Pivot-only edits retain revision and value compatibility.
- Pending commands remain invisible until flush. Compare owner publications
  after structural commits separately from hierarchy output publications.

Sol 6.1 writes tests and stops at behavioral RED. Root and independent Sol 6.1
review the tests adversarially before production. Freeze tests, then Sol 6.1
implements and stops GREEN. Repeat adversarial review for production, verify,
update canonical docs and commit this slice.

## Files and verification

Production: `mod_hierarchy_owner.go`. Canonical docs: `docs/engine/ecs.md`,
`docs/engine/modules.md`, parent roadmap and this record. One focused new
`mod_hierarchy_publication_s3f_test.go` is expected. No edits to reparent/world
helpers, animation/brush/physics, renderer or S3e API/storage are needed.

Run S3f RED/GREEN, existing S3c/S3d/S3e/hierarchy/reparent/animation/brush checks,
engine root tests, focused race and ActionGame/editor production compilation.
Notification-only behavior needs no new windowed smoke; S3d's existing bridge
and native records cover unchanged propagation/rendering. Earlier unrelated
editor/sample test baselines remain recorded in S3c/S3d.

## Execution record

Root narrowed the next step to hierarchy-owned outputs. Independent Sol 6.1
design review confirmed exact destination-bit attribution and separate ownership
for helper/input migrations. Its review of this design found no blockers. A
source and derived child share the aggregate Transform revision, so tests must
prove read-only behavior with isolated sources or output-equivalent changes,
not claim individual source attribution from that shared counter.

The pre-change focused S3c/S3d/S3e/hierarchy/reparent/animation/brush baseline
passed (1.033s). Sol 6.1 authored eight focused tests in one new file. The
focused S3f command stopped at behavioral RED (1.072s): expected immediate
Transform/LocalTransform output publications were missing. Value, equal-output
and committed-visibility checks passed.

Root and independent Sol 6.1 adversarial test review found one required gap:
child NaN outputs must remain unpublished during actual recomposition when
their resulting bits are unchanged. Idle composition reuse and root NaN checks
alone cannot protect that separate write boundary. Sol 6.1 added a subtest that
captures actual produced bits, forces an unrelated structural rebuild, then
repairs a finite direct child-world edit back to the captured NaN output. It
reacquires the component after migration and assumes no arithmetic payload
propagation. Final RED remained missing notifications only (1.045s).

Both test reviews closed with no remaining actionable gaps. Tests were frozen
before production implementation. Existing tests remain unchanged.

Sol 6.1 implemented two output publication checks in `mod_hierarchy_owner.go`
and stopped GREEN. Root and independent Sol 6.1 closed the second adversarial
review with no actionable blockers. The actual destination bits are captured
before each complete output write, then compared with resulting bits. Marks
target only the changed committed output. Existing arithmetic, assignments,
Pivot, topology, cache conditions, counters and timing remain unchanged; no new
retained state, input-read publication, structural mutation or flush was added.

Exact final commands from `gekko`:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3f' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(S3c|S3d|S3e|TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAssetAnimation|NPCAnimation|MovingBrush)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(S3c|S3d|S3e|S3f|TransformHierarchy|ReparentPreservingWorldTransform|AuthoredAssetAnimation|NPCAnimation|MovingBrush)' -count=1
```

All passed: S3f 0.974s; focused regression 0.376s; root package 10.984s; focused
race 2.009s. The race linker emitted the previously observed macOS
`LC_DYSYMTAB` warning and completed successfully.

Consumer verification:

- `actiongame`: `env GOCACHE=/tmp/gekko3d-gocache go test ./... -run '^$'`
  passed compilation of all packages without running gameplay tests.
- `gekko-editor`: `env GOCACHE=/tmp/gekko3d-gocache go build ./...` passed
  (exit 0), with the previously observed denied module stat-cache write warning.

The frozen new test hash stayed unchanged. `gofmt -l` on both changed/new Go
files and `git diff --check` were clean. No windowed smoke or engine-wide sweep
was needed for notification-only behavior. Earlier unrelated editor/sample test
baselines were not rerun; they remain documented in S3c/S3d. These checks establish
publication semantics and compilation, not rendered parity or a performance gain.

ECS/module docs and the parent roadmap describe migrated outputs and remaining
limits. S3 remains partial. Helper/input, animation, physics, gameplay and asset
producer coverage still need separate ownership designs and migrations before
publication revisions can justify skipping live extraction.
