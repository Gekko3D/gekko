# Agent Guide

This workspace is a Go game-engine umbrella, not a single top-level Git repo. The root `go.work` file ties together six modules:

- `gekko/`: core engine, ECS, content schema, voxel renderer bridge
- `gekko-editor/`: standalone editor app built on `gekko`
- `actiongame/`: playable sample game
- `spacegame_go/`: larger demo/game prototype
- `examples/testing/`: low-level rendering sample
- `examples/testing-vox/`: voxel scene sample

## First Things To Know

- Run Git commands inside a module directory such as `gekko/` or `gekko-editor/`. The workspace root has no `.git` directory.
- Run Go commands from the module you are working on. Do not assume `go test ./...` from the workspace root will be the right move.
- In this sandbox, plain `go test` may fail when Go tries to trim the default build cache. Use `env GOCACHE=/tmp/gekko3d-gocache ...` for reliable local verification.
- Several demos resolve assets relative to their own module directory. If you run them from the wrong cwd, asset loading can fail even though the files exist.

## Reliable Commands

Prefer the smallest command that matches the change:

- Engine root-package check: `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test .`
- Content/schema changes: `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- ECS compile check: `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./ecs/...`
- Physics compile check: `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./physics/...`
- Engine-wide sweep for broad refactors only: `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- Editor tests when engine/editor boundary changed: `cd /Users/ddevidch/code/go/gekko3d/gekko-editor && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- Action game compile check when gameplay integration changed: `cd /Users/ddevidch/code/go/gekko3d/actiongame && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- Space game compile check when shared runtime changed: `cd /Users/ddevidch/code/go/gekko3d/spacegame_go && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- Voxel example compile check when voxel runtime changed: `cd /Users/ddevidch/code/go/gekko3d/examples/testing-vox && env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Use [gekko/docs/engine/verification.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/engine/verification.md) for the decision tree behind these commands.

Known failing sample:

- `examples/testing` does not currently compile. `env GOCACHE=/tmp/gekko3d-gocache go test ./...` fails in [examples/testing/main.go](/Users/ddevidch/code/go/gekko3d/examples/testing/main.go) because `TextureFormatR8Uint` is referenced but not exported by the engine surface anymore.

Useful run commands when you need a windowed app:

- Editor: `cd /Users/ddevidch/code/go/gekko3d/gekko-editor && env GOCACHE=/tmp/gekko3d-gocache go run .`
- Action game: `cd /Users/ddevidch/code/go/gekko3d/actiongame && env GOCACHE=/tmp/gekko3d-gocache go run .`
- Space game: `cd /Users/ddevidch/code/go/gekko3d/spacegame_go && env GOCACHE=/tmp/gekko3d-gocache go run .`
- Voxel demo: `cd /Users/ddevidch/code/go/gekko3d/examples/testing-vox && env GOCACHE=/tmp/gekko3d-gocache go run .`

These apps use GLFW/WebGPU, so expect them to need a real desktop session.

## Where To Start Reading

If you are changing engine flow or debugging system behavior, start here:

- [gekko/app_builder.go](/Users/ddevidch/code/go/gekko3d/gekko/app_builder.go): app construction, default stage registration, module installation
- [gekko/app.go](/Users/ddevidch/code/go/gekko3d/gekko/app.go): run loop, state transitions, reflective system invocation, command flushing
- [gekko/commands.go](/Users/ddevidch/code/go/gekko3d/gekko/commands.go): buffered mutations and resource registration
- [gekko/mod.go](/Users/ddevidch/code/go/gekko3d/gekko/mod.go): module contract
- [gekko/ecs.go](/Users/ddevidch/code/go/gekko3d/gekko/ecs.go) and [gekko/ecs](/Users/ddevidch/code/go/gekko3d/gekko/ecs): archetype ECS internals
- [gekko/docs/engine/runtime.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/engine/runtime.md): runtime model, stage order, buffering, and dependency resolution
- [gekko/docs/engine/modules.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/engine/modules.md): module ownership map
- [gekko/docs/engine/ecs.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/engine/ecs.md): archetypes, queries, and ECS mutation rules
- [gekko/docs/engine/physics.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/engine/physics.md): async physics integration model

If you are changing rendering or renderer integration, start here:

- [gekko/mod_voxelrt_client.go](/Users/ddevidch/code/go/gekko3d/gekko/mod_voxelrt_client.go)
- [gekko/mod_voxelrt_client_systems.go](/Users/ddevidch/code/go/gekko3d/gekko/mod_voxelrt_client_systems.go)
- [gekko/mod_voxelrt_client_materials.go](/Users/ddevidch/code/go/gekko3d/gekko/mod_voxelrt_client_materials.go)
- [gekko/mod_voxelrt_client_skybox.go](/Users/ddevidch/code/go/gekko3d/gekko/mod_voxelrt_client_skybox.go)
- [gekko/docs/renderer/overview.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/renderer/overview.md)
- [gekko/docs/renderer/runtime.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/renderer/runtime.md)

If you are changing authored content or editor/runtime data flow, start here:

