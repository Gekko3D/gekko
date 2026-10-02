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
root-local TRS outputs; reparent, grip, attach, surface and authored aim helpers
publish their changed transform inputs. Authored animation publishes changed
final Local poses; physics publishes changed World poses. Accepted moving-brush
motion publishes its changed World and existing Local poses. The shared grounded
actor writer publishes its changed World and Local poses for motors and riders.
The ground visual helper publishes changed direct-child Local Y offsets.
Renderer/physics bridges publish changed geometry-reference normalization;
the renderer bridge publishes its changed derived Pivot.
Core camera controllers and EntityLOD selection publish their changed outputs.
Other transform and renderer-input producers remain incompletely migrated.
Hierarchy and the voxel bridge continue their live reads.

## Bounded Component Publication Journal

`Ecs.ComponentPublicationCursor()` and its `Commands` forwarder capture an
opaque comparable watermark for the shared storage owner. Copied ECS wrappers
share that owner; independent worlds do not. Cursors retain a separate identity
token, never the ECS storage or component data. These APIs are main-thread only.

`ComponentPublicationsSince(cursor)` returns a `ComponentPublicationBatch` with
independently owned `Publications`, the read-time `Cursor` and `Resync`. Each
record identifies an `Entity` and canonical struct `ComponentType` at the same
publication points as the aggregate revisions above. Order across publications
is retained; type order within one structural operation is unspecified. Records
are invalidations: resolve current committed state, which may already lack the
entity or component. Direct unmarked writes generate no records.

`ComponentPublicationHistoryLimit` is 1,024 records globally across entity/type
pairs. The ring allocates lazily; reads never flush, drain or acknowledge it.
Exactly 1,024 unseen publications remain recoverable; a further publication
requires resync. Zero/unbound, foreign, future and expired cursors return
`Resync: true`, no partial suffix, and the current watermark. Nil/zero owners
return resync with a zero cursor. An owner-bound sequence-zero cursor is valid.
Sequence wrap rotates the identity token and clears prior history.

Advance to the returned cursor only after successful processing or a committed
full rescan. Use the returned read-time watermark for resync: publications during
the rescan remain available on the next read. Reacquiring a cursor after the
rescan could skip those publications. Independent readers can replay history.

