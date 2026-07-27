# STATUS_REPORT

## Goal

Prevent bots from stalling when route execution confuses vertically distinct
navigation support at the same horizontal position.

## Scope

- Engine content query: bind route endpoints only to spans within one
  navigation voxel in 3D.
- Engine route contract: pair every waypoint with its canonical span.
- ActionGame: select roaming destinations from reachable resident graph spans
  and advance route progress by span identity.
- ActionGame: backtrack grounded actors to their last confirmed span before one
  bounded route repair, and preserve terminal navigation failure causes.
- Update focused navigation and bot tests plus endpoint documentation.

## Non-scope

- Navigation rebake or schema changes.
- Character-motor, collision, ladder, door, or tactical-scoring changes.

## Confidence

- High.
- Crossfire graph contains the correct floor component. Current endpoint
  resolution instead selects an overhead span four metres above the actor.
- Projecting the same start five centimetres onto the floor makes the failed
  routes succeed.

## Invariants

- Route starts may absorb at most one navigation voxel of motor drift.
- Exact destinations never bind to a vertically distant surface.
- Roam requests only active destinations reachable from the actor's current
  component.
- Horizontal proximity alone never completes a waypoint on another support.

## Verification

- `env GOCACHE=/private/tmp/gekko3d-codex-go-cache go test ./...` in `gekko`.
- `env GOCACHE=/private/tmp/gekko3d-codex-go-cache go test ./...` in
  `actiongame`.

## Result

- Completed.
- Original Crossfire route from `(-23.95, -43, -22.88)` to
  `(-17.90, -43, -26.06)` now succeeds entirely on `y=-43`; it previously
  failed or produced `y=-39` waypoints.
- Stair regression no longer skips lower-support waypoints while actor remains
  on upper support.
- Grounded off-graph actors backtrack to last confirmed support and then replan
  the original target once.
- All verification commands passed.
