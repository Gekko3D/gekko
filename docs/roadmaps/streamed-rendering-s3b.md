# S3b: Incremental GPU scene record preparation

Date: 2026-10-01. Status: implemented and verified. Parent S3 remains partial.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S3.
Prerequisites: S1a/S1b/S1c and S3a.

## Scope and confidence

Owner: `GpuBufferManager` scene record preparation/publication. Consumers: opaque/transparent/shadow voxel passes, bridge and renderer app. Confidence: High after comparisons, culling/BVH reuse, GPU preparation, allocations and bindings inspection. SME alignment: No. Permanent step within scene/ABI contract. Missing `base/skills-manifest.md`; workflow/renderer docs read.

Known: bridge retains `VoxelObject` identities and compares transforms/material keys. `Scene.Commit` caches world BVHs; `UpdateScene` rebuilds three relative BVHs and all instance/parameter arrays each frame. `ensureBuffer` writes unchanged arrays. Commit may consume transform dirty flags; metadata/allocations mutate without pointer changes.

Unknown: gains and retained capacity. Cache follows current pass objects, retaining no historical membership. No byte budget or frame-time gain promised.

Cache instance/parameter rows and relative BVHs. Preserve 208-byte instances, 128-byte parameters, BVH bytes, empty sentinels, ordering, origin arithmetic and recreation. No ECS events, bridge/culling bypass, LOD/Hi-Z/shadow changes, light optimization, upload budget or shader changes. ECS dirty extraction/layer selection remain S3 work.

## Architecture and invariants

1. Add focused manager-owned record preparation file. Reuse stable object
   identities to retain compiled row templates; compare actual inputs each
   preparation. Snapshot values, never mutable input pointers alone. Compare
   encoded float bits for matrices, bounds, origins, and float parameters;
   ordinary float equality misses signed zero and churns on unchanged NaNs. No worker
   mutates this owner; no new lock or ECS stage is required.
2. Instance inputs include actual forward/inverse matrices, local/world bounds,
   world-bound presence, and render origin. Matrix comparison preserves rotation,
   scale, and pivot changes even when world bounds happen to match and Commit
   cleared `Transform.Dirty`. Existing transform Dirty protocol remains.
3. Parameter inputs cover every field encoded by `writeObjectParamsData`, map ID,
   sector count, geometry allocation presence, direct lookup values, and material
   allocation presence/offset. Material payload upload remains separate owner;
   S3b does not repair existing in-place MaterialTable payload edit semantics.
   Missing geometry allocation produces existing zero parameter record;
   later admission must replace it with fresh values on unchanged visibility.
4. Preserve each pass's ordered membership separately. Instance row index, BVH
   leaves, and parameter row order must refer to same object. Per-object
   templates may be shared across passes; assembly patches pass index.
   Changes rebuild only affected row templates. Aggregate arrays may reassemble
   after row/order/origin change; idle arrays are reused.
5. BVH inputs are ordered object identity, world bounds/presence, and origin.
   Run `TLASBuilder.Build` only for changed nonempty pass inputs. Metadata-only
   parameter changes must not rebuild BVHs. Keep existing relative builder
   rather than translating world-space BVH bytes, which could change floating
   point split behavior. Camera movement changes origin; culling/Hi-Z can change
   pass membership independently even when content is stationary.
6. Gather records after `UpdateVoxelData` and sector/terrain/planet lookup
   maintenance. Allocation/material offsets and direct lookup metadata can
   change there. Move instance/BVH writes to this same boundary; preceding
   update functions do not consume those buffers. Rendering and binding
   recreation happen after complete `UpdateScene` call.
7. Track compilation revision and last-uploaded revision/destination per record
   buffer. Reuse CPU arrays independently of upload state: inspecting prepared
   records must not falsely mark them uploaded. Skip unchanged write only
   when destination buffer is still published destination. Nil,
   replaced, or insufficient buffer must receive retained bytes. Changed
   destination identity must propagate through `UpdateScene`'s recreation
   result even when replacement has enough capacity, so bindings refresh.
   Queue write failure must not mark record published. Existing `ensureBuffer`
   ignores write errors; new publication helper must explicitly check
   record write before advancing its revision/destination latch and counter.
8. Keep lights, shadow metadata/capacity, voxel upload scheduling, all lookups,
   culling, and camera/feature updates running each frame. Scene-record hit is
   not early return from `UpdateScene` or app Update.
