# Phase 4: Build Directed Span Graph

## Status

- Confidence: High
- Owner: engine content; runtime and actiongame remain later consumers
- Architecture: long-term replacement step, not a bridge
- Source: `docs/roadmaps/pure-go-voxel-navigation-generation-plan.md`

## Scope And Decision

- Filter source spans through the existing agent-profile pass.
- Create deterministic directed candidates between four-neighbor columns.
- Reject candidates exceeding step height, overlapping headroom, or clearance
  limits with stable diagnostic codes.
- Label level edges `walk`, nonlevel edges within slope as `stair`, and steeper
  supported edges within step height as `step`.
- Run local A* over one graph tile with explicit missing-start, missing-goal,
  and no-route results.
- Leave drops, jumps, special traversal, regions, seams, runtime state, and
  gameplay integration to later phases.

## Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content -run '^(TestBuildNavSourceSpans|TestNavSpanClearanceAndProfileSupport|TestNavSpanGraphReachability)$'`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- `env CGO_ENABLED=0 GOCACHE=/tmp/gekko3d-gocache-nocgo go test ./content/...`
- `git diff --check`

Table test covers walk, step, stair, reciprocal directed deltas, excessive
steps, transition headroom, transition clearance, four-neighbor exclusion,
lowest-cost routing, reachable routes, disconnected components, and explicit
search failures.

## Results

- Focused navigation tests: pass.
- Content suite: pass.
- Pure-Go content suite: pass.
- `git diff --check`: pass.
- Manual visual/GPU checks: intentionally not run; Phase 4 has no renderer or
  runtime integration.
