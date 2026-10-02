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

## S4c ownership requirements

The next design must serialize navigation and unload manifest publication,
preflight snapshot admission before cloning, and retain dirty chunks when the
queue is full or unsafe. Completed saves must validate runtime/chunk/entity/map
ownership and live edit content before removal; revisions alone cannot detect
untracked public payload writes. Newer edits remain dirty and pinned. A failed
payload/manifest keeps previous references and supports retry.

Normal observer unload requests persistence and completes later. Blocking private
helpers and Stop can join/drain the same owner. Successful Stop publishes latest
edits before teardown; failed Stop preserves live ownership. Bound snapshots,
pending results and worker count without adding a parallel residency service.
Resolve concrete queue/configuration/publication rules before S4c implementation.

## Verification boundary

S4a protects complete snapshot independence, previous durable references after a
failed manifest save, retry and navigation payload isolation. Existing persistence
and Stop tests remain unchanged. Focused checks precede full engine tests and
consumer builds; race checks cover asynchronous capture. S4c adds held-worker,
backpressure, edit-during-save, stale completion, navigation overlap, failure,
Stop and native dirty leave/reload verification. No performance claim follows
from durability checks.