9. Drop removed object references and obsolete pass snapshots/byte tails.
   owner tracks only union of current pass objects, including shadow-only
   casters. Hidden resident geometry still receives voxel uploads. Empty passes
   publish existing zero sentinels rather than stale previous arrays.
10. Add nil-safe main-thread `InvalidateSceneRecords()` to discard retained
    preparation/publication ownership, for explicit reset or external record
    buffer overwrite. This forces the next preparation/upload even if content
    values match. Counters remain cumulative for the manager lifetime. A new
    manager starts empty. Prepared data is a read-only borrowed view valid until
    the next preparation/reset; it is not an immutable historical snapshot.

Use headless production preparation boundary:

```go
type sceneRecordBatch struct { instances, bvh, params []byte }
type sceneRecordBatches struct { visible, transparent, shadow sceneRecordBatch }
func (m *GpuBufferManager) prepareSceneRecords(scene *core.Scene, origin mgl32.Vec3) sceneRecordBatches
```

Private boundary feeds native upload path. Assert actual shader records/public counters, not cache keys/layout or helper order.

Cumulative `uint64`: `SceneInstanceRecordBuildCount` and `SceneObjectParamRecordBuildCount` count encoded unique templates; `SceneBVHBuildCount` counts nonempty relative builds; `SceneRecordUploadCount` counts successful queued writes across nine record buffers. `SceneRecordObjectCount` reports retained templates. Index patching/assembly is not row encoding. Operational work/ownership contracts, not benchmarks or total cost.

`StructureRevision`/dirty flags miss metadata and consumed transform changes; serialized hashing repeats packing. Complete ECS events require broad engine/editor mutation migration. Exact input snapshots preserve public mutation compatibility while avoiding repeated compilation/writes.

## TDD coverage by functionality

User-authorized Sol 6.1 tests-first/review/implementation/review/commit workflow applies.

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

Missing declarations may cause compile RED; root reviews contracts before production. Native verification checks idle writes, replacement publication, binding recreation and continuity; no device-mocking tests.

## Expected files and verification

Files: new `voxelrt/rt/gpu/manager_scene_records.go`, focused S3b tests, `manager_scene.go`/`manager.go`, canonical renderer runtime docs and roadmaps. Bridge/core/transform changes require concrete discovered gap; no broader extraction work.

Verify S3b RED/GREEN, GPU/core/BVH/app, engine sweep, races, ActionGame/example compilation and physical smoke. Disposable public-API harness covers idle, motion, transparency/hidden handoff, removal and destination replacement. Observe write/build counters, `SceneBindingRevision` and recreation; inspect available screenshots. No pixel parity or performance claim.

## Execution record

### Design and tests

Root scoped manager contracts. Sol 6.1 architecture review confirmed bitwise snapshots, checked publication and binding refresh for sufficient-capacity replacement.

Sol 6.1 authored eleven `TestS3b` tests with thirty per-field mutations/reset checks. Root/independent Sol 6.1 review found one gap: reorder must reuse templates; author added counter assertion and repeated RED. Existing tests/production declarations unchanged.

RED command: `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -run
'^TestS3b' -count=1`. Exit 1 for missing record-preparation types/method and
metrics; functional assertions could not execute yet. Local log:
`/tmp/gekko-s3b-red.log`.

Review/verification gates: native publication/bindings and bitwise instance/BVH/origin snapshots. Test NaN/signed-zero cases cover parameters, not nonfinite matrices.

### Implementation and adversarial review

Sol 6.1 implemented cache/publication with tests unchanged. Unique objects retain index-neutral instance/parameter templates. Ordered pass snapshots reuse arrays/relative BVHs, prune absent objects, clear tails and release empty capacity. All float snapshots use bits.

`UpdateScene` prepares/publishes after voxel/lookup maintenance. Publication requests `ensureBuffer` capacity with nonnil empty slice, bypassing unchecked writes/obsolete copies. Check queue write before latching revision/destination/counter. Changed destination reports recreation even with sufficient capacity. Invalidation discards preparation/publication ownership.

Root/independent Sol 6.1 review: writer fields, origin arithmetic, index patching, reference cleanup, allocation timing, unconditional frame work and binding refresh. No blocker. Native checks below verify operation. Queue errors and nonfinite bit comparisons verified by code review; no injected failure or nonfinite native scene.

