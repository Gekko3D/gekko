# S3b: Incremental GPU scene record preparation

Date: 2026-10-01. Status: implemented and verified. Parent S3 remains partial.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S3.
Prerequisites: S1a/S1b/S1c and S3a.

## Scope and confidence

Primary owner: `GpuBufferManager` scene record preparation/publication.
Consumers: opaque, transparent, and shadow voxel passes; engine bridge and
renderer app. Confidence: High after tracing bridge identity/transform/material
comparisons, scene culling/BVH reuse, GPU preparation, allocation mutation,
and binding recreation. SME alignment required: No. This is a permanent owner
improvement within the existing scene/ABI contract. `base/skills-manifest.md`
is absent; workflow and renderer owning documentation were read.

Known: the bridge already retains stable `VoxelObject` identities and compares
transforms/material keys. `Scene.Commit` already caches world-space BVHs, but
`UpdateScene` rebuilds three render-relative BVHs and all instance/parameter
arrays every frame. `ensureBuffer` also writes unchanged arrays. Transform dirty
flags can be consumed by Commit before record gathering. Public object metadata
and allocation fields can change in place without changing pointer identities.

Unknown: workload-dependent gains and retained record capacity. Live cache
entries must follow current render-pass objects without historical memberships;
there is no new byte budget or promised frame-time gain.

Cache compiled instance rows, object parameter rows, and render-relative BVHs.
Preserve existing GPU layouts (208-byte instances, 128-byte parameters, current
BVH bytes), empty sentinels, object ordering, render-origin arithmetic, and
resource recreation behavior. This slice does not introduce ECS change events,
skip bridge scanning or scene culling, change LOD/Hi-Z/shadow policy, optimize
light records, change voxel upload budgets, or alter shaders. Future ECS dirty
extraction and layer selection keep parent S3 partial.

## Architecture and invariants

1. Add a focused manager-owned record preparation file. Reuse stable object
   identities to retain compiled row templates; compare actual inputs each
   preparation. Snapshot values, never mutable input pointers alone. Compare
   encoded float bits for matrices, bounds, origins, and float parameters;
   ordinary float equality misses signed zero and churns on unchanged NaNs. No worker
   mutates this owner; no new lock or ECS stage is required.
2. Instance inputs include actual forward/inverse matrices, local/world bounds,
   world-bound presence, and render origin. Matrix comparison preserves rotation,
   scale, and pivot changes even when world bounds happen to match and Commit
   cleared `Transform.Dirty`. The existing transform Dirty protocol remains.
3. Parameter inputs cover every field encoded by `writeObjectParamsData`, map ID,
   sector count, geometry allocation presence, direct lookup values, and material
   allocation presence/offset. Material payload upload remains a separate owner;
   S3b does not repair existing in-place MaterialTable payload edit semantics.
   Missing geometry allocation produces the existing zero parameter record;
   later admission must replace it with fresh values on unchanged visibility.
4. Preserve each pass's ordered membership separately. Instance row index, BVH
   leaves, and parameter row order must refer to the same object. Per-object
   templates may be shared across passes; assembly patches the pass index.
   Changes rebuild only affected row templates. Aggregate arrays may reassemble
   after a row/order/origin change; idle arrays are reused.
5. BVH inputs are ordered object identity, world bounds/presence, and origin.
   Run `TLASBuilder.Build` only for changed nonempty pass inputs. Metadata-only
   parameter changes must not rebuild BVHs. Keep the existing relative builder
   rather than translating world-space BVH bytes, which could change floating
   point split behavior. Camera movement changes origin; culling/Hi-Z can change
   pass membership independently even when content is stationary.
6. Gather records after `UpdateVoxelData` and sector/terrain/planet lookup
   maintenance. Allocation/material offsets and direct lookup metadata can
   change there. Move instance/BVH writes to this same boundary; preceding
   update functions do not consume those buffers. Rendering and binding
   recreation happen after the complete `UpdateScene` call.
