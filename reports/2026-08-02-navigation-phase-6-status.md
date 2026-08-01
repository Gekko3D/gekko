# STATUS_REPORT

## Metadata
- Owner: Codex
- Date: 2026-08-02
- Project/Area: compact navigation consumers, assets, and documentation
- Related Links: `docs/roadmaps/dynamic-destructible-navigation-rework-plan.md`

## Executive Summary
- Goal: complete Phase 6 and remove the last old-format consumer/asset assumptions.
- Primary owner: engine content and streamed navigation runtime.
- Affected consumers: Actiongame, `navbake`, `navdiag`, and the navigation graph lab.
- Progress: implementation and headless checks complete; windowed gameplay acceptance remains manual.

## Invariants
- Runtime and tools load only `.gkns` and `.gkng` tiles from an immutable `.gknav` snapshot.
- Special traversals identify topology with `link_id` and gameplay ownership with `owner_id`; the old overloaded `id` field is gone.
- World-delta source and graph overrides publish transactionally with the resident query snapshot.
- Routes continue to carry sorted tile/epoch dependencies, and runtime-blocked spans remain non-traversable.

## Completed
- Bumped graph manifest/tile schema to v5 and builder to `voxel_graph_v14`.
- Removed the serialized traversal `id` compatibility field and updated engine/Actiongame consumers.
- Added blocked-span coloring to the Actiongame navigation overlay.
- Added `navigation_graph_lab -bake-only`, binary-extension coverage, and route-dependency coverage.
- Rebaked local ignored bundles:
  - Crossfire: 288 tile pairs, 783,595 accepted spans, zero hard errors.
  - Gasworks: 31 tile pairs, 873,026 accepted spans, zero hard errors.
  - Navigation lab: two tile pairs, schema v5/builder v14.
- Deleted the lab's old `.gknavsource`/`.gknavgraph` output and stale compiled executable before rebaking.
- Updated canonical navigation content docs and the phase roadmap.

## Verification
- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...` from `gekko/` — pass.
- Focused root navigation runtime tests from `gekko/` — pass.
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...` from `actiongame/` — pass after rebake.
- `env GOWORK=off GOCACHE=/tmp/gekko3d-gocache go test ./...` from `examples/navigation_graph_lab/` — pass after module metadata refresh.
- `env GOCACHE=/tmp/gekko3d-gocache go run . -bake-only` from the lab — pass.
- Full engine root test remains blocked by the pre-existing renderer audit at `mod_voxelrt_client_systems.go:745`.
- Manual Crossfire, Gasworks, and lab GPU/gameplay acceptance was not run.

## Rollout / Compatibility
- Backward compatibility: none. Schema v4/v13 bundles and traversal payloads must be rebaked.
- Generated Actiongame and lab bundles are intentionally ignored by Git and were rebuilt in the local workspace.
- Rollback requires reverting the schema/code changes and restoring matching older generated bundles together.

## Next Step
- Run the roadmap's windowed Crossfire, Gasworks, and navigation-lab acceptance cases before declaring the phase gate fully green.

# END_STATUS_REPORT
