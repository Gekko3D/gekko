# STATUS_REPORT

## Goal

Represent auto-return elevator-shaft descent as a standard navigation drop
with optional dynamic landing support, while preserving ordinary carrier ride
links for conventional elevators.

## Scope

- Primary owner: navigation content schema and graph generation.
- Consumers: grounded character traversal and ActionGame locomotion.
- Non-scope: per-level Crossfire logic, arbitrary rigid-body platforms, and
  changes to ordinary static drops.

## Invariant

The motor owns physical drop completion. Navigation may wait for dynamic
support, but cannot claim success until live ground contacts identify the
expected entity. Temporary carrier motion does not invalidate the graph link.

## Change

- Navigation graph tile schema 6 and builder `voxel_graph_v15` add optional
  `landing_support` metadata to drop traversal links.
- Auto-return carrier descent now emits `drop`, not `carrier` with a drop
  boarding mode.
- Ordinary carrier links retain board, activate, ride, and exit behavior.
- ActionGame waits for and holds the required support stop, sends its live
  entity to the shared motor, retries transient landing availability failures,
  and exits to the static destination span after settlement.
- Carrier waits and retries use elapsed seconds rather than render frames.

## Verification

- Passed: content tests, focused dynamic-support motor tests, ActionGame
  startup tests, navigation-graph lab tests, and both repository builds.
- Rebuilt Crossfire and Gasworks with `voxel_graph_v15`; both report zero
  hard validation errors.
- Crossfire `navdiag` confirms upper-to-lower is a `drop` with
  `landing_support=hl1_moving_func_door_1@closed`, while lower-to-upper remains
  a `carrier` link with its button controller.
- Manual Crossfire windowed acceptance remains required.
