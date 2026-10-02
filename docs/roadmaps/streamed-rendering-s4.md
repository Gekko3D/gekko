# S4: Streamed Edit Persistence Ownership

## Approved direction and delivery

S4 follows the existing island contract: capture immutable edits on the main
thread, admit bounded worker work, publish override references after durable
success, and retain dirty ownership until publication. Workers never read live
ECS/maps or mutate runtime reference maps. Stop remains a durable barrier; failure
keeps the running generation and leases usable.

Deliver immutable publication prerequisites as S4a, capture-order fencing as
S4b, then bounded normal-unload transactions as S4c.
These are lasting steps toward the existing architecture. Content schema and
voxel snapshot semantics remain unchanged. Compression/deltas remain later work.

## S4a: Immutable manifest capture and payload publication

Current asynchronous manifest capture copies only navigation slices. Clone all
eight `WorldDeltaDef` slices and nested backing-removal brick slices before
handoff. Struct arrays and immutable strings require no further mutable backing.
Preserve the existing single active manifest writer and coalesced pending save;
synchronous saves continue joining it before writing the latest state.

Current unload/navigation writers overwrite fixed terrain, imported and object
payload names. Even atomic manifest replacement cannot protect old references
when their payload is overwritten first. Every new runtime payload therefore
gets a unique reserved name in the existing world-data directory. Write through
the existing content serializer to a separate temporary file, sync and close,
rename onto that owned reservation, then sync the directory. Return the completed
path only on success. Failed temporary/reserved files belong to this operation
and can be cleaned up; existing referenced payloads are never removed or changed.
Keep final names inside the world-data directory, preserve payload extensions
and set the completed file's intended `0644` mode before sync.

Reuse one private writer for terrain, imported-world and object snapshots,
including navigation's imported edit-analysis path. Keep current payload-write
serialization. Existing readers already follow `SnapshotPath`, so unique names
need no schema or legacy-reader change. Public content save APIs stay unchanged.
The guarantee covers durable manifest references and their payloads. Existing
in-memory override maps can stage new references before a failed manifest save;
transactional runtime publication remains S4c.

Keep blocking private unload helpers and Stop behavior, including immediate
save/reload contracts. S4a does not remove normal-unload IO or add a queue budget.
Successful old payloads and unreferenced successful attempts can accumulate on
disk; garbage collection needs separate reference ownership and is not implicit.

Rejected alternative: replace fixed files atomically. It prevents partial files
but still changes geometry referenced by a previously committed manifest when
later manifest publication fails.

## S4b: Imported capture-order publication

Unique payload files prevent overwrite but do not order their references. A
navigation analysis captured before a blocking dirty unload can finish later and
replace that unload's newer override. Fence both producers through main-thread
capture ownership keyed by imported world ID and chunk coordinate. A new edit
capture supersedes older outstanding ownership. Synchronous imported persistence
and imported backing-removal publication also invalidate older captures, including
when a newer save fails. Older geometry must not become authoritative afterward.

Use distinct nonzero-size identity tokens for outstanding captures rather than a
permanent counter history. Pending/active analysis items carry their token;
current ownership retains conservative uncommitted edit provenance. Successors
inherit unknown impact, addition flags and combined valid bounds from pending or
active predecessors until analysis commits. Rejecting a predecessor must not
silently discard its navigation impact. Completion validates ownership before
publishing payload references or queuing graph edits. Mixed completions retain
current items. Merge ignored-removal state only
for those current item coordinates, preserving unrelated state. A graph-generation
retry retains the same capture ownership and cannot replace a newer pending edit.
Release terminal ownership only when it is still current. Start and successful
Stop reset ownership after the existing worker barrier. Workers only carry tokens;
they never access or mutate the ownership map.
Terminal failure and barrier discards release every dispatched capture, including
failed/unprocessed items; a different-world pending replacement releases displaced
ownership. Cleanup never invalidates a successor capture.

Blocking imported persistence queues its latest immutable saved snapshot for
analysis in the configured navigation source world. Otherwise a PreUpdate unload
could supersede the only analysis and remove the entity before PostUpdate captures
the latest edit. Imported backing persistence must similarly retain latest removal
analysis. Replacement analysis conservatively treats uncategorized edits as
unknown, preserving rebuild progress and blockers.
Queued saved snapshots own their voxel/tag slices, preserving caller aliases.
Unknown impact remains conservative when coalesced with later bounded edits.
Merge bounds only when both coalesced edits have valid bounds.

This is a lasting prerequisite for asynchronous unload. Existing graph scheduling,
failure behavior, synchronous save/reload and content formats remain unchanged.
It does not add byte admission or make in-memory manifest publication transactional.
No historical token ledger is retained for completed chunks. A mutex-only design
is insufficient: serialized file writes do not order main-thread result commits.

Owner: streamed persistence and navigation edit capture. Files:
`streamed_level_persistence.go`, `streamed_level_runtime.go`, and
`navigation_graph_runtime.go`. Confidence: High after producer/commit/Stop source
inspection and independent review; no SME alignment required within the existing
S4 ownership direction. Verify delayed real-worker results, mixed current/stale
items, newer queued edits, graph retries and backing removals through durable
payload/reference roundtrips and latest navigation rebuild progress. No rendering
change requires a native GPU check in this prerequisite.

## S4c: Bounded asynchronous normal unload

Extend the existing streamed runtime with one admitted transaction and one IO
worker phase at a time. Busy unload requests retain small main-thread dirty
intents keyed by loaded-chunk ownership; they capture no geometry. Remember dirty
entity/classes until a matching durable checkpoint, even if the observer returns
or the renderer consumes its upload dirty queues. Intent does not authorize
removal. Existing chunk/cache/renderer ownership remains the residency owner.
Normal admission and retries require current unload or residency-upgrade demand;
keep demand defers successor captures. An admitted checkpoint still completes.
Blocking helpers and Stop save the latest edits regardless of observer demand.

