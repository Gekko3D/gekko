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

## Execution record

Sol 6.1 TDD and both adversarial reviews complete. Verification/limits: [roadmap](streamed-rendering-content-optimization.md#s3e-publication-api).