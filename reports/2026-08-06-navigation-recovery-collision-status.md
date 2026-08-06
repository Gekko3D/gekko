# STATUS_REPORT

## Goal

Keep bots moving safely across navigation graph revisions without treating
temporarily unavailable streamed collision as proof that a recovery anchor is
invalid.

## Scope

- Expose a read-only streamed-collision readiness query for a small world-space
  bounds.
- Let ActionGame distinguish ready, retryable, and invalid recovery anchors.
- Separate collision waiting from locomotion stall accounting, poll recovery
  collision at 10 Hz, and use a three-second game-time watchdog.
- On watchdog expiry, validate and replan from the bot's current grounded
  support before reporting `actor_off_nav`.
- Prefer verified current grounded support before falling back to a previous
  graph anchor, and finish recovery before requesting a route.
- Bound collision-readiness scans to 4096 chunks so malformed or stale recovery
  bounds cannot create unbounded main-thread work.

Non-scope: new tests, synchronous chunk loading, worker-side ECS access, or a
new retry scheduler.

## Ownership and invariants

- Primary owner: Gekko streamed-level runtime.
- Consumer: ActionGame NPC navigation.
- Route workers remain immutable and navigation-only; streamed collision and
  live obstruction checks remain on the main thread.
- ActionGame never plans from or moves toward an unvalidated recovery anchor.
- Collision residency waiting never consumes locomotion blocked/stalled frames.
- A graph revision or target refresh does not restart an active recovery
  watchdog.

## Verification

- Passed: streamed runtime/navigation tests in `gekko`.
- Passed: `go vet .` in `gekko`.
- Passed: `go test ./...` and `go vet ./...` in `actiongame`.
- The full `gekko` root suite remains blocked by the unrelated existing
  `TestVoxelRtBridgeRendererInternalsTouchpointsAreAudited` BufferManager audit
  failure in `mod_voxelrt_client_systems.go`.
- Manual follow-up: trigger a real navigation rebake while a bot is moving and
  confirm collision residency delays recovery without clearing navigation.
