# STATUS_REPORT

## Goal

Let ActionGame bots take bounded ledge drops, cross jumpable gaps/rises, and
descend auto-returning lift shafts without per-frame geometry searches or
navigation stalls.

## Scope

- Primary owner: voxel navigation graph generation.
- Consumers: streamed dirty-tile rebuilds and ActionGame locomotion.
- Non-scope: water, path trains, tests, and manual app playtesting.

## Change

- Builder `voxel_graph_v13` discovers directed drops only at exposed region
  boundaries, bounded by finite profile `max_drop_height`, and rejects paths
  whose horizontal exit or vertical fall fails a capsule sweep.
- One deterministic link per directed region pair limits graph growth.
- Full and delta builds use the same drop linker.
- Builder `voxel_graph_v13` discovers eight-direction gap/upward jumps and
  validates sampled capsule arcs against sparse source occupancy.
- Jump links store exact horizontal and launch speeds used by ActionGame;
  one deterministic link per directed region pair bounds graph growth.
- Span localization tolerates floating-point error at the one-voxel boundary.
  On an exact/contact miss, ActionGame may bind a grounded bot only to an
  active span directly underneath within one step plus one voxel. This keeps
  temporary support geometry from marooning the bot and runs only on misses.
- Carrier exits complete on any grounded static span in the intended
  destination region. Off-lane exits trigger a clean route refresh from the
  actual span instead of leaving the carrier state steering into an edge.
- Auto-returning vertical carriers emit upper-to-lower `board_mode: "drop"`
  links. Locomotion waits for the lower stop, performs the existing ballistic
  drop onto live carrier support, then exits onto static navigation.
- Existing traversal timeout, failed-link quarantine, and bounded replan own
  failure recovery.

## Verification

- Passed engine, ActionGame, and editor `go build ./...` sweeps.
- Crossfire v13 rebake: 288 source tiles, 288 graph tiles, 78 validated automatic
  drop links, 42 automatic jump links, seven carrier-drop links, zero hard
  validation errors.
- `navdiag` upper-to-lower `lift1` route selected the expected carrier link with
  `board_mode: "drop"`.
- `navdiag` selected both same-height gap and upward automatic jump routes.
- The reported `(11.35, -39, -0.45)` Crossfire span no longer emits its blocked
  window drop; its lower-floor route selects the `lift1` carrier-drop link.
- Reported Crossfire point `(17.95, -46.60, -35.80)` resolves to ramp span
  `76154`; `(8.30, -42.14, -21.82)` resolves downward to floor span `29485` via
  the bounded grounded-support fallback.
- Reported post-`lift2` point `(-10.15, -38.71, -33.75)` resolves downward to
  static span `40762` in carrier destination region `6`; the off-lane handoff
  now completes and replans.
- No tests created or run. App/manual playtesting left to user as requested.