7. Track compilation revision and last-uploaded revision/destination per record
   buffer. Reuse CPU arrays independently of upload state: inspecting prepared
   records must not falsely mark them uploaded. Skip an unchanged write only
   when the destination buffer is still the published destination. A nil,
   replaced, or insufficient buffer must receive retained bytes. A changed
   destination identity must propagate through `UpdateScene`'s recreation
   result even when the replacement has enough capacity, so bindings refresh.
   Queue write failure must not mark a record published. Existing `ensureBuffer`
   ignores write errors; the new publication helper must explicitly check the
   record write before advancing its revision/destination latch and counter.
8. Keep lights, shadow metadata/capacity, voxel upload scheduling, all lookups,
   culling, and camera/feature updates running each frame. A scene-record hit is
   not an early return from `UpdateScene` or app Update.
9. Drop removed object references and obsolete pass snapshots/byte tails. The
   owner tracks only the union of current pass objects, including shadow-only
   casters. Hidden resident geometry still receives voxel uploads. Empty passes
   publish the existing zero sentinels rather than stale previous arrays.
10. Add nil-safe main-thread `InvalidateSceneRecords()` to discard retained
    preparation/publication ownership, for explicit reset or external record
    buffer overwrite. This forces the next preparation/upload even if content
    values match. Counters remain cumulative for the manager lifetime. A new
    manager starts empty. Prepared data is a read-only borrowed view valid until
    the next preparation/reset; it is not an immutable historical snapshot.

Use a headless production preparation boundary:

```go
type sceneRecordBatch struct { instances, bvh, params []byte }
type sceneRecordBatches struct { visible, transparent, shadow sceneRecordBatch }
func (m *GpuBufferManager) prepareSceneRecords(scene *core.Scene, origin mgl32.Vec3) sceneRecordBatches
```

This private boundary feeds the real GPU upload path. Tests inspect actual shader
records and public operational counters, not cache keys/layout or helper order.

Add cumulative `uint64` counters: `SceneInstanceRecordBuildCount` and
`SceneObjectParamRecordBuildCount` count encoded unique object row templates;
`SceneBVHBuildCount` counts actual nonempty relative BVH builds;
`SceneRecordUploadCount` counts successfully queued writes for these nine record
buffers. `SceneRecordObjectCount` reports currently retained object templates.
Pass index patching/aggregate assembly is not row-template encoding. These are
operational work/ownership contracts, not a benchmark or total renderer cost.

Alternatives: relying on `StructureRevision`/dirty flags misses metadata and
consumed transform changes; hashing serialized arrays still repeats packing;
making ECS event tracking complete would expand this slice across engine and
editor mutation boundaries. Exact input snapshots at the current owner preserve
public mutation compatibility while removing repeated compilation and writes.

## TDD coverage by functionality

The user's continuing Sol 6.1 tests-first/review/implementation/review/commit
workflow explicitly authorizes functionality tests for this slice.

- First/idle preparation: bytes equal fresh existing builders for all passes;
  repeated unchanged preparation adds no row/BVH builds. No timing, allocation,
  map identity, private cache key, or exact helper call-order assertions.
- Targeted object changes: real transform motion after Commit, same-bounds
  rotation, nonuniform scale/pivot, local/world bounds and shared-map edits.
  Preserve byte parity and avoid re-encoding unchanged object templates.
- Origin changes: update instance matrices/bounds and relative BVH bytes while
  parameter bytes/work remain unchanged.
- Metadata/allocation changes: exercise every encoded metadata field, map
  replacement/ID, sector count, absent/present geometry/material allocation,
  in-place material offset and direct lookup mutation on stationary objects.
  Assert fresh parameter bytes and no unrelated instance/BVH builds.
- Membership and pass independence: add/remove/reorder objects; opaque,
  transparent, and shadow-only lists retain their own order/indices. Changes in
  one pass must not rebuild unrelated BVHs. Scene.Commit drives real visibility,
  transparency, hidden residency, and shadow-only cases where useful.
- Lifetime/reset/empty: release removed template ownership; no stale records
  after emptying/shrinking or switching scenes; explicit invalidation forces
  preparation again and nil invalidation is safe.
