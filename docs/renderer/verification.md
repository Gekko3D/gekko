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
without changing renderer defaults. `-materials atlas|packed` independently
selects mixed-material storage. Fixtures are deterministic: sparse planes,
near-full mixed-material bricks, and sparse geometry with repeated membership,
normal-halo and visible surface edits. Geometry bounds stay fixed during edits.

Build from the engine module, then run separate paired processes:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./cmd/voxelbench
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/voxelbench ./cmd/voxelbench
/tmp/voxelbench -mode dense -workload sparse -output /tmp/voxelbench-dense.json
/tmp/voxelbench -mode packed -workload sparse -output /tmp/voxelbench-packed.json -compare /tmp/voxelbench-dense.json
```

To measure material packing, keep normal mode fixed and compare atlas/packed:

```sh
/tmp/voxelbench -mode packed -materials atlas -workload sparse -output /tmp/voxelbench-material-atlas.json
/tmp/voxelbench -mode packed -materials packed -workload sparse -output /tmp/voxelbench-material-packed.json -compare /tmp/voxelbench-material-atlas.json
```

Repeat with dense normals and for `-workload dense` and `-workload edited`.
Comparisons require exactly one storage policy to differ. Collect at least three pairs,
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
allocator slack and replacement overhead, not merely live packet demand. Atlas
physical capacity and assigned payload bytes are separate fields: packed
materials avoid slots and uploads while the atlas textures remain allocated.
Declared texture capacity does not measure resident GPU memory. This
benchmark submits real fences and advances retired resources during updates.

Reports retain raw timestamp pairs, median and nearest-rank p95, geometry
identities/counts, settings, adapter/backend and shader identity. Initial and final
production depth/normal/material readbacks must contain valid hits. The edited
fixture must change rendered depth or normals. `-compare` rejects mismatched
settings, geometry, identities or readbacks before computing the selected policy
median ratio. Version 2 records both policies explicitly and uses
`packed_material_to_atlas_median_ratio` for material comparisons, retaining
`packed_to_dense_median_ratio` for normal comparisons. Legacy version 1 reports
represent atlas materials and can compare only with version 1. Ratios below one
favor the packed policy for that measured fixture.
Use `-output` to retain a JSON report; diagnostics go to stderr.

An isolated opaque pass does not measure full-frame FPS, transparent traversal,
particle trajectories, every camera angle or complete editor/gameplay behavior.
Interpret repeated samples and their spread before choosing a storage policy.

## Packed Material Native Parity

The P3c diagnostic uses production shader accessors and rendered G-buffer,
WBOIT and spotlight outputs. It checks canonical occupied material lanes,
IDs 0/255, uniform shortcuts, raw normal bits, particle occupancy/normal inputs,
shared sectors, material edits, seams, deferred publication, growth and removal.

```sh
env GOCACHE=/tmp/gekko3d-gocache go build -o /tmp/gekko-p3c-native docs/roadmaps/diagnostics/p3c_packed_voxel_materials.go
/tmp/gekko-p3c-native -mode dense -materials atlas -output /tmp/p3c-dense-atlas
/tmp/gekko-p3c-native -mode dense -materials packed -output /tmp/p3c-dense-packed -compare /tmp/p3c-dense-atlas
/tmp/gekko-p3c-native -mode packed -materials atlas -output /tmp/p3c-packed-atlas -compare /tmp/p3c-dense-atlas
/tmp/gekko-p3c-native -mode packed -materials packed -output /tmp/p3c-packed-packed -compare /tmp/p3c-dense-atlas
```

Unlike workload timing comparisons, this parity diagnostic compares any policy
combination with the baseline. It requires nonempty transparency and shadow
captures and a visible material-edit contribution. Particle probes verify
collision inputs, not full simulated trajectories. These windowed runs require
a desktop session; they do not establish full-frame FPS or interactive gameplay.
