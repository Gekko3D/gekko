# ECS Internals

This page explains how the `gekko` ECS works well enough to make safe agent-driven changes.

Read this when a task touches:

- component add/remove behavior
- query behavior
- archetype transitions
- reflective component access
- buffered mutation timing

## Core Model

`gekko` uses an archetype ECS.

Key ideas:

- entities live in archetypes defined by their component set
- each archetype stores one typed slice per component type
- entity lookup maps entity ID to current archetype
- component add/remove moves the entity to a different archetype

Important files:

- `ecs.go`
- `ecs_query.go`
- `ecs_reflect.go`
- `ecs/` package

## Entity and Component Identity

The ECS tracks:

- `EntityId`
  - opaque entity identity
- `componentId`
  - runtime integer ID assigned per component type
- `archetypeKey`
  - sorted list of component IDs
- `archetypeId`
  - hash-derived ID for the archetype key

The canonical identity of an archetype is the sorted component key, not the hash alone.

## Storage Layout

Each archetype stores:

- `entities`
  - entity ID to row mapping
- `componentData`
  - component ID to typed slice
- `recycled`
  - free rows reused after removal

The ECS itself stores:

- `archetypes`
  - all known archetypes
- `entityIndex`
  - current archetype for each entity

Component storage uses reflection helpers from `ecs_reflect.go`, but the underlying layout is still “typed slice per component.”

## Adding and Removing Components

Component changes are structural changes.

When adding components:

1. find the source archetype
2. compute the destination archetype key
3. reserve a row in the destination archetype
4. move overlapping component data
5. write new components
6. recycle the old row

Removing components works the same way, except the destination archetype is the subset of kept components.

This means:

- add/remove is not an in-place map update
- code that changes component sets should expect archetype migration

## Committed Structural Revision

`Ecs.StructuralRevision()` and `Commands.StructuralRevision()` expose a nil-safe
`uint64` invalidation stamp owned by shared `ecsStorage`. Value-copied ECS
wrappers read the same stamp. Reads are main-thread only, as are structural
mutation and query iteration; this API does not add concurrent ECS access.

The stamp advances once after each successful outer insertion, removal,
component addition/replacement or component removal. Migration's internal row
recycling does not advance it again. Equal-value replacement counts because
storage can relocate, and removing an absent component type preserves the
existing conservative row migration and advances the stamp.

Buffered command enqueueing, empty flushes, reads/type registration, direct
component field writes, sanitized empty public component commands and ignored
missing-entity commands leave it unchanged. Command mutations advance it at the
existing flush boundary. Treat it as invalidation for membership and row
locations, rather than a component-value version or semantic event count.

## Explicit Component Publication Revisions

`Ecs.ComponentRevision(reflect.Type)` and `Commands.ComponentRevision(reflect.Type)`
read an aggregate `uint64` publication sequence per component type in shared
`ecsStorage`. Copied wrappers observe the same owner; independent storage owners
have independent sequences. Zero means no publication yet. Each published type
retains one scalar after its last component disappears, so removal and later
re-admission cannot reset its sequence. Compare sequences within the same owner.

A successful outer committed insertion publishes every present type once.
Component addition publishes only supplied types, including equal-value
replacement; component removal publishes only types actually removed; entity
removal publishes all formerly present types. Duplicate supplied types publish
once per operation. Publication follows index/group synchronization at the
existing flush boundary. Migration copies and internal recycling publish no
change for unchanged types. Removing an absent type can still relocate rows and
advance the structural stamp without advancing a component publication sequence.

`MarkComponentChanged(entityId, reflect.Type)` immediately advances the requested
type's sequence and returns true only for a currently committed live component.
It does not write or compare values, move rows, enqueue commands, flush or change
the structural stamp. Pending additions cannot be marked until committed; pending
removals stay markable until removed at flush. Explicit publication of equal
values is valid. Reads and failed marks do not register component IDs or allocate
publication history.

Both APIs accept a struct type or one pointer to that struct as the same type.
Nil/zero owners, nil types, non-struct types and deeper pointers return zero/false.
Reads/query registration, enqueueing, empty flushes, sanitized empty component
commands and ignored missing entities publish nothing. These APIs are main-thread
only and retain no component values, pointers, queries or entity tombstones.

Direct public-field writes remain live but require their owner to explicitly
mark them for publication. Hierarchy publishes its changed derived world and
root-local TRS outputs; its input owners, animation, brushes and other producers
remain incompletely migrated. Hierarchy and the voxel bridge continue their live
reads. These sequences establish an ownership API, not a complete dirty contract,
entity worklist or performance gain. Remaining producer migration and any bounded
change journal need subsequent designs; consumers must not skip existing live
reads based on these sequences yet.

## Hierarchy Ownership

The private hierarchy owner in `ecsStorage` shares membership, topology and
counters across copied ECS wrappers. Independent storage owners remain isolated.
`TransformHierarchySystem` caches all Transform membership and row locations by
committed structural revision, and reacquires current typed columns on every
invocation. It retains no component pointers or typed column aliases. Queued
commands remain invisible until the existing flush; hierarchy never flushes them.

