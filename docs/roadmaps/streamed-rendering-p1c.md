# P1c: Sealed managed voxel ownership

Status: volume boundary, ordinary runtime integration and incremental authority publication implemented.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md).

## Approved boundary

The user approved opt-in managed geometry on 2026-10-03. Managed owners retain
an immutable construction base and tracked edits. Public mutable exposure
irreversibly promotes that owner to dense authority and requires the existing
full-snapshot persistence fallback. Existing public mutable callers retain their
contracts. This is a long-term ownership boundary, not a temporary cache.

The alternative is a breaking public map migration across all consumers before
sharing. The opt-in boundary preserves compatibility while allowing independent
instances to share sealed geometry. Restricting existing raw writes or relying
on revision notifications from exposed maps would lose supported edits.

## Initial volume scope

Introduce `volume.ManagedXBrickMap` without changing `XBrickMap`, asset access,
renderer extraction, collision, navigation or persistence. Construction copies
the source defensively. Sealed forks share private bricks and independently own
map metadata. Writes detach affected bricks, including fitted-normal halo
neighbors, before using existing dense edit semantics. Inline dense payloads
remain in this first batch; public compact storage is not claimed.

The [managed ownership contract](../renderer/editing.md#managed-voxel-ownership)
defines snapshots, exposure, final changes, ordering and exclusive access.

## Follow-up dependencies

Runtime integration must qualify geometry explicitly and distinguish internal
sealed reads from public mutable exposure. Use existing asset and streaming
owners, byte accounting and dense fallback; do not introduce parallel residency.
E2 adds versioned base-bound delta encoding and S4 publication only after those
owners can retain tracking. Collision and navigation keep authoritative source
geometry and existing synchronous publication semantics. Public compact payloads,
progressive edits and asynchronous physics remain separate decisions.

Direct exported `VoxelRtState.RtApp.Scene.Objects` and related scene slices expose
`VoxelObject.XBrickMap` without a getter. Getter hooks therefore cannot reliably
detect public runtime exposure. Promoting at scene attachment preserves raw
authority but ends sealed tracking there. A separate dense renderer derivative
retains managed authority only with explicit rules for raw derivative writes,
pointer replacement and collision/persistence precedence. That extraction and
compatibility decision was approved for explicitly opted-in managed entities on
2026-10-03: managed APIs own authoritative edits, renderer maps are independent
derivatives, and raw scene editing requires explicit promotion. Legacy entities
retain their existing runtime-map authority.

## P1d: Ordinary managed runtime integration

Use the existing `AssetServer` geometry lifetime for sealed owners and independent
authoritative snapshots. Opt-in creates an entity override; shared sources and
sibling entities remain isolated. Do not add a second residency service. Begin
with ordinary full-density entities. Reject streamed/imported, terrain, planet,
retained-renderer, LOD, voxel-backing and GPU-first geometry before mutating them.
Those paths require their existing storage, retirement and publication owners.

Internal engine reads preserve sealing; public mutable asset getters and resolvers
promote dense asset authority. Explicit runtime promotion instead binds the
attached renderer map as dense authority. Either path permanently disables
tracking. The bridge distinguishes these modes rather than treating every raw
pointer as another sealed source. Direct derivative mutation before promotion is
unsupported for opted-in entities; it cannot become collision or save authority.

Managed writes retain synchronous dense semantics and patch stable independent
renderer derivatives. An authoritative snapshot publishes after the batch,
including applied prefixes on producer panic. Collision and persistence resolve
sealed authority before renderer maps; their existing full-snapshot formats and
synchronous readiness remain. Saving clears publication dirtiness without
clearing construction-relative edit history. Built-in sphere edits use managed
writes. Raw editing helpers and destruction promote before their existing dense
operations, preserving prior managed edits.

Geometry references and source identity qualify each attachment. Replacement,
deletion and entity removal must release stale associations. Buffered enable/edit
calls resolve their pending override consistently without flushing ECS commands
early. This batch establishes authority and compatibility; incremental snapshot
publication, compact payloads and E2 deltas follow within the same owners.

Use separate tests and implementation agents and independent PRE/POST reviews.
Verify source/fork/snapshot isolation, normal halos, solid expansion, tombstones,
removal/reinsertion, tracked reverts, raw exposure and panic prefixes. Run focused
and race checks, then engine and consumer boundary checks. Native GPU validation covers managed upload, collision, instance isolation,
promotion and removal; pixel parity and performance remain unmeasured.


## P1e: Incremental authority publication

Publish clean immutable derivatives through
`ManagedXBrickMap.CopyChangedSectors(previous, sinceRevision)`. The previous map
must be an unchanged snapshot of this owner at that revision, or a matching
pre-divergence inherited snapshot. Divergent sibling history is not a valid input. Share only previous immutable sectors, never mutable
owner storage. Keep `Snapshot` independently mutable. Exposed owners always
copy fully because raw writes can bypass revisions.

Track private sector publication revisions for real assignments and fitted-normal
halo invalidation, including auxiliary-only changes across sector boundaries.
Keep public voxel and sector revisions unchanged. Fork clones this metadata;
promotion drops it. Preserve deletion tombstones, current bounds, auxiliary data,
material flags and applied-prefix finalization. Runtime authority replaces only
changed sectors in its prior snapshot; renderer replay and authored pivots remain.

This extends existing immutable derivative ownership rather than introducing a
cache or changing persistence formats. Payload copying follows changed sectors
and their halos; metadata still follows retained sectors/revision tombstones.
Latest snapshot size and downstream collision copies are not reduced by contract.
Verify exact geometry/auxiliary parity, isolation and sector reuse, then focused
race, engine/consumer checks and native runtime parity. Measure copy work without
claiming end-to-end frame or memory gains.


## E2 follow-up alignment

Status: proposed; requires human alignment before dependent implementation.
Start with ordinary authored placement/item objects using the existing S4 object
capture, byte admission and durable publication owner. Imported chunks, terrain
and voxel backing remain on their existing full/removal paths.

Recommended identity: deterministic canonical base geometry and authoritative
lattice/rasterization metadata, captured during base construction. Exclude
renderer auxiliary data and placement transforms. Paths and runtime `AssetId`
are insufficient identities. Compatible authored-base forks retain provenance;
resealed modified runtime bases and unbound full snapshots use full fallback,
without implicit delta chains or a new persistent base registry.

Recommended format boundary: a versioned voxel-object payload at the existing
`SnapshotPath`, with explicit full/base-delta selection. Preserve legacy v1 full
readers in the new loader. Bind deltas to stable placement/item ownership and
base identity; encode final assignments including explicit zero removals. Reject
mismatched bases before publishing geometry. Capture owned changes/identity on
the engine thread; workers encode through the existing S4 lifecycle. Publish
payload/reference only after successful durable replacement. Raw exposure keeps
full-snapshot fallback.

Alternative: begin with imported-world deltas and their backing/streaming owners,
or introduce a manifest-wide payload selector for every override kind. Both
expand ownership and compatibility scope. The recommended ordinary-object step
is smaller and reuses current S4 ownership; it still requires approval of this
base lifecycle and payload boundary before schema or runtime changes.