Canonical renderer docs/parent roadmap record boundary, counters, reset API, borrowed views and capacity limits. ECS dirty extraction/layer selection remain S3 work.

### Automated verification

Go 1.25.4, darwin/arm64. Engine commands run from `gekko/`; all passed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu -run '^TestS3b' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu ./voxelrt/rt/core ./voxelrt/rt/bvh ./voxelrt/rt/app
env GOCACHE=/tmp/gekko3d-gocache go test ./...
env GOCACHE=/tmp/gekko3d-gocache go test -race ./voxelrt/rt/gpu ./voxelrt/rt/core ./voxelrt/rt/app
git diff --check
```

Eleven focused tests and thirty mutation cases GREEN. Engine/race logs: `/tmp/gekko-s3b-engine.log`, `/tmp/gekko-s3b-race.log`. Existing macOS `LC_DYSYMTAB` warnings; exit 0.

Consumer compilation passed:

```sh
# actiongame/
env GOCACHE=/tmp/gekko3d-gocache go test ./... -run '^$'
# examples/destruction_derby/, using the existing workspace
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Local logs: `/tmp/gekko-s3b-actiongame-compile.log` and
`/tmp/gekko-s3b-example-workspace.log`. Example has no tests. Initial
`GOWORK=off` example check failed on missing module sums; existing workspace
provides local WebGPU replacement and compiles successfully.

Full ActionGame run failed six bot tests: selected-plan clearing, visible candidates, target prioritization, hearing falloff, direct danger and utility personality weighting. Reproduced at engine HEAD `d93f3ac` with archived engine/disposable workspace, preserving other modules and local WebGPU replacement:

```sh
# actiongame/, only the engine points to the HEAD archive
env GOWORK=/tmp/gekko-s3b-head.work GOCACHE=/tmp/gekko3d-gocache go test ./src/modules/startup -run '^TestActionGameBot' -count=1
```

Logs: `/tmp/gekko-s3b-actiongame.log` and
`/tmp/gekko-s3b-actiongame-baseline-workspace.log`. These consumer baseline
failures are outside S3b; no ActionGame code or tests were changed.

### Native GPU verification

Disposable public-API harness (`/tmp/gekko-s3b-smoke.go`, uncommitted): real bridge, saved streamed world, bounded uploads, render passes, point shadows, transparency and WebGPU. Build/run:

```sh
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/GekkoS3bSmoke.app/Contents/MacOS/GekkoS3bSmoke /tmp/gekko-s3b-smoke.go
/tmp/GekkoS3bSmoke.app/Contents/MacOS/GekkoS3bSmoke
```

Desktop exit 0: **857 frames over 29.352 seconds**. Eight stationary phases each held 88 completed frames for at least three seconds after settling. Instance/parameter/BVH builds, writes, binding revision and ownership stayed unchanged. Each frame checked camera/point/ambient values, camera/light buffers, frame progress and exact pass-union ownership.

Observed transitions:

- Ready fallback remained visible while full targets were resident, hidden and
  unfinished under paused upload budget. Restoring service completed full
  cohort and hid still-resident fallback.
- Real ECS translation/rotation retained object identity and refreshed instance
  rows, relative BVHs and writes. Hiding transparency and removing moved
  object pruned pass membership/templates; ownership fell from five to four to
  three. Material-offset changes after removal refreshed parameter rows.
- Replacing `InstancesBuf` with equal-capacity native buffer preserved CPU
  counts (7 instances, 12 parameter templates, 17 BVHs), queued exactly one new
  record write (59 to 60), and advanced `SceneBindingRevision` from 5 to 6.
- Explicit invalidation rebuilt three remaining templates and three
  nonempty BVHs and republished all nine record buffers (60 to 69 writes).
- Moving observer away unloaded full targets, restored proxy and
  reduced retained templates to two; coarsened records then remained idle.

Screenshots showed red/cyan refined geometry and yellow fallback. Native log: `/tmp/gekko-s3b-smoke.log`. First harness stopped at frame one after incorrectly counting ambient light as GPU row. Bridge stores it separately in `Scene.AmbientLight`; only disposable fixture corrected before rerun.

Established packing/work/publication contracts and visible continuity. No pixel parity, speedup, process memory bound or scene-record byte ceiling established.