# E2 follow-up: Per-brick replacement decision

Status: conditional proposal; no format or runtime implementation approved here.
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

Approve this schema-3 hybrid scope and logical-cost policy, or retain schema 2
and defer per-brick replacement. Implementation remains blocked on that choice
and meaningful measured benefit. The repository's
[human alignment gate](../../AGENTS.md#human-alignment-gate) requires alignment
before a non-trivial content-contract change with an unresolved design.