Keep `StructuralRevision` checks for membership/row changes that publish no type,
including absent-type removal and zero-component entities. The journal bounds
only its retained records, not caller-owned batches, tokens or total ECS memory.
It does not establish complete producer notifications; live extraction remains
required. Rationale: [S3q ownership decision](../roadmaps/streamed-rendering-s3e.md#s3q-bounded-publication-journal-decision).
These sequences establish an ownership API, not a complete dirty contract,
entity worklist or performance gain. Remaining producer migration and any bounded
change journal need subsequent designs. Consumers must not skip existing live
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
does not flush commands, migrate rows or advance the structural stamp. Helper
input publication is owned separately, as described below.

Nil-safe `Ecs.TransformHierarchyStats()` and `Commands.TransformHierarchyStats()`
return cumulative `TopologyBuildCount` and `CompositionCount` plus the last
prepared `TransformCount`. Reads do not prepare membership or propagate values.
First and changed empty topologies count as builds; root mirroring does not
count as composition. These are main-thread diagnostic counters, not a
concurrent access API or a measured performance claim. Removed references and
backing-array tails are cleared. Empty membership releases aggregate storage;
nonempty maps and slices may retain peak capacity without a byte ceiling.

### Transform Helper Publication

`ReparentPreservingWorldTransform` publishes a changed Parent only after local
conversion succeeds. Failed conversion restores the old Parent and publishes
no attempted input write. Its initial hierarchy invocation can still publish
legitimate output changes before failure. Existing graph validation is unchanged;
an accepted descendant reparent can create a cycle whose world outputs are retained.

`setEntityWorldTransform` publishes LocalTransform only when the actual committed
destination TRS bits change. `setEntityWorldRotation` and grip/reset/aim/IK callers
of these setters in `asset_grip.go` inherit that publication. Equal bits,
including identical NaN payloads, publish nothing; signed zero and changed NaN
payloads count. Quaternion conversion keeps
its existing normalization. Without Parent, the setter changes only local TRS;
the next hierarchy invocation mirrors authoritative world TRS back into local.
World and Pivot semantics remain unchanged.

`RestoreAuthoredAssetAttachmentMount` evaluates the mount before comparing and
writing Parent/local values. It publishes each changed input even when the new
host cannot resolve and hierarchy retains world output. A helper's boolean
reports its outcome, not a publication transaction: a completed inner setter
remains published when a later IK step fails.

Reparent and grip helpers neither flush commands nor change the structural stamp.

### Authored Attach and Surface Publication

`AttachAuthoredAssetRoot` publishes its direct LocalTransform assignment only when
the actual destination TRS bits change. It marks before its existing structural
flush. That flush publishes supplied Parent/attachment components, including
equal replacements; carrying LocalTransform through migration does not publish
it. Later mount restoration owns any further input changes separately. Equal
reattachment keeps LocalTransform quiet when restoration and hierarchy are also
unchanged. Early validation errors neither write inputs nor flush queued work.

`AlignAuthoredAssetSurfaceMount` evaluates hierarchy and the marker first, then
publishes changed world TRS and optional local TRS independently. Each comparison
uses actual destination bits, including copied scale; unchanged NaN payloads are
quiet and signed zero differs. Scale and Pivot retain their existing semantics.
It does not create a missing LocalTransform, flush commands or change membership.
The final hierarchy call owns descendant outputs. A failed lookup can still
follow legitimate initial hierarchy publications; false is not a rollback signal.

### Authored Aim Publication

`ApplyAuthoredAimOffset`, `ApplyAuthoredAimRig`, `ApplyAuthoredAimRigAtPoint` and
`ApplyAuthoredAimRigAtRay` share a rotation writer that publishes changed
LocalTransform values per bone. It compares actual destination TRS bits before
and after each assignment. Equal bits publish nothing; unchanged position/scale
NaN payloads do not make a rotation write dirty. Existing quaternion normalization
can still change local rotation with zero requested offset or zero weight.
Signed-zero rotation changes also publish, even when orientation is unchanged.

Missing LocalTransform bones are skipped. Existing Local-only bones remain
accepted, including the existing root-rotation fallback when parent World is
missing. The boolean reports whether a LocalTransform was processed, not whether
its value changed. An initial hierarchy invocation can publish legitimate outputs
before a later failure.

Aim writes leave world propagation to the caller or scheduled hierarchy. A root
used as an aim bone still has authoritative World restored into Local by the next
hierarchy invocation. Weighting, frame resolution, position/scale, Pivot and command
boundaries retain their existing behavior; aiming does not flush or create Local.

### Authored Animation Publication

`assetAnimationSystem` and `SampleAuthoredAssetAnimation` publish changed committed
LocalTransform values after the complete pose sample. They compare each selected
target's actual TRS bits before sampling with its final bits after bind reset,
base clip and ordered layers. Equal final poses publish nothing, even when
intermediate writes differ. Bind-only restoration and layer removal publish when
the final pose changes. Equal NaN payloads compare quiet; signed-zero changes
publish.

Target selection retains the existing asset-ID and ancestor filters. Local-only
descendants remain accepted; missing Local components are not created. Duplicate
item IDs retain the existing unspecified winner, with publication attached to
the selected destination.

The system skips an invalid base clip before sampling. The public sampler still
accepts an eligible nonempty animation set with a missing base clip, resets bind
transforms and applies valid layers without advancing time. Player state, layer
policy, masks and root-position key locks retain their existing behavior.

Sampling publishes Local only and leaves World propagation to hierarchy. It does
not flush commands or change membership. Animation-player and NPC state changes
are outside this Local publication contract. Other transform and renderer-input
producers still need migration.

### Physics World Publication

`SynchronousPhysicsSystem` and `PhysicsPullSystem` publish changed committed
`TransformComponent` values on the main thread. Each writer compares actual World
TRS bits immediately before and after its position/rotation assignments. The
comparison uses the converted render pose, including existing pivot/center offset
handling and interpolation, rather than the raw simulation center.

Equal destination bits publish nothing, including equal NaN payloads. Signed zero
changes publish. A repeated result tick can still publish when a different
interpolation alpha changes World; tick, velocity and sleeping state do not decide
whether a pose changed.

Existing body/model filters, rigid-body updates, collision delivery and timing
remain intact. Scale and Pivot are preserved; physics does not write Local or
invoke hierarchy. Derived hierarchy outputs retain their normal stage ownership.
Fallback PhysicsModel additions remain queued. Publication does not flush
commands or change membership; committed targets pending removal/replacement
remain writable until the existing flush boundary.

### Moving Brush Motion Publication

`movingBrushMotionSystem` publishes changed committed World and existing Local
TRS for the brush at its accepted motion commit. Each destination compares exact
bits immediately before and after its own assignments. A stationary World can
repair mismatched Local and publish Local alone; moved World can reach an
already-equal Local and publish World alone. Equal bits, including preserved NaN
payloads, publish nothing; signed-zero changes publish.

Temporary proposals and blocked door/carrier motion do not publish TRS. Rejection
also skips Local repair; existing control-state changes such as reopening a
blocked closing door remain intact. Motion math, path/wait handling, collision
bounds, World Pivot and Scale, and rider behavior are preserved. Existing numerical
World-to-Local copies retain their semantics, including brushes with Parent;
normal later hierarchy propagation keeps its stage ownership.

The writer creates no Local, invokes no hierarchy and does not flush or change
membership. Committed targets pending removal/replacement remain writable until
flush; pending additions remain invisible. Rider poses follow the
[grounded actor contract](#grounded-actor-publication). Publication of brush
control state or collision bounds remains outside this TRS contract.

### Grounded Actor Publication

`groundedPlayerApplyTransform` publishes changed committed World and existing
Local TRS on the main thread for motor, traversal and rider callers. Each
destination compares exact bits immediately before and after its own three
assignments, independently of the other destination. The existing writes copy
the supplied base position, reset rotation to identity and scale to ones;
unchanged position can still require publication for rotation/scale repair.
Equal bits, including equal NaN position payloads, publish nothing; signed-zero
changes publish. World Pivot remains untouched.

Camera updates keep their existing order and eye height, LookAt and Up behavior.
Missing camera, World or Local does not create a component or reject the other
writes. A supplied camera still updates when no transform target is committed.
The supplied nonnil controller need not be a committed component of the target;
nil commands or controller skip the whole writer. Existing numerical base position
copies retain their semantics with Parent, and normal hierarchy propagation keeps
its stage ownership.

Publication does not flush, change membership or invoke hierarchy. Committed
targets pending removal/replacement remain writable until flush; pending
additions remain invisible. Camera, motor and intent publication, and further
transform writers, remain outside this TRS contract.

### Ground Visual Publication

`ApplyCharacterVisualGroundOffsetToChildren` publishes LocalTransform immediately
when replacing a committed direct child's Local Y changes its exact float bits.
Equal Y bits, including identical NaN payloads, publish nothing; signed-zero and
NaN-payload changes publish. Other Local fields remain untouched and do not
affect this comparison. Matching children need Parent and Local, but neither
child World nor a committed parent entity is required. Grandchildren and other
parents are excluded.

The helper creates no components, invokes no hierarchy and does not flush or
change membership. Pending additions remain invisible; pending removal or
replacement leaves the current component writable until flush. Later hierarchy
owns World propagation. Ground probes and smoothing retain their behavior.

### Voxel Bridge Publication

`voxelRtSystem` and `VoxPhysicsPreCalcSystem` immediately publish
VoxelModelComponent when their existing normalization changes committed
SharedGeometry. Compare the actual destination before normalization. Publication
precedes asset-resolution failure, LOD return or physics build reuse when those
paths follow normalization. Existing nil-server and renderer hidden-entity
filters remain; already-normalized and zero references publish nothing. Geometry
precedence and copy-only edit/destruction normalization retain their behavior.
`ResolveVoxelGeometry` has no ECS owner and does not publish supplied pointers.

At its existing derived Pivot assignment, `voxelRtSystem` publishes TransformComponent
only when actual destination Pivot bits change. Equal NaN payloads are quiet;
signed-zero and changed NaN payloads publish. Unchanged TRS does not affect that
comparison. Source-space center/custom/corner and LOD adjustment formulas, effective
scale, renderer object comparisons and hierarchy Pivot exclusion are unchanged.
Failed geometry and sprite LOD returns that precede Pivot assignment do not write
or publish Pivot.

Neither annotation flushes or changes membership. Pending additions stay invisible;
pending removal/replacement remains current until flush. PhysicsModel output
commands retain their separate structural publication at flush. Collision data,
snapshot ownership and cache policy are unchanged.

### Camera and EntityLOD Publication

`FlyingCameraControlSystem` publishes changed committed CameraComponent pose,
Yaw and Pitch after its complete control write. The grounded control system
publishes changed legacy look inputs immediately after `applyGroundedLook`,
before later pose application. Its existing intent filter and time guards remain.
`groundedPlayerApplyTransform` publishes changed camera pose only when the supplied
pointer is the target entity's current committed CameraComponent. Detached,
wrong-entity or missing-target pointers retain their existing writes without
publishing an unrelated target. No writer creates a Camera.

Comparisons use exact bits of Position, LookAt, Up, Yaw and Pitch. Equal NaN bits
are quiet; signed-zero and NaN-payload changes publish. Projection fields are
preserved and excluded, so nonfinite Fov/Aspect/Near/Far cannot dirty a pose write.
Camera math, defaults, pitch clamping and grounded update order are unchanged.
Flying control-state and grounded motor/intent publication remain separate.

`entityLODSelectionSystem` publishes EntityLODComponent only when its complete
selection or clear changes SelectionValid, ActiveDistance, ActiveBandIndex,
ActiveMaxDistance or ActiveRepresentation. Distance fields compare exact bits.
Camera motion within a band can publish distance alone; repeated selection/clear
is quiet. Disabled, missing-camera and invalid-band clearing, camera precedence
over renderer fallback, authored bands/metric and ownerless selection helpers
retain their behavior. Neither publication flushes, changes membership or invokes
hierarchy; queued component mutations keep their existing visibility boundary.

Live extraction remains authoritative; consumers cannot yet skip reads using
publication revisions.

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
