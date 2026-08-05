# STATUS_REPORT

## Goal

Apply the reviewed long-term ActionGame bot scalability fixes without changing
sequential ECS ownership or adding workers.

## Scope

- Add stable, stoppable spatial-grid visitation in the engine.
- Bound ActionGame vision candidate examination before LOS.
- Cancel deferred reasoning on critical invalidations.
- Measure route latency through result application.
- Make the scale scenario fixed-step and scripted.
- Filter and rank cheap vision candidates before applying the 24-candidate cap.
- Suppress human gameplay input before scale-mode consumers run.
- Establish deterministic combat teams, pairing, targets, and aim.
- Provide logical-tick route solving for repeatability runs while preserving
  the asynchronous worker for performance runs.
- Require exact scale population and deterministic mutual-LOS combat formations
  projected from authored spawn anchors; keep spatial-grid explosion guards at
  4096 visited cells.

Non-scope: new tests, a generic scheduler, additional route workers, or raising
the supported bot limit.

## Ownership and invariants

- Primary owner: ActionGame bot runtime.
- Shared engine surface: `SpatialHashGrid` query API.
- Preserve deterministic iteration, buffered ECS mutation, main-thread gameplay
  ownership, and one pending route request per entity.

## Verification

- Passed: focused `SpatialHashGrid` tests and `go vet .` in `gekko`.
- Passed: `go test ./...`, startup race check, `go vet`, and one-shot 20/50
  bot benchmarks in `actiongame`.
- Passed after final review fixes: `go test ./...`, `go vet ./...`,
  `go test -race ./src/modules/startup`, and the existing route-worker test
  under logical-tick scale-route mode.
- Final 20-iteration cognition benchmarks: 20 bots `72.6 us/op`, 50 bots
  `236.2 us/op` on Apple M4 Pro.
- The full `gekko` root suite remains blocked by the unrelated existing
  `TestVoxelRtBridgeRendererInternalsTouchpointsAreAudited` BufferManager
  audit failure in `mod_voxelrt_client_systems.go`.
- Manual follow-up: rerun the 60-second windowed 20/32/50 scale captures.
