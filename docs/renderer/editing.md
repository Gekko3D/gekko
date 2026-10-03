# VoxelRT Picking and Editing

This document describes the current picking and voxel-edit behavior exposed through the bridge.

## Overview

There is no separate `rt/editor` package. Editing flows through:

- `mod_voxelrt_client.go`
- `mod_voxelrt_client_systems.go`
- `voxelrt/rt/core/scene.go`
- `voxelrt/rt/volume/xbrickmap_edit.go`

## Main APIs

`VoxelRtState` exposes the public helpers:

- `ScreenToWorldRay(mouseX, mouseY, camera)`
- `Raycast(origin, dir, tMax)`
- `RaycastSubstepped(...)`
- `VoxelSphereEdit(entityId, worldCenter, radius, value)`
- `GetVoxelObject(entityId)`

## Data Flow

1. Build a world ray with `ScreenToWorldRay`.
2. Call `Raycast` or `RaycastSubstepped`.
3. Apply an edit against CPU-side voxel data.
4. Let the normal renderer update path upload the change on the next frame.

Editing helpers mutate CPU-side `XBrickMap` data. They do not force an immediate GPU redraw on their own.

## Synchronous primitive edits

`volume.Sphere` and `volume.Cube` retain their existing shape predicates and
ordered voxel assignments. CPU invocations finalize material flags once per
surviving touched brick and invalidate each exact normal-halo key once. Voxel
changes, revisions, sector removal and cached bounds still update in assignment
order; all flags and auxiliary invalidation are complete before the call returns.
No-op assignments preserve auxiliary data and allocate no batch maps.

An invocation starting in `GPUEditMode` keeps sequential `SetVoxel` calls,
including when a callback changes that mode. Queue order, prewrite observations,
reentry and dirty suppression retain their existing behavior. Engine world-space
sphere edits use this path after their existing transform conversion. Single-voxel
edits keep their current path. Publication remains
synchronous; these helpers introduce no resumable or progressive visibility.

## Raycast Internals

`Scene.Raycast` currently:

- scans `Scene.Objects`, not `VisibleObjects`
- broad-phases against each object's world AABB
- transforms the ray into object space
- delegates voxel traversal to `XBrickMap.RayMarch`
- returns the nearest hit with object pointer, voxel coordinate, world distance, and normal

That means picking remains CPU-authoritative even when rendering uses GPU culling and GPU-side acceleration structures.

## Debug and Overlay Notes

- `App.DebugMode` enables the debug compute pass and profiler HUD.
- `Camera.DebugMode` is a separate shader-side debug mode.
- `RenderMode` is another separate output mode.

If a debug change appears ineffective, verify which control actually owns the output you are looking at.

Text and gizmos are frame-lifetime data:

- text is cleared in `Prelude` and must be resubmitted every frame
- gizmos are rebuilt from ECS every frame

## Notes

- Long-range picking should prefer `RaycastSubstepped`.
- GPU buffer reallocation and bind-group rebuilds happen in `App.Update()` and `GpuBufferManager`; edit helpers only change CPU-side scene data.