- Existing bridge hierarchy/animation/moving-brush, scene culling/shadows,
  upload/readiness, packing, and render-origin tests remain unchanged.

Missing declarations may make test-only RED a compile failure. Root reviews the
test contracts before production implementation. Native verification, rather
than device-mocking tests, checks actual idle record writes, replacement-buffer
publication, binding recreation, and visible scene continuity.

## Expected files and verification

Files: new `voxelrt/rt/gpu/manager_scene_records.go` and focused S3b tests;
`manager_scene.go`/`manager.go` integration; canonical renderer runtime docs;
parent roadmap and this execution record. Bridge/core/transform code changes
require a concrete discovered gap rather than broader extraction work.

Run focused S3b tests at RED/GREEN, GPU/core/BVH/app tests, engine sweep, focused
race, affected ActionGame/example compilation, and a small physical GPU smoke.
Use a disposable local public-API harness to exercise unchanged frames, motion,
transparency/hidden handoff, object removal, and a replaced destination buffer.
Observe native write/build counters, `SceneBindingRevision`, and resource recreation; inspect screenshots
where available. No pixel-perfect or performance claim follows from a smoke.

## Execution record

### Design and tests

Root extracted the manager-owned scope and contracts above. Sol 6.1 read-only
architecture review confirmed the boundary and clarified bitwise float
snapshots, checked queue publication, and sufficient-capacity destination
replacement requiring binding refresh.

Sol 6.1 authored eleven focused `TestS3b` tests, including thirty per-field
parameter mutations with reset checks. Root and an independent Sol 6.1 reviewer
found one work-contract gap: a pure pass reorder must reuse row templates. The
author added that public-counter assertion and repeated RED. No existing tests
or production declarations were changed during this phase.

RED command: `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -run
'^TestS3b' -count=1`. Exit 1 for missing record-preparation types/method and
metrics; functional assertions could not execute yet. Local log:
`/tmp/gekko-s3b-red.log`.

Native publication/binding behavior and bitwise instance/BVH/origin snapshots
remain explicit implementation-review/verification gates. The test-only NaN and
signed-zero cases cover parameter snapshots, not nonfinite scene matrices.

### Implementation and adversarial review

Sol 6.1 implemented the manager-owned record cache and publication path without
changing the reviewed tests. Unique objects retain index-neutral instance and
parameter templates. Separate ordered pass snapshots reuse aggregate bytes and
relative BVHs, prune absent objects, clear obsolete tails, and release empty
pass capacity. Exact input snapshots use float bits throughout.

`UpdateScene` prepares and publishes records after voxel and lookup maintenance.
The publication helper asks `ensureBuffer` for capacity using a nonnil empty
slice, avoiding its unchecked write and obsolete-content copy paths. It checks
the actual queue write before advancing the uploaded revision/destination and
counter. Destination changes report recreation even when capacity is sufficient;
explicit invalidation discards both preparation and publication ownership.

Root and an independent Sol 6.1 reviewer compared snapshot fields with the
existing writers, reviewed render-origin arithmetic and index patching, traced
membership/reference cleanup, and checked allocation timing, unconditional
frame work, and app binding refresh. Neither found an actionable blocker.
Native behavior was then checked independently below. Queue error handling and
nonfinite matrix/bounds/origin bit comparisons were verified by code review;
no queue-failure injection or nonfinite native scene was used.

Canonical renderer runtime documentation and the parent roadmap now describe
this completed boundary, counters, reset API, borrowed views, and capacity
limits. ECS dirty extraction and future layer selection remain S3 work.

### Automated verification

