# S3e: Component publication ownership

Date: 2026-10-02. Status: implemented in `782ca99`.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S3.

## Decision

Keep aggregate component publication sequences in shared `ecsStorage`. Copied
wrappers share the owner; independent storages remain isolated. Retain one scalar
per published type after final removal so removals and re-admission stay visible.
Use explicit main-thread marks for committed direct writes.

The public contract belongs in [ECS docs](../engine/ecs.md#explicit-component-publication-revisions).
Flush visibility belongs in [runtime docs](../engine/runtime.md#command-buffering).

## Alternatives and rationale

- Mandatory setters require migration of public-field writers across engine,
  gameplay and editor before they can establish complete mutation ownership.
- Per-entity history or a journal requires retention, overflow and consumer-cursor
  contracts. Aggregate sequences establish the shared owner without entity tombstones.
- Asset stamps alone cannot establish immutability: asset getters expose aliased
  mutable maps/slices. Mutable asset ownership needs a separate design.

## Consequences

Aggregate revisions supply no entity worklist. Hierarchy output publication is
implemented in S3f, but other producers remain incompletely migrated. Consumers
must retain live extraction until every relevant mutable input has an owner that
publishes changes. This API establishes no performance gain.

## Execution record

Sol 6.1 TDD and both adversarial reviews completed. Verification and limitations
are recorded in the [roadmap](streamed-rendering-content-optimization.md#s3e-publication-api).
