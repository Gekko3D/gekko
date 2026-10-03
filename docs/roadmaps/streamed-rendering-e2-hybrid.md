# E2 follow-up: Per-brick replacement decision

Status: deferred after synthetic codec measurements; no format or runtime
implementation approved here.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md#e2-persist-changed-bricks-instead-of-whole-geometry).

## Current boundary

E2b6 completes the implemented ordinary authored-shape path: verified canonical
bases, bounded sparse S4 publication, reload restoration and exact merged
brick/voxel counts. Assignment-count and decoded-size admission remain
conservative. Terrain, imported worlds and other source adapters retain their
existing representations.

Schema 2 already selects uniform/mixed C1 channels. Its `full` mode replaces an
entire object and has no base identity; it is not a per-brick replacement mode.
Expanding a sparse assignment mask to all 512 cells cannot reduce that channel's
logical size. The remaining roadmap optimization needs different semantics.

## Alternatives and recommendation

Keep schema 2 when measured gains are small. Whole-object full selection is a
separate possible optimization, but reload remains unbound and capture needs
complete current geometry. A true hybrid can avoid those costs for selected
changed bricks while retaining the original canonical base.

Recommend measuring representative paint/carve histories first. If the benefit
justifies another reader, add schema 3 `hybrid_delta` with deterministic logical
byte-cost selection. Preserve schema-1 and schema-2 readers and their meanings.
Actual compressed-frame selection is an alternative, but requires encoding
multiple candidate frames and charging their capture inputs and temporaries.
Logical selection makes no guarantee about the smallest compressed result.
Compare total logical document-body bytes, including canonical selector metadata
and any fixed schema/mode overhead. Require strict savings; ties keep assignments.
Keep schema 2 when no selected replacement offsets the added metadata cost.

Current C1 per-brick body costs, excluding document metadata and frame overhead:

| History | Assigned cells | Delta bytes | Full geometry bytes |
| --- | ---: | ---: | ---: |
| Uniform paint of a full brick | 512 | 82 | 81 |
| Mixed paint of a full brick | 512 | 593 | 592 |
| Remove 500 cells, retain 12 mixed cells | 500 | 82 | 92 |
| Remove 500 cells and repaint 12 mixed cells | 512 | 593 | 92 |

These are logical channel calculations, not measured compressed savings. A
replacement selector adds metadata cost. Small paint-only differences do not
justify replacement by themselves. Measure encoded size and encode/decode work
with the default codec and any affected dictionary profiles before delivery.

## Measurements: 2026-10-04

Recommendation: retain schema 2. Logical-body savings do not reliably predict
compressed savings. Codec CPU/allocation gains exist, but real save/reload
profiles must establish their value before adding a permanent format.

A temporary Go harness compared actual schema-2 payloads with simulated hybrid
C1 documents, including canonical selector metadata. It checked codec roundtrip
and independently merged geometry for every variant. No production schema-3
reader, writer or runtime path was implemented.

Environment: Go 1.25.4, darwin/arm64, GOMAXPROCS=1, default dictionary-free codec.
Three sequential runs used warmed codecs and 100 ms encode/decode benchmarks;
times below are medians. Frame bytes matched exactly across runs. No configured
production object dictionary was found.

The synthetic corpus used 1 and 32 touched bricks, including negative seams:
eight-cell paint, uniform/mixed paint, 500-cell carve, carve/repaint, whole clear,
one-cell-thick shell carve and empty reverted history. Mixed values used a fixed
xorshift seed `0x12345678`; actual assignment counts never exceeded 16,384. No
substantial checked-in ordinary authored-shape history corpus was available.

Compressed frame bytes for 32 touched bricks:

| History | Schema 2 | Logical selector | Forced replacement |
| --- | ---: | ---: | ---: |
| Sparse paint | 452 | 452 | 574 |
| Uniform paint | 449 | 449 | 548 |
| Mixed paint | 17,244 | 17,244 | 17,057 |
| Carve 500 cells per brick | 445 | 445 | 1,013 |
| Carve/repaint | 946 | 1,016 | 1,016 |
| Clear | 443 | 416 | 416 |
| Shell carve | 456 | 456 | 562 |
| Reverted history | 326 | 326 | 326 |

