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

Global illumination verification steps were removed. The renderer currently verifies direct lighting, shadows, voxel edits, particles, CA volumes, gizmos, and overlay paths only.
