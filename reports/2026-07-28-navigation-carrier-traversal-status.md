# STATUS_REPORT

## Goal

Make lifts first-class navigation traversals so actors can call, board, ride,
and exit a moving support without being localized against the static graph.

## Scope

- Primary owner: authored navigation content and graph generation.
- Consumers: streamed navigation overlays, moving-brush interaction, and
  ActionGame locomotion.
- Crossfire's two vertical `func_door` lifts are the first authored consumers.
- Non-scope: arbitrary physics debris, rotating ride surfaces, and path-train
  stop authoring.

## Confidence

- High.
- Moving-brush collision already carries actors and reports support entity IDs.
- Navigation already owns stable traversal links and failure quarantine.
- The missing contract is carrier stops plus traversal execution while static
  localization is suspended.

## Chosen design

- Add a generic `carrier` navigation role and transition.
- Bake directed links between static station spans. A traversal stores carrier
  ID, source/destination stop IDs, a boarding point, and an optional controller.
- Keep the moving brush out of static navigation occupancy.
- Reuse explicit-state trigger activation and existing moving-brush motion.
- Execute `wait_source -> board -> request_destination -> ride -> exit`.
- Confirm board/ride through the motor's support entity and confirm exit through
  the exact destination static span.
- Disable links when the authored carrier entity is absent.

## Compatibility

- The schema change is additive. Existing graph bundles contain no carriers and
  retain their current behavior.
- Newly carrier-authored levels require a navigation rebake.

## Verification

- Passed `env GOCACHE=/tmp/gekko3d-gocache go build ./...` in `gekko`.
- Passed `env GOCACHE=/tmp/gekko3d-gocache go build ./...` in `actiongame`.
- Crossfire rebaked with four carriers and zero hard validation errors.
- A lower-to-upper `lift1` query selects the directed
  `closed -> open` carrier traversal.
- A goal inside the old static lift footprint now returns
  `goal_unsupported`.
- Do not add or run tests per the user's earlier instruction.
- Manual Crossfire bot testing remains user-owned.
