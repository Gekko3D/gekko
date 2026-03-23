# VoxelRT Picking and Editing

This document describes the current picking and voxel-editing behavior exposed through the VoxelRT bridge.

## Overview

There is no separate `rt/editor` package. Editing flows through:

- `gekko/mod_voxelrt_client.go`
- `gekko/mod_voxelrt_client_systems.go`
- `gekko/voxelrt/rt/core/scene.go`
- `gekko/voxelrt/rt/volume/xbrickmap_edit.go`

## Main APIs

`VoxelRtState` exposes the public helpers:

- `ScreenToWorldRay(mouseX, mouseY, camera)`
- `Raycast(origin, dir, tMax)`
- `RaycastSubstepped(...)`
- `VoxelSphereEdit(entityId, worldCenter, radius, value)`
- `GetVoxelObject(entityId)`

## Data Flow

1. Build a ray from input using `ScreenToWorldRay`.
2. Call `Raycast` or `RaycastSubstepped`.
3. Apply an edit against CPU-side voxel data.
4. Let the normal renderer update path upload the change on the next frame.

Important: editing helpers mutate CPU-side `XBrickMap` data. They do not directly force immediate GPU re-rendering.

## Raycast Internals

`Scene.Raycast` currently:

- scans `Scene.Objects`, not `VisibleObjects`
- broad-phases against each object's world AABB
- transforms the ray into object space
- delegates voxel traversal to `XBrickMap.RayMarch`
- returns the nearest hit with object pointer, voxel coordinate, world distance, and normal

That means picking remains CPU-authoritative even when the renderer uses GPU culling and GPU BVHs.

## Debug And Overlay Notes

- `App.DebugMode` enables the debug compute pass and profiler HUD.
- `Camera.DebugMode` is a separate shader-side debug mode.
- `RenderMode` is another separate output mode.

If a debug change appears ineffective, verify you toggled the right one.

Text and gizmos are also frame-lifetime data:

- text is cleared in `Prelude` and must be resubmitted every frame
- gizmos are rebuilt from ECS every frame

## Notes

- Long-range picking should prefer `RaycastSubstepped`.
- CA volume bridging is handled in `mod_voxelrt_client_systems.go`.
- GPU buffer reallocation and bind-group rebuilds are handled in `App.Update()` and `GpuBufferManager`; edit helpers only change CPU-side scene data.
