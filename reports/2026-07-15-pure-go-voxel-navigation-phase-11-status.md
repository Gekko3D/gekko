# Phase 11: Shooter Traversal And Dynamic State

## Status

- Confidence: Medium.
- Owner: engine content contracts and runtime navigation.
- Consumers: actiongame locomotion; level/importer traversal metadata.
- Architecture: long-term voxel graph replacement, not a tactical bridge.
- Blocker: Phase 11 names behaviors but does not define authored-link ownership,
  runtime binding identity, agent capabilities, or moving-platform nodes.

## Known

- Static graph currently emits only `walk`, `step`, and `stair` transitions.
- Routes already expose transition kind through `RequiredAction`.
- Runtime navigation already swaps immutable, revisioned graph queries.
- Level content already owns ladders, water, moving brushes, and breakables.
- Actiongame already owns physics locomotion, stall/replan, and local avoidance.
- Dynamic doors and movers must change transition state/cost without rebuilding
  static spans.

## Proposed Long-Term Change

- Put authored traversal definitions beside `LevelNavigationDef`.
- Bake validated directed links against real source spans and split regions at
  action boundaries.
- Give persisted special transitions a stable binding ID for runtime state.
- Apply immutable enabled/disabled/cost overlays while creating each revisioned
  runtime query.
- Let actiongame execute each route step's `RequiredAction` through existing
  character, ladder, water, moving-brush, and breakable gameplay systems.

## Alternatives

1. Runtime spatial inference from existing gameplay volumes.
   - Smaller schema change.
   - Conflicts with deterministic baking and cannot safely split already
     compressed regions around doors or other action boundaries.
2. Generic state/cost overlay API only.
   - Useful first slice.
   - Tactical and incomplete because current graphs contain no special
     transitions to control.

Recommendation: authored, bake-time transitions plus runtime overlays.

## Decisions Required

- Confirm `LevelNavigationDef` owns traversal links and `navbake` accepts a
  `.gklevel` so it can resolve both base world geometry and traversal metadata.
- Confirm moving platforms may introduce runtime-only support nodes, or defer
  them until that node contract is designed.
- Confirm implementation should deliver every listed behavior now, despite the
  plan's instruction to implement one observed gameplay need at a time, or
  start with one named behavior.

## Likely Files

- `content/nav_graph.go`, validation, bake, region, route, and tests.
- `content/level.go`, level validation, and level tests.
- `cmd/navbake/main.go`.
- `navigation_graph_runtime.go` and runtime tests.
- actiongame navigation locomotion and tests.
- this roadmap and report.

## Verification Plan

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- `env CGO_ENABLED=0 GOCACHE=/tmp/gekko3d-gocache-nocgo go test ./content/...`
- `env GOCACHE=/tmp/gekko3d-gocache go test .`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...` in `actiongame`.
- Manual Crossfire/Gasworks checks for each implemented action and dynamic
  transition state; moving-platform behavior requires visual/physics checking.
