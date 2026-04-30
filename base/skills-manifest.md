# Agent Skills Manifest

This file is the canonical routing table for agent-driven work in `gekko`.

Use it together with:

- [`../AGENTS.md`](../AGENTS.md)
- [`../docs/workflows/agent-task-loop.md`](../docs/workflows/agent-task-loop.md)
- [`../docs/workflows/status-report.md`](../docs/workflows/status-report.md)

## Goal

Normalize a request quickly, choose the smallest useful workflow, and avoid broad edits before the owning subsystem and verification path are clear.

## 1. Normalize The Request

Extract these fields first:

- objective:
  - what outcome the user wants
- target repo/files:
  - which module and likely owner bucket
- constraints:
  - safety, scope, time, performance, compatibility, rendering, or content constraints
- expected artifacts:
  - code change, doc update, status report, verification notes, or review questions

## 2. Owner Buckets

Choose one primary owner first. List secondary consumers separately.

- engine runtime:
  - app stages, modules, ECS behavior, command buffering, resource wiring, physics integration
- renderer:
  - `mod_voxelrt_client*.go`, `voxelrt/rt/...`, render passes, GPU resources
- authored content:
  - `content/...`, asset schemas, level formats, terrain, imported worlds, world deltas
- editor boundary:
  - engine/editor schema assumptions, preview integration, `gekko-editor/src/formats/...`
- docs and workflow:
  - `AGENTS.md`, `README.md`, `docs/...`, `reports/...`, verification guidance

## 3. Routing Rules

### Deterministic rules

- If the request is docs-only or workflow-only:
  - start with `AGENTS.md`, `docs/README.md`, and the matching workflow doc
- If the request changes runtime behavior:
  - read [`../docs/engine/runtime.md`](../docs/engine/runtime.md) and [`../docs/engine/modules.md`](../docs/engine/modules.md) first
- If the request changes ECS behavior:
  - also read [`../docs/engine/ecs.md`](../docs/engine/ecs.md)
- If the request changes physics behavior:
  - also read [`../docs/engine/physics.md`](../docs/engine/physics.md)
- If the request changes renderer paths:
  - read [`../docs/renderer/overview.md`](../docs/renderer/overview.md), [`../docs/renderer/runtime.md`](../docs/renderer/runtime.md), and [`../docs/renderer/change-guide.md`](../docs/renderer/change-guide.md) before low-level edits
- If the request changes content schemas or authored runtime:
  - read the matching content docs under [`../docs/content/`](../docs/content)
- If the request changes engine/editor contracts:
  - read [`../docs/editor/integration.md`](../docs/editor/integration.md) and verify `gekko-editor` after the engine change

### Confidence rules

- Confidence `>= 0.70`:
  - proceed after naming the invariant and verification path
- Confidence `< 0.70`:
  - do not start broad implementation
  - create a status report and ask for alignment or narrow the change first

Use this practical mapping:

- `High`:
  - one owner bucket, clear invariant, clear smallest verification command
- `Medium`:
  - owner bucket is known but interfaces, invariants, or consumer impact still need clarification
- `Low`:
  - ownership, success criteria, or rollback path are unclear

## 4. Cost And Safety Gates

- Prefer at most one broad verification sweep per task unless the change truly crosses subsystems.
- Do not default to `go test ./...` as the first move.
- Prefer the smallest package-level or root-package check that matches the change.
- Do not make destructive or compatibility-breaking changes without explicit user direction.
- Treat schema changes, module install-order changes, and shared renderer contracts as high-scrutiny work.

## 5. Required Artifacts By Task Shape

- Small isolated code fix:
  - code change plus verification notes
- Broad or ambiguous code change:
  - status report in `reports/` before or during implementation
- Docs or workflow policy change:
  - update the canonical doc and any index page that should point to it
- Handoff-prone work:
  - status report plus explicit open questions

## 6. Handoff Contract

A complete handoff states:

- objective and scope
- primary owner bucket and affected consumers
- invariant preserved or intentionally changed
- verification run
- risks, gaps, or follow-up checks

If that information does not fit in the final response cleanly, put it in `reports/<YYYY-MM-DD>-<topic>-status.md`.
