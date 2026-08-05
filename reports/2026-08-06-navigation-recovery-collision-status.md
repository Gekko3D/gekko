# STATUS_REPORT

## Goal

Keep bots moving safely across navigation graph revisions without treating
temporarily unavailable streamed collision as proof that a recovery anchor is
invalid.

## Scope

- Expose a read-only streamed-collision readiness query for a small world-space
  bounds.
- Let ActionGame distinguish ready, retryable, and invalid recovery anchors.
- Reuse the existing bounded off-navigation recovery timeout.

Non-scope: new tests, synchronous chunk loading, worker-side ECS access, or a
new retry scheduler.

## Ownership and invariants

- Primary owner: Gekko streamed-level runtime.
- Consumer: ActionGame NPC navigation.
- Route workers remain immutable and navigation-only; streamed collision and
  live obstruction checks remain on the main thread.
- ActionGame never plans from or moves toward an unvalidated recovery anchor.

## Verification

- Passed: streamed runtime/navigation tests in `gekko`.
- Passed: `go vet .` in `gekko`.
- Passed: `go test ./...` and `go vet ./...` in `actiongame`.
- The full `gekko` root suite remains blocked by the unrelated existing
  `TestVoxelRtBridgeRendererInternalsTouchpointsAreAudited` BufferManager audit
  failure in `mod_voxelrt_client_systems.go`.
- Manual follow-up: trigger a real navigation rebake while a bot is moving and
  confirm collision residency delays recovery without clearing navigation.