- [gekko/content](/Users/ddevidch/code/go/gekko3d/gekko/content): shared asset, level, terrain, imported-world, and validation code
- [gekko/docs/content/game-assets.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/content/game-assets.md)
- [gekko/docs/content/levels.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/content/levels.md)
- [gekko/docs/content/streaming-and-worlds.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/content/streaming-and-worlds.md)
- [gekko/docs/content/asset-format.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/content/asset-format.md)
- [gekko/content/io.go](/Users/ddevidch/code/go/gekko3d/gekko/content/io.go)
- [gekko-editor/src/formats](/Users/ddevidch/code/go/gekko3d/gekko-editor/src/formats)
- [gekko-editor/implementation-plan.md](/Users/ddevidch/code/go/gekko3d/gekko-editor/implementation-plan.md)

If you are changing the editor UI or workflows, start here:

- [gekko-editor/main.go](/Users/ddevidch/code/go/gekko3d/gekko-editor/main.go): app bootstrap and top-level modules
- [gekko-editor/src/modules/asset_editor](/Users/ddevidch/code/go/gekko3d/gekko-editor/src/modules/asset_editor)
- [gekko-editor/src/modules/level_editor](/Users/ddevidch/code/go/gekko3d/gekko-editor/src/modules/level_editor)
- [gekko-editor/src/modules/editor_core](/Users/ddevidch/code/go/gekko3d/gekko-editor/src/modules/editor_core)
- [gekko-editor/src/modules/ui_panels](/Users/ddevidch/code/go/gekko3d/gekko-editor/src/modules/ui_panels)
- [gekko/docs/editor/integration.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/editor/integration.md): engine/editor schema and preview boundary

## Project Patterns And Gotchas

- Modules install systems into an `App` during `build()`. There is no separate dependency injection container beyond the reflective resource lookup in [gekko/app.go](/Users/ddevidch/code/go/gekko3d/gekko/app.go).
- Systems receive `*Commands` or registered resource pointers. If a dependency is missing from `app.resources`, the engine panics with a reflective resolution error.
- Entity and component mutations are buffered. `AddEntity`, `AddComponents`, `RemoveComponents`, and `RemoveEntity` take effect when the app flushes commands after each stage, not immediately at the call site.
- State changes are also buffered through `Commands.ChangeState`, then applied by the main loop.
- The editor uses two high-level modes, `AssetEditorMode` and `LevelEditorMode`, switched in [gekko-editor/main.go](/Users/ddevidch/code/go/gekko3d/gekko-editor/main.go).
- The renderer is deeply integrated but still bridged through ECS-facing modules. If you need to understand scene extraction, inspect the `mod_voxelrt_client*.go` files before changing `voxelrt/rt/...`.
- Sample apps often contain cwd fallbacks such as checking `assets/...` first and then `<module>/assets/...`. Running from the module directory is still the safest default.

## Documentation Map

- [gekko/README.md](/Users/ddevidch/code/go/gekko3d/gekko/README.md): short engine overview
- [gekko/docs/README.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/README.md): canonical documentation index
- [gekko/docs/engine/runtime.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/engine/runtime.md): core app loop, stages, buffering, and resource resolution
- [gekko/docs/engine/modules.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/engine/modules.md): module ownership and stage placement
- [gekko/docs/engine/ecs.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/engine/ecs.md): ECS archetypes, query semantics, and structural changes
- [gekko/docs/engine/physics.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/engine/physics.md): physics pull/push stages and async simulation model
- [gekko/docs/engine/verification.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/engine/verification.md): engine verification guide outside renderer specifics
- [gekko/docs/assets/runtime-assets.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/assets/runtime-assets.md): runtime asset ownership and creation paths
- [gekko/docs/editor/integration.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/editor/integration.md): engine/editor integration surface
- [gekko/docs/renderer/overview.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/renderer/overview.md): renderer landing page
- [gekko/docs/renderer/change-guide.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/renderer/change-guide.md): renderer ownership, invariants, and failure modes
- [gekko/docs/renderer/particles.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/renderer/particles.md): particle pipeline details
- [gekko/docs/content/levels.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/content/levels.md): authored level model
- [gekko/docs/content/streaming-and-worlds.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/content/streaming-and-worlds.md): terrain, imported worlds, streaming, and world deltas
- [gekko/docs/workflows/add-module.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/workflows/add-module.md): playbook for adding a module
- [gekko/docs/workflows/add-content-type.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/workflows/add-content-type.md): playbook for extending authored schemas
- [gekko/docs/workflows/agent-task-loop.md](/Users/ddevidch/code/go/gekko3d/gekko/docs/workflows/agent-task-loop.md): classify a task, read the right docs first, and choose the smallest useful verification pass
- [gekko/voxelrt/rt/shaders](/Users/ddevidch/code/go/gekko3d/gekko/voxelrt/rt/shaders): pass-specific shader sources

## Practical Workflow

- Pick a module first, then run commands from that module directory.
- When touching engine behavior, verify `gekko/` and any directly affected consumer such as `gekko-editor/` or `actiongame/`.
- When touching shared content types, verify both `gekko/content/...` tests and editor tests.
- When touching renderer-facing code, read the renderer docs before editing low-level `voxelrt/rt/...` code.