Go 1.25.4, darwin/arm64. Engine commands run from `gekko/`; all passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -run '^TestS3b' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu ./voxelrt/rt/core ./voxelrt/rt/bvh ./voxelrt/rt/app
env GOCACHE=/tmp/gekko3d-gocache go test ./...
env GOCACHE=/tmp/gekko3d-gocache go test -race ./voxelrt/rt/gpu ./voxelrt/rt/core ./voxelrt/rt/app
git diff --check
```

The eleven focused tests and thirty metadata mutation cases are GREEN. Engine
sweep and race logs: `/tmp/gekko-s3b-engine.log` and
`/tmp/gekko-s3b-race.log`. Race linking emitted the existing macOS
`LC_DYSYMTAB` warnings and exited 0.

Consumer compilation passed:

```sh
# actiongame/
env GOCACHE=/tmp/gekko3d-gocache go test ./... -run '^$'
# examples/destruction_derby/, using the existing workspace
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Local logs: `/tmp/gekko-s3b-actiongame-compile.log` and
`/tmp/gekko-s3b-example-workspace.log`. The example has no tests. An initial
`GOWORK=off` example check failed on missing module sums; the existing workspace
provides the local WebGPU replacement and compiles successfully.

An additional full ActionGame test run failed six bot tests: selected-plan
clearing, visible candidates, target prioritization, hearing falloff, direct
danger, and utility personality weighting. The same failures reproduced with
engine HEAD `d93f3ac`, using an archived engine and a disposable workspace that
preserved every other module and the local WebGPU replacement:

```sh
# actiongame/, only the engine points to the HEAD archive
env GOWORK=/tmp/gekko-s3b-head.work GOCACHE=/tmp/gekko3d-gocache go test ./src/modules/startup -run '^TestActionGameBot' -count=1
```

Logs: `/tmp/gekko-s3b-actiongame.log` and
`/tmp/gekko-s3b-actiongame-baseline-workspace.log`. These consumer baseline
failures are outside S3b; no ActionGame code or tests were changed.

### Native GPU verification

A disposable public-API harness (`/tmp/gekko-s3b-smoke.go`, not committed) used
the real ECS bridge, saved streamed world, bounded voxel uploads, render passes,
point shadows, transparent material, and WebGPU device. Build/run:

```sh
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS3bSmoke.app/Contents/MacOS/GekkoS3bSmoke /tmp/gekko-s3b-smoke.go
/tmp/GekkoS3bSmoke.app/Contents/MacOS/GekkoS3bSmoke
```

The desktop run exited 0: **857 frames over 29.352 seconds**. Eight stationary
phases each held 88 completed frames for at least three seconds after settling;
instance/parameter/BVH build counters, record-write count, binding revision and
ownership remained unchanged throughout each hold. Every completed frame also
checked fixed camera/point/ambient values, camera/light buffers, render-frame
progress, and exact live pass-union ownership.

Observed transitions:

- Ready fallback remained visible while full targets were resident, hidden and
  unfinished under a paused upload budget. Restoring service completed the full
  cohort and hid the still-resident fallback.
- Real ECS translation/rotation retained object identity and refreshed instance
  rows, relative BVHs and writes. Hiding transparency and removing the moved
  object pruned pass membership/templates; ownership fell from five to four to
  three. Material-offset changes after removal refreshed parameter rows.
- Replacing `InstancesBuf` with an equal-capacity native buffer preserved CPU
  counts (7 instances, 12 parameter templates, 17 BVHs), queued exactly one new
  record write (59 to 60), and advanced `SceneBindingRevision` from 5 to 6.
- Explicit invalidation rebuilt the three remaining templates and three
  nonempty BVHs and republished all nine record buffers (60 to 69 writes).
- Moving the observer away unloaded full targets, restored the proxy and
  reduced retained templates to two; coarsened records then remained idle.

Computer-use screenshots showed the rendered red/cyan refined geometry and
yellow coarsened fallback. Native log: `/tmp/gekko-s3b-smoke.log`. The first
harness run incorrectly counted ambient light as a GPU row and stopped at frame
one; source tracing confirmed that the bridge accumulates it separately in
`Scene.AmbientLight`. Only that disposable fixture was corrected before rerun.

These checks establish the recorded packing/work/publication contracts and
visible rendering continuity. They do not establish pixel parity, workload
speedups, total process memory bounds, or a new scene-record byte ceiling.
