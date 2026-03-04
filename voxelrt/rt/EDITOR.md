# VoxelRT Picking and Editing

This document describes the current picking/editing API exposed by the VoxelRT bridge.

## Overview

There is no separate `rt/editor` package anymore. Editing utilities are provided through:

- `gekko/mod_voxelrt_client.go` (`VoxelRtState` helpers)
- `gekko/mod_voxelrt_client_systems.go` (ECS-to-renderer bridge loop)
- `gekko/voxelrt/rt/core/scene.go` (raycast implementation)

## Main APIs

`VoxelRtState` helpers:

- `ScreenToWorldRay(mouseX, mouseY, camera)` - build world ray from screen coordinates
- `Raycast(origin, dir, tMax)` - query voxel scene hit (`RaycastHit`)
- `RaycastSubstepped(...)` - segmented raycast for long distances
- `VoxelSphereEdit(entityId, worldCenter, radius, value)` - apply sphere voxel edit in object space
- `GetVoxelObject(entityId)` - fetch object bound to ECS entity

## Data Flow

1. Build a ray from input using camera state (`ScreenToWorldRay`).
2. Call `Raycast` to get entity + voxel coord + normal.
3. Apply edit (for example `VoxelSphereEdit` or direct `SetVoxel` edits on object map).
4. Let the normal render loop sync GPU state on `Update()`.

## Raycast Internals

`Scene.Raycast` in `rt/core/scene.go`:

- broad phase: object/world AABB tests
- narrow phase: object-space traversal against `XBrickMap`
- returns nearest hit with object pointer, voxel coordinate, distance, and normal

## Notes

- CA volume voxel bridging is handled in `mod_voxelrt_client_systems.go`.
- Edit queues/resources are module-level (`VoxelEditQueue`) and applied through bridge systems.
- GPU buffer reallocation and bind-group rebuilds are handled by `GpuBufferManager` during update; editing helpers do not directly force render-pass rebuilds.
