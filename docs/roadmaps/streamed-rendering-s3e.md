# S3e: Component publication ownership

Date: 2026-10-02. Status: implemented in `782ca99`.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S3.

## Decision

Keep aggregate component publication sequences in shared `ecsStorage`. Copied wrappers share owner; independent storages stay isolated. Retain one scalar per published type after final removal; removals/re-admission remain visible. Mark committed direct writes explicitly on main thread.

Public contract: [ECS docs](../engine/ecs.md#explicit-component-publication-revisions). Flush visibility: [runtime docs](../engine/runtime.md#command-buffering).

## Alternatives and rationale

- Mandatory setters require migration of public-field writers across engine,
  gameplay and editor before they can establish complete mutation ownership.
- Per-entity history or journal requires retention, overflow and consumer-cursor
  contracts. Aggregate sequences establish shared owner without entity tombstones.
- Asset stamps alone cannot establish immutability: asset getters expose aliased
  mutable maps/slices. Mutable asset ownership needs separate design.

## Consequences

Aggregate revisions supply no entity worklist. S3f publishes hierarchy outputs; other producers remain incompletely migrated. Retain live extraction until every relevant mutable input has publishing owner. API establishes no performance gain.

## S3q: Bounded publication journal decision

Extend shared `ecsStorage` with a lazy ring of at most 1,024 entity/type
publications globally, alongside the existing per-type scalar sequences.
Consumers hold independent opaque owner-bound cursors. A cursor retains only a
separate nonzero-sized identity token, not the ECS storage or component data.
Copied wrappers share the journal; independent storage owners remain isolated.
No new worker, lock, component pointer, value snapshot or entity tombstone is
retained. Returned batches own their copies.

Expired, foreign, zero/unbound or invalid cursors return an explicit resync result
with no partial history and a read-time watermark. Consumers acknowledge that
watermark only after successful processing or a full committed rescan; publications
during the rescan remain available on the next read. Sequence wrap starts a new
owner-token era and clears prior history. The exact retention boundary includes
all 1,024 records; one further publication requires resync.

Retain `StructuralRevision` checks for membership and row locations: absent-type
removal and zero-component entity changes can publish no component type. Journal
records are invalidations, not replayable transactions or proof of complete
producer notifications. Live extraction remains required.

This extends aggregate stamps without unbounded per-entity/per-consumer history.
Mandatory setters still require consumer migration; silently truncated history
would lose removals or edits. The cap bounds only journal-owned records, not
returned batches, cursor tokens, total ECS memory or frame time. Public API and
recovery rules belong in the [ECS contract](../engine/ecs.md); execution belongs
in the parent roadmap.

## Execution record

Sol 6.1 TDD and both adversarial reviews complete. Verification/limits: [roadmap](streamed-rendering-content-optimization.md#s3e-publication-api).
