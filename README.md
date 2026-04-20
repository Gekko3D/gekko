# gekko

Gekko is a Go game engine with an ECS core and a voxel real-time renderer (`voxelrt`).

Use [`docs/README.md`](docs/README.md) as the engine documentation index. Use [`AGENTS.md`](AGENTS.md) for workspace-level orientation, reliable commands, and contributor workflow notes.

## Repository Layout

- `base/`
  - agent routing and workflow manifests
- `docs/`
  - canonical engine documentation
- `voxelrt/`
  - renderer implementation
- `content/`
  - authored content formats, validation, and IO
- `assets/`
  - shared asset IDs, handles, and voxel asset types
- `ecs/`
  - ECS internals extracted from the root package
- `physics/`
  - physics types and world helpers

## High-Value Docs

- [`base/skills-manifest.md`](base/skills-manifest.md)
- [`AGENTS.md`](AGENTS.md)
- [`docs/engine/runtime.md`](docs/engine/runtime.md)
- [`docs/engine/modules.md`](docs/engine/modules.md)
- [`docs/engine/ecs.md`](docs/engine/ecs.md)
- [`docs/engine/physics.md`](docs/engine/physics.md)
- [`docs/engine/verification.md`](docs/engine/verification.md)
- [`docs/renderer/overview.md`](docs/renderer/overview.md)
- [`docs/renderer/runtime.md`](docs/renderer/runtime.md)
- [`docs/renderer/change-guide.md`](docs/renderer/change-guide.md)
- [`docs/assets/runtime-assets.md`](docs/assets/runtime-assets.md)
- [`docs/editor/integration.md`](docs/editor/integration.md)
- [`docs/content/game-assets.md`](docs/content/game-assets.md)
- [`docs/content/levels.md`](docs/content/levels.md)
- [`docs/content/streaming-and-worlds.md`](docs/content/streaming-and-worlds.md)
- [`docs/content/asset-format.md`](docs/content/asset-format.md)
- [`docs/workflows/agent-task-loop.md`](docs/workflows/agent-task-loop.md)
- [`docs/workflows/status-report.md`](docs/workflows/status-report.md)

## Bridge Entry Points

The ECS-to-renderer bridge lives in:

- `mod_voxelrt_client.go`
- `mod_voxelrt_client_systems.go`
- `mod_voxelrt_client_materials.go`
- `mod_voxelrt_client_skybox.go`
