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

`CopyChangedSectors(previous, sinceRevision)` returns a clean immutable snapshot
with fresh identity. The previous geometry must be an unchanged snapshot of this
owner at that revision, or an inherited snapshot before fork divergence. Divergent
sibling snapshots are invalid inputs. Only previous immutable sectors may be
shared; no snapshot aliases mutable owner storage. Callers must keep shared
sectors and bricks immutable, using `Snapshot` when an independent mutable copy
is needed. Bounds/dirty metadata may be resolved without changing shared geometry.

Changed sectors include the full fitted-normal halo, even when only auxiliary
data changes and public `SectorRevisions` stays unchanged. Private publication
history follows forks independently and is dropped on exposure. Exposed owners
always copy fully because raw writes can bypass revisions. Publish only after
ordered edit finalization, including an applied panic prefix.

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

This boundary retains inline dense bricks. See
[ownership rationale](../roadmaps/streamed-rendering-p1c.md).

### Ordinary managed runtime geometry

`AssetServer.RegisterManagedVoxelGeometry(source, sourcePath)` defensively seals
an ordinary CPU source. `EnableManagedVoxelGeometry(cmd, assets, entity)` creates
an independent managed override using the existing asset lifetime. Repeated
calls resolve queued overrides without flushing ECS commands early. Neither
registration nor enable changes unrelated legacy geometry paths.

Managed runtime geometry supports ordinary full-density entities. Terrain,
planet, retained-renderer, LOD, streamed/imported, voxel-backing and GPU-first
owners require their existing paths. Enable and managed writes reject these
combinations before mutation. Registered managed sources also require eligible
attachments. Unsupported attachments have no managed renderer or voxel collider;
save lookup retains their actual source content. Use explicit legacy dense
registration for those owners.

`ApplyManagedVoxelWrites` consumes ordered assignments once and publishes an
authoritative snapshot after the batch, including the applied prefix on panic.
`ManagedVoxelGeometryChanges` reports construction-relative assignments only for
an enabled entity override. Saving clears publication dirtiness without resetting
that history. Producers may query changes but must not mutate, expose, reenter
or publish the target during consumption. Operations run on the engine thread.

The bridge uses an independent dense renderer derivative. Accepted writes patch
that derivative without replacing its pointer; source replacement and missed
publication use an authority-derived copy. Ordinary synchronization repairs an
unauthorized derivative pointer replacement. Direct derivative edits before
promotion are unsupported and never become collision or persistence authority.
Collision, navigation and save lookup use managed authority; authored pivot
bounds stay fixed while current collision bounds follow edited geometry.

Public `GetVoxelGeometry`, `GetVoxelModel`, `ResolveVoxelGeometry` and
`ResolveVoxelGeometryMap` permanently expose dense asset authority. Internal
engine reads preserve sealing. `PromoteRuntimeVoxelGeometry` instead adopts the
exact current renderer map when sealed. If an asset getter exposed authority
first, runtime promotion preserves that getter's exact pointer. Inherited sources
fork an entity override before runtime promotion, preserving siblings. Both paths
permanently disable tracking and retain existing full-snapshot persistence.
Unnotified raw writes retain existing collision-cache refresh limits; callers
must use normal revision-producing edits for an existing cached collider.

Built-in sphere edits enable and track eligible inherited managed sources.
Raw editing helpers and destruction promote before dense edits, preserving prior
managed changes. Runtime promotion retains persistence dirtiness even when raw
payload writes bypass map revisions. Geometry deletion releases its managed
sidecar; entity removal releases renderer bindings. Ordinary override assets
still require explicit asset deletion, as with existing unrefcounted geometry.

Authority publication reuses unchanged immutable sectors and copies changed
sectors plus fitted-normal halos after each changed batch. Metadata still scales
with retained sectors and revision tombstones. This does not bound total memory
or remove downstream physics copies. Compact payloads and runtime E2 delta
persistence remain follow-ups.

### Authored shape base provenance

Individual authored `voxel_shape` parts record canonical base identity at
construction, using the existing snapshot conversion and `ModelScale` resampling.
The rasterization version is `gekko-voxel-shape-v1`; changes to those construction
rules require a new version. Identity includes the effective voxel resolution
(`VoxelResolutionOrDefault`) and excludes placement transforms and renderer data.

AssetServer retains identity metadata per geometry asset and lattice, without a
second resident base map. Geometry cache keys and asset IDs remain unchanged.
A warm cache with missing lattice metadata reconstructs the authored shape once;
it never establishes canonical identity from the mutable cached map. Invalid
identity inputs or codec limits leave provenance absent without changing legacy
construction behavior.

Explicit managed enable verifies its isolated construction geometry against this
identity before capturing provenance. Later tracked writes retain that original
base identity. Internal qualification reads metadata without scanning geometry;
changed effective resolution, unsupported ownership, current geometry-reference
replacement or exposure selects full fallback. Public exposure permanently releases the
managed entry's provenance. Foreign overrides, unbound snapshot registrations
and generic managed registrations do not inherit authored eligibility.

Deleting a shared source releases its metadata while an already enabled owner
retains its independent provenance. Deleting the override releases that owner.
Other source kinds, level brushes and collapsed composites remain full fallback
until their canonical construction adapters exist. Persistence still writes legacy full snapshots. Runtime
[v2 shape loading](../content/streaming-and-worlds.md#ordinary-object-override-loading)
uses existing dense registration; managed restoration and S4 writing follow.

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
