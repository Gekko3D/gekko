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

## Ordered edit streams

`XBrickMap.ApplyVoxelWrites(iter.Seq[volume.VoxelWrite])` consumes ordered
assignments once on an existing map; a nil sequence is a no-op. CPU callers own
the map exclusively during the call. Each write updates payload, revisions,
membership and cached bounds before control returns to the producer. Material
flags finalize before return, including the applied prefix if the producer
panics; that panic propagates unchanged. Normal halos retain exact coverage.

The CPU producer may read current voxels through `GetVoxel` to choose later
writes. It must not inspect deferred material flags or `AtlasOffset`, mutate or
reenter the target, publish it, or change its edit mode. An invocation beginning
in `GPUEditMode` uses ordinary sequential `SetVoxel` throughout, preserving
callback observations and reentry even if a callback changes that mode. No
intermediate write list or new shared owner is introduced.

## Managed voxel ownership

`volume.NewManagedXBrickMap(source)` explicitly seals a defensive copy behind
`ManagedXBrickMap`; nil input creates an empty owner. Existing `XBrickMap` and
public `Brick.Payload` access retain their mutable contracts. Managed maps are
CPU owners and do not inherit the source's GPU editing mode.

`GetVoxel`, `SetVoxel` and `ApplyVoxelWrites` retain dense voxel, material,
revision, bounds and fitted-normal halo semantics. Sealed `Fork` shares private
bricks with independent metadata. Writes detach affected bricks before mutation,
including halo neighbors. `Snapshot` returns an independent ordinary map with
the existing `XBrickMap.Copy` identity, upload-state and metadata behavior.

`TrackedChanges` returns owned final assignments relative to the construction
base, sorted by z, y and x. It includes explicit zero removals and omits reverted
cells. Sealed forks inherit the original base and changes. Ordered producers can
read current voxels; applied writes remain tracked if the producer panics.

`ExposeMutable` irreversibly detaches shared storage and returns one stable dense
authority. Later raw writes remain visible; `TrackedChanges` returns `(nil,
false)` permanently. This owner releases its base/history; earlier forks remain
isolated. Forking an exposed owner seals a fresh construction base from current
raw data. Persistence consumers must use full snapshots after exposure.

Each owner requires exclusive access during operations. Independent forks may
edit concurrently after fork construction completes. Source and exposed raw maps
must also be exclusively owned while owner operations access them. Producers
must not mutate, reenter, expose or fork the owner while consuming its edit stream.

This boundary retains inline dense bricks. Asset, renderer and persistence
integration follow separately; no existing runtime path automatically opts in.
See [ownership rationale](../roadmaps/streamed-rendering-p1c.md).

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
