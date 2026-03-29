# Engine Verification

This page collects the non-renderer verification paths for `gekko`.

Use renderer-specific commands from [`../renderer/verification.md`](../renderer/verification.md) when the change is primarily inside `voxelrt`.

Run commands from the module root:

`cd /Users/ddevidch/code/go/gekko3d/gekko`

In this sandbox, prefer:

`env GOCACHE=/tmp/gekko3d-gocache ...`

## First Rule

Do not default to `go test ./...` as the first verification step.

It is broader and slower than necessary, and in this environment some package patterns need the explicit temporary Go cache to avoid sandbox cache-permission failures.

Use the smallest package or workflow that matches the change.

## Package-Level Checks

### Content and authored-data changes

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`

Use this for:

- asset schemas
- levels
- terrain baking
- imported worlds
- world deltas

### ECS internals

- `env GOCACHE=/tmp/gekko3d-gocache go test ./ecs/...`

Current note:

- the `ecs` package currently has no test files, so this is mostly a compile check

### Physics package compile check

- `env GOCACHE=/tmp/gekko3d-gocache go test ./physics/...`

Current note:

- the `physics` package currently has no test files, so this is also mainly a compile check

## High-Value Root Package Checks

These live in the top-level `gekko` package and are often the right target for agent work.

### Asset spawning and authored content runtime

Run the root-package tests when changing:

- `asset_content_spawn.go`
- `level_content_spawn.go`
- `runtime_content_loader.go`
- imported-world spawn helpers

Recommended command:

- `env GOCACHE=/tmp/gekko3d-gocache go test .`

Then, if the change is content-heavy, also run:

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`

### Streaming and world-delta runtime

Run:

- `env GOCACHE=/tmp/gekko3d-gocache go test .`

Focus on this after changes to:

- `streamed_level_runtime.go`
- `world_delta_*`
- terrain or imported-world runtime integration

### Physics integration

Run:

- `env GOCACHE=/tmp/gekko3d-gocache go test .`

Focus on this after changes to:

- `mod_physics_*`
- `mod_vox_physics.go`
- `mod_grounded_player.go`
- collision or rigid-body integration

### Spatial and hierarchy changes

Run:

- `env GOCACHE=/tmp/gekko3d-gocache go test .`

Focus on this after changes to:

- `mod_hierarchy.go`
- `mod_spatialgrid.go`
- `mod_chunking.go`

## When To Use Broad Engine Tests

Use:

- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

only when:

- a change crosses multiple engine subsystems
- module registration changed
- you changed shared types used in many root-package files
- you are doing a final confidence pass before handing off a broad refactor

Treat this as a final sweep, not a default first move.

## Cross-Module Checks Outside `gekko`

When touching shared engine behavior, also verify the affected consumer module.

Useful commands:

- editor:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko-editor && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- action game:
  - `cd /Users/ddevidch/code/go/gekko3d/actiongame && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- space game:
  - `cd /Users/ddevidch/code/go/gekko3d/spacegame_go && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- voxel example:
  - `cd /Users/ddevidch/code/go/gekko3d/examples/testing-vox && env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Known caveat:

- `examples/testing` is currently a known failing sample and should not be used as a confidence signal

## Practical Matrix

- changed content schema or validation:
  - `./content/...`
  - then `.` if runtime spawn behavior changed
- changed authored asset or level spawn:
  - `.`
  - then `./content/...`
- changed streamed level runtime:
  - `.`
  - then the affected consumer module if one uses that runtime path
- changed physics integration:
  - `.`
  - and optionally `./physics/...` as a compile check
- changed module install order or shared engine runtime:
  - `.`
  - then `./...` if the change was broad

## Visual and Interactive Checks

Use a windowed run only when the change actually needs interaction or rendering confirmation:

- editor:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko-editor && env GOCACHE=/tmp/gekko3d-gocache go run .`
- action game:
  - `cd /Users/ddevidch/code/go/gekko3d/actiongame && env GOCACHE=/tmp/gekko3d-gocache go run .`

These require a real desktop session.
