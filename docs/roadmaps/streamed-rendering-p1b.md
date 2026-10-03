# P1b: Compact private prepared sources

Status: complete for this private opt-in boundary.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), P1.
Prerequisites: P1a point reads and existing S2/P5 registration ownership.

## Boundary and choice

Add supported opt-in `StreamedLevelRuntimeConfig.CompactPreparedGeometry`.
False retains the current dense preparation path and its observable storage
attribution. True qualifies fresh immutable imported full/proxy worker sources
for compact retention in the existing prepared cache. Public assets, live
renderer maps, physics, navigation, terrain, snapshots and backing edits remain
dense. This is a long-term private representation step, not a new cache or a
disk format. Broader public mutation and codec decisions remain open.

Default compaction would change established dense prepared-result/accounting
contracts. Retaining both authorities defeats its memory purpose. Opt-in
qualification preserves the default and makes worker CPU versus retained memory
an explicit tradeoff. Changing `Brick.Payload` or restricting public raw writes
requires separate alignment; neither is part of this batch.

Existing P5a/S2k ownership supports this boundary. Reconstruction adds worker
CPU work; total process memory remains outside the existing policy charge.

## Source and authority

An existing cache entry owns either a dense source or a private immutable compact
source handle. Keep one key space, singleflight, LRU, asset mapping, physical
ledger and registering-server cleanup. Generic dense callers retain exact map
identity, shared-interior accounting and existing nil/disabled/oversized behavior.

Compact sources preserve authoritative cells, material metadata, sector masks,
coordinates, revisions/tombstones, cached bounds and exact auxiliary bytes.
Use occupancy with uniform or occupied material values; dense fallback is allowed
when compaction does not reduce the policy charge. No live GPU owner is retained.
The logical source is private and sealed before publication. Its storage layout
is not a serialized ABI or the eventual C1 codec schema.

Compact-mode results hold their source handle rather than retaining a redundant
dense prepared source. Workers materialize independent dense registration copies
with the existing Copy/bounds/clean-state contract. Prepared registration matches
the exact source handle. Registered asset reuse and single-use adoption remain.

Generic exposure of a compact entry promotes it once to retained dense authority.
Later reads/builds use that current dense source; no compact shadow tracks public
raw writes. Pending packets may retain older immutable snapshots, but cannot
adopt an older candidate over a promoted current source. Use the current dense
fallback on that mismatch. Preserve same-key coalescing across dense and compact
callers; materialization/build waits run outside the cache mutex.

Dense source handles retain atomic exposure metadata across eviction/re-admission.
Source-qualified adoption rejects exposed handles, including candidates captured
after promotion, so later raw edits use current defensive copying. Fresh qualified
dense fallback remains adoptable before exposure. Legacy dense adoption retains
its existing immutable-source convention. Generic asset-only acquisition copies
the winning compact source without promoting it. Promotion changes authority only
for the exact retained entry; uncached compact captures remain sealed independent
snapshots and never repopulate a closed cache.

Initially qualify imported full/override preparation only without captured
backing. Proxy preparation is eligible. Backing can change before commit:
materialize a current dense fallback when needed and retain the existing
registration/spawn/removal behavior. Do not change backing authority or gameplay
publication. Terrain and object snapshot packets keep their current path.

## Accounting and lifetime

Extend the existing physical ledger for actual compact allocations. Charge every
simultaneously retained compact source, dense registration, descriptor and pending
packet under the existing owners. Cache references, asset references and pins
retain their meanings. Dense promotion transfers prepared/pinned references and
recomputes warm-retention policy without losing acquired assets.

Eviction or promotion releases only the cache's source reference. Pending packets
retain and charge their captured source until consumption/cancellation. Close
cannot be repopulated by an outstanding build/promotion. Keep existing oversized
and deferred engine-thread eviction exceptions; add no new budget exception.
Temporary build/promotion work retains the documented limits of S2, rather than
claiming a heap/RSS ceiling.

## Implementation and verification

Affected systems: runtime config/jobs/results, prepared cache/storage ledger,
registration and asset adoption, pending charges, full/proxy commits and current
backing fallback. Keep extraction, shaders and persisted formats unchanged.

Use separate test and implementation agents and independent PRE/POST reviews.
Freeze new coverage before implementation; preserve all existing tests.
Minimum coverage: value/material/aux/bounds/revision parity and source isolation;
actual retained-byte reduction; full/proxy adoption and warm asset reuse;
dense promotion/current-authority fallback; barrier-controlled same-key mixed
callers and terminal cancellation/eviction/close cleanup. Exercise active pins
and pending charges, including sources no longer retained by the cache.

Run focused ownership suites and race checks, then full engine tests and affected
consumer builds once. Native imported full/proxy readiness, seam/edit isolation,
collision and terminal cleanup checks cover GPU integration; no pixel/FPS claim.
Measure retained policy bytes and cold/warm worker cost on disposable samples.

Execution and measured limits are recorded once in the
[parent roadmap](streamed-rendering-content-optimization.md#p1b-compact-private-prepared-sources).
