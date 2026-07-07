# Grounded Character Collision Status

- Date: 2026-07-07
- Owner: Gekko character controller
- Consumer: actiongame grounded player

## Goal

Prevent the player from falling through thin voxel floors and make rises no
higher than the configured step height walkable without jumping.

## Change

- Added shared grounded movement with depenetration, slide, step-up, and
  walkable landing snap.
- Added shared vertical footprint sweep and routed ordinary player gravity and
  jumping through it.
- Left ladders and swimming on their existing movement path; they reuse the
  shared vertical sweep through the existing controller helper.

## Invariant

The player may step only onto a walkable landing within `StepHeight`; vertical
motion stops at a voxel floor or ceiling rather than passing through it.

## Verification

- Passed: `env GOCACHE=/tmp/gekko3d-gocache go test . -count=1` in `gekko/`.
- Passed: `env GOCACHE=/tmp/gekko3d-gocache go test . -count=1` in
  `actiongame/`.
- Added deterministic checks for a walkable riser, a tall wall, a thin floor,
  and a floor represented by two adjacent voxel objects.
- Smoke launched Crossfire and reached `Running in stateful mode`.

## Not Verified

- Manual GPU movement through Crossfire/Subtransit ramps, stairs, and streaming
  chunk boundaries still needs a desktop play session.
