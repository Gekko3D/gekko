# S4: Streamed Edit Persistence Ownership

## Approved direction and delivery

S4 follows the existing island contract: capture immutable edits on the main
thread, admit bounded worker work, publish override references after durable
success, and retain dirty ownership until publication. Workers never read live
ECS/maps or mutate runtime reference maps. Stop remains a durable barrier; failure
keeps the running generation and leases usable.

Deliver prerequisites as S4a, then bounded normal-unload transactions as S4b.
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
transactional runtime publication remains S4b.

Keep blocking private unload helpers and Stop behavior, including immediate
save/reload contracts. S4a does not remove normal-unload IO or add a queue budget.
Successful old payloads and unreferenced successful attempts can accumulate on
disk; garbage collection needs separate reference ownership and is not implicit.

Rejected alternative: replace fixed files atomically. It prevents partial files
but still changes geometry referenced by a previously committed manifest when
later manifest publication fails.

## S4b ownership requirements

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
Resolve concrete queue/configuration/publication rules before S4b implementation.

## Verification boundary

S4a protects complete snapshot independence, previous durable references after a
failed manifest save, retry and navigation payload isolation. Existing persistence
and Stop tests remain unchanged. Focused checks precede full engine tests and
consumer builds; race checks cover asynchronous capture. S4b adds held-worker,
backpressure, edit-during-save, stale completion, navigation overlap, failure,
Stop and native dirty leave/reload verification. No performance claim follows
from durability checks.
