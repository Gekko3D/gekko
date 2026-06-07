# Baked Voxel Normals Status

- Date: 2026-06-06
- Owner bucket: VoxelRT renderer core, GPU upload, sparse voxel storage, voxel-reading shaders
- Scope: replace hit-time 6-neighbor normal sampling with uploaded per-voxel baked normals while preserving occupancy-driven voxel shading semantics

## Summary

Voxel normals now bake during voxel upload into the existing dense-occupancy storage binding, which has been expanded into a voxel auxiliary sidecar. The sidecar stores dense occupancy words first, followed by one packed normal byte per voxel. G-buffer, transparent overlay, and particle collision paths load the baked normal for the hit voxel instead of sampling six neighbors to resolve the normal.

## Preserved Invariants

- Voxel material identity remains palette-driven and block-stable.
- Visible voxels still use one stable lighting normal.
- Normals remain occupancy-driven with deterministic fallback data baked before upload.
- Terrain chunk and planet tile seam occupancy can influence baked normals across object boundaries.
- AO remains separate from normal resolution and keeps its existing sampling behavior.

## Invalidation

`XBrickMap.SetVoxel` now dirties the edited brick plus its 6-neighbor brick halo so same-object baked normals are refreshed. `GpuBufferManager.UpdateVoxelData` also propagates dirtying across scene object boundaries:

- terrain chunks dirty matching boundary bricks in adjacent chunks
- planet tile edits conservatively dirty adjacent tile bricks

## Verification Run

- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume ./voxelrt/rt/gpu ./voxelrt/rt/shaders`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core ./voxelrt/rt/app ./voxelrt/rt/gpu ./voxelrt/rt/volume ./voxelrt/rt/shaders`

## Not Verified

- Manual GPU visual smoke test in a windowed scene.
- Specific visual pass needed: edited voxel on a chunk boundary, thin one-voxel features, non-uniform scale, transparent voxel surface, and particle collision against voxel surfaces.
