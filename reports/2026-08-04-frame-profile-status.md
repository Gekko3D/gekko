# STATUS_REPORT

## Metadata
- Owner: Codex
- Date: 2026-08-04
- Project/Area: engine runtime profiling / ActionGame AI scale harness
- Related Links: `actiongame/docs/bot-ai-scalability-plan.md`

## Executive Summary
- Goal: let the fixed ActionGame scale run calculate frame and category percentiles without parsing logs.
- Scope: publish the completed frame's existing profiler totals as a read-only resource; ActionGame samples it only in scale mode.
- Non-scope: ECS ordering, renderer behavior, worker scheduling, and always-on telemetry.
- Risk: additive resource/API only; profiling remains disabled unless `GEKKO_SLOW_FRAME_MS` is set.

## Design
- `TimeModule` owns `FrameProfile` beside `Time`.
- `App` updates it after the frame completes using the existing system timing data.
- The category map is reused each frame; consumers must treat it as the previous completed frame and must not retain mutable references.

## Verification
- Passed: `cd gekko && env GOCACHE=/tmp/gekko3d-gocache go test . -run '^(TestAppProfileCategorySummary|TestAppPublishesCompletedFrameProfile)$' -count=1`
- Passed: `cd gekko && env GOCACHE=/tmp/gekko3d-gocache go vet .`
- Passed: `cd actiongame && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- Passed: focused ActionGame race tests for route application and admission fairness.
- Existing unrelated failure: the full `gekko` root suite rejects an unaudited `BufferManager` touchpoint in `mod_voxelrt_client_systems.go:760`.

## Rollback
- Remove `FrameProfile` registration/publication and return ActionGame scale reporting to log-only timing.

# END_STATUS_REPORT
