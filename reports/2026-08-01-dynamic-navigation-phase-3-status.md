# Dynamic Navigation Phase 3 Status

## Scope

- Primary owner: engine content navigation queries
- Affected consumers: runtime blockers, breakables, carriers, Actiongame tactical reachability, and `navdiag`
- Confidence: High
- SME alignment required: No

## Result

Blocker AABBs now resolve through intersecting tile and dense-column ranges.
Only baked regions containing blocked spans are flood-filled into overlay
components; unaffected baked regions retain one baseline component. Route and
reachability searches use the component portal graph and refine selected
segments through active tile-local CSR edges.

The production global resident-span edge map and global span A* were deleted.
Carrier swept footprints and runtime blockers continue through the same query
constructor, so no consumer compatibility layer was added.

## Preserved Invariant

Reachability remains exact for the active immutable snapshot and overlay.
Blocked spans cannot be localized, traversed, or counted as active exits, while
explicit carrier, ladder, gate, and other exceptional transitions retain their
action metadata during local refinement.

## Measurements

Reference: Darwin/arm64, Apple M4 Pro.

- Crossfire carrier-overlay snapshot: `225,413,800` bytes
- Crossfire overlay index build: `1,440.039 ms`
- Crossfire `lift2` carrier route, 50 samples: p50 `0.018667 ms`, p95 `0.027084 ms`, p99 `0.048042 ms`
- Synthetic carrier-blocked route: `0.150488 ms/op`
- Synthetic small overlay rebuild: `4.364442 ms/op`
- Synthetic large split-region overlay rebuild: `4.506054 ms/op`

## Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...` — pass
- `env GOCACHE=/tmp/gekko3d-gocache go test ./cmd/navdiag` — pass
- `env GOCACHE=/tmp/gekko3d-gocache go test . -run 'Navigation|NavGraph'` — pass
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...` in Actiongame — pass
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...` in Gekko — navigation packages pass; broad sweep is blocked by the unrelated renderer audit failure at `mod_voxelrt_client_systems.go:745`
- Generated blocker layouts match the independent Dijkstra reachability oracle.
- Crossfire `lift2` lower-to-upper route retains its `carrier` traversal and controller binding.
- Manual Actiongame lift execution was not run; locomotion behavior did not change.
