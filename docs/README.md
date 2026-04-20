# Gekko Documentation

This directory is the canonical home for engine documentation.

Use the root [`README.md`](../README.md) for a quick module overview. Use [`AGENTS.md`](../AGENTS.md) for workspace and verification conventions. Everything else that should be maintained as engine documentation lives here.

## Start Here

- New to the engine:
  - [`engine/runtime.md`](engine/runtime.md)
  - [`engine/modules.md`](engine/modules.md)
  - [`engine/ecs.md`](engine/ecs.md)
  - [`engine/physics.md`](engine/physics.md)
  - [`renderer/overview.md`](renderer/overview.md)
  - [`renderer/runtime.md`](renderer/runtime.md)
- Verifying or debugging engine behavior:
  - [`engine/verification.md`](engine/verification.md)
  - [`renderer/verification.md`](renderer/verification.md)
- Working on renderer changes:
  - [`renderer/change-guide.md`](renderer/change-guide.md)
  - [`renderer/media.md`](renderer/media.md)
  - [`renderer/verification.md`](renderer/verification.md)
- Working on authored content:
  - [`content/game-assets.md`](content/game-assets.md)
  - [`content/levels.md`](content/levels.md)
  - [`content/streaming-and-worlds.md`](content/streaming-and-worlds.md)
  - [`content/asset-format.md`](content/asset-format.md)
- Working on runtime asset plumbing:
  - [`assets/runtime-assets.md`](assets/runtime-assets.md)
- Working across the engine/editor boundary:
  - [`editor/integration.md`](editor/integration.md)
  - [`editor/user-guide.md`](editor/user-guide.md)
  - [`editor/workflows.md`](editor/workflows.md)
- Common agent workflows:
  - [`workflows/agent-task-loop.md`](workflows/agent-task-loop.md)
  - [`workflows/status-report.md`](workflows/status-report.md)
  - [`workflows/add-module.md`](workflows/add-module.md)
  - [`workflows/add-content-type.md`](workflows/add-content-type.md)
- Debugging GPU buffer layout issues:
  - [`reference/gpu-alignment.md`](reference/gpu-alignment.md)
- Planning renderer quality or performance work:
  - [`roadmaps/renderer-lighting.md`](roadmaps/renderer-lighting.md)

## Sections

- `engine/`
  - core runtime, ECS, physics, module ownership, and verification guidance
- `assets/`
  - runtime asset ownership and creation paths
- `editor/`
  - engine/editor integration notes plus operator-facing editor guides
- `workflows/`
  - task-oriented playbooks for common agent changes, handoffs, and status artifacts
- `renderer/`
  - renderer architecture, frame flow, analytic media, editing/picking behavior, particles, and contributor guidance
- `content/`
  - authored content formats, levels, streaming, and world data
- `reference/`
  - focused technical notes that are useful across subsystems
- `roadmaps/`
  - forward-looking plans that are still worth maintaining
