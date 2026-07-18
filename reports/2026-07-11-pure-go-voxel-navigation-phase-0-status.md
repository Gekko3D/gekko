# Phase 0: Remove Polygon Navigation Stack

## Status

- Confidence: High
- Owner: engine content; runtime and actiongame are affected consumers
- Architecture: long-term replacement step, intentionally breaking
- Source: `docs/roadmaps/pure-go-voxel-navigation-generation-plan.md`

## Scope

- Delete Recast/CGO navigation code and vendored Recast.
- Delete polygon schemas, builders, queries, routing, persistence, and tests.
- Remove old runtime, importer, actiongame, example, and generated-asset consumers.
- Keep unrelated streaming, voxel editing, NPC, and physics code compiling.

## Decision

Delete legacy navigation end to end. Do not add stubs, adapters, fallbacks, or
temporary compatibility APIs. Navigation remains unavailable until later graph
phases restore it.

## Verification

- `env CGO_ENABLED=0 GOCACHE=/tmp/gekko3d-gocache-nocgo go test ./content/...`
- `env GOCACHE=/tmp/gekko3d-gocache go test . ./content/...`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...` in `actiongame`
- compile directly affected importer/example modules
- scan tracked source for Recast, polygon-nav, old builder, and old sidecar APIs

No manual visual/GPU check is required: Phase 0 intentionally removes navigation
generation, routing, movement integration, and debug rendering.

## Completed

- Removed legacy engine content/runtime/CLI navigation code and Recast sources.
- Removed level/world-delta legacy navigation schemas and importer bake hooks.
- Removed actiongame navigation registration, routing, locomotion handoff, and debug code.
- Removed editor delta-navigation rebuild UI/runtime paths.
- Removed `examples/navmesh_lab` and generated `.gknav*` assets.
- Marked every Phase 0 checklist item complete.

## Results

- Pure-Go content suite: pass.
- Engine-wide suite: pass.
- Actiongame compile-only sweep and focused streaming/NPC tests: pass.
- Editor compile-only sweep and level-editor suite: pass.
- Workspace forbidden-symbol/file scan: pass.
- `git diff --check`: pass in affected repositories.

Full actiongame test execution reaches one unrelated fixture failure:
`TestShotgunProfileTurnsWorldModelForward` reports `missing shotgun attachment`.