Carve/repaint had 16,382 assignments. Logical bytes fell from 19,274 to 3,573
(81.5%), but compressed bytes grew 7.4%. Median codec encode/decode times fell
from 47.4/27.0 µs to 15.2/5.8 µs. Allocated bytes per encode/decode fell from
47,744/59,274 to 14,976/10,984. Clear saved only 27 compressed bytes (6.1%);
encode/decode fell from 34.2/21.0 µs to 5.2/3.7 µs. Forced mixed paint saved 1.1%
compressed bytes although the logical selector retained assignments. At one
brick, selected carve/repaint and clear both produced larger compressed frames.

Timings exclude capture, selector planning, content adapter conversion, base
verification, semantic resolution, durable writes and runtime reload. They are
codec-only measurements, not frame-time or end-to-end savings. Synthetic results
do not establish production frequency or aggregate benefit.

Temporary artifacts: `/tmp/gekko-e2-measure.go`, three JSON runs and
`/tmp/gekko-e2-measure-median.json`; they are not shipped or durable test fixtures.
Command: `env GOCACHE=/tmp/gekko3d-gocache go run /tmp/gekko-e2-measure.go`.

## Proposed format semantics

Use the existing C1 frame version and document kind, with schema-3 metadata and
`hybrid_delta` mode. Bind explicit placement/item IDs, canonical base identity and
lattice exactly as current deltas do. Schema 3 would initially support this mode
only; schema-2 full/base-delta meaning remains unchanged.

Metadata contains a unique, canonically sorted list of replacement brick
coordinates in the brick grid. Each coordinate's entire 8×8×8 voxel range must
fit signed int32, including selectors with no physical C1 record. A listed brick
replaces all 512 base cells: occupied primary values
give its complete nonzero geometry, with no secondary layer. An absent C1 record
for a listed coordinate explicitly clears that whole brick. This exception is
typed in metadata; an absent unlisted record never removes a base brick.

Unlisted C1 records retain schema-2 assignment semantics: primary presence 1,
mandatory secondary final values including zero. Reject duplicate selectors,
ambiguous layers, invalid coordinates, unsupported metadata and profile overflow.
Replacement and assignment representations cannot overlap at one coordinate.
Verify canonical base identity before applying either representation.

Count empty replacement selectors toward the logical brick limit, even though
C1 has no zero-occupancy record. Bound selector metadata and merged geometry as
well as physical codec records. Resolve into owned geometry before existing
render/collision registration. Loaded managed restoration remains relative to
the original base, with no delta chain or additional resident base registry.

## Ownership and delivery constraints

Keep `ManagedXBrickMap` and existing streaming leases as owners. Track changed
brick membership continuously. Preflight must charge owned selectors and chosen
records before capture; it cannot walk untouched geometry or choose using an
uncharged dense snapshot. Capture only selected changed-brick payloads on the
engine thread. Workers receive owned data through existing S4 durability,
freshness, cancellation and acknowledgement rules.

Likely files: `content/voxel_object_payload.go`, its model/IO adapters,
`voxelrt/rt/volume/managed_xbrickmap.go`, object persistence/restore helpers and
owning canonical docs. Do not change generic C1 brick meaning, render tickets,
physics publication or other override kinds.

Deliver content compatibility before runtime selection. Use separate test and
implementation agents with independent PRE/POST reviews. Minimum coverage:
mixed selected/unselected bricks, explicit empty replacement, negative seams,
base mismatch, legacy readers, profile/byte admission, paint/carve/revert,
save/evict/reload/edit, stale captures and failed durable publication. Run focused
and race checks, then engine and affected consumer checks at each code boundary.
No GPU layout change is proposed; any runtime derivative change needs its own
visual verification scope.

## Required alignment

Retain schema 2 under the measured recommendation above. Revisit schema-3 scope
and its cost policy only when real capture/save/reload profiles justify it.
Implementation remains conditional on that alignment and meaningful benefit.
The repository's
[human alignment gate](../../AGENTS.md#human-alignment-gate) requires alignment
before a non-trivial content-contract change with an unresolved design.