Add `StreamedLevelRuntimeConfig.MaxPendingPersistenceBytes`: zero selects 128 MiB,
negative is rejected at Start, positive selects the retained snapshot budget.
Preflight finite byte counts before cloning. Capture exact-size owned brick
coordinate/payload arrays, entity/content metadata and backing-removal values;
never carry live ECS, geometry maps, asset handles or GPU managers into workers.
Terrain capture preserves the serializer's observed AABB inputs and existing
column semantics. Imported/object captures preserve current sparse voxel semantics.
Reuse existing codecs/readers and the S4a unique durable writer.

Charge retained captures, result paths and the complete candidate manifest clone
through acknowledgement/discard. Ordinary asynchronous manifest captures use the
same exclusive admission/accounting owner; synchronous barriers retain their
blocking contract. With one admitted transaction, finite sole-owner
oversized capture or candidate growth can proceed as visible pressure; unsafe
arithmetic/allocation counts hold live dirty data without allocation. Worker
serializer tables, encoded buffers and IO temporaries are separate transient
allocations from one producer. Navigation caches/analysis and physical allocator
overhead are other owners; this is not a process-memory ceiling.
Any reconstructed map or serializer table retained in a phase result is charged
until release or explicit handoff to navigation; only worker-local temporaries
are excluded. Shared array backings transfer once without duplicate accounting.

Use two phases under exclusive manifest publication ownership:

1. Worker builds and durably publishes unique payloads from owned captures.
2. Main thread validates capture ordering and current entity/map/content ownership,
   then preflights and clones a fresh complete manifest with the durable patches.
   Worker atomically saves that candidate. Success acknowledges a durable
   checkpoint; payload/manifest failure keeps prior reference publication and
   live dirty intents for retry.

Normal transaction failures are reported in persistence metrics and retry through
observer processing; they do not set a global error that blocks their own retry.
Unrelated runtime/navigation errors retain existing behavior. Blocking barriers
return persistence errors to the caller without tearing down live ownership.

Existing ordinary manifest IO must be idle before admission, and starts remain
gated until this transaction acknowledges or fails. Coalesced ordinary requests
retain intent rather than an additional full clone and capture fresh state when
admitted. Dirty unload demand has priority after an existing manifest completes,
so repeated navigation saves cannot starve persistence. Navigation payload and
graph analysis keep their scheduling and immutable generation contracts. Imported
unload captures share S4b capture-order ownership with those analysis items.
Route all runtime manifest writes through one private IO dependency:
`worldDeltaWriter func(string, *content.WorldDeltaDef) error`, with nil selecting
`content.SaveWorldDelta`. Capture it before worker dispatch. Controlled IO can
hold publication while still using the real atomic serializer, verifying
exclusivity and acknowledgement without imposing frame/helper call order.

Durable checkpoint publication and removal are separate decisions. At manifest
acknowledgement, install each saved reference only where current RAM reference
and backing-removal fields still match the fields captured before manifest IO.
Preserve any newer published fields. Otherwise, acknowledge the saved checkpoint
as the baseline even if the observer returns or newer untracked live edits arrive.
Never rebuild the next manifest from pre-edit RAM references and revert a durable
checkpoint while its successor has no payload reference yet. Queue latest saved
navigation analysis only without superseding a newer capture.

Revalidate runtime/chunk/entity/map identity, relevant persisted metadata and
exact current payload/removal contents before removal; revision alone misses
public alias writes. New edits or ownership changes retain dirty intents and
recapture. Fresh keep/upgrade/proxy demand gates normal removal, never checkpoint
publication. Do not clear renderer/physics dirty queues or their persistence flag.
Process outstanding completions before existing runtime error early returns.

Blocking private helpers and Stop join the same publication owner, retain their
immediate durable save/reload contract and save the latest captured edits before
teardown. Private capture helpers also support the existing detached loaded-chunk
fixtures. Failed Stop preserves initialized state, generation, entities and
leases; successful Stop drains acknowledgements before releasing ownership.
Capture/results cannot be discarded by the existing generic job-drain barrier.

Expose `PendingPersistenceCount`, `PendingPersistenceBytes`,
`PendingPersistenceMaxBytes`, `PendingPersistenceOverBudgetBytes`,
`PendingPersistenceAdmissionRetries`, `PendingPersistenceOversizedAdmissions`,
`DirtyPinnedChunkCount`, `PersistenceFailureCount` and `PersistenceLastError` in
runtime metrics. Private ownership accounting remains authoritative.

Owners/files: streamed persistence capture/coordinator, `streamed_level_runtime.go`,
`streamed_level_persistence.go`, `navigation_graph_runtime.go`, and shared terrain
snapshot conversion in `world_delta_voxel_snapshot.go`. New private files may
separate capture/accounting from scheduling. Confidence: High after source and
independent architecture review. No SME alignment required within S4. A queued
second captured snapshot adds retention and progress priority without a measured
need; keep admission fixed at one. Single-phase manifest IO before validation
can publish stale candidates; select the two-phase protocol above.

## Verification boundary

S4a protects complete snapshot independence, previous durable references after a
failed manifest save, retry and navigation payload isolation. Existing persistence
and Stop tests remain unchanged. Focused checks precede full engine tests and
consumer builds; race checks cover asynchronous capture. S4c adds held-worker,
backpressure, edit-during-save, stale completion, navigation overlap, failure,
Stop and native dirty leave/reload verification. No performance claim follows
from durability checks.
