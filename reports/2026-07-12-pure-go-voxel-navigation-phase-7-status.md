# Phase 7: Hierarchical Routing

## Status

- Confidence: High
- Owner: engine content; runtime and actiongame remain later consumers
- Architecture: long-term replacement step, not a bridge
- Source: `docs/roadmaps/pure-go-voxel-navigation-generation-plan.md`

## Scope And Decision

- Resolve world X/Z columns to the nearest vertically supported profile span.
- Treat loaded graph tiles as implicit sectors; use tile A* to restrict normal
  cross-tile region A*, with unrestricted region fallback for valid detours.
- Back every region crossing with a real directed span transition.
- Run span A* only in the start and goal tiles; return crossing anchors and the
  projected supported goal as simple waypoints.
- Return stable unsupported/no-route reasons and transition actions.
- Leave route budgets, cancellation, revisions, persistence, runtime streaming,
  gameplay locomotion, and smoothing to their owning later phases.

## Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content -run '^(TestFindNavGraphRoute|TestNavSpanGraphReachability|TestCompressNavGraphRegions|TestConnectNavGraphTiles)$'`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- `env CGO_ENABLED=0 GOCACHE=/tmp/gekko3d-gocache-nocgo go test ./content/...`
- `git diff --check`

Synthetic coverage includes long cross-tile routing, region compression instead
of full span expansion, stacked-floor resolution, supported-point projection,
transition actions, and explicit start/goal/no-route failures.

Manual Crossfire/Gasworks routing is intentionally not run: Phase 8 still owns
full-world baking, loading, diagnostics, and the navigation graph lab. No visual
or GPU behavior changed.

## Results

- Focused navigation suite: pass.
- Route determinism check (`-count=20`): pass.
- Content suite: pass.
- Pure-Go content suite: pass.
- `git diff --check` and `gofmt -d`: pass.
- Manual visual/GPU checks: intentionally not run.