All child Parent edges are read before resolution. Direct edge changes rebuild
an iterative order that memoizes valid and invalid paths. Children require
Transform, LocalTransform and Parent; Transform sources without LocalTransform
remain usable even when they have Parent. Missing parents, cycles and their
descendants retain world TRS, while unrelated valid branches continue. Repair
resumes propagation in the same invocation. Roots with LocalTransform and no
Parent mirror authoritative world TRS into local each time.

Resolvable children compare exact float bits of live parent world TRS, local
TRS and their last produced world TRS. Parent reads follow ancestor composition,
so descendants receive the newly resolved world values. Unchanged inputs and
outputs skip composition; direct child-world edits are repaired. Signed zeros
are distinct and unchanged NaN bits stabilize reuse. Pivot is excluded and
preserved. Structural rebuilding may conservatively recompose valid children.

Hierarchy immediately marks a root's LocalTransform or a valid child's Transform
only when its actual destination TRS bits differ after the output write. Compare
the live destination before composition, so repairing a direct child-world edit
publishes even when the result matches the cached output. Equal outputs publish
nothing, including initial correct values, structural recomposition, equivalent
inputs and identical NaN bits; signed zero changes count. Pivot is excluded.
Input reads do not mark Parent, child LocalTransform or source world Transform.
Invalid branches retain their output and publish nothing. Output publication
does not flush commands, migrate rows or advance the structural stamp. This
migrates hierarchy outputs only; live extraction remains authoritative until
the remaining producers are covered.

Nil-safe `Ecs.TransformHierarchyStats()` and `Commands.TransformHierarchyStats()`
return cumulative `TopologyBuildCount` and `CompositionCount` plus the last
prepared `TransformCount`. Reads do not prepare membership or propagate values.
First and changed empty topologies count as builds; root mirroring does not
count as composition. These are main-thread diagnostic counters, not a
concurrent access API or a measured performance claim. Removed references and
backing-array tails are cleared. Empty membership releases aggregate storage;
nonempty maps and slices may retain peak capacity without a byte ceiling.

## Query Model

Queries are typed and generated by generic helpers:

- `MakeQuery1`
- `MakeQuery2`
- `MakeQuery3`
- `MakeQuery4`
- `MakeQuery5`

Each query:

- resolves the requested component IDs
- iterates matching archetypes
- passes pointers to component rows into the callback

Queries also support:

- `Without(...)`
  - exclude archetypes containing specific component types
- optional components
  - passed as extra type arguments to `Map(...)`

Example shape:

```go
MakeQuery3[TransformComponent, Parent, LocalTransformComponent](cmd).
    Without(SomeMarkerComponent{}).
    Map(func(eid EntityId, tr *TransformComponent, parent *Parent, local *LocalTransformComponent) bool {
        return true
    })
```

## Query Behavior Agents Should Remember

- Query callbacks operate on current ECS storage, not pending command buffers.
- Adding or removing entities/components through `Commands` during a query does not change the live iteration set until the stage flush.
- Query order is not a stable semantic guarantee.
- `Without(...)` filters archetypes, not individual callback rows after the fact.

If a change depends on newly added entities being queryable immediately, the fix is almost always stage placement or a later system, not trying to force the query model.

## Pointer Semantics

Query callbacks receive pointers into archetype slices.

That means:

- mutating the pointed-to component changes live ECS data immediately
- keeping those pointers past the callback is unsafe
- component-set changes during the same frame can move the entity to a different archetype later

An inventory may retain entity IDs and row locations while the storage owner
and structural stamp match. It must acquire current typed columns for every
iteration and discard locations after any stamp change. Immediate structural
mutation or a manual command flush during iteration is unsupported; field
writes and buffered commands follow the existing query rules.

Agents should treat query callback pointers as ephemeral row-local access.

## Reflection Helpers

`ecs_reflect.go` and the lower-level `ecs/` package provide:

- typed-slice creation
- get/set by row
- append helpers
- `AnySlice`

These exist so the ECS can maintain typed component slices without hand-writing storage code per component type.

## Common Failure Modes

- component type mismatch panic
  - a non-struct or unexpected pointer type was used as a component
- “query didn’t see the entity I just spawned”
  - command buffering delayed visibility until stage flush
- stale pointer assumptions
  - query callback pointers were treated like stable object references
- structural churn regression
  - code repeatedly adds/removes components each frame, causing unnecessary archetype movement

## Safe Editing Heuristics

- Prefer mutating component fields over add/remove churn when the component should persist.
- Use `Without(...)` when the absence of a component is part of the ownership rule.
- Be careful when adding new systems that mutate structure in the same stage as systems that query those entities.
- If you need cross-stage visibility, schedule the producer earlier or the consumer later.

## Best Reading Order

1. `ecs.go`
2. `ecs_query.go`
3. `commands.go`
4. `app.go`

Then inspect the owning module that is issuing the mutations.
