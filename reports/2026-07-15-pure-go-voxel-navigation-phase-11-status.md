# Phase 11: Shooter Traversal And Dynamic State

## Status

- Confidence: High for the ladder slice.
- Owner: engine content contracts and runtime navigation.
- Consumers: actiongame locomotion; level/importer traversal metadata.
- Architecture: long-term voxel graph replacement, not a tactical bridge.
- Scope: ladder traversal only, following the phase instruction to implement
  one observed gameplay need at a time.

## Known

- Static graph currently emits only `walk`, `step`, and `stair` transitions.
- Routes already expose transition kind through `RequiredAction`.
- Runtime navigation already swaps immutable, revisioned graph queries.
- Level content already owns ladders, water, moving brushes, and breakables.
- Actiongame already owns physics locomotion, stall/replan, and local avoidance.
- Dynamic doors and movers must change transition state/cost without rebuilding
  static spans.

## Implemented Long-Term Change

- Reuse format-neutral `.gklevel` `ladder_volumes`; HL1 import is only one
  possible producer.
- Resolve derived or explicit bottom/top mounts against supported source spans
  at bake time.
- Emit bidirectional `ladder` transitions only for profiles with the
  `climb_ladder` capability, carrying stable ladder ID and world-space mounts.
- Carry the traversal binding through route steps and waypoints.
- Execute NPC climbs in actiongame using shared collision helpers and the live
  ladder's climb speed. Bounds plus resolved mounts select a format-neutral
  outside climb lane; movement is staged as mount, vertical climb, and
  dismount so the character capsule does not sweep diagonally into the wall or
  tower lip. Top-side mount and dismount reserve the NPC's existing step-height
  clearance before crossing a raised lip, then settle onto the validated nav
  support.
- Allow optional positive ladder health. Existing breakable/explosion damage
  removes the whole ladder entity, including its movement volume.
- Derive a runtime disabled-traversal overlay from missing ladder entities and
  bump the immutable navigation revision without rebuilding voxel spans.

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

## Decisions

- Traversal facts remain on their existing level-owned gameplay definitions;
  no HL1-specific ladder schema was added.
- `navbake -level` resolves both base-world geometry and ladder metadata;
  `-world` remains available for geometry-only bakes.
- Ladder destruction is opt-in through `health`; imported ladders remain
  indestructible unless authored otherwise.
- Other Phase 11 traversal types remain deferred and must reuse the same
  directed-transition/runtime-overlay boundary where applicable.

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
- Manual Crossfire/Gasworks checks: NPC routes up and down ladders; player and
  NPC ladder movement cease after a strong explosion destroys a health-enabled
  ladder; NPC replans instead of using the removed link.

## Verification Results

- Passed engine/runtime and content suites:
  `GOCACHE=/tmp/gekko3d-gocache go test . ./content/...`.
- Passed the pure-Go content boundary:
  `CGO_ENABLED=0 GOCACHE=/tmp/gekko3d-gocache-nocgo go test ./content/...`.
- Passed all actiongame packages when excluding the unrelated existing
  `TestShotgunProfileTurnsWorldModelForward` attachment-fixture failure.
- Passed focused actiongame ladder handoff tests.
- Passed focused bidirectional ladder execution regression: mounting preserves
  height, climbing preserves the lane's X/Z, and dismount begins only at the
  destination height.
- Passed bidirectional low-ledge regression: descending mount and ascending
  dismount both lift above the lip before crossing it.
- Reproduced the reported Crossfire route (`0:-2:1/42085` to
  `0:-2:1/49424`) against the real voxel collision chunk; exact ladder
  endpoints complete in both directions.
- Manual Crossfire/Gasworks climb, collision, animation, explosion, and reroute
  acceptance remains required; automated tests cannot judge those behaviors.
- Crossfire's local graph was rebaked from `.gklevel` with all four authored
  ladder IDs. Both tower top-to-field routes now resolve through a two-step
  ladder route. The local level references that manifest directly.
