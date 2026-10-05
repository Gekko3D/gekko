# VoxelRT Verification

Run commands from the `gekko` module:

`cd /Users/ddevidch/code/go/gekko3d/gekko`

Use a temporary Go cache in this sandbox:

`env GOCACHE=/tmp/gekko3d-gocache ...`

## Fast Targeted Checks

- Culling, scene, and camera changes:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`
- GPU manager, upload, bind-group, and shadow or Hi-Z changes:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- Sparse voxel storage, traversal, or edit changes:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume`
- BVH changes:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/bvh`

## Bridge-Level Checks

For ECS bridge changes touching `mod_voxelrt_client*.go`:

- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

That is broader than the renderer-only packages, but it catches bridge regressions that package-local tests will miss.

## When To Run More Than One Package

- Pass ordering or bind-group layout change:
  - `./voxelrt/rt/gpu`
  - `./voxelrt/rt/core`
  - then `./...`
- Voxel atlas page count, voxel payload bindings, or `BrickRecord` layout change:
  - `./voxelrt/rt/gpu`
  - `./voxelrt/rt/app`
  - then `./...`
- Probe GI or deferred-lighting change:
  - `./voxelrt/rt/gpu`
  - `./voxelrt/rt/core`
  - then `./...`
- Picking or voxel-edit change:
  - `./voxelrt/rt/core`
  - `./voxelrt/rt/volume`
  - then `./...` if the bridge changed
- Particle or sprite pipeline change:
  - `./voxelrt/rt/gpu`
  - then `./...` if emitter sync or atlas handling changed

## Visual Smoke Checks

Only use a windowed run when the change needs visual confirmation:

- editor:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko-editor && env GOCACHE=/tmp/gekko3d-gocache go run .`
- voxel sample:
  - `cd /Users/ddevidch/code/go/gekko3d/examples/testing-vox && env GOCACHE=/tmp/gekko3d-gocache go run .`

These need a real desktop session.

Global illumination verification steps were removed. The renderer currently verifies direct lighting, shadows, voxel edits, particles, gizmos, and overlay paths only.

## Packed Normal Workload Benchmark

`cmd/voxelbench` measures the production opaque G-buffer traversal with a
headless WebGPU device. It compares dense and opt-in packed fitted-normal storage
without changing renderer defaults. Fixtures are deterministic: sparse planes,
near-full mixed-material bricks, and sparse geometry with repeated membership,
normal-halo and visible surface edits. Geometry bounds stay fixed during edits.

Build from the engine module, then run separate paired processes:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./cmd/voxelbench
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/voxelbench ./cmd/voxelbench
/tmp/voxelbench -mode dense -workload sparse -output /tmp/voxelbench-dense.json
/tmp/voxelbench -mode packed -workload sparse -output /tmp/voxelbench-packed.json -compare /tmp/voxelbench-dense.json
```

Repeat for `-workload dense` and `-workload edited`. Collect at least three pairs,
alternating which mode runs first. Keep resolution, warmup, samples and dispatch
batch identical. Defaults are 640×480, ten warmup batches, thirty measured
batches and eight dispatches per batch. `-help` lists flags and input bounds.

GPU queries bracket one compute pass containing the G-buffer dispatch batch.
The timed pass is submitted and completed before a separate submission resolves
and copies its query values. Resolution, readback, CPU updates and completion
polling happen outside the timestamp interval. Results retain raw GPU ticks per dispatch rather than converting
them to time; ratios require the same adapter and backend. Never interpret these
numbers as nanoseconds. Pass queries require `timestamp-query` to be advertised
and requested on the device.

Each measured sample owns distinct query slots. Cohorts contain at most 128
samples and reserve three calibration pairs, bounding each query set at 262
slots even for large sample counts. Up to three unmeasured query calibration
batches precede each cohort; their counts and raw pairs are reported separately.
Invalid measured samples are never silently removed. Unsupported or unusable
queries report GPU timing unavailable and omit the ratio.

CPU scene commit/upload preparation and normal baking report nanoseconds
separately, with actual content-upload bytes and physical auxiliary capacity.
Initial admission is separate from warm samples. Physical capacity includes
allocator slack and replacement overhead, not merely live packet demand. This
benchmark submits real fences and advances retired resources during updates.

Reports retain raw timestamp pairs, median and nearest-rank p95, geometry
identities/counts, settings, adapter/backend and shader identity. Initial and final
production depth/normal/material readbacks must contain valid hits. The edited
fixture must change rendered depth or normals. `-compare` rejects mismatched
settings, geometry, identities or readbacks before computing a packed/dense
median ratio. Ratios below one favor packed storage for that measured fixture.
Use `-output` to retain a JSON report; diagnostics go to stderr.

An isolated opaque pass does not measure full-frame FPS, transparent traversal,
particle trajectories, every camera angle or complete editor/gameplay behavior.
Interpret repeated samples and their spread before choosing a storage policy.
