# STATUS_REPORT

## Metadata
- Owner: Codex
- Date: 2026-08-02
- Project/Area: Gekko navigation snapshots and Actiongame route consumption
- Related Links: `docs/roadmaps/dynamic-destructible-navigation-rework-plan.md`

## Executive Summary
- Goal: implement Phase 5 route dependency epochs.
- Current state: routes are validated by immutable per-tile topology epochs instead of the global publication revision.
- Scope: snapshot epochs, route dependencies, runtime validation, hard/soft replan scheduling, stale async rejection, traces, HUD, and canonical docs.
- Non-scope: Phase 6 tool/asset rebuilds and Phase 7 performance tuning.

## Invariants
- A route is usable only while every recorded resident tile has the recorded topology epoch.
- A changed or unloaded dependency stops locomotion and active traversal before replanning.
- An unrelated global publication does not invalidate a route.
- Async work may publish only for the current actor request and current route dependencies.

## Design
- Compare consecutive immutable resident tile representations at publication; reuse the existing effective topology instead of maintaining a second affected-tile algorithm.
- Record sorted dependencies from route endpoints, region steps, and refined waypoint spans.
- Use a priority channel for hard-invalid routes and a one-route-per-frame admission budget for ordinary replans; retain the existing single worker.
- Keep `NavigationRevision` for diagnostics and legacy hand-built results.

## Verification
- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...` — pass.
- `env GOCACHE=/tmp/gekko3d-gocache go test .` — blocked only by the pre-existing renderer bridge audit at `mod_voxelrt_client_systems.go:745`.
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...` from `actiongame/` — pass.
- `env GOCACHE=/tmp/gekko3d-gocache go test ./content -run '^$' -bench 'BenchmarkNavGraphPhase0/dense_flat/route_dependency_validation' -benchtime=1x` — 375 ns/op, 0 allocations on Apple M4 Pro.
- Focused epoch test covers affected versus unaffected routes and the seam dependency halo.
- Manual Crossfire destruction acceptance was not run in this non-windowed pass.

## Risk / Follow-up
- Publication compares immutable tile state with `reflect.DeepEqual`; Phase 7 should replace it only if publication profiling shows material cost.
- The Crossfire manual gate remains the final gameplay acceptance check.

# END_STATUS_REPORT
