# AI Agent Context — Gekko Engine

> Read this file first before performing work in this repo.

## Project Identity

- **Name:** `gekko`
- **Purpose:** Go game-engine module with an ECS runtime, authored-content pipeline, physics integration, and a voxel real-time renderer bridge
- **Tech Stack:** Go 1.24 module, `voxelrt` renderer internals, GLFW/WebGPU desktop rendering, JSON-authored content formats
- **Repo Type:** Single engine module inside a multi-module `go.work` workspace
- **Status:** Active development

## Architecture Summary

`gekko` is the shared engine module in the wider `gekko3d` workspace. The root package owns the `App` runtime, stage scheduling, reflective resource lookup, command buffering, and most engine-facing modules. Subpackages such as `content/`, `ecs/`, `physics/`, and `voxelrt/` isolate specific engine concerns, but many high-value integration paths still live in the root package. Consumer modules such as `gekko-editor`, `actiongame`, `spacegame_go`, and several workspace examples import this repo and exercise its runtime contracts.

The renderer is deeply integrated but still enters the engine through ECS-facing bridge modules such as `mod_voxelrt_client.go` and `mod_voxelrt_client_systems.go`. Authored content is loaded and normalized through `content/` plus root-package spawn helpers. Physics is staged through the `PhysicsModule`, `PhysicsPullSystem`, and `PhysicsPushSystem` or synchronous fixed-step paths.

## Key Entry Points

| What | Location |
|---|---|
| App construction and stage setup | `app_builder.go` |
| Main runtime loop and dependency resolution | `app.go` |
| Buffered mutations and resource registration | `commands.go` |
| Module contract | `mod.go` |
| Renderer bridge | `mod_voxelrt_client.go` |
| Renderer systems | `mod_voxelrt_client_systems.go` |
| Physics module bootstrap | `mod_physics_module.go` |
| Content IO and schema normalization | `content/io.go` |
| Canonical docs index | `docs/README.md` |
| Repo workflow contract | `AGENTS.md` |

## Critical Rules (Quick Reference)

1. Run Git and Go commands from the module directory, not from the workspace root.
2. Prefer `env GOCACHE=/tmp/gekko3d-gocache ...` for local verification in this environment.
3. Do not default to `go test ./...` as the first verification step.
4. ECS mutations through `Commands` are buffered; same-stage readers do not see them until flush.
5. Renderer bridge, content schemas, and editor-facing contracts require extra scrutiny because consumer modules depend on them.

## Current State

- **Active Features:** check `.ai/features/`
- **Known Issues:** see `.ai/known-issues.md`
- **Recent Decisions:** canonical decisions live in `docs/engine/*`, `docs/renderer/*`, and `docs/workflows/*`; `.ai/adr/` is available for new repo-local decisions

## Deep References

- Architecture summary: `.ai/docs/ARCHITECTURE.md`
- Dependencies and consumers: `.ai/docs/DEPENDENCIES.md`
- Delivery and local verification model: `.ai/docs/DEPLOYMENT.md`
- Repo overview and stakeholders: `.ai/docs/PROJECT.md`
- Coding and workflow conventions: `.ai/conventions.md`

