# STATUS_REPORT

## Metadata
- Owner: Codex
- Date: 2026-08-02
- Project/Area: Gekko streamed navigation runtime and authored navigation deltas
- Related Links: `docs/roadmaps/dynamic-destructible-navigation-rework-plan.md`

## Executive Summary
- Goal: implement Phase 4 transactional destruction rebuilds.
- Current state: Phase 4 is implemented; asynchronous rebuilds are generation-checked, conservatively blocked, transactionally persisted, and immutably published.
- Scope: profile-aware dirty expansion, conservative blockers, coalesced generations, stale-result rejection, unique atomic sidecars, and atomic immutable publication.
- Non-scope: per-route dependency epochs and replan budgeting (Phase 5).

## Invariants
- A voxel edit becomes conservatively blocked before background work starts.
- A stale batch never changes the published world delta or resident query.
- A world-delta manifest never references a partial sidecar batch.
- Resident source, graph, seam, and overlay state publish through one query pointer swap.

## Design
- Keep the existing single rebuild worker and dirty-chunk map; add a monotonic edit generation and retain newer edits while a build runs.
- Reuse the existing blocker overlay with generated edit blockers instead of adding a second routing path.
- Persist generation-qualified sidecars with temp-file, sync, and rename; publish their references only after the whole batch succeeds.
- Reject runtime-generation or edit-generation mismatches before committing the delta.

## Testing Plan
- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- `env GOCACHE=/tmp/gekko3d-gocache go test .`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- Edge cases: removed floor/wall, added obstruction, repeated edit, seam edit, edit during build, stale result, and blocker retirement.

## Risk / Follow-up
- Phase 5 still owns route dependency epochs; Phase 4 increments the existing global navigation revision.
- Orphan generation-qualified sidecars after a crash or stale build are safe and may be garbage-collected by a future maintenance tool if measured necessary.
- Verification: content, race-enabled focused runtime navigation, and Actiongame suites pass. The broad engine sweep passes every package except the pre-existing renderer architecture audit at `mod_voxelrt_client_systems.go:745`.

# END_STATUS_REPORT
